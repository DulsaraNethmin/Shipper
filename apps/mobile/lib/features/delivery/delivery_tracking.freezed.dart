// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'delivery_tracking.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$RecordedMilestone {

 String get id;@JsonKey(name: 'job_id') String get jobId;/// The milestone, in the platform's wire vocabulary — one of `Docs/01` §4.4's five.
///
/// **A `String` rather than the `Milestone` enumeration**, which is four values and is *the
/// milestones this app can record*. `driver_assigned` is served here and is not one of them, and
/// a sixth could arrive without a store release. `milestoneLabel` is what names it.
 String get milestone;/// What kind of actor recorded it.
@JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown) MilestoneActor? get recordedBy;/// What a person should know that the milestone itself does not say — "nobody at the gate,
/// returning at four". Present only when one was given.
 String? get reason;/// When the actor says they acted, in UTC. **This is what a customer is shown.**
@JsonKey(name: 'recorded_at') String? get recordedAt;/// When the platform received it, in UTC, on the platform's own clock.
@JsonKey(name: 'accepted_at') String? get acceptedAt;
/// Create a copy of RecordedMilestone
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$RecordedMilestoneCopyWith<RecordedMilestone> get copyWith => _$RecordedMilestoneCopyWithImpl<RecordedMilestone>(this as RecordedMilestone, _$identity);

  /// Serializes this RecordedMilestone to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is RecordedMilestone&&(identical(other.id, id) || other.id == id)&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.milestone, milestone) || other.milestone == milestone)&&(identical(other.recordedBy, recordedBy) || other.recordedBy == recordedBy)&&(identical(other.reason, reason) || other.reason == reason)&&(identical(other.recordedAt, recordedAt) || other.recordedAt == recordedAt)&&(identical(other.acceptedAt, acceptedAt) || other.acceptedAt == acceptedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,jobId,milestone,recordedBy,reason,recordedAt,acceptedAt);

@override
String toString() {
  return 'RecordedMilestone(id: $id, jobId: $jobId, milestone: $milestone, recordedBy: $recordedBy, reason: $reason, recordedAt: $recordedAt, acceptedAt: $acceptedAt)';
}


}

