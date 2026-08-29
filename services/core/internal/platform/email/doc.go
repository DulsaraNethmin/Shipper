// Package email dispatches transactional email.
//
// Three implementations exist, which is what earns this package its place in the adapter
// tree (Docs/06 §4.1):
//
//	console.go    renders the message to the log and sends nothing
//	provider.go   posts it to a vendor's HTTP API
//	smtp.go       hands it to a mail server
//
// Which one is constructed is EMAIL_TRANSPORT — console, http or smtp — read in cmd/api and
// in cmd/notifier. Nothing else in the service knows or asks which one it holds (SHIP-32,
// SHIP-187a, SHIP-187b).
//
// # The environment used to decide, and no longer does
//
// It was UseConsole(env): the console in development, the provider everywhere else. That
// answered two cases and could not express a third — an instance hardened in every respect,
// running under every deployment-safety rule, deliberately sending its mail to a local
// catcher. SHIP-187a made the transport configuration; SHIP-187b added SMTP, which is what
// the third case needs and what every buyer already has.
//
// Unset, EMAIL_TRANSPORT still resolves from the environment: console in development, http
// elsewhere. So the safe default is unchanged and what is new is the ability to say
// otherwise. A development environment that silently sends real email to a real address is
// a mistake that only announces itself after it has happened, which is why the bias stayed
// exactly where it was.
//
// The rule and its default live in internal/config, in one place, and are tested there. No
// file in this package reads the environment.
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
// SHIP-33 was where a vendor was expected to be named and it closed without naming one,
// which cost nothing: the seam had existed since wave 1, so the wait was free. SHIP-187a
// then made the naming unnecessary. The three things vendors differ on — where the request
// goes, how the credential is presented, and what the body looks like — are all
// configuration, so following a buyer to their provider is an edit to deploy/.env and no Go
// change at all. See [Options] for the fields and for that argument in full.
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
