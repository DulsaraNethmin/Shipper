// The two routes into one negotiation, as each party walks them (SHIP-103).
//
// **Two walks, because there are two people and the ticket is about both of them.** The customer
// arrives from the offers on their own delivery; the provider arrives from their own bids. They meet
// on one screen, and the whole of the *Done when* — "both parties exchange messages and
// counter-offers against a job" — is a claim about that meeting.
//
// Through the buttons rather than by pumping the screen or following a link, for the reason every
// walk in this suite is: a route missing from `_signedInLocations` sends the app to the home shell,
// which from the outside is indistinguishable from a button that does nothing. SHIP-102's lane
// caught exactly that omission on its first run.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/message.dart';
import 'package:shipper/features/bidding/received_offer.dart';
import 'package:shipper/features/jobs/job.dart';

import '../../core/auth/session_fixtures.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import '../jobs/fake_jobs_repository.dart';
import '../jobs/fake_open_jobs_repository.dart';
import 'fake_bidding_repository.dart';

/// The job both parties are negotiating over.
const negotiationJob = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

/// The offer the screen is addressed through.
///
/// **Deliberately the *first* offer in the chain rather than the live head**, in every walk here.
/// The platform resolves the whole negotiation from any offer in it, and a client that quietly
/// depended on holding the head would work until the first counter and then stop — so the walks make
/// the harder case the ordinary one.
const openingOffer = 'offer-1';

/// One page of a conversation.
ApiPage<Message> conversation(
  List<Message> messages, {
  String? next,
  bool more = false,
}) =>
    ApiPage<Message>(data: messages, nextCursor: next, hasMore: more);

/// Signs a **provider** in and walks them to the negotiation on [openingOffer] — their feed, their
/// bids, and the button on their own offer.
Future<void> openNegotiationAsProvider(
  WidgetTester tester,
  FakeBiddingRepository bidding, {
  String bidId = openingOffer,
}) async {
  _phone(tester);

  // The card the button lives on. Without a bid in the list there is nothing to tap, which is the
  // vacuous-fixture failure wave 11 recorded: thirteen Dart tests asserting over empty lists.
  if (bidding.pages.first.data.isEmpty) {
    bidding.pages = <ApiPage<Bid>>[
      ApiPage<Bid>(data: <Bid>[aBid(id: bidId, jobId: negotiationJob)]),
    ];
  }

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.provider);

  await tester.pumpWidget(
    signupApp(identity, bidding: bidding, openJobs: FakeOpenJobsRepository()),
  );
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('your-bids')));
  await tester.pumpAndSettle();

  await tester.ensureVisible(find.byKey(Key('negotiate-bid-$bidId')));
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(Key('negotiate-bid-$bidId')));
  await tester.pumpAndSettle();
}

/// Signs a **customer** in and walks them to the same negotiation — their deliveries, the delivery,
/// the offers on it, and the button on the card.
Future<void> openNegotiationAsCustomer(
  WidgetTester tester,
  FakeBiddingRepository bidding, {
  String offerId = openingOffer,
}) async {
  _phone(tester);

  if (bidding.offerPages.first.data.isEmpty) {
    bidding.offerPages = <ApiPage<ReceivedOffer>>[
      ApiPage<ReceivedOffer>(
        data: <ReceivedOffer>[aReceivedOffer(id: offerId, jobId: negotiationJob)],
      ),
    ];
  }

  final jobs = FakeJobsRepository()
    ..page = ApiPage<Job>(data: <Job>[FakeJobsRepository().detail(negotiationJob)]);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.customer);

  await tester.pumpWidget(signupApp(identity, jobs: jobs, bidding: bidding));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('job-$negotiationJob')));
  await tester.pumpAndSettle();

  await tester.scrollUntilVisible(find.byKey(const Key('compare-offers')), 200);
  await tester.tap(find.byKey(const Key('compare-offers')));
  await tester.pumpAndSettle();

  // `ensureVisible` rather than `scrollUntilVisible`: the comparison has three scrollables — the
  // page, the horizontal card row, and each card's own body — and `scrollUntilVisible` fails with
  // "Too many elements" when it cannot pick one. `ensureVisible` walks up from the target's own
  // context, which is the right scrollable by construction.
  await tester.ensureVisible(find.byKey(Key('negotiate-offer-$offerId')));
  await tester.pumpAndSettle();
  await tester.tap(find.byKey(Key('negotiate-offer-$offerId')));
  await tester.pumpAndSettle();
}

/// Every rendered word on screen, lower-cased and joined.
///
/// **The instrument the budget guard needs and no structural check can be.** A closed key set holds
/// a *model*; this holds the *pixels*. A screen composing "the customer has set a maximum" out of no
/// field and no value passes every guard that looks at fields or values, and fails only here.
///
/// This is the *open-world* half of that guard — a ban-list, fast to read and fast to fail. See
/// [renderedStrings] for the half that does not need to anticipate the wording.
String renderedText(WidgetTester tester) =>
    renderedStrings(tester).map((data) => data.toLowerCase()).join('\n');

/// Every string this screen actually puts in front of somebody, one entry per widget.
///
/// **Both kinds of rendered text, because a disclosure does not care which widget carries it.**
/// `Text` covers headings, labels, helper text, buttons and message bodies; `EditableText` covers
/// what a field is *seeded with*, which is rendered just as visibly and which no `Text` finder sees.
/// The counter form seeds the price and the conditions from the offer being answered, so leaving
/// `EditableText` out would leave a populated, provider-visible box outside the guard.
///
/// Empty strings are dropped: an empty composer is not a sentence.
List<String> renderedStrings(WidgetTester tester) {
  final texts = tester.widgetList<Text>(find.byType(Text)).map((text) => text.data ?? '');
  final editable =
      tester.widgetList<EditableText>(find.byType(EditableText)).map((f) => f.controller.text);

  return <String>[...texts, ...editable].where((data) => data.trim().isNotEmpty).toList();
}

/// A phone-shaped surface, and a tall one: a negotiation is a chain, a conversation and two forms.
void _phone(WidgetTester tester) {
  tester.view.physicalSize = const Size(800, 2600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}
