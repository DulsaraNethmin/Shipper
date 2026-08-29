import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';

part 'job_draft_controller.freezed.dart';

/// One draft, part-way through being described (SHIP-72 to SHIP-75).
@freezed
abstract class JobDraftState with _$JobDraftState {
  const factory JobDraftState({
    /// The draft is being read. True from construction, because the read starts there.
    @Default(true) bool loading,

    /// A save is in flight. Every step disables its submit while it is true, which is what stops
    /// a second tap becoming a second `PATCH` — two taps are two actions, and an idempotency key
    /// is deliberately per-action (`Docs/07` §4).
    @Default(false) bool saving,

    /// The draft as the platform holds it, once it has arrived.
    Job? draft,

    /// What the last read or save failed with, or `null`.
    ApiFailure? failure,
  }) = _JobDraftState;

  const JobDraftState._();

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && draft == null && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => draft == null && failure != null;
}

/// The spine of the job wizard: reads one draft, and saves each step into it.
///
/// ## One controller for four steps, rather than four controllers
///
/// SHIP-71 gave the locations step its own controller because that step is the one that *creates*
/// the draft — it has no id until it succeeds, so there is nothing to key a family by. Every step
/// after it edits a draft that already exists, and they are all the same operation:
/// `PATCH /v1/jobs/{id}` with the fields that step can see. Four controllers would be four copies
/// of the same idempotency handling and four places for the draft to be held at different
/// versions.
///
/// ## Keyed by the job's id, and auto-disposed
///
/// Each step is its own route carrying the id, so the id is what identifies this state — the same
/// decision `JobDetailController` took, and for the same reason: two drafts opened one after the
/// other are two states rather than one that has to be reset.
///
/// **The steps are `push`ed rather than `go`ne to**, which is what makes this efficient: the
/// previous step stays mounted, so it stays listening, so moving from goods to schedule finds the
/// draft already loaded and issues no second read. A customer who deep-links straight into a
/// later step creates the entry fresh and it loads, which is also right.
///
/// Auto-disposed for the reason every job-shaped provider here is: `Docs/07` §3 requires cached
/// job data to go with the token at sign-out, and a family entry kept alive would hold one
/// account's delivery addresses in memory for whoever signed in next on the same handset.
///
/// ## The key is retained only when the outcome is genuinely unknown
///
/// One [ActionKey] serves every step, and that is correct rather than a shortcut:
/// [ActionKey.forRequest] fingerprints the body, so the goods step and the schedule step send
/// different bodies and mint different keys on their own. What it retains is a retry of the *same*
/// body after a failure that left the outcome unknown — a dropped connection after the platform
/// committed, which is exactly what idempotency exists for. A `422` retires the key, because the
/// platform saw the request and refused it: the customer is about to correct something, and a
/// replayed refusal is not what they asked for.
class JobDraftController extends Notifier<JobDraftState> {
  JobDraftController(this.jobId);

  /// The draft this controller is about. From the route, which is the only place it comes from.
  final String jobId;

  final _saveKey = ActionKey();

  @override
  JobDraftState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    unawaited(_load());
    return const JobDraftState();
  }

  /// Sends [fields] as a partial edit, and returns `true` when the platform accepted them.
  ///
  /// **Only the fields the step can see.** `PATCH` touches nothing else, which is what lets a
  /// wizard save one step without sending back the fields on the others — a `PUT` would require
  /// the whole job, and a client that had not been updated for a new field would silently clear
  /// it.
  Future<bool> save(Map<String, Object?> fields) async {
    final key = _saveKey.forRequest(fields);

    state = state.copyWith(saving: true, failure: null);

    try {
      final draft = await ref.read(jobsRepositoryProvider).updateDraft(
            jobId: jobId,
            fields: fields,
            idempotencyKey: key,
          );

      _saveKey.settled(null);
      if (ref.mounted) {
        state = state.copyWith(saving: false, loading: false, draft: draft, failure: null);
      }
      return true;
    } on ApiFailure catch (failure) {
      _saveKey.settled(failure);
      _failedSaving(failure);
      return false;
    } catch (error) {
      // Anything past ApiClient's mapping is a response that was not the one expected. The key is
      // kept as an unknown outcome: nothing here establishes whether the platform stored the edit.
      const failure = ApiMalformedResponse(statusCode: 0);
      _saveKey.settled(failure);
      _failedSaving(failure);
      return false;
    }
  }

  /// Reads the draft again, after a failure the customer chose to retry.
  Future<void> reload() async {
    state = state.copyWith(loading: true, failure: null);
    await _load();
  }

  Future<void> _load() async {
    try {
      final draft = await ref.read(jobsRepositoryProvider).job(jobId: jobId);
      if (!ref.mounted) return;
      state = state.copyWith(loading: false, draft: draft, failure: null);
    } on ApiFailure catch (failure) {
      _failedLoading(failure);
    } catch (error) {
      // A `200` that is not a job — the contract broken rather than a field added.
      _failedLoading(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failed read **without discarding the draft already on screen**.
  ///
  /// A reload that fails on a train should leave the customer looking at the job they were
  /// describing, with a banner saying the refresh did not work.
  void _failedLoading(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, failure: failure);
  }

  void _failedSaving(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(saving: false, failure: failure);
  }
}

/// One draft being described, keyed by its id.
final jobDraftProvider = NotifierProvider.autoDispose
    .family<JobDraftController, JobDraftState, String>(JobDraftController.new);
