// The customer's route to the offers on their own job, as a person walks it.
//
// Extracted from `compare_offers_test.dart` when SHIP-104 added a second file that needs the same
// walk. `delivery_app.dart` is the same shape on the other side of the marketplace, and for the
// same reason: two test files reaching a screen two different ways is how one of them ends up
// exercising a route the app does not actually offer.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/bidding/received_offer.dart';
import 'package:shipper/features/jobs/job.dart';

import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import '../jobs/fake_jobs_repository.dart';
import 'fake_bidding_repository.dart';

/// The job every test in these two files opens.
const offersJob = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

/// One page of offers.
ApiPage<ReceivedOffer> offersPage(
  List<ReceivedOffer> offers, {
  String? next,
  bool more = false,
}) =>
    ApiPage<ReceivedOffer>(data: offers, nextCursor: next, hasMore: more);

/// Signs a customer in and walks them to the offers on [offersJob] **the way a person gets there** —
/// the job list, the job, and the button on it.
///
/// Through the buttons rather than by pumping the screen or following a link, because every step has
/// to be reachable. A route missing from the signed-in locations sends the app to the home shell,
/// which from the outside is indistinguishable from a button that does nothing.
Future<void> openOffers(
  WidgetTester tester,
  FakeBiddingRepository bidding, {
  FakeJobsRepository? repository,
}) async {
  // A phone-shaped surface rather than the 800×600 default, and a tall one: a card row 340 logical
  // pixels high under a job's worth of detail should scroll rather than be reported as overflowing.
  tester.view.physicalSize = const Size(800, 2000);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final jobs = repository ?? FakeJobsRepository();
  if (jobs.page.data.isEmpty) {
    jobs.page = ApiPage<Job>(data: <Job>[jobs.detail(offersJob)]);
  }

  await tester.pumpWidget(signupApp(FakeIdentityRepository(), jobs: jobs, bidding: bidding));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('job-$offersJob')));
  await tester.pumpAndSettle();

  await tester.scrollUntilVisible(find.byKey(const Key('compare-offers')), 200);
  await tester.tap(find.byKey(const Key('compare-offers')));
  await tester.pumpAndSettle();
}
