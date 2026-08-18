// Every string every provider-facing screen puts in front of a provider, recorded one screen at a
// time.
//
// # Why this file is not in a feature directory
//
// **The provider surface is not a feature and it is not a route.** The gate is a widget —
// `lib/core/auth/provider_only.dart:96` — and `app_router.dart:302` says the router is "deliberately
// blind to the role", so there is no directory, no route prefix and no `CustomerOnly` counterpart to
// hang this off. What a provider can reach spans `features/jobs`, `features/bidding` and
// `shared/design_system`, and the one thing those screens have in common is the invariant rather
// than the package. So the file sits beside the feature directories, as `architecture_test.dart`
// does, and names the cross-cutting concern instead of a feature.
//
// # What it asserts, and why it is closed-world
//
// `Docs/01` §4.3 keeps the customer's maximum away from a provider **as an amount, as a band, and as
// a "budget supplied" indicator** — and the third is a sentence, which carries no field, no value
// and no digit. Four waves of guards each lost to a different mechanism, so this bans nothing. It
// **records** what each screen may say and fails on everything else, through every channel
// `perceivable` reaches — including the semantics tree, where a disclosure can leave the visible
// screen byte-identical and change only what is spoken.
//
// It is not the only guard on these screens and does not replace the ones already there.
// `provider_job_feed_test.dart`, `open_job_screen_test.dart` and `my_bids_test.dart` each render a
// payload carrying a budget and assert that no rendering of it appears; those are **taint** checks
// over a value the fixture chose, and this is the **closed world** over everything else. Neither
// subsumes the other, and both are cheap.
//
// # The one piece of copy that reads like a disclosure and is not
//
// `place_bid_panel.dart:258` says *"Shipper never shows you what the customer is willing to pay."*
// A shared ban-list fails on that sentence on its first run, which is itself the argument for
// recording copy rather than banning phrases: the sentence is a **policy** and not a fact about this
// job — the same words on a payload carrying four budget-shaped keys and on one carrying none — and
// `open_job_screen_test.dart` already holds it to exactly that.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';
import 'package:shipper/shared/formatting/dates.dart';

import '../core/auth/session_fixtures.dart';
import '../support/disclosure.dart';
import 'bidding/fake_bidding_repository.dart';
import 'identity/fake_identity_repository.dart';
import 'identity/signup_app.dart';
import 'jobs/fake_open_jobs_repository.dart';

/// The instant `anOpenJob` closes bidding at, and it must stay equal to that fake's default —
/// `fake_open_jobs_repository.dart`'s `expiresAt`. Held here so the expectation above is computed
/// from the same instant the fixture renders rather than from a transcription of one machine's
/// clock.
const _biddingClosesAt = '2026-08-25T03:30:00.000Z';

const _sofa = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';
const _pallet = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1';

/// The provider's own offer. **The only amount any of these screens is entitled to draw**, and the
/// reason every money assertion below is about provenance rather than about shape.
const _theirOwnPrice = 45000;

/// What a customer's maximum would be, if one ever reached a provider. On the wire in the taint
/// tests and never anywhere a screen could legitimately read it.
const _theCustomersMaximum = 150000;

ApiPage<OpenJob> _jobs(List<OpenJob> jobs) => ApiPage<OpenJob>(data: jobs);
ApiPage<Bid> _offers(List<Bid> bids) => ApiPage<Bid>(data: bids);

/// Signs a provider in and stops on their feed, which **is** the provider half of the shell.
///
/// Through the real app — session, router, guard, shell — rather than by pumping a screen, because a
/// route missing from `_signedInLocations` sends the app to the home shell, which from the outside
/// is indistinguishable from a button that does nothing.
Future<void> _signInAsProvider(
  WidgetTester tester, {
  required FakeOpenJobsRepository feed,
  FakeBiddingRepository? bidding,
}) async {
  tallPhone(tester);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.provider);

  await tester.pumpWidget(signupApp(identity, openJobs: feed, bidding: bidding));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();
}

