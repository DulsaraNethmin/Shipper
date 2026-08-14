import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/sync_worker.dart';
import 'package:shipper/features/delivery/milestone.dart';
import 'package:shipper/features/delivery/proof_image.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// Where one capture has got to.
enum ProofCaptureStage {
  /// Nothing has been taken yet.
  waiting,

  /// The photograph is being made small enough to send.
  ///
  /// Its own stage because it is the one step with a delay a person notices: a full-resolution
  /// decode and re-encode is hundreds of milliseconds, and a shutter that appears to do nothing is
  /// a shutter that gets pressed twice.
  compressing,

  /// On the device, in the queue, waiting for a connection.
  queued,

  /// It could not be stored. [ProofCaptureState.message] says what a person can do about it.
  failed,
}

/// What the capture screen draws.
@immutable
final class ProofCaptureState {
  const ProofCaptureState({this.stage = ProofCaptureStage.waiting, this.message});

  final ProofCaptureStage stage;

  /// Copy for a person, or `null`. Written here rather than carried out of `core/` for the reason
  /// `record_milestone_controller.dart` gives: the words live in the layer that draws them.
  final String? message;

  bool get isBusy => stage == ProofCaptureStage.compressing;

  @override
  bool operator ==(Object other) =>
      other is ProofCaptureState && other.stage == stage && other.message == message;

  @override
  int get hashCode => Object.hash(stage, message);
}

/// Compresses one photograph, stores it where only this app can read it, and queues it (SHIP-130).
///
/// ## The three clauses of the *Done when*, and where each one is
///
/// **Captured** is `ProofCamera`, which the screen drives — this class never sees a camera and takes
/// bytes, which is what lets the whole journey be tested with real JPEG bytes and no device.
///
/// **Compressed** is `compressProof`, on another isolate, against `ProofImagePolicy`.
///
/// **Queued** is [SyncWorker.record] with `OperationKind.proof` and the stored file's path in
/// `attachmentPath` — the column SHIP-124 wrote for this and left empty, with a comment naming this
/// ticket. `Docs/07` §4 is explicit that the image "uploads on reconnection, **not** as part of the
/// milestone request", so the row is queued and the exchange that sends it is
/// `core/sync/operation_sender.dart`.
///
/// **Never written to the photo library** is `ProofStore`, and it is the one clause that is a
/// property of the whole application rather than of a method. See that class.
///
/// ## The operation's key names the file, which is not a flourish
///
/// The idempotency key is minted here, at the moment the driver pressed the shutter, and the
/// compressed image is written as `<key>.jpg`. The key is `UNIQUE` in SHIP-124's table, so two
/// captures cannot collide; and a file left behind by an operation that was cleared at sign-out is
/// traceable to the row that owned it rather than being an anonymous photograph in a directory.
///
/// ## What it does **not** do
///
/// It records no status and decides no transition, exactly as `RecordMilestoneController` does not:
/// `Docs/02` §3.1 puts every transition on the platform. What it queues is a claim that a delivery
/// happened and a photograph of it — `POST /v1/jobs/{id}/milestones` with `proof`, which the
/// platform refuses without one (SHIP-118) and which is why this ticket is what finally makes
/// `delivered` recordable from this application at all.
class CaptureProofController extends Notifier<ProofCaptureState> {
  CaptureProofController(this.jobId);

  /// The job being delivered. From the route, which is the only place it comes from.
  final String jobId;

  /// The milestone this photograph is evidence for.
  ///
  /// `delivered` and nothing else today, because it is the only milestone the platform *requires*
  /// evidence for (`Docs/01` §4.4, SHIP-118) and therefore the only one this application cannot
  /// record without a camera. `POST /v1/jobs/{id}/milestones` accepts `proof` on any of the five, so
  /// widening this is a screen and a parameter rather than a design.
  static const milestone = Milestone.delivered;

  @override
  ProofCaptureState build() {
    ref.watch(syncWorkerProvider);
    return const ProofCaptureState();
  }

