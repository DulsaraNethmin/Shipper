// SHIP-34: phone one-time codes — generation, rate limiting and storage.
//
// Docs/04 §2 requires a customer's phone verified before they may publish, and the OTP is how
// that is established: a value that can only reach the handset holding the number.
//
// # The three properties the ticket names
//
//   - **Time-limited.** Ten minutes, checked against the injected clock. Long enough for a
//     message to arrive over a poor mobile connection; short enough that a code read off a
//     lock screen hours later is worthless.
//   - **Rate-limited.** Two rules, both read out of this table rather than out of Redis — see
//     [Service.RequestOTP] for why the limit is durable rather than fast.
//   - **Stored hashed.** argon2id, which is the opposite of the email token's SHA-256 and for
//     the opposite reason: six digits is 10^6, and a work factor is the only thing that makes
//     a stolen table expensive to invert.
//
// # What this file deliberately does not disclose
//
// [Service.RequestOTP] answers the same way whether or not the number belongs to an account,
// and reports a fixed retry interval rather than the true remaining one. A 429 for a known
// number and a 202 for an unknown one would turn this endpoint into a way of asking "does this
// person have a Shipper account", answerable one number at a time by anybody. That is a real
// privacy exposure and the endpoint gains nothing from the distinction: the client's next step
// is the same either way.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

const (
	// otpDigits is the length of the code.
	//
	// Six is what people expect from every other service and what an SMS message can carry
	// legibly. The search space it gives — one in a million — is defended online by
	// otpMaxAttempts and offline by argon2id, and neither of those works without the other.
	otpDigits = 6

	// otpTTL is how long a code stays usable.
	//
	// Much shorter than the email token's day, because the delivery channel is much faster and
	// because a code visible on a lock screen has a wider audience than an email does.
	otpTTL = 10 * time.Minute

	// otpResendCooldown is the shortest interval between two codes for one account.
	//
	// It is also what the endpoint reports back, so the client can run a resend timer
	// (SHIP-54). A person who does not receive a message tries again within a few seconds;
	// this is what stops that costing a second message for every tap.
	otpResendCooldown = 60 * time.Second

	// otpWindow and otpMaxPerWindow bound how many messages one account can cause.
	//
	// SMS costs money per message and arrives on a real handset, so an unbounded resend is a
	// bill and a nuisance directed at whoever owns the number. Five in an hour is more than
	// anybody needs and far less than an abuser wants.
	otpWindow       = time.Hour
	otpMaxPerWindow = 5

	// otpMaxAttempts is how many wrong codes retire the live one.
	//
	// This is the online defence, and it is the one that matters: 10^6 is only expensive if
	// guessing costs something. Five is enough for somebody mistyping on a phone and nowhere
	// near enough to walk the space.
	otpMaxAttempts = 5
)

// The additional reason a code stops being live, beyond the two the email token shares.
const consumedExhausted = "exhausted"

// phoneOTP is one issued code, as the table holds it. The code itself is not a field: it exists
// in the message and nowhere else.
type phoneOTP struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	Phone    string
	CodeHash string

	ExpiresAt time.Time
	Attempts  int

	ConsumedAt     *time.Time
	ConsumedReason *string

	CreatedAt time.Time
}

func (o phoneOTP) expiredAt(now time.Time) bool { return !now.Before(o.ExpiresAt) }

// newOTPCode returns a fresh numeric code.
//
// crypto/rand rather than math/rand, and a rejection-free construction: one uniform draw over
// [0, 10^digits) formatted with leading zeros. Drawing digit by digit from a small range is the
// usual way this goes wrong — it invites a modulo bias when somebody optimises the draw later.
func newOTPCode() (string, error) {
	limit := new(big.Int).Exp(big.NewInt(10), big.NewInt(otpDigits), nil)

	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return "", fmt.Errorf("identity: drawing a one-time code: %w", err)
	}
	return fmt.Sprintf("%0*d", otpDigits, n), nil
}

// numericCode reports whether a submitted value is the shape a code is.
//
// Checked before the argon2id comparison so that a caller cannot make the service spend 64 MiB
// per request by posting arbitrary strings — which is a denial-of-service lever on an endpoint
// that needs no account.
func numericCode(code string) bool {
	if len(code) != otpDigits {
		return false
	}
	for i := 0; i < len(code); i++ {
		if code[i] < '0' || code[i] > '9' {
			return false
		}
	}
	return true
}

