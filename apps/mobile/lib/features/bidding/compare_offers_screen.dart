import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/bidding/award_controller.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/compare_offers_controller.dart';
import 'package:shipper/features/bidding/received_offer.dart';
import 'package:shipper/shared/formatting/dates.dart';
import 'package:shipper/shared/formatting/money.dart';

/// The customer's bid comparison — `Docs/01` §4.3's "allow a customer to compare price, timing,
/// provider profile, vehicle, and declared capability" (SHIP-102).
///
/// ## Side by side, literally
///
/// The offers are cards in a **horizontally scrolling row**, each drawing the same four rows in the
/// same order: price, timing, provider, vehicle. That is what makes it a comparison rather than a
/// list — the eye runs across one row at a time, and a field that is missing on one offer leaves a
/// gap where the others have a value rather than shuffling everything up.
///
/// On a narrow phone one card and the edge of the next is what fits, which is deliberate: a card
/// that filled the screen exactly would give no cue that there is another one.
///
/// ## What this screen may not show, and the form that is easiest to get wrong
///
/// **The customer's own budget is not on it.** That reads oddly — it is their number and their
/// screen — and it is still the rule that matters here, because this screen is the mirror of the
/// one wave 9 found the defect on. The invariant `Docs/01` §4.3 states is that the budget must
/// never reach a **provider**, "not as an amount, not as a band, and not as a 'budget supplied'
/// indicator", and the third clause is the one that survives everything:
///
///     a sentence saying a maximum exists carries no field and no amount.
///
/// A screen reading "3 offers are within your budget" has no budget field in its state, no budget
/// value in its widget tree, and passes a closed key set and a source scan alike. Nothing here
/// draws such a sentence, and `compare_offers_test.dart` asserts on the **words** rendered rather
/// than only on the fields available — which is the guard a closed key set structurally cannot be.
///
/// Neither is the provider's service area or their specialties, which is SHIP-102a's *Done when*
/// naming them: the endpoint does not send them and this screen has nowhere to put them.
///
/// ## What "provider profile" amounts to today, said rather than implied
///
/// Two facts: whether the account has cleared verification, and how long it has been on the
/// platform. There is no trading name, no rating and no completed-job count — `internal/profiles`
/// holds no code yet, and the only provider profile that exists is the two fields this response may
/// not carry. The screen shows what there is and does not draw an empty frame implying more.
///
/// ## Nothing here decides what may be done
///
/// `Docs/07` §3: the app may hide or disable; the platform decides. [ReceivedOffer.isAwardable]
/// chooses whether to draw an action, and the platform checks all five of its conditions on every
/// award — a job that is not yours, an offer that has just been withdrawn, and a job somebody
/// cancelled a second ago are all refused server-side with this screen none the wiser.
///
/// ## Awarding, and why it is a dialog rather than a button (SHIP-104)
///
/// The award is **irreversible and it is not only about the offer that was tapped.** `Docs/02` §3
/// has it atomically mark one bid accepted and **every other offer on the job rejected** (SHIP-93),
/// and a rejected offer can no longer be revised, withdrawn or countered. There is no un-award, and
/// no endpoint that would be one.
///
/// So the *Done when*'s "explicit confirmation" is taken at its strength: a modal naming the price
/// and the provider being committed to, saying in a sentence what happens to the other offers, and
/// dismissable without doing anything. A row of cards that scrolls horizontally under a thumb is
/// exactly where a mis-tap happens, and the cost of one here is somebody's delivery ended at the
/// wrong price.
///
/// ## And then it shows the result, which is a read rather than an assumption
///
/// After the platform accepts, the screen re-reads the job's offers with `?status=accepted` — the
/// way the contract names — and draws the accepted offer under a banner. It does **not** rewrite
/// the list it is holding to what it believes the award did: `Docs/02` §2 makes status the
/// platform's, and a client computing the consequences of a transition keeps a second copy of the
/// state machine that is correct until the day it is not.
class CompareOffersScreen extends ConsumerWidget {
  const CompareOffersScreen({required this.jobId, super.key});

  final String jobId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(compareOffersProvider(jobId));
    final controller = ref.read(compareOffersProvider(jobId).notifier);

