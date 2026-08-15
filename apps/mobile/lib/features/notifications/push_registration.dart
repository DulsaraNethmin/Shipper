import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/auth/session_controller.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/features/notifications/device_token.dart';
import 'package:shipper/features/notifications/notifications_repository.dart';
import 'package:shipper/features/notifications/push_token_source.dart';

/// Registers this handset for push while there is a session, and deregisters it on the way out
/// (SHIP-143).
///
/// ## Register after sign-in, not at launch
///
/// A device token binds to the **device session** the access token was issued against, so there is
/// nothing to bind to before there is one — `notifications_no_device_session` is the platform's own
/// name for asking too early. Cold-start restore counts as signing in and registers too, which is
/// what the contract asks for: "Firebase hands the app a token at every launch and only sometimes
/// the same one, so SHIP-143 calls this each time."
///
/// ## Deregister with the token the session is discarding
///
/// This is the part a straightforward reading gets wrong, and it is worth writing down because the
/// wrong version passes a test. Listening for `SessionSignedOut` and sending the `DELETE` **cannot
/// work**: `SessionController.signOut` clears the in-memory access token *before* it publishes the
/// state, so a request fired from the listener travels with no credential, meets a `401`, starts a
/// refresh, finds no refresh token either, and achieves nothing. One wasted request per sign-out
/// and no deregistration at all.
///
/// So this hangs off [signOutHooksProvider], which `core/auth` declares for exactly this and which
/// hands over the **spent** access token — the same argument `session_ender.dart` makes about
/// `POST /v1/auth/logout` and the second caller of it.
///
/// **It is dispatched beside the logout rather than before it, and may therefore lose the race.**
/// That is acceptable here and would not be for anything else: the contract states that signing out
/// ends push delivery whether or not this is called, because nothing is addressed whose session has
/// been revoked. This makes the record legible.
///
/// ## Nothing here is retried, and nothing is queued
///
/// `Docs/07` §4's offline queue holds delivery milestones and proof, and `OperationKind`'s private
/// constructor closes the set — a registration cannot be enqueued. It is also not worth queueing: a
/// token is only useful while the session it binds to is live, and one sent four hours later
/// addresses a handset whose user may have signed out on it. The next sign-in registers again,
/// which is the recovery.
class PushRegistrar {
  PushRegistrar({
    required this.repository,
    required this.tokens,
    this.platform = currentDevicePlatform,
  });

  final NotificationsRepository repository;

  /// Where the token comes from, or `null` when this build has no source — which is every build
  /// today. See `push_token_source.dart`.
  final PushTokenSource? tokens;

  /// Which store this build came from, as a function rather than a value.
  ///
  /// **Injected, and a host test is the reason.** `currentDevicePlatform` reads `Platform.isIOS`,
  /// which on the macOS or Linux machine `flutter test` runs on answers `null` — so a registrar
  /// that called it directly would send nothing in every test, and a suite written around that
  /// would pass while asserting on an empty list. That is the shape of a test that is counted and
  /// does not run, which is worse than one that does not exist.
  ///
  /// A function rather than a `DevicePlatform?` for the reason `nudgeClockProvider` is one: it is
  /// read per registration, and the production value is a call.
  final DevicePlatform? Function() platform;

  StreamSubscription<String>? _rotations;

  /// The last token this process registered, or `null`.
  ///
  /// Read by [registered] and used to skip a rotation that is not one: Firebase's refresh stream
  /// commonly re-emits the token the app already holds, and re-registering it would be a request
  /// per launch that changes nothing.
  String? _current;

  /// Whether anything has been registered in this process.
  ///
  /// What decides whether the sign-out hook sends anything at all. A build with no token source
  /// never registers, so it must never send a `DELETE` either — a deregistration for a
  /// registration that was never made is a request the platform answers `204` to and nobody wanted.
  bool get registered => _current != null;

  /// Registers the current token, if there is one to register.
  ///
  /// Answers `true` when the platform recorded a registration.
  Future<bool> register() async {
    final source = tokens;
    if (source == null) return false;

    final device = platform();
    // Neither `ios` nor `android` — a host test, a desktop shell. The endpoint's enum has no third
    // value, so there is nothing to send and a `422` is the only thing asking would achieve.
    if (device == null) return false;

    String? token;
    try {
      token = await source.current();
    } catch (_) {
      // A platform channel that threw. Ordinary rather than exceptional on a handset with no
      // Google Play services, and there is nothing to tell the user: push is a prompt, never a
      // channel of record (`Docs/07` §5), so an app with no push is an app that works.
      return false;
    }

    // No permission yet (SHIP-144 asks, and asks late), no APNs token on a simulator, or a device
    // Firebase has not issued one to. All ordinary, all silent.
    if (token == null || token.isEmpty) return false;

    _listenForRotations(source);

    return _send(token, device);
  }

