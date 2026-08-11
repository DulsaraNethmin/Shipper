import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/token_store.dart';

/// Owns the session, and answers the question a cold start asks (SHIP-49).
///
/// The question is only ever "is there a refresh token on this device", and the answer decides
/// which shell the router lands in. It is not a claim that the token is valid — that is the
/// platform's to make, and it makes it on the first call that uses one.
///
/// SHIP-50 gives this class the access token it holds in memory and the refresh that mints
/// one; SHIP-51 and SHIP-55 give it the registration and sign-in calls that produce the first
/// refresh token. Until then [signIn] takes a token from its caller, because there is no
/// endpoint in this wave to get one from and inventing one would put a fake credential path
/// into the client permanently.
class SessionController extends Notifier<SessionState> {
  /// The restore started by [build], exposed so a test can await the cold start rather than
  /// pumping until it happens to have finished.
  ///
  /// Widget tests do not need it — `pumpAndSettle` covers the microtask — but a plain
  /// `ProviderContainer` test has nothing to pump.
  Future<void> get restored => _restoration;

  Future<void> _restoration = Future<void>.value();

  @override
  SessionState build() {
    // Deliberately not `await`ed into the state: Notifier.build is synchronous, and the
    // asynchronous answer is what SessionRestoring exists to represent. An AsyncNotifier would
    // have expressed the wait as AsyncLoading instead, which is the same three states in a
    // second vocabulary — and the router would then have to translate between them on every
    // redirect. One vocabulary, owned here.
    _restoration = _restore();
    return const SessionState.restoring();
  }

  Future<void> _restore() async {
    final resolved = await _readStoredSession();

    // The restore outlives nothing in the application — this provider is never disposed —
    // but a test that disposes its container mid-read would otherwise fail on an unmounted
    // ref rather than on what it was testing.
    if (!ref.mounted) return;
    state = resolved;
  }

  Future<SessionState> _readStoredSession() async {
    try {
      final token = await ref.read(tokenStoreProvider).readRefreshToken();
      return token == null || token.isEmpty
          ? const SessionState.signedOut()
          : const SessionState.signedIn();
    } catch (_) {
      // A keychain that cannot be read is treated as no session, and that direction is not
      // arbitrary. Signed-out costs the user a sign-in; signed-in with unreadable storage
      // leaves the app in a shell whose every request fails and no route back to the sign-in
      // screen. Nothing is lost either way — the session also exists server-side in
      // `device_sessions` (SHIP-38), and the stored token was about to be rotated anyway.
      return const SessionState.signedOut();
    }
  }

  /// Records a refresh token and moves the session to signed in.
  ///
  /// The write completes before the state changes, so a crash between the two cannot leave a
  /// signed-in app with nothing in the keychain — the failure that would survive a restart as
  /// a session the device cannot restore.
  Future<void> signIn({required String refreshToken}) async {
    await ref.read(tokenStoreProvider).writeRefreshToken(refreshToken);
    if (!ref.mounted) return;
    state = const SessionState.signedIn();
  }

  /// Ends the session on this device.
  ///
  /// `Docs/07` §3 also clears the offline queue, cached job data and the push registration at
  /// sign-out. Those arrive with SHIP-124, SHIP-143 and the caching work, and each clears its
  /// own store; this method is the point they will be called from.
  ///
  /// The state changes even when clearing throws, and the failure is not rethrown. A sign-out
  /// that leaves the user in the signed-in shell because a delete failed is the worst of both
  /// outcomes — they believe they are signed out and the app behaves as though they are not —
  /// and there is nothing a caller could usefully do with the error either. What actually ends
  /// the session is the server-side revocation (SHIP-46); this is the device catching up, and
  /// a stale entry it could not delete is a token the platform has already stopped honouring.
  Future<void> signOut() async {
    try {
      await ref.read(tokenStoreProvider).clear();
    } catch (_) {
      // Deliberately swallowed. See above.
    }

    if (ref.mounted) state = const SessionState.signedOut();
  }
}

/// The session, as one value the whole app reads.
///
/// Not auto-disposed: the session outlives every screen, and a provider that rebuilt when the
/// last listener went away would re-read the keychain and flash the splash on the way back.
final sessionProvider = NotifierProvider<SessionController, SessionState>(
  SessionController.new,
);
