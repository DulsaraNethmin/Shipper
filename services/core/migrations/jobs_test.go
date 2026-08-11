package migrations_test

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
	"github.com/DulsaraNethmin/Shipper/services/core/migrations"
)

// SHIP-56's table, checked where its guarantees live.
//
// Every assertion here is about a constraint, an index or a trigger, so all of them run against
// a real PostgreSQL — Docs/06 §4.1: "a mock happily accepts a write that the actual constraint
// would reject."

// newJob inserts one job at its starting status and returns its id.
//
// It names no status, which is the point: 'Draft' is the column default and 000402 refuses any
// insert that arrives at another status, so this is the only way a job is ever created.
func newJob(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, id, customer); err != nil {
		t.Fatalf("inserting a job for %s: %v", customer, err)
	}
	return id
}

// quotedLiteral pulls the string literals out of a constraint definition.
//
// PostgreSQL rewrites `status IN ('Draft', …)` as `status = ANY (ARRAY['Draft'::text, …])`, so
// what comes back from pg_get_constraintdef is not the text that was written. Reading the
// literals out of it is what makes the comparison against the Go constants meaningful rather
// than a string equality test that would fail on formatting.
var quotedLiteral = regexp.MustCompile(`'([^']*)'::text`)

// TestJobStatusConstraintCoversTheTwelveStatuses is SHIP-56's acceptance criterion, and it is
// checked in both directions.
//
// Docs/10 §3.4 requires every enumeration to be paired with a test that reads the constraint out
// of pg_constraint and asserts the PostgreSQL list equals the Go constant list. One direction
// alone is not enough: a status missing from the database is a job that cannot reach a state the
// product has, and a status missing from Go is a state the database will accept and no code
// knows how to handle.
func TestJobStatusConstraintCoversTheTwelveStatuses(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(), `
		SELECT pg_get_constraintdef(oid)
		FROM pg_constraint
		WHERE conname = 'ck_jobs_status'`,
	).Scan(&definition); err != nil {
		t.Fatalf("reading ck_jobs_status out of pg_constraint: %v", err)
	}

	inDatabase := map[string]bool{}
	for _, match := range quotedLiteral.FindAllStringSubmatch(definition, -1) {
		inDatabase[match[1]] = true
	}

	inGo := map[string]bool{}
	for _, status := range jobs.Statuses {
		inGo[string(status)] = true
	}

	if len(jobs.Statuses) != 12 {
		t.Errorf("jobs.Statuses holds %d statuses; Docs/02 §1 has twelve", len(jobs.Statuses))
	}
	if len(inGo) != len(jobs.Statuses) {
		t.Errorf("jobs.Statuses contains a duplicate: %d constants, %d distinct values",
			len(jobs.Statuses), len(inGo))
	}

	for status := range inGo {
		if !inDatabase[status] {
			t.Errorf("Go has the status %q and ck_jobs_status does not permit it; "+
				"the database would refuse a job that reached it", status)
		}
	}
	for status := range inDatabase {
		if !inGo[status] {
			t.Errorf("ck_jobs_status permits %q and Go has no constant for it; "+
				"the database would accept a state no code handles", status)
		}
	}

	// Named explicitly as well as compared, because both lists could drift together if
	// somebody "fixed" the spacing on one and then made the other match. Docs/02 §1 is the
	// authority for these exact strings, and Docs/10 §3.4 for storing them unaltered.
	for _, want := range []string{"Driver assigned", "En route to pickup", "Picked up", "In transit"} {
		if !inDatabase[want] {
			t.Errorf("ck_jobs_status does not permit %q; Docs/02 §1 writes the statuses "+
				"with spaces and in sentence case, and Docs/10 §3.4 stores them that way", want)
		}
	}
}

// TestJobStatusConstraintRefusesAnythingElse proves the constraint is real rather than
// decorative, which is the trade Docs/10 §3.4 accepts by choosing text-plus-CHECK over a
// PostgreSQL enum type: the column type no longer says what the column may hold, so something
// has to test that the constraint does.
func TestJobStatusConstraintRefusesAnythingElse(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newUser(t, pool, "unknown-status@example.com", "+61400000400", "customer")

	id, _ := uuid.NewV7()
	_, err := pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id, status) VALUES ($1, $2, $3)`,
		id, customer, "Pending")
	if err == nil {
		t.Fatal("a job was created at a status that is not one of the twelve")
	}
	if !strings.Contains(err.Error(), "ck_jobs_status") {
		t.Errorf("expected ck_jobs_status to refuse it, got: %v", err)
	}
}

// TestJobRequiresARealCustomer is the foreign key, and the fact that it is RESTRICT.
//
// Docs/10 §3.3 forbids a cascade: SHIP-171 pseudonymises an account rather than deleting it, so
// nothing should be removing a user row — and if something does, it must fail loudly rather than
// quietly destroying the transaction record Docs/05 §3.1 requires retaining.
func TestJobRequiresARealCustomer(t *testing.T) {
	pool := pgtest.DB(t)

	t.Run("an unknown customer is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		stranger, _ := uuid.NewV7()
		_, err := pool.Exec(t.Context(),
			`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, id, stranger)
		if err == nil {
			t.Fatal("a job was created for an account that does not exist")
		}
		if !strings.Contains(err.Error(), "fk_jobs_customer") {
			t.Errorf("expected the foreign key to refuse it, got: %v", err)
		}
	})

	t.Run("deleting the customer is refused while a job exists", func(t *testing.T) {
		customer := newUser(t, pool, "restrict-job@example.com", "+61400000401", "customer")
		newJob(t, pool, customer)

		_, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, customer)
		if err == nil {
			t.Fatal("the account was deleted, taking its jobs with it")
		}
		if !strings.Contains(err.Error(), "fk_jobs_customer") {
			t.Errorf("expected ON DELETE RESTRICT to refuse it, got: %v", err)
		}
	})
}

