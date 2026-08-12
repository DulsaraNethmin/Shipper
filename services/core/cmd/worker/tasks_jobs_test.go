package main

import (
	"context"
	"io"
	"log/slog"
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

// SHIP-68 — "Open jobs close at the earlier of 14 days or the pickup date passing", demonstrated
// as a pass of the real task against a real database.
//
// The deadline itself is 000406's and is tested in internal/jobs. What is tested here is the half
// that only exists as a running process: that the registered task claims what is due, moves each
// job through the guard, and that two of them at once divide the work rather than duplicating it.
//
// Every test drives the task through the scheduler's RunOnce rather than through its ticker.
// Docs/10 §6.3's injected clock is what makes that possible without waiting fourteen days: the
// claim judges against clock.Now(), so advancing a clock.Fixed is the whole of "time passed".

// expiryTask builds the registered task exactly as main would, over the given clock.
//
// Through tasks() rather than by calling expireJobs directly, deliberately. The registration in
// init is the thing a merge can drop, and a test that reached past it would keep passing after the
// task had stopped being registered — which is the failure manifest.go describes: no error, no test
// failure, and a task that silently never runs.
func expiryTask(t *testing.T, pool *pgxpool.Pool, at clock.Clock) Task {
	t.Helper()

	built, err := tasks(Deps{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:  at,
		Pool:   pool,
	})
	if err != nil {
		t.Fatalf("building the tasks: %v", err)
	}

	for _, task := range built {
		if task.Name == "job-expiry" {
			return task
		}
	}
	t.Fatal("no task is called job-expiry; the jobs domain's registration is not in the manifest")
	return Task{}
}

// runPass runs one pass and reports how many jobs it claimed.
func runPass(t *testing.T, pool *pgxpool.Pool, task Task) int {
	t.Helper()

	scheduler, err := NewScheduler(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), []Task{task})
	if err != nil {
		t.Fatalf("building the scheduler: %v", err)
	}

	claimed, err := scheduler.RunOnce(t.Context(), task)
	if err != nil {
		t.Fatalf("the pass failed: %v", err)
	}
	return claimed
}

// TestTheJobExpiryTaskIsDeclaredSensibly is the declaration rather than the work.
func TestTheJobExpiryTaskIsDeclaredSensibly(t *testing.T) {
	pool := pgtest.DB(t)
	task := expiryTask(t, pool, clock.System{})

	switch {
	case task.Every <= 0:
		t.Error("the task has no interval, so it would never run twice")
	case task.timeout() >= task.Every:
		// A pass that may outlive its own interval is a task whose passes overlap in one
		// process, which the scheduler's one-goroutine-per-task design does not expect.
		t.Errorf("the timeout %s is not shorter than the interval %s", task.timeout(), task.Every)
	case task.Close != nil:
		// Job expiry owns nothing: it runs queries inside the caller's transaction. A Close
		// here would mean a resource somebody added without saying why (SHIP-15g).
		t.Error("the task declares a Close, but it owns no long-lived resource")
	}
}

