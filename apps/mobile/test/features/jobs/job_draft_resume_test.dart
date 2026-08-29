// SHIP-75 — "A partially completed job survives app restart and can be resumed".
//
// # The survival half is a property of where the draft lives, and the test says so
//
// Nothing is kept on the device to survive. `POST /v1/jobs` stores the draft at the first step
// (SHIP-71), so the job that outlives the process is the platform's row — which is why it also
// survives a reinstall and appears on a second handset, and why there is no local cache here to go
// stale. The restart below is therefore a **real** restart: the widget tree, every provider and
// every controller are thrown away and built again, and only the fake repository — standing in for
// the platform — persists.
//
// # The resume half is which step it reopens at
//
// A customer who filled in three steps last week should not walk the first two again. Each case
// below leaves a different field missing and asserts the step that opens, including the one that
// only exists because a draft may legitimately have no addresses at all.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/job_wizard.dart';

import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_goods_categories_repository.dart';
import 'fake_jobs_repository.dart';

const _draftId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

/// A draft with everything the platform requires to publish.
Job aCompleteDraft() {
  return aJob(id: _draftId, pickup: aResolvedLocation(), dropoff: anUnresolvedLocation()).copyWith(
    goodsDescription: 'Two-seater sofa',
    goodsCategory: 'furniture',
    pickupWindow: const JobTimeWindow(start: '2026-09-03T12:00:00Z'),
  );
}

/// Signs a customer in and stops at their own job list, with [draft] in it.
Future<FakeJobsRepository> openJobList(WidgetTester tester, Job draft) async {
  final identity = FakeIdentityRepository();
  final jobs = FakeJobsRepository()
    ..page = ApiPage<Job>(data: <Job>[draft])
    ..detail = (_) => draft;

  tester.view.physicalSize = const Size(1000, 3600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  await tester.pumpWidget(
    signupApp(identity, jobs: jobs, goodsCategories: FakeGoodsCategoriesRepository()),
  );
  await tester.pumpAndSettle();
  await signInThrough(tester);

  return jobs;
}

Future<void> resume(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('job-resume-$_draftId')));
  await tester.pumpAndSettle();
}

