/// Bidding — bids, counter-offers, negotiation, award (`Docs/07` §2).
///
/// ## What is here (SHIP-100)
///
/// `Docs/01` §4.2's first verb, and only the first: a provider offers to carry a job, for a price
/// and against two commitments about timing, over `POST /v1/jobs/{id}/bids` (SHIP-84).
///
/// - `bid.dart` — the `Bid` schema from `contracts/paths/bidding.yaml`, and `BidPlacement`, which
///   is an offer on its way *to* the platform.
/// - `bid_status.dart` — the eight bid states of `Docs/02` §4, in their wire form.
/// - `bidding_repository.dart` — the one endpoint a screen calls, and what is deliberately absent.
/// - `place_bid_controller.dart` — one offer, one idempotency key, and no queue.
/// - `place_bid_panel.dart` — the form, the platform's answer, and the offer it recorded.
///
/// ## Deliberately **not** offline-capable, and now enforced rather than stated
///
/// `Docs/07` §4 excludes bidding, awarding and negotiation from the durable queue: they are
/// competitive, time-sensitive and multi-party, and a stale local decision is worse than an honest
/// "you are offline". A price queued at a loading dock and sent four hours later is an offer against
/// a job that may since have been awarded, made by somebody who believes they have bid.
///
/// SHIP-124 made that structural rather than conventional. `core/queue`'s `OperationKind` has a
/// **private constructor** and exactly two members, `delivery.milestone` and `delivery.proof`, so
/// the set of queueable operations is closed by the compiler and a bid cannot be enqueued. Nothing
/// in this package imports `core/queue` or `core/sync`, and nothing should.
///
/// **What makes a retry safe instead** is the idempotency key: minted once where the provider acted
/// (`ActionKey`), reused unchanged only while the outcome of the previous attempt is genuinely
/// unknown, and stored by the platform against the bid it placed — `uq_bids_idempotency` on
/// `(job_id, provider_id, idempotency_key)`, which is a column rather than a cache, so a phone that
/// was out of signal for a day still gets its own offer back rather than a conflict.
///
/// ## Two privacy rules govern everything here, and they are not the same rule
///
/// **The customer's maximum never appears in a provider-facing shape** in any form — not an amount,
/// not a band, not a "budget supplied" flag (`Docs/01` §4.3). Nothing in this package reads a job at
/// all, so there is no field to withhold; `Bid` carries the job's identifier and nothing else of it.
/// That is stronger than redaction, and `budget_stays_on_the_customer_side_test.dart` holds the type
/// to a closed set of keys so a field arriving as `max_price` fails as surely as `budget_cents`.
///
/// **One provider never sees another's offer or its price.** Its failure mode is a `WHERE` clause
/// rather than a response field, which is why a closed key set cannot cover it — and why it is the
/// platform's to keep: every endpoint answers with the calling provider's own offer, and the
/// idempotency key is scoped to the caller as well as to the job.
///
/// ## What is not here
///
/// Revising, withdrawing, countering and reading a negotiation's history are **served and
/// deliberately not modelled**. An endpoint no screen calls is dead code that nothing holds to the
/// contract; each arrives with the screen that needs it — the provider's own bid list for the first
/// two, and the negotiation screens for the rest.
library;
