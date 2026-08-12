import 'dart:async';

import 'package:shipper/core/auth/token_pair.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/features/identity/account.dart';
import 'package:shipper/features/identity/identity_repository.dart';

import '../../core/auth/session_fixtures.dart';

/// An account the platform could have returned.
///
/// Built from `contracts/paths/identity.yaml`'s own example so that a field renamed in the
/// contract shows up here rather than only on a device.
Account anAccount({
  String id = '0191f3c2-8a4d-7c31-9f52-3b7e1d4a6c88',
  String email = 'alice@example.com',
  String phone = '+61412345678',
  UserRole role = UserRole.customer,
  bool emailVerified = false,
  bool phoneVerified = false,
}) {
  return Account(
    id: id,
    email: email,
    phone: phone,
    role: role,
    emailVerified: emailVerified,
    phoneVerified: phoneVerified,
    status: 'active',
    createdAt: '2026-08-11T04:11:52.418Z',
  );
}

/// One recorded call.
typedef IdentityCall = ({String action, Map<String, Object?> body, String idempotencyKey});

/// An [IdentityRepository] that answers from a script and records what it was asked.
///
/// The screens are tested against this rather than against a stub transport, because a widget
/// test that also exercises `dio`'s wiring fails for two reasons and reads as one. What actually
/// reaches the wire is `identity_repository_test.dart`'s subject, against the contract.
///
/// It records the **idempotency key of every call**, which is what makes the rule in
/// `Docs/07` §4 assertable: a key reused across a retry, and a new one for a new action.
class FakeIdentityRepository implements IdentityRepository {
  final calls = <IdentityCall>[];

  /// The account every successful call answers with. Replace to change what a screen renders.
  Account account = anAccount();

  /// What a successful sign-in answers with. Its access token carries the role the shell reads,
  /// so replacing this is how a test signs somebody in as a provider.
  TokenPair tokens = aTokenPair();

  /// What the platform says to wait before asking again.
  Duration retryAfter = const Duration(seconds: 60);

  /// Set to make the next call of that action throw instead of answering.
  final failures = <String, Object>{};

  /// Set to hold the next call of that action open, so a test can assert what a screen shows
  /// while a request is in flight.
  final gates = <String, Completer<void>>{};

  /// Every idempotency key seen for [action], in order.
  List<String> keysFor(String action) =>
      calls.where((c) => c.action == action).map((c) => c.idempotencyKey).toList();

  /// Every body seen for [action], in order.
  List<Map<String, Object?>> bodiesFor(String action) =>
      calls.where((c) => c.action == action).map((c) => c.body).toList();

  Future<T> _record<T>(
    String action,
    Map<String, Object?> body,
    String idempotencyKey,
    T Function() answer,
  ) async {
    calls.add((action: action, body: body, idempotencyKey: idempotencyKey));

    final gate = gates.remove(action);
    if (gate != null) await gate.future;

    final failure = failures.remove(action);
    if (failure != null) throw failure;

    return answer();
  }

  @override
  Future<Account> register({
    required String email,
    required String phone,
    required String password,
    required UserRole role,
    required String idempotencyKey,
  }) {
    return _record(
      'register',
      registerBody(email: email, phone: phone, password: password, role: role),
      idempotencyKey,
      () => account = account.copyWith(email: email, role: role),
    );
  }

  @override
  Future<Account> verifyEmail({
    required String token,
    required String idempotencyKey,
  }) {
    return _record(
      'verify-email',
      verifyEmailBody(token),
      idempotencyKey,
      () => account = account.copyWith(emailVerified: true),
    );
  }

  @override
  Future<Duration> resendVerification({
    required String email,
    required String idempotencyKey,
  }) {
    return _record(
      'resend-verify',
      resendVerificationBody(email),
      idempotencyKey,
      () => retryAfter,
    );
  }

  @override
  Future<Duration> requestOtp({
    required String phone,
    required String idempotencyKey,
  }) {
    return _record('request-otp', phoneBody(phone), idempotencyKey, () => retryAfter);
  }

  @override
  Future<Account> verifyPhone({
    required String phone,
    required String code,
    required String idempotencyKey,
  }) {
    return _record(
      'verify-phone',
      verifyPhoneBody(phone: phone, code: code),
      idempotencyKey,
      () => account = account.copyWith(phoneVerified: true),
    );
  }

  @override
  Future<TokenPair> login({
    required String email,
    required String password,
    required String deviceLabel,
    required String idempotencyKey,
  }) {
    return _record(
      'login',
      loginBody(email: email, password: password, deviceLabel: deviceLabel),
      idempotencyKey,
      () => tokens,
    );
  }
}
