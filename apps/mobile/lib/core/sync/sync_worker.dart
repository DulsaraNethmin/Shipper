import 'dart:async';
import 'dart:math';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/backoff.dart';
import 'package:shipper/core/sync/operation_sender.dart';
import 'package:shipper/core/sync/sync_alarm.dart';
import 'package:shipper/core/sync/sync_decision.dart';
import 'package:shipper/core/sync/sync_signals.dart';

/// Drains the durable queue into the platform (SHIP-125).
///
/// SHIP-124 built a queue that holds what the user recorded and never loses it. This is the half
/// that gets it off the handset: `Docs/07` §4's "syncing is the platform's problem" is a promise
/// somebody has to keep, and this is the somebody.
///
/// ## 1. What starts a drain, and why connectivity alone would not
///
/// | Trigger | Covers |
/// |---|---|
/// | [SyncTrigger.launch] | A relaunch after a crash or a battery change. `recover()` runs first |
/// | [SyncTrigger.session] | A credential appeared. Nothing can be sent before one |
/// | [SyncTrigger.recorded] | The driver just recorded something, most often while they still have signal |
/// | [SyncTrigger.resumed] | The phone came out of a pocket, which is where an outage most often ended unobserved |
/// | [SyncTrigger.scheduled] | The worker's own wake-up, at the earliest moment any operation may be tried again |
/// | [SyncTrigger.connectivity] | A hint from outside. Nothing supplies one today — see [SyncSignals] |
///
/// **The fifth row is the one that makes the other five safe to get wrong.** "Reconnection" on a
/// handset is not a reliable event: a device reports a network while sitting behind a captive
/// portal with no route to anything, and regains a route — a mast recovers, a carrier repairs
/// transit — without any event firing at all. A worker that drained *only* on a connectivity
/// event would wait forever for one that never came, with a driver's afternoon in the queue. So
/// the worker keeps a schedule of its own, and every other trigger is an optimisation on top of
/// it.
///
/// **It is a schedule and not a poll**, which is the difference between correct and expensive.
/// After each pass the worker asks the queue when the next operation could possibly be claimed —
/// the earliest stored `next_attempt_at` among the operations at the head of their ordering key —
/// and arms exactly one wake-up for that instant. An empty queue arms nothing at all, so a phone
/// with no pending work does no work; a queue with one operation in a five-minute backoff wakes
/// once in five minutes rather than sixty times.
///
/// ## 2. What happens to one attempt
///
/// One at a time, in the order `claim()` offers them, which is FIFO within an ordering key and
/// unordered between keys (SHIP-124). Sending two at once would break the first of those, and the
/// second is not a licence to: a driver's recorded sequence for one job reaches the platform in
/// the order they recorded it.
///
/// Each answer becomes a [SyncDecision] — accepted, retry, retry-and-stop-the-pass, refused, or
/// unsendable — and `sync_decision.dart` is where the table and its awkward cases live. Two
/// properties of this class matter alongside it:
///
/// - **The idempotency key is never touched.** It was minted where the user acted, stored in the
///   row, and is put on the wire by the sender on every attempt. Nothing here mints, rotates or
///   clears one — which is what makes a retry a retry rather than a second milestone.
/// - **A refusal is quarantined, never deleted.** `block(reason: refused)` keeps the operation,
///   its key and its attempt count where `Docs/02` §3.1's ladder can count it and SHIP-132 can
///   show it. There is no dead-letter path, for the reason SHIP-135 gave on the platform side.
///
/// ## 3. Sign-out
///
/// `Docs/07` §3 clears the queue at sign-out, and this is where it now happens — see
/// [syncWorkerProvider] for why it is here rather than inside `SessionController.signOut`.
class SyncWorker {
  SyncWorker({
    required this.queue,
    required this.sender,
    this.backoff = const Backoff(),
    SyncSignals? signals,
    SyncAlarm? alarm,
    DateTime Function()? now,
    double Function()? roll,
  })  : _alarm = alarm ?? TimerAlarm(),
        _now = now ?? DateTime.now,
        _roll = roll ?? Random().nextDouble {
    _signals = signals?.signals.listen(wake);
  }

  /// The queue being drained.
  ///
  /// Exposed because a screen needs the operations as well as the number of them: SHIP-132
  /// acknowledges a blocked one, which is the only way a quarantined operation ever leaves the
  /// table. Reaching it through the worker rather than through `operationQueueProvider` is what
  /// keeps a screen from holding a second queue over a second file.
  final OperationQueue queue;

