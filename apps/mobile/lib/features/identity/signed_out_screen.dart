import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/development_session.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/routing/app_router.dart';

/// Where a signed-out cold start lands (SHIP-49).
///
/// The way into signup from SHIP-51, and still a placeholder for the sign-in screen, which is
/// SHIP-55 against `POST /v1/auth/login` — SHIP-41, an endpoint that does not exist yet.
/// `Docs/11` §7 forbids a screen depending on an endpoint from its own wave.
///
/// It sits in `features/identity` rather than in `core/routing` because that is where the real
/// screens go, and a placeholder in the wrong folder is a move-and-rename for whoever writes
/// them. The signed-*in* shell is core, for the opposite reason: every feature is shown inside
/// it, so a feature cannot own it.
class SignedOutScreen extends StatelessWidget {
  const SignedOutScreen({super.key});

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Text(
                'Shipper',
                key: const Key('shell-signed-out'),
                style: theme.textTheme.headlineMedium,
              ),
              const SizedBox(height: 8),
              Text(
                'Sign in or create an account to publish a delivery or bid on one.',
                textAlign: TextAlign.center,
                style: theme.textTheme.bodyMedium,
              ),
              const SizedBox(height: 24),
              // Sign-in stays disabled rather than absent, because Docs/07 §3 allows the app to
              // hide or disable and never to decide. There is no POST /v1/auth/login to call
              // until SHIP-41, and a button that navigates nowhere is worse than one that
              // visibly is not ready yet. Registration became real at SHIP-51.
              const FilledButton(
                key: Key('sign-in'),
                onPressed: null,
                child: Text('Sign in'),
              ),
              const SizedBox(height: 8),
              OutlinedButton(
                key: const Key('create-account'),
                // Signup starts at the role, not at the form: the platform fixes the role at
                // registration and refuses to change it afterwards (SHIP-45), so it is the one
                // decision that deserves its own screen.
                onPressed: () => context.go(Routes.chooseRole),
                child: const Text('Create an account'),
              ),
              const SizedBox(height: 24),
              const _DevelopmentSessions(),
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

/// The debug-only routes into each signed-in shell (SHIP-49, extended at SHIP-52).
///
/// The reasoning, the placeholder token and the [kDebugMode] guard all live in
/// `core/auth/development_session.dart`, because the end of the signup journey needs the same
/// affordance. What is here is only the choice of which sessions are worth reaching by hand, and
/// there are three because the shell has three answers: no role yet, which is what a restored
/// cold start actually looks like until SHIP-50 refreshes, and one for each half of the
/// marketplace.
class _DevelopmentSessions extends StatelessWidget {
  const _DevelopmentSessions();

  @override
  Widget build(BuildContext context) {
    if (!kDebugMode) return const SizedBox.shrink();

    return Column(
      children: [
        const DevelopmentSessionButton(label: 'Store a development session'),
        const DevelopmentSessionButton(
          label: 'Signed in as a customer',
          role: UserRole.customer,
        ),
        const DevelopmentSessionButton(
          label: 'Signed in as a provider',
          role: UserRole.provider,
        ),
        Text(
          'Debug builds only. Restart the app to see the cold-start route.',
          textAlign: TextAlign.center,
          style: Theme.of(context).textTheme.bodySmall,
        ),
      ],
    );
  }
}
