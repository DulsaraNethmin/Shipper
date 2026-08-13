package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
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
	return newRouter(testDeps(), idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard())
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
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

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
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
}

// TestEveryIdentityRouteIsServedAndPublic (SHIP-33, SHIP-36).
//
// One table rather than a test per route, because what is being asserted is the same thing five
// times: the route reaches a handler, and the auth class it declares is one the router can
// actually serve. Since SHIP-44 the second half is real — a class with no middleware behind it
// panics at startup rather than being served open.
func TestEveryIdentityRouteIsServedAndPublic(t *testing.T) {
	for path, body := range map[string]string{
		"/v1/auth/register":      `{"email":"","phone":"","password":"","role":""}`,
		"/v1/auth/login":         `{"email":"","password":"","device_label":""}`,
		"/v1/auth/verify-email":  `{"token":""}`,
		"/v1/auth/resend-verify": `{"email":""}`,
		"/v1/auth/request-otp":   `{"phone":""}`,
		"/v1/auth/verify-phone":  `{"phone":"","code":""}`,
		"/v1/auth/refresh":       `{"refresh_token":""}`,
	} {
		t.Run(path, func(t *testing.T) {
			rec := postJSON(t, path, body)

			if rec.Code == http.StatusNotFound {
				t.Fatalf("POST %s is not served", path)
			}
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("POST %s demands a credential, which is what it exists to help produce", path)
			}
		})
	}
}

// TestVerifyEmailRefusesAnUnknownTokenThroughTheRouter (SHIP-33).
//
// Reachable with no database because an empty token is refused before the pool is looked at,
// which is also the property that stops a guessing loop costing a connection each.
func TestVerifyEmailRefusesAnEmptyToken(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/verify-email", strings.NewReader(`{"token":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != "identity_verification_token_invalid" {
		t.Errorf("code = %q, want identity_verification_token_invalid", got)
	}
}

// TestLoginIsReachableAndPublic (SHIP-41). It is the endpoint that produces the credential every
// protected route requires, so it is the one route on the allow-list whose justification needs no
// elaboration — and since SHIP-44 the class is read at wiring time rather than being decoration.
func TestLoginIsReachableAndPublic(t *testing.T) {
	rec := postJSON(t, "/v1/auth/login", `{"email":"","password":"","device_label":""}`)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /v1/auth/login is not served")
	}
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("signing in demands a credential, which is the credential it exists to produce")
	}
}

// TestLoginValidationIsReportedPerField, end to end and with no database. Every field at once
// (Docs/10 §4.6), including the device label, which is validated here rather than inside the
// session creation it is eventually for — so a client is not told about it only after its
// password has been verified.
func TestLoginValidationIsReportedPerField(t *testing.T) {
	rec := postJSON(t, "/v1/auth/login", `{"email":"not-an-address","password":"","device_label":""}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body)
	}

	var body struct {
		Error struct {
			Code    string `json:"code"`
			Details []struct {
				Field string `json:"field"`
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
	for _, want := range []string{"email", "password", "device_label"} {
		if !named[want] {
			t.Errorf("details do not name %q; a client cannot put the message beside the input. Got %v",
				want, named)
		}
	}
}

// TestLoginRequiresAnIdempotencyKey. Signing in creates a row: a retry after a dropped connection
// would otherwise leave a device session nobody holds a token for, live for thirty days and
// visible to its owner only as a duplicate line in their device list.
func TestLoginRequiresAnIdempotencyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"a@example.com","password":"a-long-enough-one","device_label":"iPhone"}`))
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

