package main

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-134's *Done when*: "domain events commit with their transaction and publish at least
// once". Both halves are here, against a real PostgreSQL, because both are properties of the
// transaction rather than of the broker — an event in a rolled-back transaction is never
// published because it was never written, and an event published twice is a row the publisher
// did not get to mark.
//
// The broker itself is stood in for by a recorder. What a real Kafka adds to these assertions is
// the wire format and the acknowledgement, and that is demonstrated end to end against the
// running stack in scripts/verify/80-notifications.sh — an outbox row written with psql, the
// worker started, and the message read back off the topic with the console consumer.

// recorder is an EventPublisher that remembers what it was given, and can be told to fail.
//
// It records every call rather than a set of identifiers, because at-least-once is a claim about
// the *same* event arriving twice, and a set would quietly make that untestable.
type recorder struct {
	mu        sync.Mutex
	published []events.Event
	err       error
}

func (r *recorder) Publish(_ context.Context, batch []events.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.err != nil {
		return r.err
	}
	r.published = append(r.published, batch...)
	return nil
}

func (r *recorder) all() []events.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]events.Event(nil), r.published...)
}

func (r *recorder) ids() []uuid.UUID {
	out := []uuid.UUID{}
	for _, e := range r.all() {
		out = append(out, e.ID)
	}
	return out
}

func (r *recorder) fail(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.err = err
}

// emit writes one event through the same writer every domain uses.
//
// Deliberately internal/events rather than a hand-written INSERT: the two halves of the outbox
// have to agree about the columns, and a test that wrote its own insert would keep agreeing
// after they stopped.
//
// The event type is a real one — jobs.EventStatusChanged — rather than an invented string, and
// since SHIP-135 it has to be: internal/events refuses to write an event type that is in no
// catalogue, which is what makes a permanently unpublishable row impossible and is why the
// publisher still has no dead-letter path. Nothing below distinguishes events by their type; they
// are told apart by their ids.
func emit(t *testing.T, r db.Runner, aggregateID uuid.UUID, occurredAt time.Time) uuid.UUID {
	t.Helper()

	e, err := events.New(jobs.EventStatusChanged, aggregateID, occurredAt,
		map[string]string{"aggregate": aggregateID.String()})
	if err != nil {
		t.Fatalf("building an event: %v", err)
	}
	if err := events.NewOutbox().Emit(t.Context(), r, e); err != nil {
		t.Fatalf("emitting: %v", err)
	}
	return e.ID
}

// unpublished counts the rows a later pass would still find.
func unpublished(t *testing.T, pool *pgxpool.Pool) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&n); err != nil {
		t.Fatalf("counting unpublished events: %v", err)
	}
	return n
}

// drainPass runs one pass and hands back what it claimed *and* whether it failed.
//
// Named apart from tasks_jobs_test.go's runPass rather than sharing it, and the two are not the
// same helper: that one calls t.Fatal on error, which is right for a task that is only ever
// expected to succeed. Three of the tests below are about the pass failing — an unreachable
// broker, a crash before the commit — so the error has to come back rather than end the test.
//
// The collision itself is the ordinary hazard of two tracks adding files to one package: git
// merged them cleanly because the files differ, and `go vet` is what caught it. Same shape as
// SHIP-110's quotedLiteral, and the same resolution — the incomer renames.
func drainPass(t *testing.T, pool *pgxpool.Pool, d outboxDrain) (int, error) {
	t.Helper()

	claimed := 0
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		n, err := d.run(ctx, r)
		claimed = n
		return err
	})
	if err != nil {
		return 0, err
	}
	return claimed, nil
}

