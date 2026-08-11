import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/features/identity/account.dart';

/// The five identity endpoints the signup journey calls (SHIP-51…54).
///
/// Every one of them is public and every one is state-changing, which is a combination the
/// platform allows only here: each is how a caller obtains their own credentials, and an
/// endpoint that takes a token sent to an address cannot require the session that proving the
/// address leads to (`contracts/paths/identity.yaml`).
///
/// An interface with one real implementation, following `TokenStore` rather than
/// `HealthRepository`. The reason is the same one: a widget test has to be able to hand a screen
/// something that answers, and the alternative — a stub transport under a concrete class — makes
/// every screen test a test of `dio`'s wiring as well as of the screen.
///
/// **Idempotency keys arrive from the caller and are never minted here.** `Docs/07` §4 has the
/// key generated where the user acts and reused across every retry of that action; a repository
/// that minted its own would mint one per attempt, which is the exact defect
/// `IdempotencyInterceptor` refuses to allow an interceptor to introduce. `ActionKey` is what
/// callers use.
abstract interface class IdentityRepository {
  /// `POST /v1/auth/register` (SHIP-30, SHIP-45).
  ///
  /// Creates an unverified account with the role it will keep. A duplicate email or mobile
  /// number is refused with `identity_email_taken` or `identity_phone_taken` — the one place
  /// this platform tells an unauthenticated caller whether an address is known, because the
  /// alternative leaves somebody who mistyped their address on a success screen.
  Future<Account> register({
    required String email,
    required String phone,
    required String password,
    required UserRole role,
    required String idempotencyKey,
  });

  /// `POST /v1/auth/verify-email` (SHIP-33).
  ///
  /// The token is the credential and it was sent to the address being proved, so no session is
  /// involved and no email address is sent alongside it. Presenting the same token twice
  /// answers `200`: that is somebody clicking a link twice, which is not an error.
  Future<Account> verifyEmail({
    required String token,
    required String idempotencyKey,
  });

  /// `POST /v1/auth/resend-verify` (SHIP-33).
  ///
  /// Answers `202` and the same body for every outcome — known address, unknown address,
  /// already verified, inside the cooldown. Returns how long to wait before asking again, which
  /// is a **fixed** interval and not the true remaining cooldown: the true one would say this
  /// address has been written to recently, which says it belongs to an account.
  Future<Duration> resendVerification({
    required String email,
    required String idempotencyKey,
  });

  /// `POST /v1/auth/request-otp` (SHIP-34).
  ///
  /// Six digits by SMS, expiring in ten minutes, and the same deliberately uninformative `202`
  /// as [resendVerification]. The returned interval is what SHIP-54's resend timer runs from.
  Future<Duration> requestOtp({
    required String phone,
    required String idempotencyKey,
  });

  /// `POST /v1/auth/verify-phone` (SHIP-36).
  ///
  /// Carries the number as well as the code, because the caller has no session yet. Every way a
  /// code fails answers one `identity_otp_invalid`: six digits is a guessable space, so each
  /// distinction would be information handed to whoever is guessing.
  Future<Account> verifyPhone({
    required String phone,
    required String code,
    required String idempotencyKey,
  });
}

/// The real one, over [ApiClient].
///
/// Thin by construction. `Docs/10` §8.1 replaces hand-written calls with a client generated from
/// `contracts/openapi.yaml`, and what should survive that is the shape of this interface and
/// nothing in this class.
final class ApiIdentityRepository implements IdentityRepository {
  const ApiIdentityRepository(this._client);

  final ApiClient _client;

  /// Product endpoints live under `/v1` (SHIP-13). The base URL carries the host and nothing
  /// else, so the version prefix belongs here.
  static const _base = '/v1/auth';

  @override
  Future<Account> register({
    required String email,
    required String phone,
    required String password,
    required UserRole role,
    required String idempotencyKey,
  }) async {
    return Account.fromJson(
      await _client.postJson(
        '$_base/register',
        idempotencyKey: idempotencyKey,
        body: registerBody(email: email, phone: phone, password: password, role: role),
      ),
    );
  }

  @override
  Future<Account> verifyEmail({
    required String token,
    required String idempotencyKey,
  }) async {
    return Account.fromJson(
      await _client.postJson(
        '$_base/verify-email',
        idempotencyKey: idempotencyKey,
        body: verifyEmailBody(token),
      ),
    );
  }

  @override
  Future<Duration> resendVerification({
    required String email,
    required String idempotencyKey,
  }) async {
    return _retryAfter(
      await _client.postJson(
        '$_base/resend-verify',
        idempotencyKey: idempotencyKey,
        body: resendVerificationBody(email),
      ),
    );
  }

  @override
  Future<Duration> requestOtp({
    required String phone,
    required String idempotencyKey,
  }) async {
    return _retryAfter(
      await _client.postJson(
        '$_base/request-otp',
        idempotencyKey: idempotencyKey,
        body: phoneBody(phone),
      ),
    );
  }

  @override
  Future<Account> verifyPhone({
    required String phone,
    required String code,
    required String idempotencyKey,
  }) async {
    return Account.fromJson(
      await _client.postJson(
        '$_base/verify-phone',
        idempotencyKey: idempotencyKey,
        body: verifyPhoneBody(phone: phone, code: code),
      ),
    );
  }

  /// How long before asking again, from the platform's answer.
  ///
  /// Falls back to a minute — the platform's own cooldown — when the field is missing or is not
  /// a number. A resend button that is enabled again immediately would be tapped, would send
  /// nothing, and would look broken; erring towards waiting costs a person one minute in a case
  /// that means the response was not the contract anyway.
  static Duration _retryAfter(Map<String, dynamic> body) {
    final Object? seconds = body['retry_after_seconds'];
    return Duration(seconds: seconds is int && seconds > 0 ? seconds : 60);
  }
}

/// The request bodies, built where a test can read them.
///
/// Separated from the calls above so that `identity_repository_test.dart` can assert the exact
/// keys against `contracts/paths/identity.yaml` without a transport. The platform **refuses
/// unknown fields** on these bodies — `httpx.DecodeJSON` is strict, so a client that sends
/// `phone_number` is told about the typo — which makes the key names part of the contract in a
/// way a response's are not.
Map<String, Object?> registerBody({
  required String email,
  required String phone,
  required String password,
  required UserRole role,
}) {
  return <String, Object?>{
    'email': email,
    'phone': phone,
    'password': password,
    'role': role.wireName,
  };
}

/// `POST /v1/auth/verify-email`.
Map<String, Object?> verifyEmailBody(String token) => <String, Object?>{'token': token};

/// `POST /v1/auth/resend-verify`.
Map<String, Object?> resendVerificationBody(String email) => <String, Object?>{'email': email};

/// `POST /v1/auth/request-otp`.
Map<String, Object?> phoneBody(String phone) => <String, Object?>{'phone': phone};

/// `POST /v1/auth/verify-phone`.
Map<String, Object?> verifyPhoneBody({required String phone, required String code}) {
  return <String, Object?>{'phone': phone, 'code': code};
}

/// The application's identity repository.
final identityRepositoryProvider = Provider<IdentityRepository>(
  (ref) => ApiIdentityRepository(ref.watch(apiClientProvider)),
);
