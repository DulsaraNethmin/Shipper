// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'app_policy.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_AppPolicy _$AppPolicyFromJson(Map<String, dynamic> json) => _AppPolicy(
  unsyncedNudgeAfterSeconds:
      (json['unsynced_nudge_after_seconds'] as num?)?.toInt() ??
      compiledUnsyncedNudgeAfterSeconds,
  proofCompressionBudgetBytes:
      (json['proof_compression_budget_bytes'] as num?)?.toInt() ??
      compiledProofCompressionBudgetBytes,
);

Map<String, dynamic> _$AppPolicyToJson(_AppPolicy instance) =>
    <String, dynamic>{
      'unsynced_nudge_after_seconds': instance.unsyncedNudgeAfterSeconds,
      'proof_compression_budget_bytes': instance.proofCompressionBudgetBytes,
    };
