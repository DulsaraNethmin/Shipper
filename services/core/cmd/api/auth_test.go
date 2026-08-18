package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
)

// The auth class becoming load-bearing (SHIP-44).
//
// Every route here is built locally and handed to attachRoutes. Registering one from an init in a
// _test.go file would put it in the real registry for every other test in this package, which
// breaks TestRouteTableMatchesGolden and TestEveryRouteIsInTheContract — both compare the served
// surface with a committed file, and neither knows a test route from a real one.

// echoSubjectRoute is a protected route whose handler reports who the platform decided is calling.
func echoSubjectRoute(auth Auth) Route {
	return Route{
		Method:  http.MethodGet,
		Pattern: "/whoami",
		Group:   GroupV1,
		Auth:    auth,
		Limit:   LimitWrite,
		Handler: func(Deps) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				subject, ok := authctx.SubjectFrom(r.Context())
				if !ok {
					// Reached only if a protected route were served unguarded, which is
					// the defect these tests exist for. Say so rather than 500.
					http.Error(w, "no subject", http.StatusTeapot)
					return
				}
				httpx.WriteJSON(w, http.StatusOK, subject)
			})
		},
	}
}

// routerFor builds the same arrangement newRouter does, over routes this test owns.
func routerFor(t *testing.T, rs []Route, g guards) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	attachRoutes(mux, rs, GroupV1, testDeps(), g, testLimiter())

	return httpx.Chain(
		http.StripPrefix(apiPrefix, httpx.ResolveSubject(testAuthenticator())(mux)),
		httpx.RequestID,
	)
}

// SHIP-44's acceptance criterion end to end, through the real verifier, the real translation in
// auth.go and the real middleware: a protected route rejects missing, expired and malformed
// tokens, and accepts a genuine one.
func TestProtectedRouteThroughTheRealWiring(t *testing.T) {
	handler := routerFor(t, []Route{echoSubjectRoute(RequireUser)},
		guards{RequireUser: httpx.RequireSubject()})

	token, userID, sessionID := testAccessToken(t, identity.RoleProvider)

	t.Run("a genuine token is accepted and identifies the caller", func(t *testing.T) {
		rec := whoami(t, handler, "Bearer "+token)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
		}

		var got authctx.Subject
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatalf("response is not JSON: %v", err)
		}

		// The conversion in auth.go, checked rather than assumed: identity.Role and
		// authctx.Role are different types with the same values, and a cast that quietly
		// produced an unrecognised role would read as "may do nothing" rather than as an
		// error.
		want := authctx.Subject{
			UserID:    userID.String(),
			Role:      authctx.RoleProvider,
			SessionID: sessionID.String(),
		}
		if got != want {
			t.Errorf("subject = %+v, want %+v", got, want)
		}
	})

	t.Run("no credential", func(t *testing.T) {
		expect401(t, whoami(t, handler, ""), httpx.CodeUnauthenticated)
	})

	t.Run("a malformed token", func(t *testing.T) {
		expect401(t, whoami(t, handler, "Bearer not-a-token"), httpx.CodeUnauthenticated)
	})

	t.Run("a token signed by somebody else", func(t *testing.T) {
		keys, err := identity.NewKeyset(
			map[string][]byte{testKID: []byte("a-completely-different-32-byte-k")}, testKID)
		if err != nil {
			t.Fatalf("building the foreign keyset: %v", err)
		}
		issuer, err := identity.NewAccessTokenIssuer(keys, 15*time.Minute, clock.System{})
		if err != nil {
			t.Fatalf("building the foreign issuer: %v", err)
		}
		forged, err := issuer.Issue(uuid.New(), uuid.New(), identity.RoleCustomer)
		if err != nil {
			t.Fatalf("issuing the forged token: %v", err)
		}

		expect401(t, whoami(t, handler, "Bearer "+forged.Value), httpx.CodeUnauthenticated)
	})
}

// Expiry is reported distinctly all the way out to the wire, because it is the one failure the
// Flutter client acts on differently — refresh rather than sign out (SHIP-50).
func TestAnExpiredTokenIsReportedAsExpired(t *testing.T) {
	// A clock fifteen minutes behind mints a token that is already expired by the time the
	// verifier — which runs on the real clock — looks at it.
	past := clock.NewFixed(time.Now().Add(-time.Hour).UTC())

	keys, err := identity.NewKeyset(map[string][]byte{testKID: testSigningKey}, testKID)
	if err != nil {
		t.Fatalf("building the keyset: %v", err)
	}
	issuer, err := identity.NewAccessTokenIssuer(keys, 15*time.Minute, past)
	if err != nil {
		t.Fatalf("building the issuer: %v", err)
	}
	stale, err := issuer.Issue(uuid.New(), uuid.New(), identity.RoleCustomer)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}

	handler := routerFor(t, []Route{echoSubjectRoute(RequireUser)},
		guards{RequireUser: httpx.RequireSubject()})

	expect401(t, whoami(t, handler, "Bearer "+stale.Value), httpx.CodeTokenExpired)
}

