// SHIP-130's "compressed" clause, on real bytes.
//
// The compressor is pure Dart and free of `dart:ui`, which is the whole reason it was chosen over a
// native one: this file decodes and re-encodes actual JPEGs on the host, so "compressed" is a
// measurement rather than a promise a platform channel made. A native compressor would be a method
// channel with nothing behind it here, and this file would be four `expect(true)`s.

import 'dart:io';
import 'dart:math';
import 'dart:typed_data';

import 'package:flutter_test/flutter_test.dart';
import 'package:image/image.dart' as img;
import 'package:shipper/core/queue/queued_operation.dart';
import 'package:shipper/features/delivery/proof_image.dart';

import 'proof_fixture.dart';

void main() {
  group('the compression', () {
    late Uint8List original;

    setUpAll(() => original = photograph());

    test('makes a phone photograph small enough for a metered connection', () {
      const policy = ProofImagePolicy();
      final compressed = compressProofSync(original, policy: policy);

      expect(
        compressed.length,
        lessThan(original.length),
        reason: 'Docs/01 §5.2 requires the image to be compressed on the device before upload',
      );
      expect(max(compressed.width, compressed.height), policy.longestEdge);
      expect(
        compressed.length,
        lessThan(1024 * 1024 * 10),
        reason: 'and it must be inside STORAGE_MAX_UPLOAD_BYTES, which is the platform’s bound '
            'rather than this budget',
      );
    });

    test('is JPEG whatever came in, which is what the pre-signed URL is signed for', () {
      // A PNG in, a JPEG out. The content type is signed into the upload URL, so a compressor that
      // passed the input format through would produce a signature failure on a handset and nothing
      // anywhere else.
      final png = img.encodePng(img.decodeJpg(original)!);
      final compressed = compressProofSync(png);

      expect(img.findFormatForData(compressed.bytes), img.ImageFormat.jpg);
      expect(proofContentType, 'image/jpeg');
    });

    test('drops the metadata block, on the resized path and on the untouched one', () {
      // A proof photograph of somebody's front door should not carry a device model, a serial
      // number or a coordinate into a bucket. The interesting half is the second image: it is
      // already inside `longestEdge`, so it is never resized — and a build that relied on the
      // resize to drop the metadata would strip large photographs and leave small ones tagged,
      // which is the one behaviour of the three that nobody would notice.
      Uint8List tagged({required int width, required int height}) {
        final image = img.Image(width: width, height: height);
        image.exif.imageIfd['Make'] = 'a handset maker';
        image.exif.imageIfd['Model'] = 'a handset model';
        return img.encodeJpg(image, quality: 95);
      }

      for (final size in const <({int width, int height})>[
        (width: 3000, height: 2000),
        (width: 320, height: 240),
      ]) {
        final input = tagged(width: size.width, height: size.height);
        expect(
          img.decodeJpg(input)!.exif.isEmpty,
          isFalse,
          reason: 'the ${size.width}px fixture carries metadata to begin with',
        );

        final out = img.decodeJpg(compressProofSync(input).bytes)!;
        expect(out.exif.isEmpty, isTrue, reason: 'the ${size.width}px image kept its metadata');
      }
    });

    test('steps down the quality ladder until it fits, and stops', () {
      // A budget small enough that the first rung cannot meet it, and large enough that a later one
      // can — which is the only arrangement in which "a ladder" differs from "a number".
      //
      // The budgets are chosen against this fixture, which is uniform noise and therefore the
      // worst input a JPEG encoder can be given — a real photograph of a pallet compresses to a
      // small fraction of these numbers, which is why the shipped default is 1 MiB and these are
      // megabytes.
      const generous = ProofImagePolicy(maxBytes: 1024 * 1024 * 8);
      const tight = ProofImagePolicy(maxBytes: 1300 * 1024);

      expect(compressProofSync(original, policy: generous).quality, 82);

      final squeezed = compressProofSync(original, policy: tight);
      expect(squeezed.quality, greaterThan(40), reason: 'not the bottom rung for this image');
      expect(squeezed.quality, lessThan(82), reason: 'and not the top one either');
      expect(squeezed.length, lessThanOrEqualTo(tight.maxBytes));
    });

    test('sends the last rung whether or not it fits, because a delivery depends on it', () {
      // Docs/01 §4.4: a job cannot reach Delivered without proof. An image that overshoots a
      // *client-side* budget is uploaded anyway; the platform's bound is fifteen times larger and is
      // the thing that actually refuses one.
      const impossible = ProofImagePolicy(maxBytes: 1);
      final compressed = compressProofSync(original, policy: impossible);

      expect(compressed.quality, 40, reason: 'the bottom rung');
      expect(compressed.length, greaterThan(1));
    });

    test('leaves an image already inside the budget at its own size', () {
      final small = img.encodeJpg(img.Image(width: 400, height: 300), quality: 90);
      final compressed = compressProofSync(small);

      expect(compressed.width, 400);
      expect(compressed.height, 300);
    });

    test('says so when the bytes are not an image at all', () {
      // Its own type because it is the one failure here that is not retryable: a file that is not an
      // image will not become one on the next attempt, and the screen says so rather than offering
      // to try again.
      expect(
        () => compressProofSync(Uint8List.fromList(const [0, 1, 2, 3, 4])),
        throwsA(isA<ProofImageUnreadable>()),
      );
    });

    test('what it produces is what the sender declares to the platform', () {
      // Two constants that have to agree and live in two layers: the encoder's output format, and
      // the media type `OperationKind.proof` declares to `POST /v1/jobs/{id}/proof-uploads` — which
      // is **signed into the URL**. A compressor quietly emitting PNG would fail only on a handset,
      // as a signature error with no explanation.
      expect(OperationKind.proof.attachmentContentType, proofContentType);
      expect(OperationKind.milestone.attachmentContentType, isNull);
    });
  });

  group('where the compressed photograph is written', () {
    late Directory root;
    late ProofStore store;
    late ProofImage image;

    setUp(() {
      root = Directory.systemTemp.createTempSync('shipper_proof_store');
      addTearDown(() {
        if (root.existsSync()) root.deleteSync(recursive: true);
      });
      store = ProofStore(root);
      image = compressProofSync(photograph(width: 200, height: 150));
    });

    test('inside this application’s own directory and nowhere else', () async {
      final file = await store.write(image, name: 'a4f21c9e.jpg');

      expect(file.existsSync(), isTrue);
      expect(file.path.startsWith(store.directory.path), isTrue);
      expect(await file.length(), image.length);
    });

    test('a destination is never a parameter, so a gallery path cannot be one', () async {
      // **This is the test the *Done when*'s last clause turns on.** `write` takes a name and not a
      // path, so there is no argument that names `DCIM/Camera`, `~/Pictures`, or anywhere else —
      // and a name that tries to climb out is refused rather than resolved.
      for (final name in <String>[
        '/sdcard/DCIM/Camera/proof.jpg',
        '../../../../sdcard/DCIM/Camera/proof.jpg',
        r'..\..\Pictures\proof.jpg',
        '../proof.jpg',
        '',
      ]) {
        await expectLater(
          store.write(image, name: name),
          throwsA(isA<ProofStoreOutsideItsRoot>()),
          reason: '"$name" named a destination outside the store',
        );
      }
    });

    test('and discarding one that is already gone is not an error', () async {
      final file = await store.write(image, name: 'b91.jpg');

      await store.discard(file.path);
      expect(file.existsSync(), isFalse);

      await store.discard(file.path);
    });
  });
}
