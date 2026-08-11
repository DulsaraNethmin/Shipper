import 'dart:async';

import 'package:flutter/material.dart';

/// "Send it again", with the wait the platform asked for (SHIP-53, SHIP-54).
///
/// Both verification screens need it and they need it identically, so it is written once here
/// rather than twice — an email and an SMS are the same throttle with different nouns.
///
/// ## The wait is the platform's, and it is not the truth
///
/// `retry_after_seconds` is a **fixed** interval rather than the remaining cooldown, and that is
/// deliberate on the platform's side: the true remaining time would say "this address was
/// written to recently", which says "this address has an account", which makes "does this person
/// use Shipper" answerable one address at a time by anybody
/// (`contracts/paths/identity.yaml`).
///
/// Two consequences for this widget:
///
/// - **A timer that has run out does not mean a message will be sent.** The platform's real
///   limits are one per minute and five per hour, applied per account, and exceeding either
///   sends nothing while still answering `202`. So the label says "send it again", never "you
///   may now" or "a message is on its way".
/// - **The countdown is a courtesy, not a control.** The platform enforces the limit; this stops
///   somebody tapping four times because nothing appeared to happen.
class ResendButton extends StatefulWidget {
  const ResendButton({
    super.key,
    required this.label,
    required this.onResend,
    this.initialWait,
    this.enabled = true,
  });

  /// What the button says when it is available. The waiting form is derived from it.
  final String label;

  /// Asks the platform again. Answers with the interval to wait before asking once more, or
  /// `null` when nothing was sent — a refusal, or an address the app does not know.
  final Future<Duration?> Function() onResend;

  /// A wait already in force when the button appears.
  ///
  /// The phone screen requests a code as it opens, so its resend starts inside a cooldown that
  /// this widget did not begin.
  final Duration? initialWait;

  /// `false` when there is nothing to resend to. The button is shown disabled rather than hidden
  /// so that its absence is never mistaken for a missing feature (`Docs/07` §3: the app may hide
  /// or disable, and the platform decides).
  final bool enabled;

  @override
  State<ResendButton> createState() => _ResendButtonState();
}

class _ResendButtonState extends State<ResendButton> {
  Duration _remaining = Duration.zero;
  Timer? _ticker;
  bool _sending = false;

  @override
  void initState() {
    super.initState();
    if (widget.initialWait != null) _startWaiting(widget.initialWait!);
  }

  @override
  void didUpdateWidget(ResendButton oldWidget) {
    super.didUpdateWidget(oldWidget);
    final wait = widget.initialWait;
    if (wait != null && wait != oldWidget.initialWait) _startWaiting(wait);
  }

  @override
  void dispose() {
    _ticker?.cancel();
    super.dispose();
  }

  void _startWaiting(Duration wait) {
    _ticker?.cancel();
    if (wait <= Duration.zero) {
      setState(() => _remaining = Duration.zero);
      return;
    }

    setState(() => _remaining = wait);
    // Cancels itself at zero rather than ticking on. A periodic timer left running keeps
    // scheduling frames, which is a battery cost in the app and a hang in any test that settles.
    _ticker = Timer.periodic(const Duration(seconds: 1), (timer) {
      if (!mounted) return;
      final next = _remaining - const Duration(seconds: 1);
      if (next <= Duration.zero) {
        timer.cancel();
        setState(() => _remaining = Duration.zero);
        return;
      }
      setState(() => _remaining = next);
    });
  }

  Future<void> _resend() async {
    setState(() => _sending = true);
    final wait = await widget.onResend();
    if (!mounted) return;

    setState(() => _sending = false);
    // A failure leaves the button available: the platform did not accept the request, so there
    // is nothing to wait out.
    if (wait != null) _startWaiting(wait);
  }

  /// `m:ss`, because a bare "59" reads as an error code and "59 seconds" is wider than the
  /// button. Australian English and day-first dates are `CLAUDE.md`'s; this is neither.
  static String _clock(Duration remaining) {
    final seconds = remaining.inSeconds;
    return '${seconds ~/ 60}:${(seconds % 60).toString().padLeft(2, '0')}';
  }

  @override
  Widget build(BuildContext context) {
    final waiting = _remaining > Duration.zero;
    final available = widget.enabled && !waiting && !_sending;

    return TextButton(
      key: const Key('resend'),
      onPressed: available ? () => unawaited(_resend()) : null,
      child: Text(waiting ? '${widget.label} in ${_clock(_remaining)}' : widget.label),
    );
  }
}
