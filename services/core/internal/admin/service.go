package admin

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
)

// Raising a dispute (SHIP-163). The first code in the admin domain.
//
// # This is the user's half of a workflow whose other half is administrative
//
// Worth stating first, because the domain's name invites the opposite assumption. Intake is called
// by a customer or a provider, authenticated the ordinary way, and the route declares RequireUser
// like every other product endpoint in the service. Administrator sign-in is a separate system that
// a user token can never reach (SHIP-147), the middleware behind it does not exist, and a route
// declaring RequireAdmin stops the process at startup rather than being served open. Investigation
// and outcome — Docs/04 §7's other two stages — are SHIP-164 and arrive with that middleware.
//
// The dispute nevertheless belongs here rather than in `jobs` or `delivery`, because the rules
// around it are this domain's: Docs/04 §7 is the intake specification, Docs/04 §5's sixth
// moderation queue is where an open dispute lands, and Docs/04 §9's controls are what govern what
// happens to it next.
//
// # The transaction is this domain's, and Docs/10 §3.2 says why
//
// "The transaction belongs to the domain that owns the invariant." The invariant is that a job with
// an open dispute is frozen and a frozen job has an open dispute — neither half is true on its own,
// and the two writes live in different domains. So the handler opens one transaction and the port
// runs inside it.

// Service holds this domain's rules.
//
// It owns no connection. Every method takes a db.Runner, so the caller decides whether the work
// stands alone or joins a transaction it already opened.
type Service struct {
	jobs    Jobs
	parties JobParties
	clock   clock.Clock
	store   postgresStore
}

// NewService builds the domain service.
//
// All three collaborators are required and it panics without any of them, in the same spirit as
// jobs.NewService and httpx.RegisterCode: this is called once from the composition root, a missing
// collaborator is a programming mistake rather than a runtime condition, and the alternative is a
// service that starts and then answers every intake with a nil-pointer panic.
//
// None may be defaulted to something harmless. A nil JobParties is "nobody is checked", a nil Jobs
// is "the job never freezes", and a nil clock is a future-dated incident nothing refuses — all
// three are silent failures of rules this endpoint exists to enforce.
func NewService(jobs Jobs, parties JobParties, c clock.Clock) *Service {
	if jobs == nil {
		panic("admin: NewService needs the job lifecycle; a dispute that freezes no job leaves " +
			"the 72-hour auto-complete running through the middle of it (Docs/02 §3, §6.1)")
	}
	if parties == nil {
		panic("admin: NewService needs the party lookup; without it any account could raise a " +
			"dispute about any job (Docs/02 §2)")
	}
	if c == nil {
		panic("admin: NewService needs a clock (Docs/10 §6.3)")
	}
	return &Service{jobs: jobs, parties: parties, clock: c}
}

