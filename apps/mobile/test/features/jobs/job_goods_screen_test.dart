// SHIP-72 — "Category, description, dimensions, and weight captured".
//
// Driven through the real app: the router, the guard the step had to be added to, the session, the
// draft controller and the screen. Only the token store and the two repositories are substituted,
// which is what makes these demonstrations of the step rather than of a widget in isolation.
//
// The step is reached the way a person reaches it — by finishing the locations step and tapping
// Continue — because the route is new and a guard that refused `/jobs/{id}/goods` would show up
// here rather than only on a device. A route reachable only through an identifier looks, from the
// outside, like a button that does nothing.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';

import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_goods_categories_repository.dart';
import 'fake_jobs_repository.dart';

const _draftId = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

/// Signs a customer in, saves the locations step, and continues into the goods step.
Future<(FakeJobsRepository, FakeGoodsCategoriesRepository)> openGoods(
  WidgetTester tester, {
  Job? draft,
  FakeGoodsCategoriesRepository? catalogue,
}) async {
  final identity = FakeIdentityRepository();
  final jobs = FakeJobsRepository();
  final goods = catalogue ?? FakeGoodsCategoriesRepository();

  final saved = draft ?? aJob(id: _draftId, pickup: aResolvedLocation());
  jobs.draft = (_) => saved;
  jobs.detail = (_) => saved;

  // Four radio tiles, a description box, four measurements and a button are taller than the
  // 800x600 a widget test defaults to, and a `ListView` does not build what is off-screen — so
  // the submit would not be in the tree at all. A taller window is the honest fix: scrolling to it
  // would test the scroll rather than the step.
  tester.view.physicalSize = const Size(1000, 3000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  await tester.pumpWidget(signupApp(identity, jobs: jobs, goodsCategories: goods));
  await tester.pumpAndSettle();

  await signInThrough(tester);

  await tester.tap(find.byKey(const Key('new-job')));
  await tester.pumpAndSettle();

  await tester.enterText(find.byKey(const Key('pickup-line')), '12 Smith Street');
  await tester.enterText(find.byKey(const Key('dropoff-line')), 'Lot 14 Boundary Road');
  await tester.pump();
  await tester.tap(find.byKey(const Key('job-locations-submit')));
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('job-locations-continue')));
  await tester.pumpAndSettle();

  return (jobs, goods);
}

Future<void> saveGoods(WidgetTester tester) async {
  await tester.tap(find.byKey(const Key('job-goods-submit')));
  await tester.pumpAndSettle();
}

