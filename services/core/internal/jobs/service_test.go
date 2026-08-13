package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// The guard is tested against a real PostgreSQL, and it has to be: half of it is a trigger.
// Docs/06 §4.1 — "a mock happily accepts a write that the actual constraint would reject" — is
// the whole argument, and 000402 is exactly that kind of constraint.

// recordingSink watches what a transition emitted without needing the outbox table.
//
// One test uses the real events.Outbox instead, because an event that is only ever written to a
// fake proves nothing about whether it commits with the change it describes.
type recordingSink struct {
	emitted []events.Event
}

func (r *recordingSink) Emit(_ context.Context, _ db.Runner, e events.Event) error {
	r.emitted = append(r.emitted, e)
	return nil
}

// fixedClock is the service's clock, stopped, so that a transition with no actor timestamp of
// its own has a predictable one.
var testInstant = time.Date(2026, 8, 11, 3, 30, 0, 0, time.UTC)

// newTestService builds the service the transition tests use.
//
// No geocoder, deliberately: these tests are about the guard, and a job inserted straight through
// the pool has no address for one to resolve. The draft tests supply the stub.
func newTestService(sink EventSink) *Service {
	return NewService(sink, clock.NewFixed(testInstant), nil)
}

func newCustomer(t *testing.T, pool *pgxpool.Pool, email, phone string) uuid.UUID {
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

// newDraft creates a job the only way a job can be created.
func newDraft(t *testing.T, pool *pgxpool.Pool, customer uuid.UUID) uuid.UUID {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(),
		`INSERT INTO jobs (id, customer_id) VALUES ($1, $2)`, id, customer); err != nil {
		t.Fatalf("inserting a job: %v", err)
	}
	return id
}

// statusOf reads a job's status straight out of the table, bypassing the domain entirely.
func statusOf(t *testing.T, pool *pgxpool.Pool, job uuid.UUID) Status {
	t.Helper()

	var status Status
	if err := pool.QueryRow(t.Context(), `SELECT status FROM jobs WHERE id = $1`, job).Scan(&status); err != nil {
		t.Fatalf("reading the status of %s: %v", job, err)
	}
	return status
}

// TestTransitionRecordsTheActorBothClocksAndTheReason is SHIP-57a's acceptance criterion.
//
// The move is made with the actor's clock forty minutes behind the platform's, which is the
// ordinary case Docs/02 §3.1 describes: a driver records a milestone with no signal and the
// request arrives when the phone finds a tower again. Both times are kept, unaltered.
func TestTransitionRecordsTheActorBothClocksAndTheReason(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "records@example.com", "+61400000410")
	job := newDraft(t, pool, customer)

	actorRecordedAt := time.Now().UTC().Add(-40 * time.Minute).Truncate(time.Millisecond)

	var moved Job
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		moved, err = service.Transition(ctx, r, Move{
			JobID:      job,
			To:         StatusOpen,
			Actor:      User(ActorCustomer, customer),
			Reason:     "published from the app",
			RecordedAt: actorRecordedAt,
		})
		return err
	}); err != nil {
		t.Fatalf("publishing the job: %v", err)
	}

	if moved.Status != StatusOpen {
		t.Errorf("the returned job is %s, want Open", moved.Status)
	}
	if got := statusOf(t, pool, job); got != StatusOpen {
		t.Errorf("the stored job is %s, want Open", got)
	}

	history, err := service.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("the job has %d recorded transitions, want 1", len(history))
	}

	change := history[0]
	switch {
	case change.From != StatusDraft || change.To != StatusOpen:
		t.Errorf("recorded %s → %s, want Draft → Open", change.From, change.To)
	case change.Actor.Type != ActorCustomer:
		t.Errorf("actor kind is %q, want customer", change.Actor.Type)
	case change.Actor.ID != customer:
		t.Errorf("actor is %s, want %s", change.Actor.ID, customer)
	case change.Reason != "published from the app":
		t.Errorf("reason is %q", change.Reason)
	}

	if !change.ActorRecordedAt.Equal(actorRecordedAt) {
		t.Errorf("actor_recorded_at is %s, want the %s the actor claimed — the platform does "+
			"not correct the actor's clock (Docs/02 §3.1)", change.ActorRecordedAt, actorRecordedAt)
	}
	if !change.ServerRecordedAt.After(change.ActorRecordedAt) {
		t.Errorf("server_recorded_at (%s) is not after actor_recorded_at (%s); the two clocks "+
			"have been collapsed into one", change.ServerRecordedAt, change.ActorRecordedAt)
	}

	if len(sink.emitted) != 1 {
		t.Fatalf("the transition emitted %d events, want 1", len(sink.emitted))
	}
	if sink.emitted[0].Type != EventStatusChanged {
		t.Errorf("emitted %q, want %q", sink.emitted[0].Type, EventStatusChanged)
	}

	var payload statusChanged
	if err := json.Unmarshal(sink.emitted[0].Payload, &payload); err != nil {
		t.Fatalf("the payload does not parse: %v", err)
	}
	if payload.From != string(StatusDraft) || payload.To != string(StatusOpen) {
		t.Errorf("the payload says %s → %s", payload.From, payload.To)
	}
	if !payload.ActorRecordedAt.Equal(actorRecordedAt) {
		t.Errorf("the payload's actor_recorded_at is %s, want %s", payload.ActorRecordedAt, actorRecordedAt)
	}
	if payload.ServerRecordedAt.IsZero() {
		t.Error("the payload carries only one of the two clocks")
	}
}

