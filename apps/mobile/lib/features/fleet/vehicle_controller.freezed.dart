// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'vehicle_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$VehicleState {

/// The vehicle is being read, or re-read after a failure.
///
/// Deliberately not set by a pull-to-refresh, for the same reason as the fleet list:
/// `RefreshIndicator` draws its own spinner, and replacing the vehicle with a second one would
/// take it away from somebody who pulled precisely to look at it.
 bool get loading;/// An action is in flight. The screen disables its buttons while it is true, which is what stops
/// a second tap becoming a second action.
 bool get acting;/// Whether the provider has asked to change the vehicle's details.
///
/// The screen has two faces — the vehicle, and the form that edits it — and which one is showing
/// is a fact about the screen's state rather than about the widget. Keeping it here is what lets
/// the sequence be read, and tested, without a screen. Same shape, and the same reasoning, as
/// `JobLocationsState.editing`.
 bool get editing;/// The vehicle, once it has arrived.
 Vehicle? get vehicle;/// What the last read or action failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of VehicleState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$VehicleStateCopyWith<VehicleState> get copyWith => _$VehicleStateCopyWithImpl<VehicleState>(this as VehicleState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is VehicleState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.acting, acting) || other.acting == acting)&&(identical(other.editing, editing) || other.editing == editing)&&(identical(other.vehicle, vehicle) || other.vehicle == vehicle)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,acting,editing,vehicle,failure);

@override
String toString() {
  return 'VehicleState(loading: $loading, acting: $acting, editing: $editing, vehicle: $vehicle, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $VehicleStateCopyWith<$Res>  {
  factory $VehicleStateCopyWith(VehicleState value, $Res Function(VehicleState) _then) = _$VehicleStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool acting, bool editing, Vehicle? vehicle, ApiFailure? failure
});


$VehicleCopyWith<$Res>? get vehicle;

}
/// @nodoc
class _$VehicleStateCopyWithImpl<$Res>
    implements $VehicleStateCopyWith<$Res> {
  _$VehicleStateCopyWithImpl(this._self, this._then);

  final VehicleState _self;
  final $Res Function(VehicleState) _then;

/// Create a copy of VehicleState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? acting = null,Object? editing = null,Object? vehicle = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,acting: null == acting ? _self.acting : acting // ignore: cast_nullable_to_non_nullable
as bool,editing: null == editing ? _self.editing : editing // ignore: cast_nullable_to_non_nullable
as bool,vehicle: freezed == vehicle ? _self.vehicle : vehicle // ignore: cast_nullable_to_non_nullable
as Vehicle?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of VehicleState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$VehicleCopyWith<$Res>? get vehicle {
    if (_self.vehicle == null) {
    return null;
  }

  return $VehicleCopyWith<$Res>(_self.vehicle!, (value) {
    return _then(_self.copyWith(vehicle: value));
  });
}
}


/// Adds pattern-matching-related methods to [VehicleState].
extension VehicleStatePatterns on VehicleState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _VehicleState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _VehicleState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _VehicleState value)  $default,){
final _that = this;
switch (_that) {
case _VehicleState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _VehicleState value)?  $default,){
final _that = this;
switch (_that) {
case _VehicleState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool acting,  bool editing,  Vehicle? vehicle,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _VehicleState() when $default != null:
return $default(_that.loading,_that.acting,_that.editing,_that.vehicle,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool acting,  bool editing,  Vehicle? vehicle,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _VehicleState():
return $default(_that.loading,_that.acting,_that.editing,_that.vehicle,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool acting,  bool editing,  Vehicle? vehicle,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _VehicleState() when $default != null:
return $default(_that.loading,_that.acting,_that.editing,_that.vehicle,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _VehicleState extends VehicleState {
  const _VehicleState({this.loading = true, this.acting = false, this.editing = false, this.vehicle, this.failure}): super._();
  

/// The vehicle is being read, or re-read after a failure.
///
/// Deliberately not set by a pull-to-refresh, for the same reason as the fleet list:
/// `RefreshIndicator` draws its own spinner, and replacing the vehicle with a second one would
/// take it away from somebody who pulled precisely to look at it.
@override@JsonKey() final  bool loading;
/// An action is in flight. The screen disables its buttons while it is true, which is what stops
/// a second tap becoming a second action.
@override@JsonKey() final  bool acting;
/// Whether the provider has asked to change the vehicle's details.
///
/// The screen has two faces — the vehicle, and the form that edits it — and which one is showing
/// is a fact about the screen's state rather than about the widget. Keeping it here is what lets
/// the sequence be read, and tested, without a screen. Same shape, and the same reasoning, as
/// `JobLocationsState.editing`.
@override@JsonKey() final  bool editing;
/// The vehicle, once it has arrived.
@override final  Vehicle? vehicle;
/// What the last read or action failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of VehicleState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$VehicleStateCopyWith<_VehicleState> get copyWith => __$VehicleStateCopyWithImpl<_VehicleState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _VehicleState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.acting, acting) || other.acting == acting)&&(identical(other.editing, editing) || other.editing == editing)&&(identical(other.vehicle, vehicle) || other.vehicle == vehicle)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,acting,editing,vehicle,failure);

@override
String toString() {
  return 'VehicleState(loading: $loading, acting: $acting, editing: $editing, vehicle: $vehicle, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$VehicleStateCopyWith<$Res> implements $VehicleStateCopyWith<$Res> {
  factory _$VehicleStateCopyWith(_VehicleState value, $Res Function(_VehicleState) _then) = __$VehicleStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool acting, bool editing, Vehicle? vehicle, ApiFailure? failure
});


@override $VehicleCopyWith<$Res>? get vehicle;

}
/// @nodoc
class __$VehicleStateCopyWithImpl<$Res>
    implements _$VehicleStateCopyWith<$Res> {
  __$VehicleStateCopyWithImpl(this._self, this._then);

  final _VehicleState _self;
  final $Res Function(_VehicleState) _then;

/// Create a copy of VehicleState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? acting = null,Object? editing = null,Object? vehicle = freezed,Object? failure = freezed,}) {
  return _then(_VehicleState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,acting: null == acting ? _self.acting : acting // ignore: cast_nullable_to_non_nullable
as bool,editing: null == editing ? _self.editing : editing // ignore: cast_nullable_to_non_nullable
as bool,vehicle: freezed == vehicle ? _self.vehicle : vehicle // ignore: cast_nullable_to_non_nullable
as Vehicle?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of VehicleState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$VehicleCopyWith<$Res>? get vehicle {
    if (_self.vehicle == null) {
    return null;
  }

  return $VehicleCopyWith<$Res>(_self.vehicle!, (value) {
    return _then(_self.copyWith(vehicle: value));
  });
}
}

// dart format on
