// What actually reaches the wire, checked against contracts/paths/identity.yaml.
//
// The screens are tested against a fake repository, which is the right seam for them and the
// wrong one for this: a fake cannot notice that the client posts `phone_number` where the
// platform reads `phone`. The platform **refuses unknown fields** on these bodies
// (`httpx.DecodeJSON` is strict), so a key name is not a detail — a wrong one is a 400 from a
// build already on somebody's phone.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/features/identity/identity_repository.dart';

class _StubAdapter implements HttpClientAdapter {
  _StubAdapter(this.respond);

  final ResponseBody Function(RequestOptions options) respond;
  final requests = <RequestOptions>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    return respond(options);
  }

  @override
  void close({bool force = false}) {}
}

ResponseBody _json(Object body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

({IdentityRepository repo, _StubAdapter adapter}) _repoReturning(Object body, {int status = 200}) {
  final adapter = _StubAdapter((_) => _json(body, status: status));
  final dio = buildDio(baseUrl: 'http://localhost:8092')..httpClientAdapter = adapter;
  return (repo: ApiIdentityRepository(ApiClient(dio)), adapter: adapter);
}

/// The `Account` example from the contract, verbatim.
const _account = <String, Object?>{
  'id': '0191f3c2-8a4d-7c31-9f52-3b7e1d4a6c88',
  'email': 'alice@example.com',
  'phone': '+61412345678',
  'role': 'customer',
  'status': 'active',
  'email_verified': false,
  'phone_verified': false,
  'created_at': '2026-08-11T04:11:52.418Z',
};

Map<String, Object?> _sentBody(RequestOptions options) {
  final Object? data = options.data;
  return data is Map<String, Object?> ? data : <String, Object?>{};
}

void main() {
  group('register', () {
    test('posts the contract body to the versioned path', () async {
      final (:repo, :adapter) = _repoReturning(_account, status: 201);

      await repo.register(
        email: 'alice@example.com',
        phone: '0412 345 678',
        password: 'correct-horse-battery-staple',
        role: UserRole.provider,
        idempotencyKey: 'key-1',
      );

      final sent = adapter.requests.single;
      expect(sent.method, 'POST');
      expect(sent.path, '/v1/auth/register');
      expect(sent.headers[ApiHeaders.idempotencyKey], 'key-1');
      expect(_sentBody(sent), {
        'email': 'alice@example.com',
        // Sent as typed. The platform normalises to E.164 and a client that normalised too
        // would be a second normaliser to disagree with.
        'phone': '0412 345 678',
        'password': 'correct-horse-battery-staple',
        'role': 'provider',
      });
    });

    test('decodes the account, including the role that drives the shell', () async {
      final (:repo, adapter: _) = _repoReturning(_account, status: 201);

      final account = await repo.register(
        email: 'alice@example.com',
        phone: '0412 345 678',
        password: 'correct-horse-battery-staple',
        role: UserRole.customer,
        idempotencyKey: 'key-1',
      );

      expect(account.id, '0191f3c2-8a4d-7c31-9f52-3b7e1d4a6c88');
      expect(account.phone, '+61412345678', reason: 'E.164, as the platform normalised it');
      expect(account.role, UserRole.customer);
      expect(account.emailVerified, isFalse);
      expect(account.phoneVerified, isFalse);
    });

    test('tolerates a field this build has never heard of', () async {
      // Docs/07 §6. The platform adds fields rather than repurposing them, and a client that
      // rejected what it did not recognise would turn every additive change into a release —
      // for builds that cannot be updated over the air at all.
      final (:repo, adapter: _) = _repoReturning(
        {..._account, 'referral_code': 'SPRING26'},
        status: 201,
      );

      final account = await repo.register(
        email: 'alice@example.com',
        phone: '0412 345 678',
        password: 'correct-horse-battery-staple',
        role: UserRole.customer,
        idempotencyKey: 'key-1',
      );

      expect(account.email, 'alice@example.com');
    });

    test('degrades an unrecognised role rather than throwing on it', () async {
      // A build on a phone outlives the contract that shipped with it. Crashing on a role this
      // version has never seen would leave that phone with no route forward at all; the shell
      // renders UserRole.unknown as "this build does not recognise your account type", which is
      // what SHIP-167's version gate exists to resolve.
      final (:repo, adapter: _) = _repoReturning(
        {..._account, 'role': 'broker'},
        status: 201,
      );

      final account = await repo.register(
        email: 'alice@example.com',
        phone: '0412 345 678',
        password: 'correct-horse-battery-staple',
        role: UserRole.customer,
        idempotencyKey: 'key-1',
      );

      expect(account.role, UserRole.unknown);
    });

    test('survives an account with no status and no created_at', () async {
      // Only what the app reads is required. A field the platform stops sending must degrade to
      // "not shown" rather than to a decode failure on every installed build.
      final trimmed = Map<String, Object?>.from(_account)
        ..remove('status')
        ..remove('created_at');
      final (:repo, adapter: _) = _repoReturning(trimmed, status: 201);

      final account = await repo.register(
        email: 'alice@example.com',
        phone: '0412 345 678',
        password: 'correct-horse-battery-staple',
        role: UserRole.customer,
        idempotencyKey: 'key-1',
      );

      expect(account.status, isNull);
      expect(account.createdAt, isNull);
    });
  });

  group('verify-email', () {
    test('sends the token and nothing else', () async {
      // No email address alongside it, deliberately: the token *is* the credential and it was
      // sent to the address being proved.
      final (:repo, :adapter) = _repoReturning({..._account, 'email_verified': true});

      final account = await repo.verifyEmail(token: 'a-token', idempotencyKey: 'key-2');

      final sent = adapter.requests.single;
      expect(sent.path, '/v1/auth/verify-email');
      expect(_sentBody(sent), {'token': 'a-token'});
      expect(account.emailVerified, isTrue);
    });
  });

  group('verify-phone', () {
    test('sends the number as well as the code, because there is no session', () async {
      final (:repo, :adapter) = _repoReturning({..._account, 'phone_verified': true});

      final account = await repo.verifyPhone(
        phone: '+61412345678',
        code: '408213',
        idempotencyKey: 'key-3',
      );

      final sent = adapter.requests.single;
      expect(sent.path, '/v1/auth/verify-phone');
      expect(_sentBody(sent), {'phone': '+61412345678', 'code': '408213'});
      expect(account.phoneVerified, isTrue);
    });
  });

  group('the endpoints that will not say whether they did anything', () {
    test('request-otp reads the fixed interval the resend timer runs from', () async {
      final (:repo, :adapter) = _repoReturning({'retry_after_seconds': 60}, status: 202);

      final wait = await repo.requestOtp(phone: '+61412345678', idempotencyKey: 'key-4');

      expect(adapter.requests.single.path, '/v1/auth/request-otp');
      expect(_sentBody(adapter.requests.single), {'phone': '+61412345678'});
      expect(wait, const Duration(seconds: 60));
    });

    test('resend-verify takes the address rather than the token', () async {
      final (:repo, :adapter) = _repoReturning({'retry_after_seconds': 90}, status: 202);

      final wait = await repo.resendVerification(
        email: 'alice@example.com',
        idempotencyKey: 'key-5',
      );

      expect(adapter.requests.single.path, '/v1/auth/resend-verify');
      expect(_sentBody(adapter.requests.single), {'email': 'alice@example.com'});
      expect(wait, const Duration(seconds: 90));
    });

    test('falls back to a minute when the interval is missing or nonsense', () async {
      // Erring towards waiting costs a person one minute in a case that means the response was
      // not the contract anyway. Erring the other way gives them a button that is enabled,
      // sends nothing, and looks broken.
      for (final body in <Map<String, Object?>>[
        <String, Object?>{},
        {'retry_after_seconds': 0},
        {'retry_after_seconds': 'soon'},
        {'retry_after_seconds': -5},
      ]) {
        final (:repo, adapter: _) = _repoReturning(body, status: 202);
        expect(
          await repo.requestOtp(phone: '+61412345678', idempotencyKey: 'key-6'),
          const Duration(seconds: 60),
          reason: '$body',
        );
      }
    });
  });
}
