// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'account.dart';

// **************************************************************************
// JsonSerializableGenerator
// **************************************************************************

_Account _$AccountFromJson(Map<String, dynamic> json) => _Account(
  id: json['id'] as String,
  email: json['email'] as String,
  phone: json['phone'] as String,
  role: $enumDecode(
    _$UserRoleEnumMap,
    json['role'],
    unknownValue: UserRole.unknown,
  ),
  emailVerified: json['email_verified'] as bool,
  phoneVerified: json['phone_verified'] as bool,
  status: json['status'] as String?,
  createdAt: json['created_at'] as String?,
);

Map<String, dynamic> _$AccountToJson(_Account instance) => <String, dynamic>{
  'id': instance.id,
  'email': instance.email,
  'phone': instance.phone,
  'role': _$UserRoleEnumMap[instance.role]!,
  'email_verified': instance.emailVerified,
  'phone_verified': instance.phoneVerified,
  'status': instance.status,
  'created_at': instance.createdAt,
};

const _$UserRoleEnumMap = {
  UserRole.customer: 'customer',
  UserRole.provider: 'provider',
  UserRole.unknown: 'unknown',
};
