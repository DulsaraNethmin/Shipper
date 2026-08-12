import 'dart:async';

import 'package:dio/dio.dart';

import 'package:shipper/core/api/idempotency_interceptor.dart';

/// What `core/api` needs from the session in order to authenticate a request and to recover from
/// a `401` (SHIP-50).
///
/// **Declared here, by the consumer, and deliberately narrow.** `CLAUDE.md`'s rule for the Go
/// service is that infrastructure imports neither a domain nor an adapter — it takes an interface
/// it declares itself and the composition root supplies the implementation. The mobile shape is
/// the same and the risk is the same: every feature imports `core/api`, so a single import of a
/// feature from inside it welds all seven to that feature through an edge that appears in no
/// feature's own files. Two members is the whole of what this interceptor needs; the session
/// controller in `core/auth` implements them and `apiClientProvider` hands over a closure.
abstract interface class SessionTokens {
  /// The access token this device is currently holding, or `null` when it has none.
  ///
  /// `null` is ordinary rather than exceptional: it is a cold start before the first refresh, and
  /// it is every request made while signed out. The request goes without a bearer header,
  /// which is what the platform's public endpoints expect anyway.
  String? get accessToken;

  /// Obtains an access token to replace [stale], or `null` when the session cannot continue.
  ///
  /// [stale] is the token the failed request actually carried. It is passed rather than assumed
  /// because of the case that is invisible until it happens in the field: two requests go out
  /// with the same token, the first refreshes, and the second's `401` arrives afterwards. That
  /// second request must **not** refresh again — the token it was told about is already replaced,
  /// and a second refresh presenting an already-rotated token is what SHIP-40 revokes the entire
  /// device session for. The implementation answers with the current token instead.
  Future<String?> refreshedAccessToken(String? stale);
}

/// Attaches the bearer token, and turns a `401` into one refresh and one replay (SHIP-50).
///
/// ## The whole of the design, in the order it happens
///
/// 1. Every outgoing request gets a `Bearer` credential header if a token is held. Nothing is
///    attached when there is none, and the request is not blocked waiting for one.
/// 2. A `401` comes back. The interceptor asks the session for a token to replace the one that
///    request carried.
/// 3. If it gets one, the **original** [RequestOptions] is sent again — same method, same path,
///    same body, and above all the **same `Idempotency-Key`**.
/// 4. If it does not, the original `401` is passed through untouched. The session has already
///    decided what that means; a screen sees a failure, not a hang.
///
/// ## Step 3 is the half that a plausible implementation gets wrong
///
/// Replaying with a *fresh* idempotency key turns one logical action into two. `Docs/07` §4 has
/// the key generated where the user acts and reused unchanged across every retry, and a
/// refresh-and-replay is a retry of that same action by any reading — the platform saw the first
/// attempt, refused it for a credential reason, and is about to see it again. Reusing the
/// options object is what makes this correct by construction rather than by remembering: the
/// header is already in it.
///
/// ## It cannot loop
///
/// A replayed request that is refused again is marked, and the mark stops a second refresh. That
/// matters because a `401` after a refresh is not a credential problem the client can fix —
/// either the platform is refusing this caller for a reason a new token will not change, or
/// something is wrong with the token itself, and both of those retried in a loop are a client
/// hammering an endpoint it will never satisfy.
///
/// The refresh endpoint is the other half of that argument, and it is closed on the platform's
/// side: `POST /v1/auth/refresh` answers `400`, never `401`, precisely so an interceptor of this
/// shape cannot be sent round the loop by the endpoint that is supposed to end it (SHIP-42).
/// This client also sends it through a transport that carries no interceptor at all — see
/// `unauthenticatedApiClientProvider`.
class AuthInterceptor extends Interceptor {
  AuthInterceptor({required this.session, required this.replay});

  /// Read lazily. The transport is constructed before the session provider can be reached, and
  /// the session is not needed until a request is actually made.
  final SessionTokens Function() session;

  /// Sends a request again. Supplied by `buildDio` as the very transport this interceptor is
  /// installed in, so a replay goes through the whole chain — including the idempotency guard,
  /// which is a check worth running twice rather than a step worth skipping.
  final Future<Response<Object?>> Function(RequestOptions options) replay;

  /// Marks a request that has already been refreshed and replayed once.
  ///
  /// In `extra` rather than in a header: `extra` is client-side only and never reaches the wire,
  /// and a marker the platform could see would be a client telling a server about its own retry
  /// logic.
  static const replayedFlag = 'shipper.auth.replayed';

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    final token = session().accessToken;
    if (token != null && token.isNotEmpty) {
      options.headers[ApiHeaders.bearer] = 'Bearer $token';
    }
    handler.next(options);
  }

  @override
  void onError(DioException err, ErrorInterceptorHandler handler) {
    // `onError` is synchronous by signature and the recovery is not. Kicking it off here rather
    // than declaring an `async` override keeps the analyzer honest about the future being
    // deliberately unawaited — the handler, not this method's return, is what resumes the call.
    unawaited(_recover(err, handler));
  }

  Future<void> _recover(DioException err, ErrorInterceptorHandler handler) async {
    if (err.response?.statusCode != 401 || err.requestOptions.extra[replayedFlag] == true) {
      handler.next(err);
      return;
    }

    final String? token;
    try {
      token = await session().refreshedAccessToken(_bearerOf(err.requestOptions));
    } catch (_) {
      // The session has its own answer for every failure it can name, so anything thrown out of
      // it is a programming error rather than a refusal. Surfacing the original 401 is still the
      // right outcome for the caller: the request failed, and it failed for the reason the
      // platform gave.
      handler.next(err);
      return;
    }

    if (token == null) {
      handler.next(err);
      return;
    }

    final options = err.requestOptions..extra[replayedFlag] = true;

    try {
      handler.resolve(await replay(options));
    } on DioException catch (retried) {
      handler.next(retried);
    }
  }

  /// The token the failed request carried, or `null` if it carried none.
  static String? _bearerOf(RequestOptions options) {
    final Object? header = options.headers[ApiHeaders.bearer];
    if (header is! String || !header.startsWith('Bearer ')) return null;

    final token = header.substring('Bearer '.length);
    return token.isEmpty ? null : token;
  }
}
