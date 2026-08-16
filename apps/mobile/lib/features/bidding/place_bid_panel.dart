import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/instant_field.dart';
import 'package:shipper/features/bidding/place_bid_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';
import 'package:shipper/shared/formatting/money.dart';
import 'package:shipper/shared/validation/validators.dart';

/// Offering to carry one job, for a price and against two commitments about timing (SHIP-100).
///
/// ## It is a panel rather than a screen, and that is what keeps two features apart
///
/// `Docs/07` §2 puts *discovery and detail* in `jobs` and *bids* in `bidding`, and features do not
/// import one another — `architecture_test.dart` enforces it in both the `package:` and the relative
/// form. Reviewing a job and bidding on it is one thing a provider does and two features' work, so
/// the two halves meet in the **router**, which is `core` and is the only place that may know about
/// both. `app_router.dart` builds `OpenJobScreen` and hands it this panel.
///
/// That is the same arrangement the Go side uses for a domain and its adapters, which meet in
/// `cmd/api` and nowhere else. The alternative — one screen importing the other feature — is the
/// coupling the boundary exists to prevent, and it is very nearly impossible to reintroduce once it
/// has been crossed.
///
/// **This panel therefore knows the job's identifier and nothing else about the job.** It does not
/// need to: the platform does not require a bid's timing to fall inside the job's windows, because
/// offering a different day is a legitimate offer the customer is free to decline (SHIP-84).
///
/// ## Nothing here decides whether this provider may bid
///
/// `CLAUDE.md` and `Docs/07` §3 put every authorisation decision on the platform. The form is
/// offered and the request is sent; eligibility — service area, vehicles, verification, the job's
/// status — is decided server-side against the same predicate `GET /v1/jobs/open` uses, so a job in
/// the feed is a job that can be bid on and one that is not answers `404` here exactly as it does
/// there. The screen renders that refusal rather than pre-empting it.
class PlaceBidPanel extends ConsumerWidget {
  const PlaceBidPanel({required this.jobId, super.key});

  /// The job being bid on, from the route.
  final String jobId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final state = ref.watch(placeBidProvider(jobId));
    final controller = ref.read(placeBidProvider(jobId).notifier);

    if (state.bid case final placed?) return _Placed(bid: placed);

    return _BidForm(
      busy: state.sending,
      failure: state.failure,
      alreadyBid: state.alreadyBid,
      onSubmit: (bid) => unawaited(controller.place(bid)),
    );
  }
}

/// The offer the platform recorded.
///
/// **Read off the response and never off the form.** A retry answered `200` with an earlier
/// attempt's offer draws that offer, which is the reconciliation half of `Docs/02` §3.1 in the one
/// place it can bite on a surface that is never offline.
class _Placed extends StatelessWidget {
  const _Placed({required this.bid});

  final Bid bid;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final pickup = dayFirstDateTime(bid.pickupAt);
    final deliver = dayFirstDateTime(bid.deliverBy);

    return Column(
      key: const Key('bid-placed'),
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Icon(Icons.check_circle_outline, color: theme.colorScheme.primary),
            const SizedBox(width: 8),
            Expanded(
              child: Text('Your offer is with the customer', style: theme.textTheme.titleMedium),
            ),
          ],
        ),
        const SizedBox(height: 8),
        if (bid.amountCents case final cents?)
          Text(
            audFromCents(cents),
            key: const Key('bid-placed-amount'),
            style: theme.textTheme.headlineSmall,
          ),
        const SizedBox(height: 4),
        if (pickup != null) Text('Collecting $pickup', style: theme.textTheme.bodyMedium),
        if (deliver != null) Text('Delivered by $deliver', style: theme.textTheme.bodyMedium),
        if (bid.message case final message? when message.isNotEmpty) ...[
          const SizedBox(height: 8),
          Text(message, style: theme.textTheme.bodySmall),
        ],
        const SizedBox(height: 12),
        Text(
          // The status as a person reads it, in the exact name from Docs/02 §4 (CLAUDE.md) — a
          // provider reading "Submitted" in the app and support reading it in an audit entry have
          // to be reading about the same thing.
          bid.isLive
              ? 'Submitted — the customer can accept it, counter it, or let it expire.'
              : bid.status.label,
          key: Key('bid-status-${bid.status.wireName}'),
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
        const SizedBox(height: 8),
        Text(
          // Said out loud, because "change my price" is what somebody will look for next and not
          // finding it needs an explanation rather than a silence. The endpoints exist — SHIP-85
          // and SHIP-86 — and the screen that reaches them is the provider's own bid list.
          'Revising or withdrawing an offer arrives with your bids list.',
          key: const Key('bid-revision-later'),
          style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
        ),
      ],
    );
  }
}

