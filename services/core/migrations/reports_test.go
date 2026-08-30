package migrations_test

import (
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-155a's schema — `000805`.
//
// Two closed vocabularies paired against their Go constants (Docs/10 §3.4), and the guarantees the
// service cannot make on its own: `ck_reports_subject`, which is the *Done when*'s "a report names a
// job or a message and never both", and the two bounds that have to hold for a connection which
// never went through the service at all.

// TestReportSubjectConstraintMatchesTheGoConstants.
//
// The failure it catches is quiet in both directions. A subject kind Go can name and the column
// refuses is a report that fails at run time, at the moment somebody is trying to say something is
// wrong. A kind the column accepts and Go cannot name is a row a hand-written INSERT could put in
// the table, which SHIP-156's queue would then read with a `subject_type` no client has a branch for.
func TestReportSubjectConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_reports_subject_type")

	for _, subject := range admin.ReportSubjects {
		if !inDatabase[subject.String()] {
			t.Errorf("admin.ReportSubjects has %q and ck_reports_subject_type refuses it, so a "+
				"report about one fails at the moment somebody raises it", subject)
		}
		delete(inDatabase, subject.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_reports_subject_type accepts %q and admin.ReportSubjects has no such "+
			"subject, so nothing in this service can write or read a report about one", leftover)
	}
}

// TestReportReasonConstraintMatchesTheGoConstants is the same pairing for the other enumeration.
//
// The list is derived rather than quoted — `000805` records which closed line each of the seven
// traces to — which makes this pairing more load-bearing than usual: there is no document to diff
// the column against, so the Go constants and the CHECK are the only two statements of the list and
// they have to agree with each other.
func TestReportReasonConstraintMatchesTheGoConstants(t *testing.T) {
	pool := pgtest.DB(t)

	inDatabase := constraintLiterals(t, pool, "ck_reports_reason")

	for _, reason := range admin.Reasons {
		if !inDatabase[reason.String()] {
			t.Errorf("admin.Reasons has %q and ck_reports_reason refuses it, so a report giving "+
				"that reason fails at run time", reason)
		}
		delete(inDatabase, reason.String())
	}
	for leftover := range inDatabase {
		t.Errorf("ck_reports_reason accepts %q and admin.Reasons has no such reason, so nothing "+
			"in this service can write or read one", leftover)
	}
}

// TestAReportNamesAJobOrAMessageAndNeverBoth is `ck_reports_subject`, which is the *Done when*.
//
// The clause is "a report names a job or a message and never both, so SHIP-156's queue can open the
// context the report is actually about", and it has two halves that fail differently:
//
//   - a report about the *job* carrying a message id is a row whose two columns disagree about what
//     it is about, and a queue reading either one alone would open a different thing;
//   - a report about a *message* carrying none is a row claiming a conversation it cannot name,
//     which is the one SHIP-156 could not open at all.
//
// Asserted against the table rather than against the service, because it is the table that has to
// hold for the operator at a `psql` prompt and the repair script — the two writers that never see
// [admin.ReportIntake].
func TestAReportNamesAJobOrAMessageAndNeverBoth(t *testing.T) {
	pool := pgtest.DB(t)

	customer, jobID, messageID := aReportableJob(t, pool, "subject")

	t.Run("a report about the job may not name a message", func(t *testing.T) {
		if err := insertReport(t, pool, jobID, "job", &messageID, customer); err == nil {
			t.Error("a report claiming to be about the job was accepted while naming a message; " +
				"SHIP-156 would open one of the two and there is no saying which")
		}
	})

	t.Run("a report about a message must name one", func(t *testing.T) {
		if err := insertReport(t, pool, jobID, "message", nil, customer); err == nil {
			t.Error("a report claiming to be about a message was accepted without naming one; " +
				"that is a queue entry nothing can open")
		}
	})

	t.Run("both legal shapes are accepted", func(t *testing.T) {
		if err := insertReport(t, pool, jobID, "job", nil, customer); err != nil {
			t.Errorf("a report about the job itself was refused: %v", err)
		}
		if err := insertReport(t, pool, jobID, "message", &messageID, customer); err != nil {
			t.Errorf("a report about a message on the job was refused: %v", err)
		}
	})
}

