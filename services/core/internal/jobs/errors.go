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
//
// The codes below are what reaches a client, and they are a separate list on purpose: a sentinel
// says what happened, a code says what the client should do about it, and the two do not map one
// to one — ErrNotJobOwner and ErrJobNotFound deliberately share `not_found`, because telling a
// caller which of the two it was would confirm the existence of somebody else's job.
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

	// ErrNotJobOwner means the job exists and belongs to somebody else.
	//
	// Distinct from ErrJobNotFound in Go and indistinguishable from it on the wire. The
	// distinction is worth keeping here because a test asserting "the non-owner was refused"
	// should fail if the job silently stopped existing instead — those are the same 404 to a
	// client and very different defects.
	ErrNotJobOwner = errors.New("jobs: that job belongs to another customer")

	// ErrJobNotDraft means an edit arrived for a job that has been published.
	//
	// Docs/01 §4.1 lets a customer "edit or cancel it before award", and Docs/02 §3 adds that
	// core job details cannot change after award without a documented change process. SHIP-62
	// takes the narrower of the two and stops at Draft: an Open job carries bids that were
	// made against the details as they were, and amending it underneath them is SHIP-69's
	// problem rather than a PATCH.
	ErrJobNotDraft = errors.New("jobs: only a draft can be edited")

	// ErrNotCustomer means the account creating the job is not a customer account.
	//
	// 000400 has no CHECK for it — a foreign key cannot see another table's column — and says
	// the rule is enforced where the draft is created. This is that enforcement, and it reads
	// users.role rather than trusting the role claim in the token, because the claim is
	// evidence about the token and the column is the fact.
	ErrNotCustomer = errors.New("jobs: only a customer account can create a job")

	// ErrNothingToUpdate means a PATCH named no field at all.
	ErrNothingToUpdate = errors.New("jobs: the request changes nothing")

	// ErrExpiryWarningNotDue means a warning was attempted on a job that is not an Open job
	// awaiting one (SHIP-69).
	//
	// Unreachable from the sweep, which claims exactly the eligible rows and holds their locks.
	// It exists for the caller that does not exist yet: a path that picks a job some other way
	// should be told the job was not eligible rather than believing a warning was sent, because
	// the mark and the event are what make the warning happen once and a caller that skipped
	// both has silently done nothing.
	ErrExpiryWarningNotDue = errors.New("jobs: that job is not awaiting an expiry warning")

	// ErrJobNotExtendable means the job is in a status whose deadline does not end it.
	//
	// Only an Open job expires — [ExpiryClaim] filters on it, and Docs/02 §2's one "job expires
	// unclaimed" row is `Open → Cancelled`. Extending anything else would move a column nothing
	// reads and tell the customer their job was safe when it was never at risk.
	ErrJobNotExtendable = errors.New("jobs: only an Open job can have its expiry extended")

	// ErrExpiryBoundByPickup means the job is ending because its pickup date is passing, so
	// there is no time an extension could add.
	//
	// Docs/02 §6.3 calls the pickup date the operative rule and the fourteen days a backstop: "a
	// job whose pickup window has gone is dead regardless of how recently it was posted."
	// Extending past it would put a listing in front of providers advertising a collection date
	// that has been and gone, which is worse than letting the job expire.
	//
	// Distinct from ErrJobNotExtendable in Go and one code on the wire, for the reason
	// ErrNotJobOwner and ErrJobNotFound share `not_found`: the client's action is the same —
	// reload and show what is actually available — and only the sentence differs.
	ErrExpiryBoundByPickup = errors.New("jobs: the pickup date is what ends this job, not the listing period")

	// ErrNoBidderLookup means the service was built with no [Bidders] and was asked who may
	// read a job's history (SHIP-65a).
	//
	// A wiring defect rather than a condition a caller can act on, and it is an error rather
	// than a silent "no" for the reason [WithBidders] gives: refusing every provider while
	// looking like a job nobody bid on is the failure that is never noticed. It maps to an
	// opaque 500 through [apiError]'s default branch, which is the correct treatment of a
	// mistake in the composition root.
	ErrNoBidderLookup = errors.New("jobs: this service was built with no bidder lookup")

	// ErrJobNotCancellable means Docs/02 §2 has no `→ Cancelled` row for the status the job
	// is in.
	//
	// Narrower than ErrTransitionNotPermitted on purpose. The general sentinel names a move
	// the caller asked for, and a customer pressing "cancel" did not ask for a move — they
	// asked for an outcome. SHIP-63's publish will want the same treatment and a different
	// code, which is why the mapping is not made from the general sentinel at the transport
	// edge: one code per intent, not one code per guard failure.
	ErrJobNotCancellable = errors.New("jobs: this job can no longer be cancelled")
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
//
// There are deliberately few. Most failures here are already covered by the protocol codes: a
// malformed address is `validation_failed` with details, a job that is not yours is `not_found`,
// and a missing idempotency key is the middleware's business. A domain code earns its place only
// where a client would otherwise have to parse a message to know what to do.
var (
	// CodeCustomerOnly is returned when a provider account tries to create a job.
	//
	// A distinct code rather than a bare 403 because the client can act on it: the app shows
	// the provider surface, and a provider reaching this has followed a link or a deep route
	// meant for the other role (Docs/07 §3 — the app may hide, the platform decides).
	CodeCustomerOnly = httpx.RegisterCode("jobs_customer_only",
		"Only a customer account can create or edit a job. Providers bid on jobs; they do not publish them.")

	// CodeNotADraft is returned when an edit arrives for a job that has left Draft.
	//
	// 409 rather than 403: the caller is permitted, and the request contradicts the state the
	// job is in. The client's correct response is to reload the job and show its real status,
	// which is a different action from asking the user to sign in or giving up.
	CodeNotADraft = httpx.RegisterCode("jobs_not_a_draft",
		"The job has been published and can no longer be edited as a draft. Reload it to see its current status.")

	// CodeNotCancellable is returned when a cancellation arrives for a job Docs/02 §2 has no
	// route out of towards Cancelled.
	//
	// 409 rather than 403, on the same reasoning as jobs_not_a_draft: the caller is permitted
	// and the request contradicts the state the job is in. The client reloads and offers what
	// is actually available — which, once a provider has committed, is raising a dispute
	// rather than cancelling (Docs/02 §6.2).
	CodeNotCancellable = httpx.RegisterCode("jobs_not_cancellable",
		"The job can no longer be cancelled. Once a provider has been awarded the work, ending "+
			"the job is a support matter rather than a state change. Reload it to see its current status.")

	// CodeNotExtendable is returned when an extension cannot add time to a job (SHIP-70).
	//
	// 409 rather than 403, on the same reasoning as the two above: the caller is permitted and
	// the request contradicts the state the job is in. Two conditions share it — the job is not
	// Open, or its pickup date rather than its listing period is what ends it — because the
	// client's response to both is to reload and offer what is actually available. The message
	// says which; the code says what to do.
	CodeNotExtendable = httpx.RegisterCode("jobs_not_extendable",
		"The job's expiry cannot be extended. Either it is not being offered to providers any "+
			"more, or its pickup date is what is ending it — and no amount of extra listing "+
			"time keeps a job alive past the date its goods were to be collected.")
)
