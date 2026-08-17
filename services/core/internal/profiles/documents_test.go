package profiles

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-81b against a real PostgreSQL and a fake object store.
//
// # The database is real and the store is not, and the split is forced rather than lazy
//
// Everything this ticket has to get right about *records* is enforced by the schema — the four kinds
// are a CHECK, "one object is evidence for at most one provider" is a unique index, and the trail is
// append-only by trigger. A mocked repository would accept every one of those, so these run against
// the real thing, which is CLAUDE.md's rule and Docs/06 §4.1's.
//
// The object store is a fake because the platform is not in the upload path: what these tests are
// about is what the domain does with the store's *answer*, and the signature itself is exercised
// end to end against MinIO by `scripts/verify/62-profiles.sh`. That division is the one SHIP-114
// recorded as an open hole and SHIP-115 closed from the other end — a stubbed signer means no test
// here can fail when signing breaks, so the harness is where signing is demonstrated.

// fakeObjects is an object store the tests drive.
//
// It satisfies both ports this domain declares, which is what the adapter does too — and the tests
// that matter most below construct a [Documents] holding it as *both*, so a change that let one
// capability leak into the other would be visible here as well as in `cmd/api`'s assertion.
type fakeObjects struct {
	mu sync.Mutex

	// held is what the store contains, by key.
	held map[string]storedObject

	// uploadsSigned and downloadsSigned are every key a URL was minted for, in order. They are
	// what lets a test assert that a *fresh* URL was signed rather than a stored one reused.
	uploadsSigned   []string
	downloadsSigned []string

	// signed counts every URL this fake has produced, and is what makes each one distinguishable
	// from the last. A store handing back the same string twice would let a test about freshness
	// pass by accident.
	signed int

	uploadErr   error
	storedErr   error
	downloadErr error
}

type storedObject struct {
	contentType   string
	contentLength int64
	etag          string
}

func newFakeObjects() *fakeObjects {
	return &fakeObjects{held: map[string]storedObject{}}
}

func (f *fakeObjects) PresignUpload(
	_ context.Context, key, contentType string, contentLength int64, ttl time.Duration,
) (string, time.Time, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.uploadErr != nil {
		return "", time.Time{}, f.uploadErr
	}
	f.signed++
	f.uploadsSigned = append(f.uploadsSigned, key)
	return fmt.Sprintf("https://store.example.test/%s?put=%d", key, f.signed),
		testInstant.Add(ttl), nil
}

func (f *fakeObjects) Stored(_ context.Context, key string) (string, int64, string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.storedErr != nil {
		return "", 0, "", false, f.storedErr
	}
	object, found := f.held[key]
	return object.contentType, object.contentLength, object.etag, found, nil
}

func (f *fakeObjects) PresignDownload(
	_ context.Context, key string, ttl time.Duration,
) (string, time.Time, error) {

	f.mu.Lock()
	defer f.mu.Unlock()

	if f.downloadErr != nil {
		return "", time.Time{}, f.downloadErr
	}
	f.signed++
	f.downloadsSigned = append(f.downloadsSigned, key)
	return fmt.Sprintf("https://store.example.test/%s?get=%d", key, f.signed),
		testInstant.Add(ttl), nil
}

// put makes the store hold an acceptable object under key, as a completed upload would.
func (f *fakeObjects) put(key string, length int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.held[key] = storedObject{contentType: "image/jpeg", contentLength: length, etag: `"abc123"`}
}

// testDocumentPolicy is what the tests sign against.
//
// Written out rather than read from `internal/config`, because a test that took its limits from the
// same place the service does would agree with a configuration that had lost them.
func testDocumentPolicy() DocumentPolicy {
	return DocumentPolicy{
		MaxBytes:             1 << 20,
		AcceptedContentTypes: []string{"image/jpeg", "image/png"},
		UploadTTL:            15 * time.Minute,
		DownloadTTL:          5 * time.Minute,
	}
}

func newTestDocuments(store *fakeObjects) *Documents {
	return NewDocuments(clock.NewFixed(testInstant), store, store, testDocumentPolicy())
}

// upload asks for a URL and returns the key it was issued, failing the test if it was refused.
func upload(t *testing.T, docs *Documents, pool *pgxpool.Pool, provider uuid.UUID, length int64) string {
	t.Helper()

	issued, err := docs.PresignUpload(t.Context(), pool, provider,
		DocumentUploadRequest{ContentType: "image/jpeg", ContentLength: length})
	if err != nil {
		t.Fatalf("issuing an upload URL for %s: %v", provider, err)
	}
	return issued.ObjectKey
}

