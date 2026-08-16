import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';
import 'package:shipper/features/bidding/message.dart';

part 'negotiation_controller.freezed.dart';

/// Which negotiation. One job, one offer in the chain (SHIP-103).
///
/// A record rather than a composed string key, because a family argument has to have value
/// equality and a record has it for free. **Any** offer in the chain addresses the whole of it —
/// the platform's own words — so [bidId] is whichever row the screen was opened from and never
/// needs to move as the negotiation runs on.
typedef NegotiationAddress = ({String jobId, String bidId});

/// One negotiation between one customer and one provider about one job (SHIP-103).
///
/// **The offers and the conversation in one state, because they are one exchange.** `Docs/02` §4
/// runs both through the same two parties, and the *Done when* — "both parties exchange messages
/// and counter-offers against a job" — is a single sentence about a single thing. Two notifiers
/// over two halves of one screen would have to be re-read together after every write anyway, and
/// the coordination is where the bug lives.
@freezed
abstract class NegotiationState with _$NegotiationState {
  const factory NegotiationState({
    /// The negotiation is being read for the first time, or a retry after a failure is.
    @Default(true) bool loading,

    /// Whether anything has arrived at all.
    ///
    /// What separates "nobody has said anything yet" from "not yet asked". A negotiation opened a
    /// second after the offer was placed has no messages and nothing is wrong.
    @Default(false) bool loaded,

    /// Every offer in the chain, **oldest first**, exactly as `…/history` returns them.
    ///
    /// Includes offers that were withdrawn and replaced, each at the amount it was made at.
    /// Nothing is deleted and nothing is overwritten on the platform, so nothing is here either.
    @Default(<Bid>[]) List<Bid> offers,

    /// The conversation, **oldest first** — a conversation is read forward or it is not one.
    @Default(<Message>[]) List<Message> messages,

    /// A further page of the conversation is being read.
    @Default(false) bool loadingMore,

    /// The position to ask from next. **Opaque** — handed back exactly as it arrived.
    String? nextCursor,

    /// Whether asking again would return anything. Only ever about the conversation: the offer
    /// chain does not page.
    @Default(false) bool hasMore,

    /// What the last read failed with, or `null`.
    ApiFailure? failure,

    /// A message is on its way to the platform.
    @Default(false) bool sending,

    /// What the last send failed with, or `null`.
    ApiFailure? sendFailure,

    /// A counter-offer is on its way to the platform.
    @Default(false) bool countering,

    /// What the last counter failed with, or `null`.
    ApiFailure? counterFailure,
  }) = _NegotiationState;

  const NegotiationState._();

  /// The offer everything can still be done to, or `null` when the negotiation has ended.
  ///
  /// **Read off the chain rather than carried as a field**, because that is how the platform
  /// expresses it: "the one entry with no `superseded_by` is the live head, and it is the only
  /// offer anybody can act on". A chain whose head has been withdrawn, rejected or expired has no
  /// live offer at all, which is why [Bid.isLive] is the test rather than "the last row".
  ///
  /// **Presentation only.** What may actually be done to an offer is decided server-side on every
  /// request (`Docs/07` §3): the head this device is looking at may have been countered a second
  /// ago, and the platform will say so.
  Bid? get liveOffer {
    for (final offer in offers.reversed) {
      if (offer.isLive) return offer;
    }
    return null;
  }

  /// Whose answer the negotiation is waiting for, or `null` when it is over.
  ///
  /// Not a fact the platform sends, and not one it needs to: a negotiation alternates, so the party
  /// who made the live offer is the party waiting. It decides a sentence and nothing else.
  BidParty? get awaiting => switch (liveOffer?.offeredBy) {
        BidParty.provider => BidParty.customer,
        BidParty.customer => BidParty.provider,
        _ => null,
      };

  /// Whether [viewer] is the one who should answer.
  bool isAwaiting(BidParty? viewer) => viewer != null && awaiting == viewer;

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && offers.isEmpty && messages.isEmpty && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => !loaded && failure != null;

  /// The platform has been asked and nobody has written anything.
  ///
  /// An ordinary state and the first one either party meets, exactly as an empty offer list is on
  /// the comparison screen.
  bool get conversationIsEmpty => loaded && messages.isEmpty;

