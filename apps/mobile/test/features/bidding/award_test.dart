// SHIP-104 — "Customer awards a bid with explicit confirmation and sees the result".
//
// Driven through the real app, like `compare_offers_test.dart` and by the same walk: sign in, open
// the job, open the offers, and tap. A screen reachable only by pumping it is indistinguishable
// from a button that does nothing.
//
// # The three clauses, and what would make each of them false while looking true
//
// **Awards a bid.** The request has to reach `POST /v1/jobs/{id}/award` with the offer's identifier
// and an idempotency key. Asserted on what the repository was asked, because a screen that changed
// colour and sent nothing looks identical.
//
// **With explicit confirmation.** Two halves, and the second is the one worth having: tapping the
// button must **not** award, and the dialog must be dismissable with nothing sent. A confirmation
// that is only tested in the accepting direction is a confirmation nobody has checked is a
// confirmation — it would pass with a dialog that awarded on open.
//
// **Sees the result.** Not a snackbar that has gone by the time anybody looks. After the award the
// screen re-reads the offers with `?status=accepted` — the platform's record — and says what was
// accepted. A screen that rewrote its own list would show the right thing and would be maintaining
// a second copy of a state machine `Docs/02` §2 puts on the platform, so the **parameter** is
// asserted rather than the rows.
//
// # And what must not be awardable
//
// The customer's own counter-offer, which `ck_bids_only_a_providers_offer_is_accepted` refuses, and
// an offer that has ended. Neither draws a button. That is presentation and not authorisation —
// `Docs/07` §3 — so the refusal path is exercised too: the platform refusing an award this screen
// did offer has to say something a customer can act on.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/received_offer.dart';

import 'fake_bidding_repository.dart';
import 'offers_app.dart';

/// A repository holding one awardable offer, and the accepted offer to answer the award with.
FakeBiddingRepository _oneOffer({
  String id = 'o1',
  int? amountCents = 45000,
  BidStatus status = BidStatus.submitted,
  BidParty? offeredBy = BidParty.provider,
  bool verified = true,
}) {
  return FakeBiddingRepository()
    ..offerPages = [
      offersPage(<ReceivedOffer>[
        aReceivedOffer(
          id: id,
          jobId: offersJob,
          amountCents: amountCents,
          status: status,
          offeredBy: offeredBy,
          provider: ProviderSummary(
            id: 'p1',
            verified: verified,
            memberSince: '2026-03-04T22:15:07.412Z',
          ),
        ),
      ]),
    ]
    // The read that follows the award. Scripted per status rather than as a second page of the
    // default read, because `?status=accepted` is a different request — see `offerGroups`.
    ..offerGroups = <BidStatus, List<dynamic>>{}.cast()
    ..awarded = aBid(id: id, jobId: offersJob, status: BidStatus.accepted, amountCents: amountCents);
}

