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

  group('GET /v1/fleet/bids — the provider’s own offers (SHIP-101)', () {
    ({BiddingRepository repo, _StubAdapter adapter}) list(Object body, {int status = 200}) {
      final adapter = _StubAdapter((_) => _json(body, status: status));
      final dio = buildDio(baseUrl: 'http://localhost:8092')..httpClientAdapter = adapter;
      return (repo: ApiBiddingRepository(ApiClient(dio)), adapter: adapter);
    }

    const page = <String, Object?>{
      'data': [_bid],
      'next_cursor': 'MR9yZWNvcmQ',
      'has_more': true,
    };

    test('it is under /v1/fleet and not under a job', () async {
      // The route is off the `/v1/jobs/` tree deliberately (SHIP-101a): this is the caller's own
      // bids across every job, and a four-segment `GET /v1/jobs/{id}/<literal>` would also panic
      // Go's ServeMux while `GET /v1/jobs/open/{id}` exists. A fake repository cannot notice a
      // client that put it back under a job; this can.
      final wired = list(page);
      await wired.repo.myBids();

      expect(wired.adapter.requests.single.method, 'GET');
      expect(wired.adapter.requests.single.path, '/v1/fleet/bids');
    });

    test('it carries no idempotency key, because a read changes nothing', () async {
      final wired = list(page);
      await wired.repo.myBids();

      expect(
        wired.adapter.requests.single.headers.containsKey(ApiHeaders.idempotencyKey),
        isFalse,
      );
    });

    test('a first read sends no parameters at all', () async {
      // No `limit`: the page size is server configuration (`Docs/10` §4.5), and a number compiled
      // in here could not be corrected without a store release. No `status`: every group.
      final wired = list(page);
      await wired.repo.myBids();

      expect(wired.adapter.requests.single.queryParameters, isEmpty);
    });

    test('a group is asked for by its wire name, and a cursor goes back unchanged', () async {
      final wired = list(page);
      await wired.repo.myBids(status: BidStatus.superseded, cursor: 'MR9yZWNvcmQ');

      expect(
        wired.adapter.requests.single.queryParameters,
        <String, dynamic>{'status': 'superseded', 'cursor': 'MR9yZWNvcmQ'},
      );
    });

    test('BidStatus.unknown is never sent, because the endpoint refuses it', () async {
      // `unknown` is this client's own value for a status it has never heard of — the thing that
      // makes an old build degrade rather than throw (`Docs/07` §6). It is not one of Docs/02 §4's
      // eight, so sending it is a `400 bad_request`: a build that survived decoding a ninth status
      // would otherwise be unable to make the request that follows it.
      final wired = list(page);
      await wired.repo.myBids(status: BidStatus.unknown);

      expect(wired.adapter.requests.single.queryParameters, isEmpty);
    });

    test('the envelope is read, cursor and all', () async {
      final read = await list(page).repo.myBids();

      expect(read.data, hasLength(1));
      expect(read.data.single.status, BidStatus.submitted);
      expect(read.nextCursor, 'MR9yZWNvcmQ');
      expect(read.hasMore, isTrue);
    });

    test('the last page carries no cursor and says so', () async {
      final read = await list(<String, Object?>{
        'data': [_bid],
        'has_more': false,
      }).repo.myBids();

      expect(read.nextCursor, isNull);
      expect(read.hasMore, isFalse);
    });

    test('a provider who has never bid gets an empty page and not a failure', () async {
      // `200` with `[]`, which also covers a customer calling it: the scope is the token's and the
      // answer is empty by construction rather than by refusal.
      final read = await list(<String, Object?>{'data': <Object?>[], 'has_more': false})
          .repo
          .myBids();

      expect(read.data, isEmpty);
      expect(read.isEmpty, isTrue);
    });

    test('an unknown status or a mangled cursor is the platform’s 400, surfaced as one', () async {
      final repo = list(<String, Object?>{
        'error': {'code': 'bad_request', 'message': 'unknown status'},
      }, status: 400).repo;

      await expectLater(
        repo.myBids(cursor: 'not-a-cursor'),
        throwsA(isA<ApiErrorResponse>().having((f) => f.code, 'code', 'bad_request')),
      );
    });

    test('no listed offer carries anything of the customer’s', () async {
      // Docs/01 §4.3 on the newest provider-facing response, at the point the bytes become a model.
      // The element is the same `Bid` every other bidding endpoint answers with, so the closed key
      // set stays one list rather than two — see `budget_stays_on_the_customer_side_test.dart`.
      final read = await list(<String, Object?>{
        'data': [
          <String, Object?>{..._bid, 'budget_cents': 8675309, 'max_price': 8675309},
        ],
        'has_more': false,
      }).repo.myBids();

      final row = read.data.single.toJson();
      expect(row.keys.any((k) => k.contains('budget') || k.contains('price')), isFalse);
      expect(jsonEncode(row), isNot(contains('8675309')));
    });
  });

  group('GET /v1/jobs/{id}/bids/received — the offers on the customer’s job (SHIP-102)', () {
    ({BiddingRepository repo, _StubAdapter adapter}) offers(Object body, {int status = 200}) {
      final adapter = _StubAdapter((_) => _json(body, status: status));
      final dio = buildDio(baseUrl: 'http://localhost:8092')..httpClientAdapter = adapter;
      return (repo: ApiBiddingRepository(ApiClient(dio)), adapter: adapter);
    }

    const offer = <String, Object?>{
      ..._bid,
      'provider': <String, Object?>{
        'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e2',
        'verified': true,
        'member_since': '2026-03-04T22:15:07.412Z',
      },
      'vehicle': <String, Object?>{
        'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e5',
        'type': 'van',
        'make': 'Toyota',
        'model': 'HiAce',
        'capacity': <String, Object?>{
          'max_weight_kg': 1200,
          'length_cm': 300,
          'width_cm': 170,
          'height_cm': 160,
        },
      },
    };

    const page = <String, Object?>{
      'data': [offer],
      'next_cursor': 'MR9yZWNvcmQ',
      'has_more': true,
    };

    test('it is five segments, because four cannot be served', () async {
      // **The assertion a fake repository cannot make.** `GET /v1/jobs/{id}/bids` is what
      // `Docs/09` names and what three comments in `routes_bidding.go` reserved, and it panics Go's
      // ServeMux at registration beside `GET /v1/jobs/open/{id}` — both match `/v1/jobs/open/bids`
      // with neither more specific. A client that "corrected" this path back to the documented one
      // would compile, pass every widget test, and 404 on a device.
      final wired = offers(page);
      await wired.repo.offersOn(jobId: _job);

      expect(wired.adapter.requests.single.method, 'GET');
      expect(wired.adapter.requests.single.path, '/v1/jobs/$_job/bids/received');
    });

    test('a first read sends no parameters at all', () async {
      // **The absent `?status=` is the assertion.** The endpoint reads an absent status as
      // `submitted` — the offers standing now — and a client that sent it explicitly would ship a
      // copy of the platform's default in a build with no over-the-air path.
      final wired = offers(page);
      await wired.repo.offersOn(jobId: _job);

      expect(wired.adapter.requests.single.queryParameters, isEmpty);
    });

    test('a cursor goes back unchanged and a status by its wire name', () async {
      final wired = offers(page);
      await wired.repo.offersOn(jobId: _job, status: BidStatus.accepted, cursor: 'MR9yZWNvcmQ');

      expect(wired.adapter.requests.single.queryParameters['status'], 'accepted');
      expect(wired.adapter.requests.single.queryParameters['cursor'], 'MR9yZWNvcmQ');
    });

    test('BidStatus.unknown is never sent, because the endpoint refuses it', () async {
      final wired = offers(page);
      await wired.repo.offersOn(jobId: _job, status: BidStatus.unknown);

      expect(wired.adapter.requests.single.queryParameters, isEmpty);
    });

    test('it carries no idempotency key, because a read changes nothing', () async {
      final wired = offers(page);
      await wired.repo.offersOn(jobId: _job);

      expect(wired.adapter.requests.single.headers.keys.map((k) => k.toLowerCase()),
          isNot(contains('idempotency-key')));
    });

    test('the envelope decodes, with the provider and the vehicle on the element', () async {
      final read = await _repoOnlyReturning(page, status: 200).offersOn(jobId: _job);

      expect(read.data, hasLength(1));
      expect(read.nextCursor, 'MR9yZWNvcmQ');
      expect(read.hasMore, isTrue);

      final only = read.data.single;
      expect(only.amountCents, 45000);
      expect(only.provider?.verified, isTrue);
      expect(only.vehicle?.description, 'Toyota HiAce');
      expect(only.vehicle?.capacity.maxWeightKg, 1200);
      expect(only.vehicle?.capacity.statesAnything, isTrue);
    });

    test('an offer with no vehicle decodes, because most of them have none', () async {
      // `bids.vehicle_id` arrived at SHIP-102a, so every offer placed before it names none — as
      // does every offer from a provider whose client does not send the field. A decode that threw
      // on the ordinary case would be a screen that fails for most of the marketplace.
      final read = await _repoOnlyReturning(
        <String, Object?>{'data': [_bid], 'has_more': false},
        status: 200,
      ).offersOn(jobId: _job);

      expect(read.data.single.vehicle, isNull);
      expect(read.data.single.provider, isNull);
      expect(read.hasMore, isFalse);
      expect(read.nextCursor, isNull);
    });

    test('a job that is not the caller’s is a 404 like any other', () async {
      // A provider asking about a job they are bidding on, a stranger, and a job that does not
      // exist are one answer byte-identically. The client must not try to be more specific than
      // the platform was.
      await expectLater(
        _repoOnlyReturning(
          <String, Object?>{
            'error': <String, Object?>{'code': 'not_found', 'message': 'No such job.'},
          },
          status: 404,
        ).offersOn(jobId: _job),
        throwsA(isA<ApiErrorResponse>().having((e) => e.code, 'code', 'not_found')),
      );
    });

    test('nothing of the customer’s reaches the decoded offer', () async {
      // The wire half of the closed key set. A platform that started sending a budget — or a
      // proxy that injected one — must not produce a model with somewhere to put it.
      final read = await _repoOnlyReturning(
        <String, Object?>{
          'data': [
            <String, Object?>{
              ...offer,
              'budget_cents': 8675309,
              'max_price': 8675309,
              'service_area': ['VIC'],
              'specialties': ['refrigerated'],
            },
          ],
          'has_more': false,
        },
        status: 200,
      ).offersOn(jobId: _job);

      final encoded = jsonEncode(read.data.single.toJson());
      expect(encoded, isNot(contains('8675309')));
      expect(encoded, isNot(contains('VIC')));
      expect(encoded, isNot(contains('refrigerated')));
    });
  });
}