// RequestOTP sends a one-time code to the number, if the number belongs to an account.
//
// It returns how long the caller should wait before asking again, and **that value is a
// constant**. Reporting the true remaining cooldown would say "this number has been sent a code
// recently", which says "this number has an account".
//
// # Why the limits are read from PostgreSQL rather than from Redis
//
// SHIP-47 introduces a Redis token bucket for the whole authentication surface, and this is not
// a replacement for it. This limit is about how many *messages* one account causes, which is a
// bill and somebody's handset, and it must survive a Redis flush — the table already records
// every code that was sent, so the count is a query rather than a second source of truth.
//
// # Why the answer is always the same
//
// Unknown number, known number, throttled number, window exhausted: 202 and a fixed interval.
// The only observable difference is whether a message arrives, which the caller can only see if
// they hold the handset. That is the point.
func (s *Service) RequestOTP(ctx context.Context, submittedPhone string) (retryAfter time.Duration, err error) {
	phone := normalisePhone(submittedPhone)
	if err := validatePhoneField(phone); err != nil {
		// Refused rather than silently accepted. A malformed number is a client mistake
		// worth reporting, and reporting it discloses nothing about who has an account —
		// the answer is the same for every unusable value.
		return 0, err
	}

	if s.pool == nil {
		return 0, errUnavailable
	}

	var (
		user User
		code string
		send bool
	)
	if err := db.InTx(ctx, s.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		user, err = s.store.userByPhone(ctx, r, phone)
		if err != nil {
			if isNoRows(err) {
				// No account. Nothing is written and nothing is sent, and the caller is
				// told exactly what a real account would be told.
				return nil
			}
			return fmt.Errorf("identity: reading the account for an OTP: %w", err)
		}
		if user.PhoneVerified() {
			// Already verified. Sending another code would cost a message for no
			// outcome, and refusing would disclose the account's verification state.
			return nil
		}

		allowed, err := s.otpIssueAllowed(ctx, r, user.ID)
		if err != nil || !allowed {
			return err
		}

		code, err = s.issueOTP(ctx, r, user)
		if err != nil {
			return err
		}
		send = true
		return nil
	}); err != nil {
		return 0, err
	}

	if send {
		s.sendOTP(ctx, user, code)
	}
	return otpResendCooldown, nil
}

// otpIssueAllowed applies the two issue limits.
//
// Both are read inside the caller's transaction, so two requests racing cannot both see an empty
// window and both send. The row-level effect is the partial unique index: the second insert of a
// live code for one account is refused by the database whatever the count said.
func (s *Service) otpIssueAllowed(ctx context.Context, r db.Runner, userID uuid.UUID) (bool, error) {
	now := s.clock.Now().UTC()

	last, ok, err := s.store.lastOTPAt(ctx, r, userID)
	if err != nil {
		return false, err
	}
	if ok && now.Sub(last) < otpResendCooldown {
		return false, nil
	}

	sent, err := s.store.countOTPsSince(ctx, r, userID, now.Add(-otpWindow))
	if err != nil {
		return false, err
	}
	return sent < otpMaxPerWindow, nil
}

// issueOTP writes a fresh code for the account, retiring any outstanding one.
//
// Superseding first is what keeps uq_phone_otps_live satisfiable, and it is also what a person
// pressing "resend" expects: the code they were sent a minute ago stops working.
func (s *Service) issueOTP(ctx context.Context, r db.Runner, user User) (string, error) {
	if err := s.store.supersedeOTPs(ctx, r, user.ID); err != nil {
		return "", err
	}

	code, err := newOTPCode()
	if err != nil {
		return "", err
	}

	hash, err := s.hasher.Hash(code)
	if err != nil {
		return "", fmt.Errorf("identity: hashing a one-time code: %w", err)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("identity: generating a one-time code id: %w", err)
	}

	now := s.clock.Now().UTC()
	otp := phoneOTP{
		ID:        id,
		UserID:    user.ID,
		Phone:     user.Phone,
		CodeHash:  hash,
		ExpiresAt: now.Add(otpTTL),
		CreatedAt: now,
	}

	if err := s.store.insertOTP(ctx, r, otp); err != nil {
		return "", err
	}
	return code, nil
}

