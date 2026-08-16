import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/routing/app_router.dart';
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
/// ## Delivered is offered, and only through the camera (SHIP-130)
///
/// `CLAUDE.md`'s invariant: delivered requires photo proof or a recorded exception reason, never
/// neither, and `POST /v1/jobs/{id}/milestones` enforces it (SHIP-118). So `delivered` is not one of
/// the buttons above — a plain one would queue an operation whose only possible outcome is a
/// quarantined row — and is instead a route to `ProofCaptureScreen`, which photographs the delivery,
/// compresses it, and queues the milestone **with** its proof.
///
/// The other half of that rule is the reasoned exception, and it is on the same screen: SHIP-131
/// gave a driver whose camera is refused a way through rather than an honest dead end, and the
/// panel that offers it is `ProofCaptureScreen`'s.
///
/// ## The driver's own words go on the request, not only on the screen (SHIP-131a)
///
/// `MilestoneRecording.reason` has been in the published contract since SHIP-111 — optional, 500
/// characters, "what a person should know about this milestone that the milestone itself does not
/// say" — the platform has always stored it, and the **customer's tracking view has always rendered
/// it**. What did not exist was anywhere to type one: measured across both client trees, no client
/// sent the field at all, so a customer-facing surface could display a note nothing in the product
/// could write. The field above the buttons closes that, and it stays optional in the strong sense —
/// an empty one puts no key in the body.
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

class _Recording extends ConsumerStatefulWidget {
  const _Recording({required this.jobId});

  final String jobId;

  @override
  ConsumerState<_Recording> createState() => _RecordingState();
}

class _RecordingState extends ConsumerState<_Recording> {
  /// The driver's own words for the **next** milestone they record (SHIP-131a).
  ///
  /// Held by the screen rather than by the controller, and cleared the moment a recording commits.
  /// Both halves of that matter. A note belongs to one milestone — `MilestoneRecording.reason` is
  /// "what a person should know about *this* milestone" — so carrying it forward would attach a
  /// driver's sentence about a locked gate to the pickup that followed it, on the customer's
  /// timeline, with nothing on this screen to say it had happened.
  ///
  /// It sits **above** the buttons because the note is typed before the tap: a field under three
  /// large targets is one a driver fills in after they have already recorded the thing it was about.
  final _note = TextEditingController();

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  Future<void> _record(Milestone milestone) async {
    final recorded =
        await ref.read(recordMilestoneProvider(widget.jobId).notifier).record(
              milestone,
              note: _note.text,
            );

    // Cleared only when the row committed. A refusal leaves the words on screen, because the driver
    // is about to tap again and retyping a sentence they already wrote is the worst thing this
    // screen could ask of somebody standing in the rain.
    if (recorded && mounted) _note.clear();
  }

  @override
  Widget build(BuildContext context) {
    final jobId = widget.jobId;
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

        _Note(controller: _note),
        const SizedBox(height: 16),

        for (final milestone in Milestone.offered) ...[
          _RecordButton(
            milestone: milestone,
            onRecord: () => unawaited(_record(milestone)),
          ),
          const SizedBox(height: 12),
        ],

        const SizedBox(height: 8),
        _DeliveredNeedsAPhotograph(
          onPhotograph: () => context.push(Routes.deliveryProofFor(jobId)),
        ),
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

/// The driver's own words about whatever they record next (SHIP-131a).
///
/// **Optional, and said so in the label rather than only in the code.** `MilestoneRecording.reason`
/// has always been optional and no client has ever sent one, so the field a customer's tracking view
/// renders has until now been one nothing in the product could write. A driver who leaves this empty
/// records exactly what they recorded before: the body carries no `reason` key at all.
///
/// **It says who reads it**, because that changes what a person writes. "Shipper keeps this with the
/// delivery and the customer can see it" is the difference between a note meant for the customer and
/// a note meant for the provider's own records, and a driver who does not know which is writing
/// neither.
///
/// Capped at [milestoneNoteMaxLength] with no counter under it: the bound is the contract's, and a
/// character count under a field a driver will almost always leave blank is noise on a screen whose
/// whole job is three large buttons.
class _Note extends StatelessWidget {
  const _Note({required this.controller});

  final TextEditingController controller;

  @override
  Widget build(BuildContext context) {
    return TextField(
      key: const Key('milestone-note'),
      controller: controller,
      maxLength: milestoneNoteMaxLength,
      maxLines: 2,
      minLines: 1,
      textCapitalization: TextCapitalization.sentences,
      keyboardType: TextInputType.multiline,
      decoration: const InputDecoration(
        labelText: 'Anything to add? (optional)',
        helperText: 'Kept with the delivery. The customer can see it.',
        // The bound is the contract's and the field stops growing at it; a running count under a
        // box most drivers will leave empty is clutter.
        counterText: '',
        border: OutlineInputBorder(),
      ),
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

/// Delivered, which is the camera rather than a plain button. See the note on [DeliveryScreen].
class _DeliveredNeedsAPhotograph extends StatelessWidget {
  const _DeliveredNeedsAPhotograph({required this.onPhotograph});

  final VoidCallback onPhotograph;

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
                  'Marking a delivery complete needs a photograph of the goods with the '
                  'recipient. Shipper keeps it on this phone and sends it when there is signal.',
                  style: theme.textTheme.bodySmall,
                ),
                const SizedBox(height: 8),
                FilledButton.tonal(
                  key: const Key('milestone-record-delivered'),
                  onPressed: onPhotograph,
                  child: const Text('Photograph the delivery'),
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
