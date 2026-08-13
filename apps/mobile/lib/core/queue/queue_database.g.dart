// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'queue_database.dart';

// ignore_for_file: type=lint
class $QueuedOperationsTable extends QueuedOperations
    with TableInfo<$QueuedOperationsTable, QueuedOperationRow> {
  @override
  final GeneratedDatabase attachedDatabase;
  final String? _alias;
  $QueuedOperationsTable(this.attachedDatabase, [this._alias]);
  static const VerificationMeta _idMeta = const VerificationMeta('id');
  @override
  late final GeneratedColumn<int> id = GeneratedColumn<int>(
    'id',
    aliasedName,
    false,
    hasAutoIncrement: true,
    type: DriftSqlType.int,
    requiredDuringInsert: false,
    defaultConstraints: GeneratedColumn.constraintIsAlways(
      'PRIMARY KEY AUTOINCREMENT',
    ),
  );
  static const VerificationMeta _idempotencyKeyMeta = const VerificationMeta(
    'idempotencyKey',
  );
  @override
  late final GeneratedColumn<String> idempotencyKey = GeneratedColumn<String>(
    'idempotency_key',
    aliasedName,
    false,
    type: DriftSqlType.string,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _kindMeta = const VerificationMeta('kind');
  @override
  late final GeneratedColumn<String> kind = GeneratedColumn<String>(
    'kind',
    aliasedName,
    false,
    type: DriftSqlType.string,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _bodyVersionMeta = const VerificationMeta(
    'bodyVersion',
  );
  @override
  late final GeneratedColumn<int> bodyVersion = GeneratedColumn<int>(
    'body_version',
    aliasedName,
    false,
    type: DriftSqlType.int,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _orderingKeyMeta = const VerificationMeta(
    'orderingKey',
  );
  @override
  late final GeneratedColumn<String> orderingKey = GeneratedColumn<String>(
    'ordering_key',
    aliasedName,
    false,
    type: DriftSqlType.string,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _methodMeta = const VerificationMeta('method');
  @override
  late final GeneratedColumn<String> method = GeneratedColumn<String>(
    'method',
    aliasedName,
    false,
    type: DriftSqlType.string,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _pathMeta = const VerificationMeta('path');
  @override
  late final GeneratedColumn<String> path = GeneratedColumn<String>(
    'path',
    aliasedName,
    false,
    type: DriftSqlType.string,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _bodyMeta = const VerificationMeta('body');
  @override
  late final GeneratedColumn<String> body = GeneratedColumn<String>(
    'body',
    aliasedName,
    false,
    type: DriftSqlType.string,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _attachmentPathMeta = const VerificationMeta(
    'attachmentPath',
  );
  @override
  late final GeneratedColumn<String> attachmentPath = GeneratedColumn<String>(
    'attachment_path',
    aliasedName,
    true,
    type: DriftSqlType.string,
    requiredDuringInsert: false,
  );
  static const VerificationMeta _recordedAtMeta = const VerificationMeta(
    'recordedAt',
  );
  @override
  late final GeneratedColumn<DateTime> recordedAt = GeneratedColumn<DateTime>(
    'recorded_at',
    aliasedName,
    false,
    type: DriftSqlType.dateTime,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _enqueuedAtMeta = const VerificationMeta(
    'enqueuedAt',
  );
  @override
  late final GeneratedColumn<DateTime> enqueuedAt = GeneratedColumn<DateTime>(
    'enqueued_at',
    aliasedName,
    false,
    type: DriftSqlType.dateTime,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _stateMeta = const VerificationMeta('state');
  @override
  late final GeneratedColumn<String> state = GeneratedColumn<String>(
    'state',
    aliasedName,
    false,
    type: DriftSqlType.string,
    requiredDuringInsert: true,
  );
  static const VerificationMeta _attemptsMeta = const VerificationMeta(
    'attempts',
  );
  @override
  late final GeneratedColumn<int> attempts = GeneratedColumn<int>(
    'attempts',
    aliasedName,
    false,
    type: DriftSqlType.int,
    requiredDuringInsert: false,
    defaultValue: const Constant(0),
  );
  static const VerificationMeta _nextAttemptAtMeta = const VerificationMeta(
    'nextAttemptAt',
  );
  @override
  late final GeneratedColumn<DateTime> nextAttemptAt =
      GeneratedColumn<DateTime>(
        'next_attempt_at',
        aliasedName,
        true,
        type: DriftSqlType.dateTime,
        requiredDuringInsert: false,
      );
  static const VerificationMeta _blockedReasonMeta = const VerificationMeta(
    'blockedReason',
  );
  @override
  late final GeneratedColumn<String> blockedReason = GeneratedColumn<String>(
    'blocked_reason',
    aliasedName,
    true,
    type: DriftSqlType.string,
    requiredDuringInsert: false,
  );
  static const VerificationMeta _blockedDetailMeta = const VerificationMeta(
    'blockedDetail',
  );
  @override
  late final GeneratedColumn<String> blockedDetail = GeneratedColumn<String>(
    'blocked_detail',
    aliasedName,
    true,
    type: DriftSqlType.string,
    requiredDuringInsert: false,
  );
  @override
  List<GeneratedColumn> get $columns => [
    id,
    idempotencyKey,
    kind,
    bodyVersion,
    orderingKey,
    method,
    path,
    body,
    attachmentPath,
    recordedAt,
    enqueuedAt,
    state,
    attempts,
    nextAttemptAt,
    blockedReason,
    blockedDetail,
  ];
  @override
  String get aliasedName => _alias ?? actualTableName;
  @override
  String get actualTableName => $name;
  static const String $name = 'queued_operations';
  @override
  VerificationContext validateIntegrity(
    Insertable<QueuedOperationRow> instance, {
    bool isInserting = false,
  }) {
    final context = VerificationContext();
    final data = instance.toColumns(true);
    if (data.containsKey('id')) {
      context.handle(_idMeta, id.isAcceptableOrUnknown(data['id']!, _idMeta));
    }
    if (data.containsKey('idempotency_key')) {
      context.handle(
        _idempotencyKeyMeta,
        idempotencyKey.isAcceptableOrUnknown(
          data['idempotency_key']!,
          _idempotencyKeyMeta,
        ),
      );
    } else if (isInserting) {
      context.missing(_idempotencyKeyMeta);
    }
    if (data.containsKey('kind')) {
      context.handle(
        _kindMeta,
        kind.isAcceptableOrUnknown(data['kind']!, _kindMeta),
      );
    } else if (isInserting) {
      context.missing(_kindMeta);
    }
    if (data.containsKey('body_version')) {
      context.handle(
        _bodyVersionMeta,
        bodyVersion.isAcceptableOrUnknown(
          data['body_version']!,
          _bodyVersionMeta,
        ),
      );
    } else if (isInserting) {
      context.missing(_bodyVersionMeta);
    }
    if (data.containsKey('ordering_key')) {
      context.handle(
        _orderingKeyMeta,
        orderingKey.isAcceptableOrUnknown(
          data['ordering_key']!,
          _orderingKeyMeta,
        ),
      );
    } else if (isInserting) {
      context.missing(_orderingKeyMeta);
    }
    if (data.containsKey('method')) {
      context.handle(
        _methodMeta,
        method.isAcceptableOrUnknown(data['method']!, _methodMeta),
      );
    } else if (isInserting) {
      context.missing(_methodMeta);
    }
    if (data.containsKey('path')) {
      context.handle(
        _pathMeta,
        path.isAcceptableOrUnknown(data['path']!, _pathMeta),
      );
    } else if (isInserting) {
      context.missing(_pathMeta);
    }
    if (data.containsKey('body')) {
      context.handle(
        _bodyMeta,
        body.isAcceptableOrUnknown(data['body']!, _bodyMeta),
      );
    } else if (isInserting) {
      context.missing(_bodyMeta);
    }
    if (data.containsKey('attachment_path')) {
      context.handle(
        _attachmentPathMeta,
        attachmentPath.isAcceptableOrUnknown(
          data['attachment_path']!,
          _attachmentPathMeta,
        ),
      );
    }
    if (data.containsKey('recorded_at')) {
      context.handle(
        _recordedAtMeta,
        recordedAt.isAcceptableOrUnknown(data['recorded_at']!, _recordedAtMeta),
      );
    } else if (isInserting) {
      context.missing(_recordedAtMeta);
    }
    if (data.containsKey('enqueued_at')) {
      context.handle(
        _enqueuedAtMeta,
        enqueuedAt.isAcceptableOrUnknown(data['enqueued_at']!, _enqueuedAtMeta),
      );
    } else if (isInserting) {
      context.missing(_enqueuedAtMeta);
    }
    if (data.containsKey('state')) {
      context.handle(
        _stateMeta,
        state.isAcceptableOrUnknown(data['state']!, _stateMeta),
      );
    } else if (isInserting) {
      context.missing(_stateMeta);
    }
    if (data.containsKey('attempts')) {
      context.handle(
        _attemptsMeta,
        attempts.isAcceptableOrUnknown(data['attempts']!, _attemptsMeta),
      );
    }
    if (data.containsKey('next_attempt_at')) {
      context.handle(
        _nextAttemptAtMeta,
        nextAttemptAt.isAcceptableOrUnknown(
          data['next_attempt_at']!,
          _nextAttemptAtMeta,
        ),
      );
    }
    if (data.containsKey('blocked_reason')) {
      context.handle(
        _blockedReasonMeta,
        blockedReason.isAcceptableOrUnknown(
          data['blocked_reason']!,
          _blockedReasonMeta,
        ),
      );
    }
    if (data.containsKey('blocked_detail')) {
      context.handle(
        _blockedDetailMeta,
        blockedDetail.isAcceptableOrUnknown(
          data['blocked_detail']!,
          _blockedDetailMeta,
        ),
      );
    }
    return context;
  }

  @override
  Set<GeneratedColumn> get $primaryKey => {id};
  @override
  List<Set<GeneratedColumn>> get uniqueKeys => [
    {idempotencyKey},
  ];
  @override
  QueuedOperationRow map(Map<String, dynamic> data, {String? tablePrefix}) {
    final effectivePrefix = tablePrefix != null ? '$tablePrefix.' : '';
    return QueuedOperationRow(
      id: attachedDatabase.typeMapping.read(
        DriftSqlType.int,
        data['${effectivePrefix}id'],
      )!,
      idempotencyKey: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}idempotency_key'],
      )!,
      kind: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}kind'],
      )!,
      bodyVersion: attachedDatabase.typeMapping.read(
        DriftSqlType.int,
        data['${effectivePrefix}body_version'],
      )!,
      orderingKey: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}ordering_key'],
      )!,
      method: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}method'],
      )!,
      path: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}path'],
      )!,
      body: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}body'],
      )!,
      attachmentPath: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}attachment_path'],
      ),
      recordedAt: attachedDatabase.typeMapping.read(
        DriftSqlType.dateTime,
        data['${effectivePrefix}recorded_at'],
      )!,
      enqueuedAt: attachedDatabase.typeMapping.read(
        DriftSqlType.dateTime,
        data['${effectivePrefix}enqueued_at'],
      )!,
      state: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}state'],
      )!,
      attempts: attachedDatabase.typeMapping.read(
        DriftSqlType.int,
        data['${effectivePrefix}attempts'],
      )!,
      nextAttemptAt: attachedDatabase.typeMapping.read(
        DriftSqlType.dateTime,
        data['${effectivePrefix}next_attempt_at'],
      ),
      blockedReason: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}blocked_reason'],
      ),
      blockedDetail: attachedDatabase.typeMapping.read(
        DriftSqlType.string,
        data['${effectivePrefix}blocked_detail'],
      ),
    );
  }

  @override
  $QueuedOperationsTable createAlias(String alias) {
    return $QueuedOperationsTable(attachedDatabase, alias);
  }
}

