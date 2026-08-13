import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import 'package:shipper/core/version/minimum_version.dart';
import 'package:shipper/core/version/running_build.dart';
import 'package:shipper/core/version/update_required_screen.dart';

/// What the launch gate decided about this build (SHIP-168).
@immutable
class UpdateVerdict {
  const UpdateVerdict._({required this.blocked, this.storeUrl});

  /// This build may run.
  const UpdateVerdict.carryOn() : this._(blocked: false);

  /// This build is below the floor. [storeUrl] is `null` when the platform sent no link.
  const UpdateVerdict.blockedWithout() : this._(blocked: true);

  const UpdateVerdict.blockedLinkingTo(String storeUrl)
    : this._(blocked: true, storeUrl: storeUrl);

  final bool blocked;

  /// Where to send the user, or `null` — which is an ordinary case and not a failure. See
  /// `minimum_version.dart` and `update_required_screen.dart`.
  final String? storeUrl;

  @override
  bool operator ==(Object other) =>
      other is UpdateVerdict && other.blocked == blocked && other.storeUrl == storeUrl;

  @override
  int get hashCode => Object.hash(blocked, storeUrl);

  @override
  String toString() => blocked ? 'UpdateVerdict.blocked($storeUrl)' : 'UpdateVerdict.carryOn()';
}

/// The whole of the decision, as a pure function of the two facts it needs.
///
/// Separated from the providers so it can be read as a table and tested as one, in the same
/// spirit as `redirectFor` in the router. Every cell that returns [UpdateVerdict.carryOn] is a
/// deliberate fail-open and is argued in [launchVersionCheckProvider]; the only cell that blocks
/// is a known build, on a platform the endpoint answers for, strictly below a known floor.
///
/// **`<` and not `<=`.** The platform's field is the *lowest build number still permitted* —
/// `internal/config/config.go` says so in those words — so a build equal to the floor is the
/// oldest supported build and must run. Getting this one character wrong locks out every device
/// on the exact build somebody just raised the floor to, which is the version of this defect that
/// looks correct in a code review and is discovered by support.
@visibleForTesting
UpdateVerdict verdictFor({required RunningBuild? build, required MinimumVersion? floors}) {
  if (build == null || floors == null) return const UpdateVerdict.carryOn();

  final floor = switch (build.platform) {
    AppPlatform.ios => floors.ios,
    AppPlatform.android => floors.android,
    AppPlatform.other => null,
  };
  if (floor == null) return const UpdateVerdict.carryOn();

  if (build.number >= floor.minimumBuild) return const UpdateVerdict.carryOn();

  // Normalised here rather than at the screen so "no link" is one condition and not two. The
  // wire omits the key when it is blank, but a deployment that sets the variable to a space is
  // not a link either.
  final url = floor.storeUrl?.trim() ?? '';
  return url.isEmpty ? const UpdateVerdict.blockedWithout() : UpdateVerdict.blockedLinkingTo(url);
}

/// The launch check.
///
/// ## When it runs
///
/// On the first build of [VersionGate], which is the first frame of the application — `Docs/07`
/// §6's "at launch". It is **not** awaited before `runApp`: the app draws its normal first screen
/// and the block appears when the answer arrives, typically inside a second.
///
/// The alternative — hold the first frame until the platform answers — was rejected. It makes a
/// network round trip a hard dependency of *starting the app at all*, and on a bad connection the
/// user watches a blank screen for the ten seconds of `connectTimeout` before anything happens.
/// `Docs/07` §4 calls working without signal the single most important client capability; an app
/// that will not open on a loading dock is not that app.
///
/// ## What an unreachable API means, which is the decision worth arguing
///
/// **It does not block.** A check that has not answered, has failed, or has thrown while decoding
/// leaves [updateVerdictProvider] with no floor, [verdictFor] returns [UpdateVerdict.carryOn], and
/// the app runs.
///
/// That is failing open, and failing open lets an old build through — for that launch. Three
/// things make it the right trade:
///
/// - **The gate is not a control.** `Docs/07` §3 and `CLAUDE.md` both put every authorisation
///   decision on the platform, and this is the same rule in another costume: the app may block,
///   the platform decides. A build the floor has retired is one `/v1` stops answering, and that
///   is what actually retires it. This screen exists to tell a user *why*, and where to go.
/// - **Failing closed brands the app on the platform's worst day.** Every device that opens the
///   app during an outage would show an update prompt for an update that does not exist, and the
///   only way out would be a release — which is exactly the loop `Docs/07` §6 says mobile does
///   not have. A gate that turns a partial outage into a total one is worse than the builds it
///   was guarding against.
/// - **The cost is bounded and self-correcting.** The check runs again on the next launch, and
///   an old build with no connection can do nothing this gate was protecting against anyway.
///
/// **A failure is retried within the launch, and that is Riverpod's own retry rather than one
/// written here.** `ProviderContainer.defaultRetry` re-runs a failed provider ten times with
/// exponential backoff from 200ms to a 6.4-second ceiling, so a launch that lands in a lift gets
/// its answer roughly forty-five seconds later without the user doing anything, and a device with
/// no signal at all stops asking rather than polling for as long as the app is open. That is the
/// right shape for this, so the default is **kept deliberately** and not replaced by a second
/// backoff implementation — SHIP-125 already owns the one this application needs, and its five
/// minutes are for a queue that must eventually drain rather than for a courtesy at start-up.
///
/// One consequence has to be read carefully by anything that touches [updateVerdictProvider]:
/// while Riverpod is retrying, the state is **`AsyncLoading` carrying an error**, not `AsyncError`.
/// A fail-open written as "block unless the state is an error" would therefore be wrong in the
/// one case it was written for. Reading the value is the only safe form, which is what it does.
///
/// **The limitation this leaves, stated rather than hidden:** the check runs once per process,
/// and a handset process can survive for days. A floor raised this morning reaches a device the
/// next time its app is cold-started, not the next time it is brought to the foreground. Adding a
/// resume trigger is the obvious extension — SHIP-125's `sync_signals.dart` already listens to
/// the lifecycle for its own reasons — and it was left out because `Docs/09` says *launch* and
/// because a second trigger wants the soft-prompt half of `Docs/07` §6, which is not this ticket.
///
/// ## Why it makes no request when nothing supplied a build
///
/// Not an optimisation. `runningBuildProvider` is empty in every widget test (see its note), and
/// a check that fired anyway would have each of them open a connection to whatever base URL the
/// test binary was compiled with.
final launchVersionCheckProvider = FutureProvider<MinimumVersion?>((ref) async {
  if (ref.watch(runningBuildProvider) == null) return null;
  return ref.watch(minimumVersionRepositoryProvider).fetch();
});

