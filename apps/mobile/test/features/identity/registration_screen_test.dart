// SHIP-51 — "A new account can be created from the app with inline validation".
//
// Two halves, because the *Done when* line has two.
//
// The first is the journey: the app boots signed out, a person reaches the form the way a person
// reaches it, fills it, and the account the platform creates is what the app then carries. A
// test that pumped the screen on its own would pass with the guard still bouncing every
// signed-out location to the sign-in shell, which is exactly the defect SHIP-51 had to fix.
//
// The second is *whose* validation. Both sources render under the same inputs, and the ones the
// platform sends are the ones that matter — they are what lets a limit move server-side without
// a release, on builds that have no over-the-air path at all.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';

import 'fake_identity_repository.dart';
import 'signup_app.dart';

/// The platform refusing three fields at once, in the shape `internal/validate` produces.
const _validationFailed = ApiErrorResponse(
  statusCode: 422,
  code: 'validation_failed',
  message: 'The request could not be accepted.',
  requestId: '9f2c1b',
  details: [
    ApiFieldError(field: 'password', code: 'too_short', message: 'Use at least 14 characters.'),
  ],
);

bool _submitEnabled(WidgetTester tester) =>
    tester.widget<FilledButton>(find.byKey(const Key('register-submit'))).onPressed != null;

