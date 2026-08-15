// SHIP-132's *Done when*: "The user is shown clearly when a queued update lost to server state."
//
// # Four things this file is careful about, each a way the panel could be quietly wrong
//
// **Reachable.** A panel nobody can open is indistinguishable from one that does not exist, and the
// only way in is the second line of a bar mounted beside the navigator. Every test here opens it by
// tapping that line rather than by pumping the panel, which is what SHIP-98 established after a
// route reachable only by a button looked, from the outside, exactly like a button that did nothing.
//
// **Clearly.** The words are asserted, not the widget. `BlockedOperation.detail` is a `toString` of
// an `ApiFailure` — a status, a code and a request id — and `queued_operation.dart` forbids showing
// it: "never shown as user-facing copy". A panel that rendered the detail would look informative and
// would say `ApiErrorResponse(409, conflict, r-9f2)` to somebody standing on a loading dock.
//
// **Which reason.** *Lost to server state* is one of three quarantine reasons and the other two are
// statements about this build rather than about a decision anybody made. Telling a driver their
// update was rejected when in fact the app cannot read its own database would send them to argue
// with an administrator about nothing.
//
// **Retained, not discarded.** `Docs/02` §3.1 requires the update to be kept and shown. So opening
// the panel removes nothing, closing it removes nothing, and only the row's own button does — which
// is asserted in all three directions, because the version that acknowledges on open is the one
// that reads as tidy.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/blocked_updates.dart';
import 'package:shipper/core/sync/pending_updates_indicator.dart';
import 'package:shipper/core/sync/queue_watch.dart';
import 'package:shipper/shared/formatting/dates.dart';

BlockedOperation _blocked(
  int id, {
  QueueBlockReason reason = QueueBlockReason.refused,
  String kindName = 'delivery.milestone',
  String? detail = 'ApiErrorResponse(409, conflict, request 0198f2c1-6b40-7a11)',
}) =>
    BlockedOperation(
      id: id,
      idempotencyKey: 'key-$id',
      kindName: kindName,
      orderingKey: 'job:a',
      recordedAt: DateTime.utc(2026, 8, 13, 9, 30),
      enqueuedAt: DateTime.utc(2026, 8, 13, 9, 31),
      attempts: 3,
      reason: reason,
      detail: detail,
    );

QueueSnapshot _snapshot(List<BlockedOperation> blocked) => QueueSnapshot(
      total: blocked.length,
      pending: const <QueuedOperation>[],
      inFlight: const <QueuedOperation>[],
      blocked: blocked,
    );

/// The indicator and the panel over one snapshot, arranged the way `ShipperApp` arranges them.
///
/// The panel wraps and the bar sits under the content, which is the layout `core/app.dart` builds —
/// so a change that moved the panel outside the tree the bar can reach would fail here rather than
/// only on a device.
Future<void> show(
  WidgetTester tester,
  QueueSnapshot snapshot, {
  ProviderContainer? container,
}) async {
  await tester.pumpWidget(
    ProviderScope(
      overrides: [
        queueSnapshotProvider.overrideWith((ref) => Stream<QueueSnapshot>.value(snapshot)),
      ],
      child: MaterialApp(
        home: Scaffold(
          body: BlockedUpdates(
            child: Column(
              children: const <Widget>[
                Expanded(child: SizedBox.shrink()),
                PendingUpdatesIndicator(),
              ],
            ),
          ),
        ),
      ),
    ),
  );
  await tester.pumpAndSettle();
}

/// Opens the panel the way a person does.
Future<void> openPanel(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('pending-updates-blocked-open')));
  await tester.pumpAndSettle();
}

