import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/open_job.dart';
import 'package:shipper/features/jobs/open_jobs_repository.dart';

part 'open_job_controller.freezed.dart';

/// One job, as a provider deciding whether to bid is looking at it (SHIP-100).
@freezed
abstract class OpenJobState with _$OpenJobState {
  const factory OpenJobState({
    /// The job is being read, or re-read after a failure.
    ///
    /// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
    /// replacing the job with a second one would take it away from somebody who pulled precisely to
    /// look at it.
    @Default(true) bool loading,

    /// The job, once it has arrived.
    OpenJob? job,

    /// What the last read failed with, or `null`.
    ApiFailure? failure,
  }) = _OpenJobState;

  const OpenJobState._();

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && job == null && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => job == null && failure != null;

  /// Whether the platform said there is no such job **for this caller**.
  ///
  /// `404` covers a job that does not exist, a job outside this provider's eligibility, and the
  /// owning customer reading their own job at the wrong address — one answer, byte-identical, on
  /// purpose (SHIP-83). The screen uses this only to choose between "we could not reach Shipper,
  /// try again" and "this job is not one you can bid on", and it must not try to say which of the
  /// three it was.
  bool get notFound => switch (failure) {
        ApiErrorResponse(:final statusCode) => statusCode == 404,
        _ => false,
      };
}

/// Reads one job a provider may bid on (SHIP-100).
///
/// ## A family, keyed by the job's id
///
/// The screen is reached by tapping a job in the feed or by following a deep link, so the id is what
/// identifies this state. Keying the provider by it rather than resetting one shared controller is
/// what makes two jobs opened one after the other two states — and a reset somebody forgets is a
/// screen showing the previous job's suburb for a frame.
///
/// ## Nothing is handed down from the feed
///
/// The feed has an `OpenJob` in hand already and it is deliberately not passed here. Two reasons:
/// the feed's copy may be ten minutes old, and a job that has since been awarded or cancelled is
/// exactly the one a provider must not be shown as biddable; and a route reached by a deep link has
/// no feed behind it at all, so a screen that depended on one would work from a card and not from a
/// notification.
class OpenJobController extends Notifier<OpenJobState> {
  OpenJobController(this.jobId);

  /// The job this controller is about. From the route, which is the only place it comes from.
  final String jobId;

  @override
  OpenJobState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    unawaited(_load());
    return const OpenJobState();
  }

  /// Reads the job again, keeping what is on screen while it is in flight.
  Future<void> refresh() => _load();

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load();
  }

  Future<void> _load() async {
    try {
      final job = await ref.read(openJobsRepositoryProvider).openJob(jobId: jobId);
      if (!ref.mounted) return;

      state = state.copyWith(loading: false, job: job, failure: null);
    } on ApiFailure catch (failure) {
      _failed(failure);
    } catch (error) {
      // Past ApiClient's mapping: a `200` whose body is not a job — the contract broken rather than
      // a field added.
      _failed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failure **without discarding the job already on screen**.
  ///
  /// A refresh that fails at a loading dock with no signal should leave the provider looking at the
  /// job they were reading, with a banner saying the reload did not work.
  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, failure: failure);
  }
}

/// One open job, keyed by its id.
///
/// **Auto-disposed**, like the feed, and for the reason `Docs/07` §3 gives: cached job data goes
/// with the token at sign-out. A family entry kept alive would hold one account's eligible work in
/// memory for whoever signed in next on the same handset, and which jobs a competitor is eligible
/// for is information nobody published.
final openJobProvider = NotifierProvider.autoDispose
    .family<OpenJobController, OpenJobState, String>(OpenJobController.new);
