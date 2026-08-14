// SHIP-133 — "Customer sees the latest confirmed milestone and proof of delivery".
//
// Driven through the real app: the session, the router, the guard, the customer's own job screen and
// the button on it. Only the token store and the repositories are substituted, so a missing entry in
// `_signedInPatterns` — which would send the link to the home shell — fails here rather than only on
// a device.
//
// # Four things this file is careful about
//
// **"Confirmed" is the platform's record and nothing else.** Everything this endpoint serves has
// been accepted; what is unconfirmed lives in the *provider's* queue, on their handset, and a
// customer must never be shown it. The assertion that matters is therefore that the newest row wins
// and the screen does not re-sort — the endpoint sorts by the actor's clock, and a client with a
// second opinion about that order shows a delivery that ran backwards.
//
// **Proof has two shapes and the branch is on the reason, not on the URL.** A reasoned exception has
// no `download_url` at all, so a screen that branched on the URL would draw an exception as a broken
// photograph — and would draw an *expired* photograph as an exception, which is worse: it would
// invent a reason nobody recorded.
//
// **The photograph's URL is a credential.** No test asserts a fetch, because none can: `flutter_test`
// answers every HTTP request `400`, so `Image.network` lands in its `errorBuilder`. That is the
// expired-link path a customer actually meets, and it is walked rather than mocked away.
//
// **The two clocks.** `recorded_at` is when the driver says they acted and `accepted_at` is when
// Shipper received it, hours later for anything recorded out of signal. The fixture sets them to
// different values on purpose, and the screen must show the first.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/app.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/delivery/delivery_tracking.dart';
import 'package:shipper/features/delivery/proof_exception_reason.dart';

import '../../core/auth/session_fixtures.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import '../jobs/fake_jobs_repository.dart';
import 'fake_delivery_repository.dart';

const _job = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1';

ApiPage<RecordedMilestone> _page(
  List<RecordedMilestone> milestones, {
  String? next,
  bool more = false,
}) =>
    ApiPage<RecordedMilestone>(data: milestones, nextCursor: next, hasMore: more);

/// Signs a customer in, opens their job, and taps through to the delivery **the way a person does**.
///
/// Through the button on the job screen rather than by pumping the tracking screen, because the
/// route has to be reachable: a location missing from `_signedInPatterns` lands on the home shell,
/// which from the outside is a button that does nothing.
Future<void> openTracking(
  WidgetTester tester,
  FakeDeliveryRepository delivery, {
  FakeJobsRepository? jobs,
}) async {
  // A phone-shaped surface rather than the 800×600 default, and a tall one: a photograph, a driver
  // and a history should scroll rather than be reported as overflowing.
  tester.view.physicalSize = const Size(800, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.customer);
  final owned = jobs ?? FakeJobsRepository();

  await tester.pumpWidget(signupApp(identity, delivery: delivery, jobs: owned));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();

  // The job screen is reached by link and the tracking screen by the button on it, so both halves of
  // the way in are exercised.
  await followLink(tester, Routes.jobDetailFor(_job));
  await tester.tap(find.byKey(const Key('track-delivery')));
  await tester.pumpAndSettle();
}

/// Delivers [location] to the running application the way the platform delivers a deep link.
Future<void> followLink(WidgetTester tester, String location) async {
  final context = tester.element(find.byType(ShipperApp));
  ProviderScope.containerOf(context, listen: false).read(routerProvider).go(location);
  await tester.pumpAndSettle();
}

