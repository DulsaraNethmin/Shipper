// SHIP-168 — what the launch check reads, and what it sends to read it.
//
// The response shape is `cmd/api/routes_app.go`'s `minimumVersionResponse`, and this is the one
// endpoint in the application where tolerating an unfamiliar body matters most: a build too old
// to be tolerant is exactly the build this endpoint exists to talk to.

import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/errors/api_failure.dart';
import 'package:shipper/core/version/minimum_version.dart';
import 'package:shipper/core/version/running_build.dart';
import 'package:shipper/core/version/version_gate.dart';

import 'version_fixture.dart';

void main() {
  group('decoding', () {
    test('reads a floor for each platform', () {
      final floors = MinimumVersion.fromJson(
        floorsBody(ios: 42, android: 17, iosStoreUrl: 'https://apps.apple.com/app/id123'),
      );

      expect(floors.ios.minimumBuild, 42);
      expect(floors.ios.storeUrl, 'https://apps.apple.com/app/id123');
      expect(floors.android.minimumBuild, 17);
    });

    test('tolerates a floor with no store_url, which is what the pilot sends', () {
      // `store_url` is `omitempty` on the Go struct and both variables default to empty, so the
      // key is genuinely absent rather than blank. A model that required it would fail to decode
      // the only response this endpoint currently gives.
      final floors = MinimumVersion.fromJson(floorsBody(ios: 42));

      expect(floors.ios.storeUrl, isNull);
      expect(floors.android.storeUrl, isNull);
    });

    test('ignores fields it does not know about', () {
      // Docs/07 §6: server changes stay additive and unknown fields must be tolerated, so an
      // added key needs no release. Every installed build depends on this holding.
      final floors = MinimumVersion.fromJson(<String, Object?>{
        ...floorsBody(ios: 42),
        'huawei': <String, Object?>{'minimum_build': 3},
        'notice': 'raised after the 3.2 defect',
      });

      expect(floors.ios.minimumBuild, 42);
    });

    test('a platform entry with no minimum_build decodes to a floor nothing is below', () {
      final floors = MinimumVersion.fromJson(<String, Object?>{
        'ios': <String, Object?>{},
        'android': <String, Object?>{},
      });

      expect(floors.ios.minimumBuild, 0);
    });
  });

  group('the request', () {
    ProviderContainer containerFor(StubAdapter adapter, {RunningBuild? build}) {
      final container = ProviderContainer(
        overrides: [
          runningBuildProvider.overrideWithValue(build),
          minimumVersionRepositoryProvider.overrideWithValue(repositoryOver(adapter)),
        ],
      );
      addTearDown(container.dispose);
      return container;
    }

    test('goes to /v1/app/minimum-version', () async {
      final adapter = StubAdapter.returning(floorsBody(ios: 42));
      final container = containerFor(adapter, build: const RunningBuild(
        platform: AppPlatform.ios,
        number: 1,
      ));

      final answer = await container.read(launchVersionCheckProvider.future);

      expect(answer?.ios.minimumBuild, 42);
      expect(adapter.requests.single.path, MinimumVersionRepository.path);
      expect(adapter.requests.single.method, 'GET');
    });

    test('carries no idempotency key, because it changes nothing', () {
      // SHIP-15 refuses a state-changing request without one and requires one of nothing else.
      final adapter = StubAdapter.returning(floorsBody());
      final container = containerFor(adapter, build: const RunningBuild(
        platform: AppPlatform.ios,
        number: 1,
      ));

      return container.read(launchVersionCheckProvider.future).then((_) {
        expect(adapter.requests.single.headers.containsKey('Idempotency-Key'), isFalse);
      });
    });

    test('travels on the client that carries no credential', () async {
      // Not a style choice. `cmd/api/routes_app.go` says the route is public because "a build old
      // enough to be blocked may be old enough that its authentication no longer works" — so
      // putting the launch check behind the session would leave exactly those builds unable to
      // discover that they must update. The stub is installed on the *unauthenticated* client's
      // transport, so the request arriving here is the whole proof.
      final adapter = StubAdapter.returning(floorsBody(ios: 42));
      final container = ProviderContainer(
        overrides: [
          runningBuildProvider.overrideWithValue(
            const RunningBuild(platform: AppPlatform.ios, number: 1),
          ),
        ],
      );
      addTearDown(container.dispose);
      container.read(unauthenticatedApiClientProvider).transport.httpClientAdapter = adapter;

      await container.read(launchVersionCheckProvider.future);

      expect(adapter.requests, hasLength(1));
      final sent = adapter.requests.single.headers;
      expect(sent.containsKey('Authorization'), isFalse); // spelling:ok — HTTP header, RFC 9110
    });

    test('is not made at all when nothing supplied a build number', () async {
      // The default in every widget test. A check that fired anyway would have each of them open
      // a connection to whatever base URL the test binary was compiled with.
      final adapter = StubAdapter.returning(floorsBody(ios: 42));
      final container = containerFor(adapter);

      expect(await container.read(launchVersionCheckProvider.future), isNull);
      expect(adapter.requests, isEmpty);
    });

    test('an unreachable platform leaves the check with no answer rather than a floor', () async {
      final adapter = StubAdapter.unreachable();
      final container = containerFor(
        adapter,
        build: const RunningBuild(platform: AppPlatform.ios, number: 1),
      );
      final subscription = container.listen(launchVersionCheckProvider, (_, _) {});
      addTearDown(subscription.close);

      await pumpEventQueue();

      expect(adapter.requests, isNotEmpty, reason: 'it did ask');
      expect(container.read(launchVersionCheckProvider).hasValue, isFalse);

      // The fail-open, at the seam where it is actually decided rather than in the pure function
      // alone. It is asserted through the state rather than through `.future` on purpose:
      // Riverpod retries a failed provider, so during a retry the future has not settled and the
      // state is `AsyncLoading` **carrying an error** — which is exactly the shape a fail-open
      // written as "block unless the state is an error" would get wrong.
      expect(container.read(updateVerdictProvider).blocked, isFalse);
    });

    test('the failure that reached the gate really was the transport failing', () async {
      // Guards the test above from passing for the wrong reason. If the request were never made,
      // or were answered, "not blocked" would still hold and would prove nothing.
      await expectLater(
        repositoryOver(StubAdapter.unreachable()).fetch(),
        throwsA(isA<ApiUnreachable>()),
      );
    });
  });
}
