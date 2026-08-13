import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/jobs/open_job.dart';

/// The marketplace as the calling provider may bid on it (SHIP-99).
///
/// **Separate from `JobsRepository` rather than a method on it**, and the separation is the same
/// one the platform made. `JobsRepository` is entirely about *the caller's own jobs* and its type
/// carries the customer's budget; this is about jobs somebody else published, and its type has no
/// field a budget could go in (`Docs/01` §4.3). One repository holding both would be one place a
/// screen could reach the wrong shape, which is exactly the arrangement the privacy rule is
/// hardest to keep.
///
/// The endpoint is served by `internal/fleet` even though its path begins `/v1/jobs` — `fleet`
/// owns eligibility and `jobs` owns the customer's view of a job — which is why the shape lives in
/// `contracts/paths/fleet.yaml`. On the client it is `features/jobs`, because `Docs/07` §2 puts
/// **discovery** in that feature.
///
/// An interface with one real implementation, following `JobsRepository` and `FleetRepository` for
/// the same reason: a widget test has to be able to hand a screen something that answers, and a
/// stub transport under a concrete class makes every screen test a test of `dio`'s wiring as well.
abstract interface class OpenJobsRepository {
  /// `GET /v1/jobs/open` (SHIP-82) — one page of the jobs this provider may bid on, newest first.
  ///
  /// [cursor] is the `next_cursor` of a previous page, passed back **exactly** as it arrived. It
  /// is opaque: its encoding is the endpoint's business, and a cursor from an encoding no longer
  /// served is refused rather than misread.
  ///
  /// ## There is no filter to send, and that is the platform's position rather than an omission
  ///
  /// The endpoint accepts `limit` and `cursor` and **nothing else**. The contract says so in as
  /// many words — "there is no parameter that widens this and none that narrows it — no state, no
  /// vehicle, no goods type" — because eligibility is the platform's decision and a filter
  /// parameter would be a second place for that answer to be argued with (`Docs/07` §3).
  ///
  /// So the narrowing this ticket's *Done when* asks for happens on the device, over what has been
  /// read, and the contract delegates it here explicitly: "a provider narrowing their own feed
  /// further is the client's business". `open_jobs_filter.dart` is that narrowing, and it can only
  /// ever **remove** jobs from a set the platform already decided this provider is eligible for —
  /// it cannot add one, which is the property that keeps it a convenience rather than a control.
  ///
  /// `limit` is absent for the reason it is absent everywhere else in this client: the page size is
  /// server configuration (`Docs/10` §4.5), and a number compiled in here is a number that cannot
  /// be changed without a store release.
  ///
  /// ## An empty page is an answer, not a failure
  ///
  /// A provider who is eligible for nothing gets `200` with `[]`. That covers an unverified
  /// account, one that has declared no service area, one with **no vehicle in service**, and a
  /// customer who followed a link meant for the other role. None of the four is a refusal, and the
  /// screen renders each of them from an empty list rather than from an error.
  ///
  /// No idempotency key: a read changes nothing, and the middleware lets read-only methods through
  /// untouched.
  Future<ApiPage<OpenJob>> openJobs({String? cursor});
}

/// The real one, over [ApiClient].
///
/// Thin by construction. `Docs/10` §8.1 replaces hand-written calls with a client generated from
/// `contracts/openapi.yaml`, and what should survive that is the shape of the interface above and
/// nothing in this class.
///
/// **`GET /v1/jobs/open/{id}` is deliberately not here.** SHIP-83 serves it and it is the shape
/// SHIP-100's provider job detail reads; modelling an endpoint no screen calls would be dead code
/// that nothing holds to the contract. It arrives with the screen that needs it.
final class ApiOpenJobsRepository implements OpenJobsRepository {
  const ApiOpenJobsRepository(this._client);

  final ApiClient _client;

  /// Product endpoints live under `/v1` (SHIP-13). The base URL carries the host and nothing else,
  /// so the version prefix belongs here.
  static const _base = '/v1/jobs/open';

  @override
  Future<ApiPage<OpenJob>> openJobs({String? cursor}) async {
    return ApiPage.fromJson(
      await _client.getJson(
        _base,
        query: <String, dynamic>{if (cursor != null && cursor.isNotEmpty) 'cursor': cursor},
      ),
      OpenJob.fromJson,
    );
  }
}

/// The application's open-jobs repository.
final openJobsRepositoryProvider = Provider<OpenJobsRepository>(
  (ref) => ApiOpenJobsRepository(ref.watch(apiClientProvider)),
);
