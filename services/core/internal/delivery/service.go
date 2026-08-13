package delivery

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Putting a driver on an awarded job (SHIP-106).
//
// # This is the first endpoint in the delivery domain, and it is the join between three of them
//
// Nothing here decides who won the work and nothing here decides which status moves are legal.
// `bidding` owns the first and `jobs` owns the second, and both arrive through the ports in
// ports.go rather than through an import. What this domain owns is what happens in between: that
// exactly one driver is live on a job, that the assignment and the status change commit together,
// and that a retry does not produce two drivers.
//
// # The transaction is this domain's, and Docs/10 §3.2 says why
//
// "The transaction belongs to the domain that owns the invariant." The invariant is that a job at
// 'Driver assigned' has a driver and a job with a driver has moved — neither half is true on its
// own, and the two writes live in different domains. So the handler opens one transaction and both
// ports run inside it.

// Service holds this domain's rules.
//
// It owns no connection. Every method takes a db.Runner, so the caller decides whether the work
// stands alone or joins a transaction it already opened.
//
// # The clock is here for milestones and for nothing else
//
// SHIP-106 needed none — created_at is 000600's default, and the two clocks a transition records
// belong to `jobs`. SHIP-111 does: a client that records a milestone while it is online sends no
// time at all, and something has to say what "now" was. That has to be an injected clock rather
// than time.Now(), per Docs/10 §6.3, or the one behaviour worth testing here — that the actor's
// clock and the platform's are two values and stay two values — is untestable, because the two
// would agree to within a millisecond on every run.
type Service struct {
	jobs   Jobs
	awards Awards
	owners JobOwners
	tokens *DriverTokenIssuer
	proof  proofStorage
	clock  clock.Clock
	store  postgresStore
}

// proofStorage is everything the object store is asked for, and the limits it is asked within
// (SHIP-114, SHIP-115).
//
// One field rather than three on [Service], because they are never useful apart: a signer with no
// policy would issue URLs for anything, a policy with no signer is a struct nobody reads, and a
// metadata reader judged against a different policy from the one that signed would accept objects
// the platform refused to authorise. It is also what stops one positional argument to [NewService]
// becoming three as SHIP-155 adds its own.
//
// **SHIP-114 called this `proofUploader` and SHIP-115 renamed it.** With one port the specific name
// was clearer; with a second port that exists to *read* an object back, "uploader" would have been
// actively wrong about half of what it holds.
type proofStorage struct {
	uploads ProofUploads
	objects ProofObjects
	policy  UploadPolicy
}

