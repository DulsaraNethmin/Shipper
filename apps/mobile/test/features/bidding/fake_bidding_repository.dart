import 'dart:async';

import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';
import 'package:shipper/features/bidding/received_offer.dart';

/// An offer as the platform answers with one.
///
/// Built from the `Bid` example in `contracts/paths/bidding.yaml`, so that a field renamed in the
/// contract shows up here rather than only on a device.
///
/// **There is no budget parameter and there must never be one.** `Docs/01` §4.3 keeps the customer's
/// maximum away from a provider in every form, and a fixture that could carry one would be the first
/// place a screen could be written against a field the platform does not send. [amountCents] is the
/// **provider's own price**, which is a different number entirely.
Bid aBid({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  String jobId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  BidStatus status = BidStatus.submitted,
  BidParty? offeredBy = BidParty.provider,
  int? amountCents = 45000,
  String? pickupAt = '2026-08-19T23:00:00.000Z',
  String? deliverBy = '2026-08-20T07:00:00.000Z',
  String? message,
  String? supersededBy,
  String? createdAt = '2026-08-13T04:15:30.000Z',
  String? updatedAt = '2026-08-13T04:15:30.000Z',
}) {
  return Bid(
    id: id,
    jobId: jobId,
    status: status,
    offeredBy: offeredBy,
    amountCents: amountCents,
    pickupAt: pickupAt,
    deliverBy: deliverBy,
    message: message,
    supersededBy: supersededBy,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

/// One recorded placement.
typedef PlacementCall = ({String jobId, BidPlacement bid, String idempotencyKey});

/// One recorded read of the provider's own bids (SHIP-101).
typedef MyBidsCall = ({BidStatus? status, String? cursor});

/// A [BiddingRepository] that answers from a script and records what it was asked.
///
/// The screen is tested against this rather than against a stub transport, because a widget test
/// that also exercises `dio`'s wiring fails for two reasons and reads as one. What actually reaches
/// the wire is `bidding_repository_test.dart`'s subject, against the contract.
///
/// It records **the idempotency key of every attempt**, which is the property this ticket turns on:
/// a dropped connection followed by a retry must carry the key the first attempt carried, and a
/// corrected offer must not.
class FakeBiddingRepository implements BiddingRepository {
  final calls = <PlacementCall>[];

  /// What a successful placement answers with.
  Bid placed = aBid();

  /// Thrown instead of answering, on every call until it is cleared. `null` never fails.
  ///
  /// Settable mid-test on purpose: "a dropped connection and then a retry" needs an attempt that
  /// fails and then one that does not.
  Object? failure;

  /// Held open until completed, so a test can assert what the screen shows while an offer is
  /// genuinely in flight rather than already answered.
  Completer<void>? gate;

  int get attempts => calls.length;

  /// Every idempotency key sent, in order.
  List<String> get keys => calls.map((c) => c.idempotencyKey).toList(growable: false);

  /// Every body sent, decoded the way the transport would encode it.
  List<Map<String, dynamic>> get bodies =>
      calls.map((c) => c.bid.toJson()).toList(growable: false);

  @override
  Future<Bid> placeBid({
    required String jobId,
    required BidPlacement bid,
    required String idempotencyKey,
  }) async {
    calls.add((jobId: jobId, bid: bid, idempotencyKey: idempotencyKey));

    final held = gate;
    if (held != null) await held.future;

    final thrown = failure;
    if (thrown != null) throw thrown;

    return placed;
  }

  // --- SHIP-101: the provider's own bids ------------------------------------------------------

  /// Every read of the bid list, in order, with what it asked for.
  ///
  /// The **status** is the half worth recording: `?status=` runs in SQL on the platform, so a screen
  /// that narrowed on the device instead would still draw the right rows and would silently be
  /// asking a different question — one whose `has_more` is about a list the provider is not looking
  /// at. Asserting the parameter is what tells the two apart.
  final reads = <MyBidsCall>[];

  /// The pages [myBids] answers with, in order. The last is repeated once exhausted, which is what
  /// a refresh of a one-page list looks like.
  List<ApiPage<Bid>> pages = <ApiPage<Bid>>[const ApiPage<Bid>(data: <Bid>[])];

  /// Answers for one group only, when a test is about `?status=` narrowing server-side.
  ///
  /// Keyed by the status asked for; a status with no entry falls back to [pages]. It exists because
  /// the narrowing is a **different request** rather than a predicate over the same rows, and a fake
  /// that filtered [pages] itself would make a screen that narrowed on the device pass.
  Map<BidStatus, List<ApiPage<Bid>>> groups = <BidStatus, List<ApiPage<Bid>>>{};

  /// Thrown by [myBids] instead of answering, on every call until it is cleared.
  Object? readFailure;

  /// Held open until completed, so a test can assert what the screen shows mid-read.
  Completer<void>? readGate;

  @override
  Future<ApiPage<Bid>> myBids({BidStatus? status, String? cursor}) async {
    final index = reads.where((call) => call.status == status).length;
    reads.add((status: status, cursor: cursor));

    final held = readGate;
    if (held != null) await held.future;

    final thrown = readFailure;
    if (thrown != null) throw thrown;

    final script = status == null ? pages : (groups[status] ?? pages);
    if (script.isEmpty) return const ApiPage<Bid>(data: <Bid>[]);
    return script[index < script.length ? index : script.length - 1];
  }

  // --- SHIP-102: the offers on the customer's job ----------------------------------------------

  /// Every read of a job's offers, in order, with what it asked for.
  ///
  /// **The `status` is worth recording for the opposite reason it is on [reads].** This endpoint's
  /// default is not "every status" — it is `submitted` — so a screen that sent `?status=submitted`
  /// explicitly would draw exactly the right rows while shipping a copy of the platform's default
  /// in a build that cannot be updated over the air. Asserting the parameter is `null` is what tells
  /// the two apart.
  final offerReads = <OffersCall>[];

  /// The pages [offersOn] answers with, in order. The last is repeated once exhausted, which is what
  /// a refresh of a one-page list looks like.
  List<ApiPage<ReceivedOffer>> offerPages = <ApiPage<ReceivedOffer>>[
    const ApiPage<ReceivedOffer>(data: <ReceivedOffer>[]),
  ];

  /// Thrown by [offersOn] instead of answering, on every call until it is cleared.
  Object? offersFailure;

  /// Held open until completed, so a test can assert what the screen shows mid-read.
  Completer<void>? offersGate;

  @override
  Future<ApiPage<ReceivedOffer>> offersOn({
    required String jobId,
    BidStatus? status,
    String? cursor,
  }) async {
    final index = offerReads.length;
    offerReads.add((jobId: jobId, status: status, cursor: cursor));

    final held = offersGate;
    if (held != null) await held.future;

    final thrown = offersFailure;
    if (thrown != null) throw thrown;

    if (offerPages.isEmpty) return const ApiPage<ReceivedOffer>(data: <ReceivedOffer>[]);
    return offerPages[index < offerPages.length ? index : offerPages.length - 1];
  }
}

// --- SHIP-102: the customer's view of the offers on their job ---------------------------------

/// One offer as the platform answers with one on `GET /v1/jobs/{id}/bids/received`.
///
/// Built from the `ReceivedOffer` example in `contracts/paths/bidding.yaml`, so that a field renamed
/// in the contract shows up here rather than only on a device.
///
/// **There is no budget parameter and there must never be one**, exactly as [aBid] has none — and
/// on this side the reason needs stating rather than inheriting. This is the *customer's* own
/// screen, so the instinct is that their own maximum would be harmless on it. The rule is not about
/// who reads the shape; it is about what the client grows somewhere to put. A fixture that could
/// carry a budget is the first place a screen could be written against a field the platform does not
/// send, and the screen would then be one careless change away from being reachable by a provider.
///
/// [amountCents] is the **offering party's** price, which is a different number entirely.
ReceivedOffer aReceivedOffer({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  String jobId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  BidStatus status = BidStatus.submitted,
  BidParty? offeredBy = BidParty.provider,
  int? amountCents = 45000,
  String? pickupAt = '2026-08-19T23:00:00.000Z',
  String? deliverBy = '2026-08-20T07:00:00.000Z',
  String? message,
  String? supersededBy,
  ProviderSummary? provider = const ProviderSummary(
    id: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e2',
    verified: true,
    memberSince: '2026-03-04T22:15:07.412Z',
  ),
  VehicleSummary? vehicle,
  String? createdAt = '2026-08-13T04:15:30.000Z',
  String? updatedAt = '2026-08-13T04:15:30.000Z',
}) {
  return ReceivedOffer(
    id: id,
    jobId: jobId,
    status: status,
    offeredBy: offeredBy,
    amountCents: amountCents,
    pickupAt: pickupAt,
    deliverBy: deliverBy,
    message: message,
    supersededBy: supersededBy,
    provider: provider,
    vehicle: vehicle,
    createdAt: createdAt,
    updatedAt: updatedAt,
  );
}

/// A vehicle as the platform describes one to a customer.
///
/// **No registration parameter**, for the same reason the schema has no field: a plate identifies a
/// vehicle in the physical world, and a losing bidder never published theirs to this customer.
VehicleSummary aVehicle({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e5',
  String type = 'van',
  String make = 'Toyota',
  String model = 'HiAce',
  VehicleCapacity capacity = const VehicleCapacity(
    maxWeightKg: 1200,
    lengthCm: 300,
    widthCm: 170,
    heightCm: 160,
  ),
}) {
  return VehicleSummary(id: id, type: type, make: make, model: model, capacity: capacity);
}

/// One recorded read of the offers on a job (SHIP-102).
typedef OffersCall = ({String jobId, BidStatus? status, String? cursor});
