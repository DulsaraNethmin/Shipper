// SHIP-168 — the gate as the application actually mounts it.
//
// `version_gate_test.dart` proves the comparison. That is not the *Done when*: it says a build
// below the floor **blocks**, and a verdict nothing acts on blocks nobody. So this builds the real
// `ShipperApp` and checks what a person would see — including the two cases where the right answer
// is that nothing happens at all.

import 'dart:async';
import 'dart:io';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/version/minimum_version.dart';
import 'package:shipper/core/version/running_build.dart';
import 'package:shipper/core/version/version_gate.dart';

import '../auth/fake_token_store.dart';
import 'version_fixture.dart';

/// The signed-out shell, which is what this app boots into with an empty token store. Its
/// presence or absence is how "blocked" is observed: below the floor there must be no shell at
/// all, not a shell with something drawn over it.
const shell = Key('shell-signed-out');

void main() {
  /// Builds the real application with the gate's two seams filled.
  ///
  /// [held] substitutes the check itself, for the one case a stubbed transport cannot express:
  /// an answer that has not arrived yet.
  Future<void> pumpApp(
    WidgetTester tester, {
    RunningBuild? build,
    StubAdapter? platform,
    Future<MinimumVersion?>? held,
    bool settle = true,
  }) async {
    await tester.pumpWidget(
      ProviderScope(
        overrides: [
          tokenStoreProvider.overrideWithValue(FakeTokenStore()),
          runningBuildProvider.overrideWithValue(build),
          if (platform != null)
            minimumVersionRepositoryProvider.overrideWithValue(repositoryOver(platform)),
          if (held != null) launchVersionCheckProvider.overrideWith((ref) => held),
        ],
        child: const ShipperApp(),
      ),
    );
    settle ? await tester.pumpAndSettle() : await tester.pump();
  }

  testWidgets('a build below the floor replaces the application, rather than covering it', (
    tester,
  ) async {
    // The *Done when*, in the shape the pilot ships: no `store_url` in the response at all.
    await pumpApp(
      tester,
      build: const RunningBuild(platform: AppPlatform.android, number: 41),
      platform: StubAdapter.returning(floorsBody(android: 42)),
    );

    expect(find.byKey(const Key('update-required')), findsOneWidget);
    expect(find.byKey(const Key('update-no-store-link')), findsOneWidget);

    // The half that makes it a block rather than a banner. If the gate drew over the application
    // instead of in place of it, the shell would still be in the tree — reachable by the Android
    // back gesture, by a deep link, and by anything that dismissed the barrier.
    expect(find.byKey(shell), findsNothing);
  });

  testWidgets('and the same build blocks with a link when the platform sends one', (tester) async {
    // The other shape of the *Done when*. Both have to work; only the first is what runs today.
    await pumpApp(
      tester,
      build: const RunningBuild(platform: AppPlatform.ios, number: 41),
      platform: StubAdapter.returning(
        floorsBody(ios: 42, iosStoreUrl: 'https://apps.apple.com/app/id123'),
      ),
    );

    expect(find.byKey(const Key('update-open-store')), findsOneWidget);
    expect(find.byKey(const Key('update-no-store-link')), findsNothing);
    expect(find.byKey(shell), findsNothing);
  });

  testWidgets('a supported build is left alone', (tester) async {
    await pumpApp(
      tester,
      build: const RunningBuild(platform: AppPlatform.ios, number: 42),
      platform: StubAdapter.returning(floorsBody(ios: 42)),
    );

    expect(find.byKey(const Key('update-required')), findsNothing);
    expect(find.byKey(shell), findsOneWidget);
  });

  testWidgets('an unreachable platform does not block the app', (tester) async {
    // The decision this ticket is most likely to be judged on. Failing closed would turn every
    // API outage into an app that will not open, and the way out of that is a release — which is
    // the loop Docs/07 §6 says mobile does not have. See `version_gate.dart`.
    await pumpApp(
      tester,
      build: const RunningBuild(platform: AppPlatform.ios, number: 1),
      platform: StubAdapter.unreachable(),
    );

    expect(find.byKey(const Key('update-required')), findsNothing);
    expect(find.byKey(shell), findsOneWidget);
  });

  testWidgets('a check that failed once still blocks, without waiting for the next launch', (
    tester,
  ) async {
    // The other half of failing open, and the half that makes it affordable: Riverpod re-runs a
    // failed provider with exponential backoff, so a launch that landed in a lift gets its answer
    // when the signal returns rather than at the next cold start. This is the behaviour that
    // bounds how much an unreachable API actually costs, so it is held rather than assumed.
    var attempts = 0;
    final flaky = StubAdapter((options) {
      attempts++;
      if (attempts == 1) {
        throw DioException(requestOptions: options, type: DioExceptionType.connectionError);
      }
      return jsonResponse(floorsBody(ios: 42));
    });

    await pumpApp(
      tester,
      build: const RunningBuild(platform: AppPlatform.ios, number: 41),
      platform: flaky,
      settle: false,
    );

    expect(find.byKey(const Key('update-required')), findsNothing, reason: 'the first ask failed');

    await tester.pumpAndSettle();

    expect(attempts, greaterThan(1), reason: 'it asked again on its own');
    expect(find.byKey(const Key('update-required')), findsOneWidget);
  });

  testWidgets('the app draws while the check is still in flight', (tester) async {
    // Launch is not held behind the round trip. A gate that waited would show a blank screen for
    // as long as the network took, up to the ten-second connect timeout.
    final held = Completer<MinimumVersion?>();
    addTearDown(() => held.complete(null));

    await pumpApp(
      tester,
      build: const RunningBuild(platform: AppPlatform.ios, number: 1),
      held: held.future,
    );

    expect(find.byKey(shell), findsOneWidget);
    expect(find.byKey(const Key('update-required')), findsNothing);
  });

  testWidgets('nothing is asked of the platform until something supplies a build', (tester) async {
    // This is what keeps SHIP-168 free for the other 700-odd tests in this suite. `ShipperApp` is
    // built by nearly all of them, and a gate that fetched on its own would have every one of
    // them open a connection to whatever base URL the test binary was compiled with.
    final adapter = StubAdapter.returning(floorsBody(ios: 9999));

    await pumpApp(tester, platform: adapter);

    expect(adapter.requests, isEmpty);
    expect(find.byKey(shell), findsOneWidget);
  });

  test('main.dart supplies the running build, which is the whole of the production wiring', () {
    // A source assertion rather than a call, for the reason SHIP-126's is one: `main()` calls
    // `runApp` and reaches a platform channel, which a host test cannot do. What can be checked is
    // that the override is there at all — and its absence is a build whose gate can never block,
    // however high the floor is raised. No test fails, nothing is logged, and the failure is
    // discovered the day somebody needs to retire a build and finds they cannot.
    final source = File('lib/main.dart').readAsStringSync();

    expect(
      source,
      contains('runningBuildProvider.overrideWithValue'),
      reason: 'lib/main.dart no longer supplies runningBuildProvider. The launch gate reads it, '
          'and with nothing to compare it blocks nobody — see Docs/07 §6 and the note on '
          'running_build.dart.',
    );
  });
}
