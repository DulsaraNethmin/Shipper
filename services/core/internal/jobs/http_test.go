package jobs

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// The wire contract of SHIP-61, SHIP-62, SHIP-64, SHIP-65 and SHIP-66.
//
// These drive the handlers on a mux of their own rather than through cmd/api's router, because what
// is being checked here is the domain's own half: what it accepts, what it refuses, and what shape
// it puts on the wire. The middleware around them — authentication, idempotency, the error envelope
// — belongs to cmd/api and is tested there.

// newTestRouter mounts every handler on the pattern cmd/api registers it under.
//
// A real ServeMux rather than calling the handlers directly, because the path parameter is part of
// what is being tested: Update and Detail read {id} through r.PathValue, and a handler invoked
// without a pattern would see an empty one and pass a test that the served route would fail.
//
// A handler added here and not to cmd/api/routes_jobs.go is an endpoint that exists only in the
// tests, so the two lists are worth reading against each other — routes_golden.txt is what makes
// the other direction visible.
func newTestRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newDraftService(t, &fakeGeocoder{}), pool,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/jobs", handler.List())
	mux.Handle("POST /v1/jobs", handler.Create())
	mux.Handle("GET /v1/jobs/{id}", handler.Detail())
	mux.Handle("PATCH /v1/jobs/{id}", handler.Update())
	mux.Handle("POST /v1/jobs/{id}/cancel", handler.Cancel())
	mux.Handle("POST /v1/jobs/{id}/extend", handler.Extend())
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
func TestTheEndpointsRefuseAStatusField(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-status@example.com", "+61400000621")

	created := decode[jobResponse](t, as(t, router, customer, http.MethodPost, "/v1/jobs", `{}`))

	cases := map[string]struct {
		method string
		target string
	}{
		"on create": {http.MethodPost, "/v1/jobs"},
		"on edit":   {http.MethodPatch, "/v1/jobs/" + created.ID},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			rec := as(t, router, customer, tc.method, tc.target, `{"status": "open"}`)

			// 400 rather than a quietly ignored field. A client that believes it
			// published a job and did not will retry forever.
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if body := decode[errorEnvelope](t, rec); body.Error.Code != "bad_request" {
				t.Errorf("code = %q, want bad_request", body.Error.Code)
			}
			if !strings.Contains(rec.Body.String(), "status") {
				t.Errorf("the message does not name the offending field: %s", rec.Body)
			}
		})
	}

	// And the job did not move.
	if got := statusOf(t, pool, uuid.MustParse(created.ID)); got != StatusDraft {
		t.Errorf("the job is %s, want Draft", got)
	}
}