void main() {
  group('the draft that is picked up is the platform\'s, not the device\'s', () {
    testWidgets('a half-finished job is still there after the app is restarted', (tester) async {
      // Both addresses and the goods, and no collection date: a customer three steps in who put
      // the phone down. `firstIncompleteFor` reads that as the schedule step.
      final partial = aCompleteDraft().copyWith(pickupWindow: null);

      final identity = FakeIdentityRepository();
      final jobs = FakeJobsRepository()
        ..page = ApiPage<Job>(data: <Job>[partial])
        ..detail = (_) => partial;

      tester.view.physicalSize = const Size(1000, 3600);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      await tester.pumpWidget(
        signupApp(identity, jobs: jobs, goodsCategories: FakeGoodsCategoriesRepository()),
      );
      await tester.pumpAndSettle();
      await signInThrough(tester);
      expect(find.byKey(const Key('job-resume-$_draftId')), findsOneWidget);

      // **The restart, and the empty frame is the part that makes it one.** Pumping a second
      // `signupApp` on its own is not a restart: `ProviderScope` is a `StatefulWidget`, so a new
      // instance of the same type reuses the mounted `State` and with it the whole container —
      // the app would still be sitting on the shell, and the test would prove nothing. Pumping
      // something else first unmounts the tree, which disposes every provider and every
      // controller. What is left is the fake standing in for the platform.
      await tester.pumpWidget(const SizedBox.shrink());
      await tester.pumpAndSettle();

      await tester.pumpWidget(
        signupApp(identity, jobs: jobs, goodsCategories: FakeGoodsCategoriesRepository()),
      );
      await tester.pumpAndSettle();
      await signInThrough(tester);

      expect(find.byKey(const Key('job-resume-$_draftId')), findsOneWidget);

      await resume(tester);

      // And it opens where it was left, with what was typed a week ago in the fields.
      expect(find.byKey(const Key('job-schedule-form')), findsOneWidget);
    });

    testWidgets('only a draft offers to be continued', (tester) async {
      // Every other status is a job that has been published, and the wizard is not how a published
      // job is changed — cancelling it is (SHIP-77).
      await openJobList(tester, aCompleteDraft().copyWith(status: JobStatus.open));

      expect(find.byKey(const Key('job-resume-$_draftId')), findsNothing);
    });
  });

  group('it reopens at the step that is not finished', () {
    testWidgets('a draft missing its goods opens the goods step', (tester) async {
      await openJobList(
        tester,
        aCompleteDraft().copyWith(goodsCategory: '', goodsDescription: ''),
      );
      await resume(tester);

      expect(find.byKey(const Key('job-goods-form')), findsOneWidget);
      expect(find.text('Step 2 of 4 · Goods'), findsOneWidget);
    });

    testWidgets('a draft missing only its earliest collection date opens the schedule step',
        (tester) async {
      await openJobList(tester, aCompleteDraft().copyWith(pickupWindow: null));
      await resume(tester);

      expect(find.byKey(const Key('job-schedule-form')), findsOneWidget);
    });

    testWidgets('a draft with everything the platform requires opens the review step',
        (tester) async {
      await openJobList(tester, aCompleteDraft());
      await resume(tester);

      expect(find.byKey(const Key('job-review-form')), findsOneWidget);
      expect(find.text('Step 4 of 4 · Review'), findsOneWidget);
    });

    testWidgets('a draft with no addresses opens the locations step, on a route that names it',
        (tester) async {
      // The case the second locations route exists for. `POST /v1/jobs` accepts an empty body and
      // `Docs/01` §4.1 lets a customer start a job and come back, so this draft is legitimate —
      // and `/jobs/new` could not reopen it, because that route creates a *second* job.
      await openJobList(tester, aJob(id: _draftId));
      await resume(tester);

      expect(find.byKey(const Key('job-locations-edit-form')), findsOneWidget);
      expect(find.text('Step 1 of 4 · Locations'), findsOneWidget);
    });
  });

  group('resuming the locations step edits the draft rather than making a second one', () {
    testWidgets('it patches the job it was opened on', (tester) async {
      final jobs = await openJobList(tester, aJob(id: _draftId));
      await resume(tester);

      await tester.enterText(find.byKey(const Key('pickup-line')), '12 Smith Street');
      await tester.enterText(find.byKey(const Key('dropoff-line')), 'Lot 14 Boundary Road');
      await tester.pump();
      await tester.tap(find.byKey(const Key('job-locations-edit-submit')));
      await tester.pumpAndSettle();

      // The failure this route exists to prevent: a customer who came back to finish a job ending
      // up with two, and seeing the abandoned one in their own list with nothing to explain it.
      expect(jobs.callsTo('create'), isEmpty);
      expect(jobs.callsTo('update').single.jobId, _draftId);
      expect(find.byKey(const Key('job-goods-form')), findsOneWidget);
    });

    testWidgets('the address already stored comes back in the boxes', (tester) async {
      // One address given and the other not, which is what puts a draft on the locations step with
      // something already in it.
      final jobs = await openJobList(tester, aJob(id: _draftId, pickup: aResolvedLocation()));
      await resume(tester);

      expect(find.byKey(const Key('job-locations-edit-form')), findsOneWidget);
      expect(
        tester.widget<TextFormField>(find.byKey(const Key('pickup-line'))).controller?.text,
        '12 Smith Street',
      );
      expect(
        tester.widget<TextFormField>(find.byKey(const Key('pickup-postcode'))).controller?.text,
        '2042',
      );

      await tester.enterText(find.byKey(const Key('dropoff-line')), 'Lot 14 Boundary Road');
      await tester.pump();
      await tester.tap(find.byKey(const Key('job-locations-edit-submit')));
      await tester.pumpAndSettle();

      // Both addresses go back, because an address is replaced as a whole rather than merged part
      // by part — so the one that was already there has to be re-sent, not omitted.
      final pickup = jobs.callsTo('update').single.fields['pickup']! as Map<String, Object?>;
      expect(pickup['line'], '12 Smith Street');
      expect(pickup['postcode'], '2042');
    });
  });

  group('the step it chooses mirrors the platform, and is never the thing that decides',
      () {
    test('the required set is publishable()\'s, and size is deliberately not in it', () {
      // `internal/jobs/publish.go` requires both addresses, a goods description, a goods category
      // and the start of the pickup window — and deliberately not weight or dimensions, which
      // `Docs/11` §9 records as the owner's to revisit. A draft with no size at all is therefore
      // ready to review, and if that decision changes this assertion is what fails first.
      expect(JobWizardStep.firstIncompleteFor(aCompleteDraft()), JobWizardStep.review);

      expect(
        JobWizardStep.firstIncompleteFor(aCompleteDraft().copyWith(dropoff: null)),
        JobWizardStep.locations,
      );
      expect(
        JobWizardStep.firstIncompleteFor(aCompleteDraft().copyWith(goodsCategory: '')),
        JobWizardStep.goods,
      );
      expect(
        JobWizardStep.firstIncompleteFor(aCompleteDraft().copyWith(goodsDescription: '')),
        JobWizardStep.goods,
      );
      expect(
        JobWizardStep.firstIncompleteFor(
          aCompleteDraft().copyWith(pickupWindow: const JobTimeWindow()),
        ),
        JobWizardStep.schedule,
      );
    });

    test('an address stored with nothing in it counts as missing', () {
      // The platform's `requiredAddress` checks `IsZero`, not presence: a `JobLocation` whose four
      // parts are all empty is an address nobody gave. A client that tested for the object would
      // send a customer to the review step to be refused there.
      expect(
        JobWizardStep.firstIncompleteFor(
          aCompleteDraft().copyWith(pickup: const JobLocation()),
        ),
        JobWizardStep.locations,
      );
    });

    test('every step has a route that names the draft', () {
      // Including the first, which is the whole of what makes a draft resumable: `/jobs/new`
      // carries no id, so resuming into locations needs a second route that does.
      for (final step in JobWizardStep.values) {
        expect(step.pathFor(_draftId), contains(_draftId));
      }
    });
  });
}
