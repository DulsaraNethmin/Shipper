import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:go_router/go_router.dart';
import 'package:shipper/core/auth/user_role.dart';

import '../../core/auth/session_fixtures.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_fleet_repository.dart';

/// Signs in as [role] and stops at the signed-in shell.
///
/// The role travels the way it travels in the application: as a claim in the access token the
/// sign-in answered with (SHIP-50, SHIP-52). Nothing here sets a role directly, because nothing in
/// the app can — a test that assigned one would be testing a mechanism the product does not have.
Future<void> signInAs(
  WidgetTester tester,
  UserRole role, {
  FakeFleetRepository? fleet,
}) async {
  // A phone-shaped surface rather than the 800×600 default, so a screen that scrolls is not
  // reported as overflowing.
  tester.view.physicalSize = const Size(800, 1600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: role);

  await tester.pumpWidget(signupApp(identity, fleet: fleet));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();
}

/// Signs a provider in and walks them to their fleet the way a person reaches it.
///
/// Through the button on the provider half of the shell, rather than by pumping the screen: the
/// route has to be reachable, and a guard that refused `/fleet/vehicles` — which is exactly what
/// SHIP-49's guard did to the first screen hung off the shell — fails here rather than only on a
/// device.
Future<void> openFleet(WidgetTester tester, FakeFleetRepository fleet) async {
  await signInAs(tester, UserRole.provider, fleet: fleet);

  await tester.tap(find.byKey(const Key('manage-vehicles')));
  await tester.pumpAndSettle();
}

/// Signs a provider in, opens the fleet, and taps through to the first vehicle in it.
Future<void> openVehicle(WidgetTester tester, FakeFleetRepository fleet, String id) async {
  await openFleet(tester, fleet);

  await tester.tap(find.byKey(Key('vehicle-$id')));
  await tester.pumpAndSettle();
}

/// Navigates to [location] the way a deep link does — with no button involved.
///
/// `Docs/07` §5 requires every notification to open the exact thing it concerns, and SHIP-145 will
/// deliver payloads straight to a route. So "there is no button" is not the same as "it cannot be
/// reached", and the surfaces have to answer for themselves when it is.
Future<void> deepLinkTo(WidgetTester tester, String location) async {
  GoRouter.of(tester.element(find.byType(Scaffold).first)).go(location);
  await tester.pumpAndSettle();
}
