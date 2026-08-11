// The rule in Docs/07 §4, which is wrong in both directions and expensive either way.
//
// Mint a fresh key per attempt and a dropped connection duplicates the record. Share one key
// across two taps and the second tap replays the first response, so a resend button sends
// nothing at all. ActionKey is where the line between "a retry" and "a new action" is drawn, and
// this file is that line written out.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/errors/api_failure.dart';

/// A mint that counts, so a test can say *which* attempts shared a key.
({ActionKey key, List<String> minted}) _counting() {
  final minted = <String>[];
  var n = 0;
  final key = ActionKey(mint: () {
    final value = 'key-${++n}';
    minted.add(value);
    return value;
  });
  return (key: key, minted: minted);
}

const _body = {'email': 'alice@example.com'};

void main() {
  test('a fresh action gets a fresh key', () {
    final (:key, :minted) = _counting();

    expect(key.forRequest(_body), 'key-1');
    expect(minted, ['key-1']);
  });

  test('a success retires the key, so the next tap is a new action', () {
    // The resend case. Two taps of "send another code" are two requests with identical bodies,
    // and the second must not replay the first — a resend that silently sends nothing is
    // indistinguishable, from the handset, from an SMS that has not arrived yet.
    final (:key, minted: _) = _counting();

    expect(key.forRequest(_body), 'key-1');
    key.settled(null);
    expect(key.forRequest(_body), 'key-2');
  });

  test('a dropped connection retains the key, so the retry cannot duplicate', () {
    final (:key, minted: _) = _counting();

    expect(key.forRequest(_body), 'key-1');
    key.settled(const ApiUnreachable());

    // The same action, retried. The platform may already have created the account; the point of
    // the key is that it answers with what it did rather than doing it again.
    expect(key.forRequest(_body), 'key-1');
  });

  test('a corrected body mints a new key even after a dropped connection', () {
    // The trap on the other side. The platform fingerprints method, path and body, so the same
    // key on a changed body is refused with idempotency_key_reused — which would strand
    // somebody who mistyped their address behind an error they cannot clear.
    final (:key, minted: _) = _counting();

    expect(key.forRequest(_body), 'key-1');
    key.settled(const ApiUnreachable());
    expect(key.forRequest(const {'email': 'alice@corrected.com'}), 'key-2');
  });

  test('a refusal retires the key, because the platform saw the request', () {
    final (:key, minted: _) = _counting();

    for (final refusal in <ApiFailure>[
      const ApiErrorResponse(statusCode: 409, code: 'identity_email_taken', message: 'no'),
      const ApiErrorResponse(statusCode: 422, code: 'validation_failed', message: 'no'),
      const ApiErrorResponse(statusCode: 400, code: 'identity_otp_invalid', message: 'no'),
    ]) {
      final before = key.forRequest(_body);
      key.settled(refusal);
      expect(
        key.forRequest(_body),
        isNot(before),
        reason: '${refusal.runtimeType} ${(refusal as ApiErrorResponse).code} means the '
            'platform saw the request and refused it, so replaying can only replay the refusal',
      );
      key.settled(null);
    }
  });

  test('a 5xx retains the key, because the platform may have committed before it failed', () {
    final (:key, minted: _) = _counting();

    expect(key.forRequest(_body), 'key-1');
    key.settled(const ApiErrorResponse(statusCode: 503, code: 'service_unavailable', message: ''));
    expect(key.forRequest(_body), 'key-1');
  });

  test('a request still in progress retains the key, because a new one would start a second',
      () {
    final (:key, minted: _) = _counting();

    expect(key.forRequest(_body), 'key-1');
    key.settled(
      const ApiErrorResponse(
        statusCode: 409,
        code: 'idempotency_request_in_progress',
        message: '',
      ),
    );
    expect(key.forRequest(_body), 'key-1');
  });

  test('a body that was not the contract retains the key: nothing in it says what happened', () {
    final (:key, minted: _) = _counting();

    expect(key.forRequest(_body), 'key-1');
    key.settled(const ApiMalformedResponse(statusCode: 502));
    expect(key.forRequest(_body), 'key-1');
  });

  group('the minted value', () {
    test('is unique across calls', () {
      final keys = List.generate(200, (_) => newIdempotencyKey());
      expect(keys.toSet(), hasLength(200));
    });

    test('is 128 bits of hex, which the platform accepts and nobody can guess', () {
      // The public identity endpoints share the anonymous idempotency scope (Docs/11 §6), where
      // a stored response is protected by a request fingerprint alone. A counter or a timestamp
      // would be a second thing worth guessing.
      expect(newIdempotencyKey(), matches(RegExp(r'^[0-9a-f]{32}$')));
      expect(newIdempotencyKey().length, lessThanOrEqualTo(255));
    });
  });
}