// TestLoginRefusesAnUnknownField, in the direction responses are not strict. A client sending
// `deviceLabel` has made a mistake that would otherwise surface as "device_label is required"
// about a field it believes it supplied.
func TestLoginRefusesAnUnknownField(t *testing.T) {
	rec := postJSON(t, "/v1/auth/login",
		`{"email":"a@example.com","password":"a-long-enough-one","deviceLabel":"iPhone"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeBadRequest) {
		t.Errorf("code = %q, want %q", got, httpx.CodeBadRequest)
	}
}

// TestLoginWithoutADatabaseIsUnavailable, for the reason registration's equivalent gives: 503
// tells a mobile client to retry and 500 tells it to give up. The body is valid so the request
// reaches the pool check rather than being refused on its way in.
func TestLoginWithoutADatabaseIsUnavailable(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/login",
		strings.NewReader(`{"email":"a@example.com","password":"a-long-enough-one","device_label":"iPhone"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnavailable) {
		t.Errorf("code = %q, want %q — 500 tells the client to give up", got, httpx.CodeUnavailable)
	}
}

// TestLogoutRefusesACallerWithNoCredential (SHIP-43).
//
// The first route in the service with Auth: RequireUser, and so the first place httpx.RequireSubject
// is reached through the real wiring rather than through a route a test built for itself. The
// Idempotency-Key is sent deliberately: the middleware chain checks it further out than the auth
// class is enforced, so a request missing both is refused for the key and never reaches the guard.
func TestLogoutRefusesACallerWithNoCredential(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("no WWW-Authenticate challenge on a 401, which RFC 9110 requires")
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnauthenticated) {
		t.Errorf("code = %q, want %q", got, httpx.CodeUnauthenticated)
	}
}

// TestLogoutRefusesAnExpiredCredentialDistinctly. The one authentication failure a client acts on
// differently: refresh and retry, rather than sign the user out (SHIP-50).
func TestLogoutRefusesAnExpiredCredentialDistinctly(t *testing.T) {
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

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+stale.Value)

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeTokenExpired) {
		t.Errorf("code = %q, want %q — a client that cannot tell an expired token from a bad "+
			"one signs the user out instead of refreshing", got, httpx.CodeTokenExpired)
	}
}

// TestLogoutRequiresAnIdempotencyKey. It is state-changing like every other POST, and the
// requirement is checked before the auth class — so this also pins the ordering the middleware
// chain depends on (Docs/10 §4.2).
func TestLogoutRequiresAnIdempotencyKey(t *testing.T) {
	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeIdempotencyKeyRequired) {
		t.Errorf("code = %q, want %q", got, httpx.CodeIdempotencyKeyRequired)
	}
}

// TestLogoutWithACredentialButNoDatabaseIsUnavailable. The credential got the caller past the
// guard, which is the half this proves: a 401 here would mean the route is unreachable rather
// than that the database is away.
func TestLogoutWithACredentialButNoDatabaseIsUnavailable(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/logout", nil)
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnavailable) {
		t.Errorf("code = %q, want %q", got, httpx.CodeUnavailable)
	}
}

// TestTheDeviceListRefusesACallerWithNoCredential (SHIP-46).
//
// A GET, so nothing in the middleware chain answers before the auth class does — which makes this
// the shortest statement of the property in the package: the route exists, and it is unreachable
// without a credential.
func TestTheDeviceListRefusesACallerWithNoCredential(t *testing.T) {
	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/auth/sessions", nil))

	if rec.Code == http.StatusNotFound {
		t.Fatal("GET /v1/auth/sessions is not served")
	}
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if rec.Header().Get("WWW-Authenticate") == "" {
		t.Error("no WWW-Authenticate challenge on a 401, which RFC 9110 requires")
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnauthenticated) {
		t.Errorf("code = %q, want %q", got, httpx.CodeUnauthenticated)
	}
}

// TestRevokingADeviceRefusesACallerWithNoCredential. The Idempotency-Key is sent because the
// middleware checks it further out than the auth class is enforced.
func TestRevokingADeviceRefusesACallerWithNoCredential(t *testing.T) {
	req := httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/"+uuid.New().String(), nil)
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnauthenticated) {
		t.Errorf("code = %q, want %q", got, httpx.CodeUnauthenticated)
	}
}

// TestRevokingADeviceRequiresAnIdempotencyKey. DELETE is state-changing, and the requirement is
// not limited to the methods that carry a body.
func TestRevokingADeviceRequiresAnIdempotencyKey(t *testing.T) {
	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	req := httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/"+uuid.New().String(), nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeIdempotencyKeyRequired) {
		t.Errorf("code = %q, want %q", got, httpx.CodeIdempotencyKeyRequired)
	}
}