// TestTransitionWithNoActorTimeUsesTheServiceClock covers the ordinary online case, where there
// is only one clock because the actor and the platform are the same round trip apart.
func TestTransitionWithNoActorTimeUsesTheServiceClock(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "one-clock@example.com", "+61400000411")
	job := newDraft(t, pool, customer)

	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Transition(ctx, r, Move{
			JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer),
		})
		return err
	}); err != nil {
		t.Fatalf("publishing the job: %v", err)
	}

	history, err := service.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if !history[0].ActorRecordedAt.Equal(testInstant) {
		t.Errorf("actor_recorded_at is %s, want the service clock's %s",
			history[0].ActorRecordedAt, testInstant)
	}
}

// TestTransitionEmitsIntoTheOutboxWithTheChange is the seam Docs/10 §6.1 requires, checked with
// the real writer rather than a fake.
//
// The event and the status change are one transaction: rolling back must lose both, or the
// outbox is not an outbox. Docs/06 §4.0 names the failure it prevents — a consumer acting on an
// award that never committed.
func TestTransitionEmitsIntoTheOutboxWithTheChange(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(events.NewOutbox())

	customer := newCustomer(t, pool, "outbox@example.com", "+61400000412")

	t.Run("committed", func(t *testing.T) {
		job := newDraft(t, pool, customer)
		if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			_, err := service.Transition(ctx, r, Move{
				JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer),
			})
			return err
		}); err != nil {
			t.Fatalf("publishing: %v", err)
		}

		var eventType string
		if err := pool.QueryRow(t.Context(),
			`SELECT event_type FROM outbox WHERE aggregate_type = 'job' AND aggregate_id = $1`, job,
		).Scan(&eventType); err != nil {
			t.Fatalf("the transition committed with no event in the outbox: %v", err)
		}
		if eventType != EventStatusChanged {
			t.Errorf("outbox holds %q, want %q", eventType, EventStatusChanged)
		}
	})

	t.Run("rolled back", func(t *testing.T) {
		job := newDraft(t, pool, customer)
		abandoned := errors.New("something later in the transaction failed")

		err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			if _, err := service.Transition(ctx, r, Move{
				JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer),
			}); err != nil {
				return err
			}
			return abandoned
		})
		if !errors.Is(err, abandoned) {
			t.Fatalf("InTx returned %v, want the caller's own error", err)
		}

		if got := statusOf(t, pool, job); got != StatusDraft {
			t.Errorf("the job is %s after a rolled back transition", got)
		}

		var events, history int
		if err := pool.QueryRow(t.Context(),
			`SELECT (SELECT count(*) FROM outbox WHERE aggregate_id = $1),
			        (SELECT count(*) FROM job_status_history WHERE job_id = $1)`, job,
		).Scan(&events, &history); err != nil {
			t.Fatalf("counting what survived: %v", err)
		}
		if events != 0 {
			t.Errorf("%d event(s) survived a rolled back transition", events)
		}
		if history != 0 {
			t.Errorf("%d history row(s) survived a rolled back transition", history)
		}
	})
}