// NewService builds the domain service.
//
// Both collaborators are required and it panics without either, in the same spirit as
// jobs.NewService and httpx.RegisterCode: this is called once from the composition root, a missing
// collaborator is a programming mistake rather than a runtime condition, and the alternative here
// is a service that starts and then answers every assignment with a nil-pointer panic.
//
// None of them may be defaulted to something harmless, which is the reason they are not optional
// the way jobs' geocoder is. A nil Awards is "nobody is checked", a nil Jobs is "the job never
// moves", and a nil issuer is an assignment that produces no link — all three are silent failures
// of a rule this endpoint exists to enforce.
func NewService(
	jobs Jobs,
	awards Awards,
	owners JobOwners,
	tokens *DriverTokenIssuer,
	uploads ProofUploads,
	objects ProofObjects,
	policy UploadPolicy,
	c clock.Clock,
) *Service {
	if jobs == nil {
		panic("delivery: NewService needs the job lifecycle; an assignment that moves no job " +
			"leaves a driver on a job nothing downstream believes has one (Docs/02 §2)")
	}
	if awards == nil {
		panic("delivery: NewService needs the award lookup; without it any provider could put a " +
			"driver on any job (Docs/02 §3)")
	}
	if owners == nil {
		// Refused rather than defaulted to "nobody is the customer", which would start the
		// service and quietly answer 404 to every customer asking for the proof on their own
		// job — a broken screen (Docs/01 §4.4's acceptance measure) reported as a missing one.
		panic("delivery: NewService needs the job ownership lookup; without it a customer cannot " +
			"be told apart from a stranger asking about their delivery (SHIP-115)")
	}
	if tokens == nil {
		// Refused rather than made optional, because SHIP-107's *Done when* is that the token
		// is generated **on assignment**: a service that could assign a driver without minting
		// one would leave a job at 'Driver assigned' whose driver has no way to reach it, and
		// no later request would notice.
		panic("delivery: NewService needs the driver token issuer; an assignment with no " +
			"job-scoped link leaves the driver nothing to open (SHIP-107)")
	}
	if uploads == nil {
		// Refused rather than made optional, for the reason the issuer above is. A service
		// that could answer an upload request with no signer would have to answer it with
		// something, and every candidate is worse than not starting: a URL nobody signed, an
		// empty string a client would PUT to nowhere, or a 500 on the one request path
		// Docs/01 §4.4 makes the condition of completing a delivery.
		panic("delivery: NewService needs somewhere to put proof; a delivery cannot be " +
			"completed without a photograph or a recorded exception (SHIP-114)")
	}
	if objects == nil {
		// Refused rather than made optional, and this one has the sharpest failure direction of
		// the four. Without it the platform cannot ask whether a photograph was ever uploaded,
		// and the only way to record proof at all would be to believe the client — which is
		// precisely the arrangement SHIP-115 exists to refuse (see proof.go).
		panic("delivery: NewService needs to be able to read an object back; the platform is not " +
			"in the upload path, so asking the store is the only way it can know proof exists " +
			"(SHIP-115)")
	}
	if !policy.valid() {
		// Checked here rather than per request, so a configuration failure stops the process
		// at startup instead of surfacing as a validation error blaming a client's perfectly
		// good photograph. internal/config has already refused a zero TTL and an empty type
		// list, so reaching this means the policy came from somewhere other than configuration.
		panic("delivery: NewService needs an upload policy with a size limit, at least one " +
			"accepted content type, and a lifetime (SHIP-114)")
	}
	if c == nil {
		// Defaulting to clock.System{} would start the service, and the first milestone
		// recorded without an actor-supplied time would be stamped from a clock nobody chose.
		panic("delivery: NewService needs a clock (Docs/10 §6.3)")
	}
	return &Service{
		jobs:   jobs,
		awards: awards,
		owners: owners,
		tokens: tokens,
		proof:  proofStorage{uploads: uploads, objects: objects, policy: policy},
		clock:  c,
	}
}

