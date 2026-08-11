import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/features/identity/identity_repository.dart';

import '../../core/auth/fake_token_store.dart';
import 'fake_identity_repository.dart';

/// The real app, with the two things a widget test cannot have.
///
/// The token store reaches a platform channel a widget test has no plugin behind, and the
/// identity repository would reach a socket. Everything between them — the router, the guard,
/// the session, the signup state, every screen — is the application's own, which is what makes
/// these tests demonstrations of the journey rather than of a widget in isolation.
///
/// Overridden on the **root** scope rather than by wrapping a screen in a second `ProviderScope`,
/// for the reason `main.dart` gives: a nested scope hands its subtree a private copy of every
/// provider, and a test that passes against one has not tested the app's wiring.
Widget signupApp(FakeIdentityRepository identity, {FakeTokenStore? store}) {
  return ProviderScope(
    overrides: [
      tokenStoreProvider.overrideWithValue(store ?? FakeTokenStore()),
      identityRepositoryProvider.overrideWithValue(identity),
    ],
    child: const ShipperApp(),
  );
}

/// Boots the app and walks it from the signed-out shell into the registration form.
///
/// The route is reached the way a person reaches it, so a guard that bounced a signed-out user
/// off the registration screen — which is what SHIP-49's guard did before SHIP-51 widened it —
/// fails here rather than only on a device.
Future<void> openRegistration(WidgetTester tester, FakeIdentityRepository identity) async {
  await tester.pumpWidget(signupApp(identity));
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('create-account')));
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
