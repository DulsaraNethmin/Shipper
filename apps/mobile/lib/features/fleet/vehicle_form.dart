import 'package:flutter/material.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/fleet/vehicle.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/validation/validators.dart';

/// The form that adds a vehicle and the form that edits one — the same form (SHIP-98).
///
/// One widget, because a field a provider can set when adding a vehicle is a field they can change
/// afterwards. The contract makes the same call from its own side: `VehicleInput` is one schema for
/// both writing operations, and "two schemas would be two places for that list to drift" is its
/// reasoning as much as it is this file's.
///
/// ## Only two things are checked here, and neither of them is a rule
///
/// `Docs/07` §2 puts the rules on the platform and leaves the app presence and shapes:
///
/// - **Presence**, for the two fields the platform requires when adding — a plate and a type. A
///   round trip to be told a field was left blank is a round trip on mobile data in a truck yard.
/// - **Parseability**, for the four measurements, because this form has to turn what was typed into
///   a JSON number and cannot send `12 tonnes`.
///
/// **No bound is checked.** The maximum weight and the maximum dimension are limits
/// `internal/fleet/service.go` holds, `Docs/06` §5.3 keeps them server-side, and a copy compiled in
/// here could not be corrected without a store release. Nor is the plate's shape checked: the six
/// states and two territories issue different ones and personalised plates exist in all of them, so
/// a pattern here would be wrong within a year — which is exactly why the contract does not enforce
/// one either.
///
/// What the platform refuses arrives as `validation_failed` with one `details` entry per offending
/// field, keyed by the contract's own field names, and this form renders each under the input that
/// caused it.
class VehicleForm extends StatefulWidget {
  const VehicleForm({
    required this.initial,
    required this.submitLabel,
    required this.busy,
    required this.onSubmit,
    this.failure,
    this.onCancel,
    super.key,
  });

  /// What the form starts from — an empty [VehicleInput] when adding, and
  /// `VehicleInput.from(vehicle)` when editing.
  final VehicleInput initial;

  final String submitLabel;

  /// Whether a save is in flight. The submit is disabled while it is true, which is what stops a
  /// second tap becoming a second vehicle.
  final bool busy;

  /// What the last attempt failed with. Field-level messages go under their inputs; anything else
  /// goes in the banner.
  final ApiFailure? failure;

  final void Function(VehicleInput vehicle) onSubmit;

  /// Offered when there is something to go back to — editing an existing vehicle. Absent on the add
  /// screen, which has the app bar's own close affordance.
  final VoidCallback? onCancel;

  @override
  State<VehicleForm> createState() => _VehicleFormState();
}

class _VehicleFormState extends State<VehicleForm> {
  final _form = GlobalKey<FormState>();

  late final TextEditingController _registration;
  late final TextEditingController _make;
  late final TextEditingController _model;
  late final TextEditingController _maxWeight;
  late final TextEditingController _length;
  late final TextEditingController _width;
  late final TextEditingController _height;

  VehicleType? _type;

  /// Fields the provider has touched since the platform's last answer.
  ///
  /// A server message that outlives the value it was about is worse than none: it sends somebody
  /// looking for a mistake in a plate they have already corrected.
  final _edited = <String>{};

  @override
  void initState() {
    super.initState();

    _registration = TextEditingController(text: widget.initial.registration);
    _make = TextEditingController(text: widget.initial.make);
    _model = TextEditingController(text: widget.initial.model);
    _maxWeight = TextEditingController(text: _decimalText(widget.initial.maxWeightKg));
    _length = TextEditingController(text: _wholeText(widget.initial.loadLengthCm));
    _width = TextEditingController(text: _wholeText(widget.initial.loadWidthCm));
    _height = TextEditingController(text: _wholeText(widget.initial.loadHeightCm));
    _type = widget.initial.vehicleType;
  }

  @override
  void didUpdateWidget(VehicleForm oldWidget) {
    super.didUpdateWidget(oldWidget);

    // A new answer from the platform. Whatever was edited since the previous one is no longer a
    // reason to hide a message, because these messages are about what was just sent.
    if (!identical(widget.failure, oldWidget.failure)) _edited.clear();
  }

