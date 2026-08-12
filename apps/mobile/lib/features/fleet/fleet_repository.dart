import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/fleet/vehicle.dart';

/// The fleet endpoints a provider screen calls (SHIP-98).
///
/// Six routes are served and these are all six. Every one of them is authenticated and every one is
/// about **the caller's own fleet**: there is no parameter anywhere for whose vehicles to read and
/// no field for naming an owner, because a provider id taken from client input would be an
/// authorisation decision made on the device, which `Docs/07` §3 puts on the platform.
///
/// A vehicle belonging to another provider answers `404`, byte-identically to one that does not
/// exist. That is not politeness — which vehicles a competitor runs is commercial information they
/// never published — and this client neither softens nor explains it.
///
/// ## What is deliberately absent
///
/// **There is no `deleteVehicle`.** There is no `DELETE` on this resource and the contract says
/// there will not be one: a vehicle is named by the bid that won a job and by the delivery that
/// followed it, so removing the row would remove part of a commercial record `Docs/05` §3.1
/// requires retaining. Taking a vehicle off the road is [deactivate] and bringing it back is
/// [reactivate] — the two operations `Docs/01` §4.2 actually gives a provider.
///
/// **There is nothing about a provider profile.** `GET` and `PATCH /v1/fleet/profile` — the service
/// area and specialties of SHIP-79 — are not served on this branch, and a client that modelled them
/// would be a client depending on a ticket that has not landed.
///
/// An interface with one real implementation, following `JobsRepository` for the same reason: a
/// widget test has to be able to hand a screen something that answers, and the alternative — a stub
/// transport under a concrete class — makes every screen test a test of `dio`'s wiring as well.
///
/// **Idempotency keys arrive from the caller and are never minted here.** `Docs/07` §4 has the key
/// generated once where the user acts and reused unchanged across every retry of that action; a
/// repository that minted its own would mint one per attempt. `ActionKey` is what callers use.
abstract interface class FleetRepository {
  /// `GET /v1/fleet/vehicles` (SHIP-78) — one page of the provider's own fleet, newest first.
  ///
  /// [cursor] is the `next_cursor` of a previous page, passed back **exactly** as it arrived. It is
  /// opaque: its encoding is the endpoint's business, and a cursor from an encoding no longer
  /// served is refused rather than misread.
  ///
  /// ## There is deliberately no `active` filter and no limit here
  ///
  /// The endpoint takes `?active=` and `?limit=`, and this takes neither.
  ///
  /// `?active=` narrows the list to what is in service, and this screen must show both. The
  /// contract says why in as many words — "a provider looking for the vehicle they want to bring
  /// back has to be able to see it" — and reactivating is one of the two operations the fleet
  /// screen exists for. Asking twice would be two requests to draw one screen and two chances for a
  /// partial failure to hide the group with no error to explain it. The split is done on the
  /// device, where it costs nothing. SHIP-100 asks the narrower question when a bid needs a vehicle
  /// it may offer, and that is when the parameter earns its place.
  ///
  /// `?limit=` is absent because the page size is server configuration (`Docs/10` §4.5). A number
  /// compiled into this client is a number that cannot be changed without a store release.
  ///
  /// No idempotency key: a read changes nothing, and the middleware lets read-only methods through
  /// untouched.
  Future<ApiPage<Vehicle>> vehicles({String? cursor});

  /// `GET /v1/fleet/vehicles/{id}` (SHIP-78) — one vehicle, in full, to the provider who owns it.
  ///
  /// **The same shape the list returns**, deliberately: the contract answers add, edit, read, list
  /// and both activation verbs with one `Vehicle` schema, so a client parses one type whatever it
  /// did to obtain the vehicle.
  Future<Vehicle> vehicle({required String vehicleId});

  /// `POST /v1/fleet/vehicles` (SHIP-78) — adds a vehicle, in service.
  ///
  /// `registration` and `vehicle_type` are required; everything else is optional, because
  /// `Docs/01` §4.2's measure is that a provider can maintain a fleet, and somebody standing in a
  /// truck yard has the plate to hand and may not know the load height.
  ///
  /// **A customer account is refused with `403 fleet_provider_only`**, decided against the account's
  /// stored role rather than against the role claim in the token. The app does not pre-empt that
  /// refusal; `provider_only.dart` explains the difference between not offering a surface and
  /// deciding an authorisation question.
  ///
  /// A registration already in service in this fleet answers `409 fleet_duplicate_registration`.
  Future<Vehicle> add({
    required VehicleInput vehicle,
    required String idempotencyKey,
  });

  /// `PATCH /v1/fleet/vehicles/{id}` (SHIP-78) — a partial edit of a vehicle the caller owns.
  ///
  /// `PATCH` rather than `PUT`: a provider correcting one field should not have to send every field
  /// they are not changing, which is how a client that has not been updated for a new field
  /// silently clears it. What this client sends is [VehicleInput.toJson], which explains why
  /// sending every field the *form* holds is not the same mistake.
  ///
  /// A vehicle out of service can still be edited, and editing it does not bring it back:
  /// correcting the plate on a truck that is off the road for a month is ordinary, and returning it
  /// to service is [reactivate] because that is the one that can collide with a replacement on the
  /// same plate.
  Future<Vehicle> update({
    required String vehicleId,
    required VehicleInput vehicle,
    required String idempotencyKey,
  });