class QueuedOperationRow extends DataClass
    implements Insertable<QueuedOperationRow> {
  /// Also the enqueue sequence. `AUTOINCREMENT`, so an id is never reused after a delete — which
  /// matters because ordering by id is how the queue keeps a driver's recorded sequence.
  final int id;

  /// Minted at enqueue and held for the row's life (`Docs/07` §4).
  ///
  /// Unique, and the constraint is the point: enqueueing the same key twice is refused by SQLite
  /// rather than by a check somebody could forget to write. It is the same reasoning as the
  /// partial unique index behind one-accepted-bid-per-job (SHIP-80).
  final String idempotencyKey;
  final String kind;

  /// The shape `body` was written in, by the build that wrote it. See [OperationKind.bodyVersion].
  final int bodyVersion;
  final String orderingKey;
  final String method;
  final String path;

  /// The request body as JSON text.
  final String body;

  /// A local file to send with the operation — a compressed proof photograph (SHIP-130).
  final String? attachmentPath;

  /// When the user acted, and when the row committed (`Docs/02` §3.1's first clock).
  final DateTime recordedAt;
  final DateTime enqueuedAt;
  final String state;
  final int attempts;

  /// Backoff, made durable so an app restart does not reset it. Written by the sync worker.
  final DateTime? nextAttemptAt;
  final String? blockedReason;
  final String? blockedDetail;
  const QueuedOperationRow({
    required this.id,
    required this.idempotencyKey,
    required this.kind,
    required this.bodyVersion,
    required this.orderingKey,
    required this.method,
    required this.path,
    required this.body,
    this.attachmentPath,
    required this.recordedAt,
    required this.enqueuedAt,
    required this.state,
    required this.attempts,
    this.nextAttemptAt,
    this.blockedReason,
    this.blockedDetail,
  });
  @override
  Map<String, Expression> toColumns(bool nullToAbsent) {
    final map = <String, Expression>{};
    map['id'] = Variable<int>(id);
    map['idempotency_key'] = Variable<String>(idempotencyKey);
    map['kind'] = Variable<String>(kind);
    map['body_version'] = Variable<int>(bodyVersion);
    map['ordering_key'] = Variable<String>(orderingKey);
    map['method'] = Variable<String>(method);
    map['path'] = Variable<String>(path);
    map['body'] = Variable<String>(body);
    if (!nullToAbsent || attachmentPath != null) {
      map['attachment_path'] = Variable<String>(attachmentPath);
    }
    map['recorded_at'] = Variable<DateTime>(recordedAt);
    map['enqueued_at'] = Variable<DateTime>(enqueuedAt);
    map['state'] = Variable<String>(state);
    map['attempts'] = Variable<int>(attempts);
    if (!nullToAbsent || nextAttemptAt != null) {
      map['next_attempt_at'] = Variable<DateTime>(nextAttemptAt);
    }
    if (!nullToAbsent || blockedReason != null) {
      map['blocked_reason'] = Variable<String>(blockedReason);
    }
    if (!nullToAbsent || blockedDetail != null) {
      map['blocked_detail'] = Variable<String>(blockedDetail);
    }
    return map;
  }

  QueuedOperationsCompanion toCompanion(bool nullToAbsent) {
    return QueuedOperationsCompanion(
      id: Value(id),
      idempotencyKey: Value(idempotencyKey),
      kind: Value(kind),
      bodyVersion: Value(bodyVersion),
      orderingKey: Value(orderingKey),
      method: Value(method),
      path: Value(path),
      body: Value(body),
      attachmentPath: attachmentPath == null && nullToAbsent
          ? const Value.absent()
          : Value(attachmentPath),
      recordedAt: Value(recordedAt),
      enqueuedAt: Value(enqueuedAt),
      state: Value(state),
      attempts: Value(attempts),
      nextAttemptAt: nextAttemptAt == null && nullToAbsent
          ? const Value.absent()
          : Value(nextAttemptAt),
      blockedReason: blockedReason == null && nullToAbsent
          ? const Value.absent()
          : Value(blockedReason),
      blockedDetail: blockedDetail == null && nullToAbsent
          ? const Value.absent()
          : Value(blockedDetail),
    );
  }

  factory QueuedOperationRow.fromJson(
    Map<String, dynamic> json, {
    ValueSerializer? serializer,
  }) {
    serializer ??= driftRuntimeOptions.defaultSerializer;
    return QueuedOperationRow(
      id: serializer.fromJson<int>(json['id']),
      idempotencyKey: serializer.fromJson<String>(json['idempotencyKey']),
      kind: serializer.fromJson<String>(json['kind']),
      bodyVersion: serializer.fromJson<int>(json['bodyVersion']),
      orderingKey: serializer.fromJson<String>(json['orderingKey']),
      method: serializer.fromJson<String>(json['method']),
      path: serializer.fromJson<String>(json['path']),
      body: serializer.fromJson<String>(json['body']),
      attachmentPath: serializer.fromJson<String?>(json['attachmentPath']),
      recordedAt: serializer.fromJson<DateTime>(json['recordedAt']),
      enqueuedAt: serializer.fromJson<DateTime>(json['enqueuedAt']),
      state: serializer.fromJson<String>(json['state']),
      attempts: serializer.fromJson<int>(json['attempts']),
      nextAttemptAt: serializer.fromJson<DateTime?>(json['nextAttemptAt']),
      blockedReason: serializer.fromJson<String?>(json['blockedReason']),
      blockedDetail: serializer.fromJson<String?>(json['blockedDetail']),
    );
  }
  @override
  Map<String, dynamic> toJson({ValueSerializer? serializer}) {
    serializer ??= driftRuntimeOptions.defaultSerializer;
    return <String, dynamic>{
      'id': serializer.toJson<int>(id),
      'idempotencyKey': serializer.toJson<String>(idempotencyKey),
      'kind': serializer.toJson<String>(kind),
      'bodyVersion': serializer.toJson<int>(bodyVersion),
      'orderingKey': serializer.toJson<String>(orderingKey),
      'method': serializer.toJson<String>(method),
      'path': serializer.toJson<String>(path),
      'body': serializer.toJson<String>(body),
      'attachmentPath': serializer.toJson<String?>(attachmentPath),
      'recordedAt': serializer.toJson<DateTime>(recordedAt),
      'enqueuedAt': serializer.toJson<DateTime>(enqueuedAt),
      'state': serializer.toJson<String>(state),
      'attempts': serializer.toJson<int>(attempts),
      'nextAttemptAt': serializer.toJson<DateTime?>(nextAttemptAt),
      'blockedReason': serializer.toJson<String?>(blockedReason),
      'blockedDetail': serializer.toJson<String?>(blockedDetail),
    };
  }

  QueuedOperationRow copyWith({
    int? id,
    String? idempotencyKey,
    String? kind,
    int? bodyVersion,
    String? orderingKey,
    String? method,
    String? path,
    String? body,
    Value<String?> attachmentPath = const Value.absent(),
    DateTime? recordedAt,
    DateTime? enqueuedAt,
    String? state,
    int? attempts,
    Value<DateTime?> nextAttemptAt = const Value.absent(),
    Value<String?> blockedReason = const Value.absent(),
    Value<String?> blockedDetail = const Value.absent(),
  }) => QueuedOperationRow(
    id: id ?? this.id,
    idempotencyKey: idempotencyKey ?? this.idempotencyKey,
    kind: kind ?? this.kind,
    bodyVersion: bodyVersion ?? this.bodyVersion,
    orderingKey: orderingKey ?? this.orderingKey,
    method: method ?? this.method,
    path: path ?? this.path,
    body: body ?? this.body,
    attachmentPath: attachmentPath.present
        ? attachmentPath.value
        : this.attachmentPath,
    recordedAt: recordedAt ?? this.recordedAt,
    enqueuedAt: enqueuedAt ?? this.enqueuedAt,
    state: state ?? this.state,
    attempts: attempts ?? this.attempts,
    nextAttemptAt: nextAttemptAt.present
        ? nextAttemptAt.value
        : this.nextAttemptAt,
    blockedReason: blockedReason.present
        ? blockedReason.value
        : this.blockedReason,
    blockedDetail: blockedDetail.present
        ? blockedDetail.value
        : this.blockedDetail,
  );
  QueuedOperationRow copyWithCompanion(QueuedOperationsCompanion data) {
    return QueuedOperationRow(
      id: data.id.present ? data.id.value : this.id,
      idempotencyKey: data.idempotencyKey.present
          ? data.idempotencyKey.value
          : this.idempotencyKey,
      kind: data.kind.present ? data.kind.value : this.kind,
      bodyVersion: data.bodyVersion.present
          ? data.bodyVersion.value
          : this.bodyVersion,
      orderingKey: data.orderingKey.present
          ? data.orderingKey.value
          : this.orderingKey,
      method: data.method.present ? data.method.value : this.method,
      path: data.path.present ? data.path.value : this.path,
      body: data.body.present ? data.body.value : this.body,
      attachmentPath: data.attachmentPath.present
          ? data.attachmentPath.value
          : this.attachmentPath,
      recordedAt: data.recordedAt.present
          ? data.recordedAt.value
          : this.recordedAt,
      enqueuedAt: data.enqueuedAt.present
          ? data.enqueuedAt.value
          : this.enqueuedAt,
      state: data.state.present ? data.state.value : this.state,
      attempts: data.attempts.present ? data.attempts.value : this.attempts,
      nextAttemptAt: data.nextAttemptAt.present
          ? data.nextAttemptAt.value
          : this.nextAttemptAt,
      blockedReason: data.blockedReason.present
          ? data.blockedReason.value
          : this.blockedReason,
      blockedDetail: data.blockedDetail.present
          ? data.blockedDetail.value
          : this.blockedDetail,
    );
  }

  @override
  String toString() {
    return (StringBuffer('QueuedOperationRow(')
          ..write('id: $id, ')
          ..write('idempotencyKey: $idempotencyKey, ')
          ..write('kind: $kind, ')
          ..write('bodyVersion: $bodyVersion, ')
          ..write('orderingKey: $orderingKey, ')
          ..write('method: $method, ')
          ..write('path: $path, ')
          ..write('body: $body, ')
          ..write('attachmentPath: $attachmentPath, ')
          ..write('recordedAt: $recordedAt, ')
          ..write('enqueuedAt: $enqueuedAt, ')
          ..write('state: $state, ')
          ..write('attempts: $attempts, ')
          ..write('nextAttemptAt: $nextAttemptAt, ')
          ..write('blockedReason: $blockedReason, ')
          ..write('blockedDetail: $blockedDetail')
          ..write(')'))
        .toString();
  }

  @override
  int get hashCode => Object.hash(
    id,
    idempotencyKey,
    kind,
    bodyVersion,
    orderingKey,
    method,
    path,
    body,
    attachmentPath,
    recordedAt,
    enqueuedAt,
    state,
    attempts,
    nextAttemptAt,
    blockedReason,
    blockedDetail,
  );
  @override
  bool operator ==(Object other) =>
      identical(this, other) ||
      (other is QueuedOperationRow &&
          other.id == this.id &&
          other.idempotencyKey == this.idempotencyKey &&
          other.kind == this.kind &&
          other.bodyVersion == this.bodyVersion &&
          other.orderingKey == this.orderingKey &&
          other.method == this.method &&
          other.path == this.path &&
          other.body == this.body &&
          other.attachmentPath == this.attachmentPath &&
          other.recordedAt == this.recordedAt &&
          other.enqueuedAt == this.enqueuedAt &&
          other.state == this.state &&
          other.attempts == this.attempts &&
          other.nextAttemptAt == this.nextAttemptAt &&
          other.blockedReason == this.blockedReason &&
          other.blockedDetail == this.blockedDetail);
}

