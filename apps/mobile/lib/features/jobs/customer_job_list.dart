import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/customer_jobs_controller.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';
import 'package:shipper/shared/formatting/money.dart';

/// The customer's own jobs, grouped by status (SHIP-76).
///
/// This is the customer half of the signed-in shell. `Docs/07` §1 requires the two halves of the
/// marketplace to be genuinely separate inside the one app, and this is the customer's: a
/// provider never reaches this widget, and no provider screen may reuse anything in this file.
///
/// ## The budget is drawn here and the widget that draws it is private
///
/// `Docs/01` §4.3 keeps the customer's maximum private from providers — not as an amount, not as
/// a band, and not as a "budget supplied" flag. Every job on this screen belongs to the person
/// looking at it, which is what makes showing it legitimate, and it is shown because comparing
/// bids against their own maximum is what the customer keeps it for.
///
/// **`_JobCard` is deliberately private and must stay that way.** A shared "job card" that took a
/// budget and a flag for whether to show it is the arrangement `Docs/01` §4.3 is hardest to keep
/// — the flag is one careless call site away from being wrong, and nothing would fail. SHIP-82's
/// provider feed writes its own card against its own response type, exactly as the platform
/// writes a second response type rather than redacting this one.
/// `budget_stays_on_the_customer_side_test.dart` fails if the budget is read anywhere else.
///
/// ## Three states a list has, and the one everybody forgets
///
/// A customer with no jobs gets `[]` from the platform and must see an **empty state** — never a
/// spinner that does not resolve, and never an error. That is the case that never occurs in
/// development, because whoever is building this has jobs.
class CustomerJobList extends ConsumerWidget {
  const CustomerJobList({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(customerJobsProvider);
    final controller = ref.read(customerJobsProvider.notifier);

    return RefreshIndicator(
      // Pull-to-refresh, half of the *Done when* line. It holds its own spinner until the read
      // finishes, which is why `refresh()` returns its future rather than swallowing it.
      onRefresh: controller.refresh,
      child: ListView(
        // The key SHIP-52 gave the customer half. "The app is showing the customer surface" is
        // still the fact the routing and sign-in tests assert, and it is still this.
        key: const Key('shell-customer'),
        // Always scrollable, so the pull works on the empty state and on the failure state as
        // well. A refresh gesture that only worked once there was something to scroll would be a
        // refresh that stopped working exactly when somebody needed it.
        physics: const AlwaysScrollableScrollPhysics(),
        // Room at the bottom for the floating action button, which would otherwise sit on top of
        // the last job.
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 96),
        children: _children(context, theme, state, controller),
      ),
    );
  }

  List<Widget> _children(
    BuildContext context,
    ThemeData theme,
    CustomerJobsState state,
    CustomerJobsController controller,
  ) {
    final failure = state.failure;

    return <Widget>[
      Text('Your deliveries', style: theme.textTheme.headlineSmall),
      const SizedBox(height: 16),

      // A failure with a list already on screen is a banner above it, not a replacement for it.
      // Somebody who pulled to refresh in a tunnel should still be looking at their jobs.
      if (failure != null && state.jobs.isNotEmpty) ...[
        FailureBanner(failure),
        const SizedBox(height: 16),
      ],

      if (state.isFirstLoad)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 48),
          child: Center(
            child: CircularProgressIndicator(key: Key('customer-jobs-loading')),
          ),
        )
      else if (state.failedOutright && failure != null)
        _CouldNotLoad(failure: failure, onRetry: controller.retry)
      else if (state.isEmpty)
        const _NoJobsYet()
      else ...[
        for (final group in state.groups) ...[
          _GroupHeading(status: group.status.label, wireName: group.status.wireName),
          const SizedBox(height: 8),
          for (final job in group.jobs) ...[
            _JobCard(job),
            const SizedBox(height: 8),
          ],
          const SizedBox(height: 16),
        ],
        if (state.hasMore) _ShowMore(state: state, onPressed: controller.loadMore),
      ],
    ];
  }
}

/// One status, as a heading over the jobs in it.
///
/// **No count beside it**, and that is honesty rather than an omission: the list is paged, so a
/// group holds the jobs that have been read and not every job in that status. A number here would
/// be right on the first page and quietly wrong on every screen that has more.
class _GroupHeading extends StatelessWidget {
  const _GroupHeading({required this.status, required this.wireName});

