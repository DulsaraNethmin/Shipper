import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';

/// `POST /v1/auth/logout` (SHIP-43), as the session sees it.
///
/// **In `core/auth` for the reason `SessionRefresher` is**, and it is the same reason: signing out
/// on the platform is not a screen's action. No feature calls it, nothing renders its result, and
/// the only caller is the session itself. Putting it on `IdentityRepository` would mean
/// `core/auth` importing `features/identity`, which is the edge that welds every feature to one
/// of them through a file none of them contains.
///
/// ## Why it takes the token instead of letting the client attach one
///
/// This is the one call in the application that must **not** travel through [AuthInterceptor],
/// and there are two independent reasons:
///
/// - **The token is being discarded as this is sent.** `SessionController.signOut` clears the
///   in-memory access token, and the interceptor reads it at request time rather than at call
///   time — so a fire-and-forget logout dispatched through the ordinary client races the clear
///   and usually loses, going out with no credential at all and answering `401`.
/// - **A `401` would start a refresh.** The interceptor's answer to one is to refresh and replay,
///   which on the way out means minting a fresh session in order to end it — and, when the
///   refresh fails, calling `signOut` from inside `signOut`.
///
/// So the credential is passed in and set as a header on a transport that carries no session at
/// all. It is the token being thrown away, spent on the request that makes throwing it away mean
/// something.
///
/// ## It is allowed to fail, and nothing waits for it
///
/// `Docs/07` §3 is explicit that what actually ends the session is the server-side revocation and
/// that the device is catching up. A sign-out that left somebody in the signed-in shell because a
/// request failed would be the worst of both outcomes. The contract agrees from its own side: the
/// endpoint answers `204` for a session already revoked and for a token naming one that no longer
/// exists, "because a failure here would only leave somebody on a screen they cannot get past".
///
/// **The access token may already have expired, and then this does nothing.** Fifteen minutes is
/// the window, and refreshing first to widen it would be a client minting a credential in order
/// to destroy one. `DELETE /v1/auth/sessions/{id}` (SHIP-46) is how a session that outlived its
/// device is ended, and the refresh token lapses on its own at thirty days.
abstract interface class SessionEnder {
  /// Revokes the device session [accessToken] was issued against, and no other.
  ///
  /// **Which session ends is decided by the token, not by the request**: the `sid` claim names
  /// the device the call came from, which is why there is nothing to send and no body here.
  /// Signing out on one device leaves every other device signed in — somebody signing out at a
  /// shared terminal must not lose the session on the phone in their pocket.
  Future<void> end({required String accessToken, required String idempotencyKey});
}

/// The real one.
final class ApiSessionEnder implements SessionEnder {
  const ApiSessionEnder(this._client);

  final ApiClient _client;

  @override
  Future<void> end({required String accessToken, required String idempotencyKey}) {
    return _client.postNoContent(
      '/v1/auth/logout',
      idempotencyKey: idempotencyKey,
      headers: <String, Object?>{ApiHeaders.bearer: 'Bearer $accessToken'},
    );
  }
}

/// The application's session ender.
///
/// Built over [unauthenticatedApiClientProvider], deliberately — see the note on [SessionEnder].
/// The credential is supplied per call because the session is in the act of forgetting it.
final sessionEnderProvider = Provider<SessionEnder>(
  (ref) => ApiSessionEnder(ref.watch(unauthenticatedApiClientProvider)),
);
