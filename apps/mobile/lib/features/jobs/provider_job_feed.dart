import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';
import 'package:shipper/features/jobs/open_jobs_controller.dart';
import 'package:shipper/features/jobs/open_jobs_filter.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// The jobs this provider may bid on (SHIP-99).
///
/// This is the provider half of the signed-in shell, and it is the counterpart of
/// `CustomerJobList`: `Docs/07` §1 requires the two halves of the marketplace to be genuinely
/// separate inside the one app, and the shell's role switch is what makes that separation. A
/// customer never reaches this widget, and no customer screen may reuse anything in this file.
///
/// ## No budget, in any form, and no affordance implying one
///
/// `Docs/01` §4.3 keeps the customer's maximum private from providers — not as an amount, not as a
/// band, and **not as a "budget supplied" flag**. [OpenJob] has no field it could be in, and this
/// screen must never grow a placeholder, an empty row, a "budget on request" line or a sort by
/// price. `budget_stays_on_the_customer_side_test.dart` fails if this file names it at all, and
/// `provider_job_feed_test.dart` renders a payload carrying one and asserts nothing of it reaches
/// the screen.
///
/// ## The filters narrow what was read, and the screen says so
///
/// `GET /v1/fleet/jobs` accepts no filter (see `open_jobs_repository.dart`), so the chips below
/// hide jobs already in hand rather than asking a different question. Two consequences the screen
/// has to be honest about, and both of them are cases a provider meets and a developer does not:
///
/// - **The options come from the jobs**, so a state that appears only on the second page has no
///   chip until that page is read. Nothing is compiled in, which is what `CLAUDE.md` requires of
///   anything that changes under operational pressure.
/// - **Narrowing to nothing is not an empty feed.** If the filter hides everything read so far and
///   more pages exist, the screen offers the next page instead of claiming there is no work.
///
/// ## The empty state is where a provider is sent to fix their eligibility
///
/// An empty page is the truthful answer to four situations, three of which the provider can act
/// on: no vehicle in service, no service area declared, the verification baseline not met, or
/// simply no open work today. The platform will not say which — it answers all four with `[]` —
/// so the screen names them and puts the fleet within reach rather than reporting an error.
class ProviderJobFeed extends ConsumerWidget {
  const ProviderJobFeed({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(openJobsProvider);
    final controller = ref.read(openJobsProvider.notifier);

    return RefreshIndicator(
      // Pull-to-refresh. It holds its own spinner until the read finishes, which is why
      // `refresh()` returns its future rather than swallowing it.
      onRefresh: controller.refresh,
      child: ListView(
        // The key SHIP-52 gave the provider half. "The app is showing the provider surface" is
        // still the fact the routing and sign-in tests assert, and it is still this — the half
        // stopped being a placeholder here, exactly as the customer's did at SHIP-76.
        key: const Key('shell-provider'),
        // Always scrollable, so the pull works on the empty state and on the failure state as
        // well. A refresh gesture that only worked once there was something to scroll would stop
        // working exactly when somebody needed it.
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: _children(context, theme, state, controller),
      ),
    );
  }

  List<Widget> _children(
    BuildContext context,
    ThemeData theme,
    OpenJobsState state,
    OpenJobsController controller,
  ) {
    final failure = state.failure;

    return <Widget>[
      Text('Work you can bid on', style: theme.textTheme.headlineSmall),
      const SizedBox(height: 8),
      Text(
        // Says what the list is before it says what is in it. A provider seeing three jobs needs
        // to know whether that is the marketplace or their slice of it, and it is their slice —
        // decided by Shipper against what they have declared, not by anything on this device.
        'Shipper shows you the jobs your service area, your vehicles and your verification make '
        'you eligible for.',
        style: theme.textTheme.bodyMedium,
      ),
      const SizedBox(height: 12),
      const _ProviderShortcuts(),
      const SizedBox(height: 16),

      // A failure with a feed already on screen is a banner above it, not a replacement for it.
      // Somebody who pulled to refresh at a loading dock should still be looking at their work.
      if (failure != null && state.jobs.isNotEmpty) ...[
        FailureBanner(failure),
        const SizedBox(height: 16),
      ],

      if (state.isFirstLoad)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 48),
          child: Center(child: CircularProgressIndicator(key: Key('open-jobs-loading'))),
        )
      else if (state.failedOutright && failure != null)
        _CouldNotLoad(failure: failure, onRetry: controller.retry)
      else if (state.isEmpty)
        const _NothingEligible()
      else ...[
        if (state.hasFacets) ...[
          _Filters(state: state, onChanged: controller.narrow),
          const SizedBox(height: 16),
        ],
        if (state.isNarrowedToNothing)
          _NoneMatch(state: state, onClear: () => controller.narrow(state.filter.cleared()))
        else
          for (final job in state.visible) ...[
            _OpenJobCard(job),
            const SizedBox(height: 8),
          ],
        const SizedBox(height: 8),
        if (state.hasMore) _ShowMore(state: state, onPressed: controller.loadMore),
      ],
    ];
  }
}

