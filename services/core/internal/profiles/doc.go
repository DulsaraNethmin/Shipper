// Package profiles owns customer and provider profiles and the verification state that
// gates what an account may do — the second of the eight platform domains in
// Docs/06 §3.
//
// # What lives here
//
// Profile detail, the provider's declared service area and specialties, and the
// verification record: submitted documents, decision, reason, and expiry. SHIP-79,
// SHIP-153…SHIP-155, SHIP-159.
//
// # Rules this domain is responsible for
//
//   - Verification state is the eligibility gate. A provider bids only from a state that
//     permits it, and a customer publishes only from a state that permits it (Docs/04).
//     Other domains ask this one; they do not each keep their own copy of the answer.
//   - Verification documents are private evidence. They live in object storage with
//     metadata here, are reachable only through short-lived signed URLs, and every view
//     is access-logged (Docs/04 §3, SHIP-155).
//   - What is legally required, and for how long it is retained, is X-4's answer, not this
//     package's assumption.
//
// # What is built (SHIP-81a)
//
// The verification record and its five states. `provider_verifications` is migration block
// 200–299's first table, every provider has a row from registration, and the state is
// changed only by `provider_verification_decide()` — one guarded function, in the database,
// which records the actor and the reason before it moves anything. See verification.go for
// the domain and `000200_provider_verifications.up.sql` for why the guard lives where it
// does.
//
// **`internal/fleet` reads `provider_verifications` in SQL and imports nothing from here.**
// That is the seam, and it is the one the eligibility filter already uses for `users` and
// `jobs`: a Go port would put the answer to "may this provider bid" in two places, which
// `internal/fleet/eligibility.go` forbids in SHIP-81a's own words. Nothing in this package
// exports a `CanBid`, and nothing should.
//
// The profile half of this package's remit — Docs/01 §4.3's provider profile, its service
// area and its specialties — lives in `internal/fleet` today (SHIP-79, SHIP-79a), beside
// the vehicles it is declared with and the predicate it feeds. SHIP-81a deliberately did
// not move it: a migration that relocated another domain's shipped tables is a change with
// no ticket and a large blast radius, and nothing in this ticket needs it. Whether the
// declaration eventually moves here is an open question rather than a decision taken.
//
// The package was empty from SHIP-10 to SHIP-81a by design. The skeleton existed so the
// boundaries were enforced before there was code to bend them — see the file layout and the
// boundary rules in services/core/README.md.
package profiles
