import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/provider_only.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/fleet/fleet_controller.dart';
import 'package:shipper/features/fleet/vehicle.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// The provider's own fleet (SHIP-98).
///
/// **The first provider surface in this app.** Everything before it was the customer's or belonged
/// to neither half, so this is where `Docs/07` §1's requirement that the two halves stay genuinely
/// separate inside one app stops being a statement about the shell and starts being a screen.
/// [ProviderOnly] is where that separation is made, and it explains at length why hiding this
/// surface from a customer is a presentation decision and why the platform's own refusal is the
/// control.
///
/// ## Both halves of the fleet are shown, and that is the point
///
/// `GET /v1/fleet/vehicles` defaults to every vehicle, in service and off the road alike, and this
/// screen uses that default rather than filtering. A fleet screen that hid retired vehicles would
/// leave a provider with no way to find the truck they want to bring back — and returning one to
/// service is half of what this screen exists for.
///
/// ## Three states a list has, and the one everybody forgets
///
/// A provider who has just signed up has no vehicles, gets `[]` from the platform, and must see an
/// **empty state** — never a spinner that does not resolve, and never an error. That is the case
/// that never occurs in development, because whoever is building this has added a truck.
class FleetScreen extends StatelessWidget {
  const FleetScreen({super.key});

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(title: const Text('Your vehicles')),
      // Everything provider-only is inside the gate, including the affordance that adds a vehicle.
      // A customer reaching this route by a deep link gets the app bar and an explanation, and no
      // request is made on their behalf.
      body: SafeArea(child: const ProviderOnly(child: _Fleet())),
    );
  }
}

/// The fleet itself, drawn only for a provider.
class _Fleet extends ConsumerWidget {
  const _Fleet();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final state = ref.watch(fleetProvider);
    final controller = ref.read(fleetProvider.notifier);

    return RefreshIndicator(
      // Holds its own spinner until the read finishes, which is why `refresh()` returns its future
      // rather than swallowing it.
      onRefresh: controller.refresh,
      child: ListView(
        key: const Key('fleet'),
        // Always scrollable, so the pull works on the empty state and on the failure state as well.
        // A refresh gesture that only worked once there was something to scroll would stop working
        // exactly when somebody needed it.
        physics: const AlwaysScrollableScrollPhysics(),
        padding: const EdgeInsets.fromLTRB(16, 16, 16, 32),
        children: _children(context, theme, state, controller),
      ),
    );
  }

  List<Widget> _children(
    BuildContext context,
    ThemeData theme,
    FleetState state,
    FleetController controller,
  ) {
    final failure = state.failure;

    return <Widget>[
      Text('Your vehicles', style: theme.textTheme.headlineSmall),
      const SizedBox(height: 8),
      Text(
        'What you can carry decides which jobs you can bid on. Shipper checks that when a bid is '
        'placed.',
        style: theme.textTheme.bodyMedium,
      ),
      const SizedBox(height: 16),

      // A failure with a fleet already on screen is a banner above it, not a replacement for it.
      // Somebody who pulled to refresh in a depot with no signal should still see their vehicles.
      if (failure != null && state.vehicles.isNotEmpty) ...[
        FailureBanner(failure),
        const SizedBox(height: 16),
      ],

      if (state.isFirstLoad)
        const Padding(
          padding: EdgeInsets.symmetric(vertical: 48),
          child: Center(child: CircularProgressIndicator(key: Key('fleet-loading'))),
        )
      else if (state.failedOutright && failure != null)
        _CouldNotLoad(failure: failure, onRetry: controller.retry)
      else if (state.isEmpty)
        const _NoVehiclesYet()
      else ...[
        const _AddVehicleButton(),
        const SizedBox(height: 24),
        if (state.inService.isNotEmpty)
          _Group(heading: 'In service', slug: 'in-service', vehicles: state.inService),
        if (state.outOfService.isNotEmpty)
          _Group(
            heading: 'Off the road',
            slug: 'out-of-service',
            vehicles: state.outOfService,
            note: 'These make you eligible for no new work. Deliveries already under way are '
                'unaffected.',
          ),
        if (state.hasMore) _ShowMore(state: state, onPressed: controller.loadMore),
      ],
    ];
  }
}

/// The way into the add form.
///
/// A button in the list rather than a floating one, so that it sits inside [ProviderOnly] with
/// everything else this screen offers. A `FloatingActionButton` belongs to the `Scaffold` and would
/// have needed the role read a second time, in a second place, to keep it away from a customer —
/// which is how two answers to one question get out of step.
class _AddVehicleButton extends StatelessWidget {
  const _AddVehicleButton();

  @override
  Widget build(BuildContext context) {
    return FilledButton.icon(
      key: const Key('fleet-add'),
      // `push` rather than `go`, so the back gesture returns to the fleet where it was rather than
      // rebuilding the shell and re-reading the list.
      onPressed: () => context.push(Routes.newVehicle),
      icon: const Icon(Icons.add),
      label: const Text('Add a vehicle'),
    );
  }
}

/// One half of the fleet, as a heading over the vehicles in it.
///
/// **No count beside the heading**, and that is honesty rather than an omission: the list is paged,
/// so a group holds the vehicles that have been read and not every vehicle in that state. A number
/// here would be right on the first page and quietly wrong on every screen that has more.
class _Group extends StatelessWidget {
  const _Group({
    required this.heading,
    required this.slug,
    required this.vehicles,
    this.note,
  });

