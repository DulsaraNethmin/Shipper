// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'job.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$Job {

 String get id;/// **Never a settable field** (`Docs/02` §2, `CLAUDE.md`). It is read here and set nowhere:
/// no request body in this API has a `status`, and one that arrived would be refused as an
/// unknown field rather than quietly ignored.
@JsonKey(unknownEnumValue: JobStatus.unknown) JobStatus get status;/// Where the goods are collected, once the customer has given an address (SHIP-71).
 JobLocation? get pickup;/// Where they are delivered (SHIP-71).
 JobLocation? get dropoff;@JsonKey(name: 'goods_description') String? get goodsDescription;@JsonKey(name: 'length_cm') int? get lengthCm;@JsonKey(name: 'width_cm') int? get widthCm;@JsonKey(name: 'height_cm') int? get heightCm;@JsonKey(name: 'weight_kg') double? get weightKg;@JsonKey(name: 'vehicle_requirement') String? get vehicleRequirement;@JsonKey(name: 'handling_notes') String? get handlingNotes;@JsonKey(name: 'pickup_window') JobTimeWindow? get pickupWindow;@JsonKey(name: 'dropoff_window') JobTimeWindow? get dropoffWindow;/// The customer's own maximum, in **cents**, AUD (SHIP-67).
///
/// Minor units as a whole number because money is never a float (`Docs/10` §3.3), and the
/// name carries the unit because a field called `budget` holding `150000` is one somebody
/// eventually reads as dollars.
///
/// `null` means no budget was supplied, which is a different thing from a budget of zero —
/// that distinction is the reason the platform omits the field rather than sending `0`.
///
/// **Read it only on a customer surface.** See the note on this class.
@JsonKey(name: 'budget_cents') int? get budgetCents;/// When the job stops being offered (SHIP-68).
///
/// Absent while the job is a draft: the clock starts at publication, and a draft may be saved
/// and returned to indefinitely (`Docs/02` §6.3).
@JsonKey(name: 'expires_at') String? get expiresAt;@JsonKey(name: 'created_at') String? get createdAt;@JsonKey(name: 'updated_at') String? get updatedAt;
/// Create a copy of Job
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$JobCopyWith<Job> get copyWith => _$JobCopyWithImpl<Job>(this as Job, _$identity);

  /// Serializes this Job to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is Job&&(identical(other.id, id) || other.id == id)&&(identical(other.status, status) || other.status == status)&&(identical(other.pickup, pickup) || other.pickup == pickup)&&(identical(other.dropoff, dropoff) || other.dropoff == dropoff)&&(identical(other.goodsDescription, goodsDescription) || other.goodsDescription == goodsDescription)&&(identical(other.lengthCm, lengthCm) || other.lengthCm == lengthCm)&&(identical(other.widthCm, widthCm) || other.widthCm == widthCm)&&(identical(other.heightCm, heightCm) || other.heightCm == heightCm)&&(identical(other.weightKg, weightKg) || other.weightKg == weightKg)&&(identical(other.vehicleRequirement, vehicleRequirement) || other.vehicleRequirement == vehicleRequirement)&&(identical(other.handlingNotes, handlingNotes) || other.handlingNotes == handlingNotes)&&(identical(other.pickupWindow, pickupWindow) || other.pickupWindow == pickupWindow)&&(identical(other.dropoffWindow, dropoffWindow) || other.dropoffWindow == dropoffWindow)&&(identical(other.budgetCents, budgetCents) || other.budgetCents == budgetCents)&&(identical(other.expiresAt, expiresAt) || other.expiresAt == expiresAt)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt)&&(identical(other.updatedAt, updatedAt) || other.updatedAt == updatedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,status,pickup,dropoff,goodsDescription,lengthCm,widthCm,heightCm,weightKg,vehicleRequirement,handlingNotes,pickupWindow,dropoffWindow,budgetCents,expiresAt,createdAt,updatedAt);

@override
String toString() {
  return 'Job(id: $id, status: $status, pickup: $pickup, dropoff: $dropoff, goodsDescription: $goodsDescription, lengthCm: $lengthCm, widthCm: $widthCm, heightCm: $heightCm, weightKg: $weightKg, vehicleRequirement: $vehicleRequirement, handlingNotes: $handlingNotes, pickupWindow: $pickupWindow, dropoffWindow: $dropoffWindow, budgetCents: $budgetCents, expiresAt: $expiresAt, createdAt: $createdAt, updatedAt: $updatedAt)';
}


}

