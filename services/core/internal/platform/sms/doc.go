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
package sms
