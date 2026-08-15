// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'compare_offers_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$CompareOffersState {

/// The **first** page is being read, or a retry after a failure is.
///
/// Not set by a pull-to-refresh, for `MyBidsState`'s reason: the `RefreshIndicator` draws its
/// own spinner and replacing the list with a second one takes the offers away from somebody who
/// pulled precisely to look at them.
 bool get loading;/// A further page is being read.
 bool get loadingMore;/// Every offer read so far, in the platform's own order.
 List<ReceivedOffer> get offers;/// Whether a page has arrived at all.
///
/// What separates "nobody has offered yet" from "not yet asked". A customer whose job was
/// published a minute ago must see an empty state and never a spinner that does not resolve.
 bool get loaded;/// The position to ask from next. **Opaque** — passed back exactly as it arrived.
 String? get nextCursor;/// Whether asking again would return anything.
 bool get hasMore;/// How the customer wants them ordered.
 OfferOrder get order;/// Which offers are being read, or `null` for the platform's own default (SHIP-104).
///
/// `null` while the customer is comparing, which the endpoint reads as `submitted` — the offers
/// standing right now. It becomes [BidStatus.accepted] after an award, because that is the only
/// question worth asking then: the award rejected every other live offer in the same
/// transaction, so the default list would come back **empty** and the screen would draw
/// "nobody has offered yet" over a delivery that has just been awarded.
///
/// The contract names this as the way back in — "`?status=accepted` is how the awarded offer is
/// read back afterwards" — so this is reading the platform's record rather than reasoning
/// locally about what the award did to the list this device is holding.
 BidStatus? get status;/// What the last read failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of CompareOffersState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$CompareOffersStateCopyWith<CompareOffersState> get copyWith => _$CompareOffersStateCopyWithImpl<CompareOffersState>(this as CompareOffersState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is CompareOffersState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&const DeepCollectionEquality().equals(other.offers, offers)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.order, order) || other.order == order)&&(identical(other.status, status) || other.status == status)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,const DeepCollectionEquality().hash(offers),loaded,nextCursor,hasMore,order,status,failure);

@override
String toString() {
  return 'CompareOffersState(loading: $loading, loadingMore: $loadingMore, offers: $offers, loaded: $loaded, nextCursor: $nextCursor, hasMore: $hasMore, order: $order, status: $status, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $CompareOffersStateCopyWith<$Res>  {
  factory $CompareOffersStateCopyWith(CompareOffersState value, $Res Function(CompareOffersState) _then) = _$CompareOffersStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool loadingMore, List<ReceivedOffer> offers, bool loaded, String? nextCursor, bool hasMore, OfferOrder order, BidStatus? status, ApiFailure? failure
});




}
/// @nodoc
class _$CompareOffersStateCopyWithImpl<$Res>
    implements $CompareOffersStateCopyWith<$Res> {
  _$CompareOffersStateCopyWithImpl(this._self, this._then);

  final CompareOffersState _self;
  final $Res Function(CompareOffersState) _then;

/// Create a copy of CompareOffersState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? loadingMore = null,Object? offers = null,Object? loaded = null,Object? nextCursor = freezed,Object? hasMore = null,Object? order = null,Object? status = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,offers: null == offers ? _self.offers : offers // ignore: cast_nullable_to_non_nullable
as List<ReceivedOffer>,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,order: null == order ? _self.order : order // ignore: cast_nullable_to_non_nullable
as OfferOrder,status: freezed == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as BidStatus?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

}