// TestAReportMustRecordSomething is `ck_reports_description`, from the side the Go check cannot
// cover.
//
// The service trims and refuses an empty description before it gets here. This is the half that
// holds for a connection which never went through the service — which is what makes it a constraint
// rather than a convention.
//
// **The newline case is the one that matters**, and it is here because `ck_admin_notes_body` was
// caught by it: `btrim(x)` with one argument strips spaces only, so a description of a single
// newline would satisfy the one-argument form while `strings.TrimSpace` refuses the same value. The
// two would then disagree exactly on the connection this constraint exists for.
func TestAReportMustRecordSomething(t *testing.T) {
	pool := pgtest.DB(t)

	customer, jobID, _ := aReportableJob(t, pool, "empty")

	for _, tc := range []struct{ name, description string }{
		{"empty", ""},
		{"nothing but spaces", "     "},
		{"nothing but a newline", "\n"},
		{"nothing but a tab", "\t"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := pool.Exec(t.Context(), `
				INSERT INTO reports
					(id, job_id, subject_type, reporter_id, reporter_party, reason, description)
				VALUES ($1, $2, 'job', $3, 'customer', 'Other', $4)`,
				uuid.Must(uuid.NewV7()), jobID, customer, tc.description)
			if err == nil {
				t.Errorf("a report with %s as its description was accepted; Docs/04 §6 step 2 "+
					"has somebody read this before choosing an outcome", tc.name)
			}
		})
	}
}

// TestAJobMayCarryManyReports is the difference from `disputes`, asserted rather than assumed.
//
// `uq_disputes_open_per_job` exists because a job is frozen once. **Nothing here freezes**, so there
// is deliberately no such index — two parties reporting one listing for two reasons are two things a
// moderator wants to see (`000805`). A partial unique index added later "for symmetry" would break
// this test, which is the point of writing it down.
func TestAJobMayCarryManyReports(t *testing.T) {
	pool := pgtest.DB(t)

	customer, jobID, messageID := aReportableJob(t, pool, "many")

	for _, tc := range []struct {
		name      string
		subject   string
		messageID *uuid.UUID
	}{
		{"the listing", "job", nil},
		{"the listing again, another reason", "job", nil},
		{"a message on it", "message", &messageID},
	} {
		if err := insertReport(t, pool, jobID, tc.subject, tc.messageID, customer); err != nil {
			t.Fatalf("a report about %s was refused: %v", tc.name, err)
		}
	}

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM reports WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("counting the reports on %s: %v", jobID, err)
	}
	if n != 3 {
		t.Errorf("the job carries %d reports, want 3; a job is disputed once and reported freely", n)
	}
}

// TestOneKeyRaisesOneReportPerJob is `uq_reports_idempotency`.
//
// The half of "a retry raises no second report" that is still true after Redis has forgotten the
// request — SHIP-15's middleware protects nothing that outlives a TTL, an eviction or a failover.
//
// The second half of the test is the scoping decision: the *same* key against a *different* job is
// two requests that both deserve to succeed, which is why the index leads with `job_id` (`000602`'s
// reasoning, carried by `000800` and again here).
func TestOneKeyRaisesOneReportPerJob(t *testing.T) {
	pool := pgtest.DB(t)

	customer, jobID, _ := aReportableJob(t, pool, "idem")

	const key = "one-key-one-report"

	if err := insertReportWithKey(t, pool, jobID, customer, key); err != nil {
		t.Fatalf("the first report was refused: %v", err)
	}
	if err := insertReportWithKey(t, pool, jobID, customer, key); err == nil {
		t.Error("the same key raised a second report on the same job; the middleware's entry " +
			"outlives nothing, and this index is what is left")
	}

	otherJob := newReportableDraft(t, pool, customer)
	if err := insertReportWithKey(t, pool, otherJob, customer, key); err != nil {
		t.Errorf("the same key was refused on a different job: %v; a client that reuses one key "+
			"across two jobs has made two requests that both deserve to succeed: %v", otherJob, err)
	}
}

