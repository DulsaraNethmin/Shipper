// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'received_offer.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$ReceivedOffer {

 String get id;/// The job this offer is against. **The only thing about the job in this shape.**
@JsonKey(name: 'job_id') String get jobId;/// **Never a settable field.** A bid's status is the platform's.
@JsonKey(unknownEnumValue: BidStatus.unknown) BidStatus get status;/// Which party made this offer — the provider bidding, or this customer answering them.
///
/// A negotiation alternates, so the live offer on a job may be the customer's own counter. The
/// comparison screen says so rather than presenting it as something to award: `Docs/02` §3 and
/// `ck_bids_only_a_providers_offer_is_accepted` both refuse awarding your own counter-offer.
@JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) BidParty? get offeredBy;/// The price being asked, in cents. AUD; there is no currency field in the MVP.
@JsonKey(name: 'amount_cents') int? get amountCents;/// When the provider commits to collecting, in UTC.
@JsonKey(name: 'pickup_at') String? get pickupAt;/// When they commit to having delivered, in UTC.
@JsonKey(name: 'deliver_by') String? get deliverBy;/// The conditions accompanying the offer, in the offering party's own words.
 String? get message;/// The counter-offer that displaced this one. Absent while this is the live head.
@JsonKey(name: 'superseded_by') String? get supersededBy;/// Who made the offer, as much as this platform will tell a customer about them.
 ProviderSummary? get provider;/// The vehicle the offer is made with.
///
/// **Absent is the ordinary case rather than an edge case.** `bids.vehicle_id` arrived at
/// SHIP-102a, so every offer placed before it names none — as does every offer from a provider
/// whose client has not started sending the field. A screen that treated absence as a fault
/// would show most of the marketplace as broken.
 VehicleSummary? get vehicle;@JsonKey(name: 'created_at') String? get createdAt;@JsonKey(name: 'updated_at') String? get updatedAt;
/// Create a copy of ReceivedOffer
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$ReceivedOfferCopyWith<ReceivedOffer> get copyWith => _$ReceivedOfferCopyWithImpl<ReceivedOffer>(this as ReceivedOffer, _$identity);

  /// Serializes this ReceivedOffer to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is ReceivedOffer&&(identical(other.id, id) || other.id == id)&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.status, status) || other.status == status)&&(identical(other.offeredBy, offeredBy) || other.offeredBy == offeredBy)&&(identical(other.amountCents, amountCents) || other.amountCents == amountCents)&&(identical(other.pickupAt, pickupAt) || other.pickupAt == pickupAt)&&(identical(other.deliverBy, deliverBy) || other.deliverBy == deliverBy)&&(identical(other.message, message) || other.message == message)&&(identical(other.supersededBy, supersededBy) || other.supersededBy == supersededBy)&&(identical(other.provider, provider) || other.provider == provider)&&(identical(other.vehicle, vehicle) || other.vehicle == vehicle)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt)&&(identical(other.updatedAt, updatedAt) || other.updatedAt == updatedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,jobId,status,offeredBy,amountCents,pickupAt,deliverBy,message,supersededBy,provider,vehicle,createdAt,updatedAt);

@override
String toString() {
  return 'ReceivedOffer(id: $id, jobId: $jobId, status: $status, offeredBy: $offeredBy, amountCents: $amountCents, pickupAt: $pickupAt, deliverBy: $deliverBy, message: $message, supersededBy: $supersededBy, provider: $provider, vehicle: $vehicle, createdAt: $createdAt, updatedAt: $updatedAt)';
}


}

/// @nodoc
abstract mixin class $ReceivedOfferCopyWith<$Res>  {
  factory $ReceivedOfferCopyWith(ReceivedOffer value, $Res Function(ReceivedOffer) _then) = _$ReceivedOfferCopyWithImpl;
@useResult
$Res call({
 String id,@JsonKey(name: 'job_id') String jobId,@JsonKey(unknownEnumValue: BidStatus.unknown) BidStatus status,@JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) BidParty? offeredBy,@JsonKey(name: 'amount_cents') int? amountCents,@JsonKey(name: 'pickup_at') String? pickupAt,@JsonKey(name: 'deliver_by') String? deliverBy, String? message,@JsonKey(name: 'superseded_by') String? supersededBy, ProviderSummary? provider, VehicleSummary? vehicle,@JsonKey(name: 'created_at') String? createdAt,@JsonKey(name: 'updated_at') String? updatedAt
});


