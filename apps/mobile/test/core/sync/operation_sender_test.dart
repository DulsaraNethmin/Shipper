// SHIP-125 — the half of "per-item idempotency keys" that the worker's own tests cannot reach.
//
// sync_worker_test.dart proves the *row's* key does not move across attempts. This proves the
// value that actually goes on the wire is that key, on the first attempt and on every one after
// it — which is the claim SHIP-111's partial unique index depends on, and the one a sender that
// minted its own would break without failing anything else.

import 'dart:convert';
import 'dart:io';
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

/// An uploader that remembers what it was asked to put where, and can be told to fail.
///
/// It stands in for the **object store**, which is a different host from the API (`Docs/06` §5.2)
/// and reachable through no adapter this client installs. That separation is the thing worth
/// asserting rather than mocking away: nothing that goes to the store may carry this session's
/// bearer token.
class _RecordingUploader implements ObjectUploader {
  _RecordingUploader({this.fail});

  /// Thrown instead of uploading, when set.
  final Object? fail;

  final puts = <({String url, String contentType, int length})>[];

  @override
  Future<void> put(String url, {required String contentType, required Uint8List bytes}) async {
    puts.add((url: url, contentType: contentType, length: bytes.length));
    final failure = fail;
    if (failure != null) throw failure;
  }
}

({ApiOperationSender sender, _StubAdapter adapter, _RecordingUploader uploader}) _senderAnswering(
  ResponseBody Function(RequestOptions options) respond, {
  _RecordingUploader? uploader,
}) {
  final adapter = _StubAdapter(respond);
  final dio = buildDio(baseUrl: 'https://api.example.test');
  dio.httpClientAdapter = adapter;

  final store = uploader ?? _RecordingUploader();
  return (sender: ApiOperationSender(ApiClient(dio), store), adapter: adapter, uploader: store);
}

ResponseBody _json(Object body, {int status = 200}) => ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );

/// A proof photograph, queued: the same recording, with a file beside it (SHIP-130).
QueuedOperation _proof({
  String key = 'key-1',
  int attempts = 1,
  required String attachmentPath,
}) {
  return QueuedOperation(
    id: 1,
    idempotencyKey: key,
    kind: OperationKind.proof,
    orderingKey: 'job:job-a',
    method: 'POST',
    path: '/v1/jobs/job-a/milestones',
    body: const <String, dynamic>{'milestone': 'delivered'},
    attachmentPath: attachmentPath,
    recordedAt: DateTime.utc(2026, 8, 13, 6, 15),
    enqueuedAt: DateTime.utc(2026, 8, 13, 6, 15),
    state: QueueState.pending,
    attempts: attempts,
  );
}

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
      ApiOperationSender(ApiClient(dio), _RecordingUploader()).send(_milestone()),
      throwsA(isA<ApiUnreachable>()),
    );
  });

  group('a proof photograph, which is three requests and two of them are the platform\'s', () {
    /// A file on disk, because the sender reads one and the size it reads is what it declares.
    File imageFile(List<int> bytes) {
      final directory = Directory.systemTemp.createTempSync('shipper_proof_send');
      addTearDown(() {
        if (directory.existsSync()) directory.deleteSync(recursive: true);
      });
      final file = File('${directory.path}/proof.jpg')..writeAsBytesSync(bytes);
      return file;
    }

    ResponseBody presignThen(RequestOptions options) {
      if (options.path.endsWith('/proof-uploads')) {
        return _json({
          'object_key': 'proof/job-1/0198f2c1',
          'upload_url': 'https://objects.example.test/proof/job-1/0198f2c1?X-Amz-Signature=abc',
          'method': 'PUT',
          'content_type': 'image/jpeg',
          'content_length': 4,
          'expires_at': '2026-08-13T09:15:00Z',
        });
      }
      return _json({'id': 'milestone-1'}, status: 201);
    }

    test('presign, put, then record the milestone with the key the platform issued', () async {
      final file = imageFile(const [1, 2, 3, 4]);
      final answering = _senderAnswering(presignThen);

      await answering.sender.send(
        _proof(key: 'key-9', attachmentPath: file.path, attempts: 1),
      );

      expect(answering.adapter.requests, hasLength(2), reason: 'the PUT is not the API\'s');

      final presign = answering.adapter.requests.first;
      expect(
        presign.path,
        '/v1/jobs/job-a/proof-uploads',
        reason: 'derived from the recording path, so a route renamed on the platform fails here '
            'rather than on a handset',
      );
      expect(presign.data, {'content_type': 'image/jpeg', 'content_length': 4});
      expect(
        presign.headers[ApiHeaders.idempotencyKey],
        'key-9.upload.1',
        reason: 'reusing the row\'s key here replays a URL whose expiry is already running down, '
            'so an operation that failed once would re-receive a dead one for hours',
      );

      expect(answering.uploader.puts, hasLength(1));
      expect(
        answering.uploader.puts.single.url,
        'https://objects.example.test/proof/job-1/0198f2c1?X-Amz-Signature=abc',
      );
      expect(
        answering.uploader.puts.single.contentType,
        'image/jpeg',
        reason: 'the type the platform signed, echoed back verbatim',
      );
      expect(answering.uploader.puts.single.length, 4);

      final record = answering.adapter.requests.last;
      expect(record.path, '/v1/jobs/job-a/milestones');
      expect(record.headers[ApiHeaders.idempotencyKey], 'key-9', reason: 'the row\'s own key');
      expect(record.data, {
        'milestone': 'delivered',
        'proof': {'object_key': 'proof/job-1/0198f2c1'},
      });
    });

    test('the file is removed once the platform has it, and only then', () async {
      final file = imageFile(const [1, 2, 3, 4]);

      final failing = _senderAnswering(
        presignThen,
        uploader: _RecordingUploader(fail: const ApiUnreachable()),
      );
      await expectLater(
        failing.sender.send(_proof(attachmentPath: file.path)),
        throwsA(isA<ApiUnreachable>()),
      );
      expect(file.existsSync(), isTrue, reason: 'an upload that failed is retried from this file');

      await _senderAnswering(presignThen).sender.send(_proof(attachmentPath: file.path));
      expect(file.existsSync(), isFalse);
    });

    test('a file that has gone is quarantined rather than retried for ever', () async {
      // Not an ApiFailure, deliberately: the worker reads anything that is not one as "this build
      // cannot send this", quarantines it where a person can see it, and does not loop. A missing
      // file does not come back, which is the case `queued_operation.dart` predicted.
      final answering = _senderAnswering(presignThen);

      await expectLater(
        answering.sender.send(_proof(attachmentPath: '/var/mobile/gone.jpg')),
        throwsA(isA<StateError>()),
      );
      expect(answering.adapter.requests, isEmpty, reason: 'nothing half-formed was sent');
    });

    test('an attachment on a kind that declares none is quarantined, not guessed at', () async {
      // The other half of the content type living on `OperationKind`: a row that should never
      // exist is refused loudly rather than uploaded as whatever the file happens to be.
      final file = imageFile(const [1, 2, 3, 4]);
      final answering = _senderAnswering(presignThen);

      await expectLater(
        answering.sender.send(_milestone(attachmentPath: file.path)),
        throwsA(isA<StateError>()),
      );
      expect(answering.adapter.requests, isEmpty);
    });

    test('a presign response missing a field is retried, not quarantined', () async {
      final file = imageFile(const [1, 2, 3, 4]);
      final answering = _senderAnswering((options) {
        if (options.path.endsWith('/proof-uploads')) return _json({'object_key': 'k'});
        return _json({'id': 'milestone-1'}, status: 201);
      });

      await expectLater(
        answering.sender.send(_proof(attachmentPath: file.path)),
        throwsA(isA<ApiMalformedResponse>()),
      );
      expect(answering.uploader.puts, isEmpty);
      expect(file.existsSync(), isTrue, reason: 'the photograph survives a bad answer');
    });
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
