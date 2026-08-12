import 'dart:async';

import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/auth_interceptor.dart';
import 'package:shipper/core/api/idempotency_key.dart';
import 'package:shipper/core/auth/access_token.dart';
import 'package:shipper/core/auth/session_ender.dart';
import 'package:shipper/core/auth/session_refresher.dart';
import 'package:shipper/core/auth/session_state.dart';
import 'package:shipper/core/auth/token_pair.dart';
import 'package:shipper/core/auth/token_store.dart';
import 'package:shipper/core/auth/user_role.dart';
import 'package:shipper/core/errors/api_failure.dart';

/// Owns the session: the cold start (SHIP-49), the access token and the refresh that mints one
/// (SHIP-50), and the sign-in that starts it (SHIP-55).
///
/// The cold-start question is only ever "is there a refresh token on this device", and the answer
/// decides which shell the router lands in. It is not a claim that the token is valid — that is
/// the platform's to make, and it makes it on the first call that uses one.
///
/// ## The access token is a field here and not a field on [SessionState]
///
/// SHIP-49 anticipated the opposite and said so in `session_state.dart`. Two reasons changed the
/// answer, and both are about what a `freezed` field costs:
///
/// - **`toString`.** Every generated state class prints all of its fields, so an access token in
///   the state is an access token in any log line or crash report that mentions the session —
///   which is the thing `Docs/07` §3 forbids in the same sentence for tokens as for preferences
///   and documents. The refresh token was kept out of the state for exactly this reason; the
///   argument does not weaken for the shorter-lived one.
/// - **It is not view state.** No widget renders the access token. Putting it in the state would
///   rebuild every listener of the session on each refresh, for a value none of them reads.
///
/// The **role** is view state and stays in the state, where the shell reads it.
///
/// ## Concurrent refreshes are serialised, and that is the ticket's hard half
///
/// `contracts/paths/identity.yaml` is explicit: a refresh token presented a second time
/// invalidates the **whole device session**, including the token the device legitimately holds,
/// because the platform cannot tell a client replaying its own call from somebody with a stolen
/// copy. A client firing two refreshes at once therefore signs itself out. One in-flight future
/// is what prevents it: whoever asks first starts the refresh, everybody else awaits the same
/// future, and a request whose `401` arrives *after* somebody else's refresh finished is handed
/// the current token rather than starting another.
class SessionController extends Notifier<SessionState> implements SessionTokens {
  /// The cold start, exposed so a test can await it rather than pumping until it happens to have
  /// finished.
  ///
  /// It now covers the first refresh as well as the keychain read, because both are the cold
  /// start deciding what the session is. Widget tests do not need it — `pumpAndSettle` covers
  /// both — but a plain `ProviderContainer` test has nothing to pump.
  Future<void> get restored => _restoration;

  Future<void> _restoration = Future<void>.value();

  /// Held in memory and nowhere else (`Docs/07` §3). It survives no restart, which is what makes
  /// the refresh token the only thing that has to be stored.
  String? _accessToken;

  /// The one refresh that is allowed to be in flight. See the note on the class.
  Future<String?>? _inFlight;

  /// One key per refresh *action*, in the sense `Docs/07` §4 means it.
  ///
  /// A refresh whose reply never arrived may or may not have rotated the token, and that is
  /// precisely the case idempotency exists for: retrying under the same key replays the
  /// platform's stored answer, which is the new pair it already issued. [ActionKey] fingerprints
  /// the body, so a refresh of a *different* token — the next one — gets a fresh key without
  /// anybody deciding when to reset it.
  final _refreshKey = ActionKey();

  @override
  String? get accessToken => _accessToken;

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

    if (resolved is! SessionSignedIn) return;

