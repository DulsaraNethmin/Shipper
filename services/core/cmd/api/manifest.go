package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The route manifest.
//
// Every route in the service is declared here rather than registered directly on a mux, and
// the reason is a merge hazard rather than an aesthetic one.
//
// With a single registerV1 function, two branches that each add an endpoint both edit the same
// lines. Git merges them; sometimes it merges them wrongly, and a dropped mux.Handle produces
// no compile error, no test failure, and no symptom until somebody calls the endpoint that
// quietly stopped existing. That is the single most likely way parallel work breaks this
// service (Docs/10 §4.1).
//
// So: a domain contributes routes_<domain>.go, which registers its own routes from an init
// function. Adding a domain adds a file and edits none. And the full surface is rendered to
// routes_golden.txt by a test, so a route that disappears in a merge shows up as a one-line
// diff in review instead of as an incident.

// Group is which mux a route is served from.
type Group int

const (
	// GroupV1 is the versioned public API. Everything the Flutter app, the admin panel and
	// the driver portal call lives here.
	GroupV1 Group = iota

	// GroupOperational is outside the version group: /health and anything else consumed by
	// load balancers and monitoring rather than by API clients. Versioning these would
	// break every health check on the day v2 ships.
	GroupOperational
)

func (g Group) String() string {
	if g == GroupOperational {
		return "operational"
	}
	return "v1"
}

// Auth is what a route requires of its caller.
//
// It is declared alongside the route rather than implied by where the handler sits, so that
// "which endpoints are public" is answerable by reading one file. SHIP-44 turns these into
// actual middleware; until then they are enforced by TestNoMutatingRouteIsPublic, which is
// already worth having — the allow-list is short and every future addition to it is a decision
// somebody has to make deliberately.
type Auth int

const (
	// Public needs no credentials.
	Public Auth = iota

	// RequireUser needs a valid mobile access token (SHIP-44).
	RequireUser

	// RequireDriverToken needs a job-scoped driver token, which grants exactly one job and
	// cannot be exchanged for a user session (SHIP-108).
	RequireDriverToken

	// RequireAdmin needs an administrator session, which is a separate system that a user
	// token can never reach (SHIP-147).
	RequireAdmin
)

func (a Auth) String() string {
	switch a {
	case RequireUser:
		return "user"
	case RequireDriverToken:
		return "driver-token"
	case RequireAdmin:
		return "admin"
	default:
		return "public"
	}
}

// Deps is what a handler may be built from.
//
// It is deliberately one struct rather than a per-domain argument list: a domain's
// routes_<domain>.go takes what it needs out of it, and adding a dependency for one domain does
// not change any other domain's signature.
//
// # Do not add a field for your domain's collaborators
//
// This struct and its literal in main.go are shared surfaces, and wave 2 is the first wave with
// three tracks that would each have a reason to grow one (Docs/10 §9.2). Three branches adding
// three fields to the same struct and three lines to the same literal is a merge conflict in the
// one file where a badly resolved one silently unwires a domain.
//
// So the infrastructure every domain could plausibly need is seeded here ahead of the code, the
// same way internal/boundaries seeds its infrastructure list. **A domain builds its own
// collaborators inside its Handler closure**, from what is already below:
//
//	Route{Handler: func(d Deps) http.Handler {
//	    keys, _ := identity.NewKeyset(d.Config.Identity.AccessTokenKeys, …)
//	    return identity.NewHandler(d.Pool, keys)
//	}}
//
// A keyset, a hasher, a token issuer and a repository are all pure functions of the pool, the
// Redis client and the configuration. If something genuinely cannot be — an adapter with its own
// process-wide connection, say — that is a shared-surface change, and it belongs to whoever owns
// cmd/api in that cycle rather than to the domain that noticed.
type Deps struct {
	Config *config.Config
	Logger *slog.Logger
	Clock  clock.Clock

	// Pool is the PostgreSQL connection pool, and it is the whole of a domain's access to
	// the database. Persistence takes a db.Runner, which the pool satisfies, so a handler
	// hands this straight to its own postgres.go or opens a transaction with db.InTx.
	//
	// It may be nil: the service starts with an unreachable database on purpose — see the
	// note in main.go — so a handler that dereferences it without checking will panic into
	// httpx.Recover and answer 500. That is the correct answer to "the database is down"
	// and is why nothing here pretends otherwise.
	Pool *pgxpool.Pool

	// Redis is the shared cache and token store. Idempotency already has its own store
	// built over this client; SHIP-47's token bucket and the device registry take the
	// client itself.
	//
	// Nil-able for the same reason as Pool, and more routinely: the idempotency middleware
	// is built to fail closed against exactly this.
	Redis *redis.Client

	// StartedAt is when the process came up, for /health's uptime.
	StartedAt time.Time
}