void main() {
  group('the journey', () {
    testWidgets('a signed-out person can reach the form from the shell', (tester) async {
      final identity = FakeIdentityRepository();
      await tester.pumpWidget(signupApp(identity));
      await tester.pumpAndSettle();

      // Sign-in is a working form from SHIP-55 — the signed-out shell and the sign-in screen
      // are one screen, and registration is the other way out of it.
      expect(
        tester.widget<FilledButton>(find.byKey(const Key('sign-in'))).onPressed,
        isNotNull,
      );

      await tester.tap(find.byKey(const Key('create-account')));
      await tester.pumpAndSettle();

      // Signup starts at the role, because the platform fixes it at registration (SHIP-45).
      expect(find.byKey(const Key('role-customer')), findsOneWidget);

      await tester.tap(find.byKey(const Key('role-continue')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('register-email')), findsOneWidget);
    });

    testWidgets('a filled form creates an account and carries it forward', (tester) async {
      final identity = FakeIdentityRepository();
      await openRegistration(tester, identity);
      await fillRegistration(tester);

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(identity.bodiesFor('register').single, {
        'email': 'alice@example.com',
        'phone': '0412 345 678',
        'password': 'correct-horse-battery-staple',
        'role': 'customer',
      });

      // The journey continues, still signed out — registering is not signing in, and the
      // platform returns no token. The next step is confirming the address registration just
      // sent a message to.
      expect(find.byKey(const Key('verify-email-token')), findsOneWidget);
      expect(find.textContaining('alice@example.com'), findsOneWidget);
      expect(find.byKey(const Key('shell-signed-in')), findsNothing);
    });

    testWidgets('the address is trimmed before it is sent', (tester) async {
      // A phone keyboard adds a trailing space often enough that leaving it in would produce a
      // steady trickle of "valid email address" refusals nobody can see the cause of.
      final identity = FakeIdentityRepository();
      await openRegistration(tester, identity);
      await fillRegistration(tester, email: '  alice@example.com  ');

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(identity.bodiesFor('register').single['email'], 'alice@example.com');
    });

    testWidgets('the journey ends where it honestly can, and says so', (tester) async {
      final identity = FakeIdentityRepository();
      await registerThrough(tester, identity);
      await verifyEmailThrough(tester);
      await verifyPhoneThrough(tester);

      // There is no sign-in endpoint on the platform yet (SHIP-41, consumed by SHIP-55), so the
      // screen says that rather than offering a button that cannot work.
      expect(find.byKey(const Key('registered-next')), findsOneWidget);

      await tester.tap(find.byKey(const Key('registered-done')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-signed-out')), findsOneWidget);
    });
  });

  group('this device pre-validates', () {
    testWidgets('an empty form never reaches the platform', (tester) async {
      final identity = FakeIdentityRepository();
      await openRegistration(tester, identity);

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(find.text('Enter your email address.'), findsOneWidget);
      expect(find.text('Enter your mobile number.'), findsOneWidget);
      expect(find.text('Choose a password.'), findsOneWidget);
      expect(
        identity.calls,
        isEmpty,
        reason: 'a round trip to be told a field is blank is the round trip this saves',
      );
    });

    testWidgets('a malformed address and a short password are caught here', (tester) async {
      final identity = FakeIdentityRepository();
      await openRegistration(tester, identity);
      await fillRegistration(tester, email: 'alice', password: 'short');

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(find.text('Enter a valid email address.'), findsOneWidget);
      expect(find.text('Use at least 10 characters.'), findsOneWidget);
      expect(identity.calls, isEmpty);
    });
  });

  group('the platform decides', () {
    testWidgets('its field messages land under the inputs they name', (tester) async {
      final identity = FakeIdentityRepository()..failures['register'] = _validationFailed;
      await openRegistration(tester, identity);
      await fillRegistration(tester);

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      // The device's own check passed this password — ten characters — and the platform's did
      // not. This is the case that makes a limit changeable server-side: a build with the old
      // number still shows the new one, because the message came from the platform.
      expect(find.text('Use at least 14 characters.'), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
      expect(find.byKey(const Key('registered-heading')), findsNothing);
    });

    testWidgets('a duplicate address is shown on the address, not in a banner', (tester) async {
      // The platform sends no `details` for a 409, so this mapping is a branch on `code` —
      // never on `message`, which is copy and gets reworded without a client release.
      final identity = FakeIdentityRepository()
        ..failures['register'] = const ApiErrorResponse(
          statusCode: 409,
          code: 'identity_email_taken',
          message: 'An account already exists for this email address.',
        );
      await openRegistration(tester, identity);
      await fillRegistration(tester);

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(find.text('An account already exists for this email address.'), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
    });

    testWidgets('correcting the field clears what the platform said about it', (tester) async {
      // A server message that outlives the value it was about is worse than none: the person
      // corrects the address and the form still says it is taken.
      final identity = FakeIdentityRepository()
        ..failures['register'] = const ApiErrorResponse(
          statusCode: 409,
          code: 'identity_email_taken',
          message: 'An account already exists for this email address.',
        );
      await openRegistration(tester, identity);
      await fillRegistration(tester);
      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      await tester.enterText(find.byKey(const Key('register-email')), 'alice2@example.com');
      await tester.pumpAndSettle();

      expect(find.text('An account already exists for this email address.'), findsNothing);
    });

    testWidgets('a failure about no particular field goes in the banner', (tester) async {
      final identity = FakeIdentityRepository()..failures['register'] = const ApiUnreachable();
      await openRegistration(tester, identity);
      await fillRegistration(tester);

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.text('No connection. Check your signal and try again.'), findsOneWidget);
    });

    testWidgets('the banner carries the request ID, because a screenshot loses a header',
        (tester) async {
      final identity = FakeIdentityRepository()
        ..failures['register'] = const ApiErrorResponse(
          statusCode: 503,
          code: 'service_unavailable',
          message: 'This cannot be completed right now. Try again shortly.',
          requestId: '9f2c1b',
        );
      await openRegistration(tester, identity);
      await fillRegistration(tester);

      await tester.tap(find.byKey(const Key('register-submit')));
      await tester.pumpAndSettle();

      expect(find.text('Reference 9f2c1b'), findsOneWidget);
    });
  });

  testWidgets('the submit is disabled while the request is in flight', (tester) async {
    // Two taps are two actions, so the idempotency key deliberately does not cover this — the
    // second tap would mint its own and create a second account.
    final identity = FakeIdentityRepository();
    final gate = Completer<void>();
    identity.gates['register'] = gate;

    await openRegistration(tester, identity);
    await fillRegistration(tester);
    expect(_submitEnabled(tester), isTrue);

    await tester.tap(find.byKey(const Key('register-submit')));
    await tester.pump();

    expect(_submitEnabled(tester), isFalse);
    expect(find.byType(CircularProgressIndicator), findsOneWidget);

    gate.complete();
    await tester.pumpAndSettle();

    expect(identity.calls, hasLength(1));
  });
}