/// The two places a provider goes that are not a job: their vehicles, and their offers.
///
/// They sit at the top rather than at the bottom because the first is the answer to the question the
/// **empty** feed raises — a provider with no vehicle in service is eligible for nothing, and the
/// platform reports that as an empty page rather than as a problem it can name — and the second is
/// what a provider opening the app most often came to check.
///
/// Inside this widget rather than on the shell's `Scaffold`, which is the decision SHIP-98 took for
/// the fleet's affordance: everything provider-only lives inside a surface only a provider is shown,
/// so the role is read in one place instead of two that can get out of step.
///
/// **Both `push` rather than `go`**, so the back gesture returns to the feed where it was rather
/// than rebuilding the shell — which would re-read the feed and lose the scroll position and the
/// provider's narrowing.
///
/// `Routes` is `core/routing`, so naming a location in `features/bidding` from `features/jobs` is
/// not a feature importing a feature: the constant is `core`'s and the screen behind it is supplied
/// by the router, which is the same composition `Docs/07` §2 requires and SHIP-100 already used for
/// the bid panel.
class _ProviderShortcuts extends StatelessWidget {
  const _ProviderShortcuts();

  @override
  Widget build(BuildContext context) {
    return Wrap(
      spacing: 8,
      runSpacing: 8,
      children: [
        OutlinedButton.icon(
          // The key SHIP-98 gave this affordance, unchanged: "a provider can reach their fleet from
          // the shell" is the same fact whether the half around it is a placeholder or a feed.
          key: const Key('manage-vehicles'),
          onPressed: () => context.push(Routes.fleet),
          icon: const Icon(Icons.local_shipping_outlined),
          label: const Text('Your vehicles'),
        ),
        OutlinedButton.icon(
          // SHIP-101. The list has no entry point anywhere else, so forgetting this button is a
          // screen nobody can reach without a deep link.
          key: const Key('your-bids'),
          onPressed: () => context.push(Routes.myBids),
          icon: const Icon(Icons.gavel_outlined),
          label: const Text('Your bids'),
        ),
      ],
    );
  }
}

/// The narrowing controls.
///
/// Chips rather than a dropdown or a sheet, because both facets are small, multi-select and worth
/// being able to see the state of without opening anything. A provider standing beside a truck
/// should be able to read what they are looking at in one glance.
///
/// **A facet with one option is not drawn.** See `facetIsUseful`: a single chip can only ever hide
/// the whole feed, and a provider whose work is all in one state should be shown work rather than
/// a row of buttons.
class _Filters extends StatelessWidget {
  const _Filters({required this.state, required this.onChanged});

  final OpenJobsState state;
  final void Function(OpenJobsFilter) onChanged;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final filter = state.filter;
    final states = state.pickupStates;
    final statuses = state.statuses;

