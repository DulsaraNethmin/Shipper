import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/delivery/delivery_tracking.dart';
import 'package:shipper/features/delivery/proof_exception_reason.dart';
import 'package:shipper/features/delivery/tracking_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// How one delivery is going, to the customer who owns it (SHIP-133).
///
/// Reads the three-endpoint shelf SHIP-115 and SHIP-115a built:
/// `GET /v1/jobs/{id}/delivery/detail`, `…/milestones` and `…/proof`.
///
/// ## The two things the *Done when* names, and what each one actually is
///
/// **The latest confirmed milestone.** Confirmed is structural rather than a field to check: this is
/// the platform's own record and every row on it carries `accepted_at`, so there is nothing to
/// filter. The contrast is worth naming because both halves of this app now show milestones and they
/// mean different things — `DeliveryScreen` is the *provider's* side and shows what has **not** been
/// confirmed, marked pending in a word, an icon and a sentence, out of SHIP-124's durable queue on
/// that one handset. A customer sees only what the platform holds.
///
/// **Proof of delivery.** `Docs/01` §4.4 makes a photograph and a recorded exception reason the same
/// feature — delivered requires one or the other and never neither — so this screen renders both
/// shapes and branches on `exception_reason`, never on a missing `download_url`. Reading the absence
/// of a *credential* as a fact about the delivery is a bug waiting for a link to expire.
///
/// ## The photograph's URL is a credential and this screen treats it as one
///
/// It is signed for this caller after the read checked who they are, and anybody holding it can
/// fetch the image until it expires. So it is **rendered and never stored, logged or passed on**;
/// nothing here writes it down, and `Image.network`'s in-memory image cache is where it goes and
/// stops. When one expires the screen says which of the two things happened — the link ran out, or
/// the photograph could not be fetched — and offers the refresh that mints a new one, because a
/// customer looking at a grey rectangle has no way to tell those apart.
///
/// ## It decides nothing, including what the job's status is
///
/// A milestone may deliberately move nothing (SHIP-112), and the contract says the job's status is
/// not in this response. The status a customer reads is on `JobDetailScreen`, from `GET /v1/jobs/{id}`
/// — one place, which is the arrangement that keeps the two screens from disagreeing.
///
/// ## What it cannot show, and that is a gap in the platform
///
/// **The recipient's name and the delivery note.** `Docs/01` §4.4 requires a delivered job to carry
/// both alongside its proof and `Docs/02` §3 repeats it — and **no column holds either**. `Docs/11`
/// §4 carries SHIP-118 as partly done for exactly this, and SHIP-123 is the ticket. Nothing here
/// pretends otherwise: the fields are not modelled, not drawn, and not implied.
class CustomerTrackingScreen extends ConsumerWidget {
  const CustomerTrackingScreen({required this.jobId, super.key});

  /// The delivery to show, from the route.
  final String jobId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(trackingProvider(jobId));
    final controller = ref.read(trackingProvider(jobId).notifier);