  /// `POST /v1/fleet/vehicles/{id}/deactivate` (SHIP-78) — takes a vehicle out of service.
  ///
  /// **A verb under the resource, and not a field on the vehicle.** The client names an intent; the
  /// platform decides what the state becomes and answers with the vehicle as it now is. The request
  /// carries no body: there is nothing to say that is not already in the URL, and unlike cancelling
  /// a job there is no reason to record.
  ///
  /// Deactivating a vehicle that is already out of service answers `200` and changes nothing. That
  /// is not an error and needs no special case: it is a phone that lost its connection, restarted,
  /// and generated a fresh key for the same intent.
  Future<Vehicle> deactivate({
    required String vehicleId,
    required String idempotencyKey,
  });

  /// `POST /v1/fleet/vehicles/{id}/reactivate` (SHIP-78) — returns a vehicle to service.
  ///
  /// The counterpart of [deactivate], and **the one that can be refused**: only one vehicle per
  /// plate may be in service at a time, so a replacement added while this one was off the road
  /// answers `409 fleet_duplicate_registration`. A refusal the screen renders rather than one it
  /// pre-empts.
  ///
  /// A vehicle already in service answers `200`, unchanged.
  Future<Vehicle> reactivate({
    required String vehicleId,
    required String idempotencyKey,
  });
}

/// The real one, over [ApiClient].
///
/// Thin by construction. `Docs/10` §8.1 replaces hand-written calls with a client generated from
/// `contracts/openapi.yaml`, and what should survive that is the shape of the interface above and
/// nothing in this class.
final class ApiFleetRepository implements FleetRepository {
  const ApiFleetRepository(this._client);

  final ApiClient _client;

  /// Product endpoints live under `/v1` (SHIP-13). The base URL carries the host and nothing else,
  /// so the version prefix belongs here.
  static const _base = '/v1/fleet/vehicles';

  @override
  Future<ApiPage<Vehicle>> vehicles({String? cursor}) async {
    return ApiPage.fromJson(
      await _client.getJson(
        _base,
        query: <String, dynamic>{if (cursor != null && cursor.isNotEmpty) 'cursor': cursor},
      ),
      Vehicle.fromJson,
    );
  }

  @override
  Future<Vehicle> vehicle({required String vehicleId}) async {
    return Vehicle.fromJson(await _client.getJson('$_base/$vehicleId'));
  }

  @override
  Future<Vehicle> add({
    required VehicleInput vehicle,
    required String idempotencyKey,
  }) async {
    return Vehicle.fromJson(
      await _client.postJson(_base, idempotencyKey: idempotencyKey, body: vehicle.toJson()),
    );
  }

  @override
  Future<Vehicle> update({
    required String vehicleId,
    required VehicleInput vehicle,
    required String idempotencyKey,
  }) async {
    return Vehicle.fromJson(
      await _client.patchJson(
        '$_base/$vehicleId',
        idempotencyKey: idempotencyKey,
        body: vehicle.toJson(),
      ),
    );
  }

  @override
  Future<Vehicle> deactivate({
    required String vehicleId,
    required String idempotencyKey,
  }) {
    return _activation(vehicleId: vehicleId, verb: 'deactivate', idempotencyKey: idempotencyKey);
  }

  @override
  Future<Vehicle> reactivate({
    required String vehicleId,
    required String idempotencyKey,
  }) {
    return _activation(vehicleId: vehicleId, verb: 'reactivate', idempotencyKey: idempotencyKey);
  }

  /// The two activation verbs, which differ only in their name.
  ///
  /// **The body is left off on purpose**, which sends none at all. The contract says both requests
  /// take no body, and `{}` — which is what cancelling a job sends, because that endpoint does
  /// decode one — would be a body the platform never reads. `scripts/verify/60-fleet.sh` draws the
  /// same distinction from the other side, and says why: sending `{}` would demonstrate something
  /// other than what the contract states.
  ///
  /// The idempotency key still travels, because these change state and every state-changing request
  /// carries one (SHIP-15, `CLAUDE.md`).
  ///
  /// [ApiClient.postJson] rather than `postNoContent`: both answer `200` with the vehicle in full,
  /// so there is a body to read even though there was none to send.
  Future<Vehicle> _activation({
    required String vehicleId,
    required String verb,
    required String idempotencyKey,
  }) async {
    return Vehicle.fromJson(
      await _client.postJson('$_base/$vehicleId/$verb', idempotencyKey: idempotencyKey),
    );
  }
}

/// The application's fleet repository.
final fleetRepositoryProvider = Provider<FleetRepository>(
  (ref) => ApiFleetRepository(ref.watch(apiClientProvider)),
);
