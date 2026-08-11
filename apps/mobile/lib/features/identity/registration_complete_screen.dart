import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/identity/signup_controller.dart';

/// Where the signup journey ends (SHIP-51).
///
/// **This screen is a deliberate stub, and the stub is the honest part.** The journey's real
/// ending is the app signing the new account in and landing it in the shell its role selects —
/// and that needs `POST /v1/auth/login`, which is SHIP-41, consumed by SHIP-55. Neither exists,
/// and `Docs/11` §7 forbids a screen depending on an endpoint from its own wave. So rather than
/// invent a sign-in path that would have to be deleted, this says plainly what was created, what
/// is verified, and what is not yet possible.
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
              // Not a placeholder for a screen somebody forgot: there is no sign-in endpoint on
              // the platform yet. Saying so beats a button that fails, and beats a screen that
              // implies the account cannot be used.
              'Signing in from the app arrives in a later build. Everything above is recorded '
              'on the platform, and this account is the one you will sign in to.',
              key: const Key('registered-next'),
              style: theme.textTheme.bodySmall,
            ),
            const SizedBox(height: 24),
            FilledButton(
              key: const Key('registered-done'),
              onPressed: () {
                // The journey is over, so its state goes with it. Leaving an account and a
                // handful of idempotency keys behind would mean a second signup on this device
                // started half-way through the first one.
                ref.read(signupProvider.notifier).reset();
                context.go(Routes.signIn);
              },
              child: const Text('Back to start'),
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
