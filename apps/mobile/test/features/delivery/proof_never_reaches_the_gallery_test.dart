// SHIP-130's last clause: "…and **never written to the photo library**."
//
// # Proving a negative, from the two places that can actually enforce one
//
// No host test can watch `MediaStore` or `PHPhotoLibrary`, and a test that captured a photograph and
// then looked at the gallery would need a device, a granted permission, and a gallery that was empty
// to begin with. What *can* be proved is stronger than that anyway, because it holds for every code
// path rather than for the one the test walked:
//
//   1. **Android cannot.** Under scoped storage an application with no `WRITE_EXTERNAL_STORAGE` and
//      no `READ_MEDIA_IMAGES` cannot write to the shared media collections whatever code it runs.
//      The manifest declares neither, and this file fails if one appears.
//   2. **iOS cannot.** `PHPhotoLibrary` writes require `NSPhotoLibraryAddUsageDescription`, and the
//      App Store rejects a binary that calls the API without one. `Info.plist` declares neither
//      photo-library key, and this file fails if one appears.
//   3. **Nothing in this client asks.** No package that writes to a gallery is a dependency, and no
//      symbol that writes to one appears in `lib/`.
//
// The fourth guard is in `proof_image_test.dart`: `ProofStore.write` takes a **name and not a
// path**, so there is no argument that could name `DCIM/Camera` even if the permissions were there.
//
// # The mutation this file exists to catch
//
// Swapping `camera` for `image_picker`. It is one line shorter, it looks identical from Dart, and on
// Android it hands the capture to whichever camera application the manufacturer shipped — several of
// which write a copy into `DCIM/Camera` regardless of the output the caller asked for. `proof_camera.dart`
// argues it; this is what fails when somebody does it anyway.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// Packages whose whole purpose is to put an image in the device's gallery, plus the one that
/// delegates capture to an application that might.
///
/// **Adding anything to this list is a decision about SHIP-130's *Done when*.** There is no
/// allow-list beside it on purpose: the answer is not "which file may use it".
const _forbiddenPackages = <String, String>{
  'gal': 'saves images to the gallery',
  'image_gallery_saver': 'saves images to the gallery',
  'image_gallery_saver_plus': 'saves images to the gallery',
  'photo_manager': 'reads and writes the device photo library',
  'saver_gallery': 'saves images to the gallery',
  'image_picker': 'delegates capture to the platform camera app, which may keep its own copy',
  'flutter_image_gallery_saver': 'saves images to the gallery',
};

/// Symbols that write to a shared photo library, in any of the three languages this app is built
/// from. Searched as source text, because a plugin can be reached by channel name alone.
const _forbiddenSymbols = <String>[
  'UIImageWriteToSavedPhotosAlbum',
  'PHPhotoLibrary',
  'PHAssetChangeRequest',
  'MediaStore.Images',
  'ACTION_IMAGE_CAPTURE',
  'ImageSource.gallery',
  'ImageSource.camera',
  'saveImageToGallery',
  'GallerySaver',
];

/// Permissions that would let this application write to Android's shared media collections.
const _forbiddenAndroidPermissions = <String>[
  'android.permission.WRITE_EXTERNAL_STORAGE',
  'android.permission.READ_EXTERNAL_STORAGE',
  'android.permission.READ_MEDIA_IMAGES',
  'android.permission.ACCESS_MEDIA_LOCATION',
  'android.permission.MANAGE_EXTERNAL_STORAGE',
];

/// `Info.plist` keys that would let this application write to, or read, the iOS photo library.
const _forbiddenPlistKeys = <String>[
  'NSPhotoLibraryAddUsageDescription',
  'NSPhotoLibraryUsageDescription',
];

