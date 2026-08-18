// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'app_policy.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$AppPolicy {

/// `Docs/02` §3.1's second rung, in seconds.
///
/// Seconds on the wire because that is what the platform sends — `retry_after_seconds` in
/// `contracts/paths/identity.yaml` is the precedent, and a Go duration string would be an
/// implementation detail of the service. [unsyncedNudgeAfter] is the form the rest of this
/// application uses.
///
/// Defaulted rather than required so a response missing the key degrades to the compiled
/// number rather than to a decode failure, which is the same fail-quiet direction
/// `PlatformFloor.minimumBuild` takes.
@JsonKey(name: 'unsynced_nudge_after_seconds') int get unsyncedNudgeAfterSeconds;/// The size a proof photograph is compressed towards, in bytes.
///
/// **A budget, not the platform's bound.** `STORAGE_MAX_UPLOAD_BYTES` is the bound — the size
/// above which the platform refuses to sign an upload at all — and it is ten times this.
/// `core/capture/captured_image.dart` argues the difference; `internal/config` refuses a deployment that
/// inverts the two.
@JsonKey(name: 'proof_compression_budget_bytes') int get proofCompressionBudgetBytes;
/// Create a copy of AppPolicy
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$AppPolicyCopyWith<AppPolicy> get copyWith => _$AppPolicyCopyWithImpl<AppPolicy>(this as AppPolicy, _$identity);

  /// Serializes this AppPolicy to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is AppPolicy&&(identical(other.unsyncedNudgeAfterSeconds, unsyncedNudgeAfterSeconds) || other.unsyncedNudgeAfterSeconds == unsyncedNudgeAfterSeconds)&&(identical(other.proofCompressionBudgetBytes, proofCompressionBudgetBytes) || other.proofCompressionBudgetBytes == proofCompressionBudgetBytes));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,unsyncedNudgeAfterSeconds,proofCompressionBudgetBytes);

@override
String toString() {
  return 'AppPolicy(unsyncedNudgeAfterSeconds: $unsyncedNudgeAfterSeconds, proofCompressionBudgetBytes: $proofCompressionBudgetBytes)';
}


}

/// @nodoc
abstract mixin class $AppPolicyCopyWith<$Res>  {
  factory $AppPolicyCopyWith(AppPolicy value, $Res Function(AppPolicy) _then) = _$AppPolicyCopyWithImpl;
@useResult
$Res call({
@JsonKey(name: 'unsynced_nudge_after_seconds') int unsyncedNudgeAfterSeconds,@JsonKey(name: 'proof_compression_budget_bytes') int proofCompressionBudgetBytes
});




}
/// @nodoc
class _$AppPolicyCopyWithImpl<$Res>
    implements $AppPolicyCopyWith<$Res> {
  _$AppPolicyCopyWithImpl(this._self, this._then);

  final AppPolicy _self;
  final $Res Function(AppPolicy) _then;

/// Create a copy of AppPolicy
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? unsyncedNudgeAfterSeconds = null,Object? proofCompressionBudgetBytes = null,}) {
  return _then(_self.copyWith(
unsyncedNudgeAfterSeconds: null == unsyncedNudgeAfterSeconds ? _self.unsyncedNudgeAfterSeconds : unsyncedNudgeAfterSeconds // ignore: cast_nullable_to_non_nullable
as int,proofCompressionBudgetBytes: null == proofCompressionBudgetBytes ? _self.proofCompressionBudgetBytes : proofCompressionBudgetBytes // ignore: cast_nullable_to_non_nullable
as int,
  ));
}

}


