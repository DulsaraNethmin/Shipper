# Shipper — Delivery Backlog

**Status:** Draft  
**Audience:** Engineering, delivery  
**Purpose:** Break the MVP into individually shippable tickets in strict build order.

Built for a **solo developer**, so this is a single ordered queue rather than parallel workstreams. Ticket IDs run in build order: at any point the next ticket is simply the lowest-numbered one still open. Track X is the exception — it is non-code work that must start on day one and run alongside everything else.

**201 tickets, 619 points.**

## How to read this

**Estimates are Fibonacci story points**, calibrated for one experienced developer:

| Points | Means |
|---|---|
| 1 | Under an hour. Mechanical |
| 2 | Half a day |
| 3 | About a day |
| 5 | Two to three days, or one day with real unknowns |
| 8 | A week. **Nothing here is an 8** — anything that size was split |

**Done when** is the acceptance criterion. If it cannot be demonstrated, the ticket is not done.

**Depends on** lists real blockers only, not merely earlier tickets. Where a ticket has no dependency it can genuinely be pulled forward if you want a change of pace.

**A letter suffix means a ticket added after the first draft.** `SHIP-57a` sorts immediately after `SHIP-57` and before `SHIP-58`, so build order is preserved without renumbering two hundred rows. Each one exists because work the plan assumed turned out to belong to no ticket — a table nobody created, an adapter nobody owned, a process four scheduled tasks all needed.

## Milestones

| Milestone | Goal | Tickets | Points |
|---|---|---|---|
| **X** — External dependencies | Unblock everything that depends on a third party. None of this is code; all of it is slow. | 9 | 26 |
| **M0** — Foundation | The stack runs locally, CI is green, and a signed build reaches a real device. | 30 | 75 |
| **M1** — Identity and access | A person can register, verify, choose a role, and stay signed in across app restarts. | 28 | 78 |
| **M2** — Jobs | A verified customer can create, publish, amend, and cancel a job from the app. | 26 | 78 |
| **M3** — Bidding and award | Providers discover eligible jobs, bid privately, negotiate, and a customer awards exactly one. | 27 | 95 |
| **M4** — Delivery execution | A driver completes a delivery with proof, offline, through a link that needs no account. | 29 | 101 |
| **M5** — Notifications | Every essential event reaches the right person, without a notification failure losing the event. | 13 | 45 |
| **M6** — Administration and moderation | Support can see everything, act on it, and leave an auditable trail. | 20 | 65 |
| **M7** — Hardening and pilot readiness | The store prerequisites are met, the system is observable, and the release gate can be run. | 19 | 56 |
| | | **201** | **619** |

Each milestone ends somewhere demonstrable. That matters more when working alone than it does on a team — a milestone you can show someone is the thing that tells you the plan is still real.

## Track X — External dependencies

**Goal:** Unblock everything that depends on a third party. None of this is code; all of it is slow.  
**Size:** 9 tickets, 26 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| X-1 | Obtain D-U-N-S number for the legal entity | 2 | D-U-N-S number issued and recorded | — |
| X-2 | Enrol in Apple Developer Program | 3 | Enrolment approved and App Store Connect accessible | X-1 |
| X-3 | Enrol in Google Play Console | 2 | Console access granted and developer profile complete | — |
| X-4 | Brief legal adviser on retention period and verification documents | 3 | Written answers received for Docs 05 §3.1 and Docs 04 §3 | — |
| X-5 | Choose the pilot metropolitan area | 2 | Metro area named and recorded in Docs 01 §8 | — |
| X-6 | Decide whether proof-exception jobs may auto-complete | 1 | Decision recorded in Docs 02 §7 | — |
| X-7 | Publish privacy policy at a public URL | 5 | Policy live at a stable URL reachable without authentication | X-4 |
| X-8 | Draft customer terms of use and provider agreement | 5 | Both documents approved by the legal adviser | X-4 |
| X-9 | Finalise the prohibited-goods list | 3 | Category list approved and ready to load as reference data | X-4 |

## M0 — Foundation

