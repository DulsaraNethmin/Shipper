import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/jobs/job.dart';

/// The job endpoints a customer screen calls (SHIP-71, SHIP-76).
///
/// Every one of them is authenticated and every one is about **the caller's own jobs**. There is
/// no parameter anywhere for whose jobs to read or whose job to create: the owner is whoever the
/// token says is calling, which `Docs/07` §3 requires — a customer id in a request body would be
/// an authorisation decision made from client input.
///
/// An interface with one real implementation, following `IdentityRepository` for the same reason:
/// a widget test has to be able to hand a screen something that answers, and the alternative —
/// a stub transport under a concrete class — makes every screen test a test of `dio`'s wiring as
/// well as of the screen.
///
/// **Idempotency keys arrive from the caller and are never minted here.** `Docs/07` §4 has the
/// key generated once where the user acts and reused unchanged across every retry of that action;
/// a repository that minted its own would mint one per attempt. `ActionKey` is what callers use.
abstract interface class JobsRepository {
  /// `POST /v1/jobs` (SHIP-61).
  ///
  /// Creates a job in `draft`, owned by the calling customer. **Every field is optional**, so an
  /// empty body is a legitimate "start a job for me" — `Docs/01` §4.1 lets a customer save a
  /// draft and come back to it, and the app captures a job over several steps.
  ///
  /// A provider account is refused with `jobs_customer_only`, decided against the account's
  /// stored role rather than against the role claim in the token.
  Future<Job> createDraft({
    required Map<String, Object?> fields,
    required String idempotencyKey,
  });

  /// `PATCH /v1/jobs/{id}` (SHIP-62).
  ///
  /// A partial edit. Only the fields present are touched, which is what lets each step of the
  /// wizard save its own without sending back the ones it cannot see — a `PUT` would require the
  /// whole job, and a client that had not been updated for a new field would silently clear it.
  ///
  /// A job belonging to somebody else answers `404`, byte-identically to a job that does not
  /// exist. A job that has left `draft` answers `409` with `jobs_not_a_draft`.
  Future<Job> updateDraft({
    required String jobId,
    required Map<String, Object?> fields,
    required String idempotencyKey,
  });

  /// `GET /v1/jobs` (SHIP-66) — one page of the customer's own jobs, newest first.
  ///
  /// [cursor] is the `next_cursor` of a previous page, passed back **exactly** as it arrived. It
  /// is opaque: its encoding is the endpoint's business and a cursor from an encoding no longer
  /// served is refused rather than misread.
  ///
  /// ## There is deliberately no status filter and no limit here
  ///
  /// The endpoint takes `?status=` and `?limit=`, and this takes neither.
  ///
  /// `?status=` accepts **one** value per request. SHIP-76 shows the customer's jobs grouped by
  /// every status they have, so filtering would mean twelve requests to draw one screen; the
  /// contract says so itself and recommends reading the list once. The grouping is done on the
  /// device, where it costs nothing.
  ///
  /// `?limit=` is absent because the page size is server configuration (`Docs/10` §4.5). A number
  /// compiled into this client is a number that cannot be changed without a store release, which
  /// is exactly what `Docs/07` §1 puts server-side.
  ///
  /// No idempotency key: a read changes nothing, and the middleware lets read-only methods
  /// through untouched.
  Future<Page<Job>> jobs({String? cursor});
}

/// The real one, over [ApiClient].
///
/// Thin by construction. `Docs/10` §8.1 replaces hand-written calls with a client generated from
/// `contracts/openapi.yaml`, and what should survive that is the shape of the interface above and
/// nothing in this class.
final class ApiJobsRepository implements JobsRepository {
  const ApiJobsRepository(this._client);

  final ApiClient _client;

  /// Product endpoints live under `/v1` (SHIP-13). The base URL carries the host and nothing
  /// else, so the version prefix belongs here.
  static const _base = '/v1/jobs';

  @override
  Future<Job> createDraft({
    required Map<String, Object?> fields,
    required String idempotencyKey,
  }) async {
    return Job.fromJson(
      await _client.postJson(_base, idempotencyKey: idempotencyKey, body: fields),
    );
  }

  @override
  Future<Job> updateDraft({
    required String jobId,
    required Map<String, Object?> fields,
    required String idempotencyKey,
  }) async {
    return Job.fromJson(
      await _client.patchJson('$_base/$jobId', idempotencyKey: idempotencyKey, body: fields),
    );
  }

  @override
  Future<Page<Job>> jobs({String? cursor}) async {
    return Page.fromJson(
      await _client.getJson(
        _base,
        query: <String, dynamic>{if (cursor != null && cursor.isNotEmpty) 'cursor': cursor},
      ),
      Job.fromJson,
    );
  }
}

/// The body of the locations step (SHIP-71).
///
/// Built where a test can read it, so `jobs_repository_test.dart` can assert the exact keys
/// against `contracts/paths/jobs.yaml` without a transport. The platform **refuses unknown
/// fields** — `httpx.DecodeJSON` is strict — which makes the key names part of the contract in a
/// way a response's are not: a client that sends `drop_off` is told about the typo rather than
/// having the address silently ignored.
///
/// Both addresses are always present, and that is the shape rather than an oversight. An address
/// is replaced as a whole, so sending an empty one is how a customer clears it; omitting it means
/// "leave whatever is there alone", which is not what a form the customer has just emptied means.
Map<String, Object?> locationsBody({
  required AddressInput pickup,
  required AddressInput dropoff,
}) {
  return <String, Object?>{
    'pickup': pickup.toJson(),
    'dropoff': dropoff.toJson(),
  };
}

/// The application's jobs repository.
final jobsRepositoryProvider = Provider<JobsRepository>(
  (ref) => ApiJobsRepository(ref.watch(apiClientProvider)),
);
