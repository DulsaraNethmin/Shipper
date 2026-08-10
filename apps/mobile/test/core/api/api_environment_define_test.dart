// Proves the build flavour actually selects the environment — from the command line, which is
// where it is selected in anger.
//
// SHIP-18's acceptance criterion is that the client targets local, staging and production by
// build flavour. Every other test in this suite runs with no defines at all, so they can only
// ever exercise the default. This one is run three times by `make flutter-test-defines`, once
// per environment, and asserts that what arrived is what was asked for.
//
// It passes under the default run too, which is deliberate: a test that only runs under a
// special invocation is a test that stops being run.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_environment.dart';

const _requested = String.fromEnvironment(ApiEnvironment.envDefine, defaultValue: 'local');
const _port = int.fromEnvironment(ApiEnvironment.localPortDefine, defaultValue: 8080);

void main() {
  test('--dart-define=${ApiEnvironment.envDefine}=$_requested selects that environment', () {
    expect(ApiEnvironment.current.name, _requested);

    final url = ApiEnvironment.current.baseUrl(isAndroid: false);

    switch (_requested) {
      case 'local':
        expect(url, 'http://localhost:$_port');
      case 'staging':
        expect(url, 'https://api.staging.shipper.com.au');
      case 'production':
        expect(url, 'https://api.shipper.com.au');
      default:
        fail('unhandled environment "$_requested"');
    }
  });

  test('the local port follows ${ApiEnvironment.localPortDefine}', () {
    // Every git worktree runs the API on a different port (CLAUDE.md, "Working in more than
    // one branch at once"), so the port is a property of the machine and must not be an edit
    // to the source. Under the default run this asserts the documented 8080.
    expect(
      ApiEnvironment.local.baseUrl(isAndroid: true),
      'http://10.0.2.2:$_port',
    );
  });
}