// submitted uploads, puts the bytes in the store, and records the document — the whole happy path.
func submitted(
	t *testing.T, docs *Documents, store *fakeObjects, pool *pgxpool.Pool,
	provider uuid.UUID, kind Kind,
) Document {
	t.Helper()

	key := upload(t, docs, pool, provider, 4096)
	store.put(key, 4096)

	document, err := docs.Submit(t.Context(), pool, provider, kind, key)
	if err != nil {
		t.Fatalf("submitting a %s for %s: %v", kind, provider, err)
	}
	return document
}

// documentCount is the number of rows on one provider's verification record.
//
// Read straight out of the table rather than through the domain, because the assertions that matter
// most below are about a **side effect** — whether a row exists and whose record it is on — and a
// count taken through the function under test is a count that agrees with it by construction.
func documentCount(t *testing.T, pool *pgxpool.Pool, provider uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM provider_verification_documents WHERE provider_id = $1`,
		provider).Scan(&n); err != nil {
		t.Fatalf("counting the documents of %s: %v", provider, err)
	}
	return n
}

// TestTheFourKindsAreTheConstraintsFourKinds is Docs/10 §3.4's pairing, in both directions.
//
// [Kinds] is a Go copy of `ck_provider_verification_documents_kind`, and neither is derived from the
// other — which is the point. A test that built its expectation by reading the constraint would
// agree with a constraint that had lost a kind, and one that only checked the Go list would agree
// with a Go list that had. Both directions is what makes the pair honest.
//
// The four names come from Docs/04 §3 and are written out here a third time on purpose: if this
// file, the migration and the document ever disagree, the document wins and this is where that
// argument starts.
func TestTheFourKindsAreTheConstraintsFourKinds(t *testing.T) {
	pool := pgtest.DB(t)

	var definition string
	if err := pool.QueryRow(t.Context(),
		`SELECT pg_get_constraintdef(oid) FROM pg_constraint
		 WHERE conname = 'ck_provider_verification_documents_kind'`,
	).Scan(&definition); err != nil {
		t.Fatalf("reading ck_provider_verification_documents_kind: %v", err)
	}

	quoted := regexp.MustCompile(`'([^']*)'`)
	var accepted []string
	for _, match := range quoted.FindAllStringSubmatch(definition, -1) {
		accepted = append(accepted, match[1])
	}

	// Docs/04 §3's four, written out rather than derived.
	document := []string{"licence", "registration", "insurance", "abn_evidence"}

	for _, kind := range document {
		if !containsString(accepted, kind) {
			t.Errorf("Docs/04 §3 collects %q and the CHECK does not accept it: %v", kind, accepted)
		}
		if !Kind(kind).Valid() {
			t.Errorf("Docs/04 §3 collects %q and profiles.Kinds does not carry it: %v", kind, Kinds)
		}
	}
	for _, kind := range accepted {
		if !containsString(document, kind) {
			t.Errorf("the CHECK accepts %q, which is not one of Docs/04 §3's four documents", kind)
		}
	}
	for _, kind := range Kinds {
		if !containsString(document, string(kind)) {
			t.Errorf("profiles.Kinds carries %q, which is not one of Docs/04 §3's four documents", kind)
		}
	}
	if len(Kinds) != len(document) {
		t.Errorf("profiles.Kinds has %d entries and Docs/04 §3 has %d", len(Kinds), len(document))
	}
}

// TestTheUploadUrlBindsWhatTheClientDeclaredAndNamesTheProvider.
//
// The *Done when*'s "uploads it directly through a short-lived pre-signed URL, with the API never in
// the path of the bytes" has two halves. This is the half a Go test can hold: the platform chooses
// the key, signs the type and the length the client declared, and hands back a lifetime. That the
// bytes then reach a host which is not the API is `scripts/verify/62-profiles.sh`'s.
func TestTheUploadUrlBindsWhatTheClientDeclaredAndNamesTheProvider(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-upload@example.com", "0415000001")

	issued, err := docs.PresignUpload(t.Context(), pool, provider,
		DocumentUploadRequest{ContentType: "IMAGE/JPEG ", ContentLength: 4096})
	if err != nil {
		t.Fatalf("issuing an upload URL: %v", err)
	}

	if got, want := issued.ContentType, "image/jpeg"; got != want {
		t.Errorf("content type %q, want %q — the platform decides one spelling before it signs", got, want)
	}
	if issued.ContentLength != 4096 {
		t.Errorf("content length %d, want 4096", issued.ContentLength)
	}
	if want := documentKeyPrefix + "/" + provider.String() + "/"; !strings.HasPrefix(issued.ObjectKey, want) {
		t.Errorf("object key %q is not prefixed %q", issued.ObjectKey, want)
	}
	if !keyBelongsToProvider(issued.ObjectKey, provider) {
		t.Errorf("the platform issued %q, which its own ownership check refuses", issued.ObjectKey)
	}
	if got, want := issued.ExpiresAt, testInstant.Add(testDocumentPolicy().UploadTTL); !got.Equal(want) {
		t.Errorf("expires at %s, want %s", got, want)
	}

	// **Nothing was written.** An object in the bucket is bytes with a key until something says
	// whose verification record it belongs to, and issuing a URL is not that something.
	if n := documentCount(t, pool, provider); n != 0 {
		t.Errorf("issuing an upload URL wrote %d document rows, want 0", n)
	}

	// A second request mints a second key, so a client holding an old one cannot ask for a fresh
	// URL over an object that already holds evidence somebody reviewed.
	again, err := docs.PresignUpload(t.Context(), pool, provider,
		DocumentUploadRequest{ContentType: "image/jpeg", ContentLength: 4096})
	if err != nil {
		t.Fatalf("issuing a second upload URL: %v", err)
	}
	if again.ObjectKey == issued.ObjectKey {
		t.Errorf("two requests were issued the same object key %q", issued.ObjectKey)
	}
}

