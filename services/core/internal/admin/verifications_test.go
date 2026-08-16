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
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/profiles"
)

// SHIP-153 and SHIP-154 against a real PostgreSQL, through the real guarded transition.
//
// # The port is `internal/profiles` itself, not a double, and that is the point
//
// The interesting half of SHIP-154 is *what a decision does to the database*: a decision row written
// before the state moves, a trigger that refuses any move no decision describes, and an `audit_log`
// entry in the same transaction. A double has none of them, so a suite written against one would
// pass with the whole guard removed — which is precisely the mutation this branch was asked to run.
//
// A test file may import another domain. The boundary lint skips `_test.go` deliberately, and
// records why: "a test wires domains together in the same way cmd/api does". [testVerifications] is
// therefore the same adapter `cmd/api/routes_admin.go` registers, written twice rather than shared,
// because package main is not importable and no test in it has a database to reach.
//
// # Why the audit assertions are on row counts rather than on the service's answer
//
// A decision that returned successfully and wrote no entry is exactly the defect SHIP-150 exists to
// prevent, and the return value cannot distinguish it. So every assertion below counts rows in
// `audit_log` and `provider_verification_decisions` — observed side effects, which is the shape wave
// 12 found strongest.

// testVerifications adapts `internal/profiles` to the port this domain declares.
//
// Deliberately a copy of cmd/api's `providerVerifications` rather than an approximation of it: a
// double that answered its own outcomes would let a translation defect in the composition root pass
// every test here. What this cannot cover is the *registration* of that adapter, which is
// `TestEveryMutatingAdminRouteIsAudited`'s and the harness's.
type testVerifications struct {
	svc *profiles.Service
}

func (p testVerifications) VerificationsAwaitingReview(
	ctx context.Context,
	r db.Runner,
	q VerificationQuery,
) ([]VerificationEntry, error) {

	found, err := p.svc.AwaitingReview(ctx, r, profiles.QueueQuery{
		State: profiles.State(q.State),
		Limit: q.Limit,
		After: profiles.QueueCursor{
			SubmittedAt: q.After.SubmittedAt,
			ProviderID:  q.After.ProviderID,
		},
	})
	if err != nil {
		return nil, err
	}

	out := make([]VerificationEntry, 0, len(found))
	for _, e := range found {
		out = append(out, VerificationEntry{
			ProviderID:  e.ProviderID,
			Name:        e.Name,
			Email:       e.Email,
			Phone:       e.Phone,
			State:       string(e.State),
			SubmittedAt: e.SubmittedAt,
		})
	}
	return out, nil
}

func (p testVerifications) DecideVerification(
	ctx context.Context,
	r db.Runner,
	d VerificationDecision,
) (VerificationMove, VerificationChange, error) {

	decision, err := p.svc.Decide(ctx, r, d.ProviderID, profiles.State(d.To),
		profiles.Actor{Type: profiles.ActorAdmin, ID: d.ActorID}, d.Reason)

	switch {
	case err == nil:
		return VerificationDecided, VerificationChange{
			ProviderID: d.ProviderID,
			From:       string(decision.From),
			To:         string(decision.Verification.State),
		}, nil
	case errors.Is(err, profiles.ErrNoSuchProvider), errors.Is(err, profiles.ErrNotProvider):
		return VerificationProviderNotFound, VerificationChange{}, nil
	case errors.Is(err, profiles.ErrAlreadyInState):
		return VerificationAlreadyInState, VerificationChange{}, nil
	default:
		return VerificationMoveUnrecognised, VerificationChange{}, err
	}
}

// testProviderVerifications is the port [testServices] wires in.
func testProviderVerifications() ProviderVerifications {
	// The system clock, deliberately. `profiles.NewService` requires one and nothing this suite
	// measures comes from it: a decision's instant is `provider_verification_decisions.decided_at`,
	// which the database supplies. The *auditor* takes the fixture's fixed clock, because that one
	// stamps a row these tests do compare.
	return testVerifications{svc: profiles.NewService(clock.System{})}
}

// testVerificationStates is Docs/04 §4's five outcomes, taken from the domain that owns them rather
// than written out here — which is the whole arrangement [NewVerifications] exists to preserve.
func testVerificationStates() []string {
	out := make([]string, 0, len(profiles.States))
	for _, s := range profiles.States {
		out = append(out, s.String())
	}
	return out
}

