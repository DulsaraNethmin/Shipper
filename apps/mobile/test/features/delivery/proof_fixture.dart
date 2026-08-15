// The two things a proof-capture test cannot have on a host: a camera, and a photograph.
//
// **Every widget test that can reach `ProofCaptureScreen` must supply [FakeProofCamera]**, and the
// reason is sharper than "there is no device": under `testWidgets`' fake clock a platform-channel
// reply is never delivered at all, so `availableCameras()` does not throw `MissingPluginException`
// — it simply never completes, and the screen sits on its opening spinner until `pumpAndSettle`
// times out. The real camera is reachable from `integration_test/` and from nowhere else.

import 'dart:math';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:image/image.dart' as img;
import 'package:shipper/features/delivery/capture_proof_controller.dart';
import 'package:shipper/features/delivery/proof_camera.dart';
import 'package:shipper/features/delivery/proof_image.dart';

/// A camera that answers with bytes a test chose, or refuses the way a denied permission does.
class FakeProofCamera implements ProofCamera {
  FakeProofCamera({this.bytes, this.problem});

  /// What the shutter produces. Real JPEG bytes in every test that gets as far as pressing it.
  final Uint8List? bytes;

  /// Set to refuse [start], which is what a revoked camera permission looks like from Dart.
  final ProofCameraProblem? problem;

  var started = false;
  var stopped = false;
  var captures = 0;

  @override
  bool get isReady => started;

  @override
  Future<void> start() async {
    final refusal = problem;
    if (refusal != null) throw ProofCameraUnavailable(refusal, detail: 'fake');
    started = true;
  }

  @override
  Widget preview() => const SizedBox(key: Key('fake-camera-preview'), width: 100, height: 100);

  @override
  Future<Uint8List> capture() async {
    captures++;
    return bytes ?? Uint8List(0);
  }

  @override
  Future<void> stop() async {
    stopped = true;
    started = false;
  }
}

/// A photograph-shaped JPEG: noisy, so it does not compress to nothing the way a flat colour does.
///
/// The noise is the point. A 4000×3000 image of one colour encodes to a few kilobytes at any
/// quality, and every assertion about sizes and quality ladders would pass against a compressor that
/// did nothing at all.
Uint8List photograph({int width = 4000, int height = 3000, int seed = 7}) {
  final random = Random(seed);
  final image = img.Image(width: width, height: height);

  for (var y = 0; y < height; y++) {
    for (var x = 0; x < width; x++) {
      image.setPixelRgb(x, y, random.nextInt(256), random.nextInt(256), random.nextInt(256));
    }
  }

  return img.encodeJpg(image, quality: 100);
}

/// Just enough to keep a test off the platform camera, for one that is not about proof at all.
List<Override> withoutACamera() =>
    <Override>[proofCameraProvider.overrideWithValue(FakeProofCamera.new)];

/// The application with one fake device in it: a camera, a store over [root], and a named key.
///
/// The compressor is substituted for the **synchronous** one, which is the same code: the
/// application runs it through `Isolate.run` so a 12-megapixel decode does not freeze the shutter,
/// and a widget test that spawned an isolate per capture would be testing Dart's isolates.
List<Override> withDevice(
  FakeProofCamera camera,
  ProofStore store, {
  String key = 'a4f21c9e',
}) {
  return <Override>[
    proofCameraProvider.overrideWithValue(() => camera),
    proofStoreProvider.overrideWith((ref) async => store),
    proofCompressorProvider.overrideWithValue((bytes) async => compressProofSync(bytes)),
    idempotencyKeyMintProvider.overrideWithValue(() => key),
  ];
}
