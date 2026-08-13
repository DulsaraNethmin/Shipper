// SHIP-168, on a device and against a running platform.
//
// Everything under `test/` substitutes both of the gate's seams: the running build is a constant
// the test chose, and the floor comes from a stubbed transport. Those are the right seams for a
// unit test and neither is the claim `Docs/09` makes. **This file makes it**: the real
// `package_info_plus` channel reading the real `CFBundleVersion` or `versionCode`, the real `dio`
// client, real HTTP to `GET /v1/app/minimum-version` on a service the operator configured, and the
// production widget tree deciding what to draw.
//
// A green run of the third test is the *Done when* — "a build below the floor blocks with an
// update prompt linking to the store" — demonstrated rather than asserted.
//
// # It is out of `make flutter-check`, like the SHIP-48 and SHIP-129 files beside it
//
// It needs a booted simulator *and* the API this worktree runs, and the Flutter CI job is a Linux
// runner with neither.
//
// # Running it
//
// The floor is server-side configuration, so the three cases are three service configurations
// rather than three fixtures — which is exactly `Docs/07` §6's point that raising the floor is an
// operational act. From the repository root, with `make up && make migrate-up` already done:
//
//   1. A supported build, with the default floor of 1:
//
//        make run
//        cd apps/mobile && flutter test integration_test/version_gate_test.dart -d <device> \
//          --dart-define=SHIPPER_API_PORT=<HTTP_PORT> \
//          --plain-name "reports the build number this process actually is"
//        # and again with --plain-name "a supported build is not blocked"
//
//   2. Blocked, with no link — **the shape the pilot actually ships**, because `IOS_STORE_URL`
//      and `ANDROID_STORE_URL` default to empty and there is no listing to point at yet:
//
//        MIN_SUPPORTED_IOS_BUILD=9999 MIN_SUPPORTED_ANDROID_BUILD=9999 make run
//        … --plain-name "a build below the floor is blocked, and says what to do without a link"
//
//   3. Blocked, with a link — the shape X-2 and X-3 turn on by setting one variable:
//
//        MIN_SUPPORTED_IOS_BUILD=9999 MIN_SUPPORTED_ANDROID_BUILD=9999 \
//        IOS_STORE_URL=https://apps.apple.com/au/app/id0000000000 \
//        ANDROID_STORE_URL=https://play.google.com/store/apps/details?id=au.com.shipper \
//        make run
//        … --plain-name "a build below the floor is blocked, and links to the store"
//
// The store link is deliberately **not tapped**. `launchUrl` would leave the simulator's App Store
// in front of the test harness, and what this file is here to demonstrate is that the platform's
// URL reached the screen — which the button carrying it already shows. `update_required_screen.dart`
// has the tap, over a seam.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/version/running_build.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  /// `main.dart`, rather than an approximation of it: the build number is read from the platform
  /// and supplied on the root scope, and nothing else about the gate is substituted.
  Future<void> launch(WidgetTester tester) async {
    final build = await RunningBuild.read();
    expect(
      build,
      isNotNull,
      reason: 'the platform channel gave no usable build number, so the gate has nothing to '
          'compare and every case below would pass by doing nothing',
    );

    await tester.pumpWidget(
      ProviderScope(
        overrides: [runningBuildProvider.overrideWithValue(build)],
        child: const ShipperApp(),
      ),
    );
    await tester.pumpAndSettle();
  }

  testWidgets('reports the build number this process actually is', (tester) async {
    // The half no host test can reach. `pubspec.yaml` carries `version: 1.0.0+1`, Flutter writes
    // that `+1` into `CFBundleVersion` and `versionCode`, and this reads it back — which is the
    // whole argument for reading the native number instead of duplicating it into a define.
    final build = await RunningBuild.read();

    expect(build, isNotNull);
    expect(build!.platform, isNot(AppPlatform.other), reason: 'iOS or Android, not a host');
    expect(build.number, greaterThan(0));
  });

  testWidgets('a supported build is not blocked', (tester) async {
    // Needs the service on its default floor of 1. This is also the case that proves the gate is
    // not simply blocking everything.
    await launch(tester);

    expect(find.byKey(const Key('update-required')), findsNothing);
  });

  testWidgets('a build below the floor is blocked, and says what to do without a link', (
    tester,
  ) async {
    // Needs MIN_SUPPORTED_*_BUILD=9999 and no store URL — the pilot's own configuration.
    await launch(tester);

    expect(find.byKey(const Key('update-required')), findsOneWidget);
    expect(find.byKey(const Key('update-no-store-link')), findsOneWidget);
    expect(find.byKey(const Key('update-open-store')), findsNothing);

    // The block, rather than a banner over a live application.
    expect(find.byKey(const Key('shell-signed-out')), findsNothing);
  });

  testWidgets('a build below the floor is blocked, and links to the store', (tester) async {
    // Needs MIN_SUPPORTED_*_BUILD=9999 and IOS_STORE_URL / ANDROID_STORE_URL set.
    await launch(tester);

    expect(find.byKey(const Key('update-required')), findsOneWidget);
    expect(find.byKey(const Key('update-open-store')), findsOneWidget);
    expect(find.byKey(const Key('update-no-store-link')), findsNothing);
    expect(find.byKey(const Key('shell-signed-out')), findsNothing);
  });
}
