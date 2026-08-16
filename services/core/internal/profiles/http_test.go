package profiles

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// The HTTP surface, against a real database and the real handler.
//
// The mux here mirrors cmd/api/routes_profiles.go rather than the whole service: what is being tested
// is the handler and the shape it writes, and the route table itself is held to the contract by
// `cmd/api`'s own tests. The patterns must match that file exactly — a route mounted here and nowhere
// else is an endpoint that exists only in the tests.

func newTestRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newTestService(), pool,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/provider/verification", handler.Verification())
	return mux
}

// as sends a request on behalf of an authenticated caller.
//
// **The role on the subject is deliberately customer**, whatever the account is. It is a claim in a
// token, and the platform decides from `users.role` instead — so a test that carried the "right" role
// here would be proving something about its own fixture.
func as(t *testing.T, h http.Handler, caller uuid.UUID, method, target string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, nil)
	req = req.WithContext(authctx.WithSubject(req.Context(), authctx.Subject{
		UserID:    caller.String(),
		Role:      authctx.RoleCustomer,
		SessionID: uuid.Must(uuid.NewV7()).String(),
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decoding %s: %v", rec.Body, err)
	}
	return out
}

// TestAProviderReadsTheirOwnVerification, in the shape the contract publishes.
func TestAProviderReadsTheirOwnVerification(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	provider := newProvider(t, pool, "http-pv-provider@example.com", "+61400200101")

	rec := as(t, router, provider, http.MethodGet, "/v1/provider/verification")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[map[string]any](t, rec)
	if body["state"] != "Pending" {
		t.Errorf("state = %v, want Pending", body["state"])
	}
	if _, present := body["submitted_at"]; !present {
		t.Error("the response has no submitted_at, so a Pending screen cannot say how long")
	}

	// **Absent rather than empty**, which is what `omitempty` buys and what the contract says. A
	// client rendering `reason` unconditionally would otherwise show an empty banner to every
	// provider on their first day.
	for _, field := range []string{"reason", "decided_at"} {
		if _, present := body[field]; present {
			t.Errorf("an undecided record carries %s = %v", field, body[field])
		}
	}

	// And no eligibility answer, in any spelling. The platform answers "may I bid" in exactly one
	// place — `GET /v1/fleet/jobs` — and a boolean here would be a second answer computed from one
	// of four filters. This is a word-level check rather than a key check because the field that
	// would do the damage is the one nobody names `can_bid`.
	for _, disclosure := range []string{"can_bid", "eligible", "may_bid", "bidding"} {
		if _, present := body[disclosure]; present {
			t.Errorf("the verification response carries %q; eligibility has one answer and it is "+
				"the feed's", disclosure)
		}
	}
}

// TestADecidedProviderReadsTheReasonAndTheClock.
func TestADecidedProviderReadsTheReasonAndTheClock(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	provider := newProvider(t, pool, "http-pv-decided@example.com", "+61400200102")
	admin := newAdmin(t, pool, "http-pv-admin@example.com")

	const why = "The insurance certificate had expired. Upload a current one and we will look again."
	if _, err := decide(t, pool, provider, StateRejected, Actor{Type: ActorAdmin, ID: admin}, why); err != nil {
		t.Fatalf("rejecting: %v", err)
	}

	rec := as(t, router, provider, http.MethodGet, "/v1/provider/verification")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[map[string]any](t, rec)
	if body["state"] != "Rejected" {
		t.Errorf("state = %v, want Rejected", body["state"])
	}
	if body["reason"] != why {
		t.Errorf("reason = %v, want the decision's own reason", body["reason"])
	}
	if _, present := body["decided_at"]; !present {
		t.Error("a decided record has no decided_at")
	}

	// The administrator is not in it, in any form. Docs/04 §4 requires a *reason* be communicable
	// and says nothing about a name; the identity of whoever rejected somebody is what turns a
	// moderation decision into a personal one.
	for _, disclosure := range []string{admin.String(), "Verify Admin", "http-pv-admin@example.com"} {
		if strings.Contains(rec.Body.String(), disclosure) {
			t.Errorf("the response names the deciding administrator (%q): %s", disclosure, rec.Body)
		}
	}
}

// TestACustomerIsRefusedWithACodeTheAppCanActOn.
func TestACustomerIsRefusedWithACodeTheAppCanActOn(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newAccount(t, pool, "http-pv-customer@example.com", "+61400200103", "customer")

	rec := as(t, router, customer, http.MethodGet, "/v1/provider/verification")
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body)
	}
	if code := decode[errorEnvelope](t, rec).Error.Code; code != string(CodeProviderOnly) {
		t.Errorf("code = %q, want %s", code, CodeProviderOnly)
	}
}

// TestTheDatabaseBeingDownIsA503RatherThanA500.
//
// The pool may be nil — the service starts with an unreachable database on purpose — and the two
// answers say different things to a mobile client: retry, or surface a failure to the person holding
// the phone.
func TestTheDatabaseBeingDownIsA503RatherThanA500(t *testing.T) {
	router := newTestRouter(t, nil)

	rec := as(t, router, uuid.Must(uuid.NewV7()), http.MethodGet, "/v1/provider/verification")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
}
