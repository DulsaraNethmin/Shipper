package profiles

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/clock"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/db"
	"github.com/DulsaraNethmin/Shipper/services/core/internal/validate"
)

// The evidence behind a verification record (SHIP-81b).
//
// Docs/04 §3 requires a provider's licence, vehicle registration, insurance certificate and ABN
// evidence be "collected and reviewed by an administrator", and §3.1 decides how they arrive:
// photographed in the app, compressed on the device, and "uploaded directly to private object
// storage through short-lived pre-signed URLs". SHIP-114 built that mechanism for proof of delivery
// and this is its second use — the *Done when* names it as the precedent, and everything below that
// looks copied from `internal/delivery/proof.go` is copied on purpose.
//
// # The platform never sees the image, and everything here follows from that
//
// What this file produces is **permission, not storage**. The client asks for a URL, the platform
// decides whether they may have one and what it authorises, and the bytes travel between the handset
// and the store with no third party in the path. The consequences are the ones SHIP-114 and SHIP-115
// recorded and they transfer without change:
//
//   - the URL *is* the authorisation, and its lifetime is the whole of the control. Nothing revokes
//     a pre-signed URL — there is no session to end and no row to delete — so the lifetime is
//     configuration, and the two things a URL is bound to are **signed** rather than merely checked
//     here. A limit this file enforces and the URL does not carry is a limit the client made up.
//   - issuing a URL writes nothing. An object in the bucket is bytes with a key until something
//     says whose verification record it belongs to, and that is [Documents.Submit].
//   - the record is written only after the store has been *asked* what it holds. A row written from
//     the client's say-so would assert that a licence had been submitted, held by a platform that
//     never looked, in the one table Docs/04 §1 requires be an evidence trail.
//
// # Why this is a second type rather than four methods on [Service]
//
// [Service] is constructed in two places — `cmd/api/routes_profiles.go` and
// `cmd/api/routes_admin.go` — and widening `NewService` would make every caller supply a signer,
// including the administrator's queue and decision endpoints, which have no business holding one.
// A separate type with its own constructor keeps the capability where it is used: a package that
// can move a verification state cannot thereby mint a URL into the evidence bucket.
//
// SHIP-155 composes with this rather than against it. The administrator's document viewer
// constructs its own [Documents] from `cmd/api/routes_admin.go`, hands it the same two ports, and
// reaches the reader below — it does not need a wider [Service].

// Kind is one of Docs/04 §3's four verification documents.
//
// The stored form is the wire form, as [State]'s is, and both are this domain's own spelling. Snake
// case rather than the document's prose, because these are identifiers a client sends and a
// `CHECK` holds — "ABN evidence" is a label and `abn_evidence` is a value.
type Kind string

// The four, in the order Docs/04 §3's table lists them.
const (
	// KindLicence is the provider's driver licence — §3's "Licence, registration, insurance:
	// Required. Document image collected and reviewed by an administrator."
	//
	// Spelled the Australian way, which is also what `make lint-spelling` requires: the noun is
	// `licence`, and the American form of it is on that checker's list precisely because this is
	// one of the words the product turns on.
	KindLicence Kind = "licence"

	// KindRegistration is the vehicle's registration.
	//
	// It is deliberately not tied to a `vehicles` row (SHIP-79). A provider photographs a
	// registration certificate before the platform necessarily knows which vehicle it is for, and
	// a foreign key here would make the order of two independent onboarding steps load-bearing.
	// Which vehicle a registration covers is the administrator's reading of the image, which is
	// what Docs/04 §3 means by manual review.
	KindRegistration Kind = "registration"

	// KindInsurance is the insurance certificate — the document §5's expiry queue will care about
	// most, and the one whose renewal cadence is X-4's to settle rather than this ticket's.
	KindInsurance Kind = "insurance"

	// KindABNEvidence is evidence of the provider's ABN — §3's "ABN where applicable: Required
	// for businesses".
	//
	// Evidence *of* an ABN rather than the number: a registration extract or a tax invoice showing
	// it. A number is a field somebody types and this table is for things somebody looks at.
	KindABNEvidence Kind = "abn_evidence"
)

