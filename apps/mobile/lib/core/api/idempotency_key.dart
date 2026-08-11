import 'dart:convert';
import 'dart:math';

import 'package:shipper/core/errors/api_failure.dart';

/// Mints a fresh idempotency key.
///
/// 128 bits from [Random.secure], hex encoded. Not a UUID, and deliberately not a new package
/// dependency for one: the platform accepts any string up to 255 characters
/// (`contracts/paths/identity.yaml`), and what the value has to be is unique and unguessable.
///
/// **Unguessable matters and is easy to miss.** The platform namespaces stored responses by
/// caller (SHIP-44), but the public endpoints — registration and the two verification confirms —
/// share the anonymous scope, where a stored response is protected by a request fingerprint
/// alone. A sequential or time-derived key would be a second thing worth guessing.
String newIdempotencyKey() {
  final random = Random.secure();
  final bytes = List<int>.generate(16, (_) => random.nextInt(256));
  return bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join();
}

/// One idempotency key per user action, held across a retry of that same action.
///
/// `Docs/07` §4 states the rule this class keeps: **the key is generated once where the user
/// acts and reused unchanged across every retry.** An interceptor that minted a fresh key per
/// attempt would look correct, pass every test that does not simulate a retry, and duplicate
/// records in the field — which is why `IdempotencyInterceptor` refuses to generate one at all.
///
/// The hard part is the other direction, and it is what this class actually exists for:
/// **reusing a key too eagerly is as wrong as minting one too often.**
///
/// - Tapping "Send another code" twice is two actions. Reusing the key would replay the first
///   response, so no second message would ever be sent — a resend button that silently does
///   nothing, which from the handset is indistinguishable from an SMS that has not arrived yet.
/// - Correcting a mistyped address and submitting again is a different request. The platform
///   fingerprints method, path and body, so the same key on a changed body is refused with
///   `idempotency_key_reused` rather than quietly replaying the wrong answer.
///
/// So a key is retained in exactly one circumstance: the previous attempt failed **without
/// saying whether the platform acted on it**, and the request now being sent is identical. That
/// is the case idempotency exists for — a dropped connection after the server committed — and it
/// is the only case where sending the same key again is what the user meant.
final class ActionKey {
  /// [mint] is injectable so a test can assert *which* attempts share a key without having to
  /// match random hex.
  ActionKey({String Function()? mint}) : _mint = mint ?? newIdempotencyKey;

  final String Function() _mint;

  String? _key;
  String? _fingerprint;

  /// The key to send with [body].
  ///
  /// A fresh key, unless [settled] was last given a failure that left the outcome unknown and
  /// [body] encodes to exactly what that attempt sent.
  String forRequest(Object? body) {
    final fingerprint = jsonEncode(body);
    if (_key == null || _fingerprint != fingerprint) {
      _key = _mint();
      _fingerprint = fingerprint;
    }
    return _key!;
  }

  /// Records how the attempt this key was minted for ended.
  ///
  /// Pass the failure, or `null` on success. Anything except a failure that leaves the outcome
  /// unknown retires the key, so the next call to [forRequest] starts a new action.
  void settled(Object? failure) {
    if (failure != null && outcomeUnknown(failure)) return;
    _key = null;
    _fingerprint = null;
  }

  /// Whether [failure] leaves it genuinely unknown whether the platform acted on the request.
  ///
  /// The set is small on purpose. A `409`, a `422` and a `400` all mean the platform saw the
  /// request and refused it, so retrying under the same key can only replay that refusal —
  /// which is precisely the case where the user has corrected something and wants a new answer.
  static bool outcomeUnknown(Object failure) {
    return switch (failure) {
      // The reply never came back. The request may or may not have arrived, which is the whole
      // reason the key exists.
      ApiUnreachable() => true,

      // The first request with this key is still being handled. Retrying is correct and is
      // already what the client was doing; a *different* key would start a second one.
      ApiErrorResponse(code: 'idempotency_request_in_progress') => true,

      // 5xx and 503: the platform may well have committed before it failed to answer.
      ApiErrorResponse(:final statusCode) => statusCode >= 500,

      // A body that was not the contract at all, from a proxy or a broken gateway. Nothing in
      // it says whether the request reached the service.
      ApiMalformedResponse() => true,

      _ => false,
    };
  }
}
