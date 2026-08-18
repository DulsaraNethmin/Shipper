/// The camera, as much of it as this application photographs anything with (SHIP-130, SHIP-81c).
///
/// ## The copy generalised in the move, deliberately
///
/// This was `ProofCamera`, in `features/delivery/`, and every sentence in it was about proof of
/// delivery. SHIP-81c gave it a second caller — a provider photographing their licence, vehicle
/// registration, insurance certificate and ABN evidence (`Docs/04` §3) — and `Docs/07` §2 forbids
/// one feature importing another, so it moved to `core/` on the precedent `ProviderOnly` set at
/// SHIP-100. **A class that kept a doc comment about proof of delivery while a provider's licence
/// went through it would be documentation that is wrong about the code**, which is worse than
/// documentation that is missing, and the reasoning below is true of both callers.
///
/// It is `core/capture/` rather than `core/proof/` for the same reason: **the directory is part of
/// the name.** SHIP-81c's *Done when* says so in as many words, and a person reading an import list
/// learns what a file is from its path before they read a line of it.
///
/// ## `camera` rather than `image_picker`, and it is the whole reason this file exists
///
/// The obvious call is `ImagePicker().pickImage(source: ImageSource.camera)`. It is one line, needs
/// no preview, and **it is the wrong answer.** On Android it hands the job to whichever camera
/// application the manufacturer shipped, through an `ACTION_IMAGE_CAPTURE` intent. Several of those
/// write a copy of every photograph they take into `DCIM/Camera` regardless of the output the caller
/// asked for, and there is nothing this application can do about it and nothing in Dart that can
/// even observe it.
///
/// That is a *Done when* on both sides of the move. SHIP-130: a proof photograph is never written to
/// the photo library. `Docs/04` §3.1: *"Verification images must **not** be written to the device
/// photo library"* — and it says why, which is the sharper of the two reasons. A photograph of a
/// doorstep in somebody's camera roll is untidy; **a photograph of their driver licence in it is an
/// identity document the platform can neither control nor revoke.** Under `image_picker` the
/// property would be true on iOS, false on some Android handsets, and untestable on both.
///
/// `camera` is the opposite trade. The application owns the capture surface — CameraX on Android,
/// AVFoundation on iOS — and `takePicture()` writes into this app's own temporary directory. Nothing
/// in the path touches `MediaStore` or `PHPhotoLibrary`, so the property is structural rather than
/// hopeful. It is also the better screen for both jobs: one large shutter, instead of an OEM camera
/// application with its own filters, timers and share sheet.
///
/// **The cost is a real one and is recorded rather than waved through.** It is a second native
/// integration; it is CameraX on Android (minSdk 21, below this app's floor of 24, so the floor
/// does not move); and the official plugin's README asks for `NSMicrophoneUsageDescription` beside
/// `NSCameraUsageDescription` on iOS. This build passes `enableAudio: false`, so no microphone is
/// ever opened and no such string is declared — a purpose string for a microphone the app does not
/// use is worse than its absence, both to a reviewer and to the person reading the prompt.
/// `Docs/11` §3 carries that as the one store-submission risk SHIP-130 took, and SHIP-81c does not
/// change it: the same line still holds, from a file in a different folder.
///
/// ## The raw capture is read and deleted, and only the compressed image becomes a file
///
/// `takePicture()` returns an `XFile` on disk. [CaptureCamera.capture] reads it, deletes it, and
/// hands back bytes — so the full-resolution original never survives the call, and the only file
/// this application keeps is the compressed one [CapturedImageStore] wrote. That is data economy
/// (`Docs/01` §5.2) and it is one less place for somebody's front door, or their licence number, to
/// sit.
///
/// ## Declared here, by the consumer
///
/// The same rule `operation_sender.dart` follows: the interface says what a capture screen needs,
/// not what `camera` offers, so a test drives the whole screen with a fake that returns real JPEG
/// bytes and the application is the only thing that ever constructs the platform one.
library;

import 'dart:io';
import 'dart:typed_data';

import 'package:camera/camera.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// One still photograph, from a camera this application drives itself.
abstract interface class CaptureCamera {
  /// Opens the camera. Throws [CameraUnavailable] when it cannot be opened.
  Future<void> start();

  /// Whether [preview] and [capture] may be called.
  bool get isReady;

  /// The live view, for the capture screen to draw.
  Widget preview();

  /// Takes one photograph and returns its bytes, leaving no file behind.
  Future<Uint8List> capture();

  /// Releases the camera. Safe to call when it was never opened.
  Future<void> stop();
}

/// Why the camera could not be opened.
///
/// Two cases rather than one because the person can do something about only one of them, and
/// because what "something" is differs by caller: SHIP-131 turns a refusal into a recorded exception
/// reason on a delivery, and SHIP-81d turns the same refusal into a file the provider already has.
/// `Docs/04` §3.1 requires the second in as many words — *"the camera permission may be declined. A
/// file-upload fallback must exist so that a refused permission never blocks verification
/// outright."*
enum CameraProblem {
  /// The permission was refused, or has been revoked in settings.
  refused,

  /// There is no camera, or the platform would not give this application one.
  unavailable,
}

