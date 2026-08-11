import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/auth/user_role.dart';

part 'session_state.freezed.dart';

/// What the app knows about the session, on this device, right now (SHIP-49).
///
/// **Three states, not two, and the third is the whole ticket.** Reading the keychain is
/// asynchronous, so between `main()` and an answer there is a window in which the app knows
/// neither that it is signed in nor that it is signed out. A two-state model has to pick one
/// to start in, and both choices are wrong in a way users see: start signed-out and every cold
/// start flashes the sign-in screen before landing on the home shell; start signed-in and a
/// signed-out user is shown a shell with no data in it. [SessionRestoring] is the honest
/// answer for that window, and the router holds a splash while it is the answer.
///
/// It is `freezed` rather than a hand-written sealed class for one specific reason: equality.
/// The router listens to this and rebuilds its redirect when it changes, so assigning a state
/// equal to the current one must be a no-op. It also makes the fields that arrive later —
/// the in-memory access token at SHIP-50, the role at SHIP-52 — a change to one constructor
/// rather than a change to every switch over it.
///
/// **None of these states is an authorisation decision** (`Docs/07` §3, `CLAUDE.md`). Signed
/// in means "this device holds a refresh token", which is a navigation fact. What the account
/// may actually do is decided server-side on every request, and a device that believes itself
/// signed in with a revoked token discovers that on its first call.
@freezed
sealed class SessionState with _$SessionState {
  /// The keychain has not answered yet. The state a cold start begins in.
  const factory SessionState.restoring() = SessionRestoring;

  /// No refresh token on this device.
  const factory SessionState.signedOut() = SessionSignedOut;

  /// A refresh token is stored.
  ///
  /// The token itself stays in the keychain rather than being copied into application state,
  /// where it would reach any crash reporter that serialises the provider tree — `Docs/07` §3
  /// forbids exactly that. SHIP-50 adds the in-memory access token.
  ///
  /// **[role] is null on a restored cold start, and that is an honest answer rather than a
  /// gap.** The keychain holds a refresh token and nothing else, so at the moment the session is
  /// restored the app knows that it is signed in and does not yet know as whom. The role is a
  /// claim in the access token the platform signs (SHIP-37), so it arrives with the first
  /// refresh (SHIP-50) or with the sign-in that follows registration (SHIP-55). Until it does,
  /// the shell shows what both roles share rather than guessing: `Docs/07` §1 keeps the customer
  /// and provider halves genuinely separate, and showing somebody the wrong one is the failure
  /// that separation exists to prevent.
  ///
  /// **It is not an authorisation claim in either direction.** The platform decides what the
  /// account may do, on every request; this selects which screens the app draws (`Docs/07` §3).
  const factory SessionState.signedIn({UserRole? role}) = SessionSignedIn;
}
