// The application, signed in as a provider, standing on one job's delivery screen (SHIP-129).
//
// The queue underneath is the **real** one over a real SQLite file — `SyncHarness` from the sync
// tests — because that is what makes "pending" mean anything here: an in-memory fake would confirm
// whatever the test told it to, and the whole of this ticket is the difference between what the
// device holds and what the platform has.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/routing/app_router.dart';

import '../../core/auth/session_fixtures.dart';
import '../../core/sync/sync_fixture.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';

/// Signs [role] in and walks to `/jobs/{id}/delivery`.
///
/// **The route is reached the way a person reaches it — as a link** — because that is the only way
/// in today: `Docs/07` §5 deep-links a notification to the job it concerns and SHIP-145 is what
/// sends the provider one. Pushing the screen directly would skip the router's guard, which is
/// exactly where a missing entry in `_signedInPatterns` would show up, and a route that lands on
/// the home shell instead looks from the outside like a link that does nothing.
/// [nudgeClock] is what SHIP-127 measures `enqueued_at` against, and it defaults to the handset's
/// own — so a test that is not about the four-hour nudge never sees one, however old the fixture's
/// fixed date has become. A test that *is* about it hands over a clock running ahead of the queue's,
/// which is what "recorded before breakfast, still unsent at lunchtime" looks like from inside.
Future<void> openDelivery(
  WidgetTester tester, {
  required SyncHarness harness,
  required String jobId,
  UserRole role = UserRole.provider,
  DateTime Function()? nudgeClock,
}) async {
  // A phone-shaped surface rather than the 800×600 default, and a tall one: three large buttons
  // and a log of what was recorded should scroll rather than be reported as overflowing.
  tester.view.physicalSize = const Size(800, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: role);

  await tester.pumpWidget(
    signupApp(identity, worker: harness.worker, clock: nudgeClock ?? harness.now),
  );
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();

  await followLink(tester, Routes.deliveryFor(jobId));
}

/// Delivers [location] to the running application the way the platform delivers a deep link.
///
/// Through the app's own router, so the guard in `redirectFor` runs on it exactly as it does on a
/// link opened from a notification.
Future<void> followLink(WidgetTester tester, String location) async {
  final context = tester.element(find.byType(ShipperApp));
  ProviderScope.containerOf(context, listen: false).read(routerProvider).go(location);
  await tester.pumpAndSettle();
}

/// Taps one of the milestone buttons and lets the enqueue and the drain it wakes finish.
///
/// `SyncWorker.record` is enqueue-and-send in one call, so a tap is a database write and a network
/// attempt. Waiting for [SyncWorker.drained] is what makes the assertion afterwards about a settled
/// state rather than about whichever half of it had happened by the time the frame was pumped.
Future<void> recordMilestone(
  WidgetTester tester,
  SyncHarness harness,
  String wire,
) async {
  await tester.tap(find.byKey(Key('milestone-record-$wire')));
  await tester.pumpAndSettle();
  await harness.worker.drained;
  await tester.pumpAndSettle();
}
