// SHIP-103 — "Both parties exchange messages and counter-offers against a job".
//
// Driven through the real app on **both** sides: the session, the router, the guard, the shell, and
// the button each party actually taps. Only the token store and the repositories are substituted, so
// a route missing from `_signedInPatterns` — or a card that stopped offering the way in — fails here
// rather than only on a device.
//
// # The *Done when* is a claim about two people, so it is tested as one
//
// "Both parties" is not decoration. The four endpoints behind this screen each serve customer and
// provider alike and work out which from the credential, so the failure worth catching is a screen
// that is right for whoever wrote it and wrong for the other one. Every exchange group below runs
// the same behaviour from both walks.
//
// # Six things this file is careful about, each a way the screen could be quietly wrong
//
// **The thread is the platform's rows, not what was typed.** The fake echoes by default, which is
// what the platform does — and an echoing fake cannot tell a screen that appends the response from
// one that appends the composer's text. One test answers with a body that *differs*, which is the
// only shape that separates them, and it is the same property that makes a retry answered `200`
// with an earlier attempt's message reconcile correctly.
//
// **A counter is a difference, not an offer.** Anything omitted is inherited by the platform, so a
// form that sent back every field it was showing would look identical on screen and re-assert timing
// the other party had already agreed to. The **body** is asserted, not the outcome.
//
// **A counter re-reads the chain.** The response is the new head alone; the platform also superseded
// the offer it answered. A controller that wrote both facts locally renders identically and keeps a
// second copy of `Docs/02` §2's state machine. Only the extra read tells them apart, so the read
// count is the assertion.
//
// **The counter is addressed to the live head, not to the route.** A negotiation that has run on has
// a different head, and countering the row in the URL would answer an offer superseded three rounds
// ago — which the platform refuses, so this is the difference between a working screen and a
// `bidding_bid_closed` nobody could explain.
//
// **The fixtures reach the subject.** Wave 11 recorded thirteen Dart tests that asserted over empty
// lists because `Platform.isIOS` is false on a macOS host. Every group here asserts something is on
// screen before asserting what is not.
//
// **The budget, in the form that survives everything else.** `Docs/01` §4.3 keeps the customer's
// maximum away from a provider — "not as an amount, not as a band, and not as a 'budget supplied'
// indicator" — and this screen renders both parties' words beside a form for proposing a number. A
// closed key set cannot see a *sentence*, so the last group reads the **words rendered in the
// provider's view**.

import 'dart:async';
import 'dart:io';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/message.dart';

import 'fake_bidding_repository.dart';
import 'negotiation_app.dart';

const _job = negotiationJob;

/// A negotiation with one round in it: the provider's opening offer, still on the table.
List<Bid> _opening() => <Bid>[
      aBid(id: openingOffer, jobId: _job, amountCents: 52000, offeredBy: BidParty.provider),
    ];

/// The same negotiation after the customer has answered it.
///
/// Two rows, oldest first, and the first one **superseded and pointing at the second** — which is
/// exactly what the platform does rather than deleting or overwriting anything.
List<Bid> _countered() => <Bid>[
      aBid(
        id: openingOffer,
        jobId: _job,
        amountCents: 52000,
        offeredBy: BidParty.provider,
        status: BidStatus.superseded,
        supersededBy: 'offer-2',
      ),
      aBid(
        id: 'offer-2',
        jobId: _job,
        amountCents: 40000,
        offeredBy: BidParty.customer,
      ),
    ];

