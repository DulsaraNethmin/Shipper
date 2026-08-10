package main

import (
	"net/http"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// apiVersion is the version of the public API this build serves (SHIP-13).
//
// More than one version may be live at once, because old app builds persist on devices
// indefinitely and Flutter has no over-the-air update path for Dart code (Docs/06 §5.3).
// A version is retired only after telemetry shows negligible traffic from the builds that
// depend on it — not when the next one ships.
//
// Introducing v2 means adding a second route group beside this one, not editing this
// constant. A change here changes the contract every installed build is already using.
const apiVersion = "v1"

// apiPrefix is where the versioned public API is mounted.
const apiPrefix = "/" + apiVersion

// newRouter wires the HTTP surface.
//
// There are two groups, and the split is deliberate:
//
//   - The versioned public API under /v1. Everything the Flutter app, the admin panel,
//     and the driver portal call lives here (Docs/06 §2.1 — one contract, three
//     consumers).
//   - Operational endpoints outside it. /health is consumed by load balancers and
//     monitoring, not by API clients, and versioning it would break every health check on
//     the day v2 ships.
//
// Which routes exist is not decided here. Each domain declares its own in routes_<domain>.go
// and they arrive through the manifest, so adding a domain does not edit this file — see
// manifest.go for why that matters.
func newRouter(deps Deps, idempotencyStore httpx.IdempotencyStore) http.Handler {
	root := http.NewServeMux()
	attach(root, GroupOperational, deps)

	// StripPrefix means every pattern inside the group is written without /v1, so a
	// route moves between versions by being registered in a different group rather than
	// by having its pattern rewritten.
	//
	// Idempotency is applied to the group rather than per route, so a state-changing
	// endpoint cannot be added without it (SHIP-15). Read-only methods pass through
	// untouched. The scope is nil until SHIP-44 introduces authentication and can supply
	// the authenticated subject — see the note on httpx.Idempotent.
	//
	// StandardErrors is applied again *inside* it, ahead of the group's own mux. What the
	// middleware stores is what it will replay, and a replay has to be byte-identical to
	// the response the first attempt received — including for the 404 and 405 that
	// ServeMux writes as plain text. Normalising after storing would replay the raw form
	// instead.
	v1 := http.NewServeMux()
	attach(v1, GroupV1, deps)
	root.Handle(apiPrefix+"/", http.StripPrefix(apiPrefix,
		httpx.Idempotent(idempotencyStore, nil)(httpx.StandardErrors(v1))))

	// Ordering is load-bearing. RequestID is outermost so both the log record and any
	// recovered panic can be attributed to a request; Recover sits inside Logger so a
	// panicking handler still produces a request line with its 500; StandardErrors is
	// innermost so it sees the 404 and 405 that ServeMux produces itself and can put
	// them in the error contract (SHIP-12).
	//
	// Do not tidy this. Changing the order, or removing the second StandardErrors above,
	// breaks idempotent replay in a way no test outside internal/httpx will notice
	// (Docs/10 §4.2).
	return httpx.Chain(root,
		httpx.RequestID,
		httpx.Logger(deps.Logger),
		httpx.Recover(deps.Logger),
		httpx.StandardErrors,
	)
}

// The two routes that belong to the service itself rather than to any domain.
//
// Product endpoints do not go here. Identity's arrive in routes_identity.go from SHIP-30,
// jobs' in routes_jobs.go from SHIP-61, and so on — one file per domain, so that two domains
// being built at the same time never edit the same one.
func init() {
	register(
		Route{
			Method:  http.MethodGet,
			Pattern: "/health",
			Group:   GroupOperational,
			Auth:    Public,
			Handler: func(d Deps) http.Handler { return healthHandler(d) },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/{$}",
			Group:   GroupV1,
			Auth:    Public,
			Handler: func(Deps) http.Handler { return apiRootHandler() },
		},
	)
}

// apiRootResponse is the body of GET /v1/.
type apiRootResponse struct {
	APIVersion string `json:"api_version"`
}

// apiRootHandler answers the root of the version group with the version it is the root
// of (SHIP-13).
//
// It is not a product endpoint and never will be. It exists because "which API version
// does this deployment answer" is a question with an operational answer — during a period
// when v1 and v2 are both live and served by different deployments, it takes one call to
// establish which one a hostname reached. The build behind it comes from /health; this
// says only what contract is on offer.
func apiRootHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, apiRootResponse{APIVersion: apiVersion})
	}
}

// healthResponse is the body of GET /health.
//
// SHIP-19 has the Flutter app display Version from this endpoint as its proof of
// connectivity, so the field name is part of a contract with a client that will be
// installed on devices — renaming it later is a breaking change, not a tidy-up.
type healthResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	BuiltAt string `json:"built_at"`
	Dirty   bool   `json:"dirty"`
	Uptime  string `json:"uptime"`
}

// healthHandler reports that the process is alive and which build it is running
// (SHIP-6).
//
// It deliberately does not check PostgreSQL, Redis, or Kafka. This endpoint answers
// "should this instance be restarted or taken out of rotation", and a database blip is
// not a reason to kill every instance at once. A readiness endpoint that does check
// dependencies is a separate thing, and belongs with the deployment work rather than
// here.
//
// It is also deliberately outside the /v1 group: operational endpoints are consumed by
// load balancers and monitoring, not by API clients, and versioning them would mean the
// health check breaks on the day v2 ships.
func healthHandler(d Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info := buildinfo.Get()

		httpx.WriteJSON(w, http.StatusOK, healthResponse{
			Status:  "ok",
			Version: info.Version,
			Commit:  info.Commit,
			BuiltAt: info.BuiltAt,
			Dirty:   info.Dirty,
			Uptime:  d.Clock.Now().Sub(d.StartedAt).Round(time.Second).String(),
		})
	}
}
