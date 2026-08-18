// SHIP-81c's *Done when*, clause by clause, from the screen a provider actually uses.
//
// > A provider photographs each of Docs 04 §3's four kinds in features/profile/ and it reaches
// > SHIP-81b's upload; the image is never written to the device photo library, is compressed on the
// > device, and is cleared from app storage once uploaded; the capture helpers move to core/ under a
// > name and a doc comment that no longer say proof, and land under a directory that is not proof/;
// > and the camera purpose string covers this use in both the Dart constant and Info.plist.
//
// Four of the six clauses are here. The other two are elsewhere on purpose:
//
// - **"never written to the photo library"** is `test/nothing_captured_reaches_the_gallery_test.dart`,
//   which proves it for every code path rather than for the one a test walked — no permission, no
//   package, no symbol, in the whole build.
// - **"the camera purpose string"** is `test/core/permissions/permission_copy_test.dart`, which is
//   where the Dart constant and `Info.plist` are already held byte-for-byte together.
//
// **Everything is driven through the router**, from the sign-in screen, by tapping. A route missing
// from `_signedInLocations` or `_signedInPatterns` is silently redirected to the home shell, which
// from the outside is indistinguishable from a button that does nothing.

import 'package:flutter/widgets.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/core/capture/captured_image.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/profile/verification_document.dart';

import '../../support/capture_fixture.dart';
import 'fake_verification_repository.dart';
import 'verification_app.dart';