**Goal:** The stack runs locally, CI is green, and a signed build reaches a real device.  
**Size:** 30 tickets, 75 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-1 | Create monorepo directory structure | 1 | apps/mobile, apps/admin, apps/driver-portal, services/core, deploy/ exist and are committed | — |
| SHIP-2 | Add PostgreSQL to docker-compose | 2 | docker compose up starts Postgres and psql connects from the host | SHIP-1 |
| SHIP-3 | Add Redis to docker-compose | 1 | redis-cli ping returns PONG from the host | SHIP-2 |
| SHIP-4 | Add Kafka to docker-compose | 3 | A topic can be created and a test message produced and consumed | SHIP-2 |
| SHIP-5 | Go service skeleton with HTTP server | 2 | Service builds and listens on a configured port | SHIP-1 |
| SHIP-6 | Health endpoint returning build info | 1 | GET /health returns 200 with version and commit | SHIP-5 |
| SHIP-7 | Database migration tooling and first migration | 3 | Migrations run up and down from a make target | SHIP-2, SHIP-5 |
| SHIP-8 | Configuration loading from environment | 2 | All config comes from env vars with documented defaults; no secrets in code | SHIP-5 |
| SHIP-9 | Structured JSON logging with levels | 2 | Every request logs method, path, status, duration, request ID | SHIP-5 |
| SHIP-10 | Define internal package boundaries for the eight domains | 3 | Package skeleton exists for the eight domains plus a platform/ tree for integration adapters, per Docs 06 §4.1 | SHIP-5 |
| SHIP-11 | Enforce domain boundaries with an import lint rule | 3 | CI fails when one domain package imports another directly, or when an adapter imports a domain | SHIP-10 |
| SHIP-12 | Standard API error response contract | 2 | All errors return the same shape with a machine-readable code | SHIP-5 |
| SHIP-13 | API versioning scheme with a /v1 route group | 2 | All routes are served under /v1 and the version is documented | SHIP-5 |
| SHIP-14 | Request ID generation and context propagation | 2 | A request ID flows from middleware into logs and downstream calls | SHIP-9 |
| SHIP-15 | Idempotency-key middleware backed by Redis | 5 | A repeated key returns the stored original response without re-executing | SHIP-3, SHIP-12 |
| SHIP-15a | Engineering conventions and the shared-surface mechanisms | 5 | Docs 10 exists; migrations, routes, error codes and test databases each have a mechanism that makes a collision fail a test rather than a merge | SHIP-15 |
| SHIP-15b | Spelling check scoped for client code | 2 | The Australian English check passes over Flutter and Next.js source while still failing on user-facing copy inside them | SHIP-15a |
| SHIP-16 | Flutter project scaffold for iOS and Android | 2 | App builds and runs on both simulators | SHIP-1 |
| SHIP-17 | Flutter feature-folder structure and state management choice | 3 | Structure matches Docs 07 §2 and the state approach is documented | SHIP-16 |
| SHIP-17a | Published API contract | 3 | contracts/openapi.yaml exists and a Go test validates real handler responses against it | SHIP-13 |
| SHIP-18 | Flutter API client with environment-based base URL | 3 | Client targets local, staging, and production by build flavour | SHIP-17 |
| SHIP-19 | Flutter health round trip proving connectivity | 1 | App displays the API version fetched from /health | SHIP-18, SHIP-6 |
| SHIP-20 | CI: Go build, vet, and test | 2 | Workflow runs on every push and fails on a broken build or test | SHIP-5 |
| SHIP-21 | CI: Flutter analyze and test | 2 | Workflow runs on every push and fails on analyzer errors | SHIP-16 |
| SHIP-22 | Next.js admin panel scaffold | 2 | App builds and serves a placeholder authenticated shell | SHIP-1 |
| SHIP-23 | Next.js driver portal scaffold | 2 | App builds and serves a placeholder job page | SHIP-1 |
| SHIP-24 | iOS build signing in CI | 5 | CI produces a signed .ipa without developer machine involvement | SHIP-21, X-2 |
| SHIP-25 | TestFlight upload pipeline | 3 | A push to main lands a build in TestFlight and installs on a real device | SHIP-24 |
| SHIP-26 | Android build signing in CI | 3 | CI produces a signed .aab with keys held in the CI secret store | SHIP-21, X-3 |
| SHIP-27 | Play internal testing upload pipeline | 3 | A push to main lands a build in Play internal testing and installs on a real device | SHIP-26 |

## M1 — Identity and access

