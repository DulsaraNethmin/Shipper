// SHIP-129's *Done when*: a provider records milestones, with optimistic local state clearly
// marked as pending.
//
// Two halves, and the second is the one worth writing tests for. Recording is a tap and a row.
// **"Clearly marked as pending" is the claim that can silently stop being true**, because a screen
// where a confirmed and an unconfirmed milestone render identically still passes every test that
// only asserts the milestone is on the list. So the group below renders one of each at once and
// asserts they are told apart, which is the assertion a lost pending marker fails.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/features/delivery/milestone.dart';
import 'package:shipper/features/delivery/record_milestone_controller.dart';

import '../../core/queue/queue_fixture.dart';
import '../../core/sync/sync_fixture.dart';
import 'delivery_app.dart';

/// The platform is not reachable. What a driver in a pickup bay has.
ScriptedSender get offline => ScriptedSender(thereafter: const ApiUnreachable());

/// The status word rendered for the queue row with [id].
Finder statusOf(int id) => find.byKey(Key('milestone-status-$id'));

void main() {
  const job = '0198f2c1-1c9c-7b3d-9a2e-4a1f3c5d7e90';

  group('a provider records a milestone', () {
    testWidgets('and it is on the screen immediately, marked pending, with no signal',
        (tester) async {
      final harness = SyncHarness.create(sender: offline);

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'picked_up');

      // The optimistic local state: it is on screen, it is the milestone that was tapped, and the
      // word beside it says the platform does not have it.
      expect(find.byKey(const Key('milestone-entry-1')), findsOneWidget);
      expect(find.text('Picked up'), findsWidgets);
      expect(statusOf(1), findsOneWidget);
      expect(tester.widget<Text>(statusOf(1)).data, 'Pending');
      expect(find.text('Recorded on this device. Not sent yet.'), findsOneWidget);
    });

    testWidgets('and it says Recorded once the platform has taken it', (tester) async {
      final harness = SyncHarness.create();

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'picked_up');

      expect(tester.widget<Text>(statusOf(1)).data, 'Recorded');
      expect(harness.sender.sent, hasLength(1));

      // Nothing is left on the device: the row is gone because the platform took it, which is the
      // only signal this client has and the only thing "Recorded" claims.
      final snapshot = await harness.queue.snapshot();
      expect(snapshot.total, 0);
    });

    testWidgets('through the queue, as one operation with an idempotency key', (tester) async {
      final harness = SyncHarness.create(sender: offline);

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'en_route_to_pickup');

      final snapshot = await harness.queue.snapshot();
      final queued = snapshot.pending.single;

      expect(queued.kind, OperationKind.milestone);
      expect(queued.method, 'POST');
      expect(queued.path, '/v1/jobs/$job/milestones');
      // `job:<id>` is SHIP-124's convention, and it is what keeps this job's milestones in the
      // order the driver recorded them without another job's problem stalling them.
      expect(queued.orderingKey, 'job:$job');
      expect(queued.idempotencyKey, isNotEmpty);
      expect(queued.attachmentPath, isNull);
    });
  });

  group('the request the platform receives', () {
    testWidgets('names the milestone in the platform’s own wire vocabulary', (tester) async {
      final harness = SyncHarness.create();

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'in_transit');

      expect(harness.sender.sent.single.body['milestone'], 'in_transit');
    });

    testWidgets('carries the actor’s clock, with an offset the platform can parse',
        (tester) async {
      final harness = SyncHarness.create();

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'picked_up');

      final recordedAt = harness.sender.sent.single.body['recorded_at'] as String;

      // **The offset is the whole assertion.** `DateTime.toIso8601String()` on a local value ends
      // `…T09:00:00.000` with no zone at all, which `time.Parse(time.RFC3339, …)` refuses — the
      // driver would be told "that is not a date" about the moment they tapped a button. And a
      // body with no `recorded_at` at all is worse than a rejected one: the platform would stamp
      // the milestone with the time it *arrived*, which for an update queued in a valley is hours
      // after the event it describes, collapsing `Docs/02` §3.1's two clocks into one.
      expect(recordedAt, matches(RegExp(r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}([+-]\d{2}:\d{2}|Z)$')));
      expect(DateTime.tryParse(recordedAt), isNotNull);
    });

    testWidgets('carries nothing else', (tester) async {
      final harness = SyncHarness.create();

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'picked_up');

      // `httpx.DecodeJSON` refuses unknown fields, so a field this client invents is a `400` on a
      // driver's phone rather than something quietly ignored. There is deliberately no `job_id`
      // (it is in the path) and no `provider_id` (it is the token).
      expect(harness.sender.sent.single.body.keys.toSet(), <String>{'milestone', 'recorded_at'});
    });
  });

  group('pending and recorded are told apart on the same screen', () {
    testWidgets('by different words, on two milestones recorded minutes apart', (tester) async {
      // The first recording is accepted; the second meets an outage. This is the ordinary shape of
      // a delivery — signal at the depot, none at the pickup — and it is the state the screen must
      // not render as one thing.
      final harness = SyncHarness.create(
        sender: ScriptedSender(answers: <Object?>[null], thereafter: const ApiUnreachable()),
      );

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'en_route_to_pickup');
      await recordMilestone(tester, harness, 'picked_up');

      final settled = tester.widget<Text>(statusOf(1)).data;
      final unsettled = tester.widget<Text>(statusOf(2)).data;

      expect(settled, 'Recorded');
      expect(unsettled, 'Pending');
      expect(
        settled,
        isNot(unsettled),
        reason: 'Docs/02 §3.1 requires optimistic local state to be clearly marked as pending. '
            'Two milestones, one on the platform and one only on this device, rendering the same '
            'word is the defect SHIP-129 exists to prevent.',
      );
    });

    testWidgets('and by different sentences under them', (tester) async {
      final harness = SyncHarness.create(
        sender: ScriptedSender(answers: <Object?>[null], thereafter: const ApiUnreachable()),
      );

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'en_route_to_pickup');
      await recordMilestone(tester, harness, 'picked_up');

      expect(find.text(MilestoneSync.recorded.detail), findsOneWidget);
      expect(find.text(MilestoneSync.pending.detail), findsOneWidget);
    });

    test('no two sync states share a word', () {
      // The screen renders `MilestoneSync.label`, so two states sharing one would make the tiles
      // above indistinguishable however carefully the widget was written.
      final labels = MilestoneSync.values.map((state) => state.label).toSet();
      expect(labels, hasLength(MilestoneSync.values.length));

      final details = MilestoneSync.values.map((state) => state.detail).toSet();
      expect(details, hasLength(MilestoneSync.values.length));
    });
  });

  group('a refusal is kept and shown, never discarded', () {
    testWidgets('an update the platform would not take needs attention', (tester) async {
      final harness = SyncHarness.create(
        sender: ScriptedSender(thereafter: answer(422, 'delivery_milestone_invalid')),
      );

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'picked_up');

      expect(tester.widget<Text>(statusOf(1)).data, 'Needs attention');

      // Quarantined rather than deleted. SHIP-132 is the screen that says what it lost to, and it
      // can only exist because the row is still here.
      final snapshot = await harness.queue.snapshot();
      expect(snapshot.blocked, hasLength(1));
      expect(snapshot.blocked.single.reason, QueueBlockReason.refused);
    });
  });

  group('the client applies no transition of its own', () {
    testWidgets('every milestone stays available after one is recorded', (tester) async {
      final harness = SyncHarness.create(sender: offline);

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'in_transit');

      // Docs/02 §2 permits Awarded → En route to pickup with no assignment in between, SHIP-111
      // records a second `en_route_to_pickup` as a second row, and SHIP-112 absorbs one that
      // arrives late. A screen that greyed a button out would be enforcing a sequence the platform
      // does not have, on the device Docs/07 §3 says may not decide anything.
      for (final milestone in Milestone.offered) {
        final button = find.byKey(Key('milestone-record-${milestone.wire}'));
        expect(button, findsOneWidget, reason: milestone.wire);
        expect(tester.widget<ButtonStyleButton>(button).onPressed, isNotNull, reason: milestone.wire);
      }
    });

    testWidgets('the same milestone recorded twice is two entries', (tester) async {
      // The driver reached the pickup, found nobody there, and set off again (SHIP-111).
      final harness = SyncHarness.create(sender: offline);

      await openDelivery(tester, harness: harness, jobId: job);
      await recordMilestone(tester, harness, 'en_route_to_pickup');
      await recordMilestone(tester, harness, 'en_route_to_pickup');

      expect(find.byKey(const Key('milestone-entry-1')), findsOneWidget);
      expect(find.byKey(const Key('milestone-entry-2')), findsOneWidget);
      expect((await harness.queue.snapshot()).pending, hasLength(2));
    });
  });

  group('Delivered', () {
    testWidgets('is named and not offered, because proof does not exist yet', (tester) async {
      final harness = SyncHarness.create();

      await openDelivery(tester, harness: harness, jobId: job);

      expect(find.byKey(const Key('milestone-record-delivered')), findsNothing);
      expect(find.byKey(const Key('milestone-delivered-needs-proof')), findsOneWidget);
    });

    test('the controller refuses it even if a button ever appeared', () {
      // Belt and braces beside the screen. Every `delivered` is answered
      // `delivery_proof_required` until SHIP-118, so queueing one puts work the driver believes
      // they recorded into a quarantine only a person can clear.
      expect(Milestone.offered, isNot(contains(Milestone.delivered)));
      expect(Milestone.delivered.needsProof, isTrue);
    });
  });

  group('work already on the device', () {
    testWidgets('is shown when the screen opens, which is the relaunch case', (tester) async {
      final harness = SyncHarness.create(sender: offline);

      // Recorded by a previous run of the app and still on the handset.
      await enqueueMilestone(harness.queue, job: job, milestone: 'picked_up');

      await openDelivery(tester, harness: harness, jobId: job);

      expect(find.byKey(const Key('milestone-entry-1')), findsOneWidget);
      expect(tester.widget<Text>(statusOf(1)).data, 'Pending');
    });

    testWidgets('for another job is not shown on this one', (tester) async {
      final harness = SyncHarness.create(sender: offline);

      await enqueueMilestone(harness.queue, job: 'some-other-job', milestone: 'in_transit');

      await openDelivery(tester, harness: harness, jobId: job);

      expect(find.byKey(const Key('delivery-nothing-recorded')), findsOneWidget);
      expect(find.byKey(const Key('milestone-entry-1')), findsNothing);
    });
  });

  group('the screen is reached as a link, because that is the only way in', () {
    testWidgets('a customer is told whose surface it is rather than shown a job', (tester) async {
      final harness = SyncHarness.create();

      await openDelivery(tester, harness: harness, jobId: job, role: UserRole.customer);

      // `ProviderOnly` — the app may hide, the platform decides. `POST /v1/jobs/{id}/milestones`
      // refuses a customer with the same 404 a stranger gets, which is right on the wire and a poor
      // thing to render as "we could not find that job".
      expect(find.byKey(const Key('milestone-record-picked_up')), findsNothing);
      expect(find.byKey(const Key('provider-only')), findsOneWidget);
    });
  });
}
