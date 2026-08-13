// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'bid.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$Bid {

 String get id;/// The job this offer is against.
///
/// **The only thing about the job in this shape.** Read `GET /v1/jobs/open/{id}` for the job.
@JsonKey(name: 'job_id') String get jobId;/// **Never a settable field.** A bid's status is the platform's — a provider cannot accept
/// their own offer, and `withdrawn` is reached through an endpoint rather than by writing it.
/// There is no `status` field in any request body in this API.
@JsonKey(unknownEnumValue: BidStatus.unknown) BidStatus get status;/// Which party made this offer (SHIP-87).
@JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) BidParty? get offeredBy;/// The price, in cents, as this device sent it. AUD; there is no currency field in the MVP.
///
/// Cents as a whole number because money is never a floating-point value (`Docs/10` §3.3):
/// `450.50` is not exactly representable and the errors accumulate.
@JsonKey(name: 'amount_cents') int? get amountCents;/// When the provider committed to collecting, in UTC.
///
/// **An instant, not a window.** The job carries the customer's flexibility as two windows; a
/// bid answers with two instants, because a provider says "I will be there at nine".
@JsonKey(name: 'pickup_at') String? get pickupAt;/// When the provider committed to having delivered, in UTC.
@JsonKey(name: 'deliver_by') String? get deliverBy;/// The conditions accompanying the offer, whitespace collapsed by the platform.
///
/// Omitted when none was given, so a client can tell "no conditions" from "an empty note"
/// without a second flag.
 String? get message;/// The counter-offer that displaced this one (SHIP-88).
///
/// **Omitted while this offer is the live head of its negotiation**, which is the useful case:
/// an offer with no `superseded_by` is the only one that can be countered and the only one the
/// customer can award.
@JsonKey(name: 'superseded_by') String? get supersededBy;@JsonKey(name: 'created_at') String? get createdAt;@JsonKey(name: 'updated_at') String? get updatedAt;
/// Create a copy of Bid
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$BidCopyWith<Bid> get copyWith => _$BidCopyWithImpl<Bid>(this as Bid, _$identity);

  /// Serializes this Bid to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is Bid&&(identical(other.id, id) || other.id == id)&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.status, status) || other.status == status)&&(identical(other.offeredBy, offeredBy) || other.offeredBy == offeredBy)&&(identical(other.amountCents, amountCents) || other.amountCents == amountCents)&&(identical(other.pickupAt, pickupAt) || other.pickupAt == pickupAt)&&(identical(other.deliverBy, deliverBy) || other.deliverBy == deliverBy)&&(identical(other.message, message) || other.message == message)&&(identical(other.supersededBy, supersededBy) || other.supersededBy == supersededBy)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt)&&(identical(other.updatedAt, updatedAt) || other.updatedAt == updatedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,jobId,status,offeredBy,amountCents,pickupAt,deliverBy,message,supersededBy,createdAt,updatedAt);

@override
String toString() {
  return 'Bid(id: $id, jobId: $jobId, status: $status, offeredBy: $offeredBy, amountCents: $amountCents, pickupAt: $pickupAt, deliverBy: $deliverBy, message: $message, supersededBy: $supersededBy, createdAt: $createdAt, updatedAt: $updatedAt)';
}


}

