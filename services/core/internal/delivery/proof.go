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
// # What this half deliberately does not do
//
// It writes nothing. Issuing a URL reserves no object and records no row — **an object in the
// bucket is not proof of anything until something says which job and which milestone it belongs
// to**, and until then it is bytes with a key. That is also the honest answer to what a retry
// returns; see [Service.PresignProofUpload].
//
// SHIP-115 is the other half of this file, below [proofObjectKey], and it is where the record and
// the access control live.

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

// UploadPolicy is what the platform will issue a pre-signed URL for, from configuration.
//
// # Every field is server-side, and Docs/06 §5.3 is why
//
// "Anything expected to change under operational pressure lives server-side… Flutter has no
// over-the-air update path for Dart code." All of these move: a size limit is raised the
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

	// UploadTTL is how long an issued upload URL works for.
	//
	// It was URLTTL and served both directions until SHIP-15r. The rename is what makes the split
	// below reviewable: a field called URLTTL beside a DownloadTTL reads as though one of them is
	// the general case, and the compiler cannot tell you which sites meant which.
	UploadTTL time.Duration

	// DownloadTTL is how long an issued download URL works for, and it is separate because the two
	// requirements are different and only one of them is generous (SHIP-15r).
	//
	// An upload link has to outlast a phone finishing a slow PUT on a bad connection. A download
	// link has to outlast an image rendering. Serving both from one number made every read link
	// live for as long as an upload needed — and a read link is an unrevocable link to a
	// photograph of somebody's front door, which is the exposure internal/config's Storage section
	// spends a paragraph on.
	DownloadTTL time.Duration
}