  /// What to tell the person when the platform refused their counter-offer.
  ///
  /// **Branching on `error.code` and never on the message** (`Docs/07` §6, `CLAUDE.md`).
  /// `contracts/paths/bidding.yaml`'s own table for this endpoint is the authority for which code
  /// leads where; this turns each into the sentence somebody reads. `null` when there is nothing
  /// specific to say and the generic banner covers it.
  String? get counterRefusal => switch (counterFailure) {
        // Countering your own offer. The screen does not offer the button in that case, so reaching
        // this means the head changed hands underneath — the other party answered while this form
        // was open, which is exactly what a negotiation does.
        ApiErrorResponse(code: 'bidding_wrong_party') =>
          'That offer is now yours to wait on rather than to answer — the other party has already '
              'replied. Reload the negotiation to see where it stands.',

        // The customer awarded it. There is nothing further to negotiate and saying so is kinder
        // than a generic failure.
        ApiErrorResponse(code: 'bidding_bid_accepted') =>
          'This delivery has been awarded. The terms are settled, and you can still send a message '
              'to arrange the details.',

        // Superseded, withdrawn, rejected or expired — or the job can no longer be awarded. One
        // code covers all of them and this device cannot tell which, so it says the one thing true
        // of every case.
        ApiErrorResponse(code: 'bidding_bid_closed') =>
          'This negotiation is closed. The offer has been answered, withdrawn or has expired, so '
              'there is nothing left to counter.',

        // The key identified a different request. The next attempt mints a new one, so trying again
        // is the right advice rather than something to hide.
        ApiErrorResponse(code: 'idempotency_key_reused') =>
          'That did not go through. Send the counter-offer again.',

        // A negotiation that is not yours and one that does not exist are the same answer, by
        // design — so this cannot say which, and must not guess.
        ApiErrorResponse(statusCode: 404) =>
          'This negotiation could not be found. Open it again from the offer.',

        _ => null,
      };

  /// The platform's field-level rejections of the counter-offer, keyed by field.
  ///
  /// **It can name a field this device did not send**, and the form has to render it: the platform
  /// validates the *merged* offer, so countering on price alone against an offer whose collection
  /// time has since passed is a `422` naming `pickup_at`. A form that only showed messages for
  /// fields somebody had typed into would drop the explanation entirely.
  Map<String, String> get counterFieldMessages => switch (counterFailure) {
        ApiErrorResponse(statusCode: 422, :final fieldMessages) => fieldMessages,
        _ => const <String, String>{},
      };

  /// What to tell the person when the platform refused their message.
  ///
  /// Shorter than [counterRefusal] because the endpoint has fewer ways to say no: there is no
  /// status rule on a conversation at all, so the refusals left are "you are not a party to this"
  /// and "that is not a message".
  String? get sendRefusal => switch (sendFailure) {
        ApiErrorResponse(statusCode: 422, :final fieldMessages) when fieldMessages.isNotEmpty =>
          fieldMessages.values.first,
        ApiErrorResponse(statusCode: 404) =>
          'This conversation could not be found. Open it again from the offer.',
        ApiErrorResponse(code: 'idempotency_key_reused') =>
          'That did not go through. Send the message again.',
        _ => null,
      };
}

/// Reads one negotiation and writes into it (SHIP-103).
///
/// ## One controller for a read and two writes, which is a departure worth justifying
///
/// `CompareOffersController` reads and `AwardController` writes, and the split is right there: an
/// award is one irreversible act whose confirmation is a property of a screen, and the list it
/// disturbs answers a different question afterwards. Here both writes **append to the very thing
/// this controller holds** — a message joins the conversation, a counter joins the chain — and both
/// have to be reflected the moment the platform answers. Two notifiers would coordinate through the
/// screen, which is where that bug lives.
///
/// ## Neither write is queued, and the compiler is what says so
///
/// `Docs/07` §4 lists what is deliberately **not** offline: "bidding, awarding, and negotiation.
/// These are competitive, time-sensitive, and multi-party; a stale local decision is worse than an
/// honest 'you are offline.'" A counter-offer composed at a loading dock and sent four hours later
/// answers an offer that may since have been awarded. `OperationKind` has a private constructor and
/// exactly two members, both `delivery.*`, so neither of these can be enqueued — the set is closed
/// by the compiler rather than by a rule somebody follows, and nothing in this file touches
/// `core/queue` or `core/sync`.
///
/// ## Two keys, not one, and that is not tidiness
///
/// [ActionKey] holds a key only while the previous attempt failed **without saying whether the
/// platform acted**. One shared key would make sending a message retire the key a half-finished
/// counter-offer was holding, and the retry of that counter would then arrive with a fresh key —
/// which the platform reads as a **second counter**. Two independent actions get two independent
/// keys.
///
/// ## A counter re-reads the chain rather than patching it
///
/// The response is the new counter and nothing else, and the platform also moved the offer it
/// answered to `superseded` and pointed its `superseded_by` at the new row. This device could write
/// both of those itself. It does not: `Docs/02` §2 makes status the platform's, and a client that
/// computed the consequences of a transition keeps a second copy of the state machine that is right
/// until the day it is not. Asking is one request and cannot be wrong — the same call
/// `CompareOffersController.showAwarded` makes.
///
/// **A message does not re-read**, and the asymmetry is the point: sending one changes nothing
/// except that there is one more message, and the platform answers with exactly that row. There is
/// no second fact to reconcile.
class NegotiationController extends Notifier<NegotiationState> {
  NegotiationController(this.address);

