package delivery

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// What this domain needs of other domains, declared by the consumer (Docs/06 §4.1, Docs/10 §2.3).
//
// internal/delivery imports neither internal/jobs nor internal/bidding, and the boundary lint
// refuses both. cmd/api holds the implementations and is the only place the three packages meet.
//
// # Why an assignment needs anything from elsewhere at all
//
// Two facts decide whether a driver may be put on a job, and this domain owns neither of them:
//
//   - who won the work. Docs/02 §3 allows delivery updates only from "the awarded provider, their
//     assigned driver, or an administrator acting with an audit reason", and the awarded provider
//     is the provider on the job's accepted bid — a `bidding` fact (SHIP-80).
//   - whether the job may move to 'Driver assigned' at all. That is the transition table of
//     Docs/02 §2, and every status change in the platform passes one guarded function in `jobs`
//     (SHIP-57). This domain must not have a second opinion about it, and does not.
//
// # Both signatures speak neutral types, and that is forced rather than stylistic
//
// A port may not name a type declared in the package that implements it — that would be the
// import the lint refuses — so `jobs.Status`, `jobs.Move` and a `bidding` bid cannot appear
// below. jobs.Geocoder met the same constraint and answered it with a five-value signature and a
// comma-ok in place of a sentinel error (Docs/11 §9).
//
// The difference here is that a transition has more than two outcomes worth telling apart, so the
// answer is [JobMove] — an enumeration this package declares and the adapter in cmd/api fills in.
// A bool could not carry it, and `error` could not either: errors.Is against jobs' sentinels is an
// import by another name.
//
// # Both take a db.Runner, because the assignment is one transaction
//
// Docs/10 §3.2: a port that must participate in the caller's transaction takes a Runner. The
// assignment row and the status change commit together or not at all — a job at 'Driver assigned'
// with no driver, or a driver on a job that never moved, are both states nothing downstream knows
// how to read.

// Awards is who won the job.
//
// One method, and it answers the only question this domain has about bidding: which provider the
// customer awarded the work to. Not the amount, not the losing bids, not whether the job carries a
// budget — a port is what a domain needs rather than what the other domain has.
//
// awarded is false when nobody holds the job, and **also when there is no such job**. The two are
// deliberately one answer: a provider who could tell "that job exists and is not yours" from "no
// such job" would learn that somebody else's job exists, which is the same disclosure the 404 on
// a stranger's vehicle exists to prevent. A caller that needs the distinction does not exist.
type Awards interface {
	AwardedProvider(ctx context.Context, r db.Runner, jobID uuid.UUID) (providerID uuid.UUID, awarded bool, err error)
}

// ProofUploads is somewhere to put a proof photograph, declared by the domain that needs one
// (SHIP-114).
//
// # It signs a URL and does nothing else
//
// Docs/06 §5.2 puts the bytes outside this service entirely: the client is handed a short-lived
// pre-signed URL and uploads straight to the store. So this port has no Put, no Get and no Delete —
// there is no method here that moves a byte, because no byte ever reaches the platform. What the
// implementation is asked for is permission, in the form of a URL, and permission is all it can
// give.
//
// # The wide signature is forced, exactly as jobs.Geocoder's was
//
// A port may not name a type declared in the package that implements it — that is the import the
// lint refuses — so no struct from internal/platform/storage can appear below, in either direction.
// The answer is the one Docs/11 §9 records for geocoding: primitives in, primitives out, with the
// domain's own [Upload] assembled from them one statement later.
//
// # What the four arguments are, and why the domain supplies every one of them
//
// The key, because which object a job's proof goes to is this domain's question and not the
// store's — nothing in internal/platform/storage knows what a job is (its doc.go says so). The
// content type and the length, because they are what the implementation must *sign*: an upload URL
// that does not bind them authorises any body at all, and the platform's limits become a promise
// the client made to itself. And the lifetime, because "short-lived" is the whole of the
// authorisation — nothing can revoke a pre-signed URL once it is signed — and the number comes from
// configuration rather than from the signer.
//
// An implementation may refuse: a key that names another object, a content type carrying a newline,
// a length of zero. Those are failures of the mechanism rather than answers, so they come back as
// errors and become an opaque 500 — the domain has already checked everything a client could get
// wrong, so reaching one means this platform asked for something it should not have.
type ProofUploads interface {
	// PresignUpload returns a URL the client may PUT exactly one object to, and when it stops
	// working.
	//
	// The expiry is returned rather than computed by the caller, so that what a client is told
	// and what the store will enforce come from one clock and one arithmetic.
	PresignUpload(
		ctx context.Context,
		key, contentType string,
		contentLength int64,
		ttl time.Duration,
	) (uploadURL string, expiresAt time.Time, err error)
}

