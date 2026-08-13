import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';

part 'job_locations_controller.freezed.dart';

/// What the locations step is doing right now (SHIP-71).
@freezed
abstract class JobLocationsState with _$JobLocationsState {
  const factory JobLocationsState({
    /// A save is in flight. The screen disables its submit while it is true, which is what stops
    /// a second tap becoming a second **draft** — two taps are two actions, and an idempotency
    /// key is deliberately per-action (`Docs/07` §4).
    @Default(false) bool busy,

    /// The draft as the platform stored it, once it has been saved.
    ///
    /// This is what the screen shows back: the normalised addresses, and whether each one was
    /// matched to a place. It is also what makes the next save an **edit** rather than a second
    /// draft — see [JobLocationsController.save].
    Job? draft,

    /// Whether the customer has asked to change the addresses of a draft already saved.
    ///
    /// The step has two faces — the form, and what the platform made of what was typed — and
    /// which one is showing is a fact about the step rather than about the widget. Keeping it
    /// here is what lets the sequence be read, and tested, without a screen.
    @Default(false) bool editing,

    /// What the last attempt failed with, or `null`. Cleared when the next one starts.
    ApiFailure? failure,
  }) = _JobLocationsState;

  const JobLocationsState._();

  /// Whether the form is the thing to show.
  bool get showingForm => draft == null || editing;
}

/// Captures the pickup and drop-off addresses of a new job (SHIP-71).
///
/// This is the first step of the job wizard, and the step that creates the draft everything after
/// it edits. `Docs/01` §4.1 lets a customer save and come back, so saving here is a real
/// commitment rather than a staging area: the platform holds the draft, not the phone.
///
/// ## Saving twice edits the draft, and does not make a second one
///
/// The first save is `POST /v1/jobs`; every save after it is `PATCH /v1/jobs/{id}` against the id
/// the first one returned. Without that, a customer who corrected a postcode and saved again
/// would leave an abandoned draft behind on every correction — and would see it in their job list
/// (SHIP-76), which is where they would notice it and where nothing could explain it.
///
/// ## The key is retained only when the outcome is genuinely unknown
///
/// [ActionKey] mints one key per action and keeps it across a retry of *that same request*. A
/// dropped connection after the platform committed is exactly what idempotency exists for: the
/// retry replays the stored answer instead of creating a second draft. A `422` retires the key,
/// because the platform saw the request and refused it — the customer is about to correct
/// something, and a replayed refusal is not what they asked for.
class JobLocationsController extends Notifier<JobLocationsState> {
  final _key = ActionKey();

  @override
  JobLocationsState build() => const JobLocationsState();

  /// Sends both addresses, and returns `true` when the platform accepted them.
  ///
  /// **An address the platform could not place is not a failure.** SHIP-59a requires that a
  /// lookup which did not resolve does not fail the job, so `true` here means "the draft is
  /// stored" and says nothing about whether either address was matched to a place. What was
  /// matched is on [JobLocationsState.draft], for the screen to show back.
  Future<bool> save({required AddressInput pickup, required AddressInput dropoff}) async {
    final fields = locationsBody(pickup: pickup, dropoff: dropoff);
    final key = _key.forRequest(fields);
    final existing = state.draft;

    state = state.copyWith(busy: true, failure: null);

    try {
      final repository = ref.read(jobsRepositoryProvider);
      final draft = existing == null
          ? await repository.createDraft(fields: fields, idempotencyKey: key)
          : await repository.updateDraft(
              jobId: existing.id,
              fields: fields,
              idempotencyKey: key,
            );

      _key.settled(null);
      if (ref.mounted) state = JobLocationsState(draft: draft);
      return true;
    } on ApiFailure catch (failure) {
      _key.settled(failure);
      if (ref.mounted) state = state.copyWith(busy: false, failure: failure);
      return false;
    } catch (error) {
      // Anything past ApiClient's mapping is a response that was not the one expected — a job
      // with no `id` is the realistic case. The key is kept as an unknown outcome: nothing here
      // establishes whether the platform stored the draft.
      const failure = ApiMalformedResponse(statusCode: 0);
      _key.settled(failure);
      if (ref.mounted) state = state.copyWith(busy: false, failure: failure);
      return false;
    }
  }

  /// Shows the form again, keeping the draft so the next save edits it rather than making a
  /// second one.
  void editAgain() {
    if (state.draft != null) state = state.copyWith(editing: true, failure: null);
  }
}

/// The locations step's state.
///
/// Auto-disposed, unlike the sign-in and signup states, and the reason is what it holds: a
/// **draft id**. A customer who left the wizard and started a new job later must not have the
/// second one silently edit the first, and disposing when the screen goes is what makes "start a
/// job" mean a new job. Resuming a draft deliberately is SHIP-75, and arrives with an id from the
/// job list rather than from a provider that outlived its screen.
final jobLocationsProvider =
    NotifierProvider.autoDispose<JobLocationsController, JobLocationsState>(
  JobLocationsController.new,
);
