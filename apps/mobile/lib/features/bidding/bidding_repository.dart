import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';

/// What a provider offers against a job (SHIP-84), and what they have offered so far (SHIP-101).
///
/// Two methods. The three endpoints `contracts/paths/bidding.yaml` serves and this does not —
/// revise, withdraw and counter — are **served and deliberately not modelled**: an endpoint no
/// screen calls is dead code that nothing holds to the contract, which is the position
/// `OpenJobsRepository` took about `/v1/jobs/open/{id}` until the screen that needed it arrived.
/// Revising and withdrawing want a confirmation flow of their own, and countering arrives with the
/// negotiation screens (SHIP-103).
///
/// An interface with one real implementation, following the three repositories before it: a widget
/// test has to be able to hand a screen something that answers, and a stub transport under a
/// concrete class makes every screen test a test of `dio`'s wiring as well.
abstract interface class BiddingRepository {
  /// `POST /v1/jobs/{id}/bids` — offer to carry [jobId] for a price and against two commitments
  /// about timing.
  ///
  /// ## The key is the caller's, and it is the whole of what stops a retry becoming a second bid
  ///
  /// [idempotencyKey] is required rather than optional, and `ApiClient.postJson` requires it too, so
  /// forgetting it does not compile. `Docs/07` §4 has the key generated **once where the user acts**
  /// and reused unchanged across every retry of that action; `ActionKey` is what holds it, and
  /// `PlaceBidController` is where it is held.
  ///
  /// **Two guarantees sit behind that and they are not the same one** (SHIP-84):
  ///
  /// - A *second offer* on a job this provider already has a live offer on is refused with
  ///   `409 bidding_already_bid`, by `uq_bids_one_submitted_per_provider_per_job`.
  /// - A *retry* is answered `200` with the offer it placed, by `uq_bids_idempotency` on
  ///   `(job_id, provider_id, idempotency_key)` — **a stored column rather than a cached response**,
  ///   so a phone that was out of signal for a day still gets its own bid back rather than a
  ///   conflict. Redis makes the retry cheap; the column makes it correct.
  ///
  /// The caller cannot tell `201` from `200` and does not need to: both answer with the same shape,
  /// and what the platform holds is what comes back either way.
  ///
  /// ## Eligibility is not this client's decision
  ///
  /// A job this provider may not bid on and a job that does not exist are **the same answer** —
  /// `404`, byte-identically. Which jobs a competitor may bid on is not something this API
  /// discloses, and a client must not try to be more specific than the platform was.
  ///
  /// ## This does not go through the offline queue, and that is `Docs/07` §4
  ///
  /// "What is deliberately **not** offline: bidding, awarding, and negotiation. These are
  /// competitive, time-sensitive, and multi-party; a stale local decision is worse than an honest
  /// 'you are offline.'" `core/queue`'s `OperationKind` has a private constructor and exactly two
  /// members, both `delivery.*`, so a bid **cannot** be enqueued — the set is closed by the
  /// compiler rather than by a rule somebody follows.
  Future<Bid> placeBid({
    required String jobId,
    required BidPlacement bid,
    required String idempotencyKey,
  });

  /// `GET /v1/fleet/bids` (SHIP-101a) — one page of every offer in every negotiation the calling
  /// provider is in, newest first.
  ///
  /// ## It is under `/v1/fleet` and not under a job, which is a fact about the resource
  ///
  /// This is the caller's own bids **across every job**, not a filter on one job's offers. `/v1/fleet`
  /// is where a provider's own things already live — their vehicles, their service area, their
  /// profile — and a provider asking what they have bid on is asking about their operation.
  ///
  /// ## Whose offers are in it, and why the customer's counters are
  ///
  /// Every row of every negotiation this provider is a party to, **including the customer's
  /// counter-offers**. Those carry the provider's id and are told apart by [Bid.offeredBy]. That is
  /// deliberate rather than a leak: a counter from the customer is the one row waiting for an answer
  /// from this provider, and a list of their own rows alone would hide it.
  ///
  /// The scope is the token's. There is no parameter that widens it, so another provider's offer is
  /// not refused — it is never selected, and a customer who called this would get an empty page
  /// rather than a refusal.
  ///
  /// ## `Docs/01` §4.3 holds here by construction rather than by redaction
  ///
  /// The element is the same [Bid] every other bidding endpoint answers with, and nothing of the job
  /// travels in it beyond [Bid.jobId] — so there is no budget field to omit, because there is no job
  /// in the shape. `budget_stays_on_the_customer_side_test.dart` holds that type to a closed key set,
  /// and `my_bids_test.dart` holds the *screen* to it a second time.
  ///
  /// ## The two ways to group, and this models both
  ///
  /// [status] narrows to one of `Docs/02` §4's eight, in SQL rather than over the page — omit it and
  /// every status arrives with a `status` on each row for a client to group itself. The endpoint has
  /// no opinion about which; `MyBidsController` uses the first when a provider picks a group and the
  /// second when they have not.
  ///
  /// [BidStatus.unknown] is never sent. It is not one of the eight and the endpoint refuses it with
  /// `400 bad_request`, so a build that met a ninth status would otherwise turn a decode it survived
  /// into a request it cannot make.
  ///
  /// [cursor] is the `next_cursor` of a previous page, passed back **exactly** as it arrived. It is
  /// opaque, and one this endpoint did not issue is refused rather than misread.
  ///
  /// No idempotency key: a read changes nothing and the middleware lets read-only methods through.
  Future<ApiPage<Bid>> myBids({BidStatus? status, String? cursor});
}

/// The real one, over [ApiClient].
///
/// Thin by construction. `Docs/10` §8.1 replaces hand-written calls with a client generated from
/// `contracts/openapi.yaml`, and what should survive that is the shape of the interface above and
/// nothing in this class.
final class ApiBiddingRepository implements BiddingRepository {
  const ApiBiddingRepository(this._client);

  final ApiClient _client;

  @override
  Future<Bid> placeBid({
    required String jobId,
    required BidPlacement bid,
    required String idempotencyKey,
  }) async {
    return Bid.fromJson(
      // Product endpoints live under `/v1` (SHIP-13). The base URL carries the host and nothing
      // else, so the version prefix belongs here.
      await _client.postJson(
        '/v1/jobs/$jobId/bids',
        idempotencyKey: idempotencyKey,
        body: bid.toJson(),
      ),
    );
  }

  @override
  Future<ApiPage<Bid>> myBids({BidStatus? status, String? cursor}) async {
    return ApiPage.fromJson(
      await _client.getJson(
        '/v1/fleet/bids',
        query: <String, dynamic>{
          // `unknown` is this client's own value for a status it has never heard of. It is not one
          // of the eight the endpoint enumerates, so sending it would be a `400` — see the
          // interface. `limit` is deliberately absent: the page size is server configuration
          // (`Docs/10` §4.5), and a number compiled in here could not be changed without a store
          // release.
          if (status != null && status != BidStatus.unknown) 'status': status.wireName,
          if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
        },
      ),
      Bid.fromJson,
    );
  }
}

/// The application's bidding repository.
final biddingRepositoryProvider = Provider<BiddingRepository>(
  (ref) => ApiBiddingRepository(ref.watch(apiClientProvider)),
);
