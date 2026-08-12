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
// # Why two of these routes are not under /fleet
//
// SHIP-82 and SHIP-83 add `GET /v1/jobs/open` and `GET /v1/jobs/open/{id}`, and they are declared
// here rather than in routes_jobs.go. Routes are **declared, not registered**, so the file follows
// the domain that answers the request rather than the first segment of the path: `fleet` owns the
// eligibility filter, so `fleet` owns the endpoints that serve it. Declaring them in the jobs file
// would have put a `fleet.Handler` in a file the jobs track edits every wave, which is the shared
// surface this whole arrangement exists to avoid.
//
// The path is the client's view and it is right: the resource is a job. `/v1/jobs/open` is the
// collection of jobs offered to the calling provider and `/v1/jobs/open/{id}` is one member of it.
// net/http prefers the more specific pattern, so this coexists with `/v1/jobs/{id}` without either
// file knowing about the other — and the two are deliberately different resources rather than one
// endpoint returning two shapes: `/v1/jobs/{id}` is the customer's own job and carries the budget
// that `Docs/01` §4.3 forbids a provider ever seeing.
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
			Pattern: "/jobs/open",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return fleetHandler(d).OpenJobs() },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/jobs/open/{id}",
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
