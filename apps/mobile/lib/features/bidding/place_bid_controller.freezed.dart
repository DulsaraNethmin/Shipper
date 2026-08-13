// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'place_bid_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$PlaceBidState {

/// An offer is on its way to the platform.
///
/// **This is the whole of the "pending" state, and it is deliberately small.** `Docs/07` §4
/// keeps bidding out of the offline queue, so there is nothing durable to mark as pending and
/// nothing to reconcile later — either the platform answered or it did not. See the note on
/// [PlaceBidController].
 bool get sending;/// The offer the platform recorded, once it has.
///
/// **Never what was typed.** This is assigned from the response and from nothing else, which is
/// what makes a retry answered `200` with an earlier attempt's offer reconcile the screen to the
/// platform's record rather than to what is in the form.
 Bid? get bid;/// What the last attempt failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of PlaceBidState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$PlaceBidStateCopyWith<PlaceBidState> get copyWith => _$PlaceBidStateCopyWithImpl<PlaceBidState>(this as PlaceBidState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is PlaceBidState&&(identical(other.sending, sending) || other.sending == sending)&&(identical(other.bid, bid) || other.bid == bid)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,sending,bid,failure);

@override
String toString() {
  return 'PlaceBidState(sending: $sending, bid: $bid, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $PlaceBidStateCopyWith<$Res>  {
  factory $PlaceBidStateCopyWith(PlaceBidState value, $Res Function(PlaceBidState) _then) = _$PlaceBidStateCopyWithImpl;
@useResult
$Res call({
 bool sending, Bid? bid, ApiFailure? failure
});


$BidCopyWith<$Res>? get bid;

}
/// @nodoc
class _$PlaceBidStateCopyWithImpl<$Res>
    implements $PlaceBidStateCopyWith<$Res> {
  _$PlaceBidStateCopyWithImpl(this._self, this._then);

  final PlaceBidState _self;
  final $Res Function(PlaceBidState) _then;

/// Create a copy of PlaceBidState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? sending = null,Object? bid = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
sending: null == sending ? _self.sending : sending // ignore: cast_nullable_to_non_nullable
as bool,bid: freezed == bid ? _self.bid : bid // ignore: cast_nullable_to_non_nullable
as Bid?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of PlaceBidState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$BidCopyWith<$Res>? get bid {
    if (_self.bid == null) {
    return null;
  }

  return $BidCopyWith<$Res>(_self.bid!, (value) {
    return _then(_self.copyWith(bid: value));
  });
}
}


/// Adds pattern-matching-related methods to [PlaceBidState].
extension PlaceBidStatePatterns on PlaceBidState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _PlaceBidState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _PlaceBidState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _PlaceBidState value)  $default,){
final _that = this;
switch (_that) {
case _PlaceBidState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _PlaceBidState value)?  $default,){
final _that = this;
switch (_that) {
case _PlaceBidState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool sending,  Bid? bid,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _PlaceBidState() when $default != null:
return $default(_that.sending,_that.bid,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool sending,  Bid? bid,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _PlaceBidState():
return $default(_that.sending,_that.bid,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool sending,  Bid? bid,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _PlaceBidState() when $default != null:
return $default(_that.sending,_that.bid,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _PlaceBidState extends PlaceBidState {
  const _PlaceBidState({this.sending = false, this.bid, this.failure}): super._();
  

/// An offer is on its way to the platform.
///
/// **This is the whole of the "pending" state, and it is deliberately small.** `Docs/07` §4
/// keeps bidding out of the offline queue, so there is nothing durable to mark as pending and
/// nothing to reconcile later — either the platform answered or it did not. See the note on
/// [PlaceBidController].
@override@JsonKey() final  bool sending;
/// The offer the platform recorded, once it has.
///
/// **Never what was typed.** This is assigned from the response and from nothing else, which is
/// what makes a retry answered `200` with an earlier attempt's offer reconcile the screen to the
/// platform's record rather than to what is in the form.
@override final  Bid? bid;
/// What the last attempt failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of PlaceBidState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$PlaceBidStateCopyWith<_PlaceBidState> get copyWith => __$PlaceBidStateCopyWithImpl<_PlaceBidState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _PlaceBidState&&(identical(other.sending, sending) || other.sending == sending)&&(identical(other.bid, bid) || other.bid == bid)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,sending,bid,failure);

@override
String toString() {
  return 'PlaceBidState(sending: $sending, bid: $bid, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$PlaceBidStateCopyWith<$Res> implements $PlaceBidStateCopyWith<$Res> {
  factory _$PlaceBidStateCopyWith(_PlaceBidState value, $Res Function(_PlaceBidState) _then) = __$PlaceBidStateCopyWithImpl;
@override @useResult
$Res call({
 bool sending, Bid? bid, ApiFailure? failure
});


@override $BidCopyWith<$Res>? get bid;

}
/// @nodoc
class __$PlaceBidStateCopyWithImpl<$Res>
    implements _$PlaceBidStateCopyWith<$Res> {
  __$PlaceBidStateCopyWithImpl(this._self, this._then);

  final _PlaceBidState _self;
  final $Res Function(_PlaceBidState) _then;

/// Create a copy of PlaceBidState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? sending = null,Object? bid = freezed,Object? failure = freezed,}) {
  return _then(_PlaceBidState(
sending: null == sending ? _self.sending : sending // ignore: cast_nullable_to_non_nullable
as bool,bid: freezed == bid ? _self.bid : bid // ignore: cast_nullable_to_non_nullable
as Bid?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of PlaceBidState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$BidCopyWith<$Res>? get bid {
    if (_self.bid == null) {
    return null;
  }

  return $BidCopyWith<$Res>(_self.bid!, (value) {
    return _then(_self.copyWith(bid: value));
  });
}
}

// dart format on
