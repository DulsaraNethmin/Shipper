import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/goods_categories_repository.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_draft_controller.dart';
import 'package:shipper/features/jobs/job_wizard.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';
import 'package:shipper/shared/formatting/money.dart';
import 'package:shipper/shared/validation/validators.dart';

/// The last step of publishing a delivery: the optional budget, the whole job read back, and
/// publication (SHIP-74).
///
/// ## The budget is on this screen and reaches no provider
///
/// `Docs/01` §4.3 keeps the customer's maximum private — not as an amount, not as a band, and not
/// as a "budget supplied" flag. It is safe here because every screen in this wizard is the owner's
/// own, and the platform enforces the rest: a provider reads `OpenJob`, a type with no field a
/// budget could go in. `budget_stays_on_the_customer_side_test.dart` is what keeps that honest.
///
/// It is asked for **last and framed as optional**, which is the same decision the platform's own
/// copy takes. Providers are told plainly that they cannot see it (SHIP-100), and `Docs/01` §4.3
/// notes that dimensions, access constraints and handling notes do more for bid accuracy than a
/// budget signal would.
///
/// ## The declaration is per job, not per account
///
/// `Docs/04` §2 requires the terms and the goods declaration "for every job", so the box is
/// unticked every time rather than remembered: the declaration is about *these* goods, made by a
/// customer who has just described them. The button is disabled until it is ticked, which is the
/// app hiding and disabling — the platform still refuses `accepts_terms: false` with
/// `jobs_terms_not_accepted` rather than ignoring it.
///
/// ## What publication can refuse, and why each one is drawn differently
///
/// Four answers, wanting four different things from the customer:
///
/// - **`validation_failed`** — required fields are missing, and they live on *earlier steps*. The
///   screen names each one, because "that job is incomplete" on the last screen of a wizard is a
///   dead end. Making each a link is SHIP-75, which builds the routes that reach a saved draft's
///   earlier steps directly.
/// - **`jobs_prohibited_category`** — Shipper will not carry these goods, quoted in the
///   catalogue's own words. Nothing on this screen can fix it and the goods step is where it
///   changes.
/// - **`jobs_customer_not_verified`** — the account, not the job. The message says which of the
///   email address and the phone number is outstanding.
/// - **`jobs_not_publishable`** — the job is not a draft, which on this screen almost always means
///   it is already open. The screen reconciles rather than arguing.
class JobReviewScreen extends StatelessWidget {
  const JobReviewScreen({required this.jobId, super.key});

  final String jobId;

  @override
  Widget build(BuildContext context) {
    return JobWizardScaffold(
      jobId: jobId,
      step: JobWizardStep.review,
      builder: (context, state, draft) => _ReviewForm(jobId: jobId, state: state, draft: draft),
    );
  }
}

class _ReviewForm extends ConsumerStatefulWidget {
  const _ReviewForm({required this.jobId, required this.state, required this.draft});

  final String jobId;
  final JobDraftState state;
  final Job draft;

  @override
  ConsumerState<_ReviewForm> createState() => _ReviewFormState();
}

class _ReviewFormState extends ConsumerState<_ReviewForm> {
  final _form = GlobalKey<FormState>();
  final _budget = TextEditingController();

  /// Unticked on arrival, every time. See the note on [JobReviewScreen].
  bool _acceptsTerms = false;

  final _serverErrors = <String, String>{};

  @override
  void initState() {
    super.initState();

    final cents = widget.draft.budgetCents;
    // Shown as dollars, because that is what a person types. `0` is what the platform stores when
    // a budget has been cleared, and rendering it as "$0.00" would tell a customer they had set a
    // budget of nothing.
    _budget.text = (cents == null || cents == 0) ? '' : (cents / 100).toStringAsFixed(2);
  }

  @override
  void dispose() {
    _budget.dispose();
    super.dispose();
  }

  Future<void> _publish() async {
    if (!(_form.currentState?.validate() ?? false)) return;

    setState(_serverErrors.clear);

    final controller = ref.read(jobDraftProvider(widget.jobId).notifier);

    // The budget is saved first and **only when it has changed**, which is what keeps a retry
    // after a refused publication from sending a `PATCH` at a job that is by then open — the
    // platform answers `jobs_not_a_draft` to that, and the customer would see a refusal about
    // editing when they had asked to publish.
    final typed = centsFromAud(_budget.text);
    final stored = widget.draft.budgetCents ?? 0;
    if ((typed ?? 0) != stored) {
      final saved = await controller.save(budgetBody(cents: typed));
      if (!mounted) return;
      if (!saved) {
        setState(() {
          _serverErrors
              .addAll(_fieldMessagesFrom(ref.read(jobDraftProvider(widget.jobId)).failure));
        });
        _form.currentState?.validate();
        return;
      }
    }

    final published = await controller.publish(acceptsTerms: _acceptsTerms);
    if (!mounted) return;

    if (published) {
      // `go` rather than `push`: the wizard is finished and the whole stack behind it describes a
      // draft that no longer exists in that state. The customer lands on their own jobs, where the
      // job they just published is now Open.
      context.go(Routes.home);
      return;
    }

    setState(() {
      _serverErrors.addAll(_fieldMessagesFrom(ref.read(jobDraftProvider(widget.jobId)).failure));
    });
    _form.currentState?.validate();
  }

