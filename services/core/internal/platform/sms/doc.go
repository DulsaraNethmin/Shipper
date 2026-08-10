// Package sms dispatches text messages, which in the MVP means phone-verification OTPs.
//
// Two implementations exist today (Docs/06 §4.1):
//
//	console.go    development — renders the message to the log and sends nothing
//	provider.go   staging and production — hands it to the SMS provider
//
// Which one is constructed is chosen in cmd/api from SHIPPER_ENV. SHIP-35.
//
// SMS costs money per message and reaches a real handset, so the console implementation
// being the development default matters more here than it does for email: a retry loop
// against a live provider is a bill as well as a nuisance.
//
// The interface this package satisfies is declared by identity, which is the domain that
// needs an OTP delivered — never here.
//
// # No vendor, deliberately
//
// No document names an SMS vendor, and Docs/06 §4.1's stated pattern is that the seam
// exists before the vendor does. [Provider] therefore speaks a generic HTTP contract — a
// JSON POST under a configured base URL, with a bearer credential — taking base URL, key
// and sender as its own [Options] rather than reading internal/config.
//
// The vendor is named at SHIP-36, the phone-verification endpoint, which is the first
// ticket that sends anything real. Docs/11 §7 records that as decided before wave 1 rather
// than deferred by accident. Naming it should change provider.go and nothing else.
//
// # Why the method takes three strings and not a struct
//
// A message struct declared here would have to be named in the signature, so identity
// would have to import this package to declare its port — which is precisely the edge
// SHIP-11's lint fails the build over (Docs/06 §4.1, Docs/10 §2.3).
//
// So the whole signature is standard-library types:
//
//	Send(ctx context.Context, to, body string) error
//
// which identity declares for itself, in its own ports.go, with no import in either
// direction. The two meet in cmd/api.
//
// # This package does not know what an OTP is
//
// It sends a body somebody else composed. Generating the code, deciding its length and
// lifetime, rate-limiting attempts and comparing it on the way back are all identity's,
// at SHIP-34 and SHIP-36 — an adapter that knew it was carrying a one-time code would be
// a domain rule living in the transport.
package sms