  /// The negotiation this controller reads. From the route, which is the only place it comes from.
  ///
  /// A constructor parameter rather than `arg`, which is the shape every other family in this app
  /// takes: the identifiers are available before `state` exists, and reading `state` from [build]
  /// is `Bad state: Tried to read the state of an uninitialized provider` on the first frame.
  final NegotiationAddress address;

  /// One key per action, and two actions. See the note on the class.
  final _messageKey = ActionKey();
  final _counterKey = ActionKey();

  @override
  NegotiationState build() {
    // Reading the repository is synchronous and the requests are not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    unawaited(_load());
    return const NegotiationState();
  }

  /// Reads the whole negotiation again, keeping what is on screen while it is in flight.
  ///
  /// Returned rather than awaited internally so a `RefreshIndicator` can hold its spinner until the
  /// read finishes.
  Future<void> refresh() => _load();

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load();
  }

  /// Reads the next page of the conversation and appends it.
  ///
  /// **Appends rather than prepends.** The conversation arrives oldest first, so a further page
  /// walks *towards the present* — the opposite direction from every other list in this client, and
  /// the reason `has_more` here means "there is more of the conversation after this".
  Future<void> loadMore() {
    final cursor = state.nextCursor;
    if (cursor == null || state.loadingMore) return Future<void>.value();

    state = state.copyWith(loadingMore: true, failure: null);
    return _loadMessages(cursor: cursor, replacing: false);
  }

  /// Sends one message to the other party, and answers whether the platform took it.
  ///
  /// **The message appended is the platform's row, never what was typed.** A retry answered `200`
  /// with an earlier attempt's message therefore reconciles the thread to the platform's record
  /// rather than to what is in the composer — which is the case that actually happens, because a
  /// phone that lost its connection retries under the key it already holds.
  ///
  /// The trimming here is the composer's own: an empty message is a `422` and there is no reason to
  /// spend a round trip discovering it. The platform trims too, and this client does not otherwise
  /// alter a word of what somebody wrote.
  Future<bool> send(String body) async {
    final trimmed = body.trim();
    if (trimmed.isEmpty || state.sending) return false;

    final key = _messageKey.forRequest(<String, Object?>{'body': trimmed});
    state = state.copyWith(sending: true, sendFailure: null);

    try {
      final sent = await ref.read(biddingRepositoryProvider).sendMessage(
            jobId: address.jobId,
            bidId: address.bidId,
            body: trimmed,
            idempotencyKey: key,
          );

      _messageKey.settled(null);
      if (!ref.mounted) return true;

      state = state.copyWith(
        sending: false,
        sendFailure: null,
        messages: <Message>[...state.messages, sent],
      );
      return true;
    } on ApiFailure catch (failure) {
      _messageKey.settled(failure);
      if (!ref.mounted) return false;
      state = state.copyWith(sending: false, sendFailure: failure);
      return false;
    } catch (error) {
      // Past `ApiClient`'s mapping: a `201` whose body is not a message. **The key is kept**,
      // because nothing here establishes whether it was recorded — the one circumstance `ActionKey`
      // exists for, and `ApiMalformedResponse` is on its unknown-outcome list. A message sent twice
      // is the duplicate the contract calls worse than most.
      const failure = ApiMalformedResponse(statusCode: 0);
      _messageKey.settled(failure);
      if (!ref.mounted) return false;
      state = state.copyWith(sending: false, sendFailure: failure);
      return false;
    }
  }

  /// Counters the live head with [counter], and answers whether the platform took it.
  ///
  /// The offer countered is **the live head as the platform last reported it**, not the row this
  /// screen was opened from: a negotiation that has run on has a different head, and countering the
  /// row in the route would be answering an offer that was superseded three rounds ago. When there
  /// is no live head the request is not made — the platform would refuse it `bidding_bid_closed`,
  /// and there is nothing to learn from asking.
  Future<bool> counter(BidCounter counter) async {
    final head = state.liveOffer;
    if (head == null || !counter.namesSomething || state.countering) return false;

    final key = _counterKey.forRequest(counter.toJson());
    state = state.copyWith(countering: true, counterFailure: null);

    try {
      await ref.read(biddingRepositoryProvider).counterOffer(
            jobId: address.jobId,
            bidId: head.id,
            counter: counter,
            idempotencyKey: key,
          );

      _counterKey.settled(null);
      if (!ref.mounted) return true;

      state = state.copyWith(countering: false, counterFailure: null);
      // The response is the new counter alone, and the platform also superseded the offer it
      // answered. Re-read rather than reason about it here — see the note on the class.
      await _loadHistory();
      return true;
    } on ApiFailure catch (failure) {
      _counterKey.settled(failure);
      if (!ref.mounted) return false;
      state = state.copyWith(countering: false, counterFailure: failure);
      return false;
    } catch (error) {
      const failure = ApiMalformedResponse(statusCode: 0);
      _counterKey.settled(failure);
      if (!ref.mounted) return false;
      state = state.copyWith(countering: false, counterFailure: failure);
      return false;
    }
  }

  /// Clears the platform's refusal of a counter-offer so the form can be corrected and sent again.
  void dismissCounterFailure() => state = state.copyWith(counterFailure: null);

  /// Clears the platform's refusal of a message.
  void dismissSendFailure() => state = state.copyWith(sendFailure: null);

  /// Reads both halves.
  ///
  /// **Sequentially rather than concurrently**, and that is a decision rather than an oversight: the
  /// two reads share one failure field, and two in flight at once make "which one failed" a race
  /// whose answer differs between runs. A negotiation is two small requests on a screen somebody
  /// opened deliberately.
  Future<void> _load() async {
    await _loadHistory();
    if (!ref.mounted || state.failure != null) return;
    await _loadMessages(replacing: true);
  }

  Future<void> _loadHistory() async {
    try {
      final chain = await ref.read(biddingRepositoryProvider).negotiationHistory(
            jobId: address.jobId,
            bidId: address.bidId,
          );
      if (!ref.mounted) return;

      // `loaded` is deliberately **not** set here. It means "the conversation has been asked for",
      // which is what the empty state turns on, and the chain arriving says nothing about that.
      state = state.copyWith(offers: chain.data, failure: null);
    } on ApiFailure catch (failure) {
      _readFailed(failure);
    } catch (error) {
      _readFailed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  Future<void> _loadMessages({String? cursor, required bool replacing}) async {
    try {
      final page = await ref.read(biddingRepositoryProvider).messagesOn(
            jobId: address.jobId,
            bidId: address.bidId,
            cursor: cursor,
          );
      if (!ref.mounted) return;

      state = state.copyWith(
        loading: false,
        loadingMore: false,
        loaded: true,
        messages: replacing ? page.data : <Message>[...state.messages, ...page.data],
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
        failure: null,
      );
    } on ApiFailure catch (failure) {
      _readFailed(failure);
    } catch (error) {
      _readFailed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a read failure **without discarding what is already on screen**.
  ///
  /// A refresh that fails with no signal should leave both parties looking at the exchange they
  /// had, with a banner saying the reload did not work.
  void _readFailed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, loadingMore: false, failure: failure);
  }
}

/// One negotiation, keyed by the job and the offer it is addressed through.
///
/// **Auto-disposed, and that is what clears one account's negotiation before the next signs in.**
/// `Docs/07` §3 requires cached job data to go with the token at sign-out; sign-out unmounts the
/// shell, which drops the last listener, which disposes this. Kept alive it would hold a
/// competitor's prices and both parties' words in memory and show them to whoever signed in next on
/// the same handset.
final negotiationProvider = NotifierProvider.autoDispose
    .family<NegotiationController, NegotiationState, NegotiationAddress>(NegotiationController.new);
