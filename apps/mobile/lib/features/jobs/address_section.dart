import 'package:flutter/material.dart';

import 'package:shipper/features/jobs/job.dart';

/// The four text controllers of one address, kept together so a screen does not carry eight.
///
/// Extracted from `job_locations_screen.dart` at SHIP-75, which needs the same form twice: once
/// for the step that **creates** a draft and once for the step that **edits** a saved one. The two
/// differ in which request they send and in nothing a customer can see, so the form is shared and
/// the request is not.
class AddressFields {
  final line = TextEditingController();
  final suburb = TextEditingController();
  final state = TextEditingController();
  final postcode = TextEditingController();

  /// Fills the four controllers from an address the platform holds.
  ///
  /// What a customer returning to a saved draft starts from (SHIP-75). It takes the parts as they
  /// were stored rather than the geocoder's rendering of them: the coordinate is what the platform
  /// made of the address, and putting that in the boxes would change the customer's own words
  /// under them.
  void seed(JobLocation? location) {
    if (location == null) return;
    line.text = location.line;
    suburb.text = location.suburb;
    state.text = location.state;
    postcode.text = location.postcode;
  }

  /// What is in the fields, untouched.
  ///
  /// Trimmed and nothing else. The platform collapses whitespace, resolves the state and strips
  /// spaces from the postcode; a second normaliser on the device is how a client ends up unable to
  /// reproduce the address it sent.
  AddressInput get value => AddressInput(
        line: line.text.trim(),
        suburb: suburb.text.trim(),
        state: state.text.trim(),
        postcode: postcode.text.trim(),
      );

  void dispose() {
    line.dispose();
    suburb.dispose();
    state.dispose();
    postcode.dispose();
  }
}

/// One address, as a form asks for it.
///
/// **Nothing here validates.** There is no local check on any of the four inputs — not a postcode
/// pattern, not a length, not a list of the eight states. `Docs/07` §2 puts the rules on the
/// platform and `Docs/06` §5.3 keeps validation limits server-side, because Dart has no
/// over-the-air update path: a limit compiled in here cannot be corrected without a store release.
///
/// A client-side postcode rule would refuse an address the platform accepts; a client-side list of
/// states would refuse a territory renamed server-side; and a client-side "all four are required"
/// rule would refuse the empty address `Docs/01` §4.1 explicitly allows, because a draft may be
/// saved half-finished and returned to.
///
/// What the platform decides arrives as `validation_failed` with one entry per offending field
/// under a dotted path — `pickup.postcode`, `dropoff.state` — which [messageFor] is given and
/// which is rendered inline beside the input that caused it.
class AddressSection extends StatelessWidget {
  const AddressSection({
    required this.title,
    required this.path,
    required this.fields,
    required this.messageFor,
    required this.onEdited,
    super.key,
  });

  final String title;

  /// The dotted prefix the platform names this address by — `pickup` or `dropoff`.
  final String path;

  final AddressFields fields;

  /// What the platform said about one dotted field, or `null`.
  final String? Function(String field) messageFor;

  /// Called with the dotted field a customer has just changed, so a message that outlives the
  /// value it was about can be cleared. A server message under a corrected field is worse than
  /// none.
  final void Function(String field) onEdited;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(title, style: theme.textTheme.titleMedium),
        const SizedBox(height: 12),
        TextFormField(
          key: Key('$path-line'),
          controller: fields.line,
          textInputAction: TextInputAction.next,
          decoration: const InputDecoration(
            labelText: 'Street address',
            // Freeform on the platform's side too: unit, level and lot numbers, PO boxes and
            // roadside mail boxes are all legitimate and none of them is parsed.
            helperText: 'Unit or level numbers, or anything a driver needs to find the door.',
          ),
          onChanged: (_) => onEdited('$path.line'),
          validator: (_) => messageFor('$path.line'),
        ),
        const SizedBox(height: 12),
        TextFormField(
          key: Key('$path-suburb'),
          controller: fields.suburb,
          textInputAction: TextInputAction.next,
          decoration: const InputDecoration(labelText: 'Suburb'),
          onChanged: (_) => onEdited('$path.suburb'),
          validator: (_) => messageFor('$path.suburb'),
        ),
        const SizedBox(height: 12),
        Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(
              child: TextFormField(
                key: Key('$path-state'),
                controller: fields.state,
                textInputAction: TextInputAction.next,
                // A text field rather than a picker, deliberately. The eight states are the
                // platform's list and it accepts any case and the spelled-out name, so a picker
                // here would be a second copy of that list compiled into a build that cannot be
                // updated over the air (`Docs/10` §4.7, `Docs/07` §1).
                textCapitalization: TextCapitalization.characters,
                decoration: const InputDecoration(
                  labelText: 'State',
                  helperText: 'NSW, or New South Wales',
                ),
                onChanged: (_) => onEdited('$path.state'),
                validator: (_) => messageFor('$path.state'),
              ),
            ),
            const SizedBox(width: 12),
            Expanded(
              child: TextFormField(
                key: Key('$path-postcode'),
                controller: fields.postcode,
                keyboardType: TextInputType.number,
                textInputAction: TextInputAction.next,
                decoration: const InputDecoration(
                  labelText: 'Postcode',
                  helperText: 'Four digits',
                ),
                onChanged: (_) => onEdited('$path.postcode'),
                validator: (_) => messageFor('$path.postcode'),
              ),
            ),
          ],
        ),
      ],
    );
  }
}
