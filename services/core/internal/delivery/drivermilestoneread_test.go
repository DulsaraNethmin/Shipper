package delivery

import (
	"context"
	"encoding/json"
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
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-121a — the driver reads the milestones recorded on the delivery their link opens.
//
// The fixtures come from assignment_test.go, driverauth_test.go and recording_test.go; nothing here
// duplicates them.
//
// # What only this file can show
//
// cmd/api's tests prove the route is *declared* with the driver's auth class and that each credential
// system is refused on the other's endpoint, through the real middleware chain — and they stop at 503
// because testDeps carries no pool. So the rows are this file's half: that a link for one job lists
// that job's milestones and no other job's, and that the shape carries the closed set of fields
// SHIP-121a's *Done when* holds it to.

// driverMilestoneReadRouter mounts the read behind the real guard, on the pattern cmd/api serves.
//
// A mux of its own rather than cmd/api's, for the reason [newDriverRouter] gives: this package
// cannot import package main.
func driverMilestoneReadRouter(t *testing.T, pool *pgxpool.Pool) http.Handler {
	t.Helper()

	handler, err := NewHandler(newTestService(), pool, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	mux := http.NewServeMux()
	mux.Handle("GET /v1/driver/jobs/{id}/milestones",
		RequireDriverToken(testDriverVerifier(t, clock.NewFixed(testDriverIssuedAt)))(
			handler.DriverMilestones()))
	return mux
}

// listMilestonesAs presents a credential to the driver's milestone read on whichever job the caller
// names.
//
// The job is an argument rather than being read out of the token, which is the whole point of the
// negative case: a test that built the path from the credential could not tell a scoped route from an
// unscoped one. Wave 7 recorded a driver surface that derived the job from its own credential and
// survived a full suite while rendering another job's delivery with a 200.
func listMilestonesAs(h http.Handler, jobID uuid.UUID, header, query string) *httptest.ResponseRecorder {
	path := "/v1/driver/jobs/" + jobID.String() + "/milestones"
	if query != "" {
		path += "?" + query
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	if header != "" {
		req.Header.Set("Authorization", header) // spelling:ok — HTTP header name, RFC 9110
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// driverMilestonePageBody is the envelope read back as a client would, with the wire keys named a
// second time.
//
// Deliberately not [driverMilestoneResponse] through [pagination.Page]: a test that unmarshalled
// into the struct the handler marshalled from would agree with a renamed json tag by construction.
type driverMilestonePageBody struct {
	Data []struct {
		ID         string `json:"id"`
		JobID      string `json:"job_id"`
		Milestone  string `json:"milestone"`
		RecordedBy string `json:"recorded_by"`
		Reason     string `json:"reason"`
		RecordedAt string `json:"recorded_at"`
		AcceptedAt string `json:"accepted_at"`
	} `json:"data"`
	NextCursor string `json:"next_cursor"`
	HasMore    bool   `json:"has_more"`
}

// recordDriverMilestone runs one driver recording in its own transaction, which is what the handler
// does.
func recordDriverMilestone(
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

// grantFor is the grant a verified link produces, built from the assignment it names.
//
// The token id matters: [Service.AssignmentFor] compares it with `link_token_id` on the row, which is
// how a reissued link stops opening the delivery it used to.
func grantFor(t *testing.T, jobID uuid.UUID, assignment Assignment, link DriverToken) DriverGrant {
	t.Helper()

	grant := DriverGrant{JobID: jobID, AssignmentID: assignment.ID, TokenID: link.ID}
	if grant.TokenID == "" {
		t.Fatal("the minted link carries no token id; AssignmentFor compares it with the row")
	}
	return grant
}

// TestADriverListsTheMilestonesOnTheirOwnJobAndNoOther is SHIP-121a's first clause, and the two jobs
// are what make it a claim rather than a formality.
//
// One driver, two deliveries, one link each. The link for the first is presented on the second, and
// the answer must be exactly what a job that does not exist answers — not the first job's milestones
// under the second job's URL, which is the failure wave 7 recorded and which passes every test built
// from a single fixture.
func TestADriverListsTheMilestonesOnTheirOwnJobAndNoOther(t *testing.T) {
	pool := pgtest.DB(t)
	router := driverMilestoneReadRouter(t, pool)
	svc := newTestService()

	customer := newAccount(t, pool, "dml-cust@example.com", "+61400000700", "customer")
	provider := newAccount(t, pool, "dml-prov@example.com", "+61400000701", "provider")

	first := awardedJob(t, pool, customer, provider)
	second := awardedJob(t, pool, customer, provider)

	firstAssignment, firstLink, _, err := assignGranting(t, pool, svc, provider, first, Nomination{
		DriverName: "Sam Patel", DriverMobile: "0412 345 678",
	})
	if err != nil {
		t.Fatalf("assigning to the first job: %v", err)
	}
	secondAssignment, secondLink, _, err := assignGranting(t, pool, svc, provider, second, Nomination{
		DriverName: "Sam Patel", DriverMobile: "0412 345 678",
	})
	if err != nil {
		t.Fatalf("assigning to the second job: %v", err)
	}

	// Two milestones on the first delivery and one on the second, so a query missing its job
	// filter answers three and fails rather than answering plausibly.
	firstGrant := grantFor(t, first, firstAssignment, firstLink)
	if _, _, err := recordDriverMilestone(t, pool, svc, firstGrant, Recording{
		Milestone: MilestoneEnRouteToPickup, Key: "dml-first-1",
	}); err != nil {
		t.Fatalf("recording on the first job: %v", err)
	}
	if _, _, err := recordDriverMilestone(t, pool, svc, firstGrant, Recording{
		Milestone: MilestonePickedUp, Key: "dml-first-2",
		Reason: "loaded at the side gate",
	}); err != nil {
		t.Fatalf("recording the second milestone on the first job: %v", err)
	}
	if _, _, err := recordDriverMilestone(t, pool, svc,
		grantFor(t, second, secondAssignment, secondLink), Recording{
			Milestone: MilestoneEnRouteToPickup, Key: "dml-second-1",
		}); err != nil {
		t.Fatalf("recording on the second job: %v", err)
	}

	rec := listMilestonesAs(router, first, bearer(firstLink.Value), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("the link's own job answered %d, want 200 (%s)", rec.Code, rec.Body)
	}

	page := decode[driverMilestonePageBody](t, rec)
	if len(page.Data) != 2 {
		t.Fatalf("the driver sees %d milestones, want the 2 on their job (%s)", len(page.Data), rec.Body)
	}
	for _, entry := range page.Data {
		if entry.JobID != first.String() {
			t.Errorf("a milestone on %s appears in the list for %s", entry.JobID, first)
		}
		if entry.RecordedBy != "driver" {
			t.Errorf("recorded_by = %q, want driver", entry.RecordedBy)
		}
	}

	// Newest first by the actor's clock, which is what the shelf's own list does and what a client
	// implementing one implements for both.
	if page.Data[0].Milestone != "picked_up" || page.Data[1].Milestone != "en_route_to_pickup" {
		t.Errorf("the list reads %q then %q, want picked_up then en_route_to_pickup — newest "+
			"first by the actor's clock", page.Data[0].Milestone, page.Data[1].Milestone)
	}
	if page.Data[0].Reason != "loaded at the side gate" {
		t.Errorf("reason = %q, want the driver's own note", page.Data[0].Reason)
	}

	// The other job, on the first link. The same answer a job that does not exist gets.
	other := listMilestonesAs(router, second, bearer(firstLink.Value), "")
	if other.Code != http.StatusNotFound {
		t.Fatalf("a link for one job listed another job's milestones with status %d, want 404 "+
			"— the token grants exactly one job (CLAUDE.md) (%s)", other.Code, other.Body)
	}

	nowhere := listMilestonesAs(router, uuid.New(), bearer(firstLink.Value), "")
	if nowhere.Code != other.Code || nowhere.Body.String() != other.Body.String() {
		t.Errorf("another job answers %d %s and a job that does not exist answers %d %s; the two "+
			"must be indistinguishable", other.Code, other.Body, nowhere.Code, nowhere.Body)
	}
}

// TestTheDriverMilestoneListCarriesNoRecipientDetails is the *Done when*'s disclosure clause, and it
// is written against a fixture that **has** both fields to leak.
//
// A `Delivered` milestone carries `recipient_name` and `delivery_note` (SHIP-123), and both parties
// to the job read them on `GET /v1/jobs/{id}/delivery/milestones`. They are a third party's details
// recorded on the handover, and a forwardable seven-day link naming no account is not the credential
// to serve them on — so the driver's shape is a closed set that does not include them.
//
// The fixture records the delivery through the **provider's** entry point, which is the case that
// makes the omission matter: the driver would otherwise be reading a recipient's name they never
// learned.
func TestTheDriverMilestoneListCarriesNoRecipientDetails(t *testing.T) {
	pool := pgtest.DB(t)
	router := driverMilestoneReadRouter(t, pool)
	svc := newTestService()

	customer := newAccount(t, pool, "dml-priv-c@example.com", "+61400000702", "customer")
	provider := newAccount(t, pool, "dml-priv-p@example.com", "+61400000703", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	_, link, _, err := assignGranting(t, pool, svc, provider, jobID, Nomination{
		DriverName: "Sam Patel", DriverMobile: "0412 345 678",
	})
	if err != nil {
		t.Fatalf("assigning a driver: %v", err)
	}

	// Delivered is not reachable from where an assignment leaves the job, so the fixture walks the
	// status to In transit first — through [moveJob], which writes job_status_history and no
	// `milestones` row, so the list below still carries exactly the one milestone this test records.
	moveJob(t, pool, jobID, jobs.User(jobs.ActorProvider, provider),
		jobs.StatusEnRouteToPickup, jobs.StatusPickedUp, jobs.StatusInTransit)

	const recipient = "R. Chen"
	const note = "Left with reception, signed for"

	if _, _, err := recordMilestone(t, pool, svc, provider, jobID, Recording{
		Milestone:     MilestoneDelivered,
		Key:           "dml-priv-delivered",
		Exception:     ExceptionRecipientObjected,
		RecipientName: recipient,
		DeliveryNote:  note,
	}); err != nil {
		t.Fatalf("recording the delivery: %v", err)
	}

	// The fixture has something to withhold, verified rather than assumed. A privacy check whose
	// row carries neither field passes forever and proves nothing.
	var storedName, storedNote string
	if err := pool.QueryRow(t.Context(),
		`SELECT recipient_name, delivery_note FROM milestones
		  WHERE job_id = $1 AND milestone = 'Delivered'`, jobID).Scan(&storedName, &storedNote); err != nil {
		t.Fatalf("reading the stored delivery details: %v", err)
	}
	if storedName != recipient || storedNote != note {
		t.Fatalf("the stored row is %q/%q, want %q/%q — this test withholds nothing otherwise",
			storedName, storedNote, recipient, note)
	}

	rec := listMilestonesAs(router, jobID, bearer(link.Value), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	// Not vacuous: the response has to be carrying the delivered milestone before "it carries no
	// recipient" means anything at all.
	page := decode[driverMilestonePageBody](t, rec)
	if len(page.Data) != 1 || page.Data[0].Milestone != "delivered" {
		t.Fatalf("the list does not carry the delivered milestone, so it proves nothing: %s", rec.Body)
	}

	// A closed key set rather than a search for the two names, which is the argument SHIP-83's
	// budget test makes: a search for "recipient_name" catches `recipient_name` and misses
	// `recipient` or `signed_by`. A field added to the driver's shape has to be added here too.
	var document struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &document); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, rec.Body)
	}
	allowed := map[string]bool{
		"id": true, "job_id": true, "milestone": true, "recorded_by": true,
		"reason": true, "recorded_at": true, "accepted_at": true,
	}
	for _, entry := range document.Data {
		for key := range entry {
			if !allowed[key] {
				t.Errorf("the driver's milestone list carries %q.\n"+
					"  The driver's surface is narrow and its narrowness is the security "+
					"property: the credential is a forwardable seven-day link naming no "+
					"account. A field added here is a decision somebody records.", key)
			}
		}
	}

	// And the values, in the bytes, because a field could arrive under a name this list happens to
	// allow. Both are absent from every other fixture in this test.
	body := rec.Body.String()
	for _, secret := range []string{recipient, note} {
		if strings.Contains(body, secret) {
			t.Errorf("the driver's milestone list discloses %q: %s", secret, body)
		}
	}
}

// TestASupersededLinkListsNothing is the check the token alone cannot make.
//
// A driver token is stateless and cannot be recalled, so it keeps verifying after the link behind it
// has been reissued. [Service.MilestonesForDriver] asks [Service.AssignmentFor] before it reads
// anything, which is what turns "this signature is valid" into "this link still opens the delivery".
// Without that call the read would work for the holder of every link the assignment has ever had.
func TestASupersededLinkListsNothing(t *testing.T) {
	pool := pgtest.DB(t)
	router := driverMilestoneReadRouter(t, pool)
	svc := newTestService()

	customer := newAccount(t, pool, "dml-sup-c@example.com", "+61400000704", "customer")
	provider := newAccount(t, pool, "dml-sup-p@example.com", "+61400000705", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	assignment, first, _, err := assignGranting(t, pool, svc, provider, jobID, Nomination{
		DriverName: "Sam Patel", DriverMobile: "0412 345 678",
	})
	if err != nil {
		t.Fatalf("assigning a driver: %v", err)
	}
	if _, _, err := recordDriverMilestone(t, pool, svc, grantFor(t, jobID, assignment, first),
		Recording{Milestone: MilestoneEnRouteToPickup, Key: "dml-sup-1"}); err != nil {
		t.Fatalf("recording a milestone: %v", err)
	}

	if rec := listMilestonesAs(router, jobID, bearer(first.Value), ""); rec.Code != http.StatusOK {
		t.Fatalf("the live link answered %d, want 200 (%s)", rec.Code, rec.Body)
	}

	var second DriverToken
	if err := db.InTx(t.Context(), pool, func(ctx context.Context, r db.Runner) error {
		var err error
		_, second, err = svc.ReissueDriverLink(ctx, r, provider, jobID)
		return err
	}); err != nil {
		t.Fatalf("reissuing the link: %v", err)
	}

	if rec := listMilestonesAs(router, jobID, bearer(second.Value), ""); rec.Code != http.StatusOK {
		t.Fatalf("the reissued link answered %d, want 200 (%s)", rec.Code, rec.Body)
	}
	if rec := listMilestonesAs(router, jobID, bearer(first.Value), ""); rec.Code != http.StatusNotFound {
		t.Fatalf("the superseded link answered %d, want 404 — a token that still verifies is not "+
			"a link that still opens anything (%s)", rec.Code, rec.Body)
	}
}

// TestNeitherCredentialOpensTheOthersMilestoneRead is the second and third clauses of the *Done
// when*, in the half this package can reach.
//
// A mobile access token on the driver's route is what a confused client does. The other direction —
// a driver's link on the parties' shelf — is asserted in cmd/api, where the real chain runs; here the
// most that can be shown is that the guard on this route refuses a session, and it is signed with the
// driver keyset's own key so that the refusal is about the *audience* rather than the signature.
func TestNeitherCredentialOpensTheOthersMilestoneRead(t *testing.T) {
	pool := pgtest.DB(t)
	router := driverMilestoneReadRouter(t, pool)

	jobID := uuid.New()

	for name, header := range map[string]string{
		"a mobile access token":   bearer(mintMobileSession(t)),
		"no credential at all":    "",
		"a token that is not one": bearer("not-a-token"),
	} {
		t.Run(name, func(t *testing.T) {
			rec := listMilestonesAs(router, jobID, header, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
			}
		})
	}
}

// TestTheDriverMilestoneReadTakesNoJobIdentifier is the shape argument, held by a compiler rather
// than by a comment.
//
// [Service.MilestonesForDriver] takes a [DriverGrant] and no job identifier, exactly as
// [Service.AssignmentFor], [Service.RecordDriverMilestone], [Service.PresignDriverProofUpload],
// [Service.VerifyDriverProof] and [Service.DeliveryFinishedFor] do. A handler therefore cannot widen
// the scope by passing the wrong job, because it has none to pass. This asserts the signature by
// assigning it, which is what makes a fifth parameter appearing later a compile failure here.
func TestTheDriverMilestoneReadTakesNoJobIdentifier(t *testing.T) {
	var read func(context.Context, db.Runner, DriverGrant, MilestonePage) ([]Record, bool, error)
	read = newTestService().MilestonesForDriver

	if read == nil {
		t.Fatal("MilestonesForDriver is nil")
	}
}
