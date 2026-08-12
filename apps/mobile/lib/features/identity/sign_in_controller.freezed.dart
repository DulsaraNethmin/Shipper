// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'sign_in_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$SignInState {

/// A request is in flight. The screen disables its submit while it is true, which is what
/// stops a second tap becoming a second **device session** — two taps are two actions, and
/// an idempotency key is deliberately per-action (`Docs/07` §4).
 bool get busy;/// What the last attempt failed with, or `null`. Cleared when the next one starts.
 ApiFailure? get failure;
/// Create a copy of SignInState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$SignInStateCopyWith<SignInState> get copyWith => _$SignInStateCopyWithImpl<SignInState>(this as SignInState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is SignInState&&(identical(other.busy, busy) || other.busy == busy)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,busy,failure);

@override
String toString() {
  return 'SignInState(busy: $busy, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $SignInStateCopyWith<$Res>  {
  factory $SignInStateCopyWith(SignInState value, $Res Function(SignInState) _then) = _$SignInStateCopyWithImpl;
@useResult
$Res call({
 bool busy, ApiFailure? failure
});




}
/// @nodoc
class _$SignInStateCopyWithImpl<$Res>
    implements $SignInStateCopyWith<$Res> {
  _$SignInStateCopyWithImpl(this._self, this._then);

  final SignInState _self;
  final $Res Function(SignInState) _then;

/// Create a copy of SignInState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? busy = null,Object? failure = freezed,}) {
  return _then(_self.copyWith(
busy: null == busy ? _self.busy : busy // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

}


/// Adds pattern-matching-related methods to [SignInState].
extension SignInStatePatterns on SignInState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _SignInState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _SignInState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _SignInState value)  $default,){
final _that = this;
switch (_that) {
case _SignInState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _SignInState value)?  $default,){
final _that = this;
switch (_that) {
case _SignInState() when $default != null:
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
case _SignInState() when $default != null:
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
case _SignInState():
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
case _SignInState() when $default != null:
return $default(_that.busy,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _SignInState implements SignInState {
  const _SignInState({this.busy = false, this.failure});
  

/// A request is in flight. The screen disables its submit while it is true, which is what
/// stops a second tap becoming a second **device session** — two taps are two actions, and
/// an idempotency key is deliberately per-action (`Docs/07` §4).
@override@JsonKey() final  bool busy;
/// What the last attempt failed with, or `null`. Cleared when the next one starts.
@override final  ApiFailure? failure;

/// Create a copy of SignInState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$SignInStateCopyWith<_SignInState> get copyWith => __$SignInStateCopyWithImpl<_SignInState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _SignInState&&(identical(other.busy, busy) || other.busy == busy)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,busy,failure);

@override
String toString() {
  return 'SignInState(busy: $busy, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$SignInStateCopyWith<$Res> implements $SignInStateCopyWith<$Res> {
  factory _$SignInStateCopyWith(_SignInState value, $Res Function(_SignInState) _then) = __$SignInStateCopyWithImpl;
@override @useResult
$Res call({
 bool busy, ApiFailure? failure
});




}
/// @nodoc
class __$SignInStateCopyWithImpl<$Res>
    implements _$SignInStateCopyWith<$Res> {
  __$SignInStateCopyWithImpl(this._self, this._then);

  final _SignInState _self;
  final $Res Function(_SignInState) _then;

/// Create a copy of SignInState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? busy = null,Object? failure = freezed,}) {
  return _then(_SignInState(
busy: null == busy ? _self.busy : busy // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}


}

// dart format on
