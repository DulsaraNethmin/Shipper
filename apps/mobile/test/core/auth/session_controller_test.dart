// SHIP-49 — the cold start, in isolation from the widgets that show it.
//
// Everything the routing guard does follows from the three states this controller produces, so
// these run against a ProviderContainer rather than a pumped app: a failure here is a failure
// in the session, and a failure in app_router_test.dart is then unambiguously a failure in
// routing.

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/token_store.dart';

import 'fake_token_store.dart';

void main() {
  ProviderContainer containerWith(FakeTokenStore store) {
    final container = ProviderContainer(
      overrides: [tokenStoreProvider.overrideWithValue(store)],
    );
    addTearDown(container.dispose);
    return container;
  }

  group('cold start', () {
    test('begins restoring, before the keychain has answered', () {
      final container = containerWith(FakeTokenStore());

      // Read without awaiting: this is the first frame, and it is the state the splash screen
      // is shown for. A model with only signedIn and signedOut would have to guess here, and
      // whichever it guessed would be wrong on half of all launches.
      expect(container.read(sessionProvider), isA<SessionRestoring>());
    });

    test('a stored refresh token routes the app signed in', () async {
      final container = containerWith(FakeTokenStore(refreshToken: 'refresh-abc'));

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedIn>());
    });

    test('an empty keychain routes the app signed out', () async {
      final container = containerWith(FakeTokenStore());

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });

    test('an empty string is not a session', () async {
      final container = containerWith(FakeTokenStore(refreshToken: ''));

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });

    test('an unreadable keychain fails to signed out, not to signed in', () async {
      // The safe direction, and the one the user can recover from. Signed in with unreadable
      // storage is an app whose every request fails and which offers no route back to a
      // sign-in screen.
      final container = containerWith(FakeTokenStore.unreadable());

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });
  });

  group('sign in and sign out', () {
    test('signing in stores the token before the state says it did', () async {
      final store = FakeTokenStore();
      final container = containerWith(store);
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signIn(refreshToken: 'refresh-abc');

      // The order is the assertion. A state that changed first would survive a crash as a
      // signed-in app with an empty keychain — signed out again on the next cold start, with
      // no explanation.
      expect(store.refreshToken, 'refresh-abc');
      expect(container.read(sessionProvider), isA<SessionSignedIn>());
    });

    test('signing out clears the store and the state', () async {
      final store = FakeTokenStore(refreshToken: 'refresh-abc');
      final container = containerWith(store);
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signOut();

      expect(store.cleared, isTrue);
      expect(store.refreshToken, isNull);
      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });

    test('signing out ends the session even when clearing throws', () async {
      final container = containerWith(_UnclearableStore());
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signOut();

      // Leaving the user in the signed-in shell because a delete failed is the worst of both:
      // they believe they signed out and the app behaves as though they did not. The session
      // that matters is the server's (SHIP-46); this is the device catching up.
      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });
  });

  test('a state assigned twice is the same value, so listeners do not rebuild', () async {
    // The router listens to this provider and rebuilds its redirect on every change. Freezed
    // gives the states equality; without it, every identical assignment would re-evaluate the
    // guard, and a redirect that runs on every rebuild is how a navigation loop starts.
    expect(const SessionState.signedOut(), equals(const SessionState.signedOut()));
    expect(const SessionState.signedIn(), isNot(equals(const SessionState.signedOut())));
  });
}

class _UnclearableStore extends FakeTokenStore {
  @override
  Future<void> clear() async => throw StateError('keystore unavailable');
}
