import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/date_field.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_draft_controller.dart';
import 'package:shipper/features/jobs/job_wizard.dart';
import 'package:shipper/features/jobs/jobs_repository.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';

/// The third step of publishing a delivery: when it moves, and what it needs to move in
/// (SHIP-73).
///
/// ## Days, not times
///
/// The pickup window is a **constraint** rather than a commitment — "any time on Tuesday or
/// Wednesday" — and `contracts/paths/jobs.yaml` says why in its own words: a customer who names a
/// window gets more bids than one who names a minute. The provider's bid is where an instant is
/// promised (SHIP-100), and that form asks for a clock because a commitment needs one. See
/// [DateField], which is a date-only control for exactly this reason.
///
/// ## Only the earliest collection date is required, and nothing here enforces that
///
/// `publishable` in `internal/jobs/publish.go` requires `pickup_window.start` and neither of the
/// other two dates. This screen states which one matters and refuses nothing: `Docs/07` §3 puts
/// the decision on the platform, and a client-side "required" here would stop a customer saving a
/// half-finished draft that `Docs/01` §4.1 explicitly allows.
///
/// ## Handling notes are captured here, and the ticket does not name them
///
/// `Docs/09` asks this step for a date window and a vehicle requirement. Handling notes are in
/// `Docs/01` §4.1's list of what a customer may put on a job and in no step of the wizard, so the
/// field would have been reachable by no screen in the product. They sit beside the vehicle
/// requirement because they are the same question asked twice — what does carrying this actually
/// take — and `Docs/01` §4.3 notes that detail of this kind does more for bid accuracy than a
/// budget signal would.
class JobScheduleScreen extends StatelessWidget {
  const JobScheduleScreen({required this.jobId, super.key});

  final String jobId;

  @override
  Widget build(BuildContext context) {
    return JobWizardScaffold(
      jobId: jobId,
      step: JobWizardStep.schedule,
      builder: (context, state, draft) => _ScheduleForm(jobId: jobId, state: state, draft: draft),
    );
  }
}

class _ScheduleForm extends ConsumerStatefulWidget {
  const _ScheduleForm({required this.jobId, required this.state, required this.draft});

  final String jobId;
  final JobDraftState state;
  final Job draft;

  @override
  ConsumerState<_ScheduleForm> createState() => _ScheduleFormState();
}

class _ScheduleFormState extends ConsumerState<_ScheduleForm> {
  final _form = GlobalKey<FormState>();

  final _vehicle = TextEditingController();
  final _handling = TextEditingController();

  DateTime? _pickupFrom;
  DateTime? _pickupTo;
  DateTime? _dropoffBy;

  final _serverErrors = <String, String>{};

  @override
  void initState() {
    super.initState();

    final draft = widget.draft;
    _pickupFrom = dayOf(draft.pickupWindow?.start);
    _pickupTo = dayOf(draft.pickupWindow?.end);
    _dropoffBy = dayOf(draft.dropoffWindow?.end);
    _vehicle.text = draft.vehicleRequirement ?? '';
    _handling.text = draft.handlingNotes ?? '';
  }

