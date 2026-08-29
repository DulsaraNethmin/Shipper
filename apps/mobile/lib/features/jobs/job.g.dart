// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'job.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_Job _$JobFromJson(Map<String, dynamic> json) => _Job(
  id: json['id'] as String,
  status: $enumDecode(
    _$JobStatusEnumMap,
    json['status'],
    unknownValue: JobStatus.unknown,
  ),
  pickup: json['pickup'] == null
      ? null
      : JobLocation.fromJson(json['pickup'] as Map<String, dynamic>),
  dropoff: json['dropoff'] == null
      ? null
      : JobLocation.fromJson(json['dropoff'] as Map<String, dynamic>),
  goodsDescription: json['goods_description'] as String?,
  goodsCategory: json['goods_category'] as String?,
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
  budgetCents: (json['budget_cents'] as num?)?.toInt(),
  termsAcceptedAt: json['terms_accepted_at'] as String?,
  expiresAt: json['expires_at'] as String?,
  createdAt: json['created_at'] as String?,
  updatedAt: json['updated_at'] as String?,
);

Map<String, dynamic> _$JobToJson(_Job instance) => <String, dynamic>{
  'id': instance.id,
  'status': _$JobStatusEnumMap[instance.status]!,
  'pickup': instance.pickup,
  'dropoff': instance.dropoff,
  'goods_description': instance.goodsDescription,
  'goods_category': instance.goodsCategory,
  'length_cm': instance.lengthCm,
  'width_cm': instance.widthCm,
  'height_cm': instance.heightCm,
  'weight_kg': instance.weightKg,
  'vehicle_requirement': instance.vehicleRequirement,
  'handling_notes': instance.handlingNotes,
  'pickup_window': instance.pickupWindow,
  'dropoff_window': instance.dropoffWindow,
  'budget_cents': instance.budgetCents,
  'terms_accepted_at': instance.termsAcceptedAt,
  'expires_at': instance.expiresAt,
  'created_at': instance.createdAt,
  'updated_at': instance.updatedAt,
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

_JobLocation _$JobLocationFromJson(Map<String, dynamic> json) => _JobLocation(
  line: json['line'] as String? ?? '',
  suburb: json['suburb'] as String? ?? '',
  state: json['state'] as String? ?? '',
  postcode: json['postcode'] as String? ?? '',
  coordinate: json['coordinate'] == null
      ? null
      : Coordinate.fromJson(json['coordinate'] as Map<String, dynamic>),
);

Map<String, dynamic> _$JobLocationToJson(_JobLocation instance) =>
    <String, dynamic>{
      'line': instance.line,
      'suburb': instance.suburb,
      'state': instance.state,
      'postcode': instance.postcode,
      'coordinate': instance.coordinate,
    };

_Coordinate _$CoordinateFromJson(Map<String, dynamic> json) => _Coordinate(
  latitude: (json['latitude'] as num?)?.toDouble() ?? 0.0,
  longitude: (json['longitude'] as num?)?.toDouble() ?? 0.0,
  formatted: json['formatted'] as String?,
);

Map<String, dynamic> _$CoordinateToJson(_Coordinate instance) =>
    <String, dynamic>{
      'latitude': instance.latitude,
      'longitude': instance.longitude,
      'formatted': instance.formatted,
    };

_JobTimeWindow _$JobTimeWindowFromJson(Map<String, dynamic> json) =>
    _JobTimeWindow(
      start: json['start'] as String?,
      end: json['end'] as String?,
    );

Map<String, dynamic> _$JobTimeWindowToJson(_JobTimeWindow instance) =>
    <String, dynamic>{'start': instance.start, 'end': instance.end};
