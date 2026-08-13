package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-115's table, checked where its guarantees live.
//
// # Why these are here rather than in internal/delivery
//
// The domain's tests drive `Service.RecordMilestone`, which writes a proof row in the same
// transaction as the milestone it points at and therefore **cannot produce most of the states
// below**. That is the correct outcome and it is exactly why the constraints exist: a rule that only
// holds because one function is careful stops holding when a second function is written. What is
// asserted here is what the database refuses regardless of who is asking — which is Docs/06 §4.1's
// "a mock happily accepts a write that the actual constraint would reject", applied to this
// repository's own code as the thing that might one day be wrong.
//
// The sharpest of them is fk_proofs_milestone: a proof whose `job_id` names one job and whose
// milestone belongs to another reads perfectly, would put a photograph of somebody else's delivery
// on this job's timeline, and is the shape a mistyped identifier in a future writer produces.
//
// newUser, newJob and newDeliveryJob come from schema_test.go, jobs_test.go and
// driver_assignments_test.go, in this same external test package. `record` is milestones_test.go's.

// storeProof writes one proof row and returns its id and the error, so a caller can assert either.
func storeProof(t *testing.T, pool *pgxpool.Pool, job, milestone uuid.UUID, objectKey string) (uuid.UUID, error) {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	_, err = pool.Exec(t.Context(), `
		INSERT INTO proofs (id, job_id, milestone_id, object_key, content_type, content_length, etag)
		VALUES ($1, $2, $3, $4, 'image/jpeg', 137402, '9f86d081884c7d659a2feaa0c55ad015')`,
		id, job, milestone, objectKey)
	return id, err
}

// aRecordedMilestone is a job with one milestone on it, which is the least a proof row needs.
func aRecordedMilestone(t *testing.T, pool *pgxpool.Pool, email, phone string) (job, milestone uuid.UUID) {
	t.Helper()

	job = newDeliveryJob(t, pool, email, phone)
	provider := newUser(t, pool, "p-"+email, phone+"1", "provider")

	milestone, err := record(t, pool, job, "Picked up", "provider", provider, nil, time.Now().UTC())
	if err != nil {
		t.Fatalf("recording a milestone: %v", err)
	}
	return job, milestone
}

// TestProofCannotNameOneJobAndAMilestoneOnAnother is the composite foreign key, and it is the
// constraint this table exists to make impossible to get wrong.
//
// `job_id` is carried on the row so that reading a job's proof is one filter rather than a join. Two
// separate foreign keys — one to `milestones`, one to `jobs` — would each hold while permitting
// exactly this row.
func TestProofCannotNameOneJobAndAMilestoneOnAnother(t *testing.T) {
	pool := pgtest.DB(t)

	first, milestone := aRecordedMilestone(t, pool, "proof-fk-a@example.com", "+61400002100")
	second := newDeliveryJob(t, pool, "proof-fk-b@example.com", "+61400002102")

	if _, err := storeProof(t, pool, second, milestone, "proof/"+second.String()+"/one"); err == nil {
		t.Fatal("a proof row named one job and a milestone recorded on another; a photograph of " +
			"one delivery can be put on a different customer's timeline")
	}

	// The pair that agrees is accepted, so the test above is not passing because everything is
	// refused.
	if _, err := storeProof(t, pool, first, milestone, "proof/"+first.String()+"/one"); err != nil {
		t.Fatalf("a proof row whose job and milestone agree was refused: %v", err)
	}
}

// TestProofRequiresARealMilestone is the other direction of the same key.
func TestProofRequiresARealMilestone(t *testing.T) {
	pool := pgtest.DB(t)

	job := newDeliveryJob(t, pool, "proof-fk-c@example.com", "+61400002104")
	stranger, _ := uuid.NewV7()

	if _, err := storeProof(t, pool, job, stranger, "proof/"+job.String()+"/one"); err == nil {
		t.Fatal("proof was recorded against a milestone that does not exist")
	}
}

// TestOneObjectIsProofOfOneThing is uq_proofs_object_key, and it is access control rather than
// tidiness: the same object recorded twice would be evidence for a delivery it was never taken at.
func TestOneObjectIsProofOfOneThing(t *testing.T) {
	pool := pgtest.DB(t)

	job, milestone := aRecordedMilestone(t, pool, "proof-uq-a@example.com", "+61400002106")
	other, otherMilestone := aRecordedMilestone(t, pool, "proof-uq-b@example.com", "+61400002108")

	key := "proof/" + job.String() + "/" + uuid.Must(uuid.NewV7()).String()
	if _, err := storeProof(t, pool, job, milestone, key); err != nil {
		t.Fatalf("the first proof: %v", err)
	}

	if _, err := storeProof(t, pool, other, otherMilestone, key); err == nil {
		t.Fatal("one object became proof of two milestones on two different jobs")
	}
}

// TestOneMilestoneCarriesAtMostOneProof is uq_proofs_milestone.
//
// A driver who photographs twice has recorded a second milestone — 000601 has no uniqueness on
// (job_id, milestone) precisely so they can, and Docs/02 §5 calls the failed pickup attempt an
// ordinary outcome. What this refuses is two answers to one claim.
func TestOneMilestoneCarriesAtMostOneProof(t *testing.T) {
	pool := pgtest.DB(t)

	job, milestone := aRecordedMilestone(t, pool, "proof-uq-m@example.com", "+61400002110")

	if _, err := storeProof(t, pool, job, milestone, "proof/"+job.String()+"/one"); err != nil {
		t.Fatalf("the first proof: %v", err)
	}
	if _, err := storeProof(t, pool, job, milestone, "proof/"+job.String()+"/two"); err == nil {
		t.Fatal("one milestone carries two photographs, so which one is its proof has two answers")
	}
}