// AssignDriver puts a driver on a job the caller was awarded, moves the job to 'Driver assigned',
// and mints the driver's job-scoped link (SHIP-106, SHIP-107).
//
// It reports whether the assignment was created. False means the job already had this exact driver
// and nothing was written — see "a repeated nomination" below.
//
// # The token is generated here, and "on assignment" is SHIP-107's whole specification
//
// Every success returns a [DriverToken] for this assignment and this job, and there is no other
// way to obtain one: minting is not an endpoint of its own, so a token cannot exist without a
// driver_assignments row behind it. It is minted **inside the caller's transaction**, before the
// commit, which costs nothing — signing is an HMAC over a few hundred bytes and touches no I/O —
// and buys the property that a failure to sign takes the assignment with it rather than leaving a
// committed driver with no way to reach the job.
//
// A repeated nomination gets a *fresh* token for the same assignment, and both remain valid until
// they expire. That is deliberate rather than overlooked: the two tokens grant the same one job to
// the same one driver, so nothing is widened, and refusing to reissue would mean a provider whose
// phone lost the response could never obtain the link again. **Invalidating a previous link is
// SHIP-109**, which is a different intent — reissue *and revoke* — and needs state this ticket
// does not write.
//
// # The refusals, in this order, and the order is the point
//
//  1. a job nobody was awarded, or no job at all, is [ErrJobNotFound];
//  2. a job awarded to another provider is [ErrNotAwardedProvider];
//  3. a job that already has a different live driver is [ErrDriverAlreadyAssigned];
//  4. a job Docs/02 §2 has no `→ Driver assigned` row for is [ErrJobNotAssignable].
//
// The first two are one 404 on the wire. Checking them before anything else is what keeps them
// indistinguishable: a stranger who could tell "already has a driver" from "no such job" would
// learn that the job exists and roughly what is happening on it.
//
// The fourth is checked last because it is not this domain's to check. [Jobs.MoveToDriverAssigned]
// runs the guarded transition, which locks the job and reads Docs/02 §2 for itself; asking first
// would be a second copy of the transition table, which is the drift 000402's header refuses to
// introduce.
//
// # A repeated nomination of the same driver succeeds and writes nothing
//
// The idempotency middleware absorbs a retry that reuses its key. This absorbs the one that does
// not — a phone that lost its connection, was restarted, and generated a fresh key for the same
// intent — which is the ordinary shape of a delivery app (Docs/02 §3.1) and the same call
// [jobs.Service.Cancel] and fleet's deactivation both make. A *different* driver is a replacement
// rather than a retry, and that is [ErrDriverAlreadyAssigned].
//
// # What stops two drivers is the index, not this function
//
// uq_driver_assignments_active is partial on unassigned_at being NULL, so two requests racing both
// read no live assignment and both insert — and the second is refused by the btree rather than by
// the check above. That refusal comes back as [ErrDriverAlreadyAssigned] too, so a race and a
// second tap answer identically.
//
// r must be a transaction. The assignment row and the status change are one decision: an
// assignment that committed without the transition would leave a driver on an Awarded job, and a
// transition that committed without the assignment would leave a job at 'Driver assigned' with
// nobody driving it.
func (s *Service) AssignDriver(
	ctx context.Context,
	r db.Runner,
	providerID, jobID uuid.UUID,
	n Nomination,
) (Assignment, DriverToken, bool, error) {
	// pgx.Tx and *pgxpool.Pool both satisfy db.Runner and only one of them is a transaction.
	// Asking the type is blunt, and it is the only way to tell: Runner exists precisely so that
	// a query does not have to know which it is holding.
	if _, inTx := r.(pgx.Tx); !inTx {
		return s.refused(fmt.Errorf("delivery: assigning a driver to %s: %w", jobID, ErrNotInTransaction))
	}

	nomination := n.normalise()
	problems := nomination.problems()
	if err := problems.Err(); err != nil {
		return s.refused(err)
	}

	awarded, isAwarded, err := s.awards.AwardedProvider(ctx, r, jobID)
	if err != nil {
		return s.refused(err)
	}
	if !isAwarded {
		return s.refused(fmt.Errorf("delivery: %s has no accepted bid: %w", jobID, ErrJobNotFound))
	}
	if awarded != providerID {
		return s.refused(fmt.Errorf("delivery: %s was awarded to %s, not %s: %w",
			jobID, awarded, providerID, ErrNotAwardedProvider))
	}

	if nomination.Self {
		// The account's own number, read now rather than carried on the request. A mobile
		// supplied by the caller and a mobile the platform verified are different facts, and
		// only one of them is worth putting a job-scoped link on (SHIP-107).
		phone, err := s.store.accountPhone(ctx, r, providerID)
		if err != nil {
			return s.refused(err)
		}
		nomination.DriverMobile = phone
	}

	live, hasDriver, err := s.store.liveAssignment(ctx, r, jobID)
	if err != nil {
		return s.refused(err)
	}
	if hasDriver {
		if live.DriverName == nomination.DriverName && live.DriverMobile == nomination.DriverMobile {
			return s.granted(live, false)
		}
		return s.refused(fmt.Errorf("delivery: %s is already driven by %s: %w",
			jobID, live.ID, ErrDriverAlreadyAssigned))
	}

	id, err := uuid.NewV7()
	if err != nil {
		return s.refused(fmt.Errorf("delivery: generating an assignment id: %w", err))
	}

	assignment, err := s.store.insert(ctx, r, Assignment{
		ID:           id,
		JobID:        jobID,
		DriverName:   nomination.DriverName,
		DriverMobile: nomination.DriverMobile,
	})
	if err != nil {
		return s.refused(err)
	}

	move, err := s.jobs.MoveToDriverAssigned(ctx, r, jobID, providerID)
	if err != nil {
		return s.refused(err)
	}

	switch move {
	case JobMoved:
		return s.granted(assignment, true)

	case JobAlreadyInStatus:
		// The job says 'Driver assigned' and no assignment was live, which is the state a
		// stood-down driver leaves behind. The new assignment stands and the job is already
		// where it should be, so there is nothing to move and no history row to write —
		// exactly the absorption Docs/02 §3.1 asks for rather than an error.
		return s.granted(assignment, true)

	case JobNotAssignable:
		return s.refused(fmt.Errorf("delivery: %s cannot take a driver: %w", jobID, ErrJobNotAssignable))

	case JobAlreadyPast:
		// The job has been at 'Driver assigned' and has set off since. **Refused, and not
		// absorbed the way a late milestone is** — the two look alike and are not.
		//
		// A milestone is a claim about something that already happened, so keeping it costs
		// nothing and discarding it loses a record (Docs/02 §3.1). An assignment is an
		// instruction about who drives the job *now*: there is nothing historical to keep, and
		// a driver_assignments row written for a job that has already gone would name a live
		// driver on a delivery somebody else is carrying. Docs/02 §2 offers no way back to
		// 'Driver assigned' from anywhere, so the honest answer is the one SHIP-106 gave.
		//
		// It is the same answer as JobNotAssignable and it is written out rather than folded
		// in with it: an outcome no case names falls to the default below and becomes a 500,
		// which is the right treatment for an outcome nobody has thought about and the wrong
		// one for this.
		//
		// **Unreachable through any endpoint today**, for the same reason [ErrDriverLinkSuperseded]
		// is: reaching it needs a job that has been at 'Driver assigned' and has no live
		// assignment, and nothing writes `unassigned_at` until SHIP-109. A job that has set off
		// without ever being 'Driver assigned' — the ordinary case, since Docs/02 §2 permits
		// Awarded → En route to pickup directly — is [JobNotAssignable] above.
		// TestAJobPastDriverAssignedIsRefused drives it through a stubbed port for that reason.
		return s.refused(fmt.Errorf("delivery: %s has already moved past taking a driver: %w",
			jobID, ErrJobNotAssignable))

	case JobNotFound:
		// Reachable only if the job was deleted between the award lookup and here, which
		// nothing in this platform does — fk_driver_assignments_job would have refused the
		// insert first. Reported rather than assumed away.
		return s.refused(fmt.Errorf("delivery: %s vanished mid-assignment: %w", jobID, ErrJobNotFound))

	default:
		return s.refused(fmt.Errorf("delivery: moving %s: %q: %w",
			jobID, move, ErrJobMoveUnrecognised))
	}
}