/// …and walks them to one job by tapping its card, the way a person gets there.
Future<void> _openJobDetail(
  WidgetTester tester, {
  required FakeOpenJobsRepository feed,
  FakeBiddingRepository? bidding,
}) async {
  await _signInAsProvider(tester, feed: feed, bidding: bidding);
  await tester.tap(find.byKey(const Key('open-job-$_sofa')));
  await tester.pumpAndSettle();
}

/// …or to their own offers, by the button on the feed.
Future<void> _openMyBids(
  WidgetTester tester, {
  required FakeOpenJobsRepository feed,
  required FakeBiddingRepository bidding,
}) async {
  await _signInAsProvider(tester, feed: feed, bidding: bidding);
  await tester.tap(find.byKey(const Key('your-bids')));
  await tester.pumpAndSettle();
}

void main() {
  group('the open job screen and its bid panel', () {
    testWidgets('render nothing this file does not record', (tester) async {
      final feed = FakeOpenJobsRepository()..pages = [_jobs(<OpenJob>[anOpenJob(id: _sofa)])];

      await _openJobDetail(tester, feed: feed, bidding: FakeBiddingRepository());

      final perceived = await perceivable(tester);

      // **Not vacuous**, and checked before anything is concluded from an absence: wave 11 recorded
      // thirteen Dart tests that passed while asserting over empty lists.
      expect(perceived, contains('Offer to carry this job'));
      expect(perceived, contains('Newtown NSW 2042'));
      // **Derived, not written down.** `dayFirstDateTime` renders in the device's zone
      // (`dates.dart` calls `toLocal()`), so a literal here asserts the timezone of whichever
      // machine last ran the suite. This one was written as '9:00 am', which is 03:30Z seen from
      // UTC+05:30, and it passed locally and failed on CI — where the runner is UTC and the same
      // instant reads '3:30 am'. The closed-world shapes below were never affected: they match
      // the clock with a regex. Only the anti-vacuity assertion hard-coded a rendering.
      expect(perceived, contains('Bidding closes ${dayFirstDateTime(_biddingClosesAt)}'));

      expectOnlyRecordedStrings(
        perceived,
        surface: 'The open job screen and its bid panel',
        copy: _openJobCopy,
        typedByAParty: _whatTheCustomerWrote,
        computed: _openJobShapes,
      );
    });

    testWidgets('and still render nothing unrecorded once the offer is placed', (tester) async {
      // A second state, because the collector reads **one frame**. The placed panel draws the
      // provider's own price, which is the one amount this screen may show — declared in cents here
      // rather than matched by shape, so a second amount from anywhere else fails.
      final feed = FakeOpenJobsRepository()..pages = [_jobs(<OpenJob>[anOpenJob(id: _sofa)])];
      final bidding = FakeBiddingRepository()
        ..placed = aBid(id: 'b1', jobId: _sofa, amountCents: _theirOwnPrice);

      await _openJobDetail(tester, feed: feed, bidding: bidding);
      await _placeAnOffer(tester);

      final perceived = await perceivable(tester);

      expect(perceived, contains('Your offer is with the customer'));
      expect(perceived, contains(r'$450.00'));

      expectOnlyRecordedStrings(
        perceived,
        surface: 'The open job screen with an offer placed',
        copy: _openJobCopy,
        typedByAParty: _whatTheCustomerWrote,
        amounts: const <int>{_theirOwnPrice},
        computed: _openJobShapes,
      );
    });

    testWidgets('a refusal the platform words itself is inside the guard, not outside it',
        (tester) async {
      // **The live free-text channel on this screen, and it is recorded rather than closed.**
      // `place_bid_panel.dart:403` and `failure_banner.dart:49` both render
      // `ApiFailure.userMessage` verbatim, which is `ApiErrorResponse.message` — the platform's own
      // envelope. On the Go side `httpx.NewError` builds it with `fmt.Sprintf(format, a...)`, so it
      // is a template a handler can interpolate anything into.
      //
      // No provider-reachable message interpolates a budget today, measured rather than taken on
      // report: five `NewError` calls across `internal/bidding`, `internal/jobs` and
      // `internal/delivery` take a format argument, and every one interpolates either the client's
      // own `?status=` value or the literal `Idempotency-Key` header name. So this is an **open
      // channel and not a live leak**, and what is provable from the client is that the channel is
      // *covered* — a message that did disclose fails this screen's guard rather than passing
      // through it. Recording that is worth more than a claim it cannot happen.
      final feed = FakeOpenJobsRepository()
        ..pages = [_jobs(<OpenJob>[anOpenJob(id: _sofa)])]
        ..jobFailure = const ApiErrorResponse(
          statusCode: 409,
          code: 'bidding_over_budget',
          message: 'The customer will not go above \$1,500.00 on this job.',
          requestId: '01930f4c-7c9a-7c2e-9a3d-6c2c0f4b0e11',
        );

      await _openJobDetail(tester, feed: feed, bidding: FakeBiddingRepository());

      final perceived = await perceivable(tester);

      // The channel reaches the collector at all, which is the half that could be vacuous.
      expect(perceived, contains('The customer will not go above \$1,500.00 on this job.'));

      // And the guard refuses it. Both directions, because they fail for different reasons: the
      // closed world does not recognise the sentence, and the taint check recognises the number.
      expect(
        () => expectOnlyRecordedStrings(
          perceived,
          surface: 'The open job screen showing a refusal',
          copy: _openJobCopy,
          typedByAParty: _whatTheCustomerWrote,
          computed: _openJobShapes,
        ),
        throwsA(isA<TestFailure>()),
        reason: 'a platform message disclosing a budget must not pass a provider screen’s guard',
      );

      expect(
        () => expectNoTraceOfCents(perceived, cents: _theCustomersMaximum),
        throwsA(isA<TestFailure>()),
      );
    });
  });

  group('the provider feed', () {
    testWidgets('renders nothing this file does not record', (tester) async {
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _jobs(<OpenJob>[
            anOpenJob(id: _sofa),
            anOpenJob(id: _pallet, status: JobStatus.negotiating),
          ]),
        ];

      await _signInAsProvider(tester, feed: feed);

      final perceived = await perceivable(tester);

      expect(perceived, contains('Work you can bid on'));
      expect(perceived, contains('Review and bid'));

      expectOnlyRecordedStrings(
        perceived,
        surface: 'The provider job feed',
        copy: _feedCopy,
        typedByAParty: _whatTheCustomerWrote,
        computed: _feedShapes,
      );
    });

    testWidgets('and a payload carrying a budget leaves no trace of it', (tester) async {
      // The taint direction, over the **whole** perceivable surface rather than over `Text` finders.
      // Decoded through the real `OpenJob.fromJson`, so what is exercised is the path from bytes to
      // pixels — and now also to what a screen reader would be handed.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _jobs(<OpenJob>[
            OpenJob.fromJson(<String, dynamic>{
              'id': _sofa,
              'status': 'open',
              'pickup': <String, dynamic>{'suburb': 'Newtown', 'state': 'NSW', 'postcode': '2042'},
              'goods_description': 'Three-seat sofa, wrapped',
              'budget_cents': _theCustomersMaximum,
              'max_price': _theCustomersMaximum,
              'customer_maximum_cents': _theCustomersMaximum,
              'willing_to_pay': _theCustomersMaximum,
            }),
          ]),
        ];

      await _signInAsProvider(tester, feed: feed);

      final perceived = await perceivable(tester);

      expect(perceived, contains('Newtown NSW 2042'));
      expectNoTraceOfCents(perceived, cents: _theCustomersMaximum, surface: 'the provider feed');
    });
  });

  group('the provider’s own offers', () {
    testWidgets('render nothing this file does not record', (tester) async {
      final bidding = FakeBiddingRepository()
        ..pages = [
          _offers(<Bid>[
            aBid(id: 'b1', jobId: _sofa, amountCents: _theirOwnPrice),
            aBid(
              id: 'b2',
              jobId: _pallet,
              amountCents: _theirOwnPrice,
              status: BidStatus.accepted,
              message: 'Tail lift required.',
            ),
          ]),
        ];

      await _openMyBids(tester, feed: FakeOpenJobsRepository(), bidding: bidding);

      final perceived = await perceivable(tester);

      expect(perceived, contains('Your bids'));
      expect(perceived, contains(r'$450.00'));

      expectOnlyRecordedStrings(
        perceived,
        surface: 'The provider’s own offers',
        copy: _myBidsCopy,
        typedByAParty: _whatTheProviderWrote,
        amounts: const <int>{_theirOwnPrice},
        computed: _myBidsShapes,
      );
    });

    testWidgets('and a payload carrying a budget leaves no trace of it', (tester) async {
      final bidding = FakeBiddingRepository()
        ..pages = [
          _offers(<Bid>[
            Bid.fromJson(<String, dynamic>{
              'id': 'b1',
              'job_id': _sofa,
              'status': 'submitted',
              'offered_by': 'provider',
              'amount_cents': _theirOwnPrice,
              'created_at': '2026-08-13T04:15:30.000Z',
              'budget_cents': _theCustomersMaximum,
              'max_price': _theCustomersMaximum,
              'customer_maximum_cents': _theCustomersMaximum,
              'willing_to_pay': _theCustomersMaximum,
            }),
          ]),
        ];

      await _openMyBids(tester, feed: FakeOpenJobsRepository(), bidding: bidding);

      final perceived = await perceivable(tester);

      expect(perceived, contains(r'$450.00'));
      expectNoTraceOfCents(perceived, cents: _theCustomersMaximum, surface: 'the provider’s bids');
    });
  });

  group('every recorded string is still copy these screens carry', () {
    // **The staleness direction.** An allow-list that outlives its copy quietly permits a sentence
    // somebody could reintroduce under a wording that was retired for a reason.
    test('the open job and its bid panel', () {
      expectEveryRecordedStringStillExists(
        _openJobCopy,
        surface: 'the open job screen',
        sources: <String>[
          'lib/features/jobs/open_job_screen.dart',
          'lib/features/bidding/place_bid_panel.dart',
          'lib/features/bidding/instant_field.dart',
        ],
      );
    });

    test('the feed', () {
      expectEveryRecordedStringStillExists(
        _feedCopy,
        surface: 'the provider job feed',
        sources: <String>[
          'lib/features/jobs/provider_job_feed.dart',
          'lib/features/jobs/job_status.gen.dart',
          'lib/core/routing/signed_in_shell.dart',
        ],
      );
    });

    test('the provider’s own offers', () {
      expectEveryRecordedStringStillExists(
        _myBidsCopy,
        surface: 'the provider’s bids',
        sources: <String>[
          'lib/features/bidding/my_bids_screen.dart',
          'lib/features/bidding/bid_status.gen.dart',
        ],
      );
    });
  });
}