// RaiseDispute records what a customer or provider says went wrong, and freezes the job (SHIP-163).
//
// It reports whether the dispute was raised. False means this idempotency key had already raised it
// and nothing was written — the retry path, which is half of what this endpoint exists for.
//
// # The refusals, in this order, and the order is the point
//
//  1. nothing recorded, and nothing said, without an idempotency key: [ErrNoIdempotencyKey];
//  2. a job the caller is not party to, or no job at all: [ErrNotAParty] / [ErrJobNotFound];
//  3. a job that already has a dispute awaiting an outcome: [ErrDisputeAlreadyOpen];
//  4. a job Docs/02 §2 has no `→ Disputed` row for: [ErrJobNotDisputable].
//
// The first two are one 404 on the wire. Checking the party before anything else is what keeps them
// indistinguishable: a stranger who could tell "that job already has a dispute" from "no such job"
// would learn that the job exists and roughly what is happening on it.
//
// The fourth is checked last because it is not this domain's to check. [Jobs.MoveToDisputed] runs
// the guarded transition, which locks the job and reads Docs/02 §2 for itself; asking first would
// be a second copy of the transition table, which is the drift 000402's header refuses to introduce.
//
// # What a retry gets, and what a second party gets, are different answers
//
// Two requests can arrive at a job that already has an open dispute, and they deserve opposite
// treatment:
//
//	the same key again      the phone that lost its connection. It gets the dispute it already
//	                        raised, 200, and nothing is written. uq_disputes_idempotency is what
//	                        makes that true after Redis has forgotten the response.
//	a different key         somebody raising a *second* dispute — frequently the other party to
//	                        the same delivery. It is refused with [ErrDisputeAlreadyOpen], because
//	                        a job is frozen once and SHIP-164 unfreezes it by resolving one
//	                        dispute. uq_disputes_open_per_job is what makes that true under a race.
//
// The two are told apart by which index the insert conflicts on — see [postgresStore.insertDispute].
//
// r must be a transaction. The dispute row and the status change are one act: a dispute that
// committed without the transition would leave a complaint against a job the platform still thinks
// is running normally, and a transition that committed without the dispute would leave a job frozen
// with nothing to resolve.
func (s *Service) RaiseDispute(
	ctx context.Context,
	r db.Runner,
	complainantID, jobID uuid.UUID,
	in Intake,
) (Dispute, bool, error) {
	// pgx.Tx and *pgxpool.Pool both satisfy db.Runner and only one of them is a transaction.
	// Asking the type is blunt, and it is the only way to tell: Runner exists precisely so that
	// a query does not have to know which it is holding.
	if _, inTx := r.(pgx.Tx); !inTx {
		return Dispute{}, false, fmt.Errorf("admin: raising a dispute on %s: %w", jobID, ErrNotInTransaction)
	}

	intake := in.normalise()
	problems := intake.problems(s.clock.Now())
	if err := problems.Err(); err != nil {
		return Dispute{}, false, err
	}

	// Checked in the domain and not left to the handler that read the header. A dispute written
	// with a NULL key is outside uq_disputes_idempotency — the index is partial — so this is the
	// one input whose absence would silently remove a guarantee rather than failing loudly.
	if intake.Key == "" {
		return Dispute{}, false, fmt.Errorf("admin: raising a dispute on %s: %w", jobID, ErrNoIdempotencyKey)
	}

	party, isParty, err := s.parties.PartyOn(ctx, r, jobID, complainantID)
	if err != nil {
		return Dispute{}, false, err
	}
	if !isParty {
		// One answer for "no such job" and "not your job", deliberately — see [ErrNotAParty].
		// The sentinel distinguishes them so a test can tell a refusal from a disappearance;
		// the wire does not.
		return Dispute{}, false, fmt.Errorf("admin: %s is not party to %s: %w",
			complainantID, jobID, ErrNotAParty)
	}
	if !party.Valid() {
		// The port answered with something that is not one of the two. A wiring failure rather
		// than a request failure, and reported rather than written to a column
		// ck_disputes_complainant_party would refuse anyway.
		return Dispute{}, false, fmt.Errorf("admin: %q is not a party this domain recognises: %w",
			party, ErrPartyUnrecognised)
	}

	// The ordinary case of a job that already has an open dispute, read so that the refusal
	// carries a message a person can act on. The index behind it is what is right about the
	// race; this is what is right about the message — and a retry reaching this point is not
	// refused here, because the insert below is what can tell a retry from a second raise.
	if open, found, err := s.store.openDispute(ctx, r, jobID); err != nil {
		return Dispute{}, false, err
	} else if found && open.Key != intake.Key {
		return Dispute{}, false, fmt.Errorf("admin: %s is already disputed by %s: %w",
			jobID, open.ID, ErrDisputeAlreadyOpen)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Dispute{}, false, fmt.Errorf("admin: generating a dispute id: %w", err)
	}

	dispute, raised, err := s.store.insertDispute(ctx, r, Dispute{
		ID:    id,
		JobID: jobID,

		ComplainantID:    complainantID,
		ComplainantParty: party,

		Category:       intake.Category,
		Description:    intake.Description,
		DesiredOutcome: intake.DesiredOutcome,
		OccurredAt:     intake.OccurredAt.UTC(),
		Evidence:       intake.Evidence,

		Key: intake.Key,
	})
	if err != nil {
		return Dispute{}, false, err
	}
	if !raised {
		return s.alreadyRaised(ctx, r, jobID, intake)
	}

	move, err := s.jobs.MoveToDisputed(ctx, r, jobID, complainantID, party, string(intake.Category))
	if err != nil {
		return Dispute{}, false, err
	}

	switch move {
	case JobMoved:
		return dispute, true, nil

	case JobAlreadyDisputed:
		// The job says 'Disputed' and no dispute was open, which is the state a resolved
		// dispute would leave behind if SHIP-164's outcome failed to move the job back. The new
		// dispute stands and the job is already frozen, so there is nothing to move and no
		// history row to write — the same absorption Docs/02 §3.1 asks for rather than an error.
		return dispute, true, nil

	case JobNotDisputable:
		return Dispute{}, false, fmt.Errorf("admin: %s cannot be disputed: %w", jobID, ErrJobNotDisputable)

	case JobNotFound:
		// Reachable only if the job was deleted between the party lookup and here, which
		// nothing in this platform does — fk_disputes_job would have refused the insert first.
		// Reported rather than assumed away.
		return Dispute{}, false, fmt.Errorf("admin: %s vanished mid-intake: %w", jobID, ErrJobNotFound)

	default:
		return Dispute{}, false, fmt.Errorf("admin: moving %s: %q: %w",
			jobID, move, ErrJobMoveUnrecognised)
	}
}

// alreadyRaised answers a request whose idempotency key has already raised something.
//
// Split out because it is the retry path and the retry path is half the ticket: it deserves to be
// readable on its own rather than as a branch halfway down a longer function.
//
// The row is read back rather than reconstructed from the request. What the client is told is what
// the platform holds — including the incident time of the *first* attempt, which is the point: a
// complainant's account is not amended by a retry that happens to disagree with it.
func (s *Service) alreadyRaised(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
	intake Intake,
) (Dispute, bool, error) {
	existing, found, err := s.store.disputeRaisedBy(ctx, r, jobID, intake.Key)
	if err != nil {
		return Dispute{}, false, err
	}
	if !found {
		// The index refused the insert and the row is not there to be read. Nothing in this
		// platform deletes a dispute, so this is a contradiction rather than a race, and it
		// becomes an opaque 500 with its cause logged rather than a reply that invents an
		// answer.
		return Dispute{}, false, fmt.Errorf("admin: %s refused a duplicate on %s and holds no row for it: %w",
			intake.Key, jobID, ErrDisputeVanished)
	}

	// The same call the middleware makes on a fingerprint mismatch. The category is what
	// discriminates: a client that reused one key for two different complaints has made two
	// actions, and answering with the first would tell it that something it never sent had been
	// raised.
	if existing.Category != intake.Category {
		return Dispute{}, false, fmt.Errorf("admin: %s already raised %s on %s, not %s: %w",
			intake.Key, existing.Category, jobID, intake.Category, ErrIdempotencyKeyReused)
	}

	return existing, false, nil
}

// OpenDispute is the dispute a job is frozen by, if any.
//
// A read rather than a lock, and no transaction: one statement is atomic on its own.
//
// It is what SHIP-164 reads before it resolves one, and what this package's own tests read to
// establish that a refused intake left nothing behind.
func (s *Service) OpenDispute(ctx context.Context, r db.Runner, jobID uuid.UUID) (Dispute, bool, error) {
	return s.store.openDispute(ctx, r, jobID)
}
