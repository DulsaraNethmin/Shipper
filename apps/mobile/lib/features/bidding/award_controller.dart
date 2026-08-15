import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';

part 'award_controller.freezed.dart';

/// Awarding one job to one offer (SHIP-104).
@freezed
abstract class AwardState with _$AwardState {
  const factory AwardState({
    /// The award is on its way to the platform.
    ///
    /// **The only "pending" there is**, and deliberately so: `Docs/07` §4 keeps awarding out of the
    /// offline queue with bidding and negotiation, because "a stale local decision is worse than an
    /// honest 'you are offline'". Nothing durable is written, so either the platform answered or it
    /// did not.
    @Default(false) bool awarding,

    /// The offer the platform accepted, once it has.
    ///
    /// **Assigned from the response and from nothing else.** A retry answered `200` with an earlier
    /// attempt's award therefore reconciles this screen to the platform's record rather than to
    /// what the customer last tapped — which is the case that actually happens, because the second
    /// row of the contract's retry table is a phone that lost its connection and generated a fresh
    /// key for the same intent.
    Bid? accepted,

    /// What the last attempt failed with, or `null`.
    ApiFailure? failure,
  }) = _AwardState;

  const AwardState._();

  /// Whether this job has been awarded, as far as this device has been told.
  bool get awarded => accepted != null;

  /// What the customer should be told, when the platform refused the award.
  ///
  /// **Branching on `error.code` and never on the message** (`Docs/07` §6, `CLAUDE.md`). Four codes
  /// lead to three different places and `contracts/paths/bidding.yaml`'s `JobNotAwardable` table is
  /// the authority for which; this turns them into the sentence a customer reads.
  ///
  /// `null` when there is nothing to say — no failure, or one the generic banner already covers.
  String? get refusal => switch (failure) {
        // The job can no longer be awarded. The contract's own reading is "you have already awarded
        // it, or it was cancelled or expired", and this device cannot tell which — so it says the
        // one thing that is true of all three and sends them to the job.
        ApiErrorResponse(code: 'conflict') =>
          'This delivery can no longer be awarded. It may already have been awarded, or it has been '
              'cancelled or has expired. Open the delivery to see where it stands.',

        // The offer ended between this screen being drawn and the button being tapped. Ordinary
        // rather than exceptional on a marketplace: a provider may withdraw at any moment.
        ApiErrorResponse(code: 'bidding_bid_closed') =>
          'That offer is no longer open — it has been withdrawn, replaced or has expired. Reload '
              'the offers and choose another.',

        // Awarding your own counter would commit a provider to terms they never agreed to
        // (`ck_bids_only_a_providers_offer_is_accepted`). The screen already labels a customer's
        // own counter and does not offer a button on it, so reaching this means the offer changed
        // hands underneath — worth saying plainly rather than as a generic failure.
        ApiErrorResponse(code: 'bidding_wrong_party') =>
          'That is your own counter-offer. Wait for the provider to answer it, then award the offer '
              'they make.',

        // The key identified a different request. Not a retry, and not something to hide: the next
        // attempt mints a new key, so trying again is the right advice.
        ApiErrorResponse(code: 'idempotency_key_reused') =>
          'That did not go through. Try awarding the offer again.',

        // A job that is not this customer's and one that does not exist are the same answer, by
        // design — so this cannot say which, and must not guess.
        ApiErrorResponse(statusCode: 404) =>
          'This delivery could not be found. Open it again from your deliveries.',

        _ => null,
      };
}

