import 'dart:io' show Platform;

/// Which deployment the build talks to.
///
/// `Docs/07` §8 requires the environments to be separate and independently installable — a
/// tester must be able to hold a staging build and a production build on one device. This enum
/// is half of that: it fixes *where* a build points. The other half is a distinct application
/// id per environment, which needs Xcode and Gradle flavours and arrives with the signing work
/// at SHIP-24…SHIP-27. Until then a device holds one build at a time.
enum ApiEnvironment {
  /// The stack on the developer's own machine, started by `make up && make run`.
  local,

  /// The shared pre-production deployment. TestFlight and Play internal testing builds.
  staging,

  /// The pilot. What is on a customer's phone.
  production;

  /// The environment this build was compiled for.
  ///
  /// Selected by `--dart-define=SHIPPER_ENV=…`, which is the mechanism with the fewest moving
  /// parts that still decides at *build* time rather than at run time. A run-time switch is a
  /// switch a support call can talk somebody into flipping, and `Docs/07` §8 wants the
  /// environments genuinely separate.
  ///
  /// Defaults to [local]: the value a developer gets for typing `flutter run` should be the
  /// harmless one. A release build that forgets the define points at a developer's laptop and
  /// fails visibly on the first request, which is far better than one that forgets it and
  /// silently reaches production.
  static ApiEnvironment get current {
    const name = String.fromEnvironment(envDefine, defaultValue: 'local');
    return ApiEnvironment.values.firstWhere(
      (e) => e.name == name,
      orElse: () => throw ArgumentError.value(
        name,
        envDefine,
        'Unknown environment. Expected one of: '
            '${ApiEnvironment.values.map((e) => e.name).join(', ')}',
      ),
    );
  }

  /// `--dart-define` keys. Named constants because a typo in a define is silent — the value
  /// simply does not arrive and the default is used instead.
  static const envDefine = 'SHIPPER_ENV';
  static const baseUrlDefine = 'SHIPPER_API_BASE_URL';
  static const localPortDefine = 'SHIPPER_API_PORT';

  /// The base URL this build sends requests to.
  ///
  /// [isAndroid] is a parameter rather than a direct read of [Platform] so that both branches
  /// are testable on a host machine. The distinction it carries is not cosmetic: **an Android
  /// emulator does not reach the host on `localhost`** — that address is the emulated device
  /// itself — and reaches it on `10.0.2.2` instead, while an iOS simulator shares the host's
  /// network stack and does use `localhost`. Getting this wrong produces a client that works
  /// on exactly one of the two platforms, which is the kind of defect that survives review
  /// because whoever wrote it tested on the platform it works on.
  String baseUrl({bool? isAndroid}) {
    const override = String.fromEnvironment(baseUrlDefine);
    if (override.isNotEmpty) return _withoutTrailingSlash(override);

    switch (this) {
      case ApiEnvironment.local:
        final host = (isAndroid ?? Platform.isAndroid) ? _androidEmulatorHost : 'localhost';
        const port = int.fromEnvironment(localPortDefine, defaultValue: _defaultLocalPort);
        return 'http://$host:$port';

      // Provisional. Neither hostname is registered yet and neither is deployed — there is no
      // ticket for the DNS. They are written out rather than left blank so that the three
      // environments are genuinely three, and so the shape of what is needed is obvious; both
      // are overridden by SHIPPER_API_BASE_URL until they exist.
      case ApiEnvironment.staging:
        return 'https://api.staging.shipper.com.au';
      case ApiEnvironment.production:
        return 'https://api.shipper.com.au';
    }
  }

  /// The loopback alias an Android emulator uses to reach the host machine.
  static const _androidEmulatorHost = '10.0.2.2';

  /// Matches `HTTP_PORT` in `deploy/.env.example`. A worktree that moves the API — as every
  /// concurrent branch does — passes `--dart-define=SHIPPER_API_PORT=…` rather than editing
  /// this, because the port is a property of the machine and not of the application.
  static const _defaultLocalPort = 8080;

  static String _withoutTrailingSlash(String url) =>
      url.endsWith('/') ? url.substring(0, url.length - 1) : url;
}
