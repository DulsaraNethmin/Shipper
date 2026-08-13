package main

import (
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
)

// The composition root for the driver's job-scoped token (SHIP-107, SHIP-108).
//
// **This file is the one SHIP-108 writes, and it is the only file in cmd/api that ticket needs to
// touch.** It exists ahead of the code for the same reason Deps and internal/boundaries'
// infrastructure list do: cmd/api/routes.go, manifest.go and main.go are shared surfaces
// (Docs/10 §9.2), and a domain branch that had to edit one of them to serve its own routes is a
// merge conflict in the file where a bad resolution silently unwires an endpoint. So the call
// site, the signature and the wiring are all written now, and the ticket that has the verifier
// fills in the body.
//
// # What SHIP-108 replaces, and what it must not do
//
// Return a Guard that reads the credential, verifies it as a driver token, and refuses the request
// when it cannot. Docs/10 §5 fixes what "driver token" means: separate signing key material,
// `aud=shipper-driver`, exactly one `job_id`, and **the audience checked before anything else**.
//
// Three constraints on the implementation, none of them stylistic:
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
//   - **The middleware itself does not go in internal/httpx.** Infrastructure imports neither a
//     domain nor an adapter (SHIP-15c): every domain imports httpx, so one import of delivery from
//     inside it welds all eight to delivery through an edge that is in no domain's own files. This
//     is the same shape httpx.Authenticator already has — httpx declares what it needs, cmd/api
//     supplies the closure — with the difference that here httpx needs nothing at all, because the
//     guard is an ordinary func(http.Handler) http.Handler.
//
// # Why it is built here rather than taken from Deps
//
// The same argument as newAccessTokenAuthenticator, and the note on Deps in manifest.go makes it in
// full: Deps is what a *handler* is built from, and this is not one — it is a collaborator of the
// router. Adding a field for it would be an edit to a shared struct and to its literal in main.go,
// which is what a track adding one field to each is not allowed to do and this ticket exists to
// make unnecessary.
//
// The keyset and the clock are already parameters, unused today, so that the call in main.go is
// written once. A verifier reading key material from configuration is the one thing SHIP-108 is
// certain to need, and a signature that changed when the body was filled in would put main.go back
// in the diff.
//
// # Why an error rather than a panic
//
// Also the same as newAccessTokenAuthenticator: a keyset that cannot be built is a configuration
// error that will still be there after every restart, and a service that came up unable to verify
// any driver token would answer 401 to every driver while reporting itself healthy. It refuses to
// start instead, beside the other configuration failures.
//
// # What happens today
//
// It returns nil, and nil means the class is **absent** from the guard map rather than present and
// refusing — see guardsFor. A route declaring RequireDriverToken therefore stops the process at
// startup, which is where routes_delivery.go's two routes already say they are relying on it:
// both are RequireUser, because the caller is the provider, and neither is a driver route.
func newDriverTokenGuard(cfg *config.Config, clk clock.Clock) (Guard, error) {
	// SHIP-108: build the driver-token verifier from cfg and clk, and return the Guard that
	// enforces it. Until then the class is unenforced and therefore unserved.
	_, _ = cfg, clk
	return nil, nil
}
