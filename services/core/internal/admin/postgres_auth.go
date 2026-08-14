// SHIP-147: the SQL behind administrator accounts and their sessions.
//
// Methods on the same unexported [postgresStore] as postgres.go, in a file of their own because
// they are a different subject rather than a different layer — Docs/10 §2.2 has one concrete store
// per domain and no repository interface, and splitting the *type* would be two answers to "where
// does this domain's SQL live".
//
// # Every statement here is keyed, and none of them scans
//
// A credential lookup by digest, an account lookup by address, a session read by primary key. That
// is deliberate: the read path of an authentication system is on every administrative request, and
// a query whose cost grows with the number of sessions ever created is a denial of service against
// the console with the most history.
//
// The blank line below keeps this a file note rather than a second package comment.

package admin

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// administratorColumns is every column of an administrator, in the order [scanAdministrator] reads
// them — and deliberately **without `password_hash`**.
//
// The hash is selected explicitly by the one statement that needs it, so that a new caller reaching
// for "the administrator columns" cannot accidentally pull a credential into a struct that is
// logged, serialised or returned. [Administrator] has nowhere to put it either; the two facts hold
// each other up.
const administratorColumns = `id, email, name, role, status, created_at, updated_at`

// credentialByEmail reads the account and its stored hash for a sign-in.
//
// The address is compared by the column's own type: `admin_users.email` is citext, so
// `Alice@example.com` and `alice@example.com` are the same account without this statement saying
// so. [SignInCommand.Normalise] lower-cases anyway, because a value should be judged in the shape it
// will be stored in.
//
// Returns [db.ErrNoRows] when there is no such administrator, which the caller turns into the same
// refusal a wrong password gets — after spending the equivalent argon2id work.
func (postgresStore) credentialByEmail(
	ctx context.Context,
	r db.Runner,
	email string,
) (Administrator, string, error) {
	const q = `SELECT ` + administratorColumns + `, password_hash FROM admin_users WHERE email = $1`

	var (
		a    Administrator
		hash string
	)
	err := r.QueryRow(ctx, q, email).Scan(
		&a.ID, &a.Email, &a.Name, &a.Role, &a.Status, &a.CreatedAt, &a.UpdatedAt, &hash)
	if err != nil {
		return Administrator{}, "", err
	}
	return a, hash, nil
}

// updatePasswordHash replaces a stored credential.
//
// One column. `updated_at` moves by trigger (000801), which is why it is not in the SET list — two
// places writing it is two answers to when the row last changed.
func (postgresStore) updatePasswordHash(ctx context.Context, r db.Runner, id uuid.UUID, hash string) error {
	const q = `UPDATE admin_users SET password_hash = $2 WHERE id = $1`

	tag, err := r.Exec(ctx, q, id, hash)
	if err != nil {
		return fmt.Errorf("admin: updating an administrator password hash: %w", err)
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("admin: updating the password hash for %s affected %d rows",
			id, tag.RowsAffected())
	}
	return nil
}

// insertAdministrator creates an account, or reports that the address already has one.
//
// The duplicate is caught by name rather than by a prior SELECT: two concurrent creations both find
// nothing and both insert, and only `uq_admin_users_email` is right about that. Naming the
// constraint keeps the check specific — a unique violation on something else is a different defect
// and must not be reported as "that address is taken".
//
// The row is read back with RETURNING rather than assembled in Go, so that the defaults the column
// definitions carry — including `role`'s, which is the least-privileged one — are what the caller
// is told about. A struct built optimistically would report the role this code *believes* was
// applied.
func (postgresStore) insertAdministrator(
	ctx context.Context,
	r db.Runner,
	a Administrator,
	hash string,
) (Administrator, error) {
	const q = `
		INSERT INTO admin_users (id, email, name, password_hash, role, status)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING ` + administratorColumns

	var created Administrator
	err := r.QueryRow(ctx, q, a.ID, a.Email, a.Name, hash, a.Role, a.Status).Scan(
		&created.ID, &created.Email, &created.Name, &created.Role, &created.Status,
		&created.CreatedAt, &created.UpdatedAt)
	switch {
	case db.IsUniqueViolation(err, "uq_admin_users_email"):
		return Administrator{}, ErrAdminEmailTaken
	case err != nil:
		return Administrator{}, fmt.Errorf("admin: creating an administrator: %w", err)
	}
	return created, nil
}

// insertSession records a sign-in.
//
// The hash is a parameter rather than a field on [Session] so that no struct in this package can
// carry a credential, in either direction — see [Issued].
func (postgresStore) insertSession(ctx context.Context, r db.Runner, s Session, tokenHash string) error {
	const q = `
		INSERT INTO admin_sessions
			(id, admin_user_id, token_hash, idle_expires_at, absolute_expires_at, last_used_at)
		VALUES ($1, $2, $3, $4, $5, $6)`

	if _, err := r.Exec(ctx, q,
		s.ID, s.AdminID, tokenHash, s.IdleExpiresAt, s.AbsoluteExpiresAt, s.LastUsedAt,
	); err != nil {
		return fmt.Errorf("admin: recording an administrator session: %w", err)
	}
	return nil
}

