// Package email dispatches transactional email.
//
// Two implementations exist today, which is what earns this package its place in the
// adapter tree (Docs/06 §4.1):
//
//	console.go    development — renders the message to the log and sends nothing
//	provider.go   staging and production — hands it to the email provider
//
// Which one is constructed is chosen in cmd/api from SHIPPER_ENV. Nothing else in the
// service knows or asks which one it holds. SHIP-32.
//
// A development environment that silently sends real email to a real address is a
// mistake that only announces itself after it has happened, which is the whole reason the
// console implementation is the default rather than an opt-in.
//
// The interface this package satisfies is declared by the domain that sends the message —
// identity for verification email, notifications for event email — never here.
//
// # No vendor, deliberately
//
// No document names an email vendor, and Docs/06 §4.1's stated pattern is that the seam
// exists before the vendor does. [Provider] therefore speaks a generic HTTP contract —
// a JSON POST under a configured base URL, with a bearer credential — taking base URL, key
// and sender as its own [Options] rather than reading internal/config.
//
// The vendor is named at SHIP-33, the verification-email endpoint, which is the first
// ticket that sends anything real. Docs/11 §7 records that as decided before wave 1 rather
// than deferred by accident. Naming it should change provider.go and nothing else.
//
// # Why the method takes four strings and not a struct
//
// A message struct declared here would have to be named in the signature, so identity
// would have to import this package to declare its port — which is precisely the edge
// SHIP-11's lint fails the build over (Docs/06 §4.1, Docs/10 §2.3).
//
// So the whole signature is standard-library types:
//
//	Send(ctx context.Context, to, subject, body string) error
//
// which identity and notifications each declare for themselves, in their own ports.go,
// with no import in either direction. The two meet in cmd/api. A richer message — HTML
// alongside text, a reply-to, an attachment — arrives as a second method when a ticket
// needs one, not as a struct that would re-create the dependency.
package email