**Goal:** A person can register, verify, choose a role, and stay signed in across app restarts.  
**Size:** 28 tickets, 78 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-28 | users table migration | 2 | Table exists with email, phone, role, status, verification state | SHIP-7 |
| SHIP-29 | Password hashing with a modern KDF | 2 | Passwords stored with argon2id; no reversible storage anywhere | SHIP-28 |
| SHIP-30 | Registration endpoint | 3 | POST /v1/auth/register creates an unverified account and rejects duplicates | SHIP-29 |
| SHIP-31 | Email verification token issue and storage | 2 | A single-use, expiring token is generated and stored on registration | SHIP-30 |
| SHIP-32 | Email sending adapter | 3 | Emails log to console in dev and send via the provider in staging | SHIP-8 |
| SHIP-33 | Email verification confirm endpoint | 2 | POST /v1/auth/verify-email marks the address verified and consumes the token | SHIP-31, SHIP-32 |
| SHIP-34 | Phone OTP issue and storage | 3 | A time-limited numeric OTP is generated, rate-limited, and stored hashed | SHIP-30 |
| SHIP-35 | SMS adapter for OTP delivery | 3 | OTP sends via the SMS provider in staging; logs to console in dev | SHIP-8 |
| SHIP-36 | Phone verification confirm endpoint | 2 | POST /v1/auth/verify-phone marks the number verified after a correct OTP | SHIP-34, SHIP-35 |
| SHIP-37 | Access token issue | 3 | Short-lived signed token carrying user ID, role, and expiry | SHIP-30 |
| SHIP-38 | device_sessions table | 2 | One row per device holding refresh state, device label, and last seen | SHIP-28 |
| SHIP-39 | Refresh token issue with rotation | 5 | Each refresh returns a new token and invalidates its predecessor | SHIP-38, SHIP-37 |
| SHIP-40 | Refresh token reuse detection | 3 | Presenting a consumed token invalidates the entire device session | SHIP-39 |
| SHIP-41 | Login endpoint | 3 | POST /v1/auth/login returns an access and refresh token pair | SHIP-39 |
| SHIP-42 | Token refresh endpoint | 2 | POST /v1/auth/refresh rotates the pair and rejects reused tokens | SHIP-40 |
| SHIP-43 | Logout endpoint | 2 | POST /v1/auth/logout revokes the current device session only | SHIP-39 |
| SHIP-44 | Authentication middleware | 3 | Protected routes reject missing, expired, or malformed tokens with a typed error | SHIP-37, SHIP-12 |
| SHIP-45 | Role assignment at registration | 2 | Account is created as customer or provider and the role is immutable thereafter | SHIP-30 |
| SHIP-46 | Session list and revoke endpoints | 3 | A user can list their devices and revoke any one of them | SHIP-38, SHIP-44 |
| SHIP-47 | Rate limiting on authentication endpoints | 3 | Repeated failures are throttled per account and per IP | SHIP-3, SHIP-41 |
| SHIP-48 | Flutter secure storage wrapper | 3 | Refresh token persists in Keychain and Keystore and never in preferences | SHIP-17 |
| SHIP-49 | Flutter authentication state and routing guard | 5 | App routes to signed-in or signed-out shell correctly on cold start | SHIP-48 |
| SHIP-50 | Flutter token refresh interceptor | 5 | A 401 triggers one refresh and replays the request; concurrent calls refresh once | SHIP-49, SHIP-42 |
| SHIP-51 | Flutter registration screen | 3 | A new account can be created from the app with inline validation | SHIP-49, SHIP-30 |
| SHIP-52 | Flutter role selection screen | 2 | Role is chosen during signup and drives the post-login shell | SHIP-51, SHIP-45 |
| SHIP-53 | Flutter email verification screen | 2 | User can enter or deep-link a code and see verified state | SHIP-51, SHIP-33 |
| SHIP-54 | Flutter phone verification screen | 3 | User can request and enter an OTP with resend throttling | SHIP-51, SHIP-36 |
| SHIP-55 | Flutter login screen | 2 | An existing user can sign in and lands in the correct role shell | SHIP-49, SHIP-41 |

## M2 — Jobs

