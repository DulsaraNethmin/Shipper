import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/sync_worker.dart';

/// The running sync worker, for anything that only wants to **look** at the queue (SHIP-126).
///
/// `null` by default, and that default is the whole reason this provider exists rather than
/// [queueSnapshotProvider] reading [syncWorkerProvider] directly.
///
/// SHIP-124 and SHIP-125 both took the same position from opposite directions: **every widget test
/// builds `ShipperApp`**, and anything in that widget which reaches the worker would have each of
/// them open a Drift database in the platform's application-support directory — a plugin channel a
/// host test has nothing behind. SHIP-125 answered it by starting the worker from `main` and
/// nowhere else. SHIP-126 puts an indicator *inside* `ShipperApp`, which is exactly the arrangement
/// that objection was about, so the dependency is inverted instead: the display reads a seam that is
/// empty until somebody supplies one, and `main.dart` is the somebody.
///
/// The consequence worth stating plainly: **an application that never overrides this shows no
/// indicator**, whatever is in the queue. That is right for a test and would be a defect in
/// production, so `sync_wiring_test.dart` holds `main`'s override rather than trusting it.
final queueWatchProvider = Provider<SyncWorker?>((ref) => null);

/// The queue as the last drain left it (SHIP-126).
///
/// Seeded with [SyncWorker.lastSnapshot] so a screen built *after* a pass shows the queue rather
/// than nothing until the next one. A broadcast stream buffers nothing, and on a handset with a
/// full queue and no signal the next pass can be five minutes away — which is five minutes of an
/// indicator saying nothing is pending while three updates are.
final queueSnapshotProvider = StreamProvider<QueueSnapshot>((ref) {
  final worker = ref.watch(queueWatchProvider);
  if (worker == null) return const Stream<QueueSnapshot>.empty();
  return _seeded(worker);
});

/// [SyncWorker.snapshots] with the last published snapshot in front of it.
///
/// Built per listen rather than stored, because an `async*` stream is single-subscription: one
/// cached instance would throw the second time [queueSnapshotProvider] was rebuilt.
Stream<QueueSnapshot> _seeded(SyncWorker worker) async* {
  final last = worker.lastSnapshot;
  if (last != null) yield last;
  yield* worker.snapshots;
}
