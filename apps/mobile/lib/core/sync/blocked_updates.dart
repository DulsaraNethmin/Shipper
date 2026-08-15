/// What happened to an update this phone recorded and the platform would not take (SHIP-132).
///
/// ## The rung of `Docs/02` §3.1 that is about a person rather than about a connection
///
/// SHIP-126's bar counts work waiting to sync and SHIP-127 prompts the provider to go and find
/// signal. **Neither is about this.** A refused operation is not waiting for a connection — it is
/// waiting for somebody to read what happened — and both of those files say so in their own words:
/// SHIP-126 draws a second line, *needs attention* rather than *waiting to sync*, and SHIP-127
/// excludes blocked work entirely because "telling a driver to walk up a hill about an update that
/// will still be refused when they get there is worse than saying nothing".
///
/// This is where that second line goes when it is tapped.
///
/// ## Retained and shown, never discarded
///
/// `Docs/02` §3.1 and `Docs/07` §4 both require a queued update that lost — to an administrative
/// cancellation, say (SHIP-113) — to be **kept and shown to the user** rather than dropped.
/// `OperationQueue.block` never removes a row, and `OperationQueue.acknowledge` is the only path
/// that does: it refuses anything pending or in flight by construction, so the one removal on this
/// path cannot take unsent work. **A person is the only thing that clears one.**
///
/// ## Three reasons, three different sentences, and one of them is the ticket
///
/// | Reason | What happened | What the person can do |
/// |---|---|---|
/// | `refused` | The platform saw it and would not take it — **this is the ticket's case** | Read the delivery; the platform's version is the one that counts |
/// | `unsupported` | This build cannot send it: a kind or a body version it has no name for | Update the app |
/// | `unreadable` | The row itself could not be decoded | Nothing; report it |
///
/// Collapsing them would be the easy version and the wrong one. "Lost to server state" is a
/// statement about a *decision somebody or something made*, and the other two are statements about
/// this build — telling a driver their delivery update was rejected when in fact the app cannot
/// read its own database would send them to argue with an administrator about nothing.
///
/// ## The words are here and never `BlockedOperation.detail`
///
/// `queued_operation.dart` is explicit: the detail is "free text for a support conversation. Never
/// shown as user-facing copy — a screen writes its own words for the reason, which is what keeps
/// copy out of `core/`." It is a `toString` of an `ApiFailure`, carrying a status, a code and a
/// request id — useful to whoever reads a bug report and meaningless on a loading dock.
///
/// ## A panel over the application, not a route
///
/// The same construction as [UnsyncedNudge], and for two reasons. The one that is about the
/// product: this is reached from the persistent indicator, which is mounted *beside* the navigator
/// in `MaterialApp.router`'s builder and therefore has no `Navigator` above it to push onto — a
/// route would need the indicator to move inside the navigator, which is exactly what SHIP-126
/// rejected, because then it would stop being persistent.
///
/// The one that is about ownership, said plainly because it is a real constraint rather than a
/// modelling argument: **adding a location means editing `core/routing/app_router.dart`**, and
/// that file belongs to another branch this wave. A panel needs no route and no guard entry, and a
/// route would be worth asking for only if this screen were deep-linked. It is not: nothing sends
/// a notification about a quarantined update, and SHIP-128's 24-hour operations alert is
/// server-side and addressed to operations rather than to the driver.
///
/// It is **dismissible and it does not acknowledge anything**, unlike the nudge's "Got it": closing
/// the panel leaves every update where it was and the indicator still says they need attention.
/// The only thing that removes one is its own button.
library;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/queue_watch.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// Whether the panel is open.
///
/// **In memory and deliberately not durable**, the same call [NudgeSilence] makes in the other
/// direction: a relaunch closes it, which is right, because the indicator is still on the screen
/// and still says how many updates need attention.
final class BlockedUpdatesVisibility extends Notifier<bool> {
  @override
  bool build() => false;

  void open() => state = true;
  void close() => state = false;
}

/// See [BlockedUpdatesVisibility].
final blockedUpdatesOpenProvider =
    NotifierProvider<BlockedUpdatesVisibility, bool>(BlockedUpdatesVisibility.new);

/// What one quarantined update is called, in the words a person reads.
///
/// From the `kind` column verbatim rather than from a decoded [OperationKind], because a row this
/// build cannot read is precisely one whose kind it may not have — [BlockedOperation] carries the
/// string for that reason.
String blockedUpdateName(String kindName) => switch (OperationKind.byName(kindName)) {
      OperationKind.milestone => 'A delivery update',
      OperationKind.proof => 'A proof photograph',
      // A kind a later build wrote. Naming the raw string would put `delivery.something` in front
      // of a driver; saying what is true is better.
      _ => 'An update this version of Shipper does not recognise',
    };

/// Why it is still here, in the words a person reads. See the table in the library note.
String blockedUpdateReason(QueueBlockReason reason) => switch (reason) {
      QueueBlockReason.refused =>
        'Shipper would not accept this. Something changed on the delivery before this update '
            'reached us — it may have been cancelled, or this step may already have been recorded. '
            'What Shipper holds is what counts; open the delivery to see where it stands.',
      QueueBlockReason.unsupported =>
        'This version of Shipper cannot send it. It was recorded by a newer version — updating the '
            'app will let it go.',
      QueueBlockReason.unreadable =>
        'This could not be read from the phone. Nothing you can do will send it; tell support if '
            'you need a record of it.',
    };

/// The panel, wrapping the application (SHIP-132).
///
/// Costs a test nothing, by the same mechanism as every other thing in `ShipperApp`'s builder: it
/// reads [queueSnapshotProvider], which is empty until `main.dart` supplies the running worker, so
/// a widget test that has not asked for a queue draws its child and opens no database.
class BlockedUpdates extends ConsumerWidget {
  const BlockedUpdates({required this.child, super.key});

