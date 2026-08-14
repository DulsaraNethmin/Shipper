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

	// ErrProofRequired means a delivery was recorded with nothing to show for it (SHIP-118).
	//
	// CLAUDE.md states the invariant and Docs/01 §4.4 decides it: "a job cannot be recorded as
	// Delivered without photo proof, except through the exception path". This is the refusal that
	// makes it true, and **it is now a condition rather than a blanket**: 'Delivered' carrying a
	// photograph (SHIP-115) or a reasoned exception (SHIP-116) is recorded, and 'Delivered'
	// carrying neither is refused here before anything is written.
	//
	// It is one of two layers. 000605's deferred constraint trigger refuses the same row at
	// commit, whoever wrote it and through whatever path — which is what makes the invariant a
	// property of the platform rather than of one function. What this layer buys is the answer a
	// client can act on; see [CodeProofRequired].
	ErrProofRequired = errors.New("delivery: a delivery needs photo proof or a recorded exception")

	// ErrEvidenceNotCoherent means a [Recording] reached the insert carrying evidence that is not
	// one photograph or one reasoned exception (SHIP-116).
	//
	// Both shapes it refuses — a recording carrying both, and one naming a reason that is not one
	// of Docs/01 §4.4's three — are caught by [Recording.problems] and answered as
	// `validation_failed` with `proof.exception_reason` named, so **this is unreachable through
	// any endpoint**. It exists for the same reason [ErrProofNotVerified] does: the guard against a
	// future caller *inside* this package assembling a recording by hand, where the next line of
	// defence is ck_proofs_photograph_or_exception and a constraint name explains nothing
	// (Docs/10 §4.6).
	//
	// It maps to no code and becomes an opaque 500 with its cause logged, which is the right
	// treatment: the client did nothing wrong and there is nothing it could usefully be told.
	ErrEvidenceNotCoherent = errors.New("delivery: a milestone's evidence must be one photograph or one reasoned exception")

	// ErrProofNotUploaded means the object the client named is not in the store (SHIP-115).
	//
	// # It is an ordinary outcome, not an attack, and the code says so
	//
	// The bytes go from the client straight to the object store and the platform is not in the
	// path (Docs/06 §5.2), so the request that records proof is the *first* thing this service
	// hears about an upload. A driver in a yard with one bar can perfectly well be issued a URL,
	// fail the PUT, and record the milestone anyway — Docs/01 §4.4 spends a section on not
	// stranding exactly that person. So this refuses the recording and tells them to send the
	// photograph again.
	//
	// **The alternative was to believe them**, and it is worth naming because it is the cheaper
	// design and it is wrong: a `proofs` row nobody checked is the platform asserting that a
	// photograph exists when it has never looked, and Docs/01 §4.4 makes proof "the *only*
	// evidence that the job happened as claimed".
	ErrProofNotUploaded = errors.New("delivery: there is no such object in the store")

	// ErrProofRejected means the object is there and is not something the platform accepts
	// (SHIP-115).
	//
	// # This is where [UploadPolicy] stops being advice, inside the domain
	//
	// SHIP-114 signs the content type and the length into the upload URL, so the store refuses a
	// substitute on the request that carries the bytes. That is the strong guard and it is
	// **outside this package** — a domain test cannot fail on it, because the domain's tests stub
	// the signer, which SHIP-114's mutation testing recorded as an open hole.
	//
	// This closes it from the other end. What the store reports is checked against the same
	// policy before anything is recorded, so an object that reached the bucket by any route — a
	// signer that stopped binding the headers, a key uploaded to before the rules tightened — is
	// refused the moment somebody tries to make it evidence. The limits are then enforced against
	// the object that exists rather than only against the request that asked to create one.
	ErrProofRejected = errors.New("delivery: the stored object is not something this platform accepts as proof")

	// ErrProofAlreadyRecorded means that object is already proof of something (SHIP-115).
	//
	// uq_proofs_object_key is what answers it, in the database rather than here, because two
	// concurrent recordings would both read no row and both insert. One photograph is evidence for
	// one recorded claim: the same object on two milestones would put a picture of one delivery on
	// another job's timeline, and object keys are unguessable but not unshareable.
	ErrProofAlreadyRecorded = errors.New("delivery: that object is already proof of another milestone")

	// ErrProofNotForThisJob means the object key names a different delivery (SHIP-115).
	//
	// The platform chooses every key and prefixes it with the job it issued the URL for, so this
	// is a client attaching one job's photograph to another. Refused on the string alone, before
	// any lookup, which is what keeps it from being an oracle: no row is read and no object is
	// asked about, so the answer says nothing except that the caller's own body disagrees with
	// the caller's own path.
	ErrProofNotForThisJob = errors.New("delivery: that object key was issued for another job")

	// ErrProofNotVerified means a [Recording] carries proof that no [Service.VerifyProof]
	// produced.
	//
	// Unreachable through any endpoint — [VerifiedProof]'s fields are unexported, so the only
	// value another package can build is the zero one, which means "no proof" — and reported
	// rather than assumed away, because the shape it guards against is a future caller inside
	// this package assembling one by hand and skipping the store.
	ErrProofNotVerified = errors.New("delivery: proof was recorded without being checked against the store")

	// ErrMilestoneNotPermitted means the delivery has not reached the point this milestone
	// describes — an `in_transit` recorded while the job is still on its way to the pickup.
	//
	// **SHIP-112 halved what this covers, and the half it kept is the recoverable one.** Until
	// then it answered every refusal of Docs/02 §2's table, in both directions. A milestone the
	// job has already passed is now absorbed — the row kept, the job left alone, which is what
	// Docs/02 §3.1 required all along — and this is what is left: a milestone that arrived too
	// early. The distinction is worth the sentinel because the two lead somewhere different. A
	// late milestone can never succeed on a retry, since Docs/02 §2 has no way back, so refusing
	// it would discard the driver's record; a premature one succeeds unchanged as soon as the
	// delivery reaches that point, so refusing it costs a retry.
	//
	// A job cancelled or disputed before it ever reached the milestone is refused here too, and
	// **that is SHIP-113's case** rather than this one's: "a queued update that contradicts an
	// administrative action loses… the attempt is retained in history". Retaining it is that
	// ticket's change.
	ErrMilestoneNotPermitted = errors.New("delivery: this milestone cannot be recorded from the job's current status")

	// ErrMilestoneVanished means the unique index refused a duplicate and no row exists for the
	// key that caused it.
	//
	// Nothing in the platform can produce this: `milestones` is append-only, has no DELETE path,
	// and the row that caused the conflict is committed by the time the conflict is visible. It
	// is here so that an impossible state becomes a 500 with a cause in the log rather than a
	// reply that invents an answer.
	ErrMilestoneVanished = errors.New("delivery: a milestone was refused as a duplicate of a row that is not there")

	// ErrUnknownRecorder means a milestone was being written for an actor this domain cannot
	// attribute it to (SHIP-120a).
	//
	// A defect here rather than anything a caller did: every entry point builds the [Recorder]
	// itself, from a credential the guard has already verified, so a zero or unmapped one means a
	// path was added without deciding whose row `actor_id` names. It is refused in front of the
	// insert because the alternative is ck_milestones_actor_type answering with a constraint name
	// and a 500, three frames further in and after a row has been attempted.
	ErrUnknownRecorder = errors.New("delivery: a milestone was recorded for an actor this domain cannot attribute")

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

	// The four ways a driver's link fails to open a delivery (SHIP-108).
	//
	// They are four sentinels rather than one because the first three lead the driver somewhere
	// different — wait, ask for a new link, or check they opened the right message — and because
	// the fourth is not an authentication failure at all. What reaches the wire is deliberately
	// narrower than what is distinguished here; see [refuseDriverLink].

	// ErrNoDriverToken means a driver-token route was reached with no bearer credential.
	//
	// Distinct from [ErrDriverTokenRejected] so that "you have not opened the link" and "the link
	// you opened is not good" are not one answer. It is the same split httpx makes between
	// `authNone` and `authRejected`, and for the same reason: a driver who pasted a URL without
	// its token should be told to open the message they were sent.
	ErrNoDriverToken = errors.New("delivery: no driver token was presented")

	// ErrDriverTokenExpired means the link was genuine and its window has closed.
	//
	// The one refusal a driver can act on, and it gets a code of its own for that reason
	// ([CodeDriverLinkExpired]). **It is not httpx.CodeTokenExpired**, which tells a client to
	// refresh and retry: a driver holds no second credential and has nothing to refresh with, so
	// the mobile code's advice would send the portal into a loop it cannot leave (Docs/10 §5).
	ErrDriverTokenExpired = errors.New("delivery: the driver token has expired")

	// ErrDriverTokenRejected means the token did not verify, and the reason is deliberately not
	// carried any further.
	//
	// A bad signature, an unknown key identifier, `alg: none`, a mobile session token presented
	// here — every one of them arrives as this. Which check refused a credential is free help to
	// somebody probing and there is nothing a legitimate driver could do differently, which is the
	// call httpx.ResolveSubject already makes with `authRejected`.
	ErrDriverTokenRejected = errors.New("delivery: the driver token was not accepted")

	// ErrDriverTokenWrongJob means a perfectly valid link was presented on a job it does not grant.
	//
	// **This is the sentinel the *Done when* rests on** — "grants access to exactly one job and
	// nothing else" — and it is not an authentication failure: the credential is genuine and the
	// holder is who they say they are. It answers 404 rather than 403 for the reason [apiError]
	// gives about another provider's job: a 403 would confirm that the other job exists.
	ErrDriverTokenWrongJob = errors.New("delivery: the driver token does not grant this job")

	// ErrDriverLinkSuperseded means the token names an assignment that is no longer the live one on
	// its job.
	//
	// A driver stood down keeps their link — the token is stateless and cannot be recalled — so the
	// row is what says the grant has lapsed. That is the lookup [Service.Driver] was written for,
	// and it is what makes SHIP-109's revocation a read rather than a denylist.
	//
	// Nothing writes `unassigned_at` today, so this is unreachable through any endpoint. It is
	// checked anyway, because the alternative is a link that keeps working after the assignment
	// behind it has ended, which is the whole of what SHIP-109 will be asked to prevent.
	ErrDriverLinkSuperseded = errors.New("delivery: the assignment this driver token names is no longer live")
)

