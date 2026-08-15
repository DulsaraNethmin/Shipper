// SHIP-167a as the application actually applies it.
//
// `app_policy_resolution_test.dart` proves the precedence rule. That is not the *Done when*: it
// says the app **applies** the policy, and a resolved value nothing reads applies to nobody. So
// this file builds the two providers that consume it and checks what they produce — and holds the
// compiled numbers against the defaults the two consuming classes carry, which is the pair most
// likely to drift once nobody is looking at both files at once.

import 'dart:io';

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/policy/app_policy.dart';
import 'package:shipper/core/policy/app_policy_cache.dart';
import 'package:shipper/core/policy/app_policy_controller.dart';
import 'package:shipper/core/sync/unsynced_nudge.dart';
import 'package:shipper/features/delivery/capture_proof_controller.dart';
import 'package:shipper/features/delivery/proof_image.dart';

import 'policy_fixture.dart';

ProviderContainer containerWith({AppPolicyCache? cache, PolicyStubAdapter? platform}) {
  final container = ProviderContainer(
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

Future<void> settle(ProviderContainer container) async {
  await container.read(cachedAppPolicyProvider.future);
  try {
    await container.read(appPolicyRefreshProvider.future);
  } catch (_) {
    // Being offline is a case under test.
  }
}

void main() {
  group('the nudge threshold', () {
    test('follows the platform', () async {
      final container = containerWith(
        cache: FakeAppPolicyCache(),
        platform: PolicyStubAdapter.returning(policyBody(nudgeSeconds: 1800)),
      );
      await settle(container);

      expect(container.read(nudgePolicyProvider).after, const Duration(minutes: 30));
    });

    test('and follows the cache when there is no connection', () async {
      // The whole point of the ticket, at the provider that actually decides whether a driver is
      // prompted. This is the value read on a handset in a valley, which is the only kind of
      // handset the prompt ever fires on.
      final container = containerWith(
        cache: FakeAppPolicyCache(cachedPolicyUnlikeTheDefault),
        platform: PolicyStubAdapter.unreachable(),
      );
      await settle(container);

      expect(container.read(nudgePolicyProvider).after, const Duration(minutes: 30));
      expect(container.read(nudgePolicyProvider).after, isNot(const Duration(hours: 4)));
    });

    test('and is Docs/02 §3.1 four hours on an install that has never been online', () async {
      final container = containerWith(cache: null);
      await settle(container);

      expect(container.read(nudgePolicyProvider).after, const Duration(hours: 4));
    });
  });

  group('the compression budget', () {
    test('follows the platform', () async {
      final container = containerWith(
        cache: FakeAppPolicyCache(),
        platform: PolicyStubAdapter.returning(policyBody(budgetBytes: 262144)),
      );
      await settle(container);

      expect(container.read(proofImagePolicyProvider).maxBytes, 262144);
    });

    test('and follows the cache when there is no connection', () async {
      final container = containerWith(
        cache: FakeAppPolicyCache(cachedPolicyUnlikeTheDefault),
        platform: PolicyStubAdapter.unreachable(),
      );
      await settle(container);

      expect(container.read(proofImagePolicyProvider).maxBytes, 256 * 1024);
    });

    test('and the pixels stay compiled in, whatever the platform says', () async {
      // `longestEdge` and the quality ladder are a legibility judgement about a licence plate
      // photographed from two metres (Docs/01 §4.4), not an operations dial. Serving them would
      // let a deployment trade the evidence for bytes silently, which is the one thing the proof
      // photograph exists not to do.
      final container = containerWith(
        cache: FakeAppPolicyCache(),
        platform: PolicyStubAdapter.returning(policyBody(budgetBytes: 262144)),
      );
      await settle(container);

      const compiled = ProofImagePolicy();
      expect(container.read(proofImagePolicyProvider).longestEdge, compiled.longestEdge);
      expect(container.read(proofImagePolicyProvider).qualities, compiled.qualities);
    });
  });

  group('the two sets of compiled numbers agree', () {
    // Three files hold "four hours" and three hold "one mebibyte": the two consuming classes, the
    // policy's own constants, and deploy/.env.example. The last is the platform's and is checked
    // in app_policy_test.dart; these two are the ones a device that has never been online reaches,
    // and whichever it reaches first has to be the same number.
    test('the nudge default matches the compiled policy', () {
      expect(const NudgePolicy().after, compiledAppPolicy.unsyncedNudgeAfter);
    });

    test('the proof budget default matches the compiled policy', () {
      expect(const ProofImagePolicy().maxBytes, compiledAppPolicy.proofCompressionBudgetBytes);
    });
  });

  test('main.dart supplies the policy cache, which is the whole of the production wiring', () {
    // A source assertion rather than a call, for the reason SHIP-126's and SHIP-168's are: `main()`
    // calls `runApp` and reaches a platform channel, which a host test cannot do. What can be
    // checked is that the override is there at all — and its absence is a build that runs on the
    // compiled four hours forever, makes no request, writes no cache, and fails no test.
    final source = File('lib/main.dart').readAsStringSync();

    expect(
      source,
      contains('appPolicyCacheProvider.overrideWithValue'),
      reason: 'lib/main.dart no longer supplies appPolicyCacheProvider. Without it the client '
          'policy endpoint is never called and the compiled defaults apply forever, whatever the '
          'platform is configured with — see core/policy/policy.dart.',
    );
  });
}
