// SHIP-55 and SHIP-50, on a device and against a running platform.
//
// Everything under test/ substitutes the identity repository and the session refresher, which is
// the right seam for a screen test and is not the same claim as "an existing user can sign in and
// lands in the correct role shell". This file makes that claim: the production widget tree, the
// production `dio` client, the production Keychain wrapper, real HTTP to a real service, and a
// role that came out of a token the platform signed.
//
// # Running it
//
// It needs a booted simulator *and* the stack and API this worktree runs, so it is deliberately
// out of `make flutter-check` and out of CHECKS — the Flutter CI job is a Linux runner with no
// simulator and no service. From the repository root:
//
//     make up && make migrate-up && make run          # the API on this worktree's HTTP_PORT
//
//     # An account to sign in as. Registration is the app's own journey (signup_test.dart);
//     # this one only needs credentials that already exist, so curl is the shorter route:
//     curl -s -X POST localhost:<port>/v1/auth/register \
//       -H "Idempotency-Key: $(uuidgen)" -H 'Content-Type: application/json' \
//       -d '{"email":"<fresh>","phone":"<fresh>","password":"correct-horse-battery-staple",
//            "role":"provider"}'
//
//     cd apps/mobile && flutter test integration_test/sign_in_test.dart \
//       -d <device> \
//       --dart-define=SHIPPER_API_PORT=<HTTP_PORT> \
//       --dart-define=SHIPPER_DEMO_EMAIL=<that address> \
//       --dart-define=SHIPPER_DEMO_PASSWORD=correct-horse-battery-staple
//
// # The two tests are one story and run in order
//
// The first signs in. The second cold-starts a *new* provider container over the same real
// Keychain — which is what a relaunch is — and lets SHIP-50's first refresh happen against the
// real `POST /v1/auth/refresh`. The role in the second shell therefore came from a **different**
// access token than the first: the one the refresh issued, after the platform rotated the stored
// refresh token. That is the part no host test can claim.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/token_store.dart';

/// An account that already exists on the platform, supplied from outside.
///
/// Not registered by this file: `uq_users_email` refuses a second account for the same address,
/// so a self-registering test is a test that passes once.
const _email = String.fromEnvironment('SHIPPER_DEMO_EMAIL', defaultValue: '');
const _password = String.fromEnvironment('SHIPPER_DEMO_PASSWORD', defaultValue: '');

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  // The production store, with the production options. Substituting anything here would put the
  // test back where the host tests already are.
  const store = SecureTokenStore();

  setUpAll(() {
    expect(
      _email.isNotEmpty && _password.isNotEmpty,
      isTrue,
      reason: 'Pass --dart-define=SHIPPER_DEMO_EMAIL and SHIPPER_DEMO_PASSWORD — see the '
          'header of this file. The account has to exist on the platform already.',
    );
  });

  testWidgets('an existing user signs in and lands in the correct role shell', (tester) async {
    await store.clear();

    await tester.pumpWidget(const ProviderScope(child: ShipperApp()));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('shell-signed-out')), findsOneWidget);

    await tester.enterText(find.byKey(const Key('sign-in-email')), _email);
    await tester.enterText(find.byKey(const Key('sign-in-password')), _password);
    await tester.pump();

    await tester.tap(find.byKey(const Key('sign-in')));
    await tester.pumpAndSettle();

    // The role came out of the access token the platform signed for this account, through the
    // session, through the router's guard, and out as a screen. Nothing in the app was told it.
    expect(find.byKey(const Key('shell-provider')), findsOneWidget);
    expect(find.byKey(const Key('shell-customer')), findsNothing);

    // And the refresh token is in the real Keychain, which is what makes the next test a cold
    // start rather than a continuation.
    expect(await store.readRefreshToken(), isNotNull);
  });

  testWidgets('a relaunch refreshes against the platform and the role arrives with it',
      (tester) async {
    final stored = await store.readRefreshToken();
    expect(stored, isNotNull, reason: 'run the sign-in test first, in the same invocation');

    // A new ProviderScope is a new session controller reading the same real Keychain, which is
    // what a relaunch is.
    await tester.pumpWidget(const ProviderScope(child: ShipperApp()));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('shell-provider')), findsOneWidget);

    // The rotation is the proof that a real refresh happened rather than a cached anything: the
    // token presented is dead, and the one now stored is the replacement (SHIP-39, SHIP-42).
    expect(await store.readRefreshToken(), isNot(stored));

    await store.clear();
  });
}