$ProviderSummaryCopyWith<$Res>? get provider;$VehicleSummaryCopyWith<$Res>? get vehicle;

}
/// @nodoc
class _$ReceivedOfferCopyWithImpl<$Res>
    implements $ReceivedOfferCopyWith<$Res> {
  _$ReceivedOfferCopyWithImpl(this._self, this._then);

  final ReceivedOffer _self;
  final $Res Function(ReceivedOffer) _then;

/// Create a copy of ReceivedOffer
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? jobId = null,Object? status = null,Object? offeredBy = freezed,Object? amountCents = freezed,Object? pickupAt = freezed,Object? deliverBy = freezed,Object? message = freezed,Object? supersededBy = freezed,Object? provider = freezed,Object? vehicle = freezed,Object? createdAt = freezed,Object? updatedAt = freezed,}) {
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
as String?,provider: freezed == provider ? _self.provider : provider // ignore: cast_nullable_to_non_nullable
as ProviderSummary?,vehicle: freezed == vehicle ? _self.vehicle : vehicle // ignore: cast_nullable_to_non_nullable
as VehicleSummary?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,updatedAt: freezed == updatedAt ? _self.updatedAt : updatedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}
/// Create a copy of ReceivedOffer
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$ProviderSummaryCopyWith<$Res>? get provider {
    if (_self.provider == null) {
    return null;
  }

  return $ProviderSummaryCopyWith<$Res>(_self.provider!, (value) {
    return _then(_self.copyWith(provider: value));
  });
}/// Create a copy of ReceivedOffer
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$VehicleSummaryCopyWith<$Res>? get vehicle {
    if (_self.vehicle == null) {
    return null;
  }

  return $VehicleSummaryCopyWith<$Res>(_self.vehicle!, (value) {
    return _then(_self.copyWith(vehicle: value));
  });
}
}


