import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/session_ender.dart';
import 'package:shipper/core/auth/session_refresher.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/device/device_label.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';
import 'package:shipper/features/fleet/fleet_repository.dart';
import 'package:shipper/features/identity/identity_repository.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';
import 'package:shipper/features/jobs/open_jobs_repository.dart';

import '../../core/auth/fake_token_store.dart';
import '../../core/auth/session_fixtures.dart';
import '../bidding/fake_bidding_repository.dart';
import '../fleet/fake_fleet_repository.dart';
import '../jobs/fake_jobs_repository.dart';
import '../jobs/fake_open_jobs_repository.dart';
import 'fake_identity_repository.dart';

/// The real app, with only the things a widget test cannot have.
///
/// The token store reaches a platform channel a widget test has no plugin behind, and the two
/// repositories would reach a socket. Everything between them — the router, the guard, the
/// session, the signup state, every screen — is the application's own, which is what makes these
/// tests demonstrations of the journey rather than of a widget in isolation.
///
/// Overridden on the **root** scope rather than by wrapping a screen in a second `ProviderScope`,
/// for the reason `main.dart` gives: a nested scope hands its subtree a private copy of every
/// provider, and a test that passes against one has not tested the app's wiring.
Widget signupApp(
  FakeIdentityRepository identity, {
  FakeTokenStore? store,
  FakeJobsRepository? jobs,
  FakeFleetRepository? fleet,
  FakeOpenJobsRepository? openJobs,
  FakeBiddingRepository? bidding,
  FakeSessionEnder? ender,
}) {
  return ProviderScope(
    overrides: [
      tokenStoreProvider.overrideWithValue(store ?? FakeTokenStore()),
      identityRepositoryProvider.overrideWithValue(identity),
      // Sign-out tells the platform the device session is over, and does not wait to be told
      // back. Every test that signs out fires it, so it is overridden here rather than in each —
      // the same hazard as the refresher below, and a socket opened from a `finally`-shaped path
      // is the one nobody notices.
      sessionEnderProvider.overrideWithValue(ender ?? FakeSessionEnder()),
      // The customer half reads that customer's jobs as soon as it is drawn (SHIP-76), and the
      // locations step writes one (SHIP-71). Either would otherwise open a socket to whatever is
      // listening on the local API port — nothing on CI, and on a developer's machine the API.
      // Same hazard as the refresher below, and the same symptom when it is forgotten.
      jobsRepositoryProvider.overrideWithValue(jobs ?? FakeJobsRepository()),
      // The fleet screens read the provider's own vehicles as soon as they are drawn (SHIP-98),
      // and three of the five things they do are writes. Same hazard, same symptom: a socket
      // opened by a screen nobody in a given test was thinking about is the one that goes
      // unnoticed until CI has no API to open it against.
      fleetRepositoryProvider.overrideWithValue(fleet ?? FakeFleetRepository()),
      // The **provider half of the shell** reads the eligible feed as soon as it is drawn
      // (SHIP-99), which is the same hazard one step earlier than the fleet's: every test that
      // signs a provider in reaches it, including the ones that are about the router or the
      // session and never mention a job.
      openJobsRepositoryProvider.overrideWithValue(openJobs ?? FakeOpenJobsRepository()),
      // Placing a bid is a **write**, and the one screen that makes it is reachable by a deep link
      // (SHIP-100). Nothing reads bidding on arrival, so this is the least likely of the four to
      // open a socket by accident — and it is overridden here for the same reason as the rest,
      // which is that "no test I was thinking about reaches it" is not a property anybody checks.
      biddingRepositoryProvider.overrideWithValue(bidding ?? FakeBiddingRepository()),
      // A restored session refreshes as soon as the keychain answers (SHIP-50). None of these
      // tests starts with a stored token, so nothing refreshes — but a test that later does
      // would otherwise open a socket to whatever is listening on the local API port.
      sessionRefresherProvider.overrideWithValue(FakeSessionRefresher()),
      // Pinned, because a login body is asserted against it and the real one is whatever the
      // host machine happens to be running.
      deviceLabelProvider.overrideWithValue(testDeviceLabel),
    ],
    child: const ShipperApp(),
  );
}

/// The device label every widget test signs in with.
const testDeviceLabel = 'iOS 17.0';

/// Boots the app and walks it from the signed-out shell to the role screen, which is where
/// signup starts (SHIP-52).
///
/// The route is reached the way a person reaches it, so a guard that bounced a signed-out user
/// out of the journey — which is what SHIP-49's guard did before SHIP-51 widened it — fails here
/// rather than only on a device.
Future<void> openSignup(WidgetTester tester, FakeIdentityRepository identity) async {
  await tester.pumpWidget(signupApp(identity));
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('create-account')));
  await tester.pumpAndSettle();
}

/// Walks on to the registration form, choosing [role] on the way when one is given.
Future<void> openRegistration(
  WidgetTester tester,
  FakeIdentityRepository identity, {
  UserRole? role,
}) async {
  await openSignup(tester, identity);

  if (role != null) {
    await tester.tap(find.byKey(Key('role-${role.name}')));
    await tester.pumpAndSettle();
  }

  await tester.tap(find.byKey(const Key('role-continue')));
  await tester.pumpAndSettle();
}

/// Registers through the form, which is how the verification screens are reached with an account
/// in hand — the journey's own route, rather than a shortcut past the screen that creates it.
Future<void> registerThrough(WidgetTester tester, FakeIdentityRepository identity,
    {UserRole? role}) async {
  await openRegistration(tester, identity, role: role);
  await fillRegistration(tester);
  await tester.tap(find.byKey(const Key('register-submit')));
  await tester.pumpAndSettle();
}

/// Fills the registration form. Values default to ones both this device and the platform accept.
Future<void> fillRegistration(
  WidgetTester tester, {
  String email = 'alice@example.com',
  String phone = '0412 345 678',
  String password = 'correct-horse-battery-staple',
}) async {
  await tester.enterText(find.byKey(const Key('register-email')), email);
  await tester.enterText(find.byKey(const Key('register-phone')), phone);
  await tester.enterText(find.byKey(const Key('register-password')), password);
  await tester.pump();
}

/// Confirms the email address, which is the step between registration and whatever follows it.
Future<void> verifyEmailThrough(
  WidgetTester tester, {
  String token = '9qE2vT7bYw1sJk4pNc0aRlX8oZgHdM3uQiV6yB5tCfE',
}) async {
  await tester.enterText(find.byKey(const Key('verify-email-token')), token);
  await tester.tap(find.byKey(const Key('verify-email-submit')));
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('verify-email-continue')));
  await tester.pumpAndSettle();
}

/// Signs in through the form on the signed-out screen (SHIP-55).
///
/// The values default to ones both this device's validators and the platform accept. What the
/// sign-in actually answers with is [FakeIdentityRepository.tokens].
Future<void> signInThrough(
  WidgetTester tester, {
  String email = 'alice@example.com',
  String password = 'correct-horse-battery-staple',
}) async {
  await tester.enterText(find.byKey(const Key('sign-in-email')), email);
  await tester.enterText(find.byKey(const Key('sign-in-password')), password);
  await tester.pump();

  await tester.tap(find.byKey(const Key('sign-in')));
  await tester.pumpAndSettle();
}

/// Confirms the mobile number, which is the last step before the journey's end.
Future<void> verifyPhoneThrough(WidgetTester tester, {String code = '408213'}) async {
  await tester.enterText(find.byKey(const Key('verify-phone-code')), code);
  await tester.tap(find.byKey(const Key('verify-phone-submit')));
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('verify-phone-continue')));
  await tester.pumpAndSettle();
}
