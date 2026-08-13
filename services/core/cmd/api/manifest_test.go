package main

import (
	"flag"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
)

var updateGolden = flag.Bool("update", false, "rewrite routes_golden.txt from the current manifest")

const goldenPath = "routes_golden.txt"

// TestRouteTableMatchesGolden renders the whole served surface and diffs it against a file in
// the repository.
//
// This is the most valuable test in the package, and the reason is specific rather than
// general. A route that goes missing produces **no compile error**: the handler still exists,
// the package still builds, every other test still passes, and the endpoint simply is not
// there. With several branches merging into develop the likeliest way that happens is a
// conflict resolved slightly wrong, or a rebase that drops a hunk — and a rebase rewrites
// history, so afterwards there is nothing to see.
//
// Rendering the surface to a committed file turns all of that into a one-line diff that a
// reviewer reads in about two seconds. It also makes the opposite mistake visible: an endpoint
// nobody meant to expose has to be added to this file by whoever exposed it.
//
// Regenerate with `go test ./cmd/api -run TestRouteTableMatchesGolden -update`, and read the
// diff before committing it.
func TestRouteTableMatchesGolden(t *testing.T) {
	got := manifest()

	if *updateGolden {
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("writing %s: %v", goldenPath, err)
		}
		t.Logf("wrote %s", filepath.Clean(goldenPath))
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading %s: %v\nregenerate with `go test ./cmd/api -run TestRouteTableMatchesGolden -update`", goldenPath, err)
	}

	if got != string(want) {
		t.Errorf("the served route table does not match %s.\n\n"+
			"If you added or removed a route deliberately, regenerate it:\n"+
			"    go test ./cmd/api -run TestRouteTableMatchesGolden -update\n\n"+
			"If you did not, a route has been lost or gained in a merge.\n\n"+
			"--- %s\n%s\n+++ actual\n%s", goldenPath, goldenPath, want, got)
	}
}

// depsFields is the whole of what a handler may be built from, and the list is closed on
// purpose (SHIP-15c).
//
// Deps and its literal in main.go are shared surfaces, and the failure this guards is a merge
// rather than a mistake: three tracks each adding one field to one struct and one line to one
// literal, in the file where a badly resolved conflict unwires a domain without a compile error.
//
// Seeding the pool and the Redis client ahead of the endpoints that need them means a domain
// builds its collaborators in its own Handler closure and edits nothing shared. This test is
// what turns "adds no field" from an instruction in a comment into something that fails.
//
// Adding a field is not forbidden — it is a decision with a name attached. Add it here, and say
// in review why a closure over the pool, the Redis client and the configuration could not do it.
var depsFields = []string{
	"Config",
	"Logger",
	"Clock",
	"Pool",
	"Redis",
	"StartedAt",
}

func TestDepsCarriesExactlyWhatIsDeclared(t *testing.T) {
	var got []string
	for i, typ := 0, reflect.TypeOf(Deps{}); i < typ.NumField(); i++ {
		got = append(got, typ.Field(i).Name)
	}

	if !slices.Equal(got, depsFields) {
		t.Errorf("Deps has fields %v, want %v.\n\n"+
			"A domain builds its own collaborators inside its Handler closure, from the pool,\n"+
			"the Redis client and the configuration already here — see the comment on Deps.\n"+
			"If this genuinely needs a new field, add it to depsFields in %s and say why in\n"+
			"review: Deps and its literal in main.go are edited by every track at once.",
			got, depsFields, "manifest_test.go")
	}
}

// The pool and the Redis client are nil-able, and every domain has to be built expecting it —
// the service starts with an unreachable database or cache deliberately, so that a failover does
// not take the whole fleet down with it.
//
// This asserts the property the rest of the service is entitled to assume: a Deps with neither
// still builds a router and still serves the routes that do not need them. A change that made
// the pool mandatory would fail here rather than in a deployment.
func TestTheRouterBuildsWithoutADatabaseOrACache(t *testing.T) {
	deps := testDeps()
	deps.Pool, deps.Redis = nil, nil

	router := newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), nil)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("GET /health returned %d with no pool and no Redis, want 200 (%s)", rec.Code, rec.Body)
	}
}