  static Map<String, String> _fieldMessagesFrom(ApiFailure? failure) {
    return switch (failure) {
      ApiErrorResponse(:final fieldMessages) => fieldMessages,
      _ => const <String, String>{},
    };
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final state = widget.state;
    final draft = widget.draft;
    final failure = state.failure;

    // The banner carries everything the platform did not attach to a field: the prohibited
    // category, the unverified account, a job that is no longer a draft, and a dropped connection.
    // The missing-field list is drawn separately below, because it is a list of places to go
    // rather than one sentence.
    final missing = _fieldMessagesFrom(failure);
    final showBanner = failure != null && missing.isEmpty;

    return Form(
      key: _form,
      autovalidateMode: AutovalidateMode.onUserInteraction,
      child: ListView(
        key: const Key('job-review-form'),
        padding: const EdgeInsets.all(24),
        children: [
          Text(JobWizardStep.review.heading, style: theme.textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(
            'This is what providers will see, apart from your budget — which is never shown to '
            'anyone.',
            key: const Key('review-privacy-note'),
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 24),
          if (showBanner) ...[
            FailureBanner(failure),
            const SizedBox(height: 16),
          ],
          if (missing.isNotEmpty) ...[
            _StillMissing(missing: missing),
            const SizedBox(height: 16),
          ],
          _Summary(draft: draft),
          const SizedBox(height: 24),
          Text('Your budget', style: theme.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            'Optional, and private. Providers price a job on what it is worth to them, and '
            'Shipper never shows them what you are willing to pay.',
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
          const SizedBox(height: 12),
          TextFormField(
            key: const Key('review-budget'),
            controller: _budget,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            decoration: const InputDecoration(
              labelText: 'Maximum you would pay',
              prefixText: r'$',
              helperText: 'Leave it blank if you would rather not say.',
            ),
            // The one local rule on this screen, and it is a *shape* rule rather than a limit:
            // this form has to turn what was typed into a whole number of cents and cannot send
            // "four fifty". The maximum is `internal/jobs`' and arrives as `out_of_range`.
            validator: (value) =>
                _serverErrors['budget_cents'] ??
                (value == null || value.trim().isEmpty ? null : Validators.audAmount(value)),
          ),
          const SizedBox(height: 24),
          CheckboxListTile(
            key: const Key('review-accepts-terms'),
            value: _acceptsTerms,
            onChanged: (ticked) => setState(() => _acceptsTerms = ticked ?? false),
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            title: const Text(
              'I accept the terms, and the goods are described honestly and are legal to carry.',
            ),
          ),
          const SizedBox(height: 16),
          FilledButton(
            key: const Key('job-review-publish'),
            // Disabled until the declaration is made — the app disabling, not deciding. The
            // platform refuses `accepts_terms: false` on its own side (`Docs/07` §3).
            onPressed: (state.saving || !_acceptsTerms) ? null : () => unawaited(_publish()),
            child: state.saving
                ? const SizedBox(
                    height: 20,
                    width: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('Publish this job'),
          ),
          const SizedBox(height: 8),
          Text(
            'Once it is published, providers can see it and make you private offers. You can '
            'cancel it any time before you accept one.',
            style: theme.textTheme.bodySmall,
          ),
        ],
      ),
    );
  }
}

/// What the platform said is still missing, and where it lives.
///
/// A list rather than a sentence, because the platform lists **every** missing field at once and
/// each is on a different step. A customer told only "that job is incomplete" on the last screen
/// of a wizard has nowhere to go.
///
/// Making each entry a link is SHIP-75's, which builds the routes that reach a saved draft's
/// earlier steps directly. Until then the note says which step owns each one, and Back reaches it:
/// the wizard is pushed, so the earlier steps are still on the stack.
class _StillMissing extends StatelessWidget {
  const _StillMissing({required this.missing});

  final Map<String, String> missing;

  /// Which step a field the platform named belongs to.
  ///
  /// The five entries are `publishable`'s required set in `internal/jobs/publish.go`. Anything not
  /// in the map is shown without a step rather than guessed at — a wrong signpost is worse than
  /// none, and the platform can name a field this list has never heard of.
  static const _owner = <String, JobWizardStep>{
    'pickup': JobWizardStep.locations,
    'dropoff': JobWizardStep.locations,
    'goods_description': JobWizardStep.goods,
    'goods_category': JobWizardStep.goods,
    'pickup_window.start': JobWizardStep.schedule,
  };

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      key: const Key('review-still-missing'),
      padding: const EdgeInsets.all(16),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            'This job needs a little more before it can be published',
            style: theme.textTheme.titleSmall
                ?.copyWith(color: theme.colorScheme.onErrorContainer),
          ),
          const SizedBox(height: 8),
          for (final entry in missing.entries)
            Padding(
              padding: const EdgeInsets.only(bottom: 4),
              child: Text(
                switch (_owner[entry.key]) {
                  final step? => '${entry.value} (${step.label} step)',
                  _ => entry.value,
                },
                key: Key('review-missing-${entry.key}'),
                style: theme.textTheme.bodyMedium
                    ?.copyWith(color: theme.colorScheme.onErrorContainer),
              ),
            ),
          const SizedBox(height: 8),
          Text(
            'Go back to the step it belongs to, fill it in, and come back here.',
            style: theme.textTheme.bodySmall
                ?.copyWith(color: theme.colorScheme.onErrorContainer),
          ),
        ],
      ),
    );
  }
}

