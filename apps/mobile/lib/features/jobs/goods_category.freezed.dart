// GENERATED CODE - DO NOT MODIFY BY HAND
// coverage:ignore-file
// ignore_for_file: type=lint
// ignore_for_file: unused_element, deprecated_member_use, deprecated_member_use_from_same_package, use_function_type_syntax_for_parameters, unnecessary_const, avoid_init_to_null, invalid_override_different_default_values_named, prefer_expression_function_bodies, annotate_overrides, invalid_annotation_target, unnecessary_question_mark

part of 'goods_category.dart';

// **************************************************************************
// FreezedGenerator
// **************************************************************************

// dart format off
T _$identity<T>(T value) => value;

/// @nodoc
mixin _$GoodsCategory {

/// The stored form, and the value sent as a job's `goods_category` — lower snake case.
///
/// Stable across a change of wording, which is why it and not [label] is what a job holds.
 String get code;/// The short human name, in Australian English. Shown, never sent.
 String get label;/// A one-line explanation beneath the label, or `null` when the catalogue gives none.
 String? get description;/// Whether a job in this category may be **published**.
///
/// A refused category is served in the list rather than left out, so the form can show a
/// customer what Shipper does not take instead of leaving them to guess from an absence.
///
/// **A draft may name a refused category and publishing one is refused** — `Docs/07` §3 lets
/// the app hide or disable and leaves the decision to the platform, and SHIP-59 makes that
/// refusal by name, quoting the catalogue's own wording.
 bool get carried;/// Whether the entry still awaits the legal review X-4 owns.
///
/// True for every entry in the list shipping today: `Docs/11` §5 records that the owner
/// approved a provisional list in reduced form so the product could be walked end to end, and
/// the reference data says so rather than the app pretending otherwise. Per entry rather than
/// per catalogue, because a reviewer is far likelier to confirm most of a list and query two
/// than to bless or reject all of it at once.
 bool get provisional;
/// Create a copy of GoodsCategory
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$GoodsCategoryCopyWith<GoodsCategory> get copyWith => _$GoodsCategoryCopyWithImpl<GoodsCategory>(this as GoodsCategory, _$identity);

  /// Serializes this GoodsCategory to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is GoodsCategory&&(identical(other.code, code) || other.code == code)&&(identical(other.label, label) || other.label == label)&&(identical(other.description, description) || other.description == description)&&(identical(other.carried, carried) || other.carried == carried)&&(identical(other.provisional, provisional) || other.provisional == provisional));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,code,label,description,carried,provisional);

@override
String toString() {
  return 'GoodsCategory(code: $code, label: $label, description: $description, carried: $carried, provisional: $provisional)';
}


}

/// @nodoc
abstract mixin class $GoodsCategoryCopyWith<$Res>  {
  factory $GoodsCategoryCopyWith(GoodsCategory value, $Res Function(GoodsCategory) _then) = _$GoodsCategoryCopyWithImpl;
@useResult
$Res call({
 String code, String label, String? description, bool carried, bool provisional
});




}
/// @nodoc
class _$GoodsCategoryCopyWithImpl<$Res>
    implements $GoodsCategoryCopyWith<$Res> {
  _$GoodsCategoryCopyWithImpl(this._self, this._then);

  final GoodsCategory _self;
  final $Res Function(GoodsCategory) _then;

/// Create a copy of GoodsCategory
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? code = null,Object? label = null,Object? description = freezed,Object? carried = null,Object? provisional = null,}) {
  return _then(_self.copyWith(
code: null == code ? _self.code : code // ignore: cast_nullable_to_non_nullable
as String,label: null == label ? _self.label : label // ignore: cast_nullable_to_non_nullable
as String,description: freezed == description ? _self.description : description // ignore: cast_nullable_to_non_nullable
as String?,carried: null == carried ? _self.carried : carried // ignore: cast_nullable_to_non_nullable
as bool,provisional: null == provisional ? _self.provisional : provisional // ignore: cast_nullable_to_non_nullable
as bool,
  ));
}

}


