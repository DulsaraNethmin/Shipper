/// Compressing a captured photograph, and putting it somewhere only this app can read (SHIP-130,
/// SHIP-81c).
///
/// ## The copy generalised in the move, deliberately
///
/// This was `proof_image.dart`, in `features/delivery/`, and it said "proof" in every name and every
/// paragraph. SHIP-81c gave it a second caller — a provider photographing the four documents
/// `Docs/04` §3 collects — and features do not import one another (`Docs/07` §2), so it moved to
/// `core/` on the precedent `ProviderOnly` set at SHIP-100. **The names and the reasoning were
/// rewritten in the same move rather than left to be puzzled over**: a class called `ProofStore`
/// holding somebody's insurance certificate is a file listing that lies to whoever reads it next.
///
/// Two jobs, in one file because they are one decision. Both callers' requirements say the same two
/// things — `Docs/01` §5.2 and `Docs/04` §3.1 both require the image compressed on the device before
/// upload, and SHIP-130's *Done when* and `Docs/04` §3.1 both require that it is **never written to
/// the photo library**. The second is a statement about where the bytes go, so the compressor and
/// the writer are not separable: a compressor that returned bytes and let each caller choose a
/// destination is a compressor with a gallery one refactor away.
///
/// ## The compression, and what the numbers are for
///
/// `Docs/01` §5.2: *"Assume metered mobile data. Compress images on the device before upload."*
/// `Docs/04` §3.1 says it again for verification. And the image has to stay legible at the other
/// end — a licence plate, a doorstep, a pallet's label, an expiry date printed on an insurance
/// certificate. Both at once is what [CapturedImagePolicy] is: downscale the longest edge to
/// something a screen renders, then step down the JPEG quality until the file is inside a budget,
/// and stop.
///
/// **JPEG, always, whatever came in.** The platform accepts `image/jpeg`, `image/png` and
/// `image/heic` (`STORAGE_ACCEPTED_CONTENT_TYPES`), and this build sends the first of the three for
/// two reasons: it is the only one of the three that is certainly in the accepted list on every
/// deployment, and re-encoding is what **drops the EXIF block** — a camera photograph can carry an
/// orientation flag, a device model and, on some platforms, a coordinate. A proof photograph of
/// somebody's front door should not be carrying a coordinate into a bucket, and neither should a
/// photograph of a licence sitting on somebody's kitchen table. The orientation is applied to the
/// pixels first so nothing is lost by dropping the tag.
///
/// **This is also what makes SHIP-81d's file fallback safe.** A file a provider chose from their
/// own device arrives with whatever metadata its author left on it, and it goes through exactly this
/// function before anything uploads it — so the EXIF strip is a property of the pipeline rather than
/// of the camera.
///
/// ## The budget is a client-side number and the platform's limit is a different thing
///
/// `STORAGE_MAX_UPLOAD_BYTES` is 10 MiB and is the **bound**: the size above which the platform
/// refuses to sign an upload at all, and its refusal names the current limit because
/// `internal/delivery/proof.go` is explicit that a client cannot have it compiled in.
/// [CapturedImagePolicy.maxBytes] is a much smaller **budget**, and it is a different question —
/// what somebody on a metered connection should be asked to send, not what the platform will
/// tolerate.
library;

import 'dart:io';
import 'dart:isolate';
import 'dart:typed_data';

import 'package:image/image.dart' as img;
import 'package:path_provider/path_provider.dart';

/// What every photograph this build produces is, on the wire.
///
/// A constant rather than a guess from the file name: it is **signed into the pre-signed URL** —
/// `POST /v1/jobs/{id}/proof-uploads` for a delivery and
/// `POST /v1/provider/verification/documents/uploads` for a document — so the `Content-Type` on the
/// PUT has to match it byte for byte or the store refuses the upload with no explanation. One place
/// decides it, and that is here.
const capturedImageContentType = 'image/jpeg';

/// The file extension that goes with [capturedImageContentType].
const capturedImageFileExtension = 'jpg';

/// How small, and how legible, a captured photograph has to be.
///
/// **A value rather than a constant**, the same shape `QueuePolicy` uses and for the same reason:
/// a default is unavoidable on a device that compresses precisely when it cannot reach the
/// platform, but a default is not a constant.
final class CapturedImagePolicy {
  const CapturedImagePolicy({
    this.longestEdge = 1600,
    this.maxBytes = 1024 * 1024,
    this.qualities = const <int>[82, 70, 55, 40],
  });

  /// The longest edge of the stored image, in pixels.
  ///
  /// 1600 is a size at which a number plate photographed from two metres is still readable and a
  /// 12-megapixel original has lost about nine tenths of its bytes. It is deliberately larger than
  /// any screen this is viewed on: the customer's tracking view (SHIP-133) and the administrator's
  /// document viewer (SHIP-155) both need to be able to zoom into a corner, which is the whole
  /// point of the photograph — and for a document it is the whole point twice over, because
  /// `Docs/04` §3 has an administrator judging by eye whether an image is legible and not visibly
  /// expired.
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
  /// fixed quality would either waste somebody's data on the first or fail the budget on the second.
  ///
  /// **The last rung is used whether or not it fits**, and that is deliberate: `Docs/01` §4.4 makes
  /// the photograph the difference between a delivery that can be completed and one that cannot,
  /// and `Docs/04` §3 makes it the difference between a provider who can bid and one who cannot. An
  /// image that overshoots a *client-side* budget is uploaded anyway. The platform's bound is what
  /// actually refuses one, and it is fifteen times larger.
  final List<int> qualities;
}

