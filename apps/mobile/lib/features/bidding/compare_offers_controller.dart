import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';
import 'package:shipper/features/bidding/received_offer.dart';

part 'compare_offers_controller.freezed.dart';

/// How the customer wants the offers ordered (SHIP-102).
///
/// **The sorting is the client's and the platform says so.** `Docs/01` §4.3: the customer's budget
/// is "used only on the customer's side, to filter and sort the bids they receive". The endpoint
/// orders by `created_at` because a keyset cursor has to be over something stable — a price is not,
/// since a provider can revise one between two pages — so the order a customer *wants* is applied
/// here, over the page they are holding.
///
/// That is also the honest limit of it, and [CompareOffersState.sortedIsComplete] is what says so:
/// sorting the offers that have been read is not the same as sorting every offer on the job, and a
/// screen that implied otherwise would tell a customer they had seen the cheapest offer when they
/// had seen the cheapest of the first twenty.
enum OfferOrder {
  /// Cheapest first — the comparison a customer opens this screen to make.
  cheapest,

  /// Earliest collection first, for a customer whose constraint is the date rather than the price.
  soonest,

  /// The order the platform sent, newest offer first.
  newest;

  /// What the control shows. Australian English, sentence case.
  String get label => switch (this) {
        OfferOrder.cheapest => 'Lowest price',
        OfferOrder.soonest => 'Earliest collection',
        OfferOrder.newest => 'Most recent',
      };
}

/// The offers on one of the customer's jobs, as much of them as has been read (SHIP-102).
@freezed
abstract class CompareOffersState with _$CompareOffersState {
  const factory CompareOffersState({
    /// The **first** page is being read, or a retry after a failure is.
    ///
    /// Not set by a pull-to-refresh, for `MyBidsState`'s reason: the `RefreshIndicator` draws its
    /// own spinner and replacing the list with a second one takes the offers away from somebody who
    /// pulled precisely to look at them.
    @Default(true) bool loading,

    /// A further page is being read.
    @Default(false) bool loadingMore,

    /// Every offer read so far, in the platform's own order.
    @Default(<ReceivedOffer>[]) List<ReceivedOffer> offers,

    /// Whether a page has arrived at all.
    ///
    /// What separates "nobody has offered yet" from "not yet asked". A customer whose job was
    /// published a minute ago must see an empty state and never a spinner that does not resolve.
    @Default(false) bool loaded,

    /// The position to ask from next. **Opaque** — passed back exactly as it arrived.
    String? nextCursor,

    /// Whether asking again would return anything.
    @Default(false) bool hasMore,

    /// How the customer wants them ordered.
    @Default(OfferOrder.cheapest) OfferOrder order,

    /// Which offers are being read, or `null` for the platform's own default (SHIP-104).
    ///
    /// `null` while the customer is comparing, which the endpoint reads as `submitted` — the offers
    /// standing right now. It becomes [BidStatus.accepted] after an award, because that is the only
    /// question worth asking then: the award rejected every other live offer in the same
    /// transaction, so the default list would come back **empty** and the screen would draw
    /// "nobody has offered yet" over a delivery that has just been awarded.
    ///
    /// The contract names this as the way back in — "`?status=accepted` is how the awarded offer is
    /// read back afterwards" — so this is reading the platform's record rather than reasoning
    /// locally about what the award did to the list this device is holding.
    BidStatus? status,

    /// What the last read failed with, or `null`.
    ApiFailure? failure,
  }) = _CompareOffersState;

  const CompareOffersState._();

  /// The offers read so far, in the order the customer asked for.
  ///
  /// **A stable sort over a copy**, so that two offers at the same price keep the platform's own
  /// order rather than an arbitrary one that could change between rebuilds — a comparison screen
  /// whose cards swapped places on a repaint is one nobody can read.
  ///
  /// An offer missing the field being sorted on goes last rather than first, in every order. A
  /// price that failed to arrive is not a price of zero, and putting it at the top would recommend
  /// it.
  List<ReceivedOffer> get sorted {
    final ordered = List<ReceivedOffer>.of(offers);
    switch (order) {
      case OfferOrder.cheapest:
        ordered.sort((a, b) => _compare(a.amountCents, b.amountCents));
      case OfferOrder.soonest:
        ordered.sort((a, b) => _compare(a.pickupAt, b.pickupAt));
      case OfferOrder.newest:
        break;
    }
    return List<ReceivedOffer>.unmodifiable(ordered);
  }

  /// Whether [sorted] is an ordering of *every* offer on the job or only of what has been read.
  ///
  /// The screen says which. Sorting a partial list and presenting it as a ranking is the one way
  /// this screen could mislead somebody about the cheapest offer they have, and it is a sentence
  /// rather than a mechanism because the alternative — reading every page before drawing anything —
  /// is a client deciding to fetch an unbounded list to render one screen.
  bool get sortedIsComplete => !hasMore;

  /// The platform has been asked and nobody has offered.
  ///
  /// **An ordinary state rather than a fault**, and the one a customer meets first: a job published
  /// a minute ago has no offers on it and nothing is wrong.
  bool get isEmpty => loaded && offers.isEmpty;

  /// Whether this screen is showing the awarded offer rather than the comparison (SHIP-104).
  bool get showingAccepted => status == BidStatus.accepted;

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && offers.isEmpty && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => !loaded && failure != null;

  /// Orders two values, putting a missing one last whichever direction the comparison runs.
  static int _compare<T extends Comparable<Object>>(T? a, T? b) {
    if (a == null && b == null) return 0;
    if (a == null) return 1;
    if (b == null) return -1;
    return a.compareTo(b);
  }
}

