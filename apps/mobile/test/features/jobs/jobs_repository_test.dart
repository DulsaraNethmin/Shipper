// What actually reaches the wire, checked against contracts/paths/jobs.yaml.
//
// The screens are tested against a fake repository, which is the right seam for them and the
// wrong one for this: a fake cannot notice that the client sends `drop_off` where the platform
// reads `dropoff`, or that an edit went out as a POST and created a second job. The platform
// **refuses unknown fields** on these bodies (`httpx.DecodeJSON` is strict), so a key name is not
// a detail — a wrong one is a 400 from a build already on somebody's phone.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';

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

({JobsRepository repo, _StubAdapter adapter}) _repoReturning(Object body, {int status = 200}) {
  final adapter = _StubAdapter((_) => _json(body, status: status));
  final dio = buildDio(baseUrl: 'http://localhost:8092')..httpClientAdapter = adapter;
  return (repo: ApiJobsRepository(ApiClient(dio)), adapter: adapter);
}

/// A draft as the platform answers with one.
const _draft = <String, Object?>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'status': 'draft',
  'created_at': '2026-08-11T03:30:00.000Z',
  'updated_at': '2026-08-11T03:30:00.000Z',
};

Map<String, Object?> _sentBody(RequestOptions options) {
  final Object? data = options.data;
  return data is Map<String, Object?> ? data : <String, Object?>{};
}

const _pickup = AddressInput(
  line: '12 Smith Street',
  suburb: 'Newtown',
  state: 'NSW',
  postcode: '2042',
);

const _dropoff = AddressInput(
  line: 'Lot 14 Boundary Road',
  suburb: 'Coonabarabran',
  state: 'NSW',
  postcode: '2357',
);

