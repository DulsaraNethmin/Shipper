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

DateTime? _parse(String? timestamp) {
  if (timestamp == null || timestamp.isEmpty) return null;
  return DateTime.tryParse(timestamp)?.toLocal();
}