/// @nodoc
abstract mixin class $JobCopyWith<$Res>  {
  factory $JobCopyWith(Job value, $Res Function(Job) _then) = _$JobCopyWithImpl;
@useResult
$Res call({
 String id,@JsonKey(unknownEnumValue: JobStatus.unknown) JobStatus status, JobLocation? pickup, JobLocation? dropoff,@JsonKey(name: 'goods_description') String? goodsDescription,@JsonKey(name: 'length_cm') int? lengthCm,@JsonKey(name: 'width_cm') int? widthCm,@JsonKey(name: 'height_cm') int? heightCm,@JsonKey(name: 'weight_kg') double? weightKg,@JsonKey(name: 'vehicle_requirement') String? vehicleRequirement,@JsonKey(name: 'handling_notes') String? handlingNotes,@JsonKey(name: 'pickup_window') JobTimeWindow? pickupWindow,@JsonKey(name: 'dropoff_window') JobTimeWindow? dropoffWindow,@JsonKey(name: 'budget_cents') int? budgetCents,@JsonKey(name: 'expires_at') String? expiresAt,@JsonKey(name: 'created_at') String? createdAt,@JsonKey(name: 'updated_at') String? updatedAt
});


$JobLocationCopyWith<$Res>? get pickup;$JobLocationCopyWith<$Res>? get dropoff;$JobTimeWindowCopyWith<$Res>? get pickupWindow;$JobTimeWindowCopyWith<$Res>? get dropoffWindow;

}
/// @nodoc
class _$JobCopyWithImpl<$Res>
    implements $JobCopyWith<$Res> {
  _$JobCopyWithImpl(this._self, this._then);

  final Job _self;
  final $Res Function(Job) _then;

/// Create a copy of Job
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? status = null,Object? pickup = freezed,Object? dropoff = freezed,Object? goodsDescription = freezed,Object? lengthCm = freezed,Object? widthCm = freezed,Object? heightCm = freezed,Object? weightKg = freezed,Object? vehicleRequirement = freezed,Object? handlingNotes = freezed,Object? pickupWindow = freezed,Object? dropoffWindow = freezed,Object? budgetCents = freezed,Object? expiresAt = freezed,Object? createdAt = freezed,Object? updatedAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,status: null == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as JobStatus,pickup: freezed == pickup ? _self.pickup : pickup // ignore: cast_nullable_to_non_nullable
as JobLocation?,dropoff: freezed == dropoff ? _self.dropoff : dropoff // ignore: cast_nullable_to_non_nullable
as JobLocation?,goodsDescription: freezed == goodsDescription ? _self.goodsDescription : goodsDescription // ignore: cast_nullable_to_non_nullable
as String?,lengthCm: freezed == lengthCm ? _self.lengthCm : lengthCm // ignore: cast_nullable_to_non_nullable
as int?,widthCm: freezed == widthCm ? _self.widthCm : widthCm // ignore: cast_nullable_to_non_nullable
as int?,heightCm: freezed == heightCm ? _self.heightCm : heightCm // ignore: cast_nullable_to_non_nullable
as int?,weightKg: freezed == weightKg ? _self.weightKg : weightKg // ignore: cast_nullable_to_non_nullable
as double?,vehicleRequirement: freezed == vehicleRequirement ? _self.vehicleRequirement : vehicleRequirement // ignore: cast_nullable_to_non_nullable
as String?,handlingNotes: freezed == handlingNotes ? _self.handlingNotes : handlingNotes // ignore: cast_nullable_to_non_nullable
as String?,pickupWindow: freezed == pickupWindow ? _self.pickupWindow : pickupWindow // ignore: cast_nullable_to_non_nullable
as JobTimeWindow?,dropoffWindow: freezed == dropoffWindow ? _self.dropoffWindow : dropoffWindow // ignore: cast_nullable_to_non_nullable
as JobTimeWindow?,budgetCents: freezed == budgetCents ? _self.budgetCents : budgetCents // ignore: cast_nullable_to_non_nullable
as int?,expiresAt: freezed == expiresAt ? _self.expiresAt : expiresAt // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,updatedAt: freezed == updatedAt ? _self.updatedAt : updatedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}
/// Create a copy of Job
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobLocationCopyWith<$Res>? get pickup {
    if (_self.pickup == null) {
    return null;
  }

  return $JobLocationCopyWith<$Res>(_self.pickup!, (value) {
    return _then(_self.copyWith(pickup: value));
  });
}/// Create a copy of Job
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobLocationCopyWith<$Res>? get dropoff {
    if (_self.dropoff == null) {
    return null;
  }

  return $JobLocationCopyWith<$Res>(_self.dropoff!, (value) {
    return _then(_self.copyWith(dropoff: value));
  });
}/// Create a copy of Job
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
}/// Create a copy of Job
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


