package main

import (
	"context"
	"strings"
	"sync"
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

// SHIP-119 — "a Delivered job with no dispute becomes Completed after 72 hours", demonstrated as a
// pass of the real registered task against a real database.
//
// Docs/02 §6.1 is the rule and internal/jobs/autocomplete.go is the reading of it. What is tested
// here is the half that only exists as a running process: that the registration is in the manifest,
// that the claim takes what is due and nothing else, that two workers divide the work rather than
// duplicating it, and that a job which left Delivered is never taken.
//
// # Every deadline in this file is measured between two instants from the same clock
//
// This is the trap Docs/11 §9 names as the two-clock defect class, and a seventy-two hour window is
// exactly where it bites. The row timestamp — job_status_history.server_recorded_at — comes from
// the *database* clock, defaulted by 000401 and unsettable by any caller. The judgement instant
// comes from the injected *Go* clock, because Docs/10 §6.3 puts every scheduled task behind one.
//
// A test that pinned clock.Fixed to a literal date would be comparing the two: it would pass on the
// day it was written and fail permanently once now() had moved three days past the literal. So
// every clock below is built by [deliveredAt] reading the row back and adding to it. The window is
// then measured from the database instant to a Go instant derived from it, and there is no calendar
// date anywhere in the file for time to overtake.

// deliverySteps is Docs/02 §2's route from a Draft to a Delivered job, in order.
//
// The whole chain rather than a jump straight to Delivered, because the guard in Service.Transition
// asks Permitted for each move and a fixture that could not happen in the product is a fixture that
// proves nothing. Docs/02 §2 permits Awarded -> En route to pickup directly; the longer path is
// taken here because it is the one a job with a nominated driver actually walks.
var deliverySteps = []jobs.Status{
	jobs.StatusOpen,
	jobs.StatusAwarded,
	jobs.StatusDriverAssigned,
	jobs.StatusEnRouteToPickup,
	jobs.StatusPickedUp,
	jobs.StatusInTransit,
	jobs.StatusDelivered,
}

// deliveredJob walks a new job all the way to Delivered through the guard.
//
// The actor is a provider with an identifier no users row backs, which is legitimate: 000401 has no
// foreign key on actor_id on purpose — "this record must outlive its subject" — and the delivery
// half of the chain is the provider's work in Docs/02 §1. Nothing in this ticket reads the actor
// back except to check that the auto-completion is attributed to the platform rather than to them.
func deliveredJob(t *testing.T, pool *pgxpool.Pool, service *jobs.Service, customer uuid.UUID) uuid.UUID {
	t.Helper()

	job := draftJob(t, pool, service, customer)
	provider := uuid.Must(uuid.NewV7())

	for _, to := range deliverySteps {
		actor := jobs.User(jobs.ActorCustomer, customer)
		if to != jobs.StatusOpen {
			actor = jobs.User(jobs.ActorProvider, provider)
		}
		if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			_, err := service.Transition(ctx, r, jobs.Move{JobID: job, To: to, Actor: actor})
			return err
		}); err != nil {
			t.Fatalf("moving %s to %s: %v", job, to, err)
		}
	}
	return job
}

// deliveredAt is when the platform recorded the job becoming Delivered.
//
// Read back rather than remembered, and this is the anchor the whole file hangs on: it is the
// database's own clock, which is the clock the claim's predicate is expressed in.
func deliveredAt(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) time.Time {
	t.Helper()

	var at time.Time
	if err := pool.QueryRow(t.Context(),
		`SELECT server_recorded_at FROM job_status_history
		  WHERE job_id = $1 AND to_status = 'Delivered'
		  ORDER BY server_recorded_at DESC LIMIT 1`, job).Scan(&at); err != nil {
		t.Fatalf("reading when %s was delivered: %v", job, err)
	}
	return at
}

// autoCompleteTask builds the registered task over a clock that makes `at` the judgement instant.
//
// The task subtracts the window from clock.Now(), so a clock standing at at+72h judges against
// exactly at. Passing the instant to be judged against rather than the fake wall time is what keeps
// every caller below readable: a test says "judge as though the deadline had just passed" instead
// of doing the arithmetic in its own head.
func autoCompleteTask(t *testing.T, pool *pgxpool.Pool, at time.Time) Task {
	t.Helper()
	return registeredTask(t, pool, clock.NewFixed(at.Add(jobs.AutoCompleteWindow)), "job-auto-complete")
}

