import 'dart:async';
import 'dart:convert';

import 'package:shipper/core/auth/session_ender.dart';
import 'package:shipper/core/auth/session_refresher.dart';
import 'package:shipper/core/auth/token_pair.dart';
import 'package:shipper/core/auth/user_role.dart';

/// An access token shaped like the one the platform signs, carrying [role].
///
/// **Unsigned, and it has to be.** `internal/identity/token.go` holds the signing key, and a
/// test that needed a real signature would either ship one or reach the service. Nothing in the
/// client verifies the signature — it cannot, and `access_token.dart` says at length why it must
/// never start — so what a fixture has to reproduce is the *shape*: three dot-separated
/// segments with a base64url JSON payload in the middle.
///
/// [role] of `null` produces a token with no `role` claim at all, which is the case the shell
/// renders as "signed in, role not yet known".
String anAccessToken({UserRole? role = UserRole.customer, String subject = 'user-1'}) {
  final claims = <String, Object?>{
    'sub': subject,
    if (role != null) 'role': role.wireName,
  };

  return '${_segment({'alg': 'HS256', 'typ': 'JWT'})}'
      '.${_segment(claims)}'
      '.notasignature';
}

String _segment(Map<String, Object?> json) =>
    base64Url.encode(utf8.encode(jsonEncode(json))).replaceAll('=', '');

/// A pair the platform could have answered with.
///
/// [subject] is what makes two pairs distinguishable. It matters more than it looks: a fixture
/// whose every access token is the same string would make "the replay carried the *new* token"
/// and "a second refresh was avoided" both pass against an implementation that did neither.
TokenPair aTokenPair({
  UserRole? role = UserRole.customer,
  String refreshToken = 'refresh-1',
  String subject = 'user-1',
}) {
  return TokenPair(
    accessToken: anAccessToken(role: role, subject: subject),
    refreshToken: refreshToken,
  );
}

/// A [SessionRefresher] that answers from a script and records what it was asked.
///
/// It counts calls, which is the whole of SHIP-50's second clause: several requests failing with
/// a `401` at once must produce **one** refresh, not one each. Counting is also how the opposite
/// mistake shows up — a client that serialises so hard it never refreshes at all.
class FakeSessionRefresher implements SessionRefresher {
  /// Every refresh token presented, in order. Its length is the number of refreshes.
  final presented = <String>[];

  /// Every idempotency key sent, in order.
  final keys = <String>[];

  /// What each successive call answers with. The last entry is reused once the list runs out, so
  /// a test that does not care about rotation can leave it at one.
  ///
  /// The default deliberately differs from `aTokenPair()` in **both** tokens: a refresh that
  /// handed back what was already held would make several of these tests pass against an
  /// implementation that never refreshed at all.
  var pairs = <TokenPair>[aTokenPair(refreshToken: 'refresh-2', subject: 'refreshed')];

  /// Thrown instead of answering, from the call at this index onwards. `null` never fails.
  Object? failure;

  /// Held open until completed, so a test can have several requests fail while a refresh is
  /// genuinely in flight rather than already finished.
  Completer<void>? gate;

  int get calls => presented.length;

  @override
  Future<TokenPair> refresh({
    required String refreshToken,
    required String idempotencyKey,
  }) async {
    final attempt = presented.length;
    presented.add(refreshToken);
    keys.add(idempotencyKey);

    final held = gate;
    if (held != null) await held.future;

    final thrown = failure;
    if (thrown != null) throw thrown;

    return pairs[attempt < pairs.length ? attempt : pairs.length - 1];
  }
}

/// A [SessionEnder] that records what it was asked, and can refuse.
///
/// It records the **access token each call carried**, which is what makes the rule assertable:
/// the sign-out request is authenticated with the token the session is in the act of discarding,
/// and a client that sent nothing would silently leave the platform session alive for thirty
/// days — which is the defect this exists to stop coming back.
class FakeSessionEnder implements SessionEnder {
  /// Every access token presented, in order. Its length is the number of sign-outs reported.
  final presented = <String>[];

  /// Every idempotency key sent, in order.
  final keys = <String>[];

  /// Thrown instead of answering. A sign-out must survive it.
  Object? failure;

  int get calls => presented.length;

  @override
  Future<void> end({required String accessToken, required String idempotencyKey}) async {
    presented.add(accessToken);
    keys.add(idempotencyKey);

    final thrown = failure;
    if (thrown != null) throw thrown;
  }
}
