// Package sms dispatches text messages, which in the MVP means phone-verification OTPs.
//
// Two implementations, which is the whole set rather than a snapshot — there is no third
// transport an SMS gateway could want (Docs/06 §4.1):
//
//	console.go    renders the message to the log and sends nothing
//	provider.go   posts it to a gateway's HTTP API
//
// Which one is constructed is SMS_TRANSPORT — console or http — read in cmd/api and in
// cmd/notifier (SHIP-35, SHIP-187a). There is no smtp here for the obvious reason, and
// config refuses the value by name rather than ignoring it.
//
// # The environment used to decide, and no longer does
//
// It was UseConsole(env). SHIP-187a moved the choice into configuration for the reason the
// email package's own note gives, and the rule now lives in internal/config alone — no file
// in this package reads the environment.
//
// Unset, SMS_TRANSPORT still resolves from it: console in development, http elsewhere. That
// default matters more here than it does for email, and it is deliberately unchanged. SMS
// costs money per message and reaches a real handset, so a retry loop against a live gateway
// is a bill as well as a nuisance. An instance that should not send real messages says so
// with SMS_TRANSPORT=console — a visible choice in a file, rather than an inference from the
// environment.
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
// SHIP-36 was where a gateway was expected to be named and it closed without naming one,
// which cost nothing: the seam had existed since wave 1. SHIP-187a then made the naming
// unnecessary — the path, the credential's header and scheme, and the body template are all
// configuration, so following a buyer to their gateway is an edit to deploy/.env and no Go
// change. SMS gateways vary by country as well as by vendor, which makes this the adapter a
// buyer is most likely to have to repoint. See [Options].
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