    return Column(
      key: const Key('open-jobs-filters'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (facetIsUseful(states)) ...[
          _FacetHeading(
            label: 'Picked up in',
            // Said where the control is, because it is the sentence that stops the control lying.
            // The chips are built from the jobs that have been read, so a state on a page nobody
            // has asked for yet is a state with no chip.
            note: 'Built from the jobs shown below.',
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final abbreviation in states)
                FilterChip(
                  key: Key('open-jobs-state-$abbreviation'),
                  label: Text(abbreviation),
                  selected: filter.pickupStates.contains(abbreviation),
                  onSelected: (_) => onChanged(filter.togglePickupState(abbreviation)),
                ),
            ],
          ),
          const SizedBox(height: 12),
        ],
        if (facetIsUseful(statuses)) ...[
          const _FacetHeading(
            label: 'Bidding',
            // Explains what the distinction is for. A provider who does not know that
            // `Negotiating` means somebody has already bid is being offered a control with no
            // meaning attached to it.
            note: 'A job being negotiated already has bids on it.',
          ),
          const SizedBox(height: 8),
          Wrap(
            spacing: 8,
            runSpacing: 8,
            children: [
              for (final status in statuses)
                FilterChip(
                  // Keyed by the wire form rather than the label: a test naming
                  // `open-jobs-status-negotiating` is naming the contract, and the label is copy
                  // that may be reworded.
                  key: Key('open-jobs-status-${status.wireName}'),
                  label: Text(status.label),
                  selected: filter.statuses.contains(status),
                  onSelected: (_) => onChanged(filter.toggleStatus(status)),
                ),
            ],
          ),
          const SizedBox(height: 12),
        ],
        if (filter.isNotEmpty)
          Row(
            children: [
              Expanded(
                child: Text(
                  // The count is of what is drawn against what has been read, and both numbers are
                  // honest about being partial. "3 of 8 read" is a true sentence; "3 of 8" without
                  // the last word would be a claim about the marketplace.
                  '${state.visible.length} of ${state.jobs.length} jobs read',
                  key: const Key('open-jobs-narrowed'),
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ),
              TextButton(
                key: const Key('open-jobs-clear-filter'),
                onPressed: () => onChanged(filter.cleared()),
                child: const Text('Show all'),
              ),
            ],
          ),
      ],
    );
  }
}

/// One facet's label, and the sentence that keeps it honest.
class _FacetHeading extends StatelessWidget {
  const _FacetHeading({required this.label, required this.note});

  final String label;
  final String note;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          label,
          style: theme.textTheme.titleSmall?.copyWith(color: theme.colorScheme.primary),
        ),
        Text(
          note,
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ],
    );
  }
}

/// One job, as a provider deciding whether to bid sees it.
///
/// **Private, and it must stay private.** The customer's `_JobCard` draws a budget and this one
/// has no field to draw; a shared card taking both types, or one taking a "show the budget" flag,
/// is the arrangement `Docs/01` §4.3 is hardest to keep — the flag is one careless call site away
/// from being wrong and nothing would fail.
///
/// **Tappable from SHIP-100**, which is where reviewing one job and placing a bid arrived, over
/// `GET /v1/fleet/jobs/{id}` and `POST /v1/jobs/{id}/bids`. It `push`es rather than `go`es, so the
/// back gesture returns to the feed where it was rather than rebuilding the shell — which would
/// re-read the feed and lose both the scroll position and the provider's narrowing.
///
/// **The card does not hand the job across**, and the detail screen re-reads it. The feed's copy may
/// be minutes old, and a job that has since been awarded or cancelled is exactly the one nobody
/// should be shown a bid form for.
class _OpenJobCard extends StatelessWidget {
  const _OpenJobCard(this.job);

  final OpenJob job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    final measurements = <String>[
      if (job.weightKg case final weight?) _kilograms(weight),
      ?job.dimensions,
    ].join('  ·  ');

    final timing = <String>[
      if (_window(job.pickupWindow) case final pickup?) 'Pickup $pickup',
      if (_window(job.dropoffWindow) case final dropoff?) 'Drop-off $dropoff',
      if (dayFirstDate(job.expiresAt) case final expires?) 'Bidding closes $expires',
    ].join('  ·  ');