/// Adds pattern-matching-related methods to [ReceivedOffer].
extension ReceivedOfferPatterns on ReceivedOffer {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _ReceivedOffer value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _ReceivedOffer() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _ReceivedOffer value)  $default,){
final _that = this;
switch (_that) {
case _ReceivedOffer():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _ReceivedOffer value)?  $default,){
final _that = this;
switch (_that) {
case _ReceivedOffer() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(unknownEnumValue: BidStatus.unknown)  BidStatus status, @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown)  BidParty? offeredBy, @JsonKey(name: 'amount_cents')  int? amountCents, @JsonKey(name: 'pickup_at')  String? pickupAt, @JsonKey(name: 'deliver_by')  String? deliverBy,  String? message, @JsonKey(name: 'superseded_by')  String? supersededBy,  ProviderSummary? provider,  VehicleSummary? vehicle, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _ReceivedOffer() when $default != null:
return $default(_that.id,_that.jobId,_that.status,_that.offeredBy,_that.amountCents,_that.pickupAt,_that.deliverBy,_that.message,_that.supersededBy,_that.provider,_that.vehicle,_that.createdAt,_that.updatedAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(unknownEnumValue: BidStatus.unknown)  BidStatus status, @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown)  BidParty? offeredBy, @JsonKey(name: 'amount_cents')  int? amountCents, @JsonKey(name: 'pickup_at')  String? pickupAt, @JsonKey(name: 'deliver_by')  String? deliverBy,  String? message, @JsonKey(name: 'superseded_by')  String? supersededBy,  ProviderSummary? provider,  VehicleSummary? vehicle, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)  $default,) {final _that = this;
switch (_that) {
case _ReceivedOffer():
return $default(_that.id,_that.jobId,_that.status,_that.offeredBy,_that.amountCents,_that.pickupAt,_that.deliverBy,_that.message,_that.supersededBy,_that.provider,_that.vehicle,_that.createdAt,_that.updatedAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id, @JsonKey(name: 'job_id')  String jobId, @JsonKey(unknownEnumValue: BidStatus.unknown)  BidStatus status, @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown)  BidParty? offeredBy, @JsonKey(name: 'amount_cents')  int? amountCents, @JsonKey(name: 'pickup_at')  String? pickupAt, @JsonKey(name: 'deliver_by')  String? deliverBy,  String? message, @JsonKey(name: 'superseded_by')  String? supersededBy,  ProviderSummary? provider,  VehicleSummary? vehicle, @JsonKey(name: 'created_at')  String? createdAt, @JsonKey(name: 'updated_at')  String? updatedAt)?  $default,) {final _that = this;
switch (_that) {
case _ReceivedOffer() when $default != null:
return $default(_that.id,_that.jobId,_that.status,_that.offeredBy,_that.amountCents,_that.pickupAt,_that.deliverBy,_that.message,_that.supersededBy,_that.provider,_that.vehicle,_that.createdAt,_that.updatedAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _ReceivedOffer extends ReceivedOffer {
  const _ReceivedOffer({required this.id, @JsonKey(name: 'job_id') required this.jobId, @JsonKey(unknownEnumValue: BidStatus.unknown) required this.status, @JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) this.offeredBy, @JsonKey(name: 'amount_cents') this.amountCents, @JsonKey(name: 'pickup_at') this.pickupAt, @JsonKey(name: 'deliver_by') this.deliverBy, this.message, @JsonKey(name: 'superseded_by') this.supersededBy, this.provider, this.vehicle, @JsonKey(name: 'created_at') this.createdAt, @JsonKey(name: 'updated_at') this.updatedAt}): super._();
  factory _ReceivedOffer.fromJson(Map<String, dynamic> json) => _$ReceivedOfferFromJson(json);

@override final  String id;
/// The job this offer is against. **The only thing about the job in this shape.**
@override@JsonKey(name: 'job_id') final  String jobId;
/// **Never a settable field.** A bid's status is the platform's.
@override@JsonKey(unknownEnumValue: BidStatus.unknown) final  BidStatus status;
/// Which party made this offer — the provider bidding, or this customer answering them.
///
/// A negotiation alternates, so the live offer on a job may be the customer's own counter. The
/// comparison screen says so rather than presenting it as something to award: `Docs/02` §3 and
/// `ck_bids_only_a_providers_offer_is_accepted` both refuse awarding your own counter-offer.
@override@JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) final  BidParty? offeredBy;
/// The price being asked, in cents. AUD; there is no currency field in the MVP.
@override@JsonKey(name: 'amount_cents') final  int? amountCents;
/// When the provider commits to collecting, in UTC.
@override@JsonKey(name: 'pickup_at') final  String? pickupAt;
/// When they commit to having delivered, in UTC.
@override@JsonKey(name: 'deliver_by') final  String? deliverBy;
/// The conditions accompanying the offer, in the offering party's own words.
@override final  String? message;
/// The counter-offer that displaced this one. Absent while this is the live head.
@override@JsonKey(name: 'superseded_by') final  String? supersededBy;
/// Who made the offer, as much as this platform will tell a customer about them.
@override final  ProviderSummary? provider;
/// The vehicle the offer is made with.
///
/// **Absent is the ordinary case rather than an edge case.** `bids.vehicle_id` arrived at
/// SHIP-102a, so every offer placed before it names none — as does every offer from a provider
/// whose client has not started sending the field. A screen that treated absence as a fault
/// would show most of the marketplace as broken.
@override final  VehicleSummary? vehicle;
@override@JsonKey(name: 'created_at') final  String? createdAt;
@override@JsonKey(name: 'updated_at') final  String? updatedAt;

/// Create a copy of ReceivedOffer
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$ReceivedOfferCopyWith<_ReceivedOffer> get copyWith => __$ReceivedOfferCopyWithImpl<_ReceivedOffer>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$ReceivedOfferToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _ReceivedOffer&&(identical(other.id, id) || other.id == id)&&(identical(other.jobId, jobId) || other.jobId == jobId)&&(identical(other.status, status) || other.status == status)&&(identical(other.offeredBy, offeredBy) || other.offeredBy == offeredBy)&&(identical(other.amountCents, amountCents) || other.amountCents == amountCents)&&(identical(other.pickupAt, pickupAt) || other.pickupAt == pickupAt)&&(identical(other.deliverBy, deliverBy) || other.deliverBy == deliverBy)&&(identical(other.message, message) || other.message == message)&&(identical(other.supersededBy, supersededBy) || other.supersededBy == supersededBy)&&(identical(other.provider, provider) || other.provider == provider)&&(identical(other.vehicle, vehicle) || other.vehicle == vehicle)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt)&&(identical(other.updatedAt, updatedAt) || other.updatedAt == updatedAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,jobId,status,offeredBy,amountCents,pickupAt,deliverBy,message,supersededBy,provider,vehicle,createdAt,updatedAt);

@override
String toString() {
  return 'ReceivedOffer(id: $id, jobId: $jobId, status: $status, offeredBy: $offeredBy, amountCents: $amountCents, pickupAt: $pickupAt, deliverBy: $deliverBy, message: $message, supersededBy: $supersededBy, provider: $provider, vehicle: $vehicle, createdAt: $createdAt, updatedAt: $updatedAt)';
}


}

