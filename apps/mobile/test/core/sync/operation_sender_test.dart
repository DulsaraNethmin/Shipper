// SHIP-125 — the half of "per-item idempotency keys" that the worker's own tests cannot reach.
//
// sync_worker_test.dart proves the *row's* key does not move across attempts. This proves the
// value that actually goes on the wire is that key, on the first attempt and on every one after
// it — which is the claim SHIP-111's partial unique index depends on, and the one a sender that
// minted its own would break without failing anything else.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/operation_sender.dart';

/// A transport that answers from a script instead of a socket. The shape `api_client_test.dart`
/// uses, for the same reason: three methods is less than a mocking package costs.
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

({ApiOperationSender sender, _StubAdapter adapter}) _senderAnswering(
  ResponseBody Function(RequestOptions options) respond,
) {
  final adapter = _StubAdapter(respond);
  final dio = buildDio(baseUrl: 'https://api.example.test');
  dio.httpClientAdapter = adapter;

  return (sender: ApiOperationSender(ApiClient(dio)), adapter: adapter);
}

ResponseBody _json(Object body, {int status = 200}) => ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );

QueuedOperation _milestone({
  String key = 'key-1',
  int attempts = 1,
  String? attachmentPath,
}) {
  return QueuedOperation(
    id: 1,
    idempotencyKey: key,
    kind: OperationKind.milestone,
    orderingKey: 'job:job-a',
    method: 'POST',
    path: '/v1/jobs/job-a/milestones',
    body: const <String, dynamic>{'milestone': 'picked_up'},
    attachmentPath: attachmentPath,
    recordedAt: DateTime.utc(2026, 8, 13, 6, 15),
    enqueuedAt: DateTime.utc(2026, 8, 13, 6, 15),
    state: QueueState.pending,
    attempts: attempts,
  );
}

void main() {
  test('the operation is sent as it was recorded', () async {
    final answering = _senderAnswering((_) => _json({'id': 'm-1'}, status: 201));

    await answering.sender.send(_milestone());

    final sent = answering.adapter.requests.single;
    expect(sent.method, 'POST');
    expect(sent.path, '/v1/jobs/job-a/milestones');
    expect(sent.data, {'milestone': 'picked_up'});
  });

  test('every attempt carries the key the operation was created with', () async {
    // `Docs/07` §4's rule, at the only place it can be observed: the header. A sender that minted
    // one per attempt would look correct, pass every test that does not send twice, and put a
    // second milestone on the customer's timeline for one thing the driver did.
    final answering = _senderAnswering((_) => _json({'id': 'm-1'}, status: 201));
    final operation = _milestone(key: 'a4f21c9e');

    await answering.sender.send(operation);
    await answering.sender.send(_milestone(key: 'a4f21c9e', attempts: 2));
    await answering.sender.send(_milestone(key: 'a4f21c9e', attempts: 3));

    expect(
      answering.adapter.requests.map((r) => r.headers[ApiHeaders.idempotencyKey]),
      ['a4f21c9e', 'a4f21c9e', 'a4f21c9e'],
    );
  });

  test('a 2xx with nothing useful in it is a success, not a malformed response', () async {
    // The trap this method exists to avoid. `postJson` raises ApiMalformedResponse for a 2xx with
    // no JSON object in it, which for a queued operation would be a success reported as a failure
    // — and then retried forever against an endpoint that had already recorded it.
    final answering = _senderAnswering((_) => ResponseBody.fromString('', 204));

    await expectLater(answering.sender.send(_milestone()), completes);
  });

  test('a refusal arrives in the error contract, so the worker can read the code', () async {
    final answering = _senderAnswering(
      (_) => _json({
        'error': {
          'code': 'delivery_milestone_not_permitted',
          'message': 'Reload the delivery.',
          'request_id': 'req-1',
        },
      }, status: 422),
    );

    await expectLater(
      answering.sender.send(_milestone()),
      throwsA(
        isA<ApiErrorResponse>()
            .having((e) => e.statusCode, 'statusCode', 422)
            .having((e) => e.code, 'code', 'delivery_milestone_not_permitted'),
      ),
    );
  });

  test('a dead link arrives as unreachable rather than as a refusal', () async {
    // The distinction the worker's whole classification rests on: a link that never carried the
    // request is not the platform saying no.
    final dio = buildDio(baseUrl: 'https://api.example.test');
    dio.httpClientAdapter = _ThrowingAdapter();

    await expectLater(
      ApiOperationSender(ApiClient(dio)).send(_milestone()),
      throwsA(isA<ApiUnreachable>()),
    );
  });

  test('a proof photograph is refused rather than sent without its image', () async {
    // Not an ApiFailure, deliberately: the worker reads anything that is not one as "this build
    // cannot send this", quarantines it, and does not loop. SHIP-130 removes the throw.
    final answering = _senderAnswering((_) => _json({'ok': true}));

    await expectLater(
      answering.sender.send(_milestone(attachmentPath: '/var/mobile/proof-1.jpg')),
      throwsA(isA<UnimplementedError>()),
    );
    expect(answering.adapter.requests, isEmpty, reason: 'nothing half-formed was sent');
  });
}

class _ThrowingAdapter implements HttpClientAdapter {
  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    throw DioException(requestOptions: options, type: DioExceptionType.connectionError);
  }

  @override
  void close({bool force = false}) {}
}
