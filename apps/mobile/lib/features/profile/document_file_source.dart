/// The file-upload fallback `Docs/04` §3.1 requires (SHIP-81d).
///
/// > *"The camera permission may be declined. A file-upload fallback must exist so that a refused
/// > permission never blocks verification outright."*
///
/// A provider who cannot photograph their licence cannot be verified, and a provider who cannot be
/// verified cannot bid — so this is not a convenience. It is the difference between a permission
/// prompt somebody declined and somebody locked out of the marketplace.
///
/// ## A document picker, and the distinction is the whole ticket
///
/// `test/nothing_captured_reaches_the_gallery_test.dart` bans `image_picker` by name and every
/// gallery package beside it, and it checks `pubspec.yaml` and the import lines **separately**,
/// because a dependency added without a pubspec entry once slipped past it. So the fallback cannot
/// be an image picker, and `pubspec.yaml` carries the argument for the package this depends on at
/// length: `file_selector`, `flutter/packages`, `UIDocumentPickerViewController` on iOS and
/// `ACTION_OPEN_DOCUMENT` on Android, needing **no** photo-library usage string and **no** media
/// permission — which is the concrete thing that test asserts is absent.
///
/// **What that does and does not buy.** The application holds no permission to read the photo
/// library, never enumerates it, and never writes to it. What the platform's own document UI can
/// browse is the platform's business: on Android the Storage Access Framework surfaces the media
/// provider among the others, so a provider may well hand over a photo they already have. That is
/// the point of a file-upload fallback — the document they need to attach is usually a photo or a
/// scan on their phone — rather than a hole in one. What `Docs/04` §3.1 forbids is this application
/// writing an identity document *into* a camera roll.
///
/// ## It is here rather than in `core/`
///
/// `Docs/07` §2's rule for `core/` is a second caller — *"a second caller is the signal; the move is
/// the answer"* — and this has one. It is also the one part of verification capture that
/// `features/delivery/` must **not** acquire: `Docs/01` §4.4 makes proof of delivery a photograph
/// taken at the delivery point, and a file chosen from a phone is not evidence that a delivery
/// happened. A refused camera there has its own answer, and it is a recorded exception reason
/// (SHIP-131).
///
/// ## Declared here, by the consumer
///
/// The interface says what a capture screen needs — bytes, or nothing — and not what `file_selector`
/// offers. A host test hands over a fake that returns real JPEG bytes, and the application is the
/// only thing that ever constructs the platform one.
library;

import 'dart:typed_data';

import 'package:file_selector/file_selector.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// One file a person chose, from somewhere this application did not go looking.
abstract interface class DocumentFileSource {
  /// Asks the platform for one file. `null` when the person backed out.
  ///
  /// **`null` rather than an exception, because cancelling is not an error.** A provider who opens
  /// the picker and changes their mind should come back to the screen they left, and a screen that
  /// showed them a failure would be telling them something went wrong when nothing did.
  ///
  /// Throws [DocumentFileUnavailable] when the platform would not open a picker at all.
  Future<Uint8List?> pick();
}

/// The picker could not be opened.
///
/// Distinct from a cancellation for the reason above, and rare: unlike a camera there is no
/// permission to refuse, so this is a platform channel that answered something unexpected rather
/// than a person saying no.
final class DocumentFileUnavailable implements Exception {
  const DocumentFileUnavailable({this.detail});

  /// Developer-facing. Safe for a log, never for a person.
  final String? detail;

  @override
  String toString() => 'DocumentFileUnavailable${detail == null ? '' : ': $detail'}';
}

/// The picker this application actually uses.
final class PlatformDocumentFileSource implements DocumentFileSource {
  const PlatformDocumentFileSource();

  /// What the picker offers to open.
  ///
  /// **Images only, and the list is the platform's rather than this client's taste.**
  /// `STORAGE_ACCEPTED_CONTENT_TYPES` is `image/jpeg`, `image/png` and `image/heic`, and everything
  /// that gets past this is re-encoded to JPEG by `compressCaptureSync` anyway. Offering a PDF would
  /// be offering something the compressor cannot decode and the store would not accept — a provider
  /// choosing one would get "that is not an image Shipper can read" after the fact, which is a
  /// refusal it is cheaper to make in the picker.
  ///
  /// The extensions are given beside the MIME types because Android's document UI matches on either
  /// depending on the provider, and a file whose provider reports no type would otherwise be greyed
  /// out.
  static const _images = XTypeGroup(
    label: 'Images',
    mimeTypes: <String>['image/jpeg', 'image/png', 'image/heic', 'image/heif'],
    extensions: <String>['jpg', 'jpeg', 'png', 'heic', 'heif'],
    uniformTypeIdentifiers: <String>['public.jpeg', 'public.png', 'public.heic'],
  );

  @override
  Future<Uint8List?> pick() async {
    final XFile? file;
    try {
      file = await openFile(acceptedTypeGroups: const <XTypeGroup>[_images]);
    } catch (e) {
      // A `MissingPluginException` on a build where the plugin did not register, or a
      // `PlatformException` from a channel that answered something unexpected. Caught deliberately
      // rather than left to propagate: an unhandled error here leaves the screen mid-tap, and this
      // *is* the fallback — there is nothing behind it to fall back to.
      throw DocumentFileUnavailable(detail: e.runtimeType.toString());
    }

    if (file == null) return null;

    try {
      return await file.readAsBytes();
    } catch (e) {
      throw DocumentFileUnavailable(detail: e.runtimeType.toString());
    }
  }
}

/// The picker the capture screen opens.
final documentFileSourceProvider = Provider<DocumentFileSource>(
  (ref) => const PlatformDocumentFileSource(),
);
