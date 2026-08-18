/// The wiring both capture callers share (SHIP-130, SHIP-167a, SHIP-81c).
///
/// Two providers, and the reason they are here rather than in either feature is the reason
/// `Docs/07` §2 gives for `core/` at all: there are two callers, and features do not import one
/// another. A second copy of [capturedImagePolicyProvider] in `features/profile/` would be a second
/// answer to a number the platform publishes once.
///
/// **The store provider is deliberately not here.** Each caller writes to its own
/// [CaptureFolder] — a delivery photograph to `proof/`, a verification document to `verification/` —
/// so the store is one line in each feature and there is nothing to share. What is shared is the
/// budget and the compressor, which are the same question for both.
library;

import 'dart:typed_data';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/capture/captured_image.dart';
import 'package:shipper/core/policy/app_policy_controller.dart';

/// How a captured photograph is made small enough to send.
///
/// A seam rather than a direct call for one reason: the application runs the compression on another
/// isolate, and a host test that spawned one for every capture would be testing Dart's isolates. The
/// compressor a test substitutes is still [compressCaptureSync] — the real one — so what is replaced
/// is where it runs and not what it does.
typedef CaptureCompressor = Future<CapturedImage> Function(Uint8List bytes);

/// See [CaptureCompressor].
final captureCompressorProvider = Provider<CaptureCompressor>((ref) {
  final policy = ref.watch(capturedImagePolicyProvider);
  return (bytes) => compressCapture(bytes, policy: policy);
});

/// The compression budget this build uses. See [CapturedImagePolicy].
///
/// **The size comes from the platform (SHIP-167a), and the pixels do not.** `GET /v1/app/policy`
/// carries `proof_compression_budget_bytes`, which is the operational half — what somebody on a
/// metered connection should be asked to send, and a number `CLAUDE.md` says belongs server-side.
/// `longestEdge` and the quality ladder stay compiled in: 1600 pixels is a legibility judgement
/// about a licence plate photographed from two metres, and about an expiry date printed on an
/// insurance certificate, not a dial operations should be able to turn — trading evidence for bytes
/// should be a code change somebody reviewed.
///
/// **One budget for both callers, and the wire field's name is the platform's rather than a claim
/// about scope.** `proof_compression_budget_bytes` was named when proof of delivery was the only
/// image this application sent; SHIP-81c gave it a second, and a `VERIFICATION_*` twin would be a
/// second answer to "how many bytes should this handset be asked to upload" that nothing keeps in
/// step. `internal/profiles` made the same call on the platform side and for the same reason — it
/// reads `delivery.UploadPolicy`'s four numbers rather than adding four of its own.
///
/// A device that has never been online uses [CapturedImagePolicy]'s own default, which is the same
/// mebibyte — see `compiledProofCompressionBudgetBytes`, which a test holds against it.
final capturedImagePolicyProvider = Provider<CapturedImagePolicy>(
  (ref) => CapturedImagePolicy(maxBytes: ref.watch(appPolicyProvider).proofCompressionBudgetBytes),
);