/// @nodoc
abstract mixin class _$ReceivedOfferCopyWith<$Res> implements $ReceivedOfferCopyWith<$Res> {
  factory _$ReceivedOfferCopyWith(_ReceivedOffer value, $Res Function(_ReceivedOffer) _then) = __$ReceivedOfferCopyWithImpl;
@override @useResult
$Res call({
 String id,@JsonKey(name: 'job_id') String jobId,@JsonKey(unknownEnumValue: BidStatus.unknown) BidStatus status,@JsonKey(name: 'offered_by', unknownEnumValue: BidParty.unknown) BidParty? offeredBy,@JsonKey(name: 'amount_cents') int? amountCents,@JsonKey(name: 'pickup_at') String? pickupAt,@JsonKey(name: 'deliver_by') String? deliverBy, String? message,@JsonKey(name: 'superseded_by') String? supersededBy, ProviderSummary? provider, VehicleSummary? vehicle,@JsonKey(name: 'created_at') String? createdAt,@JsonKey(name: 'updated_at') String? updatedAt
});


@override $ProviderSummaryCopyWith<$Res>? get provider;@override $VehicleSummaryCopyWith<$Res>? get vehicle;

}
/// @nodoc
class __$ReceivedOfferCopyWithImpl<$Res>
    implements _$ReceivedOfferCopyWith<$Res> {
  __$ReceivedOfferCopyWithImpl(this._self, this._then);

  final _ReceivedOffer _self;
  final $Res Function(_ReceivedOffer) _then;

/// Create a copy of ReceivedOffer
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? jobId = null,Object? status = null,Object? offeredBy = freezed,Object? amountCents = freezed,Object? pickupAt = freezed,Object? deliverBy = freezed,Object? message = freezed,Object? supersededBy = freezed,Object? provider = freezed,Object? vehicle = freezed,Object? createdAt = freezed,Object? updatedAt = freezed,}) {
  return _then(_ReceivedOffer(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,jobId: null == jobId ? _self.jobId : jobId // ignore: cast_nullable_to_non_nullable
as String,status: null == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as BidStatus,offeredBy: freezed == offeredBy ? _self.offeredBy : offeredBy // ignore: cast_nullable_to_non_nullable
as BidParty?,amountCents: freezed == amountCents ? _self.amountCents : amountCents // ignore: cast_nullable_to_non_nullable
as int?,pickupAt: freezed == pickupAt ? _self.pickupAt : pickupAt // ignore: cast_nullable_to_non_nullable
as String?,deliverBy: freezed == deliverBy ? _self.deliverBy : deliverBy // ignore: cast_nullable_to_non_nullable
as String?,message: freezed == message ? _self.message : message // ignore: cast_nullable_to_non_nullable
as String?,supersededBy: freezed == supersededBy ? _self.supersededBy : supersededBy // ignore: cast_nullable_to_non_nullable
as String?,provider: freezed == provider ? _self.provider : provider // ignore: cast_nullable_to_non_nullable
as ProviderSummary?,vehicle: freezed == vehicle ? _self.vehicle : vehicle // ignore: cast_nullable_to_non_nullable
as VehicleSummary?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,updatedAt: freezed == updatedAt ? _self.updatedAt : updatedAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

/// Create a copy of ReceivedOffer
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$ProviderSummaryCopyWith<$Res>? get provider {
    if (_self.provider == null) {
    return null;
  }

  return $ProviderSummaryCopyWith<$Res>(_self.provider!, (value) {
    return _then(_self.copyWith(provider: value));
  });
}/// Create a copy of ReceivedOffer
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$VehicleSummaryCopyWith<$Res>? get vehicle {
    if (_self.vehicle == null) {
    return null;
  }

  return $VehicleSummaryCopyWith<$Res>(_self.vehicle!, (value) {
    return _then(_self.copyWith(vehicle: value));
  });
}
}


/// @nodoc
mixin _$ProviderSummary {

 String get id;/// Whether this provider has cleared the platform's verification: contact details confirmed, on
/// an account in good standing.
///
/// The same test the job feed applies before offering them work — so a provider who was able to
/// bid is a provider this is `true` about, and a `false` here is worth noticing rather than
/// worth hiding.
 bool get verified;/// When the provider's account was created, in UTC. Rendered day-first.
@JsonKey(name: 'member_since') String? get memberSince;
/// Create a copy of ProviderSummary
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$ProviderSummaryCopyWith<ProviderSummary> get copyWith => _$ProviderSummaryCopyWithImpl<ProviderSummary>(this as ProviderSummary, _$identity);

  /// Serializes this ProviderSummary to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is ProviderSummary&&(identical(other.id, id) || other.id == id)&&(identical(other.verified, verified) || other.verified == verified)&&(identical(other.memberSince, memberSince) || other.memberSince == memberSince));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,verified,memberSince);

@override
String toString() {
  return 'ProviderSummary(id: $id, verified: $verified, memberSince: $memberSince)';
}


}

/// @nodoc
abstract mixin class $ProviderSummaryCopyWith<$Res>  {
  factory $ProviderSummaryCopyWith(ProviderSummary value, $Res Function(ProviderSummary) _then) = _$ProviderSummaryCopyWithImpl;
@useResult
$Res call({
 String id, bool verified,@JsonKey(name: 'member_since') String? memberSince
});




}
/// @nodoc
class _$ProviderSummaryCopyWithImpl<$Res>
    implements $ProviderSummaryCopyWith<$Res> {
  _$ProviderSummaryCopyWithImpl(this._self, this._then);

  final ProviderSummary _self;
  final $Res Function(ProviderSummary) _then;

/// Create a copy of ProviderSummary
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? verified = null,Object? memberSince = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,verified: null == verified ? _self.verified : verified // ignore: cast_nullable_to_non_nullable
as bool,memberSince: freezed == memberSince ? _self.memberSince : memberSince // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [ProviderSummary].
extension ProviderSummaryPatterns on ProviderSummary {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _ProviderSummary value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _ProviderSummary() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _ProviderSummary value)  $default,){
final _that = this;
switch (_that) {
case _ProviderSummary():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _ProviderSummary value)?  $default,){
final _that = this;
switch (_that) {
case _ProviderSummary() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id,  bool verified, @JsonKey(name: 'member_since')  String? memberSince)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _ProviderSummary() when $default != null:
return $default(_that.id,_that.verified,_that.memberSince);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id,  bool verified, @JsonKey(name: 'member_since')  String? memberSince)  $default,) {final _that = this;
switch (_that) {
case _ProviderSummary():
return $default(_that.id,_that.verified,_that.memberSince);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id,  bool verified, @JsonKey(name: 'member_since')  String? memberSince)?  $default,) {final _that = this;
switch (_that) {
case _ProviderSummary() when $default != null:
return $default(_that.id,_that.verified,_that.memberSince);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _ProviderSummary extends ProviderSummary {
  const _ProviderSummary({required this.id, this.verified = false, @JsonKey(name: 'member_since') this.memberSince}): super._();
  factory _ProviderSummary.fromJson(Map<String, dynamic> json) => _$ProviderSummaryFromJson(json);

@override final  String id;
/// Whether this provider has cleared the platform's verification: contact details confirmed, on
/// an account in good standing.
///
/// The same test the job feed applies before offering them work — so a provider who was able to
/// bid is a provider this is `true` about, and a `false` here is worth noticing rather than
/// worth hiding.
@override@JsonKey() final  bool verified;
/// When the provider's account was created, in UTC. Rendered day-first.
@override@JsonKey(name: 'member_since') final  String? memberSince;

/// Create a copy of ProviderSummary
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$ProviderSummaryCopyWith<_ProviderSummary> get copyWith => __$ProviderSummaryCopyWithImpl<_ProviderSummary>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$ProviderSummaryToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _ProviderSummary&&(identical(other.id, id) || other.id == id)&&(identical(other.verified, verified) || other.verified == verified)&&(identical(other.memberSince, memberSince) || other.memberSince == memberSince));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,verified,memberSince);

@override
String toString() {
  return 'ProviderSummary(id: $id, verified: $verified, memberSince: $memberSince)';
}


}

/// @nodoc
abstract mixin class _$ProviderSummaryCopyWith<$Res> implements $ProviderSummaryCopyWith<$Res> {
  factory _$ProviderSummaryCopyWith(_ProviderSummary value, $Res Function(_ProviderSummary) _then) = __$ProviderSummaryCopyWithImpl;
@override @useResult
$Res call({
 String id, bool verified,@JsonKey(name: 'member_since') String? memberSince
});




}
/// @nodoc
class __$ProviderSummaryCopyWithImpl<$Res>
    implements _$ProviderSummaryCopyWith<$Res> {
  __$ProviderSummaryCopyWithImpl(this._self, this._then);

  final _ProviderSummary _self;
  final $Res Function(_ProviderSummary) _then;

/// Create a copy of ProviderSummary
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? verified = null,Object? memberSince = freezed,}) {
  return _then(_ProviderSummary(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,verified: null == verified ? _self.verified : verified // ignore: cast_nullable_to_non_nullable
as bool,memberSince: freezed == memberSince ? _self.memberSince : memberSince // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}


/// @nodoc
mixin _$VehicleSummary {

 String get id;/// The kind of vehicle, as the provider registered it.
///
/// **A string rather than an enumeration**, unlike [BidStatus]. The list is `fleet`'s and is
/// not generated from `contracts/statuses.yaml`, so a client enumeration would be a second copy
/// of somebody else's vocabulary — and a vehicle type added on the platform would arrive as
/// `unknown` on every phone until the next store release. `Docs/07` §6 is explicit that
/// anything expected to change under operational pressure stays server-side.
 String get type;/// Omitted when the provider did not state one.
 String get make; String get model;/// What it can carry, as declared — `Docs/01` §4.3's "declared capability".
 VehicleCapacity get capacity;
/// Create a copy of VehicleSummary
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$VehicleSummaryCopyWith<VehicleSummary> get copyWith => _$VehicleSummaryCopyWithImpl<VehicleSummary>(this as VehicleSummary, _$identity);

  /// Serializes this VehicleSummary to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is VehicleSummary&&(identical(other.id, id) || other.id == id)&&(identical(other.type, type) || other.type == type)&&(identical(other.make, make) || other.make == make)&&(identical(other.model, model) || other.model == model)&&(identical(other.capacity, capacity) || other.capacity == capacity));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,type,make,model,capacity);

@override
String toString() {
  return 'VehicleSummary(id: $id, type: $type, make: $make, model: $model, capacity: $capacity)';
}


}

/// @nodoc
abstract mixin class $VehicleSummaryCopyWith<$Res>  {
  factory $VehicleSummaryCopyWith(VehicleSummary value, $Res Function(VehicleSummary) _then) = _$VehicleSummaryCopyWithImpl;
@useResult
$Res call({
 String id, String type, String make, String model, VehicleCapacity capacity
});


$VehicleCapacityCopyWith<$Res> get capacity;

}
/// @nodoc
class _$VehicleSummaryCopyWithImpl<$Res>
    implements $VehicleSummaryCopyWith<$Res> {
  _$VehicleSummaryCopyWithImpl(this._self, this._then);

  final VehicleSummary _self;
  final $Res Function(VehicleSummary) _then;

/// Create a copy of VehicleSummary
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? type = null,Object? make = null,Object? model = null,Object? capacity = null,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,type: null == type ? _self.type : type // ignore: cast_nullable_to_non_nullable
as String,make: null == make ? _self.make : make // ignore: cast_nullable_to_non_nullable
as String,model: null == model ? _self.model : model // ignore: cast_nullable_to_non_nullable
as String,capacity: null == capacity ? _self.capacity : capacity // ignore: cast_nullable_to_non_nullable
as VehicleCapacity,
  ));
}
/// Create a copy of VehicleSummary
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$VehicleCapacityCopyWith<$Res> get capacity {
  
  return $VehicleCapacityCopyWith<$Res>(_self.capacity, (value) {
    return _then(_self.copyWith(capacity: value));
  });
}
}