// verificationFixture is the console, a handler, and a signed-in administrator of the role the test
// needs.
type verificationFixture struct {
	pool     *pgxpool.Pool
	auth     *Authenticator
	handler  *Handler
	console  *Verifications
	auditor  *Auditor
	admin    Administrator
	token    string
	sequence int
}

func newVerificationFixture(t *testing.T, role Role) *verificationFixture {
	t.Helper()

	creds, auth, pool, clk := adminAuth(t)

	auditor, err := NewAuditor(clk)
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	console, err := NewVerifications(testProviderVerifications(), testVerificationStates(), auditor, pool)
	if err != nil {
		t.Fatalf("building the verification console: %v", err)
	}

	services := testServices(t, creds, pool, clk)
	services.Verifications = console

	handler, err := NewHandler(services, pool, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	email := string(role) + "-reviewer@example.com"
	administrator := anAdministrator(t, creds, email, role)
	issued, _, err := signIn(t, creds, email, testPassword, "10.0.153.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	return &verificationFixture{
		pool: pool, auth: auth, handler: handler, console: console, auditor: auditor,
		admin: administrator, token: issued.Token,
	}
}

// provider registers a provider account, which `000200`'s trigger gives a Pending record.
//
// The mobile number is drawn from a counter rather than from the name, because `uq_users_phone` is
// unique across the whole clone and two fixtures agreeing on a number fail at insertion with a
// message about a phone rather than about the test.
func (f *verificationFixture) provider(t *testing.T, name string) uuid.UUID {
	t.Helper()

	f.sequence++
	id := newAccount(t, f.pool,
		name+"@example.com", "+61400153"+strconv.Itoa(100+f.sequence), "provider")

	if _, err := f.pool.Exec(t.Context(),
		`UPDATE users SET name = $2 WHERE id = $1`, id, "Provider "+name); err != nil {
		t.Fatalf("naming %s: %v", name, err)
	}
	return id
}

// submittedAt backdates a record so an ordering assertion is deterministic rather than lucky.
func (f *verificationFixture) submittedAt(t *testing.T, provider uuid.UUID, at time.Time) {
	t.Helper()

	if _, err := f.pool.Exec(t.Context(),
		`UPDATE provider_verifications SET created_at = $2 WHERE provider_id = $1`,
		provider, at); err != nil {
		t.Fatalf("backdating %s: %v", provider, err)
	}
}

// queue drives GET /v1/admin/verifications through the guard and the handler.
func (f *verificationFixture) queue(t *testing.T, query string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/verifications?"+query, nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.VerificationQueue()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// decide drives POST /v1/admin/verifications/{id}/decision through the guard and the handler.
func (f *verificationFixture) decide(t *testing.T, provider uuid.UUID, state, reason string) (int, string) {
	t.Helper()

	body, err := json.Marshal(decideVerificationRequest{State: state, Reason: reason})
	if err != nil {
		t.Fatalf("encoding the request: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost,
		"/v1/admin/verifications/"+provider.String()+"/decision", strings.NewReader(string(body)))
	req.SetPathValue("id", provider.String())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.DecideVerification()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func (f *verificationFixture) stateOf(t *testing.T, provider uuid.UUID) string {
	t.Helper()

	var state string
	if err := f.pool.QueryRow(t.Context(),
		`SELECT state FROM provider_verifications WHERE provider_id = $1`, provider).Scan(&state); err != nil {
		t.Fatalf("reading the state of %s: %v", provider, err)
	}
	return state
}

func (f *verificationFixture) decisionCount(t *testing.T, provider uuid.UUID) int {
	t.Helper()

	var rows int
	if err := f.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM provider_verification_decisions WHERE provider_id = $1`,
		provider).Scan(&rows); err != nil {
		t.Fatalf("counting decisions: %v", err)
	}
	return rows
}

// queueProviders is the provider identifiers in a queue response, in the order they were returned.
func queueProviders(t *testing.T, body string) []string {
	t.Helper()

	var page struct {
		Data []verificationEntryResponse `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the queue: %v (%s)", err, body)
	}

	out := make([]string, 0, len(page.Data))
	for _, e := range page.Data {
		out = append(out, e.ProviderID)
	}
	return out
}

// TestThePendingQueueIsListedOldestFirst is SHIP-153's *Done when*, driven over HTTP.
//
// The Go half in `internal/profiles` establishes the query; this establishes the *endpoint* — the
// permission, the ordering as it reaches a console, and that a decided provider leaves the queue.
func TestThePendingQueueIsListedOldestFirst(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)

	base := time.Date(2026, 6, 10, 8, 0, 0, 0, time.UTC)
	oldest := f.provider(t, "oldest")
	middle := f.provider(t, "middle")
	newest := f.provider(t, "newest")

	// Backdated in an order that is not the insertion order, so a queue reading the insertion
	// sequence rather than the submission clock fails here.
	f.submittedAt(t, newest, base.Add(3*time.Hour))
	f.submittedAt(t, oldest, base)
	f.submittedAt(t, middle, base.Add(time.Hour))

	code, body := f.queue(t, "state=Pending")
	if code != http.StatusOK {
		t.Fatalf("the queue answered %d: %s", code, body)
	}

	got := queueProviders(t, body)
	want := []string{oldest.String(), middle.String(), newest.String()}
	if len(got) != len(want) {
		t.Fatalf("the queue holds %d providers, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("position %d is %s, want %s — the queue is not oldest first", i, got[i], want[i])
		}
	}

	// A decision takes the provider off it, which is the half a list of every provider would get
	// wrong while looking right.
	code, body = f.decide(t, middle, "Verified",
		"Licence, registration and insurance all current and matching the account.")
	if code != http.StatusOK {
		t.Fatalf("the decision answered %d: %s", code, body)
	}

	_, body = f.queue(t, "state=Pending")
	for _, id := range queueProviders(t, body) {
		if id == middle.String() {
			t.Error("a provider who has been decided is still on the Pending queue")
		}
	}

	_, body = f.queue(t, "state=Verified")
	if got := queueProviders(t, body); len(got) != 1 || got[0] != middle.String() {
		t.Errorf("the Verified queue is %v, want just %s", got, middle)
	}
}

// TestTheVerificationQueueCarriesNothingCommercial.
//
// Docs/01 §4.3's invariant is absolute about a customer's budget, and an administrative shape that
// never carried a commercial fact cannot leak one. Held to a **closed key set** rather than searched
// for the word "budget", which is the axis SHIP-83 found a spelling-based check lacks: a field named
// anything at all carrying an amount would pass a word search and fail this.
func TestTheVerificationQueueCarriesNothingCommercial(t *testing.T) {
	f := newVerificationFixture(t, RoleSupport)
	f.provider(t, "keys")

	code, body := f.queue(t, "state=Pending")
	if code != http.StatusOK {
		t.Fatalf("the queue answered %d: %s", code, body)
	}

	var page struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if len(page.Data) != 1 {
		t.Fatalf("the queue holds %d entries, want 1", len(page.Data))
	}

	allowed := map[string]bool{
		"provider_id": true, "name": true, "email": true,
		"phone": true, "state": true, "submitted_at": true,
	}
	for key := range page.Data[0] {
		if !allowed[key] {
			t.Errorf("the queue entry carries %q, which is not one of the six keys this shape "+
				"is allowed; a queue entry is what somebody triaging needs and nothing more", key)
		}
	}
	for key := range allowed {
		if _, ok := page.Data[0][key]; !ok {
			t.Errorf("the queue entry is missing %q; every field is always present so a console "+
				"that renders without checking does not crash on the ordinary case", key)
		}
	}
}

// TestReadingTheQueueIsNotPermissionToDecideIt is Docs/04 §9's least-privilege control, as two
// permissions on two endpoints.
//
// `support` holds `verifications.read` and not `verifications.decide`. The refusal must change
// nothing — no state, no decision row, no audit entry — because an entry for a refused action would
// record a review that did not happen, in a table with no way to take it back.
func TestReadingTheQueueIsNotPermissionToDecideIt(t *testing.T) {
	f := newVerificationFixture(t, RoleSupport)
	provider := f.provider(t, "supportonly")

	if code, body := f.queue(t, "state=Pending"); code != http.StatusOK {
		t.Fatalf("a support administrator could not read the queue: %d %s", code, body)
	}

	before := entryIDs(t, f.pool)

	code, body := f.decide(t, provider, "Verified", "Everything looked fine to me.")
	if code != http.StatusForbidden {
		t.Fatalf("a support administrator decided a verification and got %d, want 403: %s", code, body)
	}
	if !strings.Contains(body, "admin_permission_denied") {
		t.Errorf("the refusal does not carry the code a console branches on: %s", body)
	}
	if got := f.stateOf(t, provider); got != "Pending" {
		t.Errorf("a refused decision moved the provider to %q", got)
	}
	if n := f.decisionCount(t, provider); n != 0 {
		t.Errorf("a refused decision wrote %d rows into the evidence trail", n)
	}
	if added := entriesAddedSince(t, f.pool, before); len(added) != 0 {
		t.Errorf("a refused decision wrote %d audit entries, in a table nothing can correct", len(added))
	}
}

// TestAReviewerSetsEachOutcomeWithARecordedReason is SHIP-154's *Done when*, read literally:
// "Reviewer sets Verified, Restricted, Rejected, or Suspended with a recorded reason."
//
// All four, in sequence on one provider, each with its own reason — and each checked in three
// places: the response, the evidence trail (Docs/04 §1) and the audit log (Docs/04 §9).
func TestAReviewerSetsEachOutcomeWithARecordedReason(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)
	provider := f.provider(t, "fouroutcomes")

	steps := []struct {
		to     string
		from   string
		reason string
	}{
		{"Verified", "Pending", "Licence, registration and insurance all current."},
		{"Restricted", "Verified", "Insurance certificate expires this month; renew it to continue."},
		{"Rejected", "Restricted", "The licence supplied is in a different name from the account."},
		{"Suspended", "Rejected", "Repeated safety reports from customers over one fortnight."},
	}

	for _, step := range steps {
		t.Run(step.to, func(t *testing.T) {
			before := entryIDs(t, f.pool)

			code, body := f.decide(t, provider, step.to, step.reason)
			if code != http.StatusOK {
				t.Fatalf("deciding %s answered %d: %s", step.to, code, body)
			}

			var out verificationDecisionResponse
			if err := json.Unmarshal([]byte(body), &out); err != nil {
				t.Fatalf("decoding: %v (%s)", err, body)
			}
			if out.From != step.from || out.To != step.to {
				t.Errorf("the response says %s → %s, want %s → %s", out.From, out.To, step.from, step.to)
			}
			if out.Reason != step.reason {
				t.Errorf("the response reads back the reason %q, want %q", out.Reason, step.reason)
			}
			if got := f.stateOf(t, provider); got != step.to {
				t.Errorf("the record says %q after a decision to %s", got, step.to)
			}

			// The provider's evidence trail (Docs/04 §1).
			var recordedFrom, recordedTo, actorType, recordedReason string
			var actorID uuid.UUID
			if err := f.pool.QueryRow(t.Context(),
				`SELECT from_state, to_state, actor_type, actor_id, reason
				 FROM provider_verification_decisions
				 WHERE provider_id = $1 ORDER BY decided_at DESC, id DESC LIMIT 1`, provider,
			).Scan(&recordedFrom, &recordedTo, &actorType, &actorID, &recordedReason); err != nil {
				t.Fatalf("reading the newest decision: %v", err)
			}
			if recordedFrom != step.from || recordedTo != step.to {
				t.Errorf("the evidence trail records %s → %s", recordedFrom, recordedTo)
			}
			if actorType != "admin" || actorID != f.admin.ID {
				t.Errorf("the decision is attributed to %s/%s, want admin/%s",
					actorType, actorID, f.admin.ID)
			}
			if recordedReason != step.reason {
				t.Errorf("the evidence trail records the reason %q", recordedReason)
			}

			// The administrator's accountability record (Docs/04 §9).
			added := entriesAddedSince(t, f.pool, before)
			if len(added) != 1 {
				t.Fatalf("one decision wrote %d audit entries, want exactly 1", len(added))
			}
			entry := added[0]
			if entry.Action != string(AuditActionVerificationDecided) {
				t.Errorf("the entry records the action %q", entry.Action)
			}
			if entry.TargetType != AuditTargetUser || entry.TargetID != provider {
				t.Errorf("the entry targets %s/%s, want %s/%s",
					entry.TargetType, entry.TargetID, AuditTargetUser, provider)
			}
			if entry.ActorID == nil || *entry.ActorID != f.admin.ID {
				t.Errorf("the entry names the actor %v, want %s", entry.ActorID, f.admin.ID)
			}
			if entry.Reason == nil || *entry.Reason != step.reason {
				t.Errorf("the entry records the reason %v, want %q", entry.Reason, step.reason)
			}
			if entry.Metadata["from"] != step.from || entry.Metadata["to"] != step.to {
				t.Errorf("the entry's metadata is %v, want from=%s to=%s",
					entry.Metadata, step.from, step.to)
			}
		})
	}

	if n := f.decisionCount(t, provider); n != len(steps) {
		t.Errorf("%d decisions recorded, want %d — the trail keeps every one of them", n, len(steps))
	}
}

// TestADecisionAndItsAuditEntryCommitTogether is the mutation this branch was asked to run, as a
// standing test.
//
// **The auditor is nil, so [Auditor.Record] fails before any SQL.** That matters: wave 10
// established that a trigger-based failure proves a rollback and proves nothing about an
// `if err != nil`, because PostgreSQL aborts the whole transaction as soon as a statement raises and
// the error reaches the caller through the COMMIT either way. Failing before the first statement is
// the only arrangement in which the check itself is under test.
//
// What must survive is nothing at all: no state change, and no decision row. A provider whose
// eligibility moved with nobody accountable for moving it is the backfill nobody can perform.
func TestADecisionAndItsAuditEntryCommitTogether(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)
	provider := f.provider(t, "atomic")

	// Built by hand, past the constructor's refusal — which is the whole reason the constructor
	// has one.
	unwritable := &Verifications{
		records: testProviderVerifications(),
		auditor: nil,
		states:  testVerificationStates(),
		pool:    f.pool,
	}

	_, err := unwritable.Decide(t.Context(), VerificationCommand{
		ProviderID: provider,
		ActorID:    f.admin.ID,
		State:      "Verified",
		Reason:     "Licence, registration and insurance all current.",
	})
	if err == nil {
		t.Fatal("a provider was verified with no audit entry behind it.\n" +
			"SHIP-150: the entry commits with the action or neither happens. Docs/04 §1 requires " +
			"every review and decision be recorded, and an eligibility change nobody is " +
			"accountable for is the one thing this table cannot be repaired to show.")
	}

	if got := f.stateOf(t, provider); got != "Pending" {
		t.Errorf("the provider is %q, want Pending — the error was returned and the transaction "+
			"was still committed, which is the same defect one statement later", got)
	}
	if n := f.decisionCount(t, provider); n != 0 {
		t.Errorf("%d decision rows survived an unwritable audit trail; the evidence trail says a "+
			"review happened and nothing says who performed it", n)
	}
}