/// @nodoc
abstract mixin class $RecordedMilestoneCopyWith<$Res>  {
  factory $RecordedMilestoneCopyWith(RecordedMilestone value, $Res Function(RecordedMilestone) _then) = _$RecordedMilestoneCopyWithImpl;
@useResult
$Res call({
 String id,@JsonKey(name: 'job_id') String jobId, String milestone,@JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown) MilestoneActor? recordedBy, String? reason,@JsonKey(name: 'recorded_at') String? recordedAt,@JsonKey(name: 'accepted_at') String? acceptedAt
});




}
/// @nodoc
class _$RecordedMilestoneCopyWithImpl<$Res>
    implements $RecordedMilestoneCopyWith<$Res> {
  _$RecordedMilestoneCopyWithImpl(this._self, this._then);

  final RecordedMilestone _self;
  final $Res Function(RecordedMilestone) _then;

/// Create a copy of RecordedMilestone
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? jobId = null,Object? milestone = null,Object? recordedBy = freezed,Object? reason = freezed,Object? recordedAt = freezed,Object? acceptedAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,milestone: null == milestone ? _self.milestone : milestone // ignore: cast_nullable_to_non_nullable
as String,recordedBy: freezed == recordedBy ? _self.recordedBy : recordedBy // ignore: cast_nullable_to_non_nullable
as MilestoneActor?,reason: freezed == reason ? _self.reason : reason // ignore: cast_nullable_to_non_nullable
as String?,recordedAt: freezed == recordedAt ? _self.recordedAt : recordedAt // ignore: cast_nullable_to_non_nullable
as String?,acceptedAt: freezed == acceptedAt ? _self.acceptedAt : acceptedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [RecordedMilestone].
extension RecordedMilestonePatterns on RecordedMilestone {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _RecordedMilestone value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _RecordedMilestone() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _RecordedMilestone value)  $default,){
final _that = this;
switch (_that) {
case _RecordedMilestone():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _RecordedMilestone value)?  $default,){
final _that = this;
switch (_that) {
case _RecordedMilestone() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'job_id')  String jobId,  String milestone, @JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown)  MilestoneActor? recordedBy,  String? reason, @JsonKey(name: 'recorded_at')  String? recordedAt, @JsonKey(name: 'accepted_at')  String? acceptedAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _RecordedMilestone() when $default != null:
return $default(_that.id,_that.jobId,_that.milestone,_that.recordedBy,_that.reason,_that.recordedAt,_that.acceptedAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'job_id')  String jobId,  String milestone, @JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown)  MilestoneActor? recordedBy,  String? reason, @JsonKey(name: 'recorded_at')  String? recordedAt, @JsonKey(name: 'accepted_at')  String? acceptedAt)  $default,) {final _that = this;
switch (_that) {
case _RecordedMilestone():
return $default(_that.id,_that.jobId,_that.milestone,_that.recordedBy,_that.reason,_that.recordedAt,_that.acceptedAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id, @JsonKey(name: 'job_id')  String jobId,  String milestone, @JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown)  MilestoneActor? recordedBy,  String? reason, @JsonKey(name: 'recorded_at')  String? recordedAt, @JsonKey(name: 'accepted_at')  String? acceptedAt)?  $default,) {final _that = this;
switch (_that) {
case _RecordedMilestone() when $default != null:
return $default(_that.id,_that.jobId,_that.milestone,_that.recordedBy,_that.reason,_that.recordedAt,_that.acceptedAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _RecordedMilestone extends RecordedMilestone {
  const _RecordedMilestone({required this.id, @JsonKey(name: 'job_id') required this.jobId, required this.milestone, @JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown) this.recordedBy, this.reason, @JsonKey(name: 'recorded_at') this.recordedAt, @JsonKey(name: 'accepted_at') this.acceptedAt}): super._();
  factory _RecordedMilestone.fromJson(Map<String, dynamic> json) => _$RecordedMilestoneFromJson(json);

@override final  String id;
@override@JsonKey(name: 'job_id') final  String jobId;
/// The milestone, in the platform's wire vocabulary — one of `Docs/01` §4.4's five.
///
/// **A `String` rather than the `Milestone` enumeration**, which is four values and is *the
/// milestones this app can record*. `driver_assigned` is served here and is not one of them, and
/// a sixth could arrive without a store release. `milestoneLabel` is what names it.
@override final  String milestone;
/// What kind of actor recorded it.
@override@JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown) final  MilestoneActor? recordedBy;
/// What a person should know that the milestone itself does not say — "nobody at the gate,
/// returning at four". Present only when one was given.
@override final  String? reason;
/// When the actor says they acted, in UTC. **This is what a customer is shown.**
@override@JsonKey(name: 'recorded_at') final  String? recordedAt;
/// When the platform received it, in UTC, on the platform's own clock.
@override@JsonKey(name: 'accepted_at') final  String? acceptedAt;

/// Create a copy of RecordedMilestone
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$RecordedMilestoneCopyWith<_RecordedMilestone> get copyWith => __$RecordedMilestoneCopyWithImpl<_RecordedMilestone>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$RecordedMilestoneToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _RecordedMilestone&&(identical(other.id, id) || other.id == id)&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.milestone, milestone) || other.milestone == milestone)&&(identical(other.recordedBy, recordedBy) || other.recordedBy == recordedBy)&&(identical(other.reason, reason) || other.reason == reason)&&(identical(other.recordedAt, recordedAt) || other.recordedAt == recordedAt)&&(identical(other.acceptedAt, acceptedAt) || other.acceptedAt == acceptedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,jobId,milestone,recordedBy,reason,recordedAt,acceptedAt);

@override
String toString() {
  return 'RecordedMilestone(id: $id, jobId: $jobId, milestone: $milestone, recordedBy: $recordedBy, reason: $reason, recordedAt: $recordedAt, acceptedAt: $acceptedAt)';
}


}

