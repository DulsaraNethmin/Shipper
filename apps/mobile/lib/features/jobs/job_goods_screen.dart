import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/goods_categories_repository.dart';
import 'package:shipper/features/jobs/goods_category.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_draft_controller.dart';
import 'package:shipper/features/jobs/job_wizard.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';

/// The second step of publishing a delivery: what is being moved (SHIP-72).
///
/// Category, description, dimensions and weight — the four things `Docs/09` asks this step to
/// capture, of which the platform requires the first two to publish and deliberately does not
/// require the last two.
///
/// ## The category list is fetched, never compiled in
///
/// `GET /v1/goods-categories` is the source (SHIP-58). `CLAUDE.md` puts category lists server-side
/// because they move under operational pressure and Flutter has no over-the-air path for Dart
/// code: a category withdrawn on legal advice has to stop being offered without a store release.
/// So this screen has **no fallback list**, and a catalogue that will not load leaves the picker
/// replaced by a retry rather than by a guess.
///
/// ## Refused categories are shown, and choosing one is allowed
///
/// A category with `carried: false` is in the list rather than left out, so a customer can see
/// what Shipper does not take instead of inferring it from an absence. Selecting one **saves**:
/// `Docs/01` §4.1 lets a customer sketch a job and come back, and somebody describing a load may
/// not have worked out yet that it will not be carried. What it does not do is publish —
/// `POST /v1/jobs/{id}/publish` refuses it by name, quoting the catalogue's own wording (SHIP-59).
///
/// That is `Docs/07` §3 exactly: the app may hide or disable, and the platform decides. This
/// screen warns and does not block.
class JobGoodsScreen extends StatelessWidget {
  const JobGoodsScreen({required this.jobId, super.key});

  final String jobId;

  @override
  Widget build(BuildContext context) {
    return JobWizardScaffold(
      jobId: jobId,
      step: JobWizardStep.goods,
      builder: (context, state, draft) => _GoodsForm(jobId: jobId, state: state, draft: draft),
    );
  }
}

/// The form, constructed only once there is a draft to seed it from.
///
/// A separate widget rather than a method, which is what lets the text controllers be seeded in
/// `initState` from what the platform holds: [JobWizardScaffold] does not call its builder until
/// the draft has arrived, so this is built with one and never without.
class _GoodsForm extends ConsumerStatefulWidget {
  const _GoodsForm({required this.jobId, required this.state, required this.draft});

  final String jobId;
  final JobDraftState state;
  final Job draft;

  @override
  ConsumerState<_GoodsForm> createState() => _GoodsFormState();
}

class _GoodsFormState extends ConsumerState<_GoodsForm> {
  final _form = GlobalKey<FormState>();

  final _description = TextEditingController();
  final _length = TextEditingController();
  final _width = TextEditingController();
  final _height = TextEditingController();
  final _weight = TextEditingController();

  /// The chosen category **code**, seeded from the draft.
  ///
  /// Seeded from the draft rather than from the catalogue, and that ordering is load-bearing: a
  /// catalogue that fails to load must not cost the customer the category their job already
  /// carries. [goodsBody] sends this field on every save, and the empty string clears it — so
  /// seeding from the catalogue would mean a draft resumed on a bad connection silently losing its
  /// category the next time the step saved.
  String _category = '';

  /// What the platform said about each field, keyed by the contract's own names.
  final _serverErrors = <String, String>{};

  @override
  void initState() {
    super.initState();

    final draft = widget.draft;
    _category = draft.goodsCategory ?? '';
    _description.text = draft.goodsDescription ?? '';
    _length.text = _measurement(draft.lengthCm);
    _width.text = _measurement(draft.widthCm);
    _height.text = _measurement(draft.heightCm);
    _weight.text = _weightOf(draft.weightKg);
  }

  /// A stored measurement as text, with **zero shown as blank**.
  ///
  /// The platform omits what is empty, so `null` is "not filled in" — but `0` is the value that
  /// *clears* a measurement, so a job that has been through this form once and had its length
  /// cleared comes back as `0` rather than as absent. Rendering that as "0" would tell a customer
  /// their sofa is zero centimetres long.
  static String _measurement(int? value) => (value == null || value == 0) ? '' : value.toString();

  static String _weightOf(double? value) {
    if (value == null || value == 0) return '';
    // Whole kilograms are the ordinary case and `45.0` reads like a precision nobody claimed.
    return value == value.roundToDouble() ? value.round().toString() : value.toString();
  }

  @override
  void dispose() {
    _description.dispose();
    _length.dispose();
    _width.dispose();
    _height.dispose();
    _weight.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(_serverErrors.clear);

    final saved = await ref.read(jobDraftProvider(widget.jobId).notifier).save(
          goodsBody(
            category: _category,
            description: _description.text.trim(),
            lengthCm: int.tryParse(_length.text.trim()),
            widthCm: int.tryParse(_width.text.trim()),
            heightCm: int.tryParse(_height.text.trim()),
            weightKg: double.tryParse(_weight.text.trim()),
          ),
        );

    if (!mounted) return;

    if (saved) {
      // The schedule step is SHIP-73 and does not exist yet, so this returns to the customer's
      // own jobs rather than to a route that is not registered. The draft is waiting there: every
      // step saves to the platform before it moves on.
      context.go(Routes.home);
      return;
    }

    setState(() {
      _serverErrors.addAll(_fieldMessagesFrom(ref.read(jobDraftProvider(widget.jobId)).failure));
    });
    _form.currentState?.validate();
  }

