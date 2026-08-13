// The parts a sync-worker test hands the worker instead of a network, a clock and a timer.
//
// The queue underneath is the real one, over a real SQLite file — see queue_fixture.dart for why
// an in-memory database would make these tests pass while proving nothing about the half of this
// ticket that is *durable* backoff.

import 'dart:async';

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/backoff.dart';
import 'package:shipper/core/sync/operation_sender.dart';
import 'package:shipper/core/sync/sync_alarm.dart';
import 'package:shipper/core/sync/sync_signals.dart';
import 'package:shipper/core/sync/sync_worker.dart';

import '../queue/queue_fixture.dart';

/// A sender that answers from a script, and remembers every operation it was handed.
///
/// [sent] is the assertion surface for the ticket's third clause: the same operation appearing
/// three times with the same `idempotencyKey` is what "generated once, reused on every retry"
/// actually means, and a sender that recorded only the last call could not show it.
final class ScriptedSender implements OperationSender {
  ScriptedSender({this.answers = const <Object?>[], this.thereafter});

  /// One entry per call: `null` accepts, anything else is thrown.
  final List<Object?> answers;

  /// What every call past [answers] does. `null` accepts.
  final Object? thereafter;

  final sent = <QueuedOperation>[];

  /// The idempotency key of every attempt, in order.
  List<String> get keys => sent.map((o) => o.idempotencyKey).toList();

  @override
  Future<void> send(QueuedOperation operation) async {
    sent.add(operation);

    final answer = sent.length <= answers.length ? answers[sent.length - 1] : thereafter;
    if (answer != null) throw answer;
  }
}

/// The worker's single pending wake-up, held where a test can read and fire it.
final class TestAlarm implements SyncAlarm {
  Duration? armedFor;
  int arms = 0;
  void Function()? _fire;

  bool get isArmed => _fire != null;

  @override
  void arm(Duration delay, void Function() fire) {
    armedFor = delay;
    _fire = fire;
    arms++;
  }

  @override
  void disarm() {
    armedFor = null;
    _fire = null;
  }

  /// The wake-up going off.
  void fire() {
    final fire = _fire;
    armedFor = null;
    _fire = null;
    fire?.call();
  }
}

/// A signal source a test pushes onto.
final class TestSignals implements SyncSignals {
  final _controller = StreamController<SyncTrigger>.broadcast();

  @override
  Stream<SyncTrigger> get signals => _controller.stream;

  void emit(SyncTrigger trigger) => _controller.add(trigger);

  @override
  void dispose() => unawaited(_controller.close());
}

/// One handset: a queue on disk, a scripted network, a clock the test moves, and a worker.
final class SyncHarness {
  SyncHarness._(this.storage, this.queue, this.worker, this.sender, this.alarm, this.signals);

  /// [roll] is the jitter draw, fixed so a test asserts on an exact delay rather than a range.
  /// `1` is the top of the jittered window, which is the nominal delay.
  factory SyncHarness.create({
    ScriptedSender? sender,
    Backoff backoff = const Backoff(),
    double roll = 1,
    DateTime Function()? clock,
    QueueFixture? storage,
    String Function()? mintKey,
  }) {
    final files = storage ?? QueueFixture.temporary();
    final now = clock ?? () => DateTime.utc(2026, 8, 13, 9);
    final queue = files.open(mintKey: mintKey ?? sequentialKeys(), now: now);
    final scripted = sender ?? ScriptedSender();
    final alarm = TestAlarm();
    final signals = TestSignals();

    final worker = SyncWorker(
      queue: queue,
      sender: scripted,
      backoff: backoff,
      signals: signals,
      alarm: alarm,
      now: now,
      roll: () => roll,
    );
    addTearDown(worker.dispose);

    return SyncHarness._(files, queue, worker, scripted, alarm, signals);
  }

  final QueueFixture storage;
  final OperationQueue queue;
  final SyncWorker worker;
  final ScriptedSender sender;
  final TestAlarm alarm;
  final TestSignals signals;

  /// Closes everything and opens it again over the same bytes on disk. The app being restarted.
  ///
  /// The only way to show that a backoff is durable: one held in memory would survive a `restart`
  /// of the worker object and would vanish here, which is what happens on a handset several times
  /// a day.
  Future<SyncHarness> relaunch({
    ScriptedSender? sender,
    Backoff backoff = const Backoff(),
    double roll = 1,
    DateTime Function()? clock,
  }) async {
    await worker.dispose();
    await storage.close();

    return SyncHarness.create(
      sender: sender,
      backoff: backoff,
      roll: roll,
      clock: clock,
      storage: storage,
    );
  }

  /// The stored `next_attempt_at` of one operation, read from the row rather than from memory.
  Future<DateTime?> nextAttemptOf(int id) async {
    final snapshot = await queue.snapshot();
    for (final operation in [...snapshot.pending, ...snapshot.inFlight]) {
      if (operation.id == id) return operation.nextAttemptAt;
    }
    return null;
  }
}

/// A refusal in the platform's error contract.
ApiErrorResponse answer(int statusCode, String code) => ApiErrorResponse(
      statusCode: statusCode,
      code: code,
      message: 'The platform said so.',
      requestId: 'req-abc123',
    );
