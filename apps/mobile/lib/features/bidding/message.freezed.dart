// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'message.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$Message {

 String get id;/// Which party wrote it.
///
/// **Required, and it is the one field on this shape that has to be.** `Docs/07` §6 makes a
/// required field a decode that *throws* on a build already on a handset, so only what the app
/// genuinely branches on earns one — and this is the whole of what a message list does. The
/// contract says the same thing from the other side: "this cannot be inferred — a conversation
/// does not alternate, either party may write twice in a row, and a reader may join it
/// halfway".
///
/// [BidParty] rather than an enumeration of its own. The vocabulary is `provider | customer`,
/// which is exactly what an offer's `offered_by` carries, and a second copy of two words is a
/// second thing to keep in step with `contracts/statuses.yaml` when a third party is ever named.
@JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown) BidParty get sentBy;/// What they wrote, as they wrote it.
///
/// Defaulted rather than required, unlike [sentBy], and the asymmetry is deliberate: a page
/// whose one malformed element threw would take the whole conversation with it. An empty bubble
/// is a worse message and a better failure.
 String get body;/// When the platform recorded it, in UTC with milliseconds.
@JsonKey(name: 'created_at') String? get createdAt;
/// Create a copy of Message
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$MessageCopyWith<Message> get copyWith => _$MessageCopyWithImpl<Message>(this as Message, _$identity);

  /// Serializes this Message to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is Message&&(identical(other.id, id) || other.id == id)&&(identical(other.sentBy, sentBy) || other.sentBy == sentBy)&&(identical(other.body, body) || other.body == body)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,sentBy,body,createdAt);

@override
String toString() {
  return 'Message(id: $id, sentBy: $sentBy, body: $body, createdAt: $createdAt)';
}


}

/// @nodoc
abstract mixin class $MessageCopyWith<$Res>  {
  factory $MessageCopyWith(Message value, $Res Function(Message) _then) = _$MessageCopyWithImpl;
@useResult
$Res call({
 String id,@JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown) BidParty sentBy, String body,@JsonKey(name: 'created_at') String? createdAt
});




}
/// @nodoc
class _$MessageCopyWithImpl<$Res>
    implements $MessageCopyWith<$Res> {
  _$MessageCopyWithImpl(this._self, this._then);

  final Message _self;
  final $Res Function(Message) _then;

/// Create a copy of Message
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? sentBy = null,Object? body = null,Object? createdAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,sentBy: null == sentBy ? _self.sentBy : sentBy // ignore: cast_nullable_to_non_nullable
as BidParty,body: null == body ? _self.body : body // ignore: cast_nullable_to_non_nullable
as String,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [Message].
extension MessagePatterns on Message {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _Message value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _Message() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _Message value)  $default,){
final _that = this;
switch (_that) {
case _Message():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _Message value)?  $default,){
final _that = this;
switch (_that) {
case _Message() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown)  BidParty sentBy,  String body, @JsonKey(name: 'created_at')  String? createdAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _Message() when $default != null:
return $default(_that.id,_that.sentBy,_that.body,_that.createdAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id, @JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown)  BidParty sentBy,  String body, @JsonKey(name: 'created_at')  String? createdAt)  $default,) {final _that = this;
switch (_that) {
case _Message():
return $default(_that.id,_that.sentBy,_that.body,_that.createdAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id, @JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown)  BidParty sentBy,  String body, @JsonKey(name: 'created_at')  String? createdAt)?  $default,) {final _that = this;
switch (_that) {
case _Message() when $default != null:
return $default(_that.id,_that.sentBy,_that.body,_that.createdAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _Message extends Message {
  const _Message({required this.id, @JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown) required this.sentBy, this.body = '', @JsonKey(name: 'created_at') this.createdAt}): super._();
  factory _Message.fromJson(Map<String, dynamic> json) => _$MessageFromJson(json);

@override final  String id;
/// Which party wrote it.
///
/// **Required, and it is the one field on this shape that has to be.** `Docs/07` §6 makes a
/// required field a decode that *throws* on a build already on a handset, so only what the app
/// genuinely branches on earns one — and this is the whole of what a message list does. The
/// contract says the same thing from the other side: "this cannot be inferred — a conversation
/// does not alternate, either party may write twice in a row, and a reader may join it
/// halfway".
///
/// [BidParty] rather than an enumeration of its own. The vocabulary is `provider | customer`,
/// which is exactly what an offer's `offered_by` carries, and a second copy of two words is a
/// second thing to keep in step with `contracts/statuses.yaml` when a third party is ever named.
@override@JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown) final  BidParty sentBy;
/// What they wrote, as they wrote it.
///
/// Defaulted rather than required, unlike [sentBy], and the asymmetry is deliberate: a page
/// whose one malformed element threw would take the whole conversation with it. An empty bubble
/// is a worse message and a better failure.
@override@JsonKey() final  String body;
/// When the platform recorded it, in UTC with milliseconds.
@override@JsonKey(name: 'created_at') final  String? createdAt;

/// Create a copy of Message
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$MessageCopyWith<_Message> get copyWith => __$MessageCopyWithImpl<_Message>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$MessageToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _Message&&(identical(other.id, id) || other.id == id)&&(identical(other.sentBy, sentBy) || other.sentBy == sentBy)&&(identical(other.body, body) || other.body == body)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,sentBy,body,createdAt);

@override
String toString() {
  return 'Message(id: $id, sentBy: $sentBy, body: $body, createdAt: $createdAt)';
}


}

/// @nodoc
abstract mixin class _$MessageCopyWith<$Res> implements $MessageCopyWith<$Res> {
  factory _$MessageCopyWith(_Message value, $Res Function(_Message) _then) = __$MessageCopyWithImpl;
@override @useResult
$Res call({
 String id,@JsonKey(name: 'sent_by', unknownEnumValue: BidParty.unknown) BidParty sentBy, String body,@JsonKey(name: 'created_at') String? createdAt
});




}
/// @nodoc
class __$MessageCopyWithImpl<$Res>
    implements _$MessageCopyWith<$Res> {
  __$MessageCopyWithImpl(this._self, this._then);

  final _Message _self;
  final $Res Function(_Message) _then;

/// Create a copy of Message
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? sentBy = null,Object? body = null,Object? createdAt = freezed,}) {
  return _then(_Message(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,sentBy: null == sentBy ? _self.sentBy : sentBy // ignore: cast_nullable_to_non_nullable
as BidParty,body: null == body ? _self.body : body // ignore: cast_nullable_to_non_nullable
as String,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
