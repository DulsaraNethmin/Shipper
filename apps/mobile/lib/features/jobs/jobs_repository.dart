import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'package:shipper/core/api/api_client.dart';
import 'package:shipper/core/api/page.dart';
import 'package:shipper/features/jobs/date_field.dart';
import 'package:shipper/features/jobs/job.dart';

/// The job endpoints a customer screen calls (SHIP-71, SHIP-76, SHIP-77).
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
  Future<ApiPage<Job>> jobs({String? cursor});

  /// `GET /v1/jobs/{id}` (SHIP-65) — one job, in full, to the customer who owns it.
  ///
  /// **The same shape the list returns**, deliberately: the contract answers create, edit, read
  /// and list with one `Job` schema so a client parses one type whatever it did to obtain the
  /// job. A "detail" shape carrying a field or two more would make every other response a subset
  /// every screen has to special-case.
  ///
  /// A job belonging to somebody else answers `404`, byte-identically to a job that does not
  /// exist. That is the platform's decision and this client neither softens nor explains it.
  Future<Job> job({required String jobId});

  /// `POST /v1/jobs/{id}/publish` (SHIP-63) — makes the caller's draft visible to providers.
  ///
  /// **A verb under the resource, because status is never a settable field** (`Docs/02` §2,
  /// `CLAUDE.md`). The client names an intent; the platform decides what the status becomes.
  ///
  /// [acceptsTerms] is the declaration `Docs/04` §2 requires **for every job** rather than once
  /// per account — it is about these goods, made by a customer who has just described them. It is
  /// sent as what the customer actually did: `false` is refused with `jobs_terms_not_accepted`
  /// rather than ignored, because a client sending it has said the customer declined and a `200`
  /// would record an acceptance nobody made.
  ///
  /// The refusals want different responses from a screen and are worth naming here:
  /// `jobs_prohibited_category` (422) — Shipper does not carry these goods, quoted in the
  /// catalogue's own words; `jobs_customer_not_verified` (403) — the email address or the phone
  /// number is outstanding; `validation_failed` (422) — a required field is missing, every one of
  /// them listed at once; `jobs_not_publishable` (409) — the job is not a draft.
  ///
  /// **Publishing a job that is already open answers `200`** and records nothing further, which is
  /// a phone that lost its connection, restarted and generated a fresh key for the same intent.
  Future<Job> publish({
    required String jobId,
    required bool acceptsTerms,
    required String idempotencyKey,
  });

  /// `POST /v1/jobs/{id}/cancel` (SHIP-64) — ends a job the caller owns, before it is awarded.
  ///
  /// **A verb under the resource, because status is never a settable field** (`Docs/02` §2,
  /// `CLAUDE.md`). The client names an intent; the platform decides what the status becomes, and
  /// answers with the job as it now is.
  ///
  /// `Docs/02` §2 permits `draft → cancelled` and `open`/`negotiating → cancelled` and nothing
  /// else from a customer, so once a bid has been awarded the answer is `jobs_not_cancellable`
  /// and a `409` — a refusal a screen renders rather than one it pre-empts.
  ///
  /// Cancelling a job that is already cancelled answers `200`, records nothing further, and is
  /// not an error: that is a phone which lost its connection, restarted, and generated a fresh
  /// key for the same intent.
  ///
  /// [reason] is optional and omitted when empty. `Docs/01` §3 requires a reason only of an
  /// administrator; a customer abandoning their own draft owes nobody an explanation.
  Future<Job> cancel({
    required String jobId,
    String reason,
    required String idempotencyKey,
  });
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
  Future<ApiPage<Job>> jobs({String? cursor}) async {
    return ApiPage.fromJson(
      await _client.getJson(
        _base,
        query: <String, dynamic>{if (cursor != null && cursor.isNotEmpty) 'cursor': cursor},
      ),
      Job.fromJson,
    );
  }

  @override
  Future<Job> job({required String jobId}) async {
    return Job.fromJson(await _client.getJson('$_base/$jobId'));
  }

  @override
  Future<Job> publish({
    required String jobId,
    required bool acceptsTerms,
    required String idempotencyKey,
  }) async {
    return Job.fromJson(
      await _client.postJson(
        '$_base/$jobId/publish',
        idempotencyKey: idempotencyKey,
        body: publicationBody(acceptsTerms: acceptsTerms),
      ),
    );
  }

  @override
  Future<Job> cancel({
    required String jobId,
    String reason = '',
    required String idempotencyKey,
  }) async {
    return Job.fromJson(
      await _client.postJson(
        '$_base/$jobId/cancel',
        idempotencyKey: idempotencyKey,
        body: cancellationBody(reason: reason),
      ),
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

/// The body of the goods step (SHIP-72).
///
/// Six fields, all of them always sent, for the reason [locationsBody] sends both addresses: the
/// step owns these six and a customer who emptied a box means "clear it", which is not what
/// omitting the key means to a `PATCH`. The contract spells the clearing values out — the empty
/// string for [category] and [description], `0` for every measurement — so a blank input is sent
/// as the value that empties the field rather than left out.
///
/// [category] is a **code** from `GET /v1/goods-categories`, never a label. The label is wording
/// and changes; the code is what the job stores.
///
/// **A category Shipper does not carry is accepted here and refused at publication** (SHIP-58,
/// SHIP-59). `Docs/01` §4.1 lets a customer save a half-finished job and come back, and somebody
/// sketching a delivery may not have worked out yet that it will not be taken.
Map<String, Object?> goodsBody({
  required String category,
  required String description,
  int? lengthCm,
  int? widthCm,
  int? heightCm,
  double? weightKg,
}) {
  return <String, Object?>{
    'goods_category': category,
    'goods_description': description,
    'length_cm': lengthCm ?? 0,
    'width_cm': widthCm ?? 0,
    'height_cm': heightCm ?? 0,
    'weight_kg': weightKg ?? 0,
  };
}

/// The body of the schedule and vehicle step (SHIP-73).
///
/// ## Both windows are always sent, and either end may be empty
///
/// The step owns `pickup_window`, `dropoff_window`, `vehicle_requirement` and `handling_notes`, so
/// all four keys go on every save for the reason [locationsBody] sends both addresses: a `PATCH`
/// leaves out what it is not given, and a customer who cleared the latest collection date meant to
/// clear it. The contract names the empty string as the clearing value for each end of a window,
/// so a cleared date is sent as `''` rather than dropped.
///
/// ## The day is turned into an instant here, and which instant depends on the edge
///
/// The form collects days, because a job's window is a constraint rather than a commitment. The
/// contract takes RFC 3339 instants. `dayStart` opens a window at the first moment of the chosen
/// day and `dayEnd` closes it at the last — a window closed at midnight *on* its day would end
/// before that day had happened, which is a delivery refused for a reason nobody typed.
///
/// **Only the start of the pickup window is required to publish**, which is
/// `internal/jobs/publish.go`'s decision and not this function's: a customer who knows the
/// earliest date they can release the goods has said something a provider can plan around, and
/// being unable to name the latest is not a reason to refuse the job.
Map<String, Object?> scheduleBody({
  DateTime? pickupFrom,
  DateTime? pickupTo,
  DateTime? dropoffBy,
  String vehicleRequirement = '',
  String handlingNotes = '',
}) {
  return <String, Object?>{
    'pickup_window': <String, Object?>{
      'start': pickupFrom == null ? '' : dayStart(pickupFrom),
      'end': pickupTo == null ? '' : dayEnd(pickupTo),
    },
    'dropoff_window': <String, Object?>{
      // No `start`: for most road transport the drop-off is a consequence of the pickup rather
      // than an independent constraint, so the form asks "by when" and nothing else. The key is
      // still sent as an empty string, because omitting it would leave a value a customer had
      // once set and then cleared.
      'start': '',
      'end': dropoffBy == null ? '' : dayEnd(dropoffBy),
    },
    'vehicle_requirement': vehicleRequirement,
    'handling_notes': handlingNotes,
  };
}

/// The body of the budget step (SHIP-74).
///
/// One key, always sent, and `0` is how a customer clears a budget they had set — which the
/// contract names explicitly and which is why this is a function rather than a literal at the call
/// site.
///
/// **In cents, never dollars** (`Docs/10` §3.3). Money is never a float and a JSON number written
/// as `1500.50` is one; `centsFromAud` does the conversion on the text rather than through a
/// `double`, so no amount is ever a cent adrift from what somebody typed.
///
/// **Never disclosed to a provider, in any form** (`Docs/01` §4.3). Not as an amount, not as a
/// band, and not as a "budget supplied" flag. It travels to the platform on the customer's own
/// request and comes back only on the customer's own view of the job.
Map<String, Object?> budgetBody({int? cents}) {
  return <String, Object?>{'budget_cents': cents ?? 0};
}

/// The body of a publication (SHIP-63, SHIP-74).
///
/// One field, and the contract marks it required: `Docs/04` §2 asks for the terms and the goods
/// declaration "for every job", so it is asked every time rather than remembered against the
/// account. The declaration is about *these* goods, made by a customer who has just described
/// them.
///
/// The value sent is what the customer actually did. `false` is refused with
/// `jobs_terms_not_accepted` rather than quietly ignored — a client that sent it has said the
/// customer declined, and answering `200` would record an acceptance nobody made.
Map<String, Object?> publicationBody({required bool acceptsTerms}) {
  return <String, Object?>{'accepts_terms': acceptsTerms};
}

/// The body of a cancellation (SHIP-77).
///
/// **An empty object is a complete request**, which is what the contract says and why this is a
/// function rather than a literal at the call site: `{}` reads like an oversight and is not one.
/// The API takes a JSON body on every state-changing request, and one endpoint excepted from that
/// would be a second answer to what a request looks like.
///
/// An empty [reason] is **omitted rather than sent as `""`**. The platform records the reason
/// against the transition in the job's history for support to read, and an empty string there is
/// a reason somebody gave rather than one nobody was asked for.
Map<String, Object?> cancellationBody({String reason = ''}) {
  return <String, Object?>{if (reason.isNotEmpty) 'reason': reason};
}

/// The application's jobs repository.
final jobsRepositoryProvider = Provider<JobsRepository>(
  (ref) => ApiJobsRepository(ref.watch(apiClientProvider)),
);
