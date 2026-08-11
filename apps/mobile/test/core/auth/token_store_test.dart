// SHIP-48 — what actually reaches the platform.
//
// The *Done when* line is "refresh token persists in Keychain and Keystore and never in
// preferences", and neither half of that is observable from the Dart side by inspecting the
// store: `SecureTokenStore.writeRefreshToken` returns a Future either way. What is observable
// is the call that crosses the boundary — flutter_secure_storage routes every operation
// through FlutterSecureStoragePlatform.instance, and on a device the instance behind it is the
// Keychain on iOS and Keystore-wrapped storage on Android.
//
// So these tests replace that instance with a recorder and assert on what arrives: the key, the
// value, and the platform options that decide *which* keychain item and *which* keystore entry.
// An implementation swapped for shared_preferences reaches this recorder with nothing at all,
// and every test below fails. That is the point of testing here rather than against a fake
// TokenStore — a fake proves the interface is called, which was never in doubt.
//
// The static half of the same guarantee — that no such swap is even importable — is in
// token_store_is_not_preferences_test.dart.

import 'package:flutter/foundation.dart';
import 'package:flutter_secure_storage_platform_interface/flutter_secure_storage_platform_interface.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/token_store.dart';

void main() {
  late _RecordingSecureStorage platform;

  setUp(() {
    platform = _RecordingSecureStorage();
    FlutterSecureStoragePlatform.instance = platform;
  });

  tearDown(() => debugDefaultTargetPlatformOverride = null);

  // The production object, not one built for the test. Constructing SecureTokenStore with a
  // hand-made FlutterSecureStorage would test the options the test chose.
  const store = SecureTokenStore();

  group('the refresh token round trips through secure storage', () {
    test('a write reaches the secure-storage platform under one known key', () async {
      await store.writeRefreshToken('refresh-abc');

      expect(platform.writes, hasLength(1));
      expect(platform.writes.single.key, SecureTokenStore.refreshTokenKey);
      expect(platform.writes.single.value, 'refresh-abc');
    });

    test('a read returns what was written', () async {
      await store.writeRefreshToken('refresh-abc');

      expect(await store.readRefreshToken(), 'refresh-abc');
    });

    test('a read with nothing stored is null, not an empty string', () async {
      // The distinction matters one layer up: SessionController treats null as signed out, and
      // an empty string that reached it as a token would present as a session with no
      // credential behind it.
      expect(await store.readRefreshToken(), isNull);
    });

    test('clear deletes every key the store declares', () async {
      await store.writeRefreshToken('refresh-abc');

      await store.clear();

      expect(platform.deletes, containsAll(SecureTokenStore.ownedKeys));
      expect(await store.readRefreshToken(), isNull);
    });

    test('clear covers the whole declared key set, not just the token', () async {
      // ownedKeys holds one key today. Sign-out has to clear whatever it holds tomorrow, and
      // this is what fails when a key is added to the set and not to the deletion.
      await store.clear();

      expect(platform.deletes.toSet(), equals(SecureTokenStore.ownedKeys));
    });
  });

  group('the platform options are the guarantee', () {
    test('iOS stores it in this device only, readable after first unlock', () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.iOS;

      await store.writeRefreshToken('refresh-abc');

      final options = platform.writes.single.options;
      // kSecAttrAccessible. `unlocked` would fail the background sync in Docs/07 §4; anything
      // without `this_device` migrates in an encrypted backup restore, which would put one
      // device's session on a second device and make Docs/07 §3's per-device revocation a lie.
      expect(options['accessibility'], 'first_unlock_this_device');
      // kSecAttrSynchronizable — the same migration, by way of iCloud Keychain.
      expect(options['synchronizable'], 'false');
    });

    test('Android wraps the storage key in the Keystore', () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.android;

      await store.writeRefreshToken('refresh-abc');

      final options = platform.writes.single.options;
      // An AES/GCM data key wrapped by an RSA-OAEP key held in the Android Keystore. This is
      // the API 23+ path Docs/07 §9 argues the minSdk of 24 from, so a change here is a change
      // to the reason that floor exists.
      expect(options['storageCipherAlgorithm'], 'AES_GCM_NoPadding');
      expect(options['keyCipherAlgorithm'], 'RSA_ECB_OAEPwithSHA_256andMGF1Padding');
    });

    test('reads and deletes carry the same options as writes', () async {
      debugDefaultTargetPlatformOverride = TargetPlatform.iOS;

      await store.writeRefreshToken('refresh-abc');
      await store.readRefreshToken();
      await store.clear();

      // On iOS the accessibility attribute is part of the item's identity: a read with
      // different options does not find an item written with these ones, and the failure looks
      // like a lost session rather than like a mismatch.
      expect(platform.readOptions.single, equals(platform.writes.single.options));
      expect(platform.deleteOptions.single, equals(platform.writes.single.options));
    });
  });
}

/// Stands where the Keychain and the Keystore stand, and writes down what it is asked to do.
///
/// Extends the platform interface rather than implementing it, as that class's own
/// documentation requires — an added method must arrive as an inherited default rather than as
/// a compile error here.
class _RecordingSecureStorage extends FlutterSecureStoragePlatform {
  final Map<String, String> _data = {};

  final List<({String key, String value, Map<String, String> options})> writes = [];
  final List<Map<String, String>> readOptions = [];
  final List<String> deletes = [];
  final List<Map<String, String>> deleteOptions = [];

  @override
  Future<void> write({
    required String key,
    required String value,
    required Map<String, String> options,
  }) async {
    writes.add((key: key, value: value, options: options));
    _data[key] = value;
  }

  @override
  Future<String?> read({
    required String key,
    required Map<String, String> options,
  }) async {
    readOptions.add(options);
    return _data[key];
  }

  @override
  Future<bool> containsKey({
    required String key,
    required Map<String, String> options,
  }) async =>
      _data.containsKey(key);

  @override
  Future<void> delete({
    required String key,
    required Map<String, String> options,
  }) async {
    deletes.add(key);
    deleteOptions.add(options);
    _data.remove(key);
  }

  @override
  Future<Map<String, String>> readAll({required Map<String, String> options}) async =>
      Map.of(_data);

  @override
  Future<void> deleteAll({required Map<String, String> options}) async => _data.clear();
}
