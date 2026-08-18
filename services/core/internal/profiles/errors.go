package profiles

import (
	"errors"

	"github.com/DulsaraNethmin/Shipper/services/core/internal/httpx"
)

// The sentinel errors this domain raises, and the error codes its endpoints answer with.
//
// Docs/10 §2.1 puts the sentinels here rather than beside the code that returns them, so a caller
// deciding what to do about a failure has one file to read. The callers written for are SHIP-153 and
// SHIP-154, which review and decide, and `cmd/api`, which maps these onto the wire.
var (
	// ErrNotProvider means the account is not a provider account.
	//
	// The same reading `fleet.ErrNotProvider` takes, and read from `users.role` rather than from the
	// role claim in the token: the claim is evidence about the token and the column is the fact.
	// SHIP-78a is where that stopped being true of only two methods.
	ErrNotProvider = errors.New("profiles: only a provider account has a verification record")

	// ErrNoSuchProvider means there is no verification record for that identifier.
	//
	// Distinct from [ErrNotProvider] in Go and one 404 on the wire. `000200` gives every provider a
	// record at registration, so reaching this means the account never existed or has been removed
	// underneath a live decision — and a reviewer being told "no such provider" rather than "not a
	// provider" is the difference between a stale queue and a wrong permission.
	ErrNoSuchProvider = errors.New("profiles: no such provider")

	// ErrAlreadyInState means a decision would not change anything.
	//
	// Refused rather than absorbed. A decision is an act by a person and Docs/04 §6.6 requires it be
	// recorded with a reason; a second one that changed nothing would be a row asserting a review
	// took place with no change to show for it. See [Service.Decide].
	ErrAlreadyInState = errors.New("profiles: the provider is already in that state")

	// ErrActorNotRecorded means the decision named no usable actor.
	//
	// Not a field error, because no client supplies the actor: it comes from the deciding
	// administrator's own credential in the composition root, so a bad one is a wiring fault rather
	// than a request somebody can correct. `ck_provider_verification_decisions_actor_id` is the
	// layer underneath.
	ErrActorNotRecorded = errors.New("profiles: a decision must record who took it")

	// ErrStateUnrecognised means a queue was asked for a state Docs/04 §4 does not have
	// (SHIP-153).
	//
	// Refused rather than answered with an empty page. An ignored filter answers "nobody", which
	// on a review queue is indistinguishable from "nobody is waiting" — and the cost of that
	// confusion is a provider who never gets looked at. `admin.Users.Search` takes the same
	// position about its standing filter for the same reason.
	ErrStateUnrecognised = errors.New("profiles: that is not a verification outcome")

	// ErrNotInTransaction means a decision was attempted outside a transaction.
	//
	// The database refuses it too — `provider_verification_decide` sets a transaction-local variable
	// the trigger reads, and outside a transaction it lasts only for the statement that set it — so
	// this is the legible half of a refusal that holds either way.
	ErrNotInTransaction = errors.New("profiles: a verification decision needs a transaction")

	// ErrDocumentNotForThisProvider means the object key was issued to somebody else (SHIP-81b).
	//
	// Decided from the string alone, with no lookup: every key is minted by the platform as
	// `verification/<provider>/<uuidv7>`, so a key naming another provider is one this caller was
	// never issued. It discloses nothing — the key names the other provider in plain text and the
	// caller is the one who sent it.
	//
	// **This is the guard that keeps two providers' evidence apart**, and the failure it prevents
	// passes every other assertion on the path: the URL is minted, the bytes upload, the row is
	// written, the kind is recorded and the document reads back through a fresh signed URL. All
	// that is wrong is whose verification record it landed in.
	ErrDocumentNotForThisProvider = errors.New("profiles: that object key was issued to another provider")

	// ErrDocumentNotUploaded means the store holds nothing under that key (SHIP-81b).
	//
	// It exists because the platform is not in the upload path (Docs/06 §5.2) and therefore cannot
	// know an upload happened until it asks. It is the one refusal on this path a provider can fix
	// without anybody's help: finish the PUT to the URL they already hold, or ask for another.
	ErrDocumentNotUploaded = errors.New("profiles: that document has not reached the store")

	// ErrDocumentRejected means the stored object is not something the platform accepts as
	// evidence (SHIP-81b).
	//
	// The object exists and is wrong — an unaccepted media type, no length, over the limit, or no
	// entity tag. A client cannot fix it by editing the request, so it is not a validation error;
	// it captures again.
	ErrDocumentRejected = errors.New("profiles: that object is not a document this platform accepts")

	// ErrDocumentAlreadyRecorded means the object is already somebody's evidence (SHIP-81b).
	//
	// `uq_provider_verification_documents_object_key` is the layer underneath, and it is access
	// control rather than tidiness: one object is evidence for at most one provider, so a single
	// photograph of a licence cannot verify two people.
	ErrDocumentAlreadyRecorded = errors.New("profiles: that object is already a recorded document")
)

