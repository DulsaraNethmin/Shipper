import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:freezed_annotation/freezed_annotation.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/identity/account.dart';
import 'package:shipper/features/identity/identity_repository.dart';

part 'signup_controller.freezed.dart';

/// Where the signup journey has got to (SHIP-51…54).
///
/// One value across four screens, because the four are one journey: the role chosen on the first
/// is what the second sends, and the account the second creates is what the third and fourth
/// verify. Splitting it per screen would mean passing the account through four route arguments
/// and losing it on any deep link that skips a step.
@freezed
abstract class SignupState with _$SignupState {
  const factory SignupState({
    /// Chosen on the role screen (SHIP-52) and sent to `POST /v1/auth/register`.
    ///
    /// Defaults to customer, which is a **form default and not a decision** — the platform fixes
    /// the role from what the request carries and makes it immutable afterwards (SHIP-45).
    @Default(UserRole.customer) UserRole role,

    /// The account, once the platform has created it. Replaced by each verification response,
    /// so `emailVerified` and `phoneVerified` are always the platform's answer and never the
    /// client's belief.
    Account? account,

    /// A request is in flight. The screens disable their submit while it is true, which stops a
    /// second tap starting a second action — the case a shared idempotency key would not cover,
    /// because the two taps would be two actions.
    @Default(false) bool busy,

    /// What the last request failed with, or `null`. Cleared when the next one starts.
    ApiFailure? failure,
  }) = _SignupState;

  const SignupState._();

  /// The address the platform recorded, normalised as it stored it. `null` before registration.
  String? get email => account?.email;

  /// The number in E.164, as the platform normalised it. This is what the OTP endpoints are
  /// given, so that the client never has to agree with the platform's normaliser — it just
  /// echoes what came back.
  String? get phone => account?.phone;

  bool get emailVerified => account?.emailVerified ?? false;
  bool get phoneVerified => account?.phoneVerified ?? false;
}

/// Drives the signup journey against the identity endpoints (SHIP-51…54).
///
/// ## It holds one [ActionKey] per action, and that is the ticket's quiet half
///
/// `CLAUDE.md` requires every state-changing request to carry an idempotency key, and `Docs/07`
/// §4 requires that key to be minted where the user acts and reused across retries of that same
/// action. Both halves are wrong in a way that is invisible until it is expensive: a fresh key
/// per attempt duplicates records after a dropped connection, and a shared key across two taps
/// of "send another code" means the second tap replays the first response and sends nothing.
/// [ActionKey] carries the rule; this class holds one per action so the actions cannot bleed
/// into one another.
///
/// ## Nothing here signs anybody in
///
/// Registration returns no token — the platform is explicit that registering is not signing in,
/// and `POST /v1/auth/login` is SHIP-41, consumed by SHIP-55. So the whole journey happens while
/// the session is signed out, which is why `app_router.dart` lets a signed-out user reach these
/// routes rather than bouncing every location to the sign-in screen.
class SignupController extends Notifier<SignupState> {
  final _keys = <String, ActionKey>{};

  @override
  SignupState build() => const SignupState();

  ActionKey _key(String action) => _keys.putIfAbsent(action, ActionKey.new);

  /// Records the role chosen at signup (SHIP-52).
  void chooseRole(UserRole role) {
    // Not a value the platform will ever send back to a client; refusing it here means a deep
    // link or a stale build cannot put the journey into a state registration would reject.
    if (role == UserRole.unknown) return;
    state = state.copyWith(role: role);
  }

  /// `POST /v1/auth/register` (SHIP-51). Returns the account, or `null` if it was refused.
  Future<Account?> register({
    required String email,
    required String phone,
    required String password,
  }) async {
    final body = registerBody(
      email: email,
      phone: phone,
      password: password,
      role: state.role,
    );

    final account = await _perform(
      'register',
      body,
      (key) => ref.read(identityRepositoryProvider).register(
            email: email,
            phone: phone,
            password: password,
            role: state.role,
            idempotencyKey: key,
          ),
    );

    if (account != null && ref.mounted) state = state.copyWith(account: account);
    return account;
  }

