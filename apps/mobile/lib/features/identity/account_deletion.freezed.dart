// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'account_deletion.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$AccountDeletion {

 String get id;/// `requested` or `deferred`.
///
/// A `String` rather than an enum, for the reason `Account.status` is one: SHIP-171 adds
/// `completed`, and a third value must not be a decode failure on a build already on a
/// handset. [deferred] is the one question this screen asks of it.
 String get state;/// When the person asked, as the platform recorded it. It does not move.
@JsonKey(name: 'requested_at') String get requestedAt;/// The date the platform will have finished by — **a promise only while [state] is
/// `requested`.**
///
/// On a deferred request it is the earliest the platform could finish, because the thirty
/// days start when the delivery closes and nobody knows yet when that is. The screen says so
/// rather than showing the same sentence in both states.
@JsonKey(name: 'completes_by') String get completesBy;/// Why the request is waiting, in the platform's own words — present only when it is.
///
/// **Rendered as given and never parsed.** It is policy copy (`Docs/05` §3.1 is a legal
/// position) and the platform holds it precisely so it can be corrected without a store
/// release; a client that reworded it would be shipping its own version of a legal sentence.
@JsonKey(name: 'deferral_reason') String? get deferralReason;
/// Create a copy of AccountDeletion
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$AccountDeletionCopyWith<AccountDeletion> get copyWith => _$AccountDeletionCopyWithImpl<AccountDeletion>(this as AccountDeletion, _$identity);

  /// Serializes this AccountDeletion to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is AccountDeletion&&(identical(other.id, id) || other.id == id)&&(identical(other.state, state) || other.state == state)&&(identical(other.requestedAt, requestedAt) || other.requestedAt == requestedAt)&&(identical(other.completesBy, completesBy) || other.completesBy == completesBy)&&(identical(other.deferralReason, deferralReason) || other.deferralReason == deferralReason));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,state,requestedAt,completesBy,deferralReason);

@override
String toString() {
  return 'AccountDeletion(id: $id, state: $state, requestedAt: $requestedAt, completesBy: $completesBy, deferralReason: $deferralReason)';
}


}