/// One compressed photograph, ready to be written.
final class CapturedImage {
  const CapturedImage({
    required this.bytes,
    required this.width,
    required this.height,
    required this.quality,
  });

  final Uint8List bytes;
  final int width;
  final int height;

  /// The rung of [CapturedImagePolicy.qualities] that was used. Kept for a log line and a test.
  final int quality;

  int get length => bytes.length;
}

/// Thrown when the bytes handed over are not an image this build can read.
///
/// A distinct type rather than a bare exception, because it is the one failure in this file that is
/// **not** the device's fault and is not retryable: a file that is not an image will not become one
/// on the next attempt, and the screen says so rather than offering to try again. SHIP-81d is what
/// makes this reachable from a file the user chose rather than only from a camera that misbehaved.
final class CapturedImageUnreadable implements Exception {
  const CapturedImageUnreadable(this.length);

  /// How many bytes were offered, which is the only thing worth saying about them.
  final int length;

  @override
  String toString() => 'CapturedImageUnreadable: $length bytes did not decode as an image';
}

/// Downscales and re-encodes [bytes] as JPEG.
///
/// Pure, synchronous and free of `dart:ui`, which is what makes the "compressed" clause of both
/// tickets something a host test can assert on real bytes rather than something a platform channel
/// promises. It is also why it is worth running through `Isolate.run` from a screen — see
/// [compressCapture].
CapturedImage compressCaptureSync(
  Uint8List bytes, {
  CapturedImagePolicy policy = const CapturedImagePolicy(),
}) {
  final img.Image? decoded;
  try {
    decoded = img.decodeImage(bytes);
  } catch (_) {
    // The decoder walks the container's own length fields, so a truncated or invented file reaches
    // a `RangeError` rather than returning null. Both mean the same thing to a caller and neither
    // is retryable.
    throw CapturedImageUnreadable(bytes.length);
  }
  if (decoded == null) throw CapturedImageUnreadable(bytes.length);

  // Applies the EXIF orientation to the pixels. It has to happen before the metadata is dropped, or
  // every photograph taken in portrait arrives on its side.
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
  // [CapturedImagePolicy.longestEdge] is not resized at all, so relying on `copyResize` to drop the
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

  return CapturedImage(bytes: encoded, width: scaled.width, height: scaled.height, quality: used);
}

/// [compressCaptureSync] off the UI isolate.
///
/// A 12-megapixel decode, resize and re-encode in Dart is hundreds of milliseconds on a good handset
/// and more on the sort of phone a driver actually carries. On the main isolate that is a frozen
/// shutter button, which reads as an app that did not take the photograph — so the button is tapped
/// again.
Future<CapturedImage> compressCapture(
  Uint8List bytes, {
  CapturedImagePolicy policy = const CapturedImagePolicy(),
}) {
  return Isolate.run(() => compressCaptureSync(bytes, policy: policy));
}

/// The directories this application keeps captured images in.
///
/// ## This is the one API decision SHIP-81c's move actually took, and it is a closed set on purpose
///
/// `ProofStore` had `static const folder = 'proof'` — a hard-coded segment that no refactoring tool
/// follows and that a verification document must not land under. SHIP-81c's *Done when* says the
/// helpers must "land under a directory that is not `proof/`", and it means both the source
/// directory and this one: a provider's driver licence filed under `proof/` is mislabelled in
/// exactly the place the label is all anybody has — a file listing pasted into a support
/// conversation, months later, by somebody who was not here.
///
/// **Three ways to parameterise it were available and this is the strongest.**
///
/// - A `String folder` field would make the destination a parameter, and the whole of
///   [CapturedImageStore]'s guarantee is that the destination is *not* one. `'../../../DCIM'` is a
///   string.
/// - A validated `String` — bare segment, no separator, no `..` — closes that hole and leaves the
///   set of directories this application uses open, discoverable only by grep.
/// - **A closed enum makes an unlisted destination fail to compile**, which is the same move
///   `OperationKind` makes with a private constructor in `core/queue`, and it puts the list of
///   places this application writes images in one readable place. Adding a third is then a decision
///   somebody recorded rather than a string somebody typed.
///
/// The names are the platform's own, which is the second half of the decision: the object key
/// `POST /v1/provider/verification/documents/uploads` mints is `verification/<provider>/<uuid>`
/// (`contracts/paths/profiles.yaml`), so the handset's directory and the bucket's prefix read the
/// same way and a person tracing one image through both is reading one word rather than two.
enum CaptureFolder {
  /// Proof of delivery (SHIP-130) — `Docs/01` §4.4's photograph of the goods at the delivery point.
  proof('proof'),

