package delivery

import (
	"context"

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

	// JobAlreadyDriverAssigned means the job was already at 'Driver assigned'.
	//
	// Distinct from JobNotAssignable because the two want opposite handling, exactly as
	// jobs.ErrAlreadyInStatus is distinct from jobs.ErrTransitionNotPermitted: a job that is
	// already where the caller wanted it has not refused anything.
	JobAlreadyDriverAssigned

	// JobNotAssignable means Docs/02 §2 has no row from the job's status to 'Driver assigned'.
	//
	// The ordinary case is a job that has not been awarded yet, or one that has already left
	// for the pickup — Docs/02 §2 permits Awarded → En route to pickup directly, and offers no
	// way back to 'Driver assigned' from there.
	JobNotAssignable
)

func (m JobMove) String() string {
	switch m {
	case JobMoved:
		return "moved"
	case JobNotFound:
		return "no such job"
	case JobAlreadyDriverAssigned:
		return "already driver assigned"
	case JobNotAssignable:
		return "not assignable"
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
type Jobs interface {
	// MoveToDriverAssigned runs the guarded transition on behalf of the provider, inside the
	// caller's transaction. providerID is the actor recorded against it.
	//
	// A non-nil error is a failure of the mechanism — the database, the outbox — rather than a
	// refusal. A refusal comes back as a [JobMove] with a nil error, because "Docs/02 does not
	// permit this" is an answer rather than a fault.
	MoveToDriverAssigned(ctx context.Context, r db.Runner, jobID, providerID uuid.UUID) (JobMove, error)
}
