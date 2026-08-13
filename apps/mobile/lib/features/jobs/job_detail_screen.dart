import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_actions.dart';
import 'package:shipper/features/jobs/job_detail_controller.dart';
import 'package:shipper/features/jobs/job_status.dart';
import 'package:shipper/features/jobs/job_timeline.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';
import 'package:shipper/shared/formatting/money.dart';

/// One delivery, in full, to the customer who owns it (SHIP-77).
///
/// Reached by tapping a job in the list (SHIP-76). The route carries the id and nothing else: the
/// job is re-read from `GET /v1/jobs/{id}` rather than handed across, so a screen opened from a
/// list page fetched ten minutes ago shows what the platform holds now, and a deep link from a
/// notification (SHIP-145) reaches exactly the same screen with nothing extra to supply.
///
/// ## The budget is drawn here, and this is a customer-only screen
///
/// `Docs/01` §4.3 keeps the customer's maximum private from providers — not as an amount, not as
/// a band, not as a "budget supplied" flag. The job on this screen belongs to the person looking
/// at it: `GET /v1/jobs/{id}` is owner-only and answers `404` to everybody else, byte-identically
/// to a job that does not exist.
///
/// **SHIP-83's provider job detail is a different screen reading a different type**, exactly as
/// the platform writes a second response shape rather than redacting this one. Nothing in this
/// file may be lifted into a shared widget that takes a "show the budget" flag — that flag is one
/// careless call site away from wrong and nothing would fail.
/// `budget_stays_on_the_customer_side_test.dart` is what holds that.
///
/// ## Three things it shows, and one it deliberately does not
///
/// The full job, a status timeline, and the actions the platform would permit. What it does not
/// show is *when* each earlier status was reached — see `job_timeline.dart`, which explains that
/// the per-transition history exists in the database and is served by no endpoint.
class JobDetailScreen extends ConsumerWidget {
  const JobDetailScreen({required this.jobId, super.key});

  /// The job to show, from the route.
  final String jobId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(jobDetailProvider(jobId));
    final controller = ref.read(jobDetailProvider(jobId).notifier);

    return Scaffold(
      appBar: AppBar(title: const Text('Delivery')),
      body: SafeArea(
        child: RefreshIndicator(
          onRefresh: controller.refresh,
          child: ListView(
            key: const Key('job-detail'),
            // Always scrollable, so the pull works on the failure state as well as on a short
            // job — a refresh gesture that only worked once there was something to scroll would
            // stop working exactly when somebody needed it.
            physics: const AlwaysScrollableScrollPhysics(),
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
            children: _children(context, theme, state, controller),
          ),
        ),
      ),
    );
  }

  List<Widget> _children(
    BuildContext context,
    ThemeData theme,
    JobDetailState state,
    JobDetailController controller,
  ) {
    final job = state.job;
    final failure = state.failure;

    if (state.isFirstLoad) {
      return const <Widget>[
        Padding(
          padding: EdgeInsets.symmetric(vertical: 48),
          child: Center(child: CircularProgressIndicator(key: Key('job-detail-loading'))),
        ),
      ];
    }

    if (job == null) {
      return <Widget>[
        if (failure != null) _CouldNotLoad(failure: failure, onRetry: controller.retry),
      ];
    }

    return <Widget>[
      _StatusHeading(job.status),
      const SizedBox(height: 16),

      // A failure with the job already on screen is a banner above it, not a replacement for
      // it. That covers a refresh that failed and a refused action alike: in both cases the
      // customer should still be looking at their delivery.
      if (failure != null) ...[
        FailureBanner(failure),
        const SizedBox(height: 16),
      ],

      _Section(
        title: 'Route',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _Leg(
              icon: Icons.trip_origin,
              heading: 'Pickup',
              location: job.pickup,
              missing: 'Not added yet',
            ),
            const SizedBox(height: 12),
            _Leg(
              icon: Icons.place_outlined,
              heading: 'Drop-off',
              location: job.dropoff,
              missing: 'Not added yet',
            ),
          ],
        ),
      ),

      if (_hasGoods(job))
        _Section(
          title: 'Goods',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (job.goodsDescription case final goods? when goods.isNotEmpty)
                _Fact(label: 'Description', value: goods),
              if (_dimensions(job) case final size?) _Fact(label: 'Size', value: size),
              if (job.weightKg case final weight?) _Fact(label: 'Weight', value: '$weight kg'),
              if (job.vehicleRequirement case final vehicle? when vehicle.isNotEmpty)
                _Fact(label: 'Vehicle', value: vehicle),
              if (job.handlingNotes case final notes? when notes.isNotEmpty)
                _Fact(label: 'Handling', value: notes),
            ],
          ),
        ),

      if (_window(job.pickupWindow) != null || _window(job.dropoffWindow) != null)
        _Section(
          title: 'Timing',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (_window(job.pickupWindow) case final window?)
                _Fact(label: 'Pickup', value: window),
              if (_window(job.dropoffWindow) case final window?)
                _Fact(label: 'Drop-off', value: window),
            ],
          ),
        ),

      _Section(
        title: 'Your details',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            // `null` is no budget, which is a different thing from a budget of nothing — the
            // platform omits the field rather than sending zero for exactly this reason. Shown
            // here because comparing bids against their own maximum is what a customer keeps it
            // for, and because every job on this screen is theirs.
            if (job.budgetCents case final budget?)
              _Fact(label: 'Your budget', value: audFromCents(budget))
            else
              const _Fact(
                label: 'Your budget',
                value: 'Not set. Providers never see it either way.',
              ),
            if (dayFirstDate(job.createdAt) case final created?)
              _Fact(label: 'Created', value: created),
            if (dayFirstDate(job.expiresAt) case final expires?)
              _Fact(label: 'Offer closes', value: expires),
          ],
        ),
      ),

      _Section(
        title: 'Progress',
        child: _Timeline(job),
      ),

      _Section(
        title: 'What you can do',
        child: _Actions(job: job, state: state, controller: controller),
      ),
    ];
  }
}

