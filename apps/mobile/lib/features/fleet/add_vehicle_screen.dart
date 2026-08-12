import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/fleet/fleet_controller.dart';
import 'package:shipper/features/fleet/provider_only.dart';
import 'package:shipper/features/fleet/vehicle.dart';
import 'package:shipper/features/fleet/vehicle_form.dart';

/// Adds a vehicle to the provider's fleet (SHIP-98).
///
/// A screen of its own rather than a dialog on the fleet list, for the reason the job wizard has
/// one: it is a form with eight inputs, and a phone that rotates or raises a keyboard has nowhere
/// to put a dialog that size.
///
/// The form itself is [VehicleForm], shared with the edit path, because a field a provider can set
/// when adding a vehicle is a field they can change afterwards.
class AddVehicleScreen extends StatelessWidget {
  const AddVehicleScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Add a vehicle'),
        leading: IconButton(
          key: const Key('add-vehicle-close'),
          icon: const Icon(Icons.close),
          tooltip: 'Close',
          // `pop` when there is somewhere to go back to — the fleet, which is how this is normally
          // reached — and the fleet itself when there is not, which is what a deep link looks like.
          onPressed: () => context.canPop() ? context.pop() : context.go(Routes.fleet),
        ),
      ),
      body: SafeArea(child: const ProviderOnly(child: _AddVehicle())),
    );
  }
}

class _AddVehicle extends ConsumerWidget {
  const _AddVehicle();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(addVehicleProvider);

    return VehicleForm(
      initial: const VehicleInput(),
      submitLabel: 'Add this vehicle',
      busy: state.busy,
      failure: state.failure,
      onSubmit: (vehicle) => unawaited(_add(context, ref, vehicle)),
    );
  }

  /// Sends the vehicle, and leaves the screen only when the platform accepted it.
  ///
  /// **A refusal keeps the form on screen, filled in.** A `409` is a plate already in service in
  /// this fleet and a `422` names the fields it objected to — both are things a provider corrects
  /// in the form they are looking at, and a screen that closed itself would take the eight values
  /// away along with the explanation.
  Future<void> _add(BuildContext context, WidgetRef ref, VehicleInput vehicle) async {
    final added = await ref.read(addVehicleProvider.notifier).add(vehicle);
    if (added == null || !context.mounted) return;

    // The fleet list is invalidated by the controller, so the new vehicle is there when this pops
    // back to it. The plate arrives normalised, which is worth seeing: somebody who typed
    // `abc 123` finds `ABC123`, and the list is where they find it rather than a confirmation
    // screen they would have to leave anyway.
    if (context.canPop()) {
      context.pop();
    } else {
      context.go(Routes.fleet);
    }
  }
}
