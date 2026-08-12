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
func init() {
	register(
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
