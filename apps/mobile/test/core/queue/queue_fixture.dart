// A queue over a real SQLite file in a temporary directory.
//
// **The file matters, and an in-memory database would not do.** SHIP-124's *Done when* is that
// queued operations survive an app restart, and the closest a host test gets to that is closing the
// database and opening a second one over the same bytes on disk — which is exactly what a relaunch
// does. `NativeDatabase.memory()` would make every test below pass while proving nothing about
// durability, which is the failure mode the ticket is about.

import 'dart:io';

import 'package:drift/drift.dart';
import 'package:drift/native.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queue_database.dart';
import 'package:shipper/core/queue/queued_operation.dart';

/// One handset's storage: a directory that outlives the queues opened over it.
///
/// [open] is a launch of the app. Call it twice and the second one is the restart.
class QueueFixture {
  QueueFixture(this._directory);

  /// A fixture whose directory is removed when the test ends.
  factory QueueFixture.temporary() {
    final directory = Directory.systemTemp.createTempSync('shipper_queue_test');
    addTearDown(() {
      if (directory.existsSync()) directory.deleteSync(recursive: true);
    });
    return QueueFixture(directory);
  }

  final Directory _directory;

  QueueDatabase? _database;

  /// The database currently open, for the tests that write a row the way another build would.
  QueueDatabase get database {
    final open = _database;
    if (open == null) throw StateError('no queue is open; call open() first');
    return open;
  }

  File get file => File('${_directory.path}/queue.sqlite');

  /// Opens the queue, as a launch of the app would.
  ///
  /// [mintKey] and [now] are handed through so a test can name the keys and the times it asserts
  /// on rather than matching random hex against a wall clock.
  OperationQueue open({
    QueuePolicy policy = const QueuePolicy(),
    String Function()? mintKey,
    DateTime Function()? now,
  }) {
    final database = QueueDatabase(NativeDatabase(file));
    _database = database;
    addTearDown(database.close);
    return OperationQueue(database, policy: policy, mintKey: mintKey, now: now);
  }

  /// Closes the queue, as the process ending would.
  Future<void> close() async {
    await _database?.close();
    _database = null;
  }

  /// Reopens over the same file. The restart.
  Future<OperationQueue> restart({
    QueuePolicy policy = const QueuePolicy(),
    String Function()? mintKey,
    DateTime Function()? now,
  }) async {
    await close();
    return open(policy: policy, mintKey: mintKey, now: now);
  }

  /// The raw `state` column, which the typed API deliberately cannot express every value of.
  Future<String> stateOf(int id) async {
    final row = await database
        .customSelect(
          'SELECT state FROM queued_operations WHERE id = ?',
          variables: [Variable<int>(id)],
          readsFrom: {database.queuedOperations},
        )
        .getSingle();
    return row.read<String>('state');
  }

  /// Rewrites a row the way a different build of the app would have written it.
  ///
  /// Deliberately a raw statement over a row this build inserted, rather than a raw `INSERT`: the
  /// point is a row whose *contents* this build cannot interpret, and constructing the timestamps
  /// by hand would only add a way for the test to be wrong about the storage format.
  Future<void> overwrite(
    int id, {
    String? kind,
    int? bodyVersion,
    String? body,
    String? state,
  }) async {
    Future<void> set(String column, Variable<Object> value) async {
      await database.customStatement(
        'UPDATE queued_operations SET $column = ? WHERE id = ?',
        [value.value, id],
      );
    }

    if (kind != null) await set('kind', Variable<String>(kind));
    if (bodyVersion != null) await set('body_version', Variable<int>(bodyVersion));
    if (body != null) await set('body', Variable<String>(body));
    if (state != null) await set('state', Variable<String>(state));
  }
}

/// A milestone the driver recorded, which is what `Docs/01` §4.4 and `Docs/07` §4 queue first.
Future<QueuedOperation> enqueueMilestone(
  OperationQueue queue, {
  String job = 'job-1',
  String milestone = 'picked_up',
  DateTime? recordedAt,
  String? idempotencyKey,
}) {
  return queue.enqueue(
    kind: OperationKind.milestone,
    orderingKey: 'job:$job',
    method: 'POST',
    path: '/v1/jobs/$job/milestones',
    body: <String, dynamic>{'milestone': milestone},
    recordedAt: recordedAt ?? DateTime.utc(2026, 8, 13, 9, 30),
    idempotencyKey: idempotencyKey,
  );
}

/// Keys a test can read: `key-1`, `key-2`, …
String Function() sequentialKeys() {
  var next = 0;
  return () => 'key-${++next}';
}
