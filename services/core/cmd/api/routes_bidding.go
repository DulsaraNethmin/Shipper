package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
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

func init() {
	register(
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
// **No adapter type was needed, and that is worth noticing rather than assuming.** routes_delivery.go
// has two — `jobLifecycle` and `acceptedBids` — because a transition has four outcomes that cannot
// be carried across without naming a `jobs` sentinel. This port answers a bool, which has no
// vocabulary to translate, so `*fleet.Service` satisfies it structurally and the wiring is one
// argument and the assertion below.
//
// A second `fleet.Service` rather than the one `fleetHandler` builds, for the reason
// `newJobService` builds a second `jobs.Service`: both are pure functions of the clock, neither
// holds state, and sharing one would be a dependency between two route files that today know nothing
// about each other.
func biddingHandler(d Deps) *bidding.Handler {
	svc := bidding.NewService(
		fleet.NewService(d.Clock),
		negotiatedJobs{jobs: newJobService(d)},
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

// Compile-time proof that the two adapters satisfy the ports bidding declared.
//
// This is the only place in the build where that can be established — `bidding` names neither `fleet`
// nor `jobs`, and neither names `bidding`, so nothing else links them. If any of them ever part
// company, these lines are what say so, at compile time and in the file whose job it is to know about
// all three.
var (
	_ bidding.Eligibility = (*fleet.Service)(nil)
	_ bidding.Negotiation = negotiatedJobs{}
)
