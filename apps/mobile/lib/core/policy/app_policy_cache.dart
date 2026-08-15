/// Where the last answer from `GET /v1/app/policy` is kept (SHIP-167a).
///
/// ## A file, and not a row in the queue's database
///
/// `core/queue` already owns a Drift database on this device, and putting one more table in it was
/// the obvious move. It is the wrong one for three reasons, and the third is the one that decides
/// it:
///
/// - **The queue's database is transactional because it has to be.** SHIP-124 chose Drift over a
///   key-value store because an operation, its idempotency key, the time the user acted and the
///   path to its proof image either all commit or none of them do. Nothing here is a transaction:
///   there is one value, it is replaced whole, and a half-written policy is a policy that failed to
///   decode and is ignored.
/// - **`Docs/07` §3 clears the queue at sign-out**, and one account's unsynced work must never be
///   sent under another's credentials. The policy is the opposite: it is about the *device*, not
///   the account, and it should survive a sign-out exactly as the compiled default does. Sharing a
///   store with something that is deliberately wiped is how it would eventually be wiped too.
/// - **It is read before there is a session, at launch.** Opening the queue's database to answer
///   "how long before I nudge" would tie the launch path to the store the sync worker owns, which
///   is the coupling `queue_watch.dart` inverted a provider to avoid.
///
/// So: one small JSON file, in the same directory `ProofStore` writes to and for the same reason —
/// `getApplicationSupportDirectory()` is private to this application on both platforms and, unlike
/// the cache directory, is **not purgeable by the operating system.** A policy the OS deleted
/// overnight would put a device that has been online for months back on the compiled default,
/// which is precisely the case this exists to prevent.
library;

import 'dart:convert';
import 'dart:io';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:path_provider/path_provider.dart';

import 'package:shipper/core/policy/app_policy.dart';

/// The last policy this device was told, if it has ever been told one.
abstract interface class AppPolicyCache {
  /// What was last written, or `null` when this device has never had an answer.
  ///
  /// **`null` and a failure are the same outcome deliberately.** A file that is missing, truncated,
  /// not JSON, or written by a build with a different shape all mean the same thing to the caller:
  /// there is nothing here to apply. Distinguishing them would offer a choice nobody can act on —
  /// there is no repair for a corrupt policy file other than fetching again, which the app does
  /// anyway on every launch.
  Future<AppPolicy?> read();

  /// Keeps [policy] for the next launch that has no connection.
  ///
  /// Failures are the caller's to swallow. A device that could not write the file still applies
  /// what it fetched for this process; what it loses is the next offline launch, which is the
  /// situation it was already in.
  Future<void> write(AppPolicy policy);
}

/// The real one: a JSON file under the application's own support directory.
final class FileAppPolicyCache implements AppPolicyCache {
  const FileAppPolicyCache(this.root);

  /// The application's private directory. See the library note for which one and why.
  final Directory root;

  /// Opens the cache the application uses.
  static Future<FileAppPolicyCache> open() async =>
      FileAppPolicyCache(await getApplicationSupportDirectory());

  /// The file name, under [root].
  ///
  /// Flat rather than in a folder of its own, unlike `ProofStore`: there is exactly one of these
  /// and there will not be a second, so a directory would be a container for one file forever.
  static const fileName = 'app_policy.json';

  File get file => File('${root.path}${Platform.pathSeparator}$fileName');

  @override
  Future<AppPolicy?> read() async {
    try {
      final handle = file;
      if (!await handle.exists()) return null;

      final decoded = jsonDecode(await handle.readAsString());
      if (decoded is! Map<String, dynamic>) return null;

      return AppPolicy.fromJson(decoded);
    } catch (_) {
      // See [AppPolicyCache.read]: every way this can fail means the same thing to the caller.
      return null;
    }
  }

  @override
  Future<void> write(AppPolicy policy) async {
    await root.create(recursive: true);
    // `flush: true`, because the case this file exists for is a device that is about to lose
    // power, signal or both. A policy sitting in the operating system's write buffer when the
    // handset dies is a policy this device never had.
    await file.writeAsString(jsonEncode(policy.toJson()), flush: true);
  }
}

/// The cache the running application uses — **`null` until `main.dart` supplies one.**
///
/// The same inversion as `queueWatchProvider` and `runningBuildProvider`, and it is here for the
/// same reason both of those are: every widget test builds `ShipperApp`, the nudge inside it reads
/// the policy, and a cache that resolved its own directory would have each of those tests call
/// `path_provider` — a plugin channel a host test has nothing behind.
///
/// **It is also what decides whether a request is made at all.** See
/// `appPolicyRefreshProvider`: an application with nowhere to keep an answer does not ask for one,
/// which keeps SHIP-167a free for the rest of the suite exactly as SHIP-168's gate is.
///
/// The consequence worth stating plainly: **an application that never overrides this runs on the
/// compiled default forever**, whatever the platform is configured with. That is right for a test
/// and would be a defect in production, so `policy_wiring_test.dart` holds `main`'s override
/// rather than trusting it.
final appPolicyCacheProvider = Provider<AppPolicyCache?>((ref) => null);
