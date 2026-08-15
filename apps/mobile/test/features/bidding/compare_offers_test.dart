// SHIP-102 — "Customer compares price, timing, provider profile, and vehicle side by side".
//
// Driven through the real app: the session, the router, the guard, the shell, the customer's job
// list, the job detail, and the tap that opens the comparison. Only the token store and the two
// repositories are substituted, so a route missing from `_signedInLocations` — or a job screen that
// stopped offering the way in — fails here rather than only on a device. That is the failure
// SHIP-98 named: a screen reachable only through a button looks, from the outside, exactly like a
// button that does nothing.
//
// # Five things this file is careful about, each a way the screen could be quietly wrong
//
// **The four things being compared.** The *Done when* names price, timing, provider profile and
// vehicle, and each is asserted on the rendered card rather than on the model behind it.
//
// **Side by side.** A list would satisfy every field assertion above and would not be a comparison.
// The cards are checked to be laid out horizontally — different `dx`, equal `dy` — which is the only
// assertion in this file that would survive somebody replacing the row with a column.
//
// **The order.** Sorting is the client's, because the platform's cursor has to be over something
// stable and a price is not. So it is a real behaviour of this screen rather than a rendering of the
// platform's, and it is asserted as an order of *cards*, not of state.
//
// **The default is the platform's.** `GET /v1/jobs/{id}/bids/received` reads an absent `?status=` as
// `submitted`. A screen that sent `?status=submitted` explicitly would draw the right rows while
// shipping a copy of the platform's default in a build with no over-the-air path, so the **absence**
// of the parameter is asserted rather than the output.
//
// **The budget, and the form that survives everything else.** `Docs/01` §4.3 keeps the customer's
// maximum away from a provider — "not as an amount, not as a band, and not as a 'budget supplied'
// indicator" — and this screen is the mirror of the one wave 9 found the third form on. A closed key
// set cannot see a *sentence*, so the last group asserts on the **words rendered**: a screen saying
// "the customer has set a maximum" has no budget field and no budget value, and every guard that
// looks at fields or values passes it.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/received_offer.dart';

import 'fake_bidding_repository.dart';
import 'offers_app.dart';

/// The job every test here opens. See `offers_app.dart` for the walk that gets to it, which
/// SHIP-104's tests share.
const _job = offersJob;

ApiPage<ReceivedOffer> _page(
  List<ReceivedOffer> offers, {
  String? next,
  bool more = false,
}) =>
    offersPage(offers, next: next, more: more);

