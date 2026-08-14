package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The RequireAdmin seam (SHIP-15r), and the ticket that will fill it in (SHIP-147).
//
// These are driverauth_test.go's first four tests, aimed at the other class. That is deliberate and
// it is the cheapest evidence available that the seam is the *same* seam: SHIP-108 filled the driver
// half without any of those four changing, which is what makes them a specification of the rule
// rather than of today's answer to it. The same four here will survive SHIP-147 the same way.
//
// The route is built locally and handed to attachRoutes, never registered from an init — a route
// declared in a _test.go init lands in the real registry for every other test in this package, which
// breaks TestRouteTableMatchesGolden and TestEveryRouteIsInTheContract.

// adminRoute is a route in the shape SHIP-147's will be: declared RequireAdmin, with a handler that
// reports whether an administrator session has been resolved into a mobile one.
//
// The handler asserts the negative half deliberately, exactly as driverRoute does. An administrator
// session must not produce an [authctx.Subject]: `ck_users_role` permits two roles and identity
// refuses to issue a token for any other (ErrInvalidRole names this ticket), so a RequireAdmin route
// reaching its handler with a subject on the context means the two systems have been joined.
func adminRoute() Route {
	return Route{
		Method:  http.MethodPost,
		Pattern: "/admin/session-probe",
		Group:   GroupV1,
		Auth:    RequireAdmin,
		Handler: func(Deps) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if _, ok := authctx.SubjectFrom(r.Context()); ok {
					http.Error(w,
						"a RequireAdmin route produced an authctx.Subject; the administrator "+
							"session and the mobile session have been joined",
						http.StatusTeapot)
					return
				}
				w.WriteHeader(http.StatusNoContent)
			})
		},
	}
}

// adminGuardStub stands in for SHIP-147's middleware: it lets a request through only when the
// stand-in credential is present, and records that it ran.
//
// A stub rather than a verifier, and that is the point of the ticket — the seam is what is being
// tested, and there is no administrator session to sign until SHIP-147.
func adminGuardStub(ran *int) Guard {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*ran++
			if r.Header.Get("X-Admin-Session-Probe") != "genuine" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// While nothing supplies an admin guard the class is absent from the map, and a route declaring it
// stops the process.
//
// **Absent, not refusing.** An entry that rejected every request would turn this startup panic into
// a route that answers 401 forever, which is indistinguishable from an expired credential to every
// client — the weaker failure, and the one guardsFor exists to avoid.
func TestWithNoAdminGuardTheClassIsAbsentAndTheRouteRefusesToStart(t *testing.T) {
	g := guardsFor(nil, nil)

	if _, enforced := g[RequireAdmin]; enforced {
		t.Fatal("RequireAdmin is mapped to something with no verifier behind it.\n" +
			"A guard that is present but refuses everything is not a substitute for an absent " +
			"one: it serves the route and answers 401 forever instead of stopping the process.")
	}

	// And absence is what makes the route unservable, rather than merely unmapped.
	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("a route requiring an administrator session was attached with no middleware " +
				"enforcing it, which serves the admin surface to anybody")
		}
		if msg, _ := p.(string); !strings.Contains(msg, RequireAdmin.String()) {
			t.Errorf("the panic does not name the auth class: %v", p)
		}
	}()

	attachRoutes(http.NewServeMux(), []Route{adminRoute()}, GroupV1, testDeps(), g)
}

