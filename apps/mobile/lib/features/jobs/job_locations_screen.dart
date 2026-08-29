import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/address_section.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_locations_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';

/// The first step of publishing a delivery: where it is collected and where it goes (SHIP-71).
///
/// ## Nothing on this screen validates an address, and that is the decision
///
/// [AddressSection] carries the argument, because SHIP-75 needs the same eight inputs on the step
/// that edits a saved draft and the reasoning has to travel with them. The short version:
/// `Docs/07` §2 puts the rules on the platform, and a client-side rule here could only refuse
/// something the platform accepts.
///
/// **This screen creates the draft; `JobLocationsEditScreen` edits one that exists.** They draw
/// the same form and send different requests — `POST /v1/jobs` against `PATCH /v1/jobs/{id}` —
/// which is the whole of the difference between starting a job and coming back to one.
///
/// ## An address the platform could not place is an ordinary outcome, not an error
///
/// SHIP-59a requires that a failed lookup does not fail the job, and the platform honours it by
/// storing the address exactly as typed with no coordinate. This screen has to honour the same
/// thing at the other end: "we could not match this to a map" is shown as information, in ordinary
/// colours, with the journey continuing — not as a failure, and never as something to fix before
/// carrying on. Rural addresses that no geocoder knows are deliveries this marketplace exists to
/// carry.
class JobLocationsScreen extends ConsumerStatefulWidget {
  const JobLocationsScreen({super.key});

  @override
  ConsumerState<JobLocationsScreen> createState() => _JobLocationsScreenState();
}

class _JobLocationsScreenState extends ConsumerState<JobLocationsScreen> {
  final _form = GlobalKey<FormState>();

  final _pickup = AddressFields();
  final _dropoff = AddressFields();

  /// What the platform said about each field, keyed by the dotted path it named.
  ///
  /// Cleared per field as that field is edited: a server message that outlives the value it was
  /// about is worse than none.
  final _serverErrors = <String, String>{};

  @override
  void dispose() {
    _pickup.dispose();
    _dropoff.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(_serverErrors.clear);

    final saved = await ref.read(jobLocationsProvider.notifier).save(
          pickup: _pickup.value,
          dropoff: _dropoff.value,
        );

    if (!mounted || saved) return;

    setState(() {
      _serverErrors.addAll(_fieldMessagesFrom(ref.read(jobLocationsProvider).failure));
    });
    _form.currentState?.validate();
  }