// TestAnEventCommittedWithItsTransactionIsPublished is the *Done when*, positive half.
func TestAnEventCommittedWithItsTransactionIsPublished(t *testing.T) {
	pool := pgtest.DB(t)
	job := uuid.New()

	var want []uuid.UUID
	if err := db.InTx(t.Context(), pool, func(_ context.Context, r db.Runner) error {
		want = append(want,
			emit(t, r, job, time.Now().Add(-3*time.Minute)),
			emit(t, r, job, time.Now().Add(-2*time.Minute)))
		return nil
	}); err != nil {
		t.Fatalf("committing the state change: %v", err)
	}

	to := &recorder{}
	claimed, err := drainPass(t, pool, outboxDrain{to: to, batch: 10})
	if err != nil {
		t.Fatalf("the pass failed: %v", err)
	}
	if claimed != 2 {
		t.Errorf("the pass claimed %d events, want 2", claimed)
	}
	if got := to.ids(); len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("published %v, want %v in that order", got, want)
	}
	if n := unpublished(t, pool); n != 0 {
		t.Errorf("%d event(s) are still unpublished after a successful pass", n)
	}

	// The payload survives the round trip. It is jsonb in the table and json.RawMessage
	// either side of it, and a scan that lost it would still have published two events.
	var body map[string]any
	if err := json.Unmarshal(to.all()[0].Payload, &body); err != nil {
		t.Fatalf("the published payload is not the JSON that was written: %v", err)
	}
	if body["aggregate"] != job.String() {
		t.Errorf("payload = %v, want the aggregate %s", body, job)
	}

	// And so does its schema version (SHIP-135), which internal/events wrote into the payload
	// when the row was created rather than the publisher looking it up now. That is the whole
	// distinction: a row written before a deployment and drained after one is entitled to the
	// version it was written under, not to whatever the catalogue says at publish time.
	if v := events.SchemaVersionOf(to.all()[0].Payload); v != 1 {
		t.Errorf("the published payload reports schema version %d, want 1", v)
	}
}

// TestAnEventInARolledBackTransactionIsNeverPublished is the *Done when*, negative half — and
// the reason the outbox exists at all (Docs/06 §4.0). A direct publish would have told a
// consumer about an award that never happened.
func TestAnEventInARolledBackTransactionIsNeverPublished(t *testing.T) {
	pool := pgtest.DB(t)
	job := uuid.New()

	boom := errors.New("the state change failed after the event was written")
	if err := db.InTx(t.Context(), pool, func(_ context.Context, r db.Runner) error {
		emit(t, r, job, time.Now())
		return boom
	}); !errors.Is(err, boom) {
		t.Fatalf("the transaction did not roll back: %v", err)
	}

	to := &recorder{}
	claimed, err := drainPass(t, pool, outboxDrain{to: to, batch: 10})
	if err != nil {
		t.Fatalf("the pass failed: %v", err)
	}
	if claimed != 0 || len(to.all()) != 0 {
		t.Errorf("the pass claimed %d and published %d, want nothing of either",
			claimed, len(to.all()))
	}
}

// TestAnUnreachableBrokerLeavesEveryRowClaimable is the failure this task is most likely to
// meet, and the one where losing an event would be silent.
//
// A pass that cannot publish must return the rows unmarked. The second pass is the assertion
// that matters: the events are still there and still publishable, in the same order.
func TestAnUnreachableBrokerLeavesEveryRowClaimable(t *testing.T) {
	pool := pgtest.DB(t)
	job := uuid.New()

	var want []uuid.UUID
	if err := db.InTx(t.Context(), pool, func(_ context.Context, r db.Runner) error {
		for i := range 3 {
			want = append(want, emit(t, r, job,
				time.Now().Add(-time.Duration(3-i)*time.Minute)))
		}
		return nil
	}); err != nil {
		t.Fatalf("committing the state change: %v", err)
	}

	to := &recorder{}
	to.fail(errors.New("dial tcp: connection refused"))

	if _, err := drainPass(t, pool, outboxDrain{to: to, batch: 10}); err == nil {
		t.Fatal("the pass reported success with an unreachable broker")
	} else if !strings.Contains(err.Error(), "connection refused") {
		t.Errorf("the error does not name the broker failure: %v", err)
	}
	if n := unpublished(t, pool); n != 3 {
		t.Fatalf("%d of 3 events are still claimable after a failed pass; the rest are lost", n)
	}

	to.fail(nil)
	claimed, err := drainPass(t, pool, outboxDrain{to: to, batch: 10})
	if err != nil {
		t.Fatalf("the pass after the broker came back failed: %v", err)
	}
	if claimed != 3 {
		t.Errorf("the recovering pass claimed %d events, want all 3", claimed)
	}
	if got := to.ids(); len(got) != 3 || got[0] != want[0] || got[2] != want[2] {
		t.Errorf("published %v, want %v in that order", got, want)
	}
}