/// Adds pattern-matching-related methods to [GoodsCategory].
extension GoodsCategoryPatterns on GoodsCategory {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _GoodsCategory value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _GoodsCategory() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _GoodsCategory value)  $default,){
final _that = this;
switch (_that) {
case _GoodsCategory():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _GoodsCategory value)?  $default,){
final _that = this;
switch (_that) {
case _GoodsCategory() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( String code,  String label,  String? description,  bool carried,  bool provisional)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _GoodsCategory() when $default != null:
return $default(_that.code,_that.label,_that.description,_that.carried,_that.provisional);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( String code,  String label,  String? description,  bool carried,  bool provisional)  $default,) {final _that = this;
switch (_that) {
case _GoodsCategory():
return $default(_that.code,_that.label,_that.description,_that.carried,_that.provisional);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( String code,  String label,  String? description,  bool carried,  bool provisional)?  $default,) {final _that = this;
switch (_that) {
case _GoodsCategory() when $default != null:
return $default(_that.code,_that.label,_that.description,_that.carried,_that.provisional);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _GoodsCategory extends GoodsCategory {
  const _GoodsCategory({required this.code, required this.label, this.description, required this.carried, required this.provisional}): super._();
  factory _GoodsCategory.fromJson(Map<String, dynamic> json) => _$GoodsCategoryFromJson(json);

/// The stored form, and the value sent as a job's `goods_category` — lower snake case.
///
/// Stable across a change of wording, which is why it and not [label] is what a job holds.
@override final  String code;
/// The short human name, in Australian English. Shown, never sent.
@override final  String label;
/// A one-line explanation beneath the label, or `null` when the catalogue gives none.
@override final  String? description;
/// Whether a job in this category may be **published**.
///
/// A refused category is served in the list rather than left out, so the form can show a
/// customer what Shipper does not take instead of leaving them to guess from an absence.
///
/// **A draft may name a refused category and publishing one is refused** — `Docs/07` §3 lets
/// the app hide or disable and leaves the decision to the platform, and SHIP-59 makes that
/// refusal by name, quoting the catalogue's own wording.
@override final  bool carried;
/// Whether the entry still awaits the legal review X-4 owns.
///
/// True for every entry in the list shipping today: `Docs/11` §5 records that the owner
/// approved a provisional list in reduced form so the product could be walked end to end, and
/// the reference data says so rather than the app pretending otherwise. Per entry rather than
/// per catalogue, because a reviewer is far likelier to confirm most of a list and query two
/// than to bless or reject all of it at once.
@override final  bool provisional;

/// Create a copy of GoodsCategory
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$GoodsCategoryCopyWith<_GoodsCategory> get copyWith => __$GoodsCategoryCopyWithImpl<_GoodsCategory>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$GoodsCategoryToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _GoodsCategory&&(identical(other.code, code) || other.code == code)&&(identical(other.label, label) || other.label == label)&&(identical(other.description, description) || other.description == description)&&(identical(other.carried, carried) || other.carried == carried)&&(identical(other.provisional, provisional) || other.provisional == provisional));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,code,label,description,carried,provisional);

@override
String toString() {
  return 'GoodsCategory(code: $code, label: $label, description: $description, carried: $carried, provisional: $provisional)';
}


}

/// @nodoc
abstract mixin class _$GoodsCategoryCopyWith<$Res> implements $GoodsCategoryCopyWith<$Res> {
  factory _$GoodsCategoryCopyWith(_GoodsCategory value, $Res Function(_GoodsCategory) _then) = __$GoodsCategoryCopyWithImpl;
@override @useResult
$Res call({
 String code, String label, String? description, bool carried, bool provisional
});




}
/// @nodoc
class __$GoodsCategoryCopyWithImpl<$Res>
    implements _$GoodsCategoryCopyWith<$Res> {
  __$GoodsCategoryCopyWithImpl(this._self, this._then);

  final _GoodsCategory _self;
  final $Res Function(_GoodsCategory) _then;

/// Create a copy of GoodsCategory
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? code = null,Object? label = null,Object? description = freezed,Object? carried = null,Object? provisional = null,}) {
  return _then(_GoodsCategory(
code: null == code ? _self.code : code // ignore: cast_nullable_to_non_nullable
as String,label: null == label ? _self.label : label // ignore: cast_nullable_to_non_nullable
as String,description: freezed == description ? _self.description : description // ignore: cast_nullable_to_non_nullable
as String?,carried: null == carried ? _self.carried : carried // ignore: cast_nullable_to_non_nullable
as bool,provisional: null == provisional ? _self.provisional : provisional // ignore: cast_nullable_to_non_nullable
as bool,
  ));
}


}


/// @nodoc
mixin _$GoodsCatalogue {

 List<GoodsCategory> get categories;
/// Create a copy of GoodsCatalogue
/// with the given fields replaced by the non-null parameter values.
@JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
$GoodsCatalogueCopyWith<GoodsCatalogue> get copyWith => _$GoodsCatalogueCopyWithImpl<GoodsCatalogue>(this as GoodsCatalogue, _$identity);

  /// Serializes this GoodsCatalogue to a JSON map.
  Map<String, dynamic> toJson();


@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is GoodsCatalogue&&const DeepCollectionEquality().equals(other.categories, categories));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,const DeepCollectionEquality().hash(categories));

@override
String toString() {
  return 'GoodsCatalogue(categories: $categories)';
}


}

