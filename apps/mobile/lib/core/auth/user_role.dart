import 'package:json_annotation/json_annotation.dart';

/// Which half of the marketplace an account belongs to (SHIP-52).
///
/// **It lives in `core/auth` rather than in `features/identity`, and that placement is the
/// decision.** The role is chosen on a screen the identity feature owns, but it is read by the
/// session (`core/auth/session_state.dart`) and by the shell the router lands in
/// (`core/routing/signed_in_shell.dart`). `Docs/07` §2 moves anything two places need out of a
/// feature — and a feature that `core/` imported would be a feature every other one depends on
/// through the back door.
///
/// **Choosing a role is not an authorisation decision** (`Docs/07` §3, `CLAUDE.md`). It selects
/// which surfaces the app *shows*: `Docs/07` §1 requires the customer and provider halves to be
/// genuinely separate inside the one app, and this is what selects between them. What an account
/// may actually do is decided server-side on every request, and the platform fixes the role at
/// registration with a database trigger (SHIP-45) rather than trusting what a device sends
/// afterwards.
enum UserRole {
  /// Publishes delivery jobs and awards a bid.
  @JsonValue('customer')
  customer,

  /// Bids on jobs and performs deliveries.
  @JsonValue('provider')
  provider,

  /// A role this build has never heard of.
  ///
  /// Not a value the platform sends today — `contracts/paths/identity.yaml` enumerates exactly
  /// the two above, and `admin` is deliberately not among them because administrators sign in
  /// through a separate system (SHIP-147). It exists because the alternative to decoding an
  /// unrecognised role is *throwing* on it, and `Docs/07` §6 is built on old builds living on
  /// devices indefinitely. A build that crashes rather than degrades has no over-the-air fix.
  ///
  /// The shell renders it as "this build does not recognise your account type", which is what
  /// SHIP-167's version gate exists to resolve — not as a customer, and not as a provider.
  /// Guessing either way would show somebody the wrong half of the marketplace.
  unknown;

  /// The roles a person may choose at signup.
  ///
  /// [unknown] is deliberately absent: it is something the platform can say, never something a
  /// client may ask for.
  static const selectable = <UserRole>[UserRole.customer, UserRole.provider];

  /// What the wire calls this role. The two selectable values match `Account.role` in
  /// `contracts/paths/identity.yaml`.
  String get wireName => switch (this) {
        UserRole.customer => 'customer',
        UserRole.provider => 'provider',
        UserRole.unknown => 'unknown',
      };

  /// One word, for a heading or a summary line. Longer copy belongs on the screen that shows
  /// it, not here — `role_selection_screen.dart` carries the sentence that explains each.
  String get label => switch (this) {
        UserRole.customer => 'Customer',
        UserRole.provider => 'Provider',
        UserRole.unknown => 'Unrecognised',
      };
}
