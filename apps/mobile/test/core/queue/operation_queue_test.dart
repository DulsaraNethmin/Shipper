// SHIP-124 — the queue itself: what it holds, what order it hands it back in, and what SHIP-125
// will drain it through.
//
// The other half of the *Done when* — "never silently dropped" — is a property of the whole class
// rather than of any one method, and it has its own file:
// queued_operations_are_never_silently_dropped_test.dart.
//
// Everything here runs against a real SQLite file. See queue_fixture.dart for why an in-memory
// database would make these tests pass while proving nothing.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queued_operation.dart';

import 'queue_fixture.dart';

void main() {
  group('surviving a restart', () {
    test('an operation enqueued in one launch is there in the next', () async {
      final fixture = QueueFixture.temporary();
      final first = fixture.open(mintKey: sequentialKeys());

      await enqueueMilestone(
        first,
        job: 'job-a',
        milestone: 'en_route_to_pickup',
        recordedAt: DateTime.utc(2026, 8, 13, 6, 15),
      );
      await enqueueMilestone(
        first,
        job: 'job-a',
        milestone: 'picked_up',
        recordedAt: DateTime.utc(2026, 8, 13, 7, 42),
      );

      final relaunched = await fixture.restart();
      final snapshot = await relaunched.snapshot();

      expect(snapshot.total, 2);
      expect(snapshot.pending.map((o) => o.body['milestone']), ['en_route_to_pickup', 'picked_up']);
      expect(snapshot.pending.map((o) => o.idempotencyKey), ['key-1', 'key-2']);
      expect(snapshot.pending.first.recordedAt, DateTime.utc(2026, 8, 13, 6, 15));
      expect(snapshot.pending.first.path, '/v1/jobs/job-a/milestones');
      expect(snapshot.pending.first.method, 'POST');
      expect(snapshot.pending.first.kind, OperationKind.milestone);
    });

    test('a proof attachment path survives with its operation', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      await queue.enqueue(
        kind: OperationKind.proof,
        orderingKey: 'job:job-a',
        method: 'POST',
        path: '/v1/jobs/job-a/proof',
        body: <String, dynamic>{'milestone': 'delivered'},
        recordedAt: DateTime.utc(2026, 8, 13, 8),
        attachmentPath: '/var/mobile/shipper/proof-1.jpg',
      );

      final relaunched = await fixture.restart();
      final stored = (await relaunched.snapshot()).pending.single;

      expect(stored.kind, OperationKind.proof);
      expect(stored.attachmentPath, '/var/mobile/shipper/proof-1.jpg');
    });
  });

  group('the idempotency key', () {
    test('is minted once and reused by every attempt', () async {
      // Docs/07 §4: the key is generated where the user acts and reused unchanged across every
      // retry. For a queued operation the enqueue *is* where the user acted, so this asserts the
      // key does not move when the row is claimed, released and claimed again — a key minted per
      // attempt would duplicate a milestone the first attempt had already committed.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(mintKey: sequentialKeys());

      final enqueued = await enqueueMilestone(queue);
      expect(enqueued.idempotencyKey, 'key-1');

      final first = await queue.claim();
      expect(first!.idempotencyKey, 'key-1');
      expect(first.attempts, 1);

      expect(await queue.release(first.id), isTrue);

      final second = await queue.claim();
      expect(second!.idempotencyKey, 'key-1');
      expect(second.attempts, 2);
    });

    test('survives the restart it exists for', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open(mintKey: sequentialKeys());
      final enqueued = await enqueueMilestone(queue);

      final relaunched = await fixture.restart();
      final claimed = await relaunched.claim();

      expect(claimed!.idempotencyKey, enqueued.idempotencyKey);
    });
  });

  test('the actor clock and the queue clock are kept apart', () async {
    // Docs/02 §3.1 carries two timestamps on every transition. The queue owns neither the
    // platform's nor the actor's — it records when the user said they acted, and separately when
    // the row was written, and never substitutes one for the other.
    final fixture = QueueFixture.temporary();
    final queue = fixture.open(now: () => DateTime.utc(2026, 8, 13, 18));

    final enqueued = await enqueueMilestone(
      queue,
      recordedAt: DateTime.utc(2026, 8, 13, 9, 30),
    );

    expect(enqueued.recordedAt, DateTime.utc(2026, 8, 13, 9, 30));
    expect(enqueued.enqueuedAt, DateTime.utc(2026, 8, 13, 18));

    final relaunched = await fixture.restart();
    final stored = (await relaunched.snapshot()).pending.single;
    expect(stored.recordedAt, DateTime.utc(2026, 8, 13, 9, 30));
    expect(stored.enqueuedAt, DateTime.utc(2026, 8, 13, 18));
  });

  group('ordering — FIFO within a key, nothing between keys', () {
    test("one job's operations are claimed in the order they were recorded", () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      await enqueueMilestone(queue, milestone: 'en_route_to_pickup');
      await enqueueMilestone(queue, milestone: 'picked_up');
      await enqueueMilestone(queue, milestone: 'in_transit');

      final drained = <String>[];
      for (var claimed = await queue.claim(); claimed != null; claimed = await queue.claim()) {
        drained.add(claimed.body['milestone'] as String);
        await queue.complete(claimed.id);
      }

      expect(drained, ['en_route_to_pickup', 'picked_up', 'in_transit']);
    });

    test('a claimed operation holds its own key and holds no other', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      await enqueueMilestone(queue, job: 'job-a', milestone: 'picked_up');
      await enqueueMilestone(queue, job: 'job-a', milestone: 'in_transit');
      await enqueueMilestone(queue, job: 'job-b', milestone: 'picked_up');

      final first = await queue.claim();
      expect(first!.orderingKey, 'job:job-a');

      // job-a is held by the claim above, so the next claimable head is job-b's — not job-a's
      // second milestone, which would reach the platform ahead of the one before it.
      final second = await queue.claim();
      expect(second!.orderingKey, 'job:job-b');

      expect(await queue.claim(), isNull);
    });

    test('a stored next-attempt time holds an operation back, and survives a restart', () async {
      final fixture = QueueFixture.temporary();
      var now = DateTime.utc(2026, 8, 13, 9);
      final queue = fixture.open(now: () => now);

      final enqueued = await enqueueMilestone(queue);
      final claimed = await queue.claim();
      expect(claimed, isNotNull);

      await queue.release(enqueued.id, nextAttemptAt: DateTime.utc(2026, 8, 13, 9, 5));
      expect(await queue.claim(), isNull, reason: 'the backoff has not elapsed');

      // A relaunch is where an in-memory backoff would quietly become no backoff at all.
      final relaunched = await fixture.restart(now: () => now);
      expect(await relaunched.claim(), isNull);

      now = DateTime.utc(2026, 8, 13, 9, 6);
      expect((await relaunched.claim())!.id, enqueued.id);
    });
  });

  group('the seam SHIP-125 drains through', () {
    test('release returns a claimed operation to pending and nothing else does', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final enqueued = await enqueueMilestone(queue);

      expect(await queue.release(enqueued.id), isFalse, reason: 'it was never claimed');

      await queue.claim();
      expect(await queue.release(enqueued.id), isTrue);
      expect((await queue.snapshot()).pending.single.state, QueueState.pending);
    });

    test('complete removes a claimed operation and refuses a pending one', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final enqueued = await enqueueMilestone(queue);

      expect(await queue.complete(enqueued.id), isFalse);
      expect(await queue.count(), 1, reason: 'nothing may delete work that was never sent');

      await queue.claim();
      expect(await queue.complete(enqueued.id), isTrue);
      expect(await queue.count(), 0);
    });

    test('acknowledge removes a blocked operation and refuses a live one', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final enqueued = await enqueueMilestone(queue);

      expect(await queue.acknowledge(enqueued.id), isFalse);

      await queue.claim();
      expect(await queue.acknowledge(enqueued.id), isFalse, reason: 'in flight is not blocked');

      await queue.block(enqueued.id, reason: QueueBlockReason.refused, detail: 'job cancelled');
      expect(await queue.acknowledge(enqueued.id), isTrue);
      expect(await queue.count(), 0);
    });

    test('block records why, and keeps the operation', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();
      final enqueued = await enqueueMilestone(queue);

      await queue.block(
        enqueued.id,
        reason: QueueBlockReason.refused,
        detail: 'an administrator cancelled the job',
      );

      final relaunched = await fixture.restart();
      final blocked = (await relaunched.snapshot()).blocked.single;

      expect(blocked.id, enqueued.id);
      expect(blocked.reason, QueueBlockReason.refused);
      expect(blocked.detail, 'an administrator cancelled the job');
      expect(blocked.idempotencyKey, enqueued.idempotencyKey);
    });
  });

  group('the snapshot', () {
    test('partitions the table into pending, in flight and blocked', () async {
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      final a = await enqueueMilestone(queue, job: 'job-a');
      await enqueueMilestone(queue, job: 'job-b');
      final c = await enqueueMilestone(queue, job: 'job-c');

      await queue.block(a.id, reason: QueueBlockReason.refused);
      final claimed = await queue.claim();

      final snapshot = await queue.snapshot();

      expect(snapshot.total, 3);
      expect(snapshot.pending.length + snapshot.inFlight.length + snapshot.blocked.length, 3);
      expect(snapshot.blocked.single.id, a.id);
      expect(snapshot.inFlight.single.id, claimed!.id);
      expect(snapshot.pending.single.id, c.id);
    });

    test('counts pending and in-flight work as unsynced, and blocked work separately', () async {
      // Docs/02 §3.1's persistent indicator answers "is my work recorded yet". A blocked operation
      // is not waiting for a connection, so counting it there would leave a number that never
      // falls however long the driver stands in the open — SHIP-132 shows it as its own thing.
      final fixture = QueueFixture.temporary();
      final queue = fixture.open();

      await enqueueMilestone(queue, job: 'job-a');
      await enqueueMilestone(queue, job: 'job-b');
      final c = await enqueueMilestone(queue, job: 'job-c');
      await queue.claim();
      await queue.block(c.id, reason: QueueBlockReason.refused);

      final snapshot = await queue.snapshot();
      expect(snapshot.unsynced, 2);
      expect(snapshot.blocked.length, 1);
      expect(snapshot.total, 3);
    });
  });

  test('the policy is a value the queue is given, not a constant it holds', () async {
    // CLAUDE.md keeps anything that changes under operational pressure off the device. A queue
    // cannot wait for the platform to tell it how large it may be — being unreachable is the
    // situation it exists for — so what is available is that the bound is a constructor argument.
    final fixture = QueueFixture.temporary();
    final queue = fixture.open(policy: const QueuePolicy(maxOperations: 1, maxBodyBytes: 32));

    expect(queue.policy.maxOperations, 1);
    expect(queue.policy.maxBodyBytes, 32);
    expect(const QueuePolicy().maxOperations, greaterThan(1));
  });

  test('the wire strings the companions use are the ones the states declare', () async {
    // operation_queue.dart repeats 'pending' and 'in_flight' as compile-time constants, because a
    // `const` companion cannot name an enum field. This is what stops the two copies drifting.
    final fixture = QueueFixture.temporary();
    final queue = fixture.open();
    final enqueued = await enqueueMilestone(queue);

    expect(await fixture.stateOf(enqueued.id), QueueState.pending.wire);
    await queue.claim();
    expect(await fixture.stateOf(enqueued.id), QueueState.inFlight.wire);
    await queue.release(enqueued.id);
    expect(await fixture.stateOf(enqueued.id), QueueState.pending.wire);
  });
}
