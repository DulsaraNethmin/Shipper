// SHIP-101 — "Provider sees their own bids grouped by status".
//
// Driven through the real app: the session, the router, the guard, the shell and the half the role
// selects. Only the token store and the repositories are substituted, so a route missing from
// `_signedInLocations` or a shell that stopped offering the way in fails here rather than only on a
// device — the failure SHIP-98 named, where a screen reachable only through a button looks, from the
// outside, like a button that does nothing.
//
// # Four things this file is careful about, each of which is a way the screen could be quietly wrong
//
// **The grouping.** It is the whole of the *Done when*, and it has to survive a status the platform
// sends in an order the screen did not choose. The order is `Docs/02` §4's, taken from
// `BidStatus.values` rather than from the rows.
//
// **The second page.** Grouping over a paged list groups *what has been read*, which is the one
// thing a screen of this shape lies about by omission. Every paging test reads two pages.
//
// **The narrowing.** `?status=` runs in SQL on the platform, so picking a group is a **different
// request** rather than a predicate over rows already in hand. A screen that narrowed on the device
// would draw the same pixels and be asking the wrong question, so the parameter is asserted rather
// than the output.
//
// **The budget.** `Docs/01` §4.3 keeps the customer's maximum away from a provider in every form.
// The last group renders a page whose rows carry seven spellings of one and asserts that nothing of
// it reaches a pixel. That is the axis the closed key set in
// `budget_stays_on_the_customer_side_test.dart` cannot have: it holds the *model*, and this holds
// the *screen*, so a budget read from anywhere other than the model it binds fails here and only
// here.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';

import '../../core/auth/session_fixtures.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import '../jobs/fake_open_jobs_repository.dart';
import 'fake_bidding_repository.dart';

const _sofa = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';
const _pallet = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1';
const _drums = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e2';

ApiPage<Bid> _page(List<Bid> bids, {String? next, bool more = false}) =>
    ApiPage<Bid>(data: bids, nextCursor: next, hasMore: more);

/// Signs a provider in and walks them to their bids **the way a person gets there** — by tapping the
/// button on their own feed.
///
/// Through the button rather than by pumping the screen or following a link, because the route has
/// to be reachable: a location missing from `_signedInLocations` sends the app to the home shell,
/// which from the outside is a button that does nothing.
Future<void> openMyBids(
  WidgetTester tester,
  FakeBiddingRepository bidding, {
  UserRole role = UserRole.provider,
}) async {
  // A phone-shaped surface rather than the 800×600 default, and a tall one: eight groups over a
  // dozen offers should scroll rather than be reported as overflowing.
  tester.view.physicalSize = const Size(800, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: role);

  await tester.pumpWidget(
    signupApp(identity, bidding: bidding, openJobs: FakeOpenJobsRepository()),
  );
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();

  await tester.tap(find.byKey(const Key('your-bids')));
  await tester.pumpAndSettle();
}

