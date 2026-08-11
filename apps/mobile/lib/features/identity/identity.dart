/// Identity — registration, sign-in, verification, role selection (`Docs/07` §2).
///
/// The role chosen at signup decides which half of the marketplace the user sees, and
/// `Docs/07` §1 requires those halves to stay genuinely separate inside the one app. That
/// separation is navigation and presentation only: the platform decides what the account may
/// actually do, on every request (`Docs/07` §3).
///
/// `signed_out_screen.dart` is the shell a signed-out cold start lands in (SHIP-49). It is a
/// placeholder: registration is SHIP-51, role selection SHIP-52, verification SHIP-53 and
/// SHIP-54, sign-in SHIP-55. Each of those consumes an endpoint built in an earlier wave —
/// `Docs/11` §7 forbids a screen depending on an endpoint from its own wave.
///
/// The session itself is not here. It lives in `core/auth`, because the router and every
/// future feature read it, and a feature that owned it would be imported by all of them.
library;
