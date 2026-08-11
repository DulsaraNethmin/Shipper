// SHIP-54 — "User can request and enter an OTP with resend throttling".
//
// Three claims, and the third is the one worth being careful about. The platform's own limits —
// one message a minute, five an hour, per account — are what actually stop somebody being billed
// for a hundred texts, and they are enforced whatever this screen does. The countdown is a
// courtesy on top of them, and its value is specific: without it a person taps four times because
// nothing appeared to happen, spends four of their five hourly messages inside one minute, and is
// then locked out of the only one that would have arrived.
//
// The platform will not say whether it sent anything — the same 202 and the same fixed interval
// for a known number, an unknown number, an already-verified number and one inside its cooldown —
// so nothing here asserts that a message went out. What is assertable is what the app asked for
// and when it let somebody ask again.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';

import 'fake_identity_repository.dart';
import 'signup_app.dart';

const _invalid = ApiErrorResponse(
  statusCode: 400,
  code: 'identity_otp_invalid',
  message: 'That code is not valid. Ask for a new one and try again.',
);

/// Walks the journey to the phone screen the way a person walks it.
Future<void> _reachPhoneScreen(WidgetTester tester, FakeIdentityRepository identity) async {
  await registerThrough(tester, identity);
  await verifyEmailThrough(tester);
}

bool _resendAvailable(WidgetTester tester) =>
    tester.widget<TextButton>(find.byKey(const Key('resend'))).onPressed != null;

