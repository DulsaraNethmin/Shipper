// The seams SHIP-143's tests substitute.
//
// A fake repository rather than a stub transport for the behaviour tests, because a widget or
// lifecycle test that also exercised `dio`'s wiring would fail for two reasons and read as one.
// What actually reaches the wire is `notifications_repository_test.dart`'s subject, against
// contracts/paths/notifications.yaml.

import 'dart:async';

import 'package:shipper/features/notifications/device_token.dart';
import 'package:shipper/features/notifications/notifications_repository.dart';
import 'package:shipper/features/notifications/push_token_source.dart';

/// One recorded registration.
typedef RegisterCall = ({String token, DevicePlatform platform, String idempotencyKey});

/// One recorded deregistration.
typedef DeregisterCall = ({String accessToken, String idempotencyKey});

/// A [NotificationsRepository] that answers from a script and records what it was asked.
class FakeNotificationsRepository implements NotificationsRepository {
  final registrations = <RegisterCall>[];
  final deregistrations = <DeregisterCall>[];

  /// What a successful registration answers with.
  DeviceToken recorded = const DeviceToken(
    id: '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
    platform: 'android',
    registeredAt: '2026-08-15T02:11:04.000Z',
  );

  /// Thrown by [register] instead of answering, on every call until it is cleared.
  Object? registerFailure;

  /// Thrown by [deregister] instead of answering.
  Object? deregisterFailure;

  @override
  Future<DeviceToken> register({
    required String token,
    required DevicePlatform platform,
    required String idempotencyKey,
  }) async {
    registrations.add((token: token, platform: platform, idempotencyKey: idempotencyKey));

    final thrown = registerFailure;
    if (thrown != null) throw thrown;

    return recorded;
  }

  @override
  Future<void> deregister({
    required String accessToken,
    required String idempotencyKey,
  }) async {
    deregistrations.add((accessToken: accessToken, idempotencyKey: idempotencyKey));

    final thrown = deregisterFailure;
    if (thrown != null) throw thrown;
  }
}

/// A [PushTokenSource] a test drives.
///
/// The real one does not exist — no Firebase project does either — so this is what stands in for
/// it. See `push_token_source.dart`, which records the gap rather than hiding it.
class FakePushTokenSource implements PushTokenSource {
  FakePushTokenSource({this.token = 'fMEr9Xk2Q3aBcDeF:APA91bHq'});

  /// What [current] answers with. `null` is the ordinary "no permission yet" case.
  String? token;

  /// Thrown by [current], which is a platform channel that failed.
  Object? failure;

  int reads = 0;

  final _rotations = StreamController<String>.broadcast();

  /// Emits a token Firebase issued after the first one.
  void rotate(String next) => _rotations.add(next);

  Future<void> close() => _rotations.close();

  @override
  Future<String?> current() async {
    reads++;
    final thrown = failure;
    if (thrown != null) throw thrown;
    return token;
  }

  @override
  Stream<String> get refreshes => _rotations.stream;
}
