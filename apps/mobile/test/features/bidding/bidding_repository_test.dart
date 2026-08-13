// What actually reaches the wire, checked against contracts/paths/bidding.yaml.
//
// The screen is tested against a fake repository, which is the right seam for it and the wrong one
// for this: a fake cannot notice that the offer went to /v1/bids rather than /v1/jobs/{id}/bids, or
// that the idempotency key was left off a state-changing request the platform refuses without one.
//
// The header is the part worth being careful about. Every state-changing request carries an
// `Idempotency-Key` and is refused without one (SHIP-15), and here the key does more than make a
// retry cheap: SHIP-84 stores it against the bid, so a retry that arrives after any cache has
// forgotten it is answered with the offer it placed rather than refused as a second bid.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';

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

({BiddingRepository repo, _StubAdapter adapter}) _repoReturning(Object body, {int status = 201}) {
  final adapter = _StubAdapter((_) => _json(body, status: status));
  final dio = buildDio(baseUrl: 'http://localhost:8092')..httpClientAdapter = adapter;
  return (repo: ApiBiddingRepository(ApiClient(dio)), adapter: adapter);
}

/// For the tests that assert on the answer rather than on the request.
BiddingRepository _repoOnlyReturning(Object body, {int status = 201}) =>
    _repoReturning(body, status: status).repo;

const _job = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1';

/// An offer as the platform answers with one.
const _bid = <String, Object?>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'job_id': _job,
  'status': 'submitted',
  'offered_by': 'provider',
  'amount_cents': 45000,
  'pickup_at': '2026-08-19T23:00:00.000Z',
  'deliver_by': '2026-08-20T07:00:00.000Z',
  'created_at': '2026-08-13T04:15:30.000Z',
  'updated_at': '2026-08-13T04:15:30.000Z',
};

const _placement = BidPlacement(
  amountCents: 45000,
  pickupAt: '2026-08-20T09:00:00+10:00',
  deliverBy: '2026-08-20T17:00:00+10:00',
  message: 'Can collect from the loading dock any time after eight.',
);