/// The price and the two commitments.
///
/// ## Two instants, not two windows, and the form says so
///
/// The job carries two *windows*, because a customer says "any time Thursday". A bid answers with
/// two *instants*, because a provider says "I will be there at nine and it will be there by five"
/// (SHIP-84). The labels are written to make that the obvious reading.
///
/// ## What is checked here, and what deliberately is not
///
/// `Docs/07` §2 leaves the app presence and shapes and puts the rules on the platform:
///
/// - **Presence**, for all three required fields. A round trip to be told a price was left blank is
///   a round trip on mobile data in a truck yard.
/// - **Shape**, for the amount, because this form has to turn what was typed into a whole number of
///   cents and cannot send `four fifty`.
///
/// **No bound and no ordering rule.** That collection has to be in the future, that delivery has to
/// be after collection, and what the largest acceptable offer is are all `internal/bidding`'s, and
/// they arrive as `validation_failed` with one `details` entry per field — rendered under the input
/// that caused it. Duplicating them here would put a second copy of a rule on a device that cannot
/// be corrected without a store release.
class _BidForm extends StatefulWidget {
  const _BidForm({
    required this.busy,
    required this.alreadyBid,
    required this.onSubmit,
    this.failure,
  });

  /// Whether an offer is in flight. The submit is disabled while it is true, which is what stops a
  /// second tap becoming a second attempt.
  final bool busy;

  /// Whether the platform said this provider already has a live offer on this job.
  final bool alreadyBid;

  final ApiFailure? failure;
  final void Function(BidPlacement bid) onSubmit;

  @override
  State<_BidForm> createState() => _BidFormState();
}

class _BidFormState extends State<_BidForm> {
  final _form = GlobalKey<FormState>();

  final _amount = TextEditingController();
  final _message = TextEditingController();

  DateTime? _pickupAt;
  DateTime? _deliverBy;

  /// Fields the provider has touched since the platform's last answer.
  ///
  /// A server message that outlives the value it was about is worse than none: it sends somebody
  /// looking for a mistake in a price they have already corrected. The same mechanism, and the same
  /// reasoning, as the vehicle form's.
  final _edited = <String>{};

  @override
  void didUpdateWidget(_BidForm oldWidget) {
    super.didUpdateWidget(oldWidget);
    if (!identical(widget.failure, oldWidget.failure)) _edited.clear();
  }

  @override
  void dispose() {
    _amount.dispose();
    _message.dispose();
    super.dispose();
  }

  /// What the platform said about each field, keyed by the contract's own field names.
  Map<String, String> get _serverErrors => switch (widget.failure) {
        ApiErrorResponse(:final fieldMessages) => fieldMessages,
        _ => const <String, String>{},
      };

  String? _serverMessage(String field) => _edited.contains(field) ? null : _serverErrors[field];

  void _touched(String field) {
    if (_edited.add(field)) setState(() {});
  }

