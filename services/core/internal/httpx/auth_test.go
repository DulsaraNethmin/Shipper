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
