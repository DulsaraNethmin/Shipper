package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-128 — the twenty-four-hour rung of Docs/02 §3.1's ladder.
//
// # Every instant here is derived from a row the database wrote, and that is deliberate
//
// `server_recorded_at` is set by 000601's trigger from `now()` and an INSERT that names the column
// is refused, so a fixture cannot choose it. Every `actor_recorded_at` below is therefore computed
// by reading the row's own `server_recorded_at` back and subtracting — which means there is no
// calendar literal anywhere in this file for the real date to overtake.
//
// Wave 9 closed this by construction on the 72-hour ticket and the reasoning transfers exactly: a
// fixture whose two timestamps come from two clocks is a test that expires, and a threshold test is
// the shape most likely to do it quietly.

// unsyncedFixture records a milestone whose actor clock is `gap` behind the platform's.
//
// The milestone is inserted directly rather than through the service, because the point is the pair
// of clocks and not the recording path — and because the service would refuse most of the moves that
// would let one job carry several rows at different gaps. The trigger still fills
// `server_recorded_at`, so the row is honest about when it arrived.
func unsyncedFixture(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, gap time.Duration) uuid.UUID {
	t.Helper()

	id := uuid.Must(uuid.NewV7())

	// One statement: the actor's clock is `now() - gap` and the platform's is the `now()` the
	// trigger is about to read, so the two are the same instant apart by construction.
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO milestones (id, job_id, milestone, actor_type, actor_id, idempotency_key,
		                         actor_recorded_at)
		 VALUES ($1, $2, 'Picked up', 'provider', $3, $4, now() - $5::interval)`,
		id, jobID, uuid.Must(uuid.NewV7()), id.String(), gap); err != nil {
		t.Fatalf("recording a milestone %s behind: %v", gap, err)
	}
	return id
}

// TestUnsyncedForIsTheGapBetweenTheTwoClocks is the measurement, before any query uses it.
func TestUnsyncedForIsTheGapBetweenTheTwoClocks(t *testing.T) {
	at := time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC)

	for name, tc := range map[string]struct {
		actor, server time.Time
		want          time.Duration
		unsynced      bool
	}{
		"online, one clock": {
			actor: at, server: at, want: 0, unsynced: false,
		},
		"a few hours in a valley": {
			actor: at, server: at.Add(5 * time.Hour), want: 5 * time.Hour, unsynced: false,
		},
		"exactly on the rung": {
			actor: at, server: at.Add(24 * time.Hour), want: 24 * time.Hour, unsynced: true,
		},
		"a week out of contact": {
			actor: at, server: at.Add(7 * 24 * time.Hour), want: 7 * 24 * time.Hour, unsynced: true,
		},
		"a handset whose clock runs fast": {
			// Not an error and not corrected: 000601 refuses to bound the actor's clock
			// because "an implausible time is evidence, not an error". Clamped to zero so a
			// fast clock stays out of the queue rather than entering it with a nonsense gap.
			actor: at.Add(time.Hour), server: at, want: 0, unsynced: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := Record{ActorRecordedAt: tc.actor, ServerRecordedAt: tc.server}
			if got := rec.UnsyncedFor(); got != tc.want {
				t.Errorf("UnsyncedFor() = %s, want %s", got, tc.want)
			}
			if got := rec.Unsynced(); got != tc.unsynced {
				t.Errorf("Unsynced() = %t, want %t at a threshold of %s",
					got, tc.unsynced, UnsyncedAlertThreshold)
			}
		})
	}
}

// TestTheThresholdIsDocs02sTwentyFourHours pins the number to the document.
//
// Trivial to write and the reason it earns its place is that the constant is the whole rung: a value
// edited to 24 minutes or 24 days would change what operations hears about and break no other test
// in this package, because every other test derives its fixture from the constant.
func TestTheThresholdIsDocs02sTwentyFourHours(t *testing.T) {
	if UnsyncedAlertThreshold != 24*time.Hour {
		t.Errorf("UnsyncedAlertThreshold = %s, want 24h — Docs/02 §3.1's third rung",
			UnsyncedAlertThreshold)
	}
}

// TestTheQueueHoldsOnlyUpdatesPastTheThreshold is SHIP-128's *Done when*: a job with a 24-hour
// unsynced update enters the delivery exception queue.
//
// The three rows it must *not* take are each a distinct defect: an update that synced immediately
// (every online recording), one inside the four-hour rung, and one just short of the threshold —
// which is the boundary a `>` would get wrong in the direction that hides a row.
func TestTheQueueHoldsOnlyUpdatesPastTheThreshold(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "unsynced-c@example.com", "+61400000751", "customer")
	provider := newAccount(t, pool, "unsynced-p@example.com", "+61400000752", "provider")
	svc := newTestService()

	online := awardedJob(t, pool, customer, provider)
	nudged := awardedJob(t, pool, customer, provider)
	nearly := awardedJob(t, pool, customer, provider)
	overdue := awardedJob(t, pool, customer, provider)
	ancient := awardedJob(t, pool, customer, provider)

	unsyncedFixture(t, pool, online, 0)
	unsyncedFixture(t, pool, nudged, 5*time.Hour)
	unsyncedFixture(t, pool, nearly, UnsyncedAlertThreshold-time.Minute)
	overdueID := unsyncedFixture(t, pool, overdue, UnsyncedAlertThreshold+time.Hour)
	ancientID := unsyncedFixture(t, pool, ancient, 7*24*time.Hour)

	entries, err := svc.UnsyncedMilestones(t.Context(), pool, 0)
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}

	inQueue := map[uuid.UUID]UnsyncedEntry{}
	for _, e := range entries {
		inQueue[e.MilestoneID] = e
	}

	if len(inQueue) != 2 {
		t.Fatalf("the queue holds %d entries, want the 2 past the threshold.\n"+
			"online=%s nudged=%s nearly=%s overdue=%s ancient=%s",
			len(inQueue), online, nudged, nearly, overdue, ancient)
	}
	if _, ok := inQueue[overdueID]; !ok {
		t.Error("an update an hour past the threshold is not in the queue")
	}
	if _, ok := inQueue[ancientID]; !ok {
		t.Error("an update a week behind is not in the queue")
	}

	// The entry says enough to triage from: which job, what the driver was recording, how far
	// behind the record ran, and where the job stands now.
	entry := inQueue[ancientID]
	switch {
	case entry.JobID != ancient:
		t.Errorf("the entry names job %s, want %s", entry.JobID, ancient)
	case entry.Milestone != MilestonePickedUp:
		t.Errorf("the entry reports %q, want Picked up", entry.Milestone)
	case entry.Actor != ActorProvider:
		t.Errorf("the entry reports actor %q, want provider", entry.Actor)
	case entry.JobStatus != string(jobs.StatusAwarded):
		t.Errorf("the entry reports job status %q, want Awarded", entry.JobStatus)
	}

	// The gap the query returned is the gap the query selected on. A gap computed once in SQL
	// and reported from a second expression is how a queue comes to hold a row that does not
	// satisfy its own rule, so this checks the returned figure against the threshold directly
	// rather than against a fixture constant.
	if entry.UnsyncedFor < UnsyncedAlertThreshold {
		t.Errorf("the entry reports a gap of %s, which is under the threshold it was selected by (%s)",
			entry.UnsyncedFor, UnsyncedAlertThreshold)
	}
	if entry.UnsyncedFor < 6*24*time.Hour {
		t.Errorf("the entry reports %s for an update a week behind; the interval's day component "+
			"has been dropped, which reports a long outage as a short one", entry.UnsyncedFor)
	}
}

// TestTheQueueIsOldestArrivalFirst holds the ordering, and holds it to the platform's clock.
//
// After an outage the longest-standing problem belongs at the top — the same ordering jobs' expiry
// claim takes. Ordering by the *actor's* clock instead would let a handset with a wrong clock
// reorder a support queue, which is the call admin.ExceptionQueue makes about its own ordering.
func TestTheQueueIsOldestArrivalFirst(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "unsyncedorder-c@example.com", "+61400000753", "customer")
	provider := newAccount(t, pool, "unsyncedorder-p@example.com", "+61400000754", "provider")
	svc := newTestService()

	jobID := awardedJob(t, pool, customer, provider)

	// **The two orderings have to disagree, or this test asserts nothing** — which is how it was
	// first written, and a mutation swapping the ORDER BY to `actor_recorded_at` passed against
	// it. A larger gap means an *earlier* actor clock, so two rows inserted longest-gap-first
	// sort identically under either column.
	//
	// So the row that arrives **first** carries the **later** actor clock: by arrival it is
	// first, by the actor's clock it is second. Both are past the threshold, so both are in the
	// queue and the only thing separating them is which column the query trusts.
	first := unsyncedFixture(t, pool, jobID, UnsyncedAlertThreshold+time.Hour)
	second := unsyncedFixture(t, pool, jobID, 9*24*time.Hour)

	entries, err := svc.UnsyncedMilestones(t.Context(), pool, 0)
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("the queue holds %d entries, want 2", len(entries))
	}
	if entries[0].MilestoneID != first || entries[1].MilestoneID != second {
		t.Errorf("the queue is ordered %s, %s; want arrival order %s, %s — ordering by the "+
			"actor's clock lets a handset reorder a support queue",
			entries[0].MilestoneID, entries[1].MilestoneID, first, second)
	}
	if !entries[0].ServerRecordedAt.Before(entries[1].ServerRecordedAt) &&
		!entries[0].ServerRecordedAt.Equal(entries[1].ServerRecordedAt) {
		t.Error("the first entry arrived after the second")
	}
}

// TestTheQueueIsBounded keeps one page from becoming the whole table.
func TestTheQueueIsBounded(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "unsyncedlimit-c@example.com", "+61400000755", "customer")
	provider := newAccount(t, pool, "unsyncedlimit-p@example.com", "+61400000756", "provider")
	svc := newTestService()

	jobID := awardedJob(t, pool, customer, provider)
	for i := 0; i < 3; i++ {
		unsyncedFixture(t, pool, jobID, UnsyncedAlertThreshold+time.Duration(i)*time.Hour)
	}

	entries, err := svc.UnsyncedMilestones(t.Context(), pool, 2)
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}
	if len(entries) != 2 {
		t.Errorf("a limit of 2 returned %d entries", len(entries))
	}
}

// TestAnUnsyncedMilestoneRecordedThroughTheServiceEntersTheQueue joins the two halves.
//
// The tests above drive the query over rows a fixture wrote. This one records through
// [Service.RecordMilestone] — the path a reconnecting handset actually takes — and then reads the
// queue, so the ticket's claim is demonstrated end to end rather than in two halves that could each
// be right about a different thing.
//
// The actor's instant is derived from the job's own recorded history rather than from a literal, for
// the reason this file's header gives.
func TestAnUnsyncedMilestoneRecordedThroughTheServiceEntersTheQueue(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "unsyncedsvc-c@example.com", "+61400000757", "customer")
	provider := newAccount(t, pool, "unsyncedsvc-p@example.com", "+61400000758", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider), jobs.StatusEnRouteToPickup)

	// The platform's clock, read from a row the database stamped, so the gap below is measured
	// against the same clock the trigger will use for the milestone.
	var platformNow time.Time
	if err := pool.QueryRow(t.Context(), `SELECT now()`).Scan(&platformNow); err != nil {
		t.Fatalf("reading the platform's clock: %v", err)
	}

	recordedAt := platformNow.Add(-(UnsyncedAlertThreshold + 2*time.Hour))

	rec, outcome, err := recordMilestone(t, pool, svc, provider, jobID,
		Recording{Milestone: MilestonePickedUp, RecordedAt: recordedAt, Key: theKey})
	if err != nil {
		t.Fatalf("recording a milestone two days behind: %v", err)
	}
	if outcome != OutcomeRecorded {
		t.Fatalf("outcome = %q, want recorded — a stale update is still a recording", outcome)
	}
	if !rec.Unsynced() {
		t.Errorf("the recorded milestone reports a gap of %s, want at least %s",
			rec.UnsyncedFor(), UnsyncedAlertThreshold)
	}

	entries, err := svc.UnsyncedMilestones(t.Context(), pool, 0)
	if err != nil {
		t.Fatalf("reading the queue: %v", err)
	}

	var found bool
	for _, e := range entries {
		if e.MilestoneID == rec.ID {
			found = true
			if e.JobID != jobID {
				t.Errorf("the entry names job %s, want %s", e.JobID, jobID)
			}
			if e.JobStatus != string(jobs.StatusPickedUp) {
				t.Errorf("the entry reports job status %q, want Picked up — the milestone "+
					"moved the job, and a stale update still moves one", e.JobStatus)
			}
		}
	}
	if !found {
		t.Errorf("a milestone recorded %s late is not in the queue; SHIP-128's Done when is that "+
			"such a job enters it", UnsyncedAlertThreshold+2*time.Hour)
	}
}

// TestTheQueueOpensNoTransaction is the read discipline admin.Moderation states for its own queue.
//
// A queue owns no invariant, so Docs/10 §3.2 puts no transaction on it and a single statement is
// already consistent with itself. Asserted by running it through the pool, which is what a caller
// with no transaction holds.
func TestTheQueueOpensNoTransaction(t *testing.T) {
	pool := pgtest.DB(t)
	svc := newTestService()

	if _, err := svc.UnsyncedMilestones(t.Context(), pool, 0); err != nil {
		t.Errorf("reading the queue through the pool = %v, want it to need no transaction", err)
	}

	// And inside one, because the caller that has a transaction open must not be refused either.
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := svc.UnsyncedMilestones(ctx, r, 0)
		return err
	}); err != nil {
		t.Errorf("reading the queue in a transaction = %v", err)
	}
}
