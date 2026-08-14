/// The camera, as much of it as proof of delivery needs (SHIP-130).
///
/// ## `camera` rather than `image_picker`, and the *Done when* is the whole reason
///
/// The obvious call is `ImagePicker().pickImage(source: ImageSource.camera)`. It is one line, needs
/// no preview, and **it is the wrong answer to this ticket.** On Android it hands the job to
/// whichever camera application the manufacturer shipped, through an `ACTION_IMAGE_CAPTURE` intent.
/// Several of those write a copy of every photograph they take into `DCIM/Camera` regardless of the
/// output the caller asked for, and there is nothing this application can do about it and nothing in
/// Dart that can even observe it. "Never written to the photo library" would then be true on iOS,
/// false on some Android handsets, and untestable on both.
///
/// `camera` is the opposite trade. The application owns the capture surface — CameraX on Android,
/// AVFoundation on iOS — and `takePicture()` writes into this app's own temporary directory. Nothing
/// in the path touches `MediaStore` or `PHPhotoLibrary`, so the property is structural rather than
/// hopeful. It is also the better screen for the job: one large shutter for a gloved thumb, instead
/// of an OEM camera application with its own filters, timers and share sheet.
///
/// **The cost is a real one and is recorded rather than waved through.** It is a second native
/// integration; it is CameraX on Android (minSdk 21, below this app's floor of 24, so the floor
/// does not move); and the official plugin's README asks for `NSMicrophoneUsageDescription` beside
/// `NSCameraUsageDescription` on iOS. This build passes `enableAudio: false`, so no microphone is
/// ever opened and no such string is declared — a purpose string for a microphone the app does not
/// use is worse than its absence, both to a reviewer and to the person reading the prompt.
/// `Docs/11` §3 carries that as the one store-submission risk this ticket takes.
///
/// ## The raw capture is read and deleted, and only the compressed image becomes a file
///
/// `takePicture()` returns an `XFile` on disk. [ProofCamera.capture] reads it, deletes it, and hands
/// back bytes — so the full-resolution original never survives the call, and the only proof file
/// this application keeps is the compressed one `ProofStore` wrote. That is data economy
/// (`Docs/01` §5.2) and it is one less place for a photograph of somebody's doorstep to sit.
///
/// ## Declared here, by the consumer
///
/// The same rule `operation_sender.dart` follows: the interface says what proof capture needs, not
/// what `camera` offers, so a test drives the whole screen with a fake that returns real JPEG bytes
/// and the application is the only thing that ever constructs the platform one.
library;

import 'dart:io';
import 'dart:typed_data';

import 'package:camera/camera.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// One still photograph, from a camera this application drives itself.
abstract interface class ProofCamera {
  /// Opens the camera. Throws [ProofCameraUnavailable] when it cannot be opened.
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
/// Two cases rather than one because the driver can do something about only one of them, and
/// SHIP-131 is the ticket that turns both into a route through the job rather than a dead end.
enum ProofCameraProblem {
  /// The permission was refused, or has been revoked in settings.
  refused,

  /// There is no camera, or the platform would not give this application one.
  unavailable,
}

/// The camera cannot be opened.
final class ProofCameraUnavailable implements Exception {
  const ProofCameraUnavailable(this.problem, {this.detail});

  final ProofCameraProblem problem;

  /// Developer-facing. Safe for a log, never for a person — the words a driver reads are in
  /// `core/permissions/permission_copy.dart` (SHIP-179).
  final String? detail;

  @override
  String toString() => 'ProofCameraUnavailable: ${problem.name}${detail == null ? '' : ' ($detail)'}';
}

/// The camera this application actually uses.
final class PlatformProofCamera implements ProofCamera {
  PlatformProofCamera();

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
      throw ProofCameraUnavailable(_problemFrom(e), detail: e.code);
    } catch (e) {
      // Anything else the platform side can raise — a `MissingPluginException` on a build where the
      // plugin did not register, a `PlatformException` from a channel that answered something
      // unexpected. **Caught deliberately rather than left to propagate**: an unhandled error here
      // leaves the capture screen showing a spinner for ever, which is the one outcome worse than
      // saying the camera is unavailable. `Docs/01` §4.4's exception path is what it is for.
      throw ProofCameraUnavailable(ProofCameraProblem.unavailable, detail: e.runtimeType.toString());
    }

    if (cameras.isEmpty) {
      throw const ProofCameraUnavailable(ProofCameraProblem.unavailable, detail: 'no cameras');
    }

    // The rear camera, falling back to whatever is first. A driver photographing a pallet is not
    // using the selfie camera, and on a handset with only a front one the photograph is still
    // better than no photograph — `Docs/01` §4.4's alternative is the delivery not completing.
    final chosen = cameras.firstWhere(
      (camera) => camera.lensDirection == CameraLensDirection.back,
      orElse: () => cameras.first,
    );

    final controller = CameraController(
      chosen,
      // 1080p rather than `max`. The stored image is downscaled to `ProofImagePolicy.longestEdge`
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
      throw ProofCameraUnavailable(_problemFrom(e), detail: e.code);
    } catch (e) {
      await controller.dispose();
      throw ProofCameraUnavailable(ProofCameraProblem.unavailable, detail: e.runtimeType.toString());
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
      throw const ProofCameraUnavailable(ProofCameraProblem.unavailable, detail: 'not started');
    }

    final XFile shot;
    try {
      shot = await controller.takePicture();
    } on CameraException catch (e) {
      throw ProofCameraUnavailable(_problemFrom(e), detail: e.code);
    } catch (e) {
      throw ProofCameraUnavailable(ProofCameraProblem.unavailable, detail: e.runtimeType.toString());
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

  /// A refused permission and a broken camera are different answers to the driver.
  ///
  /// The plugin reports the first as `CameraAccessDenied` on both platforms, and as
  /// `CameraAccessDeniedWithoutPrompt` or `CameraAccessRestricted` when settings is the only way
  /// back. Anything else is the second.
  static ProofCameraProblem _problemFrom(CameraException e) {
    return switch (e.code) {
      'CameraAccessDenied' ||
      'CameraAccessDeniedWithoutPrompt' ||
      'CameraAccessRestricted' ||
      'AccessDenied' =>
        ProofCameraProblem.refused,
      _ => ProofCameraProblem.unavailable,
    };
  }
}

/// The camera the capture screen opens.
///
/// A factory rather than an instance, because a camera is a device resource one screen holds and
/// releases: a provider handing out a shared, already-started controller would leave it open behind
/// every screen the driver navigates to afterwards.
final proofCameraProvider = Provider<ProofCamera Function()>((ref) => PlatformProofCamera.new);
