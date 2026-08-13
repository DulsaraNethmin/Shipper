import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/core/sync/pending_updates_indicator.dart';
import 'package:shipper/core/version/version_gate.dart';
import 'package:shipper/shared/design_system/app_theme.dart';

/// The application widget.
///
/// A [ConsumerWidget] because the router is itself a provider: `Docs/07` §2 keeps
/// role-specific navigation genuinely separate inside one app, which means the route set
/// depends on session state rather than being a constant. Reading it here is what lets the
/// whole navigation graph change when the session does, without a second [MaterialApp].
///
/// ## The one thing drawn outside the navigator, and why it is here (SHIP-126)
///
/// `Docs/02` §3.1 requires a **persistent** indicator of how many updates are unsynced, and
/// persistent means it does not go away when the screen does. `builder` runs below the theme and
/// above the navigator, so a widget placed here is on every route in the application and survives
/// every navigation — which is the difference between answering "did my work go?" on one screen and
/// answering it wherever the driver happens to be.
///
/// **It costs a test nothing**, which is the objection SHIP-124 raised against wiring the queue into
/// anything every widget test builds. [PendingUpdatesIndicator] reads `queueWatchProvider`, which is
/// `null` until `main.dart` supplies the running worker, so a test that has not asked for a queue
/// gets a `SizedBox.shrink()` and opens no database.
///
/// ## The second thing outside the navigator, and why it is outside the indicator too (SHIP-168)
///
/// [VersionGate] wraps the whole of the above rather than sitting beside it. Below the platform's
/// build floor the application is **replaced**, not covered — no route, no navigator, and no
/// pending-updates bar either — because `Docs/09` says the prompt *blocks*, and a barrier with a
/// live application underneath it is one somebody finds their way around.
///
/// It costs a test nothing for the same reason and by the same mechanism: `runningBuildProvider` is
/// `null` until `main.dart` supplies it, and with no build number there is nothing to compare, so
/// the gate makes no request and draws its child. `version_gate.dart` holds both halves.
class ShipperApp extends ConsumerWidget {
  const ShipperApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final router = ref.watch(routerProvider);

    return MaterialApp.router(
      title: 'Shipper',
      theme: AppTheme.light,
      darkTheme: AppTheme.dark,
      routerConfig: router,
      // Below the content rather than over it. An overlay would sit on whatever a screen put in the
      // bottom corner, and on the customer shell that is the button which publishes a delivery.
      builder: (context, child) => VersionGate(
        child: Column(
          children: <Widget>[
            Expanded(child: child ?? const SizedBox.shrink()),
            const PendingUpdatesIndicator(),
          ],
        ),
      ),
    );
  }
}