// TestTheAuditEntryIsWrittenOnTheDecisionsOwnConnection is the atomicity claim, and it is the test
// the obvious one does not make.
//
// # Why [TestADecisionAndItsAuditEntryCommitTogether] is not enough
//
// That test nils the auditor, so [Auditor.Record] fails before any SQL and the decision is refused.
// It proves the `if err != nil` is present and checked — which is worth proving, and is a different
// claim. **It passes unchanged when the audit write is moved onto its own connection**, because a
// nil auditor fails whichever runner it is handed. Measured, not assumed: the mutation
// `v.auditor.Record(ctx, tx, …)` → `v.auditor.Record(ctx, v.pool, …)` leaves `internal/admin`,
// `cmd/api`, `internal/profiles`, `migrations` and every section of `make verify` green.
//
// Nothing else could catch it either. **The database cannot**: the entry is in `audit_log` and the
// decision is in `provider_verification_decisions`, and no constraint spans two tables in two
// transactions. **The harness cannot**: it asserts that a refused decision writes nothing and an
// accepted one writes both, and on a healthy database a mutation that commits both separately
// satisfies both.
//
// # The one observable difference is the connection
//
// An entry written into the caller's transaction needs **no second connection**; one written to the
// pool needs one, and needs it while the transaction is still holding its own. So a pool with a
// single connection tells the two apart with certainty: the correct code completes, and the
// separated write blocks until the acquisition times out.
//
// That is a structural property rather than a timing one — `MaxConns = 1` makes the second
// acquisition impossible, not merely slow — and it is what "the entry commits with the thing it
// records" means expressed as something a test can see.
func TestTheAuditEntryIsWrittenOnTheDecisionsOwnConnection(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)
	provider := f.provider(t, "oneconn")

	// One connection, against the same database this test's fixtures were built in.
	config := f.pool.Config().Copy()
	config.MaxConns = 1

	single, err := pgxpool.NewWithConfig(t.Context(), config)
	if err != nil {
		t.Fatalf("building a single-connection pool: %v", err)
	}
	defer single.Close()

	auditor, err := NewAuditor(clock.System{})
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	console, err := NewVerifications(testProviderVerifications(), testVerificationStates(), auditor, single)
	if err != nil {
		t.Fatalf("building the verification console: %v", err)
	}

	// Bounded, so a separated write fails the test rather than hanging it: pgxpool waits for a
	// connection until the context is done, and the context this would otherwise take is the
	// test's own.
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()

	change, err := console.Decide(ctx, VerificationCommand{
		ProviderID: provider,
		ActorID:    f.admin.ID,
		State:      "Verified",
		Reason:     "Licence, registration and insurance all current.",
	})
	if err != nil {
		t.Fatalf("the decision could not complete against a single-connection pool: %v\n"+
			"SHIP-150: the audit entry is written into the caller's transaction, so it needs no "+
			"second connection. A write that reaches for one is a write that commits separately "+
			"from the decision it records — and Docs/04 §1 requires the trail be evidence of the "+
			"thing it sits beside.", err)
	}
	if change.From != "Pending" || change.To != "Verified" {
		t.Errorf("the decision reports %s → %s", change.From, change.To)
	}

	// And both rows are there, so the test cannot pass by the decision having quietly done less.
	if got := f.stateOf(t, provider); got != "Verified" {
		t.Errorf("the provider is %q after the decision", got)
	}
	var entries int
	if err := f.pool.QueryRow(t.Context(),
		`SELECT count(*) FROM audit_log WHERE target_id = $1 AND action = $2`,
		provider, string(AuditActionVerificationDecided)).Scan(&entries); err != nil {
		t.Fatalf("counting entries: %v", err)
	}
	if entries != 1 {
		t.Errorf("%d audit entries for one decision, want 1", entries)
	}
}

