import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/job.dart';
import 'package:shipper/features/jobs/job_draft_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';

/// The four steps of describing a delivery, in the order a customer walks them (SHIP-71 to
/// SHIP-74).
///
/// **The order is the wizard's, and the *required* set is the platform's.** They are not the same
/// list and must not be conflated: `publishable` in `internal/jobs/publish.go` requires the two
/// addresses, a goods description, a goods category and the start of the pickup window — which
/// spans the first three steps and none of the fourth. The budget is optional and always was.
/// Reading a draft against that set is what SHIP-75 resumes on, and it is written there rather
/// than here.
enum JobWizardStep {
  /// Pickup and drop-off (SHIP-71). The step that creates the draft.
  locations('Locations', 'Where is it going?'),

  /// Category, description, dimensions and weight (SHIP-72).
  goods('Goods', 'What are we moving?'),

  /// The pickup window and what the job needs to be carried in (SHIP-73).
  schedule('Schedule', 'When does it move?'),

  /// The optional budget, the whole job read back, and publication (SHIP-74).
  review('Review', 'Check and publish');

  const JobWizardStep(this.label, this.heading);

  /// The short name, for the step indicator.
  final String label;

  /// The question the step is asking, as its own heading.
  final String heading;

  /// One-based, for `Step 2 of 4`. People do not count from zero.
  int get position => index + 1;
}

/// The chrome every step after the first shares: the title, the step indicator, and the three
/// states of reading a draft.
///
/// ## Why the loading and failure handling is here rather than in each step
///
/// Three screens read the same provider and each has the same three outcomes — nothing yet, a read
/// that failed outright, and a draft to draw a form over. Written three times it is three chances
/// to forget the middle one, which is the one that matters: a step that renders an empty form
/// while the read is failing invites a customer to retype an address they already gave, and then
/// saves it over a draft nobody could read.
///
/// [builder] therefore receives a [Job] rather than a nullable one. A step cannot be asked to draw
/// without a draft.
class JobWizardScaffold extends ConsumerWidget {
  const JobWizardScaffold({
    required this.jobId,
    required this.step,
    required this.builder,
    super.key,
  });

  final String jobId;
  final JobWizardStep step;

  /// Draws the step, given the draft the platform holds.
  final Widget Function(BuildContext context, JobDraftState state, Job draft) builder;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(jobDraftProvider(jobId));
    final draft = state.draft;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Publish a delivery'),
        leading: IconButton(
          key: const Key('job-wizard-close'),
          icon: const Icon(Icons.close),
          tooltip: 'Close',
          // Home rather than `pop`, because the close button means "leave the wizard" from any
          // depth. Nothing is lost: every step saves to the platform before it moves on, so the
          // draft is waiting in the customer's jobs (SHIP-75).
          onPressed: () => context.go(Routes.home),
        ),
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(28),
          child: _StepIndicator(step: step),
        ),
      ),
      body: SafeArea(
        child: switch (state) {
          JobDraftState(isFirstLoad: true) => const Center(
              key: Key('job-wizard-loading'),
              child: CircularProgressIndicator(),
            ),
          JobDraftState(failedOutright: true) => _CouldNotRead(jobId: jobId, state: state),
          _ => builder(context, state, draft!),
        },
      ),
    );
  }
}

/// `Step 2 of 4 · Goods`.
///
/// Text rather than a row of dots, deliberately. A customer part-way through a form wants to know
/// how much is left, and four dots of which two are filled says that only to somebody who counts
/// them; it is also the form a screen reader can read out without a semantics label somebody has
/// to remember to keep in step.
class _StepIndicator extends StatelessWidget {
  const _StepIndicator({required this.step});

  final JobWizardStep step;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Padding(
      padding: const EdgeInsets.only(left: 16, right: 16, bottom: 8),
      child: Align(
        alignment: Alignment.centerLeft,
        child: Text(
          'Step ${step.position} of ${JobWizardStep.values.length} · ${step.label}',
          key: const Key('job-wizard-step'),
          style: theme.textTheme.labelMedium?.copyWith(
            color: theme.colorScheme.onSurfaceVariant,
          ),
        ),
      ),
    );
  }
}

/// The draft could not be read at all.
///
/// A retry rather than a dead end, and no form underneath it. The one thing this screen must not
/// do is offer to save: an edit sent against a draft nobody could read is an edit whose effect
/// nobody can predict.
class _CouldNotRead extends ConsumerWidget {
  const _CouldNotRead({required this.jobId, required this.state});

  final String jobId;
  final JobDraftState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final failure = state.failure;

    return ListView(
      key: const Key('job-wizard-unreadable'),
      padding: const EdgeInsets.all(24),
      children: [
        if (failure != null) ...[
          FailureBanner(failure),
          const SizedBox(height: 16),
        ],
        FilledButton(
          key: const Key('job-wizard-retry'),
          onPressed: state.loading
              ? null
              : () => ref.read(jobDraftProvider(jobId).notifier).reload(),
          child: const Text('Try again'),
        ),
      ],
    );
  }
}
