package identity

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// SHIP-183b's Credential half, which is the part cmd/api's middleware cannot hold.
//
// Docs/12 §5 puts five routes in the Credential class. Two of them — sign-in here and in
// internal/admin — carried a bucket from SHIP-47. The other three carried nothing at all: refresh
// and email verification had no limit of any kind, and phone verification had only the attempt
// counter that retires a code. SHIP-183b is where all five enforce.
//
// The two properties below are what "enforce their class" has to mean, and they are separable —
// an implementation can easily have one and not the other:
//
//   - **Every route that tests a secret is bounded.** Not just sign-in.
//   - **They share one bucket.** Docs/12 §9 settled this for the middleware classes and the same
//     argument applies here: four buckets of thirty is a hundred and twenty guesses from an
//     address the document meant to allow thirty, and an attacker who found one endpoint tight
//     would simply spread the campaign across the other three.

// testHandler builds the HTTP layer over a real service, a real database and a real Redis.
//
// The limiting under test lives in the handler rather than in the service — the class charges
// failures only, and the outcome is what the handler is holding — so a service-level test would
// exercise everything except the thing SHIP-183b built.
func testHandler(t *testing.T) *Handler {
	t.Helper()

	svc, _, _ := newTestService(t)
	h, err := NewHandler(svc, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}
	return h
}

// from posts body to a route as though it arrived from remote.
//
// RemoteAddr rather than a forwarded header, because nothing here configures a trusted proxy and
// httpx.ClientAddr therefore reads the peer — which is the point: internal/httpx's tests hold what
// happens when a proxy *is* configured, and this holds that the domain asks httpx at all.
//
// **The path is a parameter and every caller passes the route's real one**, which is not
// decoration. These handlers are driven directly rather than through cmd/api's mux, so nothing
// makes the request carry the path it would in production — and a first version of this file sent
// every one of them to "/". Under that version an implementation keying the bucket on the route
// instead of on the class was indistinguishable from a correct one: the mutation was applied, the
// tests passed, and the guard below was proving nothing.
func from(t *testing.T, handler http.Handler, path, remote, body string) *httptest.ResponseRecorder {
	t.Helper()

	r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	r.RemoteAddr = remote

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, r)
	return rec
}

// The paths cmd/api serves these handlers on, so that a bucket keyed on the route rather than on
// the class is visible here.
const (
	refreshPath     = "/v1/auth/refresh"
	verifyEmailPath = "/v1/auth/verify-email"
	verifyPhonePath = "/v1/auth/verify-phone"
)

// A refresh token that is well-formed enough to be looked up and will never match a row.
const unknownRefreshToken = `{"refresh_token":"9qE2vT7bYw1sJk4pNc0aRlX8oZgHdM3uQiV6yB5tCfE"}`

// spend makes n failed attempts from one address and fails the test if any is throttled.
func spend(t *testing.T, handler http.Handler, remote string, n int) {
	t.Helper()

	for i := range n {
		if rec := from(t, handler, refreshPath, remote, unknownRefreshToken); rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d of %d was refused; the bucket holds %d",
				i+1, n, credentialAddressCapacity)
		}
	}
}

// TestRefreshIsBoundedByTheAddressBucket is the first property on the route that had no limit at
// all.
//
// Docs/12 §1 recorded refresh as carrying no limit of any kind. It presents a bearer credential
// and answers differently depending on whether it is valid, which is the definition of a route
// somebody can guess at.
func TestRefreshIsBoundedByTheAddressBucket(t *testing.T) {
	handler := testHandler(t).Refresh()
	const attacker = "203.0.113.7:41000"

	spend(t, handler, attacker, credentialAddressCapacity)

	rec := from(t, handler, refreshPath, attacker, unknownRefreshToken)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt %d answered %d, want 429 — the bucket holds %d",
			credentialAddressCapacity+1, rec.Code, credentialAddressCapacity)
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("the refusal carries no Retry-After, so a throttled client polls")
	}
}

// TestTheCredentialClassSharesOneBucket is the second property, and the one a per-route
// implementation would fail.
//
// It spends the whole allowance on refresh and then asks a *different* Credential route. If each
// route had its own bucket the second would be admitted, and one address would hold four times the
// budget Docs/12 §3 argues for.
func TestTheCredentialClassSharesOneBucket(t *testing.T) {
	h := testHandler(t)
	const attacker = "203.0.113.7:41000"

	spend(t, h.Refresh(), attacker, credentialAddressCapacity)

	for _, route := range []struct {
		name    string
		path    string
		handler http.Handler
		body    string
	}{
		{"verify-email", verifyEmailPath, h.VerifyEmail(), `{"token":"MFhQd3ZLbjJyOXRHc0picA"}`},
		{"verify-phone", verifyPhonePath, h.VerifyPhone(), `{"phone":"+61412000111","code":"123456"}`},
	} {
		got := from(t, route.handler, route.path, attacker, route.body)
		if got.Code != http.StatusTooManyRequests {
			t.Errorf("%s answered %d after the class's allowance was spent on refresh, want "+
				"429. Each route holding its own bucket would give one address %d guesses "+
				"where Docs/12 §3 allows %d",
				route.name, got.Code, credentialAddressCapacity*4, credentialAddressCapacity)
		}
	}
}

// TestAnotherAddressIsNotThrottledByTheFirst is the availability half.
//
// A limiter that refused everybody would pass both tests above. This is what says the key is the
// caller rather than the route.
func TestAnotherAddressIsNotThrottledByTheFirst(t *testing.T) {
	handler := testHandler(t).Refresh()

	spend(t, handler, "203.0.113.7:41000", credentialAddressCapacity)

	if rec := from(t, handler, refreshPath, "198.51.100.9:41000", unknownRefreshToken); rec.Code == http.StatusTooManyRequests {
		t.Fatal("a second address was refused on its first attempt, so one caller emptying " +
			"their bucket refuses everybody")
	}
}

// TestASuccessfulRefreshIsNotCharged holds the "failures only" half of the class.
//
// Docs/12 §3 charges Credential on failures alone, and that is what makes it a control on guessing
// rather than a cap on how often a legitimate client may refresh. A mobile client refreshes on a
// schedule; charging every attempt would throttle a busy NAT for doing nothing wrong.
func TestASuccessfulRefreshIsNotCharged(t *testing.T) {
	svc, pool, user := newSessionService(t, clock.System{})

	h, err := NewHandler(svc, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	pair, err := svc.startSession(t.Context(), pool, user, "Nethmin's iPhone")
	if err != nil {
		t.Fatalf("starting the session: %v", err)
	}

	handler := h.Refresh()
	const client = "203.0.113.7:41000"

	// Far past the allowance, and every one of them a success. Rotation means each answer
	// carries the token the next attempt has to present.
	token := pair.Refresh.Value
	for i := range credentialAddressCapacity + 5 {
		rec := from(t, handler, refreshPath, client, `{"refresh_token":"`+token+`"}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("refresh %d of %d answered %d, want 200 — a success is not charged, so "+
				"a legitimate client refreshing on a schedule can never run out",
				i+1, credentialAddressCapacity+5, rec.Code)
		}

		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
			t.Fatalf("decoding refresh %d: %v", i+1, err)
		}
		token = body.RefreshToken
	}
}
