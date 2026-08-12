import 'dart:async';

import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/fleet/fleet_repository.dart';
import 'package:shipper/features/fleet/vehicle.dart';

/// A vehicle the platform could have returned.
///
/// Built from the `Vehicle` example in `contracts/paths/fleet.yaml` so that a field renamed in the
/// contract shows up here rather than only on a device.
Vehicle aVehicle({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  String registration = 'ABC123',
  VehicleType vehicleType = VehicleType.van,
  bool active = true,
  String? make = 'Mercedes-Benz',
  String? model = 'Sprinter 314 CDI MWB',
  double? maxWeightKg = 1200,
  int? loadLengthCm = 320,
  int? loadWidthCm = 175,
  int? loadHeightCm = 190,
  String? deactivatedAt,
  String createdAt = '2026-08-11T03:30:00.000Z',
}) {
  return Vehicle(
    id: id,
    registration: registration,
    vehicleType: vehicleType,
    active: active,
    make: make,
    model: model,
    maxWeightKg: maxWeightKg,
    loadLengthCm: loadLengthCm,
    loadWidthCm: loadWidthCm,
    loadHeightCm: loadHeightCm,
    deactivatedAt: deactivatedAt,
    createdAt: createdAt,
    updatedAt: createdAt,
  );
}

/// A vehicle recorded with a plate and a type and nothing else.
///
/// **The ordinary case rather than a degenerate one.** `Docs/01` §4.2's measure is that a provider
/// can maintain a fleet, and somebody standing in a truck yard has the plate to hand and may not
/// know the load height — the platform omits every optional field they left alone, so this is what
/// most first vehicles look like.
Vehicle aBareVehicle({
  String id = '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e1',
  String registration = 'XYZ789',
  VehicleType vehicleType = VehicleType.ute,
  bool active = true,
}) {
  return aVehicle(
    id: id,
    registration: registration,
    vehicleType: vehicleType,
    active: active,
    make: null,
    model: null,
    maxWeightKg: null,
    loadLengthCm: null,
    loadWidthCm: null,
    loadHeightCm: null,
  );
}

/// One recorded call.
typedef FleetCall = ({
  String action,
  String? vehicleId,
  Map<String, Object?> fields,
  String? cursor,
  String? idempotencyKey,
});

/// A [FleetRepository] that answers from a script and records what it was asked.
///
/// The screens are tested against this rather than against a stub transport, because a widget test
/// that also exercises `dio`'s wiring fails for two reasons and reads as one. What actually reaches
/// the wire is `fleet_repository_test.dart`'s subject, against the contract.
///
/// It records the **idempotency key of every write**, which is what makes the rule in `Docs/07` §4
/// assertable: one key per action, kept across a retry of that action, and a new one when the
/// action changes. Built in the same shape as `FakeJobsRepository` on purpose — two fakes that
/// answer the same questions differently is two things to learn.
class FakeFleetRepository implements FleetRepository {
  final calls = <FleetCall>[];

  /// What the list answers with.
  ApiPage<Vehicle> page = const ApiPage<Vehicle>(data: <Vehicle>[]);

  /// What `GET /v1/fleet/vehicles/{id}` answers with.
  ///
  /// A function of the id, because the detail screen is reached with one and a fake that ignored it
  /// would let a screen showing the wrong vehicle pass. It answers the **same shape** the list does,
  /// which is the contract: one `Vehicle` schema for every operation.
  Vehicle Function(String vehicleId) detail = (id) => aVehicle(id: id);

  /// What an add or an edit answers with.
  ///
  /// A function of the body rather than a fixed value, because that is what the platform does — the
  /// plate comes back normalised and the screens show what was stored, not what was typed.
  Vehicle Function(Map<String, Object?> fields) written = _storedFrom;

  /// Set to make the next call of that action throw instead of answering.
  final failures = <String, Object>{};

  /// Set to hold the next call of that action open, so a test can assert what a screen shows while
  /// a request is in flight.
  final gates = <String, Completer<void>>{};

  List<FleetCall> callsTo(String action) => calls.where((c) => c.action == action).toList();

  /// Every idempotency key seen for [action], in order.
  List<String> keysFor(String action) => callsTo(action)
      .map((c) => c.idempotencyKey)
      .whereType<String>()
      .toList(growable: false);

