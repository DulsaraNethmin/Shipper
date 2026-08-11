// Persistence for identity — concrete, unexported, and not behind an interface.
//
// Docs/10 §2.2: two layers, not three. There is no repository interface, because Docs/06 §4.1
// is explicit that PostgreSQL is not abstracted in this service — the constraints are
// load-bearing, and a mock happily accepts the write a unique index exists to reject. Every
// method takes a db.Runner as its first argument after ctx, so the same SQL runs inside a
// transaction or straight against the pool, decided by the caller (Docs/10 §3.2).
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// postgresStore is the SQL. It holds no state, so the zero value is usable and a Service can
// embed it by value.
type postgresStore struct{}

// userColumns is the projection every read of a user shares.
//
// One constant rather than repeated column lists, so that adding a column to the struct fails
// to compile in one place rather than silently returning a zero value from three query sites.
// password_hash is deliberately absent — see the note on User.
const userColumns = `id, email, phone, role, status, email_verified_at, phone_verified_at, created_at`

// insertUser writes a new account and returns it as stored.
//
// RETURNING rather than a second SELECT: created_at comes from the database default, and a
// round trip to read back what was just written is a window in which it could have changed.
func (postgresStore) insertUser(ctx context.Context, r db.Runner, u User, passwordHash string) (User, error) {
	row := r.QueryRow(ctx, `
		INSERT INTO users (id, email, phone, password_hash, role, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING `+userColumns,
		u.ID, u.Email, u.Phone, passwordHash, u.Role, u.Status)

	created, err := scanUser(row)
	if err != nil {
		return User{}, fmt.Errorf("identity: inserting a user: %w", asUniqueViolation(err))
	}
	return created, nil
}

// userByEmail reads an account by its address.
//
// The column is citext, so this matches whatever case the caller supplied without a lower() on
// either side — which matters, because a function on the column would make uq_users_email
// unusable for the lookup.
func (postgresStore) userByEmail(ctx context.Context, r db.Runner, email string) (User, error) {
	row := r.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE email = $1`, email)
	return scanUser(row)
}

// userByID reads an account by its identifier.
func (postgresStore) userByID(ctx context.Context, r db.Runner, id uuid.UUID) (User, error) {
	row := r.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	return scanUser(row)
}

// userByPhone reads an account by its number, which is stored already normalised to E.164.
func (postgresStore) userByPhone(ctx context.Context, r db.Runner, phone string) (User, error) {
	row := r.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE phone = $1`, phone)
	return scanUser(row)
}

// scanUser reads one row in the order userColumns declares.
//
// Hand-scanned rather than pgx.RowToStructByName, because the struct's field names are the
// domain's and the column names are the schema's, and they legitimately differ (ID / id,
// EmailVerifiedAt / email_verified_at). Mapping by name would need tags on every field, which
// is the same coupling written twice.
func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	if err := row.Scan(
		&u.ID, &u.Email, &u.Phone, &u.Role, &u.Status,
		&u.EmailVerifiedAt, &u.PhoneVerifiedAt, &u.CreatedAt,
	); err != nil {
		return User{}, err
	}
	return u, nil
}

