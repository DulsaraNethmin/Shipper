// SHIP-168 — the comparison, as a table.
//
// `verdictFor` is a pure function of two facts, which is what lets every cell of the table be
// written down together. Docs/07 §6 is one sentence long about this and every defect it admits is
// a cell somebody did not think about: the boundary, the platform, and the three ways a fact can
// be missing.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/version/minimum_version.dart';
import 'package:shipper/core/version/running_build.dart';
import 'package:shipper/core/version/version_gate.dart';

MinimumVersion floors({int ios = 1, int android = 1, String? iosUrl, String? androidUrl}) {
  return MinimumVersion(
    ios: PlatformFloor(minimumBuild: ios, storeUrl: iosUrl),
    android: PlatformFloor(minimumBuild: android, storeUrl: androidUrl),
  );
}

RunningBuild ios(int number) => RunningBuild(platform: AppPlatform.ios, number: number);
RunningBuild android(int number) => RunningBuild(platform: AppPlatform.android, number: number);

void main() {
  group('the comparison', () {
    test('a build below the floor is blocked', () {
      final verdict = verdictFor(build: ios(41), floors: floors(ios: 42));

      expect(verdict.blocked, isTrue);
    });

    test('a build equal to the floor is the oldest supported build, and runs', () {
      // The single most important cell in this table. `internal/config/config.go` calls the field
      // "the lowest build number still permitted", so the floor is inclusive — and `<=` here
      // would lock out every device on the exact build the floor was just raised to, which is the
      // largest population there is at that moment.
      final verdict = verdictFor(build: ios(42), floors: floors(ios: 42));

      expect(verdict.blocked, isFalse);
    });

    test('a build above the floor runs', () {
      expect(verdictFor(build: ios(43), floors: floors(ios: 42)).blocked, isFalse);
    });

    test('each platform is compared against its own floor', () {
      // One response carries both, and reading the wrong entry is invisible on whichever platform
      // the author happened to be testing on.
      final published = floors(ios: 42, android: 1);

      expect(verdictFor(build: ios(10), floors: published).blocked, isTrue);
      expect(verdictFor(build: android(10), floors: published).blocked, isFalse);
    });

    test('a platform the endpoint answers no floor for is never blocked', () {
      // A desktop host during development. There is no `other` key in the response, so there is
      // nothing to be below.
      final elsewhere = RunningBuild(platform: AppPlatform.other, number: 1);

      expect(verdictFor(build: elsewhere, floors: floors(ios: 999, android: 999)).blocked, isFalse);
    });
  });

  group('the three ways a fact can be missing, all of which fail open', () {
    test('a build number that could not be established is never blocked', () {
      // `RunningBuild.read` returns null when the channel is unavailable or CFBundleVersion is
      // not an integer, and it is also the default in every widget test.
      expect(verdictFor(build: null, floors: floors(ios: 999)).blocked, isFalse);
    });

    test('an answer that has not arrived is never blocked', () {
      // The check in flight, and — the same cell — the check that failed. Blocking here would
      // make a network round trip a condition of starting the app at all.
      expect(verdictFor(build: ios(1), floors: null).blocked, isFalse);
    });

    test('a floor that arrived without a minimum_build blocks nobody', () {
      const missing = MinimumVersion(ios: PlatformFloor(), android: PlatformFloor());

      expect(verdictFor(build: ios(1), floors: missing).blocked, isFalse);
    });
  });

  group('the link, which the platform is allowed not to send', () {
    test('a blocked build carries the link the platform sent for its own platform', () {
      final verdict = verdictFor(
        build: ios(1),
        floors: floors(ios: 42, iosUrl: 'https://apps.apple.com/app/id123', androidUrl: 'https://play.example'),
      );

      expect(verdict.blocked, isTrue);
      expect(verdict.storeUrl, 'https://apps.apple.com/app/id123');
    });

    test('an absent store_url is a block with no link, which is the pilot', () {
      final verdict = verdictFor(build: ios(1), floors: floors(ios: 42));

      expect(verdict.blocked, isTrue, reason: 'no link is not a reason to let an old build run');
      expect(verdict.storeUrl, isNull);
    });

    test('a blank or whitespace store_url is no link either', () {
      // The wire omits the key when the variable is empty, so this is about a deployment that set
      // it to a space. Normalising here keeps "no link" one condition rather than three.
      expect(verdictFor(build: ios(1), floors: floors(ios: 42, iosUrl: '')).storeUrl, isNull);
      expect(verdictFor(build: ios(1), floors: floors(ios: 42, iosUrl: '   ')).storeUrl, isNull);
    });
  });

  group('which platform this build is', () {
    test('reads the two the endpoint answers for, and gives up quietly on the rest', () {
      expect(appPlatformOf('ios'), AppPlatform.ios);
      expect(appPlatformOf('android'), AppPlatform.android);
      expect(appPlatformOf('macos'), AppPlatform.other);
      expect(appPlatformOf(''), AppPlatform.other);
    });
  });
}