// Route is one endpoint.
type Route struct {
	// Method is the HTTP method. One route per method, even where two share a pattern, so
	// that the manifest reads as the list of things a client can do.
	Method string

	// Pattern is the path within the group, written without the /v1 prefix. A route moves
	// between versions by being registered in a different group rather than by having its
	// pattern rewritten.
	Pattern string

	// Group is which mux serves it.
	Group Group

	// Auth is what the caller must present.
	Auth Auth

	// Limit is which rate-limit class serves the route (SHIP-183a).
	//
	// It has no default. [LimitUnset] is the zero value and register refuses it, so a route
	// added without a class does not reach a mux — which is what makes `LimitUnlimited`
	// something somebody decided rather than something nobody typed. Docs/12 §5 assigns
	// every one of them and TestEveryRouteMatchesItsDocumentedClass holds the two together.
	Limit LimitClass

	// Handler builds the handler from the service's dependencies. It is a function rather
	// than an http.Handler because routes are declared at init time, before anything has
	// been constructed.
	Handler func(Deps) http.Handler
}

// String renders the route as the golden file records it.
func (r Route) String() string {
	return fmt.Sprintf("%-6s %-40s %-12s %-12s %s", r.Method, r.fullPath(), r.Auth, r.Limit, r.Group)
}

func (r Route) fullPath() string {
	if r.Group == GroupV1 {
		return apiPrefix + r.Pattern
	}
	return r.Pattern
}

// mutating reports whether the route changes state.
//
// The read-only methods are the ones the idempotency middleware also lets through untouched;
// keeping the two definitions the same means a route cannot be treated as safe by one and
// state-changing by the other.
func (r Route) mutating() bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	default:
		return true
	}
}

// registry holds every declared route. Populated by init functions in routes_*.go.
var registry []Route

// register declares routes. It is called from init, so it panics rather than returning an
// error: a malformed route is a programming mistake that should stop the process at startup,
// not produce a service missing an endpoint.
func register(routes ...Route) {
	for _, r := range routes {
		switch {
		case r.Method == "":
			panic(fmt.Sprintf("route %q has no method", r.Pattern))
		case r.Pattern == "":
			panic(fmt.Sprintf("%s route has no pattern", r.Method))
		case !strings.HasPrefix(r.Pattern, "/"):
			panic(fmt.Sprintf("route %s %q must start with /", r.Method, r.Pattern))
		case r.Handler == nil:
			panic(fmt.Sprintf("route %s %q has no handler", r.Method, r.Pattern))
		case r.Limit == LimitUnset:
			// SHIP-183a's *Done when*: a route registered without a limit fails a gate
			// rather than defaulting to unlimited. This is that gate. It is a panic at
			// init rather than a test failure because the failure it prevents — a new
			// endpoint quietly served with no bucket — is invisible in a diff, and a
			// process that will not start is the loudest available version of it.
			panic(fmt.Sprintf(
				"route %s %q has no rate-limit class. Every route belongs to one; Docs/12 §5 "+
					"assigns them and LimitUnlimited is a class you type rather than a default "+
					"you fall into", r.Method, r.Pattern))
		}
		registry = append(registry, r)
	}
}

