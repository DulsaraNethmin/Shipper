// SHIP-124 — the half of the *Done when* that is not about restarts.
//
// "Queued operations survive app restart **and are never silently dropped**." The first half is a
// storage question and operation_queue_test.dart answers it. The second is an invariant over the
// whole class, and a test that only shows the happy path surviving a relaunch does not demonstrate
// it at all — the interesting cases are every way an operation could stop being visible.
//
// Six were enumerated, and each has a group below:
//
//   1. A crash between enqueue and commit.
//   2. A crash mid-drain, with an operation claimed and unresolved.
//   3. A row this build cannot read — an app upgrade, or a downgrade.
//   4. A queue that grows without bound.
//   5. Two writers racing.
//   6. A removal nobody asked for.
//
// The shape they share: an operation may fail, be retried, be held for a person, or be discarded
// on purpose — but there must be no path where it stops being counted without somebody being able
// to see that it happened.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queued_operation.dart';

import 'queue_fixture.dart';

void main() {
  group('1. a crash between enqueue and commit', () {
    test('leaves no partial row behind', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      await enqueueMilestone(queue, job: 'job-a');

      // The process dying inside the write. Modelled as an enclosing transaction that never
      // commits, which is what the operating system does to an uncommitted SQLite transaction when
      // it takes the process away.
      await expectLater(
        fixture.database.transaction(() async {
          await enqueueMilestone(queue, job: 'job-b');
          throw StateError('the process ended here');
        }),
        throwsA(isA<StateError>()),
      );

      final relaunched = await fixture.restart();
      final snapshot = await relaunched.snapshot();

      expect(snapshot.total, 1);
      expect(snapshot.pending.single.orderingKey, 'job:job-a');
      expect(snapshot.blocked, isEmpty, reason: 'a row that never committed is not a blocked row');
    });

    test('a refused enqueue writes nothing at all', () async {
      // The three refusals happen inside the insert's own transaction, so a caller that catches one
      // and carries on is not carrying on over a half-written queue.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(policy: const QueuePolicy(maxOperations: 1));

      await enqueueMilestone(queue, job: 'job-a', idempotencyKey: 'key-1');

      await expectLater(
        enqueueMilestone(queue, job: 'job-b'),
        throwsA(isA<QueueAtCapacity>()),
      );
      expect(await queue.count(), 1);

      final roomy = await fixture.restart(policy: const QueuePolicy(maxOperations: 10));
      await expectLater(
        enqueueMilestone(roomy, job: 'job-c', idempotencyKey: 'key-1'),
        throwsA(isA<QueueDuplicateOperation>()),
      );
      expect(await roomy.count(), 1);
    });

    test('what has committed by the time enqueue returns is on disk', () async {
      // This is what makes it safe for a screen to confirm to the user the moment the await
      // completes, which is what Docs/07 §4 asks it to do. Nothing is written between the two
      // statements below — the second launch reads only what the first had already committed.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(mintKey: sequentialKeys());
      await enqueueMilestone(queue);

      final relaunched = await fixture.restart();
      expect((await relaunched.snapshot()).pending.single.idempotencyKey, 'key-1');
    });
  });

  group('2. a crash mid-drain', () {
    test('an operation left in flight comes back, with the key it was created with', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(mintKey: sequentialKeys());
      await enqueueMilestone(queue);

      final claimed = await queue.claim();
      expect(claimed!.attempts, 1);

      // The process dies with the request in flight. Nobody knows whether the platform recorded it.
      final relaunched = await fixture.restart();

      expect(await relaunched.recover(), 1);

      final again = await relaunched.claim();
      expect(again!.idempotencyKey, 'key-1', reason: 'a fresh key here duplicates the milestone');
      expect(again.attempts, 2, reason: 'the attempt that died still happened');
    });

    test('an operation not yet recovered is still counted and still listed', () async {
      // recover() runs at start-up, but nothing about the invariant depends on it having run: an
      // unrecovered operation is in the total, is in the snapshot, and shows in the unsynced count
      // the user is looking at.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      await enqueueMilestone(queue);
      await queue.claim();

      final relaunched = await fixture.restart();
      final snapshot = await relaunched.snapshot();

      expect(snapshot.total, 1);
      expect(snapshot.inFlight, hasLength(1));
      expect(snapshot.unsynced, 1);
    });

    test('recovery touches only what was in flight', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      final a = await enqueueMilestone(queue, job: 'job-a');
      await enqueueMilestone(queue, job: 'job-b');
      final c = await enqueueMilestone(queue, job: 'job-c');
      await queue.claim();
      await queue.block(c.id, reason: QueueBlockReason.refused, detail: 'the job was cancelled');

      final relaunched = await fixture.restart();
      expect(await relaunched.recover(), 1);

      final snapshot = await relaunched.snapshot();
      expect(snapshot.pending.map((o) => o.id), containsAll(<int>[a.id]));
      expect(snapshot.blocked.single.id, c.id);
      expect(snapshot.blocked.single.reason, QueueBlockReason.refused);
      expect(snapshot.total, 3);
    });
  });

  group('3. a row this build cannot read', () {
    test('a kind this build has never heard of is quarantined, not skipped', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final enqueued = await enqueueMilestone(queue);

      // Written by a build that had an operation this one does not.
      await fixture.overwrite(enqueued.id, kind: 'delivery.teleport');

      final snapshot = await queue.snapshot();

      expect(snapshot.total, 1, reason: 'the row is still there');
      expect(snapshot.pending, isEmpty);
      expect(snapshot.blocked.single.kindName, 'delivery.teleport');
      expect(snapshot.blocked.single.reason, QueueBlockReason.unsupported);
      expect(snapshot.blocked.single.detail, contains('delivery.teleport'));
    });

    test('a body version this build does not write is quarantined', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final enqueued = await enqueueMilestone(queue);

      await fixture.overwrite(enqueued.id, bodyVersion: 99);

      final blocked = (await queue.snapshot()).blocked.single;
      expect(blocked.reason, QueueBlockReason.unsupported);
      expect(blocked.detail, contains('v99'));
    });

    test('a body that is not JSON at all is quarantined', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final enqueued = await enqueueMilestone(queue);

      await fixture.overwrite(enqueued.id, body: 'not json, and never was');

      final blocked = (await queue.snapshot()).blocked.single;
      expect(blocked.reason, QueueBlockReason.unreadable);
    });

    test('a state this build has no name for reads as blocked rather than as nothing', () async {
      // The subtle one. An unrecognised state that read as "not pending, not in flight, not
      // blocked" would be a row present in the table and absent from every list — a silent drop
      // wearing a database row as a disguise. Blocked is the default bucket, deliberately.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final enqueued = await enqueueMilestone(queue);

      await fixture.overwrite(enqueued.id, state: 'awaiting_supervisor_approval');

      final snapshot = await queue.snapshot();
      expect(snapshot.total, 1);
      expect(snapshot.blocked, hasLength(1));
      expect(snapshot.pending, isEmpty);
      expect(snapshot.inFlight, isEmpty);
    });

    test('the quarantine is durable, and does not overwrite a recorded refusal', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final unreadable = await enqueueMilestone(queue, job: 'job-a');
      final refused = await enqueueMilestone(queue, job: 'job-b');

      await queue.block(refused.id, reason: QueueBlockReason.refused, detail: 'admin cancelled');
      await fixture.overwrite(unreadable.id, kind: 'delivery.teleport');

      // The first read discovers the unreadable row and records the quarantine.
      await queue.snapshot();

      final relaunched = await fixture.restart();
      final blocked = (await relaunched.snapshot()).blocked;

      expect(blocked, hasLength(2));
      expect(
        blocked.firstWhere((b) => b.id == unreadable.id).reason,
        QueueBlockReason.unsupported,
      );
      expect(
        blocked.firstWhere((b) => b.id == refused.id).reason,
        QueueBlockReason.refused,
        reason: 'reading must not relabel a platform refusal as something this build could not read',
      );
      expect(blocked.firstWhere((b) => b.id == refused.id).detail, 'admin cancelled');
    });

    test('an unreadable operation holds its own key and stalls no other job', () async {
      // This is the decision the ordering guarantee exists for, in the one case that would
      // otherwise be indefensible: a poison item must not stop every other job's work leaving the
      // handset, and must not let the operations behind it in its own job overtake it.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      final poison = await enqueueMilestone(queue, job: 'job-a', milestone: 'picked_up');
      await enqueueMilestone(queue, job: 'job-a', milestone: 'in_transit');
      await enqueueMilestone(queue, job: 'job-b', milestone: 'picked_up');

      await fixture.overwrite(poison.id, body: '{{{');

      final drained = <String>[];
      for (var claimed = await queue.claim(); claimed != null; claimed = await queue.claim()) {
        drained.add(claimed.orderingKey);
        await queue.complete(claimed.id);
      }

      expect(drained, ['job:job-b'], reason: "job-b is unaffected; job-a's sequence is held");

      final snapshot = await queue.snapshot();
      expect(snapshot.total, 2);
      expect(snapshot.blocked.single.id, poison.id);
      expect(snapshot.pending.single.body['milestone'], 'in_transit');
    });

    test('an unreadable operation can only leave by somebody acknowledging it', () async {
      // The client's answer to a poison item, and it differs from the platform's at SHIP-135 for a
      // reason: the outbox made a permanently unpublishable row unwritable, and a handset cannot,
      // because the row was written by a different build of the app. So it is held, counted, and
      // removed by a person — never by machinery, and never after N attempts.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final poison = await enqueueMilestone(queue);
      await fixture.overwrite(poison.id, kind: 'delivery.teleport');

      await queue.snapshot();

      expect(await queue.claim(), isNull);
      expect(await queue.recover(), 0);
      expect(await queue.complete(poison.id), isFalse);
      expect(await queue.release(poison.id), isFalse);
      expect(await queue.count(), 1);

      expect(await queue.acknowledge(poison.id), isTrue);
      expect(await queue.count(), 0);
    });
  });

  group('4. a queue that grows without bound', () {
    test('the cap refuses the new operation and keeps every old one', () async {
      // Eviction would be the obvious answer and it is exactly the defect: dropping the oldest to
      // make room drops the operation that has waited longest, and does it where nobody is looking.
      // A refusal happens in front of the person who has just acted.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(policy: const QueuePolicy(maxOperations: 3));

      for (var i = 0; i < 3; i++) {
        await enqueueMilestone(queue, job: 'job-$i');
      }

      await expectLater(
        enqueueMilestone(queue, job: 'job-4'),
        throwsA(
          isA<QueueAtCapacity>()
              .having((e) => e.limit, 'limit', 3)
              .having((e) => e.reason, 'reason', contains('3')),
        ),
      );

      final snapshot = await queue.snapshot();
      expect(snapshot.total, 3);
      expect(snapshot.pending.first.orderingKey, 'job:job-0', reason: 'the oldest is still here');
    });

    test('blocked operations count towards the cap', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(policy: const QueuePolicy(maxOperations: 2));

      final first = await enqueueMilestone(queue, job: 'job-a');
      await enqueueMilestone(queue, job: 'job-b');
      await queue.block(first.id, reason: QueueBlockReason.refused);

      await expectLater(enqueueMilestone(queue, job: 'job-c'), throwsA(isA<QueueAtCapacity>()));
    });

    test('a body over the bound is refused before anything is written', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(policy: const QueuePolicy(maxBodyBytes: 64));

      await expectLater(
        queue.enqueue(
          kind: OperationKind.milestone,
          orderingKey: 'job:job-a',
          method: 'POST',
          path: '/v1/jobs/job-a/milestones',
          body: <String, dynamic>{'note': 'x' * 200},
          recordedAt: DateTime.utc(2026, 8, 13),
        ),
        throwsA(isA<QueueOperationTooLarge>().having((e) => e.limit, 'limit', 64)),
      );

      expect(await queue.count(), 0);
    });

    test('a kind that does not exist cannot be written at all', () {
      // The strongest form of the same idea, taken from SHIP-135: rather than handle an operation
      // nothing could ever send, make it impossible to create. OperationKind's constructor is
      // private, so the set below is closed by the compiler and enqueue needs no check for it.
      expect(OperationKind.byName('delivery.teleport'), isNull);
      expect(OperationKind.all, contains(OperationKind.milestone));
      expect(OperationKind.all, contains(OperationKind.proof));
    });
  });

  group('5. two writers racing', () {
    test('two concurrent claims never hand out the same operation', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      await enqueueMilestone(queue, job: 'job-a');
      await enqueueMilestone(queue, job: 'job-b');

      final claimed = await Future.wait([queue.claim(), queue.claim()]);

      expect(claimed.whereType<QueuedOperation>(), hasLength(2));
      expect(claimed[0]!.id, isNot(claimed[1]!.id));
      expect((await queue.snapshot()).inFlight, hasLength(2));
    });

    test('two claims on one ordering key give one operation and one nothing', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      await enqueueMilestone(queue, milestone: 'picked_up');
      await enqueueMilestone(queue, milestone: 'in_transit');

      final claimed = await Future.wait([queue.claim(), queue.claim()]);

      expect(claimed.whereType<QueuedOperation>(), hasLength(1));
      expect(claimed.where((o) => o == null), hasLength(1));
      expect((await queue.snapshot()).total, 2);
    });

    test('concurrent enqueues cannot take the queue past its cap', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(policy: const QueuePolicy(maxOperations: 2));

      await enqueueMilestone(queue, job: 'job-a');

      final outcomes = await Future.wait([
        enqueueMilestone(queue, job: 'job-b').then<Object?>((o) => o, onError: (Object e) => e),
        enqueueMilestone(queue, job: 'job-c').then<Object?>((o) => o, onError: (Object e) => e),
      ]);

      expect(outcomes.whereType<QueuedOperation>(), hasLength(1));
      expect(outcomes.whereType<QueueAtCapacity>(), hasLength(1));
      expect(await queue.count(), 2);
    });

    test('two enqueues of the same action leave one operation', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      final outcomes = await Future.wait([
        enqueueMilestone(queue, idempotencyKey: 'key-1')
            .then<Object?>((o) => o, onError: (Object e) => e),
        enqueueMilestone(queue, idempotencyKey: 'key-1')
            .then<Object?>((o) => o, onError: (Object e) => e),
      ]);

      expect(outcomes.whereType<QueuedOperation>(), hasLength(1));
      expect(outcomes.whereType<QueueDuplicateOperation>(), hasLength(1));
      expect(await queue.count(), 1);
    });
  });

  group('6. a removal nobody asked for', () {
    test('only complete, acknowledge and clear ever remove a row', () async {
      // The invariant stated as a property rather than as a comment: every other public method is
      // driven here against a populated queue, and the count does not move. A fourth removal path
      // added later fails this test, which is the point of enumerating rather than asserting.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      final a = await enqueueMilestone(queue, job: 'job-a');
      final b = await enqueueMilestone(queue, job: 'job-b');
      final c = await enqueueMilestone(queue, job: 'job-c');
      await queue.block(c.id, reason: QueueBlockReason.refused);
      await fixture.overwrite(b.id, kind: 'delivery.teleport');

      expect(await queue.count(), 3);
      await queue.count();
      await queue.snapshot();
      await queue.recover();
      await queue.claim();
      await queue.release(a.id, nextAttemptAt: DateTime.utc(2026, 8, 13, 10));
      await queue.block(a.id, reason: QueueBlockReason.refused);
      await queue.snapshot();
      expect(await queue.count(), 3, reason: 'nothing above is a removal');

      final relaunched = await fixture.restart();
      expect(await relaunched.count(), 3);
    });

    test('clear discards everything and says how much', () async {
      // Docs/07 §3 clears the queue at sign-out. It is the one bulk removal there is, and returning
      // the count is what keeps even that from being silent — the caller can tell the user how much
      // unsynced work went with the session.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      final a = await enqueueMilestone(queue, job: 'job-a');
      await enqueueMilestone(queue, job: 'job-b');
      await enqueueMilestone(queue, job: 'job-c');
      await queue.claim();
      await queue.block(a.id, reason: QueueBlockReason.refused);

      expect(await queue.clear(), 3);
      expect(await queue.count(), 0);

      final relaunched = await fixture.restart();
      expect((await relaunched.snapshot()).total, 0);
      expect(await relaunched.clear(), 0);
    });
  });
}