// TestOnlyAProviderIsIssuedAnUploadUrl.
//
// Read from `users.role` rather than from the token's claim, which is SHIP-78a's distinction: the
// claim is evidence about the token and the column is the fact.
func TestOnlyAProviderIsIssuedAnUploadUrl(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	customer := newAccount(t, pool, "documents-customer@example.com", "0415000002", "customer")

	_, err := docs.PresignUpload(t.Context(), pool, customer,
		DocumentUploadRequest{ContentType: "image/jpeg", ContentLength: 4096})
	if !errors.Is(err, ErrNotProvider) {
		t.Fatalf("a customer was issued an upload URL: %v", err)
	}
	if len(store.uploadsSigned) != 0 {
		t.Errorf("the signer was called %d times for a customer", len(store.uploadsSigned))
	}
}

// TestAnUnsignableRequestIsRefusedBeforeAnythingIsSigned.
//
// Validation runs first and discloses nothing — a client is told about its own body — and the signer
// is never reached, which matters because a pre-signed URL cannot be revoked once it exists.
func TestAnUnsignableRequestIsRefusedBeforeAnythingIsSigned(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-unsignable@example.com", "0415000003")

	for _, c := range []struct {
		name  string
		req   DocumentUploadRequest
		field string
	}{
		{"no content type", DocumentUploadRequest{ContentLength: 4096}, "content_type"},
		{"a type the platform will not store",
			DocumentUploadRequest{ContentType: "image/svg+xml", ContentLength: 4096}, "content_type"},
		{"no length", DocumentUploadRequest{ContentType: "image/jpeg"}, "content_length"},
		{"over the limit",
			DocumentUploadRequest{ContentType: "image/jpeg", ContentLength: 1 << 21}, "content_length"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := docs.PresignUpload(t.Context(), pool, provider, c.req)

			var apiErr *httpx.Error
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want a validation failure", err)
			}
			if !mentionsField(apiErr, c.field) {
				t.Errorf("the refusal does not name %s: %v", c.field, apiErr)
			}
		})
	}

	if len(store.uploadsSigned) != 0 {
		t.Errorf("%d URLs were signed for requests the platform refused", len(store.uploadsSigned))
	}
}

