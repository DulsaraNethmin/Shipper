// The Dart counterpart of the Go domain boundary lint (SHIP-11).
//
// `Docs/07` §2 and `Docs/10` §8.3 both say features do not import one another and that shared
// behaviour moves to `core/` or `shared/`. Neither document can enforce itself. The Go side
// learned this at SHIP-11 and put the lint in from the first commit rather than from the point
// the packages filled up, because a boundary is very nearly impossible to reintroduce once it
// has been crossed a dozen times — every crossing is somebody's working code.
//
// This runs as a test rather than as a separate lint because `flutter test` is already what CI
// runs (SHIP-21), and a check with its own invocation is a check that stops being invoked.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// The features `Docs/07` §2 draws, and no others.
///
/// Listing them makes an eighth feature a decision somebody recorded rather than a folder that
/// appeared — the same reasoning as `internal/boundaries` on the Go side, where a new package
/// under `internal/` fails the lint until it is classified.
const expectedFeatures = <String>{
  'identity',
  'profile',
  'fleet',
  'jobs',
  'bidding',
  'delivery',
  'notifications',
};

/// The `name:` from `pubspec.yaml`, which is the prefix of every absolute import of our own
/// code: `package:shipper/features/jobs/…`.
const packageName = 'shipper';

void main() {
  final featuresDir = Directory('lib/features');

  test('lib/features holds exactly the features Docs/07 §2 names', () {
    expect(
      featuresDir.existsSync(),
      isTrue,
      reason: 'lib/features is missing; the structure in Docs/07 §2 is the acceptance '
          'criterion for SHIP-17',
    );

    final actual = featuresDir
        .listSync()
        .whereType<Directory>()
        .map((d) => d.uri.pathSegments.where((s) => s.isNotEmpty).last)
        .toSet();

    expect(
      actual,
      equals(expectedFeatures),
      reason: 'A feature folder appeared or vanished. Docs/07 §2 draws the list, so change '
          'the document first — a feature nobody decided on is how the split stops meaning '
          'anything.',
    );
  });

  test('no feature imports another feature', () {
    final offences = <String>[];

    for (final file in _dartFilesUnder(featuresDir)) {
      final owner = _featureOf(file.path);
      if (owner == null) continue;

      for (final directive in _importsOf(file)) {
        final imported = _importedFeature(directive, fromFile: file.path);
        if (imported == null || imported == owner) continue;

        offences.add(
          '${file.path}\n'
          '      feature "$owner" imports feature "$imported"\n'
          '      via: $directive',
        );
      }
    }

    expect(
      offences,
      isEmpty,
      reason: '\n\nFeatures do not import one another (Docs/07 §2, Docs/10 §8.3).\n'
          'Move the shared behaviour to lib/core or lib/shared instead — that is the whole\n'
          'mechanism, and it is what keeps two people working in two features from\n'
          'silently coupling them.\n\n'
          '  ${offences.join('\n\n  ')}\n',
    );
  });
}

Iterable<File> _dartFilesUnder(Directory dir) {
  if (!dir.existsSync()) return const <File>[];
  return dir
      .listSync(recursive: true)
      .whereType<File>()
      .where((f) => f.path.endsWith('.dart'));
}

/// The feature a path under `lib/features/` belongs to, or null if it is not under one.
String? _featureOf(String path) {
  final segments = path.split(Platform.pathSeparator);
  final index = segments.indexOf('features');
  if (index == -1 || index + 1 >= segments.length) return null;
  // A file directly inside lib/features/ belongs to no feature.
  if (index + 2 > segments.length - 1) return null;
  return segments[index + 1];
}

/// Every `import`, `export` and `part` target in a file.
///
/// Comments are stripped first, so a doc comment mentioning `features/jobs/…` — and several
/// of them do — cannot be mistaken for a directive. Directives may only appear at the top of a
/// file, but scanning the whole of it costs nothing and cannot be defeated by formatting.
Iterable<String> _importsOf(File file) {
  final source = _withoutComments(file.readAsStringSync());
  final directive = RegExp(
    r'''^\s*(?:import|export|part)\s+(?:'([^']+)'|"([^"]+)")''',
    multiLine: true,
  );

  return directive
      .allMatches(source)
      .map((m) => m.group(1) ?? m.group(2))
      .whereType<String>();
}

String _withoutComments(String source) {
  return source
      .replaceAll(RegExp(r'/\*.*?\*/', dotAll: true), '')
      .replaceAll(RegExp(r'//.*$', multiLine: true), '');
}

/// The feature a directive target names, resolving both forms:
///
/// - `package:shipper/features/jobs/…` — absolute, the common case.
/// - `../../jobs/…` — relative, and the one a boundary check that only looked at `package:`
///   URIs would miss entirely. It is also the form an IDE's "add import" produces from inside
///   a sibling folder, which is exactly where the mistake gets made.
String? _importedFeature(String target, {required String fromFile}) {
  if (target.startsWith('dart:')) return null;

  String? path;

  if (target.startsWith('package:')) {
    const prefix = 'package:$packageName/';
    if (!target.startsWith(prefix)) return null; // A third-party package.
    path = 'lib/${target.substring(prefix.length)}';
  } else if (!target.contains(':')) {
    // Relative to the importing file's directory. Normalising is what turns `../../jobs/x`
    // into `lib/features/jobs/x`.
    final from = File(fromFile).parent.uri;
    path = from.resolve(target).toFilePath();
  } else {
    return null;
  }

  return _featureOf(path);
}
