/// Where the registration token comes from (SHIP-143).
///
/// ## This file is a seam with nothing behind it, and that is recorded rather than hidden
///
/// The token is Firebase's. `Docs/11` §3 already carries the reason there is no implementation
/// here, from the platform side of the same chain: **"No Firebase project exists, and a
/// service-account key is on `CLAUDE.md`'s never-commit list."** SHIP-139 built the dispatch adapter
/// against a stub HTTP server for exactly that reason and said so.
///
/// The client half is worse rather than better, because a Flutter app cannot fake its way to a
/// token at all: `firebase_messaging` needs `firebase_core`, which needs a `google-services.json`
/// and a `GoogleService-Info.plist` that do not exist — and adding the Gradle plugin without the
/// first breaks `make flutter-build` for every other lane. So the dependency is deliberately **not**
/// in `pubspec.yaml` yet.
///
/// **No ticket anywhere creates the Firebase project.** X-1 to X-9 are the D-U-N-S number, the two
/// store enrolments, the legal brief, the pilot metro area, the auto-complete decision, the privacy
/// policy, the terms and the prohibited-goods list; none of them is this. SHIP-139, SHIP-140,
/// SHIP-143, SHIP-144 and SHIP-145 all sit behind it. That gap is reported in `Docs/11` §3.
///
/// ## What that leaves, and why it is worth building now
///
/// Everything except the token: the two requests, the bodies, the idempotency keys, the credential
/// the deregistration has to carry, when each fires, and what happens when the platform says there
/// is no device session. All of it is exercised against a substituted source, and the day the
/// project exists the change is one class and one line in `main.dart`.
///
/// It is the shape every other platform channel in this client already has — `RunningBuild`,
/// `StoreOpener`, `ProofCompressor` — for the reason those have one: a host test has no plugin
/// behind a channel, and a seam is what lets the behaviour around it be demonstrated rather than
/// asserted.
library;

import 'package:flutter_riverpod/flutter_riverpod.dart';

/// The registration token this handset is addressable at.
abstract interface class PushTokenSource {
  /// The current token, or `null` when there is not one.
  ///
  /// `null` is an ordinary answer rather than a failure: notification permission may not have been
  /// granted yet (SHIP-144 asks for it, and asks late), the device may have no Google Play services,
  /// and a simulator may have no APNs token at all. A registrar that treated it as an error would
  /// turn the ordinary case into a log line nobody can act on.
  Future<String?> current();

  /// Tokens issued after the first, if this source can report them.
  ///
  /// Firebase rotates a token without being asked — on a restore, on a reinstall, when its own
  /// storage is cleared — and a registration that was only made at sign-in would then be addressing
  /// a handset that no longer answers to it. An implementation with no way to observe rotation
  /// returns an empty stream, and the sign-in registration is what keeps it roughly current.
  Stream<String> get refreshes;
}

/// The source the running application uses — **`null` until something supplies one.**
///
/// Two reasons, and both are the reason `queueWatchProvider` and `appPolicyCacheProvider` are the
/// same shape. The first is the one that applies today: **there is nothing to supply**, because no
/// Firebase project exists (see the library note), so a client that reached for a token on its own
/// would call a plugin that is not in the build.
///
/// The second outlives that. Every widget test builds `ShipperApp` and many of them sign in; a
/// registrar that fetched a token on its own would have each of them call a platform channel and
/// then make an HTTP request to whatever base URL the test binary was compiled with.
///
/// The consequence worth stating plainly: **an application that never overrides this registers
/// nothing**, which is the production behaviour today and is correct — a registration with no
/// dispatcher behind it would be a row in a table nothing can use.
final pushTokenSourceProvider = Provider<PushTokenSource?>((ref) => null);