void main() {
  group('reaching the step', () {
    testWidgets('the locations step continues into it, and the guard admits the route',
        (tester) async {
      await openGoods(tester);

      expect(find.byKey(const Key('job-goods-form')), findsOneWidget);
      expect(find.byKey(const Key('job-wizard-step')), findsOneWidget);
      expect(find.text('Step 2 of 4 · Goods'), findsOneWidget);
    });

    testWidgets('the draft is read once, not once per step', (tester) async {
      final (jobs, _) = await openGoods(tester);

      // The step is pushed, so the locations step stays mounted and the family entry stays alive.
      // One read is the whole point of `push` over `go` here.
      expect(jobs.callsTo('read'), hasLength(1));
    });
  });

  group('capturing the four things the ticket names', () {
    testWidgets('sends the category code, the description, the dimensions and the weight',
        (tester) async {
      final (jobs, _) = await openGoods(tester);

      await tester.tap(find.byKey(const Key('goods-category-furniture')));
      await tester.pump();
      await tester.enterText(
        find.byKey(const Key('goods-description')),
        'Two-seater sofa, wrapped, no legs attached',
      );
      await tester.enterText(find.byKey(const Key('goods-length')), '190');
      await tester.enterText(find.byKey(const Key('goods-width')), '90');
      await tester.enterText(find.byKey(const Key('goods-height')), '85');
      await tester.enterText(find.byKey(const Key('goods-weight')), '45.5');
      await tester.pump();

      await saveGoods(tester);

      final call = jobs.callsTo('update').single;
      expect(call.jobId, _draftId);
      expect(call.fields, <String, Object?>{
        // The **code**, never the label. The label is wording and changes; the code is what the
        // job stores, and a client that sent 'Furniture and white goods' would be refused.
        'goods_category': 'furniture',
        'goods_description': 'Two-seater sofa, wrapped, no legs attached',
        'length_cm': 190,
        'width_cm': 90,
        'height_cm': 85,
        'weight_kg': 45.5,
      });
    });

    testWidgets('an empty measurement is sent as the value that clears it', (tester) async {
      final (jobs, _) = await openGoods(tester);

      await tester.tap(find.byKey(const Key('goods-category-general_freight')));
      await tester.pump();
      await tester.enterText(find.byKey(const Key('goods-description')), 'A pallet');
      await tester.pump();
      await saveGoods(tester);

      // `0` rather than omitted: `PATCH` touches only the fields present, so leaving a measurement
      // out would mean "keep whatever is there" — which is not what an empty box means to the
      // customer who just emptied it. The contract names `0` as the clearing value.
      final call = jobs.callsTo('update').single;
      expect(call.fields['length_cm'], 0);
      expect(call.fields['width_cm'], 0);
      expect(call.fields['height_cm'], 0);
      expect(call.fields['weight_kg'], 0);
    });

    testWidgets('a saved draft comes back with its own values in the fields', (tester) async {
      await openGoods(
        tester,
        draft: aJob(id: _draftId, pickup: aResolvedLocation()).copyWith(
          goodsCategory: 'furniture',
          goodsDescription: 'A wardrobe',
          lengthCm: 200,
          weightKg: 60,
        ),
      );

      expect(find.text('A wardrobe'), findsOneWidget);
      expect(find.text('200'), findsOneWidget);
      // 60.0 would read as a precision nobody claimed.
      expect(find.text('60'), findsOneWidget);

      final chosen = tester.widget<RadioListTile<String>>(
        find.byKey(const Key('goods-category-furniture')),
      );
      expect(chosen.value, 'furniture');
    });

    testWidgets('a measurement the customer cleared shows as blank, not as zero', (tester) async {
      // What a job that has been through this form once and had its length cleared looks like:
      // the platform stores `0`, and rendering that as "0" tells a customer their sofa is zero
      // centimetres long.
      await openGoods(
        tester,
        draft: aJob(id: _draftId, pickup: aResolvedLocation())
            .copyWith(lengthCm: 0, weightKg: 0),
      );

      expect(
        tester.widget<TextFormField>(find.byKey(const Key('goods-length'))).controller?.text,
        isEmpty,
      );
      expect(
        tester.widget<TextFormField>(find.byKey(const Key('goods-weight'))).controller?.text,
        isEmpty,
      );
    });
  });

  group('the catalogue is the platform\'s, not the app\'s', () {
    testWidgets('every entry is offered, including the ones Shipper will not carry',
        (tester) async {
      await openGoods(tester);

      expect(find.byKey(const Key('goods-category-general_freight')), findsOneWidget);
      expect(find.byKey(const Key('goods-category-furniture')), findsOneWidget);
      // Shown rather than left out, which is what lets a customer see what is not taken instead of
      // inferring it from an absence.
      expect(find.byKey(const Key('goods-category-dangerous_goods')), findsOneWidget);
      expect(find.byKey(const Key('goods-category-live_animals')), findsOneWidget);
    });

    testWidgets('choosing a refused category warns and still saves', (tester) async {
      final (jobs, _) = await openGoods(tester);

      await tester.tap(find.byKey(const Key('goods-category-dangerous_goods')));
      await tester.pump();

      expect(find.byKey(const Key('goods-category-refused')), findsOneWidget);

      // `Docs/07` §3: the app may warn, and the platform decides. `Docs/01` §4.1 lets a customer
      // sketch a job and come back, so the draft saves — and `POST /v1/jobs/{id}/publish` is what
      // refuses it, by name (SHIP-59).
      expect(
        tester.widget<FilledButton>(find.byKey(const Key('job-goods-submit'))).onPressed,
        isNotNull,
      );

      await tester.enterText(find.byKey(const Key('goods-description')), 'Two gas cylinders');
      await tester.pump();
      await saveGoods(tester);

      expect(jobs.callsTo('update').single.fields['goods_category'], 'dangerous_goods');
    });

    testWidgets('the list says it is provisional, once rather than per entry', (tester) async {
      await openGoods(tester);

      // X-4 is open and `Docs/11` §5 records the reduced form the list ships in, so every entry
      // carries `provisional: true` — thirteen identical badges would say nothing about any of
      // them.
      expect(find.byKey(const Key('goods-categories-provisional')), findsOneWidget);
    });

    testWidgets('a catalogue that will not load leaves a retry, never a guessed list',
        (tester) async {
      final catalogue = FakeGoodsCategoriesRepository()
        ..failure = const ApiUnreachable(timedOut: true);

      await openGoods(tester, catalogue: catalogue);

      expect(find.byKey(const Key('goods-categories-unavailable')), findsOneWidget);
      // No compiled fallback anywhere. A category withdrawn on legal advice has to stop being
      // offered without a store release, which is the whole reason the list is served.
      expect(find.byKey(const Key('goods-categories')), findsNothing);
      expect(find.byKey(const Key('goods-categories-retry')), findsOneWidget);
    });

    testWidgets('a catalogue that will not load does not cost the draft its category',
        (tester) async {
      final catalogue = FakeGoodsCategoriesRepository()
        ..failure = const ApiUnreachable(timedOut: true);

      final (jobs, _) = await openGoods(
        tester,
        draft: aJob(id: _draftId, pickup: aResolvedLocation()).copyWith(
          goodsCategory: 'furniture',
          goodsDescription: 'A wardrobe',
        ),
        catalogue: catalogue,
      );

      await saveGoods(tester);

      // The code is seeded from the **draft** rather than from the catalogue. Seeded the other way
      // round, this save would send `goods_category: ''` and silently clear a category the
      // customer chose last week.
      expect(jobs.callsTo('update').single.fields['goods_category'], 'furniture');
    });
  });

  group('what the platform refuses', () {
    testWidgets('a field message lands under the field it names', (tester) async {
      final (jobs, _) = await openGoods(tester);

      jobs.failures['update'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'That job could not be saved.',
        details: [
          ApiFieldError(
            field: 'goods_description',
            code: 'too_long',
            message: 'Keep this under 2000 characters.',
          ),
        ],
      );

      await tester.enterText(find.byKey(const Key('goods-description')), 'A pallet');
      await tester.pump();
      await saveGoods(tester);

      expect(find.text('Keep this under 2000 characters.'), findsOneWidget);
      // Under the input rather than in the banner: the platform named a field, so the customer has
      // something to correct.
      expect(find.byKey(const Key('failure-banner')), findsNothing);
      expect(find.byKey(const Key('job-goods-form')), findsOneWidget);
    });

    testWidgets('a refusal about the request goes in the banner, not under an input',
        (tester) async {
      final (jobs, _) = await openGoods(tester);

      jobs.failures['update'] = const ApiErrorResponse(
        statusCode: 409,
        code: 'jobs_not_a_draft',
        message: 'This job has already been published.',
      );

      await saveGoods(tester);

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.text('This job has already been published.'), findsOneWidget);
    });

    testWidgets('a draft that cannot be read offers a retry and no form', (tester) async {
      final identity = FakeIdentityRepository();
      final jobs = FakeJobsRepository()..failures['read'] = const ApiUnreachable(timedOut: true);

      tester.view.physicalSize = const Size(1000, 3000);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      final saved = aJob(id: _draftId, pickup: aResolvedLocation());
      jobs.draft = (_) => saved;
      jobs.detail = (_) => saved;

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

      // The one thing this state must not do is offer to save. An edit sent against a draft
      // nobody could read is an edit whose effect nobody can predict.
      expect(find.byKey(const Key('job-wizard-unreadable')), findsOneWidget);
      expect(find.byKey(const Key('job-goods-form')), findsNothing);

      await tester.tap(find.byKey(const Key('job-wizard-retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('job-goods-form')), findsOneWidget);
    });
  });

  group('idempotency', () {
    testWidgets('a retry after an unknown outcome reuses the key', (tester) async {
      final (jobs, _) = await openGoods(tester);

      jobs.failures['update'] = const ApiUnreachable(timedOut: true);
      await tester.enterText(find.byKey(const Key('goods-description')), 'A pallet');
      await tester.pump();
      await saveGoods(tester);

      await saveGoods(tester);

      final keys = jobs.callsTo('update').map((c) => c.idempotencyKey).toList();
      expect(keys, hasLength(2));
      // The connection dropped, so nothing established whether the platform stored the edit. That
      // is exactly what the key exists for: the retry replays the stored answer rather than
      // applying the edit twice.
      expect(keys.first, keys.last);
    });

    testWidgets('correcting a value and saving again is a new action', (tester) async {
      final (jobs, _) = await openGoods(tester);

      jobs.failures['update'] = const ApiErrorResponse(
        statusCode: 422,
        code: 'validation_failed',
        message: 'That job could not be saved.',
        details: [
          ApiFieldError(field: 'goods_description', code: 'required', message: 'Required.'),
        ],
      );
      await saveGoods(tester);

      await tester.enterText(find.byKey(const Key('goods-description')), 'A pallet');
      await tester.pump();
      await saveGoods(tester);

      final keys = jobs.callsTo('update').map((c) => c.idempotencyKey).toList();
      // The platform saw the first request and refused it, so retrying under the same key could
      // only replay that refusal — and the body has changed besides, which the platform
      // fingerprints and would refuse as `idempotency_key_reused`.
      expect(keys.first, isNot(keys.last));
    });
  });
}