/// Adds pattern-matching-related methods to [CompareOffersState].
extension CompareOffersStatePatterns on CompareOffersState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _CompareOffersState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _CompareOffersState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _CompareOffersState value)  $default,){
final _that = this;
switch (_that) {
case _CompareOffersState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _CompareOffersState value)?  $default,){
final _that = this;
switch (_that) {
case _CompareOffersState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  List<ReceivedOffer> offers,  bool loaded,  String? nextCursor,  bool hasMore,  OfferOrder order,  BidStatus? status,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _CompareOffersState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.offers,_that.loaded,_that.nextCursor,_that.hasMore,_that.order,_that.status,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  List<ReceivedOffer> offers,  bool loaded,  String? nextCursor,  bool hasMore,  OfferOrder order,  BidStatus? status,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _CompareOffersState():
return $default(_that.loading,_that.loadingMore,_that.offers,_that.loaded,_that.nextCursor,_that.hasMore,_that.order,_that.status,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool loadingMore,  List<ReceivedOffer> offers,  bool loaded,  String? nextCursor,  bool hasMore,  OfferOrder order,  BidStatus? status,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _CompareOffersState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.offers,_that.loaded,_that.nextCursor,_that.hasMore,_that.order,_that.status,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _CompareOffersState extends CompareOffersState {
  const _CompareOffersState({this.loading = true, this.loadingMore = false, final  List<ReceivedOffer> offers = const <ReceivedOffer>[], this.loaded = false, this.nextCursor, this.hasMore = false, this.order = OfferOrder.cheapest, this.status, this.failure}): _offers = offers,super._();
  

/// The **first** page is being read, or a retry after a failure is.
///
/// Not set by a pull-to-refresh, for `MyBidsState`'s reason: the `RefreshIndicator` draws its
/// own spinner and replacing the list with a second one takes the offers away from somebody who
/// pulled precisely to look at them.
@override@JsonKey() final  bool loading;
/// A further page is being read.
@override@JsonKey() final  bool loadingMore;
/// Every offer read so far, in the platform's own order.
 final  List<ReceivedOffer> _offers;
/// Every offer read so far, in the platform's own order.
@override@JsonKey() List<ReceivedOffer> get offers {
  if (_offers is EqualUnmodifiableListView) return _offers;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_offers);
}

/// Whether a page has arrived at all.
///
/// What separates "nobody has offered yet" from "not yet asked". A customer whose job was
/// published a minute ago must see an empty state and never a spinner that does not resolve.
@override@JsonKey() final  bool loaded;
/// The position to ask from next. **Opaque** — passed back exactly as it arrived.
@override final  String? nextCursor;
/// Whether asking again would return anything.
@override@JsonKey() final  bool hasMore;
/// How the customer wants them ordered.
@override@JsonKey() final  OfferOrder order;
/// Which offers are being read, or `null` for the platform's own default (SHIP-104).
///
/// `null` while the customer is comparing, which the endpoint reads as `submitted` — the offers
/// standing right now. It becomes [BidStatus.accepted] after an award, because that is the only
/// question worth asking then: the award rejected every other live offer in the same
/// transaction, so the default list would come back **empty** and the screen would draw
/// "nobody has offered yet" over a delivery that has just been awarded.
///
/// The contract names this as the way back in — "`?status=accepted` is how the awarded offer is
/// read back afterwards" — so this is reading the platform's record rather than reasoning
/// locally about what the award did to the list this device is holding.
@override final  BidStatus? status;
/// What the last read failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of CompareOffersState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$CompareOffersStateCopyWith<_CompareOffersState> get copyWith => __$CompareOffersStateCopyWithImpl<_CompareOffersState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _CompareOffersState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&const DeepCollectionEquality().equals(other._offers, _offers)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.order, order) || other.order == order)&&(identical(other.status, status) || other.status == status)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,const DeepCollectionEquality().hash(_offers),loaded,nextCursor,hasMore,order,status,failure);

@override
String toString() {
  return 'CompareOffersState(loading: $loading, loadingMore: $loadingMore, offers: $offers, loaded: $loaded, nextCursor: $nextCursor, hasMore: $hasMore, order: $order, status: $status, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$CompareOffersStateCopyWith<$Res> implements $CompareOffersStateCopyWith<$Res> {
  factory _$CompareOffersStateCopyWith(_CompareOffersState value, $Res Function(_CompareOffersState) _then) = __$CompareOffersStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool loadingMore, List<ReceivedOffer> offers, bool loaded, String? nextCursor, bool hasMore, OfferOrder order, BidStatus? status, ApiFailure? failure
});




}
/// @nodoc
class __$CompareOffersStateCopyWithImpl<$Res>
    implements _$CompareOffersStateCopyWith<$Res> {
  __$CompareOffersStateCopyWithImpl(this._self, this._then);

  final _CompareOffersState _self;
  final $Res Function(_CompareOffersState) _then;

/// Create a copy of CompareOffersState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? loadingMore = null,Object? offers = null,Object? loaded = null,Object? nextCursor = freezed,Object? hasMore = null,Object? order = null,Object? status = freezed,Object? failure = freezed,}) {
  return _then(_CompareOffersState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,offers: null == offers ? _self._offers : offers // ignore: cast_nullable_to_non_nullable
as List<ReceivedOffer>,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,order: null == order ? _self.order : order // ignore: cast_nullable_to_non_nullable
as OfferOrder,status: freezed == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as BidStatus?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}


}

// dart format on
