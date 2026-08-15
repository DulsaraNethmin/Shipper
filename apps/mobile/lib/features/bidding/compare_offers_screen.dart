import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/errors/api_failure.dart';
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
/// chooses whether to draw an action, and the award itself is SHIP-104 — this screen deliberately
/// ships without one rather than with a button that goes nowhere.
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
              child: _Comparison(state: state, controller: controller),
            ),
        },
      ),
    );
  }
}

/// The comparison itself: a control that orders the offers, then the cards.
class _Comparison extends StatelessWidget {
  const _Comparison({required this.state, required this.controller});

  final CompareOffersState state;
  final CompareOffersController controller;

  @override
  Widget build(BuildContext context) {
    final offers = state.sorted;

    return ListView(
      key: const Key('compare-offers-list'),
      padding: const EdgeInsets.symmetric(vertical: 16),
      children: [
        if (state.failure != null)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
            child: _ReloadFailed(state.failure!),
          ),
        Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16),
          child: Text(
            offers.length == 1 ? '1 offer' : '${offers.length} offers',
            key: const Key('compare-offers-count'),
            style: Theme.of(context).textTheme.titleMedium,
          ),
        ),
        const SizedBox(height: 12),
        _Order(order: state.order, onChanged: controller.orderBy),
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
          height: 360,
          child: ListView.separated(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: 16),
            itemCount: offers.length,
            separatorBuilder: (context, index) => const SizedBox(width: 12),
            itemBuilder: (context, index) => _OfferCard(offers[index]),
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
class _OfferCard extends StatelessWidget {
  const _OfferCard(this.offer);

  final ReceivedOffer offer;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final amount = offer.amountCents;

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
            ],
          ),
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
