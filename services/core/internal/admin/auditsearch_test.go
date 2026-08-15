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
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// SHIP-165 against a real PostgreSQL.
//
// Docs/06 §4.1's argument for not mocking is unusually direct here: what this endpoint *is* is a
// `WHERE` clause and an `ORDER BY`, and a mocked store would be a test of a slice filter written
// twice. The half-open date range, the two-column cursor over a non-unique `created_at`, and the
// jsonb passed through as text are all PostgreSQL behaviours.
//
// # The entries are written by the real writer, not inserted
//
// Every fixture below goes through [Auditor.Record] inside a transaction, which is how the service
// writes one. An INSERT would agree with the reader about the column layout by construction and
// would not notice a writer that had started supplying a different instant — which is the defect
// audit.go's header spends most of its length on.

// auditSearchFixture is a handler with a live trail, a signed-in reader, and a writer sharing the
// clock the entries are dated by.
type auditSearchFixture struct {
	pool    *pgxpool.Pool
	auditor *Auditor
	clk     *clock.Fixed
	handler *Handler
	auth    *Authenticator
	token   string
}

func newAuditSearchFixture(t *testing.T) auditSearchFixture {
	t.Helper()

	creds, auth, pool, sessionClock := adminAuth(t)

	// **A clock of its own, separate from the one the sessions run on, and that is not tidiness.**
	// The date axis is exercised by advancing time a day at a time, and the administrator session
	// reading the trail runs on an idle window measured against a clock too (SHIP-147). Sharing
	// one would make every date test expire the credential it was using — which is a true fact
	// about sessions and has nothing to do with what is being tested here.
	//
	// It starts at the same instant, so an entry written before any advance is dated inside the
	// day the session was created in.
	clk := clock.NewFixed(time.Date(2026, 8, 14, 9, 0, 0, 0, time.UTC))

	auditor, err := NewAuditor(clk)
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	handler, err := NewHandler(testServices(t, creds, pool, sessionClock), pool, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	// A support administrator, because `audit.read` is held by every role — a trail only the
	// people it records can read is not a control.
	anAdministrator(t, creds, "trail-reader@example.com", RoleSupport)
	issued, _, err := signIn(t, creds, "trail-reader@example.com", testPassword, "10.0.80.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	return auditSearchFixture{
		pool: pool, auditor: auditor, clk: clk,
		handler: handler, auth: auth, token: issued.Token,
	}
}

// record appends one entry at the fixture clock's current instant and returns its identifier.
func (f auditSearchFixture) record(t *testing.T, actor, target uuid.UUID, action AuditAction, meta map[string]any) uuid.UUID {
	t.Helper()

	var id uuid.UUID
	err := db.InTx(context.Background(), f.pool, func(ctx context.Context, r db.Runner) error {
		var err error
		id, err = f.auditor.Record(ctx, r, AuditEntry{
			Actor:      AdminActor(actor),
			Action:     action,
			TargetType: AuditTargetAdministrator,
			TargetID:   target,
			Metadata:   meta,
		})
		return err
	})
	if err != nil {
		t.Fatalf("recording an audit entry: %v", err)
	}
	return id
}

// search runs one request through the guard and the handler.
func (f auditSearchFixture) search(t *testing.T, query url.Values) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/audit?"+query.Encode(), nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.AuditTrail()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// entriesFrom decodes one page.
func entriesFrom(t *testing.T, body string) ([]auditEntryResponse, string, bool) {
	t.Helper()

	var page struct {
		Data       []auditEntryResponse `json:"data"`
		NextCursor string               `json:"next_cursor"`
		HasMore    bool                 `json:"has_more"`
	}
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the page: %v (%s)", err, body)
	}
	return page.Data, page.NextCursor, page.HasMore
}

func idsOf(entries []auditEntryResponse) []string {
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.ID)
	}
	return out
}

func contains(ids []string, want uuid.UUID) bool {
	for _, id := range ids {
		if id == want.String() {
			return true
		}
	}
	return false
}