// TestEditingSomebodyElsesJobIsIndistinguishableFromItNotExisting is SHIP-62's ownership rule at
// the wire, and the disclosure question is the whole of it.
func TestEditingSomebodyElsesJobIsIndistinguishableFromItNotExisting(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	owner := newCustomer(t, pool, "http-owner@example.com", "+61400000622")
	stranger := newCustomer(t, pool, "http-stranger@example.com", "+61400000623")

	created := decode[jobResponse](t, as(t, router, owner, http.MethodPost, "/v1/jobs",
		`{"goods_description": "A piano"}`))

	theirs := as(t, router, stranger, http.MethodPatch, "/v1/jobs/"+created.ID,
		`{"goods_description": "A cheap piano"}`)
	nothing := as(t, router, stranger, http.MethodPatch,
		"/v1/jobs/"+uuid.Must(uuid.NewV7()).String(), `{"goods_description": "A cheap piano"}`)

	if theirs.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — 403 would confirm the job exists (%s)", theirs.Code, theirs.Body)
	}
	if nothing.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", nothing.Code, nothing.Body)
	}

	// Byte-identical, not merely the same status. A message that differed would disclose
	// exactly what the status code was chosen to hide.
	if theirs.Body.String() != nothing.Body.String() {
		t.Errorf("a stranger can tell somebody else's job from no job at all:\n %s\n %s",
			theirs.Body, nothing.Body)
	}

	if stored := reread(t, pool, uuid.MustParse(created.ID)); stored.GoodsDescription != "A piano" {
		t.Errorf("goods description = %q, want it unchanged", stored.GoodsDescription)
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

// A published job is refused with 409 and a code that tells the client to reload rather than to
// sign in or give up.
func TestEditingAPublishedJobIsAConflict(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-published@example.com", "+61400000625")

	created := decode[jobResponse](t, as(t, router, customer, http.MethodPost, "/v1/jobs", `{}`))
	publish(t, pool, uuid.MustParse(created.ID), customer)

	rec := as(t, router, customer, http.MethodPatch, "/v1/jobs/"+created.ID,
		`{"goods_description": "changed"}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if body := decode[errorEnvelope](t, rec); body.Error.Code != string(CodeNotADraft) {
		t.Errorf("code = %q, want %s", body.Error.Code, CodeNotADraft)
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

// A path parameter of the wrong shape is bad_request, which is httpx's own description of that
// code — and it discloses nothing, because the answer is the same whether or not a job exists.
func TestAMalformedJobIDIsRefusedBeforeAnythingIsRead(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-badid@example.com", "+61400000627")

	rec := as(t, router, customer, http.MethodPatch, "/v1/jobs/not-a-uuid",
		`{"goods_description": "anything"}`)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
}

// TestCancelEndpointAnswersWithTheCancelledJob is SHIP-64's acceptance criterion at the wire, and
// the first time an endpoint in this service moves a job.
func TestCancelEndpointAnswersWithTheCancelledJob(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-cancel@example.com", "+61400000628")

	created := decode[jobResponse](t, as(t, router, customer, http.MethodPost, "/v1/jobs", `{}`))

	rec := as(t, router, customer, http.MethodPost, "/v1/jobs/"+created.ID+"/cancel",
		`{"reason": "Found a cheaper option elsewhere."}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if body := decode[jobResponse](t, rec); body.Status != "cancelled" {
		t.Errorf("status = %q, want cancelled", body.Status)
	}

	// Read from the column rather than from the answer the endpoint gave about itself.
	if got := statusOf(t, pool, uuid.MustParse(created.ID)); got != StatusCancelled {
		t.Errorf("the stored job is %s, want Cancelled", got)
	}
}

// A job that cannot be cancelled is a 409 with a code the client can act on, rather than a bare
// 403 or a silent success.
func TestCancellingAnAwardedJobIsAConflict(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-cancel-awarded@example.com", "+61400000629")

	created := decode[jobResponse](t, as(t, router, customer, http.MethodPost, "/v1/jobs", `{}`))
	job := uuid.MustParse(created.ID)
	publish(t, pool, job, customer)
	move(t, pool, job, StatusAwarded, User(ActorCustomer, customer))

	rec := as(t, router, customer, http.MethodPost, "/v1/jobs/"+created.ID+"/cancel", `{}`)

	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}
	if body := decode[errorEnvelope](t, rec); body.Error.Code != string(CodeNotCancellable) {
		t.Errorf("code = %q, want %s", body.Error.Code, CodeNotCancellable)
	}
}

// TestCancellingSomebodyElsesJobIsIndistinguishableFromItNotExisting applies SHIP-62's disclosure
// rule to the new endpoint, and compares the bodies byte for byte rather than the statuses.
//
// The rule is the reason it is worth repeating per endpoint: a job is discoverable through
// whichever route forgets it, not through the strictest one.
func TestCancellingSomebodyElsesJobIsIndistinguishableFromItNotExisting(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	owner := newCustomer(t, pool, "http-cancel-owner@example.com", "+61400000630")
	stranger := newCustomer(t, pool, "http-cancel-stranger@example.com", "+61400000631")

	created := decode[jobResponse](t, as(t, router, owner, http.MethodPost, "/v1/jobs", `{}`))

	theirs := as(t, router, stranger, http.MethodPost, "/v1/jobs/"+created.ID+"/cancel", `{}`)
	nothing := as(t, router, stranger, http.MethodPost,
		"/v1/jobs/"+uuid.Must(uuid.NewV7()).String()+"/cancel", `{}`)

	if theirs.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — 403 would confirm the job exists (%s)", theirs.Code, theirs.Body)
	}
	if theirs.Body.String() != nothing.Body.String() {
		t.Errorf("a stranger can tell somebody else's job from no job at all:\n %s\n %s",
			theirs.Body, nothing.Body)
	}

	if got := statusOf(t, pool, uuid.MustParse(created.ID)); got != StatusDraft {
		t.Errorf("the job is %s after a refused cancellation", got)
	}
}

