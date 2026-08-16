import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/bidding/bid.dart';
import 'package:shipper/features/bidding/bid_status.dart';
import 'package:shipper/features/bidding/instant_field.dart';
import 'package:shipper/features/bidding/message.dart';
import 'package:shipper/features/bidding/negotiation_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/formatting/dates.dart';
import 'package:shipper/shared/formatting/money.dart';
import 'package:shipper/shared/validation/validators.dart';

/// One negotiation, as either party sees it — `Docs/01` §4.2's "negotiate" and `Docs/02` §4's
/// alternating chain, with the conversation that a price and two dates cannot carry (SHIP-103).
///
/// ## One screen for both parties, which is the platform's own shape
///
/// The customer opens it from an offer on their delivery; the provider opens it from their own bid.
/// Both read `…/history` and `…/messages` and both write `…/counter` and `…/messages` — four
/// endpoints, each of which serves **both** sides and works out which from the credential. Two
/// screens would be two renderings of one exchange, and the first thing they would disagree about is
/// whose turn it is.
///
/// What differs is one word: which side of the thread is "you". That comes from the session's role
/// and decides alignment and a label. **It decides nothing else.** `Docs/07` §3 puts every
/// authorisation decision on the platform, and a caller who is not a party to this negotiation gets
/// the byte-identical `404` a bid that does not exist gets — so a device that got the role wrong
/// would draw a bubble on the wrong side and disclose nothing.
///
/// ## The budget, and why this screen is the most dangerous surface in the product for it
///
/// `Docs/01` §4.3 keeps the customer's maximum away from a provider — **not as an amount, not as a
/// band, and not as a "budget supplied" indicator.** This is the one screen that renders both
/// parties' words beside each other, next to a form for proposing a number, and the third clause is
/// the one that survives everything else:
///
///     a sentence saying a maximum exists carries no field, no value and no digit.
///
/// A hint above the counter form reading "the customer has set a maximum" would pass a closed key
/// set over every model here, a source scan for `budget_cents`, and an AST walk. Wave 10 and wave 11
/// both proved exactly that. So `negotiation_test.dart` asserts on the **words rendered** in the
/// provider's view, which is the only guard shaped like the failure.
///
/// **Two numbers on this screen are legitimate and must not be confused with it.** The provider's
/// price is theirs. The customer's counter amount is a number the customer *chose to put in front of
/// a provider*, and the contract says so on the field itself. Neither is derived from the maximum
/// and nothing here may derive one from the other.
///
/// **And a party's own words are their own.** If a customer types their limit into a message, this
/// screen renders it, because the platform stores what a party wrote unaltered and §4.3 binds
/// Shipper rather than the customer's mouth. Suppressing words inside party-authored text would
/// censor a negotiation and leave the actual hole — this file's own copy — untouched.
///
/// ## Nothing here is queued
///
/// `Docs/07` §4 lists negotiation beside bidding and awarding as deliberately not offline: "a stale
/// local decision is worse than an honest 'you are offline'". `OperationKind`'s constructor is
/// private and its two members are both `delivery.*`, so neither a message nor a counter can be
/// enqueued — closed by the compiler rather than by a rule.
class NegotiationScreen extends ConsumerWidget {
  const NegotiationScreen({required this.jobId, required this.bidId, super.key});

  /// The job. From the route, which is the only place it comes from.
  final String jobId;

  /// **Any** offer in the chain. The platform addresses the whole negotiation through any one of
  /// them, so this is whichever row the screen was opened from and never has to move.
  final String bidId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final address = (jobId: jobId, bidId: bidId);
    final state = ref.watch(negotiationProvider(address));
    final controller = ref.read(negotiationProvider(address).notifier);
    final viewer = viewerParty(ref.watch(sessionProvider));

    return Scaffold(
      appBar: AppBar(title: const Text('Negotiation')),
      body: SafeArea(
        child: switch (state) {
          final s when s.isFirstLoad => const Center(
              key: Key('negotiation-loading'),
              child: CircularProgressIndicator(),
            ),
          final s when s.failedOutright => _Failed(
              failure: s.failure!,
              onRetry: controller.retry,
            ),
          _ => _Exchange(state: state, controller: controller, viewer: viewer),
        },
      ),
    );
  }
}