// Kinds is every document kind, for validation and for the contract.
//
// **It is a second copy of `ck_provider_verification_documents_kind` and is held to it by test**,
// the discipline Docs/10 §3.4 requires of every enumeration living in two places and the
// arrangement [States] is already under.
// TestTheFourKindsAreTheConstraintsFourKinds reads the CHECK out of `pg_constraint` and holds this
// list to it in both directions.
var Kinds = []Kind{KindLicence, KindRegistration, KindInsurance, KindABNEvidence}

// Valid reports whether k is one of the four.
func (k Kind) Valid() bool {
	for _, known := range Kinds {
		if k == known {
			return true
		}
	}
	return false
}

func (k Kind) String() string { return string(k) }

// kindStrings is [Kinds] as the strings a validation message and the contract use.
func kindStrings() []string {
	out := make([]string, 0, len(Kinds))
	for _, k := range Kinds {
		out = append(out, string(k))
	}
	return out
}

// documentKeyPrefix is where every verification document lives in the bucket.
//
// One bucket prefixed by concern, which is what `internal/config`'s Storage.Bucket describes and
// what `internal/delivery`'s `proofKeyPrefix` comment predicted: "verification documents take a
// prefix of their own rather than a second bucket, because both kinds of object are private
// evidence under the same access rules."
const documentKeyPrefix = "verification"

// maxDocumentContentType bounds the media type a client may ask for.
//
// A bound against a runaway text field rather than a judgement, which is why it is a constant here
// and [DocumentPolicy] is not: the *list* of acceptable types moves under operational pressure and
// lives in configuration, while "a media type is not four kilobytes long" never will. It is the same
// 128 as `ck_provider_verification_documents_content_type`.
const maxDocumentContentType = 128

// DocumentPolicy is what the platform will issue a pre-signed URL for, from configuration.
//
// Every field is server-side, and Docs/06 §5.3 is why: "anything expected to change under
// operational pressure lives server-side… Flutter has no over-the-air update path for Dart code."
// All of these move. The concrete cost of getting one wrong is a provider who cannot submit their
// insurance certificate, which under Docs/04 §1 is a provider who cannot bid.
//
// It is deliberately the same four numbers `delivery.UploadPolicy` reads, from the same
// `STORAGE_*` variables, rather than a second set of `VERIFICATION_*` ones. Two lists of accepted
// media types is two answers to what this platform will store, and the one nobody updated is the
// one that refuses a perfectly good photograph after a handset's camera changes format.
type DocumentPolicy struct {
	// MaxBytes is the largest object an upload URL will be issued for.
	MaxBytes int64

	// AcceptedContentTypes is the closed set of media types, lower case.
	//
	// A list rather than an `image/` prefix match, so that `image/svg+xml` — a script container
	// browsers execute — cannot arrive by being an image.
	AcceptedContentTypes []string

	// UploadTTL is how long an issued upload URL works for.
	UploadTTL time.Duration

	// DownloadTTL is how long an issued download URL works for, and it is separate because the
	// two requirements are different and only one of them is generous (SHIP-15r).
	//
	// An upload link has to outlast a handset finishing a slow PUT. A download link has to outlast
	// an image rendering — and a read link here is an unrevocable link to a photograph of
	// somebody's driver licence, which is the most identifying object this platform holds.
	DownloadTTL time.Duration
}

// valid reports whether this policy can issue anything at all.
//
// Checked at construction rather than per request: a service built with a zero policy would refuse
// every upload with a validation error naming the client's own perfectly good request, which is the
// worst place for a configuration failure to surface.
func (p DocumentPolicy) valid() bool {
	return p.MaxBytes > 0 && len(p.AcceptedContentTypes) > 0 &&
		p.UploadTTL > 0 && p.DownloadTTL > 0
}