  Future<T> _record<T>(FleetCall call, T Function() answer) async {
    calls.add(call);

    final gate = gates.remove(call.action);
    if (gate != null) await gate.future;

    final failure = failures.remove(call.action);
    if (failure != null) throw failure;

    return answer();
  }

  /// What the platform makes of a written body: the plate upper-cased with its spaces removed, and
  /// every empty value omitted rather than echoed back as `""` or `0`.
  static Vehicle _storedFrom(Map<String, Object?> fields) {
    String? text(String key) {
      final value = fields[key];
      return value is String && value.isNotEmpty ? value : null;
    }

    double? weight(String key) {
      final value = fields[key];
      final number = value is num ? value.toDouble() : null;
      return number == null || number == 0 ? null : number;
    }

    int? measure(String key) {
      final value = fields[key];
      final number = value is num ? value.toInt() : null;
      return number == null || number == 0 ? null : number;
    }

    final type = fields['vehicle_type'];

    return aVehicle(
      registration: (text('registration') ?? '').toUpperCase().replaceAll(' ', ''),
      vehicleType: VehicleType.values.firstWhere(
        (candidate) => candidate.wireName == type,
        orElse: () => VehicleType.van,
      ),
      make: text('make'),
      model: text('model'),
      maxWeightKg: weight('max_weight_kg'),
      loadLengthCm: measure('load_length_cm'),
      loadWidthCm: measure('load_width_cm'),
      loadHeightCm: measure('load_height_cm'),
    );
  }

  @override
  Future<ApiPage<Vehicle>> vehicles({String? cursor}) {
    return _record(
      (
        action: 'list',
        vehicleId: null,
        fields: const <String, Object?>{},
        cursor: cursor,
        idempotencyKey: null,
      ),
      () => page,
    );
  }

  @override
  Future<Vehicle> vehicle({required String vehicleId}) {
    return _record(
      (
        action: 'read',
        vehicleId: vehicleId,
        fields: const <String, Object?>{},
        cursor: null,
        idempotencyKey: null,
      ),
      () => detail(vehicleId),
    );
  }

  @override
  Future<Vehicle> add({required VehicleInput vehicle, required String idempotencyKey}) {
    final fields = vehicle.toJson();

    return _record(
      (
        action: 'add',
        vehicleId: null,
        fields: fields,
        cursor: null,
        idempotencyKey: idempotencyKey,
      ),
      () => written(fields),
    );
  }

  @override
  Future<Vehicle> update({
    required String vehicleId,
    required VehicleInput vehicle,
    required String idempotencyKey,
  }) {
    final fields = vehicle.toJson();

    return _record(
      (
        action: 'update',
        vehicleId: vehicleId,
        fields: fields,
        cursor: null,
        idempotencyKey: idempotencyKey,
      ),
      () {
        // What the platform does: the edit is applied and the vehicle comes back as it now is. The
        // answer is recorded against `detail` too, so a re-read agrees with what the edit returned.
        final stored = written(fields).copyWith(id: vehicleId, active: detail(vehicleId).active);
        detail = (_) => stored;
        return stored;
      },
    );
  }

  @override
  Future<Vehicle> deactivate({required String vehicleId, required String idempotencyKey}) {
    return _activation(vehicleId, 'deactivate', idempotencyKey, active: false);
  }

  @override
  Future<Vehicle> reactivate({required String vehicleId, required String idempotencyKey}) {
    return _activation(vehicleId, 'reactivate', idempotencyKey, active: true);
  }

  Future<Vehicle> _activation(
    String vehicleId,
    String action,
    String idempotencyKey, {
    required bool active,
  }) {
    return _record(
      (
        action: action,
        vehicleId: vehicleId,
        fields: const <String, Object?>{},
        cursor: null,
        idempotencyKey: idempotencyKey,
      ),
      () {
        final changed = detail(vehicleId).copyWith(
          active: active,
          // The platform stamps the day it came off the road, and clears it when it goes back on.
          deactivatedAt: active ? null : '2026-08-12T03:34:12.000Z',
        );
        detail = (_) => changed;
        return changed;
      },
    );
  }
}
