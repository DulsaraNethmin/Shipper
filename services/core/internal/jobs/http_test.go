package jobs

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

// The wire contract of SHIP-61 and SHIP-62.
//
// These drive the handlers on a mux of their own rather than through cmd/api's router, because what
// is being checked here is the domain's own half: what it accepts, what it refuses, and what shape
// it puts on the wire. The middleware around them — authentication, idempotency, the error envelope
// — belongs to cmd/api and is tested there.

// newTestRouter mounts both handlers on the patterns cmd/api registers them under.
//
// A real ServeMux rather than calling the handlers directly, because the path parameter is part of
// what is being tested: Update reads {id} through r.PathValue, and a handler invoked without a
// pattern would see an empty one and pass a test that the served route would fail.
func newTestRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newDraftService(t, &fakeGeocoder{}), pool,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /v1/jobs", handler.Create())
	return mux
}

// as sends a request on behalf of an authenticated caller.
//
// The subject is put on the context by hand, which is what httpx.ResolveSubject does one layer
// out. Both routes declare RequireUser, so a handler reached without one would be a wiring defect
// rather than a request anybody could send.
func as(t *testing.T, h http.Handler, caller uuid.UUID, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authctx.WithSubject(req.Context(), authctx.Subject{
		UserID:    caller.String(),
		Role:      authctx.RoleCustomer,
		SessionID: uuid.Must(uuid.NewV7()).String(),
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decode reads a response body, failing the test rather than the caller's next line.
func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()

	var out T
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, rec.Body)
	}
	return out
}

type errorEnvelope struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Details []struct {
			Field string `json:"field"`
			Code  string `json:"code"`
		} `json:"details"`
	} `json:"error"`
}

// TestCreateEndpointAnswersWithTheJobItCreated is SHIP-61's acceptance criterion at the wire.
func TestCreateEndpointAnswersWithTheJobItCreated(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-create@example.com", "+61400000620")

	rec := as(t, router, customer, http.MethodPost, "/v1/jobs", `{
		"pickup":  {"line": "12 Smith Street", "suburb": "Newtown", "state": "nsw", "postcode": "2042"},
		"dropoff": {"line": "40 Bourke Street", "suburb": "Melbourne", "state": "Victoria", "postcode": "3000"},
		"goods_description": "Two-seater sofa",
		"weight_kg": 45.5,
		"pickup_window": {"start": "2026-08-14T09:00:00+10:00", "end": "2026-08-15T17:00:00+10:00"}
	}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	body := decode[jobResponse](t, rec)

	switch {
	case body.ID == "":
		t.Error("the response carries no id")
	// Lower snake case on the wire, per Docs/10 §4.7, even though the stored form is "Draft".
	case body.Status != "draft":
		t.Errorf("status = %q, want draft", body.Status)
	case body.Pickup == nil || body.Pickup.State != "NSW":
		t.Errorf("pickup = %#v, want the state normalised to NSW", body.Pickup)
	// "Victoria" on input, "VIC" on output: any form a person types is accepted and the
	// canonical abbreviation comes back.
	case body.Dropoff == nil || body.Dropoff.State != "VIC":
		t.Errorf("dropoff = %#v, want the state normalised to VIC", body.Dropoff)
	case body.Pickup.Coordinate == nil:
		t.Error("the pickup came back with no coordinate")
	case body.PickupWindow == nil || body.PickupWindow.Start == "":
		t.Errorf("pickup window = %#v", body.PickupWindow)
	}

	// Everything the customer did not supply is omitted rather than sent empty, which is what
	// lets a client tell "not filled in" from "filled in with nothing".
	raw := rec.Body.String()
	for _, absent := range []string{"length_cm", "handling_notes", "vehicle_requirement", "dropoff_window"} {
		if strings.Contains(raw, absent) {
			t.Errorf("the response carries %s, which was never supplied: %s", absent, raw)
		}
	}
}

// Status is never a settable field, and the wire types are where that is enforced against a client
// rather than against a Go caller.
func TestCreateRefusesAStatusField(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-status@example.com", "+61400000621")

	rec := as(t, router, customer, http.MethodPost, "/v1/jobs", `{"status": "open"}`)

	// 400 rather than a quietly ignored field. A client that believes it published a job and
	// did not will retry forever.
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if body := decode[errorEnvelope](t, rec); body.Error.Code != "bad_request" {
		t.Errorf("code = %q, want bad_request", body.Error.Code)
	}
	if !strings.Contains(rec.Body.String(), "status") {
		t.Errorf("the message does not name the offending field: %s", rec.Body)
	}
}

// A provider is refused with a code the app can act on, rather than a bare 403.
func TestAProviderCannotCreateAJob(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	provider := newProvider(t, pool, "http-provider@example.com", "+61400000624")

	rec := as(t, router, provider, http.MethodPost, "/v1/jobs", `{}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body)
	}
	if body := decode[errorEnvelope](t, rec); body.Error.Code != string(CodeCustomerOnly) {
		t.Errorf("code = %q, want %s", body.Error.Code, CodeCustomerOnly)
	}
}

// A malformed field is reported by name, in the error contract's details, so the app can put the
// message beside the input that produced it.
func TestValidationFailuresNameTheField(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-invalid@example.com", "+61400000626")

	rec := as(t, router, customer, http.MethodPost, "/v1/jobs", `{
		"pickup": {"line": "12 Smith Street", "suburb": "Newtown", "state": "Westeros", "postcode": "20"},
		"pickup_window": {"start": "next tuesday"}
	}`)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body)
	}

	body := decode[errorEnvelope](t, rec)
	if body.Error.Code != "validation_failed" {
		t.Fatalf("code = %q, want validation_failed", body.Error.Code)
	}

	named := map[string]bool{}
	for _, d := range body.Error.Details {
		named[d.Field] = true
	}
	for _, field := range []string{"pickup.state", "pickup.postcode", "pickup_window.start"} {
		if !named[field] {
			t.Errorf("no detail names %s: %s", field, rec.Body)
		}
	}
}

// The pool may be nil — the service starts with an unreachable database on purpose — and the
// answer to that is 503, which tells a mobile client to retry.
func TestJobEndpointsAnswer503WithNoDatabase(t *testing.T) {
	router := newTestRouter(t, nil)

	rec := as(t, router, uuid.Must(uuid.NewV7()), http.MethodPost, "/v1/jobs", `{}`)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if body := decode[errorEnvelope](t, rec); body.Error.Code != "service_unavailable" {
		t.Errorf("code = %q, want service_unavailable", body.Error.Code)
	}
}