// DocumentUploadRequest is what a client says it is about to upload.
//
// # The length is stated, and it is not a formality
//
// A pre-signed PUT has exactly one bound available to it — the signed `Content-Length` — so the size
// a client declares here is the size the store will accept and no other. It is the client's own
// file, so it knows; and stating it is what turns [DocumentPolicy.MaxBytes] from a number in a
// document into something the object store enforces on the request that carries the bytes.
//
// **There is no field for the kind and none for the provider.** The provider is the token. The kind
// is not needed to sign anything — the object key does not encode it (see [documentObjectKey]) —
// and asking for it here would be a value the platform recorded nowhere and checked against nothing
// at the one moment it could not act on it. It is supplied when the document is submitted, which is
// the moment it is written down.
type DocumentUploadRequest struct {
	ContentType   string
	ContentLength int64
}

// normalise lower-cases the media type and trims it.
//
// Media types are case-insensitive (RFC 9110 §8.3), the configured list is lower case, and the value
// is *signed* — so a client sending `IMAGE/JPEG` must be told to send `image/jpeg`, which means the
// platform has to decide on one spelling before it signs.
func (u DocumentUploadRequest) normalise() DocumentUploadRequest {
	u.ContentType = strings.ToLower(strings.TrimSpace(u.ContentType))
	return u
}

// problems reports what is wrong with the request, in the error contract's shape.
//
// The message names the limit rather than saying "too large". Docs/04 §3.1 requires the client
// compress on the device, and it cannot choose a target it has not been told; the limit is
// configuration, so the refusal is where a client learns the current one.
func (u DocumentUploadRequest) problems(policy DocumentPolicy) validate.Errors {
	var e validate.Errors

	if e.Required("content_type", u.ContentType) {
		e.Length("content_type", u.ContentType, 1, maxDocumentContentType)

		// Named rather than left to validate.OneOf, whose message is "that is not one of the
		// available options". The set is configuration, so a client cannot have it compiled in
		// and the refusal is the only place it learns the current one.
		if !slices.Contains(policy.AcceptedContentTypes, u.ContentType) {
			e.Add("content_type", validate.CodeNotAllowed,
				"That is not a format this platform accepts. Upload one of %s.",
				strings.Join(policy.AcceptedContentTypes, ", "))
		}
	}

	switch {
	case u.ContentLength <= 0:
		e.Add("content_length", validate.CodeRequired,
			"Send the exact size of the image in bytes.")
	case u.ContentLength > policy.MaxBytes:
		e.Add("content_length", validate.CodeOutOfRange,
			"That image is %d bytes. Compress it to %d bytes or fewer.",
			u.ContentLength, policy.MaxBytes)
	}

	return e
}

// DocumentUpload is a place to put one document image, and how long it may be put there.
//
// # It is a credential, and it is returned to exactly one caller
//
// URL is the whole of the authorisation to write that object: anybody holding it can, until
// ExpiresAt. It reaches only the provider who asked for it — the idempotency middleware stores the
// response under `idem:v1:user:<provider>:<key>` (SHIP-44), so even a replay reaches nobody else.
//
// ObjectKey is returned beside the URL because it is what the client tells the platform about
// afterwards, in [Documents.Submit]. Until then it is the only handle anybody has on the object, and
// it cannot be recovered from the URL without parsing one.
type DocumentUpload struct {
	ObjectKey string

	URL           string
	ContentType   string
	ContentLength int64

	ExpiresAt time.Time
}

// Document is one submitted piece of evidence, as `provider_verification_documents` holds it.
//
// ObjectKey is on the struct and is **not** on the wire: it is a handle into the private bucket, a
// client has no use for one it did not just receive, and the reader below answers with a fresh
// signed URL instead. See the response shape in http.go.
type Document struct {
	ID uuid.UUID

	// ProviderID is the verification record this document belongs to.
	ProviderID uuid.UUID

	Kind Kind

	ObjectKey string

	// ContentType, ContentLength and ETag are what the object store reported when the platform
	// asked, after the upload — not what the client said it would send.
	ContentType   string
	ContentLength int64
	ETag          string

	SubmittedAt time.Time

	// ExpiresAt is when this document lapses, as stated at submission (SHIP-159).
	//
	// A pointer, because nil is a distinct and ordinary answer: **the platform was never told**,
	// which is not the same as "does not expire" and is not the same as an expiry in the past.
	// A document with none never appears on Docs/04 §5's expiry queue under any configuration.
	//
	// **It can only be written at INSERT.** `provider_verification_documents_no_update` refuses
	// every UPDATE, so a provider whose renewal date was mistyped re-photographs the document —
	// which is a second row, which is what this table already says a correction is.
	ExpiresAt *time.Time
}

