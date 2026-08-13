// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'fleet_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$FleetState {

/// The **first** page is being read, or a retry after a failure is.
///
/// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
/// replacing the list with a second one would take the fleet away from somebody who pulled
/// precisely to look at it.
 bool get loading;/// A further page is being read.
 bool get loadingMore;/// Every vehicle read so far, newest first, in the platform's own order.
 List<Vehicle> get vehicles;/// Whether a page has arrived at all.
///
/// This is what separates "no vehicles" from "not yet asked", and the separation is the whole of
/// the empty state: a provider who has just signed up must see an empty state and never a
/// spinner that does not resolve.
 bool get loaded;/// The position to ask from next. **Opaque** — passed back exactly as it arrived.
 String? get nextCursor;/// Whether asking again would return anything.
 bool get hasMore;/// What the last read failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of FleetState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$FleetStateCopyWith<FleetState> get copyWith => _$FleetStateCopyWithImpl<FleetState>(this as FleetState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is FleetState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&const DeepCollectionEquality().equals(other.vehicles, vehicles)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,const DeepCollectionEquality().hash(vehicles),loaded,nextCursor,hasMore,failure);

@override
String toString() {
  return 'FleetState(loading: $loading, loadingMore: $loadingMore, vehicles: $vehicles, loaded: $loaded, nextCursor: $nextCursor, hasMore: $hasMore, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $FleetStateCopyWith<$Res>  {
  factory $FleetStateCopyWith(FleetState value, $Res Function(FleetState) _then) = _$FleetStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool loadingMore, List<Vehicle> vehicles, bool loaded, String? nextCursor, bool hasMore, ApiFailure? failure
});




}
/// @nodoc
class _$FleetStateCopyWithImpl<$Res>
    implements $FleetStateCopyWith<$Res> {
  _$FleetStateCopyWithImpl(this._self, this._then);

  final FleetState _self;
  final $Res Function(FleetState) _then;

/// Create a copy of FleetState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? loadingMore = null,Object? vehicles = null,Object? loaded = null,Object? nextCursor = freezed,Object? hasMore = null,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,vehicles: null == vehicles ? _self.vehicles : vehicles // ignore: cast_nullable_to_non_nullable
as List<Vehicle>,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

}


