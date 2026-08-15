package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-68 — the deadline half. The sweep that acts on it is cmd/worker's, and is tested there.
//
// SHIP-69's warning is here too, and both of its halves are: the claim's predicate and what one
// warning does. The pass that drives them is cmd/worker's, like the expiry sweep's.
//
// Docs/02 §6.3: an Open job leaves Open at the earlier of fourteen days after publication or its
// pickup date passing. These tests are against a real PostgreSQL because most of the rule is a
// trigger (000406), and Docs/06 §4.1 is the argument: "a mock happily accepts a write that the
// actual constraint would reject" — and would happily accept a job published with no deadline
// at all, which is the failure the trigger exists to make impossible.

// deadlineOf reads a job's expires_at straight out of the table, bypassing the domain.
func deadlineOf(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) *time.Time {
	t.Helper()

	var at *time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT expires_at FROM jobs WHERE id = $1`, job).Scan(&at); err != nil {
		t.Fatalf("reading the deadline of %s: %v", job, err)
	}
	return at
}

// publishedAt is when the platform recorded a job becoming Open.
//
// The trigger computes its fourteen days from now(), which inside a transaction is the transaction
// start time — the same instant job_status_history.server_recorded_at defaults from. So the two are
// exactly equal rather than nearly, and the assertions below need no tolerance.
func publishedAt(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) time.Time {
	t.Helper()

	var at time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT server_recorded_at FROM job_status_history
		  WHERE job_id = $1 AND to_status = 'Open' ORDER BY server_recorded_at DESC LIMIT 1`,
		job).Scan(&at); err != nil {
		t.Fatalf("reading when %s was published: %v", job, err)
	}
	return at
}

// TestADraftHasNoDeadline is the boundary the fourteen days is counted from.
//
// Docs/02 §6.3 starts the clock at publication, and Docs/01 §4.1 lets a draft sit indefinitely.
// A draft carrying a deadline would be a job that expired before anybody could see it.
func TestADraftHasNoDeadline(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-draft@example.com", "+61400000680")
	job := newDraft(t, pool, customer)

	if at := deadlineOf(t, pool, job); at != nil {
		t.Errorf("a draft has a deadline of %s, want none", at)
	}
}

// TestPublishingGivesAJobTheFourteenDayBackstop is the second half of Docs/02 §6.3's "earlier of".
//
// A job with no pickup window has no pickup date to pass, so only the backstop applies. That is
// the case the "operative rule" cannot cover, and it is why the backstop exists.
func TestPublishingGivesAJobTheFourteenDayBackstop(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-backstop@example.com", "+61400000681")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	at := deadlineOf(t, pool, job)
	if at == nil {
		t.Fatal("a published job has no deadline, so nothing would ever expire it")
	}
	if want := publishedAt(t, pool, job).Add(14 * 24 * time.Hour); !at.Equal(want) {
		t.Errorf("deadline = %s, want %s — fourteen days after publication (Docs/02 §6.3)", at, want)
	}
}

// TestAPickupDateBeforeTheBackstopWins is the operative half of Docs/02 §6.3.
//
// "A job whose pickup window has gone is dead regardless of how recently it was posted." The
// fourteen days is only a backstop for jobs with distant dates, which the second case here keeps
// honest: a pickup window beyond the backstop must not extend the job past it.
func TestAPickupDateBeforeTheBackstopWins(t *testing.T) {
	pool := pgtest.DB(t)
	service := newDraftService(t, &fakeGeocoder{})

	customer := newCustomer(t, pool, "expiry-pickup@example.com", "+61400000682")

	soon := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Millisecond)
	distant := time.Now().UTC().Add(60 * 24 * time.Hour).Truncate(time.Millisecond)

	cases := map[string]struct {
		end  time.Time
		wins bool
	}{
		"a pickup window closing in two days": {soon, true},
		"a pickup window sixty days away":     {distant, false},
	}

	for name, c := range cases {
		created, err := service.CreateDraft(t.Context(), pool, customer, DraftFields{
			PickupWindow: &TimeWindow{End: c.end},
		})
		if err != nil {
			t.Fatalf("%s: creating the job: %v", name, err)
		}
		publish(t, pool, created.ID, customer)

		at := deadlineOf(t, pool, created.ID)
		if at == nil {
			t.Fatalf("%s: the published job has no deadline", name)
		}

		want := c.end
		if !c.wins {
			want = publishedAt(t, pool, created.ID).Add(14 * 24 * time.Hour)
		}
		if !at.Equal(want) {
			t.Errorf("%s: deadline = %s, want %s", name, at, want)
		}
	}
}