/// Which side of a negotiation this account is on, or `null` while that is not yet known.
///
/// **Presentation only, and the `null` case is the one worth being careful about.** A restored cold
/// start knows it is signed in and does not yet know as whom — the role is a claim in the access
/// token and arrives one refresh later (SHIP-50). Guessing either way would draw somebody's own
/// messages as the other party's, so the thread renders neutrally until the answer arrives.
@visibleForTesting
BidParty? viewerParty(SessionState session) {
  final role = switch (session) {
    SessionSignedIn(:final role) => role,
    _ => null,
  };

  return switch (role) {
    UserRole.customer => BidParty.customer,
    UserRole.provider => BidParty.provider,
    // A role this build has never heard of is not a third party to a negotiation — there are two.
    UserRole.unknown || null => null,
  };
}

/// The negotiation itself: the offers, the conversation, and the two ways to answer.
class _Exchange extends StatelessWidget {
  const _Exchange({required this.state, required this.controller, required this.viewer});

  final NegotiationState state;
  final NegotiationController controller;
  final BidParty? viewer;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return RefreshIndicator(
      onRefresh: controller.refresh,
      child: ListView(
        key: const Key('negotiation-body'),
        padding: const EdgeInsets.symmetric(vertical: 16),
        children: [
          if (state.failure != null)
            Padding(
              padding: const EdgeInsets.fromLTRB(16, 0, 16, 16),
              child: _Stale(state.failure!),
            ),

          // 1. Where the terms stand, which is what a negotiation is about.
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text('Offers', style: theme.textTheme.titleMedium),
          ),
          const SizedBox(height: 8),
          _Chain(state: state, viewer: viewer),
          const SizedBox(height: 8),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: _Standing(state: state, viewer: viewer),
          ),

          // 2. Answering with different terms.
          if (state.liveOffer != null) ...[
            const SizedBox(height: 16),
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 16),
              child: _Counter(state: state, controller: controller, viewer: viewer),
            ),
          ],

          const Divider(height: 40),

          // 3. The words.
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: Text('Messages', style: theme.textTheme.titleMedium),
          ),
          const SizedBox(height: 8),
          if (state.hasMore) ...[
            Center(
              child: state.loadingMore
                  ? const CircularProgressIndicator()
                  : OutlinedButton(
                      key: const Key('negotiation-more'),
                      onPressed: () => unawaited(controller.loadMore()),
                      // The conversation arrives oldest first, so a further page is what was said
                      // **after** this — the opposite direction from every other list in this app,
                      // and worth saying in the label rather than leaving to be discovered.
                      child: const Text('Show later messages'),
                    ),
            ),
            const SizedBox(height: 8),
          ],
          if (state.conversationIsEmpty)
            const _NothingSaidYet()
          else
            for (final message in state.messages)
              _Bubble(message: message, viewer: viewer),

          const SizedBox(height: 16),
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 16),
            child: _Composer(state: state, controller: controller),
          ),
        ],
      ),
    );
  }
}

/// Every offer in the chain, oldest first.
///
/// **Including the ones that were superseded**, which is the whole point of reading a history rather
/// than the live offer: `Docs/01` §4.3 requires the platform to record all offers, counter-offers
/// and withdrawals, and `Docs/02` §4 keeps that record visible to both parties. Somebody deciding
/// whether to accept forty thousand needs to see that it started at fifty-two.
class _Chain extends StatelessWidget {
  const _Chain({required this.state, required this.viewer});

  final NegotiationState state;
  final BidParty? viewer;

  @override
  Widget build(BuildContext context) {
    if (state.offers.isEmpty) {
      return const Padding(
        padding: EdgeInsets.symmetric(horizontal: 16),
        child: Text(
          'No offers have been read yet.',
          key: Key('negotiation-no-offers'),
        ),
      );
    }

    final head = state.liveOffer;

    return Column(
      key: const Key('negotiation-chain'),
      children: [
        for (final offer in state.offers)
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
            child: _OfferRow(offer: offer, viewer: viewer, isHead: offer.id == head?.id),
          ),
      ],
    );
  }
}

/// One round of the negotiation.
class _OfferRow extends StatelessWidget {
  const _OfferRow({required this.offer, required this.viewer, required this.isHead});

  final Bid offer;
  final BidParty? viewer;

  /// Whether this is the offer everything can still be done to.
  final bool isHead;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final amount = offer.amountCents;
    final mine = viewer != null && offer.offeredBy == viewer;

