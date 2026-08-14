import 'dart:io';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/queue/queued_operation.dart';

/// What the sync worker needs of the network, and nothing more (SHIP-125).
///
/// **Declared here, by the consumer**, which is the rule `CLAUDE.md` states for the Go service
/// and `auth_interceptor.dart` already follows on this side: one method, named by what the worker
/// does rather than by how it is done. The worker therefore has no opinion about `dio`, about
/// authentication, or about how a proof photograph is uploaded, and a test hands over a scripted
/// implementation instead of a stubbed transport.
abstract interface class OperationSender {
  /// Offers [operation] to the platform, returning normally when it was accepted.
  ///
  /// Throws `ApiFailure` when it was not — the worker reads which kind through `decideFrom`.
  /// **Anything else thrown is treated as this build being unable to send the operation at all**,
  /// so an implementation must not raise a bare error for an ordinary network problem.
  ///
  /// The operation's `idempotencyKey` goes on the wire unchanged, on this attempt and on every
  /// later one. That is the whole of `Docs/07` §4's rule, and the reason the key is a stored
  /// column rather than something minted per attempt.
  Future<void> send(QueuedOperation operation);
}

/// Puts bytes at a URL somebody else signed.
///
/// **A second transport, and it must stay one.** The URL is issued by
/// `POST /v1/jobs/{id}/proof-uploads` and points at the object store, which is a different host from
/// the API (`Docs/06` §5.2) — so sending it through [ApiClient] would attach this session's bearer
/// token to a request the platform never sees, and add an `Idempotency-Key` the store would refuse
/// as an unsigned header. It carries no credential of its own either: the signature in the URL is
/// the whole of the authorisation, which is the property `internal/delivery/proof.go` spends a
/// paragraph on.
abstract interface class ObjectUploader {
  /// Uploads [bytes] to [url]. Throws [ApiFailure] for anything that is not a success.
  Future<void> put(String url, {required String contentType, required Uint8List bytes});
}

/// [ObjectUploader] over a bare `dio` — no base URL, no interceptors, no session.
final class HttpObjectUploader implements ObjectUploader {
  HttpObjectUploader([Dio? dio])
      : _dio = dio ??
            Dio(
              BaseOptions(
                // Longer than the API's, and deliberately: this is the one request in the
                // application whose body is a megabyte rather than a sentence, sent from a handset
                // on whatever connection it has just found. `STORAGE_UPLOAD_TTL` is the other end
                // of the same thought — SHIP-15r split it from the download lifetime precisely
                // because "long enough for a phone to finish a slow PUT" and "long enough for an
                // image to render" are different numbers.
                connectTimeout: const Duration(seconds: 20),
                sendTimeout: const Duration(minutes: 2),
                receiveTimeout: const Duration(seconds: 30),
                // The store answers XML, and none of it is read: what matters is the status.
                responseType: ResponseType.plain,
              ),
            );

  final Dio _dio;

  @override
  Future<void> put(String url, {required String contentType, required Uint8List bytes}) async {
    try {
      await _dio.put<Object?>(
        url,
        data: Stream<List<int>>.value(bytes),
        options: Options(
          headers: <String, Object>{
            Headers.contentTypeHeader: contentType,
            // Signed into the URL, so it is stated rather than left to the transport. A pre-signed
            // PUT has exactly one bound available to it and this is it.
            Headers.contentLengthHeader: bytes.length,
          },
        ),
      );
    } on DioException catch (e) {
      throw _storeFailure(e);
    }
  }

  /// The store's failures, mapped so the worker retries rather than quarantining.
  ///
  /// The object store does **not** speak the platform's error contract — it answers XML — so
  /// nothing here can be an [ApiErrorResponse] with a code somebody branches on.
  /// [ApiMalformedResponse] is the honest shape and `decideFrom` reads it as `retry`, which is the
  /// right answer for every case this can be: an expired signature (the next attempt mints a fresh
  /// one), a proxy in the way, or a store that is having a bad minute.
  static ApiFailure _storeFailure(DioException e) {
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.sendTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.transformTimeout:
        return const ApiUnreachable(timedOut: true);
      case DioExceptionType.connectionError:
      case DioExceptionType.unknown:
      case DioExceptionType.cancel:
        return const ApiUnreachable();
      case DioExceptionType.badCertificate:
      case DioExceptionType.badResponse:
        return ApiMalformedResponse(statusCode: e.response?.statusCode ?? 0);
    }
  }
}

/// The application's sender, over [ApiClient].
///
/// It carries the session's bearer token and the refresh-and-replay behaviour, because it uses
/// the same client every screen does — which is what makes a `401` mid-drain one refresh rather
/// than a refusal (SHIP-50).
final class ApiOperationSender implements OperationSender {
  const ApiOperationSender(this._client, this._uploader);

  final ApiClient _client;
  final ObjectUploader _uploader;

  @override
  Future<void> send(QueuedOperation operation) async {
    final attachment = operation.attachmentPath;
    if (attachment != null) {
      await _sendWithProof(operation, attachment);
      return;
    }

    await _client.send(
      operation.method,
      operation.path,
      idempotencyKey: operation.idempotencyKey,
      body: operation.body,
    );
  }

