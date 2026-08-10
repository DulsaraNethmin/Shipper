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
/// Empty until SHIP-48.
library;