void main() {
  group('the four kinds Docs/04 §3 collects', () {
    testWidgets('are all four, and each one is reachable from the list', (tester) async {
      final verification = FakeVerificationRepository();
      await openDocuments(tester, verification: verification);

      expect(find.byKey(const Key('verification-documents')), findsOneWidget);

      // The set, not a count: a test that asserted `findsNWidgets(4)` would pass on four cards for
      // the same kind. `Docs/04` §3 names licence, registration, insurance and ABN evidence, and
      // `contracts/paths/profiles.yaml`'s `DocumentKind` spells them.
      expect(
        VerificationDocumentKind.values.map((kind) => kind.wire).toSet(),
        <String>{'licence', 'registration', 'insurance', 'abn_evidence'},
      );

      for (final kind in VerificationDocumentKind.values) {
        expect(
          find.byKey(Key('verification-document-${kind.wire}-capture')),
          findsOneWidget,
          reason: '${kind.wire} has no way to photograph it',
        );
      }
    });

    // One test per kind rather than a loop inside one, because signing in is a journey through the
    // real router and it can only be walked once per `testWidgets`.
    for (final kind in VerificationDocumentKind.values) {
      testWidgets('${kind.wire} opens its own camera, named after the document', (tester) async {
        final camera = FakeCaptureCamera(bytes: photograph(width: 400, height: 300));
        await openCapture(
          tester,
          verification: FakeVerificationRepository(),
          kind: kind,
          overrides: withCamera(camera),
        );

        expect(
          find.byKey(const Key('capture-document-screen')),
          findsOneWidget,
          reason: 'the camera screen for ${kind.wire} was not reached — check '
              '_signedInPatterns, which redirects a missing route to the home shell',
        );
        expect(find.byKey(Key('capture-document-guidance-${kind.wire}')), findsOneWidget);
      });
    }
  });

  group('a photograph reaches SHIP-81b’s upload', () {
    testWidgets('as the kind the provider chose, compressed, with one key', (tester) async {
      final verification = FakeVerificationRepository();
      final camera = FakeCaptureCamera(bytes: photograph(width: 4000, height: 3000));

      await openCapture(
        tester,
        verification: verification,
        kind: VerificationDocumentKind.insurance,
        overrides: withCamera(camera, key: 'ab12cd34'),
      );

      await tester.tap(find.byKey(const Key('capture-document-shutter')));
      await tester.pumpAndSettle();

      expect(verification.submissions, hasLength(1));
      final sent = verification.submissions.single;

      expect(sent.kind, VerificationDocumentKind.insurance);
      expect(sent.key, 'ab12cd34');

      // **"Compressed on the device", measured rather than asserted.** The compressor the screen
      // ran is the real one, so this is the actual output: the longest edge is inside the policy's
      // and the file is a fraction of what the camera produced.
      expect(sent.image.width, lessThanOrEqualTo(const CapturedImagePolicy().longestEdge));
      expect(sent.image.height, lessThanOrEqualTo(const CapturedImagePolicy().longestEdge));
      expect(sent.image.length, lessThan(camera.bytes!.length));

      expect(find.byKey(const Key('capture-document-submitted')), findsOneWidget);
    });

    testWidgets('and the list re-reads the platform rather than appending locally', (tester) async {
      final verification = FakeVerificationRepository();
      final camera = FakeCaptureCamera(bytes: photograph(width: 400, height: 300));

      await openCapture(
        tester,
        verification: verification,
        overrides: withCamera(camera),
      );

      final before = verification.reads;

      await tester.tap(find.byKey(const Key('capture-document-shutter')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('capture-document-done')));
      await tester.pumpAndSettle();

      // The read happened again, which is what makes the row on the list the platform's answer
      // rather than this device's claim. A locally appended row would show the same tick with
      // nothing behind it.
      expect(verification.reads, greaterThan(before));
      expect(
        find.byKey(const Key('verification-document-licence-state')),
        findsOneWidget,
      );
      expect(find.textContaining('Somebody will review it'), findsAtLeast(1));
    });
  });

  group('a failure leaves the photograph on the device', () {
    // The store half of `Docs/04` §3.1 — written, then cleared, and *not* cleared on a failure — is
    // proved against the real repository in `verification_repository_test.dart`, which is the only
    // place the file can be observed between the write and the discard. What is proved here is what
    // the provider does about it, which is the half a repository test cannot see.

    testWidgets('and it survives a failure, so a retry is not a second photograph', (tester) async {
      final verification = FakeVerificationRepository()
        ..submitFailure = notUploaded()
        ..failOnce = true;
      final camera = FakeCaptureCamera(bytes: photograph(width: 400, height: 300));

      await openCapture(tester, verification: verification, overrides: withCamera(camera));

      await tester.tap(find.byKey(const Key('capture-document-shutter')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('capture-document-failed')), findsOneWidget);
      expect(camera.captures, 1);

      await tester.tap(find.byKey(const Key('capture-document-retry')));
      await tester.pumpAndSettle();

      // The shutter was pressed once and the platform saw two attempts, which is the whole point:
      // a provider whose connection dropped does not have to find their licence again.
      expect(camera.captures, 1);
      expect(verification.submissions, hasLength(2));
      expect(find.byKey(const Key('capture-document-submitted')), findsOneWidget);
    });

    testWidgets('and the retry carries the same idempotency key', (tester) async {
      // `Docs/07` §4: the key is generated once where the user acts and reused unchanged across
      // every retry of that action. A second key here would record two documents for one
      // photograph — which the platform would accept, because it is two object keys.
      final verification = FakeVerificationRepository()
        ..submitFailure = const ApiUnreachable()
        ..failOnce = true;
      final camera = FakeCaptureCamera(bytes: photograph(width: 400, height: 300));

      await openCapture(
        tester,
        verification: verification,
        overrides: withCamera(camera, key: 'f0e1d2c3'),
      );

      await tester.tap(find.byKey(const Key('capture-document-shutter')));
      await tester.pumpAndSettle();
      await tester.tap(find.byKey(const Key('capture-document-retry')));
      await tester.pumpAndSettle();

      expect(verification.submissions.map((s) => s.key).toSet(), <String>{'f0e1d2c3'});
    });
  });

  group('what the screens refuse to say', () {
    testWidgets('a submitted document is “sent”, never “verified”', (tester) async {
      // `Docs/04` §4 makes the five outcomes an administrator's decision on the whole record, and
      // `profiles.Service.Decide` is reachable from no route a provider can call. A provider told
      // "verified" here would go looking for work they cannot bid on yet.
      final verification = FakeVerificationRepository();
      final camera = FakeCaptureCamera(bytes: photograph(width: 400, height: 300));

      await openCapture(tester, verification: verification, overrides: withCamera(camera));
      await tester.tap(find.byKey(const Key('capture-document-shutter')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('capture-document-submitted')), findsOneWidget);
      expect(find.textContaining('Verified'), findsNothing);
      expect(find.textContaining('verified'), findsNothing);
      expect(find.textContaining('approved'), findsNothing);
    });

    testWidgets('and the list shows a customer nothing, because it is not their surface',
        (tester) async {
      // `ProviderOnly` hides; the platform decides. `GET /v1/provider/verification/documents`
      // answers a customer `403 profiles_provider_only`, and a customer who deep-linked here would
      // otherwise read a screen asking them for a driver licence.
      await signInAs(tester, UserRole.customer, verification: FakeVerificationRepository());
      await tester.tap(find.byKey(const Key('new-job')));
      await tester.pumpAndSettle();

      expect(find.byKey(const Key('verification-documents-entry')), findsNothing);
    });
  });

  group('the camera refusing', () {
    testWidgets('says so, and leaves no shutter behind it', (tester) async {
      // What is offered instead is SHIP-81d's, and `verification_fallback_test.dart` is where the
      // route on is walked. This one holds the other half: the screen stops offering a camera that
      // will not open, rather than leaving a button that appears to do nothing.
      final camera = FakeCaptureCamera(problem: CameraProblem.refused);

      await openCapture(
        tester,
        verification: FakeVerificationRepository(),
        overrides: withCamera(camera),
      );

      expect(find.byKey(const Key('capture-document-blocked')), findsOneWidget);
      expect(find.byKey(const Key('capture-document-declined')), findsOneWidget);
      expect(find.byKey(const Key('capture-document-shutter')), findsNothing);
    });
  });
}
