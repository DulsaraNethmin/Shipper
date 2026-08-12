package jobs

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
)

// Extending an expiring job (SHIP-70).
//
// Docs/02 §6.3: "The customer is warned 48 hours before expiry and can extend in one action." The
// warning is SHIP-69, in expiry.go; this is the action.
//
// # This is not a status transition, and deciding that was the ticket's first question
//
// A job is Open before an extension and Open after it. Nothing in Docs/02 §2's table describes it,
// no actor moved the job anywhere, and `Open → Open` is a move [Service.Transition] refuses on
// purpose. So none of SHIP-57's machinery is involved: no job_status_history row, no session
// variable, and 000402's guard returns early because the statement does not name `status`.
//
// That is not a loophole — it is what the guard was built to leave alone. Its own comment says
// "every other update to a job — its category, its addresses, its budget — passes straight
// through", and a deadline is one of those. 000406 reached the same conclusion from the other side
// before this file existed: the trigger fills `expires_at` only when it is NULL precisely so that
// "SHIP-70's extend endpoint is an ordinary UPDATE".
//
// The rule the extension *does* touch structurally is SHIP-69's, and that is 000407's trigger
// rather than a line here: moving the deadline clears the warning mark, so the customer is warned
// again forty-eight hours before the new one.

// ExtensionPeriod is how long an extension keeps a job on the market.
//
// The same fourteen days Docs/02 §6.3 gives a job at publication, counted from now rather than
// added to the deadline it already has. Counting from now is what makes this an extension rather
// than a compounding one: a customer acting on the warning gets a fresh listing period, and a
// customer acting early gets no more than one.
//
// A constant rather than configuration, on the reasoning 000406 gives for the fourteen days it
// computes: this is a lifecycle rule from a document, not an operational limit of the kind
// Docs/06 §5.3 requires to be changeable without a deploy.
const ExtensionPeriod = 14 * 24 * time.Hour

// EventExpiryExtended is emitted when a customer moves their job's deadline.
//
// # Why an extension is worth an event when the *Done when* does not ask for one
//
// Because SHIP-69's warning is now wrong, and only this event can say so. A consumer that has
// already told the customer "this job expires in two days" has no other way to learn that it no
// longer does — and 000407 will re-arm the warning, so a second `job.expiry_warned` arrives later
// with nothing to explain why the first one was void. Docs/06 §4 has consumers acting on events
// without reading the database back; this is the event that keeps that true across an extension.
//
// It is also the only record that an extension happened at all. `expires_at` afterwards says the
// deadline moved; it does not say who moved it, from what, or when — and job_status_history is
// deliberately not the place for it, because this is not a transition.
const EventExpiryExtended = "job.expiry_extended"

