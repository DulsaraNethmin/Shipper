package migrations_test

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-147's two tables, checked where their guarantees live.
//
// Everything below is a constraint, an index or a default rather than application logic, which is
// where Docs/06 §4.1 puts the demonstration: "a mock happily accepts a write that the actual
// constraint would reject". Three of these are the whole of a security control —
// `ck_admin_sessions_idle_within_absolute` is what makes the twelve-hour cap true whatever writes
// the row, `role`'s default is what makes least privilege true of a row nobody chose a role for,
// and `uq_admin_users_email` is what makes one address one account when two creations race.
//
// newUser comes from schema_test.go and constraintLiterals from milestones_test.go, both in this
// external test package.

// aDeveloperHash is a syntactically valid argon2id PHC string used where a test needs the *shape*
// rather than a credential.
//
// It hashes nothing anybody knows and nothing verifies it here. `ck_admin_users_password_hash`
// checks the prefix, and that is the only thing these tests are asking about — the real hashing is
// internal/passwords' and is tested there.
const aDeveloperHash = "$argon2id$v=19$m=64,t=1,p=1$c2FsdHNhbHRzYWx0c2Fs$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// newAdministrator inserts one and returns its id, so a caller can assert on the error instead when
// that is what it is testing.
func newAdministrator(t *testing.T, pool *pgxpool.Pool, email, role string) (uuid.UUID, error) {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	if role == "" {
		// The column default is the subject of one of the tests below, so "no role stated" has
		// to be expressible as a statement that does not name the column at all.
		_, err = pool.Exec(t.Context(),
			`INSERT INTO admin_users (id, email, name, password_hash) VALUES ($1, $2, 'A Person', $3)`,
			id, email, aDeveloperHash)
	} else {
		_, err = pool.Exec(t.Context(),
			`INSERT INTO admin_users (id, email, name, password_hash, role) VALUES ($1, $2, 'A Person', $3, $4)`,
			id, email, aDeveloperHash, role)
	}
	return id, err
}

// newAdminSession inserts a session with the two expiries given, returning the error to assert on.
func newAdminSession(
	t *testing.T,
	pool *pgxpool.Pool,
	adminID uuid.UUID,
	tokenHash string,
	idle, absolute time.Time,
) error {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	_, err = pool.Exec(t.Context(), `
		INSERT INTO admin_sessions
			(id, admin_user_id, token_hash, idle_expires_at, absolute_expires_at, last_used_at)
		VALUES ($1, $2, $3, $4, $5, now())`,
		id, adminID, tokenHash, idle, absolute)
	return err
}

// TestAdminRoleConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing, in both directions.
//
// A role the database accepts and Go has no bundle for is an administrator whose permissions are
// undefined; a role Go knows and the database refuses is one nobody can be given. Neither is
// visible from either side alone, which is why this test reads the constraint rather than trusting
// that two lists written in two languages still agree.
func TestAdminRoleConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_admin_users_role")

	for _, role := range admin.Roles {
		if !inDatabase[role.String()] {
			t.Errorf("admin.Roles has %q and ck_admin_users_role refuses it, so no administrator "+
				"can ever be given that role", role)
		}
		delete(inDatabase, role.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_admin_users_role accepts %q and admin.Roles has no such role, so an "+
			"administrator could hold a role nothing in Go grants any permission for", leftover)
	}
}

// TestAdminStatusConstraintMatchesTheGoConstants is the same pairing for the other enumeration.
func TestAdminStatusConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_admin_users_status")

	for _, status := range admin.Statuses {
		if !inDatabase[status.String()] {
			t.Errorf("admin.Statuses has %q and ck_admin_users_status refuses it", status)
		}
		delete(inDatabase, status.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_admin_users_status accepts %q and admin.Statuses has no such status", leftover)
	}
}

// TestAnAdministratorWithNoRoleStatedGetsTheLeastPrivilegedOne is SHIP-148's *Done when* as a
// property of the schema.
//
// "Permissions default to the minimum" has to be true of a row written by something that never went
// through Go — a migration, a support script, a psql prompt. The column default is what makes it
// true of those, and this is the test that fails if somebody changes it to `owner` because it was
// convenient during a fixture.
func TestAnAdministratorWithNoRoleStatedGetsTheLeastPrivilegedOne(t *testing.T) {
	pool := pgtest.DB(t)

	id, err := newAdministrator(t, pool, "defaulted@example.com", "")
	if err != nil {
		t.Fatalf("inserting an administrator with no role: %v", err)
	}

	var role string
	if err := pool.QueryRow(t.Context(),
		`SELECT role FROM admin_users WHERE id = $1`, id).Scan(&role); err != nil {
		t.Fatalf("reading the role back: %v", err)
	}

	// admin.Roles is ordered least privileged first, so the first entry *is* the minimum. Written
	// this way rather than as the literal "support" so that reordering the list — which would
	// change what "the minimum" means — fails here as well as in the code that reads it.
	if want := admin.Roles[0].String(); role != want {
		t.Errorf("an administrator inserted with no role got %q, want %q.\n"+
			"Docs/04 §9 asks for least-privilege administrative access, and a default that is "+
			"anything but the smallest bundle means a forgotten column is a privilege escalation.",
			role, want)
	}
}

