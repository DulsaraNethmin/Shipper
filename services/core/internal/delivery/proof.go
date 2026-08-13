package delivery

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// Proof of delivery, part one: getting the photograph into the store (SHIP-114).
//
// # The platform never sees the photograph, and everything here follows from that
//
// Docs/06 §5.2: "proof-of-delivery and verification images uploaded directly to private object
// storage using short-lived pre-signed URLs, never proxied through the API". A driver on a poor
// connection uploading through this service would hold a request goroutine and a server timeout
// for the length of the upload, and would put megabytes of image through a service sized for JSON.
//
// So what this file produces is **permission, not storage**. The client asks for a URL, the
// platform decides whether they may have one and what it authorises, and the bytes travel between
// the client and the store with no third party in the path.
//
// # Which means the URL is the authorisation, and its lifetime is the whole of the control
//
// Nothing revokes a pre-signed URL. There is no session to end, no row to delete and no denylist
// to add to — the store will honour it until the second it was signed to expire, whoever is
// holding it by then. That is why the lifetime is configuration rather than a constant
// (internal/config bounds it at an hour and says why), and why the two things a URL is bound to —
// the content type and the length — are **signed** rather than merely checked here. A limit this
// file enforces and the URL does not carry is a limit the client could have made up.
//
// # What this ticket deliberately does not do
//
// It writes nothing. There is no `proof` table, no row, no migration — **SHIP-115 is "uploaded
// proof is linked to a job and milestone with access control"**, and that record is its *Done
// when* rather than this one's. An object in the bucket is not proof of anything until SHIP-115
// says which job and which milestone it belongs to; until then it is bytes with a key.
//
// That is also the honest answer to what a retry returns — see [Service.PresignProofUpload].

// maxProofContentType bounds the media type a client may ask for.
//
// A bound against a runaway text field rather than a judgement, which is why it is a constant here
// and [UploadPolicy] is not: the *list* of acceptable types moves under operational pressure and
// lives in configuration, while "a media type is not four kilobytes long" never will.
const maxProofContentType = 128

// proofKeyPrefix is where every proof object lives in the bucket.
//
// One bucket prefixed by concern, which internal/config's Storage.Bucket describes: verification
// documents (SHIP-155) take a prefix of their own rather than a second bucket, because both kinds
// of object are private evidence under the same access rules.
const proofKeyPrefix = "proof"

// UploadPolicy is what the platform will issue an upload URL for, from configuration.
//
// # Every field is server-side, and Docs/06 §5.3 is why
//
// "Anything expected to change under operational pressure lives server-side… Flutter has no
// over-the-air update path for Dart code." All three of these move: a size limit is raised the
// first time a handset's camera outgrows it, a type list gained HEIC without an app release
// anywhere, and a lifetime is tightened after an incident. A limit compiled into the client is a
// limit that needs a store review to change, and the concrete cost of getting it wrong is stated in
// internal/config: a proof photograph a driver cannot upload is a delivery that cannot be completed
// (Docs/01 §4.4).
//
// It is a value rather than a pointer to the configuration, so a service holds the policy it was
// built with and a test can hand it another.
type UploadPolicy struct {
	// MaxBytes is the largest object an upload URL will be issued for.
	MaxBytes int64

	// AcceptedContentTypes is the closed set of media types, lower case.
	//
	// A list rather than an `image/` prefix match, so that `image/svg+xml` — a script container
	// browsers execute — cannot arrive by being an image.
	AcceptedContentTypes []string

	// URLTTL is how long an issued URL works for.
	URLTTL time.Duration
}

// valid reports whether this policy can issue anything at all.
//
// Checked at construction rather than per request: a service built with a zero policy would refuse
// every upload with a validation error naming the client's own perfectly good request, which is the
// worst place for a configuration failure to surface.
func (p UploadPolicy) valid() bool {
	return p.MaxBytes > 0 && len(p.AcceptedContentTypes) > 0 && p.URLTTL > 0
}

// UploadRequest is what a client says it is about to upload.
//
// # The length is stated, and it is not a formality
//
// A pre-signed PUT has exactly one bound available to it — the signed `Content-Length` — so the
// size a client declares here is the size the store will accept and no other. It is the client's
// own file, so it knows; and stating it is what turns [UploadPolicy.MaxBytes] from a number in a
// document into something the object store enforces on the request that carries the bytes.
//
// There is no field for the job and none for the caller. The job is in the path and the provider is
// the token — an identifier in the body would be an authorisation decision made from client input,
// which Docs/07 §3 puts on the platform.
type UploadRequest struct {
	ContentType   string
	ContentLength int64
}

