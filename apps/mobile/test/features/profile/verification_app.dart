// The application, signed in as a provider, standing on their verification documents (SHIP-81c).
//
// **The screens are reached the way a person reaches them — by tapping** — and not by pumping a
// widget. Two things only that arrangement can catch, both of which have already cost this
// repository a run:
//
//   1. A route missing from `_signedInLocations` or `_signedInPatterns` is silently redirected to
//      the home shell, which from the outside is **indistinguishable from a button that does
//      nothing**. `negotiation_app.dart` records the run SHIP-102 lost to exactly that.
//   2. A screen pumped directly gets a private `ProviderScope`, so a test that passes against one
//      has not tested the application's wiring — which is `main.dart`'s own note.

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/misc.dart' show Override;
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/capture/capture_camera.dart';
import 'package:shipper/core/capture/capture_providers.dart';
import 'package:shipper/core/capture/captured_image.dart';
import 'package:shipper/features/profile/verification_document.dart';
import 'package:shipper/features/profile/verification_documents_controller.dart';

import '../../core/auth/session_fixtures.dart';
import '../../support/capture_fixture.dart';
import '../identity/fake_identity_repository.dart';
import '../identity/signup_app.dart';
import 'fake_verification_repository.dart';

/// Signs [role] in and stops at the signed-in shell.
///
/// The role travels the way it travels in the application: as a claim in the access token the
/// sign-in answered with (SHIP-50, SHIP-52). Nothing here sets a role directly, because nothing in
/// the app can.
Future<void> signInAs(
  WidgetTester tester,
  UserRole role, {
  FakeVerificationRepository? verification,
  List<Override> overrides = const <Override>[],
}) async {
  // A phone-shaped surface rather than the 800×600 default, and a tall one: four document cards and
  // a preamble should scroll rather than be reported as overflowing.
  tester.view.physicalSize = const Size(800, 2400);
  tester.view.devicePixelRatio = 1;
  addTearDown(tester.view.reset);

  final identity = FakeIdentityRepository()..tokens = aTokenPair(role: role);

  await tester.pumpWidget(
    signupApp(identity, verification: verification, extra: overrides),
  );
  await tester.pumpAndSettle();

  await signInThrough(tester);
  await tester.pumpAndSettle();
}

/// Signs a provider in and walks them to their documents through the button on the feed.
Future<void> openDocuments(
  WidgetTester tester, {
  required FakeVerificationRepository verification,
  List<Override> overrides = const <Override>[],
}) async {
  await signInAs(tester, UserRole.provider, verification: verification, overrides: overrides);

  await tester.tap(find.byKey(const Key('verification-documents-entry')));
  await tester.pumpAndSettle();
}

/// Walks on to the camera for one kind, from the card on the list.
Future<void> openCapture(
  WidgetTester tester, {
  required FakeVerificationRepository verification,
  VerificationDocumentKind kind = VerificationDocumentKind.licence,
  List<Override> overrides = const <Override>[],
}) async {
  await openDocuments(tester, verification: verification, overrides: overrides);

  await tester.tap(find.byKey(Key('verification-document-${kind.wire}-capture')));
  await tester.pumpAndSettle();
}

/// The overrides that put one fake device behind the capture screen.
///
/// The compressor is substituted for the **synchronous** one, which is the same code: the
/// application runs it through `Isolate.run` so a decode does not freeze the shutter, and a widget
/// test that spawned an isolate per capture would be testing Dart's isolates. What is replaced is
/// where it runs and not what it does — so every assertion about the compressed image is an
/// assertion about the real compressor.
List<Override> withCamera(FakeCaptureCamera camera, {String key = 'c7d10f22'}) {
  return <Override>[
    captureCameraProvider.overrideWithValue(() => camera),
    captureCompressorProvider.overrideWithValue((bytes) async => compressCaptureSync(bytes)),
    idempotencyKeyMintProvider.overrideWithValue(() => key),
  ];
}