/// @nodoc
abstract mixin class $AccountDeletionCopyWith<$Res>  {
  factory $AccountDeletionCopyWith(AccountDeletion value, $Res Function(AccountDeletion) _then) = _$AccountDeletionCopyWithImpl;
@useResult
$Res call({
 String id, String state,@JsonKey(name: 'requested_at') String requestedAt,@JsonKey(name: 'completes_by') String completesBy,@JsonKey(name: 'deferral_reason') String? deferralReason
});




}
/// @nodoc
class _$AccountDeletionCopyWithImpl<$Res>
    implements $AccountDeletionCopyWith<$Res> {
  _$AccountDeletionCopyWithImpl(this._self, this._then);

  final AccountDeletion _self;
  final $Res Function(AccountDeletion) _then;

/// Create a copy of AccountDeletion
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? state = null,Object? requestedAt = null,Object? completesBy = null,Object? deferralReason = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,state: null == state ? _self.state : state // ignore: cast_nullable_to_non_nullable
as String,requestedAt: null == requestedAt ? _self.requestedAt : requestedAt // ignore: cast_nullable_to_non_nullable
as String,completesBy: null == completesBy ? _self.completesBy : completesBy // ignore: cast_nullable_to_non_nullable
as String,deferralReason: freezed == deferralReason ? _self.deferralReason : deferralReason // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [AccountDeletion].
extension AccountDeletionPatterns on AccountDeletion {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _AccountDeletion value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _AccountDeletion() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _AccountDeletion value)  $default,){
final _that = this;
switch (_that) {
case _AccountDeletion():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _AccountDeletion value)?  $default,){
final _that = this;
switch (_that) {
case _AccountDeletion() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id,  String state, @JsonKey(name: 'requested_at')  String requestedAt, @JsonKey(name: 'completes_by')  String completesBy, @JsonKey(name: 'deferral_reason')  String? deferralReason)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _AccountDeletion() when $default != null:
return $default(_that.id,_that.state,_that.requestedAt,_that.completesBy,_that.deferralReason);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id,  String state, @JsonKey(name: 'requested_at')  String requestedAt, @JsonKey(name: 'completes_by')  String completesBy, @JsonKey(name: 'deferral_reason')  String? deferralReason)  $default,) {final _that = this;
switch (_that) {
case _AccountDeletion():
return $default(_that.id,_that.state,_that.requestedAt,_that.completesBy,_that.deferralReason);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id,  String state, @JsonKey(name: 'requested_at')  String requestedAt, @JsonKey(name: 'completes_by')  String completesBy, @JsonKey(name: 'deferral_reason')  String? deferralReason)?  $default,) {final _that = this;
switch (_that) {
case _AccountDeletion() when $default != null:
return $default(_that.id,_that.state,_that.requestedAt,_that.completesBy,_that.deferralReason);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _AccountDeletion extends AccountDeletion {
  const _AccountDeletion({required this.id, required this.state, @JsonKey(name: 'requested_at') required this.requestedAt, @JsonKey(name: 'completes_by') required this.completesBy, @JsonKey(name: 'deferral_reason') this.deferralReason}): super._();
  factory _AccountDeletion.fromJson(Map<String, dynamic> json) => _$AccountDeletionFromJson(json);

@override final  String id;
/// `requested` or `deferred`.
///
/// A `String` rather than an enum, for the reason `Account.status` is one: SHIP-171 adds
/// `completed`, and a third value must not be a decode failure on a build already on a
/// handset. [deferred] is the one question this screen asks of it.
@override final  String state;
/// When the person asked, as the platform recorded it. It does not move.
@override@JsonKey(name: 'requested_at') final  String requestedAt;
/// The date the platform will have finished by — **a promise only while [state] is
/// `requested`.**
///
/// On a deferred request it is the earliest the platform could finish, because the thirty
/// days start when the delivery closes and nobody knows yet when that is. The screen says so
/// rather than showing the same sentence in both states.
@override@JsonKey(name: 'completes_by') final  String completesBy;
/// Why the request is waiting, in the platform's own words — present only when it is.
///
/// **Rendered as given and never parsed.** It is policy copy (`Docs/05` §3.1 is a legal
/// position) and the platform holds it precisely so it can be corrected without a store
/// release; a client that reworded it would be shipping its own version of a legal sentence.
@override@JsonKey(name: 'deferral_reason') final  String? deferralReason;

/// Create a copy of AccountDeletion
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$AccountDeletionCopyWith<_AccountDeletion> get copyWith => __$AccountDeletionCopyWithImpl<_AccountDeletion>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$AccountDeletionToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _AccountDeletion&&(identical(other.id, id) || other.id == id)&&(identical(other.state, state) || other.state == state)&&(identical(other.requestedAt, requestedAt) || other.requestedAt == requestedAt)&&(identical(other.completesBy, completesBy) || other.completesBy == completesBy)&&(identical(other.deferralReason, deferralReason) || other.deferralReason == deferralReason));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,state,requestedAt,completesBy,deferralReason);

@override
String toString() {
  return 'AccountDeletion(id: $id, state: $state, requestedAt: $requestedAt, completesBy: $completesBy, deferralReason: $deferralReason)';
}


}

/// @nodoc
abstract mixin class _$AccountDeletionCopyWith<$Res> implements $AccountDeletionCopyWith<$Res> {
  factory _$AccountDeletionCopyWith(_AccountDeletion value, $Res Function(_AccountDeletion) _then) = __$AccountDeletionCopyWithImpl;
@override @useResult
$Res call({
 String id, String state,@JsonKey(name: 'requested_at') String requestedAt,@JsonKey(name: 'completes_by') String completesBy,@JsonKey(name: 'deferral_reason') String? deferralReason
});




}
/// @nodoc
class __$AccountDeletionCopyWithImpl<$Res>
    implements _$AccountDeletionCopyWith<$Res> {
  __$AccountDeletionCopyWithImpl(this._self, this._then);

  final _AccountDeletion _self;
  final $Res Function(_AccountDeletion) _then;

/// Create a copy of AccountDeletion
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? state = null,Object? requestedAt = null,Object? completesBy = null,Object? deferralReason = freezed,}) {
  return _then(_AccountDeletion(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,state: null == state ? _self.state : state // ignore: cast_nullable_to_non_nullable
as String,requestedAt: null == requestedAt ? _self.requestedAt : requestedAt // ignore: cast_nullable_to_non_nullable
as String,completesBy: null == completesBy ? _self.completesBy : completesBy // ignore: cast_nullable_to_non_nullable
as String,deferralReason: freezed == deferralReason ? _self.deferralReason : deferralReason // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