/// Fills the offer form and sends it, through the affordances the screen actually has.
Future<void> _placeAnOffer(WidgetTester tester) async {
  await tester.enterText(find.byKey(const Key('bid-amount')), '450');
  await tester.pump();

  for (final field in <String>['bid-pickup', 'bid-deliver-by']) {
    await tester.tap(find.byKey(Key(field)));
    await tester.pumpAndSettle();
    // The calendar, then the clock.
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
  }

  await tester.tap(find.byKey(const Key('bid-submit')));
  await tester.pumpAndSettle();
}

/// What the **customer** wrote about their own job, as the fixtures write it.
///
/// Exactly these values and no others. A customer who names their own limit in their own words has
/// disclosed it themselves, which is why this carve-out exists at all — and admitting arbitrary
/// strings under that heading would hand the guard back its hole.
const _whatTheCustomerWrote = <String>{
  'Three-seat sofa, wrapped',
  'Van or larger, two people to lift',
  'Second floor, no lift',
  'Newtown NSW 2042',
  'Geelong VIC 3220',
};

/// What the **provider** wrote on their own offer.
const _whatTheProviderWrote = <String>{'Tail lift required.'};

/// Every sentence, label and word the open job screen and its bid panel may put in front of a
/// provider.
///
/// **Hand-written, and that is the whole mechanism.** Deriving this from the source would admit
/// whatever the source said, which is the property being guarded against — the point is that a new
/// provider-visible string fails until a person decides it may be shown.
const _openJobCopy = <String>{
  // `open_job_screen.dart`.
  'Job',
  'Collect from',
  'Deliver to',
  'Not stated yet',
  'Shipper gives you the suburb and the state while you are bidding. The exact address goes to '
      'whoever is awarded the job.',
  'What is being moved',
  'The customer has not described the goods yet. Ask before you price it, or price the trip.',
  'Weight',
  'Size',
  'The customer says it needs',
  'Handling',
  'When the customer wants it',
  'The customer has not named a window. Offer the timing that suits you.',
  'Collection',
  'Delivery',
  'Bidding',
  'No offers yet.',
  'Offers have already been made. You can still bid.',
  'This job is not one you can bid on',
  'It may have been awarded or withdrawn, or it may be outside your service area or what your '
      'vehicles can carry. Your feed shows the work you are eligible for.',
  'Try again',

  // `place_bid_panel.dart` — the offer form.
  'Offer to carry this job',
  // The policy sentence, and the reason a shared ban-list cannot be the instrument here. It names
  // no number and no job: the same words on a payload with four budget-shaped keys in it and on one
  // with none.
  'Shipper never shows you what the customer is willing to pay. Price the job on what it is worth '
      'to you.',
  'Your price',
  r'$',
  'Australian dollars, for the whole job.',
  'Your commitment',
  'A time, not a window — when you will collect, and when it will be delivered.',
  'Collecting at',
  'Delivered by',
  'Say when you can collect.',
  'Say when it will be delivered.',
  'Conditions (optional)',
  'Access, timing caveats, what the price includes — in your own words.',
  'Send this offer',
  'Offers are sent straight away and are not held on your phone. If you have no signal, try again '
      'when you do.',

  // The panel once the platform has recorded the offer.
  'Your offer is with the customer',
  'Submitted — the customer can accept it, counter it, or let it expire.',
  'Revising or withdrawing an offer arrives with your bids list.',
  'Your offers are listed on your bids screen once it arrives.',

  // `instant_field.dart`.
  'Not chosen yet',
  'Choose',
  'Change',
};

