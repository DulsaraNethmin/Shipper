// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'job_detail_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$JobDetailState {

/// The job is being read, or re-read after a failure.
///
/// Deliberately not set by a pull-to-refresh, for the same reason as the list:
/// `RefreshIndicator` draws its own spinner, and replacing the job with a second one would
/// take it away from somebody who pulled precisely to look at it.
 bool get loading;/// An action is in flight. The screen disables its buttons while it is true, which is what
/// stops a second tap becoming a second action.
 bool get acting;/// The job, once it has arrived.
 Job? get job;/// What the last read or action failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of JobDetailState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$JobDetailStateCopyWith<JobDetailState> get copyWith => _$JobDetailStateCopyWithImpl<JobDetailState>(this as JobDetailState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is JobDetailState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.acting, acting) || other.acting == acting)&&(identical(other.job, job) || other.job == job)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,acting,job,failure);

@override
String toString() {
  return 'JobDetailState(loading: $loading, acting: $acting, job: $job, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $JobDetailStateCopyWith<$Res>  {
  factory $JobDetailStateCopyWith(JobDetailState value, $Res Function(JobDetailState) _then) = _$JobDetailStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool acting, Job? job, ApiFailure? failure
});


$JobCopyWith<$Res>? get job;

}
/// @nodoc
class _$JobDetailStateCopyWithImpl<$Res>
    implements $JobDetailStateCopyWith<$Res> {
  _$JobDetailStateCopyWithImpl(this._self, this._then);

  final JobDetailState _self;
  final $Res Function(JobDetailState) _then;

/// Create a copy of JobDetailState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? acting = null,Object? job = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,acting: null == acting ? _self.acting : acting // ignore: cast_nullable_to_non_nullable
as bool,job: freezed == job ? _self.job : job // ignore: cast_nullable_to_non_nullable
as Job?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of JobDetailState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobCopyWith<$Res>? get job {
    if (_self.job == null) {
    return null;
  }

  return $JobCopyWith<$Res>(_self.job!, (value) {
    return _then(_self.copyWith(job: value));
  });
}
}


/// Adds pattern-matching-related methods to [JobDetailState].
extension JobDetailStatePatterns on JobDetailState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _JobDetailState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _JobDetailState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _JobDetailState value)  $default,){
final _that = this;
switch (_that) {
case _JobDetailState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _JobDetailState value)?  $default,){
final _that = this;
switch (_that) {
case _JobDetailState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool acting,  Job? job,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _JobDetailState() when $default != null:
return $default(_that.loading,_that.acting,_that.job,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool acting,  Job? job,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _JobDetailState():
return $default(_that.loading,_that.acting,_that.job,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool acting,  Job? job,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _JobDetailState() when $default != null:
return $default(_that.loading,_that.acting,_that.job,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _JobDetailState extends JobDetailState {
  const _JobDetailState({this.loading = true, this.acting = false, this.job, this.failure}): super._();
  

/// The job is being read, or re-read after a failure.
///
/// Deliberately not set by a pull-to-refresh, for the same reason as the list:
/// `RefreshIndicator` draws its own spinner, and replacing the job with a second one would
/// take it away from somebody who pulled precisely to look at it.
@override@JsonKey() final  bool loading;
/// An action is in flight. The screen disables its buttons while it is true, which is what
/// stops a second tap becoming a second action.
@override@JsonKey() final  bool acting;
/// The job, once it has arrived.
@override final  Job? job;
/// What the last read or action failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of JobDetailState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$JobDetailStateCopyWith<_JobDetailState> get copyWith => __$JobDetailStateCopyWithImpl<_JobDetailState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _JobDetailState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.acting, acting) || other.acting == acting)&&(identical(other.job, job) || other.job == job)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,acting,job,failure);

@override
String toString() {
  return 'JobDetailState(loading: $loading, acting: $acting, job: $job, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$JobDetailStateCopyWith<$Res> implements $JobDetailStateCopyWith<$Res> {
  factory _$JobDetailStateCopyWith(_JobDetailState value, $Res Function(_JobDetailState) _then) = __$JobDetailStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool acting, Job? job, ApiFailure? failure
});


@override $JobCopyWith<$Res>? get job;

}
/// @nodoc
class __$JobDetailStateCopyWithImpl<$Res>
    implements _$JobDetailStateCopyWith<$Res> {
  __$JobDetailStateCopyWithImpl(this._self, this._then);

  final _JobDetailState _self;
  final $Res Function(_JobDetailState) _then;

/// Create a copy of JobDetailState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? acting = null,Object? job = freezed,Object? failure = freezed,}) {
  return _then(_JobDetailState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,acting: null == acting ? _self.acting : acting // ignore: cast_nullable_to_non_nullable
as bool,job: freezed == job ? _self.job : job // ignore: cast_nullable_to_non_nullable
as Job?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of JobDetailState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobCopyWith<$Res>? get job {
    if (_self.job == null) {
    return null;
  }

  return $JobCopyWith<$Res>(_self.job!, (value) {
    return _then(_self.copyWith(job: value));
  });
}
}

// dart format on
