package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// The bidding domain's routes (SHIP-84 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # The route is under /jobs and the handler is in bidding, which is now the third time
//
// A URL is a client's map of the product, not a diagram of the packages behind it. Placing a bid is
// something a provider does *to a job*, so it lives under the job — the same reading that put
// SHIP-106's driver and SHIP-111's milestones there, and the same reading that put SHIP-82's
// `/v1/jobs/open` in routes_fleet.go. What decides which domain serves a route is who owns the rule,
// and the rules here are bidding's: one live offer per provider per job, and what an offer has to
// contain before anybody can act on it.
//
// `/v1/jobs/{id}/bids` is a collection under a job, which is what it will keep being: SHIP-102's
// customer comparison is a `GET` on this same path, and SHIP-101's provider list is a different
// resource — the caller's own bids across every job — rather than a filter on this one.
//
// # Why the route requires a user and not a role
//
// The bidder is whoever the token says is calling; there is no field in the request for naming
// somebody else, so the endpoint has no meaning without a credential. **The role is not enforced
// here**, for the reason routes_fleet.go gives about vehicles and for one more: whether this account
// may bid is not a role question at all. It is Docs/01 §4.3's four filters — service area, vehicle
// capability, verification state, job status — which `fleet` answers from the database. A customer
// presenting a valid token passes RequireUser and is refused by that filter, with the same 404 an
// ineligible provider gets.
//
// It is also what makes the route safe under SHIP-44's scoped idempotency: keys land in
// idem:v1:user:<subject>:<key> rather than the shared anonymous namespace, which is the gate
// CLAUDE.md holds authenticated state-changing endpoints behind and which SHIP-44 closed.
//
// # A bid is addressed under the job it was placed on, and that is one address rather than two
//
// SHIP-85 and SHIP-86 need a *member* of the collection above, and the alternative was a flat
// `/v1/bids/{id}`. It is rejected because SHIP-102's customer comparison is a `GET` on
// `/v1/jobs/{id}/bids` — so the collection is already under the job, and a member addressed anywhere
// else would be one resource with two URLs. That is the shape where a permission check gets added to
// one address and forgotten on the other.
//
// The cost is that both handlers compare the bid's `job_id` to the path and refuse a mismatch, which
// is a real check rather than a formality: without it the first half of the URL would be decorative
// and a client pairing a real bid with any job at all would succeed.
//
// **Withdrawal is a verb and not a `DELETE`**, for the reason `POST /v1/fleet/vehicles/{id}/deactivate`
// is one: nothing is deleted. Docs/01 §4.3 requires every withdrawal to be recorded and Docs/02 §4
// keeps the chain readable, so the row survives at `Withdrawn`. It is not a `PATCH` writing a status
// either — a bid's status is the platform's, and a request naming one is refused by the decoder.
//
// # Countering is a verb under the offer being answered, and one route serves both parties
//
// SHIP-87. Docs/02 §4 gives the two directions in two sentences that describe one act — "a customer
// counter-offer supersedes the prior provider offer. A provider counter-offer supersedes the prior
// customer offer" — so there is one endpoint and the platform works out which side the caller is on.
// Two routes would have been two authorisation rules to keep in step, and they would have differed
// the first time one of them was corrected.
//
// It names the offer being answered rather than posting to the collection, because a counter *answers
// a particular offer*: two counters against one offer are then a race the platform can see, rather
// than two independent creations that only collide at an index.
//
// **`RequireUser` and not a role, and this is where that choice pays.** The two callers are told apart
// by the database — `bids.provider_id` for the provider, `jobs.customer_id` for the customer — and a
// role claim in a token is evidence about the token rather than the fact. A customer whose token
// claims `provider` is still recognised as the customer of their own job, which is what CLAUDE.md
// means by no authorisation decision on the device.
//
// # And the history is a `GET` under the same offer
//
// SHIP-88's "full chain remains readable". It is deliberately **not** `GET /v1/jobs/{id}/bids`, which
// this file reserved for SHIP-102's customer comparison at SHIP-84 and which is a different resource
// with a different privacy rule — every provider's offer side by side, where this is one negotiation's.
// Read-only, so the idempotency middleware lets it through untouched and it carries no key.
//
// # The award is a verb on the **job**, and the bid it accepts travels in the body
//
// SHIP-92, and it is the one route here that does not sit under an offer. Docs/09's *Done when* names
// `POST /v1/jobs/{id}/award`, and the shape follows what the act is: the job moves — exactly once,
// through the guarded transition — and the bid is what the customer selected. `withdraw` and
// `counter` are acts on an offer and leave the job where it is; this is the other kind, and putting
// it under `/bids/{bid_id}` would have made the two look alike.
//
// **`RequireUser` and not a role, for the fourth time and with the most riding on it.** Docs/02 §3
// gives the award to the customer alone, and which account that is is a column — `jobs.customer_id` —
// rather than a claim in a token. A provider presenting a token that says `customer` is refused by
// the database with the 404 a job that does not exist gets.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/award",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Award() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/bids",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Place() },
		},
		Route{
			Method:  http.MethodPatch,
			Pattern: "/jobs/{id}/bids/{bid_id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Revise() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/bids/{bid_id}/withdraw",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Withdraw() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs/{id}/bids/{bid_id}/counter",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).Counter() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/jobs/{id}/bids/{bid_id}/history",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return biddingHandler(d).History() },
		},
	)
}

