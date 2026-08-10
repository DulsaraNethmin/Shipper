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
package email
