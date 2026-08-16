// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'message.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_Message _$MessageFromJson(Map<String, dynamic> json) => _Message(
  id: json['id'] as String,
  sentBy: $enumDecode(
    _$BidPartyEnumMap,
    json['sent_by'],
    unknownValue: BidParty.unknown,
  ),
  body: json['body'] as String? ?? '',
  createdAt: json['created_at'] as String?,
);

Map<String, dynamic> _$MessageToJson(_Message instance) => <String, dynamic>{
  'id': instance.id,
  'sent_by': _$BidPartyEnumMap[instance.sentBy]!,
  'body': instance.body,
  'created_at': instance.createdAt,
};

const _$BidPartyEnumMap = {
  BidParty.provider: 'provider',
  BidParty.customer: 'customer',
  BidParty.unknown: 'unknown',
};
