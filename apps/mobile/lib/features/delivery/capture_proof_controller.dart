import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/policy/app_policy_controller.dart';
import 'package:shipper/core/queue/operation_queue.dart';
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/core/sync/sync_worker.dart';
import 'package:shipper/features/delivery/milestone.dart';
import 'package:shipper/features/delivery/proof_exception_reason.dart';
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

  /// **A reason** is on the device, in the queue, and there will be no photograph (SHIP-131).
  ///
  /// Its own stage rather than a flag beside [queued], because the two are different things to
  /// confirm to a driver and the confirmation is the last thing they read before walking away. "Your
  /// photograph is saved" said about a recorded exception is a driver who believes they photographed
  /// a delivery they did not, and finds out weeks later in a dispute.
  ///
  /// It is **not** a failure and must never be drawn as one. `Docs/01` §4.4 makes the photograph and
  /// the reason the same feature — delivered requires one or the other, never neither — so a reason
  /// is evidence rather than the absence of it, and this delivery is as finished as any other.
  reasonRecorded,

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

  /// Records why this delivery carries no photograph, and queues it (SHIP-131).
  ///
  /// ## This is the other half of `Docs/01` §4.4 rather than a hole in it
  ///
  /// That paragraph makes photo proof mandatory and, **in the same paragraph**, makes the exception
  /// path part of the same feature — because what must never happen is a driver standing at a
  /// delivery point unable to finish the job. `Docs/07` §7 calls a camera flow that dead-ends on a
  /// denied permission a defect in as many words. So this is not a way around the rule; it is the
  /// rule's second branch, and the platform enforces exactly one of the two arriving:
  /// `000605`'s deferred constraint trigger refuses at `COMMIT` a `Delivered` milestone carrying
  /// neither, and `ck_proofs_evidence` refuses one carrying both.
  ///
  /// ## The request is the milestone, with the reason inside it
  ///
  /// `POST /v1/jobs/{id}/milestones` with `proof.exception_reason` and **no** `object_key` — the
  /// contract's "send exactly one of the two", and sending both is refused with `validation_failed`.
  /// Nothing is uploaded and the object store is not contacted, which is why this is
  /// `OperationKind.milestone` and not `OperationKind.proof`: there is no file, so there is no
  /// attachment, and the operation the sync worker has to perform is one request rather than three.
  ///
  /// It shares [Milestone.delivered] and the `job:<id>` ordering key with everything else on this
  /// delivery, so a driver's recorded sequence reaches the platform in the order they recorded it
  /// (SHIP-124) — and it appears in the delivery screen's own log as a pending `Delivered`, because
  /// that log reads `OperationKind.milestone` rows on this key.
  ///
  /// ## The key is the queue's, not this method's
  ///
  /// Unlike [queueFrom], no key is minted here: [queueFrom] mints one because the **file** is named
  /// after it, and there is no file. The queue mints one at enqueue and every attempt reuses it
  /// unchanged (SHIP-124, SHIP-125), which is what makes a retry after a day in a valley a retry
  /// rather than a second `Delivered` on the customer's timeline.
  ///
  /// ## [note] goes beside the selected reason and never instead of one (SHIP-131a)
  ///
  /// `proof.exception_reason` is a **selection from a closed list of three**, which is what makes it
  /// enforceable — `ck_proofs_exception_reason` pairs with it — and what makes `Docs/04` §5's
  /// delivery-exception queue triageable at all. A free-text box in its place would be a queue
  /// nobody can group.
  ///
  /// What a closed list cannot carry is *which* of the three it was and why, and "the recipient
  /// asked me not to photograph their door" is the sentence that turns a queue entry into a
  /// decision. So the note is `MilestoneRecording.reason`, a second field on the same request, and
  /// this method still refuses to queue anything without a selection. **Both go in one body**: the
  /// milestone, the reason, and the proof object beside each other.
  ///
  /// Empty means no key at all, for the reason `record_milestone_controller.dart` sets out on its
  /// own `record`: an absent field says nothing, and an empty one says something and says it blank.
  ///
  /// Returns once the row is **committed**, which is when the screen may confirm it.
  Future<bool> queueException(ProofExceptionReason reason, {String note = ''}) async {
    if (state.isBusy) return false;

    // `unknown` is this client's own value for a reason a later build wrote — see
    // `ProofExceptionReasonCopy.offered`, which is why the screen cannot offer it. Refused here as
    // well, because the platform's `enum` is exactly three and it would come back as
    // `validation_failed` naming `proof.exception_reason`: a driver told their reason is not a
    // reason, hours later, from a quarantined row.
    if (reason == ProofExceptionReason.unknown) return false;

    final at = DateTime.now();
    final words = note.trim();

    try {
      await ref.read(syncWorkerProvider).record(
            kind: OperationKind.milestone,
            orderingKey: 'job:$jobId',
            method: 'POST',
            path: '/v1/jobs/$jobId/milestones',
            body: <String, dynamic>{
              'milestone': milestone.wire,
              // The actor's clock with the device's offset on it, for the reason
              // `record_milestone_controller.dart` sets out at length: omitting it would have the
              // platform stamp a delivery that happened in a valley with the time it arrived.
              'recorded_at': rfc3339(at),
              if (words.isNotEmpty) 'reason': words,
              'proof': <String, dynamic>{'exception_reason': reason.wireName},
            },
            recordedAt: at,
          );
    } on QueueRefusal catch (refusal) {
      return _failed(_refusalCopy(refusal));
    }

    if (!ref.mounted) return true;
    state = const ProofCaptureState(stage: ProofCaptureStage.reasonRecorded);
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
///
/// **The size comes from the platform now (SHIP-167a), and the pixels do not.** `GET /v1/app/policy`
/// carries `proof_compression_budget_bytes`, which is the operational half — what a driver on a
/// metered connection in a yard should be asked to send, and a number `CLAUDE.md` says belongs
/// server-side. `longestEdge` and the quality ladder stay compiled in: 1600 pixels is a legibility
/// judgement about a licence plate photographed from two metres (`Docs/01` §4.4), not a dial
/// operations should be able to turn, and trading evidence for bytes should be a code change
/// somebody reviewed.
///
/// A device that has never been online uses [ProofImagePolicy]'s own default, which is the same
/// mebibyte — see `compiledProofCompressionBudgetBytes`, which a test holds against it.
final proofImagePolicyProvider = Provider<ProofImagePolicy>(
  (ref) => ProofImagePolicy(maxBytes: ref.watch(appPolicyProvider).proofCompressionBudgetBytes),
);

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