// TestTransitionRefusesWhatDocs02DoesNot is the other half of the guard: the move has to be one
// the lifecycle has.
func TestTransitionRefusesWhatDocs02DoesNot(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "refused@example.com", "+61400000413")
	job := newDraft(t, pool, customer)

	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Transition(ctx, r, Move{
			JobID: job, To: StatusDelivered, Actor: User(ActorCustomer, customer),
		})
		return err
	})
	if !errors.Is(err, ErrTransitionNotPermitted) {
		t.Fatalf("Transition() = %v, want ErrTransitionNotPermitted", err)
	}

	if got := statusOf(t, pool, job); got != StatusDraft {
		t.Errorf("the job is %s after a refused transition", got)
	}

	history, err := service.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if len(history) != 0 {
		t.Errorf("a refused transition left %d history row(s); nothing happened, so nothing "+
			"should be recorded as having happened", len(history))
	}
}

// TestTransitionRefusesARepeat separates "already done" from "not allowed".
//
// The two want opposite handling upstream. A milestone the driver's phone sent twice is a retry
// and the answer is usually "yes, that is done"; a move Docs/02 has no row for is a defect.
func TestTransitionRefusesARepeat(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "repeat@example.com", "+61400000414")
	job := newDraft(t, pool, customer)

	publish := func() error {
		return db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			_, err := service.Transition(ctx, r, Move{
				JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer),
			})
			return err
		})
	}

	if err := publish(); err != nil {
		t.Fatalf("publishing: %v", err)
	}
	if err := publish(); !errors.Is(err, ErrAlreadyInStatus) {
		t.Errorf("publishing twice = %v, want ErrAlreadyInStatus", err)
	}
}

