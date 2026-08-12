// SHIP-71 — "Pickup and drop-off captured with validation and address lookup".
//
// Driven through the real app: the router, the guard the step had to be added to, the session,
// the controller and the screen. Only the token store and the jobs repository are substituted,
// which is what makes these demonstrations of the step rather than of a widget in isolation.
//
// The three halves of the *Done when* line map onto the three groups below — captured, validated,
// looked up — and the fourth group is the one nobody writes down and everybody needs: what
// happens when the same customer saves twice.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job_status.dart';

import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_jobs_repository.dart';

/// Signs a customer in and opens the locations step the way a person reaches it.
///
/// Through the shell's own button rather than by pushing the route, so the guard that had to
/// learn about `/jobs/new` is exercised here rather than only on a device — a signed-in user was
/// redirected home from *every* location but the shell until SHIP-71 widened it, and the symptom
/// is a button that appears to do nothing.
Future<FakeJobsRepository> openLocations(WidgetTester tester) async {
  final identity = FakeIdentityRepository();
  final jobs = FakeJobsRepository();

  // Eight inputs and two sections are taller than the 800x600 a widget test defaults to, and a
  // `ListView` does not build what is off-screen — so the submit button would not be in the tree
  // at all. A taller window is the honest fix: scrolling to it would test the scroll rather than
  // the step, and this way every field is reachable in every test here.
  tester.view.physicalSize = const Size(1000, 3000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  await tester.pumpWidget(signupApp(identity, jobs: jobs));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  expect(find.byKey(const Key('shell-customer')), findsOneWidget);

  await tester.tap(find.byKey(const Key('new-job')));
  await tester.pumpAndSettle();

  expect(find.byKey(const Key('job-locations-form')), findsOneWidget);
  return jobs;
}

Future<void> fillAddresses(
  WidgetTester tester, {
  String pickupLine = '12 Smith Street',
  String pickupSuburb = 'Newtown',
  String pickupState = 'NSW',
  String pickupPostcode = '2042',
  String dropoffLine = 'Lot 14 Boundary Road',
  String dropoffSuburb = 'Coonabarabran',
  String dropoffState = 'NSW',
  String dropoffPostcode = '2357',
}) async {
  await tester.enterText(find.byKey(const Key('pickup-line')), pickupLine);
  await tester.enterText(find.byKey(const Key('pickup-suburb')), pickupSuburb);
  await tester.enterText(find.byKey(const Key('pickup-state')), pickupState);
  await tester.enterText(find.byKey(const Key('pickup-postcode')), pickupPostcode);
  await tester.enterText(find.byKey(const Key('dropoff-line')), dropoffLine);
  await tester.enterText(find.byKey(const Key('dropoff-suburb')), dropoffSuburb);
  await tester.enterText(find.byKey(const Key('dropoff-state')), dropoffState);
  await tester.enterText(find.byKey(const Key('dropoff-postcode')), dropoffPostcode);
  await tester.pump();
}

Future<void> save(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('job-locations-submit')));
  await tester.pumpAndSettle();
}