/// Reads the offers on one of the customer's jobs (SHIP-102).
///
/// ## Why this is a family and `MyBidsController` is not
///
/// A provider has one list of their own bids; a customer has one list **per job**, and two job
/// detail screens open at once must not share a cursor. The job identifier is therefore the
/// family's argument, and Riverpod keys one controller per job.
///
/// ## The default is the live offers, and it is the platform's default rather than this screen's
///
/// [BiddingRepository.offersOn] is called with no `status`, and the endpoint reads that as
/// `submitted` — the offers standing right now. This screen deliberately does **not** send
/// `?status=submitted` explicitly: doing so would put a copy of the platform's default in a build
/// that cannot be updated over the air, and the two would part company the first time the platform
/// changed its mind.
///
/// ## Nothing here decides what may be done to an offer
///
/// `Docs/07` §3 and `CLAUDE.md` put every authorisation decision on the platform.
/// [ReceivedOffer.isAwardable] is presentation — an offer this device is looking at may have been
/// withdrawn a second ago — and what this controller does with it is choose what to draw, never
/// what is permitted.
class CompareOffersController extends Notifier<CompareOffersState> {
  CompareOffersController(this.jobId);

  /// The job whose offers this controller reads.
  ///
  /// A constructor parameter rather than `arg`, which is the shape every other family in this app
  /// takes — `JobDetailController`, `OpenJobController`, `TrackingController` and three more. It is
  /// also what keeps [build] readable: the identifier is available before `state` exists, and
  /// reading `state` from `build` is `Bad state: Tried to read the state of an uninitialized
  /// provider` on the first frame of every screen this backs.
  final String jobId;

  @override
  CompareOffersState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    unawaited(_load(replacing: true));
    return const CompareOffersState();
  }

  /// Reads the first page again, keeping what is on screen while it is in flight.
  ///
  /// Returned rather than awaited internally so `RefreshIndicator` can hold its spinner until the
  /// read finishes.
  Future<void> refresh() => _load(replacing: true, status: state.status);

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load(replacing: true, status: state.status);
  }

  /// Reorders what is on screen.
  ///
  /// **No request**, unlike `MyBidsController.show`, and the difference is worth being explicit
  /// about: `?status=` asks the platform a different question and has to re-read from the first
  /// page, while the order is a property of the list this device is holding. Re-reading here would
  /// throw away pages the customer had already asked for, to answer a question the platform does
  /// not accept anyway.
  void orderBy(OfferOrder order) {
    if (order == state.order) return;
    state = state.copyWith(order: order);
  }

  /// Reads the awarded offer back, after this customer has awarded the job (SHIP-104).
  ///
  /// **A read rather than a local edit**, and that is the decision worth naming. The award rejected
  /// every other live offer server-side, so this device could in principle rewrite the list it is
  /// holding — one accepted, the rest rejected — and show that without a round trip. It does not:
  /// `Docs/02` §2 makes status the platform's, and a client that computed the consequences of a
  /// transition would be maintaining a second copy of the state machine that is right until the day
  /// it is not. Asking is one request and cannot be wrong.
  ///
  /// The offers already on screen are dropped rather than kept, because they answered a different
  /// question: they were the live offers, and none of them is live now.
  Future<void> showAwarded() {
    state = state.copyWith(
      status: BidStatus.accepted,
      loading: true,
      loaded: false,
      offers: const <ReceivedOffer>[],
      nextCursor: null,
      hasMore: false,
      failure: null,
    );
    return _load(replacing: true, status: BidStatus.accepted);
  }

  /// Reads the next page and appends it.
  Future<void> loadMore() {
    final cursor = state.nextCursor;
    if (cursor == null || state.loadingMore) return Future<void>.value();

    state = state.copyWith(loadingMore: true, failure: null);
    return _load(cursor: cursor, replacing: false, status: state.status);
  }

  /// Reads one page.
  ///
  /// [status] is a **parameter rather than a read of `state.status`**, and that is not tidiness:
  /// `build` calls this before `state` exists, and every line of a `_load` that ran before the
  /// first suspension would be reading an uninitialised provider — the exact `Bad state` the note
  /// on [jobId] is about. It is passed by every caller that has a state to read it from.
  Future<void> _load({
    String? cursor,
    required bool replacing,
    BidStatus? status,
  }) async {
    try {
      final page = await ref.read(biddingRepositoryProvider).offersOn(
            jobId: jobId,
            status: status,
            cursor: cursor,
          );
      if (!ref.mounted) return;

      state = state.copyWith(
        loading: false,
        loadingMore: false,
        loaded: true,
        offers: replacing ? page.data : <ReceivedOffer>[...state.offers, ...page.data],
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
        failure: null,
      );
    } on ApiFailure catch (failure) {
      _failed(failure);
    } catch (error) {
      // Past `ApiClient`'s mapping: a page whose rows are not offers. Nothing a customer can act
      // on, and the same thing to them as any other failure.
      _failed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failure **without discarding the offers already on screen**.
  ///
  /// A refresh that fails with no signal should leave the customer looking at the offers they had,
  /// with a banner saying the reload did not work.
  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, loadingMore: false, failure: failure);
  }
}

/// The offers on one job, keyed by the job.
///
/// **Auto-disposed, and that is what clears one customer's offers before the next account signs
/// in.** `Docs/07` §3 requires cached job data to go with the token at sign-out; sign-out unmounts
/// the shell, which drops the last listener, which disposes this. Kept alive it would hold every
/// provider's price in memory and show them to whoever signed in next on the same handset — and
/// what a competitor bid is the number `Docs/01` §4.3 is most concerned with.
final compareOffersProvider =
    NotifierProvider.autoDispose.family<CompareOffersController, CompareOffersState, String>(
  CompareOffersController.new,
);