// normalise lower-cases the media type and trims it.
//
// Media types are case-insensitive (RFC 9110 §8.3), the configured list is lower case, and the
// value is *signed* — so a client sending `IMAGE/JPEG` must be told to send `image/jpeg`, which
// means the platform has to decide on one spelling before it signs.
func (u UploadRequest) normalise() UploadRequest {
	u.ContentType = strings.ToLower(strings.TrimSpace(u.ContentType))
	return u
}

// problems reports what is wrong with the request, in the error contract's shape.
//
// The message names the limit rather than saying "too large". A driver's client has to compress to
// fit (Docs/01 §5.2) and cannot choose a target it has not been told, and the limit is configuration
// — so the refusal is where a client learns the current one.
func (u UploadRequest) problems(policy UploadPolicy) validate.Errors {
	var e validate.Errors

	if e.Required("content_type", u.ContentType) {
		e.Length("content_type", u.ContentType, 1, maxProofContentType)

		// Named rather than left to validate.OneOf, whose message is "that is not one of the
		// available options". The set is configuration, so a client cannot have it compiled in
		// and the refusal is the only place it learns the current one — the same call
		// [recordingFrom] makes about the milestone vocabulary.
		if !slices.Contains(policy.AcceptedContentTypes, u.ContentType) {
			e.Add("content_type", validate.CodeNotAllowed,
				"That is not a format this platform accepts. Upload one of %s.",
				strings.Join(policy.AcceptedContentTypes, ", "))
		}
	}

	switch {
	case u.ContentLength <= 0:
		e.Add("content_length", validate.CodeRequired,
			"Send the exact size of the photograph in bytes.")
	case u.ContentLength > policy.MaxBytes:
		e.Add("content_length", validate.CodeOutOfRange,
			"That photograph is %d bytes. Compress it to %d bytes or fewer.",
			u.ContentLength, policy.MaxBytes)
	}

	return e
}

// Upload is a place to put one object, and how long it may be put there.
//
// # It is a credential, and it is returned to exactly one caller
//
// URL is the whole of the authorisation to write that object: anybody holding it can, until
// ExpiresAt. It reaches only the awarded provider who asked for it — the idempotency middleware
// stores the response under `idem:v1:user:<provider>:<key>` (SHIP-44), so even a replay reaches
// nobody else. That is the same property [assignmentResponse] relies on for the driver's link, and
// it is worth stating twice because neither shape looks like a credential.
//
// ObjectKey is returned beside the URL because it is what the client tells the platform about
// afterwards. SHIP-115 records it against a job and a milestone; until then it is the only handle
// anybody has on the object, and it cannot be recovered from the URL without parsing one.
type Upload struct {
	ObjectKey string

	URL           string
	ContentType   string
	ContentLength int64

	ExpiresAt time.Time
}