  /// The application, drawn underneath.
  final Widget child;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (!ref.watch(blockedUpdatesOpenProvider)) return child;

    final published = ref.watch(queueSnapshotProvider);
    final blocked = switch (published) {
      AsyncData(:final value) => value.blocked,
      _ => const <BlockedOperation>[],
    };

    // The last one was acknowledged while the panel was open. Closing it rather than drawing an
    // empty panel: an empty list here means the thing the panel exists for is finished.
    if (blocked.isEmpty) {
      // Scheduled rather than assigned, because a provider must not be written during a build.
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (context.mounted) ref.read(blockedUpdatesOpenProvider.notifier).close();
      });
      return child;
    }

    return Stack(
      fit: StackFit.expand,
      children: <Widget>[
        child,
        ModalBarrier(
          key: const Key('blocked-updates-scrim'),
          // Dismissible, unlike the nudge's. The nudge interrupts something the driver has not
          // seen; this is somewhere they chose to go, and a scrim they cannot tap to leave is a
          // screen with no way out on a device whose back gesture the router owns.
          dismissible: true,
          onDismiss: ref.read(blockedUpdatesOpenProvider.notifier).close,
          color: Theme.of(context).colorScheme.scrim.withValues(alpha: 0.6),
        ),
        _Panel(
          blocked: blocked,
          onClose: ref.read(blockedUpdatesOpenProvider.notifier).close,
          onAcknowledge: (id) => ref.read(queueWatchProvider)?.acknowledge(id),
        ),
      ],
    );
  }
}

class _Panel extends StatelessWidget {
  const _Panel({
    required this.blocked,
    required this.onClose,
    required this.onAcknowledge,
  });

  final List<BlockedOperation> blocked;
  final VoidCallback onClose;
  final void Function(int id) onAcknowledge;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return SafeArea(
      child: Center(
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 520, maxHeight: 620),
            child: Material(
              key: const Key('blocked-updates'),
              color: theme.colorScheme.surface,
              borderRadius: BorderRadius.circular(12),
              child: Column(
                mainAxisSize: MainAxisSize.min,
                crossAxisAlignment: CrossAxisAlignment.start,
                children: <Widget>[
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 20, 8, 0),
                    child: Row(
                      children: <Widget>[
                        Expanded(
                          child: Text(
                            blocked.length == 1
                                ? 'An update needs your attention'
                                : '${blocked.length} updates need your attention',
                            key: const Key('blocked-updates-headline'),
                            style: theme.textTheme.titleMedium,
                          ),
                        ),
                        IconButton(
                          key: const Key('blocked-updates-close'),
                          icon: const Icon(Icons.close),
                          tooltip: 'Close',
                          onPressed: onClose,
                        ),
                      ],
                    ),
                  ),
                  Padding(
                    padding: const EdgeInsets.fromLTRB(20, 4, 20, 0),
                    child: Text(
                      // The one sentence the whole panel exists to make true: nothing was thrown
                      // away, and nothing will be until the person says so.
                      'These are still on this phone. Waiting will not send them, and none of them '
                      'will be removed until you say you have read it.',
                      key: const Key('blocked-updates-intro'),
                      style: theme.textTheme.bodyMedium,
                    ),
                  ),
                  const SizedBox(height: 8),
                  Flexible(
                    child: ListView.separated(
                      shrinkWrap: true,
                      padding: const EdgeInsets.fromLTRB(20, 8, 20, 20),
                      itemCount: blocked.length,
                      separatorBuilder: (context, index) => const Divider(height: 24),
                      itemBuilder: (context, index) => _BlockedRow(
                        operation: blocked[index],
                        onAcknowledge: () => onAcknowledge(blocked[index].id),
                      ),
                    ),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}

class _BlockedRow extends StatelessWidget {
  const _BlockedRow({required this.operation, required this.onAcknowledge});

  final BlockedOperation operation;
  final VoidCallback onAcknowledge;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    // `recordedAt` and not `enqueuedAt`: what the person is being reminded of is when they acted,
    // which is the column `Docs/02` §3.1 calls the first of the two timestamps. `enqueuedAt` is
    // what the escalation ladder measures and is a fact about the queue.
    final recorded = dayFirstDateTime(operation.recordedAt.toUtc().toIso8601String());

    return Column(
      key: Key('blocked-update-${operation.id}'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: <Widget>[
        Text(
          blockedUpdateName(operation.kindName),
          key: Key('blocked-update-name-${operation.id}'),
          style: theme.textTheme.titleSmall,
        ),
        if (recorded != null) ...[
          const SizedBox(height: 2),
          Text(
            'You recorded this on $recorded.',
            key: Key('blocked-update-recorded-${operation.id}'),
            style: theme.textTheme.bodySmall,
          ),
        ],
        const SizedBox(height: 8),
        Text(
          blockedUpdateReason(operation.reason),
          key: Key('blocked-update-reason-${operation.id}'),
          style: theme.textTheme.bodyMedium,
        ),
        const SizedBox(height: 8),
        Align(
          alignment: Alignment.centerLeft,
          child: TextButton(
            key: Key('blocked-update-acknowledge-${operation.id}'),
            onPressed: onAcknowledge,
            // Not "Dismiss" and not "Delete". The button removes the update from this phone, and a
            // driver pressing it should know that is what happened — this is the only thing in the
            // application that takes a recorded update away.
            child: const Text('I have read this — remove it'),
          ),
        ),
      ],
    );
  }
}
