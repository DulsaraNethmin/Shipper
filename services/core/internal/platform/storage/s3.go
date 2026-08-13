package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The S3 implementation: pre-signed URLs, and one metadata request (SHIP-114, SHIP-115).
//
// # No object's bytes pass through this service, and everything below follows from that
//
// Worth stating before the first line of signing code, because it is what decides the shape of
// everything here. doc.go's rule is that files do not pass through this service: the platform
// hands a client a URL and the client moves the bytes. There is no upload path, no download path,
// no bucket listing and no delete.
//
// **SHIP-114 stated that more strongly — "this package makes no request to the object store,
// ever" — and SHIP-115 had to narrow it.** [S3.Stored] asks the store what it holds under a key,
// and it exists because of the same architecture rather than in spite of it: with the bytes going
// straight from the client to the store, a metadata request is the *only* moment the platform can
// learn that an upload happened at all. The narrowed rule is the one that was always doing the
// work — **no transfer through this service** — and a HEAD carries no body in either direction.
//
// One request of one shape, against an object this platform named, and still no SDK. The signing
// algorithm is a published specification and about eighty lines of HMAC; the SDK is a transport, a
// retry policy, a credential chain and a dozen modules, and the one request below is
// `http.Client.Do` against a URL this file already knows how to sign. The same round trip is
// demonstrated end to end in scripts/verify/00-stack.sh (SHIP-15p) in forty lines of Python, and
// that section is the reference this file was written against.
//
// **The second reason is a rule rather than a preference**: go.mod is a shared surface no domain
// branch may edit (Docs/10 §9.2), so adding a module is a request to whoever owns the wave rather
// than a commit. That constraint did not decide the design — the first paragraph did — but it is
// what made the question worth asking before writing any code.
//
// # SigV4 signs the host header, and that is the failure everybody meets first
//
// A URL pre-signed for one address and fetched at another is refused with SignatureDoesNotMatch,
// an error naming neither the address nor the cause. So [Options.Endpoint] is the address the
// *client* will use — the published host port in development, not `minio:9000` inside the compose
// network — and internal/config's Storage.Endpoint says the same thing at greater length.
//
// The region is signed too, in the credential scope, which is why the local store is started with
// MINIO_REGION set from the same variable the signer reads.

const (
	// signingAlgorithm, sigV4Service and sigV4Terminator are the fixed strings of the
	// AWS Signature Version 4 query protocol.
	signingAlgorithm = "AWS4-HMAC-SHA256"
	sigV4Service     = "s3"
	sigV4Terminator  = "aws4_request"

	// unsignedPayload tells the store not to expect a payload hash in the signature.
	//
	// The alternative is signing SHA-256 of the body, which this service cannot do: it never
	// sees the body. What replaces it is signing `content-length` and `content-type`, so the
	// store still refuses an upload that is not the one the platform authorised — see
	// [S3.PresignUpload].
	unsignedPayload = "UNSIGNED-PAYLOAD"

	// maxPresignTTL is the protocol's own ceiling on X-Amz-Expires: seven days.
	//
	// It is not the platform's limit. internal/config caps the configured TTL at an hour and
	// says why; this refuses what S3 itself would refuse, so a caller passing a week gets an
	// error here rather than a URL the store rejects.
	maxPresignTTL = 7 * 24 * time.Hour

	// maxObjectKey is S3's limit on a key, in bytes.
	maxObjectKey = 1024

	// metadataTTL is how long the URL [S3.Stored] signs for itself is good for.
	//
	// It is not configuration and must not become configuration. Nothing is handed this URL —
	// it is signed, spent on the next line and discarded — so the window is a bound on one
	// in-process round trip rather than a policy about what a credential may reach. Ten seconds
	// is the shortest value that survives a clock a few seconds out of step with the store's,
	// which is the only realistic way a URL signed here arrives already expired.
	metadataTTL = 10 * time.Second

	// metadataTimeout bounds the request itself.
	//
	// Short on purpose: this runs on the path a driver takes to record a delivery, and Docs/01
	// §4.4 is written about not stranding one. A store that is not answering within this is a
	// store the client could not have uploaded to either, so failing quickly and letting the
	// retry come back is better than holding the request open.
	metadataTimeout = 5 * time.Second

	// maxErrorBody is how much of a refusal's body is read before giving up on it.
	//
	// S3 answers a failed request with an XML document naming the code. None of it is shown to a
	// client — it becomes an opaque 500 with the cause logged (SHIP-15i) — but a person reading
	// that log wants `NoSuchBucket` rather than a status number, and an unbounded read of a body
	// this service did not ask for is how a store having a bad day becomes this service having
	// one.
	maxErrorBody = 4 << 10
)