    return Card(
      key: Key('negotiation-offer-${offer.id}'),
      margin: EdgeInsets.zero,
      color: isHead ? theme.colorScheme.secondaryContainer : null,
      child: Padding(
        padding: const EdgeInsets.all(12),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    // Cents to dollars at the point of display and nowhere earlier (`Docs/10` §3.3).
                    amount == null ? 'No price on this offer' : audFromCents(amount),
                    key: Key('negotiation-offer-amount-${offer.id}'),
                    style: theme.textTheme.titleMedium,
                  ),
                ),
                Text(
                  // Whose round this was. **Not derivable from position** — a party may join a
                  // negotiation halfway — which is why the platform sends `offered_by` and this
                  // renders it rather than counting rows.
                  switch (offer.offeredBy) {
                    null => 'Offer',
                    BidParty.unknown => 'Offer',
                    _ => mine ? 'Yours' : 'Theirs',
                  },
                  key: Key('negotiation-offer-party-${offer.id}'),
                  style: theme.textTheme.labelMedium,
                ),
              ],
            ),
            const SizedBox(height: 4),
            Text(
              'Collect ${dayFirstDateTime(offer.pickupAt) ?? 'not stated'} · '
              'deliver by ${dayFirstDateTime(offer.deliverBy) ?? 'not stated'}',
              key: Key('negotiation-offer-timing-${offer.id}'),
              style: theme.textTheme.bodySmall,
            ),
            if (offer.message case final conditions? when conditions.isNotEmpty) ...[
              const SizedBox(height: 4),
              Text(
                conditions,
                key: Key('negotiation-offer-conditions-${offer.id}'),
                style: theme.textTheme.bodySmall,
              ),
            ],
            if (isHead) ...[
              const SizedBox(height: 4),
              Text(
                'On the table',
                key: Key('negotiation-offer-head-${offer.id}'),
                style: theme.textTheme.labelSmall,
              ),
            ],
          ],
        ),
      ),
    );
  }
}

/// Where the negotiation stands, in one sentence.
///
/// **A sentence rather than a badge**, because "answered and waiting" and "closed" are different
/// things to be told and a colour communicates neither. It is derived from the chain rather than
/// carried as a field: the party who made the live offer is the party waiting.
class _Standing extends StatelessWidget {
  const _Standing({required this.state, required this.viewer});

  final NegotiationState state;
  final BidParty? viewer;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    if (state.liveOffer == null) {
      return Text(
        // Withdrawn, rejected, expired or awarded — one code covers all of them on the write side
        // and this device cannot tell which, so it says the one thing true of every case. The
        // conversation stays open underneath, which is deliberate: the endpoint has no status rule.
        'There is no offer on the table. Nothing further can be proposed here, and you can still '
        'send a message.',
        key: const Key('negotiation-closed'),
        style: theme.textTheme.bodyMedium,
      );
    }

    return Text(
      state.isAwaiting(viewer)
          ? 'It is your turn. Answer with different terms, or send a message.'
          : 'Your terms are with the other party. They can accept them, answer them, or write back.',
      key: const Key('negotiation-standing'),
      style: theme.textTheme.bodyMedium,
    );
  }
}

/// Answering the offer on the table with different terms.
///
/// Collapsed until it is asked for. A form permanently open above a conversation makes the screen
/// about the form; a negotiation is mostly reading what the other party said.
class _Counter extends StatefulWidget {
  const _Counter({required this.state, required this.controller, required this.viewer});

  final NegotiationState state;
  final NegotiationController controller;
  final BidParty? viewer;

  @override
  State<_Counter> createState() => _CounterState();
}

class _CounterState extends State<_Counter> {
  final _form = GlobalKey<FormState>();
  final _amount = TextEditingController();
  final _conditions = TextEditingController();

  bool _open = false;
  DateTime? _pickupAt;
  DateTime? _deliverBy;

  /// The offer these fields were seeded from, so a head that changes under an open form re-seeds it.
  String? _seededFrom;

  /// Set when the person pressed send and the counter changed nothing.
  ///
  /// Not a server error: the platform would refuse it `400`, and spending a round trip to be told
  /// what this device already knows is the one case worth answering locally.
  bool _changedNothing = false;

  @override
  void dispose() {
    _amount.dispose();
    _conditions.dispose();
    super.dispose();
  }

  /// Fills the form from the offer on the table.
  ///
  /// **Seeded rather than blank, and that is what makes "send only what changes" usable.** A counter
  /// carries a difference, so the form has to show what it is a difference *from* — and the fields
  /// left untouched are the ones the platform inherits.
  void _seed(Bid head) {
    _seededFrom = head.id;
    final amount = head.amountCents;
    _amount.text = amount == null ? '' : (amount / 100).toStringAsFixed(2);
    _conditions.text = head.message ?? '';
    _pickupAt = null;
    _deliverBy = null;
    _changedNothing = false;
  }

