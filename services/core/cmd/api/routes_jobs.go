package main

import (
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/platform/geocoding"
)

// The jobs domain's routes (SHIP-61 onwards).
//
// This file exists so that adding a domain adds a file and edits none. cmd/api/routes.go,
// manifest.go and main.go are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # Why both routes require a user
//
// A job belongs to a customer. There is no field in either request for naming which one — the
// owner is whoever the token says is calling — so neither endpoint has a meaning without a
// credential. That is also what makes the pair safe under SHIP-44's scoped idempotency: keys land
// in idem:v1:<subject>:<key> rather than the shared anonymous namespace, which is the gate
// CLAUDE.md holds authenticated state-changing endpoints behind and which SHIP-44 closed.
//
// The role is not enforced here. A provider presenting a valid token passes RequireUser and is
// refused by the domain, which reads users.role rather than trusting the token's claim — 000400
// says the customer-account rule is enforced where the draft is created, and that is one place
// rather than two that can disagree.
func init() {
	register(
		Route{
			Method:  http.MethodPost,
			Pattern: "/jobs",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return jobsHandler(d).Create() },
		},
	)
}

// jobsHandler builds the domain's handler from what Deps already carries.
//
// Nothing jobs needs is missing from Deps, which is the test of whether a domain has been written
// the way Docs/10 §9.2 asks: the outbox writer, the clock and the geocoder are all pure functions
// of the pool, the clock and the configuration, so no field had to be added to a shared struct.
//
// It panics for the same reason identityHandler does: it runs during attach, at startup, and
// every failure it can report is a wiring mistake that will still be there after a restart. The
// pool is deliberately not checked — it may be nil because the database was unreachable at
// startup, which is a transient condition the service is built to survive, and the handlers
// answer 503 for as long as it lasts.
func jobsHandler(d Deps) *jobs.Handler {
	svc := jobs.NewService(events.NewOutbox(), d.Clock, newGeocoder(d))

	handler, err := jobs.NewHandler(svc, d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: jobs handler: " + err.Error())
	}
	return handler
}

// newGeocoder picks the geocoding implementation for this environment (SHIP-59a, SHIP-60).
//
// The composition root doing the one thing only it can: jobs declares what it needs of a geocoder
// in its own ports.go and imports nothing from internal/platform, and the adapter knows nothing
// about jobs. Go satisfies the interface structurally, and the two meet here (Docs/06 §4.1).
//
// # Why staging and production get no geocoder at all
//
// Because there is nothing to give them. No document names a maps vendor, and there is no
// GEOCODING_* configuration for one — geocoding.Provider takes its base URL and credential as its
// own Options, and internal/config has no fields to fill them from. Adding them is a shared-surface
// change (internal/config and deploy/.env.example, held to each other by a test), which SHIP-60
// could not make from a domain branch.
//
// Of the three ways to leave it, this is the least bad:
//
//   - falling back to geocoding.Stub outside development would write coordinates that are stable,
//     plausible, inside Australia, and entirely fictional. A fictional coordinate on a real job is
//     much harder to notice than a missing one;
//   - panicking would make the API unbootable in staging the moment this file merged, which is a
//     regression in a deployment that works today, over a feature it does not yet have;
//   - a nil geocoder stores the address exactly as the customer typed it, with no coordinate.
//     Every path through the domain already copes with that, because SHIP-59a requires an
//     unrecognised address not to fail the job.
//
// The warning is deliberately at startup rather than per request: it is a fact about the
// deployment, and one line in the boot log is findable where one line per created job is noise.
func newGeocoder(d Deps) jobs.Geocoder {
	if geocoding.UseStub(d.Config.Env) {
		return geocoding.NewStub()
	}

	d.Logger.Warn("no geocoding provider is configured; job addresses will be stored unresolved",
		"env", string(d.Config.Env),
		"needs", "GEOCODING_BASE_URL and GEOCODING_API_KEY in internal/config (Docs/11 §9)")
	return nil
}

// Compile-time proof that the two implementations of the port satisfy it, which is the only place
// in the build where that can be established — the domain does not name the adapter and the
// adapter does not name the domain, so nothing else links them.
var (
	_ jobs.Geocoder = (*geocoding.Stub)(nil)
	_ jobs.Geocoder = (*geocoding.Provider)(nil)
)
