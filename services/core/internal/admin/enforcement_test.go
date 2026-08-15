package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
)

// SHIP-160 and SHIP-161 against a real PostgreSQL, through the real transition guard.
//
// # Why the guard is real here rather than stubbed
//
// The interesting half of SHIP-160 is *which jobs can be unpublished*, and that is Docs/02 §2's
// transition table plus `000402`'s trigger — neither of which a stub has. [testJobs] performs the
// move through `jobs.Service` exactly as cmd/api's adapter does, so an awarded job is refused
// because the document says so rather than because this file said so.
//
// # And why the audit entry is checked on every one of them
//
// Both actions take something away from somebody. SHIP-150's rule is that the entry commits with the
// action or neither happens, and the direction that matters is the negative one: a refused action
// must leave **nothing**, in a table with no way to take it back.
//
// [TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten] is the test wave 9 found missing
// across three packages — every existing test drives a database where the insert succeeds, so the
// error branch had never been taken once.

// enforcementFixture is the enforcement service, a handler, and a signed-in administrator of the
// role the test needs.
type enforcementFixture struct {
	pool    *pgxpool.Pool
	creds   *Credentials
	auth    *Authenticator
	clk     *clock.Fixed
	handler *Handler
	enforce *Enforcement

	// admin is the moderator most tests act as, with a live session.
	admin Administrator
	token string
}

