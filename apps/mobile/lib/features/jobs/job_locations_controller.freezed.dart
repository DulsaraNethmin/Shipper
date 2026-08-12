// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'job_locations_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$JobLocationsState {

/// A save is in flight. The screen disables its submit while it is true, which is what stops
/// a second tap becoming a second **draft** — two taps are two actions, and an idempotency
/// key is deliberately per-action (`Docs/07` §4).
 bool get busy;/// The draft as the platform stored it, once it has been saved.
///
/// This is what the screen shows back: the normalised addresses, and whether each one was
/// matched to a place. It is also what makes the next save an **edit** rather than a second
/// draft — see [JobLocationsController.save].
 Job? get draft;/// Whether the customer has asked to change the addresses of a draft already saved.
///
/// The step has two faces — the form, and what the platform made of what was typed — and
/// which one is showing is a fact about the step rather than about the widget. Keeping it
/// here is what lets the sequence be read, and tested, without a screen.
 bool get editing;/// What the last attempt failed with, or `null`. Cleared when the next one starts.
 ApiFailure? get failure;
/// Create a copy of JobLocationsState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$JobLocationsStateCopyWith<JobLocationsState> get copyWith => _$JobLocationsStateCopyWithImpl<JobLocationsState>(this as JobLocationsState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is JobLocationsState&&(identical(other.busy, busy) || other.busy == busy)&&(identical(other.draft, draft) || other.draft == draft)&&(identical(other.editing, editing) || other.editing == editing)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,busy,draft,editing,failure);

@override
String toString() {
  return 'JobLocationsState(busy: $busy, draft: $draft, editing: $editing, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $JobLocationsStateCopyWith<$Res>  {
  factory $JobLocationsStateCopyWith(JobLocationsState value, $Res Function(JobLocationsState) _then) = _$JobLocationsStateCopyWithImpl;
@useResult
$Res call({
 bool busy, Job? draft, bool editing, ApiFailure? failure
});


$JobCopyWith<$Res>? get draft;

}
/// @nodoc
class _$JobLocationsStateCopyWithImpl<$Res>
    implements $JobLocationsStateCopyWith<$Res> {
  _$JobLocationsStateCopyWithImpl(this._self, this._then);

  final JobLocationsState _self;
  final $Res Function(JobLocationsState) _then;

/// Create a copy of JobLocationsState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? busy = null,Object? draft = freezed,Object? editing = null,Object? failure = freezed,}) {
  return _then(_self.copyWith(
busy: null == busy ? _self.busy : busy // ignore: cast_nullable_to_non_nullable
as bool,draft: freezed == draft ? _self.draft : draft // ignore: cast_nullable_to_non_nullable
as Job?,editing: null == editing ? _self.editing : editing // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}
/// Create a copy of JobLocationsState
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


/// Adds pattern-matching-related methods to [JobLocationsState].
extension JobLocationsStatePatterns on JobLocationsState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _JobLocationsState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _JobLocationsState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _JobLocationsState value)  $default,){
final _that = this;
switch (_that) {
case _JobLocationsState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _JobLocationsState value)?  $default,){
final _that = this;
switch (_that) {
case _JobLocationsState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool busy,  Job? draft,  bool editing,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _JobLocationsState() when $default != null:
return $default(_that.busy,_that.draft,_that.editing,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool busy,  Job? draft,  bool editing,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _JobLocationsState():
return $default(_that.busy,_that.draft,_that.editing,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool busy,  Job? draft,  bool editing,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _JobLocationsState() when $default != null:
return $default(_that.busy,_that.draft,_that.editing,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _JobLocationsState extends JobLocationsState {
  const _JobLocationsState({this.busy = false, this.draft, this.editing = false, this.failure}): super._();
  

/// A save is in flight. The screen disables its submit while it is true, which is what stops
/// a second tap becoming a second **draft** — two taps are two actions, and an idempotency
/// key is deliberately per-action (`Docs/07` §4).
@override@JsonKey() final  bool busy;
/// The draft as the platform stored it, once it has been saved.
///
/// This is what the screen shows back: the normalised addresses, and whether each one was
/// matched to a place. It is also what makes the next save an **edit** rather than a second
/// draft — see [JobLocationsController.save].
@override final  Job? draft;
/// Whether the customer has asked to change the addresses of a draft already saved.
///
/// The step has two faces — the form, and what the platform made of what was typed — and
/// which one is showing is a fact about the step rather than about the widget. Keeping it
/// here is what lets the sequence be read, and tested, without a screen.
@override@JsonKey() final  bool editing;
/// What the last attempt failed with, or `null`. Cleared when the next one starts.
@override final  ApiFailure? failure;

/// Create a copy of JobLocationsState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$JobLocationsStateCopyWith<_JobLocationsState> get copyWith => __$JobLocationsStateCopyWithImpl<_JobLocationsState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _JobLocationsState&&(identical(other.busy, busy) || other.busy == busy)&&(identical(other.draft, draft) || other.draft == draft)&&(identical(other.editing, editing) || other.editing == editing)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,busy,draft,editing,failure);

@override
String toString() {
  return 'JobLocationsState(busy: $busy, draft: $draft, editing: $editing, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$JobLocationsStateCopyWith<$Res> implements $JobLocationsStateCopyWith<$Res> {
  factory _$JobLocationsStateCopyWith(_JobLocationsState value, $Res Function(_JobLocationsState) _then) = __$JobLocationsStateCopyWithImpl;
@override @useResult
$Res call({
 bool busy, Job? draft, bool editing, ApiFailure? failure
});


@override $JobCopyWith<$Res>? get draft;

}
/// @nodoc
class __$JobLocationsStateCopyWithImpl<$Res>
    implements _$JobLocationsStateCopyWith<$Res> {
  __$JobLocationsStateCopyWithImpl(this._self, this._then);

  final _JobLocationsState _self;
  final $Res Function(_JobLocationsState) _then;

/// Create a copy of JobLocationsState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? busy = null,Object? draft = freezed,Object? editing = null,Object? failure = freezed,}) {
  return _then(_JobLocationsState(
busy: null == busy ? _self.busy : busy // ignore: cast_nullable_to_non_nullable
as bool,draft: freezed == draft ? _self.draft : draft // ignore: cast_nullable_to_non_nullable
as Job?,editing: null == editing ? _self.editing : editing // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

/// Create a copy of JobLocationsState
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
