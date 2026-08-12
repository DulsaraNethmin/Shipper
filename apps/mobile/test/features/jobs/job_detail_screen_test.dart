// SHIP-77 — "Full job detail with status timeline and available actions".
//
// Driven through the real app: the session, the router, the guard, the shell, the customer's job
// list, and the tap that opens the detail. Only the token store and the two repositories are
// substituted, so a guard that refused `/jobs/{id}` — which is exactly what the SHIP-49 guard did
// to the first screen hung off the shell — fails here rather than only on a device.
//
// The screen is reached by tapping rather than by pumping it directly, deliberately. The route
// carries the id and the screen re-reads the job from it, and both halves of that are the ticket:
// a detail screen handed a job across from the list would show whatever the list happened to have
// fetched, and would be unreachable from a notification (Docs/07 §5, SHIP-145).

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';

import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_jobs_repository.dart';

/// The job every test here opens.
const _id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

/// Signs a customer in, lands on their job list, and taps through to [_id].
///
/// The list has to hold the job for it to be tappable, which is what makes this the journey
/// rather than a shortcut past the screen that leads to it.
Future<void> openJobDetail(WidgetTester tester, FakeJobsRepository jobs) async {
  tester.view.physicalSize = const Size(800, 1600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  if (jobs.page.data.isEmpty) {
    jobs.page = ApiPage<Job>(data: <Job>[jobs.detail(_id)]);
  }

  await tester.pumpWidget(signupApp(FakeIdentityRepository(), jobs: jobs));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('job-$_id')));
  await tester.pumpAndSettle();
}

/// A finder scoped to the detail screen.
///
/// The list is still mounted underneath the pushed route, so an unscoped `find.text` can match a
/// job card as readily as the screen on top of it.
Finder inDetail(Finder matching) => find.descendant(
      of: find.byKey(const Key('job-detail')),
      matching: matching,
    );