// Options is everything the S3 implementation needs to sign.
//
// The adapter's own struct rather than a slice of internal/config, for the reason
// geocoding.Options gives: a package that reads its own configuration decides for itself where it
// runs, and cannot be tested without the environment. The composition root reads configuration and
// this takes arguments.
//
// **Note what is not here.** The pre-signed URL's lifetime, the maximum upload size and the
// accepted content types are all in internal/config's Storage section and none of them is a field
// below. They are policy about what may be uploaded, which is the consuming domain's question
// (Docs/06 §4.1); this package is asked to sign a URL for a key, a type and a length, and it signs
// one. The TTL arrives as an argument to [S3.PresignUpload] for the same reason.
type Options struct {
	// Endpoint is the object store's S3 API, as the *client* will reach it. Scheme and host,
	// with no path — see the file header for why the host is load-bearing.
	Endpoint string

	// Bucket holds every object. One bucket, prefixed by concern (internal/config).
	Bucket string

	// Region is signed as part of the credential scope rather than merely routed on.
	Region string

	// AccessKeyID and SecretAccessKey authenticate the signature. The secret is never logged
	// and never leaves this package: what a client receives is a signature over it.
	AccessKeyID     string
	SecretAccessKey string

	// UsePathStyle addresses the bucket as `endpoint/bucket/key` rather than
	// `bucket.endpoint/key`. True for the development store, where the virtual-host form would
	// need `shipper-dev.localhost` to resolve and does not.
	UsePathStyle bool

	// Clock is the signer's own clock, and it is what X-Amz-Date is read from.
	//
	// Defaulted to clock.System when nil, unlike the domain constructors that panic. The
	// difference is the failure direction: a service built with the wrong clock here signs a
	// URL the store refuses outright, which is loud, immediate and attributable — not the
	// silent wrong answer Docs/10 §6.3 is written about.
	Clock clock.Clock

	// HTTPClient replaces the client this package would otherwise build for [S3.Stored], which
	// is the only method that makes a request (SHIP-115).
	//
	// The default wraps httpx.PropagateRequestID, so a metadata read is traceable back to the
	// request that caused it (SHIP-14) — the same shape geocoding.Options takes, and for the
	// same reason.
	HTTPClient *http.Client
}

// S3 signs pre-signed URLs against an S3-compatible object store.
//
// Safe for concurrent use: every field is set once at construction and nothing below writes.
type S3 struct {
	scheme string
	host   string

	bucket string
	region string

	accessKeyID     string
	secretAccessKey string

	usePathStyle bool

	clock  clock.Clock
	client *http.Client
}

// NewS3 validates opts and returns the signer.
//
// Everything it refuses, internal/config has already refused at startup with a message naming the
// variable — so reaching one of these errors means this package was constructed from something
// other than configuration. They are checked anyway: a signer built with an empty secret produces
// URLs that verify against nothing, and the first thing to notice would be a driver's upload.
func NewS3(opts Options) (*S3, error) {
	endpoint, err := url.Parse(opts.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("storage: endpoint %q is not a URL: %w", opts.Endpoint, err)
	}
	if endpoint.Scheme != "http" && endpoint.Scheme != "https" {
		return nil, fmt.Errorf("storage: endpoint %q must be http or https", opts.Endpoint)
	}
	if endpoint.Host == "" {
		return nil, fmt.Errorf("storage: endpoint %q names no host, and the host is what is signed",
			opts.Endpoint)
	}
	// A path prefix is refused rather than carried. It would have to appear in the canonical
	// URI ahead of the bucket, and no deployment of this platform has one — an endpoint with a
	// path is far more likely to be a bucket URL pasted into the wrong variable, which would
	// otherwise sign `/<bucket>/<bucket>/<key>`.
	if endpoint.Path != "" && endpoint.Path != "/" {
		return nil, fmt.Errorf("storage: endpoint %q must be a scheme and host with no path",
			opts.Endpoint)
	}

	switch {
	case opts.Bucket == "":
		return nil, fmt.Errorf("storage: no bucket")
	case opts.Region == "":
		return nil, fmt.Errorf("storage: no region, and the region is signed into the credential scope")
	case opts.AccessKeyID == "":
		return nil, fmt.Errorf("storage: no access key id")
	case opts.SecretAccessKey == "":
		return nil, fmt.Errorf("storage: no secret access key")
	}

	c := opts.Clock
	if c == nil {
		c = clock.System{}
	}

	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{
			Timeout:   metadataTimeout,
			Transport: httpx.PropagateRequestID(nil),
		}
	}

	return &S3{
		scheme:          endpoint.Scheme,
		host:            endpoint.Host,
		bucket:          opts.Bucket,
		region:          opts.Region,
		accessKeyID:     opts.AccessKeyID,
		secretAccessKey: opts.SecretAccessKey,
		usePathStyle:    opts.UsePathStyle,
		clock:           c,
		client:          client,
	}, nil
}

