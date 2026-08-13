// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'open_job.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$OpenJob {

 String get id;/// `open` or `negotiating`, and nothing else can reach here.
///
/// **Never a settable field** (`Docs/02` §2, `CLAUDE.md`). It is read here and set nowhere.
@JsonKey(unknownEnumValue: JobStatus.unknown) JobStatus get status;/// Where the goods are collected, at the grain a provider is given it.
///
/// Present for every job this endpoint can return — the eligibility filter matches a declared
/// region against the pickup state or postcode, so a job with neither cannot be in the feed —
/// and nullable anyway, because `Docs/07` §6 does not make a client crash on the platform's
/// future.
 JobRegion? get pickup;/// Where they are delivered. **Legitimately absent**: a customer may publish before they have
/// both ends of the trip.
 JobRegion? get dropoff;@JsonKey(name: 'goods_description') String? get goodsDescription;@JsonKey(name: 'length_cm') int? get lengthCm;@JsonKey(name: 'width_cm') int? get widthCm;@JsonKey(name: 'height_cm') int? get heightCm;/// Kilograms (`CLAUDE.md` fixes the units, and the contract carries them).
@JsonKey(name: 'weight_kg') double? get weightKg;/// What the customer says the job needs, in their own words.
///
/// **Free text, and deliberately not an enum.** `Docs/11` §3 records that it stays free text
/// until SHIP-79's capability vocabulary arrives, and `VehicleType` is explicitly not that
/// vocabulary. A client that matched it against a compiled-in list would be inventing an
/// eligibility rule the platform does not have.
@JsonKey(name: 'vehicle_requirement') String? get vehicleRequirement;@JsonKey(name: 'handling_notes') String? get handlingNotes;@JsonKey(name: 'pickup_window') JobTimeWindow? get pickupWindow;@JsonKey(name: 'dropoff_window') JobTimeWindow? get dropoffWindow;/// When the job stops being offered (SHIP-68), so a provider deciding whether to price it
/// today knows whether it will still be there tomorrow.
@JsonKey(name: 'expires_at') String? get expiresAt;@JsonKey(name: 'created_at') String? get createdAt;
/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$OpenJobCopyWith<OpenJob> get copyWith => _$OpenJobCopyWithImpl<OpenJob>(this as OpenJob, _$identity);

  /// Serializes this OpenJob to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is OpenJob&&(identical(other.id, id) || other.id == id)&&(identical(other.status, status) || other.status == status)&&(identical(other.pickup, pickup) || other.pickup == pickup)&&(identical(other.dropoff, dropoff) || other.dropoff == dropoff)&&(identical(other.goodsDescription, goodsDescription) || other.goodsDescription == goodsDescription)&&(identical(other.lengthCm, lengthCm) || other.lengthCm == lengthCm)&&(identical(other.widthCm, widthCm) || other.widthCm == widthCm)&&(identical(other.heightCm, heightCm) || other.heightCm == heightCm)&&(identical(other.weightKg, weightKg) || other.weightKg == weightKg)&&(identical(other.vehicleRequirement, vehicleRequirement) || other.vehicleRequirement == vehicleRequirement)&&(identical(other.handlingNotes, handlingNotes) || other.handlingNotes == handlingNotes)&&(identical(other.pickupWindow, pickupWindow) || other.pickupWindow == pickupWindow)&&(identical(other.dropoffWindow, dropoffWindow) || other.dropoffWindow == dropoffWindow)&&(identical(other.expiresAt, expiresAt) || other.expiresAt == expiresAt)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,status,pickup,dropoff,goodsDescription,lengthCm,widthCm,heightCm,weightKg,vehicleRequirement,handlingNotes,pickupWindow,dropoffWindow,expiresAt,createdAt);

@override
String toString() {
  return 'OpenJob(id: $id, status: $status, pickup: $pickup, dropoff: $dropoff, goodsDescription: $goodsDescription, lengthCm: $lengthCm, widthCm: $widthCm, heightCm: $heightCm, weightKg: $weightKg, vehicleRequirement: $vehicleRequirement, handlingNotes: $handlingNotes, pickupWindow: $pickupWindow, dropoffWindow: $dropoffWindow, expiresAt: $expiresAt, createdAt: $createdAt)';
}


}