// TestTheTrailIsSearchableByActorTargetAndDate is SHIP-165's *Done when*, read as three claims.
//
// Each axis is checked in **both directions** — the entry that should be there is, and the entry
// that should not be is not. One direction alone is satisfied by a filter that does nothing: a
// search returning everything passes every "is it there" assertion ever written.
func TestTheTrailIsSearchableByActorTargetAndDate(t *testing.T) {
	f := newAuditSearchFixture(t)

	alice, bob := uuid.New(), uuid.New()
	subject, other := uuid.New(), uuid.New()

	// Three entries on three days, so the date axis has something to bound.
	day1 := f.record(t, alice, subject, AuditActionAdministratorCreated, map[string]any{"role": "moderator"})

	f.clk.Advance(24 * time.Hour)
	day2 := f.record(t, bob, other, AuditActionAdministratorSignedIn, nil)

	f.clk.Advance(24 * time.Hour)
	day3 := f.record(t, alice, other, AuditActionAdministratorSignedOut, nil)

	t.Run("by actor", func(t *testing.T) {
		status, body := f.search(t, url.Values{"actor": {alice.String()}})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", status, body)
		}
		entries, _, _ := entriesFrom(t, body)
		ids := idsOf(entries)

		if !contains(ids, day1) || !contains(ids, day3) {
			t.Errorf("a search by actor missed one of that actor's entries: %v", ids)
		}
		if contains(ids, day2) {
			t.Error("a search by actor returned another actor's entry, so the filter does nothing")
		}
	})

	t.Run("by target", func(t *testing.T) {
		status, body := f.search(t, url.Values{"target": {subject.String()}})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", status, body)
		}
		entries, _, _ := entriesFrom(t, body)
		ids := idsOf(entries)

		if !contains(ids, day1) {
			t.Errorf("a search by target missed the entry naming it: %v", ids)
		}
		if contains(ids, day2) || contains(ids, day3) {
			t.Error("a search by target returned entries about something else")
		}
	})

	t.Run("by date, and the bounds tile", func(t *testing.T) {
		// The middle day only. `from` inclusive and `to` exclusive is the only pair that makes
		// consecutive days tile without overlapping — an inclusive upper bound would show an
		// entry written at midnight on both days.
		start := f.clk.Now().Add(-24 * time.Hour)
		status, body := f.search(t, url.Values{
			"from": {start.Format(time.RFC3339)},
			"to":   {start.Add(24 * time.Hour).Format(time.RFC3339)},
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", status, body)
		}
		entries, _, _ := entriesFrom(t, body)
		ids := idsOf(entries)

		if !contains(ids, day2) {
			t.Errorf("the entry inside the range is missing: %v", ids)
		}
		if contains(ids, day1) {
			t.Error("an entry before `from` came back, so the lower bound does nothing")
		}
		if contains(ids, day3) {
			t.Error("the entry written exactly at `to` came back; the upper bound is exclusive " +
				"precisely so that consecutive days do not overlap")
		}
	})

	t.Run("by action, which the table was built around", func(t *testing.T) {
		status, body := f.search(t, url.Values{
			"action": {AuditActionAdministratorSignedOut.String()},
		})
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", status, body)
		}
		entries, _, _ := entriesFrom(t, body)
		ids := idsOf(entries)

		if !contains(ids, day3) {
			t.Errorf("a search by action missed the entry: %v", ids)
		}
		if contains(ids, day1) || contains(ids, day2) {
			t.Error("a search by action returned entries with other actions")
		}
	})
}

// TestADateIsAcceptedAsADayOrAsAnInstant.
//
// A support engineer types `2026-08-14`; a console sends an instant. Accepting only the second makes
// the endpoint unusable by hand, which is most of what an audit viewer is for.
func TestADateIsAcceptedAsADayOrAsAnInstant(t *testing.T) {
	f := newAuditSearchFixture(t)

	actor := uuid.New()
	entry := f.record(t, actor, uuid.New(), AuditActionAdministratorSignedIn, nil)

	// The fixture clock is 2026-08-14T09:00:00Z, so the day that contains it is 08-14 → 08-15.
	for _, tc := range []struct{ name, from, to string }{
		{"as days", "2026-08-14", "2026-08-15"},
		{"as instants", "2026-08-14T00:00:00Z", "2026-08-15T00:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := f.search(t, url.Values{
				"actor": {actor.String()},
				"from":  {tc.from},
				"to":    {tc.to},
			})
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (%s)", status, body)
			}
			entries, _, _ := entriesFrom(t, body)
			if !contains(idsOf(entries), entry) {
				t.Errorf("the entry is not in the day that contains it: %v", idsOf(entries))
			}
		})
	}
}

