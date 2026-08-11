import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/health/health_screen.dart';
import 'package:shipper/core/routing/signed_in_shell.dart';
import 'package:shipper/core/routing/starting_screen.dart';
import 'package:shipper/features/identity/signed_out_screen.dart';

/// Route paths, named once.
///
/// String literals scattered through widgets are how a deep link ends up pointing at a route
/// that was renamed six weeks ago. `Docs/07` §5 requires every notification to deep-link to
/// the job, bid or dispute it concerns, so these paths are a contract with the server's
/// notification payloads and not merely internal navigation.
abstract final class Routes {
  /// Where a cold start lands while the keychain is being read.
  static const starting = '/';

  /// The signed-out shell. Registration, sign-in and verification hang off it from SHIP-51.
  static const signIn = '/sign-in';

  /// The signed-in shell. Role-aware from SHIP-52.
  static const home = '/home';

  /// The connectivity check (SHIP-19).
  ///
  /// Reachable from **both** shells on purpose. It is the only screen that demonstrates build
  /// flavour, base URL, transport, decoding and failure mapping end to end on a real device,
  /// and putting it behind the session would have made SHIP-19 undemonstrable on a fresh
  /// install — which is precisely when somebody needs to know whether the app can reach the
  /// API at all.
  static const health = '/health';
}

/// Locations either shell may show. See [Routes.health].
const _sessionAgnostic = <String>{Routes.health};

/// Where the session says this location should be, or `null` to leave it alone.
///
/// A pure function of the session and the location, separated from [routerProvider] so it can
/// be read as a table and tested as one. Every routing bug in a guard of this shape is a cell
/// in that table nobody thought about, and the cells are only obvious when they are written
/// down together.
///
/// **This is navigation, not authorisation.** `Docs/07` §3 and `CLAUDE.md` both put every
/// authorisation decision on the platform: keeping a signed-out user off a shell that would be
/// empty anyway is a convenience, and reaching one by any means — a deep link, a stale route,
/// a modified build — still fails server-side on the first request it makes. Nothing here is
/// load-bearing for security, and nothing should be made load-bearing for it later.
@visibleForTesting
String? redirectFor(SessionState session, String location) {
  if (_sessionAgnostic.contains(location)) return null;

  return switch (session) {
    // Hold the splash until the keychain answers. This drops a deep link that arrives during
    // the read — the app lands in the shell rather than on the linked screen. Acceptable while
    // nothing deep-links: SHIP-143 introduces the first payloads that do, and it is the ticket
    // that has to remember the arriving location across the restore.
    SessionRestoring() => location == Routes.starting ? null : Routes.starting,
    SessionSignedOut() => location == Routes.signIn ? null : Routes.signIn,
    SessionSignedIn() => location == Routes.home ? null : Routes.home,
  };
}

/// The router, as a provider.
///
/// A provider rather than a top-level constant because navigation depends on session state:
/// `Docs/07` §1 requires customer and provider surfaces to stay genuinely separate, and
/// `Docs/07` §3 makes the session the thing that decides which the user is in.
///
/// It **reads** the session and listens for changes rather than watching it. Watching would
/// rebuild the provider, which constructs a new [GoRouter] and a new navigator, discarding the
/// history and every open route on each sign-in and sign-out. `refreshListenable` is go_router's
/// answer to exactly that: one router for the life of the app, re-evaluating its redirect when
/// told to.
final routerProvider = Provider<GoRouter>((ref) {
  final sessionChanged = ValueNotifier<SessionState>(ref.read(sessionProvider));
  ref.onDispose(sessionChanged.dispose);
  ref.listen(sessionProvider, (_, next) => sessionChanged.value = next);

  return GoRouter(
    initialLocation: Routes.starting,
    refreshListenable: sessionChanged,
    redirect: (context, state) => redirectFor(
      ref.read(sessionProvider),
      state.matchedLocation,
    ),
    routes: <RouteBase>[
      GoRoute(
        path: Routes.starting,
        builder: (context, state) => const StartingScreen(),
      ),
      GoRoute(
        path: Routes.signIn,
        builder: (context, state) => const SignedOutScreen(),
      ),
      GoRoute(
        path: Routes.home,
        builder: (context, state) => const SignedInShell(),
      ),
      GoRoute(
        path: Routes.health,
        builder: (context, state) => const HealthScreen(),
      ),
    ],
  );
});
