package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// The delivery domain's routes (SHIP-106 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # This is also where three domains meet, which is the only place they may
//
// `delivery` declares two ports and imports neither `jobs` nor `bidding` (Docs/06 §4.1). The
// implementations are below, and they are the reason this file is longer than routes_jobs.go: the
// composition root is doing the one thing only it can do, which is knowing about more than one
// domain at a time.
//
// # The route is under /jobs and the handler is in delivery, and that is not a contradiction
//
// A URL is a client's map of the product, not a diagram of the packages behind it. Assigning a
// driver is something a client does *to a job*, so it lives under the job — the same reading that
// puts SHIP-111's milestones there. What decides which domain serves it is who owns the rule, and
// the rule here is delivery's: one live driver per job, and a status move that only the awarded
// provider may ask for.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/driver",
			Group:   GroupV1,

			// RequireUser, and deliberately **not** RequireDriverToken. The caller is the
			// provider, authenticated the ordinary way. The driver's job-scoped token is a
			// separate system that grants exactly one job and cannot be exchanged for a
			// session in either direction (Docs/10 §5).
			//
			// **This route mints one (SHIP-107) and still does not accept one**, which is
			// exactly the right pair: the provider is handed the link to forward, and the
			// route that spends it is the third one below (SHIP-108).
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).AssignDriver() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/milestones",
			Group:   GroupV1,

			// RequireUser again, and the reason bears repeating because this is the route
			// whose name most invites the other answer. A driver records milestones from a
			// link-authenticated portal — but **that route is SHIP-121's, not this one**.
			// This is the awarded provider recording their own delivery, checked against the
			// accepted bid. When the driver's own milestone route arrives it will declare
			// RequireDriverToken and sit under /driver, beside the read below.
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).RecordMilestone() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/driver/jobs/{id}",
			Group:   GroupV1,

			// **The first route in the service served on the driver's credential**
			// (SHIP-108), and the one that closes SHIP-15m's seam: filling in
			// newDriverTokenGuard maps this class, and until something did, declaring it
			// here would have stopped the process at startup rather than serving the route
			// open. That failure direction is unchanged — it is what a route declaring
			// RequireAdmin still gets today.
			//
			// # Why /driver/jobs/{id} rather than a second method on /jobs/{id}
			//
			// The manifest allows one auth class per method, so `GET /jobs/{id}` under a
			// driver token was available and is the wrong shape twice over. It would put two
			// credential systems on one path, where a client that presented the wrong one
			// gets an answer that reads as a permissions problem; and it would claim a URL
			// the customer's and the provider's own job view already own (SHIP-65).
			// `/driver/...` says whose surface this is, which is what SHIP-120 and SHIP-121
			// extend rather than negotiate with.
			//
			// # The {id} is checked, not decorative
			//
			// delivery.RequireDriverToken compares it with the job inside the token and
			// refuses a mismatch before the handler runs. That comparison is the auth class,
			// so a route declaring the class cannot skip it — see internal/delivery's
			// driverauth.go for why that is the design rather than a convenience.
			Auth:    RequireDriverToken,
			Handler: func(d Deps) http.Handler { return deliveryHandler(d).DriverJob() },
		},
	)
}

// deliveryHandler builds the domain's handler from what Deps already carries.
//
// Nothing delivery needs is missing from Deps, which is the test Docs/10 §9.2 sets for whether a
// domain has been written the way the shared-surface rules ask: the job service, the outbox writer,
// the two ports and — since SHIP-107 — the driver token issuer are all pure functions of the pool,
// the clock and the configuration, so no field had to be added to a shared struct and no line to its
// literal in main.go.
//
// It panics for the same reason jobsHandler does: it runs during attach, at startup, and every
// failure it can report is a wiring mistake that will still be there after a restart. The pool is
// deliberately not checked — it may be nil because the database was unreachable at startup, which is
// a transient condition the service is built to survive, and the handlers answer 503 for as long as
// it lasts.
func deliveryHandler(d Deps) *delivery.Handler {
	svc := delivery.NewService(
		jobLifecycle{jobs: newJobService(d)},
		acceptedBids{},
		driverTokenIssuer(d),
		d.Clock,
	)

	handler, err := delivery.NewHandler(svc, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: delivery handler: " + err.Error())
	}
	return handler
}