// sessionByTokenHash reads a session and the account it belongs to, in one statement.
//
// One round trip rather than two, because this runs on every administrative request and because
// the two facts have to be consistent with each other: an account disabled between the session read
// and the account read would be served for one more request, which is precisely the window this
// credential design exists to close.
//
// **No `FOR UPDATE` and no transaction.** Nothing here is a read-modify-write: the slide that
// follows is a blind UPDATE whose value depends only on the clock, and two tabs racing it both
// write approximately the same instant. `device_sessions` locks because rotation must have exactly
// one winner (SHIP-39); nothing here must.
//
// Returns [db.ErrNoRows] when the digest matches nothing. The caller must not distinguish that from
// a revoked or lapsed session on the wire.
func (postgresStore) sessionByTokenHash(
	ctx context.Context,
	r db.Runner,
	tokenHash string,
) (Session, Administrator, error) {
	const q = `
		SELECT s.id, s.admin_user_id, s.idle_expires_at, s.absolute_expires_at,
		       s.last_used_at, s.revoked_at, s.created_at,
		       a.id, a.email, a.name, a.role, a.status, a.created_at, a.updated_at
		FROM admin_sessions s
		JOIN admin_users a ON a.id = s.admin_user_id
		WHERE s.token_hash = $1`

	var (
		s         Session
		a         Administrator
		revokedAt *time.Time
	)
	err := r.QueryRow(ctx, q, tokenHash).Scan(
		&s.ID, &s.AdminID, &s.IdleExpiresAt, &s.AbsoluteExpiresAt,
		&s.LastUsedAt, &revokedAt, &s.CreatedAt,
		&a.ID, &a.Email, &a.Name, &a.Role, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	if err != nil {
		return Session{}, Administrator{}, err
	}

	// NULL means live, which the zero value says just as well — and [Session.Live] reads it that
	// way, so there is one representation of "not revoked" rather than a pointer everything has
	// to check.
	if revokedAt != nil {
		s.RevokedAt = *revokedAt
	}
	return s, a, nil
}

// slideSession moves the idle window forward and records that the console was used.
//
// # Why the WHERE clause carries more than the primary key
//
// `revoked_at IS NULL` stops a slide resurrecting a session that was signed out between the read
// and this write, which is a real race with two browser tabs: one signs out while the other is
// mid-request. Without it the second tab's slide would rewrite `idle_expires_at` on a revoked row —
// harmless today, because [Session.Live] checks revocation first, and exactly the kind of
// "harmless" that stops being so when somebody later writes a query that trusts the expiry alone.
//
// The `LEAST` is belt and braces beside the caller's own clamp and beside
// `ck_admin_sessions_idle_within_absolute`. Three copies of one rule is two too many in most places
// and right here: this is the statement that could be called from somewhere else later, the clamp
// is the one a refactor could drop, and the CHECK is the one that turns either mistake into a
// failed write rather than a session that outlives its cap.
//
// A row affected count of zero is not an error. The session was revoked or removed while this was
// in flight, and the request it belongs to has already been authorised — see [Authenticator.slide]
// for why a failure here must not overturn that.
func (postgresStore) slideSession(
	ctx context.Context,
	r db.Runner,
	id uuid.UUID,
	idleExpiresAt, usedAt time.Time,
) error {
	const q = `
		UPDATE admin_sessions
		   SET idle_expires_at = LEAST($2, absolute_expires_at),
		       last_used_at    = $3
		 WHERE id = $1
		   AND revoked_at IS NULL`

	if _, err := r.Exec(ctx, q, id, idleExpiresAt, usedAt); err != nil {
		return fmt.Errorf("admin: extending an administrator session: %w", err)
	}
	return nil
}

// revokeSession ends a session.
//
// `revoked_at IS NULL` in the WHERE makes this idempotent *and* truthful: a second sign-out is a
// success and does not move the timestamp, so the recorded instant is when the session actually
// ended rather than when somebody's browser last retried.
//
// A session that does not exist is also a success. The caller is the guard, which has just resolved
// this session id from a live row; if it has vanished in between, the outcome the caller wanted has
// happened. Reporting a 404 here would tell an administrator their sign-out failed when it had not.
func (postgresStore) revokeSession(ctx context.Context, r db.Runner, id uuid.UUID, at time.Time) error {
	const q = `UPDATE admin_sessions SET revoked_at = $2 WHERE id = $1 AND revoked_at IS NULL`

	if _, err := r.Exec(ctx, q, id, at); err != nil {
		return fmt.Errorf("admin: revoking an administrator session: %w", err)
	}
	return nil
}

// isNoRows reports whether err is the driver's "no such row".
//
// A helper rather than errors.Is at each site, matching identity's, because the sentinel is
// re-exported by internal/db and a caller reaching for pgx directly would be importing a driver to
// get one variable.
//
// There is deliberately no "revoke every session this administrator holds" here.
// [StatusDisabled]'s promise — that disabling an account ends its live sessions — is kept by
// [Authenticator.Resolve] reading the account on every request, which is immediate and needs no
// write. A sweep that had to run as well would be a second mechanism for one rule, and the one that
// could be forgotten.
func isNoRows(err error) bool { return errors.Is(err, db.ErrNoRows) }
