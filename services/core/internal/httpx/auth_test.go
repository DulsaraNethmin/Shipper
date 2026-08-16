package httpx

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
)

// SHIP-44's middleware, driven against a mux built here.
//
// Deliberately not against the real router. cmd/api's route table is populated by init functions,
// and a test that registered a route of its own would pollute manifest() for every other test in
// that package — breaking TestRouteTableMatchesGolden and TestEveryRouteIsInTheContract, which
// compare the served surface against committed files. The middleware is ordinary net/http and
// needs none of that to be exercised.

var (
	testSubject = authctx.Subject{
		UserID:    "018f3a1e-0000-7000-8000-000000000001",
		Role:      authctx.RoleCustomer,
		SessionID: "018f3a1e-0000-7000-8000-000000000002",
	}

	errNotAToken = errors.New("test: not a token")
)

// acceptOnly returns an Authenticator that accepts exactly one credential.
func acceptOnly(good string) Authenticator {
	return func(_ context.Context, credential string) (authctx.Subject, error) {
		switch credential {
		case good:
			return testSubject, nil
		case "expired":
			return authctx.Subject{}, ErrCredentialExpired
		default:
			return authctx.Subject{}, errNotAToken
		}
	}
}

// protectedMux is the arrangement newRouter builds: resolution outside, requirement per route.
func protectedMux(t *testing.T, verify Authenticator) http.Handler {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle("GET /open", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := authctx.SubjectFrom(r.Context()); ok {
			w.Header().Set("X-Test-Saw-Subject", "true")
		}
		w.WriteHeader(http.StatusOK)
	}))
	mux.Handle("GET /closed", RequireSubject()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// MustSubject rather than SubjectFrom: reaching this handler without a subject is a
		// wiring defect, and it should be loud rather than a nil-ish comparison later.
		WriteJSON(w, http.StatusOK, authctx.MustSubject(r.Context()))
	})))

	return Chain(mux, RequestID, ResolveSubject(verify))
}

func get(t *testing.T, h http.Handler, path, authorisation string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if authorisation != "" {
		req.Header.Set(HeaderAuthorization, authorisation)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body)
	}
	if body.Error.RequestID == "" {
		t.Error("the 401 carries no request_id, which is what support asks for from a screenshot")
	}
	return body.Error.Code
}

// SHIP-44's acceptance criterion: protected routes reject missing, expired, or malformed tokens
// with a typed error.
func TestProtectedRouteRejectsWhatItShould(t *testing.T) {
	cases := []struct {
		name          string
		authorisation string
		wantCode      string
		wantChallenge string
	}{
		{
			name:          "no credential at all",
			authorisation: "",
			wantCode:      string(CodeUnauthenticated),
			wantChallenge: "Bearer",
		},
		{
			name:          "expired",
			authorisation: "Bearer expired",
			wantCode:      string(CodeTokenExpired),
			wantChallenge: `Bearer error="invalid_token", error_description="the access token has expired"`,
		},
		{
			name:          "malformed",
			authorisation: "Bearer rubbish",
			wantCode:      string(CodeUnauthenticated),
			wantChallenge: `Bearer error="invalid_token"`,
		},
		{
			name:          "the scheme with no token after it",
			authorisation: "Bearer ",
			wantCode:      string(CodeUnauthenticated),
			wantChallenge: `Bearer error="invalid_token"`,
		},
		{
			// A scheme this service does not issue is treated as no credential rather than
			// as a bad one: answering "your token is invalid" would send a client looking
			// in the wrong place.
			name:          "a scheme this service does not issue",
			authorisation: "Basic dXNlcjpwYXNz",
			wantCode:      string(CodeUnauthenticated),
			wantChallenge: "Bearer",
		},
	}

	handler := protectedMux(t, acceptOnly("good"))

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := get(t, handler, "/closed", tc.authorisation)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
			}
			if got := errorCode(t, rec); got != tc.wantCode {
				t.Errorf("error.code = %q, want %q — clients branch on this", got, tc.wantCode)
			}
			// RFC 9110 requires a challenge on a 401.
			if got := rec.Header().Get("WWW-Authenticate"); got != tc.wantChallenge {
				t.Errorf("WWW-Authenticate = %q, want %q", got, tc.wantChallenge)
			}
		})
	}
}

