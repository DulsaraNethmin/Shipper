package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-163's table, checked where its guarantees live.
//
// Everything asserted below is a constraint or an index rather than application logic, which is
// where Docs/06 §4.1 puts the demonstration: "a mock happily accepts a write that the actual
// constraint would reject". The two partial unique indexes are the whole of the ticket's
// once-per-key and once-per-job promises, and neither can be exercised anywhere but here and in
// internal/admin's own tests against a real database.
//
// newUser comes from schema_test.go and newJob from jobs_test.go, both in this external test
// package. constraintLiterals and quotedLiteral come from milestones_test.go.

// raise inserts one dispute and returns its id and the error, so a caller can assert either.
//
// occurredAt and evidence are parameters rather than constants because they are what two of the
// checks below vary. Everything else is a fixed valid value: a test asserting a category constraint
// should not be able to fail because a description was too long.
func raise(
	t *testing.T,
	pool *pgxpool.Pool,
	job, complainant uuid.UUID,
	party, category string,
	occurredAt time.Time,
	evidence []string,
	key any,
) (uuid.UUID, error) {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	_, err = pool.Exec(t.Context(), `
		INSERT INTO disputes
			(id, job_id, complainant_id, complainant_party, category, description,
			 desired_outcome, occurred_at, evidence, idempotency_key)
		VALUES ($1, $2, $3, $4, $5, 'Two crates arrived staved in.', 'A record of the damage.',
		        $6, coalesce($7, '{}'::text[]), $8)`,
		id, job, complainant, party, category, occurredAt, evidence, key)
	return id, err
}