// PresignProofUpload issues one short-lived URL the awarded provider may upload one photograph to
// (SHIP-114).
//
// # The refusals, in this order
//
//  1. a request the platform will not sign — no content type, an unaccepted one, no length, or one
//     over [UploadPolicy.MaxBytes] — is `validation_failed` with the field named;
//  2. a job nobody was awarded, or no job at all, is [ErrJobNotFound];
//  3. a job awarded to another provider is [ErrNotAwardedProvider].
//
// The last two are one 404 on the wire, for the reason [apiError] gives: a 403 would confirm that a
// competitor's job exists. Validation runs first, which is the order [Service.RecordMilestone] and
// [Service.AssignDriver] both use, and it discloses nothing — a client is told about its own body.
//
// # Who may upload against which job is decided here, and from the accepted bid
//
// The same check as every other write in this domain, and deliberately not "the caller carries
// role: provider". Being the provider on the job's accepted bid is a stronger statement than a
// claim in a token, and it is the one Docs/02 §3 actually makes. **The driver cannot reach this
// endpoint at all today**, and that is a gap rather than a decision — see Docs/11 §3 on SHIP-121
// and SHIP-122, which need a driver-token write that the idempotency scope does not yet support.
//
// # The object key is fresh every time, and that is the answer to what a retry does
//
// A repeated request carrying the same `Idempotency-Key` is replayed by SHIP-15's middleware and
// never reaches this function: the client gets the identical URL, the identical key and the
// identical expiry, already ticking down. **That is the correct answer.** One intent bought one
// upload slot, and a retry must not quietly extend a credential's life.
//
// Once that entry has expired from Redis, a retry reaches here and mints a *new* URL for a *new*
// object key. There is no database row making it otherwise, and there deliberately is not:
//
//   - Nothing durable was written the first time. A second URL leaves at most one unreferenced
//     object in a bucket, and SHIP-115 is what decides which key is the job's proof. Contrast
//     [Service.RecordMilestone], where the durable guarantee had to be a unique index because a
//     second row would be a second *recorded fact* about the delivery.
//   - **A key derived from the idempotency key was considered and rejected.** It would make the
//     retry return the same object key, which sounds better and is worse: a client holding an old
//     key could ask for a fresh URL over an object that already holds proof, and proof is evidence.
//     A key nothing can predict means an issued URL can only ever write an object that did not
//     exist when it was signed.
//
// r is a reader rather than a transaction. One statement, no write, nothing to keep consistent with
// anything else — this is the same call [Service.AssignmentFor] makes.
func (s *Service) PresignProofUpload(
	ctx context.Context,
	r db.Runner,
	providerID, jobID uuid.UUID,
	req UploadRequest,
) (Upload, error) {
	request := req.normalise()
	problems := request.problems(s.uploads.policy)
	if err := problems.Err(); err != nil {
		return Upload{}, err
	}

	awarded, isAwarded, err := s.awards.AwardedProvider(ctx, r, jobID)
	if err != nil {
		return Upload{}, err
	}
	if !isAwarded {
		return Upload{}, fmt.Errorf("delivery: %s has no accepted bid: %w", jobID, ErrJobNotFound)
	}
	if awarded != providerID {
		return Upload{}, fmt.Errorf("delivery: %s was awarded to %s, not %s: %w",
			jobID, awarded, providerID, ErrNotAwardedProvider)
	}

	key, err := proofObjectKey(jobID)
	if err != nil {
		return Upload{}, err
	}

	url, expiresAt, err := s.uploads.store.PresignUpload(
		ctx, key, request.ContentType, request.ContentLength, s.uploads.policy.URLTTL)
	if err != nil {
		// A failure of the signer rather than of the request: the domain has already checked
		// everything a client could get wrong, so this is a configuration or a wiring fault and
		// becomes an opaque 500 with its cause logged (SHIP-15i).
		return Upload{}, fmt.Errorf("delivery: signing an upload for %s on %s: %w", key, jobID, err)
	}

	return Upload{
		ObjectKey:     key,
		URL:           url,
		ContentType:   request.ContentType,
		ContentLength: request.ContentLength,
		ExpiresAt:     expiresAt,
	}, nil
}

// proofObjectKey is where one photograph goes: `proof/<job>/<uuid>`.
//
// # Prefixed by the job, so that a key is legible without a lookup
//
// Support reading a key out of a log, and SHIP-155's administrator reading one out of a moderation
// queue, both want to know which delivery it belongs to. The database is still authoritative about
// that from SHIP-115 — nothing here is (internal/platform/storage's doc.go says so) — but a prefix
// that agrees with the record costs nothing and a flat namespace of a million opaque names costs
// somebody an afternoon.
//
// # No file extension, deliberately
//
// An extension would have to be derived from the content type, and the map from one to the other is
// a **hard-coded list that would have to stay in step with STORAGE_ACCEPTED_CONTENT_TYPES** — which
// is configuration precisely because it changes. Two lists that must agree and only one of which can
// be changed by a deployment is the drift Docs/06 §5.3 is written about. The store already records
// the media type from the signed `Content-Type` header, which is the authoritative copy.
//
// UUIDv7 rather than v4 so keys sort by the time they were issued, which is what makes a lifecycle
// rule over unreferenced objects expressible later.
func proofObjectKey(jobID uuid.UUID) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("delivery: generating an object key for %s: %w", jobID, err)
	}
	return proofKeyPrefix + "/" + jobID.String() + "/" + id.String(), nil
}
