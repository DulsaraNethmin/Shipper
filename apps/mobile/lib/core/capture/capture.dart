/// Photographing something, and keeping the result where only this application can read it
/// (`Docs/07` §2).
///
/// **Two callers, which is why this is `core/` and not a feature.** `Docs/07` §2 states the rule and
/// the mechanism together: *"Reuse across features is what `core/` is for, and moving is the
/// mechanism… A second caller is the signal; the move is the answer."*
///
/// | Caller | What it photographs | Its *Done when* |
/// |---|---|---|
/// | `features/delivery/` | the goods at the delivery point | SHIP-130 — `Docs/01` §4.4's photo proof |
/// | `features/profile/` | `Docs/04` §3's four verification documents | SHIP-81c — `Docs/04` §3.1's evidence capture |
///
/// The two requirements are the same three sentences written twice, in two documents, about two
/// different photographs:
///
/// - never written to the device photo library — `capture_camera.dart` and `CapturedImageStore`;
/// - compressed on the device before upload — `compressCapture`;
/// - cleared from app storage once uploaded — `CapturedImageStore.discard`, called by whoever
///   finished the upload.
///
/// **What is deliberately not here.** No upload. `core/sync/operation_sender.dart` performs the
/// delivery's three-request exchange and `features/profile/verification_repository.dart` performs
/// verification's, because the two endpoints differ in their path, their body and whether the work
/// belongs in the offline queue at all. What they share is the bytes, and the bytes are what this
/// folder owns.
library;

export 'package:shipper/core/capture/capture_camera.dart';
export 'package:shipper/core/capture/captured_image.dart';
