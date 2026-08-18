// SHIP-81c's middle clause, against the real repository: *"…and it reaches SHIP-81b's upload; the
// image is … cleared from app storage once uploaded."*
//
// # Why this is a transport-level test rather than a fake
//
// The claim is about **three requests in a fixed order, to two different hosts**, and about a file
// that must exist for the middle of it and be gone at the end. A fake repository can confirm none of
// that — it is the thing under test. So the API half runs through a real `ApiClient` over a scripted
// adapter, the store half runs through a real `CapturedImageStore` over a temporary directory, and
// the object store is a recording `ObjectUploader`, which is what it already is in the application:
// a **second transport**, carrying no session credential, because the store is a different host
// (`Docs/06` §5.2).
//
// The shape is `operation_sender_test.dart`'s, deliberately — it proves the same property for a
// delivery's proof photograph and this is the same exchange against a different pair of routes.

import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/capture/captured_image.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/sync/operation_sender.dart';
import 'package:shipper/features/profile/verification_document.dart';
import 'package:shipper/features/profile/verification_repository.dart';

import '../../support/capture_fixture.dart';

void main() {
  late CapturedImage image;

  setUpAll(() => image = compressCaptureSync(photograph(width: 800, height: 600)));

  group('the three requests one document takes', () {
    test('presign, then PUT to the store, then submit — in that order', () async {
      final harness = _repositoryAnswering(_script());

      await harness.repository.submit(
        kind: VerificationDocumentKind.insurance,
        image: image,
        idempotencyKey: 'action-1',
      );

      // **The paths, written out.** `operation_sender.dart` derives its presign path by chopping
      // the last segment off the record path, and that is exactly what would be wrong here: these
      // two routes are nested rather than siblings, so the chop gives
      // `/v1/provider/verification/uploads`, which nothing serves.
      expect(
        harness.adapter.requests.map((r) => '${r.method} ${r.path}').toList(),
        <String>[
          'POST /v1/provider/verification/documents/uploads',
          'POST /v1/provider/verification/documents',
        ],
      );

      // The PUT went to the **object store's** transport, at the signed URL, and never through the
      // API client — which is what keeps this session's bearer token off a request the platform
      // never sees.
      expect(harness.uploader.puts, hasLength(1));
      expect(harness.uploader.puts.single.url, 'https://store.example.test/put-here');
    });

    test('the PUT sends exactly the type and length that were signed', () async {
      // Both are inside the signature, and the contract says the type comes back lower-cased and
      // trimmed: *"use what you were given"*. A client echoing its own spelling gets
      // `403 SignatureDoesNotMatch` from the store, with no explanation, on a handset.
      final harness = _repositoryAnswering(_script(signedType: 'image/jpeg'));

      await harness.repository.submit(
        kind: VerificationDocumentKind.licence,
        image: image,
        idempotencyKey: 'action-1',
      );

      expect(harness.uploader.puts.single.contentType, 'image/jpeg');
      expect(harness.uploader.puts.single.length, image.length);

      final presign = harness.adapter.requests.first.data! as Map;
      expect(presign['content_length'], image.length);
      expect(presign['content_type'], capturedImageContentType);
    });

    test('the submission names the kind and the key the platform issued', () async {
      final harness = _repositoryAnswering(_script(objectKey: 'verification/p-1/o-9'));

      await harness.repository.submit(
        kind: VerificationDocumentKind.abnEvidence,
        image: image,
        idempotencyKey: 'action-1',
      );

      final submit = harness.adapter.requests.last.data! as Map;
      expect(submit['kind'], 'abn_evidence');
      expect(submit['object_key'], 'verification/p-1/o-9');

      // **And no `expires_at`.** `DocumentSubmission` carries an optional one and this client does
      // not collect it: `Docs/04` §3's *Decision required* — which documents must be renewed, and
      // how often — is Track-X row X-4 and is unanswered. A date invented here would be wrong for
      // an ABN extract by construction, because an ABN extract does not lapse.
      expect(submit.containsKey('expires_at'), isFalse);
    });
  });

  group('the two idempotency keys, which are deliberately not the same key', () {
    test('the submission carries the action’s and the presign carries a fresh one', () async {
      final harness = _repositoryAnswering(_script(), uploadKeys: <String>['upload-a']);

      await harness.repository.submit(
        kind: VerificationDocumentKind.licence,
        image: image,
        idempotencyKey: 'action-1',
      );

      String keyOf(RequestOptions options) => options.headers['Idempotency-Key'] as String;

      expect(keyOf(harness.adapter.requests.first), 'upload-a');
      expect(keyOf(harness.adapter.requests.last), 'action-1');
    });

    test('a second attempt reuses the action key and mints a second upload key', () async {
      // The contract states the consequence of getting this wrong from the platform's side: *"one
      // intent buys one upload slot… if you want a second slot, send a **new** key"*. Reusing the
      // action key on the presign replays the stored response, so every retry receives the same URL
      // with its expiry already run down — an operation that can never succeed again.
      final harness = _repositoryAnswering(
        _script(),
        uploadKeys: <String>['upload-a', 'upload-b'],
      );

      for (var attempt = 0; attempt < 2; attempt++) {
        await harness.repository.submit(
          kind: VerificationDocumentKind.licence,
          image: image,
          idempotencyKey: 'action-1',
        );
      }

      final keys = harness.adapter.requests
          .map((r) => r.headers['Idempotency-Key'] as String)
          .toList();
      expect(keys, <String>['upload-a', 'action-1', 'upload-b', 'action-1']);
    });
  });

  group('Docs/04 §3.1: cleared from app storage once uploaded', () {
    test('the file is written under verification/, not proof/', () async {
      // SHIP-81c's *Done when* in one assertion: the helpers "land under a directory that is not
      // proof/". `CaptureFolder` is a closed enum rather than a string, so an unlisted destination
      // does not compile — but the directory's *name* is not held by the compiler, and this is what
      // says which one.
      //
      // **Observed from a failed attempt rather than by racing a successful one.** The file exists
      // only between the write and the discard, and a test that reached in during the exchange
      // would be a test whose timing decided its verdict. A presign that answers `503` leaves the
      // file exactly where it was written, which is the same line under test and is deterministic.
      final store = temporaryStore(CaptureFolder.verification);
      final harness = _repositoryAnswering(_script(failAtRequest: 0), store: store);

      await expectLater(
        harness.repository.submit(
          kind: VerificationDocumentKind.licence,
          image: image,
          idempotencyKey: 'action-1',
        ),
        throwsA(isA<ApiFailure>()),
      );

      expect(store.directory.path, endsWith('verification'));
      expect(store.directory.path, isNot(contains('proof')));
      expect(
        store.directory.listSync().map((e) => e.uri.pathSegments.last).toList(),
        <String>['action-1.jpg'],
      );
    });

    test('and it is gone once the platform has recorded the submission', () async {
      final store = temporaryStore(CaptureFolder.verification);
      final harness = _repositoryAnswering(_script(), store: store);

      await harness.repository.submit(
        kind: VerificationDocumentKind.licence,
        image: image,
        idempotencyKey: 'action-1',
      );

      expect(store.directory.listSync(), isEmpty);
    });

    test('and it survives every failure, because a retry must not need a second photograph',
        () async {
      // Three failure points, one per request. The file has to outlive all three: a provider
      // standing there with their licence should be able to try again, and `Docs/04` §3.1 requires
      // the file cleared once **uploaded** rather than once attempted.
      // The three requests, by where they actually fail: the presign is the adapter's first, the
      // PUT is the uploader's and never reaches the adapter at all, and the submission is the
      // adapter's second.
      final cases = <String, ({_Harness harness, CapturedImageStore store})>{};

      for (final failure in <String>['the presign', 'the PUT', 'the submission']) {
        final store = temporaryStore(CaptureFolder.verification);
        cases[failure] = (
          harness: _repositoryAnswering(
            _script(
              failAtRequest: switch (failure) {
                'the presign' => 0,
                'the submission' => 1,
                _ => null,
              },
            ),
            store: store,
            uploader: _RecordingUploader(
              fail: failure == 'the PUT' ? const ApiUnreachable() : null,
            ),
          ),
          store: store,
        );
      }

      for (final entry in cases.entries) {
        await expectLater(
          entry.value.harness.repository.submit(
            kind: VerificationDocumentKind.licence,
            image: image,
            idempotencyKey: 'action-1',
          ),
          throwsA(isA<ApiFailure>()),
          reason: entry.key,
        );

        expect(
          entry.value.store.directory.listSync(),
          hasLength(1),
          reason: '${entry.key} failed and the compressed image was discarded, so the provider '
              'has to photograph the document again',
        );
      }
    });
  });

  group('the list', () {
    test('reads the caller’s own documents, with no parameter for whose', () async {
      final harness = _repositoryAnswering((options) => _json(<String, dynamic>{
            'data': <Object>[
              <String, dynamic>{
                'id': 'doc-1',
                'kind': 'licence',
                'content_type': 'image/jpeg',
                'content_length': 1000,
                'expires_at': null,
                'submitted_at': '2026-08-18T04:15:00Z',
              },
            ],
            'next_cursor': null,
            'has_more': false,
          }));

      final documents = await harness.repository.documents();

      expect(harness.adapter.requests.single.path, '/v1/provider/verification/documents');
      expect(harness.adapter.requests.single.queryParameters, isEmpty);
      expect(documents.single.kind, VerificationDocumentKind.licence);
    });

    test('and a kind this build does not know reads as null rather than as something else',
        () async {
      // The app-upgrade case, and the direction matters: relabelling somebody's insurance
      // certificate as a licence is worse than declining to name it. `OperationKind.byName` answers
      // the same way for the same reason.
      final harness = _repositoryAnswering((options) => _json(<String, dynamic>{
            'data': <Object>[
              <String, dynamic>{
                'id': 'doc-2',
                'kind': 'police_check',
                'content_type': 'image/jpeg',
                'content_length': 1000,
                'expires_at': null,
                'submitted_at': '2026-08-18T04:15:00Z',
              },
            ],
            'next_cursor': null,
            'has_more': false,
          }));

      expect((await harness.repository.documents()).single.kind, isNull);
    });
  });
}

