import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/my_bids_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';
import 'package:shipper/shared/formatting/money.dart';

/// Every offer this provider has made, grouped by status (SHIP-101).
///
/// Reads `GET /v1/fleet/bids` (SHIP-101a), which is the endpoint this screen was struck for two
/// waves waiting on: `Docs/11` §6 carried SHIP-101 as "every dependency met and unbuildable in fact"
/// because nothing on the served surface listed a provider their own bids.
///
/// ## The customer's budget is not on this screen, in any form, and there is nowhere to put one
///
/// `Docs/01` §4.3 keeps the customer's maximum private from providers — **not as an amount, not as
/// a band, and not as a "budget supplied" flag**. This screen is a provider surface, and the rule is
/// kept the way `ProviderJobFeed` keeps it: structurally. [Bid] carries nothing of the job beyond
/// its identifier, so there is no field to withhold — and this file must never grow a placeholder,
/// an empty row, a "budget on request" line, a sort by how close an offer is to one, or copy that
/// tells a provider a budget exists.
///
/// Three guards hold that, and they fail on three different axes:
///
/// - `budget_stays_on_the_customer_side_test.dart` scans this file for the name, and holds [Bid] to
///   a **closed key set**, so a field added to it fails whatever it is called — `max_price` is a
///   budget and does not contain the word.
/// - `my_bids_test.dart` renders a page whose rows carry seven spellings of a customer's maximum and
///   asserts that **no rendering of any of them reaches a pixel**. That is the axis the other two
///   cannot have: a screen that read a budget from somewhere other than the model it binds — a
///   route argument, a second request, a controller field — passes both of them and fails this.
///
/// ## The number on this screen is the provider's own price
///
/// [Bid.amountCents] is what this device sent. It has nothing to do with anything the customer set,
/// and the contract says so in as many words.
///
/// ## What it deliberately does not show, because nothing serves it
///
/// **Anything about the job.** A row names the job it is against and cannot describe it: `Bid` has
/// no route, no goods and no dates of the customer's in it. "View the job" leads to the provider's
/// job detail, which answers `404` for a job that is no longer open — so an offer that was accepted,
/// rejected or expired leads to a screen that cannot load, and the platform's refusal is what the
/// provider is shown rather than a guess made here. `Docs/11` §3 records that as a read gap rather
/// than working around it: **no endpoint serves a provider the job behind a closed bid.**
class MyBidsScreen extends StatelessWidget {
  const MyBidsScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Your bids')),
      // The provider's half of the marketplace, said by the surface rather than by the router
      // (`Docs/07` §1, SHIP-98). A customer reaching this route by any means sees the same thing
      // they see on the fleet: an honest statement that this is not their side, and **no request is
      // made on their behalf**.
      //
      // **That second half is why the list below is a widget rather than a subtree built here.**
      // `ProviderOnly` only *mounts* its child for a provider, and a `ref.watch(myBidsProvider)` in
      // this method would run before the role was ever consulted — one `GET /v1/fleet/bids` issued
      // on behalf of an account with no business making it, on every customer who followed the
      // link. Constructing a `const _MyBids()` costs nothing and reads nothing until it is mounted.
      //
      // It is not an authorisation control and must never be made one. A build with `ProviderOnly`
      // deleted would show a customer this screen and change nothing about what they could do with
      // it: `GET /v1/fleet/bids` scopes to the caller's own id in its `WHERE` clause, so a
      // customer's answer is an empty page by construction rather than by permission.
      body: const ProviderOnly(child: _MyBids()),
    );
  }
}

/// The list itself. See [MyBidsScreen] for why this is a widget of its own.
class _MyBids extends ConsumerWidget {
  const _MyBids();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(myBidsProvider);
    final controller = ref.read(myBidsProvider.notifier);

