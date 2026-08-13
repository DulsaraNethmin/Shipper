// SHIP-126's *Done when*: a persistent indicator shows how many updates are unsynced.
//
// Two claims, tested two ways. **How many** is arithmetic over a `QueueSnapshot` and is checked
// against snapshots built by hand, because the interesting sums are the ones a real queue rarely
// produces — in particular an operation that is in flight, which a published snapshot almost never
// carries and which the count must include anyway. **Persistent** is a journey through the running
// application: record with no signal, walk to another screen, and the answer is still there.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/pending_updates_indicator.dart';
import 'package:shipper/core/sync/queue_watch.dart';

/// One queued operation, in whichever state the caller is asking about.
QueuedOperation _operation(int id, QueueState state) => QueuedOperation(
      id: id,
      idempotencyKey: 'key-$id',
      kind: OperationKind.milestone,
      orderingKey: 'job:a',
      method: 'POST',
      path: '/v1/jobs/a/milestones',
      body: const <String, dynamic>{'milestone': 'picked_up'},
      recordedAt: DateTime.utc(2026, 8, 13, 9),
      enqueuedAt: DateTime.utc(2026, 8, 13, 9),
      state: state,
      attempts: 0,
    );

BlockedOperation _blocked(int id) => BlockedOperation(
      id: id,
      idempotencyKey: 'key-$id',
      kindName: 'delivery.milestone',
      orderingKey: 'job:a',
      recordedAt: DateTime.utc(2026, 8, 13, 9),
      enqueuedAt: DateTime.utc(2026, 8, 13, 9),
      attempts: 3,
      reason: QueueBlockReason.refused,
    );

/// A queue holding [pending] waiting operations, [inFlight] on the wire and [blocked] quarantined.
QueueSnapshot _snapshot({int pending = 0, int inFlight = 0, int blocked = 0}) {
  var id = 0;
  return QueueSnapshot(
    total: pending + inFlight + blocked,
    pending: List.generate(pending, (_) => _operation(++id, QueueState.pending)),
    inFlight: List.generate(inFlight, (_) => _operation(++id, QueueState.inFlight)),
    blocked: List.generate(blocked, (_) => _blocked(++id)),
  );
}

/// The indicator alone, over one snapshot.
Future<void> show(WidgetTester tester, QueueSnapshot? snapshot) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        if (snapshot != null)
          queueSnapshotProvider.overrideWith((ref) => Stream<QueueSnapshot>.value(snapshot)),
      ],
      child: const MaterialApp(
        home: Scaffold(body: PendingUpdatesIndicator()),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  group('how many updates are unsynced', () {
    testWidgets('counts work waiting and work on the wire, not one of the two', (tester) async {
      // **The assertion this file exists for.** `QueueSnapshot.unsynced` is pending *plus* in
      // flight, and SHIP-125 chose that sum deliberately: an operation the worker has claimed and
      // not yet resolved is work the platform does not have, and a count that skipped it would
      // under-report at exactly the moment the driver is watching. A published snapshot almost
      // never carries one — the worker publishes at the end of a pass, by which time each operation
      // has been resolved — so nothing but this test holds the second term.
      await show(tester, _snapshot(pending: 1, inFlight: 1));

      expect(find.text('2 updates waiting to sync'), findsOneWidget);
    });

    testWidgets('says one update, not 1 updates', (tester) async {
      await show(tester, _snapshot(pending: 1));
      expect(find.text('1 update waiting to sync'), findsOneWidget);
    });

    testWidgets('counts every job, because the queue is the device’s and not one delivery’s',
        (tester) async {
      await show(tester, _snapshot(pending: 3));
      expect(find.text('3 updates waiting to sync'), findsOneWidget);
    });
  });

  group('quarantined work', () {
    testWidgets('is a second line in different words, not part of the count', (tester) async {
      await show(tester, _snapshot(pending: 2, blocked: 1));

      // Two numbers rather than three-of-which-one-is-stuck. SHIP-125's reason for excluding
      // blocked work from `unsynced` is that it is not waiting for a connection — so a driver who
      // stands in the open should watch the first number fall to zero and the second stay put,
      // which is the truth and is what tells them the second needs something else.
      expect(find.text('2 updates waiting to sync'), findsOneWidget);
      expect(find.text('1 update needs attention'), findsOneWidget);
    });

    testWidgets('is shown even when nothing is waiting', (tester) async {
      // The hole that "count only what is waiting for a connection" would otherwise leave: a device
      // with one refused update and nothing pending would show no indicator at all, and Docs/02
      // §3.1's requirement is that the user is never left guessing.
      await show(tester, _snapshot(blocked: 1));

      expect(find.byKey(const Key('pending-updates')), findsOneWidget);
      expect(find.text('1 update needs attention'), findsOneWidget);
      expect(find.byKey(const Key('pending-updates-count')), findsNothing);
    });

    testWidgets('agrees with itself in the plural', (tester) async {
      await show(tester, _snapshot(blocked: 2));
      expect(find.text('2 updates need attention'), findsOneWidget);
    });
  });

  group('when there is nothing to say', () {
    testWidgets('an empty queue draws nothing at all', (tester) async {
      // Not "0 updates waiting". A row that is always there and usually says zero is a row people
      // stop reading, and this escalation is entirely about the case where the number is not zero.
      await show(tester, _snapshot());
      expect(find.byKey(const Key('pending-updates')), findsNothing);
    });

    testWidgets('an application with no worker draws nothing and opens no database',
        (tester) async {
      // `queueWatchProvider` is null by default, which is what lets this widget live inside
      // `ShipperApp` without every widget test in the suite reaching for a Drift file in the
      // platform's application-support directory (SHIP-124's objection, kept).
      await show(tester, null);
      expect(find.byKey(const Key('pending-updates')), findsNothing);
    });
  });
}