// sendOTP hands the message to the SMS adapter.
//
// Best-effort for the same reason the verification email is: the code is already committed, and
// reporting a send failure would tell the caller nothing they can act on differently — their
// remedy is to ask again, which is what they would have done anyway.
//
// The message is deliberately short. It is read on a lock screen, often while driving is about
// to start, and every word before the digits is a word between the person and the thing they
// need.
func (s *Service) sendOTP(ctx context.Context, user User, code string) {
	body := fmt.Sprintf("%s is your Shipper verification code. It expires in %d minutes.",
		code, int(otpTTL.Minutes()))

	if err := s.sms.Send(ctx, user.Phone, body); err != nil {
		// The number is not logged, for the reason the address is not: a log aggregator
		// holding every number a message failed to reach is a privacy exposure with no
		// operational benefit (Docs/05 §3).
		httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelError,
			"the verification code could not be sent",
			slog.String("user_id", user.ID.String()),
			slog.String("error", err.Error()))
	}
}

// VerifyPhone confirms a number against the code that was sent to it (SHIP-36).
//
// # Why every failure is one error
//
// Wrong code, expired code, no outstanding code, attempts exhausted, and a number with no
// account at all all return [ErrOTPInvalid]. The email token can afford to report expiry
// separately because only somebody holding a genuine token ever sees it; six digits is a
// guessable space, so every distinction here is information handed to whoever is guessing —
// including the one that matters most, which is whether the number has an account.
//
// # Why the wrong-guess path commits
//
// The attempt counter is the online defence, and an increment that rolled back with the error
// would count nothing: a caller could guess indefinitely and the column would stay at zero.
// So the transaction returns *nil* on a wrong guess, having recorded it, and the error is
// produced afterwards from the outcome it carried out. That is the one place in this package
// where a failure is deliberately not an error inside the transaction, and it is worth the
// awkwardness — the alternative is a limit that does not limit anything.
func (s *Service) VerifyPhone(ctx context.Context, submittedPhone, code string) (User, error) {
	phone := normalisePhone(submittedPhone)

	var v validate.Errors
	if v.Required("phone", phone) && !validE164(phone) {
		v.Add("phone", validate.CodeInvalid, "Enter an Australian mobile number, like 0412 345 678.")
	}
	v.Required("code", code)
	if err := v.Err(); err != nil {
		return User{}, err
	}

	// Checked before anything is hashed. argon2id at the production profile is 64 MiB per
	// call, and this endpoint needs no account — a caller posting arbitrary strings would
	// otherwise be able to choose how much memory the service allocates.
	if !numericCode(code) {
		return User{}, ErrOTPInvalid
	}

	if s.pool == nil {
		return User{}, errUnavailable
	}

	var (
		user     User
		verified bool
	)
	if err := db.InTx(ctx, s.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		user, err = s.store.userByPhone(ctx, r, phone)
		if err != nil {
			if isNoRows(err) {
				// No account. Indistinguishable from a wrong code, which is the point.
				return nil
			}
			return fmt.Errorf("identity: reading the account for a code: %w", err)
		}

		otp, err := s.store.liveOTP(ctx, r, user.ID)
		if err != nil {
			if isNoRows(err) {
				// Nothing outstanding: never asked for, already used, or exhausted.
				return nil
			}
			return fmt.Errorf("identity: reading the live one-time code: %w", err)
		}

		now := s.clock.Now().UTC()
		if otp.Phone != phone || otp.expiredAt(now) {
			// An expired code is left live rather than retired, so the next request has
			// something to supersede and the account is not silently left with none.
			return nil
		}

		matches, err := s.hasher.Verify(otp.CodeHash, code)
		if err != nil {
			// An unreadable hash is a data defect rather than a wrong code, and reporting
			// it as a wrong code would hide the defect behind an ordinary failure.
			return fmt.Errorf("identity: verifying a one-time code: %w", err)
		}

		if !matches {
			attempts, err := s.store.recordOTPAttempt(ctx, r, otp.ID)
			if err != nil {
				return err
			}
			if attempts >= otpMaxAttempts {
				// Retired rather than left to be guessed at. 'exhausted' is a separate
				// reason from 'superseded' because it is a support conversation and
				// possibly an attack, and the two should not look alike in the table.
				if _, err := s.store.consumeOTP(ctx, r, otp.ID, now, consumedExhausted); err != nil {
					return err
				}
			}
			return nil
		}

		if _, err := s.store.consumeOTP(ctx, r, otp.ID, now, consumedVerified); err != nil {
			return err
		}
		if _, err := s.store.markPhoneVerified(ctx, r, user.ID, now); err != nil {
			return err
		}

		user, err = s.store.userByID(ctx, r, user.ID)
		if err != nil {
			return err
		}
		verified = true
		return nil
	}); err != nil {
		return User{}, err
	}

	if !verified {
		return User{}, ErrOTPInvalid
	}
	return user, nil
}
