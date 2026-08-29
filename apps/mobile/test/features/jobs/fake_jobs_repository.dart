import 'dart:async';

import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';

/// A job the platform could have returned.
///
/// Built from the `Job` example in `contracts/paths/jobs.yaml` so that a field renamed in the
/// contract shows up here rather than only on a device.
Job aJob({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  JobStatus status = JobStatus.draft,
  JobLocation? pickup,
  JobLocation? dropoff,
  int? budgetCents,
  String? expiresAt,
  String createdAt = '2026-08-11T03:30:00.000Z',
}) {
  return Job(
    id: id,
    status: status,
    pickup: pickup,
    dropoff: dropoff,
    budgetCents: budgetCents,
    expiresAt: expiresAt,
    createdAt: createdAt,
    updatedAt: createdAt,
  );
}

/// An address the platform matched to a place.
JobLocation aResolvedLocation({
  String line = '12 Smith Street',
  String suburb = 'Newtown',
  String state = 'NSW',
  String postcode = '2042',
  String? formatted = '12 Smith Street, Newtown NSW 2042',
}) {
  return JobLocation(
    line: line,
    suburb: suburb,
    state: state,
    postcode: postcode,
    coordinate: Coordinate(latitude: -33.896, longitude: 151.179, formatted: formatted),
  );
}

/// An address the platform stored exactly as it was typed, with no coordinate.
///
/// **Not an error state.** SHIP-59a requires that a failed lookup does not fail the job, so this
/// is an ordinary outcome and the screens have to treat it as one.
JobLocation anUnresolvedLocation({
  String line = 'Lot 14 Boundary Road',
  String suburb = 'Coonabarabran',
  String state = 'NSW',
  String postcode = '2357',
}) {
  return JobLocation(line: line, suburb: suburb, state: state, postcode: postcode);
}

/// One recorded call.
typedef JobsCall = ({
  String action,
  String? jobId,
  Map<String, Object?> fields,
  String? cursor,
  String? idempotencyKey,
});

/// A [JobsRepository] that answers from a script and records what it was asked.
///
/// The screens are tested against this rather than against a stub transport, because a widget
/// test that also exercises `dio`'s wiring fails for two reasons and reads as one. What actually
/// reaches the wire is `jobs_repository_test.dart`'s subject, against the contract.
///
/// It records the **idempotency key of every write**, which is what makes the rule in `Docs/07`
/// §4 assertable: one key per action, kept across a retry of that action, and a new one when the
/// action changes.
class FakeJobsRepository implements JobsRepository {
  final calls = <JobsCall>[];

  /// What a create or an edit answers with.
  ///
  /// A function rather than a value so a test can answer with the addresses it was given, which
  /// is what the platform does — the locations screen shows back what was stored.
  Job Function(Map<String, Object?> fields) draft = (_) => aJob();

  /// What the list answers with.
  ApiPage<Job> page = const ApiPage<Job>(data: <Job>[]);

  /// What `GET /v1/jobs/{id}` answers with (SHIP-77).
  ///
  /// A function of the id, because the detail screen is reached with one and a fake that ignored
  /// it would let a screen showing the wrong job pass. It answers the **same shape** the list
  /// does, which is the contract: one `Job` schema for create, edit, read and list alike.
  Job Function(String jobId) detail = (jobId) => aJob(id: jobId);

  /// Set to make the next call of that action throw instead of answering.
  final failures = <String, Object>{};

  /// Set to hold the next call of that action open, so a test can assert what a screen shows
  /// while a request is in flight.
  final gates = <String, Completer<void>>{};

  List<JobsCall> callsTo(String action) => calls.where((c) => c.action == action).toList();

  Future<T> _record<T>(JobsCall call, T Function() answer) async {
    calls.add(call);

    final gate = gates.remove(call.action);
    if (gate != null) await gate.future;

    final failure = failures.remove(call.action);
    if (failure != null) throw failure;

    return answer();
  }

  @override
  Future<Job> createDraft({
    required Map<String, Object?> fields,
    required String idempotencyKey,
  }) {
    return _record(
      (
        action: 'create',
        jobId: null,
        fields: fields,
        cursor: null,
        idempotencyKey: idempotencyKey,
      ),
      () => draft(fields),
    );
  }

  @override
  Future<Job> updateDraft({
    required String jobId,
    required Map<String, Object?> fields,
    required String idempotencyKey,
  }) {
    return _record(
      (
        action: 'update',
        jobId: jobId,
        fields: fields,
        cursor: null,
        idempotencyKey: idempotencyKey,
      ),
      () => draft(fields),
    );
  }

  @override
  Future<ApiPage<Job>> jobs({String? cursor}) {
    return _record(
      (
        action: 'list',
        jobId: null,
        fields: const <String, Object?>{},
        cursor: cursor,
        idempotencyKey: null,
      ),
      () => page,
    );
  }

  @override
  Future<Job> job({required String jobId}) {
    return _record(
      (
        action: 'read',
        jobId: jobId,
        fields: const <String, Object?>{},
        cursor: null,
        idempotencyKey: null,
      ),
      () => detail(jobId),
    );
  }

  @override
  Future<Job> publish({
    required String jobId,
    required bool acceptsTerms,
    required String idempotencyKey,
  }) {
    return _record(
      (
        action: 'publish',
        jobId: jobId,
        fields: <String, Object?>{'accepts_terms': acceptsTerms},
        cursor: null,
        idempotencyKey: idempotencyKey,
      ),
      () {
        // What the platform does: the transition is applied and the job comes back as it now is,
        // with the instant the declaration was made recorded against it. Written back to `detail`
        // so a re-read agrees with what the publication returned — a job that answered `open` once
        // and `draft` on the next read would be a fake nothing in the product could produce.
        final opened = detail(jobId).copyWith(
          status: JobStatus.open,
          termsAcceptedAt: '2026-08-29T02:15:00.000Z',
        );
        detail = (_) => opened;
        return opened;
      },
    );
  }

  @override
  Future<Job> cancel({
    required String jobId,
    String reason = '',
    required String idempotencyKey,
  }) {
    return _record(
      (
        action: 'cancel',
        jobId: jobId,
        fields: <String, Object?>{if (reason.isNotEmpty) 'reason': reason},
        cursor: null,
        idempotencyKey: idempotencyKey,
      ),
      () {
        // What the platform does: the transition is applied and the job comes back as it now is.
        // The answer is recorded against `detail` too, so a re-read after the cancellation —
        // which is what a `409` makes the screen do — agrees with what the cancellation returned.
        final ended = detail(jobId).copyWith(status: JobStatus.cancelled);
        detail = (_) => ended;
        return ended;
      },
    );
  }
}
