import 'package:freezed_annotation/freezed_annotation.dart';

part 'health_status.freezed.dart';
part 'health_status.g.dart';

/// `GET /health` (SHIP-6).
///
/// Operational rather than product, so it sits outside `/v1` (SHIP-13) — load balancers and
/// monitoring call it, and versioning it would break every health check on the day v2 ships.
///
/// The field names are a contract with builds that will be installed on devices: the Go side
/// says so in `cmd/api/routes.go`, where `healthResponse` carries a comment naming SHIP-19 as
/// the reason `version` cannot be renamed later.
///
/// **Unknown fields are ignored, and that is the point** (`Docs/07` §6). `json_serializable`
/// reads only the keys declared here and steps over the rest, so the platform can add a field
/// tomorrow without every installed build rejecting the response. Everything below the two
/// required fields is nullable for the same reason from the other direction: a field that
/// disappears, or a build that predates it, must degrade to "not shown" rather than to a
/// crash.
@freezed
abstract class HealthStatus with _$HealthStatus {
  const factory HealthStatus({
    required String status,
    required String version,
    String? commit,
    @JsonKey(name: 'built_at') String? builtAt,
    @Default(false) bool dirty,
    String? uptime,
  }) = _HealthStatus;

  factory HealthStatus.fromJson(Map<String, dynamic> json) => _$HealthStatusFromJson(json);
}
