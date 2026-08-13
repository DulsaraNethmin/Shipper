import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/sync_worker.dart';
import 'package:shipper/features/delivery/milestone.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// Where one recorded milestone has got to.
///
/// **These four are the whole of what a provider is told, and they are told in words.** `Docs/02`
/// §3.1 requires optimistic local state to be "clearly marked as pending", and the failure that
/// requirement exists to prevent is a screen where a recorded-and-sent milestone and a
/// recorded-and-stuck one look the same. A colour alone would not do it — a driver in sunlight,
/// with a cracked screen, or with a colour vision deficiency reads the word and not the tint.
enum MilestoneSync {
  /// On this device, not yet offered to the platform.
  pending('Pending', 'Recorded on this device. Not sent yet.'),

  /// On the wire now.
  ///
  /// Rare to see, and kept because the queue can express it: an operation the worker has claimed
  /// and not yet resolved. A process that dies here leaves the row in this state and
  /// `OperationQueue.recover` returns it to [pending] at the next launch.
  sending('Sending', 'On its way to Shipper now.'),

  /// The platform has it.
  ///
  /// **Derived from the operation having left the queue, which is the only signal there is.**
  /// `OperationSender.send` returns nothing on success and the worker deletes the row, so there is
  /// no response body to reconcile against — see [RecordMilestoneController] for what that does and
  /// does not entitle this screen to claim.
  recorded('Recorded', 'Shipper has it.'),

  /// The platform saw it and would not take it, or this build cannot make sense of the row.
  ///
  /// The work is **not** lost: SHIP-124 quarantines rather than deletes, and SHIP-132 is the screen
  /// that shows what lost and to what. All this one says is that waiting will not fix it.
  needsAttention('Needs attention', 'Shipper would not take this update. It is still here.');

  const MilestoneSync(this.label, this.detail);

  /// The word on the badge.
  final String label;

  /// The sentence under it.
  final String detail;
}

/// One milestone this device has recorded, and where it has got to.
@immutable
final class MilestoneEntry {
  const MilestoneEntry({
    required this.operationId,
    required this.wire,
    required this.recordedAt,
    required this.sync,
  });

  /// The queue row. Monotonic from `AUTOINCREMENT`, so ordering by it is ordering by when the
  /// provider acted — without depending on a clock a person can move.
  final int operationId;

  /// The milestone as it will go on the wire.
  final String wire;

  /// When the provider acted. **The actor's clock**, which `Docs/02` §3.1 keeps distinct from the
  /// platform's, and the one a customer is shown.
  final DateTime recordedAt;

  final MilestoneSync sync;

  /// The milestone this build knows [wire] as, or `null` for one written by a later build.
  Milestone? get milestone => Milestone.byWire(wire);

  /// What the screen calls it. The raw wire value when this build has no name for it, because a
  /// queued operation that is not displayed is a silent drop one layer above the queue.
  String get label => milestone?.label ?? wire;

  @override
  bool operator ==(Object other) =>
      other is MilestoneEntry &&
      other.operationId == operationId &&
      other.wire == wire &&
      other.recordedAt == recordedAt &&
      other.sync == sync;

  @override
  int get hashCode => Object.hash(operationId, wire, recordedAt, sync);
}

/// What the delivery screen draws.
@immutable
final class DeliveryState {
  const DeliveryState({this.entries = const <MilestoneEntry>[], this.refusal});

  /// What this device has recorded for this job, newest first.
  final List<MilestoneEntry> entries;

  /// Why the last recording could not even be stored, or `null`.
  ///
  /// A [QueueRefusal] rather than a platform refusal — the device is full, or the same action was
  /// recorded twice. It is copy rather than the exception, because `Docs/07` §3 keeps the words a
  /// person reads in the layer that draws them.
  final String? refusal;

  /// How many of this job's recordings the platform does not have yet.
  int get unsent => entries.where((entry) => entry.sync != MilestoneSync.recorded).length;

  @override
  bool operator ==(Object other) =>
      other is DeliveryState && listEquals(other.entries, entries) && other.refusal == refusal;

  @override
  int get hashCode => Object.hash(Object.hashAll(entries), refusal);
}

