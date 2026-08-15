package admin

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// What this domain needs of other domains, declared by the consumer (Docs/06 §4.1, Docs/10 §2.3).
//
// internal/admin imports neither internal/jobs nor internal/bidding, and the boundary lint refuses
// both. cmd/api holds the implementations and is the only place the three packages meet.
//
// # Why a dispute needs anything from elsewhere at all
//
// Two facts decide whether a dispute may be raised, and this domain owns neither:
//
//   - whether the caller is party to the job. Docs/02 §2 permits "an eligible user/admin" to open
//     one, and the eligible users are the customer who owns the job (a `jobs` fact) and the
//     provider who won it (a `bidding` fact, through the accepted bid — SHIP-80).
//   - whether the job may move to 'Disputed' at all. That is the transition table of Docs/02 §2,
//     and every status change in the platform passes one guarded function in `jobs` (SHIP-57).
//     This domain must not have a second opinion about it, and does not.
//
// # Both take a db.Runner, because intake is one transaction
//
// Docs/10 §3.2: a port that must participate in the caller's transaction takes a Runner. The
// dispute row and the status change commit together or not at all. A dispute on a job that never
// froze would let the 72-hour auto-complete (Docs/02 §6.1) run through the middle of it, which is
// precisely the thing Docs/02 §3 says a dispute prevents; a job frozen with no dispute on it is a
// job nothing can unfreeze, because SHIP-164 unfreezes it by resolving a dispute.
//
// # Neither signature names a type declared in the package that implements it
//
// That would be the import the lint refuses, so `jobs.Status`, `jobs.Move` and a `bidding` bid
// cannot appear below. [Party] and [JobMove] are this package's own, and the adapters in cmd/api
// translate. It is the same constraint delivery/ports.go works under and the same answer.

// JobParties is who is entitled to raise a dispute about a job.
//
// One method, and it answers the only question this domain has about a job: which side of it, if
// any, this caller is on. Not who the customer is, not what the job is worth, not what it contains —
// a port is what a domain needs rather than what the other domain has.
//
// isParty is false when the caller is neither the job's customer nor the provider its bid was
// awarded to, **and also when there is no such job**. The two are deliberately one answer, and it
// is the same disclosure decision delivery.Awards records: a caller who could tell "that job exists
// and you are not on it" from "no such job" would learn that somebody else's job exists, and on a
// marketplace where the awarded provider is commercial information nobody published, that is the
// leak the 404 exists to prevent.
type JobParties interface {
	PartyOn(ctx context.Context, r db.Runner, jobID, userID uuid.UUID) (party Party, isParty bool, err error)
}

// JobMove is what the guarded transition did, in terms this domain can act on.
//
// The four values are the four outcomes of Docs/02 §2's table as seen from one caller: it moved,
// there is nothing to move, it is already there, or the document has no such row. They are
// deliberately not an error type — an error would have to be one of `jobs`' sentinels to be
// matched, and matching it would be an import.
//
// The shape is delivery.JobMove's, deliberately rather than accidentally. Two domains needing the
// same four outcomes from the same guard is not duplication to be factored out: a shared type would
// have to live somewhere both import, and the only such place is infrastructure, which may import
// neither domain (SHIP-15c). The cost is four constants; the alternative is a boundary rule bent
// for a convenience.
type JobMove int

