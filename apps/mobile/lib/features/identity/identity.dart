/// Identity — registration, sign-in, verification, role selection (`Docs/07` §2).
///
/// The role chosen at signup decides which half of the marketplace the user sees, and
/// `Docs/07` §1 requires those halves to stay genuinely separate inside the one app. That
/// separation is navigation and presentation only: the platform decides what the account may
/// actually do, on every request (`Docs/07` §3).
///
/// Empty until M1. The screens arrive at SHIP-49 onwards, against endpoints built in the
/// previous wave — `Docs/11` §7 forbids a screen depending on an endpoint from its own wave.
library;