/// @nodoc
abstract mixin class $BidCopyWith<$Res>  {
  factory $BidCopyWith(Bid value, $Res Function(Bid) _then) = _$BidCopyWithImpl;
@useResult
$Res call({
 String id,@JsonKey(name: 'job_id') String jobId,@JsonKey(unknownEnumValue: BidStatus.unknown) BidStatus status,@JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) BidParty? offeredBy,@JsonKey(name: 'amount_cents') int? amountCents,@JsonKey(name: 'pickup_at') String? pickupAt,@JsonKey(name: 'deliver_by') String? deliverBy, String? message,@JsonKey(name: 'superseded_by') String? supersededBy,@JsonKey(name: 'created_at') String? createdAt,@JsonKey(name: 'updated_at') String? updatedAt
});




}
/// @nodoc
class _$BidCopyWithImpl<$Res>
    implements $BidCopyWith<$Res> {
  _$BidCopyWithImpl(this._self, this._then);

  final Bid _self;
  final $Res Function(Bid) _then;

/// Create a copy of Bid
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? jobId = null,Object? status = null,Object? offeredBy = freezed,Object? amountCents = freezed,Object? pickupAt = freezed,Object? deliverBy = freezed,Object? message = freezed,Object? supersededBy = freezed,Object? createdAt = freezed,Object? updatedAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,status: null == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as BidStatus,offeredBy: freezed == offeredBy ? _self.offeredBy : offeredBy // ignore: cast_nullable_to_non_nullable
as BidParty?,amountCents: freezed == amountCents ? _self.amountCents : amountCents // ignore: cast_nullable_to_non_nullable
as int?,pickupAt: freezed == pickupAt ? _self.pickupAt : pickupAt // ignore: cast_nullable_to_non_nullable
as String?,deliverBy: freezed == deliverBy ? _self.deliverBy : deliverBy // ignore: cast_nullable_to_non_nullable
as String?,message: freezed == message ? _self.message : message // ignore: cast_nullable_to_non_nullable
as String?,supersededBy: freezed == supersededBy ? _self.supersededBy : supersededBy // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,updatedAt: freezed == updatedAt ? _self.updatedAt : updatedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [Bid].
extension BidPatterns on Bid {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _Bid value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _Bid() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _Bid value)  $default,){
final _that = this;
switch (_that) {
case _Bid():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _Bid value)?  $default,){
final _that = this;
switch (_that) {
case _Bid() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(unknownEnumValue: BidStatus.unknown)  BidStatus status, @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown)  BidParty? offeredBy, @JsonKey(name: 'amount_cents')  int? amountCents, @JsonKey(name: 'pickup_at')  String? pickupAt, @JsonKey(name: 'deliver_by')  String? deliverBy,  String? message, @JsonKey(name: 'superseded_by')  String? supersededBy, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _Bid() when $default != null:
return $default(_that.id,_that.jobId,_that.status,_that.offeredBy,_that.amountCents,_that.pickupAt,_that.deliverBy,_that.message,_that.supersededBy,_that.createdAt,_that.updatedAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(unknownEnumValue: BidStatus.unknown)  BidStatus status, @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown)  BidParty? offeredBy, @JsonKey(name: 'amount_cents')  int? amountCents, @JsonKey(name: 'pickup_at')  String? pickupAt, @JsonKey(name: 'deliver_by')  String? deliverBy,  String? message, @JsonKey(name: 'superseded_by')  String? supersededBy, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)  $default,) {final _that = this;
switch (_that) {
case _Bid():
return $default(_that.id,_that.jobId,_that.status,_that.offeredBy,_that.amountCents,_that.pickupAt,_that.deliverBy,_that.message,_that.supersededBy,_that.createdAt,_that.updatedAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(unknownEnumValue: BidStatus.unknown)  BidStatus status, @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown)  BidParty? offeredBy, @JsonKey(name: 'amount_cents')  int? amountCents, @JsonKey(name: 'pickup_at')  String? pickupAt, @JsonKey(name: 'deliver_by')  String? deliverBy,  String? message, @JsonKey(name: 'superseded_by')  String? supersededBy, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)?  $default,) {final _that = this;
switch (_that) {
case _Bid() when $default != null:
return $default(_that.id,_that.jobId,_that.status,_that.offeredBy,_that.amountCents,_that.pickupAt,_that.deliverBy,_that.message,_that.supersededBy,_that.createdAt,_that.updatedAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _Bid extends Bid {
  const _Bid({required this.id, @JsonKey(name: 'job_id') required this.jobId, @JsonKey(unknownEnumValue: BidStatus.unknown) required this.status, @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) this.offeredBy, @JsonKey(name: 'amount_cents') this.amountCents, @JsonKey(name: 'pickup_at') this.pickupAt, @JsonKey(name: 'deliver_by') this.deliverBy, this.message, @JsonKey(name: 'superseded_by') this.supersededBy, @JsonKey(name: 'created_at') this.createdAt, @JsonKey(name: 'updated_at') this.updatedAt}): super._();
  factory _Bid.fromJson(Map<String, dynamic> json) => _$BidFromJson(json);

@override final  String id;
/// The job this offer is against.
///
/// **The only thing about the job in this shape.** Read `GET /v1/jobs/open/{id}` for the job.
@override@JsonKey(name: 'job_id') final  String jobId;
/// **Never a settable field.** A bid's status is the platform's — a provider cannot accept
/// their own offer, and `withdrawn` is reached through an endpoint rather than by writing it.
/// There is no `status` field in any request body in this API.
@override@JsonKey(unknownEnumValue: BidStatus.unknown) final  BidStatus status;
/// Which party made this offer (SHIP-87).
@override@JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) final  BidParty? offeredBy;
/// The price, in cents, as this device sent it. AUD; there is no currency field in the MVP.
///
/// Cents as a whole number because money is never a floating-point value (`Docs/10` §3.3):
/// `450.50` is not exactly representable and the errors accumulate.
@override@JsonKey(name: 'amount_cents') final  int? amountCents;
/// When the provider committed to collecting, in UTC.
///
/// **An instant, not a window.** The job carries the customer's flexibility as two windows; a
/// bid answers with two instants, because a provider says "I will be there at nine".
@override@JsonKey(name: 'pickup_at') final  String? pickupAt;
/// When the provider committed to having delivered, in UTC.
@override@JsonKey(name: 'deliver_by') final  String? deliverBy;
/// The conditions accompanying the offer, whitespace collapsed by the platform.
///
/// Omitted when none was given, so a client can tell "no conditions" from "an empty note"
/// without a second flag.
@override final  String? message;
/// The counter-offer that displaced this one (SHIP-88).
///
/// **Omitted while this offer is the live head of its negotiation**, which is the useful case:
/// an offer with no `superseded_by` is the only one that can be countered and the only one the
/// customer can award.
@override@JsonKey(name: 'superseded_by') final  String? supersededBy;
@override@JsonKey(name: 'created_at') final  String? createdAt;
@override@JsonKey(name: 'updated_at') final  String? updatedAt;

/// Create a copy of Bid
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$BidCopyWith<_Bid> get copyWith => __$BidCopyWithImpl<_Bid>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$BidToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _Bid&&(identical(other.id, id) || other.id == id)&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.status, status) || other.status == status)&&(identical(other.offeredBy, offeredBy) || other.offeredBy == offeredBy)&&(identical(other.amountCents, amountCents) || other.amountCents == amountCents)&&(identical(other.pickupAt, pickupAt) || other.pickupAt == pickupAt)&&(identical(other.deliverBy, deliverBy) || other.deliverBy == deliverBy)&&(identical(other.message, message) || other.message == message)&&(identical(other.supersededBy, supersededBy) || other.supersededBy == supersededBy)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt)&&(identical(other.updatedAt, updatedAt) || other.updatedAt == updatedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,jobId,status,offeredBy,amountCents,pickupAt,deliverBy,message,supersededBy,createdAt,updatedAt);

