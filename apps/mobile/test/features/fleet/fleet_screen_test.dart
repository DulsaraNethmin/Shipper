// SHIP-98 — the provider's fleet: seeing it, and adding to it.
//
// Driven through the real app: the session, the router, the guard, the shell, the provider half,
// and the tap that opens the fleet. Only the token store and the repositories are substituted, so a
// guard that refused `/fleet/vehicles` — which is exactly what SHIP-49's guard did to the first
// screen hung off the shell — fails here rather than only on a device.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/fleet/vehicle.dart';

import 'fake_fleet_repository.dart';
import 'fleet_app.dart';

const _van = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';
const _ute = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1';

/// A finder scoped to the fleet screen.
Finder inFleet(Finder matching) =>
    find.descendant(of: find.byKey(const Key('fleet')), matching: matching);

void main() {
  group('getting there', () {
    testWidgets('a provider reaches their fleet from the shell', (tester) async {
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(data: <Vehicle>[aVehicle(id: _van)]);

      await openFleet(tester, fleet);

      expect(find.byKey(const Key('fleet')), findsOneWidget);
      expect(fleet.callsTo('list'), hasLength(1));
    });

    testWidgets('the fleet is read without a status filter', (tester) async {
      // Both halves have to be on screen: a provider who cannot see a retired vehicle cannot bring
      // it back, and reactivating is half of what this screen exists for.
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(data: <Vehicle>[aVehicle(id: _van)]);

      await openFleet(tester, fleet);

      expect(fleet.callsTo('list').single.cursor, isNull);
    });
  });

  group('what it shows', () {
    testWidgets('groups the fleet into what is on the road and what is not', (tester) async {
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(data: <Vehicle>[
          aVehicle(id: _van),
          aBareVehicle(
            id: _ute,
            active: false,
          ).copyWith(deactivatedAt: '2026-08-03T03:34:12.000Z'),
        ]);

      await openFleet(tester, fleet);

      expect(inFleet(find.byKey(const Key('fleet-group-in-service'))), findsOneWidget);
      expect(inFleet(find.byKey(const Key('fleet-group-out-of-service'))), findsOneWidget);
      expect(inFleet(find.byKey(const Key('vehicle-$_van'))), findsOneWidget);
      expect(inFleet(find.byKey(const Key('vehicle-$_ute'))), findsOneWidget);
    });

    testWidgets('a vehicle off the road says since when, day-first', (tester) async {
      // "Why did I stop seeing jobs" is a question the date answers and a boolean does not. Day
      // first with the month spelled, because 03/08 is a different day to two different readers.
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(data: <Vehicle>[
          aVehicle(id: _van, active: false, deactivatedAt: '2026-08-03T03:34:12.000Z'),
        ]);

      await openFleet(tester, fleet);

      expect(inFleet(find.textContaining('Off the road since 3 Aug 2026')), findsOneWidget);
    });

    testWidgets('the plate leads, in the form the platform stored it', (tester) async {
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(data: <Vehicle>[aVehicle(id: _van, registration: 'ABC123')]);

      await openFleet(tester, fleet);

      expect(inFleet(find.text('ABC123')), findsOneWidget);
    });

    testWidgets('a vehicle with nothing but a plate and a type still draws', (tester) async {
      // The ordinary case: somebody standing in a truck yard has the plate and may not know the
      // load height, and the platform omits every field they left alone.
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(data: <Vehicle>[aBareVehicle(id: _ute)]);

      await openFleet(tester, fleet);

      expect(inFleet(find.text('XYZ789')), findsOneWidget);
      expect(inFleet(find.text('Ute')), findsOneWidget);
    });

    testWidgets('a type this build cannot name is said so, not shown as a fault', (tester) async {
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(
          data: <Vehicle>[aBareVehicle(id: _ute).copyWith(vehicleType: VehicleType.unknown)],
        );

      await openFleet(tester, fleet);

      expect(inFleet(find.textContaining('Not named by this version')), findsOneWidget);
    });
  });

  group('the three states a list has', () {
    testWidgets('a provider with no vehicles sees an empty state, not a spinner', (tester) async {
      // The case that never occurs in development, because whoever is building this has a truck.
      await openFleet(tester, FakeFleetRepository());

      expect(inFleet(find.byKey(const Key('fleet-empty'))), findsOneWidget);
      expect(find.byKey(const Key('fleet-loading')), findsNothing);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
    });

    testWidgets('the empty state offers the way out of it', (tester) async {
      await openFleet(tester, FakeFleetRepository());

      expect(inFleet(find.byKey(const Key('fleet-add'))), findsOneWidget);
    });

    testWidgets('a spinner while the first page is in flight', (tester) async {
      final held = Completer<void>();
      final fleet = FakeFleetRepository()..gates['list'] = held;

      await signInAs(tester, UserRole.provider, fleet: fleet);

      // Pumped rather than settled: a spinner animates for ever, so `pumpAndSettle` while one is on
      // screen waits for something that never happens. The frames here are the push transition.
      await tester.tap(find.byKey(const Key('manage-vehicles')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      expect(find.byKey(const Key('fleet-loading')), findsOneWidget);
      expect(find.byKey(const Key('fleet-empty')), findsNothing);

      held.complete();
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('fleet-loading')), findsNothing);
      expect(find.byKey(const Key('fleet-empty')), findsOneWidget);
    });

    testWidgets('a fleet that could not be read at all offers a retry', (tester) async {
      final fleet = FakeFleetRepository()..failures['list'] = const ApiUnreachable();

      await openFleet(tester, fleet);

      expect(find.byKey(const Key('fleet-failed')), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      // "You have no vehicles" and "we could not find out" are different things to be told.
      expect(find.byKey(const Key('fleet-empty')), findsNothing);

      await tester.tap(find.byKey(const Key('fleet-retry')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('list'), hasLength(2));
    });

    testWidgets('a failed refresh keeps the fleet on screen', (tester) async {
      // Somebody who pulled to refresh in a depot with no signal should still see their vehicles.
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(data: <Vehicle>[aVehicle(id: _van)]);

      await openFleet(tester, fleet);
      fleet.failures['list'] = const ApiUnreachable();

      await tester.fling(find.byKey(const Key('fleet')), const Offset(0, 400), 1000);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(inFleet(find.byKey(const Key('vehicle-$_van'))), findsOneWidget);
    });
  });

  group('paging', () {
    testWidgets('says it is showing part of the fleet, and asks for the rest', (tester) async {
      // A group holds the vehicles that have been read, not every vehicle in that state. A heading
      // with three trucks under it when there are five is a screen that has lied quietly.
      final fleet = FakeFleetRepository()
        ..page = ApiPage<Vehicle>(
          data: <Vehicle>[aVehicle(id: _van)],
          nextCursor: 'cursor-2',
          hasMore: true,
        );

      await openFleet(tester, fleet);

      expect(inFleet(find.byKey(const Key('fleet-partial'))), findsOneWidget);

      fleet.page = ApiPage<Vehicle>(data: <Vehicle>[aBareVehicle(id: _ute)]);
      await tester.tap(find.byKey(const Key('fleet-more')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('list').last.cursor, 'cursor-2');
      expect(inFleet(find.byKey(const Key('vehicle-$_van'))), findsOneWidget);
      expect(inFleet(find.byKey(const Key('vehicle-$_ute'))), findsOneWidget);
    });
  });

  group('adding a vehicle', () {
    Future<void> openAddForm(WidgetTester tester, FakeFleetRepository fleet) async {
      await openFleet(tester, fleet);
      await tester.tap(find.byKey(const Key('fleet-add')));
      await tester.pumpAndSettle();
    }

    Future<void> fillIn(
      WidgetTester tester, {
      String registration = 'abc 123',
      String make = '',
      String maxWeight = '',
    }) async {
      await tester.enterText(find.byKey(const Key('vehicle-registration')), registration);
      if (make.isNotEmpty) {
        await tester.enterText(find.byKey(const Key('vehicle-make')), make);
      }
      if (maxWeight.isNotEmpty) {
        await tester.enterText(find.byKey(const Key('vehicle-max-weight')), maxWeight);
      }
      await tester.pump();

      // The picker, opened and chosen from the way a person does. `hitTestable` because the open
      // menu and the closed button both hold a widget with this key, and only one of them is the
      // one a finger can reach — tapping the other lands wherever it happens to be laid out.
      await tester.tap(find.byKey(const Key('vehicle-type')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('vehicle-type-van')).hitTestable());
      await tester.pumpAndSettle();
    }

    testWidgets('a plate and a type are all the form insists on', (tester) async {
      // Docs/01 §4.2: a form that demanded the load height would be filled in with guesses.
      final fleet = FakeFleetRepository();

      await openAddForm(tester, fleet);
      await fillIn(tester);
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('add'), hasLength(1));
      expect(fleet.callsTo('add').single.fields['registration'], 'abc 123');
      expect(fleet.callsTo('add').single.fields['vehicle_type'], 'van');
    });

    testWidgets('the request carries an idempotency key', (tester) async {
      // Every state-changing request carries one (CLAUDE.md, SHIP-15). A phone that retries after a
      // dropped connection must not end up with two of the same truck.
      final fleet = FakeFleetRepository();

      await openAddForm(tester, fleet);
      await fillIn(tester);
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(fleet.keysFor('add').single, isNotEmpty);
    });

    testWidgets('the new vehicle is in the fleet on the way back', (tester) async {
      final fleet = FakeFleetRepository();

      await openAddForm(tester, fleet);
      await fillIn(tester);

      // What the platform answers with, once it has normalised what was sent.
      fleet.page = ApiPage<Vehicle>(data: <Vehicle>[aVehicle(id: _van, registration: 'ABC123')]);

      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('vehicle-form')), findsNothing);
      expect(inFleet(find.text('ABC123')), findsOneWidget);
      // Re-read rather than guessed at: the plate came back changed, and the list is what shows it.
      expect(fleet.callsTo('list'), hasLength(2));
    });

    testWidgets('an empty form is refused before a round trip is spent on it', (tester) async {
      // Presence is the app's half of the split; everything that is a rule rather than a shape is
      // the platform's.
      final fleet = FakeFleetRepository();

      await openAddForm(tester, fleet);
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('add'), isEmpty);
      expect(find.text('Enter the registration.'), findsOneWidget);
      expect(find.text('Choose what kind of vehicle this is.'), findsOneWidget);
    });

    testWidgets('a measurement that is not a number is refused as a shape', (tester) async {
      // A shape, not a bound. The maximum weight is a limit the platform holds; what this form
      // cannot do is turn "12 tonnes" into a JSON number.
      final fleet = FakeFleetRepository();

      await openAddForm(tester, fleet);
      await fillIn(tester, maxWeight: '12 tonnes');
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(fleet.callsTo('add'), isEmpty);
      expect(find.text('Enter a number.'), findsOneWidget);
    });

    testWidgets('the platform refusing a plate keeps the form and says why', (tester) async {
      // 409 fleet_duplicate_registration: a plate already in service in this fleet. Something the
      // provider corrects in the form they are looking at, so the form stays and stays filled in.
      final fleet = FakeFleetRepository()
        ..failures['add'] = const ApiErrorResponse(
          statusCode: 409,
          code: 'fleet_duplicate_registration',
          message: 'A vehicle with that registration is already in service in this fleet.',
          requestId: '9f2c',
        );

      await openAddForm(tester, fleet);
      await fillIn(tester);
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('vehicle-form')), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.textContaining('already in service'), findsOneWidget);
      // The request id, because somebody reporting a problem sends a screenshot.
      expect(find.textContaining('9f2c'), findsOneWidget);
    });

    testWidgets('a field the platform rejected is said under that field', (tester) async {
      // validation_failed carries one details entry per offending field, keyed by the contract's own
      // names, precisely so a form can do this.
      final fleet = FakeFleetRepository()
        ..failures['add'] = const ApiErrorResponse(
          statusCode: 422,
          code: 'validation_failed',
          message: 'One or more fields were rejected.',
          details: <ApiFieldError>[
            ApiFieldError(
              field: 'max_weight_kg',
              code: 'out_of_range',
              message: 'Enter a capacity between 0 and 100000 kilograms.',
            ),
          ],
        );

      await openAddForm(tester, fleet);
      await fillIn(tester, maxWeight: '999999');
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      // Under the input, and *not* in the banner: it is about a value somebody typed.
      expect(find.text('Enter a capacity between 0 and 100000 kilograms.'), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
    });

    testWidgets('a server message goes when the value it was about is corrected', (tester) async {
      // A message that outlives the value it was about sends somebody looking for a mistake they
      // have already fixed.
      final fleet = FakeFleetRepository()
        ..failures['add'] = const ApiErrorResponse(
          statusCode: 422,
          code: 'validation_failed',
          message: 'One or more fields were rejected.',
          details: <ApiFieldError>[
            ApiFieldError(
              field: 'registration',
              code: 'too_long',
              message: 'Use 12 characters or fewer.',
            ),
          ],
        );

      await openAddForm(tester, fleet);
      await fillIn(tester, registration: 'THIS-IS-FAR-TOO-LONG');
      await tester.tap(find.byKey(const Key('vehicle-submit')));
      await tester.pumpAndSettle();

      expect(find.text('Use 12 characters or fewer.'), findsOneWidget);

      await tester.enterText(find.byKey(const Key('vehicle-registration')), 'ABC123');
      await tester.pumpAndSettle();

      expect(find.text('Use 12 characters or fewer.'), findsNothing);
    });
  });
}
