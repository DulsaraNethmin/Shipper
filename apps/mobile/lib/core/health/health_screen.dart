import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/health/health_repository.dart';
import 'package:shipper/core/health/health_status.dart';

/// Proves the app can reach the platform, by showing the version it answered with (SHIP-19).
///
/// A development surface, not a product one. It is what sits at `/` until the role-aware shell
/// replaces it in M1, and it earns its place twice over: it is the only thing in the client
/// that demonstrates the whole path — build flavour, base URL, transport, decoding, failure
/// mapping — end to end, on a real device, before any of it has a feature depending on it.
///
/// It shows the resolved base URL deliberately. The single most likely reason this screen
/// fails is that it is pointed somewhere there is no API, and a version number that will not
/// load tells you nothing about which address it did not load from.
class HealthScreen extends ConsumerWidget {
  const HealthScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final health = ref.watch(healthProvider);
    final environment = ref.watch(apiEnvironmentProvider);

    return Scaffold(
      appBar: AppBar(title: const Text('Shipper')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              switch (health) {
                AsyncData(:final value) => _Version(value),
                AsyncError(:final error) => _Unreachable(
                    error: error,
                    onRetry: () => ref.invalidate(healthProvider),
                  ),
                _ => const CircularProgressIndicator(),
              },
              const SizedBox(height: 32),
              _Target(environment: environment.name, baseUrl: environment.baseUrl()),
            ],
          ),
        ),
      ),
    );
  }
}

class _Version extends StatelessWidget {
  const _Version(this.health);

  final HealthStatus health;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      children: [
        Text('API version', style: theme.textTheme.labelLarge),
        const SizedBox(height: 4),
        Text(
          health.version,
          key: const Key('health-version'),
          style: theme.textTheme.headlineMedium,
        ),
        const SizedBox(height: 8),
        Text(
          health.commit == null ? health.status : '${health.status} · ${health.commit}',
          style: theme.textTheme.bodySmall,
        ),
      ],
    );
  }
}

class _Unreachable extends StatelessWidget {
  const _Unreachable({required this.error, required this.onRetry});

  final Object error;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    // Anything that is not an ApiFailure got past the client's mapping, which is a defect in
    // the client rather than in the connection — so it says so rather than blaming the signal.
    final message = error is ApiFailure
        ? (error as ApiFailure).userMessage
        : 'Something went wrong. Please try again.';

    return Column(
      children: [
        Text(
          message,
          key: const Key('health-error'),
          textAlign: TextAlign.center,
          style: Theme.of(context).textTheme.bodyLarge,
        ),
        const SizedBox(height: 16),
        FilledButton(onPressed: onRetry, child: const Text('Try again')),
      ],
    );
  }
}

class _Target extends StatelessWidget {
  const _Target({required this.environment, required this.baseUrl});

  final String environment;
  final String baseUrl;

  @override
  Widget build(BuildContext context) {
    final style = Theme.of(context).textTheme.bodySmall;

    return Column(
      children: [
        Text(environment, style: style),
        Text(baseUrl, key: const Key('health-base-url'), style: style),
      ],
    );
  }
}
