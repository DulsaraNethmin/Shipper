package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/authctx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/events"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/jobs"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/testsupport/pgtest"
)

// SHIP-114 in the domain and at the wire.
//
// What the real signer produces is exercised against a real object store in
// internal/platform/storage; these tests are about the half that decides *whether* to sign — who
// may ask, what they may ask for, and what the platform passes to the signer when it says yes. The
// port is stubbed here for exactly that reason: a test that also signed would be unable to fail on
// the interesting condition, which is the platform asking for the wrong thing.

// testUploadPolicy is a policy with all three fields deliberately different from anything a
// configuration default would produce.
//
// The lifetime especially: a stub that ignored the TTL and a service that passed the wrong one
// would both pass against 15 minutes, because that is what internal/config's default is and what a
// reader would expect to see. Seven minutes is a number nothing else in this repository uses.
func testUploadPolicy() UploadPolicy {
	return UploadPolicy{
		MaxBytes:             500_000,
		AcceptedContentTypes: []string{"image/jpeg", "image/heic"},
		URLTTL:               7 * time.Minute,
	}
}

// recordingUploads is a [ProofUploads] that signs nothing and remembers everything it was asked.
type recordingUploads struct {
	calls []presignCall

	// err, when set, is what PresignUpload answers with — the signer failing rather than the
	// request being wrong.
	err error
}

type presignCall struct {
	key           string
	contentType   string
	contentLength int64
	ttl           time.Duration
}

func (u *recordingUploads) PresignUpload(
	_ context.Context,
	key, contentType string,
	contentLength int64,
	ttl time.Duration,
) (string, time.Time, error) {
	u.calls = append(u.calls, presignCall{
		key: key, contentType: contentType, contentLength: contentLength, ttl: ttl,
	})
	if u.err != nil {
		return "", time.Time{}, u.err
	}
	return "https://store.example/" + key + "?signed=yes", testInstant.Add(ttl), nil
}

func (u *recordingUploads) last(t *testing.T) presignCall {
	t.Helper()

	if len(u.calls) == 0 {
		t.Fatal("nothing was asked of the signer")
	}
	return u.calls[len(u.calls)-1]
}

// serviceWithUploads is a service whose signer the test can inspect afterwards.
func serviceWithUploads(uploads ProofUploads, policy UploadPolicy) *Service {
	return NewService(events.NewOutbox(), staticJobs{move: JobMoved}, testAwards{}, testJobOwners(),
		testDriverIssuer(testClock()), uploads, newRecordingObjects(), policy, testClock())
}

// storedObject is what a stubbed store says it is holding.
//
// The three fields are what [ProofObjects.Stored] answers with, and the tests below set them to
// values no request asked for. That is the point of the type: SHIP-115 records what the store
// reported rather than what the client said, and a stub that echoed the request back could not fail
// if that were ever reversed.
type storedObject struct {
	contentType   string
	contentLength int64
	etag          string
}

// recordingObjects is a [ProofObjects] that holds whatever a test puts in it and remembers what was
// asked of it.
//
// The real signer is exercised against a real MinIO in internal/platform/storage; this is here for
// the half the domain owns — whether it asks at all, what it does with each answer, and whether a
// download URL is ever signed for somebody who was refused.
type recordingObjects struct {
	objects map[string]storedObject

	// storedErr, when set, is what Stored answers with: the store *failing* rather than the
	// object being absent. The two must not become one outcome — one is an opaque 500 and the
	// other is a refusal a driver can act on by uploading again.
	storedErr error

	// downloads is every key a download URL was signed for, in order. A test asserting that a
	// refused reader got nothing reads this rather than the response, because a response can be
	// empty for the wrong reason.
	downloads []string

	// lookups is every key the store was asked about, in order (SHIP-116). A recording that
	// carries a reasoned exception must ask the store nothing at all — there is no object to ask
	// about — and that is a claim about a call rather than about an answer, so a test reads it
	// here rather than inferring it from a refusal.
	lookups []string
}

func newRecordingObjects() *recordingObjects {
	return &recordingObjects{objects: map[string]storedObject{}}
}

// holding puts an object in the stub store and answers with its key, so a test reads as one line.
func (o *recordingObjects) holding(key string, obj storedObject) string {
	o.objects[key] = obj
	return key
}

func (o *recordingObjects) Stored(_ context.Context, key string) (string, int64, string, bool, error) {
	o.lookups = append(o.lookups, key)
	if o.storedErr != nil {
		return "", 0, "", false, o.storedErr
	}
	obj, ok := o.objects[key]
	if !ok {
		return "", 0, "", false, nil
	}
	return obj.contentType, obj.contentLength, obj.etag, true, nil
}

func (o *recordingObjects) PresignDownload(_ context.Context, key string, ttl time.Duration) (string, time.Time, error) {
	o.downloads = append(o.downloads, key)
	return "https://store.example/" + key + "?signed=for-reading", testInstant.Add(ttl), nil
}

// aPhotograph is an ordinary acceptable object: a type on [testUploadPolicy]'s list and a size under
// its limit.
func aPhotograph() storedObject {
	return storedObject{
		contentType:   "image/jpeg",
		contentLength: 137_402,
		etag:          "9f86d081884c7d659a2feaa0c55ad015",
	}
}

// serviceReadingProofFrom is a service over the real job lifecycle whose object store a test
// controls.
//
// [testJobs] rather than [staticJobs], because half of what SHIP-115 has to show involves a
// milestone that is absorbed or refused, and both of those are the transition guard's answers rather
// than a stub's.
func serviceReadingProofFrom(objects ProofObjects) *Service {
	return NewService(
		events.NewOutbox(),
		testJobs{svc: jobs.NewService(events.NewOutbox(), testClock(), nil)},
		testAwards{}, testJobOwners(), testDriverIssuer(testClock()),
		&recordingUploads{}, objects, testUploadPolicy(), testClock())
}

// --- the domain ---------------------------------------------------------------------------------

