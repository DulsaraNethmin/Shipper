package admin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/profiles"
)

// SHIP-155 against a real PostgreSQL, through the real `internal/profiles` reader.
//
// # The port is that domain itself, not a double, and the object store is a fake
//
// The split is documents_test.go's and it transfers without change. What this ticket has to get
// right about *records* is the schema's — the documents are append-only, the newest of a kind is
// current, and a URL is minted rather than stored because there is no column to store one in — and a
// double would accept all of it. What it has to get right about *signing* is exercised end to end
// against MinIO by `scripts/verify/62-profiles.sh` and, for this endpoint, by
// `scripts/verify/90-admin.sh`. A stubbed signer here means no test in this file can fail when
// signing breaks, which is why the harness is where signing is demonstrated.
//
// A test file may import another domain. The boundary lint skips `_test.go` deliberately — "a test
// wires domains together in the same way cmd/api does" — so [testEvidence] is the same adapter
// `cmd/api/routes_admin.go` registers, written twice rather than shared, because package main is not
// importable and no test in it has a database to reach.
//
// # Every assertion about the access entry counts rows rather than reading a return value
//
// A read that answered correctly and wrote no entry is exactly the defect this ticket exists to
// prevent, and the response cannot distinguish it. That is wave 12's finding applied here: an
// assertion on an observed side effect beats one on the function under test.

// fakeEvidenceStore is an object store the tests drive, satisfying both ports `profiles` declares.
//
// It counts every signature so that two reads of one document are distinguishable: a store handing
// back the same string twice would let an assertion about freshness pass by accident.
type fakeEvidenceStore struct {
	mu sync.Mutex

	held   map[string]fakeStoredObject
	signed int

	// downloadsSigned is every key a download URL was minted for, in order. It is what lets a
	// test say that the viewer signed on this request rather than reading something stored.
	downloadsSigned []string
}

type fakeStoredObject struct {
	contentType   string
	contentLength int64
	etag          string
}

func newFakeEvidenceStore() *fakeEvidenceStore {
	return &fakeEvidenceStore{held: map[string]fakeStoredObject{}}
}

func (f *fakeEvidenceStore) PresignUpload(
	_ context.Context, key, contentType string, contentLength int64, ttl time.Duration,
) (string, time.Time, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	f.signed++

	// The upload is treated as having happened, because what this suite is about is the read.
	// documents_test.go is where an object that was never uploaded is exercised.
	f.held[key] = fakeStoredObject{
		contentType:   contentType,
		contentLength: contentLength,
		etag:          fmt.Sprintf("etag-%d", f.signed),
	}
	return fmt.Sprintf("https://store.example.test/%s?put=%d", key, f.signed),
		time.Now().Add(ttl), nil
}

func (f *fakeEvidenceStore) Stored(_ context.Context, key string) (string, int64, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	object, ok := f.held[key]
	if !ok {
		return "", 0, "", false, nil
	}
	return object.contentType, object.contentLength, object.etag, true, nil
}

func (f *fakeEvidenceStore) PresignDownload(
	_ context.Context, key string, ttl time.Duration,
) (string, time.Time, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	f.signed++
	f.downloadsSigned = append(f.downloadsSigned, key)

	// **Two query parameters, and the `&` between them is load-bearing.** A one-parameter URL
	// is not escaped by `encoding/json`; a real pre-signed URL carries six and every `&`
	// becomes `\u0026` in the response body. A fake that produced the unescaped shape made
	// TestTheDocumentViewerCarriesNoDurableObjectKey pass while the same check in
	// `scripts/verify/90-admin.sh` failed against MinIO — the fixture, not the code. See that
	// test's own note.
	return fmt.Sprintf("https://store.example.test/%s?X-Amz-Expires=%d&X-Amz-Signature=%d",
		key, int(ttl.Seconds()), f.signed), time.Now().Add(ttl), nil
}

