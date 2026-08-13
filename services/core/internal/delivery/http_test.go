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
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
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
	mux.Handle("POST /v1/jobs/{id}/milestones", handler.RecordMilestone())
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

	DriverToken          string `json:"driver_token"`
	DriverTokenExpiresAt string `json:"driver_token_expires_at"`
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

	// SHIP-107 at the wire: the link the provider forwards, and when it stops working. What the
	// token *contains* is token_test.go's; what is checked here is that this endpoint returns one
	// at all, in the shape contracts/paths/delivery.yaml publishes.
	if strings.Count(body.DriverToken, ".") != 2 {
		t.Errorf("driver_token = %q, want a signed token", body.DriverToken)
	}
	if !strings.HasSuffix(body.DriverTokenExpiresAt, "Z") {
		t.Errorf("driver_token_expires_at = %q, want UTC", body.DriverTokenExpiresAt)
	}
	if body.DriverTokenExpiresAt <= body.AssignedAt {
		t.Errorf("the link expires at %q, which is not after the assignment at %q",
			body.DriverTokenExpiresAt, body.AssignedAt)
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

	// The 200 carries a link too. A provider whose phone lost the first response has no other
	// way to obtain one, and an absorbed repeat that answered without it would leave them with
	// a driver they cannot reach (SHIP-107).
	if decode[assignmentBody](t, second).DriverToken == "" {
		t.Error("the absorbed repeat answered with no driver_token")
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

// The wire contract of SHIP-111.
//
// **These run without the idempotency middleware in front of them**, which is deliberate and is the
// case worth testing: the middleware belongs to cmd/api, it replays from Redis, and every retry it
// absorbs is one this handler never sees. What is left when it cannot absorb one — the entry expired,
// the cache was flushed, two instances — is the handler answering from the record, which is what the
// 200 below is.

// recordAs sends a milestone recording on behalf of an authenticated caller, under a named key.
//
// The key is an argument rather than generated, because the tests that matter are the ones where two
// requests share one: passing it explicitly makes a shared key visible in the test rather than
// implied by a helper.
func recordAs(t *testing.T, h http.Handler, caller uuid.UUID, jobID uuid.UUID, key, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, milestonesPath(jobID), strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
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

// milestonesPath is the route as cmd/api serves it.
func milestonesPath(jobID uuid.UUID) string { return "/v1/jobs/" + jobID.String() + "/milestones" }

type milestoneBody struct {
	ID         string `json:"id"`
	JobID      string `json:"job_id"`
	Milestone  string `json:"milestone"`
	RecordedBy string `json:"recorded_by"`
	Reason     string `json:"reason"`
	RecordedAt string `json:"recorded_at"`
	AcceptedAt string `json:"accepted_at"`
}

// TestTheMilestoneEndpointAnswersWithWhatItRecorded is SHIP-111 at the wire.
func TestTheMilestoneEndpointAnswersWithWhatItRecorded(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-ms-c@example.com", "+61400000700", "customer")
	provider := newAccount(t, pool, "http-ms-p@example.com", "+61400000701", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	rec := recordAs(t, router, provider, jobID, theKey,
		`{"milestone": "en_route_to_pickup", "recorded_at": "2026-08-12T02:00:00Z", "reason": "on the road"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	body := decode[milestoneBody](t, rec)
	switch {
	case body.ID == "":
		t.Error("the response carries no milestone id")
	case body.JobID != jobID.String():
		t.Errorf("job_id = %q, want %s", body.JobID, jobID)
	case body.Milestone != "en_route_to_pickup":
		t.Errorf("milestone = %q, want the lower snake case wire form (Docs/10 §4.7)", body.Milestone)
	case body.RecordedBy != "provider":
		t.Errorf("recorded_by = %q, want provider", body.RecordedBy)
	case body.Reason != "on the road":
		t.Errorf("reason = %q", body.Reason)
	case body.RecordedAt != "2026-08-12T02:00:00.000Z":
		t.Errorf("recorded_at = %q, want the time the client sent, uncorrected", body.RecordedAt)
	case body.AcceptedAt == "" || body.AcceptedAt == body.RecordedAt:
		t.Errorf("accepted_at = %q; it is the platform's clock and must not be the actor's", body.AcceptedAt)
	}

	// The job's status is not echoed, for the reason the assignment response gives — and here
	// there is a second: a milestone may deliberately move nothing at all.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	if _, present := raw["status"]; present {
		t.Error("the response carries a job status, which is the jobs domain's to serialise")
	}
}

// TestARetriedMilestoneAnswers200WithTheOriginal is the retry path at the wire.
//
// The same key twice, both reaching the handler. The second must answer with the first milestone and
// must not have recorded another, which is the whole of the ticket seen from a client.
func TestARetriedMilestoneAnswers200WithTheOriginal(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-retry-c@example.com", "+61400000702", "customer")
	provider := newAccount(t, pool, "http-retry-p@example.com", "+61400000703", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	body := `{"milestone": "en_route_to_pickup"}`

	first := recordAs(t, router, provider, jobID, theKey, body)
	if first.Code != http.StatusCreated {
		t.Fatalf("the first call = %d, want 201 (%s)", first.Code, first.Body)
	}

	second := recordAs(t, router, provider, jobID, theKey, body)
	if second.Code != http.StatusOK {
		t.Fatalf("the retry = %d, want 200 (%s)", second.Code, second.Body)
	}
	if decode[milestoneBody](t, second).ID != decode[milestoneBody](t, first).ID {
		t.Error("the retry answered with a different milestone")
	}
	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows after one action retried once, want 1", n)
	}
}

// TestALateMilestoneIsAbsorbedAtTheWire is SHIP-112 seen from a client.
//
// The 409 this used to get is the whole of what changed for the app: the driver's queued record now
// lands, so it can stop being pending on a phone. What did *not* change is the shape — the same
// `201` and the same body as any other recording, with no field saying the job did not move, because
// this response has never carried the job's status and Docs/02 §3.1 puts that reconciliation on the
// job resource.
func TestALateMilestoneIsAbsorbedAtTheWire(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-late-c@example.com", "+61400000707", "customer")
	provider := newAccount(t, pool, "http-late-p@example.com", "+61400000708", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit)

	rec := recordAs(t, router, provider, jobID, theKey,
		`{"milestone": "picked_up", "recorded_at": "2026-08-12T02:00:00Z"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	body := decode[milestoneBody](t, rec)
	if body.Milestone != "picked_up" {
		t.Errorf("milestone = %q", body.Milestone)
	}
	if body.RecordedAt != "2026-08-12T02:00:00.000Z" {
		t.Errorf("recorded_at = %q, want the actor's own time uncorrected — it is the only "+
			"statement of when a late milestone actually happened", body.RecordedAt)
	}

	// The same body as any other recording. An `absorbed` field would have to be answered from a
	// stored fact on the retry below and a computed one here, which is two answers to one
	// question — see delivery.Outcome.
	var raw map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("the response is not JSON: %v", err)
	}
	for _, field := range []string{"status", "absorbed"} {
		if _, present := raw[field]; present {
			t.Errorf("the response carries %q; the milestone shape says nothing about the job's state", field)
		}
	}

	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows, want 1 — the late record must survive the request", n)
	}
	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusInTransit) {
		t.Errorf("the job is %q, want In transit — the late milestone moved it backwards", got)
	}
	if n := historyCount(t, pool, jobID, jobs.StatusPickedUp); n != 1 {
		t.Errorf("%d transitions into Picked up, want the 1 the job really made", n)
	}

	// And it is still once per key, which absorption must not have opened a second path around.
	retry := recordAs(t, router, provider, jobID, theKey,
		`{"milestone": "picked_up", "recorded_at": "2026-08-12T02:00:00Z"}`)
	if retry.Code != http.StatusOK {
		t.Fatalf("the retry = %d, want 200 (%s)", retry.Code, retry.Body)
	}
	if decode[milestoneBody](t, retry).ID != body.ID {
		t.Error("the retry answered with a different milestone")
	}
	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestone rows after the retry, want 1", n)
	}
}

// TestTheMilestoneWireRefusals covers every failure a client can produce, and the code each carries.
func TestTheMilestoneWireRefusals(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-msx-c@example.com", "+61400000704", "customer")
	provider := newAccount(t, pool, "http-msx-p@example.com", "+61400000705", "provider")
	stranger := newAccount(t, pool, "http-msx-x@example.com", "+61400000706", "provider")

	awarded := awardedJob(t, pool, customer, provider)
	inTransit := awardedJob(t, pool, customer, provider)
	moveJob(t, pool, inTransit, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit)

	// One key that has already recorded something, so the reuse refusal has something to collide
	// with. It is spent on `awarded`, which the cases below then leave at En route to pickup.
	if rec := recordAs(t, router, provider, awarded, "spent-"+theKey,
		`{"milestone": "en_route_to_pickup"}`); rec.Code != http.StatusCreated {
		t.Fatalf("setting up the spent key: %d (%s)", rec.Code, rec.Body)
	}

	for _, tc := range []struct {
		name   string
		caller uuid.UUID
		job    uuid.UUID
		key    string
		body   string
		status int
		code   string
		field  string
	}{
		{
			name:   "another provider's job is not found",
			caller: stranger, job: awarded, key: theKey + "-stranger",
			body:   `{"milestone": "picked_up"}`,
			status: http.StatusNotFound, code: "not_found",
		},
		{
			name:   "the job's own customer is refused the same way",
			caller: customer, job: awarded, key: theKey + "-customer",
			body:   `{"milestone": "picked_up"}`,
			status: http.StatusNotFound, code: "not_found",
		},
		{
			name:   "a job that does not exist is the same answer",
			caller: provider, job: uuid.Must(uuid.NewV7()), key: theKey + "-nojob",
			body:   `{"milestone": "picked_up"}`,
			status: http.StatusNotFound, code: "not_found",
		},
		{
			name:   "no idempotency key at all",
			caller: provider, job: awarded, key: "",
			body:   `{"milestone": "picked_up"}`,
			status: http.StatusBadRequest, code: "idempotency_key_required",
		},
		{
			name:   "a key that recorded something else",
			caller: provider, job: awarded, key: "spent-" + theKey,
			body:   `{"milestone": "picked_up"}`,
			status: http.StatusConflict, code: "idempotency_key_reused",
		},
		{
			// The opposite direction from the case this replaced. `awarded` has been left at
			// En route to pickup by the spent key above, so `in_transit` is a claim about
			// something that has not happened yet. **A milestone the job has moved past is no
			// longer a refusal at all** — it answers 201, and TestALateMilestoneIsAbsorbedAtTheWire
			// is where that case went (SHIP-112).
			name:   "a milestone the delivery has not reached",
			caller: provider, job: awarded, key: theKey + "-early",
			body:   `{"milestone": "in_transit"}`,
			status: http.StatusConflict, code: "delivery_milestone_not_permitted",
		},
		{
			name:   "delivered, while nothing can prove it",
			caller: provider, job: inTransit, key: theKey + "-delivered",
			body:   `{"milestone": "delivered"}`,
			status: http.StatusConflict, code: "delivery_proof_required",
		},
		{
			name:   "the stored form is not the wire form",
			caller: provider, job: awarded, key: theKey + "-stored",
			body:   `{"milestone": "Picked up"}`,
			status: 422, code: "validation_failed", field: "milestone",
		},
		{
			name:   "a milestone nobody has heard of",
			caller: provider, job: awarded, key: theKey + "-unknown",
			body:   `{"milestone": "unloaded"}`,
			status: 422, code: "validation_failed", field: "milestone",
		},
		{
			name:   "no milestone at all",
			caller: provider, job: awarded, key: theKey + "-none",
			body:   `{}`,
			status: 422, code: "validation_failed", field: "milestone",
		},
		{
			name:   "driver assigned has an endpoint of its own",
			caller: provider, job: awarded, key: theKey + "-assigned",
			body:   `{"milestone": "driver_assigned"}`,
			status: 422, code: "validation_failed", field: "milestone",
		},
		{
			name:   "a recorded_at that is not a timestamp",
			caller: provider, job: awarded, key: theKey + "-time",
			body:   `{"milestone": "picked_up", "recorded_at": "yesterday"}`,
			status: 422, code: "validation_failed", field: "recorded_at",
		},
		{
			name:   "an unknown field is a client typo rather than something to ignore",
			caller: provider, job: awarded, key: theKey + "-typo",
			body:   `{"milestone": "picked_up", "photo": "x"}`,
			status: http.StatusBadRequest, code: "bad_request",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := recordAs(t, router, tc.caller, tc.job, tc.key, tc.body)
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

	// Nothing above should have written a row beyond the one that set the spent key up, and the
	// two jobs should be where they were.
	if n := milestoneCount(t, pool, awarded); n != 1 {
		t.Errorf("%d milestones on the awarded job, want the 1 that set up the spent key", n)
	}
	if n := milestoneCount(t, pool, inTransit); n != 0 {
		t.Errorf("%d milestones survived on the in-transit job, want 0", n)
	}
	if got := jobStatus(t, pool, inTransit); got != string(jobs.StatusInTransit) {
		t.Errorf("the in-transit job is %q; a refused recording moved it", got)
	}
}

// TestTheMilestoneEndpointAnswers503WithNoDatabase.
func TestTheMilestoneEndpointAnswers503WithNoDatabase(t *testing.T) {
	router := newTestRouter(t, nil)

	rec := recordAs(t, router, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), theKey,
		`{"milestone": "picked_up"}`)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503 (%s)", rec.Code, rec.Body)
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
