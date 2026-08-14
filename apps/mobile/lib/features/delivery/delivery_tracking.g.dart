// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'delivery_tracking.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_RecordedMilestone _$RecordedMilestoneFromJson(Map<String, dynamic> json) =>
    _RecordedMilestone(
      id: json['id'] as String,
      jobId: json['job_id'] as String,
      milestone: json['milestone'] as String,
      recordedBy: $enumDecodeNullable(
        _$MilestoneActorEnumMap,
        json['recorded_by'],
        unknownValue: MilestoneActor.unknown,
      ),
      reason: json['reason'] as String?,
      recordedAt: json['recorded_at'] as String?,
      acceptedAt: json['accepted_at'] as String?,
    );

Map<String, dynamic> _$RecordedMilestoneToJson(_RecordedMilestone instance) =>
    <String, dynamic>{
      'id': instance.id,
      'job_id': instance.jobId,
      'milestone': instance.milestone,
      'recorded_by': _$MilestoneActorEnumMap[instance.recordedBy],
      'reason': instance.reason,
      'recorded_at': instance.recordedAt,
      'accepted_at': instance.acceptedAt,
    };

const _$MilestoneActorEnumMap = {
  MilestoneActor.provider: 'provider',
  MilestoneActor.driver: 'driver',
  MilestoneActor.admin: 'admin',
  MilestoneActor.system: 'system',
  MilestoneActor.unknown: 'unknown',
};

_DeliveryProof _$DeliveryProofFromJson(Map<String, dynamic> json) =>
    _DeliveryProof(
      id: json['id'] as String,
      jobId: json['job_id'] as String,
      milestoneId: json['milestone_id'] as String?,
      milestone: json['milestone'] as String,
      downloadUrl: json['download_url'] as String?,
      downloadExpiresAt: json['download_expires_at'] as String?,
      exceptionReason: $enumDecodeNullable(
        _$ProofExceptionReasonEnumMap,
        json['exception_reason'],
        unknownValue: ProofExceptionReason.unknown,
      ),
      recordedAt: json['recorded_at'] as String?,
      acceptedAt: json['accepted_at'] as String?,
    );

Map<String, dynamic> _$DeliveryProofToJson(
  _DeliveryProof instance,
) => <String, dynamic>{
  'id': instance.id,
  'job_id': instance.jobId,
  'milestone_id': instance.milestoneId,
  'milestone': instance.milestone,
  'download_url': instance.downloadUrl,
  'download_expires_at': instance.downloadExpiresAt,
  'exception_reason': _$ProofExceptionReasonEnumMap[instance.exceptionReason],
  'recorded_at': instance.recordedAt,
  'accepted_at': instance.acceptedAt,
};

const _$ProofExceptionReasonEnumMap = {
  ProofExceptionReason.recipientObjected: 'recipient_objected',
  ProofExceptionReason.cameraUnavailable: 'camera_unavailable',
  ProofExceptionReason.locationUnsafe: 'location_unsafe',
  ProofExceptionReason.unknown: 'unknown',
};

_DeliveryDriver _$DeliveryDriverFromJson(Map<String, dynamic> json) =>
    _DeliveryDriver(
      jobId: json['job_id'] as String,
      driverAssigned: json['driver_assigned'] as bool,
      assignmentId: json['assignment_id'] as String?,
      driverName: json['driver_name'] as String?,
      assignedAt: json['assigned_at'] as String?,
    );

Map<String, dynamic> _$DeliveryDriverToJson(_DeliveryDriver instance) =>
    <String, dynamic>{
      'job_id': instance.jobId,
      'driver_assigned': instance.driverAssigned,
      'assignment_id': instance.assignmentId,
      'driver_name': instance.driverName,
      'assigned_at': instance.assignedAt,
    };