// ProofObjects is what the store already holds, and permission to read one back (SHIP-115).
//
// # A second port rather than two more methods on [ProofUploads], and the split is the question
//
// [ProofUploads] answers "where may this client put a photograph". This answers "what is actually
// there, and may this reader see it" — a different question, asked at a different moment, by a
// different caller. Both are satisfied by the same adapter and cmd/api joins them in one place, so
// the split costs a line there and buys the property that a domain reading proof cannot mint
// permission to write it.
//
// # Why [ProofObjects.Stored] has to exist at all
//
// This is the whole of SHIP-115's difficulty and it is worth stating in the interface rather than
// in a commit message. SHIP-114 hands out a URL and the client PUTs the bytes **to the store**, so
// the platform is not in the path and never observes the upload: it cannot tell an upload that
// succeeded from one that failed halfway from one that was never attempted. So a proof record
// written from the client's say-so would be a row asserting that a photograph exists, held by a
// platform that has never checked — and Docs/01 §4.4 makes proof "the *only* evidence that the job
// happened as claimed".
//
// Asking the store closes it, and it also closes something else: what comes back is what was
// *stored*, so [UploadPolicy]'s limits can be applied to the object that exists rather than only
// to the request that asked to create one. See [Service.VerifyProof].
//
// # The five-value return is forced, exactly as [ProofUploads]'s width was
//
// A port may not name a type declared in the package that implements it, so no struct from
// internal/platform/storage can appear below. Primitives in, primitives out, with a comma-ok for
// "no such object" — the same answer Docs/11 §9 records for jobs.Geocoder, and the same one this
// domain's [Awards] gives.
type ProofObjects interface {
	// Stored is the media type, size and entity tag the store holds under key, or found=false
	// when there is no such object.
	//
	// A missing object is an answer rather than an error: it is what a client that has not
	// uploaded yet looks like, which for a driver on a poor connection is an ordinary morning.
	// Anything that is not an answer — a store refusing the credentials, a connection that never
	// opens — comes back as an error and becomes an opaque 500.
	Stored(ctx context.Context, key string) (
		contentType string, contentLength int64, etag string, found bool, err error)

	// PresignDownload returns a URL the caller may GET the object at, and when it stops working.
	//
	// **It authorises nothing by itself and checks nobody.** The implementation will sign a URL
	// for any key it is handed; deciding whether this reader may have it is [Service.ProofFor]'s,
	// from the database, and it happens before this is called. That division is
	// internal/platform/storage/doc.go's own rule, and the reason a download signer could not be
	// written until a domain existed to guard it.
	PresignDownload(ctx context.Context, key string, ttl time.Duration) (
		downloadURL string, expiresAt time.Time, err error)
}

