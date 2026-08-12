import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/jobs/customer_job_list.dart';

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
/// | customer | The customer's own jobs, grouped by status (SHIP-76) |
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
      appBar: AppBar(
        title: const Text('Shipper'),
        actions: [
          IconButton(
            icon: const Icon(Icons.wifi_tethering),
            tooltip: 'Connectivity',
            onPressed: () => context.go(Routes.health),
          ),
          // Sign-out is a real product action and not a development affordance: Docs/07 §3
          // requires it to clear the stored token, and this is the one place that currently can.
          // The server-side revocation is SHIP-46. It keeps the key it has had since SHIP-49 —
          // the affordance moved into the bar when the customer half became a full screen, and
          // the fact the tests assert did not change.
          IconButton(
            key: const Key('sign-out'),
            icon: const Icon(Icons.logout),
            tooltip: 'Sign out',
            onPressed: () => unawaited(ref.read(sessionProvider.notifier).signOut()),
          ),
        ],
      ),
      // Only the customer publishes. Docs/07 §1 keeps the halves genuinely separate, and an
      // action button a provider could see would be the first crack in that — even one that
      // failed server-side, because the platform refusing is not the same as the app not
      // offering.
      floatingActionButton: role == UserRole.customer
          ? FloatingActionButton.extended(
              key: const Key('new-job'),
              onPressed: () => context.go(Routes.newJob),
              icon: const Icon(Icons.add),
              label: const Text('Publish a delivery'),
            )
          : null,
      // The key SHIP-49 gave this screen stays where it is: "the app reached a signed-in shell"
      // is still a fact worth asserting on its own, separately from which one.
      body: KeyedSubtree(
        key: const Key('shell-signed-in'),
        child: switch (role) {
          UserRole.customer => const CustomerJobList(),
          UserRole.provider => const _ProviderHalf(),
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
      ),
    );
  }
}

/// The provider half of the shell (SHIP-98).
///
/// It is still mostly a placeholder, and deliberately so: the job feed is SHIP-99, bidding is
/// SHIP-100, and the delivery screens are M4. What it stopped being at SHIP-98 is *entirely* a
/// placeholder — the fleet is a real provider surface, and this is the only way into it.
///
/// **The entry point is drawn for a provider and for nobody else**, which is `Docs/07` §1's
/// requirement that the two halves stay genuinely separate. That is a presentation decision and not
/// a control: `ProviderOnly` explains why the fleet screens answer for themselves as well, because
/// a route is reachable by a deep link with no button involved.
class _ProviderHalf extends StatelessWidget {
  const _ProviderHalf();

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          key: const Key('shell-provider'),
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text(
              'Work you can bid on',
              style: theme.textTheme.headlineSmall,
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 8),
            Text(
              'Finding jobs, bidding and recording milestones arrive with the bidding and '
              'delivery screens in M3 and M4.',
              style: theme.textTheme.bodySmall,
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 32),
            Text(
              // Says why the fleet matters here rather than treating it as a settings page. What a
              // provider can carry is what decides the work they can bid on, and the platform
              // checks it when the bid is placed.
              'What you can carry decides which jobs you can bid on.',
              style: theme.textTheme.bodyMedium,
              textAlign: TextAlign.center,
            ),
            const SizedBox(height: 12),
            FilledButton.icon(
              key: const Key('manage-vehicles'),
              // `push` rather than `go`, so the back gesture returns to the shell rather than
              // rebuilding it.
              onPressed: () => context.push(Routes.fleet),
              icon: const Icon(Icons.local_shipping_outlined),
              label: const Text('Your vehicles'),
            ),
          ],
        ),
      ),
    );
  }
}

/// A half of the marketplace that has no screen yet.
///
/// The customer half stopped being one of these at SHIP-76 and is now `CustomerJobList`, and the
/// provider half at SHIP-98. What is left is the two role cases that are not a half of anything: an
/// account type this build does not know, and a restored session whose role has not arrived yet.
class _Half extends StatelessWidget {
  const _Half({required this.shellKey, required this.title, required this.body});

  final Key shellKey;
  final String title;
  final String body;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          key: shellKey,
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Text(title, style: theme.textTheme.headlineSmall, textAlign: TextAlign.center),
            const SizedBox(height: 8),
            Text(body, textAlign: TextAlign.center, style: theme.textTheme.bodySmall),
          ],
        ),
      ),
    );
  }
}