// driverTokenIssuer builds the signer of the driver's job-scoped link (SHIP-107).
//
// # Why this is a panic and newDriverTokenGuard is an error
//
// The two look like they should agree and they should not. That constructor is called from main.go
// before the router exists, where a configuration failure can be *returned* and reported beside every
// other one; this runs during attach, from a Handler closure that has nowhere to put an error. Both
// stop the process, which is the outcome that matters: a keyset that cannot be built will still be
// unbuildable after the next restart, and a service that came up unable to sign a driver token would
// answer every assignment with a 500 while reporting itself healthy.
//
// config.Load has already refused a short key, an active identifier naming no key, and a keyset
// sharing a secret with identity's — so reaching either failure below means configuration produced
// something it validates against, which is a defect in this package rather than in a deployment.
func driverTokenIssuer(d Deps) *delivery.DriverTokenIssuer {
	keys, err := delivery.NewKeyset(d.Config.Delivery.DriverTokenKeys, d.Config.Delivery.DriverTokenActiveKID)
	if err != nil {
		panic("cmd/api: driver token keyset: " + err.Error())
	}

	issuer, err := delivery.NewDriverTokenIssuer(keys, d.Config.Delivery.DriverTokenTTL, d.Clock)
	if err != nil {
		panic("cmd/api: driver token issuer: " + err.Error())
	}
	return issuer
}

// newJobService builds a job service for a domain that needs to move a job but is not `jobs`.
//
// No geocoder, and that is the same call cmd/worker's expiry task makes: an assignment moves a job
// that already exists and never touches an address, and jobs.NewService is explicit that a nil
// Geocoder is a supported state rather than a broken one. Handing this path a maps vendor would be
// an outbound dependency nothing on it has a reason for.
func newJobService(d Deps) *jobs.Service {
	return jobs.NewService(events.NewOutbox(), d.Clock, nil)
}

// jobLifecycle implements delivery.Jobs over the guarded transition (SHIP-57).
//
// This is the seam Docs/06 §4.1 describes working as intended: `delivery` names what it needs in its
// own ports.go, `jobs` knows nothing about `delivery`, and the two are joined here by a type that
// translates one vocabulary into the other. Go satisfies the interface structurally, so neither
// package imports the other.
//
// # The translation is of errors into outcomes, and that is the whole job
//
// delivery cannot call errors.Is against jobs' sentinels — that is an import — so the refusals come
// back as a delivery.JobMove instead. Everything jobs treats as a refusal becomes an outcome with a
// nil error; everything else stays an error, because a failing database is not an answer.
type jobLifecycle struct {
	jobs *jobs.Service
}

// MoveToDriverAssigned runs Docs/02 §2's `Awarded → Driver assigned` through the one guarded
// function, inside the caller's transaction.
//
// The actor is the provider, which is what Docs/02 §1 names as the primary actor for this status and
// what job_status_history records. The reason is empty on purpose: Docs/01 §3 requires one only of an
// administrator, and a provider nominating their own driver owes nobody an explanation.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. That is correct
// here and would not be for a milestone: this request is made online, by a provider looking at the
// screen, so there is only one clock. The three methods below carry the actor's separately.
func (l jobLifecycle) MoveToDriverAssigned(
	ctx context.Context,
	r db.Runner,
	jobID, providerID uuid.UUID,
) (delivery.JobMove, error) {
	return l.move(ctx, r, jobs.Move{
		JobID: jobID,
		To:    jobs.StatusDriverAssigned,
		Actor: jobs.User(jobs.ActorProvider, providerID),
	})
}