// JobOwners is whose job it is (SHIP-115).
//
// The counterpart of [Awards], and the two together are the whole of this domain's access control
// for reading proof: `bidding` knows who won the work and `jobs` knows who published it.
//
// # It asks rather than fetches, and that is the port being a port
//
// The shape a first draft reaches for is `CustomerOf(jobID) (uuid.UUID, error)` — hand back the
// identifier and let the caller compare. This domain has no use for the customer's identifier: it
// never displays one, never writes one and never passes one on. What it needs is a yes or a no
// about the caller in front of it, so that is what the port says, and the identifier of a customer
// who is not the caller never crosses the boundary at all.
//
// A job that does not exist and a job belonging to somebody else are one answer, for the reason
// [Awards] gives at greater length: a reader who could tell them apart would learn that a
// stranger's job exists.
type JobOwners interface {
	IsCustomer(ctx context.Context, r db.Runner, jobID, userID uuid.UUID) (bool, error)
}

// JobMove is what the guarded transition did, in terms this domain can act on.
//
// The four values are the four outcomes of Docs/02 §2's table as seen from one caller: it moved,
// there is nothing to move, it is already there, or the document has no such row. They are
// deliberately not an error type — an error would have to be one of `jobs`' sentinels to be
// matched, and matching it would be an import.
type JobMove int

const (
	// JobMoveUnrecognised is the zero value and is never a valid answer.
	//
	// First on purpose. An implementation that returns nothing useful — a stub, a half-written
	// adapter, a switch with a missing case — returns this, and [Service.AssignDriver] refuses
	// it rather than reading silence as success. The alternative ordering would make a
	// forgotten return look exactly like a job that moved.
	JobMoveUnrecognised JobMove = iota

	// JobMoved means the job is now 'Driver assigned' and the transition was recorded.
	JobMoved

	// JobNotFound means there is no such job.
	JobNotFound

	// JobAlreadyInStatus means the job was already where the move would have put it.
	//
	// Distinct from JobNotAssignable because the two want opposite handling, exactly as
	// jobs.ErrAlreadyInStatus is distinct from jobs.ErrTransitionNotPermitted: a job that is
	// already where the caller wanted it has not refused anything.
	//
	// **SHIP-106 called this JobAlreadyDriverAssigned, and SHIP-111 renamed it.** With one move
	// on the port the specific name was the clearer one; with four it would be actively wrong,
	// because a driver re-recording 'En route to pickup' on a job that is already there — which
	// Docs/02 §5 lists as an ordinary outcome, the failed pickup attempt — is this outcome and
	// has nothing to do with an assignment. One name for one concept, and the compiler found
	// every use.
	JobAlreadyInStatus

	// JobNotAssignable means Docs/02 §2 has no row from the job's status to the one this move
	// would take it to, **and the job has never been in that status**.
	//
	// The ordinary case is a job that has not been awarded yet, or a milestone recorded for a
	// point in the delivery the job has not reached — an `in_transit` on a job still at
	// 'En route to pickup'.
	//
	// **SHIP-112 narrowed this**, and the second clause is the narrowing. Until then it covered
	// every refusal of the transition table in both directions; [JobAlreadyPast] is now the half
	// of it that points backwards.
	JobNotAssignable

	// JobAlreadyPast means the job has been in the status this move would take it to and has
	// since moved on (SHIP-112).
	//
	// # It is a refinement of JobNotAssignable, and the two want opposite handling
	//
	// Both mean the guard refused, and Docs/02 §3.1 treats them as different events. A milestone
	// whose status the job has already passed is **late**: it is a true statement about work that
	// was done, and Docs/02 §2 offers no way back, so retrying it can never succeed and refusing
	// it discards the driver's record permanently. A milestone the job has not reached yet is
	// **premature**: the identical request succeeds once the delivery gets there, so a refusal
	// costs a retry rather than a record.
	//
	// # "Already been there" is a recorded fact, not an inference
	//
	// The test is whether `job_status_history` holds a transition **into** the target status, which
	// is Docs/02 §3.1's own phrasing — "a queued update that arrives after a later transition has
	// already been recorded". It is deliberately not a reachability search over Docs/02 §2's table:
	// a job cancelled or disputed before it ever reached the status has not passed the milestone,
	// it has lost to something else, and that is SHIP-113's "queued update contradicting an
	// administrative action" rather than this.
	//
	// # An implementation that never returns this is not a compile error, and there is no
	// mechanism that could make it one
	//
	// The value degrades to a refusal, which is what the platform did before SHIP-112. Both
	// adapters — cmd/api/routes_delivery.go and the copy in this package's tests — are held to it
	// by tests that record a late milestone against a real database, and by
	// scripts/verify/70-delivery.sh against the running binary.
	JobAlreadyPast
)

