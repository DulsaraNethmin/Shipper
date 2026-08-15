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
//
// `coalesce(name, ”)` is the projection's only expression, and it is deliberate rather than
// convenient. `users.name` is nullable because accounts predating `000006` have none and a name
// cannot be invented for them, while [User.Name] is a plain string; coalescing here means "no name"
// arrives as the empty string at every call site rather than as a NULL each of them scans into a
// pointer and dereferences. Exactly one statement writes the column and it never writes `”`.
const userColumns = `id, coalesce(name, '') AS name, email, phone, role, status, ` +
	`email_verified_at, phone_verified_at, created_at`

// insertUser writes a new account and returns it as stored.
//
// RETURNING rather than a second SELECT: created_at comes from the database default, and a
// round trip to read back what was just written is a window in which it could have changed.
func (postgresStore) insertUser(ctx context.Context, r db.Runner, u User, passwordHash string) (User, error) {
	row := r.QueryRow(ctx, `
		INSERT INTO users (id, name, email, phone, password_hash, role, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING `+userColumns,
		u.ID, u.Name, u.Email, u.Phone, passwordHash, u.Role, u.Status)

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

// credentialByEmail reads an account together with the stored password hash (SHIP-41).
//
// It is a separate method from userByEmail rather than a flag on it, and the name says what it
// returns, because the hash is the one thing User deliberately does not carry: nothing outside
// this package has a use for it, and a struct that holds it is a struct that eventually gets
// logged or serialised. Sign-in is the only caller, and having to name a different method is what
// makes reaching for the credential visible in review.
//
// The hash is returned as a bare string rather than inside User for the same reason. A local
// variable goes out of scope; a field travels.
func (postgresStore) credentialByEmail(ctx context.Context, r db.Runner, email string) (User, string, error) {
	row := r.QueryRow(ctx,
		`SELECT `+userColumns+`, password_hash FROM users WHERE email = $1`, email)

	var (
		u    User
		hash string
	)
	if err := row.Scan(
		&u.ID, &u.Name, &u.Email, &u.Phone, &u.Role, &u.Status,
		&u.EmailVerifiedAt, &u.PhoneVerifiedAt, &u.CreatedAt, &hash,
	); err != nil {
		return User{}, "", err
	}
	return u, hash, nil
}

// updatePasswordHash replaces the stored credential with one written at the current profile
// (SHIP-29, SHIP-41).
//
// This is how "the costs travel with the hash, so raising the profile needs no migration" stops
// being a property of the format and becomes something that actually happens: sign-in is the only
// moment the plaintext exists, so it is the only moment a stronger hash can be written without
// asking anybody to reset anything.
//
// RowsAffected is not checked. The row was read in this transaction and users are never deleted
// (Docs/05 §3.1 pseudonymises instead), so zero rows is not a case that arises — and failing a
// sign-in whose password verified, because a background upgrade found nothing to upgrade, would
// be the wrong trade.
func (postgresStore) updatePasswordHash(ctx context.Context, r db.Runner, userID uuid.UUID, hash string) error {
	if _, err := r.Exec(ctx,
		`UPDATE users SET password_hash = $2 WHERE id = $1`, userID, hash); err != nil {
		return fmt.Errorf("identity: upgrading a stored password hash: %w", err)
	}
	return nil
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
		&u.ID, &u.Name, &u.Email, &u.Phone, &u.Role, &u.Status,
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

// deviceSessionColumns is the projection every read of a device session shares (SHIP-39).
const deviceSessionColumns = `id, user_id, refresh_token_hash, refresh_token_expires_at, ` +
	`device_label, last_seen_at, revoked_at, revoked_reason, created_at`

// insertDeviceSession writes a newly created session (SHIP-39).
//
// created_at and last_seen_at are written rather than defaulted, from the same clock that
// computed refresh_token_expires_at. The columns carry DEFAULT now() as a safety net for any
// other writer, but a row whose timestamps come from two clocks can violate
// ck_device_sessions_refresh_expiry under ordinary skew between the service and the database —
// the same reasoning insertEmailToken gives.
func (postgresStore) insertDeviceSession(ctx context.Context, r db.Runner, s deviceSession) error {
	_, err := r.Exec(ctx, `
		INSERT INTO device_sessions
		    (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label,
		     last_seen_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`,
		s.ID, s.UserID, s.RefreshTokenHash, s.RefreshTokenExpiresAt, s.DeviceLabel,
		s.LastSeenAt, s.CreatedAt)
	if err != nil {
		return fmt.Errorf("identity: inserting a device session: %w", err)
	}
	return nil
}

// deviceSessionByRefreshHash reads the session a refresh token belongs to, locking it for the
// duration of the transaction (SHIP-39).
//
// FOR UPDATE is what makes rotation single-winner. Two refreshes presenting the same token
// arrive together — a phone retrying over a flaky connection — and without the lock both read
// the same row, both write a new hash, and the device ends up holding a token the session has
// already replaced. With it, the second one blocks and then re-evaluates the predicate against
// the committed row, finds the hash gone, and takes the not-live path instead.
func (postgresStore) deviceSessionByRefreshHash(ctx context.Context, r db.Runner, hash string) (deviceSession, error) {
	row := r.QueryRow(ctx,
		`SELECT `+deviceSessionColumns+`
		   FROM device_sessions
		  WHERE refresh_token_hash = $1
		  FOR UPDATE`, hash)
	return scanDeviceSession(row)
}

// rotateDeviceSession replaces the session's refresh token with the next one (SHIP-39).
//
// The previous hash is in the WHERE clause rather than trusted from a read that happened a
// moment ago, and RowsAffected is checked. Callers today hold the row lock from
// deviceSessionByRefreshHash, so this cannot fail — which is exactly why it is worth having: a
// later caller that forgets the lock gets an error rather than a lost update.
func (postgresStore) rotateDeviceSession(ctx context.Context, r db.Runner, s deviceSession, previousHash string) error {
	tag, err := r.Exec(ctx, `
		UPDATE device_sessions
		   SET refresh_token_hash = $2, refresh_token_expires_at = $3, last_seen_at = $4
		 WHERE id = $1 AND refresh_token_hash = $5 AND revoked_at IS NULL`,
		s.ID, s.RefreshTokenHash, s.RefreshTokenExpiresAt, s.LastSeenAt, previousHash)
	if err != nil {
		return fmt.Errorf("identity: rotating a device session: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("identity: rotating device session %s affected %d rows, want 1",
			s.ID, tag.RowsAffected())
	}
	return nil
}

// revokeDeviceSession ends a session, reporting whether this call was the one that ended it
// (SHIP-40).
//
// Guarded on revoked_at IS NULL rather than on a check the caller made a moment ago, so a token
// replayed a third time does not rewrite the reason or move the instant the session ended — and
// so two presentations arriving together produce one revocation, decided by PostgreSQL.
func (postgresStore) revokeDeviceSession(ctx context.Context, r db.Runner, id uuid.UUID, at time.Time, reason string) (bool, error) {
	tag, err := r.Exec(ctx, `
		UPDATE device_sessions
		   SET revoked_at = $2, revoked_reason = $3
		 WHERE id = $1 AND revoked_at IS NULL`, id, at, reason)
	if err != nil {
		return false, fmt.Errorf("identity: revoking a device session: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// revokeOwnDeviceSession ends a session that belongs to a named account (SHIP-43, SHIP-46).
//
// The owner is in the WHERE clause rather than checked by the caller a moment earlier, and that
// is the whole authorisation decision for both endpoints made where it cannot be skipped: a
// caller who names somebody else's session identifier updates nothing, whatever the handler above
// believed. Docs/07 §3 puts the decision on the platform, and a decision expressed as a predicate
// on the write is one no later refactor can leave out.
//
// It is a second method rather than a parameter on revokeDeviceSession because SHIP-40's caller
// genuinely has no user to scope by — it revokes on the strength of a token, having just learnt
// which session that token belonged to.
//
// Guarded on revoked_at IS NULL for the reason revokeDeviceSession gives, so the bool means "this
// call ended it" rather than "it is ended".
func (postgresStore) revokeOwnDeviceSession(ctx context.Context, r db.Runner, id, userID uuid.UUID, at time.Time, reason string) (bool, error) {
	tag, err := r.Exec(ctx, `
		UPDATE device_sessions
		   SET revoked_at = $3, revoked_reason = $4
		 WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL`, id, userID, at, reason)
	if err != nil {
		return false, fmt.Errorf("identity: revoking a device session: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

// deviceSessionOwnedBy reports whether an identifier names a session on this account (SHIP-46).
//
// It exists because revokeOwnDeviceSession's row count cannot answer the question the revoke
// endpoint has to: zero rows means "not yours", "no such session", or "already revoked", and the
// last is a success while the first two are a 404. Read and update run in one transaction, so the
// pair cannot disagree.
func (postgresStore) deviceSessionOwnedBy(ctx context.Context, r db.Runner, id, userID uuid.UUID) (bool, error) {
	var exists bool
	err := r.QueryRow(ctx,
		`SELECT true FROM device_sessions WHERE id = $1 AND user_id = $2`, id, userID).Scan(&exists)
	switch {
	case isNoRows(err):
		return false, nil
	case err != nil:
		return false, fmt.Errorf("identity: reading a device session: %w", err)
	default:
		return exists, nil
	}
}

// liveDeviceSessionsByUser reads the sessions that can currently act as an account, newest use
// first (SHIP-46).
//
// **Revoked and lapsed sessions are excluded, and that is the endpoint's whole meaning.** The
// list answers "which devices can act as me, and let me stop one"; a session that has been
// revoked or whose refresh token has expired can do neither, and offering it invites revoking
// something already dead. The rows are still there — Docs/10 §3.3 ends a session by marking it,
// never by deleting it — and remain readable for support and for SHIP-149's audit.
//
// limit is applied by the caller as a bound on the *response*, not as paging: see the note on
// [Service.Devices].
func (postgresStore) liveDeviceSessionsByUser(ctx context.Context, r db.Runner, userID uuid.UUID, now time.Time, limit int) ([]deviceSession, error) {
	rows, err := r.Query(ctx, `
		SELECT `+deviceSessionColumns+`
		  FROM device_sessions
		 WHERE user_id = $1
		   AND revoked_at IS NULL
		   AND refresh_token_expires_at > $2
		 ORDER BY last_seen_at DESC, id DESC
		 LIMIT $3`, userID, now, limit)
	if err != nil {
		return nil, fmt.Errorf("identity: listing device sessions: %w", err)
	}
	defer rows.Close()

	var sessions []deviceSession
	for rows.Next() {
		s, err := scanDeviceSession(rows)
		if err != nil {
			return nil, fmt.Errorf("identity: reading a device session: %w", err)
		}
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("identity: listing device sessions: %w", err)
	}
	return sessions, nil
}

// insertConsumedRefreshToken records a refresh token that has been rotated away (SHIP-40).
//
// consumed_at comes from the injected clock rather than from a database default, for the reason
// insertEmailToken gives: it is the instant the service reasons about, and the pruning sweep
// 000104 anticipates will count over it.
func (postgresStore) insertConsumedRefreshToken(ctx context.Context, r db.Runner, sessionID uuid.UUID, hash string, at time.Time) error {
	id, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("identity: generating a consumed refresh token id: %w", err)
	}

	if _, err := r.Exec(ctx, `
		INSERT INTO consumed_refresh_tokens (id, session_id, token_hash, consumed_at)
		VALUES ($1, $2, $3, $4)`, id, sessionID, hash, at); err != nil {
		return fmt.Errorf("identity: recording a consumed refresh token: %w", err)
	}
	return nil
}

// consumedRefreshTokenSession reads which session a spent refresh token belonged to (SHIP-40).
//
// This is the whole of reuse detection's lookup: no rows means nobody's token, a row means one
// this platform issued and has already rotated away, and the session it names is what gets
// revoked. uq_consumed_refresh_tokens_hash is what makes that answer unambiguous.
func (postgresStore) consumedRefreshTokenSession(ctx context.Context, r db.Runner, hash string) (uuid.UUID, error) {
	var sessionID uuid.UUID
	if err := r.QueryRow(ctx,
		`SELECT session_id FROM consumed_refresh_tokens WHERE token_hash = $1`,
		hash).Scan(&sessionID); err != nil {
		return uuid.Nil, err
	}
	return sessionID, nil
}

// scanDeviceSession reads one row in the order deviceSessionColumns declares.
func scanDeviceSession(row interface{ Scan(...any) error }) (deviceSession, error) {
	var s deviceSession
	if err := row.Scan(
		&s.ID, &s.UserID, &s.RefreshTokenHash, &s.RefreshTokenExpiresAt,
		&s.DeviceLabel, &s.LastSeenAt, &s.RevokedAt, &s.RevokedReason, &s.CreatedAt,
	); err != nil {
		return deviceSession{}, err
	}
	return s, nil
}

// isNoRows reports whether a read found nothing, which is an outcome rather than a failure at
// every call site in this package.
func isNoRows(err error) bool { return errors.Is(err, db.ErrNoRows) }
