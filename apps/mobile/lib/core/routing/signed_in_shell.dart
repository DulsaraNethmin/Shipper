import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/routing/app_router.dart';

/// Where a signed-in cold start lands (SHIP-49).
///
/// A placeholder for the role-aware shell. `Docs/07` §1 requires the customer and provider
/// halves to be genuinely separate inside the one app, and the role that selects between them
/// is chosen at signup and returned by the platform — so the split arrives with SHIP-52, not
/// here. What this screen is for now is demonstrating that the routing decision was made and
/// made correctly, which is SHIP-49's *Done when* and nothing more.
///
/// It lives in `core/routing` rather than in a feature because it is the shell every feature
/// is shown inside. Putting it in one feature would make every other feature import that one,
/// which is what `Docs/07` §2 forbids.
class SignedInShell extends ConsumerWidget {
  const SignedInShell({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(title: const Text('Shipper')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            children: [
              Text(
                'Signed in',
                key: const Key('shell-signed-in'),
                style: theme.textTheme.headlineSmall,
              ),
              const SizedBox(height: 8),
              Text(
                'This device holds a refresh token. The customer and provider shells that '
                'replace this screen arrive with the role, at SHIP-52.',
                textAlign: TextAlign.center,
                style: theme.textTheme.bodySmall,
              ),
              const SizedBox(height: 32),
              // Sign-out is a real product action and not a development affordance:
              // Docs/07 §3 requires it to clear the stored token, and this is the one place
              // that currently can. The server-side revocation is SHIP-46.
              FilledButton(
                key: const Key('sign-out'),
                onPressed: () =>
                    unawaited(ref.read(sessionProvider.notifier).signOut()),
                child: const Text('Sign out'),
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