/// Adds pattern-matching-related methods to [AppPolicy].
extension AppPolicyPatterns on AppPolicy {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _AppPolicy value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _AppPolicy() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _AppPolicy value)  $default,){
final _that = this;
switch (_that) {
case _AppPolicy():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _AppPolicy value)?  $default,){
final _that = this;
switch (_that) {
case _AppPolicy() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function(@JsonKey(name: 'unsynced_nudge_after_seconds')  int unsyncedNudgeAfterSeconds, @JsonKey(name: 'proof_compression_budget_bytes')  int proofCompressionBudgetBytes)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _AppPolicy() when $default != null:
return $default(_that.unsyncedNudgeAfterSeconds,_that.proofCompressionBudgetBytes);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function(@JsonKey(name: 'unsynced_nudge_after_seconds')  int unsyncedNudgeAfterSeconds, @JsonKey(name: 'proof_compression_budget_bytes')  int proofCompressionBudgetBytes)  $default,) {final _that = this;
switch (_that) {
case _AppPolicy():
return $default(_that.unsyncedNudgeAfterSeconds,_that.proofCompressionBudgetBytes);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function(@JsonKey(name: 'unsynced_nudge_after_seconds')  int unsyncedNudgeAfterSeconds, @JsonKey(name: 'proof_compression_budget_bytes')  int proofCompressionBudgetBytes)?  $default,) {final _that = this;
switch (_that) {
case _AppPolicy() when $default != null:
return $default(_that.unsyncedNudgeAfterSeconds,_that.proofCompressionBudgetBytes);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _AppPolicy extends AppPolicy {
  const _AppPolicy({@JsonKey(name: 'unsynced_nudge_after_seconds') this.unsyncedNudgeAfterSeconds = compiledUnsyncedNudgeAfterSeconds, @JsonKey(name: 'proof_compression_budget_bytes') this.proofCompressionBudgetBytes = compiledProofCompressionBudgetBytes}): super._();
  factory _AppPolicy.fromJson(Map<String, dynamic> json) => _$AppPolicyFromJson(json);

/// `Docs/02` §3.1's second rung, in seconds.
///
/// Seconds on the wire because that is what the platform sends — `retry_after_seconds` in
/// `contracts/paths/identity.yaml` is the precedent, and a Go duration string would be an
/// implementation detail of the service. [unsyncedNudgeAfter] is the form the rest of this
/// application uses.
///
/// Defaulted rather than required so a response missing the key degrades to the compiled
/// number rather than to a decode failure, which is the same fail-quiet direction
/// `PlatformFloor.minimumBuild` takes.
@override@JsonKey(name: 'unsynced_nudge_after_seconds') final  int unsyncedNudgeAfterSeconds;
/// The size a proof photograph is compressed towards, in bytes.
///
/// **A budget, not the platform's bound.** `STORAGE_MAX_UPLOAD_BYTES` is the bound — the size
/// above which the platform refuses to sign an upload at all — and it is ten times this.
/// `core/capture/captured_image.dart` argues the difference; `internal/config` refuses a deployment that
/// inverts the two.
@override@JsonKey(name: 'proof_compression_budget_bytes') final  int proofCompressionBudgetBytes;

/// Create a copy of AppPolicy
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$AppPolicyCopyWith<_AppPolicy> get copyWith => __$AppPolicyCopyWithImpl<_AppPolicy>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$AppPolicyToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _AppPolicy&&(identical(other.unsyncedNudgeAfterSeconds, unsyncedNudgeAfterSeconds) || other.unsyncedNudgeAfterSeconds == unsyncedNudgeAfterSeconds)&&(identical(other.proofCompressionBudgetBytes, proofCompressionBudgetBytes) || other.proofCompressionBudgetBytes == proofCompressionBudgetBytes));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,unsyncedNudgeAfterSeconds,proofCompressionBudgetBytes);

@override
String toString() {
  return 'AppPolicy(unsyncedNudgeAfterSeconds: $unsyncedNudgeAfterSeconds, proofCompressionBudgetBytes: $proofCompressionBudgetBytes)';
}


}

/// @nodoc
abstract mixin class _$AppPolicyCopyWith<$Res> implements $AppPolicyCopyWith<$Res> {
  factory _$AppPolicyCopyWith(_AppPolicy value, $Res Function(_AppPolicy) _then) = __$AppPolicyCopyWithImpl;
@override @useResult
$Res call({
@JsonKey(name: 'unsynced_nudge_after_seconds') int unsyncedNudgeAfterSeconds,@JsonKey(name: 'proof_compression_budget_bytes') int proofCompressionBudgetBytes
});




}
/// @nodoc
class __$AppPolicyCopyWithImpl<$Res>
    implements _$AppPolicyCopyWith<$Res> {
  __$AppPolicyCopyWithImpl(this._self, this._then);

  final _AppPolicy _self;
  final $Res Function(_AppPolicy) _then;

/// Create a copy of AppPolicy
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? unsyncedNudgeAfterSeconds = null,Object? proofCompressionBudgetBytes = null,}) {
  return _then(_AppPolicy(
unsyncedNudgeAfterSeconds: null == unsyncedNudgeAfterSeconds ? _self.unsyncedNudgeAfterSeconds : unsyncedNudgeAfterSeconds // ignore: cast_nullable_to_non_nullable
as int,proofCompressionBudgetBytes: null == proofCompressionBudgetBytes ? _self.proofCompressionBudgetBytes : proofCompressionBudgetBytes // ignore: cast_nullable_to_non_nullable
as int,
  ));
}


}

// dart format on