void main() {
  group('the latest confirmed milestone', () {
    testWidgets('is the newest the platform holds, in Docs/02 §1’s own words', (tester) async {
      // Deliberately more than one, so "the latest" is a choice rather than the only row. The list
      // arrives newest first because that is how the endpoint sorts it — by the **actor's** clock.
      final delivery = FakeDeliveryRepository()
        ..pages = [
          _page(<RecordedMilestone>[
            aMilestone(id: 'm3', milestone: 'in_transit'),
            aMilestone(id: 'm2', milestone: 'picked_up'),
            aMilestone(id: 'm1', milestone: 'en_route_to_pickup'),
          ])
        ];

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking')), findsOneWidget);
      expect(find.byKey(const Key('tracking-latest-in_transit')), findsOneWidget);
      expect(find.byKey(const Key('tracking-latest-picked_up')), findsNothing);

      // `Docs/02` §1's own name (`CLAUDE.md`), so the customer and an audit entry agree.
      expect(find.text('In transit'), findsWidgets);
    });

    testWidgets('is shown on the actor’s clock, not the platform’s', (tester) async {
      // `Docs/02` §3.1 keeps the two apart, and they differ by however long a device was out of
      // signal. The fixture puts them on **different days** so a screen reading the wrong one cannot
      // pass — and both are rendered day-first with the month spelled, so neither can be misread.
      final delivery = FakeDeliveryRepository()
        ..pages = [
          _page(<RecordedMilestone>[
            aMilestone(
              id: 'm1',
              milestone: 'picked_up',
              recordedAt: '2026-09-08T04:40:11.000Z',
              acceptedAt: '2026-09-11T02:15:02.481Z',
            ),
          ])
        ];

      await openTracking(tester, delivery);

      expect(find.textContaining('Sep 2026'), findsWidgets);
      expect(
        find.textContaining('11 Sep'),
        findsNothing,
        reason: 'accepted_at is what support reasons about, and is not what a customer is shown',
      );
    });

    testWidgets('a milestone this build has never heard of is still named', (tester) async {
      // `Docs/07` §6 is built on old builds living on devices indefinitely, and the wire vocabulary
      // is the platform's. A sixth milestone must decode and be drawn rather than crash the screen
      // or be silently dropped from the list.
      final delivery = FakeDeliveryRepository()
        ..pages = [
          _page(<RecordedMilestone>[aMilestone(id: 'm1', milestone: 'handed_to_recipient')])
        ];

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking-latest-handed_to_recipient')), findsOneWidget);
    });

    testWidgets('driver_assigned is named properly, though this app cannot record it',
        (tester) async {
      // The fifth milestone. `Milestone` is four deliberately — a value this client cannot send is a
      // button somebody would eventually wire up — and this endpoint serves all five, so the read
      // side needs a name for it. A customer must not be shown `driver_assigned`.
      final delivery = FakeDeliveryRepository()
        ..pages = [
          _page(<RecordedMilestone>[aMilestone(id: 'm1', milestone: 'driver_assigned')])
        ];

      await openTracking(tester, delivery);

      expect(find.text('Driver assigned'), findsWidgets);
      expect(find.textContaining('driver_assigned'), findsNothing);
    });

    testWidgets('a repeat is kept rather than collapsed', (tester) async {
      // `Docs/02` §5: a driver who reaches a pickup, finds nobody there and sets off again records
      // `en_route_to_pickup` twice, and SHIP-115a's whole reason for existing is that this is the
      // **only** place such a row can be seen — neither writes a status change. A screen that
      // deduplicated would hide the afternoon a driver spent going back.
      final delivery = FakeDeliveryRepository()
        ..pages = [
          _page(<RecordedMilestone>[
            aMilestone(id: 'm2', milestone: 'en_route_to_pickup', reason: 'nobody at the gate'),
            aMilestone(id: 'm1', milestone: 'en_route_to_pickup'),
          ])
        ];

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking-milestone-m1')), findsOneWidget);
      expect(find.byKey(const Key('tracking-milestone-m2')), findsOneWidget);
      expect(find.text('nobody at the gate'), findsWidgets);
    });

    testWidgets('who recorded it is said only when it is not the ordinary case', (tester) async {
      final delivery = FakeDeliveryRepository()
        ..pages = [
          _page(<RecordedMilestone>[
            aMilestone(id: 'm2', milestone: 'in_transit', recordedBy: MilestoneActor.system),
            aMilestone(id: 'm1', milestone: 'picked_up', recordedBy: MilestoneActor.provider),
          ])
        ];

      await openTracking(tester, delivery);

      // `Docs/02` §1 calls the presentation statuses the platform's own reading of a condition
      // rather than an act anybody performed, and a customer seeing a step nobody took is owed that
      // sentence.
      expect(find.text('Recorded by Shipper'), findsWidgets);
      expect(
        find.textContaining('Recorded by the transport provider'),
        findsNothing,
        reason: 'the commonest case said under every row is noise that hides the two that matter',
      );
    });
  });

  group('proof of delivery', () {
    testWidgets('a photograph is rendered from the signed URL the platform issued', (tester) async {
      final delivery = FakeDeliveryRepository()
        ..pages = [
          _page(<RecordedMilestone>[aMilestone(id: 'm1', milestone: 'delivered')])
        ]
        ..proof_ = <DeliveryProof>[aPhotograph()];

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking-proof')), findsOneWidget);

      // The URL reaches the image widget unaltered. A client that rewrote, shortened or re-signed
      // one would be building a credential rather than presenting the one it was handed.
      final image = tester.widget<Image>(find.byKey(const Key('tracking-photograph')));
      expect(
        (image.image as NetworkImage).url,
        'https://store.example.com/shipper/proof/one?X-Amz-Algorithm=AWS4-HMAC-SHA256',
      );
    });

    testWidgets('a link that will not load says which two things it could be', (tester) async {
      // `flutter_test` answers every request `400`, so this is the real error path rather than a
      // simulated one — and it is exactly what a customer meets when a short-lived link runs out.
      // "Something went wrong" would be the wrong words: the photograph is safe and the link is not.
      final delivery = FakeDeliveryRepository()..proof_ = <DeliveryProof>[aPhotograph()];

      await openTracking(tester, delivery);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('tracking-photograph-failed')), findsOneWidget);
      expect(find.textContaining('short-lived'), findsOneWidget);
    });

    testWidgets('a reasoned exception is drawn as evidence, not as a missing photograph',
        (tester) async {
      // `Docs/01` §4.4 makes the two the same feature — delivered requires a photograph **or** a
      // recorded reason, never neither — and an exception is evidence rather than the absence of it.
      // A customer who read "no photo" as "nothing was recorded" would raise a dispute over a
      // delivery that went perfectly well.
      final delivery = FakeDeliveryRepository()
        ..proof_ = <DeliveryProof>[anException(reason: ProofExceptionReason.recipientObjected)];

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking-exception-recipient_objected')), findsOneWidget);
      expect(
        find.byKey(const Key('tracking-photograph')),
        findsNothing,
        reason: 'there is no object, so there is nothing to sign a URL for',
      );
      expect(
        find.textContaining('recipient_objected'),
        findsNothing,
        reason: 'the wire form is a database value, not a sentence for a customer',
      );
      expect(find.textContaining('asked not to be photographed'), findsOneWidget);
    });

    testWidgets('the branch is on the reason and not on the URL', (tester) async {
      // The subtle one, and the reason the contract says so in as many words. A photograph whose
      // link is absent — an old build, a store the platform could not sign against — must not be
      // drawn as an exception, because that would invent a reason nobody recorded.
      final delivery = FakeDeliveryRepository()
        ..proof_ = <DeliveryProof>[aPhotograph(downloadUrl: null, downloadExpiresAt: null)];

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking-evidence-unreadable')), findsOneWidget);
      for (final reason in ProofExceptionReasonCopy.offered) {
        expect(find.byKey(Key('tracking-exception-${reason.wireName}')), findsNothing);
      }
    });

    testWidgets('evidence for another milestone is shown under the delivery’s, not instead of it',
        (tester) async {
      // `POST /v1/jobs/{id}/milestones` accepts `proof` on any of the five, so a photographed
      // collection is an ordinary case rather than a leftover.
      final delivery = FakeDeliveryRepository()
        ..proof_ = <DeliveryProof>[
          aPhotograph(id: 'p1'),
          aPhotograph(id: 'p2', milestone: 'picked_up'),
        ];

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking-proof')), findsOneWidget);
      expect(find.byKey(const Key('tracking-evidence-p2')), findsOneWidget);
    });
  });

  group('the driver', () {
    testWidgets('a job nobody is on says so rather than showing nothing', (tester) async {
      // `driver_assigned: false` with no other field is a `200` and a complete answer. Branching on
      // a missing name would draw the same thing today and would break the day a name arrived
      // without an assignment.
      await openTracking(tester, FakeDeliveryRepository());

      expect(find.byKey(const Key('tracking-no-driver')), findsOneWidget);
    });

    testWidgets('an assigned driver is named, and their number is nowhere', (tester) async {
      // `driver_mobile` reaches the provider and not the customer, and the platform blanks it in the
      // service rather than leaving the handler to omit it. This screen is the customer's, so the
      // field is one that can never arrive — and is not modelled, so there is nothing to draw.
      final delivery = FakeDeliveryRepository()
        ..driver_ = const DeliveryDriver(
          jobId: _job,
          driverAssigned: true,
          driverName: 'Sam Patel',
          assignedAt: '2026-09-07T04:15:30.000Z',
        );

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking-driver')), findsOneWidget);
      expect(find.text('Sam Patel'), findsOneWidget);
      expect(find.textContaining('+61'), findsNothing);
      expect(find.textContaining('04'), findsNothing);
    });
  });

  group('paging, and what a further page must not re-read', () {
    testWidgets('the cursor goes back unchanged and the proof is not read again', (tester) async {
      // The second half is the one worth having: re-reading `/delivery/proof` would mint a fresh set
      // of signed URLs for photographs already on screen, which reloads every image a customer is
      // looking at for no reason at all.
      final delivery = FakeDeliveryRepository()
        ..pages = [
          _page(
            <RecordedMilestone>[aMilestone(id: 'm2', milestone: 'in_transit')],
            next: 'MR9yZWNvcmQ',
            more: true,
          ),
          _page(<RecordedMilestone>[aMilestone(id: 'm1', milestone: 'picked_up')]),
        ]
        ..proof_ = <DeliveryProof>[aPhotograph()];

      await openTracking(tester, delivery);
      expect(delivery.proofReads, 1);

      await tester.tap(find.byKey(const Key('tracking-more')));
      await tester.pumpAndSettle();

      expect(delivery.cursors, <String?>[null, 'MR9yZWNvcmQ']);
      expect(delivery.proofReads, 1, reason: 'a page of history is not a reason to re-sign a URL');

      // Appended, not replaced.
      expect(find.byKey(const Key('tracking-milestone-m1')), findsOneWidget);
      expect(find.byKey(const Key('tracking-milestone-m2')), findsOneWidget);
    });
  });

  group('nothing recorded, and failure', () {
    testWidgets('a delivery nothing has happened on is an empty state, never a spinner',
        (tester) async {
      // The ordinary state of every job before a provider does anything, **and of every draft**:
      // `Service.partyTo` makes the owner a party whatever the status, so the shelf answers with an
      // empty everything rather than refusing. That is what lets the button be offered from every
      // job without the device holding a copy of Docs/02 §2's table.
      await openTracking(tester, FakeDeliveryRepository());

      expect(find.byKey(const Key('tracking-nothing-yet')), findsOneWidget);
      expect(find.byKey(const Key('tracking-latest-none')), findsOneWidget);
      expect(find.byKey(const Key('tracking-loading')), findsNothing);
      expect(find.byKey(const Key('tracking-failed')), findsNothing);
    });

    testWidgets('a read that fails offers a retry rather than an empty delivery', (tester) async {
      // A stranger's job and a job that does not exist are the same `404`, byte-identically, and the
      // client must not try to be more specific than the platform was. One shape for every reason.
      final delivery = FakeDeliveryRepository()..failure = const ApiUnreachable();

      await openTracking(tester, delivery);

      expect(find.byKey(const Key('tracking-failed')), findsOneWidget);
      expect(find.byKey(const Key('tracking-nothing-yet')), findsNothing);

      delivery
        ..failure = null
        ..proof_ = <DeliveryProof>[anException()];

      await tester.tap(find.byKey(const Key('tracking-retry')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('tracking-exception-recipient_objected')), findsOneWidget);
    });

    testWidgets('a refresh that fails leaves the proof on screen', (tester) async {
      // Somebody whose refresh failed should still be looking at their proof of delivery, with a
      // banner saying the reload did not work — not with an error where it was.
      final delivery = FakeDeliveryRepository()..proof_ = <DeliveryProof>[anException()];

      await openTracking(tester, delivery);
      expect(find.byKey(const Key('tracking-exception-recipient_objected')), findsOneWidget);

      delivery.failure = const ApiUnreachable();
      await tester.fling(find.byKey(const Key('tracking')), const Offset(0, 400), 1000);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('tracking-exception-recipient_objected')), findsOneWidget);
      expect(find.byKey(const Key('tracking-failed')), findsNothing);
    });
  });
}
