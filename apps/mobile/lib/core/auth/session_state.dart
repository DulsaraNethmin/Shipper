import 'package:freezed_annotation/freezed_annotation.dart';

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
  /// Deliberately carries nothing yet. The token itself stays in the keychain rather than
  /// being copied into application state, where it would reach any crash reporter that
  /// serialises the provider tree — `Docs/07` §3 forbids exactly that. SHIP-50 adds the
  /// in-memory access token; SHIP-52 adds the role that selects the shell.
  const factory SessionState.signedIn() = SessionSignedIn;
}
