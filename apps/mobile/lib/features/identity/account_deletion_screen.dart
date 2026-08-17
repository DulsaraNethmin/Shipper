import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/features/identity/account_deletion.dart';
import 'package:shipper/features/identity/account_deletion_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';

/// Deleting this account, from inside the app (SHIP-173).
///
/// ## Why this screen exists at all
///
/// Apple requires any app offering account creation to offer in-app account deletion, and
/// `Docs/05` §3.1 adopts the model it is built on: **delete the person, retain the transaction.**
/// This is the only place in the client that reaches `POST /v1/account/deletion`.
///
/// ## Where it lives, and what it is not
///
/// It is reached from the signed-in shell's app bar, beside sign-out. **That is a placement rather
/// than a design**: there is no settings or account screen in this app — `features/profile` is a
/// doc-only stub and `Routes` has no `/settings` — and inventing one to hold a single action would
/// be building the screen a later ticket has to reconcile with. What Apple requires is that
/// deletion be *discoverable* from inside the app, which an app-bar action on the shell every
/// signed-in person lands on satisfies. When an account screen arrives, this moves under it and
/// nothing else about it changes.
///
/// ## The three things the *Done when* asks for
///
/// **Initiated in-app.** The button sends `POST /v1/account/deletion` and nothing else does.
///
/// **With clear consequences.** `Docs/05` §3.1's split is on the screen before the button is:
/// what is irreversibly deleted, what is kept under a pseudonym, and the thirty days. A screen
/// that said only "this cannot be undone" would be true and would not tell anybody what they were
/// about to lose — and it would hide the part that is genuinely surprising, which is that the jobs
/// and bids stay.
///
/// **And confirmation.** A dialog, dismissable, sending nothing unless it is accepted.
///
/// ## What it deliberately does not draw
///
/// **No jobs.** A deferred request is explained by the platform's own sentence and by nothing this
/// screen assembles: listing the delivery being waited on would put job data on a screen both
/// halves of the marketplace reach, which engages `Docs/01` §4.3's budget invariant for no gain.
/// The person can already see their own deliveries.
///
/// **No "cancel my deletion request".** Withdrawing one has no ticket and no endpoint —
/// `000105` declined to invent the state, because doing so would invent the product decision with
/// it. A button that pretended otherwise would be worse than its absence.
class AccountDeletionScreen extends ConsumerWidget {
  const AccountDeletionScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(accountDeletionProvider);

    return Scaffold(
      appBar: AppBar(title: const Text('Delete account')),
      body: SafeArea(
        child: ListView(
          key: const Key('account-deletion'),
          padding: const EdgeInsets.all(16),
          children: [
            if (state.failure != null) ...[
              FailureBanner(state.failure!),
              if (state.refusal != null) ...[
                const SizedBox(height: 8),
                Text(state.refusal!, key: const Key('account-deletion-refusal')),
              ],
              const SizedBox(height: 8),
              TextButton(
                key: const Key('account-deletion-dismiss'),
                onPressed: ref.read(accountDeletionProvider.notifier).dismissFailure,
                child: const Text('Dismiss'),
              ),
              const SizedBox(height: 16),
            ],
            if (state.request != null)
              _Recorded(state.request!)
            else
              const _Consequences(),
            const SizedBox(height: 24),
            _DeleteButton(state: state),
          ],
        ),
      ),
    );
  }
}

/// What deleting this account costs, before anybody taps anything.
///
/// Straight from `Docs/05` §3.1's table. The two halves are separate paragraphs rather than one
/// warning because they answer two different questions — "what disappears" and "what does not" —
/// and the second is the one nobody expects.
class _Consequences extends StatelessWidget {
  const _Consequences();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Deleting your account', style: theme.textTheme.titleLarge),
        const SizedBox(height: 12),
        const Text(
          key: Key('account-deletion-removed'),
          'Your profile and contact details, your identity and verification documents, your '
          'messages, and any delivery photographs of yours are deleted for good. They cannot be '
          'recovered, and signing up again does not bring them back.',
        ),
        const SizedBox(height: 12),
        const Text(
          key: Key('account-deletion-retained'),
          'Your deliveries, offers and their history stay on the platform under a pseudonym that '
          'is no longer you. That is what keeps the other party’s own record of who carried '
          'their goods intact — nobody’s history is rewritten because somebody else left.',
        ),
        const SizedBox(height: 12),
        const Text(
          key: Key('account-deletion-window'),
          'Deletion is not instant. Shipper completes it within 30 days and will tell you the '
          'date. If you are in the middle of a delivery, it waits until that delivery is finished.',
        ),
      ],
    );
  }
}

