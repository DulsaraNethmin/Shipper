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