    return SafeArea(
      child: RefreshIndicator(
        onRefresh: controller.refresh,
        child: ListView(
          key: const Key('my-bids'),
          // Always scrollable, so the pull works on the empty state and on the failure state as
          // well. A refresh gesture that only worked once there was something to scroll would stop
          // working exactly when somebody needed it.
          physics: const AlwaysScrollableScrollPhysics(),
          padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
          children: _children(theme, state, controller),
        ),
      ),
    );
  }

  List<Widget> _children(ThemeData theme, MyBidsState state, MyBidsController controller) {
    final failure = state.failure;

    return <Widget>[
      Text(
        // Says what the list is before it says what is in it, and says the one thing about it that
        // is not obvious: a customer's counter-offer carries this provider's id and is on this list.
        // A provider who did not know that would read their own screen as showing them prices they
        // never quoted.
        'Every offer in every negotiation you are in, newest first. A counter-offer from a customer '
        'is on this list too — it is the one waiting for an answer from you.',
        style: theme.textTheme.bodyMedium,
      ),
      const SizedBox(height: 16),

      // A failure with offers already on screen is a banner above them, not a replacement for them.
      if (failure != null && state.bids.isNotEmpty) ...[
        FailureBanner(failure),
        const SizedBox(height: 16),
      ],

      if (state.isFirstLoad)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 48),
          child: Center(child: CircularProgressIndicator(key: Key('my-bids-loading'))),
        )
      else if (state.failedOutright && failure != null)
        _CouldNotLoad(failure: failure, onRetry: controller.retry)
      else if (state.isEmpty)
        const _NothingOffered()
      else ...[
        _Groups(state: state, onChanged: controller.show),
        const SizedBox(height: 16),
        if (state.isNarrowedToNothing)
          _NoneInThatGroup(state: state, onClear: () => unawaited(controller.show(null)))
        else
          for (final group in state.groups) ...[
            _GroupHeading(status: group.status, count: group.bids.length),
            const SizedBox(height: 8),
            for (final bid in group.bids) ...[
              _BidCard(bid),
              const SizedBox(height: 8),
            ],
            const SizedBox(height: 16),
          ],
        if (state.hasMore) _ShowMore(state: state, onPressed: controller.loadMore),
      ],
    ];
  }
}

/// The group control.
///
/// **These chips ask the platform a different question**, which is the opposite of the feed's — see
/// `MyBidsController.show`. `GET /v1/fleet/bids` runs `?status=` in SQL, so picking one is a
/// narrower read rather than a narrower drawing, and the page that comes back is whole.
///
/// The options are the statuses **that have been read**, never a compiled-in list of eight. Two
/// reasons: `CLAUDE.md` keeps anything that changes under operational pressure off a device with no
/// over-the-air path, and half of `Docs/02` §4's eight are statuses most providers never see —
/// `Draft` is one no client can obtain at all, because no endpoint creates one and none returns one.
class _Groups extends StatelessWidget {
  const _Groups({required this.state, required this.onChanged});

  final MyBidsState state;
  final void Function(BidStatus?) onChanged;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final offered = state.offeredGroups;

    // One group and nothing to compare it with. A single chip can only ever narrow to the whole
    // list, which is the same reasoning `facetIsUseful` applies to the feed's facets.
    if (offered.length < 2 && state.only == null) return const SizedBox.shrink();

    return Column(
      key: const Key('my-bids-groups'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'Show',
          style: theme.textTheme.titleSmall?.copyWith(color: theme.colorScheme.primary),
        ),
        const SizedBox(height: 8),
        Wrap(
          spacing: 8,
          runSpacing: 8,
          children: [
            ChoiceChip(
              key: const Key('my-bids-group-all'),
              label: const Text('Every status'),
              selected: state.only == null,
              onSelected: (_) => onChanged(null),
            ),
            for (final status in offered)
              ChoiceChip(
                // Keyed by the wire form rather than the label: a test naming
                // `my-bids-group-submitted` is naming the contract, and the label is copy that may
                // be reworded.
                key: Key('my-bids-group-${status.wireName}'),
                label: Text(status.label),
                selected: state.only == status,
                onSelected: (_) => onChanged(status),
              ),
          ],
        ),
      ],
    );
  }
}

/// One status heading, with how many offers are under it.
class _GroupHeading extends StatelessWidget {
  const _GroupHeading({required this.status, required this.count});

  final BidStatus status;
  final int count;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Row(
      key: Key('my-bids-heading-${status.wireName}'),
      children: [
        Expanded(
          child: Text(
            // The exact name from `Docs/02` §4 (`CLAUDE.md`), because a provider reading
            // "Superseded" here and support reading it in an audit entry have to be reading about
            // the same thing.
            status.label,
            style: theme.textTheme.titleMedium?.copyWith(color: theme.colorScheme.primary),
          ),
        ),
        Text('$count', style: theme.textTheme.labelLarge),
      ],
    );
  }
}

