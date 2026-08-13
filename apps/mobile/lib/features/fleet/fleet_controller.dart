import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/fleet/fleet_repository.dart';
import 'package:shipper/features/fleet/vehicle.dart';

part 'fleet_controller.freezed.dart';

/// The provider's own fleet, as much of it as has been read (SHIP-98).
@freezed
abstract class FleetState with _$FleetState {
  const factory FleetState({
    /// The **first** page is being read, or a retry after a failure is.
    ///
    /// Deliberately not set by a pull-to-refresh: `RefreshIndicator` draws its own spinner, and
    /// replacing the list with a second one would take the fleet away from somebody who pulled
    /// precisely to look at it.
    @Default(true) bool loading,

    /// A further page is being read.
    @Default(false) bool loadingMore,

    /// Every vehicle read so far, newest first, in the platform's own order.
    @Default(<Vehicle>[]) List<Vehicle> vehicles,

    /// Whether a page has arrived at all.
    ///
    /// This is what separates "no vehicles" from "not yet asked", and the separation is the whole of
    /// the empty state: a provider who has just signed up must see an empty state and never a
    /// spinner that does not resolve.
    @Default(false) bool loaded,

    /// The position to ask from next. **Opaque** — passed back exactly as it arrived.
    String? nextCursor,

    /// Whether asking again would return anything.
    @Default(false) bool hasMore,

    /// What the last read failed with, or `null`.
    ApiFailure? failure,
  }) = _FleetState;

  const FleetState._();

  /// The provider has been asked and has nothing.
  bool get isEmpty => loaded && vehicles.isEmpty;

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && vehicles.isEmpty && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => !loaded && failure != null;

  /// The vehicles that can be offered on a job.
  List<Vehicle> get inService => vehicles.where((v) => v.active).toList(growable: false);

  /// The vehicles the provider has taken off the road.
  ///
  /// Shown rather than hidden, and that is the decision the contract asks for in as many words: a
  /// provider looking for the vehicle they want to bring back has to be able to find it. A fleet
  /// screen that quietly filtered these out would make reactivation unreachable.
  List<Vehicle> get outOfService => vehicles.where((v) => !v.active).toList(growable: false);
}

/// Reads the provider's own fleet (SHIP-98).
///
/// ## The list is read whole and split here, not asked for twice
///
/// `GET /v1/fleet/vehicles` takes `?active=`, and this does not use it. The default is every
/// vehicle, in service and off the road alike, which is what this screen needs. See
/// `FleetRepository.vehicles` for the argument; the short form is that a provider who cannot see a
/// retired vehicle cannot bring it back.
///
/// ## Paging is explicit, because a split list is only as complete as what was read
///
/// The page size is server configuration and nothing here names one. A group therefore holds *the
/// vehicles that have been read* rather than every vehicle in that state, so the further pages are
/// asked for by the provider rather than swallowed silently, and the screen says that it is showing
/// the most recently added. Following every cursor automatically would be a client deciding to read
/// an unbounded list to draw one screen.
class FleetController extends Notifier<FleetState> {
  @override
  FleetState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    unawaited(_load(replacing: true));
    return const FleetState();
  }

  /// Reads the first page again, keeping what is on screen while it is in flight.
  ///
  /// Returned rather than awaited internally so `RefreshIndicator` can hold its spinner until the
  /// read finishes — a pull that snapped back before the answer arrived would look like a refresh
  /// that did nothing.
  Future<void> refresh() => _load(replacing: true);

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load(replacing: true);
  }

  /// Reads the next page and appends it.
  Future<void> loadMore() {
    final cursor = state.nextCursor;
    if (cursor == null || state.loadingMore) return Future<void>.value();

    state = state.copyWith(loadingMore: true, failure: null);
    return _load(cursor: cursor, replacing: false);
  }

  Future<void> _load({String? cursor, required bool replacing}) async {
    try {
      final page = await ref.read(fleetRepositoryProvider).vehicles(cursor: cursor);
      if (!ref.mounted) return;

      state = state.copyWith(
        loading: false,
        loadingMore: false,
        loaded: true,
        vehicles: replacing ? page.data : <Vehicle>[...state.vehicles, ...page.data],
        nextCursor: page.nextCursor,
        hasMore: page.hasMore,
        failure: null,
      );
    } on ApiFailure catch (failure) {
      _failed(failure);
    } catch (error) {
      // Past ApiClient's mapping: a page whose rows are not vehicles. Nothing a provider can act on,
      // and the same thing to them as any other failure.
      _failed(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// Records a failure **without discarding the vehicles already on screen**.
  ///
  /// A refresh that fails in a depot with no signal should leave the provider looking at the fleet
  /// they had, with a banner saying the reload did not work.
  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, loadingMore: false, failure: failure);
  }
}