  /// What this form is proposing, as a difference from [head].
  ///
  /// Every field compares against the offer being answered, so an untouched field sends nothing and
  /// the platform inherits it. The conditions are the exception the contract asks for: an emptied
  /// box sends `''`, which **clears** them, and is distinguishable from leaving them alone.
  BidCounter _difference(Bid head) {
    final cents = centsFromAud(_amount.text);
    final conditions = _conditions.text.trim();
    final wasConditions = (head.message ?? '').trim();

    return BidCounter(
      amountCents: cents != null && cents != head.amountCents ? cents : null,
      pickupAt: _pickupAt == null ? null : rfc3339(_pickupAt!),
      deliverBy: _deliverBy == null ? null : rfc3339(_deliverBy!),
      message: conditions == wasConditions ? null : conditions,
    );
  }

  Future<void> _submit(Bid head) async {
    if (!(_form.currentState?.validate() ?? false)) return;

    final counter = _difference(head);
    if (!counter.namesSomething) {
      setState(() => _changedNothing = true);
      return;
    }

    setState(() => _changedNothing = false);
    final sent = await widget.controller.counter(counter);
    if (!mounted || !sent) return;

    // The chain has been re-read and the head has moved. Closing the form is what says so — leaving
    // it open over a superseded offer would invite countering an offer that no longer exists.
    setState(() => _open = false);
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final head = widget.state.liveOffer;
    if (head == null) return const SizedBox.shrink();

    if (_seededFrom != head.id) _seed(head);

    if (!_open) {
      return Align(
        alignment: Alignment.centerLeft,
        child: OutlinedButton.icon(
          key: const Key('negotiation-counter-open'),
          onPressed: () => setState(() => _open = true),
          icon: const Icon(Icons.swap_horiz),
          // The same word the platform uses, so that a refusal naming a counter-offer is about the
          // thing on the button.
          label: const Text('Counter this offer'),
        ),
      );
    }

    final refusal = widget.state.counterRefusal;
    final fields = widget.state.counterFieldMessages;

    return Form(
      key: _form,
      // **`always`, where the placement form is `onUserInteraction`, and the difference is this
      // endpoint's own.** The platform validates the *merged* offer, so a refusal can name a field
      // nobody on this device touched — a counter on price alone against an offer whose collection
      // time has since passed is a `422` naming `pickup_at`. Under `onUserInteraction` that message
      // waits for somebody to interact with a field they have no reason to touch, so the
      // explanation for the refusal simply never appears. Nothing here validates on first paint
      // anyway: the price is seeded from the offer, both instants are optional, and the conditions
      // field's only rule is whatever the platform last said about it.
      autovalidateMode: AutovalidateMode.always,
      child: Column(
        key: const Key('negotiation-counter-form'),
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Answer with different terms', style: theme.textTheme.titleSmall),
          const SizedBox(height: 4),
          Text(
            // What "send only what changes" means, said once where somebody is about to rely on it.
            'Anything you leave as it is stays as it is. Changing nothing at all is agreement, '
            'which is a different thing from a counter-offer.',
            key: const Key('negotiation-counter-note'),
            style: theme.textTheme.bodySmall,
          ),
          const SizedBox(height: 16),

          if (refusal != null) ...[
            _Refused(
              refusalKey: const Key('negotiation-counter-refused'),
              message: refusal,
              onDismiss: widget.controller.dismissCounterFailure,
            ),
            const SizedBox(height: 16),
          ],

          if (_changedNothing) ...[
            Text(
              'This counter-offer changes nothing. Propose a different price, a different time or '
              'different conditions — or award the delivery if you agree with it.',
              key: const Key('negotiation-counter-unchanged'),
              style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.error),
            ),
            const SizedBox(height: 16),
          ],

          TextFormField(
            key: const Key('negotiation-counter-amount'),
            controller: _amount,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            textInputAction: TextInputAction.next,
            decoration: const InputDecoration(
              labelText: 'Your price',
              prefixText: r'$',
              helperText: 'Australian dollars, for the whole job. Clear the box to leave the '
                  'price as it is.',
            ),
            validator: (value) {
              // The platform's message wins over the local rule, exactly as the placement form
              // does it: a `422` names the field and this is where that field's message belongs.
              final refused = fields['amount_cents'];
              if (refused != null) return refused;

              // **Empty is legitimate here and is not on the placement form**, which is the one
              // way the two forms genuinely differ: a placement states a price and a counter states
              // a difference, so an empty box means "keep the one on the table" rather than "you
              // forgot something". Reusing `Validators.audAmount` unchanged would refuse it with a
              // sentence written for the other form.
              final typed = (value ?? '').trim();
              return typed.isEmpty ? null : Validators.audAmount(typed);
            },
          ),
          const SizedBox(height: 16),

          InstantField(
            fieldKey: 'negotiation-counter-pickup',
            label: 'Collecting at',
            value: _pickupAt,
            // Optional, unlike the placement form's: leaving it alone keeps the time on the table.
            // `missing` is therefore null rather than a sentence.
            unset: 'Keeping the time on the table',
            serverMessage: fields['pickup_at'],
            onChanged: (at) => setState(() => _pickupAt = at),
          ),
          const SizedBox(height: 16),

          InstantField(
            fieldKey: 'negotiation-counter-deliver',
            label: 'Delivered by',
            value: _deliverBy,
            unset: 'Keeping the time on the table',
            serverMessage: fields['deliver_by'],
            onChanged: (at) => setState(() => _deliverBy = at),
          ),
          const SizedBox(height: 16),

          TextFormField(
            key: const Key('negotiation-counter-message'),
            controller: _conditions,
            maxLines: 3,
            decoration: const InputDecoration(
              labelText: 'Conditions (optional)',
              // The one tri-state on this form, and the only place a user can be told about it.
              helperText: 'Clear the box to remove the conditions on the offer.',
            ),
            validator: (_) => fields['message'],
          ),
          const SizedBox(height: 16),

          Row(
            children: [
              Expanded(
                child: FilledButton(
                  key: const Key('negotiation-counter-submit'),
                  // Disabled while one is in flight. Two taps are two actions and the key is per
                  // action (`Docs/07` §4), so the second would be a second counter rather than a
                  // retry of the first.
                  onPressed: widget.state.countering ? null : () => unawaited(_submit(head)),
                  child: widget.state.countering
                      ? const SizedBox(
                          height: 18,
                          width: 18,
                          child: CircularProgressIndicator(strokeWidth: 2),
                        )
                      : const Text('Send counter-offer'),
                ),
              ),
              const SizedBox(width: 8),
              TextButton(
                key: const Key('negotiation-counter-cancel'),
                onPressed: () => setState(() => _open = false),
                child: const Text('Cancel'),
              ),
            ],
          ),
          const SizedBox(height: 8),
          Text(
            // `Docs/07` §4 excludes negotiation from the offline queue, so the person is told which
            // of the two this screen is rather than discovering it.
            'Counter-offers are sent straight away and are not held on your phone.',
            key: const Key('negotiation-not-queued-note'),
            style: theme.textTheme.bodySmall?.copyWith(color: theme.colorScheme.onSurfaceVariant),
          ),
        ],
      ),
    );
  }
}