// routes returns the registry sorted, so that the order two init functions happened to run in
// cannot change the golden file.
func routes() []Route {
	out := make([]Route, len(registry))
	copy(out, registry)
	sort.Slice(out, func(i, j int) bool {
		if out[i].fullPath() != out[j].fullPath() {
			return out[i].fullPath() < out[j].fullPath()
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// manifest renders the whole surface, one route per line. This is what the golden test
// compares against routes_golden.txt.
func manifest() string {
	var b strings.Builder
	for _, r := range routes() {
		b.WriteString(r.String())
		b.WriteByte('\n')
	}
	return b.String()
}

// Guard is the middleware that enforces one auth class.
//
// Named so that a guard can be carried through a signature without the shape being retyped at
// each hop — newDriverTokenGuard returns one, main.go passes it to newRouter, and guardsFor puts
// it in the map. It is deliberately cmd/api's type rather than one in internal/httpx: httpx
// already exports the middleware that satisfies it, and a second name for
// func(http.Handler) http.Handler in a package every domain imports would be a shared surface
// nothing needs.
type Guard func(http.Handler) http.Handler

// guards maps an auth class to the middleware that enforces it.
//
// A class with no entry is not served. That is the whole design: RequireDriverToken and
// RequireAdmin are declared in this file because the manifest has to be able to express them, and
// the middleware behind each arrives with SHIP-108 and SHIP-147. Until then a route declaring one
// stops the process at startup rather than being served open, which is the only acceptable
// direction for that mistake to fail in.
//
// **A class mapped to something that refuses everything is not the same thing and is not
// acceptable.** It turns a startup panic — loud, immediate, and impossible to deploy past — into a
// route that answers 401 forever, which looks like a credential problem to every client and to
// whoever is asked about it. Absent means absent (see guardsFor).
type guards map[Auth]Guard

// attach registers every route of a group onto a mux, wrapped in whatever its auth class requires
// (SHIP-44).
//
// Until SHIP-44 this ignored Route.Auth entirely, and the class was enforced only by
// TestNoMutatingRouteIsPublic — a test that a route was *declared* correctly, not that the
// declaration did anything. Reading it here is what turns the manifest from documentation into
// the thing that decides.
func attach(mux *http.ServeMux, group Group, deps Deps, g guards, limiter httpx.RateLimiter) {
	attachRoutes(mux, routes(), group, deps, g, limiter)
}

// attachRoutes is attach over an explicit route list.
//
// Split out so a test can exercise the auth-class wiring against routes of its own. The
// alternative — registering a route from a _test.go init — would put it in the real registry for
// every other test in this package, which breaks TestRouteTableMatchesGolden and
// TestEveryRouteIsInTheContract, both of which compare the served surface with a committed file.
func attachRoutes(mux *http.ServeMux, rs []Route, group Group, deps Deps, g guards, limiter httpx.RateLimiter) {
	scale := limitScaleFrom(deps.Config)

	for _, r := range rs {
		if r.Group != group {
			continue
		}

		handler := r.Handler(deps)

		// The limiter is wrapped *before* the guard, which puts it *inside* the guard at
		// request time, and that ordering is load-bearing (SHIP-183a).
		//
		// These classes key on the authenticated caller, and there is no caller until the
		// guard has run. Outside it, every unauthenticated request would key on the same
		// value and the first attacker to empty that bucket would refuse every anonymous
		// request to every route in the class — a limiter converted into a denial of
		// service. TestTheLimiterRunsInsideTheGuard holds this.
		if r.Limit == LimitUnset {
			// register already refuses this, so reaching it here means a Route literal
			// went to a mux without passing through the registry — which is exactly what
			// a test that builds its own routes does. Refusing in both places is what
			// makes "nothing is served unlimited by accident" a property of the mux
			// rather than of one code path into it.
			panic(fmt.Sprintf(
				"route %s %s has no rate-limit class, so attaching it would serve it "+
					"unlimited. Every route belongs to a class — see Docs/12 §5",
				r.Method, r.fullPath()))
		}

		if bucket, enforced := limitBucket(r.Limit, scale); enforced {
			if limiter == nil {
				// Same class of mistake, and the same answer, as an auth class with no
				// middleware: a route declared limited and served unlimited is worse
				// than a process that refuses to start.
				panic(fmt.Sprintf(
					"route %s %s is in the %s class and no limiter was supplied. Serving it "+
						"would make it unlimited. Pass one to newRouter",
					r.Method, r.fullPath(), r.Limit))
			}
			handler = httpx.Limit(limiter, bucket, limitKey(r.Limit))(handler)
		}

		if r.Auth != Public {
			guard, enforced := g[r.Auth]
			if !enforced {
				// Panicking at startup rather than returning an error: this is the same
				// class of mistake as a route with no handler, and register already
				// panics on that. A service that came up serving an endpoint whose auth
				// class nothing implements is worse than one that refuses to start.
				panic(fmt.Sprintf(
					"route %s %s requires %s and no middleware enforces it in this group. "+
						"Serving it would make it public. Either wire the middleware in "+
						"newRouter or do not declare the route yet",
					r.Method, r.fullPath(), r.Auth))
			}
			handler = guard(handler)
		}

		mux.Handle(r.Method+" "+r.Pattern, handler)
	}
}
