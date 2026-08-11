import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/identity/signup_controller.dart';
import 'package:shipper/shared/design_system/failure_banner.dart';
import 'package:shipper/shared/validation/validators.dart';

/// Create an account (SHIP-51).
///
/// The first screen in the client that calls a product endpoint: `POST /v1/auth/register`,
/// built at SHIP-30 in the previous wave.
///
/// ## Inline validation is two sources and one presentation
///
/// The ticket's *Done when* is "a new account can be created from the app with inline
/// validation", and the interesting half is *whose* validation. `Docs/07` §2 and `CLAUDE.md`
/// both put every rule on the platform: the app may pre-validate, and the platform decides.
///
/// So there are two sources and they render identically, under the input each concerns:
///
/// - [Validators] catches what cannot be anything but a mistake — a blank field, an address with
///   no `@` — without a round trip. On mobile data, in a yard, that is the whole point.
/// - The platform answers `validation_failed` with one `details` entry per offending field, in
///   the dotted paths this form serialised, and those messages land under the same inputs. A
///   limit that moves server-side therefore shows up correctly on a build already installed,
///   which is what `Docs/07` §1 requires of anything that changes under operational pressure.
///
/// `identity_email_taken` and `identity_phone_taken` are mapped to their fields the same way,
/// which is a **branch on `code`** — never on `message`, because the message is copy and gets
/// reworded server-side without a release (SHIP-12).
///
/// ## Registration does not sign anybody in
///
/// The platform returns an account and no token, deliberately: `POST /v1/auth/login` (SHIP-41)
/// issues the first access token at the endpoint built to take a password. The journey therefore
/// continues, still signed out, to email verification.
class RegistrationScreen extends ConsumerStatefulWidget {
  const RegistrationScreen({super.key});

  @override
  ConsumerState<RegistrationScreen> createState() => _RegistrationScreenState();
}

class _RegistrationScreenState extends ConsumerState<RegistrationScreen> {
  final _form = GlobalKey<FormState>();
  final _email = TextEditingController();
  final _phone = TextEditingController();
  final _password = TextEditingController();

  /// What the platform said about each field, keyed by the path it named.
  ///
  /// Held here rather than read from the failure at build time because it has to be **cleared
  /// per field as that field is edited**. A server message that outlives the value it was about
  /// is worse than none: the user corrects the address and the form still says it is taken.
  final _serverErrors = <String, String>{};

  bool _passwordVisible = false;

  @override
  void dispose() {
    _email.dispose();
    _phone.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(_serverErrors.clear);
    if (!(_form.currentState?.validate() ?? false)) return;

    final account = await ref.read(signupProvider.notifier).register(
          email: _email.text.trim(),
          phone: _phone.text.trim(),
          password: _password.text,
        );

    if (!mounted) return;

    if (account != null) {
      // Registration issues and sends the verification token itself (SHIP-31), inside the same
      // transaction that created the account — so the next screen has something to wait for the
      // moment it opens.
      context.go(Routes.verifyEmail);
      return;
    }

    setState(() {
      _serverErrors.addAll(_fieldMessagesFrom(ref.read(signupProvider).failure));
    });
    _form.currentState?.validate();
  }

  /// Which of the platform's refusals belong under an input rather than in the banner.
  ///
  /// Both duplicate codes name a field even though the platform sends no `details` for them: a
  /// 409 saying the address is taken is about the address, and putting that in a banner while
  /// the input sits there looking accepted is how somebody retries the same value three times.
  static Map<String, String> _fieldMessagesFrom(ApiFailure? failure) {
    return switch (failure) {
      ApiErrorResponse(code: 'identity_email_taken', :final message) => {'email': message},
      ApiErrorResponse(code: 'identity_phone_taken', :final message) => {'phone': message},
      ApiErrorResponse(:final fieldMessages) => fieldMessages,
      _ => const <String, String>{},
    };
  }

  /// A field's message: the platform's if it has one, otherwise this device's own check.
  String? _validate(String field, String? value, String? Function(String?) local) {
    return _serverErrors[field] ?? local(value);
  }