// TestTheDeadlineIsSetOnceAndSurvivesTheJobLeavingOpen keeps Docs/02 §6.3 counting from
// *publication* rather than from the most recent time a job happened to be Open.
//
// Negotiating → Open is an ordinary part of a job's life: it happens whenever the last active bid
// expires or is withdrawn (Docs/02 §2). If the deadline were recomputed there, a job with a slow
// trickle of bids would never expire at all — which is exactly the stale listing §6.3 is about.
//
// The extension in the middle is SHIP-70's mechanism, checked here because 000406 is what has to
// leave it alone: the trigger fills a NULL and never overwrites a value, so a deadline somebody
// moved deliberately stays moved.
func TestTheDeadlineIsSetOnceAndSurvivesTheJobLeavingOpen(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-cycle@example.com", "+61400000683")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	original := deadlineOf(t, pool, job)
	if original == nil {
		t.Fatal("the published job has no deadline")
	}

	// An extension, as SHIP-70 will make it: an ordinary UPDATE that does not touch status,
	// which 000402's guard lets straight through.
	extended := original.Add(7 * 24 * time.Hour)
	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET expires_at = $2 WHERE id = $1`, job, extended); err != nil {
		t.Fatalf("extending the deadline: %v", err)
	}

	move(t, pool, job, StatusNegotiating, User(ActorCustomer, customer))
	move(t, pool, job, StatusOpen, User(ActorCustomer, customer))

	after := deadlineOf(t, pool, job)
	if after == nil {
		t.Fatal("the job lost its deadline on the way back to Open")
	}
	if !after.Equal(extended) {
		t.Errorf("deadline = %s after returning to Open, want the extended %s — the clock does "+
			"not restart every time a job is offered again", after, extended)
	}
}

// TestExpiryClaimTakesOnlyWhatIsDue holds the claim to its predicate.
//
// The rows it must not take are the ones that would each be a distinct defect: a draft (never
// published, so never expiring), an Open job whose deadline is still ahead of it, and a job that
// has left Open (which is somebody's live delivery).
func TestExpiryClaimTakesOnlyWhatIsDue(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-claim@example.com", "+61400000684")

	due := newDraft(t, pool, customer)
	publish(t, pool, due, customer)
	overdue := newDraft(t, pool, customer)
	publish(t, pool, overdue, customer)

	notYet := newDraft(t, pool, customer)
	publish(t, pool, notYet, customer)

	draft := newDraft(t, pool, customer)

	awarded := newDraft(t, pool, customer)
	publish(t, pool, awarded, customer)
	move(t, pool, awarded, StatusAwarded, User(ActorCustomer, customer))

	// Deadlines placed by hand, because the fourteen days is not something a test can wait for.
	// The two due jobs are given different ones so that "longest overdue first" is a total
	// order rather than an accident of insertion.
	now := time.Now().UTC()
	setDeadline(t, pool, overdue, now.Add(-72*time.Hour))
	setDeadline(t, pool, due, now.Add(-time.Hour))
	setDeadline(t, pool, notYet, now.Add(72*time.Hour))
	setDeadline(t, pool, awarded, now.Add(-72*time.Hour))

	var claimed []uuid.UUID
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		rows, err := r.Query(ctx, ExpiryClaim, now, ExpiryBatch)
		if err != nil {
			return err
		}
		claimed, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatalf("running the claim: %v", err)
	}

	if len(claimed) != 2 || claimed[0] != overdue || claimed[1] != due {
		t.Errorf("the claim took %v, want the two due jobs longest-overdue first: [%s %s].\n"+
			"draft=%s not-yet-due=%s awarded=%s", claimed, overdue, due, draft, notYet, awarded)
	}
}

// TestExpireEndsAnOpenJobAsThePlatform is what one claimed job becomes.
//
// Docs/02 §2's table has one Open → Cancelled row and describes it as "job expires unclaimed".
// Everything here goes through the guard: the history row 000402 requires, the actor, the reason,
// and the event — so an expiry is indistinguishable in the record from any other transition except
// for who made it and why.
func TestExpireEndsAnOpenJobAsThePlatform(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "expiry-expire@example.com", "+61400000685")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	var expired Job
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		expired, err = service.Expire(ctx, r, job)
		return err
	}); err != nil {
		t.Fatalf("expiring the job: %v", err)
	}

	if expired.Status != StatusCancelled {
		t.Errorf("the expired job is %s, want Cancelled", expired.Status)
	}
	if statusOf(t, pool, job) != StatusCancelled {
		t.Error("the row did not move")
	}

	history, err := service.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	last := history[len(history)-1]

	switch {
	case last.From != StatusOpen || last.To != StatusCancelled:
		t.Errorf("the recorded move is %s -> %s, want Open -> Cancelled", last.From, last.To)
	case last.Actor.Type != ActorSystem:
		t.Errorf("the actor is %s, want the platform", last.Actor.Type)
	case last.Actor.ID != uuid.Nil:
		// The platform has no account and must not claim one. Move.validate refuses it,
		// and this is the assertion that would notice if that stopped being true.
		t.Errorf("the platform's transition names account %s", last.Actor.ID)
	case last.Reason != ExpiryReason:
		t.Errorf("the reason is %q, want %q", last.Reason, ExpiryReason)
	}

	if len(sink.emitted) != 1 || sink.emitted[0].Type != EventStatusChanged {
		t.Errorf("emitted %d events, want one %s — a state change nothing downstream hears "+
			"about is what the outbox seam exists to prevent", len(sink.emitted), EventStatusChanged)
	}
}

// TestExpireRefusesAJobThatIsNotOpen leans on Docs/02 §2 rather than on a list of its own.
//
// The claim already excludes anything but [LiveStatuses], so this is a second line rather than the
// first — and it is the line that would matter if a future caller expired a job it had chosen some
// other way. Awarded is the fixture because Docs/02 §6.2 makes ending one a support matter, and
// because it is the status SHIP-70a's widening must *not* have reached.
func TestExpireRefusesAJobThatIsNotOpen(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "expiry-refuse@example.com", "+61400000686")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)
	move(t, pool, job, StatusAwarded, User(ActorCustomer, customer))

	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Expire(ctx, r, job)
		return err
	})
	if err == nil {
		t.Fatal("an awarded job was expired; Docs/02 §6.2 makes ending it a support matter")
	}
	if statusOf(t, pool, job) != StatusAwarded {
		t.Error("the refused expiry moved the job anyway")
	}
}

// --- SHIP-69: the warning, forty-eight hours ahead ----------------------------------------------

// TestTheWarningClaimTakesOnlyJobsInsideTheWindow holds [ExpiryWarningClaim] to its predicate.
//
// Each row it must not take is a distinct defect, and two of them are the ones a looser query would
// get wrong quietly:
//
//   - a job whose deadline has already passed belongs to the *expiry* sweep. Warning it would send
//     "expires in two days" minutes before "has expired", from the same binary;
//   - a job already warned must not be warned again. Without that, a five-minute sweep sends five
//     hundred notifications about one job over the two days it is inside the window.
func TestTheWarningClaimTakesOnlyJobsInsideTheWindow(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "warning-claim@example.com", "+61400000694")
	now := time.Now().UTC()

	open := func(deadline time.Time) uuid.UUID {
		job := newDraft(t, pool, customer)
		publish(t, pool, job, customer)
		setDeadline(t, pool, job, deadline)
		return job
	}

	// Two inside the window, at different distances, so "most urgent first" is a total order
	// rather than an accident of insertion.
	urgent := open(now.Add(time.Hour))
	soon := open(now.Add(36 * time.Hour))

	distant := open(now.Add(5 * 24 * time.Hour))
	overdue := open(now.Add(-time.Hour))

	warned := open(now.Add(12 * time.Hour))
	markWarned(t, pool, warned, now.Add(-time.Minute))

	draft := newDraft(t, pool, customer)

	awarded := open(now.Add(12 * time.Hour))
	move(t, pool, awarded, StatusAwarded, User(ActorCustomer, customer))

	claimed := claimWarnings(t, pool, now)
	if len(claimed) != 2 || claimed[0] != urgent || claimed[1] != soon {
		t.Errorf("the claim took %v, want the two inside the window, most urgent first: [%s %s].\n"+
			"distant=%s overdue=%s already-warned=%s draft=%s awarded=%s",
			claimed, urgent, soon, distant, overdue, warned, draft, awarded)
	}
}

// TestWarnOfExpiryMarksTheJobAndEmitsTheEventTogether is SHIP-69's *Done when*: a domain event
// fires forty-eight hours before a job would expire.
//
// Both halves are asserted because either alone is a silent failure. An event with no mark warns
// the customer again every five minutes; a mark with no event marks the job as told about something
// nobody told them.
//
// **And it is not a transition.** The job is Open before and Open after, and job_status_history
// gains nothing — checked here rather than assumed, because reusing the status machinery would have
// been the easy way to write this and would have put an `Open -> Open` row in a customer's timeline.
func TestWarnOfExpiryMarksTheJobAndEmitsTheEventTogether(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "warning-emit@example.com", "+61400000695")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	deadline := testInstant.Add(30 * time.Hour)
	setDeadline(t, pool, job, deadline)

	before := len(historyOf(t, pool, job))

	warned, err := warn(t, pool, service, job)
	if err != nil {
		t.Fatalf("warning the job: %v", err)
	}

	if warned.Status != StatusOpen {
		t.Errorf("the warned job is %s, want Open — a warning is not a transition", warned.Status)
	}
	if statusOf(t, pool, job) != StatusOpen {
		t.Error("the row moved")
	}
	if after := len(historyOf(t, pool, job)); after != before {
		t.Errorf("the warning wrote %d history rows; a warning moves no job", after-before)
	}

	at := warnedAt(t, pool, job)
	if at == nil {
		t.Fatal("the job is not marked warned, so the next pass would warn it again")
	}
	if !at.Equal(testInstant) {
		t.Errorf("marked warned at %s, want the service clock's %s", at, testInstant)
	}

	if len(sink.emitted) != 1 || sink.emitted[0].Type != EventExpiryWarned {
		t.Fatalf("emitted %d events (%v), want one %s", len(sink.emitted), sink.emitted, EventExpiryWarned)
	}

	var payload expiryWarned
	if err := json.Unmarshal(sink.emitted[0].Payload, &payload); err != nil {
		t.Fatalf("the payload is not the shape this domain wrote: %v", err)
	}
	switch {
	case payload.JobID != job.String():
		t.Errorf("the event names job %s, want %s", payload.JobID, job)
	case payload.CustomerID != customer.String():
		// The consumer's whole job is to reach the customer; an event that made it read the
		// job back to find out who owns it would be a round trip per notification.
		t.Errorf("the event names customer %s, want %s", payload.CustomerID, customer)
	case !payload.ExpiresAt.Equal(deadline):
		t.Errorf("the event warns about %s, want the job's deadline %s", payload.ExpiresAt, deadline)
	case !payload.WarnedAt.Equal(testInstant):
		t.Errorf("the event was warned at %s, want %s", payload.WarnedAt, testInstant)
	}
}

// TestAJobIsWarnedOncePerDeadlineAndAgainWhenItMoves is the whole of 000407, and the second half is
// the one that would rot unnoticed.
//
// Once per deadline: a second warning against the same deadline is refused and the claim stops
// selecting the job. Again when it moves: the trigger clears the mark on any change to expires_at,
// so a customer who extends is warned again — without which SHIP-70 would silently switch off
// SHIP-69 for exactly the jobs that used it.
func TestAJobIsWarnedOncePerDeadlineAndAgainWhenItMoves(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "warning-once@example.com", "+61400000696")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	now := time.Now().UTC()
	setDeadline(t, pool, job, now.Add(24*time.Hour))

	if claimed := claimWarnings(t, pool, now); len(claimed) != 1 {
		t.Fatalf("the claim took %d jobs before any warning, want 1", len(claimed))
	}
	if _, err := warn(t, pool, service, job); err != nil {
		t.Fatalf("the first warning: %v", err)
	}

	if claimed := claimWarnings(t, pool, now); len(claimed) != 0 {
		t.Errorf("the claim took %v after the warning; a five-minute sweep would send one "+
			"notification per pass for two days", claimed)
	}
	if _, err := warn(t, pool, service, job); !errors.Is(err, ErrExpiryWarningNotDue) {
		t.Errorf("warning twice = %v, want ErrExpiryWarningNotDue", err)
	}

	// The deadline moves — which is what SHIP-70 does, and what an administrator adjusting a
	// listing would do — and the mark goes with it.
	setDeadline(t, pool, job, now.Add(20*24*time.Hour))
	if at := warnedAt(t, pool, job); at != nil {
		t.Errorf("the job is still marked warned at %s after its deadline moved; it would never "+
			"be warned again (Docs/02 §6.3)", at)
	}

	later := now.Add(19 * 24 * time.Hour)
	if claimed := claimWarnings(t, pool, later); len(claimed) != 1 || claimed[0] != job {
		t.Errorf("the claim took %v approaching the new deadline, want [%s]", claimed, job)
	}
}

// TestWarnOfExpiryRefusesToRunOutsideATransaction keeps the mark and the event atomic.
//
// Outside a transaction the mark would commit on its own and the event might not follow, which is
// a job recorded as warned about something nobody was told.
func TestWarnOfExpiryRefusesToRunOutsideATransaction(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "warning-notx@example.com", "+61400000697")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)

	if _, err := service.WarnOfExpiry(t.Context(), pool, job); !errors.Is(err, ErrNotInTransaction) {
		t.Errorf("warning through the pool = %v, want ErrNotInTransaction", err)
	}
	if at := warnedAt(t, pool, job); at != nil {
		t.Error("the refused warning marked the job anyway")
	}
}

// TestWarnOfExpiryRefusesAJobThatIsNotOpen is the predicate on the write rather than in a read
// before it.
//
// Unreachable from the sweep, which claims only [LiveStatuses] rows and holds their locks. It is the
// answer given to a caller that has not been written yet, and the alternative — succeeding quietly —
// would mark a Draft as warned and emit a notification about a job nobody can see.
func TestWarnOfExpiryRefusesAJobThatIsNotOpen(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "warning-draft@example.com", "+61400000698")
	job := newDraft(t, pool, customer)

	if _, err := warn(t, pool, service, job); !errors.Is(err, ErrExpiryWarningNotDue) {
		t.Errorf("warning a draft = %v, want ErrExpiryWarningNotDue", err)
	}
	if len(sink.emitted) != 0 {
		t.Errorf("the refused warning emitted %d events", len(sink.emitted))
	}
}

// TestTheExpiryEventsCarryNoBudget covers the two payloads SHIP-69 and SHIP-70 added.
//
// The companion of TestTheStatusChangedEventCarriesNoBudget in budget_test.go, which explains why
// an event is the copy of a job that most needs checking: it reaches Kafka and whatever is behind
// it (SHIP-134), well past the last endpoint that could have redacted anything. Two new event types
// are two new opportunities to serialise a job wholesale, and the source-parsing test cannot see a
// leak that arrives by embedding a whole [Job] in a payload.
//
// Read out of the outbox rather than from a recording sink, because what a consumer sees is the
// stored jsonb.
func TestTheExpiryEventsCarryNoBudget(t *testing.T) {
	pool := pgtest.DB(t)
	service := NewService(events.NewOutbox(), clock.NewFixed(testInstant), nil)

	customer := newCustomer(t, pool, "expiry-budget@example.com", "+61400000699")

	created, err := newDraftService(t, &fakeGeocoder{}).CreateDraft(t.Context(), pool, customer,
		DraftFields{BudgetCents: money(150_000)})
	if err != nil {
		t.Fatalf("creating the job: %v", err)
	}
	publish(t, pool, created.ID, customer)
	setDeadline(t, pool, created.ID, testInstant.Add(24*time.Hour))

	if _, err := warn(t, pool, service, created.ID); err != nil {
		t.Fatalf("warning the job: %v", err)
	}
	if _, err := extend(t, pool, service, customer, created.ID); err != nil {
		t.Fatalf("extending the job: %v", err)
	}

	rows, err := pool.Query(t.Context(),
		`SELECT event_type, payload::text FROM outbox WHERE aggregate_id = $1`, created.ID)
	if err != nil {
		t.Fatalf("reading the events back: %v", err)
	}
	defer rows.Close()

	// The publication went through `publish`, which writes to a recording sink rather than the
	// outbox, so exactly the two events this ticket added are in the table.
	seen := map[string]bool{}
	for rows.Next() {
		var eventType, payload string
		if err := rows.Scan(&eventType, &payload); err != nil {
			t.Fatalf("scanning an event: %v", err)
		}
		seen[eventType] = true
		if mentionsBudget(payload) || strings.Contains(payload, "150000") {
			t.Errorf("%s carries the customer's budget, which travels to every consumer there "+
				"will ever be (Docs/01 §4.3):\n%s", eventType, payload)
		}
	}
	for _, want := range []string{EventExpiryWarned, EventExpiryExtended} {
		if !seen[want] {
			t.Errorf("no %s event reached the outbox, so this test proves nothing about it", want)
		}
	}
}

// --- SHIP-70a: a live job is not only an Open one -----------------------------------------------

// TestLiveStatusesAgreeInGoAndSQL is Docs/10 §3.4's pairing applied to a predicate.
//
// [LiveStatuses] is a SQL fragment read by two claims and one write; [offered] is the Go form the
// extend endpoint asks. They are one rule, and the failure of a drift is silent in both directions:
// a status live in SQL but not in Go warns a customer about a job the endpoint then refuses to
// extend, and a status live in Go but not in SQL extends a job no sweep is watching.
//
// So the fragment is parsed rather than restated, and every one of Docs/02 §1's twelve statuses is
// put to both — which is what makes a status added to the document, and to one of these, fail here.
func TestLiveStatusesAgreeInGoAndSQL(t *testing.T) {
	inSQL := map[Status]bool{}
	for _, quoted := range strings.Split(strings.Trim(LiveStatuses, "()"), ",") {
		inSQL[Status(strings.Trim(strings.TrimSpace(quoted), "'"))] = true
	}

	if len(inSQL) == 0 {
		t.Fatalf("LiveStatuses parsed to nothing from %q", LiveStatuses)
	}

	for _, status := range Statuses {
		if inSQL[status] != offered(status) {
			t.Errorf("%s is live in SQL=%t and in Go=%t; %q and offered() are one rule",
				status, inSQL[status], offered(status), LiveStatuses)
		}
	}

	// Named rather than derived, because the point of the ticket is which two they are.
	if !offered(StatusOpen) || !offered(StatusNegotiating) {
		t.Errorf("Docs/02 §2's expiry row names Open and Negotiating; offered() takes %t and %t",
			offered(StatusOpen), offered(StatusNegotiating))
	}
	if offered(StatusAwarded) || offered(StatusDraft) {
		t.Error("a Draft has no deadline and an Awarded job is somebody's live delivery")
	}
}

// TestTheExpiryClaimTakesANegotiatingJobOnItsOwnDeadline is SHIP-70a's *Done when*.
//
// Docs/02 §6.3's deadline belongs to the job. Before this ticket both sweeps read `status = 'Open'`,
// which was the whole of "a live job" when SHIP-68 and SHIP-69 were written and stopped being so at
// SHIP-90 — so a job with one unanswered offer sat at Negotiating, out of sight of the sweep, and
// its deadline was enforced only once its last offer had lapsed and returned it to Open.
//
// The two jobs here are the same job in the two presentations, given the same overdue deadline, so
// the assertion is about the status and about nothing else.
func TestTheExpiryClaimTakesANegotiatingJobOnItsOwnDeadline(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newCustomer(t, pool, "expiry-negotiating@example.com", "+61400000670")
	now := time.Now().UTC()

	open := newDraft(t, pool, customer)
	publish(t, pool, open, customer)

	negotiating := newDraft(t, pool, customer)
	publish(t, pool, negotiating, customer)
	move(t, pool, negotiating, StatusNegotiating, User(ActorCustomer, customer))

	// The deadline survives the move to Negotiating: 000406's trigger fills a NULL and never
	// overwrites, so this is the deadline publication gave it. Asserted rather than assumed,
	// because a claim that could not see the job would look identical to a job with no deadline.
	if deadlineOf(t, pool, negotiating) == nil {
		t.Fatal("the Negotiating job lost its deadline on the way out of Open")
	}

	setDeadline(t, pool, open, now.Add(-time.Hour))
	setDeadline(t, pool, negotiating, now.Add(-2*time.Hour))

	claimed := claimExpiries(t, pool, now)
	if len(claimed) != 2 || claimed[0] != negotiating || claimed[1] != open {
		t.Fatalf("the claim took %v, want both live jobs longest-overdue first: [%s %s] — "+
			"a job somebody has bid on expires on its own deadline (Docs/02 §2)",
			claimed, negotiating, open)
	}
}

// TestExpireEndsANegotiatingJob is the transition half, and it is the half the claim cannot prove.
//
// Docs/02 §2's expiry row now reads `Open / Negotiating → Cancelled`, and [Service.Transition] is
// what has to agree — the guard already permitted `Negotiating → Cancelled` for SHIP-64's
// cancellation, so this asserts the platform reaches it rather than that it exists.
func TestExpireEndsANegotiatingJob(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "expiry-negotiating-end@example.com", "+61400000671")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)
	move(t, pool, job, StatusNegotiating, User(ActorCustomer, customer))

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Expire(ctx, r, job)
		return err
	}); err != nil {
		t.Fatalf("expiring the Negotiating job: %v", err)
	}

	if statusOf(t, pool, job) != StatusCancelled {
		t.Fatalf("the job is %s, want Cancelled", statusOf(t, pool, job))
	}

	history, err := service.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	last := history[len(history)-1]

	switch {
	case last.From != StatusNegotiating || last.To != StatusCancelled:
		t.Errorf("the recorded move is %s -> %s, want Negotiating -> Cancelled", last.From, last.To)
	case last.Actor.Type != ActorSystem:
		t.Errorf("the actor is %s, want the platform", last.Actor.Type)
	case last.Reason != ExpiryReason:
		t.Errorf("the reason is %q, want %q", last.Reason, ExpiryReason)
	}
}

// TestTheWarningClaimTakesANegotiatingJob keeps the two sweeps reading the same set.
//
// SHIP-70a's *Done when* names both — "both the expiry and the expiry-warning claims match it" —
// and the asymmetry is what would be worst: a job warned in a status the expiry sweep cannot see is
// told it is about to expire and then never expires, which is the failure this ticket ends rather
// than one it should invert.
func TestTheWarningClaimTakesANegotiatingJob(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "warning-negotiating@example.com", "+61400000672")
	now := time.Now().UTC()

	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)
	move(t, pool, job, StatusNegotiating, User(ActorCustomer, customer))
	setDeadline(t, pool, job, now.Add(24*time.Hour))

	if claimed := claimWarnings(t, pool, now); len(claimed) != 1 || claimed[0] != job {
		t.Fatalf("the warning claim took %v, want [%s]", claimed, job)
	}

	// And the write behind the claim carries the same predicate, which is the second copy of
	// the rule and the one a widening could forget: postgresStore.markExpiryWarned refuses a row
	// it would not have claimed, so a claim without it succeeds and marks nothing.
	warned, err := warn(t, pool, service, job)
	if err != nil {
		t.Fatalf("warning the Negotiating job: %v", err)
	}
	if warned.Status != StatusNegotiating {
		t.Errorf("the warned job is %s, want Negotiating — a warning is not a transition", warned.Status)
	}
	if at := warnedAt(t, pool, job); at == nil {
		t.Error("the job is not marked warned, so the next pass would warn it again")
	}
}

// --- fixtures ----------------------------------------------------------------------------------

// claimExpiries runs [ExpiryClaim] the way cmd/worker does, and returns what it took.
func claimExpiries(t *testing.T, pool *pgxpool.Pool, at time.Time) []uuid.UUID {
	t.Helper()

	var claimed []uuid.UUID
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		rows, err := r.Query(ctx, ExpiryClaim, at, ExpiryBatch)
		if err != nil {
			return err
		}
		claimed, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatalf("running the expiry claim: %v", err)
	}
	return claimed
}

// warn runs a warning in a transaction, which is what Service.WarnOfExpiry requires.
func warn(t *testing.T, pool *pgxpool.Pool, svc *Service, job uuid.UUID) (Job, error) {
	t.Helper()

	var warned Job
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		warned, err = svc.WarnOfExpiry(ctx, r, job)
		return err
	})
	return warned, err
}

// claimWarnings runs [ExpiryWarningClaim] the way cmd/worker does, and returns what it took.
//
// The horizon is computed here exactly as warnOfExpiry computes it, so a test that disagreed with
// the worker about where the window ends would be testing something the worker never asks.
func claimWarnings(t *testing.T, pool *pgxpool.Pool, at time.Time) []uuid.UUID {
	t.Helper()

	var claimed []uuid.UUID
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		rows, err := r.Query(ctx, ExpiryWarningClaim, at, at.Add(ExpiryWarning), ExpiryBatch)
		if err != nil {
			return err
		}
		claimed, err = pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
		return err
	}); err != nil {
		t.Fatalf("running the warning claim: %v", err)
	}
	return claimed
}

// warnedAt reads a job's expiry_warned_at straight out of the table, bypassing the domain.
func warnedAt(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) *time.Time {
	t.Helper()

	var at *time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT expiry_warned_at FROM jobs WHERE id = $1`, job).Scan(&at); err != nil {
		t.Fatalf("reading the warning mark of %s: %v", job, err)
	}
	return at
}