/// @nodoc
abstract mixin class _$RecordedMilestoneCopyWith<$Res> implements $RecordedMilestoneCopyWith<$Res> {
  factory _$RecordedMilestoneCopyWith(_RecordedMilestone value, $Res Function(_RecordedMilestone) _then) = __$RecordedMilestoneCopyWithImpl;
@override @useResult
$Res call({
 String id,@JsonKey(name: 'job_id') String jobId, String milestone,@JsonKey(name: 'recorded_by', unknownEnumValue: MilestoneActor.unknown) MilestoneActor? recordedBy, String? reason,@JsonKey(name: 'recorded_at') String? recordedAt,@JsonKey(name: 'accepted_at') String? acceptedAt
});




}
/// @nodoc
class __$RecordedMilestoneCopyWithImpl<$Res>
    implements _$RecordedMilestoneCopyWith<$Res> {
  __$RecordedMilestoneCopyWithImpl(this._self, this._then);

  final _RecordedMilestone _self;
  final $Res Function(_RecordedMilestone) _then;

/// Create a copy of RecordedMilestone
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? jobId = null,Object? milestone = null,Object? recordedBy = freezed,Object? reason = freezed,Object? recordedAt = freezed,Object? acceptedAt = freezed,}) {
  return _then(_RecordedMilestone(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,milestone: null == milestone ? _self.milestone : milestone // ignore: cast_nullable_to_non_nullable
as String,recordedBy: freezed == recordedBy ? _self.recordedBy : recordedBy // ignore: cast_nullable_to_non_nullable
as MilestoneActor?,reason: freezed == reason ? _self.reason : reason // ignore: cast_nullable_to_non_nullable
as String?,recordedAt: freezed == recordedAt ? _self.recordedAt : recordedAt // ignore: cast_nullable_to_non_nullable
as String?,acceptedAt: freezed == acceptedAt ? _self.acceptedAt : acceptedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}


/// @nodoc
mixin _$DeliveryProof {

 String get id;@JsonKey(name: 'job_id') String get jobId;/// The milestone this evidence is for. On this job by construction.
@JsonKey(name: 'milestone_id') String? get milestoneId;/// Which milestone was recorded, in the wire vocabulary.
 String get milestone;/// A signed URL at the object store. **A credential** — see the note on this class.
@JsonKey(name: 'download_url') String? get downloadUrl;/// When [downloadUrl] stops working.
///
/// **Read rather than assumed**: it is configuration and may be shortened without notice, which
/// is why nothing in this client compiles in a lifetime.
@JsonKey(name: 'download_expires_at') String? get downloadExpiresAt;/// Why this milestone has no photograph, and absent when it has one.
@JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown) ProofExceptionReason? get exceptionReason;/// The **actor's** clock, carried through from the milestone. What to show a customer.
@JsonKey(name: 'recorded_at') String? get recordedAt;/// When the platform recorded it. What support reasons about.
@JsonKey(name: 'accepted_at') String? get acceptedAt;
/// Create a copy of DeliveryProof
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$DeliveryProofCopyWith<DeliveryProof> get copyWith => _$DeliveryProofCopyWithImpl<DeliveryProof>(this as DeliveryProof, _$identity);

  /// Serializes this DeliveryProof to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is DeliveryProof&&(identical(other.id, id) || other.id == id)&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.milestoneId, milestoneId) || other.milestoneId == milestoneId)&&(identical(other.milestone, milestone) || other.milestone == milestone)&&(identical(other.downloadUrl, downloadUrl) || other.downloadUrl == downloadUrl)&&(identical(other.downloadExpiresAt, downloadExpiresAt) || other.downloadExpiresAt == downloadExpiresAt)&&(identical(other.exceptionReason, exceptionReason) || other.exceptionReason == exceptionReason)&&(identical(other.recordedAt, recordedAt) || other.recordedAt == recordedAt)&&(identical(other.acceptedAt, acceptedAt) || other.acceptedAt == acceptedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,jobId,milestoneId,milestone,downloadUrl,downloadExpiresAt,exceptionReason,recordedAt,acceptedAt);

@override
String toString() {
  return 'DeliveryProof(id: $id, jobId: $jobId, milestoneId: $milestoneId, milestone: $milestone, downloadUrl: $downloadUrl, downloadExpiresAt: $downloadExpiresAt, exceptionReason: $exceptionReason, recordedAt: $recordedAt, acceptedAt: $acceptedAt)';
}


}

/// @nodoc
abstract mixin class $DeliveryProofCopyWith<$Res>  {
  factory $DeliveryProofCopyWith(DeliveryProof value, $Res Function(DeliveryProof) _then) = _$DeliveryProofCopyWithImpl;
@useResult
$Res call({
 String id,@JsonKey(name: 'job_id') String jobId,@JsonKey(name: 'milestone_id') String? milestoneId, String milestone,@JsonKey(name: 'download_url') String? downloadUrl,@JsonKey(name: 'download_expires_at') String? downloadExpiresAt,@JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown) ProofExceptionReason? exceptionReason,@JsonKey(name: 'recorded_at') String? recordedAt,@JsonKey(name: 'accepted_at') String? acceptedAt
});




}
/// @nodoc
class _$DeliveryProofCopyWithImpl<$Res>
    implements $DeliveryProofCopyWith<$Res> {
  _$DeliveryProofCopyWithImpl(this._self, this._then);

  final DeliveryProof _self;
  final $Res Function(DeliveryProof) _then;

/// Create a copy of DeliveryProof
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? jobId = null,Object? milestoneId = freezed,Object? milestone = null,Object? downloadUrl = freezed,Object? downloadExpiresAt = freezed,Object? exceptionReason = freezed,Object? recordedAt = freezed,Object? acceptedAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,milestoneId: freezed == milestoneId ? _self.milestoneId : milestoneId // ignore: cast_nullable_to_non_nullable
as String?,milestone: null == milestone ? _self.milestone : milestone // ignore: cast_nullable_to_non_nullable
as String,downloadUrl: freezed == downloadUrl ? _self.downloadUrl : downloadUrl // ignore: cast_nullable_to_non_nullable
as String?,downloadExpiresAt: freezed == downloadExpiresAt ? _self.downloadExpiresAt : downloadExpiresAt // ignore: cast_nullable_to_non_nullable
as String?,exceptionReason: freezed == exceptionReason ? _self.exceptionReason : exceptionReason // ignore: cast_nullable_to_non_nullable
as ProofExceptionReason?,recordedAt: freezed == recordedAt ? _self.recordedAt : recordedAt // ignore: cast_nullable_to_non_nullable
as String?,acceptedAt: freezed == acceptedAt ? _self.acceptedAt : acceptedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [DeliveryProof].
extension DeliveryProofPatterns on DeliveryProof {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _DeliveryProof value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _DeliveryProof() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _DeliveryProof value)  $default,){
final _that = this;
switch (_that) {
case _DeliveryProof():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _DeliveryProof value)?  $default,){
final _that = this;
switch (_that) {
case _DeliveryProof() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(name: 'milestone_id')  String? milestoneId,  String milestone, @JsonKey(name: 'download_url')  String? downloadUrl, @JsonKey(name: 'download_expires_at')  String? downloadExpiresAt, @JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown)  ProofExceptionReason? exceptionReason, @JsonKey(name: 'recorded_at')  String? recordedAt, @JsonKey(name: 'accepted_at')  String? acceptedAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _DeliveryProof() when $default != null:
return $default(_that.id,_that.jobId,_that.milestoneId,_that.milestone,_that.downloadUrl,_that.downloadExpiresAt,_that.exceptionReason,_that.recordedAt,_that.acceptedAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(name: 'milestone_id')  String? milestoneId,  String milestone, @JsonKey(name: 'download_url')  String? downloadUrl, @JsonKey(name: 'download_expires_at')  String? downloadExpiresAt, @JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown)  ProofExceptionReason? exceptionReason, @JsonKey(name: 'recorded_at')  String? recordedAt, @JsonKey(name: 'accepted_at')  String? acceptedAt)  $default,) {final _that = this;
switch (_that) {
case _DeliveryProof():
return $default(_that.id,_that.jobId,_that.milestoneId,_that.milestone,_that.downloadUrl,_that.downloadExpiresAt,_that.exceptionReason,_that.recordedAt,_that.acceptedAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(name: 'milestone_id')  String? milestoneId,  String milestone, @JsonKey(name: 'download_url')  String? downloadUrl, @JsonKey(name: 'download_expires_at')  String? downloadExpiresAt, @JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown)  ProofExceptionReason? exceptionReason, @JsonKey(name: 'recorded_at')  String? recordedAt, @JsonKey(name: 'accepted_at')  String? acceptedAt)?  $default,) {final _that = this;
switch (_that) {
case _DeliveryProof() when $default != null:
return $default(_that.id,_that.jobId,_that.milestoneId,_that.milestone,_that.downloadUrl,_that.downloadExpiresAt,_that.exceptionReason,_that.recordedAt,_that.acceptedAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _DeliveryProof extends DeliveryProof {
  const _DeliveryProof({required this.id, @JsonKey(name: 'job_id') required this.jobId, @JsonKey(name: 'milestone_id') this.milestoneId, required this.milestone, @JsonKey(name: 'download_url') this.downloadUrl, @JsonKey(name: 'download_expires_at') this.downloadExpiresAt, @JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown) this.exceptionReason, @JsonKey(name: 'recorded_at') this.recordedAt, @JsonKey(name: 'accepted_at') this.acceptedAt}): super._();
  factory _DeliveryProof.fromJson(Map<String, dynamic> json) => _$DeliveryProofFromJson(json);

@override final  String id;
@override@JsonKey(name: 'job_id') final  String jobId;
/// The milestone this evidence is for. On this job by construction.
@override@JsonKey(name: 'milestone_id') final  String? milestoneId;
/// Which milestone was recorded, in the wire vocabulary.
@override final  String milestone;
/// A signed URL at the object store. **A credential** — see the note on this class.
@override@JsonKey(name: 'download_url') final  String? downloadUrl;
/// When [downloadUrl] stops working.
///
/// **Read rather than assumed**: it is configuration and may be shortened without notice, which
/// is why nothing in this client compiles in a lifetime.
@override@JsonKey(name: 'download_expires_at') final  String? downloadExpiresAt;
/// Why this milestone has no photograph, and absent when it has one.
@override@JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown) final  ProofExceptionReason? exceptionReason;
/// The **actor's** clock, carried through from the milestone. What to show a customer.
@override@JsonKey(name: 'recorded_at') final  String? recordedAt;
/// When the platform recorded it. What support reasons about.
@override@JsonKey(name: 'accepted_at') final  String? acceptedAt;

/// Create a copy of DeliveryProof
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$DeliveryProofCopyWith<_DeliveryProof> get copyWith => __$DeliveryProofCopyWithImpl<_DeliveryProof>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$DeliveryProofToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _DeliveryProof&&(identical(other.id, id) || other.id == id)&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.milestoneId, milestoneId) || other.milestoneId == milestoneId)&&(identical(other.milestone, milestone) || other.milestone == milestone)&&(identical(other.downloadUrl, downloadUrl) || other.downloadUrl == downloadUrl)&&(identical(other.downloadExpiresAt, downloadExpiresAt) || other.downloadExpiresAt == downloadExpiresAt)&&(identical(other.exceptionReason, exceptionReason) || other.exceptionReason == exceptionReason)&&(identical(other.recordedAt, recordedAt) || other.recordedAt == recordedAt)&&(identical(other.acceptedAt, acceptedAt) || other.acceptedAt == acceptedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,jobId,milestoneId,milestone,downloadUrl,downloadExpiresAt,exceptionReason,recordedAt,acceptedAt);

