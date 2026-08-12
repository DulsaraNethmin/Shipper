/// Fleet — the vehicles a provider maintains (`Docs/07` §2).
///
/// A vehicle's capacity determines which jobs a provider may bid on. The app may filter a list to
/// what looks eligible; eligibility itself is decided server-side, and a bid on an ineligible
/// vehicle is refused there (`Docs/07` §2, `CLAUDE.md`).
///
/// ## What is here (SHIP-98)
///
/// The whole of `Docs/01` §4.2 — add, edit, take off the road, bring back — over the six routes
/// `/v1/fleet/vehicles` serves, and nothing else. `fleet_repository.dart` names all six and says
/// what is deliberately absent.
///
/// **This is the first provider-only surface in the app.** `provider_only.dart` is where that is
/// decided, and it is worth reading before adding a second one: it draws the line between hiding a
/// surface, which the device may do, and deciding what an account may do, which only the platform
/// may.
///
/// ## What is not here
///
/// The **provider profile** — the service area and specialties of SHIP-79, served from
/// `/v1/fleet/profile` — is not modelled, not called, and not assumed. It belongs to the same
/// domain and to a different ticket.
///
/// The **capability vocabulary** a job's `vehicle_requirement` will be validated against is not
/// here either, and `VehicleType` is explicitly not it. See that enum's own note.
library;
