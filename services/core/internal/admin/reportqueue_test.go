package admin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-156 against a real PostgreSQL, for reports_test.go's reason and one more of its own.
//
// The queue's ordering and its cursor are `idx_reports_queue`'s `(created_at, id)`, and the whole
// question a paging test asks — does a page boundary between two rows recorded in the same
// microsecond repeat one or skip one — is a question about what PostgreSQL does with a row
// constructor. A repository double would answer it with whatever the double's author believed.
//
// The fixtures are service_test.go's and reports_test.go's, because a report queue lists exactly the
// reports raised against exactly the jobs those files already build.

// --- the ports, as cmd/api implements them -------------------------------------------------------

// ConversationOn makes [testMessages] admin.JobConversations as well as [JobMessages], which is
// precisely what jobMessageLookup does in cmd/api — one adapter satisfying two interfaces, so that
// [Reports] can hold it through the narrow one and be unable to read a conversation with it.
//
// The statement is the production one; the pair is exercised end to end by the binary rather than by
// a Go test, for the reason service_test.go's header records.
func (testMessages) ConversationOn(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
) ([]ConversationMessage, error) {
	const q = `
		SELECT id, provider_id, sent_by, body, created_at
		FROM job_messages
		WHERE job_id = $1
		ORDER BY provider_id, created_at, id`

	rows, err := r.Query(ctx, q, jobID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]ConversationMessage, 0)
	for rows.Next() {
		var m ConversationMessage
		if err := rows.Scan(&m.ID, &m.ProviderID, &m.SentBy, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// testReportedJobs is admin.ReportedJobs over `jobs`, as cmd/api reads it.
//
// reportedJobLookup's statement verbatim, including the absent `budget` — which is the column this
// shape exists not to carry (Docs/01 §4.3).
type testReportedJobs struct{}

func (testReportedJobs) JobsForReports(
	ctx context.Context,
	r db.Runner,
	jobIDs []uuid.UUID,
) (map[uuid.UUID]ReportedJob, error) {
	out := make(map[uuid.UUID]ReportedJob, len(jobIDs))
	if len(jobIDs) == 0 {
		return out, nil
	}

	const q = `
		SELECT id, customer_id, status, coalesce(goods_description, ''), created_at
		FROM jobs
		WHERE id = ANY($1)`

	rows, err := r.Query(ctx, q, jobIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var job ReportedJob
		if err := rows.Scan(
			&job.ID, &job.CustomerID, &job.Status, &job.GoodsDescription, &job.CreatedAt,
		); err != nil {
			return nil, err
		}
		out[job.ID] = job
	}
	return out, rows.Err()
}

// forgetfulJobs answers with nothing, for the contradiction no fixture can produce.
//
// `fk_reports_job` is ON DELETE RESTRICT, so no sequence of database operations can leave a report
// naming a job that is not there. The only way to reach [ErrReportedJobVanished] is a port that lies,
// which is also the only way it could happen in production — a mis-wired adapter.
type forgetfulJobs struct{}

func (forgetfulJobs) JobsForReports(
	context.Context, db.Runner, []uuid.UUID,
) (map[uuid.UUID]ReportedJob, error) {
	return map[uuid.UUID]ReportedJob{}, nil
}

// silentConversations answers with no messages, for the cases a conversation is beside the point.
type silentConversations struct{}

func (silentConversations) ConversationOn(
	context.Context, db.Runner, uuid.UUID,
) ([]ConversationMessage, error) {
	return nil, nil
}

// --- fixtures ------------------------------------------------------------------------------------

func newTestReportQueue(t *testing.T, pool *pgxpool.Pool) *ReportQueue {
	t.Helper()

	queue, err := NewReportQueue(testReportedJobs{}, testMessages{}, pool)
	if err != nil {
		t.Fatalf("building the report queue: %v", err)
	}
	return queue
}

// aMessage inserts one message into a job's conversation with a provider and answers its identifier.
//
// [aMessageOn]'s sibling with the author and the words under the test's control, because this file
// asserts on both: which side wrote a line is what a moderator reads a reported exchange for.
func aMessage(t *testing.T, pool *pgxpool.Pool, jobID, providerID uuid.UUID, sentBy, body string) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO job_messages (id, job_id, provider_id, sent_by, body)
		VALUES ($1, $2, $3, $4, $5)`, id, jobID, providerID, sentBy, body); err != nil {
		t.Fatalf("inserting a message on %s: %v", jobID, err)
	}
	return id
}

// raise puts one report on the table through the service that owns writing them.
//
// Through [Reports.Raise] rather than an INSERT, so that what the queue lists is what intake
// actually writes — a fixture writing the row directly would let the two drift and the queue would
// still pass.
func raise(t *testing.T, pool *pgxpool.Pool, reporter, jobID uuid.UUID, in ReportIntake) Report {
	t.Helper()

	report, raised, err := newTestReports().Raise(t.Context(), pool, reporter, jobID, in)
	if err != nil {
		t.Fatalf("raising a report on %s: %v", jobID, err)
	}
	if !raised {
		t.Fatalf("the report on %s under %q was not raised", jobID, in.Key)
	}
	return report
}

// describeJob gives a job the listing text a `misleading_listing` report would be about.
func describeJob(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, description string) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET goods_description = $2 WHERE id = $1`, jobID, description); err != nil {
		t.Fatalf("describing %s: %v", jobID, err)
	}
}

