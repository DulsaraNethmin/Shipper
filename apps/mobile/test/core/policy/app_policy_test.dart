// SHIP-167a — the model, the compiled default, and the read.
//
// The precedence rule is in `app_policy_resolution_test.dart`; this file is everything underneath
// it: what decodes, what is refused, what the compiled numbers are, and what reaches the wire.

import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/policy/app_policy.dart';

import 'policy_fixture.dart';

void main() {
  group('the model', () {
    test('decodes what the platform sends', () {
      final policy = AppPolicy.fromJson(policyBody(nudgeSeconds: 5400, budgetBytes: 524288));

      expect(policy.unsyncedNudgeAfterSeconds, 5400);
      expect(policy.unsyncedNudgeAfter, const Duration(minutes: 90));
      expect(policy.proofCompressionBudgetBytes, 524288);
    });

    test('ignores a field it has never heard of', () {
      // Docs/07 §6, and the reason it matters here as much as it does for the version floor: a
      // build too old to be tolerant would reject the whole answer over a third number it has no
      // use for, and fall back to values from whenever it was installed.
      final policy = AppPolicy.fromJson(<String, dynamic>{
        ...policyBody(nudgeSeconds: 60),
        'a_number_this_build_has_never_heard_of': 12,
      });

      expect(policy.unsyncedNudgeAfterSeconds, 60);
    });

    test('a missing field degrades to the compiled number rather than to a decode failure', () {
      final policy = AppPolicy.fromJson(<String, dynamic>{'unsynced_nudge_after_seconds': 60});

      expect(policy.unsyncedNudgeAfterSeconds, 60);
      expect(policy.proofCompressionBudgetBytes, compiledProofCompressionBudgetBytes);
    });

    test('a number no client could act on is not usable', () {
      // Not clamped. Clamping would apply a value nobody chose and hide the fault; refusing the
      // whole policy falls through to the previous rung, which is a value somebody did choose.
      expect(const AppPolicy(unsyncedNudgeAfterSeconds: 0).isUsable, isFalse);
      expect(const AppPolicy(unsyncedNudgeAfterSeconds: -1).isUsable, isFalse);
      expect(const AppPolicy(proofCompressionBudgetBytes: 0).isUsable, isFalse);
      expect(compiledAppPolicy.isUsable, isTrue);
    });
  });

  group('the compiled default', () {
    test('is Docs/02 §3.1 four hours and one mebibyte', () {
      expect(compiledAppPolicy.unsyncedNudgeAfter, const Duration(hours: 4));
      expect(compiledAppPolicy.proofCompressionBudgetBytes, 1024 * 1024);
    });

    // The two numbers exist twice on this side of the wire — once here, once as the default
    // argument of the class that consumes them — and they must agree, because a device that has
    // never been online gets whichever one happens to be reached first.
    //
    // Holding them together here rather than importing `NudgePolicy` and `ProofImagePolicy` into
    // this file would be the weaker test; those two are asserted in `app_policy_wiring_test.dart`,
    // where the providers that actually apply them are built.
    test('matches what deploy/.env.example configures the platform with', () {
      // UNSYNCED_NUDGE_AFTER=4h and PROOF_COMPRESSION_BUDGET_BYTES=1048576. A test rather than a
      // comment because these are two numbers in two languages in two directories, which is the
      // shape of every value that has ever drifted.
      expect(compiledUnsyncedNudgeAfterSeconds, 4 * 60 * 60);
      expect(compiledProofCompressionBudgetBytes, 1048576);
    });
  });

  group('the read', () {
    test('goes to /v1/app/policy and carries no credential', () async {
      final adapter = PolicyStubAdapter.returning(policyBody(nudgeSeconds: 60));

      await policyRepositoryOver(adapter).fetch();

      expect(adapter.requests, hasLength(1));
      expect(adapter.requests.single.path, '/v1/app/policy');
      expect(adapter.requests.single.method, 'GET');
      // The route is Public and nothing in the response is about the caller. A credential here
      // would be this client asserting a relationship the endpoint does not have — and would put
      // the read behind a session refresh on a device that may have no signal.
      expect(
        adapter.requests.single.headers.containsKey('Authorization'), // spelling:ok — HTTP header name, RFC 9110
        isFalse,
      );
    });

    test('an unreachable platform throws rather than inventing a policy', () async {
      // Deciding what to do about it belongs to `resolveAppPolicy`, which has the cache in front
      // of it. A repository that swallowed the failure and returned the compiled default would
      // have made the whole precedence rule unreachable.
      await expectLater(
        policyRepositoryOver(PolicyStubAdapter.unreachable()).fetch(),
        throwsA(anything),
      );
    });
  });
}
