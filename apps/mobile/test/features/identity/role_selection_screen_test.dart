// SHIP-52 — "Role is chosen during signup and drives the post-login shell".
//
// One sentence, two claims, and they meet in a place the client cannot yet join up on its own:
// the role is chosen here and sent to POST /v1/auth/register, and the shell reads it from the
// session, which is filled by an access token the platform signs (SHIP-37). The endpoint that
// issues one is SHIP-41, consumed by SHIP-55, and neither exists.
//
// So the two claims are tested as what they actually are today — "the chosen role is what
// registration sends" and "the session's role is what selects the shell" — plus the debug-only
// route between them that makes the pair demonstrable in one flow on a device.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_refresher.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';

import '../../core/auth/fake_token_store.dart';
import '../../core/auth/session_fixtures.dart';
import 'fake_identity_repository.dart';
import 'signup_app.dart';

/// Boots the app into a signed-in shell, which without a sign-in endpoint means one of two
/// things: a cold start with a token in the keychain and no role, or the debug-only affordance
/// that stores a placeholder and claims one.
///
/// `flutter test` runs in debug, so that affordance is present here; a release build tree-shakes
/// it and its placeholder string away entirely.
///
/// **The refresher is stubbed unreachable, and that is what keeps the no-role case reachable at
/// all.** SHIP-50 refreshes as soon as the keychain answers, and a successful refresh brings the
/// role with it — so "signed in and not yet knowing as whom" is now the window before that
/// answers, and the cold start that cannot reach the platform.
Future<ProviderContainer> _signedIn(WidgetTester tester, {UserRole? role}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        tokenStoreProvider.overrideWithValue(
          role == null ? FakeTokenStore(refreshToken: 'refresh-abc') : FakeTokenStore(),
        ),
        sessionRefresherProvider.overrideWithValue(
          FakeSessionRefresher()..failure = const ApiUnreachable(),
        ),
      ],
      child: const ShipperApp(),
    ),
  );
  await tester.pumpAndSettle();

  if (role != null) {
    await tester.tap(find.byKey(Key('development-session-${role.name}')));
    await tester.pumpAndSettle();
  }

  return ProviderScope.containerOf(tester.element(find.byType(MaterialApp)), listen: false);
}

void main() {
  group('the role is chosen during signup', () {
    testWidgets('and it is what registration sends', (tester) async {
      final identity = FakeIdentityRepository();
      await openRegistration(tester, identity, role: UserRole.provider);
      await fillRegistration(tester);

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(identity.bodiesFor('register').single['role'], 'provider');
    });

    testWidgets('the form shows it back, because it cannot be changed afterwards',
        (tester) async {
      // SHIP-45 fixes the role with a BEFORE UPDATE trigger on `users`, so the registration
      // screen is the last place a mistake is free to correct.
      final identity = FakeIdentityRepository();
      await openRegistration(tester, identity, role: UserRole.provider);

      expect(
        find.descendant(
          of: find.byKey(const Key('register-role')),
          matching: find.text('Provider'),
        ),
        findsOneWidget,
      );
    });

    testWidgets('and it can be changed from the form, up until the account exists',
        (tester) async {
      final identity = FakeIdentityRepository();
      await openRegistration(tester, identity, role: UserRole.provider);

      await tester.tap(find.byKey(const Key('register-change-role')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('role-customer')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('role-continue')));
      await tester.pumpAndSettle();

      await fillRegistration(tester);
      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(identity.bodiesFor('register').single['role'], 'customer');
    });

    testWidgets('the choice survives stepping back and forward', (tester) async {
      // The selection is held in the signup state rather than in the screen, which is what makes
      // this true. Local widget state would be gone the moment the route was rebuilt.
      final identity = FakeIdentityRepository();
      await openRegistration(tester, identity, role: UserRole.provider);

      await tester.tap(find.byKey(const Key('register-change-role')));
      await tester.pumpAndSettle();

      expect(
        find.descendant(
          of: find.byKey(const Key('role-provider')),
          matching: find.byIcon(Icons.radio_button_checked),
        ),
        findsOneWidget,
      );
    });

    testWidgets('the screen says the choice is permanent, before it is made', (tester) async {
      final identity = FakeIdentityRepository();
      await openSignup(tester, identity);

      expect(find.byKey(const Key('role-warning')), findsOneWidget);
      // Both halves are offered, and nothing else. UserRole.unknown exists for decoding a role
      // this build has never heard of; it is not something a person may ask to be.
      expect(find.byKey(const Key('role-customer')), findsOneWidget);
      expect(find.byKey(const Key('role-provider')), findsOneWidget);
      expect(find.byKey(const Key('role-unknown')), findsNothing);
    });
  });

  group('the role drives the post-login shell', () {
    testWidgets('a customer session lands in the customer half', (tester) async {
      await _signedIn(tester, role: UserRole.customer);

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
      expect(find.byKey(const Key('shell-provider')), findsNothing);
    });

    testWidgets('a provider session lands in the provider half', (tester) async {
      await _signedIn(tester, role: UserRole.provider);

      expect(find.byKey(const Key('shell-provider')), findsOneWidget);
      expect(find.byKey(const Key('shell-customer')), findsNothing);
    });

    testWidgets('a restored session with no role yet shows neither half', (tester) async {
      // What a cold start actually looks like: the keychain holds a refresh token and no role.
      // Guessing customer here would show every provider the wrong marketplace on every launch
      // until SHIP-50's first refresh — which is precisely the separation Docs/07 §1 requires.
      await _signedIn(tester);

      expect(find.byKey(const Key('shell-role-pending')), findsOneWidget);
      expect(find.byKey(const Key('shell-customer')), findsNothing);
      expect(find.byKey(const Key('shell-provider')), findsNothing);
    });

    testWidgets('a role this build does not recognise is a dead end that says so',
        (tester) async {
      // Not a crash, and not a guess. SHIP-167's version gate is what resolves it, which is why
      // the copy says "update the app" rather than "something went wrong".
      final container = await _signedIn(tester);

      await container.read(sessionProvider.notifier).signIn(aTokenPair(role: UserRole.unknown));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-role-unrecognised')), findsOneWidget);
    });
  });

  testWidgets('the end of signup can preview the shell the chosen role selects', (tester) async {
    // The debug-only bridge across the gap SHIP-55 closes. It is what makes "chosen at signup"
    // and "drives the shell" one demonstrable flow on a device rather than two separate facts.
    final identity = FakeIdentityRepository();
    await registerThrough(tester, identity, role: UserRole.provider);
    await verifyEmailThrough(tester);
    await verifyPhoneThrough(tester);

    await tester.tap(find.byKey(const Key('development-session-provider')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('shell-provider')), findsOneWidget);
  });
}
