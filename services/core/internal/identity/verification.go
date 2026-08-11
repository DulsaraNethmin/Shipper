// SHIP-31: email verification tokens — generation, storage and dispatch.
//
// Docs/04 §2 requires a customer's email verified before they may publish, and the token is how
// that is established: a value only somebody reading the address can produce, presented back to
// the platform.
//
// # The three properties the ticket names
//
//   - **Single-use.** Consuming one is an UPDATE guarded on `consumed_at IS NULL`, so two
//     confirmations racing produce one winner decided by PostgreSQL rather than by an
//     application check that read a moment earlier.
//   - **Expiring.** Twenty-four hours, checked against the injected clock rather than by the
//     database, so the boundary is testable without waiting for it.
//   - **Stored on registration.** Written in the same transaction as the account. A registration
//     that committed a user and then failed to write the token would leave an account nobody can
//     verify and no record of why.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

const (
	// emailVerificationTokenTTL is how long a verification link stays usable.
	//
	// A day rather than an hour: the message can sit in a spam folder or on a phone that is
	// out of coverage, and a person who signs up in the evening and reads their email in the
	// morning should not have to ask for a second one. The cost of the longer window is
	// bounded by the token being single-use and by there being at most one live at a time.
	emailVerificationTokenTTL = 24 * time.Hour

	// verificationTokenBytes is the entropy in a token.
	//
	// Thirty-two bytes from crypto/rand, base64url encoded to 43 characters. That is what
	// makes SHA-256 the right way to store it: guessing is 2^256 and there is nothing for a
	// work factor to protect against.
	verificationTokenBytes = 32
)

// The reasons a token stops being live. They are exactly the values
// ck_email_verification_tokens_reason permits, so the Go constants and the database constraint
// cannot drift into disagreeing (Docs/10 §3.4).
const (
	consumedVerified   = "verified"
	consumedSuperseded = "superseded"
)

// emailVerificationToken is one issued token, as the table holds it. The raw value is never a
// field: it exists in the message that carried it and nowhere else.
type emailVerificationToken struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Email     string
	TokenHash string
	ExpiresAt time.Time

	ConsumedAt     *time.Time
	ConsumedReason *string

	CreatedAt time.Time
}

func (t emailVerificationToken) consumed() bool { return t.ConsumedAt != nil }

func (t emailVerificationToken) expiredAt(now time.Time) bool { return !now.Before(t.ExpiresAt) }

// newVerificationToken produces a token and the hash to store against it.
//
// The two are produced together, by one function, because the property that matters is that the
// stored value is derived from the sent one. Splitting them is how a refactor ends up storing
// the token itself.
func newVerificationToken() (raw, hash string, err error) {
	buf := make([]byte, verificationTokenBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", "", fmt.Errorf("identity: reading verification token entropy: %w", err)
	}

	raw = base64.RawURLEncoding.EncodeToString(buf)
	return raw, hashVerificationToken(raw), nil
}

// hashVerificationToken is the one-way function the table stores.
//
// SHA-256 rather than argon2id, deliberately, and the reasoning is the reverse of the password
// case. A work factor exists to make a *small* search space expensive; this space has 2^256
// elements. Spending 64 MiB per lookup would buy nothing and would make the confirm endpoint a
// denial-of-service lever anybody can pull without an account.
func hashVerificationToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// issueEmailVerification writes a fresh token for the account, retiring any outstanding one.
//
// It takes a db.Runner rather than opening its own transaction, so registration can write the
// account and the token together and a resend can call it on its own. Docs/10 §3.2: a method
// that needs a transaction takes a Runner and trusts its caller.
//
// Superseding first is what keeps uq_email_verification_tokens_live satisfiable. It is also the
// behaviour that matters: without it a link left in an old mailbox stays usable for its full
// day after the person has asked for a new one.
func (s *Service) issueEmailVerification(ctx context.Context, r db.Runner, user User) (raw string, err error) {
	if err := s.store.supersedeEmailTokens(ctx, r, user.ID); err != nil {
		return "", err
	}

	raw, hash, err := newVerificationToken()
	if err != nil {
		return "", err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("identity: generating a verification token id: %w", err)
	}

	now := s.clock.Now().UTC()
	token := emailVerificationToken{
		ID:        id,
		UserID:    user.ID,
		Email:     user.Email,
		TokenHash: hash,
		ExpiresAt: now.Add(emailVerificationTokenTTL),
	}

	if err := s.store.insertEmailToken(ctx, r, token); err != nil {
		return "", err
	}
	return raw, nil
}

// sendVerificationEmail hands the message to the adapter.
//
// # Why a failure here does not fail the request that caused it
//
// The account is already committed. Reporting failure would leave the caller with an account
// they were told was not created, and their retry would answer `identity_email_taken` — a dead
// end reachable only by support. So the send is best-effort, the failure is logged at error
// level with the account attached, and the caller has `POST /v1/auth/resend-verify` for exactly
// this case.
//
// The token is passed as a value rather than a link. The deep-link scheme is SHIP-53's decision
// and does not exist yet, and inventing a URL here would put a hostname nobody has registered
// into a message that reaches real people. Docs/07 §6's rule applies: anything that has to
// change under operational pressure lives server-side, and a link is worth building once its
// shape is settled rather than twice.
func (s *Service) sendVerificationEmail(ctx context.Context, user User, raw string) {
	const subject = "Confirm your email address"

	body := fmt.Sprintf(`Hello,

Confirm this email address to finish setting up your Shipper account.

Your verification code is:

  %s

It expires in %d hours. If you did not create a Shipper account, you can ignore
this message and nothing further will happen.

— Shipper`, raw, int(emailVerificationTokenTTL.Hours()))

	if err := s.email.Send(ctx, user.Email, subject, body); err != nil {
		// The address is not logged. The account id finds it, and a log aggregator holding
		// every address that ever failed to receive mail is a privacy exposure with no
		// operational benefit (Docs/05 §3).
		httpx.LoggerFrom(ctx).LogAttrs(ctx, slog.LevelError,
			"the verification email could not be sent",
			slog.String("user_id", user.ID.String()),
			slog.String("error", err.Error()))
	}
}