// TestASubmittedDocumentRecordsWhatTheStoreReportedRatherThanWhatTheClientSaid.
//
// The whole reason a row is not written when the URL is issued: the platform is not in the upload
// path, so the only moment it can know an object exists is when it asks. What is stored is the
// store's answer, which is the copy that is still right when the client's claim was not.
func TestASubmittedDocumentRecordsWhatTheStoreReportedRatherThanWhatTheClientSaid(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-record@example.com", "0415000004")

	// The client declares 4096 bytes and the store holds 5120. The record follows the store.
	key := upload(t, docs, pool, provider, 4096)
	store.put(key, 5120)

	document, err := docs.Submit(t.Context(), pool, provider, KindLicence, key)
	if err != nil {
		t.Fatalf("submitting the licence: %v", err)
	}

	if document.ContentLength != 5120 {
		t.Errorf("recorded %d bytes, want the 5120 the store reported", document.ContentLength)
	}
	if document.Kind != KindLicence {
		t.Errorf("recorded kind %q, want %q", document.Kind, KindLicence)
	}
	if document.ProviderID != provider {
		t.Errorf("recorded against %s, want %s", document.ProviderID, provider)
	}
	if document.ETag == "" {
		t.Error("no entity tag was recorded, so a later overwrite could not be detected")
	}
	if document.SubmittedAt.IsZero() {
		t.Error("no submission clock was recorded")
	}

	// The row exists, on this provider's verification record, and it says what the domain says.
	var (
		storedKind  string
		storedOwner uuid.UUID
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT kind, provider_id FROM provider_verification_documents WHERE id = $1`,
		document.ID).Scan(&storedKind, &storedOwner); err != nil {
		t.Fatalf("reading the recorded document back: %v", err)
	}
	if storedKind != string(KindLicence) || storedOwner != provider {
		t.Errorf("the row says (%s, %s), want (%s, %s)", storedKind, storedOwner, KindLicence, provider)
	}
}

// TestADocumentIsNeverRecordedAgainstAnotherProvidersVerificationRecord.
//
// # This is the separation test, and it asserts on rows rather than on the function it calls
//
// The failure it exists to catch passes every other assertion in this file: the URL is minted, the
// bytes upload, a row is written, the kind is recorded and the document reads back through a fresh
// signed URL. **All that is wrong is whose file it landed in** — so a test that only checked
// `Submit`'s error would be checking the very thing a dropped ownership check removes.
//
// So the assertions here are side effects taken straight out of the table: the count on each
// provider's record before and after, and the cross-table pair `(documents.provider_id, object_key)`
// — which is what actually says a document sits on the record its object was minted for.
//
// Docs/11 §3 records this shape as the one that has killed the most mutations in this repository.
func TestADocumentIsNeverRecordedAgainstAnotherProvidersVerificationRecord(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	alice := newProvider(t, pool, "documents-alice@example.com", "0415000005")
	mallory := newProvider(t, pool, "documents-mallory@example.com", "0415000006")

	// Alice is issued a key and uploads her licence. She has not submitted it yet.
	alicesKey := upload(t, docs, pool, alice, 4096)
	store.put(alicesKey, 4096)

	before := documentCount(t, pool, alice)
	if before != 0 {
		t.Fatalf("alice starts with %d documents, want 0", before)
	}

	// Mallory holds Alice's key — a key is easy to pass on, which is exactly why prefixing it with
	// its owner is a control and not a convenience — and submits it as her own insurance.
	_, err := docs.Submit(t.Context(), pool, mallory, KindInsurance, alicesKey)
	if !errors.Is(err, ErrDocumentNotForThisProvider) {
		t.Errorf("submitting another provider's object answered %v, want ErrDocumentNotForThisProvider", err)
	}

	// The side effects, which are what would still be true if the refusal above were removed.
	if n := documentCount(t, pool, mallory); n != 0 {
		t.Errorf("mallory's verification record carries %d documents, want 0 — "+
			"one provider has attached another provider's evidence to their own file", n)
	}
	if n := documentCount(t, pool, alice); n != before {
		t.Errorf("alice's record moved from %d documents to %d without alice doing anything", before, n)
	}

	// The cross-table pair: every recorded document sits on the verification record its object key
	// was minted for. This holds for every row in the table rather than for the one just attempted,
	// so it fails on any path that could ever write a mismatched pair.
	var mismatched int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM provider_verification_documents
		  WHERE object_key NOT LIKE 'verification/' || provider_id::text || '/%'`,
	).Scan(&mismatched); err != nil {
		t.Fatalf("checking that every document sits on the record its key names: %v", err)
	}
	if mismatched != 0 {
		t.Errorf("%d documents are recorded against a verification record their object key does "+
			"not name", mismatched)
	}

	// And the store was never asked about Alice's object on Mallory's behalf, which is what stops
	// this endpoint being a way to probe whether somebody else's upload has landed.
	for _, key := range store.downloadsSigned {
		if key == alicesKey {
			t.Errorf("a download was signed for %q on behalf of another provider", alicesKey)
		}
	}

	// Alice can still submit her own document, so the refusal above is about ownership rather than
	// about the object having been touched.
	if _, err := docs.Submit(t.Context(), pool, alice, KindLicence, alicesKey); err != nil {
		t.Fatalf("alice could not submit her own document afterwards: %v", err)
	}
	if n := documentCount(t, pool, alice); n != 1 {
		t.Errorf("alice's record carries %d documents after submitting one, want 1", n)
	}
}

