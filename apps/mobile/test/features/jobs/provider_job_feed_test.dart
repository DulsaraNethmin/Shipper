// SHIP-99 — "Provider sees eligible open jobs with filters".
//
// Driven through the real app: the session, the router, the shell and the half the role selects.
// Only the token store and the repositories are substituted, so a guard or a shell that stopped
// showing a provider their own feed fails here rather than only on a device.
//
// # Three things this file is careful about, each of which is a way the screen could be quietly
// wrong
//
// **The second page.** Following a cursor is a separate code path a provider only reaches by
// scrolling, and it is where an appended list becomes a replaced one, a cursor gets re-encoded, or
// a filter's options stop growing. Every paging test below reads two pages.
//
// **The empty feed.** A provider eligible for nothing gets `200` with `[]`, which covers an
// unverified account, no declared service area, no vehicle in service, and genuinely no work. None
// of them is an error, and the case never occurs in development because whoever is building this
// has jobs.
//
// **The budget.** Docs/01 §4.3 keeps the customer's maximum away from a provider in every form.
// The last group renders a payload carrying one and asserts that nothing of it reaches the screen.

import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';

import '../../core/auth/session_fixtures.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_jobs_repository.dart';
import 'fake_open_jobs_repository.dart';

const _sofa = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0';
const _pallet = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1';
const _drums = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e2';

/// Signs a provider in and lands on their feed, which **is** the provider half of the shell.
Future<void> openProviderShell(WidgetTester tester, FakeOpenJobsRepository feed) async {
  // A phone-shaped surface rather than the 800×600 default, so a screen that scrolls is not
  // reported as overflowing.
  tester.view.physicalSize = const Size(800, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.provider);

  await tester.pumpWidget(signupApp(identity, openJobs: feed));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();
}

ApiPage<OpenJob> _page(List<OpenJob> jobs, {String? next, bool more = false}) =>
    ApiPage<OpenJob>(data: jobs, nextCursor: next, hasMore: more);

OpenJob _at(String state, {required String id, JobStatus status = JobStatus.open}) {
  return anOpenJob(id: id, status: status, pickup: JobRegion(state: state, suburb: 'Somewhere'));
}

Finder _inFeed(Finder matching) =>
    find.descendant(of: find.byKey(const Key('shell-provider')), matching: matching);

