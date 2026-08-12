package main

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// The task manifest, built the same way as the route manifest in cmd/api and for the same
// reason.
//
// Four scheduled tasks are already in the backlog — job expiry (SHIP-68), the warning
// forty-eight hours ahead of it (SHIP-69), bid expiry (SHIP-89) and the seventy-two hour
// auto-complete (SHIP-119) — and they belong to three different domains and three different
// branches. If each had to add a line to one list in main.go, the merge hazard would be exactly
// the one Docs/10 §4.1 describes for routes: git merges the lines, sometimes wrongly, and a
// dropped registration produces no compile error and no test failure. It produces a task that
// silently never runs, which is worse than a missing endpoint because nobody calls a task and
// notices it is gone.
//
// So a domain contributes cmd/worker/tasks_<domain>.go with an init that calls register, and
// edits nothing shared.

// Deps is what a task is built from.
//
// The same shape as cmd/api's Deps and pre-seeded for the same reason (Docs/10 §9.2): a track
// adding a task should not have to add a field here. A domain service is a pure function of the
// pool, the clock and configuration, so it is built inside the registration closure.
//
// There is no Redis client. Nothing scheduled needs one: idempotency is an HTTP concern, and a
// task claims its work with a row lock rather than a distributed one.
type Deps struct {
	Config *config.Config
	Logger *slog.Logger
	Clock  clock.Clock

	// Pool is never nil here, unlike cmd/api's. The worker refuses to start without a
	// database because every task it could run is a database claim — see main.go.
	Pool *pgxpool.Pool
}

// Work does one pass over whatever is due.
//
// It is handed the transaction the pass runs in, and both halves of the pass belong inside it:
// the claim and the work the claim authorises. That is what makes FOR UPDATE SKIP LOCKED safe —
// the rows stay locked until the transaction ends, so a second worker skips them, and if the
// pass fails the rows are released unclaimed rather than left half-processed.
//
// The count returned is how many rows were claimed, which is what the log line reports and what
// a test asserts. Returning it rather than logging inside the task keeps the task about the work.
type Work func(ctx context.Context, r db.Runner) (claimed int, err error)

// Task is one piece of scheduled work.
type Task struct {
	// Name identifies the task in logs and must be unique.
	Name string

	// Every is how often a pass is attempted. It is not a guarantee: a pass that outlives
	// its interval simply delays the next one, because each task has one goroutine and
	// time.Ticker drops ticks nobody was waiting for.
	Every time.Duration

	// Timeout bounds a single pass. Zero means defaultTaskTimeout.
	//
	// A pass holds a transaction, and a transaction that never ends holds its claimed rows
	// out of every other worker's reach for as long as the process lives. The rows are not
	// lost — the next pass after a restart finds them again — but the work stops happening
	// and nothing says so.
	Timeout time.Duration

	// Run is the pass itself.
	Run Work

	// Close releases whatever the task owns, once, after its loop has stopped. Optional.
	//
	// Added at SHIP-15g for SHIP-134, and the gap it fills is worth stating because the
	// original design deliberately did not have one. Deps carries the pool, the clock and
	// configuration, on the reasoning that "a domain service is a pure function" of those —
	// true of job expiry, bid expiry and the auto-complete, all of which only ever run
	// queries. The outbox publisher is the first task that is not: it holds a Kafka producer,
	// which is a connection with buffered messages behind it.
	//
	// Without this, that task had two options and both were bad. Building the producer inside
	// every pass pays a connection and a metadata fetch each time round a loop that runs every
	// few seconds. Building it once in the registration closure leaks it, and worse, drops
	// whatever it had buffered at shutdown — which for an outbox publisher means events that
	// the database believes were published.
	//
	// A hook on the task rather than a field on Deps, deliberately: Deps would need one field
	// per integration, and every future task would carry a Kafka producer it never uses. This
	// way the resource stays owned by the one task that wants it, and a track adds a file and
	// still edits none.
	Close func(context.Context) error
}

// defaultTaskTimeout bounds a pass that names no timeout of its own.
//
// Chosen to be comfortably longer than any pass should take and much shorter than the shortest
// plausible interval, so that a wedged pass is cut off well before it can be mistaken for a
// working one.
const defaultTaskTimeout = 30 * time.Second

func (t Task) timeout() time.Duration {
	if t.Timeout > 0 {
		return t.Timeout
	}
	return defaultTaskTimeout
}

// Registration builds a task from the process's dependencies. It is a function rather than a
// Task because tasks are declared at init time, before anything has been constructed.
type Registration func(Deps) Task

// registry holds every declared task. Populated by init functions in tasks_*.go.
var registry []Registration

// register declares a task. It is called from init, so it panics rather than returning an error:
// a malformed task is a programming mistake, and the alternative to stopping at startup is a
// worker that runs everything except the one thing somebody got wrong.
func register(r Registration) {
	if r == nil {
		panic("worker: a nil task registration")
	}
	registry = append(registry, r)
}

// tasks builds every registered task, in a stable order.
//
// Sorted by name so that the order two init functions happened to run in cannot change the order
// passes start in, which is the kind of difference that makes a failure reproduce on one machine
// and not another.
func tasks(d Deps) ([]Task, error) {
	out := make([]Task, 0, len(registry))
	for _, build := range registry {
		out = append(out, build(d))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })

	seen := map[string]bool{}
	for _, t := range out {
		switch {
		case t.Name == "":
			return nil, fmt.Errorf("worker: a task has no name")
		case seen[t.Name]:
			return nil, fmt.Errorf("worker: two tasks are called %q; the name is how a "+
				"pass is identified in the log", t.Name)
		case t.Every <= 0:
			return nil, fmt.Errorf("worker: task %q has no interval", t.Name)
		case t.Timeout < 0:
			return nil, fmt.Errorf("worker: task %q has a negative timeout", t.Name)
		case t.Run == nil:
			return nil, fmt.Errorf("worker: task %q does nothing", t.Name)
		}
		seen[t.Name] = true
	}
	return out, nil
}
