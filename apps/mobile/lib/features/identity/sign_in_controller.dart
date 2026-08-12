import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/device/device_label.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/identity/identity_repository.dart';

part 'sign_in_controller.freezed.dart';

/// What the sign-in screen is doing right now (SHIP-55).
@freezed
abstract class SignInState with _$SignInState {
  const factory SignInState({
    /// A request is in flight. The screen disables its submit while it is true, which is what
    /// stops a second tap becoming a second **device session** — two taps are two actions, and
    /// an idempotency key is deliberately per-action (`Docs/07` §4).
    @Default(false) bool busy,

    /// What the last attempt failed with, or `null`. Cleared when the next one starts.
    ApiFailure? failure,
  }) = _SignInState;
}

/// Signs an existing account in (SHIP-55).
///
/// Separate from `SignupController` rather than a method on it, because they are separate
/// journeys with separate lifetimes: the signup state carries an account across four screens and
/// is reset when that journey ends, and somebody signing in on a fresh install has no journey at
/// all.
///
/// ## Retrying under the same key is what keeps the device list honest
///
/// `contracts/paths/identity.yaml` warns that each sign-in creates a device session, because
/// there is no device identifier to match on — so a client that signs in twice leaves the first
/// session live for thirty days and shows its owner a device list they cannot make sense of.
/// A dropped connection during sign-in is exactly the case that would cause it. [ActionKey]
/// keeps the key when the outcome is unknown, so the retry replays the platform's stored answer
/// instead of starting a second session. A wrong password retires it, because correcting a
/// password is a new action and the platform would otherwise replay the refusal.
class SignInController extends Notifier<SignInState> {
  final _key = ActionKey();

  @override
  SignInState build() => const SignInState();

  /// Returns `true` when the session was started.
  ///
  /// The screen does not navigate on success and must not: the session changing is what moves
  /// the app, through the router's guard. A screen that also pushed a route would be a second
  /// mechanism deciding where a signed-in user goes, and the two would disagree the first time
  /// somebody signed in from anywhere else.
  Future<bool> signIn({required String email, required String password}) async {
    final deviceLabel = ref.read(deviceLabelProvider);
    final key = _key.forRequest(
      loginBody(email: email, password: password, deviceLabel: deviceLabel),
    );

    state = state.copyWith(busy: true, failure: null);

    try {
      final pair = await ref.read(identityRepositoryProvider).login(
            email: email,
            password: password,
            deviceLabel: deviceLabel,
            idempotencyKey: key,
          );

      _key.settled(null);

      // The token reaches the Keychain before the state says the app is signed in, which is the
      // session's own rule (`core/auth/session_controller.dart`).
      await ref.read(sessionProvider.notifier).signIn(pair);

      if (ref.mounted) state = const SignInState();
      return true;
    } on ApiFailure catch (failure) {
      _key.settled(failure);
      if (ref.mounted) state = state.copyWith(busy: false, failure: failure);
      return false;
    } catch (error) {
      // Anything that got past ApiClient's mapping is a response that was not the one expected —
      // a token pair missing a token is the realistic case, and `TokenPair.fromJson` says so.
      // The key is kept as an unknown outcome: nothing here establishes whether the platform
      // created a session.
      const failure = ApiMalformedResponse(statusCode: 0);
      _key.settled(failure);
      if (ref.mounted) state = state.copyWith(busy: false, failure: failure);
      return false;
    }
  }

  /// Drops the last failure, so a banner does not outlive the input that caused it.
  void clearFailure() {
    if (state.failure != null) state = state.copyWith(failure: null);
  }
}

/// The sign-in screen's state.
///
/// Not auto-disposed, so a failure survives the rebuild that shows it and the key survives the
/// keyboard opening.
final signInProvider = NotifierProvider<SignInController, SignInState>(SignInController.new);
