// The signup journey's state, and the invariant CLAUDE.md names:
// "every state-changing endpoint accepts an idempotency key ... retries must not duplicate".
//
// ActionKey's own test covers the rule in the abstract. This one covers it where it is actually
// wired, because a controller that minted a key per call would satisfy every test in that file
// and still create two accounts on a dropped connection.

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/identity/identity_repository.dart';
import 'package:shipper/features/identity/signup_controller.dart';

import 'fake_identity_repository.dart';

ProviderContainer _container(FakeIdentityRepository identity) {
  final container = ProviderContainer(
    overrides: [identityRepositoryProvider.overrideWithValue(identity)],
  );
  addTearDown(container.dispose);
  return container;
}

Future<void> _register(ProviderContainer container, {String email = 'alice@example.com'}) async {
  await container.read(signupProvider.notifier).register(
        name: 'Alice Nguyen',
        email: email,
        phone: '0412 345 678',
        password: 'correct-horse-battery-staple',
      );
}

void main() {
  group('registration', () {
    test('carries the account forward as the platform returned it', () async {
      final identity = FakeIdentityRepository();
      final container = _container(identity);

      await _register(container);

      final state = container.read(signupProvider);
      expect(state.account, isNotNull);
      expect(state.email, 'alice@example.com');
      expect(state.phone, '+61412345678', reason: 'E.164, so the OTP endpoints echo it back');
      expect(state.emailVerified, isFalse);
      expect(state.busy, isFalse);
    });

    test('a refusal leaves the failure on the state and no account', () async {
      final identity = FakeIdentityRepository()
        ..failures['register'] = const ApiErrorResponse(
          statusCode: 409,
          code: 'identity_email_taken',
          message: 'taken',
        );
      final container = _container(identity);

      await _register(container);

      expect(container.read(signupProvider).account, isNull);
      expect(container.read(signupProvider).failure, isA<ApiErrorResponse>());
      expect(container.read(signupProvider).busy, isFalse);
    });

    test('a response that is not the contract is a malformed response, not a lost connection',
        () async {
      // Blaming the signal for a decode failure sends somebody to look at their reception for a
      // defect in the client.
      final identity = FakeIdentityRepository()
        ..failures['register'] = const FormatException('not an account');
      final container = _container(identity);

      await _register(container);

      expect(container.read(signupProvider).failure, isA<ApiMalformedResponse>());
    });
  });

  group('idempotency keys', () {
    test('a retry after a dropped connection sends the same key', () async {
      final identity = FakeIdentityRepository()..failures['register'] = const ApiUnreachable();
      final container = _container(identity);

      await _register(container);
      await _register(container);

      final keys = identity.keysFor('register');
      expect(keys, hasLength(2));
      expect(
        keys.first,
        keys.last,
        reason: 'the platform may already have created the account; the key is what makes it '
            'answer with what it did rather than doing it again',
      );
    });

    test('a corrected address sends a new key', () async {
      // The platform fingerprints method, path and body, so the same key on a changed body is
      // refused with idempotency_key_reused — which would strand somebody who mistyped their
      // address behind an error they cannot clear.
      final identity = FakeIdentityRepository()..failures['register'] = const ApiUnreachable();
      final container = _container(identity);

      await _register(container);
      await _register(container, email: 'alice2@example.com');

      final keys = identity.keysFor('register');
      expect(keys.first, isNot(keys.last));
    });

    test('a second resend is a second action and sends a new key', () async {
      // Identical bodies, and they must not share a key: replaying the first 202 would mean no
      // second message is ever sent, which from the handset looks like an SMS running late.
      final identity = FakeIdentityRepository();
      final container = _container(identity);
      await _register(container);

      await container.read(signupProvider.notifier).resendVerification();
      await container.read(signupProvider.notifier).resendVerification();

      final keys = identity.keysFor('resend-verify');
      expect(keys, hasLength(2));
      expect(keys.first, isNot(keys.last));
    });

    test('two different actions never share a key', () async {
      final identity = FakeIdentityRepository();
      final container = _container(identity);
      await _register(container);

      await container.read(signupProvider.notifier).requestOtp();
      await container.read(signupProvider.notifier).verifyPhone('408213');

      final used = identity.calls.map((c) => c.idempotencyKey).toSet();
      expect(used, hasLength(identity.calls.length));
    });
  });

  group('the endpoints that need contact details the app may not have', () {
    test('resend and OTP do nothing before there is an account', () async {
      // The deep-link case: somebody opens a verification link on a phone that has restarted
      // since registering, so the app holds a token and knows nothing else. Guessing an address
      // would be worse than asking.
      final identity = FakeIdentityRepository();
      final container = _container(identity);

      expect(await container.read(signupProvider.notifier).resendVerification(), isNull);
      expect(await container.read(signupProvider.notifier).requestOtp(), isNull);
      expect(await container.read(signupProvider.notifier).verifyPhone('408213'), isNull);
      expect(identity.calls, isEmpty);
    });

    test('verifying an email works with no account, because the token is the credential',
        () async {
      final identity = FakeIdentityRepository();
      final container = _container(identity);

      final account = await container.read(signupProvider.notifier).verifyEmail('a-token');

      expect(account, isNotNull);
      expect(identity.bodiesFor('verify-email').single, {'token': 'a-token'});
      // Verifying is also what recovers the rest of the journey: the platform returns the
      // account, so the number to send an OTP to arrives with it.
      expect(container.read(signupProvider).phone, '+61412345678');
    });
  });

  group('the role', () {
    test('is what registration sends', () async {
      final identity = FakeIdentityRepository();
      final container = _container(identity);

      container.read(signupProvider.notifier).chooseRole(UserRole.provider);
      await _register(container);

      expect(identity.bodiesFor('register').single['role'], 'provider');
    });

    test('cannot be set to one the platform never issues', () async {
      // UserRole.unknown exists so a build can decode a role it has never heard of without
      // crashing. Sending it back would be a registration the platform refuses, for a value no
      // person chose.
      final identity = FakeIdentityRepository();
      final container = _container(identity);

      container.read(signupProvider.notifier).chooseRole(UserRole.unknown);

      expect(container.read(signupProvider).role, UserRole.customer);
    });
  });

  test('reset abandons the journey, keys and all', () async {
    // The first attempt fails without saying whether the platform acted, which is the one case
    // that retains a key. Reset has to drop it anyway: a second signup on this device is not a
    // retry of the first, and reusing the key would replay whatever the first one did.
    final identity = FakeIdentityRepository()..failures['register'] = const ApiUnreachable();
    final container = _container(identity);
    await _register(container);

    container.read(signupProvider.notifier).reset();
    expect(container.read(signupProvider).account, isNull);
    expect(container.read(signupProvider).failure, isNull);

    await _register(container);
    expect(identity.keysFor('register').first, isNot(identity.keysFor('register').last));
  });
}
