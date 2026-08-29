// SHIP-73 — "Date window and vehicle requirement captured".
//
// Driven through the real app, and reached the way a person reaches it: through the locations step
// and the goods step. The route is new, and a guard that refused `/jobs/{id}/schedule` would show
// up here rather than only on a device.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';

import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_jobs_repository.dart';

const _draftId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

/// Walks a signed-in customer from the shell to the schedule step.
Future<FakeJobsRepository> openSchedule(WidgetTester tester, {Job? draft}) async {
  final identity = FakeIdentityRepository();
  final jobs = FakeJobsRepository();

  final saved = draft ?? aJob(id: _draftId, pickup: aResolvedLocation());
  jobs.draft = (_) => saved;
  jobs.detail = (_) => saved;

  tester.view.physicalSize = const Size(1000, 3600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  await tester.pumpWidget(signupApp(identity, jobs: jobs));
  await tester.pumpAndSettle();
  await signInThrough(tester);

  await tester.tap(find.byKey(const Key('new-job')));
  await tester.pumpAndSettle();
  await tester.enterText(find.byKey(const Key('pickup-line')), '12 Smith Street');
  await tester.pump();
  await tester.tap(find.byKey(const Key('job-locations-submit')));
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(const Key('job-locations-continue')));
  await tester.pumpAndSettle();

  await tester.enterText(find.byKey(const Key('goods-description')), 'A pallet');
  await tester.pump();
  await tester.tap(find.byKey(const Key('job-goods-submit')));
  await tester.pumpAndSettle();

  jobs.calls.clear();
  return jobs;
}

/// Chooses a date through the calendar the field opens.
///
/// The offered date is accepted as it arrives. **The value is deliberately not steered**: what
/// this ticket has to demonstrate is that a window is captured and sent in a form the platform
/// parses, and whether *that* window is acceptable is `internal/jobs`' answer rather than this
/// device's.
Future<void> chooseDate(WidgetTester tester, String fieldKey) async {
  await tester.tap(find.byKey(Key('$fieldKey-choose')));
  await tester.pumpAndSettle();
  await tester.tap(find.text('OK'));
  await tester.pumpAndSettle();
}

Future<void> saveSchedule(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('job-schedule-submit')));
  await tester.pumpAndSettle();
}

/// The window keys of the one save that was made.
Map<String, Object?> windowOf(FakeJobsRepository jobs, String window) {
  return jobs.callsTo('update').single.fields[window]! as Map<String, Object?>;
}

