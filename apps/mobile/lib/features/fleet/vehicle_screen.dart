import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/fleet/provider_only.dart';
import 'package:shipper/features/fleet/vehicle.dart';
import 'package:shipper/features/fleet/vehicle_controller.dart';
import 'package:shipper/features/fleet/vehicle_form.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// One vehicle, in full, to the provider who owns it — and the three things they can do to it
/// (SHIP-98).
///
/// Reached by tapping a vehicle in the fleet. The route carries the id and nothing else: the vehicle
/// is re-read from `GET /v1/fleet/vehicles/{id}` rather than handed across, so a screen opened from
/// a list fetched ten minutes ago shows what the platform holds now, and a deep link reaches exactly
/// the same screen with nothing extra to supply.
///
/// ## Edit, take off the road, return to service — and no delete
///
/// There is no `DELETE` on this resource and there will not be one: a vehicle is named by the bid
/// that won a job and by the delivery that followed it, so removing the row would remove part of a
/// commercial record `Docs/05` §3.1 requires retaining. What `Docs/01` §4.2 gives a provider is
/// deactivation, which stops the vehicle making them eligible for new work and changes nothing about
/// work already under way. This screen says that in as many words, because "delete" is what somebody
/// will look for and not finding it needs an explanation rather than a silence.
class VehicleScreen extends StatelessWidget {
  const VehicleScreen({required this.vehicleId, super.key});

  /// The vehicle to show, from the route.
  final String vehicleId;

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Vehicle')),
      body: SafeArea(child: ProviderOnly(child: _Vehicle(vehicleId: vehicleId))),
    );
  }
}

class _Vehicle extends ConsumerWidget {
  const _Vehicle({required this.vehicleId});

  final String vehicleId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(vehicleProvider(vehicleId));
    final controller = ref.read(vehicleProvider(vehicleId).notifier);

    if (state.showingForm) {
      return VehicleForm(
        initial: VehicleInput.from(state.vehicle!),
        submitLabel: 'Save changes',
        busy: state.acting,
        failure: state.failure,
        onCancel: controller.stopEditing,
        onSubmit: (vehicle) => unawaited(controller.save(vehicle)),
      );
    }

    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        key: const Key('vehicle-detail'),
        // Always scrollable, so the pull works on the failure state as well as on a vehicle with
        // almost nothing recorded against it.
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: _children(context, theme, state, controller),
      ),
    );
  }

  List<Widget> _children(
    BuildContext context,
    ThemeData theme,
    VehicleState state,
    VehicleController controller,
  ) {
    final vehicle = state.vehicle;
    final failure = state.failure;

    if (state.isFirstLoad) {
      return const <Widget>[
        Padding(
          padding: EdgeInsets.symmetric(vertical: 48),
          child: Center(child: CircularProgressIndicator(key: Key('vehicle-detail-loading'))),
        ),
      ];
    }

    if (vehicle == null) {
      return <Widget>[
        if (failure != null) _CouldNotLoad(failure: failure, onRetry: controller.retry),
      ];
    }

    final since = dayFirstDate(vehicle.deactivatedAt);

    return <Widget>[
      Text(vehicle.registration, style: theme.textTheme.headlineSmall),
      const SizedBox(height: 4),
      Text(
        vehicle.active ? 'In service' : 'Off the road',
        // Keyed by the state rather than by the words, so a test names the fact and not the copy.
        key: Key(vehicle.active ? 'vehicle-in-service' : 'vehicle-out-of-service'),
        style: theme.textTheme.titleMedium?.copyWith(
          color: vehicle.active ? theme.colorScheme.primary : theme.colorScheme.error,
        ),
      ),
      if (!vehicle.active && since != null) ...[
        const SizedBox(height: 2),
        Text(
          'Since $since',
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ],
      const SizedBox(height: 16),

      // A failure with the vehicle already on screen is a banner above it, not a replacement for
      // it. That covers a refresh that failed and a refused action alike — and a refused
      // reactivation is re-read *without* clearing this, so the provider sees both what the
      // platform now holds and why their tap did not do what they expected.
      if (failure != null) ...[
        FailureBanner(failure),
        const SizedBox(height: 16),
      ],

      _Section(
        title: 'Details',
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            _Fact(label: 'Kind of vehicle', value: vehicle.vehicleType.label),
            if (vehicle.make case final make? when make.isNotEmpty)
              _Fact(label: 'Make', value: make),
            if (vehicle.model case final model? when model.isNotEmpty)
              _Fact(label: 'Model', value: model),
            if (dayFirstDate(vehicle.createdAt) case final added?)
              _Fact(label: 'Added', value: added),
          ],
        ),
      ),

      _Section(
        title: 'What it can carry',
        child: vehicle.hasCapacity
            ? Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  if (vehicle.maxWeightKg case final weight?)
                    _Fact(label: 'Maximum load', value: _kilograms(weight)),
                  if (vehicle.loadSpace case final space?)
                    _Fact(label: 'Load space', value: space),
                ],
              )
            : Text(
                // Ordinary rather than incomplete. A provider adds a truck in a yard with the plate
                // to hand, and the platform asks for nothing more than that.
                'Nothing recorded yet. Adding the load space and the maximum weight helps Shipper '
                'match you to work you can carry.',
                key: const Key('vehicle-no-capacity'),
                style: theme.textTheme.bodyMedium
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
      ),

      _Section(
        title: 'What you can do',
        child: _Actions(vehicle: vehicle, state: state, controller: controller),
      ),
    ];
  }
}