  /// How an operation reaches the platform.
  final OperationSender sender;

  /// How long a failed operation waits.
  final Backoff backoff;

  final SyncAlarm _alarm;
  final DateTime Function() _now;
  final double Function() _roll;

  StreamSubscription<SyncTrigger>? _signals;
  final _snapshots = StreamController<QueueSnapshot>.broadcast();

  /// The queue as it stood at the end of the last pass.
  ///
  /// **The seam SHIP-126 consumes, and it costs nothing** — the worker already reads a snapshot
  /// after every pass to decide when to wake next, so publishing it is a `.add`. The indicator
  /// reads [QueueSnapshot.unsynced], which counts pending and in-flight work and excludes blocked
  /// work: an operation waiting for a person is not waiting for a connection, and a number that
  /// never falls however long the driver stands in the open is not what `Docs/02` §3.1 asks for.
  Stream<QueueSnapshot> get snapshots => _snapshots.stream;

  /// The last published snapshot, for a listener that arrives after a pass rather than before.
  QueueSnapshot? get lastSnapshot => _lastSnapshot;
  QueueSnapshot? _lastSnapshot;

  /// What started the most recent pass.
  ///
  /// Kept because "why did this drain run" is the first question asked of a worker that drained
  /// too often or not at all, and on a handset there is no server log to answer it from.
  SyncTrigger? get lastTrigger => _lastTrigger;
  SyncTrigger? _lastTrigger;

  bool _paused = false;
  bool _disposed = false;

  Future<void>? _inFlight;
  bool _again = false;

  /// Recovers anything a previous process left in flight, then drains.
  ///
  /// Called once, from `main`. `recover()` is the reason it exists as its own method rather than
  /// being one more trigger: an operation claimed by a process that died is sitting in
  /// `in_flight`, invisible to `claim()`, and this is the only moment anything looks. SHIP-124 is
  /// explicit that there is no lease and no timeout — a handset runs one app process, so the next
  /// launch is the only other party there is.
  ///
  /// The recovered operation keeps the key it was created with and its attempt count, so a send
  /// that reached the platform before the process died is replayed rather than duplicated.
  Future<void> start() async {
    if (_disposed) return;
    await queue.recover();
    await drain(SyncTrigger.launch);
  }

  /// Asks for a drain and does not wait for it. The form every trigger uses.
  void wake(SyncTrigger why) => unawaited(drain(why));

  /// The drain currently running, or an already-completed future when none is.
  ///
  /// For a caller that has to wait for a drain it did not start. Every trigger but [drain] itself
  /// is fire-and-forget, so without this the only way to observe one finishing is to wait a while
  /// and hope — which is how a test that proves nothing gets written.
  Future<void> get drained => _inFlight ?? Future<void>.value();

  /// Runs a drain, coalescing with one already in progress.
  ///
  /// Two passes at once would put two operations on the wire together, which is the one thing the
  /// per-key ordering guarantee cannot survive. A trigger that arrives mid-pass is not dropped
  /// either — it is remembered, and the pass is run again once the current one finishes, because
  /// the reason it fired (a resume, a fresh operation) may be exactly what changes the outcome.
  Future<void> drain(SyncTrigger why) {
    if (_disposed) return Future<void>.value();

    final running = _inFlight;
    if (running != null) {
      _again = true;
      return running;
    }

    final started = _runPasses(why);
    _inFlight = started;
    return started;
  }

  /// Stops draining. Nothing is lost; the queue simply stops being offered to the platform.
  void pause() {
    _paused = true;
    _alarm.disarm();
  }

  /// Starts draining again, and drains now.
  void resume(SyncTrigger why) {
    if (_disposed) return;
    _paused = false;
    wake(why);
  }

  /// Discards the whole queue, because the session it belonged to has ended (`Docs/07` §3).
  ///
  /// Returns how many operations were discarded, so that even the one bulk removal is something a
  /// person can be told about — the reason `OperationQueue.clear` reports a count at all.
  Future<int> forget() async {
    pause();
    if (_disposed) return 0;

    final discarded = await queue.clear();
    await _publish();
    return discarded;
  }

