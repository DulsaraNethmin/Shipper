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

// --- SHIP-117, SHIP-157: the delivery-exception moderation queue ---------------------------------

// ExceptionQueue is Docs/04 §5's fourth queue: every job whose delivery is going wrong.
//
// # One queue on four grounds, and why it is one endpoint rather than four
//
// Docs/04 §5 numbers seven queues and this is the fourth of them — "delivery exceptions: overdue
// pickup, delayed delivery, failed proof of delivery" — to which SHIP-157's *Done when* adds
// unsynced milestones. **They are one screen because the document made them one item.** SHIP-117
// built the failed-proof ground alone and said so at the time: moderation.go's header records that
// "the other two are SHIP-157's, which is where the queue becomes one screen rather than one
// endpoint".
//
// A second endpoint per ground would have been four cursors, four pages and four things a moderator
// has to check in the morning — and it would have made "what is going wrong with deliveries right
// now, oldest first" a question the console has to assemble by merging four sorted streams. Merging
// them is what a `UNION ALL` and one ordering do for free.
//
// **The widening is additive at the endpoint and not at the shape.** `GET /v1/admin/moderation/
// exceptions` keeps its path, its permission, its ordering and its paging; an entry gains a
// [ExceptionEntry.Ground] saying why it is here and [ExceptionEntry.EntryID] replaces the
// proof-shaped `proof_id`, because three of the four grounds are not evidenced by a proof row at
// all. No console consumed the old shape — `apps/admin` is still a shell — so the rename is a
// contract change and nothing more.
//
// # Why this is a port at all
//
// The rows live in `proofs` and `milestones`, which are `internal/delivery`'s tables, and in `jobs`,
// which is `internal/jobs`'; this domain may not import either — the boundary lint refuses it in
// both directions. cmd/api holds the implementation and is the only place the three meet, exactly as
// [JobParties] spans `jobs` and `bidding`.
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
	// ExceptionsAwaitingReview returns one page of entries, oldest first, across every ground.
	//
	// Oldest first because Docs/04 §8 sets acknowledgement targets and the oldest entry is the
	// one closest to breaching one — the same ordering `idx_disputes_open` exists for.
	//
	// `unsyncedThreshold` is Docs/02 §3.1's twenty-four hours, passed rather than known. This
	// domain must not hold a second copy of it: `delivery.UnsyncedAlertThreshold` is the one
	// authority and cmd/api hands it in, which is the same arrangement [JobConsole.Statuses]
	// uses for the status vocabulary. A constant here would agree with that one by comment.
	ExceptionsAwaitingReview(
		ctx context.Context,
		r db.Runner,
		q QueueQuery,
		unsyncedThreshold time.Duration,
	) ([]ExceptionEntry, error)
}

// ExceptionGround is why an entry is in the delivery-exception queue (SHIP-157).
//
// **This vocabulary is this domain's own**, which is the difference between it and every other
// string on [ExceptionEntry]. `Reason`, `Milestone` and `JobStatus` are other domains' closed lists
// and are carried through untranslated; a ground is a *moderation* concept — it names which of
// Docs/04 §5's four situations a row is evidence of — and no other package has an opinion about it.
type ExceptionGround string

// The four grounds, in Docs/04 §5's own order with SHIP-157's addition last.
const (
	// GroundOverduePickup is a job past the end of its pickup window that has not been picked
	// up. Docs/04 §5's first named ground.
	GroundOverduePickup ExceptionGround = "overdue_pickup"

	// GroundDelayedDelivery is a job past the end of its drop-off window that has not been
	// delivered. Docs/04 §5's second.
	GroundDelayedDelivery ExceptionGround = "delayed_delivery"

	// GroundFailedProof is a delivery evidenced by a recorded reason rather than a photograph
	// (Docs/01 §4.4). Docs/04 §5's third, and the one SHIP-117 built.
	GroundFailedProof ExceptionGround = "failed_proof"

	// GroundUnsyncedMilestone is an update that reached the platform more than
	// `delivery.UnsyncedAlertThreshold` after the driver recorded it — Docs/02 §3.1's
	// twenty-four-hour rung, which SHIP-128 made measurable and SHIP-157's *Done when* adds to
	// this queue.
	GroundUnsyncedMilestone ExceptionGround = "unsynced_milestone"
)

