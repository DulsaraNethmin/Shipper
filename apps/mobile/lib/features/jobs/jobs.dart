/// Jobs — creation, publication, discovery, detail (`Docs/07` §2).
///
/// What is here, and the ticket each piece answers:
///
/// - `job.dart` — the `Job` schema from `contracts/paths/jobs.yaml`, and `AddressInput`, which is
///   an address on its way *to* the platform.
/// - `job_status.dart` — the twelve states of `Docs/02` §1, in their wire form.
/// - `jobs_repository.dart` — the five endpoints a customer screen calls.
/// - `job_locations_screen.dart` and its controller — the first step of publishing (SHIP-71).
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
