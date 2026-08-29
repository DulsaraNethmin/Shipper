// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'job_draft_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$JobDraftState {

/// The draft is being read. True from construction, because the read starts there.
 bool get loading;/// A save is in flight. Every step disables its submit while it is true, which is what stops
/// a second tap becoming a second `PATCH` — two taps are two actions, and an idempotency key
/// is deliberately per-action (`Docs/07` §4).
 bool get saving;/// The draft as the platform holds it, once it has arrived.
 Job? get draft;/// What the last read or save failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of JobDraftState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$JobDraftStateCopyWith<JobDraftState> get copyWith => _$JobDraftStateCopyWithImpl<JobDraftState>(this as JobDraftState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is JobDraftState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.saving, saving) || other.saving == saving)&&(identical(other.draft, draft) || other.draft == draft)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,saving,draft,failure);

@override
String toString() {
  return 'JobDraftState(loading: $loading, saving: $saving, draft: $draft, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $JobDraftStateCopyWith<$Res>  {
  factory $JobDraftStateCopyWith(JobDraftState value, $Res Function(JobDraftState) _then) = _$JobDraftStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool saving, Job? draft, ApiFailure? failure
});


$JobCopyWith<$Res>? get draft;

}
/// @nodoc
class _$JobDraftStateCopyWithImpl<$Res>
    implements $JobDraftStateCopyWith<$Res> {
  _$JobDraftStateCopyWithImpl(this._self, this._then);

  final JobDraftState _self;
  final $Res Function(JobDraftState) _then;

/// Create a copy of JobDraftState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? saving = null,Object? draft = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,saving: null == saving ? _self.saving : saving // ignore: cast_nullable_to_non_nullable
as bool,draft: freezed == draft ? _self.draft : draft // ignore: cast_nullable_to_non_nullable
as Job?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of JobDraftState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobCopyWith<$Res>? get draft {
    if (_self.draft == null) {
    return null;
  }

  return $JobCopyWith<$Res>(_self.draft!, (value) {
    return _then(_self.copyWith(draft: value));
  });
}
}


/// Adds pattern-matching-related methods to [JobDraftState].
extension JobDraftStatePatterns on JobDraftState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _JobDraftState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _JobDraftState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _JobDraftState value)  $default,){
final _that = this;
switch (_that) {
case _JobDraftState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _JobDraftState value)?  $default,){
final _that = this;
switch (_that) {
case _JobDraftState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool saving,  Job? draft,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _JobDraftState() when $default != null:
return $default(_that.loading,_that.saving,_that.draft,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool saving,  Job? draft,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _JobDraftState():
return $default(_that.loading,_that.saving,_that.draft,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool saving,  Job? draft,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _JobDraftState() when $default != null:
return $default(_that.loading,_that.saving,_that.draft,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _JobDraftState extends JobDraftState {
  const _JobDraftState({this.loading = true, this.saving = false, this.draft, this.failure}): super._();
  

/// The draft is being read. True from construction, because the read starts there.
@override@JsonKey() final  bool loading;
/// A save is in flight. Every step disables its submit while it is true, which is what stops
/// a second tap becoming a second `PATCH` — two taps are two actions, and an idempotency key
/// is deliberately per-action (`Docs/07` §4).
@override@JsonKey() final  bool saving;
/// The draft as the platform holds it, once it has arrived.
@override final  Job? draft;
/// What the last read or save failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of JobDraftState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$JobDraftStateCopyWith<_JobDraftState> get copyWith => __$JobDraftStateCopyWithImpl<_JobDraftState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _JobDraftState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.saving, saving) || other.saving == saving)&&(identical(other.draft, draft) || other.draft == draft)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,saving,draft,failure);

@override
String toString() {
  return 'JobDraftState(loading: $loading, saving: $saving, draft: $draft, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$JobDraftStateCopyWith<$Res> implements $JobDraftStateCopyWith<$Res> {
  factory _$JobDraftStateCopyWith(_JobDraftState value, $Res Function(_JobDraftState) _then) = __$JobDraftStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool saving, Job? draft, ApiFailure? failure
});


@override $JobCopyWith<$Res>? get draft;

}
/// @nodoc
class __$JobDraftStateCopyWithImpl<$Res>
    implements _$JobDraftStateCopyWith<$Res> {
  __$JobDraftStateCopyWithImpl(this._self, this._then);

  final _JobDraftState _self;
  final $Res Function(_JobDraftState) _then;

/// Create a copy of JobDraftState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? saving = null,Object? draft = freezed,Object? failure = freezed,}) {
  return _then(_JobDraftState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,saving: null == saving ? _self.saving : saving // ignore: cast_nullable_to_non_nullable
as bool,draft: freezed == draft ? _self.draft : draft // ignore: cast_nullable_to_non_nullable
as Job?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of JobDraftState
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$JobCopyWith<$Res>? get draft {
    if (_self.draft == null) {
    return null;
  }

  return $JobCopyWith<$Res>(_self.draft!, (value) {
    return _then(_self.copyWith(draft: value));
  });
}
}

// dart format on
