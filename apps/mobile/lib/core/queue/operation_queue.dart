import 'dart:convert';

import 'package:drift/drift.dart';
import 'package:drift_flutter/drift_flutter.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/queue/queue_database.dart';
import 'package:shipper/core/queue/queued_operation.dart';

/// The durable local operation queue (SHIP-124).
///
/// `Docs/07` §4: the user records what happened and moves on; syncing is the platform's problem.
/// Two properties are what that sentence costs, and everything below is one of them.
///
/// ## 1. It survives a restart
///
/// [enqueue] returns only after its transaction has committed, which is what makes it safe for a
/// screen to confirm to the user at that point. A crash before the commit leaves nothing at all —
/// no half-written row, and no confirmation either, because the call threw.
///
/// ## 2. Nothing is ever silently dropped
///
/// The interesting half, and it is a property of the whole class rather than of one method. Six
/// ways an operation could vanish, and what stops each:
///
/// - **A crash between enqueue and commit.** One transaction. Nothing partial can exist, and the
///   caller is told rather than left believing the work is queued.
/// - **A crash mid-drain.** A claimed row stays in the table in [QueueState.inFlight]; [recover]
///   returns it to [QueueState.pending] at the next launch. Its idempotency key is the one minted
///   when the user acted, so re-sending it cannot duplicate what the platform may already have
///   recorded.
/// - **A row this build cannot read** — an older or newer build's body, a kind that has been
///   removed. Skipping it would be the drop; throwing would stall the queue behind it. It is
///   **quarantined**: an `UPDATE` that records why, after which it is still counted, still listed,
///   and still deletable only by somebody deciding to.
/// - **Unbounded growth.** [QueuePolicy.maxOperations] is a cap, and reaching it **refuses the new
///   operation** rather than evicting the oldest. Eviction to make room is precisely a silent
///   drop; a refusal happens in front of the person who just acted, which is the only moment they
///   can do anything about it.
/// - **Two writers racing.** Every state change is a conditional `UPDATE` or `DELETE` whose
///   affected-row count is checked, so two claimants cannot both believe they hold an operation,
///   and the capacity check runs inside the insert's own transaction.
/// - **A bulk clear.** [clear] reports how many operations it discarded, so sign-out can say so.
///
/// ## What happens to an operation that can never succeed
///
/// SHIP-135 answered this on the platform side by making a permanently unpublishable outbox row
/// **unwritable** — all three of its failure classes are decided inside the transaction that
/// writes it. This queue takes that move as far as it goes and then stops, because the client's
/// situation is genuinely different.
///
/// Taken: a kind that does not exist cannot be written, because [OperationKind]'s constructor is
/// private and the set is closed at compile time; a body over [QueuePolicy.maxBodyBytes] is
/// refused by [enqueue]; a duplicate idempotency key is refused by [enqueue] and by a `UNIQUE`
/// constraint behind it.
///
/// Not available: the platform has one deployment and this app has whatever build was last
/// installed, so a row written by another build is a case no enqueue-time check can reach. Nor can
/// the client know in advance that the platform will refuse an operation — that answer arrives
/// hours later, offline being the whole point. For those two the answer is **quarantine and
/// escalate, never delete**: the row moves to blocked, `Docs/02` §3.1's ladder — indicator
/// immediately, provider nudge at four hours, operations alert at 24 — is what stops it being
/// quiet, and only a person acknowledging it (SHIP-132) removes it.
///
/// ## Ordering: FIFO within an ordering key, nothing between keys
///
/// A decision, taken on this queue's own terms rather than on SHIP-112's, which is being built in
/// parallel and is not depended on here.
///
/// **Not global FIFO.** One job's stuck operation would then hold up every other job's — a
/// head-of-line block that turns one problem into a stalled queue, which is how a driver finds out
/// their morning's work never left the handset.
///
/// **Not unordered.** `Docs/02` §3.1 has the platform absorb a milestone that arrives after a
/// later one, and SHIP-112 builds that. It is a safety net, and sending a driver's recorded
/// sequence in an arbitrary order would make every reconnection depend on it.
///
/// **Per key, then.** A driver's sequence for one job reaches the platform in the order they
/// recorded it, and one job's problem cannot stall another's. A blocked operation **does** hold
/// its own key — releasing what is behind it past it would silently reorder exactly the sequence
/// the key exists to preserve — and that is visible rather than quiet, because it is what the
/// pending indicator and the escalation ladder are counting.
class OperationQueue {
  /// Takes its database, so the application opens a file and a test opens a temporary one.
  ///
  /// [mintKey] and [now] are injectable for the same reason `ActionKey` takes a mint: a test that
  /// has to match random hex, or wait for a clock, is testing the wrong thing.
  OperationQueue(
    this._db, {
    this.policy = const QueuePolicy(),
    String Function()? mintKey,
    DateTime Function()? now,
  })  : _mintKey = mintKey ?? newIdempotencyKey,
        _now = now ?? DateTime.now;