// TestTheAutoCompleteTaskIsRegisteredAndDeclaredSensibly is the declaration rather than the work.
//
// Through tasks() rather than by calling autoCompleteDeliveries directly, for the reason
// registeredTask gives: the registration in init is the thing a merge can drop, and a task that
// stops being registered produces no compile error and no other failure.
func TestTheAutoCompleteTaskIsRegisteredAndDeclaredSensibly(t *testing.T) {
	pool := pgtest.DB(t)

	task := autoCompleteTask(t, pool, time.Now().UTC())
	switch {
	case task.Every <= 0:
		t.Errorf("%s has no interval, so it would never run twice", task.Name)
	case task.timeout() >= task.Every:
		t.Errorf("%s: the timeout %s is not shorter than the interval %s",
			task.Name, task.timeout(), task.Every)
	case task.Close != nil:
		t.Errorf("%s declares a Close, but it owns no long-lived resource", task.Name)
	}
}

// TestAPassCompletesADeliveryOnceItsWindowHasPassed is SHIP-119's acceptance criterion.
//
// Three jobs, and each of the two that must be left alone is a distinct defect if taken. A delivery
// one second inside its window has not had the seventy-two hours Docs/02 §6.1 promises the
// customer. A job still In transit has not been delivered at all.
func TestAPassCompletesADeliveryOnceItsWindowHasPassed(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-autocomplete@example.com", "+61400001190")

	due := deliveredJob(t, pool, service, customer)
	fresh := deliveredJob(t, pool, service, customer)
	inTransit := draftJob(t, pool, service, customer)
	for _, to := range deliverySteps[:len(deliverySteps)-1] {
		moveJob(t, pool, service, inTransit, to)
	}

	// The judgement instant is the older delivery's own recorded instant, so the due job is
	// exactly at its deadline — the boundary rather than a comfortable margin — and the fresh
	// one, delivered microseconds later, is inside its window.
	at := deliveredAt(t, pool, due)
	if fresh := deliveredAt(t, pool, fresh); !fresh.After(at) {
		t.Fatalf("the two deliveries were recorded at the same instant (%s); the fixture "+
			"cannot distinguish due from not due", at)
	}

	if claimed := runPass(t, pool, autoCompleteTask(t, pool, at)); claimed != 1 {
		t.Errorf("the pass claimed %d jobs, want the one whose window has passed", claimed)
	}

	if got := statusOf(t, pool, due); got != jobs.StatusCompleted {
		t.Errorf("the delivery past its window is %s, want Completed (Docs/02 §6.1)", got)
	}
	if got := statusOf(t, pool, fresh); got != jobs.StatusDelivered {
		t.Errorf("a delivery inside its window became %s; the customer had not had their "+
			"seventy-two hours", got)
	}
	if got := statusOf(t, pool, inTransit); got != jobs.StatusInTransit {
		t.Errorf("a job still in transit became %s", got)
	}
}

// TestAnAutoCompletionIsAttributedToThePlatformWithItsReason is what support and the customer's
// status timeline read.
//
// 000401's ck_job_status_history_actor_id already refuses a system actor that names an account, and
// its own comment names this ticket as one of the two transitions with no user behind them. What
// this adds is that the recorded transition says *why*, which is the difference between a job that
// closed itself and a job somebody closed.
func TestAnAutoCompletionIsAttributedToThePlatformWithItsReason(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-autocomplete-actor@example.com", "+61400001191")
	job := deliveredJob(t, pool, service, customer)

	runPass(t, pool, autoCompleteTask(t, pool, deliveredAt(t, pool, job)))

	var actorType, reason string
	var actorID *uuid.UUID
	if err := pool.QueryRow(t.Context(),
		`SELECT actor_type, actor_id, reason FROM job_status_history
		  WHERE job_id = $1 AND from_status = 'Delivered' AND to_status = 'Completed'`,
		job).Scan(&actorType, &actorID, &reason); err != nil {
		t.Fatalf("reading the recorded auto-completion: %v", err)
	}

	if actorType != string(jobs.ActorSystem) || actorID != nil {
		t.Errorf("the auto-completion is attributed to %s/%v, want the platform acting as "+
			"itself with no account", actorType, actorID)
	}
	if reason != jobs.AutoCompleteReason {
		t.Errorf("the recorded reason is %q, want %q", reason, jobs.AutoCompleteReason)
	}

	// The event, from the outbox this pass wrote into. A transition nothing downstream hears
	// about is the failure the event seam exists to prevent (Docs/10 §6.1), and SHIP-137's
	// consumer is the thing that will be listening.
	var from, payload string
	if err := pool.QueryRow(t.Context(),
		`SELECT payload->>'from', payload::text FROM outbox
		  WHERE aggregate_id = $1 AND event_type = 'job.status_changed'
		    AND payload->>'to' = 'Completed'`, job).Scan(&from, &payload); err != nil {
		t.Fatalf("reading the emitted event: %v", err)
	}
	if from != string(jobs.StatusDelivered) {
		t.Errorf("the event says the job moved from %q, want Delivered", from)
	}
	if strings.Contains(payload, "budget") {
		t.Errorf("the auto-completion event carries a budget (Docs/01 §4.3): %s", payload)
	}
}