**Goal:** A verified customer can create, publish, amend, and cancel a job from the app.  
**Size:** 26 tickets, 78 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-56 | jobs table and status enum migration | 3 | Schema covers all twelve statuses from Docs 02 §1 | SHIP-7, SHIP-28 |
| SHIP-56a | Status enumeration codegen for Go, Dart and TypeScript | 2 | One source produces all three; CI fails if a generated file is stale | SHIP-56 |
| SHIP-57 | Job status transition guard | 5 | Every transition passes one guarded function; status is never directly settable | SHIP-56 |
| SHIP-57a | job_status_history table | 2 | Every transition records actor, reason, actor time and server time | SHIP-57 |
| SHIP-58 | Goods category reference data | 2 | Categories load from configuration and are served to clients, not compiled in | SHIP-56, X-9 |
| SHIP-59 | Prohibited goods validation on publish | 3 | A job in a prohibited category cannot be published and explains why | SHIP-58 |
| SHIP-59a | Geocoding adapter | 3 | Provider-backed in staging, stub in tests; a failed lookup does not fail the job | SHIP-8 |
| SHIP-60 | Address and location value object | 3 | Pickup and drop-off validate and normalise, with coordinates resolved | SHIP-56, SHIP-59a |
| SHIP-61 | Create job draft endpoint | 3 | POST /v1/jobs creates a Draft owned by the calling customer | SHIP-57, SHIP-44 |
| SHIP-62 | Edit job draft endpoint | 3 | PATCH /v1/jobs/{id} updates a Draft and rejects edits by non-owners | SHIP-61 |
| SHIP-63 | Publish job endpoint | 5 | Publishing validates required fields, checks customer verification, and transitions to Open | SHIP-59, SHIP-60, SHIP-62 |
| SHIP-64 | Cancel job endpoint | 2 | A customer can cancel a Draft or an unawarded Open job | SHIP-57 |
| SHIP-65 | Job detail endpoint, customer view | 3 | Returns full job including budget, for the owning customer only | SHIP-61 |
| SHIP-66 | Job list endpoint, customer view | 3 | Returns the customer's own jobs, filterable by status, paginated | SHIP-65 |
| SHIP-67 | Budget field stored and never serialised to providers | 3 | A provider-facing serialisation test proves the field cannot leak | SHIP-65 |
| SHIP-67a | Scheduled task runner | 3 | cmd/worker claims due work with FOR UPDATE SKIP LOCKED and survives running twice | SHIP-7 |
| SHIP-68 | Job expiry scheduled task | 5 | Open jobs close at the earlier of 14 days or the pickup date passing | SHIP-57, SHIP-67a |
| SHIP-69 | Expiry warning 48 hours ahead | 2 | A domain event fires 48 hours before a job would expire | SHIP-68 |
| SHIP-70 | Extend job expiry endpoint | 2 | A customer can extend an expiring job in one call | SHIP-68 |
| SHIP-71 | Flutter job creation: locations step | 3 | Pickup and drop-off captured with validation and address lookup | SHIP-49, SHIP-60 |
| SHIP-72 | Flutter job creation: goods step | 3 | Category, description, dimensions, and weight captured | SHIP-71, SHIP-58 |
| SHIP-73 | Flutter job creation: schedule and vehicle step | 3 | Date window and vehicle requirement captured | SHIP-72 |
| SHIP-74 | Flutter job creation: budget and review step | 3 | Optional budget captured; full job reviewed before publish | SHIP-73 |
| SHIP-75 | Flutter draft save and resume | 3 | A partially completed job survives app restart and can be resumed | SHIP-74, SHIP-62 |
| SHIP-76 | Flutter customer job list | 3 | Customer sees their jobs grouped by status with pull-to-refresh | SHIP-49, SHIP-66 |
| SHIP-77 | Flutter customer job detail | 3 | Full job detail with status timeline and available actions | SHIP-76, SHIP-65 |

## M3 — Bidding and award

