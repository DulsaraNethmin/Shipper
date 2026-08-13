import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/fleet/fleet_controller.dart';
import 'package:shipper/features/fleet/fleet_repository.dart';
import 'package:shipper/features/fleet/vehicle.dart';

part 'vehicle_controller.freezed.dart';

/// One vehicle, as the provider who owns it is looking at it (SHIP-98).
@freezed
abstract class VehicleState with _$VehicleState {
  const factory VehicleState({
    /// The vehicle is being read, or re-read after a failure.
    ///
    /// Deliberately not set by a pull-to-refresh, for the same reason as the fleet list:
    /// `RefreshIndicator` draws its own spinner, and replacing the vehicle with a second one would
    /// take it away from somebody who pulled precisely to look at it.
    @Default(true) bool loading,

    /// An action is in flight. The screen disables its buttons while it is true, which is what stops
    /// a second tap becoming a second action.
    @Default(false) bool acting,

    /// Whether the provider has asked to change the vehicle's details.
    ///
    /// The screen has two faces — the vehicle, and the form that edits it — and which one is showing
    /// is a fact about the screen's state rather than about the widget. Keeping it here is what lets
    /// the sequence be read, and tested, without a screen. Same shape, and the same reasoning, as
    /// `JobLocationsState.editing`.
    @Default(false) bool editing,

    /// The vehicle, once it has arrived.
    Vehicle? vehicle,

    /// What the last read or action failed with, or `null`.
    ApiFailure? failure,
  }) = _VehicleState;

  const VehicleState._();

  /// Nothing has arrived and nothing has failed — the only state a spinner belongs in.
  bool get isFirstLoad => loading && vehicle == null && failure == null;

  /// Nothing arrived and the reason is a failure worth offering a retry for.
  bool get failedOutright => vehicle == null && failure != null;

  /// Whether the edit form is the thing to show.
  bool get showingForm => editing && vehicle != null;
}

/// Reads one vehicle, edits it, and takes it off the road or puts it back (SHIP-98).
///
/// ## A family, keyed by the vehicle's id
///
/// The screen is reached by tapping a vehicle, so the id is what identifies this state. Keying the
/// provider by it rather than passing the id into one shared controller is what makes two vehicles
/// opened one after the other two states rather than one that has to be reset — and a reset somebody
/// forgets is a screen showing the previous truck's plate for a frame.
///
/// **Auto-disposed**, like the list, and for the same reason: `Docs/07` §3 requires cached data to go
/// with the token at sign-out, and a family entry kept alive would hold one account's registrations
/// in memory for whoever signed in next on the same handset.
///
/// ## Three keys, not one, and that is what `Docs/07` §4 means by "one per action"
///
/// Editing, taking off the road and returning to service are three actions, so each has its own
/// [ActionKey]. One shared key would make the second of them a retry of the first: the platform
/// fingerprints method, path and body, so it would be refused with `idempotency_key_reused` — a
/// button that stops working for a reason nobody looking at the screen could guess.
///
/// ## A refusal is rendered, not pre-empted, and the vehicle is re-read
///
/// Returning a vehicle to service can be refused: only one vehicle per plate may be in service, so a
/// replacement added while this one was off the road answers `409`. The app sends the request
/// regardless of what it believes, shows the platform's answer, **and re-reads the vehicle without
/// clearing that answer**. Reloading and clearing the refusal would leave a provider watching the
/// screen change with nothing to explain why their tap appeared to do nothing.
class VehicleController extends Notifier<VehicleState> {
  VehicleController(this.vehicleId);

  /// The vehicle this controller is about. From the route, which is the only place it comes from.
  final String vehicleId;

  final _saveKey = ActionKey();
  final _deactivateKey = ActionKey();
  final _reactivateKey = ActionKey();

  @override
  VehicleState build() {
    // Reading the repository is synchronous and the request is not: `_load` suspends at its first
    // await, so this returns before anything assigns to `state`.
    unawaited(_load());
    return const VehicleState();
  }

  /// Reads the vehicle again, keeping what is on screen while it is in flight.
  Future<void> refresh() => _load();

  /// Asks again after a failure, with the spinner back.
  Future<void> retry() {
    state = state.copyWith(loading: true, failure: null);
    return _load();
  }

  /// Shows the edit form.
  void edit() {
    if (state.vehicle != null) state = state.copyWith(editing: true, failure: null);
  }

  /// Leaves the edit form without saving.
  void stopEditing() => state = state.copyWith(editing: false, failure: null);

