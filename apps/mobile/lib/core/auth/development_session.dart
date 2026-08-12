import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/token_pair.dart';
import 'package:shipper/core/auth/user_role.dart';

/// Puts a placeholder token in the keychain so the signed-in shells can be reached (SHIP-49,
/// extended for SHIP-52).
///
/// **Debug builds only, and that is enforced rather than intended.** [kDebugMode] is a
/// compile-time constant, so a profile or release build tree-shakes this widget and the string
/// it carries away entirely — there is no flag to misconfigure and nothing to strip later.
///
/// ## Why it exists at all
///
/// No endpoint in this wave issues a refresh token. `POST /v1/auth/register` returns an account
/// and deliberately no token, and `POST /v1/auth/login` is SHIP-41, consumed by SHIP-55. So
/// "the app routes to the signed-in shell on cold start" (SHIP-49) and "the role drives the
/// post-login shell" (SHIP-52) both need something in the keychain to read, and the two ways to
/// get one are this or a fake sign-in path in production code.
///
/// The value written is not a credential, and the platform will refuse it the moment SHIP-50
/// refreshes with it — which is the correct outcome. **A device believing it has a session has
/// never been the same thing as having one**, and every authorisation decision is the
/// platform's, on every request (`Docs/07` §3).
///
/// It moved out of `signed_out_screen.dart` at SHIP-52 because a second screen needs it: the end
/// of the signup journey, where the role the person just chose is the interesting thing to carry
/// into a shell. One copy means one place holding the reasoning and the placeholder string.
class DevelopmentSessionButton extends ConsumerWidget {
  const DevelopmentSessionButton({
    super.key,
    required this.label,
    this.role,
  });

  /// What the button says. The caller writes it, because "store a development session" and
  /// "preview the provider shell" are the same mechanism doing two different jobs.
  final String label;

  /// The role the session claims, or `null` for the state a restored cold start is in — signed
  /// in, with the role not yet known. Both are worth being able to reach by hand.
  final UserRole? role;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (!kDebugMode) return const SizedBox.shrink();

    return TextButton(
      // The no-role button keeps the key SHIP-49 gave it: it is the same affordance, storing
      // the same placeholder, and renaming it would break the cold-start test for no reason.
      key: role == null ? const Key('development-session') : Key('development-session-${role!.name}'),
      onPressed: () => unawaited(
        ref.read(sessionProvider.notifier).signIn(developmentPlaceholderPair(role)),
      ),
      child: Text(label),
    );
  }
}

/// Recognisable in a keychain dump, and obviously not a real token.
const developmentPlaceholderToken = 'development-placeholder-not-a-credential';

/// A pair shaped like the platform's, carrying [role] where the platform would sign it.
///
/// The session reads the role from the access token's claim rather than from an argument
/// (SHIP-50), so this has to produce something with a claim in it. The result is an **unsigned**
/// JWT shape: three segments, a readable payload, and a signature segment that is not one. The
/// platform refuses it on sight, which is the point — this is a fixture for reaching a shell by
/// hand in a debug build, not a credential.
TokenPair developmentPlaceholderPair(UserRole? role) {
  final claims = role == null ? <String, Object?>{} : <String, Object?>{'role': role.wireName};
  final payload = base64Url.encode(utf8.encode(jsonEncode(claims))).replaceAll('=', '');

  return TokenPair(
    accessToken: 'notaheader.$payload.notasignature',
    refreshToken: developmentPlaceholderToken,
  );
}
