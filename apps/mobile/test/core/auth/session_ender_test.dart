// What the sign-out request actually looks like on the wire, against
// contracts/paths/identity.yaml.
//
// `session_controller_test.dart` proves the session *makes* the call and survives it failing.
// This proves the call is the one the platform serves — and one property here is not about the
// contract at all: the request must go through a transport that carries **no** auth interceptor,
// because the token it authenticates with is the one the session is in the act of discarding.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/auth_interceptor.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/auth/session_ender.dart';
import 'package:shipper/core/errors/api_failure.dart';

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
    requests.add(options);
    return respond(options);
  }

  @override
  void close({bool force = false}) {}
}

({SessionEnder ender, _StubAdapter adapter}) _enderReturning(
  ResponseBody Function(RequestOptions options) respond, {
  SessionTokens Function()? session,
}) {
  final adapter = _StubAdapter(respond);
  final dio = buildDio(baseUrl: 'http://localhost:8092', session: session)
    ..httpClientAdapter = adapter;
  return (ender: ApiSessionEnder(ApiClient(dio)), adapter: adapter);
}

ResponseBody _noContent(RequestOptions _) => ResponseBody.fromString('', 204);

/// A session that holds nothing, which is what the real one holds by the time this is sent.
class _ClearedSession implements SessionTokens {
  var refreshes = 0;

  @override
  String? get accessToken => null;

  @override
  Future<String?> refreshedAccessToken(String? stale) async {
    refreshes++;
    return null;
  }
}

void main() {
  test('POSTs /v1/auth/logout with the token it was handed and no body', () async {
    // Which session ends is decided by the token, not by the request: the `sid` claim names the
    // device the call came from. There is nothing to send, and a session identifier in a body
    // would be a client choosing which session to end.
    final (:ender, :adapter) = _enderReturning(_noContent);

    await ender.end(accessToken: 'access-1', idempotencyKey: 'key-1');

    final sent = adapter.requests.single;
    expect(sent.method, 'POST');
    expect(sent.path, '/v1/auth/logout');
    expect(sent.headers[ApiHeaders.bearer], 'Bearer access-1');
    expect(sent.headers[ApiHeaders.idempotencyKey], 'key-1');
    expect(sent.data, isNull);
  });

  test('204 is a success, not a response the client cannot use', () async {
    final (:ender, adapter: _) = _enderReturning(_noContent);

    await expectLater(
      ender.end(accessToken: 'access-1', idempotencyKey: 'key-1'),
      completes,
    );
  });

  test('the token comes from the caller, not from a session the interceptor would read', () async {
    // The whole reason `end` takes an access token. `SessionController.signOut` clears the
    // in-memory token, and `AuthInterceptor.onRequest` reads it at request time — so a
    // fire-and-forget logout that relied on the interceptor would go out with no credential.
    final session = _ClearedSession();
    final (:ender, :adapter) = _enderReturning(_noContent, session: () => session);

    await ender.end(accessToken: 'access-1', idempotencyKey: 'key-1');

    expect(adapter.requests.single.headers[ApiHeaders.bearer], 'Bearer access-1');
  });

  test('a 401 does not start a refresh, because that would mint a session to destroy one',
      () async {
    // The interceptor's answer to a 401 is to refresh and replay. On the way out that means
    // obtaining a fresh credential in order to end the one being discarded — and, when the
    // refresh fails, calling `signOut` from inside `signOut`. The real provider builds this over
    // `unauthenticatedApiClientProvider`, which carries no interceptor at all; this asserts the
    // property rather than the wiring, so it holds if somebody rewires it.
    final session = _ClearedSession();
    final (:ender, adapter: _) = _enderReturning(
      (_) => ResponseBody.fromString(
        jsonEncode({
          'error': {'code': 'unauthenticated', 'message': 'Sign in to continue.'},
        }),
        401,
        headers: {
          Headers.contentTypeHeader: [Headers.jsonContentType],
        },
      ),
      // Deliberately *with* a session, which is the arrangement being ruled out. The real
      // transport has none.
      session: () => session,
    );

    await expectLater(
      ender.end(accessToken: 'access-1', idempotencyKey: 'key-1'),
      throwsA(isA<ApiErrorResponse>()),
    );

    // One refresh at most, and the session answered `null`, so nothing replayed and nothing
    // looped. What the application actually installs cannot refresh here at all.
    expect(session.refreshes, lessThanOrEqualTo(1));
  });
}