// raisedAt forces a report's clock, so a test can build the row order it needs to assert on.
//
// `reports.created_at` defaults to `now()`, which is transaction time — so reports raised by
// consecutive statements differ by microseconds and can never be made to collide. Two entries
// sharing an instant is the case the cursor's tie-break exists for, and this is the only way to
// produce it.
func raisedAt(t *testing.T, pool *pgxpool.Pool, reportID uuid.UUID, at time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`UPDATE reports SET created_at = $2 WHERE id = $1`, reportID, at); err != nil {
		t.Fatalf("setting the clock on %s: %v", reportID, err)
	}
}

// --- the acceptance criterion --------------------------------------------------------------------

// TestReportsSurfaceWithTheJobAndConversationInContext is SHIP-156's *Done when*, clause by clause.
//
// "Reports surface" — the queue lists what SHIP-155a wrote, which nothing else in the console can
// see. "with the job" — the entry carries where that job has got to, and the report screen carries
// the listing text the complaint is about. "and conversation in context" — the report screen carries
// what was said on the job, with the reported message among it.
func TestReportsSurfaceWithTheJobAndConversationInContext(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "queue-customer@example.com", "0400000201", "customer")
	provider := newAccount(t, pool, "queue-provider@example.com", "0400000202", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	describeJob(t, pool, jobID, "Two pallets of garden supplies, Ballarat to Bendigo.")

	// The conversation the report is about, both sides of it.
	first := aMessage(t, pool, jobID, provider, "provider",
		"Happy to do it Thursday. Cash on the day and we can skip the paperwork.")
	answer := aMessage(t, pool, jobID, provider, "customer",
		"I would rather keep it on the platform, thanks.")

	reported := raise(t, pool, customer, jobID, ReportIntake{
		Subject:     ReportSubjectMessage,
		MessageID:   first,
		Reason:      ReasonOffPlatform,
		Description: "They are asking to settle off the platform.",
		Key:         "context-one",
	})

	queue := newTestReportQueue(t, pool)

	// "Reports surface" — and with the job's status beside them, which is what a moderator
	// working oldest-first triages on.
	entries, err := queue.Queue(t.Context(), ReportQuery{Limit: 20})
	if err != nil {
		t.Fatalf("reading the report queue: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("the queue holds %d entries, want 1", len(entries))
	}

	entry := entries[0]
	switch {
	case entry.ID != reported.ID:
		t.Errorf("the queue lists %s, want %s", entry.ID, reported.ID)
	case entry.JobID != jobID:
		t.Errorf("the entry is about %s, want %s", entry.JobID, jobID)
	case entry.JobStatus != "Delivered":
		t.Errorf("the entry says the job is %q, want %q", entry.JobStatus, "Delivered")
	case entry.Subject != ReportSubjectMessage:
		t.Errorf("the entry's subject is %q, want %q", entry.Subject, ReportSubjectMessage)
	case entry.MessageID != first:
		t.Errorf("the entry points at %s, want %s", entry.MessageID, first)
	case entry.About() != first:
		t.Errorf("the entry is about %s, want the message %s", entry.About(), first)
	case entry.ReporterParty != PartyCustomer:
		t.Errorf("the reporter is recorded as %q, want %q", entry.ReporterParty, PartyCustomer)
	case entry.Reason != ReasonOffPlatform:
		t.Errorf("the entry's reason is %q, want %q", entry.Reason, ReasonOffPlatform)
	case entry.RaisedAt.IsZero():
		t.Error("the entry carries no instant, so the queue has nothing to order by")
	}

	// "with the job … in context" — the listing text the complaint is about, on the screen the
	// moderator opens rather than behind a second request.
	detail, err := queue.Report(t.Context(), reported.ID)
	if err != nil {
		t.Fatalf("opening report %s: %v", reported.ID, err)
	}
	if detail.Job.ID != jobID {
		t.Errorf("the report opened onto job %s, want %s", detail.Job.ID, jobID)
	}
	if detail.Job.CustomerID != customer {
		t.Errorf("the job's customer is %s, want %s", detail.Job.CustomerID, customer)
	}
	if detail.Job.GoodsDescription != "Two pallets of garden supplies, Ballarat to Bendigo." {
		t.Errorf("the job's listing reads %q, which is not what was described",
			detail.Job.GoodsDescription)
	}
	if detail.Job.Status != "Delivered" {
		t.Errorf("the job is %q on the report screen, want %q", detail.Job.Status, "Delivered")
	}

	// "the description" — the field the queue deliberately does not carry, and the reason there
	// are two endpoints rather than one.
	if detail.Report.Description != "They are asking to settle off the platform." {
		t.Errorf("the report reads %q on the screen that is supposed to carry it",
			detail.Report.Description)
	}

	// "and conversation in context" — both messages, in the order they were said, with the
	// reported one among them rather than quoted on its own.
	if len(detail.Conversation) != 2 {
		t.Fatalf("the conversation carries %d messages, want 2", len(detail.Conversation))
	}
	if detail.Conversation[0].ID != first || detail.Conversation[1].ID != answer {
		t.Errorf("the conversation is [%s %s], want [%s %s] — oldest first",
			detail.Conversation[0].ID, detail.Conversation[1].ID, first, answer)
	}
	if detail.Conversation[0].SentBy != "provider" || detail.Conversation[1].SentBy != "customer" {
		t.Errorf("the conversation attributes the two messages to %q and %q, want provider then customer",
			detail.Conversation[0].SentBy, detail.Conversation[1].SentBy)
	}
	if detail.Conversation[0].Body == "" {
		t.Error("the reported message arrived with no body, which is the thing a moderator opens it to read")
	}

	// The reported message is identified within the conversation rather than lifted out of it.
	if !slices.ContainsFunc(detail.Conversation, func(m ConversationMessage) bool {
		return m.ID == detail.Report.MessageID
	}) {
		t.Error("the reported message is not in the conversation the report opened onto")
	}
}

// TestAReportAboutTheJobOpensOntoTheConversationToo.
//
// The branch [ReportQueue.Report] deliberately does not have. Docs/04 §6 step 2 asks a reviewer to
// read the communications on *every* review, and a party who reports a listing as abusive has almost
// certainly been told something rather than read something — so a screen that showed the
// conversation only for a message report would be blank in the case it is most needed.
func TestAReportAboutTheJobOpensOntoTheConversationToo(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "job-report-customer@example.com", "0400000203", "customer")
	provider := newAccount(t, pool, "job-report-provider@example.com", "0400000204", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	said := aMessage(t, pool, jobID, provider, "provider", "You will regret wasting my morning.")

	reported := raise(t, pool, customer, jobID, goodReport("job-report"))

	detail, err := newTestReportQueue(t, pool).Report(t.Context(), reported.ID)
	if err != nil {
		t.Fatalf("opening report %s: %v", reported.ID, err)
	}

	if detail.Report.Subject != ReportSubjectJob {
		t.Fatalf("the report is about %q, want the job itself", detail.Report.Subject)
	}
	if detail.Report.MessageID != uuid.Nil {
		t.Errorf("a report about the job names message %s", detail.Report.MessageID)
	}
	if len(detail.Conversation) != 1 || detail.Conversation[0].ID != said {
		t.Errorf("a report about the job opened onto %d messages, want the one that was said",
			len(detail.Conversation))
	}
}

// TestTheConversationIsTheWholeJobsRatherThanOneNegotiation.
//
// `job_messages` is keyed `(job_id, provider_id)` (`000506`), so a job carries one conversation per
// provider who bid on it. A read scoped to the reported message's own negotiation would answer well
// for a message report and answer nothing for a report about the listing — and it would hide the
// losing bidder's messages from a moderator asked whether a customer is abusive to everybody.
func TestTheConversationIsTheWholeJobsRatherThanOneNegotiation(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "two-negotiations@example.com", "0400000205", "customer")
	winner := newAccount(t, pool, "winning-provider@example.com", "0400000206", "provider")
	loser := newAccount(t, pool, "losing-provider@example.com", "0400000207", "provider")

	jobID := deliveredJob(t, pool, customer, winner)
	toWinner := aMessage(t, pool, jobID, winner, "customer", "Thursday works.")
	toLoser := aMessage(t, pool, jobID, loser, "customer", "Not at that price, no.")

	reported := raise(t, pool, customer, jobID, goodReport("both-negotiations"))

	detail, err := newTestReportQueue(t, pool).Report(t.Context(), reported.ID)
	if err != nil {
		t.Fatalf("opening report %s: %v", reported.ID, err)
	}

	if len(detail.Conversation) != 2 {
		t.Fatalf("the report opened onto %d messages, want both negotiations", len(detail.Conversation))
	}

	// Grouped by negotiation, which is `idx_job_messages_conversation`'s leading column and what
	// lets a console render one thread per provider without sorting the page itself.
	providers := []uuid.UUID{detail.Conversation[0].ProviderID, detail.Conversation[1].ProviderID}
	if providers[0] == providers[1] {
		t.Errorf("both messages are attributed to negotiation %s", providers[0])
	}
	for _, want := range []uuid.UUID{toWinner, toLoser} {
		if !slices.ContainsFunc(detail.Conversation, func(m ConversationMessage) bool {
			return m.ID == want
		}) {
			t.Errorf("message %s is missing from the conversation", want)
		}
	}
}

