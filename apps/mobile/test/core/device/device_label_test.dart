// SHIP-55 — what a sign-in calls this handset in the device list.
//
// It is display text and it authenticates nothing, so what these tests are actually protecting is
// the property SHIP-46 depends on: whatever the platform reports, the label is non-empty, inside
// the contract's one-to-120 characters, and recognisable to the person deciding which device to
// revoke.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/device/device_label.dart';

void main() {
  test('names each platform this app ships to, with its version', () {
    expect(
      defaultDeviceLabel(operatingSystem: 'ios', version: 'Version 17.0 (Build 21A329)'),
      'iOS 17.0',
    );
    expect(
      defaultDeviceLabel(operatingSystem: 'android', version: 'Android 14 (API 34)'),
      'Android 14',
    );
  });

  test('survives a version string in a shape nobody predicted', () {
    // `Platform.operatingSystemVersion` is documented as human-readable and unstructured, and it
    // differs per platform. A client that parsed it strictly would stop labelling devices the
    // first time a vendor changed a word.
    expect(defaultDeviceLabel(operatingSystem: 'ios', version: ''), 'iOS');
    expect(defaultDeviceLabel(operatingSystem: 'ios', version: 'unknown'), 'iOS');
    expect(defaultDeviceLabel(operatingSystem: 'android', version: '15'), 'Android 15');
  });

  test('a platform this app does not ship to still produces something sendable', () {
    // A desktop host during development. The contract requires one to 120 characters, and an
    // empty label would be refused — which would make a login fail for a reason nobody would
    // look for in a display field.
    expect(defaultDeviceLabel(operatingSystem: 'macos', version: '26.6.1'), 'macos 26.6.1');
    expect(defaultDeviceLabel(operatingSystem: '', version: ''), 'Unknown device');
  });

  test('whatever the platform says, the label fits the contract', () {
    for (final os in ['ios', 'android', 'macos', 'linux', '']) {
      for (final version in ['', 'Version 17.0 (Build 21A329)', 'Android 14 (API 34)', '???']) {
        final label = defaultDeviceLabel(operatingSystem: os, version: version);
        expect(label, isNotEmpty, reason: '$os / $version');
        expect(label.length, lessThanOrEqualTo(120), reason: '$os / $version');
      }
    }
  });
}