/// The shapes of the values the open job screen computes rather than writes.
///
/// **Anchored at both ends, every one of them**, and **no money shape anywhere** — the collector
/// refuses one, because a shape cannot tell the provider's own price from the customer's maximum.
final _openJobShapes = <RegExp>[
  // A weight, as `_kilograms` renders one.
  RegExp(r'^\d+(?:\.\d+)? kg$'),
  // Three dimensions, as the model joins them.
  RegExp(r'^\d+ × \d+ × \d+ cm$'),
  // A window, either end of which may be absent — `_window` in `open_job_screen.dart`.
  RegExp('^(?:from |by )?$_date(?: to $_date)?\$'),
  // When bidding closes, as `dayFirstDateTime` renders it.
  RegExp('^Bidding closes $_date, $_time\$'),
  // Collection and delivery on a placed offer.
  RegExp('^Collecting $_date, $_time\$'),
  RegExp('^Delivered by $_date, $_time\$'),
];

/// The signed-in shell, which wraps the feed and is **not** in the tree on a pushed screen.
///
/// Recorded separately because it is the one surface every role shares: the customer's
/// "Publish a delivery" button lives in the same widget and is correctly absent here, which is what
/// makes an entry appearing in this set a decision about both halves rather than about one.
const _shellCopy = <String>{
  'Shipper',
  'Connectivity',
  'Sign out',
  // SHIP-173's app-bar action, a tooltip rather than visible text — so it reaches a provider
  // through `Semantics.tooltip`, which is one of the channels this collector added. It is shell
  // chrome shared by both roles and carries nothing about a job, which is why it is recorded
  // here beside 'Sign out' rather than in any screen's own set.
  'Delete account',
};

