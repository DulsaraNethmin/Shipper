package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SHIP-152, from the side this package can see.
//
// # What is here and what is deliberately in `make verify` instead
//
// The statements behind [JobDirectory] live in cmd/api, because they span `jobs`, `bids` and
// `job_status_history` — three tables belonging to two domains this package may not import. A test
// here that reimplemented them would be the same SQL written twice and would prove nothing about the
// copy that runs. So this file drives the **handler** against a directory that records what it was
// asked for, and `scripts/verify/90-admin.sh` exercises the real statements against the built binary.
//
// That is the same split SHIP-113 and SHIP-117 took for `jobPartiesLookup` and
// `exceptionQueueLookup`, for the same reason, and the verify section is not optional in it: the
// query translation is checked here and **the SQL is checked there or nowhere**.
//
// What this file establishes: the permission is in front of both endpoints, every query parameter is
// validated and reaches the query as the domain's own value, the pagination envelope and its cursor
// round trip, and the two response shapes carry a closed set of keys with nothing commercial in it.

// testJobStatuses is Docs/02 §1's list as a fixture.
//
// **Hand-written here on purpose, and it is not the copy jobsearch.go refuses.** The list the
// service actually validates against is supplied by cmd/api from `jobs.Statuses`, which is generated
// from `contracts/statuses.yaml`; this is a test fixture standing in for it, and a test fixture that
// drifts from the real list makes this file wrong rather than the product.
var testJobStatuses = []string{
	"Draft", "Open", "Negotiating", "Awarded", "Driver assigned", "En route to pickup",
	"Picked up", "In transit", "Delivered", "Completed", "Cancelled", "Disputed",
}

// testJobDirectory records what it was asked and answers what it was told to.
//
// Not a database. What it establishes is the half a fake can establish honestly: that the handler
// turned a query string into the domain's own [JobQuery], asked for one more row than the caller
// wanted, and rendered what came back. Whether the SQL behind the real directory is right is
// `make verify`'s to say.
type testJobDirectory struct {
	// searched is the last query the handler passed down.
	searched JobQuery

	// records is what SearchJobs answers with.
	records []JobRecord

	// detail is what OpenJob answers with, and found is whether it says the job exists.
	detail JobDetail
	found  bool

	// opened is the last identifier OpenJob was asked for.
	opened uuid.UUID
}

func (d *testJobDirectory) SearchJobs(_ context.Context, _ db.Runner, q JobQuery) ([]JobRecord, error) {
	d.searched = q
	return d.records, nil
}

func (d *testJobDirectory) OpenJob(_ context.Context, _ db.Runner, jobID uuid.UUID) (JobDetail, bool, error) {
	d.opened = jobID
	return d.detail, d.found, nil
}

// jobFixture is a handler with a recording directory and a signed-in support administrator.
type jobFixture struct {
	directory *testJobDirectory
	handler   *Handler
	auth      *Authenticator
	token     string
}

