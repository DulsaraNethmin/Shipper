// Package bidding owns bids, negotiation, and the award — the fifth of the eight platform
// domains in Docs/06 §3, and the highest-risk work in the project (Docs/08 Step 2).
//
// # What lives here
//
// The eight bid statuses of Docs/02 §4, bid placement, revision and withdrawal,
// counter-offers and the supersede chain, bid expiry, job-scoped messaging, and the award
// transaction. SHIP-80…SHIP-97.
//
// # Rules this domain is responsible for
//
//   - Exactly one accepted bid per job, enforced by a partial unique index in PostgreSQL
//     rather than by application logic alone (Docs/02 §3, SHIP-91). The constraint is
//     written before the award endpoint deliberately: building correct behaviour against a
//     constraint that already exists is far easier than adding one afterwards and
//     discovering the data already violates it.
//   - Award is one transaction. Accepting a bid, rejecting every competing bid, and moving
//     the job to Awarded either all happen or none do (SHIP-92, SHIP-93). A retry with the
//     same idempotency key returns the original outcome, not an error (SHIP-94).
//   - The customer's budget never reaches a provider, in any form, through anything this
//     package serialises (Docs/01 §4.3).
//   - Only the latest offer in a chain is acceptable, and the whole chain stays readable to
//     the parties Docs/02 §4 permits (SHIP-88, SHIP-96).
//
// # Why the concurrency tests are not optional
//
// A duplicate award means two providers each believe they have the job, and the first
// either of them learns otherwise is at a pickup address. SHIP-95 covers double award,
// withdrawal during award, and expiry during award against a real PostgreSQL instance —
// a mocked repository accepts exactly the write the constraint exists to reject
// (Docs/06 §4.1).
//
// The package is empty at SHIP-10 by design. The skeleton exists so the boundaries are
// enforced before there is code to bend them — see the file layout and the boundary rules
// in services/core/README.md.
package bidding
