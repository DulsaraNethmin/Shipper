// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'minimum_version.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$MinimumVersion {

 PlatformFloor get ios; PlatformFloor get android;
/// Create a copy of MinimumVersion
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$MinimumVersionCopyWith<MinimumVersion> get copyWith => _$MinimumVersionCopyWithImpl<MinimumVersion>(this as MinimumVersion, _$identity);

  /// Serializes this MinimumVersion to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is MinimumVersion&&(identical(other.ios, ios) || other.ios == ios)&&(identical(other.android, android) || other.android == android));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,ios,android);

@override
String toString() {
  return 'MinimumVersion(ios: $ios, android: $android)';
}


}

/// @nodoc
abstract mixin class $MinimumVersionCopyWith<$Res>  {
  factory $MinimumVersionCopyWith(MinimumVersion value, $Res Function(MinimumVersion) _then) = _$MinimumVersionCopyWithImpl;
@useResult
$Res call({
 PlatformFloor ios, PlatformFloor android
});


$PlatformFloorCopyWith<$Res> get ios;$PlatformFloorCopyWith<$Res> get android;

}
/// @nodoc
class _$MinimumVersionCopyWithImpl<$Res>
    implements $MinimumVersionCopyWith<$Res> {
  _$MinimumVersionCopyWithImpl(this._self, this._then);

  final MinimumVersion _self;
  final $Res Function(MinimumVersion) _then;

/// Create a copy of MinimumVersion
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? ios = null,Object? android = null,}) {
  return _then(_self.copyWith(
ios: null == ios ? _self.ios : ios // ignore: cast_nullable_to_non_nullable
as PlatformFloor,android: null == android ? _self.android : android // ignore: cast_nullable_to_non_nullable
as PlatformFloor,
  ));
}
/// Create a copy of MinimumVersion
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$PlatformFloorCopyWith<$Res> get ios {
  
  return $PlatformFloorCopyWith<$Res>(_self.ios, (value) {
    return _then(_self.copyWith(ios: value));
  });
}/// Create a copy of MinimumVersion
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$PlatformFloorCopyWith<$Res> get android {
  
  return $PlatformFloorCopyWith<$Res>(_self.android, (value) {
    return _then(_self.copyWith(android: value));
  });
}
}


/// Adds pattern-matching-related methods to [MinimumVersion].
extension MinimumVersionPatterns on MinimumVersion {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _MinimumVersion value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _MinimumVersion() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _MinimumVersion value)  $default,){
final _that = this;
switch (_that) {
case _MinimumVersion():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _MinimumVersion value)?  $default,){
final _that = this;
switch (_that) {
case _MinimumVersion() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( PlatformFloor ios,  PlatformFloor android)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _MinimumVersion() when $default != null:
return $default(_that.ios,_that.android);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( PlatformFloor ios,  PlatformFloor android)  $default,) {final _that = this;
switch (_that) {
case _MinimumVersion():
return $default(_that.ios,_that.android);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( PlatformFloor ios,  PlatformFloor android)?  $default,) {final _that = this;
switch (_that) {
case _MinimumVersion() when $default != null:
return $default(_that.ios,_that.android);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _MinimumVersion implements MinimumVersion {
  const _MinimumVersion({required this.ios, required this.android});
  factory _MinimumVersion.fromJson(Map<String, dynamic> json) => _$MinimumVersionFromJson(json);

@override final  PlatformFloor ios;
@override final  PlatformFloor android;

/// Create a copy of MinimumVersion
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$MinimumVersionCopyWith<_MinimumVersion> get copyWith => __$MinimumVersionCopyWithImpl<_MinimumVersion>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$MinimumVersionToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _MinimumVersion&&(identical(other.ios, ios) || other.ios == ios)&&(identical(other.android, android) || other.android == android));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,ios,android);

@override
String toString() {
  return 'MinimumVersion(ios: $ios, android: $android)';
}


}

/// @nodoc
abstract mixin class _$MinimumVersionCopyWith<$Res> implements $MinimumVersionCopyWith<$Res> {
  factory _$MinimumVersionCopyWith(_MinimumVersion value, $Res Function(_MinimumVersion) _then) = __$MinimumVersionCopyWithImpl;
@override @useResult
$Res call({
 PlatformFloor ios, PlatformFloor android
});


@override $PlatformFloorCopyWith<$Res> get ios;@override $PlatformFloorCopyWith<$Res> get android;

}
/// @nodoc
class __$MinimumVersionCopyWithImpl<$Res>
    implements _$MinimumVersionCopyWith<$Res> {
  __$MinimumVersionCopyWithImpl(this._self, this._then);

  final _MinimumVersion _self;
  final $Res Function(_MinimumVersion) _then;

/// Create a copy of MinimumVersion
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? ios = null,Object? android = null,}) {
  return _then(_MinimumVersion(
ios: null == ios ? _self.ios : ios // ignore: cast_nullable_to_non_nullable
as PlatformFloor,android: null == android ? _self.android : android // ignore: cast_nullable_to_non_nullable
as PlatformFloor,
  ));
}

/// Create a copy of MinimumVersion
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$PlatformFloorCopyWith<$Res> get ios {
  
  return $PlatformFloorCopyWith<$Res>(_self.ios, (value) {
    return _then(_self.copyWith(ios: value));
  });
}/// Create a copy of MinimumVersion
/// with the given fields replaced by the non-null parameter values.
@override
@pragma('vm:prefer-inline')
$PlatformFloorCopyWith<$Res> get android {
  
  return $PlatformFloorCopyWith<$Res>(_self.android, (value) {
    return _then(_self.copyWith(android: value));
  });
}
}


