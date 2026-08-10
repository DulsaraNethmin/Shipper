import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_environment.dart';

void main() {
  group('base URL per environment', () {
    test('local reaches the host machine from an iOS simulator on localhost', () {
      expect(
        ApiEnvironment.local.baseUrl(isAndroid: false),
        'http://localhost:8080',
      );
    });

    test('local reaches the host machine from an Android emulator on 10.0.2.2', () {
      // Not a preference. An Android emulator's `localhost` is the emulated device itself, so
      // a client that uses it there reaches nothing — while working perfectly on iOS. This
      // assertion exists because that defect passes every test run on one platform.
      expect(
        ApiEnvironment.local.baseUrl(isAndroid: true),
        'http://10.0.2.2:8080',
      );
    });

    test('staging and production are fixed hostnames over TLS', () {
      expect(ApiEnvironment.staging.baseUrl(), 'https://api.staging.shipper.com.au');
      expect(ApiEnvironment.production.baseUrl(), 'https://api.shipper.com.au');
    });

    test('neither remote environment is plain HTTP', () {
      for (final env in [ApiEnvironment.staging, ApiEnvironment.production]) {
        expect(env.baseUrl(), startsWith('https://'), reason: '${env.name} must use TLS');
      }
    });

    test('no base URL carries a trailing slash', () {
      // dio joins a path onto the base URL, and `…com.au/` + `/v1/jobs` is `//v1/jobs`, which
      // some proxies route and some do not.
      for (final env in ApiEnvironment.values) {
        expect(env.baseUrl(isAndroid: false), isNot(endsWith('/')));
      }
    });
  });

  group('the compiled-in selection', () {
    test('defaults to local when no --dart-define was given', () {
      // The value a developer gets for typing `flutter run` is the harmless one. Tests run
      // without defines, so this is that default.
      expect(ApiEnvironment.current, ApiEnvironment.local);
    });

    test('every environment name is a valid define value', () {
      // ApiEnvironment.current matches on `name`, so a renamed enum constant silently changes
      // what `--dart-define=SHIPPER_ENV=…` accepts — including in a CI pipeline nobody reruns.
      expect(
        ApiEnvironment.values.map((e) => e.name).toSet(),
        {'local', 'staging', 'production'},
      );
    });
  });
}
