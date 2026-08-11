// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'account.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$Account {

 String get id; String get email;/// E.164, as the platform normalised it on the way in. Shown back to the person so they can
/// see which number the code was sent to.
 String get phone;/// Fixed at registration and immutable afterwards, enforced by a database trigger
/// (SHIP-45). An unrecognised value decodes to [UserRole.unknown] rather than throwing —
/// see the note there.
@JsonKey(unknownEnumValue: UserRole.unknown) UserRole get role;@JsonKey(name: 'email_verified') bool get emailVerified;@JsonKey(name: 'phone_verified') bool get phoneVerified;/// `active`, `restricted` or `suspended` — whether the account may be used at all.
///
/// A `String` rather than an enum because nothing in the client acts on it yet, and a fourth
/// value must not be a decode failure. **It is not provider verification**, which is a
/// separate eligibility decision with five states of its own (`Docs/04` §4).
 String? get status;@JsonKey(name: 'created_at') String? get createdAt;
/// Create a copy of Account
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$AccountCopyWith<Account> get copyWith => _$AccountCopyWithImpl<Account>(this as Account, _$identity);

  /// Serializes this Account to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is Account&&(identical(other.id, id) || other.id == id)&&(identical(other.email, email) || other.email == email)&&(identical(other.phone, phone) || other.phone == phone)&&(identical(other.role, role) || other.role == role)&&(identical(other.emailVerified, emailVerified) || other.emailVerified == emailVerified)&&(identical(other.phoneVerified, phoneVerified) || other.phoneVerified == phoneVerified)&&(identical(other.status, status) || other.status == status)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,email,phone,role,emailVerified,phoneVerified,status,createdAt);

@override
String toString() {
  return 'Account(id: $id, email: $email, phone: $phone, role: $role, emailVerified: $emailVerified, phoneVerified: $phoneVerified, status: $status, createdAt: $createdAt)';
}


}

