/// Compressing a proof photograph, and putting it somewhere only this app can read (SHIP-130).
///
/// Two jobs, in one file because they are one decision: `Docs/01` §5.2 requires the image to be
/// compressed on the device before upload, and the *Done when* requires that it is **never written
/// to the photo library**. The second is a statement about where the bytes go, so the compressor
/// and the writer are not separable — a compressor that returned bytes and let each caller choose a
/// destination is a compressor with a gallery one refactor away.
///
/// ## The compression, and what the numbers are for
///
/// `Docs/01` §5.2: *"Assume metered mobile data. Compress images on the device before upload."* And
/// `Docs/01` §4.4 makes the photograph the evidence a delivery happened, so it has to stay legible —
/// a licence plate, a doorstep, a pallet's label. Both at once is what [ProofImagePolicy] is:
/// downscale the longest edge to something a screen renders, then step down the JPEG quality until
/// the file is inside a budget, and stop.
///
/// **JPEG, always, whatever came in.** The platform accepts `image/jpeg`, `image/png` and
/// `image/heic` (`STORAGE_ACCEPTED_CONTENT_TYPES`), and this build sends the first of the three for
/// two reasons: it is the only one of the three that is certainly in the accepted list on every
/// deployment, and re-encoding is what **drops the EXIF block** — a camera photograph can carry an
/// orientation flag, a device model and, on some platforms, a coordinate, and a proof photograph of
/// somebody's front door should not be carrying a coordinate into a bucket. The orientation is
/// applied to the pixels first so nothing is lost by dropping the tag.
///
/// ## The budget is a client-side number and the platform's limit is a different thing
///
/// `STORAGE_MAX_UPLOAD_BYTES` is 10 MiB and is the **bound**: the size above which the platform
/// refuses to sign an upload at all, and its refusal names the current limit because
/// `internal/delivery/proof.go` is explicit that a client cannot have it compiled in.
/// [ProofImagePolicy.maxBytes] is a much smaller **budget**, and it is a different question — what a
/// driver on a metered connection in a yard should be asked to send, not what the platform will
/// tolerate.
///
/// It is compiled in for the same reason SHIP-127's four hours is, and with the same reservation:
/// the compression happens on a handset that by assumption has no connection, so it cannot ask. A
/// client-policy endpoint should carry both this and the nudge threshold, cached while there is
/// still signal. `Docs/11` §3 has it.
library;

import 'dart:io';
import 'dart:isolate';
import 'dart:typed_data';

import 'package:image/image.dart' as img;
import 'package:path_provider/path_provider.dart';

/// What every proof photograph this build produces is, on the wire.
///
/// A constant rather than a guess from the file name: it is **signed into the pre-signed URL**
/// (`POST /v1/jobs/{id}/proof-uploads`), so the `Content-Type` on the PUT has to match it byte for
/// byte or the store refuses the upload with no explanation. One place decides it, and that is here.
const proofContentType = 'image/jpeg';

/// The file extension that goes with [proofContentType].
const proofFileExtension = 'jpg';

/// How small, and how legible, a proof photograph has to be.
///
/// **A value rather than a constant**, the same shape `QueuePolicy` uses and for the same reason:
/// a default is unavoidable on a device that compresses precisely when it cannot reach the
/// platform, but a default is not a constant.
final class ProofImagePolicy {
  const ProofImagePolicy({
    this.longestEdge = 1600,
    this.maxBytes = 1024 * 1024,
    this.qualities = const <int>[82, 70, 55, 40],
  });

  /// The longest edge of the stored image, in pixels.
  ///
  /// 1600 is a size at which a number plate photographed from two metres is still readable and a
  /// 12-megapixel original has lost about nine tenths of its bytes. It is deliberately larger than
  /// any screen this is viewed on: the customer's tracking view (SHIP-133) and the moderator's
  /// document viewer (SHIP-155) both need to be able to zoom into a corner, which is the whole
  /// point of the photograph.
  final int longestEdge;

