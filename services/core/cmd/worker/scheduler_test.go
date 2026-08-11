package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-67a is demonstrated here rather than in scripts/verify-foundation.sh, because it adds no
// HTTP endpoint — and because what has to be shown is two workers at once, which is a thing a
// test can arrange and a curl cannot.
//
// The table below is created inside the test rather than by a migration. The scheduler owns no
// schema: due work is rows in domain tables — jobs whose pickup date has passed (SHIP-68), bids
// that have expired (SHIP-89) — and each task claims its own. A scheduled_tasks table would be a
// second record of what is due, kept in step with the first by hand.

// dueWork creates a table of claimable work and fills it.
func dueWork(t *testing.T, pool *pgxpool.Pool, rows int) {
	t.Helper()

	if _, err := pool.Exec(t.Context(), `
		CREATE TABLE worker_due_work (
			id      uuid        PRIMARY KEY,
			due_at  timestamptz NOT NULL,
			done_by text,
			done_at timestamptz
		)`); err != nil {
		t.Fatalf("creating the work table: %v", err)
	}

	for i := range rows {
		id, err := uuid.NewV7()
		if err != nil {
			t.Fatalf("generating an id: %v", err)
		}
		// Distinct due times, so "the oldest first" is a total order and the two workers
		// in the concurrency test are competing for a well-defined next row.
		if _, err := pool.Exec(t.Context(),
			`INSERT INTO worker_due_work (id, due_at) VALUES ($1, now() - make_interval(secs => $2))`,
			id, float64(rows-i)); err != nil {
			t.Fatalf("inserting work: %v", err)
		}
	}
}

const claimDueWork = `
	SELECT id
	FROM worker_due_work
	WHERE done_by IS NULL AND due_at <= now()
	ORDER BY due_at
	FOR UPDATE SKIP LOCKED
	LIMIT $1`

// drain is the task under test: claim a batch, mark it, commit.
//
// It is the shape every real task will have — SHIP-68 claims Open jobs whose pickup date has
// passed and moves them, SHIP-119 claims Delivered jobs older than seventy-two hours — with the
// domain work replaced by the cheapest possible stand-in.
func drain(name, worker string, batch int, claimed *atomic.Int64) Task {
	return Task{
		Name:  name,
		Every: time.Hour,
		Run: func(ctx context.Context, r db.Runner) (int, error) {
			ids, err := ClaimIDs(ctx, r, claimDueWork, batch)
			if err != nil {
				return 0, err
			}
			if len(ids) == 0 {
				return 0, nil
			}
			if _, err := r.Exec(ctx,
				`UPDATE worker_due_work SET done_by = $2, done_at = now() WHERE id = ANY($1)`,
				ids, worker); err != nil {
				return 0, err
			}
			if claimed != nil {
				claimed.Add(int64(len(ids)))
			}
			return len(ids), nil
		},
	}
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelError}))
}

func newTestScheduler(t *testing.T, pool *pgxpool.Pool, tasks ...Task) *Scheduler {
	t.Helper()

	s, err := NewScheduler(pool, quietLogger(), tasks)
	if err != nil {
		t.Fatalf("building the scheduler: %v", err)
	}
	return s
}

// secondPool is another worker: its own connections, sharing nothing with the first.
//
// Two goroutines over one pgxpool would already use two connections, which is enough for
// PostgreSQL to behave exactly as it will in production. This is a second pool anyway, because
// "two workers" is the claim being made and a second pool is what that means.
func secondPool(t *testing.T, first *pgxpool.Pool) *pgxpool.Pool {
	t.Helper()

	other, err := pgxpool.New(t.Context(), first.Config().ConnString())
	if err != nil {
		t.Fatalf("opening a second worker's pool: %v", err)
	}
	t.Cleanup(other.Close)
	return other
}

func countDone(t *testing.T, pool *pgxpool.Pool) (done, outstanding int) {
	t.Helper()

	if err := pool.QueryRow(t.Context(), `
		SELECT count(*) FILTER (WHERE done_by IS NOT NULL),
		       count(*) FILTER (WHERE done_by IS NULL)
		FROM worker_due_work`).Scan(&done, &outstanding); err != nil {
		t.Fatalf("counting the work: %v", err)
	}
	return done, outstanding
}

// TestAPassClaimsWhatIsDue is the ordinary case: one worker, one pass, a batch smaller than the
// backlog.
func TestAPassClaimsWhatIsDue(t *testing.T) {
	pool := pgtest.DB(t)
	dueWork(t, pool, 10)

	scheduler := newTestScheduler(t, pool)

	claimed, err := scheduler.RunOnce(t.Context(), drain("drain", "one", 4, nil))
	if err != nil {
		t.Fatalf("the pass failed: %v", err)
	}
	if claimed != 4 {
		t.Errorf("the pass claimed %d rows, want the batch of 4", claimed)
	}

	done, outstanding := countDone(t, pool)
	if done != 4 || outstanding != 6 {
		t.Errorf("%d done and %d outstanding, want 4 and 6", done, outstanding)
	}
}