// DocumentLink is a document and a fresh, short-lived URL to read it at.
//
// # The URL is minted per read and is never stored
//
// The *Done when* requires a document be "reachable only by a fresh signed URL", and this type is
// where that word is honoured: nothing in `provider_verification_documents` holds a URL, so there is
// no column anybody could read one out of, and every reader pays for its own signature with its own
// expiry. A stored URL would be a credential at rest with a lifetime nobody was watching.
type DocumentLink struct {
	Document

	URL string

	// URLExpiresAt is when the signed link stops working — minutes, not years.
	//
	// **Named for the URL rather than for the document, and the rename is not cosmetic.** It was
	// `ExpiresAt` until SHIP-159 put an `ExpiresAt` on [Document]; embedding then made one shadow
	// the other, and the two are a credential's lifetime and a licence's renewal date. A caller
	// reading the shadowed field would have shown a provider their insurance expiring five
	// minutes from now. The compiler caught it once; the name is what stops it arriving again.
	URLExpiresAt time.Time
}

// Documents is the evidence half of this domain (SHIP-81b).
//
// It owns no connection, for the reason [Service] does not: the transaction belongs to whoever owns
// the invariant being protected (Docs/10 §3.2).
type Documents struct {
	clock   clock.Clock
	store   postgresStore
	uploads DocumentUploads
	objects DocumentObjects
	policy  DocumentPolicy
}

// NewDocuments builds the evidence service.
//
// It panics on a missing collaborator, in the same spirit as [NewService]: this is called once from
// the composition root, and a signer that is not there will still not be there after a restart. A
// service that came up unable to sign would answer every submission with a 500 while reporting
// itself healthy.
func NewDocuments(
	c clock.Clock,
	uploads DocumentUploads,
	objects DocumentObjects,
	policy DocumentPolicy,
) *Documents {

	switch {
	case c == nil:
		panic("profiles: NewDocuments needs a clock (Docs/10 §6.3)")
	case uploads == nil:
		panic("profiles: NewDocuments needs somewhere to put a document")
	case objects == nil:
		panic("profiles: NewDocuments needs to be able to ask the store what it holds")
	case !policy.valid():
		panic("profiles: NewDocuments needs a usable document policy — " +
			"a size limit, at least one accepted media type, and both lifetimes")
	}

	return &Documents{clock: c, uploads: uploads, objects: objects, policy: policy}
}