/// @nodoc
mixin _$PlatformFloor {

/// The lowest build number still permitted — `internal/config`'s own words.
///
/// Defaulted rather than required so that a floor arriving without it degrades to a floor
/// nothing is below, rather than to a decode failure. Both are fail-open; this one is the
/// quieter of the two and keeps the model total.
@JsonKey(name: 'minimum_build') int get minimumBuild;/// Where to send a blocked build, or `null`.
///
/// **Nullable because the platform genuinely sends nothing.** `store_url` carries
/// `omitempty`, and `IOS_STORE_URL` and `ANDROID_STORE_URL` both default to empty —
/// `internal/config/config.go` records that as a decision rather than an omission, because
/// pilot distribution is TestFlight and Play internal testing and there is no public listing
/// to link to. So the update prompt has two shapes and this is the field that selects
/// between them. See `update_required_screen.dart`.
@JsonKey(name: 'store_url') String? get storeUrl;
/// Create a copy of PlatformFloor
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$PlatformFloorCopyWith<PlatformFloor> get copyWith => _$PlatformFloorCopyWithImpl<PlatformFloor>(this as PlatformFloor, _$identity);

  /// Serializes this PlatformFloor to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is PlatformFloor&&(identical(other.minimumBuild, minimumBuild) || other.minimumBuild == minimumBuild)&&(identical(other.storeUrl, storeUrl) || other.storeUrl == storeUrl));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,minimumBuild,storeUrl);

@override
String toString() {
  return 'PlatformFloor(minimumBuild: $minimumBuild, storeUrl: $storeUrl)';
}


}