/// Every string the provider feed may put in front of a provider.
const _feedCopy = <String>{
  ..._shellCopy,
  'Work you can bid on',
  'Shipper shows you the jobs your service area, your vehicles and your verification make you '
      'eligible for.',
  'Your vehicles',
  'Your bids',
  // SHIP-81c's entry point to `features/profile/`. Recorded here because it is a string the feed
  // puts in front of a provider, and it says nothing about any job — which is the question this
  // set exists to force somebody to answer about every one of them.
  'Your documents',
  'Picked up in',
  'Built from the jobs shown below.',
  'Bidding',
  'A job being negotiated already has bids on it.',
  'Show all',
  'Review and bid',
  'Pickup region not stated',
  'Drop-off not added yet',
  'Show more jobs',
  'Showing the most recently published jobs you can bid on.',
  'No work for you to bid on right now',
  'Jobs appear here when they are picked up in your service area, a vehicle you have in service '
      'can carry them, and your account is verified. Check back, or check that all three are in '
      'place.',
  'Nothing matches what you picked',
  'Show everything again',
  'Try again',

  // `job_status.gen.dart` — the exact names from Docs/02 §1, which is why they are copy rather
  // than a shape.
  'Open',
  'Negotiating',
};

/// The shapes of the values the feed computes rather than writes.
final _feedShapes = <RegExp>[
  // The measurements line, which joins a weight and a size.
  RegExp(r'^\d+(?:\.\d+)? kg(?:  ·  \d+ × \d+ × \d+ cm)?$'),
  RegExp(r'^\d+ × \d+ × \d+ cm$'),
  // The timing line, which joins up to three of its own parts.
  RegExp('^(?:Pickup |Drop-off )?(?:from |by )?$_date'
      '(?:  ·  (?:Pickup |Drop-off )?(?:from |by )?$_date)*'
      '(?:  ·  Bidding closes $_date)?\$'),
  RegExp('^Bidding closes $_date\$'),
  // A state abbreviation on a filter chip, built from the jobs that have been read.
  RegExp(r'^[A-Z]{2,3}$'),
  // "3 of 8 jobs read", when a narrowing is live.
  RegExp(r'^\d+ of \d+ jobs read$'),
  // "None of the 8 jobs …", on the narrowed-to-nothing state.
  RegExp(r'^None of the \d+ jobs .*$'),
];