// An auth class nothing enforces must stop the process rather than be served open. This is the
// shape SHIP-108's driver token and SHIP-147's admin session are in today: declarable in the
// manifest, and deliberately not yet wired.
func TestARouteWhoseAuthClassIsUnenforcedRefusesToStart(t *testing.T) {
	for _, auth := range []Auth{RequireUser, RequireDriverToken, RequireAdmin} {
		t.Run(auth.String(), func(t *testing.T) {
			defer func() {
				p := recover()
				if p == nil {
					t.Fatalf("a route requiring %s was attached with no middleware "+
						"enforcing it, which serves it to anybody", auth)
				}
				if msg, _ := p.(string); !strings.Contains(msg, auth.String()) {
					t.Errorf("the panic does not name the auth class: %v", p)
				}
			}()

			attachRoutes(http.NewServeMux(), []Route{echoSubjectRoute(auth)},
				GroupV1, testDeps(), nil, testLimiter())
		})
	}
}

// A public route is attached untouched, so the guard map cannot accidentally become a
// requirement for everything.
func TestAPublicRouteNeedsNoGuard(t *testing.T) {
	mux := http.NewServeMux()
	attachRoutes(mux, []Route{{
		Method:  http.MethodGet,
		Pattern: "/open",
		Group:   GroupV1,
		Auth:    Public,
		Limit:   LimitPublicRead,
		Handler: func(Deps) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
		},
	}}, GroupV1, testDeps(), nil, testLimiter())

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/open", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("a public route answered %d with no guards configured, want 200", rec.Code)
	}
}

// Operational routes sit outside the version group, and so outside ResolveSubject. One declaring
// an auth class would be a route that can never be satisfied — newRouter passes no guards there,
// which makes it a startup panic; this makes it a test failure first, with the reason attached.
func TestOperationalRoutesArePublic(t *testing.T) {
	for _, r := range routes() {
		if r.Group != GroupOperational {
			continue
		}
		if r.Auth != Public {
			t.Errorf("operational route %s %s requires %s.\n"+
				"Subject resolution is applied to the /v1 group only, so nothing outside it can "+
				"be satisfied. Move the route into the version group, or extend newRouter to "+
				"resolve subjects on the root mux as well.", r.Method, r.fullPath(), r.Auth)
		}
	}
}

// SHIP-44's other half, and the reason Docs/11 §8 calls it a gate: the idempotency middleware is
// no longer wired with a nil scope.
//
// This drives the **real** newRouter, and it works because of a documented quirk — within /v1 the
// idempotency requirement is checked before routing, so even a 404 is claimed against the key and
// stored. Two callers can therefore be compared without any mutating route existing yet.
//
// The fingerprint is method, path and body, and all three are identical here. **The scope is the
// only thing that separates these two requests.** With the nil scope this shipped with, the
// second caller is handed the first caller's stored response body; with SubjectScope it is not.
func TestOneCallersIdempotencyKeyCannotReadAnothers(t *testing.T) {
	router := newRouter(testDeps(), idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard())

	alice, _, _ := testAccessToken(t, identity.RoleCustomer)
	bob, _, _ := testAccessToken(t, identity.RoleProvider)

	const sharedKey = "01J8XV3M9K7QW2ZR4T6Y8N0P1C"

	post := func(t *testing.T, token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/v1/not-a-real-endpoint",
			strings.NewReader(`{"amount":1}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpx.HeaderIdempotencyKey, sharedKey)
		req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	first := post(t, alice)
	if replayed := first.Header().Get(httpx.HeaderIdempotencyReplayed); replayed != "" {
		t.Fatalf("the first request was already a replay (%q); the store is not empty", replayed)
	}

	second := post(t, bob)
	if second.Header().Get(httpx.HeaderIdempotencyReplayed) == "true" {
		t.Error("a second caller using the same idempotency key was handed the first caller's\n" +
			"stored response. The idempotency scope is shared, which is the hole SHIP-44 exists\n" +
			"to close — check that newRouter passes httpx.SubjectScope and that ResolveSubject\n" +
			"is applied outside Idempotent (Docs/10 §4.2).")
	}

	// And the mechanism still works within one caller, or the fix above would be a way of
	// disabling idempotency rather than of scoping it.
	replay := post(t, alice)
	if replay.Header().Get(httpx.HeaderIdempotencyReplayed) != "true" {
		t.Error("the same caller repeating the same key did not get a replay; idempotency has " +
			"been scoped into uselessness rather than scoped correctly")
	}
}

// The anonymous scope still has to work, because every public route uses it and SHIP-30 lands on
// one. Two unauthenticated callers legitimately share a namespace — Docs/11 §6 explains why that
// is safe: the fingerprint covers the body, so reading a stranger's response means already
// holding the secret material in their request.
func TestAnonymousCallersStillGetIdempotency(t *testing.T) {
	router := newRouter(testDeps(), idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard())

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/v1/not-a-real-endpoint", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set(httpx.HeaderIdempotencyKey, "anonymous-caller-key")
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	post()
	if post().Header().Get(httpx.HeaderIdempotencyReplayed) != "true" {
		t.Error("an unauthenticated caller repeating a key got no replay; the anonymous scope " +
			"has stopped working")
	}
}

func whoami(t *testing.T, h http.Handler, authorisation string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/whoami", nil)
	if authorisation != "" {
		req.Header.Set(httpx.HeaderAuthorization, authorisation)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func expect401(t *testing.T, rec *httptest.ResponseRecorder, want httpx.Code) {
	t.Helper()

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("no WWW-Authenticate challenge on a 401, which RFC 9110 requires")
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body)
	}
	if body.Error.Code != string(want) {
		t.Errorf("error.code = %q, want %q — clients branch on this", body.Error.Code, want)
	}
}
