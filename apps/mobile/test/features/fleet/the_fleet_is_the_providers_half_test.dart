// SHIP-98 — the fleet is the provider's half, and a customer does not reach it.
//
// Docs/07 §1 requires the customer and provider halves to be genuinely separate inside the one app:
// "a customer should never see provider surfaces or the reverse". The fleet is the first provider
// surface, so this file is where that requirement stops being a statement about the shell.
//
// # Two mechanisms, and they are not the same mechanism
//
// The app **hides**: no entry point on a customer's shell, and the fleet screens draw an explanation
// rather than a fleet. The platform **decides**: POST /v1/fleet/vehicles answers 403
// fleet_provider_only against users.role in the database, not against the role claim in the token.
// CLAUDE.md and Docs/07 §3 keep the second on the platform, and nothing in this file may be read as
// a control — a build with ProviderOnly deleted would show a customer these screens and change
// nothing whatever about what they could do with them.
//
// # And the reason hiding is not merely tidy here
//
// GET /v1/fleet/vehicles does not check the caller's role. It answers a customer 200 with an empty
// page, because a customer owns no vehicles and there is nothing to withhold. So a customer who
// reached this screen would see an empty fleet, an "Add a vehicle" button, a form to fill in, and a
// 403 only at the end of all of it. That is why the surface says so at the start, and why the test
// below asserts that **no request is made** on a customer's behalf.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/routing/app_router.dart';

import 'fake_fleet_repository.dart';
import 'fleet_app.dart';

void main() {
  group('the shell', () {
    testWidgets('offers a provider the way into their fleet', (tester) async {
      await signInAs(tester, UserRole.provider, fleet: FakeFleetRepository());

      expect(find.byKey(const Key('manage-vehicles')), findsOneWidget);
    });

    testWidgets('offers a customer no way in', (tester) async {
      await signInAs(tester, UserRole.customer, fleet: FakeFleetRepository());

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
      expect(find.byKey(const Key('manage-vehicles')), findsNothing);
    });

    testWidgets('offers no way in before the role has arrived', (tester) async {
      // A restored cold start knows it is signed in and not yet as whom. Guessing either way would
      // show somebody the wrong half of the marketplace.
      await signInAs(tester, UserRole.unknown, fleet: FakeFleetRepository());

      expect(find.byKey(const Key('manage-vehicles')), findsNothing);
    });
  });

  group('a customer who reaches the route anyway', () {
    testWidgets('is told whose surface it is, and no request is made', (tester) async {
      final fleet = FakeFleetRepository();

      await signInAs(tester, UserRole.customer, fleet: fleet);
      await deepLinkTo(tester, Routes.fleet);

      expect(find.byKey(const Key('provider-only')), findsOneWidget);
      expect(find.byKey(const Key('fleet')), findsNothing);
      // Nothing was asked of the platform on their behalf: the fleet controller is never built,
      // because the widget that watches it is never mounted.
      expect(fleet.calls, isEmpty);
    });

    testWidgets('is refused the add form the same way', (tester) async {
      final fleet = FakeFleetRepository();

      await signInAs(tester, UserRole.customer, fleet: fleet);
      await deepLinkTo(tester, Routes.newVehicle);

      expect(find.byKey(const Key('provider-only')), findsOneWidget);
      expect(find.byKey(const Key('vehicle-form')), findsNothing);
      expect(fleet.calls, isEmpty);
    });

    testWidgets('is refused one vehicle by id the same way', (tester) async {
      // The identifier-bearing route, which is the one a notification payload would carry.
      final fleet = FakeFleetRepository();

      await signInAs(tester, UserRole.customer, fleet: fleet);
      await deepLinkTo(tester, Routes.vehicleDetailFor('0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0'));

      expect(find.byKey(const Key('provider-only')), findsOneWidget);
      expect(find.byKey(const Key('vehicle-detail')), findsNothing);
      expect(fleet.calls, isEmpty);
    });

    testWidgets('is given a way back to their own half', (tester) async {
      // A dead end that a customer can only escape by force-quitting is worse than the screen they
      // should not have reached.
      await signInAs(tester, UserRole.customer, fleet: FakeFleetRepository());
      await deepLinkTo(tester, Routes.fleet);

      await tester.tap(find.byKey(const Key('provider-only-home')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
    });

    testWidgets('is told the account type is fixed, not offered a switch', (tester) async {
      // The role is fixed at registration and a database trigger enforces it (SHIP-45), so "change
      // your account type" is advice that cannot be followed and must not be implied.
      await signInAs(tester, UserRole.customer, fleet: FakeFleetRepository());
      await deepLinkTo(tester, Routes.fleet);

      expect(find.textContaining('fixed when it is created'), findsOneWidget);
    });
  });

  group('an account type this build does not recognise', () {
    testWidgets('is told to update rather than shown a fleet', (tester) async {
      final fleet = FakeFleetRepository();

      await signInAs(tester, UserRole.unknown, fleet: fleet);
      await deepLinkTo(tester, Routes.fleet);

      expect(find.byKey(const Key('provider-only-role-unrecognised')), findsOneWidget);
      expect(fleet.calls, isEmpty);
    });
  });

  group('the router', () {
    testWidgets('does not decide who may be where', (tester) async {
      // The guard is deliberately blind to the role. A role-aware redirect would be an
      // authorisation control on the device, and — the bug it would actually have caused — the role
      // is null for the first round trip of a restored cold start, so it would bounce a provider off
      // their own fleet every time they opened the app from a notification.
      //
      // What that means concretely: a customer *arrives* at the route and is answered by the screen.
      await signInAs(tester, UserRole.customer, fleet: FakeFleetRepository());
      await deepLinkTo(tester, Routes.fleet);

      // Not redirected home — the fleet's own app bar is what they are looking at.
      expect(find.text('Your vehicles'), findsOneWidget);
      expect(find.byKey(const Key('shell-customer')), findsNothing);
    });
  });
}