func TestProtectedRouteAcceptsAValidCredential(t *testing.T) {
	rec := get(t, protectedMux(t, acceptOnly("good")), "/closed", "Bearer good")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	var got authctx.Subject
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}
	if got != testSubject {
		t.Errorf("the handler saw %+v, want %+v", got, testSubject)
	}
}

// RFC 9110 §11.1 makes the scheme case-insensitive, and a client sending "bearer" is not wrong.
func TestTheBearerSchemeIsCaseInsensitive(t *testing.T) {
	for _, scheme := range []string{"Bearer", "bearer", "BEARER", "BeArEr"} {
		t.Run(scheme, func(t *testing.T) {
			rec := get(t, protectedMux(t, acceptOnly("good")), "/closed", scheme+" good")
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d with scheme %q, want 200", rec.Code, scheme)
			}
		})
	}
}

// The property that makes refresh possible. POST /v1/auth/refresh is public and is called by
// exactly the client whose access token has just expired — so a stale token attached to a public
// request must not refuse it.
func TestAPublicRouteToleratesABadCredential(t *testing.T) {
	handler := protectedMux(t, acceptOnly("good"))

	for _, authorisation := range []string{"", "Bearer expired", "Bearer rubbish"} {
		name := authorisation
		if name == "" {
			name = "none"
		}
		t.Run(name, func(t *testing.T) {
			rec := get(t, handler, "/open", authorisation)

			if rec.Code != http.StatusOK {
				t.Fatalf("a public route answered %d for %q; the endpoint that recovers "+
					"from an expired credential cannot require a valid one", rec.Code, authorisation)
			}
			if rec.Header().Get("X-Test-Saw-Subject") != "" {
				t.Error("a public route was given a subject it should not have")
			}
		})
	}
}

func TestAPublicRouteStillSeesAValidSubject(t *testing.T) {
	rec := get(t, protectedMux(t, acceptOnly("good")), "/open", "Bearer good")

	if rec.Header().Get("X-Test-Saw-Subject") != "true" {
		t.Error("a valid credential on a public route produced no subject; SubjectScope needs one " +
			"to namespace idempotency keys")
	}
}

// The 401 body must not repeat what the credential claimed, and must not say which check failed.
func TestTheRejectionSaysNothingUseful(t *testing.T) {
	forged := "eyJhbGciOiJIUzI1NiIsImtpZCI6InNlY3JldC1raWQifQ.payload.signature"

	rec := get(t, protectedMux(t, acceptOnly("good")), "/closed", "Bearer "+forged)
	body := rec.Body.String()

	for _, leak := range []string{forged, "signature", "kid", "audience", "test: not a token"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(leak)) {
			t.Errorf("the 401 body mentions %q, which tells a caller probing it what to fix:\n%s",
				leak, body)
		}
	}
}

// SubjectScope is the whole security purpose of this ticket: it is what stops one caller's
// idempotency key reading another caller's stored response.
func TestSubjectScopeSeparatesCallers(t *testing.T) {
	anonymous := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
	if got := SubjectScope(anonymous); got != "anonymous" {
		t.Errorf("SubjectScope with no subject = %q, want %q", got, "anonymous")
	}

	one := anonymous.WithContext(authctx.WithSubject(anonymous.Context(), testSubject))
	other := authctx.Subject{UserID: "018f3a1e-0000-7000-8000-0000000000ff", Role: authctx.RoleProvider}
	two := anonymous.WithContext(authctx.WithSubject(anonymous.Context(), other))

	scopeOne, scopeTwo := SubjectScope(one), SubjectScope(two)

	if scopeOne == scopeTwo {
		t.Fatalf("two callers share the idempotency scope %q; one client that guesses "+
			"another's key would be handed that client's response body", scopeOne)
	}
	if scopeOne == "anonymous" || scopeTwo == "anonymous" {
		t.Error("an authenticated caller fell back to the anonymous scope")
	}
	if !strings.Contains(scopeOne, testSubject.UserID) {
		t.Errorf("scope %q does not identify the caller", scopeOne)
	}
}

// A subject with an empty user id must not produce a distinct-looking scope: it would namespace
// every such caller together under a key that reads as though it were somebody's.
func TestSubjectScopeRefusesAnEmptyUserID(t *testing.T) {
	r := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
	r = r.WithContext(authctx.WithSubject(r.Context(), authctx.Subject{Role: authctx.RoleCustomer}))

	if got := SubjectScope(r); got != "anonymous" {
		t.Errorf("SubjectScope with an empty user id = %q, want %q", got, "anonymous")
	}
}