// TestADecisionThatRecordsNothingIsRefused.
//
// Docs/04 §4 requires a rejection's reason be communicable to the provider and §6.6 requires one of
// every outcome. An empty string and a single character are both ways of satisfying a required field
// without recording anything, and this is the field a later reader has no way to reconstruct.
func TestADecisionThatRecordsNothingIsRefused(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)
	provider := f.provider(t, "noreason")

	for _, reason := range []string{"", "   ", "no", strings.Repeat("x", maxReasonLength+1)} {
		code, body := f.decide(t, provider, "Verified", reason)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("a reason of %d characters answered %d, want 422: %s", len(reason), code, body)
		}
		if !strings.Contains(body, `"reason"`) {
			t.Errorf("the refusal does not name the field: %s", body)
		}
	}
	if got := f.stateOf(t, provider); got != "Pending" {
		t.Errorf("a refused decision moved the provider to %q", got)
	}
	if n := f.decisionCount(t, provider); n != 0 {
		t.Errorf("%d decisions were recorded for refused requests", n)
	}
}

// TestAnOutcomeDocs04DoesNotHaveIsRefusedAndNamesTheFive.
//
// The five come from `profiles.States`, which is held to `ck_provider_verifications_state` by a test
// in both directions — so the message a console shows and the constraint the database holds cannot
// drift apart. That is the whole reason this package has no copy of the list.
func TestAnOutcomeDocs04DoesNotHaveIsRefusedAndNamesTheFive(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)
	provider := f.provider(t, "badoutcome")

	for _, state := range []string{"", "Approved", "verified", "Banned"} {
		code, body := f.decide(t, provider, state,
			"A perfectly good reason that is long enough to record something.")
		if code != http.StatusUnprocessableEntity {
			t.Errorf("the outcome %q answered %d, want 422: %s", state, code, body)
		}
		if !strings.Contains(body, `"state"`) {
			t.Errorf("the refusal does not name the field: %s", body)
		}
		for _, known := range profiles.States {
			if !strings.Contains(body, known.String()) {
				t.Errorf("the refusal does not offer %q as a choice: %s", known, body)
			}
		}
	}

	// And the queue refuses one too, rather than answering an empty page — which is what "nobody
	// is waiting" looks like to somebody who mistyped a state.
	code, body := f.queue(t, "state=Approved")
	if code != http.StatusUnprocessableEntity {
		t.Errorf("an unrecognised queue state answered %d, want 422: %s", code, body)
	}
	if code, body := f.queue(t, ""); code != http.StatusUnprocessableEntity {
		t.Errorf("a queue with no state answered %d, want 422: %s", code, body)
	}
}

