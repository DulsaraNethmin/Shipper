import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/open_job.dart';
import 'package:shipper/features/jobs/open_job_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// One job in full, as a provider deciding whether to bid sees it — and the offer they make on it
/// (SHIP-100).
///
/// Reached by tapping a card in the feed, or by a deep link. The route carries the id and nothing
/// else: the job is re-read from `GET /v1/fleet/jobs/{id}` rather than handed across, so a screen
/// opened from a list fetched ten minutes ago shows what the platform holds now.
///
/// ## The bid panel is supplied rather than imported, and that is `Docs/07` §2
///
/// Reviewing a job and bidding on it is one thing a provider does and **two features' work**:
/// `Docs/07` §2 puts discovery and detail in `jobs` and bids in `bidding`, and features do not
/// import one another. So this screen declares what it needs — a widget to put under the job — and
/// `core/routing/app_router.dart` supplies `PlaceBidPanel`, being the one place that may know about
/// both. The Go side calls this the composition root and puts it in `cmd/api`; here it is the
/// router, and the rule is the same rule.
///
/// [bidPanel] is therefore **not optional**. A job detail screen with no way to bid on it would be
/// half a ticket, and a nullable parameter is how the half that is missing stops being obvious.
///
/// ## No budget, in any form, and no affordance implying one
///
/// `Docs/01` §4.3 keeps the customer's maximum private from providers — not as an amount, not as a
/// band, and **not as a "budget supplied" flag**. [OpenJob] has no field it could be in, and this
/// screen must never grow a placeholder, an empty row, or a "budget on request" line.
/// `budget_stays_on_the_customer_side_test.dart` fails if this file names it at all, and holds the
/// decoded shape to a closed key set so that a budget arriving under any other name fails too.
///
/// **What is missing is a street line and a coordinate as well**, and neither is an oversight: a
/// provider prices on the locality, and `jobs` geocodes the whole address, so a pickup coordinate
/// *is* the street line written as two numbers. The exact address is the awarded provider's, at a
/// different moment.
///
/// ## Nothing here decides whether this provider may bid
///
/// [ProviderOnly] hides the surface from a customer, which is a presentation decision and never an
/// authorisation one — the platform answers `404` to anybody the eligibility filter does not admit,
/// byte-identically to a job that does not exist. What that means concretely is that this screen has
/// **one** failure state for every reason and does not try to be more specific than the platform
/// was.
class OpenJobScreen extends StatelessWidget {
  const OpenJobScreen({required this.jobId, required this.bidPanel, super.key});

  /// The job to show, from the route.
  final String jobId;

  /// The offer form, built by the router from `features/bidding`. See the note above.
  final Widget bidPanel;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Job')),
      body: SafeArea(
        child: ProviderOnly(child: _OpenJob(jobId: jobId, bidPanel: bidPanel)),
      ),
    );
  }
}

class _OpenJob extends ConsumerWidget {
  const _OpenJob({required this.jobId, required this.bidPanel});

