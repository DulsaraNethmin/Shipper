package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// EventStatusChanged is emitted for every transition, whichever transition it is.
//
// One event rather than twelve. A consumer that cares about publication reads `from` and `to`,
// and SHIP-134's publisher has one shape to drain — whereas a per-transition event type would
// have to be invented again by each ticket that adds a transition, which is the divergence
// Docs/10 §6.1 says the seam exists to prevent. A ticket that needs a richer event for its own
// reasons emits it in addition, not instead.
const EventStatusChanged = "job.status_changed"

// Service holds this domain's rules.
//
// It owns no connection. Every method takes a db.Runner, because the transaction belongs to
// whoever owns the invariant being protected (Docs/10 §3.2) — and for the award that is
// `bidding`, which moves a job inside a transaction of its own without importing this package.
type Service struct {
	events EventSink
	clock  clock.Clock
	geo    Geocoder
	store  postgresStore
}

// NewService builds the domain service.
//
// The sink and the clock are required and it panics without them, in the same spirit as
// httpx.RegisterCode: this is called once from the composition root, a missing collaborator is a
// programming mistake rather than a runtime condition, and the alternative is a service that
// starts and then loses every domain event it should have emitted.
//
// # The geocoder may be nil, and that is not the same kind of omission
//
// A nil Geocoder means addresses are stored exactly as the customer typed them, with no
// coordinate. That is a supported state rather than a broken one: SHIP-59a already requires an
// unrecognised address not to fail the job, so every path through this domain has to cope with an
// unresolved location anyway, and a deployment with no maps vendor configured lands on the same
// path rather than a special one.
//
// It is also the honest answer to the situation cmd/api is in today — no vendor is named and no
// GEOCODING_* configuration exists — because the alternative is worse. Falling back to the
// deterministic stub outside development would write plausible-looking coordinates that are
// fiction, and a fictional coordinate on a real job is harder to notice than none at all.
func NewService(sink EventSink, c clock.Clock, geo Geocoder) *Service {
	if sink == nil {
		panic("jobs: NewService needs an EventSink; a transition that emits no event is " +
			"a state change nothing downstream will ever hear about (Docs/10 §6.1)")
	}
	if c == nil {
		panic("jobs: NewService needs a clock (Docs/10 §6.3)")
	}
	return &Service{events: sink, clock: c, geo: geo}
}

// Transition is the guarded function. Every job status change in the platform passes through it.
//
// CLAUDE.md states the invariant — "job status is never a settable field; all transitions pass
// one guarded function" — and this is that function. What makes it more than a convention is
// that the database refuses the write on its own (000402): the status column may only move when
// a job_status_history row written in the same transaction describes the move. So a future
// postgres.go that writes `UPDATE jobs SET status = …` directly does not quietly work.
//
// The order of the four statements below is fixed by that trigger and is not free to tidy:
//
//  1. lock the job, so the check that follows is against the status it has now;
//  2. write the history row, because the trigger looks for it;
//  3. name that row in a transaction-local setting;
//  4. update the status.
//
// The event is emitted last and inside the same transaction, per Docs/10 §6.1 — an event that
// commits when the change does not is the failure the outbox exists to prevent.
//
// r must be a transaction. That is checked before anything is written: the trigger would refuse
// the update anyway, but by then the history row would have committed on its own and left a
// record of a transition that never happened.
func (s *Service) Transition(ctx context.Context, r db.Runner, m Move) (Job, error) {
	if err := m.validate(); err != nil {
		return Job{}, err
	}

	// pgx.Tx and *pgxpool.Pool both satisfy db.Runner and only one of them is a transaction.
	// Asking the type is blunt, and it is the only way to tell: Runner exists precisely so
	// that a query does not have to know which it is holding.
	if _, inTx := r.(pgx.Tx); !inTx {
		return Job{}, fmt.Errorf("jobs: %s to %s: %w", m.JobID, m.To, ErrNotInTransaction)
	}

	job, err := s.store.lockJob(ctx, r, m.JobID)
	if err != nil {
		return Job{}, err
	}

	if job.Status == m.To {
		return Job{}, fmt.Errorf("jobs: %s is already %s: %w", job.ID, m.To, ErrAlreadyInStatus)
	}
	if !Permitted(job.Status, m.To) {
		return Job{}, fmt.Errorf("jobs: %s cannot move from %s to %s: %w",
			job.ID, job.Status, m.To, ErrTransitionNotPermitted)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Job{}, fmt.Errorf("jobs: generating a history id: %w", err)
	}

	recordedAt := m.RecordedAt
	if recordedAt.IsZero() {
		// Nothing was claimed about when the actor acted, which is the ordinary case for
		// anything happening online: the two clocks agree because there is only one.
		recordedAt = s.clock.Now()
	}

	change := StatusChange{
		ID:              id,
		JobID:           job.ID,
		From:            job.Status,
		To:              m.To,
		Actor:           m.Actor,
		Reason:          m.Reason,
		ActorRecordedAt: recordedAt.UTC(),
	}

	if change.ServerRecordedAt, err = s.store.recordTransition(ctx, r, change); err != nil {
		return Job{}, err
	}
	if err := s.store.claimTransition(ctx, r, change.ID); err != nil {
		return Job{}, err
	}

	updated, err := s.store.setStatus(ctx, r, job.ID, m.To)
	if err != nil {
		return Job{}, err
	}

	if err := s.emit(ctx, r, change); err != nil {
		return Job{}, err
	}
	return updated, nil
}

