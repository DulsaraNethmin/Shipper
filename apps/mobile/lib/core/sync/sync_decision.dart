import 'package:shipper/core/errors/api_failure.dart';

/// What the sync worker does with one attempt's answer (SHIP-125).
///
/// **This enum is the ticket's most consequential decision, and it is wrong in two directions.**
/// Retrying something the platform will never take burns a driver's battery on a request that
/// cannot converge; quarantining something that failed transiently strands a real delivery update
/// on a handset until somebody notices. Neither failure announces itself — both look like a queue
/// that is simply taking a while.
enum SyncDecision {
  /// The platform has it. Remove the operation.
  accepted,

  /// Try again after a backoff, and go on to the next ordering key in this pass.
  ///
  /// The answer came *back from the platform*, which proves there is a route. Another job's
  /// operation may well succeed, and stopping here would let one job's problem stall another's —
  /// the head-of-line block that SHIP-124's per-key ordering exists to confine.
  retry,

  /// Try again after a backoff, and **end this pass**.
  ///
  /// For the failures that are about the link, the caller or the platform as a whole rather than
  /// about this operation. Nothing else in the queue would fare differently, so continuing is a
  /// radio wake-up per queued operation in exchange for nothing.
  retryAndPause,

  /// The platform saw it and will never take it. Quarantine it for a person (SHIP-132).
  refused,

  /// This build cannot send it at all. Quarantine it too, under a different reason.
  ///
  /// Not a platform answer — the request never got that far. A `StateError` from the idempotency
  /// guard, an operation kind this build has no sender for. Deterministic by construction, so a
  /// retry is a loop against a rule it can never satisfy; and quarantine is not a drop, because
  /// `Docs/02` §3.1's ladder is counting it and only a person removes it.
  unsupported,
}

/// Reads one failed attempt.
///
/// ## The table
///
/// | What came back | Decision | Why |
/// |---|---|---|
/// | no route, or a timeout | `retryAndPause` | The case the queue exists for. Nothing else will get out either |
/// | `401` | `retryAndPause` | A statement about the **credential**, never about the operation |
/// | `409 idempotency_request_in_progress` | `retry` | This operation's own earlier attempt is still running |
/// | `429 rate_limited` | `retryAndPause` | A statement about this **caller**, so every operation is equally refused |
/// | `503 service_unavailable` | `retryAndPause` | A dependency is down — including the idempotency store, which fails closed |
/// | `408`, other `5xx` | `retry` | The platform may even have committed; the stored key is what makes trying again safe |
/// | a response that was not the contract | `retry` | A proxy's error page says nothing about whether the service saw the request |
/// | any other `4xx` | `refused` | The platform understood it and said no. A retry can only be told no again |
/// | anything that is not an [ApiFailure] | `unsupported` | The request never left. See [SyncDecision.unsupported] |
///
/// ## The three awkward ones, which are where a plausible implementation goes wrong
///
/// **`401`, which a token refresh would fix.** It never reaches here untried: `AuthInterceptor`
/// (SHIP-50) already refreshes once and replays the original request — carrying the *same*
/// `Idempotency-Key`, because it replays the options object rather than rebuilding it. So a `401`
/// arriving here means no credential could be obtained *at that moment*, which on a handset is
/// most often a refresh that failed on the same dead link the operation did. Treating it as a
/// refusal would quarantine a driver's delivery update because a token expired in a tunnel. It is
/// also not a loop: the worker does not drain at all without a session, and a session that has
/// genuinely ended clears the queue.
///
/// **`409`, which is where a success hides.** A *replayed* success is not a `409` at all —
/// `httpx.Idempotent` replays the stored response byte for byte, so the second attempt of an
/// accepted milestone comes back `201` with `Idempotency-Replayed: true`, and the worker completes
/// it like any other success. What is genuinely a `409` is
/// `idempotency_request_in_progress`: the platform is *at this moment* running this operation's
/// own first attempt, which is as close to a success as a failure status gets. Refusing it would
/// quarantine an operation seconds before it was recorded. `409 conflict` is the opposite — "valid,
/// but it contradicts the current state" — and is precisely SHIP-132's case: a queued update that
/// lost to an administrative action, retained and shown rather than discarded (`Docs/02` §3.1).
///
/// **`422 delivery_milestone_not_permitted`, which will change meaning.** SHIP-111 refuses a late
/// milestone today and its own message tells the client to keep the update, because SHIP-112 makes
/// the platform absorb it instead. Quarantine is exactly "keep it": the operation stays in the
/// table with its key, counted and listed, until a person acknowledges it.
///
/// ## Why this is not `ActionKey.outcomeUnknown`, though it looks like it
///
/// That predicate answers "may I send this key again", and the answer turns on whether the
/// platform might already have acted. This one answers "will trying again ever help". The two
/// disagree in both directions, which is why they are two functions: a `429` means the platform
/// certainly did *not* act (`outcomeUnknown` false) and is the most retryable thing there is,
/// while a `400` leaves the outcome equally certain and is not retryable at all. Reusing one for
/// the other would look like removing a duplicate and would be a silent behaviour change.
SyncDecision decideFrom(Object failure) {
  return switch (failure) {
    ApiUnreachable() => SyncDecision.retryAndPause,
    ApiMalformedResponse() => SyncDecision.retry,
    ApiErrorResponse(code: 'idempotency_request_in_progress') => SyncDecision.retry,
    ApiErrorResponse(:final statusCode) when statusCode == 401 => SyncDecision.retryAndPause,
    ApiErrorResponse(:final statusCode) when statusCode == 429 => SyncDecision.retryAndPause,
    ApiErrorResponse(:final statusCode) when statusCode == 503 => SyncDecision.retryAndPause,
    ApiErrorResponse(:final statusCode) when statusCode == 408 => SyncDecision.retry,
    ApiErrorResponse(:final statusCode) when statusCode >= 500 => SyncDecision.retry,

    // Only a 4xx is a refusal somebody made. A 3xx, or a status the transport could not read at
    // all, is an unknown — and the safe direction for an unknown is to try again, because a
    // quarantine that turns out to be wrong strands a delivery until a person looks at it.
    ApiErrorResponse(:final statusCode) when statusCode >= 400 => SyncDecision.refused,
    ApiFailure() => SyncDecision.retry,

    _ => SyncDecision.unsupported,
  };
}
