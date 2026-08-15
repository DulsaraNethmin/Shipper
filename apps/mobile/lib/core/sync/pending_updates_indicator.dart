import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/blocked_updates.dart';
import 'package:shipper/core/sync/queue_watch.dart';

/// How many updates this device is holding that Shipper does not have (SHIP-126).
///
/// `Docs/02` §3.1's escalation ladder has three rungs and this is the first: *"Immediately — the app
/// shows a persistent indicator of how many updates are pending. **The user is never left guessing
/// whether their work was recorded.**"* The 4-hour nudge is SHIP-127 and the 24-hour operations
/// alert is SHIP-128; neither is here.
///
/// ## Persistent means it does not go away when the screen does
///
/// It is mounted by `ShipperApp` inside `MaterialApp.router`'s builder, **below the navigator**, so
/// it is on every screen in the application and survives every navigation. That is deliberate rather
/// than convenient: a driver records three milestones on one job, walks to the next, and the
/// question "did that go?" follows them. An indicator in one screen's app bar would answer it on
/// that screen only, which is the version of this ticket that reads as done and is not.
///
/// A bar below the content rather than an overlay on top of it, so it never covers a button — a
/// floating badge over the customer shell would sit on the action that publishes a delivery.
///
/// ## It is absent, not zero, when there is nothing to say
///
/// A row reading "0 updates waiting" is a row people learn to stop reading, and the escalation this
/// implements is about the case where the number is **not** zero. What answers "was my work
/// recorded" in the settled case is the delivery screen, where each recording says "Recorded"
/// beside it (SHIP-129) — a per-item answer, which is the stronger one.
///
/// ## The number, and the state that is deliberately not in it
///
/// [QueueSnapshot.unsynced] — **pending and in flight**. SHIP-125 chose that sum and gave the
/// reason: an operation the platform has refused is not waiting for a connection, it is waiting for
/// a person, and a number that never falls however long the driver stands in the open is not what
/// `Docs/02` §3.1 asks for.
///
/// **Blocked work is not therefore invisible**, which is the hole that reasoning would leave: a
/// device with nothing pending and one quarantined update would show no indicator at all, and the
/// user would be left guessing about the one update that most deserves attention. So it is a second
/// line in different words — *needs attention* rather than *waiting to sync* — and the indicator is
/// shown when **either** number is above zero. What lost, and to what, is SHIP-132's panel, which
/// **this line opens**; this one only says that waiting will not fix it.
class PendingUpdatesIndicator extends ConsumerWidget {
  const PendingUpdatesIndicator({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // Nothing is drawn until a snapshot has arrived. Riverpod 3 makes that an explicit case rather
    // than a nullable value, which is the right shape here: "the queue has not answered yet" and
    // "the queue is empty" are different facts and only the second is worth a row of its own — and
    // that one is empty too. See `queue_watch.dart` for the third case, an app with no worker.
    final AsyncValue<QueueSnapshot> published = ref.watch(queueSnapshotProvider);
    final snapshot = switch (published) {
      AsyncData(:final value) => value,
      _ => null,
    };
    if (snapshot == null) return const SizedBox.shrink();

    final waiting = snapshot.unsynced;
    final stuck = snapshot.blocked.length;
    if (waiting == 0 && stuck == 0) return const SizedBox.shrink();

    final theme = Theme.of(context);

    return Material(
      key: const Key('pending-updates'),
      color: stuck > 0 ? theme.colorScheme.errorContainer : theme.colorScheme.secondaryContainer,
      child: SafeArea(
        top: false,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: <Widget>[
              if (waiting > 0)
                _Line(
                  lineKey: const Key('pending-updates-count'),
                  icon: Icons.cloud_upload_outlined,
                  // Singular and plural, because "1 updates" on a driver's phone is the kind of
                  // detail that makes the rest of the screen look untrustworthy.
                  text: waiting == 1
                      ? '1 update waiting to sync'
                      : '$waiting updates waiting to sync',
                ),
              if (waiting > 0 && stuck > 0) const SizedBox(height: 4),
              if (stuck > 0)
                // **The way in to SHIP-132**, and the reason it is this line rather than a button
                // of its own: the bar already says these updates need attention and says nothing
                // about *what* they are or *why*. A person who reads that and cannot act on it is
                // the case `Docs/02` §3.1's "never left guessing" is about.
                //
                // Tapping does not acknowledge anything. It opens the panel; the panel's own
                // buttons are the only thing that removes an update.
                InkWell(
                  key: const Key('pending-updates-blocked-open'),
                  onTap: ref.read(blockedUpdatesOpenProvider.notifier).open,
                  child: _Line(
                    lineKey: const Key('pending-updates-blocked'),
                    icon: Icons.error_outline,
                    text: stuck == 1
                        ? '1 update needs attention'
                        : '$stuck updates need attention',
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}

/// One sentence with an icon in front of it.
///
/// The icon is decorative and the sentence carries the whole meaning, which is what makes this
/// legible to a screen reader and in sunlight — the two conditions `Docs/01` §4.4's driver is most
/// often in.
class _Line extends StatelessWidget {
  const _Line({required this.lineKey, required this.icon, required this.text});

  final Key lineKey;
  final IconData icon;
  final String text;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Row(
      mainAxisSize: MainAxisSize.min,
      children: <Widget>[
        Icon(icon, size: 18, color: theme.colorScheme.onSecondaryContainer),
        const SizedBox(width: 8),
        Flexible(
          child: Text(
            text,
            key: lineKey,
            style: theme.textTheme.bodyMedium,
            overflow: TextOverflow.ellipsis,
          ),
        ),
      ],
    );
  }
}