// testEvidence adapts `internal/profiles` to the port this domain declares.
//
// Deliberately a copy of cmd/api's `providerEvidence` rather than an approximation of it: a double
// that answered its own outcomes would let a translation defect in the composition root pass every
// test here. What this cannot cover is the *registration* of that adapter, which is the harness's.
type testEvidence struct {
	documents *profiles.Documents
}

func (e testEvidence) EvidenceFor(
	ctx context.Context, r db.Runner, providerID uuid.UUID,
) ([]EvidenceDocument, bool, error) {

	links, err := e.documents.For(ctx, r, providerID)
	switch {
	case errors.Is(err, profiles.ErrNotProvider), errors.Is(err, profiles.ErrNoSuchProvider):
		return nil, false, nil
	case err != nil:
		return nil, false, err
	}

	out := make([]EvidenceDocument, 0, len(links))
	for _, link := range links {
		out = append(out, EvidenceDocument{
			ID:            link.ID,
			Kind:          string(link.Kind),
			ContentType:   link.ContentType,
			ContentLength: link.ContentLength,
			ETag:          link.ETag,
			SubmittedAt:   link.SubmittedAt,
			URL:           link.URL,
			ExpiresAt:     link.ExpiresAt,
		})
	}
	return out, true, nil
}

// evidenceFixture is the viewer, a handler, a signed-in administrator and the store behind it.
type evidenceFixture struct {
	pool      *pgxpool.Pool
	auth      *Authenticator
	handler   *Handler
	viewer    *Evidence
	auditor   *Auditor
	clk       *clock.Fixed
	store     *fakeEvidenceStore
	documents *profiles.Documents
	admin     Administrator
	token     string
	sequence  int
}

func newEvidenceFixture(t *testing.T, role Role) *evidenceFixture {
	t.Helper()

	creds, auth, pool, clk := adminAuth(t)

	auditor, err := NewAuditor(clk)
	if err != nil {
		t.Fatalf("building the audit writer: %v", err)
	}

	store := newFakeEvidenceStore()
	documents := profiles.NewDocuments(clock.System{}, store, store, testDocumentPolicy())

	viewer, err := NewEvidence(testEvidence{documents: documents}, auditor, pool)
	if err != nil {
		t.Fatalf("building the document viewer: %v", err)
	}

	services := testServices(t, creds, pool, clk)
	services.Evidence = viewer

	handler, err := NewHandler(services, pool, testLogger())
	if err != nil {
		t.Fatalf("building the handler: %v", err)
	}

	email := string(role) + "-evidence@example.com"
	administrator := anAdministrator(t, creds, email, role)
	issued, _, err := signIn(t, creds, email, testPassword, "10.0.155.1")
	if err != nil {
		t.Fatalf("signing in: %v", err)
	}

	return &evidenceFixture{
		pool: pool, auth: auth, handler: handler, viewer: viewer, auditor: auditor, clk: clk,
		store: store, documents: documents, admin: administrator, token: issued.Token,
	}
}

// testDocumentPolicy is what the platform will sign for, in this suite.
//
// The same four values `cmd/api` reads out of `internal/config`, written here as literals because a
// test has no environment. The lifetimes are short and different from each other, so an assertion
// that the download expiry came from the *download* lifetime cannot pass by both being the same.
func testDocumentPolicy() profiles.DocumentPolicy {
	return profiles.DocumentPolicy{
		MaxBytes:             5 << 20,
		AcceptedContentTypes: []string{"image/jpeg", "image/png"},
		UploadTTL:            15 * time.Minute,
		DownloadTTL:          5 * time.Minute,
	}
}

// provider registers a provider account, which `000200`'s trigger gives a Pending record.
func (f *evidenceFixture) provider(t *testing.T, name string) uuid.UUID {
	t.Helper()

	f.sequence++
	return newAccount(t, f.pool,
		name+"-evidence@example.com", "+61400155"+strconv.Itoa(100+f.sequence), "provider")
}