/// @nodoc
abstract mixin class $OpenJobCopyWith<$Res>  {
  factory $OpenJobCopyWith(OpenJob value, $Res Function(OpenJob) _then) = _$OpenJobCopyWithImpl;
@useResult
$Res call({
 String id,@JsonKey(unknownEnumValue: JobStatus.unknown) JobStatus status, JobRegion? pickup, JobRegion? dropoff,@JsonKey(name: 'goods_description') String? goodsDescription,@JsonKey(name: 'length_cm') int? lengthCm,@JsonKey(name: 'width_cm') int? widthCm,@JsonKey(name: 'height_cm') int? heightCm,@JsonKey(name: 'weight_kg') double? weightKg,@JsonKey(name: 'vehicle_requirement') String? vehicleRequirement,@JsonKey(name: 'handling_notes') String? handlingNotes,@JsonKey(name: 'pickup_window') JobTimeWindow? pickupWindow,@JsonKey(name: 'dropoff_window') JobTimeWindow? dropoffWindow,@JsonKey(name: 'expires_at') String? expiresAt,@JsonKey(name: 'created_at') String? createdAt
});


$JobRegionCopyWith<$Res>? get pickup;$JobRegionCopyWith<$Res>? get dropoff;$JobTimeWindowCopyWith<$Res>? get pickupWindow;$JobTimeWindowCopyWith<$Res>? get dropoffWindow;

}
/// @nodoc
class _$OpenJobCopyWithImpl<$Res>
    implements $OpenJobCopyWith<$Res> {
  _$OpenJobCopyWithImpl(this._self, this._then);

  final OpenJob _self;
  final $Res Function(OpenJob) _then;

/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? status = null,Object? pickup = freezed,Object? dropoff = freezed,Object? goodsDescription = freezed,Object? lengthCm = freezed,Object? widthCm = freezed,Object? heightCm = freezed,Object? weightKg = freezed,Object? vehicleRequirement = freezed,Object? handlingNotes = freezed,Object? pickupWindow = freezed,Object? dropoffWindow = freezed,Object? expiresAt = freezed,Object? createdAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,status: null == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as JobStatus,pickup: freezed == pickup ? _self.pickup : pickup // ignore: cast_nullable_to_non_nullable
as JobRegion?,dropoff: freezed == dropoff ? _self.dropoff : dropoff // ignore: cast_nullable_to_non_nullable
as JobRegion?,goodsDescription: freezed == goodsDescription ? _self.goodsDescription : goodsDescription // ignore: cast_nullable_to_non_nullable
as String?,lengthCm: freezed == lengthCm ? _self.lengthCm : lengthCm // ignore: cast_nullable_to_non_nullable
as int?,widthCm: freezed == widthCm ? _self.widthCm : widthCm // ignore: cast_nullable_to_non_nullable
as int?,heightCm: freezed == heightCm ? _self.heightCm : heightCm // ignore: cast_nullable_to_non_nullable
as int?,weightKg: freezed == weightKg ? _self.weightKg : weightKg // ignore: cast_nullable_to_non_nullable
as double?,vehicleRequirement: freezed == vehicleRequirement ? _self.vehicleRequirement : vehicleRequirement // ignore: cast_nullable_to_non_nullable
as String?,handlingNotes: freezed == handlingNotes ? _self.handlingNotes : handlingNotes // ignore: cast_nullable_to_non_nullable
as String?,pickupWindow: freezed == pickupWindow ? _self.pickupWindow : pickupWindow // ignore: cast_nullable_to_non_nullable
as JobTimeWindow?,dropoffWindow: freezed == dropoffWindow ? _self.dropoffWindow : dropoffWindow // ignore: cast_nullable_to_non_nullable
as JobTimeWindow?,expiresAt: freezed == expiresAt ? _self.expiresAt : expiresAt // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}
/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobRegionCopyWith<$Res>? get pickup {
    if (_self.pickup == null) {
    return null;
  }

  return $JobRegionCopyWith<$Res>(_self.pickup!, (value) {
    return _then(_self.copyWith(pickup: value));
  });
}/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobRegionCopyWith<$Res>? get dropoff {
    if (_self.dropoff == null) {
    return null;
  }

  return $JobRegionCopyWith<$Res>(_self.dropoff!, (value) {
    return _then(_self.copyWith(dropoff: value));
  });
}/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobTimeWindowCopyWith<$Res>? get pickupWindow {
    if (_self.pickupWindow == null) {
    return null;
  }

  return $JobTimeWindowCopyWith<$Res>(_self.pickupWindow!, (value) {
    return _then(_self.copyWith(pickupWindow: value));
  });
}/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobTimeWindowCopyWith<$Res>? get dropoffWindow {
    if (_self.dropoffWindow == null) {
    return null;
  }

  return $JobTimeWindowCopyWith<$Res>(_self.dropoffWindow!, (value) {
    return _then(_self.copyWith(dropoffWindow: value));
  });
}
}


