import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/features/notifications/device_token.dart';

/// The two endpoints a handset calls about its own addressability (SHIP-140, SHIP-143).
///
/// Neither takes an identifier and neither has a "somebody else's device" case: a device token
/// binds to the device session the access token was issued against, so a client naming a session
/// would be an authorisation decision made from client input, which `Docs/07` §3 puts on the
/// platform.
///
/// An interface with one real implementation, following every other repository in this client: a
/// test has to be able to hand the registrar something that answers, and a stub transport under a
/// concrete class makes every registration test a test of `dio`'s wiring as well.
abstract interface class NotificationsRepository {
  /// `POST /v1/notifications/device-tokens` — record where this handset can be reached.
  ///
  /// ## Registering again is the ordinary case, not the exception
  ///
  /// The contract says so directly: "Firebase hands the app a token at every launch and only
  /// sometimes the same one, so SHIP-143 calls this each time." Whatever this device had registered
  /// is superseded, and so is a token that was live against **another** device session — a handset
  /// restored from someone else's backup — so a notification meant for the previous owner cannot
  /// reach the new one.
  ///
  /// It answers `201` every time, including for a token already registered: the resource created is
  /// the registration, and a client that retries after a dropped connection has created one either
  /// way.
  ///
  /// ## The `401` that must not be refreshed
  ///
  /// `notifications_no_device_session` is a `401` the ordinary answer is wrong for. There is
  /// nothing to register a token against, so refreshing and replaying — which is what the auth
  /// interceptor does with a `401` — loops against a session that will still not be there. The
  /// registrar branches on the **code** and stops.
  Future<DeviceToken> register({
    required String token,
    required DevicePlatform platform,
    required String idempotencyKey,
  });

  /// `DELETE /v1/notifications/device-tokens/current` — stop sending to this handset.
  ///
  /// ## `current` rather than a token in the path
  ///
  /// The client always knows which device it is without knowing which token is live, and putting a
  /// value that identifies somebody's handset into the path would put it into every access log and
  /// every proxy on the way, for no gain.
  ///
  /// ## It is a courtesy, and the contract says which guarantee actually holds
  ///
  /// "Signing out ends push delivery whether or not this is called" — the token is bound to the
  /// device session, nothing is addressed whose session has been revoked, and `POST /v1/auth/logout`
  /// commits that. This makes the record legible; it is not the control. That is why the whole call
  /// is fire-and-forget and why its failure changes nothing.
  ///
  /// It answers `204` whether or not anything was registered. Deregistering twice is not an error.
  ///
  /// ## [accessToken] is passed rather than picked up
  ///
  /// The same argument [SessionEnder] makes and the second caller of it: this is sent while the
  /// session is being discarded, and the interceptor reads the credential at request time rather
  /// than at call time — so a request that let the transport supply one would race the clear and
  /// usually lose.
  Future<void> deregister({
    required String accessToken,
    required String idempotencyKey,
  });
}

/// The real one, over [ApiClient].
final class ApiNotificationsRepository implements NotificationsRepository {
  const ApiNotificationsRepository(this._client, this._unauthenticated);

  /// For the registration, which happens inside a live session.
  final ApiClient _client;

  /// For the deregistration, which happens on the way out of one. See [NotificationsRepository].
  final ApiClient _unauthenticated;

  @override
  Future<DeviceToken> register({
    required String token,
    required DevicePlatform platform,
    required String idempotencyKey,
  }) async {
    return DeviceToken.fromJson(
      await _client.postJson(
        '/v1/notifications/device-tokens',
        idempotencyKey: idempotencyKey,
        body: <String, Object?>{'token': token, 'platform': platform.wire},
      ),
    );
  }

  @override
  Future<void> deregister({
    required String accessToken,
    required String idempotencyKey,
  }) {
    return _unauthenticated.deleteNoContent(
      '/v1/notifications/device-tokens/current',
      idempotencyKey: idempotencyKey,
      headers: <String, Object?>{ApiHeaders.bearer: 'Bearer $accessToken'},
    );
  }
}

/// The application's notifications repository.
///
/// **Two clients, and the split is the same one `sessionEnderProvider` makes.** Registering happens
/// inside a live session and travels through the interceptor like everything else; deregistering
/// happens as the session is being forgotten and carries the credential itself, on a transport that
/// holds none — because a `401` on the way out would otherwise start a refresh, which is a client
/// minting a session in order to end one.
final notificationsRepositoryProvider = Provider<NotificationsRepository>(
  (ref) => ApiNotificationsRepository(
    ref.watch(apiClientProvider),
    ref.watch(unauthenticatedApiClientProvider),
  ),
);