void main() {
  group('capturing both addresses', () {
    testWidgets('sends the four parts of each, exactly as they were typed', (tester) async {
      final jobs = await openLocations(tester);
      jobs.draft = (_) => aJob(pickup: aResolvedLocation(), dropoff: anUnresolvedLocation());

      await fillAddresses(tester);
      await save(tester);

      final call = jobs.callsTo('create').single;
      expect(call.fields, <String, Object?>{
        'pickup': <String, Object?>{
          'line': '12 Smith Street',
          'suburb': 'Newtown',
          'state': 'NSW',
          'postcode': '2042',
        },
        'dropoff': <String, Object?>{
          'line': 'Lot 14 Boundary Road',
          'suburb': 'Coonabarabran',
          'state': 'NSW',
          'postcode': '2357',
        },
      });
    });

    testWidgets('an empty form still saves, because a draft is allowed to be incomplete',
        (tester) async {
      // Docs/01 §4.1 lets a customer save a draft and come back to it, and the platform calls an
      // empty body "a legitimate start a job for me". A client-side "all four are required" rule
      // would refuse exactly that, which is why this screen has no local validators at all.
      final jobs = await openLocations(tester);

      await save(tester);

      expect(jobs.callsTo('create'), hasLength(1));
      expect(find.byKey(const Key('job-locations-saved')), findsOneWidget);
      expect(find.text('Not added yet'), findsNWidgets(2));
    });

    testWidgets('the state goes out as it was typed and is not upper-cased on the device',
        (tester) async {
      // Docs/10 §4.7's exception. The platform accepts `nsw`, `NSW` and `New South Wales` and
      // normalises; a second normaliser here is how a client ends up disagreeing with the
      // platform about its own address.
      final jobs = await openLocations(tester);

      await fillAddresses(tester, pickupState: 'new south wales');
      await save(tester);

      final pickup = jobs.callsTo('create').single.fields['pickup']! as Map<String, Object?>;
      expect(pickup['state'], 'new south wales');
    });

    testWidgets('a second tap while the first is in flight does not make a second draft',
        (tester) async {
      final gate = Completer<void>();
      final jobs = await openLocations(tester);
      jobs.gates['create'] = gate;

      await fillAddresses(tester);
      await tester.tap(find.byKey(const Key('job-locations-submit')));
      await tester.pump();

      // The button is disabled while busy, so this tap reaches nothing. Two taps are two
      // actions and would carry two keys — which is the one case idempotency cannot absorb.
      await tester.tap(find.byKey(const Key('job-locations-submit')));
      await tester.pump();

      gate.complete();
      await tester.pumpAndSettle();

      expect(jobs.callsTo('create'), hasLength(1));
    });
  });

  group('validation, which is the platform’s', () {
    testWidgets('renders each field error under the field the platform named', (tester) async {
      final jobs = await openLocations(tester);
      jobs.failures['create'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'One or more fields were rejected.',
        requestId: '9f2c',
        details: <ApiFieldError>[
          ApiFieldError(
            field: 'pickup.postcode',
            code: 'invalid_format',
            message: 'Enter a four-digit postcode.',
          ),
          ApiFieldError(
            field: 'dropoff.state',
            code: 'not_allowed',
            message: 'Enter an Australian state or territory.',
          ),
        ],
      );

      await fillAddresses(tester, pickupPostcode: '204', dropoffState: 'Nova Scotia');
      await save(tester);

      expect(find.text('Enter a four-digit postcode.'), findsOneWidget);
      expect(find.text('Enter an Australian state or territory.'), findsOneWidget);

      // The banner is for failures the platform did not attach to a field. Showing both would
      // say the same thing twice, in two places, about one mistake.
      expect(find.byKey(const Key('failure-banner')), findsNothing);
      expect(find.byKey(const Key('job-locations-saved')), findsNothing);
    });

    testWidgets('a message disappears when the value it was about is corrected', (tester) async {
      final jobs = await openLocations(tester);
      jobs.failures['create'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'One or more fields were rejected.',
        details: <ApiFieldError>[
          ApiFieldError(
            field: 'pickup.postcode',
            code: 'invalid_format',
            message: 'Enter a four-digit postcode.',
          ),
        ],
      );

      await fillAddresses(tester, pickupPostcode: '204');
      await save(tester);
      expect(find.text('Enter a four-digit postcode.'), findsOneWidget);

      await tester.enterText(find.byKey(const Key('pickup-postcode')), '2042');
      await tester.pumpAndSettle();

      // A server message that outlives the value it was about is worse than none: it tells
      // somebody the thing they have just fixed is still wrong.
      expect(find.text('Enter a four-digit postcode.'), findsNothing);
    });

    testWidgets('a refusal with no field goes in the banner', (tester) async {
      // `jobs_customer_only`, `service_unavailable` and a dropped connection are about the
      // request rather than about something somebody typed. Under an input they would send a
      // customer hunting for a mistake in an address that is fine.
      final jobs = await openLocations(tester);
      jobs.failures['create'] = const ApiErrorResponse(
        statusCode: 403,
        code: 'jobs_customer_only',
        message: 'Only a customer account can create or edit a job.',
        requestId: '3d81',
      );

      await fillAddresses(tester);
      await save(tester);

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.text('Only a customer account can create or edit a job.'), findsOneWidget);
      expect(find.text('Reference 3d81'), findsOneWidget);
    });

    testWidgets('a corrected address is a new action and carries a new key', (tester) async {
      // The platform saw the first attempt and refused it. Retrying under the same key would
      // replay that refusal, which is precisely not what somebody who has just corrected a
      // postcode is asking for.
      final jobs = await openLocations(tester);
      jobs.failures['create'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'One or more fields were rejected.',
        details: <ApiFieldError>[
          ApiFieldError(field: 'pickup.postcode', code: 'invalid_format', message: 'Four digits.'),
        ],
      );

      await fillAddresses(tester, pickupPostcode: '204');
      await save(tester);

      await tester.enterText(find.byKey(const Key('pickup-postcode')), '2042');
      await tester.pump();
      await save(tester);

      final keys = jobs.callsTo('create').map((c) => c.idempotencyKey).toList();
      expect(keys, hasLength(2));
      expect(keys.first, isNot(keys.last));
      expect(jobs.callsTo('update'), isEmpty, reason: 'nothing was created, so nothing to edit');
    });

    testWidgets('a dropped connection is retried under the same key', (tester) async {
      // The case idempotency exists for: the platform may already have created the draft and
      // the answer never came back. A fresh key would create a second one.
      final jobs = await openLocations(tester);
      jobs.failures['create'] = const ApiUnreachable();

      await fillAddresses(tester);
      await save(tester);
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);

      await save(tester);

      final keys = jobs.callsTo('create').map((c) => c.idempotencyKey).toList();
      expect(keys, hasLength(2));
      expect(keys.first, keys.last);
    });
  });

  group('address lookup, and the outcome that is not an error', () {
    testWidgets('shows what the platform matched each address to', (tester) async {
      final jobs = await openLocations(tester);
      jobs.draft = (_) => aJob(pickup: aResolvedLocation(), dropoff: aResolvedLocation());

      await fillAddresses(tester);
      await save(tester);

      expect(find.byKey(const Key('job-locations-saved')), findsOneWidget);
      expect(
        find.text('We matched this to 12 Smith Street, Newtown NSW 2042.'),
        findsNWidgets(2),
      );
    });

    testWidgets('an address the platform could not place is ordinary, and does not block',
        (tester) async {
      // SHIP-59a: a failed lookup does not fail the job. The address is stored exactly as typed
      // and the customer carries on — rural addresses no geocoder knows are deliveries this
      // marketplace exists to carry.
      final jobs = await openLocations(tester);
      jobs.draft = (_) => aJob(pickup: aResolvedLocation(), dropoff: anUnresolvedLocation());

      await fillAddresses(tester);
      await save(tester);

      expect(find.byKey(const Key('job-locations-saved')), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
      expect(
        find.textContaining('could not match this to a place on the map'),
        findsOneWidget,
      );
      expect(find.text('Lot 14 Boundary Road, Coonabarabran NSW 2357'), findsOneWidget);

      // The way on is present and enabled. A screen that made the customer resolve this before
      // continuing would be blocking on something they cannot change.
      expect(
        tester.widget<FilledButton>(find.byKey(const Key('job-locations-done'))).onPressed,
        isNotNull,
      );
    });

    testWidgets('a draft with no coordinate at all is still a saved draft', (tester) async {
      // What a deployment with no geocoder configured answers with — staging, until one is.
      final jobs = await openLocations(tester);
      jobs.draft = (_) => aJob(pickup: anUnresolvedLocation(), dropoff: anUnresolvedLocation());

      await fillAddresses(tester);
      await save(tester);

      expect(find.byKey(const Key('job-locations-saved')), findsOneWidget);
      expect(find.textContaining('could not match'), findsNWidgets(2));
    });
  });

  group('saving twice', () {
    testWidgets('edits the same draft rather than creating a second', (tester) async {
      // Without this, every correction leaves an abandoned draft behind — and the customer sees
      // it in their job list, where nothing can explain it.
      final jobs = await openLocations(tester);
      jobs.draft = (_) => aJob(pickup: aResolvedLocation(), dropoff: aResolvedLocation());

      await fillAddresses(tester);
      await save(tester);

      await tester.tap(find.byKey(const Key('job-locations-edit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('job-locations-form')), findsOneWidget);
      // The form comes back holding what was typed, not what the platform normalised.
      expect(find.text('12 Smith Street'), findsOneWidget);

      await tester.enterText(find.byKey(const Key('pickup-line')), '14 Smith Street');
      await tester.pump();
      await save(tester);

      expect(jobs.callsTo('create'), hasLength(1));
      final edit = jobs.callsTo('update').single;
      expect(edit.jobId, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
      expect(
        (edit.fields['pickup']! as Map<String, Object?>)['line'],
        '14 Smith Street',
      );
    });

    testWidgets('the edit carries its own key, because it is a different action', (tester) async {
      final jobs = await openLocations(tester);

      await fillAddresses(tester);
      await save(tester);
      await tester.tap(find.byKey(const Key('job-locations-edit')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const Key('pickup-suburb')), 'Erskineville');
      await tester.pump();
      await save(tester);

      expect(
        jobs.callsTo('create').single.idempotencyKey,
        isNot(jobs.callsTo('update').single.idempotencyKey),
      );
    });
  });

  group('leaving the step', () {
    testWidgets('closing returns to the customer shell', (tester) async {
      await openLocations(tester);

      await tester.tap(find.byKey(const Key('job-locations-close')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
    });

    testWidgets('a saved draft is left with the platform, not carried back', (tester) async {
      final jobs = await openLocations(tester);
      jobs.draft = (_) => aJob(status: JobStatus.draft, pickup: aResolvedLocation());

      await fillAddresses(tester);
      await save(tester);
      await tester.tap(find.byKey(const Key('job-locations-done')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);

      // Opening the step again starts a *new* job. The controller is auto-disposed, so the id of
      // the draft just finished cannot be edited by accident by somebody starting another one.
      await tester.tap(find.byKey(const Key('new-job')));
      await tester.pumpAndSettle();
      await fillAddresses(tester);
      await save(tester);

      expect(jobs.callsTo('create'), hasLength(2));
      expect(jobs.callsTo('update'), isEmpty);
    });
  });
}
