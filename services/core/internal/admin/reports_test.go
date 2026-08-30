package admin

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-155a against a real PostgreSQL, for service_test.go's reason.
//
// `uq_reports_idempotency` is a *partial* unique index and `ck_reports_subject` is a CHECK across
// two columns; a repository interface would let every test below pass while the real rules were
// being broken (Docs/06 §4.1).
//
// The fixtures are service_test.go's — [newAccount], [newDraft], [acceptBid], [deliveredJob] and
// [testParties] — because a report is raised against exactly the job a dispute is, by exactly the
// same two parties. What this file adds is a message to point at.

// testMessages is admin.JobMessages over `job_messages`, as cmd/api reads it.
//
// The production adapter is jobMessageLookup in cmd/api/routes_admin.go and this is the same
// statement; the pair is exercised end to end by the binary rather than by a Go test, for the reason
// service_test.go's header records — cmd/api has no database.
type testMessages struct{}

func (testMessages) MessageOnJob(ctx context.Context, r db.Runner, jobID, messageID uuid.UUID) (bool, error) {
	const q = `SELECT EXISTS (SELECT 1 FROM job_messages WHERE id = $1 AND job_id = $2)`

	var onJob bool
	if err := r.QueryRow(ctx, q, messageID, jobID).Scan(&onJob); err != nil {
		return false, err
	}
	return onJob, nil
}

// staticMessages answers with one outcome, for the cases no fixture can produce.
type staticMessages struct {
	onJob bool
	err   error
}

func (s staticMessages) MessageOnJob(context.Context, db.Runner, uuid.UUID, uuid.UUID) (bool, error) {
	return s.onJob, s.err
}

// --- fixtures ---------------------------------------------------------------------------------

func newTestReports() *Reports { return NewReports(testParties{}, testMessages{}) }