void main() {
  group('the customer compares price, timing, provider and vehicle', () {
    testWidgets('all four are on every card', (tester) async {
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[
            aReceivedOffer(id: 'o1', jobId: _job, amountCents: 45000, vehicle: aVehicle()),
            aReceivedOffer(
              id: 'o2',
              jobId: _job,
              amountCents: 39900,
              provider: const ProviderSummary(
                id: 'p2',
                verified: false,
                memberSince: '2026-07-01T00:00:00.000Z',
              ),
            ),
          ])
        ];

      await openOffers(tester, bidding);

      expect(find.byKey(const Key('compare-offers-list')), findsOneWidget);
      expect(find.byKey(const Key('compare-offers-count')), findsOneWidget);

      // 1. Price, in dollars at the point of display and nowhere earlier.
      expect(find.text(r'$450.00'), findsOneWidget);
      expect(find.text(r'$399.00'), findsOneWidget);

      for (final id in <String>['o1', 'o2']) {
        // 2. Timing — both commitments, on both cards.
        expect(find.byKey(Key('compare-offer-pickup-$id')), findsOneWidget, reason: id);
        expect(find.byKey(Key('compare-offer-deliver-$id')), findsOneWidget, reason: id);

        // 3. The provider, on every card rather than on some.
        expect(find.byKey(Key('compare-offer-provider-$id')), findsOneWidget, reason: id);
        expect(find.byKey(Key('compare-offer-verified-$id')), findsOneWidget, reason: id);

        // 4. The vehicle, present on both — as a description on one and as an honest "not stated"
        //    on the other, which is the pair that makes the absence meaningful.
        expect(find.byKey(Key('compare-offer-vehicle-$id')), findsOneWidget, reason: id);
      }

      // The verification state is drawn as what it is, in both directions — a screen that only ever
      // said "Verified provider" would pass a one-fixture test and be wrong about half the market.
      expect(find.text('Verified provider'), findsOneWidget);
      expect(find.text('Not yet verified'), findsOneWidget);

      // The vehicle and its declared capability, from `Docs/01` §4.3's own sentence.
      expect(find.text('Toyota HiAce (Van)'), findsOneWidget);
      expect(find.textContaining('1200 kg'), findsOneWidget);
      expect(find.textContaining('300 × 170 × 160 cm'), findsOneWidget);

      // And the offer that named none says so rather than drawing an empty frame.
      expect(find.text('No vehicle stated on this offer'), findsOneWidget);
    });

    testWidgets('the cards are side by side, not one above the other', (tester) async {
      // **The assertion that makes this a comparison rather than a list**, and the only one here
      // that would survive somebody replacing the row with a column while every field still
      // rendered. `Docs/01` §4.3 asks a customer to compare, and comparing is what a shared
      // horizontal baseline is for: the eye runs across one row of the cards at a time.
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[
            aReceivedOffer(id: 'o1', jobId: _job, amountCents: 45000),
            aReceivedOffer(id: 'o2', jobId: _job, amountCents: 39900),
          ])
        ];

      await openOffers(tester, bidding);

      final first = tester.getTopLeft(find.byKey(const Key('compare-offer-o2')));
      final second = tester.getTopLeft(find.byKey(const Key('compare-offer-o1')));

      expect(
        first.dy,
        second.dy,
        reason: 'the offer cards share a top edge, which is what makes their rows comparable',
      );
      expect(
        first.dx,
        lessThan(second.dx),
        reason: 'the offer cards are laid out across, not down — a column is a list, not a '
            'comparison',
      );
    });

    testWidgets('an offer with no price says so rather than showing nothing', (tester) async {
      // `Docs/07` §6: only what the app branches on is required, so a price that failed to arrive
      // is a decode that succeeds. A card that drew an empty space where the number goes would be
      // the one place on this screen a customer could misread a gap as a bargain.
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[aReceivedOffer(id: 'o1', jobId: _job, amountCents: null)])
        ];

      await openOffers(tester, bidding);

      expect(find.text('No price on this offer'), findsOneWidget);
    });

    testWidgets('the customer’s own counter is drawn as theirs', (tester) async {
      // The live offer in a negotiation can be the customer's own counter, and
      // `ck_bids_only_a_providers_offer_is_accepted` refuses awarding it. Saying so is better than
      // drawing it as though it were somebody's offer to accept.
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[
            aReceivedOffer(id: 'o1', jobId: _job, offeredBy: BidParty.customer),
          ])
        ];

      await openOffers(tester, bidding);

      expect(find.byKey(const Key('compare-offer-yours-o1')), findsOneWidget);
      expect(find.text('Your counter-offer'), findsOneWidget);
    });
  });

  group('the customer orders them', () {
    testWidgets('lowest price first is the default, and the cards are in that order',
        (tester) async {
      // Asserted as an order of **cards** rather than of state: a controller that sorted correctly
      // under a screen that drew `state.offers` would pass a state-level test and show the customer
      // the platform's order.
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[
            aReceivedOffer(id: 'dear', jobId: _job, amountCents: 91000),
            aReceivedOffer(id: 'cheap', jobId: _job, amountCents: 39900),
            aReceivedOffer(id: 'middling', jobId: _job, amountCents: 45000),
          ])
        ];

      await openOffers(tester, bidding);

      final across = <String, double>{
        for (final id in <String>['cheap', 'middling', 'dear'])
          id: tester.getTopLeft(find.byKey(Key('compare-offer-$id'))).dx,
      };

      expect(
        across.values.toList(),
        orderedEquals(<double>[...across.values]..sort()),
        reason: 'the cheapest offer is first, then the middling one, then the dearest',
      );
    });

    testWidgets('earliest collection reorders the cards without asking the platform again',
        (tester) async {
      // **No second read**, which is the difference from `MyBidsController.show`: `?status=` asks
      // the platform a different question and has to re-read from page one, while the order is a
      // property of the list this device is holding. Re-reading here would throw away pages the
      // customer had already asked for, to answer a question the endpoint does not accept.
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[
            aReceivedOffer(
              id: 'cheap-late',
              jobId: _job,
              amountCents: 39900,
              pickupAt: '2026-08-25T23:00:00.000Z',
            ),
            aReceivedOffer(
              id: 'dear-early',
              jobId: _job,
              amountCents: 91000,
              pickupAt: '2026-08-19T23:00:00.000Z',
            ),
          ])
        ];

      await openOffers(tester, bidding);
      expect(bidding.offerReads.length, 1);

      // Cheapest first to begin with.
      expect(
        tester.getTopLeft(find.byKey(const Key('compare-offer-cheap-late'))).dx,
        lessThan(tester.getTopLeft(find.byKey(const Key('compare-offer-dear-early'))).dx),
      );

      await tester.tap(find.byKey(const Key('compare-offers-order-soonest')));
      await tester.pumpAndSettle();

      expect(
        tester.getTopLeft(find.byKey(const Key('compare-offer-dear-early'))).dx,
        lessThan(tester.getTopLeft(find.byKey(const Key('compare-offer-cheap-late'))).dx),
        reason: 'ordering by collection time puts the earliest pickup first',
      );
      expect(
        bidding.offerReads.length,
        1,
        reason: 'reordering is a property of the list already held, not a new question for the '
            'platform',
      );
    });
  });

  group('what it asks the platform for', () {
    testWidgets('it names the job in the path and sends no status', (tester) async {
      // **The absence of `?status=` is the assertion.** The endpoint reads an absent status as
      // `submitted`, so a screen that sent it explicitly would draw identical pixels while carrying
      // a copy of the platform's default in a build that cannot be updated over the air — which is
      // exactly what `CLAUDE.md` keeps server-side.
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[aReceivedOffer(id: 'o1', jobId: _job)])
        ];

      await openOffers(tester, bidding);

      expect(bidding.offerReads.single.jobId, _job);
      expect(
        bidding.offerReads.single.status,
        isNull,
        reason: 'the live-offer default belongs to the platform, not to a build on a handset',
      );
      expect(bidding.offerReads.single.cursor, isNull);
    });

    testWidgets('a second page is asked for with the cursor, unchanged', (tester) async {
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(
            <ReceivedOffer>[aReceivedOffer(id: 'o1', jobId: _job, amountCents: 45000)],
            next: 'MR9yZWNvcmQ',
            more: true,
          ),
          _page(<ReceivedOffer>[aReceivedOffer(id: 'o2', jobId: _job, amountCents: 39900)]),
        ];

      await openOffers(tester, bidding);

      // **The honesty a partial sort owes.** Sorting what has been read is not sorting every offer
      // on the job, and a customer told "lowest price" over one page of three would reasonably
      // believe they had seen the cheapest.
      expect(find.byKey(const Key('compare-offers-partial')), findsOneWidget);

      // `ensureVisible` rather than `scrollUntilVisible`: this screen has **three** scrollables —
      // the page, the horizontal card row, and each card's own body — and `scrollUntilVisible`
      // fails with "Too many elements" when it cannot pick one. `ensureVisible` walks up from the
      // target's own context, which is the right scrollable by construction.
      await tester.ensureVisible(find.byKey(const Key('compare-offers-more')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('compare-offers-more')));
      await tester.pumpAndSettle();

      expect(bidding.offerReads.length, 2);
      expect(
        bidding.offerReads.last.cursor,
        'MR9yZWNvcmQ',
        reason: 'the cursor is opaque and is handed back exactly as it arrived',
      );

      // Both pages are on screen, and the sort now covers both.
      expect(find.byKey(const Key('compare-offer-o1')), findsOneWidget);
      expect(find.byKey(const Key('compare-offer-o2')), findsOneWidget);
      expect(find.byKey(const Key('compare-offers-partial')), findsNothing);
    });

    testWidgets('no offers yet is an ordinary state, not a failure', (tester) async {
      // The state a customer meets first: a job published a minute ago has no offers and nothing is
      // wrong. It is also the state that never occurs in development, because whoever is building
      // this has offers on their fixture.
      final bidding = FakeBiddingRepository()..offerPages = [_page(<ReceivedOffer>[])];

      await openOffers(tester, bidding);

      expect(find.byKey(const Key('compare-offers-empty')), findsOneWidget);
      expect(find.text('No offers yet'), findsOneWidget);
      expect(find.byKey(const Key('compare-offers-failed')), findsNothing);
    });

    testWidgets('a failure offers a retry rather than an empty screen', (tester) async {
      final bidding = FakeBiddingRepository()
        ..offersFailure = const ApiUnreachable()
        ..offerPages = [
          _page(<ReceivedOffer>[aReceivedOffer(id: 'o1', jobId: _job)])
        ];

      await openOffers(tester, bidding);

      expect(find.byKey(const Key('compare-offers-failed')), findsOneWidget);
      expect(find.byKey(const Key('compare-offers-empty')), findsNothing);

      bidding.offersFailure = null;
      await tester.tap(find.byKey(const Key('compare-offers-retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('compare-offer-o1')), findsOneWidget);
    });
  });

  group('nothing of the customer’s budget reaches this screen', () {
    // **The group wave 9's finding is about, and the only one that could catch its third form.**
    //
    // The platform's guard is a closed key set over the response, which cannot see a sentence; the
    // client's model guard in `budget_stays_on_the_customer_side_test.dart` is a closed key set over
    // the type, which cannot see one either. A screen reading "the customer has set a maximum" has
    // no budget field, no budget value, and passes both.
    //
    // So this reads **the words on the screen**.

    testWidgets('no rendered text names a budget, a maximum, or anything a customer set',
        (tester) async {
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[
            aReceivedOffer(id: 'o1', jobId: _job, amountCents: 45000, vehicle: aVehicle()),
            aReceivedOffer(id: 'o2', jobId: _job, amountCents: 39900),
          ])
        ];

      await openOffers(tester, bidding);

      final rendered = tester
          .widgetList<Text>(find.byType(Text))
          .map((text) => (text.data ?? '').toLowerCase())
          .join('\n');

      // Not vacuous: the screen has to be drawing offers before "it names no budget" means
      // anything at all.
      expect(rendered, contains(r'$450.00'.toLowerCase()));

      for (final phrase in <String>[
        'budget',
        'maximum',
        'max price',
        'ceiling',
        'price cap',
        'willing to pay',
        'within your',
        'over your',
        'under your',
        'you set',
        'you were prepared',
      ]) {
        expect(
          rendered,
          isNot(contains(phrase)),
          reason: '\n\nThe comparison screen renders the words “$phrase”.\n\n'
              'Docs/01 §4.3 forbids the customer’s maximum as an amount, as a band, **and as a\n'
              '“budget supplied” indicator** — and the third clause is the one that survives every\n'
              'other guard, because a sentence carries no field and no value. Wave 9 found exactly\n'
              'this form alive on the provider’s feed after a closed key set and a source scan had\n'
              'both passed it.\n\n'
              'If the screen genuinely needs to relate an offer to what the customer can spend,\n'
              'that is a product decision about Docs/01 §4.3 and belongs in the document before it\n'
              'belongs in a widget.\n',
        );
      }
    });

    testWidgets('no rendered text names the provider’s declared area or specialties',
        (tester) async {
      // The other half of SHIP-102a's *Done when*, which excludes both by name. The platform does
      // not send them, so this is a guard against a later ticket deciding to fetch them from
      // somewhere else and put them on the card.
      final bidding = FakeBiddingRepository()
        ..offerPages = [
          _page(<ReceivedOffer>[
            aReceivedOffer(id: 'o1', jobId: _job, vehicle: aVehicle()),
          ])
        ];

      await openOffers(tester, bidding);

      final rendered = tester
          .widgetList<Text>(find.byType(Text))
          .map((text) => (text.data ?? '').toLowerCase())
          .join('\n');

      expect(rendered, contains('verified provider'));
      for (final phrase in <String>[
        'service area',
        'specialis',
        'specialt',
        'refrigerated',
        'other jobs',
        'also bidding',
      ]) {
        expect(
          rendered,
          isNot(contains(phrase)),
          reason: 'the comparison screen renders “$phrase”, which SHIP-102a’s Done when excludes '
              'by name',
        );
      }
    });

    testWidgets('the customer-facing shapes carry exactly the keys the contract names',
        (tester) async {
      // A closed key set over the models this screen binds, kept **here** rather than in
      // `budget_stays_on_the_customer_side_test.dart`: that registry is explicitly the
      // *provider-facing* one, and registering a customer-facing shape in it would make its own
      // name a lie. The rule being held here is a different one — SHIP-102a's Done when — and it is
      // the fail-closed half that the word assertions above cannot be.
      final salt = <String, dynamic>{
        'budget_cents': 8675309,
        'max_price': 8675309,
        'service_area': <String>['VIC'],
        'specialties': <String>['refrigerated'],
        'registration': 'BID001',
        'other_jobs': 3,
      };

      final offer = ReceivedOffer.fromJson(<String, dynamic>{
        'id': 'a',
        'job_id': 'b',
        'status': 'submitted',
        ...salt,
      }).toJson();

      expect(
        offer.keys.toSet(),
        <String>{
          'id',
          'job_id',
          'status',
          'offered_by',
          'amount_cents',
          'pickup_at',
          'deliver_by',
          'message',
          'superseded_by',
          'provider',
          'vehicle',
          'created_at',
          'updated_at',
        },
        reason: 'ReceivedOffer gained or lost a key. A field added to a customer-facing model is a '
            'decision about Docs/01 §4.3 and SHIP-102a’s Done when, whatever it is called.',
      );

      expect(
        ProviderSummary.fromJson(<String, dynamic>{'id': 'p', ...salt}).toJson().keys.toSet(),
        <String>{'id', 'verified', 'member_since'},
        reason: 'the provider summary is closed: no service area, no specialties, no other jobs',
      );

      expect(
        VehicleSummary.fromJson(<String, dynamic>{'id': 'v', ...salt}).toJson().keys.toSet(),
        <String>{'id', 'type', 'make', 'model', 'capacity'},
        reason: 'the vehicle summary carries no registration — a losing bidder never published '
            'their plate to this customer',
      );
    });
  });
}
