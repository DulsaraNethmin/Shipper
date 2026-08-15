// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'device_token.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_DeviceToken _$DeviceTokenFromJson(Map<String, dynamic> json) => _DeviceToken(
  id: json['id'] as String,
  platform: json['platform'] as String?,
  registeredAt: json['registered_at'] as String?,
);

Map<String, dynamic> _$DeviceTokenToJson(_DeviceToken instance) =>
    <String, dynamic>{
      'id': instance.id,
      'platform': instance.platform,
      'registered_at': instance.registeredAt,
    };