/// Adds pattern-matching-related methods to [FleetState].
extension FleetStatePatterns on FleetState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _FleetState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _FleetState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _FleetState value)  $default,){
final _that = this;
switch (_that) {
case _FleetState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _FleetState value)?  $default,){
final _that = this;
switch (_that) {
case _FleetState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  List<Vehicle> vehicles,  bool loaded,  String? nextCursor,  bool hasMore,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _FleetState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.vehicles,_that.loaded,_that.nextCursor,_that.hasMore,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  List<Vehicle> vehicles,  bool loaded,  String? nextCursor,  bool hasMore,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _FleetState():
return $default(_that.loading,_that.loadingMore,_that.vehicles,_that.loaded,_that.nextCursor,_that.hasMore,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool loadingMore,  List<Vehicle> vehicles,  bool loaded,  String? nextCursor,  bool hasMore,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _FleetState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.vehicles,_that.loaded,_that.nextCursor,_that.hasMore,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _FleetState extends FleetState {
  const _FleetState({this.loading = true, this.loadingMore = false, final  List<Vehicle> vehicles = const <Vehicle>[], this.loaded = false, this.nextCursor, this.hasMore = false, this.failure}): _vehicles = vehicles,super._();
  

/// The **first** page is being read, or a retry after a failure is.
///
/// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
/// replacing the list with a second one would take the fleet away from somebody who pulled
/// precisely to look at it.
@override@JsonKey() final  bool loading;
/// A further page is being read.
@override@JsonKey() final  bool loadingMore;
/// Every vehicle read so far, newest first, in the platform's own order.
 final  List<Vehicle> _vehicles;
/// Every vehicle read so far, newest first, in the platform's own order.
@override@JsonKey() List<Vehicle> get vehicles {
  if (_vehicles is EqualUnmodifiableListView) return _vehicles;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_vehicles);
}

/// Whether a page has arrived at all.
///
/// This is what separates "no vehicles" from "not yet asked", and the separation is the whole of
/// the empty state: a provider who has just signed up must see an empty state and never a
/// spinner that does not resolve.
@override@JsonKey() final  bool loaded;
/// The position to ask from next. **Opaque** — passed back exactly as it arrived.
@override final  String? nextCursor;
/// Whether asking again would return anything.
@override@JsonKey() final  bool hasMore;
/// What the last read failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of FleetState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$FleetStateCopyWith<_FleetState> get copyWith => __$FleetStateCopyWithImpl<_FleetState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _FleetState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&const DeepCollectionEquality().equals(other._vehicles, _vehicles)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,const DeepCollectionEquality().hash(_vehicles),loaded,nextCursor,hasMore,failure);

@override
String toString() {
  return 'FleetState(loading: $loading, loadingMore: $loadingMore, vehicles: $vehicles, loaded: $loaded, nextCursor: $nextCursor, hasMore: $hasMore, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$FleetStateCopyWith<$Res> implements $FleetStateCopyWith<$Res> {
  factory _$FleetStateCopyWith(_FleetState value, $Res Function(_FleetState) _then) = __$FleetStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool loadingMore, List<Vehicle> vehicles, bool loaded, String? nextCursor, bool hasMore, ApiFailure? failure
});




}
/// @nodoc
class __$FleetStateCopyWithImpl<$Res>
    implements _$FleetStateCopyWith<$Res> {
  __$FleetStateCopyWithImpl(this._self, this._then);

  final _FleetState _self;
  final $Res Function(_FleetState) _then;

/// Create a copy of FleetState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? loadingMore = null,Object? vehicles = null,Object? loaded = null,Object? nextCursor = freezed,Object? hasMore = null,Object? failure = freezed,}) {
  return _then(_FleetState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,vehicles: null == vehicles ? _self._vehicles : vehicles // ignore: cast_nullable_to_non_nullable
as List<Vehicle>,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}


}

/// @nodoc
mixin _$AddVehicleState {

/// A save is in flight. The screen disables its submit while it is true, which is what stops a
/// second tap becoming a second **vehicle** — two taps are two actions, and an idempotency key
/// is deliberately per-action (`Docs/07` §4).
 bool get busy;/// What the last attempt failed with, or `null`. Cleared when the next one starts.
 ApiFailure? get failure;
/// Create a copy of AddVehicleState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$AddVehicleStateCopyWith<AddVehicleState> get copyWith => _$AddVehicleStateCopyWithImpl<AddVehicleState>(this as AddVehicleState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is AddVehicleState&&(identical(other.busy, busy) || other.busy == busy)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,busy,failure);

@override
String toString() {
  return 'AddVehicleState(busy: $busy, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $AddVehicleStateCopyWith<$Res>  {
  factory $AddVehicleStateCopyWith(AddVehicleState value, $Res Function(AddVehicleState) _then) = _$AddVehicleStateCopyWithImpl;
@useResult
$Res call({
 bool busy, ApiFailure? failure
});




}
/// @nodoc
class _$AddVehicleStateCopyWithImpl<$Res>
    implements $AddVehicleStateCopyWith<$Res> {
  _$AddVehicleStateCopyWithImpl(this._self, this._then);

  final AddVehicleState _self;
  final $Res Function(AddVehicleState) _then;

/// Create a copy of AddVehicleState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? busy = null,Object? failure = freezed,}) {
  return _then(_self.copyWith(
busy: null == busy ? _self.busy : busy // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

}


/// Adds pattern-matching-related methods to [AddVehicleState].
extension AddVehicleStatePatterns on AddVehicleState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _AddVehicleState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _AddVehicleState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _AddVehicleState value)  $default,){
final _that = this;
switch (_that) {
case _AddVehicleState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _AddVehicleState value)?  $default,){
final _that = this;
switch (_that) {
case _AddVehicleState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool busy,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _AddVehicleState() when $default != null:
return $default(_that.busy,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool busy,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _AddVehicleState():
return $default(_that.busy,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool busy,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _AddVehicleState() when $default != null:
return $default(_that.busy,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _AddVehicleState implements AddVehicleState {
  const _AddVehicleState({this.busy = false, this.failure});
  

/// A save is in flight. The screen disables its submit while it is true, which is what stops a
/// second tap becoming a second **vehicle** — two taps are two actions, and an idempotency key
/// is deliberately per-action (`Docs/07` §4).
@override@JsonKey() final  bool busy;
/// What the last attempt failed with, or `null`. Cleared when the next one starts.
@override final  ApiFailure? failure;

/// Create a copy of AddVehicleState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$AddVehicleStateCopyWith<_AddVehicleState> get copyWith => __$AddVehicleStateCopyWithImpl<_AddVehicleState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _AddVehicleState&&(identical(other.busy, busy) || other.busy == busy)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,busy,failure);

@override
String toString() {
  return 'AddVehicleState(busy: $busy, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$AddVehicleStateCopyWith<$Res> implements $AddVehicleStateCopyWith<$Res> {
  factory _$AddVehicleStateCopyWith(_AddVehicleState value, $Res Function(_AddVehicleState) _then) = __$AddVehicleStateCopyWithImpl;
@override @useResult
$Res call({
 bool busy, ApiFailure? failure
});




}
/// @nodoc
class __$AddVehicleStateCopyWithImpl<$Res>
    implements _$AddVehicleStateCopyWith<$Res> {
  __$AddVehicleStateCopyWithImpl(this._self, this._then);

  final _AddVehicleState _self;
  final $Res Function(_AddVehicleState) _then;

/// Create a copy of AddVehicleState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? busy = null,Object? failure = freezed,}) {
  return _then(_AddVehicleState(
busy: null == busy ? _self.busy : busy // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}


}

// dart format on