// PresignUpload issues one short-lived URL a provider may upload one document image to (SHIP-81b).
//
// # The refusals, in this order
//
//  1. a request the platform will not sign — no content type, an unaccepted one, no length, or one
//     over [DocumentPolicy.MaxBytes] — is `validation_failed` with the field named;
//  2. a caller who is not a provider account is [ErrNotProvider].
//
// Validation runs first, which is the order [Service.Decide] uses and which discloses nothing — a
// client is told about its own body.
//
// # There is no parameter for whose record this is
//
// The provider is whoever the token says is calling, so there is nothing here to widen and no filter
// to forget — the same arrangement [Service.VerificationFor] takes, and stronger than a check would
// be. `users.role` is read rather than the token's role claim, which is SHIP-78a's distinction: the
// claim is evidence about the token and the column is the fact.
//
// # The object key is fresh every time, and that is the answer to what a retry does
//
// A repeated request carrying the same `Idempotency-Key` is replayed by SHIP-15's middleware and
// never reaches this function: the client gets the identical URL, key and expiry, already ticking
// down. That is correct — one intent bought one upload slot, and a retry must not quietly extend a
// credential's life. Once the entry has expired from Redis a retry mints a *new* URL for a *new*
// key, and **a key derived from the idempotency key was considered and rejected** for SHIP-114's
// reason: it would let a client holding an old key ask for a fresh URL over an object that already
// holds evidence somebody has reviewed.
//
// r is a reader rather than a transaction. One statement, no write, nothing to keep consistent with
// anything else.
func (d *Documents) PresignUpload(
	ctx context.Context,
	r db.Runner,
	providerID uuid.UUID,
	req DocumentUploadRequest,
) (DocumentUpload, error) {

	request := req.normalise()
	problems := request.problems(d.policy)
	if err := problems.Err(); err != nil {
		return DocumentUpload{}, err
	}

	if err := mustBeProvider(ctx, d.store, r, providerID); err != nil {
		return DocumentUpload{}, err
	}

	key, err := documentObjectKey(providerID)
	if err != nil {
		return DocumentUpload{}, err
	}

	url, expiresAt, err := d.uploads.PresignUpload(
		ctx, key, request.ContentType, request.ContentLength, d.policy.UploadTTL)
	if err != nil {
		// A failure of the signer rather than of the request: the domain has already checked
		// everything a client could get wrong, so this is a configuration or a wiring fault and
		// becomes an opaque 500 with its cause logged (SHIP-15i).
		return DocumentUpload{}, fmt.Errorf("profiles: signing an upload for %s on %s: %w",
			key, providerID, err)
	}

	return DocumentUpload{
		ObjectKey:     key,
		URL:           url,
		ContentType:   request.ContentType,
		ContentLength: request.ContentLength,
		ExpiresAt:     expiresAt,
	}, nil
}

// documentObjectKey is where one document image goes: `verification/<provider>/<uuidv7>`.
//
// # Prefixed by the provider, so that a key is legible without a lookup
//
// Support reading a key out of a log, and SHIP-155's administrator reading one out of a review
// queue, both want to know whose document it is. The database is still authoritative about that —
// `internal/platform/storage`'s doc.go says nothing there is — but a prefix that agrees with the
// record costs nothing and a flat namespace of opaque names costs somebody an afternoon.
//
// **The prefix is also what [keyBelongsToProvider] checks**, which is the difference between a
// convenience and a control: because the platform mints every key and every key names its owner, a
// key naming a different provider is a key this provider was never issued.
//
// # No file extension, and no kind, deliberately
//
// An extension would have to be derived from the content type, and the map from one to the other is
// a hard-coded list that would have to stay in step with `STORAGE_ACCEPTED_CONTENT_TYPES` — which is
// configuration precisely because it changes. Two lists that must agree and only one of which a
// deployment can change is the drift Docs/06 §5.3 is written about; the store already records the
// media type from the signed `Content-Type` header, which is the authoritative copy.
//
// The kind is absent for a stronger reason: it is not known when the URL is issued and it is not
// this key's to assert. A key carrying `insurance` and a row carrying `licence` would be two
// answers to what an administrator is looking at, and the row is the one Docs/04 §1 requires.
//
// UUIDv7 rather than v4 so keys sort by the time they were issued, which is what makes a lifecycle
// rule over unreferenced objects expressible later.
func documentObjectKey(providerID uuid.UUID) (string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return "", fmt.Errorf("profiles: generating an object key for %s: %w", providerID, err)
	}
	return documentKeyPrefix + "/" + providerID.String() + "/" + id.String(), nil
}

// keyBelongsToProvider reports whether key is one [documentObjectKey] could have produced for
// providerID.
//
// # It is an authorisation check, not a tidiness check
//
// Every key is chosen by the platform and prefixed with the provider the URL was issued to, so a key
// naming another provider is a client attaching one person's evidence to another's verification
// record. The bucket is one namespace shared by every provider and by every job's proof, so without
// this the only thing standing between two providers' identity documents is that keys are hard to
// guess — and they are hard to guess and easy to *pass on*.
//
// **What it protects is separation rather than secrecy**, and the failure it prevents is worth
// naming because it passes every happy-path assertion: the URL is minted, the bytes upload, the row
// is written, the kind is recorded, and the document reads back through a fresh signed URL. All that
// is wrong is whose file it landed in. `internal/delivery`'s `keyBelongsToJob` is the same guard
// against the same shape of mistake, and this is its twin rather than a variation on it.
//
// The last segment is checked to be an identifier rather than merely non-empty, so that
// `verification/<provider>/../<other>/<id>` cannot arrive by being correctly prefixed — storage's
// own `validateObjectKey` refuses a `..` segment as well, and one of the two being enough is not a
// reason for the other to trust it.
func keyBelongsToProvider(key string, providerID uuid.UUID) bool {
	rest, ok := strings.CutPrefix(key, documentKeyPrefix+"/"+providerID.String()+"/")
	if !ok {
		return false
	}
	_, err := uuid.Parse(rest)
	return err == nil
}

