// SHIP-125 — the wiring, which is where the sign-out clause actually lives.
//
// `sync_worker_test.dart` proves `forget()` empties the queue. That is not the claim `Docs/07` §3
// makes: it says the queue is cleared **at sign-out**, and a method nobody calls satisfies no part
// of that. So this drives the real providers — the session, the worker, the queue — and signs out.
//
// It also covers the pause, which is what stops a cold start spending an attempt and a stored
// backoff on a request with no bearer token on it. The token store here is **gated** rather than
// immediate, because the window this is about — `SessionState.restoring` — closes on the first
// `await` otherwise, and a test that races it proves whichever side it happened to win.

import 'dart:async';
import 'dart:io';

import 'package:drift/native.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_ender.dart';
import 'package:shipper/core/auth/session_refresher.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queue_database.dart';
import 'package:shipper/core/sync/operation_sender.dart';
import 'package:shipper/core/sync/queue_watch.dart';
import 'package:shipper/core/sync/sync_signals.dart';
import 'package:shipper/core/sync/sync_worker.dart';

import '../auth/session_fixtures.dart';
import '../queue/queue_fixture.dart';
import 'sync_fixture.dart';

/// A [TokenStore] whose read is held open, so the cold start can be observed mid-flight.
class GatedTokenStore implements TokenStore {
  GatedTokenStore(this.refreshToken);

  String? refreshToken;
  final gate = Completer<void>();
  var cleared = false;

  /// Lets the keychain answer.
  void answer() => gate.complete();

  @override
  Future<String?> readRefreshToken() async {
    await gate.future;
    return refreshToken;
  }

  @override
  Future<void> writeRefreshToken(String token) async => refreshToken = token;

  @override
  Future<void> clear() async {
    cleared = true;
    refreshToken = null;
  }
}

void main() {
  ({ProviderContainer container, GatedTokenStore tokens, ScriptedSender sender}) app({
    String? refreshToken = 'refresh-1',
  }) {
    final files = QueueFixture.temporary();
    final database = QueueDatabase(NativeDatabase(files.file));
    addTearDown(database.close);

    final tokens = GatedTokenStore(refreshToken);
    // Nothing must actually be accepted: an operation the platform takes is removed, and a queue
    // emptied by success would make "sign-out cleared it" pass against a worker that did nothing.
    final sender = ScriptedSender(thereafter: const ApiUnreachable());

    final container = ProviderContainer(
      overrides: [
        queueDatabaseProvider.overrideWithValue(database),
        operationSenderProvider.overrideWithValue(sender),
        syncSignalsProvider.overrideWithValue(TestSignals()),
        tokenStoreProvider.overrideWithValue(tokens),
        sessionRefresherProvider.overrideWithValue(FakeSessionRefresher()),
        sessionEnderProvider.overrideWithValue(FakeSessionEnder()),
      ],
    );
    addTearDown(container.dispose);

    return (container: container, tokens: tokens, sender: sender);
  }

  test('nothing is sent while the keychain has not answered yet', () async {
    // The worker starts paused because a cold start begins in `SessionState.restoring`, and a
    // request made then carries no bearer token at all — which the platform answers `401` for a
    // reason no amount of retrying fixes.
    final harness = app();
    final worker = harness.container.read(syncWorkerProvider);

    await enqueueMilestone(worker.queue);
    await worker.drain(SyncTrigger.launch);

    expect(harness.sender.sent, isEmpty);
    expect(await worker.queue.count(), 1, reason: 'held, not lost');
  });

  test('a session appearing starts the drain by itself', () async {
    final harness = app();
    final worker = harness.container.read(syncWorkerProvider);
    await enqueueMilestone(worker.queue);

    harness.tokens.answer();
    await harness.container.read(sessionProvider.notifier).restored;
    await pumpEventQueue();
    await worker.drained;

    expect(harness.sender.sent, hasLength(1));
    expect(worker.lastTrigger, SyncTrigger.session);
  });

  test('signing out discards the queue, because it belonged to that session', () async {
    // The invariant is not new; what is new is that it can be violated. Until this ticket a
    // leftover row was inert. From here it is a row the worker will pick up and send under
    // whatever credential the device holds next, which is the other account's.
    final harness = app();
    final worker = harness.container.read(syncWorkerProvider);
    harness.tokens.answer();
    await harness.container.read(sessionProvider.notifier).restored;

    await enqueueMilestone(worker.queue);
    await worker.drain(SyncTrigger.session);
    expect(harness.sender.sent, hasLength(1), reason: 'tried, and the link was dead');
    expect(await worker.queue.count(), 1);

    await harness.container.read(sessionProvider.notifier).signOut();
    await pumpEventQueue();

    expect(await worker.queue.count(), 0);
    expect(harness.tokens.cleared, isTrue, reason: "and the keychain, which is signOut's own half");
  });

  test('a cold start with no session clears whatever a killed sign-out left behind', () async {
    // An inline call inside `signOut` would have run in exactly the one case that had already
    // happened — the process that died between clearing the keychain and clearing the queue.
    // Listening to the state covers it, because the next launch resolves to signed out.
    final harness = app(refreshToken: null);
    final worker = harness.container.read(syncWorkerProvider);

    await enqueueMilestone(worker.queue);
    expect(await worker.queue.count(), 1);

    harness.tokens.answer();
    await harness.container.read(sessionProvider.notifier).restored;
    await pumpEventQueue();

    expect(await worker.queue.count(), 0);
    expect(harness.sender.sent, isEmpty, reason: 'and nothing was sent without a credential');
  });

  test('the worker and the queue are one, so nothing opens a second database', () async {
    // Two `OperationQueue` instances over two files are two queues, and the operations in the one
    // nothing drains would never be sent. `SyncWorker.queue` is how a screen reaches the queue.
    final harness = app();

    expect(
      harness.container.read(syncWorkerProvider).queue,
      same(harness.container.read(operationQueueProvider)),
    );
  });

  test('nothing watches the queue until something supplies the worker (SHIP-126)', () {
    // `queueWatchProvider` is empty by default so that `PendingUpdatesIndicator` — which lives
    // inside `ShipperApp`, and therefore inside every widget test in this suite — opens no
    // database. That default is what makes the widget safe to mount there, and it is also the way
    // it can be silently wrong in production, so both halves are held: the default is null here,
    // and `main.dart` supplies it, below.
    final container = ProviderContainer();
    addTearDown(container.dispose);

    expect(container.read(queueWatchProvider), isNull);
  });

  test('main.dart supplies it, which is the whole of the production wiring (SHIP-126)', () {
    // A source assertion rather than a call, because `main()` calls `runApp` and constructs the
    // real queue in the platform's application-support directory — the two things a host test
    // cannot do. What can be checked is that the override is there at all, and its absence is a
    // build that runs with an indicator which never appears however full the queue gets: no test
    // fails, nothing is logged, and the failure is a driver being left to guess.
    final source = File('lib/main.dart').readAsStringSync();

    expect(
      source,
      contains('queueWatchProvider.overrideWith'),
      reason: 'lib/main.dart no longer supplies queueWatchProvider. The pending-updates indicator '
          'reads it and would draw nothing at all — see Docs/02 §3.1 and the note on '
          'queue_watch.dart.',
    );
  });
}