const (
	// JobMoveUnrecognised is the zero value and is never a valid answer.
	//
	// First on purpose. An implementation that returns nothing useful — a stub, a half-written
	// adapter, a switch with a missing case — returns this, and [Service.RaiseDispute] refuses
	// it rather than reading silence as success.
	JobMoveUnrecognised JobMove = iota

	// JobMoved means the job is now 'Disputed' and the transition was recorded.
	JobMoved

	// JobNotFound means there is no such job.
	JobNotFound

	// JobAlreadyDisputed means the job was already where the move would have put it.
	//
	// Distinct from JobNotDisputable because the two want opposite handling, exactly as
	// jobs.ErrAlreadyInStatus is distinct from jobs.ErrTransitionNotPermitted: a job that is
	// already frozen has refused nothing.
	JobAlreadyDisputed

	// JobNotDisputable means Docs/02 §2 has no row from the job's status to 'Disputed'.
	//
	// The ordinary cases are both ends of the lifecycle. A job that has not been awarded has no
	// delivery to dispute — the customer cancels it instead — and a job that has completed or
	// been cancelled has left the lifecycle entirely. Docs/02 §2 permits 'Disputed' from
	// Awarded, Driver assigned, En route to pickup, Picked up, In transit and Delivered, and
	// from nowhere else.
	JobNotDisputable

	// JobAlreadyRemoved means the job was already 'Cancelled' when an administrator moved to
	// unpublish it (SHIP-160).
	//
	// Distinct from [JobNotRemovable] for the reason [JobAlreadyDisputed] is distinct from
	// [JobNotDisputable]: a job already off the marketplace has refused nothing, and the
	// console's answer is "somebody got there first" rather than "this cannot be done". It is
	// the ordinary outcome of two moderators reading the same queue, which is why it is worth
	// telling apart at all.
	JobAlreadyRemoved

	// JobNotRemovable means Docs/02 §2 has no row from the job's status to 'Cancelled'.
	//
	// The document permits it from Draft, Open and Negotiating — the statuses in which nobody is
	// yet committed — and from 'Disputed', which is SHIP-164's resolution rather than this. **So
	// an awarded job is not unpublishable**, and that is the document's decision rather than a
	// limitation of this port: a provider has committed and may have travelled, and Docs/02 §6.2
	// makes ending it after that a support case. An administrator who needs one stopped raises a
	// dispute and resolves it, which is the path that records both sides.
	JobNotRemovable
)

func (m JobMove) String() string {
	switch m {
	case JobMoved:
		return "moved"
	case JobNotFound:
		return "no such job"
	case JobAlreadyDisputed:
		return "already disputed"
	case JobNotDisputable:
		return "not disputable"
	case JobAlreadyRemoved:
		return "already unpublished"
	case JobNotRemovable:
		return "not unpublishable"
	default:
		return "unrecognised"
	}
}

// Jobs is the job lifecycle, as far as this domain reaches into it.
//
// Deliberately not "move this job to any status I name". A port shaped that way would be the
// transition table's second opinion arriving through the back door — this domain would choose the
// target status, and the one thing CLAUDE.md says about job status is that nothing outside the
// guard chooses it. The method names the one move SHIP-163 makes, and a ticket that needs another
// declares another.
//
// SHIP-164 needs the moves *out* of 'Disputed' — Docs/02 §2 offers Completed and Cancelled — and
// they are that ticket's to add here, one method each, for the same reason.
type Jobs interface {
	// MoveToDisputed runs the guarded transition on behalf of the complainant, inside the
	// caller's transaction. actorID is the account recorded against it and party is which kind
	// of actor they were on this job; reason is what the transition history records about why
	// the job froze.
	//
	// There is no recordedAt. Docs/02 §3.1's two clocks exist because a driver records a
	// milestone out of signal and the device syncs later; raising a dispute is a person at a
	// screen, online, now — and the moment the *incident* happened is captured on the dispute
	// itself rather than smuggled into the job's status history, where it would read as the time
	// somebody froze the job.
	//
	// A non-nil error is a failure of the mechanism — the database, the outbox — rather than a
	// refusal. A refusal comes back as a [JobMove] with a nil error, because "Docs/02 does not
	// permit this" is an answer rather than a fault.
	MoveToDisputed(ctx context.Context, r db.Runner, jobID, actorID uuid.UUID, party Party, reason string) (JobMove, error)

	// Unpublish runs Docs/02 §2's move to 'Cancelled' on an administrator's behalf, inside the
	// caller's transaction (SHIP-160).
	//
	// # A second named move rather than a status parameter
	//
	// The obvious shape is `Move(jobID, status, reason)`, and it is the one thing this port must
	// not be. CLAUDE.md's invariant is that job status is never a settable field, and a port
	// taking the target status would put the choice in *this* domain — the transition table's
	// second opinion arriving through the back door. Each move this domain needs is a method,
	// named for what it is for, which is why SHIP-164 will add two more rather than widening one.
	//
	// # The reason is not optional, at three levels
	//
	// SHIP-160's *Done when* requires one; `ck_job_status_history_admin_reason` requires one of
	// any administrator's transition; and the guarded function refuses the move without one. The
	// service checks it before opening a transaction, so the failure names the field rather than
	// a constraint.
	//
	// A non-nil error is a failure of the mechanism. A refusal comes back as a [JobMove] with a
	// nil error, because "Docs/02 does not permit this" is an answer rather than a fault.
	Unpublish(ctx context.Context, r db.Runner, jobID, actorID uuid.UUID, reason string) (JobMove, error)
}

