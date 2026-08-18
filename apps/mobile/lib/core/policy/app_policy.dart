import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/api_client.dart';

part 'app_policy.freezed.dart';
part 'app_policy.g.dart';

/// `GET /v1/app/policy` (SHIP-167a) — the operational numbers this build applies locally.
///
/// Flat rather than grouped, which is the shape `cmd/api/routes_app.go` chose and gave the reason
/// for: the version floor is keyed by platform because platforms multiply, and nothing here does.
///
/// **Unknown fields are ignored** (`Docs/07` §6), as they are for the version floor and for the
/// same reason: the platform must be able to add a third number tomorrow without every installed
/// build rejecting the answer and falling back to values from months ago.
@freezed
abstract class AppPolicy with _$AppPolicy {
  const factory AppPolicy({
    /// `Docs/02` §3.1's second rung, in seconds.
    ///
    /// Seconds on the wire because that is what the platform sends — `retry_after_seconds` in
    /// `contracts/paths/identity.yaml` is the precedent, and a Go duration string would be an
    /// implementation detail of the service. [unsyncedNudgeAfter] is the form the rest of this
    /// application uses.
    ///
    /// Defaulted rather than required so a response missing the key degrades to the compiled
    /// number rather than to a decode failure, which is the same fail-quiet direction
    /// `PlatformFloor.minimumBuild` takes.
    @JsonKey(name: 'unsynced_nudge_after_seconds')
    @Default(compiledUnsyncedNudgeAfterSeconds)
    int unsyncedNudgeAfterSeconds,

    /// The size a proof photograph is compressed towards, in bytes.
    ///
    /// **A budget, not the platform's bound.** `STORAGE_MAX_UPLOAD_BYTES` is the bound — the size
    /// above which the platform refuses to sign an upload at all — and it is ten times this.
    /// `core/capture/captured_image.dart` argues the difference; `internal/config` refuses a deployment that
    /// inverts the two.
    @JsonKey(name: 'proof_compression_budget_bytes')
    @Default(compiledProofCompressionBudgetBytes)
    int proofCompressionBudgetBytes,
  }) = _AppPolicy;

  const AppPolicy._();

  factory AppPolicy.fromJson(Map<String, dynamic> json) => _$AppPolicyFromJson(json);

  /// [unsyncedNudgeAfterSeconds] as the rest of the application reads it.
  Duration get unsyncedNudgeAfter => Duration(seconds: unsyncedNudgeAfterSeconds);

  /// Whether every number in this policy is one a client could act on.
  ///
  /// **A response is not trusted merely because it decoded.** A zero or negative threshold would
  /// prompt the provider about every update the instant they recorded it — the first rung of the
  /// escalation ladder wearing the second one's card — and a zero budget would push every
  /// photograph to the lowest quality rung on the way to a size it can never reach. The platform
  /// refuses both at startup (`internal/config`'s `validate`), so this is the second lock rather
  /// than the first; it exists because *this* build may be talking to a deployment older or newer
  /// than the one that grew that check, and because the value can also arrive from a cache file a
  /// person could edit.
  ///
  /// An implausible answer is discarded rather than clamped. Clamping would apply a number nobody
  /// chose and hide the fault; discarding falls through to the previous rung — see
  /// [resolveAppPolicy] in `app_policy_controller.dart`.
  bool get isUsable => unsyncedNudgeAfterSeconds > 0 && proofCompressionBudgetBytes > 0;
}

/// The nudge threshold an install that has **never once been online** uses.
///
/// `Docs/02` §3.1's four hours, and the document is the authority for the number. It is a `const`
/// here and a configured value on the platform, which is not a duplicate: this is the floor for a
/// device that has nothing to apply, and it is the only case it is used for.
///
/// `unsynced_nudge_after_seconds` in `deploy/.env.example` is the platform's own default and
/// `app_policy_test.dart` holds the two together, because two numbers meaning the same thing in two
/// repositories' worth of files is exactly how they drift.
const compiledUnsyncedNudgeAfterSeconds = 4 * 60 * 60;

/// The compression budget for the same install. One mebibyte — see [AppPolicy.proofCompressionBudgetBytes].
const compiledProofCompressionBudgetBytes = 1024 * 1024;

/// What this build does before the platform has ever told it anything.
///
/// **Not a fallback for being offline.** See the table in `policy.dart`: a device that has been
/// online before applies what it was told, whatever its connection is doing now.
const compiledAppPolicy = AppPolicy(
  unsyncedNudgeAfterSeconds: compiledUnsyncedNudgeAfterSeconds,
  proofCompressionBudgetBytes: compiledProofCompressionBudgetBytes,
);

/// Reads the policy.
///
/// Thin on purpose, in the mould of [MinimumVersionRepository]: it exists so that what applies the
/// policy depends on something a test can substitute rather than on the transport.
class AppPolicyRepository {
  const AppPolicyRepository(this._client);

  final ApiClient _client;

  static const path = '/v1/app/policy';

  Future<AppPolicy> fetch() async => AppPolicy.fromJson(await _client.getJson(path));
}

/// The repository, over the client that carries **no credential**.
///
/// `unauthenticatedApiClientProvider`, for the reason `minimumVersionRepositoryProvider` uses it
/// and one more of this endpoint's own. The shared reason is that this is read at launch, before
/// the keychain has answered, and putting it behind the session would drag it through the
/// interceptor whose answer to a `401` is to refresh.
///
/// The reason of its own: **nothing in the response is about the caller.** Two integers, identical
/// on every handset. `cmd/api/routes_app.go` declares the route `Public` on exactly that ground, so
/// sending a credential would be this client asserting a relationship the endpoint does not have.
final appPolicyRepositoryProvider = Provider<AppPolicyRepository>(
  (ref) => AppPolicyRepository(ref.watch(unauthenticatedApiClientProvider)),
);
