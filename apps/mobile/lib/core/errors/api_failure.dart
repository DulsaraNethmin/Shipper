/// A failed request, in a form a screen can respond to.
///
/// The platform returns one error shape for every failure (SHIP-12):
///
/// ```json
/// {"error": {"code": "validation_failed", "message": "…", "request_id": "9f2c…",
///            "details": [{"field": "goods.category", "code": "…", "message": "…"}]}}
/// ```
///
/// **Branch on [ApiErrorResponse.code], never on [ApiErrorResponse.message].** The message is
/// copy: it gets reworded, and one day translated, without a client release. A build already
/// installed on a phone that switched on message text breaks on a copy edit made months later
/// — and Dart has no over-the-air fix (`Docs/07` §1).
sealed class ApiFailure implements Exception {
  const ApiFailure();

  /// What to put in front of the user when nothing more specific has been written yet.
  ///
  /// Australian English, no jargon, and never the underlying exception — `Docs/07` §7 treats
  /// user-facing copy as build work.
  String get userMessage;
}

/// The platform answered, and said no in its own error contract.
final class ApiErrorResponse extends ApiFailure {
  const ApiErrorResponse({
    required this.statusCode,
    required this.code,
    required this.message,
    this.requestId,
    this.details = const <ApiFieldError>[],
  });

  final int statusCode;

  /// The machine-readable code. This is the only part worth branching on.
  final String code;

  /// The platform's own wording. Safe to show, never safe to switch on.
  final String message;

  /// Present in the body as well as the response header, because a user reporting a problem
  /// sends a screenshot and a header does not survive one (SHIP-12).
  final String? requestId;

  final List<ApiFieldError> details;

  @override
  String get userMessage => message;

  @override
  String toString() => 'ApiErrorResponse($statusCode, $code, requestId: $requestId)';

  /// Reads the contract out of a decoded body, tolerating everything about it that might
  /// change.
  ///
  /// `Docs/07` §6 requires unknown fields to be ignored so that additive server changes need
  /// no app release; this reads the four keys it knows and steps over the rest. It also has to
  /// cope with a body that is not the contract at all — a proxy's HTML error page, or an empty
  /// 502 — which is why every field is checked rather than cast.
  static ApiErrorResponse fromJson(int statusCode, Object? body, {String? fallbackCode}) {
    final envelope = body is Map ? body['error'] : null;
    final error = envelope is Map ? envelope : const <Object?, Object?>{};

    final details = error['details'];

    return ApiErrorResponse(
      statusCode: statusCode,
      code: _string(error['code']) ?? fallbackCode ?? 'unknown_error',
      message: _string(error['message']) ?? 'Something went wrong. Please try again.',
      requestId: _string(error['request_id']),
      details: details is List
          ? details.whereType<Map<Object?, Object?>>().map(ApiFieldError.fromJson).toList()
          : const <ApiFieldError>[],
    );
  }
}

/// One field-level rejection inside [ApiErrorResponse.details].
final class ApiFieldError {
  const ApiFieldError({required this.field, required this.code, required this.message});

  final String field;
  final String code;
  final String message;

  static ApiFieldError fromJson(Map<Object?, Object?> json) => ApiFieldError(
        field: _string(json['field']) ?? '',
        code: _string(json['code']) ?? 'invalid',
        message: _string(json['message']) ?? '',
      );

  @override
  String toString() => 'ApiFieldError($field, $code)';
}

/// The request never reached the platform, or the reply never came back.
///
/// `Docs/07` §4 is built on this being ordinary rather than exceptional: drivers and providers
/// work where there is no signal, and the app's answer is to queue the operation and say so,
/// not to show an error.
final class ApiUnreachable extends ApiFailure {
  const ApiUnreachable({this.timedOut = false});

  final bool timedOut;

  @override
  String get userMessage => timedOut
      ? 'The connection timed out. Check your signal and try again.'
      : 'No connection. Check your signal and try again.';

  @override
  String toString() => 'ApiUnreachable(timedOut: $timedOut)';
}

/// The platform answered with something that is not the error contract and not the response
/// that was expected.
///
/// Kept separate from [ApiErrorResponse] because it means a different thing: not "the platform
/// refused" but "something between the app and the platform is wrong". The body is deliberately
/// not carried — it may hold anything, and `Docs/07` §3 keeps tokens out of logs and crash
/// reports.
final class ApiMalformedResponse extends ApiFailure {
  const ApiMalformedResponse({required this.statusCode});

  final int statusCode;

  @override
  String get userMessage => 'Something went wrong. Please try again.';

  @override
  String toString() => 'ApiMalformedResponse($statusCode)';
}

String? _string(Object? value) => value is String && value.isNotEmpty ? value : null;
