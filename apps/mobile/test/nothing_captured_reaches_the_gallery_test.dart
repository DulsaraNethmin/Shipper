// SHIP-130's last clause — "…and **never written to the photo library**" — and `Docs/04` §3.1's
// second, which says the same thing about a different photograph and says why it matters more:
// *"Verification images must **not** be written to the device photo library… Identity documents
// sitting in a camera roll are a privacy exposure the platform cannot control or revoke."*
//
// # Why this file is at the root of the test tree rather than under a feature
//
// It was `test/features/delivery/proof_never_reaches_the_gallery_test.dart`, which was right while
// proof of delivery was the only thing this application photographed. SHIP-81c gave it a second
// caller, and **every assertion in this file was already whole-client rather than per-feature** —
// `_sourceFiles()` walks all of `lib/`, all of `ios/Runner` and all of `android/app/src`, the
// pubspec check reads the one pubspec there is, and the two manifest checks read the one manifest
// each platform has. So the file moved to sit beside `architecture_test.dart`, which is the other
// guard that is about the build rather than about a feature.
//
// **That the assertions were already whole-client is a measurement rather than a hope**: the move
// changed no `Directory` and no glob in this file. What it did change is the one line that named a
// source file as a string literal — see the last test, which no longer does.
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
// The fourth guard is in `test/core/capture/captured_image_test.dart`: `CapturedImageStore.write`
// takes a **name and not a path**, and its directory is a closed `CaptureFolder` rather than a
// string, so there is no argument that could name `DCIM/Camera` even if the permissions were there.
//
// # The mutation this file exists to catch
//
// Swapping `camera` for `image_picker`. It is one line shorter, it looks identical from Dart, and on
// Android it hands the capture to whichever camera application the manufacturer shipped — several of
// which write a copy into `DCIM/Camera` regardless of the output the caller asked for.
// `core/capture/capture_camera.dart` argues it; this is what fails when somebody does it anyway.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// Packages whose whole purpose is to put an image in the device's gallery, plus the two that
/// reach one on the way to doing something else.
///
/// **Adding anything to this list is a decision about SHIP-130's and SHIP-81c's *Done when*.** There
/// is no allow-list beside it on purpose: the answer is not "which file may use it".
const _forbiddenPackages = <String, String>{
  'gal': 'saves images to the gallery',
  'image_gallery_saver': 'saves images to the gallery',
  'image_gallery_saver_plus': 'saves images to the gallery',
  'photo_manager': 'reads and writes the device photo library',
  'saver_gallery': 'saves images to the gallery',
  'image_picker': 'delegates capture to the platform camera app, which may keep its own copy',
  'flutter_image_gallery_saver': 'saves images to the gallery',
  // Added by SHIP-81d, which needed a file picker and had to choose between two packages whose
  // names suggest the same thing. `file_picker`'s image, video and media modes present the **photo**
  // picker on iOS rather than the document picker, so a build could reach the photo library through
  // a package named for files — and the person who reached for it would have had no reason to look.
  // `file_selector` is what this client depends on instead, and `pubspec.yaml` argues the choice at
  // length beside the dependency.
  'file_picker': 'its image and media modes present the iOS photo picker, not the document picker',
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
      // A dependency line, not a mention: the reasoning for *not* depending on `image_picker` or on
      // `file_picker` is written down in `capture_camera.dart` and in `pubspec.yaml`, and must not
      // fail its own test.
      if (RegExp('^\\s{2}${RegExp.escape(entry.key)}:', multiLine: true).hasMatch(pubspec)) {
        offences.add('${entry.key} — ${entry.value}');
      }
    }

    expect(
      offences,
      isEmpty,
      reason: '\n\nSHIP-130 and SHIP-81c: a captured photograph is never written to the photo '
          'library.\n'
          'These dependencies would put one there, or hand the capture to something that might:\n\n'
          '  ${offences.join('\n  ')}\n\n'
          'core/capture/capture_camera.dart argues why `camera` was chosen over `image_picker`,\n'
          'and pubspec.yaml argues why `file_selector` was chosen over `file_picker`. If a\n'
          'decision has genuinely changed, change it there first.\n',
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
      reason: '\n\nSHIP-130 and SHIP-81c: a captured photograph is never written to the photo '
          'library.\n\n'
          '  ${offences.join('\n  ')}\n\n'
          'Comments are stripped before the search, so explaining the rule is free and doing\n'
          'it is not.\n',
    );
  });

  test('the Android manifest declares no permission that could reach the gallery', () {
    // The guard that holds whatever the Dart does. Without one of these an application cannot write
    // to the shared media collections on Android 10 and later, so this is the platform enforcing the
    // *Done when* rather than this repository hoping for it.
    //
    // **It is also what makes SHIP-81d's fallback a document picker rather than a gallery one.**
    // `file_selector` reaches the file system through `ACTION_OPEN_DOCUMENT`, which grants a URI for
    // the one file the user chose and needs none of the permissions below. A package that enumerated
    // the photo library would need `READ_MEDIA_IMAGES` and would fail here.
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
          reason: '${manifest.path} declares $permission, which is what would let a captured '
              'photograph reach DCIM. SHIP-130 and SHIP-81c both require that it cannot.',
        );
      }
    }
  });

  test('Info.plist declares no photo-library usage string', () {
    // The same guard on the other platform. `PHPhotoLibrary` refuses without one, and App Review
    // rejects a binary that calls the API and does not declare it — so its absence is both the
    // enforcement and the statement to the reviewer.
    //
    // It is also the iOS half of SHIP-81d's argument: `UIDocumentPickerViewController` needs no
    // usage string at all, because the user picks the file in a system UI this application never
    // sees. A photo picker would need `NSPhotoLibraryUsageDescription` and would fail here.
    final plist = File('ios/Runner/Info.plist').readAsStringSync();

    for (final key in _forbiddenPlistKeys) {
      expect(
        plist.contains(key),
        isFalse,
        reason: 'ios/Runner/Info.plist declares $key. SHIP-130 and SHIP-81c require that a '
            'captured photograph never reaches the photo library, and this is what would let one.',
      );
    }

    expect(
      plist.contains('NSCameraUsageDescription'),
      isTrue,
      reason: 'and the camera string is still there — SHIP-179 wrote it, SHIP-81c widened it to '
          'cover verification documents, and iOS renders it verbatim',
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

    // **Found rather than named, and SHIP-81c is why.** This assertion used to read the file at
    // `lib/features/delivery/proof_camera.dart`, as a string literal — and a path in a string
    // literal is a path no refactoring tool follows. The move to `core/capture/` would have left it
    // pointing at nothing, and `File.readAsStringSync` on a missing path throws rather than
    // silently passing, so this one would have failed loudly. **The next one might not**: a second
    // `CameraController` added in a file this literal did not name would have been invisible.
    //
    // So the rule is now over every file that opens a camera, whichever folder it is in and however
    // many there are. `isNotEmpty` is the half that matters — a guard over an empty set is green.
    final openers = _sourceFiles()
        .where((file) => file.path.endsWith('.dart'))
        .map((file) => MapEntry(file.path, _withoutComments(file.readAsStringSync())))
        .where((entry) => entry.value.contains('CameraController('))
        .toList();

    expect(
      openers,
      isNotEmpty,
      reason: 'nothing in lib/ constructs a CameraController any more. Either the camera was '
          'removed — in which case SHIP-130 needs rereading — or this guard has stopped looking '
          'where the code is.',
    );

    for (final opener in openers) {
      expect(
        opener.value,
        contains('enableAudio: false'),
        reason: '${opener.key} opens a camera without saying enableAudio: false. That is the line '
            'that keeps both of the above true.',
      );
    }

    // **Comments stripped**, and a mutation run is why. The first version of this assertion read
    // the file whole, and `enableAudio: false` also appears in that library's doc comment — so
    // flipping the argument to `true` left the sentence behind and the guard passed. A rule that a
    // comment can satisfy is a rule about documentation.
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
