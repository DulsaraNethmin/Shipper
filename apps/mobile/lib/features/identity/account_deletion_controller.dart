import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/identity/account_deletion.dart';
import 'package:shipper/features/identity/identity_repository.dart';

part 'account_deletion_controller.freezed.dart';

/// Asking for this account to be deleted (SHIP-173).
@freezed
abstract class AccountDeletionState with _$AccountDeletionState {
  const factory AccountDeletionState({
    /// The request is on its way to the platform.
    @Default(false) bool submitting,

    /// What the platform recorded, once it has answered.
    ///
    /// **Assigned from the response and from nothing else.** A repeat answers `200` with the
    /// request that already exists, so a screen fed from this reconciles to the platform's record
    /// rather than to what the person last tapped — which matters here more than usual, because a
    /// repeat is also how a deferral lifts.
    AccountDeletion? request,

    /// What the last attempt failed with, or `null`.
    ApiFailure? failure,
  }) = _AccountDeletionState;

  const AccountDeletionState._();

  /// Whether the platform has recorded a request for this account.
  bool get recorded => request != null;

  /// What the person should be told when the platform refused.
  ///
  /// **Branching on `code` and never on the message** (`Docs/07` §6, `CLAUDE.md`). There is
  /// deliberately little here: `contracts/paths/identity.yaml` gives this endpoint four failures
  /// and three of them — `401`, `500`, `503` — are already covered by the generic banner and say
  /// nothing a person could act on. `null` means the banner is the whole of what is known.
  String? get refusal => switch (failure) {
        // The key identified a different request. The next attempt mints a new one, so trying
        // again is the right advice rather than a dead end.
        ApiErrorResponse(code: 'idempotency_key_reused') =>
          'That did not go through. Try again.',

        // The platform could not reach its database. Transient by construction, and the contract
        // says so — which makes "try again shortly" honest rather than a shrug.
        ApiErrorResponse(code: 'unavailable') =>
          'Shipper could not record that just now. Try again in a moment.',

        _ => null,
      };
}

/// Asks the platform to delete this account (SHIP-173).
///
/// ## The confirmation is not in here, and that is deliberate
///
/// The *Done when* is "deletion is initiated in-app with clear consequences and confirmation". The
/// confirmation is a property of the screen — a dialog naming what is lost, dismissable, with
/// nothing sent unless it is accepted — and it lives in `account_deletion_screen.dart`. A
/// `confirming` flag here would make a dialog look like part of the protocol, and the platform
/// deliberately has no confirmation field on the wire: a `"confirm": true` is a checkbox nobody can
/// see anybody tick.
///
/// ## Asking twice, and why this does not refuse to
///
/// [AwardController] refuses to run once the job is awarded, because a second award is a different
/// act. This is the opposite case and the difference is worth stating: **asking again is how a
/// deferral lifts.** A person deferred behind a delivery comes back a week later, taps again, and
/// the platform moves the request to `requested` and answers `200`. A controller that short-circuited
/// on `recorded` would leave them looking at a stale deferral forever.
///
/// What it does refuse is a second request while one is in flight, which is only a double tap.
///
/// ## The key, and the one case it is held
///
/// [ActionKey] keeps a key only when the previous attempt failed **without saying whether the
/// platform acted** — a dropped connection, which is what idempotency exists for. The guarantee does
/// not rest on it either way: the endpoint is idempotent by *state*, so a fresh key from a phone that
/// restarted between attempts is answered with the request that already exists rather than a second
/// one. The body is empty, so every attempt fingerprints identically.
///
/// ## Not queued
///
/// `Docs/07` §4's offline queue is for delivery milestones and proof. Nothing here touches
/// `core/queue` or `core/sync`: a deletion request held on a handset and sent days later would be a
/// legal clock started by a phone reconnecting, and `OperationKind`'s closed set makes it a compile
/// error rather than a rule somebody follows.
class AccountDeletionController extends Notifier<AccountDeletionState> {
  /// One key for one action. See the note on the class.
  final _key = ActionKey();

  @override
  AccountDeletionState build() => const AccountDeletionState();

  /// Asks the platform to delete this account, and answers whether it took the request.
  Future<bool> request() async {
    if (state.submitting) return false;

    final key = _key.forRequest(null);
    state = state.copyWith(submitting: true, failure: null);

    try {
      final recorded = await ref
          .read(identityRepositoryProvider)
          .requestAccountDeletion(idempotencyKey: key);

      _key.settled(null);
      if (!ref.mounted) return true;

      state = state.copyWith(submitting: false, request: recorded, failure: null);
      return true;
    } on ApiFailure catch (failure) {
      _key.settled(failure);
      _failed(failure);
      return false;
    } catch (_) {
      // Past `ApiClient`'s mapping: a `2xx` whose body is not a deletion request. **The key is
      // kept**, because nothing here establishes whether the request was recorded — the one
      // circumstance `ActionKey` exists for.
      const failure = ApiMalformedResponse(statusCode: 0);
      _key.settled(failure);
      _failed(failure);
      return false;
    }
  }

  /// Clears the platform's refusal so the person can try again.
  ///
  /// It does **not** clear [AccountDeletionState.request]. A request the platform recorded is not
  /// something a button on this device takes back, and there is no endpoint that would — withdrawing
  /// a deletion request has no ticket and `000105` declined to invent the state for it.
  void dismissFailure() => state = state.copyWith(failure: null);

  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(submitting: false, failure: failure);
  }
}

/// Deleting this account.
///
/// **Auto-disposed**, like every other controller in this client and for the reason `Docs/07` §3
/// gives: what a session cached goes with the token at sign-out. Kept alive it would hold one
/// person's deletion date for whoever signed in next on the same handset.
final accountDeletionProvider =
    NotifierProvider.autoDispose<AccountDeletionController, AccountDeletionState>(
  AccountDeletionController.new,
);
