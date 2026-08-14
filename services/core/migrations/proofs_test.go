package migrations_test

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
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

// --- SHIP-116, the reasoned exception ------------------------------------------------------------

// storeException writes one exception row and returns the error, so a caller can assert either way.
func storeException(t *testing.T, pool *pgxpool.Pool, job, milestone uuid.UUID, reason string) error {
	t.Helper()
	return storeExceptionIn(t, pool, job, milestone, reason)
}

// storeExceptionIn is the same write inside whatever the caller is holding.
//
// A db.Runner rather than a pool, because milestones_test.go's offline batch needs one *inside its
// transaction*: SHIP-118's constraint trigger is deferred to COMMIT, so a delivered milestone and
// its evidence have to arrive together or the batch is refused — which is also exactly what a sync
// worker draining a driver's queue does.
func storeExceptionIn(t *testing.T, r db.Runner, job, milestone uuid.UUID, reason string) error {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	_, err = r.Exec(t.Context(), `
		INSERT INTO proofs (id, job_id, milestone_id, exception_reason)
		VALUES ($1, $2, $3, $4)`, id, job, milestone, reason)
	return err
}

// TestProofExceptionConstraintMatchesTheGoConstants is Docs/10 §3.4's pairing, in both directions.
//
// A reason the database accepts and Go has no constant for is a value nothing can display; one Go
// has and the database refuses is a reason a driver can select and never record. The three
// literals are named as well as compared, because both lists could drift together if somebody
// renamed one and made the other match — Docs/01 §4.4 is the authority for what they mean.
func TestProofExceptionConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	if len(delivery.ProofExceptionReasons) != 3 {
		t.Errorf("delivery.ProofExceptionReasons holds %d values; Docs/01 §4.4 lists three",
			len(delivery.ProofExceptionReasons))
	}

	inGo := map[string]bool{}
	for _, r := range delivery.ProofExceptionReasons {
		inGo[string(r)] = true
	}
	if len(inGo) != len(delivery.ProofExceptionReasons) {
		t.Errorf("delivery.ProofExceptionReasons contains a duplicate: %d constants, %d distinct",
			len(delivery.ProofExceptionReasons), len(inGo))
	}

	inDatabase := constraintLiterals(t, pool, "ck_proofs_exception_reason")

	for r := range inGo {
		if !inDatabase[r] {
			t.Errorf("Go has the exception reason %q and ck_proofs_exception_reason does not "+
				"permit it; a driver could select it and never record it", r)
		}
	}
	for r := range inDatabase {
		if !inGo[r] {
			t.Errorf("ck_proofs_exception_reason permits %q and Go has no constant for it", r)
		}
	}

	for _, want := range []string{"recipient_objected", "camera_unavailable", "location_unsafe"} {
		if !inDatabase[want] {
			t.Errorf("ck_proofs_exception_reason does not permit %q", want)
		}
	}

	// There is deliberately no catch-all. A reason nobody can group is a moderation queue nobody
	// can triage (Docs/04 §5), and the driver's own words go in milestones.reason.
	for _, notAReason := range []string{"other", "unknown", ""} {
		if inDatabase[notAReason] {
			t.Errorf("ck_proofs_exception_reason permits %q, which is not one of Docs/01 §4.4's "+
				"three", notAReason)
		}
	}
}