// TestALockedRowIsSkippedRatherThanWaitedFor is SKIP LOCKED, shown deterministically on two real
// connections rather than argued from the SQL.
//
// The first transaction claims two rows and keeps them. The second must be handed the *next*
// two — not the same two, and not nothing while it waits for the first to commit. Without SKIP
// LOCKED the second Exec below would block until the deadline rather than return.
func TestALockedRowIsSkippedRatherThanWaitedFor(t *testing.T) {
	pool := pgtest.DB(t)
	dueWork(t, pool, 5)

	first, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning the first transaction: %v", err)
	}
	defer func() { _ = first.Rollback(context.WithoutCancel(t.Context())) }()

	held, err := ClaimIDs(t.Context(), first, claimDueWork, 2)
	if err != nil {
		t.Fatalf("the first claim: %v", err)
	}
	if len(held) != 2 {
		t.Fatalf("the first worker claimed %d rows, want 2", len(held))
	}

	// A separate connection, and a deadline: if the claim waits behind the first
	// transaction's locks rather than skipping them, this is what says so.
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	second, err := secondPool(t, pool).Begin(ctx)
	if err != nil {
		t.Fatalf("beginning the second transaction: %v", err)
	}
	defer func() { _ = second.Rollback(context.WithoutCancel(t.Context())) }()

	next, err := ClaimIDs(ctx, second, claimDueWork, 2)
	if err != nil {
		t.Fatalf("the second claim waited for the first rather than skipping it: %v", err)
	}
	if len(next) != 2 {
		t.Fatalf("the second worker claimed %d rows, want 2", len(next))
	}

	taken := map[uuid.UUID]bool{}
	for _, id := range held {
		taken[id] = true
	}
	for _, id := range next {
		if taken[id] {
			t.Errorf("both workers claimed %s; the rows were read but not locked", id)
		}
	}
}

