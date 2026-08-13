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

	// ErrNotInTransaction means a service method that writes two tables was handed a connection
	// pool rather than a transaction.
	//
	// Checked here rather than left to the database, even though the status guard would refuse
	// the transition on its own (000402's setting is transaction-local): by then the first row
	// would have committed alone, leaving a driver on a job that never moved, or a milestone on a
	// delivery whose status disagrees with it.
	ErrNotInTransaction = errors.New("delivery: this must run inside a transaction")

	// ErrNoIdempotencyKey means a milestone was offered with no key to record it against.
	//
	// SHIP-15's middleware refuses a state-changing request without one, so this is unreachable
	// through the served route. It is checked anyway, because uq_milestones_idempotency is
	// *partial*: a row with a NULL key falls outside the index, and the one thing SHIP-111
	// promises — once per key — would be absent rather than broken. A guarantee that can be
	// removed by omitting a header is one this domain should refuse to write without.
	ErrNoIdempotencyKey = errors.New("delivery: a milestone must carry the key it was recorded under")

	// ErrIdempotencyKeyReused means the key has already recorded a different milestone on this
	// job.
	//
	// The database's version of the fingerprint check httpx.Idempotent makes in Redis, and it
	// answers with the same code. Replaying the first milestone would tell a client that
	// something it never sent had been recorded; recording the second under the same key would
	// make "once per key" false.
	ErrIdempotencyKeyReused = errors.New("delivery: that idempotency key already recorded a different milestone")

	// ErrProofRequired means a delivery was recorded with nothing to show for it.
	//
	// CLAUDE.md states the invariant and Docs/01 §4.4 decides it: "a job cannot be recorded as
	// Delivered without photo proof, except through the exception path". Neither the proof
	// (SHIP-114, SHIP-115) nor the exception (SHIP-116) can be captured yet, so **every**
	// 'Delivered' is refused here for now — which is the only answer that does not leave a job at
	// Delivered with neither.
	//
	// **SHIP-118 is the ticket that narrows this**, from "always" to "when the job has neither
	// proof nor a recorded exception". The sentinel, the code and the message already say what
	// that ticket needs to say; what changes is the condition in front of them.
	ErrProofRequired = errors.New("delivery: a delivery needs photo proof or a recorded exception")

	// ErrMilestoneNotPermitted means Docs/02 §2 has no transition from where the job stands to
	// where this milestone would put it.
	//
	// **This is the interim answer to a case Docs/02 §3.1 says must not be an error.** A queued
	// update that arrives after a later one "must be absorbed, not rejected" — the row kept, the
	// job left alone — and that absorption is SHIP-112, a five-point ticket of its own. Until it
	// lands the milestone rolls back with its transaction and the client is told the job has
	// moved on, which at least does not lose the driver's work silently: the device still holds
	// it and shows it as pending (Docs/02 §3.1).
	ErrMilestoneNotPermitted = errors.New("delivery: this milestone cannot be recorded from the job's current status")

	// ErrMilestoneVanished means the unique index refused a duplicate and no row exists for the
	// key that caused it.
	//
	// Nothing in the platform can produce this: `milestones` is append-only, has no DELETE path,
	// and the row that caused the conflict is committed by the time the conflict is visible. It
	// is here so that an impossible state becomes a 500 with a cause in the log rather than a
	// reply that invents an answer.
	ErrMilestoneVanished = errors.New("delivery: a milestone was refused as a duplicate of a row that is not there")

	// ErrInvalidKeyset means the driver token signing material cannot be used to sign anything
	// (SHIP-107).
	//
	// A configuration failure rather than a request failure. internal/config catches every form
	// of it at startup — a short key, an active identifier naming no key, a set that shares a
	// secret with identity's — so reaching this means [NewKeyset] was called with something
	// configuration never produced.
	ErrInvalidKeyset = errors.New("delivery: the driver token keyset cannot sign")

	// ErrNoSigningKey means a token names a key identifier the keyset does not hold.
	//
	// The ordinary cause is a key rotated out of the set while tokens signed with it were still
	// live, which is why a rotation leaves the outgoing key in place until the last token it
	// signed has expired — unlike a mobile client, a driver holds a link with nothing behind it
	// that can refresh.
	ErrNoSigningKey = errors.New("delivery: no signing key with that identifier")

	// ErrMalformedDriverToken means a token verified cryptographically and then said something
	// that is not an identifier.
	//
	// Distinct from a signature failure on purpose: this is a token *this service signed* whose
	// claims cannot be acted on, which is a defect here rather than an attack. It exists so that
	// a `job_id` of "" cannot be read as uuid.Nil and become a grant over the nil job.
	ErrMalformedDriverToken = errors.New("delivery: the driver token names something that is not an identifier")

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
// There are deliberately four, and the list is short for a reason. A malformed mobile number is
// `validation_failed` with details, a job that is not the caller's is `not_found`, and a repeated
// idempotency key is the protocol's own `idempotency_key_reused` rather than a delivery code — the
// database enforces it here (000602) and the middleware enforces it in Redis, and a client that had
// to tell the two apart would be branching on where the platform happened to catch it. A domain code
// earns its place only where a client would otherwise parse a message to know what to do, and each
// of these four leads to a different screen.
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

	// CodeMilestoneNotPermitted is returned when the job has moved past the milestone being
	// recorded, or has not reached the point where it makes sense.
	//
	// 409 rather than 422: the value is a perfectly good milestone and the request contradicts
	// the state the job is in. It is a distinct code from delivery_job_not_assignable because the
	// screens differ — the app reloads the delivery and shows what it can record *now*, which for
	// a job already 'In transit' is not the same list.
	//
	// **The client that gets this must keep the update rather than discard it.** Docs/02 §3.1 has
	// the platform absorbing it instead of refusing, and SHIP-112 is what makes that true; a
	// client that deletes the driver's work on a 409 will lose real records on the day this
	// answer stops being sent.
	CodeMilestoneNotPermitted = httpx.RegisterCode("delivery_milestone_not_permitted",
		"This milestone cannot be recorded from the job's current status. Reload the delivery to "+
			"see what it is, and keep the update — a late one will be absorbed rather than refused "+
			"once SHIP-112 lands.")

	// CodeProofRequired is returned when a delivery is recorded with no proof and no exception.
	//
	// A code of its own because it leads somewhere specific: the camera, or the exception path
	// beside it (Docs/01 §4.4). What a client must never do with it is offer "try again", which
	// is what a generic conflict would suggest.
	CodeProofRequired = httpx.RegisterCode("delivery_proof_required",
		"A delivery is recorded with photo proof, or with a reason why there is none. Capturing "+
			"either is not built yet, so 'delivered' cannot be recorded through this endpoint.")
)
