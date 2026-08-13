package admin

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
// [ErrNotAParty] deliberately share `not_found`, because telling a caller which of the two it was
// would confirm that somebody else's job exists.
var (
	// ErrJobNotFound means no job with that identifier exists — or none this caller has any
	// business knowing about. The endpoint answers 404 either way.
	ErrJobNotFound = errors.New("admin: no such job")

	// ErrNotAParty means the job exists and the caller is neither its customer nor the provider
	// who won it.
	//
	// Distinct from ErrJobNotFound in Go and indistinguishable from it on the wire, which is the
	// arrangement `jobs`, `fleet` and `delivery` all use: the distinction is worth keeping so a
	// test asserting "the stranger was refused" fails if the job silently stopped existing
	// instead.
	//
	// **The refusal is explicit.** Wave 5 found six of eight fleet endpoints scoping their query
	// to the caller rather than refusing an outsider, which leaks nothing and answers 200 to
	// somebody who had no business asking. This endpoint asks who the caller is on the job and
	// refuses when the answer is nobody, which is a different thing from writing a row nobody
	// can see.
	ErrNotAParty = errors.New("admin: that job is not this caller's to dispute")

	// ErrJobNotDisputable means Docs/02 §2 has no route from the job's status to 'Disputed'.
	//
	// Narrower than "that transition is not permitted" on purpose, in the same spirit as
	// jobs.ErrJobNotCancellable: somebody pressing "report a problem" asked for an outcome
	// rather than for a named move, and the code they get back should describe the outcome they
	// wanted.
	//
	// Docs/02 §2 permits it from Awarded through Delivered, which is the span during which there
	// is a delivery to dispute. A job nobody has been awarded is cancelled rather than disputed;
	// a job that has completed or been cancelled has left the lifecycle.
	ErrJobNotDisputable = errors.New("admin: this job cannot be disputed in its current status")

	// ErrDisputeAlreadyOpen means the job already has a dispute awaiting an outcome.
	//
	// This domain's reading of uq_disputes_open_per_job, which is partial on resolved_at being
	// NULL. The index is what makes the rule true — two callers racing both find nothing open
	// and both write, and only the index is right about that — and this sentinel is what makes
	// the refusal legible, because a caller handed an index name learns nothing they can act on.
	//
	// It is a different answer from the retry below, and the difference matters to the client: a
	// retry gets the dispute it already raised, and this is told that somebody has already raised
	// one. On a job with two parties, the somebody is frequently the other one.
	ErrDisputeAlreadyOpen = errors.New("admin: this job already has an open dispute")

	// ErrNoIdempotencyKey means an intake arrived with no key to record it against.
	//
	// SHIP-15's middleware refuses a state-changing request without one, so this is unreachable
	// through the served route. It is checked anyway, because uq_disputes_idempotency is
	// *partial*: a row with a NULL key falls outside the index, and the guarantee that a retry
	// raises no second dispute would be absent rather than broken. A guarantee that can be
	// removed by omitting a header is one this domain should refuse to write without.
	ErrNoIdempotencyKey = errors.New("admin: a dispute must carry the key it was raised under")

	// ErrIdempotencyKeyReused means the key has already raised a different dispute on this job.
	//
	// The database's version of the fingerprint check httpx.Idempotent makes in Redis, and it
	// answers with the same code. Replaying the first dispute would tell a client that something
	// it never sent had been raised.
	ErrIdempotencyKeyReused = errors.New("admin: that idempotency key already raised a different dispute")

	// ErrDisputeVanished means the unique index refused a duplicate and no row exists for the key
	// that caused it.
	//
	// Nothing in the platform can produce this: intake has no DELETE path, and the row that
	// caused the conflict is committed by the time the conflict is visible. It is here so that an
	// impossible state becomes a 500 with a cause in the log rather than a reply that invents an
	// answer.
	ErrDisputeVanished = errors.New("admin: a dispute was refused as a duplicate of a row that is not there")

	// ErrNotInTransaction means a service method that writes two tables was handed a connection
	// pool rather than a transaction.
	//
	// Checked here rather than left to the database, even though the status guard would refuse
	// the transition on its own (000402's setting is transaction-local): by then the dispute row
	// would have committed alone, leaving a complaint recorded against a job that never froze.
	ErrNotInTransaction = errors.New("admin: this must run inside a transaction")

	// ErrPartyUnrecognised means the [JobParties] port said the caller is party to the job and
	// then named a side that is not one of [Parties].
	//
	// A wiring failure rather than a request failure, so it becomes an opaque 500.
	// ck_disputes_complainant_party would refuse the row anyway; this refuses it a statement
	// earlier, with a cause a reader can act on rather than a constraint name.
	ErrPartyUnrecognised = errors.New("admin: the party lookup named a side this domain does not recognise")

	// ErrJobMoveUnrecognised means the [Jobs] port answered with a [JobMove] this domain has no
	// case for.
	//
	// A wiring failure rather than a request failure, so it becomes an opaque 500. It exists
	// because the alternative — treating an unrecognised answer as success — would record a
	// dispute on a job whose status nobody moved.
	ErrJobMoveUnrecognised = errors.New("admin: the job lifecycle answered with an outcome this domain does not recognise")
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
//
// There are deliberately two. A caller who is not party to the job gets `not_found`, a malformed
// intake gets `validation_failed` with details, and a key that raised something else gets the
// protocol's own `idempotency_key_reused` — because the database enforces it here (000800) and the
// middleware enforces it in Redis, and a client that had to tell the two apart would be branching
// on where the platform happened to catch it. A domain code earns its place only where a client
// would otherwise parse a message to know what to do, and these two lead to different screens.
var (
	// CodeJobNotDisputable is returned when the job is in no status a dispute can be raised
	// from.
	//
	// 409 rather than 403: the caller is permitted and the request contradicts the state the job
	// is in. The client reloads the job and offers what is actually available — which, for a job
	// still open for bids, is cancelling it, and for a job that completed a month ago is
	// contacting support.
	CodeJobNotDisputable = httpx.RegisterCode("admin_job_not_disputable",
		"A dispute can only be raised on a job that has been awarded and has not yet been "+
			"completed or cancelled. Reload the job to see its current status.")

	// CodeDisputeAlreadyOpen is returned when the job already has a dispute awaiting an outcome.
	//
	// 409 for the same reason, and a distinct code because the client's response is different
	// again: show the dispute that is already open rather than offer the form a second time. On a
	// job with two parties this is the answer the *other* party gets, and "somebody has already
	// raised this" is what they need to be told.
	CodeDisputeAlreadyOpen = httpx.RegisterCode("admin_dispute_already_open",
		"This job already has a dispute waiting on an outcome. Open it rather than raising "+
			"another — one job is disputed once at a time.")
)
