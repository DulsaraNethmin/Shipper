// Package storage holds proof-of-delivery photographs and provider verification
// documents in private object storage.
//
// Two implementations exist today (Docs/06 §4.1):
//
//	local.go   development — the local filesystem, with pre-signed URLs it signs itself
//	s3.go      staging and production — S3, private, with pre-signed URLs
//
// SHIP-114.
//
// # Files do not pass through this service
//
// A client asks for a short-lived pre-signed URL and uploads directly to object storage;
// the API never proxies the bytes (Docs/06 §5.2). A driver on a poor connection uploading
// a photograph through the API would occupy a request goroutine and a server timeout for
// the duration, and would put megabytes of image through a service sized for JSON.
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
