import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/features/bidding/bid.dart';

/// What a provider offers against a job (SHIP-84).
///
/// One method today, which is `Docs/01` §4.2's first verb. The other four endpoints
/// `contracts/paths/bidding.yaml` serves — revise, withdraw, counter, and the history read — are
/// **served and deliberately not modelled**: an endpoint no screen calls is dead code that nothing
/// holds to the contract, which is the position `OpenJobsRepository` took about `/v1/jobs/open/{id}`
/// until the screen that needed it arrived. Revising and withdrawing arrive with SHIP-101's bid
/// list, and countering with the negotiation screens.
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
}

/// The application's bidding repository.
final biddingRepositoryProvider = Provider<BiddingRepository>(
  (ref) => ApiBiddingRepository(ref.watch(apiClientProvider)),
);
