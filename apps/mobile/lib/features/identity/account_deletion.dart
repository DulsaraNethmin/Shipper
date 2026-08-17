import 'package:freezed_annotation/freezed_annotation.dart';

part 'account_deletion.freezed.dart';
part 'account_deletion.g.dart';

/// An outstanding request to delete this account (SHIP-169, SHIP-170).
///
/// The `DeletionRequest` schema in `contracts/paths/identity.yaml`, and the body of both answers
/// `POST /v1/account/deletion` gives — `202` the first time, `200` on every repeat.
///
/// ## The status code is not the answer, and this is why there is no field for it
///
/// A client that read `202` as "recorded" and `200` as "nothing happened" would be wrong in both
/// directions. `200` is the ordinary answer to somebody opening this screen twice, and it is also
/// what comes back when the request has just stopped being deferred — a real change to the account
/// with no new row to show for it. So the screen branches on [state] and never on the code, which
/// is what the contract asks for in as many words.
///
/// ## Only what the screen renders is required
///
/// `Docs/07` §6: ignore unknown fields so an additive server change needs no release, and require
/// only the fields the app actually reads — a required field is a decode that throws, and a field
/// the platform stops sending would then crash every installed build.
@freezed
abstract class AccountDeletion with _$AccountDeletion {
  const factory AccountDeletion({
    required String id,

    /// `requested` or `deferred`.
    ///
    /// A `String` rather than an enum, for the reason `Account.status` is one: SHIP-171 adds
    /// `completed`, and a third value must not be a decode failure on a build already on a
    /// handset. [deferred] is the one question this screen asks of it.
    required String state,

    /// When the person asked, as the platform recorded it. It does not move.
    @JsonKey(name: 'requested_at') required String requestedAt,

    /// The date the platform will have finished by — **a promise only while [state] is
    /// `requested`.**
    ///
    /// On a deferred request it is the earliest the platform could finish, because the thirty
    /// days start when the delivery closes and nobody knows yet when that is. The screen says so
    /// rather than showing the same sentence in both states.
    @JsonKey(name: 'completes_by') required String completesBy,

    /// Why the request is waiting, in the platform's own words — present only when it is.
    ///
    /// **Rendered as given and never parsed.** It is policy copy (`Docs/05` §3.1 is a legal
    /// position) and the platform holds it precisely so it can be corrected without a store
    /// release; a client that reworded it would be shipping its own version of a legal sentence.
    @JsonKey(name: 'deferral_reason') String? deferralReason,
  }) = _AccountDeletion;

  const AccountDeletion._();

  factory AccountDeletion.fromJson(Map<String, dynamic> json) =>
      _$AccountDeletionFromJson(json);

  /// Whether this request is waiting on a delivery.
  ///
  /// Compared against the contract's own string. An unrecognised state reads as *not* deferred,
  /// which is the safe direction for a screen: SHIP-171's `completed` is a request that has been
  /// executed, and rendering "waiting on your delivery" for it would be worse than rendering the
  /// ordinary sentence.
  bool get deferred => state == 'deferred';
}