// TestExtendEndpointAnswersWithTheExtendedJob is SHIP-70's *Done when* at the wire: one call, an
// empty body, and the job comes back with a deadline further away than it had.
func TestExtendEndpointAnswersWithTheExtendedJob(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newCustomer(t, pool, "http-extend@example.com", "+61400000640")
	job := expiringJob(t, pool, customer, 24*time.Hour, time.Time{})

	rec := as(t, router, customer, http.MethodPost, "/v1/jobs/"+job.String()+"/extend", `{}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[jobResponse](t, rec)
	if body.Status != "open" {
		t.Errorf("status = %q, want open — an extension moves no job", body.Status)
	}
	if want := timestamp(testInstant.Add(ExtensionPeriod)); body.ExpiresAt != want {
		t.Errorf("expires_at = %q, want %q", body.ExpiresAt, want)
	}

	// The same shape every other job endpoint answers with, so a client parses one type whatever
	// it did to obtain the job.
	detail := as(t, router, customer, http.MethodGet, "/v1/jobs/"+job.String(), "")
	if detail.Body.String() != rec.Body.String() {
		t.Errorf("the extension and the read answer with different shapes:\n %s\n %s",
			rec.Body, detail.Body)
	}
}

// TestExtendRefusesToBeToldHowLong is the request type having no fields, demonstrated rather than
// asserted.
//
// Docs/02 §6.3 asks for an extension "in one action" and the platform computes the deadline. A
// client that sent a period and had it silently ignored would believe it bought thirty days and
// have no way to tell from a successful response — so the unknown field is reported.
func TestExtendRefusesToBeToldHowLong(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newCustomer(t, pool, "http-extend-days@example.com", "+61400000641")
	job := expiringJob(t, pool, customer, 24*time.Hour, time.Time{})

	rec := as(t, router, customer, http.MethodPost, "/v1/jobs/"+job.String()+"/extend",
		`{"days": 30}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "days") {
		t.Errorf("the refusal does not name the field: %s", rec.Body)
	}

	// An empty body is refused too, on the rule SHIP-64 set: httpx.DecodeJSON needs one, and a
	// second endpoint excepted from that would be a second answer to what a request looks like.
	empty := as(t, router, customer, http.MethodPost, "/v1/jobs/"+job.String()+"/extend", "")
	if empty.Code != http.StatusBadRequest {
		t.Errorf("an empty body = %d, want 400 (%s)", empty.Code, empty.Body)
	}

	if at := deadlineOf(t, pool, job); at == nil || !at.Equal(testInstant.Add(24*time.Hour)) {
		t.Errorf("a refused request moved the deadline to %v", at)
	}
}

// TestExtendingAJobThatCannotBeIsAConflict covers both refusals under one code.
//
// 409 rather than 403: the caller is permitted and the request contradicts the state the job is in.
// The client's action is the same for both — reload and offer what is actually available — which is
// why they share `jobs_not_extendable` and differ only in the sentence.
func TestExtendingAJobThatCannotBeIsAConflict(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newCustomer(t, pool, "http-extend-conflict@example.com", "+61400000642")

	draft := decode[jobResponse](t, as(t, router, customer, http.MethodPost, "/v1/jobs", `{}`))

	pickupEnd := testInstant.Add(30 * time.Hour).Truncate(time.Millisecond)
	bound := expiringJob(t, pool, customer, 30*time.Hour, pickupEnd)

	for name, id := range map[string]string{
		"a draft":                     draft.ID,
		"a job bounded by its pickup": bound.String(),
	} {
		rec := as(t, router, customer, http.MethodPost, "/v1/jobs/"+id+"/extend", `{}`)
		if rec.Code != http.StatusConflict {
			t.Errorf("%s = %d, want 409 (%s)", name, rec.Code, rec.Body)
			continue
		}
		if code := decode[errorEnvelope](t, rec).Error.Code; code != "jobs_not_extendable" {
			t.Errorf("%s: code = %q, want jobs_not_extendable", name, code)
		}
	}
}

// TestExtendingSomebodyElsesJobIsIndistinguishableFromItNotExisting applies SHIP-62's disclosure
// rule to the third endpoint that names a job.
//
// Repeated per endpoint rather than trusted per domain, for the reason the cancellation version
// gives: a job is discoverable through whichever route forgets it.
func TestExtendingSomebodyElsesJobIsIndistinguishableFromItNotExisting(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	owner := newCustomer(t, pool, "http-extend-owner@example.com", "+61400000643")
	stranger := newCustomer(t, pool, "http-extend-stranger@example.com", "+61400000644")

	job := expiringJob(t, pool, owner, 24*time.Hour, time.Time{})

	theirs := as(t, router, stranger, http.MethodPost, "/v1/jobs/"+job.String()+"/extend", `{}`)
	nothing := as(t, router, stranger, http.MethodPost,
		"/v1/jobs/"+uuid.Must(uuid.NewV7()).String()+"/extend", `{}`)

	if theirs.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — 403 would confirm the job exists (%s)", theirs.Code, theirs.Body)
	}
	if theirs.Body.String() != nothing.Body.String() {
		t.Errorf("a stranger can tell somebody else's job from no job at all:\n %s\n %s",
			theirs.Body, nothing.Body)
	}

	if at := deadlineOf(t, pool, job); at == nil || !at.Equal(testInstant.Add(24*time.Hour)) {
		t.Errorf("the stranger's refused extension moved the deadline to %v", at)
	}
}