  /// Applies an edit, and answers whether the platform accepted it.
  ///
  /// **The form stays open on a refusal.** A `422` names the offending fields and the form renders
  /// each message under the input that caused it; closing the form would throw away what the
  /// provider typed along with the explanation of what was wrong with it.
  Future<bool> save(VehicleInput vehicle) async {
    if (state.acting) return false;

    final key = _saveKey.forRequest(vehicle.toJson());
    state = state.copyWith(acting: true, failure: null);

    try {
      final updated = await ref
          .read(fleetRepositoryProvider)
          .update(vehicleId: vehicleId, vehicle: vehicle, idempotencyKey: key);

      _saveKey.settled(null);
      if (!ref.mounted) return true;

      state = state.copyWith(acting: false, editing: false, vehicle: updated, failure: null);
      _fleetIsNowStale();
      return true;
    } on ApiFailure catch (failure) {
      _saveKey.settled(failure);
      _failedActing(failure);
      return false;
    } catch (error) {
      // Past ApiClient's mapping: a `200` whose body is not a vehicle. Nothing here establishes
      // whether the edit was applied, so the key is kept and the vehicle is re-read.
      const failure = ApiMalformedResponse(statusCode: 0);
      _saveKey.settled(failure);
      _failedActing(failure);
      unawaited(_load(keepingFailure: true));
      return false;
    }
  }

  /// Takes the vehicle out of service, and answers whether the platform accepted it.
  ///
  /// A vehicle already out of service answers `200` and changes nothing, so there is no special case
  /// for it: the vehicle comes back deactivated either way.
  Future<bool> deactivate() => _activate(
        key: _deactivateKey,
        send: (repository, idempotencyKey) => repository.deactivate(
          vehicleId: vehicleId,
          idempotencyKey: idempotencyKey,
        ),
      );

  /// Returns the vehicle to service, and answers whether the platform accepted it.
  ///
  /// **This is the one that can be refused.** See the note on the class.
  Future<bool> reactivate() => _activate(
        key: _reactivateKey,
        send: (repository, idempotencyKey) => repository.reactivate(
          vehicleId: vehicleId,
          idempotencyKey: idempotencyKey,
        ),
      );

  /// The two activation verbs, which differ only in which endpoint they call.
  ///
  /// The key is minted against `null` because neither request carries a body — [ActionKey]
  /// fingerprints what is sent, and what is sent here is nothing. They are separate keys because
  /// they are separate actions; see the note on the class.
  Future<bool> _activate({
    required ActionKey key,
    required Future<Vehicle> Function(FleetRepository, String) send,
  }) async {
    if (state.acting) return false;

    final idempotencyKey = key.forRequest(null);
    state = state.copyWith(acting: true, failure: null);

    try {
      final updated = await send(ref.read(fleetRepositoryProvider), idempotencyKey);

      key.settled(null);
      if (!ref.mounted) return true;

      state = state.copyWith(acting: false, vehicle: updated, failure: null);
      _fleetIsNowStale();
      return true;
    } on ApiFailure catch (failure) {
      key.settled(failure);
      _failedActing(failure);

      // A refusal the platform made against a fleet this client did not have — a replacement on the
      // same plate, added on another device. Reload, so what the provider is looking at is what the
      // platform holds, **while keeping the refusal on screen**.
      if (failure is ApiErrorResponse && failure.statusCode == 409) {
        unawaited(_load(keepingFailure: true));
      }
      return false;
    } catch (error) {
      const failure = ApiMalformedResponse(statusCode: 0);
      key.settled(failure);
      _failedActing(failure);
      unawaited(_load(keepingFailure: true));
      return false;
    }
  }

  /// Reads the vehicle.
  ///
  /// [keepingFailure] is for the reload that follows a refused action: the vehicle is reconciled to
  /// what the platform holds, and what the platform said about the attempt stays on screen.
  Future<void> _load({bool keepingFailure = false}) async {
    try {
      final vehicle = await ref.read(fleetRepositoryProvider).vehicle(vehicleId: vehicleId);
      if (!ref.mounted) return;

      state = state.copyWith(
        loading: false,
        acting: false,
        vehicle: vehicle,
        failure: keepingFailure ? state.failure : null,
      );
    } on ApiFailure catch (failure) {
      _failedLoading(failure);
    } catch (error) {
      // A `200` that is not a vehicle — the contract broken rather than a field added.
      _failedLoading(const ApiMalformedResponse(statusCode: 0));
    }
  }

  /// The fleet list is now wrong about this vehicle.
  ///
  /// Invalidated rather than edited in place, for the reason the customer's job list is: the platform
  /// decides what the vehicle became — the plate is normalised, the deactivation is stamped — so the
  /// list re-reads rather than guessing.
  void _fleetIsNowStale() => ref.invalidate(fleetProvider);

  /// Records a failed read **without discarding the vehicle already on screen**.
  void _failedLoading(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(loading: false, failure: failure);
  }

  void _failedActing(ApiFailure failure) {
    if (!ref.mounted) return;
    state = state.copyWith(acting: false, failure: failure);
  }
}

/// One vehicle, keyed by its id.
final vehicleProvider = NotifierProvider.autoDispose
    .family<VehicleController, VehicleState, String>(VehicleController.new);