  /// Tells the platform to stop sending to this handset, with the credential it is being given.
  ///
  /// Never throws. See the note on the class for why a failure changes nothing.
  Future<void> deregister({required String accessToken}) async {
    await _rotations?.cancel();
    _rotations = null;

    if (!registered) return;
    _current = null;

    try {
      await repository.deregister(
        accessToken: accessToken,
        // A fresh key rather than an [ActionKey], and that is the right call here rather than an
        // omission: `Docs/07` §4 holds a key across a retry of the *same* action, and nothing
        // retries this. Two sign-outs are two actions against two different sessions.
        idempotencyKey: newIdempotencyKey(),
      );
    } catch (_) {
      // Swallowed. What actually ends push delivery is the session revocation.
    }
  }

  /// Stops watching for rotations. Called when the provider holding this is disposed.
  Future<void> dispose() async {
    await _rotations?.cancel();
    _rotations = null;
  }

  void _listenForRotations(PushTokenSource source) {
    if (_rotations != null) return;
    _rotations = source.refreshes.listen((token) {
      final device = platform();
      if (device == null || token.isEmpty || token == _current) return;
      unawaited(_send(token, device));
    });
  }

  Future<bool> _send(String token, DevicePlatform device) async {
    try {
      await repository.register(
        token: token,
        platform: device,
        // One key per registration, and a **fresh** one each time on purpose. Registering again is
        // the ordinary case rather than a retry of the last one — a new token is a different
        // request, and reusing a key across two of them is what `idempotency_key_reused` is for.
        idempotencyKey: newIdempotencyKey(),
      );
      _current = token;
      return true;
    } on ApiErrorResponse catch (failure) {
      // The one refusal worth naming. There is no device session to bind to, so refreshing and
      // replaying — the interceptor's ordinary answer to a `401` — loops against a session that
      // will still not be there. Stop, and let the next sign-in try.
      if (failure.code == 'notifications_no_device_session') return false;
      return false;
    } catch (_) {
      // Anything else: no signal, a `503`, a body that is not a registration. The next sign-in
      // registers again, and push is a prompt rather than a channel of record.
      return false;
    }
  }
}

/// The registrar the application runs, wired to the session.
///
/// **The listener rather than a call inside `SessionController`**, which is the arrangement
/// `syncWorkerProvider` established and the reason is the same one twice over: `Docs/07` §2 forbids
/// `core/auth` knowing about a feature, and every widget test builds the app — so a registration
/// reached from inside the session would fire on all of them.
///
/// `fireImmediately` covers the cold start that restores a session: the state is published before
/// this provider is first read, so a listener that waited for a *change* would never register on a
/// launch where the user was already signed in.
///
/// The sign-out half is **not** here. It cannot be — see [PushRegistrar] — and lives on
/// [signOutHooksProvider], supplied by `main.dart`.
final pushRegistrarProvider = Provider<PushRegistrar>((ref) {
  final registrar = PushRegistrar(
    repository: ref.watch(notificationsRepositoryProvider),
    tokens: ref.watch(pushTokenSourceProvider),
    platform: ref.watch(devicePlatformProvider),
  );
  ref.onDispose(() => unawaited(registrar.dispose()));

  ref.listen(
    sessionProvider,
    (previous, next) {
      if (next is SessionSignedIn) unawaited(registrar.register());
    },
    fireImmediately: true,
  );

  return registrar;
});

/// Which store this build came from. See [PushRegistrar.platform] for why it is injectable.
final devicePlatformProvider =
    Provider<DevicePlatform? Function()>((ref) => currentDevicePlatform);

/// The sign-out half, as a hook `main.dart` puts on [signOutHooksProvider].
///
/// A function of the container rather than a closure written inline there, so that what runs on
/// sign-out is readable in the file that owns the behaviour rather than in the wiring.
SignOutHook pushDeregistrationHook(Ref ref) {
  return (accessToken) => ref.read(pushRegistrarProvider).deregister(accessToken: accessToken);
}