// granted is every successful exit from [Service.AssignDriver]: the assignment stands, so the
// driver gets their link.
//
// One function rather than a mint at each of the three success points, for the reason
// cmd/api/routes_delivery.go gives about its own `move`: a fourth outcome that minted differently —
// or not at all — would still compile, still pass its own test, and hand one path a link the other
// two did not.
//
// The token names the assignment rather than the driver, because a driver has nothing else to be
// named by (see [DriverClaims.AssignmentID]).
func (s *Service) granted(a Assignment, created bool) (Assignment, DriverToken, bool, error) {
	token, err := s.tokens.Issue(a.JobID, a.ID)
	if err != nil {
		// Inside the transaction, so this rolls the assignment back. An assignment that
		// committed without a link would leave a driver on a job they cannot open, and nothing
		// downstream would notice: the row is complete and the status is right.
		return s.refused(fmt.Errorf("delivery: minting the link for %s on %s: %w", a.ID, a.JobID, err))
	}
	return a, token, created, nil
}

// refused is the empty answer, so that a refusal cannot accidentally carry a token.
//
// Four return values is three too many to retype at twelve exit points, and the one that matters is
// the second: a `DriverToken{}` written by hand at each of them is a `DriverToken{Value: token}`
// away from a refusal that hands out a link anyway.
func (s *Service) refused(err error) (Assignment, DriverToken, bool, error) {
	return Assignment{}, DriverToken{}, false, err
}