/// One message, on the side of the thread that wrote it.
class _Bubble extends StatelessWidget {
  const _Bubble({required this.message, required this.viewer});

  final Message message;
  final BidParty? viewer;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final mine = message.isFrom(viewer);
    final at = dayFirstDateTime(message.createdAt);

    return Padding(
      padding: const EdgeInsets.fromLTRB(16, 0, 16, 8),
      child: Align(
        alignment: mine ? Alignment.centerRight : Alignment.centerLeft,
        child: Container(
          key: Key('negotiation-message-${message.id}'),
          constraints: const BoxConstraints(maxWidth: 280),
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: mine ? theme.colorScheme.primaryContainer : theme.colorScheme.surfaceContainerHigh,
            borderRadius: BorderRadius.circular(12),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                // **Rendered exactly as it was written.** The platform contributes no prose to a
                // message and neither does this widget: what is here is one party's own words.
                message.body,
                key: Key('negotiation-message-body-${message.id}'),
                style: theme.textTheme.bodyMedium,
              ),
              if (at != null) ...[
                const SizedBox(height: 4),
                Text(
                  // Who and when, in one line. "You" and "them" rather than "customer" and
                  // "provider": a conversation does not alternate and either party may write twice
                  // in a row, so `sent_by` is what says which — never the position in the list.
                  mine ? 'You · $at' : 'Them · $at',
                  key: Key('negotiation-message-at-${message.id}'),
                  style: theme.textTheme.labelSmall,
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

/// Writing one.
class _Composer extends StatefulWidget {
  const _Composer({required this.state, required this.controller});

  final NegotiationState state;
  final NegotiationController controller;

  @override
  State<_Composer> createState() => _ComposerState();
}

class _ComposerState extends State<_Composer> {
  final _body = TextEditingController();

  @override
  void dispose() {
    _body.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    final sent = await widget.controller.send(_body.text);
    // **Cleared only when the platform took it.** A composer emptied on a failure throws away what
    // somebody wrote along with any chance of sending it, and the failure most likely to happen
    // here is no signal.
    if (mounted && sent) _body.clear();
  }

  @override
  Widget build(BuildContext context) {
    final refusal = widget.state.sendRefusal;

    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        if (refusal != null) ...[
          _Refused(
            refusalKey: const Key('negotiation-send-refused'),
            message: refusal,
            onDismiss: widget.controller.dismissSendFailure,
          ),
          const SizedBox(height: 8),
        ],
        Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Expanded(
              child: TextField(
                key: const Key('negotiation-composer'),
                controller: _body,
                maxLines: 4,
                minLines: 1,
                // The platform's own limit, so an over-long message is refused by the keyboard
                // rather than by a round trip. `maxLength` is deliberately not rendered as a
                // counter: 2000 characters is far past what anybody types into a delivery
                // negotiation, and a counter under every box implies a target.
                maxLength: 2000,
                decoration: const InputDecoration(
                  labelText: 'Write to the other party',
                  counterText: '',
                  helperText: 'Questions a price cannot answer — access, timing, what is being '
                      'moved.',
                ),
              ),
            ),
            const SizedBox(width: 8),
            IconButton.filled(
              key: const Key('negotiation-send'),
              onPressed: widget.state.sending ? null : () => unawaited(_send()),
              icon: widget.state.sending
                  ? const SizedBox(
                      height: 18,
                      width: 18,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.send),
              tooltip: 'Send',
            ),
          ],
        ),
      ],
    );
  }
}

/// Nobody has written anything yet.
///
/// **An ordinary state and the first one either party meets**, so it says what the box below is for
/// rather than apologising.
class _NothingSaidYet extends StatelessWidget {
  const _NothingSaidYet();