// --- the ordering and the cursor -----------------------------------------------------------------

// TestTheReportQueueIsOldestFirst.
//
// Docs/04 §8 sets an acknowledgement target, so the oldest entry is the one closest to breaching it —
// the ordering `idx_reports_queue` was created for and the same one the verification queue and the
// open half of the dispute queue use. The account and job searches are newest-first for the opposite
// reason, so getting this backwards would look plausible.
func TestTheReportQueueIsOldestFirst(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "oldest-first@example.com", "0400000208", "customer")
	provider := newAccount(t, pool, "oldest-first-p@example.com", "0400000209", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	base := time.Date(2026, 8, 20, 9, 0, 0, 0, time.UTC)
	var order []uuid.UUID
	for i, key := range []string{"third", "first", "second"} {
		report := raise(t, pool, customer, jobID, goodReport(key))
		// Written out of order on purpose: if the queue were ordering by insertion or by
		// identifier rather than by the clock, this test would pass by accident otherwise.
		raisedAt(t, pool, report.ID, base.Add(time.Duration(i)*time.Hour))
		order = append(order, report.ID)
	}
	// order is [09:00, 10:00, 11:00] by construction, which is already oldest first.

	entries, err := newTestReportQueue(t, pool).Queue(t.Context(), ReportQuery{Limit: 20})
	if err != nil {
		t.Fatalf("reading the report queue: %v", err)
	}
	if len(entries) != 3 {
		t.Fatalf("the queue holds %d entries, want 3", len(entries))
	}
	for i, want := range order {
		if entries[i].ID != want {
			t.Fatalf("entry %d is %s, want %s — the queue is not oldest first", i, entries[i].ID, want)
		}
	}
}

