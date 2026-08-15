// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'tracking_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$TrackingState {

/// The **first** read is in flight, or a retry after a failure is.
 bool get loading;/// A further page of milestones is being read.
 bool get loadingMore;/// Whether a complete read has arrived at all.
///
/// What separates "nothing has been recorded yet" from "not yet asked", and the separation is
/// the whole of the empty state: a job that was published this morning has an empty milestone
/// list, and a client that treated that as "still loading" would leave the customer watching a
/// spinner until somebody collected their goods.
 bool get loaded;/// Who is carrying it, or that nobody is yet.
 DeliveryDriver? get driver;/// Every milestone read so far, newest first by the actor's clock.
 List<RecordedMilestone> get milestones;/// Every piece of evidence on this delivery, newest acted first.
///
/// **Never persisted.** Each carries a URL signed for this caller and good until it expires; a
/// stored copy is a stored credential. See [DeliveryProof].
 List<DeliveryProof> get proof;/// The position to ask from next, for the milestone list. **Opaque.**
 String? get nextCursor;/// Whether asking again would return more milestones.
 bool get hasMore;/// What the last read failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of TrackingState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$TrackingStateCopyWith<TrackingState> get copyWith => _$TrackingStateCopyWithImpl<TrackingState>(this as TrackingState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is TrackingState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.driver, driver) || other.driver == driver)&&const DeepCollectionEquality().equals(other.milestones, milestones)&&const DeepCollectionEquality().equals(other.proof, proof)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,loaded,driver,const DeepCollectionEquality().hash(milestones),const DeepCollectionEquality().hash(proof),nextCursor,hasMore,failure);

@override
String toString() {
  return 'TrackingState(loading: $loading, loadingMore: $loadingMore, loaded: $loaded, driver: $driver, milestones: $milestones, proof: $proof, nextCursor: $nextCursor, hasMore: $hasMore, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $TrackingStateCopyWith<$Res>  {
  factory $TrackingStateCopyWith(TrackingState value, $Res Function(TrackingState) _then) = _$TrackingStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool loadingMore, bool loaded, DeliveryDriver? driver, List<RecordedMilestone> milestones, List<DeliveryProof> proof, String? nextCursor, bool hasMore, ApiFailure? failure
});


$DeliveryDriverCopyWith<$Res>? get driver;

}
/// @nodoc
class _$TrackingStateCopyWithImpl<$Res>
    implements $TrackingStateCopyWith<$Res> {
  _$TrackingStateCopyWithImpl(this._self, this._then);

  final TrackingState _self;
  final $Res Function(TrackingState) _then;

/// Create a copy of TrackingState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? loadingMore = null,Object? loaded = null,Object? driver = freezed,Object? milestones = null,Object? proof = null,Object? nextCursor = freezed,Object? hasMore = null,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,driver: freezed == driver ? _self.driver : driver // ignore: cast_nullable_to_non_nullable
as DeliveryDriver?,milestones: null == milestones ? _self.milestones : milestones // ignore: cast_nullable_to_non_nullable
as List<RecordedMilestone>,proof: null == proof ? _self.proof : proof // ignore: cast_nullable_to_non_nullable
as List<DeliveryProof>,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of TrackingState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$DeliveryDriverCopyWith<$Res>? get driver {
    if (_self.driver == null) {
    return null;
  }

  return $DeliveryDriverCopyWith<$Res>(_self.driver!, (value) {
    return _then(_self.copyWith(driver: value));
  });
}
}


