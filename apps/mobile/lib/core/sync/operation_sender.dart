import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/queue/queued_operation.dart';

/// What the sync worker needs of the network, and nothing more (SHIP-125).
///
/// **Declared here, by the consumer**, which is the rule `CLAUDE.md` states for the Go service
/// and `auth_interceptor.dart` already follows on this side: one method, named by what the worker
/// does rather than by how it is done. The worker therefore has no opinion about `dio`, about
/// authentication, or about how a proof photograph will eventually be uploaded, and a test hands
/// over a scripted implementation instead of a stubbed transport.
abstract interface class OperationSender {
  /// Offers [operation] to the platform, returning normally when it was accepted.
  ///
  /// Throws `ApiFailure` when it was not — the worker reads which kind through `decideFrom`.
  /// **Anything else thrown is treated as this build being unable to send the operation at all**,
  /// so an implementation must not raise a bare error for an ordinary network problem.
  ///
  /// The operation's `idempotencyKey` goes on the wire unchanged, on this attempt and on every
  /// later one. That is the whole of `Docs/07` §4's rule, and the reason the key is a stored
  /// column rather than something minted per attempt.
  Future<void> send(QueuedOperation operation);
}

/// The application's sender, over [ApiClient].
///
/// It carries the session's bearer token and the refresh-and-replay behaviour, because it uses
/// the same client every screen does — which is what makes a `401` mid-drain one refresh rather
/// than a refusal (SHIP-50).
final class ApiOperationSender implements OperationSender {
  const ApiOperationSender(this._client);

  final ApiClient _client;

  @override
  Future<void> send(QueuedOperation operation) async {
    // `Docs/07` §4 uploads a proof photograph as a local file "on reconnection, **not** as part of
    // the milestone request", so it is a multipart send that this build has no code for. Nothing
    // can enqueue one yet — SHIP-130 is the ticket that captures and queues an image, and it comes
    // after SHIP-129 — so the case is unreachable today and this is here to make sure it stays
    // loud if it ever is not. The worker quarantines it rather than retrying it, which keeps it
    // counted and visible instead of looping against a request it cannot construct.
    if (operation.attachmentPath != null) {
      throw UnimplementedError(
        'Operation ${operation.id} carries an attachment and this build has no upload for it. '
        'SHIP-130 adds the multipart send.',
      );
    }

    await _client.send(
      operation.method,
      operation.path,
      idempotencyKey: operation.idempotencyKey,
      body: operation.body,
    );
  }
}

/// The application's sender.
final operationSenderProvider = Provider<OperationSender>((ref) {
  return ApiOperationSender(ref.watch(apiClientProvider));
});
