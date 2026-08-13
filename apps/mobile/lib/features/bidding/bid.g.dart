// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'bid.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_Bid _$BidFromJson(Map<String, dynamic> json) => _Bid(
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
  createdAt: json['created_at'] as String?,
  updatedAt: json['updated_at'] as String?,
);

Map<String, dynamic> _$BidToJson(_Bid instance) => <String, dynamic>{
  'id': instance.id,
  'job_id': instance.jobId,
  'status': _$BidStatusEnumMap[instance.status]!,
  'offered_by': _$BidPartyEnumMap[instance.offeredBy],
  'amount_cents': instance.amountCents,
  'pickup_at': instance.pickupAt,
  'deliver_by': instance.deliverBy,
  'message': instance.message,
  'superseded_by': instance.supersededBy,
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