func (m JobMove) String() string {
	switch m {
	case JobMoved:
		return "moved"
	case JobNotFound:
		return "no such job"
	case JobAlreadyInStatus:
		return "already in that status"
	case JobNotAssignable:
		return "not assignable"
	case JobAlreadyPast:
		return "already past that status"
	default:
		return "unrecognised"
	}
}

// Jobs is the job lifecycle, as far as this domain reaches into it.
//
// Deliberately not "move this job to any status I name". A port shaped that way would be the
// transition table's second opinion arriving through the back door — this domain would choose the
// target status, and the one thing CLAUDE.md says about job status is that nothing outside the
// guard chooses it. The method names the one move SHIP-106 makes, and a domain that needs another
// declares another.
// # Four methods and not one taking a milestone, which is the same refusal a second time
//
// SHIP-111 records four things that move a job, and the obvious shape — one method taking the
// milestone — is the shape this comment already refused. The target status would then be chosen by
// whatever `delivery` passed, and the mapping from a milestone to a status would live in a
// translation table that Docs/02 §2 never sees. Four methods cost four lines each in the adapter
// and buy the property that **the set of moves this domain can ask for is fixed at compile time**:
// a fifth requires editing this interface, which is a decision somebody records rather than a
// string somebody passes.
//
// The methods take the actor's clock rather than reading one. A milestone recorded offline at
// 06:40 and synced at 09:15 is one act, and the two rows it writes — a milestone and a
// job_status_history transition — must agree about when the actor says it happened (Docs/02 §3.1).
// Each table stamps its own arrival time; neither takes the other's word for the actor's.
//
// # An implementation owes one thing beyond the translation, since SHIP-112
//
// A refusal must be reported as [JobAlreadyPast] when the job has already recorded a transition
// into the status the move names, and as [JobNotAssignable] when it has not. `jobs` reports both as
// one sentinel, because the transition table has no opinion about which direction a refused move
// was pointing in; telling them apart is a second question about the same job, asked of the same
// domain, in the same transaction. It is asked in the composition root because that is the only
// place a `jobs.Status` may be named at all.
type Jobs interface {
	// MoveToDriverAssigned runs the guarded transition on behalf of the provider, inside the
	// caller's transaction. providerID is the actor recorded against it.
	//
	// A non-nil error is a failure of the mechanism — the database, the outbox — rather than a
	// refusal. A refusal comes back as a [JobMove] with a nil error, because "Docs/02 does not
	// permit this" is an answer rather than a fault.
	MoveToDriverAssigned(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID) (JobMove, error)

	// MoveToEnRouteToPickup is Docs/02 §2's `Awarded / Driver assigned → En route to pickup`.
	//
	// Two `from` statuses and one method, because the caller is not choosing between them:
	// a provider driving the job themselves sets off from Awarded without nominating anybody,
	// and the guard is what knows that both are permitted.
	MoveToEnRouteToPickup(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID, recordedAt time.Time) (JobMove, error)

	// MoveToPickedUp is `En route to pickup → Picked up`.
	MoveToPickedUp(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID, recordedAt time.Time) (JobMove, error)

	// MoveToInTransit is `Picked up → In transit`.
	//
	// Docs/02 §2 permits this one as "an automatic presentation change" as well as an act, which
	// is why milestone.go's ActorSystem exists. Nothing applies it automatically yet, and when
	// something does it will be a task in cmd/worker rather than a request, so this method stays
	// the actor's path and is not widened to carry an actor type it would only ever be given one
	// value of.
	MoveToInTransit(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID, recordedAt time.Time) (JobMove, error)
}