// TestACrashBetweenPublishingAndCommittingRepublishes is at-least-once, shown rather than
// asserted.
//
// The window is real and cannot be closed: the broker has the event and the transaction that
// would have marked it never commits. Docs/06 §4.0 names the consequence — every consumer
// deduplicates on the event identifier — and this is the test that proves the consequence is
// duplication rather than loss.
func TestACrashBetweenPublishingAndCommittingRepublishes(t *testing.T) {
	pool := pgtest.DB(t)
	job := uuid.New()

	var id uuid.UUID
	if err := db.InTx(t.Context(), pool, func(_ context.Context, r db.Runner) error {
		id = emit(t, r, job, time.Now())
		return nil
	}); err != nil {
		t.Fatalf("committing the state change: %v", err)
	}

	to := &recorder{}
	drain := outboxDrain{to: to, batch: 10}

	// The crash: the pass publishes and marks, and then the transaction goes away before it
	// can commit. db.InTx rolls back on any error, which is exactly what a dying process
	// leaves behind.
	crash := errors.New("the worker died before committing")
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		if _, err := drain.run(ctx, r); err != nil {
			return err
		}
		return crash
	}); !errors.Is(err, crash) {
		t.Fatalf("the transaction did not roll back: %v", err)
	}

	if len(to.all()) != 1 {
		t.Fatalf("the broker was given %d events before the crash, want 1", len(to.all()))
	}
	if n := unpublished(t, pool); n != 1 {
		t.Fatalf("the event is marked published although the mark never committed")
	}

	claimed, err := drainPass(t, pool, drain)
	if err != nil {
		t.Fatalf("the pass after the crash failed: %v", err)
	}
	if claimed != 1 {
		t.Errorf("the pass after the crash claimed %d events, want the 1 it lost", claimed)
	}

	got := to.ids()
	if len(got) != 2 || got[0] != id || got[1] != id {
		t.Errorf("the broker saw %v, want %s twice — at-least-once, deduplicated by the "+
			"consumer on the event id", got, id)
	}
	if n := unpublished(t, pool); n != 0 {
		t.Errorf("%d event(s) still unpublished after the recovering pass", n)
	}
}

// TestOneAggregateBelongsToOneWorker is the ordering promise under a rolling deployment.
//
// Two workers are the normal state of a deployment, and SKIP LOCKED alone divides the outbox by
// *row*: the second worker would take job X's third event while the first still held its first
// two, and publish it sooner. Dividing by aggregate is what keeps "events for one job publish in
// the order they were written" true with more than one worker running — see claimUnpublished.
//
// The second half of the assertion matters as much as the first: the second worker must be given
// the *other* aggregate rather than nothing. Excluding it from job X must not mean queueing
// behind the first worker, which is the failure SKIP LOCKED exists to prevent.
func TestOneAggregateBelongsToOneWorker(t *testing.T) {
	pool := pgtest.DB(t)
	held, free := uuid.New(), uuid.New()

	if err := db.InTx(t.Context(), pool, func(_ context.Context, r db.Runner) error {
		for i := range 3 {
			emit(t, r, held,
				time.Now().Add(-time.Duration(10-i)*time.Minute))
		}
		for i := range 2 {
			emit(t, r, free,
				time.Now().Add(-time.Duration(5-i)*time.Minute))
		}
		return nil
	}); err != nil {
		t.Fatalf("committing the state changes: %v", err)
	}

	// The first worker takes one row, which is the oldest and belongs to `held`. It keeps
	// its transaction open, so it keeps the aggregate.
	first, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning the first worker's transaction: %v", err)
	}
	defer func() { _ = first.Rollback(context.WithoutCancel(t.Context())) }()

	one := &recorder{}
	if claimed, err := (outboxDrain{to: one, batch: 1}).run(t.Context(), first); err != nil {
		t.Fatalf("the first worker's pass failed: %v", err)
	} else if claimed != 1 {
		t.Fatalf("the first worker claimed %d events, want its batch of 1", claimed)
	}

	// The second worker, on its own connection, while the first still holds the aggregate.
	other := secondPool(t, pool)
	second, err := other.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning the second worker's transaction: %v", err)
	}
	defer func() { _ = second.Rollback(context.WithoutCancel(t.Context())) }()

	two := &recorder{}
	claimed, err := (outboxDrain{to: two, batch: 10}).run(t.Context(), second)
	if err != nil {
		t.Fatalf("the second worker's pass failed: %v", err)
	}

	if claimed != 2 {
		t.Errorf("the second worker claimed %d events, want the 2 belonging to the "+
			"aggregate the first worker does not hold", claimed)
	}
	for _, e := range two.all() {
		if e.AggregateID == held {
			t.Fatalf("the second worker took %s from an aggregate the first worker "+
				"holds; events for one job can now publish out of order", e.ID)
		}
	}
}