  /// Records an operation and asks for it to be sent.
  ///
  /// **The seam SHIP-129 consumes**, and the reason it is here rather than on the queue: an
  /// `enqueue` that is not followed by a drain is an operation that sits on a phone with a full
  /// bar of signal until something else happens to wake the worker. Making the two one call is
  /// what stops that being a rule somebody has to remember on every screen that records anything.
  ///
  /// The parameters and every refusal are `OperationQueue.enqueue`'s, unchanged — a screen still
  /// catches `QueueRefusal` and still writes its own copy for it.
  Future<QueuedOperation> record({
    required OperationKind kind,
    required String orderingKey,
    required String method,
    required String path,
    required Map<String, dynamic> body,
    required DateTime recordedAt,
    String? attachmentPath,
    String? idempotencyKey,
  }) async {
    final operation = await queue.enqueue(
      kind: kind,
      orderingKey: orderingKey,
      method: method,
      path: path,
      body: body,
      recordedAt: recordedAt,
      attachmentPath: attachmentPath,
      idempotencyKey: idempotencyKey,
    );

    wake(SyncTrigger.recorded);
    return operation;
  }

  Future<void> dispose() async {
    _disposed = true;
    _alarm.disarm();
    await _signals?.cancel();
    await _snapshots.close();
  }

  Future<void> _runPasses(SyncTrigger why) async {
    try {
      await _pass(why);
      while (_again && !_disposed) {
        _again = false;
        await _pass(why);
      }
    } finally {
      _again = false;
      _inFlight = null;
    }
  }

  /// One drain: claim, send, resolve, repeat until there is nothing claimable.
  ///
  /// It terminates for a reason worth stating, because a loop that sends is a loop that could not.
  /// Every outcome either removes the operation from the table or writes a `next_attempt_at` in
  /// the future, and `claim()` will not offer an operation before that time — so each pass offers
  /// the head of each ordering key at most once, and there are finitely many keys.
  Future<void> _pass(SyncTrigger why) async {
    if (_disposed) return;
    _alarm.disarm();
    _lastTrigger = why;

    // Set only when the pass stops early, and used in place of the queue's own answer about when
    // something is next claimable. After a [SyncDecision.retryAndPause] the operations this pass
    // never reached still have no stored delay, so asking the queue would answer "now" and the
    // wake-up would fire straight back into the same failure.
    DateTime? stoppedUntil;

    while (!_disposed && !_paused) {
      final operation = await queue.claim();
      if (operation == null) break;

      final attempt = await _attempt(operation);
      if (attempt.decision == SyncDecision.retryAndPause) {
        stoppedUntil = attempt.retryAt;
        break;
      }
    }

    if (_disposed) return;

    final snapshot = await _publish();
    if (_disposed || _paused || snapshot == null) return;

    _arm(stoppedUntil ?? _earliestClaimable(snapshot));
  }

  /// Sends one operation and records what the answer means.
  Future<({SyncDecision decision, DateTime? retryAt})> _attempt(QueuedOperation operation) async {
    SyncDecision decision;
    String? detail;

    try {
      await sender.send(operation);
      decision = SyncDecision.accepted;
    } on ApiFailure catch (failure) {
      decision = decideFrom(failure);
      // `ApiFailure.toString` carries the status, the machine-readable code and the request id,
      // and deliberately carries neither the platform's copy nor the response body — `Docs/07` §3
      // keeps tokens and payloads out of anything that can be read later, and this string is
      // stored on the device and shown to support.
      detail = failure.toString();
    } catch (error) {
      decision = SyncDecision.unsupported;
      detail = error.toString();
    }

    DateTime? retryAt;

    switch (decision) {
      case SyncDecision.accepted:
        await queue.complete(operation.id);

      case SyncDecision.retry:
      case SyncDecision.retryAndPause:
        // `operation.attempts` was incremented by the claim, so the first failure asks the policy
        // for the delay after one attempt. The instant, not the duration, is what is stored — a
        // duration would have to be added to something at read time, and the only clock available
        // then is a launch that may be days later.
        retryAt = _now().add(backoff.after(operation.attempts, roll: _roll()));
        await queue.release(operation.id, nextAttemptAt: retryAt);

      case SyncDecision.refused:
        await queue.block(operation.id, reason: QueueBlockReason.refused, detail: detail);

      case SyncDecision.unsupported:
        await queue.block(operation.id, reason: QueueBlockReason.unsupported, detail: detail);
    }

    return (decision: decision, retryAt: retryAt);
  }

  Future<QueueSnapshot?> _publish() async {
    if (_disposed) return null;

    final snapshot = await queue.snapshot();
    if (_disposed) return null;

    _lastSnapshot = snapshot;
    if (!_snapshots.isClosed) _snapshots.add(snapshot);
    return snapshot;
  }

