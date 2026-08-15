package migrations_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-30a's column, checked where its guarantees live.
//
// Every assertion here is about the constraint or about the column's nullability, so all of them
// run against a real PostgreSQL — Docs/06 §4.1: "a mock happily accepts a write that the actual
// constraint would reject."

// TestAnAccountMayHaveNoName is the decision `000006` argues, asserted rather than commented.
//
// The column is nullable **on purpose**: every account that existed before the migration has no
// name, and a name cannot be backfilled — a `DEFAULT ”` or an 'Unknown' would put a value nobody
// supplied into the one field whose whole purpose is that somebody supplied it.
//
// This is therefore a test of an *absence of a constraint*, which is unusual and is why it is
// written out. A future `SET NOT NULL` — which `000006` names as the tightening to make once every
// row has a name — fails here, which is the correct place for that decision to be noticed.
func TestAnAccountMayHaveNoName(t *testing.T) {
	pool := pgtest.DB(t)

	id := newUser(t, pool, "nameless@example.com", "+61400000301", "customer")

	var name *string
	if err := pool.QueryRow(t.Context(),
		`SELECT name FROM users WHERE id = $1`, id).Scan(&name); err != nil {
		t.Fatalf("reading the name back: %v", err)
	}
	if name != nil {
		t.Errorf("name = %q, want NULL — an account created without one has none", *name)
	}
}

// TestASuppliedNameMayNotBeBlank is `ck_users_name`, and the tab and the newline are the cases.
//
// **PostgreSQL's one-argument `btrim` strips spaces only.** `ck_admin_notes_body` (SHIP-162) was
// written that way and a body of a single newline satisfied it while `strings.TrimSpace` in the
// service refused the same value — two checks that were believed to agree and did not, disagreeing
// only through a connection that did not go through the service, which is exactly the connection a
// CHECK constraint exists for. `000006` spells the character set out; this is what holds it there.
func TestASuppliedNameMayNotBeBlank(t *testing.T) {
	pool := pgtest.DB(t)

	for i, blank := range []string{"", " ", "  ", "\t", "\n", "\r", " \t\r\n "} {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an id: %v", err)
		}

		// The address and the number are made unique per case rather than derived from the
		// identifier: a UUIDv7 begins with a timestamp, so two generated in the same
		// millisecond share their leading characters and the *uniqueness* index would refuse
		// the second row before `ck_users_name` ever saw it — a green test of the wrong
		// constraint.
		_, err = pool.Exec(t.Context(),
			`INSERT INTO users (id, name, email, phone, password_hash, role)
			 VALUES ($1, $2, $3, $4, 'x', 'customer')`,
			id, blank, fmt.Sprintf("blank%d@example.com", i), fmt.Sprintf("+6140310%04d", i))
		if err == nil {
			t.Errorf("a name of %q was stored; NULL and blank would then be two spellings "+
				"of no name", blank)
			continue
		}
		if !strings.Contains(err.Error(), "ck_users_name") {
			t.Errorf("a name of %q was refused by something other than ck_users_name: %v",
				blank, err)
		}
	}
}

// TestANameWithSurroundingTextIsStoredAsGiven. The constraint bounds blankness and nothing else.
//
// A name is not a format. `000404`s comment settles where length limits live for this schema —
// they are validation limits, Docs/06 §5.3 wants those changeable without a deploy, and a CHECK is
// a migration — so the database refuses only "nothing at all" and `internal/identity` bounds the
// rest. A mononym, a single character and a name with internal spacing all store.
func TestANameWithSurroundingTextIsStoredAsGiven(t *testing.T) {
	pool := pgtest.DB(t)

	for i, name := range []string{"A", "Prince", "Ngô Đình", "Alice  Nguyen", "  padded  "} {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an id: %v", err)
		}

		if _, err := pool.Exec(t.Context(),
			`INSERT INTO users (id, name, email, phone, password_hash, role)
			 VALUES ($1, $2, $3, $4, 'x', 'customer')`,
			id, name, fmt.Sprintf("named%d@example.com", i),
			fmt.Sprintf("+6140320%04d", i)); err != nil {
			t.Errorf("a name of %q was refused: %v", name, err)
			continue
		}

		var stored string
		if err := pool.QueryRow(t.Context(),
			`SELECT name FROM users WHERE id = $1`, id).Scan(&stored); err != nil {
			t.Fatalf("reading %q back: %v", name, err)
		}
		if stored != name {
			t.Errorf("stored %q as %q; the column normalises nothing", name, stored)
		}
	}
}