// settle resolves every open dispute on a job, the way `000804` requires one to be resolved.
//
// **All three columns, because `ck_disputes_resolution` binds them** (SHIP-164). These three tests
// each free `uq_disputes_open_per_job` in order to reach a *second* refusal, and each of them wrote
// `resolved_at` alone until that constraint existed — which is the wave-13 collision in miniature: a
// constraint added by one ticket refusing fixtures written by another, in a file the constraint's
// own branch had no reason to open. One helper rather than three copies, so the next column the
// resolution grows is one edit.
func settle(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, by uuid.UUID) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		UPDATE disputes
		SET resolved_at = now(), outcome = 'Delivery completed as agreed', resolved_by = $2
		WHERE job_id = $1 AND resolved_at IS NULL`, jobID, by); err != nil {
		t.Fatalf("resolving the disputes on %s: %v", jobID, err)
	}
}

// disputeFixture is a job, its customer, and the instant everything below is dated from.
func disputeFixture(t *testing.T, suffix string) (*pgxpool.Pool, uuid.UUID, uuid.UUID, time.Time) {
	t.Helper()

	pool := pgtest.DB(t)
	customer := newUser(t, pool, "dispute-"+suffix+"@example.com", "+6140000"+suffix, "customer")
	job := newJob(t, pool, customer)

	return pool, job, customer, time.Date(2026, 8, 11, 15, 40, 0, 0, time.UTC)
}

// TestDisputeCategoryConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing, in both directions.
//
// A category missing from the database is one a complainant can never file under; a category
// missing from Go is a value the database will accept and no code knows how to display.
//
// **The list itself is a reading rather than a quotation, and that is why the strings are named
// below as well as compared.** Docs/04 §7 asks intake to capture a category and enumerates none;
// these six are derived from Docs/02 §5's exception table plus an escape hatch. Naming them here is
// what makes a change to that reading visible as a change to this test rather than as two lists
// quietly drifting together.
func TestDisputeCategoryConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	if len(admin.Categories) != 6 {
		t.Errorf("admin.Categories holds %d values; the reading taken from Docs/02 §5 has six",
			len(admin.Categories))
	}

	inGo := map[string]bool{}
	for _, c := range admin.Categories {
		inGo[string(c)] = true
	}
	if len(inGo) != len(admin.Categories) {
		t.Errorf("admin.Categories contains a duplicate: %d constants, %d distinct values",
			len(admin.Categories), len(inGo))
	}

	inDatabase := constraintLiterals(t, pool, "ck_disputes_category")

	for c := range inGo {
		if !inDatabase[c] {
			t.Errorf("Go has the category %q and ck_disputes_category does not permit it; "+
				"a complainant could never file under it", c)
		}
	}
	for c := range inDatabase {
		if !inGo[c] {
			t.Errorf("ck_disputes_category permits %q and Go has no constant for it", c)
		}
	}

	for _, want := range []string{
		"Provider fails to arrive",
		"Goods differ from listing",
		"Customer unavailable",
		"Delivery is late",
		"Goods damaged or missing",
		"Other",
	} {
		if !inDatabase[want] {
			t.Errorf("ck_disputes_category does not permit %q", want)
		}
	}
}

// TestDisputePartyConstraintMatchesTheGoConstants is the same pairing for the complainant's side.
//
// The exclusion is the point. Docs/02 §2 permits "an eligible user/admin" to open a dispute, and
// this column records which *party to the delivery* the complainant was — so 'admin' does not
// belong in it. An administrator opening one on somebody's behalf is SHIP-164's, and it is recorded
// as an audit entry naming the administrator rather than as a complainant who was on the job.
func TestDisputePartyConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inGo := map[string]bool{}
	for _, p := range admin.Parties {
		inGo[string(p)] = true
	}

	inDatabase := constraintLiterals(t, pool, "ck_disputes_complainant_party")

	for p := range inGo {
		if !inDatabase[p] {
			t.Errorf("Go has the party %q and ck_disputes_complainant_party does not permit it", p)
		}
	}
	for p := range inDatabase {
		if !inGo[p] {
			t.Errorf("ck_disputes_complainant_party permits %q and Go has no constant for it", p)
		}
	}

	for _, notAParty := range []string{"admin", "driver", "system"} {
		if inDatabase[notAParty] {
			t.Errorf("ck_disputes_complainant_party permits %q, which is not a side of a "+
				"delivery somebody can be on", notAParty)
		}
	}
}

// TestOneOpenDisputePerJob is the index the ticket's "a job is frozen once" rests on.
//
// Docs/02 §3 has a dispute freeze automatic completion until an administrator resolves it, and a
// job cannot be frozen twice — two open disputes would be two things SHIP-164 could unfreeze the
// job by resolving, and it would have to pick.
//
// The second half is what makes it a *partial* index rather than a unique constraint: once the
// first is resolved the job can be disputed again, which is the ordinary shape of a delivery that
// goes wrong twice.
func TestOneOpenDisputePerJob(t *testing.T) {
	pool, job, customer, at := disputeFixture(t, "01")

	if _, err := raise(t, pool, job, customer, "customer", "Goods damaged or missing", at, nil, "key-1"); err != nil {
		t.Fatalf("raising the first dispute: %v", err)
	}

	if _, err := raise(t, pool, job, customer, "customer", "Delivery is late", at, nil, "key-2"); err == nil {
		t.Fatal("a second open dispute was accepted on the same job; uq_disputes_open_per_job " +
			"is what stops one job being frozen twice")
	}

	settle(t, pool, job, aModerator(t, pool, "dispute-index-01@example.com"))

	if _, err := raise(t, pool, job, customer, "customer", "Delivery is late", at, nil, "key-2"); err != nil {
		t.Fatalf("a job could not be disputed again after the first was resolved: %v", err)
	}
}

// TestOneDisputePerIdempotencyKey is the other index, and it is a different guarantee.
//
// SHIP-15's middleware replays a stored response out of Redis for as long as its entry lives; this
// refuses the second row permanently, which is what is still true when a phone reconnects after the
// entry has expired. The scope is per job, so one client reusing a key across two jobs has made two
// requests that both deserve to succeed.
func TestOneDisputePerIdempotencyKey(t *testing.T) {
	pool, job, customer, at := disputeFixture(t, "02")

	if _, err := raise(t, pool, job, customer, "customer", "Goods damaged or missing", at, nil, "same-key"); err != nil {
		t.Fatalf("raising the first dispute: %v", err)
	}

	// Resolved, so that the refusal below can only be the idempotency index rather than the
	// one-open-per-job index answering first.
	settle(t, pool, job, aModerator(t, pool, "dispute-index-02@example.com"))

	if _, err := raise(t, pool, job, customer, "customer", "Delivery is late", at, nil, "same-key"); err == nil {
		t.Fatal("one idempotency key raised two disputes on one job")
	}

	// The same key against a *different* job is a different action and must succeed. This is the
	// half a subject-scoped or global index would get wrong.
	other := newJob(t, pool, customer)
	if _, err := raise(t, pool, other, customer, "customer", "Delivery is late", at, nil, "same-key"); err != nil {
		t.Fatalf("the same key on a different job was refused: %v", err)
	}
}

// TestAKeylessDisputeIsOutsideTheIdempotencyIndex records why that index is partial.
//
// A dispute opened by an administrator (SHIP-164, SHIP-165) arrives through no client request and
// has no key behind it. NULLs are distinct to a btree, so two keyless disputes on one job do not
// collide there — they collide on uq_disputes_open_per_job instead, which is the correct rule for
// them. internal/admin refuses a keyless intake in Go for exactly this reason: the guarantee would
// be absent rather than broken.
func TestAKeylessDisputeIsOutsideTheIdempotencyIndex(t *testing.T) {
	pool, job, customer, at := disputeFixture(t, "03")

	if _, err := raise(t, pool, job, customer, "customer", "Other", at, nil, nil); err != nil {
		t.Fatalf("raising a keyless dispute: %v", err)
	}
	settle(t, pool, job, aModerator(t, pool, "dispute-index-03@example.com"))
	if _, err := raise(t, pool, job, customer, "customer", "Other", at, nil, nil); err != nil {
		t.Fatalf("a second keyless dispute was refused by the idempotency index, which is "+
			"partial precisely so that it is not: %v", err)
	}
}

// TestTheIntakeFieldsAreRequired holds the columns to Docs/04 §7's list.
//
// Six of the seven fields it names are NOT NULL columns, and the seventh — evidence — is NOT NULL
// with a default because "nothing described" is a list of length zero rather than an absence. A
// migration that made any of them nullable would let a dispute be filed that an administrator
// cannot act on, and nothing in Go would notice.
func TestTheIntakeFieldsAreRequired(t *testing.T) {
	pool, job, customer, at := disputeFixture(t, "04")

	for _, field := range []string{"category", "description", "desired_outcome", "occurred_at", "evidence"} {
		var nullable bool
		if err := pool.QueryRow(t.Context(), `
			SELECT is_nullable = 'YES' FROM information_schema.columns
			WHERE table_name = 'disputes' AND column_name = $1`, field).Scan(&nullable); err != nil {
			t.Fatalf("reading whether disputes.%s is nullable: %v", field, err)
		}
		if nullable {
			t.Errorf("disputes.%s is nullable; Docs/04 §7 names it as an intake field", field)
		}
	}

	// And the two the platform supplies, which are equally not optional.
	for _, field := range []string{"job_id", "complainant_id", "complainant_party"} {
		var nullable bool
		if err := pool.QueryRow(t.Context(), `
			SELECT is_nullable = 'YES' FROM information_schema.columns
			WHERE table_name = 'disputes' AND column_name = $1`, field).Scan(&nullable); err != nil {
			t.Fatalf("reading whether disputes.%s is nullable: %v", field, err)
		}
		if nullable {
			t.Errorf("disputes.%s is nullable", field)
		}
	}

	// Evidence defaults to the empty list rather than NULL, so a reader never has to tell the
	// two apart.
	var evidence []string
	id, err := raise(t, pool, job, customer, "customer", "Other", at, nil, "defaults")
	if err != nil {
		t.Fatalf("raising: %v", err)
	}
	if err := pool.QueryRow(t.Context(), `SELECT evidence FROM disputes WHERE id = $1`, id).Scan(&evidence); err != nil {
		t.Fatalf("reading evidence back: %v", err)
	}
	if evidence == nil || len(evidence) != 0 {
		t.Errorf("evidence = %v, want an empty list", evidence)
	}
}

// TestEvidenceIsBounded checks the constraint that could not be written the obvious way.
//
// A per-item length check wants to be `unnest`, and a CHECK constraint may not contain a subquery —
// which is a real failure this migration hit rather than a hypothetical. What replaced it bounds
// the list, refuses an empty or NULL entry, and bounds the total; Go bounds each item where it can
// report which one was wrong.
func TestEvidenceIsBounded(t *testing.T) {
	pool, job, customer, at := disputeFixture(t, "05")

	tooMany := make([]string, 21)
	for i := range tooMany {
		tooMany[i] = "a reference"
	}
	if _, err := raise(t, pool, job, customer, "customer", "Other", at, tooMany, "too-many"); err == nil {
		t.Error("a list of twenty-one evidence references was accepted")
	}

	if _, err := raise(t, pool, job, customer, "customer", "Other", at, []string{""}, "empty"); err == nil {
		t.Error("an empty evidence reference was accepted; it is a blank form row, not evidence")
	}

	huge := make([]string, 5)
	for i := range huge {
		huge[i] = strings.Repeat("x", 600)
	}
	if _, err := raise(t, pool, job, customer, "customer", "Other", at, huge, "huge"); err == nil {
		t.Error("three thousand characters of evidence were accepted")
	}

	if _, err := raise(t, pool, job, customer, "customer", "Other", at,
		[]string{"Photographed the crates at the depot", "The driver's message of 11/08"}, "ok"); err != nil {
		t.Fatalf("an ordinary pair of references was refused: %v", err)
	}
}

// TestADisputeOutlivesNothingAndBlocksDeletion is Docs/10 §3.3's ON DELETE RESTRICT, both ways.
//
// A job with a dispute on it cannot be deleted, and neither can the account that raised one. That
// is the same reading of Docs/05 §3.1 that job_status_history and milestones take: SHIP-171
// pseudonymises rather than deletes, so a cascade here would destroy the record of a complaint that
// is the reason the retention obligation exists.
func TestADisputeOutlivesNothingAndBlocksDeletion(t *testing.T) {
	pool, job, customer, at := disputeFixture(t, "06")

	if _, err := raise(t, pool, job, customer, "customer", "Other", at, nil, "restrict"); err != nil {
		t.Fatalf("raising: %v", err)
	}

	if _, err := pool.Exec(t.Context(), `DELETE FROM jobs WHERE id = $1`, job); err == nil {
		t.Error("a job with a dispute on it was deleted")
	}
	if _, err := pool.Exec(t.Context(), `DELETE FROM users WHERE id = $1`, customer); err == nil {
		t.Error("the account that raised a dispute was deleted")
	}
}