class QueuedOperationsCompanion extends UpdateCompanion<QueuedOperationRow> {
  final Value<int> id;
  final Value<String> idempotencyKey;
  final Value<String> kind;
  final Value<int> bodyVersion;
  final Value<String> orderingKey;
  final Value<String> method;
  final Value<String> path;
  final Value<String> body;
  final Value<String?> attachmentPath;
  final Value<DateTime> recordedAt;
  final Value<DateTime> enqueuedAt;
  final Value<String> state;
  final Value<int> attempts;
  final Value<DateTime?> nextAttemptAt;
  final Value<String?> blockedReason;
  final Value<String?> blockedDetail;
  const QueuedOperationsCompanion({
    this.id = const Value.absent(),
    this.idempotencyKey = const Value.absent(),
    this.kind = const Value.absent(),
    this.bodyVersion = const Value.absent(),
    this.orderingKey = const Value.absent(),
    this.method = const Value.absent(),
    this.path = const Value.absent(),
    this.body = const Value.absent(),
    this.attachmentPath = const Value.absent(),
    this.recordedAt = const Value.absent(),
    this.enqueuedAt = const Value.absent(),
    this.state = const Value.absent(),
    this.attempts = const Value.absent(),
    this.nextAttemptAt = const Value.absent(),
    this.blockedReason = const Value.absent(),
    this.blockedDetail = const Value.absent(),
  });
  QueuedOperationsCompanion.insert({
    this.id = const Value.absent(),
    required String idempotencyKey,
    required String kind,
    required int bodyVersion,
    required String orderingKey,
    required String method,
    required String path,
    required String body,
    this.attachmentPath = const Value.absent(),
    required DateTime recordedAt,
    required DateTime enqueuedAt,
    required String state,
    this.attempts = const Value.absent(),
    this.nextAttemptAt = const Value.absent(),
    this.blockedReason = const Value.absent(),
    this.blockedDetail = const Value.absent(),
  }) : idempotencyKey = Value(idempotencyKey),
       kind = Value(kind),
       bodyVersion = Value(bodyVersion),
       orderingKey = Value(orderingKey),
       method = Value(method),
       path = Value(path),
       body = Value(body),
       recordedAt = Value(recordedAt),
       enqueuedAt = Value(enqueuedAt),
       state = Value(state);
  static Insertable<QueuedOperationRow> custom({
    Expression<int>? id,
    Expression<String>? idempotencyKey,
    Expression<String>? kind,
    Expression<int>? bodyVersion,
    Expression<String>? orderingKey,
    Expression<String>? method,
    Expression<String>? path,
    Expression<String>? body,
    Expression<String>? attachmentPath,
    Expression<DateTime>? recordedAt,
    Expression<DateTime>? enqueuedAt,
    Expression<String>? state,
    Expression<int>? attempts,
    Expression<DateTime>? nextAttemptAt,
    Expression<String>? blockedReason,
    Expression<String>? blockedDetail,
  }) {
    return RawValuesInsertable({
      if (id != null) 'id': id,
      if (idempotencyKey != null) 'idempotency_key': idempotencyKey,
      if (kind != null) 'kind': kind,
      if (bodyVersion != null) 'body_version': bodyVersion,
      if (orderingKey != null) 'ordering_key': orderingKey,
      if (method != null) 'method': method,
      if (path != null) 'path': path,
      if (body != null) 'body': body,
      if (attachmentPath != null) 'attachment_path': attachmentPath,
      if (recordedAt != null) 'recorded_at': recordedAt,
      if (enqueuedAt != null) 'enqueued_at': enqueuedAt,
      if (state != null) 'state': state,
      if (attempts != null) 'attempts': attempts,
      if (nextAttemptAt != null) 'next_attempt_at': nextAttemptAt,
      if (blockedReason != null) 'blocked_reason': blockedReason,
      if (blockedDetail != null) 'blocked_detail': blockedDetail,
    });
  }

