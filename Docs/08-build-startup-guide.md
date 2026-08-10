# Shipper — Build Startup Guide

**Status:** Draft  
**Audience:** Engineering  
**Purpose:** Take the project from planning documents to running code, in an order that avoids rework.

The repository currently contains documentation only. This guide defines what to do first, what to defer, and what must be settled before code is written at all.

## Step 0 — Settled before writing code

These questions were open because **code written before they closed would be rebuilt**. They are now decided. Each is recorded in its source document; this table is the index.

| Decision | Position | Source |
|---|---|---|
| Customer's maximum budget | **Private.** Never shown to providers in any form — not as an amount, band, or flag. Used only as a customer-side filter | `01` §4.3 |
| Completion confirmation window | **72 hours** after Delivered, then auto-complete if no dispute | `02` §6 |
| Photo proof of delivery | **Mandatory**, with a reasoned exception path that flags the job for review | `01` §4.4 |
| Awarded job when the provider cancels | **Returns to Open with all bids cleared**, between Awarded and Picked up only | `02` §6 |
| Maximum time a job stays Open | Earlier of **14 days** from publication, or the job's pickup date passing | `02` §6 |
| Provider verification evidence | **Collected and reviewed by an administrator by eye.** No third-party verification integration in MVP | `04` §3 |
| Account deletion versus audit | **Delete the person, retain the transaction** under a stable pseudonym | `05` §3 |
| Driver portal link delivery | **Provider forwards it.** No SMS integration in MVP, but the driver's name and mobile are captured at assignment | `01` §4.5 |
| Unsynced-milestone handling | In-app indicator immediately, provider nudge at **4 hours**, operations alert at **24 hours** | `02` §3.1 |
| Pilot geography | **One metropolitan area plus a ~150 km radius**, within a single state | `01` §8 |

### Two decisions carry a design consequence worth stating

**Private budget is a one-way door.** If providers learn to bid at the customer's maximum, that behaviour cannot be untaught — the anchor persists even if the signal is later removed. Starting private preserves the option to add a signal if bid quality proves poor. Starting visible does not. Watch time-to-first-bid and the share of jobs receiving a bid; those are the metrics that would justify revisiting it.

**Mandatory proof needs its exception path built at the same time**, not after. A denied camera permission, a recipient who objects to being photographed, or an unlit loading dock must not leave a driver unable to complete a job. The exception is a reasoned selection that records why proof is missing and flags the job for review — see `01` §4.4.

### Still open — does not block Step 1 or Step 2

| Question | Owner | Needed by |
|---|---|---|
| Which metropolitan area, specifically | Business | Before provider recruitment begins |
| Retention period for pseudonymised transaction records | Legal | Before slice 7 implements account deletion |
| Which verification documents are legally required, and renewal frequency | Legal / Insurance | Before pilot users are invited |
| May a job completed via the proof exception path auto-complete? | Operations | Before slice 4 implements the completion timer |
| Cancellation fee policy and no-show responsibility | Business / Legal | Phase 2; no payment flows exist in MVP |

These are advice-gathering and policy calls, not design work, and can run in parallel with the whole of Step 1 and most of Step 2. Only two touch the critical path: the exception-path question at slice 4, and the retention period at slice 7.

### Start immediately, in parallel — long lead time

**Apple Developer Program and Google Play Console enrolment.** This blocks the first build reaching a real device, not just launch. Organisational Apple enrolment requires a D-U-N-S number for the legal entity and commonly takes one to two weeks or more; obtaining a D-U-N-S number that does not yet exist adds further time. Nothing else in the plan depends on a third party this slow.

Start it on day one, before any code. Google Play enrolment is faster but has its own verification steps.

## Step 1 — Repository and local stack

```
Shipper/
  Docs/                    existing
  Source/                  existing
  apps/
    mobile/                Flutter — customer + provider
    admin/                 Next.js — admin panel
    driver-portal/         Next.js — job-scoped web portal
  services/
    core/                  Go — public API + domain
  deploy/
    docker-compose.yml     Postgres, Redis, Kafka for local development
  .github/workflows/       CI
```

