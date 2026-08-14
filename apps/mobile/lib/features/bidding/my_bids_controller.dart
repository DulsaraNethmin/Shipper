import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/bidding_repository.dart';

part 'my_bids_controller.freezed.dart';

/// One group of a provider's bids: a status, and the offers in it (SHIP-101).
///
/// A record rather than a map entry so the screen iterates a list and gets `Docs/02` §4's own order
/// without sorting anything — see [MyBidsState.groups].
typedef BidGroup = ({BidStatus status, List<Bid> bids});

/// The offers this provider has made, as much of them as has been read (SHIP-101).
@freezed
abstract class MyBidsState with _$MyBidsState {
  const factory MyBidsState({
    /// The **first** page is being read, or a retry after a failure is.
    ///
    /// Deliberately not set by a pull-to-refresh, for the reason `OpenJobsState` gives: the
    /// `RefreshIndicator` draws its own spinner, and replacing the list with a second one takes the
    /// work away from somebody who pulled precisely to look at it.
    @Default(true) bool loading,

    /// A further page is being read.
    @Default(false) bool loadingMore,

    /// Every offer read so far, newest first, in the platform's own order.
    @Default(<Bid>[]) List<Bid> bids,

    /// Whether a page has arrived at all.
    ///
    /// What separates "you have not bid on anything" from "not yet asked". A provider with no
    /// offers must see an empty state and never a spinner that does not resolve.
    @Default(false) bool loaded,

    /// The position to ask from next. **Opaque** — passed back exactly as it arrived.
    String? nextCursor,

    /// Whether asking again would return anything.
    @Default(false) bool hasMore,

    /// The one group the provider asked the platform for, or `null` for every status.
    ///
    /// **State of the request, unlike `OpenJobsFilter`**, and that difference is the whole reason
    /// this field exists rather than a client-side predicate. `GET /v1/jobs/open` accepts no filter
    /// at all, so a provider narrowing their feed can only hide what was read; `GET /v1/fleet/bids`
    /// accepts `?status=` and runs it in SQL, so asking for one group asks a different question and
    /// gets a page that is whole rather than a page with holes in it.
    BidStatus? only,

    /// What the last read failed with, or `null`.
    ApiFailure? failure,
  }) = _MyBidsState;

  const MyBidsState._();

  /// The offers read so far, grouped by status, in `Docs/02` §4's own order.
  ///
  /// **Derived from `BidStatus.values` rather than from the rows**, which is what fixes the order:
  /// `bid_status.gen.dart` is generated from `contracts/statuses.yaml` in the document's order and
  /// keeps `unknown` last deliberately, "so that a screen grouping by this enumeration gets that
  /// order without writing a second list". This is that screen.
  ///
  /// A status with nothing in it is **not** a group. A provider who has never had an offer expire
  /// should not be shown an empty "Expired" heading, and eight headings over three offers is a
  /// screen that reads as mostly empty.
  List<BidGroup> get groups {
    final grouped = <BidStatus, List<Bid>>{};
    for (final bid in bids) {
      grouped.putIfAbsent(bid.status, () => <Bid>[]).add(bid);
    }

    return <BidGroup>[
      for (final status in BidStatus.values)
        if (grouped[status] case final held? when held.isNotEmpty)
          (status: status, bids: List<Bid>.unmodifiable(held)),
    ];
  }

  /// The statuses worth offering as a control, in the same order.
  ///
  /// **Derived from what has been read and never compiled in**, which is what `CLAUDE.md` requires
  /// of anything that changes under operational pressure — and the honest thing besides. Offering
  /// all eight would put "Draft" on the screen, which is a status no client can ever obtain: there
  /// is no endpoint that creates one and none that returns one.
  ///
  /// It is deliberately **not** cleared when [only] narrows the list, because a control that
  /// vanished the moment it was used would leave a provider with no way back to the other groups.
  /// See [MyBidsController.show].
  List<BidStatus> get offeredGroups => groups.map((group) => group.status).toList(growable: false);

  /// The platform has been asked and this provider has offered nothing.
  ///
  /// **An ordinary state rather than a fault**, and the one that never occurs in development
  /// because whoever is building this has bid on something. A brand-new provider opens this screen
  /// on their first day.
  bool get isEmpty => loaded && bids.isEmpty && only == null;

  /// Offers exist, and the group the provider asked for holds none of them.
  ///
  /// Kept apart from [isEmpty] because they are different things to be told, and only one of them
  /// has "show every status" as its answer.
  bool get isNarrowedToNothing => loaded && bids.isEmpty && only != null;

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && bids.isEmpty && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => !loaded && failure != null;
}

