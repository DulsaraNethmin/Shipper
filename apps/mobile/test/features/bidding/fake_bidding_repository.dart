import 'dart:async';

import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';
import 'package:shipper/features/bidding/message.dart';
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

  /// Answers for one status only, when a test is about the read that follows an award (SHIP-104).
  ///
  /// The counterpart of [groups], and it exists for the same reason: `?status=accepted` is a
  /// **different request**, not a predicate over the rows already read, and a fake that filtered
  /// [offerPages] itself would let a screen which reasoned locally about what the award did to its
  /// list pass a test about reading the platform's record.
  Map<BidStatus, List<ApiPage<ReceivedOffer>>> offerGroups =
      <BidStatus, List<ApiPage<ReceivedOffer>>>{};

  @override
  Future<ApiPage<ReceivedOffer>> offersOn({
    required String jobId,
    BidStatus? status,
    String? cursor,
  }) async {
    // Counted per status rather than over every read, so the page a test scripted for the awarded
    // offer is not consumed by however many times the comparison was refreshed first.
    final index = offerReads.where((call) => call.status == status).length;
    offerReads.add((jobId: jobId, status: status, cursor: cursor));

    final held = offersGate;
    if (held != null) await held.future;

    final thrown = offersFailure;
    if (thrown != null) throw thrown;

    final script = status == null ? offerPages : (offerGroups[status] ?? offerPages);
    if (script.isEmpty) return const ApiPage<ReceivedOffer>(data: <ReceivedOffer>[]);
    return script[index < script.length ? index : script.length - 1];
  }

  // --- SHIP-104: awarding the job to one offer -------------------------------------------------

  /// Every award attempt, in order, with the key it carried.
  ///
  /// The key is the half worth recording: `Docs/07` §4 mints one per action and reuses it across a
  /// retry of that same action, and only a recorded sequence can tell a client that holds it from
  /// one that mints a fresh key each time — both of which look identical on screen.
  final awards = <AwardCall>[];

  /// What a successful award answers with. The platform answers with the accepted offer.
  Bid awarded = aBid(status: BidStatus.accepted);

  /// Thrown by [awardTo] instead of answering, on every call until it is cleared.
  Object? awardFailure;

  /// Held open until completed, so a test can assert what the screen shows mid-award.
  Completer<void>? awardGate;

  /// Every idempotency key an award carried, in order.
  List<String> get awardKeys => awards.map((c) => c.idempotencyKey).toList(growable: false);

  @override
  Future<Bid> awardTo({
    required String jobId,
    required String bidId,
    required String idempotencyKey,
  }) async {
    awards.add((jobId: jobId, bidId: bidId, idempotencyKey: idempotencyKey));

    final held = awardGate;
    if (held != null) await held.future;

    final thrown = awardFailure;
    if (thrown != null) throw thrown;

    return awarded;
  }

  // --- SHIP-103: the negotiation, as both parties work it ---------------------------------------

  /// Every read of a negotiation's offer chain, in order.
  ///
  /// **The count is the assertion that matters here**, not only the arguments. A counter-offer
  /// re-reads the chain rather than patching the list it is holding, because the platform also
  /// superseded the offer that was answered and this device is not entitled to decide that. A
  /// controller that wrote both facts locally would render identically and would be maintaining a
  /// second copy of `Docs/02` §2's state machine; only the extra read tells them apart.
  final chainReads = <HistoryCall>[];

  /// The chains [negotiationHistory] answers with, in order, one per read.
  ///
  /// A **list of chains** rather than one chain, so a test can say what the negotiation looked like
  /// before a counter and after it. The last is repeated once exhausted, which is what a refresh of
  /// a settled negotiation looks like.
  List<List<Bid>> chains = <List<Bid>>[<Bid>[]];

  /// Thrown by [negotiationHistory] instead of answering, on every call until it is cleared.
  Object? chainFailure;

  @override
  Future<ApiPage<Bid>> negotiationHistory({
    required String jobId,
    required String bidId,
  }) async {
    final index = chainReads.length;
    chainReads.add((jobId: jobId, bidId: bidId));

    final thrown = chainFailure;
    if (thrown != null) throw thrown;

    if (chains.isEmpty) return const ApiPage<Bid>(data: <Bid>[]);
    // `next_cursor` is always null on this endpoint and `has_more` is a truncation report rather
    // than an invitation, so the fake answers the way the platform does: one envelope, no cursor.
    return ApiPage<Bid>(data: chains[index < chains.length ? index : chains.length - 1]);
  }

  /// Every counter-offer, in order, with the difference it carried and the key it was sent under.
  ///
  /// The **difference** is what is worth recording. `BidCounter` is not an offer: anything omitted
  /// is inherited from the offer being answered, so a form that sent back every field it was showing
  /// would look identical on screen and would re-assert timing the other party had already agreed
  /// to. Asserting the body is what tells "countering on price" from "re-stating the whole offer".
  final counters = <CounterCall>[];

  /// What a successful counter answers with — the new live head.
  Bid countered = aBid(id: 'counter-1', amountCents: 40000, offeredBy: BidParty.customer);

  /// Thrown by [counterOffer] instead of answering, on every call until it is cleared.
  Object? counterFailure;

  /// Held open until completed, so a test can assert what the screen shows mid-counter.
  Completer<void>? counterGate;

  /// Every idempotency key a counter carried, in order.
  List<String> get counterKeys => counters.map((c) => c.idempotencyKey).toList(growable: false);

  /// Every counter body sent, encoded the way the transport would encode it.
  List<Map<String, dynamic>> get counterBodies =>
      counters.map((c) => c.counter.toJson()).toList(growable: false);

  @override
  Future<Bid> counterOffer({
    required String jobId,
    required String bidId,
    required BidCounter counter,
    required String idempotencyKey,
  }) async {
    counters.add((jobId: jobId, bidId: bidId, counter: counter, idempotencyKey: idempotencyKey));

    final held = counterGate;
    if (held != null) await held.future;

    final thrown = counterFailure;
    if (thrown != null) throw thrown;

    return countered;
  }

  /// Every read of a conversation, in order, with the cursor it asked from.
  final messageReads = <MessagesCall>[];

  /// The pages [messagesOn] answers with, in order. The last is repeated once exhausted.
  List<ApiPage<Message>> messagePages = <ApiPage<Message>>[
    const ApiPage<Message>(data: <Message>[]),
  ];

  /// Thrown by [messagesOn] instead of answering, on every call until it is cleared.
  Object? messagesFailure;

  @override
  Future<ApiPage<Message>> messagesOn({
    required String jobId,
    required String bidId,
    String? cursor,
  }) async {
    final index = messageReads.length;
    messageReads.add((jobId: jobId, bidId: bidId, cursor: cursor));

    final thrown = messagesFailure;
    if (thrown != null) throw thrown;

    if (messagePages.isEmpty) return const ApiPage<Message>(data: <Message>[]);
    return messagePages[index < messagePages.length ? index : messagePages.length - 1];
  }

  /// Every message sent, in order, with the key it carried.
  final sends = <SendMessageCall>[];

  /// What the platform answers a send with, or `null` to echo what was sent.
  ///
  /// **Echoing is the realistic default and the override is the interesting case.** The contract
  /// says the platform adds nothing and stores the body as written — so a screen that appended the
  /// composer's text instead of the response would look right against an echoing fake. Setting this
  /// to a message whose body differs is what separates "renders the platform's row" from "renders
  /// what was typed", and that distinction is the whole of why a retry answered `200` with an
  /// earlier attempt's message reconciles correctly.
  Message? messageReply;

  /// Thrown by [sendMessage] instead of answering, on every call until it is cleared.
  Object? sendFailure;

  /// Held open until completed, so a test can assert what the screen shows mid-send.
  Completer<void>? sendGate;

  /// Every idempotency key a message carried, in order.
  List<String> get sendKeys => sends.map((c) => c.idempotencyKey).toList(growable: false);

  @override
  Future<Message> sendMessage({
    required String jobId,
    required String bidId,
    required String body,
    required String idempotencyKey,
  }) async {
    sends.add((jobId: jobId, bidId: bidId, body: body, idempotencyKey: idempotencyKey));

    final held = sendGate;
    if (held != null) await held.future;

    final thrown = sendFailure;
    if (thrown != null) throw thrown;

    return messageReply ??
        aMessage(id: 'sent-${sends.length}', sentBy: BidParty.customer, body: body);
  }
}

