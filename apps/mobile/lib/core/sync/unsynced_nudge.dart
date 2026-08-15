/// The second rung of `Docs/02` §3.1's escalation ladder (SHIP-127).
///
/// | Elapsed unsynced | Response | Where |
/// |---|---|---|
/// | Immediately | A persistent indicator of how many updates are pending | SHIP-126 |
/// | **4 hours** | **The provider is nudged, so someone who can find signal knows to** | **here** |
/// | 24 hours | Operations alert; the job enters the delivery-exception queue | SHIP-128, server-side |
///
/// ## It is a prompt rather than a louder indicator, and that is the ticket
///
/// SHIP-126's bar has been on the screen since the first update was recorded. Adding a fourth line
/// to it four hours later is not an escalation — it is the same row the driver has already learned
/// to read past, which is the version of this ticket that reads as done and is not. So the nudge
/// **interrupts**: a card over a scrim, above the router, dismissed by an explicit tap and by
/// nothing else. Tapping the scrim does not dismiss it, because an accidental dismissal buys four
/// more hours of silence (see [NudgePolicy.silencedThroughAfter]).
///
/// It is weaker than [VersionGate], which *replaces* the application below the build floor: this
/// one is dismissible and the driver goes straight back to work. That is the right strength for a
/// nudge — the 24-hour rung is where somebody other than the driver is told.
///
/// ## What it measures, and what it deliberately does not
///
/// [QueuedOperation.enqueuedAt] of the **oldest pending or in-flight** operation, which is the
/// column SHIP-124 wrote for exactly this: *"When the row was committed. What `Docs/02` §3.1's
/// four-hour and 24-hour escalations measure."*
///
/// **Blocked work is excluded**, for the reason SHIP-125 gave and SHIP-126 kept: an operation the
/// platform refused is not waiting for a connection, it is waiting for a person, and this prompt's
/// entire content is *go and find signal*. Telling a driver to walk up a hill about an update that
/// will still be refused when they get there is worse than saying nothing. SHIP-132 is the screen
/// for those, and SHIP-126's second line already says one exists.
///
/// **The known gap that leaves**, recorded rather than argued away: a blocked operation holds its
/// own ordering key, so a *pending* operation queued behind one on the same job is genuinely
/// unsynced, genuinely ageing, and genuinely not fixable by finding signal. It nudges anyway,
/// because it is pending, and the driver's trip up the hill will not clear it. Closing that needs
/// SHIP-132's acknowledgement to exist first — there is nothing the driver could do about it today
/// even if this prompt said so.
///
/// ## When it is evaluated — a snapshot, and no timer at all
///
/// The decision is a function of the last published [QueueSnapshot] and the clock at build time.
/// **Nothing arms a wake-up for the instant the threshold is crossed**, and that is a decision:
///
/// - A prompt nobody is looking at is not a prompt. What matters is that it is on the screen the
///   next time the driver looks, not that it appeared at 13:04:07.
/// - Every trigger that brings a person back to the application already publishes a snapshot.
///   `SyncSignals` drains on resume, and the worker publishes at the end of every pass — so
///   foregrounding the app re-evaluates this by itself.
/// - While there is claimable work the worker keeps its own schedule, ceilinged at five minutes,
///   so a phone left open crosses the threshold and is told within one backoff interval.
/// - A timer would also have to be cancelled correctly in every test that mounts `ShipperApp` with
///   a live queue, and a `Timer` left pending fails a widget test — a cost paid for granularity
///   nobody can perceive on a four-hour rule.
///
/// ## The threshold is a value and not a constant, and it should not stay on the device
///
/// `CLAUDE.md`: anything expected to change under operational pressure lives server-side, and
/// Flutter has no over-the-air path for Dart code. Four hours is exactly that kind of number — it
/// is an operations tuning knob, and moving it currently needs a store release.
///
/// It is compiled in **because it cannot be fetched at the moment it is needed**: this prompt fires
/// on a handset with no connection, which is the whole premise. What is available is the shape
/// `QueuePolicy` already uses — a default injected through [nudgePolicyProvider] rather than a
/// `const` somebody has to find — and a request that a client-policy endpoint carry it so the app
/// caches the current value while it still has signal. `GET /v1/app/minimum-version` (SHIP-167) is
/// the endpoint already shaped like that one. Recorded in `Docs/11` §3.
library;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/policy/app_policy_controller.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/pending_updates_indicator.dart';
import 'package:shipper/core/sync/queue_watch.dart';
import 'package:shipper/core/version/version_gate.dart';

/// What the nudge decided, from one snapshot and one reading of the clock.
@immutable
final class NudgeDecision {
  const NudgeDecision({
    required this.due,
    required this.waiting,
    required this.oldestFor,
  });

  /// Nothing to say: no queue, no unsynced work, or not old enough yet.
  static const quiet = NudgeDecision(due: false, waiting: 0, oldestFor: Duration.zero);

  /// Whether the provider should be prompted now.
  final bool due;

  /// How many operations are unsynced — pending **and** in flight, as SHIP-126 counts them.
  final int waiting;

