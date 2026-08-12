import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';

part 'customer_jobs_controller.freezed.dart';

/// One status and the jobs the customer has in it.
typedef JobGroup = ({JobStatus status, List<Job> jobs});

/// The customer's own jobs, as much of them as has been read (SHIP-76).
@freezed
abstract class CustomerJobsState with _$CustomerJobsState {
  const factory CustomerJobsState({
    /// The **first** page is being read, or a retry after a failure is.
    ///
    /// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
    /// replacing the list with a second one would take the jobs away from somebody who pulled
    /// precisely to look at them.
    @Default(true) bool loading,

    /// A further page is being read.
    @Default(false) bool loadingMore,

    /// Every job read so far, newest first, in the platform's own order.
    @Default(<Job>[]) List<Job> jobs,

    /// Whether a page has arrived at all.
    ///
    /// This is what separates "no jobs" from "not yet asked", and the separation is the whole
    /// of the empty state: a customer with nothing must see an empty state and never a spinner
    /// that does not resolve.
    @Default(false) bool loaded,

    /// The position to ask from next. **Opaque** — passed back exactly as it arrived.
    String? nextCursor,

    /// Whether asking again would return anything.
    @Default(false) bool hasMore,

    /// What the last read failed with, or `null`.
    ApiFailure? failure,
  }) = _CustomerJobsState;

  const CustomerJobsState._();

  /// The customer has been asked and has nothing.
  bool get isEmpty => loaded && jobs.isEmpty;

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && jobs.isEmpty && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => !loaded && failure != null;

  /// The jobs grouped by status, in `Docs/02` §1's order.
  List<JobGroup> get groups => groupJobsByStatus(jobs);
}

/// Groups jobs by status, in the order `Docs/02` §1 lists them.
///
/// **The order comes from `JobStatus.values` rather than from a list written here**, which is
/// what stops a second ordering existing: the enum is declared in the document's own order, so a
/// status added in the right place is grouped in the right place with nothing else to edit.
/// A status with no jobs produces no heading.
///
/// A pure function so the grouping can be read and tested without a screen. It is the whole of
/// what "grouped by status" means, and a widget test asserting on headings would be testing the
/// layout as well.
List<JobGroup> groupJobsByStatus(List<Job> jobs) {
  final byStatus = <JobStatus, List<Job>>{};
  for (final job in jobs) {
    byStatus.putIfAbsent(job.status, () => <Job>[]).add(job);
  }

  return <JobGroup>[
    for (final status in JobStatus.values)
      if (byStatus[status] case final group?) (status: status, jobs: group),
  ];
}

/// Reads the customer's own jobs (SHIP-76).
///
/// ## The list is read once and grouped here, not filtered twelve times
///
/// `GET /v1/jobs` takes `?status=`, and it takes **one** value. A screen that shows every status
/// a customer has would therefore need one request per status to use it — twelve round trips on
/// mobile data to draw one screen, and twelve chances for a partial failure to produce a screen
/// that is missing a group with no error to explain it. The contract recommends the opposite and
/// this follows it: one read, grouped on the device where it costs nothing.
///
/// ## Paging is explicit, because grouping a partial list is partial
///
/// The page size is server configuration and nothing here names one. What that means for a screen
/// grouped by status is that a group holds *the jobs that have been read*, not every job in that
/// status — so the further pages are asked for by the customer rather than swallowed silently,
/// and the screen says that it is showing the most recent jobs. Following every cursor
/// automatically would be a client deciding to read an unbounded list to draw one screen.
class CustomerJobsController extends Notifier<CustomerJobsState> {
  @override
  CustomerJobsState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    unawaited(_load(replacing: true));
    return const CustomerJobsState();
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

  Future<void> _load({String? cursor, required bool replacing}) async {
    try {
      final page = await ref.read(jobsRepositoryProvider).jobs(cursor: cursor);
      if (!ref.mounted) return;

      state = state.copyWith(
        loading: false,
        loadingMore: false,
        loaded: true,
        jobs: replacing ? page.data : <Job>[...state.jobs, ...page.data],
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
        failure: null,
      );
    } on ApiFailure catch (failure) {
      _failed(failure);
    } catch (error) {
      // Past ApiClient's mapping: a page whose rows are not jobs. Nothing a customer can act on,
      // and the same thing to them as any other failure.
      _failed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failure **without discarding the jobs already on screen**.
  ///
  /// A refresh that fails on a train should leave the customer looking at the list they had, with
  /// a banner saying the reload did not work. Clearing it would be the app punishing somebody for
  /// pulling to refresh in a tunnel.
  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, loadingMore: false, failure: failure);
  }
}

/// The customer's job list.
///
/// **Auto-disposed, and that is what clears one customer's jobs before the next one signs in.**
/// `Docs/07` §3 requires cached job data to go with the token at sign-out; sign-out unmounts the
/// shell, which drops the last listener, which disposes this. Kept alive it would hold the
/// previous account's jobs in memory and show them to whoever signed in next on the same handset
/// — on a shared phone that is a disclosure rather than a stale screen.
///
/// A token refresh does *not* dispose it: the shell rebuilds but the list widget stays mounted,
/// so nothing is re-read. Leaving the shell for the job wizard and coming back does, which is
/// how a draft just saved appears without anybody having to pull.
final customerJobsProvider =
    NotifierProvider.autoDispose<CustomerJobsController, CustomerJobsState>(
  CustomerJobsController.new,
);
