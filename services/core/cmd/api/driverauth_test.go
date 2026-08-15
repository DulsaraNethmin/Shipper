package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/delivery"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
)

// The RequireDriverToken seam (SHIP-15m), and the ticket that filled it in (SHIP-108).
//
// SHIP-108 supplies the guard by filling in newDriverTokenGuard and editing nothing shared. The
// first four tests hold the two halves of that arrangement — that the class is unserved while
// nothing supplies one, and that supplying one is what serves *and* guards it — so neither can be
// quietly lost. **They are unchanged by SHIP-108**, which is what they were written for: the last of
// them asserts the rule rather than today's answer to it, so it kept passing on the day the nil
// stopped being nil.
//
// The two at the bottom are SHIP-108's own, and they are in this package rather than in
// internal/delivery because what they exercise is the wiring — the guard the constructor builds,
// attached by attach to the route the manifest declares, behind the real middleware chain.
//
// Every route in the first group is built locally and handed to attachRoutes, for the reason
// auth_test.go states: registering one from a _test.go init would put it in the real registry for
// every other test in this package, which breaks TestRouteTableMatchesGolden and
// TestEveryRouteIsInTheContract. The two at the bottom need no such thing — they call the route
// delivery declares in routes_delivery.go, which is in the registry because it is real.

// driverRoute is a route in the shape SHIP-108's will be: declared RequireDriverToken, with a
// handler that reports whether the platform decided anybody is calling.
//
// The handler asserts the *negative* half of Docs/10 §5 deliberately. A driver token must never
// resolve into an authctx.Subject — that is the exchange between the two token systems the
// invariant forbids — so a driver-token route reaching its handler with a subject on the context is
// a defect, and this says so rather than passing.
func driverRoute() Route {
	return Route{
		Method:  http.MethodPost,
		Pattern: "/jobs/{id}/driver-token-probe",
		Group:   GroupV1,
		Auth:    RequireDriverToken,
		Handler: func(Deps) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := authctx.SubjectFrom(r.Context()); ok {
					http.Error(w,
						"a driver-token route produced an authctx.Subject; the two token "+
							"systems have been joined (Docs/10 §5)", http.StatusTeapot)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
		},
	}
}

// refusingGuard stands in for SHIP-108's middleware: it lets a request through only when the
// stand-in credential is present, and records that it ran.
//
// A stub rather than a verifier, and that is the point of the ticket — the seam is what is being
// tested, and a real driver token does not exist until SHIP-107 signs one.
func refusingGuard(ran *int) Guard {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*ran++
			if r.Header.Get("X-Driver-Token-Probe") != "genuine" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// While nothing supplies a driver guard the class is absent from the map, and a route declaring it
// stops the process.
//
// **Absent, not refusing.** An entry that rejected every request would turn this startup panic into
// a route that answers 401 forever, which is indistinguishable from an expired credential to every
// client — the weaker failure, and the one guardsFor exists to avoid.
func TestWithNoDriverGuardTheClassIsAbsentAndTheRouteRefusesToStart(t *testing.T) {
	g := guardsFor(nil, nil)

	if _, enforced := g[RequireDriverToken]; enforced {
		t.Fatal("RequireDriverToken is mapped to something with no verifier behind it.\n" +
			"A guard that is present but refuses everything is not a substitute for an absent " +
			"one: it serves the route and answers 401 forever instead of stopping the process.")
	}

	// And absence is what makes the route unservable, rather than merely unmapped.
	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("a route requiring driver-token was attached with no middleware enforcing " +
				"it, which serves it to anybody")
		}
		if msg, _ := p.(string); !strings.Contains(msg, RequireDriverToken.String()) {
			t.Errorf("the panic does not name the auth class: %v", p)
		}
	}()

	attachRoutes(http.NewServeMux(), []Route{driverRoute()}, GroupV1, testDeps(), g)
}

