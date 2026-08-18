import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/capture/capture_providers.dart';
import 'package:shipper/core/capture/captured_image.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/profile/verification_document.dart';
import 'package:shipper/features/profile/verification_repository.dart';

/// Where one document's capture has got to.
///
/// The same shape `ProofCaptureStage` uses, and deliberately **not** the same enum: a delivery's
/// capture ends at `queued` because `Docs/07` §4 sends it through the offline queue, and this one
/// ends at `submitted` because it does not. Sharing the type would have meant a `queued` value that
/// is unreachable here and a `submitted` value that is unreachable there, which is a state machine
/// that describes neither screen.
enum DocumentCaptureStage {
  /// Nothing has been taken yet.
  waiting,

  /// The photograph is being made small enough to send.
  ///
  /// Its own stage because it is the one step with a delay a person notices: a full-resolution
  /// decode and re-encode is hundreds of milliseconds, and a shutter that appears to do nothing is
  /// a shutter that gets pressed twice.
  compressing,

  /// The three requests are in flight.
  ///
  /// Distinct from [compressing] because the two fail differently and are worth different words: a
  /// compression failure is about the image, and this one is about the connection.
  uploading,

  /// The platform has recorded it against this provider's verification record.
  submitted,

  /// It could not be sent. [DocumentCaptureState.message] says what a person can do about it.
  ///
  /// **The compressed image is still on the device** whenever this is reached after the write, so
  /// [CaptureDocumentController.retry] is a second attempt at the upload rather than a second
  /// photograph. See `verification_repository.dart`.
  failed,
}

/// What a document capture screen draws.
@immutable
final class DocumentCaptureState {
  const DocumentCaptureState({this.stage = DocumentCaptureStage.waiting, this.message});

  final DocumentCaptureStage stage;

  /// Copy for a person, or `null`. Written here rather than carried out of `core/` for the reason
  /// `capture_proof_controller.dart` gives: the words live in the layer that draws them.
  final String? message;

  bool get isBusy =>
      stage == DocumentCaptureStage.compressing || stage == DocumentCaptureStage.uploading;

  @override
  bool operator ==(Object other) =>
      other is DocumentCaptureState && other.stage == stage && other.message == message;

  @override
  int get hashCode => Object.hash(stage, message);
}

/// Compresses one photograph of one document, stores it privately, and submits it (SHIP-81c).
///
/// ## The four clauses of the *Done when*, and where each one is
///
/// **Photographed in `features/profile/`** is this class and `capture_document_screen.dart`.
/// `Docs/07` §2 assigns verification evidence to `profile/` explicitly and closes the feature list
/// at seven, so there is no `verification/` folder and this is not an eighth feature.
///
/// **It reaches SHIP-81b's upload** is `VerificationRepository.submit`, which performs the presign,
/// the PUT and the submission as one operation.
///
/// **Never written to the photo library, compressed on the device, cleared from app storage once
/// uploaded** is `core/capture` and the repository between them, and none of the three is new
/// behaviour — which is the point. They were built for SHIP-130 and they moved rather than being
/// rewritten, so a provider's licence is held to the guarantee a delivery photograph already was.
///
/// ## It compresses, and the repository sends
///
/// The split is where the delay a person notices is. `Docs/07` §4's shape for a capture screen is a
/// stage per perceptible step, and the compression is one — so it happens here, where the state can
/// be [DocumentCaptureStage.compressing] while it runs, and the repository is handed a
/// [CapturedImage] it could not have been called without.
///
/// ## What it does not do
///
/// **It decides no verification state.** `Docs/04` §4's five outcomes are an administrator's, and
/// `profiles.Service.Decide` is deliberately reachable from no route a provider can call. Submitting
/// a document is evidence arriving, not a state changing, and this screen must never imply
/// otherwise.
class CaptureDocumentController extends Notifier<DocumentCaptureState> {
  CaptureDocumentController(this.kind);

  /// Which of the four is being photographed. From the route, which is the only place it comes from.
  final VerificationDocumentKind kind;

  /// The action's key, held across a retry of the same submission (`Docs/07` §4).
  ///
  /// **An `ActionKey` rather than a fresh key per attempt**, because a submission whose connection
  /// dropped after the platform committed must replay that outcome instead of recording a second
  /// document. It is retired the moment the outcome is known either way — see `ActionKey.settled`.
  late final ActionKey _key = ActionKey(mint: ref.read(idempotencyKeyMintProvider));

  /// The compressed image, kept between attempts.
  ///
  /// This is what makes [retry] a second *upload* rather than a second photograph. `Docs/04` §3.1
  /// requires the file be cleared once uploaded and says nothing about before, and a provider who
  /// lost their connection halfway through should not be asked to find their licence again.
  CapturedImage? _pending;

