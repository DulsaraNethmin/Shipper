// SHIP-173 — "Deletion is initiated in-app with clear consequences and confirmation."
//
// Driven through the real app, like `award_test.dart` and by the same walk: sign in, find the
// affordance on the shell every signed-in person lands on, and tap it. **A screen reachable only by
// pumping it is indistinguishable from a button that does nothing** — and this route is the third
// entry in `_signedInLocations`, where a missing line sends the app silently to the home shell.
// SHIP-102's lane lost a run to exactly that.
//
// # The three clauses, and what would make each of them false while looking true
//
// **Initiated in-app.** The request has to reach `POST /v1/account/deletion` with an idempotency
// key. Asserted on what the repository was asked, because a screen that changed colour and sent
// nothing looks identical.
//
// **With clear consequences.** `Docs/05` §3.1's split is two facts, and the second — that the jobs
// and bids *stay*, under a pseudonym — is the one nobody expects and the one a warning that said
// only "this cannot be undone" would omit.
//
// **And confirmation.** Two halves, and the second is the one worth having: tapping the button must
// **not** send anything, and the dialog must be dismissable with nothing sent. A confirmation that
// is only tested in the accepting direction is a confirmation nobody has checked is a confirmation
// — it would pass with a dialog that deleted on open.
//
// # And the state SHIP-170 added
//
// A deferred request is a `202` like any other, and a screen that ignored `state` would tell
// somebody mid-delivery that their account goes on the fifteenth. The deferred rendering is asserted
// against the platform's own sentence, and the lift is asserted as a *second call* — because asking
// again is the only thing that drains the queue.

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';

import '../../core/auth/session_fixtures.dart';
import 'fake_identity_repository.dart';
import 'signup_app.dart';

/// Signs somebody in and walks them to the deletion screen the way a person reaches it.
///
/// Through the app bar rather than by navigating to the route, for the reason at the top of this
/// file. [role] is a parameter because both halves of the marketplace can delete their account and
/// the affordance must not be a customer's alone.
Future<void> openAccountDeletion(
  WidgetTester tester,
  FakeIdentityRepository identity, {
  UserRole role = UserRole.customer,
}) async {
  identity.tokens = aTokenPair(role: role);

  await tester.pumpWidget(signupApp(identity));
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();

  // The shell is where the affordance lives. Asserting it is on screen *before* tapping is what
  // separates "the button is missing" from "the tap did nothing" — and it is the vacuous-fixture
  // check wave 11 recorded, applied to a walk rather than to a list.
  expect(find.byKey(const Key('shell-signed-in')), findsOneWidget);
  expect(find.byKey(const Key('delete-account')), findsOneWidget);

  await tester.tap(find.byKey(const Key('delete-account')));
  await tester.pumpAndSettle();
}

/// Every `POST /v1/account/deletion` the repository was asked to make.
List<String> deletionKeys(FakeIdentityRepository identity) =>
    identity.keysFor('account-deletion');

