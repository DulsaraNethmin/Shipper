package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// SHIP-38's table, checked where its guarantees actually live.
//
// Every assertion here is about a constraint, an index or a trigger, so all of them run against
// a real PostgreSQL — Docs/06 §4.1: "a mock happily accepts a write that the actual constraint
// would reject."

// newSession inserts one device session and returns its id.
func newSession(t *testing.T, pool *pgxpool.Pool, user uuid.UUID, hash, label string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	// refresh_token_expires_at is supplied rather than defaulted (SHIP-39). The column is NOT
	// NULL with no default on purpose: a session row with no expiry is a credential that never
	// lapses, which is the defect 000103 exists to prevent, so a helper that omitted it would
	// be testing a row the service can never write.
	_, err = pool.Exec(t.Context(),
		`INSERT INTO device_sessions
		     (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
		 VALUES ($1, $2, $3, now() + interval '30 days', $4)`,
		id, user, hash, label)
	if err != nil {
		t.Fatalf("inserting a session for %s: %v", user, err)
	}
	return id
}

func TestDeviceSessionRoundTrips(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "sessions@example.com", "+61400000100", "customer")
	id := newSession(t, pool, user, "hash-of-an-opaque-token", "Nethmin's iPhone")

	var (
		label      string
		lastSeen   time.Time
		created    time.Time
		storedUser uuid.UUID
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT user_id, device_label, last_seen_at, created_at FROM device_sessions WHERE id = $1`, id,
	).Scan(&storedUser, &label, &lastSeen, &created); err != nil {
		t.Fatalf("reading the session back: %v", err)
	}

	if storedUser != user {
		t.Errorf("session belongs to %s, want %s", storedUser, user)
	}
	if label != "Nethmin's iPhone" {
		t.Errorf("device_label = %q", label)
	}
	// Defaulted rather than supplied: a session that has never been used was last seen when it
	// was created, which is what the device list should show.
	if lastSeen.IsZero() {
		t.Error("last_seen_at was not defaulted on insert")
	}
}

// TestDeviceSessionRequiresARealUser is the foreign key, and the fact that it is RESTRICT.
//
// Docs/10 §3.3 forbids a cascade here. SHIP-171 pseudonymises an account rather than deleting
// it, so nothing should ever be removing a user row — and if something does, it must fail
// loudly rather than quietly signing every device out.
func TestDeviceSessionRequiresARealUser(t *testing.T) {
	pool := pgtest.DB(t)

	t.Run("an unknown user is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO device_sessions
			     (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
			 VALUES ($1, $2, 'h', now() + interval '30 days', 'Pixel 8')`, id, stranger)
		if err == nil {
			t.Fatal("a session was created for an account that does not exist")
		}
		if !strings.Contains(err.Error(), "fk_device_sessions_user") {
			t.Errorf("expected the foreign key to refuse it, got: %v", err)
		}
	})

	t.Run("deleting the account is refused while a session exists", func(t *testing.T) {
		user := newUser(t, pool, "restrict@example.com", "+61400000101", "provider")
		newSession(t, pool, user, "hash-restrict", "Pixel 8")

		_, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, user)
		if err == nil {
			t.Fatal("the account was deleted, taking its sessions with it")
		}
		if !strings.Contains(err.Error(), "fk_device_sessions_user") {
			t.Errorf("expected ON DELETE RESTRICT to refuse it, got: %v", err)
		}
	})
}