// customer registers a customer account, which has no verification record at all.
func (f *evidenceFixture) customer(t *testing.T, name string) uuid.UUID {
	t.Helper()

	f.sequence++
	return newAccount(t, f.pool,
		name+"-evidence@example.com", "+61400155"+strconv.Itoa(100+f.sequence), "customer")
}

// submit puts one document into the provider's file, through the real domain path.
//
// A URL is minted, the fake store records the object under that key, and the submission is recorded
// — which is what `POST /v1/provider/verification/documents` does one layer up. Writing the row with
// SQL instead would let the object key, the media type and the entity tag be whatever a test felt
// like, and every one of those is the store's answer rather than anybody's choice.
func (f *evidenceFixture) submit(t *testing.T, provider uuid.UUID, kind profiles.Kind) profiles.Document {
	t.Helper()

	upload, err := f.documents.PresignUpload(t.Context(), f.pool, provider,
		profiles.DocumentUploadRequest{ContentType: "image/jpeg", ContentLength: 52})
	if err != nil {
		t.Fatalf("minting an upload URL for %s: %v", kind, err)
	}

	document, err := f.documents.Submit(t.Context(), f.pool, provider, kind, upload.ObjectKey)
	if err != nil {
		t.Fatalf("submitting a %s: %v", kind, err)
	}
	return document
}

// open drives GET /v1/admin/verifications/{id}/documents through the guard and the handler.
func (f *evidenceFixture) open(t *testing.T, provider uuid.UUID) (int, string) {
	t.Helper()

	path := "/v1/admin/verifications/" + provider.String() + "/documents"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("id", provider.String())
	req.Header.Set(httpx.HeaderAuthorization, "Bearer "+f.token)

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.VerificationEvidence()).ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// evidencePage is the response, decoded.
type evidencePage struct {
	Data []struct {
		ID                string    `json:"id"`
		Kind              string    `json:"kind"`
		ContentType       string    `json:"content_type"`
		ContentLength     int64     `json:"content_length"`
		ETag              string    `json:"etag"`
		SubmittedAt       time.Time `json:"submitted_at"`
		DownloadURL       string    `json:"download_url"`
		DownloadExpiresAt time.Time `json:"download_expires_at"`
	} `json:"data"`
	NextCursor *string `json:"next_cursor"`
	HasMore    bool    `json:"has_more"`
}

func decodeEvidence(t *testing.T, body string) evidencePage {
	t.Helper()

	var page evidencePage
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the page: %v\n%s", err, body)
	}
	return page
}

// accessEntries is every `verification.evidence_viewed` entry against one provider.
func accessEntries(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID) []storedEntry {
	t.Helper()

	rows, err := pool.Query(t.Context(), `
		SELECT id, actor_type, actor_id, action, target_type, target_id, reason, metadata, created_at
		FROM audit_log
		WHERE action = $1 AND target_id = $2
		ORDER BY id`,
		AuditActionVerificationEvidenceViewed.String(), provider)
	if err != nil {
		t.Fatalf("reading the access entries: %v", err)
	}
	defer rows.Close()

	var found []storedEntry
	for rows.Next() {
		var e storedEntry
		if err := rows.Scan(&e.ID, &e.ActorType, &e.ActorID, &e.Action, &e.TargetType,
			&e.TargetID, &e.Reason, &e.Metadata, &e.CreatedAt); err != nil {
			t.Fatalf("reading an access entry: %v", err)
		}
		found = append(found, e)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("reading the access entries: %v", err)
	}
	return found
}

