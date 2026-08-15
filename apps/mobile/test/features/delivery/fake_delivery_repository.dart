import 'dart:async';

import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/delivery/delivery_repository.dart';
import 'package:shipper/features/delivery/delivery_tracking.dart';
import 'package:shipper/features/delivery/proof_exception_reason.dart';

/// A recorded milestone as the platform answers with one.
///
/// Built from the `Milestone` example in `contracts/paths/delivery.yaml`, so that a field renamed in
/// the contract shows up here rather than only on a device.
///
/// The two clocks are **deliberately different values**. `Docs/02` §3.1 keeps them apart —
/// [recordedAt] is when the actor says they acted and [acceptedAt] is when the platform received it
/// — and a fixture that set them equal would let a screen render the wrong one and still pass.
RecordedMilestone aMilestone({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e2',
  String jobId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  String milestone = 'picked_up',
  MilestoneActor? recordedBy = MilestoneActor.provider,
  String? reason,
  String? recordedAt = '2026-09-08T23:40:11.000Z',
  String? acceptedAt = '2026-09-09T02:15:02.481Z',
}) {
  return RecordedMilestone(
    id: id,
    jobId: jobId,
    milestone: milestone,
    recordedBy: recordedBy,
    reason: reason,
    recordedAt: recordedAt,
    acceptedAt: acceptedAt,
  );
}

/// A photograph as the platform answers with one.
///
/// [downloadUrl] is a URL nothing will ever fetch in a host test: `flutter_test` installs an
/// `HttpOverrides` that answers every request `400`, so `Image.network` reaches its `errorBuilder`.
/// That is not a limitation to work around — the error path is the one a customer meets when a
/// short-lived link runs out, and it is worth having a test walk it.
DeliveryProof aPhotograph({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e5',
  String jobId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  String milestone = 'delivered',
  String? milestoneId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e2',
  String? downloadUrl = 'https://store.example.com/shipper/proof/one?X-Amz-Algorithm=AWS4-HMAC-SHA256',
  String? downloadExpiresAt = '2026-09-09T04:45:00.000Z',
  String? recordedAt = '2026-09-08T23:40:11.000Z',
}) {
  return DeliveryProof(
    id: id,
    jobId: jobId,
    milestone: milestone,
    milestoneId: milestoneId,
    downloadUrl: downloadUrl,
    downloadExpiresAt: downloadExpiresAt,
    recordedAt: recordedAt,
    acceptedAt: '2026-09-09T02:15:02.000Z',
  );
}

/// A reasoned exception as the platform answers with one (SHIP-116).
///
/// **No `download_url` and no `download_expires_at`**, which is the contract's own shape: there is
/// no object, so there is nothing to sign a URL for. A fixture that carried one would let a screen
/// that branched on the URL rather than on the reason pass.
DeliveryProof anException({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e6',
  String jobId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  String milestone = 'delivered',
  ProofExceptionReason reason = ProofExceptionReason.recipientObjected,
  String? recordedAt = '2026-09-08T23:40:11.000Z',
}) {
  return DeliveryProof(
    id: id,
    jobId: jobId,
    milestone: milestone,
    milestoneId: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e2',
    exceptionReason: reason,
    recordedAt: recordedAt,
    acceptedAt: '2026-09-09T02:15:02.000Z',
  );
}

/// A [DeliveryRepository] that answers from a script and records what it was asked.
///
/// The screen is tested against this rather than against a stub transport, because a widget test
/// that also exercises `dio`'s wiring fails for two reasons and reads as one. What actually reaches
/// the wire is `delivery_repository_test.dart`'s subject, against the contract.
class FakeDeliveryRepository implements DeliveryRepository {
  /// Every read, in order, so a test can assert that a further page did **not** re-read the proof —
  /// which would mint a fresh set of signed URLs for photographs already on screen.
  final calls = <String>[];

  /// The cursors the milestone read was given, in order. `null` is a first page.
  final cursors = <String?>[];

  DeliveryDriver driver_ = const DeliveryDriver(
    jobId: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
    driverAssigned: false,
  );

  /// The pages of milestones, in order. The last is repeated once exhausted.
  List<ApiPage<RecordedMilestone>> pages = <ApiPage<RecordedMilestone>>[
    const ApiPage<RecordedMilestone>(data: <RecordedMilestone>[]),
  ];

  List<DeliveryProof> proof_ = <DeliveryProof>[];

  /// Thrown instead of answering, on every read until it is cleared.
  Object? failure;

  /// Held open until completed, so a test can assert what the screen shows mid-read.
  Completer<void>? gate;

  int get milestoneReads => calls.where((call) => call == 'milestones').length;
  int get proofReads => calls.where((call) => call == 'proof').length;

  @override
  Future<DeliveryDriver> driver({required String jobId}) async {
    calls.add('driver');
    await _hold();
    return driver_;
  }

  @override
  Future<ApiPage<RecordedMilestone>> milestones({required String jobId, String? cursor}) async {
    final index = cursors.length;
    calls.add('milestones');
    cursors.add(cursor);
    await _hold();

    if (pages.isEmpty) return const ApiPage<RecordedMilestone>(data: <RecordedMilestone>[]);
    return pages[index < pages.length ? index : pages.length - 1];
  }

  @override
  Future<List<DeliveryProof>> proof({required String jobId}) async {
    calls.add('proof');
    await _hold();
    return proof_;
  }

  Future<void> _hold() async {
    final held = gate;
    if (held != null) await held.future;

    final thrown = failure;
    if (thrown != null) throw thrown;
  }
}
