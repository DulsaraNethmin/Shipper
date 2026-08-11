// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'signup_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$SignupState {

/// Chosen on the role screen (SHIP-52) and sent to `POST /v1/auth/register`.
///
/// Defaults to customer, which is a **form default and not a decision** — the platform fixes
/// the role from what the request carries and makes it immutable afterwards (SHIP-45).
 UserRole get role;/// The account, once the platform has created it. Replaced by each verification response,
/// so `emailVerified` and `phoneVerified` are always the platform's answer and never the
/// client's belief.
 Account? get account;/// A request is in flight. The screens disable their submit while it is true, which stops a
/// second tap starting a second action — the case a shared idempotency key would not cover,
/// because the two taps would be two actions.
 bool get busy;/// What the last request failed with, or `null`. Cleared when the next one starts.
 ApiFailure? get failure;
/// Create a copy of SignupState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$SignupStateCopyWith<SignupState> get copyWith => _$SignupStateCopyWithImpl<SignupState>(this as SignupState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is SignupState&&(identical(other.role, role) || other.role == role)&&(identical(other.account, account) || other.account == account)&&(identical(other.busy, busy) || other.busy == busy)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,role,account,busy,failure);

@override
String toString() {
  return 'SignupState(role: $role, account: $account, busy: $busy, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $SignupStateCopyWith<$Res>  {
  factory $SignupStateCopyWith(SignupState value, $Res Function(SignupState) _then) = _$SignupStateCopyWithImpl;
@useResult
$Res call({
 UserRole role, Account? account, bool busy, ApiFailure? failure
});


$AccountCopyWith<$Res>? get account;

}
/// @nodoc
class _$SignupStateCopyWithImpl<$Res>
    implements $SignupStateCopyWith<$Res> {
  _$SignupStateCopyWithImpl(this._self, this._then);

  final SignupState _self;
  final $Res Function(SignupState) _then;

/// Create a copy of SignupState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? role = null,Object? account = freezed,Object? busy = null,Object? failure = freezed,}) {
  return _then(_self.copyWith(
role: null == role ? _self.role : role // ignore: cast_nullable_to_non_nullable
as UserRole,account: freezed == account ? _self.account : account // ignore: cast_nullable_to_non_nullable
as Account?,busy: null == busy ? _self.busy : busy // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of SignupState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$AccountCopyWith<$Res>? get account {
    if (_self.account == null) {
    return null;
  }

  return $AccountCopyWith<$Res>(_self.account!, (value) {
    return _then(_self.copyWith(account: value));
  });
}
}


/// Adds pattern-matching-related methods to [SignupState].
extension SignupStatePatterns on SignupState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _SignupState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _SignupState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _SignupState value)  $default,){
final _that = this;
switch (_that) {
case _SignupState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _SignupState value)?  $default,){
final _that = this;
switch (_that) {
case _SignupState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( UserRole role,  Account? account,  bool busy,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _SignupState() when $default != null:
return $default(_that.role,_that.account,_that.busy,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( UserRole role,  Account? account,  bool busy,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _SignupState():
return $default(_that.role,_that.account,_that.busy,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( UserRole role,  Account? account,  bool busy,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _SignupState() when $default != null:
return $default(_that.role,_that.account,_that.busy,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _SignupState extends SignupState {
  const _SignupState({this.role = UserRole.customer, this.account, this.busy = false, this.failure}): super._();
  

/// Chosen on the role screen (SHIP-52) and sent to `POST /v1/auth/register`.
///
/// Defaults to customer, which is a **form default and not a decision** — the platform fixes
/// the role from what the request carries and makes it immutable afterwards (SHIP-45).
@override@JsonKey() final  UserRole role;
/// The account, once the platform has created it. Replaced by each verification response,
/// so `emailVerified` and `phoneVerified` are always the platform's answer and never the
/// client's belief.
@override final  Account? account;
/// A request is in flight. The screens disable their submit while it is true, which stops a
/// second tap starting a second action — the case a shared idempotency key would not cover,
/// because the two taps would be two actions.
@override@JsonKey() final  bool busy;
/// What the last request failed with, or `null`. Cleared when the next one starts.
@override final  ApiFailure? failure;

/// Create a copy of SignupState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$SignupStateCopyWith<_SignupState> get copyWith => __$SignupStateCopyWithImpl<_SignupState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _SignupState&&(identical(other.role, role) || other.role == role)&&(identical(other.account, account) || other.account == account)&&(identical(other.busy, busy) || other.busy == busy)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,role,account,busy,failure);

@override
String toString() {
  return 'SignupState(role: $role, account: $account, busy: $busy, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$SignupStateCopyWith<$Res> implements $SignupStateCopyWith<$Res> {
  factory _$SignupStateCopyWith(_SignupState value, $Res Function(_SignupState) _then) = __$SignupStateCopyWithImpl;
@override @useResult
$Res call({
 UserRole role, Account? account, bool busy, ApiFailure? failure
});


@override $AccountCopyWith<$Res>? get account;

}
/// @nodoc
class __$SignupStateCopyWithImpl<$Res>
    implements _$SignupStateCopyWith<$Res> {
  __$SignupStateCopyWithImpl(this._self, this._then);

  final _SignupState _self;
  final $Res Function(_SignupState) _then;

/// Create a copy of SignupState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? role = null,Object? account = freezed,Object? busy = null,Object? failure = freezed,}) {
  return _then(_SignupState(
role: null == role ? _self.role : role // ignore: cast_nullable_to_non_nullable
as UserRole,account: freezed == account ? _self.account : account // ignore: cast_nullable_to_non_nullable
as Account?,busy: null == busy ? _self.busy : busy // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of SignupState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$AccountCopyWith<$Res>? get account {
    if (_self.account == null) {
    return null;
  }

  return $AccountCopyWith<$Res>(_self.account!, (value) {
    return _then(_self.copyWith(account: value));
  });
}
}

// dart format on