/// Awards one job to one offer (SHIP-104).
///
/// ## What "explicit confirmation" is for, and why it is not in this class
///
/// The *Done when* asks the customer to award "with explicit confirmation". That is a property of
/// the screen — a dialog naming the price and the provider, dismissable, with the consequence
/// written on it — and it lives in `compare_offers_screen.dart`. This controller is deliberately
/// **not** a two-step state machine with a `confirming` flag: a confirmation the platform never
/// hears about is presentation, and putting it here would make it look like part of the protocol.
///
/// What is worth stating is *why* the confirmation has to be explicit at all, because a dialog can
/// look like ceremony. Awarding is the one irreversible action a customer takes on this screen:
/// `Docs/02` §3 has the award atomically mark one bid accepted **and every other offer on the job
/// rejected** (SHIP-93), and a `rejected` offer can no longer be revised, withdrawn or countered.
/// There is no un-award. A mis-tap on a horizontally scrolling row of cards would end the bidding
/// on somebody's delivery at the wrong price.
///
/// ## Not queued, and the type system says so
///
/// `Docs/07` §4: bidding, awarding and negotiation are "competitive, time-sensitive, and
/// multi-party". `OperationKind` has a private constructor and exactly two members, both
/// `delivery.*`, so an award **cannot** be enqueued — the set is closed by the compiler rather than
/// by a rule somebody follows. This file touches neither `core/queue` nor `core/sync`.
///
/// ## Retrying, and the one place this differs from placing a bid
///
/// [ActionKey] holds a key only when the previous attempt failed without saying whether the
/// platform acted — a dropped connection, which is exactly the case idempotency exists for.
///
/// **But the guarantee here does not rest on the key**, and the contract is explicit that a client
/// must not design as though it did: an award is an update of a row that already exists, so a
/// *fresh* key naming the *same* offer is answered `200` with that offer and nothing further is
/// recorded. A phone that was restarted between attempts is safe. What is refused is a fresh key
/// naming a **different** offer on a job already awarded — `409 conflict`, one job and one accepted
/// bid, enforced by a database constraint rather than by a check.
class AwardController extends Notifier<AwardState> {
  AwardController(this.jobId);

  /// The job being awarded. From the route, which is the only place it comes from.
  final String jobId;

  /// One key for one action. See the note on the class.
  final _key = ActionKey();

  @override
  AwardState build() => const AwardState();

  /// Awards [bidId], and answers whether the platform took it.
  ///
  /// Refuses to run twice: a second tap while the first is in flight, or after the job has been
  /// awarded, does nothing. The platform would answer the second correctly either way — this only
  /// stops the screen sending a request it already knows the answer to.
  Future<bool> award(String bidId) async {
    if (state.awarding || state.awarded) return false;

    final body = <String, Object?>{'bid_id': bidId};
    final key = _key.forRequest(body);
    state = state.copyWith(awarding: true, failure: null);

    try {
      final accepted = await ref.read(biddingRepositoryProvider).awardTo(
            jobId: jobId,
            bidId: bidId,
            idempotencyKey: key,
          );

      _key.settled(null);
      if (!ref.mounted) return true;

      state = state.copyWith(awarding: false, accepted: accepted, failure: null);
      return true;
    } on ApiFailure catch (failure) {
      _key.settled(failure);
      _failed(failure);
      return false;
    } catch (error) {
      // Past `ApiClient`'s mapping: a `200` whose body is not a bid. **The key is kept**, because
      // nothing here establishes whether the award was recorded — the one circumstance `ActionKey`
      // exists for, and `ApiMalformedResponse` is on its unknown-outcome list.
      const failure = ApiMalformedResponse(statusCode: 0);
      _key.settled(failure);
      _failed(failure);
      return false;
    }
  }

  /// Clears the platform's refusal so the customer can choose again.
  ///
  /// It does **not** clear [AwardState.accepted]. An award the platform recorded is not something a
  /// button on this device takes back, and there is no endpoint that would.
  void dismissFailure() => state = state.copyWith(failure: null);

  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(awarding: false, failure: failure);
  }
}

/// Awarding one job, keyed by the job's id.
///
/// **Auto-disposed**, like every other family in this client and for the reason `Docs/07` §3 gives:
/// cached job data goes with the token at sign-out. Kept alive it would hold the accepted offer —
/// its price included — for whoever signed in next on the same handset.
final awardProvider =
    NotifierProvider.autoDispose.family<AwardController, AwardState, String>(AwardController.new);
