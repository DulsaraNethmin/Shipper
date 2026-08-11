package jobs

import "errors"

// The sentinel errors this domain raises.
//
// Docs/10 §2.1 puts them here rather than beside the code that returns them, so a caller
// deciding what to do about a failure has one file to read. The machine-readable error codes
// that reach a client are a separate list and arrive with the first endpoint (SHIP-61, SHIP-64);
// these are for Go callers, and `bidding` awarding a job (SHIP-92) is one of them.
var (
	// ErrJobNotFound means no job with that identifier exists. It is deliberately not
	// distinguished from "exists and is none of your business" — that decision belongs to
	// the endpoint, which is the layer that knows who is asking.
	ErrJobNotFound = errors.New("jobs: no such job")

	// ErrTransitionNotPermitted means Docs/02 §2 has no such move.
	//
	// A caller absorbing a late offline update tests for this rather than treating it as a
	// failure: Docs/02 §3.1 requires a queued "Picked up" that arrives after "In transit" to
	// be accepted as a historical fact without moving the job backwards.
	ErrTransitionNotPermitted = errors.New("jobs: that transition is not permitted")

	// ErrAlreadyInStatus means the job is already where the move would take it.
	//
	// Separate from ErrTransitionNotPermitted because the two want opposite handling. A
	// repeat of a milestone that has already been recorded is a retry, and the answer to a
	// retry is usually "yes, that is done" rather than an error.
	ErrAlreadyInStatus = errors.New("jobs: the job is already in that status")

	// ErrInvalidStatus means a status outside the twelve in Docs/02 §1.
	ErrInvalidStatus = errors.New("jobs: not one of the twelve job statuses")

	// ErrInvalidActor means an actor the platform does not record transitions for, or an
	// account-backed actor with no account — 'system' is the only kind that may have none.
	ErrInvalidActor = errors.New("jobs: not an actor this platform records transitions for")

	// ErrReasonRequired means an administrator moved a job without saying why. Docs/01 §3
	// forbids an administrator changing a commercial record without an auditable reason, and
	// job_status_history is where that reason is auditable.
	ErrReasonRequired = errors.New("jobs: an administrator's transition needs a reason")

	// ErrNotInTransaction means Transition was handed a connection pool rather than a
	// transaction.
	//
	// The database refuses the update in that case anyway — the session variable naming the
	// history row is transaction-local, so outside a transaction it is gone by the time the
	// update runs. This is caught first because by then the history row would already have
	// been committed on its own, leaving a record of a transition that never happened.
	ErrNotInTransaction = errors.New("jobs: a transition must run inside a transaction")
)