// aMessageOn inserts one message into the job's conversation and answers its identifier.
func aMessageOn(t *testing.T, pool *pgxpool.Pool, jobID, providerID uuid.UUID) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO job_messages (id, job_id, provider_id, sent_by, body)
		VALUES ($1, $2, $3, 'provider', 'Cash on the day and we can skip the paperwork.')`,
		id, jobID, providerID); err != nil {
		t.Fatalf("inserting a message on %s: %v", jobID, err)
	}
	return id
}

// goodReport is an intake with nothing wrong with it, which each test then varies one field of.
//
// The description carries a deliberate line break and stray surrounding whitespace. Both are
// asserted on the way out: the whitespace goes and the line break stays, which is the distinction
// [ReportIntake.normalise] exists to make — a moderator reads this field, and four paragraphs
// flattened into one wall of text is the thing that makes it hard to.
func goodReport(key string) ReportIntake {
	return ReportIntake{
		Subject:     ReportSubjectJob,
		Reason:      ReasonProhibitedGoods,
		Description: "  The load is described as garden supplies.\n\nThe photographs show gas cylinders. ",
		Key:         key,
	}
}

// reportCount is how many rows the table holds for a job, whatever the service reported.
func reportCount(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM reports WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("counting reports on %s: %v", jobID, err)
	}
	return n
}

// --- the acceptance criterion -----------------------------------------------------------------

// TestACustomerReportsAJobRecordingReporterSubjectAndMoment is SHIP-155a's *Done when*, clause by
// clause.
//
// "The report records its reporter, its subject and the moment it was made." All three are read back
// off the row rather than off what the service returned, because what the service returned is what
// is under test.
func TestACustomerReportsAJobRecordingReporterSubjectAndMoment(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "report-customer@example.com", "0400000155", "customer")
	provider := newAccount(t, pool, "report-provider@example.com", "0400000156", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	report, raised, err := newTestReports().Raise(
		t.Context(), pool, customer, jobID, goodReport("report-one"))
	if err != nil {
		t.Fatalf("raising a report: %v", err)
	}
	if !raised {
		t.Fatal("the first report on a job reported that it had already been raised")
	}

	var (
		gotJob         uuid.UUID
		gotSubject     string
		gotMessage     *uuid.UUID
		gotReporter    uuid.UUID
		gotParty       string
		gotReason      string
		gotDescription string
		gotCreated     any
	)
	if err := pool.QueryRow(t.Context(), `
		SELECT job_id, subject_type, message_id, reporter_id, reporter_party, reason,
		       description, created_at
		FROM reports WHERE id = $1`, report.ID).Scan(
		&gotJob, &gotSubject, &gotMessage, &gotReporter, &gotParty, &gotReason,
		&gotDescription, &gotCreated); err != nil {
		t.Fatalf("reading the report back: %v", err)
	}

	// "its subject" — the job, and no message beside it.
	if gotJob != jobID {
		t.Errorf("the report is against %s, want %s", gotJob, jobID)
	}
	if gotSubject != string(ReportSubjectJob) {
		t.Errorf("subject_type is %q, want %q", gotSubject, ReportSubjectJob)
	}
	if gotMessage != nil {
		t.Errorf("a report about the job named message %s", *gotMessage)
	}

	// "its reporter" — the account, and the side the *platform* put them on.
	if gotReporter != customer {
		t.Errorf("the reporter is %s, want %s", gotReporter, customer)
	}
	if gotParty != string(PartyCustomer) {
		t.Errorf("reporter_party is %q, want %q; it is resolved from the job and its accepted "+
			"bid, never from the request", gotParty, PartyCustomer)
	}

	if gotReason != string(ReasonProhibitedGoods) {
		t.Errorf("reason is %q, want %q", gotReason, ReasonProhibitedGoods)
	}

	// "the moment it was made" — present, and the only instant the row carries.
	if report.CreatedAt.IsZero() {
		t.Error("the report came back with no moment on it")
	}

	// normalise trims the ends and keeps the structure.
	const want = "The load is described as garden supplies.\n\nThe photographs show gas cylinders."
	if gotDescription != want {
		t.Errorf("the description stored is %q, want %q; the surrounding whitespace goes and the "+
			"line breaks stay", gotDescription, want)
	}
}

// TestReportingAJobMovesNoStatus is the modelling decision, asserted rather than asserted-about.
//
// **The clause is "moves no job status".** Docs/02 §2 has no transition for a report and Docs/04 §6
// puts the outcome in an administrator's hands, so a report that froze a job would hand either party
// a unilateral freeze over the other's delivery.
//
// The status is read off the column before and after, and the history table is counted too: a
// transition that happened and was then reversed would leave the first assertion true and the second
// one false.
func TestReportingAJobMovesNoStatus(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "nomove-customer@example.com", "0400000157", "customer")
	provider := newAccount(t, pool, "nomove-provider@example.com", "0400000158", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	before := jobStatus(t, pool, jobID)
	historyBefore := historyRows(t, pool, jobID)

	if _, _, err := newTestReports().Raise(
		t.Context(), pool, customer, jobID, goodReport("nomove")); err != nil {
		t.Fatalf("raising a report: %v", err)
	}

	if after := jobStatus(t, pool, jobID); after != before {
		t.Errorf("reporting the job moved it from %q to %q; Docs/02 §2 has no transition for a "+
			"report, and one that froze a job would be a denial of service dressed as moderation",
			before, after)
	}
	if after := historyRows(t, pool, jobID); after != historyBefore {
		t.Errorf("reporting the job wrote %d job_status_history rows; a report is not a "+
			"transition", after-historyBefore)
	}
}

// TestTheAwardedProviderMayAlsoReport is the other half of "a customer or provider".
func TestTheAwardedProviderMayAlsoReport(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "prov-report-customer@example.com", "0400000159", "customer")
	provider := newAccount(t, pool, "prov-report-provider@example.com", "0400000160", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	report, raised, err := newTestReports().Raise(
		t.Context(), pool, provider, jobID, goodReport("provider-report"))
	if err != nil {
		t.Fatalf("the awarded provider could not report the job: %v", err)
	}
	if !raised {
		t.Fatal("the provider's report reported that it had already been raised")
	}
	if report.ReporterParty != PartyProvider {
		t.Errorf("the provider was recorded as %q", report.ReporterParty)
	}
}

// TestAStrangerCannotReportAJobTheyAreNotOn is the *Done when*'s last clause.
//
// "A party cannot report a job they are not on, because the platform resolves which side they were
// from the job and its accepted bid rather than taking it from the request."
//
// Two strangers, and the second is the one worth having: a provider who **bid and lost** is a
// provider, and `users.role` would call them one. Only the accepted bid makes somebody the provider
// on a job, which is why the check reads the award rather than the role.
//
// Nothing is written in either case, asserted by counting the table rather than by trusting the
// error.
func TestAStrangerCannotReportAJobTheyAreNotOn(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "stranger-customer@example.com", "0400000161", "customer")
	provider := newAccount(t, pool, "stranger-provider@example.com", "0400000162", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	otherCustomer := newAccount(t, pool, "stranger-other-c@example.com", "0400000163", "customer")
	losingProvider := newAccount(t, pool, "stranger-other-p@example.com", "0400000164", "provider")

	for _, tc := range []struct {
		name    string
		account uuid.UUID
	}{
		{"a customer with no connection to the job", otherCustomer},
		{"a provider who is not the one awarded it", losingProvider},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := newTestReports().Raise(
				t.Context(), pool, tc.account, jobID, goodReport("stranger-"+tc.name))
			if !errors.Is(err, ErrNotAParty) {
				t.Fatalf("got %v, want ErrNotAParty", err)
			}
		})
	}

	t.Run("and a job that does not exist answers the same way", func(t *testing.T) {
		_, _, err := newTestReports().Raise(
			t.Context(), pool, customer, uuid.New(), goodReport("absent-job"))
		if !errors.Is(err, ErrNotAParty) {
			t.Fatalf("got %v, want ErrNotAParty; telling a caller which of the two it was would "+
				"disclose that somebody else's job exists", err)
		}
	})

	if n := reportCount(t, pool, jobID); n != 0 {
		t.Errorf("%d reports were written by callers who are not party to the job", n)
	}
}

// TestReportingAMessageNamesTheMessageAndNotTheJob is the subject half of the *Done when*.
//
// "A report names a job or a message and never both, so SHIP-156's queue can open the context the
// report is actually about." The row carries the job as context and the message as the subject, and
// [Report.About] is the one place that reading lives.
func TestReportingAMessageNamesTheMessageAndNotTheJob(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "msg-customer@example.com", "0400000165", "customer")
	provider := newAccount(t, pool, "msg-provider@example.com", "0400000166", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	messageID := aMessageOn(t, pool, jobID, provider)

	in := goodReport("message-report")
	in.Subject = ReportSubjectMessage
	in.MessageID = messageID
	in.Reason = ReasonOffPlatform

	report, raised, err := newTestReports().Raise(t.Context(), pool, customer, jobID, in)
	if err != nil {
		t.Fatalf("reporting a message: %v", err)
	}
	if !raised {
		t.Fatal("the first report on a message reported that it had already been raised")
	}

	if report.Subject != ReportSubjectMessage {
		t.Errorf("the subject is %q, want %q", report.Subject, ReportSubjectMessage)
	}
	if report.MessageID != messageID {
		t.Errorf("the report names message %s, want %s", report.MessageID, messageID)
	}
	if report.JobID != jobID {
		t.Errorf("the report lost the job context: %s, want %s", report.JobID, jobID)
	}
	if report.About() != messageID {
		t.Errorf("About() is %s, want the message %s — it is what SHIP-156 opens",
			report.About(), messageID)
	}
}

// TestAMessageOnAnotherJobIsRefused is the check no foreign key can make.
//
// `fk_reports_message` establishes that the message exists. That it is on *this* job is a comparison
// between two tables, which is why [JobMessages] exists at all — and without it a reporter could
// file a message from one delivery into the queue under another.
func TestAMessageOnAnotherJobIsRefused(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "cross-customer@example.com", "0400000167", "customer")
	provider := newAccount(t, pool, "cross-provider@example.com", "0400000168", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	otherJob := deliveredJob(t, pool, customer, provider)
	elsewhere := aMessageOn(t, pool, otherJob, provider)

	in := goodReport("cross-job")
	in.Subject = ReportSubjectMessage
	in.MessageID = elsewhere

	_, _, err := newTestReports().Raise(t.Context(), pool, customer, jobID, in)
	if !errors.Is(err, ErrMessageNotOnJob) {
		t.Fatalf("got %v, want ErrMessageNotOnJob", err)
	}
	if n := reportCount(t, pool, jobID); n != 0 {
		t.Errorf("%d reports were written naming a message from another job", n)
	}

	t.Run("and a message that does not exist answers the same way", func(t *testing.T) {
		absent := goodReport("absent-message")
		absent.Subject = ReportSubjectMessage
		absent.MessageID = uuid.New()

		_, _, err := newTestReports().Raise(t.Context(), pool, customer, jobID, absent)
		if !errors.Is(err, ErrMessageNotOnJob) {
			t.Fatalf("got %v, want ErrMessageNotOnJob", err)
		}
	})
}

// TestThePartyCheckRunsBeforeTheMessageCheck is the disclosure decision in [Reports.Raise].
//
// Reversed, a stranger could send any message id against a job they have nothing to do with and
// learn from the answer whether that message exists and which job it is on. Asserted by giving the
// service a message port that would answer "yes" to anything: a stranger must still be refused as a
// stranger, which can only happen if the party check ran first.
func TestThePartyCheckRunsBeforeTheMessageCheck(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "order-customer@example.com", "0400000169", "customer")
	provider := newAccount(t, pool, "order-provider@example.com", "0400000170", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	stranger := newAccount(t, pool, "order-stranger@example.com", "0400000171", "customer")

	in := goodReport("ordering")
	in.Subject = ReportSubjectMessage
	in.MessageID = uuid.New()

	// A port that says every message is on every job. If the message check ran first, this would
	// get past it and the stranger would learn the message "exists".
	svc := NewReports(testParties{}, staticMessages{onJob: true})

	_, _, err := svc.Raise(t.Context(), pool, stranger, jobID, in)
	if !errors.Is(err, ErrNotAParty) {
		t.Fatalf("got %v, want ErrNotAParty; a stranger must be refused before anything about "+
			"the job's messages is read", err)
	}
}

// TestARetryRaisesNoSecondReport is the *Done when*'s idempotency clause.
//
// "A repeated Idempotency-Key answers with the report already raised rather than raising a second."
//
// This is the path *after* SHIP-15's middleware entry has expired or been evicted — while it lives,
// the handler is never reached. `uq_reports_idempotency` is what is left, and what it buys is that a
// phone reconnecting a day later is told about its own report rather than somebody else's.
func TestARetryRaisesNoSecondReport(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "retry-customer@example.com", "0400000172", "customer")
	provider := newAccount(t, pool, "retry-provider@example.com", "0400000173", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	svc := newTestReports()

	first, raised, err := svc.Raise(t.Context(), pool, customer, jobID, goodReport("same-key"))
	if err != nil || !raised {
		t.Fatalf("the first report: %v, raised=%v", err, raised)
	}

	again, raisedAgain, err := svc.Raise(t.Context(), pool, customer, jobID, goodReport("same-key"))
	if err != nil {
		t.Fatalf("the retry was refused: %v", err)
	}
	if raisedAgain {
		t.Error("the retry reported that it had raised a second report")
	}
	if again.ID != first.ID {
		t.Errorf("the retry answered with report %s, want the one already raised, %s",
			again.ID, first.ID)
	}
	if n := reportCount(t, pool, jobID); n != 1 {
		t.Errorf("the job carries %d reports after one raise and one retry, want 1", n)
	}
}

// TestAKeyReusedForAnotherReportIsRefused is the retry check's other side, and it is **wider than
// the dispute version by one field**.
//
// A dispute is discriminated by its category alone, because a job carries at most one open dispute.
// A job carries any number of reports — about itself and about each of its messages — so the
// *subject* is as much a part of "which action was this" as the reason. Both are varied below.
func TestAKeyReusedForAnotherReportIsRefused(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "reuse-customer@example.com", "0400000174", "customer")
	provider := newAccount(t, pool, "reuse-provider@example.com", "0400000175", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	messageID := aMessageOn(t, pool, jobID, provider)

	svc := newTestReports()

	if _, _, err := svc.Raise(t.Context(), pool, customer, jobID, goodReport("reused")); err != nil {
		t.Fatalf("the first report: %v", err)
	}

	t.Run("a different reason under the same key", func(t *testing.T) {
		other := goodReport("reused")
		other.Reason = ReasonAbusive

		if _, _, err := svc.Raise(t.Context(), pool, customer, jobID, other); !errors.Is(err, ErrIdempotencyKeyReused) {
			t.Fatalf("got %v, want ErrIdempotencyKeyReused", err)
		}
	})

	t.Run("a different subject under the same key", func(t *testing.T) {
		// The case the dispute version cannot have and this one must: same reason, same job,
		// different thing being reported. Answering with the first would tell the client that
		// something it never sent had been raised.
		other := goodReport("reused")
		other.Subject = ReportSubjectMessage
		other.MessageID = messageID

		if _, _, err := svc.Raise(t.Context(), pool, customer, jobID, other); !errors.Is(err, ErrIdempotencyKeyReused) {
			t.Fatalf("got %v, want ErrIdempotencyKeyReused", err)
		}
	})

	if n := reportCount(t, pool, jobID); n != 1 {
		t.Errorf("the job carries %d reports, want 1: neither reuse should have written one", n)
	}
}

// TestASecondReportOnOneJobIsAllowed is the difference from a dispute, at the service.
//
// `uq_disputes_open_per_job` refuses a second open dispute because a job is frozen once. Nothing
// here freezes, so two parties reporting one job under two keys are two rows — and the moderator
// sees both. A "one report per job" rule added later would break this.
func TestASecondReportOnOneJobIsAllowed(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "second-customer@example.com", "0400000176", "customer")
	provider := newAccount(t, pool, "second-provider@example.com", "0400000177", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	svc := newTestReports()

	if _, _, err := svc.Raise(t.Context(), pool, customer, jobID, goodReport("first")); err != nil {
		t.Fatalf("the customer's report: %v", err)
	}

	fromProvider := goodReport("second")
	fromProvider.Reason = ReasonAbusive
	if _, raised, err := svc.Raise(t.Context(), pool, provider, jobID, fromProvider); err != nil || !raised {
		t.Fatalf("the provider's report: %v, raised=%v; a job is disputed once and reported "+
			"freely", err, raised)
	}

	if n := reportCount(t, pool, jobID); n != 2 {
		t.Errorf("the job carries %d reports, want 2", n)
	}
}

// TestAKeylessReportIsRefused.
//
// SHIP-15's middleware refuses a state-changing request without a key, so this is unreachable
// through the served route. It is checked anyway because `uq_reports_idempotency` is *partial*: a row
// with a NULL key falls outside the index, so the guarantee that a retry raises no second report
// would be **absent rather than broken** — which is the failure mode that produces no error at all.
func TestAKeylessReportIsRefused(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "keyless-customer@example.com", "0400000178", "customer")
	provider := newAccount(t, pool, "keyless-provider@example.com", "0400000179", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	in := goodReport("")
	if _, _, err := newTestReports().Raise(t.Context(), pool, customer, jobID, in); !errors.Is(err, ErrNoIdempotencyKey) {
		t.Fatalf("got %v, want ErrNoIdempotencyKey", err)
	}
	if n := reportCount(t, pool, jobID); n != 0 {
		t.Errorf("%d reports were written without a key to record them against", n)
	}
}

// TestReportIntakeIsValidatedBeforeAnythingIsRead gathers every field problem in one answer.
//
// internal/validate's header is the argument: a form should not take one round trip per field. The
// service is given ports that would panic if called, so a test that passes also establishes that
// nothing was read before the request was found to be malformed.
func TestReportIntakeIsValidatedBeforeAnythingIsRead(t *testing.T) {
	pool := pgtest.DB(t)

	// Ports that answer wrongly rather than panicking: if validation did not run first, the
	// request would be refused as ErrNotAParty and the assertion below would name the wrong
	// failure — which is a clearer report than a panic's stack.
	svc := NewReports(staticParties{}, staticMessages{})

	for _, tc := range []struct {
		name  string
		in    ReportIntake
		field string
	}{
		{
			name:  "no subject",
			in:    ReportIntake{Reason: ReasonOther, Description: "Something is wrong.", Key: "k"},
			field: "subject_type",
		},
		{
			name: "a subject nobody can report",
			in: ReportIntake{
				Subject: "profile", Reason: ReasonOther,
				Description: "Something is wrong.", Key: "k",
			},
			field: "subject_type",
		},
		{
			name: "a message subject with no message",
			in: ReportIntake{
				Subject: ReportSubjectMessage, Reason: ReasonOther,
				Description: "Something is wrong.", Key: "k",
			},
			field: "message_id",
		},
		{
			name: "a job subject carrying a message",
			in: ReportIntake{
				Subject: ReportSubjectJob, MessageID: uuid.New(), Reason: ReasonOther,
				Description: "Something is wrong.", Key: "k",
			},
			field: "message_id",
		},
		{
			name:  "no reason",
			in:    ReportIntake{Subject: ReportSubjectJob, Description: "Something is wrong.", Key: "k"},
			field: "reason",
		},
		{
			name: "a reason nobody offers",
			in: ReportIntake{
				Subject: ReportSubjectJob, Reason: "i just do not like them",
				Description: "Something is wrong.", Key: "k",
			},
			field: "reason",
		},
		{
			name:  "no description",
			in:    ReportIntake{Subject: ReportSubjectJob, Reason: ReasonOther, Key: "k"},
			field: "description",
		},
		{
			name: "a description of nothing but whitespace",
			in: ReportIntake{
				Subject: ReportSubjectJob, Reason: ReasonOther, Description: "   \n\t ", Key: "k",
			},
			field: "description",
		},
		{
			name: "a description past the bound",
			in: ReportIntake{
				Subject: ReportSubjectJob, Reason: ReasonOther,
				Description: strings.Repeat("a", maxReportDescription+1), Key: "k",
			},
			field: "description",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := svc.Raise(t.Context(), pool, uuid.New(), uuid.New(), tc.in)
			if err == nil {
				t.Fatal("the intake was accepted")
			}

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("got %v, want a field-level problem naming %q", err, tc.field)
			}
			if apiErr.Code != httpx.CodeValidationFailed {
				t.Fatalf("code = %q, want %q", apiErr.Code, httpx.CodeValidationFailed)
			}

			var named bool
			for _, d := range apiErr.Details {
				if d.Field == tc.field {
					named = true
				}
			}
			if !named {
				t.Errorf("the refusal does not name %q: %+v", tc.field, apiErr.Details)
			}
		})
	}
}

// TestReportsRefusesToBeBuiltWithoutItsCollaborators.
//
// Both are decided once in the composition root, and neither may be defaulted to something harmless:
// a nil party lookup is "nobody is checked", which is the one refusal this endpoint exists for, and a
// nil message lookup is "any message id is on any job".
func TestReportsRefusesToBeBuiltWithoutItsCollaborators(t *testing.T) {
	for _, tc := range []struct {
		name     string
		parties  JobParties
		messages JobMessages
	}{
		{"no party lookup", nil, testMessages{}},
		{"no message lookup", testParties{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Error("a report service was built with a missing collaborator; the first " +
						"request would panic instead")
				}
			}()
			NewReports(tc.parties, tc.messages)
		})
	}
}

// TestReportsOnAJobComeBackNewestFirst is the read this package's own tests lean on.
//
// Not SHIP-156 — that queue is every report on the platform, paged and oldest first. This answers
// "what has been reported about this one job", which is what makes a refusal assertable without
// reaching into the table.
func TestReportsOnAJobComeBackNewestFirst(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "list-customer@example.com", "0400000180", "customer")
	provider := newAccount(t, pool, "list-provider@example.com", "0400000181", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	svc := newTestReports()

	first, _, err := svc.Raise(t.Context(), pool, customer, jobID, goodReport("list-one"))
	if err != nil {
		t.Fatalf("the first report: %v", err)
	}

	second := goodReport("list-two")
	second.Reason = ReasonSafety
	latest, _, err := svc.Raise(t.Context(), pool, provider, jobID, second)
	if err != nil {
		t.Fatalf("the second report: %v", err)
	}

	got, err := svc.ReportsOn(t.Context(), pool, jobID)
	if err != nil {
		t.Fatalf("reading the reports on %s: %v", jobID, err)
	}
	if len(got) != 2 {
		t.Fatalf("%d reports came back, want 2", len(got))
	}
	if got[0].ID != latest.ID || got[1].ID != first.ID {
		t.Errorf("the reports came back in the wrong order: %s then %s, want %s then %s",
			got[0].ID, got[1].ID, latest.ID, first.ID)
	}
}

// historyRows counts a job's transitions, so a test can tell "the status is unchanged" from "the
// status was moved and moved back".
func historyRows(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("counting the transitions on %s: %v", jobID, err)
	}
	return n
}

// Compile-time proof that the fakes satisfy the ports, which is what makes them a stand-in for
// cmd/api's adapters rather than a different shape that happens to work.
var (
	_ JobMessages = testMessages{}
	_ JobMessages = staticMessages{}
	_ jobs.Status = jobs.StatusDelivered
)