    return Scaffold(
      appBar: AppBar(title: const Text('Offers on your delivery')),
      body: SafeArea(
        child: switch (state) {
          final s when s.isFirstLoad => const Center(
              key: Key('compare-offers-loading'),
              child: CircularProgressIndicator(),
            ),
          final s when s.failedOutright => _Failed(
              failure: s.failure!,
              onRetry: controller.retry,
            ),
          final s when s.isEmpty => const _NoOffersYet(),
          _ => RefreshIndicator(
              onRefresh: controller.refresh,
              child: _Comparison(jobId: jobId, state: state, controller: controller),
            ),
        },
      ),
    );
  }
}

/// The comparison itself: a control that orders the offers, then the cards.
class _Comparison extends ConsumerWidget {
  const _Comparison({required this.jobId, required this.state, required this.controller});

  final String jobId;
  final CompareOffersState state;
  final CompareOffersController controller;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final offers = state.sorted;
    final award = ref.watch(awardProvider(jobId));

    return ListView(
      key: const Key('compare-offers-list'),
      padding: const EdgeInsets.symmetric(vertical: 16),
      children: [
        if (award.awarded) ...[
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            child: _Awarded(award.accepted!),
          ),
        ],
        if (award.failure != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            child: _AwardFailed(
              state: award,
              onDismiss: ref.read(awardProvider(jobId).notifier).dismissFailure,
            ),
          ),
        if (state.failure != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            child: _ReloadFailed(state.failure!),
          ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          child: Text(
            // After an award the list answers a different question — the offer that was accepted,
            // read back from the platform — so counting it as "1 offer" would be an odd thing to
            // say about a delivery that has just been awarded.
            state.showingAccepted
                ? 'The offer you accepted'
                : offers.length == 1
                    ? '1 offer'
                    : '${offers.length} offers',
            key: const Key('compare-offers-count'),
            style: Theme.of(context).textTheme.titleMedium,
          ),
        ),
        const SizedBox(height: 12),
        // Ordering a list of one is a control with nothing to do. It goes rather than being
        // disabled: a disabled radio group under an awarded delivery reads as something that has
        // stopped working.
        if (!state.showingAccepted) _Order(order: state.order, onChanged: controller.orderBy),
        if (!state.sortedIsComplete) ...[
          const SizedBox(height: 8),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text(
              // **The honesty this screen owes.** Sorting what has been read is not sorting every
              // offer on the job, and a customer told "lowest price" over a partial list would
              // reasonably believe they had seen the cheapest.
              'Sorted from the offers loaded so far. Load the rest to compare them all.',
              key: const Key('compare-offers-partial'),
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ),
        ],
        const SizedBox(height: 16),

        // The row itself. A fixed height, because cards of different heights side by side stop the
        // rows lining up — which is the whole of what makes this a comparison.
        SizedBox(
          // Raised from 360 when SHIP-104 added the action, and again when SHIP-103 added the way
          // into the negotiation, so that both buttons are on the card rather than below the fold of
          // the card's own scroll view. A primary action a customer has to scroll a card to find is
          // one most of them will not find.
          height: 480,
          child: ListView.separated(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 16),
            itemCount: offers.length,
            separatorBuilder: (context, index) => const SizedBox(width: 12),
            itemBuilder: (context, index) => _OfferCard(offers[index], jobId: jobId),
          ),
        ),

        if (state.hasMore) ...[
          const SizedBox(height: 16),
          Center(
            child: state.loadingMore
                ? const CircularProgressIndicator()
                : OutlinedButton(
                    key: const Key('compare-offers-more'),
                    onPressed: controller.loadMore,
                    child: const Text('Load more offers'),
                  ),
          ),
        ],
      ],
    );
  }
}

/// How to order them.
///
/// **`RadioGroup` rather than a `groupValue` on each tile.** The per-tile form is deprecated after
/// Flutter 3.32 and the analyzer treats the deprecation as fatal here, so the pattern that compiles
/// is the ancestor widget — the same call `proof_capture_screen.dart` makes.
class _Order extends StatelessWidget {
  const _Order({required this.order, required this.onChanged});

