// SHIP-55 — "An existing user can sign in and lands in the correct role shell".
//
// Driven through the real application: the real router and its guard, the real session, the real
// shell. Only the socket and the Keychain are substituted, because a widget test has neither.
// That is what makes "lands in the correct role shell" a demonstration rather than an assertion
// about a widget — the role travels from the platform's access token, through the session, into
// the guard's redirect, and out as a different screen.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';

import '../../core/auth/fake_token_store.dart';
import '../../core/auth/session_fixtures.dart';
import 'fake_identity_repository.dart';
import 'signup_app.dart';

/// The platform's answer to a wrong password, and to an address with no account. One code for
/// both, deliberately (`contracts/paths/identity.yaml`).
const _credentialsInvalid = ApiErrorResponse(
  statusCode: 400,
  code: 'identity_credentials_invalid',
  message: 'Check your email address and password.',
  requestId: '9f2c1b',
);

Future<FakeIdentityRepository> _open(
  WidgetTester tester, {
  FakeIdentityRepository? identity,
  FakeTokenStore? store,
}) async {
  final repository = identity ?? FakeIdentityRepository();
  await tester.pumpWidget(signupApp(repository, store: store));
  await tester.pumpAndSettle();
  return repository;
}

void main() {
  group('an existing user signs in', () {
    testWidgets('and a customer lands in the customer half', (tester) async {
      final identity = await _open(
        tester,
        identity: FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.customer),
      );

      await signInThrough(tester);

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
      expect(find.byKey(const Key('shell-provider')), findsNothing);
      expect(identity.keysFor('login'), hasLength(1));
    });

    testWidgets('and a provider lands in the provider half', (tester) async {
      await _open(
        tester,
        identity: FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.provider),
      );

      await signInThrough(tester);

      expect(find.byKey(const Key('shell-provider')), findsOneWidget);
      expect(find.byKey(const Key('shell-customer')), findsNothing);
    });

    testWidgets('the refresh token reaches the keychain and the access token does not',
        (tester) async {
      // Docs/07 §3: the refresh token goes to the Keychain or the Keystore, the access token is
      // held in memory and reaches no disk at all. One write, and it is the refresh token.
      final store = FakeTokenStore();
      await _open(
        tester,
        identity: FakeIdentityRepository()..tokens = aTokenPair(refreshToken: 'refresh-abc'),
        store: store,
      );

      await signInThrough(tester);

      expect(store.written, ['refresh-abc']);
    });

    testWidgets('sends the contract body, with a device label somebody could act on',
        (tester) async {
      // device_label is display text for GET /v1/auth/sessions (SHIP-46). A person deciding
      // which device to revoke cannot act on four rows reading "Unknown device", which is why
      // the platform requires the field and why this client derives one rather than omitting it.
      final identity = await _open(tester);

      await signInThrough(tester, email: 'alice@example.com', password: 'a-real-password');

      expect(identity.bodiesFor('login').single, {
        'email': 'alice@example.com',
        'password': 'a-real-password',
        'device_label': testDeviceLabel,
      });
    });

    testWidgets('nothing navigates: the session is what moves the app', (tester) async {
      // The screen returns to no route and pushes none. Two mechanisms deciding where a
      // signed-in user goes would disagree the first time somebody signed in from anywhere else.
      await _open(tester, identity: FakeIdentityRepository()..tokens = aTokenPair());

      await signInThrough(tester);

      final container = ProviderScope.containerOf(
        tester.element(find.byType(MaterialApp)),
        listen: false,
      );
      expect(container.read(sessionProvider), isA<SessionSignedIn>());
      expect(find.byKey(const Key('sign-in')), findsNothing);
    });
  });

  group('a refusal', () {
    testWidgets('says one thing for a wrong password and for an unknown address', (tester) async {
      // The platform answers one code for both so that an unauthenticated endpoint is not an
      // account-existence oracle. A client that put the message under the password field would
      // undo that — it would say the address was recognised.
      await _open(tester, identity: FakeIdentityRepository()..failures['login'] = _credentialsInvalid);

      await signInThrough(tester);

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.text('Check your email address and password.'), findsOneWidget);
      expect(find.byKey(const Key('shell-signed-in')), findsNothing);
    });

    testWidgets('shows the request ID, because a screenshot does not carry a header',
        (tester) async {
      await _open(tester, identity: FakeIdentityRepository()..failures['login'] = _credentialsInvalid);

      await signInThrough(tester);

      expect(find.text('Reference 9f2c1b'), findsOneWidget);
    });

    testWidgets('a suspended account is told so, and stays on the screen', (tester) async {
      // The one place account standing is disclosed, and it is safe here: it is said after the
      // password verified, so the caller has just proved they own the account (SHIP-41).
      await _open(
        tester,
        identity: FakeIdentityRepository()
          ..failures['login'] = const ApiErrorResponse(
            statusCode: 403,
            code: 'identity_account_suspended',
            message: 'This account is suspended. Contact support.',
          ),
      );

      await signInThrough(tester);

      expect(find.text('This account is suspended. Contact support.'), findsOneWidget);
      expect(find.byKey(const Key('sign-in')), findsOneWidget);
    });

    testWidgets('a lost connection is a banner, not a verdict on the password', (tester) async {
      await _open(tester, identity: FakeIdentityRepository()..failures['login'] = const ApiUnreachable());

      await signInThrough(tester);

      expect(find.text('No connection. Check your signal and try again.'), findsOneWidget);
    });

    testWidgets('retrying after a lost connection reuses the key, so no second device appears',
        (tester) async {
      // contracts/paths/identity.yaml: each sign-in creates a device session, because there is
      // no device identifier to match on. A dropped connection during sign-in is exactly what
      // would leave somebody with two — the platform may have created the first and lost the
      // reply. The same key replays its stored answer instead.
      final identity = await _open(
        tester,
        identity: FakeIdentityRepository()..failures['login'] = const ApiUnreachable(),
      );

      await signInThrough(tester);
      identity.failures['login'] = const ApiUnreachable();
      await signInThrough(tester);

      expect(identity.keysFor('login'), hasLength(2));
      expect(identity.keysFor('login').first, identity.keysFor('login').last);
    });

    testWidgets('a refusal retires the key, so trying again is genuinely tried again',
        (tester) async {
      // The other direction, and the one that is easy to get wrong. The platform saw this
      // attempt and refused it; a second tap under the same key would replay that refusal rather
      // than checking anything. The body is identical on both attempts precisely so the assertion
      // is about the key being retired and not about the fingerprint changing.
      final identity = await _open(
        tester,
        identity: FakeIdentityRepository()..failures['login'] = _credentialsInvalid,
      );

      await signInThrough(tester, password: 'the-same-one');
      identity.failures['login'] = _credentialsInvalid;
      await signInThrough(tester, password: 'the-same-one');

      expect(identity.bodiesFor('login').first, identity.bodiesFor('login').last);
      expect(identity.keysFor('login'), hasLength(2));
      expect(identity.keysFor('login').first, isNot(identity.keysFor('login').last));
    });
  });

  group('this device pre-validates', () {
    testWidgets('an empty form never reaches the platform', (tester) async {
      final identity = await _open(tester);

      await tester.tap(find.byKey(const Key('sign-in')));
      await tester.pumpAndSettle();

      expect(find.text('Enter your email address.'), findsOneWidget);
      expect(find.text('Enter your password.'), findsOneWidget);
      expect(identity.calls, isEmpty);
    });

    testWidgets('a short password is not refused here, because it may predate the floor',
        (tester) async {
      // Validators.password enforces the ten-character minimum somebody must *choose*. Applying
      // it to a password being presented would refuse an existing account locally, without the
      // platform that would have accepted it ever being asked.
      final identity = await _open(tester);

      await signInThrough(tester, password: 'short');

      expect(identity.bodiesFor('login').single['password'], 'short');
    });
  });

  testWidgets('the end of signup arrives with the address already filled in', (tester) async {
    // One field to type rather than two. The password is deliberately not carried across —
    // a plaintext password in the provider tree is one crash report away from being somewhere it
    // must never be.
    final identity = FakeIdentityRepository();
    await registerThrough(tester, identity, role: UserRole.customer);
    await verifyEmailThrough(tester);
    await verifyPhoneThrough(tester);

    await tester.tap(find.byKey(const Key('registered-done')));
    await tester.pumpAndSettle();

    expect(find.text('alice@example.com'), findsOneWidget);
    expect(find.text('correct-horse-battery-staple'), findsNothing);
  });
}
