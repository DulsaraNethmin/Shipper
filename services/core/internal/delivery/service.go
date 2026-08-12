package delivery

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
// There is no clock here, unlike `jobs` and `fleet`. No timestamp in an assignment is Go's to
// choose: created_at is 000600's default, and the two clocks a transition records belong to `jobs`
// (Docs/02 §3.1). SHIP-111 records a milestone with an actor-supplied time and will need one.
type Service struct {
	jobs   Jobs
	awards Awards
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
func NewService(jobs Jobs, awards Awards) *Service {
	if jobs == nil {
		panic("delivery: NewService needs the job lifecycle; an assignment that moves no job " +
			"leaves a driver on a job nothing downstream believes has one (Docs/02 §2)")
	}
	if awards == nil {
		panic("delivery: NewService needs the award lookup; without it any provider could put a " +
			"driver on any job (Docs/02 §3)")
	}
	return &Service{jobs: jobs, awards: awards}
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

	case JobAlreadyDriverAssigned:
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

// Driver is the live assignment on a job, if there is one.
//
// A read rather than a lock, and no transaction: one statement is atomic on its own. It exists for
// this domain's own tests and for SHIP-111, which has to attribute a milestone to the driver who
// recorded it.
func (s *Service) Driver(ctx context.Context, r db.Runner, jobID uuid.UUID) (Assignment, bool, error) {
	return s.store.liveAssignment(ctx, r, jobID)
}