void main() {
  test('no dependency of this client writes to a photo library', () {
    final pubspec = File('pubspec.yaml').readAsStringSync();
    final offences = <String>[];

    for (final entry in _forbiddenPackages.entries) {
      // A dependency line, not a mention: the reasoning for *not* depending on `image_picker` is
      // written down in proof_camera.dart and must not fail its own test.
      if (RegExp('^\\s{2}${RegExp.escape(entry.key)}:', multiLine: true).hasMatch(pubspec)) {
        offences.add('${entry.key} — ${entry.value}');
      }
    }

    expect(
      offences,
      isEmpty,
      reason: '\n\nSHIP-130: a proof photograph is never written to the photo library.\n'
          'These dependencies would put one there, or hand the capture to something that might:\n\n'
          '  ${offences.join('\n  ')}\n\n'
          'proof_camera.dart argues why `camera` was chosen over `image_picker` and what the\n'
          'difference costs. If the decision has genuinely changed, change it there first.\n',
    );
  });

  test('no source file in this client names a photo-library API', () {
    final offences = <String>[];

    for (final file in _sourceFiles()) {
      final source = _withoutComments(file.readAsStringSync());

      for (final symbol in _forbiddenSymbols) {
        if (source.contains(symbol)) offences.add('${file.path} names $symbol');
      }

      // **The import, as well as the symbol.** A mutation run added `import
      // 'package:image_picker/image_picker.dart';` to a file and the pubspec check did not see it —
      // `flutter pub add` had not been run, so the dependency was not declared and the sweep passed.
      // That is a real gap rather than a contrived one: an import lands in a diff before the
      // pubspec entry does, and a reviewer reading the Dart is the person most likely to be looking.
      for (final package in _forbiddenPackages.keys) {
        if (source.contains('package:$package/')) {
          offences.add('${file.path} imports package:$package — ${_forbiddenPackages[package]}');
        }
      }
    }

    expect(
      offences,
      isEmpty,
      reason: '\n\nSHIP-130: a proof photograph is never written to the photo library.\n\n'
          '  ${offences.join('\n  ')}\n\n'
          'Comments are stripped before the search, so explaining the rule is free and doing\n'
          'it is not.\n',
    );
  });

  test('the Android manifest declares no permission that could reach the gallery', () {
    // The guard that holds whatever the Dart does. Without one of these an application cannot write
    // to the shared media collections on Android 10 and later, so this is the platform enforcing the
    // *Done when* rather than this repository hoping for it.
    final manifests = <File>[
      File('android/app/src/main/AndroidManifest.xml'),
      File('android/app/src/debug/AndroidManifest.xml'),
      File('android/app/src/profile/AndroidManifest.xml'),
    ].where((file) => file.existsSync());

    expect(manifests, isNotEmpty, reason: 'the main manifest is missing');

    for (final manifest in manifests) {
      final source = manifest.readAsStringSync();
      for (final permission in _forbiddenAndroidPermissions) {
        expect(
          source.contains(permission),
          isFalse,
          reason: '${manifest.path} declares $permission, which is what would let a proof '
              'photograph reach DCIM. SHIP-130 requires that it cannot.',
        );
      }
    }
  });

  test('Info.plist declares no photo-library usage string', () {
    // The same guard on the other platform. `PHPhotoLibrary` refuses without one, and App Review
    // rejects a binary that calls the API and does not declare it — so its absence is both the
    // enforcement and the statement to the reviewer.
    final plist = File('ios/Runner/Info.plist').readAsStringSync();

    for (final key in _forbiddenPlistKeys) {
      expect(
        plist.contains(key),
        isFalse,
        reason: 'ios/Runner/Info.plist declares $key. SHIP-130 requires that a proof photograph '
            'never reaches the photo library, and this is what would let one.',
      );
    }

    expect(
      plist.contains('NSCameraUsageDescription'),
      isTrue,
      reason: 'and the camera string is still there — SHIP-179 wrote it and iOS renders it verbatim',
    );
  });

  test('and no microphone string either, because the camera is opened without audio', () {
    // `camera`'s README asks for NSMicrophoneUsageDescription beside the camera one. This build
    // passes `enableAudio: false`, so no microphone is ever opened — and a purpose string for a
    // microphone the application does not use is worse than its absence, to a reviewer and to the
    // person reading the prompt. Docs/11 §3 carries this as the one store risk SHIP-130 takes.
    expect(File('ios/Runner/Info.plist').readAsStringSync(), isNot(contains('NSMicrophone')));
    expect(
      File('android/app/src/main/AndroidManifest.xml').readAsStringSync(),
      isNot(contains('RECORD_AUDIO')),
    );
    // **Comments stripped**, and a mutation run is why. The first version of this assertion read
    // the file whole, and `enableAudio: false` also appears in that library's doc comment — so
    // flipping the argument to `true` left the sentence behind and the guard passed. A rule that a
    // comment can satisfy is a rule about documentation.
    expect(
      _withoutComments(File('lib/features/delivery/proof_camera.dart').readAsStringSync()),
      contains('enableAudio: false'),
      reason: 'this is the line that keeps both of the above true',
    );
  });
}

/// Every file this application is built from that could name one of the symbols above.
Iterable<File> _sourceFiles() sync* {
  for (final directory in <String>['lib', 'ios/Runner', 'android/app/src']) {
    final dir = Directory(directory);
    if (!dir.existsSync()) continue;

    yield* dir
        .listSync(recursive: true)
        .whereType<File>()
        .where((file) => const <String>['.dart', '.swift', '.m', '.h', '.kt', '.java']
            .any((extension) => file.path.endsWith(extension)));
  }
}

String _withoutComments(String source) {
  return source
      .replaceAll(RegExp(r'/\*.*?\*/', dotAll: true), '')
      .replaceAll(RegExp(r'//.*$', multiLine: true), '');
}