**Goal:** Providers discover eligible jobs, bid privately, negotiate, and a customer awards exactly one.  
**Size:** 27 tickets, 95 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-78 | vehicles table and fleet CRUD endpoints | 5 | A provider can add, edit, and deactivate vehicles | SHIP-44, SHIP-7 |
| SHIP-79 | Provider profile and service area | 3 | Provider declares service area and specialties; stored and queryable | SHIP-78 |
| SHIP-80 | bids table and status enum | 3 | Schema covers all eight bid statuses from Docs 02 §4 | SHIP-56 |
| SHIP-81 | Job eligibility filter query | 5 | Filters by service area, vehicle capability, verification state, and job status | SHIP-79, SHIP-80 |
| SHIP-82 | Open jobs feed endpoint for providers | 3 | GET /v1/jobs/open returns only eligible jobs, paginated | SHIP-81 |
| SHIP-83 | Provider job detail with budget stripped | 3 | Provider view omits budget entirely; verified by test | SHIP-82, SHIP-67 |
| SHIP-84 | Place bid endpoint | 3 | A verified, eligible provider can bid once per job with price and timing | SHIP-83 |
| SHIP-85 | Update bid endpoint | 2 | A provider can revise their own active bid | SHIP-84 |
| SHIP-86 | Withdraw bid endpoint | 2 | A provider can withdraw before acceptance; status becomes Withdrawn | SHIP-84 |
| SHIP-87 | Counter-offer endpoint for both parties | 5 | Customer and provider can counter; each counter supersedes the prior offer | SHIP-84 |
| SHIP-88 | Offer supersede and history chain | 3 | Only the latest valid offer is acceptable; full chain remains readable | SHIP-87 |
| SHIP-89 | Bid expiry scheduled task | 3 | Bids expire on their own terms and emit an event | SHIP-84, SHIP-67a |
| SHIP-90 | Negotiating presentation status | 2 | A job with active offers presents as Negotiating without closing to new bids | SHIP-87, SHIP-57 |
| SHIP-91 | Database constraint: one accepted bid per job | 2 | A partial unique index makes a second accepted bid impossible at the database level | SHIP-80 |
| SHIP-92 | Award endpoint, transactional happy path | 5 | POST /v1/jobs/{id}/award accepts one bid and moves the job to Awarded in one transaction | SHIP-91, SHIP-88 |
| SHIP-93 | Award closes all competing bids atomically | 3 | Every other bid on the job becomes Rejected in the same transaction | SHIP-92 |
| SHIP-94 | Award idempotency | 3 | A retried award with the same key returns the original outcome, not an error | SHIP-92, SHIP-15 |
| SHIP-95 | Award concurrency test suite | 5 | Tests prove correctness under double award, withdraw-during-award, and expiry-during-award races | SHIP-93, SHIP-94 |
| SHIP-96 | Bid history visibility rules | 3 | Customer, bidding provider, and admin each see only what Docs 02 §4 permits | SHIP-88 |
| SHIP-97 | Job-scoped messaging between customer and provider | 5 | Messages attach to a job and are visible only to its two parties and admins | SHIP-84 |
| SHIP-98 | Flutter provider fleet management | 5 | Provider can manage vehicles from the app | SHIP-49, SHIP-78 |
| SHIP-99 | Flutter provider job feed | 3 | Provider sees eligible open jobs with filters | SHIP-98, SHIP-82 |
| SHIP-100 | Flutter provider job detail and bid placement | 3 | Provider can review a job and submit a bid | SHIP-99, SHIP-84 |
| SHIP-101 | Flutter provider bid list | 3 | Provider sees their own bids grouped by status | SHIP-100 |
| SHIP-102 | Flutter customer bid comparison | 5 | Customer compares price, timing, provider profile, and vehicle side by side | SHIP-77, SHIP-96 |
| SHIP-103 | Flutter negotiation and messaging UI | 5 | Both parties exchange messages and counter-offers against a job | SHIP-102, SHIP-97 |
| SHIP-104 | Flutter award confirmation flow | 3 | Customer awards a bid with explicit confirmation and sees the result | SHIP-102, SHIP-92 |

## M4 — Delivery execution

