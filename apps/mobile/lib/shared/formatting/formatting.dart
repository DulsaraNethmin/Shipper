/// Presentation formatting (`Docs/07` §2).
///
/// The conventions are fixed in `CLAUDE.md`: currency is AUD, distances are kilometres, and
/// dates in user-facing copy are day-first. A date rendered month-first to an Australian
/// customer is not a cosmetic bug — 03/04 is a different day.
///
/// - `dates.dart` — day-first dates, converted to the device's own time zone (SHIP-76).
/// - `money.dart` — an amount in cents as Australian currency (SHIP-76).
///
/// Distances arrive with the provider feed's eligibility radius and have no home here yet.
///
/// **These are formatters and never parsers.** What the platform sends is the value; what these
/// produce is copy. A round trip through one of them — rendering a date and reading it back, or
/// turning `$1,500.00` into cents to send — is how a client ends up disagreeing with the platform
/// about somebody's money.
library;