  QueuedOperationsCompanion copyWith({
    Value<int>? id,
    Value<String>? idempotencyKey,
    Value<String>? kind,
    Value<int>? bodyVersion,
    Value<String>? orderingKey,
    Value<String>? method,
    Value<String>? path,
    Value<String>? body,
    Value<String?>? attachmentPath,
    Value<DateTime>? recordedAt,
    Value<DateTime>? enqueuedAt,
    Value<String>? state,
    Value<int>? attempts,
    Value<DateTime?>? nextAttemptAt,
    Value<String?>? blockedReason,
    Value<String?>? blockedDetail,
  }) {
    return QueuedOperationsCompanion(
      id: id ?? this.id,
      idempotencyKey: idempotencyKey ?? this.idempotencyKey,
      kind: kind ?? this.kind,
      bodyVersion: bodyVersion ?? this.bodyVersion,
      orderingKey: orderingKey ?? this.orderingKey,
      method: method ?? this.method,
      path: path ?? this.path,
      body: body ?? this.body,
      attachmentPath: attachmentPath ?? this.attachmentPath,
      recordedAt: recordedAt ?? this.recordedAt,
      enqueuedAt: enqueuedAt ?? this.enqueuedAt,
      state: state ?? this.state,
      attempts: attempts ?? this.attempts,
      nextAttemptAt: nextAttemptAt ?? this.nextAttemptAt,
      blockedReason: blockedReason ?? this.blockedReason,
      blockedDetail: blockedDetail ?? this.blockedDetail,
    );
  }

