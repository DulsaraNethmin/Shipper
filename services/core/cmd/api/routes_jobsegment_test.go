package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

// SHIP-83a — the `/v1/jobs/{id}/<literal>` space, and the test that proves it is free.
//
// # What was wrong, precisely
//
// `GET /v1/jobs/open/{id}` (SHIP-83) put a literal where an identifier goes. Go's `ServeMux`
// resolves an overlap by preferring the more specific pattern, and "more specific" is decided
// segment by segment: against `GET /v1/jobs/{id}/<literal>` the open feed wins segment 3 —
// `open` beats `{id}` — and loses segment 4, where `{id}` loses to a literal. Neither dominates,
// both match `/v1/jobs/open/<literal>`, and `ServeMux` **panics at registration** rather than
// choosing. That is not a 404 at request time. The process does not start, and it does not start
// for every endpoint, so one badly shaped route takes the service down at boot.
//
// Four tickets paid a workaround before the shape was worth fixing: SHIP-115 took
// `/delivery/proof`, SHIP-115a `/delivery/detail` and `/delivery/milestones`, SHIP-101a went to
// `/v1/fleet/bids` rather than under the job at all, and SHIP-102a took `/bids/received`.
//
// # Why this is a test rather than a route
//
// SHIP-83a's *Done when* asks for the freed space to be **demonstrated by registering one**
// four-segment literal, not asserted. There were two ways to do that. Adding a real endpoint would
// have demonstrated it once, on the day it was added, and left the guard resting on that endpoint
// never being removed — and it would have meant shipping an endpoint no ticket asked for, which is
// the kind of thing that acquires a client and then cannot be withdrawn.
//
// Registering it here demonstrates the same fact on every run, against the **real** route table
// rather than a fixture of one: [attachRoutes] is handed `routes()` plus one extra, so anything
// that reintroduces a literal in the `{id}` slot — under `/v1/jobs` or under `/v1/fleet/jobs` —
// fails this test instead of the process's next start. It is the seam adminauth_test.go and
// driverauth_test.go already use, and it exists precisely so a test can exercise routing without
// registering from a `_test.go` `init`, which would pollute `manifest()` and break
// TestRouteTableMatchesGolden.

// jobLiteralProbe is the four-segment route this test registers alongside the real table.
//
// `segment-probe` is deliberately not a word any live ticket wants. A probe named after a plausible
// future endpoint invites somebody to promote it to a real one, and then the guard is gone.
func jobLiteralProbe() Route {
	return Route{
		Method:  http.MethodGet,
		Pattern: "/jobs/{id}/segment-probe",
		Group:   GroupV1,
		Auth:    Public,
		Handler: func(Deps) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("probe:" + r.PathValue("id")))
			})
		},
	}
}

// TestFourSegmentJobLiteralsCanBeRegistered is SHIP-83a's acceptance criterion.
//
// It attaches every real v1 route plus one four-segment `GET /v1/jobs/{id}/<literal>` to a fresh
// mux. Before SHIP-83a this panicked inside `mux.Handle`; the recover below turns that into a
// named failure rather than a stack trace, because the panic text `ServeMux` produces names the two
// patterns and is worth reading.
func TestFourSegmentJobLiteralsCanBeRegistered(t *testing.T) {
	mux := http.NewServeMux()

	func() {
		defer func() {
			if p := recover(); p != nil {
				t.Fatalf("registering a four-segment GET /v1/jobs/{id}/<literal> beside the real "+
					"route table panicked, which means something has put a literal back in the "+
					"{id} slot under /v1/jobs — SHIP-83a moved the provider feed to /v1/fleet/jobs "+
					"to free exactly this space:\n\n%v", p)
			}
		}()

		attachRoutes(mux, append(routes(), jobLiteralProbe()), GroupV1, testDeps(),
			guardsFor(testDriverGuard(), testAdminGuard()))
	}()

	// Registration succeeding is the criterion; routing to the right handler is what makes the
	// registration meaningful. A mux that accepted both patterns and then sent every request to
	// one of them would satisfy a panic-free test and nothing else.
	id := uuid.New().String()

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/jobs/"+id+"/segment-probe", nil))

	if got, want := rec.Body.String(), "probe:"+id; got != want {
		t.Errorf("GET /v1/jobs/{id}/segment-probe was served by the wrong handler: body %q, want %q",
			got, want)
	}
}

// TestTheOpenFeedIsGoneFromTheJobsPrefix holds the other half of the *Done when* — "the old path is
// gone rather than aliased".
//
// A redirect would have been the comfortable migration and it would have kept the collision: a
// pattern registered to answer 301 collides with a four-segment literal exactly as one registered
// to answer 200 does, because `ServeMux` refuses the pair before either handler is reached. So the
// old path is not served at all, and this is what says so.
func TestTheOpenFeedIsGoneFromTheJobsPrefix(t *testing.T) {
	for _, r := range routes() {
		if r.Group != GroupV1 {
			continue
		}
		if strings.HasPrefix(r.Pattern, "/jobs/open") {
			t.Errorf("%s %s: SHIP-83a removed the provider feed from the /jobs prefix and did not "+
				"alias it. Anything under /v1/jobs/open reintroduces the registration collision "+
				"that TestFourSegmentJobLiteralsCanBeRegistered exists to catch", r.Method, r.fullPath())
		}
	}
}