// valid reports whether this policy can issue anything at all.
//
// Checked at construction rather than per request: a service built with a zero policy would refuse
// every upload with a validation error naming the client's own perfectly good request, which is the
// worst place for a configuration failure to surface.
//
// Both lifetimes are required. A zero DownloadTTL would sign a download URL that has already
// expired, which is a broken image in a customer's tracking view rather than an error anybody sees
// — the quietest of the failures available here, and the reason it is refused at construction
// alongside the rest.
func (p UploadPolicy) valid() bool {
	return p.MaxBytes > 0 && len(p.AcceptedContentTypes) > 0 &&
		p.UploadTTL > 0 && p.DownloadTTL > 0
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
	problems := request.problems(s.proof.policy)
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

	url, expiresAt, err := s.proof.uploads.PresignUpload(
		ctx, key, request.ContentType, request.ContentLength, s.proof.policy.UploadTTL)
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

// Proof of delivery, part two: making an uploaded object evidence (SHIP-115).
//
// # The platform never saw the photograph, and this is the file where that has to be dealt with
//
// Part one hands out permission and writes nothing. The client PUTs the bytes to the object store
// with this service in neither direction, so at the end of SHIP-114 the platform holds **no record
// that anything was uploaded** and cannot obtain one by waiting: there is no callback, no
// notification and nothing to poll. Two failures follow directly from that, and they are not
// symmetric:
//
//   - **a row with no object.** The client says "I uploaded proof/<job>/<uuid>" and nothing did.
//     The platform would then hold a record asserting that a delivery was photographed, when it
//     has never looked. Docs/01 §4.4 makes proof "the *only* evidence that the job happened as
//     claimed" — a record that might point at nothing is not evidence, and a dispute (Docs/04 §7)
//     is where that would be discovered.
//   - **an object with no row.** A URL was issued and spent and the client never came back, or
//     came back into a request that failed. The bucket holds bytes nothing references.
//
// **The first is refused and the second is expected**, and the asymmetry is the decision this file
// exists to record. [Service.VerifyProof] asks the store what it holds before anything is written,
// so a `proofs` row always has an object behind it — checked, not asserted. An unreferenced object
// is proof of nothing, cannot be found by anybody (the bucket has no public read path and the keys
// are unguessable), and ages out under a lifecycle rule, which is what SHIP-114 chose UUIDv7 keys
// for. Making *that* direction impossible would mean recording something at the moment a URL is
// issued, and SHIP-114 argues at length why nothing is recorded then.
//
// # What is stored is what the store reported
//
// [VerifiedProof] carries the content type, the length and the entity tag the store answered with,
// not the ones the client asked for. Those are usually the same values — the upload URL signs both
// — and this is the copy that is still right when they are not, which is what lets [UploadPolicy]
// be enforced against the object that exists rather than only against the request that asked to
// create one.

// VerifiedProof is an object the platform has looked at, and it is the only thing that can be
// recorded as proof.
//
// # Its fields are unexported, and that is the mechanism rather than a style
//
// The zero value means "no proof", and it is the only value another package can construct: nothing
// outside `delivery` can set a field, so nothing outside `delivery` can hand [Service.RecordMilestone]
// a photograph the platform has not checked. This is the same shape [DriverGrant] uses — a value
// obtainable from one function and from nowhere else — and it is here for the same reason: a rule
// enforced by what a caller is *able* to build survives a refactor that a rule enforced by calling
// order does not.
type VerifiedProof struct {
	objectKey string

	contentType   string
	contentLength int64
	etag          string
}

// present reports whether this recording carries proof at all.
func (p VerifiedProof) present() bool { return p.objectKey != "" }

// complete reports whether every fact the store answered with is here.
//
// Checked before the insert rather than left to 000603's CHECK constraints, for the reason
// Docs/10 §4.6 gives about validation generally: a constraint name in a 500 explains nothing.
// Nothing can produce a partial value through an endpoint — the fields are unexported and
// [Service.VerifyProof] sets all four or returns an error — so this is the guard against a future
// caller *inside* this package assembling one and skipping the store.
func (p VerifiedProof) complete() bool {
	return p.objectKey != "" && p.contentType != "" && p.contentLength > 0 && p.etag != ""
}

// ObjectKey is where the photograph is, for a caller that needs to log or display it.
//
// A reader rather than an exported field, so the zero value stays the only one another package can
// make.
func (p VerifiedProof) ObjectKey() string { return p.objectKey }

// VerifyProof checks that an object exists, belongs to this job, and is something the platform will
// accept as evidence (SHIP-115).
//
// # It runs outside the transaction, deliberately
//
// This makes a network request to the object store, and a database transaction held open across a
// call to another service is a pool connection hostage to that service's worst day. So the handler
// calls this first, against the pool, and opens its transaction afterwards with the answer in hand.
// The awarded check is therefore made twice — once here and once inside [Service.RecordMilestone] —
// which is one indexed read, and the alternative is worse in both directions: skipping it here would
// let a stranger learn whether an object exists, and skipping it there would make this function's
// return value load-bearing for authorisation.
//
// # The refusals, in this order, and the order is the same one every other write in this domain uses
//
//  1. a key the platform did not issue for *this* job is [ErrProofNotForThisJob] — decided from the
//     string alone, with no lookup, so it discloses nothing but the caller's own body;
//  2. a job nobody was awarded, or no job at all, is [ErrJobNotFound];
//  3. a job awarded to another provider is [ErrNotAwardedProvider];
//  4. an object that is not there is [ErrProofNotUploaded];
//  5. an object outside [UploadPolicy] is [ErrProofRejected].
//
// (2) and (3) are one 404 on the wire, for the reason [apiError] gives. They come before the store
// is asked anything, which is what stops this endpoint being a way to probe whether a key exists.
//
// # Why the policy is checked again here
//
// It was checked before the URL was signed and signed into the URL, so an upload that changed either
// value is refused by the store on the request that carried the bytes. That guard is real and it is
// **entirely outside this package** — the domain's tests stub the signer, so no test here can fail
// when it stops working, which is what SHIP-114's mutation testing recorded as an open hole. Reading
// the stored values back and judging them closes it from the other end: whatever route an object
// took into the bucket, it is measured against the platform's current limits before it becomes
// evidence.
func (s *Service) VerifyProof(
	ctx context.Context,
	r db.Runner,
	providerID, jobID uuid.UUID,
	objectKey string,
) (VerifiedProof, error) {
	key := strings.TrimSpace(objectKey)
	if !keyBelongsToJob(key, jobID) {
		return VerifiedProof{}, fmt.Errorf("delivery: %q is not an object key %s issued: %w",
			key, jobID, ErrProofNotForThisJob)
	}

	awarded, isAwarded, err := s.awards.AwardedProvider(ctx, r, jobID)
	if err != nil {
		return VerifiedProof{}, err
	}
	if !isAwarded {
		return VerifiedProof{}, fmt.Errorf("delivery: %s has no accepted bid: %w", jobID, ErrJobNotFound)
	}
	if awarded != providerID {
		return VerifiedProof{}, fmt.Errorf("delivery: %s was awarded to %s, not %s: %w",
			jobID, awarded, providerID, ErrNotAwardedProvider)
	}

	contentType, contentLength, etag, found, err := s.proof.objects.Stored(ctx, key)
	if err != nil {
		// A failure of the store rather than an answer from it — see [ProofObjects.Stored]. It
		// becomes an opaque 500 with its cause logged (SHIP-15i), which is the right answer: the
		// client did nothing wrong and there is nothing it can usefully be told.
		return VerifiedProof{}, fmt.Errorf("delivery: asking the store about %s on %s: %w", key, jobID, err)
	}
	if !found {
		return VerifiedProof{}, fmt.Errorf("delivery: %s holds nothing on %s: %w",
			key, jobID, ErrProofNotUploaded)
	}

	// Normalised the way an upload request is, and for the same reason: the configured list is
	// lower case, media types are case-insensitive (RFC 9110 §8.3), and a store echoing back
	// `Image/JPEG` must not be a reason to refuse a perfectly good photograph. A parameter —
	// `image/jpeg; charset=binary` — is not trimmed off, because the platform signed a bare type
	// and an object carrying a parameter is not the object it authorised.
	stored := strings.ToLower(strings.TrimSpace(contentType))

	switch {
	case !slices.Contains(s.proof.policy.AcceptedContentTypes, stored):
		return VerifiedProof{}, fmt.Errorf("delivery: %s holds %q, which is not one of %s: %w",
			key, stored, strings.Join(s.proof.policy.AcceptedContentTypes, ", "), ErrProofRejected)

	case contentLength <= 0:
		return VerifiedProof{}, fmt.Errorf("delivery: %s holds %d bytes: %w",
			key, contentLength, ErrProofRejected)

	case contentLength > s.proof.policy.MaxBytes:
		return VerifiedProof{}, fmt.Errorf("delivery: %s holds %d bytes, over the %d-byte limit: %w",
			key, contentLength, s.proof.policy.MaxBytes, ErrProofRejected)
	}

	if strings.TrimSpace(etag) == "" {
		// Every S3-compatible store answers a HEAD with one, and a record without it could not
		// later show that the bytes had been replaced. Reported rather than stored empty:
		// ck_proofs_etag would refuse the row anyway, and a constraint name is a worse
		// explanation than this.
		return VerifiedProof{}, fmt.Errorf("delivery: %s exists and the store reported no entity tag: %w",
			key, ErrProofRejected)
	}

	return VerifiedProof{
		objectKey:     key,
		contentType:   stored,
		contentLength: contentLength,
		etag:          etag,
	}, nil
}

// keyBelongsToJob reports whether key is one [proofObjectKey] could have produced for jobID.
//
// # It is an authorisation check, not a tidiness check
//
// Every key is chosen by the platform and prefixed with the job the URL was issued against, so a
// key naming another job is a client attaching one delivery's photograph to another. The bucket is
// one namespace shared by every job and — from SHIP-155 — by verification documents, so without
// this the only thing standing between two jobs' evidence is that keys are hard to guess. They are
// hard to guess and easy to *pass on*: a provider awarded two jobs holds keys for both.
//
// The last segment is checked to be an identifier rather than merely non-empty, so that
// `proof/<job>/../<other-job>/<id>` cannot arrive by being correctly prefixed — storage's own
// validateObjectKey refuses a `..` segment as well, and one of the two being enough is not a reason
// for the other to trust it.
func keyBelongsToJob(key string, jobID uuid.UUID) bool {
	rest, ok := strings.CutPrefix(key, proofKeyPrefix+"/"+jobID.String()+"/")
	if !ok {
		return false
	}
	_, err := uuid.Parse(rest)
	return err == nil
}

// Proof of delivery, part three: the delivery that could not be photographed (SHIP-116).
//
// # The exception is not a hole in the rule. It is the other half of it
//
// Docs/01 §4.4 decides that photo proof is mandatory **and**, in the same paragraph, that "the
// exception path is part of the same feature and must be built with it, not after". The reason it
// gives is operational rather than generous: proof can be genuinely impossible — a recipient who
// objects to being photographed, a camera permission denied, a delivery point unlit or unsafe — and
// "what must never happen is a driver standing at a delivery point unable to finish the job — that
// converts a UI constraint into an operational failure and a support call".
//
// So an exception is **evidence, not the absence of it**: a reason, chosen by the actor from a
// closed list, recorded in the same transaction and the same table as the photographs. That is why
// [ProofExceptionReason] is a vocabulary rather than a free-text field and why 000604 widened
// `proofs` rather than adding a table beside it — "never both, never neither" is expressible as one
// CHECK on one row and expressible in no way at all across two tables.
//
// # An exception may stand behind any milestone, not only 'Delivered'
//
// The narrower rule was considered and is wrong in the direction that costs a driver something.
// Docs/01 §4.4's three reasons are about **capture** — a camera, a recipient, a place — and none of
// them knows which milestone is being recorded. A driver who cannot photograph a pickup is in
// exactly the position the paragraph describes, and refusing the exception there would leave them
// choosing between recording nothing and lying about what they have.
//
// What is specific to 'Delivered' is that evidence is *required* at all, and that is SHIP-118's
// rule about milestones rather than this file's rule about proof.
//
// # SHIP-117 reads this, and X-6 is deliberately not decided here
//
// An exception-completed job enters the moderation queue (Docs/04 §5's fourth: "failed proof of
// delivery"), and what that ticket needs from here is the ability to ask which jobs have one —
// `idx_proofs_exception` is that answer, and the flag itself is a fact about the job that belongs
// with the queue. X-6 is the open decision about whether such a job may auto-complete under
// Docs/02 §6.1 at all; **nothing here presumes either answer**, and SHIP-119 can branch on the row
// in whichever direction operations settles it.

// The three proof exception reasons are generated (SHIP-56a).
//
// contracts/statuses.yaml is the source and proofexception_gen.go beside this file is the Go form:
// ProofExceptionReason, its constants, ProofExceptionReasons, Valid, String, Wire and
// ProofExceptionReasonFromWire. Docs/01 §4.4's three clauses moved into the specification with the
// values they justify, which is where they can reach a driver's phone and a driver's browser as
// well as this package.
//
// The stored form is the wire form here, unlike the job and bid statuses. Nothing about that
// changed: it is written out per value in the specification, both columns the same, rather than
// being a case the generator knows about.

// proofExceptionWire is every accepted reason, for the message a client is refused with.
//
// Derived from [ProofExceptionReasons] rather than written out beside it, for the reason
// [recordableWire] is derived: a hand-written list in a message eventually offers a value the
// handler then rejects.
func proofExceptionWire() []string {
	wire := make([]string, 0, len(ProofExceptionReasons))
	for _, r := range ProofExceptionReasons {
		wire = append(wire, string(r))
	}
	return wire
}

// recordEvidence writes the row that stands behind a recorded claim, inside the caller's
// transaction.
//
// It is called from [Service.RecordMilestone] and from nowhere else, which is what keeps the
// evidence and the milestone one act: there is no path that attaches a photograph or a reason to a
// milestone recorded earlier, because a milestone committed without either is precisely the state
// SHIP-118 refuses and this domain therefore never produces.
//
// # It takes both and writes one, and the switch is the guard rather than a formality
//
// A recording carrying both a photograph and a reason is incoherent — Docs/01 §4.4's exception is
// "in place of" the photograph — and one carrying an unknown reason is a value the database would
// refuse with a constraint name. Both are caught by [Recording.problems] before this is reached,
// and both are checked again here for the reason [VerifiedProof.complete] is checked: this is the
// guard against a future caller **inside this package** assembling a [Recording] by hand.
// ck_proofs_photograph_or_exception is the third layer and the only one that survives a rewrite of
// the first two.
//
// **SHIP-115 called this `recordProof` and SHIP-116 renamed it.** With one kind of evidence the
// specific name was clearer; with two, "proof" would have been the name of the branch this
// function no longer always takes.
//
// r must be the transaction the milestone was written in. Not asserted here — [Service.RecordMilestone]
// checks it once at the top, and a second check would suggest this is reachable on its own.
func (s *Service) recordEvidence(
	ctx context.Context,
	r db.Runner,
	jobID, milestoneID uuid.UUID,
	proof VerifiedProof,
	exception ProofExceptionReason,
) (Proof, error) {
	switch {
	case proof.present() && exception != "":
		return Proof{}, fmt.Errorf("delivery: %s carries a photograph and the reason %q: %w",
			milestoneID, exception, ErrEvidenceNotCoherent)

	case exception != "":
		if !exception.Valid() {
			return Proof{}, fmt.Errorf("delivery: %q is not one of %s: %w",
				exception, strings.Join(proofExceptionWire(), ", "), ErrEvidenceNotCoherent)
		}

	case proof.present():
		if !proof.complete() {
			return Proof{}, fmt.Errorf("delivery: %q was not checked against the store: %w",
				proof.objectKey, ErrProofNotVerified)
		}

	default:
		return Proof{}, fmt.Errorf("delivery: %s has nothing behind it: %w",
			milestoneID, ErrEvidenceNotCoherent)
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Proof{}, fmt.Errorf("delivery: generating a proof id for %s: %w", jobID, err)
	}

	return s.store.insertProof(ctx, r, Proof{
		ID:          id,
		JobID:       jobID,
		MilestoneID: milestoneID,

		ObjectKey:     proof.objectKey,
		ContentType:   proof.contentType,
		ContentLength: proof.contentLength,
		ETag:          proof.etag,

		ExceptionReason: exception,
	})
}

// Proof is the evidence for one recorded claim: a photograph, or the reason there is none.
//
// # One type for both, because they answer one question
//
// SHIP-116 widened this from "one photograph" and did not add a second type beside it. A reader
// assembling a delivery's evidence — the customer's tracking screen (SHIP-133), an administrator in
// a dispute (Docs/04 §7) — is asking what stands behind each milestone, and "a photograph" and "a
// reason there is none" are two answers to that one question rather than two questions. The
// alternative was a second collection a client would have to merge and order itself, from two
// endpoints, against the same milestones.
//
// **[Proof.IsException] is how the two are told apart**, and the fields make it safe: 000604's
// ck_proofs_photograph_or_exception means a row carries all four photograph facts or a reason, and
// never a mixture, so a caller that checks one field has checked all of them.
//
// # It is attached to a milestone, and through the milestone to a job
//
// Docs/01 §4.4 numbers five things an actor records and requires a photograph of one of them, so
// proof is evidence *for a recorded claim* rather than a property of the delivery. The consequence
// worth knowing is what an absorbed milestone does with it: SHIP-112 keeps a late milestone's row
// and moves nothing, so **proof follows the milestone**, is kept with it, and appears on a
// timeline at the time the actor recorded it rather than at the time the phone found signal. Proof
// hung off the job would have had to answer "which one is current" from the arrival order, which is
// the one order Docs/02 §3.1 says not to show a customer.
//
// RecordedAt is the *milestone's* actor clock and AcceptedAt is when this row was written. There is
// no third timestamp and no actor: both are already on the milestone, in one row, written in the
// same transaction, and a second copy could only ever disagree with the first.
type Proof struct {
	ID          uuid.UUID
	JobID       uuid.UUID
	MilestoneID uuid.UUID

	Milestone Milestone

	// ObjectKey, ContentType, ContentLength and ETag are the photograph, and are empty together
	// when this row is a reasoned exception. The database refuses any mixture of the two states
	// (000604), so [Proof.IsException] reading one of them is reading all four.
	ObjectKey string

	ContentType   string
	ContentLength int64

	// ExceptionReason is why there is no photograph, and is empty when there is one (SHIP-116).
	//
	// It is what SHIP-117 queries to flag an exception-completed job into the moderation queue,
	// and what X-6 will be decided about — whether such a job may auto-complete under
	// Docs/02 §6.1. Neither is decided here.
	ExceptionReason ProofExceptionReason

	// ETag is the store's tag for the bytes at the moment they became proof.
	//
	// Not returned to any client and stored because it is unrecoverable later: a pre-signed PUT
	// stays usable until it expires, so a holder of the URL can overwrite the object inside that
	// window, and this is what makes such an overwrite detectable rather than silent. SHIP-155's
	// viewer is where an administrator would be shown a mismatch.
	ETag string

	RecordedAt time.Time
	AcceptedAt time.Time
}

// IsException reports whether this row is a reasoned exception rather than a photograph.
//
// One reader rather than a comparison at each call site, because the two states are total: 000604
// permits no row that is neither and no row that is both.
func (p Proof) IsException() bool { return p.ExceptionReason != "" }

// ProofLink is a [Proof] with somewhere to actually look at it, when there is anything to look at.
//
// The URL is issued **after** the authorisation check in [Service.ProofFor] and never before —
// internal/platform/storage will sign one for any key it is handed and says so, so the decision
// about who may see a delivery photograph lives here and only here.
//
// It is short-lived on the same reasoning as the upload URL: nothing can revoke a pre-signed URL, so
// its lifetime is the whole of the control. A rendered image URL that leaks out of a customer's
// browser history reaches a photograph of somebody's front door until it expires.
//
// **URL and ExpiresAt are empty on an exception, and nothing is signed for one** (SHIP-116). There
// is no object, so a URL would either name an empty key or name somebody else's — and the signer
// will sign whatever it is handed, which is exactly why the decision not to ask it lives here.
type ProofLink struct {
	Proof

	URL       string
	ExpiresAt time.Time
}

// ProofFor is the proof on one job, for a reader entitled to see it (SHIP-115).
//
// # Who may read a delivery's proof, and it is a shorter list than it looks
//
// Two parties, decided from the database rather than from a role claim:
//
//   - **the customer who owns the job.** Docs/01 §4.4's acceptance measure is theirs — "a customer
//     can see the latest delivery milestone and proof of delivery for their awarded job" — and
//     SHIP-133 is the screen.
//   - **the provider the job was awarded to**, on the same accepted bid every write in this domain
//     is checked against. They took the photograph; being unable to see what they submitted would
//     make a dispute about it unanswerable from their side.
//
// Both get the same thing, and that is a decision rather than an omission: there is no reduced view
// for one of them. A photograph is not a field that can be redacted, and a customer and a provider
// looking at the same delivery are arguing about the same object when they argue (Docs/04 §7).
//
// **The assigned driver is not on the list**, and the reason is not that they are untrusted. It is
// that nothing they could do with it exists yet: SHIP-122 is the ticket that has a driver capture
// proof at all, it is blocked on the idempotency scope (Docs/11 §3, SHIP-114), and a driver's link
// is forwardable through whatever channel the provider used and lives seven days. A proof
// photograph identifies an address and a recipient, which is the most sensitive thing a delivery
// produces, and widening a seven-day forwardable credential to reach one before any driver can even
// take one is a cost with no matching benefit. **SHIP-122 is where that gets revisited**, with a
// concrete need in front of it.
//
// **An administrator is not on the list either, and cannot be.** Docs/04 §6 and §7 both put proof in
// front of one, and there is no administrator: `ck_users_role` refuses 'admin' and admin sign-in is
// a separate system (SHIP-147). SHIP-155 is that reader, and what it needs from here is the
// download signing this function already uses plus the access log Docs/04 §6 requires.
//
// # A stranger and a job that does not exist get the same answer
//
// Both are [ErrJobNotFound] and one 404, which is the reading every other endpoint in this domain
// takes: a 403 would confirm that a job exists and that somebody is delivering it.
//
// # A customer whose job has no proof yet gets an empty list rather than a 404
//
// They are entitled to look, and "nothing has been photographed" is a true and useful answer.
// Distinguishing it from "no such job" tells them nothing they did not already know, because it is
// their job.
//
// r is a reader rather than a transaction: two statements, no writes, nothing to keep consistent.
func (s *Service) ProofFor(
	ctx context.Context,
	r db.Runner,
	readerID, jobID uuid.UUID,
) ([]ProofLink, error) {
	mayRead, err := s.mayReadProof(ctx, r, readerID, jobID)
	if err != nil {
		return nil, err
	}
	if !mayRead {
		return nil, fmt.Errorf("delivery: %s may not read the proof on %s: %w",
			readerID, jobID, ErrJobNotFound)
	}

	stored, err := s.store.proofOn(ctx, r, jobID)
	if err != nil {
		return nil, err
	}

	links := make([]ProofLink, 0, len(stored))
	for _, p := range stored {
		// A reasoned exception has no object, so nothing is asked of the signer (SHIP-116). The
		// store would sign a URL for the empty key without complaint — it authorises nothing by
		// itself and says so — and what came back would be a credential naming an object that
		// does not exist, handed to a client that would then render a broken image.
		if p.IsException() {
			links = append(links, ProofLink{Proof: p})
			continue
		}

		url, expiresAt, err := s.proof.objects.PresignDownload(ctx, p.ObjectKey, s.proof.policy.DownloadTTL)
		if err != nil {
			return nil, fmt.Errorf("delivery: signing a download for %s on %s: %w", p.ObjectKey, jobID, err)
		}
		links = append(links, ProofLink{Proof: p, URL: url, ExpiresAt: expiresAt})
	}
	return links, nil
}

// mayReadProof answers whether this account is one of the two parties to the delivery.
//
// The customer is asked first and the awarded provider second, which is an ordering rather than a
// preference: the two are never the same account today — `ck_users_role` fixes the role at
// registration and SHIP-45's trigger keeps it fixed — and cmd/api's jobPartiesLookup records the
// same ordering for the same reason.
//
// Both questions are asked of ports rather than of a role claim on the token. Being the customer on
// the job and the provider on its accepted bid are facts; `role: provider` is an assertion the
// platform issued about an account and says nothing about *this* delivery.
func (s *Service) mayReadProof(
	ctx context.Context,
	r db.Runner,
	readerID, jobID uuid.UUID,
) (bool, error) {
	isCustomer, err := s.owners.IsCustomer(ctx, r, jobID, readerID)
	if err != nil {
		return false, err
	}
	if isCustomer {
		return true, nil
	}

	awarded, isAwarded, err := s.awards.AwardedProvider(ctx, r, jobID)
	if err != nil {
		return false, err
	}
	return isAwarded && awarded == readerID, nil
}