// TestVerificationImagesRenderThroughShortLivedSignedURLs is the *Done when*'s first clause.
//
// Three separate claims and each fails differently:
//
//   - **every document comes back with a URL.** One missing is a reviewer who cannot see one of
//     Docs/04 §3's four documents, which is the review not happening.
//   - **the URL is short-lived.** Its expiry is the whole of the control — nothing can revoke a
//     pre-signed URL — and it comes from the *download* lifetime rather than the upload one, which
//     is why the fixture's policy makes the two different numbers.
//   - **the URL is fresh on this read.** Two reads of the same document produce two URLs and two
//     signatures. A stored credential would produce one, and would look identical from a single
//     read.
func TestVerificationImagesRenderThroughShortLivedSignedURLs(t *testing.T) {
	f := newEvidenceFixture(t, RoleSupport)
	provider := f.provider(t, "renders")

	for _, kind := range profiles.Kinds {
		f.submit(t, provider, kind)
	}

	status, body := f.open(t, provider)
	if status != http.StatusOK {
		t.Fatalf("opening the file: status = %d, want 200 (%s)", status, body)
	}

	first := decodeEvidence(t, body)
	if len(first.Data) != len(profiles.Kinds) {
		t.Fatalf("the file holds %d documents, want Docs/04 §3's %d",
			len(first.Data), len(profiles.Kinds))
	}

	for _, d := range first.Data {
		if d.DownloadURL == "" {
			t.Errorf("the %s came back with no download URL; there is no other way to see it", d.Kind)
		}
		if d.DownloadExpiresAt.IsZero() {
			t.Errorf("the %s URL has no expiry. Nothing revokes a pre-signed URL, so an "+
				"expiry that is not there is a permanent link to somebody's identity "+
				"document", d.Kind)
		}
		if d.ETag == "" {
			t.Errorf("the %s carries no entity tag; `000201` stores one for this endpoint so "+
				"a viewer can tell the recorded bytes from bytes written over them", d.Kind)
		}
	}

	// The second read, which is where "fresh" is measured. Nothing about the provider's file
	// has changed between the two.
	signaturesBefore := len(f.store.downloadsSigned)

	status, body = f.open(t, provider)
	if status != http.StatusOK {
		t.Fatalf("re-opening the file: status = %d, want 200 (%s)", status, body)
	}
	second := decodeEvidence(t, body)

	if got := len(f.store.downloadsSigned) - signaturesBefore; got != len(profiles.Kinds) {
		t.Errorf("the second read signed %d URLs, want one per document (%d). A viewer that "+
			"reused a stored credential would sign none", got, len(profiles.Kinds))
	}

	for i := range first.Data {
		if first.Data[i].DownloadURL == second.Data[i].DownloadURL {
			t.Errorf("both reads of the %s answered with the same URL, so the credential did "+
				"not move with the request — which is exactly what a stored one looks like",
				first.Data[i].Kind)
		}
	}
}

