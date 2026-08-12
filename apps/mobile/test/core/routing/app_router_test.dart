// SHIP-49 — "App routes to signed-in or signed-out shell correctly on cold start".
//
// Two halves, because the *Done when* line has two.
//
// The first is the guard as a table. `redirectFor` is a pure function of the session and the
// location, and every routing defect of this shape is a cell in that table nobody considered —
// so the cells are written out, including the ones that should redirect nowhere.
//
// The second is a real cold start: a ProviderScope with a token in the store, or without one,
// pumped from the same widget main() builds. That is what SHIP-49 actually claims, and a table
// test alone would pass with the guard wired to nothing.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/session_refresher.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/health/health_repository.dart';
import 'package:shipper/core/health/health_status.dart';
import 'package:shipper/core/routing/app_router.dart';

import '../auth/fake_token_store.dart';
import '../auth/session_fixtures.dart';

void main() {
  group('the guard, as a table', () {
    test('restoring holds the splash and sends everything else to it', () {
      const restoring = SessionState.restoring();

      expect(redirectFor(restoring, Routes.starting), isNull);
      expect(redirectFor(restoring, Routes.signIn), Routes.starting);
      expect(redirectFor(restoring, Routes.home), Routes.starting);
    });

    test('signed out reaches the signed-out shell and nothing else', () {
      const signedOut = SessionState.signedOut();

      expect(redirectFor(signedOut, Routes.signIn), isNull);
      expect(redirectFor(signedOut, Routes.home), Routes.signIn);
      expect(redirectFor(signedOut, Routes.starting), Routes.signIn);
    });

    test('the whole signup journey is reachable while signed out', () {
      // POST /v1/auth/register returns an account and no token — registering is not signing in
      // — so every screen in the journey runs with no session at all. SHIP-49's guard sent a
      // signed-out user to the sign-in shell from every other location, which would have
      // bounced them straight back out of registration on the first redirect.
      const signedOut = SessionState.signedOut();

      for (final location in [Routes.register, Routes.registered]) {
        expect(redirectFor(signedOut, location), isNull, reason: location);
      }
    });

    test('a signed-in user has no business in the signup journey', () {
      const signedIn = SessionState.signedIn();

      for (final location in [Routes.register, Routes.registered]) {
        expect(redirectFor(signedIn, location), Routes.home, reason: location);
      }
    });

    test('signed in reaches the signed-in shell and nothing else', () {
      const signedIn = SessionState.signedIn();

      expect(redirectFor(signedIn, Routes.home), isNull);
      expect(redirectFor(signedIn, Routes.signIn), Routes.home);
      expect(redirectFor(signedIn, Routes.starting), Routes.home);
    });

    test('the connectivity screen is reachable from either shell, and during the restore', () {
      // SHIP-19's demonstration. Putting it behind the session would have made "the app can
      // reach the API" unanswerable on a fresh install, which is exactly when it is asked.
      expect(redirectFor(const SessionState.restoring(), Routes.health), isNull);
      expect(redirectFor(const SessionState.signedOut(), Routes.health), isNull);
      expect(redirectFor(const SessionState.signedIn(), Routes.health), isNull);
    });
  });

  group('a deep link that arrives while the keychain is being read', () {
    // SHIP-49 wrote this case down as dropped and named SHIP-143 as the ticket that would have
    // to fix it. SHIP-53 got there first: a verification link is opened at a cold start, which
    // is exactly when the session is restoring, so for that link it is not a rare case but the
    // only case.

    test('is held, and delivered once the session answers', () {
      final redirector = Redirector();
      final link = Uri.parse('/verify-email?token=abc');

      // The restore is still in flight, so the app cannot show it yet.
      expect(redirector(const SessionState.restoring(), link), Routes.starting);
      expect(redirector(const SessionState.restoring(), Uri.parse(Routes.starting)), isNull);

      // The keychain answers, and the link is reissued — with its query, which is the whole
      // point: the screen without the token is worse than not arriving at all.
      expect(
        redirector(const SessionState.signedOut(), Uri.parse(Routes.starting)),
        '/verify-email?token=abc',
      );
    });

    test('is still put through the guard rather than exempted by being held', () {
      final redirector = Redirector();

      expect(redirector(const SessionState.restoring(), Uri.parse(Routes.home)), Routes.starting);
      expect(
        redirector(const SessionState.signedOut(), Uri.parse(Routes.starting)),
        Routes.signIn,
        reason: 'a link into the signed-in shell on a signed-out device is still refused',
      );
    });

    test('is delivered once and not again on a later sign-out', () {
      final redirector = Redirector();

      redirector(const SessionState.restoring(), Uri.parse('/verify-email?token=abc'));
      redirector(const SessionState.signedOut(), Uri.parse(Routes.starting));

      // Somebody signs in, then out. Reissuing the held link here would take them back to a
      // screen they finished with, minutes later, for no reason they could see.
      expect(redirector(const SessionState.signedOut(), Uri.parse(Routes.starting)), Routes.signIn);
    });

    test('an ordinary cold start with no link behaves exactly as it did', () {
      final redirector = Redirector();

      expect(redirector(const SessionState.restoring(), Uri.parse(Routes.starting)), isNull);
      expect(redirector(const SessionState.signedOut(), Uri.parse(Routes.starting)), Routes.signIn);
    });
  });

  group('cold start', () {
    testWidgets('a stored refresh token lands in the signed-in shell', (tester) async {
      await tester.pumpWidget(_app(FakeTokenStore(refreshToken: 'refresh-abc')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-signed-in')), findsOneWidget);
      expect(find.byKey(const Key('shell-signed-out')), findsNothing);
    });

    testWidgets('an empty keychain lands in the signed-out shell', (tester) async {
      await tester.pumpWidget(_app(FakeTokenStore()));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-signed-out')), findsOneWidget);
      expect(find.byKey(const Key('shell-signed-in')), findsNothing);
    });

    testWidgets('the first frame is the splash, not either shell', (tester) async {
      await tester.pumpWidget(_app(FakeTokenStore(refreshToken: 'refresh-abc')));

      // One pump, no settle: the keychain read has been started and has not answered. Landing
      // on a shell here would mean the app had guessed, and the guess is visible to the user
      // as the sign-in screen appearing and being snatched away.
      expect(find.byKey(const Key('session-restoring')), findsOneWidget);
      expect(find.byKey(const Key('shell-signed-in')), findsNothing);
      expect(find.byKey(const Key('shell-signed-out')), findsNothing);

      await tester.pumpAndSettle();
    });
  });

  group('the session moves the app between shells', () {
    testWidgets('signing out from the signed-in shell returns to signed out', (tester) async {
      await tester.pumpWidget(_app(FakeTokenStore(refreshToken: 'refresh-abc')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('sign-out')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-signed-out')), findsOneWidget);
    });

    testWidgets('storing a session moves the app to the signed-in shell', (tester) async {
      // The debug-only affordance on the signed-out screen. It is what makes the cold-start
      // criterion demonstrable on a simulator in a wave with no authentication endpoint —
      // flutter test runs in debug, so it is present here.
      final store = FakeTokenStore();
      await tester.pumpWidget(_app(store));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('development-session')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-signed-in')), findsOneWidget);
      expect(store.refreshToken, isNotNull);
    });

    testWidgets('the router survives a session change rather than being rebuilt',
        (tester) async {
      // A GoRouter rebuilt on every session change discards the navigator and its history,
      // which is silent until something depends on a back stack. refreshListenable is what
      // avoids it, and this is the assertion that notices if somebody replaces it with a
      // ref.watch.
      final store = FakeTokenStore(refreshToken: 'refresh-abc');
      late final ProviderContainer container;

      await tester.pumpWidget(
        _scope(
          store,
          child: Builder(
            builder: (context) {
              container = ProviderScope.containerOf(context, listen: false);
              return const ShipperApp();
            },
          ),
        ),
      );
      await tester.pumpAndSettle();

      final before = container.read(routerProvider);
      await tester.tap(find.byKey(const Key('sign-out')));
      await tester.pumpAndSettle();

      expect(identical(container.read(routerProvider), before), isTrue);
      expect(find.byKey(const Key('shell-signed-out')), findsOneWidget);
    });
  });
}

Widget _app(FakeTokenStore store) => _scope(store, child: const ShipperApp());

Widget _scope(FakeTokenStore store, {required Widget child}) => ProviderScope(
      overrides: [
        tokenStoreProvider.overrideWithValue(store),
        // The connectivity screen is reachable from both shells, and its provider would
        // otherwise attempt a real request from a widget test.
        healthProvider.overrideWith(
          (ref) => const HealthStatus(status: 'ok', version: 'v0.0.0-test'),
        ),
        // A restored session refreshes as soon as the keychain answers (SHIP-50). Without this
        // every cold-start test here would open a socket to whatever is listening on the local
        // API port — which is nothing on CI and, on a developer's machine, is the API.
        sessionRefresherProvider.overrideWithValue(FakeSessionRefresher()),
      ],
      child: child,
    );
