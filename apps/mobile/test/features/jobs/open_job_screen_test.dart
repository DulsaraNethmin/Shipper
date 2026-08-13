// SHIP-100 — the first half of "a provider can review a job and submit a bid".
//
// Driven through the real app: the session, the router, the shell, the feed and the card that leads
// here. Only the token store and the repositories are substituted, so a guard that refused the route
// — which is exactly what SHIP-49's guard did to the first screen hung off the shell — fails here
// rather than only on a device.
//
// # Three things this file is careful about
//
// **The route is identifier-bearing**, which is the case a notification payload delivers somebody
// straight to (Docs/07 §5). So it is exercised both ways: through the card, and with no button
// involved at all.
//
// **The 404.** A job outside this provider's eligibility, a job that has been awarded, and a job
// that never existed are one answer byte-identically (SHIP-83). The screen must not try to be more
// specific than the platform was, and must not report it as a fault.
//
// **The budget.** Docs/01 §4.3 keeps the customer's maximum away from a provider in every form. The
// last group renders a payload carrying one under four names and asserts that nothing of it reaches
// the screen.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';
import 'package:shipper/shared/formatting/dates.dart';

import '../bidding/fake_bidding_repository.dart';
import '../fleet/fleet_app.dart' show deepLinkTo;
import 'fake_open_jobs_repository.dart';
import 'open_job_app.dart';

const _sofa = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

FakeOpenJobsRepository _feedWith(OpenJob job) {
  return FakeOpenJobsRepository()
    ..pages = [ApiPage<OpenJob>(data: <OpenJob>[job])]
    ..job = job;
}

