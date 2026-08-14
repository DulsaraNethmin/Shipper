// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'my_bids_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$MyBidsState {

/// The **first** page is being read, or a retry after a failure is.
///
/// Deliberately not set by a pull-to-refresh, for the reason `OpenJobsState` gives: the
/// `RefreshIndicator` draws its own spinner, and replacing the list with a second one takes the
/// work away from somebody who pulled precisely to look at it.
 bool get loading;/// A further page is being read.
 bool get loadingMore;/// Every offer read so far, newest first, in the platform's own order.
 List<Bid> get bids;/// Whether a page has arrived at all.
///
/// What separates "you have not bid on anything" from "not yet asked". A provider with no
/// offers must see an empty state and never a spinner that does not resolve.
 bool get loaded;/// The position to ask from next. **Opaque** — passed back exactly as it arrived.
 String? get nextCursor;/// Whether asking again would return anything.
 bool get hasMore;/// The one group the provider asked the platform for, or `null` for every status.
///
/// **State of the request, unlike `OpenJobsFilter`**, and that difference is the whole reason
/// this field exists rather than a client-side predicate. `GET /v1/jobs/open` accepts no filter
/// at all, so a provider narrowing their feed can only hide what was read; `GET /v1/fleet/bids`
/// accepts `?status=` and runs it in SQL, so asking for one group asks a different question and
/// gets a page that is whole rather than a page with holes in it.
 BidStatus? get only;/// What the last read failed with, or `null`.
 ApiFailure? get failure;
/// Create a copy of MyBidsState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$MyBidsStateCopyWith<MyBidsState> get copyWith => _$MyBidsStateCopyWithImpl<MyBidsState>(this as MyBidsState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is MyBidsState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&const DeepCollectionEquality().equals(other.bids, bids)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.only, only) || other.only == only)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,const DeepCollectionEquality().hash(bids),loaded,nextCursor,hasMore,only,failure);

@override
String toString() {
  return 'MyBidsState(loading: $loading, loadingMore: $loadingMore, bids: $bids, loaded: $loaded, nextCursor: $nextCursor, hasMore: $hasMore, only: $only, failure: $failure)';
}


}

/// @nodoc
abstract mixin class $MyBidsStateCopyWith<$Res>  {
  factory $MyBidsStateCopyWith(MyBidsState value, $Res Function(MyBidsState) _then) = _$MyBidsStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool loadingMore, List<Bid> bids, bool loaded, String? nextCursor, bool hasMore, BidStatus? only, ApiFailure? failure
});




}
/// @nodoc
class _$MyBidsStateCopyWithImpl<$Res>
    implements $MyBidsStateCopyWith<$Res> {
  _$MyBidsStateCopyWithImpl(this._self, this._then);

  final MyBidsState _self;
  final $Res Function(MyBidsState) _then;

/// Create a copy of MyBidsState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? loadingMore = null,Object? bids = null,Object? loaded = null,Object? nextCursor = freezed,Object? hasMore = null,Object? only = freezed,Object? failure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,bids: null == bids ? _self.bids : bids // ignore: cast_nullable_to_non_nullable
as List<Bid>,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,only: freezed == only ? _self.only : only // ignore: cast_nullable_to_non_nullable
as BidStatus?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

}


/// Adds pattern-matching-related methods to [MyBidsState].
extension MyBidsStatePatterns on MyBidsState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _MyBidsState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _MyBidsState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _MyBidsState value)  $default,){
final _that = this;
switch (_that) {
case _MyBidsState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _MyBidsState value)?  $default,){
final _that = this;
switch (_that) {
case _MyBidsState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  List<Bid> bids,  bool loaded,  String? nextCursor,  bool hasMore,  BidStatus? only,  ApiFailure? failure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _MyBidsState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.bids,_that.loaded,_that.nextCursor,_that.hasMore,_that.only,_that.failure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool loadingMore,  List<Bid> bids,  bool loaded,  String? nextCursor,  bool hasMore,  BidStatus? only,  ApiFailure? failure)  $default,) {final _that = this;
switch (_that) {
case _MyBidsState():
return $default(_that.loading,_that.loadingMore,_that.bids,_that.loaded,_that.nextCursor,_that.hasMore,_that.only,_that.failure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool loadingMore,  List<Bid> bids,  bool loaded,  String? nextCursor,  bool hasMore,  BidStatus? only,  ApiFailure? failure)?  $default,) {final _that = this;
switch (_that) {
case _MyBidsState() when $default != null:
return $default(_that.loading,_that.loadingMore,_that.bids,_that.loaded,_that.nextCursor,_that.hasMore,_that.only,_that.failure);case _:
  return null;

}
}

}

