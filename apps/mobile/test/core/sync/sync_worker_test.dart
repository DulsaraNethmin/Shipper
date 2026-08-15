// SHIP-125 — "queue drains on reconnection with exponential backoff and per-item idempotency
// keys". Three clauses, and each one is a group below.
//
// The queue underneath is SHIP-124's, over a real SQLite file. Nothing here reaches a network:
// what "the platform answered 503" means is a decision this ticket takes, and a test that had to
// arrange a real 503 would be testing a stub server.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/backoff.dart';
import 'package:shipper/core/sync/sync_signals.dart';

import '../queue/queue_fixture.dart';
import 'sync_fixture.dart';

void main() {
  group('what starts a drain', () {
    test('launch recovers what a dead process was mid-send, and sends it again', () async {
      // The case `recover()` exists for: the process died between the claim and the answer, so
      // the row is sitting in `in_flight` where `claim()` cannot see it. Nothing times it out —
      // a handset runs one app process, and this is the only other party there is.
      final first = SyncHarness.create();
      final enqueued = await enqueueMilestone(first.queue);
      final claimed = await first.queue.claim();
      expect(claimed, isNotNull, reason: 'the send that the process died during');

      final relaunched = await first.relaunch();
      await relaunched.worker.start();

      expect(relaunched.sender.sent.single.id, enqueued.id);
      expect(relaunched.worker.lastTrigger, SyncTrigger.launch);
      expect(await relaunched.queue.count(), 0, reason: 'the platform took it');
    });

    test('a signal from outside the app drains, whatever the signal was about', () async {
      // The seam a connectivity package plugs into. It is a *hint*: the worker treats a
      // reconnection event as a reason to try now, never as evidence that a request will succeed.
      final harness = SyncHarness.create();
      await enqueueMilestone(harness.queue);

      harness.signals.emit(SyncTrigger.connectivity);
      await pumpEventQueue();
      await harness.worker.drained;

      expect(harness.sender.sent, hasLength(1));
      expect(harness.worker.lastTrigger, SyncTrigger.connectivity);
    });

    test('the app coming back to the foreground drains', () async {
      final harness = SyncHarness.create();
      await enqueueMilestone(harness.queue);

      harness.signals.emit(SyncTrigger.resumed);
      await pumpEventQueue();
      await harness.worker.drained;

      expect(harness.sender.sent, hasLength(1));
      expect(harness.worker.lastTrigger, SyncTrigger.resumed);
    });

    test('recording something sends it, without the screen having to ask', () async {
      // `record` is enqueue-and-drain in one call, which is what stops "wake the worker
      // afterwards" being a rule every screen that records anything has to remember.
      final harness = SyncHarness.create();

      await harness.worker.record(
        kind: OperationKind.milestone,
        orderingKey: 'job:job-a',
        method: 'POST',
        path: '/v1/jobs/job-a/milestones',
        body: <String, dynamic>{'milestone': 'picked_up'},
        recordedAt: DateTime.utc(2026, 8, 13, 9),
      );
      await harness.worker.drained;

      expect(harness.sender.sent, hasLength(1));
      expect(harness.worker.lastTrigger, SyncTrigger.recorded);
      expect(await harness.queue.count(), 0);
    });

    test('the worker wakes itself at the moment the next operation may be tried', () async {
      // **The trigger that makes the other five safe to get wrong.** A handset regains a route
      // with no event at all — a mast recovers, a captive portal is signed into — and a worker
      // that drained only on a connectivity event would wait forever for one that never came.
      var now = DateTime.utc(2026, 8, 13, 9);
      final harness = SyncHarness.create(
        clock: () => now,
        sender: ScriptedSender(answers: [answer(500, 'internal_error')]),
      );
      await enqueueMilestone(harness.queue);

      await harness.worker.drain(SyncTrigger.launch);
      expect(harness.alarm.armedFor, const Duration(seconds: 5));

      now = now.add(const Duration(seconds: 5));
      harness.alarm.fire();
      await harness.worker.drained;

      expect(harness.sender.sent, hasLength(2));
      expect(harness.worker.lastTrigger, SyncTrigger.scheduled);
      expect(await harness.queue.count(), 0);
    });

    test('an idle phone arms no wake-up at all', () async {
      // A schedule and not a poll: nothing pending means nothing to wait for, and a timer ticking
      // against an empty queue is battery spent on arithmetic.
      final harness = SyncHarness.create();

      await harness.worker.drain(SyncTrigger.launch);
      expect(harness.alarm.isArmed, isFalse);

      await enqueueMilestone(harness.queue);
      await harness.worker.drain(SyncTrigger.recorded);
      expect(harness.alarm.isArmed, isFalse, reason: 'it was accepted, so nothing is waiting');
    });

    test('a queue of nothing but blocked work arms no wake-up either', () async {
      // A blocked operation is waiting for a person, not for a connection (`Docs/02` §3.1). A
      // timer for it would fire for the rest of the battery and claim nothing.
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: answer(404, 'not_found')),
      );
      await enqueueMilestone(harness.queue);

      await harness.worker.drain(SyncTrigger.launch);

      expect((await harness.queue.snapshot()).blocked, hasLength(1));
      expect(harness.alarm.isArmed, isFalse);
    });

    test('two triggers at once are one pass, and the second is not lost', () async {
      // Two passes in parallel would put two operations on the wire together, which is the one
      // thing the per-key ordering guarantee cannot survive.
      final harness = SyncHarness.create();
      await enqueueMilestone(harness.queue, job: 'job-a');

      final first = harness.worker.drain(SyncTrigger.launch);
      final second = harness.worker.drain(SyncTrigger.resumed);
      await Future.wait([first, second]);

      expect(harness.sender.sent, hasLength(1));
    });

    test('nothing is sent while there is no session to send it under', () async {
      // A cold start would otherwise spend an attempt — and a stored backoff — on a request with
      // no bearer token on it, which the platform answers `401` for a reason the queue cannot fix.
      final harness = SyncHarness.create();
      await enqueueMilestone(harness.queue);

      harness.worker.pause();
      await harness.worker.drain(SyncTrigger.launch);
      expect(harness.sender.sent, isEmpty);
      expect(await harness.queue.count(), 1, reason: 'held, not lost');

      harness.worker.resume(SyncTrigger.session);
      await harness.worker.drained;
      expect(harness.sender.sent, hasLength(1));
    });
  });

  group('exponential backoff', () {
    test('grows with each failed attempt, and is stored rather than remembered', () async {
      var now = DateTime.utc(2026, 8, 13, 9);
      final harness = SyncHarness.create(
        clock: () => now,
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      final enqueued = await enqueueMilestone(harness.queue);

      await harness.worker.drain(SyncTrigger.launch);
      expect(await harness.nextAttemptOf(enqueued.id), now.add(const Duration(seconds: 5)));

      now = now.add(const Duration(seconds: 5));
      await harness.worker.drain(SyncTrigger.scheduled);
      expect(await harness.nextAttemptOf(enqueued.id), now.add(const Duration(seconds: 10)));

      now = now.add(const Duration(seconds: 10));
      await harness.worker.drain(SyncTrigger.scheduled);
      expect(await harness.nextAttemptOf(enqueued.id), now.add(const Duration(seconds: 20)));
    });

    test('holds at the ceiling however long the outage runs', () async {
      var now = DateTime.utc(2026, 8, 13, 9);
      final harness = SyncHarness.create(
        clock: () => now,
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      final enqueued = await enqueueMilestone(harness.queue);

      for (var attempt = 0; attempt < 12; attempt++) {
        await harness.worker.drain(SyncTrigger.scheduled);
        now = (await harness.nextAttemptOf(enqueued.id))!;
      }

      await harness.worker.drain(SyncTrigger.scheduled);
      expect(
        await harness.nextAttemptOf(enqueued.id),
        now.add(const Duration(minutes: 5)),
        reason: 'without a ceiling this would be days away, which is not retrying at all',
      );
    });

    test('survives the relaunch it exists for', () async {
      // **A backoff reset by every launch is no backoff on a handset**, which is restarted far
      // more often than a server. SHIP-124 stores the instant in the row for exactly this, and
      // this is the test that the worker writes it there rather than holding it.
      var now = DateTime.utc(2026, 8, 13, 9);
      final first = SyncHarness.create(
        clock: () => now,
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      final enqueued = await enqueueMilestone(first.queue);

      // Four failures in all, so the wait is 40 seconds rather than the base.
      for (var attempt = 0; attempt < 3; attempt++) {
        await first.worker.drain(SyncTrigger.scheduled);
        now = (await first.nextAttemptOf(enqueued.id))!;
      }
      await first.worker.drain(SyncTrigger.scheduled);
      final due = (await first.nextAttemptOf(enqueued.id))!;
      expect(due, now.add(const Duration(seconds: 40)));

      final relaunched = await first.relaunch(clock: () => now);
      await relaunched.worker.start();
      expect(relaunched.sender.sent, isEmpty, reason: 'the stored wait has not elapsed');
      expect(relaunched.alarm.armedFor, const Duration(seconds: 40));

      now = due.add(const Duration(seconds: 1));
      relaunched.alarm.fire();
      await relaunched.worker.drained;
      expect(relaunched.sender.sent, hasLength(1));
    });

    test('a fresh operation starts at the base however long another has been failing', () async {
      // Nothing resets a stored wait — SHIP-124's seam cannot clear one, deliberately, because a
      // worker that could pull an operation forward could reorder the sequence its key preserves.
      // What resets the schedule is the operation leaving the queue, and a new one starting at
      // zero.
      var now = DateTime.utc(2026, 8, 13, 9);
      final harness = SyncHarness.create(
        clock: () => now,
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      final old = await enqueueMilestone(harness.queue, job: 'job-a');

      for (var attempt = 0; attempt < 6; attempt++) {
        await harness.worker.drain(SyncTrigger.scheduled);
        now = (await harness.nextAttemptOf(old.id))!;
      }
      // A second short of its next attempt, so the old operation is not claimable and the pass
      // reaches the fresh one rather than stopping on the link failure again.
      now = now.subtract(const Duration(seconds: 1));

      final fresh = await enqueueMilestone(harness.queue, job: 'job-b');
      await harness.worker.drain(SyncTrigger.recorded);

      expect(await harness.nextAttemptOf(fresh.id), now.add(const Duration(seconds: 5)));
    });
  });

  group('per-item idempotency keys', () {
    test('the same key goes out on the second and third attempts', () async {
      // `Docs/07` §4: generated once where the user acts, reused unchanged across every retry. A
      // key minted per attempt would make each retry a new operation, and SHIP-111's endpoint
      // would record a second milestone on the customer's timeline for one thing the driver did.
      var now = DateTime.utc(2026, 8, 13, 9);
      final harness = SyncHarness.create(
        clock: () => now,
        sender: ScriptedSender(
          answers: [answer(500, 'internal_error'), answer(503, 'service_unavailable')],
        ),
      );
      final enqueued = await enqueueMilestone(harness.queue);

      await harness.worker.drain(SyncTrigger.launch);
      now = now.add(const Duration(minutes: 1));
      await harness.worker.drain(SyncTrigger.scheduled);
      now = now.add(const Duration(minutes: 1));
      await harness.worker.drain(SyncTrigger.scheduled);

      expect(harness.sender.keys, ['key-1', 'key-1', 'key-1']);
      expect(harness.sender.sent.map((o) => o.attempts), [1, 2, 3]);
      expect(enqueued.idempotencyKey, 'key-1');
      expect(await harness.queue.count(), 0, reason: 'the third attempt was accepted');
    });

    test('and the same key again after the app has been restarted', () async {
      // The gap the key is really for: a driver records in a pickup bay with no signal and the
      // phone syncs the next day, by which point the platform's cached response has long expired
      // and the request runs again all the way to the table. SHIP-111's partial unique index is
      // what makes that safe, and it can only do its job if the key is the same one.
      var now = DateTime.utc(2026, 8, 13, 9);
      final first = SyncHarness.create(
        clock: () => now,
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      final enqueued = await enqueueMilestone(first.queue);
      await first.worker.drain(SyncTrigger.launch);
      expect(first.sender.keys, [enqueued.idempotencyKey]);

      now = DateTime.utc(2026, 8, 14, 7);
      final relaunched = await first.relaunch(clock: () => now);
      await relaunched.worker.start();

      expect(relaunched.sender.keys, [enqueued.idempotencyKey]);
      expect(relaunched.sender.sent.single.attempts, 2, reason: 'the attempt still counted');
    });

    test('a refresh-and-replay is one attempt from here, and cannot change the key', () async {
      // `AuthInterceptor` replays the original `RequestOptions` after refreshing (SHIP-50), so the
      // `Idempotency-Key` header goes back unchanged and the worker never sees the `401` at all.
      // What the worker must not do is mint a key of its own for the operation, and it cannot:
      // there is one key, on the row, and nothing here writes to that column.
      final harness = SyncHarness.create();
      final enqueued = await enqueueMilestone(harness.queue);

      await harness.worker.drain(SyncTrigger.launch);

      expect(harness.sender.sent.single.idempotencyKey, enqueued.idempotencyKey);
    });
  });

  group('retry, or refuse — and the awkward ones', () {
    Future<QueueBlockReason?> blockedReason(SyncHarness harness) async {
      final blocked = (await harness.queue.snapshot()).blocked;
      return blocked.isEmpty ? null : blocked.single.reason;
    }

    /// Drives one operation against one answer and reports what became of it.
    Future<({int remaining, QueueBlockReason? blocked, int sends})> against(
      Object failure,
    ) async {
      final harness = SyncHarness.create(sender: ScriptedSender(thereafter: failure));
      await enqueueMilestone(harness.queue);
      await harness.worker.drain(SyncTrigger.launch);

      final snapshot = await harness.queue.snapshot();
      return (
        remaining: snapshot.pending.length + snapshot.inFlight.length,
        blocked: await blockedReason(harness),
        sends: harness.sender.sent.length,
      );
    }

    test('no route is retried — it is the case the whole queue exists for', () async {
      final outcome = await against(const ApiUnreachable());
      expect(outcome.remaining, 1);
      expect(outcome.blocked, isNull);
    });

    test('a timeout is retried', () async {
      final outcome = await against(const ApiUnreachable(timedOut: true));
      expect(outcome.remaining, 1);
      expect(outcome.blocked, isNull);
    });

    test('a response that was not the error contract at all is retried', () async {
      // A proxy's HTML page, or an empty 502. Nothing in it says whether the service saw the
      // request, so refusing would quarantine something the platform may already hold.
      final outcome = await against(const ApiMalformedResponse(statusCode: 502));
      expect(outcome.remaining, 1);
      expect(outcome.blocked, isNull);
    });

    test('a 500 is retried, because the platform may even have committed', () async {
      final outcome = await against(answer(500, 'internal_error'));
      expect(outcome.remaining, 1);
      expect(outcome.blocked, isNull);
    });

    test('a 503 is retried — the idempotency store fails closed, and that is a 503', () async {
      final outcome = await against(answer(503, 'service_unavailable'));
      expect(outcome.remaining, 1);
      expect(outcome.blocked, isNull);
    });

    test('a 429 is retried rather than refused', () async {
      final outcome = await against(answer(429, 'rate_limited'));
      expect(outcome.remaining, 1);
      expect(outcome.blocked, isNull);
    });

    test('a 401 a token refresh would fix is retried, never quarantined', () async {
      // The transport has already refreshed once and replayed (SHIP-50), so a `401` arriving here
      // means no credential could be had *at that moment* — most often a refresh that failed on
      // the same dead link the operation did. Quarantining it would strand a driver's delivery
      // update because a token expired in a tunnel.
      final expired = await against(answer(401, 'token_expired'));
      expect(expired.remaining, 1);
      expect(expired.blocked, isNull);

      final unauthenticated = await against(answer(401, 'unauthenticated'));
      expect(unauthenticated.remaining, 1);
      expect(unauthenticated.blocked, isNull);
    });

    test('a 409 saying the first attempt is still running is retried', () async {
      // The success hiding behind a failure status: the platform is at this moment running this
      // operation's own first attempt. Refusing it would quarantine an operation seconds before
      // it was recorded.
      final outcome = await against(answer(409, 'idempotency_request_in_progress'));
      expect(outcome.remaining, 1);
      expect(outcome.blocked, isNull);
    });

    test('a 409 saying the job has moved on is quarantined for a person', () async {
      // "Valid, but it contradicts the current state" — a queued update that lost to an
      // administrative action. `Docs/02` §3.1 requires it retained and shown, which is what
      // blocked means, and SHIP-132 is the screen that shows it.
      final outcome = await against(answer(409, 'conflict'));
      expect(outcome.remaining, 0);
      expect(outcome.blocked, QueueBlockReason.refused);
    });

    test('a milestone the job cannot take is quarantined, and kept', () async {
      final outcome = await against(answer(422, 'delivery_milestone_not_permitted'));
      expect(outcome.blocked, QueueBlockReason.refused);
    });

    test('a 404 and a 403 are quarantined, because no retry can change them', () async {
      expect((await against(answer(404, 'not_found'))).blocked, QueueBlockReason.refused);
      expect((await against(answer(403, 'forbidden'))).blocked, QueueBlockReason.refused);
    });

    test('a quarantined operation is kept whole — key, attempts and a request id', () async {
      // Never deleted. There is no dead-letter path, for the reason SHIP-135 gave on the platform
      // side: it is a place things go to stop being anybody's problem.
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: answer(409, 'conflict')),
      );
      final enqueued = await enqueueMilestone(harness.queue);

      await harness.worker.drain(SyncTrigger.launch);
      final blocked = (await harness.queue.snapshot()).blocked.single;

      expect(blocked.id, enqueued.id);
      expect(blocked.idempotencyKey, enqueued.idempotencyKey);
      expect(blocked.attempts, 1);
      expect(blocked.detail, contains('req-abc123'), reason: 'support needs the request id');
      expect(blocked.detail, isNot(contains('The platform said so.')),
          reason: 'the copy is not stored; a screen writes its own words');
    });

    test('an operation this build cannot send is quarantined rather than retried forever',
        () async {
      // Not a platform answer — the request never left. Deterministic by construction, so a retry
      // is a loop against a rule it can never satisfy; quarantine keeps it counted and visible.
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: StateError('no Idempotency-Key on this request')),
      );
      await enqueueMilestone(harness.queue);

      await harness.worker.drain(SyncTrigger.launch);

      expect((await harness.queue.snapshot()).blocked.single.reason, QueueBlockReason.unsupported);
      expect(harness.sender.sent, hasLength(1), reason: 'tried once, not in a loop');
    });

    test('a proof photograph is held rather than sent half-formed', () async {
      // `Docs/07` §4 uploads it as a local file, which is a multipart send SHIP-130 writes.
      // Nothing can enqueue one yet; this is here so the case stays loud if that changes first.
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: UnimplementedError('SHIP-130')),
      );
      await harness.queue.enqueue(
        kind: OperationKind.proof,
        orderingKey: 'job:job-a',
        method: 'POST',
        path: '/v1/jobs/job-a/proof',
        body: <String, dynamic>{'milestone': 'delivered'},
        recordedAt: DateTime.utc(2026, 8, 13, 9),
        attachmentPath: '/var/mobile/proof-1.jpg',
      );

      await harness.worker.drain(SyncTrigger.launch);

      expect((await harness.queue.snapshot()).blocked.single.reason, QueueBlockReason.unsupported);
    });
  });

  group("one job's problem does not become another job's", () {
    test('a refusal holds its own key and stops nothing else in the same pass', () async {
      final harness = SyncHarness.create(
        sender: ScriptedSender(answers: [answer(409, 'conflict')]),
      );
      await enqueueMilestone(harness.queue, job: 'job-a');
      await enqueueMilestone(harness.queue, job: 'job-b');

      await harness.worker.drain(SyncTrigger.launch);

      expect(harness.sender.sent, hasLength(2));
      final snapshot = await harness.queue.snapshot();
      expect(snapshot.blocked.single.orderingKey, 'job:job-a');
      expect(snapshot.total, 1, reason: "job-b's was accepted and removed");
    });

    test('a 500 on one job does not stop another job being tried in the same pass', () async {
      // The answer came back from the platform, which proves there is a route. Stopping here
      // would let one job's problem stall another's — the head-of-line block that the per-key
      // ordering exists to confine.
      final harness = SyncHarness.create(
        sender: ScriptedSender(answers: [answer(500, 'internal_error')]),
      );
      await enqueueMilestone(harness.queue, job: 'job-a');
      await enqueueMilestone(harness.queue, job: 'job-b');

      await harness.worker.drain(SyncTrigger.launch);

      expect(harness.sender.sent.map((o) => o.orderingKey), ['job:job-a', 'job:job-b']);
      expect((await harness.queue.snapshot()).pending.single.orderingKey, 'job:job-a');
    });

    test('no route stops the pass, because nothing else would get out either', () async {
      // The one failure that is about the link rather than about the operation. Trying every
      // other queued operation is a radio wake-up each in exchange for nothing.
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      await enqueueMilestone(harness.queue, job: 'job-a');
      await enqueueMilestone(harness.queue, job: 'job-b');

      await harness.worker.drain(SyncTrigger.launch);

      expect(harness.sender.sent, hasLength(1));
      expect(harness.alarm.armedFor, const Duration(seconds: 5),
          reason: "the wake-up is the released operation's, not now for the untouched ones");
    });

    test('a 429 stops the pass, because it is about the caller and not the operation', () async {
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: answer(429, 'rate_limited')),
      );
      await enqueueMilestone(harness.queue, job: 'job-a');
      await enqueueMilestone(harness.queue, job: 'job-b');

      await harness.worker.drain(SyncTrigger.launch);

      expect(harness.sender.sent, hasLength(1));
    });

    test("one job's operations reach the platform in the order they were recorded", () async {
      final harness = SyncHarness.create();
      await enqueueMilestone(harness.queue, milestone: 'en_route_to_pickup');
      await enqueueMilestone(harness.queue, milestone: 'picked_up');
      await enqueueMilestone(harness.queue, milestone: 'in_transit');

      await harness.worker.drain(SyncTrigger.launch);

      expect(
        harness.sender.sent.map((o) => o.body['milestone']),
        ['en_route_to_pickup', 'picked_up', 'in_transit'],
      );
    });

    test('a blocked operation holds the rest of its own key, and is not stepped over', () async {
      // Releasing what is behind it past it would silently reorder exactly the sequence the key
      // exists to preserve.
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: answer(409, 'conflict')),
      );
      await enqueueMilestone(harness.queue, milestone: 'picked_up');
      await enqueueMilestone(harness.queue, milestone: 'in_transit');

      await harness.worker.drain(SyncTrigger.launch);

      expect(harness.sender.sent, hasLength(1));
      expect(harness.alarm.isArmed, isFalse, reason: 'nothing behind a blocked head is claimable');
      expect((await harness.queue.snapshot()).pending.single.body['milestone'], 'in_transit');
    });
  });

  group('what the screens after this one read', () {
    test('a snapshot is published after every pass, and counts unsynced work', () async {
      // The seam SHIP-126 consumes. It costs nothing: the worker already reads a snapshot after
      // each pass to decide when to wake next.
      final harness = SyncHarness.create(
        sender: ScriptedSender(answers: [answer(409, 'conflict')], thereafter: const ApiUnreachable()),
      );
      final published = <int>[];
      harness.worker.snapshots.listen((snapshot) => published.add(snapshot.unsynced));

      await enqueueMilestone(harness.queue, job: 'job-a');
      await enqueueMilestone(harness.queue, job: 'job-b');
      await harness.worker.drain(SyncTrigger.launch);
      await pumpEventQueue();

      expect(published, [1], reason: 'the blocked one waits for a person, not a connection');
      expect(harness.worker.lastSnapshot!.blocked, hasLength(1));
      expect(harness.worker.lastSnapshot!.total, 2);
    });
  });

  group('sign-out', () {
    test('discards the queue and says how much it discarded', () async {
      // `Docs/07` §3. One account's unsynced work must never be sent under another's credentials,
      // and from this ticket onwards a leftover row is a row something will pick up and send.
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      await enqueueMilestone(harness.queue, job: 'job-a');
      await enqueueMilestone(harness.queue, job: 'job-b');
      await harness.worker.drain(SyncTrigger.launch);

      expect(await harness.worker.forget(), 2);
      expect(await harness.queue.count(), 0);
      expect(harness.alarm.isArmed, isFalse);
    });

    test('stops the worker, so nothing is sent after the session ended', () async {
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      await enqueueMilestone(harness.queue);
      await harness.worker.drain(SyncTrigger.launch);
      expect(harness.sender.sent, hasLength(1));

      await harness.worker.forget();
      await enqueueMilestone(harness.queue, job: 'job-later');
      await harness.worker.drain(SyncTrigger.recorded);

      expect(harness.sender.sent, hasLength(1), reason: 'paused until a session appears again');
    });
  });

  test('a slower policy needs no change to the worker', () async {
    var now = DateTime.utc(2026, 8, 13, 9);
    final harness = SyncHarness.create(
      clock: () => now,
      backoff: const Backoff(base: Duration(minutes: 1), ceiling: Duration(hours: 1)),
      sender: ScriptedSender(thereafter: const ApiUnreachable()),
    );
    final enqueued = await enqueueMilestone(harness.queue);

    await harness.worker.drain(SyncTrigger.launch);

    expect(await harness.nextAttemptOf(enqueued.id), now.add(const Duration(minutes: 1)));
    expect(harness.alarm.armedFor, const Duration(minutes: 1));
  });

  group('acknowledging a quarantined operation (SHIP-132)', () {
    /// A queue holding one refused operation, which is the state SHIP-132's panel draws.
    Future<SyncHarness> withOneRefused() async {
      final harness = SyncHarness.create(
        sender: ScriptedSender(
          thereafter: const ApiErrorResponse(statusCode: 409, code: 'conflict', message: 'lost'),
        ),
      );
      await enqueueMilestone(harness.queue);
      await harness.worker.drain(SyncTrigger.launch);

      expect((await harness.queue.snapshot()).blocked, hasLength(1));
      return harness;
    }

    test('a person removes it, and nothing else does', () async {
      // Docs/02 §3.1: a queued update that lost is **retained and shown**, not discarded. Nothing
      // in the worker deletes one — a whole drain over a queue of nothing but blocked work leaves
      // it exactly where it was, which is what makes the panel's button the only way out.
      final harness = await withOneRefused();

      await harness.worker.drain(SyncTrigger.connectivity);
      expect((await harness.queue.snapshot()).blocked, hasLength(1));

      final blocked = (await harness.queue.snapshot()).blocked.single;
      expect(await harness.worker.acknowledge(blocked.id), isTrue);
      expect(await harness.queue.count(), 0);
    });

    test('it republishes, so the indicator stops counting it', () async {
      // The reason this is on the worker rather than a direct call to `OperationQueue.acknowledge`
      // from the panel: the queue removes the row and publishes nothing, so a bar counting blocked
      // work would keep counting it until the next pass — which on a phone with nothing left to
      // send is never.
      final harness = await withOneRefused();
      final blocked = (await harness.queue.snapshot()).blocked.single;

      final published = <QueueSnapshot>[];
      final watching = harness.worker.snapshots.listen(published.add);
      addTearDown(watching.cancel);

      await harness.worker.acknowledge(blocked.id);
      await pumpEventQueue();

      expect(published, isNotEmpty, reason: 'the removal has to reach whoever is drawing the bar');
      expect(published.last.blocked, isEmpty);
    });

    test('acknowledging the same thing twice is not an error', () async {
      // A double tap on a phone. The second answers false and removes nothing rather than
      // reporting a failure to somebody who did what the screen asked.
      final harness = await withOneRefused();
      final blocked = (await harness.queue.snapshot()).blocked.single;

      expect(await harness.worker.acknowledge(blocked.id), isTrue);
      expect(await harness.worker.acknowledge(blocked.id), isFalse);
    });

    test('it cannot take an operation that is still waiting to be sent', () async {
      // The guard that makes this a removal rather than a drop. `OperationQueue.acknowledge`
      // refuses anything pending or in flight by construction, so a panel handed the wrong
      // identifier cannot take unsent work off a driver's phone.
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
      );
      final pending = await enqueueMilestone(harness.queue);

      expect(await harness.worker.acknowledge(pending.id), isFalse);
      expect(await harness.queue.count(), 1);
    });
  });
}
