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
/// `token_store.dart` is the Keychain and Keystore wrapper (SHIP-48), and it is all that is
/// here so far. The session that reads it on a cold start is SHIP-49; the refresh interceptor
/// is SHIP-50; the screens that produce a first token are SHIP-51 and SHIP-55.
///
/// **Nothing here decides what an account may do.** Holding a refresh token is a navigation
/// fact; the platform decides the rest, on every request.
library;
