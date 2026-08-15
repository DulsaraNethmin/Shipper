/// Notifications — push registration, inbox, deep-link routing (`Docs/07` §2).
///
/// Permission is requested at a moment its value is obvious — after a first bid arrives, not
/// on first launch (`Docs/07` §5). The app stays fully usable when it is declined and offers a
/// route back.
///
/// Push is a prompt, never a channel of record (`Docs/07` §5): anything that matters is also
/// retrievable in-app. Notification content excludes addresses, goods descriptions and full
/// customer names, because it renders on a lock screen (`Docs/05` §4).
///
/// ## What is here (SHIP-143)
///
/// - `device_token.dart` — the registration the platform answers with, and which store this build
///   came from. **No `token` field**, because the contract does not send one back.
/// - `notifications_repository.dart` — the two endpoints, and why the deregistration travels on a
///   client that carries no credential.
/// - `push_token_source.dart` — where the token comes from, which is **a seam with nothing behind
///   it**: no Firebase project exists and no ticket anywhere creates one. Read that file before
///   assuming push works on a device.
/// - `push_registration.dart` — when each call fires, and the reason the sign-out half cannot be a
///   listener on the session.
///
/// The inbox and deep-link routing are SHIP-145 and SHIP-146 and are not here.
library;