// publicMutatingRoutes are the only state-changing endpoints that may be reached without
// credentials, because they are how a caller obtains them in the first place.
//
// Every one of these is rate limited by SHIP-47, and adding to this list is a decision with a
// security consequence rather than a routing detail — which is the point of keeping the list
// here, short, and in the same file as the test that enforces it.
var publicMutatingRoutes = map[string]bool{
	"POST /v1/auth/register":      true, // SHIP-30
	"POST /v1/auth/login":         true, // SHIP-41
	"POST /v1/auth/refresh":       true, // SHIP-42
	"POST /v1/auth/verify-email":  true, // SHIP-33
	"POST /v1/auth/verify-phone":  true, // SHIP-36
	"POST /v1/auth/request-otp":   true, // SHIP-34
	"POST /v1/auth/resend-verify": true, // SHIP-33
}

// TestNoMutatingRouteIsPublic stops a state-changing endpoint being reachable without
// authentication.
//
// SHIP-44 introduces the split between the public and protected subtrees, and the risk arrives
// with it: once a public group exists, adding an endpoint to the wrong one is a single line
// that looks entirely ordinary in review. Docs/07 §3 puts every authorisation decision on the
// platform, so an unauthenticated mutation is not a small mistake.
//
// The test runs now, before that split exists, so the rule is in place before the habit forms.
func TestNoMutatingRouteIsPublic(t *testing.T) {
	for _, r := range routes() {
		if !r.mutating() || r.Auth != Public {
			continue
		}
		key := r.Method + " " + r.fullPath()
		if publicMutatingRoutes[key] {
			continue
		}
		t.Errorf("%s changes state but requires no authentication.\n"+
			"Give it an Auth class, or — if it genuinely must be public, like sign-in — "+
			"add it to publicMutatingRoutes in %s and say why.", key, "manifest_test.go")
	}
}

// TestEveryRouteIsReachable guards against a pattern that ServeMux will accept and then never
// match, and against two routes claiming the same one.
//
// http.ServeMux panics on a duplicate registration, so a collision would already fail loudly at
// startup — but it would do so in whichever environment started first, rather than here.
func TestEveryRouteIsReachable(t *testing.T) {
	seen := map[string]bool{}

	for _, r := range routes() {
		key := r.Method + " " + r.fullPath()
		if seen[key] {
			t.Errorf("%s is registered twice", key)
		}
		seen[key] = true

		if strings.Contains(r.Pattern, "//") {
			t.Errorf("%s has an empty path segment", key)
		}
		if r.Group == GroupV1 && strings.HasPrefix(r.Pattern, apiPrefix) {
			t.Errorf("%s repeats the %s prefix; patterns in the v1 group are written without it",
				key, apiPrefix)
		}
	}
}

// TestOperationalRoutesAreNotVersioned keeps /health and its kind outside /v1.
//
// Versioning an endpoint that load balancers and monitoring call means every health check
// breaks on the day v2 ships.
func TestOperationalRoutesAreNotVersioned(t *testing.T) {
	for _, r := range routes() {
		if r.Group != GroupOperational {
			continue
		}
		if strings.HasPrefix(r.Pattern, apiPrefix) {
			t.Errorf("operational route %s is under %s", r.Pattern, apiPrefix)
		}
	}
}

// TestTheManifestIsWhatIsServed checks that declaring a route actually serves it.
//
// Everything else in this file tests the manifest against itself. This one closes the loop: if
// attach stopped registering routes, every other test here would still pass while the service
// answered 404 to everything.
func TestTheManifestIsWhatIsServed(t *testing.T) {
	router := testRouter()

	for _, r := range routes() {
		// Only GET, and only patterns with no path variable — a variable needs a real
		// value to match, and those routes are covered by their own domain's tests.
		if r.Method != http.MethodGet {
			continue
		}
		if strings.Contains(r.Pattern, "{") && !strings.HasSuffix(r.Pattern, "{$}") {
			continue
		}

		path := strings.TrimSuffix(r.fullPath(), "{$}")

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(r.Method, path, nil))

		if rec.Code == http.StatusNotFound {
			t.Errorf("%s %s is in the manifest but the router answers 404", r.Method, path)
		}
	}
}
