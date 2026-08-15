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
/// The endpoint is served by `internal/fleet` — `fleet` owns eligibility and `jobs` owns the
/// customer's view of a job — which is why the shape lives in `contracts/paths/fleet.yaml` and why
/// SHIP-83a put the path under `/v1/fleet/jobs`. On the client it is `features/jobs`, because
/// `Docs/07` §2 puts **discovery** in that feature, and the two do not have to agree.
///
/// An interface with one real implementation, following `JobsRepository` and `FleetRepository` for
/// the same reason: a widget test has to be able to hand a screen something that answers, and a
/// stub transport under a concrete class makes every screen test a test of `dio`'s wiring as well.
abstract interface class OpenJobsRepository {
  /// `GET /v1/fleet/jobs` (SHIP-82) — one page of the jobs this provider may bid on, newest first.
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

  /// `GET /v1/fleet/jobs/{id}` (SHIP-83) — one job out of the feed, in the shape the feed sends.
  ///
  /// **The same type, and that is itself a privacy decision.** The platform answers both operations
  /// with `OpenJob` deliberately, and a Go test asserts the detail response is byte-identical to the
  /// feed entry: two shapes would be two places a budget field could be added and two responses a
  /// test would have to know to check. A "detail" view carrying a field or two more is exactly where
  /// somebody would later put "just a little more".
  ///
  /// ## A job this provider may not bid on is a job that does not exist
  ///
  /// `404`, with a body byte-identical to a job that is not there, and a message that says nothing
  /// about eligibility — a refusal that explained itself would disclose what the status code is
  /// withholding. That covers a job outside the service area, one no vehicle in service can carry,
  /// an unverified account, a job that is no longer open, **and the owning customer**, who is
  /// refused their own job here because `GET /v1/jobs/{id}` is where they read it.
  ///
  /// So a screen must not try to be more specific than the platform was. There is one failure state
  /// for every reason, and the indistinguishability is the control.
  ///
  /// The read is not cached and not passed down from the feed: a screen opened from a list fetched
  /// ten minutes ago shows what the platform holds now, and a deep link reaches the same screen with
  /// nothing extra to supply.
  Future<OpenJob> openJob({required String jobId});
}

/// The real one, over [ApiClient].
///
/// Thin by construction. `Docs/10` §8.1 replaces hand-written calls with a client generated from
/// `contracts/openapi.yaml`, and what should survive that is the shape of the interface above and
/// nothing in this class.
///
/// `GET /v1/fleet/jobs/{id}` arrived with SHIP-100, which is the screen that needed it. Until then it
/// was served and deliberately not modelled: an endpoint no screen calls is dead code that nothing
/// holds to the contract.
final class ApiOpenJobsRepository implements OpenJobsRepository {
  const ApiOpenJobsRepository(this._client);

  final ApiClient _client;

  /// Product endpoints live under `/v1` (SHIP-13). The base URL carries the host and nothing else,
  /// so the version prefix belongs here.
  ///
  /// **`/v1/fleet/jobs` since SHIP-83a, and the old `/v1/jobs/open` is gone rather than aliased.**
  /// The feed put a literal where the platform's `{id}` wildcard goes, which closed the whole
  /// four-segment `GET /v1/jobs/{id}/<literal>` space to every ticket after it. There is no fallback
  /// here on purpose: a client that quietly retried the old path would hide the very breakage the
  /// move is supposed to surface at build time.
  static const _base = '/v1/fleet/jobs';

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

  @override
  Future<OpenJob> openJob({required String jobId}) async {
    return OpenJob.fromJson(await _client.getJson('$_base/$jobId'));
  }
}

/// The application's open-jobs repository.
final openJobsRepositoryProvider = Provider<OpenJobsRepository>(
  (ref) => ApiOpenJobsRepository(ref.watch(apiClientProvider)),
);
