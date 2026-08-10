import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/health/health_status.dart';

void main() {
  test('decodes what GET /health actually returns', () {
    // Copied from healthResponse in services/core/cmd/api/routes.go. If that struct's JSON
    // tags change, this fails here rather than on a device.
    final health = HealthStatus.fromJson(const {
      'status': 'ok',
      'version': 'v0.1.0-12-gc218f8b',
      'commit': 'c218f8b',
      'built_at': '2026-08-10T07:53:00Z',
      'dirty': false,
      'uptime': '1m30s',
    });

    expect(health.status, 'ok');
    expect(health.version, 'v0.1.0-12-gc218f8b');
    expect(health.commit, 'c218f8b');
    expect(health.builtAt, '2026-08-10T07:53:00Z');
    expect(health.dirty, isFalse);
    expect(health.uptime, '1m30s');
  });

  test('ignores fields this build has never heard of', () {
    // Docs/07 §6. Server changes stay backward compatible by adding fields, and a build
    // already on a phone has to step over anything it does not recognise — there is no
    // over-the-air fix if it does not.
    final health = HealthStatus.fromJson(const {
      'status': 'ok',
      'version': 'v0.2.0',
      'region': 'ap-southeast-2',
      'dependencies': {'postgres': 'ok', 'redis': 'ok'},
    });

    expect(health.version, 'v0.2.0');
  });

  test('survives a response missing everything optional', () {
    // The other direction: a field that disappears, or an older deployment that never sent it,
    // degrades to "not shown" rather than to a crash on a phone.
    final health = HealthStatus.fromJson(const {'status': 'ok', 'version': 'dev'});

    expect(health.commit, isNull);
    expect(health.builtAt, isNull);
    expect(health.uptime, isNull);
    expect(health.dirty, isFalse);
  });

  test('has value equality, so an unchanged response does not rebuild the screen', () {
    const a = HealthStatus(status: 'ok', version: 'v1');
    const b = HealthStatus(status: 'ok', version: 'v1');

    expect(a, b);
  });
}
