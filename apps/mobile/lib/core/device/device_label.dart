import 'dart:io' show Platform;

import 'package:flutter_riverpod/flutter_riverpod.dart';

/// What `device_label` on `POST /v1/auth/login` carries (SHIP-55).
///
/// ## It is display text and it authenticates nothing
///
/// `contracts/paths/identity.yaml` says so plainly, and it is worth restating because a field
/// naming a handset looks like it ought to identify one: two devices may legitimately carry the
/// same label, and nothing about the label decides anything. What it is for is
/// `GET /v1/auth/sessions` (SHIP-46) — a person deciding which of their devices to revoke, who
/// cannot act on four rows reading "Unknown device".
///
/// ## Derived, not asked for
///
/// The contract observes that the sign-in screen is the only moment a label could be collected,
/// which reads as an argument for a third field on the form. It is not taken: a person signing in
/// on their own phone should not be asked to name it, and whatever they typed would be worse than
/// this for the one job the field has.
///
/// ## Why not the model name
///
/// "iPhone 15 Pro" is what the contract's example suggests and what a person would recognise
/// best. Reading it needs `device_info_plus`, and a new package is a decision with a maintenance
/// cost, two native integrations and a store-privacy consequence — not something to take inside a
/// two-point ticket that does not need it. **`Docs/11` §9 carries it as a named follow-up**; when
/// somebody adds the package, this function is the only thing that changes.
///
/// ## Why it is in `core/device/` and not in `core/auth/`
///
/// It reads [Platform], and `token_store_is_not_preferences_test.dart` forbids `dart:io` anywhere
/// under `core/auth/` — that rule is about the application documents directory, the third
/// location `Docs/07` §3 rules out for a token, and its own note says it will acquire no
/// exceptions. A folder for facts about the handset is the honest home for this in any case, and
/// it is where a future `device_info_plus` would land.
///
/// [operatingSystem] and [version] are parameters rather than direct reads of [Platform] so that
/// every branch is testable on a host machine, following `ApiEnvironment.baseUrl`.
String defaultDeviceLabel({String? operatingSystem, String? version}) {
  final os = operatingSystem ?? Platform.operatingSystem;
  final release = _firstVersionIn(version ?? Platform.operatingSystemVersion);

  final name = switch (os) {
    'ios' => 'iOS',
    'android' => 'Android',
    // Neither platform this app ships to. A build running somewhere else — a desktop host during
    // development — still has to send something the contract accepts, which is one to 120
    // characters of anything.
    _ => os.isEmpty ? 'Unknown device' : os,
  };

  return release == null ? name : '$name $release';
}

/// The first version-shaped run of digits in a platform string.
///
/// `Platform.operatingSystemVersion` is documented as human-readable and unstructured, and it
/// differs per platform — "Version 17.0 (Build 21A329)" against "Android 14 (API 34)". Anything
/// that parsed it strictly would be a client that stops labelling devices when a vendor changes a
/// word, so this takes the first number it recognises and gives up quietly.
String? _firstVersionIn(String description) {
  final match = RegExp(r'\d+(?:\.\d+)*').firstMatch(description);
  return match?.group(0);
}

/// The label this build sends.
///
/// A provider so a test can pin it — the alternative is asserting a login body against whatever
/// the host machine happens to be — and so that a future `device_info_plus` reads the model name
/// in one place.
final deviceLabelProvider = Provider<String>((ref) => defaultDeviceLabel());
