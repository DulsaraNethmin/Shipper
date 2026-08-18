package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/profiles"
)

// SHIP-159 through the console, against a real PostgreSQL and the real `internal/profiles` reader.
//
// `internal/profiles`' own expiry_test.go is where the *query* is exercised — the horizons, the
// reduction to the current document of a kind, and the cursor. What is left over is this domain's
// half, and it is small on purpose: a permission, a page, and one field that exists because an empty
// page is ambiguous without it.
//
// The lead time is configured here rather than left empty, because an empty map is the shipping
// default and would make every assertion about the *expiring* half vacuous — which is precisely the
// shape wave 12 recorded as "a suite can be vacuous and green".

// testExpiryLead is the horizon this suite configures, for the one kind it configures.
//
// Sixty days on insurance certificates and nothing on the other three, so that "a kind with no
// configured lead time surfaces only once it has lapsed" is exercised by the same fixture rather
// than by a second one.
const testExpiryLead = 60 * 24 * time.Hour

// testExpiringDocuments is the port [testServices] wires in.
//
// Deliberately a copy of cmd/api's `expiringDocuments` rather than an approximation of it: a double
// that answered its own dates would let a translation defect in the composition root pass every test
// here. What this cannot cover is the *registration* of that adapter, which is the harness's.
func testExpiringDocuments(clk clock.Clock) ExpiringDocuments {
	return testExpiry{expiry: profiles.NewExpiry(clk, map[profiles.Kind]time.Duration{
		profiles.KindInsurance: testExpiryLead,
	})}
}

type testExpiry struct {
	expiry *profiles.Expiry
}

func (e testExpiry) DocumentsNearingExpiry(
	ctx context.Context, r db.Runner, q DocumentExpiryQuery,
) ([]ExpiringDocumentEntry, error) {

	found, err := e.expiry.Due(ctx, r, profiles.ExpiryQuery{
		Limit: q.Limit,
		After: profiles.ExpiryCursor{
			ExpiresAt:  q.After.ExpiresAt,
			DocumentID: q.After.DocumentID,
		},
	})
	if err != nil {
		return nil, err
	}

	out := make([]ExpiringDocumentEntry, 0, len(found))
	for _, d := range found {
		out = append(out, ExpiringDocumentEntry{
			DocumentID:        d.DocumentID,
			ProviderID:        d.ProviderID,
			Name:              d.Name,
			Email:             d.Email,
			Phone:             d.Phone,
			VerificationState: string(d.VerificationState),
			Kind:              string(d.Kind),
			ExpiresAt:         d.ExpiresAt,
			Expired:           d.Expired,
			SubmittedAt:       d.SubmittedAt,
		})
	}
	return out, nil
}

func (e testExpiry) ExpiryLeadTimes() map[string]time.Duration {
	held := e.expiry.LeadTimes()

	out := make(map[string]time.Duration, len(held))
	for kind, ahead := range held {
		out[string(kind)] = ahead
	}
	return out
}

// expiryFixture is the queue, a handler, a signed-in administrator and the store behind it.
type expiryFixture struct {
	pool      *pgxpool.Pool
	auth      *Authenticator
	handler   *Handler
	clk       *clock.Fixed
	store     *fakeEvidenceStore
	documents *profiles.Documents
	token     string
	sequence  int
}