// markWarned puts a job's expiry_warned_at where a test needs it, without emitting anything.
func markWarned(t *testing.T, pool *pgxpool.Pool, job uuid.UUID, at time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET expiry_warned_at = $2 WHERE id = $1`, job, at); err != nil {
		t.Fatalf("marking %s warned: %v", job, err)
	}
}

// historyOf is every recorded transition for a job, read straight out of the table.
func historyOf(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) []string {
	t.Helper()

	rows, err := pool.Query(t.Context(),
		`SELECT from_status || '->' || to_status FROM job_status_history
		  WHERE job_id = $1 ORDER BY server_recorded_at, id`, job)
	if err != nil {
		t.Fatalf("reading the history of %s: %v", job, err)
	}
	defer rows.Close()

	moves, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		t.Fatalf("scanning the history of %s: %v", job, err)
	}
	return moves
}

// setDeadline puts a job's expires_at where a test needs it.
//
// A plain UPDATE, which 000402's guard permits because it does not touch status and 000406's
// trigger ignores because the job is already Open. That is the same statement SHIP-70's extend
// endpoint will make, which is why the fixture is honest rather than a way around anything.
func setDeadline(t *testing.T, pool *pgxpool.Pool, job uuid.UUID, at time.Time) {
	t.Helper()

	if _, err := pool.Exec(t.Context(),
		`UPDATE jobs SET expires_at = $2 WHERE id = $1`, job, at); err != nil {
		t.Fatalf("setting the deadline of %s: %v", job, err)
	}
}
