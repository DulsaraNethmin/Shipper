// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'open_job_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$OpenJobState {

/// The job is being read, or re-read after a failure.
///
/// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
/// replacing the job with a second one would take it away from somebody who pulled precisely to
/// look at it.
 bool get loading;/// The job, once it has arrived.
 OpenJob? get job;/// What the last read failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of OpenJobState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$OpenJobStateCopyWith<OpenJobState> get copyWith => _$OpenJobStateCopyWithImpl<OpenJobState>(this as OpenJobState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is OpenJobState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.job, job) || other.job == job)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,job,failure);

@override
String toString() {
  return 'OpenJobState(loading: $loading, job: $job, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $OpenJobStateCopyWith<$Res>  {
  factory $OpenJobStateCopyWith(OpenJobState value, $Res Function(OpenJobState) _then) = _$OpenJobStateCopyWithImpl;
@useResult
$Res call({
 bool loading, OpenJob? job, ApiFailure? failure
});


$OpenJobCopyWith<$Res>? get job;

}
/// @nodoc
class _$OpenJobStateCopyWithImpl<$Res>
    implements $OpenJobStateCopyWith<$Res> {
  _$OpenJobStateCopyWithImpl(this._self, this._then);

  final OpenJobState _self;
  final $Res Function(OpenJobState) _then;

/// Create a copy of OpenJobState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? job = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,job: freezed == job ? _self.job : job // ignore: cast_nullable_to_non_nullable
as OpenJob?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of OpenJobState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$OpenJobCopyWith<$Res>? get job {
    if (_self.job == null) {
    return null;
  }

  return $OpenJobCopyWith<$Res>(_self.job!, (value) {
    return _then(_self.copyWith(job: value));
  });
}
}


/// Adds pattern-matching-related methods to [OpenJobState].
extension OpenJobStatePatterns on OpenJobState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _OpenJobState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _OpenJobState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _OpenJobState value)  $default,){
final _that = this;
switch (_that) {
case _OpenJobState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _OpenJobState value)?  $default,){
final _that = this;
switch (_that) {
case _OpenJobState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  OpenJob? job,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _OpenJobState() when $default != null:
return $default(_that.loading,_that.job,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  OpenJob? job,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _OpenJobState():
return $default(_that.loading,_that.job,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  OpenJob? job,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _OpenJobState() when $default != null:
return $default(_that.loading,_that.job,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _OpenJobState extends OpenJobState {
  const _OpenJobState({this.loading = true, this.job, this.failure}): super._();
  

/// The job is being read, or re-read after a failure.
///
/// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
/// replacing the job with a second one would take it away from somebody who pulled precisely to
/// look at it.
@override@JsonKey() final  bool loading;
/// The job, once it has arrived.
@override final  OpenJob? job;
/// What the last read failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of OpenJobState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$OpenJobStateCopyWith<_OpenJobState> get copyWith => __$OpenJobStateCopyWithImpl<_OpenJobState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _OpenJobState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.job, job) || other.job == job)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,job,failure);

@override
String toString() {
  return 'OpenJobState(loading: $loading, job: $job, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$OpenJobStateCopyWith<$Res> implements $OpenJobStateCopyWith<$Res> {
  factory _$OpenJobStateCopyWith(_OpenJobState value, $Res Function(_OpenJobState) _then) = __$OpenJobStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, OpenJob? job, ApiFailure? failure
});


@override $OpenJobCopyWith<$Res>? get job;

}
/// @nodoc
class __$OpenJobStateCopyWithImpl<$Res>
    implements _$OpenJobStateCopyWith<$Res> {
  __$OpenJobStateCopyWithImpl(this._self, this._then);

  final _OpenJobState _self;
  final $Res Function(_OpenJobState) _then;

/// Create a copy of OpenJobState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? job = freezed,Object? failure = freezed,}) {
  return _then(_OpenJobState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,job: freezed == job ? _self.job : job // ignore: cast_nullable_to_non_nullable
as OpenJob?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of OpenJobState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$OpenJobCopyWith<$Res>? get job {
    if (_self.job == null) {
    return null;
  }

  return $OpenJobCopyWith<$Res>(_self.job!, (value) {
    return _then(_self.copyWith(job: value));
  });
}
}

// dart format on