/// Recording milestones on one job, and showing what has and has not reached the platform
/// (SHIP-129).
///
/// ## The transition is never applied here, and that is the whole shape of this class
///
/// `Docs/02` §3.1: *"Transitions are never applied by the client. The app displays optimistic local
/// state, clearly marked as pending, and reconciles to whatever the platform returns."* So this
/// controller records a **claim about something that happened** and never a status. It holds no
/// copy of `Docs/02` §2's transition table, it does not know what the job's status is, and it never
/// decides that one milestone must follow another — which is why the screen leaves every button
/// enabled after one is used. Three reasons that is right rather than lazy:
///
/// - `Docs/02` §2 permits `Awarded → En route to pickup` directly, because a provider driving the
///   job themselves has nobody to nominate. A client walking the five as a chain is wrong about
///   that job.
/// - A driver who reaches a pickup, finds nobody there and sets off again records
///   `en_route_to_pickup` **twice** (SHIP-111). Two rows, one transition.
/// - A queued update that arrives after a later one must be **absorbed, not rejected** (SHIP-112).
///   A client that refused to record it would be discarding a driver's work to protect a rule the
///   platform does not have.
///
/// ## What "recorded" is derived from, and what it does not claim
///
/// `SyncWorker.record` enqueues and drains in one call, `OperationSender.send` returns nothing on
/// success, and the worker **deletes** the row when the platform accepts it. So the only signal the
/// client has is the operation leaving the queue, and that is what [MilestoneSync.recorded] means:
/// the platform took it. It deliberately does **not** claim the job moved — an absorbed milestone
/// is a `201` that moves nothing (SHIP-112), and no response field distinguishes the two, by
/// decision. **This matters for what the screen must not say**: "Recorded" is honest; "Job is now
/// In transit" would not be.
///
/// ## The read is fresh, and the sequence number is why
///
/// Each pass of the worker publishes a snapshot, and this controller uses that only as a **trigger**
/// — it re-reads the queue itself rather than trusting the payload. The reason is an ordering one
/// and it produces a wrong answer rather than a slow one: a snapshot is read at one instant and
/// delivered through a broadcast stream at a later one, so a snapshot **read before** this
/// controller's own `enqueue` committed can be **delivered after** it. Trusting it would show a
/// milestone recorded a moment ago as already on the platform, then flick it back to pending on the
/// next pass — the exact confusion `Docs/02` §3.1's escalation ladder exists to prevent, in
/// miniature.
///
/// A fresh read cannot be stale, because it is issued after the enqueue returned. What is left is
/// the same race one step up: a fresh read *already in flight* when the enqueue commits. So each
/// read carries a sequence number and an entry records the sequence in force when it was created;
/// only a read issued **after** the entry may conclude that it has gone. `_applied` discards a read
/// that comes back out of order.
class RecordMilestoneController extends Notifier<DeliveryState> {
  RecordMilestoneController(this.jobId);

  /// The job being delivered. From the route, which is the only place it comes from.
  final String jobId;

  /// What this operation must stay in order behind. `job:<id>` is SHIP-124's convention, and it is
  /// what makes one job's milestones reach the platform in the order they were recorded while
  /// another job's problem does not stall them.
  String get orderingKey => 'job:$jobId';

  final _tracked = <int, _Tracked>{};

  int _reads = 0;
  int _applied = 0;

  late SyncWorker _worker;

  @override
  DeliveryState build() {
    _worker = ref.watch(syncWorkerProvider);

    final subscription = _worker.snapshots.listen((_) => unawaited(_reconcile()));
    ref.onDispose(subscription.cancel);

    // Catch up with whatever is already in the queue. This is the relaunch case and it is not a
    // rare one: the driver recorded three milestones in a yard, the app was killed, and the work is
    // still on the device. Without this the screen would open empty and the queue would be full.
    unawaited(_reconcile());

    return DeliveryState(entries: _entries());
  }

  /// Records [milestone] against this job and asks for it to be sent.
  ///
  /// Returns once the row is **committed**, which is when the screen may confirm it: `Docs/07` §4
  /// has the user record what happened and move on, and the confirmation is immediate and marked
  /// pending rather than waiting on a network the driver may not have.
  ///
  /// The idempotency key is minted by the queue at this moment — the moment the user acted — and
  /// reused unchanged by every attempt for the life of the row (SHIP-124, SHIP-125). Nothing here
  /// touches it, which is what makes a retry after a day in a valley a retry rather than a second
  /// milestone on the customer's timeline.
  Future<bool> record(Milestone milestone) async {
    // Belt and braces beside the screen, which offers no button for it. The platform refuses every
    // `delivered` with `delivery_proof_required`, so queueing one would put work the driver
    // believes they recorded into a quarantine that only a person can clear.
    if (milestone.needsProof) return false;

    final at = DateTime.now();
    final bornAt = _reads;

    try {
      final operation = await _worker.record(
        kind: OperationKind.milestone,
        orderingKey: orderingKey,
        method: 'POST',
        path: '/v1/jobs/$jobId/milestones',
        body: <String, dynamic>{
          'milestone': milestone.wire,
          // **The actor's clock, with the device's offset on it.** `DateTime.toIso8601String()`
          // on a local value carries no offset at all, which `time.Parse(time.RFC3339, …)`
          // refuses — the driver is told "that is not a date" about the moment they tapped a
          // button. Omitting the field would be worse than wrong copy: the platform would then
          // stamp the milestone with the time it *arrived*, which for an update queued in a valley
          // is hours after the delivery event it describes, and `Docs/02` §3.1's two clocks would
          // be one.
          'recorded_at': rfc3339(at),
        },
        recordedAt: at,
      );

      if (!ref.mounted) return true;

      _tracked[operation.id] = _Tracked(wire: milestone.wire, recordedAt: at, bornAt: bornAt);
      state = DeliveryState(entries: _entries());
      return true;
    } on QueueRefusal catch (refusal) {
      if (ref.mounted) state = DeliveryState(entries: _entries(), refusal: _refusalCopy(refusal));
      return false;
    }
  }