/// [verdictFor] over the two providers.
final updateVerdictProvider = Provider<UpdateVerdict>((ref) {
  // Loading and error both fall through to `null`, which **is** the fail-open argued above, and
  // it is one expression on purpose: an unreachable API and a check still in flight are the same
  // situation seen from the gate — nobody has told this build it is too old. Riverpod 3 makes
  // that an explicit case rather than a nullable value, as `PendingUpdatesIndicator` also reads.
  //
  // Matching on the value rather than on the *absence* of an error is load-bearing: a failure
  // being retried is reported as `AsyncLoading` **with an error attached**. See the class note.
  final answered = ref.watch(launchVersionCheckProvider);
  final floors = switch (answered) {
    AsyncData(:final value) => value,
    _ => null,
  };

  return verdictFor(build: ref.watch(runningBuildProvider), floors: floors);
});

/// How the app leaves for the store.
///
/// A seam rather than a bare `launchUrl` call, for the reason every other platform channel in
/// this application has one: a host test has no plugin behind it, and a test that cannot observe
/// the destination cannot tell a link to the store from a link to nowhere. The typedef is
/// declared beside the screen that takes it — see `update_required_screen.dart`.
final storeOpenerProvider = Provider<StoreOpener>((ref) {
  // `externalApplication` so the link opens the App Store or Play app rather than a web view
  // inside Shipper — a store page in an in-app browser cannot install anything.
  return (destination) => launchUrl(destination, mode: LaunchMode.externalApplication);
});

/// The barrier (SHIP-168).
///
/// Mounted by `ShipperApp` inside `MaterialApp.router`'s builder, above the navigator and above
/// the pending-updates indicator, so it has the theme and the media query and covers every route
/// there is.
///
/// ## It replaces the application rather than covering it, and that is what "blocks" means
///
/// `Docs/09` says *blocks*, which is a stronger word than *prompts*, and the reading taken here
/// is the strong one: below the floor there is **no way back into the app in this process**.
/// Returning the screen in place of [child] means the router is not built at all — there is no
/// screen behind this one to reach by dismissing it, by the Android back gesture, or by a deep
/// link, because there is nothing there. A dialog or a `Stack` overlay would have left a live
/// application underneath, and on Android a modal barrier is dismissible almost by definition.
///
/// It is not dismissible and it has no "later". `Docs/07` §6 has a soft prompt for the case where
/// carrying on is acceptable — above the floor — and it is a different mechanism for a different
/// situation. This one is for builds the platform is retiring.
///
/// **The queue keeps draining while it is up**, because the sync worker runs from `main` and not
/// from the widget tree. That is deliberate: work a driver already recorded belongs to them, and
/// a blocked build should still hand it over if the platform will still take it.
class VersionGate extends ConsumerWidget {
  const VersionGate({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final verdict = ref.watch(updateVerdictProvider);
    if (!verdict.blocked) return child;

    return UpdateRequiredScreen(
      storeUrl: verdict.storeUrl,
      openStore: ref.read(storeOpenerProvider),
    );
  }
}