/// Adds pattern-matching-related methods to [Job].
extension JobPatterns on Job {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _Job value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _Job() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _Job value)  $default,){
final _that = this;
switch (_that) {
case _Job():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _Job value)?  $default,){
final _that = this;
switch (_that) {
case _Job() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id, @JsonKey(unknownEnumValue: JobStatus.unknown)  JobStatus status,  JobLocation? pickup,  JobLocation? dropoff, @JsonKey(name: 'goods_description')  String? goodsDescription, @JsonKey(name: 'length_cm')  int? lengthCm, @JsonKey(name: 'width_cm')  int? widthCm, @JsonKey(name: 'height_cm')  int? heightCm, @JsonKey(name: 'weight_kg')  double? weightKg, @JsonKey(name: 'vehicle_requirement')  String? vehicleRequirement, @JsonKey(name: 'handling_notes')  String? handlingNotes, @JsonKey(name: 'pickup_window')  JobTimeWindow? pickupWindow, @JsonKey(name: 'dropoff_window')  JobTimeWindow? dropoffWindow, @JsonKey(name: 'budget_cents')  int? budgetCents, @JsonKey(name: 'expires_at')  String? expiresAt, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _Job() when $default != null:
return $default(_that.id,_that.status,_that.pickup,_that.dropoff,_that.goodsDescription,_that.lengthCm,_that.widthCm,_that.heightCm,_that.weightKg,_that.vehicleRequirement,_that.handlingNotes,_that.pickupWindow,_that.dropoffWindow,_that.budgetCents,_that.expiresAt,_that.createdAt,_that.updatedAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id, @JsonKey(unknownEnumValue: JobStatus.unknown)  JobStatus status,  JobLocation? pickup,  JobLocation? dropoff, @JsonKey(name: 'goods_description')  String? goodsDescription, @JsonKey(name: 'length_cm')  int? lengthCm, @JsonKey(name: 'width_cm')  int? widthCm, @JsonKey(name: 'height_cm')  int? heightCm, @JsonKey(name: 'weight_kg')  double? weightKg, @JsonKey(name: 'vehicle_requirement')  String? vehicleRequirement, @JsonKey(name: 'handling_notes')  String? handlingNotes, @JsonKey(name: 'pickup_window')  JobTimeWindow? pickupWindow, @JsonKey(name: 'dropoff_window')  JobTimeWindow? dropoffWindow, @JsonKey(name: 'budget_cents')  int? budgetCents, @JsonKey(name: 'expires_at')  String? expiresAt, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)  $default,) {final _that = this;
switch (_that) {
case _Job():
return $default(_that.id,_that.status,_that.pickup,_that.dropoff,_that.goodsDescription,_that.lengthCm,_that.widthCm,_that.heightCm,_that.weightKg,_that.vehicleRequirement,_that.handlingNotes,_that.pickupWindow,_that.dropoffWindow,_that.budgetCents,_that.expiresAt,_that.createdAt,_that.updatedAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id, @JsonKey(unknownEnumValue: JobStatus.unknown)  JobStatus status,  JobLocation? pickup,  JobLocation? dropoff, @JsonKey(name: 'goods_description')  String? goodsDescription, @JsonKey(name: 'length_cm')  int? lengthCm, @JsonKey(name: 'width_cm')  int? widthCm, @JsonKey(name: 'height_cm')  int? heightCm, @JsonKey(name: 'weight_kg')  double? weightKg, @JsonKey(name: 'vehicle_requirement')  String? vehicleRequirement, @JsonKey(name: 'handling_notes')  String? handlingNotes, @JsonKey(name: 'pickup_window')  JobTimeWindow? pickupWindow, @JsonKey(name: 'dropoff_window')  JobTimeWindow? dropoffWindow, @JsonKey(name: 'budget_cents')  int? budgetCents, @JsonKey(name: 'expires_at')  String? expiresAt, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)?  $default,) {final _that = this;
switch (_that) {
case _Job() when $default != null:
return $default(_that.id,_that.status,_that.pickup,_that.dropoff,_that.goodsDescription,_that.lengthCm,_that.widthCm,_that.heightCm,_that.weightKg,_that.vehicleRequirement,_that.handlingNotes,_that.pickupWindow,_that.dropoffWindow,_that.budgetCents,_that.expiresAt,_that.createdAt,_that.updatedAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _Job implements Job {
  const _Job({required this.id, @JsonKey(unknownEnumValue: JobStatus.unknown) required this.status, this.pickup, this.dropoff, @JsonKey(name: 'goods_description') this.goodsDescription, @JsonKey(name: 'length_cm') this.lengthCm, @JsonKey(name: 'width_cm') this.widthCm, @JsonKey(name: 'height_cm') this.heightCm, @JsonKey(name: 'weight_kg') this.weightKg, @JsonKey(name: 'vehicle_requirement') this.vehicleRequirement, @JsonKey(name: 'handling_notes') this.handlingNotes, @JsonKey(name: 'pickup_window') this.pickupWindow, @JsonKey(name: 'dropoff_window') this.dropoffWindow, @JsonKey(name: 'budget_cents') this.budgetCents, @JsonKey(name: 'expires_at') this.expiresAt, @JsonKey(name: 'created_at') this.createdAt, @JsonKey(name: 'updated_at') this.updatedAt});
  factory _Job.fromJson(Map<String, dynamic> json) => _$JobFromJson(json);

@override final  String id;
/// **Never a settable field** (`Docs/02` §2, `CLAUDE.md`). It is read here and set nowhere:
/// no request body in this API has a `status`, and one that arrived would be refused as an
/// unknown field rather than quietly ignored.
@override@JsonKey(unknownEnumValue: JobStatus.unknown) final  JobStatus status;
/// Where the goods are collected, once the customer has given an address (SHIP-71).
@override final  JobLocation? pickup;
/// Where they are delivered (SHIP-71).
@override final  JobLocation? dropoff;
@override@JsonKey(name: 'goods_description') final  String? goodsDescription;
@override@JsonKey(name: 'length_cm') final  int? lengthCm;
@override@JsonKey(name: 'width_cm') final  int? widthCm;
@override@JsonKey(name: 'height_cm') final  int? heightCm;
@override@JsonKey(name: 'weight_kg') final  double? weightKg;
@override@JsonKey(name: 'vehicle_requirement') final  String? vehicleRequirement;
@override@JsonKey(name: 'handling_notes') final  String? handlingNotes;
@override@JsonKey(name: 'pickup_window') final  JobTimeWindow? pickupWindow;
@override@JsonKey(name: 'dropoff_window') final  JobTimeWindow? dropoffWindow;
/// The customer's own maximum, in **cents**, AUD (SHIP-67).
///
/// Minor units as a whole number because money is never a float (`Docs/10` §3.3), and the
/// name carries the unit because a field called `budget` holding `150000` is one somebody
/// eventually reads as dollars.
///
/// `null` means no budget was supplied, which is a different thing from a budget of zero —
/// that distinction is the reason the platform omits the field rather than sending `0`.
///
/// **Read it only on a customer surface.** See the note on this class.
@override@JsonKey(name: 'budget_cents') final  int? budgetCents;
/// When the job stops being offered (SHIP-68).
///
/// Absent while the job is a draft: the clock starts at publication, and a draft may be saved
/// and returned to indefinitely (`Docs/02` §6.3).
@override@JsonKey(name: 'expires_at') final  String? expiresAt;
@override@JsonKey(name: 'created_at') final  String? createdAt;
@override@JsonKey(name: 'updated_at') final  String? updatedAt;

/// Create a copy of Job
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$JobCopyWith<_Job> get copyWith => __$JobCopyWithImpl<_Job>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$JobToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _Job&&(identical(other.id, id) || other.id == id)&&(identical(other.status, status) || other.status == status)&&(identical(other.pickup, pickup) || other.pickup == pickup)&&(identical(other.dropoff, dropoff) || other.dropoff == dropoff)&&(identical(other.goodsDescription, goodsDescription) || other.goodsDescription == goodsDescription)&&(identical(other.lengthCm, lengthCm) || other.lengthCm == lengthCm)&&(identical(other.widthCm, widthCm) || other.widthCm == widthCm)&&(identical(other.heightCm, heightCm) || other.heightCm == heightCm)&&(identical(other.weightKg, weightKg) || other.weightKg == weightKg)&&(identical(other.vehicleRequirement, vehicleRequirement) || other.vehicleRequirement == vehicleRequirement)&&(identical(other.handlingNotes, handlingNotes) || other.handlingNotes == handlingNotes)&&(identical(other.pickupWindow, pickupWindow) || other.pickupWindow == pickupWindow)&&(identical(other.dropoffWindow, dropoffWindow) || other.dropoffWindow == dropoffWindow)&&(identical(other.budgetCents, budgetCents) || other.budgetCents == budgetCents)&&(identical(other.expiresAt, expiresAt) || other.expiresAt == expiresAt)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt)&&(identical(other.updatedAt, updatedAt) || other.updatedAt == updatedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,status,pickup,dropoff,goodsDescription,lengthCm,widthCm,heightCm,weightKg,vehicleRequirement,handlingNotes,pickupWindow,dropoffWindow,budgetCents,expiresAt,createdAt,updatedAt);

@override
String toString() {
  return 'Job(id: $id, status: $status, pickup: $pickup, dropoff: $dropoff, goodsDescription: $goodsDescription, lengthCm: $lengthCm, widthCm: $widthCm, heightCm: $heightCm, weightKg: $weightKg, vehicleRequirement: $vehicleRequirement, handlingNotes: $handlingNotes, pickupWindow: $pickupWindow, dropoffWindow: $dropoffWindow, budgetCents: $budgetCents, expiresAt: $expiresAt, createdAt: $createdAt, updatedAt: $updatedAt)';
}


}

/// @nodoc
abstract mixin class _$JobCopyWith<$Res> implements $JobCopyWith<$Res> {
  factory _$JobCopyWith(_Job value, $Res Function(_Job) _then) = __$JobCopyWithImpl;
@override @useResult
$Res call({
 String id,@JsonKey(unknownEnumValue: JobStatus.unknown) JobStatus status, JobLocation? pickup, JobLocation? dropoff,@JsonKey(name: 'goods_description') String? goodsDescription,@JsonKey(name: 'length_cm') int? lengthCm,@JsonKey(name: 'width_cm') int? widthCm,@JsonKey(name: 'height_cm') int? heightCm,@JsonKey(name: 'weight_kg') double? weightKg,@JsonKey(name: 'vehicle_requirement') String? vehicleRequirement,@JsonKey(name: 'handling_notes') String? handlingNotes,@JsonKey(name: 'pickup_window') JobTimeWindow? pickupWindow,@JsonKey(name: 'dropoff_window') JobTimeWindow? dropoffWindow,@JsonKey(name: 'budget_cents') int? budgetCents,@JsonKey(name: 'expires_at') String? expiresAt,@JsonKey(name: 'created_at') String? createdAt,@JsonKey(name: 'updated_at') String? updatedAt
});


@override $JobLocationCopyWith<$Res>? get pickup;@override $JobLocationCopyWith<$Res>? get dropoff;@override $JobTimeWindowCopyWith<$Res>? get pickupWindow;@override $JobTimeWindowCopyWith<$Res>? get dropoffWindow;

}
/// @nodoc
class __$JobCopyWithImpl<$Res>
    implements _$JobCopyWith<$Res> {
  __$JobCopyWithImpl(this._self, this._then);

  final _Job _self;
  final $Res Function(_Job) _then;

/// Create a copy of Job
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? status = null,Object? pickup = freezed,Object? dropoff = freezed,Object? goodsDescription = freezed,Object? lengthCm = freezed,Object? widthCm = freezed,Object? heightCm = freezed,Object? weightKg = freezed,Object? vehicleRequirement = freezed,Object? handlingNotes = freezed,Object? pickupWindow = freezed,Object? dropoffWindow = freezed,Object? budgetCents = freezed,Object? expiresAt = freezed,Object? createdAt = freezed,Object? updatedAt = freezed,}) {
  return _then(_Job(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,status: null == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as JobStatus,pickup: freezed == pickup ? _self.pickup : pickup // ignore: cast_nullable_to_non_nullable
as JobLocation?,dropoff: freezed == dropoff ? _self.dropoff : dropoff // ignore: cast_nullable_to_non_nullable
as JobLocation?,goodsDescription: freezed == goodsDescription ? _self.goodsDescription : goodsDescription // ignore: cast_nullable_to_non_nullable
as String?,lengthCm: freezed == lengthCm ? _self.lengthCm : lengthCm // ignore: cast_nullable_to_non_nullable
as int?,widthCm: freezed == widthCm ? _self.widthCm : widthCm // ignore: cast_nullable_to_non_nullable
as int?,heightCm: freezed == heightCm ? _self.heightCm : heightCm // ignore: cast_nullable_to_non_nullable
as int?,weightKg: freezed == weightKg ? _self.weightKg : weightKg // ignore: cast_nullable_to_non_nullable
as double?,vehicleRequirement: freezed == vehicleRequirement ? _self.vehicleRequirement : vehicleRequirement // ignore: cast_nullable_to_non_nullable
as String?,handlingNotes: freezed == handlingNotes ? _self.handlingNotes : handlingNotes // ignore: cast_nullable_to_non_nullable
as String?,pickupWindow: freezed == pickupWindow ? _self.pickupWindow : pickupWindow // ignore: cast_nullable_to_non_nullable
as JobTimeWindow?,dropoffWindow: freezed == dropoffWindow ? _self.dropoffWindow : dropoffWindow // ignore: cast_nullable_to_non_nullable
as JobTimeWindow?,budgetCents: freezed == budgetCents ? _self.budgetCents : budgetCents // ignore: cast_nullable_to_non_nullable
as int?,expiresAt: freezed == expiresAt ? _self.expiresAt : expiresAt // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,updatedAt: freezed == updatedAt ? _self.updatedAt : updatedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

/// Create a copy of Job
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobLocationCopyWith<$Res>? get pickup {
    if (_self.pickup == null) {
    return null;
  }

  return $JobLocationCopyWith<$Res>(_self.pickup!, (value) {
    return _then(_self.copyWith(pickup: value));
  });
}/// Create a copy of Job
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobLocationCopyWith<$Res>? get dropoff {
    if (_self.dropoff == null) {
    return null;
  }

  return $JobLocationCopyWith<$Res>(_self.dropoff!, (value) {
    return _then(_self.copyWith(dropoff: value));
  });
}/// Create a copy of Job
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
}/// Create a copy of Job
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
mixin _$JobLocation {

 String get line; String get suburb;/// The state or territory, as the **upper-case abbreviation** — `NSW`, `VIC`.
///
/// A `String` and deliberately not an enum. `Docs/10` §4.7 records this as the one exception
/// to lower snake case on the wire, taken because a client *prints* the state rather than
/// branching on it — and an enum here would be a second list of the eight states to keep in
/// step with the platform's, for no behaviour that depends on it. Input is accepted in any
/// case and as the spelled-out name, so nothing needs normalising on the device either.
 String get state;/// Four digits, leading zero kept — `0800` is Darwin, and an `int` would lose it.
 String get postcode;/// Where the address resolved to, or `null` when it did not (SHIP-59a, SHIP-60).
///
/// **A missing coordinate is an ordinary outcome and never an error.** The platform stores
/// the address exactly as typed and the job proceeds; a client that treated this as a failure
/// would block a rural delivery the marketplace exists to carry.
 Coordinate? get coordinate;
/// Create a copy of JobLocation
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$JobLocationCopyWith<JobLocation> get copyWith => _$JobLocationCopyWithImpl<JobLocation>(this as JobLocation, _$identity);

  /// Serializes this JobLocation to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is JobLocation&&(identical(other.line, line) || other.line == line)&&(identical(other.suburb, suburb) || other.suburb == suburb)&&(identical(other.state, state) || other.state == state)&&(identical(other.postcode, postcode) || other.postcode == postcode)&&(identical(other.coordinate, coordinate) || other.coordinate == coordinate));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,line,suburb,state,postcode,coordinate);

@override
String toString() {
  return 'JobLocation(line: $line, suburb: $suburb, state: $state, postcode: $postcode, coordinate: $coordinate)';
}


}

/// @nodoc
abstract mixin class $JobLocationCopyWith<$Res>  {
  factory $JobLocationCopyWith(JobLocation value, $Res Function(JobLocation) _then) = _$JobLocationCopyWithImpl;
@useResult
$Res call({
 String line, String suburb, String state, String postcode, Coordinate? coordinate
});


$CoordinateCopyWith<$Res>? get coordinate;

}
/// @nodoc
class _$JobLocationCopyWithImpl<$Res>
    implements $JobLocationCopyWith<$Res> {
  _$JobLocationCopyWithImpl(this._self, this._then);

  final JobLocation _self;
  final $Res Function(JobLocation) _then;

/// Create a copy of JobLocation
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? line = null,Object? suburb = null,Object? state = null,Object? postcode = null,Object? coordinate = freezed,}) {
  return _then(_self.copyWith(
line: null == line ? _self.line : line // ignore: cast_nullable_to_non_nullable
as String,suburb: null == suburb ? _self.suburb : suburb // ignore: cast_nullable_to_non_nullable
as String,state: null == state ? _self.state : state // ignore: cast_nullable_to_non_nullable
as String,postcode: null == postcode ? _self.postcode : postcode // ignore: cast_nullable_to_non_nullable
as String,coordinate: freezed == coordinate ? _self.coordinate : coordinate // ignore: cast_nullable_to_non_nullable
as Coordinate?,
  ));
}
/// Create a copy of JobLocation
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$CoordinateCopyWith<$Res>? get coordinate {
    if (_self.coordinate == null) {
    return null;
  }

  return $CoordinateCopyWith<$Res>(_self.coordinate!, (value) {
    return _then(_self.copyWith(coordinate: value));
  });
}
}


/// Adds pattern-matching-related methods to [JobLocation].
extension JobLocationPatterns on JobLocation {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _JobLocation value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _JobLocation() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _JobLocation value)  $default,){
final _that = this;
switch (_that) {
case _JobLocation():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _JobLocation value)?  $default,){
final _that = this;
switch (_that) {
case _JobLocation() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String line,  String suburb,  String state,  String postcode,  Coordinate? coordinate)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _JobLocation() when $default != null:
return $default(_that.line,_that.suburb,_that.state,_that.postcode,_that.coordinate);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String line,  String suburb,  String state,  String postcode,  Coordinate? coordinate)  $default,) {final _that = this;
switch (_that) {
case _JobLocation():
return $default(_that.line,_that.suburb,_that.state,_that.postcode,_that.coordinate);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String line,  String suburb,  String state,  String postcode,  Coordinate? coordinate)?  $default,) {final _that = this;
switch (_that) {
case _JobLocation() when $default != null:
return $default(_that.line,_that.suburb,_that.state,_that.postcode,_that.coordinate);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _JobLocation extends JobLocation {
  const _JobLocation({this.line = '', this.suburb = '', this.state = '', this.postcode = '', this.coordinate}): super._();
  factory _JobLocation.fromJson(Map<String, dynamic> json) => _$JobLocationFromJson(json);

@override@JsonKey() final  String line;
@override@JsonKey() final  String suburb;
/// The state or territory, as the **upper-case abbreviation** — `NSW`, `VIC`.
///
/// A `String` and deliberately not an enum. `Docs/10` §4.7 records this as the one exception
/// to lower snake case on the wire, taken because a client *prints* the state rather than
/// branching on it — and an enum here would be a second list of the eight states to keep in
/// step with the platform's, for no behaviour that depends on it. Input is accepted in any
/// case and as the spelled-out name, so nothing needs normalising on the device either.
@override@JsonKey() final  String state;
/// Four digits, leading zero kept — `0800` is Darwin, and an `int` would lose it.
@override@JsonKey() final  String postcode;
/// Where the address resolved to, or `null` when it did not (SHIP-59a, SHIP-60).
///
/// **A missing coordinate is an ordinary outcome and never an error.** The platform stores
/// the address exactly as typed and the job proceeds; a client that treated this as a failure
/// would block a rural delivery the marketplace exists to carry.
@override final  Coordinate? coordinate;

/// Create a copy of JobLocation
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$JobLocationCopyWith<_JobLocation> get copyWith => __$JobLocationCopyWithImpl<_JobLocation>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$JobLocationToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _JobLocation&&(identical(other.line, line) || other.line == line)&&(identical(other.suburb, suburb) || other.suburb == suburb)&&(identical(other.state, state) || other.state == state)&&(identical(other.postcode, postcode) || other.postcode == postcode)&&(identical(other.coordinate, coordinate) || other.coordinate == coordinate));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,line,suburb,state,postcode,coordinate);

@override
String toString() {
  return 'JobLocation(line: $line, suburb: $suburb, state: $state, postcode: $postcode, coordinate: $coordinate)';
}


}

/// @nodoc
abstract mixin class _$JobLocationCopyWith<$Res> implements $JobLocationCopyWith<$Res> {
  factory _$JobLocationCopyWith(_JobLocation value, $Res Function(_JobLocation) _then) = __$JobLocationCopyWithImpl;
@override @useResult
$Res call({
 String line, String suburb, String state, String postcode, Coordinate? coordinate
});


@override $CoordinateCopyWith<$Res>? get coordinate;

}
/// @nodoc
class __$JobLocationCopyWithImpl<$Res>
    implements _$JobLocationCopyWith<$Res> {
  __$JobLocationCopyWithImpl(this._self, this._then);

  final _JobLocation _self;
  final $Res Function(_JobLocation) _then;

/// Create a copy of JobLocation
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? line = null,Object? suburb = null,Object? state = null,Object? postcode = null,Object? coordinate = freezed,}) {
  return _then(_JobLocation(
line: null == line ? _self.line : line // ignore: cast_nullable_to_non_nullable
as String,suburb: null == suburb ? _self.suburb : suburb // ignore: cast_nullable_to_non_nullable
as String,state: null == state ? _self.state : state // ignore: cast_nullable_to_non_nullable
as String,postcode: null == postcode ? _self.postcode : postcode // ignore: cast_nullable_to_non_nullable
as String,coordinate: freezed == coordinate ? _self.coordinate : coordinate // ignore: cast_nullable_to_non_nullable
as Coordinate?,
  ));
}