  /// Which of the platform's refusals belong under an input rather than in the banner.
  ///
  /// Only the ones it named a field for. `service_unavailable`, `jobs_not_a_draft` and a dropped
  /// connection are about the request rather than about a value somebody typed, and putting them
  /// under an input would send a customer looking for a mistake in a description that is fine.
  static Map<String, String> _fieldMessagesFrom(ApiFailure? failure) {
    return switch (failure) {
      ApiErrorResponse(:final fieldMessages) => fieldMessages,
      _ => const <String, String>{},
    };
  }

  void _clearServerError(String field) {
    if (_serverErrors.remove(field) != null) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final state = widget.state;
    final failure = state.failure;
    final showBanner = failure != null && _fieldMessagesFrom(failure).isEmpty;

    return Form(
      key: _form,
      // What autovalidates here is the *platform's* answer, not a local rule. It is what makes a
      // server message disappear the moment the customer edits the value it was about.
      autovalidateMode: AutovalidateMode.onUserInteraction,
      child: ListView(
        key: const Key('job-goods-form'),
        padding: const EdgeInsets.all(24),
        children: [
          Text(JobWizardStep.goods.heading, style: theme.textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(
            'The more a provider knows about the load, the closer their offer will be to what '
            'the job actually takes.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 24),
          if (showBanner) ...[
            FailureBanner(failure),
            const SizedBox(height: 16),
          ],
          _CategoryPicker(
            selected: _category,
            serverMessage: _serverErrors['goods_category'],
            onChanged: (code) {
              setState(() => _category = code);
              _clearServerError('goods_category');
            },
          ),
          const SizedBox(height: 24),
          TextFormField(
            key: const Key('goods-description'),
            controller: _description,
            minLines: 2,
            maxLines: 5,
            textCapitalization: TextCapitalization.sentences,
            decoration: const InputDecoration(
              labelText: 'What are you sending?',
              helperText: 'In your own words — "two-seater sofa, wrapped, no legs attached".',
            ),
            onChanged: (_) => _clearServerError('goods_description'),
            validator: (_) => _serverErrors['goods_description'],
          ),
          const SizedBox(height: 24),
          Text('Size and weight', style: theme.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            // Said plainly because it is true and because a customer who cannot measure a pallet
            // should not abandon the job here. `publishable` in `internal/jobs/publish.go` leaves
            // both out of the required set for exactly this reason.
            'Optional. You can publish without these, and a provider can ask if they need them.',
            key: const Key('goods-size-optional-note'),
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
          const SizedBox(height: 12),
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(child: _measurementField('goods-length', _length, 'Length', 'length_cm')),
              const SizedBox(width: 12),
              Expanded(child: _measurementField('goods-width', _width, 'Width', 'width_cm')),
              const SizedBox(width: 12),
              Expanded(child: _measurementField('goods-height', _height, 'Height', 'height_cm')),
            ],
          ),
          const SizedBox(height: 12),
          TextFormField(
            key: const Key('goods-weight'),
            controller: _weight,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: const InputDecoration(labelText: 'Weight', suffixText: 'kg'),
            onChanged: (_) => _clearServerError('weight_kg'),
            validator: (_) => _serverErrors['weight_kg'],
          ),
          const SizedBox(height: 32),
          FilledButton(
            key: const Key('job-goods-submit'),
            onPressed: state.saving ? null : () => unawaited(_submit()),
            child: state.saving
                ? const SizedBox(
                    height: 20,
                    width: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('Save and continue'),
          ),
        ],
      ),
    );
  }

  /// One of the three dimensions. Centimetres on all of them, stated once per field rather than
  /// once for the row, because a suffix on the last box is a suffix two boxes do not have.
  Widget _measurementField(
    String key,
    TextEditingController controller,
    String label,
    String field,
  ) {
    return TextFormField(
      key: Key(key),
      controller: controller,
      keyboardType: TextInputType.number,
      decoration: InputDecoration(labelText: label, suffixText: 'cm'),
      onChanged: (_) => _clearServerError(field),
      validator: (_) => _serverErrors[field],
    );
  }
}

/// The catalogue, as a list of choices.
///
/// A `FormField` so that the platform's message about `goods_category` lands in the same place as
/// every other field's — a required value validated somewhere else is a required value somebody
/// forgets.
///
/// **Radio tiles rather than a dropdown.** Entries carry a one-line description and a "we do not
/// carry this" marker, and a dropdown menu shows neither without a custom item builder that a
/// screen reader then has to be told about separately.
class _CategoryPicker extends ConsumerWidget {
  const _CategoryPicker({
    required this.selected,
    required this.onChanged,
    this.serverMessage,
  });

  final String selected;
  final ValueChanged<String> onChanged;
  final String? serverMessage;

  /// The choices, when the catalogue has arrived — `null` when it has not.
  ///
  /// A value present wins over an error attached to it, which is the same precedence
  /// `resolveAppPolicy` applies: a fetch that succeeded and is now being refreshed still has an
  /// answer worth drawing.
  Widget? _catalogueOf(AsyncValue<GoodsCatalogue> catalogue) {
    if (!catalogue.hasValue) return null;
    return _Choices(
      catalogue: catalogue.requireValue,
      selected: selected,
      onChanged: onChanged,
    );
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Type of goods', style: theme.textTheme.titleMedium),
        const SizedBox(height: 12),
        // Matching on **what is there** rather than on the `AsyncValue` subclass, which is the
        // trap `version_gate.dart` and `app_policy_controller.dart` both pay for: a provider whose
        // fetch failed is retried by Riverpod, and while it retries the state is `AsyncLoading`
        // *carrying an error* rather than `AsyncError`. A `switch` on `AsyncError()` therefore
        // never matches, and the picker sits on a spinner that never resolves.
        _catalogueOf(ref.watch(goodsCatalogueProvider)) ??
            (ref.watch(goodsCatalogueProvider).hasError
                ? _CatalogueUnavailable(selected: selected)
                : const Padding(
                    padding: EdgeInsets.symmetric(vertical: 16),
                    child: Center(
                      key: Key('goods-categories-loading'),
                      child: CircularProgressIndicator(),
                    ),
                  )),
        if (serverMessage case final message?) ...[
          const SizedBox(height: 8),
          Text(
            message,
            key: const Key('goods-category-error'),
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.error),
          ),
        ],
      ],
    );
  }
}