func newExpiryFixture(t *testing.T, role Role) *expiryFixture {
	t.Helper()

	creds, auth, pool, clk := adminAuth(t)

	store := newFakeEvidenceStore()
	documents := profiles.NewDocuments(clk, store, store, testDocumentPolicy())

	handler, err := NewHandler(testServices(t, creds, pool, clk), pool, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	email := string(role) + "-expiry@example.com"
	anAdministrator(t, creds, email, role)
	issued, _, err := signIn(t, creds, email, testPassword, "10.0.159.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	return &expiryFixture{
		pool: pool, auth: auth, handler: handler, clk: clk, store: store,
		documents: documents, token: issued.Token,
	}
}

func (f *expiryFixture) provider(t *testing.T, name string) uuid.UUID {
	t.Helper()

	f.sequence++
	return newAccount(t, f.pool,
		name+"-expiry@example.com", "+61400159"+strconv.Itoa(100+f.sequence), "provider")
}

// submit records one document, optionally stating when it lapses.
func (f *expiryFixture) submit(
	t *testing.T, provider uuid.UUID, kind profiles.Kind, expires *time.Time,
) profiles.Document {
	t.Helper()

	upload, err := f.documents.PresignUpload(t.Context(), f.pool, provider,
		profiles.DocumentUploadRequest{ContentType: "image/jpeg", ContentLength: 52})
	if err != nil {
		t.Fatalf("minting an upload URL: %v", err)
	}

	document, err := f.documents.Submit(
		t.Context(), f.pool, provider, kind, upload.ObjectKey, expires)
	if err != nil {
		t.Fatalf("submitting a %s: %v", kind, err)
	}
	return document
}

// queue drives GET /v1/admin/moderation/expiring-documents through the guard and the handler.
func (f *expiryFixture) queue(t *testing.T, query string) (int, string) {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet,
		"/v1/admin/moderation/expiring-documents?"+query, nil)
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.ExpiringDocumentsQueue()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// expiryPage is the response, decoded.
type expiryPage struct {
	Data []struct {
		DocumentID        string    `json:"document_id"`
		ProviderID        string    `json:"provider_id"`
		Name              string    `json:"name"`
		Email             string    `json:"email"`
		Phone             string    `json:"phone"`
		VerificationState string    `json:"verification_state"`
		Kind              string    `json:"kind"`
		ExpiresAt         time.Time `json:"expires_at"`
		Expired           bool      `json:"expired"`
		SubmittedAt       time.Time `json:"submitted_at"`
	} `json:"data"`
	NextCursor string           `json:"next_cursor"`
	HasMore    bool             `json:"has_more"`
	LeadTimes  map[string]int64 `json:"lead_times"`
}

func decodeExpiry(t *testing.T, body string) expiryPage {
	t.Helper()

	var page expiryPage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the page: %v\n%s", err, body)
	}
	return page
}

// TestExpiringAndExpiredDocumentsBothSurfaceOnTheQueue is SHIP-159's *Done when*, clause by clause.
//
// "Expiring **and** expired provider documents surface **ahead of time**" is three claims:
//
//   - a document that has lapsed is on the queue and reads as expired;
//   - a document that has not lapsed but is inside its kind's configured horizon is on the queue and
//     reads as *not* expired — which is the "ahead of time" half, and the one that needs a number
//     somebody chose;
//   - a document outside every horizon is not on the queue at all.
func TestExpiringAndExpiredDocumentsBothSurfaceOnTheQueue(t *testing.T) {
	f := newExpiryFixture(t, RoleSupport)
	provider := f.provider(t, "both")

	now := f.clk.Now()
	lapsedAt := now.AddDate(0, 0, -30)
	expiringAt := now.Add(testExpiryLead / 2)
	distantAt := now.Add(testExpiryLead * 3)

	lapsed := f.submit(t, provider, profiles.KindInsurance, &lapsedAt)
	distant := f.submit(t, provider, profiles.KindLicence, &distantAt)

	second := f.provider(t, "both-two")
	expiring := f.submit(t, second, profiles.KindInsurance, &expiringAt)

	status, body := f.queue(t, "limit=50")
	if status != http.StatusOK {
		t.Fatalf("reading the queue: status = %d, want 200 (%s)", status, body)
	}
	page := decodeExpiry(t, body)

	found := map[string]bool{}
	expired := map[string]bool{}
	for _, e := range page.Data {
		found[e.DocumentID] = true
		expired[e.DocumentID] = e.Expired
	}

	if !found[lapsed.ID.String()] {
		t.Error("a document that lapsed a month ago is not on Docs/04 §5's expiry queue")
	} else if !expired[lapsed.ID.String()] {
		t.Error("the lapsed document does not read as expired, so a console cannot tell the two " +
			"halves of the queue apart")
	}

	if !found[expiring.ID.String()] {
		t.Error("a document inside its kind's configured horizon is not on the queue. That is " +
			"the \"ahead of time\" half of the *Done when* and the half a lead time exists for")
	} else if expired[expiring.ID.String()] {
		t.Error("a document that has not lapsed reads as expired; a provider would be chased as " +
			"though they could not trade")
	}

	if found[distant.ID.String()] {
		t.Error("a licence lapsing well beyond every configured horizon is on the queue. No lead " +
			"time is configured for licences, so it should surface only once it has lapsed")
	}

	// Soonest first: the one that has been out of date longest is at the top.
	if len(page.Data) < 2 {
		t.Fatalf("the queue holds %d entries, want at least 2", len(page.Data))
	}
	for i := 1; i < len(page.Data); i++ {
		if page.Data[i].ExpiresAt.Before(page.Data[i-1].ExpiresAt) {
			t.Errorf("entry %d expires before entry %d; the queue is not soonest first", i, i-1)
		}
	}
}