// TestEveryBadTrailFilterIsRefusedAndNamed.
//
// An unrecognised filter is refused rather than ignored, which is the same decision the account and
// job searches take — and it matters most here. An ignored filter answers with the whole trail, and
// "everything" and "the seventeen entries for this action" are indistinguishable to somebody who
// mistyped one, who then concludes the action never happened.
//
// 422 rather than 400: these are field-level validation failures in validate.Errors' shape.
func TestEveryBadTrailFilterIsRefusedAndNamed(t *testing.T) {
	f := newAuditSearchFixture(t)

	for _, tc := range []struct {
		name  string
		query url.Values
		field string
	}{
		{"an action nothing records", url.Values{"action": {"administrator.deleted"}}, "action"},
		{"an actor that is not an identifier", url.Values{"actor": {"nobody"}}, "actor"},
		{"a target that is not an identifier", url.Values{"target": {"something"}}, "target"},
		{"a date that is not a date", url.Values{"from": {"last Tuesday"}}, "from"},
		{
			"a range that ends before it starts",
			url.Values{"from": {"2026-08-14"}, "to": {"2026-08-01"}},
			"to",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := f.search(t, tc.query)
			if status != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", status, body)
			}
			if !strings.Contains(body, `"`+tc.field+`"`) {
				t.Errorf("the refusal does not name %q: %s", tc.field, body)
			}
		})
	}
}

// TestEveryBadTrailFilterIsReportedAtOnce is Docs/10 §4.6.
//
// Every problem rather than the first. A console sending three bad parameters should be told about
// three, not walked through them one request at a time.
func TestEveryBadTrailFilterIsReportedAtOnce(t *testing.T) {
	f := newAuditSearchFixture(t)

	status, body := f.search(t, url.Values{
		"actor":  {"nobody"},
		"action": {"administrator.deleted"},
		"from":   {"last Tuesday"},
	})
	if status != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422 (%s)", status, body)
	}
	for _, field := range []string{"actor", "action", "from"} {
		if !strings.Contains(body, `"`+field+`"`) {
			t.Errorf("the refusal does not mention %q: %s", field, body)
		}
	}
}

// TestTheTrailPagesOverEntriesWrittenInOneInstant.
//
// **The case a one-column cursor gets wrong, and it is the ordinary case here rather than a rare
// one.** [Auditor.Record] gives every entry in a transaction the same injected instant, by design —
// so `created_at` alone cannot order them, and a cursor built from it would skip an entry or repeat
// one on exactly the rows most worth reading together.
//
// The tie is broken by a v7 identifier, which is time-ordered, so entries appended in one
// transaction come back in the order they were appended.
func TestTheTrailPagesOverEntriesWrittenInOneInstant(t *testing.T) {
	f := newAuditSearchFixture(t)

	actor := uuid.New()

	// Four entries, all at the same instant: the clock is fixed and is not advanced.
	written := make([]uuid.UUID, 0, 4)
	for range 4 {
		written = append(written, f.record(t, actor, uuid.New(), AuditActionAdministratorSignedIn, nil))
	}

	seen := map[string]int{}
	cursor := ""
	for range 10 {
		query := url.Values{"actor": {actor.String()}, "limit": {"2"}}
		if cursor != "" {
			query.Set("cursor", cursor)
		}

		status, body := f.search(t, query)
		if status != http.StatusOK {
			t.Fatalf("status = %d, want 200 (%s)", status, body)
		}
		entries, next, hasMore := entriesFrom(t, body)
		for _, e := range entries {
			seen[e.ID]++
		}
		if !hasMore {
			break
		}
		if next == "" {
			t.Fatal("the page reports more and issues no cursor, so a client cannot reach the rest")
		}
		cursor = next
	}

	if len(seen) != len(written) {
		t.Fatalf("paging saw %d entries, want %d — a cursor over a non-unique created_at either "+
			"skips a row or repeats one, which in this table is the failure it exists to prevent",
			len(seen), len(written))
	}
	for _, id := range written {
		if seen[id.String()] != 1 {
			t.Errorf("%s was seen %d times across the pages, want exactly 1", id, seen[id.String()])
		}
	}
}