    return Scaffold(
      appBar: AppBar(title: const Text('Tracking')),
      body: SafeArea(
        child: RefreshIndicator(
          onRefresh: controller.refresh,
          child: ListView(
            key: const Key('tracking'),
            // Always scrollable, so the pull works on the empty state and on the failure state as
            // well — and the pull is what re-signs an expired photograph link, so it has to work
            // exactly when the screen has least on it.
            physics: const AlwaysScrollableScrollPhysics(),
            padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
            children: _children(theme, state, controller),
          ),
        ),
      ),
    );
  }

  List<Widget> _children(
    ThemeData theme,
    TrackingState state,
    TrackingController controller,
  ) {
    final failure = state.failure;

    if (state.isFirstLoad) {
      return const <Widget>[
        Padding(
          padding: EdgeInsets.symmetric(vertical: 48),
          child: Center(child: CircularProgressIndicator(key: Key('tracking-loading'))),
        ),
      ];
    }

    if (state.failedOutright && failure != null) {
      return <Widget>[_CouldNotLoad(failure: failure, onRetry: controller.retry)];
    }

    return <Widget>[
      // A failure with the delivery already on screen is a banner above it, not a replacement for
      // it. Somebody whose refresh failed should still be looking at their proof of delivery.
      if (failure != null) ...[
        FailureBanner(failure),
        const SizedBox(height: 16),
      ],

      _Latest(state.latest),
      const SizedBox(height: 24),

      if (state.deliveryProof case final proof?) ...[
        _Section(title: 'Proof of delivery', child: _Evidence(proof, key: const Key('tracking-proof'))),
      ] else if (state.nothingRecorded) ...[
        // Said rather than left blank. An empty screen on a job published this morning reads as a
        // screen that failed to load.
        const _NothingYet(),
      ],

      if (state.otherProof.isNotEmpty)
        _Section(
          title: 'Other photographs',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              for (final record in state.otherProof) ...[
                _Evidence(record, key: Key('tracking-evidence-${record.id}')),
                const SizedBox(height: 16),
              ],
            ],
          ),
        ),

      _Section(title: 'Driver', child: _Driver(state.driver)),

      if (state.milestones.isNotEmpty)
        _Section(
          title: 'Everything recorded',
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              for (final milestone in state.milestones) _MilestoneRow(milestone),
              if (state.hasMore) ...[
                const SizedBox(height: 8),
                _ShowMore(state: state, onPressed: controller.loadMore),
              ],
            ],
          ),
        ),
    ];
  }
}

/// The most recent milestone the platform holds — the thing this screen leads with.
class _Latest extends StatelessWidget {
  const _Latest(this.milestone);

  final RecordedMilestone? milestone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final latest = milestone;

    if (latest == null) {
      return Column(
        key: const Key('tracking-latest-none'),
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Latest update', style: theme.textTheme.labelMedium),
          const SizedBox(height: 4),
          Text(
            'Nothing recorded yet',
            style: theme.textTheme.headlineSmall
                ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      );
    }

    final at = dayFirstDateTime(latest.recordedAt);
    final attribution = latest.recordedBy?.attribution;

    return Column(
      // Keyed by the wire form rather than the label: a test naming `tracking-latest-picked_up` is
      // naming the contract, and the label is copy that may be reworded.
      key: Key('tracking-latest-${latest.milestone}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Latest update', style: theme.textTheme.labelMedium),
        const SizedBox(height: 4),
        Text(
          // `Docs/02` §1's own name for the step (`CLAUDE.md`), so a customer reading it here and
          // support reading it in an audit entry are reading about the same thing.
          latest.label,
          style: theme.textTheme.headlineSmall?.copyWith(color: theme.colorScheme.primary),
        ),
        if (at != null)
          Text(
            // **The actor's clock, day-first with the month spelled.** `Docs/02` §3.1 keeps the two
            // clocks apart and this is the one that says when the thing happened; `accepted_at` is
            // when Shipper received it, which is support's question rather than a customer's.
            at,
            style: theme.textTheme.bodyMedium,
          ),
        if (attribution != null)
          Text(
            attribution,
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        if (latest.reason case final reason? when reason.isNotEmpty) ...[
          const SizedBox(height: 8),
          Text(reason, style: theme.textTheme.bodyMedium),
        ],
      ],
    );
  }
}

/// One piece of evidence: a photograph, or the recorded reason there is none.
///
/// **Branches on `exception_reason`**, which is the contract's own instruction. Branching on a
/// missing `download_url` would work today and would be reading the absence of a credential as a
/// fact about the delivery.
class _Evidence extends StatelessWidget {
  const _Evidence(this.proof, {super.key});

  final DeliveryProof proof;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final at = dayFirstDateTime(proof.recordedAt);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (proof.isException)
          _Exception(proof.exceptionReason!)
        else if (proof.downloadUrl case final url?)
          _Photograph(url)
        else
          // Neither a photograph nor a reason. `Docs/01` §4.4 and `000605`'s deferred constraint
          // trigger between them make this unwritable, so it is the old-build case rather than an
          // ordinary one — a later build sending a third shape this one cannot name. Said plainly,
          // because a customer with a blank space where their proof should be will telephone.
          Text(
            'Shipper has a record for this step that this version of the app cannot show. '
            'Update the app, or ask Shipper support.',
            key: const Key('tracking-evidence-unreadable'),
            style: theme.textTheme.bodyMedium,
          ),
        if (at != null) ...[
          const SizedBox(height: 8),
          Text(
            '${proof.label} · $at',
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      ],
    );
  }
}