/// @nodoc
abstract mixin class $AccountCopyWith<$Res>  {
  factory $AccountCopyWith(Account value, $Res Function(Account) _then) = _$AccountCopyWithImpl;
@useResult
$Res call({
 String id, String email, String phone,@JsonKey(unknownEnumValue: UserRole.unknown) UserRole role,@JsonKey(name: 'email_verified') bool emailVerified,@JsonKey(name: 'phone_verified') bool phoneVerified, String? status,@JsonKey(name: 'created_at') String? createdAt
});




}
/// @nodoc
class _$AccountCopyWithImpl<$Res>
    implements $AccountCopyWith<$Res> {
  _$AccountCopyWithImpl(this._self, this._then);

  final Account _self;
  final $Res Function(Account) _then;

/// Create a copy of Account
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? email = null,Object? phone = null,Object? role = null,Object? emailVerified = null,Object? phoneVerified = null,Object? status = freezed,Object? createdAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,email: null == email ? _self.email : email // ignore: cast_nullable_to_non_nullable
as String,phone: null == phone ? _self.phone : phone // ignore: cast_nullable_to_non_nullable
as String,role: null == role ? _self.role : role // ignore: cast_nullable_to_non_nullable
as UserRole,emailVerified: null == emailVerified ? _self.emailVerified : emailVerified // ignore: cast_nullable_to_non_nullable
as bool,phoneVerified: null == phoneVerified ? _self.phoneVerified : phoneVerified // ignore: cast_nullable_to_non_nullable
as bool,status: freezed == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [Account].
extension AccountPatterns on Account {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _Account value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _Account() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _Account value)  $default,){
final _that = this;
switch (_that) {
case _Account():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _Account value)?  $default,){
final _that = this;
switch (_that) {
case _Account() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id,  String email,  String phone, @JsonKey(unknownEnumValue: UserRole.unknown)  UserRole role, @JsonKey(name: 'email_verified')  bool emailVerified, @JsonKey(name: 'phone_verified')  bool phoneVerified,  String? status, @JsonKey(name: 'created_at')  String? createdAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _Account() when $default != null:
return $default(_that.id,_that.email,_that.phone,_that.role,_that.emailVerified,_that.phoneVerified,_that.status,_that.createdAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id,  String email,  String phone, @JsonKey(unknownEnumValue: UserRole.unknown)  UserRole role, @JsonKey(name: 'email_verified')  bool emailVerified, @JsonKey(name: 'phone_verified')  bool phoneVerified,  String? status, @JsonKey(name: 'created_at')  String? createdAt)  $default,) {final _that = this;
switch (_that) {
case _Account():
return $default(_that.id,_that.email,_that.phone,_that.role,_that.emailVerified,_that.phoneVerified,_that.status,_that.createdAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id,  String email,  String phone, @JsonKey(unknownEnumValue: UserRole.unknown)  UserRole role, @JsonKey(name: 'email_verified')  bool emailVerified, @JsonKey(name: 'phone_verified')  bool phoneVerified,  String? status, @JsonKey(name: 'created_at')  String? createdAt)?  $default,) {final _that = this;
switch (_that) {
case _Account() when $default != null:
return $default(_that.id,_that.email,_that.phone,_that.role,_that.emailVerified,_that.phoneVerified,_that.status,_that.createdAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _Account implements Account {
  const _Account({required this.id, required this.email, required this.phone, @JsonKey(unknownEnumValue: UserRole.unknown) required this.role, @JsonKey(name: 'email_verified') required this.emailVerified, @JsonKey(name: 'phone_verified') required this.phoneVerified, this.status, @JsonKey(name: 'created_at') this.createdAt});
  factory _Account.fromJson(Map<String, dynamic> json) => _$AccountFromJson(json);

@override final  String id;
@override final  String email;
/// E.164, as the platform normalised it on the way in. Shown back to the person so they can
/// see which number the code was sent to.
@override final  String phone;
/// Fixed at registration and immutable afterwards, enforced by a database trigger
/// (SHIP-45). An unrecognised value decodes to [UserRole.unknown] rather than throwing —
/// see the note there.
@override@JsonKey(unknownEnumValue: UserRole.unknown) final  UserRole role;
@override@JsonKey(name: 'email_verified') final  bool emailVerified;
@override@JsonKey(name: 'phone_verified') final  bool phoneVerified;
/// `active`, `restricted` or `suspended` — whether the account may be used at all.
///
/// A `String` rather than an enum because nothing in the client acts on it yet, and a fourth
/// value must not be a decode failure. **It is not provider verification**, which is a
/// separate eligibility decision with five states of its own (`Docs/04` §4).
@override final  String? status;
@override@JsonKey(name: 'created_at') final  String? createdAt;

/// Create a copy of Account
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$AccountCopyWith<_Account> get copyWith => __$AccountCopyWithImpl<_Account>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$AccountToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _Account&&(identical(other.id, id) || other.id == id)&&(identical(other.email, email) || other.email == email)&&(identical(other.phone, phone) || other.phone == phone)&&(identical(other.role, role) || other.role == role)&&(identical(other.emailVerified, emailVerified) || other.emailVerified == emailVerified)&&(identical(other.phoneVerified, phoneVerified) || other.phoneVerified == phoneVerified)&&(identical(other.status, status) || other.status == status)&&(identical(other.createdAt, createdAt) || other.createdAt == createdAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,email,phone,role,emailVerified,phoneVerified,status,createdAt);

@override
String toString() {
  return 'Account(id: $id, email: $email, phone: $phone, role: $role, emailVerified: $emailVerified, phoneVerified: $phoneVerified, status: $status, createdAt: $createdAt)';
}


}

/// @nodoc
abstract mixin class _$AccountCopyWith<$Res> implements $AccountCopyWith<$Res> {
  factory _$AccountCopyWith(_Account value, $Res Function(_Account) _then) = __$AccountCopyWithImpl;
@override @useResult
$Res call({
 String id, String email, String phone,@JsonKey(unknownEnumValue: UserRole.unknown) UserRole role,@JsonKey(name: 'email_verified') bool emailVerified,@JsonKey(name: 'phone_verified') bool phoneVerified, String? status,@JsonKey(name: 'created_at') String? createdAt
});




}
/// @nodoc
class __$AccountCopyWithImpl<$Res>
    implements _$AccountCopyWith<$Res> {
  __$AccountCopyWithImpl(this._self, this._then);

  final _Account _self;
  final $Res Function(_Account) _then;

/// Create a copy of Account
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? email = null,Object? phone = null,Object? role = null,Object? emailVerified = null,Object? phoneVerified = null,Object? status = freezed,Object? createdAt = freezed,}) {
  return _then(_Account(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,email: null == email ? _self.email : email // ignore: cast_nullable_to_non_nullable
as String,phone: null == phone ? _self.phone : phone // ignore: cast_nullable_to_non_nullable
as String,role: null == role ? _self.role : role // ignore: cast_nullable_to_non_nullable
as UserRole,emailVerified: null == emailVerified ? _self.emailVerified : emailVerified // ignore: cast_nullable_to_non_nullable
as bool,phoneVerified: null == phoneVerified ? _self.phoneVerified : phoneVerified // ignore: cast_nullable_to_non_nullable
as bool,status: freezed == status ? _self.status : status // ignore: cast_nullable_to_non_nullable
as String?,createdAt: freezed == createdAt ? _self.createdAt : createdAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
