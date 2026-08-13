import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/customer_jobs_controller.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';

part 'job_detail_controller.freezed.dart';

/// One job, as its owner is looking at it (SHIP-77).
@freezed
abstract class JobDetailState with _$JobDetailState {
  const factory JobDetailState({
    /// The job is being read, or re-read after a failure.
    ///
    /// Deliberately not set by a pull-to-refresh, for the same reason as the list:
    /// `RefreshIndicator` draws its own spinner, and replacing the job with a second one would
    /// take it away from somebody who pulled precisely to look at it.
    @Default(true) bool loading,

    /// An action is in flight. The screen disables its buttons while it is true, which is what
    /// stops a second tap becoming a second action.
    @Default(false) bool acting,

    /// The job, once it has arrived.
    Job? job,

    /// What the last read or action failed with, or `null`.
    ApiFailure? failure,
  }) = _JobDetailState;

  const JobDetailState._();

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && job == null && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => job == null && failure != null;
}

/// Reads one job, and performs the actions the platform offers on it (SHIP-77).
///
/// ## A family, keyed by the job's id
///
/// The screen is reached by tapping a job, so the id is what identifies this state. Keying the
/// provider by it rather than passing the id into a single shared controller is what makes two
/// jobs opened one after the other two states rather than one that has to be reset — and a reset
/// somebody forgets is a screen showing the previous job's addresses for a frame.
///
/// **Auto-disposed**, like the list, and for the same reason: `Docs/07` §3 requires cached job
/// data to go with the token at sign-out, and a family entry kept alive would hold one account's
/// delivery addresses in memory for whoever signed in next on the same handset. Leaving the
/// screen drops the last listener, which disposes it.
///
/// ## The read is a read, and the action is an action
///
/// `GET /v1/jobs/{id}` carries no idempotency key — it changes nothing, and the middleware lets
/// read-only methods through untouched. The cancellation carries one, minted by [ActionKey] and
/// held across a retry of that same request: a dropped connection after the platform committed is
/// exactly what idempotency exists for, and retrying under the same key replays the stored answer
/// rather than attempting a second transition.
class JobDetailController extends Notifier<JobDetailState> {
  JobDetailController(this.jobId);

  /// The job this controller is about. From the route, which is the only place it comes from.
  final String jobId;

  final _cancelKey = ActionKey();

  @override
  JobDetailState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its
    // first await, so this returns before anything assigns to `state`.
    unawaited(_load());
    return const JobDetailState();
  }

  /// Reads the job again, keeping what is on screen while it is in flight.
  ///
  /// Returned rather than awaited internally so `RefreshIndicator` can hold its spinner until the
  /// read finishes.
  Future<void> refresh() => _load();

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load();
  }

  /// Cancels the job, and returns whether the platform accepted it.
  ///
  /// **A refusal is rendered, not pre-empted.** `Docs/02` §2 decides what may be cancelled and the
  /// platform holds that table; this client offers the button against its own copy of it
  /// (`actionsFor`) and sends the request regardless of what it believes. A `409` means the app
  /// was looking at a stale status — the job was awarded a minute ago on another device — so the
  /// job is **re-read** rather than merely reported, which is `Docs/02` §3.1's rule that on
  /// conflict the server wins and the app reconciles to platform state.
  ///
  /// An already-cancelled job answers `200` and is not an error. The screen needs no special case
  /// for it: the job comes back cancelled either way.
  Future<bool> cancel({String reason = ''}) async {
    if (state.acting) return false;

    final key = _cancelKey.forRequest(cancellationBody(reason: reason));
    state = state.copyWith(acting: true, failure: null);

    try {
      final job = await ref
          .read(jobsRepositoryProvider)
          .cancel(jobId: jobId, reason: reason, idempotencyKey: key);

      _cancelKey.settled(null);
      if (!ref.mounted) return true;

      state = state.copyWith(acting: false, job: job, failure: null);

      // The list this screen was opened from is now wrong about this job's status. Invalidating
      // it is cheaper and more honest than editing one row in another controller's state: the
      // platform decides what the job became, so the list re-reads rather than guessing.
      ref.invalidate(customerJobsProvider);
      return true;
    } on ApiFailure catch (failure) {
      _cancelKey.settled(failure);
      _failedActing(failure);

      // A refusal the platform made against a status this client did not have. Reload, so what
      // the customer is looking at is what the platform holds — while **keeping the refusal on
      // screen**, because a reload that cleared it would leave somebody looking at a job that
      // silently changed under them with no account of why their tap did nothing.
      if (failure is ApiErrorResponse && failure.statusCode == 409) {
        unawaited(_load(keepingFailure: true));
      }
      return false;
    } catch (error) {
      // Past ApiClient's mapping: a `200` whose body is not a job. Nothing here establishes
      // whether the platform cancelled it, so the key is kept and the job is re-read.
      const failure = ApiMalformedResponse(statusCode: 0);
      _cancelKey.settled(failure);
      _failedActing(failure);
      unawaited(_load(keepingFailure: true));
      return false;
    }
  }

  /// Reads the job.
  ///
  /// [keepingFailure] is for the reload that follows a refused action: the job is reconciled to
  /// what the platform holds, and what the platform said about the attempt stays on screen.
  Future<void> _load({bool keepingFailure = false}) async {
    try {
      final job = await ref.read(jobsRepositoryProvider).job(jobId: jobId);
      if (!ref.mounted) return;

      state = state.copyWith(
        loading: false,
        acting: false,
        job: job,
        failure: keepingFailure ? state.failure : null,
      );
    } on ApiFailure catch (failure) {
      _failedLoading(failure);
    } catch (error) {
      // A `200` that is not a job — the contract broken rather than a field added.
      _failedLoading(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failed read **without discarding the job already on screen**.
  ///
  /// A refresh that fails on a train should leave the customer looking at the delivery they had,
  /// with a banner saying the reload did not work.
  void _failedLoading(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, failure: failure);
  }

  void _failedActing(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(acting: false, failure: failure);
  }
}

/// One job's detail, keyed by its id.
final jobDetailProvider = NotifierProvider.autoDispose
    .family<JobDetailController, JobDetailState, String>(JobDetailController.new);
