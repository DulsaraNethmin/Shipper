// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'received_offer.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_ReceivedOffer _$ReceivedOfferFromJson(Map<String, dynamic> json) =>
    _ReceivedOffer(
      id: json['id'] as String,
      jobId: json['job_id'] as String,
      status: $enumDecode(
        _$BidStatusEnumMap,
        json['status'],
        unknownValue: BidStatus.unknown,
      ),
      offeredBy: $enumDecodeNullable(
        _$BidPartyEnumMap,
        json['offered_by'],
        unknownValue: BidParty.unknown,
      ),
      amountCents: (json['amount_cents'] as num?)?.toInt(),
      pickupAt: json['pickup_at'] as String?,
      deliverBy: json['deliver_by'] as String?,
      message: json['message'] as String?,
      supersededBy: json['superseded_by'] as String?,
      provider: json['provider'] == null
          ? null
          : ProviderSummary.fromJson(json['provider'] as Map<String, dynamic>),
      vehicle: json['vehicle'] == null
          ? null
          : VehicleSummary.fromJson(json['vehicle'] as Map<String, dynamic>),
      createdAt: json['created_at'] as String?,
      updatedAt: json['updated_at'] as String?,
    );

Map<String, dynamic> _$ReceivedOfferToJson(_ReceivedOffer instance) =>
    <String, dynamic>{
      'id': instance.id,
      'job_id': instance.jobId,
      'status': _$BidStatusEnumMap[instance.status]!,
      'offered_by': _$BidPartyEnumMap[instance.offeredBy],
      'amount_cents': instance.amountCents,
      'pickup_at': instance.pickupAt,
      'deliver_by': instance.deliverBy,
      'message': instance.message,
      'superseded_by': instance.supersededBy,
      'provider': instance.provider,
      'vehicle': instance.vehicle,
      'created_at': instance.createdAt,
      'updated_at': instance.updatedAt,
    };

const _$BidStatusEnumMap = {
  BidStatus.draft: 'draft',
  BidStatus.submitted: 'submitted',
  BidStatus.countered: 'countered',
  BidStatus.accepted: 'accepted',
  BidStatus.rejected: 'rejected',
  BidStatus.withdrawn: 'withdrawn',
  BidStatus.expired: 'expired',
  BidStatus.superseded: 'superseded',
  BidStatus.unknown: 'unknown',
};

const _$BidPartyEnumMap = {
  BidParty.provider: 'provider',
  BidParty.customer: 'customer',
  BidParty.unknown: 'unknown',
};

_ProviderSummary _$ProviderSummaryFromJson(Map<String, dynamic> json) =>
    _ProviderSummary(
      id: json['id'] as String,
      verified: json['verified'] as bool? ?? false,
      memberSince: json['member_since'] as String?,
    );

Map<String, dynamic> _$ProviderSummaryToJson(_ProviderSummary instance) =>
    <String, dynamic>{
      'id': instance.id,
      'verified': instance.verified,
      'member_since': instance.memberSince,
    };

_VehicleSummary _$VehicleSummaryFromJson(Map<String, dynamic> json) =>
    _VehicleSummary(
      id: json['id'] as String,
      type: json['type'] as String? ?? '',
      make: json['make'] as String? ?? '',
      model: json['model'] as String? ?? '',
      capacity: json['capacity'] == null
          ? const VehicleCapacity()
          : VehicleCapacity.fromJson(json['capacity'] as Map<String, dynamic>),
    );

Map<String, dynamic> _$VehicleSummaryToJson(_VehicleSummary instance) =>
    <String, dynamic>{
      'id': instance.id,
      'type': instance.type,
      'make': instance.make,
      'model': instance.model,
      'capacity': instance.capacity,
    };

_VehicleCapacity _$VehicleCapacityFromJson(Map<String, dynamic> json) =>
    _VehicleCapacity(
      maxWeightKg: (json['max_weight_kg'] as num?)?.toDouble() ?? 0,
      lengthCm: (json['length_cm'] as num?)?.toInt() ?? 0,
      widthCm: (json['width_cm'] as num?)?.toInt() ?? 0,
      heightCm: (json['height_cm'] as num?)?.toInt() ?? 0,
    );

Map<String, dynamic> _$VehicleCapacityToJson(_VehicleCapacity instance) =>
    <String, dynamic>{
      'max_weight_kg': instance.maxWeightKg,
      'length_cm': instance.lengthCm,
      'width_cm': instance.widthCm,
      'height_cm': instance.heightCm,
    };
