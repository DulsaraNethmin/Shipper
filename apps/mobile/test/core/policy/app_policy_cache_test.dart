// SHIP-167a — the file the whole ticket rests on.
//
// The precedence rule is only worth anything if the cached policy is actually there on the next
// launch, so this exercises the real [FileAppPolicyCache] against a real directory rather than the
// in-memory fake the other files use. A second instance over the same directory is what stands in
// for the next process, which is the only property that matters and the one a fake cannot show.

import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/policy/app_policy.dart';
import 'package:shipper/core/policy/app_policy_cache.dart';

import 'policy_fixture.dart';

void main() {
  late Directory root;

  setUp(() async {
    root = await Directory.systemTemp.createTemp('shipper-policy-');
  });

  tearDown(() async {
    if (root.existsSync()) await root.delete(recursive: true);
  });

  test('a device that has never been online has nothing to read', () async {
    expect(await FileAppPolicyCache(root).read(), isNull);
  });

  test('what was written survives into the next launch', () async {
    await FileAppPolicyCache(root).write(cachedPolicyUnlikeTheDefault);

    // A second instance over the same directory: the next process, as far as this can be observed
    // on a host. Reusing the first would only prove a field was set.
    expect(await FileAppPolicyCache(root).read(), cachedPolicyUnlikeTheDefault);
  });

  test('the newest answer replaces the last one rather than accumulating', () async {
    final cache = FileAppPolicyCache(root);

    await cache.write(cachedPolicyUnlikeTheDefault);
    await cache.write(const AppPolicy(unsyncedNudgeAfterSeconds: 60));

    final read = await cache.read();
    expect(read?.unsyncedNudgeAfterSeconds, 60);
  });

  test('a file this build cannot read is nothing to read, not a crash', () async {
    // Three ways to reach the same outcome, and they are one case deliberately: there is no repair
    // for a corrupt policy file other than fetching again, which the app does on every launch. A
    // caller offered the distinction could do nothing with it.
    for (final content in <String>['', 'not json at all', '[1, 2, 3]', '{"unsynced_nudge_after_seconds": "soon"}']) {
      final cache = FileAppPolicyCache(root);
      await cache.file.writeAsString(content);

      expect(
        await cache.read(),
        isNull,
        reason: 'reading ${content.isEmpty ? 'an empty file' : content} should be "nothing here", '
            'and the device then falls through to the compiled default rather than failing to '
            'start',
      );
    }
  });

  test('it writes inside the directory it was given and names nothing else', () async {
    await FileAppPolicyCache(root).write(compiledAppPolicy);

    final written = root.listSync().map((e) => e.uri.pathSegments.last).toList();
    expect(written, [FileAppPolicyCache.fileName]);
  });
}
