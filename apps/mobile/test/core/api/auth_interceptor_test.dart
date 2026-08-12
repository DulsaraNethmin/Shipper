// SHIP-50 — "A 401 triggers one refresh and replays the request; concurrent calls refresh once".
//
// Driven end to end rather than against the interceptor in isolation: the real `apiClientProvider`
// transport, the real `SessionController`, the real single-flight rule, and a stub only at the
// two edges a host test cannot have — the socket and the Keychain. An interceptor tested with a
// hand-made session would pass with the session wired to nothing, and "concurrent calls refresh
// once" is a claim about the two of them together.
//
// The second clause is the hard half and it is why the refresher can be held open: several
// requests failing while a refresh is genuinely in flight is a different code path from several
// failing after one has finished, and only the first is what a phone coming back into signal
// actually does.

import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/auth_interceptor.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_refresher.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/errors/api_failure.dart';

import '../auth/fake_token_store.dart';
import '../auth/session_fixtures.dart';

/// A transport that answers from a script and records every attempt.
class _StubAdapter implements HttpClientAdapter {
  _StubAdapter(this.respond);

  final ResponseBody Function(RequestOptions options) respond;
  final requests = <RequestOptions>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    // Copied, not held: the interceptor mutates the options object it replays, so keeping the
    // reference would make every recorded attempt look like the last one.
    requests.add(options.copyWith());
    return respond(options);
  }

  @override
  void close({bool force = false}) {}
}