/// Adds pattern-matching-related methods to [VehicleSummary].
extension VehicleSummaryPatterns on VehicleSummary {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _VehicleSummary value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _VehicleSummary() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _VehicleSummary value)  $default,){
final _that = this;
switch (_that) {
case _VehicleSummary():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _VehicleSummary value)?  $default,){
final _that = this;
switch (_that) {
case _VehicleSummary() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id,  String type,  String make,  String model,  VehicleCapacity capacity)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _VehicleSummary() when $default != null:
return $default(_that.id,_that.type,_that.make,_that.model,_that.capacity);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id,  String type,  String make,  String model,  VehicleCapacity capacity)  $default,) {final _that = this;
switch (_that) {
case _VehicleSummary():
return $default(_that.id,_that.type,_that.make,_that.model,_that.capacity);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id,  String type,  String make,  String model,  VehicleCapacity capacity)?  $default,) {final _that = this;
switch (_that) {
case _VehicleSummary() when $default != null:
return $default(_that.id,_that.type,_that.make,_that.model,_that.capacity);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _VehicleSummary extends VehicleSummary {
  const _VehicleSummary({required this.id, this.type = '', this.make = '', this.model = '', this.capacity = const VehicleCapacity()}): super._();
  factory _VehicleSummary.fromJson(Map<String, dynamic> json) => _$VehicleSummaryFromJson(json);

@override final  String id;
/// The kind of vehicle, as the provider registered it.
///
/// **A string rather than an enumeration**, unlike [BidStatus]. The list is `fleet`'s and is
/// not generated from `contracts/statuses.yaml`, so a client enumeration would be a second copy
/// of somebody else's vocabulary — and a vehicle type added on the platform would arrive as
/// `unknown` on every phone until the next store release. `Docs/07` §6 is explicit that
/// anything expected to change under operational pressure stays server-side.
@override@JsonKey() final  String type;
/// Omitted when the provider did not state one.
@override@JsonKey() final  String make;
@override@JsonKey() final  String model;
/// What it can carry, as declared — `Docs/01` §4.3's "declared capability".
@override@JsonKey() final  VehicleCapacity capacity;

/// Create a copy of VehicleSummary
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$VehicleSummaryCopyWith<_VehicleSummary> get copyWith => __$VehicleSummaryCopyWithImpl<_VehicleSummary>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$VehicleSummaryToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _VehicleSummary&&(identical(other.id, id) || other.id == id)&&(identical(other.type, type) || other.type == type)&&(identical(other.make, make) || other.make == make)&&(identical(other.model, model) || other.model == model)&&(identical(other.capacity, capacity) || other.capacity == capacity));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,type,make,model,capacity);

@override
String toString() {
  return 'VehicleSummary(id: $id, type: $type, make: $make, model: $model, capacity: $capacity)';
}


}