// TestEvidenceIsAPhotographOrAReasonAndNeverBothOrNeither is ck_proofs_photograph_or_exception, and
// it is the row-level half of CLAUDE.md's invariant.
//
// It is written here rather than in internal/delivery because **no Go path in this service can
// produce three of the four rows below**. Service.RecordMilestone refuses both-at-once before the
// insert and never assembles a partial photograph, which is exactly the point Docs/06 §4.1 makes:
// a rule that holds because one function is careful stops holding when a second function is
// written. What is asserted here is what the database refuses whoever is asking.
func TestEvidenceIsAPhotographOrAReasonAndNeverBothOrNeither(t *testing.T) {
	pool := pgtest.DB(t)

	job, milestone := aRecordedMilestone(t, pool, "proof-xor@example.com", "+61400002116")

	t.Run("a reasoned exception with no photograph is accepted", func(t *testing.T) {
		if err := storeException(t, pool, job, milestone, "recipient_objected"); err != nil {
			t.Fatalf("an exception row was refused: %v", err)
		}
	})

	// A second milestone, because uq_proofs_milestone would refuse the rows below for the wrong
	// reason on the one above.
	other, otherMilestone := aRecordedMilestone(t, pool, "proof-xor-b@example.com", "+61400002118")

	t.Run("a row with neither is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		if _, err := pool.Exec(t.Context(), `
			INSERT INTO proofs (id, job_id, milestone_id) VALUES ($1, $2, $3)`,
			id, other, otherMilestone); err == nil {
			t.Fatal("a proof row was written with neither a photograph nor a reason; a milestone " +
				"can carry evidence that is nothing at all")
		}
	})

	t.Run("a row with both is refused", func(t *testing.T) {
		id, _ := uuid.NewV7()
		if _, err := pool.Exec(t.Context(), `
			INSERT INTO proofs
				(id, job_id, milestone_id, object_key, content_type, content_length, etag,
				 exception_reason)
			VALUES ($1, $2, $3, 'proof/x/both', 'image/jpeg', 137402, '9f86d081', 'camera_unavailable')`,
			id, other, otherMilestone); err == nil {
			t.Fatal("a photograph was recorded alongside a reason there is none; Docs/01 §4.4's " +
				"exception is in place of the photograph, not beside it")
		}
	})

	t.Run("half a photograph is refused", func(t *testing.T) {
		// The failure 000603's four NOT NULLs used to refuse and 000604 had to keep refusing:
		// a key with no entity tag is a photograph the platform recorded half of, and it would
		// read as evidence in every query that only selects object_key.
		id, _ := uuid.NewV7()
		if _, err := pool.Exec(t.Context(), `
			INSERT INTO proofs (id, job_id, milestone_id, object_key, content_type, content_length)
			VALUES ($1, $2, $3, 'proof/x/half', 'image/jpeg', 137402)`,
			id, other, otherMilestone); err == nil {
			t.Fatal("a proof row was written with a key and no entity tag")
		}
	})
}

// TestTwoJobsMayRecordTheSameExceptionReason is the uniqueness that must *not* have been added.
//
// uq_proofs_object_key makes one object evidence for one claim. A *reason* is a selection from
// three, so every delivery in the country can honestly carry the same one, and an index that made
// it unique would refuse the second driver whose recipient objected.
func TestTwoJobsMayRecordTheSameExceptionReason(t *testing.T) {
	pool := pgtest.DB(t)

	first, firstMilestone := aRecordedMilestone(t, pool, "proof-dup-a@example.com", "+61400002120")
	second, secondMilestone := aRecordedMilestone(t, pool, "proof-dup-b@example.com", "+61400002122")

	if err := storeException(t, pool, first, firstMilestone, "location_unsafe"); err != nil {
		t.Fatalf("the first exception: %v", err)
	}
	if err := storeException(t, pool, second, secondMilestone, "location_unsafe"); err != nil {
		t.Fatalf("a second delivery could not record the same reason: %v", err)
	}
}

// TestAnExceptionIsAppendOnlyToo, because it is evidence exactly as a photograph is.
//
// Docs/04 §7 has an administrator reviewing it in a dispute, and a reason that can be edited after
// somebody complains is a reason worth nothing.
func TestAnExceptionIsAppendOnlyToo(t *testing.T) {
	pool := pgtest.DB(t)

	job, milestone := aRecordedMilestone(t, pool, "proof-ao-x@example.com", "+61400002124")
	if err := storeException(t, pool, job, milestone, "camera_unavailable"); err != nil {
		t.Fatalf("recording an exception: %v", err)
	}

	if _, err := pool.Exec(t.Context(),
		`UPDATE proofs SET exception_reason = 'recipient_objected' WHERE milestone_id = $1`,
		milestone); err == nil {
		t.Fatal("a recorded reason was rewritten in place")
	}
}

// TestTheExceptionQueueIndexExists is what SHIP-117 and X-6 both start from.
//
// "Which jobs completed through the exception path" is the moderation queue's question (Docs/04 §5)
// and the one X-6 will be decided about. Partial, because the rows it selects are the rare ones.
func TestTheExceptionQueueIndexExists(t *testing.T) {
	pool := pgtest.DB(t)

	var partial bool
	if err := pool.QueryRow(t.Context(), `
		SELECT i.indpred IS NOT NULL
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		WHERE c.relname = 'idx_proofs_exception'`).Scan(&partial); err != nil {
		t.Fatalf("idx_proofs_exception is not there: %v", err)
	}
	if !partial {
		t.Error("idx_proofs_exception covers every row; almost every delivery is photographed and " +
			"an index over all of them is mostly rows nobody is asking about")
	}
}

// --- SHIP-118, the delivered milestone that has to have something behind it ------------------------

// TestADeliveredMilestoneCannotBeWrittenWithoutEvidence is CLAUDE.md's invariant, in the database.
//
// # Why this is here and not only in internal/delivery
//
// `Service.RecordMilestone` refuses it first, with a message a client can act on, and **that layer
// is the one a future writer does not inherit**. SHIP-121 gives the driver's portal a milestone
// endpoint, SHIP-113 rewrites the switch deciding what an administrative conflict does with one, and
// cmd/worker already applies transitions on a timer. This is what each of them meets.
//
// The insert itself succeeds and the **COMMIT** is what fails, which is the whole reason the trigger
// is deferred: a proof row points at its milestone, so it can only be written second. The test
// therefore has to drive a transaction by hand rather than a statement.
func TestADeliveredMilestoneCannotBeWrittenWithoutEvidence(t *testing.T) {
	pool := pgtest.DB(t)

	job := newDeliveryJob(t, pool, "delivered-ck@example.com", "+61400002126")
	driver := assign(t, pool, job, "Priya Sharma", "+61498765433")

	deliver := func(t *testing.T, evidence func(tx pgx.Tx, milestone uuid.UUID) error) error {
		t.Helper()

		tx, err := pool.Begin(t.Context())
		if err != nil {
			t.Fatalf("beginning: %v", err)
		}
		defer func() { _ = tx.Rollback(t.Context()) }()

		id, _ := uuid.NewV7()
		if _, err := tx.Exec(t.Context(), `
			INSERT INTO milestones
				(id, job_id, milestone, actor_type, actor_id, actor_recorded_at, recipient_name, delivery_note)
			VALUES ($1, $2, 'Delivered', 'driver', $3, now(), 'R. Chen', 'Left with reception')`, id, job, driver); err != nil {
			// The insert must *not* be what fails: the trigger is deferred precisely so that a
			// milestone can exist before the row that points at it.
			t.Fatalf("inserting the delivered milestone: %v", err)
		}

		if evidence != nil {
			if err := evidence(tx, id); err != nil {
				t.Fatalf("writing the evidence: %v", err)
			}
		}
		return tx.Commit(t.Context())
	}

	t.Run("with nothing behind it, the commit is refused", func(t *testing.T) {
		err := deliver(t, nil)
		if err == nil {
			t.Fatal("a delivered milestone was committed with neither photo proof nor a recorded " +
				"exception; the invariant holds only for callers who remember to check")
		}
		if !strings.Contains(err.Error(), "photo proof or a recorded exception") {
			t.Errorf("the refusal does not say why: %v", err)
		}
	})

	t.Run("with a reasoned exception, it commits", func(t *testing.T) {
		if err := deliver(t, func(tx pgx.Tx, milestone uuid.UUID) error {
			return storeExceptionIn(t, tx, job, milestone, "location_unsafe")
		}); err != nil {
			t.Fatalf("a delivery evidenced by a reasoned exception was refused: %v", err)
		}
	})

	t.Run("and an ordinary milestone is untouched by any of it", func(t *testing.T) {
		// The `WHEN` clause, which is what keeps the other four free of a rule Docs/01 §4.4 puts
		// on one moment rather than on five.
		if _, err := record(t, pool, job, "In transit", "driver", driver, nil, time.Now().UTC()); err != nil {
			t.Fatalf("an unphotographed 'In transit' was refused: %v", err)
		}
	})
}