// TestTheReportQueuePagesWithoutRepeatingOrSkipping.
//
// The cursor is `(created_at, id)`, which is `idx_reports_queue`'s whole key, and the case it exists
// for is two reports raised in the same instant landing either side of a page boundary. `000805`
// widened the index at creation precisely so SHIP-156 would not have to, and this is the assertion
// that the widening was needed: with a `created_at`-only cursor one of these six is lost.
func TestTheReportQueuePagesWithoutRepeatingOrSkipping(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "paging@example.com", "0400000210", "customer")
	provider := newAccount(t, pool, "paging-p@example.com", "0400000211", "provider")
	jobID := deliveredJob(t, pool, customer, provider)

	// **Two groups of three, read two at a time**, so that a page boundary falls *inside* a
	// group of rows sharing an instant. The arrangement matters more than it looks: three
	// groups of two read two at a time would put every boundary on a group edge, where a
	// cursor comparing `created_at` alone happens to be correct — a paging test can be written
	// so that the thing it exists to catch cannot fail it, and this one was, until the mutation
	// below passed against it.
	collision := time.Date(2026, 8, 21, 11, 30, 0, 0, time.UTC)
	raisedIDs := make(map[uuid.UUID]bool, 6)
	for i, key := range []string{"a", "b", "c", "d", "e", "f"} {
		report := raise(t, pool, customer, jobID, goodReport(key))
		raisedAt(t, pool, report.ID, collision.Add(time.Duration(i/3)*time.Minute))
		raisedIDs[report.ID] = false
	}

	queue := newTestReportQueue(t, pool)

	var (
		cursor ReportCursor
		pages  int
	)
	for {
		pages++
		if pages > 10 {
			t.Fatal("the queue did not run out after ten pages of two, so a cursor is not advancing")
		}

		entries, err := queue.Queue(t.Context(), ReportQuery{Limit: 2, After: cursor})
		if err != nil {
			t.Fatalf("reading page %d: %v", pages, err)
		}
		if len(entries) == 0 {
			break
		}
		for _, entry := range entries {
			seen, known := raisedIDs[entry.ID]
			if !known {
				t.Fatalf("page %d carries %s, which was never raised", pages, entry.ID)
			}
			if seen {
				t.Fatalf("page %d repeats %s", pages, entry.ID)
			}
			raisedIDs[entry.ID] = true
		}
		last := entries[len(entries)-1]
		cursor = ReportCursor{RaisedAt: last.RaisedAt, ID: last.ID}
	}

	for id, seen := range raisedIDs {
		if !seen {
			t.Errorf("%s was never returned — the cursor skipped it at a tie", id)
		}
	}
}

