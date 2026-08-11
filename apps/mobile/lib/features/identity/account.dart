import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/auth/user_role.dart';

part 'account.freezed.dart';
part 'account.g.dart';

/// An account, as its own owner sees it (SHIP-51).
///
/// The `Account` schema in `contracts/paths/identity.yaml`, and the body of all three of
/// `POST /v1/auth/register`, `/verify-email` and `/verify-phone`. **There is no token in it, and
/// that is deliberate on the platform's side: registering is not signing in.** SHIP-41 issues
/// the first access token, at the endpoint that exists to take a password.
///
/// Verification arrives as two booleans rather than the timestamps the database holds, because
/// the client's question is which screen comes next; "since when" is a support question.
///
/// ## What is required here, and what is not
///
/// `Docs/07` §6 requires unknown fields to be ignored so an additive server change needs no
/// release, and `json_serializable` does that by reading only the keys declared below. The other
/// direction is the reason `status` and `createdAt` are nullable while nothing else is: a
/// required field is a decode that throws, so **only the fields the app actually reads are
/// required.** Making the whole contract required would turn a field the platform stops sending
/// into a crash on every installed build — and the two nobody renders would be the ones to
/// crash on.
@freezed
abstract class Account with _$Account {
  const factory Account({
    required String id,
    required String email,

    /// E.164, as the platform normalised it on the way in. Shown back to the person so they can
    /// see which number the code was sent to.
    required String phone,

    /// Fixed at registration and immutable afterwards, enforced by a database trigger
    /// (SHIP-45). An unrecognised value decodes to [UserRole.unknown] rather than throwing —
    /// see the note there.
    @JsonKey(unknownEnumValue: UserRole.unknown) required UserRole role,
    @JsonKey(name: 'email_verified') required bool emailVerified,
    @JsonKey(name: 'phone_verified') required bool phoneVerified,

    /// `active`, `restricted` or `suspended` — whether the account may be used at all.
    ///
    /// A `String` rather than an enum because nothing in the client acts on it yet, and a fourth
    /// value must not be a decode failure. **It is not provider verification**, which is a
    /// separate eligibility decision with five states of its own (`Docs/04` §4).
    String? status,
    @JsonKey(name: 'created_at') String? createdAt,
  }) = _Account;

  factory Account.fromJson(Map<String, dynamic> json) => _$AccountFromJson(json);
}