// Supplying a guard is the whole of what SHIP-108 has to do to serve the class: the route attaches,
// the guard runs in front of the handler, and its refusal is the response.
func TestASuppliedDriverGuardServesAndGuardsTheRoute(t *testing.T) {
	ran := 0
	g := guardsFor(refusingGuard(&ran), nil)

	if _, enforced := g[RequireDriverToken]; !enforced {
		t.Fatal("a supplied driver guard did not reach the map, so the class stays unserved")
	}

	mux := http.NewServeMux()
	attachRoutes(mux, []Route{driverRoute()}, GroupV1, testDeps(), g)

	probe := func(credential string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/jobs/"+
			"3f1a0d9c-5a0e-4a3f-9a1b-2c7e8d4f6a11/driver-token-probe", nil)
		if credential != "" {
			req.Header.Set("X-Driver-Token-Probe", credential)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := probe(""); rec.Code != http.StatusUnauthorized {
		t.Errorf("status without a driver credential = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if rec := probe("genuine"); rec.Code != http.StatusNoContent {
		t.Errorf("status with a driver credential = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if ran != 2 {
		t.Errorf("the guard ran %d times over two requests; it is not in front of the handler", ran)
	}
}

// RequireUser is enforced whatever the driver guard is, so a change to the seam cannot take the
// mobile session's guard out with it (SHIP-44).
func TestRequireUserIsEnforcedWithOrWithoutADriverGuard(t *testing.T) {
	for name, driver := range map[string]Guard{
		"no driver guard": nil,
		"a driver guard":  refusingGuard(new(int)),
	} {
		t.Run(name, func(t *testing.T) {
			if _, enforced := guardsFor(driver, nil)[RequireUser]; !enforced {
				t.Error("RequireUser is unenforced, so every route declaring it panics at startup")
			}
		})
	}
}

// The seam and its constructor, held together.
//
// This is the test that survives SHIP-108 unchanged, and it is the one worth keeping: it asserts
// the *rule* — the class is served exactly when newDriverTokenGuard supplies a verifier — rather
// than today's answer to it. It fails if somebody makes the nil case permissive, and it keeps
// passing on the day the nil stops being nil.
func TestTheClassIsServedExactlyWhenTheConstructorSuppliesAGuard(t *testing.T) {
	cfg := testDeps().Config

	driverToken, err := newDriverTokenGuard(cfg, clock.System{})
	if err != nil {
		t.Fatalf("building the driver-token guard: %v", err)
	}

	_, enforced := guardsFor(driverToken, nil)[RequireDriverToken]
	if enforced != (driverToken != nil) {
		t.Errorf("newDriverTokenGuard returned a guard: %t, but the class is enforced: %t.\n"+
			"These must agree — a class enforced with no verifier behind it is a route that "+
			"refuses forever, and a verifier that never reaches the map is a route that panics "+
			"at startup for no reason.", driverToken != nil, enforced)
	}
}

// --- SHIP-108: the class is now filled in, and these run against the real router ----------------

// driverJobPath is the one route in the service served on a driver's credential.
const driverJobPath = "/v1/driver/jobs/"

// testDriverLink mints a real job-scoped link from the configured driver keyset.
//
// The real issuer over testDeliveryConfig's keyset, which is the same material testDriverGuard
// verifies against — so what the two tests below exercise is the wiring, not a fixture agreeing with
// itself.
func testDriverLink(t *testing.T, jobID uuid.UUID) string {
	t.Helper()

	cfg := testDeliveryConfig()
	keys, err := delivery.NewKeyset(cfg.DriverTokenKeys, cfg.DriverTokenActiveKID)
	if err != nil {
		t.Fatalf("building the driver keyset: %v", err)
	}
	issuer, err := delivery.NewDriverTokenIssuer(keys, cfg.DriverTokenTTL, clock.System{})
	if err != nil {
		t.Fatalf("building the driver token issuer: %v", err)
	}

	link, err := issuer.Issue(jobID, uuid.New())
	if err != nil {
		t.Fatalf("issuing a driver link: %v", err)
	}
	return link.Value
}

// TestNeitherTokenSystemOpensTheOthersRoutes is CLAUDE.md's invariant through the real router, in
// both directions (SHIP-108).
//
// internal/delivery proves both directions of the *parser* and internal/identity proves its own
// half; what only this package can show is that the two are wired to the routes that way — that
// ResolveSubject, the manifest's auth classes and the driver guard end up refusing each other's
// credentials on the endpoints a client actually calls.
//
// **The driver direction could not be tested at all before this ticket**, because no route accepted
// a driver token. That was the gap SHIP-107 recorded and this is what closes it in Go;
// scripts/verify/70-delivery.sh does the same pair against the running binary.
//
// Neither case needs a database: both are refused by a guard, in front of the handler that would
// have wanted one.
func TestNeitherTokenSystemOpensTheOthersRoutes(t *testing.T) {
	router := testRouter()
	jobID := uuid.New()

	access, _, _ := testAccessToken(t, identity.RoleProvider)
	link := testDriverLink(t, jobID)

	for name, tc := range map[string]struct {
		method     string
		path       string
		credential string
	}{
		"a mobile session on the driver's route": {
			method: http.MethodGet, path: driverJobPath + jobID.String(), credential: access,
		},
		"a driver link on a user route": {
			method: http.MethodGet, path: "/v1/jobs", credential: link,
		},
	} {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			req.Header.Set(httpx.HeaderAuthorization, "Bearer "+tc.credential)

			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 — one token system was accepted by the "+
					"other (Docs/10 §5, CLAUDE.md)\n  body: %s", rec.Code, rec.Body)
			}
		})
	}
}

// TestTheDriverRouteIsServedAndScopedThroughTheRealRouter checks the half of SHIP-108 that only the
// composition root can show: that the guard newDriverTokenGuard builds is the one attached to the
// route the manifest declares, and that it is scoped to the job in the path.
//
// A link presented on another job is refused with 404 here, exactly as it is in internal/delivery's
// own tests — the point of repeating it is that this one goes through guardsFor, attach and the real
// middleware chain rather than through a mux a test built.
//
// The granted job answers 503 rather than 200, and that is correct: testDeps carries no pool, so the
// handler behind the guard has no database. What matters is that it *reached* the handler, which is
// what tells the two refusals apart from a route that refuses everything.
func TestTheDriverRouteIsServedAndScopedThroughTheRealRouter(t *testing.T) {
	router := testRouter()
	granted, other := uuid.New(), uuid.New()
	link := testDriverLink(t, granted)

	open := func(job uuid.UUID) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, driverJobPath+job.String(), nil)
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+link)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	if rec := open(granted); rec.Code != http.StatusServiceUnavailable {
		t.Errorf("the link's own job answered %d, want 503 from a handler with no database "+
			"— anything else means the guard did not let it through (%s)", rec.Code, rec.Body)
	}
	if rec := open(other); rec.Code != http.StatusNotFound {
		t.Errorf("the same link opened another job with status %d, want 404 (%s)", rec.Code, rec.Body)
	}
}
