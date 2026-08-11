import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/health/health_screen.dart';
import 'package:shipper/core/routing/signed_in_shell.dart';
import 'package:shipper/core/routing/starting_screen.dart';
import 'package:shipper/features/identity/email_verification_screen.dart';
import 'package:shipper/features/identity/registration_complete_screen.dart';
import 'package:shipper/features/identity/registration_screen.dart';
import 'package:shipper/features/identity/role_selection_screen.dart';
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

  /// The signed-out shell. Registration hangs off it from SHIP-51; sign-in is SHIP-55.
  static const signIn = '/sign-in';

  /// The first step of signup: which half of the marketplace this account is (SHIP-52).
  static const chooseRole = '/register/role';

  /// The registration form (SHIP-51).
  static const register = '/register';

  /// Confirm the email address (SHIP-53).
  ///
  /// **Also the app's first deep link, and the scheme is `shipper:///verify-email?token=…`.**
  /// `internal/identity/verification.go` left the choice to this ticket and sends a bare value
  /// meanwhile. A custom scheme needs no registered domain and no store account, both of which
  /// are blocked on X-2 and X-3; the production form is an HTTPS universal and app link, which
  /// needs the entitlement work in SHIP-24…27 and resolves to this same route with this same
  /// query parameter.
  static const verifyEmail = '/verify-email';

  /// Where the signup journey ends (SHIP-51).
  ///
  /// A stub, and honestly so: the real ending signs the new account in, which needs
  /// `POST /v1/auth/login` — SHIP-41, consumed by SHIP-55. Neither exists yet.
  static const registered = '/register/done';

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

/// Locations a signed-out user may be at (SHIP-51).
///
/// **Signing up happens entirely while signed out, and that is the platform's design rather
/// than an oversight.** `POST /v1/auth/register` returns an account and no token — registering
/// is not signing in — so every screen in the journey runs with no session at all. A guard that
/// sent a signed-out user to the sign-in shell from *every* location, which is what SHIP-49 did
/// while there was nothing else to reach, would bounce the user out of registration on the first
/// redirect.
const _signedOutLocations = <String>{
  Routes.signIn,
  Routes.chooseRole,
  Routes.register,
  Routes.verifyEmail,
  Routes.registered,
};

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
    // Hold the splash until the keychain answers. Where a deep link that arrives during the
    // read is *remembered* is [Redirector] — see the note there.
    SessionRestoring() => location == Routes.starting ? null : Routes.starting,
    SessionSignedOut() =>
      _signedOutLocations.contains(location) ? null : Routes.signIn,
    SessionSignedIn() => location == Routes.home ? null : Routes.home,
  };
}

/// The redirect the router actually installs: [redirectFor], plus the one thing a pure function
/// of the session and the location cannot do (SHIP-53).
///
/// **SHIP-49 wrote down that a deep link arriving during the keychain read is dropped**, judged
/// it acceptable while nothing deep-linked, and named SHIP-143 — push payloads — as the ticket
/// that would have to remember the arriving location. SHIP-53 turned out to be the first ticket
/// that deep-links: `shipper:///verify-email?token=…` arrives at a cold start, which is exactly
/// when the session is restoring, so it is *always* the dropped case rather than a rare one.
/// The person taps the link in their email and lands on the sign-in screen.
///
/// So the location is held for the few milliseconds the keychain takes and reissued when the
/// answer arrives. Two properties are worth stating because they are what make this safe:
///
/// - **The held location is still put through [redirectFor].** Holding a location does not
///   exempt it from the guard — a link to the home shell arriving on a signed-out device still
///   goes to the sign-in screen.
/// - **It holds the whole URI, query and all.** Holding only the path would deliver somebody to
///   the verification screen with no token in it, which is worse than not delivering them.
///
/// It is a class rather than a closure so it can be tested as a sequence, which is what it is —
/// its whole behaviour is what the second call does about the first.
@visibleForTesting
class Redirector {
  String? _held;

  /// Where this redirect should go, or `null` to leave it alone.
  String? call(SessionState session, Uri uri) {
    final location = uri.path;

    if (session is SessionRestoring) {
      // Anything other than the splash during the restore is a location somebody asked for and
      // the app is not ready to show yet.
      if (location != Routes.starting) _held = uri.toString();
      return redirectFor(session, location);
    }

    final held = _held;
    if (held != null) {
      // Cleared unconditionally: a location held across one restore must not be reissued on
      // some later sign-out, which would take somebody back to a screen they had left.
      _held = null;
      if (location == Routes.starting) {
        return redirectFor(session, Uri.parse(held).path) ?? held;
      }
    }

    return redirectFor(session, location);
  }
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

  // One per router, and the router is one per application: the hold has to survive every
  // redirect of a single cold start and nothing longer.
  final redirector = Redirector();

  return GoRouter(
    // Only the starting location when the platform has not supplied one. go_router prefers the
    // platform's default route, which is how a cold-start deep link arrives.
    initialLocation: Routes.starting,
    refreshListenable: sessionChanged,
    redirect: (context, state) => redirector(ref.read(sessionProvider), state.uri),
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
        path: Routes.chooseRole,
        builder: (context, state) => const RoleSelectionScreen(),
      ),
      GoRoute(
        path: Routes.register,
        builder: (context, state) => const RegistrationScreen(),
      ),
      GoRoute(
        path: Routes.verifyEmail,
        // The token arrives in the query string, which is the form a deep link of either kind
        // resolves to — a custom scheme now, an HTTPS universal and app link once the domain
        // and the entitlements exist. `matchedLocation` excludes the query, so the guard above
        // sees `/verify-email` either way.
        builder: (context, state) => EmailVerificationScreen(
          deepLinkedToken: state.uri.queryParameters['token'],
        ),
      ),
      GoRoute(
        path: Routes.registered,
        builder: (context, state) => const RegistrationCompleteScreen(),
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