// TestOneObjectIsEvidenceForAtMostOneProvider.
//
// The layer underneath [keyBelongsToProvider], and it refuses a different thing: that one refuses a
// key minted for somebody else, and `uq_provider_verification_documents_object_key` refuses a key
// already spent. Both are access control — a single photograph of a licence must not verify two
// people, and must not verify one person twice under two kinds.
func TestOneObjectIsEvidenceForAtMostOneProvider(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-once@example.com", "0415000007")

	key := upload(t, docs, pool, provider, 4096)
	store.put(key, 4096)

	if _, err := docs.Submit(t.Context(), pool, provider, KindLicence, key); err != nil {
		t.Fatalf("submitting the licence: %v", err)
	}

	_, err := docs.Submit(t.Context(), pool, provider, KindInsurance, key)
	if !errors.Is(err, ErrDocumentAlreadyRecorded) {
		t.Errorf("recording one object as two documents answered %v, want ErrDocumentAlreadyRecorded", err)
	}
	if n := documentCount(t, pool, provider); n != 1 {
		t.Errorf("the record carries %d documents, want 1 — one image became two pieces of evidence", n)
	}
}

// TestTheFourKindsAreSubmittedIndependentlyAndARetakeIsANewRow.
//
// Docs/04 §3 collects four documents and §4 puts a provider in Restricted "pending clarification or
// document renewal", so each kind is independently capturable and re-uploadable. `000201` is
// append-only, so a retake is a second row and the newest of a kind is current — the replaced image
// is what an administrator already looked at.
func TestTheFourKindsAreSubmittedIndependentlyAndARetakeIsANewRow(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-four@example.com", "0415000008")

	for _, kind := range Kinds {
		submitted(t, docs, store, pool, provider, kind)
	}
	if n := documentCount(t, pool, provider); n != len(Kinds) {
		t.Fatalf("the record carries %d documents after submitting each kind, want %d", n, len(Kinds))
	}

	// The insurance certificate was illegible. The provider photographs it again.
	retake := submitted(t, docs, store, pool, provider, KindInsurance)

	if n := documentCount(t, pool, provider); n != len(Kinds)+1 {
		t.Errorf("the record carries %d documents after one retake, want %d — a retake replaced "+
			"the image an administrator had already reviewed", n, len(Kinds)+1)
	}

	links, err := docs.For(t.Context(), pool, provider)
	if err != nil {
		t.Fatalf("reading the documents back: %v", err)
	}
	if len(links) != len(Kinds)+1 {
		t.Fatalf("read back %d documents, want %d", len(links), len(Kinds)+1)
	}

	// Newest first within each kind, so "the current insurance certificate" is the first row of
	// its kind rather than a question the client has to sort out.
	var insurance []DocumentLink
	for _, link := range links {
		if link.Kind == KindInsurance {
			insurance = append(insurance, link)
		}
	}
	if len(insurance) != 2 {
		t.Fatalf("read back %d insurance documents, want 2", len(insurance))
	}
	if insurance[0].ID != retake.ID {
		t.Errorf("the first insurance document is %s, want the retake %s", insurance[0].ID, retake.ID)
	}
}

