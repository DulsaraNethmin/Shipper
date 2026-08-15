package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
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

// --- SHIP-147b: the second resolver -------------------------------------------------------------

// resolverFor recognises one credential, rejects every other, and counts how many times it was
// asked.
//
// The count is what proves the laziness the file comment claims. Resolving an administrator
// session is a database read, and a resolver called on every request would pay for one on every
// GET whose scope is never computed.
func resolverFor(credential string, p Principal, calls *int) PrincipalResolver {
	return func(_ context.Context, presented string) (Principal, error) {
		if calls != nil {
			*calls++
		}
		if presented != credential {
			return Principal{}, errNotAToken
		}
		return p, nil
	}
}

// scopeOf runs a request through ResolvePrincipal and reports the scope Idempotent would compute.
//
// It drives the middleware rather than calling SubjectScope on a hand-built context, because the
// context carrier is unexported and a test that reached around it would be asserting on a shape
// this package could change without anybody noticing.
func scopeOf(t *testing.T, credential string, resolvers ...PrincipalResolver) string {
	t.Helper()

	var scope string
	handler := ResolvePrincipal(resolvers...)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		scope = SubjectScope(r)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notes", nil)
	if credential != "" {
		req.Header.Set(HeaderAuthorization, "Bearer "+credential)
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)
	return scope
}

// The clause SHIP-147b exists for, at the level this package can state it: two callers of a
// credential system that produces no authctx.Subject must not share a namespace.
//
// The end-to-end form — two real administrator sessions through the real router, compared on
// their response bodies — is cmd/api's TestTwoAdministratorSessionsDoNotShareAnIdempotencyScope,
// because it needs a database and the admin domain, and this package may have neither.
func TestSubjectScopeSeparatesTwoPrincipals(t *testing.T) {
	one := Principal{Kind: PrincipalAdmin, ID: "018f3a1e-0000-7000-8000-00000000000a"}
	two := Principal{Kind: PrincipalAdmin, ID: "018f3a1e-0000-7000-8000-00000000000b"}

	resolve := func(_ context.Context, presented string) (Principal, error) {
		switch presented {
		case "one":
			return one, nil
		case "two":
			return two, nil
		}
		return Principal{}, errNotAToken
	}

	scopeOne, scopeTwo := scopeOf(t, "one", resolve), scopeOf(t, "two", resolve)

	if scopeOne == scopeTwo {
		t.Fatalf("two administrators share the idempotency scope %q; one that guessed the "+
			"other's key would be handed that administrator's response body", scopeOne)
	}
	if scopeOne == "anonymous" || scopeTwo == "anonymous" {
		t.Errorf("a verified principal fell back to the anonymous scope: %q, %q", scopeOne, scopeTwo)
	}
	if !strings.Contains(scopeOne, one.ID) || !strings.HasPrefix(scopeOne, string(PrincipalAdmin)+":") {
		t.Errorf("scope %q does not name the administrator and the system they came from", scopeOne)
	}

	// A credential no resolver recognises is still anonymous. Every public route depends on it,
	// and a resolver that claimed unknown credentials would give any caller a private namespace
	// of their own choosing.
	if got := scopeOf(t, "neither", resolve); got != "anonymous" {
		t.Errorf("an unrecognised credential scoped to %q, want anonymous", got)
	}
	if got := scopeOf(t, "", resolve); got != "anonymous" {
		t.Errorf("a request with no credential scoped to %q, want anonymous", got)
	}
}

// A subject and a principal cannot both be genuine — the three credential systems are separate and
// none is exchangeable for another — so this pins which wins if a wiring mistake ever produced
// both, and it pins it towards the answer that was already there.
func TestSubjectScopePrefersTheSubject(t *testing.T) {
	resolve := resolverFor("both", Principal{Kind: PrincipalAdmin, ID: "an-administrator"}, nil)

	var scope string
	handler := ResolvePrincipal(resolve)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		scope = SubjectScope(r)
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/jobs", nil)
	req.Header.Set(HeaderAuthorization, "Bearer both")
	req = req.WithContext(authctx.WithSubject(req.Context(), testSubject))

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if scope != "user:"+testSubject.UserID {
		t.Errorf("scope = %q, want the subject's — an account holder's scope must not change "+
			"because something else also recognised their credential", scope)
	}
}

