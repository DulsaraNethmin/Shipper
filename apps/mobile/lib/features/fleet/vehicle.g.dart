// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'vehicle.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_Vehicle _$VehicleFromJson(Map<String, dynamic> json) => _Vehicle(
  id: json['id'] as String,
  registration: json['registration'] as String,
  vehicleType: $enumDecode(
    _$VehicleTypeEnumMap,
    json['vehicle_type'],
    unknownValue: VehicleType.unknown,
  ),
  active: json['active'] as bool,
  make: json['make'] as String?,
  model: json['model'] as String?,
  maxWeightKg: (json['max_weight_kg'] as num?)?.toDouble(),
  loadLengthCm: (json['load_length_cm'] as num?)?.toInt(),
  loadWidthCm: (json['load_width_cm'] as num?)?.toInt(),
  loadHeightCm: (json['load_height_cm'] as num?)?.toInt(),
  deactivatedAt: json['deactivated_at'] as String?,
  createdAt: json['created_at'] as String?,
  updatedAt: json['updated_at'] as String?,
);

Map<String, dynamic> _$VehicleToJson(_Vehicle instance) => <String, dynamic>{
  'id': instance.id,
  'registration': instance.registration,
  'vehicle_type': _$VehicleTypeEnumMap[instance.vehicleType]!,
  'active': instance.active,
  'make': instance.make,
  'model': instance.model,
  'max_weight_kg': instance.maxWeightKg,
  'load_length_cm': instance.loadLengthCm,
  'load_width_cm': instance.loadWidthCm,
  'load_height_cm': instance.loadHeightCm,
  'deactivated_at': instance.deactivatedAt,
  'created_at': instance.createdAt,
  'updated_at': instance.updatedAt,
};

const _$VehicleTypeEnumMap = {
  VehicleType.motorcycle: 'motorcycle',
  VehicleType.car: 'car',
  VehicleType.ute: 'ute',
  VehicleType.van: 'van',
  VehicleType.trayTruck: 'tray_truck',
  VehicleType.boxTruck: 'box_truck',
  VehicleType.refrigeratedTruck: 'refrigerated_truck',
  VehicleType.flatbed: 'flatbed',
  VehicleType.tipper: 'tipper',
  VehicleType.primeMover: 'prime_mover',
  VehicleType.trailer: 'trailer',
  VehicleType.unknown: 'unknown',
};