@override
String toString() {
  return 'DeliveryProof(id: $id, jobId: $jobId, milestoneId: $milestoneId, milestone: $milestone, downloadUrl: $downloadUrl, downloadExpiresAt: $downloadExpiresAt, exceptionReason: $exceptionReason, recordedAt: $recordedAt, acceptedAt: $acceptedAt)';
}


}

/// @nodoc
abstract mixin class _$DeliveryProofCopyWith<$Res> implements $DeliveryProofCopyWith<$Res> {
  factory _$DeliveryProofCopyWith(_DeliveryProof value, $Res Function(_DeliveryProof) _then) = __$DeliveryProofCopyWithImpl;
@override @useResult
$Res call({
 String id,@JsonKey(name: 'job_id') String jobId,@JsonKey(name: 'milestone_id') String? milestoneId, String milestone,@JsonKey(name: 'download_url') String? downloadUrl,@JsonKey(name: 'download_expires_at') String? downloadExpiresAt,@JsonKey(name: 'exception_reason', unknownEnumValue: ProofExceptionReason.unknown) ProofExceptionReason? exceptionReason,@JsonKey(name: 'recorded_at') String? recordedAt,@JsonKey(name: 'accepted_at') String? acceptedAt
});




}
/// @nodoc
class __$DeliveryProofCopyWithImpl<$Res>
    implements _$DeliveryProofCopyWith<$Res> {
  __$DeliveryProofCopyWithImpl(this._self, this._then);

  final _DeliveryProof _self;
  final $Res Function(_DeliveryProof) _then;

/// Create a copy of DeliveryProof
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? jobId = null,Object? milestoneId = freezed,Object? milestone = null,Object? downloadUrl = freezed,Object? downloadExpiresAt = freezed,Object? exceptionReason = freezed,Object? recordedAt = freezed,Object? acceptedAt = freezed,}) {
  return _then(_DeliveryProof(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,milestoneId: freezed == milestoneId ? _self.milestoneId : milestoneId // ignore: cast_nullable_to_non_nullable
as String?,milestone: null == milestone ? _self.milestone : milestone // ignore: cast_nullable_to_non_nullable
as String,downloadUrl: freezed == downloadUrl ? _self.downloadUrl : downloadUrl // ignore: cast_nullable_to_non_nullable
as String?,downloadExpiresAt: freezed == downloadExpiresAt ? _self.downloadExpiresAt : downloadExpiresAt // ignore: cast_nullable_to_non_nullable
as String?,exceptionReason: freezed == exceptionReason ? _self.exceptionReason : exceptionReason // ignore: cast_nullable_to_non_nullable
as ProofExceptionReason?,recordedAt: freezed == recordedAt ? _self.recordedAt : recordedAt // ignore: cast_nullable_to_non_nullable
as String?,acceptedAt: freezed == acceptedAt ? _self.acceptedAt : acceptedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}


/// @nodoc
mixin _$DeliveryDriver {

@JsonKey(name: 'job_id') String get jobId;/// Whether anybody is driving the job yet. When `false`, nothing else is present, and that is a
/// complete answer rather than a missing one.
@JsonKey(name: 'driver_assigned') bool get driverAssigned;/// The assignment record. A driver has no account, so this is their identity for the length of
/// one job — it is what their milestones are attributed to.
@JsonKey(name: 'assignment_id') String? get assignmentId;/// The name the transport provider wrote down.
@JsonKey(name: 'driver_name') String? get driverName;/// When the driver was put on the job, on the platform's clock, in UTC.
@JsonKey(name: 'assigned_at') String? get assignedAt;
/// Create a copy of DeliveryDriver
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$DeliveryDriverCopyWith<DeliveryDriver> get copyWith => _$DeliveryDriverCopyWithImpl<DeliveryDriver>(this as DeliveryDriver, _$identity);

  /// Serializes this DeliveryDriver to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is DeliveryDriver&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.driverAssigned, driverAssigned) || other.driverAssigned == driverAssigned)&&(identical(other.assignmentId, assignmentId) || other.assignmentId == assignmentId)&&(identical(other.driverName, driverName) || other.driverName == driverName)&&(identical(other.assignedAt, assignedAt) || other.assignedAt == assignedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,jobId,driverAssigned,assignmentId,driverName,assignedAt);

@override
String toString() {
  return 'DeliveryDriver(jobId: $jobId, driverAssigned: $driverAssigned, assignmentId: $assignmentId, driverName: $driverName, assignedAt: $assignedAt)';
}


}

/// @nodoc
abstract mixin class $DeliveryDriverCopyWith<$Res>  {
  factory $DeliveryDriverCopyWith(DeliveryDriver value, $Res Function(DeliveryDriver) _then) = _$DeliveryDriverCopyWithImpl;
@useResult
$Res call({
@JsonKey(name: 'job_id') String jobId,@JsonKey(name: 'driver_assigned') bool driverAssigned,@JsonKey(name: 'assignment_id') String? assignmentId,@JsonKey(name: 'driver_name') String? driverName,@JsonKey(name: 'assigned_at') String? assignedAt
});




}
/// @nodoc
class _$DeliveryDriverCopyWithImpl<$Res>
    implements $DeliveryDriverCopyWith<$Res> {
  _$DeliveryDriverCopyWithImpl(this._self, this._then);

  final DeliveryDriver _self;
  final $Res Function(DeliveryDriver) _then;

/// Create a copy of DeliveryDriver
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? jobId = null,Object? driverAssigned = null,Object? assignmentId = freezed,Object? driverName = freezed,Object? assignedAt = freezed,}) {
  return _then(_self.copyWith(
jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,driverAssigned: null == driverAssigned ? _self.driverAssigned : driverAssigned // ignore: cast_nullable_to_non_nullable
as bool,assignmentId: freezed == assignmentId ? _self.assignmentId : assignmentId // ignore: cast_nullable_to_non_nullable
as String?,driverName: freezed == driverName ? _self.driverName : driverName // ignore: cast_nullable_to_non_nullable
as String?,assignedAt: freezed == assignedAt ? _self.assignedAt : assignedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [DeliveryDriver].
extension DeliveryDriverPatterns on DeliveryDriver {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _DeliveryDriver value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _DeliveryDriver() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _DeliveryDriver value)  $default,){
final _that = this;
switch (_that) {
case _DeliveryDriver():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _DeliveryDriver value)?  $default,){
final _that = this;
switch (_that) {
case _DeliveryDriver() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function(@JsonKey(name: 'job_id')  String jobId, @JsonKey(name: 'driver_assigned')  bool driverAssigned, @JsonKey(name: 'assignment_id')  String? assignmentId, @JsonKey(name: 'driver_name')  String? driverName, @JsonKey(name: 'assigned_at')  String? assignedAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _DeliveryDriver() when $default != null:
return $default(_that.jobId,_that.driverAssigned,_that.assignmentId,_that.driverName,_that.assignedAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function(@JsonKey(name: 'job_id')  String jobId, @JsonKey(name: 'driver_assigned')  bool driverAssigned, @JsonKey(name: 'assignment_id')  String? assignmentId, @JsonKey(name: 'driver_name')  String? driverName, @JsonKey(name: 'assigned_at')  String? assignedAt)  $default,) {final _that = this;
switch (_that) {
case _DeliveryDriver():
return $default(_that.jobId,_that.driverAssigned,_that.assignmentId,_that.driverName,_that.assignedAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function(@JsonKey(name: 'job_id')  String jobId, @JsonKey(name: 'driver_assigned')  bool driverAssigned, @JsonKey(name: 'assignment_id')  String? assignmentId, @JsonKey(name: 'driver_name')  String? driverName, @JsonKey(name: 'assigned_at')  String? assignedAt)?  $default,) {final _that = this;
switch (_that) {
case _DeliveryDriver() when $default != null:
return $default(_that.jobId,_that.driverAssigned,_that.assignmentId,_that.driverName,_that.assignedAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _DeliveryDriver extends DeliveryDriver {
  const _DeliveryDriver({@JsonKey(name: 'job_id') required this.jobId, @JsonKey(name: 'driver_assigned') required this.driverAssigned, @JsonKey(name: 'assignment_id') this.assignmentId, @JsonKey(name: 'driver_name') this.driverName, @JsonKey(name: 'assigned_at') this.assignedAt}): super._();
  factory _DeliveryDriver.fromJson(Map<String, dynamic> json) => _$DeliveryDriverFromJson(json);

@override@JsonKey(name: 'job_id') final  String jobId;
/// Whether anybody is driving the job yet. When `false`, nothing else is present, and that is a
/// complete answer rather than a missing one.
@override@JsonKey(name: 'driver_assigned') final  bool driverAssigned;
/// The assignment record. A driver has no account, so this is their identity for the length of
/// one job — it is what their milestones are attributed to.
@override@JsonKey(name: 'assignment_id') final  String? assignmentId;
/// The name the transport provider wrote down.
@override@JsonKey(name: 'driver_name') final  String? driverName;
/// When the driver was put on the job, on the platform's clock, in UTC.
@override@JsonKey(name: 'assigned_at') final  String? assignedAt;

/// Create a copy of DeliveryDriver
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$DeliveryDriverCopyWith<_DeliveryDriver> get copyWith => __$DeliveryDriverCopyWithImpl<_DeliveryDriver>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$DeliveryDriverToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _DeliveryDriver&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.driverAssigned, driverAssigned) || other.driverAssigned == driverAssigned)&&(identical(other.assignmentId, assignmentId) || other.assignmentId == assignmentId)&&(identical(other.driverName, driverName) || other.driverName == driverName)&&(identical(other.assignedAt, assignedAt) || other.assignedAt == assignedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,jobId,driverAssigned,assignmentId,driverName,assignedAt);

@override
String toString() {
  return 'DeliveryDriver(jobId: $jobId, driverAssigned: $driverAssigned, assignmentId: $assignmentId, driverName: $driverName, assignedAt: $assignedAt)';
}


}

/// @nodoc
abstract mixin class _$DeliveryDriverCopyWith<$Res> implements $DeliveryDriverCopyWith<$Res> {
  factory _$DeliveryDriverCopyWith(_DeliveryDriver value, $Res Function(_DeliveryDriver) _then) = __$DeliveryDriverCopyWithImpl;
@override @useResult
$Res call({
@JsonKey(name: 'job_id') String jobId,@JsonKey(name: 'driver_assigned') bool driverAssigned,@JsonKey(name: 'assignment_id') String? assignmentId,@JsonKey(name: 'driver_name') String? driverName,@JsonKey(name: 'assigned_at') String? assignedAt
});




}
/// @nodoc
class __$DeliveryDriverCopyWithImpl<$Res>
    implements _$DeliveryDriverCopyWith<$Res> {
  __$DeliveryDriverCopyWithImpl(this._self, this._then);

  final _DeliveryDriver _self;
  final $Res Function(_DeliveryDriver) _then;

/// Create a copy of DeliveryDriver
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? jobId = null,Object? driverAssigned = null,Object? assignmentId = freezed,Object? driverName = freezed,Object? assignedAt = freezed,}) {
  return _then(_DeliveryDriver(
jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,driverAssigned: null == driverAssigned ? _self.driverAssigned : driverAssigned // ignore: cast_nullable_to_non_nullable
as bool,assignmentId: freezed == assignmentId ? _self.assignmentId : assignmentId // ignore: cast_nullable_to_non_nullable
as String?,driverName: freezed == driverName ? _self.driverName : driverName // ignore: cast_nullable_to_non_nullable
as String?,assignedAt: freezed == assignedAt ? _self.assignedAt : assignedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