void main() {
  group('a provider sees their own offers, grouped by status', () {
    testWidgets('every group the offers fall into is drawn, in Docs/02 §4’s order', (tester) async {
      // Deliberately shuffled relative to the enumeration: the platform answers newest first, which
      // is an order about *time*, and the grouping is an order about the document. A screen that
      // grouped in arrival order would pass a test whose fixture happened to arrive sorted.
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[
            aBid(id: 'b1', jobId: _sofa, status: BidStatus.rejected, amountCents: 44000),
            aBid(id: 'b2', jobId: _pallet, status: BidStatus.submitted, amountCents: 45000),
            aBid(id: 'b3', jobId: _drums, status: BidStatus.accepted, amountCents: 91000),
            aBid(id: 'b4', jobId: _sofa, status: BidStatus.submitted, amountCents: 46000),
          ])
        ];

      await openMyBids(tester, bidding);

      expect(find.byKey(const Key('my-bids')), findsOneWidget);

      // `Docs/02` §4's order: submitted, accepted, rejected — and `bid_status.gen.dart` is generated
      // from `contracts/statuses.yaml` in that order, which is why nothing here sorts anything.
      final headings = <String>['submitted', 'accepted', 'rejected'];
      for (final wire in headings) {
        expect(find.byKey(Key('my-bids-heading-$wire')), findsOneWidget, reason: wire);
      }

      final drawn = headings
          .map((wire) => tester.getTopLeft(find.byKey(Key('my-bids-heading-$wire'))).dy)
          .toList();
      expect(
        drawn,
        orderedEquals(<double>[...drawn]..sort()),
        reason: 'the groups are drawn in Docs/02 §4’s order, not in the order the rows arrived',
      );

      // Both of one provider's submitted offers are under the one heading.
      expect(find.byKey(const Key('my-bid-b2')), findsOneWidget);
      expect(find.byKey(const Key('my-bid-b4')), findsOneWidget);
    });

    testWidgets('a status nothing is in gets no heading', (tester) async {
      // Eight headings over one offer is a screen that reads as mostly empty, and an "Expired"
      // heading with nothing under it invites a provider to wonder what they have missed.
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[aBid(id: 'b1', status: BidStatus.submitted)])
        ];

      await openMyBids(tester, bidding);

      expect(find.byKey(const Key('my-bids-heading-submitted')), findsOneWidget);
      for (final wire in <String>['draft', 'expired', 'withdrawn', 'superseded', 'countered']) {
        expect(find.byKey(Key('my-bids-heading-$wire')), findsNothing, reason: wire);
      }
    });

    testWidgets('a customer’s counter-offer is on the list and is labelled as theirs',
        (tester) async {
      // `bids.provider_id` means the provider a negotiation is *with* rather than the author of any
      // one row, so a customer's counter carries it and is in this list. It is the row waiting for
      // an answer, and a screen that hid it — or drew it as the provider's own price — would be the
      // one row a provider most needs to see, missing or lying.
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[
            aBid(id: 'mine', status: BidStatus.superseded, offeredBy: BidParty.provider),
            aBid(id: 'theirs', status: BidStatus.submitted, offeredBy: BidParty.customer),
          ])
        ];

      await openMyBids(tester, bidding);

      expect(find.byKey(const Key('my-bid-theirs')), findsOneWidget);
      expect(find.byKey(const Key('my-bid-party-customer')), findsOneWidget);
      expect(find.byKey(const Key('my-bid-party-provider')), findsOneWidget);
      expect(find.text('From the customer'), findsOneWidget);
    });

    testWidgets('an offer a counter has displaced says so in words', (tester) async {
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[
            aBid(id: 'old', status: BidStatus.superseded, supersededBy: 'new'),
          ])
        ];

      await openMyBids(tester, bidding);

      // In a sentence rather than a colour: `Docs/02` §3.1's reasoning about pending state applies
      // to every state a person has to act on, and "an offer answered with another one" is not
      // something a tint communicates to a driver in sunlight.
      expect(find.byKey(const Key('my-bid-superseded-old')), findsOneWidget);
    });

    testWidgets('the price, the two instants and the date are the provider’s own, day-first',
        (tester) async {
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[
            aBid(
              id: 'b1',
              amountCents: 45050,
              // Chosen so day-first and month-first are different readings: 8 September, which a
              // month-first screen would render as August.
              pickupAt: '2026-09-08T23:00:00.000Z',
              deliverBy: '2026-09-09T07:00:00.000Z',
              createdAt: '2026-09-07T04:15:30.000Z',
            )
          ])
        ];

      await openMyBids(tester, bidding);

      expect(find.text('\$450.50'), findsOneWidget);

      // `CLAUDE.md` fixes day-first, and `dates.dart` spells the month so the convention cannot be
      // misread at all — `8 Sep 2026` rather than `08/09` or `09/08`.
      //
      // **The day number and the clock time are deliberately not asserted, and that is not
      // laziness.** `dayFirstDateTime` converts to the device's own zone, so `…T23:00:00Z` is the
      // 8th at 23:00 in UTC, the 9th at 09:00 in Sydney and the 8th at 16:00 in California. A test
      // naming one of those is a test that passes on the machine it was written on and fails on CI
      // — the timezone form of the fixture-with-two-clocks trap wave 8 recorded twice. What is
      // *this screen's* to get right is that it uses the spelled day-first form and carries a time
      // at all; `formatting_test.dart` holds the conversion itself, with the zone pinned.
      expect(find.textContaining('Sep 2026'), findsWidgets);
      expect(
        find.textContaining(RegExp(r'\d{1,2}:\d{2} (am|pm)')),
        findsWidgets,
        reason: 'a bid is two instants and not two windows — a provider says "I will be there at '
            'nine", and a screen showing only the day drops half of the offer',
      );
      expect(
        find.textContaining(RegExp(r'\d{2}/\d{2}')),
        findsNothing,
        reason: 'a numeric date is ambiguous to an Australian reader whichever way round it is',
      );
    });

    testWidgets('a provider with no offers sees an empty state and never a spinner', (tester) async {
      // `data` is `[]` rather than `null` precisely so this case is ordinary. It never occurs in
      // development, because whoever is building this has bid on something.
      await openMyBids(tester, FakeBiddingRepository());

      expect(find.byKey(const Key('my-bids-empty')), findsOneWidget);
      expect(find.byKey(const Key('my-bids-loading')), findsNothing);
      expect(find.byKey(const Key('my-bids-failed')), findsNothing);
    });
  });

  group('the group control asks the platform rather than narrowing what was read', () {
    testWidgets('picking a group sends ?status= and drops the previous question’s rows',
        (tester) async {
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[
            aBid(id: 'live', status: BidStatus.submitted),
            aBid(id: 'lost', status: BidStatus.rejected),
          ])
        ]
        ..groups = {
          BidStatus.rejected: [
            _page(<Bid>[aBid(id: 'lost', status: BidStatus.rejected)])
          ]
        };

      await openMyBids(tester, bidding);

      expect(bidding.reads.single.status, isNull, reason: 'the first read asks for every status');

      await tester.tap(find.byKey(const Key('my-bids-group-rejected')));
      await tester.pumpAndSettle();

      expect(
        bidding.reads.last.status,
        BidStatus.rejected,
        reason: 'the narrowing runs in SQL on the platform (SHIP-101a), not over the page',
      );
      expect(
        bidding.reads.last.cursor,
        isNull,
        reason: 'a cursor issued for one question does not answer another',
      );

      expect(find.byKey(const Key('my-bid-lost')), findsOneWidget);
      expect(find.byKey(const Key('my-bid-live')), findsNothing);
    });

    testWidgets('a group the platform answers empty is not the same as having never bid',
        (tester) async {
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[
            aBid(id: 'live', status: BidStatus.submitted),
            aBid(id: 'lost', status: BidStatus.rejected),
          ])
        ]
        ..groups = {
          BidStatus.rejected: [const ApiPage<Bid>(data: <Bid>[])]
        };

      await openMyBids(tester, bidding);
      await tester.tap(find.byKey(const Key('my-bids-group-rejected')));
      await tester.pumpAndSettle();

      // Two different things to be told, and only one of them has "show every status" as its answer.
      expect(find.byKey(const Key('my-bids-none-in-group')), findsOneWidget);
      expect(find.byKey(const Key('my-bids-empty')), findsNothing);

      await tester.tap(find.byKey(const Key('my-bids-show-all')));
      await tester.pumpAndSettle();

      expect(bidding.reads.last.status, isNull);
      expect(find.byKey(const Key('my-bid-live')), findsOneWidget);
    });

    testWidgets('one group is not worth a control', (tester) async {
      // A single chip beside "Every status" can only ever narrow to the whole list. Same reasoning
      // as `facetIsUseful` on the provider feed.
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[aBid(id: 'b1', status: BidStatus.submitted)])
        ];

      await openMyBids(tester, bidding);

      expect(find.byKey(const Key('my-bids-groups')), findsNothing);
    });
  });

  group('paging, and what the screen owes the provider about it', () {
    testWidgets('the second page is appended and regrouped, and the cursor goes back unchanged',
        (tester) async {
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(
            <Bid>[aBid(id: 'b1', status: BidStatus.submitted)],
            next: 'MR9yZWNvcmQ',
            more: true,
          ),
          _page(<Bid>[aBid(id: 'b2', status: BidStatus.expired)]),
        ];

      await openMyBids(tester, bidding);

      // The honest sentence: the headings above are built from what has been read, and there is
      // more. A screen showing one group when there are two has quietly claimed to be complete.
      expect(find.byKey(const Key('my-bids-partial')), findsOneWidget);
      expect(find.byKey(const Key('my-bids-heading-expired')), findsNothing);

      await tester.tap(find.byKey(const Key('my-bids-more')));
      await tester.pumpAndSettle();

      expect(
        bidding.reads.last.cursor,
        'MR9yZWNvcmQ',
        reason: 'opaque, and passed back exactly as it arrived',
      );

      // Appended, not replaced — and the new status has grown a heading of its own.
      expect(find.byKey(const Key('my-bid-b1')), findsOneWidget);
      expect(find.byKey(const Key('my-bid-b2')), findsOneWidget);
      expect(find.byKey(const Key('my-bids-heading-expired')), findsOneWidget);
      expect(find.byKey(const Key('my-bids-partial')), findsNothing);
    });
  });

  group('failure', () {
    testWidgets('a first read that fails offers a retry rather than an empty list', (tester) async {
      // "You have not bid on anything" and "we could not find out" are different things to be told,
      // and only one of them has a retry. Answering the second with the first is the failure that
      // makes a provider think their offers were lost.
      final bidding = FakeBiddingRepository()..readFailure = const ApiUnreachable();

      await openMyBids(tester, bidding);

      expect(find.byKey(const Key('my-bids-failed')), findsOneWidget);
      expect(find.byKey(const Key('my-bids-empty')), findsNothing);

      bidding
        ..readFailure = null
        ..pages = [
          _page(<Bid>[aBid(id: 'b1', status: BidStatus.submitted)])
        ];

      await tester.tap(find.byKey(const Key('my-bids-retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('my-bid-b1')), findsOneWidget);
    });

    testWidgets('a refresh that fails leaves the offers on screen', (tester) async {
      // Somebody who pulled to refresh at a loading dock should still be looking at their offers,
      // with a banner saying the reload did not work.
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[aBid(id: 'b1', status: BidStatus.submitted)])
        ];

      await openMyBids(tester, bidding);
      expect(find.byKey(const Key('my-bid-b1')), findsOneWidget);

      bidding.readFailure = const ApiUnreachable();
      await tester.fling(find.byKey(const Key('my-bids')), const Offset(0, 400), 1000);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('my-bid-b1')), findsOneWidget);
      expect(find.byKey(const Key('my-bids-failed')), findsNothing);
    });
  });

  group('whose surface this is', () {
    testWidgets('a customer who reaches the route is told, and no read is made for them',
        (tester) async {
      // `ProviderOnly`. Not an authorisation control — `GET /v1/fleet/bids` scopes to the caller's
      // own id in its `WHERE` clause, so a customer's answer is an empty page by construction. What
      // this buys is an honest sentence instead of an empty list, and one fewer request made on
      // behalf of an account with no business making it.
      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[aBid(id: 'b1', status: BidStatus.submitted)])
        ];

      final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.customer);

      tester.view.physicalSize = const Size(800, 2400);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      await tester.pumpWidget(signupApp(identity, bidding: bidding));
      await tester.pumpAndSettle();
      await signInThrough(tester);
      await tester.pumpAndSettle();

      // A customer is never offered the button — the shortcuts live inside the provider half.
      expect(find.byKey(const Key('your-bids')), findsNothing);

      await followMyBidsLink(tester);

      expect(find.byKey(const Key('provider-only')), findsOneWidget);
      expect(find.byKey(const Key('my-bid-b1')), findsNothing);
      expect(
        bidding.reads,
        isEmpty,
        reason: 'the controller is never constructed for an account that has no offers to read',
      );
    });
  });

  group('the customer’s budget reaches nothing on this screen', () {
    testWidgets('a page whose rows carry one renders none of it', (tester) async {
      // `Docs/01` §4.3: not as an amount, not as a band, and not as a "budget supplied" flag.
      //
      // **This is the third guard and the only one that can see a budget the model does not hold.**
      // The platform proves the field never leaves the service; the closed key set in
      // `budget_stays_on_the_customer_side_test.dart` proves `Bid` has nowhere to put one that
      // arrived anyway; and this proves the *screen* draws none of it. The payload is decoded
      // through the real `Bid.fromJson`, so what is exercised is the whole path from bytes to
      // pixels.
      //
      // Seven names and three values, matching the salt that file uses — because the finding it
      // records is that a search for "budget" cannot see `max_price`, and a value that appears
      // nowhere else is what stops the assertion passing by accident.
      const salt = <String, dynamic>{
        'budget_cents': 8675309,
        'max_price': 8675309,
        'budget': 86753,
        'customer_maximum_cents': 8675309,
        'ceiling_cents': 8675309,
        'willing_to_pay': 8675309,
        'reserve': 424242,
      };

      final bidding = FakeBiddingRepository()
        ..pages = [
          _page(<Bid>[
            Bid.fromJson(<String, dynamic>{
              'id': 'b1',
              'job_id': _sofa,
              'status': 'submitted',
              'offered_by': 'provider',
              'amount_cents': 45000,
              'pickup_at': '2026-08-19T23:00:00.000Z',
              'deliver_by': '2026-08-20T07:00:00.000Z',
              'created_at': '2026-08-13T04:15:30.000Z',
              ...salt,
            }),
          ])
        ];

      await openMyBids(tester, bidding);

      expect(find.byKey(const Key('my-bid-b1')), findsOneWidget);
      // The provider's own price is drawn, which is what makes the rest of this a real assertion
      // rather than a test of an empty screen.
      expect(find.text('\$450.00'), findsOneWidget);

      for (final forbidden in <String>[
        '8675309',
        '86753',
        '424242',
        '\$86,753.09',
        '\$867.53',
        '\$4,242.42',
        'budget',
        'Budget',
        'maximum',
      ]) {
        expect(
          find.textContaining(forbidden),
          findsNothing,
          reason: '\n\nThe customer’s budget is never exposed to a provider, in any form\n'
              '(Docs/01 §4.3, CLAUDE.md). `$forbidden` reached this screen.\n\n'
              'If a field was added to `Bid`, it does not belong on a provider-facing shape.\n'
              'If a *widget* was added that names or hints at one — a placeholder, a band, a\n'
              '"budget supplied" flag, or a sort by how close an offer is to one — it does not\n'
              'belong on a provider surface at all.\n',
        );
      }
    });
  });
}

/// Delivers `/bids` to the running application the way the platform delivers a deep link.
///
/// Through the app's own router, so `redirectFor` runs on it exactly as it does on a link opened
/// from a notification — which is the only way a customer could ever arrive here. Pushing the
/// screen directly would skip the guard, which is where a missing entry in `_signedInLocations`
/// shows up.
Future<void> followMyBidsLink(WidgetTester tester) async {
  final context = tester.element(find.byType(ShipperApp));
  ProviderScope.containerOf(context, listen: false).read(routerProvider).go(Routes.myBids);
  await tester.pumpAndSettle();
}
