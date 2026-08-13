// What actually reaches the wire, checked against contracts/paths/fleet.yaml.
//
// The screen is tested against a fake repository, which is the right seam for it and the wrong one
// for this: a fake cannot notice that the feed went to /v1/jobs rather than /v1/jobs/open — which
// is the *customer's* own jobs and would answer a provider with an empty page rather than an error
// — or that a cursor was re-encoded on the way back out.
//
// The cursor is the part worth being careful about. The contract calls it opaque and says a cursor
// from an encoding no longer served is refused rather than misread, so "passed back exactly as it
// arrived" is a property of this class and of nothing else in the client.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_jobs_repository.dart';

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

ResponseBody _json(Object body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

({OpenJobsRepository repo, _StubAdapter adapter}) _repoReturning(Object body, {int status = 200}) {
  final adapter = _StubAdapter((_) => _json(body, status: status));
  final dio = buildDio(baseUrl: 'http://localhost:8092')..httpClientAdapter = adapter;
  return (repo: ApiOpenJobsRepository(ApiClient(dio)), adapter: adapter);
}

/// For the tests that assert on the answer rather than on the request.
OpenJobsRepository _repoOnlyReturning(Object body, {int status = 200}) =>
    _repoReturning(body, status: status).repo;

/// A job as the platform answers with one.
const _job = <String, Object?>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'status': 'open',
  'pickup': <String, Object?>{'suburb': 'Newtown', 'state': 'NSW', 'postcode': '2042'},
  'created_at': '2026-08-11T03:30:00.000Z',
};

/// A cursor in the shape the contract's own example gives.
const _cursor = 'MR8yMDI2LTA4LTExVDAzOjMwOjAwWh8wMTk4ZjJjMS02YjQwLTdhMTEtOWMzZS0yZjlhNGQ1MWI3ZTA';

