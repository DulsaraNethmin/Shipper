// What actually reaches the wire, checked against contracts/paths/notifications.yaml.
//
// The registrar is tested against a fake repository, which is the right seam for it and the wrong
// one for this: a fake cannot notice that the deregistration went out with no credential, or that
// the idempotency key was left off a state-changing request the platform refuses without one.
//
// The credential is the part worth being careful about. `DELETE …/device-tokens/current` is sent as
// the session is being discarded, so it must carry the token itself and must **not** travel through
// the interceptor that reads the session's — see `session_ender.dart`, which made the same argument
// first for `POST /v1/auth/logout`.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/notifications/device_token.dart';
import 'package:shipper/features/notifications/notifications_repository.dart';

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

ResponseBody _json(Object body, {int status = 201}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

ResponseBody _noContent() => ResponseBody.fromString('', 204);

({NotificationsRepository repo, _StubAdapter adapter}) _repoAnswering(
  ResponseBody Function(RequestOptions) respond,
) {
  final adapter = _StubAdapter(respond);
  final dio = buildDio(baseUrl: 'http://localhost:8094')..httpClientAdapter = adapter;
  final client = ApiClient(dio);
  // Both halves over the same transport here: the split in production is about which client
  // carries a session, and neither does in a test. What is asserted is the request that came out.
  return (repo: ApiNotificationsRepository(client, client), adapter: adapter);
}

const _registration = <String, Object?>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'platform': 'android',
  'registered_at': '2026-08-15T02:11:04.000Z',
};

void main() {
  group('POST /v1/notifications/device-tokens', () {
    test('the token and the platform are the whole body', () async {
      final wired = _repoAnswering((_) => _json(_registration));
      await wired.repo.register(
        token: 'fMEr9Xk2Q3aBcDeF:APA91bHq',
        platform: DevicePlatform.android,
        idempotencyKey: 'k1',
      );

      final sent = wired.adapter.requests.single;
      expect(sent.method, 'POST');
      expect(sent.path, '/v1/notifications/device-tokens');
      // `additionalProperties: false` on the request schema — a client sending anything else is
      // told the field does not exist rather than having it ignored. There is no `{id}` on the path
      // and no field naming whose device this is: the token binds to the session the credential was
      // issued against, and a client naming one would be an authorisation decision made from client
      // input (Docs/07 §3).
      expect(sent.data, <String, Object?>{
        'token': 'fMEr9Xk2Q3aBcDeF:APA91bHq',
        'platform': 'android',
      });
    });

    test('it carries the idempotency key it was given', () async {
      final wired = _repoAnswering((_) => _json(_registration));
      await wired.repo.register(
        token: 't',
        platform: DevicePlatform.ios,
        idempotencyKey: 'the-key',
      );

      expect(wired.adapter.requests.single.headers[ApiHeaders.idempotencyKey], 'the-key');
    });

    test('the platform travels as the enum the contract names', () async {
      final wired = _repoAnswering((_) => _json(_registration));
      await wired.repo.register(token: 't', platform: DevicePlatform.ios, idempotencyKey: 'k');

      expect((wired.adapter.requests.single.data! as Map)['platform'], 'ios');
    });

    test('no device session arrives as its code, not as a credential problem', () async {
      // A `401` the ordinary answer is wrong for: there is nothing to register against, so
      // refreshing and replaying loops against a session that will still not be there. The client
      // branches on `code` (Docs/07 §6) and stops.
      final wired = _repoAnswering(
        (_) => _json(
          <String, Object?>{
            'error': <String, Object?>{
              'code': 'notifications_no_device_session',
              'message': 'this wording is not what anything branches on',
              'request_id': 'r1',
            },
          },
          status: 401,
        ),
      );

      await expectLater(
        wired.repo.register(token: 't', platform: DevicePlatform.ios, idempotencyKey: 'k'),
        throwsA(
          isA<ApiErrorResponse>()
              .having((f) => f.code, 'code', 'notifications_no_device_session')
              .having((f) => f.statusCode, 'statusCode', 401),
        ),
      );
    });

    test('the registration that comes back carries no token', () async {
      // The response schema is `additionalProperties: false` and has no `token`. This holds the
      // *model* to it as well, so a platform that started sending one — or a proxy that injected
      // one — could not produce a value with somewhere to put it. A token identifies somebody's
      // handset and belongs in as few places as possible, response bodies included.
      final wired = _repoAnswering(
        (_) => _json(<String, Object?>{..._registration, 'token': 'APA91bHq-should-not-survive'}),
      );

      final recorded = await wired.repo.register(
        token: 't',
        platform: DevicePlatform.android,
        idempotencyKey: 'k',
      );

      expect(jsonEncode(recorded.toJson()), isNot(contains('APA91bHq')));
    });
  });

  group('DELETE /v1/notifications/device-tokens/current', () {
    test('it names `current` rather than the token', () async {
      // Putting a value that identifies somebody's handset into the path would put it into every
      // access log and every proxy on the way, for no gain — the client always knows which device
      // it is without knowing which token is live.
      final wired = _repoAnswering((_) => _noContent());
      await wired.repo.deregister(accessToken: 'a', idempotencyKey: 'k');

      final sent = wired.adapter.requests.single;
      expect(sent.method, 'DELETE');
      expect(sent.path, '/v1/notifications/device-tokens/current');
    });

    test('it carries the credential it was handed, and an idempotency key', () async {
      // **The assertion a fake repository cannot make, and the one this endpoint turns on.** The
      // session has already forgotten this token by the time the signed-out state is published, so
      // a request that let the transport supply one would go out with nothing.
      final wired = _repoAnswering((_) => _noContent());
      await wired.repo.deregister(accessToken: 'the-spent-token', idempotencyKey: 'k1');

      final sent = wired.adapter.requests.single;
      expect(sent.headers[ApiHeaders.bearer], 'Bearer the-spent-token');
      expect(sent.headers[ApiHeaders.idempotencyKey], 'k1');
    });

    test('a 204 is a success rather than a broken response', () async {
      // The trap `postNoContent` exists for, arriving on the other verb: a `204` read as JSON is
      // raised as ApiMalformedResponse — a success reported as a failure.
      final wired = _repoAnswering((_) => _noContent());

      await expectLater(wired.repo.deregister(accessToken: 'a', idempotencyKey: 'k'), completes);
    });
  });
}