/// The catalogue as radio tiles, with the refused entries marked.
class _Choices extends StatelessWidget {
  const _Choices({required this.catalogue, required this.selected, required this.onChanged});

  final GoodsCatalogue catalogue;
  final String selected;
  final ValueChanged<String> onChanged;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final chosen = catalogue.byCode(selected);

    return Column(
      key: const Key('goods-categories'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        // The selection is held by a `RadioGroup` ancestor rather than by each tile's own
        // `groupValue` and `onChanged`, which Flutter deprecated after 3.32: the tiles carry a
        // value and nothing else, and the group owns which one is chosen.
        RadioGroup<String>(
          groupValue: selected,
          onChanged: (code) => onChanged(code ?? ''),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              for (final category in catalogue.categories)
                RadioListTile<String>(
                  key: Key('goods-category-${category.code}'),
                  value: category.code,
                  contentPadding: EdgeInsets.zero,
                  title: Text(category.label),
                  subtitle: switch (category) {
                    GoodsCategory(carried: false) => Text(
                        'Shipper does not carry this.',
                        style:
                            theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.error),
                      ),
                    GoodsCategory(:final description?) => Text(description),
                    _ => null,
                  },
                ),
            ],
          ),
        ),

        // The warning sits under the list rather than inside the chosen tile, so it reads as a
        // consequence of the choice rather than as a label on an option. It does not disable the
        // button: `Docs/07` §3 lets the app warn and leaves the refusal to the platform, and
        // `Docs/01` §4.1 lets the draft be saved either way.
        if (chosen != null && !chosen.carried) ...[
          const SizedBox(height: 8),
          Text(
            'You can save this as a draft, but Shipper cannot publish a job carrying '
            '${chosen.label.toLowerCase()}.',
            key: const Key('goods-category-refused'),
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.error),
          ),
        ],

        // One note for the whole list rather than a badge on every row. Every entry is provisional
        // today — X-4 is open and `Docs/11` §5 records the reduced form the list ships in — so a
        // per-entry marker would be thirteen identical badges saying nothing about any of them.
        if (catalogue.categories.any((c) => c.provisional)) ...[
          const SizedBox(height: 8),
          Text(
            'This list is under review and may change.',
            key: const Key('goods-categories-provisional'),
            style: theme.textTheme.bodySmall?.copyWith(
              color: theme.colorScheme.onSurfaceVariant,
            ),
          ),
        ],
      ],
    );
  }
}

/// The catalogue could not be read.
///
/// The rest of the form stays usable and the step can still save, which is deliberate: a customer
/// on a bad connection can write down what they are sending and come back for the category. What
/// they must not do is lose the category the draft already has, and they do not — the code is
/// seeded from the draft and re-sent unchanged.
class _CatalogueUnavailable extends ConsumerWidget {
  const _CatalogueUnavailable({required this.selected});

  final String selected;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);

    return Column(
      key: const Key('goods-categories-unavailable'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          selected.isEmpty
              ? 'We could not load the list of goods types. You can fill in the rest and choose '
                  'one when you come back.'
              : 'We could not load the list of goods types. Your job keeps the type you already '
                  'chose.',
          style: theme.textTheme.bodyMedium,
        ),
        const SizedBox(height: 8),
        OutlinedButton(
          key: const Key('goods-categories-retry'),
          onPressed: () => ref.invalidate(goodsCatalogueProvider),
          child: const Text('Try again'),
        ),
      ],
    );
  }
}