void main() {
  group('reading the feed', () {
    test('lists from /v1/jobs/open, which is not the customer’s own /v1/jobs', () async {
      // The path is the whole of this test. /v1/jobs is the caller's own jobs and answers a
      // provider `200` with an empty page — no error, no clue, and a feed that is silently always
      // empty.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{
        'data': <Object?>[_job],
        'has_more': false,
      });

      final page = await repo.openJobs();

      expect(adapter.requests.single.method, 'GET');
      expect(adapter.requests.single.path, '/v1/jobs/open');
      expect(page.data.single.status, JobStatus.open);
      expect(page.data.single.pickup?.state, 'NSW');
    });

    test('sends no filter and no limit, because the endpoint accepts neither', () async {
      // The contract: "there is no parameter that widens this and none that narrows it — no state,
      // no vehicle, no goods type". A client that sent one would be refused, and the narrowing this
      // screen does happens on the device instead (open_jobs_filter.dart).
      //
      // `limit` is absent for the reason it is absent everywhere else: the page size is server
      // configuration, and a number compiled in here cannot be changed without a store release.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{
        'data': <Object?>[],
        'has_more': false,
      });

      await repo.openJobs();

      expect(adapter.requests.single.queryParameters, isEmpty);
    });

    test('a read carries no idempotency key', () async {
      // Reads change nothing, and the middleware lets read-only methods through untouched.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{'data': <Object?>[]});

      await repo.openJobs();

      expect(adapter.requests.single.headers.containsKey(ApiHeaders.idempotencyKey), isFalse);
    });

    test('an empty feed is a page, not a failure', () async {
      // A provider who is eligible for nothing gets `200` with `[]` — an unverified account, no
      // declared service area, no vehicle in service, or a customer who followed the wrong link.
      // None of the four is a refusal.
      final repo = _repoOnlyReturning(<String, Object?>{'data': <Object?>[], 'has_more': false});

      final page = await repo.openJobs();

      expect(page.data, isEmpty);
      expect(page.hasMore, isFalse);
      expect(page.nextCursor, isNull);
    });
  });

  group('the cursor', () {
    test('the first page names no position', () async {
      final (:repo, :adapter) = _repoReturning(<String, Object?>{'data': <Object?>[]});

      await repo.openJobs();

      expect(adapter.requests.single.queryParameters['cursor'], isNull);
    });

    test('is read out of the envelope with has_more beside it', () async {
      // `has_more` is carried rather than inferred: no cursor is also what the *first* request
      // looks like, so a client inferring the end of the list from an absent cursor would decide it
      // had reached the end before it started.
      final repo = _repoOnlyReturning(<String, Object?>{
        'data': <Object?>[_job],
        'next_cursor': _cursor,
        'has_more': true,
      });

      final page = await repo.openJobs();

      expect(page.nextCursor, _cursor);
      expect(page.hasMore, isTrue);
    });

    test('is sent back byte-for-byte, not re-encoded', () async {
      // The contract calls it opaque and refuses a cursor from an encoding it no longer serves
      // rather than misreading it. A client that url-decoded, trimmed or re-encoded it would turn
      // "the second page" into a `400` that only ever happens to somebody who scrolled.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{'data': <Object?>[]});

      await repo.openJobs(cursor: _cursor);

      expect(adapter.requests.single.queryParameters['cursor'], _cursor);
    });

    test('an empty cursor is omitted rather than sent as an empty parameter', () async {
      // `?cursor=` is a position the platform has to decide what to do with, and "the start" is
      // what sending nothing already means.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{'data': <Object?>[]});

      await repo.openJobs(cursor: '');

      expect(adapter.requests.single.queryParameters, isEmpty);
    });

    test('following the cursor is a second request to the same path', () async {
      // The second page is a separate code path a provider only reaches by asking for it, and it
      // must not become a different endpoint or acquire a parameter on the way.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{'data': <Object?>[_job]});

      await repo.openJobs();
      await repo.openJobs(cursor: _cursor);

      expect(adapter.requests.map((r) => r.path).toList(), <String>[
        '/v1/jobs/open',
        '/v1/jobs/open',
      ]);
      expect(adapter.requests.first.queryParameters, isEmpty);
      expect(adapter.requests.last.queryParameters, <String, Object?>{'cursor': _cursor});
    });
  });

  group('reading one job', () {
    test('reads /v1/jobs/open/{id}, which is not the owner’s /v1/jobs/{id}', () async {
      // The path is the whole of this test. `GET /v1/jobs/{id}` is the *owner's* view and is the one
      // shape in this API that carries the budget; a provider calling it gets `404`, and a client
      // that reached for it would be asking for the response Docs/01 §4.3 exists to keep away from
      // this half of the marketplace.
      final (:repo, :adapter) = _repoReturning(_job);

      final job = await repo.openJob(jobId: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');

      expect(adapter.requests.single.method, 'GET');
      expect(adapter.requests.single.path, '/v1/jobs/open/0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(job.status, JobStatus.open);
      expect(job.pickup?.state, 'NSW');
    });

    test('carries no idempotency key and no query', () async {
      final (:repo, :adapter) = _repoReturning(_job);

      await repo.openJob(jobId: 'a');

      expect(adapter.requests.single.headers.containsKey(ApiHeaders.idempotencyKey), isFalse);
      expect(adapter.requests.single.queryParameters, isEmpty);
    });

    test('parses the same type the feed does', () async {
      // SHIP-83 asserts on the platform's side that the detail response is byte-identical to the
      // feed entry, and this is the client's half: one type, whatever the client did to obtain the
      // job. Two shapes would be two places a budget field could be added.
      final page = await _repoOnlyReturning(<String, Object?>{
        'data': <Object?>[_job],
        'has_more': false,
      }).openJobs();
      final single = await _repoOnlyReturning(_job).openJob(jobId: 'a');

      expect(single.toJson(), page.data.single.toJson());
    });

    test('a job this provider may not bid on is a 404 like any other', () async {
      // Nine cases on the platform answer identically, the owning customer among them. The client
      // must not try to be more specific than the platform was — the indistinguishability is the
      // privacy control.
      final repo = _repoOnlyReturning(
        <String, Object?>{
          'error': <String, Object?>{'code': 'not_found', 'message': 'No such job.'},
        },
        status: 404,
      );

      await expectLater(
        repo.openJob(jobId: 'a'),
        throwsA(isA<ApiErrorResponse>().having((f) => f.statusCode, 'statusCode', 404)),
      );
    });

    test('a budget arriving anyway has nowhere to land', () async {
      // The platform holds its response to a closed set of keys, so this can never happen. The
      // client's own type is held to the same standard from the other direction — whatever the key
      // is called, there is no field for it and `toJson` cannot produce one.
      final job = await _repoOnlyReturning(<String, Object?>{
        ..._job,
        'budget_cents': 150000,
        'max_price': 150000,
      }).openJob(jobId: 'a');

      expect(job.toJson().values, isNot(contains(150000)));
      for (final key in job.toJson().keys) {
        expect(key, isNot(anyOf(contains('budget'), contains('price'), contains('maximum'))));
      }
    });
  });
}
