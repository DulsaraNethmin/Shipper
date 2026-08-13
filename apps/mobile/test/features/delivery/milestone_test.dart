// The four strings this client puts on the wire, held to the platform's own vocabulary.
//
// `internal/delivery/milestone.go` derives its wire form from `Docs/02` §1's names and reads it
// back **case-sensitively on purpose** — a client sending `Picked up` has misread the contract
// rather than typed the wrong case, and is refused with a `422` rather than quietly understood. So
// these are a contract, and a rename on either side has to fail somewhere. On this side it fails
// here.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/features/delivery/milestone.dart';

void main() {
  group('the wire vocabulary', () {
    test('is the platform’s, exactly', () {
      expect(
        Milestone.values.map((milestone) => milestone.wire).toList(),
        <String>['en_route_to_pickup', 'picked_up', 'in_transit', 'delivered'],
      );
    });

    test('is read back by the same names it writes', () {
      for (final milestone in Milestone.values) {
        expect(Milestone.byWire(milestone.wire), milestone);
      }
    });

    test('does not answer to the stored form, for the reason the platform does not', () {
      expect(Milestone.byWire('Picked up'), isNull);
      expect(Milestone.byWire('PICKED_UP'), isNull);
    });

    test('has no name for a milestone a later build might send', () {
      // The app-upgrade case, and it is real: a row written by a later build sits in SHIP-124's
      // queue and is read back by this one. `null` is what makes the screen render the raw value
      // rather than dropping the row.
      expect(Milestone.byWire('handed_to_recipient'), isNull);
    });

    test('has no driver_assigned, because that milestone has an endpoint of its own', () {
      // Docs/01 §4.4 numbers five and the column holds five; the milestone endpoint refuses this
      // one with a 422 pointing at POST /v1/jobs/{id}/driver (SHIP-106, SHIP-111). A value this
      // client cannot send is a button somebody would eventually wire up.
      expect(Milestone.byWire('driver_assigned'), isNull);
      expect(Milestone.values.map((m) => m.wire), isNot(contains('driver_assigned')));
    });
  });

  group('what the screen offers', () {
    test('is every milestone except the one that needs proof', () {
      expect(Milestone.offered, <Milestone>[
        Milestone.enRouteToPickup,
        Milestone.pickedUp,
        Milestone.inTransit,
      ]);
    });

    test('is derived from needsProof rather than listed twice', () {
      // A second hand-written list is a second thing to keep in step, and the one that goes stale
      // is the one nobody reads. SHIP-130 flips `delivered` into the offered set by capturing a
      // photograph, not by editing a list.
      expect(
        Milestone.offered,
        Milestone.values.where((milestone) => !milestone.needsProof).toList(),
      );
      expect(Milestone.delivered.needsProof, isTrue);
    });
  });

  group('the labels', () {
    test('are Docs/02 §1’s own names for the statuses', () {
      // The customer's timeline (SHIP-133) renders the same vocabulary. A driver telling a customer
      // "I marked it collected" about a screen that says `Picked up` is a support call made out of
      // a wording choice.
      expect(
        Milestone.values.map((milestone) => milestone.label).toList(),
        <String>['En route to pickup', 'Picked up', 'In transit', 'Delivered'],
      );
    });

    test('are distinct, and so are the hints under them', () {
      expect(Milestone.values.map((m) => m.label).toSet(), hasLength(Milestone.values.length));
      expect(Milestone.values.map((m) => m.hint).toSet(), hasLength(Milestone.values.length));
    });
  });
}
