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
	clock  clock.Clock
	store  postgresStore
}

// NewService builds the domain service.
//
// Both collaborators are required and it panics without either, in the same spirit as
// jobs.NewService and httpx.RegisterCode: this is called once from the composition root, a missing
// collaborator is a programming mistake rather than a runtime condition, and the alternative here
// is a service that starts and then answers every assignment with a nil-pointer panic.
//
// Neither may be defaulted to something harmless, which is the reason they are not optional the way
// jobs' geocoder is. A nil Awards is "nobody is checked", and a nil Jobs is "the job never moves" —
// both are silent failures of the two rules this endpoint exists to enforce.
func NewService(jobs Jobs, awards Awards, c clock.Clock) *Service {
	if jobs == nil {
		panic("delivery: NewService needs the job lifecycle; an assignment that moves no job " +
			"leaves a driver on a job nothing downstream believes has one (Docs/02 §2)")
	}
	if awards == nil {
		panic("delivery: NewService needs the award lookup; without it any provider could put a " +
			"driver on any job (Docs/02 §3)")
	}
	if c == nil {
		// Defaulting to clock.System{} would start the service, and the first milestone
		// recorded without an actor-supplied time would be stamped from a clock nobody chose.
		panic("delivery: NewService needs a clock (Docs/10 §6.3)")
	}
	return &Service{jobs: jobs, awards: awards, clock: c}
}

// AssignDriver puts a driver on a job the caller was awarded, and moves the job to
// 'Driver assigned' (SHIP-106).
//
// It reports whether the assignment was created. False means the job already had this exact driver
// and nothing was written — see "a repeated nomination" below.
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
) (Assignment, bool, error) {
	// pgx.Tx and *pgxpool.Pool both satisfy db.Runner and only one of them is a transaction.
	// Asking the type is blunt, and it is the only way to tell: Runner exists precisely so that
	// a query does not have to know which it is holding.
	if _, inTx := r.(pgx.Tx); !inTx {
		return Assignment{}, false, fmt.Errorf("delivery: assigning a driver to %s: %w", jobID, ErrNotInTransaction)
	}

	nomination := n.normalise()
	problems := nomination.problems()
	if err := problems.Err(); err != nil {
		return Assignment{}, false, err
	}

	awarded, isAwarded, err := s.awards.AwardedProvider(ctx, r, jobID)
	if err != nil {
		return Assignment{}, false, err
	}
	if !isAwarded {
		return Assignment{}, false, fmt.Errorf("delivery: %s has no accepted bid: %w", jobID, ErrJobNotFound)
	}
	if awarded != providerID {
		return Assignment{}, false, fmt.Errorf("delivery: %s was awarded to %s, not %s: %w",
			jobID, awarded, providerID, ErrNotAwardedProvider)
	}

	if nomination.Self {
		// The account's own number, read now rather than carried on the request. A mobile
		// supplied by the caller and a mobile the platform verified are different facts, and
		// only one of them is worth putting a job-scoped link on (SHIP-107).
		phone, err := s.store.accountPhone(ctx, r, providerID)
		if err != nil {
			return Assignment{}, false, err
		}
		nomination.DriverMobile = phone
	}

	live, hasDriver, err := s.store.liveAssignment(ctx, r, jobID)
	if err != nil {
		return Assignment{}, false, err
	}
	if hasDriver {
		if live.DriverName == nomination.DriverName && live.DriverMobile == nomination.DriverMobile {
			return live, false, nil
		}
		return Assignment{}, false, fmt.Errorf("delivery: %s is already driven by %s: %w",
			jobID, live.ID, ErrDriverAlreadyAssigned)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Assignment{}, false, fmt.Errorf("delivery: generating an assignment id: %w", err)
	}

	assignment, err := s.store.insert(ctx, r, Assignment{
		ID:           id,
		JobID:        jobID,
		DriverName:   nomination.DriverName,
		DriverMobile: nomination.DriverMobile,
	})
	if err != nil {
		return Assignment{}, false, err
	}

	move, err := s.jobs.MoveToDriverAssigned(ctx, r, jobID, providerID)
	if err != nil {
		return Assignment{}, false, err
	}

	switch move {
	case JobMoved:
		return assignment, true, nil

	case JobAlreadyInStatus:
		// The job says 'Driver assigned' and no assignment was live, which is the state a
		// stood-down driver leaves behind. The new assignment stands and the job is already
		// where it should be, so there is nothing to move and no history row to write —
		// exactly the absorption Docs/02 §3.1 asks for rather than an error.
		return assignment, true, nil

	case JobNotAssignable:
		return Assignment{}, false, fmt.Errorf("delivery: %s cannot take a driver: %w", jobID, ErrJobNotAssignable)

	case JobNotFound:
		// Reachable only if the job was deleted between the award lookup and here, which
		// nothing in this platform does — fk_driver_assignments_job would have refused the
		// insert first. Reported rather than assumed away.
		return Assignment{}, false, fmt.Errorf("delivery: %s vanished mid-assignment: %w", jobID, ErrJobNotFound)

	default:
		return Assignment{}, false, fmt.Errorf("delivery: moving %s: %q: %w",
			jobID, move, ErrJobMoveUnrecognised)
	}
}

