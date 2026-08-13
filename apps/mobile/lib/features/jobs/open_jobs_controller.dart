import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';
import 'package:shipper/features/jobs/open_jobs_filter.dart';
import 'package:shipper/features/jobs/open_jobs_repository.dart';

part 'open_jobs_controller.freezed.dart';

/// The jobs this provider may bid on, as much of them as has been read (SHIP-99).
@freezed
abstract class OpenJobsState with _$OpenJobsState {
  const factory OpenJobsState({
    /// The **first** page is being read, or a retry after a failure is.
    ///
    /// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
    /// replacing the feed with a second one would take the work away from somebody who pulled
    /// precisely to look at it.
    @Default(true) bool loading,

    /// A further page is being read.
    @Default(false) bool loadingMore,

    /// Every job read so far, newest first, in the platform's own order.
    @Default(<OpenJob>[]) List<OpenJob> jobs,

    /// Whether a page has arrived at all.
    ///
    /// This is what separates "no work right now" from "not yet asked", and the separation is the
    /// whole of the empty state: a provider with nothing eligible must see an empty state and
    /// never a spinner that does not resolve.
    @Default(false) bool loaded,

    /// The position to ask from next. **Opaque** — passed back exactly as it arrived.
    String? nextCursor,

    /// Whether asking again would return anything.
    @Default(false) bool hasMore,

    /// How the provider has narrowed their own feed.
    ///
    /// **State of the screen, not of the request.** Nothing here is ever sent: the endpoint takes
    /// no filter, and this only hides jobs the platform already offered. It survives a refresh and
    /// a further page on purpose — a provider who narrowed to Victoria and pulled to refresh asked
    /// to see Victoria again, not to have their choice quietly discarded.
    @Default(OpenJobsFilter()) OpenJobsFilter filter,

    /// What the last read failed with, or `null`.
    ApiFailure? failure,
  }) = _OpenJobsState;

  const OpenJobsState._();

  /// The jobs the provider is actually looking at.
  List<OpenJob> get visible => narrowOpenJobs(jobs, filter);

  /// The pickup states worth offering as a control, derived from everything read so far.
  List<String> get pickupStates => pickupStatesIn(jobs);

  /// The statuses worth offering as a control, derived from everything read so far.
  List<JobStatus> get statuses => statusesIn(jobs);

  /// Whether either facet has enough options to be worth drawing.
  bool get hasFacets => facetIsUseful(pickupStates) || facetIsUseful(statuses);

  /// The platform has been asked and this provider is eligible for nothing.
  ///
  /// **Four ordinary situations rather than a fault**: the account has not met the verification
  /// baseline, no service area has been declared, no vehicle is in service, or there is genuinely
  /// no open work matching any of them. The platform answers all four with an empty page, so the
  /// screen says what a provider can do about it rather than reporting an error.
  bool get isEmpty => loaded && jobs.isEmpty;

  /// Jobs arrived, and the provider's own narrowing hides all of them.
  ///
  /// Kept apart from [isEmpty] because they are different things to be told. "There is no work for
  /// you" and "there is none in the states you picked" have different answers, and only one of
  /// them is about the filter.
  bool get isNarrowedToNothing => loaded && jobs.isNotEmpty && visible.isEmpty;

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && jobs.isEmpty && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => !loaded && failure != null;
}

/// Reads the jobs this provider may bid on (SHIP-99).
///
/// ## The feed is the platform's answer, and the filter never touches the request
///
/// `GET /v1/jobs/open` takes `limit` and `cursor` and no filter at all, which is the contract's
/// own position: eligibility is decided by the platform against the provider's service area,
/// vehicles, verification state and the job's own status, and a client parameter would be a second
/// place for that to be argued with (`Docs/07` §3). Every read this controller makes is therefore
/// the same read — the whole eligible feed, a page at a time — and [narrow] only changes which of
/// the jobs already in hand are drawn.
///
/// That has one consequence worth stating, because it is the thing a screen gets wrong: **a filter
/// over a paged list is a filter over what was read**. Narrowing to a state that appears only on
/// page three shows nothing until page three arrives, so the screen says so and offers the next
/// page rather than claiming there is no work.
///
/// ## Paging is explicit
///
/// The page size is server configuration and nothing here names one. Further pages are asked for
/// by the provider rather than swallowed silently; following every cursor automatically would be a
/// client deciding to read an unbounded list to draw one screen.
class OpenJobsController extends Notifier<OpenJobsState> {
  @override
  OpenJobsState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    unawaited(_load(replacing: true));
    return const OpenJobsState();
  }

  /// Reads the first page again, keeping what is on screen while it is in flight.
  ///
  /// Returned rather than awaited internally so `RefreshIndicator` can hold its spinner until the
  /// read finishes — a pull that snapped back before the answer arrived would look like a refresh
  /// that did nothing.
  Future<void> refresh() => _load(replacing: true);

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load(replacing: true);
  }

  /// Reads the next page and appends it.
  Future<void> loadMore() {
    final cursor = state.nextCursor;
    if (cursor == null || state.loadingMore) return Future<void>.value();

    state = state.copyWith(loadingMore: true, failure: null);
    return _load(cursor: cursor, replacing: false);
  }

  /// Narrows, or widens, what is drawn. **No request is made.**
  ///
  /// A single entry point taking the whole filter rather than one method per facet, so that the
  /// invariant "changing the filter never touches the platform" is visible in one place instead of
  /// being a property of three.
  void narrow(OpenJobsFilter filter) {
    state = state.copyWith(filter: filter);
  }

  Future<void> _load({String? cursor, required bool replacing}) async {
    try {
      final page = await ref.read(openJobsRepositoryProvider).openJobs(cursor: cursor);
      if (!ref.mounted) return;

      state = state.copyWith(
        loading: false,
        loadingMore: false,
        loaded: true,
        jobs: replacing ? page.data : <OpenJob>[...state.jobs, ...page.data],
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
        failure: null,
      );
    } on ApiFailure catch (failure) {
      _failed(failure);
    } catch (error) {
      // Past ApiClient's mapping: a page whose rows are not jobs. Nothing a provider can act on,
      // and the same thing to them as any other failure.
      _failed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failure **without discarding the jobs already on screen**.
  ///
  /// A refresh that fails at a loading dock with no signal should leave the provider looking at
  /// the work they had, with a banner saying the reload did not work.
  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, loadingMore: false, failure: failure);
  }
}

/// The provider's feed.
///
/// **Auto-disposed, and that is what clears one provider's feed before the next one signs in.**
/// `Docs/07` §3 requires cached job data to go with the token at sign-out; sign-out unmounts the
/// shell, which drops the last listener, which disposes this. Kept alive it would hold the
/// previous account's eligible work in memory and show it to whoever signed in next on the same
/// handset — and which jobs a competitor is eligible for is information nobody published.
final openJobsProvider = NotifierProvider.autoDispose<OpenJobsController, OpenJobsState>(
  OpenJobsController.new,
);
