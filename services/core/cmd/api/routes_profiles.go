package main

import (
	"net/http"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/profiles"
)

// The profiles domain's routes (SHIP-81a onwards).
//
// This file exists so that adding a domain adds a file and edits none. `cmd/api/routes.go`,
// `manifest.go` and `main.go` are shared surfaces (Docs/10 §9.2); a route registration that had to go
// into one of them is a line every concurrent branch also touches, and a badly resolved conflict
// there drops an endpoint with no compile error and no failing test.
//
// # Why the prefix is `/provider` rather than `/profiles` or `/fleet`
//
// `/fleet` established the pattern that a first segment can name the *caller's role* rather than a
// resource, and `/driver` (SHIP-108) is the second instance of it. This is the third. A verification
// record is not a collection anybody browses — there is exactly one per caller and no identifier for
// it — so `/v1/provider/verification` reads as what it is, and a `/profiles` prefix would name the
// package rather than anything a client can see.
//
// It is deliberately not under `/fleet`. That prefix is the provider's *fleet* — vehicles, service
// area, the jobs they may bid on — and `internal/fleet` answers all of it. Verification is a
// different domain's record, and putting it there would suggest the eligibility answer lives in the
// same place as the record it reads, which is precisely the confusion SHIP-81a exists to remove.
//
// # There is no route to the decision, and that is not an omission
//
// `profiles.Service.Decide` is exported and has no endpoint here. Deciding somebody's verification
// standing is an administrator's act on the administrator credential (SHIP-147), so SHIP-153's queue
// and SHIP-154's decision are `/v1/admin` routes served by `internal/admin` — which will call this
// transition through a port it declares for itself. A route here would be a second way to reach the
// same act, on the wrong credential, and Docs/04 §9's least-privilege requirement is the reason not
// to build one before the reviewer exists.
func init() {
	register(
		Route{
			Method:  http.MethodGet,
			Pattern: "/provider/verification",
			Group:   GroupV1,
			Auth:    RequireUser,
			Handler: func(d Deps) http.Handler { return profilesHandler(d).Verification() },
		},
	)
}

// profilesHandler builds the domain's handler from what Deps already carries.
//
// Nothing this domain needs is missing from Deps: the clock and the pool, which is the same pair
// `fleet` needs and the test Docs/10 §9.2 sets for whether a domain has been written the way the
// shared-surface rules ask — no field had to be added to a shared struct.
//
// It panics for the reason `fleetHandler` does: it runs during attach, at startup, and every failure
// it can report is a wiring mistake that will still be there after a restart. The pool is deliberately
// not checked — it may be nil because the database was unreachable at startup, which is a transient
// condition the service is built to survive.
func profilesHandler(d Deps) *profiles.Handler {
	handler, err := profiles.NewHandler(profiles.NewService(d.Clock), d.Pool, d.Logger)
	if err != nil {
		panic("cmd/api: profiles handler: " + err.Error())
	}
	return handler
}