/// Whether anything in the goods section has been filled in.
///
/// A draft is mostly empty for most of its life and the platform omits what is empty, so a
/// section with nothing under it is the ordinary case rather than an error — and an empty heading
/// is worse than no heading.
bool _hasGoods(Job job) {
  return (job.goodsDescription?.isNotEmpty ?? false) ||
      _dimensions(job) != null ||
      job.weightKg != null ||
      (job.vehicleRequirement?.isNotEmpty ?? false) ||
      (job.handlingNotes?.isNotEmpty ?? false);
}

/// The three dimensions, in centimetres, or `null` when none was given.
///
/// Partial is legitimate: a draft may carry a length and no width. The parts that exist are
/// shown, rather than the whole thing being withheld until all three arrive.
String? _dimensions(Job job) {
  final parts = <String>[
    if (job.lengthCm case final length?) '${length}L',
    if (job.widthCm case final width?) '${width}W',
    if (job.heightCm case final height?) '${height}H',
  ];
  return parts.isEmpty ? null : '${parts.join(' × ')} cm';
}

/// A time window as a person reads one, or `null` when neither end was given.
///
/// Day-first, because 03/04 is a different day to an Australian and an American and the reader
/// cannot tell which convention a screen used. Road transport is not scheduled to the minute
/// (`Docs/02`), so the date is the useful part and the time of day is not shown.
String? _window(JobTimeWindow? window) {
  final start = dayFirstDate(window?.start);
  final end = dayFirstDate(window?.end);

  return switch ((start, end)) {
    (final String from, final String to) => from == to ? from : '$from – $to',
    (final String from, null) => 'From $from',
    (null, final String to) => 'By $to',
    _ => null,
  };
}

/// The job's status, as the thing the screen leads with.
class _StatusHeading extends StatelessWidget {
  const _StatusHeading(this.status);

  final JobStatus status;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Status', style: theme.textTheme.labelMedium),
        const SizedBox(height: 4),
        Text(
          // The exact name from `Docs/02` §1 (`CLAUDE.md`) — the words the whole product uses,
          // so a customer reading it here and support reading it in an audit entry are reading
          // about the same thing.
          status.label,
          // Keyed by the wire form rather than the label: a test naming
          // `job-detail-status-driver_assigned` is naming the contract, and the label is copy
          // that may be reworded.
          key: Key('job-detail-status-${status.wireName}'),
          style: theme.textTheme.headlineSmall?.copyWith(color: theme.colorScheme.primary),
        ),
      ],
    );
  }
}

/// The status timeline (SHIP-77).
///
/// Reads `jobTimeline`, which is where the reasoning lives. Two things this widget is careful
/// about, and both are that file's rules rendered rather than restated:
///
/// - **No tick on a step behind the current one.** `Docs/02` §2 permits skips, so "the job is
///   past this point" is true where "this happened" is not.
/// - **No date it was not given.** A step behind the current one carries no timestamp, and the
///   screen says why rather than leaving a reader to wonder.
class _Timeline extends StatelessWidget {
  const _Timeline(this.job);

  final Job job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final steps = jobTimeline(job);

    return Column(
      key: const Key('job-detail-timeline'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        for (final step in steps) _TimelineStep(step),
        const SizedBox(height: 8),
        Text(
          // Said out loud. A timeline that showed no dates and did not say why reads as a screen
          // that failed to load them.
          'Shipper records when each step happened. Showing those times in the app arrives with '
          'the tracking screens.',
          key: const Key('job-detail-timeline-note'),
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ],
    );
  }
}

/// One row of the timeline.
class _TimelineStep extends StatelessWidget {
  const _TimelineStep(this.step);

  final JobTimelineStep step;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final current = step.position == JobTimelinePosition.current;
    final ahead = step.position == JobTimelinePosition.ahead;