// RecordMilestone records what the awarded provider says happened on a delivery, and moves the job
// if Docs/02 §2 has a row for it (SHIP-111).
//
// It reports whether the milestone was recorded. False means this idempotency key had already
// recorded it and nothing was written — the retry path, which is what this endpoint exists for.
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
//  5. a milestone Docs/02 §2 has no transition for from where the job now stands:
//     [ErrMilestoneNotPermitted] — **and this one is SHIP-112's, not this ticket's.** See below.
//
// The customer of the job is refused by (2) and (3) like any other stranger, which is Docs/02 §3
// read exactly: "delivery-status updates must be made only by the awarded provider, their assigned
// driver, or an administrator acting with an audit reason". Confirming a delivery is not recording
// one.
//
// # A milestone that moves nothing is still recorded, and that boundary is deliberate
//
// A repeat of the milestone the job is already at — a driver who reaches a pickup, finds nobody
// there, and records 'En route to pickup' again on the way back (Docs/02 §5) — writes a row and
// moves nothing. 000601 has no uniqueness on (job_id, milestone) precisely so that it can.
//
// What is *not* built here is the late arrival: a queued 'Picked up' that syncs after 'In transit'
// is already recorded. Docs/02 §3.1 requires it to be "absorbed, not rejected as an error", and
// that is **SHIP-112**, a ticket of its own. Until it lands the answer is [ErrMilestoneNotPermitted]
// and the transaction rolls back, so nothing half-recorded is left behind. The seam is one case of
// the switch at the bottom of this function: SHIP-112 changes what `JobNotAssignable` returns, and
// nothing else, because the milestone row is already written by then and already independent of
// whether the job moved.
//
// r must be a transaction. The milestone and the transition are one act recorded in two tables, and
// a milestone that committed without its transition would leave a delivery whose timeline and whose
// status disagree.
func (s *Service) RecordMilestone(
	ctx context.Context,
	r db.Runner,
	providerID, jobID uuid.UUID,
	rec Recording,
) (Record, bool, error) {
	if _, inTx := r.(pgx.Tx); !inTx {
		return Record{}, false, fmt.Errorf("delivery: recording on %s: %w", jobID, ErrNotInTransaction)
	}

	recording := rec.normalise()
	problems := recording.problems()
	if err := problems.Err(); err != nil {
		return Record{}, false, err
	}

	// Checked in the domain and not left to the handler that read the header. A milestone
	// written with a NULL key is outside uq_milestones_idempotency — the index is partial — so
	// this is the one input whose absence would silently remove the guarantee this function
	// spends most of its length on, rather than failing loudly.
	if recording.Key == "" {
		return Record{}, false, fmt.Errorf("delivery: recording %s on %s: %w",
			recording.Milestone, jobID, ErrNoIdempotencyKey)
	}

	awarded, isAwarded, err := s.awards.AwardedProvider(ctx, r, jobID)
	if err != nil {
		return Record{}, false, err
	}
	if !isAwarded {
		return Record{}, false, fmt.Errorf("delivery: %s has no accepted bid: %w", jobID, ErrJobNotFound)
	}
	if awarded != providerID {
		return Record{}, false, fmt.Errorf("delivery: %s was awarded to %s, not %s: %w",
			jobID, awarded, providerID, ErrNotAwardedProvider)
	}

	if recording.Milestone == MilestoneDelivered {
		return Record{}, false, fmt.Errorf("delivery: %s cannot be recorded delivered yet: %w",
			jobID, ErrProofRequired)
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
		return Record{}, false, fmt.Errorf("delivery: generating a milestone id: %w", err)
	}

	stored, recorded, err := s.store.insertMilestone(ctx, r, Record{
		ID:        id,
		JobID:     jobID,
		Milestone: recording.Milestone,

		// The provider, always, in this ticket. The driver's job-scoped token is SHIP-107 and
		// SHIP-108 and cannot be presented here yet, so ActorDriver is declared (000601, and
		// milestone.go) and unreachable: a driver's milestone would name their
		// driver_assignments row rather than an account, which is why Service.Driver exists
		// and why this is the one field that changes when that token lands.
		Actor:   ActorProvider,
		ActorID: providerID,

		Reason:          recording.Reason,
		Key:             recording.Key,
		ActorRecordedAt: recordedAt.UTC(),
	})
	if err != nil {
		return Record{}, false, err
	}

	if !recorded {
		return s.alreadyRecorded(ctx, r, jobID, recording)
	}

	move, err := s.moveFor(ctx, r, providerID, jobID, recording.Milestone, recordedAt)
	if err != nil {
		return Record{}, false, err
	}

	switch move {
	case JobMoved:
		return stored, true, nil

	case JobAlreadyInStatus:
		// The job is already where this milestone would put it, which is a recording rather
		// than a transition — the failed pickup attempt above. The row stands and no history
		// row is written, because nothing moved.
		return stored, true, nil

	case JobNotAssignable:
		// **SHIP-112's seam.** Docs/02 §3.1 wants this absorbed: the row kept, the job left
		// where it is, and the client told what happened. Doing that here would be building
		// SHIP-112, so the transaction rolls back instead and the milestone goes with it.
		// What SHIP-112 changes is this case and this case alone.
		return Record{}, false, fmt.Errorf("delivery: %s cannot record %s from where it stands: %w",
			jobID, recording.Milestone, ErrMilestoneNotPermitted)

	case JobNotFound:
		// Unreachable in practice — fk_milestones_job would have refused the insert above —
		// and reported rather than assumed away.
		return Record{}, false, fmt.Errorf("delivery: %s vanished mid-recording: %w", jobID, ErrJobNotFound)

	default:
		return Record{}, false, fmt.Errorf("delivery: moving %s: %q: %w",
			jobID, move, ErrJobMoveUnrecognised)
	}
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
) (Record, bool, error) {
	existing, found, err := s.store.milestoneRecordedBy(ctx, r, jobID, recording.Key)
	if err != nil {
		return Record{}, false, err
	}
	if !found {
		// The index refused the insert and the row is not there to be read. Nothing in this
		// platform deletes a milestone — the table is append-only and has no DELETE path — so
		// this is a contradiction rather than a race, and it becomes an opaque 500 with its
		// cause logged rather than a reply that invents an answer.
		return Record{}, false, fmt.Errorf("delivery: %s refused a duplicate on %s and holds no row for it: %w",
			recording.Key, jobID, ErrMilestoneVanished)
	}

	if existing.Milestone != recording.Milestone {
		return Record{}, false, fmt.Errorf("delivery: %s already recorded %s on %s, not %s: %w",
			recording.Key, existing.Milestone, jobID, recording.Milestone, ErrIdempotencyKeyReused)
	}

	return existing, false, nil
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
// caller of [Service.RecordMilestone] is the awarded provider, because the job-scoped token a driver
// would present is SHIP-107 and SHIP-108 and does not exist. The attribution it was written for is
// one field — `Actor` in that function — and it needs this lookup on the day a driver token can name
// the assignment it belongs to, not before. It stays because this domain's tests read it and because
// deleting it would only mean writing it again at SHIP-108.
func (s *Service) Driver(ctx context.Context, r db.Runner, jobID uuid.UUID) (Assignment, bool, error) {
	return s.store.liveAssignment(ctx, r, jobID)
}