void main() {
  group('getting there', () {
    testWidgets('the bar says how many need attention and nothing else until it is tapped', (
      tester,
    ) async {
      await show(tester, _snapshot([_blocked(1)]));

      expect(find.text('1 update needs attention'), findsOneWidget);
      expect(find.byKey(const Key('blocked-updates')), findsNothing);
    });

    testWidgets('tapping that line opens the panel', (tester) async {
      await show(tester, _snapshot([_blocked(1), _blocked(2)]));
      await openPanel(tester);

      expect(find.byKey(const Key('blocked-updates')), findsOneWidget);
      expect(
        tester.widget<Text>(find.byKey(const Key('blocked-updates-headline'))).data,
        '2 updates need your attention',
      );
    });
  });

  group('what it says about an update the platform refused', () {
    testWidgets('it names what the update was, when it was recorded, and what happened', (
      tester,
    ) async {
      await show(tester, _snapshot([_blocked(1)]));
      await openPanel(tester);

      expect(
        tester.widget<Text>(find.byKey(const Key('blocked-update-name-1'))).data,
        'A delivery update',
      );
      // Day-first through the shared formatter, and **`recorded_at` rather than `enqueued_at`** —
      // what the person is being reminded of is when they acted, which is the first of the two
      // timestamps Docs/02 §3.1 keeps. The fixture sets the two a minute apart precisely so a
      // reading of the wrong column fails here; asserting against the formatter rather than
      // against a literal keeps the test honest on a machine in any timezone.
      expect(
        tester.widget<Text>(find.byKey(const Key('blocked-update-recorded-1'))).data,
        'You recorded this on ${dayFirstDateTime('2026-08-13T09:30:00.000Z')}.',
      );

      final reason = tester.widget<Text>(find.byKey(const Key('blocked-update-reason-1'))).data!;
      // **The *Done when*, in words.** It lost to server state, and the sentence says both halves:
      // something changed on the delivery, and what the platform holds is what counts.
      expect(reason, contains('would not accept'));
      expect(reason, contains('Something changed on the delivery'));
    });

    testWidgets('and never shows the support detail', (tester) async {
      // `queued_operation.dart`: the detail is "free text for a support conversation. Never shown
      // as user-facing copy — a screen writes its own words for the reason". It is an ApiFailure's
      // toString, carrying a status, a code and a request id.
      await show(tester, _snapshot([_blocked(1)]));
      await openPanel(tester);

      for (final text in tester.widgetList<Text>(find.byType(Text))) {
        final rendered = text.data ?? '';
        expect(rendered, isNot(contains('ApiErrorResponse')));
        expect(rendered, isNot(contains('0198f2c1')));
        expect(rendered, isNot(contains('409')));
      }
    });

    testWidgets('it says the update is still on the phone until the person removes it', (
      tester,
    ) async {
      // The sentence the whole panel exists to make true. Docs/02 §3.1 and Docs/07 §4 both require
      // a queued update that lost to be retained and shown rather than discarded.
      await show(tester, _snapshot([_blocked(1)]));
      await openPanel(tester);

      final intro = tester.widget<Text>(find.byKey(const Key('blocked-updates-intro'))).data!;
      expect(intro, contains('still on this phone'));
      expect(intro, contains('Waiting will not send them'));
    });
  });

  group('the three reasons are three different sentences', () {
    testWidgets('an update this build cannot send is not reported as a refusal', (tester) async {
      // The distinction a tidier implementation would collapse. "Lost to server state" is about a
      // decision somebody or something made; this one is about the app being out of date, and
      // sending a driver to argue with an administrator about it would be worse than saying
      // nothing.
      await show(tester, _snapshot([_blocked(1, reason: QueueBlockReason.unsupported)]));
      await openPanel(tester);

      final reason = tester.widget<Text>(find.byKey(const Key('blocked-update-reason-1'))).data!;
      expect(reason, contains('cannot send it'));
      expect(reason, contains('updating the app'));
      expect(reason, isNot(contains('would not accept')));
    });

    testWidgets('a row this build cannot read says so, and offers nothing to do', (tester) async {
      await show(tester, _snapshot([_blocked(1, reason: QueueBlockReason.unreadable)]));
      await openPanel(tester);

      final reason = tester.widget<Text>(find.byKey(const Key('blocked-update-reason-1'))).data!;
      expect(reason, contains('could not be read'));
      expect(reason, contains('support'));
    });

    testWidgets('a kind this build has never heard of is named honestly', (tester) async {
      // A row written by a later build. Rendering `delivery.something` would put a wire value in
      // front of a driver; saying what is true is better.
      await show(tester, _snapshot([_blocked(1, kindName: 'delivery.something-newer')]));
      await openPanel(tester);

      expect(
        tester.widget<Text>(find.byKey(const Key('blocked-update-name-1'))).data,
        contains('does not recognise'),
      );
      expect(
        tester.widget<Text>(find.byKey(const Key('blocked-update-name-1'))).data,
        isNot(contains('delivery.something-newer')),
      );
    });
  });

  group('nothing is removed except by the button that says it removes it', () {
    testWidgets('opening the panel acknowledges nothing', (tester) async {
      // The version of this that reads as tidy and is a silent drop: "they have seen it now".
      final snapshot = _snapshot([_blocked(1)]);
      await show(tester, snapshot);
      await openPanel(tester);

      expect(find.byKey(const Key('blocked-update-1')), findsOneWidget);
      expect(find.text('1 update needs attention'), findsOneWidget);
    });

    testWidgets('closing it leaves every update where it was', (tester) async {
      await show(tester, _snapshot([_blocked(1)]));
      await openPanel(tester);
      await tester.tap(find.byKey(const Key('blocked-updates-close')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('blocked-updates')), findsNothing);
      expect(find.text('1 update needs attention'), findsOneWidget);
    });

    testWidgets('the scrim closes it too, and removes nothing', (tester) async {
      await show(tester, _snapshot([_blocked(1)]));
      await openPanel(tester);
      await tester.tapAt(const Offset(10, 10));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('blocked-updates')), findsNothing);
      expect(find.text('1 update needs attention'), findsOneWidget);
    });

    testWidgets("the row's own button is labelled with what it does", (tester) async {
      // Not "Dismiss" and not "OK". This is the only thing in the application that takes a recorded
      // update away, and a driver pressing it should know that is what happened.
      await show(tester, _snapshot([_blocked(1)]));
      await openPanel(tester);

      expect(
        tester
            .widget<TextButton>(find.byKey(const Key('blocked-update-acknowledge-1')))
            .child
            .toString(),
        contains('remove it'),
      );
    });
  });
}