/// Kilograms, without a decimal point nobody typed.
String _kilograms(double value) =>
    value == value.roundToDouble() ? '${value.toInt()} kg' : '$value kg';

/// The three things a provider can do to their own vehicle (SHIP-98).
///
/// **Every one of them is an offer, not a decision.** `CLAUDE.md` and `Docs/07` §3 put every
/// authorisation decision on the platform. Which button is shown is read off the vehicle's own
/// `active` field — one is the opposite of the other, so there is no table to get stale — and the
/// request goes out regardless of what this client believes. Returning a vehicle to service can be
/// refused when a replacement is already in service on the same plate, and that refusal is rendered
/// rather than pre-empted.
class _Actions extends StatelessWidget {
  const _Actions({required this.vehicle, required this.state, required this.controller});

  final Vehicle vehicle;
  final VehicleState state;
  final VehicleController controller;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        OutlinedButton(
          key: const Key('vehicle-action-edit'),
          // Disabled while an action is in flight, which is what stops a second tap becoming a
          // second action. Two taps are two actions, and the idempotency key is per action
          // (`Docs/07` §4) — so the second would not be absorbed by the first.
          onPressed: state.acting ? null : controller.edit,
          child: const Text('Edit details'),
        ),
        const SizedBox(height: 8),

        if (vehicle.active)
          OutlinedButton(
            key: const Key('vehicle-action-deactivate'),
            onPressed: state.acting ? null : () => unawaited(_takeOffTheRoad(context)),
            child: state.acting
                ? const SizedBox(
                    height: 20,
                    width: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('Take off the road'),
          )
        else
          FilledButton(
            key: const Key('vehicle-action-reactivate'),
            // Not confirmed. Returning a vehicle to service is reversible by the button above it,
            // and a confirmation on a harmless action is what teaches people to dismiss the one
            // that matters.
            onPressed: state.acting ? null : () => unawaited(controller.reactivate()),
            child: state.acting
                ? const SizedBox(
                    height: 20,
                    width: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('Return to service'),
          ),

        const SizedBox(height: 16),
        Text(
          // Said out loud, because "delete" is what somebody will look for. Not finding it needs an
          // explanation rather than a silence — and the explanation is a real one, not a limitation
          // of this release.
          'Vehicles are never deleted. A vehicle is named by the bids you have won and the '
          'deliveries you have made, so it stays on your record even when it is off the road.',
          key: const Key('vehicle-no-delete'),
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ],
    );
  }

  Future<void> _takeOffTheRoad(BuildContext context) async {
    // Confirmed rather than taken on one tap. A provider who takes a truck off the road without
    // meaning to stops receiving work and has no reason to suspect why — the failure is silent
    // from where they are standing, which is what makes it worth one extra tap.
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (context) => AlertDialog(
        key: const Key('vehicle-deactivate-dialog'),
        title: Text('Take ${vehicle.registration} off the road?'),
        content: const Text(
          'You will stop being eligible for new work on this vehicle. Deliveries already under '
          'way are unaffected, and you can bring it back at any time.',
        ),
        actions: [
          TextButton(
            key: const Key('vehicle-deactivate-dismiss'),
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Keep it in service'),
          ),
          TextButton(
            key: const Key('vehicle-deactivate-confirm'),
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Take it off the road'),
          ),
        ],
      ),
    );

    if (confirmed ?? false) await controller.deactivate();
  }
}

/// What a provider sees when the vehicle could not be read at all.
///
/// One shape for every reason, deliberately. A vehicle belonging to another provider answers `404`
/// byte-identically to one that does not exist, and the client must not try to be more specific than
/// the platform was — the indistinguishability is the privacy control.
class _CouldNotLoad extends StatelessWidget {
  const _CouldNotLoad({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('vehicle-detail-failed'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          FailureBanner(failure),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('vehicle-detail-retry'),
            onPressed: () => unawaited(onRetry()),
            child: const Text('Try again'),
          ),
        ],
      ),
    );
  }
}

/// A titled block of the screen.
class _Section extends StatelessWidget {
  const _Section({required this.title, required this.child});

  final String title;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      padding: const EdgeInsets.only(bottom: 24),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            title,
            style: theme.textTheme.titleMedium?.copyWith(color: theme.colorScheme.primary),
          ),
          const SizedBox(height: 8),
          child,
        ],
      ),
    );
  }
}

/// One labelled value.
class _Fact extends StatelessWidget {
  const _Fact({required this.label, required this.value});

  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      padding: const EdgeInsets.only(bottom: 6),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(label, style: theme.textTheme.labelMedium),
          Text(value, style: theme.textTheme.bodyMedium),
        ],
      ),
    );
  }
}