    return Card(
      key: Key('open-job-${job.id}'),
      margin: EdgeInsets.zero,
      child: InkWell(
        onTap: () => context.push(Routes.openJobDetailFor(job.id)),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _leg(theme, Icons.trip_origin, job.pickup, 'Pickup region not stated'),
              const SizedBox(height: 4),
              _leg(theme, Icons.place_outlined, job.dropoff, 'Drop-off not added yet'),
              if (job.goodsDescription case final goods? when goods.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(goods, style: theme.textTheme.bodyMedium),
              ],
              if (job.hasMeasurements && measurements.isNotEmpty) ...[
                const SizedBox(height: 4),
                Text(
                  measurements,
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ],
              if (job.vehicleRequirement case final requirement? when requirement.isNotEmpty) ...[
                const SizedBox(height: 8),
                _Note(icon: Icons.local_shipping_outlined, text: requirement),
              ],
              if (job.handlingNotes case final notes? when notes.isNotEmpty) ...[
                const SizedBox(height: 4),
                _Note(icon: Icons.info_outline, text: notes),
              ],
              if (timing.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(
                  timing,
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ],
              const SizedBox(height: 8),
              Row(
                children: [
                  Expanded(child: _StatusChip(job.status)),
                  // Says the card leads somewhere. A card that is tappable and does not look it is
                  // a screen most people never find.
                  Text('Review and bid', style: theme.textTheme.labelLarge),
                  Icon(Icons.chevron_right, color: theme.colorScheme.onSurfaceVariant),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _leg(ThemeData theme, IconData icon, JobRegion? region, String missing) {
    final supplied = region != null && !region.isEmpty;

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 18, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            // The suburb, the state and the postcode — the whole of what a provider is given. The
            // street line arrives after an award, and there is deliberately nothing here saying
            // one exists.
            supplied ? region.oneLine : missing,
            style: supplied
                ? theme.textTheme.bodyMedium
                : theme.textTheme.bodyMedium
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ),
      ],
    );
  }
}

/// Something the customer said about the job, beside the icon that says which kind of thing it is.
class _Note extends StatelessWidget {
  const _Note({required this.icon, required this.text});

  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 16, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 8),
        Expanded(child: Text(text, style: theme.textTheme.bodySmall)),
      ],
    );
  }
}

/// Whether this job already has bids on it.
///
/// `Docs/02` §1 keeps a negotiating job open to eligible bids, so this is information rather than a
/// warning: a provider pricing against company should know they are.
class _StatusChip extends StatelessWidget {
  const _StatusChip(this.status);

  final JobStatus status;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Align(
      alignment: Alignment.centerLeft,
      child: Container(
        key: Key('open-job-status-${status.wireName}'),
        padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
        decoration: BoxDecoration(
          color: theme.colorScheme.secondaryContainer,
          borderRadius: BorderRadius.circular(999),
        ),
        child: Text(
          // The exact name from Docs/02 §1 (CLAUDE.md), because a provider reading "Negotiating"
          // in the app and support reading it in an audit entry have to be reading about the same
          // thing.
          status.label,
          style: theme.textTheme.labelSmall?.copyWith(color: theme.colorScheme.onSecondaryContainer),
        ),
      ),
    );
  }
}