// TestDeviceSessionRefreshHashIsUnique keeps one token from refreshing two sessions.
func TestDeviceSessionRefreshHashIsUnique(t *testing.T) {
	pool := pgtest.DB(t)

	first := newUser(t, pool, "one-token@example.com", "+61400000102", "customer")
	second := newUser(t, pool, "two-token@example.com", "+61400000103", "customer")
	newSession(t, pool, first, "the-same-hash", "iPhone")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO device_sessions
		     (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
		 VALUES ($1, $2, 'the-same-hash', now() + interval '30 days', 'Pixel 8')`, id, second)
	if err == nil {
		t.Fatal("two sessions hold the same refresh token hash")
	}
	if !strings.Contains(err.Error(), "uq_device_sessions_refresh_token_hash") {
		t.Errorf("expected the uniqueness index to refuse it, got: %v", err)
	}
}

func TestDeviceSessionLabelIsConstrained(t *testing.T) {
	pool := pgtest.DB(t)
	user := newUser(t, pool, "label@example.com", "+61400000104", "customer")

	cases := map[string]string{
		"blank":    "",
		"too long": strings.Repeat("x", 121),
	}

	for name, label := range cases {
		t.Run(name, func(t *testing.T) {
			id, _ := uuid.NewV7()
			_, err := pool.Exec(t.Context(),
				`INSERT INTO device_sessions
				     (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label)
				 VALUES ($1, $2, $3, now() + interval '30 days', $4)`, id, user, "hash-"+name, label)
			if err == nil {
				t.Fatal("a device nobody could identify in their own device list was accepted")
			}
			if !strings.Contains(err.Error(), "ck_device_sessions_device_label") {
				t.Errorf("expected ck_device_sessions_device_label to refuse it, got: %v", err)
			}
		})
	}
}

// TestDeviceSessionUpdatedAtIsMaintained proves the trigger is attached to this table, rather
// than relying on the sweep in schema_test.go noticing later.
//
// The failure it catches is the quiet one: the column exists, defaults correctly, is NOT NULL,
// and simply never moves — so "when was this device last touched" answers with the creation
// time forever.
func TestDeviceSessionUpdatedAtIsMaintained(t *testing.T) {
	pool := pgtest.DB(t)

	user := newUser(t, pool, "touched@example.com", "+61400000105", "customer")
	id := newSession(t, pool, user, "hash-touched", "iPhone")

	var before time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM device_sessions WHERE id = $1`, id).Scan(&before); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	// A separate statement, and therefore a separate transaction: now() is transaction start
	// time, so within one transaction the timestamp would legitimately not move.
	if _, err := pool.Exec(t.Context(),
		`UPDATE device_sessions SET last_seen_at = now() WHERE id = $1`, id); err != nil {
		t.Fatalf("updating: %v", err)
	}

	var after time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM device_sessions WHERE id = $1`, id).Scan(&after); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	if !after.After(before) {
		t.Errorf("updated_at did not move on update (%s then %s); is the trigger attached?", before, after)
	}
}

// TestDeviceSessionTimestampsCarryTheirZone is Docs/10 §3.3's "timestamptz always, never
// timestamp".
//
// A plain timestamp column accepts every write and reads back correctly on the machine that
// wrote it. It goes wrong only across a zone boundary, months later, in a way that looks like a
// data-entry mistake rather than a schema one.
func TestDeviceSessionTimestampsCarryTheirZone(t *testing.T) {
	pool := pgtest.DB(t)

	rows, err := pool.Query(t.Context(), `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'device_sessions'
		  AND data_type LIKE 'timestamp%'
		ORDER BY column_name`)
	if err != nil {
		t.Fatalf("reading the column types: %v", err)
	}
	defer rows.Close()

	found := 0
	for rows.Next() {
		var name, dataType string
		if err := rows.Scan(&name, &dataType); err != nil {
			t.Fatalf("scanning: %v", err)
		}
		found++
		if dataType != "timestamp with time zone" {
			t.Errorf("device_sessions.%s is %s, want timestamp with time zone", name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if found != 4 {
		t.Errorf("found %d timestamp columns, want 4 (refresh_token_expires_at, last_seen_at, "+
			"created_at, updated_at)", found)
	}
}

// TestDeviceSessionForeignKeyIsIndexed is the other half of Docs/10 §3.3's foreign key rule.
//
// An unindexed foreign key is invisible until the table is large: every delete on users takes a
// sequential scan of this one to check the constraint, and so does every "list my devices".
func TestDeviceSessionForeignKeyIsIndexed(t *testing.T) {
	pool := pgtest.DB(t)

	var indexes int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t     ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'device_sessions' AND a.attname = 'user_id'`,
	).Scan(&indexes); err != nil {
		t.Fatalf("looking for the index: %v", err)
	}
	if indexes == 0 {
		t.Error("device_sessions.user_id carries a foreign key and no index leading with it")
	}
}

// TestDeviceSessionMigrationIsInTheIdentityBlock names the number, because the block scheme is
// only worth having if something checks that a migration landed inside its range.
//
// TestEveryMigrationIsInItsBlock already sweeps every file; this says which block this one
// belongs to, so renumbering it into another domain's range fails here rather than silently
// consuming a number that domain was relying on.
func TestDeviceSessionMigrationIsInTheIdentityBlock(t *testing.T) {
	block, ok := migrations.BlockContaining(100)
	if !ok {
		t.Fatal("version 100 falls in no reserved block")
	}
	if block.Domain != "identity" {
		t.Errorf("version 100 is in the %q block, want identity", block.Domain)
	}
}

// TestDeviceSessionRefreshExpiryIsRequired is SHIP-39's expiry decision, enforced by the schema
// rather than remembered by the code that writes the row.
//
// A session with no expiry is a refresh token that never lapses, which is a stolen phone signed
// in forever. Docs/11 §9 carried the question from SHIP-38 and 000103 answers it here — so the
// column is NOT NULL with no default, and an INSERT that omits it fails rather than quietly
// producing an immortal credential.
func TestDeviceSessionRefreshExpiryIsRequired(t *testing.T) {
	pool := pgtest.DB(t)
	user := newUser(t, pool, "no-expiry@example.com", "+61400000106", "customer")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO device_sessions (id, user_id, refresh_token_hash, device_label)
		 VALUES ($1, $2, 'hash-no-expiry', 'iPhone')`, id, user)
	if err == nil {
		t.Fatal("a session was created with no refresh token expiry, which is a credential " +
			"that never lapses")
	}
	if !strings.Contains(err.Error(), "refresh_token_expires_at") {
		t.Errorf("expected the NOT NULL on refresh_token_expires_at to refuse it, got: %v", err)
	}
}

// TestDeviceSessionRefreshExpiryMustBeInTheFuture catches the clock or configuration mistake
// where it happens, rather than as a device that can never stay signed in.
func TestDeviceSessionRefreshExpiryMustBeInTheFuture(t *testing.T) {
	pool := pgtest.DB(t)
	user := newUser(t, pool, "past-expiry@example.com", "+61400000107", "customer")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(), `
		INSERT INTO device_sessions
		    (id, user_id, refresh_token_hash, refresh_token_expires_at, device_label, created_at)
		VALUES ($1, $2, 'hash-past-expiry', now() - interval '1 day', 'iPhone', now())`, id, user)
	if err == nil {
		t.Fatal("a session was created holding a token that had already expired")
	}
	if !strings.Contains(err.Error(), "ck_device_sessions_refresh_expiry") {
		t.Errorf("expected ck_device_sessions_refresh_expiry to refuse it, got: %v", err)
	}
}