  final String jobId;
  final Widget bidPanel;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(openJobProvider(jobId));
    final controller = ref.read(openJobProvider(jobId).notifier);

    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        key: const Key('open-job-detail'),
        // Always scrollable, so the pull works on the failure state as well as on a job with
        // almost nothing stated about it.
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: _children(theme, state, controller),
      ),
    );
  }

  List<Widget> _children(ThemeData theme, OpenJobState state, OpenJobController controller) {
    final job = state.job;
    final failure = state.failure;

    if (state.isFirstLoad) {
      return const <Widget>[
        Padding(
          padding: EdgeInsets.symmetric(vertical: 48),
          child: Center(child: CircularProgressIndicator(key: Key('open-job-loading'))),
        ),
      ];
    }

    if (job == null) {
      return <Widget>[
        if (failure != null)
          _CouldNotLoad(failure: failure, notFound: state.notFound, onRetry: controller.retry),
      ];
    }

    return <Widget>[
      _Leg(theme: theme, icon: Icons.trip_origin, label: 'Collect from', region: job.pickup),
      const SizedBox(height: 8),
      _Leg(theme: theme, icon: Icons.place_outlined, label: 'Deliver to', region: job.dropoff),
      const SizedBox(height: 8),
      Text(
        // The whole of what a provider is given about where the job goes. Said out loud, because a
        // provider looking for a street number needs to know it is withheld rather than missing.
        'Shipper gives you the suburb and the state while you are bidding. The exact address goes '
        'to whoever is awarded the job.',
        key: const Key('open-job-address-note'),
        style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
      ),
      const SizedBox(height: 16),

      // A failure with the job already on screen is a banner above it, not a replacement for it.
      // Somebody who pulled to refresh at a loading dock should still be looking at the job.
      if (failure != null) ...[
        FailureBanner(failure),
        const SizedBox(height: 16),
      ],

      _Section(
        title: 'What is being moved',
        child: _Goods(job: job),
      ),

      _Section(
        title: 'When the customer wants it',
        child: _Timing(job: job),
      ),

      _Section(
        title: 'Bidding',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _StatusLine(status: job.status),
            if (dayFirstDateTime(job.expiresAt) case final closes?) ...[
              const SizedBox(height: 4),
              Text(
                'Bidding closes $closes',
                key: const Key('open-job-expires'),
                style: theme.textTheme.bodyMedium,
              ),
            ],
          ],
        ),
      ),

      // The other feature's half of this ticket, handed in by the router.
      bidPanel,
    ];
  }
}

/// One end of the trip, at the grain a provider is given it.
class _Leg extends StatelessWidget {
  const _Leg({
    required this.theme,
    required this.icon,
    required this.label,
    required this.region,
  });

  final ThemeData theme;
  final IconData icon;
  final String label;
  final JobRegion? region;

  @override
  Widget build(BuildContext context) {
    final supplied = region != null && !region!.isEmpty;

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 20, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 8),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(label, style: theme.textTheme.labelMedium),
              Text(
                supplied ? region!.oneLine : 'Not stated yet',
                style: supplied
                    ? theme.textTheme.titleMedium
                    : theme.textTheme.titleMedium
                        ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
            ],
          ),
        ),
      ],
    );
  }
}

/// What the customer said about the goods.
///
/// A job published with two addresses and nothing else is **ordinary rather than incomplete** — the
/// wizard collects the addresses first — so the empty case says so rather than showing a heading
/// with nothing under it.
class _Goods extends StatelessWidget {
  const _Goods({required this.job});

  final OpenJob job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    final described = (job.goodsDescription ?? '').isNotEmpty;
    final requirement = job.vehicleRequirement ?? '';
    final notes = job.handlingNotes ?? '';

    if (!described && !job.hasMeasurements && requirement.isEmpty && notes.isEmpty) {
      return Text(
        'The customer has not described the goods yet. Ask before you price it, or price the trip.',
        key: const Key('open-job-no-goods'),
        style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (described) Text(job.goodsDescription!, style: theme.textTheme.bodyLarge),
        if (job.weightKg case final weight?) _Fact(label: 'Weight', value: _kilograms(weight)),
        if (job.dimensions case final size?) _Fact(label: 'Size', value: size),
        if (requirement.isNotEmpty)
          _Fact(
            label: 'The customer says it needs',
            // Free text and deliberately not matched against anything. `Docs/11` §3 records that
            // `vehicle_requirement` stays free text until SHIP-79's capability vocabulary exists,
            // and a client matching it against a compiled-in list would be inventing an eligibility
            // rule the platform does not have.
            value: requirement,
          ),
        if (notes.isNotEmpty) _Fact(label: 'Handling', value: notes),
      ],
    );
  }
}

/// The customer's own flexibility, which is a window at each end and may be absent at both.
///
/// **Windows here, instants in the bid.** A customer says "any time Thursday"; a provider answers "I
/// will be there at nine". The bid panel below states the same distinction from its side.
class _Timing extends StatelessWidget {
  const _Timing({required this.job});