  /// The three-request exchange one proof photograph takes (SHIP-114, SHIP-115, SHIP-130).
  ///
  /// ## Why it is three requests and not one
  ///
  /// `Docs/06` §5.2 keeps the bytes out of the API: the client asks for a place to put them, puts
  /// them there directly, and then tells the platform about the object. So one queued operation is
  ///
  ///  1. `POST /v1/jobs/{id}/proof-uploads` — a signed URL and an object key;
  ///  2. `PUT <upload_url>` — the bytes, to the store, on [ObjectUploader]'s own transport;
  ///  3. `POST /v1/jobs/{id}/milestones` with `proof: {object_key}` — the recorded fact.
  ///
  /// **The row's `path` is the third request**, because that is the one that records something. The
  /// first is derived from it by [_presignPathFor], which is a rule about two routes rather than a
  /// guess — see that function.
  ///
  /// SHIP-125 left this as an `UnimplementedError` naming "the multipart send", and **that name was
  /// wrong**: there is no multipart anywhere in this exchange, and the API never sees the image at
  /// all. The comment was written before SHIP-114 landed and is corrected here rather than carried.
  ///
  /// ## Two keys, and only one of them is the operation's
  ///
  /// Step 3 carries the operation's stored key, unchanged on every attempt. That is the one that
  /// matters: it is what makes a replay after a dropped connection replay the platform's original
  /// outcome instead of recording a second delivery.
  ///
  /// Step 1 carries **a key of its own, per attempt**. Reusing the stored key there would look
  /// tidier and would strand the operation: SHIP-15's middleware replays a stored response, so every
  /// retry would receive the *same* URL with its expiry already run down — and an operation that
  /// failed after being issued one would then re-receive a dead URL on every attempt until the
  /// idempotency entry expired from Redis, which is hours. A fresh key per attempt mints a fresh
  /// URL and a fresh object key, and `internal/delivery/proof.go` is explicit that this is
  /// affordable: nothing durable was written, so at most one unreferenced object is left in the
  /// bucket, and SHIP-115 is what decides which key is the job's proof.
  ///
  /// ## The file
  ///
  /// A file that has gone is thrown as an ordinary error, which the worker reads as `unsupported`
  /// and quarantines — the outcome `queued_operation.dart` predicted for exactly this case. It is
  /// not retried, because a missing file does not come back.
  ///
  /// A file the platform has accepted is **deleted here**, and this is the only place that happens.
  /// The worker removes the row; nothing else knows the row pointed at a megabyte.
  Future<void> _sendWithProof(QueuedOperation operation, String attachment) async {
    final contentType = operation.kind.attachmentContentType;
    if (contentType == null) {
      // A kind that carries no attachment, with one attached. Nothing can send it, and the worker
      // quarantines it where a person can see it rather than looping.
      throw StateError(
        'Operation ${operation.id} is a ${operation.kind} and carries an attachment, which that '
        'kind does not declare a content type for.',
      );
    }

    final file = File(attachment);
    final Uint8List bytes;
    try {
      bytes = await file.readAsBytes();
    } on FileSystemException catch (e) {
      throw StateError(
        'Operation ${operation.id} points at $attachment, which cannot be read: ${e.osError}',
      );
    }

    final issued = await _client.postJson(
      _presignPathFor(operation.path),
      idempotencyKey: '${operation.idempotencyKey}.upload.${operation.attempts}',
      body: <String, dynamic>{'content_type': contentType, 'content_length': bytes.length},
    );

    final uploadUrl = issued['upload_url'];
    final objectKey = issued['object_key'];
    // The platform echoes the type it signed, and it is used rather than the declared one: it has
    // already normalised the spelling by the time it signs, and a client sending back its own
    // rendering gets a signature failure with no explanation.
    final signedType = issued['content_type'];

    if (uploadUrl is! String || objectKey is! String || signedType is! String) {
      throw const ApiMalformedResponse(statusCode: 200);
    }

    await _uploader.put(uploadUrl, contentType: signedType, bytes: bytes);

    await _client.send(
      operation.method,
      operation.path,
      idempotencyKey: operation.idempotencyKey,
      body: <String, dynamic>{
        ...operation.body,
        'proof': <String, dynamic>{'object_key': objectKey},
      },
    );

    try {
      if (await file.exists()) await file.delete();
    } on FileSystemException {
      // The platform has it, which is what the driver cares about. A file that would not delete is
      // a wasted megabyte on the handset, and failing an accepted operation over one would put a
      // recorded delivery back in the queue to be sent again.
    }
  }

  /// `/v1/jobs/{id}/milestones` → `/v1/jobs/{id}/proof-uploads`.
  ///
  /// A derivation rather than a second stored column, and it is a rule about **two named routes**
  /// rather than string cleverness: both live under `/v1/jobs/{id}/`, the job identifier is the same
  /// in both, and a queued row cannot hold a second path without SHIP-124's schema growing a column
  /// for one kind. The alternative considered was putting the job id in the body, which the platform
  /// would ignore and a reader would have to be told to ignore too.
  ///
  /// `operation_sender_test.dart` pins it, so a route renamed on the platform fails here rather than
  /// on a driver's handset.
  static String _presignPathFor(String recordPath) {
    final cut = recordPath.lastIndexOf('/');
    if (cut < 0) return recordPath;
    return '${recordPath.substring(0, cut)}/proof-uploads';
  }
}

/// The application's sender.
final operationSenderProvider = Provider<OperationSender>((ref) {
  return ApiOperationSender(ref.watch(apiClientProvider), ref.watch(objectUploaderProvider));
});

/// The application's uploader. See [ObjectUploader] for why it is not the API's client.
final objectUploaderProvider = Provider<ObjectUploader>((ref) => HttpObjectUploader());