/// @nodoc
abstract mixin class _$VehicleSummaryCopyWith<$Res> implements $VehicleSummaryCopyWith<$Res> {
  factory _$VehicleSummaryCopyWith(_VehicleSummary value, $Res Function(_VehicleSummary) _then) = __$VehicleSummaryCopyWithImpl;
@override @useResult
$Res call({
 String id, String type, String make, String model, VehicleCapacity capacity
});


@override $VehicleCapacityCopyWith<$Res> get capacity;

}
/// @nodoc
class __$VehicleSummaryCopyWithImpl<$Res>
    implements _$VehicleSummaryCopyWith<$Res> {
  __$VehicleSummaryCopyWithImpl(this._self, this._then);

  final _VehicleSummary _self;
  final $Res Function(_VehicleSummary) _then;

/// Create a copy of VehicleSummary
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? type = null,Object? make = null,Object? model = null,Object? capacity = null,}) {
  return _then(_VehicleSummary(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,type: null == type ? _self.type : type // ignore: cast_nullable_to_non_nullable
as String,make: null == make ? _self.make : make // ignore: cast_nullable_to_non_nullable
as String,model: null == model ? _self.model : model // ignore: cast_nullable_to_non_nullable
as String,capacity: null == capacity ? _self.capacity : capacity // ignore: cast_nullable_to_non_nullable
as VehicleCapacity,
  ));
}