// markEmailVerified records the instant the address was confirmed (SHIP-33).
//
// It is a no-op on an account that is already verified: the WHERE clause leaves the original
// timestamp alone, so a second confirmation cannot move "verified since" forward. The bool
// reports whether this call was the one that changed it, which is what distinguishes a first
// confirmation from a repeat for the caller's own logging.
func (postgresStore) markEmailVerified(ctx context.Context, r db.Runner, userID uuid.UUID, at time.Time) (bool, error) {
	tag, err := r.Exec(ctx, `
		UPDATE users SET email_verified_at = $2
		WHERE id = $1 AND email_verified_at IS NULL`, userID, at)
	if err != nil {
		return false, fmt.Errorf("identity: marking the email verified: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// markPhoneVerified records the instant the number was confirmed (SHIP-36).
func (postgresStore) markPhoneVerified(ctx context.Context, r db.Runner, userID uuid.UUID, at time.Time) (bool, error) {
	tag, err := r.Exec(ctx, `
		UPDATE users SET phone_verified_at = $2
		WHERE id = $1 AND phone_verified_at IS NULL`, userID, at)
	if err != nil {
		return false, fmt.Errorf("identity: marking the phone verified: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// emailTokenColumns is the projection every read of a verification token shares.
const emailTokenColumns = `id, user_id, email, token_hash, expires_at, consumed_at, consumed_reason, created_at`

// insertEmailToken writes a freshly issued verification token (SHIP-31).
func (postgresStore) insertEmailToken(ctx context.Context, r db.Runner, t emailVerificationToken) error {
	// created_at is written rather than defaulted, from the same clock that computed
	// expires_at. The column has a DEFAULT now() as a safety net for any other writer, but a
	// row whose two timestamps come from two different clocks is a row that can violate
	// ck_email_verification_tokens_expiry under ordinary skew between the service and the
	// database — and the rate limits below count over created_at, so they have to agree with
	// the clock the service reasons about.
	_, err := r.Exec(ctx, `
		INSERT INTO email_verification_tokens (id, user_id, email, token_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		t.ID, t.UserID, t.Email, t.TokenHash, t.ExpiresAt, t.CreatedAt)
	if err != nil {
		return fmt.Errorf("identity: inserting a verification token: %w", err)
	}
	return nil
}

// supersedeEmailTokens retires whatever the account currently holds, so that a newly issued
// token is the only live one.
//
// This is what keeps uq_email_verification_tokens_live satisfiable — and, more to the point,
// what stops a link left in an old mailbox working after a replacement has been asked for.
func (postgresStore) supersedeEmailTokens(ctx context.Context, r db.Runner, userID uuid.UUID) error {
	_, err := r.Exec(ctx, `
		UPDATE email_verification_tokens
		   SET consumed_at = now(), consumed_reason = $2
		 WHERE user_id = $1 AND consumed_at IS NULL`, userID, consumedSuperseded)
	if err != nil {
		return fmt.Errorf("identity: superseding outstanding verification tokens: %w", err)
	}
	return nil
}

// emailTokenByHash reads a token whether or not it is still live.
//
// Consumed and expired rows are returned rather than filtered out, because the caller answers
// differently for each: an expired token means "ask for another", a consumed one on an already
// verified account means the person clicked the link twice, and neither is the same as a token
// this platform never issued.
func (postgresStore) emailTokenByHash(ctx context.Context, r db.Runner, hash string) (emailVerificationToken, error) {
	row := r.QueryRow(ctx,
		`SELECT `+emailTokenColumns+` FROM email_verification_tokens WHERE token_hash = $1`, hash)

	var t emailVerificationToken
	if err := row.Scan(&t.ID, &t.UserID, &t.Email, &t.TokenHash, &t.ExpiresAt,
		&t.ConsumedAt, &t.ConsumedReason, &t.CreatedAt); err != nil {
		return emailVerificationToken{}, err
	}
	return t, nil
}

// consumeEmailToken marks a token used, reporting whether this call was the one that did it.
//
// The guard is in the WHERE clause rather than in a check the caller made a moment ago, which is
// what makes "single use" hold when two confirmations arrive at once: PostgreSQL decides, and
// the loser is told the token is no longer valid rather than both being told they verified it.
func (postgresStore) consumeEmailToken(ctx context.Context, r db.Runner, id uuid.UUID, at time.Time) (bool, error) {
	tag, err := r.Exec(ctx, `
		UPDATE email_verification_tokens
		   SET consumed_at = $2, consumed_reason = $3
		 WHERE id = $1 AND consumed_at IS NULL`, id, at, consumedVerified)
	if err != nil {
		return false, fmt.Errorf("identity: consuming a verification token: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// countEmailTokensSince is what the resend limit counts over. Superseded rows are included
// deliberately: the limit is on how many messages were sent, and every one of them was.
func (postgresStore) countEmailTokensSince(ctx context.Context, r db.Runner, userID uuid.UUID, since time.Time) (int, error) {
	var n int
	if err := r.QueryRow(ctx, `
		SELECT count(*) FROM email_verification_tokens
		 WHERE user_id = $1 AND created_at >= $2`, userID, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("identity: counting recent verification tokens: %w", err)
	}
	return n, nil
}

// lastEmailTokenAt is when the account was last sent one, for the resend cooldown.
func (postgresStore) lastEmailTokenAt(ctx context.Context, r db.Runner, userID uuid.UUID) (time.Time, bool, error) {
	var at *time.Time
	if err := r.QueryRow(ctx, `
		SELECT max(created_at) FROM email_verification_tokens WHERE user_id = $1`,
		userID).Scan(&at); err != nil {
		return time.Time{}, false, fmt.Errorf("identity: reading the last verification token: %w", err)
	}
	if at == nil {
		return time.Time{}, false, nil
	}
	return *at, true, nil
}

// otpColumns is the projection every read of a one-time code shares.
const otpColumns = `id, user_id, phone, code_hash, expires_at, attempts, consumed_at, consumed_reason, created_at`

// insertOTP writes a freshly issued one-time code (SHIP-34).
func (postgresStore) insertOTP(ctx context.Context, r db.Runner, o phoneOTP) error {
	// created_at from the injected clock, for the reason insertEmailToken gives.
	_, err := r.Exec(ctx, `
		INSERT INTO phone_otps (id, user_id, phone, code_hash, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		o.ID, o.UserID, o.Phone, o.CodeHash, o.ExpiresAt, o.CreatedAt)
	if err != nil {
		return fmt.Errorf("identity: inserting a one-time code: %w", err)
	}
	return nil
}

// supersedeOTPs retires whatever the account currently holds, so a newly issued code is the only
// live one — which is what a person pressing "resend" expects.
func (postgresStore) supersedeOTPs(ctx context.Context, r db.Runner, userID uuid.UUID) error {
	_, err := r.Exec(ctx, `
		UPDATE phone_otps
		   SET consumed_at = now(), consumed_reason = $2
		 WHERE user_id = $1 AND consumed_at IS NULL`, userID, consumedSuperseded)
	if err != nil {
		return fmt.Errorf("identity: superseding outstanding one-time codes: %w", err)
	}
	return nil
}

// liveOTP reads the account's outstanding code, locking the row for the duration of the
// transaction.
//
// FOR UPDATE is what makes the attempt counter an actual limit. Without it two guesses arriving
// together both read attempts = 4, both write 5, and the account has had six tries — repeatable
// on purpose by anybody who wants more than five.
func (postgresStore) liveOTP(ctx context.Context, r db.Runner, userID uuid.UUID) (phoneOTP, error) {
	row := r.QueryRow(ctx, `
		SELECT `+otpColumns+`
		  FROM phone_otps
		 WHERE user_id = $1 AND consumed_at IS NULL
		 FOR UPDATE`, userID)

	var o phoneOTP
	if err := row.Scan(&o.ID, &o.UserID, &o.Phone, &o.CodeHash, &o.ExpiresAt, &o.Attempts,
		&o.ConsumedAt, &o.ConsumedReason, &o.CreatedAt); err != nil {
		return phoneOTP{}, err
	}
	return o, nil
}

// recordOTPAttempt counts one wrong guess and returns the new total.
func (postgresStore) recordOTPAttempt(ctx context.Context, r db.Runner, id uuid.UUID) (int, error) {
	var attempts int
	if err := r.QueryRow(ctx, `
		UPDATE phone_otps SET attempts = attempts + 1
		 WHERE id = $1
		 RETURNING attempts`, id).Scan(&attempts); err != nil {
		return 0, fmt.Errorf("identity: recording a one-time code attempt: %w", err)
	}
	return attempts, nil
}

// consumeOTP retires a code, reporting whether this call was the one that did it.
//
// The reason is a parameter rather than a constant because a code leaves the live state three
// ways, and a support conversation needs to tell "it worked" from "somebody guessed at it five
// times" (ck_phone_otps_reason).
func (postgresStore) consumeOTP(ctx context.Context, r db.Runner, id uuid.UUID, at time.Time, reason string) (bool, error) {
	tag, err := r.Exec(ctx, `
		UPDATE phone_otps SET consumed_at = $2, consumed_reason = $3
		 WHERE id = $1 AND consumed_at IS NULL`, id, at, reason)
	if err != nil {
		return false, fmt.Errorf("identity: consuming a one-time code: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// countOTPsSince is what the window limit counts over. Superseded and exhausted rows are
// included: the limit is on messages sent, and every one of them was sent.
func (postgresStore) countOTPsSince(ctx context.Context, r db.Runner, userID uuid.UUID, since time.Time) (int, error) {
	var n int
	if err := r.QueryRow(ctx, `
		SELECT count(*) FROM phone_otps WHERE user_id = $1 AND created_at >= $2`,
		userID, since).Scan(&n); err != nil {
		return 0, fmt.Errorf("identity: counting recent one-time codes: %w", err)
	}
	return n, nil
}

// lastOTPAt is when the account was last sent a code, for the resend cooldown.
func (postgresStore) lastOTPAt(ctx context.Context, r db.Runner, userID uuid.UUID) (time.Time, bool, error) {
	var at *time.Time
	if err := r.QueryRow(ctx,
		`SELECT max(created_at) FROM phone_otps WHERE user_id = $1`, userID).Scan(&at); err != nil {
		return time.Time{}, false, fmt.Errorf("identity: reading the last one-time code: %w", err)
	}
	if at == nil {
		return time.Time{}, false, nil
	}
	return *at, true, nil
}

// isNoRows reports whether a read found nothing, which is an outcome rather than a failure at
// every call site in this package.
func isNoRows(err error) bool { return errors.Is(err, db.ErrNoRows) }
