package jobs

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-64 against a real PostgreSQL.
//
// The guard is half trigger (000402), so these run against the database for the same reason the
// transition tests do: a mock would accept every write the constraint exists to have refused.

// cancel runs a cancellation in a transaction, which is what Service.Cancel requires.
func cancel(t *testing.T, pool *pgxpool.Pool, svc *Service, customer, job uuid.UUID, reason string) (Job, error) {
	t.Helper()

	var cancelled Job
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		cancelled, err = svc.Cancel(ctx, r, customer, job, reason)
		return err
	})
	return cancelled, err
}

// move applies any transition through the guard, so a test can put a job in a state the
// endpoints themselves cannot reach yet — Awarded has no endpoint until SHIP-92.
func move(t *testing.T, pool *pgxpool.Pool, job uuid.UUID, to Status, actor Actor) {
	t.Helper()

	svc := newTestService(&recordingSink{})
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		_, err := svc.Transition(ctx, r, Move{JobID: job, To: to, Actor: actor})
		return err
	}); err != nil {
		t.Fatalf("moving %s to %s: %v", job, to, err)
	}
}

// TestCancelEndsADraftAndAnUnawardedOpenJob is SHIP-64's acceptance criterion.
//
// Both permitted moves, not one: Docs/02 §2 has `Draft → Cancelled` and
// `Open/Negotiating → Cancelled`, and a test that only covered the first would pass with an
// implementation that refused every published job.
func TestCancelEndsADraftAndAnUnawardedOpenJob(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "cancel-both@example.com", "+61400000640")

	cases := map[string]func() uuid.UUID{
		"a draft": func() uuid.UUID { return newDraft(t, pool, customer) },
		"an unawarded open job": func() uuid.UUID {
			job := newDraft(t, pool, customer)
			publish(t, pool, job, customer)
			return job
		},
		"a job under negotiation": func() uuid.UUID {
			job := newDraft(t, pool, customer)
			publish(t, pool, job, customer)
			move(t, pool, job, StatusNegotiating, User(ActorCustomer, customer))
			return job
		},
	}

	for name, setUp := range cases {
		t.Run(name, func(t *testing.T) {
			job := setUp()
			before := statusOf(t, pool, job)

			cancelled, err := cancel(t, pool, service, customer, job, "changed my mind")
			if err != nil {
				t.Fatalf("cancelling %s: %v", before, err)
			}

			if cancelled.Status != StatusCancelled {
				t.Errorf("the returned job is %s, want Cancelled", cancelled.Status)
			}
			if got := statusOf(t, pool, job); got != StatusCancelled {
				t.Errorf("the stored job is %s, want Cancelled", got)
			}

			// The transition went through the guard, which means it left the record
			// 000402 refuses the status write without.
			history, err := service.History(t.Context(), pool, job)
			if err != nil {
				t.Fatalf("reading the history: %v", err)
			}
			last := history[len(history)-1]
			switch {
			case last.From != before || last.To != StatusCancelled:
				t.Errorf("recorded %s → %s, want %s → Cancelled", last.From, last.To, before)
			case last.Actor.Type != ActorCustomer || last.Actor.ID != customer:
				t.Errorf("the cancellation was recorded as %s/%s", last.Actor.Type, last.Actor.ID)
			case last.Reason != "changed my mind":
				t.Errorf("reason = %q, want the one the customer gave", last.Reason)
			}
		})
	}
}

// TestCancelRefusesAJobThatHasBeenAwarded is the refusal half, and the one worth being careful
// about: "unawarded" in SHIP-64's Done when is enforced by Docs/02 §2's table rather than by a
// list in cancel.go, so this is what proves the table is actually consulted.
//
// Docs/02 §6.2 is the reason. Once a provider has committed, ending the job is a support matter,
// and after pickup the route out is Disputed rather than Cancelled.
func TestCancelRefusesAJobThatHasBeenAwarded(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "cancel-awarded@example.com", "+61400000641")
	job := newDraft(t, pool, customer)
	publish(t, pool, job, customer)
	move(t, pool, job, StatusAwarded, User(ActorCustomer, customer))

	if _, err := cancel(t, pool, service, customer, job, ""); !errors.Is(err, ErrJobNotCancellable) {
		t.Fatalf("cancelling an awarded job = %v, want ErrJobNotCancellable", err)
	}

	if got := statusOf(t, pool, job); got != StatusAwarded {
		t.Errorf("the job is %s after a refused cancellation", got)
	}

	// Nothing happened, so nothing is recorded as having happened.
	history, err := service.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if len(history) != 2 {
		t.Errorf("the job has %d recorded transitions, want the 2 that really occurred", len(history))
	}
}