// Submit records that an uploaded object is one of this provider's four documents (SHIP-81b).
//
// # It asks the store before it writes anything, and that is the whole of the difficulty
//
// The platform is not in the upload path, so at the end of [Documents.PresignUpload] it holds **no
// record that anything was uploaded** and cannot obtain one by waiting: there is no callback, no
// notification and nothing to poll. Two failures follow and they are not symmetric.
//
//   - **a row with no object.** The client says "I uploaded verification/<provider>/<uuid>" and
//     nothing did. The platform would hold a record asserting that a licence was submitted, when it
//     has never looked — in the one table Docs/04 §1 requires be an evidence trail, and the place a
//     reviewer would later be asked what was checked.
//   - **an object with no row.** A URL was issued and spent and the client never came back. The
//     bucket holds bytes nothing references.
//
// **The first is refused and the second is expected.** [DocumentObjects.Stored] is asked before
// anything is written, so a row always has an object behind it — checked, not asserted. An
// unreferenced object is evidence of nothing, cannot be found by anybody, and ages out under a
// lifecycle rule, which is what UUIDv7 keys were chosen for.
//
// # The refusals, in this order
//
//  1. a kind Docs/04 §3 does not have is `validation_failed` on `kind`;
//  2. a key the platform did not issue to *this* provider is [ErrDocumentNotForThisProvider] —
//     decided from the string alone, with no lookup, so it discloses nothing but the caller's own
//     body;
//  3. a caller who is not a provider account is [ErrNotProvider];
//  4. an object that is not there is [ErrDocumentNotUploaded];
//  5. an object outside [DocumentPolicy] is [ErrDocumentRejected];
//  6. an object already recorded, for anybody, is [ErrDocumentAlreadyRecorded].
//
// (2) comes before (3) so that the store is never asked about a key on behalf of a caller whose
// standing has not been established.
//
// # Why the policy is checked again here
//
// It was checked before the URL was signed and signed into the URL, so an upload that changed either
// value is refused by the store on the request that carried the bytes. That guard is real and it is
// entirely outside this package — this domain's tests stub the signer, so no test here can fail when
// it stops working. Reading the stored values back and judging them closes it from the other end:
// whatever route an object took into the bucket, it is measured against the platform's current
// limits before it becomes evidence.
//
// # A second submission of the same kind is an ordinary event and writes a second row
//
// Docs/04 §4 puts a provider in `Restricted` "pending clarification or **document renewal**", and
// §3 has an administrator refusing an image that is illegible or visibly expired. So a retake is
// expected, and `000201` is append-only with the newest row of a kind current — the replaced image
// is what somebody already reviewed, and overwriting it would remove the evidence that the first
// submission was ever made.
//
// r is the pool rather than a transaction, deliberately. This makes a network request to the object
// store, and a database transaction held open across a call to another service is a pool connection
// hostage to that service's worst day. There is one write and it is one statement, so there is
// nothing a transaction would make atomic that is not already.
func (d *Documents) Submit(
	ctx context.Context,
	r db.Runner,
	providerID uuid.UUID,
	kind Kind,
	objectKey string,
	expiresAt *time.Time,
) (Document, error) {

	var problems validate.Errors
	if !kind.Valid() {
		problems.Add("kind", validate.CodeNotAllowed,
			"That is not a document this platform collects. Send one of %s.",
			strings.Join(kindStrings(), ", "))
	}
	key := strings.TrimSpace(objectKey)
	if problems.Required("object_key", key) {
		problems.Length("object_key", key, 1, maxDocumentObjectKey)
	}
	checkExpiry(&problems, expiresAt)
	if err := problems.Err(); err != nil {
		return Document{}, err
	}

	if !keyBelongsToProvider(key, providerID) {
		return Document{}, fmt.Errorf("profiles: %q is not an object key %s was issued: %w",
			key, providerID, ErrDocumentNotForThisProvider)
	}

	if err := mustBeProvider(ctx, d.store, r, providerID); err != nil {
		return Document{}, err
	}

	stored, err := d.stored(ctx, providerID, key)
	if err != nil {
		return Document{}, err
	}

	id, err := uuid.NewV7()
	if err != nil {
		return Document{}, fmt.Errorf("profiles: generating a document id for %s: %w", providerID, err)
	}

	document := Document{
		ID:            id,
		ProviderID:    providerID,
		Kind:          kind,
		ObjectKey:     key,
		ContentType:   stored.contentType,
		ContentLength: stored.contentLength,
		ETag:          stored.etag,
		ExpiresAt:     expiresAt,
	}

	submitted, err := d.store.recordDocument(ctx, r, document)
	if err != nil {
		return Document{}, err
	}
	return submitted, nil
}