/// Create a copy of JobLocation
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$CoordinateCopyWith<$Res>? get coordinate {
    if (_self.coordinate == null) {
    return null;
  }

  return $CoordinateCopyWith<$Res>(_self.coordinate!, (value) {
    return _then(_self.copyWith(coordinate: value));
  });
}
}


/// @nodoc
mixin _$Coordinate {

 double get latitude; double get longitude;/// The geocoder's own rendering of the place it matched, which is not always what the
/// customer typed.
///
/// This is the field worth showing back: it answers "did the platform understand the address
/// I gave it", which is a different and more useful question than a pair of numbers. Omitted
/// when the provider supplied none.
 String? get formatted;
/// Create a copy of Coordinate
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$CoordinateCopyWith<Coordinate> get copyWith => _$CoordinateCopyWithImpl<Coordinate>(this as Coordinate, _$identity);

  /// Serializes this Coordinate to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is Coordinate&&(identical(other.latitude, latitude) || other.latitude == latitude)&&(identical(other.longitude, longitude) || other.longitude == longitude)&&(identical(other.formatted, formatted) || other.formatted == formatted));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,latitude,longitude,formatted);

@override
String toString() {
  return 'Coordinate(latitude: $latitude, longitude: $longitude, formatted: $formatted)';
}


}

