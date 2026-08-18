// SHIP-130's *Done when*, walked: photo captured, compressed, queued.
//
// The fourth clause — "and never written to the photo library" — is
// `proof_never_reaches_the_gallery_test.dart`, because it is a property of the whole build rather
// than of this journey.
//
// Everything below the fake camera is the real application: the real router, the real compressor,
// the real `CapturedImageStore` over a temporary directory, and SHIP-124's real queue over a real SQLite
// file. The camera is the one thing a host test cannot have — there is no device — so it is the one
// thing replaced, and it is replaced with something that returns **real JPEG bytes** so that the
// compression and the file that comes out of it are real too.

import 'dart:io';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/core/capture/captured_image.dart';

import '../../core/sync/sync_fixture.dart';
import 'delivery_app.dart';
import 'proof_fixture.dart';

void main() {
  const job = '0198f2c1-1c9c-7b3d-9a2e-4a1f3c5d7e90';

  /// Presses the shutter and waits for the capture to settle.
  ///
  /// **Not `pumpAndSettle`**, and the reason is the screen rather than the test: compressing shows
  /// an indeterminate `CircularProgressIndicator`, which never stops animating, so `pumpAndSettle`
  /// waits for a frame that will not come and times out. Pumping a bounded number of real intervals
  /// is what lets the compression and the two file writes underneath it actually finish.
  Future<void> pressShutter(WidgetTester tester, {bool expectQueued = true}) async {
    await tester.tap(find.byKey(const Key('proof-capture-shutter')));

    for (var frame = 0; frame < 100; frame++) {
      // `runAsync` steps outside the test's fake clock, which is what lets the **real** file I/O
      // underneath this finish: `CapturedImageStore.write` creates a directory and writes half a megabyte,
      // and `dart:io` completes those on the real event loop rather than on the fake one every other
      // await in a widget test runs on.
      await tester.runAsync(() => Future<void>.delayed(const Duration(milliseconds: 10)));
      await tester.pump();

      final settled = find.byKey(const Key('proof-capture-queued')).evaluate().isNotEmpty ||
          find.byKey(const Key('proof-capture-failed')).evaluate().isNotEmpty;
      if (settled) return;
    }

    expect(expectQueued, isFalse, reason: 'the capture never settled');
  }

  Directory temporary() {
    final directory = Directory.systemTemp.createTempSync('shipper_proof_journey');
    addTearDown(() {
      if (directory.existsSync()) directory.deleteSync(recursive: true);
    });
    return directory;
  }

  testWidgets('the delivery is photographed, compressed and queued with its milestone',
      (tester) async {
    final original = photograph(width: 2400, height: 1800);
    final camera = FakeCaptureCamera(bytes: original);
    final root = temporary();

    // Nothing may be accepted: an operation the platform takes is deleted, and a queue emptied by
    // success would make "queued" pass against a build that queued nothing.
    final harness = SyncHarness.create(sender: ScriptedSender(thereafter: const ApiUnreachable()));

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, CapturedImageStore(root, folder: CaptureFolder.proof)),
    );

    // Delivered is not one of the three plain buttons — a plain one would queue an operation whose
    // only possible outcome is a quarantined row, because SHIP-118 refuses `delivered` without
    // proof. It is a route to the camera instead.
    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('proof-capture-screen')), findsOneWidget);
    expect(find.byKey(const Key('fake-camera-preview')), findsOneWidget);
    expect(camera.started, isTrue);

    await pressShutter(tester);
    await harness.worker.drained;
    await tester.pumpAndSettle();

    expect(camera.captures, 1);
    expect(camera.stopped, isTrue, reason: 'the camera is given back as soon as the shot is taken');
    expect(find.byKey(const Key('proof-capture-queued')), findsOneWidget);

    // --- queued -----------------------------------------------------------------------------
    final snapshot = await harness.queue.snapshot();
    expect(snapshot.blocked, isEmpty, reason: 'nothing was quarantined');
    expect(snapshot.pending, hasLength(1));

    final queued = snapshot.pending.single;
    expect(queued.kind, OperationKind.proof);
    expect(queued.orderingKey, 'job:$job');
    expect(queued.path, '/v1/jobs/$job/milestones');
    expect(queued.body['milestone'], 'delivered');
    expect(queued.body['recorded_at'], isA<String>());
    expect(
      queued.idempotencyKey,
      'a4f21c9e',
      reason: 'minted where the driver acted and reused by every attempt (Docs/07 §4)',
    );

    // --- compressed, and in a file only this application can read -----------------------------
    final path = queued.attachmentPath;
    expect(path, isNotNull, reason: 'Docs/07 §4 queues the image as a local file, not as a body');

    final stored = File(path!);
    expect(stored.existsSync(), isTrue);
    expect(
      stored.path.startsWith(CapturedImageStore(root, folder: CaptureFolder.proof).directory.path),
      isTrue,
      reason: 'inside this application’s own directory — see '
          'proof_never_reaches_the_gallery_test.dart for the rest of that claim',
    );
    expect(
      stored.uri.pathSegments.last,
      'a4f21c9e.jpg',
      reason: 'named after the operation that will send it, so an orphan file is traceable',
    );

    final bytes = stored.readAsBytesSync();
    expect(bytes.length, lessThan(original.length), reason: 'Docs/01 §5.2: compressed on the device');
    expect(img.findFormatForData(bytes), img.ImageFormat.jpg);
    expect(
      img.decodeJpg(bytes)!.width,
      const CapturedImagePolicy().longestEdge,
      reason: 'and downscaled, not merely re-encoded',
    );

    // --- and the driver is told the truth about where it is -----------------------------------
    expect(find.textContaining('It is on this phone'), findsOneWidget);
    expect(find.text('1 update waiting to sync'), findsOneWidget, reason: 'SHIP-126 counts it');
  });

  testWidgets('a refused camera explains itself rather than showing a dead shutter',
      (tester) async {
    // SHIP-131 is the ticket that turns this into a route through the job — the reasoned exception
    // needs `proof.exception_reason`, which is SHIP-116 on the platform and no screen here yet. What
    // this ticket owes is that the refusal is explained rather than looking like a broken camera.
    final camera = FakeCaptureCamera(problem: CameraProblem.refused);
    final harness = SyncHarness.create(sender: ScriptedSender(thereafter: const ApiUnreachable()));

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, CapturedImageStore(temporary(), folder: CaptureFolder.proof)),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('proof-capture-blocked')), findsOneWidget);
    expect(find.byKey(const Key('proof-capture-shutter')), findsNothing);
    expect(
      find.textContaining('record a reason for the missing photo'),
      findsOneWidget,
      reason: 'SHIP-179 wrote the words; Docs/07 §7 calls a camera flow that dead-ends a defect',
    );
    expect(await harness.queue.count(), 0, reason: 'and nothing half-formed was queued');
  });

  testWidgets('bytes that are not an image are refused, and nothing is queued', (tester) async {
    // The one failure in this journey that is not retryable: a file that is not an image will not
    // become one on the next attempt. It leaves no row and no file behind.
    final camera = FakeCaptureCamera(bytes: Uint8List.fromList(const [0, 1, 2, 3, 4]));
    final root = temporary();
    final harness = SyncHarness.create(sender: ScriptedSender(thereafter: const ApiUnreachable()));

    await openDelivery(
      tester,
      harness: harness,
      jobId: job,
      overrides: withDevice(camera, CapturedImageStore(root, folder: CaptureFolder.proof)),
    );

    await tester.tap(find.byKey(const Key('milestone-record-delivered')));
    await tester.pumpAndSettle();
    await pressShutter(tester, expectQueued: false);
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('proof-capture-queued')), findsNothing);
    expect(
      find.byKey(const Key('proof-capture-failed')),
      findsOneWidget,
      reason: 'and the driver is told, rather than watching a shutter that did nothing',
    );
    expect(await harness.queue.count(), 0);
    expect(
      CapturedImageStore(root, folder: CaptureFolder.proof).directory.existsSync() &&
          CapturedImageStore(root, folder: CaptureFolder.proof).directory.listSync().isNotEmpty,
      isFalse,
      reason: 'and no photograph was written for an operation that does not exist',
    );
  });
}
