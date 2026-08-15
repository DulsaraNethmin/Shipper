/// The milestones this app records (SHIP-129).
///
/// `Docs/01` §4.4 numbers five and `internal/delivery/milestone.go` holds all five, because the
/// column does. This list is **four**, and the missing one is not an omission: `driver_assigned`
/// has an endpoint of its own (`POST /v1/jobs/{id}/driver`, SHIP-106) and the milestone endpoint
/// refuses it with a `422` pointing there. A value this client cannot send is a button somebody
/// would eventually wire up.
///
/// ## The wire forms are the platform's and are matched exactly
///
/// `Milestone.Wire` on the platform is lower snake case derived from `Docs/02` §1's own strings,
/// and `MilestoneFromWire` is **case-sensitive on purpose** — a client sending `Picked up` has
/// misread the contract rather than typed the wrong case, and is told so. So these four strings
/// are a contract and not a formatting choice, and `milestone_test.dart` holds them.
///
/// ## What the label says, and why it is the platform's word
///
/// The label is `Docs/02` §1's own name for the status, not a friendlier synonym. The customer's
/// timeline (SHIP-133) renders the same vocabulary, and a driver telling a customer "I marked it
/// collected" about a screen that says `Picked up` is a support call made out of a wording choice.
library;

/// One of the four milestones a provider can record from the app.
enum Milestone {
  /// The driver has set off towards the collection address.
  enRouteToPickup(
    'en_route_to_pickup',
    'En route to pickup',
    'The driver has set off to collect the goods.',
  ),

  /// The goods are on the vehicle.
  pickedUp(
    'picked_up',
    'Picked up',
    'The goods are on the vehicle.',
  ),

  /// The delivery leg has begun.
  inTransit(
    'in_transit',
    'In transit',
    'On the way to the delivery address.',
  ),

  /// The goods are with the recipient.
  ///
  /// **Not offered by this build, and the refusal is the platform's rather than this screen's
  /// opinion.** `CLAUDE.md`'s invariant is that delivered requires photo proof or a recorded
  /// exception reason and never neither; `POST /v1/jobs/{id}/milestones` answers every `delivered`
  /// with `delivery_proof_required` until SHIP-118 narrows it, and neither a photograph nor an
  /// exception can be captured on this device until SHIP-130 and SHIP-131. Offering the button
  /// anyway would queue an operation whose only possible outcome is a quarantined row — work the
  /// driver believes they recorded, sitting in the queue waiting for a person.
  ///
  /// It stays in this list so the screen can name it and say what it is waiting for, and so
  /// SHIP-130 has the value to send rather than a string to invent.
  delivered(
    'delivered',
    'Delivered',
    'The goods are with the recipient.',
  );

  const Milestone(this.wire, this.label, this.hint);

  /// What goes in the request body. The platform's own wire form (`Docs/10` §4.7).
  final String wire;

  /// What the screen calls it. `Docs/02` §1's name for the status.
  final String label;

  /// One line under the label, so a driver in a yard is not guessing which of two applies.
  final String hint;

  /// The milestones this build offers a button for.
  ///
  /// [delivered] is deliberately absent — see its own note. Derived rather than listed a second
  /// time, so the two cannot disagree.
  static List<Milestone> get offered =>
      values.where((milestone) => !milestone.needsProof).toList(growable: false);

  /// Whether recording this milestone requires proof the app cannot capture yet.
  bool get needsProof => this == Milestone.delivered;

  /// The milestone [wire] names, or `null` if this build has never heard of it.
  ///
  /// `null` is a real case rather than a defensive one: a row written by a later build sits in
  /// SHIP-124's queue and is read back by this one. The screen renders the raw value rather than
  /// dropping the row, because a queued operation that is not displayed is the silent drop
  /// `Docs/07` §4 forbids, arriving one layer above the queue.
  ///
  /// **A milestone this app cannot record is not a milestone it cannot be shown**, which is why
  /// [milestoneLabel] exists beside this rather than a fifth value being added here. See below.
  static Milestone? byWire(String wire) {
    for (final milestone in values) {
      if (milestone.wire == wire) return milestone;
    }
    return null;
  }
}

/// What to call the milestone [wire] names, whether or not this app can record it (SHIP-133).
///
/// ## Why this is a function beside [Milestone] rather than a fifth value inside it
///
/// [Milestone] is deliberately four: it is *the milestones this app records*, and
/// `driver_assigned` is not one — it has an endpoint of its own and the milestone endpoint refuses
/// it with a `422` pointing there. Adding it to the enumeration would put it in
/// [Milestone.offered] unless a second flag were added to take it out again, which is a button a
/// driver would eventually be shown for an act this app cannot perform.
///
/// **Reading is the other half, and it arrived with the customer's tracking view.**
/// `GET /v1/jobs/{id}/delivery/milestones` serves *every* milestone anybody recorded, and its
/// enumeration is all five. A customer looking at their own delivery must not be shown
/// `driver_assigned` in its wire spelling.
///
/// So the four labels are **derived** from the enumeration and cannot drift from it, and the fifth
/// is named once, here. An unrecognised value is returned unaltered rather than dropped or reported
/// as a fault: `Docs/07` §6 is built on old builds living on devices indefinitely, and a sixth
/// milestone is a row this build should still show a person.
String milestoneLabel(String wire) => Milestone.byWire(wire)?.label ?? _readOnlyLabels[wire] ?? wire;

/// The milestones this app can be shown and cannot record.
///
/// `Docs/02` §1's own name, as `CLAUDE.md` requires: a customer reading "Driver assigned" here and
/// support reading it in an audit entry have to be reading about the same thing.
const _readOnlyLabels = <String, String>{
  'driver_assigned': 'Driver assigned',
};