// RecordMilestone records what the awarded provider says happened on a delivery, and moves the job
// if Docs/02 §2 has a row for it (SHIP-111, SHIP-112).
//
// It reports what it did, as an [Outcome]: the milestone was recorded, it was recorded and
// deliberately moved nothing because the job is already past it, or this idempotency key had
// already recorded it and nothing was written.
//
// # What guarantees "once per idempotency key", and it is not Redis
//
// Two mechanisms sit on this, and they are not the same guarantee:
//
//	SHIP-15's middleware   replays the first response for a repeated key, from Redis, for as long
//	                       as the entry lives. It never reaches this function, so it costs nothing
//	                       and it protects nothing that outlives a TTL or a flush.
//	uq_milestones_idempotency  refuses the second row, in the database, permanently. It is what is
//	                       still true when the phone reconnects after a day in a valley, when the
//	                       cache has been evicted, and when two retries arrive at two instances in
//	                       the same millisecond.
//
// The second is the one the *Done when* rests on. This function is written as though the middleware
// were not there: it inserts, and if the key had already been used it reads what that key recorded
// and answers with it. A retry that arrives after the response has expired from Redis therefore
// gets the same milestone back rather than a second one, and two concurrent retries — which the
// middleware would not both let through, but two instances or a cache miss might — resolve to one
// row, because they resolve on a btree rather than on a read-then-write.
//
// A key that recorded a *different* milestone is [ErrIdempotencyKeyReused] rather than a replay,
// which is the same call the middleware makes on a fingerprint mismatch: answering with the first
// request's outcome would tell a client that something it never sent had succeeded.
//
// # The refusals, in this order
//
//  1. nothing recorded, and nothing said, without an idempotency key: [ErrNoIdempotencyKey];
//  2. a job nobody was awarded, or no job at all: [ErrJobNotFound];
//  3. a job awarded to another provider: [ErrNotAwardedProvider];
//  4. 'Delivered', until proof can be captured: [ErrProofRequired];
//  5. a milestone the job has not reached the point for: [ErrMilestoneNotPermitted].
//
// The customer of the job is refused by (2) and (3) like any other stranger, which is Docs/02 §3
// read exactly: "delivery-status updates must be made only by the awarded provider, their assigned
// driver, or an administrator acting with an audit reason". Confirming a delivery is not recording
// one.
//
// # A milestone that moves nothing is still recorded, and there are now two ways to get there
//
// A repeat of the milestone the job is already at — a driver who reaches a pickup, finds nobody
// there, and records 'En route to pickup' again on the way back (Docs/02 §5) — writes a row and
// moves nothing. 000601 has no uniqueness on (job_id, milestone) precisely so that it can.
//
// A **late** milestone does the same and means something different. A queued 'Picked up' that syncs
// after 'In transit' is already recorded is [OutcomeAbsorbed]: Docs/02 §3.1 requires it to be
// "absorbed, not rejected as an error… the platform accepts the historical fact without moving the
// job backwards", and that is SHIP-112. This is where the milestone row already being written pays
// off — it is inserted before the move is attempted and is independent of whether the job moved, so
// absorbing costs nothing but declining to return the error.
//
// # What an absorbed milestone writes, and what it deliberately does not
//
// One row, in `milestones`, carrying the actor's own clock. That is the whole of it.
//
// **No `job_status_history` row**, and that is what "without moving the job backwards" means here
// rather than a softer reading. Every row in that table describes a status change (000402's trigger
// will not let one exist otherwise), the job's status does not change, and a history row for a move
// that did not happen would be read by SHIP-77's timeline and by support as though it had. No status
// update, and no domain event: `jobs` emits `job.status_changed` from inside the transition, and
// there was no transition.
//
// # Late and premature are different refusals, and only one of them is absorbed
//
// [JobAlreadyPast] is the job having been there already. [JobNotAssignable] is the job not having
// arrived yet — an `in_transit` recorded while the delivery is still on its way to the pickup — and
// stays [ErrMilestoneNotPermitted] with the transaction rolled back. Docs/02 §3.1 asks for the
// first and says nothing about the second, and the asymmetry is not pedantry: a late milestone can
// never succeed on a retry, because Docs/02 §2 has no way back, while a premature one succeeds
// unchanged as soon as the delivery reaches that point. Absorbing a premature milestone would
// answer a client "recorded" for a move that then silently never happened.
//
// A job cancelled or disputed before it reached the milestone falls in the second group today, and
// **that is SHIP-113's** — "a queued update that contradicts an administrative action loses… the
// attempt is retained in history". Retaining it is that ticket's change to this same switch.
//
// r must be a transaction. The milestone and the transition are one act recorded in two tables, and
// a milestone that committed without its transition would leave a delivery whose timeline and whose
// status disagree.
func (s *Service) RecordMilestone(
	ctx context.Context,
	r db.Runner,
	providerID, jobID uuid.UUID,
	rec Recording,
) (Record, Outcome, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return notRecorded(fmt.Errorf("delivery: recording on %s: %w", jobID, ErrNotInTransaction))
	}

	recording := rec.normalise()
	problems := recording.problems()
	if err := problems.Err(); err != nil {
		return notRecorded(err)
	}

	// Checked in the domain and not left to the handler that read the header. A milestone
	// written with a NULL key is outside uq_milestones_idempotency — the index is partial — so
	// this is the one input whose absence would silently remove the guarantee this function
	// spends most of its length on, rather than failing loudly.
	if recording.Key == "" {
		return notRecorded(fmt.Errorf("delivery: recording %s on %s: %w",
			recording.Milestone, jobID, ErrNoIdempotencyKey))
	}

	awarded, isAwarded, err := s.awards.AwardedProvider(ctx, r, jobID)
	if err != nil {
		return notRecorded(err)
	}
	if !isAwarded {
		return notRecorded(fmt.Errorf("delivery: %s has no accepted bid: %w", jobID, ErrJobNotFound))
	}
	if awarded != providerID {
		return notRecorded(fmt.Errorf("delivery: %s was awarded to %s, not %s: %w",
			jobID, awarded, providerID, ErrNotAwardedProvider))
	}

	// Checked in front of the insert, so nothing is written for a 'Delivered' at all.
	//
	// **This is the first of three layers, and mutation testing was what established the order
	// of them.** Moving this check to *after* the insert changes nothing today, because it still
	// returns an error and the transaction still rolls the row back — and because [Service.moveFor]
	// has no case for 'Delivered' either, so one reaching the switch below would fail there. What
	// SHIP-112 changes is that a case in that switch now *commits* rather than rolling back, so the
	// layer that has to hold is the one that keeps a delivered milestone from reaching it. Only
	// removing this check **and** giving 'Delivered' a move that answers [JobAlreadyPast] produces
	// a delivered milestone with neither photo proof nor an exception behind it — which is what
	// SHIP-118 will be editing, and the shape TestAbsorptionCannotReachDelivered exists to catch.
	if recording.Milestone == MilestoneDelivered {
		return notRecorded(fmt.Errorf("delivery: %s cannot be recorded delivered yet: %w",
			jobID, ErrProofRequired))
	}

	recordedAt := recording.RecordedAt
	if recordedAt.IsZero() {
		// Nothing was claimed about when the actor acted, which is what an online client
		// sends: there is one clock and both columns are read from it. An offline client
		// supplies its own, and the two columns then differ by however long the phone was
		// out of signal — which is the whole of Docs/02 §3.1.
		recordedAt = s.clock.Now()
	}

	id, err := uuid.NewV7()
	if err != nil {
		return notRecorded(fmt.Errorf("delivery: generating a milestone id: %w", err))
	}

	stored, recorded, err := s.store.insertMilestone(ctx, r, Record{
		ID:        id,
		JobID:     jobID,
		Milestone: recording.Milestone,

		// The provider, always, in this ticket. SHIP-107 mints the driver's job-scoped
		// token and **nothing verifies one** until SHIP-108, so no request can arrive here
		// as a driver and ActorDriver stays declared (000601, and milestone.go) and
		// unreachable. What that ticket needs for the attribution is already inside the
		// token: DriverClaims.AssignmentID names the driver_assignments row, which is the
		// only identity a driver has.
		Actor:   ActorProvider,
		ActorID: providerID,

		Reason:          recording.Reason,
		Key:             recording.Key,
		ActorRecordedAt: recordedAt.UTC(),
	})
	if err != nil {
		return notRecorded(err)
	}

	if !recorded {
		return s.alreadyRecorded(ctx, r, jobID, recording)
	}

	// The evidence, in the same transaction as the claim it stands behind (SHIP-115, SHIP-116).
	//
	// Written after the milestone because it points at it, and before the move because the move
	// is the one step that can *commit* on a refusal: SHIP-112's absorption keeps the row and
	// leaves the job alone, and a delivery whose absorbed milestone lost its photograph on the way
	// through would be the record growing and the evidence not.
	//
	// A photograph and a reasoned exception take the identical path, which is what makes the
	// absorption argument above true of both: a driver who could not photograph a pickup in a yard
	// with no signal keeps their reason when the queued milestone finally syncs.
	if recording.hasEvidence() {
		if _, err := s.recordEvidence(ctx, r, jobID, stored.ID, recording.Proof, recording.Exception); err != nil {
			return notRecorded(err)
		}
	}

	move, err := s.moveFor(ctx, r, providerID, jobID, recording.Milestone, recordedAt)
	if err != nil {
		return notRecorded(err)
	}

	switch move {
	case JobMoved:
		return stored, OutcomeRecorded, nil

	case JobAlreadyInStatus:
		// The job is already where this milestone would put it, which is a recording rather
		// than a transition — the failed pickup attempt above. The row stands and no history
		// row is written, because nothing moved.
		return stored, OutcomeRecorded, nil

	case JobAlreadyPast:
		// **SHIP-112.** The job has been past this point and Docs/02 §3.1 is explicit: "the
		// platform accepts the historical fact without moving the job backwards". So this
		// returns rather than erroring, the transaction commits, and the milestone inserted
		// above is the whole of what it leaves behind — see this function's header for what
		// is deliberately not written with it.
		//
		// Nothing is undone and nothing extra is done. The value of the seam SHIP-111 left is
		// exactly that: the row was already independent of whether the job moved.
		return stored, OutcomeAbsorbed, nil

	case JobNotAssignable:
		// The delivery has not reached the point this milestone describes. Refused, and the
		// transaction rolls the milestone back with it — see the header for why this one is
		// not absorbed and the case above is.
		return notRecorded(fmt.Errorf("delivery: %s cannot record %s from where it stands: %w",
			jobID, recording.Milestone, ErrMilestoneNotPermitted))

	case JobNotFound:
		// Unreachable in practice — fk_milestones_job would have refused the insert above —
		// and reported rather than assumed away.
		return notRecorded(fmt.Errorf("delivery: %s vanished mid-recording: %w", jobID, ErrJobNotFound))

	default:
		return notRecorded(fmt.Errorf("delivery: moving %s: %q: %w",
			jobID, move, ErrJobMoveUnrecognised))
	}
}

