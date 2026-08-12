// SHIP-50 — the credentials sign-in and refresh both answer with, read against
// contracts/paths/identity.yaml's TokenPair schema.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/auth/token_pair.dart';
import 'package:shipper/core/errors/api_failure.dart';

/// The contract's own example, verbatim.
const _pair = <String, dynamic>{
  'access_token': 'eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIwMTkxZjNjMiJ9.signature',
  'expires_in': 900,
  'refresh_token': 'hV8pQ2mXk4tZ7nR1bY6wJ3sL0aD5cF9gE2iU8oT4xM7',
  'refresh_token_expires_in': 2592000,
};

void main() {
  test('reads the two tokens', () {
    final pair = TokenPair.fromJson(_pair);

    expect(pair.accessToken, _pair['access_token']);
    expect(pair.refreshToken, _pair['refresh_token']);
  });

  test('tolerates a field this build has never heard of', () {
    // Docs/07 §6: the platform adds fields rather than repurposing them, and a client that
    // refused what it did not recognise would turn every additive change into a store release.
    final pair = TokenPair.fromJson({..._pair, 'scope': 'everything'});

    expect(pair.accessToken, _pair['access_token']);
  });

  test('neither lifetime is read, and that is deliberate', () {
    // Nothing schedules on expires_in. This client refreshes when the platform says a token is
    // no longer accepted — a 401 — and never on a clock, because a handset that has been out of
    // signal does not agree with the platform about what time it is. The assertion is that the
    // pair still parses when the two counts are absent or nonsense, which is what "not read"
    // has to mean to be worth anything.
    for (final body in <Map<String, dynamic>>[
      {'access_token': 'a', 'refresh_token': 'r'},
      {'access_token': 'a', 'refresh_token': 'r', 'expires_in': 'soon'},
    ]) {
      expect(TokenPair.fromJson(body).accessToken, 'a', reason: '$body');
    }
  });

  group('a body that is not the contract', () {
    test('is a malformed response rather than a pair with an empty token', () {
      // An empty access token would surface much later as an unexplained 401 that a refresh
      // cannot fix. Failing at the parse names the actual problem.
      for (final body in <Map<String, dynamic>>[
        <String, dynamic>{},
        {'access_token': 'a'},
        {'refresh_token': 'r'},
        {'access_token': '', 'refresh_token': 'r'},
        {'access_token': 'a', 'refresh_token': ''},
        {'access_token': 1, 'refresh_token': 'r'},
      ]) {
        expect(
          () => TokenPair.fromJson(body),
          throwsA(isA<ApiMalformedResponse>()),
          reason: '$body',
        );
      }
    });

    test('is a failure that leaves the outcome unknown', () {
      // Which matters, and is why the parse throws this failure rather than a StateError:
      // nothing about an unparseable body says whether the platform rotated the token, so the
      // idempotency key has to be retained for the retry rather than retired.
      late final Object thrown;
      try {
        TokenPair.fromJson(const <String, dynamic>{});
        fail('an empty body must not parse');
      } catch (e) {
        thrown = e;
      }

      expect(ActionKey.outcomeUnknown(thrown), isTrue);
    });
  });

  test('printing it does not print either token', () {
    // Docs/07 §3 forbids a token reaching logs or crash reports, and a generated toString is
    // how one gets into both at once. This is the reason the class is hand-written.
    final printed = TokenPair.fromJson(_pair).toString();

    expect(printed, isNot(contains(_pair['access_token'] as String)));
    expect(printed, isNot(contains(_pair['refresh_token'] as String)));
  });
}
