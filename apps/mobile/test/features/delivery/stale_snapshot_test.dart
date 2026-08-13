// The one thing about `SyncWorker.snapshots` that produces a wrong answer rather than a slow one.
//
// A snapshot is **read** at one instant and **delivered** at a later one. So a read that started
// before a recording was enqueued can come back after it, and it will not contain that recording —
// not because the platform took it, but because it did not exist yet. A controller that concluded
// "gone from the queue, therefore on the platform" from such a read would tell a driver their work
// was recorded by Shipper a moment after they tapped a button with no signal. That is precisely the
// reassurance `Docs/02` §3.1's indicator exists to make truthful.
//
// `RecordMilestoneController` guards it with a read sequence number, and this file is what makes
// the guard real rather than asserted. **It was written because the mutation survived**: removing
// the guard broke nothing in `record_milestone_test.dart`, because a real queue over a real file
// resolves too quickly for the interleaving to happen by chance. So the interleaving is arranged
// here rather than waited for — [GatedQueue] lets a read complete and holds its *answer*, which is
// the shape of the hazard exactly.

import 'dart:async';
import 'dart:io';

import 'package:drift/native.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queue_database.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/sync_worker.dart';
import 'package:shipper/features/delivery/milestone.dart';
import 'package:shipper/features/delivery/record_milestone_controller.dart';

import '../../core/sync/sync_fixture.dart';

/// A queue whose reads finish when they finish and answer when the test says so.
///
/// The delay is **after** `super.snapshot()` on purpose. Holding the read itself would produce a
/// fresh answer late, which is harmless; holding the answer produces a *stale* one late, which is
/// the case the sequence number exists for.
final class GatedQueue extends OperationQueue {
  GatedQueue(super.database);

  final held = <Completer<void>>[];
  bool holding = false;

  @override
  Future<QueueSnapshot> snapshot() async {
    final answer = await super.snapshot();

    if (holding) {
      final gate = Completer<void>();
      held.add(gate);
      await gate.future;
    }

    return answer;
  }

  /// Lets the [index]th held answer through.
  ///
  /// Not called `release`: `OperationQueue` already has one, and it means something else entirely.
  void letThrough(int index) {
    if (!held[index].isCompleted) held[index].complete();
  }

  /// Lets every remaining answer through, so nothing is parked when the test ends.
  void letEverythingThrough() {
    holding = false;
    for (final gate in held) {
      if (!gate.isCompleted) gate.complete();
    }
  }
}

void main() {
  const job = '0198f2c1-1c9c-7b3d-9a2e-4a1f3c5d7e90';

  test('a read that started before the recording cannot conclude the platform has it', () async {
    final directory = Directory.systemTemp.createTempSync('shipper_delivery_test');
    addTearDown(() {
      if (directory.existsSync()) directory.deleteSync(recursive: true);
    });

    final database = QueueDatabase(NativeDatabase(File('${directory.path}/queue.sqlite')));
    addTearDown(database.close);

    final queue = GatedQueue(database);
    final worker = SyncWorker(
      queue: queue,
      // No signal: nothing is going to reach the platform during this test, so anything the screen
      // says about a recording having got there is wrong by construction.
      sender: ScriptedSender(thereafter: const ApiUnreachable()),
      alarm: TestAlarm(),
    );
    addTearDown(worker.dispose);

    final container = ProviderContainer(
      overrides: [syncWorkerProvider.overrideWithValue(worker)],
    );
    addTearDown(container.dispose);

    // The screen opens and catches up with the queue. That read is issued **now**, against a queue
    // that is empty, and its answer is held.
    queue.holding = true;
    container.listen(recordMilestoneProvider(job), (_, _) {}, fireImmediately: true);
    await pumpEventQueue();
    expect(queue.held, hasLength(1), reason: 'the catch-up read should be in flight');

    // The driver taps. The row is committed before this returns, so the held answer above is now
    // stale: it describes a queue that did not contain this milestone yet.
    final recorded = await container
        .read(recordMilestoneProvider(job).notifier)
        .record(Milestone.pickedUp);
    expect(recorded, isTrue);
    await pumpEventQueue();

    // The stale answer arrives, and it is the most recent one the controller has applied.
    queue.letThrough(0);
    await pumpEventQueue();

    expect(
      container.read(recordMilestoneProvider(job)).entries.single.sync,
      MilestoneSync.pending,
      reason: 'A read issued before the recording existed cannot show it as reaching Shipper. '
          'Docs/02 §3.1: optimistic local state is clearly marked as pending until the platform '
          'has it — and here the platform has nothing, because the sender is offline.',
    );

    queue.letEverythingThrough();
    await pumpEventQueue();

    // And it is still pending afterwards, which is the honest answer: the row is on the device.
    expect(container.read(recordMilestoneProvider(job)).entries.single.sync, MilestoneSync.pending);
    expect((await queue.snapshot()).pending, hasLength(1));
  });
}