  @override
  void dispose() {
    _vehicle.dispose();
    _handling.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(_serverErrors.clear);

    final saved = await ref.read(jobDraftProvider(widget.jobId).notifier).save(
          scheduleBody(
            pickupFrom: _pickupFrom,
            pickupTo: _pickupTo,
            dropoffBy: _dropoffBy,
            vehicleRequirement: _vehicle.text.trim(),
            handlingNotes: _handling.text.trim(),
          ),
        );

    if (!mounted) return;

    if (saved) {
      // The review step is SHIP-74 and does not exist yet, so this returns to the customer's own
      // jobs rather than to a route that is not registered. The draft is waiting there.
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

  void _clearServerError(String field) {
    if (_serverErrors.remove(field) != null) setState(() {});
  }

  /// The message the platform put on a window's end, under whichever key it used.
  ///
  /// The platform names a window's parts under a dotted path — `pickup_window.start` — and may
  /// also refuse the window as a whole under `pickup_window`. Both are shown against the field
  /// that produced them, because a message rendered nowhere is a refusal the customer cannot act
  /// on.
  String? _windowMessage(String window, String edge) =>
      _serverErrors['$window.$edge'] ?? _serverErrors[window];

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
        key: const Key('job-schedule-form'),
        padding: const EdgeInsets.all(24),
        children: [
          Text(JobWizardStep.schedule.heading, style: theme.textTheme.headlineSmall),
          const SizedBox(height: 8),
          Text(
            'Give a window rather than a time. Providers plan a run around several jobs, and a '
            'job that can move on either of two days attracts more offers than one that cannot.',
            style: theme.textTheme.bodyMedium,
          ),
          const SizedBox(height: 24),
          if (showBanner) ...[
            FailureBanner(failure),
            const SizedBox(height: 16),
          ],
          Text('Collection', style: theme.textTheme.titleMedium),
          const SizedBox(height: 12),
          DateField(
            fieldKey: 'pickup-from',
            label: 'Earliest collection date',
            helperText: 'The one date a provider needs to plan around.',
            value: _pickupFrom,
            serverMessage: _windowMessage('pickup_window', 'start'),
            onChanged: (day) {
              setState(() => _pickupFrom = day);
              _clearServerError('pickup_window.start');
              _clearServerError('pickup_window');
            },
          ),
          const SizedBox(height: 12),
          DateField(
            fieldKey: 'pickup-to',
            label: 'Latest collection date',
            helperText: 'Optional. Leave it out if only the earliest date matters.',
            value: _pickupTo,
            serverMessage: _serverErrors['pickup_window.end'],
            onChanged: (day) {
              setState(() => _pickupTo = day);
              _clearServerError('pickup_window.end');
              _clearServerError('pickup_window');
            },
          ),
          const SizedBox(height: 24),
          Text('Delivery', style: theme.textTheme.titleMedium),
          const SizedBox(height: 12),
          DateField(
            fieldKey: 'dropoff-by',
            label: 'Deliver by',
            // No "earliest delivery" field. For most road transport the drop-off follows from the
            // pickup rather than being a constraint of its own, and the platform requires neither
            // end of this window.
            helperText: 'Optional. A deadline, if the delivery has one.',
            value: _dropoffBy,
            serverMessage: _windowMessage('dropoff_window', 'end'),
            onChanged: (day) {
              setState(() => _dropoffBy = day);
              _clearServerError('dropoff_window.end');
              _clearServerError('dropoff_window');
            },
          ),
          const SizedBox(height: 24),
          Text('How it needs to be carried', style: theme.textTheme.titleMedium),
          const SizedBox(height: 12),
          TextFormField(
            key: const Key('vehicle-requirement'),
            controller: _vehicle,
            decoration: const InputDecoration(
              labelText: 'Vehicle needed',
              // Free text on the platform's side too. The capability vocabulary belongs to the
              // fleet domain (SHIP-79) and this field is validated against it when that list
              // exists; a picker here would be a second copy of a list that does not exist yet.
              helperText: 'Optional — "van", "ute with a tailgate lifter".',
            ),
            onChanged: (_) => _clearServerError('vehicle_requirement'),
            validator: (_) => _serverErrors['vehicle_requirement'],
          ),
          const SizedBox(height: 12),
          TextFormField(
            key: const Key('handling-notes'),
            controller: _handling,
            minLines: 2,
            maxLines: 5,
            textCapitalization: TextCapitalization.sentences,
            decoration: const InputDecoration(
              labelText: 'Anything the driver should know',
              helperText: 'Optional — stairs, gate codes, access times, "ring ahead".',
            ),
            onChanged: (_) => _clearServerError('handling_notes'),
            validator: (_) => _serverErrors['handling_notes'],
          ),
          const SizedBox(height: 32),
          FilledButton(
            key: const Key('job-schedule-submit'),
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
