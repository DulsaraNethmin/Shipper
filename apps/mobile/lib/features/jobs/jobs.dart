/// Jobs — creation, publication, discovery, detail (`Docs/07` §2).
///
/// What is here, and the ticket each piece answers:
///
/// - `job.dart` — the `Job` schema from `contracts/paths/jobs.yaml`, and `AddressInput`, which is
///   an address on its way *to* the platform.
/// - `job_status.dart` — the twelve states of `Docs/02` §1, in their wire form.
/// - `jobs_repository.dart` — the six endpoints a customer screen calls.
/// - `job_locations_screen.dart` and its controller — the first step of publishing (SHIP-71). It
///   *creates* the draft; `job_locations_edit_screen.dart` edits one that exists (SHIP-75), which
///   is the whole of the difference between starting a job and coming back to one.
/// - `address_section.dart` — the eight inputs both of those draw, and the argument for why none
///   of them validates on the device.
/// - `job_wizard.dart` — the four steps of describing a delivery, and the chrome they share.
/// - `job_draft_controller.dart` — the spine of the wizard: one draft, read once and edited a
///   step at a time. Every step after the first uses it, because they are all the same operation.
/// - `goods_category.dart` — the `GoodsCategory` schema, and the catalogue as one response.
/// - `goods_categories_repository.dart` — `GET /v1/goods-categories` (SHIP-58). Its own
///   repository rather than a method on `jobs_repository.dart`, because it is public,
///   unauthenticated, and describes no job.
/// - `job_goods_screen.dart` — the second step: what is being moved (SHIP-72).
/// - `job_schedule_screen.dart` — the third step: when it moves and what it needs (SHIP-73).
/// - `job_review_screen.dart` — the fourth step: the optional budget, the whole job read back, and
///   `POST /v1/jobs/{id}/publish` (SHIP-74). The end of the journey the marketplace waited on.
/// - `date_field.dart` — a date-only picker. `features/bidding`'s `InstantField` is the same shape
///   and the wrong control: a bid promises a time, and a job's window only constrains a day.
/// - `customer_job_list.dart` and its controller — the customer's own jobs (SHIP-76).
/// - `job_detail_screen.dart` and its controller — one delivery in full (SHIP-77).
/// - `job_timeline.dart` — where a job has reached, derived from its status because the
///   per-transition history is recorded in the database and served by no endpoint.
/// - `job_actions.dart` — what the platform would permit on a job right now, which the app uses
///   to decide what is worth offering and never to decide anything else.
/// - `open_job.dart` — the `OpenJob` schema from `contracts/paths/fleet.yaml`: a job as a
///   *provider* sees it, which is a second type rather than `Job` with a field hidden.
/// - `open_jobs_repository.dart` — `GET /v1/fleet/jobs`, the provider's eligible feed (SHIP-82).
/// - `open_jobs_filter.dart` — the narrowing a provider applies to their own feed, which the
///   endpoint deliberately accepts no parameter for.
/// - `provider_job_feed.dart` and its controller — the provider half of the shell (SHIP-99).
/// - `open_job_screen.dart` and its controller — one job as a provider deciding whether to bid
///   sees it (SHIP-100), over `GET /v1/fleet/jobs/{id}`.
///
/// **Both halves of the marketplace are in this package, and they meet nowhere.** `Docs/07` §2
/// puts *discovery* in `jobs` and the feed is discovery, so the provider's screens live beside the
/// customer's rather than in `fleet` — where the endpoint's own domain is, or in `bidding`, where
/// the bid they lead to lives. What keeps them apart is that they share no type and no widget: two
/// response shapes, two cards, two controllers, two detail screens, and the rule below is why.
///
/// **The bid form is not in this package and is not imported by it.** Reviewing a job and offering
/// to carry it is one thing a provider does and two features' work, and features do not import one
/// another. `open_job_screen.dart` declares that it needs a panel; `core/routing/app_router.dart`
/// supplies `features/bidding`'s. That is the composition-root arrangement the Go side uses for a
/// domain and its adapters, which meet in `cmd/api` and nowhere else.
///
/// ## Three rules this package is held to
///
/// **Status is read and never set** (`Docs/02` §2, `CLAUDE.md`). Every transition passes one
/// guarded server-side function, and there is no `status` field in any request body in the API —
/// one that arrived would be refused as an unknown field rather than quietly ignored.
///
/// **The twelve status names are generated eventually, and typed here meanwhile.** `Docs/10` §8.2
/// makes `contracts/statuses.yaml` the source for Go, Dart and TypeScript at SHIP-56a. That file
/// does not exist yet, so `job_status.dart` is the Dart copy, written to be replaced.
///
/// **The customer's budget never reaches a provider's device** (`Docs/01` §4.3). Not as an
/// amount, a band, or a "budget supplied" flag. `Job.budgetCents` is legitimate because every
/// screen that reads `Job` is the owner's own — and **the provider screen this package now holds
/// reads `OpenJob` instead**, a type with no field a budget could go in, exactly as the platform
/// writes a second response shape rather than redacting the first.
/// `test/features/jobs/budget_stays_on_the_customer_side_test.dart` is what keeps that honest,
/// and it is the client's half of the platform's own serialisation test.
library;