// TestADisputeStopsTheClock is the "with no dispute" half of the *Done when*, and it is the half
// that decides where this ticket lives.
//
// Docs/02 §3: "a dispute freezes automatic completion until an administrator resolves it." The
// sweep asks no question about disputes at all, because Disputed is a status of its own and a
// disputed job has already left Delivered — which is why this needed nothing from internal/delivery
// and could be built in the jobs domain.
func TestADisputeStopsTheClock(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-autocomplete-dispute@example.com", "+61400001192")

	disputed := deliveredJob(t, pool, service, customer)
	at := deliveredAt(t, pool, disputed)
	moveJob(t, pool, service, disputed, jobs.StatusDisputed)

	// Judged long after the window closed, so nothing but the dispute can be what saves it.
	if claimed := runPass(t, pool, autoCompleteTask(t, pool, at.Add(30*24*time.Hour))); claimed != 0 {
		t.Errorf("the pass claimed %d jobs; a disputed delivery must not auto-complete", claimed)
	}
	if got := statusOf(t, pool, disputed); got != jobs.StatusDisputed {
		t.Errorf("the disputed job is %s, want Disputed — only an administrator resolves it", got)
	}
}

// TestASecondPassCompletesNothingAgain is the sweep's own idempotence.
//
// It is not an idempotency key or a mark column: a completed job is no longer Delivered, so the
// claim does not select it. That is the same shape SHIP-86 calls idempotence by state, and it is
// stronger than a key because nothing has to remember anything.
func TestASecondPassCompletesNothingAgain(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-autocomplete-twice@example.com", "+61400001193")
	job := deliveredJob(t, pool, service, customer)
	at := deliveredAt(t, pool, job)

	if claimed := runPass(t, pool, autoCompleteTask(t, pool, at)); claimed != 1 {
		t.Fatalf("the first pass claimed %d jobs, want one", claimed)
	}
	if claimed := runPass(t, pool, autoCompleteTask(t, pool, at)); claimed != 0 {
		t.Errorf("the second pass claimed %d jobs; the job is Completed and is not due for "+
			"anything", claimed)
	}

	var completions int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history
		  WHERE job_id = $1 AND to_status = 'Completed'`, job).Scan(&completions); err != nil {
		t.Fatalf("counting the completions: %v", err)
	}
	if completions != 1 {
		t.Errorf("the job has %d recorded completions, want one — a second is a second "+
			"notification for one event", completions)
	}
}

// TestTwoWorkersCompleteEachDeliveryExactlyOnce is what FOR UPDATE SKIP LOCKED buys, run rather
// than asserted.
//
// A rolling deployment runs two workers as a matter of course. Without SKIP LOCKED the second
// queues behind the first; without FOR UPDATE both claim the same rows and both act. Neither
// failure produces an error, which is why cmd/worker.ClaimIDs refuses a claim query missing either
// clause — and why this test exists as well as that check, because the check reads the string and
// this reads the outcome.
func TestTwoWorkersCompleteEachDeliveryExactlyOnce(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-autocomplete-race@example.com", "+61400001194")

	const due = 6
	ids := make([]uuid.UUID, 0, due)
	for range due {
		ids = append(ids, deliveredJob(t, pool, service, customer))
	}
	at := deliveredAt(t, pool, ids[len(ids)-1])

	var wg sync.WaitGroup
	claimed := make([]int, 2)
	for worker := range claimed {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			claimed[n] = runPass(t, pool, autoCompleteTask(t, pool, at))
		}(worker)
	}
	wg.Wait()

	if total := claimed[0] + claimed[1]; total != due {
		t.Errorf("two workers claimed %d deliveries between them (%v), want %d",
			total, claimed, due)
	}

	var completions, completed int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*), count(DISTINCT job_id) FROM job_status_history
		  WHERE to_status = 'Completed' AND actor_type = 'system'`).Scan(&completions, &completed); err != nil {
		t.Fatalf("counting the completions: %v", err)
	}
	if completions != due || completed != due {
		t.Errorf("%d completions across %d jobs, want %d of each — a job completed twice is "+
			"two notifications for one event", completions, completed, due)
	}
}

// moveJob is one transition through the guard, for the fixtures above.
func moveJob(t *testing.T, pool *pgxpool.Pool, service *jobs.Service, job uuid.UUID, to jobs.Status) {
	t.Helper()

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Transition(ctx, r, jobs.Move{
			JobID: job, To: to, Actor: jobs.User(jobs.ActorProvider, uuid.Must(uuid.NewV7())),
		})
		return err
	}); err != nil {
		t.Fatalf("moving %s to %s: %v", job, to, err)
	}
}
