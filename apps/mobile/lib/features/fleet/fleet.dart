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
/// **This was the first provider-only surface in the app**, and `ProviderOnly` is where that was
/// decided. It is worth reading before adding another one: it draws the line between hiding a
/// surface, which the device may do, and deciding what an account may do, which only the platform
/// may.
///
/// **It has moved to `core/auth/provider_only.dart`** (SHIP-100). It was written here while the
/// fleet was its only caller; the provider's job detail is its second, and features do not import
/// one another (`Docs/07` §2, `architecture_test.dart`). Nothing it does changed in the move.
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
