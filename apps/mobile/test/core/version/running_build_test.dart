// SHIP-168 — where the running build's number comes from, and what happens when it does not come.
//
// This file exists because of a mutation that survived the rest of the suite. `RunningBuild.read`
// swallows everything and answers `null`, and deleting that guard broke no test at all — while in
// production it is the difference between an app that starts with an inert gate and an app that
// **throws before `runApp`** and shows nothing. A launch-time check that can stop the launch is the
// worst possible version of this ticket, and nothing was holding it.
//
// The three cases below are the three the class documents, in the order the platform can produce
// them.

import 'package:flutter_test/flutter_test.dart';
import 'package:package_info_plus/package_info_plus.dart';
import 'package:shipper/core/version/running_build.dart';

/// [PackageInfo.setMockInitialValues] with only the field this ticket reads varying.
void platformReports(String buildNumber) {
  PackageInfo.setMockInitialValues(
    appName: 'Shipper',
    packageName: 'au.com.shipper',
    version: '1.0.0',
    buildNumber: buildNumber,
    buildSignature: '',
  );
}

void main() {
  TestWidgetsFlutterBinding.ensureInitialized();

  // **These run in order and have to.** `PackageInfo` caches the first answer in a static with no
  // reset, so once anything has mocked it, the un-mocked case can never be asked again in this
  // isolate. The un-mocked one is first for that reason and not by accident.

  test('answers null when there is no plugin behind the channel', () async {
    // A host test is this case, and so is a platform the plugin does not implement. What matters
    // is the *shape* of the answer: `main` awaits this before `runApp`, so a throw here is an
    // application that does not start — a version check that is itself the outage.
    expect(await RunningBuild.read(), isNull);
  });

  test('reads the native build number the platform reports', () async {
    // `flutter build --build-number=42` writes 42 into CFBundleVersion and versionCode, and this
    // is the read of it. On a host the platform is neither iOS nor Android, which is its own case
    // in `verdictFor`; the number is what this test is about.
    platformReports('42');

    expect((await RunningBuild.read())?.number, 42);
  });

  test('answers null when the build number is not an integer', () async {
    // Nothing stops a project putting `1.0.0` in CFBundleVersion, and a build whose number cannot
    // be parsed cannot be compared to a floor. Answering null puts it on the same footing as a
    // build the platform has no opinion about, rather than blocking or crashing.
    platformReports('1.0.0');

    expect(await RunningBuild.read(), isNull);
  });
}
