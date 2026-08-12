import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/identity/signup_controller.dart';

/// Where the signup journey ends (SHIP-51), and hands over to sign-in (SHIP-55).
///
/// **The new account is not signed in automatically, and that is the platform's design rather
/// than a gap.** `POST /v1/auth/register` returns an account and no token — registering is not
/// signing in — so the only route to a session is `POST /v1/auth/login` with the password. This
/// screen could have kept that password in memory from the form and used it, and deliberately
/// does not: a plaintext password living in the provider tree is one crash report away from being
/// somewhere it must never be (`Docs/07` §3 draws that line for tokens, and a password is worse).
/// It carries the *address* forward instead, so the person types one field rather than two.
///
/// It reads the account rather than a local flag, so what it reports is the platform's answer to
/// the last verification call and not the client's belief about it.
class RegistrationCompleteScreen extends ConsumerWidget {
  const RegistrationCompleteScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final signup = ref.watch(signupProvider);
    final account = signup.account;

    return Scaffold(
      appBar: AppBar(title: const Text('Account created')),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            Text(
              'Your Shipper account is ready',
              key: const Key('registered-heading'),
              style: theme.textTheme.headlineSmall,
            ),
            const SizedBox(height: 8),
            Text(
              'You are set up as a ${signup.role.label.toLowerCase()}. That is fixed for this '
              'account — a different one would need a new signup.',
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 24),
            _Channel(
              key: const Key('registered-email'),
              label: 'Email address',
              value: account?.email ?? '—',
              verified: signup.emailVerified,
            ),
            _Channel(
              key: const Key('registered-phone'),
              label: 'Mobile number',
              value: account?.phone ?? '—',
              verified: signup.phoneVerified,
            ),
            const SizedBox(height: 24),
            Text(
              'Signing in is the last step. Your account is created and everything above is '
              'recorded on the platform.',
              key: const Key('registered-next'),
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 24),
            FilledButton(
              key: const Key('registered-done'),
              onPressed: () {
                // The address is read before the reset, and carried in the route rather than left
                // in state for the next screen to find. The sign-in screen belongs to no journey
                // and must work identically on a fresh install.
                final email = signup.email;

                // The journey is over, so its state goes with it. Leaving an account and a
                // handful of idempotency keys behind would mean a second signup on this device
                // started half-way through the first one.
                ref.read(signupProvider.notifier).reset();

                context.go(
                  email == null
                      ? Routes.signIn
                      : Uri(path: Routes.signIn, queryParameters: {'email': email}).toString(),
                );
              },
              child: const Text('Sign in'),
            ),
          ],
        ),
      ),
    );
  }
}

/// One contact channel and whether the platform considers it verified.
class _Channel extends StatelessWidget {
  const _Channel({
    super.key,
    required this.label,
    required this.value,
    required this.verified,
  });

  final String label;
  final String value;
  final bool verified;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return ListTile(
      contentPadding: EdgeInsets.zero,
      leading: Icon(
        verified ? Icons.check_circle : Icons.radio_button_unchecked,
        color: verified ? theme.colorScheme.primary : theme.colorScheme.outline,
      ),
      title: Text(label, style: theme.textTheme.labelLarge),
      subtitle: Text(value),
      trailing: Text(
        verified ? 'Verified' : 'Not verified',
        style: theme.textTheme.bodySmall,
      ),
    );
  }
}
