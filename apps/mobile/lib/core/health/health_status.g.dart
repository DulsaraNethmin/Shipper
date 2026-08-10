// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'health_status.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_HealthStatus _$HealthStatusFromJson(Map<String, dynamic> json) =>
    _HealthStatus(
      status: json['status'] as String,
      version: json['version'] as String,
      commit: json['commit'] as String?,
      builtAt: json['built_at'] as String?,
      dirty: json['dirty'] as bool? ?? false,
      uptime: json['uptime'] as String?,
    );

Map<String, dynamic> _$HealthStatusToJson(_HealthStatus instance) =>
    <String, dynamic>{
      'status': instance.status,
      'version': instance.version,
      'commit': instance.commit,
      'built_at': instance.builtAt,
      'dirty': instance.dirty,
      'uptime': instance.uptime,
    };