/// One offer, as the provider who is a party to it sees it.
///
/// **Private, and it must stay private.** The customer's comparison screen will draw a different
/// shape from a different endpoint; a shared card taking both, or one taking a "show the budget"
/// flag, is the arrangement `Docs/01` §4.3 is hardest to keep — the flag is one careless call site
/// away from being wrong and nothing would fail.
class _BidCard extends StatelessWidget {
  const _BidCard(this.bid);

  final Bid bid;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final amount = bid.amountCents;
    final made = dayFirstDate(bid.createdAt);

    return Card(
      key: Key('my-bid-${bid.id}'),
      margin: EdgeInsets.zero,
      child: InkWell(
        // `push` rather than `go`, so the back gesture returns to the list where it was rather than
        // rebuilding it — which would re-read every page the provider had loaded.
        //
        // The destination answers `404` once the job is no longer open, and that is the platform's
        // answer rather than this screen's guess. See the note on `MyBidsScreen`.
        onTap: () => context.push(Routes.openJobDetailFor(bid.jobId)),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    child: Text(
                      // The provider's own price. Cents to dollars at the point of display and
                      // nowhere earlier (`Docs/10` §3.3).
                      amount == null ? 'No price on this offer' : audFromCents(amount),
                      key: Key('my-bid-amount-${bid.id}'),
                      style: theme.textTheme.titleLarge,
                    ),
                  ),
                  _OfferedBy(bid.offeredBy),
                ],
              ),
              const SizedBox(height: 8),
              _Commitment(
                icon: Icons.trip_origin,
                label: 'Collect',
                // Day-first with the month spelled and the time of day beside it (`CLAUDE.md`).
                // **A bid is two instants and not two windows**: a provider says "I will be there at
                // nine", so dropping the time would drop half of what was offered.
                at: dayFirstDateTime(bid.pickupAt),
              ),
              const SizedBox(height: 4),
              _Commitment(
                icon: Icons.place_outlined,
                label: 'Deliver by',
                at: dayFirstDateTime(bid.deliverBy),
              ),
              if (bid.message case final message? when message.isNotEmpty) ...[
                const SizedBox(height: 8),
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(Icons.info_outline, size: 16, color: theme.colorScheme.onSurfaceVariant),
                    const SizedBox(width: 8),
                    Expanded(child: Text(message, style: theme.textTheme.bodySmall)),
                  ],
                ),
              ],
              if (bid.supersededBy != null) ...[
                const SizedBox(height: 8),
                Text(
                  // `Docs/02` §4 keeps the whole chain readable, and this is the row's place in it.
                  // Said in words rather than shown as a state, because "an offer answered with
                  // another one" is not something a colour communicates.
                  'A counter-offer has answered this one. It can be read but not acted on.',
                  key: Key('my-bid-superseded-${bid.id}'),
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ],
              const SizedBox(height: 8),
              Row(
                children: [
                  Expanded(
                    child: Text(
                      // The date the offer was made, so two offers on one job are in order without
                      // the provider counting rows. Day-first with the month spelled (`CLAUDE.md`).
                      made == null ? 'Offered' : 'Offered $made',
                      style: theme.textTheme.bodySmall
                          ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                    ),
                  ),
                  // Says the card leads somewhere. A card that is tappable and does not look it is
                  // a screen most people never find.
                  Text('View the job', style: theme.textTheme.labelLarge),
                  Icon(Icons.chevron_right, color: theme.colorScheme.onSurfaceVariant),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Which party made this offer.
///
/// `Docs/02` §4's negotiation alternates and this says whose turn each entry was (SHIP-87). It is
/// **not derivable from position**, which is why the platform sends it: a provider may join a
/// negotiation halfway, and one job's negotiation may hold more than one chain.
class _OfferedBy extends StatelessWidget {
  const _OfferedBy(this.party);

  final BidParty? party;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    final label = switch (party) {
      BidParty.provider => 'Yours',
      BidParty.customer => 'From the customer',
      // A third kind of party this build has never heard of, and a party the platform did not send.
      // Both are drawn as nothing rather than as a fault: the offer is fine and the label is not
      // what a provider is here for.
      _ => null,
    };
    if (label == null) return const SizedBox.shrink();

    return Container(
      key: Key('my-bid-party-${party!.wireName}'),
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer,
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text(
        label,
        style: theme.textTheme.labelSmall?.copyWith(color: theme.colorScheme.onSecondaryContainer),
      ),
    );
  }
}

/// One of the two instants an offer commits to.
class _Commitment extends StatelessWidget {
  const _Commitment({required this.icon, required this.label, required this.at});

  final IconData icon;
  final String label;
  final String? at;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Row(
      children: [
        Icon(icon, size: 16, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 8),
        Text('$label ', style: theme.textTheme.labelMedium),
        Expanded(
          child: Text(
            // A commitment the platform did not send. It is required by the contract, so this is
            // the old-build case `Docs/07` §6 is built on rather than an ordinary one — and saying
            // nothing is better than saying a date nobody chose.
            at ?? 'Not stated',
            style: theme.textTheme.bodyMedium,
          ),
        ),
      ],
    );
  }
}

/// What a provider who has never bid sees.
///
/// An empty state and never a spinner or an error: `data` is `[]` rather than `null` precisely so
/// this case is ordinary, and a client that treated it as "still loading" would leave every new
/// provider watching a spinner for ever.
class _NothingOffered extends StatelessWidget {
  const _NothingOffered();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('my-bids-empty'),
      padding: const EdgeInsets.symmetric(vertical: 32),
      child: Column(
        children: [
          Icon(Icons.gavel_outlined, size: 48, color: theme.colorScheme.outline),
          const SizedBox(height: 16),
          Text(
            'You have not bid on anything yet',
            style: theme.textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 8),
          Text(
            'Offers you make appear here, grouped by what has become of them.',
            style: theme.textTheme.bodyMedium,
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

/// What a provider sees when the group they asked for holds nothing.
///
/// **Deliberately not the empty state.** "You have not bid on anything" and "nothing of yours is
/// expired" are different things to be told, and only the second has "show every status" as its
/// answer.
class _NoneInThatGroup extends StatelessWidget {
  const _NoneInThatGroup({required this.state, required this.onClear});

  final MyBidsState state;
  final VoidCallback onClear;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('my-bids-none-in-group'),
      padding: const EdgeInsets.symmetric(vertical: 32),
      child: Column(
        children: [
          Icon(Icons.filter_alt_off_outlined, size: 40, color: theme.colorScheme.outline),
          const SizedBox(height: 12),
          Text(
            'Nothing of yours is ${state.only?.label.toLowerCase() ?? 'in that group'}',
            style: theme.textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('my-bids-show-all'),
            onPressed: onClear,
            child: const Text('Show every status'),
          ),
        ],
      ),
    );
  }
}

/// What a provider sees when the list could not be read at all.
///
/// Kept apart from the empty state on purpose. "You have not bid on anything" and "we could not find
/// out" are different things to be told, and only one of them has a retry.
class _CouldNotLoad extends StatelessWidget {
  const _CouldNotLoad({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('my-bids-failed'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          FailureBanner(failure),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('my-bids-retry'),
            onPressed: () => unawaited(onRetry()),
            child: const Text('Try again'),
          ),
        ],
      ),
    );
  }
}

/// The next page, asked for rather than swallowed.
class _ShowMore extends StatelessWidget {
  const _ShowMore({required this.state, required this.onPressed});

  final MyBidsState state;
  final Future<void> Function() onPressed;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      children: [
        Text(
          // Said out loud, and it is the sentence that stops this screen lying. The headings and the
          // chips above are built from the offers that have been read, so a status that appears only
          // on the next page has no group yet — a screen showing four offers under two headings when
          // there are nine under five has quietly claimed to be a complete list.
          state.only == null
              ? 'Grouped from the offers read so far. There are more to read.'
              : 'Showing the most recent of these. There are more to read.',
          key: const Key('my-bids-partial'),
          style: theme.textTheme.bodySmall,
          textAlign: TextAlign.center,
        ),
        const SizedBox(height: 8),
        OutlinedButton(
          key: const Key('my-bids-more'),
          onPressed: state.loadingMore ? null : () => unawaited(onPressed()),
          child: state.loadingMore
              ? const SizedBox(height: 20, width: 20, child: CircularProgressIndicator(strokeWidth: 2))
              : const Text('Show more offers'),
        ),
      ],
    );
  }
}