  void _clearServerError(String field) {
    if (_serverErrors.remove(field) != null) setState(() {});
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final signup = ref.watch(signupProvider);
    final failure = signup.failure;
    final showBanner = failure != null && _fieldMessagesFrom(failure).isEmpty;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Create an account'),
        leading: BackButton(onPressed: () => context.go(Routes.chooseRole)),
      ),
      body: SafeArea(
        child: Form(
          key: _form,
          autovalidateMode: AutovalidateMode.onUserInteraction,
          child: ListView(
            padding: const EdgeInsets.all(24),
            children: [
              _RoleSummary(role: signup.role),
              const SizedBox(height: 24),
              if (showBanner) ...[
                FailureBanner(failure),
                const SizedBox(height: 16),
              ],
              TextFormField(
                key: const Key('register-email'),
                controller: _email,
                autofillHints: const [AutofillHints.email],
                keyboardType: TextInputType.emailAddress,
                textInputAction: TextInputAction.next,
                autocorrect: false,
                decoration: const InputDecoration(
                  labelText: 'Email address',
                  helperText: 'We send a verification link here.',
                ),
                onChanged: (_) => _clearServerError('email'),
                validator: (value) => _validate('email', value, Validators.email),
              ),
              const SizedBox(height: 16),
              TextFormField(
                key: const Key('register-phone'),
                controller: _phone,
                autofillHints: const [AutofillHints.telephoneNumber],
                keyboardType: TextInputType.phone,
                textInputAction: TextInputAction.next,
                decoration: const InputDecoration(
                  labelText: 'Mobile number',
                  helperText: 'We send a six-digit code here, like 0412 345 678.',
                ),
                onChanged: (_) => _clearServerError('phone'),
                validator: (value) => _validate('phone', value, Validators.australianMobile),
              ),
              const SizedBox(height: 16),
              TextFormField(
                key: const Key('register-password'),
                controller: _password,
                autofillHints: const [AutofillHints.newPassword],
                obscureText: !_passwordVisible,
                decoration: InputDecoration(
                  labelText: 'Password',
                  helperText: 'At least ${Validators.passwordMinimumLength} characters. '
                      'Length is the only rule.',
                  suffixIcon: IconButton(
                    key: const Key('register-password-visibility'),
                    icon: Icon(_passwordVisible ? Icons.visibility_off : Icons.visibility),
                    tooltip: _passwordVisible ? 'Hide password' : 'Show password',
                    onPressed: () => setState(() => _passwordVisible = !_passwordVisible),
                  ),
                ),
                onChanged: (_) => _clearServerError('password'),
                validator: (value) => _validate('password', value, Validators.password),
              ),
              const SizedBox(height: 24),
              FilledButton(
                key: const Key('register-submit'),
                // Disabled while a request is in flight, which is what stops a second tap
                // becoming a second action. An idempotency key does not cover that case: two
                // taps are two actions, and the key is deliberately per-action (Docs/07 §4).
                onPressed: signup.busy ? null : () => unawaited(_submit()),
                child: signup.busy
                    ? const SizedBox(
                        height: 20,
                        width: 20,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Text('Create account'),
              ),
              const SizedBox(height: 16),
              Text(
                // Docs/03 §1 asks for a privacy explanation at registration, and Docs/04 §2
                // requires both channels verified before a customer may publish — so this says
                // what the two contact details are for rather than only that they are needed.
                // The policy URL itself is X-7 and does not exist yet; a link to nowhere would
                // be worse than the sentence.
                'Shipper uses your email address and mobile number to verify your account and '
                'to contact you about a delivery. Both must be verified before you can publish '
                'a job or bid on one.',
                key: const Key('register-privacy'),
                style: theme.textTheme.bodySmall,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// The role chosen on the previous screen, shown rather than decided invisibly (SHIP-52).
///
/// **This is the last screen on which changing it costs nothing.** The platform fixes the role
/// at registration and a `BEFORE UPDATE` trigger on `users` refuses to change it afterwards
/// (SHIP-45), so a person who meant to sign up as a provider needs a second account. Saying so
/// here is cheaper than the support conversation.
class _RoleSummary extends StatelessWidget {
  const _RoleSummary({required this.role});

  final UserRole role;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Card(
      key: const Key('register-role'),
      margin: EdgeInsets.zero,
      child: ListTile(
        title: Text(role.label, style: theme.textTheme.titleMedium),
        subtitle: Text(
          'Fixed once the account exists. Change it now if it is wrong.',
          style: theme.textTheme.bodySmall,
        ),
        trailing: TextButton(
          key: const Key('register-change-role'),
          onPressed: () => context.go(Routes.chooseRole),
          child: const Text('Change'),
        ),
      ),
    );
  }
}