// TestADocumentIsReachableOnlyByAFreshSignedUrl.
//
// The last clause of the *Done when*. Nothing stored holds a URL — the schema has no column for one
// — so every reader pays for its own signature with its own expiry, and two reads of the same
// document produce two different credentials.
func TestADocumentIsReachableOnlyByAFreshSignedUrl(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-fresh@example.com", "0415000009")
	document := submitted(t, docs, store, pool, provider, KindRegistration)

	first, err := docs.For(t.Context(), pool, provider)
	if err != nil {
		t.Fatalf("reading the documents: %v", err)
	}
	second, err := docs.For(t.Context(), pool, provider)
	if err != nil {
		t.Fatalf("reading the documents a second time: %v", err)
	}
	if len(first) != 1 || len(second) != 1 {
		t.Fatalf("read %d and %d documents, want 1 each", len(first), len(second))
	}

	if first[0].URL == "" {
		t.Fatal("the document came back with no URL, so nothing can render it")
	}
	if first[0].URL == second[0].URL {
		t.Errorf("two reads produced the same URL %q — a stored credential is one nobody is "+
			"watching the clock on", first[0].URL)
	}
	if got, want := first[0].ExpiresAt, testInstant.Add(testDocumentPolicy().DownloadTTL); !got.Equal(want) {
		t.Errorf("the download expires at %s, want %s", got, want)
	}
	if len(store.downloadsSigned) != 2 {
		t.Errorf("the signer was called %d times for two reads, want 2", len(store.downloadsSigned))
	}

	// There is no column a URL could have come out of, which is what makes the freshness above a
	// property of the schema rather than of this implementation.
	var columns int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM information_schema.columns
		  WHERE table_name = 'provider_verification_documents'
		    AND (column_name LIKE '%url%' OR column_name LIKE '%link%')`,
	).Scan(&columns); err != nil {
		t.Fatalf("reading the table's columns: %v", err)
	}
	if columns != 0 {
		t.Errorf("provider_verification_documents has %d URL-shaped columns, want 0", columns)
	}

	_ = document
}

// TestAProviderReadsTheirOwnDocumentsAndNobodyElses.
//
// There is no parameter for whose, so a stranger's evidence is not refused — it is never selected.
// The test drives two providers in one database, which is what makes it fail in either direction if
// the query ever stopped being scoped.
func TestAProviderReadsTheirOwnDocumentsAndNobodyElses(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	mine := newProvider(t, pool, "documents-mine@example.com", "0415000010")
	theirs := newProvider(t, pool, "documents-theirs@example.com", "0415000011")

	submitted(t, docs, store, pool, mine, KindLicence)
	submitted(t, docs, store, pool, theirs, KindLicence)
	submitted(t, docs, store, pool, theirs, KindInsurance)

	read, err := docs.For(t.Context(), pool, mine)
	if err != nil {
		t.Fatalf("reading my documents: %v", err)
	}
	if len(read) != 1 {
		t.Fatalf("read %d documents, want the 1 that is mine", len(read))
	}
	if read[0].ProviderID != mine {
		t.Errorf("read a document belonging to %s", read[0].ProviderID)
	}

	customer := newAccount(t, pool, "documents-reader@example.com", "0415000012", "customer")
	if _, err := docs.For(t.Context(), pool, customer); !errors.Is(err, ErrNotProvider) {
		t.Errorf("a customer read the document list: %v", err)
	}
}

// TestAnObjectTheStoreDoesNotHoldIsNotRecorded.
//
// The asymmetry `000201` and Docs/01 §4.4 both turn on: a row with no object is refused, and an
// object with no row is expected. A record asserting that a licence was submitted, held by a
// platform that never looked, is not evidence.
func TestAnObjectTheStoreDoesNotHoldIsNotRecorded(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-missing@example.com", "0415000013")
	key := upload(t, docs, pool, provider, 4096)

	// The URL was issued and the PUT never happened, or failed halfway.
	_, err := docs.Submit(t.Context(), pool, provider, KindLicence, key)
	if !errors.Is(err, ErrDocumentNotUploaded) {
		t.Errorf("submitting an object the store does not hold answered %v, want ErrDocumentNotUploaded", err)
	}
	if n := documentCount(t, pool, provider); n != 0 {
		t.Errorf("the record carries %d documents, want 0 — a row was written for an object "+
			"nobody has looked at", n)
	}
}

// TestAnObjectOutsideThePolicyIsNotRecorded.
//
// The limits were signed into the URL, so the store refuses an upload that changed them — and that
// guard is entirely outside this package, because these tests stub the signer. Reading the stored
// values back and judging them closes it from the other end: whatever route an object took into the
// bucket, it is measured against the platform's current limits before it becomes evidence.
func TestAnObjectOutsideThePolicyIsNotRecorded(t *testing.T) {
	pool := pgtest.DB(t)

	for i, c := range []struct {
		name   string
		object storedObject
	}{
		{"a type the platform will not store",
			storedObject{contentType: "image/svg+xml", contentLength: 4096, etag: `"a"`}},
		{"nothing at all", storedObject{contentType: "image/jpeg", contentLength: 0, etag: `"a"`}},
		{"over the limit",
			storedObject{contentType: "image/jpeg", contentLength: 1 << 21, etag: `"a"`}},
		{"no entity tag", storedObject{contentType: "image/jpeg", contentLength: 4096}},
	} {
		t.Run(c.name, func(t *testing.T) {
			store := newFakeObjects()
			docs := newTestDocuments(store)

			// The mobile number is derived from the case's index rather than from its name:
			// "nothing at all" and "over the limit" are both fourteen characters, so a number
			// built from the length collided on the phone unique index and failed one case for a
			// reason that had nothing to do with the policy.
			provider := newProvider(t, pool,
				fmt.Sprintf("documents-policy-%s@example.com", strings.ReplaceAll(c.name, " ", "-")),
				fmt.Sprintf("04150001%02d", 40+i))

			key := upload(t, docs, pool, provider, 4096)
			store.mu.Lock()
			store.held[key] = c.object
			store.mu.Unlock()

			_, err := docs.Submit(t.Context(), pool, provider, KindLicence, key)
			if !errors.Is(err, ErrDocumentRejected) {
				t.Errorf("err = %v, want ErrDocumentRejected", err)
			}
			if n := documentCount(t, pool, provider); n != 0 {
				t.Errorf("the record carries %d documents, want 0", n)
			}
		})
	}
}

// TestAKindDocs04DoesNotHaveIsRefusedWithTheFieldNamed.
//
// A constraint name is a worse explanation than a field error (Docs/10 §4.6), and the CHECK is still
// there underneath as the layer that survives a rewrite of this one.
func TestAKindDocs04DoesNotHaveIsRefusedWithTheFieldNamed(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-kind@example.com", "0415000020")
	key := upload(t, docs, pool, provider, 4096)
	store.put(key, 4096)

	_, err := docs.Submit(t.Context(), pool, provider, Kind("passport"), key)

	var apiErr *httpx.Error
	if !errors.As(err, &apiErr) {
		t.Fatalf("err = %v, want a validation failure", err)
	}
	if !mentionsField(apiErr, "kind") {
		t.Errorf("the refusal does not name the kind field: %v", apiErr)
	}
	if n := documentCount(t, pool, provider); n != 0 {
		t.Errorf("a document of an unknown kind was recorded")
	}
}

// TestTheDocumentTrailIsAppendOnly.
//
// Docs/04 §6.6 requires "the decision, actor, timestamp, reason, and evidence reference" of every
// moderation outcome. A row that can be updated is an evidence reference that can be pointed at a
// different image after a decision was taken against the first one.
func TestTheDocumentTrailIsAppendOnly(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	docs := newTestDocuments(store)

	provider := newProvider(t, pool, "documents-appendonly@example.com", "0415000021")
	document := submitted(t, docs, store, pool, provider, KindLicence)

	if _, err := pool.Exec(t.Context(),
		`UPDATE provider_verification_documents SET kind = 'insurance' WHERE id = $1`,
		document.ID); err == nil {
		t.Error("a recorded document was rewritten")
	}
	if _, err := pool.Exec(t.Context(),
		`DELETE FROM provider_verification_documents WHERE id = $1`, document.ID); err == nil {
		t.Error("a recorded document was deleted")
	}
	if n := documentCount(t, pool, provider); n != 1 {
		t.Errorf("the record carries %d documents, want 1", n)
	}
}

// TestThereIsNoExpiryColumn.
//
// Docs/04 §3 gives the renewal cadence to legal and insurance advisers — Track-X row X-4 — and says
// it "remains genuinely outside engineering's competence to settle". SHIP-81b's *Done when* does not
// mention expiry, so guessing one here would be the platform enforcing a number nobody decided.
//
// **This test exists to be deleted by SHIP-159**, deliberately: whoever adds the column is the
// person who has X-4's answer in front of them, and a failing test is where they will read why the
// column was left out rather than forgotten.
func TestThereIsNoExpiryColumn(t *testing.T) {
	pool := pgtest.DB(t)

	var columns []string
	rows, err := pool.Query(t.Context(),
		`SELECT column_name FROM information_schema.columns
		  WHERE table_name = 'provider_verification_documents'
		    AND (column_name LIKE '%expir%' OR column_name LIKE '%renew%')`)
	if err != nil {
		t.Fatalf("reading the table's columns: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var column string
		if err := rows.Scan(&column); err != nil {
			t.Fatalf("scanning a column name: %v", err)
		}
		columns = append(columns, column)
	}
	if len(columns) != 0 {
		t.Errorf("provider_verification_documents carries %v. Docs/04 §3 gives the renewal cadence "+
			"to X-4; SHIP-159 is the ticket that adds this, and deleting this test is part of it",
			columns)
	}
}

// mentionsField reports whether an error contract failure names a field.
func mentionsField(err *httpx.Error, field string) bool {
	for _, detail := range err.Details {
		if detail.Field == field {
			return true
		}
	}
	return false
}

// postAs sends an authenticated request with a JSON body.
//
// The role on the subject is deliberately customer, whatever the account is — see [as] for why.
func postAs(t *testing.T, h http.Handler, caller uuid.UUID, target, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authctx.WithSubject(req.Context(), authctx.Subject{
		UserID:    caller.String(),
		Role:      authctx.RoleCustomer,
		SessionID: uuid.Must(uuid.NewV7()).String(),
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// TestTheDocumentEndpointsOnTheWire drives the three routes through the real handlers.
//
// # What this adds over the domain tests above
//
// The status codes, the JSON field names and the two deliberate absences. A domain test proves what
// the service decided; this proves what a client receives, which is the half a client is written
// against — and the two absences below are decisions that exist only at this layer, so nothing else
// in this package could catch their loss.
func TestTheDocumentEndpointsOnTheWire(t *testing.T) {
	pool := pgtest.DB(t)
	store := newFakeObjects()
	router := newTestRouterWith(t, pool, store)

	provider := newProvider(t, pool, "documents-wire@example.com", "0415000030")

	// --- the upload URL ---------------------------------------------------------------------
	issued := postAs(t, router, provider, "/v1/provider/verification/documents/uploads",
		`{"content_type":"image/jpeg","content_length":4096}`)
	if issued.Code != http.StatusOK {
		t.Fatalf("issuing an upload URL answered %d, want 200 — nothing was created", issued.Code)
	}

	slot := decode[struct {
		ObjectKey     string `json:"object_key"`
		UploadURL     string `json:"upload_url"`
		Method        string `json:"method"`
		ContentType   string `json:"content_type"`
		ContentLength int64  `json:"content_length"`
		ExpiresAt     string `json:"expires_at"`
	}](t, issued)

	if slot.Method != http.MethodPut {
		t.Errorf("method %q, want PUT — it is not guessable from a JSON body", slot.Method)
	}
	if slot.UploadURL == "" || slot.ExpiresAt == "" {
		t.Errorf("the response carries no URL or no expiry: %s", issued.Body)
	}
	if slot.ContentType != "image/jpeg" || slot.ContentLength != 4096 {
		t.Errorf("the response describes %s of %d bytes", slot.ContentType, slot.ContentLength)
	}

	// --- submitting it ----------------------------------------------------------------------
	store.put(slot.ObjectKey, 4096)

	created := postAs(t, router, provider, "/v1/provider/verification/documents",
		fmt.Sprintf(`{"kind":"licence","object_key":%q}`, slot.ObjectKey))
	if created.Code != http.StatusCreated {
		t.Fatalf("submitting answered %d, want 201: %s", created.Code, created.Body)
	}

	// **No download URL on the 201.** The idempotency middleware stores this body and replays it,
	// so a credential here would be one at rest in Redis with a clock nobody was watching.
	if strings.Contains(created.Body.String(), "download_url") {
		t.Errorf("the submission response carries a download URL: %s", created.Body)
	}
	// **No object key on the wire, ever.** It is a durable handle into the bucket holding identity
	// documents, and every read is answered with a short-lived URL instead.
	if strings.Contains(created.Body.String(), slot.ObjectKey) {
		t.Errorf("the submission response carries the object key: %s", created.Body)
	}

	// --- reading them back ------------------------------------------------------------------
	read := as(t, router, provider, http.MethodGet, "/v1/provider/verification/documents")
	if read.Code != http.StatusOK {
		t.Fatalf("reading the documents answered %d, want 200: %s", read.Code, read.Body)
	}

	list := decode[struct {
		Data []struct {
			ID                string `json:"id"`
			Kind              string `json:"kind"`
			ContentType       string `json:"content_type"`
			ContentLength     int64  `json:"content_length"`
			SubmittedAt       string `json:"submitted_at"`
			DownloadURL       string `json:"download_url"`
			DownloadExpiresAt string `json:"download_expires_at"`
		} `json:"data"`
		NextCursor *string `json:"next_cursor"`
		HasMore    bool    `json:"has_more"`
	}](t, read)

	if len(list.Data) != 1 {
		t.Fatalf("read %d documents, want 1: %s", len(list.Data), read.Body)
	}
	got := list.Data[0]
	if got.Kind != "licence" || got.ContentType != "image/jpeg" || got.ContentLength != 4096 {
		t.Errorf("read back (%s, %s, %d)", got.Kind, got.ContentType, got.ContentLength)
	}
	if got.DownloadURL == "" || got.DownloadExpiresAt == "" {
		t.Errorf("a document came back with no signed URL: %s", read.Body)
	}
	if got.SubmittedAt == "" || got.ID == "" {
		t.Errorf("a document came back with no id or no clock: %s", read.Body)
	}
	if strings.Contains(read.Body.String(), "object_key") {
		t.Errorf("the document list carries an object key: %s", read.Body)
	}
	if list.NextCursor != nil || list.HasMore {
		t.Errorf("the collection envelope claims another page: %s", read.Body)
	}

	// --- and a customer is refused with a code the app can act on ---------------------------
	customer := newAccount(t, pool, "documents-wire-customer@example.com", "0415000031", "customer")

	refused := as(t, router, customer, http.MethodGet, "/v1/provider/verification/documents")
	if refused.Code != http.StatusForbidden {
		t.Fatalf("a customer read the list: %d", refused.Code)
	}
	if code := decode[errorEnvelope](t, refused).Error.Code; code != string(CodeProviderOnly) {
		t.Errorf("code = %q, want %q", code, CodeProviderOnly)
	}
}
