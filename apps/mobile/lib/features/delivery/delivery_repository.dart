import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/delivery/delivery_tracking.dart';

/// How a delivery is going, to either of the two parties to the job (SHIP-115, SHIP-115a).
///
/// ## The three reads are one shelf, and every path has five segments
///
/// `GET /v1/jobs/{id}/delivery` **cannot be served**: it and `GET /v1/jobs/open/{id}` both match
/// `/v1/jobs/open/delivery` with neither more specific, and Go's `ServeMux` panics at registration —
/// `make run` dies at startup rather than an endpoint answering oddly. So every read on this shelf is
/// `/delivery/<something>`, and that is a fact about the platform's router rather than a naming
/// choice this client may tidy.
///
/// ## Who may call them
///
/// **The customer who owns the job, and the provider whose bid they accepted.** Being either is
/// decided from the job rather than from a role claim, and anybody else gets `404`, identical to a
/// job that does not exist. So this client must not try to be more specific than the platform was:
/// there is one failure state for every reason, and the indistinguishability is the control.
///
/// **Being the owner does not require the job to have got anywhere.** `Service.partyTo` asks whether
/// the caller is the job's customer before it asks anything about status, so a customer reads this
/// shelf on a draft and gets an empty assignment and an empty milestone list rather than a refusal.
/// That is what lets the tracking screen be offered from every job without the device holding a copy
/// of `Docs/02` §2's table to decide when it is worth offering.
///
/// ## Nothing here is written, and nothing here is queued
///
/// Three reads. `core/queue` is for what a driver records with no signal; a customer looking at a
/// delivery is online or is not looking.
///
/// An interface with one real implementation, following the four repositories before it: a widget
/// test has to be able to hand a screen something that answers, and a stub transport under a
/// concrete class makes every screen test a test of `dio`'s wiring as well.
abstract interface class DeliveryRepository {
  /// `GET /v1/jobs/{id}/delivery/detail` — who is carrying this delivery, or that nobody is yet.
  ///
  /// A job with no driver answers `200` with `driver_assigned: false` and no other field. Branch on
  /// [DeliveryDriver.driverAssigned], never on a missing name.
  Future<DeliveryDriver> driver({required String jobId});

  /// `GET /v1/jobs/{id}/delivery/milestones` — every milestone anybody has recorded, newest first.
  ///
  /// ## This is the only place a milestone that moved nothing can be seen
  ///
  /// A driver who reaches a pickup, finds nobody there and sets off again records
  /// `en_route_to_pickup` twice (`Docs/02` §5); a queued update that syncs after the delivery has
  /// moved on is absorbed as history without moving the job backwards (SHIP-112). **Neither writes a
  /// status change**, so neither appears in anything derived from the job's status — including
  /// SHIP-77's timeline, which is derived from exactly that.
  ///
  /// ## Newest first by the actor's clock, which is deliberately not arrival order
  ///
  /// A batch recorded through the morning in a yard with no signal arrives all at once, and showing
  /// the sync order would show a customer a delivery that ran backwards.
  ///
  /// It pages, and `/delivery/proof` does not: `000601` has no uniqueness on `(job_id, milestone)`
  /// so a repeat is a legitimate outcome and this collection has no domain bound. [cursor] is opaque
  /// and one the endpoint did not issue is refused rather than misread.
  Future<ApiPage<RecordedMilestone>> milestones({required String jobId, String? cursor});

  /// `GET /v1/jobs/{id}/delivery/proof` — the evidence recorded on this delivery, newest acted
  /// first.
  ///
  /// **It does not page, and that is a fact about the tables rather than an omission.** A delivery
  /// has at most five recordable milestones and at most one photograph each, so the collection is
  /// bounded by the delivery: `has_more` is always `false` and `next_cursor` always `null`. The
  /// envelope is there so a client can tell a collection from a single resource without knowing the
  /// endpoint, and it is read through `ApiPage` for that reason and then flattened here — a `cursor`
  /// parameter on this method would be one no caller could ever usefully pass.
  ///
  /// ## Every URL in the answer is a credential with a clock on it
  ///
  /// `download_url` is signed **for this caller, after this request checked who they are**, and
  /// anybody holding it can fetch the image until it expires. **Do not cache this response.** A
  /// cached response is a cache of expiring links, and the URLs are minted per request — so asking
  /// again is the supported way to get fresh ones, and is what this client does.
  ///
  /// Both parties see exactly the same thing: a photograph is not a field that can be redacted, and
  /// the two of them are looking at the same object if they ever disagree about a delivery.
  Future<List<DeliveryProof>> proof({required String jobId});
}

/// The real one, over [ApiClient].
///
/// Thin by construction. `Docs/10` §8.1 replaces hand-written calls with a client generated from
/// `contracts/openapi.yaml`, and what should survive that is the shape of the interface above and
/// nothing in this class.
final class ApiDeliveryRepository implements DeliveryRepository {
  const ApiDeliveryRepository(this._client);

  final ApiClient _client;

  /// Product endpoints live under `/v1` (SHIP-13). The base URL carries the host and nothing else,
  /// so the version prefix belongs here.
  static String _shelf(String jobId) => '/v1/jobs/$jobId/delivery';

  @override
  Future<DeliveryDriver> driver({required String jobId}) async {
    return DeliveryDriver.fromJson(await _client.getJson('${_shelf(jobId)}/detail'));
  }

  @override
  Future<ApiPage<RecordedMilestone>> milestones({
    required String jobId,
    String? cursor,
  }) async {
    return ApiPage.fromJson(
      await _client.getJson(
        '${_shelf(jobId)}/milestones',
        query: <String, dynamic>{
          // No `limit`: the page size is server configuration (`Docs/10` §4.5), and a number
          // compiled in here could not be changed without a store release.
          if (cursor != null && cursor.isNotEmpty) 'cursor': cursor,
        },
      ),
      RecordedMilestone.fromJson,
    );
  }

  @override
  Future<List<DeliveryProof>> proof({required String jobId}) async {
    final page = ApiPage.fromJson(
      await _client.getJson('${_shelf(jobId)}/proof'),
      DeliveryProof.fromJson,
    );
    return page.data;
  }
}

/// The application's delivery-read repository.
final deliveryRepositoryProvider = Provider<DeliveryRepository>(
  (ref) => ApiDeliveryRepository(ref.watch(apiClientProvider)),
);