// TestRunningTwiceDoesEveryRowExactlyOnce is SHIP-67a's acceptance criterion.
//
// Two schedulers, each with its own pool, drain one table at the same time until nothing is
// left. Two things are checked and the second is the one that matters: every row was done, and
// the passes claimed exactly as many rows in total as there were rows. A row claimed twice
// inflates that total even if the second claim overwrote the first invisibly.
func TestRunningTwiceDoesEveryRowExactlyOnce(t *testing.T) {
	const rows = 200

	pool := pgtest.DB(t)
	dueWork(t, pool, rows)

	var claimed atomic.Int64

	workers := []*Scheduler{
		newTestScheduler(t, pool),
		newTestScheduler(t, secondPool(t, pool)),
	}

	var wg sync.WaitGroup
	for i, scheduler := range workers {
		wg.Add(1)
		go func(i int, s *Scheduler) {
			defer wg.Done()

			task := drain("drain", string(rune('a'+i)), 7, &claimed)
			for {
				n, err := s.RunOnce(context.Background(), task)
				if err != nil {
					t.Errorf("worker %d: %v", i, err)
					return
				}
				if n == 0 {
					return
				}
			}
		}(i, scheduler)
	}
	wg.Wait()

	done, outstanding := countDone(t, pool)
	if done != rows || outstanding != 0 {
		t.Errorf("%d rows done and %d outstanding, want %d and 0", done, outstanding, rows)
	}
	if got := claimed.Load(); got != rows {
		t.Errorf("the two workers claimed %d rows between them and there were %d; "+
			"a row claimed twice is work done twice", got, rows)
	}

	// Both workers should have taken a share. This is not a correctness property — one
	// worker finishing the backlog before the other's first claim lands is a legitimate
	// outcome — so it is reported rather than failed.
	var shares int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(DISTINCT done_by) FROM worker_due_work`).Scan(&shares); err != nil {
		t.Fatalf("counting the workers that did something: %v", err)
	}
	if shares < 2 {
		t.Logf("only %d worker claimed anything; the run was not concurrent enough to "+
			"demonstrate sharing, though exactly-once still held", shares)
	}
}

// TestAFailedPassClaimsNothing is why the claim and the work share one transaction.
//
// A pass that failed halfway would otherwise leave rows marked as claimed and not acted on, and
// nothing would ever come back for them: the claim query only looks at rows nobody has taken.
func TestAFailedPassClaimsNothing(t *testing.T) {
	pool := pgtest.DB(t)
	dueWork(t, pool, 5)

	scheduler := newTestScheduler(t, pool)
	halfway := errors.New("the domain refused the work")

	claimed, err := scheduler.RunOnce(t.Context(), Task{
		Name:  "fails-halfway",
		Every: time.Hour,
		Run: func(ctx context.Context, r db.Runner) (int, error) {
			ids, err := ClaimIDs(ctx, r, claimDueWork, 3)
			if err != nil {
				return 0, err
			}
			if _, err := r.Exec(ctx,
				`UPDATE worker_due_work SET done_by = 'one' WHERE id = ANY($1)`, ids); err != nil {
				return 0, err
			}
			return len(ids), halfway
		},
	})
	if !errors.Is(err, halfway) {
		t.Fatalf("RunOnce() = %v, want the task's own error", err)
	}
	if claimed != 0 {
		t.Errorf("a failed pass reported %d rows claimed; the transaction rolled back", claimed)
	}

	done, outstanding := countDone(t, pool)
	if done != 0 || outstanding != 5 {
		t.Errorf("%d done and %d outstanding after a failed pass, want 0 and 5", done, outstanding)
	}
}

// TestAPanickingTaskBecomesAnError keeps one bad task from taking the process with it.
//
// Four tasks share this process, and three of them working is strictly better than none.
func TestAPanickingTaskBecomesAnError(t *testing.T) {
	pool := pgtest.DB(t)
	dueWork(t, pool, 3)

	scheduler := newTestScheduler(t, pool)

	claimed, err := scheduler.RunOnce(t.Context(), Task{
		Name:  "panics",
		Every: time.Hour,
		Run: func(ctx context.Context, r db.Runner) (int, error) {
			if _, err := ClaimIDs(ctx, r, claimDueWork, 1); err != nil {
				return 0, err
			}
			panic("a nil pointer, somewhere in a domain")
		},
	})
	if err == nil {
		t.Fatal("a panicking task returned no error")
	}
	if !strings.Contains(err.Error(), "panicked") {
		t.Errorf("the error does not say what happened: %v", err)
	}
	if claimed != 0 {
		t.Errorf("a panicking pass reported %d rows claimed", claimed)
	}

	if done, outstanding := countDone(t, pool); done != 0 || outstanding != 3 {
		t.Errorf("%d done and %d outstanding after a panic, want 0 and 3", done, outstanding)
	}
}

// TestAPassIsBoundedByItsTimeout stops a wedged pass holding its claim for the life of the
// process. The rows are not lost — they are released the moment the transaction ends — but until
// then no other worker can see them.
func TestAPassIsBoundedByItsTimeout(t *testing.T) {
	pool := pgtest.DB(t)
	dueWork(t, pool, 2)

	scheduler := newTestScheduler(t, pool)

	started := time.Now()
	_, err := scheduler.RunOnce(t.Context(), Task{
		Name:    "wedged",
		Every:   time.Hour,
		Timeout: 250 * time.Millisecond,
		Run: func(ctx context.Context, r db.Runner) (int, error) {
			if _, err := ClaimIDs(ctx, r, claimDueWork, 1); err != nil {
				return 0, err
			}
			_, err := r.Exec(ctx, `SELECT pg_sleep(10)`)
			return 0, err
		},
	})
	if err == nil {
		t.Fatal("a pass that ran ten seconds past its timeout was allowed to finish")
	}
	if elapsed := time.Since(started); elapsed > 5*time.Second {
		t.Errorf("the pass took %s to be cut off, and its timeout was 250ms", elapsed)
	}

	if done, outstanding := countDone(t, pool); done != 0 || outstanding != 2 {
		t.Errorf("%d done and %d outstanding after a timed-out pass, want 0 and 2", done, outstanding)
	}
}

// TestRunKeepsTickingUntilItIsStopped covers the loop rather than the pass: the first pass runs
// immediately, passes repeat, and a cancelled context ends it.
func TestRunKeepsTickingUntilItIsStopped(t *testing.T) {
	pool := pgtest.DB(t)
	dueWork(t, pool, 20)

	var claimed atomic.Int64
	task := drain("drain", "ticker", 1, &claimed)
	task.Every = 5 * time.Millisecond

	scheduler := newTestScheduler(t, pool, task)

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- scheduler.Run(ctx) }()

	deadline := time.After(10 * time.Second)
	for claimed.Load() < 3 {
		select {
		case <-deadline:
			t.Fatalf("only %d passes claimed anything in ten seconds", claimed.Load())
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("Run() = %v, want nil after a clean stop", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

// TestRunWithNoTasksWaitsRatherThanExiting keeps the worker supervisable before SHIP-68 gives it
// anything to do. A process that exits immediately looks like a crash to whatever restarts it.
func TestRunWithNoTasksWaitsRatherThanExiting(t *testing.T) {
	pool := pgtest.DB(t)
	scheduler := newTestScheduler(t, pool)

	ctx, cancel := context.WithCancel(t.Context())
	stopped := make(chan error, 1)
	go func() { stopped <- scheduler.Run(ctx) }()

	select {
	case <-stopped:
		t.Fatal("the worker exited immediately because nothing is registered yet")
	case <-time.After(100 * time.Millisecond):
	}

	cancel()
	select {
	case err := <-stopped:
		if err != nil {
			t.Errorf("Run() = %v, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after its context was cancelled")
	}
}

func TestNewSchedulerNeedsWhatItCannotWorkWithout(t *testing.T) {
	if _, err := NewScheduler(nil, quietLogger(), nil); err == nil {
		t.Error("a scheduler was built with no database")
	}
	if _, err := NewScheduler(&pgxpool.Pool{}, nil, nil); err == nil {
		t.Error("a scheduler was built with no logger")
	}
}
