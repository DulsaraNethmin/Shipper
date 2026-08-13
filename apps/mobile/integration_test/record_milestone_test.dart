// SHIP-129, on a device and against a running platform.
//
// Everything under `test/` substitutes the sender, which is the right seam for a screen test and
// is not the same claim as "a provider records a milestone and Shipper has it". This file makes
// that claim: the production widget tree, the production `dio` client with its auth and
// idempotency interceptors, the real Drift queue on the device's own filesystem, the real sync
// worker, and real HTTP to `POST /v1/jobs/{id}/milestones`.
//
// **A green run here means the platform accepted the body this ticket builds** — including
// `recorded_at` in RFC 3339 with the device's offset, which `time.Parse(time.RFC3339, …)` refuses
// without. The screen says "Recorded" only when the operation has left the queue, and an operation
// leaves the queue on exactly one path.
//
// # Running it
//
// It needs a booted simulator *and* the stack and API this worktree runs, so it is deliberately
// out of `make flutter-check` and out of CHECKS — the Flutter CI job is a Linux runner with no
// simulator and no service. From the repository root:
//
//     make up && make migrate-up && make run          # the API on this worktree's HTTP_PORT
//
// It also needs something no endpoint can create yet: **a job awarded to the account signing in.**
// SHIP-92's award endpoint does not exist, so the accepted bid is written the way
// `scripts/verify/70-delivery.sh` writes it — a guarded transition to `Awarded` and one row in
// `bids` with status `Accepted`. That script's `delivery_awarded_job` is the recipe; the header of
// this file deliberately does not duplicate the SQL, because a second copy of it is a second thing
// to keep in step with `Docs/02` §2's trigger.
//
//     cd apps/mobile && flutter test integration_test/record_milestone_test.dart \
//       -d <device> \
//       --dart-define=SHIPPER_API_PORT=<HTTP_PORT> \
//       --dart-define=SHIPPER_DEMO_EMAIL=<the awarded provider> \
//       --dart-define=SHIPPER_DEMO_PASSWORD=correct-horse-battery-staple \
//       --dart-define=SHIPPER_DEMO_JOB=<that job's id>
//
// # It records `en_route_to_pickup`, and that is not an arbitrary choice
//
// `Docs/02` §2 permits `Awarded → En route to pickup` directly — a provider driving the job
// themselves has nobody to nominate — so this is the one milestone an awarded job can take with no
// further setup. It also means the run is repeatable against the same job: SHIP-111 records a
// second `en_route_to_pickup` as a second row under a second key, which is `Docs/02` §5's failed
// attempt and is deliberately not refused.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/core/sync/sync_worker.dart';

/// An account that already exists on the platform, and a job already awarded to it.
const _email = String.fromEnvironment('SHIPPER_DEMO_EMAIL', defaultValue: '');
const _password = String.fromEnvironment('SHIPPER_DEMO_PASSWORD', defaultValue: '');
const _job = String.fromEnvironment('SHIPPER_DEMO_JOB', defaultValue: '');

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  const store = SecureTokenStore();

  setUpAll(() {
    expect(
      _email.isNotEmpty && _password.isNotEmpty && _job.isNotEmpty,
      isTrue,
      reason: 'Pass --dart-define=SHIPPER_DEMO_EMAIL, SHIPPER_DEMO_PASSWORD and '
          'SHIPPER_DEMO_JOB — see the header of this file.',
    );
  });

  testWidgets('a provider records a milestone and the platform takes it', (tester) async {
    await store.clear();

    // The application's own wiring: one container, the worker started outside the widget tree so
    // `recover()` runs before the first frame. This is `main.dart` rather than an approximation of
    // it — a `ProviderScope` on its own would leave the worker unstarted and nothing would drain.
    final container = ProviderContainer();
    addTearDown(container.dispose);

    final worker = container.read(syncWorkerProvider);
    await worker.start();

    await tester.pumpWidget(
      UncontrolledProviderScope(container: container, child: const ShipperApp()),
    );
    await tester.pumpAndSettle();

    await tester.enterText(find.byKey(const Key('sign-in-email')), _email);
    await tester.enterText(find.byKey(const Key('sign-in-password')), _password);
    await tester.pump();
    await tester.tap(find.byKey(const Key('sign-in')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('shell-provider')), findsOneWidget);

    // The way a provider reaches this screen: a link. SHIP-145 delivers one in a push payload.
    container.read(routerProvider).go(Routes.deliveryFor(_job));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('delivery-screen')), findsOneWidget);

    await tester.tap(find.byKey(const Key('milestone-record-en_route_to_pickup')));
    await tester.pumpAndSettle();

    // The drain is fire-and-forget from the tap, so the assertion waits for the pass rather than
    // for a duration. A pass includes the HTTP request, so this is the round trip.
    await worker.drained;
    await tester.pumpAndSettle();

    // "Recorded" means the row left the queue, and a row leaves the queue on one path only: the
    // platform answered and the worker completed it. A `422` would have quarantined it and this
    // would read "Needs attention"; no signal would have left it "Pending".
    expect(find.text('Recorded'), findsOneWidget);
    expect(find.text('Pending'), findsNothing);

    final snapshot = await worker.queue.snapshot();
    expect(snapshot.total, 0, reason: 'nothing should be left on the device');

    unawaited(worker.dispose());
    await store.clear();
  });
}
