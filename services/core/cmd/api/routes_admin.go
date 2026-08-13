package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/admin"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// The admin domain's routes (SHIP-163 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # The first route in M6, and it is not an administrative one
//
// Docs/04 §7 has three stages and the first is a party to a delivery reporting that something went
// wrong. So this route declares **RequireUser**, and deliberately not RequireAdmin: the caller is a
// customer or a provider, authenticated the ordinary way. Administrator sign-in is a separate system
// a user token can never reach (SHIP-147), the guards map in routes.go has no entry for that class,
// and a route declaring it stops the process at startup rather than being served open — which is the
// right outcome and not one to work around. SHIP-164's administrative half arrives with the
// middleware.
//
// # The route is under /jobs and the handler is in admin, and that is not a contradiction
//
// A URL is a client's map of the product, not a diagram of the packages behind it. Reporting a
// problem is something a client does *to a job*, so it lives under the job — the same reading that
// puts SHIP-106's driver and SHIP-111's milestones there. What decides which domain serves it is who
// owns the rule, and the rules here are `admin`'s: Docs/04 §7 is the intake specification, Docs/04
// §5's sixth queue is where the result lands, and Docs/04 §9's controls govern what happens next.
// SHIP-82 settled the general form of this — a route is declared where its answer is decided, not
// where its path points.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/disputes",
			Group:   GroupV1,

			// RequireUser. See the file header: the complainant is a customer or a provider,
			// and the administrative stages of Docs/04 §7 are a separate ticket behind a
			// separate credential.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return adminHandler(d).RaiseDispute() },
		},
	)
}

// adminHandler builds the domain's handler from what Deps already carries.
//
// Nothing admin needs is missing from Deps, which is the test Docs/10 §9.2 sets for whether a domain
// has been written the way the shared-surface rules ask: the job service, the outbox writer and the
// two ports are all pure functions of the pool, the clock and the configuration, so no field had to
// be added to a shared struct and no line to its literal in main.go.
//
// It panics for the same reason jobsHandler and deliveryHandler do: it runs during attach, at
// startup, and every failure it can report is a wiring mistake that will still be there after a
// restart. The pool is deliberately not checked — it may be nil because the database was unreachable
// at startup, which is a transient condition the service is built to survive, and the handlers
// answer 503 for as long as it lasts.
// newJobService is routes_delivery.go's, and it is shared rather than copied deliberately: it is
// already written as "a job service for a domain that needs to move a job but is not `jobs`", and a
// second constructor differing only in which file it sits in is a second place for the geocoder
// decision to be made differently.
func adminHandler(d Deps) *admin.Handler {
	svc := admin.NewService(disputeLifecycle{jobs: newJobService(d)}, jobPartiesLookup{}, d.Clock)

	handler, err := admin.NewHandler(svc, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: admin handler: " + err.Error())
	}
	return handler
}

// disputeLifecycle implements admin.Jobs over the guarded transition (SHIP-57).
//
// The same seam routes_delivery.go describes working as intended: `admin` names what it needs in its
// own ports.go, `jobs` knows nothing about `admin`, and the two are joined here by a type that
// translates one vocabulary into the other. Go satisfies the interface structurally, so neither
// package imports the other.
//
// # The translation is of errors into outcomes, and of a party into an actor
//
// admin cannot call errors.Is against jobs' sentinels — that is an import — so the refusals come
// back as an admin.JobMove instead. Everything jobs treats as a refusal becomes an outcome with a
// nil error; everything else stays an error, because a failing database is not an answer.
//
// The second translation is admin.Party into jobs.ActorType. Both packages need a word for "the
// customer" and neither may use the other's, so the mapping lives here — the composition root, where
// both vocabularies are visible. **This is the only place in the service that knows the two lists
// correspond.**
type disputeLifecycle struct {
	jobs *jobs.Service
}

