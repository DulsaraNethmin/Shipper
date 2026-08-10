package pgtest_test

import (
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// TestCloneCarriesTheSchema proves the template was migrated before it was cloned.
//
// If `make test-db-template` ever stops running the migration chain, every integration test in
// the service starts failing at once with an unhelpful "relation does not exist". This one
// fails with the reason instead.
func TestCloneCarriesTheSchema(t *testing.T) {
	pool := pgtest.DB(t)

	var hasCitext bool
	if err := pool.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'citext')`,
	).Scan(&hasCitext); err != nil {
		t.Fatalf("querying the clone: %v", err)
	}
	if !hasCitext {
		t.Error("the clone has no citext extension, so the template was not migrated")
	}

	var hasTrigger bool
	if err := pool.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM pg_proc WHERE proname = 'set_updated_at')`,
	).Scan(&hasTrigger); err != nil {
		t.Fatalf("querying the clone: %v", err)
	}
	if !hasTrigger {
		t.Error("the clone has no set_updated_at function, so the template was not migrated")
	}
}

// TestEachTestGetsItsOwnDatabase is the property the whole package exists for.
//
// Two tests writing to one database interfere in ways that look like flakiness rather than
// like a shared-state bug, and `go test ./...` runs packages in parallel by default — so this
// matters even before anyone is working in parallel.
func TestEachTestGetsItsOwnDatabase(t *testing.T) {
	first := pgtest.DB(t)
	second := pgtest.DB(t)

	if _, err := first.Exec(t.Context(),
		`CREATE TABLE only_in_the_first (id int)`,
	); err != nil {
		t.Fatalf("creating a table in the first database: %v", err)
	}

	var visible bool
	if err := second.QueryRow(t.Context(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_name = 'only_in_the_first')`,
	).Scan(&visible); err != nil {
		t.Fatalf("querying the second database: %v", err)
	}
	if visible {
		t.Error("a table created in one test database was visible from another; they are not isolated")
	}
}

// TestTheCloneIsWritable catches a template left marked read-only, or permissions that allow
// the copy but not the use.
func TestTheCloneIsWritable(t *testing.T) {
	pool := pgtest.DB(t)

	if _, err := pool.Exec(t.Context(), `CREATE TABLE scratch (id int primary key)`); err != nil {
		t.Fatalf("creating a table: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `INSERT INTO scratch (id) VALUES (1)`); err != nil {
		t.Fatalf("inserting: %v", err)
	}

	var n int
	if err := pool.QueryRow(t.Context(), `SELECT count(*) FROM scratch`).Scan(&n); err != nil {
		t.Fatalf("counting: %v", err)
	}
	if n != 1 {
		t.Errorf("count = %d, want 1", n)
	}
}