/// The camera cannot be opened.
final class CameraUnavailable implements Exception {
  const CameraUnavailable(this.problem, {this.detail});

  final CameraProblem problem;

  /// Developer-facing. Safe for a log, never for a person — the words anybody reads are in
  /// `core/permissions/permission_copy.dart` (SHIP-179).
  final String? detail;

  @override
  String toString() => 'CameraUnavailable: ${problem.name}${detail == null ? '' : ' ($detail)'}';
}

/// The camera this application actually uses.
final class PlatformCaptureCamera implements CaptureCamera {
  PlatformCaptureCamera();

  CameraController? _controller;

  @override
  bool get isReady => _controller?.value.isInitialized ?? false;

  @override
  Future<void> start() async {
    if (isReady) return;

    final List<CameraDescription> cameras;
    try {
      cameras = await availableCameras();
    } on CameraException catch (e) {
      throw CameraUnavailable(_problemFrom(e), detail: e.code);
    } catch (e) {
      // Anything else the platform side can raise — a `MissingPluginException` on a build where the
      // plugin did not register, a `PlatformException` from a channel that answered something
      // unexpected. **Caught deliberately rather than left to propagate**: an unhandled error here
      // leaves the capture screen showing a spinner for ever, which is the one outcome worse than
      // saying the camera is unavailable. Both callers have a route on from that answer and neither
      // has one from a spinner.
      throw CameraUnavailable(CameraProblem.unavailable, detail: e.runtimeType.toString());
    }

    if (cameras.isEmpty) {
      throw const CameraUnavailable(CameraProblem.unavailable, detail: 'no cameras');
    }

    // The rear camera, falling back to whatever is first. Nobody photographs a pallet or a licence
    // with the selfie camera, and on a handset with only a front one the photograph is still better
    // than no photograph — the alternative is a delivery that does not complete or a verification
    // that does not start.
    final chosen = cameras.firstWhere(
      (camera) => camera.lensDirection == CameraLensDirection.back,
      orElse: () => cameras.first,
    );

    final controller = CameraController(
      chosen,
      // 1080p rather than `max`. The stored image is downscaled to `CapturedImagePolicy.longestEdge`
      // anyway, so a 48-megapixel capture would buy nothing and cost a second of decoding on the
      // handset that can least afford it.
      ResolutionPreset.veryHigh,
      // **No microphone, ever.** This is the line that keeps `NSMicrophoneUsageDescription` and
      // Android's `RECORD_AUDIO` out of the build — see the note on this library.
      enableAudio: false,
      imageFormatGroup: ImageFormatGroup.jpeg,
    );

    try {
      await controller.initialize();
    } on CameraException catch (e) {
      await controller.dispose();
      throw CameraUnavailable(_problemFrom(e), detail: e.code);
    } catch (e) {
      await controller.dispose();
      throw CameraUnavailable(CameraProblem.unavailable, detail: e.runtimeType.toString());
    }

    _controller = controller;
  }

  @override
  Widget preview() {
    final controller = _controller;
    if (controller == null || !controller.value.isInitialized) return const SizedBox.shrink();
    return CameraPreview(controller);
  }

  @override
  Future<Uint8List> capture() async {
    final controller = _controller;
    if (controller == null || !controller.value.isInitialized) {
      throw const CameraUnavailable(CameraProblem.unavailable, detail: 'not started');
    }

    final XFile shot;
    try {
      shot = await controller.takePicture();
    } on CameraException catch (e) {
      throw CameraUnavailable(_problemFrom(e), detail: e.code);
    } catch (e) {
      throw CameraUnavailable(CameraProblem.unavailable, detail: e.runtimeType.toString());
    }

    final bytes = await shot.readAsBytes();

    // The full-resolution original does not outlive this call. See the note on this library.
    try {
      await File(shot.path).delete();
    } on FileSystemException {
      // It was in a temporary directory the platform owns and will clear. Failing the capture over
      // a file that could not be removed would lose the photograph to protect the disk.
    }

    return bytes;
  }

  @override
  Future<void> stop() async {
    final controller = _controller;
    _controller = null;
    await controller?.dispose();
  }

  /// A refused permission and a broken camera are different answers to the person holding the phone.
  ///
  /// The plugin reports the first as `CameraAccessDenied` on both platforms, and as
  /// `CameraAccessDeniedWithoutPrompt` or `CameraAccessRestricted` when settings is the only way
  /// back. Anything else is the second.
  static CameraProblem _problemFrom(CameraException e) {
    return switch (e.code) {
      'CameraAccessDenied' ||
      'CameraAccessDeniedWithoutPrompt' ||
      'CameraAccessRestricted' ||
      'AccessDenied' =>
        CameraProblem.refused,
      _ => CameraProblem.unavailable,
    };
  }
}

/// The camera a capture screen opens.
///
/// A factory rather than an instance, because a camera is a device resource one screen holds and
/// releases: a provider handing out a shared, already-started controller would leave it open behind
/// every screen the user navigates to afterwards.
final captureCameraProvider = Provider<CaptureCamera Function()>((ref) => PlatformCaptureCamera.new);
