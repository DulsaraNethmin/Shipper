// SHIP-167a's *Done when*, and the one clause it is easy to half-meet.
//
//   "the app caches the last response and applies it with no connection, falling back to a
//    compiled default only when it has never had one"
//
// **"With no connection" and "has never had one" are two different situations and only the second
// gets the compiled default.** A build that conflated them would pass an offline test, would look
// entirely correct in review, and would make the endpoint have no effect on the devices it exists
// for — because those devices are offline at the moment the number is used. Every test below that
// names the cache is there to make that build fail.
//
// The fixture is load-bearing and is written about in `policy_fixture.dart`: the cached policy
// differs from the compiled default **in both fields**, so an assertion that sees its values can
// only have got them from the cache. A fixture that cached four hours and one mebibyte would let
// the wrong build produce exactly the right answer.

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/policy/app_policy.dart';
import 'package:shipper/core/policy/app_policy_cache.dart';
import 'package:shipper/core/policy/app_policy_controller.dart';

import 'policy_fixture.dart';

/// The policy in force in a container wired the way this test decided.
Future<AppPolicy> policyIn(ProviderContainer container) async {
  // Both providers have to have settled, or the answer is whatever the first frame saw. Reading
  // the futures is how a plain container waits — there is nothing to pump.
  await container.read(cachedAppPolicyProvider.future);
  try {
    await container.read(appPolicyRefreshProvider.future);
  } catch (_) {
    // An unreachable platform is one of the cases under test, not a failure of the test.
  }
  return container.read(appPolicyProvider);
}

ProviderContainer containerWith({
  required AppPolicyCache? cache,
  PolicyStubAdapter? platform,
}) {
  final container = ProviderContainer(
    // Riverpod re-runs a failed provider ten times with exponential backoff by default, which is
    // right in production and is forty-five seconds of waiting in a test about being offline. One
    // attempt, and the retry behaviour itself is SHIP-168's to hold.
    retry: (_, _) => null,
    overrides: [
      appPolicyCacheProvider.overrideWithValue(cache),
      if (platform != null)
        appPolicyRepositoryProvider.overrideWithValue(policyRepositoryOver(platform)),
    ],
  );
  addTearDown(container.dispose);
  return container;
}

void main() {
  group('resolveAppPolicy — the table', () {
    test('the platform answered, so that is what applies', () {
      expect(
        resolveAppPolicy(fetched: cachedPolicyUnlikeTheDefault, cached: compiledAppPolicy),
        cachedPolicyUnlikeTheDefault,
      );
    });

    test('it did not, and this device has been told something before', () {
      // **The clause.** Not the compiled default — the cache.
      expect(
        resolveAppPolicy(fetched: null, cached: cachedPolicyUnlikeTheDefault),
        cachedPolicyUnlikeTheDefault,
      );
    });

    test('it did not, and this device has never been online', () {
      expect(resolveAppPolicy(fetched: null, cached: null), compiledAppPolicy);
    });

    test('a policy no client could act on falls one rung, not all the way', () {
      // A platform answering with a zero threshold leaves a device with a good cached policy using
      // the cached one. Filtering the result rather than each rung would have sent it to the
      // compiled default, discarding a value somebody actually chose.
      expect(
        resolveAppPolicy(
          fetched: const AppPolicy(unsyncedNudgeAfterSeconds: 0),
          cached: cachedPolicyUnlikeTheDefault,
        ),
        cachedPolicyUnlikeTheDefault,
      );

      expect(
        resolveAppPolicy(
          fetched: null,
          cached: const AppPolicy(proofCompressionBudgetBytes: 0),
        ),
        compiledAppPolicy,
      );
    });
  });

  group('the providers, end to end', () {
    test('a device with signal applies what the platform said, and keeps it', () async {
      final cache = FakeAppPolicyCache();
      final container = containerWith(
        cache: cache,
        platform: PolicyStubAdapter.returning(policyBody(nudgeSeconds: 1800, budgetBytes: 262144)),
      );

      final policy = await policyIn(container);

      expect(policy.unsyncedNudgeAfter, const Duration(minutes: 30));
      expect(policy.proofCompressionBudgetBytes, 262144);
      // "Caches the last response" — the half of the clause that makes the next launch work.
      expect(cache.writes, [policy]);
    });

    test('a device with no connection applies what it was told last time', () async {
      // **This is the test the mutation has to fail.** The device has been online before, is
      // offline now, and must use the cached numbers — not the compiled ones.
      final container = containerWith(
        cache: FakeAppPolicyCache(cachedPolicyUnlikeTheDefault),
        platform: PolicyStubAdapter.unreachable(),
      );

      final policy = await policyIn(container);

      expect(
        policy,
        cachedPolicyUnlikeTheDefault,
        reason: 'a device that has been told a policy applies it when it is offline; the compiled '
            'default is for an install that has never once been online, and nothing else',
      );
      expect(
        policy,
        isNot(compiledAppPolicy),
        reason: 'falling back to the compiled default because there is no connection is the '
            'half-meeting of the Done when clause: it makes the endpoint have no effect on the '
            'devices it exists for, because those devices are offline when the value is used',
      );
      // Named rather than implied, so a reader can see the two numbers are genuinely different.
      expect(policy.unsyncedNudgeAfter, const Duration(minutes: 30));
      expect(policy.proofCompressionBudgetBytes, 256 * 1024);
    });

    test('an install that has never been online uses the compiled default', () async {
      final container = containerWith(
        cache: FakeAppPolicyCache(),
        platform: PolicyStubAdapter.unreachable(),
      );

      expect(await policyIn(container), compiledAppPolicy);
    });

    test('a cache it could not write to still applies what it fetched', () async {
      // The device loses its *next* offline launch, which is the situation it was in before this
      // ticket. Turning the write failure into a discarded policy would make a full disk cost the
      // value it just successfully fetched.
      final cache = FakeAppPolicyCache()..failWrites = true;
      final container = containerWith(
        cache: cache,
        platform: PolicyStubAdapter.returning(policyBody(nudgeSeconds: 1800)),
      );

      final policy = await policyIn(container);

      expect(policy.unsyncedNudgeAfter, const Duration(minutes: 30));
      expect(cache.writes, isEmpty);
    });

    test('an unusable answer is not written over a good cached one', () async {
      final cache = FakeAppPolicyCache(cachedPolicyUnlikeTheDefault);
      final container = containerWith(
        cache: cache,
        platform: PolicyStubAdapter.returning(policyBody(nudgeSeconds: 0)),
      );

      expect(await policyIn(container), cachedPolicyUnlikeTheDefault);
      expect(
        cache.writes,
        isEmpty,
        reason: 'a policy the client will not apply must not replace the one it is applying, or a '
            'single bad deploy would leave every handset on the compiled default for good',
      );
    });

    test('nothing is asked of the platform until something supplies a cache', () async {
      // What keeps SHIP-167a free for the rest of the suite. `ShipperApp` is built by nearly every
      // widget test, the nudge inside it reads this policy, and a provider that fetched on its own
      // would have each of them open a connection to whatever base URL the binary was compiled
      // with — the objection queue_watch.dart and version_gate.dart both answered this way.
      final adapter = PolicyStubAdapter.returning(policyBody(nudgeSeconds: 1));
      final container = containerWith(cache: null, platform: adapter);

      expect(await policyIn(container), compiledAppPolicy);
      expect(adapter.requests, isEmpty);
    });
  });
}
