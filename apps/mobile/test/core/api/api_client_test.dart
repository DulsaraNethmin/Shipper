import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/errors/api_failure.dart';

/// A transport that answers from a script instead of a socket.
///
/// Hand-written rather than pulling in a mocking package: the whole of the interface is three
/// methods, and it lets each test say exactly what came back — including the bodies that are
/// not JSON, which is the interesting half.
class _StubAdapter implements HttpClientAdapter {
  _StubAdapter(this.respond);

  /// Called with the outgoing request; returns the reply, or throws to simulate a transport
  /// failure.
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

ResponseBody _json(Object body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

({ApiClient client, _StubAdapter adapter}) _clientReturning(
  ResponseBody Function(RequestOptions options) respond,
) {
  final adapter = _StubAdapter(respond);
  final dio = buildDio(baseUrl: 'http://localhost:8080')..httpClientAdapter = adapter;
  return (client: ApiClient(dio), adapter: adapter);
}

void main() {
  group('reads', () {
    test('decode to a map', () async {
      final (:client, :adapter) = _clientReturning(
        (_) => _json({'status': 'ok', 'version': 'v0.1.0'}),
      );

      expect(await client.getJson('/health'), containsPair('version', 'v0.1.0'));
      expect(adapter.requests.single.method, 'GET');
    });

    test('tolerate fields this build has never heard of', () async {
      // Docs/07 §6. Old builds live on devices indefinitely, so the server adds fields rather
      // than repurposing them — and a client that rejected what it did not recognise would
      // turn every additive change into a store release.
      final (:client, adapter: _) = _clientReturning(
        (_) => _json({
          'status': 'ok',
          'version': 'v0.1.0',
          'a_field_added_next_quarter': {'nested': true},
        }),
      );

      final body = await client.getJson('/health');
      expect(body['version'], 'v0.1.0');
      expect(body.containsKey('a_field_added_next_quarter'), isTrue);
    });

    test('carry no idempotency key, because they change nothing', () async {
      final (:client, :adapter) = _clientReturning((_) => _json({'status': 'ok'}));

      await client.getJson('/health');
      expect(adapter.requests.single.headers, isNot(contains(ApiHeaders.idempotencyKey)));
    });
  });

  group('writes', () {
    test('send the caller\'s idempotency key unchanged', () async {
      final (:client, :adapter) = _clientReturning((_) => _json({'id': 'bid_1'}, status: 201));

      await client.postJson('/v1/bids', idempotencyKey: 'key-from-the-queue', body: {'x': 1});

      expect(
        adapter.requests.single.headers[ApiHeaders.idempotencyKey],
        'key-from-the-queue',
      );
    });

    test('are refused locally when no key was set', () async {
      // Going through the raw transport, which is the only way to reach a write without a key
      // — postJson requires one at compile time. The platform would refuse this too, with
      // 400 idempotency_key_required, but it would do so days later from a build already on a
      // phone.
      final (:client, :adapter) = _clientReturning((_) => _json({}));

      await expectLater(
        client.transport.post<Object?>('/v1/bids', data: {'x': 1}),
        throwsA(isA<DioException>()),
      );
      expect(adapter.requests, isEmpty, reason: 'the request must not leave the device');
    });
  });

  group('failures', () {
    test('read the platform error contract', () async {
      final (:client, adapter: _) = _clientReturning(
        (_) => _json({
          'error': {
            'code': 'validation_failed',
            'message': 'The job could not be published.',
            'request_id': '9f2c1b',
            'details': [
              {'field': 'goods.category', 'code': 'prohibited_category', 'message': 'No.'},
            ],
          },
        }, status: 422),
      );

      late final ApiErrorResponse error;
      try {
        await client.getJson('/v1/jobs');
        fail('a 422 must not resolve');
      } on ApiErrorResponse catch (e) {
        error = e;
      }

      expect(error.code, 'validation_failed');
      expect(error.statusCode, 422);
      expect(error.requestId, '9f2c1b');
      expect(error.details.single.field, 'goods.category');
      expect(error.details.single.code, 'prohibited_category');
    });

    test('survive a body that is not the contract at all', () async {
      // A proxy's HTML error page, or an empty 502. The app still has to say something, and
      // it must not be a crash on a cast.
      final (:client, adapter: _) = _clientReturning(
        (_) => ResponseBody.fromString('<html>502 Bad Gateway</html>', 502),
      );

      await expectLater(
        client.getJson('/health'),
        throwsA(
          isA<ApiErrorResponse>()
              .having((e) => e.code, 'code', 'unexpected_status')
              .having((e) => e.statusCode, 'statusCode', 502)
              .having((e) => e.userMessage, 'userMessage', isNotEmpty),
        ),
      );
    });

    test('report no connection as unreachable rather than as a refusal', () async {
      final adapter = _StubAdapter(
        (options) => throw DioException.connectionError(
          requestOptions: options,
          reason: 'no route to host',
        ),
      );
      final dio = buildDio(baseUrl: 'http://localhost:8080')..httpClientAdapter = adapter;

      await expectLater(
        ApiClient(dio).getJson('/health'),
        throwsA(isA<ApiUnreachable>()),
      );
    });

    test('report a timeout distinctly, because the answer to it is different', () async {
      final adapter = _StubAdapter(
        (options) => throw DioException.receiveTimeout(
          timeout: const Duration(seconds: 1),
          requestOptions: options,
        ),
      );
      final dio = buildDio(baseUrl: 'http://localhost:8080')..httpClientAdapter = adapter;

      await expectLater(
        ApiClient(dio).getJson('/health'),
        throwsA(isA<ApiUnreachable>().having((e) => e.timedOut, 'timedOut', isTrue)),
      );
    });

    test('a 204 is not a usable JSON object', () async {
      final (:client, adapter: _) = _clientReturning((_) => ResponseBody.fromString('', 204));

      await expectLater(
        client.getJson('/v1/something'),
        throwsA(isA<ApiMalformedResponse>()),
      );
    });
  });
}