  final OfferOrder order;
  final void Function(OfferOrder) onChanged;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.symmetric(horizontal: 16),
      child: RadioGroup<OfferOrder>(
        groupValue: order,
        onChanged: (chosen) {
          if (chosen != null) onChanged(chosen);
        },
        child: Wrap(
          spacing: 16,
          children: [
            for (final option in OfferOrder.values)
              // A radio and its label rather than a `RadioListTile`, so the three sit on one line
              // on a phone instead of taking three rows above the cards they order.
              InkWell(
                key: Key('compare-offers-order-${option.name}'),
                onTap: () => onChanged(option),
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Radio<OfferOrder>(value: option),
                    Text(option.label),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

/// One offer, with the four things `Docs/01` §4.3 asks a customer to compare, in a fixed order.
///
/// **Private, and it must stay private**, exactly as `_BidCard` on the provider's own list is. A
/// shared card taking either shape, or one taking a "this is the customer's view" flag, is the
/// arrangement the budget invariant is hardest to keep: the flag is one careless call site away
/// from being wrong and nothing would fail.
class _OfferCard extends ConsumerWidget {
  const _OfferCard(this.offer, {required this.jobId});

  final ReceivedOffer offer;

  /// The job, so the card can reach the award for it. Passed down rather than read off the offer's
  /// `jobId`: the route is where the identifier this screen is about comes from, and an offer whose
  /// `job_id` disagreed with the route is a response worth failing on rather than following.
  final String jobId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final amount = offer.amountCents;
    final award = ref.watch(awardProvider(jobId));

    return SizedBox(
      width: 280,
      child: Card(
        key: Key('compare-offer-${offer.id}'),
        margin: EdgeInsets.zero,
        // **Scrollable inside a fixed-height card, rather than a Column sized to fit.** The card
        // height is fixed because that is what makes the rows line up across the comparison, and
        // the content is not: an offer with no price wraps onto two lines, a customer's own counter
        // adds a line, and a vehicle with make, model and four capacity numbers adds two. A Column
        // sized to its children overflows on the first of those, which is a rendering error rather
        // than a truncation. Scrolling within the card keeps every card the same size and loses
        // nothing.
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              // 1. Price.
              Text(
                // Cents to dollars at the point of display and nowhere earlier (`Docs/10` §3.3).
                amount == null ? 'No price on this offer' : audFromCents(amount),
                key: Key('compare-offer-amount-${offer.id}'),
                style: theme.textTheme.headlineSmall,
              ),
              if (offer.offeredBy == BidParty.customer)
                Text(
                  // The live offer in a negotiation can be the customer's own counter, and it is
                  // not something they can award — `ck_bids_only_a_providers_offer_is_accepted`.
                  // Saying so is better than drawing it as though it were a provider's offer.
                  'Your counter-offer',
                  key: Key('compare-offer-yours-${offer.id}'),
                  style: theme.textTheme.labelMedium,
                ),
              const Divider(height: 24),

              // 2. Timing.
              _Row(
                icon: Icons.trip_origin,
                label: 'Collect',
                value: dayFirstDateTime(offer.pickupAt) ?? 'Not stated',
                valueKey: Key('compare-offer-pickup-${offer.id}'),
              ),
              const SizedBox(height: 8),
              _Row(
                icon: Icons.flag_outlined,
                label: 'Deliver by',
                value: dayFirstDateTime(offer.deliverBy) ?? 'Not stated',
                valueKey: Key('compare-offer-deliver-${offer.id}'),
              ),
              const Divider(height: 24),

              // 3. The provider.
              _Provider(offer),
              const Divider(height: 24),

              // 4. The vehicle and its declared capability.
              _Vehicle(offer),

              // 5. The way into the negotiation (SHIP-103).
              //
              // **Offered on every card, whatever the offer's state**, which is the same call
              // `_CompareOffers` on the job screen makes about itself: the conversation endpoint has
              // no status rule at all — "the moment two parties most need to arrange something is
              // after the award" — so a rule on the device about when it is worth opening would be a
              // copy of a decision the platform deliberately did not make.
              const SizedBox(height: 16),
              _NegotiateButton(offer: offer, jobId: jobId),

              // 6. The action, when there is one to offer (SHIP-104).
              //
              // Below the four things being compared rather than above them, because it is what a
              // customer does *after* reading the card. `isAwardable` is presentation — the
              // platform decides — and the button is simply absent on the customer's own counter
              // and on an offer that has ended, which the rows above already say in words.
              if (offer.isAwardable && !award.awarded) ...[
                const SizedBox(height: 8),
                _AwardButton(offer: offer, jobId: jobId, awarding: award.awarding),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// The way from one offer into the negotiation behind it (SHIP-103).
///
/// **The card is the summary and the negotiation is the record.** A card shows the offer as it
/// stands; the screen behind this shows every round it went through, both parties' words, and the
/// two ways to answer. `Docs/01` §4.3 requires the platform to record all offers, counter-offers and
/// withdrawals, and this is the customer's way to read that record.
///
/// It is addressed by **this offer's** identifier, which is enough: the platform resolves the whole
/// chain from any offer in it, so a customer opening the negotiation from a superseded card and one
/// opening it from the live head arrive at the same conversation.
class _NegotiateButton extends StatelessWidget {
  const _NegotiateButton({required this.offer, required this.jobId});

  final ReceivedOffer offer;
  final String jobId;

  @override
  Widget build(BuildContext context) {
    return SizedBox(
      width: double.infinity,
      child: OutlinedButton.icon(
        key: Key('negotiate-offer-${offer.id}'),
        // `push` rather than `go`, so the back gesture returns to the comparison where it was
        // rather than re-reading every page the customer had loaded.
        onPressed: () => context.push(Routes.negotiationFor(jobId, offer.id)),
        icon: const Icon(Icons.forum_outlined),
        label: const Text('Message and counter'),
      ),
    );
  }
}

/// The button, and the confirmation it opens (SHIP-104).
class _AwardButton extends ConsumerWidget {
  const _AwardButton({required this.offer, required this.jobId, required this.awarding});

  final ReceivedOffer offer;
  final String jobId;

  /// Whether **any** award on this job is in flight.
  ///
  /// Every card is disabled while one is, not merely the card that was tapped. Two awards on one
  /// job are two different offers and the second would be refused `409 conflict` — but the customer
  /// would have watched two spinners and been told one of them failed, which is not what happened.
  final bool awarding;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return SizedBox(
      width: double.infinity,
      child: FilledButton(
        key: Key('award-offer-${offer.id}'),
        onPressed: awarding ? null : () => _confirm(context, ref),
        child: awarding
            ? const SizedBox(
                height: 18,
                width: 18,
                child: CircularProgressIndicator(strokeWidth: 2),
              )
            : const Text('Award this offer'),
      ),
    );
  }

  Future<void> _confirm(BuildContext context, WidgetRef ref) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => _AwardConfirmation(offer),
    );
    if (confirmed != true) return;

    final awarded = await ref.read(awardProvider(jobId).notifier).award(offer.id);
    if (!awarded) return;

    // The result the *Done when* asks for, read back from the platform rather than assumed. See
    // `CompareOffersController.showAwarded`.
    await ref.read(compareOffersProvider(jobId).notifier).showAwarded();
  }
}

/// The explicit confirmation.
///
/// **It names the two things being committed to** — the price and who is being engaged — because a
/// dialog reading "Are you sure?" over a horizontally scrolling row of near-identical cards
/// confirms that the customer tapped something, not that they tapped the right thing.
///
/// It says what happens to the other offers, which is the part nobody would guess: the award closes
/// every one of them in the same transaction (`Docs/02` §3, SHIP-93), and none of them can be
/// revived. And it says there is no way back, because there is not — `contracts/paths/bidding.yaml`
/// has one job and one accepted bid, held by a database constraint.
class _AwardConfirmation extends StatelessWidget {
  const _AwardConfirmation(this.offer);

  final ReceivedOffer offer;

  @override
  Widget build(BuildContext context) {
    final amount = offer.amountCents;
    final verified = offer.provider?.verified ?? false;

    return AlertDialog(
      key: const Key('award-confirm'),
      title: const Text('Award this delivery?'),
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            amount == null
                ? 'This offer names no price.'
                : 'You will be committing to ${audFromCents(amount)}.',
            key: const Key('award-confirm-amount'),
            style: Theme.of(context).textTheme.titleMedium,
          ),
          const SizedBox(height: 8),
          Text(
            // The same two facts the card shows, and no more: there is no trading name and no
            // rating on this platform, and inventing a warmer sentence here would imply one.
            verified
                ? 'The provider has cleared verification.'
                : 'This provider has not yet cleared verification.',
            key: const Key('award-confirm-provider'),
          ),
          const SizedBox(height: 12),
          const Text(
            'Awarding closes every other offer on this delivery, and it cannot be undone.',
            key: Key('award-confirm-consequence'),
          ),
        ],
      ),
      actions: [
        TextButton(
          key: const Key('award-confirm-cancel'),
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('Cancel'),
        ),
        FilledButton(
          key: const Key('award-confirm-accept'),
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('Award'),
        ),
      ],
    );
  }
}

/// What the customer sees once the platform has accepted (SHIP-104).
///
/// It draws from [Bid] — the award response — rather than from the list, so it is on screen the
/// instant the platform answers and does not wait for the re-read behind it. The list follows and
/// shows the same offer with its provider and vehicle.
class _Awarded extends StatelessWidget {
  const _Awarded(this.accepted);

  final Bid accepted;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final amount = accepted.amountCents;

    return Card(
      key: const Key('award-result'),
      color: theme.colorScheme.secondaryContainer,
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(Icons.handshake_outlined, color: theme.colorScheme.onSecondaryContainer),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text('Delivery awarded', style: theme.textTheme.titleMedium),
                  const SizedBox(height: 4),
                  Text(
                    amount == null
                        ? 'The offer you accepted named no price.'
                        : 'You accepted ${audFromCents(amount)}. Every other offer is now closed.',
                    key: const Key('award-result-amount'),
                    style: theme.textTheme.bodyMedium,
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// The platform refused the award.
///
/// Every one of the four codes is a thing the customer can do something about, and the sentence
/// comes from [AwardState.refusal] rather than from the platform's `message`: `Docs/07` §6 branches
/// on `code`, and a message is copy that gets reworded.
class _AwardFailed extends StatelessWidget {
  const _AwardFailed({required this.state, required this.onDismiss});

  final AwardState state;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Card(
      key: const Key('award-failed'),
      color: theme.colorScheme.errorContainer,
      margin: EdgeInsets.zero,
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              state.refusal ??
                  // Anything the codes do not name — no connection, a `500`, a `503`. Nothing is
                  // known about whether the award happened, so the honest advice is to look rather
                  // than to promise it did not.
                  'The delivery was not awarded. Check your connection and try again — if the '
                      'offer disappears when you reload, it went through.',
              key: const Key('award-failed-message'),
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 8),
            Align(
              alignment: Alignment.centerRight,
              child: TextButton(
                key: const Key('award-failed-dismiss'),
                onPressed: onDismiss,
                child: const Text('Close'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// What this platform will tell a customer about a provider, which is two facts.
class _Provider extends StatelessWidget {
  const _Provider(this.offer);

  final ReceivedOffer offer;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final provider = offer.provider;

    if (provider == null) {
      return Text(
        'Provider details unavailable',
        key: Key('compare-offer-provider-${offer.id}'),
        style: theme.textTheme.bodySmall,
      );
    }

    final since = dayFirstDate(provider.memberSince);

    return Column(
      key: Key('compare-offer-provider-${offer.id}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(
              provider.verified ? Icons.verified_outlined : Icons.help_outline,
              size: 18,
              color: provider.verified ? theme.colorScheme.primary : theme.colorScheme.outline,
            ),
            const SizedBox(width: 8),
            // `Expanded`, because a card is a fixed 280 wide and the text is not: without it the
            // row is sized to its natural width and overflows on the narrow phone this screen is
            // drawn on. The comparison depends on every card being the same width, so the text
            // wraps rather than the card growing.
            Expanded(
              child: Text(
                // **A statement about the account, not a rating.** There is no rating to show and
                // implying one would be worse than saying nothing.
                provider.verified ? 'Verified provider' : 'Not yet verified',
                key: Key('compare-offer-verified-${offer.id}'),
                style: theme.textTheme.bodyMedium,
              ),
            ),
          ],
        ),
        if (since != null) ...[
          const SizedBox(height: 4),
          Text(
            'On Shipper since $since',
            key: Key('compare-offer-since-${offer.id}'),
            style: theme.textTheme.bodySmall,
          ),
        ],
      ],
    );
  }
}

/// The vehicle the offer is made with, and what the provider declared it can carry.
class _Vehicle extends StatelessWidget {
  const _Vehicle(this.offer);

  final ReceivedOffer offer;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final vehicle = offer.vehicle;

    if (vehicle == null) {
      return Text(
        // **The ordinary case rather than a fault**: every offer placed before providers could
        // state a vehicle names none, as does every offer from a client that does not send it. The
        // wording says what is true of the offer rather than suggesting something went wrong.
        'No vehicle stated on this offer',
        key: Key('compare-offer-vehicle-${offer.id}'),
        style: theme.textTheme.bodySmall,
      );
    }

    final capacity = vehicle.capacity;
    final described = vehicle.description;

    return Column(
      key: Key('compare-offer-vehicle-${offer.id}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            const Icon(Icons.local_shipping_outlined, size: 18),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                described.isEmpty ? _readable(vehicle.type) : '$described (${_readable(vehicle.type)})',
                key: Key('compare-offer-vehicle-name-${offer.id}'),
                style: theme.textTheme.bodyMedium,
              ),
            ),
          ],
        ),
        if (capacity.statesAnything) ...[
          const SizedBox(height: 4),
          Text(
            // Kilograms and centimetres (`CLAUDE.md`). A zero is "unstated" rather than "nothing",
            // so each part is only drawn when the provider declared it.
            <String>[
              if (capacity.maxWeightKg > 0) 'up to ${_trim(capacity.maxWeightKg)} kg',
              if (capacity.lengthCm > 0 || capacity.widthCm > 0 || capacity.heightCm > 0)
                '${capacity.lengthCm} × ${capacity.widthCm} × ${capacity.heightCm} cm',
            ].join(', '),
            key: Key('compare-offer-capacity-${offer.id}'),
            style: theme.textTheme.bodySmall,
          ),
        ],
      ],
    );
  }

  /// Turns `box_truck` into `Box truck`.
  ///
  /// **Here rather than in a compiled-in map of vehicle types**, which is the point: the list is
  /// the platform's and a type added there must render sensibly on a phone that has never heard of
  /// it, rather than as "Unknown" until the next store release (`CLAUDE.md`, `Docs/07` §6).
  static String _readable(String type) {
    if (type.isEmpty) return 'Vehicle';
    final words = type.replaceAll('_', ' ');
    return words[0].toUpperCase() + words.substring(1);
  }

  /// 1200.0 → `1200`, 1200.5 → `1200.5`.
  static String _trim(double value) =>
      value == value.roundToDouble() ? value.toStringAsFixed(0) : value.toString();
}

/// One labelled line inside a card, so every card's rows line up across the row.
class _Row extends StatelessWidget {
  const _Row({
    required this.icon,
    required this.label,
    required this.value,
    required this.valueKey,
  });

