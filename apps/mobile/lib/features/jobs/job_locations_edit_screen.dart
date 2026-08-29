import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/address_section.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_draft_controller.dart';
import 'package:shipper/features/jobs/job_wizard.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';

/// The locations step of a draft that already exists (SHIP-75).
///
/// ## Why this is a second screen rather than a mode of the first
///
/// `JobLocationsScreen` is the step that **creates** a draft: it has no id until it succeeds, it
/// posts, and it shows the platform's geocoding back before offering the way on. This one edits a
/// draft the platform already holds: it has an id from the route, it patches, and it sits in the
/// same chrome as every other resumable step.
///
/// The two share the eight inputs — [AddressSection], extracted for exactly this — and share
/// nothing else. Folding them into one widget with a nullable id would put two request paths,
/// two provider families and two navigation outcomes behind one `if`, which is the shape a
/// half-finished draft gets posted as a second job by.
///
/// ## What it is for
///
/// A draft may be saved with its addresses empty: `Docs/01` §4.1 lets a customer start a job and
/// come back, and `POST /v1/jobs` accepts an empty body. So `JobWizardStep.firstIncompleteFor` can
/// legitimately answer `locations` for a draft in the customer's list, and without a route naming
/// an id that draft could be resumed at no step at all.
class JobLocationsEditScreen extends StatelessWidget {
  const JobLocationsEditScreen({required this.jobId, super.key});

  final String jobId;

  @override
  Widget build(BuildContext context) {
    return JobWizardScaffold(
      jobId: jobId,
      step: JobWizardStep.locations,
      builder: (context, state, draft) => _EditForm(jobId: jobId, state: state, draft: draft),
    );
  }
}

class _EditForm extends ConsumerStatefulWidget {
  const _EditForm({required this.jobId, required this.state, required this.draft});

  final String jobId;
  final JobDraftState state;
  final Job draft;

  @override
  ConsumerState<_EditForm> createState() => _EditFormState();
}

class _EditFormState extends ConsumerState<_EditForm> {
  final _form = GlobalKey<FormState>();

  final _pickup = AddressFields();
  final _dropoff = AddressFields();

  final _serverErrors = <String, String>{};

  @override
  void initState() {
    super.initState();

    // The parts as they were stored, not the geocoder's rendering of them. What the platform
    // matched is shown separately on the step that created the draft; writing it into the boxes
    // would change the customer's own words under them.
    _pickup.seed(widget.draft.pickup);
    _dropoff.seed(widget.draft.dropoff);
  }

  @override
  void dispose() {
    _pickup.dispose();
    _dropoff.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(_serverErrors.clear);

    final saved = await ref.read(jobDraftProvider(widget.jobId).notifier).save(
          locationsBody(pickup: _pickup.value, dropoff: _dropoff.value),
        );

    if (!mounted) return;

    if (saved) {
      unawaited(context.push(Routes.jobGoodsFor(widget.jobId)));
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
      autovalidateMode: AutovalidateMode.onUserInteraction,
      child: ListView(
        key: const Key('job-locations-edit-form'),
        padding: const EdgeInsets.all(24),
        children: [
          Text(JobWizardStep.locations.heading, style: theme.textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(
            'We look each address up so providers can see how far the job is. If we cannot find '
            'one, the job still goes ahead with the address exactly as you write it.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 24),
          if (showBanner) ...[
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
            key: const Key('job-locations-edit-submit'),
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
}
