package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
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
	return newRouter(testDeps(), idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard())
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
	rec := postJSON(t, "/v1/auth/register", `{"name":"","email":"","phone":"","password":"","role":""}`)

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
		strings.NewReader(`{"name":"Alice Nguyen","email":"a@example.com","phone":"0412345678","password":"a-long-enough-one","role":"customer"}`))
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
		`{"name":"","email":"not-an-address","phone":"123","password":"short","role":"driver"}`)

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
	for _, want := range []string{"name", "email", "phone", "password", "role"} {
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
		`{"name":"Alice Nguyen","email":"a@example.com","phone":"0412345678","pasword":"typo","role":"customer"}`)

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
		strings.NewReader(`{"name":"Alice Nguyen","email":"a@example.com","phone":"0412345678","password":"a-long-enough-one","role":"customer"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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
		"/v1/auth/register":      `{"name":"","email":"","phone":"","password":"","role":""}`,
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
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

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

// TestAccountDeletionRefusesACallerWithNoCredential (SHIP-169).
//
// The first route this domain serves outside /v1/auth, and the property that has to hold before
// anything else about it matters: asking for an account to be deleted is unreachable without a
// credential, so the account being deleted is always the caller's own. The Idempotency-Key is sent
// deliberately — the middleware checks it further out than the auth class is enforced, so a request
// missing both is refused for the key and never reaches the guard.
func TestAccountDeletionRefusesACallerWithNoCredential(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/v1/account/deletion", nil)
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())

	rec := httptest.NewRecorder()
	identityRouter().ServeHTTP(rec, req)

	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /v1/account/deletion is not served")
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

// TestAccountDeletionRequiresAnIdempotencyKey.
//
// A deletion request is a state change and carries a key like every other one. What the key buys
// here is narrower than usual and worth naming: the database already refuses a second open request
// (uq_account_deletion_requests_open), so a retry cannot produce two rows either way — the key is
// what makes the *answer* to a retry the same bytes as the answer to the original, rather than the
// 200-instead-of-202 a re-execution would produce.
func TestAccountDeletionRequiresAnIdempotencyKey(t *testing.T) {
	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	req := httptest.NewRequest(http.MethodPost, "/v1/account/deletion", nil)
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

// TestAccountDeletionWithACredentialButNoDatabaseIsUnavailable. The credential got the caller past
// the guard, which is the half this proves: a 401 here would mean the route is unreachable rather
// than that the database is away.
func TestAccountDeletionWithACredentialButNoDatabaseIsUnavailable(t *testing.T) {
	deps := testDeps()
	deps.Pool = nil

	token, _, _ := testAccessToken(t, identity.RoleCustomer)

	req := httptest.NewRequest(http.MethodPost, "/v1/account/deletion", nil)
	req.Header.Set(httpx.HeaderIdempotencyKey, t.Name())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	newRouter(deps, idempotency.NewMemoryStore(), testLimiter(), testAuthenticator(), testDriverGuard(), testAdminGuard()).ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if got := errorCode(t, rec); got != string(httpx.CodeUnavailable) {
		t.Errorf("code = %q, want %q — 500 tells the client to give up", got, httpx.CodeUnavailable)
	}
}

// --- SHIP-170: the active-job lookup, against real rows ---------------------------------------
//
// The rest of this file is deliberately database-free, and this section deliberately is not.
//
// [activeJobLookup] is the half of SHIP-170 that `internal/identity` cannot hold: it names `jobs`
// and `bids`, which belong to two other domains, and a domain may not import another. So the domain
// tests answer through an [identity.ActiveJobs] they control — which proves the *rule* and can prove
// nothing about the statement behind it — and this proves the statement. [jobBidders] in
// routes_jobs_test.go sits in exactly the same position and is the precedent, down to the fixture
// helpers, which are shared with it because test helpers do not cross a package boundary in Go.
//
// The two failures this catches and the domain tests cannot: a predicate that sees only the
// customer, and a status set that is not Docs/05 §3.1's range.

// moveJobTo walks a job to a status through the guard SHIP-57a installed.
//
// The status column is not settable (`jobs_status_change_is_guarded`, 000402): an UPDATE has to be
// accompanied, in the same transaction, by a `job_status_history` row describing it and named to
// the trigger through a transaction-local setting. This is that protocol, and it is the same one
// `move_job` in scripts/verify/50-jobs.sh runs — which is 000402's own intention, that a fixture and
// the domain be the same caller rather than the fixture having a private way in.
//
// **It is not a claim about which transitions Docs/02 §2 permits.** The database does not hold that
// table — the guarded function in `internal/jobs` does — so this helper will happily record a move
// the product forbids. That is what lets the walk below cover Docs/02 §1's whole vocabulary without
// having to route through eleven legal transitions, and the walk says so where it uses it.
func moveJobTo(t *testing.T, pool *pgxpool.Pool, job, actor uuid.UUID, from, to string) {
	t.Helper()

	tx, err := pool.Begin(t.Context())
	if err != nil {
		t.Fatalf("opening the transition transaction: %v", err)
	}
	defer func() { _ = tx.Rollback(t.Context()) }()

	var transition uuid.UUID
	if err := tx.QueryRow(t.Context(), `
		INSERT INTO job_status_history
		    (id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
		VALUES (gen_random_uuid(), $1, $2, $3, 'customer', $4, now())
		RETURNING id`, job, from, to, actor).Scan(&transition); err != nil {
		t.Fatalf("recording %s -> %s: %v", from, to, err)
	}

	if _, err := tx.Exec(t.Context(),
		`SELECT set_config('shipper.job_status_transition', $1, true)`, transition.String()); err != nil {
		t.Fatalf("naming the transition to the guard: %v", err)
	}

	if _, err := tx.Exec(t.Context(),
		`UPDATE jobs SET status = $2 WHERE id = $1`, job, to); err != nil {
		t.Fatalf("moving the job to %s: %v", to, err)
	}

	if err := tx.Commit(t.Context()); err != nil {
		t.Fatalf("committing %s -> %s: %v", from, to, err)
	}
}

// acceptBidOn puts a provider on a job as the awarded one.
//
// `uq_bids_one_accepted_per_job` is partial on `status = 'Accepted'`, so exactly one of these can
// exist per job — which is the property [activeJobLookup]'s LEFT JOIN relies on to not multiply
// rows. `ck_bids_offer_has_timing` is why both instants are supplied.
func acceptBidOn(t *testing.T, pool *pgxpool.Pool, job, provider uuid.UUID, status string) {
	t.Helper()

	id, err := uuid.NewV7()
	if err != nil {
		t.Fatalf("generating an id: %v", err)
	}
	if _, err := pool.Exec(t.Context(), `
		INSERT INTO bids (id, job_id, provider_id, status, amount, pickup_at, deliver_by)
		VALUES ($1, $2, $3, $4, 185.00, now() + interval '2 days', now() + interval '3 days')`,
		id, job, provider, status); err != nil {
		t.Fatalf("placing a %s bid: %v", status, err)
	}
}

// TestTheActiveJobLookupSeesBothParties is the assertion the whole ticket rests on.
//
// Docs/05 §3.1: "Erasing a party mid-delivery would strand the counterparty." **Party**, not
// customer — and the deletion endpoint is `RequireUser` with no role predicate, so both halves of
// the marketplace reach it. A lookup that read `jobs.customer_id` alone would pass every
// customer-side assertion in this repository and delete a driver in the middle of a delivery.
//
// Four accounts and one job, so that the false answers are as load-bearing as the true ones: a
// predicate that answered true for everybody would satisfy the two positive cases on its own.
func TestTheActiveJobLookupSeesBothParties(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newBidFixtureUser(t, pool, "deferral-c@example.com", "+61400000710", "customer")
	awarded := newBidFixtureUser(t, pool, "deferral-p1@example.com", "+61400000711", "provider")
	losing := newBidFixtureUser(t, pool, "deferral-p2@example.com", "+61400000712", "provider")
	bystander := newBidFixtureUser(t, pool, "deferral-p3@example.com", "+61400000713", "provider")

	job := newBidFixtureJob(t, pool, customer)
	acceptBidOn(t, pool, job, awarded, "Accepted")
	acceptBidOn(t, pool, job, losing, "Rejected")

	moveJobTo(t, pool, job, customer, "Draft", "Open")
	moveJobTo(t, pool, job, customer, "Open", "Awarded")

	lookup := activeJobLookup{}

	for _, c := range []struct {
		name string
		user uuid.UUID
		want bool
	}{
		{"the customer whose goods are moving", customer, true},
		{"the provider whose offer was accepted", awarded, true},
		{"a provider whose offer was rejected", losing, false},
		{"a provider with no offer on it at all", bystander, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := lookup.HasActiveJob(t.Context(), pool, c.user)
			if err != nil {
				t.Fatalf("reading: %v", err)
			}
			if got != c.want {
				t.Errorf("HasActiveJob(%s) = %v, want %v.\n"+
					"  Docs/05 §3.1 defers deletion for a *party* to a delivery — the "+
					"customer and the provider holding the accepted bid are each the "+
					"other's counterparty, and a lookup that sees one of them strands "+
					"the other.", c.user, got, c.want)
			}
		})
	}
}

// TestTheActiveJobLookupCoversDocs02sWholeVocabulary walks one job through every status in
// Docs/02 §1 and asserts the answer at each, for **both** parties.
//
// # Why a walk rather than six fixtures
//
// The six committed statuses are the ones that must defer, and the other six are the ones that must
// not — and only asserting both makes the range a range. A test that checked `Awarded` alone would
// pass against a predicate reading `status <> 'Draft'`, which defers a person whose job was
// cancelled a year ago and never lets them leave.
//
// It walks Docs/02 §1's order rather than Docs/02 §2's permitted transitions, and the database is
// what makes that legitimate: 000402 requires a recorded history row for a status change and has no
// opinion about which changes are allowed — that table lives in the guarded function in
// `internal/jobs`. So this covers the *vocabulary* completely, which is what stops a thirteenth
// status being added and silently untested.
func TestTheActiveJobLookupCoversDocs02sWholeVocabulary(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newBidFixtureUser(t, pool, "deferral-walk-c@example.com", "+61400000720", "customer")
	provider := newBidFixtureUser(t, pool, "deferral-walk-p@example.com", "+61400000721", "provider")

	job := newBidFixtureJob(t, pool, customer)
	acceptBidOn(t, pool, job, provider, "Accepted")

	active := map[jobs.Status]bool{}
	for _, status := range activeStatusesFromTheLifecycle() {
		active[status] = true
	}
	if len(active) != 6 {
		t.Fatalf("Docs/05 §3.1's range resolves to %d statuses, want 6: %v", len(active), active)
	}

	lookup := activeJobLookup{}
	at := jobs.StatusDraft

	for _, status := range jobs.Statuses {
		if status != at {
			moveJobTo(t, pool, job, customer, at.String(), status.String())
			at = status
		}

		for _, party := range []struct {
			name string
			user uuid.UUID
		}{{"the customer", customer}, {"the awarded provider", provider}} {
			got, err := lookup.HasActiveJob(t.Context(), pool, party.user)
			if err != nil {
				t.Fatalf("%s at %s: %v", party.name, status, err)
			}
			if got != active[status] {
				t.Errorf("at %q, HasActiveJob for %s = %v, want %v.\n"+
					"  Docs/05 §3.1 defers a request made between Awarded and "+
					"Delivered, and nowhere else: a job that has not been awarded has "+
					"no counterparty to strand, and one that has completed, been "+
					"cancelled or gone to dispute is no longer in flight.",
					status, party.name, got, active[status])
			}
		}
	}
}

// TestTheActiveJobLookupAnswersFalseForAnAccountWithNoJobs records the answer rather than leaving
// it to be discovered.
//
// False rather than an error, and that matters: it is the answer every account that has never
// traded gets, which is most of them, and an error would make the ordinary case the failing one.
func TestTheActiveJobLookupAnswersFalseForAnAccountWithNoJobs(t *testing.T) {
	pool := pgtest.DB(t)

	active, err := activeJobLookup{}.HasActiveJob(t.Context(), pool, uuid.New())
	if err != nil {
		t.Fatalf("reading: %v", err)
	}
	if active {
		t.Error("an account that does not exist is carrying a delivery")
	}
}

// TestTheActiveStatusesAreTheOnesTheLifecycleDeclares pairs [activeJobStatuses] with Docs/02 §1.
//
// The SQL list is a hand-written copy of six values from a generated vocabulary
// (`contracts/statuses.yaml`, SHIP-56a), so `want` is built from `jobs.Statuses` — a different
// source from the subject, which is the property wave 12 recorded as the difference between a test
// and a tautology.
//
// It is a text guard and cannot prove the query uses it; the walk above is what proves that.
func TestTheActiveStatusesAreTheOnesTheLifecycleDeclares(t *testing.T) {
	inSQL := map[string]bool{}
	for _, match := range quotedBidStrings.FindAllStringSubmatch(activeJobStatuses, -1) {
		inSQL[match[1]] = true
	}

	want := map[string]bool{}
	for _, status := range activeStatusesFromTheLifecycle() {
		want[status.String()] = true
	}

	for status := range want {
		if !inSQL[status] {
			t.Errorf("%q is between Awarded and Delivered and activeJobStatuses does not name "+
				"it — a deletion during it would not be deferred", status)
		}
	}
	for status := range inSQL {
		if !want[status] {
			t.Errorf("activeJobStatuses names %q, which is outside Docs/05 §3.1's range — a "+
				"person whose job is in that status would be held indefinitely", status)
		}
	}
}

// activeStatusesFromTheLifecycle is Docs/05 §3.1's range, resolved against Docs/02 §1's own order.
//
// "A request made between Awarded and Delivered", inclusive at both ends, read off the ordered
// vocabulary rather than typed out again. A status inserted into Docs/02 §1 between those two ends
// up here, which is what makes the pairing above notice it.
func activeStatusesFromTheLifecycle() []jobs.Status {
	var (
		out   []jobs.Status
		open  bool
		known = jobs.Statuses
	)
	for _, status := range known {
		if status == jobs.StatusAwarded {
			open = true
		}
		if open {
			out = append(out, status)
		}
		if status == jobs.StatusDelivered {
			break
		}
	}
	return out
}