  @override
  DocumentCaptureState build() => const DocumentCaptureState();

  /// Compresses [bytes] and submits the result. `true` when the platform recorded it.
  Future<bool> submit(Uint8List bytes) async {
    if (state.isBusy) return false;

    state = const DocumentCaptureState(stage: DocumentCaptureStage.compressing);

    final CapturedImage image;
    try {
      image = await ref.read(captureCompressorProvider)(bytes);
    } on CapturedImageUnreadable {
      return _failed('That photograph could not be read. Take it again.');
    } catch (error) {
      return _failed('That photograph could not be prepared to send. Take it again.');
    }

    if (!ref.mounted) return false;
    _pending = image;

    return _send(image);
  }

  /// Sends the image already on the device again, after a failure.
  ///
  /// Returns `false` with nothing sent when there is none — which is the state after a compression
  /// failure, where there is nothing to retry and the screen offers the camera instead.
  Future<bool> retry() async {
    final image = _pending;
    if (image == null || state.isBusy) return false;
    return _send(image);
  }

  Future<bool> _send(CapturedImage image) async {
    state = const DocumentCaptureState(stage: DocumentCaptureStage.uploading);

    final body = <String, dynamic>{'kind': kind.wire, 'bytes': image.length};
    final key = _key.forRequest(body);

    try {
      await ref.read(verificationRepositoryProvider).submit(
            kind: kind,
            image: image,
            idempotencyKey: key,
          );
    } on ApiFailure catch (failure) {
      _key.settled(failure);
      return _failed(_copyFor(failure));
    } catch (error) {
      // A device condition rather than a platform one — a full disk, or a directory the platform
      // would not create. `ActionKey` is retired because nothing was sent, so the next attempt is a
      // new action rather than a replay of one the platform never saw.
      _key.settled(null);
      return _failed(
        'This phone would not store the photograph. Free up some space and take it again.',
      );
    }

    _key.settled(null);
    _pending = null;

    if (!ref.mounted) return true;
    state = const DocumentCaptureState(stage: DocumentCaptureStage.submitted);
    return true;
  }

  bool _failed(String message) {
    if (ref.mounted) {
      state = DocumentCaptureState(stage: DocumentCaptureStage.failed, message: message);
    }
    return false;
  }

  /// The words for a submission the platform would not take.
  ///
  /// **Every one of these is a refusal the contract names**, and the copy says what to do rather
  /// than what happened. `profiles_document_not_uploaded` in particular is a race the provider can
  /// simply lose — the platform asks the store what it holds before it writes anything, so a PUT
  /// that has not finished is a `409` and not a fault.
  String _copyFor(ApiFailure failure) => switch (failure) {
        ApiErrorResponse(code: 'profiles_document_not_uploaded') =>
          'The upload did not finish. Try sending it again.',
        ApiErrorResponse(code: 'profiles_document_rejected') =>
          'Shipper could not read that image. Photograph the document again, in better light if '
              'you can.',
        ApiErrorResponse(code: 'profiles_document_already_recorded') =>
          'That photograph has already been submitted. Take a new one if you need to replace it.',
        ApiErrorResponse(code: 'profiles_provider_only') =>
          'Only a transport provider account is verified to bid. This account publishes jobs.',
        ApiErrorResponse(code: 'validation_failed') =>
          'Shipper would not accept that image. Photograph the document again.',
        ApiUnreachable() =>
          'Shipper could not reach the network. The photograph is still on this phone — try again '
              'when you have a connection.',
        // Everything else, including a `500` and a response this build could not parse. The image
        // is on the device either way, which is the sentence that matters to somebody standing
        // there holding their licence.
        _ => 'That did not send. The photograph is still on this phone — try again.',
      };
}

/// Capturing one document, keyed by its kind.
final captureDocumentProvider = NotifierProvider.autoDispose.family<CaptureDocumentController,
    DocumentCaptureState, VerificationDocumentKind>(CaptureDocumentController.new);

/// What this provider has already submitted, and what is still outstanding.
///
/// A `FutureProvider` rather than a notifier because nothing on this screen mutates the list: a
/// submission is recorded by [CaptureDocumentController] and this is invalidated, which re-reads it
/// from the platform. **The platform's answer is the only one drawn** — a locally appended row would
/// be this client asserting something it has not been told, which is the failure mode `Docs/07` §4
/// spends its length on for the *other* direction.
final verificationDocumentsProvider = FutureProvider<List<VerificationDocument>>(
  (ref) => ref.watch(verificationRepositoryProvider).documents(),
);

/// How the idempotency key for a submission is minted.
///
/// Injected only so a test can name the key it asserts on — and, here, the **file name**, because
/// the stored image is named after the action that will send it.
final idempotencyKeyMintProvider = Provider<String Function()>((ref) => newIdempotencyKey);
