import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/app.dart';
import 'package:shipper/core/sync/sync_worker.dart';

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
void main() {
  // Required before a provider is read, because the sync worker's signal source attaches an
  // `AppLifecycleListener` to the binding. `runApp` would have initialised it a few lines later,
  // which is too late.
  WidgetsFlutterBinding.ensureInitialized();

  final container = ProviderContainer();
  unawaited(container.read(syncWorkerProvider).start());

  runApp(UncontrolledProviderScope(container: container, child: const ShipperApp()));
}