  final QueueDatabase _db;

  /// The bounds this queue enforces. See [QueuePolicy] for why it is a value and not a constant.
  final QueuePolicy policy;

  final String Function() _mintKey;
  final DateTime Function() _now;

  /// The wire value [block] writes.
  ///
  /// Reading does not depend on it: **anything** that is not [QueueState.pending] or
  /// [QueueState.inFlight] reads as blocked, so a value written by another build lands in the same
  /// bucket rather than in none.
  static const _blockedState = 'blocked';

  /// Records an operation the user has just performed.
  ///
  /// Returns once the row is committed, which is when a screen may confirm it (`Docs/07` §4: the
  /// UI confirms immediately, marked clearly as pending). Throws a [QueueRefusal] rather than
  /// queueing something that could never be sent.
  ///
  /// [recordedAt] is when the **user acted**, and the caller supplies it because only the caller
  /// knows: `Docs/02` §3.1 keeps that clock distinct from every other, and a queue reading its own
  /// clock at insert time would quietly substitute the wrong one.
  ///
  /// [idempotencyKey] is minted here by default. That is the correct place for it — `Docs/07` §4
  /// generates the key where the user acts and reuses it unchanged for every retry, and for a
  /// queued operation this call *is* the moment the user acted. It is a parameter only so a test
  /// can name the key it is asserting on, and so a screen that already minted one for an attempt
  /// it made online can queue the same action under the same key.
  Future<QueuedOperation> enqueue({
    required OperationKind kind,
    required String orderingKey,
    required String method,
    required String path,
    required Map<String, dynamic> body,
    required DateTime recordedAt,
    String? attachmentPath,
    String? idempotencyKey,
  }) async {
    final encoded = jsonEncode(body);
    final size = utf8.encode(encoded).length;
    if (size > policy.maxBodyBytes) {
      throw QueueOperationTooLarge(size: size, limit: policy.maxBodyBytes);
    }

    final key = idempotencyKey ?? _mintKey();
    final enqueuedAt = _now();

    return _db.transaction(() async {
      final total = await count();
      if (total >= policy.maxOperations) {
        throw QueueAtCapacity(limit: policy.maxOperations);
      }

      // Checked here as well as by the UNIQUE constraint behind it. The constraint is what makes
      // it true — including against a raw statement that never came through this method — and this
      // is what turns it into an error a caller can read rather than a driver-level exception.
      final existing = await (_db.select(_db.queuedOperations)
            ..where((t) => t.idempotencyKey.equals(key))
            ..limit(1))
          .get();
      if (existing.isNotEmpty) {
        throw QueueDuplicateOperation(idempotencyKey: key);
      }

      final id = await _db.into(_db.queuedOperations).insert(
            QueuedOperationsCompanion.insert(
              idempotencyKey: key,
              kind: kind.name,
              bodyVersion: kind.bodyVersion,
              orderingKey: orderingKey,
              method: method,
              path: path,
              body: encoded,
              attachmentPath: Value(attachmentPath),
              recordedAt: recordedAt,
              enqueuedAt: enqueuedAt,
              state: QueueState.pending.wire,
            ),
          );

      return QueuedOperation(
        id: id,
        idempotencyKey: key,
        kind: kind,
        orderingKey: orderingKey,
        method: method,
        path: path,
        body: body,
        attachmentPath: attachmentPath,
        recordedAt: recordedAt,
        enqueuedAt: enqueuedAt,
        state: QueueState.pending,
        attempts: 0,
      );
    });
  }