void main() {
  group('both parties exchange messages', () {
    testWidgets('a provider writes one, and the platform’s row joins the thread', (tester) async {
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsProvider(tester, bidding);

      // Reaches the subject: the screen is drawn and the conversation is genuinely empty rather
      // than never asked for.
      expect(find.byKey(const Key('negotiation-body')), findsOneWidget);
      expect(find.byKey(const Key('negotiation-no-messages')), findsOneWidget);

      await tester.enterText(
        find.byKey(const Key('negotiation-composer')),
        '  Is there a lift?  ',
      );
      await tester.pump();
      await tester.tap(find.byKey(const Key('negotiation-send')));
      await tester.pumpAndSettle();

      expect(bidding.sends.single.jobId, _job);
      expect(
        bidding.sends.single.bidId,
        openingOffer,
        reason: 'a conversation is addressed through the offer the screen was opened from',
      );
      expect(
        bidding.sends.single.body,
        'Is there a lift?',
        reason: 'the composer trims its own surroundings; the platform stores the rest as written',
      );

      expect(find.text('Is there a lift?'), findsOneWidget);
      expect(find.byKey(const Key('negotiation-no-messages')), findsNothing);
    });

    testWidgets('the customer sees the same conversation from the other side', (tester) async {
      // **The half that makes "both parties" mean anything.** The same two messages, read by the
      // other party, have to change sides — and `sent_by` is what says which, never the position in
      // the list, because a conversation does not alternate.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[
          conversation(<Message>[
            aMessage(id: 'm1', sentBy: BidParty.provider, body: 'Is there a lift?'),
            aMessage(id: 'm2', sentBy: BidParty.customer, body: 'There is, but it is out.'),
          ]),
        ];

      await openNegotiationAsCustomer(tester, bidding);

      expect(find.text('Is there a lift?'), findsOneWidget);
      expect(find.text('There is, but it is out.'), findsOneWidget);

      final theirs = tester.getTopLeft(find.byKey(const Key('negotiation-message-m1'))).dx;
      final mine = tester.getTopLeft(find.byKey(const Key('negotiation-message-m2'))).dx;

      expect(
        mine,
        greaterThan(theirs),
        reason: 'to a customer, the customer’s own message is the one on the right — the provider '
            'reads the same two the other way round',
      );
      expect(find.byKey(const Key('negotiation-message-at-m2')), findsOneWidget);
      expect(find.textContaining('You · '), findsOneWidget);
      expect(find.textContaining('Them · '), findsOneWidget);
    });

    testWidgets('the thread shows the platform’s row rather than what was typed', (tester) async {
      // **The assertion an echoing fake cannot make.** A screen that appended the composer's text
      // would be indistinguishable from one that appended the response for as long as the two are
      // equal, and they stop being equal on the case that actually happens: a retry answered `200`
      // with the message an earlier attempt sent.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])]
        ..messageReply = aMessage(
          id: 'm-recorded',
          sentBy: BidParty.customer,
          body: 'the message the platform actually holds',
        );

      await openNegotiationAsCustomer(tester, bidding);

      await tester.enterText(
        find.byKey(const Key('negotiation-composer')),
        'what this device typed',
      );
      await tester.pump();
      await tester.tap(find.byKey(const Key('negotiation-send')));
      await tester.pumpAndSettle();

      expect(find.text('the message the platform actually holds'), findsOneWidget);
      expect(
        find.text('what this device typed'),
        findsNothing,
        reason: 'the thread is assigned from the response and from nothing else',
      );
    });

    testWidgets('a send that fails keeps what was written, and a retry reuses the key',
        (tester) async {
      // `Docs/07` §4: one key per action, held across a retry of that same action. A dropped
      // connection leaves it genuinely unknown whether the platform recorded the message — and the
      // contract calls a duplicated message worse than most duplicated writes, because the other
      // party has already read it and cannot tell which sending was the mistake.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])]
        ..sendFailure = const ApiUnreachable();

      await openNegotiationAsProvider(tester, bidding);

      await tester.enterText(find.byKey(const Key('negotiation-composer')), 'Nine works for me.');
      await tester.pump();
      await tester.tap(find.byKey(const Key('negotiation-send')));
      await tester.pumpAndSettle();

      expect(
        find.text('Nine works for me.'),
        findsOneWidget,
        reason: 'the composer keeps what was written when the send failed — the failure most likely '
            'here is no signal, and emptying the box would throw the words away with it',
      );

      bidding.sendFailure = null;
      await tester.tap(find.byKey(const Key('negotiation-send')));
      await tester.pumpAndSettle();

      expect(bidding.sends.length, 2);
      expect(
        bidding.sendKeys.first,
        bidding.sendKeys.last,
        reason: 'a retry of the same message carries the key the first attempt carried',
      );
    });

    testWidgets('a second, different message carries a new key', (tester) async {
      // The other direction, and the one a client gets wrong by holding a key too eagerly: two
      // messages are two actions, and reusing the key would replay the first — a send button that
      // silently does nothing.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsProvider(tester, bidding);

      for (final body in <String>['First question.', 'Second question.']) {
        await tester.enterText(find.byKey(const Key('negotiation-composer')), body);
        await tester.pump();
        await tester.tap(find.byKey(const Key('negotiation-send')));
        await tester.pumpAndSettle();
      }

      expect(bidding.sends.map((c) => c.body), <String>['First question.', 'Second question.']);
      expect(
        bidding.sendKeys.first,
        isNot(bidding.sendKeys.last),
        reason: 'two messages are two actions, and an action gets its own key',
      );
    });

    testWidgets('the conversation keeps working when there is no offer on the table',
        (tester) async {
      // The endpoint has **no status rule at all** — "the moment two parties most need to arrange
      // something is after the award". A screen that hid the composer behind a live offer would be
      // inventing a rule the platform deliberately did not make.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[
          <Bid>[
            aBid(
              id: openingOffer,
              jobId: _job,
              amountCents: 52000,
              status: BidStatus.accepted,
            ),
          ],
        ]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsProvider(tester, bidding);

      expect(find.byKey(const Key('negotiation-closed')), findsOneWidget);
      expect(
        find.byKey(const Key('negotiation-counter-open')),
        findsNothing,
        reason: 'there is nothing left to counter',
      );
      expect(
        find.byKey(const Key('negotiation-composer')),
        findsOneWidget,
        reason: 'and there is still everything left to arrange',
      );
    });
  });

  group('both parties exchange counter-offers', () {
    testWidgets('the customer answers the provider’s price, and only the price is sent',
        (tester) async {
      final bidding = FakeBiddingRepository()
        // Two chains: what the negotiation looked like before the counter, and after it. The second
        // is only reached if the screen re-reads, which is the point.
        ..chains = <List<Bid>>[_opening(), _countered()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])]
        ..countered = aBid(id: 'offer-2', jobId: _job, amountCents: 40000, offeredBy: BidParty.customer);

      await openNegotiationAsCustomer(tester, bidding);

      // Reaches the subject: the opening offer is on screen and is the head.
      expect(find.byKey(Key('negotiation-offer-$openingOffer')), findsOneWidget);
      expect(find.text(r'$520.00'), findsOneWidget);
      expect(bidding.chainReads.length, 1);

      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();

      // Seeded from the offer being answered, which is what makes "send only what changes" usable.
      expect(
        tester.widget<TextFormField>(find.byKey(const Key('negotiation-counter-amount'))).controller?.text,
        '520.00',
      );

      await tester.enterText(find.byKey(const Key('negotiation-counter-amount')), '400');
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('negotiation-counter-submit')));
      await tester.tap(find.byKey(const Key('negotiation-counter-submit')));
      await tester.pumpAndSettle();

      expect(
        bidding.counterBodies.single,
        <String, dynamic>{'amount_cents': 40000},
        reason: 'a counter carries a difference: the timing and conditions on the table stay on the '
            'table, and re-sending them would re-assert terms the other party had agreed to',
      );
    });

    testWidgets('the chain is re-read rather than patched, and the new head is drawn',
        (tester) async {
      // `Docs/02` §2 makes status the platform's. The response is the counter alone; the platform
      // also moved the offer it answered to `superseded` and pointed it at the new row. A client
      // that wrote both facts itself would render identically here and would be keeping a second
      // copy of the state machine — right until the day it is not.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening(), _countered()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsCustomer(tester, bidding);
      expect(bidding.chainReads.length, 1);

      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const Key('negotiation-counter-amount')), '400');
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('negotiation-counter-submit')));
      await tester.tap(find.byKey(const Key('negotiation-counter-submit')));
      await tester.pumpAndSettle();

      expect(
        bidding.chainReads.length,
        2,
        reason: 'the counter re-reads the negotiation instead of deciding what the platform did',
      );

      // Both rounds are on screen, oldest first, and the head has moved.
      expect(find.byKey(Key('negotiation-offer-$openingOffer')), findsOneWidget);
      expect(find.byKey(const Key('negotiation-offer-offer-2')), findsOneWidget);
      expect(find.byKey(const Key('negotiation-offer-head-offer-2')), findsOneWidget);
      expect(
        find.byKey(Key('negotiation-offer-head-$openingOffer')),
        findsNothing,
        reason: 'the offer that was answered is no longer the one anything can be done to',
      );
      expect(
        find.byKey(const Key('negotiation-counter-form')),
        findsNothing,
        reason: 'the form closes over a head that has moved, rather than inviting a counter to an '
            'offer that no longer exists',
      );
    });

    testWidgets('the provider answers the customer’s counter, from the same screen', (tester) async {
      // **The second direction, which is what "both parties" costs.** The provider opens the
      // negotiation on their own original offer — now superseded — and counters the customer's
      // answer to it. One screen, one endpoint, and the side is the credential's.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_countered()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsProvider(tester, bidding);

      // The customer's counter is on the table and it is theirs, not this provider's.
      expect(find.byKey(const Key('negotiation-offer-head-offer-2')), findsOneWidget);
      expect(
        tester.widget<Text>(find.byKey(const Key('negotiation-offer-party-offer-2'))).data,
        'Theirs',
      );
      expect(
        tester.widget<Text>(find.byKey(Key('negotiation-offer-party-$openingOffer'))).data,
        'Yours',
      );

      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const Key('negotiation-counter-amount')), '460');
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('negotiation-counter-submit')));
      await tester.tap(find.byKey(const Key('negotiation-counter-submit')));
      await tester.pumpAndSettle();

      expect(
        bidding.counters.single.bidId,
        'offer-2',
        reason: 'a counter answers the **live head**, not the offer the screen was opened from — '
            'the platform refuses anything else with bidding_bid_closed',
      );
      expect(bidding.counterBodies.single, <String, dynamic>{'amount_cents': 46000});
    });

    testWidgets('clearing the conditions sends an empty string, which is how they are removed',
        (tester) async {
      // The one tri-state in this client: `null` leaves the conditions alone and `''` clears them.
      // A `toJson` copied from `BidPlacement`'s — which omits an empty message — would silently
      // drop the only way to remove them, and nothing on screen would say so.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[
          <Bid>[
            aBid(
              id: openingOffer,
              jobId: _job,
              amountCents: 52000,
              message: 'Tail lift required.',
            ),
          ],
        ]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsCustomer(tester, bidding);

      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();

      expect(
        tester
            .widget<TextFormField>(find.byKey(const Key('negotiation-counter-message')))
            .controller
            ?.text,
        'Tail lift required.',
      );

      await tester.enterText(find.byKey(const Key('negotiation-counter-message')), '');
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('negotiation-counter-submit')));
      await tester.tap(find.byKey(const Key('negotiation-counter-submit')));
      await tester.pumpAndSettle();

      expect(bidding.counterBodies.single, <String, dynamic>{'message': ''});
    });

    testWidgets('a counter that changes nothing is refused here, without a request', (tester) async {
      // "A counter-offer that changes nothing is agreement, and the way to agree is to award the
      // job." The platform refuses an empty body with `400`, and spending a round trip to be told
      // what this device already knows is the one case worth answering locally.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsCustomer(tester, bidding);

      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();
      await tester.ensureVisible(find.byKey(const Key('negotiation-counter-submit')));
      await tester.tap(find.byKey(const Key('negotiation-counter-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('negotiation-counter-unchanged')), findsOneWidget);
      expect(bidding.counters, isEmpty);
    });

    testWidgets('a 422 naming a field this device did not send is shown on that field',
        (tester) async {
      // **The refusal shape unique to this endpoint.** The platform validates the *merged* offer, so
      // countering on price alone against an offer whose collection time has since passed is
      // rejected naming `pickup_at` — a field the form never touched. A form that only rendered
      // messages for fields somebody had typed into would drop the explanation entirely.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])]
        ..counterFailure = const ApiErrorResponse(
          statusCode: 422,
          code: 'validation_failed',
          message: 'one or more fields were rejected',
          details: <ApiFieldError>[
            ApiFieldError(
              field: 'pickup_at',
              code: 'in_the_past',
              message: 'Collection is already in the past. Propose a new time.',
            ),
          ],
        );

      await openNegotiationAsCustomer(tester, bidding);

      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const Key('negotiation-counter-amount')), '400');
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('negotiation-counter-submit')));
      await tester.tap(find.byKey(const Key('negotiation-counter-submit')));
      await tester.pumpAndSettle();

      expect(bidding.counters, hasLength(1));
      expect(
        find.text('Collection is already in the past. Propose a new time.'),
        findsOneWidget,
        reason: 'the platform names the field, and the form renders it there — even though nobody '
            'touched it',
      );
      expect(
        find.byKey(const Key('negotiation-counter-form')),
        findsOneWidget,
        reason: 'a refusal leaves the form open with what was typed in it',
      );
    });

    testWidgets('a refusal by code is rendered as its code, never as the platform’s message',
        (tester) async {
      // `Docs/07` §6 and `CLAUDE.md`: clients branch on `code`, never on `message`. The message is
      // copy and gets reworded; the code is the contract.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])]
        ..counterFailure = const ApiErrorResponse(
          statusCode: 409,
          code: 'bidding_bid_closed',
          message: 'this wording is not what anything branches on',
        );

      await openNegotiationAsCustomer(tester, bidding);

      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();
      await tester.enterText(find.byKey(const Key('negotiation-counter-amount')), '400');
      await tester.pump();
      await tester.ensureVisible(find.byKey(const Key('negotiation-counter-submit')));
      await tester.tap(find.byKey(const Key('negotiation-counter-submit')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('negotiation-counter-refused')), findsOneWidget);
      expect(find.textContaining('This negotiation is closed'), findsOneWidget);
      expect(find.text('this wording is not what anything branches on'), findsNothing);
    });
  });

  group('the record of the negotiation', () {
    testWidgets('every round is drawn, oldest first, including the superseded ones', (tester) async {
      // `Docs/01` §4.3 requires the platform to record all offers, counter-offers and withdrawals,
      // and `Docs/02` §4 keeps that record visible to both parties. Somebody deciding whether to
      // accept four hundred needs to see that it started at five hundred and twenty.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_countered()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsCustomer(tester, bidding);

      final first = tester.getTopLeft(find.byKey(Key('negotiation-offer-$openingOffer'))).dy;
      final second = tester.getTopLeft(find.byKey(const Key('negotiation-offer-offer-2'))).dy;

      expect(first, lessThan(second), reason: 'oldest first — a negotiation is read forward');
      expect(find.text(r'$520.00'), findsOneWidget);
      expect(find.text(r'$400.00'), findsOneWidget);
      expect(
        bidding.chainReads.single.bidId,
        openingOffer,
        reason: 'any offer in the chain addresses the whole of it, so the one the screen was opened '
            'from is the one it asks with',
      );
    });

    testWidgets('a further page of the conversation is asked for with the cursor, unchanged',
        (tester) async {
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[
          conversation(
            <Message>[aMessage(id: 'm1', body: 'Earlier.')],
            next: 'MR9yZWNvcmQ',
            more: true,
          ),
          conversation(<Message>[aMessage(id: 'm2', body: 'Later.')]),
        ];

      await openNegotiationAsProvider(tester, bidding);

      expect(find.text('Earlier.'), findsOneWidget);
      await tester.ensureVisible(find.byKey(const Key('negotiation-more')));
      await tester.tap(find.byKey(const Key('negotiation-more')));
      await tester.pumpAndSettle();

      expect(bidding.messageReads.length, 2);
      expect(
        bidding.messageReads.last.cursor,
        'MR9yZWNvcmQ',
        reason: 'the cursor is opaque and is handed back exactly as it arrived',
      );
      expect(find.text('Earlier.'), findsOneWidget);
      expect(
        find.text('Later.'),
        findsOneWidget,
        reason: 'the conversation arrives oldest first, so a further page is appended rather than '
            'prepended — it is what was said after this',
      );
    });

    testWidgets('a read that fails outright offers a retry rather than an empty screen',
        (tester) async {
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])]
        ..chainFailure = const ApiUnreachable();

      await openNegotiationAsProvider(tester, bidding);

      expect(find.byKey(const Key('negotiation-failed')), findsOneWidget);

      bidding.chainFailure = null;
      await tester.tap(find.byKey(const Key('negotiation-retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(Key('negotiation-offer-$openingOffer')), findsOneWidget);
    });

    testWidgets('a send in flight disables the button rather than sending twice', (tester) async {
      final gate = Completer<void>();
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])]
        ..sendGate = gate;

      await openNegotiationAsProvider(tester, bidding);

      await tester.enterText(find.byKey(const Key('negotiation-composer')), 'Nine works.');
      await tester.pump();
      await tester.tap(find.byKey(const Key('negotiation-send')));
      await tester.pump();

      expect(tester.widget<IconButton>(find.byKey(const Key('negotiation-send'))).onPressed, isNull);
      await tester.tap(find.byKey(const Key('negotiation-send')), warnIfMissed: false);
      await tester.pump();
      expect(
        bidding.sends,
        hasLength(1),
        reason: 'two taps are two actions and the key is per action, so the second would be a '
            'second message rather than a retry of the first',
      );

      gate.complete();
      await tester.pumpAndSettle();
      expect(bidding.sends, hasLength(1));
    });
  });

  group('nothing of the customer’s budget reaches the provider’s side of a negotiation', () {
    // **The group `Docs/01` §4.3's third clause is about, and the only shape that can catch it.**
    //
    // The platform's guard is a closed key set over the response, which cannot see a sentence; the
    // client's model guard in `budget_stays_on_the_customer_side_test.dart` is a closed key set over
    // the type, which cannot see one either. A screen composing "the customer has set a maximum" has
    // no budget field, no budget value and no digit, and passes both. Wave 10 and wave 11 each
    // proved it on a live tree.
    //
    // So this reads **the words on the provider's screen**, with the counter form open — which is
    // where a hint about what the other side can afford would most plausibly be written.

    testWidgets('no rendered text names a maximum, a ceiling, or anything the customer set',
        (tester) async {
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_countered()]
        ..messagePages = <ApiPage<Message>>[
          conversation(<Message>[
            aMessage(id: 'm1', sentBy: BidParty.provider, body: 'Is there a lift?'),
            aMessage(id: 'm2', sentBy: BidParty.customer, body: 'There is, but it is out.'),
          ]),
        ];

      await openNegotiationAsProvider(tester, bidding);
      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();

      final rendered = renderedText(tester);

      // **Not vacuous.** The screen has to be drawing a negotiation before "it names no maximum"
      // means anything at all — which is the failure wave 11 recorded, where thirteen Dart tests
      // asserted over empty lists and passed.
      expect(rendered, contains(r'$520.00'.toLowerCase()));
      expect(rendered, contains(r'$400.00'.toLowerCase()));
      expect(rendered, contains('is there a lift?'));
      expect(rendered, contains('answer with different terms'));

      for (final phrase in <String>[
        'budget',
        'maximum',
        'max price',
        'ceiling',
        'price cap',
        'willing to pay',
        'can afford',
        'within their',
        'over their',
        'under their',
        'they set',
        'has set a',
        'they were prepared',
        'room to move',
      ]) {
        expect(
          rendered,
          isNot(contains(phrase)),
          reason: '\n\nThe negotiation screen renders the words “$phrase” to a provider.\n\n'
              'Docs/01 §4.3 forbids the customer’s maximum as an amount, as a band, **and as a\n'
              '“budget supplied” indicator** — and the third clause is the one that survives every\n'
              'other guard, because a sentence carries no field, no value and no digit. Wave 10\n'
              'and wave 11 both found exactly this form alive after a closed key set, a word\n'
              'search over source, a value search and an AST walk had all passed it.\n\n'
              'The two numbers on this screen that are legitimate are the provider’s own price and\n'
              'the customer’s counter amount — a number the customer chose to put in front of\n'
              'them. Neither is derived from the maximum and nothing here may derive one from the\n'
              'other.\n\n'
              'If the screen genuinely needs to relate an offer to what the customer can spend,\n'
              'that is a product decision about Docs/01 §4.3 and belongs in the document before it\n'
              'belongs in a widget.\n',
        );
      }
    });

    testWidgets('the same holds with a refusal, an empty chain and a closed negotiation on screen',
        (tester) async {
      // The states nobody writes copy for first. A disclosure is likeliest to be written into the
      // sentence that explains *why* something was refused — which is the one place a well-meaning
      // change would put "they cannot go that high".
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[
          <Bid>[aBid(id: openingOffer, jobId: _job, amountCents: 52000, status: BidStatus.expired)],
        ]
        ..messagePages = <ApiPage<Message>>[conversation(<Message>[])];

      await openNegotiationAsProvider(tester, bidding);

      final rendered = renderedText(tester);

      expect(rendered, contains('there is no offer on the table'));
      for (final phrase in <String>['budget', 'maximum', 'ceiling', 'afford', 'has set a']) {
        expect(rendered, isNot(contains(phrase)), reason: 'the closed state renders “$phrase”');
      }
    });

    testWidgets('a party’s own words are rendered as written, which is the limit of that guard',
        (tester) async {
      // **Recorded rather than hidden.** `Docs/01` §4.3 binds the platform and the product; it does
      // not bind the customer's own mouth. A customer who types their limit into a message has
      // disclosed it themselves, the platform stores what they wrote unaltered — "the platform
      // contributes no prose to it at all" — and this screen renders it.
      //
      // The alternative is worse in both directions: suppressing words inside party-authored text
      // would censor a negotiation, and it would leave the actual hole — this screen's own copy —
      // exactly where it was. So the guard above is scoped to what the app says, and this test says
      // so out loud rather than leaving the next person to discover it as a gap.
      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[_opening()]
        ..messagePages = <ApiPage<Message>>[
          conversation(<Message>[
            aMessage(
              id: 'm1',
              sentBy: BidParty.customer,
              body: 'My budget is tight, so 400 is where I have to land.',
            ),
          ]),
        ];

      await openNegotiationAsProvider(tester, bidding);

      expect(
        find.text('My budget is tight, so 400 is where I have to land.'),
        findsOneWidget,
        reason: 'what a party wrote is theirs, and the platform stores it unaltered',
      );
    });

    testWidgets('every string on the provider’s screen is one somebody recorded', (tester) async {
      // **The closed-world half, and the reason it exists is that the ban-list above lost.**
      //
      // The orchestrator ran a sharper mutation than the one this file was written against:
      // `The customer cannot go higher than this.` — a paraphrase, in the provider's counter form,
      // naming none of the fourteen phrases. All 1030 tests passed. It then proved the fixture was
      // not the reason, by swapping only the wording at the same location to the known phrase and
      // watching the ban-list fail. So the location was reachable, the guard was live, and the
      // paraphrase simply walked past it.
      //
      // **That is not a phrase the list forgot. It is the shape of a ban-list.** Wave 10 beat a
      // closed key set with a sentence; wave 11 beat a word search with the same sentence; a phrase
      // list is the next rung and it loses to a thesaurus. Every wave so far has patched the
      // instance and kept the shape.
      //
      // So this inverts it. Every string the provider's screen renders must be one of three things:
      // copy this file records by hand, a value whose *shape* is written down, or the exact words a
      // party typed into this fixture. **Anything else fails, whatever it says** — which makes
      // adding provider-visible copy a decision somebody recorded rather than something that
      // happened, exactly as `budget_stays_on_the_customer_side_test.dart` does for model keys. It
      // is the same principle moved from the type to the pixels, and it is the only form that does
      // not have to anticipate the wording.
      //
      // The state rendered is the richest one the provider can reach: both parties' offers with
      // conditions on each, the counter form open and seeded from the live head, and a conversation
      // with a message from each side.
      const providerAsked = 'Is there a lift, or is it stairs to the second floor?';
      const customerAnswered = 'There is a lift, but it is out until Thursday.';
      const providerConditions = 'Tail lift required.';
      const customerConditions = 'Happy at this price if you can make it Thursday.';

      final bidding = FakeBiddingRepository()
        ..chains = <List<Bid>>[
          <Bid>[
            aBid(
              id: openingOffer,
              jobId: _job,
              amountCents: 52000,
              offeredBy: BidParty.provider,
              status: BidStatus.superseded,
              supersededBy: 'offer-2',
              message: providerConditions,
            ),
            aBid(
              id: 'offer-2',
              jobId: _job,
              amountCents: 40000,
              offeredBy: BidParty.customer,
              message: customerConditions,
            ),
          ],
        ]
        ..messagePages = <ApiPage<Message>>[
          conversation(<Message>[
            aMessage(id: 'm1', sentBy: BidParty.provider, body: providerAsked),
            aMessage(id: 'm2', sentBy: BidParty.customer, body: customerAnswered),
          ]),
        ];

      await openNegotiationAsProvider(tester, bidding);
      await tester.tap(find.byKey(const Key('negotiation-counter-open')));
      await tester.pumpAndSettle();

      // **Exactly the words this fixture wrote, not free text generally.** The carve-out below is
      // for what a party typed — a customer who names their own limit in a message has disclosed it
      // themselves — and admitting arbitrary strings under that heading would hand the guard back
      // its hole. These four values and no others.
      const partyWords = <String>{
        providerAsked,
        customerAnswered,
        providerConditions,
        customerConditions,
      };

      final rendered = renderedStrings(tester);

      // **Not vacuous**, and checked before anything is concluded from an absence: wave 11 recorded
      // thirteen Dart tests that passed while asserting over empty lists.
      expect(rendered, contains(r'$520.00'));
      expect(rendered, contains(r'$400.00'));
      expect(rendered, contains(providerAsked));
      expect(rendered, contains(customerAnswered));
      expect(rendered, contains('Answer with different terms'));

      final unrecorded = rendered
          .where((data) => !_providerCopy.contains(data))
          .where((data) => !partyWords.contains(data))
          .where((data) => !_computed.any((shape) => shape.hasMatch(data)))
          .toSet();

      expect(
        unrecorded,
        isEmpty,
        reason: '\n\nThe negotiation screen renders a string to a **provider** that nothing in this '
            'file records:\n\n'
            '  ${unrecorded.map((s) => '“$s”').join('\n  ')}\n\n'
            'This assertion is closed-world on purpose. Docs/01 §4.3 forbids the customer’s\n'
            'maximum as an amount, as a band, **and as a “budget supplied” indicator** — and the\n'
            'third form is a sentence, which carries no field, no value and no digit. A list of\n'
            'banned phrases loses to a paraphrase; this one loses to nothing, because it fails on\n'
            'anything it was not told about.\n\n'
            'If the string above is legitimate copy, add it to `_providerCopy` — that is the\n'
            'decision being recorded, and it is a decision about what a provider may be told. If\n'
            'it is a value the screen computes, add its **shape** to `_computed`, tightly enough\n'
            'that prose cannot match it. If it is something a party typed, it belongs in this\n'
            'test’s `partyWords`, which is the fixture’s own four strings and not a licence.\n\n'
            'If it relates an offer to what the customer can spend, in any wording at all, it is a\n'
            'product decision about Docs/01 §4.3 and belongs in the document before it belongs in\n'
            'a widget.\n',
      );
    });

    test('every recorded string is still copy the negotiation actually carries', () {
      // **The other direction, and the reason `budget_stays_on_the_customer_side_test.dart` has its
      // own version of it**: an allow-list that outlives its copy quietly permits a sentence
      // somebody could reintroduce under a wording that was retired for a reason. A reworded line
      // has to be re-recorded rather than inherited.
      //
      // Adjacent Dart string literals are joined before searching, because the source wraps a long
      // sentence across two quoted parts and the rendered string has no such seam.
      final source = <String>[
        'lib/features/bidding/negotiation_screen.dart',
        'lib/features/bidding/negotiation_controller.dart',
        'lib/features/bidding/instant_field.dart',
      ].map((path) => File(path).readAsStringSync()).join('\n').replaceAll(RegExp(r"'\s*\n\s*'"), '');

      final stale = _providerCopy.where((copy) => !source.contains(copy)).toList();

      expect(
        stale,
        isEmpty,
        reason: '\n\nThese strings are recorded as copy a provider may be shown and no longer '
            'appear in the negotiation’s source:\n\n'
            '  ${stale.map((s) => '“$s”').join('\n  ')}\n\n'
            'Delete them, or correct them to what the screen now says. A recorded string that\n'
            'matches nothing is a slot the closed-world assertion above would wave through.\n',
      );
    });
  });
}

