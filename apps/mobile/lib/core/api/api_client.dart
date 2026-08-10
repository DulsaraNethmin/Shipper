import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_environment.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/errors/api_failure.dart';

/// The one HTTP client, over `dio` (`Docs/10` §8.3).
///
/// It talks to the versioned public API directly; there is no BFF tier (`Docs/06` §2.1).
///
/// The surface is deliberately narrow — a JSON map in, a JSON map out, [ApiFailure] on
/// anything else — because `Docs/10` §8.1 replaces the hand-written calls with a client
/// generated from `contracts/openapi.yaml` at SHIP-17a. What survives that is this class:
/// the base URL resolution, the timeouts, the idempotency rule and the failure mapping. What
/// does not survive is any per-endpoint method, which is why there are none beyond what a
/// caller passes in.
class ApiClient {
  /// Takes its transport positionally so that a test can hand in a `Dio` wired to
  /// `DioAdapter` without the class exposing a settable field.
  ApiClient(this._dio);

  /// Builds a client for [environment], or for the environment this build was compiled with.
  factory ApiClient.forEnvironment([ApiEnvironment? environment]) {
    final env = environment ?? ApiEnvironment.current;
    return ApiClient(buildDio(baseUrl: env.baseUrl()));
  }

  final Dio _dio;

  /// The underlying transport, for the generated client at SHIP-17a and for tests that need to
  /// install `DioAdapter`. Nothing in `features/` should reach for it.
  Dio get transport => _dio;

  /// A read.
  ///
  /// Reads take no idempotency key: they change nothing, and the platform requires one only
  /// for state-changing methods (SHIP-15).
  Future<Map<String, dynamic>> getJson(
    String path, {
    Map<String, dynamic>? query,
  }) {
    return _json(() => _dio.get<Object?>(path, queryParameters: query));
  }

  /// A write.
  ///
  /// [idempotencyKey] is required rather than optional, and that is the point of the method.
  /// A caller cannot forget it, because forgetting it does not compile. `Docs/07` §4 has the
  /// key generated once where the user acts and reused unchanged across every retry, so it
  /// arrives from the caller and is never minted here.
  Future<Map<String, dynamic>> postJson(
    String path, {
    required String idempotencyKey,
    Object? body,
  }) {
    return _json(
      () => _dio.post<Object?>(
        path,
        data: body,
        options: Options(headers: {ApiHeaders.idempotencyKey: idempotencyKey}),
      ),
    );
  }

  Future<Map<String, dynamic>> _json(Future<Response<Object?>> Function() send) async {
    try {
      final response = await send();
      final data = response.data;

      if (data is Map<String, dynamic>) return data;

      // A 204, an HTML error page from a proxy, or a JSON array where an object was expected.
      // All of them mean the same thing to a caller: this is not the response it can use.
      throw ApiMalformedResponse(statusCode: response.statusCode ?? 0);
    } on DioException catch (e) {
      // The idempotency guard rejects with the call site's own error. Surfacing that as a
      // network problem would hide a programming mistake behind a plausible excuse, and the
      // request would then be "retried" forever against a rule it can never satisfy.
      final cause = e.error;
      if (cause is StateError) throw cause;

      throw _failureFrom(e);
    }
  }

  /// Maps a transport failure onto the small set of things a screen can actually respond to.
  static ApiFailure _failureFrom(DioException e) {
    final cause = e.error;
    if (cause is ApiFailure) return cause;

    switch (e.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.sendTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.transformTimeout:
        return const ApiUnreachable(timedOut: true);

      case DioExceptionType.connectionError:
      case DioExceptionType.unknown:
        return const ApiUnreachable();

      case DioExceptionType.badCertificate:
        return const ApiMalformedResponse(statusCode: 0);

      case DioExceptionType.cancel:
        return const ApiUnreachable();

      case DioExceptionType.badResponse:
        final response = e.response;
        return ApiErrorResponse.fromJson(
          response?.statusCode ?? 0,
          response?.data,
          fallbackCode: 'unexpected_status',
        );
    }
  }
}

/// Constructs the transport.
///
/// Separate from [ApiClient] so a test can build one against `DioAdapter` without reproducing
/// the timeouts and interceptors — a test that configures its own transport is testing its own
/// transport.
Dio buildDio({required String baseUrl}) {
  final dio = Dio(
    BaseOptions(
      baseUrl: baseUrl,
      // Chosen for a phone on mobile data rather than for a data centre. Long enough that a
      // slow connection is not reported as a failure, short enough that a user is not left
      // watching a spinner with no idea whether anything is happening.
      connectTimeout: const Duration(seconds: 10),
      sendTimeout: const Duration(seconds: 30),
      receiveTimeout: const Duration(seconds: 30),
      responseType: ResponseType.json,
      headers: const {'Accept': 'application/json'},
    ),
  );

  dio.interceptors.add(const IdempotencyInterceptor());
  return dio;
}

/// The environment this build was compiled for.
///
/// A provider so a test, or a future in-app diagnostics screen, can override it. It is still
/// decided at build time — overriding it does not move a release build to another deployment,
/// because the base URL was compiled in.
final apiEnvironmentProvider = Provider<ApiEnvironment>((ref) => ApiEnvironment.current);

/// The application's client.
final apiClientProvider = Provider<ApiClient>((ref) {
  return ApiClient.forEnvironment(ref.watch(apiEnvironmentProvider));
});
