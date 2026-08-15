// SHIP-143 — "Token registers after sign-in and de-registers on sign-out".
//
// # The two clauses, and the one that a plausible implementation gets wrong
//
// **Registers after sign-in.** Not at launch: a device token binds to the device session the access
// token was issued against, and there is nothing to bind to before there is one. Asserted through
// the real `SessionController`, so a listener wired to the wrong state fails here.
//
// **De-registers on sign-out.** This is the half worth being careful about. The obvious
// implementation — listen for `SessionSignedOut` and send the `DELETE` — **cannot work**, because
// `signOut` clears the in-memory access token before it publishes the state, so the request travels
// with no credential and achieves nothing. It also *looks* like it works: something is sent, a
// future completes, nothing throws. So the assertion here is on the **credential the request
// carried**, which is the only thing that tells the two apart.
//
// # What this file cannot demonstrate, said rather than implied
//
// The token itself. `PushTokenSource` has no implementation because **no Firebase project exists**
// and no ticket anywhere creates one — the same blocker SHIP-139 recorded from the platform side.
// Every test here substitutes the source, so what is demonstrated is when each request is made,
// what it carries, and what happens when it fails. On a device today nothing registers, which is
// the honest state and is what `push_token_source.dart` says.

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/token_pair.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/notifications/device_token.dart';
import 'package:shipper/features/notifications/notifications_repository.dart';
import 'package:shipper/features/notifications/push_registration.dart';
import 'package:shipper/features/notifications/push_token_source.dart';

import '../../core/auth/fake_token_store.dart';
import 'fake_notifications_repository.dart';

/// An access token the session will accept. Its claims are not read by anything here.
const _accessToken = 'header.payload.signature';

/// A container with the session real and the two push seams substituted.
///
/// The session is **not** faked: "after sign-in" and "on sign-out" are facts about
/// `SessionController`'s own lifecycle, and a fake session would let a listener wired to the wrong
/// state pass.
({ProviderContainer container, FakeNotificationsRepository repository, FakePushTokenSource? source})
    wired({
  FakePushTokenSource? source,
  FakeNotificationsRepository? repository,
  FakeTokenStore? store,
}) {
  final notifications = repository ?? FakeNotificationsRepository();
  final container = ProviderContainer(
    overrides: [
      tokenStoreProvider.overrideWithValue(store ?? FakeTokenStore()),
      notificationsRepositoryProvider.overrideWithValue(notifications),
      pushTokenSourceProvider.overrideWithValue(source),
      // **Not left to `Platform.isIOS`.** `flutter test` runs on macOS or Linux, where
      // `currentDevicePlatform()` answers `null` and the registrar correctly sends nothing — so a
      // suite that did not supply this would assert on empty lists and pass while testing no
      // behaviour at all. A test that is counted and does not run is worse than one that does not
      // exist.
      devicePlatformProvider.overrideWithValue(() => DevicePlatform.android),
      // What `main.dart` supplies. Without it the deregistration never runs — which is exactly
      // what `push_registration_wiring_test.dart` holds `main.dart` to.
      signOutHooksProvider.overrideWith((ref) => <SignOutHook>[pushDeregistrationHook(ref)]),
    ],
  );
  addTearDown(container.dispose);
  addTearDown(() async => source?.close());
  return (container: container, repository: notifications, source: source);
}

