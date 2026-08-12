// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'vehicle.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$Vehicle {

 String get id;/// The plate, upper case with its spaces removed — the platform's stored form, not what was
/// typed. `abc 123` and `ABC123` are one vehicle rather than two.
 String get registration;@JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown) VehicleType get vehicleType;/// Whether the vehicle is in service. See the note on this class.
 bool get active; String? get make; String? get model;/// The most it can carry, in **kilograms**. `null` means the provider has not stated one, which
/// is a different thing from a capacity of zero.
@JsonKey(name: 'max_weight_kg') double? get maxWeightKg;/// The **load space**, in centimetres — not the vehicle's own length. A customer's 190 cm sofa
/// has to fit inside, not alongside.
@JsonKey(name: 'load_length_cm') int? get loadLengthCm;@JsonKey(name: 'load_width_cm') int? get loadWidthCm;@JsonKey(name: 'load_height_cm') int? get loadHeightCm;/// When the provider took it out of service. **Absent while it is in service**, which is what
/// lets a screen show "off the road since 3 Aug 2026" without a second field meaning the same
/// thing as [active].
@JsonKey(name: 'deactivated_at') String? get deactivatedAt;@JsonKey(name: 'created_at') String? get createdAt;@JsonKey(name: 'updated_at') String? get updatedAt;
/// Create a copy of Vehicle
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$VehicleCopyWith<Vehicle> get copyWith => _$VehicleCopyWithImpl<Vehicle>(this as Vehicle, _$identity);

  /// Serializes this Vehicle to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is Vehicle&&(identical(other.id, id) || other.id == id)&&(identical(other.registration, registration) || other.registration == registration)&&(identical(other.vehicleType, vehicleType) || other.vehicleType == vehicleType)&&(identical(other.active, active) || other.active == active)&&(identical(other.make, make) || other.make == make)&&(identical(other.model, model) || other.model == model)&&(identical(other.maxWeightKg, maxWeightKg) || other.maxWeightKg == maxWeightKg)&&(identical(other.loadLengthCm, loadLengthCm) || other.loadLengthCm == loadLengthCm)&&(identical(other.loadWidthCm, loadWidthCm) || other.loadWidthCm == loadWidthCm)&&(identical(other.loadHeightCm, loadHeightCm) || other.loadHeightCm == loadHeightCm)&&(identical(other.deactivatedAt, deactivatedAt) || other.deactivatedAt == deactivatedAt)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt)&&(identical(other.updatedAt, updatedAt) || other.updatedAt == updatedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,registration,vehicleType,active,make,model,maxWeightKg,loadLengthCm,loadWidthCm,loadHeightCm,deactivatedAt,createdAt,updatedAt);

@override
String toString() {
  return 'Vehicle(id: $id, registration: $registration, vehicleType: $vehicleType, active: $active, make: $make, model: $model, maxWeightKg: $maxWeightKg, loadLengthCm: $loadLengthCm, loadWidthCm: $loadWidthCm, loadHeightCm: $loadHeightCm, deactivatedAt: $deactivatedAt, createdAt: $createdAt, updatedAt: $updatedAt)';
}


}