void main() {
  group('placing an offer', () {
    test('posts to the job’s own bids collection', () async {
      // The path names the job. There is no `job_id` in the body, because a bid is addressed under
      // the job it is placed on and one resource has one address.
      final (:repo, :adapter) = _repoReturning(_bid);

      await repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'k');

      expect(adapter.requests.single.method, 'POST');
      expect(adapter.requests.single.path, '/v1/jobs/$_job/bids');
    });

    test('sends the price and the two commitments, under the contract’s own names', () async {
      final (:repo, :adapter) = _repoReturning(_bid);

      await repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'k');

      expect(adapter.requests.single.data, <String, dynamic>{
        'amount_cents': 45000,
        'pickup_at': '2026-08-20T09:00:00+10:00',
        'deliver_by': '2026-08-20T17:00:00+10:00',
        'message': 'Can collect from the loading dock any time after eight.',
      });
    });

    test('carries the caller’s idempotency key and never mints one', () async {
      // Docs/07 §4: the key is generated once where the user acts and reused unchanged across every
      // retry. A repository that minted one per call would look correct, pass every test that does
      // not simulate a retry, and duplicate a provider's offer in the field.
      final (:repo, :adapter) = _repoReturning(_bid);

      await repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'the-caller-s-key');

      expect(
        adapter.requests.single.headers[ApiHeaders.idempotencyKey],
        'the-caller-s-key',
      );
    });

    test('the same key on two attempts reaches the wire unchanged', () async {
      // The property this ticket turns on. Whether the key is *held* is `ActionKey`'s decision and
      // is tested where that decision is made; what this asserts is that nothing between the
      // controller and the socket rewrites it.
      final (:repo, :adapter) = _repoReturning(_bid);

      await repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'one-action');
      await repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'one-action');

      expect(
        adapter.requests.map((r) => r.headers[ApiHeaders.idempotencyKey]),
        <String>['one-action', 'one-action'],
      );
    });

    test('a 201 and a 200 are the same answer to a caller', () async {
      // `201` is a new offer and `200` is a retry answered from the row it wrote. Both carry the
      // same shape deliberately, so a client that does not care which happened parses one type —
      // and this one genuinely does not care: what the platform holds is what comes back either
      // way.
      final created = await _repoReturning(_bid).repo.placeBid(
            jobId: _job,
            bid: _placement,
            idempotencyKey: 'k',
          );
      final replayed = await _repoReturning(_bid, status: 200).repo.placeBid(
            jobId: _job,
            bid: _placement,
            idempotencyKey: 'k',
          );

      expect(created.id, replayed.id);
      expect(created.status, BidStatus.submitted);
      expect(replayed.status, BidStatus.submitted);
    });

    test('a 409 arrives as the platform’s own code, not as a message to match on', () async {
      // Docs/07 §6: clients branch on `code` and never on `message`. Three different things answer
      // `409` here — an existing live offer, a key reused for a different request, and a request
      // still in flight — and they lead to three different places.
      final repo = _repoOnlyReturning(
        <String, Object?>{
          'error': <String, Object?>{
            'code': 'bidding_already_bid',
            'message': 'You already have a live offer on this job.',
            'request_id': '9f2c',
          },
        },
        status: 409,
      );

      await expectLater(
        repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'k'),
        throwsA(
          isA<ApiErrorResponse>()
              .having((f) => f.statusCode, 'statusCode', 409)
              .having((f) => f.code, 'code', 'bidding_already_bid')
              .having((f) => f.requestId, 'requestId', '9f2c'),
        ),
      );
    });

    test('a 422 arrives with one entry per field, keyed by the field it names', () async {
      // internal/bidding collects every problem rather than the first, so a provider who mistypes a
      // date and an amount is told about both at once. The form renders each under its own input.
      final repo = _repoOnlyReturning(
        <String, Object?>{
          'error': <String, Object?>{
            'code': 'validation_failed',
            'message': 'Some fields were rejected.',
            'details': <Object?>[
              <String, Object?>{
                'field': 'amount_cents',
                'code': 'out_of_range',
                'message': r'Enter an amount between $1 and $1000000.',
              },
              <String, Object?>{
                'field': 'pickup_at',
                'code': 'out_of_range',
                'message': 'Enter a collection time in the future.',
              },
            ],
          },
        },
        status: 422,
      );

      try {
        await repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'k');
        fail('a 422 should not resolve');
      } on ApiErrorResponse catch (failure) {
        expect(failure.fieldMessages.keys, <String>{'amount_cents', 'pickup_at'});
        expect(
          failure.fieldMessages['pickup_at'],
          'Enter a collection time in the future.',
        );
      }
    });

    test('a job this provider may not bid on is a 404 like any other', () async {
      // Which jobs a competitor may bid on is not something this API discloses, so a job outside
      // the eligibility filter and a job that does not exist are the same answer. A client must not
      // try to be more specific than the platform was.
      final repo = _repoOnlyReturning(
        <String, Object?>{
          'error': <String, Object?>{'code': 'not_found', 'message': 'No such job.'},
        },
        status: 404,
      );

      await expectLater(
        repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'k'),
        throwsA(isA<ApiErrorResponse>().having((f) => f.statusCode, 'statusCode', 404)),
      );
    });

    test('a 201 that is not a bid is a malformed response rather than a crash', () async {
      // A proxy's HTML error page, or a JSON array where an object was expected. The controller
      // treats this as an unknown outcome and keeps its idempotency key, which is the only safe
      // reading: nothing here establishes whether the offer was recorded.
      final repo = _repoOnlyReturning(<Object?>[]);

      await expectLater(
        repo.placeBid(jobId: _job, bid: _placement, idempotencyKey: 'k'),
        throwsA(isA<ApiMalformedResponse>()),
      );
    });

    test('nothing about the job comes back beyond its identifier', () async {
      // Docs/01 §4.3, made structural rather than remembered: a bid names its job and copies
      // nothing from it, so there is no budget to withhold. If the platform ever started sending
      // one, this type would still have nowhere to put it — which is `bid_test.dart`'s half.
      final bid = await _repoReturning(_bid).repo.placeBid(
            jobId: _job,
            bid: _placement,
            idempotencyKey: 'k',
          );

      expect(bid.jobId, _job);
      expect(
        bid.toJson().keys.where((k) => k.startsWith('job_')),
        <String>['job_id'],
      );
    });
  });
}