// TestTheQueueReportsTheHorizonsItUsed.
//
// **An empty "expiring" half means two different things and only one of them is a problem.**
// "Nothing is due" and "nobody has configured a lead time for that kind yet" render identically, and
// only the second is a reason to go and ask legal for Docs/04 §3's unanswered X-4. So the response
// carries the horizons the query actually used — read back off the domain rather than out of
// configuration a second time, so the legend cannot describe a different page from the one beside it.
func TestTheQueueReportsTheHorizonsItUsed(t *testing.T) {
	f := newExpiryFixture(t, RoleSupport)

	status, body := f.queue(t, "")
	if status != http.StatusOK {
		t.Fatalf("reading the queue: status = %d, want 200 (%s)", status, body)
	}
	page := decodeExpiry(t, body)

	if page.LeadTimes == nil {
		t.Fatal("lead_times is null. An empty object is the honest report that nothing is " +
			"configured, and it is the shipping default — null makes a console check before it " +
			"can explain its own empty page")
	}
	if got, want := page.LeadTimes[string(profiles.KindInsurance)], int64(testExpiryLead/time.Second); got != want {
		t.Errorf("lead_times[insurance] = %d seconds, want %d", got, want)
	}
	if _, configured := page.LeadTimes[string(profiles.KindLicence)]; configured {
		t.Error("lead_times names licence, for which nothing is configured. A kind with no " +
			"horizon must be absent rather than zero, so a console can tell \"nobody has " +
			"decided\" from \"decided: none\"")
	}
	if page.Data == nil {
		t.Error("data is null; an empty queue answers [], which is a collection a console can " +
			"render without a special case")
	}
}

// TestADocumentThatStatesNoExpiryIsNeverOnTheQueue.
//
// NULL means the platform was never told, which is not the same as "does not expire" and not the
// same as a date that has passed. Docs/04 §3's renewal cadence is Track-X row X-4 and is unanswered,
// so a document with no stated date must not be aged onto a queue by a number this platform invented.
func TestADocumentThatStatesNoExpiryIsNeverOnTheQueue(t *testing.T) {
	f := newExpiryFixture(t, RoleModerator)
	provider := f.provider(t, "silent")

	silent := f.submit(t, provider, profiles.KindInsurance, nil)

	status, body := f.queue(t, "limit=50")
	if status != http.StatusOK {
		t.Fatalf("reading the queue: status = %d, want 200 (%s)", status, body)
	}
	for _, e := range decodeExpiry(t, body).Data {
		if e.DocumentID == silent.ID.String() {
			t.Fatal("a document that states no expiry is on the queue. The platform was never " +
				"told when it lapses, and computing one from the submission date is the " +
				"renewal cadence Docs/04 §3 gives to legal and insurance advisers")
		}
	}
}

