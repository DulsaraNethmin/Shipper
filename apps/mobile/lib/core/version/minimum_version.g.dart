// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'minimum_version.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_MinimumVersion _$MinimumVersionFromJson(Map<String, dynamic> json) =>
    _MinimumVersion(
      ios: PlatformFloor.fromJson(json['ios'] as Map<String, dynamic>),
      android: PlatformFloor.fromJson(json['android'] as Map<String, dynamic>),
    );

Map<String, dynamic> _$MinimumVersionToJson(_MinimumVersion instance) =>
    <String, dynamic>{'ios': instance.ios, 'android': instance.android};

_PlatformFloor _$PlatformFloorFromJson(Map<String, dynamic> json) =>
    _PlatformFloor(
      minimumBuild: (json['minimum_build'] as num?)?.toInt() ?? 0,
      storeUrl: json['store_url'] as String?,
    );

Map<String, dynamic> _$PlatformFloorToJson(_PlatformFloor instance) =>
    <String, dynamic>{
      'minimum_build': instance.minimumBuild,
      'store_url': instance.storeUrl,
    };