// PresignUpload returns a URL a client may PUT exactly one object to, and when it stops working.
//
// # Three headers are signed, and two of them are the whole point
//
// `host`, because SigV4 always signs it. And then `content-type` and `content-length`, which is
// what turns the platform's limits from advice into enforcement:
//
//   - Without them the URL authorises *any* body. A caller could ask for a 200 KB `image/jpeg`,
//     be told yes, and then PUT a 400 MB file of any type at all — and every check the domain
//     made would have been a check on a promise.
//   - With them the store recomputes the signature over the headers the request actually
//     carried, so an upload that changes either is refused with SignatureDoesNotMatch. The size
//     limit and the accepted-type list are then enforced by the store on the request that
//     matters, rather than by the API on a request that carries no bytes.
//
// `content-length` is the only bound on size available to a pre-signed PUT — S3's
// `content-length-range` belongs to the browser POST-policy form, which is a different protocol —
// and it is exact rather than a maximum. A client therefore states the size it is about to send,
// which it knows: it has the file.
//
// ctx is unused today and is in the signature anyway, because a port that has to grow one later
// changes every implementation and every caller at once.
func (s *S3) PresignUpload(
	_ context.Context,
	key, contentType string,
	contentLength int64,
	ttl time.Duration,
) (uploadURL string, expiresAt time.Time, err error) {
	if err := validateObjectKey(key); err != nil {
		return "", time.Time{}, err
	}
	if err := validateHeaderValue("content type", contentType); err != nil {
		return "", time.Time{}, err
	}
	if contentLength <= 0 {
		return "", time.Time{}, fmt.Errorf("storage: an upload of %d bytes signs nothing a client could send",
			contentLength)
	}

	seconds, err := signedSeconds(ttl)
	if err != nil {
		return "", time.Time{}, err
	}

	now := s.clock.Now().UTC()
	signed, err := s.presign(now, http.MethodPut, key, seconds, [][2]string{
		{"content-length", strconv.FormatInt(contentLength, 10)},
		{"content-type", contentType},
	})
	if err != nil {
		return "", time.Time{}, err
	}

	// The instant the store will start refusing, derived from the same `now` and the same
	// count of seconds the signature carries. Computed rather than read from a second clock:
	// a caller told a different expiry from the one in the URL would report a live link as
	// dead or, worse, the other way round.
	return signed, now.Add(time.Duration(seconds) * time.Second), nil
}

// PresignDownload returns a URL a client may GET one object at, and when it stops working
// (SHIP-115).
//
// # It signs `host` and nothing else, which is the opposite of the upload and is correct
//
// [S3.PresignUpload] binds `content-type` and `content-length` because the request it authorises
// carries a body the platform has not seen and wants to bound. A GET carries none: the only thing
// this URL can do is fetch the bytes that are already there, under a key the caller could not
// choose. There is nothing further to constrain, and signing a header the client would then have
// to reproduce exactly would be a signature failure waiting for the first browser that normalises
// one.
//
// # It is a credential, and the authorisation happened before this was called
//
// Whoever holds this URL can read the object until it expires, and nothing can revoke it — the
// same property the upload URL has and the same reason the lifetime is short. **Nothing here
// checks who is asking.** doc.go's rule is that this package is not authoritative about who may
// see a file; the delivery domain decides that from `proofs` and calls this afterwards, and this
// method would sign a URL for any key it is handed.
//
// ctx is unused for the same reason [S3.PresignUpload]'s is.
func (s *S3) PresignDownload(
	_ context.Context,
	key string,
	ttl time.Duration,
) (downloadURL string, expiresAt time.Time, err error) {
	if err := validateObjectKey(key); err != nil {
		return "", time.Time{}, err
	}

	seconds, err := signedSeconds(ttl)
	if err != nil {
		return "", time.Time{}, err
	}

	now := s.clock.Now().UTC()
	signed, err := s.presign(now, http.MethodGet, key, seconds, nil)
	if err != nil {
		return "", time.Time{}, err
	}
	return signed, now.Add(time.Duration(seconds) * time.Second), nil
}

