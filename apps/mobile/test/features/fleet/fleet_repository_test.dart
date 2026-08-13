// What actually reaches the wire, checked against contracts/paths/fleet.yaml.
//
// The screens are tested against a fake repository, which is the right seam for them and the wrong
// one for this: a fake cannot notice that an edit went out as a POST and added a second vehicle, or
// that deactivation was sent as a DELETE to a resource that has none. The platform refuses unknown
// fields on these bodies (httpx.DecodeJSON is strict), so a key name is not a detail — a wrong one
// is a 400 from a build already on somebody's phone.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/features/fleet/fleet_repository.dart';
import 'package:shipper/features/fleet/vehicle.dart';

class _StubAdapter implements HttpClientAdapter {
  _StubAdapter(this.respond);

  final ResponseBody Function(RequestOptions options) respond;
  final requests = <RequestOptions>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    return respond(options);
  }

  @override
  void close({bool force = false}) {}
}

ResponseBody _json(Object body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

({FleetRepository repo, _StubAdapter adapter}) _repoReturning(Object body, {int status = 200}) {
  final adapter = _StubAdapter((_) => _json(body, status: status));
  final dio = buildDio(baseUrl: 'http://localhost:8092')..httpClientAdapter = adapter;
  return (repo: ApiFleetRepository(ApiClient(dio)), adapter: adapter);
}

/// A vehicle as the platform answers with one.
const _vehicle = <String, Object?>{
  'id': '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0',
  'registration': 'ABC123',
  'vehicle_type': 'van',
  'active': true,
  'created_at': '2026-08-11T03:30:00.000Z',
  'updated_at': '2026-08-11T03:30:00.000Z',
};

const _input = VehicleInput(registration: 'abc 123', vehicleType: VehicleType.van);

Map<String, Object?> _sentBody(RequestOptions options) {
  final Object? data = options.data;
  return data is Map<String, Object?> ? data : <String, Object?>{};
}

void main() {
  group('reading the fleet', () {
    test('lists from /v1/fleet/vehicles with no filter and no limit', () async {
      final (:repo, :adapter) = _repoReturning(<String, Object?>{
        'data': <Object?>[_vehicle],
        'has_more': false,
      });

      final page = await repo.vehicles();

      expect(adapter.requests.single.method, 'GET');
      expect(adapter.requests.single.path, '/v1/fleet/vehicles');
      // No `active`, because this screen shows both halves of the fleet, and no `limit`, because
      // the page size is server configuration a client must not compile in.
      expect(adapter.requests.single.queryParameters, isEmpty);
      expect(page.data.single.registration, 'ABC123');
    });

    test('a read carries no idempotency key', () async {
      // It changes nothing, and the middleware lets read-only methods through untouched. A key here
      // would be a client claiming a read is an action.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{
        'data': <Object?>[],
        'has_more': false,
      });

      await repo.vehicles();

      expect(adapter.requests.single.headers.containsKey(ApiHeaders.idempotencyKey), isFalse);
    });

    test('the cursor is passed back exactly as it arrived', () async {
      const cursor = 'MR8yMDI2LTA4LTExVDAzOjMwOjAwWh8wMTk4ZjJjMS02YjQwLTdhMTEtOWMzZS0yZjlhNGQ1MWI3ZTA';
      final (:repo, :adapter) = _repoReturning(<String, Object?>{
        'data': <Object?>[],
        'has_more': false,
      });

      await repo.vehicles(cursor: cursor);

      expect(adapter.requests.single.queryParameters['cursor'], cursor);
    });

    test('an empty cursor is not sent at all', () async {
      // "" is not a position; sending it would ask the endpoint to read an encoding it never issued.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{
        'data': <Object?>[],
        'has_more': false,
      });

      await repo.vehicles(cursor: '');

      expect(adapter.requests.single.queryParameters, isEmpty);
    });

    test('reads one vehicle by id', () async {
      final (:repo, :adapter) = _repoReturning(_vehicle);

      final vehicle = await repo.vehicle(vehicleId: 'v-1');

      expect(adapter.requests.single.method, 'GET');
      expect(adapter.requests.single.path, '/v1/fleet/vehicles/v-1');
      expect(vehicle.id, '0198f2c1-6b40-7a11-9c3e-2f9a4d51b7e0');
    });
  });

  group('writing', () {
    test('adding is a POST to the collection, with the key the caller minted', () async {
      final (:repo, :adapter) = _repoReturning(_vehicle, status: 201);

      await repo.add(vehicle: _input, idempotencyKey: 'key-1');

      expect(adapter.requests.single.method, 'POST');
      expect(adapter.requests.single.path, '/v1/fleet/vehicles');
      expect(adapter.requests.single.headers[ApiHeaders.idempotencyKey], 'key-1');
      // Sent as typed. The platform normalises the plate; two normalisers that disagree is how a
      // provider ends up unable to find the vehicle they just added.
      expect(_sentBody(adapter.requests.single)['registration'], 'abc 123');
    });

    test('editing is a PATCH against the vehicle, not a POST that would add a second', () async {
      final (:repo, :adapter) = _repoReturning(_vehicle);

      await repo.update(vehicleId: 'v-1', vehicle: _input, idempotencyKey: 'key-2');

      expect(adapter.requests.single.method, 'PATCH');
      expect(adapter.requests.single.path, '/v1/fleet/vehicles/v-1');
      expect(adapter.requests.single.headers[ApiHeaders.idempotencyKey], 'key-2');
    });

    test('both writes send the same eight keys', () async {
      // One schema for both operations, which is the contract's own decision: a field a provider can
      // set when adding is a field they can change afterwards, and two lists would drift.
      final add = _repoReturning(_vehicle, status: 201);
      final edit = _repoReturning(_vehicle);

      await add.repo.add(vehicle: _input, idempotencyKey: 'key-1');
      await edit.repo.update(vehicleId: 'v-1', vehicle: _input, idempotencyKey: 'key-2');

      expect(
        _sentBody(add.adapter.requests.single).keys,
        _sentBody(edit.adapter.requests.single).keys,
      );
    });
  });

  group('taking a vehicle off the road and putting it back', () {
    test('deactivating is a POST to a verb under the resource', () async {
      // Not a DELETE, and not a PATCH setting a field. The client names an intent; the platform
      // decides what the state becomes.
      final (:repo, :adapter) = _repoReturning(<String, Object?>{..._vehicle, 'active': false});

      final vehicle = await repo.deactivate(vehicleId: 'v-1', idempotencyKey: 'key-3');

      expect(adapter.requests.single.method, 'POST');
      expect(adapter.requests.single.path, '/v1/fleet/vehicles/v-1/deactivate');
      expect(vehicle.active, isFalse);
    });

    test('reactivating is its counterpart', () async {
      final (:repo, :adapter) = _repoReturning(_vehicle);

      final vehicle = await repo.reactivate(vehicleId: 'v-1', idempotencyKey: 'key-4');

      expect(adapter.requests.single.path, '/v1/fleet/vehicles/v-1/reactivate');
      expect(vehicle.active, isTrue);
    });

    test('neither sends a body, and both send an idempotency key', () async {
      // The contract says both take no body. `{}` — which cancelling a job sends, because that
      // endpoint decodes one — would be a body the platform never reads. The key still travels,
      // because these change state.
      final (:repo, :adapter) = _repoReturning(_vehicle);

      await repo.deactivate(vehicleId: 'v-1', idempotencyKey: 'key-3');

      expect(adapter.requests.single.data, isNull);
      expect(adapter.requests.single.headers[ApiHeaders.idempotencyKey], 'key-3');
    });
  });
}