  /// Every row, counted in SQL.
  ///
  /// The number a row cannot hide from: it depends on nothing decoding, on no kind being known,
  /// and on no state being recognised.
  Future<int> count() async {
    final row = await _db.customSelect(
      'SELECT COUNT(*) AS c FROM queued_operations',
      readsFrom: {_db.queuedOperations},
    ).getSingle();
    return row.read<int>('c');
  }

  /// The whole queue, partitioned, from one read.
  ///
  /// **This is a read that writes**, and it is the only one. Reading is the moment a build
  /// discovers it cannot make sense of a row, and the alternatives are worse in a way that matters
  /// here: skipping the row drops the operation, and throwing stalls every other operation behind
  /// the one that cannot be read. So the discovery is recorded as a quarantine and the row moves to
  /// [QueueSnapshot.blocked], where it is counted and can be shown.
  Future<QueueSnapshot> snapshot() async {
    final total = await count();
    final rows = await _rowsInOrder();

    final pending = <QueuedOperation>[];
    final inFlight = <QueuedOperation>[];
    final blocked = <BlockedOperation>[];

    for (final row in rows) {
      // A row whose state is neither of the two live ones is already blocked — by [block], or by a
      // build that wrote a state this one has no name for. Either way it keeps the reason it
      // carries; re-quarantining it would overwrite a platform refusal with "unreadable".
      if (!_isLive(row.state)) {
        blocked.add(_blockedFrom(row, QueueBlockReason.fromWire(row.blockedReason), row.blockedDetail));
        continue;
      }

      final reading = _read(row);
      final operation = reading.operation;
      if (operation == null) {
        await _writeBlock(row.id, reading.reason!, reading.detail);
        blocked.add(_blockedFrom(row, reading.reason!, reading.detail));
        continue;
      }

      if (operation.state == QueueState.pending) {
        pending.add(operation);
      } else {
        inFlight.add(operation);
      }
    }

    return QueueSnapshot(total: total, pending: pending, inFlight: inFlight, blocked: blocked);
  }

  /// Returns anything a crash left in flight to [QueueState.pending], and says how many.
  ///
  /// Called at start-up and on resume by the sync worker (SHIP-125). There is no lease and no
  /// timeout, deliberately: a handset runs one app process, so the only other party that could
  /// hold a claim is the process that died, and the next launch is where that is known.
  ///
  /// [QueuedOperation.attempts] is **not** adjusted. The claim already counted the attempt, and it
  /// did happen — the send may even have reached the platform, which is precisely why the row
  /// carries the same idempotency key it was created with.
  Future<int> recover() {
    return (_db.update(_db.queuedOperations)
          ..where((t) => t.state.equals(QueueState.inFlight.wire)))
        .write(const QueuedOperationsCompanion(state: Value(_pendingWire)));
  }

  /// Takes the next operation that may be sent, marking it in flight. `null` when there is none.
  ///
  /// The claim is a conditional `UPDATE` whose affected-row count is checked, so two callers can
  /// never both hold the same operation — the loser simply looks at the next candidate.
  ///
  /// An unreadable row met here is quarantined and the search continues, which is what stops one
  /// poison row stalling the queue while still holding its own ordering key.
  Future<QueuedOperation?> claim() async {
    final at = _now();
    final passedOver = <int>{};

    while (true) {
      final head = await _nextClaimableHead(at, passedOver);
      if (head == null) return null;
      passedOver.add(head.id);

      final reading = _read(head);
      if (reading.operation == null) {
        await _writeBlock(head.id, reading.reason!, reading.detail);
        continue;
      }

      final taken = await (_db.update(_db.queuedOperations)
            ..where((t) => t.id.equals(head.id) & t.state.equals(QueueState.pending.wire)))
          .write(
        QueuedOperationsCompanion(
          state: const Value(_inFlightWire),
          attempts: Value(head.attempts + 1),
        ),
      );

      // Somebody else claimed it between the read and the write. It is no longer pending, so the
      // next pass will not offer it again.
      if (taken != 1) continue;

      final updated = await (_db.select(_db.queuedOperations)
            ..where((t) => t.id.equals(head.id)))
          .getSingleOrNull();
      if (updated == null) continue;

      final claimed = _read(updated).operation;
      if (claimed != null) return claimed;
    }
  }