/// Create a copy of VehicleSummary
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$VehicleCapacityCopyWith<$Res> get capacity {
  
  return $VehicleCapacityCopyWith<$Res>(_self.capacity, (value) {
    return _then(_self.copyWith(capacity: value));
  });
}
}


/// @nodoc
mixin _$VehicleCapacity {

@JsonKey(name: 'max_weight_kg') double get maxWeightKg;@JsonKey(name: 'length_cm') int get lengthCm;@JsonKey(name: 'width_cm') int get widthCm;@JsonKey(name: 'height_cm') int get heightCm;
/// Create a copy of VehicleCapacity
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$VehicleCapacityCopyWith<VehicleCapacity> get copyWith => _$VehicleCapacityCopyWithImpl<VehicleCapacity>(this as VehicleCapacity, _$identity);

  /// Serializes this VehicleCapacity to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is VehicleCapacity&&(identical(other.maxWeightKg, maxWeightKg) || other.maxWeightKg == maxWeightKg)&&(identical(other.lengthCm, lengthCm) || other.lengthCm == lengthCm)&&(identical(other.widthCm, widthCm) || other.widthCm == widthCm)&&(identical(other.heightCm, heightCm) || other.heightCm == heightCm));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,maxWeightKg,lengthCm,widthCm,heightCm);

@override
String toString() {
  return 'VehicleCapacity(maxWeightKg: $maxWeightKg, lengthCm: $lengthCm, widthCm: $widthCm, heightCm: $heightCm)';
}


}

/// @nodoc
abstract mixin class $VehicleCapacityCopyWith<$Res>  {
  factory $VehicleCapacityCopyWith(VehicleCapacity value, $Res Function(VehicleCapacity) _then) = _$VehicleCapacityCopyWithImpl;
@useResult
$Res call({
@JsonKey(name: 'max_weight_kg') double maxWeightKg,@JsonKey(name: 'length_cm') int lengthCm,@JsonKey(name: 'width_cm') int widthCm,@JsonKey(name: 'height_cm') int heightCm
});




}
/// @nodoc
class _$VehicleCapacityCopyWithImpl<$Res>
    implements $VehicleCapacityCopyWith<$Res> {
  _$VehicleCapacityCopyWithImpl(this._self, this._then);

  final VehicleCapacity _self;
  final $Res Function(VehicleCapacity) _then;

/// Create a copy of VehicleCapacity
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? maxWeightKg = null,Object? lengthCm = null,Object? widthCm = null,Object? heightCm = null,}) {
  return _then(_self.copyWith(
maxWeightKg: null == maxWeightKg ? _self.maxWeightKg : maxWeightKg // ignore: cast_nullable_to_non_nullable
as double,lengthCm: null == lengthCm ? _self.lengthCm : lengthCm // ignore: cast_nullable_to_non_nullable
as int,widthCm: null == widthCm ? _self.widthCm : widthCm // ignore: cast_nullable_to_non_nullable
as int,heightCm: null == heightCm ? _self.heightCm : heightCm // ignore: cast_nullable_to_non_nullable
as int,
  ));
}

}