// MoveToDisputed runs Docs/02 §2's `Awarded through Delivered → Disputed` through the one guarded
// function, inside the caller's transaction.
//
// The actor is the complainant, recorded as the side of the job they were on — which is what Docs/02
// §1 names as the primary actors for this status ("a customer, provider, or admin has raised an
// unresolved issue") and what job_status_history records.
//
// The reason is the dispute's category. Docs/01 §3 requires one only of an administrator, so this is
// not obligatory — it is here because the alternative is a job_status_history row saying a job froze
// and not saying why, when the answer was already in hand.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. That is correct
// here: the request is made online, by a person at a screen, so there is only one clock for the act
// of freezing the job. When the *incident* happened is a different fact and is recorded on the
// dispute, where it is not mistaken for the time somebody reported it.
func (l disputeLifecycle) MoveToDisputed(
	ctx context.Context,
	r db.Runner,
	jobID, actorID uuid.UUID,
	party admin.Party,
	reason string,
) (admin.JobMove, error) {
	actor, err := actorFor(party)
	if err != nil {
		return admin.JobMoveUnrecognised, err
	}

	_, err = l.jobs.Transition(ctx, r, jobs.Move{
		JobID:  jobID,
		To:     jobs.StatusDisputed,
		Actor:  jobs.User(actor, actorID),
		Reason: reason,
	})

	switch {
	case err == nil:
		return admin.JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return admin.JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return admin.JobAlreadyDisputed, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return admin.JobNotDisputable, nil
	default:
		return admin.JobMoveUnrecognised, err
	}
}

// actorFor maps the party a complainant was on to the actor kind job_status_history records.
//
// A switch rather than a cast, even though the strings are identical today. They are two closed
// lists owned by two packages — admin.Parties has two values and jobs.ActorTypes has five — and a
// cast would compile for ever after either list changed. This is the one place the correspondence is
// asserted, so it is the one place it can be wrong, and a value with no case is an error rather than
// a silently mistyped actor.
func actorFor(party admin.Party) (jobs.ActorType, error) {
	switch party {
	case admin.PartyCustomer:
		return jobs.ActorCustomer, nil
	case admin.PartyProvider:
		return jobs.ActorProvider, nil
	default:
		return "", fmt.Errorf("cmd/api: %q is not a party a transition can be attributed to", party)
	}
}

// jobPartiesLookup implements admin.JobParties by reading the job and its accepted bid.
//
// # Why this query is here and not in a domain
//
// It spans `jobs` and `bidding`, and `admin` may import neither. The composition root is where a
// dependency between domains is visible to anyone reading how the service is wired, rather than
// buried in `admin/postgres.go` where `jobs` and `bids` would read as tables admin owns.
//
// It is the same call routes_delivery.go's acceptedBids makes, and it will move for the same reason:
// when `bidding` grows a store at SHIP-92, the accepted-bid half of this statement becomes a method
// on it. The customer half belongs to `jobs`, and the join between them belongs here regardless.
//
// # 'Accepted' is the awarded provider, and the index is why that is safe to assume
//
// uq_bids_one_accepted_per_job is partial on `status = 'Accepted'`, so the LEFT JOIN cannot multiply
// rows however many bids a job carries (SHIP-80, SHIP-91). Docs/02 §3 — "awarding a job atomically
// marks one bid accepted and all others closed" — is the statement it enforces.
type jobPartiesLookup struct{}

// PartyOn is which side of the job this account is on, if either.
//
// No lock. The transition that follows in the same transaction takes the job row FOR UPDATE and
// re-reads its status, so a job that moved in the instant between the two statements is refused
// there rather than here.
//
// A job that does not exist and a job the caller has nothing to do with are the same answer, and
// admin turns both into one 404. See admin.JobParties.
//
// **The customer is checked before the provider, and on a job where one account is both, customer
// wins.** Nothing in the platform makes that possible today — ck_users_role fixes the role at
// registration and SHIP-45's trigger keeps it fixed — so the ordering is a decision recorded rather
// than one relied on.
func (jobPartiesLookup) PartyOn(
	ctx context.Context,
	r db.Runner,
	jobID, userID uuid.UUID,
) (admin.Party, bool, error) {
	const q = `
		SELECT CASE
		           WHEN j.customer_id = $2 THEN 'customer'
		           WHEN b.provider_id = $2 THEN 'provider'
		       END
		FROM jobs j
		LEFT JOIN bids b ON b.job_id = j.id AND b.status = 'Accepted'
		WHERE j.id = $1`

	var party *string
	err := r.QueryRow(ctx, q, jobID, userID).Scan(&party)
	switch {
	case errors.Is(err, db.ErrNoRows):
		// No such job. Deliberately the same answer as "not your job" below.
		return "", false, nil
	case err != nil:
		return "", false, fmt.Errorf("cmd/api: reading who is party to %s: %w", jobID, err)
	}
	if party == nil {
		return "", false, nil
	}
	return admin.Party(*party), true, nil
}

// Compile-time proof that the two adapters satisfy the ports admin declared, which is the only place
// in the build where that can be established — admin names neither type and neither type names
// admin, so nothing else links them.
var (
	_ admin.Jobs       = disputeLifecycle{}
	_ admin.JobParties = jobPartiesLookup{}
)
