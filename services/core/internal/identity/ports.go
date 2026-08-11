// What this domain needs of the world outside it.
//
// Docs/10 §2.3 and Docs/06 §4.1: **the consumer declares the interface.** internal/platform/email
// knows nothing about identity, and identity imports nothing from it — the two are written
// independently, satisfied structurally, and meet in cmd/api. The import lint fails the build in
// both directions, so this is enforced rather than remembered.
//
// # Why every signature here is standard-library types
//
// A message struct declared in the adapter would have to be named in the signature below, and
// naming it means importing the adapter — which is exactly the edge the lint refuses. So the
// whole contract is context, strings and error. The adapters' own documentation makes the same
// point from the other side.
//
// Wave 1 surfaced the cost of this rule in geocoding, where it produced a five-value return and
// a comma-ok instead of a sentinel error. Docs/11 §9 records the open question — whether a
// neutral infrastructure package should hold shared value types — to be decided at SHIP-60.
// Nothing here is wide enough for it to matter yet.
//
// # What is deliberately absent
//
// There is no port for persistence. Docs/10 §2.2 is explicit: postgres.go is concrete and
// unexported, because PostgreSQL is not abstracted in this service and a repository interface
// would hide the constraints that make the rules true.
//
// There is no EventSink either, and that is a scope decision rather than an oversight.
// Docs/10 §6.1 has every domain writing events through one inside the transaction that changes
// state, but no ticket in SHIP-30…SHIP-36 specifies an event, and inventing `user.registered`
// here would fix a name that SHIP-136 and SHIP-137 have to honour. It arrives with the ticket
// that has a consumer for it.
//
// The blank line below keeps this a file note rather than a second package comment.

package identity

import "context"

// EmailSender delivers a transactional message.
//
// Satisfied by *email.Console in development and *email.Provider in staging and production
// (SHIP-32). Which one exists is decided in cmd/api from SHIPPER_ENV; nothing here knows or asks
// — and the console implementation logging the body in full is the whole reason a developer can
// complete verification without a mailbox.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}
