// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'account_deletion_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$AccountDeletionState {

/// The request is on its way to the platform.
 bool get submitting;/// What the platform recorded, once it has answered.
///
/// **Assigned from the response and from nothing else.** A repeat answers `200` with the
/// request that already exists, so a screen fed from this reconciles to the platform's record
/// rather than to what the person last tapped — which matters here more than usual, because a
/// repeat is also how a deferral lifts.
 AccountDeletion? get request;/// What the last attempt failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of AccountDeletionState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$AccountDeletionStateCopyWith<AccountDeletionState> get copyWith => _$AccountDeletionStateCopyWithImpl<AccountDeletionState>(this as AccountDeletionState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is AccountDeletionState&&(identical(other.submitting, submitting) || other.submitting == submitting)&&(identical(other.request, request) || other.request == request)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,submitting,request,failure);

@override
String toString() {
  return 'AccountDeletionState(submitting: $submitting, request: $request, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $AccountDeletionStateCopyWith<$Res>  {
  factory $AccountDeletionStateCopyWith(AccountDeletionState value, $Res Function(AccountDeletionState) _then) = _$AccountDeletionStateCopyWithImpl;
@useResult
$Res call({
 bool submitting, AccountDeletion? request, ApiFailure? failure
});


$AccountDeletionCopyWith<$Res>? get request;

}
/// @nodoc
class _$AccountDeletionStateCopyWithImpl<$Res>
    implements $AccountDeletionStateCopyWith<$Res> {
  _$AccountDeletionStateCopyWithImpl(this._self, this._then);

  final AccountDeletionState _self;
  final $Res Function(AccountDeletionState) _then;

/// Create a copy of AccountDeletionState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? submitting = null,Object? request = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
submitting: null == submitting ? _self.submitting : submitting // ignore: cast_nullable_to_non_nullable
as bool,request: freezed == request ? _self.request : request // ignore: cast_nullable_to_non_nullable
as AccountDeletion?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of AccountDeletionState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$AccountDeletionCopyWith<$Res>? get request {
    if (_self.request == null) {
    return null;
  }

  return $AccountDeletionCopyWith<$Res>(_self.request!, (value) {
    return _then(_self.copyWith(request: value));
  });
}
}


/// Adds pattern-matching-related methods to [AccountDeletionState].
extension AccountDeletionStatePatterns on AccountDeletionState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _AccountDeletionState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _AccountDeletionState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _AccountDeletionState value)  $default,){
final _that = this;
switch (_that) {
case _AccountDeletionState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _AccountDeletionState value)?  $default,){
final _that = this;
switch (_that) {
case _AccountDeletionState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool submitting,  AccountDeletion? request,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _AccountDeletionState() when $default != null:
return $default(_that.submitting,_that.request,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool submitting,  AccountDeletion? request,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _AccountDeletionState():
return $default(_that.submitting,_that.request,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool submitting,  AccountDeletion? request,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _AccountDeletionState() when $default != null:
return $default(_that.submitting,_that.request,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _AccountDeletionState extends AccountDeletionState {
  const _AccountDeletionState({this.submitting = false, this.request, this.failure}): super._();
  

/// The request is on its way to the platform.
@override@JsonKey() final  bool submitting;
/// What the platform recorded, once it has answered.
///
/// **Assigned from the response and from nothing else.** A repeat answers `200` with the
/// request that already exists, so a screen fed from this reconciles to the platform's record
/// rather than to what the person last tapped — which matters here more than usual, because a
/// repeat is also how a deferral lifts.
@override final  AccountDeletion? request;
/// What the last attempt failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of AccountDeletionState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$AccountDeletionStateCopyWith<_AccountDeletionState> get copyWith => __$AccountDeletionStateCopyWithImpl<_AccountDeletionState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _AccountDeletionState&&(identical(other.submitting, submitting) || other.submitting == submitting)&&(identical(other.request, request) || other.request == request)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,submitting,request,failure);

@override
String toString() {
  return 'AccountDeletionState(submitting: $submitting, request: $request, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$AccountDeletionStateCopyWith<$Res> implements $AccountDeletionStateCopyWith<$Res> {
  factory _$AccountDeletionStateCopyWith(_AccountDeletionState value, $Res Function(_AccountDeletionState) _then) = __$AccountDeletionStateCopyWithImpl;
@override @useResult
$Res call({
 bool submitting, AccountDeletion? request, ApiFailure? failure
});


@override $AccountDeletionCopyWith<$Res>? get request;

}
/// @nodoc
class __$AccountDeletionStateCopyWithImpl<$Res>
    implements _$AccountDeletionStateCopyWith<$Res> {
  __$AccountDeletionStateCopyWithImpl(this._self, this._then);

  final _AccountDeletionState _self;
  final $Res Function(_AccountDeletionState) _then;

/// Create a copy of AccountDeletionState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? submitting = null,Object? request = freezed,Object? failure = freezed,}) {
  return _then(_AccountDeletionState(
submitting: null == submitting ? _self.submitting : submitting // ignore: cast_nullable_to_non_nullable
as bool,request: freezed == request ? _self.request : request // ignore: cast_nullable_to_non_nullable
as AccountDeletion?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of AccountDeletionState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$AccountDeletionCopyWith<$Res>? get request {
    if (_self.request == null) {
    return null;
  }

  return $AccountDeletionCopyWith<$Res>(_self.request!, (value) {
    return _then(_self.copyWith(request: value));
  });
}
}

// dart format on
