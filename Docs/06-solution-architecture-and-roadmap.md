# Shipper — Solution Architecture and Delivery Roadmap

**Status:** Draft  
**Purpose:** Set the architectural direction without prescribing implementation-level design.

## 1. Architecture principles

- Start simple: a modular core platform, not a large set of microservices.
- Make job, bid, and delivery records authoritative and auditable.
- Separate customer/provider experience from administrator experience.
- Keep asynchronous work—notifications, analytics, future integrations—away from critical job transactions.
- Design security and operational visibility into the first release.
- Extract independent services only when scale, team ownership, or operational evidence justifies it.

## 2. Logical architecture

| Layer | Responsibility | Preferred technology |
|---|---|---|
| Customer/provider experience | Marketplace mobile application | Flutter (iOS + Android) |
| Administrator experience | Privileged support and moderation interface | Next.js |
| Assigned-driver portal | Job-scoped, link-authenticated delivery updates | Next.js (responsive web) |
| Public API | Versioned REST/JSON contract for app, admin, and driver portal | Go |
| Core platform | Authoritative business rules and domain workflows | Go |
| System of record | Users, jobs, bids, messages, delivery, audit records | PostgreSQL |
| Cache | Refresh tokens, device registry, idempotency keys, rate limits | Redis |
| Event backbone | Notifications, reporting, integrations, background processing | Kafka |
| Push delivery | Device notification fan-out | Firebase Cloud Messaging (fronting APNs) |
| Observability | Logs, metrics, traces, alerts | Datadog |
| Cloud foundation | Managed runtime, network, storage, security | AWS |

### 2.1 Why the separate BFF tier is gone

The earlier design placed a Next.js BFF between a Next.js web client and the Go platform. That tier existed to assemble session-aware server-rendered page data — a need a native mobile client does not have.

A Flutter client consumes a plain versioned JSON API, so the BFF's responsibilities collapse into the Go platform, which now owns the public API contract directly. This removes a deployable, a language, and a network hop from the critical path.

Two consequences follow:

- **One contract, three consumers.** The mobile app, the admin panel, and the driver portal all consume the same versioned Go API. The admin panel keeps its own Next.js server-side data access for privileged screens, but it is an application detail, not a shared platform tier.
- **Redis changes role.** It no longer holds web sessions. It now backs refresh-token state, the device registry, idempotency keys for offline retries, and rate limits. The rule from §4 still holds: it must not be the only copy of a job, bid, or status.

## 3. Platform domains

The core platform should have clear ownership boundaries:

- Identity and access
- Customer/provider profiles
- Vehicle fleet and eligibility
- Jobs
- Bidding and negotiation
- Delivery execution and proof
- Notifications
- Administration, disputes, and audit

At MVP scale, these domains may share one deployable Go application and one PostgreSQL database while maintaining clear internal boundaries. This allows later extraction without premature operational cost.

## 4. Data and integration rules

- PostgreSQL is the source of truth for business decisions.
- Redis may accelerate reads but must not be the only copy of a job, bid, or status.
- Every critical state change emits a durable domain event for notifications, audit, and reporting.
- External services must not be able to alter the core job lifecycle directly.
- Files such as proof-of-delivery images and verification documents live in private object storage; the database retains metadata and access controls.
- Maps/geocoding, push, email/SMS, identity checks, and payments are replaceable integrations behind domain-owned interfaces.
- State-changing API calls accept a client-supplied idempotency key. A mobile client that retries after a dropped connection must be able to do so safely, and the platform must not create duplicate bids, milestones, or proof records as a result.

### 4.1 Adapters — where the pattern applies, and where it does not

The rule above about replaceable integrations is implemented with adapters, but the pattern is applied **selectively**. Applied everywhere it becomes ceremony; applied nowhere the platform becomes untestable and welded to its vendors.

**The test: does a second implementation exist today?**

Not "might one exist someday" — that question always answers yes and justifies any abstraction. If there is no second implementation now, the seam is speculation, and speculative seams tend to be wrong in exactly the way that matters when the real second implementation finally arrives.

#### Adapters are used here

| Integration | Second implementation today |
|---|---|
| Email | Console in development, provider in staging and production |
| SMS | Console in development, provider in staging and production |
| Push | Firebase Cloud Messaging, plus a no-op used in tests |
| Object storage | Local storage in development, S3 deployed |
| Maps and geocoding | Provider-backed, with a stub for tests |

Payments and third-party identity verification have no implementation yet, but both are named commitments for later phases (§6). Their interfaces are defined when the domain that needs them is built, so the seam exists before the vendor does.

#### Adapters are not used over PostgreSQL

PostgreSQL is the source of truth by decision, not by circumstance. It is not a replaceable integration, and a generic repository abstraction over it costs more than it returns.

The cost is concrete rather than theoretical. A generic persistence interface pulls the platform toward lowest-common-denominator SQL, and the two mechanisms holding award correctness together are both PostgreSQL-specific:

- The **partial unique index** enforcing one accepted bid per job (§3, `02` §3).
- Explicit **row locking** in the award transaction.

Both are load-bearing. Neither survives an abstraction designed to keep the database swappable, and losing either converts a database-enforced guarantee into an application-level hope.

Where the goal is testable domain logic, a **real PostgreSQL instance in tests** provides it with far more fidelity than a mocked repository. A mock happily accepts a write that the actual constraint would reject — which is precisely the failure the constraint exists to prevent.

#### Interface ownership

Interfaces are declared by the domain that **consumes** them, not by the package that implements them. `delivery` declares what it needs from storage; the storage package knows nothing about delivery.

