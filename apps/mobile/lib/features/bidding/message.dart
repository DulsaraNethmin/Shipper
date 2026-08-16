import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/features/bidding/bid_status.dart';

part 'message.freezed.dart';
part 'message.g.dart';

/// One message in a conversation about a job — the `Message` schema in
/// `contracts/paths/bidding.yaml` (SHIP-97).
///
/// Counter-offers say what the terms are. This is the shape that carries the questions a price and
/// two dates cannot — "is there a lift?", "can you be there before nine?" — and it is the **only
/// response in the bidding domain that carries free text**.
///
/// ## Which is why its key set is closed on both sides of the wire
///
/// The contract says it in as many words: the platform "contributes no prose to it at all — every
/// string you receive is a key name, an identifier, an instant, which party wrote the message, or
/// the words a party typed." That is not a stylistic note. `Docs/01` §4.3 forbids the customer's
/// maximum reaching a provider **as an amount, as a band, and as a "budget supplied" indicator**,
/// and the third form is a *sentence* — no field, no value, no digit. A shape that carried one extra
/// prose field would be the one place in this domain such a sentence could travel with a name that
/// looks innocuous.
///
/// So this type is registered in `budget_stays_on_the_customer_side_test.dart`'s closed key set
/// alongside `Bid`, which fails on a field added under any name at all. The half a closed key set
/// cannot cover — a sentence composed *by the client* — is held by
/// `negotiation_test.dart`'s word-level assertions over what the screen actually renders.
///
/// ## A party's own words are their own, and that is a real limit worth stating
///
/// [body] is what somebody typed, stored unaltered. If a customer chooses to write "I can go to
/// four hundred" into a message, this client renders it, and that is **not** a breach of `Docs/01`
/// §4.3: the invariant binds the platform and the product, not the customer's own mouth. What is
/// forbidden is Shipper disclosing it. The distinction matters because a guard that tried to
/// suppress the word "budget" in party-authored text would be censoring a negotiation while leaving
/// the actual hole — the app's own copy — untouched.
///
/// ## There is no `job_id` and no `provider_id`, and that is the platform's reasoning
///
/// A message is only ever reached through its own conversation's address, so both would be the same
/// value on every element of every page. The sender's `Idempotency-Key` is not here either: it is
/// their client's token and is nothing the other party needs.
@freezed
abstract class Message with _$Message {
  const factory Message({
    required String id,

    /// Which party wrote it.
    ///
    /// **Required, and it is the one field on this shape that has to be.** `Docs/07` §6 makes a
    /// required field a decode that *throws* on a build already on a handset, so only what the app
    /// genuinely branches on earns one — and this is the whole of what a message list does. The
    /// contract says the same thing from the other side: "this cannot be inferred — a conversation
    /// does not alternate, either party may write twice in a row, and a reader may join it
    /// halfway".
    ///
    /// [BidParty] rather than an enumeration of its own. The vocabulary is `provider | customer`,
    /// which is exactly what an offer's `offered_by` carries, and a second copy of two words is a
    /// second thing to keep in step with `contracts/statuses.yaml` when a third party is ever named.
    @JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown) required BidParty sentBy,

    /// What they wrote, as they wrote it.
    ///
    /// Defaulted rather than required, unlike [sentBy], and the asymmetry is deliberate: a page
    /// whose one malformed element threw would take the whole conversation with it. An empty bubble
    /// is a worse message and a better failure.
    @Default('') String body,

    /// When the platform recorded it, in UTC with milliseconds.
    @JsonKey(name: 'created_at') String? createdAt,
  }) = _Message;

  const Message._();

  factory Message.fromJson(Map<String, dynamic> json) => _$MessageFromJson(json);

  /// Whether [viewer] wrote this message.
  ///
  /// **Presentation, and nothing else.** It decides which side of the thread a bubble is drawn on.
  /// Who may read or write this conversation is the platform's decision on every request
  /// (`Docs/07` §3): a caller who is neither party gets the byte-identical `404` a bid that does not
  /// exist gets, so a client that got this wrong would draw a bubble on the wrong side rather than
  /// disclose anything.
  bool isFrom(BidParty? viewer) => viewer != null && viewer == sentBy;
}
