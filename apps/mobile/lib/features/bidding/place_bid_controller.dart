import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';

part 'place_bid_controller.freezed.dart';

/// Placing one offer against one job (SHIP-100).
@freezed
abstract class PlaceBidState with _$PlaceBidState {
  const factory PlaceBidState({
    /// An offer is on its way to the platform.
    ///
    /// **This is the whole of the "pending" state, and it is deliberately small.** `Docs/07` §4
    /// keeps bidding out of the offline queue, so there is nothing durable to mark as pending and
    /// nothing to reconcile later — either the platform answered or it did not. See the note on
    /// [PlaceBidController].
    @Default(false) bool sending,

    /// The offer the platform recorded, once it has.
    ///
    /// **Never what was typed.** This is assigned from the response and from nothing else, which is
    /// what makes a retry answered `200` with an earlier attempt's offer reconcile the screen to the
    /// platform's record rather than to what is in the form.
    Bid? bid,

    /// What the last attempt failed with, or `null`.
    ApiFailure? failure,
  }) = _PlaceBidState;

  const PlaceBidState._();

  /// Whether the offer is placed and the form has done its job.
  bool get placed => bid != null;

  /// Whether the platform says this provider already has a live offer on this job.
  ///
  /// A `409` rather than a `422`: the values are well formed and the request contradicts the state
  /// the job is in, so the screen says so rather than marking a field as invalid. Branching on the
  /// **code** and never on the message (`Docs/07` §6) — the message is copy and gets reworded.
  bool get alreadyBid => switch (failure) {
        ApiErrorResponse(code: 'bidding_already_bid') => true,
        _ => false,
      };
}

/// Places one bid, once (SHIP-100).
///
/// ## The bid is sent directly and never queued, and `Docs/07` §4 is explicit about why
///
/// "What is deliberately **not** offline: bidding, awarding, and negotiation. These are competitive,
/// time-sensitive, and multi-party; **a stale local decision is worse than an honest 'you are
/// offline'**." A price queued at a loading dock and sent four hours later is an offer against a job
/// that may have been awarded, cancelled or re-priced in the meantime, made by somebody who thinks
/// they have bid.
///
/// SHIP-124 built that refusal into the type system rather than leaving it as a rule: `OperationKind`
/// has a **private constructor** and exactly two members, `delivery.milestone` and `delivery.proof`,
/// so the set of queueable operations is closed by the compiler. A bid cannot be enqueued. This
/// controller therefore never touches `core/queue` or `core/sync`, and there is no
/// `SyncWorker.record` call anywhere in `features/bidding`.
///
/// ## So what stops a dropped connection producing two bids
///
/// [ActionKey], and the platform's stored key column, doing two different halves of the job.
///
/// `Docs/07` §4 has one idempotency key per **action**, minted where the user acted and reused
/// unchanged across every retry of that same action. [ActionKey] holds a key in exactly one
/// circumstance — the previous attempt failed **without saying whether the platform acted on it**
/// and the request now being sent is identical. A dropped connection is precisely that case, so the
/// retry carries the key the first attempt carried.
///
/// What the platform does with it is SHIP-84's second index: `uq_bids_idempotency` on
/// `(job_id, provider_id, idempotency_key)` is a **stored column**, so the retry is answered `200`
/// with the bid the first attempt placed even after any cache has forgotten it. Without that column
/// the retry would meet the *one live offer* index instead and be told `bidding_already_bid` — a
/// client showing a failure for a bid that is live and awaiting an answer.
///
/// And in the other direction: a `422` or a `409` retires the key, because the platform saw the
/// request and refused it. A provider who corrects a mistyped date and submits again is making a
/// **new** offer, and reusing the key there would replay the refusal.
///
/// ## Optimistic local state, and the honest version of it here
///
/// `Docs/02` §3.1 has the client show optimistic local state clearly marked as pending and reconcile
/// to whatever the platform returns. That is about the offline queue, and the half of it that
/// applies to an operation which is never queued is the second half. **Nothing is shown as offered
/// until the platform says so** — [sending] marks the attempt, and [bid] is assigned from the
/// response and never from the form. Showing an offer as placed before the platform confirmed it
/// would be exactly the stale local decision `Docs/07` §4 refuses.
class PlaceBidController extends Notifier<PlaceBidState> {
  PlaceBidController(this.jobId);

  /// The job being bid on. From the route, which is the only place it comes from.
  final String jobId;

  /// One key for one action. See the note on the class.
  final _key = ActionKey();

  @override
  PlaceBidState build() => const PlaceBidState();

  /// Sends the offer, and answers whether the platform took it.
  ///
  /// **A refusal leaves the form open with what was typed in it.** A `422` names the offending
  /// fields and the form renders each message under the input that caused it; closing the form would
  /// throw away the provider's work along with the explanation of what was wrong with it.
  Future<bool> place(BidPlacement bid) async {
    if (state.sending || state.placed) return false;

    final key = _key.forRequest(bid.toJson());
    state = state.copyWith(sending: true, failure: null);

    try {
      final placed = await ref.read(biddingRepositoryProvider).placeBid(
            jobId: jobId,
            bid: bid,
            idempotencyKey: key,
          );

      _key.settled(null);
      if (!ref.mounted) return true;

      state = state.copyWith(sending: false, bid: placed, failure: null);
      return true;
    } on ApiFailure catch (failure) {
      _key.settled(failure);
      _failed(failure);
      return false;
    } catch (error) {
      // Past ApiClient's mapping: a `201` whose body is not a bid. **The key is kept**, because
      // nothing here establishes whether the offer was recorded — which is the one circumstance
      // ActionKey exists for, and `ApiMalformedResponse` is on its unknown-outcome list.
      const failure = ApiMalformedResponse(statusCode: 0);
      _key.settled(failure);
      _failed(failure);
      return false;
    }
  }

  /// Clears the platform's answer so the provider can try again.
  ///
  /// It does **not** clear [PlaceBidState.bid]: an offer the platform recorded is not something a
  /// button on this device takes back. Withdrawing one is `POST /v1/jobs/{id}/bids/{bid_id}/withdraw`
  /// and belongs to the screen that lists a provider's own bids.
  void dismissFailure() => state = state.copyWith(failure: null);

  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(sending: false, failure: failure);
  }
}

/// Placing a bid on one job, keyed by the job's id.
///
/// **Auto-disposed**, like every other read in this client, and for the reason `Docs/07` §3 gives:
/// cached job data goes with the token at sign-out. A family entry kept alive would hold one
/// account's offer — its price included — in memory for whoever signed in next on the same handset.
final placeBidProvider = NotifierProvider.autoDispose
    .family<PlaceBidController, PlaceBidState, String>(PlaceBidController.new);
