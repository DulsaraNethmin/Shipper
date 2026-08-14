// What actually reaches the wire, checked against contracts/paths/delivery.yaml.
//
// The screen is tested against a fake repository, which is the right seam for it and the wrong one
// for this: a fake cannot notice that the read went to `/v1/jobs/{id}/delivery` rather than
// `/v1/jobs/{id}/delivery/detail`.
//
// **And that particular mistake is not a 404 — it is a service that will not start.**
// `GET /v1/jobs/{id}/delivery` and `GET /v1/jobs/open/{id}` both match `/v1/jobs/open/delivery` with
// neither more specific, and Go's `ServeMux` panics at registration, so `make run` dies. The
// platform cannot serve the four-segment form even if a client asks for it, which is why
// `Docs/09`'s own row names both paths in full and why the first test here is about the shape of a
// URL rather than about a response.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/delivery/delivery_repository.dart';
import 'package:shipper/features/delivery/proof_exception_reason.dart';

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

({DeliveryRepository repo, _StubAdapter adapter}) _repo(Object body, {int status = 200}) {
  final adapter = _StubAdapter(
    (_) => ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    ),
  );
  final dio = buildDio(baseUrl: 'http://localhost:8094')..httpClientAdapter = adapter;
  return (repo: ApiDeliveryRepository(ApiClient(dio)), adapter: adapter);
}

const _job = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1';

