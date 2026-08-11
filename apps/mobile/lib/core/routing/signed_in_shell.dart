import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/routing/app_router.dart';

/// Where a signed-in cold start lands, and the first place the role matters (SHIP-49, SHIP-52).
///
/// `Docs/07` §1 requires the customer and provider halves to be genuinely separate inside the
/// one app: "a customer should never see provider surfaces or the reverse". This is where that
/// separation is made — the role chosen at signup and returned by the platform selects the shell
/// the app draws.
///
/// **It is presentation, not authorisation.** Reaching the wrong shell by any means — a stale
/// route, a modified build, a deep link — still fails server-side on the first request it makes,
/// because the platform decides what the account may do on every one of them (`Docs/07` §3,
/// `CLAUDE.md`). Nothing here is load-bearing for security and nothing should be made so later.
///
/// ## Four cases, and two of them are easy to forget
///
/// | Role | Shell |
/// |---|---|
/// | customer | The customer half |
/// | provider | The provider half |
/// | not yet known | What both halves share — a restored cold start, until SHIP-50 refreshes |
/// | unrecognised | An honest dead end, with the update prompt SHIP-167 exists for |
///
/// The third is the state a restored session is genuinely in: the keychain holds a refresh token
/// and no role, so the app knows it is signed in and not yet as whom. Guessing customer would
/// show provider accounts the wrong marketplace on every cold start until the first refresh.
///
/// It lives in `core/routing` rather than in a feature because it is the shell every feature is
/// shown inside. Putting it in one feature would make every other feature import that one, which
/// is what `Docs/07` §2 forbids.
class SignedInShell extends ConsumerWidget {
  const SignedInShell({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionProvider);
    final role = switch (session) {
      SessionSignedIn(:final role) => role,
      _ => null,
    };

    return Scaffold(
      appBar: AppBar(title: const Text('Shipper')),
      body: Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisAlignment: MainAxisAlignment.center,
            // The key SHIP-49 gave this screen stays where it is: "the app reached a signed-in
            // shell" is still a fact worth asserting on its own, separately from which one.
            key: const Key('shell-signed-in'),
            children: [
              switch (role) {
                UserRole.customer => const _Half(
                    shellKey: Key('shell-customer'),
                    title: 'Deliveries you have published',
                    body: 'Publishing a job, comparing bids and tracking a delivery arrive with '
                        'the job screens in M2.',
                  ),
                UserRole.provider => const _Half(
                    shellKey: Key('shell-provider'),
                    title: 'Work you can bid on',
                    body: 'Finding jobs, bidding and recording milestones arrive with the '
                        'bidding and delivery screens in M3 and M4.',
                  ),
                UserRole.unknown => const _Half(
                    shellKey: Key('shell-role-unrecognised'),
                    title: 'This version cannot show your account',
                    // Not "something went wrong". The account is fine and the build is old, and
                    // saying which is the difference between an update and a support call.
                    body: 'Your account type is not one this version of Shipper knows about. '
                        'Update the app to continue.',
                  ),
                null => const _Half(
                    shellKey: Key('shell-role-pending'),
                    title: 'Signed in',
                    body: 'This device holds a refresh token. Which half of the marketplace to '
                        'show comes from the platform with the next access token.',
                  ),
              },
              const SizedBox(height: 32),
              // Sign-out is a real product action and not a development affordance:
              // Docs/07 §3 requires it to clear the stored token, and this is the one place
              // that currently can. The server-side revocation is SHIP-46.
              FilledButton(
                key: const Key('sign-out'),
                onPressed: () => unawaited(ref.read(sessionProvider.notifier).signOut()),
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

/// One half of the marketplace, as much of it as exists.
///
/// Placeholders, and deliberately shallow ones: the screens that fill them are M2 to M4 and each
/// belongs to a feature. What matters at SHIP-52 is that the two are different screens selected
/// by the role, not what is written on them.
class _Half extends StatelessWidget {
  const _Half({required this.shellKey, required this.title, required this.body});

  final Key shellKey;
  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Column(
      key: shellKey,
      children: [
        Text(title, style: theme.textTheme.headlineSmall, textAlign: TextAlign.center),
        const SizedBox(height: 8),
        Text(body, textAlign: TextAlign.center, style: theme.textTheme.bodySmall),
      ],
    );
  }
}
