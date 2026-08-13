package main

import (
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
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
// SHIP-86's withdrawal takes the same address with a verb under it, for the same reason.
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
	svc := bidding.NewService(fleet.NewService(d.Clock), d.Clock)

	handler, err := bidding.NewHandler(svc, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: bidding handler: " + err.Error())
	}
	return handler
}

// Compile-time proof that fleet's eligibility filter is what bidding's port asks for.
//
// This is the only place in the build where that can be established — `bidding` does not name
// `fleet` and `fleet` does not name `bidding`, so nothing else links them. If the two ever part
// company, this line is what says so, at compile time and in the file whose job it is to know about
// both.
var _ bidding.Eligibility = (*fleet.Service)(nil)
