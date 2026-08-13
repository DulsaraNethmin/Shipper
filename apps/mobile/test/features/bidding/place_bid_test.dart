// SHIP-100 — "a provider can review a job and submit a bid", end to end through the real app.
//
// The session, the router, the shell, the feed, the card, the job, the form, and the offer the
// platform recorded. Only the token store and the repositories are substituted.
//
// # The three things this file exists for
//
// **The journey.** The *Done when* is one sentence and it spans two features, so the test that
// demonstrates it has to as well: sign in, tap a job, read it, price it, send it, see it.
//
// **The idempotency key.** A dropped connection followed by a retry must not create two bids
// (CLAUDE.md, Docs/07 §4). What makes that true is that the *same* key goes out on both attempts —
// and, in the other direction, that a corrected offer after a refusal goes out under a *new* one.
// Both are asserted on the keys the repository actually received.
//
// **What is not offline.** Docs/07 §4 excludes bidding from the durable queue, and SHIP-124 closed
// `OperationKind` by making its constructor private. So there is nothing to assert about a queue
// here except that the request is made straight away and its failure is shown rather than absorbed.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/open_job.dart';

import '../jobs/fake_open_jobs_repository.dart';
import '../jobs/open_job_app.dart';
import 'fake_bidding_repository.dart';

const _sofa = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';

FakeOpenJobsRepository _feed() {
  final job = anOpenJob(id: _sofa);
  return FakeOpenJobsRepository()
    ..pages = [ApiPage<OpenJob>(data: <OpenJob>[job])]
    ..job = job;
}

/// A refusal that leaves it genuinely unknown whether the platform acted on the request.
///
/// The case idempotency exists for: the reply never came back, and the offer may or may not have
/// been recorded.
const _droppedConnection = ApiUnreachable();

/// A refusal the platform made, and told the client about.
const _refused = ApiErrorResponse(
  statusCode: 422,
  code: 'validation_failed',
  message: 'Some fields were rejected.',
  details: <ApiFieldError>[
    ApiFieldError(
      field: 'amount_cents',
      code: 'out_of_range',
      message: r'Enter an amount between $1 and $1000000.',
    ),
  ],
);

