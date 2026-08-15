// SHIP-143 as `main.dart` actually wires it.
//
// Source assertions rather than calls, for the reason `sync_wiring_test.dart` and
// `version_gate_wiring_test.dart` are: `main()` calls `runApp` and reaches platform channels, which
// a host test cannot do. What can be checked is that the two lines are there at all — and the
// absence of either is a build that registers nothing, deregisters nothing, logs nothing, and fails
// no test. The failure is discovered the day somebody wonders why no notification has ever arrived.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  final source = File('lib/main.dart').readAsStringSync();

  test('main.dart reads the registrar, which is what installs its session listener', () {
    // Riverpod does not build a provider nobody has read, and the registration half of this ticket
    // is a `ref.listen` in the provider's body. Without this line the listener does not exist.
    expect(
      source,
      contains('container.read(pushRegistrarProvider)'),
      reason: 'lib/main.dart no longer reads pushRegistrarProvider. Its body is where the session '
          'listener is installed, so an unread provider is a build that never registers for push '
          '— see features/notifications/push_registration.dart.',
    );
  });

  test('main.dart supplies the sign-out hook, which is the only way to deregister', () {
    // It cannot be a listener on the session: `signOut` clears the access token before publishing
    // the state, so a request fired from a listener carries no credential. The hook is handed the
    // spent token, and `core/auth` declares the shape without knowing what fills it.
    expect(
      source,
      contains('signOutHooksProvider.overrideWith'),
      reason: 'lib/main.dart no longer supplies signOutHooksProvider. Nothing else can deregister '
          'this handset — the session has forgotten its access token by the time the signed-out '
          'state is published.',
    );
    expect(source, contains('pushDeregistrationHook'));
  });

  test('nothing supplies a push token source, and that is the recorded state', () {
    // **An assertion about an absence, and it is here so the absence stays a decision.** No
    // Firebase project exists and no ticket anywhere creates one, so `firebase_messaging` is not a
    // dependency and `pushTokenSourceProvider` is empty — which means this build registers nothing
    // on a device. When the project exists, this test is what tells whoever wires it up that they
    // have found the one line to change; delete it in the same commit.
    expect(
      source,
      isNot(contains('pushTokenSourceProvider')),
      reason: 'a token source is now supplied — good. Update Docs/11 §3 and remove this test in '
          'the same change, because push registration is no longer inert.',
    );

    final pubspec = File('pubspec.yaml').readAsStringSync();
    expect(
      pubspec,
      isNot(contains('firebase')),
      reason: 'firebase_messaging is now a dependency. It needs google-services.json and '
          'GoogleService-Info.plist, which no ticket creates — check make flutter-build still '
          'passes, then remove this test.',
    );
  });
}