// TestAPassExpiresTheJobsWhosePickupDateHasPassed is the operative half of Docs/02 §6.3, end to
// end: a real deadline set by 000406 from a real pickup window, and the real task acting on it.
//
// The three jobs it must leave alone are each a distinct defect if taken: a draft was never
// published, an Open job with a future deadline is a live listing, and an Awarded job is somebody's
// delivery in progress.
func TestAPassExpiresTheJobsWhosePickupDateHasPassed(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-expiry@example.com", "+61400000690")

	// Its pickup window closed an hour ago, so 000406 set the deadline to that instant and
	// the job is due now. Nothing was written into expires_at by hand.
	past := time.Now().UTC().Add(-time.Hour)
	stale := publishedJob(t, pool, service, customer, &jobs.TimeWindow{End: past})

	live := publishedJob(t, pool, service, customer, &jobs.TimeWindow{
		End: time.Now().UTC().Add(48 * time.Hour),
	})
	draft := draftJob(t, pool, service, customer)

	if claimed := runPass(t, pool, expiryTask(t, pool, clock.System{})); claimed != 1 {
		t.Errorf("the pass claimed %d jobs, want the one whose pickup date has passed", claimed)
	}

	if got := statusOf(t, pool, stale); got != jobs.StatusCancelled {
		t.Errorf("the stale job is %s, want Cancelled — Docs/02 §2 calls it 'job expires "+
			"unclaimed'", got)
	}
	if got := statusOf(t, pool, live); got != jobs.StatusOpen {
		t.Errorf("a job whose pickup window is still open became %s", got)
	}
	if got := statusOf(t, pool, draft); got != jobs.StatusDraft {
		t.Errorf("a draft became %s; the clock starts at publication", got)
	}

	// Through the guard, as the platform, with the record 000402 requires. A job that reached
	// Cancelled with no history row would mean the sweep had gone round the guard.
	var from, actor, reason string
	if err := pool.QueryRow(t.Context(),
		`SELECT from_status, actor_type, COALESCE(reason, '')
		   FROM job_status_history WHERE job_id = $1 AND to_status = 'Cancelled'`,
		stale).Scan(&from, &actor, &reason); err != nil {
		t.Fatalf("reading the recorded transition: %v", err)
	}
	if from != "Open" || actor != "system" || reason != jobs.ExpiryReason {
		t.Errorf("recorded %s -> Cancelled by %s (%q), want Open -> Cancelled by system (%q)",
			from, actor, reason, jobs.ExpiryReason)
	}

	// And the event, in the same transaction as the change (Docs/10 §6.1). A consumer that
	// never hears about an expiry cannot tell the provider the job has gone.
	var events int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM outbox WHERE aggregate_id = $1 AND event_type = $2`,
		stale, jobs.EventStatusChanged).Scan(&events); err != nil {
		t.Fatalf("counting the events: %v", err)
	}
	if events != 2 {
		t.Errorf("the expired job has %d status events, want two: its publication and its "+
			"expiry", events)
	}
}

// TestTheFourteenDayBackstopExpiresAJobWithNoPickupDate is the other half of "the earlier of".
//
// A job with no pickup window has no date to pass, so only the backstop can end it — and this is
// where the injected clock earns its place: the test moves fifteen days forward rather than
// waiting, and the claim judges against the instant the worker was handed.
func TestTheFourteenDayBackstopExpiresAJobWithNoPickupDate(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-backstop@example.com", "+61400000691")
	job := publishedJob(t, pool, service, customer, nil)

	// Thirteen days is not yet fourteen. Without this the test would pass against an
	// implementation that expired every Open job it found.
	early := clock.NewFixed(time.Now().UTC().Add(13 * 24 * time.Hour))
	if claimed := runPass(t, pool, expiryTask(t, pool, early)); claimed != 0 {
		t.Fatalf("a pass thirteen days in claimed %d jobs, want none", claimed)
	}
	if got := statusOf(t, pool, job); got != jobs.StatusOpen {
		t.Fatalf("the job is %s after thirteen days, want Open", got)
	}

	late := clock.NewFixed(time.Now().UTC().Add(15 * 24 * time.Hour))
	if claimed := runPass(t, pool, expiryTask(t, pool, late)); claimed != 1 {
		t.Errorf("a pass fifteen days in claimed no job, want the one past its backstop")
	}
	if got := statusOf(t, pool, job); got != jobs.StatusCancelled {
		t.Errorf("the job is %s after fifteen days, want Cancelled", got)
	}
}

// TestTwoWorkersExpireEachJobExactlyOnce is the property a rolling deployment depends on.
//
// Two workers at once is the normal state during a deployment, and nothing coordinates them: the
// claim's FOR UPDATE SKIP LOCKED hands the second whatever the first did not take. The failure
// this rules out is quiet in both directions — duplicated work produces two expiry events for one
// job, and a missing SKIP LOCKED produces two workers doing the work of one, slowly.
//
// Counted in job_status_history rather than in the two return values, because the record is what a
// customer and support would see. Two rows for one job is the defect; the split between the
// workers is not this test's business.
func TestTwoWorkersExpireEachJobExactlyOnce(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-concurrent@example.com", "+61400000692")

	const due = 6
	past := time.Now().UTC().Add(-time.Hour)
	for range due {
		publishedJob(t, pool, service, customer, &jobs.TimeWindow{End: past})
	}

	var wg sync.WaitGroup
	claimed := make([]int, 2)
	for worker := range claimed {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			claimed[n] = runPass(t, pool, expiryTask(t, pool, clock.System{}))
		}(worker)
	}
	wg.Wait()

	if total := claimed[0] + claimed[1]; total != due {
		t.Errorf("two workers claimed %d jobs between them (%v), want %d", total, claimed, due)
	}

	var expiries, expired int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*), count(DISTINCT job_id) FROM job_status_history
		  WHERE to_status = 'Cancelled' AND actor_type = 'system'`).Scan(&expiries, &expired); err != nil {
		t.Fatalf("counting the expiries: %v", err)
	}
	if expiries != due || expired != due {
		t.Errorf("%d expiries across %d jobs, want %d of each — a job expired twice is two "+
			"notifications for one event", expiries, expired, due)
	}
}

