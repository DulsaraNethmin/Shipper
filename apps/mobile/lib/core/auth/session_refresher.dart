import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/auth/token_pair.dart';

/// `POST /v1/auth/refresh` (SHIP-42), as the session sees it.
///
/// **It lives in `core/auth` rather than on `IdentityRepository`, and the reason is the import
/// direction.** Refreshing is not a screen's action — no feature calls it, nothing renders its
/// result, and the only caller is the session itself. Putting it on the identity feature would
/// mean `core/auth` importing `features/identity`, which is the edge `CLAUDE.md` names as the
/// one easiest to break by accident: every feature already imports `core/`, so a single import
/// the other way welds all seven to identity. Sign-in stays on `IdentityRepository` (SHIP-55),
/// because signing in *is* a screen's action.
///
/// An interface with one real implementation, following `TokenStore`: the session's tests are
/// about single-flight refreshing and about what a refused refresh does, and neither is a test
/// of `dio`'s wiring.
abstract interface class SessionRefresher {
  /// Exchanges [refreshToken] for a new pair, retiring the one presented.
  ///
  /// **There is no window in which both are valid.** The token sent stops working the moment
  /// this succeeds, so the caller stores the new one before making another request — and
  /// presenting the old one again invalidates the whole device session rather than merely
  /// failing (SHIP-40).
  ///
  /// [idempotencyKey] arrives from the caller, as everywhere else in this client. The contract
  /// asks for it specifically here: "reuse the same `Idempotency-Key` for a retry of the same
  /// refresh, and serialise concurrent calls so that only one is in flight". Both halves are the
  /// session's to keep, and it keeps them with `ActionKey` and a single in-flight future.
  Future<TokenPair> refresh({
    required String refreshToken,
    required String idempotencyKey,
  });
}

/// The real one.
final class ApiSessionRefresher implements SessionRefresher {
  const ApiSessionRefresher(this._client);

  final ApiClient _client;

  @override
  Future<TokenPair> refresh({
    required String refreshToken,
    required String idempotencyKey,
  }) async {
    return TokenPair.fromJson(
      await _client.postJson(
        '/v1/auth/refresh',
        idempotencyKey: idempotencyKey,
        body: refreshBody(refreshToken),
      ),
    );
  }
}

/// The request body, built where a test can read it against `contracts/paths/identity.yaml`.
///
/// The platform refuses unknown fields on this body, so the key name is part of the contract.
Map<String, Object?> refreshBody(String refreshToken) =>
    <String, Object?>{'refresh_token': refreshToken};

/// The application's refresher.
///
/// Built over [unauthenticatedApiClientProvider] rather than the ordinary client, deliberately.
/// The credential for this call is in the body; a bearer token on it would be an expired token
/// sent to the endpoint that exists to replace it, and — worse — the refresh would then be
/// travelling through the very interceptor that reacts to a `401` by refreshing.
final sessionRefresherProvider = Provider<SessionRefresher>(
  (ref) => ApiSessionRefresher(ref.watch(unauthenticatedApiClientProvider)),
);