**Goal:** A driver completes a delivery with proof, offline, through a link that needs no account.  
**Size:** 29 tickets, 101 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-105 | driver_assignments table | 2 | Stores driver name and mobile against an awarded job | SHIP-56 |
| SHIP-106 | Assign driver endpoint | 3 | Provider nominates a driver or self-assigns; job moves to Driver assigned | SHIP-105, SHIP-57 |
| SHIP-107 | Job-scoped token generation | 5 | Signed, time-limited, single-job token generated on assignment | SHIP-106 |
| SHIP-108 | Driver token validation middleware | 3 | Token grants access to exactly one job and nothing else; cannot be exchanged for a user session | SHIP-107 |
| SHIP-109 | Revoke and regenerate driver token | 3 | Provider or admin can reissue a link, invalidating the previous one | SHIP-108 |
| SHIP-110 | milestones table with dual timestamps | 3 | Each row records actor time and server time separately | SHIP-56 |
| SHIP-111 | Milestone update endpoint with idempotency | 5 | Records a milestone once per idempotency key and enforces actor permissions | SHIP-110, SHIP-15 |
| SHIP-112 | Out-of-order milestone absorption | 5 | A late milestone is recorded as history without moving the job backwards | SHIP-111 |
| SHIP-113 | Administrative conflict resolution | 3 | A queued update contradicting an admin action loses and is retained with its reason | SHIP-112 |
| SHIP-114 | Object storage bucket and pre-signed upload endpoint | 5 | Client receives a short-lived pre-signed URL and uploads directly | SHIP-8 |
| SHIP-115 | Proof metadata record | 3 | Uploaded proof is linked to a job and milestone with access control | SHIP-114, SHIP-110 |
| SHIP-116 | Proof exception reason capture | 3 | A reasoned exception can be recorded in place of a photo | SHIP-115 |
| SHIP-117 | Exception flags the job for moderation | 2 | An exception-completed job enters the moderation queue | SHIP-116 |
| SHIP-118 | Delivered validation requires proof or exception | 3 | Delivered is rejected without either; verified by test | SHIP-116, SHIP-57 |
| SHIP-119 | 72-hour auto-complete task | 3 | A Delivered job with no dispute becomes Completed after 72 hours | SHIP-118, SHIP-67a |
| SHIP-120 | Driver portal token landing and job view | 5 | Opening the link shows only that job's delivery detail | SHIP-23, SHIP-108 |
| SHIP-121 | Driver portal milestone controls | 3 | Large touch targets record each milestone from a mobile browser | SHIP-120, SHIP-111 |
| SHIP-122 | Driver portal photo capture and upload | 5 | Browser camera captures proof and uploads via pre-signed URL | SHIP-121, SHIP-114 |
| SHIP-123 | Driver portal delivery completion form | 3 | Recipient name, note, and proof captured; portal becomes read-only after | SHIP-122, SHIP-118 |
| SHIP-124 | Flutter durable local operation queue | 5 | Queued operations survive app restart and are never silently dropped | SHIP-17 |
| SHIP-125 | Flutter sync worker with backoff | 5 | Queue drains on reconnection with exponential backoff and per-item idempotency keys | SHIP-124, SHIP-111 |
| SHIP-126 | Flutter pending-updates indicator | 2 | A persistent indicator shows how many updates are unsynced | SHIP-125 |
| SHIP-127 | Flutter four-hour unsynced nudge | 2 | The provider is prompted when an update has been pending four hours | SHIP-126 |
| SHIP-128 | Operations alert at 24 hours unsynced | 3 | A job with a 24-hour unsynced update enters the delivery exception queue | SHIP-127 |
| SHIP-129 | Flutter milestone update UI | 3 | Provider records milestones with optimistic local state clearly marked pending | SHIP-125 |
| SHIP-130 | Flutter camera capture with on-device compression | 5 | Photo captured, compressed, queued, and never written to the photo library | SHIP-129, SHIP-114 |
| SHIP-131 | Flutter camera permission fallback | 3 | A denied permission offers the exception path instead of a dead end | SHIP-130, SHIP-116 |
| SHIP-132 | Flutter offline conflict reconciliation UI | 3 | The user is shown clearly when a queued update lost to server state | SHIP-125, SHIP-113 |
| SHIP-133 | Flutter customer tracking view | 3 | Customer sees the latest confirmed milestone and proof of delivery | SHIP-77, SHIP-115 |

## M5 — Notifications

**Goal:** Every essential event reaches the right person, without a notification failure losing the event.  
**Size:** 13 tickets, 45 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-134 | Transactional outbox table and publisher | 5 | Domain events commit with their transaction and publish at least once | SHIP-4, SHIP-57 |
| SHIP-135 | Kafka topics and event schema | 3 | Topics exist with a versioned schema for each domain event | SHIP-134 |
| SHIP-136 | Emit domain events from job, bid, and delivery transitions | 5 | Every state change in Docs 01 §4.5 emits its event from the domain, not the API layer | SHIP-135 |
| SHIP-137 | Notification consumer service | 5 | Consumer reads events, resolves recipients, and dispatches per channel | SHIP-136 |
| SHIP-138 | Email templates and dispatch | 3 | Each essential event has an email template and sends reliably | SHIP-137, SHIP-32 |
| SHIP-139 | Firebase Cloud Messaging adapter | 3 | Push dispatches to iOS and Android and handles token rejection | SHIP-137 |
| SHIP-140 | device_tokens table with register and deregister | 3 | Tokens bind to a device session and clear on sign-out | SHIP-38, SHIP-139 |
| SHIP-141 | Push content redaction rules | 2 | No address, goods description, or full customer name appears in a notification body | SHIP-139 |
| SHIP-142 | Notification preferences per user | 3 | A user can mute non-essential categories; essential events cannot be muted | SHIP-137 |
| SHIP-143 | Flutter push registration | 3 | Token registers after sign-in and de-registers on sign-out | SHIP-140, SHIP-50 |
| SHIP-144 | Flutter permission prompt at the right moment | 2 | Notification permission is requested contextually, never on first launch | SHIP-143 |
| SHIP-145 | Flutter deep link routing | 5 | Tapping a notification opens the exact job, bid, or dispute it concerns | SHIP-143 |
| SHIP-146 | Flutter notification inbox | 3 | In-app list of recent notifications with read state | SHIP-145 |

## M6 — Administration and moderation