/// What the platform recorded, once it has answered.
///
/// **Two states, drawn differently**, which is the whole reason this is not one sentence with a
/// date substituted into it. A `requested` request has a promise attached; a `deferred` one has a
/// delivery in front of it and a date that is not yet a promise (SHIP-170). Saying "we will delete
/// your account by 15 September" to somebody whose request has not started counting would be the
/// platform's own contract read wrongly by its own client.
class _Recorded extends StatelessWidget {
  const _Recorded(this.request);

  final AccountDeletion request;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final completesBy = dayFirstDate(request.completesBy);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          request.deferred ? 'Deletion is waiting' : 'Deletion requested',
          key: const Key('account-deletion-heading'),
          style: theme.textTheme.titleLarge,
        ),
        const SizedBox(height: 12),
        if (request.deferred) ...[
          // The platform's own words, rendered as given. It is policy copy and is served rather
          // than shipped precisely so it can be corrected without a store release; rewording it
          // here would be this client shipping its own version of a legal sentence.
          Text(
            request.deferralReason ??
                'Your account will be deleted once your current delivery is finished.',
            key: const Key('account-deletion-deferred'),
          ),
          if (completesBy != null) ...[
            const SizedBox(height: 12),
            Text(
              'At the earliest, that would be $completesBy. Open this screen again after your '
              'delivery finishes and Shipper will confirm the date.',
              key: const Key('account-deletion-earliest'),
            ),
          ],
        ] else ...[
          Text(
            completesBy == null
                ? 'Shipper has recorded your request and will complete it within 30 days.'
                : 'Shipper has recorded your request and will complete it by $completesBy.',
            key: const Key('account-deletion-promised'),
          ),
          const SizedBox(height: 12),
          const Text(
            'You can keep using Shipper until then. Nothing else you need to do.',
            key: Key('account-deletion-meanwhile'),
          ),
        ],
      ],
    );
  }
}

/// The button, and the confirmation it opens.
class _DeleteButton extends ConsumerWidget {
  const _DeleteButton({required this.state});

  final AccountDeletionState state;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        FilledButton(
          key: const Key('account-deletion-start'),
          style: FilledButton.styleFrom(
            backgroundColor: theme.colorScheme.error,
            foregroundColor: theme.colorScheme.onError,
          ),
          onPressed: state.submitting ? null : () => _confirm(context, ref),
          child: state.submitting
              ? const SizedBox(
                  height: 18,
                  width: 18,
                  child: CircularProgressIndicator(strokeWidth: 2),
                )
              // **The label changes once a request exists**, because the act does. Asking again is
              // how a deferral lifts (SHIP-170), so the button stays — but "Delete my account"
              // over a request that is already recorded would read as a second deletion.
              : Text(state.recorded ? 'Check again' : 'Delete my account'),
        ),
      ],
    );
  }

  Future<void> _confirm(BuildContext context, WidgetRef ref) async {
    // **Only the first request is confirmed.** Checking again on a request that already exists
    // sends the same idempotent call and cannot delete anything twice; putting a dialog in front
    // of it would train somebody to dismiss the one that matters.
    if (!state.recorded) {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => const _DeleteConfirmation(),
      );
      if (confirmed != true) return;
    }

    await ref.read(accountDeletionProvider.notifier).request();
  }
}

/// The explicit confirmation.
///
/// **It names what is lost rather than asking whether somebody is sure.** "Are you sure?" confirms
/// that a person tapped something, not that they meant this — and this is the one act in the app
/// that cannot be undone by any means, from either side.
class _DeleteConfirmation extends StatelessWidget {
  const _DeleteConfirmation();

  @override
  Widget build(BuildContext context) {
    return AlertDialog(
      key: const Key('account-deletion-confirm'),
      title: const Text('Delete your account?'),
      content: const Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            key: Key('account-deletion-confirm-consequence'),
            'Your profile, contact details, documents, messages and photographs are deleted for '
            'good. This cannot be undone, and Shipper cannot restore them for you afterwards.',
          ),
          SizedBox(height: 12),
          Text(
            key: Key('account-deletion-confirm-window'),
            'Shipper completes deletion within 30 days and will tell you the date.',
          ),
        ],
      ),
      actions: [
        TextButton(
          key: const Key('account-deletion-confirm-cancel'),
          onPressed: () => Navigator.of(context).pop(false),
          child: const Text('Keep my account'),
        ),
        FilledButton(
          key: const Key('account-deletion-confirm-accept'),
          onPressed: () => Navigator.of(context).pop(true),
          child: const Text('Delete'),
        ),
      ],
    );
  }
}
