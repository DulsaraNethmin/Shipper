// Shared plumbing for the SHIP-167a tests.
//
// The same hand-rolled adapter `version_fixture.dart` uses, for the same reason: what reaches the
// wire is part of the contract, and a fake repository cannot notice that the policy read went to
// the wrong path or carried a credential it should not have.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/policy/app_policy.dart';
import 'package:shipper/core/policy/app_policy_cache.dart';

/// Records what was asked for and answers with whatever the test decided.
class PolicyStubAdapter implements HttpClientAdapter {
  PolicyStubAdapter(this.respond);

  /// Answers every request with one body.
  PolicyStubAdapter.returning(Object body, {int status = 200})
    : respond = ((_) => policyJsonResponse(body, status: status));

  /// Answers nothing at all, as a handset with no signal does.
  ///
  /// **This is the adapter the ticket turns on.** A device using it has been online before — its
  /// cache says so — and the endpoint exists precisely because the value is needed at a moment
  /// like this one.
  PolicyStubAdapter.unreachable()
    : respond = ((options) => throw DioException(
        requestOptions: options,
        type: DioExceptionType.connectionError,
      ));

  final ResponseBody Function(RequestOptions options) respond;
  final requests = <RequestOptions>[];

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    requests.add(options);
    return respond(options);
  }

  @override
  void close({bool force = false}) {}
}

ResponseBody policyJsonResponse(Object body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

/// A repository over a transport the test owns.
AppPolicyRepository policyRepositoryOver(PolicyStubAdapter adapter) {
  return AppPolicyRepository(
    ApiClient(buildDio(baseUrl: 'http://localhost:8080')..httpClientAdapter = adapter),
  );
}

/// The body `GET /v1/app/policy` answers with, as `cmd/api/routes_app.go` writes it.
Map<String, Object?> policyBody({int nudgeSeconds = 14400, int budgetBytes = 1048576}) {
  return <String, Object?>{
    'unsynced_nudge_after_seconds': nudgeSeconds,
    'proof_compression_budget_bytes': budgetBytes,
  };
}

/// A policy **deliberately unlike [compiledAppPolicy] in both fields.**
///
/// This is the fixture the mutation test rests on, so it is worth saying why it is written this
/// way rather than reusing the defaults. Wave 10 lost a mutation to a fixture that sorted
/// identically under both columns; the equivalent here would be a cached policy holding four hours
/// and one mebibyte, which is what the compiled default holds. A build that wrongly fell back to
/// the compiled numbers whenever it was offline would then produce **exactly the right answer** in
/// every assertion, and the defect would ship.
///
/// Thirty minutes and 256 KiB, then: two values nothing else in this application uses, so an
/// assertion that sees them can only have got them from the cache.
const cachedPolicyUnlikeTheDefault = AppPolicy(
  unsyncedNudgeAfterSeconds: 30 * 60,
  proofCompressionBudgetBytes: 256 * 1024,
);

/// An [AppPolicyCache] that holds one value in memory.
///
/// Enough for every test here: what a real cache adds is durability across processes, which a host
/// test cannot observe anyway. `app_policy_cache_test.dart` exercises the file implementation
/// against a real temporary directory.
final class FakeAppPolicyCache implements AppPolicyCache {
  FakeAppPolicyCache([this.held]);

  /// What this device was last told, or `null` for a device that has never been online.
  AppPolicy? held;

  /// Every policy written, in order. A test that asserts the fetch was kept reads this.
  final writes = <AppPolicy>[];

  /// Set to make [write] throw, which is the device with a full or read-only disk.
  bool failWrites = false;

  int reads = 0;

  @override
  Future<AppPolicy?> read() async {
    reads++;
    return held;
  }

  @override
  Future<void> write(AppPolicy policy) async {
    if (failWrites) throw const FileSystemException('no space left on device');
    writes.add(policy);
    held = policy;
  }
}

/// [FileSystemException] without importing dart:io into every test that uses the fake.
class FileSystemException implements Exception {
  const FileSystemException(this.message);
  final String message;
  @override
  String toString() => 'FileSystemException: $message';
}
