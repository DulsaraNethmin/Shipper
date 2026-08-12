// SHIP-50 — the role claim, which is the only thing this client reads out of an access token.
//
// Every test here is also a statement about what the function must not do. It does not verify,
// it does not throw, and it does not guess: `Docs/07` §3 and `CLAUDE.md` put every authorisation
// decision on the platform, and this picks which half of the marketplace to draw.

import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/auth/access_token.dart';
import 'package:shipper/core/auth/user_role.dart';

import 'session_fixtures.dart';

/// A token whose payload is [claims], with no padding — which is what a JWT actually looks like
/// and what a naive `base64Url.decode` chokes on.
String _tokenWith(Map<String, Object?> claims) {
  final payload = base64Url.encode(utf8.encode(jsonEncode(claims))).replaceAll('=', '');
  return 'header.$payload.signature';
}

void main() {
  test('reads each role the platform can send', () {
    expect(roleFromAccessToken(anAccessToken(role: UserRole.customer)), UserRole.customer);
    expect(roleFromAccessToken(anAccessToken(role: UserRole.provider)), UserRole.provider);
  });

  test('a role this build has never heard of is unknown, not a crash and not a guess', () {
    // A build on a phone outlives the contract it shipped with (Docs/07 §6). The shell renders
    // unknown as "update the app" (SHIP-167); guessing customer would show a provider the wrong
    // half of the marketplace on every launch.
    expect(roleFromAccessToken(_tokenWith({'role': 'broker'})), UserRole.unknown);
  });

  test('no role claim is null, which is a different answer from unrecognised', () {
    // Null means the token said nothing readable — the shell shows what both halves share.
    // Unknown means the platform named a role this build cannot draw. Collapsing the two would
    // put an update prompt in front of somebody whose token simply had not arrived yet.
    expect(roleFromAccessToken(anAccessToken(role: null)), isNull);
    expect(roleFromAccessToken(_tokenWith({'sub': 'user-1'})), isNull);
    expect(roleFromAccessToken(_tokenWith({'role': ''})), isNull);
    expect(roleFromAccessToken(_tokenWith({'role': 42})), isNull);
  });

  test('decodes a payload whose length is not a multiple of four', () {
    // JWT strips base64 padding and `base64Url.decode` refuses input without it. Getting this
    // wrong produces a client that works for some tokens and throws for others, from inside a
    // network interceptor — which is as hard to reproduce as it sounds.
    for (final subject in ['a', 'ab', 'abc', 'abcd']) {
      final token = _tokenWith({'sub': subject, 'role': 'provider'});
      expect(roleFromAccessToken(token), UserRole.provider, reason: token);
    }
  });

  group('nothing throws, whatever arrives', () {
    // This runs inside the refresh path. An exception here would surface as an unexplained
    // failure of whatever request happened to be in flight.
    test('a string that is not a token at all', () {
      for (final value in [
        '',
        'not-a-token',
        'two.segments',
        'four.segments.here.now',
        'development-placeholder-not-a-credential',
      ]) {
        expect(roleFromAccessToken(value), isNull, reason: value);
      }
    });

    test('a middle segment that is not base64url, or is not JSON, or is not an object', () {
      expect(roleFromAccessToken('header.!!!!.signature'), isNull);
      expect(
        roleFromAccessToken('header.${base64Url.encode(utf8.encode('{'))}.signature'),
        isNull,
      );
      expect(
        roleFromAccessToken('header.${base64Url.encode(utf8.encode('[1,2]'))}.signature'),
        isNull,
      );
    });
  });
}
