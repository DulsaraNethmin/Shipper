package profiles

import (
	"context"
	"time"
)

// What this domain needs of the world outside it, declared by the consumer (Docs/06 §4.1,
// Docs/10 §2.3).
//
// `internal/platform/storage` is named nowhere in this package and this package is named nowhere in
// it — its own doc.go says so: "The interface this package satisfies is declared by the domain that
// needs a file stored — delivery for proof, profiles for verification documents — never here."
// `cmd/api` holds the implementation and is the only place the two meet.
//
// # Both signatures speak primitives, and that is forced rather than stylistic
//
// A port may not name a type declared in the package that implements it — that is the import the
// boundary lint refuses — so no struct from `internal/platform/storage` can appear below, in either
// direction. The answer is the one Docs/11 §9 records for `jobs.Geocoder` and `delivery.ProofUploads`
// alike: primitives in, primitives out, with this domain's own [DocumentUpload] and [Document]
// assembled from them one statement later.

// DocumentUploads is somewhere to put one verification document (SHIP-81b).
//
// # It signs a URL and does nothing else
//
// Docs/06 §5.2 and Docs/04 §3.1 both put the bytes outside this service entirely: the provider
// photographs their licence, the client is handed a short-lived pre-signed URL, and the image
// travels from the handset to the store with the API in neither direction. So this port has no Put
// and no Get — there is no method here that moves a byte, because no byte ever reaches the
// platform. What the implementation is asked for is permission, in the form of a URL, and permission
// is all it can give.
//
// # There is no Delete either, and SHIP-171a corrected the reason rather than the fact
//
// This paragraph read "no Put, no Get and no Delete" and gave one reason for all three. The reason
// is right for two of them and was never right for the third: **a delete moves no byte**, so
// "nothing here moves a byte" never explained its absence, and SHIP-171a proved the point by
// adding `Delete` to `internal/platform/storage` on exactly that argument.
//
// The fact is unchanged and the honest reason is a shorter one: **this domain has no act that
// removes a verification document.** Docs/04 §3 keeps the evidence trail — a decision is made
// against the documents that were reviewed, and a package that could delete one while rendering it
// is a capability nothing here needs. Removing them is SHIP-172's cascade, on Docs/05 §3.1's other
// half, and it is blocked on X-4's retention answer; when that arrives, whichever domain performs
// the act declares the port for it, and this file is where profiles' half would go. **An
// implementation existing is not a reason for a consumer to declare a method** — that is this
// package's own rule about who declares an interface, read in the direction it is usually read
// from.
//
// # What the four arguments are, and why the domain supplies every one of them
//
// The key, because which object a provider's licence goes to is this domain's question and not the
// store's — nothing in `internal/platform/storage` knows what a verification record is. The content
// type and the length, because they are what the implementation must *sign*: an upload URL that does
// not bind them authorises any body at all, and the platform's limits become a promise the client
// made to itself. And the lifetime, because "short-lived" is the whole of the authorisation —
// nothing can revoke a pre-signed URL once it is signed — and the number comes from configuration
// rather than from the signer.
//
// An implementation may refuse: a key that names another object, a content type carrying a newline,
// a length of zero. Those are failures of the mechanism rather than answers, so they come back as
// errors and become an opaque 500 — the domain has already checked everything a client could get
// wrong, so reaching one means this platform asked for something it should not have.
type DocumentUploads interface {
	// PresignUpload returns a URL the client may PUT exactly one object to, and when it stops
	// working.
	//
	// The expiry is returned rather than computed by the caller, so that what a client is told
	// and what the store will enforce come from one clock and one arithmetic.
	PresignUpload(
		ctx context.Context,
		key, contentType string,
		contentLength int64,
		ttl time.Duration,
	) (uploadURL string, expiresAt time.Time, err error)
}

// DocumentObjects is what the store already holds, and permission to read one back (SHIP-81b).
//
// # A second port rather than two more methods on [DocumentUploads], and the split is the question
//
// This is `internal/delivery`'s division copied deliberately rather than by habit, and its ports.go
// states the property it buys: "a domain reading proof cannot mint permission to write it."
// [DocumentUploads] answers "where may this provider put a photograph of their insurance
// certificate". This answers "what is actually there, and may this reader see it" — a different
// question, asked at a different moment, by a different caller. Both are satisfied by the same
// adapter and `cmd/api` joins them in one place, so the split costs a line there.
//
// **SHIP-155 is the second reader and it is an administrator**, on a credential this package never
// sees. When it arrives it will construct a [Documents] of its own from `cmd/api/routes_admin.go`
// and hand it the same pair; what it must not be able to do is issue an upload URL while rendering
// somebody's licence, and the reason it cannot is that the two capabilities are two interfaces.
//
// # Why [DocumentObjects.Stored] has to exist at all
//
// The platform is not in the upload path, so it never observes the upload: it cannot tell an upload
// that succeeded from one that failed halfway from one that was never attempted. A document record
// written from the client's say-so would be a row asserting that a licence had been submitted, held
// by a platform that has never looked — and Docs/04 §1 requires the verification record be an
// evidence trail. Asking the store closes it, and it closes something else too: what comes back is
// what was *stored*, so [DocumentPolicy]'s limits can be applied to the object that exists rather
// than only to the request that asked to create one.
type DocumentObjects interface {
	// Stored is the media type, size and entity tag the store holds under key, or found=false
	// when there is no such object.
	//
	// A missing object is an answer rather than an error: it is what a provider whose upload is
	// still going looks like, which on a handset in a depot is an ordinary afternoon. Anything
	// that is not an answer — a store refusing the credentials, a connection that never opens —
	// comes back as an error and becomes an opaque 500.
	Stored(ctx context.Context, key string) (
		contentType string, contentLength int64, etag string, found bool, err error)

	// PresignDownload returns a URL the caller may GET the object at, and when it stops working.
	//
	// **It authorises nothing by itself and checks nobody.** The implementation will sign a URL
	// for any key it is handed; deciding whether this reader may have it is [Documents.For]'s,
	// from the database, and it happens before this is called. That division is
	// `internal/platform/storage/doc.go`'s own rule, stated there as "the authorisation happens
	// in the domain, before the call".
	PresignDownload(ctx context.Context, key string, ttl time.Duration) (
		downloadURL string, expiresAt time.Time, err error)
}
