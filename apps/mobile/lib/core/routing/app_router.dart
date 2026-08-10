import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

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
      GoRoute(
        path: Routes.home,
        builder: (context, state) => const _HomePlaceholder(),
      ),
    ],
  );
});

/// Stands in until the post-login shell arrives at SHIP-49.
///
/// SHIP-19 replaces what sits at [Routes.home] with the connectivity check, and the real
/// role-aware shell replaces that in M1.
class _HomePlaceholder extends StatelessWidget {
  const _HomePlaceholder();

  @override
  Widget build(BuildContext context) {
    return const Scaffold(
      body: Center(child: Text('Shipper')),
    );
  }
}
