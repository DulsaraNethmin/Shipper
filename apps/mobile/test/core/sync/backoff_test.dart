// SHIP-125 — "with exponential backoff", as a policy that can be read on its own.
//
// The worker's tests prove the policy is applied and stored durably. These prove the shape of it:
// that it grows, that it stops growing, and that two handsets that failed at the same instant do
// not come back at the same instant.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/sync/backoff.dart';

void main() {
  const backoff = Backoff();

  group('growth', () {
    test('the first failure waits the base, and each one after it waits twice as long', () {
      // roll: 1 is the top of the jittered window, which is the nominal delay — so this reads the
      // schedule without the jitter in the way.
      expect(backoff.after(1, roll: 1), const Duration(seconds: 5));
      expect(backoff.after(2, roll: 1), const Duration(seconds: 10));
      expect(backoff.after(3, roll: 1), const Duration(seconds: 20));
      expect(backoff.after(4, roll: 1), const Duration(seconds: 40));
      expect(backoff.after(5, roll: 1), const Duration(seconds: 80));
    });

    test('an attempt count below one is treated as the first rather than thrown over', () {
      // A row written by another build is not worth an exception. Its stored attempts column is
      // read as-is by SHIP-124, which does not bound it.
      expect(backoff.after(0, roll: 1), backoff.after(1, roll: 1));
      expect(backoff.after(-3, roll: 1), backoff.after(1, roll: 1));
    });

    test('the delay never decreases as attempts grow', () {
      var previous = Duration.zero;
      for (var attempts = 1; attempts <= 40; attempts++) {
        final delay = backoff.after(attempts, roll: 1);
        expect(delay, greaterThanOrEqualTo(previous), reason: 'at attempt $attempts');
        previous = delay;
      }
    });
  });

  group('the ceiling', () {
    test('holds the wait at five minutes however long the outage lasts', () {
      // Without one, doubling reaches a day inside sixteen failures — an operation that has
      // effectively stopped retrying, which for a delivery update is indistinguishable from one
      // that was dropped.
      expect(backoff.after(7, roll: 1), const Duration(minutes: 5));
      expect(backoff.after(50, roll: 1), const Duration(minutes: 5));
    });

    test('survives an attempt count that would overflow a shift', () {
      // A phone left in a depot over a long weekend reaches attempt counts where `base * 2^n`
      // wraps, and a wrapped delay clamps to whatever sign it landed on. Doubling in a loop that
      // stops at the ceiling is what makes this a non-event rather than a negative wait.
      expect(backoff.after(1000, roll: 1), const Duration(minutes: 5));
      expect(backoff.after(1 << 40, roll: 0), const Duration(minutes: 2, seconds: 30));
    });
  });

  group('jitter', () {
    test('spreads each wait across the top half of its nominal delay', () {
      // Equal jitter: half the nominal delay is never skipped, and the other half is spread. The
      // floor is what stops a full-jitter short draw retrying pointlessly early against a link
      // that has not changed.
      expect(backoff.after(3, roll: 0), const Duration(seconds: 10));
      expect(backoff.after(3, roll: 0.5), const Duration(seconds: 15));
      expect(backoff.after(3, roll: 1), const Duration(seconds: 20));
    });

    test('two devices that failed together do not come back together', () {
      // The thundering herd a ceiling does not save you from: the ceiling decides how often, the
      // jitter decides whether every queued operation in a region arrives in the same instant.
      final draws = {
        for (final roll in [0.0, 0.2, 0.4, 0.6, 0.8, 1.0]) backoff.after(9, roll: roll),
      };

      expect(draws.length, 6, reason: 'every draw should land somewhere different');
      expect(draws.reduce((a, b) => a < b ? a : b), const Duration(minutes: 2, seconds: 30));
      expect(draws.reduce((a, b) => a > b ? a : b), const Duration(minutes: 5));
    });

    test('a roll outside the unit interval is clamped rather than trusted', () {
      expect(backoff.after(2, roll: -1), backoff.after(2, roll: 0));
      expect(backoff.after(2, roll: 9), backoff.after(2, roll: 1));
    });
  });

  test('the policy is a value, so a slower or faster one needs no change here', () {
    const patient = Backoff(base: Duration(seconds: 30), ceiling: Duration(hours: 1));

    expect(patient.after(1, roll: 1), const Duration(seconds: 30));
    expect(patient.after(100, roll: 1), const Duration(hours: 1));
  });
}