  /// Puts a claimed operation back, optionally not before [nextAttemptAt].
  ///
  /// The delay is stored rather than held in memory: a backoff that reset on every launch would be
  /// no backoff at all on a handset. **What** the delay should be is SHIP-125's to decide; the
  /// queue only remembers it.
  Future<bool> release(int id, {DateTime? nextAttemptAt}) async {
    final released = await (_db.update(_db.queuedOperations)
          ..where((t) => t.id.equals(id) & t.state.equals(QueueState.inFlight.wire)))
        .write(
      QueuedOperationsCompanion(
        state: const Value(_pendingWire),
        nextAttemptAt: Value(nextAttemptAt),
      ),
    );
    return released == 1;
  }

  /// Moves an operation to blocked: it cannot proceed and needs a person.
  ///
  /// Never removes it. That is the whole point — `Docs/02` §3.1 requires a queued update that lost
  /// to an administrative action to be retained and shown, not discarded.
  Future<bool> block(int id, {required QueueBlockReason reason, String? detail}) async {
    final blocked = await _writeBlock(id, reason, detail);
    return blocked == 1;
  }

  /// Removes an operation the platform accepted. Only ever a claimed one.
  ///
  /// Narrow on purpose: this is one of exactly three ways a row leaves the table, and a `DELETE`
  /// that would take a pending operation is a `DELETE` that can drop work nobody has sent.
  Future<bool> complete(int id) async {
    final removed = await (_db.delete(_db.queuedOperations)
          ..where((t) => t.id.equals(id) & t.state.equals(QueueState.inFlight.wire)))
        .go();
    return removed == 1;
  }

  /// Removes a blocked operation, because the user has seen it (SHIP-132).
  ///
  /// The second of the three removal paths, and the only one that acts on a blocked row — which is
  /// what makes "quarantined, not dropped" true: no machinery deletes these, a person does.
  Future<bool> acknowledge(int id) async {
    final removed = await (_db.delete(_db.queuedOperations)
          ..where(
            (t) => t.id.equals(id) & t.state.isNotIn(const [_pendingWire, _inFlightWire]),
          ))
        .go();
    return removed == 1;
  }

  /// Discards everything, and reports how much. The third removal path.
  ///
  /// `Docs/07` §3 clears the queue at sign-out, and one account's unsynced work must not be sent
  /// under another's credentials. The count is returned so that even this — the one bulk removal —
  /// is something the user can be told about rather than something that just happens.
  Future<int> clear() => _db.delete(_db.queuedOperations).go();

  Future<List<QueuedOperationRow>> _rowsInOrder() {
    return (_db.select(_db.queuedOperations)..orderBy([(t) => OrderingTerm.asc(t.id)])).get();
  }

  /// The lowest-id operation that may be claimed now, skipping ids already tried this pass.
  ///
  /// "Head" is per [QueuedOperation.orderingKey]: only the oldest row of each key is a candidate,
  /// so an in-flight or blocked operation holds everything behind it in its own key and holds
  /// nothing in any other. The evaluation is in Dart rather than in SQL because the table is
  /// bounded by [QueuePolicy.maxOperations] and the rule is the ticket's main decision — it should
  /// be readable.
  Future<QueuedOperationRow?> _nextClaimableHead(DateTime at, Set<int> passedOver) async {
    final rows = await _rowsInOrder();
    final headed = <String>{};

    for (final row in rows) {
      if (!headed.add(row.orderingKey)) continue; // Not the head of its key.
      if (passedOver.contains(row.id)) continue;
      if (row.state != QueueState.pending.wire) continue;

      final notBefore = row.nextAttemptAt;
      if (notBefore != null && notBefore.isAfter(at)) continue;

      return row;
    }

    return null;
  }