ResponseBody _json(Object body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

/// The platform's answer to a request whose access token has expired.
ResponseBody _unauthorised() {
  return _json({
    'error': {
      'code': 'unauthorised',
      'message': 'Sign in to continue.',
      'request_id': '9f2c1b',
    },
  }, status: 401);
}

/// Whether this attempt is the interceptor replaying an earlier one.
bool _isReplay(RequestOptions options) => options.extra[AuthInterceptor.replayedFlag] == true;

String? _bearerOf(RequestOptions options) {
  final Object? header = options.headers[ApiHeaders.bearer];
  return header is String ? header : null;
}

typedef _Harness = ({
  ProviderContainer container,
  SessionController session,
  FakeTokenStore store,
  FakeSessionRefresher refresher,
  _StubAdapter adapter,
});

/// A signed-in app whose socket is a script.
///
/// It signs in rather than restoring from a stored token on purpose: a restore refreshes
/// eagerly (SHIP-50), which would put a refresh on the counter before the test had done
/// anything. Signing in leaves the counter at zero, so every refresh a test sees is one it
/// caused.
Future<_Harness> _signedIn(ResponseBody Function(RequestOptions options) respond) async {
  final store = FakeTokenStore();
  final refresher = FakeSessionRefresher();
  final adapter = _StubAdapter(respond);

  final container = ProviderContainer(
    overrides: [
      tokenStoreProvider.overrideWithValue(store),
      sessionRefresherProvider.overrideWithValue(refresher),
    ],
  );
  addTearDown(container.dispose);

  final session = container.read(sessionProvider.notifier);
  await session.restored;
  await session.signIn(aTokenPair(refreshToken: 'refresh-1'));

  container.read(apiClientProvider).transport.httpClientAdapter = adapter;

  return (
    container: container,
    session: session,
    store: store,
    refresher: refresher,
    adapter: adapter,
  );
}

void main() {
  test('every request carries the access token the session is holding', () async {
    final h = await _signedIn((_) => _json({'ok': true}));

    await h.container.read(apiClientProvider).getJson('/v1/jobs');

    expect(_bearerOf(h.adapter.requests.single), 'Bearer ${h.session.accessToken}');
  });

  test('a signed-out app sends no bearer header at all', () async {
    // Registration and the two verification confirms are public and run with no session. A
    // header with an empty token would be a malformed credential rather than no credential.
    final adapter = _StubAdapter((_) => _json({'ok': true}));
    final container = ProviderContainer(
      overrides: [
        tokenStoreProvider.overrideWithValue(FakeTokenStore()),
        sessionRefresherProvider.overrideWithValue(FakeSessionRefresher()),
      ],
    );
    addTearDown(container.dispose);
    await container.read(sessionProvider.notifier).restored;
    container.read(apiClientProvider).transport.httpClientAdapter = adapter;

    await container.read(apiClientProvider).getJson('/health');

    expect(adapter.requests.single.headers, isNot(contains(ApiHeaders.bearer)));
  });

  group('a 401', () {
    test('triggers one refresh and replays the request', () async {
      final h = await _signedIn((options) => _isReplay(options) ? _json({'ok': true}) : _unauthorised());

      final body = await h.container.read(apiClientProvider).getJson('/v1/jobs');

      expect(body, containsPair('ok', true));
      expect(h.refresher.calls, 1);
      expect(h.adapter.requests, hasLength(2), reason: 'the original and its replay');
      expect(h.adapter.requests.first.path, '/v1/jobs');
      expect(h.adapter.requests.last.path, '/v1/jobs');
    });

    test('replays with the new token rather than the one that was refused', () async {
      final h = await _signedIn((options) => _isReplay(options) ? _json({'ok': true}) : _unauthorised());
      final refused = h.session.accessToken;

      await h.container.read(apiClientProvider).getJson('/v1/jobs');

      expect(_bearerOf(h.adapter.requests.first), 'Bearer $refused');
      expect(_bearerOf(h.adapter.requests.last), 'Bearer ${h.session.accessToken}');
      expect(h.session.accessToken, isNot(refused));
    });

    test('replays a write under the original idempotency key, not a fresh one', () async {
      // The heart of it. `Docs/07` §4 has the key generated where the user acts and reused
      // across every retry of that action, and a refresh-and-replay is a retry by any reading:
      // the platform saw the first attempt, refused it for a credential reason, and is about to
      // see it again. A fresh key would turn one bid into two.
      final h = await _signedIn(
        (options) => _isReplay(options) ? _json({'id': 'bid_1'}, status: 201) : _unauthorised(),
      );

      await h.container.read(apiClientProvider).postJson(
            '/v1/bids',
            idempotencyKey: 'key-from-the-user-action',
            body: {'amount': 1200},
          );

      expect(h.adapter.requests, hasLength(2));
      expect(
        h.adapter.requests.map((r) => r.headers[ApiHeaders.idempotencyKey]),
        everyElement('key-from-the-user-action'),
      );
      expect(h.adapter.requests.last.data, {'amount': 1200}, reason: 'and the same body');
    });

    test('is passed through when the replay is refused again, without a second refresh', () async {
      // A 401 after a refresh is not something a newer token fixes. Retrying is a client
      // hammering an endpoint it can never satisfy.
      final h = await _signedIn((_) => _unauthorised());

      await expectLater(
        h.container.read(apiClientProvider).getJson('/v1/jobs'),
        throwsA(isA<ApiErrorResponse>().having((e) => e.statusCode, 'statusCode', 401)),
      );

      expect(h.refresher.calls, 1);
      expect(h.adapter.requests, hasLength(2));
    });

    test('is passed through unchanged when the refresh itself is refused', () async {
      final h = await _signedIn((_) => _unauthorised());
      h.refresher.failure = const ApiErrorResponse(
        statusCode: 400,
        code: 'identity_refresh_token_invalid',
        message: 'Sign in again.',
      );

      await expectLater(
        h.container.read(apiClientProvider).getJson('/v1/jobs'),
        throwsA(isA<ApiErrorResponse>().having((e) => e.statusCode, 'statusCode', 401)),
      );

      expect(h.adapter.requests, hasLength(1), reason: 'nothing to replay it with');
      expect(h.container.read(sessionProvider), isA<SessionSignedOut>());
    });
  });

  group('concurrent calls', () {
    test('refresh once, and every one of them is replayed', () async {
      // The clause a plausible implementation gets wrong. Three requests go out with the same
      // token; all three are refused; a client that refreshed per request would present the
      // rotated token twice, and SHIP-40's reuse detection would revoke the whole device
      // session for it — the client doing to itself exactly what that mechanism exists to catch.
      final h = await _signedIn(
        (options) => _isReplay(options) ? _json({'ok': true}) : _unauthorised(),
      );

      // Held open so all three failures land while the refresh is genuinely in flight, rather
      // than after the first has already finished — which is a different code path and the
      // easier of the two to get right by accident.
      final gate = Completer<void>();
      h.refresher.gate = gate;

      final client = h.container.read(apiClientProvider);
      final calls = Future.wait([
        client.getJson('/v1/jobs'),
        client.getJson('/v1/bids'),
        client.getJson('/v1/auth/sessions'),
      ]);

      // Let all three reach the socket, be refused, and queue behind the one refresh before it
      // is allowed to finish. Bounded so a regression is a failed expectation rather than a
      // test that never returns.
      for (var turn = 0; turn < 50 && h.adapter.requests.length < 3; turn++) {
        await Future<void>.delayed(Duration.zero);
      }
      for (var turn = 0; turn < 5; turn++) {
        await Future<void>.delayed(Duration.zero);
      }

      h.refresher.gate = null;
      gate.complete();

      final bodies = await calls;

      expect(bodies, hasLength(3));
      expect(bodies, everyElement(containsPair('ok', true)));
      expect(h.refresher.calls, 1, reason: 'one refresh for three failures');
      expect(h.adapter.requests, hasLength(6), reason: 'three originals and three replays');
    });

    test('a 401 that lands after somebody else refreshed replays without refreshing again',
        () async {
      // The other half of the same rule, and the one a single in-flight future does not cover:
      // by the time this failure arrives the refresh has finished, so there is nothing to queue
      // behind. What makes it correct is that the token the failed request carried is compared
      // with the one now held.
      final h = await _signedIn((_) => _json({'ok': true}));
      final stale = h.session.accessToken;

      final first = await h.session.refreshedAccessToken(stale);
      final second = await h.session.refreshedAccessToken(stale);

      expect(h.refresher.calls, 1);
      expect(second, first);
    });
  });

  group('a refresh that says nothing about whether the session is alive', () {
    test('leaves the device signed in', () async {
      // A driver in a tunnel. Signing out on a dropped connection would end a perfectly good
      // session because the signal did — and the session also exists server-side, where nothing
      // has happened.
      final h = await _signedIn((_) => _unauthorised());
      h.refresher.failure = const ApiUnreachable();

      await expectLater(
        h.container.read(apiClientProvider).getJson('/v1/jobs'),
        throwsA(isA<ApiFailure>()),
      );

      expect(h.container.read(sessionProvider), isA<SessionSignedIn>());
      expect(h.store.refreshToken, 'refresh-1', reason: 'the stored token is untouched');
    });

    test('keeps the idempotency key, so the retry can replay the rotation it may have caused',
        () async {
      // The case idempotency exists for: the platform may have rotated the token and lost the
      // reply. Retrying under the same key replays the pair it already issued; a fresh key would
      // present a token that has already been rotated away, which revokes the session.
      final h = await _signedIn((_) => _json({'ok': true}));
      h.refresher.failure = const ApiUnreachable();

      await h.session.refreshedAccessToken(h.session.accessToken);
      h.refresher.failure = null;
      await h.session.refreshedAccessToken(h.session.accessToken);

      expect(h.refresher.keys, hasLength(2));
      expect(h.refresher.keys.first, h.refresher.keys.last);
    });

    test('a refusal retires the key instead, because the next refresh is a new action', () async {
      final h = await _signedIn((_) => _json({'ok': true}));
      h.refresher.failure = const ApiErrorResponse(
        statusCode: 400,
        code: 'identity_refresh_token_invalid',
        message: 'Sign in again.',
      );

      await h.session.refreshedAccessToken(h.session.accessToken);
      expect(h.container.read(sessionProvider), isA<SessionSignedOut>());

      // Sign in again and refresh **the same token value**, so the body fingerprint is identical
      // and a retained key would still be reused. A key held from the dead session would make
      // the platform replay that session's stored answer.
      await h.session.signIn(aTokenPair(refreshToken: 'refresh-1'));
      h.refresher.failure = null;
      await h.session.refreshedAccessToken(h.session.accessToken);

      expect(h.refresher.keys.first, isNot(h.refresher.keys.last));
    });
  });
}