/// @nodoc
abstract mixin class $VehicleCopyWith<$Res>  {
  factory $VehicleCopyWith(Vehicle value, $Res Function(Vehicle) _then) = _$VehicleCopyWithImpl;
@useResult
$Res call({
 String id, String registration,@JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown) VehicleType vehicleType, bool active, String? make, String? model,@JsonKey(name: 'max_weight_kg') double? maxWeightKg,@JsonKey(name: 'load_length_cm') int? loadLengthCm,@JsonKey(name: 'load_width_cm') int? loadWidthCm,@JsonKey(name: 'load_height_cm') int? loadHeightCm,@JsonKey(name: 'deactivated_at') String? deactivatedAt,@JsonKey(name: 'created_at') String? createdAt,@JsonKey(name: 'updated_at') String? updatedAt
});




}
/// @nodoc
class _$VehicleCopyWithImpl<$Res>
    implements $VehicleCopyWith<$Res> {
  _$VehicleCopyWithImpl(this._self, this._then);

  final Vehicle _self;
  final $Res Function(Vehicle) _then;

/// Create a copy of Vehicle
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? registration = null,Object? vehicleType = null,Object? active = null,Object? make = freezed,Object? model = freezed,Object? maxWeightKg = freezed,Object? loadLengthCm = freezed,Object? loadWidthCm = freezed,Object? loadHeightCm = freezed,Object? deactivatedAt = freezed,Object? createdAt = freezed,Object? updatedAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,registration: null == registration ? _self.registration : registration // ignore: cast_nullable_to_non_nullable
as String,vehicleType: null == vehicleType ? _self.vehicleType : vehicleType // ignore: cast_nullable_to_non_nullable
as VehicleType,active: null == active ? _self.active : active // ignore: cast_nullable_to_non_nullable
as bool,make: freezed == make ? _self.make : make // ignore: cast_nullable_to_non_nullable
as String?,model: freezed == model ? _self.model : model // ignore: cast_nullable_to_non_nullable
as String?,maxWeightKg: freezed == maxWeightKg ? _self.maxWeightKg : maxWeightKg // ignore: cast_nullable_to_non_nullable
as double?,loadLengthCm: freezed == loadLengthCm ? _self.loadLengthCm : loadLengthCm // ignore: cast_nullable_to_non_nullable
as int?,loadWidthCm: freezed == loadWidthCm ? _self.loadWidthCm : loadWidthCm // ignore: cast_nullable_to_non_nullable
as int?,loadHeightCm: freezed == loadHeightCm ? _self.loadHeightCm : loadHeightCm // ignore: cast_nullable_to_non_nullable
as int?,deactivatedAt: freezed == deactivatedAt ? _self.deactivatedAt : deactivatedAt // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,updatedAt: freezed == updatedAt ? _self.updatedAt : updatedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [Vehicle].
extension VehiclePatterns on Vehicle {
/// A variant of `map` that fallback to returning `orElse`.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case _:
///     return orElse();
/// }
/// ```

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _Vehicle value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _Vehicle() when $default != null:
return $default(_that);case _:
  return orElse();

}
}
/// A `switch`-like method, using callbacks.
///
/// Callbacks receives the raw object, upcasted.
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case final Subclass2 value:
///     return ...;
/// }
/// ```

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _Vehicle value)  $default,){
final _that = this;
switch (_that) {
case _Vehicle():
return $default(_that);case _:
  throw StateError('Unexpected subclass');

}
}
/// A variant of `map` that fallback to returning `null`.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case final Subclass value:
///     return ...;
///   case _:
///     return null;
/// }
/// ```

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _Vehicle value)?  $default,){
final _that = this;
switch (_that) {
case _Vehicle() when $default != null:
return $default(_that);case _:
  return null;

}
}
/// A variant of `when` that fallback to an `orElse` callback.
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case _:
///     return orElse();
/// }
/// ```

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id,  String registration, @JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown)  VehicleType vehicleType,  bool active,  String? make,  String? model, @JsonKey(name: 'max_weight_kg')  double? maxWeightKg, @JsonKey(name: 'load_length_cm')  int? loadLengthCm, @JsonKey(name: 'load_width_cm')  int? loadWidthCm, @JsonKey(name: 'load_height_cm')  int? loadHeightCm, @JsonKey(name: 'deactivated_at')  String? deactivatedAt, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _Vehicle() when $default != null:
return $default(_that.id,_that.registration,_that.vehicleType,_that.active,_that.make,_that.model,_that.maxWeightKg,_that.loadLengthCm,_that.loadWidthCm,_that.loadHeightCm,_that.deactivatedAt,_that.createdAt,_that.updatedAt);case _:
  return orElse();

}
}
/// A `switch`-like method, using callbacks.
///
/// As opposed to `map`, this offers destructuring.
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case Subclass2(:final field2):
///     return ...;
/// }
/// ```

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id,  String registration, @JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown)  VehicleType vehicleType,  bool active,  String? make,  String? model, @JsonKey(name: 'max_weight_kg')  double? maxWeightKg, @JsonKey(name: 'load_length_cm')  int? loadLengthCm, @JsonKey(name: 'load_width_cm')  int? loadWidthCm, @JsonKey(name: 'load_height_cm')  int? loadHeightCm, @JsonKey(name: 'deactivated_at')  String? deactivatedAt, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)  $default,) {final _that = this;
switch (_that) {
case _Vehicle():
return $default(_that.id,_that.registration,_that.vehicleType,_that.active,_that.make,_that.model,_that.maxWeightKg,_that.loadLengthCm,_that.loadWidthCm,_that.loadHeightCm,_that.deactivatedAt,_that.createdAt,_that.updatedAt);case _:
  throw StateError('Unexpected subclass');

}
}
/// A variant of `when` that fallback to returning `null`
///
/// It is equivalent to doing:
/// ```dart
/// switch (sealedClass) {
///   case Subclass(:final field):
///     return ...;
///   case _:
///     return null;
/// }
/// ```

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id,  String registration, @JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown)  VehicleType vehicleType,  bool active,  String? make,  String? model, @JsonKey(name: 'max_weight_kg')  double? maxWeightKg, @JsonKey(name: 'load_length_cm')  int? loadLengthCm, @JsonKey(name: 'load_width_cm')  int? loadWidthCm, @JsonKey(name: 'load_height_cm')  int? loadHeightCm, @JsonKey(name: 'deactivated_at')  String? deactivatedAt, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)?  $default,) {final _that = this;
switch (_that) {
case _Vehicle() when $default != null:
return $default(_that.id,_that.registration,_that.vehicleType,_that.active,_that.make,_that.model,_that.maxWeightKg,_that.loadLengthCm,_that.loadWidthCm,_that.loadHeightCm,_that.deactivatedAt,_that.createdAt,_that.updatedAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _Vehicle extends Vehicle {
  const _Vehicle({required this.id, required this.registration, @JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown) required this.vehicleType, required this.active, this.make, this.model, @JsonKey(name: 'max_weight_kg') this.maxWeightKg, @JsonKey(name: 'load_length_cm') this.loadLengthCm, @JsonKey(name: 'load_width_cm') this.loadWidthCm, @JsonKey(name: 'load_height_cm') this.loadHeightCm, @JsonKey(name: 'deactivated_at') this.deactivatedAt, @JsonKey(name: 'created_at') this.createdAt, @JsonKey(name: 'updated_at') this.updatedAt}): super._();
  factory _Vehicle.fromJson(Map<String, dynamic> json) => _$VehicleFromJson(json);

@override final  String id;
/// The plate, upper case with its spaces removed — the platform's stored form, not what was
/// typed. `abc 123` and `ABC123` are one vehicle rather than two.
@override final  String registration;
@override@JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown) final  VehicleType vehicleType;
/// Whether the vehicle is in service. See the note on this class.
@override final  bool active;
@override final  String? make;
@override final  String? model;
/// The most it can carry, in **kilograms**. `null` means the provider has not stated one, which
/// is a different thing from a capacity of zero.
@override@JsonKey(name: 'max_weight_kg') final  double? maxWeightKg;
/// The **load space**, in centimetres — not the vehicle's own length. A customer's 190 cm sofa
/// has to fit inside, not alongside.
@override@JsonKey(name: 'load_length_cm') final  int? loadLengthCm;
@override@JsonKey(name: 'load_width_cm') final  int? loadWidthCm;
@override@JsonKey(name: 'load_height_cm') final  int? loadHeightCm;
/// When the provider took it out of service. **Absent while it is in service**, which is what
/// lets a screen show "off the road since 3 Aug 2026" without a second field meaning the same
/// thing as [active].
@override@JsonKey(name: 'deactivated_at') final  String? deactivatedAt;
@override@JsonKey(name: 'created_at') final  String? createdAt;
@override@JsonKey(name: 'updated_at') final  String? updatedAt;

/// Create a copy of Vehicle
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$VehicleCopyWith<_Vehicle> get copyWith => __$VehicleCopyWithImpl<_Vehicle>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$VehicleToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _Vehicle&&(identical(other.id, id) || other.id == id)&&(identical(other.registration, registration) || other.registration == registration)&&(identical(other.vehicleType, vehicleType) || other.vehicleType == vehicleType)&&(identical(other.active, active) || other.active == active)&&(identical(other.make, make) || other.make == make)&&(identical(other.model, model) || other.model == model)&&(identical(other.maxWeightKg, maxWeightKg) || other.maxWeightKg == maxWeightKg)&&(identical(other.loadLengthCm, loadLengthCm) || other.loadLengthCm == loadLengthCm)&&(identical(other.loadWidthCm, loadWidthCm) || other.loadWidthCm == loadWidthCm)&&(identical(other.loadHeightCm, loadHeightCm) || other.loadHeightCm == loadHeightCm)&&(identical(other.deactivatedAt, deactivatedAt) || other.deactivatedAt == deactivatedAt)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt)&&(identical(other.updatedAt, updatedAt) || other.updatedAt == updatedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,registration,vehicleType,active,make,model,maxWeightKg,loadLengthCm,loadWidthCm,loadHeightCm,deactivatedAt,createdAt,updatedAt);

@override
String toString() {
  return 'Vehicle(id: $id, registration: $registration, vehicleType: $vehicleType, active: $active, make: $make, model: $model, maxWeightKg: $maxWeightKg, loadLengthCm: $loadLengthCm, loadWidthCm: $loadWidthCm, loadHeightCm: $loadHeightCm, deactivatedAt: $deactivatedAt, createdAt: $createdAt, updatedAt: $updatedAt)';
}


}