/// The photograph itself.
///
/// `Image.network` over a URL signed for this caller. Two things it is careful about, and both are
/// cases a customer meets and a developer does not:
///
/// - **A link that has expired**, which is the ordinary outcome of leaving the screen open. It is
///   short-lived by configuration and nothing can revoke one, so expiry is the mechanism working.
/// - **A store that could not be reached.** Indistinguishable from the above from here, which is why
///   the copy names both and the answer to each is the same pull-to-refresh.
///
/// Nothing writes the URL down. The bytes live in Flutter's in-memory image cache for as long as the
/// screen does, which is the only place a credential-bearing URL is allowed to go.
class _Photograph extends StatelessWidget {
  const _Photograph(this.url);

  final String url;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return ClipRRect(
      borderRadius: BorderRadius.circular(12),
      child: Image.network(
        url,
        key: const Key('tracking-photograph'),
        fit: BoxFit.cover,
        width: double.infinity,
        loadingBuilder: (context, child, progress) {
          if (progress == null) return child;
          return const SizedBox(
            height: 200,
            child: Center(child: CircularProgressIndicator(key: Key('tracking-photograph-loading'))),
          );
        },
        errorBuilder: (context, error, stack) => Container(
          key: const Key('tracking-photograph-failed'),
          padding: const EdgeInsets.all(24),
          color: theme.colorScheme.surfaceContainerHighest,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Icon(Icons.image_not_supported_outlined, color: theme.colorScheme.onSurfaceVariant),
              const SizedBox(height: 12),
              Text(
                // Names both causes rather than guessing between them, and gives the one action
                // that fixes either. **Not "something went wrong"**: the photograph is safe and the
                // link to it is not, and saying which is the difference between a pull and a
                // telephone call.
                'This photograph could not be shown. The link to it is short-lived for your '
                'privacy and may have run out. Pull down to fetch it again.',
                style: theme.textTheme.bodyMedium,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The recorded reason a milestone carries no photograph.
///
/// `Docs/01` §4.4's three, and this is the customer's side of SHIP-116 and SHIP-131. **An exception
/// is evidence rather than the absence of it** — a reason chosen from a closed list, recorded in the
/// same transaction and the same table as the photographs — and the copy says so, because a customer
/// who read "no photo" as "nothing was recorded" would raise a dispute over a delivery that went
/// perfectly well.
class _Exception extends StatelessWidget {
  const _Exception(this.reason);

  final ProofExceptionReason reason;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      key: Key('tracking-exception-${reason.wireName}'),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer,
        borderRadius: BorderRadius.circular(12),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.no_photography_outlined, size: 18, color: theme.colorScheme.onSecondaryContainer),
              const SizedBox(width: 8),
              Expanded(
                child: Text(
                  'No photograph, and Shipper recorded why',
                  style: theme.textTheme.titleSmall
                      ?.copyWith(color: theme.colorScheme.onSecondaryContainer),
                ),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            reason.customerExplanation,
            style: theme.textTheme.bodyMedium
                ?.copyWith(color: theme.colorScheme.onSecondaryContainer),
          ),
        ],
      ),
    );
  }
}

/// Who is carrying the delivery, or that nobody is yet.
///
/// **Branches on `driver_assigned` and never on a missing name**, which is what the contract asks
/// for: a job nobody has been put on answers `200` with the flag `false` and no other field, and
/// that is a complete answer rather than a missing one.
///
/// The driver's mobile number is not here and is not modelled: the platform sends it to the provider
/// and blanks it for the customer, so this screen could never receive one. See [DeliveryDriver].
class _Driver extends StatelessWidget {
  const _Driver(this.driver);

