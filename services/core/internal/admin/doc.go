// Package admin owns support, moderation, disputes, and the audit trail — the eighth of
// the eight platform domains in Docs/06 §3.
//
// # What lives here
//
// Administrator authentication and permissions, the append-only audit log, user and job
// search, the moderation and exception queues of Docs/04 §5, unpublish and suspension
// actions, internal notes, and the dispute workflow. SHIP-147…SHIP-166.
//
// # Rules this domain is responsible for
//
//   - Administrator sign-in is a separate system from user sign-in, and a user token can
//     never reach an administrative endpoint (SHIP-147). The privilege boundary is the
//     point; sharing the token system erases it.
//   - Audit entries are append-only. An ordinary administrator cannot delete one, and
//     every privileged mutation writes one recording actor, action, target, time, and
//     reason (SHIP-149, SHIP-150). Audit cannot be backfilled, which is why it is built
//     with the actions rather than after them.
//   - Permissions are granular and default to the minimum (SHIP-148).
//   - A permanent suspension needs a second administrator's approval (SHIP-166).
//   - Internal notes are never user-visible (SHIP-162).
//
// # Why this sits late in the backlog but is worth pulling forward
//
// M6 is scheduled after the product works because no real users exist until M7. Working
// solo, user and job search (SHIP-151, SHIP-152) pay for themselves as debugging tools
// long before that, and Docs/09 explicitly permits pulling just those two forward.
//
// The package is empty at SHIP-10 by design. The skeleton exists so the boundaries are
// enforced before there is code to bend them — see the file layout and the boundary rules
// in services/core/README.md.
package admin