  void _submit() {
    if (!(_form.currentState?.validate() ?? false)) return;

    final cents = centsFromAud(_amount.text);
    final pickup = _pickupAt;
    final deliver = _deliverBy;
    if (cents == null || pickup == null || deliver == null) return;

    widget.onSubmit(
      BidPlacement(
        amountCents: cents,
        pickupAt: rfc3339(pickup),
        deliverBy: rfc3339(deliver),
        message: _message.text,
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final failure = widget.failure;

    // Field-level messages go under their inputs; anything else goes in the banner. `409
    // bidding_already_bid` is neither: it is about the job's state rather than about a value, and it
    // gets its own note because the provider's next action is a different screen, not a correction.
    final showBanner = failure != null && _serverErrors.isEmpty && !widget.alreadyBid;

    return Form(
      key: _form,
      autovalidateMode: AutovalidateMode.onUserInteraction,
      child: Column(
        key: const Key('bid-form'),
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Offer to carry this job', style: theme.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            // Said because a provider pricing a job will look for the customer's budget, and there
            // is none to look for. Docs/01 §4.3 keeps the customer's maximum private in every form —
            // not as an amount, not as a band, and not as a flag saying one was set. Saying so is
            // better than a provider inferring that this build simply does not show it.
            'Shipper never shows you what the customer is willing to pay. Price the job on what it '
            'is worth to you.',
            key: const Key('bid-no-budget-note'),
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
          const SizedBox(height: 16),

          if (showBanner) ...[
            FailureBanner(failure),
            const SizedBox(height: 16),
          ],

          if (widget.alreadyBid && failure != null) ...[
            _AlreadyBid(failure: failure),
            const SizedBox(height: 16),
          ],

          TextFormField(
            key: const Key('bid-amount'),
            controller: _amount,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            textInputAction: TextInputAction.next,
            decoration: const InputDecoration(
              labelText: 'Your price',
              prefixText: r'$',
              // AUD, because CLAUDE.md fixes the currency for the whole product and there is no
              // second one. Whole dollars are ordinary and the cents are optional.
              helperText: 'Australian dollars, for the whole job.',
            ),
            onChanged: (_) => _touched('amount_cents'),
            validator: (value) => _serverMessage('amount_cents') ?? Validators.audAmount(value),
          ),
          const SizedBox(height: 24),

          Text('Your commitment', style: theme.textTheme.titleMedium),
          const SizedBox(height: 4),
          Text(
            // The distinction the contract draws, in the words a provider would use. The job says
            // when the customer is flexible; the bid says when this provider will actually be there.
            'A time, not a window — when you will collect, and when it will be delivered.',
            key: const Key('bid-timing-note'),
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 16),

          InstantField(
            fieldKey: 'bid-pickup',
            label: 'Collecting at',
            value: _pickupAt,
            serverMessage: _serverMessage('pickup_at'),
            missing: 'Say when you can collect.',
            onChanged: (at) {
              setState(() => _pickupAt = at);
              _touched('pickup_at');
            },
          ),
          const SizedBox(height: 16),

          InstantField(
            fieldKey: 'bid-deliver-by',
            label: 'Delivered by',
            value: _deliverBy,
            serverMessage: _serverMessage('deliver_by'),
            missing: 'Say when it will be delivered.',
            onChanged: (at) {
              setState(() => _deliverBy = at);
              _touched('deliver_by');
            },
          ),
          const SizedBox(height: 24),

          TextFormField(
            key: const Key('bid-message'),
            controller: _message,
            maxLines: 3,
            decoration: const InputDecoration(
              labelText: 'Conditions (optional)',
              helperText: 'Access, timing caveats, what the price includes — in your own words.',
            ),
            onChanged: (_) => _touched('message'),
            validator: (_) => _serverMessage('message'),
          ),
          const SizedBox(height: 24),

          SizedBox(
            width: double.infinity,
            child: FilledButton(
              key: const Key('bid-submit'),
              // Disabled while the offer is in flight. Two taps are two actions, and the
              // idempotency key is per action (`Docs/07` §4) — so the second would not be absorbed
              // by the first, and one of them would be refused as a second offer.
              onPressed: widget.busy ? null : _submit,
              child: widget.busy
                  ? const SizedBox(
                      height: 20,
                      width: 20,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Text('Send this offer'),
            ),
          ),
          const SizedBox(height: 8),
          Text(
            // Docs/07 §4 excludes bidding from the offline queue: competitive, time-sensitive and
            // multi-party, and a stale local decision is worse than an honest "you are offline". So
            // the provider is told which of the two this screen is, rather than discovering it.
            'Offers are sent straight away and are not held on your phone. If you have no signal, '
            'try again when you do.',
            key: const Key('bid-not-queued-note'),
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      ),
    );
  }
}

/// The `409` that is about the job rather than about a value.
///
/// A live offer is one the customer may accept at any moment, so two from one provider would be two
/// prices for one job — which is what `uq_bids_one_submitted_per_provider_per_job` refuses. **This
/// is not a retry**: a retry carries the key its first attempt carried and is answered with the
/// offer that attempt placed.
class _AlreadyBid extends StatelessWidget {
  const _AlreadyBid({required this.failure});

  final ApiFailure failure;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      key: const Key('bid-already-placed'),
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.secondaryContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            // The platform's own wording, shown and never switched on (`Docs/07` §6).
            failure.userMessage,
            style: theme.textTheme.bodyMedium
                ?.copyWith(color: theme.colorScheme.onSecondaryContainer),
          ),
          const SizedBox(height: 4),
          Text(
            'Your offers are listed on your bids screen once it arrives.',
            style: theme.textTheme.bodySmall
                ?.copyWith(color: theme.colorScheme.onSecondaryContainer),
          ),
        ],
      ),
    );
  }
}
