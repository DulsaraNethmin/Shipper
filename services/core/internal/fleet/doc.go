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
// # What SHIP-78 built
//
// The `vehicles` table (000300) and the six endpoints under `/v1/fleet/vehicles` that let a
// provider add, edit, deactivate and bring back a vehicle, and read their own fleet. Docs/01 §4.2's
// four verbs are three of them plus selection, which is a bid's business (SHIP-89) and needs no
// column here.
//
// Two things about the shape are worth knowing before reading the code:
//
//   - **Deactivation is an intent, not a field.** `deactivated_at` is not in the columns an edit
//     writes and there is no `active` field in any request type, for the same reason job status is
//     not settable: a provider who believes they took a truck off the road and did not will keep
//     receiving work for it. `DELETE` is likewise absent — a vehicle is named by the bid that won a
//     job and by the delivery that followed it, so the row outlives the provider's interest in it
//     (Docs/05 §3.1, Docs/10 §3.3).
//   - **The one-live-plate rule is a partial unique index**, not application logic.
//     `uq_vehicles_provider_registration` covers only vehicles still in service, which is what lets
//     a retired plate be added again while refusing a second live row for one truck. Two requests
//     racing would both find nothing on a SELECT-then-INSERT, so the index is the only thing that
//     can be right about it.
//
// # This domain emits no events and requires nothing of anybody
//
// There is no ports.go, and that is a finding rather than an omission: fleet is the first domain
// that needs neither another domain nor an adapter. Verification is checked against the *provider*
// and belongs to `profiles` (SHIP-84 onwards); SHIP-81 is where fleet first has to ask another
// domain a question, and it declares the interface it needs then.
//
// Nor does it emit a domain event. Nothing downstream is waiting to hear that a provider bought a
// van: SHIP-81's eligibility filter reads this table directly rather than a projection, and
// SHIP-89's bid names a vehicle by id at the moment it is placed. An event today would have no
// consumer, and an event with no consumer is a shape somebody later has to either keep or break.
// See the note at the top of service.go.
//
// # What is deliberately not here
//
//   - SHIP-79's service area and specialties. Those belong to the *provider* rather than to one
//     vehicle, and the capability vocabulary `jobs.vehicle_requirement` will one day be validated
//     against is that ticket's to define. [VehicleType] is a property of a vehicle and is not that
//     list — nothing in `jobs` is validated against it and nothing here reads that column.
//   - SHIP-81's eligibility filter. It reads these columns; it adds none.
//   - The customer's view of a vehicle. Docs/01 §4.3 lets a customer compare "provider profile,
//     vehicle, and declared capability" alongside the bids on their job, and that arrives with
//     SHIP-96 as a schema of its own — for the reason `jobs` keeps the customer's and the
//     provider's views apart.
package fleet