// The three moves a recorded milestone can make (SHIP-111).
//
// One method per move rather than one taking a status, which delivery/ports.go argues at length and
// this file is where the argument is cashed: **the four `jobs.Status` values below appear in cmd/api
// and nowhere else.** `delivery` names milestones, this names statuses, and the mapping between the
// two vocabularies lives in the composition root where both are visible.
//
// `Delivered` is deliberately absent. Docs/01 §4.4 makes photo proof or a recorded exception the
// condition of recording one, neither can be captured until SHIP-114…SHIP-116, and a method here
// would be a way to reach that status without either. The delivery domain refuses it in front of
// this (delivery.ErrProofRequired); having no method behind it as well means the refusal cannot be
// removed by editing one file.
//
// RecordedAt is the actor's clock, passed through to job_status_history's actor_recorded_at. The
// transition and the milestone that caused it are one act, and the pair of rows they leave must
// agree about when the actor says it happened (Docs/02 §3.1). Each table stamps its own arrival
// time from the database's clock, so the two are never collapsed.

func (l jobLifecycle) MoveToEnRouteToPickup(
	ctx context.Context,
	r db.Runner,
	jobID, providerID uuid.UUID,
	recordedAt time.Time,
) (delivery.JobMove, error) {
	return l.move(ctx, r, jobs.Move{
		JobID:      jobID,
		To:         jobs.StatusEnRouteToPickup,
		Actor:      jobs.User(jobs.ActorProvider, providerID),
		RecordedAt: recordedAt,
	})
}

func (l jobLifecycle) MoveToPickedUp(
	ctx context.Context,
	r db.Runner,
	jobID, providerID uuid.UUID,
	recordedAt time.Time,
) (delivery.JobMove, error) {
	return l.move(ctx, r, jobs.Move{
		JobID:      jobID,
		To:         jobs.StatusPickedUp,
		Actor:      jobs.User(jobs.ActorProvider, providerID),
		RecordedAt: recordedAt,
	})
}

func (l jobLifecycle) MoveToInTransit(
	ctx context.Context,
	r db.Runner,
	jobID, providerID uuid.UUID,
	recordedAt time.Time,
) (delivery.JobMove, error) {
	return l.move(ctx, r, jobs.Move{
		JobID:      jobID,
		To:         jobs.StatusInTransit,
		Actor:      jobs.User(jobs.ActorProvider, providerID),
		RecordedAt: recordedAt,
	})
}

// move is the translation itself, in one place: what `jobs` treats as a refusal becomes a
// delivery.JobMove with a nil error, and everything else stays an error because a failing database
// is not an answer.
//
// Written once rather than four times because a fifth move must not be able to translate
// jobs.ErrAlreadyInStatus differently from the four before it. That is exactly the drift that would
// be invisible: every one of these methods would still compile, still pass its own test, and answer
// a repeated milestone with a refusal on one path and an absorption on another.
//
// # One refusal becomes two answers, and SHIP-112 is why
//
// jobs.ErrTransitionNotPermitted says only that Docs/02 §2 has no row from where the job stands. It
// does not say which way the refused move was pointing, and it should not: the transition table is a
// set of permitted edges and has no notion of forwards. Docs/02 §3.1 does, and it turns on exactly
// one thing — whether "a later transition has already been recorded" — so that is what is asked
// here, of the same domain, in the same transaction.
func (l jobLifecycle) move(ctx context.Context, r db.Runner, m jobs.Move) (delivery.JobMove, error) {
	_, err := l.jobs.Transition(ctx, r, m)

	switch {
	case err == nil:
		return delivery.JobMoved, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return delivery.JobNotFound, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus):
		return delivery.JobAlreadyInStatus, nil
	case errors.Is(err, jobs.ErrTransitionNotPermitted):
		return l.refusal(ctx, r, m)
	default:
		return delivery.JobMoveUnrecognised, err
	}
}