**Goal:** Support can see everything, act on it, and leave an auditable trail.  
**Size:** 20 tickets, 65 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-147 | Admin authentication, separate from user auth | 5 | Admin sign-in is independent and cannot be reached with a user token | SHIP-44, SHIP-22 |
| SHIP-148 | Admin roles and least-privilege permissions | 3 | Permissions are granular and default to the minimum | SHIP-147 |
| SHIP-149 | audit_log table and write helper | 3 | Append-only log capturing actor, action, target, timestamp, and reason | SHIP-7 |
| SHIP-150 | Audit every privileged action | 5 | All admin mutations write an audit entry; verified by test | SHIP-149, SHIP-148 |
| SHIP-151 | Admin user search | 3 | Search users by email, phone, name, and status | SHIP-147 |
| SHIP-152 | Admin job and bid search | 3 | Search and open any job with its full bid and status history | SHIP-151 |
| SHIP-153 | Admin verification queue | 3 | Pending provider verifications listed oldest first | SHIP-152 |
| SHIP-154 | Admin verification review and decision | 5 | Reviewer sets Verified, Restricted, Rejected, or Suspended with a recorded reason | SHIP-153, SHIP-150 |
| SHIP-155 | Admin document viewer for private evidence | 3 | Verification images render through short-lived signed URLs and are access-logged | SHIP-154, SHIP-114 |
| SHIP-156 | Admin reported jobs and messages queue | 3 | Reports surface with the job and conversation in context | SHIP-152 |
| SHIP-157 | Admin delivery exception queue | 3 | Overdue pickup, delayed delivery, failed proof, and unsynced milestones surface here | SHIP-152, SHIP-128 |
| SHIP-158 | Admin post-award cancellation queue | 2 | Cancellations after award are listed with the provider's history | SHIP-152 |
| SHIP-159 | Admin verification expiry queue | 3 | Expiring and expired provider documents surface ahead of time | SHIP-154 |
| SHIP-160 | Admin unpublish job | 2 | A policy-breaching job is removed with a recorded reason and the customer notified | SHIP-150 |
| SHIP-161 | Admin restrict or suspend a user | 3 | Account access is limited or disabled with a recorded reason | SHIP-150 |
| SHIP-162 | Admin internal notes | 2 | Support notes attach to a user or job and are never user-visible | SHIP-150 |
| SHIP-163 | Dispute intake endpoint | 3 | A customer or provider raises a dispute capturing the Docs 04 §7 intake fields | SHIP-57 |
| SHIP-164 | Admin dispute workflow and outcome | 5 | Dispute moves through investigation to a documented outcome that unfreezes the job | SHIP-163, SHIP-150 |
| SHIP-165 | Admin audit log viewer | 3 | Immutable history is searchable by actor, target, and date | SHIP-150 |
| SHIP-166 | Two-person review for permanent suspension | 3 | A permanent suspension requires a second administrator's approval | SHIP-161 |

## M7 — Hardening and pilot readiness

**Goal:** The store prerequisites are met, the system is observable, and the release gate can be run.  
**Size:** 19 tickets, 56 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-167 | Minimum supported version endpoint | 2 | GET /v1/app/minimum-version returns the floor per platform | SHIP-13 |
| SHIP-168 | Flutter launch-time version gate | 3 | A build below the floor blocks with an update prompt linking to the store | SHIP-167, SHIP-49 |
| SHIP-169 | Account deletion request endpoint | 3 | A signed-in user can request deletion and receives a completion date | SHIP-44 |
| SHIP-170 | Deletion deferral during an active job | 3 | A request during Awarded to Delivered queues until the job closes and explains why | SHIP-169, SHIP-57 |
| SHIP-171 | User record pseudonymisation | 5 | Profile and contact data are irreversibly replaced by a stable pseudonym | SHIP-170 |
| SHIP-172 | Cascade deletion of personal artefacts | 5 | Verification documents, message bodies, device tokens, and attributable images are removed | SHIP-171 |
| SHIP-173 | Flutter account deletion UI | 3 | Deletion is initiated in-app with clear consequences and confirmation | SHIP-169, SHIP-49 |
| SHIP-174 | Datadog APM and log ingestion | 3 | Traces and structured logs arrive from the Go service and are searchable | SHIP-9 |
| SHIP-175 | Datadog job and bid outcome dashboards | 3 | Publication, bid, and award success rates are visible on one board | SHIP-174 |
| SHIP-176 | Notification failure alerting | 2 | A rise in undelivered notifications pages someone | SHIP-174, SHIP-137 |
| SHIP-177 | Delayed and stuck job alerting | 2 | Jobs stalled in a status beyond threshold raise an alert | SHIP-174 |
| SHIP-178 | Crash and adoption reporting per app version | 3 | Crash rate and install base are visible per build, feeding version retirement | SHIP-21 |
| SHIP-179 | Permission purpose strings | 1 | Camera and notification prompts explain their purpose in plain language | SHIP-16 |
| SHIP-180 | Apple privacy labels submission | 2 | Labels submitted and consistent with the published privacy policy | SHIP-25, X-7 |
| SHIP-181 | Google Play data safety declaration | 2 | Declaration submitted and consistent with the published privacy policy | SHIP-27, X-7 |
| SHIP-182 | Backup and restore rehearsal | 5 | A production-shaped database is restored from backup and verified | SHIP-2 |
| SHIP-183 | API-wide rate limiting review | 3 | Every public endpoint has a considered limit and returns a typed error when exceeded | SHIP-47 |
| SHIP-184 | Pilot-scale load smoke test | 3 | The stack handles expected pilot concurrency without error-rate degradation | SHIP-174 |
| SHIP-185 | Release gate run-through | 3 | Every condition in Docs 01 §8 is evidenced and signed off | SHIP-180, SHIP-181 |

