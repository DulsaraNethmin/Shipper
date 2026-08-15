import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/received_offer.dart';

/// What a provider offers against a job (SHIP-84), what they have offered so far (SHIP-101), and
/// what a customer has been offered on theirs (SHIP-102).
///
/// Three methods. The three endpoints `contracts/paths/bidding.yaml` serves and this does not —
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

  /// `GET /v1/jobs/{id}/bids/received` (SHIP-102a) — one page of the offers on a job this customer
  /// owns, newest first.
  ///
  /// ## The path is five segments and the ticket says four, which is a routing fact
  ///
  /// `GET /v1/jobs/{id}/bids` **cannot be served**. `GET /v1/jobs/open/{id}` puts a literal where
  /// the job identifier goes, so it and any four-segment `GET /v1/jobs/{id}/<literal>` both match
  /// `/v1/jobs/open/bids` with neither more specific, and the router refuses the pair at start-up —
  /// the same collision that shaped `/v1/jobs/{id}/delivery/…`. `POST /v1/jobs/{id}/bids` is
  /// unaffected only because it is a different method, which is why placing a bid is the shorter
  /// path and reading them is not.
  ///
  /// ## Whose offers, and what a provider gets
  ///
  /// The job has to be the caller's. A provider calling this — **including one bidding on that very
  /// job** — gets the byte-identical `404` a stranger gets and a job that does not exist gets. This
  /// is the one endpoint that would otherwise hand a competitor every rival's price on a job in a
  /// single request, so a client must not try to be more specific than the platform was: there is
  /// nothing to distinguish and nothing to tell a user beyond "no such job".
  ///
  /// ## What arrives, and the one thing that does not
  ///
  /// Each element is a [ReceivedOffer]: the price, the two timing commitments, a closed
  /// [ProviderSummary], and a [VehicleSummary] when the offer names one. **No budget in any form**
  /// — nothing of the job travels beyond the job's identifier — and no service area, specialties or
  /// other jobs of the provider's.
  ///
  /// ## Which offers
  ///
  /// [status] narrows to one of `Docs/02` §4's eight, in SQL. **Omitting it is not "every status"**
  /// on this endpoint, unlike [myBids]: the default is `submitted`, the offers standing right now,
  /// because this is a comparison rather than a record and an offer that was withdrawn is not one
  /// the customer can act on. `?status=accepted` is how the awarded offer is read back afterwards.
  ///
  /// [cursor] is opaque and passed back exactly as it arrived. No idempotency key: a read changes
  /// nothing.
  Future<ApiPage<ReceivedOffer>> offersOn({
    required String jobId,
    BidStatus? status,
    String? cursor,
  });

  /// `POST /v1/jobs/{id}/award` (SHIP-104) — accept one provider's offer and end the bidding.
  ///
  /// ## A verb on the job, and the offer travels in the body
  ///
  /// The job is what moves — to `awarded`, through the one guarded transition — so the path names
  /// the job and `{"bid_id": …}` names the offer. `withdraw` and `counter` act on an offer and
  /// leave the job where it is; this ends the bidding on it.
  ///
  /// ## It is not this client's decision, and the refusals say so
  ///
  /// Five things have to be true and the platform checks all five: the job is the caller's, the bid
  /// is on **this** job, the job can still be awarded, the offer is live, and the offer is a
  /// provider's rather than the customer's own counter. A job that is not yours and one that does
  /// not exist are the **same** `404`, made before the body is read — so this endpoint cannot be
  /// used to find out whether a bid identifier names anything.
  ///
  /// [ReceivedOffer.isAwardable] is presentation only. It stops the screen drawing a button the
  /// platform would refuse; it decides nothing.
  ///
  /// ## Every other offer closes with it, and none of them are in the response
  ///
  /// In the same transaction, every offer still `submitted` becomes `rejected` (SHIP-93) —
  /// `Docs/02` §3's "awarding a job atomically marks one bid accepted and all others closed". The
  /// response is the offer that was accepted and nothing else; the job's other bids are read where
  /// they were always read, which is why the screen re-reads them with `?status=accepted`
  /// afterwards rather than reasoning about what the award did to the list it is holding.
  ///
  /// ## Retrying is safe, and it does not depend on the key
  ///
  /// The same key soon replays the stored response. **A fresh key naming the same offer is answered
  /// `200` with that offer** and records nothing further — an award is an update of a row that
  /// already exists, so applying it twice reaches the state applying it once reaches. That is the
  /// row to design against: a phone that lost its connection, was restarted and minted a new key
  /// must not be told its award failed when it succeeded.
  ///
  /// A fresh key naming a **different** offer on a job already awarded is `409 conflict`. One job,
  /// one accepted bid — enforced by a partial unique index rather than by application logic.
  ///
  /// ## Not offline, ever
  ///
  /// `Docs/07` §4 puts awarding beside bidding and negotiation as deliberately not queued. The
  /// compiler agrees: `OperationKind`'s constructor is private and its two members are both
  /// `delivery.*`.
  Future<Bid> awardTo({
    required String jobId,
    required String bidId,
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

  @override
  Future<ApiPage<ReceivedOffer>> offersOn({
    required String jobId,
    BidStatus? status,
    String? cursor,
  }) async {
    return ApiPage.fromJson(
      await _client.getJson(
        // `/received` rather than `/bids`, and the reason is in the interface above: the four
        // segment form cannot be registered beside `GET /v1/jobs/open/{id}`.
        '/v1/jobs/$jobId/bids/received',
        query: <String, dynamic>{
          // `unknown` is this client's own value for a status it has never heard of, and is not one
          // of the eight the endpoint enumerates — sending it would be a `400`. `limit` is absent
          // deliberately: the page size is server configuration (`Docs/10` §4.5).
          if (status != null && status != BidStatus.unknown) 'status': status.wireName,
          if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
        },
      ),
      ReceivedOffer.fromJson,
    );
  }

  @override
  Future<Bid> awardTo({
    required String jobId,
    required String bidId,
    required String idempotencyKey,
  }) async {
    return Bid.fromJson(
      await _client.postJson(
        '/v1/jobs/$jobId/award',
        idempotencyKey: idempotencyKey,
        // The one field the schema has. Unknown fields are refused, and there is deliberately
        // nothing here about the status of the bid or of the job — both are the platform's.
        body: <String, Object?>{'bid_id': bidId},
      ),
    );
  }
}

/// The application's bidding repository.
final biddingRepositoryProvider = Provider<BiddingRepository>(
  (ref) => ApiBiddingRepository(ref.watch(apiClientProvider)),
);