  final String status;
  final String wireName;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Text(
      status,
      // Keyed by the wire form rather than the label: a test naming `job-group-driver_assigned`
      // is naming the contract, and the label is copy that may be reworded.
      key: Key('job-group-$wireName'),
      style: theme.textTheme.titleMedium?.copyWith(color: theme.colorScheme.primary),
    );
  }
}

/// One job, as its owner sees it.
///
/// Private, and see the note on [CustomerJobList] for why it must stay private: it draws the
/// budget, and a shared card carrying a "show the budget" flag is how `Docs/01` §4.3 gets broken
/// without anything failing.
class _JobCard extends StatelessWidget {
  const _JobCard(this.job);

  final Job job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final budget = job.budgetCents;
    final expires = dayFirstDate(job.expiresAt);
    final created = dayFirstDate(job.createdAt);

    return Card(
      key: Key('job-${job.id}'),
      margin: EdgeInsets.zero,
      // Tapping through to the job in full (SHIP-77). `push` rather than `go`, so the back
      // gesture returns to the list where it was rather than rebuilding the shell — which would
      // re-read the list and lose the customer's scroll position.
      child: InkWell(
        onTap: () => context.push(Routes.jobDetailFor(job.id)),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              _leg(theme, Icons.trip_origin, job.pickup, 'Pickup address not added yet'),
              const SizedBox(height: 4),
              _leg(theme, Icons.place_outlined, job.dropoff, 'Drop-off address not added yet'),
              if (job.goodsDescription case final goods? when goods.isNotEmpty) ...[
                const SizedBox(height: 8),
                Text(goods, style: theme.textTheme.bodyMedium),
              ],
              const SizedBox(height: 8),
              Text(
                [
                  // `null` is no budget, which is a different thing from a budget of nothing —
                  // the platform omits the field rather than sending zero for exactly this
                  // reason.
                  if (budget != null) 'Budget ${audFromCents(budget)}',
                  if (created != null) 'Created $created',
                  if (expires != null) 'Expires $expires',
                ].join('  ·  '),
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
            ],
          ),
        ),
      ),
    );
  }

  Widget _leg(ThemeData theme, IconData icon, JobLocation? location, String missing) {
    final supplied = location != null && !location.isEmpty;

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, size: 18, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 8),
        Expanded(
          child: Text(
            supplied ? location.oneLine : missing,
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

/// What a customer with nothing sees.
///
/// An empty state and never a spinner or an error, which is the half of SHIP-76 that is easiest
/// to get wrong: `data` is `[]` rather than `null` precisely so this case is ordinary, and a
/// client that treated an empty list as "still loading" would leave every new account watching a
/// spinner for ever.
class _NoJobsYet extends StatelessWidget {
  const _NoJobsYet();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('customer-jobs-empty'),
      padding: const EdgeInsets.symmetric(vertical: 48),
      child: Column(
        children: [
          Icon(Icons.local_shipping_outlined, size: 48, color: theme.colorScheme.outline),
          const SizedBox(height: 16),
          Text(
            'You have not published a delivery yet',
            style: theme.textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 8),
          Text(
            'Publish one and verified transport providers will bid on it privately. You choose '
            'which bid to accept.',
            style: theme.textTheme.bodyMedium,
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

/// What a customer sees when the list could not be read at all.
///
/// Kept apart from the empty state on purpose. "You have no jobs" and "we could not find out" are
/// different things to be told, and only one of them has a retry.
class _CouldNotLoad extends StatelessWidget {
  const _CouldNotLoad({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('customer-jobs-failed'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          FailureBanner(failure),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('customer-jobs-retry'),
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

  final CustomerJobsState state;
  final Future<void> Function() onPressed;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      children: [
        Text(
          // Said out loud, because the groups above are grouped from what has been read. A
          // heading with three drafts under it when there are five is a screen that has lied
          // quietly, and this is the sentence that stops it.
          'Showing your most recent jobs.',
          key: const Key('customer-jobs-partial'),
          style: theme.textTheme.bodySmall,
        ),
        const SizedBox(height: 8),
        OutlinedButton(
          key: const Key('customer-jobs-more'),
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