/// @nodoc
abstract mixin class $CoordinateCopyWith<$Res>  {
  factory $CoordinateCopyWith(Coordinate value, $Res Function(Coordinate) _then) = _$CoordinateCopyWithImpl;
@useResult
$Res call({
 double latitude, double longitude, String? formatted
});




}
/// @nodoc
class _$CoordinateCopyWithImpl<$Res>
    implements $CoordinateCopyWith<$Res> {
  _$CoordinateCopyWithImpl(this._self, this._then);

  final Coordinate _self;
  final $Res Function(Coordinate) _then;

/// Create a copy of Coordinate
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? latitude = null,Object? longitude = null,Object? formatted = freezed,}) {
  return _then(_self.copyWith(
latitude: null == latitude ? _self.latitude : latitude // ignore: cast_nullable_to_non_nullable
as double,longitude: null == longitude ? _self.longitude : longitude // ignore: cast_nullable_to_non_nullable
as double,formatted: freezed == formatted ? _self.formatted : formatted // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [Coordinate].
extension CoordinatePatterns on Coordinate {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _Coordinate value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _Coordinate() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _Coordinate value)  $default,){
final _that = this;
switch (_that) {
case _Coordinate():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _Coordinate value)?  $default,){
final _that = this;
switch (_that) {
case _Coordinate() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( double latitude,  double longitude,  String? formatted)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _Coordinate() when $default != null:
return $default(_that.latitude,_that.longitude,_that.formatted);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( double latitude,  double longitude,  String? formatted)  $default,) {final _that = this;
switch (_that) {
case _Coordinate():
return $default(_that.latitude,_that.longitude,_that.formatted);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( double latitude,  double longitude,  String? formatted)?  $default,) {final _that = this;
switch (_that) {
case _Coordinate() when $default != null:
return $default(_that.latitude,_that.longitude,_that.formatted);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _Coordinate implements Coordinate {
  const _Coordinate({this.latitude = 0.0, this.longitude = 0.0, this.formatted});
  factory _Coordinate.fromJson(Map<String, dynamic> json) => _$CoordinateFromJson(json);

@override@JsonKey() final  double latitude;
@override@JsonKey() final  double longitude;
/// The geocoder's own rendering of the place it matched, which is not always what the
/// customer typed.
///
/// This is the field worth showing back: it answers "did the platform understand the address
/// I gave it", which is a different and more useful question than a pair of numbers. Omitted
/// when the provider supplied none.
@override final  String? formatted;

/// Create a copy of Coordinate
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$CoordinateCopyWith<_Coordinate> get copyWith => __$CoordinateCopyWithImpl<_Coordinate>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$CoordinateToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _Coordinate&&(identical(other.latitude, latitude) || other.latitude == latitude)&&(identical(other.longitude, longitude) || other.longitude == longitude)&&(identical(other.formatted, formatted) || other.formatted == formatted));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,latitude,longitude,formatted);

@override
String toString() {
  return 'Coordinate(latitude: $latitude, longitude: $longitude, formatted: $formatted)';
}


}

/// @nodoc
abstract mixin class _$CoordinateCopyWith<$Res> implements $CoordinateCopyWith<$Res> {
  factory _$CoordinateCopyWith(_Coordinate value, $Res Function(_Coordinate) _then) = __$CoordinateCopyWithImpl;
@override @useResult
$Res call({
 double latitude, double longitude, String? formatted
});




}
/// @nodoc
class __$CoordinateCopyWithImpl<$Res>
    implements _$CoordinateCopyWith<$Res> {
  __$CoordinateCopyWithImpl(this._self, this._then);

  final _Coordinate _self;
  final $Res Function(_Coordinate) _then;

/// Create a copy of Coordinate
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? latitude = null,Object? longitude = null,Object? formatted = freezed,}) {
  return _then(_Coordinate(
latitude: null == latitude ? _self.latitude : latitude // ignore: cast_nullable_to_non_nullable
as double,longitude: null == longitude ? _self.longitude : longitude // ignore: cast_nullable_to_non_nullable
as double,formatted: freezed == formatted ? _self.formatted : formatted // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}


/// @nodoc
mixin _$JobTimeWindow {

 String? get start; String? get end;
/// Create a copy of JobTimeWindow
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$JobTimeWindowCopyWith<JobTimeWindow> get copyWith => _$JobTimeWindowCopyWithImpl<JobTimeWindow>(this as JobTimeWindow, _$identity);

  /// Serializes this JobTimeWindow to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is JobTimeWindow&&(identical(other.start, start) || other.start == start)&&(identical(other.end, end) || other.end == end));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,start,end);

@override
String toString() {
  return 'JobTimeWindow(start: $start, end: $end)';
}


}

/// @nodoc
abstract mixin class $JobTimeWindowCopyWith<$Res>  {
  factory $JobTimeWindowCopyWith(JobTimeWindow value, $Res Function(JobTimeWindow) _then) = _$JobTimeWindowCopyWithImpl;
@useResult
$Res call({
 String? start, String? end
});




}
/// @nodoc
class _$JobTimeWindowCopyWithImpl<$Res>
    implements $JobTimeWindowCopyWith<$Res> {
  _$JobTimeWindowCopyWithImpl(this._self, this._then);

  final JobTimeWindow _self;
  final $Res Function(JobTimeWindow) _then;

/// Create a copy of JobTimeWindow
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? start = freezed,Object? end = freezed,}) {
  return _then(_self.copyWith(
start: freezed == start ? _self.start : start // ignore: cast_nullable_to_non_nullable
as String?,end: freezed == end ? _self.end : end // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [JobTimeWindow].
extension JobTimeWindowPatterns on JobTimeWindow {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _JobTimeWindow value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _JobTimeWindow() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _JobTimeWindow value)  $default,){
final _that = this;
switch (_that) {
case _JobTimeWindow():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _JobTimeWindow value)?  $default,){
final _that = this;
switch (_that) {
case _JobTimeWindow() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String? start,  String? end)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _JobTimeWindow() when $default != null:
return $default(_that.start,_that.end);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String? start,  String? end)  $default,) {final _that = this;
switch (_that) {
case _JobTimeWindow():
return $default(_that.start,_that.end);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String? start,  String? end)?  $default,) {final _that = this;
switch (_that) {
case _JobTimeWindow() when $default != null:
return $default(_that.start,_that.end);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _JobTimeWindow implements JobTimeWindow {
  const _JobTimeWindow({this.start, this.end});
  factory _JobTimeWindow.fromJson(Map<String, dynamic> json) => _$JobTimeWindowFromJson(json);

@override final  String? start;
@override final  String? end;

/// Create a copy of JobTimeWindow
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$JobTimeWindowCopyWith<_JobTimeWindow> get copyWith => __$JobTimeWindowCopyWithImpl<_JobTimeWindow>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$JobTimeWindowToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _JobTimeWindow&&(identical(other.start, start) || other.start == start)&&(identical(other.end, end) || other.end == end));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,start,end);

@override
String toString() {
  return 'JobTimeWindow(start: $start, end: $end)';
}


}

/// @nodoc
abstract mixin class _$JobTimeWindowCopyWith<$Res> implements $JobTimeWindowCopyWith<$Res> {
  factory _$JobTimeWindowCopyWith(_JobTimeWindow value, $Res Function(_JobTimeWindow) _then) = __$JobTimeWindowCopyWithImpl;
@override @useResult
$Res call({
 String? start, String? end
});




}
/// @nodoc
class __$JobTimeWindowCopyWithImpl<$Res>
    implements _$JobTimeWindowCopyWith<$Res> {
  __$JobTimeWindowCopyWithImpl(this._self, this._then);

  final _JobTimeWindow _self;
  final $Res Function(_JobTimeWindow) _then;

/// Create a copy of JobTimeWindow
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? start = freezed,Object? end = freezed,}) {
  return _then(_JobTimeWindow(
start: freezed == start ? _self.start : start // ignore: cast_nullable_to_non_nullable
as String?,end: freezed == end ? _self.end : end // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
