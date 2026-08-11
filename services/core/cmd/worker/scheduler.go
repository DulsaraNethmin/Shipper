package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Scheduler is what Docs/09 calls the scheduled task runner (SHIP-67a).
//
// It is named Scheduler rather than Runner because db.Runner appears throughout this package and
// means something else entirely — the thing a query is sent to.
//
// # What it is, and what it deliberately is not
//
// Docs/10 §6.2 specifies the whole design in one line: "a ticker plus a SELECT … FOR UPDATE SKIP
// LOCKED claim loop. No external scheduler." There is no cron, no queue broker, and no leader
// election, and the absence of the last one is the part worth explaining.
//
// Two workers running at once is the normal state of a rolling deployment, and it is safe here
// by construction rather than by arrangement. Both tick, both open a transaction, and both run
// the same claim query — and SKIP LOCKED means the second one is handed the rows the first did
// not take rather than waiting behind it. The work is divided instead of duplicated, with no
// lease table, no lock service, and nothing to go stale if a worker dies holding it: the
// transaction ends when the connection does, and the rows are free again.
//
// That is also why there is no scheduled_tasks table. The due work is rows in domain tables —
// jobs whose pickup date has passed, bids that have expired, deliveries seventy-two hours old —
// and each task claims its own. A table of tasks would add a second thing to keep in step with
// the first, and the first is the one that is true.
//
// # What a pass is
//
// One transaction. The claim and the work the claim authorises commit together or not at all,
// which is what makes a crashed worker harmless: rows it had claimed are released unprocessed
// and the next pass finds them again. A pass that claimed rows and then committed only half its
// work would be the failure this shape exists to prevent.
type Scheduler struct {
	pool  *pgxpool.Pool
	log   *slog.Logger
	tasks []Task
}

// NewScheduler builds the scheduler over an already-validated task list.
func NewScheduler(pool *pgxpool.Pool, log *slog.Logger, tasks []Task) (*Scheduler, error) {
	if pool == nil {
		return nil, errors.New("worker: the scheduler needs a database; every task is a claim")
	}
	if log == nil {
		return nil, errors.New("worker: the scheduler needs a logger; a pass nobody can see " +
			"failing is a pass nobody knows stopped running")
	}
	return &Scheduler{pool: pool, log: log, tasks: tasks}, nil
}

// Run works until ctx is cancelled, then waits for the pass in flight to finish.
//
// Each task gets one goroutine and one ticker, so two passes of the same task never overlap
// within a process: a slow pass delays its own next tick and nothing else. Across processes they
// do overlap, which is the case SKIP LOCKED handles.
//
// The first pass of each task runs immediately rather than one interval after start-up. A
// deployment would otherwise leave every task idle for its whole interval — up to an hour for
// something that only needs checking hourly — precisely when a restart may have interrupted one.
func (s *Scheduler) Run(ctx context.Context) error {
	if len(s.tasks) == 0 {
		// Not an error. cmd/worker exists before the tasks that will fill it (SHIP-68,
		// SHIP-69, SHIP-89, SHIP-119), and a process that exits immediately would look
		// like a crash to whatever is supervising it.
		s.log.Warn("no scheduled tasks are registered; the worker is idle")
	}

	var wg sync.WaitGroup
	for _, task := range s.tasks {
		wg.Add(1)
		go func(t Task) {
			defer wg.Done()
			s.loop(ctx, t)
		}(task)
	}

	<-ctx.Done()
	wg.Wait()
	return nil
}

func (s *Scheduler) loop(ctx context.Context, t Task) {
	log := s.log.With(slog.String("task", t.Name))
	log.Info("scheduled task started",
		slog.Duration("every", t.Every), slog.Duration("timeout", t.timeout()))

	ticker := time.NewTicker(t.Every)
	defer ticker.Stop()

	for {
		s.pass(ctx, t, log)

		select {
		case <-ctx.Done():
			log.Info("scheduled task stopped")
			return
		case <-ticker.C:
		}
	}
}

// pass runs one pass and reports what happened.
//
// A failure is logged and the loop continues. The alternative — stopping the task — would mean
// one transient database error silently ends job expiry for the lifetime of the process, and the
// symptom of that is jobs quietly staying open, days later, with nothing to connect it to.
func (s *Scheduler) pass(ctx context.Context, t Task, log *slog.Logger) {
	if ctx.Err() != nil {
		return
	}

	started := time.Now()
	claimed, err := s.RunOnce(ctx, t)
	elapsed := time.Since(started).Round(time.Millisecond)

	switch {
	case err != nil && ctx.Err() != nil:
		// Shutting down. The pass was cut off by the signal rather than by a fault, and
		// reporting it as an error would make every clean deployment log one.
		log.Info("scheduled pass abandoned at shutdown", slog.Duration("took", elapsed))
	case err != nil:
		log.Error("scheduled pass failed",
			slog.String("error", err.Error()), slog.Duration("took", elapsed))
	case claimed > 0:
		log.Info("scheduled pass claimed work",
			slog.Int("claimed", claimed), slog.Duration("took", elapsed))
	default:
		log.Debug("scheduled pass found nothing due", slog.Duration("took", elapsed))
	}
}

// RunOnce runs a single pass in a single transaction, and is the whole of what a pass is.
//
// It is exported and takes the task as an argument so that a test can drive one pass without a
// ticker, which is the difference between testing the claim and testing time.
//
// A panic inside a task becomes an error. db.InTx already rolls back and re-panics, so the
// transaction is safe either way; catching it here is about the process, which otherwise dies
// because one task hit a nil pointer and takes the other three with it.
func (s *Scheduler) RunOnce(ctx context.Context, t Task) (claimed int, err error) {
	ctx, cancel := context.WithTimeout(ctx, t.timeout())
	defer cancel()

	defer func() {
		if p := recover(); p != nil {
			claimed, err = 0, fmt.Errorf("worker: task %q panicked: %v", t.Name, p)
		}
	}()

	err = db.InTx(ctx, s.pool, func(ctx context.Context, r db.Runner) error {
		n, runErr := t.Run(ctx, r)
		claimed = n
		return runErr
	})
	if err != nil {
		// The transaction rolled back, so nothing was claimed however many rows the task
		// thought it had. Saying otherwise in the log would be a count of work that did
		// not happen.
		return 0, fmt.Errorf("worker: task %q: %w", t.Name, err)
	}
	return claimed, nil
}
