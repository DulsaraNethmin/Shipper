import 'package:shipper/core/auth/token_store.dart';

/// An in-memory [TokenStore], for the tests that are about what the session does with a token
/// rather than about where the token is kept.
///
/// Those are genuinely separate questions, and answering both in one test answers neither
/// well: where the token is kept is `token_store_test.dart`, against the real
/// `flutter_secure_storage` platform seam. Everything above the store substitutes this, so a
/// routing test fails on routing.
class FakeTokenStore implements TokenStore {
  FakeTokenStore({this.refreshToken});

  /// A store whose reads throw, for the failure the session has to have an answer for.
  FakeTokenStore.unreadable() : _unreadable = true;

  /// What is currently stored — readable directly, so a test can assert that signing out
  /// actually cleared it rather than only that the state changed.
  String? refreshToken;

  bool _unreadable = false;

  /// Whether [clear] was called, which is a different fact from the store being empty.
  var cleared = false;

  @override
  Future<String?> readRefreshToken() async {
    if (_unreadable) throw StateError('keystore unavailable');
    return refreshToken;
  }

  @override
  Future<void> writeRefreshToken(String token) async => refreshToken = token;

  @override
  Future<void> clear() async {
    cleared = true;
    refreshToken = null;
  }
}