void main() {
  group('getting there', () {
    testWidgets('a provider lands on the feed, and it is read once', (tester) async {
      final feed = FakeOpenJobsRepository()..pages = [_page(<OpenJob>[anOpenJob(id: _sofa)])];

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('shell-provider')), findsOneWidget);
      expect(feed.reads, 1);
      // The first page names no position. A cursor sent on the first request would be a client
      // inventing one, and the platform refuses a cursor it did not issue.
      expect(feed.cursors, <String?>[null]);
    });

    testWidgets('a customer never reads the provider feed', (tester) async {
      // Docs/07 §1: a customer should never see provider surfaces or the reverse. `GET
      // /v1/jobs/open` does not check the caller's role — it answers a customer `200` with an
      // empty page — so "no request is made on their behalf" is the assertion that matters, not
      // "the platform refused".
      final feed = FakeOpenJobsRepository();
      final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.customer);

      await tester.pumpWidget(signupApp(identity, openJobs: feed, jobs: FakeJobsRepository()));
      await tester.pumpAndSettle();
      await signInThrough(tester);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('shell-customer')), findsOneWidget);
      expect(find.byKey(const Key('shell-provider')), findsNothing);
      expect(feed.calls, isEmpty);
    });

    testWidgets('the fleet is still reachable from the provider half', (tester) async {
      // SHIP-98 hung the fleet off this half and the feed replaced the placeholder around it. The
      // affordance keeps its key, because "a provider can reach their vehicles from the shell" is
      // the same fact whatever the half is made of — and it is the answer to an empty feed.
      final feed = FakeOpenJobsRepository();

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('manage-vehicles')), findsOneWidget);
    });
  });

  group('what a provider sees', () {
    testWidgets('the jobs they are eligible for, at the grain the platform gives them',
        (tester) async {
      final feed = FakeOpenJobsRepository()..pages = [_page(<OpenJob>[anOpenJob(id: _sofa)])];

      await openProviderShell(tester, feed);

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
      expect(_inFeed(find.textContaining('Newtown NSW 2042')), findsOneWidget);
      expect(_inFeed(find.textContaining('Geelong VIC 3220')), findsOneWidget);
      expect(_inFeed(find.text('Three-seat sofa, wrapped')), findsOneWidget);
      expect(_inFeed(find.textContaining('190 × 90 × 85 cm')), findsOneWidget);
    });

    testWidgets('no street line, because the platform sends none', (tester) async {
      // SHIP-83 confirmed the decision and widened it to the coordinate: a provider prices on the
      // locality and the doorstep is needed after an award. A client that drew a placeholder for an
      // address would be promising something the response cannot carry.
      final feed = FakeOpenJobsRepository()..pages = [_page(<OpenJob>[anOpenJob(id: _sofa)])];

      await openProviderShell(tester, feed);

      expect(_inFeed(find.textContaining('Street')), findsNothing);
      expect(_inFeed(find.textContaining('Address')), findsNothing);
    });

    testWidgets('a job with two regions and nothing else is drawn, not skipped', (tester) async {
      // The wizard collects the addresses first, so a job can be open to bids before its owner has
      // described the goods. A card that needed a description would hide real work.
      final feed = FakeOpenJobsRepository()..pages = [_page(<OpenJob>[aBareOpenJob(id: _pallet)])];

      await openProviderShell(tester, feed);

      expect(_inFeed(find.byKey(const Key('open-job-$_pallet'))), findsOneWidget);
      expect(_inFeed(find.textContaining('Darwin NT 0800')), findsOneWidget);
      expect(_inFeed(find.text('Drop-off not added yet')), findsOneWidget);
    });

    testWidgets('a job already being negotiated says so', (tester) async {
      // Docs/02 §1 keeps a negotiating job open to eligible bids, so this is worth knowing before
      // pricing it rather than a reason to hide the job.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[anOpenJob(id: _sofa, status: JobStatus.negotiating)])
        ];

      await openProviderShell(tester, feed);

      expect(_inFeed(find.byKey(const Key('open-job-status-negotiating'))), findsOneWidget);
      expect(_inFeed(find.text('Negotiating')), findsWidgets);
    });

    testWidgets('dates are day-first with the month spelled', (tester) async {
      // CLAUDE.md fixes the convention, and 03/04 is a different day to two different readers.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[anOpenJob(id: _sofa, expiresAt: '2026-08-25T03:30:00.000Z')])
        ];

      await openProviderShell(tester, feed);

      expect(_inFeed(find.textContaining('Bidding closes 25 Aug 2026')), findsOneWidget);
    });
  });

  group('the three states a list has', () {
    testWidgets('a spinner while the first page is in flight', (tester) async {
      final held = Completer<void>();
      final feed = FakeOpenJobsRepository()..gate = held;

      tester.view.physicalSize = const Size(800, 2400);
      tester.view.devicePixelRatio = 1;
      addTearDown(tester.view.reset);

      final identity = FakeIdentityRepository()..tokens = aTokenPair(role: UserRole.provider);
      await tester.pumpWidget(signupApp(identity, openJobs: feed));
      await tester.pumpAndSettle();

      // Signed in by hand rather than through `signInThrough`, and pumped rather than settled: a
      // spinner animates for ever, so `pumpAndSettle` while one is on screen waits for something
      // that never happens. **The feed is read during the sign-in itself** — it is the provider
      // half of the shell, so there is no later tap to hang this on the way the fleet has.
      await tester.enterText(find.byKey(const Key('sign-in-email')), 'alice@example.com');
      await tester.enterText(
        find.byKey(const Key('sign-in-password')),
        'correct-horse-battery-staple',
      );
      await tester.pump();
      await tester.tap(find.byKey(const Key('sign-in')));
      await tester.pump();
      await tester.pump(const Duration(milliseconds: 400));

      expect(find.byKey(const Key('open-jobs-loading')), findsOneWidget);
      expect(find.byKey(const Key('open-jobs-empty')), findsNothing);

      held.complete();
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('open-jobs-loading')), findsNothing);
      expect(find.byKey(const Key('open-jobs-empty')), findsOneWidget);
    });

    testWidgets('an empty feed is an empty state, never a spinner and never an error',
        (tester) async {
      // The platform answers four ordinary situations with `[]` — unverified, no service area, no
      // vehicle in service, no work — and will not say which. A client that treated an empty list
      // as "still loading" would leave every new provider watching a spinner for ever.
      final feed = FakeOpenJobsRepository()..pages = [_page(<OpenJob>[])];

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('open-jobs-empty')), findsOneWidget);
      expect(find.byKey(const Key('open-jobs-loading')), findsNothing);
      expect(find.byKey(const Key('open-jobs-failed')), findsNothing);
      expect(find.byKey(const Key('failure-banner')), findsNothing);
    });

    testWidgets('the empty state names what a provider can do about it', (tester) async {
      // The platform will not say which of the four it is, so the screen lists them rather than
      // pretending to know — and the fleet, which is the one a provider can fix from here, is on
      // the screen already.
      final feed = FakeOpenJobsRepository()..pages = [_page(<OpenJob>[])];

      await openProviderShell(tester, feed);

      final inEmptyState = find.descendant(
        of: find.byKey(const Key('open-jobs-empty')),
        matching: find.textContaining('service area'),
      );

      expect(inEmptyState, findsOneWidget);
      expect(
        find.descendant(
          of: find.byKey(const Key('open-jobs-empty')),
          matching: find.textContaining('verified'),
        ),
        findsOneWidget,
      );
      expect(find.byKey(const Key('manage-vehicles')), findsOneWidget);
    });

    testWidgets('a failure with nothing on screen offers a retry', (tester) async {
      final feed = FakeOpenJobsRepository()..failure = const ApiUnreachable();

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('open-jobs-failed')), findsOneWidget);
      expect(find.byKey(const Key('open-jobs-empty')), findsNothing);

      feed.failure = null;
      feed.pages = [_page(<OpenJob>[anOpenJob(id: _sofa)])];

      await tester.tap(find.byKey(const Key('open-jobs-retry')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
    });

    testWidgets('a failure with a feed on screen is a banner above it, not a replacement',
        (tester) async {
      // Somebody who pulled to refresh at a loading dock with no signal should still be looking at
      // the work they had.
      final feed = FakeOpenJobsRepository()..pages = [_page(<OpenJob>[anOpenJob(id: _sofa)])];

      await openProviderShell(tester, feed);

      feed.failure = const ApiUnreachable();
      await tester.fling(find.byKey(const Key('shell-provider')), const Offset(0, 600), 1000);
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
    });
  });

  group('the second page, which a provider only reaches by asking', () {
    testWidgets('follows the cursor back exactly as it arrived', (tester) async {
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[anOpenJob(id: _sofa)], next: 'cursor-2', more: true),
          _page(<OpenJob>[anOpenJob(id: _pallet)]),
        ];

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('open-jobs-more')), findsOneWidget);

      await tester.tap(find.byKey(const Key('open-jobs-more')));
      await tester.pumpAndSettle();

      expect(feed.cursors, <String?>[null, 'cursor-2']);
    });

    testWidgets('appends rather than replaces', (tester) async {
      // The mistake this catches is a `_load` that assigned the page instead of concatenating it:
      // the screen looks right on page one and silently loses everything above page two.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[anOpenJob(id: _sofa)], next: 'cursor-2', more: true),
          _page(<OpenJob>[anOpenJob(id: _pallet)]),
        ];

      await openProviderShell(tester, feed);
      await tester.tap(find.byKey(const Key('open-jobs-more')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
      expect(_inFeed(find.byKey(const Key('open-job-$_pallet'))), findsOneWidget);
    });

    testWidgets('the last page stops offering more', (tester) async {
      // `has_more` is carried rather than inferred, because an absent cursor is also what the first
      // request looks like.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[anOpenJob(id: _sofa)], next: 'cursor-2', more: true),
          _page(<OpenJob>[anOpenJob(id: _pallet)]),
        ];

      await openProviderShell(tester, feed);
      await tester.tap(find.byKey(const Key('open-jobs-more')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('open-jobs-more')), findsNothing);
    });

    testWidgets('says the list is partial while it is', (tester) async {
      // Both the cards and the filter chips are built from what has been read. A screen showing
      // four jobs and two states when there are nine and four has lied quietly.
      final feed = FakeOpenJobsRepository()
        ..pages = [_page(<OpenJob>[anOpenJob(id: _sofa)], next: 'cursor-2', more: true)];

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('open-jobs-partial')), findsOneWidget);
    });

    testWidgets('a refresh goes back to the first page and drops the second', (tester) async {
      // Pull-to-refresh asks the platform again from the start, so what is on screen afterwards is
      // one page of the current feed rather than one page of the current feed appended to a page of
      // the old one.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[anOpenJob(id: _sofa)], next: 'cursor-2', more: true),
          _page(<OpenJob>[anOpenJob(id: _pallet)]),
          _page(<OpenJob>[anOpenJob(id: _drums)]),
        ];

      await openProviderShell(tester, feed);
      await tester.tap(find.byKey(const Key('open-jobs-more')));
      await tester.pumpAndSettle();

      await tester.fling(find.byKey(const Key('shell-provider')), const Offset(0, 600), 1000);
      await tester.pumpAndSettle();

      expect(feed.cursors, <String?>[null, 'cursor-2', null]);
      expect(_inFeed(find.byKey(const Key('open-job-$_drums'))), findsOneWidget);
      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsNothing);
    });
  });

  group('the filters', () {
    testWidgets('narrow the feed to a pickup state', (tester) async {
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[_at('NSW', id: _sofa), _at('VIC', id: _pallet)])
        ];

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('open-jobs-filters')), findsOneWidget);

      await tester.tap(find.byKey(const Key('open-jobs-state-NSW')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
      expect(_inFeed(find.byKey(const Key('open-job-$_pallet'))), findsNothing);
    });

    testWidgets('narrow the feed to jobs nobody has bid on yet', (tester) async {
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[
            _at('NSW', id: _sofa),
            _at('NSW', id: _pallet, status: JobStatus.negotiating),
          ])
        ];

      await openProviderShell(tester, feed);

      await tester.tap(find.byKey(const Key('open-jobs-status-open')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
      expect(_inFeed(find.byKey(const Key('open-job-$_pallet'))), findsNothing);
    });

    testWidgets('never ask the platform anything — the endpoint accepts no filter',
        (tester) async {
      // The property that keeps this a convenience rather than a control that disagrees with
      // server-side eligibility. `GET /v1/jobs/open` takes `limit` and `cursor` and nothing else,
      // so a filter that produced a request would be producing one the platform refuses.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[_at('NSW', id: _sofa), _at('VIC', id: _pallet)])
        ];

      await openProviderShell(tester, feed);
      expect(feed.reads, 1);

      await tester.tap(find.byKey(const Key('open-jobs-state-NSW')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('open-jobs-state-VIC')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('open-jobs-state-NSW')));
      await tester.pumpAndSettle();

      expect(feed.reads, 1, reason: 'narrowing is done on the device and sends nothing');
    });

    testWidgets('the chips are built from the jobs, and the second page adds to them',
        (tester) async {
      // Nothing is compiled in: a list of the eight states here would be a vocabulary needing a
      // store release. What that means for a paged list is that a state appearing only on page two
      // has no chip until page two is read, which is exactly what this asserts.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(
            <OpenJob>[_at('NSW', id: _sofa), _at('VIC', id: _pallet)],
            next: 'cursor-2',
            more: true,
          ),
          _page(<OpenJob>[_at('QLD', id: _drums)]),
        ];

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('open-jobs-state-NSW')), findsOneWidget);
      expect(find.byKey(const Key('open-jobs-state-QLD')), findsNothing);

      await tester.tap(find.byKey(const Key('open-jobs-more')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('open-jobs-state-QLD')), findsOneWidget);
    });

    testWidgets('survive a further page, and apply to it', (tester) async {
      // A provider who narrowed to NSW and then read more asked to see more NSW work, not to have
      // their choice quietly discarded — and not to be shown the VIC job that arrived with it.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(
            <OpenJob>[_at('NSW', id: _sofa), _at('VIC', id: _pallet)],
            next: 'cursor-2',
            more: true,
          ),
          _page(<OpenJob>[_at('NSW', id: _drums), _at('VIC', id: 'later-vic')]),
        ];

      await openProviderShell(tester, feed);

      await tester.tap(find.byKey(const Key('open-jobs-state-NSW')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('open-jobs-more')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
      expect(_inFeed(find.byKey(const Key('open-job-$_drums'))), findsOneWidget);
      expect(_inFeed(find.byKey(const Key('open-job-$_pallet'))), findsNothing);
    });

    testWidgets('the two facets are an intersection on the screen as well as in the function',
        (tester) async {
      // A provider looking for NSW work nobody has bid on yet is asking one question with two
      // parts, not two questions. A screen that unioned them would show them the VIC job as well.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[
            _at('NSW', id: _sofa),
            _at('NSW', id: _pallet, status: JobStatus.negotiating),
            _at('VIC', id: _drums),
          ])
        ];

      await openProviderShell(tester, feed);

      await tester.tap(find.byKey(const Key('open-jobs-state-NSW')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('open-jobs-status-open')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
      expect(_inFeed(find.byKey(const Key('open-job-$_pallet'))), findsNothing);
      expect(_inFeed(find.byKey(const Key('open-job-$_drums'))), findsNothing);
    });

    testWidgets('a facet with one option is not drawn at all', (tester) async {
      // A single chip can only ever hide the whole feed, and a provider whose work is all in one
      // state should be shown work rather than a row of buttons.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[_at('NSW', id: _sofa), _at('NSW', id: _pallet)])
        ];

      await openProviderShell(tester, feed);

      expect(find.byKey(const Key('open-jobs-state-NSW')), findsNothing);
      expect(find.byKey(const Key('open-jobs-status-open')), findsNothing);
    });

    testWidgets('say how much of what was read is being shown, and offer to show all',
        (tester) async {
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[_at('NSW', id: _sofa), _at('VIC', id: _pallet), _at('VIC', id: _drums)])
        ];

      await openProviderShell(tester, feed);

      await tester.tap(find.byKey(const Key('open-jobs-state-VIC')));
      await tester.pumpAndSettle();

      // "of 3 jobs read" rather than "of 3 jobs": the second would be a claim about the
      // marketplace, and the list is paged.
      expect(find.text('2 of 3 jobs read'), findsOneWidget);

      await tester.tap(find.byKey(const Key('open-jobs-clear-filter')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
      expect(find.byKey(const Key('open-jobs-narrowed')), findsNothing);
    });
  });

  group('a filter that hides everything read so far', () {
    /// Two NSW jobs on page one, a VIC job on page two, and page one again on the third read.
    ///
    /// The third entry is what a **refresh** answers with, and it is what makes this scenario
    /// reachable at all: a provider narrows to a state that only page two carries, pulls to
    /// refresh, and is back to one page that has none of it. The filter is still theirs and still
    /// applies, so the screen has to say "none of what has been read matches" rather than "there
    /// is no work for you" — which would be false, and would send them to check a fleet that is
    /// perfectly fine.
    FakeOpenJobsRepository split() {
      List<OpenJob> firstPage() => <OpenJob>[
            _at('NSW', id: _sofa),
            _at('NSW', id: _pallet, status: JobStatus.negotiating),
          ];

      return FakeOpenJobsRepository()
        ..pages = [
          _page(firstPage(), next: 'cursor-2', more: true),
          _page(<OpenJob>[_at('VIC', id: _drums)]),
          _page(firstPage(), next: 'cursor-2', more: true),
        ];
    }

    /// Reads both pages, narrows to the state only page two carries, then refreshes back to one.
    Future<void> narrowPastTheEndOfWhatIsRead(WidgetTester tester, FakeOpenJobsRepository feed) async {
      await openProviderShell(tester, feed);

      await tester.tap(find.byKey(const Key('open-jobs-more')));
      await tester.pumpAndSettle();

      // The VIC chip exists only because page two was read — the options come from the jobs.
      await tester.tap(find.byKey(const Key('open-jobs-state-VIC')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_drums'))), findsOneWidget);

      await tester.fling(find.byKey(const Key('shell-provider')), const Offset(0, 600), 1000);
      await tester.pumpAndSettle();
    }

    testWidgets('says so, and does not claim there is no work', (tester) async {
      final feed = split();

      await narrowPastTheEndOfWhatIsRead(tester, feed);

      expect(find.byKey(const Key('open-jobs-none-match')), findsOneWidget);
      expect(find.byKey(const Key('open-jobs-empty')), findsNothing);
      expect(find.textContaining('more to read'), findsOneWidget);
      // And the next page is offered, because reading further is one of the two honest answers.
      expect(find.byKey(const Key('open-jobs-more')), findsOneWidget);
    });

    testWidgets('offers a way back to everything', (tester) async {
      final feed = split();

      await narrowPastTheEndOfWhatIsRead(tester, feed);

      await tester.tap(find.byKey(const Key('open-jobs-show-all')));
      await tester.pumpAndSettle();

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);
      expect(find.byKey(const Key('open-jobs-none-match')), findsNothing);
    });
  });

  group('the customer’s budget reaches nothing on this screen', () {
    testWidgets('a response carrying one renders none of it', (tester) async {
      // Docs/01 §4.3: not as an amount, not as a band, and not as a "budget supplied" flag. The
      // platform proves the field never leaves the service; this proves the screen has nowhere to
      // put one that arrived anyway. The payload is decoded through the real `OpenJob.fromJson`,
      // so this is the whole path from bytes to pixels.
      final feed = FakeOpenJobsRepository()
        ..pages = [
          _page(<OpenJob>[
            OpenJob.fromJson(<String, dynamic>{
              'id': _sofa,
              'status': 'open',
              'pickup': <String, dynamic>{'suburb': 'Newtown', 'state': 'NSW', 'postcode': '2042'},
              'goods_description': 'Three-seat sofa, wrapped',
              'budget_cents': 150000,
              'max_price': 150000,
              'created_at': '2026-08-11T03:30:00.000Z',
            }),
          ])
        ];

      await openProviderShell(tester, feed);

      expect(_inFeed(find.byKey(const Key('open-job-$_sofa'))), findsOneWidget);

      for (final forbidden in <String>['150000', '1500', '\$1,500', '\$1500', '1,500']) {
        expect(
          find.textContaining(forbidden),
          findsNothing,
          reason: 'a provider must never be shown the customer’s maximum (Docs/01 §4.3)',
        );
      }
    });

    testWidgets('and there is no affordance implying a budget exists', (tester) async {
      // The subtler half of the rule. "Budget on request", "no budget set", a price filter or a
      // sort by budget would each disclose that a maximum was or was not supplied, which §4.3
      // forbids as explicitly as the amount itself.
      final feed = FakeOpenJobsRepository()..pages = [_page(<OpenJob>[anOpenJob(id: _sofa)])];

      await openProviderShell(tester, feed);

      for (final forbidden in <String>['Budget', 'budget', 'Maximum', 'Price', 'price']) {
        expect(
          find.textContaining(forbidden),
          findsNothing,
          reason: 'nothing on a provider surface may imply a customer stated a maximum',
        );
      }
    });
  });
}