  /// The size the encoder aims to come in under.
  ///
  /// One mebibyte, which is roughly ten seconds on a poor mobile connection and comfortably inside
  /// the platform's 10 MiB bound. See the library note for why the two numbers are different
  /// questions.
  final int maxBytes;

  /// The JPEG qualities to try, in order, stopping at the first that fits [maxBytes].
  ///
  /// A ladder rather than a single number because the size of a JPEG depends on the *subject*: a
  /// pallet in a bright warehouse compresses to a fraction of what a wet street at dusk does, and a
  /// fixed quality would either waste a driver's data on the first or fail the budget on the second.
  ///
  /// **The last rung is used whether or not it fits**, and that is deliberate: `Docs/01` §4.4 makes
  /// the photograph the difference between a delivery that can be completed and one that cannot, so
  /// an image that overshoots a *client-side* budget is uploaded anyway. The platform's bound is
  /// what actually refuses one, and it is fifteen times larger.
  final List<int> qualities;
}

/// One compressed photograph, ready to be written.
final class ProofImage {
  const ProofImage({
    required this.bytes,
    required this.width,
    required this.height,
    required this.quality,
  });

  final Uint8List bytes;
  final int width;
  final int height;

  /// The rung of [ProofImagePolicy.qualities] that was used. Kept for a log line and a test.
  final int quality;

  int get length => bytes.length;
}

/// Thrown when the bytes handed over are not an image this build can read.
///
/// A distinct type rather than a bare exception, because it is the one failure in this file that is
/// **not** the device's fault and is not retryable: a file that is not an image will not become one
/// on the next attempt, and the screen says so rather than offering to try again.
final class ProofImageUnreadable implements Exception {
  const ProofImageUnreadable(this.length);

  /// How many bytes were offered, which is the only thing worth saying about them.
  final int length;

  @override
  String toString() => 'ProofImageUnreadable: $length bytes did not decode as an image';
}

/// Downscales and re-encodes [bytes] as JPEG.
///
/// Pure, synchronous and free of `dart:ui`, which is what makes the *Done when*'s "compressed"
/// clause something a host test can assert on real bytes rather than something a platform channel
/// promises. It is also why it is worth running through `Isolate.run` from a screen — see
/// [compressProof].
ProofImage compressProofSync(Uint8List bytes, {ProofImagePolicy policy = const ProofImagePolicy()}) {
  final img.Image? decoded;
  try {
    decoded = img.decodeImage(bytes);
  } catch (_) {
    // The decoder walks the container's own length fields, so a truncated or invented file reaches
    // a `RangeError` rather than returning null. Both mean the same thing to a caller and neither
    // is retryable.
    throw ProofImageUnreadable(bytes.length);
  }
  if (decoded == null) throw ProofImageUnreadable(bytes.length);

  // Applies the EXIF orientation to the pixels. It has to happen before the metadata is dropped, or
  // every photograph taken in portrait arrives at the customer on its side.
  final upright = img.bakeOrientation(decoded);

  final longest = upright.width > upright.height ? upright.width : upright.height;
  final scaled = longest <= policy.longestEdge
      ? upright
      : img.copyResize(
          upright,
          width: upright.width >= upright.height ? policy.longestEdge : null,
          height: upright.height > upright.width ? policy.longestEdge : null,
          interpolation: img.Interpolation.average,
        );

  // **Explicit, and not a side effect of the resize.** A photograph already inside
  // [ProofImagePolicy.longestEdge] is not resized at all, so relying on `copyResize` to drop the
  // metadata would strip the coordinates off large images and leave them on small ones — which is
  // the worst of the three possible behaviours, because it is the one nobody would notice.
  scaled.exif = img.ExifData();

  late Uint8List encoded;
  late int used;

  for (final quality in policy.qualities) {
    encoded = img.encodeJpg(scaled, quality: quality);
    used = quality;
    if (encoded.length <= policy.maxBytes) break;
  }

  return ProofImage(bytes: encoded, width: scaled.width, height: scaled.height, quality: used);
}

