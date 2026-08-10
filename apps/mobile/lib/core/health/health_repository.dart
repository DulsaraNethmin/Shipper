import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/health/health_status.dart';

/// Reads `GET /health`.
///
/// Thin on purpose. It exists so the screen depends on something it can substitute in a test,
/// rather than on the transport — and so `Docs/10` §8.1's generated client can take over the
/// call at SHIP-17a without a widget knowing.
class HealthRepository {
  const HealthRepository(this._client);

  final ApiClient _client;

  Future<HealthStatus> fetch() async {
    return HealthStatus.fromJson(await _client.getJson('/health'));
  }
}

final healthRepositoryProvider = Provider<HealthRepository>(
  (ref) => HealthRepository(ref.watch(apiClientProvider)),
);

/// The platform's answer, or the failure that stopped it arriving.
///
/// A `FutureProvider` rather than a hand-rolled loading flag: it gives the three states a
/// network call actually has, and `ref.invalidate` is the whole of the retry.
final healthProvider = FutureProvider<HealthStatus>(
  (ref) => ref.watch(healthRepositoryProvider).fetch(),
);