// TestTransitionNeedsATransaction is the check that runs before anything is written, and the
// reason it cannot be left to the database.
//
// 000402 would refuse the update on its own — the setting naming the history row is
// transaction-local — but by then the history row would have committed by itself, leaving a
// record of a transition that never happened. Nothing downstream could tell that record from a
// real one.
func TestTransitionNeedsATransaction(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "no-tx@example.com", "+61400000415")
	job := newDraft(t, pool, customer)

	_, err := service.Transition(t.Context(), pool, Move{
		JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer),
	})
	if !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("Transition() with a pool = %v, want ErrNotInTransaction", err)
	}

	var history int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1`, job).Scan(&history); err != nil {
		t.Fatalf("counting the history: %v", err)
	}
	if history != 0 {
		t.Errorf("%d history row(s) were written outside a transaction and cannot be rolled back", history)
	}
	if got := statusOf(t, pool, job); got != StatusDraft {
		t.Errorf("the job is %s", got)
	}
}

// TestTransitionRefusesAnUnknownJob keeps "no such job" distinct from every other failure, since
// it is the one an endpoint turns into a 404.
func TestTransitionRefusesAnUnknownJob(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "unknown-job@example.com", "+61400000416")
	stranger, _ := uuid.NewV7()

	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := service.Transition(ctx, r, Move{
			JobID: stranger, To: StatusOpen, Actor: User(ActorCustomer, customer),
		})
		return err
	})
	if !errors.Is(err, ErrJobNotFound) {
		t.Errorf("Transition() = %v, want ErrJobNotFound", err)
	}
}

// TestTheSystemAndTheAdministratorAreBothRecordable covers the two actors that are not an
// ordinary account: the platform expiring a job on its own (SHIP-68), and an administrator, who
// may not act without saying why.
func TestTheSystemAndTheAdministratorAreBothRecordable(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "system-actor@example.com", "+61400000417")
	administrator, _ := uuid.NewV7()

	move := func(job uuid.UUID, m Move) error {
		return db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
			_, err := service.Transition(ctx, r, m)
			return err
		})
	}

	t.Run("the platform expires a job", func(t *testing.T) {
		job := newDraft(t, pool, customer)
		if err := move(job, Move{JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer)}); err != nil {
			t.Fatalf("publishing: %v", err)
		}
		if err := move(job, Move{JobID: job, To: StatusCancelled, Actor: System(), Reason: "expired unclaimed"}); err != nil {
			t.Fatalf("expiring: %v", err)
		}

		history, err := service.History(t.Context(), pool, job)
		if err != nil {
			t.Fatalf("reading the history: %v", err)
		}
		if len(history) != 2 {
			t.Fatalf("the job has %d transitions, want 2", len(history))
		}
		if history[1].Actor.Type != ActorSystem || history[1].Actor.ID != uuid.Nil {
			t.Errorf("the expiry was recorded as %s/%s, want system with no account",
				history[1].Actor.Type, history[1].Actor.ID)
		}
	})

	t.Run("an administrator must say why", func(t *testing.T) {
		job := newDraft(t, pool, customer)
		if err := move(job, Move{JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer)}); err != nil {
			t.Fatalf("publishing: %v", err)
		}

		err := move(job, Move{JobID: job, To: StatusCancelled, Actor: User(ActorAdmin, administrator)})
		if !errors.Is(err, ErrReasonRequired) {
			t.Errorf("an administrator cancelled with no reason: %v", err)
		}

		if err := move(job, Move{
			JobID: job, To: StatusCancelled, Actor: User(ActorAdmin, administrator),
			Reason: "prohibited goods",
		}); err != nil {
			t.Errorf("an administrator with a reason was refused: %v", err)
		}
	})
}

// TestTwoTransitionsAtOnceCannotBothWin is why lockJob selects FOR UPDATE.
//
// Without the lock both transactions read Draft, both find Draft → Open permitted, and both
// commit — one job with two publication records and two events. With it, the second blocks, and
// PostgreSQL's READ COMMITTED re-reads the row it was waiting for, so the check runs against the
// status the job actually has by then.
func TestTwoTransitionsAtOnceCannotBothWin(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "race@example.com", "+61400000418")
	job := newDraft(t, pool, customer)

	first, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("beginning the first transaction: %v", err)
	}
	defer func() { _ = first.Rollback(context.WithoutCancel(t.Context())) }()

	if _, err := service.Transition(t.Context(), first, Move{
		JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer),
	}); err != nil {
		t.Fatalf("the first transition: %v", err)
	}

	second := make(chan error, 1)
	go func() {
		second <- db.InTx(context.Background(), pool, func(ctx context.Context, r db.Runner) error {
			_, err := service.Transition(ctx, r, Move{
				JobID: job, To: StatusOpen, Actor: User(ActorCustomer, customer),
			})
			return err
		})
	}()

	// The second transaction is now blocked on the row lock, or about to be. Committing the
	// first is what releases it.
	select {
	case err := <-second:
		t.Fatalf("the second transition did not wait for the row lock: %v", err)
	case <-time.After(250 * time.Millisecond):
	}

	if err := first.Commit(t.Context()); err != nil {
		t.Fatalf("committing the first transaction: %v", err)
	}

	select {
	case err := <-second:
		if !errors.Is(err, ErrAlreadyInStatus) {
			t.Errorf("the second transition = %v, want ErrAlreadyInStatus", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the second transition never returned after the first committed")
	}

	var history int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM job_status_history WHERE job_id = $1`, job).Scan(&history); err != nil {
		t.Fatalf("counting the history: %v", err)
	}
	if history != 1 {
		t.Errorf("the job has %d recorded publications, want 1", history)
	}
}

// TestNewServiceRefusesToBeBuiltWithoutItsCollaborators keeps the panic honest. A service with no
// event sink would run perfectly and lose every domain event, which is the kind of failure that
// is noticed a milestone later.
//
// A nil geocoder is deliberately not among them: it means "addresses are stored unresolved",
// which is a state the domain supports, and SHIP-59a already requires every path to cope with an
// address that did not resolve.
func TestNewServiceRefusesToBeBuiltWithoutItsCollaborators(t *testing.T) {
	cases := map[string]func(){
		"no sink":  func() { NewService(nil, clock.System{}, nil) },
		"no clock": func() { NewService(&recordingSink{}, nil, nil) },
	}

	for name, build := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					t.Fatal("NewService returned a service that cannot work")
				}
				if !strings.Contains(recovered.(string), "jobs:") {
					t.Errorf("the panic does not say which package: %v", recovered)
				}
			}()
			build()
		})
	}
}
