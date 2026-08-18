import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/core/capture/captured_image.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/sync/operation_sender.dart' show ObjectUploader, objectUploaderProvider;
import 'package:shipper/features/profile/verification_document.dart';

/// The verification endpoints a provider screen calls (SHIP-81b, SHIP-81c).
///
/// Three routes are served on the provider's side and these are two of them — the third,
/// `GET /v1/provider/verification`, answers the *state* `Docs/04` §4 fixes, and belongs to whichever
/// ticket draws it rather than to the one that captures evidence.
///
/// Every operation here is about **the caller's own record**: the contract has no parameter for
/// whose verification to read and no field for naming a provider, because *"a provider id taken from
/// client input would be an authorisation decision made on the device"* (`Docs/07` §3).
///
/// An interface with one real implementation, following `FleetRepository` for the same reason: a
/// widget test has to be able to hand a screen something that answers, and the alternative — a stub
/// transport under a concrete class — makes every screen test a test of `dio`'s wiring as well.
abstract interface class VerificationRepository {
  /// `GET /v1/provider/verification/documents` — what this provider has already submitted.
  ///
  /// Newest first within each kind, so the current licence is the first `licence` entry. The
  /// envelope is `ApiPage`'s, and the contract says `next_cursor` is always `null` and `has_more`
  /// always `false` — four kinds and a handful of retakes is bounded by the domain rather than by a
  /// page size — so this takes no cursor and there is no second page to ask for.
  ///
  /// No idempotency key: a read changes nothing.
  Future<List<VerificationDocument>> documents();

  /// A compressed photograph → the platform, in three requests, as one of the four kinds.
  ///
  /// ## Why it is three requests and not one, and why they are one method
  ///
  /// `Docs/06` §5.2 keeps the bytes out of the API and `Docs/04` §3.1 requires it in as many words:
  /// the client asks for a place to put them, PUTs them there directly, and then tells the platform
  /// about the object.
  ///
  ///  1. `POST /v1/provider/verification/documents/uploads` — a signed URL and an object key;
  ///  2. `PUT <upload_url>` — the bytes, to the store, on [ObjectUploader]'s own transport;
  ///  3. `POST /v1/provider/verification/documents` — `{kind, object_key}`, the recorded fact.
  ///
  /// **One method rather than three, because two of the three are useless alone.** An object in the
  /// store *"is evidence of nothing until it is submitted"* and no administrator can see it; a
  /// submission naming a key nothing was PUT to answers `409 profiles_document_not_uploaded`. A
  /// caller that could reach the middle of this sequence would be a caller that could leave a
  /// provider's licence in a bucket with no row, which is the one outcome invisible from both ends.
  ///
  /// ## Two keys, and only one of them is the action's
  ///
  /// Step 3 carries [idempotencyKey], which is `ActionKey`'s — minted where the provider acted and
  /// reused unchanged if the same submission is retried after a dropped connection.
  ///
  /// Step 1 carries **a key of its own, per attempt**, and reusing the action's key there would
  /// strand the submission: SHIP-15's middleware replays a stored response, so every retry would
  /// receive the *same* URL with its expiry already run down. The contract states the consequence
  /// from the platform's side — *"one intent buys one upload slot… if you want a second slot, send
  /// a **new** key"* — and says a fresh key is affordable, because nothing durable is written and at
  /// most one unreferenced object is left in the bucket. `operation_sender.dart` reached the same
  /// conclusion for a delivery's proof and this is the same argument.
  ///
  /// ## `Docs/04` §3.1's three guarantees, and which line holds each
  ///
  /// - **Never written to the photo library** — [image] came from `core/capture`, and the file is
  ///   written by [CapturedImageStore], whose directory is a closed [CaptureFolder] and whose
  ///   `write` takes a name rather than a path.
  /// - **Compressed on the device** — the parameter is a [CapturedImage] rather than bytes, so the
  ///   compressor is not something this method can be called without having run. `content_length`
  ///   is signed against the compressed size and there is no path on which an original is uploaded.
  /// - **Cleared from app storage once uploaded** — the file is discarded after step 3 returns, and
  ///   **only after**: every failing path deliberately leaves it on the device, so a provider whose
  ///   connection dropped mid-upload retries rather than photographing their licence again. That is
  ///   the whole reason it is written down instead of held in memory.
  ///
  /// ## There is no `expires_at`
  ///
  /// `DocumentSubmission` carries an optional one — *"optional, and optional deliberately"* — and
  /// this client does not collect it. `Docs/04` §3's *Decision required* (which documents must be
  /// renewed, and how often; Track-X row **X-4**) is owned by legal and insurance advisers, is
  /// unanswered, and *"remains genuinely outside engineering's competence to settle"*. A screen
  /// asking every provider for a date would be this application answering it four times over — and
  /// getting one of the four wrong by construction, because an ABN extract does not lapse at all.
  ///
  /// It is stated at submission or never, and the table is append-only, so collecting it later is a
  /// text field and a second submission. Collecting it now and being wrong is a data-cleanup.
  Future<VerificationDocument> submit({
    required VerificationDocumentKind kind,
    required CapturedImage image,
    required String idempotencyKey,
  });
}