// ExceptionGrounds is the closed set, for validation and for a message that names the choices.
var ExceptionGrounds = []ExceptionGround{
	GroundOverduePickup,
	GroundDelayedDelivery,
	GroundFailedProof,
	GroundUnsyncedMilestone,
}

// Valid reports whether g is one of the four.
func (g ExceptionGround) Valid() bool {
	for _, known := range ExceptionGrounds {
		if g == known {
			return true
		}
	}
	return false
}

func (g ExceptionGround) String() string { return string(g) }

// QueueQuery is one page of a moderation queue.
//
// Cursor paged rather than offset paged, per Docs/10 §4.5: the queue grows at the end while somebody
// is reading it, and an offset would show them the same entry twice or skip one.
type QueueQuery struct {
	// Limit is how many entries to return. Bounded by internal/pagination before it gets here.
	Limit int

	// After is where the previous page stopped. The zero value is the first page.
	After QueueCursor

	// Ground narrows to one of [ExceptionGrounds] (SHIP-157). Empty is every ground.
	//
	// **A filter rather than four endpoints.** Docs/04 §5 makes these one queue, and a moderator
	// working through overdue pickups is narrowing a screen rather than opening a different one —
	// so the cursor, the ordering and the shape stay the same and only the predicate changes. An
	// unrecognised value is refused rather than ignored: an ignored filter answers with every
	// entry, which reads exactly like "everything is on this ground" to somebody who mistyped.
	Ground ExceptionGround
}

// QueueCursor is the position of the last entry a caller saw.
//
// **Three fields, and the third is SHIP-157's.** `recorded_at` is not unique — two deliveries
// recorded in the same millisecond would make a single-column cursor either skip one or repeat it —
// and once the queue unions four grounds the identifier is not enough either: `overdue_pickup` and
// `delayed_delivery` are both keyed by the **job**, so one job whose pickup and drop-off windows end
// at the same instant produces two entries agreeing on both other columns. The ground breaks that
// tie and is what makes the ordering total again.
type QueueCursor struct {
	RecordedAt time.Time
	Ground     ExceptionGround
	EntryID    uuid.UUID
}

// Zero reports whether this is the first page.
func (c QueueCursor) Zero() bool {
	return c.EntryID == uuid.Nil && c.Ground == "" && c.RecordedAt.IsZero()
}

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
	// Ground is why this entry is in the queue — one of [ExceptionGrounds].
	//
	// It is on every entry rather than implied by which fields are populated, because "the
	// milestone is empty" is a fact about a row and not an explanation a moderator can act on.
	Ground ExceptionGround

	// EntryID identifies the row this entry was derived from, and with [ExceptionEntry.Ground]
	// and [ExceptionEntry.RecordedAt] it is what the cursor is built from.
	//
	// **What it identifies depends on the ground**, which is the honest shape rather than an
	// evasion: a proof for `failed_proof`, a milestone for `unsynced_milestone`, and the job
	// itself for the two window grounds — where there is no evidence row, because the fact is
	// that nothing was recorded. It is not a handle a client may resolve; it exists to make the
	// ordering total and to let a console tell two entries apart.
	EntryID uuid.UUID

	// JobID is the delivery to open. The only identifier on this shape that a client may use.
	JobID uuid.UUID

	// Milestone is which recorded claim the entry stands behind, where one exists.
	//
	// 'Delivered' for the deliveries Docs/01 §4.4 is about, carried rather than assumed because
	// `proofs` does not restrict itself to one milestone kind. **Empty on the two window
	// grounds**, where the entry exists precisely because no milestone was recorded.
	Milestone string

	// Reason is why there is no photograph, from Docs/01 §4.4's three. Empty on every ground but
	// `failed_proof`, which is the only one that has an evidenced reason.
	Reason string

	// Note is the actor's own words, where they left any. Empty is ordinary: `milestones.reason`
	// is optional, and a driver who selected a reason has already said the most important part.
	Note string

	// RecordedAt is the platform's clock, and it is the fact that put this entry in the queue.
	//
	// It means something slightly different per ground and that is deliberate rather than sloppy:
	// when the evidence was recorded for `failed_proof`, when the update *arrived* for
	// `unsynced_milestone`, and when the window closed for the two window grounds. In every case
	// it is the instant from which the entry has been waiting, which is what an ordering by
	// urgency needs and what Docs/04 §8's targets are measured from.
	//
	// Deliberately never the actor's clock. Docs/02 §3.1 keeps the two apart because a driver
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