/// A message as the platform answers with one.
///
/// Built from the `Message` example in `contracts/paths/bidding.yaml`, so that a field renamed in the
/// contract shows up here rather than only on a device.
///
/// **There is no budget parameter and there must never be one**, exactly as [aBid] and
/// [aReceivedOffer] have none — and the reason is sharper on this shape than on either of those.
/// This is the only response in the bidding domain carrying free text, so it is the one place a
/// disclosure could travel as a *sentence*: no field, no value, no digit. [body] is what a party
/// typed, which is theirs to write and is not the platform speaking.
Message aMessage({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e5',
  BidParty sentBy = BidParty.provider,
  String body = 'Is there a lift, or is it stairs to the second floor?',
  String? createdAt = '2026-08-15T03:30:00.000Z',
}) {
  return Message(id: id, sentBy: sentBy, body: body, createdAt: createdAt);
}

/// One recorded read of a negotiation's offer chain (SHIP-103).
typedef HistoryCall = ({String jobId, String bidId});

/// One recorded counter-offer (SHIP-103).
typedef CounterCall = ({
  String jobId,
  String bidId,
  BidCounter counter,
  String idempotencyKey,
});

/// One recorded read of a conversation (SHIP-103).
typedef MessagesCall = ({String jobId, String bidId, String? cursor});

/// One recorded message (SHIP-103).
typedef SendMessageCall = ({
  String jobId,
  String bidId,
  String body,
  String idempotencyKey,
});

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

/// One recorded award (SHIP-104).
typedef AwardCall = ({String jobId, String bidId, String idempotencyKey});
