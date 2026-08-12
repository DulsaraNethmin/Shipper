import 'package:json_annotation/json_annotation.dart';

/// One of the twelve job states in `Docs/02` §1.
///
/// **Lower snake case on the wire, and this is the enum a client genuinely branches on.**
/// `Docs/10` §4.7 puts enum values on the wire in lower snake case even where the stored form has
/// spaces, so `En route to pickup` reaches the app as `en_route_to_pickup`. That is deliberately
/// unlike the Australian state, which is returned as `NSW` because a client prints it rather than
/// switching on it.
///
/// ## This is the Dart copy, and it is temporary by design
///
/// `Docs/10` §8.2 makes `contracts/statuses.yaml` the source for all three languages, generated
/// by `make codegen` at SHIP-56a. That file does not exist yet, so the Go service wrote its own
/// copy at SHIP-61 — derived from its twelve constants rather than tabulated — and this is the
/// Dart one. **When SHIP-56a lands it replaces this file**, and the wire strings below are the
/// contract it has to reproduce: a build already installed on a phone branches on `driver_assigned`
/// and cannot have it renamed underneath it.
///
/// [label] is the other half, and it is not free-form copy: `CLAUDE.md` requires the exact names
/// in `Docs/02` §1, because those are the words the whole product uses — a customer reading
/// "Driver assigned" in the app and support reading it in an audit entry have to be reading about
/// the same thing.
enum JobStatus {
  /// The customer is still preparing the job. Not visible to providers.
  @JsonValue('draft')
  draft,

  /// Published and eligible for bids.
  @JsonValue('open')
  open,

  /// One or more active bids exist. The job is still open to eligible bids — `Docs/02` §1 calls
  /// this a presentation status rather than a closed one.
  @JsonValue('negotiating')
  negotiating,

  /// The customer has accepted one bid.
  @JsonValue('awarded')
  awarded,

  @JsonValue('driver_assigned')
  driverAssigned,

  @JsonValue('en_route_to_pickup')
  enRouteToPickup,

  @JsonValue('picked_up')
  pickedUp,

  @JsonValue('in_transit')
  inTransit,

  @JsonValue('delivered')
  delivered,

  @JsonValue('completed')
  completed,

  @JsonValue('cancelled')
  cancelled,

  @JsonValue('disputed')
  disputed,

  /// A status this build has never heard of.
  ///
  /// Not one the platform sends: the contract enumerates exactly the twelve above. It exists
  /// because the alternative to decoding a thirteenth is **throwing** on it, and `Docs/07` §6 is
  /// built on old builds living on devices indefinitely — a client that crashes rather than
  /// degrades has no over-the-air fix. Same reasoning, and the same shape, as `UserRole.unknown`.
  ///
  /// It is deliberately last, so that [values] stays in `Docs/02` §1's own order and a screen
  /// grouping by status gets that order without writing a second list.
  unknown;

  /// What the wire calls this status.
  ///
  /// Present for the query parameter and for tests that assert against the contract, not because
  /// anything renders it. Nothing in this app *sets* a status — `Docs/02` §2 and `CLAUDE.md` put
  /// every transition behind one guarded server-side function, and there is no request field for
  /// it anywhere in the API.
  String get wireName => switch (this) {
        JobStatus.draft => 'draft',
        JobStatus.open => 'open',
        JobStatus.negotiating => 'negotiating',
        JobStatus.awarded => 'awarded',
        JobStatus.driverAssigned => 'driver_assigned',
        JobStatus.enRouteToPickup => 'en_route_to_pickup',
        JobStatus.pickedUp => 'picked_up',
        JobStatus.inTransit => 'in_transit',
        JobStatus.delivered => 'delivered',
        JobStatus.completed => 'completed',
        JobStatus.cancelled => 'cancelled',
        JobStatus.disputed => 'disputed',
        JobStatus.unknown => 'unknown',
      };

  /// The status as a person reads it — the exact name from `Docs/02` §1 (`CLAUDE.md`).
  String get label => switch (this) {
        JobStatus.draft => 'Draft',
        JobStatus.open => 'Open',
        JobStatus.negotiating => 'Negotiating',
        JobStatus.awarded => 'Awarded',
        JobStatus.driverAssigned => 'Driver assigned',
        JobStatus.enRouteToPickup => 'En route to pickup',
        JobStatus.pickedUp => 'Picked up',
        JobStatus.inTransit => 'In transit',
        JobStatus.delivered => 'Delivered',
        JobStatus.completed => 'Completed',
        JobStatus.cancelled => 'Cancelled',
        JobStatus.disputed => 'Disputed',

        // Deliberately not "Unknown", which reads as a fault. The job is fine; this build is
        // old, and saying which is the difference between an update and a support call —
        // the same distinction the shell draws for an unrecognised account type.
        JobStatus.unknown => 'Not shown by this version',
      };
}