// TestTheDocumentViewerCarriesNoDurableObjectKey.
//
// The key is a durable handle into the private bucket holding identity documents. A console has no
// operation that takes one, and every read is answered with a credential that expires instead — so
// a key on the wire as a *field* would be handing out the thing the expiry exists to make
// temporary. `internal/profiles` declines it on the provider's own endpoint for the same reason.
//
// # What this can assert, and the thing it deliberately does not
//
// **A pre-signed URL necessarily names the object it authorises**, so the key is inside
// `download_url` and cannot be removed from it — that is what a signature over a request is. The
// claim worth making is therefore narrower and still worth making: the key appears **nowhere the
// expiry does not reach**. Strip the signed URLs out of the body and the key is gone with them, so
// a console that stores the response keeps nothing that outlives the credential.
//
// The first version of this test asserted the key was absent from the whole body, went red, and was
// right to: an assertion that would fail on every correct implementation of a pre-signed URL is an
// assertion about the wrong thing. It is recorded here rather than quietly narrowed.
//
// # It is asserted on the parsed body with the URLs removed, and the first version was not
//
// Removing the *decoded* URL from the *raw* text does not work and looks as though it does:
// `encoding/json` escapes `&` to `\u0026`, a pre-signed URL carries six of them, and the
// substitution therefore matches nothing while reporting nothing. The harness caught it against
// MinIO and this suite did not, because the fake's URL had one query parameter and no `&`. Both
// halves are fixed: the fake signs a URL with an ampersand in it, and the check below re-serialises
// the parsed page with `download_url` removed rather than doing text surgery on the response.
//
// The `"object_key"` field check stays on the raw body, because a field the response type does not
// declare is exactly the one a decode would drop.
func TestTheDocumentViewerCarriesNoDurableObjectKey(t *testing.T) {
	f := newEvidenceFixture(t, RoleModerator)
	provider := f.provider(t, "nokey")

	document := f.submit(t, provider, profiles.KindLicence)
	if document.ObjectKey == "" {
		t.Fatal("the fixture recorded no object key, so every assertion below would pass vacuously")
	}

	status, body := f.open(t, provider)
	if status != http.StatusOK {
		t.Fatalf("opening the file: status = %d, want 200 (%s)", status, body)
	}
	if !strings.Contains(body, document.ObjectKey) {
		t.Fatal("the signed URL does not name the object, so this fixture is not signing " +
			"anything and the assertion below would pass against a viewer that leaked the key")
	}

	if strings.Contains(body, `"object_key"`) {
		t.Error("the response carries an object_key field. It is a durable handle into the " +
			"bucket holding identity documents, and it would outlive the credential beside it")
	}

	// The parsed page with every signed URL removed, re-serialised. What is left is what a
	// console could keep after the credential has expired.
	var page map[string]any
	if err := json.Unmarshal([]byte(body), &page); err != nil {
		t.Fatalf("decoding the page: %v", err)
	}
	rows, ok := page["data"].([]any)
	if !ok || len(rows) == 0 {
		t.Fatalf("the page carries no documents, so nothing is being stripped: %s", body)
	}
	for _, row := range rows {
		entry, ok := row.(map[string]any)
		if !ok {
			t.Fatalf("a document is not an object: %v", row)
		}
		if url, _ := entry["download_url"].(string); url == "" {
			t.Fatal("a document came back with no URL, so nothing is being stripped")
		}
		delete(entry, "download_url")
	}
	stripped, err := json.Marshal(page)
	if err != nil {
		t.Fatalf("re-encoding the page: %v", err)
	}
	if strings.Contains(string(stripped), document.ObjectKey) {
		t.Errorf("the object key %q survives outside the signed URL, so it outlives the "+
			"credential that was supposed to bound it.\n%s", document.ObjectKey, stripped)
	}
}

