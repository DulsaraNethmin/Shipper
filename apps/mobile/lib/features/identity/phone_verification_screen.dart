import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/identity/resend_button.dart';
import 'package:shipper/features/identity/signup_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/validation/validators.dart';

/// Confirm the mobile number (SHIP-54).
///
/// `POST /v1/auth/request-otp` (SHIP-34) and `POST /v1/auth/verify-phone` (SHIP-36).
/// `Docs/04` §2 requires both contact channels verified before a customer may publish, so this
/// is the second half of the same gate the email screen is the first half of.
///
/// ## A code is asked for as the screen opens
///
/// Registration issues the *email* token itself, inside the transaction that creates the
/// account (SHIP-31). **It does not send an OTP** — the code costs money and wakes a handset,
/// so the platform sends one only when asked. Somebody arriving here to confirm their number
/// has, by arriving, asked.
///
/// ## Resend throttling, and what the timer actually means
///
/// The platform applies two limits per account: one message a minute, and five an hour.
/// Exceeding either **sends nothing and still answers `202`**, because a different answer for a
/// number it recognises would make "does this person use Shipper" answerable one number at a
/// time by anybody. `retry_after_seconds` is likewise a fixed interval and not the true
/// remaining cooldown, for the same reason.
///
/// So the countdown on the resend button is a courtesy and not a control — the platform enforces
/// the limit whatever this screen does. What it buys is real all the same: without it somebody
/// taps four times because nothing appeared to happen, uses four of their five hourly messages
/// on one minute, and is then locked out of the only one that would have arrived.
///
/// ## One failure code, and no guessing at it
///
/// Wrong code, expired code, no outstanding code, five wrong guesses already used, a number with
/// no account at all: every one answers `identity_otp_invalid`. Six digits is a guessable space,
/// so each distinction the platform declined to make is one this screen must not invent. The
/// remedy is the same in every case, which is why the answer is always shown beside the resend.
class PhoneVerificationScreen extends ConsumerStatefulWidget {
  const PhoneVerificationScreen({super.key});

  @override
  ConsumerState<PhoneVerificationScreen> createState() => _PhoneVerificationScreenState();
}

class _PhoneVerificationScreenState extends ConsumerState<PhoneVerificationScreen> {
  final _form = GlobalKey<FormState>();
  final _code = TextEditingController();

  /// The interval the platform last asked for, handed to the resend button.
  Duration? _wait;

  @override
  void initState() {
    super.initState();

    final signup = ref.read(signupProvider);
    // Nothing to ask about, or nothing to ask for. Requesting a code for an already-verified
    // number would spend one of the account's five hourly messages to tell it something it
    // has already been told.
    if (signup.phone == null || signup.phoneVerified) return;

    WidgetsBinding.instance.addPostFrameCallback((_) => unawaited(_request()));
  }

  @override
  void dispose() {
    _code.dispose();
    super.dispose();
  }

  Future<Duration?> _request() async {
    final wait = await ref.read(signupProvider.notifier).requestOtp();
    if (mounted && wait != null) setState(() => _wait = wait);
    return wait;
  }

  Future<void> _verify() async {
    if (!(_form.currentState?.validate() ?? false)) return;

    final account = await ref.read(signupProvider.notifier).verifyPhone(_code.text.trim());
    if (!mounted || account == null) return;

    // Not navigated automatically: the verified state is half of what this ticket has to show,
    // and replacing it with the next screen would make the criterion true only for a frame.
    setState(() {});
  }

  /// The platform's refusal, when it was about the code rather than about the request.
  static String? _codeMessage(ApiFailure? failure) {
    return switch (failure) {
      ApiErrorResponse(code: 'identity_otp_invalid', :final message) => message,
      _ => null,
    };
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final signup = ref.watch(signupProvider);
    final failure = signup.failure;
    final codeMessage = _codeMessage(failure);

    if (signup.phoneVerified) {
      return _Verified(phone: signup.phone);
    }

    return Scaffold(
      appBar: AppBar(title: const Text('Confirm your mobile')),
      body: SafeArea(
        child: Form(
          key: _form,
          child: ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Text(
                signup.phone == null
                    ? 'Confirm your mobile number from the device you registered on.'
                    // "Sent" rather than "will arrive": the platform will not say whether a
                    // message went out, and a screen that promised one would be wrong every
                    // time a rate limit was in force.
                    : 'We sent a six-digit code to ${signup.phone}. It expires in ten minutes.',
                key: const Key('verify-phone-instructions'),
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 24),
              if (failure != null && codeMessage == null) ...[
                FailureBanner(failure),
                const SizedBox(height: 16),
              ],
              TextFormField(
                key: const Key('verify-phone-code'),
                controller: _code,
                keyboardType: TextInputType.number,
                inputFormatters: [
                  FilteringTextInputFormatter.digitsOnly,
                  LengthLimitingTextInputFormatter(6),
                ],
                autofillHints: const [AutofillHints.oneTimeCode],
                decoration: InputDecoration(
                  labelText: 'Six-digit code',
                  // An errorText rather than the validator, for the reason the email screen
                  // gives: a validator's closure is captured at build time, so a message that
                  // arrives from a response would not be shown until something re-validated.
                  errorText: codeMessage,
                ),
                validator: Validators.verificationCode,
                onChanged: (_) => ref.read(signupProvider.notifier).clearFailure(),
              ),
              const SizedBox(height: 24),
              FilledButton(
                key: const Key('verify-phone-submit'),
                onPressed: signup.busy || signup.phone == null
                    ? null
                    : () => unawaited(_verify()),
                child: signup.busy
                    ? const SizedBox(
                        height: 20,
                        width: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Confirm mobile'),
              ),
              const SizedBox(height: 8),
              ResendButton(
                label: 'Send another code',
                onResend: _request,
                // The screen asked for a code as it opened, so the resend starts inside a
                // cooldown this button did not begin.
                initialWait: _wait,
                enabled: signup.phone != null,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The verified state, and the end of the verification the product actually gates on.
class _Verified extends StatelessWidget {
  const _Verified({required this.phone});

  final String? phone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: const Text('Confirm your mobile')),
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              key: const Key('verify-phone-verified'),
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(Icons.check_circle, size: 48, color: theme.colorScheme.primary),
                const SizedBox(height: 16),
                Text('Mobile confirmed', style: theme.textTheme.headlineSmall),
                const SizedBox(height: 8),
                Text(phone ?? '', textAlign: TextAlign.center, style: theme.textTheme.bodyMedium),
                const SizedBox(height: 32),
                FilledButton(
                  key: const Key('verify-phone-continue'),
                  onPressed: () => context.go(Routes.registered),
                  child: const Text('Continue'),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