// Supplying a guard is the whole of what SHIP-147 has to do to serve the class: the route attaches,
// the guard runs in front of the handler, and its refusal is the response.
func TestASuppliedAdminGuardServesAndGuardsTheRoute(t *testing.T) {
	ran := 0
	g := guardsFor(nil, adminGuardStub(&ran))

	if _, enforced := g[RequireAdmin]; !enforced {
		t.Fatal("a supplied admin guard did not reach the map, so the class stays unserved")
	}

	mux := http.NewServeMux()
	attachRoutes(mux, []Route{adminRoute()}, GroupV1, testDeps(), g)

	probe := func(credential string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/admin/session-probe", nil)
		if credential != "" {
			req.Header.Set("X-Admin-Session-Probe", credential)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		return rec
	}

	if rec := probe(""); rec.Code != http.StatusUnauthorized {
		t.Errorf("status without an administrator credential = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if rec := probe("genuine"); rec.Code != http.StatusNoContent {
		t.Errorf("status with an administrator credential = %d, want 204 (%s)", rec.Code, rec.Body)
	}
	if ran != 2 {
		t.Errorf("the guard ran %d times over two requests; it is not in front of the handler", ran)
	}
}

// The two classes are independent, so filling one cannot take the other out with it.
//
// This is the test the driver seam did not have and now wants, because there are two optional guards
// rather than one: a `guards` literal that assigned instead of adding, or a nil check written against
// the wrong parameter, would be invisible to every other test here while nothing declares RequireAdmin.
func TestEachGuardReachesItsOwnClassAndNoOther(t *testing.T) {
	driver, admin := refusingGuard(new(int)), adminGuardStub(new(int))

	for name, tc := range map[string]struct {
		driver, admin         Guard
		wantDriver, wantAdmin bool
	}{
		"neither":     {nil, nil, false, false},
		"driver only": {driver, nil, true, false},
		"admin only":  {nil, admin, false, true},
		"both":        {driver, admin, true, true},
	} {
		t.Run(name, func(t *testing.T) {
			g := guardsFor(tc.driver, tc.admin)

			if _, enforced := g[RequireDriverToken]; enforced != tc.wantDriver {
				t.Errorf("RequireDriverToken enforced = %t, want %t", enforced, tc.wantDriver)
			}
			if _, enforced := g[RequireAdmin]; enforced != tc.wantAdmin {
				t.Errorf("RequireAdmin enforced = %t, want %t", enforced, tc.wantAdmin)
			}
			// RequireUser is unconditional, and a seam edit that dropped it would take every
			// authenticated route in the service with it (SHIP-44).
			if _, enforced := g[RequireUser]; !enforced {
				t.Error("RequireUser is unenforced, so every route declaring it panics at startup")
			}
		})
	}
}

// The seam and its constructor, held together.
//
// This is the test that survives SHIP-147 unchanged, and it is the one worth keeping: it asserts the
// *rule* — the class is served exactly when newAdminGuard supplies a verifier — rather than today's
// answer to it. It fails if somebody makes the nil case permissive, and it keeps passing on the day
// the nil stops being nil.
func TestTheAdminClassIsServedExactlyWhenTheConstructorSuppliesAGuard(t *testing.T) {
	cfg := testDeps().Config

	// nil for the pool is what main.go passes when PostgreSQL was unreachable at startup, which
	// is a state the service is built to survive — so the constructor has to as well.
	admin, err := newAdminGuard(cfg, nil, clock.System{})
	if err != nil {
		t.Fatalf("building the admin guard: %v", err)
	}

	_, enforced := guardsFor(nil, admin)[RequireAdmin]
	if enforced != (admin != nil) {
		t.Errorf("newAdminGuard returned a guard: %t, but the class is enforced: %t.\n"+
			"These must agree — a class enforced with no verifier behind it is a route that "+
			"refuses forever, and a verifier that never reaches the map is a route that panics "+
			"at startup for no reason.", admin != nil, enforced)
	}
}

// --- SHIP-147, and the direction only this package can prove -------------------------------------
//
// The four tests above are SHIP-15r's, aimed at the seam. These are the ticket's, and they are here
// rather than in internal/admin for one reason: **this is the only package where both verifiers are
// visible at once.** `admin` may not import `identity` and `identity` may not import `admin`, so a
// test that an administrator credential is refused by the *mobile* authenticator cannot be written
// in either domain. SHIP-108 put the driver pair's equivalent in delivery, because one package owns
// both halves of that one; nothing owns both halves of this one, so cmd/api does.
//
// The domain's own side — a mobile access token presented to the administrator verifier — is
// TestAMobileAccessTokenIsNotAnAdministratorSession in internal/admin, where there is a database to
// resolve against. Both directions are also exercised against the running binary with a *real*
// administrator token by scripts/verify/90-admin.sh, which is the strongest form of the evidence
// and the one a test with no database cannot produce.

// anAdministratorShapedCredential is exactly what admin.Credentials issues: 32 bytes of entropy,
// base64url encoded, 43 characters, no padding.
//
// A literal rather than a call, because admin's minting function is unexported — which is itself the
// point being relied on. What matters is the *shape*: this is byte-for-byte the kind of value an
// administrator's browser sends, and the mobile authenticator must have no path that accepts one.
const anAdministratorShapedCredential = "9dW3nP1qTzR7lK0sYbV2xC8gH4jM6oA5eF1uI3rQ7wY"

// TestAnAdministratorCredentialIsRefusedOnAMobileRoute is the second half of SHIP-147's *Done
// when*, read backwards.
//
// The ticket says an administrator session "cannot be reached with a user token". The converse has
// to hold as well, and it is the half a plausible implementation leaves open: a verifier that fell
// back to treating an unparseable credential as *something* — a session id, a lookup key — would
// join the two systems in the direction nobody thought to test.
//
// It is driven through the real router rather than against the verifier directly, so what is being
// asserted is the served behaviour: subject resolution, the RequireUser guard, and the error
// contract all in the path a browser would actually take.
func TestAnAdministratorCredentialIsRefusedOnAMobileRoute(t *testing.T) {
	router := testRouter()

	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+anAdministratorShapedCredential)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)\n"+
			"An administrator credential reached a route secured by the mobile access token. "+
			"CLAUDE.md: the credential systems are separate and neither can be exchanged for "+
			"the other.", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "unauthenticated") {
		t.Errorf("body = %s, want the `unauthenticated` code", rec.Body)
	}
}

// TestTheMobileAuthenticatorNeverResolvesAnAdministratorCredentialIntoASubject is the same
// statement one level down, and it is the one that would catch a *partial* acceptance.
//
// A 401 tells you the request was refused. It does not tell you that nothing was put on the context
// on the way past — httpx.ResolveSubject deliberately never rejects, it resolves and carries on, and
// a public route reached with this credential would then be serving somebody. So this asks the
// authenticator directly: an administrator credential must resolve to no subject at all.
func TestTheMobileAuthenticatorNeverResolvesAnAdministratorCredentialIntoASubject(t *testing.T) {
	authenticate := testAuthenticator()

	subject, err := authenticate(t.Context(), anAdministratorShapedCredential)
	if err == nil {
		t.Fatalf("an administrator credential resolved to the subject %+v", subject)
	}
	if subject.UserID != "" || subject.Role != "" || subject.SessionID != "" {
		t.Errorf("the refusal still produced subject material: %+v", subject)
	}
}