// biddingHandler builds the domain's handler from what Deps already carries.
//
// Nothing bidding needs is missing from Deps: the clock and the pool, which is the same short list
// fleet needs. That is the test Docs/10 §9.2 sets for whether a domain has been written the way the
// shared-surface rules ask — no field had to be added to a shared struct and no line to its literal
// in main.go.
//
// It panics for the same reason jobsHandler does: it runs during attach, at startup, and every
// failure it can report is a wiring mistake that will still be there after a restart. The pool is
// deliberately not checked — it may be nil because the database was unreachable at startup, which is
// a transient condition the service is built to survive, and the handlers answer 503 for as long as
// it lasts.
//
// # This is where two domains meet, and the meeting costs one argument
//
// `bidding` needs to know whether a provider may bid on a job, and that is `fleet`'s answer:
// SHIP-81's eligibility filter is one SQL predicate over four tables, and its own header names
// SHIP-84 as the reader it built `EligibleFor` for. `bidding` declares what it needs in its own
// ports.go and imports no domain; `fleet` knows nothing about `bidding`; the two are joined here,
// which is the only place they may be (Docs/06 §4.1, CLAUDE.md).
//
// **The eligibility port needed no adapter type, and that is worth noticing rather than assuming.**
// routes_delivery.go has two — `jobLifecycle` and `acceptedBids` — because a transition has four
// outcomes that cannot be carried across without naming a `jobs` sentinel. That port answers a bool,
// which has no vocabulary to translate, so `*fleet.Service` satisfies it structurally and the wiring
// is one argument and the assertion below.
//
// The other two do need types, and for two different reasons: `negotiatedJobs` translates `jobs`'
// sentinels into bools (SHIP-87), and `awardableJobs` carries an outcome and takes a lock (SHIP-92).
//
// A second `fleet.Service` rather than the one `fleetHandler` builds, for the reason
// `newJobService` builds a second `jobs.Service`: both are pure functions of the clock, neither
// holds state, and sharing one would be a dependency between two route files that today know nothing
// about each other.
func biddingHandler(d Deps) *bidding.Handler {
	svc := bidding.NewService(
		events.NewOutbox(),
		fleet.NewService(d.Clock),
		negotiatedJobs{jobs: newJobService(d)},
		awardableJobs{jobs: newJobService(d)},
		d.Clock,
	)

	handler, err := bidding.NewHandler(svc, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: bidding handler: " + err.Error())
	}
	return handler
}

// negotiatedJobs implements bidding.Negotiation over the customer's own view of a job (SHIP-87).
//
// The second seam this file joins, and the first that needed a type. `fleet`'s answer is a bool with
// no vocabulary to translate, so `*fleet.Service` satisfies its port structurally; `jobs` answers with
// a `Job` and two sentinels, and turning those into two bools is exactly the translation
// routes_delivery.go's `jobLifecycle` does for a transition. Neither package names the other.
//
// # Both methods go through jobs.Service.Job, which is the ownership check itself
//
// That method reads the row and compares `customer_id` in Go, raising `jobs.ErrNotJobOwner` for
// somebody else's job and `jobs.ErrJobNotFound` for nobody's. **Both become `false` here**, which is
// the same collapse `bidding` makes on the wire and `fleet` makes for eligibility: distinguishing them
// would take a second query whose only product is the knowledge that some identifier exists.
//
// Anything else stays an error, because a failing database is not an answer.
type negotiatedJobs struct {
	jobs *jobs.Service
}

