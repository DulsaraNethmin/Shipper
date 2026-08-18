// Delivery's half of the capture wiring: the providers `capture_proof_controller.dart` declares,
// overridden for a host test.
//
// **The camera fake and the JPEG generator are no longer here.** SHIP-81c gave both a second caller
// in `features/profile/`, so they moved to `test/support/capture_fixture.dart` — the same answer
// `Docs/07` §2 gives for `lib/`, applied to the test tree for the same reason. What is left is what
// is genuinely delivery's: which folder the store writes into, and which key the operation is
// minted with.

import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/core/capture/capture_providers.dart';
import 'package:shipper/core/capture/captured_image.dart';
import 'package:shipper/features/delivery/capture_proof_controller.dart';

import '../../support/capture_fixture.dart';

export '../../support/capture_fixture.dart';

/// Just enough to keep a test off the platform camera, for one that is not about proof at all.
List<Override> withoutACamera() =>
    <Override>[captureCameraProvider.overrideWithValue(FakeCaptureCamera.new)];

/// The application with one fake device in it: a camera, a [store], and a named key.
///
/// The compressor is substituted for the **synchronous** one, which is the same code: the
/// application runs it through `Isolate.run` so a 12-megapixel decode does not freeze the shutter,
/// and a widget test that spawned an isolate per capture would be testing Dart's isolates.
List<Override> withDevice(
  FakeCaptureCamera camera,
  CapturedImageStore store, {
  String key = 'a4f21c9e',
}) {
  return <Override>[
    captureCameraProvider.overrideWithValue(() => camera),
    proofStoreProvider.overrideWith((ref) async => store),
    captureCompressorProvider.overrideWithValue((bytes) async => compressCaptureSync(bytes)),
    idempotencyKeyMintProvider.overrideWithValue(() => key),
  ];
}