// Stored is what the object store holds under key: the media type it recorded, the size it
// accepted, and its entity tag for the bytes. found is false when there is no such object
// (SHIP-115).
//
// # This is the one request this package makes, and the architecture is why it has to exist
//
// The bytes go from the client straight to the store, so the platform never observes an upload:
// it issues a URL and then hears nothing, whether the photograph arrived, failed halfway or was
// never attempted. **Asking is the only way to find out.** Without it the platform's record of
// proof would be the client's assertion that it uploaded something, and Docs/01 §4.4 makes proof
// "the *only* evidence that the job happened as claimed" — an assertion is not that.
//
// It reports what the store says rather than what anybody expected, which is the second reason it
// is here. `internal/delivery` compares the returned type and length against
// STORAGE_ACCEPTED_CONTENT_TYPES and STORAGE_MAX_UPLOAD_BYTES before recording anything, so those
// limits are enforced against the object that actually exists — not only against the request that
// asked for permission to create one, which carried no bytes.
//
// # Signed for itself, spent immediately
//
// The URL below never leaves this function. Signing one rather than building an HTTP
// `Authorization` header (spelling:ok — the header's own name, RFC 9110 §11.6.2) keeps the whole
// file on one protocol: the query form is what every other method here produces and what
// `TestTheAWSExampleSignsToThePublishedSignature` holds to AWS's published answer, and header
// signing is a second, longer specification for no gain.
//
// # 404 is an answer; everything else is a fault
//
// A missing object means the client has not uploaded yet, which is an ordinary thing for a driver
// on a poor connection and becomes a refusal the client can act on. Anything else — 403, 500, a
// connection that never opens — is reported as an error and becomes an opaque 500 with its cause
// logged. **403 is deliberately not folded into "not there"**: a store answers it when the
// credentials are wrong, and reading that as "the driver never uploaded" would turn a
// configuration fault into a message blaming the driver.
func (s *S3) Stored(
	ctx context.Context,
	key string,
) (contentType string, contentLength int64, etag string, found bool, err error) {
	if err := validateObjectKey(key); err != nil {
		return "", 0, "", false, err
	}

	seconds, err := signedSeconds(metadataTTL)
	if err != nil {
		return "", 0, "", false, err
	}

	signed, err := s.presign(s.clock.Now().UTC(), http.MethodHead, key, seconds, nil)
	if err != nil {
		return "", 0, "", false, err
	}

	// The method has to be the one that was signed. SigV4 covers it in the canonical request,
	// so a HEAD spent against a GET-signed URL is SignatureDoesNotMatch — which would look
	// exactly like a wrong secret.
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, signed, nil)
	if err != nil {
		return "", 0, "", false, fmt.Errorf("storage: asking about %q: %w", key, err)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", 0, "", false, fmt.Errorf("storage: asking about %q: %w", key, err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return "", 0, "", false, nil

	case resp.StatusCode < 200 || resp.StatusCode > 299:
		return "", 0, "", false, fmt.Errorf("storage: asking about %q: the store answered %s%s",
			key, resp.Status, describeBody(resp.Body))
	}

	// ContentLength is -1 when the store sends no header, which no S3 implementation does for a
	// HEAD of an object that exists. Refused rather than recorded as -1: the value is compared
	// against a configured limit one caller up, and a negative number passes every such
	// comparison.
	if resp.ContentLength < 0 {
		return "", 0, "", false, fmt.Errorf("storage: %q exists and the store reported no length", key)
	}

	// The entity tag is quoted in the header — `"d41d8…"` — and the quotes are part of the
	// syntax rather than of the value (RFC 9110 §8.8.3). Stripped here so that what is stored is
	// the tag, not a rendering of it, and so a weak validator's `W/` prefix is visible in a
	// comparison rather than hidden inside a quoted string.
	return resp.Header.Get("Content-Type"), resp.ContentLength,
		strings.Trim(resp.Header.Get("ETag"), `"`), true, nil
}

