import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_environment.dart';
import 'package:shipper/core/api/auth_interceptor.dart';
import 'package:shipper/core/api/idempotency_interceptor.dart';
import 'package:shipper/core/auth/session_controller.dart';
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
  ///
  /// [session] is what makes it an *authenticated* client — omit it and nothing attaches a
  /// bearer token and nothing reacts to a `401`. That is the right shape for the refresh call
  /// itself, and for a test that is about something else.
  factory ApiClient.forEnvironment([
    ApiEnvironment? environment,
    SessionTokens Function()? session,
  ]) {
    final env = environment ?? ApiEnvironment.current;
    return ApiClient(buildDio(baseUrl: env.baseUrl(), session: session));
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
    return _write('POST', path, idempotencyKey: idempotencyKey, body: body);
  }

  /// A partial edit.
  ///
  /// Identical to [postJson] in everything that matters — it is state-changing, so it carries an
  /// idempotency key and is refused without one (SHIP-15). It exists as its own method because
  /// the platform distinguishes the two: `PATCH /v1/jobs/{id}` touches only the fields present,
  /// which is what lets one step of a wizard save its own without sending back the fields it
  /// cannot see. A client that reached for `POST` there would create a second job.
  Future<Map<String, dynamic>> patchJson(
    String path, {
    required String idempotencyKey,
    Object? body,
  }) {
    return _write('PATCH', path, idempotencyKey: idempotencyKey, body: body);
  }

  /// A write the platform answers with `204 No Content`.
  ///
  /// `POST /v1/auth/logout` is the only one today and is why this exists: it answers `204` with
  /// an empty body, which [postJson] would raise as [ApiMalformedResponse] — a *success* reported
  /// as a broken response. Nothing branches on that today, because the one caller is
  /// fire-and-forget, and that is exactly the kind of wrongness that stays harmless until
  /// something does.
  ///
  /// [headers] is for the one call that has to carry a credential this transport will not supply.
  /// See `session_ender.dart`, which explains why the sign-out request must not travel through
  /// the interceptor that reads the session's token.
  Future<void> postNoContent(
    String path, {
    required String idempotencyKey,
    Map<String, Object?> headers = const <String, Object?>{},
    Object? body,
  }) async {
    await _guarded(
      () => _dio.request<Object?>(
        path,
        data: body,
        options: Options(
          method: 'POST',
          headers: {ApiHeaders.idempotencyKey: idempotencyKey, ...headers},
        ),
      ),
    );
  }

  /// A removal the platform answers with `204 No Content` (SHIP-143).
  ///
  /// The `DELETE` counterpart of [postNoContent], and it exists for the same two reasons that one
  /// does. It is state-changing, so it carries an idempotency key and is refused without one
  /// (SHIP-15) — a phone that retries after a dropped connection must not be told it did something
  /// wrong the second time. And a `204` through [postJson] would be raised as
  /// [ApiMalformedResponse]: a success reported as a broken response.
  ///
  /// [headers] is for the same one call shape [postNoContent]'s is — a request that has to carry a
  /// credential this transport will not supply, because the session is in the act of forgetting it.
  /// `DELETE /v1/notifications/device-tokens/current` is sent on the way out of a session and would
  /// otherwise race the clear and go out with nothing. See `session_ender.dart`, which argues the
  /// whole of it, and `push_registration.dart`, which is the second caller of the same argument.
  Future<void> deleteNoContent(
    String path, {
    required String idempotencyKey,
    Map<String, Object?> headers = const <String, Object?>{},
  }) async {
    await _guarded(
      () => _dio.request<Object?>(
        path,
        options: Options(
          method: 'DELETE',
          headers: {ApiHeaders.idempotencyKey: idempotencyKey, ...headers},
        ),
      ),
    );
  }

  /// A write whose **status** is the answer, rather than its body (SHIP-125).
  ///
  /// For the sync worker, which needs to know one thing about a queued operation — did the
  /// platform take it — and has no use for what came back. Returns the status code on any
  /// success and throws [ApiFailure] on everything else, exactly as the methods above do.
  ///
  /// It exists rather than reusing [postJson] because of a trap that would be invisible until it
  /// bit: [postJson] raises [ApiMalformedResponse] for a `2xx` with no JSON object in it, which
  /// for a queued operation would be **a success reported as a failure, and then retried
  /// forever** against an endpoint that had already recorded it. The same reasoning gave
  /// [postNoContent] its existence; this is the general form of it.
  Future<int> send(
    String method,
    String path, {
    required String idempotencyKey,
    Object? body,
  }) async {
    final response = await _guarded(
      () => _dio.request<Object?>(
        path,
        data: body,
        options: Options(
          method: method,
          headers: {ApiHeaders.idempotencyKey: idempotencyKey},
        ),
      ),
    );
    return response.statusCode ?? 0;
  }

  Future<Map<String, dynamic>> _write(
    String method,
    String path, {
    required String idempotencyKey,
    Object? body,
  }) {
    return _json(
      () => _dio.request<Object?>(
        path,
        data: body,
        options: Options(
          method: method,
          headers: {ApiHeaders.idempotencyKey: idempotencyKey},
        ),
      ),
    );
  }

  Future<Map<String, dynamic>> _json(Future<Response<Object?>> Function() send) async {
    final response = await _guarded(send);
    final data = response.data;

    if (data is Map<String, dynamic>) return data;

    // A 204, an HTML error page from a proxy, or a JSON array where an object was expected.
    // All of them mean the same thing to a caller: this is not the response it can use.
    throw ApiMalformedResponse(statusCode: response.statusCode ?? 0);
  }

  /// Sends, mapping a transport failure onto [ApiFailure].
  Future<Response<Object?>> _guarded(Future<Response<Object?>> Function() send) async {
    try {
      return await send();
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
///
/// [session] installs the bearer token and the refresh-on-`401` behaviour (SHIP-50). It is
/// optional because two transports in this application deliberately do without it: the one the
/// refresh call itself travels on, and the ones tests build for something else entirely.
Dio buildDio({required String baseUrl, SessionTokens Function()? session}) {
  // `late` so the replay closure can name the transport it is being installed into. A replay
  // that bypassed the interceptor chain would skip the idempotency guard, and would send the
  // *previous* token because nothing would re-run `onRequest`.
  late final Dio dio;

  dio = Dio(
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

  if (session != null) {
    dio.interceptors.add(
      AuthInterceptor(session: session, replay: (options) => dio.fetch<Object?>(options)),
    );
  }

  return dio;
}

/// The environment this build was compiled for.
///
/// A provider so a test, or a future in-app diagnostics screen, can override it. It is still
/// decided at build time — overriding it does not move a release build to another deployment,
/// because the base URL was compiled in.
final apiEnvironmentProvider = Provider<ApiEnvironment>((ref) => ApiEnvironment.current);

/// The application's client.
///
/// The session is read through a closure rather than watched, and that is what keeps the two
/// providers from depending on each other's construction: the session needs a client to refresh
/// with, and the client needs the session to authenticate with. Nothing is read until a request
/// is actually made, by which point both exist.
final apiClientProvider = Provider<ApiClient>((ref) {
  return ApiClient.forEnvironment(
    ref.watch(apiEnvironmentProvider),
    () => ref.read(sessionProvider.notifier),
  );
});

/// A second client, carrying no credential and reacting to no `401` (SHIP-50).
///
/// One call needs it and no screen should ever reach for it: `POST /v1/auth/refresh`. That
/// endpoint takes its credential in the request body, so a bearer token on it would be the
/// expired token being sent to the endpoint that replaces it — and sending it through
/// [apiClientProvider] would put the refresh inside the interceptor whose answer to a failure is
/// to refresh. The contract closes that loop from its own side by answering `400` rather than
/// `401`; this closes it from the client's, so neither side is the only thing holding it.
final unauthenticatedApiClientProvider = Provider<ApiClient>((ref) {
  return ApiClient.forEnvironment(ref.watch(apiEnvironmentProvider));
});
