package admin

import (
	"context"
	"encoding/json"
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

// SHIP-153 against a real PostgreSQL, through the real records.
//
// # The port is `internal/profiles` itself, not a double, and that is the point
//
// The interesting half of this queue is *which rows it selects*: only providers, only in the state
// asked for, ordered by a clock the record carries rather than by insertion. A double would answer
// whatever it was told to, so a suite written against one would pass with the query replaced by any
// other query.
//
// A test file may import another domain. The boundary lint skips `_test.go` deliberately, and
// records why: "a test wires domains together in the same way cmd/api does". [testVerifications] is
// therefore the same adapter `cmd/api/routes_admin.go` registers, written twice rather than shared,
// because package main is not importable and no test in it has a database to reach.

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

// decided moves a provider through the one guarded transition.
//
// SQL rather than an endpoint because the endpoint is SHIP-154's. `provider_verification_decide`
// writes the decision, names it to the trigger through a transaction-local setting and moves the
// state; a bare `UPDATE` here would be refused by `provider_verification_change_is_guarded`, which
// is the guard doing its job even against a test fixture.
func (f *verificationFixture) decided(t *testing.T, provider uuid.UUID, state, reason string) {
	t.Helper()

	if _, err := f.pool.Exec(t.Context(),
		`SELECT provider_verification_decide($1, $2, 'admin', $3, $4)`,
		provider, state, f.admin.ID, reason); err != nil {
		t.Fatalf("deciding %s for %s: %v", state, provider, err)
	}
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
	//
	// Driven through the database's own guarded function rather than through an endpoint, because
	// there is not one yet: SHIP-154 is the route to this act. That is not a weaker fixture —
	// `provider_verification_decide` is the single implementation of a transition, so the console,
	// the domain and this line are the same caller.
	f.decided(t, middle, "Verified", "Licence, registration and insurance all current.")

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

// TestAnOutcomeDocs04DoesNotHaveIsRefusedRatherThanAnsweredEmpty.
//
// The five come from `profiles.States`, which is held to `ck_provider_verifications_state` by a test
// in both directions — so the message a console shows and the constraint the database holds cannot
// drift apart. That is the whole reason this package has no copy of the list.
//
// Refused rather than ignored: an ignored filter answers an empty page, and an empty review queue is
// what "nobody is waiting" looks like to somebody who mistyped a state.
func TestAnOutcomeDocs04DoesNotHaveIsRefusedRatherThanAnsweredEmpty(t *testing.T) {
	f := newVerificationFixture(t, RoleSupport)
	f.provider(t, "badfilter")

	for _, query := range []string{"state=Approved", "state=verified", "state=", ""} {
		code, body := f.queue(t, query)
		if code != http.StatusUnprocessableEntity {
			t.Errorf("%q answered %d, want 422: %s", query, code, body)
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
}
