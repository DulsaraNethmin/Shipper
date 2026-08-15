import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/features/bidding/bid_status.dart';

part 'received_offer.freezed.dart';
part 'received_offer.g.dart';

/// One offer on the customer's own job — the `ReceivedOffer` schema in
/// `contracts/paths/bidding.yaml` (SHIP-102a).
///
/// Every field of `Bid`, plus the two things about an offer that are not in `bids`: who made it and
/// what they are offering it with. `Docs/01` §4.3's "allow a customer to compare price, timing,
/// provider profile, vehicle, and declared capability" is the sentence this whole shape exists for,
/// and [SHIP-102]'s comparison screen is its only reader.
///
/// ## Why this is a separate type from `Bid` rather than a field added to it
///
/// `Bid` is what the **provider** who made an offer sees, and its doc comment turns on that: nothing
/// of the job travels in it, and `budget_stays_on_the_customer_side_test.dart` holds it to a closed
/// key set so a field arriving as `max_price` fails as surely as `budget_cents`. Widening it with
/// [provider] and [vehicle] would put customer-facing keys in the type that guards the
/// provider-facing rule, and the two closed sets would stop being separable.
///
/// So there are two types over one endpoint family, and the duplication is the point.
///
/// ## The customer's budget is not here either, and that is worth stating on the customer's side
///
/// It reads oddly at first — the budget is the customer's own number, and this is the customer's own
/// screen. The invariant is not about who reads this shape; it is about **who must never be able to
/// obtain it**, and the platform settles that by refusing a provider the byte-identical 404 a
/// stranger gets. What this type contributes is that there is nothing here to leak if that ever
/// slipped: no job beyond [jobId], no declaration of the provider's, and no field a budget could
/// arrive in under another name.
///
/// `budget_stays_on_the_customer_side_test.dart` holds this type to a closed key set for the same
/// reason it holds `Bid` to one, and `compare_offers_test.dart` holds the **screen** to it a second
/// time — including the words on it, which is the form a closed key set cannot see.
///
/// ## Nearly everything is nullable, for `Docs/07` §6's reason
///
/// A required field is a decode that **throws**, on a build already on a phone that cannot be fixed
/// over the air. Only what a screen genuinely branches on is required: the identifiers and the
/// status. [provider] is nullable for the same reason even though the contract requires it — an
/// older build meeting a response that dropped it should draw an offer with an unnamed provider
/// rather than refuse the whole page.
@freezed
abstract class ReceivedOffer with _$ReceivedOffer {
  const factory ReceivedOffer({
    required String id,

    /// The job this offer is against. **The only thing about the job in this shape.**
    @JsonKey(name: 'job_id') required String jobId,

    /// **Never a settable field.** A bid's status is the platform's.
    @JsonKey(unknownEnumValue: BidStatus.unknown) required BidStatus status,

    /// Which party made this offer — the provider bidding, or this customer answering them.
    ///
    /// A negotiation alternates, so the live offer on a job may be the customer's own counter. The
    /// comparison screen says so rather than presenting it as something to award: `Docs/02` §3 and
    /// `ck_bids_only_a_providers_offer_is_accepted` both refuse awarding your own counter-offer.
    @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) BidParty? offeredBy,

    /// The price being asked, in cents. AUD; there is no currency field in the MVP.
    @JsonKey(name: 'amount_cents') int? amountCents,

    /// When the provider commits to collecting, in UTC.
    @JsonKey(name: 'pickup_at') String? pickupAt,

    /// When they commit to having delivered, in UTC.
    @JsonKey(name: 'deliver_by') String? deliverBy,

    /// The conditions accompanying the offer, in the offering party's own words.
    String? message,

    /// The counter-offer that displaced this one. Absent while this is the live head.
    @JsonKey(name: 'superseded_by') String? supersededBy,

    /// Who made the offer, as much as this platform will tell a customer about them.
    ProviderSummary? provider,

    /// The vehicle the offer is made with.
    ///
    /// **Absent is the ordinary case rather than an edge case.** `bids.vehicle_id` arrived at
    /// SHIP-102a, so every offer placed before it names none — as does every offer from a provider
    /// whose client has not started sending the field. A screen that treated absence as a fault
    /// would show most of the marketplace as broken.
    VehicleSummary? vehicle,

    @JsonKey(name: 'created_at') String? createdAt,
    @JsonKey(name: 'updated_at') String? updatedAt,
  }) = _ReceivedOffer;

  const ReceivedOffer._();

  factory ReceivedOffer.fromJson(Map<String, dynamic> json) => _$ReceivedOfferFromJson(json);

  /// Whether this offer is still standing and nothing has displaced it.
  ///
  /// **Presentation only.** Whether it can actually be awarded is decided server-side on every
  /// request; this decides what is worth drawing.
  bool get isLive => status.isLive && supersededBy == null;

  /// Whether this is an offer the customer could award, as opposed to their own counter.
  ///
  /// Also presentation. `ck_bids_only_a_providers_offer_is_accepted` is the fact; this only stops
  /// the screen offering a button that the platform would refuse.
  bool get isAwardable => isLive && offeredBy == BidParty.provider;
}