// ResolveSubject with no Authenticator would make every protected route permanently unreachable,
// which is a wiring mistake that should stop the process rather than serve 401s all day.
func TestResolveSubjectNeedsAnAuthenticator(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ResolveSubject(nil) did not panic")
		}
	}()
	ResolveSubject(nil)
}

// RequireSubject applied without ResolveSubject ahead of it must fail closed. It is the mistake
// a future route file makes by wiring a guard onto a group that has no resolution.
func TestRequireSubjectWithoutResolutionRefuses(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /closed", RequireSubject()(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))

	rec := get(t, Chain(mux, RequestID), "/closed", "Bearer good")

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 — a guard with no resolution ahead of it must fail closed", rec.Code)
	}
}

// --- SHIP-147b: the scope for a caller who produces no subject ------------------------------------

// scopeOf reports the scope Idempotent would compute for a request whose credential header holds
// this value.
//
// It takes the whole header rather than the token, because half of what is asserted below is about
// values that are not a bearer credential at all. It builds a request rather than calling
// SubjectScope on a hand-made one so that the parsing is part of what is under test: the scope is
// read from the same header the guard reads, and a divergence between those two readings is the
// class of defect this function is the only witness to.
func scopeOf(t *testing.T, header string) string {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notes", nil)
	if header != "" {
		req.Header.Set(HeaderAuthorization, header)
	}
	return SubjectScope(req)
}

// The clause SHIP-147b exists for, at the level this package can state it: two callers of a
// credential system that produces no authctx.Subject must not share a namespace.
//
// The end-to-end form — two real administrator sessions through the real router, compared on
// their response bodies — is cmd/api's TestTwoAdministratorSessionsDoNotShareAnIdempotencyScope,
// because it needs a database and the admin domain, and this package may have neither.
func TestSubjectScopeSeparatesTwoCredentials(t *testing.T) {
	one, two := scopeOf(t, "Bearer session-one"), scopeOf(t, "Bearer session-two")

	if one == two {
		t.Fatalf("two administrators share the idempotency scope %q; one that guessed the "+
			"other's key would be handed that administrator's response body", one)
	}
	if one == "anonymous" || two == "anonymous" {
		t.Errorf("a presented credential fell back to the anonymous scope: %q, %q", one, two)
	}
	if !strings.HasPrefix(one, credentialScopeKind+":") {
		t.Errorf("scope %q does not name the namespace it is in", one)
	}

	// The same credential is the same caller, on every request it is presented on. This is the
	// property the retry contract rests on, stated on its own so that a change which broke it
	// fails here as well as in the middleware test below.
	if again := scopeOf(t, "Bearer session-one"); again != one {
		t.Errorf("the same credential scoped to %q and then to %q; a retry would miss its own "+
			"stored response", one, again)
	}
}

// A subject and a bearer credential this service does not recognise cannot both be genuine, so
// this pins which wins — and it pins it towards the answer that was already there.
//
// It is load-bearing rather than defensive. An account holder's scope is their **account**,
// because identity has a refresh: a client whose access token expires mid-retry gets a new one and
// retries with it, and a credential-shaped scope would make that retry a different caller and
// execute the request a second time. That is the duplicate bid the invariant exists to prevent.
func TestSubjectScopePrefersTheSubject(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
	req.Header.Set(HeaderAuthorization, "Bearer some-other-credential")
	req = req.WithContext(authctx.WithSubject(req.Context(), testSubject))

	if scope := SubjectScope(req); scope != "user:"+testSubject.UserID {
		t.Errorf("scope = %q, want the subject's — an account holder's scope must not change "+
			"because they also presented something else", scope)
	}
}

// Anonymous is still where a request with nothing to say for itself goes. Every public route
// depends on it, and a caller who presented nothing must not be given a namespace built out of
// the empty string, which is one namespace shared by all of them wearing a private name.
func TestSubjectScopeIsAnonymousWithoutACredential(t *testing.T) {
	for name, header := range map[string]string{
		"no header at all":       "",
		"an empty header":        " ",
		"a scheme with no token": "Bearer",
		"an empty bearer token":  "Bearer ",
		"another scheme":         "Basic YWxpY2U6cGE1NXdvcmQ=",
	} {
		t.Run(name, func(t *testing.T) {
			if got := scopeOf(t, header); got != "anonymous" {
				t.Errorf("scope = %q, want anonymous", got)
			}
		})
	}
}