/// Adds pattern-matching-related methods to [TrackingState].
extension TrackingStatePatterns on TrackingState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _TrackingState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _TrackingState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _TrackingState value)  $default,){
final _that = this;
switch (_that) {
case _TrackingState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _TrackingState value)?  $default,){
final _that = this;
switch (_that) {
case _TrackingState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  bool loaded,  DeliveryDriver? driver,  List<RecordedMilestone> milestones,  List<DeliveryProof> proof,  String? nextCursor,  bool hasMore,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _TrackingState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.loaded,_that.driver,_that.milestones,_that.proof,_that.nextCursor,_that.hasMore,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  bool loaded,  DeliveryDriver? driver,  List<RecordedMilestone> milestones,  List<DeliveryProof> proof,  String? nextCursor,  bool hasMore,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _TrackingState():
return $default(_that.loading,_that.loadingMore,_that.loaded,_that.driver,_that.milestones,_that.proof,_that.nextCursor,_that.hasMore,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool loadingMore,  bool loaded,  DeliveryDriver? driver,  List<RecordedMilestone> milestones,  List<DeliveryProof> proof,  String? nextCursor,  bool hasMore,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _TrackingState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.loaded,_that.driver,_that.milestones,_that.proof,_that.nextCursor,_that.hasMore,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _TrackingState extends TrackingState {
  const _TrackingState({this.loading = true, this.loadingMore = false, this.loaded = false, this.driver, final  List<RecordedMilestone> milestones = const <RecordedMilestone>[], final  List<DeliveryProof> proof = const <DeliveryProof>[], this.nextCursor, this.hasMore = false, this.failure}): _milestones = milestones,_proof = proof,super._();
  

/// The **first** read is in flight, or a retry after a failure is.
@override@JsonKey() final  bool loading;
/// A further page of milestones is being read.
@override@JsonKey() final  bool loadingMore;
/// Whether a complete read has arrived at all.
///
/// What separates "nothing has been recorded yet" from "not yet asked", and the separation is
/// the whole of the empty state: a job that was published this morning has an empty milestone
/// list, and a client that treated that as "still loading" would leave the customer watching a
/// spinner until somebody collected their goods.
@override@JsonKey() final  bool loaded;
/// Who is carrying it, or that nobody is yet.
@override final  DeliveryDriver? driver;
/// Every milestone read so far, newest first by the actor's clock.
 final  List<RecordedMilestone> _milestones;
/// Every milestone read so far, newest first by the actor's clock.
@override@JsonKey() List<RecordedMilestone> get milestones {
  if (_milestones is EqualUnmodifiableListView) return _milestones;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_milestones);
}

/// Every piece of evidence on this delivery, newest acted first.
///
/// **Never persisted.** Each carries a URL signed for this caller and good until it expires; a
/// stored copy is a stored credential. See [DeliveryProof].
 final  List<DeliveryProof> _proof;
/// Every piece of evidence on this delivery, newest acted first.
///
/// **Never persisted.** Each carries a URL signed for this caller and good until it expires; a
/// stored copy is a stored credential. See [DeliveryProof].
@override@JsonKey() List<DeliveryProof> get proof {
  if (_proof is EqualUnmodifiableListView) return _proof;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_proof);
}

/// The position to ask from next, for the milestone list. **Opaque.**
@override final  String? nextCursor;
/// Whether asking again would return more milestones.
@override@JsonKey() final  bool hasMore;
/// What the last read failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of TrackingState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$TrackingStateCopyWith<_TrackingState> get copyWith => __$TrackingStateCopyWithImpl<_TrackingState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _TrackingState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.driver, driver) || other.driver == driver)&&const DeepCollectionEquality().equals(other._milestones, _milestones)&&const DeepCollectionEquality().equals(other._proof, _proof)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,loaded,driver,const DeepCollectionEquality().hash(_milestones),const DeepCollectionEquality().hash(_proof),nextCursor,hasMore,failure);

@override
String toString() {
  return 'TrackingState(loading: $loading, loadingMore: $loadingMore, loaded: $loaded, driver: $driver, milestones: $milestones, proof: $proof, nextCursor: $nextCursor, hasMore: $hasMore, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$TrackingStateCopyWith<$Res> implements $TrackingStateCopyWith<$Res> {
  factory _$TrackingStateCopyWith(_TrackingState value, $Res Function(_TrackingState) _then) = __$TrackingStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool loadingMore, bool loaded, DeliveryDriver? driver, List<RecordedMilestone> milestones, List<DeliveryProof> proof, String? nextCursor, bool hasMore, ApiFailure? failure
});


@override $DeliveryDriverCopyWith<$Res>? get driver;

}
/// @nodoc
class __$TrackingStateCopyWithImpl<$Res>
    implements _$TrackingStateCopyWith<$Res> {
  __$TrackingStateCopyWithImpl(this._self, this._then);

  final _TrackingState _self;
  final $Res Function(_TrackingState) _then;

/// Create a copy of TrackingState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? loadingMore = null,Object? loaded = null,Object? driver = freezed,Object? milestones = null,Object? proof = null,Object? nextCursor = freezed,Object? hasMore = null,Object? failure = freezed,}) {
  return _then(_TrackingState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,driver: freezed == driver ? _self.driver : driver // ignore: cast_nullable_to_non_nullable
as DeliveryDriver?,milestones: null == milestones ? _self._milestones : milestones // ignore: cast_nullable_to_non_nullable
as List<RecordedMilestone>,proof: null == proof ? _self._proof : proof // ignore: cast_nullable_to_non_nullable
as List<DeliveryProof>,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of TrackingState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$DeliveryDriverCopyWith<$Res>? get driver {
    if (_self.driver == null) {
    return null;
  }

  return $DeliveryDriverCopyWith<$Res>(_self.driver!, (value) {
    return _then(_self.copyWith(driver: value));
  });
}
}

// dart format on
