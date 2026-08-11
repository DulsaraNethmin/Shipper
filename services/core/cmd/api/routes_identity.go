package main

import (
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
)

// The identity domain's routes (SHIP-30 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to
// go into one of them is a line every concurrent branch also touches, and a badly resolved
// conflict there drops an endpoint with no compile error and no failing test.
//
// # Why every route here is Public
//
// All of them are how a caller obtains their own credentials, which is the only justification
// cmd/api's publicMutatingRoutes allow-list accepts. Registration takes a password and returns
// an account; the verification endpoints take a token or a code that was sent to the contact
// details being proved. None of them can require the credential they exist to produce.
//
// They are still behind httpx.Idempotent like every other state-changing request, and they are
// still rate limited — by their own issue rules today (SHIP-34) and by SHIP-47's token bucket
// across the whole authentication surface.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/auth/register",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return identityHandler(d).Register() },
		},
	)
}

// identityHandler builds the domain's handler from what Deps already carries.
//
// # Why this is built here rather than added to Deps
//
// Deps and its literal in main.go are the shared surfaces wave 2's three tracks would otherwise
// each grow a field on, and TestDepsCarriesExactlyWhatIsDeclared exists to make that a decision
// with a name attached. Nothing identity needs is missing: a hasher, a service and a handler are
// all pure functions of the pool, the clock and the configuration.
//
// # Why it panics
//
// It runs during attach, at startup, from a route's Handler function. Every failure it can
// report is a configuration or wiring mistake that will still be there after a restart — an
// argon2 profile outside the range this package will run, a nil clock. A service that came up
// serving registration with no password hasher would accept a password and store something
// nobody can verify against, which is worse than not starting.
//
// The pool is deliberately *not* checked. It may be nil because the database was unreachable at
// startup, which is a transient condition the service is built to survive (see the note on
// Deps); the handlers answer 503 for as long as it lasts.
func identityHandler(d Deps) *identity.Handler {
	hasher, err := identity.NewPasswordHasher(identity.Argon2Profile{
		MemoryKiB:   d.Config.Identity.Argon2.MemoryKiB,
		Iterations:  d.Config.Identity.Argon2.Iterations,
		Parallelism: d.Config.Identity.Argon2.Parallelism,
	})
	if err != nil {
		panic("cmd/api: identity password hasher: " + err.Error())
	}

	svc, err := identity.NewService(d.Pool, hasher, d.Clock)
	if err != nil {
		panic("cmd/api: identity service: " + err.Error())
	}

	handler, err := identity.NewHandler(svc, d.Logger)
	if err != nil {
		panic("cmd/api: identity handler: " + err.Error())
	}
	return handler
}
