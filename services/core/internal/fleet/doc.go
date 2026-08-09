// Package fleet owns vehicles and the capability half of job eligibility — the third of
// the eight platform domains in Docs/06 §3.
//
// # What lives here
//
// Vehicle records, their capabilities and deactivation, and the query that decides which
// open jobs a given provider may see. SHIP-78, SHIP-81.
//
// # Rules this domain is responsible for
//
//   - Eligibility is a server-side decision. The app may grey out a job it believes the
//     provider cannot take, but the feed itself is filtered here and a bid on an
//     ineligible job is refused here (Docs/07 §3).
//   - A deactivated vehicle stops making its provider eligible for new work without
//     disturbing deliveries already under way. Vehicles are therefore deactivated, never
//     deleted.
//
// The package is empty at SHIP-10 by design. The skeleton exists so the boundaries are
// enforced before there is code to bend them — see the file layout and the boundary rules
// in services/core/README.md.
package fleet