// TestTheOutboxPublisherIsRegistered is the seam, not the work.
//
// A task that is declared and never registered runs nothing, produces no compile error and no
// test failure, and is the exact hazard cmd/worker/manifest.go's registry exists to prevent. The
// only way to notice is to ask the manifest.
//
// Deps is filled out further than this task needs, because tasks() builds *every* registration
// and a half-filled Deps fails in somebody else's closure — SHIP-68's job expiry panics without a
// clock. That is the manifest working as intended rather than a nuisance: the registry is shared,
// so a test that asks it a question has to satisfy every task in it.
func TestTheOutboxPublisherIsRegistered(t *testing.T) {
	built, err := tasks(Deps{
		Config: &config.Config{},
		Logger: slog.New(slog.DiscardHandler),
		Clock:  clock.System{},
	})
	if err != nil {
		t.Fatalf("building the registered tasks: %v", err)
	}

	for _, task := range built {
		if task.Name != outboxTaskName {
			continue
		}
		if task.Close == nil {
			t.Error("the outbox task has no Close; its producer would be dropped at " +
				"shutdown with whatever it had buffered")
		}
		if task.Every != outboxEvery {
			t.Errorf("the outbox task runs every %s, want %s", task.Every, outboxEvery)
		}
		return
	}
	t.Fatalf("no task called %q is registered", outboxTaskName)
}

// TestTheOutboxTaskShutsDownWithNoBrokerConfigured checks the two halves of the empty-brokers
// path together: the task still exists, and it refuses rather than panics.
//
// The zero Deps case is not hypothetical tidiness. tasks() builds every registration, so another
// track's test asking the manifest about *its* task runs this closure with whatever Deps that
// test needed — and SHIP-68's did exactly that, with no Config, and this function took it down.
// The registry is shared even though the files are not.
func TestTheOutboxTaskShutsDownWithNoBrokerConfigured(t *testing.T) {
	for name, deps := range map[string]Deps{
		"no brokers": {Config: &config.Config{}, Logger: slog.New(slog.DiscardHandler)},
		"nothing at all, as another track's manifest test supplies it": {},
	} {
		t.Run(name, func(t *testing.T) {
			w, publish := newKafkaEventPublisher(deps)

			if err := publish.Publish(t.Context(), []events.Event{{}}); err == nil {
				t.Error("publishing with no brokers configured reported success")
			}
			if err := closeWriter(t.Context(), w); err != nil {
				t.Errorf("closing a producer that was never built: %v", err)
			}
		})
	}
}

// TestTheClaimIsAClaim is cheap and worth having: checkClaim is a text check, so a rewrite of
// the query that dropped SKIP LOCKED would pass every other test in this file — one worker
// never notices — and fail only under a second one.
func TestTheClaimIsAClaim(t *testing.T) {
	if err := checkClaim(claimUnpublished); err != nil {
		t.Error(err)
	}
}