// signedSeconds is X-Amz-Expires: a lifetime as the integer count of seconds the protocol carries.
//
// One function rather than three copies, because the three methods that sign must not be able to
// disagree about rounding. Truncated rather than rounded: rounding up hands out a URL that
// outlives what the caller asked for, which is the wrong direction for a credential nothing can
// revoke.
func signedSeconds(ttl time.Duration) (int64, error) {
	switch {
	case ttl <= 0:
		return 0, fmt.Errorf("storage: a URL good for %s is good for nothing", ttl)
	case ttl > maxPresignTTL:
		return 0, fmt.Errorf("storage: %s is longer than the protocol's own ceiling of %s",
			ttl, maxPresignTTL)
	}

	seconds := int64(ttl / time.Second)
	if seconds == 0 {
		return 0, fmt.Errorf("storage: %s is less than the one-second resolution of X-Amz-Expires", ttl)
	}
	return seconds, nil
}

// describeBody is as much of a refusal as is worth putting in an error, or nothing.
//
// Bounded by maxErrorBody and never shown to a client: the caller wraps this into an opaque 500
// and httpx logs the cause against the request id.
func describeBody(body io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(body, maxErrorBody))
	if err != nil || len(raw) == 0 {
		return ""
	}
	return ": " + strings.TrimSpace(string(raw))
}

// presign is the AWS Signature Version 4 query protocol, in one function.
//
// It is written to be read beside the specification rather than to be short. Every string joined
// below is a named part of it, and the one thing a reader should check is that nothing reorders:
// the canonical query must be sorted by escaped name, and the canonical headers by lower-cased
// name.
//
// extraHeaders must already be lower-cased and sorted; PresignUpload is the only caller and passes
// them that way. `host` is appended here because it is signed on every request and forgetting it
// is not a mistake this function should make twice.
func (s *S3) presign(
	now time.Time,
	method, key string,
	expiresSeconds int64,
	extraHeaders [][2]string,
) (string, error) {
	amzDate := now.Format("20060102T150405Z")
	dateStamp := now.Format("20060102")
	scope := strings.Join([]string{dateStamp, s.region, sigV4Service, sigV4Terminator}, "/")

	host, canonicalURI := s.address(key)

	headers := append(append([][2]string{}, extraHeaders...), [2]string{"host", host})
	sort.Slice(headers, func(i, j int) bool { return headers[i][0] < headers[j][0] })

	names := make([]string, 0, len(headers))
	var canonicalHeaders strings.Builder
	for _, h := range headers {
		names = append(names, h[0])
		canonicalHeaders.WriteString(h[0])
		canonicalHeaders.WriteString(":")
		canonicalHeaders.WriteString(strings.TrimSpace(h[1]))
		canonicalHeaders.WriteString("\n")
	}
	signedHeaders := strings.Join(names, ";")

	query := [][2]string{
		{"X-Amz-Algorithm", signingAlgorithm},
		{"X-Amz-Credential", s.accessKeyID + "/" + scope},
		{"X-Amz-Date", amzDate},
		{"X-Amz-Expires", strconv.FormatInt(expiresSeconds, 10)},
		{"X-Amz-SignedHeaders", signedHeaders},
	}
	canonicalQuery := canonicalQueryString(query)

	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQuery,
		canonicalHeaders.String(),
		signedHeaders,
		unsignedPayload,
	}, "\n")

	stringToSign := strings.Join([]string{
		signingAlgorithm,
		amzDate,
		scope,
		hex.EncodeToString(sha256Sum(canonicalRequest)),
	}, "\n")

	signature := hex.EncodeToString(hmacSHA256(s.signingKey(dateStamp), stringToSign))

	return s.scheme + "://" + host + canonicalURI + "?" + canonicalQuery +
		"&X-Amz-Signature=" + signature, nil
}

// signingKey derives the date-, region- and service-scoped key the signature is taken with.
//
// Four nested HMACs, in this order. The scope they build is the same one X-Amz-Credential names,
// which is why a URL signed for one region cannot be spent in another however it is routed.
func (s *S3) signingKey(dateStamp string) []byte {
	key := hmacSHA256([]byte("AWS4"+s.secretAccessKey), dateStamp)
	key = hmacSHA256(key, s.region)
	key = hmacSHA256(key, sigV4Service)
	return hmacSHA256(key, sigV4Terminator)
}

