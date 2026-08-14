package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-120a against a real PostgreSQL and a real router.
//
// The fixtures and the ports come from assignment_test.go and driverauth_test.go; nothing here
// duplicates them.
//
// # What only this file can show
//
// cmd/api's tests prove the route is declared with the driver's auth class and that a link for one
// job is refused on another, through the real middleware chain. What they cannot reach is the
// database: testDeps carries no pool, so every request there stops at 503. So the two halves of the
// *Done when* are split — the guard's half is proved in package main, and **what the milestone
// actually says about who recorded it** is proved here, by reading the rows.
//
// # The attribution is the part with no other witness
//
// A driver's milestone writes two rows in two tables, and both have to say a driver did it: 000601's
// `actor_type` / `actor_id` on the milestone, and 000401's on the transition it causes. Neither is
// visible in a response body — [milestoneResponse] carries `recorded_by` and not the identifier —
// and a `provider` written into either would be a false attribution that is perfectly well formed.
// Every test below that records something checks the columns.

// driverMilestoneRouter is the SHIP-120a route behind the real guard, over a real database.
//
// A mux of its own rather than cmd/api's, for the reason [newDriverRouter] gives: this package
// cannot import package main, and what is being exercised here is the handler and the domain
// against real rows. The idempotency middleware is deliberately **not** in the chain — its
// contribution is a cached replay, and the guarantee this ticket rests on is the unique index
// underneath it (000602). Leaving it out is what lets a retry reach the handler at all.
func driverMilestoneRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newTestService(), pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("POST /v1/driver/jobs/{id}/milestones",
		RequireDriverToken(testDriverVerifier(t, clock.NewFixed(testDriverIssuedAt)))(
			handler.RecordDriverMilestone()))
	return mux
}

// postMilestoneAs presents a credential to the driver's milestone route on whichever job the caller
// names.
//
// The job is an argument rather than being read out of the token, which is the whole point of the
// negative case: a test that built the path from the credential could not tell a scoped route from
// an unscoped one.
func postMilestoneAs(h http.Handler, jobID uuid.UUID, header, key, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost,
		"/v1/driver/jobs/"+jobID.String()+"/milestones", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if header != "" {
		req.Header.Set("Authorization", header) // spelling:ok — HTTP header name, RFC 9110
	}
	if key != "" {
		req.Header.Set(httpx.HeaderIdempotencyKey, key)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// driverOnJob puts a driver on an awarded job and returns the grant their link produces.
//
// The grant is built from the assignment the service actually wrote rather than from a token minted
// beside it, because the two agreeing is what [Service.AssignmentFor] checks and a fixture that
// invented an assignment identifier would make every test below pass against nothing.
func driverOnJob(t *testing.T, pool *pgxpool.Pool, provider, jobID uuid.UUID) (Assignment, DriverGrant, DriverToken) {
	t.Helper()

	assignment, token, _, err := assignGranting(t, pool, newTestService(), provider, jobID, Nomination{
		DriverName:   "Sam Patel",
		DriverMobile: "0412 345 678",
	})
	if err != nil {
		t.Fatalf("assigning a driver: %v", err)
	}
	return assignment, DriverGrant{
		JobID:        jobID,
		AssignmentID: assignment.ID,
		ExpiresAt:    token.ExpiresAt,
	}, token
}

// recordAsDriver runs one driver recording in its own transaction, which is what the handler does.
func recordAsDriver(
	t *testing.T,
	pool *pgxpool.Pool,
	svc *Service,
	grant DriverGrant,
	rec Recording,
) (Record, Outcome, error) {
	t.Helper()

	var (
		record  Record
		outcome Outcome
	)
	err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		record, outcome, err = svc.RecordDriverMilestone(ctx, r, grant, rec)
		return err
	})
	return record, outcome, err
}

// milestoneActor reads what the row says about who recorded it — which is the fact no response body
// carries.
func milestoneActor(t *testing.T, pool *pgxpool.Pool, milestoneID uuid.UUID) (string, uuid.UUID) {
	t.Helper()

	var (
		actorType string
		actorID   uuid.UUID
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT actor_type, actor_id FROM milestones WHERE id = $1`, milestoneID).
		Scan(&actorType, &actorID); err != nil {
		t.Fatalf("reading the actor on milestone %s: %v", milestoneID, err)
	}
	return actorType, actorID
}

// transitionActor reads who `job_status_history` says moved the job into a status.
func transitionActor(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID, to jobs.Status) (string, uuid.UUID) {
	t.Helper()

	var (
		actorType string
		actorID   uuid.UUID
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT actor_type, actor_id FROM job_status_history WHERE job_id = $1 AND to_status = $2`,
		jobID, string(to)).Scan(&actorType, &actorID); err != nil {
		t.Fatalf("reading who moved %s to %s: %v", jobID, to, err)
	}
	return actorType, actorID
}

