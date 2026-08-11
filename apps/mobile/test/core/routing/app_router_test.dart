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
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/health/health_repository.dart';
import 'package:shipper/core/health/health_status.dart';
import 'package:shipper/core/routing/app_router.dart';

import '../auth/fake_token_store.dart';

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
      ],
      child: child,
    );