  /// `POST /v1/auth/verify-email` (SHIP-53).
  ///
  /// Works with no account in state, which is the deep-link case: somebody who opened the link
  /// on a phone that has been restarted since registering holds a token and nothing else. The
  /// platform returns the account, so verifying is also what recovers the rest of the journey.
  Future<Account?> verifyEmail(String token) async {
    final account = await _perform(
      'verify-email',
      verifyEmailBody(token),
      (key) => ref.read(identityRepositoryProvider).verifyEmail(
            token: token,
            idempotencyKey: key,
          ),
    );

    if (account != null && ref.mounted) state = state.copyWith(account: account);
    return account;
  }

  /// `POST /v1/auth/resend-verify` (SHIP-53). Returns how long to wait before asking again.
  ///
  /// `null` when there is no address to send to — the deep-link case again, where the app knows
  /// a token and not who it belongs to. The screen asks for the address rather than guessing.
  Future<Duration?> resendVerification() async {
    final email = state.email;
    if (email == null) return null;

    return _perform(
      'resend-verify',
      resendVerificationBody(email),
      (key) => ref.read(identityRepositoryProvider).resendVerification(
            email: email,
            idempotencyKey: key,
          ),
    );
  }

  /// `POST /v1/auth/request-otp` (SHIP-54). Returns the interval the resend timer runs from.
  ///
  /// A **fixed** interval rather than the true remaining cooldown, and the difference matters to
  /// the screen: a timer that has run out does not mean a code will be sent, only that the
  /// platform will accept being asked. The screen says "send another code", not "you may now".
  Future<Duration?> requestOtp() async {
    final phone = state.phone;
    if (phone == null) return null;

    return _perform(
      'request-otp',
      phoneBody(phone),
      (key) => ref.read(identityRepositoryProvider).requestOtp(
            phone: phone,
            idempotencyKey: key,
          ),
    );
  }

  /// `POST /v1/auth/verify-phone` (SHIP-54).
  Future<Account?> verifyPhone(String code) async {
    final phone = state.phone;
    if (phone == null) return null;

    final account = await _perform(
      'verify-phone',
      verifyPhoneBody(phone: phone, code: code),
      (key) => ref.read(identityRepositoryProvider).verifyPhone(
            phone: phone,
            code: code,
            idempotencyKey: key,
          ),
    );

    if (account != null && ref.mounted) state = state.copyWith(account: account);
    return account;
  }

  /// Drops the last failure, so a banner does not outlive the input that caused it.
  void clearFailure() {
    if (state.failure != null) state = state.copyWith(failure: null);
  }

  /// Abandons the journey. Called when the user leaves signup for the sign-in screen.
  void reset() {
    _keys.clear();
    state = const SignupState();
  }

  /// One request: mints or reuses the key, marks the journey busy, and turns a failure into
  /// something the screen can render.
  Future<T?> _perform<T>(
    String action,
    Object? body,
    Future<T> Function(String idempotencyKey) send,
  ) async {
    final actionKey = _key(action);
    final key = actionKey.forRequest(body);

    state = state.copyWith(busy: true, failure: null);

    try {
      final result = await send(key);
      actionKey.settled(null);
      if (ref.mounted) state = state.copyWith(busy: false);
      return result;
    } on ApiFailure catch (failure) {
      actionKey.settled(failure);
      if (ref.mounted) state = state.copyWith(busy: false, failure: failure);
      return null;
    } catch (error) {
      // Anything that got past ApiClient's mapping is a response that was not the one expected
      // — a body that decodes to the wrong shape is the realistic case. ApiMalformedResponse
      // says exactly that, and says it without blaming the connection. The key is retired as an
      // unknown outcome, because nothing here establishes whether the platform acted.
      const failure = ApiMalformedResponse(statusCode: 0);
      actionKey.settled(failure);
      if (ref.mounted) state = state.copyWith(busy: false, failure: failure);
      return null;
    }
  }
}

/// The signup journey, as one value the four screens share.
///
/// Not auto-disposed: the journey survives every screen in it, and a provider rebuilt when the
/// last listener went away would lose the account between the registration screen and the
/// verification ones.
final signupProvider = NotifierProvider<SignupController, SignupState>(SignupController.new);
