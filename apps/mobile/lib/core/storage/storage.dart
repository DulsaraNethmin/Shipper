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
/// **The secure half is built and is not here.** SHIP-48 put it in `core/auth/token_store.dart`
/// next to the only thing that reads it, rather than in a storage folder that would then be
/// imported by an authentication concern for one call. This folder is now the cached-reads
/// half alone, and it is empty until the first screen has a job worth caching.
library;