  /// Provider verification evidence (SHIP-81c) — `Docs/04` §3's four documents.
  verification('verification');

  const CaptureFolder(this.folder);

  /// The directory's name under [CapturedImageStore.root]. A bare segment, always.
  final String folder;
}

/// Where a compressed photograph is kept until the platform has it.
///
/// ## This class is a *Done when* on both sides, and it is a closed door rather than a convention
///
/// "Never written to the photo library" is a statement about a destination, so the destination is
/// not a parameter. [write] computes the path itself, under one directory this app owns, and
/// [CapturedImageStoreOutsideItsRoot] is what a caller gets for trying to name one anywhere else.
/// There is no method here that takes a path, and [CaptureFolder] is a closed set rather than a
/// string, so there is no argument to any of this that could name `DCIM/Camera`.
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
/// `test/nothing_captured_reaches_the_gallery_test.dart` holds all three, and holds that no package
/// or symbol which writes to a gallery appears anywhere in this client.
///
/// ## `Docs/04` §3.1's third clause is the caller's, and the method for it is [discard]
///
/// *"…and must be cleared from app storage once uploaded."* A store cannot know when that has
/// happened, so what this class offers is a removal that is safe to call twice, and the obligation
/// sits with whoever completed the upload. For a delivery that is `operation_sender.dart`, which
/// deletes the file after the platform has accepted the milestone; for verification it is
/// `verification_repository.dart`, which deletes it after the submission is recorded.
final class CapturedImageStore {
  const CapturedImageStore(this.root, {required this.folder});

  /// The application's private directory. See the note on the class for which one and why.
  final Directory root;

  /// Which of this application's image directories this store writes to.
  final CaptureFolder folder;

  /// Opens the store [folder] names, in the directory the application owns.
  static Future<CapturedImageStore> open(CaptureFolder folder) async =>
      CapturedImageStore(await getApplicationSupportDirectory(), folder: folder);

  Directory get directory =>
      Directory('${root.path}${Platform.pathSeparator}${folder.folder}');

  /// Writes [image] under [name], which must be a bare file name.
  ///
  /// [name] is a name and not a path, and that is the whole guard: anything carrying a separator or
  /// a parent reference is refused with [CapturedImageStoreOutsideItsRoot] rather than resolved,
  /// because a name that can climb is a destination the caller chose.
  Future<File> write(CapturedImage image, {required String name}) async {
    if (name.isEmpty ||
        name.contains('/') ||
        name.contains(r'\') ||
        name.contains('..') ||
        name.startsWith('.')) {
      throw CapturedImageStoreOutsideItsRoot(name);
    }

    final destination = directory;
    await destination.create(recursive: true);

    final file = File('${destination.path}${Platform.pathSeparator}$name');

    // Belt and braces beside the check above, and the one that survives a change to it: whatever
    // the name did, the resolved path has to be inside the directory this store owns.
    if (!isInside(file, destination)) throw CapturedImageStoreOutsideItsRoot(name);

    await file.writeAsBytes(image.bytes, flush: true);
    return file;
  }

  /// Whether [file] resolves to somewhere inside [folder].
  ///
  /// ## It normalises first, and SHIP-81c's mutation run is why
  ///
  /// This was `file.absolute.path.startsWith(folder.absolute.path)` — inherited unchanged from
  /// `ProofStore`, described in its own comment as the check "that survives a change" to the name
  /// guard, and **inert**. `String.startsWith` compares the joined string rather than the resolved
  /// one, and `<folder>/../../DCIM/x.jpg` starts with `<folder>` on every platform. Measured
  /// directly: `'/tmp/root/verification/../x.jpg'.startsWith('/tmp/root/verification')` is `true`.
  ///
  /// So the store had one guard wearing a two-layer disguise. Deleting the name guard's `..` clause
  /// in a mutation run was refused only by the operating system — a `PathNotFoundException` from
  /// `writeAsBytes`, which on a device where the directory happens to exist is not a refusal at all.
  ///
  /// `Uri.normalizePath` resolves the `..` segments before the comparison, which is what makes this
  /// the second layer its comment always claimed it was.
  ///
  /// **Public so it can be tested on its own.** A second layer that can only be reached through the
  /// first is a second layer nobody can demonstrate — which is exactly how the previous one stayed
  /// inert through every test this store has ever had.
  static bool isInside(File file, Directory folder) {
    final resolved = Uri.file(file.absolute.path).normalizePath().toFilePath();
    final root = Uri.directory(folder.absolute.path).normalizePath().toFilePath();
    return resolved.startsWith(root);
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
final class CapturedImageStoreOutsideItsRoot implements Exception {
  const CapturedImageStoreOutsideItsRoot(this.name);

  final String name;

  @override
  String toString() =>
      'CapturedImageStoreOutsideItsRoot: "$name" is not a bare file name; a captured photograph is '
      'written inside this application and nowhere else';
}