// notRecorded is every failing exit from [Service.RecordMilestone], so that a refusal cannot
// accidentally carry an outcome that says something was written.
//
// The same call [Service.refused] makes one function up, and it earns its place for the same reason:
// [OutcomeUnrecognised] typed out by hand at eleven exit points is one keystroke from
// [OutcomeRecorded], and a caller reading that beside a non-nil error would take the branch that
// writes a 201.
func notRecorded(err error) (Record, Outcome, error) {
	return Record{}, OutcomeUnrecognised, err
}

// alreadyRecorded answers a request whose idempotency key has already recorded something.
//
// Split out because it is the retry path and the retry path is the ticket: it deserves to be
// readable on its own rather than as a branch halfway down a longer function.
//
// The row is read back rather than reconstructed from the request. What the client is told is what
// the platform holds — including the timestamp of the *first* attempt, which is the point: an
// actor's clock is not corrected by a later retry that happens to disagree with it.
func (s *Service) alreadyRecorded(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	recording Recording,
) (Record, Outcome, error) {
	existing, found, err := s.store.milestoneRecordedBy(ctx, r, jobID, recording.Key)
	if err != nil {
		return notRecorded(err)
	}
	if !found {
		// The index refused the insert and the row is not there to be read. Nothing in this
		// platform deletes a milestone — the table is append-only and has no DELETE path — so
		// this is a contradiction rather than a race, and it becomes an opaque 500 with its
		// cause logged rather than a reply that invents an answer.
		return notRecorded(fmt.Errorf("delivery: %s refused a duplicate on %s and holds no row for it: %w",
			recording.Key, jobID, ErrMilestoneVanished))
	}

	if existing.Milestone != recording.Milestone {
		return notRecorded(fmt.Errorf("delivery: %s already recorded %s on %s, not %s: %w",
			recording.Key, existing.Milestone, jobID, recording.Milestone, ErrIdempotencyKeyReused))
	}

	// [OutcomeAlreadyRecorded] whether the first attempt moved the job or was absorbed, and the
	// move is not re-evaluated to find out which. The question "is the job past this?" has a
	// different answer at a different moment — a delivery only travels forwards — so asking it
	// again would let one action's outcome change under a retry, which is precisely what an
	// idempotency key exists to prevent. What the client is told is what the platform holds.
	return existing, OutcomeAlreadyRecorded, nil
}