typedef _Harness = ({
  VerificationRepository repository,
  _StubAdapter adapter,
  _RecordingUploader uploader,
});

_Harness _repositoryAnswering(
  ResponseBody Function(RequestOptions options) respond, {
  CapturedImageStore? store,
  _RecordingUploader? uploader,
  List<String> uploadKeys = const <String>['upload-a', 'upload-b', 'upload-c'],
}) {
  final adapter = _StubAdapter(respond);
  final dio = buildDio(baseUrl: 'https://api.example.test');
  dio.httpClientAdapter = adapter;

  final object = uploader ?? _RecordingUploader();
  var minted = 0;

  return (
    repository: ApiVerificationRepository(
      ApiClient(dio),
      object,
      Future<CapturedImageStore>.value(store ?? temporaryStore(CaptureFolder.verification)),
      mintUploadKey: () => uploadKeys[minted++ % uploadKeys.length],
    ),
    adapter: adapter,
    uploader: object,
  );
}

/// The platform answering both requests successfully, unless [failAtRequest] says otherwise.
ResponseBody Function(RequestOptions) _script({
  String signedType = 'image/jpeg',
  String objectKey = 'verification/p-1/o-1',
  int? failAtRequest,
}) {
  var seen = -1;

  return (options) {
    seen++;
    if (failAtRequest != null && seen == failAtRequest) {
      return _json(<String, dynamic>{
        'error': <String, dynamic>{'code': 'service_unavailable', 'message': 'no'},
      }, status: 503);
    }

    if (options.path.endsWith('/uploads')) {
      return _json(<String, dynamic>{
        'object_key': objectKey,
        'upload_url': 'https://store.example.test/put-here',
        'method': 'PUT',
        'content_type': signedType,
        'content_length': 1,
        'expires_at': '2026-08-18T04:30:00.000Z',
      });
    }

    return _json(<String, dynamic>{
      'id': 'doc-1',
      'kind': 'licence',
      'content_type': signedType,
      'content_length': 1,
      'expires_at': null,
      'submitted_at': '2026-08-18T04:15:00.000Z',
    }, status: 201);
  };
}

/// A transport that answers from a script instead of a socket. The shape `api_client_test.dart` and
/// `operation_sender_test.dart` both use, for the same reason: three methods is less than a mocking
/// package costs.
class _StubAdapter implements HttpClientAdapter {
  _StubAdapter(this.respond);

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

/// An uploader that remembers what it was asked to put where, and can be told to fail.
///
/// It stands in for the **object store**, which is a different host from the API and reachable
/// through no adapter this client installs. That separation is the thing worth asserting rather
/// than mocking away: nothing that goes to the store may carry this session's bearer token.
class _RecordingUploader implements ObjectUploader {
  _RecordingUploader({this.fail});

  final Object? fail;

  final puts = <({String url, String contentType, int length})>[];

  @override
  Future<void> put(String url, {required String contentType, required Uint8List bytes}) async {
    puts.add((url: url, contentType: contentType, length: bytes.length));
    final failure = fail;
    if (failure != null) throw failure;
  }
}

ResponseBody _json(Object body, {int status = 200}) => ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: [Headers.jsonContentType],
      },
    );
