// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'goods_category.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_GoodsCategory _$GoodsCategoryFromJson(Map<String, dynamic> json) =>
    _GoodsCategory(
      code: json['code'] as String,
      label: json['label'] as String,
      description: json['description'] as String?,
      carried: json['carried'] as bool,
      provisional: json['provisional'] as bool,
    );

Map<String, dynamic> _$GoodsCategoryToJson(_GoodsCategory instance) =>
    <String, dynamic>{
      'code': instance.code,
      'label': instance.label,
      'description': instance.description,
      'carried': instance.carried,
      'provisional': instance.provisional,
    };

_GoodsCatalogue _$GoodsCatalogueFromJson(Map<String, dynamic> json) =>
    _GoodsCatalogue(
      categories:
          (json['categories'] as List<dynamic>?)
              ?.map((e) => GoodsCategory.fromJson(e as Map<String, dynamic>))
              .toList() ??
          const <GoodsCategory>[],
    );

Map<String, dynamic> _$GoodsCatalogueToJson(_GoodsCatalogue instance) =>
    <String, dynamic>{'categories': instance.categories};