void main() {
  group('registering', () {
    test('nothing is sent before there is a session', () async {
      final w = wired(source: FakePushTokenSource());
      w.container.read(pushRegistrarProvider);

      // The restore has not finished, so the session is still `SessionRestoring`.
      expect(w.repository.registrations, isEmpty);
    });

    test('a build with no token source registers nothing at all', () async {
      // The production case today, and it has to be silent rather than an error: a registration
      // with no dispatcher behind it is a row in a table nothing can use.
      final w = wired(source: null, store: FakeTokenStore(refreshToken: 'stored'));
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;

      expect(w.repository.registrations, isEmpty);
    });

    test('signing in registers the token, with an idempotency key', () async {
      final w = wired(source: FakePushTokenSource(token: 'the-token'));
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;

      await w.container
          .read(sessionProvider.notifier)
          .signIn(const TokenPair(accessToken: _accessToken, refreshToken: 'r'));
      await Future<void>.delayed(Duration.zero);

      expect(w.repository.registrations, hasLength(1));
      expect(w.repository.registrations.single.token, 'the-token');
      expect(w.repository.registrations.single.idempotencyKey, isNotEmpty);
    });

    test('no permission yet is silence, not a failure', () async {
      // SHIP-144 asks for permission contextually and asks late, so a signed-in device with no
      // token is the ordinary state for a while. Push is a prompt and never a channel of record
      // (Docs/07 §5) — an app with no push is an app that works.
      final w = wired(source: FakePushTokenSource(token: null));
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;

      await w.container
          .read(sessionProvider.notifier)
          .signIn(const TokenPair(accessToken: _accessToken, refreshToken: 'r'));
      await Future<void>.delayed(Duration.zero);

      expect(w.repository.registrations, isEmpty);
    });

    test('a token Firebase reissues is registered too', () async {
      // Firebase rotates without being asked — a restore, a reinstall, its own storage cleared —
      // and a registration only made at sign-in would then address a handset that no longer
      // answers to it.
      final source = FakePushTokenSource(token: 'first');
      final w = wired(source: source);
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;
      await w.container
          .read(sessionProvider.notifier)
          .signIn(const TokenPair(accessToken: _accessToken, refreshToken: 'r'));
      await Future<void>.delayed(Duration.zero);

      source.rotate('second');
      await Future<void>.delayed(Duration.zero);

      expect(w.repository.registrations.map((c) => c.token), ['first', 'second']);
      // Two registrations are two actions, so two keys. Reusing one across them is what
      // `idempotency_key_reused` exists to refuse.
      expect(
        w.repository.registrations[0].idempotencyKey,
        isNot(w.repository.registrations[1].idempotencyKey),
      );
    });

    test('the same token again is not re-sent', () async {
      final source = FakePushTokenSource(token: 'same');
      final w = wired(source: source);
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;
      await w.container
          .read(sessionProvider.notifier)
          .signIn(const TokenPair(accessToken: _accessToken, refreshToken: 'r'));
      await Future<void>.delayed(Duration.zero);

      source.rotate('same');
      await Future<void>.delayed(Duration.zero);

      expect(w.repository.registrations, hasLength(1));
    });
  });

  group('deregistering', () {
    /// Signs in with a token source, then signs out.
    Future<({FakeNotificationsRepository repository, ProviderContainer container})> signInThenOut(
      FakePushTokenSource? source, {
      FakeNotificationsRepository? repository,
    }) async {
      final w = wired(source: source, repository: repository);
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;
      await w.container
          .read(sessionProvider.notifier)
          .signIn(const TokenPair(accessToken: _accessToken, refreshToken: 'r'));
      await Future<void>.delayed(Duration.zero);

      await w.container.read(sessionProvider.notifier).signOut();
      await Future<void>.delayed(Duration.zero);

      return (repository: w.repository, container: w.container);
    }

    test('signing out deregisters, carrying the token the session is discarding', () async {
      // **The assertion that separates this from the implementation that looks right.** A
      // deregistration wired to the `SessionSignedOut` state would send a request with no
      // credential at all, because `signOut` clears the access token before publishing the state.
      final out = await signInThenOut(FakePushTokenSource());

      expect(out.repository.deregistrations, hasLength(1));
      expect(
        out.repository.deregistrations.single.accessToken,
        _accessToken,
        reason: 'the deregistration has to carry the spent access token; the session has already '
            'forgotten it by the time the signed-out state is published',
      );
      expect(out.repository.deregistrations.single.idempotencyKey, isNotEmpty);
    });

    test('a device that never registered sends nothing', () async {
      // Every build today. A `DELETE` for a registration that was never made is a request the
      // platform answers `204` to and nobody wanted.
      final out = await signInThenOut(null);

      expect(out.repository.deregistrations, isEmpty);
    });

    test('a deregistration that fails does not break signing out', () async {
      // Docs/07 §3: what ends the session is the server-side revocation and the device is catching
      // up. A sign-out that left somebody in the signed-in shell because a train went into a tunnel
      // would be a defect rather than a safeguard — and the contract says push delivery ends with
      // the session whether or not this call lands.
      final repository = FakeNotificationsRepository()..deregisterFailure = const ApiUnreachable();
      final out = await signInThenOut(FakePushTokenSource(), repository: repository);

      expect(out.repository.deregistrations, hasLength(1));
      expect(out.container.read(sessionProvider), isA<SessionSignedOut>());
    });

    test('signing in again registers again', () async {
      final source = FakePushTokenSource();
      final w = wired(source: source);
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;

      for (var i = 0; i < 2; i++) {
        await w.container
            .read(sessionProvider.notifier)
            .signIn(const TokenPair(accessToken: _accessToken, refreshToken: 'r'));
        await Future<void>.delayed(Duration.zero);
        await w.container.read(sessionProvider.notifier).signOut();
        await Future<void>.delayed(Duration.zero);
      }

      // The contract asks for this in as many words: Firebase hands the app a token at every
      // launch and only sometimes the same one, so this is called each time.
      expect(w.repository.registrations, hasLength(2));
      expect(w.repository.deregistrations, hasLength(2));
    });
  });

  group('when the platform refuses a registration', () {
    test('no device session stops rather than looping', () async {
      // `notifications_no_device_session` is a `401` the ordinary answer is wrong for: there is
      // nothing to register against, so refreshing and replaying loops against a session that will
      // still not be there. Nothing is retried, and the next sign-in tries again.
      final repository = FakeNotificationsRepository()
        ..registerFailure = const ApiErrorResponse(
          statusCode: 401,
          code: 'notifications_no_device_session',
          message: 'ignored',
        );

      final w = wired(source: FakePushTokenSource(), repository: repository);
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;
      await w.container
          .read(sessionProvider.notifier)
          .signIn(const TokenPair(accessToken: _accessToken, refreshToken: 'r'));
      await Future<void>.delayed(Duration.zero);

      expect(repository.registrations, hasLength(1));
      // Nothing was registered, so signing out must not claim there was.
      await w.container.read(sessionProvider.notifier).signOut();
      await Future<void>.delayed(Duration.zero);
      expect(repository.deregistrations, isEmpty);
    });

    test('a lost connection is silent', () async {
      final repository = FakeNotificationsRepository()..registerFailure = const ApiUnreachable();
      final w = wired(source: FakePushTokenSource(), repository: repository);
      w.container.read(pushRegistrarProvider);
      await w.container.read(sessionProvider.notifier).restored;
      await w.container
          .read(sessionProvider.notifier)
          .signIn(const TokenPair(accessToken: _accessToken, refreshToken: 'r'));
      await Future<void>.delayed(Duration.zero);

      expect(w.container.read(sessionProvider), isA<SessionSignedIn>());
    });
  });

  group('the model', () {
    test('carries no token, because the platform sends none back', () {
      // The contract is explicit: "the client sent the token and already has it, and a value that
      // identifies somebody's handset should appear in as few places as possible — including
      // response bodies, which are logged by more middleware than anybody remembers". A model with
      // somewhere to put it is the first place a screen could be written against one.
      final decoded = DeviceToken.fromJson(<String, dynamic>{
        'id': 'r1',
        'platform': 'ios',
        'registered_at': '2026-08-15T02:11:04.000Z',
        'token': 'fMEr9Xk2Q3aBcDeF:APA91bHq',
      });

      expect(decoded.id, 'r1');
      expect(decoded.platform, 'ios');
      expect(decoded.toString(), isNot(contains('APA91bHq')));
      expect(decoded.toJson().keys, isNot(contains('token')));
    });
  });
}
