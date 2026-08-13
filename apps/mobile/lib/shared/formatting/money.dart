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

/// What somebody typed into a dollars field, as a whole number of cents — or `null` when it is not
/// an amount at all.
///
/// The inverse of [audFromCents], beside it so the two conventions cannot drift. SHIP-100 is the
/// first screen that takes money *from* a person: a provider types their price for a job, and
/// `POST /v1/jobs/{id}/bids` takes `amount_cents` as a whole number.
///
/// **The conversion is done on the text and never through a `double`**, which is the whole reason
/// this is not one line. `(double.parse('450.55') * 100).round()` is right far more often than it is
/// wrong, and the times it is wrong are a cent adrift on somebody's price with nothing in the code
/// that looks like a mistake. Splitting the string keeps every digit that was typed.
///
/// What it accepts is what people type into a price box: a leading `$`, spaces, commas between
/// thousands, and no decimal point at all. What it refuses — by answering `null` — is anything that
/// is not a number, more than one decimal point, a negative amount, and **more than two decimal
/// places**, because `45.005` is a price the platform cannot store and rounding it quietly would be
/// this client deciding what the offer was.
///
/// No upper bound is checked. `internal/bidding` holds `maxOfferCents` and `Docs/06` §5.3 keeps
/// limits server-side; a copy compiled in here could not be corrected without a store release, and
/// what comes back instead is `out_of_range` with the real number in it, under the input that caused
/// it.
int? centsFromAud(String? typed) {
  final input = (typed ?? '').trim().replaceAll(RegExp(r'[\s,\$]'), '');
  if (input.isEmpty) return null;

  final parts = input.split('.');
  if (parts.length > 2) return null;

  final dollars = parts[0];
  final fraction = parts.length == 2 ? parts[1] : '';

  // A bare `.50` is what somebody types when they mean fifty cents, so an empty dollars part is
  // allowed — but not when the fraction is empty too, which is a lone full stop.
  if (dollars.isEmpty && fraction.isEmpty) return null;
  if (fraction.length > 2) return null;
  if (!_digitsOnly(dollars) || !_digitsOnly(fraction)) return null;

  final whole = dollars.isEmpty ? 0 : int.tryParse(dollars);
  if (whole == null) return null;

  // `.5` is fifty cents and not five: the fraction is padded on the **right**, which is what the
  // decimal point means and is the direction it is easy to get backwards.
  final cents = fraction.isEmpty ? 0 : int.parse(fraction.padRight(2, '0'));

  return whole * 100 + cents;
}

/// Whether every character is a digit. The empty string is handled by the caller.
bool _digitsOnly(String value) =>
    value.codeUnits.every((unit) => unit >= 0x30 && unit <= 0x39);

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