// The error codes this domain's endpoints answer with.
//
// Registered rather than declared as constants, so that the generated Docs/10-api-error-codes.md
// describes them and `cmd/api`'s uniqueness test can see them. Named `<domain>_<condition>`, which is
// what stops two domains meaning different things by one string.
//
// **There is deliberately one.** Everything else here is covered by the protocol codes: a reason that
// is too long is `validation_failed` with details, a provider who does not exist is `not_found`, and
// a decision that changes nothing is `conflict`. A domain code earns its place only where a client
// would otherwise have to parse a message to know what to do.
var (
	// CodeProviderOnly is returned when an account that is not a provider reaches a verification
	// endpoint.
	//
	// A distinct code rather than a bare 403 for `fleet.CodeProviderOnly`'s reason: the app can act
	// on it by showing the customer surface. It is a *separate* registration from fleet's and not a
	// shared one, because two domains sharing a code is two domains that can no longer change their
	// own message — and `httpx.RegisterCode` refuses a duplicate string, which is what makes that a
	// decision rather than an accident.
	CodeProviderOnly = httpx.RegisterCode("profiles_provider_only",
		"Only a provider account has a verification record. Customers publish jobs; they are not "+
			"verified to bid.")

	// CodeDocumentNotUploaded is returned when the object a submission names is not in the store
	// (SHIP-81b).
	//
	// 409, and a code of its own because it is the one refusal on this path a client can fix
	// without a person: retry the PUT to the URL it already holds, or ask for a new one and PUT
	// again. Every other conflict here tells the provider to do something different; this tells
	// the app to finish what it started.
	//
	// It exists at all because the platform is not in the upload path (Docs/06 §5.2) and therefore
	// cannot know an upload happened until it asks. See [ErrDocumentNotUploaded].
	CodeDocumentNotUploaded = httpx.RegisterCode("profiles_document_not_uploaded",
		"That document is not in the store yet. Finish uploading it to the URL you were given, "+
			"then submit it again.")

	// CodeDocumentRejected is returned when the stored object is not something the platform
	// accepts as evidence (SHIP-81b).
	//
	// 409 rather than 422, and the difference is which thing was wrong: the request body is
	// perfectly well formed and names an object that exists — what fails the platform's rules is
	// the object. A client cannot fix it by editing the request, so it is not a validation error;
	// it photographs the document again.
	CodeDocumentRejected = httpx.RegisterCode("profiles_document_rejected",
		"That file is not a document this platform accepts. Photograph it again, and compress it "+
			"if it is large.")

	// CodeDocumentAlreadyRecorded is returned when the object is already a recorded document
	// (SHIP-81b).
	//
	// 409, and distinct from the two above because the app must not retry with the same key: it
	// asks for a fresh upload URL, which is a fresh object, which is what every request gets.
	CodeDocumentAlreadyRecorded = httpx.RegisterCode("profiles_document_already_recorded",
		"That image has already been submitted. Ask for a new upload URL and send it again.")
)