// Extend gives an expiring job more time, in one call (SHIP-70).
//
// # The new deadline is the earlier of fourteen days from now and the pickup window's end
//
// That is 000406's rule applied again from the moment the customer acts, and it is the whole design.
// Docs/02 §6.3 makes the pickup date the operative rule and the fourteen days a backstop for jobs
// with distant dates: "a job whose pickup window has gone is dead regardless of how recently it was
// posted". An extension that ignored the pickup window would keep a listing in front of providers
// advertising a collection date that had passed, which is worse for the marketplace than the stale
// listing §6.3 is about — it wastes a bid rather than a glance.
//
// So the useful case is exactly the one §6.3 names: a job whose deadline is the backstop, or whose
// pickup window is further out than the deadline it currently has. A job already bounded by its
// pickup date gets [ErrExpiryBoundByPickup], which is the honest answer — the way to keep that job
// alive is to move the pickup date, and no endpoint does that today (see Docs/11 §3).
//
// # What it deliberately does not do
//
// It takes no period from the client. Docs/02 §6.3 says "in one action", and a client naming its
// own deadline is a client choosing how long the platform's rules apply to it — the same shape as
// naming a status, which Docs/02 §2 forbids for the same reason. It is also why the request body
// has no fields at all.
//
// There is no cap on how many times a job may be extended. Every job with a pickup window is
// bounded by it, and a job without one is the "distant dates" case where the customer's continued
// interest is the only signal there is. A cap is a counter column and a policy decision; it is
// recorded as a finding rather than invented here.
//
// # The three refusals, in this order
//
//  1. a job that does not exist is [ErrJobNotFound];
//  2. a job belonging to somebody else is [ErrNotJobOwner];
//  3. a job that is not Open is [ErrJobNotExtendable], and one bounded by its pickup date is
//     [ErrExpiryBoundByPickup].
//
// Ownership before status, exactly as [Service.Cancel] has it and for the same reason: the first
// two are one 404 on the wire, so a stranger who could tell "not extendable" from "no such job"
// would learn that the job exists and roughly what state it is in.
//
// r must be a transaction. The read, the ownership check, the write and the event are one decision
// against one version of the row.
func (s *Service) Extend(ctx context.Context, r db.Runner, customerID, jobID uuid.UUID) (Job, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Job{}, fmt.Errorf("jobs: extending %s: %w", jobID, ErrNotInTransaction)
	}

	// Locked before anything is decided, so the deadline this reads is the deadline the write
	// moves. Without it, two extensions arriving together would each compute a new deadline from
	// the same old one and the second would overwrite the first — harmless here, but the same
	// window would let an extension race the expiry sweep and revive a cancelled job.
	job, err := s.store.lockJob(ctx, r, jobID)
	if err != nil {
		return Job{}, err
	}

	if job.CustomerID != customerID {
		return Job{}, fmt.Errorf("jobs: %s does not belong to %s: %w", jobID, customerID, ErrNotJobOwner)
	}
	if job.Status != StatusOpen {
		return Job{}, fmt.Errorf("jobs: %s is %s: %w", jobID, job.Status, ErrJobNotExtendable)
	}

	at := s.clock.Now().UTC()

	deadline := extendedDeadline(at, job.PickupWindow.End)
	if !deadline.After(job.ExpiresAt) {
		// The pickup window is at or before where the extension would reach, so the job's
		// deadline already *is* its pickup date. Reported as its own sentinel rather than as
		// "nothing changed", because the customer needs to know which of their two dates is
		// ending the job.
		return Job{}, fmt.Errorf("jobs: %s expires at %s, which its pickup window at %s already "+
			"bounds: %w", jobID, job.ExpiresAt, job.PickupWindow.End, ErrExpiryBoundByPickup)
	}

	extended, err := s.store.setDeadline(ctx, r, jobID, deadline)
	if err != nil {
		return Job{}, err
	}

	if err := s.emitExpiryExtended(ctx, r, extended, job.ExpiresAt, at); err != nil {
		return Job{}, err
	}
	return extended, nil
}

// extendedDeadline is Docs/02 §6.3's "earlier of", computed from the instant the customer acted.
//
// A free function rather than a method, and separate from [Service.Extend], because it is the one
// piece of arithmetic in the ticket and a test can hold it to the document directly — including the
// case a running service makes awkward to reach, where the pickup window has already closed and the
// answer is a deadline in the past that the caller must refuse rather than write.
//
// A zero pickupEnd means the job named no pickup window, which is the case the backstop exists for.
func extendedDeadline(at, pickupEnd time.Time) time.Time {
	deadline := at.Add(ExtensionPeriod)
	if !pickupEnd.IsZero() && pickupEnd.Before(deadline) {
		return pickupEnd.UTC()
	}
	return deadline
}

// expiryExtended is the event payload.
//
// Both deadlines, because "the deadline moved" is only actionable to a consumer that knows what it
// moved from: SHIP-69's warning names the instant it was warning about, and a consumer holding one
// needs to see its own instant here to know the warning it sent is the one being voided.
//
// There is no budget field here and there never will be (Docs/01 §4.3).
type expiryExtended struct {
	JobID      string `json:"job_id"`
	CustomerID string `json:"customer_id"`

	PreviousExpiresAt time.Time `json:"previous_expires_at"`
	ExpiresAt         time.Time `json:"expires_at"`

	ExtendedAt time.Time `json:"extended_at"`
}

// emitExpiryExtended writes the domain event, inside the caller's transaction.
func (s *Service) emitExpiryExtended(ctx context.Context, r db.Runner, job Job, previous, at time.Time) error {
	event, err := events.New("job", job.ID, EventExpiryExtended, at, expiryExtended{
		JobID:             job.ID.String(),
		CustomerID:        job.CustomerID.String(),
		PreviousExpiresAt: previous.UTC(),
		ExpiresAt:         job.ExpiresAt.UTC(),
		ExtendedAt:        at,
	})
	if err != nil {
		return err
	}
	return s.events.Emit(ctx, r, event)
}