// moveFor runs the transition a milestone implies, through the port method that names it.
//
// A switch rather than a map, so that a milestone with no move is a compile-time-visible case
// rather than a missing key that reads as zero. The two milestones with no case here are refused
// before this is reached — 'Driver assigned' by [Recording.problems] and 'Delivered' by
// [ErrProofRequired] — and reaching it with either would mean one of those checks had been removed,
// which is worth an error rather than a silent no-op.
func (s *Service) moveFor(
	ctx context.Context,
	r db.Runner,
	providerID, jobID uuid.UUID,
	m Milestone,
	recordedAt time.Time,
) (JobMove, error) {
	switch m {
	case MilestoneEnRouteToPickup:
		return s.jobs.MoveToEnRouteToPickup(ctx, r, jobID, providerID, recordedAt)
	case MilestonePickedUp:
		return s.jobs.MoveToPickedUp(ctx, r, jobID, providerID, recordedAt)
	case MilestoneInTransit:
		return s.jobs.MoveToInTransit(ctx, r, jobID, providerID, recordedAt)
	default:
		return JobMoveUnrecognised, fmt.Errorf("delivery: %s implies no move this domain can ask for: %w",
			m, ErrJobMoveUnrecognised)
	}
}

// Driver is the live assignment on a job, if there is one.
//
// A read rather than a lock, and no transaction: one statement is atomic on its own.
//
// SHIP-106 wrote it for SHIP-111, expecting it to be how a driver's milestone is attributed.
// **SHIP-111 does not use it, and that is the honest outcome rather than an oversight**: every
// caller of [Service.RecordMilestone] is the awarded provider, because nothing verifies a driver
// token until SHIP-108.
//
// **SHIP-107 narrows what it will eventually be for, and that is worth recording before somebody
// reaches for it.** A verified token already names the assignment
// ([DriverClaims.AssignmentID]), so SHIP-108 does not need this lookup to *identify* a driver —
// what it needs it for is the question the claim cannot answer, which is whether that assignment
// is still live. A token outlives a stand-down; the row is what says so.
//
// **SHIP-108 uses it for exactly that, through [Service.AssignmentFor] rather than directly**, and
// the indirection is the point rather than ceremony: this method takes a bare job id, so anybody
// holding one can ask it anything. The method a driver route calls takes a [DriverGrant], and a
// grant is obtainable only from a verified token.
func (s *Service) Driver(ctx context.Context, r db.Runner, jobID uuid.UUID) (Assignment, bool, error) {
	return s.store.liveAssignment(ctx, r, jobID)
}

