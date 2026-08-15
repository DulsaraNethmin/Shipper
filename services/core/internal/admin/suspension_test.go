package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SHIP-166 against a real PostgreSQL.
//
// Docs/04 §9's second required internal control: "two-person review for permanent account suspension
// where practical". Every claim here is about a *rule*, and two of them are rules the database holds
// — `ck_suspension_reviews_two_people` and `uq_suspension_reviews_one_pending` — so a mocked store
// would report these as passing while proving nothing (Docs/06 §4.1).
//
// newAuditFixture, signedIn, newAccount, standingOf and entriesFor come from the audit and
// enforcement suites.

// requestSuspension drives POST /v1/admin/users/{id}/suspension and returns the status and body.
func requestSuspension(
	t *testing.T,
	f auditFixture,
	token string,
	userID uuid.UUID,
	reason string,
) (int, string) {
	t.Helper()

	body, err := json.Marshal(map[string]string{"reason": reason})
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/v1/admin/users/"+userID.String()+"/suspension", strings.NewReader(string(body)))
	req.SetPathValue("id", userID.String())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.RequestSuspension()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// approveSuspension drives POST /v1/admin/suspensions/{id}/approval.
func approveSuspension(t *testing.T, f auditFixture, token, reviewID string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost,
		"/v1/admin/suspensions/"+reviewID+"/approval", nil)
	req.SetPathValue("id", reviewID)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.ApproveSuspension()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// reviewIDOf reads the identifier out of a review response.
func reviewIDOf(t *testing.T, body string) string {
	t.Helper()

	var review suspensionReviewResponse
	if err := json.Unmarshal([]byte(body), &review); err != nil {
		t.Fatalf("decoding the review: %v (%s)", err, body)
	}
	if review.ID == "" {
		t.Fatalf("the response carries no review id: %s", body)
	}
	return review.ID
}

// TestATwoPersonReviewSuspendsTheAccount is SHIP-166's *Done when*, in the direction that succeeds.
//
// "A permanent suspension requires a second administrator's approval." Two administrators, two
// requests, and the account is suspended only after the second — which is the whole of the control.
//
// **The account is checked after the request as well as after the approval.** A control that
// suspended on the request and collected a signature afterwards would pass a test that only looked at
// the end, and it would be no control at all: the account is already gone by the time anybody
// reviews it.
func TestATwoPersonReviewSuspendsTheAccount(t *testing.T) {
	f := newAuditFixture(t)

	_, requester := f.signedIn(t, "two-a@example.com", RoleModerator, "10.0.66.1")
	approverAdmin, approver := f.signedIn(t, "two-b@example.com", RoleModerator, "10.0.66.2")

	userID := newAccount(t, f.pool, "two-subject@example.com", "+61400661", "provider")

	const reason = "Three unresolved safety reports in a fortnight; see the exception queue."

	status, body := requestSuspension(t, f, requester, userID, reason)
	if status != http.StatusAccepted {
		t.Fatalf("requesting: status = %d, want 202 (%s)", status, body)
	}
	if s := standingOf(t, f.pool, userID); s != "active" {
		t.Fatalf("the account is %q after a *request*, want active.\n"+
			"A control that suspends first and collects the second signature afterwards is "+
			"not a control: the account is gone before anybody has reviewed anything.", s)
	}

	var requested suspensionReviewResponse
	if err := json.Unmarshal([]byte(body), &requested); err != nil {
		t.Fatalf("decoding the review: %v (%s)", err, body)
	}
	if requested.Status != ReviewPending.String() {
		t.Errorf("status = %q, want %q", requested.Status, ReviewPending)
	}
	if requested.ApprovedBy != "" {
		t.Errorf("a pending review names an approver (%q)", requested.ApprovedBy)
	}
	if requested.Reason != reason {
		t.Errorf("reason = %q, want the case the second administrator is asked to agree with",
			requested.Reason)
	}

	status, body = approveSuspension(t, f, approver, requested.ID)
	if status != http.StatusOK {
		t.Fatalf("approving: status = %d, want 200 (%s)", status, body)
	}

	var approved suspensionReviewResponse
	if err := json.Unmarshal([]byte(body), &approved); err != nil {
		t.Fatalf("decoding the approval: %v (%s)", err, body)
	}
	if approved.Status != ReviewApproved.String() {
		t.Errorf("status = %q, want %q", approved.Status, ReviewApproved)
	}
	if approved.ApprovedBy != approverAdmin.ID.String() {
		t.Errorf("approved_by = %q, want the second administrator %s",
			approved.ApprovedBy, approverAdmin.ID)
	}
	if approved.ApprovedBy == approved.RequestedBy {
		t.Error("the review records one administrator twice, which is a two-person review with " +
			"one person in it")
	}
	if approved.ApprovedAt == "" {
		t.Error("an approved review carries no instant")
	}

	if s := standingOf(t, f.pool, userID); s != "suspended" {
		t.Fatalf("the account is %q after the approval, want suspended", s)
	}
}

// TestOneAdministratorCannotCompleteATwoPersonReview is the ticket.
//
// **This is the test the mutation targets.** Docs/04 §9 asks the platform to require two people, and
// the single failure that matters is one person satisfying it alone — which is not a crash, not a
// panic and not a wrong number, but a control that quietly is not one.
//
// The refusal is checked at three levels, because each can be removed independently:
//
//  1. the response is `409 admin_same_administrator` rather than a success;
//  2. the **account is untouched**, which is what a partial write would break;
//  3. the review is still `pending`, so a second administrator can still act on it.
//
// The database's own refusal is TestTheDatabaseRefusesAReviewApprovedByItsRequester, which drives
// `ck_suspension_reviews_two_people` past this service entirely.
func TestOneAdministratorCannotCompleteATwoPersonReview(t *testing.T) {
	f := newAuditFixture(t)

	requesterAdmin, requester := f.signedIn(t, "alone@example.com", RoleModerator, "10.0.66.3")
	userID := newAccount(t, f.pool, "alone-subject@example.com", "+61400663", "provider")

	status, body := requestSuspension(t, f, requester, userID,
		"Repeated refusal to supply an insurance certificate after three requests.")
	if status != http.StatusAccepted {
		t.Fatalf("requesting: status = %d, want 202 (%s)", status, body)
	}
	reviewID := reviewIDOf(t, body)

	status, body = approveSuspension(t, f, requester, reviewID)
	if status != http.StatusConflict {
		t.Fatalf("the administrator who requested the suspension approved it: status = %d, "+
			"want 409.\nDocs/04 §9 requires a two-person review, and a rule one person can "+
			"satisfy alone is not one. (%s)", status, body)
	}
	if !strings.Contains(body, string(CodeSameAdministrator)) {
		t.Errorf("body = %s, want %q so a console can say who is needed",
			body, CodeSameAdministrator)
	}

	if s := standingOf(t, f.pool, userID); s != "active" {
		t.Fatalf("the account is %q after a refused approval, want active.\n"+
			"The suspension applied even though the approval was refused, which is the "+
			"partial write the transaction exists to prevent.", s)
	}

	var stored struct {
		Status     string
		ApprovedBy *uuid.UUID
	}
	if err := f.pool.QueryRow(t.Context(),
		`SELECT status, approved_by FROM suspension_reviews WHERE id = $1`, reviewID,
	).Scan(&stored.Status, &stored.ApprovedBy); err != nil {
		t.Fatalf("reading the review back: %v", err)
	}
	if stored.Status != ReviewPending.String() {
		t.Errorf("the review is %q after a refused approval, want pending — a second "+
			"administrator must still be able to act on it", stored.Status)
	}
	if stored.ApprovedBy != nil {
		t.Errorf("the review records %s as approver after the approval was refused",
			*stored.ApprovedBy)
	}

	// And the second administrator can still complete it, which is what says the refusal
	// narrowed *who* rather than closing the review.
	other, otherToken := f.signedIn(t, "alone-b@example.com", RoleModerator, "10.0.66.4")
	status, body = approveSuspension(t, f, otherToken, reviewID)
	if status != http.StatusOK {
		t.Fatalf("a second administrator could not approve the review: status = %d (%s)",
			status, body)
	}
	if s := standingOf(t, f.pool, userID); s != "suspended" {
		t.Errorf("the account is %q after a valid approval, want suspended", s)
	}
	if other.ID == requesterAdmin.ID {
		t.Fatal("the fixture signed in the same administrator twice, so this test proves nothing")
	}
}

// TestTheDatabaseRefusesAReviewApprovedByItsRequester is the Docs/10 §3.4 pairing for the control.
//
// The service refuses it and so does `ck_suspension_reviews_two_people`, and **two checks believed to
// agree are two checks until something compares them.** This one drives the constraint through a
// connection that does not go through the service — which is precisely the connection a CHECK exists
// for: a repair script, a support query typed at a psql prompt, or the next endpoint somebody adds
// without reading suspension.go.
func TestTheDatabaseRefusesAReviewApprovedByItsRequester(t *testing.T) {
	f := newAuditFixture(t)

	requester, token := f.signedIn(t, "sql-a@example.com", RoleModerator, "10.0.66.5")
	userID := newAccount(t, f.pool, "sql-subject@example.com", "+61400665", "provider")

	status, body := requestSuspension(t, f, token, userID,
		"Goods repeatedly collected outside the agreed window without notice.")
	if status != http.StatusAccepted {
		t.Fatalf("requesting: status = %d, want 202 (%s)", status, body)
	}
	reviewID := reviewIDOf(t, body)

	_, err := f.pool.Exec(t.Context(), `
		UPDATE suspension_reviews
		   SET status = 'approved', approved_by = $2, approved_at = now()
		 WHERE id = $1`, reviewID, requester.ID)
	if err == nil {
		t.Fatal("the database accepted a review approved by the administrator who requested it.\n" +
			"Application logic refusing to write is a convention, and a convention does not " +
			"apply to a repair script or to a psql prompt — which is why Docs/04 §9's control " +
			"is a CHECK constraint as well as a check in Go.")
	}
	if !strings.Contains(err.Error(), "ck_suspension_reviews_two_people") {
		t.Errorf("the write was refused by something other than the two-person constraint: %v", err)
	}
}

// TestAnAccountMayHaveOnlyOneSuspensionWaiting.
//
// `uq_suspension_reviews_one_pending`, and it is more than tidiness: two pending reviews would let
// one administrator approve the *other's* request while their own waited — the letter of a
// two-person rule with one person driving both halves.
//
// Refused by the index rather than by a check-then-insert, which two moderators opening one account
// at the same moment lose in practice rather than in theory.
func TestAnAccountMayHaveOnlyOneSuspensionWaiting(t *testing.T) {
	f := newAuditFixture(t)

	_, first := f.signedIn(t, "dup-a@example.com", RoleModerator, "10.0.66.6")
	_, second := f.signedIn(t, "dup-b@example.com", RoleModerator, "10.0.66.7")
	userID := newAccount(t, f.pool, "dup-subject@example.com", "+61400666", "provider")

	if status, body := requestSuspension(t, f, first, userID,
		"Failed to attend three consecutive scheduled pickups."); status != http.StatusAccepted {
		t.Fatalf("the first request: status = %d, want 202 (%s)", status, body)
	}

	status, body := requestSuspension(t, f, second, userID,
		"A second administrator asking for the same thing at the same time.")
	if status != http.StatusConflict {
		t.Fatalf("a second pending review was accepted: status = %d, want 409 (%s)", status, body)
	}
	if !strings.Contains(body, string(CodeSuspensionReviewOutstanding)) {
		t.Errorf("body = %s, want %q so the console sends them to the existing review",
			body, CodeSuspensionReviewOutstanding)
	}
}

// TestASettledReviewCannotBeApprovedTwice.
//
// The ordinary outcome of two moderators reading one queue. It matters because the second approval
// would otherwise overwrite the first administrator's name in the one row that records who agreed —
// and a two-person control whose record can be rewritten by a third party is not one.
func TestASettledReviewCannotBeApprovedTwice(t *testing.T) {
	f := newAuditFixture(t)

	_, requester := f.signedIn(t, "twice-a@example.com", RoleModerator, "10.0.66.8")
	firstAdmin, firstApprover := f.signedIn(t, "twice-b@example.com", RoleModerator, "10.0.66.9")
	_, secondApprover := f.signedIn(t, "twice-c@example.com", RoleModerator, "10.0.66.10")

	userID := newAccount(t, f.pool, "twice-subject@example.com", "+61400668", "provider")

	_, body := requestSuspension(t, f, requester, userID,
		"Two customers reported goods delivered to the wrong address in one week.")
	reviewID := reviewIDOf(t, body)

	if status, body := approveSuspension(t, f, firstApprover, reviewID); status != http.StatusOK {
		t.Fatalf("the first approval: status = %d, want 200 (%s)", status, body)
	}

	status, body := approveSuspension(t, f, secondApprover, reviewID)
	if status != http.StatusConflict {
		t.Fatalf("a settled review was approved again: status = %d, want 409 (%s)", status, body)
	}
	if !strings.Contains(body, string(CodeSuspensionReviewSettled)) {
		t.Errorf("body = %s, want %q", body, CodeSuspensionReviewSettled)
	}

	var approver uuid.UUID
	if err := f.pool.QueryRow(t.Context(),
		`SELECT approved_by FROM suspension_reviews WHERE id = $1`, reviewID).Scan(&approver); err != nil {
		t.Fatalf("reading the review back: %v", err)
	}
	if approver != firstAdmin.ID {
		t.Errorf("approved_by = %s, want the first approver %s — a second attempt rewrote the "+
			"record of who agreed", approver, firstAdmin.ID)
	}
}

// TestTheStandingEndpointNoLongerSuspends is the control being visible where it used to be absent.
//
// SHIP-161 suspended in one call and SHIP-166 took that away. **A console that kept calling the old
// endpoint must be told so rather than silently succeeding**, and the code is what tells it where to
// go — `admin_suspension_needs_review`, not a validation failure on the field, because `suspended` is
// a standing the platform has and nothing was mistyped.
//
// The other two directions still work in one call, which is checked here so that the refusal is
// demonstrably about suspension rather than about the endpoint.
func TestTheStandingEndpointNoLongerSuspends(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)
	userID := newAccount(t, f.pool, "old-route@example.com", "+61400669", "provider")

	status, body := f.setStanding(t, userID, "suspended",
		"Repeated policy breaches across three deliveries.")
	if status != http.StatusConflict {
		t.Fatalf("the standing endpoint suspended an account on one administrator's say-so: "+
			"status = %d, want 409 (%s)", status, body)
	}
	if !strings.Contains(body, string(CodeSuspensionNeedsReview)) {
		t.Errorf("body = %s, want %q so a console knows where to go", body, CodeSuspensionNeedsReview)
	}
	if s := standingOf(t, f.pool, userID); s != "active" {
		t.Fatalf("the account is %q, want active — the refusal did not prevent the change", s)
	}
	if entries := entriesFor(t, f.pool, userID); len(entries) != 0 {
		t.Errorf("a refused suspension wrote %d audit entries", len(entries))
	}

	if status, body := f.setStanding(t, userID, "restricted",
		"Insurance certificate expired; bidding paused until renewed."); status != http.StatusOK {
		t.Fatalf("restricting: status = %d, want 200 (%s)", status, body)
	}
	if status, body := f.setStanding(t, userID, "active",
		"Renewed certificate received and checked; restriction lifted."); status != http.StatusOK {
		t.Fatalf("reinstating: status = %d, want 200 (%s)", status, body)
	}
}