void main() {
  group('every path on the shelf has five segments', () {
    test('the driver read is /delivery/detail', () async {
      final wired = _repo(<String, Object?>{'job_id': _job, 'driver_assigned': false});
      await wired.repo.driver(jobId: _job);

      expect(wired.adapter.requests.single.path, '/v1/jobs/$_job/delivery/detail');
    });

    test('the milestone read is /delivery/milestones', () async {
      final wired = _repo(<String, Object?>{'data': <Object?>[], 'has_more': false});
      await wired.repo.milestones(jobId: _job);

      expect(wired.adapter.requests.single.path, '/v1/jobs/$_job/delivery/milestones');
    });

    test('the proof read is /delivery/proof', () async {
      final wired = _repo(<String, Object?>{'data': <Object?>[], 'has_more': false});
      await wired.repo.proof(jobId: _job);

      expect(wired.adapter.requests.single.path, '/v1/jobs/$_job/delivery/proof');
    });

    test('none of them carries an idempotency key, because a read changes nothing', () async {
      for (final call in <Future<void> Function(DeliveryRepository)>[
        (repo) => repo.driver(jobId: _job),
        (repo) => repo.milestones(jobId: _job),
        (repo) => repo.proof(jobId: _job),
      ]) {
        final wired = _repo(<String, Object?>{
          'job_id': _job,
          'driver_assigned': false,
          'data': <Object?>[],
          'has_more': false,
        });
        await call(wired.repo);

        expect(
          wired.adapter.requests.single.headers.containsKey(ApiHeaders.idempotencyKey),
          isFalse,
        );
      }
    });
  });

  group('the driver read', () {
    test('a job with no driver is a 200 and a complete answer', () async {
      // Not a 404. An awarded job nobody has been put on is an ordinary state, and so is a draft —
      // `Service.partyTo` makes the owner a party whatever the status.
      final read = await _repo(<String, Object?>{
        'job_id': _job,
        'driver_assigned': false,
      }).repo.driver(jobId: _job);

      expect(read.driverAssigned, isFalse);
      expect(read.driverName, isNull);
    });

    test('an assigned driver comes back named', () async {
      final read = await _repo(<String, Object?>{
        'job_id': _job,
        'driver_assigned': true,
        'assignment_id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
        'driver_name': 'Sam Patel',
        'assigned_at': '2026-09-07T04:15:30.000Z',
      }).repo.driver(jobId: _job);

      expect(read.driverAssigned, isTrue);
      expect(read.driverName, 'Sam Patel');
      expect(read.assignedAt, '2026-09-07T04:15:30.000Z');
    });

    test('a driver_mobile the platform should never send a customer is not modelled', () async {
      // The platform blanks it for a customer in the service rather than in the handler — "a
      // handler that never receives a number cannot render one" (SHIP-115a). This is the client's
      // half of the same argument: a field that cannot arrive is a field a later screen cannot
      // accidentally draw. `Docs/07` §6 ignores unknown fields, so one arriving anyway is dropped
      // at the decode rather than carried into the app.
      final read = await _repo(<String, Object?>{
        'job_id': _job,
        'driver_assigned': true,
        'driver_name': 'Sam Patel',
        'driver_mobile': '+61412345678',
      }).repo.driver(jobId: _job);

      expect(jsonEncode(read.toJson()), isNot(contains('61412345678')));
      expect(read.toJson().keys, isNot(contains('driver_mobile')));
    });

    test('a stranger gets the same 404 a missing job gets, surfaced as one failure', () async {
      final repo = _repo(<String, Object?>{
        'error': {'code': 'not_found', 'message': 'not found'},
      }, status: 404).repo;

      await expectLater(
        repo.driver(jobId: _job),
        throwsA(isA<ApiErrorResponse>().having((f) => f.statusCode, 'statusCode', 404)),
      );
    });
  });

  group('the milestone read', () {
    test('a first page sends no parameters, and a cursor goes back unchanged', () async {
      // No `limit`: the page size is server configuration (`Docs/10` §4.5).
      final first = _repo(<String, Object?>{'data': <Object?>[], 'has_more': false});
      await first.repo.milestones(jobId: _job);
      expect(first.adapter.requests.single.queryParameters, isEmpty);

      final next = _repo(<String, Object?>{'data': <Object?>[], 'has_more': false});
      await next.repo.milestones(jobId: _job, cursor: 'MR9yZWNvcmQ');
      expect(
        next.adapter.requests.single.queryParameters,
        <String, dynamic>{'cursor': 'MR9yZWNvcmQ'},
      );
    });

    test('both clocks come back and they are different fields', () async {
      // `Docs/02` §3.1 keeps them apart and they differ by however long a device was out of signal.
      // A client that read one into both would look right on every online delivery.
      final read = await _repo(<String, Object?>{
        'data': [
          <String, Object?>{
            'id': 'm1',
            'job_id': _job,
            'milestone': 'picked_up',
            'recorded_by': 'driver',
            'recorded_at': '2026-09-08T06:40:11.000Z',
            'accepted_at': '2026-09-08T09:15:02.481Z',
          },
        ],
        'has_more': false,
      }).repo.milestones(jobId: _job);

      final row = read.data.single;
      expect(row.recordedAt, '2026-09-08T06:40:11.000Z');
      expect(row.acceptedAt, '2026-09-08T09:15:02.481Z');
      expect(row.recordedBy?.name, 'driver');
    });

    test('an actor this build has never heard of decodes rather than throwing', () async {
      // `Docs/07` §6: an old build on a device indefinitely, and no over-the-air fix for Dart.
      final read = await _repo(<String, Object?>{
        'data': [
          <String, Object?>{
            'id': 'm1',
            'job_id': _job,
            'milestone': 'picked_up',
            'recorded_by': 'auditor',
            'recorded_at': '2026-09-08T06:40:11.000Z',
            'accepted_at': '2026-09-08T09:15:02.481Z',
          },
        ],
        'has_more': false,
      }).repo.milestones(jobId: _job);

      expect(read.data.single.recordedBy?.name, 'unknown');
      expect(read.data.single.recordedBy?.attribution, isNull);
    });

    test('a job nothing has been recorded on is an empty page, not a 404', () async {
      final read =
          await _repo(<String, Object?>{'data': <Object?>[], 'has_more': false}).repo.milestones(
                jobId: _job,
              );

      expect(read.data, isEmpty);
      expect(read.hasMore, isFalse);
    });
  });

  group('the proof read', () {
    test('it does not page, and the envelope is flattened rather than exposed', () async {
      // A delivery has at most five recordable milestones and at most one photograph each, so
      // `has_more` is always false and `next_cursor` always null. A `cursor` parameter on this
      // method would be one no caller could usefully pass.
      final read = await _repo(<String, Object?>{
        'data': [
          <String, Object?>{
            'id': 'p1',
            'job_id': _job,
            'milestone_id': 'm1',
            'milestone': 'delivered',
            'object_key': 'proof/one',
            'content_type': 'image/jpeg',
            'content_length': 137402,
            'download_url': 'https://store.example.com/one?X-Amz-Algorithm=AWS4-HMAC-SHA256',
            'download_expires_at': '2026-09-09T04:45:00.000Z',
            'recorded_at': '2026-09-08T06:40:11.000Z',
            'accepted_at': '2026-09-08T09:15:02.000Z',
          },
        ],
        'next_cursor': null,
        'has_more': false,
      }).repo.proof(jobId: _job);

      expect(read, hasLength(1));
      expect(read.single.downloadUrl, contains('X-Amz-Algorithm'));
      expect(read.single.downloadExpiresAt, '2026-09-09T04:45:00.000Z');
      expect(read.single.isException, isFalse);
    });

    test('a reasoned exception carries the reason and no URL at all', () async {
      // SHIP-116's shape: there is no object, so there is nothing to sign a URL for. Branch on the
      // reason, never on the missing URL.
      final read = await _repo(<String, Object?>{
        'data': [
          <String, Object?>{
            'id': 'p1',
            'job_id': _job,
            'milestone_id': 'm1',
            'milestone': 'delivered',
            'exception_reason': 'camera_unavailable',
            'recorded_at': '2026-09-08T06:40:11.000Z',
            'accepted_at': '2026-09-08T09:15:02.000Z',
          },
        ],
        'has_more': false,
      }).repo.proof(jobId: _job);

      expect(read.single.isException, isTrue);
      expect(read.single.exceptionReason, ProofExceptionReason.cameraUnavailable);
      expect(read.single.downloadUrl, isNull);
    });

    test('a reason this build has never heard of decodes rather than throwing', () async {
      final read = await _repo(<String, Object?>{
        'data': [
          <String, Object?>{
            'id': 'p1',
            'job_id': _job,
            'milestone_id': 'm1',
            'milestone': 'delivered',
            'exception_reason': 'goods_refused',
            'recorded_at': '2026-09-08T06:40:11.000Z',
            'accepted_at': '2026-09-08T09:15:02.000Z',
          },
        ],
        'has_more': false,
      }).repo.proof(jobId: _job);

      expect(read.single.exceptionReason, ProofExceptionReason.unknown);
      expect(read.single.isException, isTrue, reason: 'a reason it cannot name is still a reason');
    });

    test('a job with no evidence is an empty list, not a 404', () async {
      final read =
          await _repo(<String, Object?>{'data': <Object?>[], 'has_more': false}).repo.proof(
                jobId: _job,
              );

      expect(read, isEmpty);
    });
  });
}