  /// Which of the platform's refusals belong under an input rather than in the banner.
  ///
  /// Only the ones it named a field for. `service_unavailable`, `jobs_customer_only` and a
  /// dropped connection are about the request rather than about a value somebody typed, and
  /// putting them under an input would send a customer looking for a mistake in an address that
  /// is fine.
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
    final step = ref.watch(jobLocationsProvider);
    final failure = step.failure;
    final showBanner = failure != null && _fieldMessagesFrom(failure).isEmpty;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Publish a delivery'),
        leading: IconButton(
          key: const Key('job-locations-close'),
          icon: const Icon(Icons.close),
          tooltip: 'Close',
          onPressed: () => context.go(Routes.home),
        ),
      ),
      body: SafeArea(
        child: step.showingForm
            ? _buildForm(theme, step, showBanner: showBanner, failure: failure)
            : _buildSaved(theme, step.draft!),
      ),
    );
  }

  Widget _buildForm(
    ThemeData theme,
    JobLocationsState step, {
    required bool showBanner,
    required ApiFailure? failure,
  }) {
    return Form(
      key: _form,
      // What autovalidates here is the *platform's* answer, not a local rule — there are no local
      // rules on this form. It is what makes a server message disappear the moment the customer
      // edits the value it was about, rather than sitting under a field that has since been
      // corrected.
      autovalidateMode: AutovalidateMode.onUserInteraction,
      child: ListView(
        key: const Key('job-locations-form'),
        padding: const EdgeInsets.all(24),
        children: [
          Text('Where is it going?', style: theme.textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(
            'We look each address up so providers can see how far the job is. If we cannot find '
            'one, the job still goes ahead with the address exactly as you write it.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 24),
          if (showBanner && failure != null) ...[
            FailureBanner(failure),
            const SizedBox(height: 16),
          ],
          AddressSection(
            title: 'Pickup',
            path: 'pickup',
            fields: _pickup,
            messageFor: (field) => _serverErrors[field],
            onEdited: _clearServerError,
          ),
          const SizedBox(height: 24),
          AddressSection(
            title: 'Drop-off',
            path: 'dropoff',
            fields: _dropoff,
            messageFor: (field) => _serverErrors[field],
            onEdited: _clearServerError,
          ),
          const SizedBox(height: 32),
          FilledButton(
            key: const Key('job-locations-submit'),
            onPressed: step.busy ? null : () => unawaited(_submit()),
            child: step.busy
                ? const SizedBox(
                    height: 20,
                    width: 20,
                    child: CircularProgressIndicator(strokeWidth: 2),
                  )
                : const Text('Save and continue'),
          ),
          const SizedBox(height: 8),
          Text(
            // Docs/01 §4.1 saves drafts, and saying so is what makes the button above feel
            // like progress rather than a commitment somebody is not ready to make.
            'Your job is saved as a draft. Nothing is published and no provider can see it '
            'until you publish it.',
            key: const Key('job-locations-draft-note'),
            style: theme.textTheme.bodySmall,
          ),
        ],
      ),
    );
  }

  Widget _buildSaved(ThemeData theme, Job draft) {
    return ListView(
      key: const Key('job-locations-saved'),
      padding: const EdgeInsets.all(24),
      children: [
        Text('Locations saved', style: theme.textTheme.headlineSmall),
        const SizedBox(height: 8),
        Text(
          'This is what we have for your delivery. You can change it at any time while the job '
          'is a draft.',
          style: theme.textTheme.bodyMedium,
        ),
        const SizedBox(height: 24),
        _LookupOutcome(
          key: const Key('pickup-resolution'),
          title: 'Pickup',
          location: draft.pickup,
        ),
        const SizedBox(height: 16),
        _LookupOutcome(
          key: const Key('dropoff-resolution'),
          title: 'Drop-off',
          location: draft.dropoff,
        ),
        const SizedBox(height: 32),
        OutlinedButton(
          key: const Key('job-locations-edit'),
          onPressed: () {
            // The controllers still hold what was typed, so the form comes back filled in. What
            // the platform normalised is deliberately not written back over it: the customer
            // would see their own words change under them, and the normalisation is the
            // platform's business rather than a correction of theirs.
            ref.read(jobLocationsProvider.notifier).editAgain();
          },
          child: const Text('Change these addresses'),
        ),
        const SizedBox(height: 8),
        FilledButton(
          key: const Key('job-locations-continue'),
          // Pushed rather than gone to, which keeps this step in the stack: the next step finds
          // the draft it needs already read, and Back returns here rather than to the shell.
          onPressed: () => unawaited(context.push(Routes.jobGoodsFor(draft.id))),
          child: const Text('Continue'),
        ),
        const SizedBox(height: 8),
        TextButton(
          key: const Key('job-locations-done'),
          onPressed: () => context.go(Routes.home),
          child: const Text('Done for now'),
        ),
        const SizedBox(height: 16),
        Text(
          // Said because the button above commits to nothing: leaving is safe, and a customer who
          // does not know that will finish a form they were not ready to finish.
          'What you are sending, when it needs to move and your budget come next. Your draft '
          'waits in your jobs until you finish it.',
          key: const Key('job-locations-next-note'),
          style: theme.textTheme.bodySmall,
        ),
      ],
    );
  }
}

/// One address, and what the platform made of it.
///
/// Three outcomes, and the middle one is the whole point of this widget: **not recognised is not
/// a problem.** It carries no error colour, no warning icon and no call to action, because the
/// job proceeds either way (SHIP-59a) and a customer told to fix a rural address that is already
/// correct has nothing they can do.
class _LookupOutcome extends StatelessWidget {
  const _LookupOutcome({super.key, required this.title, required this.location});

  final String title;
  final JobLocation? location;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final address = location;

    final (IconData icon, String typed, String outcome) = switch (address) {
      null || JobLocation(isEmpty: true) => (
          Icons.more_horiz,
          'Not added yet',
          'You can add this before you publish the job.',
        ),
      JobLocation(resolved: true, :final oneLine, :final coordinate) => (
          Icons.place_outlined,
          oneLine,
          'We matched this to ${coordinate?.formatted ?? 'a place on the map'}.',
        ),
      JobLocation(:final oneLine) => (
          Icons.edit_location_alt_outlined,
          oneLine,
          'We could not match this to a place on the map. That is fine — it is saved exactly as '
              'you wrote it and the driver will see it that way.',
        ),
    };

    return Row(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Icon(icon, color: theme.colorScheme.onSurfaceVariant),
        const SizedBox(width: 12),
        Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(title, style: theme.textTheme.labelLarge),
              const SizedBox(height: 2),
              Text(typed, style: theme.textTheme.bodyMedium),
              const SizedBox(height: 2),
              Text(
                outcome,
                style: theme.textTheme.bodySmall
                    ?.copyWith(color: theme.colorScheme.onSurfaceVariant),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
