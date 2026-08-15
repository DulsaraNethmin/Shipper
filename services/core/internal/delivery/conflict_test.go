package delivery

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-113 — the queued update that contradicts an administrative action.
//
// Docs/02 §3.1's fourth bullet, in full, because every assertion in this file is one clause of it:
//
//	A queued update that contradicts an administrative action loses. If a driver records
//	"Delivered" offline while an administrator cancels the job, the cancellation stands, the
//	attempt is retained in history, and the app must show the driver what happened rather than
//	silently discarding their work.
//
// Three of those four clauses are the platform's and are tested here — the cancellation stands, the
// attempt is retained, and it loses. The fourth is the client's (SHIP-132), and what it reconciles
// against is the job resource, which is where §3.1 puts reconciliation.
//
// # Why every fixture goes through Disputed
//
// Docs/02 §2 has no `Awarded → Cancelled` row, and none from any delivery status. Once the goods are
// somebody's responsibility the route out is `Disputed`, and an administrator resolving it — §6.2 is
// explicit that "after Picked up this does not apply… that is a support case under §3, not a state
// transition". So an administrator ending a live delivery is two moves, and both are here rather
// than a fixture writing `Cancelled` straight into the column, which the guard would refuse anyway.

// adminMove makes a transition as an administrator, with the reason Docs/01 §3 requires of one.
//
// Not [moveJob], which passes no reason: jobs.Move.validate refuses an administrator without one,
// and that refusal is the whole of why an administrative action is distinguishable in the record
// afterwards. A test that routed round it would be demonstrating SHIP-113 against a move no
// administrator could actually make.
func adminMove(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, reason string, to ...jobs.Status) {
	t.Helper()

	svc := jobs.NewService(events.NewOutbox(), clock.NewFixed(testInstant), nil)
	admin := uuid.Must(uuid.NewV7())

	for _, status := range to {
		err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			_, err := svc.Transition(ctx, r, jobs.Move{
				JobID:  jobID,
				To:     status,
				Actor:  jobs.User(jobs.ActorAdmin, admin),
				Reason: reason,
			})
			return err
		})
		if err != nil {
			t.Fatalf("moving %s to %s as an administrator: %v", jobID, status, err)
		}
	}
}

