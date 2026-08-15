// SHIP-127's *Done when*: the provider is prompted when an update has been pending four hours.
//
// Three claims, tested three ways.
//
// **Four hours** is arithmetic over a `QueueSnapshot` and is checked directly against `NudgePolicy`,
// which is a pure function of a snapshot and a clock. The boundary is the assertion that matters: a
// threshold of zero and a threshold of a week both make a widget appear on *some* input, and only a
// test that pins 3h59m against 4h00m tells them apart.
//
// **Prompted** is a widget test — a card over a scrim that the driver has to acknowledge, not a
// fourth line in a bar they have already been reading past for four hours.
//
// **Persistent across the application** is the running app: the nudge is mounted above the router,
// so it is not a property of whichever screen happened to be open.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/core/sync/queue_watch.dart';
import 'package:shipper/core/sync/unsynced_nudge.dart';

import '../../features/delivery/delivery_app.dart';
import 'sync_fixture.dart';

/// The moment every clock in this file is read against.
final _now = DateTime.utc(2026, 8, 13, 17);

/// How far ahead of the row's own age its `recorded_at` is set, in every fixture below.
///
/// The two columns are deliberately **not** equal. `Docs/02` §3.1 keeps them distinct — when the
/// actor acted, and when the row was committed — and SHIP-124 says in as many words that
/// `enqueued_at` is "what `Docs/02` §3.1's four-hour and 24-hour escalations measure". A fixture
/// that set both to the same instant would let a reading of the wrong column pass every test here.
const _actedEarlier = Duration(hours: 1);

QueuedOperation _operation(int id, QueueState state, {required Duration waited}) => QueuedOperation(
      id: id,
      idempotencyKey: 'key-$id',
      kind: OperationKind.milestone,
      orderingKey: 'job:a',
      method: 'POST',
      path: '/v1/jobs/a/milestones',
      body: const <String, dynamic>{'milestone': 'picked_up'},
      recordedAt: _now.subtract(waited + _actedEarlier),
      // The column the escalation ladder measures, and the only one that moves in this file.
      enqueuedAt: _now.subtract(waited),
      state: state,
      attempts: 0,
    );

BlockedOperation _blocked(int id, {required Duration waited}) => BlockedOperation(
      id: id,
      idempotencyKey: 'key-$id',
      kindName: 'delivery.milestone',
      orderingKey: 'job:a',
      recordedAt: _now.subtract(waited + _actedEarlier),
      enqueuedAt: _now.subtract(waited),
      attempts: 3,
      reason: QueueBlockReason.refused,
    );

/// A queue whose operations have each been waiting the given time.
QueueSnapshot _snapshot({
  List<Duration> pending = const <Duration>[],
  List<Duration> inFlight = const <Duration>[],
  List<Duration> blocked = const <Duration>[],
}) {
  var id = 0;
  return QueueSnapshot(
    total: pending.length + inFlight.length + blocked.length,
    pending: [for (final w in pending) _operation(++id, QueueState.pending, waited: w)],
    inFlight: [for (final w in inFlight) _operation(++id, QueueState.inFlight, waited: w)],
    blocked: [for (final w in blocked) _blocked(++id, waited: w)],
  );
}

