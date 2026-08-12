package fleet

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

// The wire contract of SHIP-78.
//
// These drive the handlers on a mux of their own rather than through cmd/api's router, because what
// is being checked here is the domain's own half: what it accepts, what it refuses, and what shape
// it puts on the wire. The middleware around them — authentication, idempotency, the error envelope
// — belongs to cmd/api and is tested there.

// newTestRouter mounts every handler on the pattern cmd/api registers it under.
//
// A real ServeMux rather than calling the handlers directly, because the path parameter is part of
// what is being tested: four of the six read {id} through r.PathValue, and a handler invoked without
// a pattern would see an empty one and pass a test that the served route would fail.
//
// A handler added here and not to cmd/api/routes_fleet.go is an endpoint that exists only in the
// tests, so the two lists are worth reading against each other — routes_golden.txt is what makes the
// other direction visible.
func newTestRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newTestService(), pool,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/fleet/vehicles", handler.List())
	mux.Handle("POST /v1/fleet/vehicles", handler.Add())
	mux.Handle("GET /v1/fleet/vehicles/{id}", handler.Detail())
	mux.Handle("PATCH /v1/fleet/vehicles/{id}", handler.Update())
	mux.Handle("POST /v1/fleet/vehicles/{id}/deactivate", handler.Deactivate())
	mux.Handle("POST /v1/fleet/vehicles/{id}/reactivate", handler.Reactivate())
	return mux
}

// as sends a request on behalf of an authenticated caller.
//
// The subject is put on the context by hand, which is what httpx.ResolveSubject does one layer out.
// Every route declares RequireUser, so a handler reached without one would be a wiring defect rather
// than a request anybody could send.
//
// **The role on the subject is deliberately customer**, whatever the account is. It is a claim in a
// token, and the platform decides from users.role instead — so a test that carried the "right" role
// here would be proving something about its own fixture.
func as(t *testing.T, h http.Handler, caller uuid.UUID, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
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

// TestAddEndpointAnswersWithTheVehicleItCreated is SHIP-78's first verb at the wire.
func TestAddEndpointAnswersWithTheVehicleItCreated(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	provider := newProvider(t, pool, "http-add@example.com", "+61400000330")

	rec := as(t, router, provider, http.MethodPost, "/v1/fleet/vehicles", `{
		"registration": "abc 123",
		"vehicle_type": "Van",
		"make": "Mercedes-Benz",
		"model": "Sprinter 314",
		"max_weight_kg": 1200,
		"load_length_cm": 320
	}`)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	body := decode[vehicleResponse](t, rec)
	switch {
	case body.ID == "":
		t.Error("the response carries no id")
	case body.Registration != "ABC123":
		t.Errorf("registration = %q, want it normalised to ABC123", body.Registration)
	case body.VehicleType != "van":
		t.Errorf("vehicle_type = %q, want van", body.VehicleType)
	case !body.Active:
		t.Error("a new vehicle is not in service")
	}

	// The optional fields nobody supplied are omitted rather than sent as "" and 0, which is
	// what tells a client which of them the provider has actually filled in.
	raw := rec.Body.String()
	for _, absent := range []string{"load_width_cm", "load_height_cm", "deactivated_at"} {
		if strings.Contains(raw, absent) {
			t.Errorf("%s is in the response and was never supplied: %s", absent, raw)
		}
	}
	// `active` is the exception and is always present: a boolean absent when false is one every
	// client has to remember to default.
	if !strings.Contains(raw, `"active"`) {
		t.Errorf("active is missing from the response: %s", raw)
	}
}

// TestActiveIsNotASettableField is the fleet analogue of "status is never settable".
//
// httpx.DecodeJSON refuses unknown fields, so a client that sends it is told the field does not
// exist rather than having it quietly ignored — which is the failure worth preventing, because a
// provider who believes they took a truck off the road and did not will keep receiving work for it.
func TestActiveIsNotASettableField(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	provider := newProvider(t, pool, "http-active@example.com", "+61400000331")
	vehicle := added(t, pool, provider, "ACT111")

	for _, target := range []struct {
		method, path, body string
	}{
		{http.MethodPost, "/v1/fleet/vehicles", `{"registration":"ACT222","vehicle_type":"van","active":false}`},
		{http.MethodPatch, "/v1/fleet/vehicles/" + vehicle.ID.String(), `{"active": false}`},
		{http.MethodPatch, "/v1/fleet/vehicles/" + vehicle.ID.String(), `{"deactivated_at": "2026-08-11T03:30:00Z"}`},
	} {
		rec := as(t, router, provider, target.method, target.path, target.body)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s %s accepted %s: %d", target.method, target.path, target.body, rec.Code)
		}
	}
}

// TestACustomerIsRefusedWithACodeTheAppCanActOn.
//
// The subject in `as` claims role customer for every caller, so this also proves the platform reads
// users.role rather than the token's claim: the provider tests above pass with the same claim.
func TestACustomerIsRefusedWithACodeTheAppCanActOn(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newAccount(t, pool, "http-customer@example.com", "+61400000332", "customer")

	rec := as(t, router, customer, http.MethodPost, "/v1/fleet/vehicles",
		`{"registration": "CUS111", "vehicle_type": "ute"}`)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != string(CodeProviderOnly) {
		t.Errorf("code = %q, want %q", got, CodeProviderOnly)
	}
}

