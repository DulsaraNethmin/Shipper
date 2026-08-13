package main

import (
	"fmt"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
)

// The composition root for the driver's job-scoped token (SHIP-107, SHIP-108).
//
// **This file is the one SHIP-108 wrote, and it was the only non-test file in cmd/api that ticket
// touched.** It exists ahead of the code for the same reason Deps and internal/boundaries'
// infrastructure list do: cmd/api/routes.go, manifest.go and main.go are shared surfaces
// (Docs/10 §9.2), and a domain branch that had to edit one of them to serve its own routes is a
// merge conflict in the file where a bad resolution silently unwires an endpoint. So the call
// site, the signature and the wiring were all written first, and the ticket that had the verifier
// filled in the body.
//
// **The one thing the seam did not carry is the test callers.** newRouter takes the guard as an
// argument, so every test that builds a router passes one — seventeen call sites across five files
// passed `nil` while nothing declared the class, and all seventeen had to become a real guard the
// moment a route did. None of those files is shared in the Docs/10 §9.2 sense and the edit is
// mechanical, but it is the part of the seam that was not free, and it is recorded in Docs/11 §3
// rather than left for the next class to rediscover.
//
// # What SHIP-108 replaced, and what it must not do
//
// Return a Guard that reads the credential, verifies it as a driver token, and refuses the request
// when it cannot. Docs/10 §5 fixes what "driver token" means: separate signing key material,
// `aud=shipper-driver`, exactly one `job_id`, and **the audience checked before anything else**.
//
// Three constraints on the implementation, none of them stylistic, and all three held:
//
//   - **It must not produce an [authctx.Subject].** The invariant in CLAUDE.md is that the driver's
//     job-scoped token and the mobile auth token are separate systems and neither can be exchanged
//     for the other. Resolving a driver token into the type the mobile session uses is exactly that
//     exchange, one helper function later — every domain reading a subject would then be reading a
//     driver as though a user had signed in, and the grant to *one job* would be gone. identity
//     already refuses the driver audience on the mobile side, with a test; this is the other
//     direction, and Docs/11 §8 has always kept both verifiers with one owner for that reason.
//
//   - **The grant belongs to internal/delivery**, not to internal/authctx and not to internal/httpx.
//     Delivery is the only domain that will ever serve a driver-token route, Docs/06 §4.1 has the
//     consuming domain declare what it needs, and a job-scoped grant sitting in authctx beside
//     Subject is an invitation to write the conversion the paragraph above forbids. So the guard
//     built here may import internal/delivery — cmd/api is where a domain and its collaborators
//     meet — and put delivery's own grant type on the context with delivery's own key. Nothing in
//     the seam names it, which is why the seam did not have to decide it (Docs/11 §3, SHIP-15m).
//
//     SHIP-108 went one step further: the grant type is exported, its context key and its accessor
//     are **not**, so nothing outside internal/delivery can read a driver grant at all. "Delivery is
//     the only domain that serves a driver-token route" is therefore a fact about the build rather
//     than a rule somebody follows.
//
//   - **The middleware itself does not go in internal/httpx.** Infrastructure imports neither a
//     domain nor an adapter (SHIP-15c): every domain imports httpx, so one import of delivery from
//     inside it welds all eight to delivery through an edge that is in no domain's own files. This
//     is the same shape httpx.Authenticator already has — httpx declares what it needs, cmd/api
//     supplies the closure — with the difference that here httpx needs nothing at all, because the
//     guard is an ordinary func(http.Handler) http.Handler.
//
//     It went to internal/delivery rather than staying in this file, and that was the one thing the
//     seam deliberately left open. The argument is in internal/delivery/driverauth.go: everything
//     the middleware does is that domain's — its error codes, its path parameter, its grant — and
//     handlers live in the domain (Docs/10 §2.1), a guard being the front half of one.
//
// # Why it is built here rather than taken from Deps
//
// The same argument as newAccessTokenAuthenticator, and the note on Deps in manifest.go makes it in
// full: Deps is what a *handler* is built from, and this is not one — it is a collaborator of the
// router. Adding a field for it would be an edit to a shared struct and to its literal in main.go,
// which is what a track adding one field to each is not allowed to do and this ticket exists to
// make unnecessary.
//
// The keyset and the clock were already parameters, unused, so that the call in main.go was written
// once. That paid: a verifier reading key material from configuration is exactly what SHIP-108
// needed, both parameters are used below, and main.go is not in that ticket's diff.
//
// # Why an error rather than a panic
//
// Also the same as newAccessTokenAuthenticator: a keyset that cannot be built is a configuration
// error that will still be there after every restart, and a service that came up unable to verify
// any driver token would answer 401 to every driver while reporting itself healthy. It refuses to
// start instead, beside the other configuration failures.
//
// # What it does now (SHIP-108), and how little of it is here
//
// Three statements, and the shape is newAccessTokenAuthenticator's exactly: build the keyset from
// configuration, build the verifier over it, hand back the middleware the domain declares. Nothing
// about a driver token is decided in this package — not the audience, not the claim set, not what a
// verified token grants, and not what a refusal answers with. That all lives in internal/delivery,
// beside the issuer that has to agree with it.
//
// **The seam held.** `cmd/api/routes.go`, `manifest.go`, `main.go` and `Deps` are untouched by
// SHIP-108: filling this body is what maps RequireDriverToken, and `routes_delivery.go` — the
// delivery track's own file — is what declares the first route on it. A route declaring an
// *unimplemented* class still stops the process at startup, which is what RequireAdmin gets today
// and is unchanged.
func newDriverTokenGuard(cfg *config.Config, clk clock.Clock) (Guard, error) {
	keys, err := delivery.NewKeyset(cfg.Delivery.DriverTokenKeys, cfg.Delivery.DriverTokenActiveKID)
	if err != nil {
		return nil, fmt.Errorf("driver token keyset: %w", err)
	}

	verifier, err := delivery.NewDriverTokenVerifier(keys, clk)
	if err != nil {
		return nil, fmt.Errorf("driver token verifier: %w", err)
	}

	// delivery.RequireDriverToken returns a func(http.Handler) http.Handler, which is Guard's
	// underlying type — so no conversion and, more to the point, no second declaration of the
	// middleware shape in a package every domain can see.
	return delivery.RequireDriverToken(verifier), nil
}
