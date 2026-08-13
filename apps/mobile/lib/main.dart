import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/app.dart';
import 'package:shipper/core/sync/queue_watch.dart';
import 'package:shipper/core/sync/sync_worker.dart';
import 'package:shipper/core/version/running_build.dart';

/// Entry point.
///
/// Everything above the app widget lives here and nowhere else: one provider scope at the
/// root, and the app. Riverpod resolves providers through this scope, so a second one — in a
/// test helper, or wrapped around a screen "just for this feature" — silently gives that
/// subtree its own copy of every provider, including the session. Tests override providers on
/// this scope rather than creating another.
///
/// ## The container is built here rather than by `ProviderScope`, so the sync worker can start
///
/// SHIP-125's worker has to run before the first frame: `recover()` returns to the queue whatever
/// a previous process was mid-send when it died, and nothing else in the application looks. That
/// needs a provider read outside the widget tree, which is what [UncontrolledProviderScope] is
/// for — it is still exactly one scope, and every override still goes on it.
///
/// **The worker is started here and not from `ShipperApp`,** which is a decision about tests
/// rather than about start-up. Every widget test builds `ShipperApp`, and a worker in that widget
/// would have each of them open the platform's application-support directory to construct a
/// queue database — the objection SHIP-124 raised against wiring sign-out early, arriving from
/// the other direction. `main` is the one place the real application exists and no test does.
///
/// ## The one thing awaited before the first frame, and the one thing deliberately not (SHIP-168)
///
/// [RunningBuild.read] is awaited. It is a single platform-channel round trip that cannot fail on
/// a network and cannot hang on a connection, and it returns `null` rather than throwing on
/// anything at all — so the worst case is an application that starts with its version gate inert,
/// which is the same as the application that existed before SHIP-168.
///
/// **The launch check itself is not awaited**, and that is the decision, not an oversight. Holding
/// the first frame behind `GET /v1/app/minimum-version` would make a network round trip a hard
/// dependency of starting the app — up to `connectTimeout` of blank screen on a bad connection,
/// against a `Docs/07` §4 that calls working without signal the client's most important capability.
/// The gate starts the request on the first frame and blocks when the answer arrives.
/// `version_gate.dart` argues the whole of it.
Future<void> main() async {
  // Required before a provider is read, because the sync worker's signal source attaches an
  // `AppLifecycleListener` to the binding. `runApp` would have initialised it a few lines later,
  // which is too late. It is also required before any platform channel, which the line below is.
  WidgetsFlutterBinding.ensureInitialized();

  final runningBuild = await RunningBuild.read();

  // Two seams, filled here and nowhere else, for one reason.
  //
  // `queueWatchProvider` is what the pending-updates indicator reads (SHIP-126), and it is empty
  // until it is supplied here. That inversion is deliberate and is SHIP-124's objection kept: the
  // indicator lives inside `ShipperApp`, which every widget test builds, and a provider that reached
  // the worker on its own would have each of them open a Drift database in the platform's
  // application-support directory. **This override is the whole of the production wiring** — without
  // it the app runs with an indicator that never appears, so `sync_wiring_test.dart` holds it.
  //
  // `runningBuildProvider` is the same shape one ticket later (SHIP-168). `VersionGate` also lives
  // inside `ShipperApp`, so a provider that reached the package-info channel on its own would have
  // every widget test call a plugin with nothing behind it and then make an HTTP request. Without
  // this override the app runs with a gate that can never block, however high the platform raises
  // the floor, and `version_gate_wiring_test.dart` holds it for exactly that reason.
  final container = ProviderContainer(
    overrides: [
      queueWatchProvider.overrideWith((ref) => ref.watch(syncWorkerProvider)),
      runningBuildProvider.overrideWithValue(runningBuild),
    ],
  );
  unawaited(container.read(syncWorkerProvider).start());

  runApp(UncontrolledProviderScope(container: container, child: const ShipperApp()));
}
