// Keeps the platform manifests and the in-app copy from drifting apart (SHIP-179).
//
// The purpose string iOS renders lives in Info.plist; the words the app itself shows live in
// Dart. Nothing connects them, so one gets reworded and the other does not — and the mismatch
// is invisible until a reviewer reads both, which on iOS is exactly the person who can reject
// the build. Docs/07 §7 records that every item on the store-compliance list has already
// blocked a real submission.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/permissions/permission_copy.dart';

const _infoPlist = 'ios/Runner/Info.plist';
const _androidManifest = 'android/app/src/main/AndroidManifest.xml';

void main() {
  group('iOS', () {
    test('declares a camera purpose string', () {
      expect(
        _plistString('NSCameraUsageDescription'),
        isNotNull,
        reason: 'Apple rejects a build that asks for the camera without one',
      );
    });

    test('renders exactly the words the app promises', () {
      expect(_plistString('NSCameraUsageDescription'), PermissionCopy.cameraPurpose);
    });

    test('the purpose string says what is captured and what happens without it', () {
      // "Shipper needs access to your camera" is the string Apple rejects. A reviewer has to be
      // able to weigh the request, and Docs/01 §4.4 means declining is a route through the job
      // rather than the end of it — so the string has to mention the alternative.
      final purpose = _plistString('NSCameraUsageDescription')!;

      expect(purpose.length, greaterThan(60), reason: 'too vague to be assessable');
      expect(purpose.toLowerCase(), contains('proof'));
      expect(purpose.toLowerCase(), contains('reason instead'));
    });
  });

  group('Android', () {
    late final String manifest;

    setUpAll(() => manifest = File(_androidManifest).readAsStringSync());

    test('declares CAMERA and POST_NOTIFICATIONS', () {
      expect(manifest, contains('android.permission.CAMERA'));
      expect(manifest, contains('android.permission.POST_NOTIFICATIONS'));
    });

    test('does not make a camera a hardware requirement', () {
      // Declaring CAMERA implies <uses-feature android:required="true"> unless it is said
      // otherwise, and Google Play then hides the app from devices without a camera. The app
      // works without one, because Docs/01 §4.4 allows a recorded exception reason.
      expect(
        manifest,
        contains(RegExp(
          r'android:name="android\.hardware\.camera"\s+android:required="false"',
        )),
      );
    });

    test('asks for nothing at run time that has no copy explaining it', () {
      // Android's own prompt is a fixed sentence, so every *runtime* permission needs in-app
      // copy or the user is asked a question with no context. Adding one without writing its
      // rationale fails here.
      final requested = RegExp(r'android\.permission\.([A-Z_]+)')
          .allMatches(manifest)
          .map((m) => m.group(1)!)
          .toSet();

      // Install-time permissions are granted without a prompt, so there is no moment at which
      // copy could be shown. They are listed rather than pattern-matched, so that a new one is
      // a decision somebody made here.
      const installTime = {'INTERNET'};
      const explained = {'CAMERA', 'POST_NOTIFICATIONS'};

      expect(
        requested.difference(explained).difference(installTime),
        isEmpty,
        reason: 'a permission was added without rationale copy in PermissionCopy',
      );
    });
  });

  group('the copy itself', () {
    test('every string tells the user what happens if they decline', () {
      // Docs/01 §4.4 and Docs/07 §5 both guarantee the app still works. A prompt that reads as
      // an ultimatum is refused more often, and the refusal has to be handled either way.
      expect(PermissionCopy.cameraPurpose, contains('If you cannot take a photo'));
      expect(PermissionCopy.cameraDeclined, contains('finish the delivery'));
      expect(PermissionCopy.notificationsDeclined, contains('still shows'));
    });

    test('the notification rationale states what is kept off a locked screen', () {
      // Docs/05 §4 keeps addresses, goods descriptions and full names out of notification
      // content. Saying so is what makes the permission worth granting.
      expect(PermissionCopy.notificationsPurpose, contains('locked screen'));
      expect(PermissionCopy.notificationsPurpose, contains('addresses'));
    });

    test('is written in Australian English', () {
      // scripts/check-spelling.sh covers the words it knows; this covers the one that matters
      // most here, since a permission prompt is copy a customer reads.
      for (final copy in [
        PermissionCopy.cameraPurpose,
        PermissionCopy.cameraDeclined,
        PermissionCopy.notificationsTitle,
        PermissionCopy.notificationsPurpose,
        PermissionCopy.notificationsDeclined,
      ]) {
        expect(copy.toLowerCase(), isNot(contains('authorize')));
        expect(copy.toLowerCase(), isNot(contains('canceled')));
        expect(copy.toLowerCase(), isNot(contains('customize')));
      }
    });
  });
}

/// Reads a `<key>…</key><string>…</string>` pair out of the plist.
///
/// A regex rather than a plist parser: the file is a fixed, hand-maintained shape, and pulling
/// in a dependency to read one key from it would be a strange trade.
String? _plistString(String key) {
  final source = File(_infoPlist).readAsStringSync();
  final match = RegExp(
    '<key>$key</key>\\s*<string>(.*?)</string>',
    dotAll: true,
  ).firstMatch(source);

  return match?.group(1);
}