// --- SHIP-117: the delivery-exception moderation queue -------------------------------------------

// ExceptionQueue is the jobs whose delivery evidence is a recorded reason rather than a photograph.
//
// # Why this is a port at all
//
// The rows live in `proofs`, which is `internal/delivery`'s table, and this domain may not import
// that package — the boundary lint refuses it in both directions. cmd/api holds the implementation
// and is the only place the two meet, exactly as [JobParties] spans `jobs` and `bidding`.
//
// **The vocabulary stays on the other side of the port.** [ExceptionEntry.Reason],
// [ExceptionEntry.Milestone] and [ExceptionEntry.JobStatus] are plain strings here, and that is not
// laziness: `delivery.ProofExceptionReason` is generated from `contracts/statuses.yaml` (SHIP-56a)
// and `jobs.Status` is Docs/02 §1's own list, so a copy of either in this package would be a second
// list to keep in step with a generated one. This domain does not decide what a valid exception
// reason is; it reports what was recorded, and `ck_proofs_exception_reason` is what makes that a
// closed set.
//
// # What "enters the moderation queue" means, and what it deliberately does not
//
// SHIP-117's *Done when* is that an exception-completed job **enters the moderation queue**, and the
// queue is a *query* rather than a table somebody has to remember to write to. `000604` said so when
// it built the index this reads through: "whether a job is queued for review is a fact about the
// *job*", and what that migration owed this ticket was "a cheap answer to which jobs completed
// through the exception path", which is `idx_proofs_exception`.
//
// A flag column would have needed `delivery` to write it — a cross-domain write, through a port that
// domain would have to declare — and it would have been a second source of truth that a repair
// script or a backfill could put out of step with the evidence. A query cannot drift from the rows
// it reads.
//
// **X-6 — whether a job completed through this path may auto-complete — is undecided and is not
// decided here.** A job entering this queue and a job auto-completing are not exclusive: this
// reports what was recorded, and nothing about the entry changes if operations answers X-6 either
// way. [ExceptionEntry.JobStatus] is carried for triage and means only what it says.
type ExceptionQueue interface {
	// ExceptionsAwaitingReview returns one page of entries, oldest first.
	//
	// Oldest first because Docs/04 §8 sets acknowledgement targets and the oldest entry is the
	// one closest to breaching one — the same ordering `idx_disputes_open` exists for.
	ExceptionsAwaitingReview(ctx context.Context, r db.Runner, q QueueQuery) ([]ExceptionEntry, error)
}

// QueueQuery is one page of a moderation queue.
//
// Cursor paged rather than offset paged, per Docs/10 §4.5: the queue grows at the end while somebody
// is reading it, and an offset would show them the same entry twice or skip one.
type QueueQuery struct {
	// Limit is how many entries to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After QueueCursor
}

// QueueCursor is the position of the last entry a caller saw.
//
// Two fields, because `recorded_at` is not unique: two deliveries recorded in the same millisecond
// would make a single-column cursor either skip one or repeat it. The identifier breaks the tie and
// is what makes the ordering total.
type QueueCursor struct {
	RecordedAt time.Time
	ProofID    uuid.UUID
}

// Zero reports whether this is the first page.
func (c QueueCursor) Zero() bool { return c.ProofID == uuid.Nil && c.RecordedAt.IsZero() }