// TestAFailedPassLeavesEveryClaimedJobOpen is what makes a crashed worker harmless.
//
// The pass is one transaction, so a failure part way through releases every claimed row
// unexpired rather than leaving some jobs moved and others not. Provoked by cancelling the context
// mid-pass, which is what a SIGKILL looks like from the database's side.
func TestAFailedPassLeavesEveryClaimedJobOpen(t *testing.T) {
	pool := pgtest.DB(t)
	service := jobs.NewService(events.NewOutbox(), clock.System{}, nil)

	customer := newJobCustomer(t, pool, "worker-rollback@example.com", "+61400000693")
	past := time.Now().UTC().Add(-time.Hour)
	job := publishedJob(t, pool, service, customer, &jobs.TimeWindow{End: past})

	scheduler, err := NewScheduler(pool, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err != nil {
		t.Fatalf("building the scheduler: %v", err)
	}

	// The task's own work, wrapped so that the pass fails after the claim and the transitions
	// have happened but before the transaction commits.
	task := expiryTask(t, pool, clock.System{})
	failing := task
	failing.Run = func(ctx context.Context, r db.Runner) (int, error) {
		if _, err := task.Run(ctx, r); err != nil {
			return 0, err
		}
		return 0, context.Canceled // spelling:ok — standard library sentinel, not our word
	}

	if _, err := scheduler.RunOnce(t.Context(), failing); err == nil {
		t.Fatal("the pass reported success")
	}
	if got := statusOf(t, pool, job); got != jobs.StatusOpen {
		t.Errorf("the job is %s after a failed pass, want Open — a claim that is not committed "+
			"is a claim that never happened", got)
	}
}

// --- fixtures ----------------------------------------------------------------------------------

func newJobCustomer(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO users (id, email, phone, password_hash, role) VALUES ($1, $2, $3, 'x', 'customer')`,
		id, email, phone); err != nil {
		t.Fatalf("inserting %s: %v", email, err)
	}
	return id
}

func draftJob(t *testing.T, pool *pgxpool.Pool, service *jobs.Service, customer uuid.UUID) uuid.UUID {
	t.Helper()

	created, err := service.CreateDraft(t.Context(), pool, customer, jobs.DraftFields{})
	if err != nil {
		t.Fatalf("creating a draft: %v", err)
	}
	return created.ID
}

// publishedJob creates a draft with the given pickup window and moves it to Open through the
// guard, which is the only way a job reaches Open — and the only way it gets a deadline.
func publishedJob(t *testing.T, pool *pgxpool.Pool, service *jobs.Service,
	customer uuid.UUID, window *jobs.TimeWindow) uuid.UUID {
	t.Helper()

	created, err := service.CreateDraft(t.Context(), pool, customer,
		jobs.DraftFields{PickupWindow: window})
	if err != nil {
		t.Fatalf("creating a draft: %v", err)
	}

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Transition(ctx, r, jobs.Move{
			JobID: created.ID,
			To:    jobs.StatusOpen,
			Actor: jobs.User(jobs.ActorCustomer, customer),
		})
		return err
	}); err != nil {
		t.Fatalf("publishing %s: %v", created.ID, err)
	}
	return created.ID
}

func statusOf(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) jobs.Status {
	t.Helper()

	var status jobs.Status
	if err := pool.QueryRow(t.Context(),
		`SELECT status FROM jobs WHERE id = $1`, job).Scan(&status); err != nil {
		t.Fatalf("reading the status of %s: %v", job, err)
	}
	return status
}
