// The word "persistent" in SHIP-126's *Done when*, taken literally.
//
// An indicator in one screen's app bar answers "did my work go?" on that screen. A driver records
// three milestones at a loading dock and walks to the next job, and the question follows them — so
// this file drives the running application, records with no signal, and then leaves the screen the
// recording was made on. Everything below the assertion is the real app: the real router, the real
// queue over a real file, the real worker.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';

import '../../features/delivery/delivery_app.dart';
import 'sync_fixture.dart';

void main() {
  const job = '0198f2c1-1c9c-7b3d-9a2e-4a1f3c5d7e90';

  testWidgets('the count follows the driver off the screen they recorded on', (tester) async {
    final harness = SyncHarness.create(sender: ScriptedSender(thereafter: const ApiUnreachable()));

    await openDelivery(tester, harness: harness, jobId: job);

    // Nothing recorded, nothing said.
    expect(find.byKey(const Key('pending-updates')), findsNothing);

    await recordMilestone(tester, harness, 'picked_up');
    expect(find.text('1 update waiting to sync'), findsOneWidget);

    await recordMilestone(tester, harness, 'in_transit');
    expect(find.text('2 updates waiting to sync'), findsOneWidget);

    // Off to the shell — a different route, a different screen, a different feature.
    await followLink(tester, Routes.home);
    expect(find.byKey(const Key('shell-provider')), findsOneWidget);
    expect(find.byKey(const Key('delivery-screen')), findsNothing);

    expect(
      find.text('2 updates waiting to sync'),
      findsOneWidget,
      reason: 'Docs/02 §3.1: the indicator is persistent, and the user is never left guessing '
          'whether their work was recorded. An indicator that only appears on the screen where the '
          'work was recorded answers the question in the one place it is not being asked.',
    );
  });

  testWidgets('and it goes when the platform has taken everything', (tester) async {
    final harness = SyncHarness.create();

    await openDelivery(tester, harness: harness, jobId: job);
    await recordMilestone(tester, harness, 'picked_up');

    // The per-item answer is still on the delivery screen — "Recorded" beside the milestone
    // (SHIP-129). What the indicator says at zero is nothing at all, deliberately: a bar that is
    // always there and usually says zero is a bar people stop reading.
    expect(find.text('Recorded'), findsOneWidget);
    expect(find.byKey(const Key('pending-updates')), findsNothing);
  });
}