  final OpenJob job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    final pickup = _window(job.pickupWindow);
    final dropoff = _window(job.dropoffWindow);

    if (pickup == null && dropoff == null) {
      return Text(
        // Not a defect and not rare: the contract says jobs frequently state no window at all, and
        // a bid is not required to fall inside one even when they do.
        'The customer has not named a window. Offer the timing that suits you.',
        key: const Key('open-job-no-window'),
        style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (pickup != null) _Fact(label: 'Collection', value: pickup),
        if (dropoff != null) _Fact(label: 'Delivery', value: dropoff),
      ],
    );
  }
}

/// Whether this job already has offers on it.
///
/// `Docs/02` §1 keeps a negotiating job **open to eligible bids**, so this is information rather
/// than a warning: a provider pricing against company should know they are, and nothing about it
/// stops them bidding.
class _StatusLine extends StatelessWidget {
  const _StatusLine({required this.status});

  final JobStatus status;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Text(
      switch (status) {
        JobStatus.open => 'No offers yet.',
        JobStatus.negotiating => 'Offers have already been made. You can still bid.',
        // Everything else is a status the feed cannot serve — and a build old enough not to
        // recognise one says so rather than guessing.
        _ => status.label,
      },
      key: Key('open-job-status-${status.wireName}'),
      style: theme.textTheme.bodyMedium,
    );
  }
}

/// What a provider sees when the job could not be read.
///
/// **Two messages and one of them is deliberately vague.** A `404` here covers a job that does not
/// exist, one outside this provider's eligibility, one no longer open, and the owning customer at
/// the wrong address — the platform answers all of them identically on purpose (SHIP-83), and a
/// client that guessed which would be disclosing exactly what the status code withholds.
class _CouldNotLoad extends StatelessWidget {
  const _CouldNotLoad({
    required this.failure,
    required this.notFound,
    required this.onRetry,
  });

  final ApiFailure failure;
  final bool notFound;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    if (notFound) {
      return Padding(
        key: const Key('open-job-not-available'),
        padding: const EdgeInsets.symmetric(vertical: 32),
        child: Column(
          children: [
            Icon(Icons.search_off, size: 48, color: theme.colorScheme.outline),
            const SizedBox(height: 16),
            Text(
              'This job is not one you can bid on',
              style: theme.textTheme.titleMedium,
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 8),
            Text(
              'It may have been awarded or withdrawn, or it may be outside your service area or '
              'what your vehicles can carry. Your feed shows the work you are eligible for.',
              style: theme.textTheme.bodyMedium,
              textAlign: TextAlign.center,
            ),
          ],
        ),
      );
    }

    return Padding(
      key: const Key('open-job-failed'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          FailureBanner(failure),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('open-job-retry'),
            onPressed: () => unawaited(onRetry()),
            child: const Text('Try again'),
          ),
        ],
      ),
    );
  }
}

/// A titled block of the screen.
class _Section extends StatelessWidget {
  const _Section({required this.title, required this.child});

  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      padding: const EdgeInsets.only(bottom: 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: theme.textTheme.titleMedium?.copyWith(color: theme.colorScheme.primary),
          ),
          const SizedBox(height: 8),
          child,
        ],
      ),
    );
  }
}

/// One labelled value.
class _Fact extends StatelessWidget {
  const _Fact({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      padding: const EdgeInsets.only(top: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: theme.textTheme.labelMedium),
          Text(value, style: theme.textTheme.bodyMedium),
        ],
      ),
    );
  }
}

/// Kilograms, without a decimal point nobody typed.
String _kilograms(double value) =>
    value == value.roundToDouble() ? '${value.toInt()} kg' : '$value kg';

/// A window as a person reads it — `25 Aug 2026`, or `25 Aug 2026 to 27 Aug 2026`.
///
/// Day-first with the month spelled, because `CLAUDE.md` fixes the convention and 03/04 is a
/// different day to two different readers. Either end may be absent: a customer who only cared about
/// the deadline states one, and a window with neither is not sent at all.
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
