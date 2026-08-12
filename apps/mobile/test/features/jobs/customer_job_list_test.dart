// SHIP-76 — "Customer sees their jobs grouped by status with pull-to-refresh".
//
// Driven through the real app: the session, the router, the shell and the half the role selects.
// Only the token store and the jobs repository are substituted.
//
// The grouping itself is checked twice and on purpose. `groupJobsByStatus` is a pure function and
// is tested as one, because "grouped by status" is a fact about the data rather than about a
// layout; the widget tests then check that what is grouped is what is drawn, and in the order
// Docs/02 §1 lists.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/customer_jobs_controller.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';

import '../../core/auth/session_fixtures.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_jobs_repository.dart';

/// Signs a customer in and lands on their job list, which **is** the customer half of the shell.
Future<void> openCustomerShell(WidgetTester tester, FakeJobsRepository jobs) async {
  tester.view.physicalSize = const Size(800, 1200);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  await tester.pumpWidget(signupApp(FakeIdentityRepository(), jobs: jobs));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();
}

void main() {
  group('grouping, as a function', () {
    test('groups by status in the order Docs/02 §1 lists them', () {
      // Not alphabetical and not by arrival: Draft, Open, Negotiating … is the job's own
      // lifecycle, and a customer scanning the screen reads it in that order. The order comes
      // from JobStatus.values, so there is no second list to disagree with the first.
      final groups = groupJobsByStatus(<Job>[
        aJob(id: 'c', status: JobStatus.cancelled),
        aJob(id: 'o', status: JobStatus.open),
        aJob(id: 'd1', status: JobStatus.draft),
        aJob(id: 'o2', status: JobStatus.open),
        aJob(id: 'd2', status: JobStatus.draft),
      ]);

      expect(
        groups.map((g) => g.status).toList(),
        <JobStatus>[JobStatus.draft, JobStatus.open, JobStatus.cancelled],
      );
      expect(groups.first.jobs.map((j) => j.id).toList(), <String>['d1', 'd2']);
    });

    test('a status with no jobs produces no group', () {
      final groups = groupJobsByStatus(<Job>[aJob(status: JobStatus.delivered)]);

      expect(groups, hasLength(1));
      expect(groups.single.status, JobStatus.delivered);
    });

    test('a status this build does not know is grouped last rather than dropped', () {
      // Docs/07 §6. A thirteenth status must not make a job vanish from its owner's list — the
      // customer would have no way of knowing anything was missing.
      final groups = groupJobsByStatus(<Job>[
        aJob(id: 'x', status: JobStatus.unknown),
        aJob(id: 'd', status: JobStatus.draft),
      ]);

      expect(groups.map((g) => g.status).toList(), <JobStatus>[JobStatus.draft, JobStatus.unknown]);
    });

    test('nothing at all is no groups', () {
      expect(groupJobsByStatus(const []), isEmpty);
    });
  });

  group('the customer half', () {
    testWidgets('shows the jobs grouped, with the status as the heading', (tester) async {
      final jobs = FakeJobsRepository()
        ..page = ApiPage(
          data: [
            aJob(id: 'job-open', status: JobStatus.open, pickup: aResolvedLocation()),
            aJob(id: 'job-draft'),
          ],
        );

      await openCustomerShell(tester, jobs);

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
      expect(find.byKey(const Key('job-group-draft')), findsOneWidget);
      expect(find.byKey(const Key('job-group-open')), findsOneWidget);
      expect(find.byKey(const Key('job-job-draft')), findsOneWidget);
      expect(find.byKey(const Key('job-job-open')), findsOneWidget);

      // The heading is the exact name from Docs/02 §1 (CLAUDE.md).
      expect(find.text('Draft'), findsOneWidget);
      expect(find.text('Open'), findsOneWidget);
    });

    testWidgets('reads the list once rather than once per status', (tester) async {
      // `?status=` takes one value, so a screen showing every status would need twelve requests.
      // The contract recommends reading once and grouping on the device, and this is that.
      final jobs = FakeJobsRepository()
        ..page = ApiPage(
          data: [
            aJob(id: 'a', status: JobStatus.draft),
            aJob(id: 'b', status: JobStatus.open),
            aJob(id: 'c', status: JobStatus.awarded),
          ],
        );

      await openCustomerShell(tester, jobs);

      expect(jobs.callsTo('list'), hasLength(1));
      expect(jobs.callsTo('list').single.cursor, isNull);
    });

    testWidgets('a customer with no jobs sees an empty state, not a spinner and not an error',
        (tester) async {
      // The case that never occurs in development, because whoever is building this has jobs.
      // `data` is `[]` rather than `null` precisely so this is ordinary.
      await openCustomerShell(tester, FakeJobsRepository());

      expect(find.byKey(const Key('customer-jobs-empty')), findsOneWidget);
      expect(find.byKey(const Key('customer-jobs-loading')), findsNothing);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
      expect(find.text('You have not published a delivery yet'), findsOneWidget);
    });

    testWidgets('shows a spinner only while the first page is genuinely in flight',
        (tester) async {
      final gate = Completer<void>();
      final jobs = FakeJobsRepository()..gates['list'] = gate;

      tester.view.physicalSize = const Size(800, 1200);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      await tester.pumpWidget(signupApp(FakeIdentityRepository(), jobs: jobs));
      await tester.pumpAndSettle();

      await tester.enterText(find.byKey(const Key('sign-in-email')), 'alice@example.com');
      await tester.enterText(
          find.byKey(const Key('sign-in-password')), 'correct-horse-battery-staple');
      await tester.tap(find.byKey(const Key('sign-in')));

      // Deliberately not `pumpAndSettle`: a progress indicator animates for ever, so settling is
      // precisely what this state cannot do — which is also why it is worth asserting that the
      // app does not sit in it when there is nothing in flight.
      for (var i = 0; i < 5; i++) {
        await tester.pump(const Duration(milliseconds: 50));
      }

      expect(find.byKey(const Key('customer-jobs-loading')), findsOneWidget);

      gate.complete();
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('customer-jobs-loading')), findsNothing);
      expect(find.byKey(const Key('customer-jobs-empty')), findsOneWidget);
    });
  });

  group('the owner’s own budget', () {
    testWidgets('is shown in Australian currency, from cents', (tester) async {
      // Docs/01 §4.3 keeps it from providers; every job here belongs to the person looking at
      // it, which is what makes showing it legitimate — and comparing bids against their own
      // maximum is what a customer keeps it for.
      final jobs = FakeJobsRepository()
        ..page = ApiPage(data: [aJob(budgetCents: 150000)]);

      await openCustomerShell(tester, jobs);

      expect(find.textContaining(r'Budget $1,500.00'), findsOneWidget);
    });

    testWidgets('no budget is not a budget of nothing', (tester) async {
      // The platform omits the field rather than sending zero, so that these two are
      // distinguishable. A client that rendered `$0.00` for "not supplied" would tell a
      // provider-facing conversation something untrue.
      final jobs = FakeJobsRepository()..page = ApiPage(data: [aJob()]);

      await openCustomerShell(tester, jobs);

      expect(find.textContaining('Budget'), findsNothing);
    });
  });

  group('dates are day-first', () {
    testWidgets('a deadline reads the way an Australian customer reads one', (tester) async {
      // 03/04 is a different day to an Australian and an American, and the person reading it
      // cannot tell which convention was used — so the month is spelled.
      final jobs = FakeJobsRepository()
        ..page = ApiPage(
          data: [
            aJob(
              status: JobStatus.open,
              createdAt: '2026-08-11T03:30:00.000Z',
              expiresAt: '2026-08-25T03:30:00.000Z',
            ),
          ],
        );

      await openCustomerShell(tester, jobs);

      expect(find.textContaining('Created 11 Aug 2026'), findsOneWidget);
      expect(find.textContaining('Expires 25 Aug 2026'), findsOneWidget);
    });

    testWidgets('a draft has no deadline and none is shown', (tester) async {
      final jobs = FakeJobsRepository()..page = ApiPage(data: [aJob()]);

      await openCustomerShell(tester, jobs);

      expect(find.textContaining('Expires'), findsNothing);
    });
  });

  group('pull to refresh', () {
    testWidgets('reads the first page again and shows what changed', (tester) async {
      final jobs = FakeJobsRepository()..page = ApiPage(data: [aJob(id: 'first')]);
      await openCustomerShell(tester, jobs);
      expect(find.byKey(const Key('job-first')), findsOneWidget);

      jobs.page = ApiPage(data: [aJob(id: 'first'), aJob(id: 'second', status: JobStatus.open)]);

      await tester.fling(find.byKey(const Key('shell-customer')), const Offset(0, 600), 1000);
      await tester.pumpAndSettle();

      expect(jobs.callsTo('list'), hasLength(2));
      expect(find.byKey(const Key('job-second')), findsOneWidget);
    });

    testWidgets('works on the empty state as well', (tester) async {
      // A pull gesture that only worked once there was something to scroll would stop working
      // exactly when somebody most wants it: a new customer waiting for their first job to show.
      final jobs = FakeJobsRepository();
      await openCustomerShell(tester, jobs);
      expect(find.byKey(const Key('customer-jobs-empty')), findsOneWidget);

      jobs.page = ApiPage(data: [aJob(id: 'arrived')]);
      await tester.fling(find.byKey(const Key('shell-customer')), const Offset(0, 600), 1000);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('job-arrived')), findsOneWidget);
    });

    testWidgets('a refresh that fails leaves the jobs on screen', (tester) async {
      // Somebody who pulled to refresh in a tunnel should still be looking at their list, with a
      // banner saying the reload did not work — not an empty screen.
      final jobs = FakeJobsRepository()..page = ApiPage(data: [aJob(id: 'held')]);
      await openCustomerShell(tester, jobs);

      jobs.failures['list'] = const ApiUnreachable();
      await tester.fling(find.byKey(const Key('shell-customer')), const Offset(0, 600), 1000);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('job-held')), findsOneWidget);
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
    });
  });

  group('when the list cannot be read at all', () {
    testWidgets('offers a retry, and is not the empty state', (tester) async {
      // "You have no jobs" and "we could not find out" are different things to be told, and only
      // one of them has a retry.
      final jobs = FakeJobsRepository()..failures['list'] = const ApiUnreachable();

      await openCustomerShell(tester, jobs);

      expect(find.byKey(const Key('customer-jobs-failed')), findsOneWidget);
      expect(find.byKey(const Key('customer-jobs-empty')), findsNothing);

      jobs.page = ApiPage(data: [aJob(id: 'recovered')]);
      await tester.tap(find.byKey(const Key('customer-jobs-retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('job-recovered')), findsOneWidget);
      expect(find.byKey(const Key('customer-jobs-failed')), findsNothing);
    });
  });

  group('paging', () {
    testWidgets('asks for the next page with the cursor it was given, and appends it',
        (tester) async {
      const token = 'MR8yMDI2LTA4LTExVDAzOjMwOjAwWh8wMTk4ZjJjMS02YjQwLTdhMTE';
      final jobs = FakeJobsRepository()
        ..page = ApiPage(data: [aJob(id: 'page-one')], nextCursor: token, hasMore: true);

      await openCustomerShell(tester, jobs);

      // Said out loud, because a group holds what has been read rather than every job in that
      // status. A heading with three drafts under it when there are five is a quiet lie.
      expect(find.byKey(const Key('customer-jobs-partial')), findsOneWidget);

      jobs.page = ApiPage(data: [aJob(id: 'page-two', status: JobStatus.open)]);
      await tester.tap(find.byKey(const Key('customer-jobs-more')));
      await tester.pumpAndSettle();

      expect(jobs.callsTo('list').last.cursor, token,
          reason: 'the cursor is opaque and goes back exactly as it arrived');
      expect(find.byKey(const Key('job-page-one')), findsOneWidget);
      expect(find.byKey(const Key('job-page-two')), findsOneWidget);
      expect(find.byKey(const Key('customer-jobs-more')), findsNothing);
    });

    testWidgets('offers nothing more when the platform says there is nothing more',
        (tester) async {
      final jobs = FakeJobsRepository()..page = ApiPage(data: [aJob()]);

      await openCustomerShell(tester, jobs);

      expect(find.byKey(const Key('customer-jobs-more')), findsNothing);
      expect(find.byKey(const Key('customer-jobs-partial')), findsNothing);
    });
  });

  group('the two halves stay separate', () {
    testWidgets('a provider sees neither the job list nor the way to publish one', (tester) async {
      // Docs/07 §1: a customer never sees provider surfaces or the reverse. The platform would
      // refuse a provider creating a job (`jobs_customer_only`), and that is a different thing
      // from the app not offering it.
      final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.provider);
      final jobs = FakeJobsRepository();

      await tester.pumpWidget(signupApp(identity, jobs: jobs));
      await tester.pumpAndSettle();
      await signInThrough(tester);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-provider')), findsOneWidget);
      expect(find.byKey(const Key('shell-customer')), findsNothing);
      expect(find.byKey(const Key('new-job')), findsNothing);
      expect(jobs.calls, isEmpty, reason: 'a provider must not read the customer job list');
    });
  });
}
