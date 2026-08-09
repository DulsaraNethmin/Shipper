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
// The package is empty at SHIP-10 by design. The skeleton exists so the boundaries are
// enforced before there is code to bend them — see the file layout and the boundary rules
// in services/core/README.md.
package profiles
