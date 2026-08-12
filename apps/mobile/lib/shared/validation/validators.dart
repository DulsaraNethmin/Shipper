/// Field-level checks for the signup forms (SHIP-51, SHIP-53, SHIP-54).
///
/// **Every rule here is a convenience and none of them is a control.** `Docs/07` §2 is explicit:
/// the app may pre-validate, and the platform decides. `internal/identity/service.go` runs the
/// real rules and answers `validation_failed` with one `details` entry per offending field, in
/// the shape a form can render — so a field these functions accept may still be refused, and the
/// form has to show that refusal rather than assume it cannot happen.
///
/// What that buys, and why these exist at all: a person on mobile data in a truck yard should
/// not spend a round trip to be told they left the password blank. The split is
///
/// - **here:** presence, and shapes that cannot be anything but a mistake — no `@` in an email,
///   letters in a phone number, four digits where six were asked for.
/// - **the platform:** everything that is a rule rather than a shape.
///
/// ## The one limit that is duplicated, and what stops it drifting
///
/// [password] mirrors the ten-character minimum from `contracts/paths/identity.yaml`, and that
/// is the sort of duplication `Docs/07` §1 warns about — Dart has no over-the-air update path,
/// so a limit raised server-side leaves every installed build enforcing the old one.
///
/// It is duplicated anyway, for a reason worth stating rather than hiding: a password field with
/// no stated minimum is a field people fill in twice. What makes it safe is the direction of the
/// failure. This check can only be *stricter* than the platform in the case that matters — if
/// the platform lowers its minimum, the app asks for more than it needs, which is a nuisance —
/// and if the platform raises it, the app's own check passes and the platform's `details` land
/// under the same input with the correct number in them. **The screen renders the platform's
/// field messages inline, which is what makes a limit that moves show up correctly without a
/// release.** Nothing else here encodes a limit.
library;

/// Australian English throughout — this is copy a customer reads (`CLAUDE.md`).
abstract final class Validators {
  /// The contract's minimum (`contracts/paths/identity.yaml`). See the library note above for
  /// why this one number lives in two places.
  static const passwordMinimumLength = 10;

  /// A code from an SMS: exactly six digits, per the contract's `^[0-9]{6}$`.
  static final _sixDigits = RegExp(r'^[0-9]{6}$');

  /// The punctuation people write numbers with, which the platform's normaliser also strips.
  static final _phonePunctuation = RegExp(r'[\s\-().]');

  /// A run of digits, optionally with the country-code `+` in front.
  static final _dialledDigits = RegExp(r'^\+?[0-9]{9,15}$');

  /// An address the app is willing to send. Deliberately loose.
  ///
  /// It refuses what cannot be an address — nothing before the `@`, nothing after it, no dot in
  /// the domain, a space in the middle — and refuses nothing else. A tighter pattern is a
  /// promise about which addresses exist, and every such pattern in the wild has eventually
  /// refused somebody's real address with no way to override it from the device.
  static String? email(String? value) {
    final input = value?.trim() ?? '';
    if (input.isEmpty) return 'Enter your email address.';

    final at = input.indexOf('@');
    final looksLikeAnAddress = at > 0 &&
        at == input.lastIndexOf('@') &&
        at < input.length - 1 &&
        input.substring(at + 1).contains('.') &&
        !input.substring(at + 1).endsWith('.') &&
        !input.contains(' ');

    return looksLikeAnAddress ? null : 'Enter a valid email address.';
  }

  /// An Australian mobile number, in any of the forms a person types it.
  ///
  /// The platform normalises to E.164 before it stores or compares anything (SHIP-30), so
  /// `0412 345 678` and `+61412345678` are one number to it and must be one number here. This
  /// checks only that what was typed is made of digits and the punctuation people put between
  /// them, and that there are enough of them — **it does not normalise**, because a client that
  /// rewrote the number would be deciding what the account is, and two normalisers that disagree
  /// is how a handset ends up unable to match its own OTP.
  ///
  /// The message names the shape rather than the standard, matching the platform's own wording:
  /// somebody told their number "is not E.164" has learnt nothing.
  static String? australianMobile(String? value) {
    final input = value?.trim() ?? '';
    if (input.isEmpty) return 'Enter your mobile number.';

    final dialled = input.replaceAll(_phonePunctuation, '');
    return _dialledDigits.hasMatch(dialled)
        ? null
        : 'Enter an Australian mobile number, like 0412 345 678.';
  }

  /// A password the app is willing to send.
  ///
  /// Length is the only rule, here and on the platform — `contracts/paths/identity.yaml` cites
  /// NIST SP 800-63B for it. Composition requirements produce `Password1!`, which is a narrower
  /// search space and a harder password to remember.
  static String? password(String? value) {
    final input = value ?? '';
    if (input.isEmpty) return 'Choose a password.';
    if (input.length < passwordMinimumLength) {
      return 'Use at least $passwordMinimumLength characters.';
    }
    return null;
  }

  /// A password being **presented** rather than chosen (SHIP-55).
  ///
  /// Presence only, and the difference from [password] is deliberate rather than an oversight.
  /// The ten-character minimum is a rule about what somebody may choose; applying it at sign-in
  /// would refuse an account whose password predates the current floor — locally, so the person
  /// could not even reach the platform that would have accepted them. `contracts/paths/identity
  /// .yaml` states the same rule from the other side: sign-in checks the password for presence.
  static String? presentedPassword(String? value) =>
      (value ?? '').isEmpty ? 'Enter your password.' : null;

  /// The six-digit code from a verification SMS (SHIP-54).
  ///
  /// A shape rather than a rule: whether *this* code is the live one is the platform's to say,
  /// and it answers `identity_otp_invalid` for every way it is not — wrong, expired, retired by
  /// five earlier guesses, or for a number with no account. The screen shows that answer as it
  /// arrives and adds nothing to it.
  static String? verificationCode(String? value) {
    final input = value?.trim() ?? '';
    if (input.isEmpty) return 'Enter the six-digit code.';
    return _sixDigits.hasMatch(input) ? null : 'The code is six digits.';
  }

  /// The token from a verification email (SHIP-53).
  ///
  /// Presence only. The contract describes it as 43 base64url characters, and checking that
  /// would be the client deciding which of the platform's own tokens it is prepared to present
  /// — a token format that changes server-side would then be refused by every installed build
  /// before it ever reached the endpoint that could accept it.
  static String? verificationToken(String? value) {
    final input = value?.trim() ?? '';
    return input.isEmpty ? 'Enter the code from your email.' : null;
  }
}