### Why one repository

Shipper is one product with four deployables, not four products.

The decisive factor is the API contract. Adding a field to the job endpoint touches Go, Flutter, and the admin panel; across separate repositories that is three pull requests, three reviews, and a window in which each `main` is mutually inconsistent. In one repository it is a single commit that either compiles or does not — and that change gets made hundreds of times.

The concrete case is the job status model (`02` §1): twelve statuses with strict transition rules, expressed in **three languages** — Go, Dart, and TypeScript. Keeping those synchronised across repositories is a permanent tax whose only product is drift, and drift there causes exactly the class of defect that is hardest to trace.

The usual costs of a monorepo largely do not apply at this scale: there are four projects with standard toolchains, no cross-team coordination, and negligible clone size. **One practical requirement follows** — path-filter the CI workflows from the first commit. macOS runners for iOS builds cost roughly ten times Linux minutes, and a Go-only change must not trigger one.

The one genuine argument for splitting is that the mobile app has a different release cadence: store builds are tagged and long-lived, while the server deploys continuously. That is a *release* boundary, and tags and release branches address it. Making it a *repository* boundary would solve it while reintroducing contract drift. If the split ever becomes necessary, the seam is `apps/mobile` alone — not four repositories.

### Structuring the Go service

Structure `services/core` around the platform domains in `06` §3 — identity, profiles, fleet, jobs, bidding, delivery, notifications, admin — as internal packages with clear boundaries inside one deployable. This is what `06` §3 means by allowing later extraction without premature operational cost. Enforce the boundaries from the first commit; they are almost impossible to reintroduce later.

```
services/core/internal/
  jobs/
    service.go       domain logic and transition rules
    ports.go         interfaces this domain requires
    postgres.go      persistence — concrete, not behind an interface
    http.go          handlers and route registration
  bidding/
  delivery/
  platform/
    email/           console.go, provider.go
    sms/             console.go, provider.go
    push/            fcm.go, noop.go
    storage/         local.go, s3.go
```

`http.go` sits in the domain rather than in `cmd/api`, so that adding a domain adds a file instead of editing a shared one — which is what lets two domains be built at the same time without conflicting. A domain importing `internal/httpx` is sitting on infrastructure, not crossing a boundary. The full file layout, including `doc.go`, `model.go` and `errors.go`, is in `Docs/10` §2.1.

Two rules make this hold, both from `06` §4.1:

- **Adapters wrap external integrations only**, and only where a second implementation exists today. PostgreSQL is not abstracted; the partial unique index in SHIP-91 and the row locking in SHIP-92 are load-bearing and PostgreSQL-specific.
- **Interfaces are declared by the domain that consumes them**, never by the package implementing them. `delivery/ports.go` declares what delivery needs from storage; the storage package knows nothing about delivery.

The second rule is what SHIP-11's import lint check enforces, so the architectural intent and the automated check are the same rule expressed twice.

**Step 1 is done when:** `docker compose up` brings up Postgres, Redis, and Kafka; the Go service answers a health check; the Flutter app launches on a simulator and completes a round trip to that endpoint; and CI runs on every push.

Wire **TestFlight and Play internal testing during this step**, not at the end. Getting a signed build onto a real device on day one surfaces signing, provisioning, permission-prompt, and push-delivery problems while they are still cheap. Discovering them near launch is a well-known way to lose a fortnight.

## Step 2 — Build the walking skeleton

Build the critical path **thin and end to end** — register → publish job → bid → award → deliver → complete — before adding breadth anywhere. Each slice spans Flutter, Go, and Postgres together and leaves the system demonstrable.

Resist building any one layer out fully. A complete data model with no app to exercise it, or a polished app against stub endpoints, both hide exactly the integration problems this order is designed to expose early.