void main() {
  group('getting there', () {
    testWidgets('a provider taps a job in the feed and lands on it', (tester) async {
      final feed = _feedWith(anOpenJob(id: _sofa));

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      expect(find.byKey(const Key('open-job-detail')), findsOneWidget);
      // Re-read rather than handed across: the feed's copy may be minutes old, and a job that has
      // since been awarded is exactly the one nobody should be shown a bid form for.
      expect(feed.jobReads, <String>[_sofa]);
    });

    testWidgets('a deep link reaches the same screen with nothing else supplied', (tester) async {
      // Docs/07 §5 requires every notification to open the exact thing it concerns, and SHIP-145
      // delivers payloads straight to a route. The id is in the path rather than in a constructor
      // argument, which is the whole of what makes that work.
      final feed = _feedWith(anOpenJob(id: _sofa));

      await signInForBidding(tester, UserRole.provider, feed: feed);
      await deepLinkTo(tester, Routes.openJobDetailFor(_sofa));

      expect(find.byKey(const Key('open-job-detail')), findsOneWidget);
      expect(feed.jobReads, <String>[_sofa]);
    });

    testWidgets('the provider’s route is not the customer’s route', (tester) async {
      // Two endpoints, two response shapes, two screens. One screen branching on the role would be
      // the arrangement Docs/01 §4.3 is hardest to keep — the branch is one careless call site away
      // from being wrong and nothing would fail.
      expect(Routes.openJobDetailFor(_sofa), '/jobs/open/$_sofa');
      expect(Routes.jobDetailFor(_sofa), '/jobs/$_sofa');
    });
  });

  group('what a provider is shown', () {
    testWidgets('the two regions, and the note saying the street line is withheld', (tester) async {
      final feed = _feedWith(anOpenJob(id: _sofa));

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      expect(find.text('Newtown NSW 2042'), findsOneWidget);
      expect(find.text('Geelong VIC 3220'), findsOneWidget);
      // Said out loud, because a provider looking for a street number needs to know it is withheld
      // rather than missing. The coordinate is withheld for the same reason and by the same
      // decision: `jobs` geocodes the whole address, so a coordinate *is* the street line.
      expect(find.byKey(const Key('open-job-address-note')), findsOneWidget);
    });

    testWidgets('the goods, the size and what the customer says it needs', (tester) async {
      final feed = _feedWith(anOpenJob(id: _sofa));

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      expect(find.text('Three-seat sofa, wrapped'), findsOneWidget);
      expect(find.text('190 × 90 × 85 cm'), findsOneWidget);
      expect(find.text('65 kg'), findsOneWidget);
      // Free text and deliberately not matched against a compiled-in list: `vehicle_requirement`
      // stays free text until SHIP-79's capability vocabulary exists, and matching it here would be
      // the client inventing an eligibility rule the platform does not have.
      expect(find.text('Van or larger, two people to lift'), findsOneWidget);
    });

    testWidgets('a job with two regions and nothing else says so rather than showing gaps',
        (tester) async {
      // The ordinary case rather than a degenerate one: the wizard collects the addresses first, so
      // a job can be open to bids before its owner has described the goods.
      final feed = _feedWith(aBareOpenJob(id: _sofa));

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      expect(find.byKey(const Key('open-job-no-goods')), findsOneWidget);
      expect(find.byKey(const Key('open-job-no-window')), findsOneWidget);
    });

    testWidgets('a job already being negotiated says so, and does not discourage bidding',
        (tester) async {
      // Docs/02 §1 keeps a negotiating job open to eligible bids. This is information rather than a
      // warning — a provider pricing against company should know they are — and the form is still
      // there.
      final feed = _feedWith(anOpenJob(id: _sofa, status: JobStatus.negotiating));

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      expect(find.byKey(const Key('open-job-status-negotiating')), findsOneWidget);
      expect(find.byKey(const Key('bid-form')), findsOneWidget);
    });

    testWidgets('a window the customer named is shown as a window', (tester) async {
      // Windows here, instants in the bid. The customer says "any time Thursday"; the provider
      // answers "I will be there at nine".
      final feed = _feedWith(
        anOpenJob(
          id: _sofa,
          pickupWindow: const JobTimeWindow(
            start: '2026-08-20T23:00:00.000Z',
            end: '2026-08-22T23:00:00.000Z',
          ),
        ),
      );

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      // Composed through the same helper the screen uses, because the rendered day depends on the
      // device's zone: `2026-08-20T23:00Z` is the 21st in Sydney and the 20th on a UTC CI runner.
      final from = dayFirstDate('2026-08-20T23:00:00.000Z')!;
      final to = dayFirstDate('2026-08-22T23:00:00.000Z')!;

      expect(find.text('$from to $to'), findsOneWidget);
    });
  });

  group('a job the platform will not serve', () {
    testWidgets('is not reported as a fault, and offers no retry', (tester) async {
      // 404 covers a job outside this provider's eligibility, one that has been awarded or
      // withdrawn, one that never existed, and the owning customer at the wrong address. The
      // platform answers all of them identically on purpose, and a client that guessed which would
      // disclose exactly what the status code withholds.
      final feed = FakeOpenJobsRepository()
        ..pages = [ApiPage<OpenJob>(data: <OpenJob>[anOpenJob(id: _sofa)])]
        ..jobFailure = const ApiErrorResponse(
          statusCode: 404,
          code: 'not_found',
          message: 'No such job.',
        );

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      expect(find.byKey(const Key('open-job-not-available')), findsOneWidget);
      expect(find.byKey(const Key('open-job-retry')), findsNothing);
      // And no bid form: there is nothing to bid on.
      expect(find.byKey(const Key('bid-form')), findsNothing);
    });

    testWidgets('a job that could not be reached at all does offer a retry', (tester) async {
      // "There is no such job for you" and "we could not find out" are different things to be told,
      // and only one of them has a retry.
      final feed = FakeOpenJobsRepository()
        ..pages = [ApiPage<OpenJob>(data: <OpenJob>[anOpenJob(id: _sofa)])]
        ..jobFailure = const ApiUnreachable();

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      expect(find.byKey(const Key('open-job-failed')), findsOneWidget);
      expect(find.byKey(const Key('open-job-retry')), findsOneWidget);
    });

    testWidgets('the retry asks again', (tester) async {
      final feed = FakeOpenJobsRepository()
        ..pages = [ApiPage<OpenJob>(data: <OpenJob>[anOpenJob(id: _sofa)])]
        ..jobFailure = const ApiUnreachable();

      await openJobDetail(tester, feed: feed, jobId: _sofa);

      feed
        ..jobFailure = null
        ..job = anOpenJob(id: _sofa);

      await tester.tap(find.byKey(const Key('open-job-retry')));
      await tester.pumpAndSettle();

      expect(feed.jobReads, <String>[_sofa, _sofa]);
      expect(find.text('Three-seat sofa, wrapped'), findsOneWidget);
    });

    testWidgets('a spinner is what a first load shows, and it resolves', (tester) async {
      final gate = Completer<void>();
      final feed = FakeOpenJobsRepository()
        ..pages = [ApiPage<OpenJob>(data: <OpenJob>[anOpenJob(id: _sofa)])];

      await signInForBidding(tester, UserRole.provider, feed: feed);

      feed
        ..job = anOpenJob(id: _sofa)
        ..gate = gate;

      await tester.tap(find.byKey(const Key('open-job-$_sofa')));
      await tester.pump();
      await tester.pump();

      expect(find.byKey(const Key('open-job-loading')), findsOneWidget);

      gate.complete();
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('open-job-loading')), findsNothing);
      expect(find.text('Three-seat sofa, wrapped'), findsOneWidget);
    });
  });

  group('this is the provider’s half', () {
    testWidgets('a customer who follows the link is told so, and nothing is read', (tester) async {
      // The identifier-bearing route is the one a notification payload carries, so the surface has
      // to answer for itself when it is reached with no button involved.
      //
      // Unlike the fleet, the platform *would* refuse this caller — with a 404, byte-identically to
      // a job that does not exist. That is the correct answer on the wire and a poor thing to
      // render: "we could not find that job" is not what happened.
      final feed = _feedWith(anOpenJob(id: _sofa));
      final bidding = FakeBiddingRepository();

      await signInForBidding(tester, UserRole.customer, feed: feed, bidding: bidding);
      await deepLinkTo(tester, Routes.openJobDetailFor(_sofa));

      expect(find.byKey(const Key('provider-only')), findsOneWidget);
      expect(find.byKey(const Key('open-job-detail')), findsNothing);
      expect(find.byKey(const Key('bid-form')), findsNothing);
      // Nothing was asked of the platform on their behalf: neither controller is ever built,
      // because the widgets that watch them are never mounted.
      expect(feed.untouched, isTrue);
      expect(bidding.attempts, 0);
    });

    testWidgets('a customer is given a way back to their own half', (tester) async {
      final feed = _feedWith(anOpenJob(id: _sofa));

      await signInForBidding(tester, UserRole.customer, feed: feed);
      await deepLinkTo(tester, Routes.openJobDetailFor(_sofa));

      await tester.tap(find.byKey(const Key('provider-only-home')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
    });

    testWidgets('an account type this build does not recognise is told to update', (tester) async {
      final feed = _feedWith(anOpenJob(id: _sofa));

      await signInForBidding(tester, UserRole.unknown, feed: feed);
      await deepLinkTo(tester, Routes.openJobDetailFor(_sofa));

      expect(find.byKey(const Key('provider-only-role-unrecognised')), findsOneWidget);
      expect(feed.untouched, isTrue);
    });

    testWidgets('the router still does not decide who may be where', (tester) async {
      // The guard is deliberately blind to the role. A role-aware redirect would be an
      // authorisation control on the device, and — the bug it would actually have caused — the role
      // is null for the first round trip of a restored cold start, so it would bounce a provider off
      // their own screens every time they opened the app from a notification.
      final feed = _feedWith(anOpenJob(id: _sofa));

      await signInForBidding(tester, UserRole.customer, feed: feed);
      await deepLinkTo(tester, Routes.openJobDetailFor(_sofa));

      // Not redirected home — the screen's own app bar is what they are looking at.
      expect(find.text('Job'), findsOneWidget);
      expect(find.byKey(const Key('shell-customer')), findsNothing);
    });
  });

  group('the budget stays on the customer’s side', () {
    testWidgets('a payload carrying one under four names shows none of it', (tester) async {
      // Rendered through the real decoder, from the real wire shape. Docs/01 §4.3 forbids the
      // amount, a band, and a "budget supplied" flag alike, so this asserts that no number reaches
      // the screen *and* that no affordance implying a maximum was or was not set does either.
      final job = OpenJob.fromJson(<String, dynamic>{
        'id': _sofa,
        'status': 'open',
        'pickup': <String, dynamic>{'suburb': 'Newtown', 'state': 'NSW', 'postcode': '2042'},
        'goods_description': 'Three-seat sofa, wrapped',
        'budget_cents': 150000,
        'max_price': 150000,
        'budget': 1500,
        'customer_maximum_cents': 150000,
      });

      await openJobDetail(tester, feed: _feedWith(job), jobId: _sofa);

      for (final rendering in <String>['1500', '150000', r'$1,500.00', r'$1500', '1,500']) {
        expect(
          find.textContaining(rendering),
          findsNothing,
          reason: 'the customer’s maximum reached the provider’s screen as "$rendering"',
        );
      }

      // And no affordance implying one was set: no heading, no empty row, no "budget on request".
      // `Budget` with a capital is what a label would be; the lower-case word appears only inside
      // the constant note asserted below, which is the same sentence on every job.
      expect(find.textContaining('Budget'), findsNothing);
      expect(find.textContaining('on request'), findsNothing);
    });

    testWidgets('the note about it is a policy rather than a fact about this job', (tester) async {
      // A provider pricing a job will look for the customer's maximum, and saying there is none to
      // look for is better than leaving them to infer that this build simply does not show it.
      //
      // **A constant sentence is not a "budget supplied" flag.** Docs/01 §4.3 forbids a provider
      // learning *whether this job carries one*, and what makes this sentence safe is that it names
      // no number and no job — it is the same words on a payload with four budget-shaped keys in it
      // and on the one below with none.
      final job = OpenJob.fromJson(<String, dynamic>{
        'id': _sofa,
        'status': 'open',
        'budget_cents': 150000,
        'max_price': 150000,
      });

      await openJobDetail(tester, feed: _feedWith(job), jobId: _sofa);
      final note = tester.widget<Text>(find.byKey(const Key('bid-no-budget-note'))).data!;

      expect(note, isNot(contains('1500')));
      expect(note, isNot(contains('150000')));
      expect(note, contains('never shows you'));
    });

    testWidgets('and it is drawn on a job whose payload carried nothing of the sort',
        (tester) async {
      await openJobDetail(tester, feed: _feedWith(anOpenJob(id: _sofa)), jobId: _sofa);

      expect(find.byKey(const Key('bid-no-budget-note')), findsOneWidget);
    });
  });
}