  /// Clears the message about a recording the device would not store.
  void dismissRefusal() {
    if (state.refusal == null) return;
    state = DeliveryState(entries: state.entries);
  }

  /// Re-reads the queue and moves every entry to where the queue says it is.
  Future<void> _reconcile() async {
    if (!ref.mounted) return;

    final at = ++_reads;

    final QueueSnapshot snapshot;
    try {
      snapshot = await _worker.queue.snapshot();
    } catch (_) {
      // The queue could not be read. The screen keeps what it is showing rather than claiming
      // anything moved, and the next pass tries again — an entry wrongly shown as recorded is the
      // one outcome worth a swallowed exception.
      return;
    }

    if (!ref.mounted || at <= _applied) return;
    _applied = at;

    final live = <int, MilestoneSync>{};

    void note(QueuedOperation operation, MilestoneSync sync) {
      if (operation.orderingKey != orderingKey) return;
      if (operation.kind != OperationKind.milestone) return; // SHIP-130's proof upload is not one.

      live[operation.id] = sync;

      // Learn about work this controller did not record: a relaunch, or the same job open twice.
      final wire = operation.body['milestone'];
      _tracked.putIfAbsent(
        operation.id,
        () => _Tracked(
          wire: wire is String ? wire : '',
          recordedAt: operation.recordedAt,
          bornAt: 0,
        ),
      );
    }

    for (final operation in snapshot.pending) {
      note(operation, MilestoneSync.pending);
    }
    for (final operation in snapshot.inFlight) {
      note(operation, MilestoneSync.sending);
    }
    for (final operation in snapshot.blocked) {
      // A blocked row carries no body — it is the shape a row takes when this build could not read
      // one — so a quarantined operation this screen never saw recorded cannot be named here.
      // SHIP-132 is the screen that shows those, and SHIP-126's indicator counts them meanwhile.
      if (operation.orderingKey != orderingKey) continue;
      if (_tracked.containsKey(operation.id)) live[operation.id] = MilestoneSync.needsAttention;
    }

    for (final entry in _tracked.entries) {
      final where = live[entry.key];
      if (where != null) {
        entry.value.sync = where;
        continue;
      }
      // Absent from a read issued after this entry was created: the queue has let it go, which
      // happens on exactly one path — the platform accepted it. See the note on the class for why
      // the sequence number is what makes that conclusion safe.
      if (at > entry.value.bornAt) entry.value.sync = MilestoneSync.recorded;
    }

    state = DeliveryState(entries: _entries(), refusal: state.refusal);
  }

  List<MilestoneEntry> _entries() {
    final entries = _tracked.entries
        .map((entry) => MilestoneEntry(
              operationId: entry.key,
              wire: entry.value.wire,
              recordedAt: entry.value.recordedAt,
              sync: entry.value.sync,
            ))
        .toList()
      ..sort((a, b) => b.operationId.compareTo(a.operationId));

    return List<MilestoneEntry>.unmodifiable(entries);
  }

  /// The words for a device that would not store the recording.
  ///
  /// Each one says what the provider can do about it, because all three are conditions of this
  /// handset rather than of the delivery.
  ///
  /// The switch is exhaustive over a **sealed** hierarchy, so a fourth refusal added to the queue
  /// fails to compile here rather than reaching a driver as a blank message.
  String _refusalCopy(QueueRefusal refusal) => switch (refusal) {
        QueueAtCapacity() => 'This device is holding as many unsent updates as it can. '
            'Find signal so the ones already recorded can be sent, then record this one again.',
        QueueOperationTooLarge() => 'That update is too large to store on this device.',
        QueueDuplicateOperation() => 'That update is already waiting to be sent.',
      };
}

/// What the controller remembers about one queue row between reads.
///
/// Mutable and private: [MilestoneEntry] is the immutable value the screen sees, and this is the
/// bookkeeping behind it. [bornAt] is the read sequence in force when the row became known, and it
/// is the whole of the staleness guard — see the note on [RecordMilestoneController].
final class _Tracked {
  _Tracked({required this.wire, required this.recordedAt, required this.bornAt});

  final String wire;
  final DateTime recordedAt;
  final int bornAt;

  MilestoneSync sync = MilestoneSync.pending;
}

/// Recording milestones on one job, keyed by the job's id.
///
/// **Auto-disposed**, like every other read in this client (`Docs/07` §3): what a device holds
/// about a job goes with the token at sign-out. The cost is that leaving the screen forgets the
/// entries the platform has already taken — the pending ones come back from the queue, because the
/// queue is durable, and the accepted ones do not, because nothing on the device stored them and
/// **no endpoint serves an awarded job's milestones back to the provider who recorded them**. That
/// is a gap in the platform rather than in this screen, and it is recorded in `Docs/11` §3.
final recordMilestoneProvider = NotifierProvider.autoDispose
    .family<RecordMilestoneController, DeliveryState, String>(RecordMilestoneController.new);
