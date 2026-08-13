import 'package:freezed_annotation/freezed_annotation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';

part 'minimum_version.freezed.dart';
part 'minimum_version.g.dart';

/// `GET /v1/app/minimum-version` (SHIP-167).
///
/// Keyed by platform rather than flattened, which is the shape `cmd/api/routes_app.go` chose so
/// that adding a platform is an added key rather than a reshaped response. This build reads the
/// one entry it runs on.
///
/// **Unknown fields are ignored** (`Docs/07` §6), which is what lets the platform add a key
/// tomorrow without every installed build rejecting the answer — and this is the one response in
/// the application where that matters most, because a build too old to be tolerant is precisely
/// the build this endpoint exists to talk to.
@freezed
abstract class MinimumVersion with _$MinimumVersion {
  const factory MinimumVersion({
    required PlatformFloor ios,
    required PlatformFloor android,
  }) = _MinimumVersion;

  factory MinimumVersion.fromJson(Map<String, dynamic> json) => _$MinimumVersionFromJson(json);
}

/// The floor for one platform.
@freezed
abstract class PlatformFloor with _$PlatformFloor {
  const factory PlatformFloor({
    /// The lowest build number still permitted — `internal/config`'s own words.
    ///
    /// Defaulted rather than required so that a floor arriving without it degrades to a floor
    /// nothing is below, rather than to a decode failure. Both are fail-open; this one is the
    /// quieter of the two and keeps the model total.
    @JsonKey(name: 'minimum_build') @Default(0) int minimumBuild,

    /// Where to send a blocked build, or `null`.
    ///
    /// **Nullable because the platform genuinely sends nothing.** `store_url` carries
    /// `omitempty`, and `IOS_STORE_URL` and `ANDROID_STORE_URL` both default to empty —
    /// `internal/config/config.go` records that as a decision rather than an omission, because
    /// pilot distribution is TestFlight and Play internal testing and there is no public listing
    /// to link to. So the update prompt has two shapes and this is the field that selects
    /// between them. See `update_required_screen.dart`.
    @JsonKey(name: 'store_url') String? storeUrl,
  }) = _PlatformFloor;

  factory PlatformFloor.fromJson(Map<String, dynamic> json) => _$PlatformFloorFromJson(json);
}

/// Reads the floor.
///
/// Thin on purpose, in the mould of `HealthRepository`: it exists so the gate depends on
/// something a test can substitute rather than on the transport.
class MinimumVersionRepository {
  const MinimumVersionRepository(this._client);

  final ApiClient _client;

  static const path = '/v1/app/minimum-version';

  Future<MinimumVersion> fetch() async =>
      MinimumVersion.fromJson(await _client.getJson(path));
}

/// The repository, over the client that carries **no credential** (SHIP-50).
///
/// `unauthenticatedApiClientProvider` rather than `apiClientProvider`, and that is not a detail.
/// `cmd/api/routes_app.go` says why the route is `Public`: "a build old enough to be blocked may
/// be old enough that its authentication no longer works, which would otherwise leave it unable
/// to discover that it needs updating". Sending this through the authenticated client would put
/// the launch check behind the session — an expired refresh token, a revoked device session, or
/// simply a cold start that has not read the keychain yet — and would drag it through the
/// interceptor whose answer to a `401` is to refresh. The one request that has to work on a
/// build in trouble should depend on as little as possible.
final minimumVersionRepositoryProvider = Provider<MinimumVersionRepository>(
  (ref) => MinimumVersionRepository(ref.watch(unauthenticatedApiClientProvider)),
);
