/// What a queued operation is, and what it can be read back as.
///
/// Two shapes rather than one, and the split is the ticket's central idea. A row that this build
/// can read becomes a [QueuedOperation] — typed, sendable, renderable. A row it cannot read
/// becomes a [BlockedOperation], which carries only the columns that cannot fail to decode. There
/// is no third outcome, and in particular there is no outcome where a row is skipped: a queue
/// that quietly steps over what it does not understand is a queue that drops operations, which is
/// the one thing `Docs/07` §4 says it must never do.
library;

/// A kind of operation the queue can hold.
///
/// **The constructor is private, so the set below is closed by the compiler.** That is the same
/// move `internal/events` makes on the platform side (SHIP-135): rather than build a recovery path
/// for an operation nothing can ever send, make it unwritable. A feature cannot invent a kind in
/// its own file; it uses one of these, and adding a ninth is a decision somebody recorded in this
/// list — exactly as an eighth Go domain is a line in `internal/boundaries`.
///
/// Features may read this because it is in `core/`; `Docs/07` §2 forbids features importing one
/// another, not importing core.
final class OperationKind {
  const OperationKind._(this.name, this.bodyVersion);

  /// What is written into the row, and what a row written by another build is matched against.
  final String name;

  /// The shape of [QueuedOperation.body] that **this build** writes and reads for this kind.
  ///
  /// Stored per row, not looked up at read time, for the reason SHIP-135 gives about the outbox's
  /// schema version: a row written by one build and read by another is entitled to the shape it
  /// was written under, and a version resolved at read time would relabel it as the newer one.
  ///
  /// A row whose version does not match is **not** guessed at. It is quarantined — see
  /// [QueueBlockReason.unsupported]. Whoever bumps a version and wants the queued rows carried
  /// forward writes a Drift migration that rewrites the bodies and the column together, which is
  /// the same obligation a schema change already carries.
  final int bodyVersion;

  /// A delivery milestone the driver or provider recorded (`Docs/01` §4.4, `Docs/02` §3.1).
  ///
  /// The one `Docs/07` §4 names first, and the reason the queue exists: pickup bays, warehouses
  /// and rural routes have no usable signal, and a driver cannot be asked to stand still until a
  /// request completes.
  static const milestone = OperationKind._('delivery.milestone', 1);

  /// A captured proof photograph, uploading from a local file.
  ///
  /// A separate operation rather than part of [milestone], because `Docs/07` §4 is explicit that
  /// proof images "upload on reconnection, **not** as part of the milestone request". The image
  /// itself never enters the row; [QueuedOperation.attachmentPath] points at the compressed file
  /// SHIP-130 wrote.
  static const proof = OperationKind._('delivery.proof', 1);

  /// Every kind this build understands.
  static const all = <OperationKind>[milestone, proof];

  /// The kind [name] names, or `null` if this build has never heard of it.
  ///
  /// `null` is the app-upgrade case and is a real one: a row written by a build that had a kind
  /// this build does not. It is quarantined rather than dropped.
  static OperationKind? byName(String name) {
    for (final kind in all) {
      if (kind.name == name) return kind;
    }
    return null;
  }

  @override
  String toString() => '$name/v$bodyVersion';
}

/// Where an operation is in its life.
///
/// Stored as text, and **read defensively**: only the two strings below are recognised, and
/// anything else — a state written by a later build, a corrupted value — reads as blocked. That
/// direction is deliberate. An unrecognised state that read as "not pending, not in flight, not
/// blocked" would be a row present in the table and absent from every list, which is a silent drop
/// wearing a database row as a disguise.
enum QueueState {
  /// Waiting to be sent.
  pending('pending'),

  /// Claimed by the sync worker and not yet resolved.
  ///
  /// A process that dies here leaves the row in this state. [OperationQueue.recover] is what
  /// returns it to [pending]; nothing times it out, because a phone runs one app process and the
  /// next launch is the only other party there is.
  inFlight('in_flight');

  const QueueState(this.wire);

  /// What goes in the column.
  final String wire;
}

/// Why an operation cannot proceed without somebody looking at it.
enum QueueBlockReason {
  /// This build cannot make sense of the row.
  ///
  /// A body that is not JSON, or is not a JSON object, or a `state` column holding something this
  /// build has no name for.
  unreadable('unreadable'),

  /// This build understands the row's structure but not its contents.
  ///
  /// A kind it does not have, or a body version it does not write. Both are the app-upgrade case:
  /// the platform has one deployment, a handset has whatever build was last installed, so unlike
  /// the outbox at SHIP-135 the client **cannot** make this unwritable. What it can do is refuse
  /// to guess, and keep the row where a person can see it.
  unsupported('unsupported'),

  /// The platform saw the operation and would not take it.
  ///
  /// Set by the sync worker, not here. `Docs/02` §3.1 and `Docs/07` §4 both require that a queued
  /// update which lost — to an administrative cancellation, say — is retained and **shown to the
  /// user** rather than discarded, which is SHIP-132. This is the state it is shown from.
  refused('refused');

  const QueueBlockReason(this.wire);

  /// What goes in the column.
  final String wire;

  /// The reason [wire] names, defaulting to [unreadable] for anything unrecognised.
  static QueueBlockReason fromWire(String? wire) {
    for (final reason in QueueBlockReason.values) {
      if (reason.wire == wire) return reason;
    }
    return unreadable;
  }
}

