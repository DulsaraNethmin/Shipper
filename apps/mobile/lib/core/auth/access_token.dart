import 'dart:convert';

import 'package:shipper/core/auth/user_role.dart';

/// Reads the `role` claim out of an access token, for the one thing the client uses it for:
/// choosing which half of the marketplace to draw (SHIP-50, SHIP-52).
///
/// ## This is not verification, and nothing here may become an authorisation decision
///
/// The signature is **not** checked, and it cannot be — the key belongs to the platform
/// (`internal/identity/token.go`), and shipping a verification key in a build on somebody's
/// phone would not make the check mean anything anyway. `Docs/07` §3 and `CLAUDE.md` both put
/// every authorisation decision server-side, on every request. A device that hand-edits its own
/// access token gets a different *shell* and exactly the same refusals from the platform.
///
/// What it is for: `SessionState.signedIn` carries a role so the shell knows which half to show,
/// the role is a claim in the token the platform signed (`Docs/10` §5 fixes the claim set as
/// `sub, role, sid, iat, exp, jti, iss, aud`), and reading one field of an already-received
/// response is cheaper and more honest than a second endpoint that answers the same question.
///
/// ## Three answers, and the difference between two of them matters
///
/// - A role this build knows — [UserRole.customer] or [UserRole.provider].
/// - A role it does not — [UserRole.unknown], which the shell renders as "update the app"
///   (SHIP-167). A build on a phone outlives the contract it shipped with.
/// - **`null`, meaning the token said nothing this function could read.** Not the same as
///   unrecognised: it is a malformed token, an empty string, or a body with no `role` at all.
///   The shell shows what both halves share, which is what a restored cold start looks like
///   before the first refresh. Guessing customer here would show every provider the wrong
///   marketplace.
UserRole? roleFromAccessToken(String accessToken) {
  final claims = _claimsOf(accessToken);
  final role = claims?['role'];
  if (role is! String || role.isEmpty) return null;

  return UserRole.values.firstWhere(
    (candidate) => candidate.wireName == role && candidate != UserRole.unknown,
    orElse: () => UserRole.unknown,
  );
}

/// The payload segment, decoded, or `null` if it is not readable as one.
///
/// Every failure is the same answer. A token that is not three segments, a segment that is not
/// base64url, base64url that is not JSON, JSON that is not an object: none of them is a case the
/// app can do anything different about, and each would otherwise be an exception thrown from
/// inside a network interceptor.
Map<String, Object?>? _claimsOf(String token) {
  final segments = token.split('.');
  if (segments.length != 3) return null;

  try {
    // JWT uses base64url with the padding stripped, which `base64Url.decode` refuses;
    // `normalize` puts it back. Doing this by hand is how a token whose payload length happens
    // to be a multiple of four decodes and every other one throws.
    final payload = utf8.decode(base64Url.decode(base64Url.normalize(segments[1])));
    final decoded = jsonDecode(payload);
    return decoded is Map<String, Object?> ? decoded : null;
  } catch (_) {
    return null;
  }
}