  @override
  Map<String, Expression> toColumns(bool nullToAbsent) {
    final map = <String, Expression>{};
    if (id.present) {
      map['id'] = Variable<int>(id.value);
    }
    if (idempotencyKey.present) {
      map['idempotency_key'] = Variable<String>(idempotencyKey.value);
    }
    if (kind.present) {
      map['kind'] = Variable<String>(kind.value);
    }
    if (bodyVersion.present) {
      map['body_version'] = Variable<int>(bodyVersion.value);
    }
    if (orderingKey.present) {
      map['ordering_key'] = Variable<String>(orderingKey.value);
    }
    if (method.present) {
      map['method'] = Variable<String>(method.value);
    }
    if (path.present) {
      map['path'] = Variable<String>(path.value);
    }
    if (body.present) {
      map['body'] = Variable<String>(body.value);
    }
    if (attachmentPath.present) {
      map['attachment_path'] = Variable<String>(attachmentPath.value);
    }
    if (recordedAt.present) {
      map['recorded_at'] = Variable<DateTime>(recordedAt.value);
    }
    if (enqueuedAt.present) {
      map['enqueued_at'] = Variable<DateTime>(enqueuedAt.value);
    }
    if (state.present) {
      map['state'] = Variable<String>(state.value);
    }
    if (attempts.present) {
      map['attempts'] = Variable<int>(attempts.value);
    }
    if (nextAttemptAt.present) {
      map['next_attempt_at'] = Variable<DateTime>(nextAttemptAt.value);
    }
    if (blockedReason.present) {
      map['blocked_reason'] = Variable<String>(blockedReason.value);
    }
    if (blockedDetail.present) {
      map['blocked_detail'] = Variable<String>(blockedDetail.value);
    }
    return map;
  }

