import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/features/bidding/bid_status.dart';

part 'bid.freezed.dart';
part 'bid.g.dart';

/// One offer against one job, as the provider who made it sees it — the `Bid` schema in
/// `contracts/paths/bidding.yaml` (SHIP-84).
///
/// One type for all five bidding operations. The platform answers `POST`, `PATCH`, the withdrawal,
/// the counter and the history with the same shape deliberately, so a client parses one thing
/// whatever it did to obtain the offer.
///
/// ## Nothing of the job travels in this shape, and that is the budget rule made structural
///
/// `Docs/01` §4.3 keeps the customer's maximum private from providers — **not as an amount, not as
/// a band, and not as a "budget supplied" flag**. This shape does not withhold the budget; it has
/// no job in it at all beyond [jobId], so there is nothing to withhold. That is stronger than
/// redaction, and it is the platform's own reasoning:
/// `TestTheBidResponseCarriesNothingOfTheCustomers` holds the serialised response to a **closed set
/// of keys at every depth**, so a field arriving as `max_price` fails as surely as one arriving as
/// `budget_cents`.
///
/// The client's half of that is `budget_stays_on_the_customer_side_test.dart`, which holds this
/// type's own round trip to the closed set below. **A field added here fails that test whatever it
/// is called**, which is the property SHIP-83 established on the platform and SHIP-100 brought
/// across the wire.
///
/// [amountCents] is **the provider's own price**, sent by this device. It has nothing to do with
/// anything the customer set, and the contract says so in as many words.
///
/// ## Why almost everything is nullable when the contract requires it
///
/// `Docs/07` §6 is built on old builds living on devices indefinitely: a required field is a decode
/// that **throws**, and a build already on a phone cannot be fixed over the air. So only what this
/// app genuinely branches on is required here, exactly as `OpenJob` does it — the contract's own
/// `required` list is longer and that is the right list for the *platform* to keep.
///
/// [status] is required because every screen reading a bid branches on it: an offer that is still
/// standing and one that was superseded are different things to be shown.
@freezed
abstract class Bid with _$Bid {
  const factory Bid({
    required String id,

    /// The job this offer is against.
    ///
    /// **The only thing about the job in this shape.** Read `GET /v1/jobs/open/{id}` for the job.
    @JsonKey(name: 'job_id') required String jobId,

    /// **Never a settable field.** A bid's status is the platform's — a provider cannot accept
    /// their own offer, and `withdrawn` is reached through an endpoint rather than by writing it.
    /// There is no `status` field in any request body in this API.
    @JsonKey(unknownEnumValue: BidStatus.unknown) required BidStatus status,

    /// Which party made this offer (SHIP-87).
    @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) BidParty? offeredBy,

    /// The price, in cents, as this device sent it. AUD; there is no currency field in the MVP.
    ///
    /// Cents as a whole number because money is never a floating-point value (`Docs/10` §3.3):
    /// `450.50` is not exactly representable and the errors accumulate.
    @JsonKey(name: 'amount_cents') int? amountCents,

    /// When the provider committed to collecting, in UTC.
    ///
    /// **An instant, not a window.** The job carries the customer's flexibility as two windows; a
    /// bid answers with two instants, because a provider says "I will be there at nine".
    @JsonKey(name: 'pickup_at') String? pickupAt,

    /// When the provider committed to having delivered, in UTC.
    @JsonKey(name: 'deliver_by') String? deliverBy,

    /// The conditions accompanying the offer, whitespace collapsed by the platform.
    ///
    /// Omitted when none was given, so a client can tell "no conditions" from "an empty note"
    /// without a second flag.
    String? message,

    /// The counter-offer that displaced this one (SHIP-88).
    ///
    /// **Omitted while this offer is the live head of its negotiation**, which is the useful case:
    /// an offer with no `superseded_by` is the only one that can be countered and the only one the
    /// customer can award.
    @JsonKey(name: 'superseded_by') String? supersededBy,

    @JsonKey(name: 'created_at') String? createdAt,
    @JsonKey(name: 'updated_at') String? updatedAt,
  }) = _Bid;

  const Bid._();

  factory Bid.fromJson(Map<String, dynamic> json) => _$BidFromJson(json);

  /// Whether this offer is still standing and nothing has displaced it.
  ///
  /// **Presentation only.** See [BidStatus.isLive]: what may be done to an offer is decided
  /// server-side on every request, and this decides only what is worth drawing.
  bool get isLive => status.isLive && supersededBy == null;
}

/// What a provider is offering — the `BidPlacement` schema in `contracts/paths/bidding.yaml`.
///
/// A separate type from [Bid], for the reason the platform writes a separate `bidRequest`: there is
/// no field here for whose offer this is and none for its status. The bidder is whoever the token
/// says is calling, and the status is the platform's. A provider id in a body would be an
/// authorisation decision made from client input, which `Docs/07` §3 puts on the platform.
///
/// The schema is `additionalProperties: false`, so an unknown field is **refused** rather than
/// ignored — a client typo fails loudly. [toJson] therefore sends exactly the four keys and no
/// more, and `message` is omitted rather than sent empty when there is nothing to say.
final class BidPlacement {
  const BidPlacement({
    required this.amountCents,
    required this.pickupAt,
    required this.deliverBy,
    this.message = '',
  });

  /// The provider's price for the whole job, in cents.
  final int amountCents;

  /// RFC 3339, with the device's own offset. Sent as typed rather than converted to UTC here: the
  /// platform parses RFC 3339 and normalises, and a client that shifted the instant itself would be
  /// a second timezone conversion to keep correct.
  final String pickupAt;
  final String deliverBy;

  /// Conditions accompanying the offer, in the provider's own words. Optional.
  final String message;

  Map<String, dynamic> toJson() => <String, dynamic>{
        'amount_cents': amountCents,
        'pickup_at': pickupAt,
        'deliver_by': deliverBy,
        // Omitted rather than sent as `""`. The contract's `maxLength` accepts an empty string and
        // storing one would make "no conditions" and "an empty note" the same thing on a screen
        // that renders the message.
        if (message.trim().isNotEmpty) 'message': message.trim(),
      };
}