/// An operation this build can read, and therefore send.
final class QueuedOperation {
  const QueuedOperation({
    required this.id,
    required this.idempotencyKey,
    required this.kind,
    required this.orderingKey,
    required this.method,
    required this.path,
    required this.body,
    required this.recordedAt,
    required this.enqueuedAt,
    required this.state,
    required this.attempts,
    this.attachmentPath,
    this.nextAttemptAt,
  });

  /// The row's identity, and the enqueue sequence.
  ///
  /// Monotonic, from SQLite's `AUTOINCREMENT`, so ordering by it is ordering by when the user
  /// acted — without depending on a clock that a user can move.
  final int id;

  /// Minted **once**, when the user acted, and reused by every attempt for the life of the row.
  ///
  /// This is the mechanism `Docs/02` §3.1 and `Docs/07` §4 both turn on, and storing it is what
  /// makes it true across a restart: a retry after a dropped connection replays the platform's
  /// original outcome rather than creating a second record. `ActionKey` in `core/api` does the
  /// same job for a retry inside one screen; the difference is only how long the key has to live,
  /// which is why one is a field and the other is a column.
  final String idempotencyKey;

  final OperationKind kind;

  /// What this operation must stay in order behind. Conventionally `job:<id>`.
  ///
  /// The queue guarantees FIFO **within** a key and promises nothing between keys. See
  /// `operation_queue.dart` for why that is the guarantee rather than one of the other two.
  final String orderingKey;

  final String method;
  final String path;

  /// The request body, exactly as it will be sent.
  final Map<String, dynamic> body;

  /// A local file to upload with the operation, or `null`.
  ///
  /// `Docs/07` §4 queues proof images as compressed local files. The queue guarantees the **row**
  /// survives; keeping the file it points at alive is SHIP-130's, and a row whose file has gone is
  /// a refusal the worker records rather than something this layer can pre-empt.
  final String? attachmentPath;

  /// When the user acted.
  ///
  /// `Docs/02` §3.1 carries two timestamps on every transition, and this is the first: what the
  /// customer sees. The second — when the platform accepted it — belongs to the platform and is
  /// never written here.
  final DateTime recordedAt;

  /// When the row was committed. What `Docs/02` §3.1's four-hour and 24-hour escalations measure.
  final DateTime enqueuedAt;

  final QueueState state;

  /// How many times this operation has been claimed.
  ///
  /// Incremented by the claim, so a process that dies mid-send still counts the attempt. What to
  /// **do** with a number that keeps growing is SHIP-125's backoff policy; the queue's own promise
  /// is only that the row is still here and still counted whatever that policy decides.
  final int attempts;

  /// The earliest this operation may be claimed again, or `null` for "now".
  ///
  /// Written by [OperationQueue.release]. Durable on purpose: a backoff that reset on every app
  /// launch would be no backoff at all on a handset, which is restarted far more often than a
  /// server.
  final DateTime? nextAttemptAt;

  @override
  String toString() => 'QueuedOperation($id, $kind, $orderingKey, ${state.wire})';
}

/// An operation that needs a person, held in the columns that cannot fail to decode.
///
/// Deliberately carries no body and no typed kind. It is the shape a row takes when the reason it
/// is here is that this build could not read it — a [QueuedOperation] would have to decode the
/// very thing that failed.
final class BlockedOperation {
  const BlockedOperation({
    required this.id,
    required this.idempotencyKey,
    required this.kindName,
    required this.orderingKey,
    required this.recordedAt,
    required this.enqueuedAt,
    required this.attempts,
    required this.reason,
    this.detail,
  });

  final int id;
  final String idempotencyKey;

  /// The `kind` column verbatim, which may name a kind this build does not have.
  final String kindName;

  final String orderingKey;
  final DateTime recordedAt;
  final DateTime enqueuedAt;
  final int attempts;
  final QueueBlockReason reason;

  /// Free text for a support conversation. Never shown as user-facing copy — a screen writes its
  /// own words for the reason, which is what keeps copy out of `core/`.
  final String? detail;

  @override
  String toString() => 'BlockedOperation($id, $kindName, ${reason.wire})';
}

/// Everything in the queue, partitioned, from one read.
///
/// One method rather than three accessors, because the three lists have to be a **partition** of
/// the table and three separate reads cannot promise that: reading is also when an unreadable row
/// is discovered and moved, so a row could slip between two of them. [total] is counted from the
/// table itself and is the number a row cannot hide from, whatever is in it.
final class QueueSnapshot {
  const QueueSnapshot({
    required this.total,
    required this.pending,
    required this.inFlight,
    required this.blocked,
  });

  /// Every row in the table. Counted in SQL, so it does not depend on anything decoding.
  final int total;

  final List<QueuedOperation> pending;
  final List<QueuedOperation> inFlight;
  final List<BlockedOperation> blocked;

  /// What `Docs/02` §3.1's persistent indicator counts: work the user recorded that the platform
  /// has not yet accepted.
  ///
  /// Blocked operations are excluded because they are not waiting for a connection — they are
  /// waiting for a person, and SHIP-132 shows them as their own thing rather than as a number that
  /// never goes down.
  int get unsynced => pending.length + inFlight.length;

  @override
  String toString() =>
      'QueueSnapshot(total: $total, pending: ${pending.length}, '
      'inFlight: ${inFlight.length}, blocked: ${blocked.length})';
}