// TestJobStartsAsADraft pins the column default.
//
// Docs/02 §2 has exactly one entry point into the lifecycle, and every path out of Draft is a
// transition. A job created at any other status would have skipped one.
func TestJobStartsAsADraft(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "draft@example.com", "+61400000402", "customer")
	id := newJob(t, pool, customer)

	var status string
	if err := pool.QueryRow(t.Context(),
		`SELECT status FROM jobs WHERE id = $1`, id).Scan(&status); err != nil {
		t.Fatalf("reading the job back: %v", err)
	}
	if status != string(jobs.StatusDraft) {
		t.Errorf("a new job is %q, want %q", status, jobs.StatusDraft)
	}
}

// TestJobUpdatedAtIsMaintained proves the trigger is attached to this table, rather than relying
// on the sweep in schema_test.go noticing later.
//
// The write is a no-op on purpose: jobs has no mutable column of its own yet, and status is not
// one anybody may set. Any UPDATE fires the trigger, which is what is being checked.
func TestJobUpdatedAtIsMaintained(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newUser(t, pool, "touched-job@example.com", "+61400000403", "customer")
	id := newJob(t, pool, customer)

	var before time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM jobs WHERE id = $1`, id).Scan(&before); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	// A separate statement, and therefore a separate transaction: now() is transaction start
	// time, so within one transaction the timestamp would legitimately not move.
	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET customer_id = customer_id WHERE id = $1`, id); err != nil {
		t.Fatalf("updating: %v", err)
	}

	var after time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT updated_at FROM jobs WHERE id = $1`, id).Scan(&after); err != nil {
		t.Fatalf("reading updated_at: %v", err)
	}

	if !after.After(before) {
		t.Errorf("updated_at did not move on update (%s then %s); is the trigger attached?", before, after)
	}
}

// TestJobTimestampsCarryTheirZone is Docs/10 §3.3's "timestamptz always, never timestamp".
//
// A plain timestamp column accepts every write and reads back correctly on the machine that
// wrote it. It goes wrong only across a zone boundary, months later, in a way that looks like a
// data-entry mistake rather than a schema one — and this table will carry pickup windows read by
// a driver in another state.
func TestJobTimestampsCarryTheirZone(t *testing.T) {
	pool := pgtest.DB(t)

	rows, err := pool.Query(t.Context(), `
		SELECT column_name, data_type
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'jobs'
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
			t.Errorf("jobs.%s is %s, want timestamp with time zone", name, dataType)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterating: %v", err)
	}
	if found != 2 {
		t.Errorf("found %d timestamp columns, want 2 (created_at, updated_at)", found)
	}
}

// TestJobForeignKeyIsIndexed is the other half of Docs/10 §3.3's foreign key rule.
//
// An unindexed foreign key is invisible until the table is large: every delete on users takes a
// sequential scan of this one to check the constraint, and so does SHIP-66's "my jobs" list.
func TestJobForeignKeyIsIndexed(t *testing.T) {
	pool := pgtest.DB(t)

	var indexes int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t     ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'jobs' AND a.attname = 'customer_id'`,
	).Scan(&indexes); err != nil {
		t.Fatalf("looking for the index: %v", err)
	}
	if indexes == 0 {
		t.Error("jobs.customer_id carries a foreign key and no index leading with it")
	}
}

// TestJobMigrationIsInTheJobsBlock names the number, because the block scheme is only worth
// having if something checks that a migration landed inside its range.
func TestJobMigrationIsInTheJobsBlock(t *testing.T) {
	block, ok := migrations.BlockContaining(400)
	if !ok {
		t.Fatal("version 400 falls in no reserved block")
	}
	if block.Domain != "jobs" {
		t.Errorf("version 400 is in the %q block, want jobs", block.Domain)
	}
}