// TestAReportKeepsWhatItPointsAt is the decision `000805` records against `admin_notes`.
//
// A note deliberately has no foreign key on its subject, because it outlives it. A report is the
// opposite case: it is *about* a live thing an administrator is going to open, and one pointing at a
// job or a message that is not there is unactionable. Both keys are ON DELETE RESTRICT.
func TestAReportKeepsWhatItPointsAt(t *testing.T) {
	pool := pgtest.DB(t)

	customer, jobID, messageID := aReportableJob(t, pool, "restrict")

	t.Run("a report cannot name a job that does not exist", func(t *testing.T) {
		if err := insertReport(t, pool, uuid.New(), "job", nil, customer); err == nil {
			t.Error("a report was accepted against a job that is not there")
		}
	})

	t.Run("a report cannot name a message that does not exist", func(t *testing.T) {
		absent := uuid.New()
		if err := insertReport(t, pool, jobID, "message", &absent, customer); err == nil {
			t.Error("a report was accepted against a message that is not there")
		}
	})

	t.Run("a reported message cannot then be deleted", func(t *testing.T) {
		if err := insertReport(t, pool, jobID, "message", &messageID, customer); err != nil {
			t.Fatalf("reporting the message: %v", err)
		}
		if _, err := pool.Exec(t.Context(),
			`DELETE FROM job_messages WHERE id = $1`, messageID); err == nil {
			t.Error("a reported message was deleted out from under the report; the queue entry " +
				"would name a conversation nobody can open")
		}
	})
}

// --- fixtures ---------------------------------------------------------------------------------

// aReportableJob is a customer, a job they own, and one message on it — the three things every
// report needs to point at. It answers the customer, the job and the message.
//
// The job stays at Draft. **Nothing about a report depends on the job's status**, which is the whole
// of the modelling decision `000805` records: Docs/02 §2 has no transition for a report, so there is
// no status a report has to be raised from and none it moves the job to. A fixture that published
// and awarded the job would suggest otherwise.
func aReportableJob(t *testing.T, pool *pgxpool.Pool, tag string) (customer, jobID, messageID uuid.UUID) {
	t.Helper()

	customer = aUser(t, pool, "report-"+tag+"-customer@example.com", "customer")
	provider := aUser(t, pool, "report-"+tag+"-provider@example.com", "provider")
	jobID = newReportableDraft(t, pool, customer)

	messageID = uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO job_messages (id, job_id, provider_id, sent_by, body)
		VALUES ($1, $2, $3, 'provider', 'Can you confirm what is actually in the crates?')`,
		messageID, jobID, provider); err != nil {
		t.Fatalf("inserting a message on %s: %v", jobID, err)
	}
	return customer, jobID, messageID
}

// aUser inserts a users row with the role the fixture needs.
func aUser(t *testing.T, pool *pgxpool.Pool, email, role string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())

	// The phone is taken from the *last* group of the identifier, which is the random one.
	// A UUIDv7's leading hex is a millisecond timestamp, so two users made in the same test
	// share it — and `uq_users_phone` catches that rather than the fixture noticing.
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', $4)`,
		id, email, "04"+id.String()[24:], role); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

// newReportableDraft inserts a job at Draft, the only status `000402` lets one be created at.
func newReportableDraft(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, id, customer); err != nil {
		t.Fatalf("inserting a job: %v", err)
	}
	return id
}

// insertReport writes one row directly, which is what an operator or a repair script would do.
//
// Deliberately not through the service: every assertion in this file is about what the *table*
// refuses, and a write that went through [admin.Reports] would be testing the Go checks a second
// time and the constraints not at all.
func insertReport(
	t *testing.T,
	pool *pgxpool.Pool,
	jobID uuid.UUID,
	subject string,
	messageID *uuid.UUID,
	reporter uuid.UUID,
) error {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO reports
			(id, job_id, subject_type, message_id, reporter_id, reporter_party, reason, description)
		VALUES ($1, $2, $3, $4, $5, 'customer', 'Other', 'Something is wrong with this.')`,
		uuid.Must(uuid.NewV7()), jobID, subject, messageID, reporter)
	return err
}

// insertReportWithKey is [insertReport] carrying an idempotency key, for the index that needs one.
func insertReportWithKey(
	t *testing.T,
	pool *pgxpool.Pool,
	jobID, reporter uuid.UUID,
	key string,
) error {
	t.Helper()

	_, err := pool.Exec(t.Context(), `
		INSERT INTO reports
			(id, job_id, subject_type, reporter_id, reporter_party, reason, description,
			 idempotency_key)
		VALUES ($1, $2, 'job', $3, 'customer', 'Other', 'Something is wrong with this.', $4)`,
		uuid.Must(uuid.NewV7()), jobID, reporter, key)
	return err
}