// The closed kind set, from the direction that matters: a resolver cannot put a caller into the
// namespace an account holder is already using.
//
// `user:` is what SubjectScope writes for a subject, so a resolver returning PrincipalKind("user")
// with somebody's user identifier would land in that person's scope exactly.
func TestSubjectScopeRefusesAPrincipalItCannotNamespace(t *testing.T) {
	for name, p := range map[string]Principal{
		"an unknown kind":   {Kind: PrincipalKind("something"), ID: "an-id"},
		"the user kind":     {Kind: PrincipalKind("user"), ID: testSubject.UserID},
		"no kind at all":    {Kind: "", ID: "an-id"},
		"no identifier":     {Kind: PrincipalAdmin, ID: ""},
		"the anonymous one": {Kind: PrincipalKind("anonymous"), ID: "an-id"},
	} {
		t.Run(name, func(t *testing.T) {
			got := scopeOf(t, "credential", resolverFor("credential", p, nil))
			if got != "anonymous" {
				t.Errorf("SubjectScope built %q out of %+v.\n"+
					"The kind set is closed so that a resolver in cmd/api cannot choose a "+
					"namespace somebody else is in.", got, p)
			}
		})
	}
}

// The laziness the design rests on. Resolving an administrator session is a database read; a
// console makes several requests a second, and only its state-changing ones are ever scoped.
func TestResolvePrincipalDoesNotResolveUntilTheScopeIsAsked(t *testing.T) {
	calls := 0
	p := Principal{Kind: PrincipalAdmin, ID: "an-administrator"}

	var first, second string
	handler := ResolvePrincipal(resolverFor("good", p, &calls))(
		http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			if calls != 0 {
				t.Errorf("the resolver ran %d times before anything asked for a scope; "+
					"every read request would pay for a session lookup it never uses", calls)
			}
			first, second = SubjectScope(r), SubjectScope(r)
		}))

	req := httptest.NewRequest(http.MethodPost, "/v1/admin/notes", nil)
	req.Header.Set(HeaderAuthorization, "Bearer good")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if calls != 1 {
		t.Errorf("the resolver ran %d times for two scope computations, want 1 — the answer is "+
			"memoised per request", calls)
	}
	if first != second || first != string(PrincipalAdmin)+":an-administrator" {
		t.Errorf("scopes = %q and %q, want both %q", first, second, "admin:an-administrator")
	}
}

// Resolvers are tried in order and the first that answers wins.
//
// This is the property that makes the driver half of Docs/11 §9 a cmd/api change rather than
// another edit to this package: a second closure, over internal/delivery, appended to the call in
// newRouter. Nothing here needs to know it happened.
func TestResolvePrincipalTriesEveryResolverInOrder(t *testing.T) {
	adminCalls, driverCalls := 0, 0
	admin := resolverFor("an-admin-session", Principal{Kind: PrincipalAdmin, ID: "a"}, &adminCalls)
	driver := resolverFor("a-driver-token", Principal{Kind: PrincipalDriver, ID: "d"}, &driverCalls)

	if got := scopeOf(t, "a-driver-token", admin, driver); got != "driver:d" {
		t.Errorf("scope = %q, want driver:d — the second resolver was not tried", got)
	}
	if adminCalls != 1 {
		t.Errorf("the first resolver ran %d times, want 1", adminCalls)
	}

	// And the first still wins when it is the one that answers, without the second being asked.
	adminCalls, driverCalls = 0, 0
	if got := scopeOf(t, "an-admin-session", admin, driver); got != "admin:a" {
		t.Errorf("scope = %q, want admin:a", got)
	}
	if driverCalls != 0 {
		t.Errorf("the second resolver ran %d times after the first answered, want 0", driverCalls)
	}
}

// A nil resolver is the wiring mistake whose consequence is silent: every administrative route
// serves normally and every administrator shares one namespace.
func TestResolvePrincipalRefusesANilResolver(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("ResolvePrincipal(nil) did not panic; a resolver that is not there is a " +
				"scope that silently falls back to anonymous")
		}
	}()
	ResolvePrincipal(nil)
}

// No resolvers at all is legitimate — a deployment with no credential system but the access token
// — and must not break the subject scope or the anonymous one.
func TestResolvePrincipalWithNoResolversIsAPassthrough(t *testing.T) {
	if got := scopeOf(t, "anything"); got != "anonymous" {
		t.Errorf("scope = %q, want anonymous", got)
	}

	var scope string
	ResolvePrincipal()(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		scope = SubjectScope(r)
	})).ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/v1/jobs", nil).
			WithContext(authctx.WithSubject(context.Background(), testSubject)))

	if scope != "user:"+testSubject.UserID {
		t.Errorf("scope = %q, want the subject's", scope)
	}
}
