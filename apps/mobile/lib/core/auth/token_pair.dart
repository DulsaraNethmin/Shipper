import 'package:shipper/core/errors/api_failure.dart';

/// The credentials `POST /v1/auth/login` and `POST /v1/auth/refresh` both answer with
/// (SHIP-41, SHIP-42).
///
/// ## Hand-written, and that is the decision worth reading
///
/// Every other model in this client is `freezed` plus `json_serializable` (`Docs/10` §8.3), and
/// this one deliberately is not. Both generators write a `toString` listing every field, and
/// `Docs/07` §3 forbids a token reaching "application preferences, application documents, logs,
/// or crash reports". A generated `toString` is precisely how one gets into all three at once:
/// somebody logs the object, or a crash reporter serialises the state holding it, and the
/// credential is in a log aggregator forever. [toString] here names the class and nothing else.
///
/// ## Only two of the contract's four fields are read
///
/// `TokenPair` in `contracts/paths/identity.yaml` also carries `expires_in` and
/// `refresh_token_expires_in`. They are ignored on purpose rather than by omission.
///
/// This client refreshes when the platform says a token is no longer accepted — a `401` — and
/// never on a clock. Scheduling on `expires_in` would require the handset's clock to agree with
/// the platform's, and `Docs/07` §4 is built on phones that have been out of signal, where it
/// does not. A build that refreshed early would burn a rotation for nothing; one that refreshed
/// late would fail the request it was trying to protect. The `401` is the platform's own answer
/// and needs no agreement about what time it is.
///
/// Ignoring them is also what `Docs/07` §6 asks of every response: read what you use, tolerate
/// the rest, so an additive server change needs no release.
final class TokenPair {
  const TokenPair({required this.accessToken, required this.refreshToken});

  /// Sent as a bearer token, held in memory only (`Docs/07` §3).
  final String accessToken;

  /// Stored in the Keychain or the Keystore, and **it replaces the one that was sent**. The
  /// previous token is dead the moment the platform answers, and presenting it again invalidates
  /// the whole device session (SHIP-40).
  final String refreshToken;

  /// Reads the contract's response body.
  ///
  /// Throws [ApiMalformedResponse] rather than returning a pair with an empty field: a response
  /// missing either token is not something a caller can proceed with, and a silently empty
  /// access token would surface much later as an unexplained `401` loop. The status is `200`
  /// because that is what the platform actually answered — this is a body that was not the
  /// contract, not a refusal.
  ///
  /// [ApiMalformedResponse] is also one of the failures `ActionKey` treats as leaving the
  /// outcome unknown, which is the correct reading: nothing about an unparseable body says
  /// whether the platform rotated the token.
  static TokenPair fromJson(Map<String, dynamic> json) {
    final access = json['access_token'];
    final refresh = json['refresh_token'];

    if (access is! String || access.isEmpty || refresh is! String || refresh.isEmpty) {
      throw const ApiMalformedResponse(statusCode: 200);
    }

    return TokenPair(accessToken: access, refreshToken: refresh);
  }

  /// Deliberately carries neither token. See the note on the class.
  @override
  String toString() => 'TokenPair(redacted)';
}
