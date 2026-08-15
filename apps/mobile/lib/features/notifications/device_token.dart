import 'dart:io';

import 'package:flutter/foundation.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

part 'device_token.freezed.dart';
part 'device_token.g.dart';

/// Which store this build came from (SHIP-143).
///
/// **Not how a message is routed** — `contracts/paths/notifications.yaml` says so in as many words:
/// Firebase fronts APNs, so one adapter reaches both. It is what lets support answer which handsets
/// an account is signed in on.
///
/// The platform's enum is `ios` and `android`, and there is no third value. A build running
/// anywhere else — a host test, a desktop shell — has no registration to make, which is why
/// [currentDevicePlatform] answers `null` rather than inventing one.
enum DevicePlatform {
  ios('ios'),
  android('android');

  const DevicePlatform(this.wire);

  /// What goes in the request body.
  final String wire;
}

/// The platform this process is running on, or `null` where the endpoint has no answer for it.
///
/// `Platform.isIOS` rather than a `--dart-define`, for the reason `running_build.dart` gives about
/// the build number: a value that has to be passed by the release pipeline forever is a value the
/// first build that forgets gets wrong, with no test failure either way.
///
/// Guarded on [kIsWeb] because `dart:io`'s `Platform` throws there, and a client that crashed at
/// launch on a platform it does not target would be worse than one that registers nothing.
DevicePlatform? currentDevicePlatform() {
  if (kIsWeb) return null;
  if (Platform.isIOS) return DevicePlatform.ios;
  if (Platform.isAndroid) return DevicePlatform.android;
  return null;
}

/// A registration, as the platform answers with one (SHIP-140, SHIP-143).
///
/// **There is no `token` field and there must not be one.** The contract is explicit: "the client
/// sent the token and already has it, and a value that identifies somebody's handset should appear
/// in as few places as possible — including response bodies, which are logged by more middleware
/// than anybody remembers". A model with somewhere to put it is the first place a screen could be
/// written against one.
@freezed
abstract class DeviceToken with _$DeviceToken {
  const factory DeviceToken({
    /// The registration.
    required String id,

    /// Which store the registered build came from.
    ///
    /// Nullable and defaulted rather than required, the same call `PlatformFloor.minimumBuild`
    /// makes: a required field is a decode that **throws**, on a build already on a phone that
    /// cannot be fixed over the air, and nothing branches on this.
    String? platform,

    /// When the platform recorded this registration, in UTC.
    @JsonKey(name: 'registered_at') String? registeredAt,
  }) = _DeviceToken;

  factory DeviceToken.fromJson(Map<String, dynamic> json) => _$DeviceTokenFromJson(json);
}
