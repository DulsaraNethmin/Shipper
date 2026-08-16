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
import 'package:flutter/rendering.dart';
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
/// a *model*; this holds what a person actually receives. A screen composing "the customer has set a
/// maximum" out of no field and no value passes every guard that looks at fields or values, and
/// fails only here.
///
/// This is the *open-world* half of that guard — a ban-list, fast to read and fast to fail. See
/// [renderedStrings] for the half that does not have to anticipate the wording.
Future<String> renderedText(WidgetTester tester) async =>
    (await renderedStrings(tester)).map((data) => data.toLowerCase()).join('\n');

/// Every string this screen puts in front of somebody, across all **three** surfaces it can use.
///
/// **A disclosure does not care which surface carries it, so a collector that reads one is a guard
/// against one.** Each surface here was added because the set before it was got past:
///
/// - **`Text`** — headings, labels, helper text, buttons, message bodies. What anybody looks at.
/// - **`EditableText`** — what a field is *seeded with*. Rendered just as visibly, and invisible to
///   every `Text` finder. The counter form seeds the price and the conditions from the offer being
///   answered, so omitting this leaves a populated, provider-visible box outside the guard.
/// - **The semantics tree** — what VoiceOver and TalkBack *say*. A `Text` carrying a
///   `semanticsLabel:` renders one string and announces a different one, so a screen-reader user can
///   be told something nobody looking at the screen can see. **That is worse than a visible
///   disclosure rather than better**, and it is the rung that got past the first two.
///
/// ## Why the semantics half is complete rather than sampled
///
/// It **walks** the `SemanticsNode` tree from its root through `visitChildren` — the tree the
/// platform accessibility bridge serialises and hands to the screen reader. What is collected is
/// therefore what is announced, by construction rather than by enumerating cases somebody thought
/// of.
///
/// `find.bySemanticsLabel` was the alternative and is the wrong instrument: it is a **matcher**,
/// taking a pattern and returning what matches. It can confirm a label somebody already suspects and
/// cannot enumerate the ones they do not — a ban-list wearing a different hat, and a ban-list is the
/// exact thing this collector exists because of.
///
/// All four announced properties are read rather than `label` alone: `value` carries what a text
/// field holds, and `hint` and `tooltip` are spoken too. A sentence in any of them is spoken all the
/// same.
///
/// **Merged nodes are split on newlines.** A `Card` merges its descendants into one node whose label
/// is theirs joined by `\n`, so the *fragments* are what get checked — otherwise one long
/// concatenation would match nothing recorded and would have to be waved through or listed whole.
///
/// ## It turns semantics on itself, and that is deliberate
///
/// A widget test builds **no semantics tree at all** unless a `SemanticsHandle` is held, so the
/// quiet failure available here is a collector that reads an empty tree, finds nothing to object to,
/// and reports success. "Collects some labels" is the shape that reads as a fix.
///
/// Leaving the handle to the caller was tried and is worse in two ways: a test that forgets it gets
/// exactly that silent pass, and `addTearDown` is **too late** to dispose one — the framework
/// verifies no handle is outstanding *before* tear-downs run, so every test in the file failed with
/// "A SemanticsHandle was active at the end of the test". Enabling, pumping and disposing inside one
/// call removes both failure modes: no caller can forget, and no handle outlives the collection.
///
/// The `pump` is not optional. `ensureSemantics` marks the tree as needing semantics; the tree is
/// built on the **next frame**, so collecting without one reads an empty tree.
///
/// Empty strings are dropped: an empty composer is not a sentence.
Future<List<String>> renderedStrings(WidgetTester tester) async {
  final handle = tester.ensureSemantics();
  try {
    await tester.pump();

    final texts = tester.widgetList<Text>(find.byType(Text)).map((text) => text.data ?? '');
    final editable =
        tester.widgetList<EditableText>(find.byType(EditableText)).map((f) => f.controller.text);

    return <String>[...texts, ...editable, ..._announced(tester)]
        .expand((data) => data.split('\n'))
        .map((data) => data.trim())
        .where((data) => data.isNotEmpty)
        .toList();
  } finally {
    handle.dispose();
  }
}

/// Every string the accessibility layer would speak, from the whole semantics tree.
List<String> _announced(WidgetTester tester) {
  final root = _semanticsRoot(tester);
  if (root == null) {
    throw StateError(
      'Semantics are enabled and the tree is empty. Something has changed about how it is built, '
      'and an empty collection here would silently pass every assertion resting on it.',
    );
  }

  final spoken = <String>[];
  void walk(SemanticsNode node) {
    final data = node.getSemanticsData();
    spoken.addAll(<String>[data.label, data.value, data.hint, data.tooltip]);
    node.visitChildren((child) {
      walk(child);
      return true;
    });
  }

  walk(root);
  return spoken;
}

/// The root of the semantics tree, found from the root pipeline owner.
///
/// `RendererBinding.pipelineOwner` is the one-liner and is **deprecated**, which this repository's
/// analyzer treats as fatal. The replacement is the owner *tree*: the root owner delegates the render
/// tree to a child, so the semantics owner is found by descending rather than by reading a property.
SemanticsNode? _semanticsRoot(WidgetTester tester) {
  SemanticsNode? found;

  void visit(PipelineOwner owner) {
    found ??= owner.semanticsOwner?.rootSemanticsNode;
    owner.visitChildren(visit);
  }

  visit(tester.binding.rootPipelineOwner);
  return found;
}

/// A phone-shaped surface, and a tall one: a negotiation is a chain, a conversation and two forms.
void _phone(WidgetTester tester) {
  tester.view.physicalSize = const Size(800, 2600);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);
}
