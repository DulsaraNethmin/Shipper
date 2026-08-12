package delivery

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The sentinel errors this domain raises, and the error codes its endpoints answer with.
//
// Docs/10 §2.1 puts the sentinels here rather than beside the code that returns them, so a caller
// deciding what to do about a failure has one file to read.
//
// The codes below are what reaches a client, and they are a separate list on purpose: a sentinel
// says what happened, a code says what the client should do about it. [ErrJobNotFound] and
// [ErrNotAwardedProvider] deliberately share `not_found`, because telling a caller which of the two
// it was would confirm that somebody else's job exists.
var (
	// ErrJobNotFound means no job with that identifier exists — or none this caller has any
	// business knowing about. The endpoint answers 404 either way.
	ErrJobNotFound = errors.New("delivery: no such job")

	// ErrNotAwardedProvider means the job exists and was awarded to another provider.
	//
	// Distinct from ErrJobNotFound in Go and indistinguishable from it on the wire, which is the
	// arrangement `jobs` and `fleet` both use: the distinction is worth keeping so a test
	// asserting "the stranger was refused" fails if the job silently stopped existing instead.
	//
	// This is the answer to the question 000600 left open — "SHIP-106 is the ticket that knows
	// whether an administrator may assign at all". Only the awarded provider may, for now. An
	// administrator has no account this domain could name (ck_users_role refuses 'admin', and
	// admin sign-in is a separate system at SHIP-147), and Docs/02 §1 gives 'Driver assigned' one
	// primary actor: the provider.
	ErrNotAwardedProvider = errors.New("delivery: that job was awarded to another provider")

	// ErrJobNotAssignable means Docs/02 §2 has no route from the job's status to
	// 'Driver assigned'.
	//
	// Narrower than "that transition is not permitted" on purpose, in the same spirit as
	// jobs.ErrJobNotCancellable: a provider pressing "assign" asked for an outcome rather than
	// for a named move, and the code they get back should describe the outcome they wanted.
	ErrJobNotAssignable = errors.New("delivery: this job cannot take a driver in its current status")

	// ErrDriverAlreadyAssigned means the job already has a live assignment naming somebody else.
	//
	// It is this domain's reading of uq_driver_assignments_active, which is partial on
	// unassigned_at being NULL. The index is what makes the rule true — two requests racing both
	// find nothing and both write, and only the index is right about that — and this sentinel is
	// what makes the refusal legible, because a caller handed a constraint name learns nothing
	// they can act on.
	//
	// **Replacing a driver is deliberately not this endpoint.** 000600 describes the shape it
	// would take (end this assignment, insert another) and no ticket owns it yet; SHIP-109
	// reissues a *link* to the same driver, which is a different intent. A repeat naming the
	// same driver is absorbed rather than refused — see [Service.AssignDriver].
	ErrDriverAlreadyAssigned = errors.New("delivery: this job already has a driver")

	// ErrNotInTransaction means AssignDriver was handed a connection pool rather than a
	// transaction.
	//
	// Checked here rather than left to the database, even though the status guard would refuse
	// the transition on its own (000402's setting is transaction-local): by then the assignment
	// row would have committed alone, leaving a driver on a job that never moved.
	ErrNotInTransaction = errors.New("delivery: an assignment must run inside a transaction")

	// ErrJobMoveUnrecognised means the [Jobs] port answered with a [JobMove] this domain has no
	// case for.
	//
	// A wiring failure rather than a request failure, so it becomes an opaque 500. It exists
	// because the alternative — treating an unrecognised answer as success — would leave an
	// assignment on a job whose status nobody moved.
	ErrJobMoveUnrecognised = errors.New("delivery: the job lifecycle answered with an outcome this domain does not recognise")
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
//
// There are deliberately two. A malformed mobile number is `validation_failed` with details, a job
// that is not the caller's is `not_found`, and a missing idempotency key is the middleware's
// business. A domain code earns its place only where a client would otherwise have to parse a
// message to know what to do — and these two lead to two different screens.
var (
	// CodeJobNotAssignable is returned when the job is in no status a driver can be assigned
	// from.
	//
	// 409 rather than 403: the caller is permitted and the request contradicts the state the job
	// is in. The client reloads the job and offers what is actually available — which, for a job
	// already on its way to the pickup, is recording the next milestone rather than assigning
	// anybody.
	CodeJobNotAssignable = httpx.RegisterCode("delivery_job_not_assignable",
		"A driver can only be assigned to a job that has been awarded and has not yet set off. "+
			"Reload the job to see its current status.")

	// CodeDriverAlreadyAssigned is returned when the job already has a different live driver.
	//
	// 409 for the same reason, and a distinct code because the client's response is different
	// again: show the provider the driver already on the job. A generic conflict would leave the
	// app guessing which of the two states it is in.
	CodeDriverAlreadyAssigned = httpx.RegisterCode("delivery_driver_already_assigned",
		"This job already has a driver. Reload it to see who is carrying it.")
)