  @override
  String toString() {
    return (StringBuffer('QueuedOperationsCompanion(')
          ..write('id: $id, ')
          ..write('idempotencyKey: $idempotencyKey, ')
          ..write('kind: $kind, ')
          ..write('bodyVersion: $bodyVersion, ')
          ..write('orderingKey: $orderingKey, ')
          ..write('method: $method, ')
          ..write('path: $path, ')
          ..write('body: $body, ')
          ..write('attachmentPath: $attachmentPath, ')
          ..write('recordedAt: $recordedAt, ')
          ..write('enqueuedAt: $enqueuedAt, ')
          ..write('state: $state, ')
          ..write('attempts: $attempts, ')
          ..write('nextAttemptAt: $nextAttemptAt, ')
          ..write('blockedReason: $blockedReason, ')
          ..write('blockedDetail: $blockedDetail')
          ..write(')'))
        .toString();
  }
}

abstract class _$QueueDatabase extends GeneratedDatabase {
  _$QueueDatabase(QueryExecutor e) : super(e);
  $QueueDatabaseManager get managers => $QueueDatabaseManager(this);
  late final $QueuedOperationsTable queuedOperations = $QueuedOperationsTable(
    this,
  );
  @override
  Iterable<TableInfo<Table, Object?>> get allTables =>
      allSchemaEntities.whereType<TableInfo<Table, Object?>>();
  @override
  List<DatabaseSchemaEntity> get allSchemaEntities => [queuedOperations];
}

typedef $$QueuedOperationsTableCreateCompanionBuilder =
    QueuedOperationsCompanion Function({
      Value<int> id,
      required String idempotencyKey,
      required String kind,
      required int bodyVersion,
      required String orderingKey,
      required String method,
      required String path,
      required String body,
      Value<String?> attachmentPath,
      required DateTime recordedAt,
      required DateTime enqueuedAt,
      required String state,
      Value<int> attempts,
      Value<DateTime?> nextAttemptAt,
      Value<String?> blockedReason,
      Value<String?> blockedDetail,
    });
typedef $$QueuedOperationsTableUpdateCompanionBuilder =
    QueuedOperationsCompanion Function({
      Value<int> id,
      Value<String> idempotencyKey,
      Value<String> kind,
      Value<int> bodyVersion,
      Value<String> orderingKey,
      Value<String> method,
      Value<String> path,
      Value<String> body,
      Value<String?> attachmentPath,
      Value<DateTime> recordedAt,
      Value<DateTime> enqueuedAt,
      Value<String> state,
      Value<int> attempts,
      Value<DateTime?> nextAttemptAt,
      Value<String?> blockedReason,
      Value<String?> blockedDetail,
    });