/// Adds pattern-matching-related methods to [VehicleCapacity].
extension VehicleCapacityPatterns on VehicleCapacity {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _VehicleCapacity value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _VehicleCapacity() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _VehicleCapacity value)  $default,){
final _that = this;
switch (_that) {
case _VehicleCapacity():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _VehicleCapacity value)?  $default,){
final _that = this;
switch (_that) {
case _VehicleCapacity() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function(@JsonKey(name: 'max_weight_kg')  double maxWeightKg, @JsonKey(name: 'length_cm')  int lengthCm, @JsonKey(name: 'width_cm')  int widthCm, @JsonKey(name: 'height_cm')  int heightCm)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _VehicleCapacity() when $default != null:
return $default(_that.maxWeightKg,_that.lengthCm,_that.widthCm,_that.heightCm);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function(@JsonKey(name: 'max_weight_kg')  double maxWeightKg, @JsonKey(name: 'length_cm')  int lengthCm, @JsonKey(name: 'width_cm')  int widthCm, @JsonKey(name: 'height_cm')  int heightCm)  $default,) {final _that = this;
switch (_that) {
case _VehicleCapacity():
return $default(_that.maxWeightKg,_that.lengthCm,_that.widthCm,_that.heightCm);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function(@JsonKey(name: 'max_weight_kg')  double maxWeightKg, @JsonKey(name: 'length_cm')  int lengthCm, @JsonKey(name: 'width_cm')  int widthCm, @JsonKey(name: 'height_cm')  int heightCm)?  $default,) {final _that = this;
switch (_that) {
case _VehicleCapacity() when $default != null:
return $default(_that.maxWeightKg,_that.lengthCm,_that.widthCm,_that.heightCm);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _VehicleCapacity extends VehicleCapacity {
  const _VehicleCapacity({@JsonKey(name: 'max_weight_kg') this.maxWeightKg = 0, @JsonKey(name: 'length_cm') this.lengthCm = 0, @JsonKey(name: 'width_cm') this.widthCm = 0, @JsonKey(name: 'height_cm') this.heightCm = 0}): super._();
  factory _VehicleCapacity.fromJson(Map<String, dynamic> json) => _$VehicleCapacityFromJson(json);

@override@JsonKey(name: 'max_weight_kg') final  double maxWeightKg;
@override@JsonKey(name: 'length_cm') final  int lengthCm;
@override@JsonKey(name: 'width_cm') final  int widthCm;
@override@JsonKey(name: 'height_cm') final  int heightCm;

/// Create a copy of VehicleCapacity
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$VehicleCapacityCopyWith<_VehicleCapacity> get copyWith => __$VehicleCapacityCopyWithImpl<_VehicleCapacity>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$VehicleCapacityToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _VehicleCapacity&&(identical(other.maxWeightKg, maxWeightKg) || other.maxWeightKg == maxWeightKg)&&(identical(other.lengthCm, lengthCm) || other.lengthCm == lengthCm)&&(identical(other.widthCm, widthCm) || other.widthCm == widthCm)&&(identical(other.heightCm, heightCm) || other.heightCm == heightCm));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,maxWeightKg,lengthCm,widthCm,heightCm);

@override
String toString() {
  return 'VehicleCapacity(maxWeightKg: $maxWeightKg, lengthCm: $lengthCm, widthCm: $widthCm, heightCm: $heightCm)';
}


}

/// @nodoc
abstract mixin class _$VehicleCapacityCopyWith<$Res> implements $VehicleCapacityCopyWith<$Res> {
  factory _$VehicleCapacityCopyWith(_VehicleCapacity value, $Res Function(_VehicleCapacity) _then) = __$VehicleCapacityCopyWithImpl;
@override @useResult
$Res call({
@JsonKey(name: 'max_weight_kg') double maxWeightKg,@JsonKey(name: 'length_cm') int lengthCm,@JsonKey(name: 'width_cm') int widthCm,@JsonKey(name: 'height_cm') int heightCm
});




}
/// @nodoc
class __$VehicleCapacityCopyWithImpl<$Res>
    implements _$VehicleCapacityCopyWith<$Res> {
  __$VehicleCapacityCopyWithImpl(this._self, this._then);

  final _VehicleCapacity _self;
  final $Res Function(_VehicleCapacity) _then;

/// Create a copy of VehicleCapacity
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? maxWeightKg = null,Object? lengthCm = null,Object? widthCm = null,Object? heightCm = null,}) {
  return _then(_VehicleCapacity(
maxWeightKg: null == maxWeightKg ? _self.maxWeightKg : maxWeightKg // ignore: cast_nullable_to_non_nullable
as double,lengthCm: null == lengthCm ? _self.lengthCm : lengthCm // ignore: cast_nullable_to_non_nullable
as int,widthCm: null == widthCm ? _self.widthCm : widthCm // ignore: cast_nullable_to_non_nullable
as int,heightCm: null == heightCm ? _self.heightCm : heightCm // ignore: cast_nullable_to_non_nullable
as int,
  ));
}


}

// dart format on
