// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'account_deletion.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_AccountDeletion _$AccountDeletionFromJson(Map<String, dynamic> json) =>
    _AccountDeletion(
      id: json['id'] as String,
      state: json['state'] as String,
      requestedAt: json['requested_at'] as String,
      completesBy: json['completes_by'] as String,
      deferralReason: json['deferral_reason'] as String?,
    );

Map<String, dynamic> _$AccountDeletionToJson(_AccountDeletion instance) =>
    <String, dynamic>{
      'id': instance.id,
      'state': instance.state,
      'requested_at': instance.requestedAt,
      'completes_by': instance.completesBy,
      'deferral_reason': instance.deferralReason,
    };
