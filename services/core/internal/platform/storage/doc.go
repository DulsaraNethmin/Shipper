// Package storage holds proof-of-delivery photographs and provider verification
// documents in private object storage.
//
// One implementation (SHIP-114):
//
//	s3.go   every environment — an S3-compatible store, private, with pre-signed URLs
//
// # There is no local.go, and that is a decision SHIP-114 took rather than an omission
//
// This comment specified two implementations from SHIP-10 until SHIP-114: the local filesystem in
// development, S3 in staging and production. **That specification was written when development had
// no object store**, and SHIP-15p gave it one — a pinned MinIO in `make up` and the same image in
// CI. Docs/11 §3 records the reasoning; the short form is four points.
//
//   - The premise is gone. The implementation that runs in production is now the one exercised
//     locally, against genuine SigV4 semantics, which is the argument CLAUDE.md already makes for
//     testing against a real PostgreSQL rather than a mocked repository, and the argument SHIP-15p
//     made for choosing a real store over a mock. A mock that never checks a signature passes every
//     test this package could write and fails the first time a real bucket sees one.
//   - Docs/06 §4.1's test for an adapter is "does a second implementation exist today". Writing one
//     so that the answer becomes yes inverts the test. Email and SMS have two because console-versus-
//     provider is a real operational difference — development must not send mail to a person — and
//     storage has no equivalent, because writing a byte into a local bucket harms nobody.
//   - A filesystem implementation would have to sign its own URLs, which is a **second signing
//     scheme** to keep correct, plus a verifier inside the API, plus a route serving the bytes. That
//     last part contradicts the rule this package states most firmly, below: files do not pass
//     through this service. It would not be a second implementation of the same thing; it would be a
//     different architecture that only development ever ran — the drift that produces "it worked
//     locally".
//   - "A developer with no container running" is answered the way it already is for PostgreSQL and
//     Redis: `make up`. The tests fail rather than skip without the stack, deliberately.
//
// Docs/06 §4.1's adapter table still lists this row as "Local storage in development, S3 deployed".
// Correcting it is a shared-file edit SHIP-114's branch may not make, and the request is recorded in
// Docs/11 §3.
//
// # Files do not pass through this service
//
// A client asks for a short-lived pre-signed URL and uploads directly to object storage;
// the API never proxies the bytes (Docs/06 §5.2). A driver on a poor connection uploading
// a photograph through the API would occupy a request goroutine and a server timeout for
// the duration, and would put megabytes of image through a service sized for JSON.
//
// The consequence for this package is total: it makes no request to the object store, ever. There
// is no upload, no download, no listing and no delete — only signing. s3.go's header has what
// follows from that, including why the signature is written against the standard library rather
// than an SDK.
//
// The database keeps the metadata and the access controls (Docs/06 §4). Nothing here is
// authoritative about which job a file belongs to or who may see it — that is the delivery
// and profiles domains' answer, and this package only stores bytes.
//
// # Everything in here is private
//
// There is no public read path. Verification documents are private evidence and proof
// photographs identify an address and a recipient; both are reached only through a
// short-lived signed URL issued after an authorisation check (SHIP-115, SHIP-155).
//
// The interface this package satisfies is declared by the domain that needs a file stored
// — delivery for proof, profiles for verification documents — never here.
package storage
