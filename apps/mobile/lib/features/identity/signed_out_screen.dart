import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/routing/app_router.dart';

/// Where a signed-out cold start lands (SHIP-49).
///
/// A placeholder for the registration and sign-in screens, which arrive at SHIP-51 and
/// SHIP-55 against endpoints built in an earlier wave — `Docs/11` §7 forbids a screen
/// depending on an endpoint from its own wave, and there is no `POST /v1/auth/login` yet.
///
/// It sits in `features/identity` rather than in `core/routing` because that is where the real
/// screens go, and a placeholder in the wrong folder is a move-and-rename for whoever writes
/// them. The signed-*in* shell is core, for the opposite reason: every feature is shown inside
/// it, so a feature cannot own it.
class SignedOutScreen extends ConsumerWidget {
  const SignedOutScreen({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
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
              // Disabled rather than absent, because Docs/07 §3 allows the app to hide or
              // disable and never to decide. What these will do is call the platform; until
              // SHIP-51 and SHIP-55 there is nothing to call, and a button that navigates
              // nowhere is worse than one that visibly is not ready yet.
              const FilledButton(onPressed: null, child: Text('Sign in')),
              const SizedBox(height: 8),
              const OutlinedButton(onPressed: null, child: Text('Create an account')),
              const SizedBox(height: 24),
              const _DevelopmentSession(),
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

/// Puts a token in the keychain so the cold-start routing can be demonstrated (SHIP-49).
///
/// **Debug builds only, and that is enforced rather than intended.** `kDebugMode` is a
/// compile-time constant, so in a profile or release build the tree-shaker removes this widget
/// and the string it carries entirely — there is no flag to misconfigure and nothing to strip
/// later.
///
/// It exists because SHIP-49's *Done when* is a routing criterion and this wave has no
/// endpoint that issues a refresh token. Demonstrating "routes to the signed-in shell on cold
/// start" needs a token in the keychain, and the two ways to get one are this or a fake sign-in
/// path in production code. The value written is not a credential and the platform will refuse
/// it the moment SHIP-50 tries to refresh with it, which is the correct outcome: the device
/// believing it has a session has never been the same thing as having one.
class _DevelopmentSession extends ConsumerWidget {
  const _DevelopmentSession();

  /// Recognisable in a keychain dump, and obviously not a real token.
  static const _placeholder = 'development-placeholder-not-a-credential';

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (!kDebugMode) return const SizedBox.shrink();

    return Column(
      children: [
        TextButton(
          key: const Key('development-session'),
          onPressed: () => unawaited(
            ref.read(sessionProvider.notifier).signIn(refreshToken: _placeholder),
          ),
          child: const Text('Store a development session'),
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