void main() {
  group('requesting a code', () {
    testWidgets('the screen asks for one as it opens', (tester) async {
      // Registration issues the *email* token itself, inside the transaction that creates the
      // account (SHIP-31). It sends no OTP — a text costs money and wakes a handset — so
      // somebody arriving here has, by arriving, asked for one.
      final identity = FakeIdentityRepository();
      await _reachPhoneScreen(tester, identity);

      expect(identity.bodiesFor('request-otp').single, {'phone': '+61412345678'});
    });

    testWidgets('it asks against the number the platform normalised, not the typed one',
        (tester) async {
      // The form was filled with `0412 345 678`. Sending that back would make the client a
      // second normaliser, and two normalisers that disagree is a handset that cannot match its
      // own code.
      final identity = FakeIdentityRepository();
      await _reachPhoneScreen(tester, identity);

      expect(identity.bodiesFor('request-otp').single['phone'], '+61412345678');
      expect(find.textContaining('+61412345678'), findsOneWidget);
    });
  });

  group('entering a code', () {
    testWidgets('a correct code confirms the number and shows the verified state',
        (tester) async {
      final identity = FakeIdentityRepository();
      await _reachPhoneScreen(tester, identity);

      await tester.enterText(find.byKey(const Key('verify-phone-code')), '408213');
      await tester.tap(find.byKey(const Key('verify-phone-submit')));
      await tester.pumpAndSettle();

      // The number goes with the code, because the caller still has no session.
      expect(
        identity.bodiesFor('verify-phone').single,
        {'phone': '+61412345678', 'code': '408213'},
      );
      expect(find.byKey(const Key('verify-phone-verified')), findsOneWidget);
      expect(find.text('Mobile confirmed'), findsOneWidget);
    });

    testWidgets('the wrong shape is caught here rather than spent on a round trip',
        (tester) async {
      final identity = FakeIdentityRepository();
      await _reachPhoneScreen(tester, identity);

      await tester.enterText(find.byKey(const Key('verify-phone-code')), '4082');
      await tester.tap(find.byKey(const Key('verify-phone-submit')));
      await tester.pumpAndSettle();

      expect(find.text('The code is six digits.'), findsOneWidget);
      expect(identity.calls.where((c) => c.action == 'verify-phone'), isEmpty);
    });

    testWidgets('the field refuses anything that is not a digit', (tester) async {
      // A numeric keyboard is a suggestion; a hardware keyboard, a paste and an autofill are
      // not bound by it.
      final identity = FakeIdentityRepository();
      await _reachPhoneScreen(tester, identity);

      await tester.enterText(find.byKey(const Key('verify-phone-code')), '40a8-213999');
      await tester.pump();

      final field = tester.widget<TextField>(
        find.descendant(
          of: find.byKey(const Key('verify-phone-code')),
          matching: find.byType(TextField),
        ),
      );
      expect(field.controller?.text, '408213');
    });

    testWidgets('one refusal covers every way a code fails, and the screen adds nothing',
        (tester) async {
      // Wrong, expired, none outstanding, five guesses already used, or a number with no
      // account: the platform answers identity_otp_invalid for all of them, because six digits
      // is a guessable space and every distinction is help for whoever is guessing.
      final identity = FakeIdentityRepository()..failures['verify-phone'] = _invalid;
      await _reachPhoneScreen(tester, identity);

      await tester.enterText(find.byKey(const Key('verify-phone-code')), '000000');
      await tester.tap(find.byKey(const Key('verify-phone-submit')));
      await tester.pumpAndSettle();

      expect(find.text(_invalid.message), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
      expect(find.byKey(const Key('verify-phone-verified')), findsNothing);
    });

    testWidgets('a lost connection is a banner, not a verdict on the code', (tester) async {
      final identity = FakeIdentityRepository()
        ..failures['verify-phone'] = const ApiUnreachable();
      await _reachPhoneScreen(tester, identity);

      await tester.enterText(find.byKey(const Key('verify-phone-code')), '408213');
      await tester.tap(find.byKey(const Key('verify-phone-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.text(_invalid.message), findsNothing);
    });
  });

  group('resend throttling', () {
    testWidgets('the wait starts from the request the screen made on opening', (tester) async {
      final identity = FakeIdentityRepository()..retryAfter = const Duration(seconds: 60);
      await _reachPhoneScreen(tester, identity);

      expect(
        _resendAvailable(tester),
        isFalse,
        reason: 'a code was just requested, so the platform will not send another for a minute',
      );
      expect(find.textContaining('1:00'), findsOneWidget);
    });

    testWidgets('the countdown runs down and then lets the person ask again', (tester) async {
      final identity = FakeIdentityRepository()..retryAfter = const Duration(seconds: 60);
      await _reachPhoneScreen(tester, identity);

      await tester.pump(const Duration(seconds: 30));
      expect(_resendAvailable(tester), isFalse);
      expect(find.textContaining('0:30'), findsOneWidget);

      await tester.pump(const Duration(seconds: 31));
      expect(_resendAvailable(tester), isTrue);
    });

    testWidgets('a resend asks again and restarts the wait', (tester) async {
      final identity = FakeIdentityRepository()..retryAfter = const Duration(seconds: 60);
      await _reachPhoneScreen(tester, identity);
      await tester.pump(const Duration(seconds: 61));

      await tester.tap(find.byKey(const Key('resend')));
      await tester.pump();
      await tester.pump();

      expect(identity.keysFor('request-otp'), hasLength(2));
      expect(_resendAvailable(tester), isFalse);
    });

    testWidgets('each resend is a new action and carries a new idempotency key', (tester) async {
      // Identical bodies. Sharing a key would replay the first 202 and no second message would
      // ever be sent — which, from the handset, is indistinguishable from one running late.
      final identity = FakeIdentityRepository()..retryAfter = const Duration(seconds: 60);
      await _reachPhoneScreen(tester, identity);
      await tester.pump(const Duration(seconds: 61));

      await tester.tap(find.byKey(const Key('resend')));
      await tester.pump();
      await tester.pump(const Duration(seconds: 61));

      final keys = identity.keysFor('request-otp');
      expect(keys, hasLength(2));
      expect(keys.first, isNot(keys.last));
    });

    testWidgets('the platform deciding the interval is what the timer runs from',
        (tester) async {
      // Not a constant in the client. retry_after_seconds is a fixed interval rather than the
      // true remaining cooldown — the true one would disclose that the number has an account —
      // but it is still the platform's number to change without a client release.
      final identity = FakeIdentityRepository()..retryAfter = const Duration(seconds: 120);
      await _reachPhoneScreen(tester, identity);

      expect(find.textContaining('2:00'), findsOneWidget);

      await tester.pump(const Duration(seconds: 61));
      expect(_resendAvailable(tester), isFalse, reason: 'the platform asked for two minutes');

      await tester.pump(const Duration(seconds: 60));
      expect(_resendAvailable(tester), isTrue);
    });
  });

  testWidgets('the journey ends after both channels are confirmed', (tester) async {
    final identity = FakeIdentityRepository();
    await _reachPhoneScreen(tester, identity);

    await tester.enterText(find.byKey(const Key('verify-phone-code')), '408213');
    await tester.tap(find.byKey(const Key('verify-phone-submit')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('verify-phone-continue')));
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('registered-heading')), findsOneWidget);
    // Docs/04 §2's gate, as the platform reports it rather than as the client remembers it.
    expect(find.text('Verified'), findsNWidgets(2));
  });
}
