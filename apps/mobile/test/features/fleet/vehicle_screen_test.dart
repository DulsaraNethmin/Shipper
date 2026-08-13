// SHIP-98 — one vehicle: reading it, editing it, and taking it off the road or putting it back.
//
// Reached the way a provider reaches it — sign in, open the fleet, tap the vehicle — because the
// route carries the id and the screen re-reads from it, and both halves of that are the ticket: a
// screen handed a vehicle across from the list would show whatever the list happened to have
// fetched, and would be unreachable from a notification (Docs/07 §5, SHIP-145).

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/fleet/vehicle.dart';

import 'fake_fleet_repository.dart';
import 'fleet_app.dart';

const _id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

/// A fake whose fleet holds exactly the vehicle these tests open.
FakeFleetRepository fleetHolding(Vehicle vehicle) {
  return FakeFleetRepository()
    ..page = ApiPage<Vehicle>(data: <Vehicle>[vehicle])
    ..detail = (_) => vehicle;
}

/// A finder scoped to the vehicle screen.
///
/// The fleet is still mounted underneath the pushed route, so an unscoped `find.text` can match a
/// card as readily as the screen on top of it.
Finder inVehicle(Finder matching) =>
    find.descendant(of: find.byKey(const Key('vehicle-detail')), matching: matching);