This keeps every dependency arrow pointing inward at the domain, and it is the same property the import lint rule in §3 enforces — the architectural rule and the automated check are the same rule expressed twice.

## 5. Security and operations baseline

### 5.1 Platform

- Separate development, staging, and production environments.
- Private data services with least-privilege service access.
- Role-based and job-scoped authorisation.
- Secrets managed outside application code.
- Encrypted connections and encrypted storage.
- Backups, restore tests, deployment rollback, and incident alerting.
- Datadog dashboards for errors, job/bid success, notifications, delayed jobs, and suspicious activity.

### 5.2 Mobile client

- Short-lived access tokens with rotating refresh tokens, held in the iOS Keychain and Android Keystore — never in application preferences or on disk in clear text.
- Per-device session records, so a lost or stolen phone can be revoked without signing the user out everywhere.
- The mobile token scheme is strictly separate from the driver portal's signed, time-limited, single-job link token. Neither may be exchanged for the other.
- No business authorisation decided on the device. The app hides what a user may not do; the platform enforces it.
- Build signing keys and store credentials held in the CI secret store, not on developer machines.
- Proof-of-delivery and verification images uploaded directly to private object storage using short-lived pre-signed URLs, never proxied through the API.

### 5.3 Release control

- The public API is versioned, and more than one version may be live at once because old builds persist on devices indefinitely.
- The app checks a minimum-supported-version endpoint at launch and blocks with an update prompt when it falls below the floor.
- An API version is retired only after telemetry shows negligible traffic from builds that depend on it.
- Flutter has no over-the-air update path for Dart code, so anything expected to change under operational pressure — pricing rules, category lists, policy copy, feature switches — belongs server-side in Go, not compiled into the app.

## 6. Delivery roadmap

### Phase 0 — Discovery and operating model

Approve product scope, lifecycle, business model, verification policy, prohibited-goods policy, pilot geography, and legal-review outputs.

**Begin Apple Developer Program and Google Play Console enrolment in this phase, not later.** This is the longest lead-time item in the plan and it blocks the first device build, not just launch. Organisational Apple enrolment requires a D-U-N-S number for the legal entity and commonly takes one to two weeks or more; a D-U-N-S number that does not yet exist adds further time. Nothing else in the roadmap depends on a third party this slow, so starting it late silently delays every subsequent phase.

### Phase 1 — Marketplace MVP

Deliver:

- Customer and provider onboarding in the mobile app, with the role chosen at signup.
- Provider profile and vehicle management.
- Job creation, publication, discovery, bidding, negotiation, and award.
- Manual delivery milestones, proof of delivery, and job-scoped driver portal.
- Essential notifications, with push as the primary channel.
- Admin support, moderation, and audit capabilities.
- Mobile build pipeline: signed iOS and Android builds from CI, distributed to TestFlight and Play internal testing.
- Forced-upgrade gate and in-app account deletion, both of which are store-submission prerequisites rather than optional polish.

**Exit criterion:** Pilot users can complete real delivery jobs with support visibility and no unresolved critical safety/control gap.

### Phase 2 — Trust and operational maturity

Deliver:

- Enhanced verification and document expiry management.
- Reviews and reputation.
- Improved disputes, reporting, and risk controls.
- Better job matching and provider discovery.
- Pilot expansion based on completion and retention metrics.

### Phase 3 — Commercial scale

Evaluate and introduce only where commercially justified:

- Payments, commissions, invoicing, and refunds.
- Live tracking and ETA.
- Recurring jobs and business accounts.
- Analytics-driven matching/pricing insight.
- Additional regions, vehicle classes, and integrations.

## 7. Key architecture decisions to close

| Decision | Recommended position |
|---|---|
| Initial service shape | Modular Go platform with clear domain boundaries |
| Integration seams | Adapters for external integrations only, gated on a second implementation existing today; no abstraction over PostgreSQL — see §4.1 |
| Client structure | One Flutter app for customer and provider roles; separate Next.js admin panel; link-authenticated web driver portal |
| Mobile framework | Flutter, chosen for existing team capability; accepts store-review release cadence |
| Experience API tier | No separate BFF; the Go platform owns the versioned public API |
| API versioning | Version the public API from the first release; support at least one prior version while old builds remain in use |
| App version enforcement | Launch-time minimum-version check with a blocking update prompt |
| Pilot distribution | TestFlight and Play internal testing; no public store listing during the pilot |
| Payment ownership | No payment processing in MVP |
| Tracking | Manual milestones and proof of delivery first |
| Real-time requirements | Notifications are asynchronous; authoritative status is transactional |
| Offline behaviour | Delivery milestones queue on device and sync with idempotency keys; the server remains authoritative |
| Search/matching | Database-backed filtering first; dedicated search only when evidence requires it |
| Provider verification | Baseline eligibility before bidding; document evidence reviewed manually in MVP; enhanced controls in Phase 2 |
| Bid price confidentiality | The customer's budget is never exposed to a provider through any API response — a platform-enforced rule, not a client concern |
| Account deletion | Delete the person, retain the transaction under a stable pseudonym; audit history survives deletion |

## 8. Delivery governance

- Product owns scope, user experience, KPI definition, and prioritisation.
- Operations owns verification, support, moderation, and pilot feedback.
- Legal/privacy advisers approve public-facing policy and compliance positions.
- Architecture owns non-functional requirements, security baseline, resilience, and technical decision records.
- Business leadership owns geography, revenue model, risk appetite, and launch approval.