## Sequencing notes

### Start Track X on day one

X-1 through X-3 gate SHIP-24 to SHIP-27, which is how a build reaches a real device. Apple enrolment can take weeks and needs a D-U-N-S number first. Everything else in M0 proceeds without it, but if you leave enrolment until you *want* a device build, you will wait weeks with working code and nowhere to put it.

X-4 gates the privacy policy and the verification document list, both of which are needed well before pilot users. Send that brief early; advisers work on their own clock.

### M3 is the hard one

SHIP-91 through SHIP-95 are the correctness core of the marketplace. SHIP-91 (the database constraint) comes *before* the award endpoint deliberately — it is far easier to build correct behaviour against a constraint that already exists than to add one afterwards and discover your data violates it. SHIP-95's concurrency tests are not optional; a duplicate award means two providers both believe they have the job.

### Consider a thin admin earlier than M6

M6 sits late because no real users exist until M7. But working solo, you will want to inspect your own system long before then. SHIP-151 and SHIP-152 (user and job search) are cheap and pay for themselves as debugging tools — pulling just those two forward to sit after M2 is a reasonable trade.

### Mandatory proof and the offline queue are one problem

SHIP-124 to SHIP-131 all answer the same question: the driver must be able to finish the job when something is missing — signal, permission, or light. Build them together. Splitting them across milestones would mean writing the same fallback logic twice.

## What this means in calendar time

Worth stating plainly rather than discovering in month four.

| Working pattern | Points per week | Elapsed |
|---|---|---|
| Full-time, focused | 20–25 | **23–29 weeks** (roughly 5–7 months) |
| Full-time, with interruptions | 15 | **~39 weeks** |
| Evenings and weekends | 6–8 | **74–99 weeks** (over a year) |

These assume the point scale above and one experienced developer who already knows Flutter. They do **not** assume time spent learning Go, AWS, or Kafka — if any of those are new, add to M0 and M5 specifically.

Two things move this number more than working faster does: cutting scope (below), and not building the admin panel and driver portal yourself. Those two web surfaces are roughly 90 points of the total and are the most separable work in the plan.

## If you need to cut scope

599 points is a substantial solo build. These are the honest levers, in the order I would pull them:

| Cut | Saves | What you lose |
|---|---|---|
| SHIP-97, SHIP-103 — free-text messaging | 10 | Negotiation happens through counter-offers alone. Workable, and it keeps conversations structured, but parties will want to ask questions |
| SHIP-146 — notification inbox | 3 | Push and email only; no in-app history of what was sent |
| SHIP-166 — two-person suspension review | 3 | An internal control, not a user-facing feature. Reinstate before the team grows |
| SHIP-142 — notification preferences | 3 | Everyone gets everything. Acceptable at pilot volume, irritating beyond it |
| SHIP-159 — verification expiry queue | 3 | Manual tracking of expiring documents until Phase 2 |
| SHIP-184 — load smoke test | 3 | You find your limits in production. Only acceptable because pilot volume is small |

**Do not cut:** SHIP-91 to SHIP-95 (award correctness), SHIP-167 to SHIP-173 (store prerequisites — these block submission outright), SHIP-149 and SHIP-150 (audit — impossible to backfill), or SHIP-131 (permission fallback, which is what stops a driver being stranded).

## Open questions that touch the backlog

- **X-6** must be answered before SHIP-119 (the 72-hour auto-complete task) can be written correctly.
- **X-4** must be answered before SHIP-171 and SHIP-172 (pseudonymisation) can define what is retained.
- **X-9** must be answered before SHIP-58 (goods categories) can load real reference data.

None of these blocks the start of its milestone; each blocks one specific ticket inside it.