/// Reads the offers this provider has made (SHIP-101).
///
/// ## "Grouped by status" is this screen's job, and the platform made it possible two ways
///
/// `Docs/10` §4.5's collection envelope is a flat array with a cursor, so a response of named
/// buckets would have to page each bucket separately or abandon paging. What the endpoint owes the
/// screen instead is the ability to **ask for one group** — `?status=` — and the status on every row
/// so a client can group without asking twice. Both are used here, and which one is in force is
/// [MyBidsState.only]:
///
/// - **No group chosen**: one read of every status, grouped on the device by [MyBidsState.groups].
/// - **A group chosen**: the platform is asked for that group, in SQL, and the list is whole.
///
/// The distinction matters at a page boundary and nowhere else, which is exactly where it is easy
/// to get wrong: grouping on the device groups **what has been read**, so a status that appears only
/// on page three has no heading until page three arrives. The screen says so rather than implying
/// the list is complete — the same honesty `ProviderJobFeed` owes about its filter chips, arriving
/// by a different route.
///
/// ## Paging is explicit
///
/// The page size is server configuration and nothing here names one. Further pages are asked for by
/// the provider rather than swallowed silently; following every cursor automatically would be a
/// client deciding to read an unbounded list to draw one screen.
///
/// ## Nothing here decides what may be done to an offer
///
/// `Docs/07` §3 and `CLAUDE.md` put every authorisation decision on the platform. [BidStatus.isLive]
/// is presentation — a `submitted` offer this device is looking at may have been accepted, countered
/// or expired a second ago — and what this controller does with it is choose words, never rights.
class MyBidsController extends Notifier<MyBidsState> {
  @override
  MyBidsState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    //
    // **Which group to ask for is a parameter and never `state.only`**, and that is not a
    // stylistic choice: `state` does not exist yet on this path, and reading it here is
    // `Bad state: Tried to read the state of an uninitialized provider` — thrown from the
    // constructor of every screen this controller backs, on the *first* frame, which is the only
    // frame a widget test of an empty list ever gets to.
    unawaited(_load(asked: null, replacing: true));
    return const MyBidsState();
  }

  /// Reads the first page again, keeping what is on screen while it is in flight.
  ///
  /// Returned rather than awaited internally so `RefreshIndicator` can hold its spinner until the
  /// read finishes — a pull that snapped back before the answer arrived would look like a refresh
  /// that did nothing.
  ///
  /// It refreshes **the group the provider is looking at**, not every status. A pull that quietly
  /// widened the list would be a gesture that changed the question.
  Future<void> refresh() => _load(asked: state.only, replacing: true);

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load(asked: state.only, replacing: true);
  }

  /// Asks the platform for one group, or for every status when [status] is `null`.
  ///
  /// **A request rather than a predicate**, which is the difference from `OpenJobsController.narrow`
  /// and is worth being explicit about: this re-reads from the first page, because a cursor issued
  /// for one question does not answer another. Keeping the old cursor would be a client pairing a
  /// position with a list it was never a position in.
  Future<void> show(BidStatus? status) {
    if (status == state.only) return Future<void>.value();

    state = state.copyWith(
      only: status,
      loading: true,
      failure: null,
      // The rows on screen belong to the previous question. Dropping them is what stops one frame
      // of the old group being drawn under the new heading.
      bids: const <Bid>[],
      loaded: false,
      nextCursor: null,
      hasMore: false,
    );
    return _load(asked: status, replacing: true);
  }

  /// Reads the next page and appends it.
  Future<void> loadMore() {
    final cursor = state.nextCursor;
    if (cursor == null || state.loadingMore) return Future<void>.value();

    state = state.copyWith(loadingMore: true, failure: null);
    return _load(asked: state.only, cursor: cursor, replacing: false);
  }

  /// [asked] is the group this read is *for*, taken from the caller rather than from `state` — see
  /// [build]. It is also what the answer is checked against: a provider may pick another group while
  /// a page is in flight, and a page appended under a question nobody asked is worse than one
  /// discarded.
  Future<void> _load({
    required BidStatus? asked,
    String? cursor,
    required bool replacing,
  }) async {
    try {
      final page = await ref.read(biddingRepositoryProvider).myBids(status: asked, cursor: cursor);
      if (!ref.mounted) return;
      if (state.only != asked) return;

      state = state.copyWith(
        loading: false,
        loadingMore: false,
        loaded: true,
        bids: replacing ? page.data : <Bid>[...state.bids, ...page.data],
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
        failure: null,
      );
    } on ApiFailure catch (failure) {
      _failed(failure);
    } catch (error) {
      // Past `ApiClient`'s mapping: a page whose rows are not bids. Nothing a provider can act on,
      // and the same thing to them as any other failure.
      _failed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failure **without discarding the offers already on screen**.
  ///
  /// A refresh that fails at a loading dock with no signal should leave the provider looking at the
  /// offers they had, with a banner saying the reload did not work.
  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, loadingMore: false, failure: failure);
  }
}

/// The provider's own bids.
///
/// **Auto-disposed, and that is what clears one provider's offers before the next one signs in.**
/// `Docs/07` §3 requires cached job data to go with the token at sign-out; sign-out unmounts the
/// shell, which drops the last listener, which disposes this. Kept alive it would hold the previous
/// account's prices in memory and show them to whoever signed in next on the same handset — and what
/// a competitor bid is the one number `Docs/01` §4.3 is most concerned with.
final myBidsProvider = NotifierProvider.autoDispose<MyBidsController, MyBidsState>(
  MyBidsController.new,
);