// TestAnEmptyReportQueueIsAnEmptyPageRatherThanAFault.
//
// The ordinary state of a healthy platform, and the one a queue must answer plainly: nothing to
// review is an answer, and a service that reported an error for it would train whoever reads the
// console to ignore the error.
func TestAnEmptyReportQueueIsAnEmptyPageRatherThanAFault(t *testing.T) {
	pool := pgtest.DB(t)

	entries, err := newTestReportQueue(t, pool).Queue(t.Context(), ReportQuery{Limit: 20})
	if err != nil {
		t.Fatalf("reading an empty queue: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("an empty queue reported %d entries", len(entries))
	}
}

// --- what the shapes carry and refuse ------------------------------------------------------------

// TestTheReportQueueShapeCarriesNothingCommercial.
//
// A closed key set rather than a search for the word "budget", for the reason
// TestTheCancellationShapeCarriesNothingCommercial records: SHIP-83 established that a field called
// `max_price` passes that search and leaks the same fact.
//
// **It is also where the description is held out.** The field this queue would most plausibly
// acquire is the reporter's own account of what is wrong — it is on the row, it is one column away,
// and adding it would make a page of fifty carry two hundred thousand characters nothing renders.
func TestTheReportQueueShapeCarriesNothingCommercial(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "queue-shape@example.com", "0400000212", "customer")
	provider := newAccount(t, pool, "queue-shape-p@example.com", "0400000213", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	raise(t, pool, customer, jobID, goodReport("shape"))

	entries, err := newTestReportQueue(t, pool).Queue(t.Context(), ReportQuery{Limit: 5})
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("no entries to check the shape of")
	}

	encoded, err := json.Marshal(reportEntryFrom(entries[0]))
	if err != nil {
		t.Fatalf("encoding an entry: %v", err)
	}

	var keys map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &keys); err != nil {
		t.Fatalf("decoding an entry: %v", err)
	}

	permitted := map[string]bool{
		"id": true, "job_id": true, "job_status": true, "subject_type": true,
		"message_id": true, "reporter_id": true, "reporter_party": true, "reason": true,
		"raised_at": true,
	}

	var unexpected []string
	for key := range keys {
		if !permitted[key] {
			unexpected = append(unexpected, key)
		}
	}
	slices.Sort(unexpected)

	if len(unexpected) != 0 {
		t.Errorf("a report queue entry carries %v.\nThis shape is a closed set. A customer's "+
			"budget is never exposed in any form (Docs/01 §4.3), and the reporter's description "+
			"belongs on GET /v1/admin/reports/{id} rather than on a page of fifty — if a field "+
			"belongs here, add it to the permitted set deliberately.", unexpected)
	}
	for key := range permitted {
		if _, ok := keys[key]; !ok {
			t.Errorf("a report queue entry is missing %q", key)
		}
	}
}

