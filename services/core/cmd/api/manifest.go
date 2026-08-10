package main

import (
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
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
// not change any other domain's signature. It grows as the service does — a pool at SHIP-28's
// first endpoint, a Redis client at SHIP-47 — and every addition is shared-surface work.
type Deps struct {
	Config *config.Config
	Logger *slog.Logger
	Clock  clock.Clock

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

	// Handler builds the handler from the service's dependencies. It is a function rather
	// than an http.Handler because routes are declared at init time, before anything has
	// been constructed.
	Handler func(Deps) http.Handler
}

// String renders the route as the golden file records it.
func (r Route) String() string {
	return fmt.Sprintf("%-6s %-40s %-12s %s", r.Method, r.fullPath(), r.Auth, r.Group)
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

// attach registers every route of a group onto a mux.
func attach(mux *http.ServeMux, group Group, deps Deps) {
	for _, r := range routes() {
		if r.Group != group {
			continue
		}
		mux.Handle(r.Method+" "+r.Pattern, r.Handler(deps))
	}
}
