// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'award_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$AwardState {

/// The award is on its way to the platform.
///
/// **The only "pending" there is**, and deliberately so: `Docs/07` §4 keeps awarding out of the
/// offline queue with bidding and negotiation, because "a stale local decision is worse than an
/// honest 'you are offline'". Nothing durable is written, so either the platform answered or it
/// did not.
 bool get awarding;/// The offer the platform accepted, once it has.
///
/// **Assigned from the response and from nothing else.** A retry answered `200` with an earlier
/// attempt's award therefore reconciles this screen to the platform's record rather than to
/// what the customer last tapped — which is the case that actually happens, because the second
/// row of the contract's retry table is a phone that lost its connection and generated a fresh
/// key for the same intent.
 Bid? get accepted;/// What the last attempt failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of AwardState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$AwardStateCopyWith<AwardState> get copyWith => _$AwardStateCopyWithImpl<AwardState>(this as AwardState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is AwardState&&(identical(other.awarding, awarding) || other.awarding == awarding)&&(identical(other.accepted, accepted) || other.accepted == accepted)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,awarding,accepted,failure);

@override
String toString() {
  return 'AwardState(awarding: $awarding, accepted: $accepted, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $AwardStateCopyWith<$Res>  {
  factory $AwardStateCopyWith(AwardState value, $Res Function(AwardState) _then) = _$AwardStateCopyWithImpl;
@useResult
$Res call({
 bool awarding, Bid? accepted, ApiFailure? failure
});


$BidCopyWith<$Res>? get accepted;

}
/// @nodoc
class _$AwardStateCopyWithImpl<$Res>
    implements $AwardStateCopyWith<$Res> {
  _$AwardStateCopyWithImpl(this._self, this._then);

  final AwardState _self;
  final $Res Function(AwardState) _then;

/// Create a copy of AwardState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? awarding = null,Object? accepted = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
awarding: null == awarding ? _self.awarding : awarding // ignore: cast_nullable_to_non_nullable
as bool,accepted: freezed == accepted ? _self.accepted : accepted // ignore: cast_nullable_to_non_nullable
as Bid?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of AwardState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$BidCopyWith<$Res>? get accepted {
    if (_self.accepted == null) {
    return null;
  }

  return $BidCopyWith<$Res>(_self.accepted!, (value) {
    return _then(_self.copyWith(accepted: value));
  });
}
}


/// Adds pattern-matching-related methods to [AwardState].
extension AwardStatePatterns on AwardState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _AwardState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _AwardState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _AwardState value)  $default,){
final _that = this;
switch (_that) {
case _AwardState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _AwardState value)?  $default,){
final _that = this;
switch (_that) {
case _AwardState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool awarding,  Bid? accepted,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _AwardState() when $default != null:
return $default(_that.awarding,_that.accepted,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool awarding,  Bid? accepted,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _AwardState():
return $default(_that.awarding,_that.accepted,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool awarding,  Bid? accepted,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _AwardState() when $default != null:
return $default(_that.awarding,_that.accepted,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _AwardState extends AwardState {
  const _AwardState({this.awarding = false, this.accepted, this.failure}): super._();
  

/// The award is on its way to the platform.
///
/// **The only "pending" there is**, and deliberately so: `Docs/07` §4 keeps awarding out of the
/// offline queue with bidding and negotiation, because "a stale local decision is worse than an
/// honest 'you are offline'". Nothing durable is written, so either the platform answered or it
/// did not.
@override@JsonKey() final  bool awarding;
/// The offer the platform accepted, once it has.
///
/// **Assigned from the response and from nothing else.** A retry answered `200` with an earlier
/// attempt's award therefore reconciles this screen to the platform's record rather than to
/// what the customer last tapped — which is the case that actually happens, because the second
/// row of the contract's retry table is a phone that lost its connection and generated a fresh
/// key for the same intent.
@override final  Bid? accepted;
/// What the last attempt failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of AwardState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$AwardStateCopyWith<_AwardState> get copyWith => __$AwardStateCopyWithImpl<_AwardState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _AwardState&&(identical(other.awarding, awarding) || other.awarding == awarding)&&(identical(other.accepted, accepted) || other.accepted == accepted)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,awarding,accepted,failure);

@override
String toString() {
  return 'AwardState(awarding: $awarding, accepted: $accepted, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$AwardStateCopyWith<$Res> implements $AwardStateCopyWith<$Res> {
  factory _$AwardStateCopyWith(_AwardState value, $Res Function(_AwardState) _then) = __$AwardStateCopyWithImpl;
@override @useResult
$Res call({
 bool awarding, Bid? accepted, ApiFailure? failure
});


@override $BidCopyWith<$Res>? get accepted;

}
/// @nodoc
class __$AwardStateCopyWithImpl<$Res>
    implements _$AwardStateCopyWith<$Res> {
  __$AwardStateCopyWithImpl(this._self, this._then);

  final _AwardState _self;
  final $Res Function(_AwardState) _then;

/// Create a copy of AwardState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? awarding = null,Object? accepted = freezed,Object? failure = freezed,}) {
  return _then(_AwardState(
awarding: null == awarding ? _self.awarding : awarding // ignore: cast_nullable_to_non_nullable
as bool,accepted: freezed == accepted ? _self.accepted : accepted // ignore: cast_nullable_to_non_nullable
as Bid?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of AwardState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$BidCopyWith<$Res>? get accepted {
    if (_self.accepted == null) {
    return null;
  }

  return $BidCopyWith<$Res>(_self.accepted!, (value) {
    return _then(_self.copyWith(accepted: value));
  });
}
}

// dart format on