/// @nodoc
abstract mixin class $GoodsCatalogueCopyWith<$Res>  {
  factory $GoodsCatalogueCopyWith(GoodsCatalogue value, $Res Function(GoodsCatalogue) _then) = _$GoodsCatalogueCopyWithImpl;
@useResult
$Res call({
 List<GoodsCategory> categories
});




}
/// @nodoc
class _$GoodsCatalogueCopyWithImpl<$Res>
    implements $GoodsCatalogueCopyWith<$Res> {
  _$GoodsCatalogueCopyWithImpl(this._self, this._then);

  final GoodsCatalogue _self;
  final $Res Function(GoodsCatalogue) _then;

/// Create a copy of GoodsCatalogue
/// with the given fields replaced by the non-null parameter values.
@pragma('vm:prefer-inline') @override $Res call({Object? categories = null,}) {
  return _then(_self.copyWith(
categories: null == categories ? _self.categories : categories // ignore: cast_nullable_to_non_nullable
as List<GoodsCategory>,
  ));
}

}


/// Adds pattern-matching-related methods to [GoodsCatalogue].
extension GoodsCataloguePatterns on GoodsCatalogue {
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

@optionalTypeArgs TResult maybeMap<TResult extends Object?>(TResult Function( _GoodsCatalogue value)?  $default,{required TResult orElse(),}){
final _that = this;
switch (_that) {
case _GoodsCatalogue() when $default != null:
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

@optionalTypeArgs TResult map<TResult extends Object?>(TResult Function( _GoodsCatalogue value)  $default,){
final _that = this;
switch (_that) {
case _GoodsCatalogue():
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

@optionalTypeArgs TResult? mapOrNull<TResult extends Object?>(TResult? Function( _GoodsCatalogue value)?  $default,){
final _that = this;
switch (_that) {
case _GoodsCatalogue() when $default != null:
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

@optionalTypeArgs TResult maybeWhen<TResult extends Object?>(TResult Function( List<GoodsCategory> categories)?  $default,{required TResult orElse(),}) {final _that = this;
switch (_that) {
case _GoodsCatalogue() when $default != null:
return $default(_that.categories);case _:
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

@optionalTypeArgs TResult when<TResult extends Object?>(TResult Function( List<GoodsCategory> categories)  $default,) {final _that = this;
switch (_that) {
case _GoodsCatalogue():
return $default(_that.categories);case _:
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

@optionalTypeArgs TResult? whenOrNull<TResult extends Object?>(TResult? Function( List<GoodsCategory> categories)?  $default,) {final _that = this;
switch (_that) {
case _GoodsCatalogue() when $default != null:
return $default(_that.categories);case _:
  return null;

}
}

}

/// @nodoc
@JsonSerializable()

class _GoodsCatalogue extends GoodsCatalogue {
  const _GoodsCatalogue({final  List<GoodsCategory> categories = const <GoodsCategory>[]}): _categories = categories,super._();
  factory _GoodsCatalogue.fromJson(Map<String, dynamic> json) => _$GoodsCatalogueFromJson(json);

 final  List<GoodsCategory> _categories;
@override@JsonKey() List<GoodsCategory> get categories {
  if (_categories is EqualUnmodifiableListView) return _categories;
  // ignore: implicit_dynamic_type
  return EqualUnmodifiableListView(_categories);
}


/// Create a copy of GoodsCatalogue
/// with the given fields replaced by the non-null parameter values.
@override @JsonKey(includeFromJson: false, includeToJson: false)
@pragma('vm:prefer-inline')
_$GoodsCatalogueCopyWith<_GoodsCatalogue> get copyWith => __$GoodsCatalogueCopyWithImpl<_GoodsCatalogue>(this, _$identity);

@override
Map<String, dynamic> toJson() {
  return _$GoodsCatalogueToJson(this, );
}

@override
bool operator ==(Object other) {
  return identical(this, other) || (other.runtimeType == runtimeType&&other is _GoodsCatalogue&&const DeepCollectionEquality().equals(other._categories, _categories));
}

@JsonKey(includeFromJson: false, includeToJson: false)
@override
int get hashCode => Object.hash(runtimeType,const DeepCollectionEquality().hash(_categories));

@override
String toString() {
  return 'GoodsCatalogue(categories: $categories)';
}


}

/// @nodoc
abstract mixin class _$GoodsCatalogueCopyWith<$Res> implements $GoodsCatalogueCopyWith<$Res> {
  factory _$GoodsCatalogueCopyWith(_GoodsCatalogue value, $Res Function(_GoodsCatalogue) _then) = __$GoodsCatalogueCopyWithImpl;
@override @useResult
$Res call({
 List<GoodsCategory> categories
});




}
/// @nodoc
class __$GoodsCatalogueCopyWithImpl<$Res>
    implements _$GoodsCatalogueCopyWith<$Res> {
  __$GoodsCatalogueCopyWithImpl(this._self, this._then);

  final _GoodsCatalogue _self;
  final $Res Function(_GoodsCatalogue) _then;

/// Create a copy of GoodsCatalogue
/// with the given fields replaced by the non-null parameter values.
@override @pragma('vm:prefer-inline') $Res call({Object? categories = null,}) {
  return _then(_GoodsCatalogue(
categories: null == categories ? _self._categories : categories // ignore: cast_nullable_to_non_nullable
as List<GoodsCategory>,
  ));
}


}

// dart format on