// checkExpiry adds a field error when the stated expiry is not a date at all (SHIP-159).
//
// # It bounds what a timestamp *is*, and deliberately not how long a document may last
//
// Docs/04 §3 gives the renewal cadence to legal and insurance advisers (Track-X row X-4) and it is
// unanswered, so a rule like "no more than five years out" would be the platform enforcing a number
// nobody decided — exactly what `000201` and `000202` both decline to write. **A date in the past is
// accepted for the same reason it has no CHECK in the schema**: Docs/04 §3 has an administrator
// refusing an image that is "visibly expired", which means the platform has to be able to hold one
// long enough for somebody to look at it.
//
// What is refused is a value PostgreSQL cannot store, and the two ways a client produces one. The
// zero time is what `{"expires_at": null}` decodes to if a caller sends the literal
// `0001-01-01T00:00:00Z`, and it would otherwise be recorded as a document that lapsed two thousand
// years ago. The year bound is the encoder's rather than a policy's: `timestamptz` runs to 294276 AD
// and Go's `time.Time` runs a great deal further, so an absurd value would arrive as an opaque 500
// rather than as a field a client can correct. It is the same kind of bound as
// [maxDocumentContentType] — "a media type is not four kilobytes long" — and not the same kind as
// [DocumentPolicy].
func checkExpiry(problems *validate.Errors, expiresAt *time.Time) {
	if expiresAt == nil {
		return
	}
	if expiresAt.IsZero() || expiresAt.Year() < 1 || expiresAt.Year() > 9999 {
		problems.Add("expires_at", validate.CodeInvalid,
			"Send the date this document expires, or leave it out if it does not expire.")
	}
}

// maxDocumentObjectKey is the same 1024 as `ck_provider_verification_documents_object_key`, and the
// same bound `storage.validateObjectKey` applies.
//
// Bounded here so that a client sending a megabyte of text in `object_key` is answered with a field
// error rather than with a constraint name (Docs/10 §4.6). The prefix check below it is what
// actually decides whether the key is this provider's; this is what stops the platform doing string
// work on something absurd first.
const maxDocumentObjectKey = 1024

// storedDocument is what the object store reported, judged against [DocumentPolicy].
//
// Unexported and returned by value, because nothing outside this package may construct one: it is
// the platform's own observation of the bucket, and a caller that could build one could write a
// record about an object nobody looked at.
type storedDocument struct {
	contentType   string
	contentLength int64
	etag          string
}

