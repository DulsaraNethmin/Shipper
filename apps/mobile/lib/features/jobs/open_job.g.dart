// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'open_job.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_OpenJob _$OpenJobFromJson(Map<String, dynamic> json) => _OpenJob(
  id: json['id'] as String,
  status: $enumDecode(
    _$JobStatusEnumMap,
    json['status'],
    unknownValue: JobStatus.unknown,
  ),
  pickup: json['pickup'] == null
      ? null
      : JobRegion.fromJson(json['pickup'] as Map<String, dynamic>),
  dropoff: json['dropoff'] == null
      ? null
      : JobRegion.fromJson(json['dropoff'] as Map<String, dynamic>),
  goodsDescription: json['goods_description'] as String?,
  lengthCm: (json['length_cm'] as num?)?.toInt(),
  widthCm: (json['width_cm'] as num?)?.toInt(),
  heightCm: (json['height_cm'] as num?)?.toInt(),
  weightKg: (json['weight_kg'] as num?)?.toDouble(),
  vehicleRequirement: json['vehicle_requirement'] as String?,
  handlingNotes: json['handling_notes'] as String?,
  pickupWindow: json['pickup_window'] == null
      ? null
      : JobTimeWindow.fromJson(json['pickup_window'] as Map<String, dynamic>),
  dropoffWindow: json['dropoff_window'] == null
      ? null
      : JobTimeWindow.fromJson(json['dropoff_window'] as Map<String, dynamic>),
  expiresAt: json['expires_at'] as String?,
  createdAt: json['created_at'] as String?,
);

Map<String, dynamic> _$OpenJobToJson(_OpenJob instance) => <String, dynamic>{
  'id': instance.id,
  'status': _$JobStatusEnumMap[instance.status]!,
  'pickup': instance.pickup,
  'dropoff': instance.dropoff,
  'goods_description': instance.goodsDescription,
  'length_cm': instance.lengthCm,
  'width_cm': instance.widthCm,
  'height_cm': instance.heightCm,
  'weight_kg': instance.weightKg,
  'vehicle_requirement': instance.vehicleRequirement,
  'handling_notes': instance.handlingNotes,
  'pickup_window': instance.pickupWindow,
  'dropoff_window': instance.dropoffWindow,
  'expires_at': instance.expiresAt,
  'created_at': instance.createdAt,
};

const _$JobStatusEnumMap = {
  JobStatus.draft: 'draft',
  JobStatus.open: 'open',
  JobStatus.negotiating: 'negotiating',
  JobStatus.awarded: 'awarded',
  JobStatus.driverAssigned: 'driver_assigned',
  JobStatus.enRouteToPickup: 'en_route_to_pickup',
  JobStatus.pickedUp: 'picked_up',
  JobStatus.inTransit: 'in_transit',
  JobStatus.delivered: 'delivered',
  JobStatus.completed: 'completed',
  JobStatus.cancelled: 'cancelled',
  JobStatus.disputed: 'disputed',
  JobStatus.unknown: 'unknown',
};

_JobRegion _$JobRegionFromJson(Map<String, dynamic> json) => _JobRegion(
  suburb: json['suburb'] as String? ?? '',
  state: json['state'] as String? ?? '',
  postcode: json['postcode'] as String? ?? '',
);

Map<String, dynamic> _$JobRegionToJson(_JobRegion instance) =>
    <String, dynamic>{
      'suburb': instance.suburb,
      'state': instance.state,
      'postcode': instance.postcode,
    };
