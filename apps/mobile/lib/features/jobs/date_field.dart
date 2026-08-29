import 'dart:async';

import 'package:flutter/material.dart';

import 'package:shipper/shared/formatting/dates.dart';

/// One date, chosen on a calendar.
///
/// ## A date, where `features/bidding` needs an instant
///
/// `InstantField` is the same shape one feature over and is deliberately not reused — not because
/// features may not import one another (they may not, `Docs/07` §2), but because it would be the
/// wrong control. A bid is a **commitment**: "I will collect at nine and deliver by five", and the
/// clock is half of what is being promised. A job's pickup window is a **constraint**: "any time
/// on Tuesday or Wednesday", and asking a customer for a time they do not have narrows the job for
/// no reason. `contracts/paths/jobs.yaml` says the same thing in its own words — a window rather
/// than an instant, "because road transport is not scheduled to the minute and a customer who says
/// 'Tuesday or Wednesday' gets more bids than one who says '10:15'".
///
/// So this asks for a day, and the time of day is derived from which end of a window it is. See
/// [dayStart] and [dayEnd].
///
/// A `FormField` rather than a button and a label, so the platform's own message about the field
/// lands where every other field's does — a value validated somewhere else is a value somebody
/// forgets.
///
/// **The calendar's range is what a calendar has to be drawn over, and not a rule.** It starts
/// today because a picker offering last Tuesday guarantees a wasted round trip, and ends far
/// enough out that nothing legitimate is unreachable. Whether the window is actually acceptable —
/// and whether the end follows the start — is `internal/jobs`' answer and arrives as a field
/// message. `Docs/06` §5.3 keeps validation limits server-side, and a rule compiled in here could
/// not be corrected without a store release.
class DateField extends StatelessWidget {
  const DateField({
    required this.fieldKey,
    required this.label,
    required this.value,
    required this.onChanged,
    this.helperText,
    this.serverMessage,
    this.unset = 'Not chosen yet',
    super.key,
  });

  /// The prefix every key on this field is built from, so a test names one thing.
  final String fieldKey;

  final String label;
  final DateTime? value;
  final ValueChanged<DateTime?> onChanged;
  final String? helperText;

  /// What the platform said about this field, or `null`.
  final String? serverMessage;

  /// What to show while [value] is null.
  final String unset;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final chosen = value;

    return InputDecorator(
      key: Key(fieldKey),
      decoration: InputDecoration(
        labelText: label,
        helperText: helperText,
        errorText: serverMessage,
        border: const OutlineInputBorder(),
      ),
      child: Row(
        children: [
          Expanded(
            child: InkWell(
              key: Key('$fieldKey-choose'),
              onTap: () => unawaited(_choose(context)),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: 8),
                child: Text(
                  chosen == null ? unset : dayFirstDate(chosen.toIso8601String()) ?? unset,
                  style: chosen == null
                      ? theme.textTheme.bodyLarge
                          ?.copyWith(color: theme.colorScheme.onSurfaceVariant)
                      : theme.textTheme.bodyLarge,
                ),
              ),
            ),
          ),
          if (chosen != null)
            IconButton(
              key: Key('$fieldKey-clear'),
              icon: const Icon(Icons.close),
              // Clearing is how a customer says "no latest date after all", and the contract has
              // an explicit clearing value for it — the empty string — rather than treating an
              // absent field as a clear.
              tooltip: 'Clear $label',
              onPressed: () => onChanged(null),
            ),
        ],
      ),
    );
  }

  Future<void> _choose(BuildContext context) async {
    final now = DateTime.now();
    final today = DateTime(now.year, now.month, now.day);

    final picked = await showDatePicker(
      context: context,
      initialDate: value ?? today,
      firstDate: today,
      lastDate: DateTime(now.year + 2, now.month, now.day),
    );

    if (picked != null) onChanged(picked);
  }
}

/// The chosen day as the **start** of that day, local: `2026-09-03T00:00:00+10:00`.
///
/// A window's opening edge is the first moment of the day somebody picked. Anything later would
/// exclude part of a day the customer said was fine.
String dayStart(DateTime day) => rfc3339(DateTime(day.year, day.month, day.day));

/// The chosen day as the **end** of that day, local: `2026-09-05T23:59:59+10:00`.
///
/// This is the one worth stating plainly, because the obvious implementation is wrong in a way
/// nobody notices until a delivery is refused. A window that ends at midnight *on* the chosen day
/// ends before that day has happened — a customer who says "collect by Friday" would have said
/// "collect by Thursday night". The closing edge is therefore the last second of the day, not the
/// first.
String dayEnd(DateTime day) =>
    rfc3339(DateTime(day.year, day.month, day.day, 23, 59, 59));

/// A timestamp from the platform as the day it falls on, in the device's own zone — or `null`.
///
/// The inverse of [dayStart] and [dayEnd] for the purpose the form needs: a stored window comes
/// back as an instant, and the picker takes a day. The conversion is to **local** time, which
/// matters here for the same reason `dayFirstDate` documents — a window ending at
/// `2026-09-05T13:59:59Z` is the 5th in Sydney and the 5th is what the customer chose.
DateTime? dayOf(String? timestamp) {
  if (timestamp == null || timestamp.isEmpty) return null;
  final at = DateTime.tryParse(timestamp)?.toLocal();
  if (at == null) return null;
  return DateTime(at.year, at.month, at.day);
}