  /// How long the oldest of them has been waiting, from [QueuedOperation.enqueuedAt].
  final Duration oldestFor;

  @override
  bool operator ==(Object other) =>
      other is NudgeDecision &&
      other.due == due &&
      other.waiting == waiting &&
      other.oldestFor == oldestFor;

  @override
  int get hashCode => Object.hash(due, waiting, oldestFor);

  @override
  String toString() => 'NudgeDecision(due: $due, waiting: $waiting, oldestFor: $oldestFor)';
}

/// How long an update may sit unsynced before the provider is prompted.
///
/// A value rather than a constant for the reason [QueuePolicy] gives about the same rule: a default
/// is unavoidable on a device that has to work with no platform to ask, but a default is not a
/// constant, and moving this needs a value passed to a provider rather than a code change.
@immutable
final class NudgePolicy {
  const NudgePolicy({this.after = const Duration(hours: 4)});

  /// `Docs/02` §3.1's second rung. Four hours, and the document is the authority for the number.
  final Duration after;

  /// When a dismissal taken at [now] stops holding.
  ///
  /// **A wall-clock instant rather than an age**, and the difference is a case that turns up on a
  /// working day rather than a hypothetical one. Silencing "until the oldest is four hours older"
  /// travels with the queue: the driver dismisses at four hours, that operation syncs, and the next
  /// backlog inherits eight hours of quiet it did nothing to earn. Silencing until a *time* buys
  /// exactly one quiet interval, whatever the queue does in it, so the prompt can appear at most
  /// once per [after] and cannot be silenced by work that has already gone.
  ///
  /// A dismissal that silenced forever would make the 24-hour rung the first thing anybody heard
  /// about, which is precisely the backstop `Docs/02` §3.1 says it must not become.
  DateTime silencedUntil(DateTime now) => now.add(after);

  /// Reads one snapshot.
  ///
  /// [silencedUntil] is when a dismissal stops holding, or `null` when nothing has been dismissed.
  ///
  /// A clock that has moved backwards produces a negative age, which clamps to zero rather than
  /// reading as "not yet": [QueuedOperation.enqueuedAt] is written from the device's own clock and
  /// a person can move that clock, so the arithmetic has to survive it. A clock moved *forwards*
  /// nudges early, which is a prompt appearing sooner than it should and is the harmless direction.
  NudgeDecision decide(
    QueueSnapshot? snapshot,
    DateTime now, {
    DateTime? silencedUntil,
  }) {
    if (snapshot == null) return NudgeDecision.quiet;

    DateTime? oldest;
    var waiting = 0;

    for (final operation in <QueuedOperation>[...snapshot.pending, ...snapshot.inFlight]) {
      waiting++;
      final at = operation.enqueuedAt;
      if (oldest == null || at.isBefore(oldest)) oldest = at;
    }

    if (oldest == null) return NudgeDecision.quiet;

    var age = now.difference(oldest);
    if (age.isNegative) age = Duration.zero;

    final quiet = silencedUntil != null && now.isBefore(silencedUntil);

    return NudgeDecision(due: age >= after && !quiet, waiting: waiting, oldestFor: age);
  }
}

/// The threshold the running application uses.
///
/// **It comes from the platform now (SHIP-167a).** `GET /v1/app/policy` carries
/// `unsynced_nudge_after_seconds`, the app fetches it while it still has signal and keeps it, and
/// a device that has been online before applies what it was told even when it is offline at the
/// moment the prompt is due — which is every time the prompt is due. The library note above asked
/// for exactly this endpoint; `core/policy` is the answer.
///
/// [NudgePolicy]'s own four hours remains the value for an install that has never once been
/// online, and `app_policy_test.dart` holds it against `compiledUnsyncedNudgeAfterSeconds` so the
/// two cannot drift.
final nudgePolicyProvider = Provider<NudgePolicy>(
  (ref) => NudgePolicy(after: ref.watch(appPolicyProvider).unsyncedNudgeAfter),
);

/// The clock the nudge reads.
///
/// Injected for the reason `SyncWorker` takes one: a test that has to wait four hours is a test
/// nobody runs. It is a function rather than a `DateTime` because the value is read at every build.
final nudgeClockProvider = Provider<DateTime Function()>((ref) => DateTime.now);

/// When a dismissal stops holding, in the running application. `null` until one is taken.
///
/// **In memory, and deliberately not durable.** A relaunch re-asks, which is correct: the driver has
/// just picked the phone up, which is the moment a prompt is worth showing — and the alternative is
/// a device that stays quiet across restarts about work it is still holding.
final class NudgeSilence extends Notifier<DateTime?> {
  @override
  DateTime? build() => null;

  /// Silences the prompt for one [NudgePolicy.after] from now.
  void dismiss() {
    state = ref.read(nudgePolicyProvider).silencedUntil(ref.read(nudgeClockProvider)());
  }
}

/// See [NudgeSilence].
final nudgeSilenceProvider = NotifierProvider<NudgeSilence, DateTime?>(NudgeSilence.new);

