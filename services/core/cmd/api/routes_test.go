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

// testDriverSigningKey is the driver token's throwaway, and it is deliberately **not**
// testSigningKey (SHIP-107).
//
// Docs/10 §5 requires the two token systems to have separate signing key material, config.Load
// refuses a configuration in which they share a secret, and a test fixture that shared one would be
// the only place in the repository where they did.
var testDriverSigningKey = []byte("cmd-api-test-driver-token-key-012")

const testDriverKID = "test-driver"

// testDeliveryConfig is the driver token keyset the delivery handler is built from.
//
// It has to be a real one for the reason [testIdentityConfig]'s argon2 profile does: the delivery
// handler is built during attach, from every test in this package that constructs a router, and a
// keyset that cannot sign stops the process at startup rather than producing a service that quietly
// hands out no link.
func testDeliveryConfig() config.Delivery {
	return config.Delivery{
		DriverTokenTTL:       7 * 24 * time.Hour,
		DriverTokenKeys:      map[string][]byte{testDriverKID: testDriverSigningKey},
		DriverTokenActiveKID: testDriverKID,
	}
}

// testStorageConfig is the object store the proof-upload signer is built from (SHIP-114).
//
// It is here for the third time in this file's history and for the third identical reason, after
// the argon2 profile and the driver keyset above: the delivery handler is built during attach, from
// every test in this package that constructs a router, and a signer that cannot sign stops the
// process at startup. `config.Storage{}` has an empty endpoint, and an empty endpoint is not a URL
// — internal/config refuses one at load for exactly the same reason.
//
// **Nothing here reaches the store.** Signing is an HMAC over a few hundred bytes and touches no
// I/O, so these values need to be well-formed rather than real: no test in this package uploads
// anything, and the URLs the signer produces are exercised against a live bucket in
// internal/platform/storage instead.
func testStorageConfig() config.Storage {
	return config.Storage{
		Endpoint:             "http://localhost:9000",
		Bucket:               "cmd-api-test",
		Region:               "ap-southeast-2",
		AccessKeyID:          "cmd-api-test-key",
		SecretAccessKey:      "cmd-api-test-secret",
		UsePathStyle:         true,
		PresignTTL:           15 * time.Minute,
		DownloadTTL:          5 * time.Minute,
		MaxUploadBytes:       5 << 20,
		AcceptedContentTypes: []string{"image/jpeg", "image/heic"},
	}
}

func testDeps() Deps {
	return Deps{
		Config: &config.Config{
			Identity: testIdentityConfig(),
			Delivery: testDeliveryConfig(),
			Storage:  testStorageConfig(),
			App:      testAppConfig(),
		},
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

// testDriverGuard builds the real driver-token middleware, for the reason [testAuthenticator] gives
// about the real authenticator (SHIP-108).
//
// **It is not optional any more, and that is the change SHIP-108 made to every caller of newRouter
// in this package.** While nothing declared RequireDriverToken, passing nil was correct: the class
// stayed out of the guard map and no route wanted it. `GET /v1/driver/jobs/{id}` declares it, so a
// router built with nil now panics at startup, naming the class — which is the seam behaving exactly
// as SHIP-15m designed it, met from the other side.
//
// A stub would satisfy the panic and prove nothing. The real guard is what puts internal/delivery's
// verifier, the configured keyset and the manifest's auth class in one process, which is the only
// place they meet.
func testDriverGuard() Guard {
	guard, err := newDriverTokenGuard(testDeps().Config, clock.System{})
	if err != nil {
		panic("cmd/api test: building the driver token guard: " + err.Error())
	}
	return guard
}

// testAdminGuard is the administrator half of the same seam (SHIP-15r), and it exists ahead of the
// guard for a reason that is about this package's *tests* rather than about the guard.
//
// SHIP-108's measured cost is on record in Docs/11 §9: filling the driver seam changed 15 `nil`
// call sites across five test files, because newRouter takes the guard as an argument and every
// test that builds a router passes one. That churn is mechanical, it is unreviewable in a diff that
// also contains a token verifier, and it belongs to nobody — so SHIP-15r absorbs it here instead of
// leaving it in SHIP-147's diff. Every caller in this package already says `testAdminGuard()`, and
// SHIP-147 changes the body of this function and of newAdminGuard, and nothing else.
//
// It returns nil today, which is correct rather than a placeholder: no route declares RequireAdmin,
// so the class stays out of the guard map and a router built from this is exactly the router
// main.go builds. When a route does declare it, a nil guard makes the router panic at startup
// naming the class — which is how the driver half announced itself, and is the failure this helper
// converts from fifteen edits into one.
func testAdminGuard() Guard {
	guard, err := newAdminGuard(testDeps().Config, nil, clock.System{})
	if err != nil {
		panic("cmd/api test: building the admin guard: " + err.Error())
	}
	return guard
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
	return newRouter(testDeps(), idempotency.NewMemoryStore(), testAuthenticator(), testDriverGuard(), testAdminGuard())
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

// unroutedPath is a path inside /v1 that no route serves and none is planned to.
//
// It was `/v1/jobs` until SHIP-61 made that a real endpoint, at which point both tests below
// started asserting the 405 the router correctly produces for the wrong method. A stand-in for
// "unknown" has to be a path nobody will implement, and naming it once means the next endpoint to
// collide with it changes one line rather than finding two failures that look unrelated.
const unroutedPath = "/v1/no-such-endpoint"

func TestUnknownRouteIsNotFound(t *testing.T) {
	rec := httptest.NewRecorder()
	testRouter().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, unroutedPath, nil))

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
	req := httptest.NewRequest(http.MethodGet, unroutedPath, nil)
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
