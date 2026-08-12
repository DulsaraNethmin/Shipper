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
//     lives here, and no provider-facing shape may carry it. SHIP-67 holds that with a test
//     rather than with review: budget_test.go reads this package's own source and refuses a
//     budget field on any struct but the owning customer's response, so a provider shape
//     written later cannot acquire one quietly.
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
// The file layout and the boundary rules are in services/core/README.md.
package jobs
