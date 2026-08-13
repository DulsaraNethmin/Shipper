package delivery

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
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
	return NewService(staticJobs{move: JobMoved}, testAwards{}, testDriverIssuer(testClock()),
		uploads, policy, testClock())
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
