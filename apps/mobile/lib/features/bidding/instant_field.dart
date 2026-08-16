import 'dart:async';

import 'package:flutter/material.dart';

import 'package:shipper/shared/formatting/dates.dart';

/// One instant, chosen on a calendar and a clock.
///
/// A `FormField` rather than a button and a label, so that presence and the platform's own message
/// land in the same place as every other field on a form — a required value that validated somewhere
/// else is a required value somebody forgets.
///
/// **The calendar's range is what a calendar has to be drawn over, and not a rule.** It starts today
/// because a date picker offering last Tuesday guarantees a wasted round trip, and it ends far
/// enough out that nothing legitimate is unreachable. Whether the chosen instant is actually in the
/// future, and whether delivery follows collection, are `internal/bidding`'s answers and arrive as
/// field messages.
///
/// ## Why it is not private to one panel any more
///
/// It was `_InstantField` inside `place_bid_panel.dart` until SHIP-103, which needs the same control
/// on the counter-offer form: a counter is an offer with different terms, so the terms are entered
/// the same way or the two forms disagree about what a commitment is. `Docs/07` §2 moves shared
/// behaviour out of the place that happened to write it first — the same call the client made for
/// `ProviderOnly` — and this stays inside `features/bidding` because both callers are here and
/// nothing outside the feature has any use for it.
///
/// [missing] is nullable in a way the private version was not: the placement form **requires** both
/// instants, and the counter form requires **neither** — an omitted field there means "keep the one
/// on the table". Passing `null` is how a caller says the field is optional.
class InstantField extends StatelessWidget {
  const InstantField({
    required this.fieldKey,
    required this.label,
    required this.value,
    required this.onChanged,
    this.missing,
    this.serverMessage,
    this.unset = 'Not chosen yet',
    super.key,
  });

  /// The prefix every key on this field is built from, so a test names one thing.
  final String fieldKey;

  final String label;
  final DateTime? value;

  /// What to say when nothing has been chosen, or `null` when choosing nothing is allowed.
  final String? missing;

  /// What the platform said about this field, or `null`.
  ///
  /// **It can arrive for a field nobody touched**, which is why it is separate from [missing]: the
  /// counter endpoint validates the merged offer, so a counter on price alone can be refused naming
  /// `pickup_at`.
  final String? serverMessage;

  /// What to show while [value] is null.
  final String unset;

  final void Function(DateTime at) onChanged;

  @override
  Widget build(BuildContext context) {
    return FormField<DateTime>(
      initialValue: value,
      validator: (_) => serverMessage ?? (value == null ? missing : null),
      builder: (field) {
        final chosen = value;

        return InputDecorator(
          decoration: InputDecoration(
            labelText: label,
            errorText: field.errorText,
            border: const OutlineInputBorder(),
          ),
          child: Row(
            children: [
              Expanded(
                child: Text(
                  chosen == null ? unset : dayFirstDateTime(chosen.toIso8601String())!,
                  key: Key('$fieldKey-value'),
                ),
              ),
              TextButton(
                key: Key(fieldKey),
                onPressed: () => unawaited(_choose(context, field)),
                child: Text(chosen == null ? 'Choose' : 'Change'),
              ),
            ],
          ),
        );
      },
    );
  }

  Future<void> _choose(BuildContext context, FormFieldState<DateTime> field) async {
    final now = DateTime.now();
    final start = value ?? now;

    final date = await showDatePicker(
      context: context,
      initialDate: start.isBefore(now) ? now : start,
      firstDate: DateTime(now.year, now.month, now.day),
      lastDate: DateTime(now.year + 5, now.month, now.day),
    );
    if (date == null || !context.mounted) return;

    final time = await showTimePicker(
      context: context,
      initialTime: TimeOfDay.fromDateTime(start),
      // Typed rather than dialled. A party committing to "09:00" types four digits; the dial is for
      // choosing an approximate time, and this is a commitment rather than a preference.
      initialEntryMode: TimePickerEntryMode.input,
    );
    if (time == null) return;

    final at = DateTime(date.year, date.month, date.day, time.hour, time.minute);
    field.didChange(at);
    onChanged(at);
  }
}