void main() {
  group('getting there', () {
    testWidgets('tapping a vehicle opens it, and the screen reads it by id', (tester) async {
      final fleet = fleetHolding(aVehicle(id: _id));

      await openVehicle(tester, fleet, _id);

      expect(find.byKey(const Key('vehicle-detail')), findsOneWidget);
      expect(fleet.callsTo('read'), hasLength(1));
      expect(fleet.callsTo('read').single.vehicleId, _id);
    });

    testWidgets('the vehicle is re-read rather than carried across from the list', (tester) async {
      // The list page may be minutes old. What the provider opens has to be what the platform holds
      // now, which is also what makes a deep link reach the same screen with nothing but an id.
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(data: <Vehicle>[aVehicle(id: _id)])
        ..detail = (id) => aVehicle(id: id, active: false, deactivatedAt: '2026-08-03T00:00:00Z');

      await openVehicle(tester, fleet, _id);

      expect(inVehicle(find.byKey(const Key('vehicle-out-of-service'))), findsOneWidget);
    });

    testWidgets('a vehicle that could not be read at all offers a retry', (tester) async {
      final fleet = fleetHolding(aVehicle(id: _id));
      fleet.failures['read'] = const ApiErrorResponse(
        statusCode: 404,
        code: 'not_found',
        message: 'No such vehicle.',
      );

      await openVehicle(tester, fleet, _id);

      expect(find.byKey(const Key('vehicle-detail-failed')), findsOneWidget);

      await tester.tap(find.byKey(const Key('vehicle-detail-retry')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('read'), hasLength(2));
    });
  });

  group('what it shows', () {
    testWidgets('the vehicle in full, in the units CLAUDE.md fixes', (tester) async {
      await openVehicle(tester, fleetHolding(aVehicle(id: _id)), _id);

      expect(inVehicle(find.text('ABC123')), findsOneWidget);
      expect(inVehicle(find.text('Van')), findsOneWidget);
      expect(inVehicle(find.text('Mercedes-Benz')), findsOneWidget);
      // Kilograms and centimetres, and no decimal point nobody typed.
      expect(inVehicle(find.text('1200 kg')), findsOneWidget);
      expect(inVehicle(find.text('320 × 175 × 190 cm')), findsOneWidget);
    });

    testWidgets('a vehicle with no capacity says so as an ordinary thing', (tester) async {
      await openVehicle(tester, fleetHolding(aBareVehicle(id: _id)), _id);

      expect(inVehicle(find.byKey(const Key('vehicle-no-capacity'))), findsOneWidget);
    });

    testWidgets('it says why there is no delete', (tester) async {
      // "Delete" is what somebody will look for. Not finding it needs an explanation rather than a
      // silence, and the explanation is a real one: the vehicle is named by bids and deliveries.
      await openVehicle(tester, fleetHolding(aVehicle(id: _id)), _id);

      expect(inVehicle(find.byKey(const Key('vehicle-no-delete'))), findsOneWidget);
    });
  });

  group('editing', () {
    Future<void> openEditor(WidgetTester tester, FakeFleetRepository fleet) async {
      await openVehicle(tester, fleet, _id);
      await tester.tap(find.byKey(const Key('vehicle-action-edit')));
      await tester.pumpAndSettle();
    }

    testWidgets('the form starts from what the platform stored', (tester) async {
      await openEditor(tester, fleetHolding(aVehicle(id: _id)));

      expect(find.byKey(const Key('vehicle-form')), findsOneWidget);
      expect(find.widgetWithText(TextFormField, 'ABC123'), findsOneWidget);
      expect(find.widgetWithText(TextFormField, 'Mercedes-Benz'), findsOneWidget);
      // 1200 rather than 1200.0: a form that echoed a decimal point nobody typed teaches people
      // that the app rewrites what they enter.
      expect(find.widgetWithText(TextFormField, '1200'), findsOneWidget);
    });

    testWidgets('an edit is a PATCH against this vehicle, with its own key', (tester) async {
      final fleet = fleetHolding(aVehicle(id: _id));

      await openEditor(tester, fleet);
      await tester.enterText(find.byKey(const Key('vehicle-make')), 'Isuzu');
      await tester.pump();
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('update'), hasLength(1));
      expect(fleet.callsTo('update').single.vehicleId, _id);
      expect(fleet.callsTo('update').single.fields['make'], 'Isuzu');
      expect(fleet.keysFor('update').single, isNotEmpty);
    });

    testWidgets('emptying a box clears the value rather than leaving it alone', (tester) async {
      // The contract's middle row, and the reason it exists: without it a provider could state a
      // load height and never correct it back to "unstated".
      final fleet = fleetHolding(aVehicle(id: _id));

      await openEditor(tester, fleet);
      await tester.enterText(find.byKey(const Key('vehicle-load-height')), '');
      await tester.pump();
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('update').single.fields['load_height_cm'], 0);
    });

    testWidgets('a saved edit closes the form and shows what came back', (tester) async {
      final fleet = fleetHolding(aVehicle(id: _id));

      await openEditor(tester, fleet);
      await tester.enterText(find.byKey(const Key('vehicle-make')), 'Isuzu');
      await tester.pump();
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('vehicle-form')), findsNothing);
      expect(inVehicle(find.text('Isuzu')), findsOneWidget);
    });

    testWidgets('a refused edit keeps the form, filled in', (tester) async {
      final fleet = fleetHolding(aVehicle(id: _id));
      fleet.failures['update'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'One or more fields were rejected.',
        details: <ApiFieldError>[
          ApiFieldError(
            field: 'registration',
            code: 'required',
            message: "Enter the vehicle's registration.",
          ),
        ],
      );

      await openEditor(tester, fleet);
      await tester.enterText(find.byKey(const Key('vehicle-make')), 'Isuzu');
      await tester.pump();
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('vehicle-form')), findsOneWidget);
      expect(find.widgetWithText(TextFormField, 'Isuzu'), findsOneWidget);
      expect(find.text("Enter the vehicle's registration."), findsOneWidget);
    });

    testWidgets('a type this build cannot name can be left alone while the rest is saved',
        (tester) async {
      // An old build editing a vehicle whose type was added after it shipped. It must be able to
      // correct the plate without being forced to reclassify a truck it cannot describe.
      final fleet = fleetHolding(
        aVehicle(id: _id).copyWith(vehicleType: VehicleType.unknown),
      );

      await openEditor(tester, fleet);
      await tester.enterText(find.byKey(const Key('vehicle-make')), 'Kenworth');
      await tester.pump();
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('update'), hasLength(1));
      // Omitted, which is PATCH's own way of saying "leave it as it was".
      expect(fleet.callsTo('update').single.fields.containsKey('vehicle_type'), isFalse);
      expect(fleet.callsTo('update').single.fields['make'], 'Kenworth');
    });

    testWidgets('discarding an edit leaves the vehicle untouched', (tester) async {
      final fleet = fleetHolding(aVehicle(id: _id));

      await openEditor(tester, fleet);
      await tester.enterText(find.byKey(const Key('vehicle-make')), 'Isuzu');
      await tester.pump();
      await tester.tap(find.byKey(const Key('vehicle-cancel')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('vehicle-detail')), findsOneWidget);
      expect(fleet.callsTo('update'), isEmpty);
    });
  });

  group('taking it off the road', () {
    testWidgets('it is confirmed rather than taken on one tap', (tester) async {
      // A provider who takes a truck off the road without meaning to stops receiving work and has
      // no reason to suspect why — the failure is silent from where they are standing.
      final fleet = fleetHolding(aVehicle(id: _id));

      await openVehicle(tester, fleet, _id);
      await tester.tap(find.byKey(const Key('vehicle-action-deactivate')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('vehicle-deactivate-dialog')), findsOneWidget);

      await tester.tap(find.byKey(const Key('vehicle-deactivate-dismiss')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('deactivate'), isEmpty);
    });

    testWidgets('confirming sends it, with an idempotency key and no body', (tester) async {
      final fleet = fleetHolding(aVehicle(id: _id));

      await openVehicle(tester, fleet, _id);
      await tester.tap(find.byKey(const Key('vehicle-action-deactivate')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('vehicle-deactivate-confirm')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('deactivate'), hasLength(1));
      expect(fleet.callsTo('deactivate').single.fields, isEmpty);
      expect(fleet.keysFor('deactivate').single, isNotEmpty);
      expect(inVehicle(find.byKey(const Key('vehicle-out-of-service'))), findsOneWidget);
    });

    testWidgets('the button becomes its opposite, and back again', (tester) async {
      // Read off the vehicle's own `active` field rather than from a table this client keeps, so
      // there is nothing here that can go stale.
      final fleet = fleetHolding(aVehicle(id: _id));

      await openVehicle(tester, fleet, _id);
      await tester.tap(find.byKey(const Key('vehicle-action-deactivate')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('vehicle-deactivate-confirm')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('vehicle-action-reactivate')), findsOneWidget);
      expect(find.byKey(const Key('vehicle-action-deactivate')), findsNothing);

      await tester.tap(find.byKey(const Key('vehicle-action-reactivate')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('reactivate'), hasLength(1));
      expect(find.byKey(const Key('vehicle-action-deactivate')), findsOneWidget);
      expect(inVehicle(find.byKey(const Key('vehicle-in-service'))), findsOneWidget);
    });

    testWidgets('the two verbs do not share an idempotency key', (tester) async {
      // Docs/07 §4: one key per action. A shared key would make the second verb a retry of the
      // first, refused with idempotency_key_reused — a button that stops working for a reason
      // nobody looking at the screen could guess.
      final fleet = fleetHolding(aVehicle(id: _id));

      await openVehicle(tester, fleet, _id);
      await tester.tap(find.byKey(const Key('vehicle-action-deactivate')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('vehicle-deactivate-confirm')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('vehicle-action-reactivate')));
      await tester.pumpAndSettle();

      expect(fleet.keysFor('deactivate').single, isNot(fleet.keysFor('reactivate').single));
    });
  });

  group('a refusal the app did not expect', () {
    testWidgets('a refused reactivation is shown and the vehicle is re-read', (tester) async {
      // Only one vehicle per plate may be in service, so a replacement added on another device
      // refuses this with a 409. Docs/02 §3.1's rule is that on conflict the server wins and the app
      // reconciles — and the refusal stays on screen while it does, because a reload that cleared it
      // would leave somebody watching the screen change with nothing to explain their tap.
      final fleet = fleetHolding(
        aVehicle(id: _id, active: false, deactivatedAt: '2026-08-03T00:00:00Z'),
      );
      fleet.failures['reactivate'] = const ApiErrorResponse(
        statusCode: 409,
        code: 'fleet_duplicate_registration',
        message: 'A vehicle with that registration is already in service in this fleet.',
      );

      await openVehicle(tester, fleet, _id);
      await tester.tap(find.byKey(const Key('vehicle-action-reactivate')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.textContaining('already in service'), findsOneWidget);
      // Reconciled: the vehicle was read again after the refusal.
      expect(fleet.callsTo('read'), hasLength(2));
      // And it is still off the road, because the platform refused to change that.
      expect(inVehicle(find.byKey(const Key('vehicle-out-of-service'))), findsOneWidget);
    });

    testWidgets('the app offers the action anyway rather than pre-empting the platform',
        (tester) async {
      // The client holds no table of what may be reactivated. It offers the opposite of whatever
      // the vehicle currently is and lets the platform decide, which is the whole of "the app may
      // hide or disable; the platform decides".
      final fleet = fleetHolding(
        aVehicle(id: _id, active: false, deactivatedAt: '2026-08-03T00:00:00Z'),
      );

      await openVehicle(tester, fleet, _id);

      expect(find.byKey(const Key('vehicle-action-reactivate')), findsOneWidget);
    });
  });
}