// TestADeliveredMilestoneOnACancelledJobIsRetained is Docs/02 §3.1's own example, run.
//
// A driver records the delivery in a place with no signal. While they are out of contact an
// administrator disputes the job and resolves it as cancelled. The phone reconnects hours later and
// syncs.
//
// Before SHIP-113 this answered `delivery_milestone_not_permitted` and **rolled the transaction
// back**, which destroyed the driver's record of a delivery they had actually made and the evidence
// attached to it. The evidence is the part worth stating twice: the exception is written before the
// move is attempted, so a rollback took that too.
func TestADeliveredMilestoneOnACancelledJobIsRetained(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "conflict-c@example.com", "+61400000731", "customer")
	provider := newAccount(t, pool, "conflict-p@example.com", "+61400000732", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	// The delivery is under way, and the driver is at the drop-off with no signal.
	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit)

	actedAt := testInstant.Add(-3 * time.Hour)

	// Meanwhile, an administrator ends the job.
	adminMove(t, pool, jobID, "Customer reported the goods were never collected.",
		jobs.StatusDisputed, jobs.StatusCancelled)

	svc := serviceReadingProofFrom(newRecordingObjects())

	record, outcome, err := recordWithException(t, pool, svc, provider, jobID,
		Recording{
			Milestone:     MilestoneDelivered,
			RecordedAt:    actedAt,
			Key:           theKey,
			RecipientName: "R. Chen",
			DeliveryNote:  "Left with reception",
		}, ExceptionRecipientObjected)
	if err != nil {
		t.Fatalf("RecordMilestone(delivered, job cancelled) = %v, want the attempt retained "+
			"rather than refused (Docs/02 §3.1)", err)
	}
	if outcome != OutcomeOverruled {
		t.Fatalf("outcome = %q, want overruled", outcome)
	}

	// **The attempt is retained in history.** Read back through the pool, because what the
	// service returned says nothing about what committed — and committing is the entire ticket.
	var storedMilestone, storedRecipient, storedNote string
	var storedActorRecordedAt time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT milestone, recipient_name, delivery_note, actor_recorded_at
		   FROM milestones WHERE id = $1`, record.ID).
		Scan(&storedMilestone, &storedRecipient, &storedNote, &storedActorRecordedAt); err != nil {
		t.Fatalf("reading the overruled milestone back: %v — the transaction rolled back, which "+
			"is exactly what SHIP-113 exists to stop", err)
	}
	switch {
	case storedMilestone != string(MilestoneDelivered):
		t.Errorf("the committed row is %q, want Delivered", storedMilestone)
	case !storedActorRecordedAt.Equal(actedAt):
		t.Errorf("actor_recorded_at = %s, want %s — the driver's clock is not corrected because "+
			"the job moved on without them", storedActorRecordedAt, actedAt)
	case storedRecipient != "R. Chen" || storedNote != "Left with reception":
		t.Errorf("the retained row lost Docs/01 §4.4's delivery details: recipient %q, note %q",
			storedRecipient, storedNote)
	}

	// And the evidence with it. A reasoned exception is the driver's account of why there is no
	// photograph, and it is the half a rollback took silently.
	if n := proofCount(t, pool, jobID); n != 1 {
		t.Errorf("%d evidence rows, want 1 — the retained attempt lost the reason recorded with it", n)
	}

	// **The cancellation stands.** Nothing here re-opens the job, moves it, or annotates it.
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusCancelled) {
		t.Errorf("the job is %q, want Cancelled — the administrative action must win", got)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusDelivered); n != 0 {
		t.Errorf("%d transitions into Delivered, want 0 — the milestone lost, so it moved nothing "+
			"and wrote no job_status_history row", n)
	}
}

// TestAMilestoneOnADisputedJobIsRetained is the freeze rather than the ending.
//
// Docs/02 §3 says "a dispute freezes automatic completion until an administrator resolves it", and
// §2 gives `Disputed` only `Completed` and `Cancelled` — so no milestone can ever be reached from
// it. A driver still in the vehicle when a dispute is opened is the ordinary way this happens, and
// their next queued update must not be thrown away while somebody investigates.
func TestAMilestoneOnADisputedJobIsRetained(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "frozen-c@example.com", "+61400000733", "customer")
	provider := newAccount(t, pool, "frozen-p@example.com", "+61400000734", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp)
	adminMove(t, pool, jobID, "Customer says the pallet count is wrong.", jobs.StatusDisputed)

	record, outcome, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestoneInTransit, RecordedAt: testInstant.Add(-time.Hour), Key: theKey})
	if err != nil {
		t.Fatalf("RecordMilestone(in transit, job disputed) = %v, want it retained", err)
	}
	if outcome != OutcomeOverruled {
		t.Errorf("outcome = %q, want overruled", outcome)
	}

	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows, want 1 — the driver's update was discarded", n)
	}
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusDisputed) {
		t.Errorf("the job is %q, want Disputed — recording against a frozen job unfroze it", got)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusInTransit); n != 0 {
		t.Errorf("%d transitions into In transit, want 0", n)
	}
	if record.ID == uuid.Nil {
		t.Error("the retained milestone has no identifier")
	}
}

// TestAbsorptionIsAnsweredBeforeAnOverruling is the ordering in [testJobs.refusal], and the one
// case where getting it wrong would be invisible.
//
// A job that reached 'Delivered' and was then disputed satisfies both tests for a queued 'Picked
// up': the job **has** been past the pickup, *and* it now stands somewhere with no way back. It is
// the first of those. The work was done and recorded in sequence, so the driver's update is late
// (SHIP-112) rather than overruled (SHIP-113) — and telling them their pickup lost to an
// administrative action would be false.
//
// Both answers retain the row, so nothing about the stored record distinguishes them. Only the
// outcome does, which is why this is asserted on the outcome and could not be asserted on the table.
func TestAbsorptionIsAnsweredBeforeAnOverruling(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "order-c@example.com", "+61400000735", "customer")
	provider := newAccount(t, pool, "order-p@example.com", "+61400000736", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit)
	adminMove(t, pool, jobID, "Customer disputes the condition of the goods.", jobs.StatusDisputed)

	_, outcome, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestonePickedUp, RecordedAt: testInstant.Add(-4 * time.Hour), Key: theKey})
	if err != nil {
		t.Fatalf("RecordMilestone(late pickup on a disputed job) = %v, want it absorbed", err)
	}
	if outcome != OutcomeAbsorbed {
		t.Errorf("outcome = %q, want absorbed — the job really was past the pickup, and the "+
			"dispute afterwards does not turn a late milestone into an overruled one", outcome)
	}
}

// TestAJobStillOnItsDeliveryDoesNotRetainARefusedMilestone is the boundary SHIP-113 had to not
// cross, asserted from the side that would have moved.
//
// Retention is for a job that has **left** the delivery. A job that is merely not there yet is
// SHIP-112's premature case and stays a rollback, because the identical request succeeds once the
// delivery catches up — so keeping it would store an attempt that a retry was about to record
// properly, and the two rows would both be true and one would be noise.
//
// # The case this test deliberately cannot reach, and where it is held instead
//
// The tempting implementation of this ticket is "can the job still reach the target?", and it would
// have swept in a status the delivery legitimately *skipped*: Docs/02 §2 makes 'Driver assigned'
// skippable, so a job at 'In transit' can never reach it, and a reachability test would call that an
// administrative conflict. It cannot be driven from here — [Recording.problems] refuses
// 'Driver assigned' outright, because a driver is put on a job through its own endpoint — so
// cmd/api's TestTheOutOfDeliveryStatusesAreWhatTheTransitionTableSays holds that half, against
// jobs.Permitted directly. Named here so the gap is a decision rather than an oversight.
func TestAJobStillOnItsDeliveryDoesNotRetainARefusedMilestone(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "stillon-c@example.com", "+61400000737", "customer")
	provider := newAccount(t, pool, "stillon-p@example.com", "+61400000738", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider), jobs.StatusEnRouteToPickup)

	_, outcome, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestoneInTransit, Key: theKey})
	if !errors.Is(err, ErrMilestoneNotPermitted) {
		t.Fatalf("RecordMilestone(premature, job live) = %v (outcome %q), want "+
			"ErrMilestoneNotPermitted — the job is still on its delivery, so this is SHIP-112's "+
			"question and not SHIP-113's", err, outcome)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestone rows survived, want 0 — only a job that has left the delivery "+
			"retains a refused milestone", n)
	}
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusEnRouteToPickup) {
		t.Errorf("the job is %q, want En route to pickup", got)
	}
}

// TestAnOverruledMilestoneIsStillRecordedOncePerKey is the retry path over a retained row.
//
// A phone that syncs into a cancelled job and then loses the response retries with the same key. It
// must get the same row back and write nothing further — retention must not become a second way to
// put two rows under one key, which is the same thing SHIP-112's absorption had to prove.
//
// The answer is [OutcomeAlreadyRecorded] rather than [OutcomeOverruled], exactly as it is for an
// absorbed milestone: what the client is told is what the platform holds, and the platform holds one
// row. [Service.alreadyRecorded] deliberately does not re-evaluate the move to find out which of the
// three ways it was written.
func TestAnOverruledMilestoneIsStillRecordedOncePerKey(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "conflictretry-c@example.com", "+61400000739", "customer")
	provider := newAccount(t, pool, "conflictretry-p@example.com", "+61400000740", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	svc := newTestService()

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp)
	adminMove(t, pool, jobID, "Vehicle broke down; job reassigned offline.",
		jobs.StatusDisputed, jobs.StatusCancelled)

	queued := Recording{
		Milestone:  MilestoneInTransit,
		RecordedAt: testInstant.Add(-2 * time.Hour),
		Key:        theKey,
	}

	first, outcome, err := recordMilestone(t, pool, svc, provider, jobID, queued)
	if err != nil {
		t.Fatalf("the first sync: %v", err)
	}
	if outcome != OutcomeOverruled {
		t.Fatalf("the first sync = %q, want overruled", outcome)
	}

	second, outcome, err := recordMilestone(t, pool, svc, provider, jobID, queued)
	if err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if outcome != OutcomeAlreadyRecorded {
		t.Errorf("the retry = %q, want already recorded", outcome)
	}
	if second.ID != first.ID {
		t.Errorf("the retry answered with row %s, want the first attempt's %s", second.ID, first.ID)
	}
	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows after a retry, want 1", n)
	}
}

// TestARetainedMilestoneEmitsWithJobMovedFalse keeps the domain event honest about what happened.
//
// SHIP-136 put `job_moved` on the payload precisely because a milestone may write no
// `job_status_history` row and emit no `job.status_changed`. An overruled milestone is the third
// way to get there, and a consumer that read this one as a status change would report a delivery on
// a job an administrator had cancelled.
func TestARetainedMilestoneEmitsWithJobMovedFalse(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "conflictevent-c@example.com", "+61400000741", "customer")
	provider := newAccount(t, pool, "conflictevent-p@example.com", "+61400000742", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider), jobs.StatusEnRouteToPickup)
	adminMove(t, pool, jobID, "Goods withdrawn by the customer.",
		jobs.StatusDisputed, jobs.StatusCancelled)

	if _, outcome, err := recordMilestone(t, pool, newTestService(), provider, jobID,
		Recording{Milestone: MilestonePickedUp, Key: theKey}); err != nil || outcome != OutcomeOverruled {
		t.Fatalf("recording into a cancelled job = %v (outcome %q), want it retained", err, outcome)
	}

	var payload string
	if err := pool.QueryRow(t.Context(),
		`SELECT payload::text FROM outbox
		  WHERE aggregate_id = $1 AND event_type = 'delivery.milestone_recorded'`, jobID).
		Scan(&payload); err != nil {
		t.Fatalf("reading the emitted event: %v — a retained milestone that emits nothing is "+
			"invisible to every consumer", err)
	}
	// Matched loosely, because jsonb reformats what it stores: the key order and the spacing in
	// the round-tripped text are PostgreSQL's business rather than the payload's.
	if !strings.Contains(strings.ReplaceAll(payload, " ", ""), `"job_moved":false`) {
		t.Errorf("the event payload is %s, want job_moved false — the job did not move, and a "+
			"consumer reading otherwise would report a delivery on a cancelled job", payload)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusPickedUp); n != 0 {
		t.Errorf("%d transitions into Picked up, want 0", n)
	}
}
