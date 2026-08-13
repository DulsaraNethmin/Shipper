package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// The RequireDriverToken seam (SHIP-15m).
//
// SHIP-108 supplies the guard by filling in newDriverTokenGuard and editing nothing shared. These
// tests hold the two halves of that arrangement — that the class is unserved while nothing supplies
// one, and that supplying one is what serves *and* guards it — so neither can be quietly lost.
//
// Every route here is built locally and handed to attachRoutes, for the reason auth_test.go states:
// registering one from a _test.go init would put it in the real registry for every other test in
// this package, which breaks TestRouteTableMatchesGolden and TestEveryRouteIsInTheContract.

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
	g := guardsFor(nil)

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
	g := guardsFor(refusingGuard(&ran))

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
			if _, enforced := guardsFor(driver)[RequireUser]; !enforced {
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

	_, enforced := guardsFor(driverToken)[RequireDriverToken]
	if enforced != (driverToken != nil) {
		t.Errorf("newDriverTokenGuard returned a guard: %t, but the class is enforced: %t.\n"+
			"These must agree — a class enforced with no verifier behind it is a route that "+
			"refuses forever, and a verifier that never reaches the map is a route that panics "+
			"at startup for no reason.", driverToken != nil, enforced)
	}
}
