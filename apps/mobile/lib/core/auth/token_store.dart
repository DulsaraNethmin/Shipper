import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// The refresh token's only home (SHIP-48).
///
/// `Docs/07` §3 is unusually specific about this one, and it is worth restating rather than
/// referencing: the refresh token is stored in the **iOS Keychain and the Android Keystore**,
/// and "tokens must never be written to application preferences, application documents, logs,
/// or crash reports". That is why the Android floor is API 24 (`Docs/07` §9) — a lower floor
/// would silently degrade the storage to something weaker, and a token store that is only
/// sometimes hardware-backed is not the guarantee the document makes.
///
/// The access token is deliberately **not** here. It is short-lived and held in memory
/// (`Docs/07` §3), which means it survives no restart and reaches no disk at all. Writing it
/// alongside the refresh token would give an attacker with filesystem access two credentials
/// where the design gives them one, and would buy nothing: a refresh produces a new access
/// token in the time one round trip takes.
abstract interface class TokenStore {
  /// The stored refresh token, or `null` when there is none.
  ///
  /// `null` is the signed-out answer. A read that *fails* is a different thing and throws, so
  /// that the caller decides what a broken keystore means rather than having it silently
  /// reported as "no session" here — `session_controller.dart` is where that decision is made.
  Future<String?> readRefreshToken();

  /// Stores [token], replacing any previous one.
  ///
  /// Rotation (`Docs/07` §3) is a write per refresh, so this is called on a normal path and
  /// not only at sign-in.
  Future<void> writeRefreshToken(String token);

  /// Deletes everything this store holds.
  ///
  /// Sign-out clears more than this — the offline queue, cached job data, and the push token
  /// registration (`Docs/07` §3). Those live in `core/queue`, `core/storage` and
  /// `features/notifications`, and each clears its own; this one is not a place to reach
  /// across and clear theirs.
  Future<void> clear();
}

/// The real one: Keychain on iOS, Keystore-wrapped storage on Android.
///
/// **The options below are the ticket.** `flutter_secure_storage`'s defaults are already the
/// hardware-backed path on both platforms, but a default is a thing that can change in a minor
/// version without anybody reading the changelog, and `Docs/07` §3 is a guarantee rather than a
/// preference. Stating them makes the guarantee visible in the diff and, more usefully,
/// testable — `token_store_test.dart` asserts what actually reaches the platform.
class SecureTokenStore implements TokenStore {
  /// Takes its storage positionally so a test can hand in one built over a recording platform
  /// implementation. Defaults to a store carrying [iosOptions] and [androidOptions], which is
  /// what the application uses.
  const SecureTokenStore([FlutterSecureStorage? storage])
      : _storage = storage ?? const FlutterSecureStorage(
          iOptions: iosOptions,
          aOptions: androidOptions,
        );

  final FlutterSecureStorage _storage;

  /// The keychain / keystore entry holding the refresh token.
  ///
  /// Renaming this orphans every installed build's session — the old entry is not read and not
  /// deleted, and the user is signed out with a token still on the device. If it ever has to
  /// change, the change reads the old key once and deletes it.
  static const refreshTokenKey = 'shipper.refresh_token';

  /// Every key this store owns.
  ///
  /// [clear] deletes all of them, so a key added here is cleared at sign-out without anybody
  /// remembering a second edit. `deleteAll()` was the alternative and is worse: it would also
  /// remove entries a future ticket stores under its own name, from a method whose caller only
  /// asked to end a session.
  static const ownedKeys = <String>{refreshTokenKey};

  /// iOS.
  ///
  /// `first_unlock_this_device`, and both halves are deliberate.
  ///
  /// **`first_unlock`** rather than `unlocked`, because `Docs/07` §4 has a sync worker that
  /// finishes queued milestones and proof uploads after the app is backgrounded, and §5 has
  /// push arriving on a locked screen. Those need a token that is readable after the first
  /// unlock following a restart; `unlocked` would fail exactly then, which reads as a random
  /// sync failure rather than as a storage decision.
  ///
  /// **`this_device`** because `Docs/07` §3 keeps one session record per device, individually
  /// revocable. A token that migrates in an encrypted backup restore arrives on a second
  /// device holding the first device's session, which makes the revocation list a lie.
  /// `synchronizable` is `false` for the same reason, from the iCloud direction.
  static const iosOptions = IOSOptions(
    accessibility: KeychainAccessibility.first_unlock_this_device,
    synchronizable: false,
  );

  /// Android.
  ///
  /// The default cipher pair — an AES/GCM data key wrapped by an RSA-OAEP key held in the
  /// Android Keystore — is the API 23+ path `Docs/07` §9 argues the floor from, and API 24
  /// clears it. The plugin reached the Keystore through `EncryptedSharedPreferences` when that
  /// argument was written and reaches it through its own Keystore-wrapped ciphers now; the
  /// requirement of API 23 and the conclusion of API 24 are unchanged, which is the reason
  /// `Docs/07` §9 needed a correction and not a re-argument.
  ///
  /// `resetOnError` stays at its default of `true`, and it is a real trade-off rather than an
  /// oversight. A keystore entry can become undecryptable — a restored backup, a changed screen
  /// lock, a device migration — and the two available outcomes are "sign the user out" and "the
  /// app cannot read its own storage until it is reinstalled". The first is recoverable by
  /// signing in again. Nothing here is the only copy of anything: the session also exists
  /// server-side in `device_sessions` (SHIP-38).
  static const androidOptions = AndroidOptions();

  @override
  Future<String?> readRefreshToken() => _storage.read(key: refreshTokenKey);

  @override
  Future<void> writeRefreshToken(String token) =>
      _storage.write(key: refreshTokenKey, value: token);

  @override
  Future<void> clear() async {
    for (final key in ownedKeys) {
      await _storage.delete(key: key);
    }
  }
}

/// The application's token store.
///
/// A provider so that tests and the router substitute an in-memory one rather than reaching a
/// real keychain, and so that nothing constructs its own — a second [SecureTokenStore] built
/// with different options is a second set of guarantees.
final tokenStoreProvider = Provider<TokenStore>((ref) => const SecureTokenStore());