// TestDetailEndpointAnswersWithTheSameShapeTheWritesDo is SHIP-65's acceptance criterion at the
// wire.
//
// Compared byte for byte against the create response rather than field by field, because the
// property worth holding is that there is one shape: a client that parses the answer to POST must
// parse the answer to GET with the same type.
func TestDetailEndpointAnswersWithTheSameShapeTheWritesDo(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-detail@example.com", "+61400000632")

	body := `{
		"pickup":  {"line": "12 Smith Street", "suburb": "Newtown", "state": "nsw", "postcode": "2042"},
		"goods_description": "Two-seater sofa",
		"weight_kg": 45.5,
		"handling_notes": "Second-floor walk-up, no lift."
	}`
	written := as(t, router, customer, http.MethodPost, "/v1/jobs", body)
	created := decode[jobResponse](t, written)

	rec := as(t, router, customer, http.MethodGet, "/v1/jobs/"+created.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if rec.Body.String() != written.Body.String() {
		t.Errorf("the detail response differs from the create response:\n GET  %s\n POST %s",
			rec.Body, written.Body)
	}

	read := decode[jobResponse](t, rec)
	switch {
	case read.Status != "draft":
		t.Errorf("status = %q, want draft", read.Status)
	case read.GoodsDescription != "Two-seater sofa":
		t.Errorf("goods_description = %q", read.GoodsDescription)
	case read.HandlingNotes != "Second-floor walk-up, no lift.":
		t.Errorf("handling_notes = %q", read.HandlingNotes)
	case read.Pickup == nil || read.Pickup.State != "NSW":
		t.Errorf("pickup = %#v", read.Pickup)
	}
}

// TestReadingSomebodyElsesJobIsIndistinguishableFromItNotExisting is the half of SHIP-65 that is
// substantive: "for the owning customer only".
func TestReadingSomebodyElsesJobIsIndistinguishableFromItNotExisting(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	owner := newCustomer(t, pool, "http-detail-owner@example.com", "+61400000633")
	stranger := newCustomer(t, pool, "http-detail-stranger@example.com", "+61400000634")

	created := decode[jobResponse](t, as(t, router, owner, http.MethodPost, "/v1/jobs",
		`{"goods_description": "A piano"}`))

	theirs := as(t, router, stranger, http.MethodGet, "/v1/jobs/"+created.ID, "")
	nothing := as(t, router, stranger, http.MethodGet,
		"/v1/jobs/"+uuid.Must(uuid.NewV7()).String(), "")

	if theirs.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 — 403 would confirm the job exists (%s)", theirs.Code, theirs.Body)
	}
	if theirs.Body.String() != nothing.Body.String() {
		t.Errorf("a stranger can tell somebody else's job from no job at all:\n %s\n %s",
			theirs.Body, nothing.Body)
	}

	// And nothing about the job leaked into the refusal.
	if strings.Contains(theirs.Body.String(), "piano") {
		t.Errorf("the refusal carries the job's contents: %s", theirs.Body)
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

// jobPage is the envelope Docs/10 §4.5 gives every list, decoded.
type jobPage struct {
	Data       []jobResponse `json:"data"`
	NextCursor string        `json:"next_cursor"`
	HasMore    bool          `json:"has_more"`
}

// TestListEndpointPagesThroughTheCallersOwnJobs is SHIP-66's acceptance criterion at the wire, over
// the cursor a client actually receives rather than the domain value behind it.
//
// The client is followed the way a client would follow it: take next_cursor, send it back, stop
// when has_more is false. A cursor that did not survive the encode/decode round trip fails here and
// nowhere else.
func TestListEndpointPagesThroughTheCallersOwnJobs(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	mine := newCustomer(t, pool, "http-list@example.com", "+61400000635")
	theirs := newCustomer(t, pool, "http-list-other@example.com", "+61400000636")

	for range 5 {
		as(t, router, mine, http.MethodPost, "/v1/jobs", `{}`)
	}
	as(t, router, theirs, http.MethodPost, "/v1/jobs", `{"goods_description": "not yours"}`)

	seen := map[string]int{}
	target := "/v1/jobs?limit=2"

	for pages := 0; ; pages++ {
		if pages > 5 {
			t.Fatal("paging did not terminate; the cursor is not advancing")
		}

		rec := as(t, router, mine, http.MethodGet, target, "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
		}

		page := decode[jobPage](t, rec)
		for _, job := range page.Data {
			seen[job.ID]++
		}
		if !page.HasMore {
			if page.NextCursor != "" {
				t.Error("the last page carries a cursor to a page that does not exist")
			}
			break
		}
		if page.NextCursor == "" {
			t.Fatal("has_more is true and there is no cursor to follow")
		}
		target = "/v1/jobs?limit=2&cursor=" + url.QueryEscape(page.NextCursor)
	}

	if len(seen) != 5 {
		t.Fatalf("paging returned %d distinct jobs, want the caller's 5", len(seen))
	}
	for id, times := range seen {
		if times != 1 {
			t.Errorf("%s appeared %d times across the pages", id, times)
		}
	}

	// The other customer's job never appeared, which is checked by its contents rather than by
	// its id: a list that leaked it would leak the whole shape.
	all := as(t, router, mine, http.MethodGet, "/v1/jobs", "")
	if strings.Contains(all.Body.String(), "not yours") {
		t.Errorf("the list carries another customer's job: %s", all.Body)
	}
}

// An empty list is `"data": []`, never `null`.
//
// A client writing `for (final j in body['data'])` breaks on null the first time a new customer
// opens the app, and never again in testing.
func TestAnEmptyListSendsAnEmptyArray(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-list-empty@example.com", "+61400000637")

	rec := as(t, router, customer, http.MethodGet, "/v1/jobs", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if got := strings.TrimSpace(rec.Body.String()); got != `{"data":[],"has_more":false}` {
		t.Errorf("an empty list is %s", got)
	}
}

// The status filter takes the wire form, and a status that is not one of the twelve is a
// bad_request rather than an empty list — which would tell a client its filter worked.
func TestTheListFiltersOnTheWireStatus(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-list-status@example.com", "+61400000638")

	first := decode[jobResponse](t, as(t, router, customer, http.MethodPost, "/v1/jobs", `{}`))
	as(t, router, customer, http.MethodPost, "/v1/jobs", `{}`)
	publish(t, pool, uuid.MustParse(first.ID), customer)

	open := decode[jobPage](t, as(t, router, customer, http.MethodGet, "/v1/jobs?status=open", ""))
	if len(open.Data) != 1 || open.Data[0].ID != first.ID {
		t.Errorf("?status=open returned %d jobs", len(open.Data))
	}

	drafts := decode[jobPage](t, as(t, router, customer, http.MethodGet, "/v1/jobs?status=draft", ""))
	if len(drafts.Data) != 1 {
		t.Errorf("?status=draft returned %d jobs", len(drafts.Data))
	}

	// The stored form is not the wire form, and only the wire form is accepted — otherwise
	// two spellings of one status would both work and clients would drift onto either.
	for _, wrong := range []string{"Draft", "not-a-status", "en route to pickup"} {
		rec := as(t, router, customer, http.MethodGet, "/v1/jobs?status="+url.QueryEscape(wrong), "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("?status=%q returned %d, want 400 (%s)", wrong, rec.Code, rec.Body)
		}
	}
}

// TestAListRefusesACursorItDidNotIssue is the domain half of the cursor's validation.
//
// internal/pagination establishes the shape — this version, two fields. Only the domain knows the
// fields have to be a timestamp and a UUID, and a cursor that decoded but did not parse would
// otherwise reach the query as a zero time and silently answer with the first page.
func TestAListRefusesACursorItDidNotIssue(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)
	customer := newCustomer(t, pool, "http-list-cursor@example.com", "+61400000639")

	as(t, router, customer, http.MethodPost, "/v1/jobs", `{}`)

	twoFieldsOfNonsense := pagination.Cursor{"yesterday", "not-a-uuid"}.Encode()

	for _, cursor := range []string{"!!!", "YWJj", twoFieldsOfNonsense} {
		rec := as(t, router, customer, http.MethodGet,
			"/v1/jobs?cursor="+url.QueryEscape(cursor), "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("cursor %q returned %d, want 400 (%s)", cursor, rec.Code, rec.Body)
		}
		if body := decode[errorEnvelope](t, rec); body.Error.Code != "bad_request" {
			t.Errorf("cursor %q gave code %q, want bad_request", cursor, body.Error.Code)
		}
	}

	// A limit that is not a positive whole number is a client defect, and answering it with the
	// default would hide one.
	for _, limit := range []string{"0", "-1", "twenty"} {
		rec := as(t, router, customer, http.MethodGet, "/v1/jobs?limit="+limit, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("limit %q returned %d, want 400 (%s)", limit, rec.Code, rec.Body)
		}
	}
}
