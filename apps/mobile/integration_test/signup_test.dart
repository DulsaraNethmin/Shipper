// SHIP-51 to SHIP-54, on a device and against a running platform.
//
// Everything under test/ substitutes the identity repository, which is the right seam for a
// screen test and is not the same claim as "a new account can be created from the app". This
// file makes that claim: the production widget tree, the production `dio` client, real HTTP to
// a real service, a real PostgreSQL row at the end of it.
//
// # Running it
//
// It needs a booted simulator *and* the stack and API this worktree runs, so it is deliberately
// out of `make flutter-check` and out of CHECKS — the Flutter CI job is a Linux runner with no
// simulator and no service. From the repository root:
//
//     make up && make migrate-up && make run          # the API on this worktree's HTTP_PORT
//     cd apps/mobile && flutter test integration_test/signup_test.dart \
//       -d <device> \
//       --dart-define=SHIPPER_API_PORT=<HTTP_PORT> \
//       --dart-define=SHIPPER_DEMO_EMAIL=<a fresh address> \
//       --dart-define=SHIPPER_DEMO_PHONE=<a fresh mobile number> \
//       --plain-name "creates an account"
//
// # Why it takes the codes from outside rather than reading them
//
// The verification token and the OTP exist in exactly one place a developer can reach: the
// service log, where the console email and SMS adapters write them (SHIP-32, SHIP-35). That is
// deliberate — they are the credential — and it means no automated run can obtain them for
// itself. So the journey is demonstrated in two invocations with a look at the log between:
//
//   1. `--plain-name "creates an account"` registers through the form. The service logs the
//      verification token as it sends the email.
//   2. Read the token out of the log, ask for an OTP, read that too:
//
//        curl -s -X POST localhost:<port>/v1/auth/request-otp \
//          -H 'Idempotency-Key: demo-otp-1' -H 'Content-Type: application/json' \
//          -d '{"phone":"<the number>"}'
//
//   3. `--plain-name "confirms both contact details"` with `SHIPPER_DEMO_TOKEN` and
//      `SHIPPER_DEMO_OTP` finishes the journey — deep link, verified state, code, verified
//      state, and the end of signup.
//
// The second invocation deep-links rather than continuing, because a second `flutter test` is a
// new process with no signup state — which is also the case the screen has to survive in the
// field, and so is worth exercising rather than working around.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/routing/app_router.dart';

/// A fresh address and number per run, supplied from outside.
///
/// `uq_users_email` and `uq_users_phone` refuse a second account for either, which is the
/// platform behaving correctly and would look like a broken test. Passing them in keeps the run
/// repeatable without the test inventing values that have to be unique.
const _email = String.fromEnvironment('SHIPPER_DEMO_EMAIL', defaultValue: '');
const _phone = String.fromEnvironment('SHIPPER_DEMO_PHONE', defaultValue: '');
const _token = String.fromEnvironment('SHIPPER_DEMO_TOKEN', defaultValue: '');
const _otp = String.fromEnvironment('SHIPPER_DEMO_OTP', defaultValue: '');

const _password = 'correct-horse-battery-staple';

/// Pumps until [finder] matches, or fails.
///
/// **`pumpAndSettle` is the wrong tool here and it is worth saying why**, because it looks
/// right: it returns as soon as no frame is scheduled, and a request waiting on a socket
/// schedules no frames. So it settles *before* the platform has answered, and the assertion
/// after it races the response — which is exactly what happened, passing twice and failing once
/// against the same service. Waiting for the widget the response produces is the only honest
/// condition.
Future<void> waitFor(
  WidgetTester tester,
  Finder finder, {
  Duration timeout = const Duration(seconds: 30),
}) async {
  final deadline = DateTime.now().add(timeout);
  while (DateTime.now().isBefore(deadline)) {
    await tester.pump(const Duration(milliseconds: 200));
    if (finder.evaluate().isNotEmpty) return;
  }
  fail('timed out after $timeout waiting for: $finder');
}

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  testWidgets('creates an account from the form, against the running platform', (tester) async {
    expect(_email, isNotEmpty, reason: 'pass --dart-define=SHIPPER_DEMO_EMAIL=<fresh address>');
    expect(_phone, isNotEmpty, reason: 'pass --dart-define=SHIPPER_DEMO_PHONE=<fresh number>');

    await tester.pumpWidget(const ProviderScope(child: ShipperApp()));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('create-account')));
    await tester.pumpAndSettle();

    await tester.tap(find.byKey(const Key('role-provider')));
    await tester.pumpAndSettle();
    await tester.tap(find.byKey(const Key('role-continue')));
    await tester.pumpAndSettle();

    await tester.enterText(find.byKey(const Key('register-email')), _email);
    await tester.enterText(find.byKey(const Key('register-phone')), _phone);
    await tester.enterText(find.byKey(const Key('register-password')), _password);
    await tester.pump();

    await tester.tap(find.byKey(const Key('register-submit')));

    // The platform created the account and answered with it, so the journey moved on to the
    // address it has just sent a message to.
    await waitFor(tester, find.byKey(const Key('verify-email-token')));

    // Read off the instructions rather than searched for across the tree: the registration
    // screen is still mounted for the length of the route transition, and its own field holds
    // the same address.
    expect(
      tester.widget<Text>(find.byKey(const Key('verify-email-instructions'))).data,
      contains(_email),
    );
  });

  testWidgets('confirms both contact details and finishes the journey', (tester) async {
    expect(_token, isNotEmpty, reason: 'pass --dart-define=SHIPPER_DEMO_TOKEN=<from the log>');
    expect(_otp, isNotEmpty, reason: 'pass --dart-define=SHIPPER_DEMO_OTP=<from the log>');

    late final ProviderContainer container;
    await tester.pumpWidget(
      ProviderScope(
        child: Builder(
          builder: (context) {
            container = ProviderScope.containerOf(context, listen: false);
            return const ShipperApp();
          },
        ),
      ),
    );
    await tester.pumpAndSettle();

    // What tapping the link in the message does: `shipper:///verify-email?token=…` reaches
    // go_router as this location. A cold-start link takes the same path through the platform's
    // default route, which `test/features/identity/email_verification_screen_test.dart` covers.
    container.read(routerProvider).go('${Routes.verifyEmail}?token=$_token');

    await waitFor(tester, find.byKey(const Key('verify-email-verified')));

    await tester.tap(find.byKey(const Key('verify-email-continue')));
    // The phone screen asks for a code as it opens. Inside the platform's one-a-minute cooldown
    // that sends nothing and still answers 202, which is why the code read from the log a moment
    // ago is still the live one.
    await waitFor(tester, find.byKey(const Key('verify-phone-code')));

    await tester.enterText(find.byKey(const Key('verify-phone-code')), _otp);
    await tester.tap(find.byKey(const Key('verify-phone-submit')));

    await waitFor(tester, find.byKey(const Key('verify-phone-verified')));

    await tester.tap(find.byKey(const Key('verify-phone-continue')));
    await waitFor(tester, find.byKey(const Key('registered-heading')));

    // Both channels verified, as the platform reports them rather than as the client remembers.
    expect(find.byKey(const Key('registered-heading')), findsOneWidget);
    expect(find.text('Verified'), findsNWidgets(2));
  });
}