// TestOpeningAProvidersEvidenceIsAccessLogged is the *Done when*'s second clause.
//
// One entry per read, naming the administrator who made it and the provider whose file it was, with
// the images it handed over in the metadata. Docs/04 §6.6 asks a moderation record to carry its
// "evidence reference", and a signed URL cannot be revoked — so this entry is the only durable
// record that a particular person was given a link to another person's identity documents.
func TestOpeningAProvidersEvidenceIsAccessLogged(t *testing.T) {
	f := newEvidenceFixture(t, RoleSupport)
	provider := f.provider(t, "logged")

	licence := f.submit(t, provider, profiles.KindLicence)
	insurance := f.submit(t, provider, profiles.KindInsurance)

	if entries := accessEntries(t, f.pool, provider); len(entries) != 0 {
		t.Fatalf("%d access entries exist before anybody looked", len(entries))
	}

	status, body := f.open(t, provider)
	if status != http.StatusOK {
		t.Fatalf("opening the file: status = %d, want 200 (%s)", status, body)
	}

	entries := accessEntries(t, f.pool, provider)
	if len(entries) != 1 {
		t.Fatalf("one read wrote %d access entries, want exactly 1", len(entries))
	}

	entry := entries[0]
	if entry.ActorType != AuditActorAdmin.String() {
		t.Errorf("actor_type = %q, want %q", entry.ActorType, AuditActorAdmin)
	}
	if entry.ActorID == nil || *entry.ActorID != f.admin.ID {
		t.Errorf("actor_id = %v, want the administrator who looked, %s", entry.ActorID, f.admin.ID)
	}
	if entry.TargetType != AuditTargetUser {
		t.Errorf("target_type = %q, want %q — a support query asking what happened to this "+
			"account should return who looked at their documents", entry.TargetType, AuditTargetUser)
	}
	if entry.TargetID != provider {
		t.Errorf("target_id = %s, want the provider %s", entry.TargetID, provider)
	}
	if !entry.CreatedAt.Equal(f.clk.Now()) {
		t.Errorf("created_at = %s, want the injected %s (Docs/11 §9: one row, one clock)",
			entry.CreatedAt.UTC(), f.clk.Now())
	}

	// The evidence reference. Which images were rendered is the fact that makes a later
	// question — "was the insurance certificate current when this was reviewed?" — answerable,
	// because the newest row of a kind moves when the provider retakes it.
	count, ok := entry.Metadata["document_count"].(float64)
	if !ok || int(count) != 2 {
		t.Errorf("document_count = %v, want 2", entry.Metadata["document_count"])
	}
	recorded, ok := entry.Metadata["document_ids"].([]any)
	if !ok {
		t.Fatalf("document_ids = %v, want the identifiers of the images handed over",
			entry.Metadata["document_ids"])
	}
	seen := map[string]bool{}
	for _, id := range recorded {
		seen[fmt.Sprint(id)] = true
	}
	for _, want := range []profiles.Document{licence, insurance} {
		if !seen[want.ID.String()] {
			t.Errorf("the entry does not name the %s the reviewer was shown", want.Kind)
		}
	}

	// A second read is a second access. Two administrators looking at one person's licence is
	// two facts, and a trail that recorded the first only would say the second never happened.
	if _, _ = f.open(t, provider); len(accessEntries(t, f.pool, provider)) != 2 {
		t.Error("a second read wrote no second entry; an access log that records the first " +
			"look only is one that under-reports every provider anybody revisits")
	}
}

// TestAnEmptyFileIsStillAnAccessAndStillWritesAnEntry.
//
// A provider who has submitted nothing is the ordinary state of every provider on the day they
// register, and it is exactly what a reviewer about to reject somebody for supplying no evidence
// needs to see. It is an answer rather than a 404 — and the access is recorded the same way, because
// "an administrator opened this person's file" is the fact and whether there was anything in it is
// metadata.
func TestAnEmptyFileIsStillAnAccessAndStillWritesAnEntry(t *testing.T) {
	f := newEvidenceFixture(t, RoleSupport)
	provider := f.provider(t, "empty")

	status, body := f.open(t, provider)
	if status != http.StatusOK {
		t.Fatalf("opening an empty file: status = %d, want 200 (%s)", status, body)
	}

	page := decodeEvidence(t, body)
	if page.Data == nil {
		t.Error("data is null; a provider who has submitted nothing answers [], which is a " +
			"collection a console can render without a special case")
	}
	if len(page.Data) != 0 {
		t.Errorf("an empty file came back with %d documents", len(page.Data))
	}

	entries := accessEntries(t, f.pool, provider)
	if len(entries) != 1 {
		t.Fatalf("opening an empty file wrote %d entries, want 1", len(entries))
	}
	if count, _ := entries[0].Metadata["document_count"].(float64); int(count) != 0 {
		t.Errorf("document_count = %v, want 0", entries[0].Metadata["document_count"])
	}
}