// TestTheReportScreenNeverCarriesTheIdempotencyKey.
//
// The key is on the row and is read back by [postgresStore.reportByID], because intake needs it to
// tell a retry from a reused key. It must not reach the wire: it is the reporter's own value coming
// back at an administrator who did not send it, and a response carrying it invites a console to
// treat it as an identifier the platform issued.
func TestTheReportScreenNeverCarriesTheIdempotencyKey(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "no-key@example.com", "0400000214", "customer")
	provider := newAccount(t, pool, "no-key-p@example.com", "0400000215", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	reported := raise(t, pool, customer, jobID, goodReport("a-key-nobody-else-should-see"))

	detail, err := newTestReportQueue(t, pool).Report(t.Context(), reported.ID)
	if err != nil {
		t.Fatalf("opening report %s: %v", reported.ID, err)
	}

	// It is read from the row — that is what makes the omission below a decision rather than an
	// accident of the column list.
	if detail.Report.Key != "a-key-nobody-else-should-see" {
		t.Fatalf("the report was read back with key %q, so this test is not testing what it thinks",
			detail.Report.Key)
	}

	encoded, err := json.Marshal(reportDetailFrom(detail))
	if err != nil {
		t.Fatalf("encoding the report screen: %v", err)
	}
	if body := string(encoded); strings.Contains(body, "a-key-nobody-else-should-see") {
		t.Errorf("the report screen carries the reporter's idempotency key:\n%s", body)
	}
}

// --- the refusals --------------------------------------------------------------------------------

