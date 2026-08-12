// SHIP-49 and SHIP-50 — the cold start and the refresh, in isolation from the widgets that show
// them.
//
// Everything the routing guard does follows from the three states this controller produces, so
// these run against a ProviderContainer rather than a pumped app: a failure here is a failure
// in the session, and a failure in app_router_test.dart is then unambiguously a failure in
// routing.
//
// What runs end to end through the real transport — a 401, the replay, and several concurrent
// failures producing one refresh — is core/api/auth_interceptor_test.dart. This file is about
// what the session does on its own.

import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_ender.dart';
import 'package:shipper/core/auth/session_refresher.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';

import 'fake_token_store.dart';
import 'session_fixtures.dart';

void main() {
  // The ender is a parameter rather than a returned field, deliberately: adding it to the record
  // would change the shape every `final (:container, refresher: _)` in this file destructures,
  // and sixteen edited lines to observe one call in two tests is the wrong trade. A test that
  // cares constructs its own and keeps the reference.
  ({ProviderContainer container, FakeSessionRefresher refresher}) containerWith(
    FakeTokenStore store, {
    FakeSessionRefresher? refresher,
    FakeSessionEnder? ender,
  }) {
    final refresh = refresher ?? FakeSessionRefresher();
    final container = ProviderContainer(
      overrides: [
        tokenStoreProvider.overrideWithValue(store),
        sessionRefresherProvider.overrideWithValue(refresh),
        // Sign-out now tells the platform (`POST /v1/auth/logout`). Without this override the
        // fire-and-forget call would open a socket to whatever is listening on the local API
        // port — nothing on CI, and on a developer's machine the API.
        sessionEnderProvider.overrideWithValue(ender ?? FakeSessionEnder()),
      ],
    );
    addTearDown(container.dispose);
    return (container: container, refresher: refresh);
  }

  group('cold start', () {
    test('begins restoring, before the keychain has answered', () {
      final (:container, refresher: _) = containerWith(FakeTokenStore());

      // Read without awaiting: this is the first frame, and it is the state the splash screen
      // is shown for. A model with only signedIn and signedOut would have to guess here, and
      // whichever it guessed would be wrong on half of all launches.
      expect(container.read(sessionProvider), isA<SessionRestoring>());
    });

    test('a stored refresh token routes the app signed in', () async {
      final (:container, refresher: _) = containerWith(FakeTokenStore(refreshToken: 'refresh-1'));

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedIn>());
    });

    test('an empty keychain routes the app signed out', () async {
      final (:container, refresher: _) = containerWith(FakeTokenStore());

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });

    test('an empty string is not a session', () async {
      final (:container, refresher: _) = containerWith(FakeTokenStore(refreshToken: ''));

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });

    test('an unreadable keychain fails to signed out, not to signed in', () async {
      // The safe direction, and the one the user can recover from. Signed in with unreadable
      // storage is an app whose every request fails and which offers no route back to a
      // sign-in screen.
      final (:container, refresher: _) = containerWith(FakeTokenStore.unreadable());

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });
  });

  group('the first refresh, which is what a restored session is missing', () {
    test('happens as soon as the keychain answers, and brings the role with it', () async {
      // The keychain holds a refresh token and no role — the role is a claim in the access token
      // the platform signs. Without this the shell would sit on "signed in, role not yet known"
      // until something happened to make a request, which in M1 is nothing at all.
      final refresher = FakeSessionRefresher()
        ..pairs = [aTokenPair(role: UserRole.provider, refreshToken: 'refresh-2')];
      final store = FakeTokenStore(refreshToken: 'refresh-1');
      final (:container, refresher: _) = containerWith(store, refresher: refresher);

      await container.read(sessionProvider.notifier).restored;

      expect(refresher.presented, ['refresh-1']);
      expect(container.read(sessionProvider), const SessionState.signedIn(role: UserRole.provider));
    });

    test('stores the rotated token, because the one presented is already dead', () async {
      final store = FakeTokenStore(refreshToken: 'refresh-1');
      final (:container, refresher: _) = containerWith(
        store,
        refresher: FakeSessionRefresher()..pairs = [aTokenPair(refreshToken: 'refresh-2')],
      );

      await container.read(sessionProvider.notifier).restored;

      expect(store.refreshToken, 'refresh-2');
    });

    test('does not happen at all when the device is signed out', () async {
      final (:container, :refresher) = containerWith(FakeTokenStore());

      await container.read(sessionProvider.notifier).restored;

      expect(refresher.calls, 0, reason: 'there is nothing to refresh with');
    });

    test('does not hold the splash: the shell is shown before the network answers', () async {
      // Docs/07 §4 is built on connections that are sometimes not there. A cold start that
      // waited for a round trip would be as slow as the signal, every time — and offline it
      // would be as slow as the connect timeout.
      final gate = Completer<void>();
      final refresher = FakeSessionRefresher()..gate = gate;
      final (:container, refresher: _) = containerWith(
        FakeTokenStore(refreshToken: 'refresh-1'),
        refresher: refresher,
      );

      // Reading is what builds the provider, which is what starts the cold start.
      expect(container.read(sessionProvider), isA<SessionRestoring>());

      for (var turn = 0; turn < 50 && refresher.calls == 0; turn++) {
        await Future<void>.delayed(Duration.zero);
      }

      // The refresh is still in flight — the fake is holding it — and the app is already in the
      // shell rather than on the splash.
      expect(refresher.calls, 1);
      expect(container.read(sessionProvider), isA<SessionSignedIn>());

      gate.complete();
      await container.read(sessionProvider.notifier).restored;
    });

    test('a refused refresh signs the device out', () async {
      // identity_refresh_token_invalid: the platform saw the token and will not honour it. The
      // session is over and the stored token is worth nothing.
      final store = FakeTokenStore(refreshToken: 'refresh-1');
      final (:container, refresher: _) = containerWith(
        store,
        refresher: FakeSessionRefresher()
          ..failure = const ApiErrorResponse(
            statusCode: 400,
            code: 'identity_refresh_token_invalid',
            message: 'Sign in again.',
          ),
      );

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
      expect(store.refreshToken, isNull);
    });

    test('an unreachable platform does not', () async {
      final store = FakeTokenStore(refreshToken: 'refresh-1');
      final (:container, refresher: _) = containerWith(
        store,
        refresher: FakeSessionRefresher()..failure = const ApiUnreachable(),
      );

      await container.read(sessionProvider.notifier).restored;

      expect(container.read(sessionProvider), isA<SessionSignedIn>());
      expect(store.refreshToken, 'refresh-1');
    });
  });

  group('sign in and sign out', () {
    test('signing in stores the token before the state says it did', () async {
      final store = FakeTokenStore();
      final (:container, refresher: _) = containerWith(store);
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signIn(aTokenPair(refreshToken: 'refresh-1'));

      // The order is the assertion. A state that changed first would survive a crash as a
      // signed-in app with an empty keychain — signed out again on the next cold start, with
      // no explanation.
      expect(store.refreshToken, 'refresh-1');
      expect(container.read(sessionProvider), isA<SessionSignedIn>());
    });

    test('signing in takes the role from the access token, not from the caller', () async {
      // The platform fixes the role at registration and signs it into the token (SHIP-37,
      // SHIP-45). Reading it here is reading the platform's answer rather than the client's
      // belief about it — and there is no second copy to disagree with later.
      final (:container, refresher: _) = containerWith(FakeTokenStore());
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signIn(aTokenPair(role: UserRole.provider));

      expect(container.read(sessionProvider), const SessionState.signedIn(role: UserRole.provider));
    });

    test('the role is not written to the keychain', () async {
      // A stored role is a second copy that every later refresh would have to agree with, and
      // the keychain is not where the platform's claims live.
      final store = FakeTokenStore();
      final (:container, refresher: _) = containerWith(store);
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signIn(
            aTokenPair(role: UserRole.provider, refreshToken: 'refresh-1'),
          );

      expect(store.refreshToken, 'refresh-1');
      expect(store.written, ['refresh-1'], reason: 'one write, and it is the token');
    });

    test('signing out clears the store, the state and the access token', () async {
      final store = FakeTokenStore(refreshToken: 'refresh-1');
      final (:container, refresher: _) = containerWith(store);
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signOut();

      expect(store.cleared, isTrue);
      expect(store.refreshToken, isNull);
      expect(container.read(sessionProvider.notifier).accessToken, isNull);
      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });

    test('signing out ends the session even when clearing throws', () async {
      final (:container, refresher: _) = containerWith(_UnclearableStore());
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signOut();

      // Leaving the user in the signed-in shell because a delete failed is the worst of both:
      // they believe they signed out and the app behaves as though they did not. The session
      // that matters is the server's (SHIP-46); this is the device catching up.
      expect(container.read(sessionProvider), isA<SessionSignedOut>());
    });
  });

  group('signing out tells the platform', () {
    // Docs/11 §9: `POST /v1/auth/logout` was never called, so the refresh token the device
    // discarded stayed valid server-side for up to thirty days and the handset kept a row in
    // `GET /v1/auth/sessions`. Closing it needed SHIP-50 — before it there was no access token
    // to authenticate the call with.

    test('with the access token it is in the act of discarding', () async {
      // Authenticated with the token being thrown away, spent on the request that makes throwing
      // it away mean something. The `sid` claim in it is what names the session to end, which is
      // why the request has no body and no session identifier of its own.
      final ender = FakeSessionEnder();
      final (:container, refresher: _) =
          containerWith(FakeTokenStore(refreshToken: 'refresh-1'), ender: ender);
      await container.read(sessionProvider.notifier).restored;

      final held = container.read(sessionProvider.notifier).accessToken;
      await container.read(sessionProvider.notifier).signOut();
      await Future<void>.delayed(Duration.zero);

      expect(ender.calls, 1);
      expect(ender.presented.single, held);
      expect(ender.presented.single, isNotNull);
      expect(ender.keys.single, isNotEmpty);
    });

    test('and does not wait to be told back', () async {
      // Fire-and-forget, and it must stay that way. Docs/07 §3 has the device catching up rather
      // than asking permission, and a sign-out that failed because a train went into a tunnel
      // would be a defect rather than a safeguard.
      final ender = FakeSessionEnder()..failure = const ApiUnreachable();
      final store = FakeTokenStore(refreshToken: 'refresh-1');
      final (:container, refresher: _) = containerWith(store, ender: ender);
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signOut();
      await Future<void>.delayed(Duration.zero);

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
      expect(store.cleared, isTrue);
      expect(container.read(sessionProvider.notifier).accessToken, isNull);
    });

    test('but not when the platform has just refused the refresh token', () async {
      // `identity_refresh_token_invalid` is the platform saying the session is already over.
      // Telling it back would be a wasted request from a device that may have no signal, sent
      // with a credential it has just refused — and SHIP-40's reuse detection means it may have
      // ended the session precisely because a spent token was presented.
      final ender = FakeSessionEnder();
      final refresher = FakeSessionRefresher()
        ..failure = const ApiErrorResponse(
          statusCode: 400,
          code: 'identity_refresh_token_invalid',
          message: 'This session has ended. Sign in again.',
        );

      final (:container, refresher: _) = containerWith(
        FakeTokenStore(refreshToken: 'refresh-1'),
        refresher: refresher,
        ender: ender,
      );
      await container.read(sessionProvider.notifier).restored;
      await Future<void>.delayed(Duration.zero);

      expect(container.read(sessionProvider), isA<SessionSignedOut>());
      expect(ender.calls, 0);
    });

    test('and not when the device has no token to end a session with', () async {
      final ender = FakeSessionEnder();
      final (:container, refresher: _) = containerWith(FakeTokenStore(), ender: ender);
      await container.read(sessionProvider.notifier).restored;

      await container.read(sessionProvider.notifier).signOut();
      await Future<void>.delayed(Duration.zero);

      expect(ender.calls, 0, reason: 'a signed-out device has no session for the platform to end');
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
