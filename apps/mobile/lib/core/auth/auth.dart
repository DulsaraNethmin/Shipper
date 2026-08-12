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
/// The files, in the order a cold start uses them:
///
/// - `token_store.dart` — the Keychain and Keystore wrapper (SHIP-48).
/// - `session_state.dart` — restoring, signed out, signed in (SHIP-49).
/// - `session_controller.dart` — the restore, the refresh, sign-in and sign-out (SHIP-49,
///   SHIP-50, SHIP-55).
/// - `token_pair.dart` — the credentials sign-in and refresh both answer with (SHIP-41,
///   SHIP-42).
/// - `session_refresher.dart` — `POST /v1/auth/refresh`. The session's own call, not a
///   screen's, which is why it is not on `IdentityRepository` (SHIP-50).
/// - `access_token.dart` — the `role` claim, read for presentation and never for authorisation.
/// - `device_label.dart` — what a sign-in calls this handset in the device list (SHIP-55).
///
/// **Nothing here decides what an account may do.** Signed in means this device holds a
/// refresh token, which is a navigation fact; the platform decides the rest, on every request.
///
/// The interceptor that reacts to a `401` is `core/api/auth_interceptor.dart`, because it is a
/// `dio` interceptor. It takes the two-member interface it needs from the session rather than
/// importing this folder's controller — the same rule `CLAUDE.md` applies to `internal/httpx`.
library;