// CustomerOf reports whether this account owns this job, whatever its status.
//
// Status is deliberately not consulted. SHIP-88 keeps a negotiation readable to its parties after the
// job is awarded, cancelled or completed — a record is at its most useful once the work is over.
func (n negotiatedJobs) CustomerOf(ctx context.Context, r db.Runner, userID, jobID uuid.UUID) (bool, error) {
	_, err := n.jobs.Job(ctx, r, userID, jobID)
	return n.answer(err)
}

// AwardableBy reports whether this customer's job could still be awarded.
//
// # `jobs.Permitted(status, Awarded)` rather than a list of statuses, and that is the whole point of
// this method
//
// Docs/02 §2's transition table is the authority, `jobs` holds it in exactly one place and exports the
// question, and `Open` and `Negotiating` are the two statuses that can reach `Awarded`. Writing those
// two strings here instead would have put a **third** copy of that list in the service — after `jobs`'
// own table and `fleet.biddableStatuses` — and a third copy is the one nobody remembers to correct.
//
// It also states the rule in the form the product actually has it. A customer countering is not asking
// whether the job is biddable; they are asking whether this negotiation can still end in an award. The
// two select the same statuses today and they are different questions, and this one is the one whose
// answer follows Docs/02 §2 automatically if that document ever changes.
func (n negotiatedJobs) AwardableBy(
	ctx context.Context,
	r db.Runner,
	customerID, jobID uuid.UUID,
) (bool, error) {
	job, err := n.jobs.Job(ctx, r, customerID, jobID)
	if owned, err := n.answer(err); err != nil || !owned {
		return false, err
	}
	return jobs.Permitted(job.Status, jobs.StatusAwarded), nil
}

// answer is the translation, in one place: what `jobs` treats as a refusal becomes `false` with a nil
// error, and everything else stays an error.
//
// Written once rather than twice for the reason `jobLifecycle.move` is written once — a third method
// added later must not be able to treat `jobs.ErrNotJobOwner` differently from these two, and that
// drift would be invisible: both methods would still compile, still pass their own tests, and answer
// a stranger with a refusal on one path and a 500 on the other.
func (negotiatedJobs) answer(err error) (bool, error) {
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, jobs.ErrNotJobOwner), errors.Is(err, jobs.ErrJobNotFound):
		return false, nil
	default:
		return false, fmt.Errorf("cmd/api: reading a job for a counter-offer: %w", err)
	}
}

// awardableJobs implements bidding.Awarding, which is the port the award writes through (SHIP-92).
//
// # Why the lock is a statement here rather than a method on jobs.Service
//
// `jobs` has `lockJob` and it is unexported, reachable only from its own `Transition`. Exporting it
// would mean editing another domain from a branch that owns `bidding`, and it would widen that
// domain's surface for one caller. So the statement is written here, in the composition root — the
// same call `acceptedBids` in routes_delivery.go makes in the opposite direction, with the same
// reasoning recorded there: this is where a dependency between two domains is *visible to anybody
// reading how the service is wired*, rather than buried in a domain's postgres.go where it would read
// as a table that domain owns.
//
// **The trade is deliberate and it is not free.** Two statements now know that a job has a
// `customer_id` and a `status`: this one, and `jobs`' own store. What keeps them honest is that
// nothing here interprets the status — `jobs.Permitted` is asked, which is Docs/02 §2's table in the
// one place that holds it — and that the transition itself still goes through the guarded function
// below. When `jobs` grows an exported lock for a caller in another domain, this method becomes a
// call to it and nothing else changes.
//
// # It takes the lock even to refuse
//
// A read that answered "awardable" and let go would be answering about a job that can be cancelled,
// disputed or awarded to somebody else before `bidding` writes anything. Docs/11 §3's SHIP-88 entry
// puts this first in the ordering for that reason: it is the outermost lock, and the only one shared
// with the status guard and `job_status_history`.
type awardableJobs struct {
	jobs *jobs.Service
}