void main() {
  group('the locations body', () {
    test('is the two keys the contract names, each with all four parts', () {
      expect(locationsBody(pickup: _pickup, dropoff: _dropoff), <String, Object?>{
        'pickup': <String, Object?>{
          'line': '12 Smith Street',
          'suburb': 'Newtown',
          'state': 'NSW',
          'postcode': '2042',
        },
        'dropoff': <String, Object?>{
          'line': 'Lot 14 Boundary Road',
          'suburb': 'Coonabarabran',
          'state': 'NSW',
          'postcode': '2357',
        },
      });
    });

    test('carries no status, and there is no field for one', () {
      // Docs/02 §2 and CLAUDE.md: job status is never a settable field. The request schema is
      // additionalProperties: false, so a client that sent one would be told the field does not
      // exist rather than having it quietly ignored — which is the failure worth preventing,
      // because a client that believes it published a job and did not will retry forever.
      final body = locationsBody(pickup: _pickup, dropoff: _dropoff);

      expect(body.containsKey('status'), isFalse);
      expect(jsonEncode(body), isNot(contains('status')));
    });

    test('sends the state exactly as it was typed', () {
      // The platform accepts any case and the spelled-out name and normalises to the
      // abbreviation. A client that normalised first would be a second normaliser to keep in
      // step with the platform's, for no gain.
      const typed = AddressInput(state: 'new south wales');

      expect(typed.toJson()['state'], 'new south wales');
    });
  });

  group('creating a draft', () {
    test('POSTs /v1/jobs with the idempotency key it was given', () async {
      final stub = _repoReturning(_draft, status: 201);

      final job = await stub.repo.createDraft(
        fields: locationsBody(pickup: _pickup, dropoff: _dropoff),
        idempotencyKey: 'key-1',
      );

      final sent = stub.adapter.requests.single;
      expect(sent.method, 'POST');
      expect(sent.path, '/v1/jobs');
      expect(sent.headers[ApiHeaders.idempotencyKey], 'key-1');
      expect(_sentBody(sent)['pickup'], isA<Map<String, Object?>>());
      expect(job.status, JobStatus.draft);
    });
  });

  group('editing a draft', () {
    test('PATCHes /v1/jobs/{id} rather than posting a second job', () async {
      // The distinction this whole method exists for. A POST here would create a second draft
      // every time a customer corrected a postcode, and the customer would find both in their
      // job list with nothing to explain the extra one.
      final stub = _repoReturning(_draft);

      await stub.repo.updateDraft(
        jobId: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
        fields: locationsBody(pickup: _pickup, dropoff: _dropoff),
        idempotencyKey: 'key-2',
      );

      final sent = stub.adapter.requests.single;
      expect(sent.method, 'PATCH');
      expect(sent.path, '/v1/jobs/0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(sent.headers[ApiHeaders.idempotencyKey], 'key-2');
    });

    test('an edit with no idempotency key is refused before it is sent', () async {
      // The interceptor covers PATCH as well as POST (SHIP-15). Proving it here rather than
      // trusting the set: a state-changing method missing from that set is silent until the
      // platform answers 400 to a build already in the field.
      final stub = _repoReturning(_draft);

      await expectLater(
        stub.repo.updateDraft(
          jobId: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
          fields: const <String, Object?>{},
          idempotencyKey: '',
        ),
        throwsA(isA<StateError>()),
      );
      expect(stub.adapter.requests, isEmpty);
    });
  });

  group('listing', () {
    test('GETs /v1/jobs with no status, no limit and no cursor on the first page', () async {
      // Three deliberate absences. `?status=` takes one value and the app groups by all twelve,
      // so filtering would be twelve requests to draw one screen. `?limit=` is server
      // configuration and a number compiled in here cannot be changed without a store release.
      // And a read carries no idempotency key.
      final stub = _repoReturning(<String, Object?>{
        'data': <Object?>[_draft],
        'has_more': false,
      });

      final page = await stub.repo.jobs();

      final sent = stub.adapter.requests.single;
      expect(sent.method, 'GET');
      expect(sent.path, '/v1/jobs');
      expect(sent.queryParameters, isEmpty);
      expect(sent.headers.containsKey(ApiHeaders.idempotencyKey), isFalse);
      expect(page.data.single.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(page.hasMore, isFalse);
    });

    test('passes a cursor back exactly as it arrived', () async {
      const token = 'MR8yMDI2LTA4LTExVDAzOjMwOjAwWh8wMTk4ZjJjMS02YjQwLTdhMTE';
      final stub = _repoReturning(<String, Object?>{'data': <Object?>[], 'has_more': false});

      await stub.repo.jobs(cursor: token);

      // Opaque: nothing in the client reads it, rewrites it or composes one. The contract says
      // its encoding may change and that a cursor from an encoding no longer served is refused
      // rather than misread.
      expect(stub.adapter.requests.single.queryParameters, <String, Object?>{'cursor': token});
    });

    test('a customer with no jobs is an empty page rather than a failure', () async {
      final stub = _repoReturning(<String, Object?>{'data': <Object?>[], 'has_more': false});

      final page = await stub.repo.jobs();

      expect(page.data, isEmpty);
      expect(page.hasMore, isFalse);
    });
  });

  group('reading one job', () {
    test('GETs /v1/jobs/{id} with no idempotency key', () async {
      // A single resource is a bare object rather than a one-item page — the envelope is how a
      // client tells the two apart without knowing the endpoint (`Docs/10` §4.5).
      final stub = _repoReturning(<String, Object?>{
        ..._draft,
        'status': 'open',
        'budget_cents': 150000,
      });

      final job = await stub.repo.job(jobId: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');

      final sent = stub.adapter.requests.single;
      expect(sent.method, 'GET');
      expect(sent.path, '/v1/jobs/0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(sent.headers.containsKey(ApiHeaders.idempotencyKey), isFalse);
      expect(job.status, JobStatus.open);
      // The one shape in this API that carries the budget, read here by the customer who owns
      // the job (SHIP-67, `Docs/01` §4.3).
      expect(job.budgetCents, 150000);
    });
  });

  group('cancelling', () {
    test('POSTs the verb under the resource, and sends no status', () async {
      // Job status is never a settable field (`Docs/02` §2, `CLAUDE.md`). A client naming the
      // status it wants would be a client deciding a transition, and `{"status": "cancelled"}`
      // is refused as an unknown field rather than quietly obeyed.
      final stub = _repoReturning(<String, Object?>{..._draft, 'status': 'cancelled'});

      final job = await stub.repo.cancel(
        jobId: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
        idempotencyKey: 'key-3',
      );

      final sent = stub.adapter.requests.single;
      expect(sent.method, 'POST');
      expect(sent.path, '/v1/jobs/0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0/cancel');
      expect(sent.headers[ApiHeaders.idempotencyKey], 'key-3');
      expect(_sentBody(sent).containsKey('status'), isFalse);
      expect(job.status, JobStatus.cancelled);
    });

    test('an empty body is a complete request', () async {
      // The contract says so: every field of `JobCancellation` is optional, and the API takes a
      // JSON body on every state-changing request — one endpoint excepted from that would be a
      // second answer to what a request looks like.
      final stub = _repoReturning(<String, Object?>{..._draft, 'status': 'cancelled'});

      await stub.repo.cancel(jobId: 'j', idempotencyKey: 'key-4');

      expect(_sentBody(stub.adapter.requests.single), isEmpty);
    });

    test('a reason travels under the key the contract names', () async {
      // The platform refuses unknown fields on this body, so a key name is part of the contract
      // in a way a response's is not.
      final stub = _repoReturning(<String, Object?>{..._draft, 'status': 'cancelled'});

      await stub.repo.cancel(
        jobId: 'j',
        reason: 'Found a cheaper option elsewhere.',
        idempotencyKey: 'key-5',
      );

      expect(
        _sentBody(stub.adapter.requests.single),
        <String, Object?>{'reason': 'Found a cheaper option elsewhere.'},
      );
    });

    test('a cancellation with no idempotency key is refused before it is sent', () async {
      final stub = _repoReturning(_draft);

      await expectLater(
        stub.repo.cancel(jobId: 'j', idempotencyKey: ''),
        throwsA(isA<StateError>()),
      );
      expect(stub.adapter.requests, isEmpty);
    });
  });
}