// TestADriverRecordsAMilestoneAttributedToTheirAssignment is SHIP-120a's first clause: a driver
// holding a job-scoped token records a milestone on that job.
//
// Both rows are read, because both can be wrong independently. The milestone could say `driver` and
// the transition `provider`, which would compile, pass a response-shape assertion, and leave a
// delivery whose timeline and whose status history disagree about who was driving.
func TestADriverRecordsAMilestoneAttributedToTheirAssignment(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvms-c@example.com", "+61400000700", "customer")
	provider := newAccount(t, pool, "drvms-p@example.com", "+61400000701", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, grant, _ := driverOnJob(t, pool, provider, jobID)

	record, outcome, err := recordAsDriver(t, pool, newTestService(), grant, enRoute(theKey))
	if err != nil {
		t.Fatalf("RecordDriverMilestone() = %v", err)
	}

	switch {
	case outcome != OutcomeRecorded:
		t.Errorf("outcome = %q, want recorded", outcome)
	case record.JobID != jobID:
		t.Errorf("the milestone is on %s, want the job the link grants, %s", record.JobID, jobID)
	case record.Actor != ActorDriver:
		t.Errorf("actor = %q, want driver — a driver has no account, so the row is their identity", record.Actor)
	case record.ActorID != assignment.ID:
		t.Errorf("actor_id = %s, want the driver_assignments row %s", record.ActorID, assignment.ID)
	}

	if actorType, actorID := milestoneActor(t, pool, record.ID); actorType != "driver" || actorID != assignment.ID {
		t.Errorf("milestones says %s/%s recorded it, want driver/%s", actorType, actorID, assignment.ID)
	}

	if got := jobStatus(t, pool, jobID); got != string(jobs.StatusEnRouteToPickup) {
		t.Fatalf("the job is %q, want En route to pickup", got)
	}
	actorType, actorID := transitionActor(t, pool, jobID, jobs.StatusEnRouteToPickup)
	if actorType != "driver" || actorID != assignment.ID {
		t.Errorf("job_status_history says %s/%s moved the job, want driver/%s — a transition a "+
			"driver caused must not be attributed to their provider (000401)",
			actorType, actorID, assignment.ID)
	}
}

// TestADriverGrantRecordsOnNoOtherJob is the *Done when*'s "and on no other" inside the domain.
//
// The service takes a grant and no job identifier, so the only way to aim it at somebody else's
// delivery is to forge the grant itself — which is what this does. The refusal comes from the row
// rather than from the token: the assignment named is not the live one on the job named, so the
// link opens nothing, and that answers what a job that does not exist answers.
//
// The wire half of this — a genuine link presented on another job's path — is in cmd/api, where the
// real guard is attached to the real route.
func TestADriverGrantRecordsOnNoOtherJob(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvms-other-c@example.com", "+61400000702", "customer")
	provider := newAccount(t, pool, "drvms-other-p@example.com", "+61400000703", "provider")

	granted := awardedJob(t, pool, customer, provider)
	other := awardedJob(t, pool, customer, provider)

	assignment, _, _ := driverOnJob(t, pool, provider, granted)
	_, _, _ = driverOnJob(t, pool, provider, other)

	// The grant a widened token would produce: this driver's assignment, somebody else's job.
	forged := DriverGrant{JobID: other, AssignmentID: assignment.ID}

	_, _, err := recordAsDriver(t, pool, newTestService(), forged, enRoute(theKey))
	if !errors.Is(err, ErrDriverLinkSuperseded) {
		t.Fatalf("RecordDriverMilestone() on another job = %v, want ErrDriverLinkSuperseded", err)
	}
	if n := milestoneCount(t, pool, other); n != 0 {
		t.Errorf("%d milestones on the other job, want 0 — a refused recording must write nothing", n)
	}
}

// TestALinkWhoseAssignmentHasEndedRecordsNothing is SHIP-109's mechanism seen from the write side.
//
// A driver token is stateless and cannot be recalled, so it keeps verifying after the assignment
// behind it has ended. The row is what knows, and it has to be read on a write for the same reason
// it is read on the read — a stood-down driver who could still record milestones would be worse than
// one who could still see the job.
func TestALinkWhoseAssignmentHasEndedRecordsNothing(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvms-gone-c@example.com", "+61400000704", "customer")
	provider := newAccount(t, pool, "drvms-gone-p@example.com", "+61400000705", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	if _, err := pool.Exec(t.Context(),
		`UPDATE driver_assignments SET unassigned_at = now() WHERE job_id = $1`, jobID); err != nil {
		t.Fatalf("standing the driver down: %v", err)
	}

	_, _, err := recordAsDriver(t, pool, newTestService(), grant, enRoute(theKey))
	if !errors.Is(err, ErrDriverLinkSuperseded) {
		t.Fatalf("RecordDriverMilestone() after a stand-down = %v, want ErrDriverLinkSuperseded", err)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestones, want 0", n)
	}
}

// TestADriverRetryRecordsOneMilestone is the invariant the driver's route exists to be safe under.
//
// A mobile browser in a yard is the retry case, and the middleware's replay is not what makes it
// correct — the entry expires, the handler runs again, and `uq_milestones_idempotency (job_id,
// idempotency_key)` refuses the second row. This runs the service twice with no cache in front of
// it, which is exactly the path a phone reconnecting after a day takes.
func TestADriverRetryRecordsOneMilestone(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvms-retry-c@example.com", "+61400000706", "customer")
	provider := newAccount(t, pool, "drvms-retry-p@example.com", "+61400000707", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, grant, _ := driverOnJob(t, pool, provider, jobID)
	svc := newTestService()

	first, _, err := recordAsDriver(t, pool, svc, grant, enRoute(theKey))
	if err != nil {
		t.Fatalf("the first recording: %v", err)
	}

	second, outcome, err := recordAsDriver(t, pool, svc, grant, enRoute(theKey))
	if err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if outcome != OutcomeAlreadyRecorded {
		t.Errorf("outcome = %q, want already recorded", outcome)
	}
	if second.ID != first.ID {
		t.Errorf("the retry answered with %s, want the milestone the first attempt recorded, %s",
			second.ID, first.ID)
	}
	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestones, want 1 — the key must record once whoever retries", n)
	}
}

// TestADriverAndTheirProviderShareOneKeyNamespace records the consequence of the scope decision
// rather than leaving it to be discovered.
//
// `uq_milestones_idempotency` is `(job_id, idempotency_key)` and carries no actor, which 000602
// argued in advance and named this exact case. So a key the provider has used answers the driver
// with the row it recorded. That is correct — they are the two actors on one delivery, and a
// milestone is not private between them — and it is asserted so that a future change to the index
// has to change this test and say why.
func TestADriverAndTheirProviderShareOneKeyNamespace(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvms-share-c@example.com", "+61400000708", "customer")
	provider := newAccount(t, pool, "drvms-share-p@example.com", "+61400000709", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, grant, _ := driverOnJob(t, pool, provider, jobID)
	svc := newTestService()

	byProvider, _, err := recordMilestone(t, pool, svc, provider, jobID, enRoute(theKey))
	if err != nil {
		t.Fatalf("the provider's recording: %v", err)
	}

	byDriver, outcome, err := recordAsDriver(t, pool, svc, grant, enRoute(theKey))
	if err != nil {
		t.Fatalf("the driver's recording under the same key: %v", err)
	}
	if outcome != OutcomeAlreadyRecorded || byDriver.ID != byProvider.ID {
		t.Errorf("the driver's request under the provider's key answered %q/%s, want already "+
			"recorded and the provider's row %s", outcome, byDriver.ID, byProvider.ID)
	}
	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestones, want 1", n)
	}
}

// TestADriverCannotRecordDeliveredWithNothingToShowForIt is CLAUDE.md's invariant on the second
// entry point.
//
// It is checked in [Service.record], which both routes go through, so this could only fail if the
// driver's path were ever given a shortcut around it. That is precisely why it is asserted from
// here: the invariant belongs to the platform rather than to one handler.
func TestADriverCannotRecordDeliveredWithNothingToShowForIt(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvms-del-c@example.com", "+61400000710", "customer")
	provider := newAccount(t, pool, "drvms-del-p@example.com", "+61400000711", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	_, _, err := recordAsDriver(t, pool, newTestService(), grant,
		Recording{Milestone: MilestoneDelivered, Key: theKey})
	if !errors.Is(err, ErrProofRequired) {
		t.Fatalf("a driver recorded Delivered with no evidence: %v, want ErrProofRequired", err)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestones, want 0 — nothing is written when the evidence rule refuses", n)
	}
}

// TestADriverMilestoneWithNoKeyIsRefused holds the check the domain makes rather than the
// middleware.
//
// A milestone written with a NULL key falls outside the partial index entirely, so the absence of
// the header would remove the guarantee silently rather than loudly. The driver's route is served
// under the middleware, which refuses a keyless request before a handler runs; this is the second
// layer, and it is the one that survives somebody serving the route another way.
func TestADriverMilestoneWithNoKeyIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvms-key-c@example.com", "+61400000712", "customer")
	provider := newAccount(t, pool, "drvms-key-p@example.com", "+61400000713", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	_, _, err := recordAsDriver(t, pool, newTestService(), grant, enRoute(""))
	if !errors.Is(err, ErrNoIdempotencyKey) {
		t.Fatalf("RecordDriverMilestone() with no key = %v, want ErrNoIdempotencyKey", err)
	}
}

// TestRecordDriverMilestoneRefusesAPool is the same guard [TestRecordMilestoneRefusesAPool] holds on
// the provider's path: a milestone and its transition are one act in two tables.
func TestRecordDriverMilestoneRefusesAPool(t *testing.T) {
	pool := pgtest.DB(t)

	_, _, err := newTestService().RecordDriverMilestone(t.Context(), pool,
		DriverGrant{JobID: uuid.New(), AssignmentID: uuid.New()}, enRoute(theKey))
	if !errors.Is(err, ErrNotInTransaction) {
		t.Fatalf("RecordDriverMilestone() on a pool = %v, want ErrNotInTransaction", err)
	}
}

// TestTheDriverRouteRecordsAMilestoneOverHTTP is the whole ticket at the wire, against real rows:
// the guard verifies the link, the handler takes the grant, and the delivery moves.
func TestTheDriverRouteRecordsAMilestoneOverHTTP(t *testing.T) {
	pool := pgtest.DB(t)
	router := driverMilestoneRouter(t, pool)

	customer := newAccount(t, pool, "drvhttp-c@example.com", "+61400000714", "customer")
	provider := newAccount(t, pool, "drvhttp-p@example.com", "+61400000715", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, _, token := driverOnJob(t, pool, provider, jobID)

	rec := postMilestoneAs(router, jobID, bearer(token.Value), theKey,
		`{"milestone":"en_route_to_pickup","recorded_at":"2026-08-12T06:40:11Z"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201 (%s)", rec.Code, rec.Body)
	}

	body := decode[struct {
		ID         string `json:"id"`
		JobID      string `json:"job_id"`
		Milestone  string `json:"milestone"`
		RecordedBy string `json:"recorded_by"`
		RecordedAt string `json:"recorded_at"`
		AcceptedAt string `json:"accepted_at"`
	}](t, rec)

	switch {
	case body.JobID != jobID.String():
		t.Errorf("job_id = %q, want %s", body.JobID, jobID)
	case body.Milestone != "en_route_to_pickup":
		t.Errorf("milestone = %q", body.Milestone)
	case body.RecordedBy != "driver":
		t.Errorf("recorded_by = %q, want driver — this is where a client tells "+
			"\"you recorded this\" from \"your driver did\"", body.RecordedBy)
	case body.RecordedAt == body.AcceptedAt:
		t.Errorf("the two clocks are the same value %q; the actor recorded this well before the "+
			"platform heard about it (Docs/02 §3.1)", body.RecordedAt)
	}

	id, err := uuid.Parse(body.ID)
	if err != nil {
		t.Fatalf("the response carries no milestone id: %v", err)
	}
	if actorType, actorID := milestoneActor(t, pool, id); actorType != "driver" || actorID != assignment.ID {
		t.Errorf("milestones says %s/%s, want driver/%s", actorType, actorID, assignment.ID)
	}
}

// TestTheDriverRouteRecordsOnNoOtherJobOverHTTP is the *Done when*'s negative case with nothing
// stubbed: two real jobs, two real assignments, and one genuine link.
//
// **This is the test wave 7's surviving mutation exists to make necessary.** A surface that decided
// which delivery to act on from its own credential rather than from what was asked for answered 200
// while every test passed, so the assertion has to be about the *answer to a request naming another
// job*, not about what the code reads.
func TestTheDriverRouteRecordsOnNoOtherJobOverHTTP(t *testing.T) {
	pool := pgtest.DB(t)
	router := driverMilestoneRouter(t, pool)

	customer := newAccount(t, pool, "drvhttp-x-c@example.com", "+61400000716", "customer")
	provider := newAccount(t, pool, "drvhttp-x-p@example.com", "+61400000717", "provider")

	granted := awardedJob(t, pool, customer, provider)
	other := awardedJob(t, pool, customer, provider)

	_, _, token := driverOnJob(t, pool, provider, granted)
	_, _, _ = driverOnJob(t, pool, provider, other)

	rec := postMilestoneAs(router, other, bearer(token.Value), theKey, `{"milestone":"en_route_to_pickup"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("a link for %s recorded on %s with status %d, want 404 (%s)",
			granted, other, rec.Code, rec.Body)
	}
	if code := errorCodeOf(t, rec); code != string(httpx.CodeNotFound) {
		t.Errorf("code = %q, want %q", code, httpx.CodeNotFound)
	}

	if n := milestoneCount(t, pool, other); n != 0 {
		t.Errorf("%d milestones landed on the other job, want 0", n)
	}
	if got := jobStatus(t, pool, other); got != string(jobs.StatusDriverAssigned) {
		t.Errorf("the other job is %q, want Driver assigned — nothing may have moved it", got)
	}
}

// TestADriverMayRecordAnExceptionAndMayNotAttachAPhotograph is the evidence line this ticket draws,
// and it is a line rather than a gap.
//
// The exception needs nothing but a string and Docs/01 §4.4 makes it part of the same feature, so a
// driver who cannot photograph a pickup can still record the pickup. A photograph needs an upload
// URL and **there is no operation a driver can call to obtain one** — SHIP-122 builds the pair — so
// a key presented here came from somewhere a driver should not have been, and is refused as a field
// error rather than verified against the store.
func TestADriverMayRecordAnExceptionAndMayNotAttachAPhotograph(t *testing.T) {
	pool := pgtest.DB(t)
	router := driverMilestoneRouter(t, pool)

	customer := newAccount(t, pool, "drvproof-c@example.com", "+61400000718", "customer")
	provider := newAccount(t, pool, "drvproof-p@example.com", "+61400000719", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, _, token := driverOnJob(t, pool, provider, jobID)

	refused := postMilestoneAs(router, jobID, bearer(token.Value), theKey,
		`{"milestone":"en_route_to_pickup","proof":{"object_key":"proof/`+jobID.String()+
			`/019bd7a1-2c44-7f10-9a2c-3d4e5f607182"}}`)
	if refused.Code != http.StatusUnprocessableEntity {
		t.Fatalf("a driver attached a photograph with status %d, want 422 (%s)", refused.Code, refused.Body)
	}
	if field := firstFieldOf(t, refused); field != "proof.object_key" {
		t.Errorf("the refusal names %q, want proof.object_key", field)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestones, want 0 — the refused request must write nothing", n)
	}

	accepted := postMilestoneAs(router, jobID, bearer(token.Value), theKey+"-exception",
		`{"milestone":"en_route_to_pickup","proof":{"exception_reason":"camera_unavailable"}}`)
	if accepted.Code != http.StatusCreated {
		t.Fatalf("a driver recorded a reasoned exception with status %d, want 201 (%s)",
			accepted.Code, accepted.Body)
	}
}

// firstFieldOf reads the first `error.details[].field` out of a validation refusal.
func firstFieldOf(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var body struct {
		Error struct {
			Details []struct {
				Field string `json:"field"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal is not JSON: %v (%s)", err, rec.Body)
	}
	if len(body.Error.Details) == 0 {
		t.Fatalf("the refusal carries no field errors: %s", rec.Body)
	}
	return body.Error.Details[0].Field
}

// TestAnUnattributableRecorderRecordsNothing holds the refusal cmd/api's adapter would otherwise
// meet as a constraint name.
//
// No route can produce this — both entry points build the [Recorder] themselves — which is exactly
// what makes it worth a test: the check is unreachable from outside, so nothing else would notice
// if it were deleted, and what it protects against is a third entry point being added without
// deciding whose row `actor_id` names.
func TestAnUnattributableRecorderRecordsNothing(t *testing.T) {
	pool := pgtest.DB(t)
	customer := newAccount(t, pool, "drvms-actor-c@example.com", "+61400000720", "customer")
	provider := newAccount(t, pool, "drvms-actor-p@example.com", "+61400000721", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := newTestService()
	for name, by := range map[string]Recorder{
		"nothing at all":      {},
		"the platform itself": {Type: ActorSystem},
		"an administrator":    {Type: ActorAdmin, ID: uuid.New()},
		"a provider with no account": {
			Type: ActorProvider, ID: uuid.Nil,
		},
	} {
		t.Run(name, func(t *testing.T) {
			err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
				_, _, err := svc.record(ctx, r, by, jobID, enRoute(theKey))
				return err
			})
			if !errors.Is(err, ErrUnknownRecorder) {
				t.Fatalf("record() for %v = %v, want ErrUnknownRecorder", by, err)
			}
		})
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestones, want 0", n)
	}
}
