/// Identity — registration, sign-in, verification, role selection (`Docs/07` §2).
///
/// The role chosen at signup decides which half of the marketplace the user sees, and
/// `Docs/07` §1 requires those halves to stay genuinely separate inside the one app. That
/// separation is navigation and presentation only: the platform decides what the account may
/// actually do, on every request (`Docs/07` §3).
///
/// ## What is here
///
/// - `signed_out_screen.dart` — where a signed-out cold start lands (SHIP-49).
/// - `account.dart` — the `Account` schema from `contracts/paths/identity.yaml`, which is the
///   body of registration and of both verification confirms.
/// - `identity_repository.dart` — the five public identity endpoints, all built in wave 2.
/// - `signup_controller.dart` — the journey's state, and one idempotency key per action.
/// - `role_selection_screen.dart` — which half of the marketplace, chosen first (SHIP-52).
/// - `registration_screen.dart` — create an account (SHIP-51).
/// - `registration_complete_screen.dart` — where the journey ends, honestly stubbed.
///
/// Verification is SHIP-53 and SHIP-54, sign-in SHIP-55. Each consumes an endpoint built in an
/// earlier wave — `Docs/11` §7 forbids a screen depending on an endpoint from its own wave, and
/// `POST /v1/auth/login` does not exist at all yet.
///
/// ## The whole journey happens signed out
///
/// `POST /v1/auth/register` returns an account and **no token**: registering is not signing in,
/// and the platform is deliberate about it. So the session stays `SessionSignedOut` from the
/// first screen to the last, which is why `core/routing/app_router.dart` carries a set of
/// locations a signed-out user may be at rather than one.
///
/// The session itself is not here. It lives in `core/auth`, because the router and every future
/// feature read it, and a feature that owned it would be imported by all of them. `UserRole` is
/// there for the same reason: the shell reads it.
library;