// TestTheExpiryQueueCarriesNothingCommercialAndNoCredential.
//
// Two invariants at once, and both are structural rather than a matter of what the mapping happens
// to copy today. A closed key set rather than a search for a word: SHIP-83 established that a
// spelling-based guard misses a field named anything at all.
//
//   - **No budget in any form** (Docs/01 §4.3). There is nowhere on this shape to put one.
//   - **No download URL and no object key.** This is a list of dates: a reviewer who wants to look
//     opens SHIP-155's viewer, which writes an access entry. A credential here would make every load
//     of this queue an unlogged read of everybody's identity documents at once.
func TestTheExpiryQueueCarriesNothingCommercialAndNoCredential(t *testing.T) {
	f := newExpiryFixture(t, RoleSupport)
	provider := f.provider(t, "shape")

	lapsedAt := f.clk.Now().AddDate(0, 0, -1)
	f.submit(t, provider, profiles.KindInsurance, &lapsedAt)

	status, body := f.queue(t, "limit=50")
	if status != http.StatusOK {
		t.Fatalf("reading the queue: status = %d, want 200 (%s)", status, body)
	}

	var raw struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		t.Fatalf("decoding the page: %v", err)
	}
	if len(raw.Data) == 0 {
		t.Fatal("the queue is empty, so this assertion would pass vacuously")
	}

	allowed := map[string]bool{
		"document_id": true, "provider_id": true, "name": true, "email": true, "phone": true,
		"verification_state": true, "kind": true, "expires_at": true, "expired": true,
		"submitted_at": true,
	}
	for _, entry := range raw.Data {
		for field := range entry {
			if !allowed[field] {
				t.Errorf("an entry carries %q, which is not one of the ten this queue answers "+
					"with. A budget reaches a provider through a field somebody added, and a "+
					"credential reaches a log through the same door", field)
			}
		}
		for _, want := range []string{"document_id", "provider_id", "kind", "expires_at", "expired"} {
			if _, present := entry[want]; !present {
				t.Errorf("an entry is missing %q", want)
			}
		}
	}
}

// TestTheExpiryQueueNeedsAnAdministratorCredentialAndTheModerationPermission.
//
// `RequireAdmin` for the credential and `moderation.read` inside the handler, which is the
// permission permissions.go already names SHIP-159 for. Every role holds it, `support` included:
// reading a queue is what the least-privileged role exists to be able to do.
//
// **And unlike SHIP-155's viewer, this writes nothing.** The two live one queue apart and the line
// between them is what the response contains — dates and contact details here, unrevocable links to
// somebody's identity documents there.
func TestTheExpiryQueueNeedsAnAdministratorCredentialAndWritesNothing(t *testing.T) {
	f := newExpiryFixture(t, RoleSupport)
	provider := f.provider(t, "guarded")

	lapsedAt := f.clk.Now().AddDate(0, 0, -1)
	f.submit(t, provider, profiles.KindInsurance, &lapsedAt)

	req := httptest.NewRequest(http.MethodGet, "/v1/admin/moderation/expiring-documents", nil)
	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.ExpiringDocumentsQueue()).ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated read answered %d, want 401 (%s)", rec.Code, rec.Body)
	}

	before := entryIDs(t, f.pool)
	if status, body := f.queue(t, "limit=50"); status != http.StatusOK {
		t.Fatalf("a support administrator could not read the queue: %d (%s)", status, body)
	}
	if added := entriesAddedSince(t, f.pool, before); len(added) != 0 {
		t.Errorf("reading the queue wrote %d audit entries. Docs/01 §5.1 asks for audit logs of "+
			"privileged actions; an entry per queue load would bury the actions in the reads, "+
			"and SHIP-155's viewer is the deliberate exception because it hands over a "+
			"credential", len(added))
	}
}