| # | Slice | Contents | Watch for |
|---|---|---|---|
| 1 | **Identity** | Register, sign in, verify email and phone, role selection at signup, token issue and rotation, secure device storage | Get token handling right now; retrofitting it is painful and security-sensitive |
| 2 | **Jobs** | Draft, publish, amend, cancel. Schema for the `02` §1 status model, with transitions enforced in one place | Model status transitions as guarded operations, never as a settable field |
| 3 | **Bidding** | Eligibility filtering, bid placement, private prices, counter-offers, **award** | The highest-risk work in the project — see below |
| 4 | **Delivery** | Driver assignment, job-scoped link generation, the driver web portal, milestones with idempotency keys, proof upload via pre-signed URLs | First place offline handling becomes real; build the queue here, not later |
| 5 | **Notifications** | Kafka domain events, FCM push, email, device registry, deep links | Events must be emitted by the domain, never by the API layer |
| 6 | **Admin** | Search, moderation queues (`04` §5), dispute workflow, audit trail | Needed before real users, not after; support has no other tooling |
| 7 | **Hardening** | Offline queue completion, forced-upgrade gate, in-app account deletion, Datadog dashboards | The upgrade gate and account deletion are store prerequisites, not polish |

### The award transaction is the highest-risk piece in the system

`02` §3 requires that awarding a job **atomically marks one bid accepted and closes all others**, and `01` §6 requires exactly one accepted bid per job. This is a concurrency problem, and it is the correctness core of the marketplace:

- Two rapid award attempts on the same job.
- A provider withdrawing a bid at the moment it is being accepted.
- An expiring bid racing an acceptance.
- A retry arriving after the original award succeeded.

Handle it in a single database transaction with appropriate locking, enforce the one-accepted-bid rule with a **database constraint** rather than application logic alone, and make it idempotent so a retried request returns the original outcome. Write concurrency tests that actually run these races. A duplicate award means two providers both believe they have the job — a marketplace-credibility failure, not a bug report.

### Deliberately deferred

Ratings and reputation, live GPS tracking, payments, and ML-driven matching are all out of scope per `01` §2 and belong to Phases 2 and 3 in `06` §6. Database-backed filtering is sufficient for pilot matching; do not add a search engine without evidence.

## Step 3 — Get it in front of real users

The pilot distributes through TestFlight and Play internal testing. No public store review stands between a build and a pilot user, which keeps the loop short while the release gate in `01` §8 is still open.

Two things to keep in mind throughout:

- **Dart code cannot be hot-fixed.** Anything expected to change under operational pressure — validation limits, category lists, policy copy, expiry windows, feature switches — belongs server-side in Go where it can be changed without a release. Decide this per feature as you build, not afterwards.
- **The upgrade gate must ship in the first build.** It cannot be added retroactively to builds already on devices, which is precisely when it is needed.

## Suggested order of work

1. Start Apple and Google enrolment. Begin closing Step 0 decisions.
2. Stand up the repository, local stack, CI, and device distribution.
3. Slices 1 and 2 — a user can register and publish a job.
4. Slice 3 — bidding and award, with concurrency tests. Longest and riskiest slice.
5. Slice 4 — delivery, the driver portal, and offline handling.
6. Slices 5 and 6 — notifications and admin. Both are needed before real users arrive.
7. Slice 7 — hardening, then invite pilot users against the `01` §8 release gate.

Steps 1 and 2 depend on nothing external and can begin immediately. Only the store-enrolment dependency, and the Step 0 decisions affecting the bidding and delivery data models, can block progress — which is why both start on day one.

## Ticket-level breakdown

This guide defines the shape of the work. `09-delivery-backlog.md` breaks it into **194 individually shippable tickets in strict build order**, each with an acceptance criterion and a story-point estimate, and maps the seven slices above onto milestones M0–M7 plus a non-code dependency track.

Use this document to understand *why* the order is what it is, and the backlog to decide what to do next.
