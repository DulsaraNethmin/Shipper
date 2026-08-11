/// Session and tokens (`Docs/07` §3).
///
/// The access token is short-lived and held **in memory**; the refresh token is rotated on
/// every use and stored in the iOS Keychain or Android Keystore through
/// `flutter_secure_storage`. Neither ever reaches application preferences, application
/// documents, logs, or crash reports. That is the rule the Android floor of API 24 exists to
/// keep (`Docs/07` §9).
///
/// The driver portal's signed single-job link token is a **separate system**. Neither token
/// can be exchanged for the other, and nothing here should acquire a code path that tries.
///
/// Three files, in the order a cold start uses them:
///
/// - `token_store.dart` — the Keychain and Keystore wrapper (SHIP-48).
/// - `session_state.dart` — restoring, signed out, signed in (SHIP-49).
/// - `session_controller.dart` — the restore, and sign-in and sign-out (SHIP-49).
///
/// **Nothing here decides what an account may do.** Signed in means this device holds a
/// refresh token, which is a navigation fact; the platform decides the rest, on every request.
///
/// The refresh interceptor is SHIP-50, and the screens that produce a first token are SHIP-51
/// and SHIP-55.
library;
