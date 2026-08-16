/// Bidding — bids, counter-offers, negotiation, award (`Docs/07` §2).
///
/// ## What is here (SHIP-100, SHIP-101, SHIP-102)
///
/// `Docs/01` §4.2's first verb, and what became of it: a provider offers to carry a job, for a price
/// and against two commitments about timing, over `POST /v1/jobs/{id}/bids` (SHIP-84) — and reads
/// back every offer in every negotiation they are in over `GET /v1/fleet/bids` (SHIP-101a).
///
/// - `bid.dart` — the `Bid` schema from `contracts/paths/bidding.yaml`, and `BidPlacement`, which
///   is an offer on its way *to* the platform.
/// - `bid_status.dart` — the eight bid states of `Docs/02` §4, in their wire form.
/// - `bidding_repository.dart` — the two endpoints a screen calls, and what is deliberately absent.
/// - `place_bid_controller.dart` — one offer, one idempotency key, and no queue.
/// - `place_bid_panel.dart` — the form, the platform's answer, and the offer it recorded.
/// - `my_bids_controller.dart` — the provider's own offers, and the two ways to group them.
/// - `my_bids_screen.dart` — the list, grouped by status, with no field a budget could be in.
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
/// contract; each arrives with the screen that needs it, and the negotiation screens (SHIP-103) are
/// where the last three belong. SHIP-101 deliberately did **not** take revise and withdraw with it:
/// both are writes that end or change a commitment somebody else is relying on, and both want a
/// confirmation flow rather than a button on a list.
///
/// ## The customer's half arrived at SHIP-102a, and its privacy rule is a different one
///
/// This paragraph used to say the customer's half "cannot be built", because no endpoint served it.
/// `GET /v1/jobs/{id}/bids/received` now does — at five segments, because `GET /v1/jobs/{id}/bids`
/// cannot be registered beside `GET /v1/jobs/open/{id}`.
///
/// - `received_offer.dart` — the `ReceivedOffer`, `ProviderSummary` and `VehicleSummary` schemas.
/// - `compare_offers_controller.dart` — one list per job, and sorting that is the client's.
/// - `compare_offers_screen.dart` — the cards, side by side, with no word a budget could hide in.
///
/// **The rule on that side is not `Bid`'s rule and does not inherit from it.** `Bid`'s guarantee is
/// structural — there is no job in the shape, so there is no budget to withhold. `ReceivedOffer`
/// crosses *providers*, so what one provider could learn about another through it is the question,
/// and the platform answers it by refusing a provider the byte-identical `404` a stranger gets
/// rather than by leaving anything out of the shape.
///
/// **And the third clause of `Docs/01` §4.3 needs a guard neither of the two above can be.** A
/// screen saying "the customer has set a maximum" carries no field and no value: it passes a closed
/// key set over the model and a source scan over the file. `compare_offers_test.dart` asserts on the
/// **words rendered**, which is what wave 9's finding cost to learn.
///
/// ## The negotiation arrived at SHIP-103, and it is one screen for two people
///
/// `Docs/01` §4.2's "negotiate" and `Docs/02` §4's alternating chain, with the conversation that a
/// price and two dates cannot carry.
///
/// - `message.dart` — the `Message` schema, and the only free text this domain receives.
/// - `bid.dart`'s `BidCounter` — a **difference**, not an offer, which is why it is a third type
///   rather than a nullable `BidPlacement`.
/// - `instant_field.dart` — the calendar and clock both forms enter a commitment through. It was
///   private to `place_bid_panel.dart` until a second caller needed it (`Docs/07` §2).
/// - `negotiation_controller.dart` — one state over the chain and the conversation, two idempotency
///   keys, and a counter that re-reads rather than reasons.
/// - `negotiation_screen.dart` — the rounds, the words, the composer and the counter form.
///
/// **One screen for both parties, where the job has two.** `jobDetail` and `openJobDetail` are
/// separate because the customer's job and a provider's view of it are different *responses* — one
/// carries the customer's private figure and the other must never be able to. A negotiation is the
/// opposite case: all four of its endpoints serve both sides and work out which from the credential,
/// so two screens would be two renderings of one exchange, disagreeing first about whose turn it is.
///
/// **And it is the most dangerous surface in this product for `Docs/01` §4.3**, because it renders
/// both parties' words beside a form for proposing a number. The third clause — no "budget supplied"
/// indicator — is a *sentence*, which carries no field, no value and no digit, and a closed key set
/// cannot see one. `negotiation_test.dart` therefore asserts on the **words rendered in the
/// provider's view**. Two numbers on that screen are legitimate and must not be confused with it:
/// the provider's own price, and the customer's counter amount, which the customer chose to put in
/// front of them.
///
/// What is still not here: revising your own offer (`PATCH …/{bid_id}`) and withdrawing it
/// (`POST …/withdraw`). Both end or change a commitment the other party is relying on, and both want
/// a confirmation flow of their own rather than a button on a list.
library;