void main() {
  group('a person deletes their account', () {
    testWidgets('the deletion screen is reachable from the signed-in shell', (tester) async {
      final identity = FakeIdentityRepository();
      await openAccountDeletion(tester, identity);

      // The guard's failure mode is a *silent redirect to the home shell*, so both halves are
      // asserted: this screen is here, and the shell is not.
      expect(find.byKey(const Key('account-deletion')), findsOneWidget);
      expect(find.byKey(const Key('shell-signed-in')), findsNothing);
      expect(deletionKeys(identity), isEmpty, reason: 'opening the screen must not delete anything');
    });

    testWidgets('a provider reaches it too', (tester) async {
      // Docs/05 §3.1 defers a *party* to a delivery, and the endpoint is RequireUser with no role
      // predicate. An affordance only the customer half could see would make the platform's rule
      // unreachable for the other half.
      final identity = FakeIdentityRepository();
      await openAccountDeletion(tester, identity, role: UserRole.provider);

      expect(find.byKey(const Key('account-deletion')), findsOneWidget);
    });

    testWidgets('it says what is lost and what is kept before anything is tapped', (tester) async {
      await openAccountDeletion(tester, FakeIdentityRepository());

      final removed = tester.widget<Text>(find.byKey(const Key('account-deletion-removed'))).data!;
      expect(removed, contains('deleted for good'));
      expect(removed, contains('documents'));
      expect(removed, contains('messages'));

      // The half nobody expects, and the half a "this cannot be undone" warning would omit.
      final retained = tester.widget<Text>(find.byKey(const Key('account-deletion-retained'))).data!;
      expect(retained, contains('pseudonym'));
      expect(retained, contains('stay'));

      // Docs/05 §3.1's thirty days, and the deferral, said before anybody commits to anything.
      final window = tester.widget<Text>(find.byKey(const Key('account-deletion-window'))).data!;
      expect(window, contains('30 days'));
      expect(window, contains('delivery'));
    });

    testWidgets('tapping the button does not delete anything', (tester) async {
      // **The half of "confirmation" that is easy to leave untested.** A dialog that sent the
      // request when it opened would pass every assertion about the accepting path.
      final identity = FakeIdentityRepository();
      await openAccountDeletion(tester, identity);

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('account-deletion-confirm')), findsOneWidget);
      expect(
        deletionKeys(identity),
        isEmpty,
        reason: 'opening the confirmation must not request deletion',
      );
    });

    testWidgets('the confirmation names the consequence and the window', (tester) async {
      // A dialog reading "Are you sure?" confirms that somebody tapped something, not that they
      // meant this — and this is the one act in the app nobody can undo from either side.
      await openAccountDeletion(tester, FakeIdentityRepository());

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();

      final consequence = tester
          .widget<Text>(find.byKey(const Key('account-deletion-confirm-consequence')))
          .data!;
      expect(consequence, contains('cannot be undone'));
      expect(consequence, contains('deleted for good'));

      final window =
          tester.widget<Text>(find.byKey(const Key('account-deletion-confirm-window'))).data!;
      expect(window, contains('30 days'));
    });

    testWidgets('dismissing it sends nothing', (tester) async {
      final identity = FakeIdentityRepository();
      await openAccountDeletion(tester, identity);

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-confirm-cancel')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('account-deletion-confirm')), findsNothing);
      expect(
        deletionKeys(identity),
        isEmpty,
        reason: 'the confirmation was dismissed and nothing may have been sent',
      );
      // And the screen is still usable rather than left in a half-committed state.
      expect(find.byKey(const Key('account-deletion-start')), findsOneWidget);
    });

    testWidgets('accepting it requests deletion and shows the date', (tester) async {
      final identity = FakeIdentityRepository()
        ..deletionRequest = aDeletionRequest(completesBy: '2026-09-15T09:30:00.000Z');
      await openAccountDeletion(tester, identity);

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-confirm-accept')));
      await tester.pumpAndSettle();

      // What the repository was asked, which is the clause. One call, carrying a key.
      expect(deletionKeys(identity), hasLength(1));
      expect(deletionKeys(identity).single, isNotEmpty);

      expect(
        tester.widget<Text>(find.byKey(const Key('account-deletion-heading'))).data,
        'Deletion requested',
      );
      // Day-first, which is what an Australian customer reads (CLAUDE.md). The fixture is
      // September so a month-first rendering would read as a different, plausible date.
      expect(
        tester.widget<Text>(find.byKey(const Key('account-deletion-promised'))).data,
        contains('15 Sep 2026'),
      );
    });
  });

  group('a request made during a delivery', () {
    testWidgets('is shown as waiting, in the platform’s own words', (tester) async {
      final identity = FakeIdentityRepository()..deletionRequest = aDeferredDeletionRequest();
      await openAccountDeletion(tester, identity);

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-confirm-accept')));
      await tester.pumpAndSettle();

      expect(deletionKeys(identity), hasLength(1));

      expect(
        tester.widget<Text>(find.byKey(const Key('account-deletion-heading'))).data,
        'Deletion is waiting',
      );

      // Rendered as given. The platform holds this sentence so it can be corrected without a
      // store release, and a client that reworded it would ship its own version of a legal one.
      expect(
        tester.widget<Text>(find.byKey(const Key('account-deletion-deferred'))).data,
        aDeferredDeletionRequest().deferralReason,
      );

      // **The date must not be presented as a promise**, which is the failure this whole state
      // exists to prevent: a deferred request's thirty days have not started.
      expect(find.byKey(const Key('account-deletion-promised')), findsNothing);
      expect(
        tester.widget<Text>(find.byKey(const Key('account-deletion-earliest'))).data,
        contains('At the earliest'),
      );
    });

    testWidgets('names no delivery, because it does not need one', (tester) async {
      // Both halves of the marketplace reach this screen. Rendering the job being waited on would
      // put job data in front of a provider, which is where Docs/01 §4.3 lives — and there is
      // nothing a person could do with it that opening their own deliveries does not do better.
      final identity = FakeIdentityRepository()..deletionRequest = aDeferredDeletionRequest();
      await openAccountDeletion(tester, identity);

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-confirm-accept')));
      await tester.pumpAndSettle();

      // Asserted against what is actually on screen rather than against a widget tree walk: the
      // deferred sentence is present, so this is a real negative rather than one that would pass
      // against a blank screen.
      expect(find.byKey(const Key('account-deletion-deferred')), findsOneWidget);
      expect(find.textContaining('\$'), findsNothing);
      expect(find.textContaining('Delivery #'), findsNothing);
    });

    testWidgets('lifts when the person checks again, on a second call', (tester) async {
      // "Queues until the job closes" is a claim no single call can demonstrate. The queue is
      // drained by asking again, so the second answer is the ticket.
      final identity = FakeIdentityRepository()
        ..deletionAnswers.addAll([
          aDeferredDeletionRequest(),
          aDeletionRequest(completesBy: '2026-10-02T09:30:00.000Z'),
        ]);
      await openAccountDeletion(tester, identity);

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-confirm-accept')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('account-deletion-deferred')), findsOneWidget);

      // The button stays, and it does **not** ask for confirmation again: checking on a request
      // that already exists cannot delete anything twice, and a second dialog would train
      // somebody to dismiss the one that matters.
      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();

      expect(deletionKeys(identity), hasLength(2));
      expect(find.byKey(const Key('account-deletion-deferred')), findsNothing);
      expect(
        tester.widget<Text>(find.byKey(const Key('account-deletion-promised'))).data,
        contains('2 Oct 2026'),
      );
    });
  });

  group('when the platform refuses', () {
    testWidgets('the screen says so and nothing claims to have been recorded', (tester) async {
      final identity = FakeIdentityRepository()
        ..failures['account-deletion'] = const ApiErrorResponse(
          statusCode: 503,
          code: 'unavailable',
          message: 'the database is unreachable',
          requestId: 'req-deletion-1',
        );
      await openAccountDeletion(tester, identity);

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-confirm-accept')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('failure-banner')), findsOneWidget);
      expect(
        tester.widget<Text>(find.byKey(const Key('account-deletion-refusal'))).data,
        contains('Try again'),
      );

      // **The consequences are still what is on screen**, not a completion date. A refusal that
      // rendered as a recorded request would tell somebody their account was going when it is not.
      expect(find.byKey(const Key('account-deletion-heading')), findsNothing);
      expect(find.byKey(const Key('account-deletion-removed')), findsOneWidget);
    });

    testWidgets('and a second attempt reuses the key, because the outcome is unknown',
        (tester) async {
      // Docs/07 §4: a key is held when the previous attempt failed without saying whether the
      // platform acted. A 503 is exactly that — and minting a fresh one would risk a second
      // request if the first had in fact been recorded.
      final identity = FakeIdentityRepository()
        ..failures['account-deletion'] = const ApiUnreachable();
      await openAccountDeletion(tester, identity);

      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-confirm-accept')));
      await tester.pumpAndSettle();

      await tester.tap(find.byKey(const Key('account-deletion-dismiss')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-start')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('account-deletion-confirm-accept')));
      await tester.pumpAndSettle();

      final keys = deletionKeys(identity);
      expect(keys, hasLength(2));
      expect(keys.first, keys.last,
          reason: 'a retry of an attempt whose outcome is unknown carries the same key');
    });
  });
}