void main() {
  group('reaching the step', () {
    testWidgets('the goods step continues into it, and the guard admits the route',
        (tester) async {
      await openSchedule(tester);

      expect(find.byKey(const Key('job-schedule-form')), findsOneWidget);
      expect(find.text('Step 3 of 4 · Schedule'), findsOneWidget);
    });
  });

  group('capturing the date window', () {
    testWidgets('sends both ends of the pickup window under the keys the contract names',
        (tester) async {
      final jobs = await openSchedule(tester);

      await chooseDate(tester, 'pickup-from');
      await chooseDate(tester, 'pickup-to');
      await saveSchedule(tester);

      final pickup = windowOf(jobs, 'pickup_window');
      expect(pickup['start'], isNotEmpty);
      expect(pickup['end'], isNotEmpty);
    });

    testWidgets('a window opens at the start of its day and closes at the end of one',
        (tester) async {
      final jobs = await openSchedule(tester);

      await chooseDate(tester, 'pickup-from');
      await chooseDate(tester, 'pickup-to');
      await saveSchedule(tester);

      final pickup = windowOf(jobs, 'pickup_window');

      // The edge that is easy to get wrong: a window closing at midnight *on* its day ends before
      // that day has happened, so "collect by Friday" would mean "collect by Thursday night".
      expect(pickup['start'], contains('T00:00:00'));
      expect(pickup['end'], contains('T23:59:59'));
    });

    testWidgets('the instants carry an offset, which is what the platform parses', (tester) async {
      final jobs = await openSchedule(tester);

      await chooseDate(tester, 'pickup-from');
      await saveSchedule(tester);

      // `DateTime.toIso8601String()` on a local value has no offset at all, which is not RFC 3339
      // and which `time.Parse(time.RFC3339, …)` refuses — reaching the customer as "that is not a
      // date" about a date they picked from a calendar.
      expect(
        windowOf(jobs, 'pickup_window')['start'],
        matches(RegExp(r'T00:00:00[+-]\d{2}:\d{2}$')),
      );
    });

    testWidgets('a date the customer never chose is sent as the value that clears it',
        (tester) async {
      final jobs = await openSchedule(tester);

      await chooseDate(tester, 'pickup-from');
      await saveSchedule(tester);

      // Empty rather than omitted. A `PATCH` leaves out what it is not given, so omitting these
      // would mean "keep whatever is there" — which is not what a customer who cleared a date
      // meant. The contract names the empty string as the clearing value.
      expect(windowOf(jobs, 'pickup_window')['end'], '');
      expect(windowOf(jobs, 'dropoff_window')['end'], '');
      // The drop-off's start is never collected: for most road transport it follows from the
      // pickup rather than constraining it.
      expect(windowOf(jobs, 'dropoff_window')['start'], '');
    });

    testWidgets('a chosen date can be cleared again', (tester) async {
      final jobs = await openSchedule(tester);

      await chooseDate(tester, 'pickup-to');
      expect(find.byKey(const Key('pickup-to-clear')), findsOneWidget);

      await tester.tap(find.byKey(const Key('pickup-to-clear')));
      await tester.pumpAndSettle();
      await saveSchedule(tester);

      expect(windowOf(jobs, 'pickup_window')['end'], '');
    });

    testWidgets('a saved window comes back on the day it was chosen for', (tester) async {
      await openSchedule(
        tester,
        draft: aJob(id: _draftId, pickup: aResolvedLocation()).copyWith(
          // **Midday UTC, deliberately.** The stored instant is rendered in the device's own zone,
          // which is the conversion `dayFirstDate` exists to make — a job created at 8am in Sydney
          // is the previous day in UTC, and showing the raw date would give a customer the wrong
          // day for every job created after 10am local. That also makes a fixture written at
          // midnight timezone-dependent: `2026-09-03T00:00:00+10:00` is the 2nd on a UTC runner
          // and the 3rd on a developer's machine in Australia, and the test would pass in one
          // place and fail in the other for no defect. Midday UTC is the 3rd in every zone from
          // UTC-11 to UTC+11.
          pickupWindow: const JobTimeWindow(
            start: '2026-09-03T12:00:00Z',
            end: '2026-09-05T12:00:00Z',
          ),
        ),
      );

      // Day-first and the month spelled, which removes the 03/09 ambiguity altogether.
      expect(find.text('3 Sep 2026'), findsOneWidget);
      expect(find.text('5 Sep 2026'), findsOneWidget);
    });
  });

  group('capturing the vehicle requirement', () {
    testWidgets('sends it as the customer wrote it, with the handling notes beside it',
        (tester) async {
      final jobs = await openSchedule(tester);

      await tester.enterText(
        find.byKey(const Key('vehicle-requirement')),
        'Ute with a tailgate lifter',
      );
      await tester.enterText(
        find.byKey(const Key('handling-notes')),
        'Second-floor walk-up, no lift. Buzzer 4B, ring ahead.',
      );
      await tester.pump();
      await saveSchedule(tester);

      final fields = jobs.callsTo('update').single.fields;
      expect(fields['vehicle_requirement'], 'Ute with a tailgate lifter');
      // Not named by SHIP-73 and captured by no other step. `Docs/01` §4.1 lists it as something a
      // customer may put on a job, so without this the field would be reachable by no screen.
      expect(fields['handling_notes'], 'Second-floor walk-up, no lift. Buzzer 4B, ring ahead.');
    });

    testWidgets('a saved draft comes back with both in the fields', (tester) async {
      await openSchedule(
        tester,
        draft: aJob(id: _draftId, pickup: aResolvedLocation()).copyWith(
          vehicleRequirement: 'Van',
          handlingNotes: 'Ring ahead.',
        ),
      );

      expect(find.text('Van'), findsOneWidget);
      expect(find.text('Ring ahead.'), findsOneWidget);
    });
  });

  group('nothing on this screen decides what the platform decides', () {
    testWidgets('a draft with no dates at all still saves', (tester) async {
      final jobs = await openSchedule(tester);

      // `publishable` requires `pickup_window.start`, and publication is where that is enforced.
      // `Docs/01` §4.1 lets a customer save a half-finished job and come back, so a client-side
      // "required" here would refuse what the platform allows.
      await saveSchedule(tester);

      expect(jobs.callsTo('update'), hasLength(1));
      expect(windowOf(jobs, 'pickup_window')['start'], '');
    });

    testWidgets('a message about a window lands on the field that produced it', (tester) async {
      final jobs = await openSchedule(tester);

      jobs.failures['update'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'That job could not be saved.',
        details: [
          ApiFieldError(
            field: 'pickup_window.end',
            code: 'out_of_range',
            message: 'The latest date cannot be before the earliest.',
          ),
        ],
      );

      await chooseDate(tester, 'pickup-from');
      await saveSchedule(tester);

      expect(find.text('The latest date cannot be before the earliest.'), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
    });

    testWidgets('a refusal about the whole window is shown, not swallowed', (tester) async {
      final jobs = await openSchedule(tester);

      // The platform may refuse the window as a whole rather than one of its ends. A screen that
      // only looked for the dotted paths would render this nowhere.
      jobs.failures['update'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'That job could not be saved.',
        details: [
          ApiFieldError(
            field: 'pickup_window',
            code: 'out_of_range',
            message: 'That window has already passed.',
          ),
        ],
      );

      await saveSchedule(tester);

      expect(find.text('That window has already passed.'), findsOneWidget);
    });
  });
}