// ExceptionEntry is one delivery that was evidenced by a reason rather than a photograph.
//
// **No object key, no signed URL and no photograph**, because by construction there is none — a
// proof row is one or the other and never both (`ck_proofs_photograph_or_exception`). A moderator
// following up reaches the delivery through the job.
//
// **No customer, no provider and no budget.** A queue entry is what somebody triaging needs to
// decide whether to open the job, and a customer's budget is never exposed to a provider under any
// circumstances — the safest way to keep an administrative shape clear of it is for it never to have
// carried one.
type ExceptionEntry struct {
	// ProofID identifies the evidence row, and is what the cursor is built from.
	ProofID uuid.UUID

	// JobID is the delivery to open.
	JobID uuid.UUID

	// Milestone is which recorded claim the exception stands behind — 'Delivered' for the
	// deliveries Docs/01 §4.4 is about, and carried rather than assumed because `proofs` does not
	// restrict itself to one milestone kind.
	Milestone string

	// Reason is why there is no photograph, from Docs/01 §4.4's three.
	Reason string

	// Note is the actor's own words, where they left any. Empty is ordinary: `milestones.reason`
	// is optional, and a driver who selected a reason has already said the most important part.
	Note string

	// RecordedAt is when the platform recorded the evidence, on the platform's clock.
	//
	// Deliberately not the actor's clock. Docs/02 §3.1 keeps the two apart because a driver
	// records a milestone out of signal and the device syncs later; a queue ordered by the
	// *device's* clock could be reordered by a handset with the wrong time, which is a queue an
	// entry can hide at the back of.
	RecordedAt time.Time

	// JobStatus is where the job is now, for triage.
	//
	// It implies nothing about X-6. A job in this queue may or may not be eligible for the
	// 72-hour auto-complete, that question is operations' and is open, and this field reports the
	// job's status rather than an opinion about it.
	JobStatus string
}

// --- SHIP-152: the administrator's view of jobs and their bids ------------------------------------

// JobDirectory is every job, its offers and its recorded transitions.
//
// # Why this is a port when the account search is not
//
// [Users] selects from `users` directly and postgres_users.go argues why: one shared table, read by
// the domain whose console needs it. This spans `jobs` and `job_status_history` (internal/jobs') and
// `bids` (internal/bidding'), and `admin` may import neither — the boundary lint refuses both. So
// the statements live in cmd/api beside [JobParties] and [ExceptionQueue], which is the only place
// the three packages meet.
//
// **The vocabulary stays on the other side.** [JobRecord.Status], [BidRecord.Status] and
// [StatusEvent.ActorType] are plain strings here for the reason [ExceptionEntry] gives: two of the
// three are generated from `contracts/statuses.yaml` (SHIP-56a), and a copy in this package would be
// a hand-written list shadowing a generated one. What the *filter* is validated against is supplied
// to [NewJobConsole] by cmd/api rather than copied — see [JobConsole.Statuses].
//
// # Both methods take a Runner, and only one of them needs it to be a transaction
//
// [JobDirectory.SearchJobs] is a single statement and is handed the pool. [JobDirectory.OpenJob] is
// three, and [JobConsole.Open] wraps them so that the job header, the bid list and the status
// history are one snapshot — a screen showing an `Awarded` job beside a bid list with nothing
// accepted is worse than a stale one, because somebody acts on it.
type JobDirectory interface {
	// SearchJobs returns one page of jobs matching the query, newest first.
	SearchJobs(ctx context.Context, r db.Runner, q JobQuery) ([]JobRecord, error)

	// OpenJob is one job with every bid and every recorded transition.
	//
	// found is false when there is no such job, which the caller turns into [ErrJobNotFound].
	// **Not a disclosure decision**, unlike the identical-looking answer [JobParties] gives: the
	// caller here is an administrator holding `jobs.read` and every job is theirs to open.
	OpenJob(ctx context.Context, r db.Runner, jobID uuid.UUID) (detail JobDetail, found bool, err error)
}
