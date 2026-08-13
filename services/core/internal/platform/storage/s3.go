package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
)

// The S3 implementation: pre-signed URLs, and nothing else (SHIP-114).
//
// # This package makes no request to the object store, ever
//
// Worth stating before the first line of signing code, because it is what decides the shape of
// everything below. doc.go's rule is that files do not pass through this service, and the
// consequence is that the platform never speaks the S3 API at all — it hands a client a URL and
// the client speaks it. There is no upload path, no download path, no bucket listing and no
// delete, so there is nothing here for an SDK to do.
//
// That is why this signs by hand against the standard library rather than taking a dependency on
// aws-sdk-go-v2. The signing algorithm is a published specification and about eighty lines of
// HMAC; the SDK is a transport, a retry policy, a credential chain and a dozen modules, none of
// which this package would use. The same round trip is already demonstrated end to end in
// scripts/verify/00-stack.sh (SHIP-15p) in forty lines of Python, and that section is the
// reference this file was written against.
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

	clock clock.Clock
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

	return &S3{
		scheme:          endpoint.Scheme,
		host:            endpoint.Host,
		bucket:          opts.Bucket,
		region:          opts.Region,
		accessKeyID:     opts.AccessKeyID,
		secretAccessKey: opts.SecretAccessKey,
		usePathStyle:    opts.UsePathStyle,
		clock:           c,
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
// # What it deliberately does not do
//
// There is no PresignDownload. Reading proof back needs an authorisation check this package must
// not make and a consumer that does not exist yet — SHIP-115 for the customer's view and SHIP-155
// for the administrator's — and a second signed method written before either is the speculative
// seam Docs/06 §4.1 exists to refuse. When one arrives it is four lines: this method's body with
// GET and no content headers.
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
	if ttl <= 0 {
		return "", time.Time{}, fmt.Errorf("storage: a URL good for %s is good for nothing", ttl)
	}
	if ttl > maxPresignTTL {
		return "", time.Time{}, fmt.Errorf("storage: %s is longer than the protocol's own ceiling of %s",
			ttl, maxPresignTTL)
	}

	// Truncated to whole seconds because X-Amz-Expires is an integer count of them. Rounding
	// up would hand out a URL that outlives what the caller asked for, which is the wrong
	// direction for a credential nothing can revoke.
	seconds := int64(ttl / time.Second)
	if seconds == 0 {
		return "", time.Time{}, fmt.Errorf("storage: %s is less than the one-second resolution of X-Amz-Expires", ttl)
	}

	now := s.clock.Now().UTC()
	signed, err := s.presign(now, "PUT", key, seconds, [][2]string{
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
