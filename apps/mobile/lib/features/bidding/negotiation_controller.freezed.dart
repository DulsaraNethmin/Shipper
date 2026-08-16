// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'negotiation_controller.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;
/// @nodoc
mixin _$NegotiationState {

/// The negotiation is being read for the first time, or a retry after a failure is.
 bool get loading;/// Whether anything has arrived at all.
///
/// What separates "nobody has said anything yet" from "not yet asked". A negotiation opened a
/// second after the offer was placed has no messages and nothing is wrong.
 bool get loaded;/// Every offer in the chain, **oldest first**, exactly as `…/history` returns them.
///
/// Includes offers that were withdrawn and replaced, each at the amount it was made at.
/// Nothing is deleted and nothing is overwritten on the platform, so nothing is here either.
 List<Bid> get offers;/// The conversation, **oldest first** — a conversation is read forward or it is not one.
 List<Message> get messages;/// A further page of the conversation is being read.
 bool get loadingMore;/// The position to ask from next. **Opaque** — handed back exactly as it arrived.
 String? get nextCursor;/// Whether asking again would return anything. Only ever about the conversation: the offer
/// chain does not page.
 bool get hasMore;/// What the last read failed with, or `null`.
 ApiFailure? get failure;/// A message is on its way to the platform.
 bool get sending;/// What the last send failed with, or `null`.
 ApiFailure? get sendFailure;/// A counter-offer is on its way to the platform.
 bool get countering;/// What the last counter failed with, or `null`.
 ApiFailure? get counterFailure;
/// Create a copy of NegotiationState
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$NegotiationStateCopyWith<NegotiationState> get copyWith => _$NegotiationStateCopyWithImpl<NegotiationState>(this as NegotiationState, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is NegotiationState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&const DeepCollectionEquality().equals(other.offers, offers)&&const DeepCollectionEquality().equals(other.messages, messages)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.failure, failure) || other.failure == failure)&&(identical(other.sending, sending) || other.sending == sending)&&(identical(other.sendFailure, sendFailure) || other.sendFailure == sendFailure)&&(identical(other.countering, countering) || other.countering == countering)&&(identical(other.counterFailure, counterFailure) || other.counterFailure == counterFailure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loaded,const DeepCollectionEquality().hash(offers),const DeepCollectionEquality().hash(messages),loadingMore,nextCursor,hasMore,failure,sending,sendFailure,countering,counterFailure);

@override
String toString() {
  return 'NegotiationState(loading: $loading, loaded: $loaded, offers: $offers, messages: $messages, loadingMore: $loadingMore, nextCursor: $nextCursor, hasMore: $hasMore, failure: $failure, sending: $sending, sendFailure: $sendFailure, countering: $countering, counterFailure: $counterFailure)';
}


}