/// @nodoc


class _MyBidsState extends MyBidsState {
  const _MyBidsState({this.loading = true, this.loadingMore = false, final  List<Bid> bids = const <Bid>[], this.loaded = false, this.nextCursor, this.hasMore = false, this.only, this.failure}): _bids = bids,super._();
  

/// The **first** page is being read, or a retry after a failure is.
///
/// Deliberately not set by a pull-to-refresh, for the reason `OpenJobsState` gives: the
/// `RefreshIndicator` draws its own spinner, and replacing the list with a second one takes the
/// work away from somebody who pulled precisely to look at it.
@override@JsonKey() final  bool loading;
/// A further page is being read.
@override@JsonKey() final  bool loadingMore;
/// Every offer read so far, newest first, in the platform's own order.
 final  List<Bid> _bids;
/// Every offer read so far, newest first, in the platform's own order.
@override@JsonKey() List<Bid> get bids {
  if (_bids is EqualUnmodifiableListView) return _bids;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_bids);
}

/// Whether a page has arrived at all.
///
/// What separates "you have not bid on anything" from "not yet asked". A provider with no
/// offers must see an empty state and never a spinner that does not resolve.
@override@JsonKey() final  bool loaded;
/// The position to ask from next. **Opaque** — passed back exactly as it arrived.
@override final  String? nextCursor;
/// Whether asking again would return anything.
@override@JsonKey() final  bool hasMore;
/// The one group the provider asked the platform for, or `null` for every status.
///
/// **State of the request, unlike `OpenJobsFilter`**, and that difference is the whole reason
/// this field exists rather than a client-side predicate. `GET /v1/jobs/open` accepts no filter
/// at all, so a provider narrowing their feed can only hide what was read; `GET /v1/fleet/bids`
/// accepts `?status=` and runs it in SQL, so asking for one group asks a different question and
/// gets a page that is whole rather than a page with holes in it.
@override final  BidStatus? only;
/// What the last read failed with, or `null`.
@override final  ApiFailure? failure;

/// Create a copy of MyBidsState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$MyBidsStateCopyWith<_MyBidsState> get copyWith => __$MyBidsStateCopyWithImpl<_MyBidsState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _MyBidsState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&const DeepCollectionEquality().equals(other._bids, _bids)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.only, only) || other.only == only)&&(identical(other.failure, failure) || other.failure == failure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loadingMore,const DeepCollectionEquality().hash(_bids),loaded,nextCursor,hasMore,only,failure);

@override
String toString() {
  return 'MyBidsState(loading: $loading, loadingMore: $loadingMore, bids: $bids, loaded: $loaded, nextCursor: $nextCursor, hasMore: $hasMore, only: $only, failure: $failure)';
}


}

/// @nodoc
abstract mixin class _$MyBidsStateCopyWith<$Res> implements $MyBidsStateCopyWith<$Res> {
  factory _$MyBidsStateCopyWith(_MyBidsState value, $Res Function(_MyBidsState) _then) = __$MyBidsStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool loadingMore, List<Bid> bids, bool loaded, String? nextCursor, bool hasMore, BidStatus? only, ApiFailure? failure
});




}
/// @nodoc
class __$MyBidsStateCopyWithImpl<$Res>
    implements _$MyBidsStateCopyWith<$Res> {
  __$MyBidsStateCopyWithImpl(this._self, this._then);

  final _MyBidsState _self;
  final $Res Function(_MyBidsState) _then;

/// Create a copy of MyBidsState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? loadingMore = null,Object? bids = null,Object? loaded = null,Object? nextCursor = freezed,Object? hasMore = null,Object? only = freezed,Object? failure = freezed,}) {
  return _then(_MyBidsState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,bids: null == bids ? _self._bids : bids // ignore: cast_nullable_to_non_nullable
as List<Bid>,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,only: freezed == only ? _self.only : only // ignore: cast_nullable_to_non_nullable
as BidStatus?,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}


}

// dart format on
