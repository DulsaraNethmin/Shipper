// SHIP-48 — "and never in preferences", enforced rather than intended.
//
// token_store_test.dart proves the token reaches secure storage today. This proves the other
// half of the sentence, which is a property of the whole client rather than of one class:
// there is no second place the token could go, because nothing that could store it insecurely
// is reachable from `lib/` at all.
//
// **This is the test that fails if somebody swaps the implementation for shared_preferences.**
// Not "fails eventually, in a way somebody notices" — the package is not in the lockfile, so
// the swap needs `pub add`, and adding it fails the second assertion below before a line of it
// is written. The reason for going that far is in Docs/06 §5.2 and Docs/07 §3: preferences are
// a plain XML file on Android and a plist on iOS, both readable on a rooted or jailbroken
// device and both present in unencrypted backups, and a refresh token is a credential that
// stays useful until it is used.
//
// The same shape as architecture_test.dart, and for the same reason: a rule two documents state
// and nothing checks is a rule that lasts until the first person who has not read them.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

/// Names that mean the token has left secure storage.
///
/// Deliberately short. Each of these has no legitimate use anywhere in this client — the
/// preferences store and the two native APIs behind it — so the rule needs no exceptions and
/// will not acquire any. A broader list covering, say, every route to a file on disk would have
/// to be relaxed at SHIP-124, which puts the offline queue in a Drift database on purpose, and
/// a rule that gets relaxed once stops being read as a rule.
const _neverInLib = <String>[
  'shared_preferences',
  'SharedPreferences',
  'NSUserDefaults',
  'UserDefaults',
];

/// The application documents directory is the third location `Docs/07` §3 names, and this is
/// where it is kept out of: the folder holding the token may not open a file at all.
const _neverInAuth = <String>[
  'dart:io',
  'path_provider',
];

/// The one package allowed to hold the token, and the one folder allowed to talk to it.
const _secureStoragePackage = 'package:flutter_secure_storage/';
const _theOnlyFolderThatStores = 'lib/core/auth/';

void main() {
  test('no preferences store is reachable from lib/', () {
    final offences = <String>[];

    for (final file in _dartFilesUnder(Directory('lib'))) {
      // Comments are stripped first. A doc comment naming shared_preferences in order to say
      // the token does not go there is the correct thing to write, and flagging it would teach
      // people to stop explaining themselves.
      final source = _withoutComments(file.readAsStringSync());

      for (final name in _neverInLib) {
        if (source.contains(name)) offences.add('${file.path}: $name');
      }
    }

    expect(
      offences,
      isEmpty,
      reason: '\n\nDocs/07 §3: tokens are never written to application preferences,\n'
          'application documents, logs, or crash reports. The refresh token lives in the\n'
          'iOS Keychain and the Android Keystore, through lib/core/auth/token_store.dart\n'
          'and nowhere else.\n\n'
          '  ${offences.join('\n  ')}\n',
    );
  });

  test('the preferences package is not even in the lockfile', () {
    // Resolved dependencies, direct and transitive. Nothing in the client's tree pulls it in
    // today, so this is the assertion that turns "we chose not to" into "it is not there" —
    // and it fails on `flutter pub add`, before any code exists to review.
    final lock = File('pubspec.lock').readAsStringSync();

    expect(
      lock.contains('shared_preferences'),
      isFalse,
      reason: 'shared_preferences entered the dependency tree. If some package genuinely '
          'needs it, say so here and keep the lib/ scan — but the refresh token still goes '
          'in the Keychain and the Keystore (Docs/07 §3).',
    );
  });

  test('exactly one folder imports flutter_secure_storage', () {
    final importers = <String>{};

    for (final file in _dartFilesUnder(Directory('lib'))) {
      final source = _withoutComments(file.readAsStringSync());
      if (source.contains(_secureStoragePackage)) importers.add(file.path);
    }

    expect(
      importers,
      isNotEmpty,
      reason: 'Nothing imports flutter_secure_storage. The token store is meant to use it '
          '(Docs/07 §3) — if this fails, the store has been replaced by something else.',
    );

    expect(
      importers.where((p) => !p.startsWith(_theOnlyFolderThatStores)),
      isEmpty,
      reason: '\n\nSecure storage is reached through lib/core/auth/ alone. A second caller is\n'
          'a second set of Keychain and Keystore options, and the options are the guarantee —\n'
          'see SecureTokenStore.iosOptions.\n',
    );
  });

  test('the folder holding the token cannot write a file', () {
    final offences = <String>[];

    for (final file in _dartFilesUnder(Directory(_theOnlyFolderThatStores))) {
      final source = _withoutComments(file.readAsStringSync());

      for (final name in _neverInAuth) {
        if (source.contains(name)) offences.add('${file.path}: $name');
      }
    }

    expect(
      offences,
      isEmpty,
      reason: '\n\nNothing in lib/core/auth/ opens a file. Docs/07 §3 rules out the\n'
          'application documents directory as firmly as it rules out preferences, and the\n'
          'shortest route there is a token written to a cache "just while it syncs".\n\n'
          '  ${offences.join('\n  ')}\n',
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

String _withoutComments(String source) {
  return source
      .replaceAll(RegExp(r'/\*.*?\*/', dotAll: true), '')
      .replaceAll(RegExp(r'//.*$', multiLine: true), '');
}