/// @nodoc
abstract mixin class $NegotiationStateCopyWith<$Res>  {
  factory $NegotiationStateCopyWith(NegotiationState value, $Res Function(NegotiationState) _then) = _$NegotiationStateCopyWithImpl;
@useResult
$Res call({
 bool loading, bool loaded, List<Bid> offers, List<Message> messages, bool loadingMore, String? nextCursor, bool hasMore, ApiFailure? failure, bool sending, ApiFailure? sendFailure, bool countering, ApiFailure? counterFailure
});




}
/// @nodoc
class _$NegotiationStateCopyWithImpl<$Res>
    implements $NegotiationStateCopyWith<$Res> {
  _$NegotiationStateCopyWithImpl(this._self, this._then);

  final NegotiationState _self;
  final $Res Function(NegotiationState) _then;

/// Create a copy of NegotiationState
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? loading = null,Object? loaded = null,Object? offers = null,Object? messages = null,Object? loadingMore = null,Object? nextCursor = freezed,Object? hasMore = null,Object? failure = freezed,Object? sending = null,Object? sendFailure = freezed,Object? countering = null,Object? counterFailure = freezed,}) {
  return _then(_self.copyWith(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,offers: null == offers ? _self.offers : offers // ignore: cast_nullable_to_non_nullable
as List<Bid>,messages: null == messages ? _self.messages : messages // ignore: cast_nullable_to_non_nullable
as List<Message>,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,sending: null == sending ? _self.sending : sending // ignore: cast_nullable_to_non_nullable
as bool,sendFailure: freezed == sendFailure ? _self.sendFailure : sendFailure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,countering: null == countering ? _self.countering : countering // ignore: cast_nullable_to_non_nullable
as bool,counterFailure: freezed == counterFailure ? _self.counterFailure : counterFailure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}

}


/// Adds pattern-matching-related methods to [NegotiationState].
extension NegotiationStatePatterns on NegotiationState {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _NegotiationState value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _NegotiationState() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _NegotiationState value)  $default,){
final _that = this;
switch (_that) {
case _NegotiationState():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _NegotiationState value)?  $default,){
final _that = this;
switch (_that) {
case _NegotiationState() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( bool loading,  bool loaded,  List<Bid> offers,  List<Message> messages,  bool loadingMore,  String? nextCursor,  bool hasMore,  ApiFailure? failure,  bool sending,  ApiFailure? sendFailure,  bool countering,  ApiFailure? counterFailure)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _NegotiationState() when $default != null:
return $default(_that.loading,_that.loaded,_that.offers,_that.messages,_that.loadingMore,_that.nextCursor,_that.hasMore,_that.failure,_that.sending,_that.sendFailure,_that.countering,_that.counterFailure);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( bool loading,  bool loaded,  List<Bid> offers,  List<Message> messages,  bool loadingMore,  String? nextCursor,  bool hasMore,  ApiFailure? failure,  bool sending,  ApiFailure? sendFailure,  bool countering,  ApiFailure? counterFailure)  $default,) {final _that = this;
switch (_that) {
case _NegotiationState():
return $default(_that.loading,_that.loaded,_that.offers,_that.messages,_that.loadingMore,_that.nextCursor,_that.hasMore,_that.failure,_that.sending,_that.sendFailure,_that.countering,_that.counterFailure);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( bool loading,  bool loaded,  List<Bid> offers,  List<Message> messages,  bool loadingMore,  String? nextCursor,  bool hasMore,  ApiFailure? failure,  bool sending,  ApiFailure? sendFailure,  bool countering,  ApiFailure? counterFailure)?  $default,) {final _that = this;
switch (_that) {
case _NegotiationState() when $default != null:
return $default(_that.loading,_that.loaded,_that.offers,_that.messages,_that.loadingMore,_that.nextCursor,_that.hasMore,_that.failure,_that.sending,_that.sendFailure,_that.countering,_that.counterFailure);case _:
  return null;

}
}

}

/// @nodoc


class _NegotiationState extends NegotiationState {
  const _NegotiationState({this.loading = true, this.loaded = false, final  List<Bid> offers = const <Bid>[], final  List<Message> messages = const <Message>[], this.loadingMore = false, this.nextCursor, this.hasMore = false, this.failure, this.sending = false, this.sendFailure, this.countering = false, this.counterFailure}): _offers = offers,_messages = messages,super._();
  

/// The negotiation is being read for the first time, or a retry after a failure is.
@override@JsonKey() final  bool loading;
/// Whether anything has arrived at all.
///
/// What separates "nobody has said anything yet" from "not yet asked". A negotiation opened a
/// second after the offer was placed has no messages and nothing is wrong.
@override@JsonKey() final  bool loaded;
/// Every offer in the chain, **oldest first**, exactly as `…/history` returns them.
///
/// Includes offers that were withdrawn and replaced, each at the amount it was made at.
/// Nothing is deleted and nothing is overwritten on the platform, so nothing is here either.
 final  List<Bid> _offers;
/// Every offer in the chain, **oldest first**, exactly as `…/history` returns them.
///
/// Includes offers that were withdrawn and replaced, each at the amount it was made at.
/// Nothing is deleted and nothing is overwritten on the platform, so nothing is here either.
@override@JsonKey() List<Bid> get offers {
  if (_offers is EqualUnmodifiableListView) return _offers;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_offers);
}

/// The conversation, **oldest first** — a conversation is read forward or it is not one.
 final  List<Message> _messages;
/// The conversation, **oldest first** — a conversation is read forward or it is not one.
@override@JsonKey() List<Message> get messages {
  if (_messages is EqualUnmodifiableListView) return _messages;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_messages);
}

/// A further page of the conversation is being read.
@override@JsonKey() final  bool loadingMore;
/// The position to ask from next. **Opaque** — handed back exactly as it arrived.
@override final  String? nextCursor;
/// Whether asking again would return anything. Only ever about the conversation: the offer
/// chain does not page.
@override@JsonKey() final  bool hasMore;
/// What the last read failed with, or `null`.
@override final  ApiFailure? failure;
/// A message is on its way to the platform.
@override@JsonKey() final  bool sending;
/// What the last send failed with, or `null`.
@override final  ApiFailure? sendFailure;
/// A counter-offer is on its way to the platform.
@override@JsonKey() final  bool countering;
/// What the last counter failed with, or `null`.
@override final  ApiFailure? counterFailure;

