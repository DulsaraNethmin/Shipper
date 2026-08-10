import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/health/health_screen.dart';

/// Route paths, named once.
///
/// String literals scattered through widgets are how a deep link ends up pointing at a route
/// that was renamed six weeks ago. `Docs/07` §5 requires every notification to deep-link to
/// the job, bid or dispute it concerns, so these paths are a contract with the server's
/// notification payloads and not merely internal navigation.
abstract final class Routes {
  static const home = '/';
}

/// The router, as a provider.
///
/// A provider rather than a top-level constant because navigation depends on session state:
/// `Docs/07` §1 requires customer and provider surfaces to stay genuinely separate, and
/// `Docs/07` §3 makes the session the thing that decides which the user is in. When the
/// session lands (SHIP-49) the redirect logic attaches here.
///
/// **It never makes an authorisation decision.** `Docs/07` §3 and `CLAUDE.md` both put that
/// on the platform: a redirect may keep a signed-out user away from a screen that would be
/// empty anyway, and that is a convenience. Anything the user is not entitled to do fails
/// server-side whether or not the route was reachable.
final routerProvider = Provider<GoRouter>((ref) {
  return GoRouter(
    initialLocation: Routes.home,
    routes: <RouteBase>[
      // The connectivity check (SHIP-19), which stands in until the role-aware shell replaces
      // it in M1. Docs/07 §1 makes that shell the thing that decides which half of the
      // marketplace a signed-in user sees.
      GoRoute(
        path: Routes.home,
        builder: (context, state) => const HealthScreen(),
      ),
    ],
  );
});