/// The whole job, read back.
///
/// The *full* job, which is what "reviewed before publish" means: a summary that showed only what
/// this screen collected would let a customer publish addresses and dates they last saw three
/// screens ago.
class _Summary extends ConsumerWidget {
  const _Summary({required this.draft});

  final Job draft;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    // The category is stored as a code and shown as a label, so the catalogue is needed to render
    // it. A code the catalogue no longer serves — or a catalogue that would not load — falls back
    // to the code itself rather than to nothing: it is what the job actually says.
    final fetched = ref.watch(goodsCatalogueProvider);
    final catalogue = fetched.hasValue ? fetched.requireValue : null;
    final code = draft.goodsCategory;
    final category = catalogue?.byCode(code)?.label ?? code;

    return Column(
      key: const Key('review-summary'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        _Line(label: 'Pickup', value: draft.pickup?.oneLine, field: 'pickup'),
        _Line(label: 'Drop-off', value: draft.dropoff?.oneLine, field: 'dropoff'),
        _Line(label: 'Goods', value: draft.goodsDescription, field: 'goods-description'),
        _Line(label: 'Type', value: category, field: 'goods-category'),
        _Line(label: 'Size', value: _size(draft), field: 'size'),
        _Line(
          label: 'Collection',
          value: _window(draft.pickupWindow),
          field: 'pickup-window',
        ),
        _Line(
          label: 'Deliver by',
          value: dayFirstDate(draft.dropoffWindow?.end),
          field: 'dropoff-window',
        ),
        _Line(label: 'Vehicle', value: draft.vehicleRequirement, field: 'vehicle'),
        _Line(label: 'Notes for the driver', value: draft.handlingNotes, field: 'handling'),
      ],
    );
  }

  /// The three dimensions and the weight, as one line, omitting whatever was not given.
  ///
  /// `0` is the platform's cleared value rather than a measurement, so it is treated as absent
  /// here exactly as the goods form treats it.
  static String? _size(Job draft) {
    final box = <String>[
      if ((draft.lengthCm ?? 0) > 0) '${draft.lengthCm}cm long',
      if ((draft.widthCm ?? 0) > 0) '${draft.widthCm}cm wide',
      if ((draft.heightCm ?? 0) > 0) '${draft.heightCm}cm high',
      if ((draft.weightKg ?? 0) > 0) '${_kg(draft.weightKg!)}kg',
    ];
    return box.isEmpty ? null : box.join(', ');
  }

  static String _kg(double value) =>
      value == value.roundToDouble() ? value.round().toString() : value.toString();

  /// A window as a person reads it: one date, or a range.
  static String? _window(JobTimeWindow? window) {
    final from = dayFirstDate(window?.start);
    final to = dayFirstDate(window?.end);

    if (from == null && to == null) return null;
    if (to == null) return 'From $from';
    if (from == null) return 'By $to';
    if (from == to) return from;
    return '$from to $to';
  }
}

/// One line of the summary.
///
/// **A field with nothing in it is shown as "Not given" rather than left out**, which is the point
/// of a review screen: a customer scanning for what they forgot cannot find an absence. The ones
/// the platform requires are the ones this makes visible, and the optional ones read as optional
/// because the copy on their own steps said so.
class _Line extends StatelessWidget {
  const _Line({required this.label, required this.value, required this.field});

  final String label;
  final String? value;
  final String field;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final given = value != null && value!.isNotEmpty;

    return Padding(
      padding: const EdgeInsets.only(bottom: 12),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: theme.textTheme.labelMedium
                ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
          const SizedBox(height: 2),
          Text(
            given ? value! : 'Not given',
            key: Key('review-$field'),
            style: given
                ? theme.textTheme.bodyMedium
                : theme.textTheme.bodyMedium
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      ),
    );
  }
}
