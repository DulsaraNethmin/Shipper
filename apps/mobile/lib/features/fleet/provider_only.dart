import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/routing/app_router.dart';

/// Draws [child] to a provider, and something else to everybody else (SHIP-98).
///
/// The fleet is the **first provider-only surface in this app**, so this is the first time the
/// question has had to be answered concretely. It is worth being precise about what this widget is
/// and what it is not, because the two are indistinguishable from a screenshot.
///
/// ## This hides a surface. It decides nothing.
///
/// `CLAUDE.md` and `Docs/07` §3 say the same thing twice: **no authorisation decision is made on the
/// device.** The app may hide or disable; the platform decides. So this widget's whole job is "which
/// screen is worth drawing for this account", and every request the screens behind it make still
/// goes to the platform and is still refused there if the platform disagrees —
/// `POST /v1/fleet/vehicles` answers `403 fleet_provider_only`, decided against `users.role` in the
/// database rather than against the role claim in the token this device happens to be holding.
///
/// A build with this widget deleted would show a customer the fleet screens and would change nothing
/// about what they could actually do with them. That is the property that makes it safe for the
/// client to hold an opinion here at all.
///
/// ## Why leaving the customer no button is not enough
///
/// The shell offers no fleet entry point to a customer, and that would be the whole answer if a
/// button were the only way in. It is not: `Docs/07` §5 makes every route deep-linkable and SHIP-145
/// will deliver notification payloads straight to one. The surface has to be able to answer for
/// itself when it is reached with no button involved.
///
/// ## And why it does not simply draw the fleet and let the platform refuse
///
/// Because it would not refuse. `GET /v1/fleet/vehicles` does **not** check the caller's role — it
/// answers a customer `200` with an empty page, because a customer owns no vehicles and there is
/// nothing for the endpoint to withhold. A customer who reached this screen would see an empty
/// fleet, an "Add a vehicle" button, a form to fill in, and a `403` only at the end of all of it.
/// Saying so at the start is the difference between an explanation and a dead end.
///
/// ## Four cases, and the third is the one that is easy to get wrong
///
/// | Role | What is drawn |
/// |---|---|
/// | provider | [child] |
/// | customer | the note below, and a way back |
/// | not yet known | a hold, **not** a refusal |
/// | unrecognised | the update prompt SHIP-167 exists for |
///
/// A restored cold start knows it is signed in and does not yet know as whom: the keychain holds a
/// refresh token, and the role is a claim in the access token, which arrives one refresh later
/// (SHIP-50). Treating that window as "not a provider" would bounce a provider off their own fleet
/// every time they opened the app from a notification — the shape of bug that gets reported as "it
/// works the second time".
class ProviderOnly extends ConsumerWidget {
  const ProviderOnly({required this.child, super.key});

  /// The provider surface. Constructed by the caller and only **mounted** for a provider, so nothing
  /// behind this widget reads an endpoint on behalf of an account that has no business there.
  final Widget child;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final session = ref.watch(sessionProvider);
    final role = switch (session) {
      SessionSignedIn(:final role) => role,
      _ => null,
    };

    return switch (role) {
      UserRole.provider => child,
      UserRole.customer => const _NotYourHalf(
          noticeKey: Key('provider-only'),
          title: 'This is the provider side of Shipper',
          // Says why rather than only that. The role is fixed at registration and a database trigger
          // enforces it (SHIP-45), so "switch your account over" is advice that cannot be followed
          // and should not be implied.
          body: 'Your account publishes deliveries. Vehicles belong to the transport providers who '
              'carry them, and the kind of account is fixed when it is created.',
          back: 'Back to your deliveries',
        ),
      UserRole.unknown => const _NotYourHalf(
          noticeKey: Key('provider-only-role-unrecognised'),
          title: 'This version cannot show your account',
          body: 'Your account type is not one this version of Shipper knows about. Update the app '
              'to continue.',
          back: 'Back',
        ),
      // Signed in, and the role has not arrived yet. A hold rather than an answer.
      null => const Center(
          child: Padding(
            padding: EdgeInsets.symmetric(vertical: 48),
            child: CircularProgressIndicator(key: Key('provider-only-waiting')),
          ),
        ),
    };
  }
}

/// A surface this account has no use for, said in words rather than shown as an error.
class _NotYourHalf extends StatelessWidget {
  const _NotYourHalf({
    required this.noticeKey,
    required this.title,
    required this.body,
    required this.back,
  });

  final Key noticeKey;
  final String title;
  final String body;
  final String back;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Center(
      child: Padding(
        padding: const EdgeInsets.all(24),
        child: Column(
          key: noticeKey,
          mainAxisAlignment: MainAxisAlignment.center,
          children: [
            Icon(Icons.local_shipping_outlined, size: 48, color: theme.colorScheme.outline),
            const SizedBox(height: 16),
            Text(title, style: theme.textTheme.titleMedium, textAlign: TextAlign.center),
            const SizedBox(height: 8),
            Text(body, style: theme.textTheme.bodyMedium, textAlign: TextAlign.center),
            const SizedBox(height: 24),
            OutlinedButton(
              key: const Key('provider-only-home'),
              // `go` rather than `pop`, because this may have been reached by a deep link with
              // nothing underneath it to pop back to.
              onPressed: () => context.go(Routes.home),
              child: Text(back),
            ),
          ],
        ),
      ),
    );
  }
}