// TestOpeningAReportThatIsNotThere.
//
// Disclosed plainly, unlike intake's 404: the caller holds `moderation.read` over every report on
// the platform, and the indistinguishability [ErrNotAParty] buys is a rule about strangers.
func TestOpeningAReportThatIsNotThere(t *testing.T) {
	pool := pgtest.DB(t)
	queue := newTestReportQueue(t, pool)

	t.Run("no such report", func(t *testing.T) {
		if _, err := queue.Report(t.Context(), uuid.Must(uuid.NewV7())); !errors.Is(err, ErrReportNotFound) {
			t.Errorf("opening an unknown report answered %v, want ErrReportNotFound", err)
		}
	})

	t.Run("the zero identifier is refused before it reaches the database", func(t *testing.T) {
		if _, err := queue.Report(t.Context(), uuid.Nil); !errors.Is(err, ErrReportNotFound) {
			t.Errorf("opening the nil report answered %v, want ErrReportNotFound", err)
		}
	})
}

// TestAReportNamingAJobTheLookupCannotFindIsAContradiction.
//
// `fk_reports_job` is ON DELETE RESTRICT (`000805`), so nothing in this platform can remove a job a
// report points at. A lookup that answers without one is therefore a mis-wire, and it becomes an
// opaque 500 with its cause logged rather than a queue entry carrying a blank status — which would
// read as a broken console and be looked for in the wrong place. [Reports.alreadyRaised] takes the
// same position on the same kind of impossibility.
func TestAReportNamingAJobTheLookupCannotFindIsAContradiction(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "vanished@example.com", "0400000216", "customer")
	provider := newAccount(t, pool, "vanished-p@example.com", "0400000217", "provider")
	jobID := deliveredJob(t, pool, customer, provider)
	reported := raise(t, pool, customer, jobID, goodReport("vanished"))

	queue, err := NewReportQueue(forgetfulJobs{}, silentConversations{}, pool)
	if err != nil {
		t.Fatalf("building the queue: %v", err)
	}

	t.Run("on the queue", func(t *testing.T) {
		entries, err := queue.Queue(t.Context(), ReportQuery{Limit: 20})
		if !errors.Is(err, ErrReportedJobVanished) {
			t.Errorf("the queue answered %d entries and %v, want ErrReportedJobVanished",
				len(entries), err)
		}
	})

	t.Run("on the report screen", func(t *testing.T) {
		if _, err := queue.Report(t.Context(), reported.ID); !errors.Is(err, ErrReportedJobVanished) {
			t.Errorf("opening the report answered %v, want ErrReportedJobVanished", err)
		}
	})
}

// TestTheReportQueueNeedsItsPortsAndAPool.
//
// The three failures a queue has that nothing else notices: a database that is gone, a job lookup
// that leaves every status blank, and a conversation lookup that makes every report look like a
// complaint about nothing.
func TestTheReportQueueNeedsItsPortsAndAPool(t *testing.T) {
	t.Run("an unreachable database says so rather than reporting an empty queue", func(t *testing.T) {
		queue, err := NewReportQueue(testReportedJobs{}, silentConversations{}, nil)
		if err != nil {
			t.Fatalf("building a queue with no pool: %v", err)
		}

		if entries, err := queue.Queue(t.Context(), ReportQuery{Limit: 20}); !errors.Is(err, ErrAdminUnavailable) {
			t.Errorf("an unreachable database reported %d entries and %v, want ErrAdminUnavailable",
				len(entries), err)
		}
		if _, err := queue.Report(t.Context(), uuid.Must(uuid.NewV7())); !errors.Is(err, ErrAdminUnavailable) {
			t.Errorf("an unreachable database answered %v, want ErrAdminUnavailable", err)
		}
	})

	t.Run("a queue with no job lookup is refused at construction", func(t *testing.T) {
		if _, err := NewReportQueue(nil, silentConversations{}, nil); err == nil {
			t.Error("a queue with no job lookup was built; every entry's status would be blank")
		}
	})

	t.Run("a queue with no conversation lookup is refused at construction", func(t *testing.T) {
		if _, err := NewReportQueue(testReportedJobs{}, nil, nil); err == nil {
			t.Error("a queue with no conversation lookup was built; every report would open " +
				"onto an empty conversation, which is indistinguishable from nothing having " +
				"been said")
		}
	})
}