// TestCancelRejectsCancellationsByNonOwners keeps the two refusals apart in Go while they are one
// answer on the wire.
//
// A test asserting "the stranger was refused" should fail if the job silently stopped existing
// instead — the same 404 to a client and very different defects.
func TestCancelRejectsCancellationsByNonOwners(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	owner := newCustomer(t, pool, "cancel-owner@example.com", "+61400000642")
	stranger := newCustomer(t, pool, "cancel-stranger@example.com", "+61400000643")
	job := newDraft(t, pool, owner)

	if _, err := cancel(t, pool, service, stranger, job, ""); !errors.Is(err, ErrNotJobOwner) {
		t.Fatalf("a stranger's cancellation = %v, want ErrNotJobOwner", err)
	}
	if got := statusOf(t, pool, job); got != StatusDraft {
		t.Errorf("the job is %s after a stranger tried to cancel it", got)
	}

	missing, _ := uuid.NewV7()
	if _, err := cancel(t, pool, service, stranger, missing, ""); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("cancelling a job that does not exist = %v, want ErrJobNotFound", err)
	}
}

// TestCancellingAnAlreadyCancelledJobIsAbsorbed covers the retry that does not reuse its
// idempotency key — a phone that lost its connection, was restarted, and generated a fresh one for
// the same intent.
//
// The outcome the caller asked for holds, so the answer is the job rather than an error, and
// nothing further is written: a second history row would say a job was cancelled twice.
func TestCancellingAnAlreadyCancelledJobIsAbsorbed(t *testing.T) {
	pool := pgtest.DB(t)
	sink := &recordingSink{}
	service := newTestService(sink)

	customer := newCustomer(t, pool, "cancel-twice@example.com", "+61400000644")
	job := newDraft(t, pool, customer)

	if _, err := cancel(t, pool, service, customer, job, "first"); err != nil {
		t.Fatalf("the first cancellation: %v", err)
	}

	emittedOnce := len(sink.emitted)

	again, err := cancel(t, pool, service, customer, job, "second")
	if err != nil {
		t.Fatalf("cancelling twice = %v, want the job (Docs/02 §3.1 absorbs an overtaken update)", err)
	}
	if again.Status != StatusCancelled {
		t.Errorf("the returned job is %s, want Cancelled", again.Status)
	}

	history, err := service.History(t.Context(), pool, job)
	if err != nil {
		t.Fatalf("reading the history: %v", err)
	}
	if len(history) != 1 {
		t.Errorf("the job has %d recorded cancellations, want 1", len(history))
	}
	if len(sink.emitted) != emittedOnce {
		t.Errorf("the second cancellation emitted %d further event(s)", len(sink.emitted)-emittedOnce)
	}
}

// TestCancelRefusesToRunOutsideATransaction is the same check Transition and UpdateDraft make, for
// the same reason: the read, the ownership check and the transition are one decision against one
// version of the row, and lockJob's FOR UPDATE only holds for the length of a transaction.
func TestCancelRefusesToRunOutsideATransaction(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "cancel-no-tx@example.com", "+61400000645")
	job := newDraft(t, pool, customer)

	if _, err := service.Cancel(t.Context(), pool, customer, job, ""); !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("Cancel() with a pool = %v, want ErrNotInTransaction", err)
	}
	if got := statusOf(t, pool, job); got != StatusDraft {
		t.Errorf("the job is %s", got)
	}
}

// A reason longer than the column should carry is reported in the error contract's shape, naming
// the field, rather than reaching the database.
func TestCancelRefusesAnOverlongReason(t *testing.T) {
	pool := pgtest.DB(t)
	service := newTestService(&recordingSink{})

	customer := newCustomer(t, pool, "cancel-long@example.com", "+61400000646")
	job := newDraft(t, pool, customer)

	_, err := cancel(t, pool, service, customer, job, strings.Repeat("a", maxCancellationReason+1))
	if err == nil {
		t.Fatal("an overlong reason was accepted")
	}

	// Reported as a field problem in the error contract's own shape, not as a message a
	// client would have to read: the app puts it beside the input that produced it.
	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("Cancel() = %v, want an error in the contract's shape", err)
	}
	named := false
	for _, detail := range apiErr.Details {
		if detail.Field == "reason" {
			named = true
		}
	}
	if !named {
		t.Errorf("no detail names the reason field: %#v", apiErr.Details)
	}
	if got := statusOf(t, pool, job); got != StatusDraft {
		t.Errorf("the job is %s after a refused cancellation", got)
	}
}