  Future<int> _writeBlock(int id, QueueBlockReason reason, String? detail) {
    return (_db.update(_db.queuedOperations)..where((t) => t.id.equals(id))).write(
      QueuedOperationsCompanion(
        state: const Value(_blockedState),
        blockedReason: Value(reason.wire),
        blockedDetail: Value(detail),
      ),
    );
  }

  static bool _isLive(String state) =>
      state == QueueState.pending.wire || state == QueueState.inFlight.wire;

  static BlockedOperation _blockedFrom(
    QueuedOperationRow row,
    QueueBlockReason reason,
    String? detail,
  ) {
    return BlockedOperation(
      id: row.id,
      idempotencyKey: row.idempotencyKey,
      kindName: row.kind,
      orderingKey: row.orderingKey,
      recordedAt: row.recordedAt,
      enqueuedAt: row.enqueuedAt,
      attempts: row.attempts,
      reason: reason,
      detail: detail,
    );
  }

  /// Reads a stored row, or says why this build cannot.
  ///
  /// Every failure here is a row another build wrote — a kind since removed, a body shape since
  /// changed, text that is not JSON at all. None of them can be prevented at enqueue on a device,
  /// which is why the answer is a reading that reports its own failure rather than an exception.
  static _RowReading _read(QueuedOperationRow row) {
    final kind = OperationKind.byName(row.kind);
    if (kind == null) {
      return _RowReading.unreadable(
        QueueBlockReason.unsupported,
        'This version of Shipper does not have an operation of kind "${row.kind}".',
      );
    }

    if (kind.bodyVersion != row.bodyVersion) {
      return _RowReading.unreadable(
        QueueBlockReason.unsupported,
        'Stored as ${row.kind} v${row.bodyVersion}; this version writes v${kind.bodyVersion}.',
      );
    }

    final QueueState state;
    if (row.state == QueueState.pending.wire) {
      state = QueueState.pending;
    } else if (row.state == QueueState.inFlight.wire) {
      state = QueueState.inFlight;
    } else {
      return _RowReading.unreadable(
        QueueBlockReason.unreadable,
        'Unrecognised state "${row.state}".',
      );
    }

    Object? decoded;
    try {
      decoded = jsonDecode(row.body);
    } on FormatException catch (e) {
      return _RowReading.unreadable(QueueBlockReason.unreadable, 'Body is not JSON: ${e.message}');
    }

    if (decoded is! Map<String, dynamic>) {
      return _RowReading.unreadable(
        QueueBlockReason.unreadable,
        'Body decoded to ${decoded.runtimeType}, not a JSON object.',
      );
    }

    return _RowReading.readable(
      QueuedOperation(
        id: row.id,
        idempotencyKey: row.idempotencyKey,
        kind: kind,
        orderingKey: row.orderingKey,
        method: row.method,
        path: row.path,
        body: decoded,
        attachmentPath: row.attachmentPath,
        recordedAt: row.recordedAt,
        enqueuedAt: row.enqueuedAt,
        state: state,
        attempts: row.attempts,
        nextAttemptAt: row.nextAttemptAt,
      ),
    );
  }

  // The wire strings again as compile-time constants, because a `const` companion needs them and
  // an enum's field is not one. `queued_operation.dart` remains where they are decided; a test
  // holds these two to it.
  static const _pendingWire = 'pending';
  static const _inFlightWire = 'in_flight';
}

/// The outcome of reading one stored row.
final class _RowReading {
  const _RowReading.readable(QueuedOperation this.operation)
      : reason = null,
        detail = null;

  const _RowReading.unreadable(QueueBlockReason this.reason, this.detail) : operation = null;