// address is the host to sign and the canonical URI to sign it against.
//
// The two forms differ in where the bucket goes, and both are exercised: development is path-style
// against a store on localhost, and a deployment against AWS is virtual-host.
func (s *S3) address(key string) (host, canonicalURI string) {
	if s.usePathStyle {
		return s.host, "/" + escapePath(s.bucket) + "/" + escapePath(key)
	}
	return s.bucket + "." + s.host, "/" + escapePath(key)
}

// canonicalQueryString is the query, escaped and sorted the way the signature expects.
//
// Sorted by the *escaped* name, which is what the specification says and what the store will do
// when it recomputes. Built by hand rather than with url.Values.Encode because that helper writes
// a space as `+` and this protocol requires `%20` — a difference no value here currently has, and
// exactly the kind that would appear the first time a key contained one.
func canonicalQueryString(pairs [][2]string) string {
	escaped := make([][2]string, 0, len(pairs))
	for _, p := range pairs {
		escaped = append(escaped, [2]string{escapeRFC3986(p[0]), escapeRFC3986(p[1])})
	}
	sort.Slice(escaped, func(i, j int) bool {
		if escaped[i][0] != escaped[j][0] {
			return escaped[i][0] < escaped[j][0]
		}
		return escaped[i][1] < escaped[j][1]
	})

	parts := make([]string, 0, len(escaped))
	for _, p := range escaped {
		parts = append(parts, p[0]+"="+p[1])
	}
	return strings.Join(parts, "&")
}

// escapePath escapes every character of an object key except the separators between its segments.
//
// A key is a name, not a path, so `/` is data — but it is the one byte the canonical URI must not
// escape, because the store splits on it too.
func escapePath(key string) string {
	segments := strings.Split(key, "/")
	for i, segment := range segments {
		segments[i] = escapeRFC3986(segment)
	}
	return strings.Join(segments, "/")
}

// escapeRFC3986 percent-encodes everything outside the unreserved set.
//
// Written out rather than taken from net/url, which has three escaping modes and none of them is
// this one: url.QueryEscape writes a space as `+`, and url.PathEscape leaves several sub-delimiters
// alone. The signature is a byte-for-byte agreement with the store about how a string was written
// down, so the encoding has to be the specification's rather than a near neighbour.
func escapeRFC3986(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case 'A' <= c && c <= 'Z', 'a' <= c && c <= 'z', '0' <= c && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// validateObjectKey refuses a key that would name something other than what the caller meant.
//
// The domain builds every key this package sees, so none of this is reachable from a request
// today. It is checked because the *next* caller may not — SHIP-115 stores keys and SHIP-155 hands
// them to an administrator — and a key with a `..` segment in it is a request for a different
// object, which percent-encoding alone does not prevent: the store resolves the path.
func validateObjectKey(key string) error {
	switch {
	case key == "":
		return fmt.Errorf("storage: an object key is required")
	case len(key) > maxObjectKey:
		return fmt.Errorf("storage: an object key of %d bytes is over the %d-byte limit",
			len(key), maxObjectKey)
	case strings.HasPrefix(key, "/"):
		return fmt.Errorf("storage: object key %q begins with a separator", key)
	}

	for _, segment := range strings.Split(key, "/") {
		switch segment {
		case "":
			return fmt.Errorf("storage: object key %q has an empty segment", key)
		case ".", "..":
			return fmt.Errorf("storage: object key %q has a %q segment, which names another object",
				key, segment)
		}
	}

	for i := 0; i < len(key); i++ {
		if key[i] < 0x20 || key[i] == 0x7f {
			return fmt.Errorf("storage: object key %q holds a control character at byte %d", key, i)
		}
	}
	return nil
}

// validateHeaderValue refuses a value that cannot be signed as one header.
//
// A newline is the one that matters: a value carrying one would be two lines of the canonical
// headers block, so a caller able to choose it could sign a request the platform never described.
// Nothing can send that value today — the domain checks the content type against a configured list
// — and this is the layer that does not depend on that remaining true.
func validateHeaderValue(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("storage: the %s is required", name)
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] == 0x7f {
			return fmt.Errorf("storage: the %s %q holds a control character at byte %d", name, value, i)
		}
	}
	return nil
}

func hmacSHA256(key []byte, data string) []byte {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(data))
	return mac.Sum(nil)
}

func sha256Sum(data string) []byte {
	sum := sha256.Sum256([]byte(data))
	return sum[:]
}