  final String heading;
  final String slug;
  final List<Vehicle> vehicles;
  final String? note;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          heading,
          key: Key('fleet-group-$slug'),
          style: theme.textTheme.titleMedium?.copyWith(color: theme.colorScheme.primary),
        ),
        if (note case final explanation?) ...[
          const SizedBox(height: 4),
          Text(
            explanation,
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
        const SizedBox(height: 8),
        for (final vehicle in vehicles) ...[
          _VehicleCard(vehicle),
          const SizedBox(height: 8),
        ],
        const SizedBox(height: 16),
      ],
    );
  }
}

/// One vehicle, as its owner sees it.
class _VehicleCard extends StatelessWidget {
  const _VehicleCard(this.vehicle);

  final Vehicle vehicle;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    final description = <String>[
      vehicle.vehicleType.label,
      if (vehicle.make case final make? when make.isNotEmpty) make,
      if (vehicle.model case final model? when model.isNotEmpty) model,
    ].join(' · ');

    final capacity = <String>[
      if (vehicle.maxWeightKg case final weight?) 'Up to ${_kilograms(weight)}',
      if (vehicle.loadSpace case final space?) 'Load space $space',
    ].join('  ·  ');

    final since = dayFirstDate(vehicle.deactivatedAt);

    return Card(
      key: Key('vehicle-${vehicle.id}'),
      margin: EdgeInsets.zero,
      child: InkWell(
        // `push` rather than `go`, so the back gesture returns to the fleet where it was.
        onTap: () => context.push(Routes.vehicleDetailFor(vehicle.id)),
        child: Padding(
          padding: const EdgeInsets.all(16),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                // The plate leads, because it is how a provider knows which truck this is. It is
                // the platform's stored form — capitals, no spaces — rather than what was typed.
                vehicle.registration,
                style: theme.textTheme.titleMedium,
              ),
              const SizedBox(height: 2),
              Text(description, style: theme.textTheme.bodyMedium),
              if (capacity.isNotEmpty) ...[
                const SizedBox(height: 4),
                Text(
                  capacity,
                  style: theme.textTheme.bodySmall
                      ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
                ),
              ],
              if (!vehicle.active) ...[
                const SizedBox(height: 4),
                Text(
                  // The date, because "why did this provider stop seeing jobs" is a question the
                  // day answers and a boolean does not.
                  since == null ? 'Off the road' : 'Off the road since $since',
                  style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.error),
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// Kilograms, without a decimal point nobody typed.
String _kilograms(double value) =>
    value == value.roundToDouble() ? '${value.toInt()} kg' : '$value kg';

/// What a provider with no vehicles sees.
///
/// An empty state and never a spinner or an error, which is the half of this ticket easiest to get
/// wrong: `data` is `[]` rather than `null` precisely so this case is ordinary, and a client that
/// treated an empty list as "still loading" would leave every new provider watching a spinner for
/// ever.
class _NoVehiclesYet extends StatelessWidget {
  const _NoVehiclesYet();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      key: const Key('fleet-empty'),
      padding: const EdgeInsets.symmetric(vertical: 32),
      child: Column(
        children: [
          Icon(Icons.local_shipping_outlined, size: 48, color: theme.colorScheme.outline),
          const SizedBox(height: 16),
          Text(
            'You have not added a vehicle yet',
            style: theme.textTheme.titleMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 8),
          Text(
            'Add the vehicles you run. The registration and what kind of vehicle it is are all '
            'that is needed to start.',
            style: theme.textTheme.bodyMedium,
            textAlign: TextAlign.center,
          ),
          const SizedBox(height: 24),
          const _AddVehicleButton(),
        ],
      ),
    );
  }
}

/// What a provider sees when the fleet could not be read at all.
///
/// Kept apart from the empty state on purpose. "You have no vehicles" and "we could not find out"
/// are different things to be told, and only one of them has a retry.
class _CouldNotLoad extends StatelessWidget {
  const _CouldNotLoad({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Padding(
      key: const Key('fleet-failed'),
      padding: const EdgeInsets.symmetric(vertical: 24),
      child: Column(
        children: [
          FailureBanner(failure),
          const SizedBox(height: 16),
          OutlinedButton(
            key: const Key('fleet-retry'),
            onPressed: () => unawaited(onRetry()),
            child: const Text('Try again'),
          ),
        ],
      ),
    );
  }
}

/// The next page, asked for rather than swallowed.
class _ShowMore extends StatelessWidget {
  const _ShowMore({required this.state, required this.onPressed});

  final FleetState state;
  final Future<void> Function() onPressed;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      children: [
        Text(
          // Said out loud, because the groups above are split from what has been read. A heading
          // with three vehicles under it when there are five is a screen that has lied quietly, and
          // this is the sentence that stops it.
          'Showing the vehicles you added most recently.',
          key: const Key('fleet-partial'),
          style: theme.textTheme.bodySmall,
        ),
        const SizedBox(height: 8),
        OutlinedButton(
          key: const Key('fleet-more'),
          onPressed: state.loadingMore ? null : () => unawaited(onPressed()),
          child: state.loadingMore
              ? const SizedBox(
                  height: 20,
                  width: 20,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              : const Text('Show more vehicles'),
        ),
      ],
    );
  }
}