/// Adds pattern-matching-related methods to [OpenJob].
extension OpenJobPatterns on OpenJob {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _OpenJob value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _OpenJob() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _OpenJob value)  $default,){
final _that = this;
switch (_that) {
case _OpenJob():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _OpenJob value)?  $default,){
final _that = this;
switch (_that) {
case _OpenJob() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id, @JsonKey(unknownEnumValue: JobStatus.unknown)  JobStatus status,  JobRegion? pickup,  JobRegion? dropoff, @JsonKey(name: 'goods_description')  String? goodsDescription, @JsonKey(name: 'length_cm')  int? lengthCm, @JsonKey(name: 'width_cm')  int? widthCm, @JsonKey(name: 'height_cm')  int? heightCm, @JsonKey(name: 'weight_kg')  double? weightKg, @JsonKey(name: 'vehicle_requirement')  String? vehicleRequirement, @JsonKey(name: 'handling_notes')  String? handlingNotes, @JsonKey(name: 'pickup_window')  JobTimeWindow? pickupWindow, @JsonKey(name: 'dropoff_window')  JobTimeWindow? dropoffWindow, @JsonKey(name: 'expires_at')  String? expiresAt, @JsonKey(name: 'created_at')  String? createdAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _OpenJob() when $default != null:
return $default(_that.id,_that.status,_that.pickup,_that.dropoff,_that.goodsDescription,_that.lengthCm,_that.widthCm,_that.heightCm,_that.weightKg,_that.vehicleRequirement,_that.handlingNotes,_that.pickupWindow,_that.dropoffWindow,_that.expiresAt,_that.createdAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id, @JsonKey(unknownEnumValue: JobStatus.unknown)  JobStatus status,  JobRegion? pickup,  JobRegion? dropoff, @JsonKey(name: 'goods_description')  String? goodsDescription, @JsonKey(name: 'length_cm')  int? lengthCm, @JsonKey(name: 'width_cm')  int? widthCm, @JsonKey(name: 'height_cm')  int? heightCm, @JsonKey(name: 'weight_kg')  double? weightKg, @JsonKey(name: 'vehicle_requirement')  String? vehicleRequirement, @JsonKey(name: 'handling_notes')  String? handlingNotes, @JsonKey(name: 'pickup_window')  JobTimeWindow? pickupWindow, @JsonKey(name: 'dropoff_window')  JobTimeWindow? dropoffWindow, @JsonKey(name: 'expires_at')  String? expiresAt, @JsonKey(name: 'created_at')  String? createdAt)  $default,) {final _that = this;
switch (_that) {
case _OpenJob():
return $default(_that.id,_that.status,_that.pickup,_that.dropoff,_that.goodsDescription,_that.lengthCm,_that.widthCm,_that.heightCm,_that.weightKg,_that.vehicleRequirement,_that.handlingNotes,_that.pickupWindow,_that.dropoffWindow,_that.expiresAt,_that.createdAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id, @JsonKey(unknownEnumValue: JobStatus.unknown)  JobStatus status,  JobRegion? pickup,  JobRegion? dropoff, @JsonKey(name: 'goods_description')  String? goodsDescription, @JsonKey(name: 'length_cm')  int? lengthCm, @JsonKey(name: 'width_cm')  int? widthCm, @JsonKey(name: 'height_cm')  int? heightCm, @JsonKey(name: 'weight_kg')  double? weightKg, @JsonKey(name: 'vehicle_requirement')  String? vehicleRequirement, @JsonKey(name: 'handling_notes')  String? handlingNotes, @JsonKey(name: 'pickup_window')  JobTimeWindow? pickupWindow, @JsonKey(name: 'dropoff_window')  JobTimeWindow? dropoffWindow, @JsonKey(name: 'expires_at')  String? expiresAt, @JsonKey(name: 'created_at')  String? createdAt)?  $default,) {final _that = this;
switch (_that) {
case _OpenJob() when $default != null:
return $default(_that.id,_that.status,_that.pickup,_that.dropoff,_that.goodsDescription,_that.lengthCm,_that.widthCm,_that.heightCm,_that.weightKg,_that.vehicleRequirement,_that.handlingNotes,_that.pickupWindow,_that.dropoffWindow,_that.expiresAt,_that.createdAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _OpenJob extends OpenJob {
  const _OpenJob({required this.id, @JsonKey(unknownEnumValue: JobStatus.unknown) required this.status, this.pickup, this.dropoff, @JsonKey(name: 'goods_description') this.goodsDescription, @JsonKey(name: 'length_cm') this.lengthCm, @JsonKey(name: 'width_cm') this.widthCm, @JsonKey(name: 'height_cm') this.heightCm, @JsonKey(name: 'weight_kg') this.weightKg, @JsonKey(name: 'vehicle_requirement') this.vehicleRequirement, @JsonKey(name: 'handling_notes') this.handlingNotes, @JsonKey(name: 'pickup_window') this.pickupWindow, @JsonKey(name: 'dropoff_window') this.dropoffWindow, @JsonKey(name: 'expires_at') this.expiresAt, @JsonKey(name: 'created_at') this.createdAt}): super._();
  factory _OpenJob.fromJson(Map<String, dynamic> json) => _$OpenJobFromJson(json);

@override final  String id;
/// `open` or `negotiating`, and nothing else can reach here.
///
/// **Never a settable field** (`Docs/02` §2, `CLAUDE.md`). It is read here and set nowhere.
@override@JsonKey(unknownEnumValue: JobStatus.unknown) final  JobStatus status;
/// Where the goods are collected, at the grain a provider is given it.
///
/// Present for every job this endpoint can return — the eligibility filter matches a declared
/// region against the pickup state or postcode, so a job with neither cannot be in the feed —
/// and nullable anyway, because `Docs/07` §6 does not make a client crash on the platform's
/// future.
@override final  JobRegion? pickup;
/// Where they are delivered. **Legitimately absent**: a customer may publish before they have
/// both ends of the trip.
@override final  JobRegion? dropoff;
@override@JsonKey(name: 'goods_description') final  String? goodsDescription;
@override@JsonKey(name: 'length_cm') final  int? lengthCm;
@override@JsonKey(name: 'width_cm') final  int? widthCm;
@override@JsonKey(name: 'height_cm') final  int? heightCm;
/// Kilograms (`CLAUDE.md` fixes the units, and the contract carries them).
@override@JsonKey(name: 'weight_kg') final  double? weightKg;
/// What the customer says the job needs, in their own words.
///
/// **Free text, and deliberately not an enum.** `Docs/11` §3 records that it stays free text
/// until SHIP-79's capability vocabulary arrives, and `VehicleType` is explicitly not that
/// vocabulary. A client that matched it against a compiled-in list would be inventing an
/// eligibility rule the platform does not have.
@override@JsonKey(name: 'vehicle_requirement') final  String? vehicleRequirement;
@override@JsonKey(name: 'handling_notes') final  String? handlingNotes;
@override@JsonKey(name: 'pickup_window') final  JobTimeWindow? pickupWindow;
@override@JsonKey(name: 'dropoff_window') final  JobTimeWindow? dropoffWindow;
/// When the job stops being offered (SHIP-68), so a provider deciding whether to price it
/// today knows whether it will still be there tomorrow.
@override@JsonKey(name: 'expires_at') final  String? expiresAt;
@override@JsonKey(name: 'created_at') final  String? createdAt;

/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$OpenJobCopyWith<_OpenJob> get copyWith => __$OpenJobCopyWithImpl<_OpenJob>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$OpenJobToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _OpenJob&&(identical(other.id, id) || other.id == id)&&(identical(other.status, status) || other.status == status)&&(identical(other.pickup, pickup) || other.pickup == pickup)&&(identical(other.dropoff, dropoff) || other.dropoff == dropoff)&&(identical(other.goodsDescription, goodsDescription) || other.goodsDescription == goodsDescription)&&(identical(other.lengthCm, lengthCm) || other.lengthCm == lengthCm)&&(identical(other.widthCm, widthCm) || other.widthCm == widthCm)&&(identical(other.heightCm, heightCm) || other.heightCm == heightCm)&&(identical(other.weightKg, weightKg) || other.weightKg == weightKg)&&(identical(other.vehicleRequirement, vehicleRequirement) || other.vehicleRequirement == vehicleRequirement)&&(identical(other.handlingNotes, handlingNotes) || other.handlingNotes == handlingNotes)&&(identical(other.pickupWindow, pickupWindow) || other.pickupWindow == pickupWindow)&&(identical(other.dropoffWindow, dropoffWindow) || other.dropoffWindow == dropoffWindow)&&(identical(other.expiresAt, expiresAt) || other.expiresAt == expiresAt)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,status,pickup,dropoff,goodsDescription,lengthCm,widthCm,heightCm,weightKg,vehicleRequirement,handlingNotes,pickupWindow,dropoffWindow,expiresAt,createdAt);

@override
String toString() {
  return 'OpenJob(id: $id, status: $status, pickup: $pickup, dropoff: $dropoff, goodsDescription: $goodsDescription, lengthCm: $lengthCm, widthCm: $widthCm, heightCm: $heightCm, weightKg: $weightKg, vehicleRequirement: $vehicleRequirement, handlingNotes: $handlingNotes, pickupWindow: $pickupWindow, dropoffWindow: $dropoffWindow, expiresAt: $expiresAt, createdAt: $createdAt)';
}


}

/// @nodoc
abstract mixin class _$OpenJobCopyWith<$Res> implements $OpenJobCopyWith<$Res> {
  factory _$OpenJobCopyWith(_OpenJob value, $Res Function(_OpenJob) _then) = __$OpenJobCopyWithImpl;
@override @useResult
$Res call({
 String id,@JsonKey(unknownEnumValue: JobStatus.unknown) JobStatus status, JobRegion? pickup, JobRegion? dropoff,@JsonKey(name: 'goods_description') String? goodsDescription,@JsonKey(name: 'length_cm') int? lengthCm,@JsonKey(name: 'width_cm') int? widthCm,@JsonKey(name: 'height_cm') int? heightCm,@JsonKey(name: 'weight_kg') double? weightKg,@JsonKey(name: 'vehicle_requirement') String? vehicleRequirement,@JsonKey(name: 'handling_notes') String? handlingNotes,@JsonKey(name: 'pickup_window') JobTimeWindow? pickupWindow,@JsonKey(name: 'dropoff_window') JobTimeWindow? dropoffWindow,@JsonKey(name: 'expires_at') String? expiresAt,@JsonKey(name: 'created_at') String? createdAt
});


@override $JobRegionCopyWith<$Res>? get pickup;@override $JobRegionCopyWith<$Res>? get dropoff;@override $JobTimeWindowCopyWith<$Res>? get pickupWindow;@override $JobTimeWindowCopyWith<$Res>? get dropoffWindow;

}
/// @nodoc
class __$OpenJobCopyWithImpl<$Res>
    implements _$OpenJobCopyWith<$Res> {
  __$OpenJobCopyWithImpl(this._self, this._then);

  final _OpenJob _self;
  final $Res Function(_OpenJob) _then;

/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? status = null,Object? pickup = freezed,Object? dropoff = freezed,Object? goodsDescription = freezed,Object? lengthCm = freezed,Object? widthCm = freezed,Object? heightCm = freezed,Object? weightKg = freezed,Object? vehicleRequirement = freezed,Object? handlingNotes = freezed,Object? pickupWindow = freezed,Object? dropoffWindow = freezed,Object? expiresAt = freezed,Object? createdAt = freezed,}) {
  return _then(_OpenJob(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,status: null == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as JobStatus,pickup: freezed == pickup ? _self.pickup : pickup // ignore: cast_nullable_to_non_nullable
as JobRegion?,dropoff: freezed == dropoff ? _self.dropoff : dropoff // ignore: cast_nullable_to_non_nullable
as JobRegion?,goodsDescription: freezed == goodsDescription ? _self.goodsDescription : goodsDescription // ignore: cast_nullable_to_non_nullable
as String?,lengthCm: freezed == lengthCm ? _self.lengthCm : lengthCm // ignore: cast_nullable_to_non_nullable
as int?,widthCm: freezed == widthCm ? _self.widthCm : widthCm // ignore: cast_nullable_to_non_nullable
as int?,heightCm: freezed == heightCm ? _self.heightCm : heightCm // ignore: cast_nullable_to_non_nullable
as int?,weightKg: freezed == weightKg ? _self.weightKg : weightKg // ignore: cast_nullable_to_non_nullable
as double?,vehicleRequirement: freezed == vehicleRequirement ? _self.vehicleRequirement : vehicleRequirement // ignore: cast_nullable_to_non_nullable
as String?,handlingNotes: freezed == handlingNotes ? _self.handlingNotes : handlingNotes // ignore: cast_nullable_to_non_nullable
as String?,pickupWindow: freezed == pickupWindow ? _self.pickupWindow : pickupWindow // ignore: cast_nullable_to_non_nullable
as JobTimeWindow?,dropoffWindow: freezed == dropoffWindow ? _self.dropoffWindow : dropoffWindow // ignore: cast_nullable_to_non_nullable
as JobTimeWindow?,expiresAt: freezed == expiresAt ? _self.expiresAt : expiresAt // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobRegionCopyWith<$Res>? get pickup {
    if (_self.pickup == null) {
    return null;
  }

  return $JobRegionCopyWith<$Res>(_self.pickup!, (value) {
    return _then(_self.copyWith(pickup: value));
  });
}/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobRegionCopyWith<$Res>? get dropoff {
    if (_self.dropoff == null) {
    return null;
  }

  return $JobRegionCopyWith<$Res>(_self.dropoff!, (value) {
    return _then(_self.copyWith(dropoff: value));
  });
}/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobTimeWindowCopyWith<$Res>? get pickupWindow {
    if (_self.pickupWindow == null) {
    return null;
  }

  return $JobTimeWindowCopyWith<$Res>(_self.pickupWindow!, (value) {
    return _then(_self.copyWith(pickupWindow: value));
  });
}/// Create a copy of OpenJob
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobTimeWindowCopyWith<$Res>? get dropoffWindow {
    if (_self.dropoffWindow == null) {
    return null;
  }

  return $JobTimeWindowCopyWith<$Res>(_self.dropoffWindow!, (value) {
    return _then(_self.copyWith(dropoffWindow: value));
  });
}
}


/// @nodoc
mixin _$JobRegion {

 String get suburb;/// The state or territory, as the **upper-case abbreviation** — `NSW`, `VIC`.
///
/// A `String` and deliberately not an enum, for the reason [JobLocation.state] gives: a client
/// prints it rather than branching on it, and an enum would be a second list of the eight
/// states to keep in step for no behaviour that depends on it. The feed's own narrowing groups
/// by the values it was actually sent rather than by a list compiled in here, which is the
/// same decision seen from the other side.
 String get state;/// Four digits, leading zero kept — `0800` is Darwin, and an `int` would lose it.
 String get postcode;
/// Create a copy of JobRegion
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$JobRegionCopyWith<JobRegion> get copyWith => _$JobRegionCopyWithImpl<JobRegion>(this as JobRegion, _$identity);

  /// Serializes this JobRegion to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is JobRegion&&(identical(other.suburb, suburb) || other.suburb == suburb)&&(identical(other.state, state) || other.state == state)&&(identical(other.postcode, postcode) || other.postcode == postcode));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,suburb,state,postcode);

@override
String toString() {
  return 'JobRegion(suburb: $suburb, state: $state, postcode: $postcode)';
}


}

/// @nodoc
abstract mixin class $JobRegionCopyWith<$Res>  {
  factory $JobRegionCopyWith(JobRegion value, $Res Function(JobRegion) _then) = _$JobRegionCopyWithImpl;
@useResult
$Res call({
 String suburb, String state, String postcode
});




}
/// @nodoc
class _$JobRegionCopyWithImpl<$Res>
    implements $JobRegionCopyWith<$Res> {
  _$JobRegionCopyWithImpl(this._self, this._then);

  final JobRegion _self;
  final $Res Function(JobRegion) _then;

/// Create a copy of JobRegion
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? suburb = null,Object? state = null,Object? postcode = null,}) {
  return _then(_self.copyWith(
suburb: null == suburb ? _self.suburb : suburb // ignore: cast_nullable_to_non_nullable
as String,state: null == state ? _self.state : state // ignore: cast_nullable_to_non_nullable
as String,postcode: null == postcode ? _self.postcode : postcode // ignore: cast_nullable_to_non_nullable
as String,
  ));
}

}


/// Adds pattern-matching-related methods to [JobRegion].
extension JobRegionPatterns on JobRegion {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _JobRegion value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _JobRegion() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _JobRegion value)  $default,){
final _that = this;
switch (_that) {
case _JobRegion():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _JobRegion value)?  $default,){
final _that = this;
switch (_that) {
case _JobRegion() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String suburb,  String state,  String postcode)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _JobRegion() when $default != null:
return $default(_that.suburb,_that.state,_that.postcode);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String suburb,  String state,  String postcode)  $default,) {final _that = this;
switch (_that) {
case _JobRegion():
return $default(_that.suburb,_that.state,_that.postcode);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String suburb,  String state,  String postcode)?  $default,) {final _that = this;
switch (_that) {
case _JobRegion() when $default != null:
return $default(_that.suburb,_that.state,_that.postcode);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _JobRegion extends JobRegion {
  const _JobRegion({this.suburb = '', this.state = '', this.postcode = ''}): super._();
  factory _JobRegion.fromJson(Map<String, dynamic> json) => _$JobRegionFromJson(json);

@override@JsonKey() final  String suburb;
/// The state or territory, as the **upper-case abbreviation** — `NSW`, `VIC`.
///
/// A `String` and deliberately not an enum, for the reason [JobLocation.state] gives: a client
/// prints it rather than branching on it, and an enum would be a second list of the eight
/// states to keep in step for no behaviour that depends on it. The feed's own narrowing groups
/// by the values it was actually sent rather than by a list compiled in here, which is the
/// same decision seen from the other side.
@override@JsonKey() final  String state;
/// Four digits, leading zero kept — `0800` is Darwin, and an `int` would lose it.
@override@JsonKey() final  String postcode;

/// Create a copy of JobRegion
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$JobRegionCopyWith<_JobRegion> get copyWith => __$JobRegionCopyWithImpl<_JobRegion>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$JobRegionToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _JobRegion&&(identical(other.suburb, suburb) || other.suburb == suburb)&&(identical(other.state, state) || other.state == state)&&(identical(other.postcode, postcode) || other.postcode == postcode));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,suburb,state,postcode);

@override
String toString() {
  return 'JobRegion(suburb: $suburb, state: $state, postcode: $postcode)';
}


}

/// @nodoc
abstract mixin class _$JobRegionCopyWith<$Res> implements $JobRegionCopyWith<$Res> {
  factory _$JobRegionCopyWith(_JobRegion value, $Res Function(_JobRegion) _then) = __$JobRegionCopyWithImpl;
@override @useResult
$Res call({
 String suburb, String state, String postcode
});




}
/// @nodoc
class __$JobRegionCopyWithImpl<$Res>
    implements _$JobRegionCopyWith<$Res> {
  __$JobRegionCopyWithImpl(this._self, this._then);

  final _JobRegion _self;
  final $Res Function(_JobRegion) _then;

/// Create a copy of JobRegion
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? suburb = null,Object? state = null,Object? postcode = null,}) {
  return _then(_JobRegion(
suburb: null == suburb ? _self.suburb : suburb // ignore: cast_nullable_to_non_nullable
as String,state: null == state ? _self.state : state // ignore: cast_nullable_to_non_nullable
as String,postcode: null == postcode ? _self.postcode : postcode // ignore: cast_nullable_to_non_nullable
as String,
  ));
}


}

// dart format on