    // **The state is published before this, not after.** The shell appears as soon as the
    // keychain answers; the network round trip happens behind it. Holding the splash for a
    // refresh would make every cold start as slow as the connection, and `Docs/07` §4 is built
    // on connections that are sometimes not there at all.
    //
    // Refreshing eagerly rather than waiting for the first `401` is what puts the role in front
    // of the user on a cold start. The keychain holds a refresh token and no role — the role is
    // a claim in the access token the platform signs — so without this the shell would sit on
    // "signed in, role not yet known" until something happened to make a request.
    await refreshedAccessToken(null);
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

  /// Starts a session from the pair `POST /v1/auth/login` returned (SHIP-55).
  ///
  /// The role comes from the access token's own claim rather than from anything the caller
  /// believes: the platform fixes the role at registration and signs it into the token
  /// (SHIP-37, SHIP-45), so reading it here is reading the platform's answer. It is deliberately
  /// **not** persisted beside the refresh token — a stored role is a second copy every later
  /// refresh would have to agree with, and the keychain is not where the platform's claims live.
  Future<void> signIn(TokenPair pair) => _adopt(pair);

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
  /// the session is the server-side revocation; this is the device catching up, and a stale
  /// entry it could not delete is a token the platform has already stopped honouring.
  ///
  /// ## It tells the platform, and does not wait to be told back
  ///
  /// The revocation the paragraph above leans on is `POST /v1/auth/logout` (SHIP-43), and until
  /// this it was never called — so the refresh token the device discarded stayed valid
  /// server-side for up to thirty days and the handset kept a row in `GET /v1/auth/sessions`.
  /// `Docs/11` §9 recorded that gap and named SHIP-50 as what made closing it possible: before
  /// it there was no access token to authenticate the call with.
  ///
  /// **Fire-and-forget, and it must stay that way.** It is dispatched and not awaited, its
  /// failures are swallowed, and nothing about the local clear depends on it. `Docs/07` §3 is
  /// explicit that the device is catching up rather than asking permission, and a sign-out that
  /// failed because a train went into a tunnel would be a defect rather than a safeguard.
  ///
  /// [notifyingPlatform] is `false` on the one path where the platform has already told *us* the
  /// session is over — a refused refresh. Telling it back would be a wasted request from a device
  /// that may well have no signal, sent with a credential the platform has just refused.
  Future<void> signOut({bool notifyingPlatform = true}) async {
    // Read before the clear, because the request carries it. See `session_ender.dart` for why it
    // is passed rather than picked up by an interceptor: the interceptor reads the token at
    // request time, by which point this method has cleared it.
    final spent = _accessToken;

    _accessToken = null;
    // Retired rather than carried: a key held from a refresh whose outcome was unknown belongs
    // to a session that no longer exists, and the next sign-in is a different action entirely.
    _refreshKey.settled(null);

    if (notifyingPlatform && spent != null && spent.isNotEmpty) {
      unawaited(_endPlatformSession(spent));
    }

    try {
      await ref.read(tokenStoreProvider).clear();
    } catch (_) {
      // Deliberately swallowed. See above.
    }

    if (ref.mounted) state = const SessionState.signedOut();
  }

  /// Tells the platform this device's session is over. Never awaited, never rethrown.
  ///
  /// A fresh idempotency key each time rather than an [ActionKey], and that is the right choice
  /// here rather than an omission: `Docs/07` §4 keeps a key across a retry of the *same* action,
  /// and nothing retries this. Two sign-outs are two actions against two different sessions.
  Future<void> _endPlatformSession(String accessToken) async {
    try {
      await ref.read(sessionEnderProvider).end(
            accessToken: accessToken,
            idempotencyKey: newIdempotencyKey(),
          );
    } catch (_) {
      // Every outcome is the same outcome to this device: it has signed out. The endpoint
      // answers `204` to a session already revoked, so the failures left are a lost connection
      // and an access token that expired before somebody tapped the button — and neither is
      // something to put in front of a person who is leaving.
    }
  }

