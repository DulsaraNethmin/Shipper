package delivery

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

// The wire contract of SHIP-106.
//
// These drive the handler on a mux of its own rather than through cmd/api's router, because what is
// being checked here is the domain's own half: what it accepts, what it refuses, and what shape it
// puts on the wire. The middleware around it — authentication, idempotency, the error envelope —
// belongs to cmd/api and is tested there, and the whole stack is exercised against the real binary
// by scripts/verify/70-delivery.sh.

// newTestRouter mounts the handler on the pattern cmd/api registers it under.
//
// A real ServeMux rather than calling the handler directly, because the path parameter is part of
// what is being tested: the handler reads {id} through r.PathValue, and a handler invoked without a
// pattern would see an empty one and pass a test that the served route would fail.
func newTestRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newTestService(), pool,
		slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /v1/jobs/{id}/driver", handler.AssignDriver())
	return mux
}

// as sends a request on behalf of an authenticated caller.
//
// The subject is put on the context by hand, which is what httpx.ResolveSubject does one layer out.
// The route declares RequireUser, so a handler reached without one would be a wiring defect rather
// than a request anybody could send.
//
// **The role on the subject is deliberately customer**, whatever the account is. It is a claim in a
// token, and this endpoint decides from the accepted bid instead — so a test that carried the
// "right" role here would be proving something about its own fixture.
func as(t *testing.T, h http.Handler, caller uuid.UUID, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
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

type assignmentBody struct {
	ID           string `json:"id"`
	JobID        string `json:"job_id"`
	DriverName   string `json:"driver_name"`
	DriverMobile string `json:"driver_mobile"`
	AssignedAt   string `json:"assigned_at"`
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

// driverPath is the route as cmd/api serves it.
func driverPath(jobID uuid.UUID) string { return "/v1/jobs/" + jobID.String() + "/driver" }

// TestAssignEndpointAnswersWithTheAssignmentItCreated is SHIP-106 at the wire.
func TestAssignEndpointAnswersWithTheAssignmentItCreated(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-assign-c@example.com", "+61400000640", "customer")
	provider := newAccount(t, pool, "http-assign-p@example.com", "+61400000641", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	rec := as(t, router, provider, driverPath(jobID),
		`{"driver_name": "Sam Patel", "driver_mobile": "0412 345 678"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	body := decode[assignmentBody](t, rec)
	switch {
	case body.ID == "":
		t.Error("the response carries no assignment id")
	case body.JobID != jobID.String():
		t.Errorf("job_id = %q, want %s", body.JobID, jobID)
	case body.DriverName != "Sam Patel":
		t.Errorf("driver_name = %q", body.DriverName)
	case body.DriverMobile != "+61412345678":
		t.Errorf("driver_mobile = %q, want the E.164 form", body.DriverMobile)
	case !strings.HasSuffix(body.AssignedAt, "Z"):
		t.Errorf("assigned_at = %q, want UTC", body.AssignedAt)
	}

	// The status is not echoed, on purpose: it is the jobs endpoints' vocabulary. A test rather
	// than a comment, because the field is the sort of thing that gets added helpfully.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	if _, present := raw["status"]; present {
		t.Error("the response carries a job status, which is the jobs domain's to serialise")
	}
}

// TestSelfAssignmentNeedsNoMobile is the second half of the Done when, at the wire.
func TestSelfAssignmentNeedsNoMobile(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-self-c@example.com", "+61400000642", "customer")
	provider := newAccount(t, pool, "http-self-p@example.com", "+61400000643", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	rec := as(t, router, provider, driverPath(jobID), `{"driver_name": "Ravi Chandra", "self": true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}
	if got := decode[assignmentBody](t, rec).DriverMobile; got != "+61400000643" {
		t.Errorf("driver_mobile = %q, want the provider's own number", got)
	}
}

// TestARepeatedNominationAnswers200 distinguishes the two successes.
func TestARepeatedNominationAnswers200(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-twice-c@example.com", "+61400000644", "customer")
	provider := newAccount(t, pool, "http-twice-p@example.com", "+61400000645", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	body := `{"driver_name": "Sam Patel", "driver_mobile": "+61412345678"}`

	first := as(t, router, provider, driverPath(jobID), body)
	if first.Code != http.StatusCreated {
		t.Fatalf("the first call = %d, want 201 (%s)", first.Code, first.Body)
	}

	second := as(t, router, provider, driverPath(jobID), body)
	if second.Code != http.StatusOK {
		t.Fatalf("the repeat = %d, want 200 (%s)", second.Code, second.Body)
	}
	if decode[assignmentBody](t, second).ID != decode[assignmentBody](t, first).ID {
		t.Error("the repeat answered with a different assignment")
	}
}

// TestTheWireRefusals covers every failure a client can produce, and the code each carries.
//
// Clients branch on `error.code` and never on the message (Docs/10 §4.4), so the code is what is
// asserted.
func TestTheWireRefusals(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-refuse-c@example.com", "+61400000646", "customer")
	provider := newAccount(t, pool, "http-refuse-p@example.com", "+61400000647", "provider")
	stranger := newAccount(t, pool, "http-refuse-x@example.com", "+61400000648", "provider")

	awarded := awardedJob(t, pool, customer, provider)
	assigned := awardedJob(t, pool, customer, provider)
	draft := newDraft(t, pool, customer)
	acceptBid(t, pool, draft, provider)

	if rec := as(t, router, provider, driverPath(assigned),
		`{"driver_name": "Sam Patel", "driver_mobile": "+61412345678"}`); rec.Code != http.StatusCreated {
		t.Fatalf("setting up the assigned job: %d (%s)", rec.Code, rec.Body)
	}

	for _, tc := range []struct {
		name   string
		caller uuid.UUID
		target string
		body   string
		status int
		code   string
		field  string
	}{
		{
			name:   "another provider's job is not found",
			caller: stranger, target: driverPath(awarded),
			body:   `{"driver_name": "Sam Patel", "driver_mobile": "+61412345678"}`,
			status: http.StatusNotFound, code: "not_found",
		},
		{
			name:   "a job that does not exist is the same answer",
			caller: provider, target: driverPath(uuid.Must(uuid.NewV7())),
			body:   `{"driver_name": "Sam Patel", "driver_mobile": "+61412345678"}`,
			status: http.StatusNotFound, code: "not_found",
		},
		{
			name:   "a job that has not been awarded cannot take a driver",
			caller: provider, target: driverPath(draft),
			body:   `{"driver_name": "Sam Patel", "driver_mobile": "+61412345678"}`,
			status: http.StatusConflict, code: "delivery_job_not_assignable",
		},
		{
			name:   "a second driver is refused",
			caller: provider, target: driverPath(assigned),
			body:   `{"driver_name": "Ravi Chandra", "driver_mobile": "+61412000999"}`,
			status: http.StatusConflict, code: "delivery_driver_already_assigned",
		},
		{
			name:   "a malformed job id is a bad request",
			caller: provider, target: "/v1/jobs/not-a-uuid/driver",
			body:   `{"driver_name": "Sam Patel", "driver_mobile": "+61412345678"}`,
			status: http.StatusBadRequest, code: "bad_request",
		},
		{
			name:   "an unknown field is a client typo rather than something to ignore",
			caller: provider, target: driverPath(awarded),
			body:   `{"driver_name": "Sam Patel", "driver_mobile": "+61412345678", "driver_email": "sam@example.com"}`,
			status: http.StatusBadRequest, code: "bad_request",
		},
		{
			name:   "a mobile that is not a number is reported against its field",
			caller: provider, target: driverPath(awarded),
			body:   `{"driver_name": "Sam Patel", "driver_mobile": "not a phone"}`,
			status: 422, code: "validation_failed", field: "driver_mobile",
		},
		{
			name:   "a self-assignment carrying a mobile is refused",
			caller: provider, target: driverPath(awarded),
			body:   `{"driver_name": "Sam Patel", "driver_mobile": "+61412345678", "self": true}`,
			status: 422, code: "validation_failed", field: "driver_mobile",
		},
		{
			name:   "a nomination with no name at all",
			caller: provider, target: driverPath(awarded),
			body:   `{"driver_mobile": "+61412345678"}`,
			status: 422, code: "validation_failed", field: "driver_name",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := as(t, router, tc.caller, tc.target, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}

			envelope := decode[errorEnvelope](t, rec)
			if envelope.Error.Code != tc.code {
				t.Errorf("error.code = %q, want %q", envelope.Error.Code, tc.code)
			}
			if tc.field == "" {
				return
			}
			for _, d := range envelope.Error.Details {
				if d.Field == tc.field {
					return
				}
			}
			t.Errorf("no detail about %s: %s", tc.field, rec.Body)
		})
	}
}

// TestNoDriverIsRecordedWhenTheJobCannotMove is the transaction boundary seen from the wire.
//
// The assignment row is written before the transition is attempted, so a refused move must take it
// with it. A job that answered 409 and kept a driver would be the exact inconsistency the single
// transaction exists to prevent.
func TestNoDriverIsRecordedWhenTheJobCannotMove(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-rollback-c@example.com", "+61400000649", "customer")
	provider := newAccount(t, pool, "http-rollback-p@example.com", "+61400000650", "provider")

	jobID := newDraft(t, pool, customer)
	acceptBid(t, pool, jobID, provider)

	rec := as(t, router, provider, driverPath(jobID),
		`{"driver_name": "Sam Patel", "driver_mobile": "+61412345678"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", rec.Code, rec.Body)
	}

	var rows int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM driver_assignments WHERE job_id = $1`, jobID).Scan(&rows); err != nil {
		t.Fatalf("counting assignments: %v", err)
	}
	if rows != 0 {
		t.Errorf("%d assignment rows survived a refused transition, want 0", rows)
	}
}

// TestTheEndpointAnswers503WithNoDatabase.
//
// The pool may be nil — the service starts with an unreachable database on purpose — and the
// difference between 503 and 500 is what a mobile client does next: retry, or tell the person
// holding the phone that something is broken.
func TestTheEndpointAnswers503WithNoDatabase(t *testing.T) {
	router := newTestRouter(t, nil)

	rec := as(t, router, uuid.Must(uuid.NewV7()), driverPath(uuid.Must(uuid.NewV7())),
		`{"driver_name": "Sam Patel", "driver_mobile": "+61412345678"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
	}
	if got := decode[errorEnvelope](t, rec).Error.Code; got != "service_unavailable" {
		t.Errorf("error.code = %q, want service_unavailable", got)
	}
}
