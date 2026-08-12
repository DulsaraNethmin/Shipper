/// Money, rendered for an Australian customer.
///
/// Currency is AUD (`CLAUDE.md`) and there is no second one, so nothing here takes a currency
/// argument — a parameter with one possible value is a parameter somebody eventually passes the
/// wrong thing to.
///
/// ## Cents in, never dollars
///
/// The platform holds and publishes money as a whole number of **cents** (`Docs/10` §3.3, and
/// `budget_cents` in `contracts/paths/jobs.yaml`), because money is never a float and a JSON
/// number written as `1500.50` is one. This client keeps that: the conversion to dollars happens
/// here, at the point of display, and nowhere earlier. An `int` that has been through a `double`
/// is an `int` that can be out by a cent.
library;

/// An amount in cents as Australian currency: `150000` becomes `$1,500.00`.
///
/// A bare `$` rather than `A$`, which is what a domestic Australian screen shows; the prefix is
/// for distinguishing currencies, and this app has one.
///
/// Negative amounts are rendered with the sign before the symbol — `-$12.50` — which is what a
/// refund or an adjustment would want. Nothing produces one today; it is one line, and the
/// alternative is `$-12.50`.
String audFromCents(int cents) {
  final negative = cents < 0;
  final magnitude = negative ? -cents : cents;

  final dollars = _grouped(magnitude ~/ 100);
  final remainder = (magnitude % 100).toString().padLeft(2, '0');

  return '${negative ? '-' : ''}\$$dollars.$remainder';
}

/// Thousands separated by commas: `1500` becomes `1,500`.
String _grouped(int whole) {
  final digits = whole.toString();
  final buffer = StringBuffer();

  for (var i = 0; i < digits.length; i++) {
    // A separator before every digit whose distance from the end is a multiple of three, except
    // at the very start — which is what stops `100` becoming `,100`.
    if (i > 0 && (digits.length - i) % 3 == 0) buffer.write(',');
    buffer.write(digits[i]);
  }

  return buffer.toString();
}
