package delivery

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/pagination"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-115a against a real PostgreSQL, and through the real handlers.
//
// The fixtures come from assignment_test.go and recording_test.go; nothing here duplicates them.
//
// # What these are for, over and above the service tests
//
// The *Done when* is about two parties and a stranger, and "a stranger gets exactly what a missing
// job gets" is a claim about a **response**, not about a sentinel. So every access check below is
// made twice: once against the service, where the error can be named, and once at the wire, where a
// 404 body from a stranger is compared with a 404 body from a job identifier nothing has ever used.
// A test that only checked the sentinel would pass on a handler that mapped it to 403.

// readShelfRouter serves the two SHIP-115a routes with a subject on the context, over a real
// database.
//
// A mux of its own rather than cmd/api's, for the reason [newDriverRouter] gives: this package
// cannot import package main. httpx.RequireSubject is not in the chain — the subject is injected
// directly — because what is being exercised is the two-party check inside the handler, and the auth
// class is cmd/api's to attach.
func readShelfRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newTestService(), pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/jobs/{id}/delivery/detail", handler.DeliveryDetail())
	mux.Handle("GET /v1/jobs/{id}/delivery/milestones", handler.MilestonesOnJob())
	return mux
}

// readAs makes a request on the shelf as one account.
func readAs(h http.Handler, path string, readerID uuid.UUID) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req = req.WithContext(authctx.WithSubject(req.Context(),
		authctx.Subject{UserID: readerID.String()}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func detailPath(jobID uuid.UUID) string { return "/v1/jobs/" + jobID.String() + "/delivery/detail" }

func shelfMilestonesPath(jobID uuid.UUID) string {
	return "/v1/jobs/" + jobID.String() + "/delivery/milestones"
}

type deliveryDetailBody struct {
	JobID          string `json:"job_id"`
	DriverAssigned bool   `json:"driver_assigned"`
	AssignmentID   string `json:"assignment_id"`
	DriverName     string `json:"driver_name"`
	DriverMobile   string `json:"driver_mobile"`
	AssignedAt     string `json:"assigned_at"`
}

type milestonePageBody struct {
	Data []struct {
		ID         string `json:"id"`
		JobID      string `json:"job_id"`
		Milestone  string `json:"milestone"`
		RecordedBy string `json:"recorded_by"`
		RecordedAt string `json:"recorded_at"`
		AcceptedAt string `json:"accepted_at"`
	} `json:"data"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

// TestBothPartiesReadTheDriverAssignment is the first clause of SHIP-115a's *Done when*.
func TestBothPartiesReadTheDriverAssignment(t *testing.T) {
	pool := pgtest.DB(t)
	router := readShelfRouter(t, pool)

	customer := newAccount(t, pool, "shelf-c@example.com", "+61400000730", "customer")
	provider := newAccount(t, pool, "shelf-p@example.com", "+61400000731", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, _, _ := driverOnJob(t, pool, provider, jobID)

	for name, reader := range map[string]uuid.UUID{
		"the customer who owns the job":       customer,
		"the provider whose bid was accepted": provider,
	} {
		t.Run(name, func(t *testing.T) {
			rec := readAs(router, detailPath(jobID), reader)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
			}

			body := decode[deliveryDetailBody](t, rec)
			switch {
			case body.JobID != jobID.String():
				t.Errorf("job_id = %q, want %s", body.JobID, jobID)
			case !body.DriverAssigned:
				t.Error("driver_assigned is false on a job with a live driver")
			case body.AssignmentID != assignment.ID.String():
				t.Errorf("assignment_id = %q, want %s", body.AssignmentID, assignment.ID)
			case body.DriverName != "Sam Patel":
				t.Errorf("driver_name = %q", body.DriverName)
			}
		})
	}
}

// TestOnlyTheProviderIsGivenTheDriversNumber holds the one field that differs between the two
// parties.
//
// Held as a **closed set of keys** rather than by searching for the field name, which is SHIP-83's
// argument and the one wave 6 proved the hard way: a search for `driver_mobile` catches
// `driver_mobile` and misses `mobile` or `phone`. `Docs/01` §4 asks the platform to minimise how far
// a phone number travels, and a driver has no account and no way to consent.
func TestOnlyTheProviderIsGivenTheDriversNumber(t *testing.T) {
	pool := pgtest.DB(t)
	router := readShelfRouter(t, pool)

	customer := newAccount(t, pool, "shelf-mob-c@example.com", "+61400000732", "customer")
	provider := newAccount(t, pool, "shelf-mob-p@example.com", "+61400000733", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	driverOnJob(t, pool, provider, jobID)

	keysOf := func(reader uuid.UUID) map[string]bool {
		rec := readAs(router, detailPath(jobID), reader)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
		}
		var raw map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
			t.Fatalf("the response is not JSON: %v", err)
		}
		keys := map[string]bool{}
		for k := range raw {
			keys[k] = true
		}
		return keys
	}

	providerKeys := keysOf(provider)
	want := map[string]bool{
		"job_id": true, "driver_assigned": true, "assignment_id": true,
		"driver_name": true, "driver_mobile": true, "assigned_at": true,
	}
	for k := range providerKeys {
		if !want[k] {
			t.Errorf("the provider's view carries an unexpected field %q", k)
		}
	}
	if len(providerKeys) != len(want) {
		t.Errorf("the provider's view has %d fields, want %d: %v", len(providerKeys), len(want), providerKeys)
	}

	customerKeys := keysOf(customer)
	delete(want, "driver_mobile")
	for k := range customerKeys {
		if !want[k] {
			t.Errorf("the customer's view carries %q; a driver has no account and no way to consent "+
				"to their number reaching the person receiving the goods (Docs/01 §4)", k)
		}
	}
	if len(customerKeys) != len(want) {
		t.Errorf("the customer's view has %d fields, want %d: %v", len(customerKeys), len(want), customerKeys)
	}
}

// TestAStrangerGetsWhatAMissingJobGets is the clause the *Done when* names last and the one worth
// asserting at the wire.
//
// Three requests and one answer. The bodies are compared for **equality once the request id is
// removed**, because "exactly what a missing job gets" is a statement about what a prober can tell
// apart, and a difference in the message is a difference they can act on.
func TestAStrangerGetsWhatAMissingJobGets(t *testing.T) {
	pool := pgtest.DB(t)
	router := readShelfRouter(t, pool)

	customer := newAccount(t, pool, "shelf-x-c@example.com", "+61400000734", "customer")
	provider := newAccount(t, pool, "shelf-x-p@example.com", "+61400000735", "provider")
	stranger := newAccount(t, pool, "shelf-x-s@example.com", "+61400000736", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	driverOnJob(t, pool, provider, jobID)

	for _, path := range []func(uuid.UUID) string{detailPath, shelfMilestonesPath} {
		strangerBody := refusalBody(t, readAs(router, path(jobID), stranger))
		missingBody := refusalBody(t, readAs(router, path(uuid.New()), customer))

		if strangerBody != missingBody {
			t.Errorf("a stranger and a missing job answer differently:\n  stranger: %s\n  missing:  %s",
				strangerBody, missingBody)
		}
	}
}

// refusalBody is a 404's body with the request id removed, so two refusals can be compared.
func refusalBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", rec.Code, rec.Body)
	}

	var body struct {
		Error struct {
			Code      string `json:"code"`
			Message   string `json:"message"`
			RequestID string `json:"request_id"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("the refusal is not JSON: %v (%s)", err, rec.Body)
	}
	return body.Error.Code + " / " + body.Error.Message
}

// TestAJobWithNoDriverIsNotAMissingDelivery keeps the two states apart.
//
// A party to an awarded job that nobody has been put on is entitled to look, and "no driver yet" is
// a true and useful answer. Answering 404 would be indistinguishable from a stranger's refusal and
// would make the customer's tracking screen (SHIP-133) unable to tell the two apart.
func TestAJobWithNoDriverIsNotAMissingDelivery(t *testing.T) {
	pool := pgtest.DB(t)
	router := readShelfRouter(t, pool)

	customer := newAccount(t, pool, "shelf-nd-c@example.com", "+61400000737", "customer")
	provider := newAccount(t, pool, "shelf-nd-p@example.com", "+61400000738", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	rec := readAs(router, detailPath(jobID), customer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[deliveryDetailBody](t, rec)
	switch {
	case body.DriverAssigned:
		t.Error("driver_assigned is true on a job with no driver")
	case body.AssignmentID != "":
		t.Errorf("assignment_id = %q, want it absent", body.AssignmentID)
	case body.DriverName != "":
		t.Errorf("driver_name = %q, want it absent", body.DriverName)
	}
}

// TestTheShelfReadsTheLiveDriverAndNotAPastOne is why the read is `liveAssignment` rather than the
// most recent row.
func TestTheShelfReadsTheLiveDriverAndNotAPastOne(t *testing.T) {
	pool := pgtest.DB(t)
	router := readShelfRouter(t, pool)

	customer := newAccount(t, pool, "shelf-live-c@example.com", "+61400000739", "customer")
	provider := newAccount(t, pool, "shelf-live-p@example.com", "+61400000740", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	driverOnJob(t, pool, provider, jobID)

	if _, err := pool.Exec(t.Context(),
		`UPDATE driver_assignments SET unassigned_at = now() WHERE job_id = $1`, jobID); err != nil {
		t.Fatalf("standing the driver down: %v", err)
	}

	rec := readAs(router, detailPath(jobID), customer)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if body := decode[deliveryDetailBody](t, rec); body.DriverAssigned {
		t.Errorf("a stood-down driver is still reported as driving: %+v", body)
	}
}

// TestBothPartiesReadEveryRecordedMilestone is the second clause of the *Done when*, and it drives
// the case that has no other source.
//
// Three recordings, one of which moves nothing: a repeated `en_route_to_pickup` writes a second row
// and leaves the job where it is (Docs/02 §5). A timeline derived from the job's status shows two
// events; this shows three, which is the whole reason the operation exists.
func TestBothPartiesReadEveryRecordedMilestone(t *testing.T) {
	pool := pgtest.DB(t)
	router := readShelfRouter(t, pool)

	customer := newAccount(t, pool, "shelf-ms-c@example.com", "+61400000741", "customer")
	provider := newAccount(t, pool, "shelf-ms-p@example.com", "+61400000742", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	_, grant, _ := driverOnJob(t, pool, provider, jobID)

	svc := newTestService()
	base := testInstant

	if _, _, err := recordMilestone(t, pool, svc, provider, jobID, Recording{
		Milestone: MilestoneEnRouteToPickup, Key: "shelf-1", RecordedAt: base,
	}); err != nil {
		t.Fatalf("the first milestone: %v", err)
	}
	// A failed pickup attempt: the same milestone again, moving nothing.
	if _, _, err := recordAsDriver(t, pool, svc, grant, Recording{
		Milestone: MilestoneEnRouteToPickup, Key: "shelf-2", RecordedAt: base.Add(time.Hour),
	}); err != nil {
		t.Fatalf("the repeated milestone: %v", err)
	}
	if _, _, err := recordAsDriver(t, pool, svc, grant, Recording{
		Milestone: MilestonePickedUp, Key: "shelf-3", RecordedAt: base.Add(2 * time.Hour),
	}); err != nil {
		t.Fatalf("the pickup: %v", err)
	}

	if n := historyCount(t, pool, jobID, jobs.StatusEnRouteToPickup); n != 1 {
		t.Fatalf("%d transitions into En route to pickup, want 1 — the fixture is not exercising "+
			"a milestone that moved nothing", n)
	}

	for name, reader := range map[string]uuid.UUID{
		"the customer": customer,
		"the provider": provider,
	} {
		t.Run(name, func(t *testing.T) {
			rec := readAs(router, shelfMilestonesPath(jobID), reader)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
			}

			page := decode[milestonePageBody](t, rec)
			if len(page.Data) != 3 {
				t.Fatalf("%d milestones, want 3 — a recording that moved nothing has to appear "+
					"here or it appears nowhere: %+v", len(page.Data), page.Data)
			}
			if page.HasMore {
				t.Error("has_more is true on a three-row collection")
			}

			// Newest by the actor's clock first.
			if page.Data[0].Milestone != "picked_up" {
				t.Errorf("the first row is %q, want the most recently acted picked_up",
					page.Data[0].Milestone)
			}
			if page.Data[0].RecordedBy != "driver" || page.Data[2].RecordedBy != "provider" {
				t.Errorf("recorded_by is %q then %q; the list must say who recorded each",
					page.Data[0].RecordedBy, page.Data[2].RecordedBy)
			}
		})
	}
}

// TestTheMilestoneListPagesWithoutRepeatingOrSkipping is why the cursor carries the identifier as
// well as the clock.
//
// Every row shares one `actor_recorded_at`, which is exactly the batch an offline phone syncs: a
// cursor that carried only the timestamp would resume at the start of the group and repeat it, or
// past the group and drop it. The assertion is that a full walk in pages of one sees each row once.
func TestTheMilestoneListPagesWithoutRepeatingOrSkipping(t *testing.T) {
	pool := pgtest.DB(t)
	router := readShelfRouter(t, pool)

	customer := newAccount(t, pool, "shelf-pg-c@example.com", "+61400000743", "customer")
	provider := newAccount(t, pool, "shelf-pg-p@example.com", "+61400000744", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := newTestService()
	const rows = 5
	for i := range rows {
		if _, _, err := recordMilestone(t, pool, svc, provider, jobID, Recording{
			Milestone:  MilestoneEnRouteToPickup,
			Key:        "shelf-page-" + uuid.New().String(),
			RecordedAt: testInstant,
		}); err != nil {
			t.Fatalf("recording %d: %v", i, err)
		}
	}

	seen := map[string]int{}
	path := shelfMilestonesPath(jobID) + "?limit=1"
	for pages := 0; ; pages++ {
		if pages > rows+2 {
			t.Fatal("the walk did not terminate; has_more is never false")
		}

		rec := readAs(router, path, customer)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
		}
		page := decode[milestonePageBody](t, rec)
		for _, row := range page.Data {
			seen[row.ID]++
		}
		if !page.HasMore {
			break
		}
		if page.NextCursor == "" {
			t.Fatal("has_more is true and no cursor came with it")
		}
		path = shelfMilestonesPath(jobID) + "?limit=1&cursor=" + page.NextCursor
	}

	if len(seen) != rows {
		t.Errorf("saw %d distinct milestones over the walk, want %d", len(seen), rows)
	}
	for id, n := range seen {
		if n != 1 {
			t.Errorf("%s appeared %d times; every row shares one actor clock, so the "+
				"identifier is what has to break the tie", id, n)
		}
	}
}

// TestACursorThisEndpointDidNotIssueIsRefused is the failure a client can actually cause, and the
// one whose wrong answer is silent: a cursor that reached the query as a zero time would answer with
// the first page and start a client's list again.
func TestACursorThisEndpointDidNotIssueIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	router := readShelfRouter(t, pool)

	customer := newAccount(t, pool, "shelf-cur-c@example.com", "+61400000745", "customer")
	provider := newAccount(t, pool, "shelf-cur-p@example.com", "+61400000746", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	for name, cursor := range map[string]string{
		"not base64":       "!!!!",
		"the wrong shape":  "MQ",
		"a bad timestamp":  encodedCursor(t, "not-a-time", uuid.New().String()),
		"a bad identifier": encodedCursor(t, testInstant.Format(time.RFC3339Nano), "not-a-uuid"),
	} {
		t.Run(name, func(t *testing.T) {
			rec := readAs(router, shelfMilestonesPath(jobID)+"?cursor="+cursor, customer)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (%s)", rec.Code, rec.Body)
			}
			if code := errorCodeOf(t, rec); code != string(httpx.CodeBadRequest) {
				t.Errorf("code = %q, want %q", code, httpx.CodeBadRequest)
			}
		})
	}
}

// TestTheReadShelfRefusesAStrangerInTheDomainToo is the same rule asked of the service, so a test
// can tell "the stranger was refused" from "the job silently stopped existing".
func TestTheReadShelfRefusesAStrangerInTheDomainToo(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "shelf-dom-c@example.com", "+61400000747", "customer")
	provider := newAccount(t, pool, "shelf-dom-p@example.com", "+61400000748", "provider")
	stranger := newAccount(t, pool, "shelf-dom-s@example.com", "+61400000749", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	driverOnJob(t, pool, provider, jobID)

	svc := newTestService()

	if _, err := svc.DeliveryFor(t.Context(), pool, stranger, jobID); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("DeliveryFor() for a stranger = %v, want ErrJobNotFound", err)
	}
	if _, _, err := svc.MilestonesFor(t.Context(), pool, stranger, jobID,
		MilestonePage{Limit: 10}); !errors.Is(err, ErrJobNotFound) {
		t.Errorf("MilestonesFor() for a stranger = %v, want ErrJobNotFound", err)
	}
}

// encodedCursor builds a cursor of the right shape carrying whatever two fields a test names, so a
// refusal can be aimed at the parsing rather than at the encoding.
func encodedCursor(t *testing.T, at, id string) string {
	t.Helper()
	return pagination.Cursor{at, id}.Encode()
}