  final QueuedOperation? operation;
  final QueueBlockReason? reason;
  final String? detail;
}

/// The bounds the queue enforces.
///
/// **Injected rather than compiled in**, which is what `CLAUDE.md`'s rule about operational
/// pressure asks for in the only form available here. A compiled-in default is unavoidable — the
/// queue's entire purpose is to work when the platform cannot be reached, so it cannot wait for
/// the platform to tell it how big it may be — but a default is not the same as a constant, and
/// moving these needs a value passed to a constructor rather than a store release.
///
/// Neither number is tuned, and neither is trying to be. Their job is to make a queue that grows
/// without limit, and a body that could never be sent, impossible — which is the same reasoning
/// SHIP-135 records for the outbox's 16 KiB payload bound.
final class QueuePolicy {
  const QueuePolicy({this.maxOperations = 500, this.maxBodyBytes = 64 * 1024});

  /// How many operations may be held at once, blocked ones included.
  ///
  /// Blocked operations count because they occupy the device just as much, and because a queue
  /// filling with operations nobody has looked at should become somebody's problem before it
  /// becomes a disk problem.
  ///
  /// 500 is a long way past any real day's work — `Docs/01` §4.4 has five recordable milestones
  /// per job — and a long way short of anything a handset would notice.
  final int maxOperations;

  /// The largest request body an operation may carry.
  ///
  /// Comfortably above a milestone and far below anything that has to be an upload. A proof
  /// photograph is not a body: `Docs/07` §4 queues it as a compressed local file and
  /// [QueuedOperation.attachmentPath] points at it.
  final int maxBodyBytes;
}

/// An operation the queue would not accept.
///
/// Carries no user-facing copy on purpose. `Docs/07` §7 treats copy as build work and
/// `core/permissions/permission_copy.dart` is where the app's lives; a screen writes the sentence,
/// this says what happened.
sealed class QueueRefusal implements Exception {
  const QueueRefusal();

  /// Developer-facing. Safe for a log, never for a person.
  String get reason;

  @override
  String toString() => 'QueueRefusal: $reason';
}

/// The queue already holds [QueuePolicy.maxOperations] operations.
///
/// Refusing rather than evicting is the decision: dropping the oldest to make room for the newest
/// is a silent drop, and it drops the operation that has been waiting longest, which is the one
/// most likely to matter.
final class QueueAtCapacity extends QueueRefusal {
  const QueueAtCapacity({required this.limit});

  final int limit;

  @override
  String get reason => 'the queue already holds $limit operations';
}

/// The body is larger than [QueuePolicy.maxBodyBytes].
final class QueueOperationTooLarge extends QueueRefusal {
  const QueueOperationTooLarge({required this.size, required this.limit});

  final int size;
  final int limit;

  @override
  String get reason => 'body is $size bytes, over the $limit byte limit';
}

/// An operation with this idempotency key is already queued.
///
/// Which means the action has already been recorded. Queueing it again would put two rows in front
/// of the platform under one key — the second of which the platform would answer by replaying the
/// first, so the user would see one result for two things they believed they did.
final class QueueDuplicateOperation extends QueueRefusal {
  const QueueDuplicateOperation({required this.idempotencyKey});

  final String idempotencyKey;

  @override
  String get reason => 'an operation with idempotency key $idempotencyKey is already queued';
}

/// The application's queue database.
///
/// **Reading this in a host test opens a real file**, because `driftDatabase` resolves a platform
/// directory through `path_provider`. Tests override it with a database over a temporary file, the
/// same way they override `tokenStoreProvider` rather than reaching a real keychain. Nothing else
/// should construct a [QueueDatabase]: a second one over a second file is a second queue, and the
/// operations in it would never be sent.
final queueDatabaseProvider = Provider<QueueDatabase>((ref) {
  final database = QueueDatabase(driftDatabase(name: 'shipper_queue'));
  ref.onDispose(database.close);
  return database;
});

/// The application's operation queue.
final operationQueueProvider = Provider<OperationQueue>((ref) {
  return OperationQueue(ref.watch(queueDatabaseProvider));
});
