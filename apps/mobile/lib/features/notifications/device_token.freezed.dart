// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'device_token.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$DeviceToken implements DiagnosticableTreeMixin {

/// The registration.
 String get id;/// Which store the registered build came from.
///
/// Nullable and defaulted rather than required, the same call `PlatformFloor.minimumBuild`
/// makes: a required field is a decode that **throws**, on a build already on a phone that
/// cannot be fixed over the air, and nothing branches on this.
 String? get platform;/// When the platform recorded this registration, in UTC.
@JsonKey(name: 'registered_at') String? get registeredAt;
/// Create a copy of DeviceToken
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$DeviceTokenCopyWith<DeviceToken> get copyWith => _$DeviceTokenCopyWithImpl<DeviceToken>(this as DeviceToken, _$identity);

  /// Serializes this DeviceToken to a JSON map.
  Map<String, dynamic> toJson();

@override
void debugFillProperties(DiagnosticPropertiesBuilder properties) {
  properties
    ..add(DiagnosticsProperty('type', 'DeviceToken'))
    ..add(DiagnosticsProperty('id', id))..add(DiagnosticsProperty('platform', platform))..add(DiagnosticsProperty('registeredAt', registeredAt));
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is DeviceToken&&(identical(other.id, id) || other.id == id)&&(identical(other.platform, platform) || other.platform == platform)&&(identical(other.registeredAt, registeredAt) || other.registeredAt == registeredAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,platform,registeredAt);

@override
String toString({ DiagnosticLevel minLevel = DiagnosticLevel.info }) {
  return 'DeviceToken(id: $id, platform: $platform, registeredAt: $registeredAt)';
}


}

/// @nodoc
abstract mixin class $DeviceTokenCopyWith<$Res>  {
  factory $DeviceTokenCopyWith(DeviceToken value, $Res Function(DeviceToken) _then) = _$DeviceTokenCopyWithImpl;
@useResult
$Res call({
 String id, String? platform,@JsonKey(name: 'registered_at') String? registeredAt
});




}
/// @nodoc
class _$DeviceTokenCopyWithImpl<$Res>
    implements $DeviceTokenCopyWith<$Res> {
  _$DeviceTokenCopyWithImpl(this._self, this._then);

  final DeviceToken _self;
  final $Res Function(DeviceToken) _then;

/// Create a copy of DeviceToken
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? id = null,Object? platform = freezed,Object? registeredAt = freezed,}) {
  return _then(_self.copyWith(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,platform: freezed == platform ? _self.platform : platform // ignore: cast_nullable_to_non_nullable
as String?,registeredAt: freezed == registeredAt ? _self.registeredAt : registeredAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [DeviceToken].
extension DeviceTokenPatterns on DeviceToken {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _DeviceToken value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _DeviceToken() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _DeviceToken value)  $default,){
final _that = this;
switch (_that) {
case _DeviceToken():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _DeviceToken value)?  $default,){
final _that = this;
switch (_that) {
case _DeviceToken() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String id,  String? platform, @JsonKey(name: 'registered_at')  String? registeredAt)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _DeviceToken() when $default != null:
return $default(_that.id,_that.platform,_that.registeredAt);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String id,  String? platform, @JsonKey(name: 'registered_at')  String? registeredAt)  $default,) {final _that = this;
switch (_that) {
case _DeviceToken():
return $default(_that.id,_that.platform,_that.registeredAt);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String id,  String? platform, @JsonKey(name: 'registered_at')  String? registeredAt)?  $default,) {final _that = this;
switch (_that) {
case _DeviceToken() when $default != null:
return $default(_that.id,_that.platform,_that.registeredAt);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _DeviceToken with DiagnosticableTreeMixin implements DeviceToken {
  const _DeviceToken({required this.id, this.platform, @JsonKey(name: 'registered_at') this.registeredAt});
  factory _DeviceToken.fromJson(Map<String, dynamic> json) => _$DeviceTokenFromJson(json);

/// The registration.
@override final  String id;
/// Which store the registered build came from.
///
/// Nullable and defaulted rather than required, the same call `PlatformFloor.minimumBuild`
/// makes: a required field is a decode that **throws**, on a build already on a phone that
/// cannot be fixed over the air, and nothing branches on this.
@override final  String? platform;
/// When the platform recorded this registration, in UTC.
@override@JsonKey(name: 'registered_at') final  String? registeredAt;

/// Create a copy of DeviceToken
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$DeviceTokenCopyWith<_DeviceToken> get copyWith => __$DeviceTokenCopyWithImpl<_DeviceToken>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$DeviceTokenToJson(this, );
}
@override
void debugFillProperties(DiagnosticPropertiesBuilder properties) {
  properties
    ..add(DiagnosticsProperty('type', 'DeviceToken'))
    ..add(DiagnosticsProperty('id', id))..add(DiagnosticsProperty('platform', platform))..add(DiagnosticsProperty('registeredAt', registeredAt));
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _DeviceToken&&(identical(other.id, id) || other.id == id)&&(identical(other.platform, platform) || other.platform == platform)&&(identical(other.registeredAt, registeredAt) || other.registeredAt == registeredAt));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,id,platform,registeredAt);

@override
String toString({ DiagnosticLevel minLevel = DiagnosticLevel.info }) {
  return 'DeviceToken(id: $id, platform: $platform, registeredAt: $registeredAt)';
}


}

/// @nodoc
abstract mixin class _$DeviceTokenCopyWith<$Res> implements $DeviceTokenCopyWith<$Res> {
  factory _$DeviceTokenCopyWith(_DeviceToken value, $Res Function(_DeviceToken) _then) = __$DeviceTokenCopyWithImpl;
@override @useResult
$Res call({
 String id, String? platform,@JsonKey(name: 'registered_at') String? registeredAt
});




}
/// @nodoc
class __$DeviceTokenCopyWithImpl<$Res>
    implements _$DeviceTokenCopyWith<$Res> {
  __$DeviceTokenCopyWithImpl(this._self, this._then);

  final _DeviceToken _self;
  final $Res Function(_DeviceToken) _then;

/// Create a copy of DeviceToken
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? id = null,Object? platform = freezed,Object? registeredAt = freezed,}) {
  return _then(_DeviceToken(
id: null == id ? _self.id : id // ignore: cast_nullable_to_non_nullable
as String,platform: freezed == platform ? _self.platform : platform // ignore: cast_nullable_to_non_nullable
as String?,registeredAt: freezed == registeredAt ? _self.registeredAt : registeredAt // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