class $$QueuedOperationsTableFilterComposer
    extends Composer<_$QueueDatabase, $QueuedOperationsTable> {
  $$QueuedOperationsTableFilterComposer({
    required super.$db,
    required super.$table,
    super.joinBuilder,
    super.$addJoinBuilderToRootComposer,
    super.$removeJoinBuilderFromRootComposer,
  });
  ColumnFilters<int> get id => $composableBuilder(
    column: $table.id,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get idempotencyKey => $composableBuilder(
    column: $table.idempotencyKey,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get kind => $composableBuilder(
    column: $table.kind,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<int> get bodyVersion => $composableBuilder(
    column: $table.bodyVersion,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get orderingKey => $composableBuilder(
    column: $table.orderingKey,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get method => $composableBuilder(
    column: $table.method,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get path => $composableBuilder(
    column: $table.path,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get body => $composableBuilder(
    column: $table.body,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get attachmentPath => $composableBuilder(
    column: $table.attachmentPath,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<DateTime> get recordedAt => $composableBuilder(
    column: $table.recordedAt,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<DateTime> get enqueuedAt => $composableBuilder(
    column: $table.enqueuedAt,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get state => $composableBuilder(
    column: $table.state,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<int> get attempts => $composableBuilder(
    column: $table.attempts,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<DateTime> get nextAttemptAt => $composableBuilder(
    column: $table.nextAttemptAt,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get blockedReason => $composableBuilder(
    column: $table.blockedReason,
    builder: (column) => ColumnFilters(column),
  );

  ColumnFilters<String> get blockedDetail => $composableBuilder(
    column: $table.blockedDetail,
    builder: (column) => ColumnFilters(column),
  );
}

class $$QueuedOperationsTableOrderingComposer
    extends Composer<_$QueueDatabase, $QueuedOperationsTable> {
  $$QueuedOperationsTableOrderingComposer({
    required super.$db,
    required super.$table,
    super.joinBuilder,
    super.$addJoinBuilderToRootComposer,
    super.$removeJoinBuilderFromRootComposer,
  });
  ColumnOrderings<int> get id => $composableBuilder(
    column: $table.id,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get idempotencyKey => $composableBuilder(
    column: $table.idempotencyKey,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get kind => $composableBuilder(
    column: $table.kind,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<int> get bodyVersion => $composableBuilder(
    column: $table.bodyVersion,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get orderingKey => $composableBuilder(
    column: $table.orderingKey,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get method => $composableBuilder(
    column: $table.method,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get path => $composableBuilder(
    column: $table.path,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get body => $composableBuilder(
    column: $table.body,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get attachmentPath => $composableBuilder(
    column: $table.attachmentPath,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<DateTime> get recordedAt => $composableBuilder(
    column: $table.recordedAt,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<DateTime> get enqueuedAt => $composableBuilder(
    column: $table.enqueuedAt,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get state => $composableBuilder(
    column: $table.state,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<int> get attempts => $composableBuilder(
    column: $table.attempts,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<DateTime> get nextAttemptAt => $composableBuilder(
    column: $table.nextAttemptAt,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get blockedReason => $composableBuilder(
    column: $table.blockedReason,
    builder: (column) => ColumnOrderings(column),
  );

  ColumnOrderings<String> get blockedDetail => $composableBuilder(
    column: $table.blockedDetail,
    builder: (column) => ColumnOrderings(column),
  );
}

class $$QueuedOperationsTableAnnotationComposer
    extends Composer<_$QueueDatabase, $QueuedOperationsTable> {
  $$QueuedOperationsTableAnnotationComposer({
    required super.$db,
    required super.$table,
    super.joinBuilder,
    super.$addJoinBuilderToRootComposer,
    super.$removeJoinBuilderFromRootComposer,
  });
  GeneratedColumn<int> get id =>
      $composableBuilder(column: $table.id, builder: (column) => column);

  GeneratedColumn<String> get idempotencyKey => $composableBuilder(
    column: $table.idempotencyKey,
    builder: (column) => column,
  );

  GeneratedColumn<String> get kind =>
      $composableBuilder(column: $table.kind, builder: (column) => column);

  GeneratedColumn<int> get bodyVersion => $composableBuilder(
    column: $table.bodyVersion,
    builder: (column) => column,
  );

  GeneratedColumn<String> get orderingKey => $composableBuilder(
    column: $table.orderingKey,
    builder: (column) => column,
  );

  GeneratedColumn<String> get method =>
      $composableBuilder(column: $table.method, builder: (column) => column);

  GeneratedColumn<String> get path =>
      $composableBuilder(column: $table.path, builder: (column) => column);

  GeneratedColumn<String> get body =>
      $composableBuilder(column: $table.body, builder: (column) => column);

  GeneratedColumn<String> get attachmentPath => $composableBuilder(
    column: $table.attachmentPath,
    builder: (column) => column,
  );

  GeneratedColumn<DateTime> get recordedAt => $composableBuilder(
    column: $table.recordedAt,
    builder: (column) => column,
  );

  GeneratedColumn<DateTime> get enqueuedAt => $composableBuilder(
    column: $table.enqueuedAt,
    builder: (column) => column,
  );

  GeneratedColumn<String> get state =>
      $composableBuilder(column: $table.state, builder: (column) => column);

  GeneratedColumn<int> get attempts =>
      $composableBuilder(column: $table.attempts, builder: (column) => column);

  GeneratedColumn<DateTime> get nextAttemptAt => $composableBuilder(
    column: $table.nextAttemptAt,
    builder: (column) => column,
  );

  GeneratedColumn<String> get blockedReason => $composableBuilder(
    column: $table.blockedReason,
    builder: (column) => column,
  );

  GeneratedColumn<String> get blockedDetail => $composableBuilder(
    column: $table.blockedDetail,
    builder: (column) => column,
  );
}

class $$QueuedOperationsTableTableManager
    extends
        RootTableManager<
          _$QueueDatabase,
          $QueuedOperationsTable,
          QueuedOperationRow,
          $$QueuedOperationsTableFilterComposer,
          $$QueuedOperationsTableOrderingComposer,
          $$QueuedOperationsTableAnnotationComposer,
          $$QueuedOperationsTableCreateCompanionBuilder,
          $$QueuedOperationsTableUpdateCompanionBuilder,
          (
            QueuedOperationRow,
            BaseReferences<
              _$QueueDatabase,
              $QueuedOperationsTable,
              QueuedOperationRow
            >,
          ),
          QueuedOperationRow,
          PrefetchHooks Function()
        > {
  $$QueuedOperationsTableTableManager(
    _$QueueDatabase db,
    $QueuedOperationsTable table,
  ) : super(
        TableManagerState(
          db: db,
          table: table,
          createFilteringComposer: () =>
              $$QueuedOperationsTableFilterComposer($db: db, $table: table),
          createOrderingComposer: () =>
              $$QueuedOperationsTableOrderingComposer($db: db, $table: table),
          createComputedFieldComposer: () =>
              $$QueuedOperationsTableAnnotationComposer($db: db, $table: table),
          updateCompanionCallback:
              ({
                Value<int> id = const Value.absent(),
                Value<String> idempotencyKey = const Value.absent(),
                Value<String> kind = const Value.absent(),
                Value<int> bodyVersion = const Value.absent(),
                Value<String> orderingKey = const Value.absent(),
                Value<String> method = const Value.absent(),
                Value<String> path = const Value.absent(),
                Value<String> body = const Value.absent(),
                Value<String?> attachmentPath = const Value.absent(),
                Value<DateTime> recordedAt = const Value.absent(),
                Value<DateTime> enqueuedAt = const Value.absent(),
                Value<String> state = const Value.absent(),
                Value<int> attempts = const Value.absent(),
                Value<DateTime?> nextAttemptAt = const Value.absent(),
                Value<String?> blockedReason = const Value.absent(),
                Value<String?> blockedDetail = const Value.absent(),
              }) => QueuedOperationsCompanion(
                id: id,
                idempotencyKey: idempotencyKey,
                kind: kind,
                bodyVersion: bodyVersion,
                orderingKey: orderingKey,
                method: method,
                path: path,
                body: body,
                attachmentPath: attachmentPath,
                recordedAt: recordedAt,
                enqueuedAt: enqueuedAt,
                state: state,
                attempts: attempts,
                nextAttemptAt: nextAttemptAt,
                blockedReason: blockedReason,
                blockedDetail: blockedDetail,
              ),
          createCompanionCallback:
              ({
                Value<int> id = const Value.absent(),
                required String idempotencyKey,
                required String kind,
                required int bodyVersion,
                required String orderingKey,
                required String method,
                required String path,
                required String body,
                Value<String?> attachmentPath = const Value.absent(),
                required DateTime recordedAt,
                required DateTime enqueuedAt,
                required String state,
                Value<int> attempts = const Value.absent(),
                Value<DateTime?> nextAttemptAt = const Value.absent(),
                Value<String?> blockedReason = const Value.absent(),
                Value<String?> blockedDetail = const Value.absent(),
              }) => QueuedOperationsCompanion.insert(
                id: id,
                idempotencyKey: idempotencyKey,
                kind: kind,
                bodyVersion: bodyVersion,
                orderingKey: orderingKey,
                method: method,
                path: path,
                body: body,
                attachmentPath: attachmentPath,
                recordedAt: recordedAt,
                enqueuedAt: enqueuedAt,
                state: state,
                attempts: attempts,
                nextAttemptAt: nextAttemptAt,
                blockedReason: blockedReason,
                blockedDetail: blockedDetail,
              ),
          withReferenceMapper: (p0) => p0
              .map((e) => (e.readTable(table), BaseReferences(db, table, e)))
              .toList(),
          prefetchHooksCallback: null,
        ),
      );
}

typedef $$QueuedOperationsTableProcessedTableManager =
    ProcessedTableManager<
      _$QueueDatabase,
      $QueuedOperationsTable,
      QueuedOperationRow,
      $$QueuedOperationsTableFilterComposer,
      $$QueuedOperationsTableOrderingComposer,
      $$QueuedOperationsTableAnnotationComposer,
      $$QueuedOperationsTableCreateCompanionBuilder,
      $$QueuedOperationsTableUpdateCompanionBuilder,
      (
        QueuedOperationRow,
        BaseReferences<
          _$QueueDatabase,
          $QueuedOperationsTable,
          QueuedOperationRow
        >,
      ),
      QueuedOperationRow,
      PrefetchHooks Function()
    >;

class $QueueDatabaseManager {
  final _$QueueDatabase _db;
  $QueueDatabaseManager(this._db);
  $$QueuedOperationsTableTableManager get queuedOperations =>
      $$QueuedOperationsTableTableManager(_db, _db.queuedOperations);
}