// TestDecidingTheOutcomeAlreadyHeldIsRefusedRatherThanRecorded.
//
// The ordinary outcome of two moderators reading one queue, and the console's right response is to
// reload. An entry saying "changed from Verified to Verified" is noise in the one table whose value
// is that everything in it happened.
func TestDecidingTheOutcomeAlreadyHeldIsRefusedRatherThanRecorded(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)
	provider := f.provider(t, "nochange")

	if code, body := f.decide(t, provider, "Verified", "Everything current at first review."); code != http.StatusOK {
		t.Fatalf("the first decision answered %d: %s", code, body)
	}

	before := entryIDs(t, f.pool)

	code, body := f.decide(t, provider, "Verified", "Looked at it again and left it alone.")
	if code != http.StatusConflict {
		t.Fatalf("re-deciding the outcome already held answered %d, want 409: %s", code, body)
	}
	if !strings.Contains(body, "admin_verification_unchanged") {
		t.Errorf("the refusal does not carry the code a console branches on: %s", body)
	}
	if n := f.decisionCount(t, provider); n != 1 {
		t.Errorf("%d decisions recorded, want 1 — a review with no change to show for it is not a review", n)
	}
	if added := entriesAddedSince(t, f.pool, before); len(added) != 0 {
		t.Errorf("a refused decision wrote %d audit entries", len(added))
	}
}

