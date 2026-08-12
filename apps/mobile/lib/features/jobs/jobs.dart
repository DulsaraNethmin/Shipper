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
/// amount, a band, or a "budget supplied" flag. Every screen in this package today is a customer
/// screen, which is what makes `Job.budgetCents` legitimate — and the moment a provider screen
/// exists (SHIP-82, SHIP-83) it reads a different type rather than this one with a field skipped.
/// `test/features/jobs/budget_stays_on_the_customer_side_test.dart` is what keeps that honest,
/// and it is the client's half of the platform's own serialisation test.
library;