func newJobFixture(t *testing.T) jobFixture {
	t.Helper()

	creds, auth, pool, clk := adminAuth(t)
	directory := &testJobDirectory{}

	services := testServices(t, creds, pool, clk)
	console, err := NewJobConsole(directory, testJobStatuses, pool)
	if err != nil {
		t.Fatalf("building the job search: %v", err)
	}
	services.Jobs = console

	handler, err := NewHandler(services, pool, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	// A support administrator, because `jobs.read` is held by every role — looking is what the
	// least-privileged one exists to be able to do (Docs/01 §4.6 lists searching first).
	anAdministrator(t, creds, "job-reader@example.com", RoleSupport)
	issued, _, err := signIn(t, creds, "job-reader@example.com", testPassword, "10.0.70.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	return jobFixture{directory: directory, handler: handler, auth: auth, token: issued.Token}
}

// search runs one request through the guard and the handler.
func (f jobFixture) search(t *testing.T, query url.Values) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/jobs?"+query.Encode(), nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.SearchJobs()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// open runs one detail request through the guard and the handler.
func (f jobFixture) open(t *testing.T, jobID string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/jobs/"+jobID, nil)
	req.SetPathValue("id", jobID)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.OpenJob()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func aJobRecord(status string, createdAt time.Time) JobRecord {
	id, _ := uuid.NewV7()
	customer, _ := uuid.NewV7()
	return JobRecord{
		ID:               id,
		CustomerID:       customer,
		Status:           status,
		GoodsDescription: "An upright piano and two stools",
		BidCount:         3,
		CreatedAt:        createdAt,
		UpdatedAt:        createdAt,
	}
}

// TestTheJobSearchPassesEveryFilterToTheDomainAsItsOwnValue.
//
// The handler's actual work. A query string is text; [JobQuery] is the domain's vocabulary, and
// everything between the two — trimming, parsing an identifier, asking for one extra row — is where
// a search silently answers the wrong question.
func TestTheJobSearchPassesEveryFilterToTheDomainAsItsOwnValue(t *testing.T) {
	f := newJobFixture(t)
	customer := uuid.New()

	status, body := f.search(t, url.Values{
		"q":        {"  piano  "},
		"status":   {"En route to pickup"},
		"customer": {customer.String()},
		"limit":    {"5"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	got := f.directory.searched
	if got.Term != "piano" {
		t.Errorf("term = %q, want %q — the term is trimmed before it becomes a pattern", got.Term, "piano")
	}
	if got.Status != "En route to pickup" {
		t.Errorf("status = %q, want the stored form with its spaces intact", got.Status)
	}
	if got.CustomerID != customer {
		t.Errorf("customer = %s, want %s", got.CustomerID, customer)
	}

	// One more than asked for, which is how "is there another page" is answered by the rows
	// rather than by a second COUNT. A handler that passed the limit through would report
	// has_more=false on a full page.
	if got.Limit != 6 {
		t.Errorf("limit = %d, want 6 — the handler asks for one more row than the caller wanted",
			got.Limit)
	}
}

// TestAMistypedJobStatusIsRefusedRatherThanIgnored.
//
// The same decision the account search takes about a standing, and the reasoning is identical:
// ignoring an unrecognised filter answers with **every** job, and a support engineer who typed
// `Delivred` would read the whole marketplace as delivered. 422 rather than 400, because it is a
// field-level validation failure in validate.Errors' shape.
func TestAMistypedJobStatusIsRefusedRatherThanIgnored(t *testing.T) {
	f := newJobFixture(t)
	f.directory.records = []JobRecord{aJobRecord("Open", time.Now())}

	status, body := f.search(t, url.Values{"status": {"Delivred"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", status, body)
	}
	if !strings.Contains(body, `"status"`) {
		t.Errorf("the refusal does not name the field: %s", body)
	}

	// The alternatives are named, so somebody can correct the typo without reading Docs/02.
	if !strings.Contains(body, "En route to pickup") {
		t.Errorf("the refusal does not say what the statuses are: %s", body)
	}

	// And nothing was searched. A refused query that still reached the database would be a
	// scan somebody paid for and nobody read.
	if f.directory.searched.Limit != 0 {
		t.Error("a refused query reached the directory anyway")
	}
}

// TestAMalformedCustomerFilterIsRefused, for the same reason and in the same shape.
func TestAMalformedCustomerFilterIsRefused(t *testing.T) {
	f := newJobFixture(t)

	status, body := f.search(t, url.Values{"customer": {"not-a-uuid"}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", status, body)
	}
	if !strings.Contains(body, `"customer"`) {
		t.Errorf("the refusal does not name the field: %s", body)
	}
}

// TestAnOverlongJobSearchTermIsRefused.
//
// A leading-wildcard LIKE cannot use an index, so the cost of the scan should not be a function of
// what somebody pasted into a search box.
func TestAnOverlongJobSearchTermIsRefused(t *testing.T) {
	f := newJobFixture(t)

	status, body := f.search(t, url.Values{"q": {strings.Repeat("x", maxJobTermLength+1)}})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", status, body)
	}
	if !strings.Contains(body, `"q"`) {
		t.Errorf("the refusal does not name the field: %s", body)
	}
}

// TestASearchTermIsAStringAndNotAPattern.
//
// `%` is LIKE's "anything". Unescaped, one character in a search box returns every job in the
// marketplace to the least-privileged role. The escaping lives in [LikePattern] because the
// statement that uses it is in cmd/api and the rule about what a search term *means* is this
// domain's — so this asserts the exported rule directly.
func TestASearchTermIsAStringAndNotAPattern(t *testing.T) {
	for _, tc := range []struct{ term, want string }{
		{"piano", `%piano%`},
		{"100%", `%100\%%`},
		{"a_b", `%a\_b%`},
		{`back\slash`, `%back\\slash%`},
	} {
		if got := LikePattern(tc.term); got != tc.want {
			t.Errorf("LikePattern(%q) = %q, want %q", tc.term, got, tc.want)
		}
	}
}

// TestTheJobSearchPagesAndIssuesACursorItsOwnDecoderAccepts.
//
// A cursor is written by one function and read by another, and the round trip is the only thing that
// establishes they agree. A page that hands out a cursor its own endpoint refuses is a search that
// stops at the first page, which nothing else here would notice.
func TestTheJobSearchPagesAndIssuesACursorItsOwnDecoderAccepts(t *testing.T) {
	f := newJobFixture(t)

	base := time.Date(2026, 8, 1, 9, 0, 0, 0, time.UTC)
	f.directory.records = []JobRecord{
		aJobRecord("Open", base.Add(2*time.Hour)),
		aJobRecord("Open", base.Add(time.Hour)),
		aJobRecord("Open", base),
	}

	status, body := f.search(t, url.Values{"limit": {"2"}})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var page struct {
		Data       []adminJobResponse `json:"data"`
		NextCursor string             `json:"next_cursor"`
		HasMore    bool               `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the page: %v (%s)", err, body)
	}

	// Two rows, not three: the extra row answers "is there another page" and is dropped.
	if len(page.Data) != 2 {
		t.Fatalf("the page carries %d jobs, want 2 — the extra row is the has-more probe and "+
			"must not be served", len(page.Data))
	}
	if !page.HasMore || page.NextCursor == "" {
		t.Fatalf("a full page reported no more: has_more=%v cursor=%q", page.HasMore, page.NextCursor)
	}

	decoded, err := decodeJobCursor(page.NextCursor)
	if err != nil {
		t.Fatalf("the endpoint issued a cursor its own decoder refuses: %v", err)
	}
	if decoded.JobID != f.directory.records[1].ID {
		t.Errorf("the cursor names %s, want the last job served, %s",
			decoded.JobID, f.directory.records[1].ID)
	}
}

// TestTheAdminJobShapesCarryNothingCommercialOrPrivate.
//
// **A closed key set, not a search for the word "budget".** SHIP-83 established the axis: a field
// called `max_price` passes a spelling-based check and leaks exactly the same fact. So this asserts
// what is *present*, which fails when a field is added as well as when one is renamed.
//
// The budget is the field this is really about — jobsearch.go argues why an administrator's shape
// leaves it out even though Docs/01 §4.3's invariant names providers — and the addresses and contact
// details are Docs/01 §5.1's "minimise exposure".
func TestTheAdminJobShapesCarryNothingCommercialOrPrivate(t *testing.T) {
	f := newJobFixture(t)

	jobID := uuid.New()
	bidID, _ := uuid.NewV7()
	eventID, _ := uuid.NewV7()

	f.directory.found = true
	f.directory.detail = JobDetail{
		Job: aJobRecord("Awarded", time.Now()),
		Bids: []BidRecord{{
			ID: bidID, ProviderID: uuid.New(), Status: "Accepted",
			OfferedBy: "provider", AmountCents: 45000,
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		}},
		History: []StatusEvent{{
			ID: eventID, From: "Open", To: "Awarded", ActorType: "customer",
			ActorID: uuid.New(), ActorRecordedAt: time.Now(), ServerRecordedAt: time.Now(),
		}},
	}

	status, body := f.open(t, jobID.String())
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var detail struct {
		Job     map[string]any   `json:"job"`
		Bids    []map[string]any `json:"bids"`
		History []map[string]any `json:"history"`
	}
	if err := json.Unmarshal([]byte(body), &detail); err != nil {
		t.Fatalf("decoding the detail: %v (%s)", err, body)
	}

	assertKeys(t, "the job", detail.Job,
		"id", "customer_id", "status", "goods_description", "bid_count",
		"expires_at", "created_at", "updated_at")

	if len(detail.Bids) != 1 {
		t.Fatalf("the detail carries %d bids, want 1", len(detail.Bids))
	}
	assertKeys(t, "a bid", detail.Bids[0],
		"id", "provider_id", "status", "offered_by", "amount_cents",
		"pickup_at", "deliver_by", "message", "superseded_by", "created_at", "updated_at")

	if len(detail.History) != 1 {
		t.Fatalf("the detail carries %d transitions, want 1", len(detail.History))
	}
	assertKeys(t, "a transition", detail.History[0],
		"id", "from", "to", "actor_type", "actor_id", "reason",
		"actor_recorded_at", "server_recorded_at")
}

// assertKeys holds one serialised object to exactly the keys named.
func assertKeys(t *testing.T, what string, got map[string]any, want ...string) {
	t.Helper()

	expected := map[string]bool{}
	for _, k := range want {
		expected[k] = true
		if _, ok := got[k]; !ok {
			t.Errorf("%s is missing %q", what, k)
		}
	}
	for k := range got {
		if !expected[k] {
			t.Errorf("%s carries %q, which is not in the closed set this shape is held to.\n"+
				"If the field is intended, add it here deliberately — this assertion exists so "+
				"that a customer's budget cannot arrive on an administrative shape by accident "+
				"(Docs/01 §4.3, SHIP-83).", what, k)
		}
	}
}

// TestOpeningAJobThatDoesNotExistIs404.
//
// And it is **not** a disclosure decision, unlike the identical answer the dispute endpoint gives. An
// administrator holding `jobs.read` may open any job; there is nothing here being kept from them,
// and a job they cannot find genuinely does not exist.
func TestOpeningAJobThatDoesNotExistIs404(t *testing.T) {
	f := newJobFixture(t)
	f.directory.found = false

	jobID := uuid.New()
	status, body := f.open(t, jobID.String())
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", status, body)
	}
	if f.directory.opened != jobID {
		t.Errorf("the directory was asked for %s, want %s", f.directory.opened, jobID)
	}
}

// TestBothJobEndpointsRefuseAnUnauthenticatedCaller.
//
// The credential, on the wire. Every administrative route is behind RequireAdmin, and a route
// declared without it would be served open — which for a search over every job in the marketplace is
// the whole of Docs/01 §4.6 undone.
func TestBothJobEndpointsRefuseAnUnauthenticatedCaller(t *testing.T) {
	f := newJobFixture(t)
	f.directory.records = []JobRecord{aJobRecord("Open", time.Now())}

	for _, tc := range []struct {
		name    string
		request func() *http.Request
		handler http.Handler
	}{
		{
			name: "the search",
			request: func() *http.Request {
				return httptest.NewRequest(http.MethodGet, "/v1/admin/jobs", nil)
			},
			handler: f.handler.SearchJobs(),
		},
		{
			name: "the detail view",
			request: func() *http.Request {
				r := httptest.NewRequest(http.MethodGet, "/v1/admin/jobs/"+uuid.New().String(), nil)
				r.SetPathValue("id", uuid.New().String())
				return r
			},
			handler: f.handler.OpenJob(),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			RequireAdmin(f.auth)(tc.handler).ServeHTTP(rec, tc.request())

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
			}
			if strings.Contains(rec.Body.String(), "piano") {
				t.Errorf("a refused request answered with jobs anyway: %s", rec.Body)
			}
		})
	}
}

// TestTheJobConsoleRefusesToBeBuiltWithoutWhatItNeeds.
//
// Two collaborators, two failures that are invisible at run time. A nil directory answers "no such
// job" to everything, which is indistinguishable from a quiet marketplace — the failure
// [NewModeration] refuses for the same reason. An empty status list makes every status filter a 422
// naming no alternatives, which reads as the endpoint being broken rather than the caller having
// mistyped something.
func TestTheJobConsoleRefusesToBeBuiltWithoutWhatItNeeds(t *testing.T) {
	if _, err := NewJobConsole(nil, testJobStatuses, nil); err == nil {
		t.Error("a job console was built with no directory behind it")
	}
	if _, err := NewJobConsole(&testJobDirectory{}, nil, nil); err == nil {
		t.Error("a job console was built with no status list, so no filter could ever be valid")
	}
}

// TestTheStatusListCannotBeWidenedByACaller.
//
// [JobConsole.Statuses] hands out a copy. The stored slice is the service's, and a caller that
// appended to what it was handed would widen the filter for the whole process — which is not a
// hypothetical shape, it is exactly how a response builder that adds "and also this one" goes wrong.
func TestTheStatusListCannotBeWidenedByACaller(t *testing.T) {
	console, err := NewJobConsole(&testJobDirectory{}, testJobStatuses, nil)
	if err != nil {
		t.Fatalf("building the job console: %v", err)
	}

	//nolint:staticcheck // appending to the returned slice is the mistake being reproduced.
	_ = append(console.Statuses(), "Impounded")

	if console.KnowsStatus("Impounded") {
		t.Error("a caller widened the status list by appending to what Statuses() returned")
	}
}
