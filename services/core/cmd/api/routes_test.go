package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/config"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/idempotency"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/identity"
)

// testSigningKey is a throwaway, thirty-two bytes so the keyset's own length rule is satisfied.
var testSigningKey = []byte("cmd-api-test-signing-key-0123456")

const testKID = "test"

func testIdentityConfig() config.Identity {
	return config.Identity{
		// The cheapest profile internal/identity will run. It has to be a real one: the
		// identity handler is built during attach, from every test in this package that
		// constructs a router, and a zero profile stops the process at startup rather than
		// producing a hasher nothing can verify against. 64 MiB per hash across parallel
		// packages would thrash a laptop (Docs/10 §5), and no test here hashes anything.
		Argon2: config.Argon2{MemoryKiB: 1024, Iterations: 1, Parallelism: 1},

		AccessTokenTTL:       15 * time.Minute,
		AccessTokenKeys:      map[string][]byte{testKID: testSigningKey},
		AccessTokenActiveKID: testKID,
	}
}

func testDeps() Deps {
	return Deps{
		Config:    &config.Config{Identity: testIdentityConfig()},
		Logger:    slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Clock:     clock.System{},
		StartedAt: time.Now(),
	}
}

// testAuthenticator builds the real thing rather than a stub.
//
// The point of exercising it here is that this is the only place the identity verifier, the
// authctx subject and the httpx middleware are wired together — a stub would test the middleware
// against itself and leave the translation in auth.go unexercised, which is exactly the part that
// converts between two Role types and could be silently wrong.
func testAuthenticator() httpx.Authenticator {
	authenticate, err := newAccessTokenAuthenticator(testIdentityConfig(), clock.System{})
	if err != nil {
		panic("cmd/api test: building the authenticator: " + err.Error())
	}
	return authenticate
}

// testAccessToken mints a token the test router will accept.
func testAccessToken(t *testing.T, role identity.Role) (raw string, userID, sessionID uuid.UUID) {
	t.Helper()

	keys, err := identity.NewKeyset(map[string][]byte{testKID: testSigningKey}, testKID)
	if err != nil {
		t.Fatalf("building the keyset: %v", err)
	}
	issuer, err := identity.NewAccessTokenIssuer(keys, 15*time.Minute, clock.System{})
	if err != nil {
		t.Fatalf("building the issuer: %v", err)
	}

	userID, sessionID = uuid.New(), uuid.New()
	token, err := issuer.Issue(userID, sessionID, role)
	if err != nil {
		t.Fatalf("issuing: %v", err)
	}
	return token.Value, userID, sessionID
}

func testRouter() http.Handler {
	return newRouter(testDeps(), idempotency.NewMemoryStore(), testAuthenticator())
}

// SHIP-6's acceptance criterion: GET /health returns 200 with version and commit.
func TestHealthReturnsBuildInfo(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}

	var body healthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}

	if body.Status != "ok" {
		t.Errorf("status = %q, want ok", body.Status)
	}
	// Never empty: an unstamped build reports "dev" and "unknown" rather than nothing,
	// so a client comparing versions always has something to compare.
	if body.Version == "" {
		t.Error("version was empty")
	}
	if body.Commit == "" {
		t.Error("commit was empty")
	}
	if body.Version != buildinfo.Get().Version {
		t.Errorf("version = %q, want %q from buildinfo", body.Version, buildinfo.Get().Version)
	}
	if body.Commit != buildinfo.Get().Commit {
		t.Errorf("commit = %q, want %q from buildinfo", body.Commit, buildinfo.Get().Commit)
	}
}

// SHIP-19 has the Flutter app read `version` from this payload as its connectivity
// proof, so the JSON field names are a contract with builds that will be on devices.
func TestHealthFieldNamesAreStable(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v", err)
	}

	for _, field := range []string{"status", "version", "commit", "built_at", "dirty", "uptime"} {
		if _, ok := body[field]; !ok {
			t.Errorf("response has no %q field; got %v", field, body)
		}
	}
}

// Health is consumed by load balancers and monitoring rather than by API clients, so it
// stays outside the /v1 group SHIP-13 introduces — versioning it would break the health
// check on the day v2 ships.
func TestHealthIsNotVersioned(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("GET /v1/health returned %d, want 404", rec.Code)
	}
}

func TestHealthRejectsNonGetMethods(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testRouter().ServeHTTP(rec, httptest.NewRequest(method, "/health", nil))

			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s /health returned %d, want 405", method, rec.Code)
			}
		})
	}
}

// The router must apply the middleware, not merely define it.
func TestRouterAttachesRequestID(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if got := rec.Header().Get(httpx.HeaderRequestID); got == "" {
		t.Errorf("no %s header on the response", httpx.HeaderRequestID)
	}
}

func TestUnknownRouteIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/jobs", nil))

	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", rec.Code)
	}
}

// SHIP-13's acceptance criterion: the public API is served under /v1, and the version is
// discoverable from the group itself.
func TestVersionGroupRootReportsItsVersion(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}

	var body apiRootResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.APIVersion != apiVersion {
		t.Errorf("api_version = %q, want %q", body.APIVersion, apiVersion)
	}
}

// /v1 without the trailing slash has to reach the same place, because clients write it
// both ways and a 404 on one of them is a support ticket.
func TestVersionGroupRedirectsFromThePrefixWithoutASlash(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1", nil))

	if rec.Code != http.StatusMovedPermanently {
		t.Fatalf("status = %d, want 301", rec.Code)
	}
	if got := rec.Header().Get("Location"); got != "/v1/" {
		t.Errorf("Location = %q, want /v1/", got)
	}
}

// Routes registered inside the group exist only inside it. If they answered at the server
// root as well, "all routes are served under /v1" would be true only by convention.
func TestVersionedRoutesAreNotServedAtTheRoot(t *testing.T) {
	for _, path := range []string{"/", "/jobs"} {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))

			if rec.Code != http.StatusNotFound {
				t.Errorf("GET %s returned %d, want 404", path, rec.Code)
			}
		})
	}
}

// SHIP-12 through the real router: a 404 from ServeMux itself still arrives as the error
// contract, because that is the response a client meets first while it is being written.
func TestRouterErrorsUseTheStandardContract(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/jobs", nil)
	req.Header.Set(httpx.HeaderRequestID, "known-request-id")
	testRouter().ServeHTTP(rec, req)

	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q, want JSON", ct)
	}

	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}

	if body.Error.Code != string(httpx.CodeNotFound) {
		t.Errorf("code = %q, want %q", body.Error.Code, httpx.CodeNotFound)
	}
	if body.Error.Message == "" {
		t.Error("message was empty")
	}
	if body.Error.RequestID != "known-request-id" {
		t.Errorf("request_id = %q, want it carried in the body", body.Error.RequestID)
	}
}

// SHIP-15 is applied to the whole /v1 group rather than per route, so an endpoint cannot
// be added to the versioned API without it. Proving that needs the real router.
func TestStateChangingRequestsUnderV1RequireAnIdempotencyKey(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/jobs", nil))

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}

	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("response is not JSON: %v (%s)", err, rec.Body.String())
	}
	if body.Error.Code != string(httpx.CodeIdempotencyKeyRequired) {
		t.Errorf("code = %q, want %q", body.Error.Code, httpx.CodeIdempotencyKeyRequired)
	}
}

// Operational endpoints sit outside the group, and so outside the middleware. A load
// balancer does not send an idempotency key.
func TestHealthNeedsNoIdempotencyKey(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200", rec.Code)
	}
}