void main() {
  group('getting there', () {
    testWidgets('tapping a job opens it, and the screen reads it by id', (tester) async {
      final jobs = FakeJobsRepository();

      await openJobDetail(tester, jobs);

      expect(find.byKey(const Key('job-detail')), findsOneWidget);
      expect(jobs.callsTo('read'), hasLength(1));
      expect(jobs.callsTo('read').single.jobId, _id);
    });

    testWidgets('the job is re-read rather than carried across from the list', (tester) async {
      // The list page may be minutes old. What the customer opens has to be what the platform
      // holds now, which is also what makes a deep link from a notification reach the same
      // screen with nothing but an id.
      final jobs = FakeJobsRepository()
        ..page = ApiPage<Job>(data: <Job>[aJob(id: _id)])
        ..detail = (id) => aJob(id: id, status: JobStatus.open);

      await openJobDetail(tester, jobs);

      expect(inDetail(find.byKey(const Key('job-detail-status-open'))), findsOneWidget);
    });
  });

  group('the job in full', () {
    testWidgets('shows both addresses the way a person writes one', (tester) async {
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(
              id: id,
              status: JobStatus.open,
              pickup: aResolvedLocation(),
              dropoff: aResolvedLocation(
                line: '4 Harbour Road',
                suburb: 'Fremantle',
                state: 'WA',
                postcode: '6160',
              ),
            );

      await openJobDetail(tester, jobs);

      // The state stays as the platform sent it — the upper-case abbreviation (Docs/10 §4.7).
      expect(inDetail(find.text('12 Smith Street, Newtown NSW 2042')), findsOneWidget);
      expect(inDetail(find.text('4 Harbour Road, Fremantle WA 6160')), findsOneWidget);
    });

    testWidgets('an address the platform could not place is information, not an error',
        (tester) async {
      // SHIP-59a: a failed lookup must not fail the job. Rural addresses no geocoder knows are
      // deliveries this marketplace exists to carry.
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id, pickup: anUnresolvedLocation());

      await openJobDetail(tester, jobs);

      expect(inDetail(find.textContaining('could not match this to a map')), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
    });

    testWidgets('shows the goods, the size, the weight and the handling notes', (tester) async {
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id).copyWith(
              goodsDescription: 'Two-seater sofa, wrapped, no legs attached',
              lengthCm: 190,
              widthCm: 90,
              heightCm: 85,
              weightKg: 45.5,
              vehicleRequirement: 'Ute with a tailgate lifter',
              handlingNotes: 'Second-floor walk-up, no lift.',
            );

      await openJobDetail(tester, jobs);

      expect(inDetail(find.text('Two-seater sofa, wrapped, no legs attached')), findsOneWidget);
      expect(inDetail(find.text('190L × 90W × 85H cm')), findsOneWidget);
      expect(inDetail(find.text('45.5 kg')), findsOneWidget);
      expect(inDetail(find.text('Ute with a tailgate lifter')), findsOneWidget);
      expect(inDetail(find.text('Second-floor walk-up, no lift.')), findsOneWidget);
    });

    testWidgets('a draft with nothing in it draws no empty sections', (tester) async {
      // A draft is mostly empty for most of its life and the platform omits what is empty. A
      // heading with nothing under it is worse than no heading.
      await openJobDetail(tester, FakeJobsRepository());

      expect(inDetail(find.text('Goods')), findsNothing);
      expect(inDetail(find.text('Timing')), findsNothing);
      expect(inDetail(find.text('Route')), findsOneWidget);
    });

    testWidgets('a time window reads day-first', (tester) async {
      // 03/04 is a different day to an Australian and an American, and the reader cannot tell
      // which convention a screen used — so the month is spelled.
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id).copyWith(
              pickupWindow: const JobTimeWindow(
                start: '2026-08-15T01:00:00Z',
                end: '2026-08-16T01:00:00Z',
              ),
            );

      await openJobDetail(tester, jobs);

      expect(inDetail(find.textContaining('Aug 2026')), findsWidgets);
    });
  });

  group('the owner’s own budget', () {
    testWidgets('is shown in Australian currency, from cents', (tester) async {
      // Docs/01 §4.3 keeps it from providers. `GET /v1/jobs/{id}` is owner-only and answers 404
      // to everybody else, which is what makes showing it here legitimate.
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id, budgetCents: 150000);

      await openJobDetail(tester, jobs);

      expect(inDetail(find.text(r'$1,500.00')), findsOneWidget);
    });

    testWidgets('no budget is said out loud, and is not a budget of nothing', (tester) async {
      // The platform omits the field rather than sending zero, precisely so the two are
      // distinguishable. Rendering `$0.00` here would tell the customer something untrue about
      // what they had set.
      await openJobDetail(tester, FakeJobsRepository());

      expect(inDetail(find.textContaining('Not set')), findsOneWidget);
      expect(inDetail(find.textContaining(r'$0.00')), findsNothing);
    });
  });

  group('the status timeline', () {
    testWidgets('draws every step of the lifecycle, with the current one marked', (tester) async {
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id, status: JobStatus.awarded);

      await openJobDetail(tester, jobs);

      expect(find.byKey(const Key('job-detail-timeline')), findsOneWidget);
      for (final status in <String>['draft', 'open', 'awarded', 'delivered', 'completed']) {
        expect(
          find.byKey(Key('job-detail-step-$status')),
          findsOneWidget,
          reason: 'the timeline is missing $status',
        );
      }

      // The exact names from Docs/02 §1 (CLAUDE.md) — the words the whole product uses.
      expect(inDetail(find.text('En route to pickup')), findsOneWidget);
      expect(inDetail(find.text('Driver assigned')), findsOneWidget);
    });

    testWidgets('says why the earlier steps carry no times', (tester) async {
      // A timeline with no dates on it and no explanation reads as a screen that failed to load
      // them. The per-transition history is recorded (SHIP-57a) and served by no endpoint.
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id, status: JobStatus.inTransit);

      await openJobDetail(tester, jobs);

      expect(find.byKey(const Key('job-detail-timeline-note')), findsOneWidget);
    });

    testWidgets('a cancelled job shows where it began and where it ended, and nothing else',
        (tester) async {
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id, status: JobStatus.cancelled);

      await openJobDetail(tester, jobs);

      expect(find.byKey(const Key('job-detail-step-draft')), findsOneWidget);
      expect(find.byKey(const Key('job-detail-step-cancelled')), findsOneWidget);
      expect(find.byKey(const Key('job-detail-step-awarded')), findsNothing);
      expect(find.byKey(const Key('job-detail-step-completed')), findsNothing);
    });

    testWidgets('the creation date is shown against the first step', (tester) async {
      // The one timestamp that is honest without qualification: the contract states a job is
      // created as `draft`, so this is when it entered that step.
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id, createdAt: '2026-08-11T03:30:00.000Z');

      await openJobDetail(tester, jobs);

      expect(inDetail(find.text('Created 11 Aug 2026')), findsOneWidget);
    });
  });

  group('the available actions', () {
    testWidgets('a draft can be cancelled', (tester) async {
      await openJobDetail(tester, FakeJobsRepository());

      expect(find.byKey(const Key('job-action-cancel')), findsOneWidget);
      expect(find.byKey(const Key('job-detail-no-actions')), findsNothing);
    });

    testWidgets('an awarded job offers nothing, and says why rather than going quiet',
        (tester) async {
      // Docs/02 §6.2: once a provider has committed, ending the job is a support matter. "No
      // actions available" reads as a fault in the app; naming the reason is what sends somebody
      // to support instead of tapping a greyed control.
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id, status: JobStatus.awarded);

      await openJobDetail(tester, jobs);

      expect(find.byKey(const Key('job-action-cancel')), findsNothing);
      expect(find.byKey(const Key('job-detail-no-actions')), findsOneWidget);
      expect(inDetail(find.textContaining('support matter')), findsOneWidget);
    });

    testWidgets('cancelling is confirmed first, and dismissing it does nothing', (tester) async {
      // Cancelling is not reversible from the app — Docs/02 §2 has no route out of `cancelled` —
      // and a job with bids on it ends other people's work as well as the customer's own.
      final jobs = FakeJobsRepository();
      await openJobDetail(tester, jobs);

      await tester.tap(find.byKey(const Key('job-action-cancel')));
      await tester.pumpAndSettle();
      expect(find.byKey(const Key('job-cancel-dialog')), findsOneWidget);

      await tester.tap(find.byKey(const Key('job-cancel-dismiss')));
      await tester.pumpAndSettle();

      expect(jobs.callsTo('cancel'), isEmpty);
      expect(find.byKey(const Key('job-action-cancel')), findsOneWidget);
    });

    testWidgets('confirming sends the cancellation and shows the job as it now is',
        (tester) async {
      final jobs = FakeJobsRepository();
      await openJobDetail(tester, jobs);

      await tester.tap(find.byKey(const Key('job-action-cancel')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('job-cancel-confirm')));
      await tester.pumpAndSettle();

      final call = jobs.callsTo('cancel').single;
      expect(call.jobId, _id);
      expect(call.idempotencyKey, isNotNull);
      expect(call.idempotencyKey, isNotEmpty);
      // `{}` is a complete request. Docs/01 §3 requires a reason only of an administrator; a
      // customer abandoning their own draft owes nobody an explanation.
      expect(call.fields, isEmpty);

      // The status is the platform's answer, not an optimistic local change: the screen shows
      // what came back.
      expect(inDetail(find.byKey(const Key('job-detail-status-cancelled'))), findsOneWidget);
      expect(find.byKey(const Key('job-action-cancel')), findsNothing);
      expect(find.byKey(const Key('job-detail-no-actions')), findsOneWidget);
    });

    testWidgets('the button is disabled while the cancellation is in flight', (tester) async {
      // Two taps are two actions and the idempotency key is per action (Docs/07 §4), so a second
      // tap would not be absorbed by the first — it would be a second request.
      final gate = Completer<void>();
      final jobs = FakeJobsRepository();
      await openJobDetail(tester, jobs);

      jobs.gates['cancel'] = gate;
      await tester.tap(find.byKey(const Key('job-action-cancel')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('job-cancel-confirm')));
      await tester.pump();

      final button = tester.widget<OutlinedButton>(find.byKey(const Key('job-action-cancel')));
      expect(button.onPressed, isNull);

      gate.complete();
      await tester.pumpAndSettle();
      expect(jobs.callsTo('cancel'), hasLength(1));
    });

    testWidgets('a refusal is rendered and the job is re-read, because the platform decides',
        (tester) async {
      // The app was showing a stale status — the job was awarded a minute ago on another
      // device. Docs/02 §3.1: on conflict the server wins and the app reconciles to platform
      // state rather than arguing with it or discarding the answer.
      final jobs = FakeJobsRepository()
        ..failures['cancel'] = const ApiErrorResponse(
          statusCode: 409,
          code: 'jobs_not_cancellable',
          message: 'This job can no longer be cancelled. Reload it to see its current status.',
          requestId: '01930f4c-1a2b-7c3d-9e4f-5a6b7c8d9e0f',
        );

      await openJobDetail(tester, jobs);
      expect(jobs.callsTo('read'), hasLength(1));

      // What the platform actually holds, which the app is about to find out.
      jobs.detail = (id) => aJob(id: id, status: JobStatus.awarded);

      await tester.tap(find.byKey(const Key('job-action-cancel')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('job-cancel-confirm')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(jobs.callsTo('read'), hasLength(2), reason: 'a 409 means reload, not merely report');
      expect(inDetail(find.byKey(const Key('job-detail-status-awarded'))), findsOneWidget);
    });
  });

  group('when the job cannot be read', () {
    testWidgets('offers a retry rather than an empty screen', (tester) async {
      final jobs = FakeJobsRepository()..failures['read'] = const ApiUnreachable();

      await openJobDetail(tester, jobs);

      expect(find.byKey(const Key('job-detail-failed')), findsOneWidget);
      expect(find.byKey(const Key('job-detail-loading')), findsNothing);

      await tester.tap(find.byKey(const Key('job-detail-retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('job-detail-failed')), findsNothing);
      expect(inDetail(find.byKey(const Key('job-detail-status-draft'))), findsOneWidget);
    });

    testWidgets('a 404 is shown as the platform worded it, with no client-side embellishment',
        (tester) async {
      // A job belonging to somebody else answers 404 byte-identically to one that does not
      // exist, and that indistinguishability is the privacy control. A client that guessed which
      // it was would undo it.
      final jobs = FakeJobsRepository()
        ..failures['read'] = const ApiErrorResponse(
          statusCode: 404,
          code: 'not_found',
          message: 'No job with that identifier.',
          requestId: '01930f4c-1a2b-7c3d-9e4f-5a6b7c8d9e0f',
        );

      await openJobDetail(tester, jobs);

      expect(find.text('No job with that identifier.'), findsOneWidget);
      expect(find.textContaining('Reference 01930f4c'), findsOneWidget);
    });

    testWidgets('a refresh that fails leaves the job on screen', (tester) async {
      // Somebody who pulled to refresh in a tunnel should still be looking at their delivery,
      // with a banner saying the reload did not work — not an empty screen.
      final jobs = FakeJobsRepository()
        ..detail = (id) => aJob(id: id, pickup: aResolvedLocation());

      await openJobDetail(tester, jobs);

      jobs.failures['read'] = const ApiUnreachable();
      await tester.fling(find.byKey(const Key('job-detail')), const Offset(0, 600), 1000);
      await tester.pumpAndSettle();

      expect(inDetail(find.text('12 Smith Street, Newtown NSW 2042')), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.byKey(const Key('job-detail-failed')), findsNothing);
    });

    testWidgets('shows a spinner only while the first read is genuinely in flight',
        (tester) async {
      final gate = Completer<void>();
      final jobs = FakeJobsRepository()..gates['read'] = gate;

      tester.view.physicalSize = const Size(800, 1600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      jobs.page = ApiPage<Job>(data: <Job>[aJob(id: _id)]);
      await tester.pumpWidget(signupApp(FakeIdentityRepository(), jobs: jobs));
      await tester.pumpAndSettle();
      await signInThrough(tester);
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('job-$_id')));

      // Deliberately not `pumpAndSettle`: a progress indicator animates for ever, so settling is
      // precisely what this state cannot do.
      for (var i = 0; i < 10; i++) {
        await tester.pump(const Duration(milliseconds: 50));
      }
      expect(find.byKey(const Key('job-detail-loading')), findsOneWidget);

      gate.complete();
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('job-detail-loading')), findsNothing);
      expect(inDetail(find.byKey(const Key('job-detail-status-draft'))), findsOneWidget);
    });
  });
}