/// Create a copy of NegotiationState
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$NegotiationStateCopyWith<_NegotiationState> get copyWith => __$NegotiationStateCopyWithImpl<_NegotiationState>(this, _$identity);



@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _NegotiationState&&(identical(other.loading, loading) || other.loading == loading)&&(identical(other.loaded, loaded) || other.loaded == loaded)&&const DeepCollectionEquality().equals(other._offers, _offers)&&const DeepCollectionEquality().equals(other._messages, _messages)&&(identical(other.loadingMore, loadingMore) || other.loadingMore == loadingMore)&&(identical(other.nextCursor, nextCursor) || other.nextCursor == nextCursor)&&(identical(other.hasMore, hasMore) || other.hasMore == hasMore)&&(identical(other.failure, failure) || other.failure == failure)&&(identical(other.sending, sending) || other.sending == sending)&&(identical(other.sendFailure, sendFailure) || other.sendFailure == sendFailure)&&(identical(other.countering, countering) || other.countering == countering)&&(identical(other.counterFailure, counterFailure) || other.counterFailure == counterFailure));
}


@override
int get hashCode => Object.hash(runtimeType,loading,loaded,const DeepCollectionEquality().hash(_offers),const DeepCollectionEquality().hash(_messages),loadingMore,nextCursor,hasMore,failure,sending,sendFailure,countering,counterFailure);

@override
String toString() {
  return 'NegotiationState(loading: $loading, loaded: $loaded, offers: $offers, messages: $messages, loadingMore: $loadingMore, nextCursor: $nextCursor, hasMore: $hasMore, failure: $failure, sending: $sending, sendFailure: $sendFailure, countering: $countering, counterFailure: $counterFailure)';
}


}

/// @nodoc
abstract mixin class _$NegotiationStateCopyWith<$Res> implements $NegotiationStateCopyWith<$Res> {
  factory _$NegotiationStateCopyWith(_NegotiationState value, $Res Function(_NegotiationState) _then) = __$NegotiationStateCopyWithImpl;
@override @useResult
$Res call({
 bool loading, bool loaded, List<Bid> offers, List<Message> messages, bool loadingMore, String? nextCursor, bool hasMore, ApiFailure? failure, bool sending, ApiFailure? sendFailure, bool countering, ApiFailure? counterFailure
});




}
/// @nodoc
class __$NegotiationStateCopyWithImpl<$Res>
    implements _$NegotiationStateCopyWith<$Res> {
  __$NegotiationStateCopyWithImpl(this._self, this._then);

  final _NegotiationState _self;
  final $Res Function(_NegotiationState) _then;

/// Create a copy of NegotiationState
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? loading = null,Object? loaded = null,Object? offers = null,Object? messages = null,Object? loadingMore = null,Object? nextCursor = freezed,Object? hasMore = null,Object? failure = freezed,Object? sending = null,Object? sendFailure = freezed,Object? countering = null,Object? counterFailure = freezed,}) {
  return _then(_NegotiationState(
loading: null == loading ? _self.loading : loading // ignore: cast_nullable_to_non_nullable
as bool,loaded: null == loaded ? _self.loaded : loaded // ignore: cast_nullable_to_non_nullable
as bool,offers: null == offers ? _self._offers : offers // ignore: cast_nullable_to_non_nullable
as List<Bid>,messages: null == messages ? _self._messages : messages // ignore: cast_nullable_to_non_nullable
as List<Message>,loadingMore: null == loadingMore ? _self.loadingMore : loadingMore // ignore: cast_nullable_to_non_nullable
as bool,nextCursor: freezed == nextCursor ? _self.nextCursor : nextCursor // ignore: cast_nullable_to_non_nullable
as String?,hasMore: null == hasMore ? _self.hasMore : hasMore // ignore: cast_nullable_to_non_nullable
as bool,failure: freezed == failure ? _self.failure : failure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,sending: null == sending ? _self.sending : sending // ignore: cast_nullable_to_non_nullable
as bool,sendFailure: freezed == sendFailure ? _self.sendFailure : sendFailure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,countering: null == countering ? _self.countering : countering // ignore: cast_nullable_to_non_nullable
as bool,counterFailure: freezed == counterFailure ? _self.counterFailure : counterFailure // ignore: cast_nullable_to_non_nullable
as ApiFailure?,
  ));
}


}

// dart format on
