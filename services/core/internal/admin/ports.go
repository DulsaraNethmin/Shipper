package admin

import (
	"context"

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
