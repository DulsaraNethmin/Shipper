import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/features/delivery/milestone.dart';
import 'package:shipper/features/delivery/record_milestone_controller.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// Recording the milestones of one delivery (SHIP-129).
///
/// The provider's own screen for a job they have been awarded: three large buttons, and a list of
/// what this device has recorded with **what has and has not reached Shipper written beside each
/// one**. That second half is the ticket — `Docs/02` §3.1 requires optimistic local state to be
/// "clearly marked as pending", and an optimistic update a driver cannot tell from a confirmed one
/// is the defect this screen exists to prevent.
///
/// ## The screen shows the job's identifier and nothing else about the job, and that is a platform
/// gap rather than a design choice
///
/// There is **no endpoint that serves an awarded job to the provider delivering it.**
/// `GET /v1/jobs/{id}` is the owning customer's and answers everybody else `404`;
/// `GET /v1/jobs/open/{id}` serves a job only while it is still biddable, so it stops answering the
/// moment the provider wins it; `GET /v1/driver/jobs/{id}` is the driver's link-authenticated view
/// and its token cannot be exchanged for a session. So the addresses, the goods and the windows
/// this screen would like to show are, today, reachable by the driver and not by the provider who
/// assigned them. Recorded in `Docs/11` §3 with the tickets it touches.
///
/// What that does **not** stop is the ticket: recording a milestone needs the job's identifier and
/// the provider's own credential, and both are here.
///
/// ## Every button stays enabled, and that is `Docs/02` rather than an oversight
///
/// Recording one milestone disables nothing. `Docs/02` §3.1 puts every transition on the platform;
/// `Docs/02` §2 permits `Awarded → En route to pickup` with no assignment in between; SHIP-111
/// records a second `en_route_to_pickup` as a second row when a driver reaches a pickup, finds
/// nobody, and sets off again; and SHIP-112 **absorbs** an update that arrives after a later one
/// rather than refusing it. A screen that greyed a button out after its milestone was recorded
/// would be enforcing a sequence the platform does not have, on the device `Docs/07` §3 says may
/// not decide anything.
///
/// ## Delivered is not offered, and the reason is on the platform
///
/// `CLAUDE.md`'s invariant: delivered requires photo proof or a recorded exception reason, never
/// neither. `POST /v1/jobs/{id}/milestones` refuses every `delivered` with
/// `delivery_proof_required` until SHIP-118, and this device can capture neither a photograph
/// (SHIP-130) nor an exception (SHIP-131). A fourth button would queue an operation whose only
/// possible outcome is a quarantined row — work the driver believes they recorded, waiting for a
/// person. So the screen names the milestone and says what it is waiting for instead.
class DeliveryScreen extends ConsumerWidget {
  const DeliveryScreen({required this.jobId, super.key});

  /// The job being delivered. From the path, which is what makes this screen deep-linkable —
  /// `Docs/07` §5 requires every notification to open the exact job it concerns, and an awarded
  /// provider is told they have won by push (SHIP-145).
  final String jobId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return Scaffold(
      appBar: AppBar(title: const Text('Delivery')),
      body: ProviderOnly(child: _Recording(jobId: jobId)),
    );
  }
}

class _Recording extends ConsumerWidget {
  const _Recording({required this.jobId});

  final String jobId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(recordMilestoneProvider(jobId));
    final controller = ref.read(recordMilestoneProvider(jobId).notifier);

    return ListView(
      key: const Key('delivery-screen'),
      padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
      children: <Widget>[
        Text('Record what happened', style: theme.textTheme.headlineSmall),
        const SizedBox(height: 8),
        Text(
          // The promise `Docs/07` §4 makes, said to the person who has to trust it. A driver who
          // believes an update needs signal will stand in a yard waiting for a bar rather than
          // getting on with the job, which is the behaviour the queue exists to make unnecessary.
          'Tap as each step happens. Shipper keeps what you record on this device and sends it '
          'when there is signal — you do not have to wait for a connection.',
          style: theme.textTheme.bodyMedium,
        ),
        const SizedBox(height: 4),
        Text('Job $jobId', key: const Key('delivery-job'), style: theme.textTheme.bodySmall),
        const SizedBox(height: 20),

        if (state.refusal != null) ...[
          _Refused(message: state.refusal!, onDismiss: controller.dismissRefusal),
          const SizedBox(height: 16),
        ],

        for (final milestone in Milestone.offered) ...[
          _RecordButton(
            milestone: milestone,
            onRecord: () => unawaited(controller.record(milestone)),
          ),
          const SizedBox(height: 12),
        ],

        const SizedBox(height: 8),
        const _DeliveredWaitsForProof(),
        const SizedBox(height: 24),

        Text('What you have recorded', style: theme.textTheme.titleMedium),
        const SizedBox(height: 8),
        if (state.entries.isEmpty)
          Text(
            'Nothing yet. What you record appears here with whether Shipper has it.',
            key: const Key('delivery-nothing-recorded'),
            style: theme.textTheme.bodyMedium,
          )
        else
          for (final entry in state.entries) _EntryTile(entry: entry),
      ],
    );
  }
}

