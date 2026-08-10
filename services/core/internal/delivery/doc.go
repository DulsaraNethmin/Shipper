// Package delivery owns execution and proof, from driver assignment to completion — the
// sixth of the eight platform domains in Docs/06 §3.
//
// # What lives here
//
// Driver assignment, the job-scoped link token, milestones with dual timestamps,
// out-of-order absorption, proof of delivery and the reasoned exception that may stand in
// for it, and the auto-complete timer. SHIP-105…SHIP-119.
//
// # Rules this domain is responsible for
//
//   - Delivered requires photo proof or a recorded exception reason — never neither
//     (Docs/01 §4.4, SHIP-118). An exception-completed job goes to the moderation queue
//     rather than being quietly accepted (SHIP-117).
//   - The job-scoped token grants access to exactly one job and cannot be exchanged for a
//     user session (Docs/06 §5.2, SHIP-108). It is a separate system from the mobile
//     session tokens the identity domain issues.
//   - Every milestone records the actor's clock and the server's clock separately
//     (SHIP-110). A device that has been offline for hours reports a time the server
//     cannot verify, and collapsing the two loses the only evidence that they disagreed.
//   - A late milestone arriving after the job has moved on is recorded as history and never
//     moves the job backwards (SHIP-112). Where a queued update contradicts an
//     administrator's decision, the administrator wins and the update is retained with its
//     reason (SHIP-113).
//
// # Offline is the normal case, not the exception
//
// Milestones and proof are captured where the signal is worst. The device queues durably
// and replays with a per-item idempotency key (SHIP-124, SHIP-125); this package must
// therefore treat a repeated milestone as the same milestone, and the server stays
// authoritative throughout (Docs/02 §6).
//
// Proof images are uploaded directly to private object storage through short-lived
// pre-signed URLs and never proxied through this service (Docs/06 §5.2, SHIP-114). What
// is stored here is the metadata and the access control.
//
// The package is empty at SHIP-10 by design. The skeleton exists so the boundaries are
// enforced before there is code to bend them — see the file layout and the boundary rules
// in services/core/README.md.
package delivery