// TestOneAdministratorAccountPerAddress is uq_admin_users_email.
//
// Case-insensitively, because the column is citext: an administrator typing Alice@ and alice@ at a
// sign-in screen means one account, and two rows would be two credentials for one person that
// nobody could tell apart in an audit trail.
func TestOneAdministratorAccountPerAddress(t *testing.T) {
	pool := pgtest.DB(t)

	if _, err := newAdministrator(t, pool, "taken@example.com", "support"); err != nil {
		t.Fatalf("inserting the first administrator: %v", err)
	}
	if _, err := newAdministrator(t, pool, "TAKEN@example.com", "owner"); err == nil {
		t.Error("a second administrator was created for the same address in a different case")
	}
}

// TestAnAdministratorAccountIsNotAUserAccount is the *Done when*'s second clause as a fact about
// the schema.
//
// The two systems share an address space and nothing else. A person who moderates the marketplace
// may also ship pallets on it, and the platform must not infer the second from the first — so the
// same address in both tables is two accounts, and neither insert knows about the other.
//
// **What is really being asserted is the absence of a join.** There is no column on `admin_users`
// pointing at `users` and none the other way, so there is nothing for a later ticket to resolve one
// credential into the other with. This test would fail the moment somebody added one, because the
// second insert would need a value for it.
func TestAnAdministratorAccountIsNotAUserAccount(t *testing.T) {
	pool := pgtest.DB(t)

	newUser(t, pool, "both@example.com", "0400000801", "customer")

	if _, err := newAdministrator(t, pool, "both@example.com", "moderator"); err != nil {
		t.Fatalf("an address with a user account could not also have an administrator account: %v\n"+
			"These are separate credential systems (SHIP-147). Refusing this would make being a "+
			"marketplace user and an administrator mutually exclusive, which no document asks for.",
			err)
	}
}

// TestAStoredAdministratorHashMustLookLikeArgon2id is ck_admin_users_password_hash.
//
// A value that is not a PHC argon2id string is a credential nothing can verify, and it would
// surface as an unreadable-hash 500 at somebody's sign-in rather than at the write that caused it.
// Cheap to refuse where it happens.
func TestAStoredAdministratorHashMustLookLikeArgon2id(t *testing.T) {
	pool := pgtest.DB(t)

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	_, err = pool.Exec(t.Context(),
		`INSERT INTO admin_users (id, email, name, password_hash) VALUES ($1, $2, 'A Person', $3)`,
		id, "plaintext@example.com", "hunter2")
	if err == nil {
		t.Error("an administrator was stored with a password hash that is not an argon2id PHC string")
	}
}

// TestASessionCannotSlidePastItsAbsoluteExpiry is ck_admin_sessions_idle_within_absolute, and it is
// the constraint that makes SHIP-147's twelve-hour cap a control rather than a convention.
//
// The Go that slides the window clamps it (Authenticator.slide) and the UPDATE clamps it again
// (postgresStore.slideSession). This is the third copy and the only one that is still true when
// somebody deletes the other two, which is exactly the argument Docs/06 §4.1 makes for testing
// against a real database rather than a mock.
func TestASessionCannotSlidePastItsAbsoluteExpiry(t *testing.T) {
	pool := pgtest.DB(t)

	id, err := newAdministrator(t, pool, "capped@example.com", "support")
	if err != nil {
		t.Fatalf("inserting an administrator: %v", err)
	}

	now := time.Now().UTC()
	absolute := now.Add(2 * time.Hour)

	if err := newAdminSession(t, pool, id, "capped-hash", now.Add(30*time.Minute), absolute); err != nil {
		t.Fatalf("inserting a session inside its cap: %v", err)
	}

	// The insert itself must refuse an idle window that starts beyond the cap.
	if err := newAdminSession(t, pool, id, "over-hash", absolute.Add(time.Minute), absolute); err == nil {
		t.Error("a session was created with an idle expiry beyond its absolute one")
	}

	// And the UPDATE must refuse one that is moved beyond it, which is the direction a sliding
	// window actually fails in.
	if _, err := pool.Exec(t.Context(),
		`UPDATE admin_sessions SET idle_expires_at = $1 WHERE token_hash = 'capped-hash'`,
		absolute.Add(time.Hour),
	); err == nil {
		t.Error("an administrator session's idle window was slid past its absolute expiry, so the " +
			"twelve-hour cap can be defeated by using the console")
	}
}

// TestOneAdministratorSessionPerToken is uq_admin_sessions_token_hash.
//
// One token belongs to one session. Without this, a bug that wrote the same digest twice would
// leave a credential resolving to two administrators, and which one an audit entry named would
// depend on the query plan.
func TestOneAdministratorSessionPerToken(t *testing.T) {
	pool := pgtest.DB(t)

	first, err := newAdministrator(t, pool, "one@example.com", "support")
	if err != nil {
		t.Fatalf("inserting an administrator: %v", err)
	}
	second, err := newAdministrator(t, pool, "two@example.com", "support")
	if err != nil {
		t.Fatalf("inserting a second administrator: %v", err)
	}

	now := time.Now().UTC()
	if err := newAdminSession(t, pool, first, "shared-hash", now.Add(time.Minute), now.Add(time.Hour)); err != nil {
		t.Fatalf("inserting the first session: %v", err)
	}
	if err := newAdminSession(t, pool, second, "shared-hash", now.Add(time.Minute), now.Add(time.Hour)); err == nil {
		t.Error("two administrators hold sessions with the same token digest")
	}
}