// TestAnEntryIsReportedExactlyAsItWasWritten.
//
// The metadata is passed through as raw JSON rather than decoded and re-encoded, so what a reader
// sees is what the platform wrote. Every other field is checked here too, because an audit viewer
// that renamed or dropped one would be a second version of the record.
func TestAnEntryIsReportedExactlyAsItWasWritten(t *testing.T) {
	f := newAuditSearchFixture(t)

	actor, target := uuid.New(), uuid.New()
	entry := f.record(t, actor, target, AuditActionAdministratorCreated,
		map[string]any{"role": "moderator"})

	status, body := f.search(t, url.Values{"actor": {actor.String()}})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	entries, _, _ := entriesFrom(t, body)
	if len(entries) != 1 {
		t.Fatalf("the page carries %d entries, want 1 (%s)", len(entries), body)
	}

	got := entries[0]
	if got.ID != entry.String() {
		t.Errorf("id = %s, want %s", got.ID, entry)
	}
	if got.ActorType != AuditActorAdmin.String() {
		t.Errorf("actor_type = %q, want %q", got.ActorType, AuditActorAdmin)
	}
	if got.ActorID != actor.String() {
		t.Errorf("actor_id = %s, want %s", got.ActorID, actor)
	}
	if got.Action != AuditActionAdministratorCreated.String() {
		t.Errorf("action = %q, want %q", got.Action, AuditActionAdministratorCreated)
	}
	if got.TargetType != AuditTargetAdministrator {
		t.Errorf("target_type = %q, want %q", got.TargetType, AuditTargetAdministrator)
	}
	if got.TargetID != target.String() {
		t.Errorf("target_id = %s, want %s", got.TargetID, target)
	}

	var meta map[string]any
	if err := json.Unmarshal(got.Metadata, &meta); err != nil {
		t.Fatalf("the metadata is not an object: %v (%s)", err, got.Metadata)
	}
	if meta["role"] != "moderator" {
		t.Errorf("metadata = %s, want the role that was granted", got.Metadata)
	}

	// The instant is the injected clock's, which is what makes an entry sort against the session
	// and status-history rows it describes (Docs/11 §9 — one row, one clock).
	if got.CreatedAt != timestamp(f.clk.Now()) {
		t.Errorf("created_at = %q, want the injected %q", got.CreatedAt, timestamp(f.clk.Now()))
	}
}

// TestAnEntryWithNoMetadataIsAnObjectAndNotNull.
//
// `null` is what a console renders by crashing. The column is NOT NULL DEFAULT '{}' and
// [marshalMetadata] writes an object for nothing, so a reader should never have to check — which is
// only true if the read side does not turn an empty string into invalid JSON on the way out.
func TestAnEntryWithNoMetadataIsAnObjectAndNotNull(t *testing.T) {
	f := newAuditSearchFixture(t)

	actor := uuid.New()
	f.record(t, actor, uuid.New(), AuditActionAdministratorSignedIn, nil)

	status, body := f.search(t, url.Values{"actor": {actor.String()}})
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", status, body)
	}
	if strings.Contains(body, `"metadata":null`) {
		t.Fatalf("an entry reported null metadata: %s", body)
	}

	entries, _, _ := entriesFrom(t, body)
	if len(entries) != 1 {
		t.Fatalf("the page carries %d entries, want 1", len(entries))
	}
	if string(entries[0].Metadata) != "{}" {
		t.Errorf("metadata = %s, want {}", entries[0].Metadata)
	}
}

// TestTheTrailIsRefusedWithoutACredential.
//
// The whole point of the table is that it records what administrators did. Reachable without one, it
// would be a public list of who runs the platform and what they touch.
func TestTheTrailIsRefusedWithoutACredential(t *testing.T) {
	f := newAuditSearchFixture(t)

	actor := uuid.New()
	f.record(t, actor, uuid.New(), AuditActionAdministratorSignedIn, nil)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/audit", nil)
	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.AuditTrail()).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if strings.Contains(rec.Body.String(), actor.String()) {
		t.Errorf("a refused request answered with entries anyway: %s", rec.Body)
	}
}

// TestNothingInThisPackageCanRewriteOrRemoveAnEntry.
//
// CLAUDE.md's invariant, checked from the side SHIP-165 is most able to break: this is the ticket
// that gives the trail a reader, and the obvious next thing somebody adds is a way to correct one.
//
// The Go half is that [AuditTrail] has one verb and [Auditor] has one verb, and neither is an update.
// The database half is `000003`s triggers, checked here against a row **this service wrote** rather
// than one the test inserted — which is the claim an operator actually cares about.
func TestNothingInThisPackageCanRewriteOrRemoveAnEntry(t *testing.T) {
	f := newAuditSearchFixture(t)

	entry := f.record(t, uuid.New(), uuid.New(), AuditActionAdministratorSignedIn, nil)

	if _, err := f.pool.Exec(t.Context(),
		`UPDATE audit_log SET reason = 'rewritten' WHERE id = $1`, entry); err == nil {
		t.Error("an entry this service wrote was rewritten")
	}
	if _, err := f.pool.Exec(t.Context(),
		`DELETE FROM audit_log WHERE id = $1`, entry); err == nil {
		t.Error("an entry this service wrote was deleted")
	}
}
