// SHIP-74 — "Optional budget captured; full job reviewed before publish".
//
// This is the screen that finishes the journey the whole marketplace waited on: until SHIP-63
// there was no way to move a job to Open at all, and until this screen there was no way to ask.
// So the tests walk the entire wizard rather than pumping the route — a customer signs in,
// describes a delivery over four steps, and publishes it.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';

import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_goods_categories_repository.dart';
import 'fake_jobs_repository.dart';

const _draftId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

/// A draft with everything the platform requires to publish.
Job aPublishableDraft() {
  return aJob(id: _draftId, pickup: aResolvedLocation(), dropoff: anUnresolvedLocation()).copyWith(
    goodsDescription: 'Two-seater sofa, wrapped, no legs attached',
    goodsCategory: 'furniture',
    lengthCm: 190,
    weightKg: 45.5,
    vehicleRequirement: 'Ute with a tailgate lifter',
    handlingNotes: 'Second-floor walk-up, no lift.',
    pickupWindow: const JobTimeWindow(start: '2026-09-03T12:00:00Z'),
  );
}

/// Walks a signed-in customer through the whole wizard to the review step.
Future<(FakeJobsRepository, FakeGoodsCategoriesRepository)> openReview(
  WidgetTester tester, {
  Job? draft,
}) async {
  final identity = FakeIdentityRepository();
  final jobs = FakeJobsRepository();
  final goods = FakeGoodsCategoriesRepository();

  final saved = draft ?? aPublishableDraft();
  jobs.draft = (_) => saved;
  jobs.detail = (_) => saved;

  tester.view.physicalSize = const Size(1000, 4000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  await tester.pumpWidget(signupApp(identity, jobs: jobs, goodsCategories: goods));
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

  await tester.tap(find.byKey(const Key('job-goods-submit')));
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(const Key('job-schedule-submit')));
  await tester.pumpAndSettle();

  jobs.calls.clear();
  return (jobs, goods);
}

Future<void> acceptTerms(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('review-accepts-terms')));
  await tester.pumpAndSettle();
}

Future<void> publish(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('job-review-publish')));
  await tester.pumpAndSettle();
}

