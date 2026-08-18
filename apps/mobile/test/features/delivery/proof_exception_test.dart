// SHIP-131 — "A denied permission offers the exception path instead of a dead end".
//
// Docs/07 §7 calls a camera flow that dead-ends on a denied permission a defect, and Docs/01 §4.4
// makes the photograph and the recorded reason the same feature: delivered requires one or the
// other, never neither. So the acceptance criterion is not "the screen says something helpful" —
// SHIP-179's copy already did that. It is that the **button under the copy queues something the
// platform will accept**, and that is what this file asserts: the queue row, its kind, its path, its
// body, and its ordering key.
//
// Everything below the fake camera is the real application: the real router, the real
// `CaptureProofController`, and SHIP-124's real queue over a real SQLite file. **The camera is
// refused rather than absent**, which is what a revoked permission looks like from Dart — and it
// must still be supplied, because under `testWidgets`' fake clock a platform-channel reply is never
// delivered at all, so the real `availableCameras()` does not throw, it never completes.

import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/permissions/permission_copy.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/features/delivery/proof_exception_reason.dart';
import 'package:shipper/core/capture/captured_image.dart';

import '../../core/sync/sync_fixture.dart';
import 'delivery_app.dart';
import 'proof_fixture.dart';

void main() {
  const job = '0198f2c1-1c9c-7b3d-9a2e-4a1f3c5d7e90';

  /// A proof store over a temporary directory.
  ///
  /// Supplied even to the tests that never write a file, because `withDevice` is what puts the fake
  /// camera in and the store comes with it. **Nothing here writes one** — that is the whole point of
  /// the exception path: no object is created and the object store is never contacted.
  CapturedImageStore store() {
    final directory = Directory.systemTemp.createTempSync('shipper_proof_exception');
    addTearDown(() {
      if (directory.existsSync()) directory.deleteSync(recursive: true);
    });
    return CapturedImageStore(directory, folder: CaptureFolder.proof);
  }

  /// A queue nothing may empty.
  ///
  /// An operation the platform accepts is **deleted**, so a queue emptied by success would make
  /// every "it was queued" assertion below pass against a build that queued nothing.
  SyncHarness stuckQueue() =>
      SyncHarness.create(sender: ScriptedSender(thereafter: const ApiUnreachable()));

  testWidgets('a refused camera offers the three reasons rather than a dead end', (tester) async {
    final camera = FakeCaptureCamera(problem: CameraProblem.refused);
    final harness = stuckQueue();

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, store()),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('proof-capture-blocked')), findsOneWidget);
    expect(find.byKey(const Key('proof-capture-screen')), findsNothing);

    // SHIP-179's copy is still what a driver reads when the camera is why.
    expect(find.text(PermissionCopy.cameraDeclined), findsOneWidget);

    // And the three reasons Docs/01 §4.4 names — all three, because only one of them is about the
    // camera and a driver whose recipient objected is standing in front of this screen too.
    for (final reason in ProofExceptionReasonCopy.offered) {
      expect(find.byKey(Key('proof-exception-${reason.wireName}')), findsOneWidget);
    }
    expect(
      ProofExceptionReasonCopy.offered.length,
      3,
      reason: 'the list is closed, and `unknown` is this client’s own value rather than one the '
          'platform accepts',
    );
    expect(
      find.byKey(Key('proof-exception-${ProofExceptionReason.unknown.wireName}')),
      findsNothing,
    );
  });

  testWidgets('choosing a reason and recording it queues the milestone the platform accepts',
      (tester) async {
    final camera = FakeCaptureCamera(problem: CameraProblem.refused);
    final harness = stuckQueue();

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, store()),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();

    // Two taps, and the first commits nothing. A reason cannot be taken back from this screen, and
    // three one-tap targets are three ways for a gloved thumb to finish a delivery by accident.
    expect(
      tester.widget<FilledButton>(find.byKey(const Key('proof-exception-record'))).onPressed,
      isNull,
      reason: 'nothing is selected yet',
    );

    await tester.tap(find.byKey(const Key('proof-exception-camera_unavailable')));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('proof-exception-record')));
    await tester.pumpAndSettle();
    await harness.worker.drained;
    await tester.pumpAndSettle();

    // --- what is on the device ------------------------------------------------------------------
    final snapshot = await harness.queue.snapshot();
    expect(snapshot.blocked, isEmpty, reason: 'nothing was quarantined');
    expect(snapshot.pending, hasLength(1));

    final queued = snapshot.pending.single;
    expect(
      queued.kind,
      OperationKind.milestone,
      reason: 'there is no file, so there is nothing for OperationKind.proof to carry',
    );
    expect(queued.attachmentPath, isNull);
    expect(queued.path, '/v1/jobs/$job/milestones');
    expect(queued.method, 'POST');
    expect(
      queued.orderingKey,
      'job:$job',
      reason: 'one job’s records reach the platform in the order the driver made them (SHIP-124)',
    );

    // The contract's shape: exactly one of `object_key` and `exception_reason`, and sending both is
    // refused with `validation_failed`.
    expect(queued.body['milestone'], 'delivered');
    expect(queued.body['recorded_at'], isA<String>());
    expect(queued.body['proof'], <String, dynamic>{'exception_reason': 'camera_unavailable'});
    expect((queued.body['proof'] as Map<String, dynamic>).containsKey('object_key'), isFalse);

    // The idempotency key is minted by the queue at the moment the driver acted and reused unchanged
    // by every attempt, which is what makes a retry after a day in a valley a retry rather than a
    // second Delivered on the customer's timeline.
    expect(queued.idempotencyKey, isNotEmpty);

    // --- what the driver is told -----------------------------------------------------------------
    expect(find.byKey(const Key('proof-exception-recorded')), findsOneWidget);
    expect(
      find.byKey(const Key('proof-capture-queued')),
      findsNothing,
      reason: '"Photograph saved" about a recorded exception is a driver who believes they '
          'photographed a delivery they did not',
    );
    expect(find.textContaining('Photograph saved'), findsNothing);
    expect(find.text('Reason recorded'), findsOneWidget);
  });

  testWidgets('the way back to the delivery is the same one the photograph path uses',
      (tester) async {
    final camera = FakeCaptureCamera(problem: CameraProblem.refused);
    final harness = stuckQueue();

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, store()),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('proof-exception-recipient_objected')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('proof-exception-record')));
    await tester.pumpAndSettle();
    await harness.worker.drained;
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('proof-exception-done')));
    await tester.pumpAndSettle();

    // Back on the delivery, and the recorded exception is in its log as a pending `Delivered` —
    // because that log reads `OperationKind.milestone` rows on this job's ordering key, which is
    // exactly what an exception is. A driver must be able to see that the thing they recorded is on
    // the device and not yet sent.
    expect(find.byKey(const Key('milestone-record-delivered')), findsOneWidget);
    expect(find.text('Pending'), findsWidgets);
  });

  testWidgets('a working camera still offers the two reasons that are not about the camera',
      (tester) async {
    // Only `camera_unavailable` is about the camera. A driver whose recipient objects, or who is
    // standing somewhere unlit, has the same problem SHIP-131 exists to solve and a perfectly good
    // camera in their hand.
    final camera = FakeCaptureCamera(bytes: photograph(width: 40, height: 30));
    final harness = stuckQueue();

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, store()),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('proof-capture-screen')), findsOneWidget);

    await tester.tap(find.byKey(const Key('proof-cannot-photograph')));
    await tester.pumpAndSettle();

    // The camera's own explanation is **not** shown: this driver's camera is working, and telling
    // them it will not open is a support call made out of a reused string.
    expect(find.text(PermissionCopy.cameraDeclined), findsNothing);
    expect(find.byKey(const Key('proof-exception-preamble')), findsOneWidget);

    // And there is a way back to the shutter, which the blocked screen does not have because there
    // is nothing to go back to.
    expect(find.byKey(const Key('proof-exception-back')), findsOneWidget);
    await tester.tap(find.byKey(const Key('proof-exception-back')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('proof-capture-screen')), findsOneWidget);
    expect(camera.started, isTrue, reason: 'the preview was never given away');
  });

  // SHIP-131a. The closed list stays closed — `Docs/04` §5 needs a reason it can group — and the
  // driver's own words go **beside** the selection on the same request. The assertions are on the
  // queued body rather than on the field, because a note the driver can see and the platform never
  // receives is the whole defect this ticket exists to close.
  testWidgets('a note is recorded beside the selected reason, not instead of one', (tester) async {
    final camera = FakeCaptureCamera(problem: CameraProblem.refused);
    final harness = stuckQueue();

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, store()),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();

    await tester.enterText(
      find.byKey(const Key('proof-exception-note')),
      'The recipient asked me not to photograph their door.',
    );
    await tester.pumpAndSettle();

    // **A note alone records nothing.** The button is still disabled, which is the "beside rather
    // than instead of" clause said in the one place a driver could otherwise get round it.
    expect(
      tester.widget<FilledButton>(find.byKey(const Key('proof-exception-record'))).onPressed,
      isNull,
      reason: 'typing a sentence substituted for choosing one of the three',
    );

    await tester.tap(find.byKey(const Key('proof-exception-recipient_objected')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('proof-exception-record')));
    await tester.pumpAndSettle();
    await harness.worker.drained;
    await tester.pumpAndSettle();

    final queued = (await harness.queue.snapshot()).pending.single;

    // Both, in one body. The selection is what a queue can group and a CHECK constraint can hold;
    // the sentence is which of the three it actually was.
    expect(queued.body['proof'], <String, dynamic>{'exception_reason': 'recipient_objected'});
    expect(queued.body['reason'], 'The recipient asked me not to photograph their door.');
    expect(
      queued.body.keys.toSet(),
      <String>{'milestone', 'recorded_at', 'reason', 'proof'},
    );
  });

  testWidgets('a reason with no note is recorded exactly as it was before the field existed',
      (tester) async {
    final camera = FakeCaptureCamera(problem: CameraProblem.refused);
    final harness = stuckQueue();

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, store()),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();

    // Spaces rather than nothing: the platform collapses whitespace and then bounds what is left,
    // so a note of spaces is a note nobody wrote — and `"reason": ""` would put a blank line under
    // the customer's latest update, because their view branches on the field being present.
    await tester.enterText(find.byKey(const Key('proof-exception-note')), '  ');
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('proof-exception-camera_unavailable')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('proof-exception-record')));
    await tester.pumpAndSettle();
    await harness.worker.drained;
    await tester.pumpAndSettle();

    final queued = (await harness.queue.snapshot()).pending.single;
    expect(queued.body.containsKey('reason'), isFalse);
    expect(queued.body['proof'], <String, dynamic>{'exception_reason': 'camera_unavailable'});
  });

  testWidgets('a refused camera offers no way back to a shutter that does not exist',
      (tester) async {
    final camera = FakeCaptureCamera(problem: CameraProblem.unavailable);
    final harness = stuckQueue();

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, store()),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('proof-exception-back')), findsNothing);
  });
}