/// The nudge over one snapshot, wrapped round a screen it must not disturb.
Future<void> show(
  WidgetTester tester,
  QueueSnapshot? snapshot, {
  NudgePolicy policy = const NudgePolicy(),
  DateTime? now,
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        nudgePolicyProvider.overrideWithValue(policy),
        nudgeClockProvider.overrideWithValue(() => now ?? _now),
        if (snapshot != null)
          queueSnapshotProvider.overrideWith((ref) => Stream<QueueSnapshot>.value(snapshot)),
      ],
      child: const MaterialApp(
        home: UnsyncedNudge(
          child: Scaffold(body: Center(child: Text('the application', key: Key('underneath')))),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

void main() {
  const policy = NudgePolicy();

  group('four hours, and the boundary is the assertion', () {
    test('three hours fifty-nine is not yet', () {
      final decision = policy.decide(
        _snapshot(pending: const [Duration(hours: 3, minutes: 59)]),
        _now,
      );

      expect(decision.due, isFalse);
      expect(decision.waiting, 1);
    });

    test('four hours exactly is', () {
      // `Docs/02` §3.1's row reads "4 hours", so the rung is reached at four rather than passed at
      // four. Inclusive on purpose, and this is the test that says which way it was read.
      final decision = policy.decide(_snapshot(pending: const [Duration(hours: 4)]), _now);

      expect(decision.due, isTrue);
      expect(decision.oldestFor, const Duration(hours: 4));
    });

    test('a threshold that is not four hours is a different answer at four hours', () {
      // The mutation guard. A policy of zero fires on work recorded a second ago; a policy of a
      // week never fires at all. Both of those pass every other test in this file.
      final snapshot = _snapshot(pending: const [Duration(hours: 4)]);

      expect(const NudgePolicy(after: Duration.zero).decide(snapshot, _now).due, isTrue);
      expect(const NudgePolicy(after: Duration(days: 7)).decide(snapshot, _now).due, isFalse);
      expect(
        const NudgePolicy(after: Duration.zero)
            .decide(_snapshot(pending: const [Duration(seconds: 1)]), _now)
            .due,
        isTrue,
        reason: 'a zero threshold nudges about work the driver recorded a second ago, which is what '
            'makes the four in Docs/02 §3.1 worth pinning',
      );
    });
  });

  group('what the age is measured from', () {
    test('the oldest unsynced operation, not the newest and not the count', () {
      final decision = policy.decide(
        _snapshot(pending: const [Duration(minutes: 1), Duration(hours: 9), Duration(hours: 2)]),
        _now,
      );

      expect(decision.due, isTrue);
      expect(decision.oldestFor, const Duration(hours: 9));
      expect(decision.waiting, 3);
    });

    test('work on the wire counts, exactly as SHIP-126 counts it', () {
      // An operation the worker has claimed and not yet resolved is work the platform does not
      // have. A published snapshot almost never carries one, so nothing but this holds the term.
      final decision = policy.decide(_snapshot(inFlight: const [Duration(hours: 5)]), _now);

      expect(decision.due, isTrue);
      expect(decision.waiting, 1);
    });

    test('quarantined work does not, however old it is', () {
      // The whole content of this prompt is "go and find signal". An operation the platform
      // refused will still be refused at the top of the hill, so nudging about it sends a driver
      // out of their way for nothing. SHIP-132 is the screen for those, and SHIP-126's second line
      // already says one exists.
      final decision = policy.decide(_snapshot(blocked: const [Duration(days: 3)]), _now);

      expect(decision.due, isFalse);
      expect(decision.waiting, 0);
    });

    test('an empty queue and an absent queue are both quiet', () {
      expect(policy.decide(_snapshot(), _now).due, isFalse);
      expect(policy.decide(null, _now), NudgeDecision.quiet);
    });

    test('a clock that has moved backwards reads as no time at all, not as negative', () {
      // `enqueuedAt` is written from the device's own clock and a person can move that clock. The
      // arithmetic has to survive it; clamping is what stops a negative age comparing oddly.
      final decision = policy.decide(
        _snapshot(pending: const [Duration(hours: -6)]),
        _now,
      );

      expect(decision.oldestFor, Duration.zero);
      expect(decision.due, isFalse);
    });
  });

  group('dismissing buys one quiet interval and then re-asks', () {
    test('a dismissal holds for exactly one threshold and no longer', () {
      final until = policy.silencedUntil(_now);
      expect(until, _now.add(const Duration(hours: 4)));

      final snapshot = _snapshot(pending: const [Duration(hours: 4)]);

      expect(policy.decide(snapshot, _now, silencedUntil: until).due, isFalse);
      expect(
        policy
            .decide(snapshot, _now.add(const Duration(hours: 3, minutes: 59)), silencedUntil: until)
            .due,
        isFalse,
      );
      expect(
        policy.decide(snapshot, until, silencedUntil: until).due,
        isTrue,
        reason: 'Docs/02 §3.1 makes the 24-hour operations alert a backstop rather than the first '
            'anybody hears; a dismissal that silenced forever would make it the first',
      );
    });

    test('and it is bought in time rather than in queue age, so cleared work cannot extend it', () {
      // The case an age-based silence gets wrong on an ordinary working day: dismiss at four hours,
      // that operation syncs, and the *next* backlog would inherit quiet it did nothing to earn.
      final until = policy.silencedUntil(_now);
      final later = _now.add(const Duration(hours: 5));

      expect(
        policy.decide(_snapshot(pending: const [Duration(hours: 4)]), later, silencedUntil: until)
            .due,
        isTrue,
      );
    });
  });

  group('the words', () {
    test('whole units, singular and plural', () {
      expect(waitedInWords(const Duration(hours: 4)), '4 hours');
      expect(waitedInWords(const Duration(hours: 1, minutes: 59)), '1 hour');
      expect(waitedInWords(const Duration(hours: 26)), '26 hours');
      expect(waitedInWords(const Duration(hours: 48)), '2 days');
      expect(waitedInWords(const Duration(days: 9)), '9 days');
    });
  });

  group('being prompted', () {
    testWidgets('an interruption over the application, not a line inside it', (tester) async {
      await show(tester, _snapshot(pending: const [Duration(hours: 4)]));

      expect(find.byKey(const Key('unsynced-nudge')), findsOneWidget);
      expect(
        find.byKey(const Key('unsynced-nudge-scrim')),
        findsOneWidget,
        reason: 'a card with the application live and tappable underneath it is the floating badge '
            'SHIP-126 rejected, sitting on whatever button happens to be behind it',
      );
      expect(find.byKey(const Key('underneath')), findsOneWidget, reason: 'still there, under it');
    });

    testWidgets('says how long, and how many, in words a driver reads', (tester) async {
      await show(
        tester,
        _snapshot(pending: const [Duration(hours: 6), Duration(hours: 1)]),
      );

      expect(
        find.text('2 updates you recorded have been waiting on this phone, the oldest for 6 hours.'),
        findsOneWidget,
      );
    });

    testWidgets('says one update, not 1 updates', (tester) async {
      await show(tester, _snapshot(pending: const [Duration(hours: 4)]));

      expect(
        find.text('An update you recorded has been waiting on this phone for 4 hours.'),
        findsOneWidget,
      );
    });

    testWidgets('offers no retry, because the worker already does that', (tester) async {
      // A button that tries and fails in front of the driver reads as the work being lost. The
      // action Docs/02 §3.1 actually asks for is physical, and the copy is where it is asked for.
      await show(tester, _snapshot(pending: const [Duration(hours: 4)]));

      expect(find.widgetWithText(FilledButton, 'Got it'), findsOneWidget);
      expect(find.textContaining('Try again'), findsNothing);
      expect(find.textContaining('coverage'), findsOneWidget);
    });

    testWidgets('nothing at three hours', (tester) async {
      await show(tester, _snapshot(pending: const [Duration(hours: 3)]));

      expect(find.byKey(const Key('unsynced-nudge')), findsNothing);
      expect(find.byKey(const Key('underneath')), findsOneWidget);
    });

    testWidgets('an application with no worker draws its child and opens no database',
        (tester) async {
      // `queueWatchProvider` is null by default, which is what lets this widget wrap `ShipperApp`
      // without every widget test in the suite reaching for a Drift file in the platform's
      // application-support directory (SHIP-124's objection, kept).
      await show(tester, null);

      expect(find.byKey(const Key('unsynced-nudge')), findsNothing);
      expect(find.byKey(const Key('underneath')), findsOneWidget);
    });

    testWidgets('the scrim does not dismiss it; only the button does', (tester) async {
      // An accidental dismissal buys four more hours of silence, so it must be deliberate.
      await show(tester, _snapshot(pending: const [Duration(hours: 4)]));

      await tester.tapAt(const Offset(10, 10));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('unsynced-nudge')), findsOneWidget);

      await tester.tap(find.byKey(const Key('unsynced-nudge-dismiss')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('unsynced-nudge')), findsNothing);
    });

    testWidgets('and it asks again one threshold later, on the next pass of the worker',
        (tester) async {
      // **Re-evaluation is a published snapshot and nothing else** — no timer, by decision, because
      // a prompt nobody is looking at is not a prompt and every trigger that brings a person back
      // to the app already drains the queue. So the test pushes snapshots, which is what the worker
      // does at the end of every pass.
      final passes = StreamController<QueueSnapshot>.broadcast();
      addTearDown(passes.close);

      var clock = _now;
      await tester.pumpWidget(
        ProviderScope(
          overrides: [
            nudgeClockProvider.overrideWithValue(() => clock),
            queueSnapshotProvider.overrideWith((ref) => passes.stream),
          ],
          child: const MaterialApp(
            home: UnsyncedNudge(child: Scaffold(body: SizedBox.shrink())),
          ),
        ),
      );

      // One drain finishing, with the same operation still in the queue. The snapshot is built
      // fresh each time because that is what the worker publishes, and an identical instance would
      // not rebuild anything.
      Future<void> pass() async {
        passes.add(_snapshot(pending: const [Duration(hours: 4)]));
        await tester.pumpAndSettle();
      }

      await pass();
      expect(find.byKey(const Key('unsynced-nudge')), findsOneWidget);

      await tester.tap(find.byKey(const Key('unsynced-nudge-dismiss')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('unsynced-nudge')), findsNothing);

      // Three more hours: seven in total, and still inside the dismissal.
      clock = _now.add(const Duration(hours: 3));
      await pass();
      expect(find.byKey(const Key('unsynced-nudge')), findsNothing);

      // Four, and it asks again — about an update now eight hours old.
      clock = _now.add(const Duration(hours: 4));
      await pass();
      expect(find.byKey(const Key('unsynced-nudge')), findsOneWidget);
      expect(find.textContaining('8 hours'), findsOneWidget);
    });
  });

  group('in the running application', () {
    const job = '0198f2c1-1c9c-7b3d-9a2e-4a1f3c5d7e90';

    testWidgets('it is above the router, so walking away does not answer it', (tester) async {
      // The whole app: the real router, the real queue over a real file, the real worker. The
      // queue's clock is five hours behind the one the nudge reads, which is what a milestone
      // recorded before breakfast and still unsent at lunchtime looks like from inside the app.
      final recorded = DateTime.utc(2026, 8, 13, 9);
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: const ApiUnreachable()),
        clock: () => recorded,
      );

      await openDelivery(
        tester,
        harness: harness,
        jobId: job,
        nudgeClock: () => recorded.add(const Duration(hours: 5)),
      );
      expect(find.byKey(const Key('unsynced-nudge')), findsNothing, reason: 'nothing recorded yet');

      await recordMilestone(tester, harness, 'picked_up');

      expect(find.byKey(const Key('unsynced-nudge')), findsOneWidget);
      expect(find.textContaining('5 hours'), findsOneWidget);

      // Off to the shell — a different route, a different screen, a different feature.
      await followLink(tester, Routes.home);

      expect(
        find.byKey(const Key('unsynced-nudge')),
        findsOneWidget,
        reason: 'a prompt mounted inside a screen is one the driver dismisses by navigating, which '
            'is not an answer to Docs/02 §3.1 — the update is still on the phone',
      );
    });
  });
}