/// What a customer may know about a provider who has offered on their job — the `ProviderSummary`
/// schema in `contracts/paths/bidding.yaml` (SHIP-102a).
///
/// **Deliberately small, and the smallness is a fact about the platform rather than about this
/// type.** `internal/profiles` — which owns profile detail and the five verification states of
/// `Docs/04` §4 — holds no code at all yet, and the only provider profile that exists is the
/// service area and the specialties, both of which SHIP-102a's *Done when* forbids this response
/// from carrying. So there is no trading name, no rating and no completed-job count to show, and a
/// screen must not imply otherwise.
@freezed
abstract class ProviderSummary with _$ProviderSummary {
  const factory ProviderSummary({
    required String id,

    /// Whether this provider has cleared the platform's verification: contact details confirmed, on
    /// an account in good standing.
    ///
    /// The same test the job feed applies before offering them work — so a provider who was able to
    /// bid is a provider this is `true` about, and a `false` here is worth noticing rather than
    /// worth hiding.
    @Default(false) bool verified,

    /// When the provider's account was created, in UTC. Rendered day-first.
    @JsonKey(name: 'member_since') String? memberSince,
  }) = _ProviderSummary;

  const ProviderSummary._();

  factory ProviderSummary.fromJson(Map<String, dynamic> json) => _$ProviderSummaryFromJson(json);
}

/// The vehicle an offer is made with — the `VehicleSummary` schema in
/// `contracts/paths/bidding.yaml` (SHIP-102a).
///
/// **There is no registration and there must never be one.** A plate identifies a vehicle in the
/// physical world; a customer comparing offers is choosing between them rather than meeting a
/// driver, and a losing bidder never published their plate to this customer. The platform does not
/// select the column, which is stronger than a client not rendering it.
@freezed
abstract class VehicleSummary with _$VehicleSummary {
  const factory VehicleSummary({
    required String id,

    /// The kind of vehicle, as the provider registered it.
    ///
    /// **A string rather than an enumeration**, unlike [BidStatus]. The list is `fleet`'s and is
    /// not generated from `contracts/statuses.yaml`, so a client enumeration would be a second copy
    /// of somebody else's vocabulary — and a vehicle type added on the platform would arrive as
    /// `unknown` on every phone until the next store release. `Docs/07` §6 is explicit that
    /// anything expected to change under operational pressure stays server-side.
    @Default('') String type,

    /// Omitted when the provider did not state one.
    @Default('') String make,
    @Default('') String model,

    /// What it can carry, as declared — `Docs/01` §4.3's "declared capability".
    @Default(VehicleCapacity()) VehicleCapacity capacity,
  }) = _VehicleSummary;

  const VehicleSummary._();

  factory VehicleSummary.fromJson(Map<String, dynamic> json) => _$VehicleSummaryFromJson(json);

  /// A human-readable name for the vehicle, or the empty string when the provider stated neither.
  ///
  /// The make and the model are separately optional, so all four combinations occur.
  String get description => <String>[make, model].where((part) => part.isNotEmpty).join(' ');
}

/// What a vehicle can carry, as the provider declared it — the `Capacity` schema.
///
/// **Zero means unstated rather than nothing**, which is why every field has a default rather than
/// being nullable: the platform always sends all four, and a provider who stated no maximum weight
/// has a vehicle whose capacity is unstated. [statesAnything] is what a screen branches on.
@freezed
abstract class VehicleCapacity with _$VehicleCapacity {
  const factory VehicleCapacity({
    @JsonKey(name: 'max_weight_kg') @Default(0) double maxWeightKg,
    @JsonKey(name: 'length_cm') @Default(0) int lengthCm,
    @JsonKey(name: 'width_cm') @Default(0) int widthCm,
    @JsonKey(name: 'height_cm') @Default(0) int heightCm,
  }) = _VehicleCapacity;

  const VehicleCapacity._();

  factory VehicleCapacity.fromJson(Map<String, dynamic> json) => _$VehicleCapacityFromJson(json);

  /// Whether the provider declared any of it.
  bool get statesAnything => maxWeightKg > 0 || lengthCm > 0 || widthCm > 0 || heightCm > 0;
}
