package main

import (
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/fleet"
)

// The fleet domain's routes (SHIP-78 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # Why every route requires a user
//
// A vehicle belongs to a provider. There is no field in any request for naming which one — the
// owner is whoever the token says is calling — so no endpoint here has a meaning without a
// credential. That is also what makes them safe under SHIP-44's scoped idempotency: keys land in
// idem:v1:<subject>:<key> rather than the shared anonymous namespace, which is the gate CLAUDE.md
// holds authenticated state-changing endpoints behind and which SHIP-44 closed.
//
// The role is not enforced here. A customer presenting a valid token passes RequireUser and is
// refused by the domain, which reads users.role rather than trusting the token's claim — 000300
// says the provider-account rule is enforced where a vehicle is added, and that is one place rather
// than two that can disagree.
//
// # Why deactivate and reactivate are verbs rather than a field
//
// Taking a vehicle out of service is an intent, and the platform decides what the state becomes.
// A `PATCH` carrying `{"active": false}` would be a client naming a state — and there is no
// `DELETE`, because a vehicle is named by the bid that won a job and by the delivery that followed
// it, so the row survives the provider's interest in it (Docs/05 §3.1, Docs/10 §3.3).
//
// # Why the declaration is one document rather than a collection
//
// SHIP-79 adds `/fleet/profile`, and it is a `GET` and a `PATCH` with no `{id}` beneath them. The
// provider's service area and specialties are sets they edit as sets — the client renders chips and
// sends the set back — so there is no `/fleet/profile/states/{state}` to `DELETE`. Two operations
// over one collection is where a client and a server stop agreeing about what is in it.
//
// # The provider's feed is under /fleet, and SHIP-83a moved it there
//
// SHIP-82 and SHIP-83 declared the feed as `GET /v1/jobs/open` and `GET /v1/jobs/open/{id}`, on the
// argument that the path is the client's view and the resource is a job. The argument was right
// about the resource and wrong about the cost, and **SHIP-83a reverses it**: the feed is now
// `GET /v1/fleet/jobs` and `GET /v1/fleet/jobs/{id}`, and the old paths are gone rather than
// aliased.
//
// **What the old shape cost was the whole `/v1/jobs/{id}/<literal>` space.** `/v1/jobs/open/{id}`
// puts a literal where an identifier goes, so it and any four-segment `GET /v1/jobs/{id}/<literal>`
// both match `/v1/jobs/open/<literal>` with neither pattern more specific — segment 3 favours one
// and segment 4 the other — and Go's `ServeMux` panics at registration rather than choosing. Not a
// 404 at request time: the process does not start. Four tickets paid a workaround for it before it
// was worth fixing — SHIP-115 took `/delivery/proof`, SHIP-115a `/delivery/detail` and
// `/delivery/milestones`, SHIP-101a went to `/v1/fleet/bids` rather than under the job at all, and
// SHIP-102a took `/bids/received`. Each is defensible alone; the set is a shape nobody chose.
//
// **`/v1/fleet/jobs` is the honest name rather than merely a free one.** `/fleet` is not a resource
// prefix here — it is the caller's *role*, which is exactly what the collection is scoped by. There
// is no such thing as "the open jobs" in general: the feed is the platform's eligibility decision
// about one provider, so two providers reading the same path see different sets, and `/v1/jobs/open`
// suggested a public shelf that does not exist. `/v1/jobs/{id}` remains the *customer's* job,
// carrying the budget `Docs/01` §4.3 forbids a provider ever seeing, and the two are now different
// first segments rather than two readings of one.
//
// **Nothing under `/v1/fleet/jobs` may put a literal in the `{id}` slot either**, or the same trap
// is simply reproduced one prefix down — the reason SHIP-96a widens `/v1/fleet/jobs/{id}` rather
// than adding `/v1/fleet/jobs/mine/{id}` beside it. TestFourSegmentJobLiteralsCanBeRegistered in
// routes_jobsegment_test.go is what holds that: it attaches the real route table plus a
// four-segment `GET /v1/jobs/{id}/<literal>` to a mux, and fails if the pair is refused.
//
// They are declared here rather than in routes_jobs.go for the reason they always were. Routes are
// **declared, not registered**, so the file follows the domain that answers the request rather than
// the first segment of the path: `fleet` owns the eligibility filter, so `fleet` owns the endpoints
// that serve it. Declaring them in the jobs file would have put a `fleet.Handler` in a file the jobs
// track edits every wave, which is the shared surface this whole arrangement exists to avoid.
func init() {
	register(
		Route{
			Method:  http.MethodGet,
			Pattern: "/fleet/profile",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).Profile() },
		},
		Route{
			Method:  http.MethodPatch,
			Pattern: "/fleet/profile",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).Declare() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/fleet/vehicles",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).List() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/fleet/vehicles",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).Add() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/fleet/vehicles/{id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).Detail() },
		},
		Route{
			Method:  http.MethodPatch,
			Pattern: "/fleet/vehicles/{id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).Update() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/fleet/vehicles/{id}/deactivate",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).Deactivate() },
		},
		Route{
			Method:  http.MethodPost,
			Pattern: "/fleet/vehicles/{id}/reactivate",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).Reactivate() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/fleet/jobs",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).OpenJobs() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/fleet/jobs/{id}",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).OpenJob() },
		},
	)
}

// fleetHandler builds the domain's handler from what Deps already carries.
//
// Nothing fleet needs is missing from Deps, and it needs less than any domain so far: the clock and
// the pool. That is the test of whether a domain has been written the way Docs/10 §9.2 asks — no
// field had to be added to a shared struct.
//
// It panics for the same reason jobsHandler does: it runs during attach, at startup, and every
// failure it can report is a wiring mistake that will still be there after a restart. The pool is
// deliberately not checked — it may be nil because the database was unreachable at startup, which is
// a transient condition the service is built to survive, and the handlers answer 503 for as long as
// it lasts.
func fleetHandler(d Deps) *fleet.Handler {
	handler, err := fleet.NewHandler(fleet.NewService(d.Clock), d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: fleet handler: " + err.Error())
	}
	return handler
}