func newEnforcementFixture(t *testing.T, role Role) enforcementFixture {
	t.Helper()

	creds, auth, pool, clk := adminAuth(t)

	auditor, err := NewAuditor(clk)
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	// The real guard, with a real outbox — so the `job.status_changed` event that ends up
	// notifying the customer is written by the same transaction the test is watching.
	lifecycle := testJobs{svc: jobs.NewService(events.NewOutbox(), clk, nil)}

	enforce, err := NewEnforcement(lifecycle, auditor, pool)
	if err != nil {
		t.Fatalf("building enforcement: %v", err)
	}

	services := testServices(t, creds, pool, clk)
	services.Enforcement = enforce

	handler, err := NewHandler(services, pool, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	administrator := anAdministrator(t, creds, string(role)+"-enforcer@example.com", role)
	issued, _, err := signIn(t, creds, string(role)+"-enforcer@example.com", testPassword, "10.0.90.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	return enforcementFixture{
		pool: pool, creds: creds, auth: auth, clk: clk,
		handler: handler, enforce: enforce,
		admin: administrator, token: issued.Token,
	}
}

// openJob is a published job nobody has bid on, moved there through the guard.
func (f enforcementFixture) openJob(t *testing.T, suffix string) (jobID, customerID uuid.UUID) {
	t.Helper()

	customerID = newAccount(t, f.pool, "cust-"+suffix+"@example.com", "+6140000"+suffix, "customer")
	jobID = newDraft(t, f.pool, customerID)
	moveJob(t, f.pool, jobID, jobs.User(jobs.ActorCustomer, customerID), jobs.StatusOpen)
	return jobID, customerID
}

// unpublish drives POST /v1/admin/jobs/{id}/unpublish through the guard and the handler.
func (f enforcementFixture) unpublish(t *testing.T, jobID uuid.UUID, reason string) (int, string) {
	t.Helper()

	body, err := json.Marshal(unpublishJobRequest{Reason: reason})
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/v1/admin/jobs/"+jobID.String()+"/unpublish", strings.NewReader(string(body)))
	req.SetPathValue("id", jobID.String())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.UnpublishJob()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// setStanding drives POST /v1/admin/users/{id}/standing through the guard and the handler.
func (f enforcementFixture) setStanding(t *testing.T, userID uuid.UUID, standing, reason string) (int, string) {
	t.Helper()

	body, err := json.Marshal(setStandingRequest{Standing: standing, Reason: reason})
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/v1/admin/users/"+userID.String()+"/standing", strings.NewReader(string(body)))
	req.SetPathValue("id", userID.String())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.SetStanding()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// statusOf reads a job's current status.
func statusOf(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) string {
	t.Helper()

	var status string
	if err := pool.QueryRow(t.Context(),
		`SELECT status FROM jobs WHERE id = $1`, jobID).Scan(&status); err != nil {
		t.Fatalf("reading the status of %s: %v", jobID, err)
	}
	return status
}

// standingOf reads an account's current standing.
func standingOf(t *testing.T, pool *pgxpool.Pool, userID uuid.UUID) string {
	t.Helper()

	var standing string
	if err := pool.QueryRow(t.Context(),
		`SELECT status FROM users WHERE id = $1`, userID).Scan(&standing); err != nil {
		t.Fatalf("reading the standing of %s: %v", userID, err)
	}
	return standing
}

// entriesFor is every audit entry naming this target, newest first.
func entriesFor(t *testing.T, pool *pgxpool.Pool, target uuid.UUID) []storedEntry {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT id, actor_type, actor_id, action, target_type, target_id, reason, metadata, created_at
		FROM audit_log WHERE target_id = $1 ORDER BY created_at DESC, id DESC`, target)
	if err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	defer rows.Close()

	var out []storedEntry
	for rows.Next() {
		var e storedEntry
		var metadata []byte
		if err := rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &e.TargetType,
			&e.TargetID, &e.Reason, &metadata, &e.CreatedAt); err != nil {
			t.Fatalf("reading an audit entry: %v", err)
		}
		if err := json.Unmarshal(metadata, &e.Metadata); err != nil {
			t.Fatalf("decoding the metadata: %v", err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the audit log: %v", err)
	}
	return out
}

// --- SHIP-160 ------------------------------------------------------------------------------------

// TestUnpublishingAnOpenJobCancelsItAndRecordsWhyInTwoPlaces is SHIP-160's *Done when*, less the
// notification, which has its own test below.
//
// "A policy-breaching job is removed with a recorded reason." Removed means `Cancelled` through the
// one guarded function — there is no `unpublished` status and no hidden flag, because a second way
// for a job to be off the marketplace is a second thing every feed and every sweep would have to
// know about.
//
// **The reason is written twice on purpose.** `job_status_history` says why the *job* moved, which
// is what a customer's support conversation reads; `audit_log` says what the *administrator* did,
// which is what Docs/04 §9's controls read. A reader of either should not have to find the other.
func TestUnpublishingAnOpenJobCancelsItAndRecordsWhyInTwoPlaces(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)
	jobID, _ := f.openJob(t, "160a")

	const reason = "Listing offers to transport a live animal, which Docs/05 prohibits."

	status, body := f.unpublish(t, jobID, reason)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var got unpublishJobResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decoding the response: %v (%s)", err, body)
	}
	if got.Status != "Cancelled" {
		t.Errorf("status = %q, want Cancelled — Docs/02 §2 has one row for this and it is "+
			"Open → Cancelled, admin may intervene", got.Status)
	}

	if s := statusOf(t, f.pool, jobID); s != "Cancelled" {
		t.Fatalf("the job is %q, want Cancelled", s)
	}

	// The transition went through the guard, attributed to an administrator, with the reason.
	var (
		actorType, historyReason string
		actorID                  uuid.UUID
	)
	if err := f.pool.QueryRow(t.Context(), `
		SELECT actor_type, actor_id, coalesce(reason, '')
		FROM job_status_history
		WHERE job_id = $1 AND to_status = 'Cancelled'`, jobID,
	).Scan(&actorType, &actorID, &historyReason); err != nil {
		t.Fatalf("reading the transition: %v", err)
	}
	if actorType != "admin" {
		t.Errorf("actor_type = %q, want admin — a job removed by the platform's staff must not "+
			"read as one its customer cancelled", actorType)
	}
	if actorID != f.admin.ID {
		t.Errorf("actor_id = %s, want the administrator who acted, %s", actorID, f.admin.ID)
	}
	if historyReason != reason {
		t.Errorf("the transition's reason is %q, want %q", historyReason, reason)
	}

	// And the audit entry, which is the other reader.
	entries := entriesFor(t, f.pool, jobID)
	if len(entries) != 1 {
		t.Fatalf("the job has %d audit entries, want exactly 1", len(entries))
	}
	e := entries[0]
	if e.Action != AuditActionJobUnpublished.String() {
		t.Errorf("action = %q, want %q", e.Action, AuditActionJobUnpublished)
	}
	if e.TargetType != AuditTargetJob {
		t.Errorf("target_type = %q, want %q", e.TargetType, AuditTargetJob)
	}
	if e.ActorID == nil || *e.ActorID != f.admin.ID {
		t.Errorf("actor_id = %v, want %s", e.ActorID, f.admin.ID)
	}
	if e.Reason == nil || *e.Reason != reason {
		t.Errorf("the entry's reason is %v, want %q", e.Reason, reason)
	}
	if !e.CreatedAt.Equal(f.clk.Now()) {
		t.Errorf("created_at = %s, want the injected %s — Docs/11 §9, one row one clock",
			e.CreatedAt.UTC(), f.clk.Now())
	}
}

// TestUnpublishingEmitsTheStatusChangeThatNotifiesTheCustomer is the last clause of the *Done when*.
//
// **No new domain event was added, and that is the finding rather than a shortcut.** The guarded
// transition emits `job.status_changed` in this transaction, and `notifications.StatusRules` already
// routes a move to `Cancelled` to the job's customer and the awarded provider by email, under "A job
// has been cancelled." An `admin.job_unpublished` event would have been a *second* announcement of
// one state change — precisely the failure those rules are written to prevent, where a recipient who
// learns the platform emails twice starts ignoring the first one.
//
// This test asserts the event exists with the right `to`, which is the fact the routing rule keys
// on. That the rule then routes it is `internal/notifications`' own test, and the two meet in
// cmd/api's exhaustiveness check.
func TestUnpublishingEmitsTheStatusChangeThatNotifiesTheCustomer(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)
	jobID, _ := f.openJob(t, "160b")

	if status, body := f.unpublish(t, jobID, "Prohibited goods; see the moderation queue."); status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var (
		eventType string
		payload   []byte
	)
	if err := f.pool.QueryRow(t.Context(), `
		SELECT event_type, payload FROM outbox
		WHERE aggregate_id = $1 ORDER BY occurred_at DESC LIMIT 1`, jobID,
	).Scan(&eventType, &payload); err != nil {
		t.Fatalf("reading the outbox: %v", err)
	}

	if eventType != "job.status_changed" {
		t.Fatalf("event_type = %q, want job.status_changed — the notification the customer gets "+
			"is a consequence of the move, not a second event this domain emits", eventType)
	}

	var got struct {
		To        string `json:"to"`
		ActorType string `json:"actor_type"`
		Reason    string `json:"reason"`
	}
	if err := json.Unmarshal(payload, &got); err != nil {
		t.Fatalf("decoding the payload: %v", err)
	}
	if got.To != "Cancelled" {
		t.Errorf("the event says the job moved to %q; notifications.StatusRules keys on this "+
			"exact value to decide who hears about it", got.To)
	}
	if got.ActorType != "admin" {
		t.Errorf("actor_type = %q, want admin", got.ActorType)
	}
	if got.Reason == "" {
		t.Error("the event carries no reason, so a consumer cannot say why the job ended")
	}
}

// TestAnAwardedJobCannotBeUnpublishedAndNothingIsWritten.
//
// Docs/02 §2 offers no route from Awarded to Cancelled: a provider has committed and may have
// travelled, and Docs/02 §6.2 makes ending it after that a support case. The administrator's path is
// a dispute they then resolve (SHIP-164), which records both sides.
//
// **The "nothing is written" half is the one worth having.** A 409 with an audit entry behind it
// would record a removal that did not happen, in a table that cannot be corrected.
func TestAnAwardedJobCannotBeUnpublishedAndNothingIsWritten(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)

	jobID, customerID := f.openJob(t, "160c")
	provider := newAccount(t, f.pool, "prov-160c@example.com", "+61400160c", "provider")
	acceptBid(t, f.pool, jobID, provider)
	moveJob(t, f.pool, jobID, jobs.User(jobs.ActorCustomer, customerID), jobs.StatusAwarded)

	status, body := f.unpublish(t, jobID, "Prohibited goods reported after the award.")
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", status, body)
	}
	if !strings.Contains(body, string(CodeJobNotUnpublishable)) {
		t.Errorf("body = %s, want %q", body, CodeJobNotUnpublishable)
	}

	if s := statusOf(t, f.pool, jobID); s != "Awarded" {
		t.Errorf("the job is %q, want Awarded — a refused removal must not move it", s)
	}
	if entries := entriesFor(t, f.pool, jobID); len(entries) != 0 {
		t.Errorf("a refused removal wrote %d audit entries; an entry for an action that did not "+
			"happen cannot be taken back", len(entries))
	}
}

// TestUnpublishingAJobThatIsAlreadyGoneSaysSoRatherThanRefusingGenerically.
//
// The ordinary outcome of two moderators reading the same queue, and it wants a different answer
// from "this cannot be done": reload and see who removed it.
func TestUnpublishingAJobThatIsAlreadyGoneSaysSoRatherThanRefusingGenerically(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)
	jobID, _ := f.openJob(t, "160d")

	if status, body := f.unpublish(t, jobID, "Prohibited goods; removed after review."); status != http.StatusOK {
		t.Fatalf("first removal: status = %d, want 200 (%s)", status, body)
	}

	status, body := f.unpublish(t, jobID, "Prohibited goods; removed after review.")
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", status, body)
	}
	if !strings.Contains(body, string(CodeJobAlreadyUnpublished)) {
		t.Errorf("body = %s, want %q — a job somebody else already removed is a different answer "+
			"from one that cannot be removed", body, CodeJobAlreadyUnpublished)
	}

	// Still exactly one entry. A second removal that wrote a second entry would make the trail
	// say the job was taken down twice.
	if entries := entriesFor(t, f.pool, jobID); len(entries) != 1 {
		t.Errorf("the job has %d audit entries after two attempts, want 1", len(entries))
	}
}

// TestARemovalWithNoRealReasonIsRefusedAndNamesTheField.
//
// "With a recorded reason" is the *Done when*, and an empty string satisfies a required field
// without recording anything — as does a single character, which is worse, because in the trail it
// looks exactly like a reason.
func TestARemovalWithNoRealReasonIsRefusedAndNamesTheField(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)

	for _, tc := range []struct{ name, reason string }{
		{"empty", ""},
		{"whitespace", "     "},
		{"too short to record anything", "spam"},
		{"longer than the field holds", strings.Repeat("x", maxReasonLength+1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			jobID, _ := f.openJob(t, "160e"+strings.ReplaceAll(tc.name, " ", "")[:4])

			status, body := f.unpublish(t, jobID, tc.reason)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", status, body)
			}
			if !strings.Contains(body, `"reason"`) {
				t.Errorf("the refusal does not name the field: %s", body)
			}
			if s := statusOf(t, f.pool, jobID); s != "Open" {
				t.Errorf("the job is %q, want Open — a refused removal must not move it", s)
			}
			if entries := entriesFor(t, f.pool, jobID); len(entries) != 0 {
				t.Errorf("a refused removal wrote %d audit entries", len(entries))
			}
		})
	}
}

// TestSupportMayOpenAJobAndMayNotRemoveOne is Docs/04 §9's least-privilege control, as two
// permissions on two endpoints.
//
// `support` holds `jobs.read` and not `jobs.unpublish`. The refusal names no permission — that goes
// to the log — and, critically, writes nothing.
func TestSupportMayOpenAJobAndMayNotRemoveOne(t *testing.T) {
	f := newEnforcementFixture(t, RoleSupport)
	jobID, _ := f.openJob(t, "160f")

	status, body := f.unpublish(t, jobID, "Prohibited goods; escalating for removal.")
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", status, body)
	}
	if !strings.Contains(body, string(CodeAdminPermissionDenied)) {
		t.Errorf("body = %s, want %q", body, CodeAdminPermissionDenied)
	}
	if strings.Contains(body, PermissionJobsUnpublish.String()) {
		t.Errorf("the refusal names the permission it wanted: %s", body)
	}

	if s := statusOf(t, f.pool, jobID); s != "Open" {
		t.Errorf("the job is %q, want Open", s)
	}
	if entries := entriesFor(t, f.pool, jobID); len(entries) != 0 {
		t.Errorf("a refused removal wrote %d audit entries", len(entries))
	}
}

// --- SHIP-161 ------------------------------------------------------------------------------------

// TestSuspendingAnAccountRecordsBothEndsAndWhy is SHIP-161's *Done when*.
//
// "Account access is limited or disabled with a recorded reason." The column is `users.status`,
// which has existed since `000002`; what this ticket adds is the administrative act, the reason and
// the entry.
//
// **Both ends are recorded, never just the new one.** `users` keeps no version of its own and the
// trail is append-only, so an entry saying only "restricted" could never afterwards be joined to
// what the account held before.
func TestSuspendingAnAccountRecordsBothEndsAndWhy(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)
	userID := newAccount(t, f.pool, "susp-161a@example.com", "+61400161a", "provider")

	const reason = "Two unresolved no-shows in a fortnight; see the delivery exception queue."

	status, body := f.setStanding(t, userID, "suspended", reason)
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}

	var got standingResponse
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("decoding the response: %v (%s)", err, body)
	}
	if got.From != "active" || got.To != "suspended" {
		t.Errorf("the response says %q → %q, want active → suspended; a console rendering only "+
			"the new standing cannot tell a tightening from a loosening", got.From, got.To)
	}

	if s := standingOf(t, f.pool, userID); s != "suspended" {
		t.Fatalf("the account is %q, want suspended", s)
	}

	entries := entriesFor(t, f.pool, userID)
	if len(entries) != 1 {
		t.Fatalf("the account has %d audit entries, want exactly 1", len(entries))
	}
	e := entries[0]
	if e.Action != AuditActionUserStandingChanged.String() {
		t.Errorf("action = %q, want %q", e.Action, AuditActionUserStandingChanged)
	}
	if e.TargetType != AuditTargetUser {
		t.Errorf("target_type = %q, want %q — a customer is not an administrator, and one target "+
			"kind for both would make 'everything done to this administrator' return customers",
			e.TargetType, AuditTargetUser)
	}
	if e.Reason == nil || *e.Reason != reason {
		t.Errorf("the entry's reason is %v, want %q", e.Reason, reason)
	}
	if e.Metadata["from"] != "active" || e.Metadata["to"] != "suspended" {
		t.Errorf("metadata = %v, want both ends of the change", e.Metadata)
	}
}

// TestRestrictingAndThenReinstatingIsOneEndpointAndOneAction.
//
// Reinstatement is the same route, the same permission and the same audit action with the direction
// in its metadata. A separate reinstate endpoint would be a second place for the reason to become
// optional — and the trail that records a restriction but not its reversal is the one that makes a
// person look permanently suspect.
func TestRestrictingAndThenReinstatingIsOneEndpointAndOneAction(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)
	userID := newAccount(t, f.pool, "reinst-161b@example.com", "+61400161b", "provider")

	if status, body := f.setStanding(t, userID, "restricted",
		"Insurance certificate expired; bidding paused until renewed."); status != http.StatusOK {
		t.Fatalf("restricting: status = %d, want 200 (%s)", status, body)
	}
	if s := standingOf(t, f.pool, userID); s != "restricted" {
		t.Fatalf("the account is %q, want restricted", s)
	}

	if status, body := f.setStanding(t, userID, "active",
		"Renewed certificate received and checked; restriction lifted."); status != http.StatusOK {
		t.Fatalf("reinstating: status = %d, want 200 (%s)", status, body)
	}
	if s := standingOf(t, f.pool, userID); s != "active" {
		t.Fatalf("the account is %q, want active", s)
	}

	entries := entriesFor(t, f.pool, userID)
	if len(entries) != 2 {
		t.Fatalf("the account has %d audit entries, want 2", len(entries))
	}
	for _, e := range entries {
		if e.Action != AuditActionUserStandingChanged.String() {
			t.Errorf("action = %q, want %q — one action for every direction, so that "+
				"'what has happened to this account's standing' is one filter",
				e.Action, AuditActionUserStandingChanged)
		}
		if e.Reason == nil || *e.Reason == "" {
			t.Error("a standing change was recorded with no reason; reinstatement needs one too")
		}
	}

	// Newest first, so this is the reinstatement.
	if entries[0].Metadata["from"] != "restricted" || entries[0].Metadata["to"] != "active" {
		t.Errorf("the reinstatement's metadata is %v, want restricted → active", entries[0].Metadata)
	}
}

// TestSettingTheStandingAnAccountAlreadyHoldsIsRefused.
//
// An entry saying "changed from suspended to suspended" is noise in the one table whose value is
// that everything in it happened. The console's right response is to reload — most often because
// another administrator got there first, which the trail will show.
func TestSettingTheStandingAnAccountAlreadyHoldsIsRefused(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)
	userID := newAccount(t, f.pool, "noop-161c@example.com", "+61400161c", "customer")

	status, body := f.setStanding(t, userID, "active", "Confirming this account is in good standing.")
	if status != http.StatusConflict {
		t.Fatalf("status = %d, want 409 (%s)", status, body)
	}
	if !strings.Contains(body, string(CodeUserStandingUnchanged)) {
		t.Errorf("body = %s, want %q", body, CodeUserStandingUnchanged)
	}
	if entries := entriesFor(t, f.pool, userID); len(entries) != 0 {
		t.Errorf("a no-op wrote %d audit entries", len(entries))
	}
}

// TestEveryBadStandingRequestIsRefusedAndNamesItsField.
//
// A standing outside `ck_users_status`s three, and a reason that records nothing. Both are 422 in
// validate.Errors' shape, because both are fields somebody typed.
func TestEveryBadStandingRequestIsRefusedAndNamesItsField(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)

	for i, tc := range []struct{ name, standing, reason, field string }{
		{"a standing the platform does not have", "banned", "Repeated policy breaches on delivery.", "standing"},
		{"no reason at all", "suspended", "", "reason"},
		{"a reason that records nothing", "suspended", "bad", "reason"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// The index rather than the case's own fields, because two of the three differ
			// only in the reason — and `uq_users_email` refuses the second, which reads as a
			// failure of the endpoint rather than of the fixture.
			suffix := strconv.Itoa(i)
			userID := newAccount(t, f.pool,
				"bad-standing-"+suffix+"@example.com", "+614001610"+suffix, "customer")

			status, body := f.setStanding(t, userID, tc.standing, tc.reason)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", status, body)
			}
			if !strings.Contains(body, `"`+tc.field+`"`) {
				t.Errorf("the refusal does not name %q: %s", tc.field, body)
			}
			if s := standingOf(t, f.pool, userID); s != "active" {
				t.Errorf("the account is %q, want active — a refused change must not apply", s)
			}
			if entries := entriesFor(t, f.pool, userID); len(entries) != 0 {
				t.Errorf("a refused change wrote %d audit entries", len(entries))
			}
		})
	}
}

// TestAnAccountThatDoesNotExistIsAPlain404.
//
// Disclosed plainly, unlike the 404 on dispute intake. The caller is an administrator holding
// `users.restrict`, every account is theirs to act on, and there is nothing being kept from them —
// the disclosure decision on the customer-facing endpoint is about a *customer* learning that
// somebody else's job exists.
func TestAnAccountThatDoesNotExistIsAPlain404(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)

	status, body := f.setStanding(t, uuid.New(), "suspended",
		"Reported for repeated no-shows across three jobs.")
	if status != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (%s)", status, body)
	}
}

// TestSupportMaySearchAccountsAndMayNotRestrictOne, the other half of the least-privilege control.
func TestSupportMaySearchAccountsAndMayNotRestrictOne(t *testing.T) {
	f := newEnforcementFixture(t, RoleSupport)
	userID := newAccount(t, f.pool, "perm-161d@example.com", "+61400161d", "provider")

	status, body := f.setStanding(t, userID, "suspended",
		"Reported for repeated no-shows; escalating to a moderator.")
	if status != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (%s)", status, body)
	}
	if strings.Contains(body, PermissionUsersRestrict.String()) {
		t.Errorf("the refusal names the permission it wanted: %s", body)
	}
	if s := standingOf(t, f.pool, userID); s != "active" {
		t.Errorf("the account is %q, want active", s)
	}
	if entries := entriesFor(t, f.pool, userID); len(entries) != 0 {
		t.Errorf("a refused change wrote %d audit entries", len(entries))
	}
}

// --- the branch three packages of tests had never taken -------------------------------------------

// TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten.
//
// # Why this test exists at all
//
// SHIP-150's design is that the audit entry commits with the action or neither happens: the writer
// takes the caller's transaction and its error is returned rather than logged and swallowed. Wave 9
// found that **an `Auditor.Record` whose error was ignored passed `internal/admin`, `cmd/api` and
// `migrations`, all green** — because every test in all three drives a database where the insert
// succeeds, so the error branch was never taken once. A guarantee no test can take the failing
// branch of is a comment, not a control.
//
// # The database-refusal arm does not discriminate, and finding that out is half this test
//
// The obvious instrument is a `BEFORE INSERT` trigger on `audit_log` that raises, and it is
// [TestATriggerRefusalRollsTheWholeActionBack] below. **Mutating the swallowed error does not make
// that test fail**, which was measured rather than assumed: PostgreSQL aborts the whole transaction
// as soon as a statement in it raises, so every later statement fails and the COMMIT reports the
// abort. The error reaches the caller whether or not the code checks it. That test is still worth
// having — it establishes the rollback — but it says nothing about the `if err != nil`.
//
// # So this one fails the write *before any SQL is issued*
//
// A nil auditor. [Auditor.Record] refuses a nil receiver and returns without touching the database,
// so the transaction stays perfectly healthy and a swallowed error commits the mutation on its own —
// which is exactly the wave-9 defect, reproduced. Constructed by hand rather than through
// [NewEnforcement], which refuses one; audit.go's own note says reaching that branch means "something
// built the service by hand", and this is a test doing precisely that on purpose.
//
// # What it asserts
//
// Not merely that an error came back. It asserts the **job did not move** and **no transition was
// recorded**, which is the actual claim — an implementation that returned the error after committing
// would pass the first check and fail these.
//
// Both actions are covered, because they are two call sites and only one of them can be wrong.
func TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)

	// Built by hand, past the constructor's refusal. See the note above.
	unwritable := &Enforcement{
		jobs:    testJobs{svc: jobs.NewService(events.NewOutbox(), f.clk, nil)},
		auditor: nil,
		pool:    f.pool,
	}

	t.Run("a job is not unpublished", func(t *testing.T) {
		jobID, _ := f.openJob(t, "nomut1")

		err := unwritable.Unpublish(t.Context(), UnpublishCommand{
			JobID:   jobID,
			ActorID: f.admin.ID,
			Reason:  "Prohibited goods; removed after review of the listing.",
		})
		if err == nil {
			t.Fatal("the job was unpublished with no audit entry behind it.\n" +
				"SHIP-150: the entry commits with the action or neither happens. An action " +
				"that succeeded with no record of it is the backfill nobody can perform " +
				"(Docs/09 puts audit on the do-not-cut list for exactly this reason).")
		}

		if s := statusOf(t, f.pool, jobID); s != "Open" {
			t.Errorf("the job is %q, want Open — the error was returned and the transaction "+
				"was still committed, which is the same defect one statement later", s)
		}

		var transitions int
		if err := f.pool.QueryRow(t.Context(),
			`SELECT count(*) FROM job_status_history WHERE job_id = $1 AND to_status = 'Cancelled'`,
			jobID).Scan(&transitions); err != nil {
			t.Fatalf("reading the transitions: %v", err)
		}
		if transitions != 0 {
			t.Errorf("%d cancellation rows survived an unwritable audit trail; the history says "+
				"the job was removed and nothing says who removed it", transitions)
		}
	})

	t.Run("an account's standing is not changed", func(t *testing.T) {
		userID := newAccount(t, f.pool, "nomut2@example.com", "+61400161m2", "provider")

		_, err := unwritable.SetStanding(t.Context(), StandingCommand{
			UserID:   userID,
			ActorID:  f.admin.ID,
			Standing: StandingSuspended,
			Reason:   "Repeated policy breaches across three deliveries.",
		})
		if err == nil {
			t.Fatal("an account was suspended with no audit entry behind it")
		}
		if s := standingOf(t, f.pool, userID); s != "active" {
			t.Errorf("the account is %q, want active — an account disabled with no record of "+
				"who disabled it is one nobody can justify keeping disabled", s)
		}
	})
}

// TestATriggerRefusalRollsTheWholeActionBack is the database-refusal half.
//
// **Weaker than the test above and kept deliberately**, because it establishes something that one
// does not: that when the *database* refuses the entry — a full disk, a permission change, a
// constraint somebody adds — the mutation rolls back with it rather than committing alone.
//
// It does not establish that the `if err != nil` is present, and the note above records why: a
// raised exception aborts the whole PostgreSQL transaction, so the failure reaches the caller
// through the COMMIT whether the code checks the error or not. Keeping the two apart is the point —
// a reader should know which claim each one supports.
//
// The trigger is confined to this test's own cloned database (pgtest gives every test one), so it
// cannot leak into another test or another worktree.
func TestATriggerRefusalRollsTheWholeActionBack(t *testing.T) {
	f := newEnforcementFixture(t, RoleModerator)

	// Created after the fixture has signed in, because signing in writes an audit entry of its
	// own and would otherwise fail here.
	if _, err := f.pool.Exec(t.Context(), `
		CREATE FUNCTION refuse_audit_insert() RETURNS trigger AS $$
		BEGIN
			RAISE EXCEPTION 'the audit trail is unwritable';
		END;
		$$ LANGUAGE plpgsql;

		CREATE TRIGGER audit_log_refuse_insert
			BEFORE INSERT ON audit_log
			FOR EACH ROW EXECUTE FUNCTION refuse_audit_insert();`); err != nil {
		t.Fatalf("making the audit trail unwritable: %v", err)
	}

	t.Run("a job is not unpublished", func(t *testing.T) {
		jobID, _ := f.openJob(t, "trig1")

		err := f.enforce.Unpublish(t.Context(), UnpublishCommand{
			JobID:   jobID,
			ActorID: f.admin.ID,
			Reason:  "Prohibited goods; removed after review of the listing.",
		})
		if err == nil {
			t.Fatal("the job was unpublished with no audit entry behind it.\n" +
				"SHIP-150: the entry commits with the action or neither happens. An action " +
				"that succeeded with no record of it is the backfill nobody can perform " +
				"(Docs/09 puts audit on the do-not-cut list for exactly this reason).")
		}

		if s := statusOf(t, f.pool, jobID); s != "Open" {
			t.Errorf("the job is %q, want Open — the error was returned but the transaction "+
				"was not rolled back, which is the same defect one statement later", s)
		}

		var transitions int
		if err := f.pool.QueryRow(t.Context(),
			`SELECT count(*) FROM job_status_history WHERE job_id = $1 AND to_status = 'Cancelled'`,
			jobID).Scan(&transitions); err != nil {
			t.Fatalf("reading the transitions: %v", err)
		}
		if transitions != 0 {
			t.Errorf("%d cancellation rows survived a failed audit write; the history says the "+
				"job was removed and nothing says who removed it", transitions)
		}
	})

	t.Run("an account's standing is not changed", func(t *testing.T) {
		userID := newAccount(t, f.pool, "trig2@example.com", "+61400161t2", "provider")

		_, err := f.enforce.SetStanding(t.Context(), StandingCommand{
			UserID:   userID,
			ActorID:  f.admin.ID,
			Standing: StandingSuspended,
			Reason:   "Repeated policy breaches across three deliveries.",
		})
		if err == nil {
			t.Fatal("an account was suspended with no audit entry behind it")
		}

		if s := standingOf(t, f.pool, userID); s != "active" {
			t.Errorf("the account is %q, want active — an account disabled with no record of "+
				"who disabled it is one nobody can justify keeping disabled", s)
		}
	})
}

// TestEnforcementRefusesToBeBuiltWithoutWhatItNeeds.
//
// The auditor check is the one worth writing out. [Auditor.Record] does refuse a nil receiver, but it
// refuses at the moment somebody exercises a privileged action — which is exactly when a service
// must not be discovering its own wiring. The composition root decides this once, at startup.
func TestEnforcementRefusesToBeBuiltWithoutWhatItNeeds(t *testing.T) {
	auditor, err := NewAuditor(clock.NewFixed(testInstant))
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	if _, err := NewEnforcement(nil, auditor, nil); err == nil {
		t.Error("enforcement was built with no job lifecycle behind it")
	}
	if _, err := NewEnforcement(staticJobs{}, nil, nil); err == nil {
		t.Error("enforcement was built with no audit writer, so a privileged action could " +
			"succeed leaving no record")
	}
}

// TestEveryEnforcementActionRefusesWhenTheDatabaseIsUnreachable.
//
// The pool may be nil — the process starts with an unreachable database on purpose so a failover
// does not take the fleet down — and the answer is 503 for as long as it lasts, not a panic.
func TestEveryEnforcementActionRefusesWhenTheDatabaseIsUnreachable(t *testing.T) {
	auditor, err := NewAuditor(clock.NewFixed(testInstant))
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}
	enforce, err := NewEnforcement(staticJobs{move: JobMoved}, auditor, nil)
	if err != nil {
		t.Fatalf("building enforcement: %v", err)
	}

	unpublishErr := enforce.Unpublish(context.Background(), UnpublishCommand{
		JobID: uuid.New(), ActorID: uuid.New(),
		Reason: "Prohibited goods; removed after review.",
	})
	if !errors.Is(unpublishErr, ErrAdminUnavailable) {
		t.Errorf("unpublishing with no database: %v, want %v", unpublishErr, ErrAdminUnavailable)
	}

	_, standingErr := enforce.SetStanding(context.Background(), StandingCommand{
		UserID: uuid.New(), ActorID: uuid.New(), Standing: StandingSuspended,
		Reason: "Repeated policy breaches across three deliveries.",
	})
	if !errors.Is(standingErr, ErrAdminUnavailable) {
		t.Errorf("changing a standing with no database: %v, want %v", standingErr, ErrAdminUnavailable)
	}
}
