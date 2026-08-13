/// Dates, rendered the way an Australian customer reads them.
///
/// **Day-first, always** (`CLAUDE.md`). A date rendered month-first to an Australian customer is
/// not a cosmetic bug: 03/04 is a different day, and the person reading it has no way to tell
/// which convention the screen used.
///
/// The month is spelled rather than numbered — `3 Apr 2026` — which removes the ambiguity
/// altogether rather than relying on the reader knowing the convention. Numeric day-first
/// remains correct and is what a compact table would use; nothing in the app needs one yet.
///
/// ## No `intl`, and that is a decision rather than an omission
///
/// `intl` is the obvious package for this and is deliberately not added. The MVP ships one
/// locale — the marketplace is Australian, currency is AUD, distances are kilometres — so what
/// the package would buy is locale negotiation nobody wants: a device set to `en-US` would start
/// rendering month-first, which is the exact defect this file exists to prevent. It becomes worth
/// adding when a second locale does, and not before.
library;

/// The month names, indexed from one so the month number reads straight in.
const _months = <String>[
  '',
  'Jan',
  'Feb',
  'Mar',
  'Apr',
  'May',
  'Jun',
  'Jul',
  'Aug',
  'Sep',
  'Oct',
  'Nov',
  'Dec',
];

/// A timestamp from the API as a day-first date: `25 Aug 2026`.
///
/// Returns `null` for a missing or unparseable value, so a caller renders nothing rather than
/// `null` or `Invalid date`. Every timestamp the platform sends is optional in some state — a
/// draft has no `expires_at` — and a screen asking for one it does not have is ordinary.
///
/// **Converted to the device's own time zone**, which matters more than it looks. The platform
/// sends UTC with a `Z`, and Australia is between eight and eleven hours ahead of it: a job
/// created at 8am in Sydney is `2026-08-11T22:00:00Z` the day *before*. Rendering the raw UTC
/// date would show a customer the wrong day for every job created after 10am local.
String? dayFirstDate(String? timestamp) {
  final at = _parse(timestamp);
  if (at == null) return null;
  return '${at.day} ${_months[at.month]} ${at.year}';
}

/// A timestamp from the API as a day-first date **and a time**: `20 Aug 2026, 9:00 am`.
///
/// The same conversion and the same `null` behaviour as [dayFirstDate], with the time added —
/// because a *commitment* about when a vehicle arrives is a different thing from a date. A bid says
/// "I will be there at nine and it will be there by five" (SHIP-84), and a screen that rendered only
/// the day would drop the half of the offer the customer is comparing.
///
/// Twelve-hour with `am` and `pm` in lower case, which is how an Australian screen writes it.
String? dayFirstDateTime(String? timestamp) {
  final at = _parse(timestamp);
  if (at == null) return null;
  return '${at.day} ${_months[at.month]} ${at.year}, ${_clock(at)}';
}

/// A local instant as RFC 3339, **carrying the device's own offset**: `2026-08-20T09:00:00+10:00`.
///
/// This exists because `DateTime.toIso8601String()` on a local value produces
/// `2026-08-20T09:00:00.000` with **no offset at all**. That is not RFC 3339, and the platform's
/// `time.Parse(time.RFC3339, …)` refuses it — which reaches the provider as "that is not a date"
/// about a date they picked from a calendar.
///
/// The offset is sent rather than the value being converted to UTC here. The platform parses RFC
/// 3339 and normalises to UTC itself, so converting on the device would be a second timezone
/// conversion to keep correct, running on a handset whose zone is whatever it was last set to.
String rfc3339(DateTime at) {
  final local = at.toLocal();
  final offset = local.timeZoneOffset;
  final minutes = offset.inMinutes.abs();

  return '${_four(local.year)}-${_two(local.month)}-${_two(local.day)}'
      'T${_two(local.hour)}:${_two(local.minute)}:${_two(local.second)}'
      '${offset.isNegative ? '-' : '+'}${_two(minutes ~/ 60)}:${_two(minutes % 60)}';
}

/// Twelve-hour clock time: `9:05 am`, `12:00 pm`.
String _clock(DateTime at) {
  final hour = at.hour % 12 == 0 ? 12 : at.hour % 12;
  return '$hour:${_two(at.minute)} ${at.hour < 12 ? 'am' : 'pm'}';
}

String _two(int value) => value.toString().padLeft(2, '0');

String _four(int value) => value.toString().padLeft(4, '0');

DateTime? _parse(String? timestamp) {
  if (timestamp == null || timestamp.isEmpty) return null;
  return DateTime.tryParse(timestamp)?.toLocal();
}