/// What a provider who is eligible for nothing sees.
///
/// An empty state and never a spinner or an error, which is the half of this ticket easiest to get
/// wrong: `data` is `[]` rather than `null` precisely so this case is ordinary, and a client that
/// treated it as "still loading" would leave every new provider watching a spinner for ever.
///
/// **It names what a provider can do about it.** The platform answers four situations with the
/// same empty page and will not say which — an unverified account, no declared service area, no
/// vehicle in service, or genuinely no open work — so the screen lists them rather than pretending
/// to know. Only the fleet is reachable from here today: declaring a service area is SHIP-79's
/// endpoint and has no screen yet, and verification is `Docs/04`'s own journey.
class _NothingEligible extends StatelessWidget {
  const _NothingEligible();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('open-jobs-empty'),
      padding: const EdgeInsets.symmetric(vertical: 32),
      child: Column(
        children: [
          Icon(Icons.work_outline, size: 48, color: theme.colorScheme.outline),
          const SizedBox(height: 16),
          Text(
            'No work for you to bid on right now',
            style: theme.textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 8),
          Text(
            'Jobs appear here when they are picked up in your service area, a vehicle you have in '
            'service can carry them, and your account is verified. Check back, or check that all '
            'three are in place.',
            style: theme.textTheme.bodyMedium,
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

/// What a provider sees when their own narrowing hides everything that has been read.
///
/// **Deliberately not the empty state.** "There is no work for you" and "there is none in the
/// states you picked, out of the jobs read so far" are different things to be told, and the second
/// has two answers: widen the filter, or read further. Both are offered.
class _NoneMatch extends StatelessWidget {
  const _NoneMatch({required this.state, required this.onClear});

  final OpenJobsState state;
  final VoidCallback onClear;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('open-jobs-none-match'),
      padding: const EdgeInsets.symmetric(vertical: 32),
      child: Column(
        children: [
          Icon(Icons.filter_alt_off_outlined, size: 40, color: theme.colorScheme.outline),
          const SizedBox(height: 12),
          Text(
            'Nothing matches what you picked',
            style: theme.textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 8),
          Text(
            state.hasMore
                // The case a client-side filter over a paged list has to be honest about: the
                // narrowing applies to what was read, and there is more that has not been.
                ? 'None of the ${state.jobs.length} jobs read so far match. There are more to '
                    'read — load them, or show everything.'
                : 'None of the ${state.jobs.length} jobs you can bid on match.',
            style: theme.textTheme.bodyMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('open-jobs-show-all'),
            onPressed: onClear,
            // Deliberately worded differently from the "Show all" beside the chips. They do the
            // same thing, and two identical labels on one screen is how a test — and a person —
            // taps the wrong one.
            child: const Text('Show everything again'),
          ),
        ],
      ),
    );
  }
}

/// What a provider sees when the feed could not be read at all.
///
/// Kept apart from the empty state on purpose. "There is no work for you" and "we could not find
/// out" are different things to be told, and only one of them has a retry.
class _CouldNotLoad extends StatelessWidget {
  const _CouldNotLoad({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('open-jobs-failed'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          FailureBanner(failure),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('open-jobs-retry'),
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

  final OpenJobsState state;
  final Future<void> Function() onPressed;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      children: [
        Text(
          // Said out loud, because both the list and the filter chips above are built from what
          // has been read. A screen that showed four jobs and two states when there are nine and
          // four has lied quietly, and this is the sentence that stops it.
          'Showing the most recently published jobs you can bid on.',
          key: const Key('open-jobs-partial'),
          style: theme.textTheme.bodySmall,
          textAlign: TextAlign.center,
        ),
        const SizedBox(height: 8),
        OutlinedButton(
          key: const Key('open-jobs-more'),
          onPressed: state.loadingMore ? null : () => unawaited(onPressed()),
          child: state.loadingMore
              ? const SizedBox(
                  height: 20,
                  width: 20,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text('Show more jobs'),
        ),
      ],
    );
  }
}

/// Kilograms, without a decimal point nobody typed.
String _kilograms(double value) =>
    value == value.roundToDouble() ? '${value.toInt()} kg' : '$value kg';

/// A window as a person reads it — `25 Aug 2026`, or `25 Aug 2026 to 27 Aug 2026`.
///
/// Day-first with the month spelled, because `CLAUDE.md` fixes the convention and 03/04 is a
/// different day to two different readers. Either end may be absent: a customer who only cared
/// about the deadline states one, and a window with neither is not sent at all.
String? _window(JobTimeWindow? window) {
  if (window == null) return null;

  final start = dayFirstDate(window.start);
  final end = dayFirstDate(window.end);

  return switch ((start, end)) {
    (final from?, final to?) when from == to => from,
    (final from?, final to?) => '$from to $to',
    (final from?, null) => 'from $from',
    (null, final to?) => 'by $to',
    _ => null,
  };
}