/// @nodoc
abstract mixin class $PlatformFloorCopyWith<$Res>  {
  factory $PlatformFloorCopyWith(PlatformFloor value, $Res Function(PlatformFloor) _then) = _$PlatformFloorCopyWithImpl;
@useResult
$Res call({
@JsonKey(name: 'minimum_build') int minimumBuild,@JsonKey(name: 'store_url') String? storeUrl
});




}
/// @nodoc
class _$PlatformFloorCopyWithImpl<$Res>
    implements $PlatformFloorCopyWith<$Res> {
  _$PlatformFloorCopyWithImpl(this._self, this._then);

  final PlatformFloor _self;
  final $Res Function(PlatformFloor) _then;

/// Create a copy of PlatformFloor
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? minimumBuild = null,Object? storeUrl = freezed,}) {
  return _then(_self.copyWith(
minimumBuild: null == minimumBuild ? _self.minimumBuild : minimumBuild // ignore: cast_nullable_to_non_nullable
as int,storeUrl: freezed == storeUrl ? _self.storeUrl : storeUrl // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}

}


/// Adds pattern-matching-related methods to [PlatformFloor].
extension PlatformFloorPatterns on PlatformFloor {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _PlatformFloor value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _PlatformFloor() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _PlatformFloor value)  $default,){
final _that = this;
switch (_that) {
case _PlatformFloor():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _PlatformFloor value)?  $default,){
final _that = this;
switch (_that) {
case _PlatformFloor() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function(@JsonKey(name: 'minimum_build')  int minimumBuild, @JsonKey(name: 'store_url')  String? storeUrl)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _PlatformFloor() when $default != null:
return $default(_that.minimumBuild,_that.storeUrl);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function(@JsonKey(name: 'minimum_build')  int minimumBuild, @JsonKey(name: 'store_url')  String? storeUrl)  $default,) {final _that = this;
switch (_that) {
case _PlatformFloor():
return $default(_that.minimumBuild,_that.storeUrl);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function(@JsonKey(name: 'minimum_build')  int minimumBuild, @JsonKey(name: 'store_url')  String? storeUrl)?  $default,) {final _that = this;
switch (_that) {
case _PlatformFloor() when $default != null:
return $default(_that.minimumBuild,_that.storeUrl);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _PlatformFloor implements PlatformFloor {
  const _PlatformFloor({@JsonKey(name: 'minimum_build') this.minimumBuild = 0, @JsonKey(name: 'store_url') this.storeUrl});
  factory _PlatformFloor.fromJson(Map<String, dynamic> json) => _$PlatformFloorFromJson(json);

/// The lowest build number still permitted — `internal/config`'s own words.
///
/// Defaulted rather than required so that a floor arriving without it degrades to a floor
/// nothing is below, rather than to a decode failure. Both are fail-open; this one is the
/// quieter of the two and keeps the model total.
@override@JsonKey(name: 'minimum_build') final  int minimumBuild;
/// Where to send a blocked build, or `null`.
///
/// **Nullable because the platform genuinely sends nothing.** `store_url` carries
/// `omitempty`, and `IOS_STORE_URL` and `ANDROID_STORE_URL` both default to empty —
/// `internal/config/config.go` records that as a decision rather than an omission, because
/// pilot distribution is TestFlight and Play internal testing and there is no public listing
/// to link to. So the update prompt has two shapes and this is the field that selects
/// between them. See `update_required_screen.dart`.
@override@JsonKey(name: 'store_url') final  String? storeUrl;

/// Create a copy of PlatformFloor
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$PlatformFloorCopyWith<_PlatformFloor> get copyWith => __$PlatformFloorCopyWithImpl<_PlatformFloor>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$PlatformFloorToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _PlatformFloor&&(identical(other.minimumBuild, minimumBuild) || other.minimumBuild == minimumBuild)&&(identical(other.storeUrl, storeUrl) || other.storeUrl == storeUrl));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,minimumBuild,storeUrl);

@override
String toString() {
  return 'PlatformFloor(minimumBuild: $minimumBuild, storeUrl: $storeUrl)';
}


}

/// @nodoc
abstract mixin class _$PlatformFloorCopyWith<$Res> implements $PlatformFloorCopyWith<$Res> {
  factory _$PlatformFloorCopyWith(_PlatformFloor value, $Res Function(_PlatformFloor) _then) = __$PlatformFloorCopyWithImpl;
@override @useResult
$Res call({
@JsonKey(name: 'minimum_build') int minimumBuild,@JsonKey(name: 'store_url') String? storeUrl
});




}
/// @nodoc
class __$PlatformFloorCopyWithImpl<$Res>
    implements _$PlatformFloorCopyWith<$Res> {
  __$PlatformFloorCopyWithImpl(this._self, this._then);

  final _PlatformFloor _self;
  final $Res Function(_PlatformFloor) _then;

/// Create a copy of PlatformFloor
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? minimumBuild = null,Object? storeUrl = freezed,}) {
  return _then(_PlatformFloor(
minimumBuild: null == minimumBuild ? _self.minimumBuild : minimumBuild // ignore: cast_nullable_to_non_nullable
as int,storeUrl: freezed == storeUrl ? _self.storeUrl : storeUrl // ignore: cast_nullable_to_non_nullable
as String?,
  ));
}


}

// dart format on