  @override
  Widget build(BuildContext context) {
    return const Padding(
      padding: EdgeInsets.symmetric(horizontal: 16),
      child: Text(
        'No messages yet. Counter-offers say what the terms are; this is where the questions go.',
        key: Key('negotiation-no-messages'),
      ),
    );
  }
}

/// The platform refused a write, in words somebody can act on.
///
/// The sentence comes from the state's own mapping of `error.code` rather than from the platform's
/// `message` (`Docs/07` §6, `CLAUDE.md`): clients branch on the code, and a message is copy that
/// gets reworded.
class _Refused extends StatelessWidget {
  const _Refused({
    required this.refusalKey,
    required this.message,
    required this.onDismiss,
  });

  final Key refusalKey;
  final String message;
  final VoidCallback onDismiss;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Container(
      key: refusalKey,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: theme.colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Expanded(
            child: Text(
              message,
              style: TextStyle(color: theme.colorScheme.onErrorContainer),
            ),
          ),
          TextButton(
            onPressed: onDismiss,
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }
}

/// A reload failed and the exchange already read is still on screen.
class _Stale extends StatelessWidget {
  const _Stale(this.failure);

  final ApiFailure failure;

  @override
  Widget build(BuildContext context) {
    return Container(
      key: const Key('negotiation-stale'),
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Theme.of(context).colorScheme.errorContainer,
        borderRadius: BorderRadius.circular(8),
      ),
      child: Text(
        'This negotiation may be out of date. ${failure.userMessage}',
        style: TextStyle(color: Theme.of(context).colorScheme.onErrorContainer),
      ),
    );
  }
}

/// Nothing arrived at all.
class _Failed extends StatelessWidget {
  const _Failed({required this.failure, required this.onRetry});

  final ApiFailure failure;
  final Future<void> Function() onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      key: const Key('negotiation-failed'),
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            FailureBanner(failure),
            const SizedBox(height: 16),
            FilledButton(
              key: const Key('negotiation-retry'),
              onPressed: () => unawaited(onRetry()),
              child: const Text('Try again'),
            ),
          ],
        ),
      ),
    );
  }
}
