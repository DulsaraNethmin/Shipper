package jobs

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The sentinel errors this domain raises, and the error codes its endpoints answer with.
//
// Docs/10 §2.1 puts the sentinels here rather than beside the code that returns them, so a caller
// deciding what to do about a failure has one file to read. They are for Go callers, and
// `bidding` awarding a job (SHIP-92) is one of them.
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

	// ErrNotCustomer means the account creating the job is not a customer account.
	//
	// 000400 has no CHECK for it — a foreign key cannot see another table's column — and says
	// the rule is enforced where the draft is created. This is that enforcement, and it reads
	// users.role rather than trusting the role claim in the token, because the claim is
	// evidence about the token and the column is the fact.
	ErrNotCustomer = errors.New("jobs: only a customer account can create a job")
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
var (
	// CodeCustomerOnly is returned when a provider account tries to create a job.
	//
	// A distinct code rather than a bare 403 because the client can act on it: the app shows
	// the provider surface, and a provider reaching this has followed a link or a deep route
	// meant for the other role (Docs/07 §3 — the app may hide, the platform decides).
	CodeCustomerOnly = httpx.RegisterCode("jobs_customer_only",
		"Only a customer account can create or edit a job. Providers bid on jobs; they do not publish them.")
)