/// Every string the provider's own bid list may put in front of them.
const _myBidsCopy = <String>{
  'Your bids',
  'Every offer in every negotiation you are in, newest first. A counter-offer from a customer is on '
      'this list too — it is the one waiting for an answer from you.',
  'Show',
  'Every status',
  'No price on this offer',
  'Collect',
  'Deliver by',
  'Not stated',
  'A counter-offer has answered this one. It can be read but not acted on.',
  'Offered',
  'View the job',
  'Message and counter',
  'You have not bid on anything yet',
  'Offers you make appear here, grouped by what has become of them.',
  'Show every status',
  'Grouped from the offers read so far. There are more to read.',
  'Showing the most recent of these. There are more to read.',
  'Show more offers',
  'Try again',

  // Who made the offer. A customer's counter-offer is on this list, which is the thing about it a
  // provider would otherwise misread as a price they never quoted.
  'Yours',
  'From the customer',

  // `bid_status.gen.dart` — the exact names from Docs/02 §4, which is why they are copy rather than
  // a shape. All of them, because any status can arrive on a page and the alternative is a guard
  // that fails on a fixture rather than on a disclosure.
  'Draft',
  'Submitted',
  'Countered',
  'Accepted',
  'Rejected',
  'Withdrawn',
  'Expired',
  'Superseded',
  'Not shown by this version',
};

/// The shapes of the values the bid list computes rather than writes.
final _myBidsShapes = <RegExp>[
  // The two instants an offer commits to, beside their labels.
  RegExp('^$_date, $_time\$'),
  // When the offer was made.
  RegExp('^Offered $_date\$'),
  // The per-group count, which is a count and not an amount — one or two digits, and
  // `readsAsAnAmount` draws the line at three.
  RegExp(r'^\d{1,2}$'),
  // The empty-group sentence, which interpolates a status label in lower case.
  RegExp(r'^Nothing of yours is [a-z ]+$'),
];

/// A day-first date as `dayFirstDate` renders one — `20 Aug 2026`.
///
/// Written as a **shape rather than a value**, because the alternative is building the expectation
/// out of the formatter the screen uses, and a `want` computed from the subject agrees with a
/// mutation by construction.
const _date = r'\d{1,2} [A-Z][a-z]{2} \d{4}';

/// The time half of `dayFirstDateTime` — `4:30 am`.
const _time = r'\d{1,2}:\d{2} [ap]m';
