import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/routing/app_router.dart';
import 'package:shipper/features/identity/signup_controller.dart';

/// The first step of signup: which half of the marketplace this account is (SHIP-52).
///
/// `Docs/07` §1 puts customers and providers in one app with the role chosen at signup, and
/// requires the two halves to stay genuinely separate inside it. This screen is where that
/// choice is made, and it is the *first* step rather than a field on the registration form for
/// one reason: **the platform fixes the role at registration and refuses to change it
/// afterwards** (SHIP-45, enforced by a `BEFORE UPDATE` trigger on `users`). A person who picks
/// wrongly needs a new account, so the choice deserves a screen that says what each one means
/// rather than a segmented control above a password field.
///
/// ## What this screen does not do
///
/// It does not grant anything. `Docs/07` §3 and `CLAUDE.md` both keep every authorisation
/// decision on the platform: what is sent is a preference in a registration request, the
/// platform records it, and every later request is judged against what the platform recorded and
/// never against what this device believes. Choosing "provider" here does not make an account
/// eligible to bid either — that is a separate verification decision with five states of its own
/// (`Docs/04` §3, §4).
class RoleSelectionScreen extends ConsumerWidget {
  const RoleSelectionScreen({super.key});

  /// What each role means, in the words the person choosing would use.
  ///
  /// Copy rather than data: it is here, on the screen that shows it, and not on [UserRole],
  /// because the enum is read by the router and the shell and neither has any use for a
  /// paragraph. `Docs/07` §7 treats user-facing copy as build work.
  static const _description = <UserRole, String>{
    UserRole.customer: 'I have goods to move. I describe the delivery, transport providers bid '
        'privately, and I choose one.',
    UserRole.provider: 'I move goods for others. I see delivery work I am eligible for and bid '
        'on it.',
  };

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final chosen = ref.watch(signupProvider).role;

    return Scaffold(
      appBar: AppBar(
        title: const Text('How will you use Shipper?'),
        leading: BackButton(onPressed: () => context.go(Routes.signIn)),
      ),
      body: SafeArea(
        child: ListView(
          padding: const EdgeInsets.all(24),
          children: [
            Text(
              'Choose one. This is fixed once the account is created, so a change later means a '
              'new account.',
              key: const Key('role-warning'),
              style: theme.textTheme.bodyMedium,
            ),
            const SizedBox(height: 24),
            for (final role in UserRole.selectable)
              _RoleOption(
                key: Key('role-${role.name}'),
                role: role,
                description: _description[role] ?? '',
                selected: role == chosen,
                // The chosen role is held in the signup state rather than in this widget, so the
                // registration screen can show it back and the register call can send it. Local
                // state would be lost the moment somebody stepped back a screen.
                onSelected: () => ref.read(signupProvider.notifier).chooseRole(role),
              ),
            const SizedBox(height: 12),
            FilledButton(
              key: const Key('role-continue'),
              onPressed: () => context.go(Routes.register),
              child: const Text('Continue'),
            ),
          ],
        ),
      ),
    );
  }
}

/// One choosable role, as a whole tappable card.
///
/// Written out rather than assembled from `RadioListTile`, whose `groupValue` and `onChanged`
/// are deprecated in this Flutter version — and the analyzer here fails on an info, deliberately
/// (`analysis_options.yaml`). The card is also the better target: `Docs/03` §5 asks for large
/// touch targets usable with one hand, and the description is the part people actually read.
class _RoleOption extends StatelessWidget {
  const _RoleOption({
    super.key,
    required this.role,
    required this.description,
    required this.selected,
    required this.onSelected,
  });

  final UserRole role;
  final String description;
  final bool selected;
  final VoidCallback onSelected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final scheme = theme.colorScheme;

    return Card(
      margin: const EdgeInsets.only(bottom: 12),
      // A visible outline as well as the icon: colour alone is not a state indicator somebody
      // with a colour-vision deficiency can read.
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: BorderSide(
          color: selected ? scheme.primary : scheme.outlineVariant,
          width: selected ? 2 : 1,
        ),
      ),
      child: InkWell(
        onTap: onSelected,
        borderRadius: BorderRadius.circular(12),
        child: Semantics(
          selected: selected,
          button: true,
          child: Padding(
            padding: const EdgeInsets.all(16),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  selected ? Icons.radio_button_checked : Icons.radio_button_unchecked,
                  color: selected ? scheme.primary : scheme.outline,
                ),
                const SizedBox(width: 12),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(role.label, style: theme.textTheme.titleMedium),
                      const SizedBox(height: 4),
                      Text(description, style: theme.textTheme.bodyMedium),
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}