  final IconData icon;
  final String label;
  final String value;
  final Key valueKey;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 18, color: theme.colorScheme.outline),
        const SizedBox(width: 8),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(label, style: theme.textTheme.labelSmall),
              Text(value, key: valueKey, style: theme.textTheme.bodyMedium),
            ],
          ),
        ),
      ],
    );
  }
}

/// Nobody has offered yet.
///
/// **An ordinary state, and the first one a customer meets.** A job published a minute ago has no
/// offers and nothing is wrong, so this says what happens next rather than apologising.
class _NoOffersYet extends StatelessWidget {
  const _NoOffersYet();

  @override
  Widget build(BuildContext context) {
    return Center(
      key: const Key('compare-offers-empty'),
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const Icon(Icons.inbox_outlined, size: 48),
            const SizedBox(height: 16),
            Text(
              'No offers yet',
              style: Theme.of(context).textTheme.titleMedium,
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 8),
            const Text(
              'Providers who can carry this delivery will see it in their feed. '
              'Offers appear here as they arrive.',
              textAlign: TextAlign.center,
            ),
          ],
        ),
      ),
    );
  }
}

/// Nothing arrived at all.
class _Failed extends StatelessWidget {
  const _Failed({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      key: const Key('compare-offers-failed'),
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(failure.userMessage, textAlign: TextAlign.center),
            const SizedBox(height: 16),
            FilledButton(
              key: const Key('compare-offers-retry'),
              onPressed: onRetry,
              child: const Text('Try again'),
            ),
          ],
        ),
      ),
    );
  }
}

/// A reload failed and the offers already read are still on screen.
class _ReloadFailed extends StatelessWidget {
  const _ReloadFailed(this.failure);

  final ApiFailure failure;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      key: const Key('compare-offers-stale'),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text(
        'These offers may be out of date. ${failure.userMessage}',
        style: TextStyle(color: theme.colorScheme.onErrorContainer),
      ),
    );
  }
}