// TestProofIsAppendOnly is the control 000003 puts on audit_log and 000601 on milestones.
//
// Proof is evidence and Docs/04 §7 has an administrator reviewing it in a dispute. A row that can be
// updated is a photograph that can be swapped after somebody complained, with nothing in the record
// to say it was.
func TestProofIsAppendOnly(t *testing.T) {
	pool := pgtest.DB(t)

	job, milestone := aRecordedMilestone(t, pool, "proof-ao@example.com", "+61400002112")
	key := "proof/" + job.String() + "/" + uuid.Must(uuid.NewV7()).String()

	id, err := storeProof(t, pool, job, milestone, key)
	if err != nil {
		t.Fatalf("recording proof: %v", err)
	}

	t.Run("it cannot be updated", func(t *testing.T) {
		_, err := pool.Exec(t.Context(),
			`UPDATE proofs SET object_key = $2 WHERE id = $1`, id, key+"-swapped")
		if err == nil {
			t.Fatal("a photograph was replaced in place")
		}
		if !strings.Contains(err.Error(), "append-only") {
			t.Errorf("the refusal does not say why: %v", err)
		}
	})

	t.Run("it cannot be deleted", func(t *testing.T) {
		if _, err := pool.Exec(t.Context(), `DELETE FROM proofs WHERE id = $1`, id); err == nil {
			t.Fatal("evidence was deleted")
		}
	})

	t.Run("and the job it belongs to cannot be deleted either", func(t *testing.T) {
		// ON DELETE RESTRICT plus the append-only trigger, which together are the reading of
		// Docs/05 §3.1 that 000401 and 000601 already take.
		if _, err := pool.Exec(t.Context(), `DELETE FROM jobs WHERE id = $1`, job); err == nil {
			t.Fatal("a job with proof on it was deleted, taking the evidence with it")
		}
	})
}

// TestProofConstraints covers what the columns will not hold.
//
// The size limit is deliberately *not* among them: STORAGE_MAX_UPLOAD_BYTES is configuration
// because it moves, and a CHECK holding a copy of it would refuse rows the running platform had
// just accepted. What is bounded here is what cannot become sensible at any setting.
func TestProofConstraints(t *testing.T) {
	pool := pgtest.DB(t)

	job, milestone := aRecordedMilestone(t, pool, "proof-ck@example.com", "+61400002114")

	for _, tc := range []struct {
		name   string
		column string
		value  any
	}{
		{"an empty object key", "object_key", ""},
		{"an empty content type", "content_type", ""},
		{"an empty entity tag", "etag", ""},
		{"a content type that is a paragraph", "content_type", strings.Repeat("a", 129)},
		{"no bytes at all", "content_length", 0},
		{"a negative length", "content_length", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			id, err := uuid.NewV7()
			if err != nil {
				t.Fatalf("generating an id: %v", err)
			}

			// Built as one statement per column rather than a full row per case, so a case
			// cannot pass because a *different* column was wrong.
			//nolint:gosec // tc.column is one of six literals above, not input.
			q := `INSERT INTO proofs
			          (id, job_id, milestone_id, object_key, content_type, content_length, etag)
			      VALUES ($1, $2, $3,
			              ` + valueOrDefault(tc.column, "object_key", `'proof/x/'||$1`) + `,
			              ` + valueOrDefault(tc.column, "content_type", `'image/jpeg'`) + `,
			              ` + valueOrDefault(tc.column, "content_length", `137402`) + `,
			              ` + valueOrDefault(tc.column, "etag", `'9f86d081'`) + `)`

			if _, err := pool.Exec(t.Context(), q, id, job, milestone, tc.value); err == nil {
				t.Fatalf("%s was accepted", tc.name)
			}
		})
	}
}

// valueOrDefault is `$4` for the column under test and the ordinary literal for the other three.
//
// A small helper rather than four `if`s inline, so that the table above reads as a list of things
// the database refuses.
func valueOrDefault(under, column, ordinary string) string {
	if under == column {
		return "$4"
	}
	return ordinary
}

// TestProofForeignKeyIsIndexed is Docs/10 §3.3's "every foreign key is indexed", on the column every
// read of this table filters by.
func TestProofForeignKeyIsIndexed(t *testing.T) {
	pool := pgtest.DB(t)

	var indexes int
	if err := pool.QueryRow(t.Context(), `
		SELECT count(*)
		FROM pg_index i
		JOIN pg_class t     ON t.oid = i.indrelid
		JOIN pg_attribute a ON a.attrelid = t.oid AND a.attnum = i.indkey[0]
		WHERE t.relname = 'proofs' AND a.attname = 'job_id'`,
	).Scan(&indexes); err != nil {
		t.Fatalf("looking for the index: %v", err)
	}
	if indexes == 0 {
		t.Error("proofs.job_id carries a foreign key and no index leading with it")
	}
}

// TestProofTimestampCarriesItsZone is Docs/10 §3.3's "timestamptz always".
func TestProofTimestampCarriesItsZone(t *testing.T) {
	assertTimestamptz(t, "proofs", "created_at")
}