// TestAnUploadURLIsIssuedToTheAwardedProvider is the *Done when*'s first half in the domain: the
// client receives a short-lived pre-signed URL.
func TestAnUploadURLIsIssuedToTheAwardedProvider(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-award-c@example.com", "+61400000700", "customer")
	provider := newAccount(t, pool, "proof-award-p@example.com", "+61400000701", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	uploads := &recordingUploads{}
	svc := serviceWithUploads(uploads, testUploadPolicy())

	upload, err := svc.PresignProofUpload(t.Context(), pool, provider, jobID,
		UploadRequest{ContentType: "image/jpeg", ContentLength: 120_000})
	if err != nil {
		t.Fatalf("issuing an upload URL: %v", err)
	}

	if upload.URL == "" {
		t.Error("no URL was returned, so there is nothing to upload to")
	}
	if !strings.HasPrefix(upload.ObjectKey, "proof/"+jobID.String()+"/") {
		t.Errorf("object_key = %q, want it prefixed by proof/<job>/", upload.ObjectKey)
	}

	call := uploads.last(t)
	if call.ttl != testUploadPolicy().URLTTL {
		t.Errorf("the signer was asked for a URL good for %s, want the configured %s — "+
			"the lifetime is the whole of the authorisation and nothing can revoke it",
			call.ttl, testUploadPolicy().URLTTL)
	}
	if call.contentType != "image/jpeg" || call.contentLength != 120_000 {
		t.Errorf("the signer was asked to sign %s/%d, want image/jpeg/120000 — "+
			"an unsigned type or length is a limit the store does not enforce",
			call.contentType, call.contentLength)
	}
	if !upload.ExpiresAt.After(testInstant) {
		t.Errorf("expires_at = %s, which is not in the future of %s", upload.ExpiresAt, testInstant)
	}
}

// TestEveryUploadGetsAKeyOfItsOwn.
//
// The reasoning is in [Service.PresignProofUpload]: a key a second request could predict or reuse
// would let a URL issued today overwrite an object that already holds proof, and proof is evidence.
// This is the test that fails if somebody makes the key deterministic to "fix" retry behaviour.
func TestEveryUploadGetsAKeyOfItsOwn(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-keys-c@example.com", "+61400000702", "customer")
	provider := newAccount(t, pool, "proof-keys-p@example.com", "+61400000703", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := serviceWithUploads(&recordingUploads{}, testUploadPolicy())

	seen := map[string]bool{}
	for range 5 {
		upload, err := svc.PresignProofUpload(t.Context(), pool, provider, jobID,
			UploadRequest{ContentType: "image/jpeg", ContentLength: 1000})
		if err != nil {
			t.Fatalf("issuing an upload URL: %v", err)
		}
		if seen[upload.ObjectKey] {
			t.Fatalf("%s was issued twice; a reissued key can overwrite an object that "+
				"already holds proof", upload.ObjectKey)
		}
		seen[upload.ObjectKey] = true
	}
}

// TestOnlyTheAwardedProviderGetsAnUploadURL is the platform deciding which job a caller may upload
// against, which is not a decision the device makes (Docs/07 §3).
func TestOnlyTheAwardedProviderGetsAnUploadURL(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-who-c@example.com", "+61400000704", "customer")
	provider := newAccount(t, pool, "proof-who-p@example.com", "+61400000705", "provider")
	stranger := newAccount(t, pool, "proof-who-s@example.com", "+61400000706", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	uploads := &recordingUploads{}
	svc := serviceWithUploads(uploads, testUploadPolicy())

	for _, tc := range []struct {
		name   string
		caller uuid.UUID
		job    uuid.UUID
		want   error
	}{
		{name: "another provider", caller: stranger, job: jobID, want: ErrNotAwardedProvider},
		{name: "the customer who published it", caller: customer, job: jobID, want: ErrNotAwardedProvider},
		{name: "a job that does not exist", caller: provider, job: uuid.Must(uuid.NewV7()), want: ErrJobNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := svc.PresignProofUpload(t.Context(), pool, tc.caller, tc.job,
				UploadRequest{ContentType: "image/jpeg", ContentLength: 1000})
			if err == nil {
				t.Fatal("an upload URL was issued")
			}
			if !errors.Is(err, tc.want) {
				t.Errorf("error = %v, want %v", err, tc.want)
			}
		})
	}

	if len(uploads.calls) != 0 {
		t.Errorf("the signer was asked %d times for a caller who may not upload; "+
			"a URL refused after being signed is a URL that existed", len(uploads.calls))
	}
}

// TestTheUploadPolicyIsEnforcedBeforeAnythingIsSigned covers what a client can get wrong.
//
// The limits are configuration rather than constants (Docs/06 §5.3), so the table is written
// against [testUploadPolicy] and not against a literal: a test that hard-coded five megabytes would
// keep passing after somebody stopped reading the configured value.
func TestTheUploadPolicyIsEnforcedBeforeAnythingIsSigned(t *testing.T) {
	policy := testUploadPolicy()

	for _, tc := range []struct {
		name  string
		given UploadRequest
		field string
	}{
		{name: "no content type", given: UploadRequest{ContentLength: 1000}, field: "content_type"},
		{name: "whitespace content type", given: UploadRequest{ContentType: "  ", ContentLength: 1000}, field: "content_type"},
		{name: "a type not on the list", given: UploadRequest{ContentType: "image/png", ContentLength: 1000}, field: "content_type"},
		{name: "a script container being an image", given: UploadRequest{ContentType: "image/svg+xml", ContentLength: 1000}, field: "content_type"},
		{name: "a document", given: UploadRequest{ContentType: "application/pdf", ContentLength: 1000}, field: "content_type"},
		{name: "a runaway type", given: UploadRequest{ContentType: strings.Repeat("a", maxProofContentType+1), ContentLength: 1000}, field: "content_type"},
		{name: "no length", given: UploadRequest{ContentType: "image/jpeg"}, field: "content_length"},
		{name: "a negative length", given: UploadRequest{ContentType: "image/jpeg", ContentLength: -1}, field: "content_length"},
		{name: "one byte over the limit", given: UploadRequest{ContentType: "image/jpeg", ContentLength: policy.MaxBytes + 1}, field: "content_length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			problems := tc.given.normalise().problems(policy)
			if !problems.Any() {
				t.Fatalf("%#v was accepted", tc.given)
			}
			if got := problems.Fields()[0].Field; got != tc.field {
				t.Errorf("the problem is reported against %q, want %q", got, tc.field)
			}
		})
	}

	// And the boundary in the other direction: exactly the limit is allowed, because a limit
	// nobody may reach is a limit one byte lower than it says.
	problems := UploadRequest{ContentType: "image/jpeg", ContentLength: policy.MaxBytes}.
		normalise().problems(policy)
	if problems.Any() {
		t.Errorf("a photograph of exactly %d bytes was refused: %v", policy.MaxBytes, problems.Fields())
	}
}

// TestAMediaTypeIsMatchedCaseInsensitively, because RFC 9110 §8.3 says they are — and because the
// value is signed, so the platform has to settle on one spelling before it signs.
func TestAMediaTypeIsMatchedCaseInsensitively(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-case-c@example.com", "+61400000707", "customer")
	provider := newAccount(t, pool, "proof-case-p@example.com", "+61400000708", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	uploads := &recordingUploads{}
	svc := serviceWithUploads(uploads, testUploadPolicy())

	upload, err := svc.PresignProofUpload(t.Context(), pool, provider, jobID,
		UploadRequest{ContentType: " IMAGE/JPEG ", ContentLength: 1000})
	if err != nil {
		t.Fatalf("IMAGE/JPEG was refused: %v", err)
	}
	if upload.ContentType != "image/jpeg" {
		t.Errorf("content_type came back as %q; the client must be told the spelling that was "+
			"signed, or the store refuses its upload with no explanation", upload.ContentType)
	}
	if got := uploads.last(t).contentType; got != "image/jpeg" {
		t.Errorf("the signer was given %q", got)
	}
}

// TestASignerFailureIsNotAClientError.
//
// The domain has already checked everything a request could get wrong by the time the signer is
// reached, so a failure there is configuration or wiring — an opaque 500 with its cause logged,
// never a validation error blaming the caller's photograph.
func TestASignerFailureIsNotAClientError(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-fail-c@example.com", "+61400000709", "customer")
	provider := newAccount(t, pool, "proof-fail-p@example.com", "+61400000710", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	broken := errors.New("the store's credentials are wrong")
	svc := serviceWithUploads(&recordingUploads{err: broken}, testUploadPolicy())

	_, err := svc.PresignProofUpload(t.Context(), pool, provider, jobID,
		UploadRequest{ContentType: "image/jpeg", ContentLength: 1000})
	if err == nil {
		t.Fatal("a URL was returned by a signer that failed")
	}
	if !errors.Is(err, broken) {
		t.Errorf("error = %v, want the signer's own failure wrapped rather than replaced", err)
	}

	// apiError has no case for it, so it falls through unmapped and httpx.WriteError turns it
	// into an opaque 500 with the cause logged against the request id (SHIP-15i). A *httpx.Error
	// here would mean the domain had decided this was the caller's fault.
	var mapped *httpx.Error
	if errors.As(apiError(err), &mapped) {
		t.Errorf("a signer failure was mapped to %d/%s; it is not a client error",
			mapped.Status, mapped.Code)
	}
}

// --- the wire -----------------------------------------------------------------------------------

type proofUploadBody struct {
	ObjectKey string `json:"object_key"`

	UploadURL string `json:"upload_url"`
	Method    string `json:"method"`

	ContentType   string `json:"content_type"`
	ContentLength int64  `json:"content_length"`

	ExpiresAt string `json:"expires_at"`
}

// proofUploadPath is the route as cmd/api serves it.
func proofUploadPath(jobID uuid.UUID) string {
	return "/v1/jobs/" + jobID.String() + "/proof-uploads"
}

// TestProofUploadEndpointAnswersWithSomewhereToUpload is SHIP-114 at the wire.
func TestProofUploadEndpointAnswersWithSomewhereToUpload(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-proof-c@example.com", "+61400000711", "customer")
	provider := newAccount(t, pool, "http-proof-p@example.com", "+61400000712", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	rec := as(t, router, provider, proofUploadPath(jobID),
		`{"content_type": "image/jpeg", "content_length": 120000}`)

	// 200, not 201: nothing was created. The record that makes an uploaded object into proof is
	// SHIP-115's, and that one is a 201.
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body)
	}

	body := decode[proofUploadBody](t, rec)
	switch {
	case body.UploadURL == "":
		t.Error("no upload_url, so the client has nowhere to send the photograph")
	case body.ObjectKey == "":
		t.Error("no object_key, so SHIP-115 has nothing to record")
	case body.Method != http.MethodPut:
		t.Errorf("method = %q, want PUT", body.Method)
	case body.ContentType != "image/jpeg":
		t.Errorf("content_type = %q — it is signed, so the client must be told it", body.ContentType)
	case body.ContentLength != 120000:
		t.Errorf("content_length = %d — it is signed, so the client must be told it", body.ContentLength)
	case !strings.HasSuffix(body.ExpiresAt, "Z"):
		t.Errorf("expires_at = %q, want UTC", body.ExpiresAt)
	}

	// The URL does not point at this service. It is the whole of Docs/06 §5.2 — a photograph
	// travels between the client and the store with the API in neither direction — and it is
	// the assertion that would fail if somebody "simplified" this into a proxy upload.
	if strings.Contains(body.UploadURL, "/v1/") {
		t.Errorf("upload_url = %q, which looks like a route on this service; the bytes must "+
			"never be proxied through the API", body.UploadURL)
	}
}

// TestAnUnacceptableUploadIsRefusedWithTheFieldNamed.
//
// 422 with `content_length` or `content_type` named rather than a domain error code, because a
// client's response is to compress or convert rather than to go to a different screen — which is
// the test errors.go sets for whether a code earns its place.
func TestAnUnacceptableUploadIsRefusedWithTheFieldNamed(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-proof-bad-c@example.com", "+61400000713", "customer")
	provider := newAccount(t, pool, "http-proof-bad-p@example.com", "+61400000714", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	for _, tc := range []struct {
		name  string
		body  string
		field string
	}{
		{name: "a script container", body: `{"content_type": "image/svg+xml", "content_length": 1000}`, field: "content_type"},
		{name: "over the size limit", body: `{"content_type": "image/jpeg", "content_length": 999999999}`, field: "content_length"},
		{name: "no size at all", body: `{"content_type": "image/jpeg"}`, field: "content_length"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := as(t, router, provider, proofUploadPath(jobID), tc.body)
			if rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (%s)", rec.Code, rec.Body)
			}

			envelope := decode[errorEnvelope](t, rec)
			if envelope.Error.Code != "validation_failed" {
				t.Errorf("code = %q, want validation_failed", envelope.Error.Code)
			}
			if len(envelope.Error.Details) == 0 || envelope.Error.Details[0].Field != tc.field {
				t.Errorf("details = %+v, want %s named", envelope.Error.Details, tc.field)
			}
		})
	}
}

// TestAStrangerAskingForAnUploadURLGetsTheSameAnswerAsAMissingJob.
//
// 404 rather than 403, for the reason apiError gives: a 403 would confirm that a competitor's job
// exists and that somebody was awarded it.
func TestAStrangerAskingForAnUploadURLGetsTheSameAnswerAsAMissingJob(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-proof-str-c@example.com", "+61400000715", "customer")
	provider := newAccount(t, pool, "http-proof-str-p@example.com", "+61400000716", "provider")
	stranger := newAccount(t, pool, "http-proof-str-s@example.com", "+61400000717", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	strangers := as(t, router, stranger, proofUploadPath(jobID),
		`{"content_type": "image/jpeg", "content_length": 1000}`)
	missing := as(t, router, provider, proofUploadPath(uuid.Must(uuid.NewV7())),
		`{"content_type": "image/jpeg", "content_length": 1000}`)

	if strangers.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("a stranger got %d and a missing job got %d, want 404 for both",
			strangers.Code, missing.Code)
	}
	if strangers.Body.String() != missing.Body.String() {
		t.Errorf("the two answers differ:\n  stranger %s\n  missing  %s",
			strangers.Body, missing.Body)
	}
}

// TestAnUnknownFieldOnAnUploadRequestIsRefused, because a client that sent `object_key` should be
// told the field does not exist rather than have its choice of object silently ignored.
func TestAnUnknownFieldOnAnUploadRequestIsRefused(t *testing.T) {
	pool := pgtest.DB(t)
	router := newTestRouter(t, pool)

	customer := newAccount(t, pool, "http-proof-unk-c@example.com", "+61400000718", "customer")
	provider := newAccount(t, pool, "http-proof-unk-p@example.com", "+61400000719", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	rec := as(t, router, provider, proofUploadPath(jobID),
		`{"content_type": "image/jpeg", "content_length": 1000, "object_key": "proof/mine"}`)
	if rec.Code == http.StatusOK {
		t.Fatalf("a client chose its own object key and was allowed to: %s", rec.Body)
	}
}

// --- SHIP-115: the record, and who may read it --------------------------------------------------
//
// # What these tests can and cannot show
//
// The store is stubbed, exactly as it is above, and the *reason* is different here and worth
// stating. Above, a stub is used because the tests are about whether the platform asks for the
// right thing. Below, a stub is used because the platform's behaviour has to be driven from an
// object store's *answers* — an object that is not there, an object that is there and is 900 KB,
// an object whose media type nobody accepts — and producing those against a real MinIO would mean
// uploading a 900 KB file to assert a number.
//
// **That is also what closes the hole SHIP-114 recorded.** Its mutation testing found that reducing
// the signer to `host` alone left `internal/delivery`'s whole test package green, so the claim
// "the platform's upload limits are enforced" had no guard inside the domain at all. It has one
// now, and it is a different guard rather than the same one moved: the limits are applied to what
// the store says it is holding, at the moment an object becomes evidence, whatever route it took
// into the bucket. TestProofOverTheSizeLimitIsRefusedEvenThoughItReachedTheBucket and
// TestProofOfAnUnacceptedTypeIsRefusedEvenThoughItReachedTheBucket are the two that fail if that
// check is removed, and neither depends on the signer.

// recordWithProof records a milestone carrying proof, the way [Handler.RecordMilestone] does it:
// the store is asked outside the transaction and the row is written inside one.
//
// Written as one helper because the ordering is the design (see http.go), and a test that opened the
// transaction first would be testing an arrangement the service does not use.
func recordWithProof(
	t *testing.T,
	pool *pgxpool.Pool,
	svc *Service,
	provider, jobID uuid.UUID,
	rec Recording,
	objectKey string,
) (Record, Outcome, error) {
	t.Helper()

	proof, err := svc.VerifyProof(t.Context(), pool, provider, jobID, objectKey)
	if err != nil {
		return Record{}, OutcomeUnrecognised, err
	}
	rec.Proof = proof

	return recordMilestone(t, pool, svc, provider, jobID, rec)
}

// proofCount is how many rows a job has, read from the table rather than from what the service said.
func proofCount(t *testing.T, pool *pgxpool.Pool, jobID uuid.UUID) int {
	t.Helper()

	var n int
	if err := pool.QueryRow(t.Context(),
		`SELECT count(*) FROM proofs WHERE job_id = $1`, jobID).Scan(&n); err != nil {
		t.Fatalf("counting proof on %s: %v", jobID, err)
	}
	return n
}

// TestProofIsLinkedToTheJobAndTheMilestoneItProves is the *Done when*, in the database.
//
// It reads the row rather than the return value, and it asserts the *stored* content type and
// length rather than anything the request mentioned — because the request mentions neither, and
// what a `proofs` row holds is what the store reported.
func TestProofIsLinkedToTheJobAndTheMilestoneItProves(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-link-c@example.com", "+61400000730", "customer")
	provider := newAccount(t, pool, "proof-link-p@example.com", "+61400000731", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	record, outcome, err := recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key)
	if err != nil {
		t.Fatalf("recording a milestone with proof: %v", err)
	}
	if outcome != OutcomeRecorded {
		t.Fatalf("outcome = %s, want recorded", outcome)
	}

	var (
		storedJob       uuid.UUID
		storedMilestone uuid.UUID
		storedKey       string
		contentType     string
		contentLength   int64
		etag            string
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT job_id, milestone_id, object_key, content_type, content_length, etag
		   FROM proofs WHERE job_id = $1`, jobID).
		Scan(&storedJob, &storedMilestone, &storedKey, &contentType, &contentLength, &etag); err != nil {
		t.Fatalf("reading the proof row on %s: %v", jobID, err)
	}

	if storedJob != jobID {
		t.Errorf("the proof is on job %s, want %s", storedJob, jobID)
	}
	if storedMilestone != record.ID {
		t.Errorf("the proof names milestone %s, want the one just recorded, %s",
			storedMilestone, record.ID)
	}
	if storedKey != key {
		t.Errorf("object_key = %q, want %q", storedKey, key)
	}

	want := aPhotograph()
	if contentType != want.contentType || contentLength != want.contentLength || etag != want.etag {
		t.Errorf("the row holds %s/%d/%s, want the store's own %s/%d/%s — what is recorded is "+
			"what the store reported, not what anybody asked for",
			contentType, contentLength, etag, want.contentType, want.contentLength, want.etag)
	}
}

// TestProofIsRefusedWhenNothingWasUploaded is the reconciliation this ticket turns on: the platform
// is not in the upload path, so it asks, and a row is never written for an object that is not there.
//
// The milestone goes with it. A delivery recorded as photographed, with no photograph, is the state
// SHIP-118 will be enforcing against and the one Docs/01 §4.4 calls the only evidence there is.
func TestProofIsRefusedWhenNothingWasUploaded(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-gone-c@example.com", "+61400000732", "customer")
	provider := newAccount(t, pool, "proof-gone-p@example.com", "+61400000733", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	// An empty store: the URL was issued and the PUT never arrived.
	svc := serviceReadingProofFrom(newRecordingObjects())

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}

	_, _, err = recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key)
	if !errors.Is(err, ErrProofNotUploaded) {
		t.Fatalf("recording proof for an object that is not there: %v, want ErrProofNotUploaded", err)
	}

	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestones were recorded, want 0 — a delivery must not be recorded as "+
			"photographed when nothing was", n)
	}
	if n := proofCount(t, pool, jobID); n != 0 {
		t.Errorf("%d proof rows survived, want 0", n)
	}
}

// TestAStoreThatFailsIsNotAMissingPhotograph.
//
// The two must not collapse into one outcome. A store that cannot be reached is the platform's
// problem and becomes an opaque 500; an object that is not there is the client's and becomes a
// refusal telling them to finish uploading. Folding the first into the second would tell a driver
// their photograph never arrived every time the object store had a bad minute.
func TestAStoreThatFailsIsNotAMissingPhotograph(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-fail-c@example.com", "+61400000734", "customer")
	provider := newAccount(t, pool, "proof-fail-p@example.com", "+61400000735", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	objects.storedErr = errors.New("the store answered 503 Service Unavailable")
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}

	_, _, err = recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key)
	switch {
	case err == nil:
		t.Fatal("the store failed and proof was recorded anyway")
	case errors.Is(err, ErrProofNotUploaded):
		t.Fatal("a store that could not answer was reported as a photograph that was never sent")
	}
}

// TestProofOverTheSizeLimitIsRefusedEvenThoughItReachedTheBucket.
//
// **This is one of the two tests that close SHIP-114's recorded hole.** The upload URL signs the
// length, so ordinarily the store refuses an oversized body on the request that carries it — and no
// test in this package can fail when that stops working, because the signer is stubbed here. This
// fails instead: whatever route an object took into the bucket, its size is measured against
// STORAGE_MAX_UPLOAD_BYTES before it can become evidence.
func TestProofOverTheSizeLimitIsRefusedEvenThoughItReachedTheBucket(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-big-c@example.com", "+61400000736", "customer")
	provider := newAccount(t, pool, "proof-big-p@example.com", "+61400000737", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	oversized := aPhotograph()
	oversized.contentLength = testUploadPolicy().MaxBytes + 1
	objects.holding(key, oversized)

	_, _, err = recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key)
	if !errors.Is(err, ErrProofRejected) {
		t.Fatalf("an object of %d bytes against a %d-byte limit: %v, want ErrProofRejected",
			oversized.contentLength, testUploadPolicy().MaxBytes, err)
	}
	if n := proofCount(t, pool, jobID); n != 0 {
		t.Errorf("%d proof rows were written for an object over the limit, want 0", n)
	}
}

// TestProofOfAnUnacceptedTypeIsRefusedEvenThoughItReachedTheBucket is the other half.
//
// `image/svg+xml` is the case the accepted-type list exists for — a script container that browsers
// execute, arriving by being an image — and it is refused on what the store says it is holding
// rather than on what anybody claimed it would be.
func TestProofOfAnUnacceptedTypeIsRefusedEvenThoughItReachedTheBucket(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-svg-c@example.com", "+61400000738", "customer")
	provider := newAccount(t, pool, "proof-svg-p@example.com", "+61400000739", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	script := aPhotograph()
	script.contentType = "image/svg+xml"
	objects.holding(key, script)

	_, _, err = recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key)
	if !errors.Is(err, ErrProofRejected) {
		t.Fatalf("an object stored as image/svg+xml: %v, want ErrProofRejected", err)
	}
	if n := proofCount(t, pool, jobID); n != 0 {
		t.Errorf("%d proof rows were written for a script container, want 0", n)
	}
}

// TestProofFromAnotherJobIsRefusedBeforeAnythingIsLookedUp.
//
// A provider awarded two jobs holds an object key for each, and nothing but this stops them putting
// one delivery's photograph on the other's timeline. It is refused on the string, so the store is
// never asked — asserted here, because a check that ran *after* the lookup would also pass this
// test's first assertion while turning the endpoint into a way to ask whether a key exists.
func TestProofFromAnotherJobIsRefusedBeforeAnythingIsLookedUp(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-other-c@example.com", "+61400000740", "customer")
	provider := newAccount(t, pool, "proof-other-p@example.com", "+61400000741", "provider")
	first := awardedJob(t, pool, customer, provider)
	second := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	// A perfectly real object, uploaded against the first job, offered as proof on the second.
	key, err := proofObjectKey(first)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	_, _, err = recordWithProof(t, pool, svc, provider, second, enRoute(theKey), key)
	if !errors.Is(err, ErrProofNotForThisJob) {
		t.Fatalf("one job's object recorded on another: %v, want ErrProofNotForThisJob", err)
	}
	if n := proofCount(t, pool, second); n != 0 {
		t.Errorf("%d proof rows were written on the wrong job, want 0", n)
	}
}

// TestOnlyTheAwardedProviderMayRecordProof.
//
// The same check every other write in this domain makes, and it is repeated for this path rather
// than assumed: VerifyProof runs on the pool before the transaction opens, so it is a *separate*
// place the rule has to hold. A stranger is refused before the store is asked anything, which is
// what stops the endpoint confirming that an object exists.
func TestOnlyTheAwardedProviderMayRecordProof(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-str-c@example.com", "+61400000742", "customer")
	provider := newAccount(t, pool, "proof-str-p@example.com", "+61400000743", "provider")
	stranger := newAccount(t, pool, "proof-str-o@example.com", "+61400000744", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	if _, err := svc.VerifyProof(t.Context(), pool, stranger, jobID, key); !errors.Is(err, ErrNotAwardedProvider) {
		t.Fatalf("a provider who did not win the job: %v, want ErrNotAwardedProvider", err)
	}
	if _, err := svc.VerifyProof(t.Context(), pool, customer, jobID, key); !errors.Is(err, ErrNotAwardedProvider) {
		t.Fatalf("the job's own customer recorded proof: %v, want ErrNotAwardedProvider — "+
			"Docs/02 §3 permits the provider, their driver and an administrator, and a customer "+
			"is none of them", err)
	}
}

// TestOneObjectIsProofOfAtMostOneMilestone.
//
// uq_proofs_object_key, from the domain's side. One photograph is evidence for one recorded claim:
// the same object on two milestones would put a picture of one moment against another, and the
// second recording is refused with a code that tells the app to ask for a fresh upload URL.
func TestOneObjectIsProofOfAtMostOneMilestone(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-twice-c@example.com", "+61400000745", "customer")
	provider := newAccount(t, pool, "proof-twice-p@example.com", "+61400000746", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	if _, _, err := recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key); err != nil {
		t.Fatalf("the first recording: %v", err)
	}

	// A second, different milestone under a different idempotency key, offering the same object.
	second := Recording{Milestone: MilestonePickedUp, Key: theKey + "-again"}
	_, _, err = recordWithProof(t, pool, svc, provider, jobID, second, key)
	if !errors.Is(err, ErrProofAlreadyRecorded) {
		t.Fatalf("one object recorded as proof twice: %v, want ErrProofAlreadyRecorded", err)
	}

	if n := proofCount(t, pool, jobID); n != 1 {
		t.Errorf("%d proof rows, want 1", n)
	}
	if n := milestoneCount(t, pool, jobID); n != 1 {
		t.Errorf("%d milestones, want 1 — the refused recording must roll back whole", n)
	}
}

// TestAnAbsorbedMilestoneKeepsItsProof.
//
// **This is the answer to "does proof follow the milestone or the job".** SHIP-112 keeps a late
// milestone's row and deliberately does not move the job; proof is attached to that row, so it is
// kept with it and it carries the actor's own clock. A photograph taken at dawn in a yard with no
// signal is evidence of what happened at dawn, whatever time the phone found a tower.
func TestAnAbsorbedMilestoneKeepsItsProof(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-late-c@example.com", "+61400000747", "customer")
	provider := newAccount(t, pool, "proof-late-p@example.com", "+61400000748", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	// The delivery runs on past the pickup.
	for _, rec := range []Recording{
		{Milestone: MilestoneEnRouteToPickup, Key: theKey + "-1"},
		{Milestone: MilestonePickedUp, Key: theKey + "-2"},
		{Milestone: MilestoneInTransit, Key: theKey + "-3"},
	} {
		if _, _, err := recordMilestone(t, pool, svc, provider, jobID, rec); err != nil {
			t.Fatalf("recording %s: %v", rec.Milestone, err)
		}
	}

	// The queued one arrives, photograph and all, for a point the job has already passed.
	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	late := Recording{
		Milestone:  MilestonePickedUp,
		RecordedAt: testInstant.Add(-6 * time.Hour),
		Key:        theKey + "-late",
	}
	record, outcome, err := recordWithProof(t, pool, svc, provider, jobID, late, key)
	if err != nil {
		t.Fatalf("a late milestone carrying proof: %v", err)
	}
	if outcome != OutcomeAbsorbed {
		t.Fatalf("outcome = %s, want absorbed", outcome)
	}

	var (
		milestoneID uuid.UUID
		recordedAt  time.Time
	)
	if err := pool.QueryRow(t.Context(),
		`SELECT p.milestone_id, m.actor_recorded_at
		   FROM proofs p JOIN milestones m ON m.id = p.milestone_id
		  WHERE p.object_key = $1`, key).Scan(&milestoneID, &recordedAt); err != nil {
		t.Fatalf("the absorbed milestone's proof is not there: %v", err)
	}
	if milestoneID != record.ID {
		t.Errorf("the proof names milestone %s, want the absorbed one, %s", milestoneID, record.ID)
	}
	if !recordedAt.Equal(late.RecordedAt) {
		t.Errorf("the proof reads at %s, want the actor's own %s — proof follows the milestone, "+
			"and an absorbed milestone carries the time the driver acted",
			recordedAt, late.RecordedAt)
	}
}

// TestARefusedMilestoneLeavesNoProofBehind is the other direction of the same seam.
//
// A milestone the delivery has not reached is refused and the transaction rolls back — SHIP-112
// kept that deliberately — and the photograph must go with it. A proof row for a milestone that was
// never recorded would point at nothing, which the composite foreign key would refuse anyway; this
// asserts the domain does not try.
func TestARefusedMilestoneLeavesNoProofBehind(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-early-c@example.com", "+61400000749", "customer")
	provider := newAccount(t, pool, "proof-early-p@example.com", "+61400000750", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	// `in_transit` on a job that has not left for the pickup.
	premature := Recording{Milestone: MilestoneInTransit, Key: theKey}
	if _, _, err := recordWithProof(t, pool, svc, provider, jobID, premature, key); !errors.Is(err, ErrMilestoneNotPermitted) {
		t.Fatalf("a premature milestone: %v, want ErrMilestoneNotPermitted", err)
	}

	if n := proofCount(t, pool, jobID); n != 0 {
		t.Errorf("%d proof rows survived a refused milestone, want 0", n)
	}
	if n := milestoneCount(t, pool, jobID); n != 0 {
		t.Errorf("%d milestones survived, want 0", n)
	}
}

// TestARetryRecordsNoSecondProof.
//
// The milestone's idempotency key is what makes this true and there is no key column on `proofs`:
// a retry that reaches the table finds the milestone already recorded and returns before the proof
// insert is ever attempted. Driven with the cached response out of the picture entirely, which is
// what a phone reconnecting after a day in a valley does.
func TestARetryRecordsNoSecondProof(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-retry-c@example.com", "+61400000751", "customer")
	provider := newAccount(t, pool, "proof-retry-p@example.com", "+61400000752", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	first, _, err := recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key)
	if err != nil {
		t.Fatalf("the first recording: %v", err)
	}

	again, outcome, err := recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key)
	if err != nil {
		t.Fatalf("the retry: %v", err)
	}
	if outcome != OutcomeAlreadyRecorded {
		t.Fatalf("outcome = %s, want already recorded", outcome)
	}
	if again.ID != first.ID {
		t.Errorf("the retry answered with milestone %s, want %s", again.ID, first.ID)
	}
	if n := proofCount(t, pool, jobID); n != 1 {
		t.Errorf("%d proof rows after a retry, want 1", n)
	}
}

// TestBothPartiesToTheDeliveryMayReadProof is the access control's permitted half.
//
// The customer who owns the job and the provider it was awarded to get the same thing: the record,
// and a short-lived signed URL each. Neither gets a reduced view, which is a decision — a photograph
// is not a field that can be redacted, and both parties are looking at the same object when they
// disagree about it (Docs/04 §7).
func TestBothPartiesToTheDeliveryMayReadProof(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-read-c@example.com", "+61400000753", "customer")
	provider := newAccount(t, pool, "proof-read-p@example.com", "+61400000754", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	record, _, err := recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key)
	if err != nil {
		t.Fatalf("recording proof: %v", err)
	}

	for _, reader := range []struct {
		name string
		id   uuid.UUID
	}{
		{"the customer who owns the job", customer},
		{"the provider it was awarded to", provider},
	} {
		t.Run(reader.name, func(t *testing.T) {
			links, err := svc.ProofFor(t.Context(), pool, reader.id, jobID)
			if err != nil {
				t.Fatalf("reading the proof: %v", err)
			}
			if len(links) != 1 {
				t.Fatalf("%d proof records, want 1", len(links))
			}

			got := links[0]
			if got.MilestoneID != record.ID {
				t.Errorf("the proof names milestone %s, want %s", got.MilestoneID, record.ID)
			}
			if got.Milestone != MilestoneEnRouteToPickup {
				t.Errorf("milestone = %s, want %s", got.Milestone, MilestoneEnRouteToPickup)
			}
			if got.URL == "" {
				t.Error("no download URL was signed, so there is nothing to look at")
			}
			if !got.ExpiresAt.After(testInstant) {
				t.Errorf("the download expires at %s, which is not in the future — nothing can "+
					"revoke a signed URL, so its lifetime is the whole of the control", got.ExpiresAt)
			}
		})
	}
}

// TestNobodyElseMayReadProofAndNoURLIsSignedForThem is the refused half, and the second assertion is
// the one that matters.
//
// A refusal that still signed a URL would have handed out the credential before deciding whether the
// caller may have it — and because the signer never contacts the store, that would leave no trace
// anywhere except in whatever the response did with it. So this asserts the store was never asked to
// sign at all, not merely that the answer was empty.
func TestNobodyElseMayReadProofAndNoURLIsSignedForThem(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-deny-c@example.com", "+61400000755", "customer")
	provider := newAccount(t, pool, "proof-deny-p@example.com", "+61400000756", "provider")
	otherCustomer := newAccount(t, pool, "proof-deny-c2@example.com", "+61400000757", "customer")
	otherProvider := newAccount(t, pool, "proof-deny-p2@example.com", "+61400000758", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	svc := serviceReadingProofFrom(objects)

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	if _, _, err := recordWithProof(t, pool, svc, provider, jobID, enRoute(theKey), key); err != nil {
		t.Fatalf("recording proof: %v", err)
	}

	signedBefore := len(objects.downloads)

	for _, reader := range []struct {
		name string
		id   uuid.UUID
	}{
		{"another customer", otherCustomer},
		{"a provider who did not win the job", otherProvider},
	} {
		t.Run(reader.name, func(t *testing.T) {
			links, err := svc.ProofFor(t.Context(), pool, reader.id, jobID)
			if !errors.Is(err, ErrJobNotFound) {
				t.Fatalf("reading somebody else's proof: %v, want ErrJobNotFound — a 403 would "+
					"confirm the job exists and that somebody is delivering it", err)
			}
			if links != nil {
				t.Errorf("%d proof records came back with the refusal", len(links))
			}
		})
	}

	if len(objects.downloads) != signedBefore {
		t.Errorf("%d download URLs were signed for callers who were refused, want 0 — the "+
			"authorisation decision must come before the credential, not after it",
			len(objects.downloads)-signedBefore)
	}
}

// TestAJobWithNothingPhotographedAnswersItsCustomerWithAnEmptyList.
//
// "Nothing has been photographed yet" and "no such job" are different answers to the job's own
// customer, and only the first is true. Telling them apart discloses nothing: it is their job.
func TestAJobWithNothingPhotographedAnswersItsCustomerWithAnEmptyList(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "proof-empty-c@example.com", "+61400000759", "customer")
	provider := newAccount(t, pool, "proof-empty-p@example.com", "+61400000760", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	svc := serviceReadingProofFrom(newRecordingObjects())

	links, err := svc.ProofFor(t.Context(), pool, customer, jobID)
	if err != nil {
		t.Fatalf("reading the proof on a job with none: %v", err)
	}
	if len(links) != 0 {
		t.Errorf("%d proof records on a job with none", len(links))
	}
}

// --- SHIP-115 at the wire -----------------------------------------------------------------------

// proofPath is the read route as cmd/api serves it.
func proofPath(jobID uuid.UUID) string { return "/v1/jobs/" + jobID.String() + "/delivery/proof" }

// readingAs sends a GET on behalf of an authenticated caller, which [as] cannot: that helper is a
// POST because every route before this one was.
func readingAs(t *testing.T, h http.Handler, caller uuid.UUID, target string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, target, nil)
	req = req.WithContext(authctx.WithSubject(req.Context(), authctx.Subject{
		UserID:    caller.String(),
		Role:      authctx.RoleCustomer,
		SessionID: uuid.Must(uuid.NewV7()).String(),
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

type proofBody struct {
	ID          string `json:"id"`
	JobID       string `json:"job_id"`
	MilestoneID string `json:"milestone_id"`

	Milestone string `json:"milestone"`
	ObjectKey string `json:"object_key"`

	ContentType   string `json:"content_type"`
	ContentLength int64  `json:"content_length"`

	DownloadURL       string `json:"download_url"`
	DownloadExpiresAt string `json:"download_expires_at"`

	ExceptionReason string `json:"exception_reason"`

	RecordedAt string `json:"recorded_at"`
	AcceptedAt string `json:"accepted_at"`
}

type proofListBody struct {
	Data    []proofBody `json:"data"`
	HasMore bool        `json:"has_more"`
	Cursor  *string     `json:"next_cursor"`
}

// TestProofIsRecordedWithTheMilestoneAndReadBackByBothParties is the *Done when* at the wire, end to
// end through the handler: one request attaches a photograph to a milestone, and both parties to the
// delivery can then see it with a link each.
func TestProofIsRecordedWithTheMilestoneAndReadBackByBothParties(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "http-proof-link-c@example.com", "+61400000770", "customer")
	provider := newAccount(t, pool, "http-proof-link-p@example.com", "+61400000771", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	router := newTestRouterFor(t, pool, serviceReadingProofFrom(objects))

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	rec := recordAs(t, router, provider, jobID, theKey,
		`{"milestone": "en_route_to_pickup", "proof": {"object_key": "`+key+`"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("recording a milestone with proof: %d (%s)", rec.Code, rec.Body)
	}
	milestoneID := decode[milestoneBody](t, rec).ID

	read := readingAs(t, router, customer, proofPath(jobID))
	if read.Code != http.StatusOK {
		t.Fatalf("the customer reading their own job's proof: %d (%s)", read.Code, read.Body)
	}

	list := decode[proofListBody](t, read)
	if len(list.Data) != 1 {
		t.Fatalf("%d proof records, want 1 (%s)", len(list.Data), read.Body)
	}

	got := list.Data[0]
	switch {
	case got.MilestoneID != milestoneID:
		t.Errorf("milestone_id = %q, want the milestone just recorded, %s", got.MilestoneID, milestoneID)
	case got.JobID != jobID.String():
		t.Errorf("job_id = %q, want %s", got.JobID, jobID)
	case got.Milestone != "en_route_to_pickup":
		t.Errorf("milestone = %q, want the wire form en_route_to_pickup", got.Milestone)
	case got.ObjectKey != key:
		t.Errorf("object_key = %q, want %q", got.ObjectKey, key)
	case got.DownloadURL == "":
		t.Error("no download_url, so there is nothing to render")
	case got.DownloadExpiresAt == "":
		t.Error("no download_expires_at — a link nothing can revoke must say when it stops working")
	}

	if got.ContentType != aPhotograph().contentType || got.ContentLength != aPhotograph().contentLength {
		t.Errorf("the response reports %s/%d, want the store's own %s/%d",
			got.ContentType, got.ContentLength, aPhotograph().contentType, aPhotograph().contentLength)
	}

	// The provider sees the same thing. No reduced view for either party.
	asProvider := readingAs(t, router, provider, proofPath(jobID))
	if asProvider.Code != http.StatusOK {
		t.Fatalf("the awarded provider reading their own proof: %d (%s)", asProvider.Code, asProvider.Body)
	}
	if providerList := decode[proofListBody](t, asProvider); len(providerList.Data) != 1 {
		t.Errorf("the provider sees %d records, want 1", len(providerList.Data))
	}
}

// TestTheProofResponseCarriesAClosedSetOfFields.
//
// Asserted as a whole set rather than searched for the field it must not carry, which is SHIP-83's
// argument: a search for `etag` catches `etag` and misses whatever a later ticket adds beside it.
// The entity tag in particular is recorded and never returned — it is an integrity fact for an
// administrator (SHIP-155), not something a customer's app would do anything with.
func TestTheProofResponseCarriesAClosedSetOfFields(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "http-proof-keys-c@example.com", "+61400000772", "customer")
	provider := newAccount(t, pool, "http-proof-keys-p@example.com", "+61400000773", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	router := newTestRouterFor(t, pool, serviceReadingProofFrom(objects))

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	rec := recordAs(t, router, provider, jobID, theKey,
		`{"milestone": "en_route_to_pickup", "proof": {"object_key": "`+key+`"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("recording a milestone with proof: %d (%s)", rec.Code, rec.Body)
	}

	read := readingAs(t, router, customer, proofPath(jobID))

	var envelope struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(read.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("the response is not JSON: %v (%s)", err, read.Body)
	}
	if len(envelope.Data) != 1 {
		t.Fatalf("%d proof records, want 1", len(envelope.Data))
	}

	got := make([]string, 0, len(envelope.Data[0]))
	for field := range envelope.Data[0] {
		got = append(got, field)
	}
	sort.Strings(got)

	want := []string{
		"accepted_at", "content_length", "content_type", "download_expires_at", "download_url",
		"id", "job_id", "milestone", "milestone_id", "object_key", "recorded_at",
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("the proof response carries\n  %s\nwant\n  %s", strings.Join(got, ","), strings.Join(want, ","))
	}
}

// TestAStrangerReadingProofGetsExactlyWhatAMissingJobGets.
//
// Byte for byte, which is the shape SHIP-114's own 404-parity test takes: a 403 would confirm that
// the job exists and that somebody is delivering it, and a differently *worded* 404 would confirm it
// almost as well.
func TestAStrangerReadingProofGetsExactlyWhatAMissingJobGets(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "http-proof-deny-c@example.com", "+61400000774", "customer")
	provider := newAccount(t, pool, "http-proof-deny-p@example.com", "+61400000775", "provider")
	stranger := newAccount(t, pool, "http-proof-deny-o@example.com", "+61400000776", "provider")
	jobID := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	router := newTestRouterFor(t, pool, serviceReadingProofFrom(objects))

	key, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(key, aPhotograph())

	if rec := recordAs(t, router, provider, jobID, theKey,
		`{"milestone": "en_route_to_pickup", "proof": {"object_key": "`+key+`"}}`); rec.Code != http.StatusCreated {
		t.Fatalf("recording a milestone with proof: %d (%s)", rec.Code, rec.Body)
	}

	strangers := readingAs(t, router, stranger, proofPath(jobID))
	missing := readingAs(t, router, stranger, proofPath(uuid.Must(uuid.NewV7())))

	if strangers.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("a stranger got %d and a missing job got %d, want 404 for both (%s)",
			strangers.Code, missing.Code, strangers.Body)
	}
	if strangers.Body.String() != missing.Body.String() {
		t.Errorf("the two answers differ:\n  stranger %s\n  missing  %s", strangers.Body, missing.Body)
	}
}

// TestProofOnTheWireIsRefusedByTheCasesAClientCanCause.
//
// One table over the wire because each of these leads to a different screen, and the code is what a
// client branches on. The refusals themselves are argued in errors.go; what this asserts is that the
// mapping from a domain sentinel to a code did not get lost between them.
func TestProofOnTheWireIsRefusedByTheCasesAClientCanCause(t *testing.T) {
	pool := pgtest.DB(t)

	customer := newAccount(t, pool, "http-proof-bad-c@example.com", "+61400000777", "customer")
	provider := newAccount(t, pool, "http-proof-bad-p@example.com", "+61400000778", "provider")
	jobID := awardedJob(t, pool, customer, provider)
	otherJob := awardedJob(t, pool, customer, provider)

	objects := newRecordingObjects()
	router := newTestRouterFor(t, pool, serviceReadingProofFrom(objects))

	uploaded, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(uploaded, aPhotograph())

	neverUploaded, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}

	oversizedKey, err := proofObjectKey(jobID)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	oversized := aPhotograph()
	oversized.contentLength = testUploadPolicy().MaxBytes + 1
	objects.holding(oversizedKey, oversized)

	elsewhere, err := proofObjectKey(otherJob)
	if err != nil {
		t.Fatalf("generating an object key: %v", err)
	}
	objects.holding(elsewhere, aPhotograph())

	for _, tc := range []struct {
		name   string
		key    string
		body   string
		status int
		code   string
		field  string
	}{
		{
			name: "the photograph never arrived", key: theKey + "-gone",
			body:   `{"milestone": "en_route_to_pickup", "proof": {"object_key": "` + neverUploaded + `"}}`,
			status: http.StatusConflict, code: "delivery_proof_not_uploaded",
		},
		{
			name: "the object is over the size limit", key: theKey + "-big",
			body:   `{"milestone": "en_route_to_pickup", "proof": {"object_key": "` + oversizedKey + `"}}`,
			status: http.StatusConflict, code: "delivery_proof_rejected",
		},
		{
			name: "the key belongs to another job", key: theKey + "-else",
			body:   `{"milestone": "en_route_to_pickup", "proof": {"object_key": "` + elsewhere + `"}}`,
			status: http.StatusUnprocessableEntity, code: "validation_failed", field: "proof.object_key",
		},
		{
			name: "proof was sent with nothing in it", key: theKey + "-empty",
			body:   `{"milestone": "en_route_to_pickup", "proof": {"object_key": "   "}}`,
			status: http.StatusUnprocessableEntity, code: "validation_failed", field: "proof.object_key",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := recordAs(t, router, provider, jobID, tc.key, tc.body)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, rec.Body)
			}

			envelope := decode[errorEnvelope](t, rec)
			if envelope.Error.Code != tc.code {
				t.Errorf("code = %q, want %q", envelope.Error.Code, tc.code)
			}
			if tc.field != "" {
				if len(envelope.Error.Details) == 0 || envelope.Error.Details[0].Field != tc.field {
					t.Errorf("the refusal does not name %s: %s", tc.field, rec.Body)
				}
			}

			if n := milestoneCount(t, pool, jobID); n != 0 {
				t.Errorf("%d milestones were recorded by a refused request, want 0", n)
			}
		})
	}

	// And the one that works, so the table above is not passing because everything is refused.
	rec := recordAs(t, router, provider, jobID, theKey,
		`{"milestone": "en_route_to_pickup", "proof": {"object_key": "`+uploaded+`"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("the acceptable photograph was refused: %d (%s)", rec.Code, rec.Body)
	}
}
