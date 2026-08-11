package jobs

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Cancelling a job (SHIP-64).
//
// # This is the first endpoint in the service that moves a job
//
// SHIP-61 and SHIP-62 never needed the guard: creation lands at Draft because 000400 defaults the
// column, and an edit does not touch status at all. Cancelling is a transition, so everything
// SHIP-57 built — the permitted table, the history row, the trigger that refuses a status write
// without one, the event — runs here for the first time from a request rather than from a test.
//
// Nothing below decides which moves are legal. `permitted` in model.go is that decision, taken
// from Docs/02 §2, and this file asks it rather than restating it.

// maxCancellationReason bounds the reason a customer may give.
//
// A constant for the same reason the draft's limits are: it is a bound against a runaway text
// field rather than a judgement that moves under operational pressure. Longer than a sentence,
// far shorter than the handling notes, because this is read in a support queue rather than by a
// driver standing at a door.
const maxCancellationReason = 500

// Cancel ends a job the caller owns (SHIP-64).
//
// Docs/02 §2 permits `Draft → Cancelled` and `Open/Negotiating → Cancelled`, and permits nothing
// else towards Cancelled from a customer's side. That is what makes "a Draft or an unawarded Open
// job" true without this function containing a list: an Awarded job's only routes out are
// Driver assigned, En route to pickup, Open and Disputed, so [Permitted] refuses the cancellation
// on its own. Docs/02 §6.2 is the reason — once a provider has committed, ending the job is a
// support matter and, after pickup, a dispute.
//
// # The three refusals, in this order
//
//  1. a job that does not exist is [ErrJobNotFound];
//  2. a job belonging to somebody else is [ErrNotJobOwner];
//  3. a job Docs/02 has no `→ Cancelled` row for is [ErrJobNotCancellable].
//
// Ownership before status, exactly as [Service.UpdateDraft] has it, and for the same reason: the
// first two are one 404 on the wire, so a stranger who could tell "not cancellable" from "no such
// job" would learn that the job exists and roughly what state it is in.
//
// # Cancelling a job that is already Cancelled succeeds
//
// It returns the job and writes nothing — no second history row, no second event. The idempotency
// middleware already absorbs a retry that reuses its key; this absorbs the one that does not,
// which is the ordinary shape of a phone that lost its connection, was restarted, and generated a
// fresh key for the same intent. [ErrAlreadyInStatus] exists to be distinguished from
// [ErrTransitionNotPermitted] precisely so a caller can make this choice, and Docs/02 §3.1 makes
// the same call for a queued update that has been overtaken: absorbed, not reported as an error.
//
// r must be a transaction. The read, the ownership check and the transition are one decision
// against one version of the row, and [Service.Transition] refuses a pool anyway.
func (s *Service) Cancel(ctx context.Context, r db.Runner, customerID, jobID uuid.UUID, reason string) (Job, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Job{}, fmt.Errorf("jobs: cancelling %s: %w", jobID, ErrNotInTransaction)
	}

	reason = collapse(reason)
	if err := validateCancellationReason(reason); err != nil {
		return Job{}, err
	}

	// Locked before anything is decided, so the status this reads is the status the
	// transition will act on. Transition locks it again a few statements later, which costs
	// one more statement inside a transaction that already holds the row and buys the
	// precise refusal below — a caller told "this job cannot be cancelled" rather than
	// "that transition is not permitted", which names a move they did not ask for.
	job, err := s.store.lockJob(ctx, r, jobID)
	if err != nil {
		return Job{}, err
	}

	if job.CustomerID != customerID {
		return Job{}, fmt.Errorf("jobs: %s does not belong to %s: %w", jobID, customerID, ErrNotJobOwner)
	}
	if job.Status == StatusCancelled {
		return job, nil
	}
	if !Permitted(job.Status, StatusCancelled) {
		return Job{}, fmt.Errorf("jobs: %s is %s: %w", jobID, job.Status, ErrJobNotCancellable)
	}

	return s.Transition(ctx, r, Move{
		JobID:  jobID,
		To:     StatusCancelled,
		Actor:  User(ActorCustomer, customerID),
		Reason: reason,
	})
}

// validateCancellationReason reports a reason that is too long, in the error contract's shape.
//
// An absent reason is fine. Docs/01 §3 requires one only of an administrator, for whom
// [Move.validate] already refuses the move without it; a customer abandoning their own draft owes
// nobody an explanation.
func validateCancellationReason(reason string) error {
	var e validate.Errors
	e.Length("reason", reason, 0, maxCancellationReason)
	return e.Err()
}