// LockForAward holds the job row and reports whether this customer may still award it.
//
// The two refusals are deliberately different values rather than one. "Not yours or no such job"
// leads a client to a 404 and "the job has moved on" to a 409 naming what became of it, and a bool
// would have made `bidding` choose one answer for both — see bidding.JobAward.
//
// `customer_id` is compared here rather than folded into the `WHERE` clause, which is the same choice
// `jobs.Service.Job` makes and for the same reason: a query scoped to the caller that returns nothing
// cannot say whether the job was somebody else's or nobody's, and only one of those is a caller
// probing for identifiers. Both are one answer on the wire; keeping them apart costs a comparison and
// buys a fact a test can assert.
func (a awardableJobs) LockForAward(
	ctx context.Context,
	r db.Runner,
	customerID, jobID uuid.UUID,
) (bidding.JobAward, error) {
	const q = `SELECT customer_id, status FROM jobs WHERE id = $1 FOR UPDATE`

	var (
		owner  uuid.UUID
		status jobs.Status
	)
	switch err := r.QueryRow(ctx, q, jobID).Scan(&owner, &status); {
	case errors.Is(err, db.ErrNoRows):
		return bidding.JobAwardNoSuchJob, nil
	case err != nil:
		return bidding.JobAwardUnrecognised, fmt.Errorf("cmd/api: holding %s for award: %w", jobID, err)
	}

	if owner != customerID {
		return bidding.JobAwardNoSuchJob, nil
	}

	// jobs.Permitted rather than a list of statuses written here. Docs/02 §2's table is authoritative
	// and `jobs` holds it in exactly one place; the two statuses that can reach 'Awarded' today are
	// Open and Negotiating, and writing them out would be a copy that stops agreeing with the
	// document the first time somebody corrects one of them. negotiatedJobs.AwardableBy makes the
	// same call for the counter's version of this question.
	if !jobs.Permitted(status, jobs.StatusAwarded) {
		return bidding.JobAwardNotPermitted, nil
	}
	return bidding.JobAwardable, nil
}

// MoveToAwarded runs Docs/02 §2's `Open / Negotiating → Awarded` through the one guarded function,
// inside the caller's transaction.
//
// The actor is the **customer**, which is Docs/02 §3's first transition control — "only the customer
// can award a job" — and what `job_status_history` records. No reason: Docs/01 §3 requires one only
// of an administrator.
//
// RecordedAt is left zero, so jobs.Transition uses the platform's clock for both. An award is made
// online, by a customer looking at the screen, so there is only one clock — unlike a milestone, where
// routes_delivery.go carries the actor's separately.
//
// The translation is of errors into outcomes, exactly as `jobLifecycle.move` does it: everything
// `jobs` treats as a refusal becomes an outcome with a nil error, and everything else stays an error
// because a failing database is not an answer.
func (a awardableJobs) MoveToAwarded(
	ctx context.Context,
	r db.Runner,
	jobID, customerID uuid.UUID,
) (bidding.JobAward, error) {
	_, err := a.jobs.Transition(ctx, r, jobs.Move{
		JobID: jobID,
		To:    jobs.StatusAwarded,
		Actor: jobs.User(jobs.ActorCustomer, customerID),
	})

	switch {
	case err == nil:
		return bidding.JobAwarded, nil
	case errors.Is(err, jobs.ErrJobNotFound):
		return bidding.JobAwardNoSuchJob, nil
	case errors.Is(err, jobs.ErrAlreadyInStatus), errors.Is(err, jobs.ErrTransitionNotPermitted):
		// One outcome for both, unlike delivery's port, and the difference is what the caller does
		// with it. A milestone arriving late is a record worth keeping (SHIP-112), so `delivery` has
		// to tell "already been there" from "not there yet". An award has no such case: a job already
		// at 'Awarded' and a job that can never reach it are both "this award did not happen", and
		// the accepted bid the customer is holding is what tells them which.
		return bidding.JobAwardNotPermitted, nil
	default:
		return bidding.JobAwardUnrecognised, err
	}
}

// Compile-time proof that the three adapters satisfy the ports bidding declared.
//
// This is the only place in the build where that can be established — `bidding` names neither `fleet`
// nor `jobs`, and neither names `bidding`, so nothing else links them. If any of them ever part
// company, these lines are what say so, at compile time and in the file whose job it is to know about
// all three.
var (
	_ bidding.Eligibility = (*fleet.Service)(nil)
	_ bidding.Negotiation = negotiatedJobs{}
	_ bidding.Awarding    = awardableJobs{}
)