/// Every sentence, label and word the negotiation is allowed to put in front of a **provider**.
///
/// **Hand-written, and that is the whole mechanism.** Deriving this from the source would admit
/// whatever the source said, which is the property being guarded against — the point is that a new
/// provider-visible string fails until a person decides it may be shown. The companion test above
/// keeps it from drifting the other way.
///
/// It deliberately includes the **refusal** copy, which lives in `negotiation_controller.dart`:
/// explaining why the platform said no is the most plausible place anybody would write "they cannot
/// go higher than that", and it is the copy least likely to be read by a reviewer looking at a
/// screen.
///
/// What is **not** here, and is therefore fail-closed rather than allowed: the stale-read banner,
/// which interpolates `ApiFailure.userMessage` — the platform's own free text. No fixture in this
/// group renders it, so no shape admits it, and a test that renders one will fail until somebody
/// decides what that means. See `Docs/11` §3.
const _providerCopy = <String>{
  // The screen itself.
  'Negotiation',
  'Offers',
  'Messages',
  'No offers have been read yet.',
  'No price on this offer',
  'Offer',
  'Yours',
  'Theirs',
  'On the table',
  'There is no offer on the table. Nothing further can be proposed here, and you can still send a message.',
  'It is your turn. Answer with different terms, or send a message.',
  'Your terms are with the other party. They can accept them, answer them, or write back.',
  'Show later messages',
  'No messages yet. Counter-offers say what the terms are; this is where the questions go.',
  'Write to the other party',
  'Questions a price cannot answer — access, timing, what is being moved.',
  'Close',
  'Try again',

  // The counter form.
  'Counter this offer',
  'Answer with different terms',
  'Anything you leave as it is stays as it is. Changing nothing at all is agreement, which is a different thing from a counter-offer.',
  'This counter-offer changes nothing. Propose a different price, a different time or different conditions — or award the delivery if you agree with it.',
  'Your price',
  r'$',
  'Australian dollars, for the whole job. Clear the box to leave the price as it is.',
  'Collecting at',
  'Delivered by',
  'Keeping the time on the table',
  'Conditions (optional)',
  'Clear the box to remove the conditions on the offer.',
  'Send counter-offer',
  'Cancel',
  'Counter-offers are sent straight away and are not held on your phone.',

  // `instant_field.dart`.
  'Choose',
  'Change',

  // The refusals, from `negotiation_controller.dart`.
  'That offer is now yours to wait on rather than to answer — the other party has already replied. Reload the negotiation to see where it stands.',
  'This delivery has been awarded. The terms are settled, and you can still send a message to arrange the details.',
  'This negotiation is closed. The offer has been answered, withdrawn or has expired, so there is nothing left to counter.',
  'That did not go through. Send the counter-offer again.',
  'This negotiation could not be found. Open it again from the offer.',
  'This conversation could not be found. Open it again from the offer.',
  'That did not go through. Send the message again.',
};

/// A day-first instant as `dayFirstDateTime` renders one — `20 Aug 2026, 4:30 am`.
///
/// Written as a **shape rather than a value**, because the alternative is building the expectation
/// out of the formatter the screen uses, which is the mistake `rules_test.go` made: a `want`
/// computed from the subject agrees with a mutation by construction.
const _instant = r'\d{1,2} [A-Z][a-z]{2} \d{4}, \d{1,2}:\d{2} [ap]m';

/// The shapes of the values the screen computes rather than writes.
///
/// **Anchored at both ends, every one of them.** An unanchored pattern is a hole: a mutant that
/// appended a sentence to a price line would still match `\$[\d,]+\.\d{2}` somewhere inside itself.
final _computed = <RegExp>[
  // A price, as `audFromCents` renders it.
  RegExp(r'^\$-?[\d,]+\.\d{2}$'),

  // The same price seeded into the counter form's own box, which has no currency symbol.
  RegExp(r'^\d+\.\d{2}$'),

  // One offer's two commitments, either of which may be absent.
  RegExp('^Collect (?:$_instant|not stated) · deliver by (?:$_instant|not stated)\$'),

  // Who wrote a message and when.
  RegExp('^(?:You|Them) · $_instant\$'),
];