// refusal tells a job that has already been past this status from one that has not reached it
// (SHIP-112).
//
// # It reads history rather than reasoning about the table
//
// The question could have been asked of Docs/02 §2's graph — "can the job still reach this status by
// any permitted sequence?" — and that answer would be wrong in a way nobody would notice for a while.
// A job cancelled or disputed at Awarded can no longer reach 'Picked up' either, and it has not moved
// *past* the pickup; it has lost the delivery to something else. Docs/02 §3.1 keeps those two apart
// and gives them different handling — the second is the administrative-conflict bullet, which is
// SHIP-113 — so the test is the recorded fact, not the reachable set.
//
// # Why the composition root and not the domain
//
// `delivery` may not name a jobs.Status (Docs/06 §4.1), and this question is about one. Everything
// below is already visible here: the target status is in the Move this file built, and
// jobs.Service.History is that domain's own reader. The domain gets an answer in its own vocabulary
// and still holds no second copy of the transition table.
//
// The read runs in the caller's transaction, after Transition has taken the job row FOR UPDATE, so
// nothing can move the job between the refusal and this question.
//
// An error here is reported rather than resolved to either answer. Guessing "not assignable" would
// discard a driver's late milestone on a failing database, and guessing "already past" would commit
// one that should have been refused.
func (l jobLifecycle) refusal(ctx context.Context, r db.Runner, m jobs.Move) (delivery.JobMove, error) {
	history, err := l.jobs.History(ctx, r, m.JobID)
	if err != nil {
		return delivery.JobMoveUnrecognised, fmt.Errorf(
			"cmd/api: reading whether %s has already been %s: %w", m.JobID, m.To, err)
	}

	for _, change := range history {
		if change.To == m.To {
			return delivery.JobAlreadyPast, nil
		}
	}
	return delivery.JobNotAssignable, nil
}

// acceptedBids implements delivery.Awards by reading the job's accepted bid.
//
// # Why this query is here and not in a domain
//
// It belongs to `bidding`, which is `doc.go` and `model.go` today: SHIP-80 built the table and the
// one-accepted-bid index, and SHIP-92 — the award transaction, a single-owner branch that never
// parallelises (Docs/11 §8) — is what gives that domain a store. Until it does, this is the honest
// place for the statement: the composition root, where a dependency between two domains is visible
// to anyone reading how the service is wired, rather than buried in `delivery/postgres.go` where it
// would read as a table delivery owns.
//
// **When `bidding` grows its store, this type is deleted and a method on that store is passed
// instead.** Nothing in `delivery` changes, which is the point of the port.
//
// # 'Accepted' is the awarded provider, and the index is why that is safe to assume
//
// uq_bids_one_accepted_per_job is partial on `status = 'Accepted'`, so this query cannot return two
// rows however many bids a job carries (SHIP-80, SHIP-91). Docs/02 §3 — "awarding a job atomically
// marks one bid accepted and all others closed" — is the statement it enforces.
type acceptedBids struct{}

// AwardedProvider is the provider whose bid the customer accepted, if any.
//
// No lock. The award is checked again by the transition that follows it in the same transaction —
// jobs.Transition takes the job row FOR UPDATE and re-reads its status — so a job awarded to
// somebody else in the instant between the two statements is refused there rather than here.
//
// A job that does not exist and a job nobody has been awarded are the same answer, and delivery
// turns both into one 404. See delivery.Awards.
func (acceptedBids) AwardedProvider(
	ctx context.Context,
	r db.Runner,
	jobID uuid.UUID,
) (uuid.UUID, bool, error) {
	const q = `SELECT provider_id FROM bids WHERE job_id = $1 AND status = 'Accepted'`

	var providerID uuid.UUID
	err := r.QueryRow(ctx, q, jobID).Scan(&providerID)
	switch {
	case errors.Is(err, db.ErrNoRows):
		return uuid.Nil, false, nil
	case err != nil:
		return uuid.Nil, false, fmt.Errorf("cmd/api: reading the accepted bid on %s: %w", jobID, err)
	}
	return providerID, true, nil
}

// Compile-time proof that the two adapters satisfy the ports delivery declared, which is the only
// place in the build where that can be established — delivery names neither type and neither type
// names delivery, so nothing else links them.
var (
	_ delivery.Jobs   = jobLifecycle{}
	_ delivery.Awards = acceptedBids{}
)