  /// Compresses [bytes], stores the result, and queues it. `true` when the row is committed.
  ///
  /// Returns once the row is **committed**, which is when the screen may confirm it: `Docs/07` §4
  /// has the user record what happened and move on, and the confirmation is immediate and marked
  /// pending rather than waiting on a network the driver may not have.
  Future<bool> queueFrom(Uint8List bytes) async {
    if (state.isBusy) return false;

    state = const ProofCaptureState(stage: ProofCaptureStage.compressing);

    final at = DateTime.now();
    final key = ref.read(idempotencyKeyMintProvider)();

    final ProofImage image;
    try {
      image = await ref.read(proofCompressorProvider)(bytes);
    } on ProofImageUnreadable {
      return _failed(
        'That photograph could not be read. Take it again.',
      );
    } catch (error) {
      return _failed('That photograph could not be prepared to send. Take it again.');
    }

    if (!ref.mounted) return false;

    final String path;
    try {
      final store = await ref.read(proofStoreProvider.future);
      final file = await store.write(image, name: '$key.$proofFileExtension');
      path = file.path;
    } catch (error) {
      // A full disk, or a directory the platform would not create. Both are conditions of this
      // handset rather than of the delivery, and both are worth saying plainly.
      return _failed(
        'This phone would not store the photograph. Free up some space and take it again.',
      );
    }

    if (!ref.mounted) return false;

    try {
      await ref.read(syncWorkerProvider).record(
            kind: OperationKind.proof,
            // The same key as this job's milestones, so a driver's recorded sequence for one
            // delivery reaches the platform in the order they recorded it (SHIP-124).
            orderingKey: 'job:$jobId',
            method: 'POST',
            path: '/v1/jobs/$jobId/milestones',
            body: <String, dynamic>{
              'milestone': milestone.wire,
              // The actor's clock with the device's offset on it, for the reason
              // `record_milestone_controller.dart` sets out at length: omitting it would have the
              // platform stamp a delivery that happened in a valley with the time it arrived.
              'recorded_at': rfc3339(at),
            },
            recordedAt: at,
            attachmentPath: path,
            idempotencyKey: key,
          );
    } on QueueRefusal catch (refusal) {
      // The row did not commit, so the file it would have pointed at is an orphan. Removing it here
      // is the only chance anything has: nothing else knows it exists.
      await _discard(path);
      return _failed(_refusalCopy(refusal));
    }

    if (!ref.mounted) return true;
    state = const ProofCaptureState(stage: ProofCaptureStage.queued);
    return true;
  }

  Future<void> _discard(String path) async {
    try {
      final store = await ref.read(proofStoreProvider.future);
      await store.discard(path);
    } catch (_) {
      // Best effort. A leftover file in this application's own directory is a wasted megabyte, and
      // failing the capture over it would be reporting a problem the driver cannot act on.
    }
  }

  bool _failed(String message) {
    if (ref.mounted) {
      state = ProofCaptureState(stage: ProofCaptureStage.failed, message: message);
    }
    return false;
  }

  /// The words for a device that would not queue the operation.
  ///
  /// Exhaustive over a **sealed** hierarchy, so a fourth refusal added to the queue fails to compile
  /// here rather than reaching a driver as a blank message.
  String _refusalCopy(QueueRefusal refusal) => switch (refusal) {
        QueueAtCapacity() => 'This device is holding as many unsent updates as it can. Find signal '
            'so the ones already recorded can be sent, then take the photograph again.',
        QueueOperationTooLarge() => 'That update is too large to store on this device.',
        QueueDuplicateOperation() => 'That photograph is already waiting to be sent.',
      };
}

/// Capturing proof for one job, keyed by the job's id.
final captureProofProvider = NotifierProvider.autoDispose
    .family<CaptureProofController, ProofCaptureState, String>(CaptureProofController.new);

/// How a proof photograph is made small enough to send.
///
/// A seam rather than a direct call for one reason: the application runs the compression on another
/// isolate, and a host test that spawned one for every capture would be testing Dart's isolates. The
/// compressor a test substitutes is still `compressProofSync` — the real one — so what is replaced
/// is where it runs and not what it does.
typedef ProofCompressor = Future<ProofImage> Function(Uint8List bytes);

/// See [ProofCompressor].
final proofCompressorProvider = Provider<ProofCompressor>((ref) {
  final policy = ref.watch(proofImagePolicyProvider);
  return (bytes) => compressProof(bytes, policy: policy);
});

/// The compression budget this build uses. See [ProofImagePolicy].
final proofImagePolicyProvider = Provider<ProofImagePolicy>((ref) => const ProofImagePolicy());

/// Where compressed proof photographs are kept.
///
/// A `FutureProvider` because resolving the application's private directory is a platform call. A
/// host test overrides it with a store over a temporary directory, the same way `queueDatabaseProvider`
/// is overridden rather than reaching a real application-support directory.
final proofStoreProvider = FutureProvider<ProofStore>((ref) => ProofStore.open());

/// How the idempotency key for a queued proof is minted.
///
/// Injected only so a test can name the key it asserts on — and, here, the **file name**, because
/// the stored image is named after the operation that will send it.
final idempotencyKeyMintProvider = Provider<String Function()>((ref) => newIdempotencyKey);
