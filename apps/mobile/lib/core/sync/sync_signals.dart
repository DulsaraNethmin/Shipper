import 'dart:async';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Why a drain was asked for (SHIP-125).
///
/// Carried through the worker so that a failure has a cause and not just a moment. The set is
/// closed, and adding to it is the decision "something else should start a drain" — which is the
/// decision this ticket exists to take carefully.
enum SyncTrigger {
  /// The application started. Follows `recover()`.
  launch,

  /// A session appeared, so there is now a credential to send under.
  session,

  /// The user just recorded something.
  recorded,

  /// The application came back to the foreground.
  resumed,

  /// The worker's own wake-up, at the earliest moment an operation may be tried again.
  scheduled,

  /// Something outside the app suggested the network changed.
  ///
  /// Nothing supplies this today, and that is the position `Docs/11` §3 records rather than an
  /// omission. See [SyncSignals].
  connectivity,
}

/// A source of "now would be a good moment to try" (SHIP-125).
///
/// ## Why this is a stream the worker is given, and not a connectivity package it imports
///
/// The *Done when* says the queue drains on **reconnection**, and the obvious reading — add a
/// connectivity plugin, drain when it fires — produces a worker that is wrong in both directions
/// on a handset:
///
/// - **A connectivity event is not sufficient.** "Joined a network" is true on a hotel captive
///   portal, on a Wi-Fi access point whose uplink is down, and on a bar of GPRS that will time out
///   every request. The only honest test of "can this device reach the platform" is a request to
///   the platform, which is the very thing the worker was about to do.
/// - **A connectivity event is not necessary, and this is the half that strands work.** A route
///   comes back with no event at all — a mast recovers, a captive portal is signed into, a
///   carrier's transit is repaired, a VPN reconnects — and the operating system reports the same
///   network throughout. **A worker that drains only on a connectivity event waits forever for one
///   that never comes.**
///
/// So the worker treats every signal as a *hint* and owns a schedule of its own that does not
/// depend on any of them: see `SyncWorker`'s six triggers, of which the self-scheduled wake-up is
/// the one that cannot fail to arrive. A connectivity package is then worth adding for **latency**
/// — draining a second after the radio returns rather than up to a backoff later — and worth
/// nothing for correctness, which is the right basis on which to weigh a new native dependency
/// (`Docs/11` §9 weighs those against the store privacy declarations). Whoever adds one pushes
/// [SyncTrigger.connectivity] onto this stream and changes nothing else.
abstract interface class SyncSignals {
  Stream<SyncTrigger> get signals;

  /// Releases whatever the source is holding.
  void dispose();
}

/// The app coming back to the foreground, as a signal (SHIP-125).
///
/// The one trigger of the five that is genuinely an *event about the outside world*, and the
/// closest thing to "reconnection" the framework offers for free. It is also when it matters
/// most: a driver who pulls the phone out of a pocket expects the indicator to be moving, and a
/// phone in a pocket is where an outage is most likely to have ended unobserved.
final class LifecycleSyncSignals implements SyncSignals {
  LifecycleSyncSignals() {
    _lifecycle = AppLifecycleListener(
      onResume: () => _controller.add(SyncTrigger.resumed),
    );
  }

  final _controller = StreamController<SyncTrigger>.broadcast();
  late final AppLifecycleListener _lifecycle;

  @override
  Stream<SyncTrigger> get signals => _controller.stream;

  @override
  void dispose() {
    _lifecycle.dispose();
    unawaited(_controller.close());
  }
}

/// The application's signal source.
final syncSignalsProvider = Provider<SyncSignals>((ref) {
  final signals = LifecycleSyncSignals();
  ref.onDispose(signals.dispose);
  return signals;
});