/// [compressProofSync] off the UI isolate.
///
/// A 12-megapixel decode, resize and re-encode in Dart is hundreds of milliseconds on a good handset
/// and more on the sort of phone a driver actually carries. On the main isolate that is a frozen
/// shutter button, which reads as an app that did not take the photograph — so the driver taps it
/// again.
Future<ProofImage> compressProof(
  Uint8List bytes, {
  ProofImagePolicy policy = const ProofImagePolicy(),
}) {
  return Isolate.run(() => compressProofSync(bytes, policy: policy));
}

/// Where a compressed proof photograph is kept until the queue has sent it.
///
/// ## This class is the *Done when*'s last clause, and it is a closed door rather than a convention
///
/// "Never written to the photo library" is a statement about a destination, so the destination is
/// not a parameter. [write] computes the path itself, under one directory this app owns, and
/// [ProofStoreOutsideItsRoot] is what a caller gets for trying to name one anywhere else. There is
/// no method here that takes a path.
///
/// Three things hold the property, and only the first is code in this file:
///
/// - **The directory.** `getApplicationSupportDirectory()`, which is `Library/Application Support`
///   on iOS and `files/` in the app's own data directory on Android. Private to this application on
///   both, and — unlike the cache directory — **not purgeable by the operating system**, which
///   matters because a queued proof may sit here for hours in a valley and `Docs/07` §4 forbids
///   losing it.
/// - **The Android manifest declares no storage or media permission at all.** Under scoped storage
///   an app without `WRITE_EXTERNAL_STORAGE` or `READ_MEDIA_IMAGES` cannot write to the shared
///   media collections, whatever code it runs.
/// - **`Info.plist` declares no `NSPhotoLibraryAddUsageDescription`.** Without it iOS refuses
///   `PHPhotoLibrary` writes, and the App Store rejects a binary that asks.
///
/// `proof_never_reaches_the_gallery_test.dart` holds all three, and holds that no package or symbol
/// which writes to a gallery appears anywhere in this client.
final class ProofStore {
  const ProofStore(this.root);

  /// The application's private directory. See the note on the class for which one and why.
  final Directory root;

  /// Opens the store the application uses.
  static Future<ProofStore> open() async => ProofStore(await getApplicationSupportDirectory());

  /// Where every proof photograph goes, under [root].
  ///
  /// Named rather than loose in the root so that a person reading a bug report's file listing can
  /// tell a proof photograph from the queue's database.
  static const folder = 'proof';

  Directory get directory => Directory('${root.path}${Platform.pathSeparator}$folder');

  /// Writes [image] under [name], which must be a bare file name.
  ///
  /// [name] is a name and not a path, and that is the whole guard: anything carrying a separator or
  /// a parent reference is refused with [ProofStoreOutsideItsRoot] rather than resolved, because a
  /// name that can climb is a destination the caller chose.
  Future<File> write(ProofImage image, {required String name}) async {
    if (name.isEmpty ||
        name.contains('/') ||
        name.contains(r'\') ||
        name.contains('..') ||
        name.startsWith('.')) {
      throw ProofStoreOutsideItsRoot(name);
    }

    final folder = directory;
    await folder.create(recursive: true);

    final file = File('${folder.path}${Platform.pathSeparator}$name');

    // Belt and braces beside the check above, and the one that survives a change to it: whatever
    // the name did, the resolved path has to be inside the directory this store owns.
    if (!file.absolute.path.startsWith(folder.absolute.path)) {
      throw ProofStoreOutsideItsRoot(name);
    }

    await file.writeAsBytes(image.bytes, flush: true);
    return file;
  }

  /// Removes a stored photograph, once the platform has it.
  ///
  /// Missing is not an error: the sync worker deletes the row when the platform accepts the
  /// operation, and a file already gone is that having happened twice.
  Future<void> discard(String path) async {
    final file = File(path);
    if (await file.exists()) await file.delete();
  }
}

/// A caller tried to name a destination outside the store's own directory.
final class ProofStoreOutsideItsRoot implements Exception {
  const ProofStoreOutsideItsRoot(this.name);

  final String name;

  @override
  String toString() =>
      'ProofStoreOutsideItsRoot: "$name" is not a bare file name; a proof photograph is written '
      'inside this application and nowhere else';
}
