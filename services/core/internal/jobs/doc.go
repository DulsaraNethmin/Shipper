// Package jobs owns the job record and its lifecycle — the fourth of the eight platform
// domains in Docs/06 §3, and the centre of the product.
//
// # What lives here
//
// The twelve statuses of Docs/02 §1 and the single guarded transition function, drafts,
// publication, amendment, cancellation, goods categories, addresses, budget, and expiry.
// SHIP-56…SHIP-70.
//
// # Rules this domain is responsible for
//
//   - Status is never a settable field. Every transition passes one guarded function that
//     validates the move, records who made it and why, and emits the domain event
//     (Docs/02 §2, SHIP-57). A status assignment anywhere else is a defect.
//   - The customer's budget is never exposed to a provider — not as an amount, a band, or
//     a "budget supplied" flag, through any endpoint or response (Docs/01 §4.3). The field
//     lives here, and the provider-facing serialisation cannot carry it. SHIP-67 proves
//     that with a test rather than trusting review to catch it.
//   - Category lists and validation limits are reference data loaded at runtime, not
//     constants (SHIP-58). Flutter has no over-the-air update path for Dart code, so
//     anything that moves under operational pressure has to move server-side (Docs/06
//     §5.3).
//
// # Where the events go
//
// Domain events are emitted by this package as part of the transaction that changes the
// job, not by the API layer (Docs/08 slice 5, SHIP-134). A caller that changes a job and
// separately publishes an event has built two sources of truth.
//
// The package is empty at SHIP-10 by design. The skeleton exists so the boundaries are
// enforced before there is code to bend them — see the file layout and the boundary rules
// in services/core/README.md.
package jobs