void main() {
  group('the Done when', () {
    testWidgets('a provider reviews a job and submits a bid', (tester) async {
      final feed = _feed();
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: feed, bidding: bidding, jobId: _sofa);

      // Reviewed: the job is on screen, read from the endpoint rather than handed down.
      expect(feed.jobReads, <String>[_sofa]);
      expect(find.text('Three-seat sofa, wrapped'), findsOneWidget);
      expect(find.byKey(const Key('bid-form')), findsOneWidget);

      await placeBidThrough(tester, amount: '450.50', message: 'Two people and a tail lift.');

      // Submitted: one attempt, against this job, carrying a key.
      expect(bidding.attempts, 1);
      expect(bidding.calls.single.jobId, _sofa);
      expect(bidding.keys.single, isNotEmpty);

      // And the offer the platform recorded is what is drawn.
      expect(find.byKey(const Key('bid-placed')), findsOneWidget);
      expect(find.byKey(const Key('bid-form')), findsNothing);
    });

    testWidgets('the price is sent as whole cents, not as dollars', (tester) async {
      // Money is never a decimal in transit (Docs/10 §3.3). `450.50` typed into a dollars box is
      // `45050`, and the conversion happens on the text rather than through a double.
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester, amount: '450.50');

      expect(bidding.bodies.single['amount_cents'], 45050);
    });

    testWidgets('both instants are sent as RFC 3339 with an offset', (tester) async {
      // The trap `rfc3339` exists for: a local `DateTime.toIso8601String()` carries no zone, and
      // `time.Parse(time.RFC3339, …)` refuses it — reaching the provider as "that is not a date"
      // about a date they picked from a calendar.
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester);

      final body = bidding.bodies.single;
      final rfc = RegExp(r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$');

      expect(body['pickup_at'], matches(rfc));
      expect(body['deliver_by'], matches(rfc));
    });

    testWidgets('the offer drawn afterwards is the platform’s, not the form’s', (tester) async {
      // Docs/02 §3.1 has the client reconcile to whatever the platform returns. Here that is not a
      // formality: a retry is answered with the offer the *first* attempt placed, so a screen that
      // rendered what was typed would show a provider a price they are not standing behind.
      final bidding = FakeBiddingRepository()..placed = aBid(amountCents: 39900);

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester, amount: '450');

      expect(find.text(r'$399.00'), findsOneWidget);
      expect(find.text(r'$450.00'), findsNothing);
    });

    testWidgets('nothing is drawn as offered until the platform has said so', (tester) async {
      // The honest version of "optimistic local state" on a surface that is never offline: the
      // attempt is marked, and the offer is not. Showing a bid as placed before the platform
      // confirmed it would be the stale local decision Docs/07 §4 refuses.
      final gate = Completer<void>();
      final bidding = FakeBiddingRepository()..gate = gate;

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await tester.enterText(find.byKey(const Key('bid-amount')), '450');
      await tester.pump();
      await chooseInstant(tester, 'bid-pickup');
      await chooseInstant(tester, 'bid-deliver-by');

      await tester.tap(find.byKey(const Key('bid-submit')));
      await tester.pump();

      expect(bidding.attempts, 1);
      expect(find.byKey(const Key('bid-placed')), findsNothing);
      // And the button is disabled while it is in flight, which is what stops a second tap becoming
      // a second attempt under a second key.
      expect(tester.widget<FilledButton>(find.byKey(const Key('bid-submit'))).onPressed, isNull);

      gate.complete();
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('bid-placed')), findsOneWidget);
    });
  });

  group('a dropped connection and then a retry', () {
    testWidgets('sends the same idempotency key, and does not create a second bid', (tester) async {
      // The failure the key exists for. The request may or may not have arrived, so the retry has
      // to be the *same action*: SHIP-84 stores the key against the bid, so the platform answers
      // `200` with the offer the first attempt placed rather than refusing the second as
      // `bidding_already_bid`.
      final bidding = FakeBiddingRepository()..failure = _droppedConnection;

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester, amount: '450');

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.byKey(const Key('bid-placed')), findsNothing);

      bidding.failure = null;
      await tester.tap(find.byKey(const Key('bid-submit')));
      await tester.pumpAndSettle();

      expect(bidding.attempts, 2);
      expect(
        bidding.keys[0],
        bidding.keys[1],
        reason: 'a retry of the same action must carry the key its first attempt carried '
            '(Docs/07 §4) — a fresh key is a second bid',
      );
      expect(find.byKey(const Key('bid-placed')), findsOneWidget);
    });

    testWidgets('a corrected offer after a refusal gets a new key', (tester) async {
      // The other direction, and it is as wrong to get backwards. A `422` means the platform saw
      // the request and refused it, so retrying under the same key could only replay that refusal —
      // and the platform fingerprints method, path and body, so a changed body under an old key is
      // `idempotency_key_reused`.
      final bidding = FakeBiddingRepository()..failure = _refused;

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester, amount: '99999999');

      bidding.failure = null;
      await tester.enterText(find.byKey(const Key('bid-amount')), '450');
      await tester.pump();
      await tester.tap(find.byKey(const Key('bid-submit')));
      await tester.pumpAndSettle();

      expect(bidding.attempts, 2);
      expect(
        bidding.keys[0],
        isNot(bidding.keys[1]),
        reason: 'a different offer is a different action, and one key per action',
      );
      expect(bidding.bodies[1]['amount_cents'], 45000);
    });

    testWidgets('two taps on one screen are one attempt', (tester) async {
      // Two taps are two actions and the key is per action, so the second would not be absorbed by
      // the first — one of them would be refused as a second offer. The button is disabled while
      // one is in flight, and the controller refuses re-entry as well.
      final gate = Completer<void>();
      final bidding = FakeBiddingRepository()..gate = gate;

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await tester.enterText(find.byKey(const Key('bid-amount')), '450');
      await tester.pump();
      await chooseInstant(tester, 'bid-pickup');
      await chooseInstant(tester, 'bid-deliver-by');

      await tester.tap(find.byKey(const Key('bid-submit')));
      await tester.pump();
      await tester.tap(find.byKey(const Key('bid-submit')), warnIfMissed: false);
      await tester.pump();

      expect(bidding.attempts, 1);

      gate.complete();
      await tester.pumpAndSettle();
    });

    testWidgets('an offer already placed cannot be sent again from this screen', (tester) async {
      // The form is gone once the platform has the offer, and the controller refuses a second
      // placement even if something reached it. Revising is a different endpoint and a different
      // screen.
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester);

      expect(find.byKey(const Key('bid-submit')), findsNothing);
      expect(find.byKey(const Key('bid-revision-later')), findsOneWidget);
      expect(bidding.attempts, 1);
    });
  });

  group('what the platform refused', () {
    testWidgets('a field message goes under the field it names', (tester) async {
      // internal/bidding answers `validation_failed` with one `details` entry per offending field,
      // keyed by the contract's own field names, precisely so a form can do this.
      final bidding = FakeBiddingRepository()..failure = _refused;

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester, amount: '99999999');

      expect(find.text(r'Enter an amount between $1 and $1000000.'), findsOneWidget);
      // And the form is still there with what was typed in it. Closing it would throw away the
      // provider's work along with the explanation of what was wrong with it.
      expect(find.byKey(const Key('bid-form')), findsOneWidget);
      expect(find.text('99999999'), findsOneWidget);
    });

    testWidgets('the message goes when the value it was about is corrected', (tester) async {
      // A server message that outlives the value it was about sends somebody looking for a mistake
      // in a price they have already fixed.
      final bidding = FakeBiddingRepository()..failure = _refused;

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester, amount: '99999999');

      await tester.enterText(find.byKey(const Key('bid-amount')), '450');
      await tester.pumpAndSettle();

      expect(find.text(r'Enter an amount between $1 and $1000000.'), findsNothing);
    });

    testWidgets('a live offer already standing is said as such, not as a bad field', (tester) async {
      // `409` rather than `422`: the values are well formed and the request contradicts the state
      // the job is in, so the provider's next action is a different screen rather than a
      // correction. The client branches on the **code** and never on the message.
      final bidding = FakeBiddingRepository()
        ..failure = const ApiErrorResponse(
          statusCode: 409,
          code: 'bidding_already_bid',
          message: 'You already have a live offer on this job. Revise or withdraw it rather than '
              'placing a second.',
        );

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester);

      expect(find.byKey(const Key('bid-already-placed')), findsOneWidget);
      // Not in the ordinary failure banner, which is where a correctable problem goes.
      expect(find.byKey(const Key('failure-banner')), findsNothing);
      expect(find.textContaining('already have a live offer'), findsOneWidget);
    });

    testWidgets('a job this provider may not bid on refuses the offer, not the form', (tester) async {
      // Eligibility is the platform's decision and a `404` is what it answers — byte-identically to
      // a job that does not exist. Nothing on this device pre-empts it: the form is offered, the
      // request is sent, and the refusal is rendered.
      final bidding = FakeBiddingRepository()
        ..failure = const ApiErrorResponse(
          statusCode: 404,
          code: 'not_found',
          message: 'No such job.',
        );

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester);

      expect(bidding.attempts, 1, reason: 'the client must not decide eligibility for itself');
      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(find.byKey(const Key('bid-placed')), findsNothing);
    });
  });

  group('what the form does before it sends anything', () {
    testWidgets('an empty form is refused here rather than by the platform', (tester) async {
      // Presence, which is the half Docs/07 §2 leaves to the app: a round trip to be told the price
      // was left blank is a round trip on mobile data in a truck yard.
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await tester.tap(find.byKey(const Key('bid-submit')));
      await tester.pumpAndSettle();

      expect(bidding.attempts, 0);
      expect(find.text('Enter what you are asking for this job.'), findsOneWidget);
      expect(find.text('Say when you can collect.'), findsOneWidget);
      expect(find.text('Say when it will be delivered.'), findsOneWidget);
    });

    testWidgets('a price that is not a number is refused as a shape', (tester) async {
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await tester.enterText(find.byKey(const Key('bid-amount')), 'four fifty');
      await tester.pump();
      await tester.tap(find.byKey(const Key('bid-submit')));
      await tester.pumpAndSettle();

      expect(bidding.attempts, 0);
      expect(find.text('Enter an amount, like 450 or 450.50.'), findsOneWidget);
    });

    testWidgets('no bound is checked here, and a large price still reaches the platform',
        (tester) async {
      // The maximum is `internal/bidding`'s and Docs/06 §5.3 keeps it server-side: a copy compiled
      // in here could not be corrected without a store release. What comes back instead is
      // `out_of_range` with the real number in it.
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester, amount: '99999999');

      expect(bidding.attempts, 1);
      expect(bidding.bodies.single['amount_cents'], 9999999900);
    });

    testWidgets('collection after delivery is sent, because the ordering rule is the platform’s',
        (tester) async {
      // Both instants are chosen from the same pickers and land on the same value, so the offer the
      // form sends has delivery not *after* collection — which `Offer.validate` refuses. The form
      // sends it anyway, which is the point: two definitions of a rule is one more than Docs/07 §3
      // permits, and the one on the device is the one that cannot be corrected.
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester);

      expect(bidding.attempts, 1);
      expect(bidding.bodies.single['pickup_at'], bidding.bodies.single['deliver_by']);
    });

    testWidgets('an empty conditions box sends no message key at all', (tester) async {
      final bidding = FakeBiddingRepository();

      await openJobDetail(tester, feed: _feed(), bidding: bidding, jobId: _sofa);
      await placeBidThrough(tester);

      expect(bidding.bodies.single.containsKey('message'), isFalse);
    });

    testWidgets('the screen says offers are not held on the phone', (tester) async {
      // Docs/07 §4 excludes bidding from the durable queue, so a provider with no signal has to be
      // told which of the two this screen is rather than discovering it when nothing arrives.
      await openJobDetail(tester, feed: _feed(), jobId: _sofa);

      expect(find.byKey(const Key('bid-not-queued-note')), findsOneWidget);
    });
  });
}