// TestADuplicatePlateIs409WithItsOwnCode.
//
// 409 rather than 422 because the value is well formed and it is the state of the fleet that
// contradicts the request — so the client shows the provider the vehicle they already have rather
// than marking the field invalid.
func TestADuplicatePlateIs409WithItsOwnCode(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	provider := newProvider(t, pool, "http-dup@example.com", "+61400000333")
	added(t, pool, provider, "DUP999")

	rec := as(t, router, provider, http.MethodPost, "/v1/fleet/vehicles",
		`{"registration": "dup 999", "vehicle_type": "ute"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != string(CodeDuplicateRegistration) {
		t.Errorf("code = %q, want %q", got, CodeDuplicateRegistration)
	}
}

// TestTheLifecycleOverHTTP walks add, edit, deactivate, reactivate and read on one vehicle.
//
// One test rather than five, because what is being checked is that the five answer with **one
// shape**: a client parses `vehicleResponse` whatever it did to obtain the vehicle, and a "detail"
// shape carrying a field or two more would make every write response a subset to special-case.
func TestTheLifecycleOverHTTP(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	provider := newProvider(t, pool, "http-life@example.com", "+61400000334")

	created := decode[vehicleResponse](t, as(t, router, provider, http.MethodPost, "/v1/fleet/vehicles",
		`{"registration": "LIF111", "vehicle_type": "van", "make": "Isuzu", "max_weight_kg": 4500}`))
	path := "/v1/fleet/vehicles/" + created.ID

	t.Run("edit", func(t *testing.T) {
		rec := as(t, router, provider, http.MethodPatch, path,
			`{"vehicle_type": "box_truck", "max_weight_kg": 0}`)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
		}

		body := decode[vehicleResponse](t, rec)
		if body.VehicleType != "box_truck" {
			t.Errorf("vehicle_type = %q, want box_truck", body.VehicleType)
		}
		if body.Make != "Isuzu" {
			t.Errorf("make = %q, want a field that was not mentioned left alone", body.Make)
		}
		if strings.Contains(rec.Body.String(), "max_weight_kg") {
			t.Errorf("the cleared capacity is still in the response: %s", rec.Body)
		}
	})

	t.Run("an edit naming no field is refused", func(t *testing.T) {
		if rec := as(t, router, provider, http.MethodPatch, path, `{}`); rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400 (%s)", rec.Code, rec.Body)
		}
	})

	t.Run("deactivate takes no body and answers with the vehicle", func(t *testing.T) {
		rec := as(t, router, provider, http.MethodPost, path+"/deactivate", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
		}

		body := decode[vehicleResponse](t, rec)
		if body.Active {
			t.Error("the vehicle is still in service")
		}
		if body.DeactivatedAt == "" {
			t.Error("deactivated_at is absent on a vehicle out of service")
		}

		// Again, with what would be a fresh idempotency key: absorbed rather than refused.
		if again := as(t, router, provider, http.MethodPost, path+"/deactivate", ""); again.Code != http.StatusOK {
			t.Errorf("deactivating twice = %d, want 200 (%s)", again.Code, again.Body)
		}
	})

	t.Run("reactivate brings it back", func(t *testing.T) {
		rec := as(t, router, provider, http.MethodPost, path+"/reactivate", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
		}
		body := decode[vehicleResponse](t, rec)
		if !body.Active {
			t.Error("the vehicle did not return to service")
		}
		if body.DeactivatedAt != "" {
			t.Errorf("deactivated_at = %q on a vehicle in service", body.DeactivatedAt)
		}
	})

	t.Run("the read answers with the same shape the writes do", func(t *testing.T) {
		read := as(t, router, provider, http.MethodGet, path, "")
		if read.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", read.Code, read.Body)
		}
		again := as(t, router, provider, http.MethodPost, path+"/reactivate", "")

		if read.Body.String() != again.Body.String() {
			t.Errorf("the read and the write answer with different shapes:\n%s\n%s", read.Body, again.Body)
		}
	})
}

// TestAStrangerCannotTellAVehicleFromNothingAtAll.
//
// 404 rather than 403, byte-identical to a vehicle that does not exist. A 403 would confirm that a
// vehicle with that id exists, and which vehicles a competitor runs is commercial information they
// never published.
func TestAStrangerCannotTellAVehicleFromNothingAtAll(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	owner := newProvider(t, pool, "http-owner@example.com", "+61400000335")
	stranger := newProvider(t, pool, "http-stranger@example.com", "+61400000336")

	vehicle := added(t, pool, owner, "STR111")
	missing := "00000000-0000-7000-8000-000000000000"

	for _, target := range []struct {
		method, suffix, body string
	}{
		{http.MethodGet, "", ""},
		{http.MethodPatch, "", `{"make": "Hino"}`},
		{http.MethodPost, "/deactivate", ""},
		{http.MethodPost, "/reactivate", ""},
	} {
		theirs := as(t, router, stranger, target.method,
			"/v1/fleet/vehicles/"+vehicle.ID.String()+target.suffix, target.body)
		nothing := as(t, router, stranger, target.method,
			"/v1/fleet/vehicles/"+missing+target.suffix, target.body)

		if theirs.Code != http.StatusNotFound || nothing.Code != http.StatusNotFound {
			t.Errorf("%s%s: somebody else's = %d, no such vehicle = %d; want 404 for both",
				target.method, target.suffix, theirs.Code, nothing.Code)
			continue
		}
		if theirs.Body.String() != nothing.Body.String() {
			t.Errorf("%s%s: somebody else's vehicle answers differently from no vehicle at all:\n%s\n%s",
				target.method, target.suffix, theirs.Body, nothing.Body)
		}
	}

	// A malformed id is bad_request rather than not_found, which is httpx's own division: it is
	// part of how the request was addressed rather than a guess at an identifier.
	if rec := as(t, router, stranger, http.MethodGet, "/v1/fleet/vehicles/not-a-uuid", ""); rec.Code != http.StatusBadRequest {
		t.Errorf("a malformed id = %d, want 400", rec.Code)
	}
}

// TestTheListIsTheEnvelopeAndTheFilterIsRefusedWhenItIsNonsense.
func TestTheListIsTheEnvelopeAndTheFilterIsRefusedWhenItIsNonsense(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	provider := newProvider(t, pool, "http-list@example.com", "+61400000337")
	fresh := newProvider(t, pool, "http-list-empty@example.com", "+61400000338")

	added(t, pool, provider, "LSA111")
	added(t, pool, provider, "LSA222")

	t.Run("the envelope", func(t *testing.T) {
		rec := as(t, router, provider, http.MethodGet, "/v1/fleet/vehicles", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
		}

		var page struct {
			Data       []vehicleResponse `json:"data"`
			NextCursor string            `json:"next_cursor"`
			HasMore    bool              `json:"has_more"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("the response is not the envelope: %v (%s)", err, rec.Body)
		}
		if len(page.Data) != 2 {
			t.Errorf("%d vehicles, want 2", len(page.Data))
		}
		if page.HasMore || page.NextCursor != "" {
			t.Errorf("a complete page claims more: %s", rec.Body)
		}
	})

	t.Run("an empty fleet is an empty array, never null", func(t *testing.T) {
		rec := as(t, router, fresh, http.MethodGet, "/v1/fleet/vehicles", "")
		if got := strings.ReplaceAll(rec.Body.String(), " ", ""); !strings.Contains(got, `"data":[]`) {
			t.Errorf("an empty fleet is %s", rec.Body)
		}
	})

	t.Run("paging follows next_cursor", func(t *testing.T) {
		rec := as(t, router, provider, http.MethodGet, "/v1/fleet/vehicles?limit=1", "")
		var page struct {
			Data       []vehicleResponse `json:"data"`
			NextCursor string            `json:"next_cursor"`
			HasMore    bool              `json:"has_more"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
			t.Fatalf("the response is not the envelope: %v", err)
		}
		if !page.HasMore || page.NextCursor == "" {
			t.Fatalf("the first page of two claims to be the last: %s", rec.Body)
		}

		next := as(t, router, provider, http.MethodGet,
			"/v1/fleet/vehicles?limit=1&cursor="+page.NextCursor, "")
		var second struct {
			Data    []vehicleResponse `json:"data"`
			HasMore bool              `json:"has_more"`
		}
		if err := json.Unmarshal(next.Body.Bytes(), &second); err != nil {
			t.Fatalf("the second page is not the envelope: %v", err)
		}
		if len(second.Data) != 1 || second.Data[0].ID == page.Data[0].ID {
			t.Errorf("the cursor did not advance: %s", next.Body)
		}
		if second.HasMore {
			t.Errorf("the last page of two claims more: %s", next.Body)
		}
	})

	t.Run("nonsense parameters are refused rather than read as a default", func(t *testing.T) {
		// Answering `?active=yes` with the retired vehicles would tell a client its filter
		// worked, which is the failure the status filter in `jobs` refuses for the same reason.
		for _, query := range []string{"?active=yes", "?limit=0", "?cursor=not-a-cursor"} {
			rec := as(t, router, provider, http.MethodGet, "/v1/fleet/vehicles"+query, "")
			if rec.Code != http.StatusBadRequest {
				t.Errorf("%s = %d, want 400 (%s)", query, rec.Code, rec.Body)
			}
		}
		// An over-large limit is narrowed rather than refused: a client asking for more than
		// the platform will give is asking for a page, not making a mistake.
		if rec := as(t, router, provider, http.MethodGet, "/v1/fleet/vehicles?limit=5000", ""); rec.Code != http.StatusOK {
			t.Errorf("?limit=5000 = %d, want it narrowed to the maximum", rec.Code)
		}
	})
}
