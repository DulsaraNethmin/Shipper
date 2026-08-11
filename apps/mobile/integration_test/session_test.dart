// SHIP-48, on a device.
//
// Everything under test/ runs on the host, where there is no Keychain and no Keystore. Those
// tests prove the right call is made with the right options — which is worth proving, and is
// not the same claim as "the refresh token persists in Keychain and Keystore". This file makes
// that claim, by running the production `SecureTokenStore` against the real platform.
//
//     make flutter-integration d=<device>
//
// Deliberately not in `make flutter-check`, and not in CHECKS. It needs a booted simulator, and
// the Flutter CI job runs on Linux — Docs/08 Step 1 puts macOS runners at roughly ten times the
// cost, and nothing else in this track needs one. A check that cannot run where CI runs has to
// be invoked by a person; this one is invoked when the storage or the session changes.
//
// # The last two tests are a pair, and on iOS they are run separately
//
// One process proves the token reaches the platform. It does not prove the token *survives* the
// process, which is the word "persists" in the acceptance criterion. So the last two also run
// as two invocations against the same build:
//
//     make flutter-integration d=<ios-simulator-udid> only="leaves it for the next launch"
//     make flutter-integration d=<ios-simulator-udid> only="left by a previous launch"
//
// The second is a new process reading what the first one wrote. That is why no `setUp` clears
// the store here and each test arranges its own state instead — a shared teardown would erase
// exactly the thing the pair exists to observe.
//
// **The same two invocations do not demonstrate anything on Android**, and the second one fails
// there: `flutter test` reinstalls the APK, and the encrypted entry does not survive the
// reinstall. Android has a better route to the same claim — install the debug build once, then:
//
//     adb shell am force-stop au.com.shipper && adb shell am start -n au.com.shipper/.MainActivity
//     adb shell run-as au.com.shipper cat /data/data/au.com.shipper/shared_prefs/FlutterSecureStorage.xml
//
// The relaunch is a real cold start of a process that has exited, and the second command shows
// the stored value is ciphertext under a Keystore-wrapped key rather than the token.
//
// Run as a whole file, both tests pass on both platforms — the write is simply in the same
// process as the read. That is a weaker claim, and the header is here so nobody reads it as the
// stronger one.

import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';
import 'package:shipper/core/auth/token_store.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  // The production store, with the production options. Substituting anything here would put the
  // test back where the host tests already are.
  const store = SecureTokenStore();
  const token = 'integration-refresh-token';

  testWidgets('the refresh token round trips through the real platform store', (tester) async {
    await store.clear();

    await store.writeRefreshToken(token);

    expect(await store.readRefreshToken(), token);
    await store.clear();
  });

  testWidgets('a second store instance reads what the first one wrote', (tester) async {
    await store.clear();
    await store.writeRefreshToken(token);

    // The application constructs its store once, so nothing in production depends on this.
    // What it demonstrates is that the value went to the platform rather than into a field:
    // an implementation holding the token in memory passes every host test and fails here.
    expect(await const SecureTokenStore().readRefreshToken(), token);
    await store.clear();
  });

  testWidgets('clearing removes it from the platform store', (tester) async {
    await store.writeRefreshToken(token);

    await store.clear();

    expect(await store.readRefreshToken(), isNull);
  });

  testWidgets('writing a token leaves it for the next launch', (tester) async {
    await store.writeRefreshToken(token);

    expect(await store.readRefreshToken(), token);
  });

  testWidgets('a token left by a previous launch is still there', (tester) async {
    // Run on its own, this reads what a *previous process* wrote. Run as part of the whole
    // file, the test above wrote it — still a true statement, and the reason the header says
    // which invocation demonstrates which claim.
    expect(await store.readRefreshToken(), token);

    await store.clear();
  });
}