/// How long the oldest update has been waiting, in the words a person reads.
///
/// Whole units and no "about": `4 hours`, `1 day`. Minutes are never rendered because nothing below
/// four hours reaches this, and a duration to the minute invites the reader to work out whether 4h
/// 3m is different from 4h — it is not, and the ladder's next rung is twenty hours away.
///
/// It lives here rather than in `shared/formatting` because it renders one thing: an escalation age
/// in the ladder's own units. A second consumer is what would move it, and there is not one.
String waitedInWords(Duration waited) {
  if (waited.inHours >= 48) {
    final days = waited.inDays;
    return days == 1 ? '1 day' : '$days days';
  }

  final hours = waited.inHours;
  return hours == 1 ? '1 hour' : '$hours hours';
}

/// Prompts the provider when an update has been pending four hours (SHIP-127).
///
/// Wraps the application rather than sitting beside it, so the card covers every screen **and**
/// SHIP-126's bar. See the library note above for why it interrupts, what it measures, and when it
/// is evaluated.
///
/// **It costs a test nothing**, by the same mechanism as [PendingUpdatesIndicator]: it reads
/// [queueSnapshotProvider], which is empty until `main.dart` supplies the running worker, so a
/// widget test that has not asked for a queue draws its child and opens no database.
class UnsyncedNudge extends ConsumerWidget {
  const UnsyncedNudge({required this.child, super.key});

  /// The application, drawn underneath.
  final Widget child;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final AsyncValue<QueueSnapshot> published = ref.watch(queueSnapshotProvider);
    final snapshot = switch (published) {
      AsyncData(:final value) => value,
      _ => null,
    };

    final policy = ref.watch(nudgePolicyProvider);
    final now = ref.watch(nudgeClockProvider)();
    final decision = policy.decide(
      snapshot,
      now,
      silencedUntil: ref.watch(nudgeSilenceProvider),
    );

    if (!decision.due) return child;

    return Stack(
      fit: StackFit.expand,
      children: <Widget>[
        child,
        // Absorbing rather than passing through. A card floating over a live application is what
        // SHIP-126 rejected for the indicator — it sits on whatever button is underneath it — and
        // the answer for something that is *meant* to interrupt is to stop the taps rather than to
        // let them land on a screen the driver cannot see properly.
        ModalBarrier(
          key: const Key('unsynced-nudge-scrim'),
          dismissible: false,
          color: Theme.of(context).colorScheme.scrim.withValues(alpha: 0.6),
        ),
        _NudgeCard(
          decision: decision,
          onDismiss: ref.read(nudgeSilenceProvider.notifier).dismiss,
        ),
      ],
    );
  }
}

/// The card itself.
class _NudgeCard extends StatelessWidget {
  const _NudgeCard({required this.decision, required this.onDismiss});

  final NudgeDecision decision;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return SafeArea(
      child: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 420),
            child: Material(
              key: const Key('unsynced-nudge'),
              color: theme.colorScheme.surface,
              borderRadius: BorderRadius.circular(12),
              child: Padding(
                padding: const EdgeInsets.all(20),
                child: Column(
                  mainAxisSize: MainAxisSize.min,
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: <Widget>[
                    Icon(Icons.cloud_off_outlined, color: theme.colorScheme.error),
                    const SizedBox(height: 12),
                    Text(
                      'Shipper still does not have your updates',
                      key: const Key('unsynced-nudge-headline'),
                      style: theme.textTheme.titleMedium,
                    ),
                    const SizedBox(height: 12),
                    Text(
                      _waiting,
                      key: const Key('unsynced-nudge-waiting'),
                      style: theme.textTheme.bodyMedium,
                    ),
                    const SizedBox(height: 8),
                    Text(
                      // The action is physical, and saying so is the whole of the ticket:
                      // `Docs/02` §3.1 nudges the provider "so someone who can find signal knows
                      // to". **There is no Try again button**, deliberately — the worker already
                      // retries on its own schedule and on every resume, so a button would do
                      // nothing this phone is not doing, and one that failed in front of the
                      // driver would read as the work being lost.
                      'Nothing has been lost — it is all still on this phone. Shipper will send it '
                      'by itself as soon as there is a connection, so getting to somewhere with '
                      'coverage is what will clear it.',
                      key: const Key('unsynced-nudge-advice'),
                      style: theme.textTheme.bodyMedium,
                    ),
                    const SizedBox(height: 20),
                    Align(
                      alignment: Alignment.centerRight,
                      child: FilledButton(
                        key: const Key('unsynced-nudge-dismiss'),
                        onPressed: onDismiss,
                        child: const Text('Got it'),
                      ),
                    ),
                  ],
                ),
              ),
            ),
          ),
        ),
      ),
    );
  }

  /// Singular and plural, because "1 updates" on a driver's phone makes the rest of the screen
  /// look untrustworthy — the same call SHIP-126's indicator makes.
  String get _waiting {
    final waited = waitedInWords(decision.oldestFor);
    return decision.waiting == 1
        ? 'An update you recorded has been waiting on this phone for $waited.'
        : '${decision.waiting} updates you recorded have been waiting on this phone, the oldest '
            'for $waited.';
  }
}
