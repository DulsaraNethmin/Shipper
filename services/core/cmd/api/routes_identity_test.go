package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
)

// The identity routes through the real router (SHIP-30 onwards).
//
// These deliberately need no database. Everything they assert happens before the pool is
// touched — the middleware chain, the auth class, validation — and a package that stayed
// DB-free is one whose tests keep running when PostgreSQL is not up. The rules that need a real
// database are tested in internal/identity, against one.
//
// No route is registered from an init in this file. Test-file inits run during `go test`, so a
// route declared here would land in the real registry and break both TestRouteTableMatchesGolden
// and TestEveryRouteIsInTheContract, which compare the served surface against committed files.

func identityRouter() http.Handler {
	return newRouter(testDeps(), idempotency.NewMemoryStore(), testAuthenticator())
}

// postJSON drives a route the way a client does, with a fresh idempotency key each time so that
// one test cannot replay another's stored response.
func postJSON(t *testing.T, path, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)
	return rec
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Code      string `json:"code"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body)
	}
	return body.Error.Code
}

// TestRegisterIsReachableAndPublic. The route is declared Public, and since SHIP-44 that
// declaration is read at wiring time rather than being decoration — so this also proves the
// class is one the router can serve rather than one it panics on.
func TestRegisterIsReachableAndPublic(t *testing.T) {
	rec := postJSON(t, "/v1/auth/register", `{"email":"","phone":"","password":"","role":""}`)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /v1/auth/register is not served")
	}
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("registration demands a credential, which is the credential it exists to produce")
	}
}

// TestRegisterRequiresAnIdempotencyKey. Registration is public and state-changing, which is
// exactly the combination a retry duplicates: two taps on a slow connection must not produce two
// accounts or a duplicate-address error for the account just created.
func TestRegisterRequiresAnIdempotencyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register",
		strings.NewReader(`{"email":"a@example.com","phone":"0412345678","password":"a-long-enough-one","role":"customer"}`))
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeIdempotencyKeyRequired) {
		t.Errorf("code = %q, want %q", got, httpx.CodeIdempotencyKeyRequired)
	}
}

// TestRegisterValidationIsReportedPerField, end to end and with no database.
//
// Validation runs before the pool is looked at, which is what makes this reachable here — and
// is also the property that stops an invalid request costing a connection.
func TestRegisterValidationIsReportedPerField(t *testing.T) {
	rec := postJSON(t, "/v1/auth/register",
		`{"email":"not-an-address","phone":"123","password":"short","role":"driver"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details []struct {
				Field string `json:"field"`
				Code  string `json:"code"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body)
	}

	if body.Error.Code != string(httpx.CodeValidationFailed) {
		t.Errorf("code = %q, want %q", body.Error.Code, httpx.CodeValidationFailed)
	}

	named := map[string]bool{}
	for _, d := range body.Error.Details {
		named[d.Field] = true
	}
	for _, want := range []string{"email", "phone", "password", "role"} {
		if !named[want] {
			t.Errorf("details do not name %q; a client cannot put the message beside the input. Got %v",
				want, named)
		}
	}
}

// TestRegisterRefusesAnUnknownField. Requests are strict in the direction responses are not: a
// client that sends `pasword` has made a mistake that would otherwise surface as "password is
// required" about a field it believes it supplied (Docs/10 §4.3).
func TestRegisterRefusesAnUnknownField(t *testing.T) {
	rec := postJSON(t, "/v1/auth/register",
		`{"email":"a@example.com","phone":"0412345678","pasword":"typo","role":"customer"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeBadRequest) {
		t.Errorf("code = %q, want %q", got, httpx.CodeBadRequest)
	}
}

// TestRegisterWithoutADatabaseIsUnavailable.
//
// The pool is nil-able by design: the service starts with an unreachable database so that a
// failover does not take every instance down at once (see the note on Deps). What must not
// happen is a 500, which tells a mobile client to give up rather than to retry.
func TestRegisterWithoutADatabaseIsUnavailable(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/register",
		strings.NewReader(`{"email":"a@example.com","phone":"0412345678","password":"a-long-enough-one","role":"customer"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator()).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnavailable) {
		t.Errorf("code = %q, want %q — 500 tells the client to give up", got, httpx.CodeUnavailable)
	}
}

// TestRequestOTPIsReachableAndPublic (SHIP-34). Public for the same reason registration is: the
// code it sends is how the caller proves the number, and nothing has issued them a session yet.
func TestRequestOTPIsReachableAndPublic(t *testing.T) {
	rec := postJSON(t, "/v1/auth/request-otp", `{"phone":""}`)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /v1/auth/request-otp is not served")
	}
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("requesting a code demands a credential the caller cannot have yet")
	}
}

// TestRequestOTPRejectsAnUnusableNumberPerField. Reported as a field error rather than as a bare
// 400, so the client can put the message beside the input.
func TestRequestOTPRejectsAnUnusableNumberPerField(t *testing.T) {
	rec := postJSON(t, "/v1/auth/request-otp", `{"phone":"123"}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeValidationFailed) {
		t.Errorf("code = %q, want %q", got, httpx.CodeValidationFailed)
	}
}

// TestRequestOTPWithoutADatabaseIsUnavailable, for the reason registration's equivalent gives:
// 503 tells a mobile client to retry and 500 tells it to give up.
func TestRequestOTPWithoutADatabaseIsUnavailable(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/request-otp",
		strings.NewReader(`{"phone":"0412345678"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator()).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
}
