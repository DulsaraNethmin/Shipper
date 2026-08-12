// The two conventions CLAUDE.md fixes and a screen can silently break: day-first dates and AUD.
//
// Both are the kind of thing that looks right in every test written by somebody who already knows
// what the value was meant to be. 03/04 is a different day, and $1500 is a different number from
// $15.00.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/shared/formatting/dates.dart';
import 'package:shipper/shared/formatting/money.dart';

void main() {
  group('dates are day-first', () {
    test('renders the day before the month, with the month spelled', () {
      // Spelled rather than numbered, which removes the ambiguity instead of relying on the
      // reader knowing which convention the screen used.
      expect(dayFirstDate('2026-08-25T03:30:00.000Z'), '25 Aug 2026');
      expect(dayFirstDate('2026-01-03T12:00:00.000Z'), '3 Jan 2026');
      expect(dayFirstDate('2026-12-31T12:00:00.000Z'), '31 Dec 2026');
    });

    test('converts to the device’s own time zone', () {
      // The platform sends UTC and Australia is eight to eleven hours ahead of it, so a job
      // created at 8am in Sydney is the day *before* in UTC. Rendering the raw date would show
      // the wrong day for every job created after 10am local.
      final utc = DateTime.utc(2026, 8, 11, 22);
      final local = utc.toLocal();

      expect(dayFirstDate(utc.toIso8601String()), startsWith('${local.day} '));
    });

    test('a missing or unparseable timestamp is nothing, not an error', () {
      // Every timestamp in this API is optional in some state — a draft has no `expires_at` —
      // so a screen asking for one it does not have is ordinary. `null` lets the caller render
      // nothing rather than the word "null".
      expect(dayFirstDate(null), isNull);
      expect(dayFirstDate(''), isNull);
      expect(dayFirstDate('next tuesday'), isNull);
    });
  });

  group('money is AUD, from cents', () {
    test('renders cents as dollars with two decimal places', () {
      expect(audFromCents(150000), r'$1,500.00');
      expect(audFromCents(1550), r'$15.50');
      expect(audFromCents(5), r'$0.05');
      expect(audFromCents(0), r'$0.00');
    });

    test('groups thousands, and does not put a separator in front of the first digit', () {
      expect(audFromCents(10000), r'$100.00');
      expect(audFromCents(100000), r'$1,000.00');
      expect(audFromCents(100000000), r'$1,000,000.00');
    });

    test('the sign goes before the symbol', () {
      expect(audFromCents(-1250), r'-$12.50');
    });

    test('the conversion happens here and not earlier', () {
      // Docs/10 §3.3: money is never a float. A cents value that has been through a double is a
      // value that can be out by a cent, and this is the only place the division happens.
      expect(audFromCents(999999), r'$9,999.99');
    });
  });
}