  @override
  Future<String?> refreshedAccessToken(String? stale) {
    final current = _accessToken;

    // Somebody else already refreshed while this request was in flight. Refreshing again would
    // present a token the platform has already rotated away, and SHIP-40's reuse detection would
    // revoke the whole device session for it — the client doing to itself what that mechanism
    // exists to catch somebody else doing.
    if (current != null && current != stale) return Future<String?>.value(current);

    return _inFlight ??= _refresh().whenComplete(() => _inFlight = null);
  }

  /// One exchange of the stored refresh token. Never called concurrently with itself.
  Future<String?> _refresh() async {
    final stored = await _storedRefreshToken();
    if (stored == null || stored.isEmpty) {
      // Nothing to refresh with. Whatever the state said, this device has no session — and with
      // no refresh token there is nothing for the platform to end either.
      await signOut(notifyingPlatform: false);
      return null;
    }

    final key = _refreshKey.forRequest(refreshBody(stored));

    try {
      final pair = await ref
          .read(sessionRefresherProvider)
          .refresh(refreshToken: stored, idempotencyKey: key);

      _refreshKey.settled(null);
      await _adopt(pair);
      return pair.accessToken;
    } on ApiFailure catch (failure) {
      _refreshKey.settled(failure);

      // **The distinction here is the one that decides whether a driver in a tunnel gets signed
      // out.** `identity_refresh_token_invalid` is a `400`: the platform saw the token and will
      // not honour it, so the session is over and the stored token is worth nothing. A dropped
      // connection, a `503`, or a proxy's HTML error page say nothing at all about whether the
      // session is alive — signing out on those would end a perfectly good session because the
      // signal dropped. `ActionKey.outcomeUnknown` is already exactly this predicate, and using
      // it means the retry rule and the sign-out rule cannot drift apart.
      //
      // Nothing is sent to `POST /v1/auth/logout` on this path. The platform has just refused
      // the refresh token, so it has already ended the session — and reuse detection (SHIP-40)
      // means it may have ended it precisely because somebody presented a spent token.
      if (!ActionKey.outcomeUnknown(failure)) await signOut(notifyingPlatform: false);
      return null;
    } catch (error) {
      // Anything that got past ApiClient's mapping is a response that was not the one expected.
      // Nothing in it establishes whether the platform rotated the token, so the key is kept and
      // the session is left alone.
      _refreshKey.settled(const ApiMalformedResponse(statusCode: 0));
      return null;
    }
  }

  Future<String?> _storedRefreshToken() async {
    try {
      return await ref.read(tokenStoreProvider).readRefreshToken();
    } catch (_) {
      return null;
    }
  }

  /// Takes a new pair: stores the refresh token, holds the access token, publishes the role.
  ///
  /// The write completes before the state changes, so a crash between the two cannot leave a
  /// signed-in app with nothing in the keychain — the failure that would survive a restart as a
  /// session the device cannot restore. It is also what the contract requires of a rotation:
  /// the token presented is already dead, so the new one is stored before another request is
  /// made with it.
  Future<void> _adopt(TokenPair pair) async {
    await ref.read(tokenStoreProvider).writeRefreshToken(pair.refreshToken);
    if (!ref.mounted) return;

    _accessToken = pair.accessToken;
    // Falling back to the role already known, rather than to null, so a token this build could
    // not read does not move somebody from their own half of the marketplace to the neutral one.
    state = SessionState.signedIn(role: roleFromAccessToken(pair.accessToken) ?? _knownRole);
  }

  UserRole? get _knownRole => switch (state) {
        SessionSignedIn(:final role) => role,
        _ => null,
      };
}

/// The session, as one value the whole app reads.
///
/// Not auto-disposed: the session outlives every screen, and a provider that rebuilt when the
/// last listener went away would re-read the keychain and flash the splash on the way back.
final sessionProvider = NotifierProvider<SessionController, SessionState>(
  SessionController.new,
);
