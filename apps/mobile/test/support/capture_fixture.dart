// The two things a capture test cannot have on a host: a camera, and a photograph.
//
// **Every widget test that can reach a capture screen must supply [FakeCaptureCamera]**, and the
// reason is sharper than "there is no device": under `testWidgets`' fake clock a platform-channel
// reply is never delivered at all, so `availableCameras()` does not throw `MissingPluginException`
// — it simply never completes, and the screen sits on its opening spinner until `pumpAndSettle`
// times out. The real camera is reachable from `integration_test/` and from nowhere else.
//
// It is in `test/support/` rather than in either feature's folder because SHIP-81c gave the camera a
// second caller. `architecture_test.dart` holds `lib/features` to the no-cross-feature-import rule
// and deliberately does not scan `test/`, so a harness may cross — but two copies of a fake camera
// drifting apart is exactly the shape of bug that makes one feature's tests quietly weaker than the
// other's.

import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/core/capture/captured_image.dart';

/// A camera that answers with bytes a test chose, or refuses the way a denied permission does.
class FakeCaptureCamera implements CaptureCamera {
  FakeCaptureCamera({this.bytes, this.problem});

  /// What the shutter produces. Real JPEG bytes in every test that gets as far as pressing it.
  final Uint8List? bytes;

  /// Set to refuse [start], which is what a revoked camera permission looks like from Dart.
  final CameraProblem? problem;

  var started = false;
  var stopped = false;
  var captures = 0;

  @override
  bool get isReady => started;

  @override
  Future<void> start() async {
    final refusal = problem;
    if (refusal != null) throw CameraUnavailable(refusal, detail: 'fake');
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

/// A store over a fresh temporary directory, removed when the test ends.
///
/// [folder] is required for the same reason `CapturedImageStore`'s constructor requires it: a test
/// that wrote into whichever directory the store happened to default to would be the test that
/// stopped noticing SHIP-81c's whole change.
CapturedImageStore temporaryStore(CaptureFolder folder) {
  final root = Directory.systemTemp.createTempSync('shipper_capture_${folder.folder}');
  addTearDown(() {
    if (root.existsSync()) root.deleteSync(recursive: true);
  });
  return CapturedImageStore(root, folder: folder);
}