// stored is what the store holds under key, judged against [DocumentPolicy].
//
// It authorises nobody. The caller has already decided that this provider may attach this key,
// which is why it takes an identifier for the message rather than for a decision.
func (d *Documents) stored(ctx context.Context, providerID uuid.UUID, key string) (storedDocument, error) {
	contentType, contentLength, etag, found, err := d.objects.Stored(ctx, key)
	if err != nil {
		// A failure of the store rather than an answer from it — see [DocumentObjects.Stored]. It
		// becomes an opaque 500 with its cause logged (SHIP-15i), which is the right answer: the
		// client did nothing wrong and there is nothing it can usefully be told.
		return storedDocument{}, fmt.Errorf("profiles: asking the store about %s for %s: %w",
			key, providerID, err)
	}
	if !found {
		return storedDocument{}, fmt.Errorf("profiles: %s holds nothing: %w", key, ErrDocumentNotUploaded)
	}

	// Normalised the way an upload request is, and for the same reason: the configured list is
	// lower case, media types are case-insensitive (RFC 9110 §8.3), and a store echoing back
	// `Image/JPEG` must not be a reason to refuse a perfectly good photograph. A parameter —
	// `image/jpeg; charset=binary` — is not trimmed off, because the platform signed a bare type
	// and an object carrying a parameter is not the object it authorised.
	mediaType := strings.ToLower(strings.TrimSpace(contentType))

	switch {
	case !slices.Contains(d.policy.AcceptedContentTypes, mediaType):
		return storedDocument{}, fmt.Errorf("profiles: %s holds %q, which is not one of %s: %w",
			key, mediaType, strings.Join(d.policy.AcceptedContentTypes, ", "), ErrDocumentRejected)

	case contentLength <= 0:
		return storedDocument{}, fmt.Errorf("profiles: %s holds %d bytes: %w",
			key, contentLength, ErrDocumentRejected)

	case contentLength > d.policy.MaxBytes:
		return storedDocument{}, fmt.Errorf("profiles: %s holds %d bytes, over the %d-byte limit: %w",
			key, contentLength, d.policy.MaxBytes, ErrDocumentRejected)
	}

	if strings.TrimSpace(etag) == "" {
		// Every S3-compatible store answers a HEAD with one, and a record without it could not
		// later show that the bytes had been replaced. Reported rather than stored empty:
		// ck_provider_verification_documents_etag would refuse the row anyway, and a constraint
		// name is a worse explanation than this.
		return storedDocument{}, fmt.Errorf(
			"profiles: %s exists and the store reported no entity tag: %w", key, ErrDocumentRejected)
	}

	return storedDocument{contentType: mediaType, contentLength: contentLength, etag: etag}, nil
}

// For is every document this provider has submitted, each with a fresh signed URL (SHIP-81b).
//
// # There is no parameter for whose, and that is the access control
//
// The provider is the token. Another provider's evidence is not refused here, it is never selected —
// the same arrangement [Service.VerificationFor] takes and the reason this endpoint has no
// identifier in its path.
//
// # Every URL is minted on this request and none is stored
//
// The *Done when* says a document is "reachable only by a fresh signed URL", and this is where that
// holds: `provider_verification_documents` has no URL column, so there is nothing to read one out
// of, and a link handed to a client stops working when its own window closes. The signer will sign
// for any key it is handed and checks nobody — `internal/platform/storage`'s doc.go says so — which
// is exactly why the caller has been established before this point and not after.
//
// Newest first within each kind, which is the order `idx_provider_verification_documents_provider`
// gives and the order both readers want: the current licence is the first row of its kind, and the
// ones behind it are what was reviewed before.
//
// r is a reader rather than a transaction: one statement, no writes, nothing to keep consistent.
func (d *Documents) For(ctx context.Context, r db.Runner, providerID uuid.UUID) ([]DocumentLink, error) {
	if err := mustBeProvider(ctx, d.store, r, providerID); err != nil {
		return nil, err
	}

	stored, err := d.store.documentsFor(ctx, r, providerID)
	if err != nil {
		return nil, err
	}

	links := make([]DocumentLink, 0, len(stored))
	for _, document := range stored {
		url, expiresAt, err := d.objects.PresignDownload(ctx, document.ObjectKey, d.policy.DownloadTTL)
		if err != nil {
			return nil, fmt.Errorf("profiles: signing a download for %s: %w", document.ObjectKey, err)
		}
		links = append(links, DocumentLink{Document: document, URL: url, URLExpiresAt: expiresAt})
	}
	return links, nil
}
