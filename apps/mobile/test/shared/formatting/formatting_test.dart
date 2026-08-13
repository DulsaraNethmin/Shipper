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

  group('a date and a time, for a commitment rather than a day', () {
    test('renders day-first with a twelve-hour clock', () {
      // A bid says "I will be there at nine" (SHIP-84). A screen that rendered only the day would
      // drop the half of the offer the customer is comparing.
      final morning = DateTime(2026, 8, 20, 9, 5);
      final afternoon = DateTime(2026, 8, 20, 17);

      expect(dayFirstDateTime(morning.toIso8601String()), '20 Aug 2026, 9:05 am');
      expect(dayFirstDateTime(afternoon.toIso8601String()), '20 Aug 2026, 5:00 pm');
    });

    test('midnight and midday are 12, not 0', () {
      // `hour % 12` is 0 at both ends, and "0:00 am" is the bug that reads as plausible.
      expect(dayFirstDateTime(DateTime(2026, 8, 20).toIso8601String()), '20 Aug 2026, 12:00 am');
      expect(dayFirstDateTime(DateTime(2026, 8, 20, 12).toIso8601String()), '20 Aug 2026, 12:00 pm');
    });

    test('a missing or unparseable timestamp is nothing, not an error', () {
      expect(dayFirstDateTime(null), isNull);
      expect(dayFirstDateTime(''), isNull);
      expect(dayFirstDateTime('next tuesday'), isNull);
    });
  });

  group('an instant on its way to the platform carries an offset', () {
    test('is RFC 3339, with the zone and not without it', () {
      // The trap this function exists for: DateTime.toIso8601String() on a local value produces
      // `2026-08-20T09:00:00.000` with **no offset at all**, which time.Parse(time.RFC3339, …)
      // refuses — reaching the provider as "that is not a date" about a date they picked from a
      // calendar.
      final sent = rfc3339(DateTime(2026, 8, 20, 9, 30));

      expect(sent, startsWith('2026-08-20T09:30:00'));
      expect(
        sent,
        matches(RegExp(r'^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}[+-]\d{2}:\d{2}$')),
        reason: 'the platform parses RFC 3339, which requires an offset',
      );
      expect(sent, isNot(contains('.')), reason: 'no fractional seconds, which nothing needs here');
    });

    test('pads every part, so a single-digit month is two digits', () {
      expect(rfc3339(DateTime(2026, 1, 2, 3, 4, 5)), startsWith('2026-01-02T03:04:05'));
    });

    test('a UTC instant is rendered in the device’s own zone rather than as Z', () {
      // Deliberate: the platform normalises to UTC itself, so converting on the device would be a
      // second timezone conversion to keep correct. What matters is that the offset is stated.
      final at = DateTime.utc(2026, 8, 19, 23);

      expect(rfc3339(at), startsWith('${at.toLocal().year}-'));
      expect(rfc3339(at), isNot(endsWith('Z')));
    });
  });

  group('what somebody typed into a price box, as cents', () {
    test('whole dollars and cents both read straight in', () {
      expect(centsFromAud('450'), 45000);
      expect(centsFromAud('450.50'), 45050);
      expect(centsFromAud('0.99'), 99);
    });

    test('the fraction is padded on the right, not the left', () {
      // `.5` is fifty cents and not five. This is the direction it is easy to get backwards, and
      // getting it backwards is a bid ten times too small with nothing that looks like a mistake.
      expect(centsFromAud('450.5'), 45050);
      expect(centsFromAud('.5'), 50);
      expect(centsFromAud('.05'), 5);
    });

    test('accepts what people actually type into a price box', () {
      expect(centsFromAud(r'$450'), 45000);
      expect(centsFromAud('1,500'), 150000);
      expect(centsFromAud('  450.50  '), 45050);
    });

    test('refuses more than two decimal places rather than rounding them', () {
      // 45.005 is a price the platform cannot store, and rounding it quietly would be this client
      // deciding what the offer was.
      expect(centsFromAud('45.005'), isNull);
    });

    test('refuses anything that is not an amount', () {
      expect(centsFromAud(null), isNull);
      expect(centsFromAud(''), isNull);
      expect(centsFromAud('.'), isNull);
      expect(centsFromAud('four fifty'), isNull);
      expect(centsFromAud('450.50.25'), isNull);
      expect(centsFromAud('-450'), isNull);
      expect(centsFromAud('45e2'), isNull);
    });

    test('round-trips through the renderer', () {
      // The two conventions live in one file so they cannot drift, and this is what says so.
      for (final typed in <String>['450', '450.50', '1,000', '0.01']) {
        final cents = centsFromAud(typed)!;
        expect(centsFromAud(audFromCents(cents)), cents);
      }
    });

    test('a large amount is not turned into a double on the way through', () {
      // Docs/10 §3.3: money is never a float. `(double.parse('99999999.99') * 100).round()` is the
      // implementation this refuses to be, and it is wrong here by a cent.
      expect(centsFromAud('99999999.99'), 9999999999);
    });
  });
}