// History is every recorded transition for one job, oldest first.
func (s *Service) History(ctx context.Context, r db.Runner, jobID uuid.UUID) ([]StatusChange, error) {
	return s.store.history(ctx, r, jobID)
}

// statusChanged is the event payload.
//
// The status values are the stored form — Docs/02 §1's own strings — rather than the lower
// snake case Docs/10 §4.7 puts on the public wire. This is an internal event read by SHIP-134's
// publisher and the consumers behind it, not a response body, and the mapping between the two
// forms arrives with SHIP-56a, which owns it in all three languages.
//
// There is no budget field here and there never will be. Docs/01 §4.3 keeps the customer's
// maximum private from providers, and an event is a copy of a job that travels further than the
// endpoint that would have to redact it.
type statusChanged struct {
	JobID string `json:"job_id"`
	From  string `json:"from"`
	To    string `json:"to"`

	ActorType string `json:"actor_type"`
	ActorID   string `json:"actor_id,omitempty"`
	Reason    string `json:"reason,omitempty"`

	ActorRecordedAt  time.Time `json:"actor_recorded_at"`
	ServerRecordedAt time.Time `json:"server_recorded_at"`
}

// emit writes the domain event, inside the caller's transaction.
//
// OccurredAt is the platform's clock rather than the actor's, and deliberately: an event says
// what the platform decided and when it decided it. The actor's claim about when they acted
// travels in the payload, where a consumer that cares can see both and tell them apart.
func (s *Service) emit(ctx context.Context, r db.Runner, c StatusChange) error {
	payload := statusChanged{
		JobID:            c.JobID.String(),
		From:             string(c.From),
		To:               string(c.To),
		ActorType:        string(c.Actor.Type),
		Reason:           c.Reason,
		ActorRecordedAt:  c.ActorRecordedAt,
		ServerRecordedAt: c.ServerRecordedAt,
	}
	if c.Actor.ID != uuid.Nil {
		payload.ActorID = c.Actor.ID.String()
	}

	event, err := events.New(EventStatusChanged, c.JobID, c.ServerRecordedAt, payload)
	if err != nil {
		return err
	}
	if err := s.events.Emit(ctx, r, event); err != nil {
		return err
	}
	return nil
}

// validate checks what the caller supplied, before anything is locked or written.
//
// These are the same rules 000401 carries as constraints. Both exist on purpose: the constraints
// are what make the guarantee true of the database, and these are what make the failure legible
// to whoever called — a caller that has forgotten an administrator's reason should be told that,
// not handed ck_job_status_history_admin_reason.
func (m Move) validate() error {
	if m.JobID == uuid.Nil {
		return fmt.Errorf("jobs: a transition names no job: %w", ErrJobNotFound)
	}
	if !m.To.Valid() {
		return fmt.Errorf("jobs: %q: %w", m.To, ErrInvalidStatus)
	}
	if !m.Actor.Type.Valid() {
		return fmt.Errorf("jobs: %q: %w", m.Actor.Type, ErrInvalidActor)
	}

	// The platform has no account; everybody else must name theirs. Attribution is the whole
	// value of the history table, and "somebody did this" is not attribution.
	switch {
	case m.Actor.Type == ActorSystem && m.Actor.ID != uuid.Nil:
		return fmt.Errorf("jobs: the platform acts as itself and has no account: %w", ErrInvalidActor)
	case m.Actor.Type != ActorSystem && m.Actor.ID == uuid.Nil:
		return fmt.Errorf("jobs: a %s transition names no %s: %w", m.Actor.Type, m.Actor.Type, ErrInvalidActor)
	}

	if m.Actor.Type == ActorAdmin && m.Reason == "" {
		return fmt.Errorf("jobs: moving %s to %s: %w", m.JobID, m.To, ErrReasonRequired)
	}
	return nil
}
