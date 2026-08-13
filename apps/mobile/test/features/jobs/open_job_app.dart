import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/user_role.dart';

import '../../core/auth/session_fixtures.dart';
import '../bidding/fake_bidding_repository.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_open_jobs_repository.dart';

/// Signs [role] in and stops at the signed-in shell (SHIP-100).
///
/// The role travels the way it travels in the application: as a claim in the access token the
/// sign-in answered with (SHIP-50, SHIP-52). Nothing here sets a role directly, because nothing in
/// the app can — a test that assigned one would be testing a mechanism the product does not have.
Future<void> signInForBidding(
  WidgetTester tester,
  UserRole role, {
  required FakeOpenJobsRepository feed,
  FakeBiddingRepository? bidding,
}) async {
  // A phone-shaped surface rather than the 800×600 default, and a tall one: this screen carries a
  // job, a form and two picker dialogs, and a screen that scrolls should not be reported as
  // overflowing.
  tester.view.physicalSize = const Size(800, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: role);

  await tester.pumpWidget(signupApp(identity, openJobs: feed, bidding: bidding));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();
}

/// Signs a provider in and walks them to one job the way a person reaches it — by tapping its card
/// in the feed.
///
/// Through the card rather than by pumping the screen, because the route has to be reachable: a
/// guard that refused `/jobs/open/{id}` would show up here rather than only on a device, and a route
/// reachable only through an identifier looks, from the outside, like a card that does nothing when
/// it is tapped.
Future<void> openJobDetail(
  WidgetTester tester, {
  required FakeOpenJobsRepository feed,
  FakeBiddingRepository? bidding,
  required String jobId,
}) async {
  await signInForBidding(tester, UserRole.provider, feed: feed, bidding: bidding);

  await tester.tap(find.byKey(Key('open-job-$jobId')));
  await tester.pumpAndSettle();
}

/// Fills the offer form and sends it.
///
/// [amount] is typed the way a provider types it — dollars, with or without cents. The two instants
/// are chosen from the pickers the screen actually uses, so a change to either affordance fails here
/// rather than on a device.
Future<void> placeBidThrough(WidgetTester tester, {String amount = '450', String? message}) async {
  await tester.enterText(find.byKey(const Key('bid-amount')), amount);
  await tester.pump();

  await chooseInstant(tester, 'bid-pickup');
  await chooseInstant(tester, 'bid-deliver-by');

  if (message != null) {
    await tester.enterText(find.byKey(const Key('bid-message')), message);
    await tester.pump();
  }

  await tester.tap(find.byKey(const Key('bid-submit')));
  await tester.pumpAndSettle();
}

/// Chooses a date and a time through the two dialogs the field opens.
///
/// Both are accepted as they arrive. **The value is deliberately not steered**: what this ticket has
/// to demonstrate is that an instant is chosen and sent in a form the platform parses, and whether
/// *that* instant is acceptable — in the future, and after the collection — is `internal/bidding`'s
/// answer rather than this device's.
Future<void> chooseInstant(WidgetTester tester, String fieldKey) async {
  await tester.tap(find.byKey(Key(fieldKey)));
  await tester.pumpAndSettle();

  // The calendar.
  await tester.tap(find.text('OK'));
  await tester.pumpAndSettle();

  // The clock, opened in input mode — a provider committing to a time types it.
  await tester.tap(find.text('OK'));
  await tester.pumpAndSettle();
}