// AssignmentFor is the delivery a driver's own link opens (SHIP-108).
//
// # It takes a grant rather than a job id, and that is the whole signature
//
// There is no way to ask this question about an arbitrary job: a [DriverGrant] is produced by
// [DriverTokenVerifier.Verify] and by nothing else, and the middleware that produces one has already
// checked that the job in the request path is the job inside the token. A handler therefore cannot
// widen the scope by passing the wrong identifier, because it has no identifier to pass.
//
// # What it checks that the token cannot say
//
// A token is stateless and cannot be recalled, so it keeps verifying after the assignment behind it
// has ended. The row is what knows: if the job's live assignment is not the one the grant names, the
// link has been superseded and opens nothing ([ErrDriverLinkSuperseded]). That is the lookup
// [Service.Driver]'s comment reserved for this ticket, and it is the mechanism SHIP-109 will reissue
// against — revocation as a read rather than a denylist.
//
// Nothing writes `unassigned_at` today, so a superseded link is unreachable through any endpoint;
// the check is here because the alternative is a link that outlives its assignment in silence.
//
// One statement and no transaction: a single read is atomic on its own.
func (s *Service) AssignmentFor(ctx context.Context, r db.Runner, grant DriverGrant) (Assignment, error) {
	live, hasDriver, err := s.Driver(ctx, r, grant.JobID)
	if err != nil {
		return Assignment{}, err
	}

	// One answer for two states, deliberately: a job with no live driver and a job whose live
	// driver is somebody else both mean this link no longer opens anything, and telling them apart
	// would say something about the job to a holder who is no longer on it.
	if !hasDriver || live.ID != grant.AssignmentID {
		return Assignment{}, fmt.Errorf("delivery: %s is not the live assignment on %s: %w",
			grant.AssignmentID, grant.JobID, ErrDriverLinkSuperseded)
	}

	return live, nil
}