// TestAPendingReviewIsVisibleToASecondAdministrator.
//
// **Without this the control does not work.** A second administrator has to be able to find a request
// they did not make; a review nobody can see is a review nobody approves, which turns a two-person
// rule into an account that stays active because the queue was invisible.
//
// The queue needs `users.read`, which `support` holds — seeing that a suspension has been proposed is
// looking. Acting on it is the other permission, which `support` does not hold, and that is checked
// here too so the two are not confused.
func TestAPendingReviewIsVisibleToASecondAdministrator(t *testing.T) {
	f := newAuditFixture(t)

	_, requester := f.signedIn(t, "queue-a@example.com", RoleModerator, "10.0.66.11")
	_, supportToken := f.signedIn(t, "queue-support@example.com", RoleSupport, "10.0.66.12")

	userID := newAccount(t, f.pool, "queue-subject@example.com", "+614006611", "provider")

	_, body := requestSuspension(t, f, requester, userID,
		"Refused a safety inspection at the depot and left with the load.")
	reviewID := reviewIDOf(t, body)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/suspensions", nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+supportToken)
	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.PendingSuspensions()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("reading the queue: status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	var page struct {
		Data []suspensionReviewResponse `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatalf("decoding the queue: %v (%s)", err, rec.Body)
	}

	var found bool
	for _, review := range page.Data {
		if review.ID == reviewID {
			found = true
			if review.Status != ReviewPending.String() {
				t.Errorf("the queue carries a %q review", review.Status)
			}
			if review.Reason == "" {
				t.Error("the queue entry carries no reason, so the second administrator is " +
					"being asked to agree with nothing")
			}
		}
	}
	if !found {
		t.Fatalf("a pending review is not in the queue (%d entries).\n"+
			"A second administrator cannot approve what they cannot find.", len(page.Data))
	}

	// Looking is not acting. `support` holds users.read and not users.restrict.
	if status, body := approveSuspension(t, f, supportToken, reviewID); status != http.StatusForbidden {
		t.Errorf("a support administrator approved a suspension: status = %d, want 403 (%s)",
			status, body)
	}

	// And an approved review leaves the queue, so it is a queue rather than a log.
	_, approver := f.signedIn(t, "queue-b@example.com", RoleModerator, "10.0.66.13")
	if status, body := approveSuspension(t, f, approver, reviewID); status != http.StatusOK {
		t.Fatalf("approving: status = %d, want 200 (%s)", status, body)
	}

	after := httptest.NewRequest(http.MethodGet, "/v1/admin/suspensions", nil)
	after.Header.Set(httpx.HeaderAuthorization, "Bearer "+supportToken)
	afterRec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.PendingSuspensions()).ServeHTTP(afterRec, after)

	var afterPage struct {
		Data []suspensionReviewResponse `json:"data"`
	}
	if err := json.Unmarshal(afterRec.Body.Bytes(), &afterPage); err != nil {
		t.Fatalf("decoding the queue: %v (%s)", err, afterRec.Body)
	}
	for _, review := range afterPage.Data {
		if review.ID == reviewID {
			t.Error("an approved review is still in the pending queue, so the queue never empties")
		}
	}
}

// TestARequestIsRefusedWithoutAReasonOrAnAccount.
//
// The reason is required at the *request* rather than at the approval, because the second
// administrator is being asked to agree with a stated case — a request carrying none would make the
// control a formality, somebody clicking approve on a proposition nobody made.
func TestARequestIsRefusedWithoutAReasonOrAnAccount(t *testing.T) {
	f := newAuditFixture(t)
	_, token := f.signedIn(t, "bad-req@example.com", RoleModerator, "10.0.66.14")
	userID := newAccount(t, f.pool, "bad-req-subject@example.com", "+614006614", "provider")

	for name, reason := range map[string]string{
		"no reason at all":              "",
		"a reason that records nothing": "bad",
		"a reason of whitespace":        "            ",
	} {
		t.Run(name, func(t *testing.T) {
			status, body := requestSuspension(t, f, token, userID, reason)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", status, body)
			}
			if !strings.Contains(body, `"reason"`) {
				t.Errorf("the refusal does not name the field: %s", body)
			}
		})
	}

	t.Run("an account that does not exist", func(t *testing.T) {
		status, body := requestSuspension(t, f, token, uuid.New(),
			"Reported for repeated no-shows across three jobs.")
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (%s)", status, body)
		}
	})

	t.Run("a review that does not exist", func(t *testing.T) {
		status, body := approveSuspension(t, f, token, uuid.New().String())
		if status != http.StatusNotFound {
			t.Fatalf("status = %d, want 404 (%s)", status, body)
		}
	})
}

// TestASuspensionReviewWithNoDatabaseSaysSoRatherThanSucceeding.
//
// The worst available failure for a control is one that appears to work. An unreachable database
// answers 503 on every path here rather than reporting an empty queue or a completed review.
func TestASuspensionReviewWithNoDatabaseSaysSoRatherThanSucceeding(t *testing.T) {
	auditor, err := NewAuditor(testClock())
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	suspensions, err := NewSuspensions(auditor, nil)
	if err != nil {
		t.Fatalf("building the suspension review with no pool: %v", err)
	}

	if _, err := suspensions.Request(t.Context(), SuspensionRequest{
		UserID: uuid.New(), ActorID: uuid.New(),
		Reason: "Repeated policy breaches across three deliveries.",
	}); err == nil {
		t.Error("a request with no database succeeded")
	}

	if _, err := suspensions.Approve(t.Context(), SuspensionApproval{
		ReviewID: uuid.New(), ActorID: uuid.New(),
	}); err == nil {
		t.Error("an approval with no database succeeded")
	}

	if reviews, err := suspensions.Pending(t.Context(), 20); err == nil {
		t.Errorf("an unreachable database reported %d pending reviews rather than failing",
			len(reviews))
	}

	if _, err := NewSuspensions(nil, nil); err == nil {
		t.Error("a suspension review was built with no audit writer, so a control could be " +
			"exercised with no record of who exercised it")
	}
}
