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

// guardsFor builds the auth-class map the version group is served with (SHIP-15m).
//
// # This function is the seam, and the seam is the whole of what SHIP-15m delivers
//
// `cmd/api/routes.go` is a shared surface (Docs/10 §9.2), so the delivery track cannot add its own
// entry to this map — which made SHIP-108 unbuildable from a domain branch, because the class it
// enforces has to be mapped *here*. The map is therefore built from an argument rather than from a
// literal: whoever supplies the guard supplies it at startup, and this file is not edited again.
//
// SHIP-108 fills it by writing the verifier behind newDriverTokenGuard (driverauth.go) and nothing
// in this file, manifest.go or Deps.
//
// # Absent is not the same as refusing, and the difference is the property worth keeping
//
// A nil guard leaves the class **out of the map**, and attach panics at startup for any route
// declaring it. That is deliberate and it is the reason this is not simply
// `guards{RequireDriverToken: refuseEverything}`: an entry that refuses everything is a route that
// answers 401 forever, indistinguishable from an expired credential to every client and to
// whoever gets asked about it. A process that will not start is the loudest possible version of
// "this route's auth class is not implemented yet", and it cannot reach production.
//
// RequireAdmin got the same treatment at SHIP-15r, before SHIP-147 rather than during it, which is
// what the paragraph this one replaces promised: a second parameter here and newAdminGuard beside
// newDriverTokenGuard. Both classes are now seated, and **this function is finished** — Docs/06 has
// no fifth auth class, so the next edit to it is a class somebody has argued for rather than one a
// track needed on a Tuesday.
func guardsFor(driverToken, admin Guard) guards {
	g := guards{
		// The mobile access token, resolved group-wide by ResolveSubject and required per
		// route here (SHIP-44).
		RequireUser: httpx.RequireSubject(),
	}

	// Absent rather than permissive, and absent rather than refusing. See above.
	if driverToken != nil {
		g[RequireDriverToken] = driverToken
	}

	// Nil today: newAdminGuard returns nothing until SHIP-147 fills it, so RequireAdmin stays
	// out of the map and `internal/admin`'s first protected route stops the process rather than
	// being served open. That is the same state RequireDriverToken was in between SHIP-15m and
	// SHIP-108, and it is the state this branch exists to make survivable.
	if admin != nil {
		g[RequireAdmin] = admin
	}

	return g
}

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
func newRouter(
	deps Deps,
	idempotencyStore httpx.IdempotencyStore,
	limiter httpx.RateLimiter,
	authenticate httpx.Authenticator,
	driverToken Guard,
	admin Guard,
) http.Handler {
	// Which middleware enforces which auth class. A class absent from this map cannot be
	// served at all — see attach and guardsFor.
	protected := guardsFor(driverToken, admin)

	root := http.NewServeMux()

	// Operational routes are outside the version group and therefore outside subject
	// resolution, so nothing here can require a credential. Passing no guards makes that a
	// startup panic rather than a route that 401s forever, and TestOperationalRoutesArePublic
	// makes it a test failure before anyone gets that far.
	attach(root, GroupOperational, deps, nil, limiter)

	// StripPrefix means every pattern inside the group is written without /v1, so a
	// route moves between versions by being registered in a different group rather than
	// by having its pattern rewritten.
	//
	// Idempotency is applied to the group rather than per route, so a state-changing
	// endpoint cannot be added without it (SHIP-15). Read-only methods pass through
	// untouched.
	//
	// StandardErrors is applied again *inside* it, ahead of the group's own mux. What the
	// middleware stores is what it will replay, and a replay has to be byte-identical to
	// the response the first attempt received — including for the 404 and 405 that
	// ServeMux writes as plain text. Normalising after storing would replay the raw form
	// instead.
	v1 := http.NewServeMux()
	attach(v1, GroupV1, deps, protected, limiter)

	// ResolveSubject sits **outside** Idempotent, and that is the ordering SHIP-44 exists for.
	//
	// httpx.Idempotent namespaces stored responses by caller. Its scope was nil until now, so
	// every key landed in `idem:v1:anonymous:<key>` and a client that guessed another
	// client's key was handed that client's response body. SubjectScope closes it — but only
	// if the subject is on the context before the scope is computed, which means resolution
	// has to happen further out than idempotency does.
	//
	// Requiring a credential stays per route, inside, because the manifest declares it per
	// route. Resolving one is group-wide and rejects nothing: /v1/auth/refresh is public and
	// is called by exactly the client whose token has just expired (Docs/10 §4.2).
	root.Handle(apiPrefix+"/", http.StripPrefix(apiPrefix,
		httpx.ResolveSubject(authenticate)(
			httpx.Idempotent(idempotencyStore, httpx.SubjectScope)(
				httpx.StandardErrors(v1)))))

	// Ordering is load-bearing. RequestID is outermost so both the log record and any
	// recovered panic can be attributed to a request; Recover sits inside Logger so a
	// panicking handler still produces a request line with its 500; StandardErrors is
	// innermost so it sees the 404 and 405 that ServeMux produces itself and can put
	// them in the error contract (SHIP-12).
	//
	// The client-address resolver sits inside Logger and Recover for both of those reasons —
	// it logs a misconfigured proxy chain through the request-scoped logger, and a malformed
	// header must not be able to panic past the recovery handler. It sits outside everything
	// that reads an address, which is every address-keyed limit in the service (SHIP-183b).
	//
	// Do not tidy this. Changing the order, or removing the second StandardErrors above,
	// breaks idempotent replay in a way no test outside internal/httpx will notice
	// (Docs/10 §4.2).
	return httpx.Chain(root,
		httpx.RequestID,
		httpx.Logger(deps.Logger),
		httpx.Recover(deps.Logger),
		resolveClientAddr(deps),
		httpx.StandardErrors,
	)
}

// resolveClientAddr builds the client-address middleware from the deployment's configuration
// (SHIP-183b).
//
// # Why it is read here rather than passed to newRouter
//
// It is not a collaborator the composition root has to build — it is two values already on Deps,
// and threading them through a signature that five tests construct by hand would be five more
// places to forget them. Every one of those would then run with the resolver absent, which
// [httpx.ClientAddr] handles by falling back to RemoteAddr and is exactly the shape a test wants.
//
// A nil Config is a test's rather than a deployment's, and it means trusting no proxy — the same
// answer config.Load gives a deployment that sets neither variable.
func resolveClientAddr(deps Deps) func(http.Handler) http.Handler {
	if deps.Config == nil {
		return httpx.ResolveClientAddr(0, nil)
	}
	return httpx.ResolveClientAddr(deps.Config.TrustedProxy.Hops, deps.Config.TrustedProxy.Networks)
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
			// The only unlimited route on the manifest, and the reason is that the
			// caller is the infrastructure: a load balancer and a container
			// orchestrator poll this, so throttling it takes a *healthy* instance out
			// of rotation — a limiter converted into an outage (Docs/12 §3).
			Limit:   LimitUnlimited,
			Handler: func(d Deps) http.Handler { return healthHandler(d) },
		},
		Route{
			Method:  http.MethodGet,
			Pattern: "/{$}",
			Group:   GroupV1,
			Auth:    Public,
			Limit:   LimitPublicRead,
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