// TestRevokingAnIdentifierThatIsNotAUUIDIsANotFound.
//
// Reachable with no database, which is also the point: a malformed identifier is refused before
// the pool is looked at, and it is refused with the answer a session belonging to somebody else
// gets. A distinct "that is not a uuid" would tell a caller probing identifiers which of their
// guesses were at least the right shape.
func TestRevokingAnIdentifierThatIsNotAUUIDIsANotFound(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	req := httptest.NewRequest(http.MethodDelete, "/v1/auth/sessions/not-a-uuid", nil)
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != "identity_session_not_found" {
		t.Errorf("code = %q, want identity_session_not_found", got)
	}
}

// TestTheDeviceListWithACredentialButNoDatabaseIsUnavailable. The credential got the caller past
// the guard, which is the half worth pinning: a 401 here would mean the route is unreachable
// rather than that the database is away.
func TestTheDeviceListWithACredentialButNoDatabaseIsUnavailable(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/sessions", nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnavailable) {
		t.Errorf("code = %q, want %q", got, httpx.CodeUnavailable)
	}
}

// TestTheDeviceListNeedsNoIdempotencyKey. It is read-only, and the middleware lets safe methods
// through untouched — a key stored against a request that changes nothing is a key stored for no
// reason.
func TestTheDeviceListNeedsNoIdempotencyKey(t *testing.T) {
	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	req := httptest.NewRequest(http.MethodGet, "/v1/auth/sessions", nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)

	if got := errorCode(t, rec); got == string(httpx.CodeIdempotencyKeyRequired) {
		t.Error("a read-only endpoint demanded an Idempotency-Key")
	}
}

// TestRefreshIsReachableAndPublic (SHIP-42).
//
// Public, and it has to be: the caller is the client whose access token has just expired, so
// requiring one would lock them out of the endpoint that replaces it. Since SHIP-44 the class is
// read at wiring time, so this also proves the router can serve it rather than panicking on it.
func TestRefreshIsReachableAndPublic(t *testing.T) {
	rec := postJSON(t, "/v1/auth/refresh", `{"refresh_token":""}`)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /v1/auth/refresh is not served")
	}
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("refreshing demands a credential, which is the credential it exists to replace")
	}
}

// TestRefreshRefusesAnUnusableTokenWithOneCode (SHIP-40, SHIP-42).
//
// Reachable with no database because an empty token is refused before the pool is looked at,
// which is also what stops a guessing loop costing a connection each. The status is deliberately
// 400 rather than 401: SHIP-50's interceptor refreshes on a 401, and a 401 from this endpoint is
// the one answer that can send it round the loop again.
func TestRefreshRefusesAnUnusableTokenWithOneCode(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh", strings.NewReader(`{"refresh_token":""}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != "identity_refresh_token_invalid" {
		t.Errorf("code = %q, want identity_refresh_token_invalid", got)
	}
}

// TestRefreshRequiresAnIdempotencyKey. Rotation is the most retry-sensitive request in the
// platform: a client that fires the same refresh twice with two keys revokes its own session
// (SHIP-40), and the middleware replaying the first response is what makes an honest retry safe.
func TestRefreshRequiresAnIdempotencyKey(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh",
		strings.NewReader(`{"refresh_token":"hV8pQ2mXk4tZ7nR1bY6wJ3sL0aD5cF9gE2iU8oT4xM7"}`))
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

// TestRefreshWithoutADatabaseIsUnavailable, for the reason registration's equivalent gives: 503
// tells a mobile client to retry and 500 tells it to give up. The token is non-empty so that the
// request reaches the pool check rather than being refused on its way in.
func TestRefreshWithoutADatabaseIsUnavailable(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/refresh",
		strings.NewReader(`{"refresh_token":"hV8pQ2mXk4tZ7nR1bY6wJ3sL0aD5cF9gE2iU8oT4xM7"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnavailable) {
		t.Errorf("code = %q, want %q — 500 tells the client to give up", got, httpx.CodeUnavailable)
	}
}

// TestRefreshRefusesAnUnknownField, in the direction responses are not strict. A client that
// sends `refreshToken` has made a mistake that would otherwise surface as "the session has
// ended" about a token it believes it supplied.
func TestRefreshRefusesAnUnknownField(t *testing.T) {
	rec := postJSON(t, "/v1/auth/refresh", `{"refreshToken":"hV8pQ2mXk4tZ7nR1bY6wJ3sL0aD5cF9g"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeBadRequest) {
		t.Errorf("code = %q, want %q", got, httpx.CodeBadRequest)
	}
}
