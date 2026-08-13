// Shared plumbing for the SHIP-168 tests.
//
// The same hand-rolled adapter `jobs_repository_test.dart` uses, for the same reason: what
// reaches the wire is part of the contract, and a fake repository cannot notice that the launch
// check went to the wrong path or carried a credential it should not have.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/version/minimum_version.dart';

/// Records what was asked for and answers with whatever the test decided.
class StubAdapter implements HttpClientAdapter {
  StubAdapter(this.respond);

  /// Answers every request with one body.
  StubAdapter.returning(Object body, {int status = 200})
    : respond = ((_) => jsonResponse(body, status: status));

  /// Answers nothing at all, as a handset with no signal does.
  StubAdapter.unreachable()
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

ResponseBody jsonResponse(Object body, {int status = 200}) {
  return ResponseBody.fromString(
    jsonEncode(body),
    status,
    headers: {
      Headers.contentTypeHeader: [Headers.jsonContentType],
    },
  );
}

/// A repository over a transport the test owns.
///
/// `buildDio` rather than a bare `Dio`, so the request travels through the timeouts and the
/// idempotency guard the application actually installs — the same transport every other
/// repository test builds.
MinimumVersionRepository repositoryOver(StubAdapter adapter) {
  return MinimumVersionRepository(
    ApiClient(buildDio(baseUrl: 'http://localhost:8080')..httpClientAdapter = adapter),
  );
}

/// The body `GET /v1/app/minimum-version` answers with, as `cmd/api/routes_app.go` writes it.
///
/// `store_url` carries `omitempty`, so a blank one is an **absent key** rather than an empty
/// string — which is the case the pilot actually ships and the one worth getting right in a
/// fixture.
Map<String, Object?> floorsBody({
  int ios = 1,
  int android = 1,
  String? iosStoreUrl,
  String? androidStoreUrl,
}) {
  return <String, Object?>{
    'ios': <String, Object?>{
      'minimum_build': ios,
      'store_url': ?iosStoreUrl,
    },
    'android': <String, Object?>{
      'minimum_build': android,
      'store_url': ?androidStoreUrl,
    },
  };
}
