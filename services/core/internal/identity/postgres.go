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

// isNoRows reports whether a read found nothing, which is an outcome rather than a failure at
// every call site in this package.
func isNoRows(err error) bool { return errors.Is(err, db.ErrNoRows) }