// The credential must not be recoverable from a scope, and the scope must not *be* a value some
// other part of the platform stores.
//
// `admin_sessions.token_hash` and identity's refresh-token column both hold `sha256(credential)`.
// Rendering that into a Redis key would put the database's stored verifier into the cache and into
// anything that can run `SCAN`, which is why the digest is salted.
func TestTheScopeIsNeitherTheCredentialNorItsStoredDigest(t *testing.T) {
	const credential = "V29uZGVyZnVsLXNlY3JldC10b2tlbi12YWx1ZS1oZXJl"

	scope := scopeOf(t, "Bearer "+credential)

	if strings.Contains(scope, credential) {
		t.Errorf("the scope %q contains the credential; every idempotency key in Redis would "+
			"carry a live administrator session", scope)
	}

	stored := sha256.Sum256([]byte(credential))
	if strings.Contains(scope, hex.EncodeToString(stored[:])) {
		t.Errorf("the scope %q is sha256(credential), which is what admin_sessions.token_hash "+
			"stores — the cache would be publishing the database's verifier", scope)
	}
}

// **This is the regression SHIP-147b's first form shipped, pinned at the layer that had it.**
//
// scripts/verify/90-admin.sh has asserted since SHIP-147 that a retried sign-out replays its 204,
// and states the contract it protects: the middleware answers *from outside the guard, so the dead
// credential is never consulted*, and a dropped connection therefore does not report a failed
// sign-out. Resolving the credential into the administrator it named broke that — on the retry the
// session is revoked, resolution fails, and the scope falls back to `anonymous`, where the stored
// 204 is not.
//
// The arrangement below is newRouter's, with the domain's parts replaced by the smallest thing
// that has the same shape: a session that a handler can end, and a guard that refuses once it has.
// `make check` was green while `make verify` was red, which is why this test exists in this
// package rather than only in the harness.
func TestARetriedRequestReplaysAfterItsOwnCredentialIsRevoked(t *testing.T) {
	const credential = "an-administrator-session"

	live := true
	guardCalls, handlerCalls := 0, 0

	// The per-route guard, applied *inside* Idempotent exactly as an auth class is.
	guard := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			guardCalls++
			if !live {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}

	// The endpoint whose purpose is to invalidate what it was called with.
	signOut := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		handlerCalls++
		live = false
		w.WriteHeader(http.StatusNoContent)
	})

	handler := Idempotent(idempotency.NewMemoryStore(), SubjectScope)(guard(signOut))

	signOutWith := func(key string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodDelete, "/v1/admin/sessions/current", nil)
		req.Header.Set(HeaderAuthorization, "Bearer "+credential)
		req.Header.Set(HeaderIdempotencyKey, key)

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	const key = "01J9ZK4P2M8SB3TC6VE9XA0N7D"

	if first := signOutWith(key); first.Code != http.StatusNoContent {
		t.Fatalf("signing out answered %d, want 204", first.Code)
	}
	if live {
		t.Fatal("the handler did not end the session, so this test proves nothing")
	}

	retry := signOutWith(key)
	if retry.Code != http.StatusNoContent {
		t.Errorf("a retried sign-out answered %d, want the replayed 204.\n"+
			"The scope must be stable across the revocation of the credential it is computed "+
			"from, or a dropped connection reports a failed sign-out (scripts/verify/90-admin.sh).",
			retry.Code)
	}
	if retry.Header().Get(HeaderIdempotencyReplayed) != "true" {
		t.Error("the retry was not answered from the store")
	}
	if guardCalls != 1 {
		t.Errorf("the guard ran %d times, want 1 — the replay must come from outside it, so "+
			"that the dead credential is never consulted", guardCalls)
	}
	if handlerCalls != 1 {
		t.Errorf("the handler ran %d times, want 1", handlerCalls)
	}

	// And a *fresh* key still reaches the guard, which refuses the credential the first call
	// ended. Without this the test above would be satisfied by a scope that never separated
	// anybody, or by a guard that had stopped running.
	if again := signOutWith("a-different-key"); again.Code != http.StatusUnauthorized {
		t.Errorf("a fresh key with the ended credential answered %d, want 401 — the scope is a "+
			"namespace and must never become an authorisation decision", again.Code)
	}
}