  /// The earliest moment `claim()` could return something, or `null` if it never could.
  ///
  /// It mirrors the queue's own rule — only the oldest operation of each ordering key is a
  /// candidate — because anything less would arm a wake-up for an operation sitting behind a
  /// blocked one in its own key, which is a timer that fires forever and claims nothing.
  DateTime? _earliestClaimable(QueueSnapshot snapshot) {
    final byId = <int, ({String orderingKey, DateTime? at, bool claimable})>{};

    for (final operation in snapshot.pending) {
      byId[operation.id] = (
        orderingKey: operation.orderingKey,
        at: operation.nextAttemptAt,
        claimable: true,
      );
    }
    // In flight and blocked operations are not candidates themselves, and both **hold their own
    // key**: an operation behind one is not claimable however long it waits.
    for (final operation in snapshot.inFlight) {
      byId[operation.id] = (orderingKey: operation.orderingKey, at: null, claimable: false);
    }
    for (final operation in snapshot.blocked) {
      byId[operation.id] = (orderingKey: operation.orderingKey, at: null, claimable: false);
    }

    final ids = byId.keys.toList()..sort();
    final headed = <String>{};
    final now = _now();
    DateTime? earliest;

    for (final id in ids) {
      final row = byId[id]!;
      if (!headed.add(row.orderingKey)) continue; // Not the head of its key.
      if (!row.claimable) continue;

      final at = row.at ?? now;
      if (earliest == null || at.isBefore(earliest)) earliest = at;
    }

    return earliest;
  }

  void _arm(DateTime? at) {
    if (at == null) return; // Nothing to wait for. An idle phone arms no timer.

    var delay = at.difference(_now());
    if (delay <= Duration.zero) {
      // A pass that has just finished cannot leave something claimable now, so this is a clock
      // that moved or a state nothing anticipated. Waiting a base interval rather than firing
      // immediately is what stops either becoming a wake-up loop.
      delay = backoff.base;
    }

    _alarm.arm(delay, () => wake(SyncTrigger.scheduled));
  }
}

/// The application's sync worker.
///
/// ## Sign-out clears the queue, and it happens here rather than in `SessionController`
///
/// `Docs/07` §3 requires it, and SHIP-124 recorded why it was left unwired: there was no queue
/// anything wrote to, and calling it from `signOut` would have made every widget test that signs
/// out open a platform directory to clear a store that was always empty. **This ticket is what
/// changes that**, and not because a worker exists — because until now a leftover row was inert,
/// and from this commit it is a row something will pick up and send under whatever credential the
/// device holds next. That is the invariant `Docs/07` §3 is protecting, and it goes live with the
/// worker rather than with the first screen that queues anything.
///
/// It is wired through this listener rather than by calling the queue from `signOut` for three
/// reasons, in increasing order of importance:
///
/// - `core/auth` does not learn about `core/queue`, which is the same consumer-declares-the-seam
///   rule `auth_interceptor.dart` follows for the opposite direction.
/// - A widget test that signs out still touches nothing, because it does not build this provider.
///   SHIP-124's objection is answered rather than overruled.
/// - **It also covers the sign-out that did not finish.** A process killed between clearing the
///   keychain and clearing the queue leaves rows behind; the next launch resolves the session to
///   signed-out, this listener fires, and they go. An inline call in `signOut` would have run in
///   exactly the one case that had already happened.
///
/// A session that cannot be read is treated as no session by `SessionController`, and therefore
/// clears the queue too. That is the right direction even though it discards work: with no
/// refresh token there is no credential those operations could ever be sent under, and the
/// alternative is unsendable rows waiting for the next account on the device.
///
/// The worker is also **paused while there is no session**, which is what stops a cold start
/// spending an attempt — and a stored backoff — on a request that has no bearer token on it.
final syncWorkerProvider = Provider<SyncWorker>((ref) {
  final worker = SyncWorker(
    queue: ref.watch(operationQueueProvider),
    sender: ref.watch(operationSenderProvider),
    signals: ref.watch(syncSignalsProvider),
  );
  ref.onDispose(() => unawaited(worker.dispose()));

  ref.listen(
    sessionProvider,
    (previous, next) {
      switch (next) {
        case SessionSignedIn():
          worker.resume(SyncTrigger.session);
        case SessionSignedOut():
          unawaited(worker.forget());
        case SessionRestoring():
          worker.pause();
      }
    },
    fireImmediately: true,
  );

  return worker;
});