/// The provider's fleet.
///
/// **Auto-disposed, and that is what clears one provider's fleet before the next one signs in.**
/// `Docs/07` §3 requires cached data to go with the token at sign-out; sign-out unmounts the shell,
/// which drops the last listener, which disposes this. Kept alive it would hold the previous
/// account's registrations in memory and show them to whoever signed in next on the same handset —
/// on a shared phone that is a disclosure rather than a stale screen.
final fleetProvider = NotifierProvider.autoDispose<FleetController, FleetState>(
  FleetController.new,
);

/// What adding a vehicle is doing right now (SHIP-98).
@freezed
abstract class AddVehicleState with _$AddVehicleState {
  const factory AddVehicleState({
    /// A save is in flight. The screen disables its submit while it is true, which is what stops a
    /// second tap becoming a second **vehicle** — two taps are two actions, and an idempotency key
    /// is deliberately per-action (`Docs/07` §4).
    @Default(false) bool busy,

    /// What the last attempt failed with, or `null`. Cleared when the next one starts.
    ApiFailure? failure,
  }) = _AddVehicleState;
}

/// Adds one vehicle to the provider's fleet (SHIP-98).
///
/// ## The key is retained only when the outcome is genuinely unknown
///
/// [ActionKey] mints one key per action and keeps it across a retry of *that same request*. A
/// dropped connection after the platform committed is exactly what idempotency exists for: the
/// retry replays the stored answer instead of adding the truck twice. A `422` or a `409` retires the
/// key, because the platform saw the request and refused it — the provider is about to correct a
/// plate, and a replayed refusal is not what they asked for.
class AddVehicleController extends Notifier<AddVehicleState> {
  final _key = ActionKey();

  @override
  AddVehicleState build() => const AddVehicleState();

  /// Sends the vehicle, and answers with it when the platform accepted it — `null` otherwise.
  ///
  /// The vehicle rather than a bool, because the caller wants what the platform actually stored:
  /// the plate comes back normalised, which is worth showing to somebody who typed it with a space
  /// in it.
  Future<Vehicle?> add(VehicleInput vehicle) async {
    if (state.busy) return null;

    final key = _key.forRequest(vehicle.toJson());
    state = state.copyWith(busy: true, failure: null);

    try {
      final added =
          await ref.read(fleetRepositoryProvider).add(vehicle: vehicle, idempotencyKey: key);

      _key.settled(null);
      if (!ref.mounted) return added;

      state = const AddVehicleState();

      // The fleet list is now missing a vehicle. Invalidating it is cheaper and more honest than
      // inserting a row into another controller's state: the platform decides what was stored — the
      // plate is normalised on the way in — so the list re-reads rather than guessing.
      ref.invalidate(fleetProvider);
      return added;
    } on ApiFailure catch (failure) {
      _key.settled(failure);
      _failed(failure);
      return null;
    } catch (error) {
      // Past ApiClient's mapping: a `201` whose body is not a vehicle. Nothing here establishes
      // whether the platform stored it, so the key is kept — a retry replays rather than adding a
      // second truck on the same plate.
      const failure = ApiMalformedResponse(statusCode: 0);
      _key.settled(failure);
      _failed(failure);
      return null;
    }
  }

  void _failed(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(busy: false, failure: failure);
  }
}

/// The add-a-vehicle form's state.
///
/// Auto-disposed, so leaving the form and coming back is a new vehicle rather than a second attempt
/// at the last one — which is also what retires the idempotency key along with it.
final addVehicleProvider = NotifierProvider.autoDispose<AddVehicleController, AddVehicleState>(
  AddVehicleController.new,
);