/// The real one, over [ApiClient] and [ObjectUploader].
///
/// **The upload does not go through the offline queue, and that is a decision rather than an
/// omission.** `core/queue`'s `OperationKind` set is closed by a private constructor, so carrying
/// this would mean a third kind and a generalised `ApiOperationSender._sendWithProof` — whose
/// presign path is derived by chopping the last segment off `/v1/jobs/{id}/milestones`, which is
/// right for delivery's shape and wrong for this pair of routes. That is the mechanical half. The
/// product half is why it was not worth doing anyway:
///
/// - `Docs/07` §4 puts the queue where it is needed — *"pickup bays, warehouses and rural routes
///   have no usable signal, and a driver cannot be asked to stand still until a request
///   completes."* Verification is onboarding. Nobody photographs their insurance certificate in a
///   valley, and nobody is standing at somebody's door while they do it.
/// - The queue's guarantee is FIFO **within an ordering key**, and a document submission is ordered
///   against nothing: the contract makes the four kinds independent and a retake a new submission.
/// - **A queued submission would confirm the wrong thing.** The provider's next question is "may I
///   bid yet", and the honest answer needs the platform to have taken the document — an object with
///   no row is *"evidence of nothing"*. "Saved on this phone" is the right answer for a delivery
///   recorded at a door and the wrong one for the step that unblocks somebody's ability to earn.
///
/// What is kept from the queue's design is the half that matters here: the file survives a failure,
/// and the idempotency key belongs to the action rather than to the attempt.
final class ApiVerificationRepository implements VerificationRepository {
  const ApiVerificationRepository(
    this._client,
    this._uploader,
    this._store, {
    this.mintUploadKey = newIdempotencyKey,
  });

  final ApiClient _client;
  final ObjectUploader _uploader;

  /// Where the compressed image is kept until the platform has recorded it.
  final Future<CapturedImageStore> _store;

  /// A fresh idempotency key for step 1, on every attempt. See [VerificationRepository.submit].
  ///
  /// Public because it is injected only so a test can name the key it asserts on, and a private
  /// field with a public named parameter is the shape the analyzer asks not to write.
  final String Function() mintUploadKey;

  /// Product endpoints live under `/v1` (SHIP-13). The base URL carries the host and nothing else.
  static const _documents = '/v1/provider/verification/documents';

  /// **Written out rather than derived from [_documents].** `operation_sender.dart` derives its
  /// presign path by chopping the last segment, and its own note says that is a rule about two named
  /// routes rather than string cleverness. Here the two routes are nested rather than siblings, so
  /// the same chop would produce `/v1/provider/verification/uploads` — which is served by nothing:
  /// a `404` at the first request of three, on a handset, from a helper that looked correct.
  static const _uploads = '$_documents/uploads';

  @override
  Future<List<VerificationDocument>> documents() async {
    final page = ApiPage.fromJson(
      await _client.getJson(_documents),
      VerificationDocument.fromJson,
    );
    return page.data;
  }

  @override
  Future<VerificationDocument> submit({
    required VerificationDocumentKind kind,
    required CapturedImage image,
    required String idempotencyKey,
  }) async {
    final store = await _store;

    // Named after the action's key rather than after the kind, following the proof photograph's own
    // naming for the same two reasons: two attempts at one submission reuse one file rather than
    // filling the directory, and a retake — a *different* action, and so a different key — cannot
    // overwrite an image whose upload is still in flight.
    final file = await store.write(
      image,
      name: '$idempotencyKey.$capturedImageFileExtension',
    );

    final issued = await _client.postJson(
      _uploads,
      idempotencyKey: mintUploadKey(),
      body: <String, dynamic>{
        'content_type': capturedImageContentType,
        'content_length': image.length,
      },
    );

    final uploadUrl = issued['upload_url'];
    final objectKey = issued['object_key'];
    // The platform echoes the type it signed, and it is used rather than the declared one: it has
    // already normalised the spelling by the time it signs, and a client sending back its own
    // rendering gets a signature failure with no explanation. The contract says it directly —
    // *"use what you were given"*.
    final signedType = issued['content_type'];

    if (uploadUrl is! String || objectKey is! String || signedType is! String) {
      throw const ApiMalformedResponse(statusCode: 200);
    }

    await _uploader.put(uploadUrl, contentType: signedType, bytes: image.bytes);

    final recorded = VerificationDocument.fromJson(
      await _client.postJson(
        _documents,
        idempotencyKey: idempotencyKey,
        body: <String, dynamic>{'kind': kind.wire, 'object_key': objectKey},
      ),
    );

    // `Docs/04` §3.1's third clause, and this is the only line that can honour it: the platform has
    // the image, so the copy on the handset is now a second copy of an identity document with
    // nothing watching it. Everything above can throw, and every one of those paths deliberately
    // leaves the file behind — see [VerificationRepository.submit].
    await store.discard(file.path);

    return recorded;
  }
}

/// Where compressed verification documents are kept until the platform has them.
///
/// `CaptureFolder.verification` rather than `CaptureFolder.proof`, which is SHIP-81c's *Done when*
/// in one line: a driver licence must not land under `proof/`. The name matches the platform's own
/// object-key prefix — `verification/<provider>/<uuid>` — so the handset's directory and the
/// bucket's read the same way.
///
/// A `FutureProvider` because resolving the application's private directory is a platform call. A
/// host test overrides it with a store over a temporary directory, the same way `proofStoreProvider`
/// is overridden rather than reaching a real application-support directory.
final verificationStoreProvider = FutureProvider<CapturedImageStore>(
  (ref) => CapturedImageStore.open(CaptureFolder.verification),
);

/// The application's verification repository.
final verificationRepositoryProvider = Provider<VerificationRepository>((ref) {
  return ApiVerificationRepository(
    ref.watch(apiClientProvider),
    ref.watch(objectUploaderProvider),
    ref.watch(verificationStoreProvider.future),
  );
});