/// @nodoc
abstract mixin class _$VehicleCopyWith<$Res> implements $VehicleCopyWith<$Res> {
  factory _$VehicleCopyWith(_Vehicle value, $Res Function(_Vehicle) _then) = __$VehicleCopyWithImpl;
@override @useResult
$Res call({
 String id, String registration,@JsonKey(name: 'vehicle_type', unknownEnumValue: VehicleType.unknown) VehicleType vehicleType, bool active, String? make, String? model,@JsonKey(name: 'max_weight_kg') double? maxWeightKg,@JsonKey(name: 'load_length_cm') int? loadLengthCm,@JsonKey(name: 'load_width_cm') int? loadWidthCm,@JsonKey(name: 'load_height_cm') int? loadHeightCm,@JsonKey(name: 'deactivated_at') String? deactivatedAt,@JsonKey(name: 'created_at') String? createdAt,@JsonKey(name: 'updated_at') String? updatedAt
});




}
/// @nodoc
class __$VehicleCopyWithImpl<$Res>
    implements _$VehicleCopyWith<$Res> {
  __$VehicleCopyWithImpl(this._self, this._then);

  final _Vehicle _self;
  final $Res Function(_Vehicle) _then;

/// Create a copy of Vehicle
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? registration = null,Object? vehicleType = null,Object? active = null,Object? make = freezed,Object? model = freezed,Object? maxWeightKg = freezed,Object? loadLengthCm = freezed,Object? loadWidthCm = freezed,Object? loadHeightCm = freezed,Object? deactivatedAt = freezed,Object? createdAt = freezed,Object? updatedAt = freezed,}) {
  return _then(_Vehicle(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,registration: null == registration ? _self.registration : registration // ignore: cast_nullable_to_non_nullable
as String,vehicleType: null == vehicleType ? _self.vehicleType : vehicleType // ignore: cast_nullable_to_non_nullable
as VehicleType,active: null == active ? _self.active : active // ignore: cast_nullable_to_non_nullable
as bool,make: freezed == make ? _self.make : make // ignore: cast_nullable_to_non_nullable
as String?,model: freezed == model ? _self.model : model // ignore: cast_nullable_to_non_nullable
as String?,maxWeightKg: freezed == maxWeightKg ? _self.maxWeightKg : maxWeightKg // ignore: cast_nullable_to_non_nullable
as double?,loadLengthCm: freezed == loadLengthCm ? _self.loadLengthCm : loadLengthCm // ignore: cast_nullable_to_non_nullable
as int?,loadWidthCm: freezed == loadWidthCm ? _self.loadWidthCm : loadWidthCm // ignore: cast_nullable_to_non_nullable
as int?,loadHeightCm: freezed == loadHeightCm ? _self.loadHeightCm : loadHeightCm // ignore: cast_nullable_to_non_nullable
as int?,deactivatedAt: freezed == deactivatedAt ? _self.deactivatedAt : deactivatedAt // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,updatedAt: freezed == updatedAt ? _self.updatedAt : updatedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