/// One milestone, as a target big enough for a gloved thumb in a loading bay.
class _RecordButton extends StatelessWidget {
  const _RecordButton({required this.milestone, required this.onRecord});

  final Milestone milestone;
  final VoidCallback onRecord;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return SizedBox(
      width: double.infinity,
      child: FilledButton(
        key: Key('milestone-record-${milestone.wire}'),
        onPressed: onRecord,
        style: FilledButton.styleFrom(
          padding: const EdgeInsets.symmetric(vertical: 16),
          alignment: Alignment.centerLeft,
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: <Widget>[
            Text(milestone.label, style: theme.textTheme.titleMedium?.copyWith(
              color: theme.colorScheme.onPrimary,
            )),
            const SizedBox(height: 2),
            Text(
              milestone.hint,
              style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onPrimary),
            ),
          ],
        ),
      ),
    );
  }
}

/// One recorded milestone, and where it has got to.
///
/// **The status is a word, an icon and a sentence — never a colour on its own.** The two states a
/// driver must never confuse are [MilestoneSync.pending] and [MilestoneSync.recorded], and they
/// differ in all three.
class _EntryTile extends StatelessWidget {
  const _EntryTile({required this.entry});

  final MilestoneEntry entry;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final settled = entry.sync == MilestoneSync.recorded;
    final wrong = entry.sync == MilestoneSync.needsAttention;

    final colour = wrong
        ? theme.colorScheme.error
        : settled
            ? theme.colorScheme.primary
            : theme.colorScheme.tertiary;

    return Card(
      key: Key('milestone-entry-${entry.operationId}'),
      margin: const EdgeInsets.only(bottom: 8),
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: <Widget>[
            Icon(_icon, color: colour),
            const SizedBox(width: 12),
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Text(entry.label, style: theme.textTheme.titleSmall),
                  const SizedBox(height: 2),
                  Text(
                    // The **actor's** clock, which is the one `Docs/02` §3.1 shows a person. The
                    // platform's `accepted_at` is what support reasons about and never appears
                    // here — a screen that rendered it would be showing a sync time as though it
                    // were a delivery event.
                    dayFirstDateTime(entry.recordedAt.toIso8601String()) ?? '',
                    style: theme.textTheme.bodySmall,
                  ),
                  const SizedBox(height: 6),
                  Text(
                    entry.sync.label,
                    key: Key('milestone-status-${entry.operationId}'),
                    style: theme.textTheme.labelLarge?.copyWith(color: colour),
                  ),
                  Text(entry.sync.detail, style: theme.textTheme.bodySmall),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }

  IconData get _icon => switch (entry.sync) {
        MilestoneSync.pending => Icons.schedule_send_outlined,
        MilestoneSync.sending => Icons.sync,
        MilestoneSync.recorded => Icons.check_circle_outline,
        MilestoneSync.needsAttention => Icons.error_outline,
      };
}

/// Why there is no Delivered button. See the note on [DeliveryScreen].
class _DeliveredWaitsForProof extends StatelessWidget {
  const _DeliveredWaitsForProof();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      key: const Key('milestone-delivered-needs-proof'),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.surfaceContainerHighest,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(Icons.photo_camera_outlined, color: theme.colorScheme.onSurfaceVariant),
          const SizedBox(width: 12),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: <Widget>[
                Text('Delivered', style: theme.textTheme.titleSmall),
                const SizedBox(height: 2),
                Text(
                  'Marking a delivery complete needs a photograph or a written reason there is '
                  'none. This version of Shipper cannot take one yet.',
                  style: theme.textTheme.bodySmall,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// The device would not store the recording. Rare, and worth saying plainly when it happens.
class _Refused extends StatelessWidget {
  const _Refused({required this.message, required this.onDismiss});

  final String message;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      key: const Key('milestone-refused'),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: <Widget>[
          Icon(Icons.error_outline, color: theme.colorScheme.onErrorContainer),
          const SizedBox(width: 12),
          Expanded(
            child: Text(
              message,
              style: theme.textTheme.bodyMedium
                  ?.copyWith(color: theme.colorScheme.onErrorContainer),
            ),
          ),
          IconButton(
            key: const Key('milestone-refused-dismiss'),
            icon: const Icon(Icons.close),
            tooltip: 'Dismiss',
            onPressed: onDismiss,
          ),
        ],
      ),
    );
  }
}