void main() {
  group('the customer awards a bid', () {
    testWidgets('the offer carries a button and tapping it does not award', (tester) async {
      // **The half of "explicit confirmation" that is easy to leave untested.** A dialog that
      // awarded when it opened would pass every assertion about the accepting path.
      final bidding = _oneOffer();
      await openOffers(tester, bidding);

      expect(find.byKey(const Key('award-offer-o1')), findsOneWidget);

      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('award-confirm')), findsOneWidget);
      expect(bidding.awards, isEmpty, reason: 'opening the confirmation must not award anything');
    });

    testWidgets('the confirmation names the price and the provider, and what it costs', (
      tester,
    ) async {
      // A dialog reading "Are you sure?" confirms that the customer tapped something, not that they
      // tapped the right thing — and the row of cards it sits over is horizontally scrolling and
      // near-identical card to card.
      await openOffers(tester, _oneOffer(amountCents: 45000));

      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();

      expect(
        tester.widget<Text>(find.byKey(const Key('award-confirm-amount'))).data,
        contains(r'$450.00'),
      );
      expect(
        tester.widget<Text>(find.byKey(const Key('award-confirm-provider'))).data,
        contains('verification'),
      );
      // The consequence nobody would guess: the award closes every other offer in the same
      // transaction (Docs/02 §3, SHIP-93) and there is no un-award.
      final consequence =
          tester.widget<Text>(find.byKey(const Key('award-confirm-consequence'))).data!;
      expect(consequence, contains('closes every other offer'));
      expect(consequence, contains('cannot be undone'));
    });

    testWidgets('dismissing it sends nothing', (tester) async {
      final bidding = _oneOffer();
      await openOffers(tester, bidding);

      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('award-confirm-cancel')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('award-confirm')), findsNothing);
      expect(bidding.awards, isEmpty);
      // And the offer is still there to award, which is the point of cancelling.
      expect(find.byKey(const Key('award-offer-o1')), findsOneWidget);
    });

    testWidgets('confirming awards that offer, with an idempotency key', (tester) async {
      final bidding = _oneOffer();
      bidding.offerGroups[BidStatus.accepted] = [
        offersPage(<ReceivedOffer>[
          aReceivedOffer(id: 'o1', jobId: offersJob, status: BidStatus.accepted, amountCents: 45000),
        ]),
      ];

      await openOffers(tester, bidding);

      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('award-confirm-accept')));
      await tester.pumpAndSettle();

      expect(bidding.awards, hasLength(1));
      expect(bidding.awards.single.jobId, offersJob);
      expect(bidding.awards.single.bidId, 'o1');
      // Every state-changing request carries one and is refused without it (SHIP-15). A phone that
      // retries after a dropped connection must not award twice.
      expect(bidding.awards.single.idempotencyKey, isNotEmpty);
    });
  });

  group('and sees the result', () {
    testWidgets('the accepted offer is named, and the other offers are said to be closed', (
      tester,
    ) async {
      final bidding = _oneOffer(amountCents: 39900);
      bidding.offerGroups[BidStatus.accepted] = [
        offersPage(<ReceivedOffer>[
          aReceivedOffer(id: 'o1', jobId: offersJob, status: BidStatus.accepted, amountCents: 39900),
        ]),
      ];

      await openOffers(tester, bidding);
      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('award-confirm-accept')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('award-result')), findsOneWidget);
      final result = tester.widget<Text>(find.byKey(const Key('award-result-amount'))).data!;
      expect(result, contains(r'$399.00'));
      expect(result, contains('closed'));
    });

    testWidgets('the result is read back from the platform, not computed here', (tester) async {
      // The assertion that separates "asked what happened" from "decided what happened". A screen
      // that rewrote its own list would draw the same thing and would keep a second copy of a state
      // machine Docs/02 §2 puts on the platform.
      final bidding = _oneOffer();
      bidding.offerGroups[BidStatus.accepted] = [
        offersPage(<ReceivedOffer>[
          aReceivedOffer(id: 'o1', jobId: offersJob, status: BidStatus.accepted),
        ]),
      ];

      await openOffers(tester, bidding);
      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('award-confirm-accept')));
      await tester.pumpAndSettle();

      expect(
        bidding.offerReads.map((call) => call.status),
        contains(BidStatus.accepted),
        reason: 'the awarded offer is read back with ?status=accepted, which the contract names as '
            'how it is read back afterwards',
      );
      // The comparison controls go: ordering a list of one is a control with nothing to do.
      expect(find.byKey(const Key('compare-offers-order-cheapest')), findsNothing);
      expect(
        tester.widget<Text>(find.byKey(const Key('compare-offers-count'))).data,
        'The offer you accepted',
      );
    });

    testWidgets('and the button does not come back', (tester) async {
      // One job, one accepted bid. A second award would be refused `409 conflict`, and a button
      // that offered it would be the app promising something the platform will not do.
      final bidding = _oneOffer();
      bidding.offerGroups[BidStatus.accepted] = [
        offersPage(<ReceivedOffer>[
          aReceivedOffer(id: 'o1', jobId: offersJob, status: BidStatus.accepted),
        ]),
      ];

      await openOffers(tester, bidding);
      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('award-confirm-accept')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('award-offer-o1')), findsNothing);
    });
  });

  group('what is not awardable draws no button', () {
    testWidgets("the customer's own counter-offer", (tester) async {
      // `ck_bids_only_a_providers_offer_is_accepted` refuses it, and the card already says whose
      // offer it is. Drawing a button the platform would refuse is worse than drawing none.
      await openOffers(tester, _oneOffer(offeredBy: BidParty.customer));

      expect(find.byKey(const Key('compare-offer-yours-o1')), findsOneWidget);
      expect(find.byKey(const Key('award-offer-o1')), findsNothing);
    });

    testWidgets('an offer that has ended', (tester) async {
      await openOffers(tester, _oneOffer(status: BidStatus.withdrawn));

      expect(find.byKey(const Key('award-offer-o1')), findsNothing);
    });
  });

  group('when the platform refuses', () {
    /// Awards the one offer with [failure] waiting, and settles.
    Future<FakeBiddingRepository> refusedWith(WidgetTester tester, Object failure) async {
      final bidding = _oneOffer()..awardFailure = failure;
      await openOffers(tester, bidding);

      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('award-confirm-accept')));
      await tester.pumpAndSettle();

      return bidding;
    }

    testWidgets('a job that can no longer be awarded says so and sends them to the delivery', (
      tester,
    ) async {
      await refusedWith(
        tester,
        const ApiErrorResponse(statusCode: 409, code: 'conflict', message: 'ignored'),
      );

      expect(find.byKey(const Key('award-failed')), findsOneWidget);
      expect(
        tester.widget<Text>(find.byKey(const Key('award-failed-message'))).data,
        contains('can no longer be awarded'),
      );
      // Nothing was accepted, so the screen must not claim anything was.
      expect(find.byKey(const Key('award-result')), findsNothing);
    });

    testWidgets('an offer that ended between the draw and the tap says to choose another', (
      tester,
    ) async {
      await refusedWith(
        tester,
        const ApiErrorResponse(statusCode: 409, code: 'bidding_bid_closed', message: 'ignored'),
      );

      expect(
        tester.widget<Text>(find.byKey(const Key('award-failed-message'))).data,
        contains('no longer open'),
      );
    });

    testWidgets('a lost connection does not claim the award failed', (tester) async {
      // Nothing here establishes whether the platform acted, so the honest advice is to look. A
      // message saying it did not go through would be a guess, and the wrong one half the time.
      await refusedWith(tester, const ApiUnreachable());

      final message = tester.widget<Text>(find.byKey(const Key('award-failed-message'))).data!;
      expect(message, contains('try again'));
      expect(find.byKey(const Key('award-result')), findsNothing);
    });

    testWidgets('the offer can still be awarded after the message is closed', (tester) async {
      final bidding = await refusedWith(
        tester,
        const ApiErrorResponse(statusCode: 409, code: 'conflict', message: 'ignored'),
      );

      await tester.tap(find.byKey(const Key('award-failed-dismiss')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('award-failed')), findsNothing);
      expect(
        find.byKey(const Key('award-offer-o1')),
        findsOneWidget,
        reason: 'a refusal is not an award; the customer has to be able to try the same offer or '
            'choose another',
      );
      expect(bidding.awards, hasLength(1));
    });
  });

  group('the budget stays where it was', () {
    testWidgets('no word of a maximum appears on the confirmation or the result', (tester) async {
      // The form `Docs/01` §4.3 is hardest to keep, and the one wave 9 found: a *sentence* saying a
      // maximum exists carries no field and no value, so a closed key set cannot see it. Two new
      // surfaces arrived with this ticket and both are checked in words.
      final bidding = _oneOffer();
      bidding.offerGroups[BidStatus.accepted] = [
        offersPage(<ReceivedOffer>[
          aReceivedOffer(id: 'o1', jobId: offersJob, status: BidStatus.accepted),
        ]),
      ];

      await openOffers(tester, bidding);
      await tester.tap(find.byKey(const Key('award-offer-o1')));
      await tester.pumpAndSettle();

      void refuseBudgetWords() {
        for (final text in tester.widgetList<Text>(find.byType(Text))) {
          final words = (text.data ?? '').toLowerCase();
          for (final forbidden in <String>['budget', 'maximum', 'your limit', 'you set']) {
            expect(
              words.contains(forbidden),
              isFalse,
              reason: 'rendered text "${text.data}" names the customer\'s budget',
            );
          }
        }
      }

      refuseBudgetWords();

      await tester.tap(find.byKey(const Key('award-confirm-accept')));
      await tester.pumpAndSettle();

      refuseBudgetWords();
    });
  });
}