void main() {
  group('reviewing the full job before publishing', () {
    testWidgets('shows what was captured on every earlier step, not just this one',
        (tester) async {
      await openReview(tester);

      // "Reviewed before publish" means the whole job. A summary of only what this screen
      // collected would let a customer publish addresses and dates they last saw three screens
      // ago.
      expect(find.text('12 Smith Street, Newtown NSW 2042'), findsOneWidget);
      expect(find.text('Lot 14 Boundary Road, Coonabarabran NSW 2357'), findsOneWidget);
      expect(find.text('Two-seater sofa, wrapped, no legs attached'), findsOneWidget);
      expect(find.text('190cm long, 45.5kg'), findsOneWidget);
      expect(find.text('Ute with a tailgate lifter'), findsOneWidget);
      expect(find.text('Second-floor walk-up, no lift.'), findsOneWidget);
      // A window with only an earliest date reads as an opening rather than as a day: the
      // customer said "not before this", and rendering a bare date would say "on this day".
      expect(find.text('From 3 Sep 2026'), findsOneWidget);
    });

    testWidgets('renders the goods category as its label, not the code it is stored as',
        (tester) async {
      await openReview(tester);

      // The job stores `furniture`; showing that to a customer would be showing them the wire.
      expect(find.text('Furniture and white goods'), findsOneWidget);
    });

    testWidgets('a category the catalogue no longer serves falls back to the code', (tester) async {
      await openReview(
        tester,
        draft: aPublishableDraft().copyWith(goodsCategory: 'withdrawn_since'),
      );

      // Not nothing: it is what the job actually says, and a customer looking at their own job is
      // entitled to see it. `Docs/07` §3 leaves the decision about it to the platform.
      expect(find.text('withdrawn_since'), findsOneWidget);
    });

    testWidgets('a field with nothing in it says so rather than being left out', (tester) async {
      await openReview(
        tester,
        draft: aJob(id: _draftId, pickup: aResolvedLocation()),
      );

      // The point of a review screen: a customer scanning for what they forgot cannot find an
      // absence.
      expect(find.byKey(const Key('review-goods-description')), findsOneWidget);
      expect(find.text('Not given'), findsWidgets);
    });
  });

  group('the optional budget', () {
    testWidgets('is captured in cents, from dollars typed', (tester) async {
      final (jobs, _) = await openReview(tester);

      await tester.enterText(find.byKey(const Key('review-budget')), '1500.50');
      await tester.pump();
      await acceptTerms(tester);
      await publish(tester);

      // Minor units as a whole number, because money is never a float and a JSON number written
      // as `1500.50` is one. The conversion is done on the text, so no digit is lost.
      expect(jobs.callsTo('update').single.fields, <String, Object?>{'budget_cents': 150050});
    });

    testWidgets('is optional, and a blank one publishes without a budget call', (tester) async {
      final (jobs, _) = await openReview(tester);

      await acceptTerms(tester);
      await publish(tester);

      // Nothing changed, so nothing is saved. This is also what keeps a retry after a refused
      // publication from PATCHing a job that is by then open.
      expect(jobs.callsTo('update'), isEmpty);
      expect(jobs.callsTo('publish'), hasLength(1));
    });

    testWidgets('a budget the customer clears is sent as the value that clears it',
        (tester) async {
      final (jobs, _) = await openReview(
        tester,
        draft: aPublishableDraft().copyWith(budgetCents: 150000),
      );

      expect(find.text('1500.00'), findsOneWidget);

      await tester.enterText(find.byKey(const Key('review-budget')), '');
      await tester.pump();
      await acceptTerms(tester);
      await publish(tester);

      expect(jobs.callsTo('update').single.fields, <String, Object?>{'budget_cents': 0});
    });

    testWidgets(r'a stored budget of zero shows as blank, not as $0.00', (tester) async {
      await openReview(tester, draft: aPublishableDraft().copyWith(budgetCents: 0));

      expect(
        tester.widget<TextFormField>(find.byKey(const Key('review-budget'))).controller?.text,
        isEmpty,
      );
    });

    testWidgets('an amount that is not an amount is refused before it is sent', (tester) async {
      final (jobs, _) = await openReview(tester);

      await tester.enterText(find.byKey(const Key('review-budget')), 'four fifty');
      await tester.pump();
      await acceptTerms(tester);
      await publish(tester);

      // The one local rule on this screen, and it is a *shape* rule rather than a limit: the form
      // has to turn what was typed into a whole number of cents and cannot send "four fifty". The
      // maximum is the platform's and arrives as `out_of_range`.
      expect(jobs.callsTo('publish'), isEmpty);
    });

    testWidgets('the customer is told the budget reaches no provider', (tester) async {
      await openReview(tester);

      // `Docs/01` §4.3 keeps it private in every form, and saying so is what stops a customer
      // leaving it blank out of suspicion — or filling it in believing it is a signal.
      expect(find.byKey(const Key('review-privacy-note')), findsOneWidget);
      expect(find.textContaining('never shown to anyone'), findsOneWidget);
    });
  });

  group('publishing', () {
    testWidgets('sends the declaration and nothing else, and lands the customer back home',
        (tester) async {
      final (jobs, _) = await openReview(tester);

      await acceptTerms(tester);
      await publish(tester);

      final call = jobs.callsTo('publish').single;
      expect(call.jobId, _draftId);
      expect(call.fields, <String, Object?>{'accepts_terms': true});
      // Status is never a settable field: the client names an intent and the platform decides what
      // the state becomes (`Docs/02` §2).
      expect(call.fields.containsKey('status'), isFalse);

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
    });

    testWidgets('the declaration is asked for every time and gates the button', (tester) async {
      await openReview(tester);

      // `Docs/04` §2 requires it "for every job" rather than once per account: the declaration is
      // about *these* goods, made by a customer who has just described them.
      expect(
        tester.widget<FilledButton>(find.byKey(const Key('job-review-publish'))).onPressed,
        isNull,
      );

      await acceptTerms(tester);

      expect(
        tester.widget<FilledButton>(find.byKey(const Key('job-review-publish'))).onPressed,
        isNotNull,
      );
    });

    testWidgets('the job comes back open, which is the platform\'s decision and not the app\'s',
        (tester) async {
      final (jobs, _) = await openReview(tester);

      await acceptTerms(tester);
      await publish(tester);

      expect(jobs.detail(_draftId).status, JobStatus.open);
      expect(jobs.detail(_draftId).termsAcceptedAt, isNotNull);
    });
  });

  group('what publication can refuse', () {
    testWidgets('missing fields are named one by one, with the step each belongs to',
        (tester) async {
      final (jobs, _) = await openReview(tester);

      jobs.failures['publish'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'That job cannot be published as it stands.',
        details: [
          ApiFieldError(
            field: 'goods_category',
            code: 'required',
            message: 'Choose what kind of goods these are.',
          ),
          ApiFieldError(
            field: 'pickup_window.start',
            code: 'required',
            message: 'Say the earliest date the goods can be collected.',
          ),
        ],
      );

      await acceptTerms(tester);
      await publish(tester);

      // Every one at once, because the platform lists them all — and a customer told only "that
      // job is incomplete" on the last screen of a wizard has nowhere to go.
      expect(find.byKey(const Key('review-still-missing')), findsOneWidget);
      expect(find.textContaining('Choose what kind of goods these are.'), findsOneWidget);
      expect(find.textContaining('(Goods step)'), findsOneWidget);
      expect(find.textContaining('(Schedule step)'), findsOneWidget);
    });

    testWidgets('a field the app has never heard of is shown without a made-up step',
        (tester) async {
      final (jobs, _) = await openReview(tester);

      jobs.failures['publish'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'That job cannot be published as it stands.',
        details: [
          ApiFieldError(field: 'something_new', code: 'required', message: 'This is required.'),
        ],
      );

      await acceptTerms(tester);
      await publish(tester);

      // A wrong signpost is worse than none. The platform can name a field this build has never
      // seen, and guessing which step owns it would send the customer to the wrong screen.
      expect(find.text('This is required.'), findsOneWidget);
    });

    testWidgets('a prohibited category is shown in the catalogue\'s own words', (tester) async {
      final (jobs, _) = await openReview(tester);

      jobs.failures['publish'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'jobs_prohibited_category',
        message: 'Shipper does not carry dangerous goods.',
      );

      await acceptTerms(tester);
      await publish(tester);

      // In the banner rather than under a field: nothing on this screen can fix it, and the goods
      // step is where it changes.
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.text('Shipper does not carry dangerous goods.'), findsOneWidget);
      expect(find.byKey(const Key('review-still-missing')), findsNothing);
    });

    testWidgets('an unverified account is a refusal about the account, not the job',
        (tester) async {
      final (jobs, _) = await openReview(tester);

      jobs.failures['publish'] = const ApiErrorResponse(
        statusCode: 403,
        code: 'jobs_customer_not_verified',
        message: 'Verify your phone number before publishing a job.',
      );

      await acceptTerms(tester);
      await publish(tester);

      expect(find.text('Verify your phone number before publishing a job.'), findsOneWidget);
      // Still on the review step: the job is fine and the customer has something else to do.
      expect(find.byKey(const Key('job-review-form')), findsOneWidget);
    });

    testWidgets('a job that is no longer a draft is reported, not argued with', (tester) async {
      final (jobs, _) = await openReview(tester);

      jobs.failures['publish'] = const ApiErrorResponse(
        statusCode: 409,
        code: 'jobs_not_publishable',
        message: 'This job has already been published.',
      );

      await acceptTerms(tester);
      await publish(tester);

      expect(find.text('This job has already been published.'), findsOneWidget);
    });
  });

  group('idempotency', () {
    testWidgets('a retry after an unknown outcome reuses the publication key', (tester) async {
      final (jobs, _) = await openReview(tester);

      await acceptTerms(tester);
      jobs.failures['publish'] = const ApiUnreachable(timedOut: true);
      await publish(tester);
      await publish(tester);

      final keys = jobs.callsTo('publish').map((c) => c.idempotencyKey).toList();
      expect(keys, hasLength(2));
      // The connection dropped, so nothing established whether the job was published. Retrying
      // under the same key replays the stored answer rather than publishing twice.
      expect(keys.first, keys.last);
    });

    testWidgets('a publication key is never the key a budget save used', (tester) async {
      final (jobs, _) = await openReview(tester);

      await tester.enterText(find.byKey(const Key('review-budget')), '1500');
      await tester.pump();
      await acceptTerms(tester);
      await publish(tester);

      // Two actions, two keys. Sharing one would make the publication a replay of the edit.
      expect(
        jobs.callsTo('update').single.idempotencyKey,
        isNot(jobs.callsTo('publish').single.idempotencyKey),
      );
    });
  });
}