  final DeliveryDriver? driver;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final assignment = driver;

    if (assignment == null || !assignment.driverAssigned) {
      return Text(
        'Nobody is on this delivery yet.',
        key: const Key('tracking-no-driver'),
        style: theme.textTheme.bodyMedium?.copyWith(color: theme.colorScheme.onSurfaceVariant),
      );
    }

    final since = dayFirstDate(assignment.assignedAt);

    return Column(
      key: const Key('tracking-driver'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          assignment.driverName ?? 'A driver has been assigned',
          style: theme.textTheme.bodyLarge,
        ),
        if (since != null)
          Text(
            'On this delivery since $since',
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
      ],
    );
  }
}

/// One row of the recorded history.
///
/// **Every milestone anybody recorded**, including the ones that moved nothing: a second
/// `en_route_to_pickup` after a failed pickup attempt is a real thing that happened and this is the
/// only place a customer can see it. It is deliberately not deduplicated — `Docs/02` §5 has the
/// repeat as the correct record, and a screen that collapsed two into one would be hiding the
/// afternoon a driver spent going back.
class _MilestoneRow extends StatelessWidget {
  const _MilestoneRow(this.milestone);

  final RecordedMilestone milestone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final at = dayFirstDateTime(milestone.recordedAt);
    final attribution = milestone.recordedBy?.attribution;

    return Padding(
      key: Key('tracking-milestone-${milestone.id}'),
      padding: const EdgeInsets.only(bottom: 12),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.circle_outlined, size: 14, color: theme.colorScheme.onSurfaceVariant),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(milestone.label, style: theme.textTheme.bodyLarge),
                if (at != null)
                  Text(
                    at,
                    style: theme.textTheme.bodySmall
                        ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                  ),
                if (attribution != null)
                  Text(
                    attribution,
                    style: theme.textTheme.bodySmall
                        ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                  ),
                if (milestone.reason case final reason? when reason.isNotEmpty)
                  Text(reason, style: theme.textTheme.bodySmall),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// What a customer sees on a delivery nothing has been recorded on.
///
/// **The ordinary state of every job before a provider does anything**, and of every draft: the read
/// shelf makes the job's owner a party whatever the status, so this screen answers rather than
/// refusing. It is an empty state and never a spinner or an error.
class _NothingYet extends StatelessWidget {
  const _NothingYet();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('tracking-nothing-yet'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          Icon(Icons.local_shipping_outlined, size: 48, color: theme.colorScheme.outline),
          const SizedBox(height: 16),
          Text(
            'Nothing has been recorded on this delivery yet',
            style: theme.textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 8),
          Text(
            'Once a provider has been awarded the job and a driver starts work, every step they '
            'record appears here — with the photograph taken at delivery.',
            style: theme.textTheme.bodyMedium,
            textAlign: TextAlign.center,
          ),
        ],
      ),
    );
  }
}

/// What a customer sees when the delivery could not be read at all.
///
/// One shape for every reason, deliberately. A job belonging to somebody else answers `404`
/// byte-identically to a job that does not exist, and the client must not try to be more specific
/// than the platform was — the indistinguishability is the privacy control.
class _CouldNotLoad extends StatelessWidget {
  const _CouldNotLoad({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('tracking-failed'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          FailureBanner(failure),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('tracking-retry'),
            onPressed: () => unawaited(onRetry()),
            child: const Text('Try again'),
          ),
        ],
      ),
    );
  }
}

/// The next page of recorded milestones, asked for rather than swallowed.
class _ShowMore extends StatelessWidget {
  const _ShowMore({required this.state, required this.onPressed});

  final TrackingState state;
  final Future<void> Function() onPressed;

  @override
  Widget build(BuildContext context) {
    return Align(
      alignment: Alignment.centerLeft,
      child: OutlinedButton(
        key: const Key('tracking-more'),
        onPressed: state.loadingMore ? null : () => unawaited(onPressed()),
        child: state.loadingMore
            ? const SizedBox(height: 20, width: 20, child: CircularProgressIndicator(strokeWidth: 2))
            : const Text('Show earlier updates'),
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
