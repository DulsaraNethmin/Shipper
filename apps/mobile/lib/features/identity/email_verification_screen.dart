import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/identity/resend_button.dart';
import 'package:shipper/features/identity/signup_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/validation/validators.dart';

/// Confirm the email address (SHIP-53).
///
/// `POST /v1/auth/verify-email`, built at SHIP-33. `Docs/04` §2 requires both contact channels
/// verified before a customer may publish, so this is a step in signup rather than a setting.
///
/// ## Two ways in, and they are the same screen
///
/// The *Done when* line is "user can enter or deep-link a code and see verified state", which is
/// two entry points into one verification:
///
/// - **Typed.** The message carries a value, and somebody reads it across from their email.
/// - **Deep-linked.** The message carries a link, the device opens the app at
///   `/verify-email?token=…`, and the screen verifies it without being asked. That is what a
///   person clicking a link expects, and a screen that pre-filled a field and waited for a
///   second tap would be asking them to confirm something they already confirmed.
///
/// **The scheme is `shipper:///verify-email?token=…`, and choosing it was this ticket's to
/// make** — `internal/identity/verification.go` says so in as many words, and sends a bare value
/// meanwhile. Registering a custom scheme needs no domain and no Apple or Google account, both
/// of which are blocked on X-2 and X-3. The production form is an HTTPS universal and app link
/// on the registered domain, which needs the entitlement work in SHIP-24…27; **this screen does
/// not change when that happens**, because a link of either kind resolves to the same route with
/// the same query parameter.
///
/// ## What it does not decide
///
/// Whether a token is usable is entirely the platform's: `identity_verification_token_expired`
/// when it was genuine and its day is up, `identity_verification_token_invalid` for every other
/// way — unknown, already used, superseded by a resend. Those are deliberately not
/// distinguished server-side, and this screen does not try to guess between them.
class EmailVerificationScreen extends ConsumerStatefulWidget {
  const EmailVerificationScreen({super.key, this.deepLinkedToken});

  /// The `token` query parameter, when the app was opened by a link rather than by a person.
  final String? deepLinkedToken;

  @override
  ConsumerState<EmailVerificationScreen> createState() => _EmailVerificationScreenState();
}

class _EmailVerificationScreenState extends ConsumerState<EmailVerificationScreen> {
  final _form = GlobalKey<FormState>();
  final _token = TextEditingController();

  @override
  void initState() {
    super.initState();

    final token = widget.deepLinkedToken?.trim();
    if (token == null || token.isEmpty) return;

    _token.text = token;
    // After the first frame, so the screen is on screen while the request is in flight rather
    // than appearing already-answered.
    WidgetsBinding.instance.addPostFrameCallback((_) => unawaited(_verify()));
  }

  @override
  void dispose() {
    _token.dispose();
    super.dispose();
  }

  Future<void> _verify() async {
    if (!(_form.currentState?.validate() ?? false)) return;

    final account = await ref.read(signupProvider.notifier).verifyEmail(_token.text.trim());
    if (!mounted || account == null) return;

    // Nothing is navigated automatically. The verified state is half of what this ticket has to
    // show, and replacing it with the next screen before it has been read would make the
    // criterion true only for a frame.
    setState(() {});
  }

  Future<Duration?> _resend() => ref.read(signupProvider.notifier).resendVerification();

  /// The platform's refusal, when it was about the token rather than about the request.
  static String? _tokenMessage(ApiFailure? failure) {
    return switch (failure) {
      ApiErrorResponse(
        code: 'identity_verification_token_invalid' || 'identity_verification_token_expired',
        :final message,
      ) =>
        message,
      _ => null,
    };
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final signup = ref.watch(signupProvider);
    final failure = signup.failure;
    final tokenMessage = _tokenMessage(failure);

    if (signup.emailVerified) {
      return _Verified(email: signup.email);
    }

    return Scaffold(
      appBar: AppBar(title: const Text('Confirm your email')),
      body: SafeArea(
        child: Form(
          key: _form,
          child: ListView(
            padding: const EdgeInsets.all(24),
            children: [
              Text(
                signup.email == null
                    // The deep-link case on a phone that has restarted since registering: the
                    // app holds a token and does not know whose it is.
                    ? 'Enter the code from the message we sent you, or open the link in it on '
                        'this device.'
                    : 'We sent a message to ${signup.email}. Enter the code from it, or open '
                        'the link in it on this device.',
                key: const Key('verify-email-instructions'),
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 24),
              if (failure != null && tokenMessage == null) ...[
                FailureBanner(failure),
                const SizedBox(height: 16),
              ],
              TextFormField(
                key: const Key('verify-email-token'),
                controller: _token,
                autocorrect: false,
                decoration: InputDecoration(
                  labelText: 'Code from the message',
                  // The platform's refusal, as an errorText rather than through the validator.
                  // A validator's closure is captured when the form is built, so a message that
                  // arrives from a response would not be shown until something else caused a
                  // re-validation — which, on a screen whose only field is already filled in, is
                  // never.
                  errorText: tokenMessage,
                ),
                validator: Validators.verificationToken,
                // Typing clears it: a message about the previous value is worse than none.
                onChanged: (_) => ref.read(signupProvider.notifier).clearFailure(),
              ),
              const SizedBox(height: 24),
              FilledButton(
                key: const Key('verify-email-submit'),
                onPressed: signup.busy ? null : () => unawaited(_verify()),
                child: signup.busy
                    ? const SizedBox(
                        height: 20,
                        width: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Confirm email'),
              ),
              const SizedBox(height: 8),
              ResendButton(
                label: 'Send another message',
                onResend: _resend,
                // Nothing to resend to. The address is the request body, and the app has one
                // only once it has an account.
                enabled: signup.email != null,
              ),
              if (signup.email == null)
                Text(
                  'Ask for another message from the device you registered on.',
                  style: theme.textTheme.bodySmall,
                ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The verified state, which is the second half of this ticket's *Done when*.
class _Verified extends StatelessWidget {
  const _Verified({required this.email});

  final String? email;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: const Text('Confirm your email')),
      body: SafeArea(
        child: Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Column(
              key: const Key('verify-email-verified'),
              mainAxisAlignment: MainAxisAlignment.center,
              children: [
                Icon(Icons.check_circle, size: 48, color: theme.colorScheme.primary),
                const SizedBox(height: 16),
                Text('Email confirmed', style: theme.textTheme.headlineSmall),
                const SizedBox(height: 8),
                Text(
                  email ?? '',
                  textAlign: TextAlign.center,
                  style: theme.textTheme.bodyMedium,
                ),
                const SizedBox(height: 32),
                FilledButton(
                  key: const Key('verify-email-continue'),
                  // SHIP-54 puts phone verification here. Until then the journey ends at the
                  // screen that says what was created and what is not yet possible.
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
