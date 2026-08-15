import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/policy/app_policy.dart';
import 'package:shipper/core/policy/app_policy_cache.dart';

/// Which of the three possible answers this build applies (SHIP-167a).
///
/// **This function is the ticket.** Its *Done when* has a clause that is easy to half-meet — *"the
/// app caches the last response and applies it with no connection, falling back to a compiled
/// default only when it has never had one"* — and the half-meeting is a build that treats *offline*
/// and *never told* as one case. It reads correctly, it passes an offline test, and it makes the
/// endpoint have no effect at all on the devices it exists for, because those devices are offline
/// at the moment the number is used.
///
/// So the precedence is three rungs and not two:
///
/// 1. **[fetched]** — the platform answered during this launch. Whatever it said.
/// 2. **[cached]** — it did not, and this device has been told something before. That still
///    applies. Having no connection *now* says nothing about what the platform decided *then*, and
///    the answer was fetched in advance precisely so it would be here for this moment.
/// 3. **[compiledAppPolicy]** — this install has never once been online. The only case the compiled
///    numbers are used for.
///
/// [AppPolicy.isUsable] filters each rung rather than the result, which is the difference between
/// falling back a rung and falling all the way down: a platform that answered with a zero threshold
/// leaves a device that has a good cached policy using the cached one, not the compiled one.
///
/// Pure, and separated from the providers for the reason `verdictFor` and `redirectFor` are: it is
/// a table, every defect in it is a cell somebody did not think about, and the cells are only
/// obvious written down together.
@visibleForTesting
AppPolicy resolveAppPolicy({required AppPolicy? fetched, required AppPolicy? cached}) {
  if (fetched != null && fetched.isUsable) return fetched;
  if (cached != null && cached.isUsable) return cached;
  return compiledAppPolicy;
}

/// What this device was told last time it had signal, or `null` if it never has been.
///
/// Read once per launch. Nothing invalidates it: [appPolicyRefreshProvider] writes the file and
/// returns the same value through [appPolicyProvider], so a re-read would only ever confirm what
/// the fetch already supplied.
final cachedAppPolicyProvider = FutureProvider<AppPolicy?>((ref) async {
  final cache = ref.watch(appPolicyCacheProvider);
  if (cache == null) return null;
  return cache.read();
});

/// One fetch of `GET /v1/app/policy`, and the write that makes it survive the process.
///
/// ## When it runs, and why nothing waits for it
///
/// On the first read of [appPolicyProvider], which is the first frame — the same moment SHIP-168's
/// launch check fires, and for the same reasons. Nothing is awaited before `runApp`: `Docs/07` §4
/// calls working without signal the client's most important capability, and an app that will not
/// open on a loading dock is not that app.
///
/// A failure is Riverpod's to retry. `ProviderContainer.defaultRetry` re-runs a failed provider ten
/// times with exponential backoff, so a launch that starts in a lift gets its answer when the
/// signal returns — and while it is retrying the state is `AsyncLoading` **carrying an error**,
/// which is why [appPolicyProvider] reads the value rather than testing for `AsyncError`. The same
/// trap `version_gate.dart` documents.
///
/// ## It makes no request when nothing supplied a cache
///
/// Not an optimisation. `appPolicyCacheProvider` is empty in every widget test, and the nudge that
/// reads this policy is mounted inside `ShipperApp` — which nearly every widget test builds. A
/// provider that fetched on its own would have each of them open a connection to whatever base URL
/// the test binary was compiled with, which is the objection `queue_watch.dart` and
/// `version_gate.dart` both answered the same way.
///
/// The cache is the right seam for it rather than an arbitrary one: an application with nowhere to
/// keep an answer has no use for fetching it, because the whole value of this endpoint is that the
/// answer is still there on the launch that has no signal.
///
/// ## The write is not awaited by anything that applies the value
///
/// A device that could not write the file still applies what it fetched for this process. What it
/// loses is the *next* offline launch — which is the situation it was in before this ticket — so
/// the failure is swallowed rather than turned into a policy nobody can use.
final appPolicyRefreshProvider = FutureProvider<AppPolicy?>((ref) async {
  final cache = ref.watch(appPolicyCacheProvider);
  if (cache == null) return null;

  final policy = await ref.watch(appPolicyRepositoryProvider).fetch();

  if (policy.isUsable) {
    try {
      await cache.write(policy);
    } catch (_) {
      // Deliberately swallowed. See the note above.
    }
  }

  return policy;
});

/// The policy in force, as everything that applies one reads it.
///
/// A synchronous `Provider`, so a widget reads a number rather than an `AsyncValue` of one. There
/// is always an answer — that is what [compiledAppPolicy] is for — and a consumer forced to handle
/// "no policy yet" would invent a fourth case that does not exist.
final appPolicyProvider = Provider<AppPolicy>((ref) {
  // Matching on the value rather than on the absence of an error, in both cases. A failure being
  // retried is reported as `AsyncLoading` with an error attached, so "not an error" is not the same
  // as "answered" — see `version_gate.dart`, which pays for this distinction in the one case it was
  // written for.
  final fetched = switch (ref.watch(appPolicyRefreshProvider)) {
    AsyncData(:final value) => value,
    _ => null,
  };
  final cached = switch (ref.watch(cachedAppPolicyProvider)) {
    AsyncData(:final value) => value,
    _ => null,
  };

  return resolveAppPolicy(fetched: fetched, cached: cached);
});