@override
String toString() {
  return 'Bid(id: $id, jobId: $jobId, status: $status, offeredBy: $offeredBy, amountCents: $amountCents, pickupAt: $pickupAt, deliverBy: $deliverBy, message: $message, supersededBy: $supersededBy, createdAt: $createdAt, updatedAt: $updatedAt)';
}


}

/// @nodoc
abstract mixin class _$BidCopyWith<$Res> implements $BidCopyWith<$Res> {
  factory _$BidCopyWith(_Bid value, $Res Function(_Bid) _then) = __$BidCopyWithImpl;
@override @useResult
$Res call({
 String id,@JsonKey(name: 'job_id') String jobId,@JsonKey(unknownEnumValue: BidStatus.unknown) BidStatus status,@JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) BidParty? offeredBy,@JsonKey(name: 'amount_cents') int? amountCents,@JsonKey(name: 'pickup_at') String? pickupAt,@JsonKey(name: 'deliver_by') String? deliverBy, String? message,@JsonKey(name: 'superseded_by') String? supersededBy,@JsonKey(name: 'created_at') String? createdAt,@JsonKey(name: 'updated_at') String? updatedAt
});




}
/// @nodoc
class __$BidCopyWithImpl<$Res>
    implements _$BidCopyWith<$Res> {
  __$BidCopyWithImpl(this._self, this._then);

  final _Bid _self;
  final $Res Function(_Bid) _then;

/// Create a copy of Bid
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? jobId = null,Object? status = null,Object? offeredBy = freezed,Object? amountCents = freezed,Object? pickupAt = freezed,Object? deliverBy = freezed,Object? message = freezed,Object? supersededBy = freezed,Object? createdAt = freezed,Object? updatedAt = freezed,}) {
  return _then(_Bid(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,status: null == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as BidStatus,offeredBy: freezed == offeredBy ? _self.offeredBy : offeredBy // ignore: cast_nullable_to_non_nullable
as BidParty?,amountCents: freezed == amountCents ? _self.amountCents : amountCents // ignore: cast_nullable_to_non_nullable
as int?,pickupAt: freezed == pickupAt ? _self.pickupAt : pickupAt // ignore: cast_nullable_to_non_nullable
as String?,deliverBy: freezed == deliverBy ? _self.deliverBy : deliverBy // ignore: cast_nullable_to_non_nullable
as String?,message: freezed == message ? _self.message : message // ignore: cast_nullable_to_non_nullable
as String?,supersededBy: freezed == supersededBy ? _self.supersededBy : supersededBy // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,updatedAt: freezed == updatedAt ? _self.updatedAt : updatedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
