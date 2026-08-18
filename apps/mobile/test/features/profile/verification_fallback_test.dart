// SHIP-81d's *Done when*, from the screen a provider who declined the camera actually uses.
//
// > A provider who refuses the camera permission still submits all four documents, through a picker
// > that reaches no photo library — the file-upload fallback Docs 04 §3.1 requires so that a refused
// > permission never blocks verification outright — and the package it depends on carries the
// > written argument the gallery guard demands of anything in that space.
//
// **"All four" is walked rather than asserted about.** The clause is that a refused permission never
// blocks verification *outright*, and the only honest reading of that is a provider who reaches the
// end: four documents, four refusals, four submissions. A test that checked one kind and trusted the
// loop would be a test of the loop.
//
// The last clause — the written argument — is `pubspec.yaml`'s, and what holds it is
// `test/nothing_captured_reaches_the_gallery_test.dart`, which now bans `file_picker` by name and
// checks that neither photo-library permission nor usage string appeared.

import 'dart:typed_data';

import 'package:flutter/widgets.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/features/profile/document_file_source.dart';
import 'package:shipper/features/profile/verification_document.dart';

import '../../support/capture_fixture.dart';
import 'fake_verification_repository.dart';
import 'verification_app.dart';

void main() {
  group('a provider who refuses the camera', () {
    // One test per kind rather than a loop inside one, because signing in is a journey through the
    // real router and can only be walked once per `testWidgets`. Four tests is the *Done when*'s
    // "all four", and each one starts from a fresh refusal.
    for (final kind in VerificationDocumentKind.values) {
      testWidgets('still submits their ${kind.wire}', (tester) async {
        final verification = FakeVerificationRepository();
        final files = FakeDocumentFileSource(bytes: photograph(width: 500, height: 400));

        await openCapture(
          tester,
          verification: verification,
          kind: kind,
          overrides: <Override>[
            ...withCamera(FakeCaptureCamera(problem: CameraProblem.refused)),
            documentFileSourceProvider.overrideWithValue(files),
          ],
        );

        // The camera refused, so there is no shutter — which is what makes the next tap the whole
        // ticket rather than a convenience beside one.
        expect(find.byKey(const Key('capture-document-blocked')), findsOneWidget);
        expect(find.byKey(const Key('capture-document-shutter')), findsNothing);

        await tester.tap(find.byKey(const Key('capture-document-choose-file-blocked')));
        await tester.pumpAndSettle();

        expect(files.opened, 1);
        expect(verification.submissions, hasLength(1));
        expect(verification.submissions.single.kind, kind);
        expect(find.byKey(const Key('capture-document-submitted')), findsOneWidget);
      });
    }

    testWidgets('and the file goes through the same compressor a photograph does', (tester) async {
      // `Docs/04` §3.1 requires the image compressed on the device, and it says so about
      // verification images rather than about photographs. A file a provider chose arrives with
      // whatever its author left on it — including, for a photo taken on the same phone, an EXIF
      // block with a coordinate in it — so the fallback must not be the path that skips the
      // compressor. It is not: the screen hands bytes to the same controller the shutter does.
      final verification = FakeVerificationRepository();
      final original = photograph(width: 4000, height: 3000);
      final files = FakeDocumentFileSource(bytes: original);

      await openCapture(
        tester,
        verification: verification,
        overrides: <Override>[
          ...withCamera(FakeCaptureCamera(problem: CameraProblem.refused)),
          documentFileSourceProvider.overrideWithValue(files),
        ],
      );

      await tester.tap(find.byKey(const Key('capture-document-choose-file-blocked')));
      await tester.pumpAndSettle();

      final sent = verification.submissions.single.image;
      expect(sent.length, lessThan(original.length));
      expect(sent.width, lessThanOrEqualTo(1600));
    });

    testWidgets('and a file that is not an image is refused on the device', (tester) async {
      // Not in a bucket, and not by an administrator's eye a week later. The picker offers image
      // types only, but a document provider may report a type it cannot honour, so the compressor
      // is the guard that actually holds.
      final verification = FakeVerificationRepository();
      final files = FakeDocumentFileSource(bytes: Uint8List.fromList(const <int>[0, 1, 2, 3, 4]));

      await openCapture(
        tester,
        verification: verification,
        overrides: <Override>[
          ...withCamera(FakeCaptureCamera(problem: CameraProblem.refused)),
          documentFileSourceProvider.overrideWithValue(files),
        ],
      );

      await tester.tap(find.byKey(const Key('capture-document-choose-file-blocked')));
      await tester.pumpAndSettle();

      expect(verification.submissions, isEmpty);
      expect(find.byKey(const Key('capture-document-blocked-failed')), findsOneWidget);
    });

    testWidgets('and backing out of the picker changes nothing', (tester) async {
      // Cancelling is not an error. A provider who opens the picker and changes their mind comes
      // back to the screen they left — and a failure message there would be telling them something
      // went wrong when nothing did.
      final verification = FakeVerificationRepository();
      final files = FakeDocumentFileSource();

      await openCapture(
        tester,
        verification: verification,
        overrides: <Override>[
          ...withCamera(FakeCaptureCamera(problem: CameraProblem.refused)),
          documentFileSourceProvider.overrideWithValue(files),
        ],
      );

      await tester.tap(find.byKey(const Key('capture-document-choose-file-blocked')));
      await tester.pumpAndSettle();

      expect(files.opened, 1);
      expect(verification.submissions, isEmpty);
      expect(find.byKey(const Key('capture-document-blocked')), findsOneWidget);
      expect(find.byKey(const Key('capture-document-blocked-failed')), findsNothing);
      expect(find.byKey(const Key('capture-document-picker-failed-blocked')), findsNothing);
    });

    testWidgets('and a picker that will not open says so rather than doing nothing', (tester) async {
      // The one case where a provider genuinely has no route on: no camera and no picker. It is rare
      // — unlike a camera there is no permission to refuse — and it is worth a sentence rather than
      // a tap that appears to do nothing, which is what a swallowed exception would look like.
      final files = FakeDocumentFileSource(unavailable: true);

      await openCapture(
        tester,
        verification: FakeVerificationRepository(),
        overrides: <Override>[
          ...withCamera(FakeCaptureCamera(problem: CameraProblem.refused)),
          documentFileSourceProvider.overrideWithValue(files),
        ],
      );

      await tester.tap(find.byKey(const Key('capture-document-choose-file-blocked')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('capture-document-picker-failed-blocked')), findsOneWidget);
    });
  });

  group('a provider whose camera works', () {
    testWidgets('is offered the file too, because the document is often already on the phone',
        (tester) async {
      // An insurance certificate emailed as a scan, an ABN extract downloaded from the tax office.
      // Making somebody photograph a screen would be worse evidence for the administrator who has
      // to read it — `Docs/04` §3 has them judging legibility by eye.
      final verification = FakeVerificationRepository();
      final files = FakeDocumentFileSource(bytes: photograph(width: 500, height: 400));

      await openCapture(
        tester,
        verification: verification,
        overrides: <Override>[
          ...withCamera(FakeCaptureCamera(bytes: photograph(width: 500, height: 400))),
          documentFileSourceProvider.overrideWithValue(files),
        ],
      );

      // Both routes are on the screen, and photographing is the loud one.
      expect(find.byKey(const Key('capture-document-shutter')), findsOneWidget);
      expect(find.byKey(const Key('capture-document-choose-file')), findsOneWidget);

      await tester.tap(find.byKey(const Key('capture-document-choose-file')));
      await tester.pumpAndSettle();

      expect(verification.submissions, hasLength(1));
      expect(find.byKey(const Key('capture-document-submitted')), findsOneWidget);
    });
  });
}

/// A picker that answers with bytes a test chose, cancels, or refuses to open.
class FakeDocumentFileSource implements DocumentFileSource {
  FakeDocumentFileSource({this.bytes, this.unavailable = false});

  /// What the picker returns. `null` is a person who backed out, which is not an error.
  final Uint8List? bytes;

  /// Set to throw, which is a platform that would not open a picker at all.
  final bool unavailable;

  var opened = 0;

  @override
  Future<Uint8List?> pick() async {
    opened++;
    if (unavailable) throw const DocumentFileUnavailable(detail: 'fake');
    return bytes;
  }
}