// The error codes this domain's endpoints answer with (Docs/10 §4.4).
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and cmd/api's uniqueness test can see them. Named <domain>_<condition>, which is
// what stops two domains meaning different things by one string.
//
// The list is short for a reason. A malformed mobile number is `validation_failed` with details, a
// job that is not the caller's is `not_found`, and a repeated idempotency key is the protocol's own
// `idempotency_key_reused` rather than a delivery code — the database enforces it here (000602) and
// the middleware enforces it in Redis, and a client that had to tell the two apart would be
// branching on where the platform happened to catch it. A domain code earns its place only where a
// client would otherwise parse a message to know what to do, and each of these leads to a different
// screen.
//
// **The fifth arrived with SHIP-108 and is the only one on an authentication failure.** Every other
// refusal a driver's link can meet is `unauthenticated` or `not_found`, which is deliberate: what
// the driver portal must do about a bad link is the same whatever was wrong with it, except when it
// has simply run out.
//
// **SHIP-115 added three, which is a lot for one ticket and each earns it on the same test.** The
// photograph is not there yet, the photograph is there and is not acceptable, and the photograph is
// already evidence for something else: retry the upload, capture again, ask for a new URL. Three
// different things for the app to do, and a driver standing at a delivery point is exactly the
// person who must not be shown one generic conflict and left to guess (Docs/01 §4.4).
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

	// CodeMilestoneNotPermitted is returned when the delivery has not reached the point the
	// milestone describes.
	//
	// 409 rather than 422: the value is a perfectly good milestone and the request contradicts
	// the state the job is in. It is a distinct code from delivery_job_not_assignable because the
	// screens differ — the app reloads the delivery and shows what it can record *now*, which for
	// a job still on its way to the pickup is not the same list.
	//
	// **Since SHIP-112 this no longer means "too late".** A milestone the job has already moved
	// past is absorbed and answers `201` (Docs/02 §3.1), so what is left here is the opposite
	// direction — too early — and it is worth retrying, unchanged, once the delivery gets there.
	// A client is still right to hold the update rather than discard it.
	CodeMilestoneNotPermitted = httpx.RegisterCode("delivery_milestone_not_permitted",
		"This milestone cannot be recorded from the job's current status. Reload the delivery to "+
			"see where it is, and keep the update — the delivery has not reached this point yet, "+
			"and one it has already passed is recorded rather than refused.")

	// CodeProofRequired is returned when a delivery is recorded with no proof and no exception.
	//
	// A code of its own because it leads somewhere specific: the camera, or the exception path
	// beside it (Docs/01 §4.4). What a client must never do with it is offer "try again", which
	// is what a generic conflict would suggest — the identical request will be refused for as long
	// as it carries nothing, and the way out is to capture something or to say why there is
	// nothing to capture.
	CodeProofRequired = httpx.RegisterCode("delivery_proof_required",
		"A delivery is recorded with photo proof, or with a reason why there is none. Send the "+
			"object_key of a photograph you have uploaded, or one of the exception reasons, in "+
			"this request's proof field.")

	// CodeDriverLinkExpired is returned when a driver's job-scoped link has run out (SHIP-108).
	//
	// 401, and **not** `token_expired`. That code exists for the mobile session and its whole
	// meaning is "refresh and retry, do not sign the user out" — advice a driver portal cannot
	// take, because there is no refresh behind a driver's link and no account to sign back into
	// (Docs/10 §5). A portal that branched on `token_expired` would loop; this tells it to say
	// so and stop.
	//
	// It is the only refusal in this domain that names what was wrong with a credential. Every
	// other one is `unauthenticated`, deliberately undifferentiated — see [ErrDriverTokenRejected].
	CodeDriverLinkExpired = httpx.RegisterCode("delivery_driver_link_expired",
		"This delivery link has expired. There is nothing to refresh — ask the transport provider "+
			"to send a new one.")

	// CodeProofNotUploaded is returned when the photograph never reached the store (SHIP-115).
	//
	// 409, and a code of its own because it is the one refusal on this path a client can fix
	// without a person: retry the PUT to the URL it already holds, or ask for a new one and PUT
	// again. Every other conflict here tells the driver to do something different; this tells the
	// app to finish what it started.
	//
	// It exists at all because the platform is not in the upload path (Docs/06 §5.2) and
	// therefore cannot know an upload happened until it asks. See [ErrProofNotUploaded].
	CodeProofNotUploaded = httpx.RegisterCode("delivery_proof_not_uploaded",
		"That photograph is not in the store yet. Finish uploading it to the URL you were given, "+
			"then record the milestone again.")

	// CodeProofRejected is returned when the stored object is not something the platform accepts
	// as proof (SHIP-115).
	//
	// 409 rather than 422, and the difference is which thing was wrong: the request body is
	// perfectly well formed and names an object that exists — what fails the platform's rules is
	// the object. A client cannot fix it by editing the request, so it is not a validation error;
	// it captures again.
	CodeProofRejected = httpx.RegisterCode("delivery_proof_rejected",
		"That file is not a photograph this platform accepts. Capture it again, and compress it "+
			"if it is large.")

	// CodeProofAlreadyRecorded is returned when the object is already proof of another milestone
	// (SHIP-115).
	//
	// 409, and distinct from the two above because the app must not retry with the same key: it
	// asks for a fresh upload URL, which is a fresh object, which is what SHIP-114 guarantees
	// every request gets.
	CodeProofAlreadyRecorded = httpx.RegisterCode("delivery_proof_already_recorded",
		"That photograph is already the proof for another milestone. Ask for a new upload URL and "+
			"send it again.")
)
