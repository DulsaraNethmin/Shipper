/// Local storage (`Docs/07` §2).
///
/// Two stores with different guarantees, kept apart on purpose:
///
/// - **Secure storage** for the refresh token — `core/auth`, Keychain and Keystore only.
/// - **Cached reads** for offline viewing of an active job. Cleared on sign-out and after the
///   job closes (`Docs/07` §4).
///
/// The durable operation queue is neither of these and lives in `core/queue`, because it has
/// a transactional requirement the other two do not.
///
/// Empty until M1.
library;
