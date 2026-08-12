import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/identity/sign_in_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/validation/validators.dart';

/// Sign in (SHIP-55), and the screen a signed-out cold start lands on (SHIP-49).
///
/// **The signed-out shell and the sign-in screen are one screen, not two.** SHIP-49 built the
/// first as a placeholder holding a disabled button, because `POST /v1/auth/login` did not exist.
/// It does now (SHIP-41), and a landing screen whose only purpose is a button leading to a form
/// is a tap somebody makes every time they are signed out. The route is unchanged, so every
/// redirect, every guard entry and every deep link still resolve to `/sign-in`.
///
/// ## What it does not do
///
/// It does not navigate on success. The session changing is what moves the app, through the
/// router's guard — and the role that decides *which* shell comes from the access token the
/// platform just signed, not from anything this screen knows.
///
/// ## The password rule here is not the password rule at registration
///
/// `Validators.password` enforces the ten-character minimum a person must *choose*. Applying it
/// to a password being *presented* would refuse an account whose password predates the current
/// floor — the contract is explicit that sign-in checks for presence only — and would do it
/// locally, so the person could not even reach the platform that would have accepted them.
class SignInScreen extends ConsumerStatefulWidget {
  const SignInScreen({super.key, this.prefilledEmail});

  /// The address to start with, when the app already knows it.
  ///
  /// Set by the end of the signup journey, which arrives here with the account it just created.
  /// Passed through the route rather than read from the signup state so that this screen depends
  /// on nothing that journey owns — somebody signing in on a fresh install has no journey.
  final String? prefilledEmail;

  @override
  ConsumerState<SignInScreen> createState() => _SignInScreenState();
}

class _SignInScreenState extends ConsumerState<SignInScreen> {
  final _form = GlobalKey<FormState>();
  late final TextEditingController _email = TextEditingController(text: widget.prefilledEmail);
  final _password = TextEditingController();

  /// What the platform said about each field, keyed by the path it named.
  ///
  /// Cleared per field as that field is edited: a server message that outlives the value it was
  /// about is worse than none.
  final _serverErrors = <String, String>{};

  bool _passwordVisible = false;

  @override
  void dispose() {
    _email.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(_serverErrors.clear);
    if (!(_form.currentState?.validate() ?? false)) return;

    final signedIn = await ref.read(signInProvider.notifier).signIn(
          email: _email.text.trim(),
          password: _password.text,
        );

    if (!mounted || signedIn) return;

    setState(() {
      _serverErrors.addAll(_fieldMessagesFrom(ref.read(signInProvider).failure));
    });
    _form.currentState?.validate();
  }

  /// Which of the platform's refusals belong under an input rather than in the banner.
  ///
  /// `identity_credentials_invalid` is deliberately **not** one of them, and that is the whole
  /// point of the code: it is one answer for a wrong password and for an address with no account,
  /// so that an unauthenticated endpoint is not an account-existence oracle. Putting it under the
  /// password field would be this client undoing that — it would tell somebody the address was
  /// recognised. It goes in the banner, about the pair.
  static Map<String, String> _fieldMessagesFrom(ApiFailure? failure) {
    return switch (failure) {
      ApiErrorResponse(code: 'identity_credentials_invalid') => const <String, String>{},
      ApiErrorResponse(:final fieldMessages) => fieldMessages,
      _ => const <String, String>{},
    };
  }

  String? _validate(String field, String? value, String? Function(String?) local) {
    return _serverErrors[field] ?? local(value);
  }

  void _clearServerError(String field) {
    if (_serverErrors.remove(field) != null) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final signIn = ref.watch(signInProvider);
    final failure = signIn.failure;
    final showBanner = failure != null && _fieldMessagesFrom(failure).isEmpty;

    return Scaffold(
      body: SafeArea(
        child: Form(
          key: _form,
          autovalidateMode: AutovalidateMode.onUserInteraction,
          child: ListView(
            padding: const EdgeInsets.all(24),
            children: [
              const SizedBox(height: 24),
              Text(
                'Shipper',
                // The key SHIP-49 gave the signed-out shell stays where it is. "The app is
                // showing the signed-out surface" is still the fact the routing tests assert,
                // and it is still this screen.
                key: const Key('shell-signed-out'),
                textAlign: TextAlign.center,
                style: theme.textTheme.headlineMedium,
              ),
              const SizedBox(height: 8),
              Text(
                'Sign in to publish a delivery or bid on one.',
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 24),
              if (showBanner) ...[
                FailureBanner(failure),
                const SizedBox(height: 16),
              ],
              TextFormField(
                key: const Key('sign-in-email'),
                controller: _email,
                autofillHints: const [AutofillHints.username, AutofillHints.email],
                keyboardType: TextInputType.emailAddress,
                textInputAction: TextInputAction.next,
                autocorrect: false,
                decoration: const InputDecoration(labelText: 'Email address'),
                onChanged: (_) => _clearServerError('email'),
                validator: (value) => _validate('email', value, Validators.email),
              ),
              const SizedBox(height: 16),
              TextFormField(
                key: const Key('sign-in-password'),
                controller: _password,
                autofillHints: const [AutofillHints.password],
                obscureText: !_passwordVisible,
                textInputAction: TextInputAction.done,
                onFieldSubmitted: (_) => unawaited(_submit()),
                decoration: InputDecoration(
                  labelText: 'Password',
                  suffixIcon: IconButton(
                    key: const Key('sign-in-password-visibility'),
                    icon: Icon(_passwordVisible ? Icons.visibility_off : Icons.visibility),
                    tooltip: _passwordVisible ? 'Hide password' : 'Show password',
                    onPressed: () => setState(() => _passwordVisible = !_passwordVisible),
                  ),
                ),
                onChanged: (_) => _clearServerError('password'),
                validator: (value) => _validate('password', value, Validators.presentedPassword),
              ),
              const SizedBox(height: 24),
              FilledButton(
                // The key SHIP-49's disabled placeholder carried. It is the same affordance,
                // finally connected to something.
                key: const Key('sign-in'),
                onPressed: signIn.busy ? null : () => unawaited(_submit()),
                child: signIn.busy
                    ? const SizedBox(
                        height: 20,
                        width: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Sign in'),
              ),
              const SizedBox(height: 8),
              OutlinedButton(
                key: const Key('create-account'),
                // Signup starts at the role, not at the form: the platform fixes the role at
                // registration and refuses to change it afterwards (SHIP-45), so it is the one
                // decision that deserves its own screen.
                onPressed: signIn.busy ? null : () => context.go(Routes.chooseRole),
                child: const Text('Create an account'),
              ),
              const SizedBox(height: 24),
              Text(
                // There is no password-reset endpoint — it is not in Docs/09 at all. Saying so
                // costs one sentence and saves somebody hunting for a link that is not there.
                'Forgotten your password? Resetting it from the app is not available yet.',
                key: const Key('sign-in-no-reset'),
                textAlign: TextAlign.center,
                style: theme.textTheme.bodySmall,
              ),
              const SizedBox(height: 8),
              TextButton(
                onPressed: () => context.go(Routes.health),
                child: const Text('Connectivity'),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
