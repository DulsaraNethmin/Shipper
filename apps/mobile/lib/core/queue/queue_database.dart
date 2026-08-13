import 'package:drift/drift.dart';

part 'queue_database.g.dart';

/// One row per operation the user has recorded and the platform has not yet accepted.
///
/// The column set is chosen so that **reading a row can never fail at the storage layer**. Every
/// column is a primitive; nothing is a foreign key; nothing is an enum the database enforces. The
/// interpretation — is this a kind we know, is this body the shape we write — happens in Dart,
/// where a failure can be recorded as a state change instead of throwing out of a query.
///
/// `state`, `kind` and `blocked_reason` are therefore `TEXT` rather than checked vocabularies. On
/// the platform side `jobs.status` is a `CHECK` constraint for the opposite reason and both are
/// right: the platform has one deployment and can refuse a status it has never heard of, while a
/// handset holds rows written by whatever build was installed last week.
@DataClassName('QueuedOperationRow')
class QueuedOperations extends Table {
  /// Also the enqueue sequence. `AUTOINCREMENT`, so an id is never reused after a delete — which
  /// matters because ordering by id is how the queue keeps a driver's recorded sequence.
  IntColumn get id => integer().autoIncrement()();

  /// Minted at enqueue and held for the row's life (`Docs/07` §4).
  ///
  /// Unique, and the constraint is the point: enqueueing the same key twice is refused by SQLite
  /// rather than by a check somebody could forget to write. It is the same reasoning as the
  /// partial unique index behind one-accepted-bid-per-job (SHIP-80).
  TextColumn get idempotencyKey => text()();

  TextColumn get kind => text()();

  /// The shape `body` was written in, by the build that wrote it. See [OperationKind.bodyVersion].
  IntColumn get bodyVersion => integer()();

  TextColumn get orderingKey => text()();

  TextColumn get method => text()();
  TextColumn get path => text()();

  /// The request body as JSON text.
  TextColumn get body => text()();

  /// A local file to send with the operation — a compressed proof photograph (SHIP-130).
  TextColumn get attachmentPath => text().nullable()();

  /// When the user acted, and when the row committed (`Docs/02` §3.1's first clock).
  DateTimeColumn get recordedAt => dateTime()();
  DateTimeColumn get enqueuedAt => dateTime()();

  TextColumn get state => text()();

  IntColumn get attempts => integer().withDefault(const Constant(0))();

  /// Backoff, made durable so an app restart does not reset it. Written by the sync worker.
  DateTimeColumn get nextAttemptAt => dateTime().nullable()();

  TextColumn get blockedReason => text().nullable()();
  TextColumn get blockedDetail => text().nullable()();

  @override
  List<Set<Column<Object>>> get uniqueKeys => [
        {idempotencyKey},
      ];
}

/// The queue's database. One table, and it is not shared with anything else.
///
/// Separate from cached reads on purpose. `Docs/07` §4 clears cached job data when a job closes
/// and at sign-out; the queue is cleared at sign-out only, and losing a cached read costs a
/// network request while losing a queued operation costs a driver's afternoon. Two databases make
/// that difference structural rather than a rule somebody has to remember when writing a
/// `DELETE`.
@DriftDatabase(tables: [QueuedOperations])
class QueueDatabase extends _$QueueDatabase {
  /// Takes its executor, so the application opens a file and a test opens a temporary one — the
  /// same shape `ApiClient` uses for its transport.
  QueueDatabase(super.executor);

  @override
  int get schemaVersion => 1;

  /// Timestamps are stored as ISO-8601 text rather than as unix seconds.
  ///
  /// Drift's default is seconds, which silently truncates sub-second precision and makes the raw
  /// column unreadable to a person. Two operations a driver recorded four hundred milliseconds
  /// apart would then share a `recorded_at`, and `Docs/02` §3.1's whole point is that the actor's
  /// clock is the one the customer sees.
  @override
  DriftDatabaseOptions get options =>
      const DriftDatabaseOptions(storeDateTimeAsText: true);
}
