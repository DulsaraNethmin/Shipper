import 'dart:io' show Platform;

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:package_info_plus/package_info_plus.dart';

/// The platforms `GET /v1/app/minimum-version` carries a floor for, and everything else.
enum AppPlatform {
  ios,
  android,

  /// A desktop host during development, or a platform this app does not ship to.
  ///
  /// The endpoint answers for two platforms and this is neither, so there is no floor to compare
  /// and the gate never blocks. `defaultDeviceLabel` takes the same shape for the same reason:
  /// a build running somewhere the platform has no opinion about is not a build to lock out.
  other,
}

/// Which platform a `Platform.operatingSystem` string names.
///
/// A function of the string rather than a read of [Platform] so that every branch is testable on
/// a host machine, following `ApiEnvironment.baseUrl` and `defaultDeviceLabel`.
AppPlatform appPlatformOf(String operatingSystem) => switch (operatingSystem) {
  'ios' => AppPlatform.ios,
  'android' => AppPlatform.android,
  _ => AppPlatform.other,
};

/// The build this process actually is (SHIP-168).
///
/// ## Where [number] comes from, which is the whole question
///
/// It is the **native build number** — `CFBundleVersion` on iOS, `versionCode` on Android — read
/// back through `package_info_plus`. `Docs/07` §8 requires every build to carry a unique,
/// monotonically increasing build number, and this is that number rather than a copy of it: the
/// Info.plist holds `$(FLUTTER_BUILD_NUMBER)` and `build.gradle.kts` holds `flutter.versionCode`,
/// so what is read here is exactly what `flutter build --build-number=…` set, and locally exactly
/// the `+1` of `version: 1.0.0+1` in `pubspec.yaml`.
///
/// **The alternative was a `--dart-define`, and it was rejected** even though it needs no package
/// and `ApiEnvironment` already uses that mechanism for the environment. A define is a *second*
/// place the build number lives, and nothing can make the two agree: the release pipeline at
/// SHIP-24…27 would have to pass `--build-number=N` and `--dart-define=…=N` together forever, and
/// the day it passes only the first, this gate compares the wrong integer. Wrong in one direction
/// locks out a supported build; wrong in the other lets through exactly the build the floor was
/// raised to retire. Neither produces a test failure or a log line. Reading the number the store
/// actually sees removes the disagreement rather than documenting it.
///
/// The comparison is on an integer and not on a version string, which is the platform's decision
/// rather than this one — `internal/config/config.go` gives the reasoning: ordering pre-release
/// suffixes correctly is the kind of nearly-right that locks out a valid build.
@immutable
class RunningBuild {
  const RunningBuild({required this.platform, required this.number});

  final AppPlatform platform;

  /// The native build number, always an integer. See the class note.
  final int number;

  /// Reads the running build, or `null` when it cannot be established.
  ///
  /// **Null on any failure, and never a throw**, because this is awaited before `runApp` and
  /// nothing about a version check may be the reason an app does not start. Three things produce
  /// it, and all three end at the same place — a gate with nothing to compare, which blocks
  /// nobody:
  ///
  /// - No plugin behind the channel. A host test, or a platform the plugin does not implement.
  /// - A `CFBundleVersion` that is not an integer. Nothing stops somebody putting `1.0.0` there,
  ///   and a build whose number cannot be parsed cannot be compared to a floor.
  /// - Any other platform-channel failure.
  ///
  /// Failing open here is the same decision `version_gate.dart` argues at length for an
  /// unreachable API, and it is taken for the same reason.
  ///
  /// **The `catch` is load-bearing and `running_build_test.dart` exists because of it.** Removing
  /// it was a mutation that survived every other test in this suite, while in production it turns
  /// a version check into the thing that stops the version starting.
  static Future<RunningBuild?> read() async {
    try {
      final info = await PackageInfo.fromPlatform();
      final number = int.tryParse(info.buildNumber.trim());
      if (number == null) return null;
      return RunningBuild(platform: appPlatformOf(Platform.operatingSystem), number: number);
    } catch (_) {
      return null;
    }
  }

  @override
  bool operator ==(Object other) =>
      other is RunningBuild && other.platform == platform && other.number == number;

  @override
  int get hashCode => Object.hash(platform, number);

  @override
  String toString() => 'RunningBuild(${platform.name}, $number)';
}

/// The build the application is running, or `null` when nothing supplied one.
///
/// `null` by default, and that default is deliberate in exactly the way `queueWatchProvider`'s is
/// (SHIP-126). **Every widget test builds `ShipperApp`**, and from this ticket `ShipperApp` holds
/// a gate — so a provider that reached the platform channel on its own would have every one of
/// them call a plugin that a host test has nothing behind, and would then have every one of them
/// make an HTTP request to whatever base URL the build was compiled with.
///
/// With the seam empty there is no build number, so there is nothing to compare, so **no request
/// is made at all**. `version_gate.dart` holds that as a test rather than trusting it.
///
/// The consequence worth stating plainly, because it is the same one SHIP-126 wrote down: **an
/// application that never overrides this is never blocked**, whatever floor the platform
/// publishes. That is right for a test and would be a defect in production, so
/// `version_gate_wiring_test.dart` holds `main`'s override.
final runningBuildProvider = Provider<RunningBuild?>((ref) => null);