    final tone = current
        ? theme.colorScheme.primary
        : (ahead ? theme.colorScheme.outline : theme.colorScheme.onSurfaceVariant);

    final stamp = step.at == null ? null : dayFirstDate(step.at);

    return Padding(
      key: Key('job-detail-step-${step.status.wireName}'),
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(
            current ? Icons.radio_button_checked : Icons.circle_outlined,
            size: 16,
            color: tone,
          ),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  step.status.label,
                  style: current
                      ? theme.textTheme.bodyLarge?.copyWith(
                          color: tone,
                          fontWeight: FontWeight.w600,
                        )
                      : theme.textTheme.bodyMedium?.copyWith(color: tone),
                ),
                if (stamp != null)
                  Text(
                    '${step.atLabel} $stamp',
                    style: theme.textTheme.bodySmall?.copyWith(color: tone),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// The actions the platform would permit on this job (SHIP-77).
///
/// **Every one of them is an offer, not a decision.** `CLAUDE.md` and `Docs/07` §3 put every
/// authorisation decision on the platform; `job_actions.dart` explains at length why hiding a
/// button is allowed and deciding with it is not.
class _Actions extends StatelessWidget {
  const _Actions({required this.job, required this.state, required this.controller});

  final Job job;
  final JobDetailState state;
  final JobDetailController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final actions = actionsFor(job.status);
    final nothing = noActionsBecause(job.status);

    if (nothing != null) {
      return Text(
        nothing,
        key: const Key('job-detail-no-actions'),
        style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
      );
    }

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        for (final action in actions)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: OutlinedButton(
              key: Key(action.widgetKey),
              // Disabled while an action is in flight, which is what stops a second tap becoming
              // a second action. Two taps are two actions, and the idempotency key is per action
              // (`Docs/07` §4) — so the second would not be absorbed by the first.
              onPressed: state.acting ? null : () => _perform(context, action),
              child: state.acting
                  ? const SizedBox(
                      height: 20,
                      width: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : Text(action.label),
            ),
          ),
      ],
    );
  }

  Future<void> _perform(BuildContext context, JobAction action) async {
    switch (action) {
      case JobAction.cancel:
        // Confirmed rather than taken on one tap. Cancelling is not reversible from the app —
        // `Docs/02` §2 has no route out of `cancelled` — and a job with bids on it ends other
        // people's work as well as the customer's own.
        final confirmed = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            key: const Key('job-cancel-dialog'),
            title: const Text('Cancel this delivery?'),
            content: const Text(
              'Providers will stop being able to bid on it, and any bids already made are '
              'closed. This cannot be undone.',
            ),
            actions: [
              TextButton(
                key: const Key('job-cancel-dismiss'),
                onPressed: () => Navigator.of(context).pop(false),
                child: const Text('Keep it'),
              ),
              TextButton(
                key: const Key('job-cancel-confirm'),
                onPressed: () => Navigator.of(context).pop(true),
                child: const Text('Cancel the delivery'),
              ),
            ],
          ),
        );

        if (confirmed ?? false) await controller.cancel();
    }
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
      padding: const EdgeInsets.only(bottom: 6),
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

/// One end of the journey.
///
/// An address the platform could not place is shown as information, in ordinary colours, and
/// never as something to fix: SHIP-59a requires that a failed lookup does not fail the job, and
/// rural addresses no geocoder knows are deliveries this marketplace exists to carry.
class _Leg extends StatelessWidget {
  const _Leg({
    required this.icon,
    required this.heading,
    required this.location,
    required this.missing,
  });

  final IconData icon;
  final String heading;
  final JobLocation? location;
  final String missing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final place = location;
    final supplied = place != null && !place.isEmpty;

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 18, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 8),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(heading, style: theme.textTheme.labelMedium),
              Text(
                // The state stays as the platform sent it — the upper-case abbreviation, `NSW`,
                // which `Docs/10` §4.7 records as the one exception to lower snake case on the
                // wire because a client prints it rather than branching on it.
                supplied ? place.oneLine : missing,
                style: supplied
                    ? theme.textTheme.bodyMedium
                    : theme.textTheme.bodyMedium
                        ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
              if (supplied && !place.resolved)
                Text(
                  'Shipper could not match this to a map. The address is stored exactly as you '
                  'typed it and the delivery goes ahead.',
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
            ],
          ),
        ),
      ],
    );
  }
}

/// What a customer sees when the job could not be read at all.
///
/// One shape for every reason, deliberately. A job belonging to somebody else answers `404`
/// byte-identically to a job that does not exist, and the client must not try to be more
/// specific than the platform was — the indistinguishability is the privacy control.
class _CouldNotLoad extends StatelessWidget {
  const _CouldNotLoad({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('job-detail-failed'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          FailureBanner(failure),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('job-detail-retry'),
            onPressed: () => unawaited(onRetry()),
            child: const Text('Try again'),
          ),
        ],
      ),
    );
  }
}