// TestTheDocumentViewerRendersNothingWhenItsAccessEntryCannotBeWritten.
//
// **The clause of this ticket most easily met in name only.** An implementation that returns the
// documents and then writes an entry whose error it ignores passes every other test in this file:
// the images render, the URLs are fresh, the expiry is short. The only thing wrong with it is that
// the record is best-effort, and the circumstances that make the write fail are exactly the ones in
// which somebody would later want the record.
//
// The auditor is nil, past [NewEvidence]'s refusal, on [TestAPrivilegedActionIsRefusedWhenItsAuditEntryCannotBeWritten]'s
// pattern — and that shape is chosen deliberately. [Auditor.Record] refuses a nil receiver **before
// any SQL**, which is what wave 10 established is required to prove an `if err != nil` is checked at
// all: a failure raised by PostgreSQL aborts the transaction regardless, so a test driven by a
// constraint violation would pass against code that never read the error.
func TestTheDocumentViewerRendersNothingWhenItsAccessEntryCannotBeWritten(t *testing.T) {
	f := newEvidenceFixture(t, RoleSupport)
	provider := f.provider(t, "unwritable")
	f.submit(t, provider, profiles.KindLicence)

	// Built by hand, past the constructor's refusal.
	unwritable := &Evidence{
		documents: testEvidence{documents: f.documents},
		auditor:   nil,
		pool:      f.pool,
	}

	documents, err := unwritable.For(t.Context(), provider, f.admin.ID)
	if err == nil {
		t.Fatal("a provider's identity documents were rendered with no record of who was shown " +
			"them.\nSHIP-155: the access entry commits with the read or neither happens. A " +
			"signed URL cannot be revoked, so an unlogged read is a credential handed out " +
			"with nothing naming who holds it.")
	}
	if len(documents) != 0 {
		t.Errorf("the refused read still answered with %d documents", len(documents))
	}
	if entries := accessEntries(t, f.pool, provider); len(entries) != 0 {
		t.Errorf("%d access entries survived an unwritable trail", len(entries))
	}
}

// TestAnAccountWithNoVerificationRecordIsNotFoundAndIsNotLogged.
//
// A customer has no verification record — `000200` gives one to providers — so there is no file to
// open. It is a plain 404 because the caller is an administrator holding a permission over
// verifications and, unlike dispute intake, nothing is being kept from them.
//
// **And nothing is written**, which is the asymmetry worth asserting: an entry for a read that did
// not happen would record a review of a file nobody was shown, in a table with no way to take it
// back. It is also the shape that would let somebody probe which identifiers exist by reading the
// trail rather than the responses.
func TestAnAccountWithNoVerificationRecordIsNotFoundAndIsNotLogged(t *testing.T) {
	f := newEvidenceFixture(t, RoleModerator)

	for _, tc := range []struct {
		name string
		id   uuid.UUID
	}{
		{"a customer", f.customer(t, "notprovider")},
		{"an account that does not exist", uuid.New()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			status, body := f.open(t, tc.id)
			if status != http.StatusNotFound {
				t.Fatalf("status = %d, want 404 (%s)", status, body)
			}
			if entries := accessEntries(t, f.pool, tc.id); len(entries) != 0 {
				t.Errorf("%d access entries were written for a file nobody was shown",
					len(entries))
			}
		})
	}
}

// TestTheDocumentViewerNeedsAnAdministratorCredential.
//
// The evidence is behind `RequireAdmin` and behind `verifications.read`. Every role holds that
// permission, `support` included, and the handler's own comment argues why against a narrower one:
// Docs/01 §4.6 gives "review provider verification status" to support and Docs/04 §3 defines that
// review as looking at the images, so a support administrator who could see the queue and not the
// evidence could not perform the review the document assigns them.
//
// What that makes load-bearing is the entry, not the permission — which is what the rest of this
// file is about.
func TestTheDocumentViewerNeedsAnAdministratorCredential(t *testing.T) {
	f := newEvidenceFixture(t, RoleSupport)
	provider := f.provider(t, "unauthenticated")
	f.submit(t, provider, profiles.KindLicence)

	path := "/v1/admin/verifications/" + provider.String() + "/documents"
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.SetPathValue("id", provider.String())

	rec := httptest.NewRecorder()
	RequireAdmin(f.auth)(f.handler.VerificationEvidence()).ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("an unauthenticated read answered %d, want 401 (%s)", rec.Code, rec.Body)
	}
	if entries := accessEntries(t, f.pool, provider); len(entries) != 0 {
		t.Errorf("%d access entries were written for a caller the guard refused", len(entries))
	}
}