  @override
  void dispose() {
    _registration.dispose();
    _make.dispose();
    _model.dispose();
    _maxWeight.dispose();
    _length.dispose();
    _width.dispose();
    _height.dispose();
    super.dispose();
  }

  /// A number as a form shows one: `0` is "not stated" and shows as empty, and a whole number of
  /// kilograms shows without a decimal point nobody typed.
  static String _decimalText(double value) {
    if (value == 0) return '';
    return value == value.roundToDouble() ? '${value.toInt()}' : '$value';
  }

  static String _wholeText(int value) => value == 0 ? '' : '$value';

  /// What the platform said about each field, keyed by the contract's own field names.
  Map<String, String> get _serverErrors => switch (widget.failure) {
        ApiErrorResponse(:final fieldMessages) => fieldMessages,
        _ => const <String, String>{},
      };

  /// The platform's message for [field], unless the provider has since changed it.
  String? _serverMessage(String field) =>
      _edited.contains(field) ? null : _serverErrors[field];

  void _touched(String field) {
    if (_edited.add(field)) setState(() {});
  }

  void _submit() {
    if (!(_form.currentState?.validate() ?? false)) return;

    widget.onSubmit(
      VehicleInput(
        registration: _registration.text.trim(),
        vehicleType: _type,
        make: _make.text.trim(),
        model: _model.text.trim(),
        // The form has already refused anything unparseable, so the fallback is only reached for an
        // empty field — which is what `0` means to the platform: not stated, or cleared.
        maxWeightKg: double.tryParse(_maxWeight.text.trim()) ?? 0,
        loadLengthCm: int.tryParse(_length.text.trim()) ?? 0,
        loadWidthCm: int.tryParse(_width.text.trim()) ?? 0,
        loadHeightCm: int.tryParse(_height.text.trim()) ?? 0,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final failure = widget.failure;
    final showBanner = failure != null && _serverErrors.isEmpty;

    // A type this build cannot name is offered back to itself, so a provider editing such a vehicle
    // can save the make without being forced to reclassify a truck they did not misclassify. It is
    // omitted from the request rather than sent — see `VehicleInput.toJson`.
    final unnamed = _type == VehicleType.unknown;
    final types = <VehicleType>[
      if (unnamed) VehicleType.unknown,
      ...VehicleType.selectable,
    ];

    return Form(
      key: _form,
      // What autovalidates is presence, parseability, and the platform's own answer. It is what
      // makes a server message disappear the moment the provider edits the value it was about,
      // rather than sitting under a field that has since been corrected.
      autovalidateMode: AutovalidateMode.onUserInteraction,
      child: ListView(
        key: const Key('vehicle-form'),
        padding: const EdgeInsets.all(24),
        children: [
          if (showBanner) ...[
            FailureBanner(failure),
            const SizedBox(height: 16),
          ],

          TextFormField(
            key: const Key('vehicle-registration'),
            controller: _registration,
            textCapitalization: TextCapitalization.characters,
            textInputAction: TextInputAction.next,
            decoration: const InputDecoration(
              labelText: 'Registration',
              // Said, because the plate comes back changed and a provider who typed `abc 123` should
              // know why the list shows `ABC123` rather than wondering what happened to it.
              helperText: 'Stored in capitals with the spaces removed.',
            ),
            onChanged: (_) => _touched('registration'),
            validator: (value) =>
                _serverMessage('registration') ??
                ((value ?? '').trim().isEmpty ? 'Enter the registration.' : null),
          ),
          const SizedBox(height: 16),

          DropdownButtonFormField<VehicleType>(
            key: const Key('vehicle-type'),
            initialValue: _type,
            decoration: InputDecoration(
              labelText: 'Kind of vehicle',
              helperText: unnamed
                  ? 'This version has no name for the type on record. Leave it as it is, or choose '
                      'one of these.'
                  : null,
            ),
            items: [
              for (final type in types)
                DropdownMenuItem<VehicleType>(
                  value: type,
                  // The key is on the label rather than on the item, and that is not cosmetic: the
                  // open menu rebuilds each item's *child*, so a key on the item names only the
                  // copy laid out inside the closed button — which a test can find and cannot tap.
                  child: Text(type.label, key: Key('vehicle-type-${type.wireName}')),
                ),
            ],
            onChanged: (value) {
              setState(() => _type = value);
              _touched('vehicle_type');
            },
            validator: (value) =>
                _serverMessage('vehicle_type') ??
                (value == null ? 'Choose what kind of vehicle this is.' : null),
          ),
          const SizedBox(height: 24),

          Text('Optional', style: theme.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            // Docs/01 §4.2's measure is that a provider can maintain a fleet, and a form that
            // insisted on a load height would be filled in with guesses.
            'Add what you know. You can fill the rest in later, and leaving a box empty clears '
            'anything recorded there.',
            key: const Key('vehicle-optional-note'),
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 16),

          TextFormField(
            key: const Key('vehicle-make'),
            controller: _make,
            textInputAction: TextInputAction.next,
            decoration: const InputDecoration(labelText: 'Make'),
            onChanged: (_) => _touched('make'),
            validator: (_) => _serverMessage('make'),
          ),
          const SizedBox(height: 16),

          TextFormField(
            key: const Key('vehicle-model'),
            controller: _model,
            textInputAction: TextInputAction.next,
            decoration: const InputDecoration(labelText: 'Model'),
            onChanged: (_) => _touched('model'),
            validator: (_) => _serverMessage('model'),
          ),
          const SizedBox(height: 16),

          TextFormField(
            key: const Key('vehicle-max-weight'),
            controller: _maxWeight,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            textInputAction: TextInputAction.next,
            decoration: const InputDecoration(
              labelText: 'Maximum load',
              // Kilograms, because CLAUDE.md fixes the units for the whole product and a number
              // with no unit beside it is a number somebody reads as tonnes.
              suffixText: 'kg',
            ),
            onChanged: (_) => _touched('max_weight_kg'),
            validator: (value) => _serverMessage('max_weight_kg') ?? Validators.decimal(value),
          ),
          const SizedBox(height: 24),

          Text('Load space', style: theme.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            // The distinction the contract draws, in the words a provider would use: a customer's
            // 190 cm sofa has to fit inside, not alongside.
            'The space inside, in centimetres — not the size of the vehicle.',
            key: const Key('vehicle-load-space-note'),
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 16),

          _dimension(
            fieldKey: 'vehicle-load-length',
            controller: _length,
            label: 'Length',
            field: 'load_length_cm',
          ),
          const SizedBox(height: 16),
          _dimension(
            fieldKey: 'vehicle-load-width',
            controller: _width,
            label: 'Width',
            field: 'load_width_cm',
          ),
          const SizedBox(height: 16),
          _dimension(
            fieldKey: 'vehicle-load-height',
            controller: _height,
            label: 'Height',
            field: 'load_height_cm',
          ),
          const SizedBox(height: 32),

          FilledButton(
            key: const Key('vehicle-submit'),
            onPressed: widget.busy ? null : _submit,
            child: widget.busy
                ? const SizedBox(
                    height: 20,
                    width: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : Text(widget.submitLabel),
          ),

          if (widget.onCancel case final cancel?) ...[
            const SizedBox(height: 8),
            TextButton(
              key: const Key('vehicle-cancel'),
              onPressed: widget.busy ? null : cancel,
              child: const Text('Discard changes'),
            ),
          ],
        ],
      ),
    );
  }

  Widget _dimension({
    required String fieldKey,
    required TextEditingController controller,
    required String label,
    required String field,
  }) {
    return TextFormField(
      key: Key(fieldKey),
      controller: controller,
      keyboardType: TextInputType.number,
      textInputAction: TextInputAction.next,
      decoration: InputDecoration(labelText: label, suffixText: 'cm'),
      onChanged: (_) => _touched(field),
      validator: (value) => _serverMessage(field) ?? Validators.wholeNumber(value),
    );
  }
}
