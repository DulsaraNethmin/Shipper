// SHIP-53 — "User can enter or deep-link a code and see verified state".
//
// Three claims: typed, deep-linked, and the verified state itself. The first two are two entry
// points into one verification, and the deep-linked one is the interesting half — a person who
// has clicked a link has already confirmed; a screen that pre-filled a field and waited for a
// second tap would be asking them to do it again.
//
// The deep link is exercised at the route, because that is where a link of either kind arrives:
// `shipper:///verify-email?token=…` today, an HTTPS universal and app link once SHIP-24…27 and a
// registered domain exist. Neither reaches Dart as anything but this route and this query
// parameter. What a widget test cannot cover is the native association itself — that the
// operating system hands the URL over at all — which is `Info.plist` and `AndroidManifest.xml`,
// and is demonstrated on a device with `xcrun simctl openurl`.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';

import 'fake_identity_repository.dart';
import 'signup_app.dart';

const _token = '9qE2vT7bYw1sJk4pNc0aRlX8oZgHdM3uQiV6yB5tCfE';

const _expired = ApiErrorResponse(
  statusCode: 400,
  code: 'identity_verification_token_expired',
  message: 'This verification link has expired. Ask for a new one.',
);

/// Cold-starts the app at [url], the way the operating system starts it for a link.
///
/// `defaultRouteName` is the channel a platform deep link arrives on, and go_router prefers it
/// over `initialLocation`, so this is the real path rather than a shortcut around it — including
/// the part that had to be fixed: the redirect runs first while the keychain is still being
/// read, and SHIP-49's guard sent every location that was not the splash to the splash.
///
/// Note what is *not* set up: no signup state, no account, no session. A link opened on a phone
/// that has restarted since registering arrives with a token and nothing else.
Future<void> _deepLink(WidgetTester tester, FakeIdentityRepository identity, String url) async {
  tester.platformDispatcher.defaultRouteNameTestValue = url;
  addTearDown(tester.platformDispatcher.clearDefaultRouteNameTestValue);

  await tester.pumpWidget(signupApp(identity));
  await tester.pumpAndSettle();
}

void main() {
  group('typed', () {
    testWidgets('registration lands here, because registration is what sent the message',
        (tester) async {
      final identity = FakeIdentityRepository();
      await registerThrough(tester, identity);

      expect(find.byKey(const Key('verify-email-token')), findsOneWidget);
      // The address is shown back, so "check your email" names which one.
      expect(find.textContaining('alice@example.com'), findsOneWidget);
    });

    testWidgets('a code confirms the address and shows the verified state', (tester) async {
      final identity = FakeIdentityRepository();
      await registerThrough(tester, identity);

      await tester.enterText(find.byKey(const Key('verify-email-token')), _token);
      await tester.tap(find.byKey(const Key('verify-email-submit')));
      await tester.pumpAndSettle();

      // The token is the whole request: no address alongside it, because the token *is* the
      // credential and it was sent to the address being proved.
      expect(identity.bodiesFor('verify-email').single, {'token': _token});
      expect(find.byKey(const Key('verify-email-verified')), findsOneWidget);
      expect(find.text('Email confirmed'), findsOneWidget);
    });

    testWidgets('an empty field never reaches the platform', (tester) async {
      final identity = FakeIdentityRepository();
      await registerThrough(tester, identity);

      await tester.tap(find.byKey(const Key('verify-email-submit')));
      await tester.pumpAndSettle();

      expect(find.text('Enter the code from your email.'), findsOneWidget);
      expect(identity.calls.where((c) => c.action == 'verify-email'), isEmpty);
    });
  });

  group('deep-linked', () {
    testWidgets('a token in the link verifies without being asked twice', (tester) async {
      final identity = FakeIdentityRepository();
      await _deepLink(tester, identity, '${Routes.verifyEmail}?token=$_token');

      expect(identity.bodiesFor('verify-email').single, {'token': _token});
      expect(find.byKey(const Key('verify-email-verified')), findsOneWidget);
    });

    testWidgets('the same link twice is not an error', (tester) async {
      // The platform answers 200 for a token presented again, provided it is the one that
      // verified the address — somebody clicking a link twice. The client must not turn that
      // into a failure of its own.
      final identity = FakeIdentityRepository()..account = anAccount(emailVerified: true);
      await _deepLink(tester, identity, '${Routes.verifyEmail}?token=$_token');

      expect(find.byKey(const Key('verify-email-verified')), findsOneWidget);
    });

    testWidgets('a link with no token waits for one rather than calling the platform',
        (tester) async {
      final identity = FakeIdentityRepository();
      await _deepLink(tester, identity, Routes.verifyEmail);

      expect(identity.calls, isEmpty);
      expect(find.byKey(const Key('verify-email-token')), findsOneWidget);
    });

    testWidgets('a link that arrives before the app knows whose it is still works',
        (tester) async {
      // The reason resend is disabled rather than hidden here: the app has a token and no
      // address, and the resend endpoint takes an address.
      final identity = FakeIdentityRepository();
      await _deepLink(tester, identity, '${Routes.verifyEmail}?token=$_token');

      expect(find.byKey(const Key('verify-email-verified')), findsOneWidget);
    });
  });

  group('the platform decides whether a token is usable', () {
    testWidgets('an expired token says so, under the field', (tester) async {
      final identity = FakeIdentityRepository()..failures['verify-email'] = _expired;
      await registerThrough(tester, identity);

      await tester.enterText(find.byKey(const Key('verify-email-token')), _token);
      await tester.tap(find.byKey(const Key('verify-email-submit')));
      await tester.pumpAndSettle();

      expect(find.text(_expired.message), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
      expect(find.byKey(const Key('verify-email-verified')), findsNothing);
    });

    testWidgets('a lost connection is a banner, not a verdict on the token', (tester) async {
      final identity = FakeIdentityRepository()
        ..failures['verify-email'] = const ApiUnreachable();
      await registerThrough(tester, identity);

      await tester.enterText(find.byKey(const Key('verify-email-token')), _token);
      await tester.tap(find.byKey(const Key('verify-email-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
    });
  });

  group('resend', () {
    testWidgets('asks for another message and then waits out the platform\'s interval',
        (tester) async {
      final identity = FakeIdentityRepository()..retryAfter = const Duration(seconds: 60);
      await registerThrough(tester, identity);

      await tester.tap(find.byKey(const Key('resend')));
      await tester.pump();
      await tester.pump();

      expect(identity.bodiesFor('resend-verify').single, {'email': 'alice@example.com'});
      expect(
        tester.widget<TextButton>(find.byKey(const Key('resend'))).onPressed,
        isNull,
        reason: 'the platform asked for a minute; tapping again inside it sends nothing',
      );

      await tester.pump(const Duration(seconds: 61));
      expect(tester.widget<TextButton>(find.byKey(const Key('resend'))).onPressed, isNotNull);
    });

    testWidgets('is disabled when the app has no address to send to', (tester) async {
      // Disabled rather than absent: Docs/07 §3 lets the app hide or disable, and an affordance
      // that vanishes reads as a missing feature.
      final identity = FakeIdentityRepository();
      await _deepLink(tester, identity, Routes.verifyEmail);

      expect(tester.widget<TextButton>(find.byKey(const Key('resend'))).onPressed, isNull);
    });
  });
}