// TestAnAccountWithNoVerificationRecordIsAPlain404.
//
// A customer and an identifier that names nobody are the same answer, because `000200` gives every
// provider a record at registration and the two are indistinguishable from the record's side. Unlike
// dispute intake's identical-looking 404 this is not a disclosure decision — the caller holds
// `verifications.decide` and nothing is being kept from them.
func TestAnAccountWithNoVerificationRecordIsAPlain404(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)

	customer := newAccount(t, f.pool, "verif-customer@example.com", "+61400153999", "customer")
	nobody := uuid.New()

	for _, id := range []uuid.UUID{customer, nobody} {
		code, body := f.decide(t, id, "Verified",
			"A reason long enough to be recorded against a decision.")
		if code != http.StatusNotFound {
			t.Errorf("deciding %s answered %d, want 404: %s", id, code, body)
		}
	}

	// And the customer is not on the queue either — most accounts on this platform are customers,
	// and a queue holding them is a queue of people nobody will ever review.
	_, body := f.queue(t, "state=Pending")
	if strings.Contains(body, customer.String()) {
		t.Errorf("a customer is on the provider verification queue: %s", body)
	}
}

// TestTheQueueCursorNeitherSkipsNorRepeats, over HTTP and through the encoded cursor.
//
// The domain half proves the SQL; this proves the encoding round trip, which is where a cursor with
// one field instead of two, or a timestamp that loses precision, would go wrong without any SQL
// changing.
func TestTheQueueCursorNeitherSkipsNorRepeats(t *testing.T) {
	f := newVerificationFixture(t, RoleModerator)

	same := time.Date(2026, 6, 11, 9, 30, 0, 0, time.UTC)
	var registered []uuid.UUID
	for _, name := range []string{"tiea", "tieb", "tiec"} {
		id := f.provider(t, name)
		f.submittedAt(t, id, same)
		registered = append(registered, id)
	}

	seen := map[string]bool{}
	cursor := ""
	for range len(registered) {
		query := "state=Pending&limit=1"
		if cursor != "" {
			query += "&cursor=" + cursor
		}

		code, body := f.queue(t, query)
		if code != http.StatusOK {
			t.Fatalf("paging answered %d: %s", code, body)
		}

		var page struct {
			Data []verificationEntryResponse `json:"data"`
			Next string                      `json:"next_cursor"`
			More bool                        `json:"has_more"`
		}
		if err := json.Unmarshal([]byte(body), &page); err != nil {
			t.Fatalf("decoding: %v (%s)", err, body)
		}
		if len(page.Data) != 1 {
			t.Fatalf("a page of one returned %d entries: %s", len(page.Data), body)
		}
		if seen[page.Data[0].ProviderID] {
			t.Errorf("%s was returned twice while paging", page.Data[0].ProviderID)
		}
		seen[page.Data[0].ProviderID] = true
		cursor = page.Next
	}

	for _, id := range registered {
		if !seen[id.String()] {
			t.Errorf("%s was never returned; paging skipped a provider waiting for review", id)
		}
	}
}
