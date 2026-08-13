package storage

import (
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// The signer, tested twice over: against AWS's own published example, and against a real store.
//
// Neither test replaces the other. The first says the algorithm is the specification's — it is a
// known answer nobody in this repository computed, so it cannot be made to pass by adjusting the
// code until it agrees with itself. The second says the URLs this package produces are accepted by
// something that checks them, which is the only evidence that matters for SHIP-114's *Done when*.
//
// The integration tests **fail rather than skip** when the store is missing, per CLAUDE.md: a test
// that quietly does not run is worse than one that does not exist, because it is counted.
// `go test -short` skips them deliberately.

// --- the published example -------------------------------------------------------------------

// TestTheAWSExampleSignsToThePublishedSignature is a known-answer test.
//
// The inputs and the expected signature are AWS's "Example: Query string request authentication"
// from the Signature Version 4 documentation, which is a value this repository did not compute.
// That is what makes it worth having: every other test here would still pass if the algorithm were
// self-consistently wrong.
//
// It signs a GET with `host` alone, which is why it goes through [S3.presign] rather than through
// PresignUpload — the example predates any content headers, and adding them would change the
// signature and destroy the property that makes the test useful.
func TestTheAWSExampleSignsToThePublishedSignature(t *testing.T) {
	signer, err := NewS3(Options{
		Endpoint:        "https://s3.amazonaws.com",
		Bucket:          "examplebucket",
		Region:          "us-east-1",
		AccessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		Clock:           clock.NewFixed(time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("building the signer: %v", err)
	}

	signed, err := signer.presign(signer.clock.Now(), http.MethodGet, "test.txt", 86400, nil)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}

	const want = "https://examplebucket.s3.amazonaws.com/test.txt?" +
		"X-Amz-Algorithm=AWS4-HMAC-SHA256&" +
		"X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request&" +
		"X-Amz-Date=20130524T000000Z&" +
		"X-Amz-Expires=86400&" +
		"X-Amz-SignedHeaders=host&" +
		"X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"

	if signed != want {
		t.Errorf("the published example signed to\n  %s\nwant\n  %s", signed, want)
	}
}

// --- what the URL says, without a store ---------------------------------------------------------

// testSigner is the signer every unit test below uses: path-style, fixed clock, throwaway keys.
func testSigner(t *testing.T) *S3 {
	t.Helper()

	signer, err := NewS3(Options{
		Endpoint:        "http://localhost:9000",
		Bucket:          "shipper-test",
		Region:          "ap-southeast-2",
		AccessKeyID:     "unit-test-key",
		SecretAccessKey: "unit-test-secret",
		UsePathStyle:    true,
		Clock:           clock.NewFixed(time.Date(2026, 8, 13, 4, 30, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("building the signer: %v", err)
	}
	return signer
}

// query is the signed URL's parameters, parsed.
func query(t *testing.T, signed string) url.Values {
	t.Helper()

	parsed, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("the signed URL does not parse: %v", err)
	}
	return parsed.Query()
}

// TestAnUploadURLSignsTheTwoHeadersThatBoundIt is the property the platform's limits rest on.
//
// Without content-type and content-length in X-Amz-SignedHeaders the URL authorises any body at
// all, and every check the delivery domain makes is a check on a promise. This is the assertion
// that would fail if somebody simplified the signer.
func TestAnUploadURLSignsTheTwoHeadersThatBoundIt(t *testing.T) {
	signed, expiresAt, err := testSigner(t).PresignUpload(
		t.Context(), "proof/job/object", "image/jpeg", 1024, 15*time.Minute)
	if err != nil {
		t.Fatalf("signing an upload: %v", err)
	}

	got := query(t, signed).Get("X-Amz-SignedHeaders")
	if got != "content-length;content-type;host" {
		t.Errorf("X-Amz-SignedHeaders = %q, want content-length;content-type;host — "+
			"an unsigned content type or length is a limit the store does not enforce", got)
	}

	if !strings.HasPrefix(signed, "http://localhost:9000/shipper-test/proof/job/object?") {
		t.Errorf("the URL is %q, want the path-style bucket and key", signed)
	}

	if want := time.Date(2026, 8, 13, 4, 45, 0, 0, time.UTC); !expiresAt.Equal(want) {
		t.Errorf("expiresAt = %s, want %s", expiresAt, want)
	}
	if got := query(t, signed).Get("X-Amz-Expires"); got != "900" {
		t.Errorf("X-Amz-Expires = %q, want 900 — the URL and the reported expiry must agree", got)
	}
}

// TestTheSignatureCoversEveryInput mutates one input at a time and requires the signature to move.
//
// A signature that did not change when the content length did would mean the length was in the URL
// and not in the signature — which reads identically in a response and enforces nothing.
func TestTheSignatureCoversEveryInput(t *testing.T) {
	signer := testSigner(t)

	base, _, err := signer.PresignUpload(t.Context(), "proof/job/object", "image/jpeg", 1024, 15*time.Minute)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	baseSignature := query(t, base).Get("X-Amz-Signature")

	for _, tc := range []struct {
		name          string
		key           string
		contentType   string
		contentLength int64
		ttl           time.Duration
	}{
		{name: "a different key", key: "proof/job/other", contentType: "image/jpeg", contentLength: 1024, ttl: 15 * time.Minute},
		{name: "a different content type", key: "proof/job/object", contentType: "image/heic", contentLength: 1024, ttl: 15 * time.Minute},
		{name: "a different length", key: "proof/job/object", contentType: "image/jpeg", contentLength: 1025, ttl: 15 * time.Minute},
		{name: "a different lifetime", key: "proof/job/object", contentType: "image/jpeg", contentLength: 1024, ttl: 5 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signed, _, err := signer.PresignUpload(t.Context(), tc.key, tc.contentType, tc.contentLength, tc.ttl)
			if err != nil {
				t.Fatalf("signing: %v", err)
			}
			if got := query(t, signed).Get("X-Amz-Signature"); got == baseSignature {
				t.Error("the signature did not change, so this input is not signed")
			}
		})
	}
}

// TestTheVirtualHostFormPutsTheBucketInTheHost, because that host is what gets signed.
func TestTheVirtualHostFormPutsTheBucketInTheHost(t *testing.T) {
	signer, err := NewS3(Options{
		Endpoint:        "https://s3.ap-southeast-4.amazonaws.com",
		Bucket:          "shipper-production-evidence",
		Region:          "ap-southeast-4",
		AccessKeyID:     "unit-test-key",
		SecretAccessKey: "unit-test-secret",
		Clock:           clock.NewFixed(time.Date(2026, 8, 13, 4, 30, 0, 0, time.UTC)),
	})
	if err != nil {
		t.Fatalf("building the signer: %v", err)
	}

	signed, _, err := signer.PresignUpload(t.Context(), "proof/job/object", "image/jpeg", 1024, time.Minute)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if !strings.HasPrefix(signed, "https://shipper-production-evidence.s3.ap-southeast-4.amazonaws.com/proof/job/object?") {
		t.Errorf("the URL is %q, want the bucket in the host and the key alone in the path", signed)
	}
}

// TestTheSignerRefusesWhatItCannotSign covers the inputs that would produce a URL granting
// something other than what was asked for.
func TestTheSignerRefusesWhatItCannotSign(t *testing.T) {
	signer := testSigner(t)

	for _, tc := range []struct {
		name          string
		key           string
		contentType   string
		contentLength int64
		ttl           time.Duration
	}{
		{name: "no key", key: "", contentType: "image/jpeg", contentLength: 1, ttl: time.Minute},
		{name: "a traversing key", key: "proof/../../etc/passwd", contentType: "image/jpeg", contentLength: 1, ttl: time.Minute},
		{name: "an absolute key", key: "/proof/job/object", contentType: "image/jpeg", contentLength: 1, ttl: time.Minute},
		{name: "an empty segment", key: "proof//object", contentType: "image/jpeg", contentLength: 1, ttl: time.Minute},
		{name: "a control character in the key", key: "proof/job/ob\nject", contentType: "image/jpeg", contentLength: 1, ttl: time.Minute},
		{name: "an oversized key", key: strings.Repeat("a", maxObjectKey+1), contentType: "image/jpeg", contentLength: 1, ttl: time.Minute},
		{name: "no content type", key: "proof/job/object", contentType: "", contentLength: 1, ttl: time.Minute},
		{name: "a newline in the content type", key: "proof/job/object", contentType: "image/jpeg\nhost:elsewhere", contentLength: 1, ttl: time.Minute},
		{name: "no length", key: "proof/job/object", contentType: "image/jpeg", contentLength: 0, ttl: time.Minute},
		{name: "a negative length", key: "proof/job/object", contentType: "image/jpeg", contentLength: -1, ttl: time.Minute},
		{name: "no lifetime", key: "proof/job/object", contentType: "image/jpeg", contentLength: 1, ttl: 0},
		{name: "a sub-second lifetime", key: "proof/job/object", contentType: "image/jpeg", contentLength: 1, ttl: 500 * time.Millisecond},
		{name: "longer than the protocol allows", key: "proof/job/object", contentType: "image/jpeg", contentLength: 1, ttl: 8 * 24 * time.Hour},
	} {
		t.Run(tc.name, func(t *testing.T) {
			signed, _, err := signer.PresignUpload(t.Context(), tc.key, tc.contentType, tc.contentLength, tc.ttl)
			if err == nil {
				t.Fatalf("signed anyway: %s", signed)
			}
		})
	}
}

// TestNewS3RefusesAnUnusableConfiguration. internal/config refuses all of this at startup with a
// message naming the variable; reaching one of these means the signer was built from something
// other than configuration.
func TestNewS3RefusesAnUnusableConfiguration(t *testing.T) {
	usable := Options{
		Endpoint:        "http://localhost:9000",
		Bucket:          "shipper-test",
		Region:          "ap-southeast-2",
		AccessKeyID:     "unit-test-key",
		SecretAccessKey: "unit-test-secret",
		UsePathStyle:    true,
	}

	for _, tc := range []struct {
		name  string
		spoil func(*Options)
	}{
		{name: "no endpoint", spoil: func(o *Options) { o.Endpoint = "" }},
		{name: "an endpoint with no scheme", spoil: func(o *Options) { o.Endpoint = "localhost:9000" }},
		{name: "an endpoint with a path", spoil: func(o *Options) { o.Endpoint = "http://localhost:9000/shipper-test" }},
		{name: "no bucket", spoil: func(o *Options) { o.Bucket = "" }},
		{name: "no region", spoil: func(o *Options) { o.Region = "" }},
		{name: "no access key", spoil: func(o *Options) { o.AccessKeyID = "" }},
		{name: "no secret", spoil: func(o *Options) { o.SecretAccessKey = "" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opts := usable
			tc.spoil(&opts)
			if _, err := NewS3(opts); err == nil {
				t.Error("built a signer that cannot sign correctly")
			}
		})
	}

	if _, err := NewS3(usable); err != nil {
		t.Errorf("the usable options were refused: %v", err)
	}
}

// --- against the store that is actually running -------------------------------------------------

// liveSigner is a signer pointed at this worktree's bucket, or a failure saying how to get one.
//
// It reads the same STORAGE_* variables the service does, which `make` exports from deploy/.env —
// so `make test` gets this tree's own bucket and bare `go test` gets the shared development one.
// That is the same trap CLAUDE.md's worktree table names for the database, and the same answer:
// run the make target.
func liveSigner(t *testing.T) (*S3, string) {
	t.Helper()

	endpoint := env("STORAGE_ENDPOINT", "http://localhost:9000")
	bucket := env("STORAGE_BUCKET", "shipper-dev")

	signer, err := NewS3(Options{
		Endpoint:        endpoint,
		Bucket:          bucket,
		Region:          env("STORAGE_REGION", "ap-southeast-2"),
		AccessKeyID:     env("STORAGE_ACCESS_KEY_ID", "shipper"),
		SecretAccessKey: env("STORAGE_SECRET_ACCESS_KEY", "shipperminio"),
		UsePathStyle:    env("STORAGE_USE_PATH_STYLE", "true") != "false",
	})
	if err != nil {
		t.Fatalf("storage: building a signer for %s: %v", endpoint, err)
	}

	response, err := http.Get(endpoint + "/minio/health/live") //nolint:noctx // a liveness probe
	if err == nil {
		_ = response.Body.Close()
	}
	if err != nil {
		if testing.Short() {
			t.Skipf("storage: no object store at %s and -short was given (%v)", endpoint, err)
		}
		t.Fatalf("storage: no object store at %s (%v)\n"+
			"These tests exercise the real thing deliberately: a mock that never checks a\n"+
			"signature passes everything and fails the first time a bucket sees one.\n"+
			"Start it with `make up`, which also creates this worktree's bucket.", endpoint, err)
	}

	return signer, bucket
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// put sends body to a pre-signed URL with the headers named, and answers with the status.
func put(t *testing.T, signed, contentType, body string) int {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPut, signed, strings.NewReader(body))
	if err != nil {
		t.Fatalf("building the upload request: %v", err)
	}
	request.Header.Set("Content-Type", contentType)
	request.ContentLength = int64(len(body))

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("uploading: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)

	return response.StatusCode
}

// TestAPresignedURLUploadsToTheRealStore is SHIP-114's *Done when* in Go, and the rest of this
// file's integration tests are the ways it must fail.
//
// The object key carries the test's own identifier and every assertion names it, because the store
// is shared between worktrees and the bucket may be too: a fence on a topic or a bucket has to be
// an id rather than a count or a timestamp (CLAUDE.md's worktree table).
func TestAPresignedURLUploadsToTheRealStore(t *testing.T) {
	signer, bucket := liveSigner(t)
	key := testKey(t)
	t.Cleanup(func() { removeObject(t, signer, key) })

	const body = "not really a photograph, but exactly this many bytes"

	signed, expiresAt, err := signer.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
	if err != nil {
		t.Fatalf("signing an upload: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("the URL expired at %s, before it was used", expiresAt)
	}

	if status := put(t, signed, "image/jpeg", body); status != http.StatusOK {
		t.Fatalf("the pre-signed PUT into %s answered %d, want 200", bucket, status)
	}

	// Read back through a signed GET, which the test signs itself: the package deliberately
	// exposes no download method until SHIP-115 needs one.
	read, err := signer.presign(signer.clock.Now().UTC(), http.MethodGet, key, 300, nil)
	if err != nil {
		t.Fatalf("signing a read: %v", err)
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, read, nil)
	if err != nil {
		t.Fatalf("building the read request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("reading back: %v", err)
	}
	defer response.Body.Close()

	stored, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading the body: %v", err)
	}
	if response.StatusCode != http.StatusOK || string(stored) != body {
		t.Fatalf("the object read back as %d %q, want 200 %q", response.StatusCode, stored, body)
	}
	if got := response.Header.Get("Content-Type"); got != "image/jpeg" {
		t.Errorf("the stored object's content type is %q, want image/jpeg — "+
			"the signed header is what the store recorded", got)
	}
}

// TestTheBucketHasNoPublicReadPath. Everything in the store is private: a proof photograph
// identifies an address and a recipient (doc.go).
func TestTheBucketHasNoPublicReadPath(t *testing.T) {
	signer, bucket := liveSigner(t)
	key := testKey(t)
	t.Cleanup(func() { removeObject(t, signer, key) })

	const body = "private evidence"

	signed, _, err := signer.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if status := put(t, signed, "image/jpeg", body); status != http.StatusOK {
		t.Fatalf("the upload answered %d", status)
	}

	unsigned := strings.Split(signed, "?")[0]
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, unsigned, nil)
	if err != nil {
		t.Fatalf("building the unsigned request: %v", err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("fetching unsigned: %v", err)
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, response.Body)

	if response.StatusCode != http.StatusForbidden {
		t.Errorf("an unsigned GET of %s answered %d, want 403 — %s has a public read path",
			key, response.StatusCode, bucket)
	}
}

// TestTheStoreRefusesAnUploadThatChangesWhatWasSigned is the check that makes the platform's
// limits real rather than advisory.
//
// The delivery domain refuses an oversized or unaccepted upload before issuing a URL. This is what
// happens when a client is issued a URL and then sends something else — and the answer has to come
// from the store, because the API is not in the path.
func TestTheStoreRefusesAnUploadThatChangesWhatWasSigned(t *testing.T) {
	signer, _ := liveSigner(t)

	const body = "the body the platform authorised"

	t.Run("a different content type", func(t *testing.T) {
		key := testKey(t)
		t.Cleanup(func() { removeObject(t, signer, key) })

		signed, _, err := signer.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		if status := put(t, signed, "image/svg+xml", body); status != http.StatusForbidden {
			t.Errorf("uploading as image/svg+xml answered %d, want 403 — "+
				"the content type is signed, so the store must refuse a substitute", status)
		}
	})

	t.Run("a longer body", func(t *testing.T) {
		key := testKey(t)
		t.Cleanup(func() { removeObject(t, signer, key) })

		signed, _, err := signer.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		if status := put(t, signed, "image/jpeg", body+" and then some"); status != http.StatusForbidden {
			t.Errorf("uploading a longer body answered %d, want 403 — "+
				"the content length is signed, and it is the only bound a pre-signed PUT has", status)
		}
	})

	t.Run("another key entirely", func(t *testing.T) {
		key := testKey(t)
		t.Cleanup(func() { removeObject(t, signer, key) })

		signed, _, err := signer.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
		if err != nil {
			t.Fatalf("signing: %v", err)
		}
		elsewhere := strings.Replace(signed, key, testKey(t), 1)
		if status := put(t, elsewhere, "image/jpeg", body); status != http.StatusForbidden {
			t.Errorf("uploading to a repointed key answered %d, want 403", status)
		}
	})
}

// TestAnExpiredURLIsRefused, without waiting for one to expire.
//
// The lifetime is the whole of the authorisation — nothing revokes a pre-signed URL once it is
// signed (internal/config's Storage.PresignTTL) — so "short-lived" has to be a property the store
// acts on rather than a number in a response. Signed against a clock an hour in the past, which is
// the same trick scripts/verify/70-delivery.sh uses for an expired driver link.
func TestAnExpiredURLIsRefused(t *testing.T) {
	live, _ := liveSigner(t)
	key := testKey(t)

	// A second signer over the same options, stopped an hour ago.
	past := *live
	past.clock = clock.NewFixed(time.Now().UTC().Add(-time.Hour))

	const body = "too late"
	signed, expiresAt, err := past.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
	if err != nil {
		t.Fatalf("signing: %v", err)
	}
	if expiresAt.After(time.Now()) {
		t.Fatalf("the URL reports that it expires at %s, which has not happened yet", expiresAt)
	}

	if status := put(t, signed, "image/jpeg", body); status != http.StatusForbidden {
		t.Errorf("an expired URL uploaded with status %d, want 403", status)
	}
}

// testKey is an object key nothing else in any worktree will produce.
func testKey(t *testing.T) string {
	t.Helper()
	return "test/" + strings.ReplaceAll(t.Name(), "/", "-") + "/" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

// removeObject deletes what a test uploaded, signing the DELETE the same way.
//
// Best effort and never a failure: a leftover object in a development bucket is untidy rather than
// wrong, and a cleanup that failed a passing test would be worse than the mess.
func removeObject(t *testing.T, signer *S3, key string) {
	t.Helper()

	signed, err := signer.presign(time.Now().UTC(), http.MethodDelete, key, 60, nil)
	if err != nil {
		return
	}
	request, err := http.NewRequestWithContext(t.Context(), http.MethodDelete, signed, nil)
	if err != nil {
		return
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
}

// --- SHIP-115: reading an object back ------------------------------------------------------------

// TestStoredReportsWhatTheStoreActuallyHolds is the one request this package makes, against the
// store that is actually running.
//
// **It is the only place the claim can be checked.** The delivery domain stubs this port, so the
// assertion that a HEAD against a self-signed URL returns the object's real type, size and entity
// tag has nowhere else to live — and the values it reports are what `proofs` records and what the
// platform's upload limits are then applied to (SHIP-115).
func TestStoredReportsWhatTheStoreActuallyHolds(t *testing.T) {
	signer, _ := liveSigner(t)
	key := testKey(t)
	t.Cleanup(func() { removeObject(t, signer, key) })

	const body = "forty-nine bytes of something that is not a photo."

	signed, _, err := signer.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
	if err != nil {
		t.Fatalf("signing the upload: %v", err)
	}
	if status := put(t, signed, "image/jpeg", body); status != http.StatusOK {
		t.Fatalf("uploading answered %d, want 200", status)
	}

	contentType, contentLength, etag, found, err := signer.Stored(t.Context(), key)
	if err != nil {
		t.Fatalf("asking the store about %s: %v", key, err)
	}
	if !found {
		t.Fatal("the object was uploaded and the store says it is not there")
	}

	if contentType != "image/jpeg" {
		t.Errorf("content type = %q, want image/jpeg", contentType)
	}
	if contentLength != int64(len(body)) {
		t.Errorf("content length = %d, want %d", contentLength, len(body))
	}
	if etag == "" {
		t.Error("no entity tag came back, so a later overwrite of the bytes would be undetectable")
	}
	if strings.Contains(etag, `"`) {
		t.Errorf("etag = %q — the quotes are the header's syntax, not part of the tag", etag)
	}
}

// TestStoredSaysSoWhenNothingWasUploaded.
//
// This is the case SHIP-115 exists for: a URL is issued, the client never PUTs, and the platform
// has no other way to find out. A missing object is an answer with a nil error, so the domain can
// turn it into a refusal telling the driver to upload again rather than into a 500.
func TestStoredSaysSoWhenNothingWasUploaded(t *testing.T) {
	signer, _ := liveSigner(t)

	_, _, _, found, err := signer.Stored(t.Context(), testKey(t))
	if err != nil {
		t.Fatalf("asking about an object that was never uploaded: %v, want no error", err)
	}
	if found {
		t.Error("the store reported an object nobody uploaded")
	}
}

// TestStoredRefusesAWrongCredentialRatherThanCallingItMissing.
//
// A store answers 403 when the signature does not verify, and folding that into "not there" would
// turn a configuration fault into a message telling every driver their photograph never arrived.
// Driven with a signer holding the wrong secret, which is the shape a rotated key produces.
func TestStoredRefusesAWrongCredentialRatherThanCallingItMissing(t *testing.T) {
	good, bucket := liveSigner(t)
	key := testKey(t)
	t.Cleanup(func() { removeObject(t, good, key) })

	const body = "a real object, asked about with the wrong key"

	signed, _, err := good.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
	if err != nil {
		t.Fatalf("signing the upload: %v", err)
	}
	if status := put(t, signed, "image/jpeg", body); status != http.StatusOK {
		t.Fatalf("uploading answered %d, want 200", status)
	}

	bad, err := NewS3(Options{
		Endpoint:        env("STORAGE_ENDPOINT", "http://localhost:9000"),
		Bucket:          bucket,
		Region:          env("STORAGE_REGION", "ap-southeast-2"),
		AccessKeyID:     env("STORAGE_ACCESS_KEY_ID", "shipper"),
		SecretAccessKey: "this-is-not-the-secret-the-store-holds",
		UsePathStyle:    env("STORAGE_USE_PATH_STYLE", "true") != "false",
	})
	if err != nil {
		t.Fatalf("building a signer with a wrong secret: %v", err)
	}

	_, _, _, found, err := bad.Stored(t.Context(), key)
	if found {
		t.Fatal("an unverifiable signature was accepted")
	}
	if err == nil {
		t.Fatal("a refused credential was reported as an object that is not there, which would " +
			"tell every driver their photograph had failed to upload")
	}
}

// TestADownloadURLReadsTheObjectAndNothingElseDoes is the read path's half of SHIP-115's access
// control, at the store.
//
// Two claims in one test because they are one claim: the signed URL works, and the same object is
// refused without it. A download signer that happened to work on a bucket with a public read path
// would prove nothing at all — TestTheBucketHasNoPublicReadPath is the other half of that pairing
// and this one repeats the unsigned check on the object it just wrote.
func TestADownloadURLReadsTheObjectAndNothingElseDoes(t *testing.T) {
	signer, bucket := liveSigner(t)
	key := testKey(t)
	t.Cleanup(func() { removeObject(t, signer, key) })

	const body = "a photograph of somebody's front door, allegedly"

	upload, _, err := signer.PresignUpload(t.Context(), key, "image/jpeg", int64(len(body)), 5*time.Minute)
	if err != nil {
		t.Fatalf("signing the upload: %v", err)
	}
	if status := put(t, upload, "image/jpeg", body); status != http.StatusOK {
		t.Fatalf("uploading answered %d, want 200", status)
	}

	download, expiresAt, err := signer.PresignDownload(t.Context(), key, 5*time.Minute)
	if err != nil {
		t.Fatalf("signing the download: %v", err)
	}
	if !expiresAt.After(time.Now()) {
		t.Fatalf("the download expires at %s, which has already happened", expiresAt)
	}

	got, status := get(t, download)
	if status != http.StatusOK {
		t.Fatalf("the signed download answered %d, want 200", status)
	}
	if got != body {
		t.Errorf("the object reads back as %q, want %q", got, body)
	}

	// The same object, unsigned. A proof photograph identifies an address and a recipient, so
	// there is no route to one that does not carry a signature.
	endpoint := env("STORAGE_ENDPOINT", "http://localhost:9000")
	if _, status := get(t, endpoint+"/"+bucket+"/"+key); status != http.StatusForbidden {
		t.Errorf("an unsigned GET answered %d, want 403", status)
	}
}

// TestADownloadURLStopsWorkingWhenItSaysItWill.
//
// The lifetime is the whole of the control: nothing can revoke a pre-signed URL, so a URL that
// outlived its stated expiry would be a permanent link to a photograph of somebody's front door.
// Signed with a clock five minutes in the past against a one-minute window, so the store refuses it
// without the test waiting.
func TestADownloadURLStopsWorkingWhenItSaysItWill(t *testing.T) {
	_, bucket := liveSigner(t)

	expired, err := NewS3(Options{
		Endpoint:        env("STORAGE_ENDPOINT", "http://localhost:9000"),
		Bucket:          bucket,
		Region:          env("STORAGE_REGION", "ap-southeast-2"),
		AccessKeyID:     env("STORAGE_ACCESS_KEY_ID", "shipper"),
		SecretAccessKey: env("STORAGE_SECRET_ACCESS_KEY", "shipperminio"),
		UsePathStyle:    env("STORAGE_USE_PATH_STYLE", "true") != "false",
		Clock:           clock.NewFixed(time.Now().UTC().Add(-5 * time.Minute)),
	})
	if err != nil {
		t.Fatalf("building a signer with a past clock: %v", err)
	}

	signed, _, err := expired.PresignDownload(t.Context(), testKey(t), time.Minute)
	if err != nil {
		t.Fatalf("signing the download: %v", err)
	}
	if _, status := get(t, signed); status != http.StatusForbidden {
		t.Errorf("an expired download URL answered %d, want 403", status)
	}
}

// get fetches a URL and answers with its body and status.
func get(t *testing.T, signed string) (string, int) {
	t.Helper()

	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, signed, nil)
	if err != nil {
		t.Fatalf("building the download request: %v", err)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("downloading: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("reading the download: %v", err)
	}
	return string(body), response.StatusCode
}
