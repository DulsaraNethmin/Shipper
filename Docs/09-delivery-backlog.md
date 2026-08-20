# Shipper — Delivery Backlog

**Status:** Draft  
**Audience:** Engineering, delivery  
**Purpose:** Break the MVP into individually shippable tickets in strict build order.

Built for a **solo developer**, so this is a single ordered queue rather than parallel workstreams. Ticket IDs run in build order: at any point the next ticket is simply the lowest-numbered one still open. Track X is the exception — it is non-code work that must start on day one and run alongside everything else.

**240 tickets, 747 points.** Counted from the rows on 2026-08-19, at `c710ed4`, plus the one row
this branch adds. The previous figure — 239 and 744 — was correct for the tree it was written on
and this pass adds `SHIP-17d`, three points.

**The line before that read 237 and 738, and it was wrong for two waves.** It predated wave 16's
Phase 0, which split three rows without the header being recounted, and `SHIP-15ao` exists only to
have corrected it. **`make status` reads the rows rather than this line**, so a header left behind
does not fail a gate — it disagrees with the plan quietly, which is why the count is restated here
whenever a row is added rather than left for a later pass to reconcile.

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

**Dependencies point backwards, with exactly ten forward edges across nine tickets — do not write a parser that assumes otherwise, and count edges rather than rows.** `SHIP-15c` depends on `SHIP-17a`, `SHIP-15e` depends on `SHIP-44`, `SHIP-15m` depends on `SHIP-44` **and** `SHIP-135` — two edges from one row, which is why this sentence says which unit it is counting — `SHIP-30a` depends on `SHIP-151`, `SHIP-65a` depends on `SHIP-83a`, `SHIP-70a` depends on `SHIP-90`, `SHIP-78a` depends on `SHIP-79`, `SHIP-81b` depends on `SHIP-114`, and `SHIP-134a` depends on `SHIP-135`. All of them exist because a lettered ticket is inserted at the point in build order where it *belongs* rather than where its blockers sit. Compute startability from the dependency column itself, never from ticket order.

**The count is ten edges and it is unchanged by the wave-11 reconciliation, which added two.** `SHIP-144 → X-10` and `SHIP-145 → X-10` are both *backward*: Track X sorts first in build order, as it does in the milestone table above, so a dependency on an X row is never a forward edge whatever it gates. Re-derived by parsing the column rather than counted by eye, on `5a3b8d7` and again after the row was added.

**Every one of the ten points at finished work again, and that is a state this paragraph has now been through in both directions.** The wave-10 reconciliation recorded the first exception in the file's history — `SHIP-65a → SHIP-83a`, where the target was open, so `SHIP-65a` was genuinely not startable and a tool treating forward edges as decorative would have reported it as ready. **SHIP-83a landed in wave 11**, so that edge is satisfied and SHIP-65a is startable; the warning is kept rather than deleted because the condition recurs the next time a lettered row is written ahead of its blocker. **Compute startability from the dependency column itself, never from ticket order, and never from whether this paragraph currently names an exception.**

**This figure is hand-maintained and has been checked by a parser rather than counted by eye.** Both totals above, the milestone table below, and the **Size:** line under each milestone heading are all the same kind of number — the rows are the truth, `scripts/delivery-status.sh` reads them, and nothing reads any of these. Whoever adds a lettered row recounts **all eight figures across those four places** in the same change: the two headline totals, the milestone table's row and its total line, and that milestone's Size line.

**This sentence used to say "all three" and omitted the Size lines, and that omission is what they drifted through.** A row added to M3 moved its table row and not its Size line, so one figure had two hand-maintained copies that disagreed with each other; M0's and M7's went the same way. **An enumeration that is not exhaustive is worse than no enumeration**, because it reads as a checklist somebody has completed.

**The two totals above are maintained by hand and the rows are the truth.** `scripts/delivery-status.sh` parses the rows, so `make status` is unaffected by a stale header — which is precisely why one drifted unnoticed after SHIP-15e was added. If the two disagree, correct the header.

**A letter suffix means a ticket added after the first draft.** `SHIP-57a` sorts immediately after `SHIP-57` and before `SHIP-58`, so build order is preserved without renumbering two hundred rows. Each one exists because work the plan assumed turned out to belong to no ticket — a table nobody created, an adapter nobody owned, a process four scheduled tasks all needed.

## Milestones

| Milestone | Goal | Tickets | Points |
|---|---|---|---|
| **X** — External dependencies | Unblock everything that depends on a third party. None of this is code; all of it is slow. | 10 | 29 |
| **M0** — Foundation | The stack runs locally, CI is green, and a signed build reaches a real device. | 41 | 117 |
| **M1** — Identity and access | A person can register, verify, choose a role, and stay signed in across app restarts. | 29 | 83 |
| **M2** — Jobs | A verified customer can create, publish, amend, and cancel a job from the app. | 28 | 84 |
| **M3** — Bidding and award | Providers discover eligible jobs, bid privately, negotiate, and a customer awards exactly one. | 39 | 133 |
| **M4** — Delivery execution | A driver completes a delivery with proof, offline, through a link that needs no account. | 33 | 112 |
| **M5** — Notifications | Every essential event reaches the right person, without a notification failure losing the event. | 14 | 48 |
| **M6** — Administration and moderation | Support can see everything, act on it, and leave an auditable trail. | 22 | 69 |
| **M7** — Hardening and pilot readiness | The store prerequisites are met, the system is observable, and the release gate can be run. | 24 | 72 |
| | | **240** | **747** |

Each milestone ends somewhere demonstrable. That matters more when working alone than it does on a team — a milestone you can show someone is the thing that tells you the plan is still real.

## Track X — External dependencies

**Goal:** Unblock everything that depends on a third party. None of this is code; all of it is slow.  
**Size:** 10 tickets, 29 points

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
| X-10 | Create the Firebase project and issue its push credentials | 3 | A Firebase project exists; a service-account key is in the CI secret store and in no commit; `google-services.json` and `GoogleService-Info.plist` are available to the mobile build; and a push sent with the platform's own credential arrives on a real handset | — |

**X-10 was written at the wave-11 reconciliation, eleven waves after the work behind it started, and the reason it stayed invisible is worth more than the row.** `Docs/11` §5 lists only work blocked on a Track-X ticket, and **this was blocked on nothing** — there was no row to be blocked on. So SHIP-139 built the Firebase adapter against a fake FCM server, SHIP-143 shipped a `PushTokenSource` seam with a test asserting the absence, both said in their own write-ups that no project exists, and neither could do anything about it. **A prerequisite the backlog assumed and never wrote down is invisible to every instrument here**: `make status` counts rows, `make verify` exercises endpoints, and neither can report a thing that is missing from the plan itself. It is the same shape as the SHIP-153 gap the wave-10 reconciliation found, and the same answer — write the row.

**It depends on nothing and can be done this week, which is the operationally important half.** Creating the project, downloading the two client configuration files and issuing a service-account key need no third-party approval and no enrolment. **The one part that does wait is the iOS leg**: FCM reaches an iPhone through APNs, which needs an authentication key from the Apple Developer Program, so that half arrives with X-2. Split the ticket if the Android leg is wanted sooner; do not let the iOS half hold the project.

**Four rows take the dependency and a fifth deliberately does not.** SHIP-144 and SHIP-145 are open and take it in the column below, which is what makes them stop reading as startable. SHIP-139 and SHIP-143 are **done in reduced form** and carry the gap in `Docs/11` §4 instead, with X-10 as the named owner — a done ticket with an unmet dependency in this column would misreport the graph, and §4 is the instrument for a shipped ticket whose *Done when* is partly met. **SHIP-140 takes neither**, and that is measured rather than assumed: its *Done when* is "tokens bind to a device session and clear on sign-out", a push token is an opaque string to the platform, and `make verify` demonstrates the whole of it today.

## M0 — Foundation

**Goal:** The stack runs locally, CI is green, and a signed build reaches a real device.  
**Size:** 41 tickets, 117 points

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
| SHIP-15c | Wave-2 shared surfaces: dependencies, isolation, error codes | 5 | Deps carries the pool and the Redis client, a second worktree's tests cannot drop this one's template, infrastructure importing a domain fails the lint, and httpx.RegisterCode backs a generated code list | SHIP-15a, SHIP-17a |
| SHIP-15e | Wave-3 shared surfaces: the verify script, the handler helpers, the done list | 5 | A domain adds a verify section by adding a file and editing none; httpx.H and httpx.DecodeJSON exist and internal/identity uses them; the done list is merge=union; make web-check type-checks after the build | SHIP-15c, SHIP-44 |
| SHIP-15g | Wave-4 shared surfaces: geocoding and pagination configuration, the out-of-order migration guard, a shutdown hook for scheduled tasks | 5 | Two tracks can start a wave without either needing an internal/config edit; a migration numbered below the current version can no longer be skipped silently; a scheduled task that owns a resource can release it | SHIP-15e |
| SHIP-15i | Wave-5 shared surfaces: the measured verify count, the tracker index check, the cause of an unmapped 500 | 3 | Two tracks can start wave 5 without either needing an edit to a file the other owns | SHIP-15g |
| SHIP-15m | Wave-6 shared surfaces: the driver-token guard seam, the Kafka replication factor, three parallel-working rules | 3 | SHIP-108 can supply the RequireDriverToken middleware without editing cmd/api/routes.go, manifest.go or Deps, and a route declaring the class still refuses to start while nothing supplies one; the replication factor is configuration with the flag kept as an override | SHIP-44, SHIP-135 |
| SHIP-15p | Wave-7 shared surfaces: object storage in the local stack, the STORAGE_* configuration section, the app test fixture | 3 | Two tracks can start wave 7 without either needing an edit to a file the other owns: `make reset && make up` brings an S3-compatible store up healthy with a usable bucket and no manual step, internal/config exposes the storage settings and deploy/.env.example documents them, and a new configuration section no longer breaks routes_app_test.go | SHIP-15m |
| SHIP-15r | Wave-8 shared surfaces: the RequireAdmin guard seam, argon2id promoted out of a domain, the download URL's own lifetime, and the rule for starting cmd/worker from a verify section | 5 | Four tracks can start wave 8 without any of them needing an edit to a file another owns: SHIP-147 supplies the administrator guard by filling newAdminGuard alone and hashes a password without importing another domain, a proof download is signed for STORAGE_DOWNLOAD_TTL rather than the upload's, and a verify section that starts cmd/worker has one written rule to follow | SHIP-15p |
| SHIP-16 | Flutter project scaffold for iOS and Android | 2 | App builds and runs on both simulators | SHIP-1 |
| SHIP-17 | Flutter feature-folder structure and state management choice | 3 | Structure matches Docs 07 §2 and the state approach is documented | SHIP-16 |
| SHIP-17a | Published API contract | 3 | contracts/openapi.yaml exists and a Go test validates real handler responses against it | SHIP-13 |
| SHIP-17b | Contract validation that reaches the authenticated surface | 5 | The response check covers the routes its exercisable() filter skips today — every non-GET, every authenticated and every parameterised route, which is 82 of the 86 on the manifest — by driving them with fixtures rather than naming them as skipped; a request body carrying a field the contract does not declare fails a gate, which is the half that has no check at all; and whatever is still unreached is named in the output rather than counted | SHIP-17a |
| SHIP-17c | A job-state builder, so the contract check reaches the lifecycle | 3 | The 18 routes SHIP-17b named unreached for want of a job past Draft are driven against fixtures that reach them — the job seeded up Docs/02 §2's ladder through the guarded transition rather than by UPDATE, an eligible provider, a real award, a driver assignment and a delivery under way; coverage is reported as a measured count; and whatever is still unreached is named with a reason that is not "the job is a Draft" | SHIP-17b |
| SHIP-17d | The last five contract routes, and an object store to reach one of them | 3 | Every route on the manifest is driven by a fixture and `contractUnreached` is empty — the two-person suspension approval driven by a *second* signed-in administrator, the provider's verification evidence read back as a page that carries a document rather than as an empty one, and the email token and phone code minted and stored the way the platform stores them rather than read back from a message the platform keeps no copy of; the document-recording route reaches a store that answers a real HEAD, wired before the router rather than in a fixture hook; and a route whose success status does not by itself prove the handler found anything says so with a check that fails when it does not | SHIP-17c |
| SHIP-18 | Flutter API client with environment-based base URL | 3 | Client targets local, staging, and production by build flavour | SHIP-17 |
| SHIP-19 | Flutter health round trip proving connectivity | 1 | App displays the API version fetched from /health | SHIP-18, SHIP-6 |
| SHIP-20 | CI: Go build, vet, and test | 2 | Workflow runs on every push and fails on a broken build or test | SHIP-5 |
| SHIP-21 | CI: Flutter analyze and test | 2 | Workflow runs on every push and fails on analyzer errors | SHIP-16 |
| SHIP-22 | Next.js admin panel scaffold | 2 | App builds and serves a placeholder authenticated shell | SHIP-1 |
| SHIP-23 | Next.js driver portal scaffold | 2 | App builds and serves a placeholder job page | SHIP-1 |
| SHIP-23a | CI: admin panel and driver portal | 2 | One path-filtered workflow per web surface; a Go-only change triggers neither | SHIP-22, SHIP-23 |
| SHIP-24 | iOS build signing in CI | 5 | CI produces a signed .ipa without developer machine involvement | SHIP-21, X-2 |
| SHIP-25 | TestFlight upload pipeline | 3 | A push to main lands a build in TestFlight and installs on a real device | SHIP-24 |
| SHIP-26 | Android build signing in CI | 3 | CI produces a signed .aab with keys held in the CI secret store | SHIP-21, X-3 |
| SHIP-27 | Play internal testing upload pipeline | 3 | A push to main lands a build in Play internal testing and installs on a real device | SHIP-26 |

**SHIP-15c is a 5 that was sized at closer to a 7, and the scale has no such number.** *How to read this* caps the scale at 5 and says nothing here is an 8, so the honest options were to round or to split. It is recorded as a 5 rather than split because its five parts are one argument — every one of them is a shared surface wave 2 opens, and a wave that took three of them and left the fourth would still have the collision the ticket exists to prevent. The rounding is noted here rather than hidden, because the milestone arithmetic is otherwise quietly wrong by two points.

## M1 — Identity and access

**Goal:** A person can register, verify, choose a role, and stay signed in across app restarts.  
**Size:** 29 tickets, 83 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-28 | users table migration | 2 | Table exists with email, phone, role, status, verification state | SHIP-7 |
| SHIP-29 | Password hashing with a modern KDF | 2 | Passwords stored with argon2id; no reversible storage anywhere | SHIP-28 |
| SHIP-30 | Registration endpoint | 3 | POST /v1/auth/register creates an unverified account and rejects duplicates | SHIP-29 |
| SHIP-30a | A user's name, collected at registration | 5 | Registration requires a name and `users` holds it in a column of its own; the app's registration screen collects it; and GET /v1/admin/users matches a search term against it — so all four of SHIP-151's terms answer on the wire rather than three | SHIP-30, SHIP-151 |
| SHIP-31 | Email verification token issue and storage | 2 | A single-use, expiring token is generated and stored on registration | SHIP-30 |
| SHIP-32 | Email sending adapter | 3 | Emails log to console in dev and send via the provider in staging | SHIP-8 |
| SHIP-33 | Email verification confirm endpoint | 2 | POST /v1/auth/verify-email marks the address verified and consumes the token | SHIP-31, SHIP-32 |
| SHIP-34 | Phone OTP issue and storage | 3 | A time-limited numeric OTP is generated, rate-limited, and stored hashed | SHIP-30 |
| SHIP-35 | SMS adapter for OTP delivery | 3 | OTP sends via the SMS provider in staging; logs to console in dev | SHIP-8 |
| SHIP-36 | Phone verification confirm endpoint | 2 | POST /v1/auth/verify-phone marks the number verified after a correct OTP | SHIP-34, SHIP-35 |
| SHIP-37 | Access token issue | 3 | Short-lived signed token carrying user ID, role, and expiry | SHIP-28 |
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

**SHIP-37 depended on SHIP-30 until wave 1, and no longer does.** Issuing a signed token is a pure function of a user id, a role, a session id and a clock, all of which exist once `users` does — the registration endpoint is the first *caller*, not a blocker. Under the rule in *How to read this*, that made it an earlier ticket rather than a real blocker, and the effect was to hold a two-day piece of work behind an endpoint that needs it. Amended to SHIP-28 so the identity foundation can be built alongside the endpoints that consume it. SHIP-39 and SHIP-44 continue to depend on SHIP-37, which is a real blocker in both cases.

**SHIP-30a re-opens a milestone that had been complete, and that is the honest reading rather than an accounting slip.** M1 stood at 28 of 28 from wave 4 until this row was written; it now stands at 28 of 29. **Its exit criterion is untouched** — "a person can register, verify, choose a role, and stay signed in across app restarts" needs no name and is still demonstrated on a real handset. What is short is a field two later tickets assumed: `users` has never had a name column, `000002_users` never had one and registration has never asked, and the only `name` columns anywhere in the schema are `admin_users.name` and `driver_assignments.driver_name` — neither of which is a user's.

**It is here rather than in M6 because a name cannot be backfilled.** SHIP-151 shipped searching users by email, phone and status, and its *Done when* says "email, phone, name, and status"; `Docs/11` §4 has carried it as partly met since. The gap is not the search — serving it is one more `OR` in `internal/admin/postgres_users.go` and one line in the contract — it is that nothing ever collected the value. That makes this registration's work, and the letter-suffix rule puts a ticket where it belongs rather than where its blockers sit.

**The forward edge to SHIP-151 is deliberate and is one of the six tickets *How to read this* counts.** The column, the endpoint, the contract and the app screen are all M1 work with backward dependencies; the one clause that needs SHIP-151 is the search term, and splitting the row in two to avoid one edge would leave SHIP-151 partly met with the closing half unowned — which is the exact shape this row exists to end. **The migration is a new one in the shared block (1–99), not an edit to `000002`**: `000005_users_role_is_immutable` is the precedent, and rewriting an applied migration breaks every database that has run it.

## M2 — Jobs

**Goal:** A verified customer can create, publish, amend, and cancel a job from the app.  
**Size:** 28 tickets, 84 points

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
| SHIP-65a | A job's status history served to its parties — `GET /v1/jobs/{id}/history`, auth class `RequireUser` | 3 | Every `job_status_history` row for one job is served oldest first with its actor, its reason and both clocks, in the Docs 10 §4.5 collection envelope; the owning customer and a provider holding a bid on that job each read it, anybody else gets exactly what a missing job gets, and the response carries no budget in any form | SHIP-57a, SHIP-65, SHIP-83a |
| SHIP-66 | Job list endpoint, customer view | 3 | Returns the customer's own jobs, filterable by status, paginated | SHIP-65 |
| SHIP-67 | Budget field stored and never serialised to providers | 3 | A provider-facing serialisation test proves the field cannot leak | SHIP-65 |
| SHIP-67a | Scheduled task runner | 3 | cmd/worker claims due work with FOR UPDATE SKIP LOCKED and survives running twice | SHIP-7 |
| SHIP-68 | Job expiry scheduled task | 5 | Open jobs close at the earlier of 14 days or the pickup date passing | SHIP-57, SHIP-67a |
| SHIP-69 | Expiry warning 48 hours ahead | 2 | A domain event fires 48 hours before a job would expire | SHIP-68 |
| SHIP-70 | Extend job expiry endpoint | 2 | A customer can extend an expiring job in one call | SHIP-68 |
| SHIP-70a | Expiry sweeps see a job with live offers, not only an Open one | 3 | Docs 02 §2's expiry row names the statuses a job can expire from, and both the expiry and the expiry-warning claims match it; a job that reached Negotiating expires on its own deadline rather than waiting for its last offer to lapse | SHIP-68, SHIP-69, SHIP-90 |
| SHIP-71 | Flutter job creation: locations step | 3 | Pickup and drop-off captured with validation and address lookup | SHIP-49, SHIP-60 |
| SHIP-72 | Flutter job creation: goods step | 3 | Category, description, dimensions, and weight captured | SHIP-71, SHIP-58 |
| SHIP-73 | Flutter job creation: schedule and vehicle step | 3 | Date window and vehicle requirement captured | SHIP-72 |
| SHIP-74 | Flutter job creation: budget and review step | 3 | Optional budget captured; full job reviewed before publish | SHIP-73 |
| SHIP-75 | Flutter draft save and resume | 3 | A partially completed job survives app restart and can be resumed | SHIP-74, SHIP-62 |
| SHIP-76 | Flutter customer job list | 3 | Customer sees their jobs grouped by status with pull-to-refresh | SHIP-49, SHIP-66 |
| SHIP-77 | Flutter customer job detail | 3 | Full job detail with status timeline and available actions | SHIP-76, SHIP-65 |

**SHIP-70a exists because SHIP-90 narrowed SHIP-68 and SHIP-69 without either ticket being reopened.** Both sweeps in `internal/jobs/expiry.go` claim `WHERE status = 'Open'`, which was the whole of "a live job" when they were written. Since SHIP-90, a job with one unanswered offer sits at `Negotiating` — so neither sweep can see it, and `Docs/02` §6.3's deadline stops being enforced on exactly the jobs somebody has bid on. **Nothing is lost, only delayed**: every live offer runs out at its own collection time under SHIP-89, the last one leaving returns the job to `Open`, and the next pass takes it. That is why this is a ticket rather than an incident.

**It is a document change before it is a code change, and that ordering is the ticket.** `Docs/02` §2 has one expiry row and it says `Open → Cancelled`. Widening the claim without widening that row would be resolving a contradiction silently in code, which `CLAUDE.md` forbids — so the *Done when* names the document first. Whoever takes it decides what `Negotiating → Cancelled` means for the offers on the job, which is a product question the sweep cannot answer for itself.

**Its dependency on SHIP-90 is a forward edge**, for the reason *How to read this* gives: the row sits where the work belongs, beside the two expiry tickets it corrects, rather than where its blocker sits. SHIP-90 is done, so nothing computed today changes.

**SHIP-65a is the row `Docs/11` §9 has been asking for since wave 5, and it is the reason SHIP-77 has sat in §4 longer than any other ticket.** `job_status_history` has recorded the actor, the reason and both clocks since SHIP-57a, append-only; `jobs.Service.History` reads it in Go; and **nothing exposes it over HTTP**. `GET /v1/jobs/{id}` answers the `Job` schema, which is `additionalProperties: false` and carries one `status`, `created_at` and `updated_at` — there is no `history` array anywhere on the served surface. So SHIP-77's "status timeline" is derived from the current status and written as a list of things the screen refuses to claim: it will not date a step it cannot date, and it will not tick a step `Docs/02` §2 permits a job to skip. That is the honest treatment of what exists, and it is not the clause being met.

**It is written in M2 rather than beside the screen, because the gap is a read and not a rendering** — the same call `Docs/11` §6 makes for SHIP-101a, SHIP-115a, SHIP-120a, SHIP-96a and SHIP-102a. What changes in `apps/mobile` is that `jobTimeline` takes the history and the steps gain their real times; nothing else about the screen moves.

**Its dependency on SHIP-83a is a forward edge and is a hard one rather than a placement convention.** `GET /v1/jobs/{id}/history` is a four-segment `GET` under `/v1/jobs/{id}/`, and while `GET /v1/jobs/open/{id}` exists both patterns match `/v1/jobs/open/history` with neither more specific — Go's `ServeMux` panics at registration and the process does not start. **Registering the intersection does not help.** The alternatives are all worse than waiting: a five-segment path, or a `POST` for a read. This row is therefore the fifth ticket to be shaped by that clash, and unlike the four before it — SHIP-115, SHIP-115a, SHIP-101a and SHIP-102a, each of which took a workaround — it declares the blocker instead. That is what SHIP-83a's "cheapest before there is a deployment" argument buys.

**The blocker cleared in wave 11 and this paragraph is now history, deliberately kept.** SHIP-83a moved the feed to `GET /v1/fleet/jobs` and `GET /v1/fleet/jobs/{id}` and **proved the freed slot by registering a probe route** rather than asserting it, so `GET /v1/jobs/{id}/history` is registrable today and SHIP-65a is startable. It is kept because it is the worked example of a row that declared a blocker instead of taking a workaround, and it is the only one of the five that did.

## M3 — Bidding and award

**Goal:** Providers discover eligible jobs, bid privately, negotiate, and a customer awards exactly one.  
**Size:** 39 tickets, 133 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-78 | vehicles table and fleet CRUD endpoints | 5 | A provider can add, edit, and deactivate vehicles | SHIP-44, SHIP-7 |
| SHIP-78a | Every fleet endpoint refuses a caller who is not a provider | 2 | All eight `internal/fleet` service methods refuse a customer with the same typed error `Add` and `Declare` already return, so a customer reaching the fleet surface is refused at the first call rather than at the last; a test drives every method with a customer subject and fails when any one of them answers instead of refusing | SHIP-78, SHIP-79 |
| SHIP-79 | Provider profile and service area | 3 | Provider declares service area and specialties; stored and queryable | SHIP-78 |
| SHIP-79a | A provider profile a customer may be shown | 3 | A customer reading an offer sees a provider summary that says something about the provider — beyond whether they are verified and when they joined — and none of it is the service area or specialties SHIP-102a forbids; the fields are a closed set, and the same set is what an admin read and the open feed disclose | SHIP-79 |
| SHIP-80 | bids table and status enum | 3 | Schema covers all eight bid statuses from Docs 02 §4 | SHIP-56 |
| SHIP-81 | Job eligibility filter query | 5 | Filters by service area, vehicle capability, verification state, and job status | SHIP-79, SHIP-80 |
| SHIP-81a | The provider verification record and its five states | 5 | Docs 04 §4's five outcomes — Pending, Verified, Restricted, Rejected and Suspended — exist as a provider's verification record in migration block 200–299 owned by `internal/profiles`; every transition passes one guarded function and records its actor and its reason, and no code sets the state directly; a provider reads their own state and no other provider's; and SHIP-81's eligibility predicate reads this record instead of its automated stand-in, so there is exactly one answer to who may bid | SHIP-79, SHIP-81 |
| SHIP-81b | The verification document record and its upload | 3 | Each of Docs 04 §3's four documents — licence, registration, insurance and ABN evidence — uploads directly through a short-lived pre-signed URL on SHIP-114's precedent, with the API never in the path of the bytes; each stored document names its kind and the verification record it belongs to, is private, and is reachable only by a fresh signed URL | SHIP-81a, SHIP-114 |
| SHIP-81c | A provider captures their verification documents in the app | 5 | A provider photographs each of Docs 04 §3's four kinds in features/profile/ and it reaches SHIP-81b's upload; the image is never written to the device photo library, is compressed on the device, and is cleared from app storage once uploaded; the capture helpers move to core/ under a name and a doc comment that no longer say proof, and land under a directory that is not proof/; and the camera purpose string covers this use in both the Dart constant and Info.plist | SHIP-81b |
| SHIP-81d | The verification fallback for a declined camera permission | 3 | A provider who refuses the camera permission still submits all four documents, through a picker that reaches no photo library — the file-upload fallback Docs 04 §3.1 requires so that a refused permission never blocks verification outright — and the package it depends on carries the written argument the gallery guard demands of anything in that space | SHIP-81c |
| SHIP-82 | Open jobs feed endpoint for providers | 3 | GET /v1/jobs/open returns only eligible jobs, paginated | SHIP-81 |
| SHIP-83 | Provider job detail with budget stripped | 3 | Provider view omits budget entirely; verified by test | SHIP-82, SHIP-67 |
| SHIP-83a | Move the open feed off the `{id}` slot | 3 | `GET /v1/jobs/{id}/<literal>` can be registered at four segments — demonstrated by registering one — and the provider feed answers on a path that no longer puts a literal where an identifier goes; routes_golden.txt, the contract fragment and the Dart client all move together and the old path is gone rather than aliased | SHIP-83 |
| SHIP-84 | Place bid endpoint | 3 | A verified, eligible provider can bid once per job with price and timing | SHIP-83 |
| SHIP-85 | Update bid endpoint | 2 | A provider can revise their own active bid | SHIP-84 |
| SHIP-86 | Withdraw bid endpoint | 2 | A provider can withdraw before acceptance; status becomes Withdrawn | SHIP-84 |
| SHIP-87 | Counter-offer endpoint for both parties | 5 | Customer and provider can counter; each counter supersedes the prior offer | SHIP-84 |
| SHIP-87a | `ck_bids_offer_has_timing` restored now that SHIP-87 has made the design | 2 | A bid past Draft cannot be written without both of its stated instants, enforced by a CHECK constraint rather than by a validator a worker or an admin path could bypass; `migrations/bids_test.go`'s fixtures are updated to state timing where they exercise `ck_bids_status`, and the constraint is demonstrated refusing a direct INSERT | SHIP-80, SHIP-87 |
| SHIP-88 | Offer supersede and history chain | 3 | Only the latest valid offer is acceptable; full chain remains readable | SHIP-87 |
| SHIP-89 | Bid expiry scheduled task | 3 | Bids expire on their own terms and emit an event | SHIP-84, SHIP-67a |
| SHIP-90 | Negotiating presentation status | 2 | A job with active offers presents as Negotiating without closing to new bids | SHIP-87, SHIP-57 |
| SHIP-91 | Database constraint: one accepted bid per job | 2 | A partial unique index makes a second accepted bid impossible at the database level | SHIP-80 |
| SHIP-92 | Award endpoint, transactional happy path | 5 | POST /v1/jobs/{id}/award accepts one bid and moves the job to Awarded in one transaction | SHIP-91, SHIP-88 |
| SHIP-93 | Award closes all competing bids atomically | 3 | Every other bid on the job becomes Rejected in the same transaction | SHIP-92 |
| SHIP-94 | Award idempotency | 3 | A retried award with the same key returns the original outcome, not an error | SHIP-92, SHIP-15 |
| SHIP-95 | Award concurrency test suite | 5 | Tests prove correctness under double award, withdraw-during-award, and expiry-during-award races | SHIP-93, SHIP-94 |
| SHIP-95a | Race test for the Negotiating presentation lock | 3 | An expiry sweep and an award are observed contending for the same job row — by polling pg_blocking_pids until PostgreSQL confirms it, as SHIP-95 does — and removing SKIP LOCKED from LeaveNegotiation fails the test rather than passing make check | SHIP-90, SHIP-95 |
| SHIP-96 | Bid history visibility rules | 3 | Customer, bidding provider, and admin each see only what Docs 02 §4 permits | SHIP-88 |
| SHIP-96a | A provider reads a job once it has left the open feed — `GET /v1/fleet/jobs/{id}`, auth class `RequireUser` | 3 | A provider holding any bid on a job, live or closed, and the provider awarded it, read that job in the same budget-stripped shape the open feed serves, for as long as the bid or the award exists; a provider with neither gets exactly what a missing job gets; the response carries no budget in any form | SHIP-83, SHIP-92, SHIP-96 |
| SHIP-97 | Job-scoped messaging between customer and provider | 5 | Messages attach to a job and are visible only to its two parties and admins | SHIP-84 |
| SHIP-98 | Flutter provider fleet management | 5 | Provider can manage vehicles from the app | SHIP-49, SHIP-78 |
| SHIP-99 | Flutter provider job feed | 3 | Provider sees eligible open jobs with filters | SHIP-98, SHIP-82 |
| SHIP-100 | Flutter provider job detail and bid placement | 3 | Provider can review a job and submit a bid | SHIP-99, SHIP-84 |
| SHIP-101 | Flutter provider bid list | 3 | Provider sees their own bids grouped by status | SHIP-100 |
| SHIP-101a | A provider reads their own bids — `GET /v1/fleet/bids`, auth class `RequireUser` | 3 | A provider lists every bid they have placed, grouped by status, paginated, and sees no other provider's; the response carries no customer budget in any form | SHIP-88, SHIP-66 |
| SHIP-102a | A customer reads the offers on their own job — `GET /v1/jobs/{id}/bids/received`, auth class `RequireUser` | 3 | The owning customer lists every live offer on one of their jobs in the Docs 10 §4.5 collection envelope with cursor pagination, each element carrying the offer's price and timing, a closed customer-facing provider summary and the vehicle it is offered with; a provider gets what a stranger gets; the response carries no budget in any form, and no provider's service area, specialties or other jobs | SHIP-84, SHIP-88, SHIP-66 |
| SHIP-102 | Flutter customer bid comparison | 5 | Customer compares price, timing, provider profile, and vehicle side by side | SHIP-77, SHIP-96 |
| SHIP-103 | Flutter negotiation and messaging UI | 5 | Both parties exchange messages and counter-offers against a job | SHIP-102, SHIP-97 |
| SHIP-104 | Flutter award confirmation flow | 3 | Customer awards a bid with explicit confirmation and sees the result | SHIP-102, SHIP-92 |

**SHIP-101a sorts after SHIP-101 and is its prerequisite, which is the one place in this milestone where the table's order is not the build order.** A letter suffix sorts immediately after its parent (see *How to read this*), and the read the screen needs was found after the screen was written: `Docs/11` §6 struck SHIP-101 as "every dependency met and unbuildable in fact" because nothing on the served surface lists a provider their own bids. Build SHIP-101a first. Its own dependency column points backwards, as every row's must; SHIP-101's is left naming SHIP-100 rather than rewritten, because the ticket it depends on for *data* is a different question from the one it depends on for *sequence*, and no tool computes the second.

**SHIP-102a is the second instance of the same shape, found the same way — by a client lane rather than by this file.** SHIP-102's *Done when* names four things to compare and **all four are unserved.** `routes_golden.txt` has `POST /v1/jobs/{id}/bids` and, as the only `GET` under that tree, `/v1/jobs/{id}/bids/{bid_id}/history` — which needs a bid identifier the customer would have to hold already. So price and timing have no source either, not merely the profile and the vehicle. `cmd/api/routes_bidding.go` **reserves** `/v1/jobs/{id}/bids` for SHIP-102 in three comments, and a reservation is not a route. `contracts/paths/fleet.yaml` says in as many words that the customer's view of a provider is "a separate schema arriving with SHIP-96" — and SHIP-96 shipped no route at all, by its own account: no migration, no `$ref`, no `routes_golden.txt` line.

**Its *Done when* names the disclosure rules in both directions, deliberately.** The customer's budget must not reach the response, which is the invariant every bidding read carries. The mirror is the one that is easy to miss: a **provider** summary rendered to a customer must not disclose what `contracts/paths/fleet.yaml` calls commercial information — the regions a provider covers and the work they specialise in — because that is a competitor's map of the market. So the row says the summary is a **closed** set of fields rather than "the provider's profile", which is the same argument SHIP-83 makes for the job view and the one wave 6 proved the hard way.

**SHIP-102a's row named `GET /v1/jobs/{id}/bids` until wave 10, and that path cannot be registered.** Measured in a standalone program rather than inferred: against the existing `GET /v1/jobs/open/{id}`, `net/http.ServeMux` panics because both match `/v1/jobs/open/bids` and neither is more specific. Renaming the literal to `/offers` panics identically. **Registering the intersection `GET /v1/jobs/open/bids` as a third route does not help either, in either registration order** — that is the escape hatch anybody reaches for and Go has no such rule. `POST /v1/jobs/{id}/bids` is unaffected only because the conflicting route is a `GET`, and five segments are safe, which is why the manifest already serves `GET /v1/jobs/{id}/bids/{bid_id}/history`. **The row now names `GET /v1/jobs/{id}/bids/received`, which is what the lane built and what `routes_golden.txt` carries.**

**The deciding factor was ownership rather than resource modelling, and that is the part to carry.** Two ways out were available: insert a segment under the job, as `internal/delivery` did with `/delivery/proof`, or move the open feed off the `{id}` slot, which frees the whole `/v1/jobs/{id}/<literal>` space for good. **The move is the better design and was not taken**, because it needs `contracts/paths/fleet.yaml`, `internal/fleet/http_test.go` and the fleet verify section — three files the lane did not own — and a four-line change spread across another lane's files is how a route gets dropped in a merge. **A path shape can therefore be decided by who holds which files in a given wave**, which is worth knowing before reading the manifest as though every path in it were a modelling decision. The move is now **SHIP-83a**.

**The route is `GET /v1/fleet/bids` rather than anything under `/v1/jobs/`, and that is a constraint rather than a preference.** `GET /v1/jobs/open/{id}` puts a literal in the `{id}` position, so it and any `GET /v1/jobs/{id}/<literal>` both match `/v1/jobs/open/<literal>` with neither more specific — Go's `ServeMux` panics at registration and the process does not start. A provider's own bids are a fleet-side collection anyway, beside `/v1/fleet/vehicles`.

**SHIP-96a closes two recorded gaps with one read, and writing it as one ticket is the point of the row.** The first is SHIP-129's, recorded in wave 7 by three lanes independently: nothing serves a job to the provider delivering it, so the milestone screen can show a job identifier and no address, no customer and no pickup window. The second is its mirror at the other end of the bid — `GET /v1/jobs/open/{id}` filters on `status IN ('Open','Negotiating')` and on the eligibility predicate, so "View the job" works while an offer is live and stops the instant the job is awarded, cancelled or expires. **The provider who wins a job loses their view of it by winning**, and every provider who lost it loses theirs in the same transaction. One read answers both, because both audiences are "a provider with a relationship to this job that is not eligibility".

**What the response is, and what it is not.** It is `openJobResponse` — SHIP-83's budget-stripped shape, held to a closed key set by three separate guards — with the *authorisation* predicate changed and nothing else: eligibility is replaced by "you hold a bid on this job, or you hold the award". `GET /v1/jobs/{id}` is the customer's job and carries their budget, and one shape with a redaction step somebody has to remember is the arrangement the privacy rule is hardest to keep with. It is **not** the delivery shelf: `GET /v1/jobs/{id}/delivery/detail` (SHIP-115a) serves the driver assignment and nothing about the job itself, which is why building it did not close this. `/v1/fleet/jobs/{id}` avoids the `ServeMux` panic the paragraph above describes, and sits beside `/v1/fleet/bids` where a provider's own things already live.

**SHIP-95a exists because the guard it tests is the strongest untested invariant on the board, and that was verified rather than suspected.** `LeaveNegotiation`'s `FOR UPDATE SKIP LOCKED` in `cmd/api/routes_bidding.go` is what keeps the `bids` → `jobs` lock order out of the sweep, against the award's `jobs` → `bids`; making it blocking reintroduces exactly the cycle SHIP-88's ordering exists to prevent, and `make check` exits 0 with the mutation applied. It is a **survivor by inspection**: a deadlock needs a sweep and an award racing, and a single-transaction test cannot produce that failure — one that appeared to would be testing something else. **The harness already exists.** SHIP-95 observes contention by holding a transaction open and polling `pg_blocking_pids` until PostgreSQL confirms the other backend is waiting, and wave 9 built the same shape for the delivery sweep in `TestTwoWorkersCompleteEachDeliveryExactlyOnce`. Three points is one race test per claim, extending that harness rather than writing a third.

**SHIP-83a is the structural fix four tickets have now paid a workaround for, and it gets cheaper the earlier it is made.** `GET /v1/jobs/open/{id}` (SHIP-83) puts a literal in the `{id}` position, so it and any four-segment `GET /v1/jobs/{id}/<literal>` both match `/v1/jobs/open/<literal>` with neither more specific, and Go's `ServeMux` panics at registration — the process does not start. **The workarounds are visible in the manifest**: SHIP-115 took `/delivery/proof`, SHIP-115a took `/delivery/detail` and `/delivery/milestones`, SHIP-101a went to `/v1/fleet/bids` rather than under the job at all, and SHIP-102a took `/bids/received`. Each is defensible on its own and the set is a shape nobody chose.

**Why it is a row rather than a note, and why it is three points.** It was recorded as a recommendation in `Docs/11` when SHIP-115 first met it, and three tickets have hit the same wall since — a recommendation with no owner is how a finding goes quiet, which this backlog has two worked examples of in SHIP-56a and SHIP-136. The cost today is one path in `contracts/paths/fleet.yaml`, one route file, `internal/fleet/http_test.go`, one Dart client method and one verify section. **It is a breaking contract change**, so it is cheapest before there is a deployment and before more clients bind to the path. The *Done when* asks for a four-segment `GET` under the job to be **registered**, not merely for the feed to move, because the whole point is the space it frees.

**SHIP-79a exists because a customer has nothing to be told about a provider, and no row anywhere creates it.** SHIP-102's *Done when* has the customer comparing "price, timing, provider profile, and vehicle", and the profile clause is servable today only in reduced form. Measured: `internal/profiles` holds `doc.go` and nothing else, and the only provider profile in the service is `fleet.Profile`, **whose two fields are the service area and the specialties — precisely what SHIP-102a's *Done when* forbids disclosing to a customer**, because they are a competitor's map of the market. There is no trading name, no rating and no completed-job count anywhere in the schema.

**It is placed at SHIP-79 rather than beside the screen, because the gap is data and not presentation.** A provider's public-facing identity is declared where they declare their service area; a screen can only render what exists. The row deliberately does **not** enumerate the fields — a rating implies a review mechanism nobody has specified and a completed-job count is a figure the platform can derive — and asks instead for a closed set, which is the discipline SHIP-83, SHIP-102a and the budget-privacy invariant all already impose. **Whoever reconciles the wave that lands SHIP-102 should add it to `Docs/11` §4 with this row as its named owner**, which is the shape SHIP-118 and SHIP-123 closed in and the shape SHIP-77 has never been able to reach. **That happened at the wave-10 reconciliation and `Docs/11` §4 now carries the row**, so this sentence is a closed instruction rather than a standing one.

**SHIP-81a and SHIP-81b exist because `Docs/04` §4's five verification outcomes are in no table, and four M6 rows worth 14 points are unbuildable until they are.** Measured on `develop` at `605ad3a`: the schema holds twenty-two tables and not one of them records a provider verification; **migration block 200–299 is entirely unused**; `internal/profiles` is `doc.go` alone, eleven waves after SHIP-10 created it. `000002_users.up.sql` says where the states belong in its own column comment — *"Provider verification state is separate and lives with profiles (Docs/04 §4)"* — and `users.status` is a different question with three values, `active`, `restricted` and `suspended`, which is account standing rather than eligibility to bid.

**SHIP-153's *Done when* is "pending provider verifications listed oldest first", and there is no pending provider verification anywhere to list.** SHIP-153 blocks SHIP-154, which blocks SHIP-155 and SHIP-159 — four rows and 14 points — and every one of them had every dependency met. **`internal/fleet/eligibility.go` names the ticket it expected to fix this and names the wrong one**: it says the document-review half of `Docs/04` §3 "arrives with SHIP-152…154". SHIP-152 shipped in wave 10 and is job and bid search; it built none of this, correctly, because nothing in its *Done when* asks for it.

**They are two rows rather than one, and the split is where `Docs/04` splits.** §4 is a *state* — five outcomes, a transition, an actor and a reason — and it is what SHIP-153's queue and SHIP-154's decision read. §3.1 is *evidence*, an in-app photograph of four documents uploaded straight to object storage, and it is what SHIP-155's viewer renders. The state is buildable on its own and unblocks three of the four M6 rows; the evidence needs SHIP-114's pre-signed upload, which is the forward edge in SHIP-81b's column and the same precedent SHIP-155 already depends on.

**SHIP-81a sits after SHIP-81 rather than beside SHIP-79, because it completes a predicate rather than a profile.** `internal/fleet/eligibility.go` says in as many words that its verification clause is *"deliberately incomplete"*, that it checks the automated half — email and phone confirmed on an account in good standing — and that **whatever completes it "adds its clause to this predicate. It must not add a second eligibility check elsewhere, or there will be two answers to who may bid."** That sentence is the *Done when*'s last clause. Placing the row before SHIP-81 would have made it a forward edge for no gain; placing it after keeps both its dependencies backward and puts it exactly where the code says the seam is.

**SHIP-78a is `Docs/11` §9's oldest ownerless finding**, open since wave 5 under two different measurements and given a ticket here rather than a seventh restatement. `isProvider` is called in `Add` and `Declare` and in no other method: `Update`, `Deactivate`, `Reactivate`, `Vehicle`, `Vehicles` and `Profile` do not check. **This is not a data leak and must not be reported as one** — every one of those methods scopes to the caller's own id, so a customer gets an empty list or a `404`, never another provider's vehicle. What it is is an endpoint declining to *refuse* somebody who has no business there, which is exactly what "the app may hide or disable; the platform decides" exists to make safe. SHIP-98 gated the surface on the device instead, which was the right call for that ticket and is a workaround for a platform gap rather than a fix for it.

**SHIP-87a is small and has been answerable for three waves.** `000501` removed `ck_bids_offer_has_timing` because it would have bound SHIP-87's design; SHIP-87 has landed, the design is made, and every offer this platform writes past `Draft` names both instants — so the constraint holds against the data the service produces today. It is two points rather than one because `000501`'s *other* finding still stands: it failed six of SHIP-80's own migration tests, which insert `Submitted` and `Accepted` bids with no timing in order to exercise `ck_bids_status`, so taking it means editing another ticket's fixtures. Worth taking, because a stated-timing rule enforced only by a validator is one a worker or an admin path can bypass.

## M4 — Delivery execution

**Goal:** A driver completes a delivery with proof, offline, through a link that needs no account.  
**Size:** 33 tickets, 112 points

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
| SHIP-115a | Delivery read shelf — `GET /v1/jobs/{id}/delivery/detail` and `GET /v1/jobs/{id}/delivery/milestones`, auth class `RequireUser`. **Both paths are five segments and must stay so** — see the note below the table | 3 | Both parties to a job read the driver assignment from `/delivery/detail` and every recorded milestone from `/delivery/milestones`; a stranger gets exactly what a missing job gets | SHIP-111, SHIP-115 |
| SHIP-116 | Proof exception reason capture | 3 | A reasoned exception can be recorded in place of a photo | SHIP-115 |
| SHIP-117 | Exception flags the job for moderation | 2 | An exception-completed job enters the moderation queue | SHIP-116 |
| SHIP-118 | Delivered validation requires proof or exception | 3 | Delivered is rejected without either; verified by test | SHIP-116, SHIP-57 |
| SHIP-119 | 72-hour auto-complete task | 3 | A Delivered job with no dispute becomes Completed after 72 hours | SHIP-118, SHIP-67a |
| SHIP-120 | Driver portal token landing and job view | 5 | Opening the link shows only that job's delivery detail | SHIP-23, SHIP-108 |
| SHIP-120a | `POST /v1/driver/jobs/{id}/milestones` — a driver records a milestone on the job-scoped token, auth class `RequireDriverToken` | 3 | A driver holding a job-scoped token records a milestone on that job and on no other; a mobile access token is refused on the route and the driver token is refused on `POST /v1/jobs/{id}/milestones` | SHIP-108, SHIP-111 |
| SHIP-121 | Driver portal milestone controls | 3 | Large touch targets record each milestone from a mobile browser | SHIP-120, SHIP-111 |
| SHIP-121a | A driver reads the milestones they recorded — `GET /v1/driver/jobs/{id}/milestones`, auth class `RequireDriverToken` | 2 | A driver holding a job-scoped token lists the milestones recorded on that job and on no other, so the portal's controls survive a page reload; a mobile access token is refused on the route, the driver token is still refused on `GET /v1/jobs/{id}/delivery/milestones`, and the response discloses nothing the driver's own job view does not already carry | SHIP-108, SHIP-111, SHIP-121 |
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
| SHIP-131a | The driver's own words on a milestone | 3 | A note typed on any screen that records a milestone — the Flutter proof-exception panel, the Flutter milestone screen and the driver portal — arrives as MilestoneRecording.reason and is read back on the customer's tracking view; it stays optional, and on the exception path it sits beside the selected reason rather than instead of one | SHIP-123, SHIP-129, SHIP-131 |
| SHIP-132 | Flutter offline conflict reconciliation UI | 3 | The user is shown clearly when a queued update lost to server state | SHIP-125, SHIP-113 |
| SHIP-133 | Flutter customer tracking view | 3 | Customer sees the latest confirmed milestone and proof of delivery | SHIP-77, SHIP-115 |

**SHIP-115a's two paths are five segments, and building it as four does not fail a test — it stops the process.** `GET /v1/jobs/{id}/delivery` is four (`v1 / jobs / {id} / delivery`) and so is `GET /v1/jobs/open/{id}`. Both match `/v1/jobs/open/delivery`, neither is more specific, and Go's `ServeMux` **panics at registration**: `make run` dies at startup rather than a route answering a 404 somebody debugs. So the row names `GET /v1/jobs/{id}/delivery/detail` and `GET /v1/jobs/{id}/delivery/milestones` rather than describing a shelf and leaving the shape to whoever builds it.

Five segments or more are safe, because the literal route has only three after `/v1` — which is exactly why SHIP-115's `GET /v1/jobs/{id}/delivery/proof` works and is the shelf this row extends. `Docs/11` §9 carries the structural fix, moving the open feed off the `{id}` slot, and it is not this ticket's.

**SHIP-120a exists because three separate lanes specified it in wave 7 and none built it**, each correctly finding it belonged to another lane's ticket. The auth class is in the row for that reason: `RequireDriverToken` is the whole of why the route is separate from `POST /v1/jobs/{id}/milestones`, which is `RequireUser` and always will be. Neither token may be exchanged for the other, so the driver's route is a second entry point to the same milestone service rather than a relaxation of the first one's guard. SHIP-121 is the driver portal's screen over it.

**SHIP-131a is a field the platform has always accepted and no client has ever sent.** `MilestoneRecording.reason` is in the published contract — optional, 500 characters, "what a person should know about this milestone that the milestone itself does not say" — and `internal/delivery` bounds and stores it. Measured across both client trees: the driver portal's `recordMilestone` takes `evidence`, `completion` and `recordedAt` and no note; the Flutter client sends `reason` on a job **cancellation** and on no milestone; and the Flutter tracking view *reads* one back and renders it. So the customer-facing surface can display a note that nothing in the product can write.

**Why it matters more on the exception path than anywhere else.** A reasoned exception is a *selection* from a closed list of three, which is what makes it enforceable and what `ck_proofs_exception_reason` pairs with — and a closed list is only triageable in `Docs/04` §5's delivery-exception queue if the driver can say which of the three it was and why. "The recipient asked me not to photograph their door" is the sentence that turns a queue entry into a decision, and today there is nowhere to type it. `Docs/11` §3's SHIP-131 entry names the gap and says it should be a ticket; this is it.

**It writes into two trees, and that is priced into how it should be scheduled rather than into the estimate.** SHIP-56a is the worked example: a ticket touching `services/core`, `apps/mobile` and `apps/driver-portal` at once lost to every ticket that opened one tree and was cut from six consecutive waves. This one opens no Go package at all — the platform half is already built — so it wants a wave in which one lane already holds `apps/mobile` or `apps/driver-portal`, or a serial slot. Do not defer it without writing down why; a deferral with no reason is how SHIP-56a went quiet.

**SHIP-121a is the fourth instance of the shape SHIP-101a, SHIP-115a, SHIP-120a, SHIP-96a and SHIP-102a all have** — a client ticket whose real precondition is a route on the served surface, which the dependency column does not record. Measured on `routes_golden.txt` at `605ad3a`: the driver surface is three routes, `GET /v1/driver/jobs/{id}` and two `POST`s, and **there is no milestone read on it at all**. So the portal's controls start at rest on every page view and a reload forgets what the last one did. **It costs a driver nothing they cannot recover from** — tapping a milestone twice is safe and `Docs/02` §5 makes a repeat an ordinary recording — which is why this is two points and a usability row rather than a correctness one.

**The route is deliberately a fourth entry on a surface whose narrowness is its security property**, so its *Done when* asks for the two refusals as well as the read: a mobile access token does not open it, and the driver token still does not open `GET /v1/jobs/{id}/delivery/milestones`. Five segments, so it registers today with no dependency on SHIP-83a.

## M5 — Notifications

**Goal:** Every essential event reaches the right person, without a notification failure losing the event.  
**Size:** 14 tickets, 48 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-134 | Transactional outbox table and publisher | 5 | Domain events commit with their transaction and publish at least once | SHIP-4, SHIP-57 |
| SHIP-134a | The outbox-to-topic check survives a concurrent worktree | 3 | scripts/verify/80-notifications.sh neither deletes a topic another run may be reading nor asserts set equality over one: its SHIP-134 comparison is subset-plus-completeness scoped to the outbox ids this run created, and the section passes while a second make verify publishes into the same broker — demonstrated by running two concurrently, not argued | SHIP-134, SHIP-135 |
| SHIP-135 | Kafka topics and event schema | 3 | Topics exist with a versioned schema for each domain event | SHIP-134 |
| SHIP-136 | Emit domain events from job, bid, and delivery transitions | 5 | Every state change in Docs 01 §4.5 emits its event from the domain, not the API layer | SHIP-135 |
| SHIP-137 | Notification consumer service | 5 | Consumer reads events, resolves recipients, and dispatches per channel | SHIP-136 |
| SHIP-138 | Email templates and dispatch | 3 | Each essential event has an email template and sends reliably | SHIP-137, SHIP-32 |
| SHIP-139 | Firebase Cloud Messaging adapter | 3 | Push dispatches to iOS and Android and handles token rejection | SHIP-137 |
| SHIP-140 | device_tokens table with register and deregister | 3 | Tokens bind to a device session and clear on sign-out | SHIP-38, SHIP-139 |
| SHIP-141 | Push content redaction rules | 2 | No address, goods description, or full customer name appears in a notification body | SHIP-139 |
| SHIP-142 | Notification preferences per user | 3 | A user can mute non-essential categories; essential events cannot be muted | SHIP-137 |
| SHIP-143 | Flutter push registration | 3 | Token registers after sign-in and de-registers on sign-out | SHIP-140, SHIP-50 |
| SHIP-144 | Flutter permission prompt at the right moment | 2 | Notification permission is requested contextually, never on first launch | SHIP-143, X-10 |
| SHIP-145 | Flutter deep link routing | 5 | Tapping a notification opens the exact job, bid, or dispute it concerns | SHIP-143, X-10 |
| SHIP-146 | Flutter notification inbox | 3 | In-app list of recent notifications with read state | SHIP-145 |

**SHIP-134a is an assertion-design ticket rather than a harness one, and that distinction is why it is a row rather than a lane's passing fix.** `80-notifications.sh` compares `consumed_sorted` against `outbox_sorted` — set **equality** over `shipper.job`. One broker serves every worktree on the machine, so a concurrent run's events land inside that set and the check fails on a tree where nothing is wrong; wave 9 saw it twice, identified by id in both cases. **No fence closes it.** Fencing narrows where a consumer starts reading and says nothing about what else arrives, so the equality has to become subset-plus-completeness over ids this run created — and somebody has to decide what completeness means once "everything on the topic" stops being the answer. That is a change to what a guard asserts, which is exactly the thing a lane must not weaken in passing.

**SHIP-144 and SHIP-145 depend on X-10, and until the wave-11 reconciliation they were the two rows on this table that read as startable and were not buildable.** Both need a push token, a token needs `firebase_messaging`, and `firebase_messaging` needs `firebase_core`, a `google-services.json` and a `GoogleService-Info.plist` — none of which exists, because **no row in this file asked anybody to create the Firebase project** until X-10 was written. SHIP-143 shipped its half against a `PushTokenSource` seam with **a test asserting the absence**, which is the honest form and is not a substitute for the thing. `Docs/11` §4 carries what SHIP-139 and SHIP-143 owe the same row; SHIP-140 owes it nothing, because a device token is an opaque string to this platform and its *Done when* is demonstrated end to end today.

**The deletion is the other half and is the reason a fence alone is not enough either.** The section deletes and recreates `shipper.job` on every run, and its own header says that is "safe today only because no other section asserts on a topic it did not create" — a justification scoped to *sections*, which does not survive a second worktree. A delete destroys the offsets a run-start fence captured, so the neighbouring run's subset check fails reporting its own ids as missing. Once the comparison is scoped to this run's ids the deletion has nothing left to buy, which is why the row asks for both in one change. `CLAUDE.md`'s worktree table carries the operational rule meanwhile: serialise `make verify` across trees.

## M6 — Administration and moderation

**Goal:** Support can see everything, act on it, and leave an auditable trail.  
**Size:** 22 tickets, 69 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-147 | Admin authentication, separate from user auth | 5 | Admin sign-in is independent and cannot be reached with a user token | SHIP-44, SHIP-22 |
| SHIP-147a | The platform's password cost is configured under its own name | 2 | One argon2id cost setting, named for the platform rather than for identity, with one environment prefix; the identity and admin hashers and deploy/.env.example all read the renamed setting, no second cost knob exists anywhere, and the release note says which variable a deployment must rename | SHIP-147 |
| SHIP-147b | An administrator's idempotency key is scoped to that administrator | 2 | Two administrator sessions sending the same `Idempotency-Key` with the same method, path and body to a `RequireAdmin` route each get their own execution and their own response rather than one replaying the other's, demonstrated by a test that fails when the scope reverts to today's user-only form; `internal/httpx` still imports no domain, so the administrator's identity reaches the scope through a function or an interface `httpx` declares itself and `cmd/api` supplies | SHIP-147, SHIP-44 |
| SHIP-148 | Admin roles and least-privilege permissions | 3 | Permissions are granular and default to the minimum | SHIP-147 |
| SHIP-149 | audit_log table and write helper | 3 | Append-only log capturing actor, action, target, timestamp, and reason | SHIP-7 |
| SHIP-150 | Audit every privileged action | 5 | All admin mutations write an audit entry; verified by test | SHIP-149, SHIP-148 |
| SHIP-151 | Admin user search | 3 | Search users by email, phone, name, and status | SHIP-147 |
| SHIP-152 | Admin job and bid search | 3 | Search and open any job with its full bid and status history | SHIP-151 |
| SHIP-153 | Admin verification queue | 3 | Pending provider verifications listed oldest first | SHIP-152, SHIP-81a |
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

**SHIP-147a sits in M6 because M6 is where the name became wrong, not because a configuration rename is administration work.** `Config.Identity.Argon2` and `IDENTITY_ARGON2_*` were accurate until SHIP-147: argon2id lived in `internal/identity` and the cost was that domain's. SHIP-15r moved the hashing to `internal/passwords` precisely so a second domain would not be the reason for a second implementation, and SHIP-147 then hashed an administrator's password with the same profile — correctly, because a second cost knob is `Docs/10` §3.4's failure one level up: two security parameters that agree by comment until somebody raises one. `cmd/api/routes_admin.go` says so in the comment beside the call and records that the field's name is now narrower than its meaning. The letter suffix puts the row at the ticket that made the statement false.

**It is two points and it gets more expensive, which is the argument for not letting it drift to M7.** The change is one section in `internal/config`, one environment prefix, two `cmd/api` call sites, `deploy/.env.example` and the configuration tests — and today no deployment exists to have `IDENTITY_ARGON2_*` set in its secret store. **The environment variable is an operational contract rather than an internal name**, so the ticket's *Done when* asks for the release note as well as the rename: after the first deployment this stops being a rename and becomes a migration somebody has to sequence. That is also why it is a ticket rather than a prep-branch edit — deciding whether to accept both names for one release is a decision, not a mechanical substitution.

**SHIP-147b exists because an administrator's idempotency key lands in `idem:v1:anonymous:<key>`, and it was found empirically rather than by reading.** A wave-10 lane watched a sign-in key collide with a key on a `RequireAdmin` route, which is only possible if the two share a scope. The mechanism is visible in `cmd/api/routes.go`: `httpx.Idempotent(idempotencyStore, httpx.SubjectScope)` is applied to the whole `/v1` group, wrapped by `ResolveSubject`, and `httpx.SubjectScope` returns `"user:" + s.UserID` only when `authctx.SubjectFrom` carries one. **An administrator never puts a subject there** — the session is resolved later, by the per-route `RequireAdmin` guard, inside the middleware that has already computed the scope. `cmd/api/adminauth_test.go` asserts that a `RequireAdmin` route produces no `authctx.Subject`, so the behaviour is pinned rather than incidental.

**This is not an open hole, and reporting it as one would be wrong.** `replayOrRefuse` fingerprints method, path and body, so reading another administrator's stored response means sending their exact request. What it is is a **weaker property than SHIP-44 established for users**, in the one place where the callers are the most privileged on the platform: two administrators who happen to generate the same key on the same mutation get one execution and one response, so the second administrator's action is silently skipped and they are told it succeeded. That is a correctness defect on a `RequireAdmin` write, not a disclosure.

**Its hard constraint is in the *Done when* because it is what makes the ticket non-trivial.** `internal/httpx` is infrastructure and imports neither a domain nor an adapter — `CLAUDE.md` calls this the rule easiest to break by accident and hardest to see afterwards, because every domain imports `httpx`. So the administrator's identity cannot reach `SubjectScope` by `httpx` importing `internal/admin`; it arrives through a function or an interface `httpx` declares itself, with `cmd/api` supplying the closure, exactly as `newAdminGuard` already does for the guard.

## M7 — Hardening and pilot readiness

**Goal:** The store prerequisites are met, the system is observable, and the release gate can be run.  
**Size:** 24 tickets, 72 points

| ID | Ticket | Pts | Done when | Depends on |
|---|---|---|---|---|
| SHIP-167 | Minimum supported version endpoint | 2 | GET /v1/app/minimum-version returns the floor per platform | SHIP-13 |
| SHIP-167a | Client policy endpoint for operational thresholds | 2 | GET /v1/app/policy serves the unsynced-nudge threshold and the proof compression budget; the app caches the last response and applies it with no connection, falling back to a compiled default only when it has never had one | SHIP-167, SHIP-127, SHIP-130 |
| SHIP-168 | Flutter launch-time version gate | 3 | A build below the floor blocks with an update prompt linking to the store | SHIP-167, SHIP-49 |
| SHIP-169 | Account deletion request endpoint | 3 | A signed-in user can request deletion and receives a completion date | SHIP-44 |
| SHIP-170 | Deletion deferral during an active job | 3 | A request during Awarded to Delivered queues until the job closes and explains why | SHIP-169, SHIP-57 |
| SHIP-171 | User record pseudonymisation | 5 | Profile and contact data are irreversibly replaced by a stable pseudonym | SHIP-170 |
| SHIP-171a | The platform can delete an object it stored | 3 | internal/platform/storage issues a delete for a key the platform named, demonstrated against the real object store rather than a fake, and the three places stating that this package has no delete say what changed and why; nothing belonging to a person is deleted by this row — it builds the call SHIP-172 makes | SHIP-171 |
| SHIP-171b | A pseudonymised account stops being a notification recipient | 3 | No notification is written addressed to a pseudonymised account and none already queued dispatches to one, so the attempts counter stops climbing on a person the platform has deleted | SHIP-171 |
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
| SHIP-183 | API-wide rate limiting review | 5 | Every endpoint on the manifest has a limit chosen and written down with the reason, including the ones deliberately left unlimited; Docs 01 states no rate-limiting requirement anywhere, so these numbers are decided here rather than derived from one; and the questions Docs 11 §9 parks on this row are answered rather than parked again | SHIP-47 |
| SHIP-183a | Apply the subject-keyed limits | 3 | Each of the 75 routes Docs 12 §5 keys on the authenticated subject enforces its class and answers the typed error with an honest Retry-After; a route registered without a limit fails a gate rather than defaulting to unlimited; the eleven address-keyed routes carry their class and are not yet enforced, which the gate accepts only because SHIP-183b is open | SHIP-183, SHIP-47 |
| SHIP-183b | Enforce the address-keyed limits behind a trusted proxy | 3 | The eleven routes Docs 12 §8 names enforce their class; the client address is read from a forwarded header only for a configured trusted-proxy hop count or CIDR allow-list, and from RemoteAddr otherwise, so no caller can choose their own bucket | SHIP-183a, SHIP-2 |
| SHIP-184 | Pilot-scale load smoke test | 3 | The stack handles expected pilot concurrency without error-rate degradation | SHIP-174 |
| SHIP-185 | Release gate run-through | 3 | Every condition in Docs 01 §8 is evidenced and signed off | SHIP-180, SHIP-181 |

**SHIP-167a exists because two shipped features carry a threshold that `CLAUDE.md` says belongs server-side, and neither could have it.** SHIP-127's four-hour unsynced nudge and SHIP-130's compression budget are both operational numbers — the kind "anything expected to change under operational pressure lives server-side" is written about — and **both fire on a handset that by assumption has no connection**. That is the premise of the features, not an oversight in them, so an endpoint fetched at the moment of use could never work.

What does work is an endpoint the app reads **while it still has signal** and keeps. SHIP-167's `GET /v1/app/minimum-version` is already exactly that shape — unauthenticated, per-platform, changed by configuration rather than by a release — which is why this row sits beside it and depends on it rather than inventing a second convention. The compiled default stays as the floor for an install that has never once been online, and is the *only* case it is used for; a build that has ever reached the platform uses what it was told.

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
| Full-time, focused | 20–25 | **29–37 weeks** (roughly 7–9 months) |
| Full-time, with interruptions | 15 | **~49 weeks** |
| Evenings and weekends | 6–8 | **93–124 weeks** (around two years) |

**The three rows are `floor(747 / rate)` and nothing else, and the total they divide is the one the rows carry.** They read 23, 29, 39, 74 and 99 until this pass, which is `floor(599 / rate)` exactly — so the table was not merely stale, it was arithmetic over a total the plan stopped having long ago, and the 599 was still printed under *If you need to cut scope* where it was easiest to read as current. Recompute all five figures whenever the point total moves — 29, 37, 49, 93 and 124 — together with the 747 under *If you need to cut scope*, which is the same total wearing prose. They are derived, not estimated.

These assume the point scale above and one experienced developer who already knows Flutter. They do **not** assume time spent learning Go, AWS, or Kafka — if any of those are new, add to M0 and M5 specifically.

Two things move this number more than working faster does: cutting scope (below), and not building the admin panel and driver portal yourself. Those two web surfaces are roughly 90 points of the total and are the most separable work in the plan.

## If you need to cut scope

747 points is a substantial solo build. These are the honest levers, in the order I would pull them:

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

- ~~**X-6** must be answered before SHIP-119 (the 72-hour auto-complete task) can be written correctly.~~ **Answered on 14 August 2026 — an exception-completed job auto-completes on §6.1's ordinary 72-hour rule.** The decision and its reasoning are in `Docs/02` §6.1. SHIP-119 is unblocked, and it needs no column that does not already exist.
- **X-4** must be answered before SHIP-172 (cascade deletion) can define what is retained. **SHIP-171 shipped without it**, and deliberately: replacing contact data with a pseudonym implicates no retention rule. Deleting verification evidence does — X-4's row is titled *retention period and verification documents*, and Docs 04 §3 has it deciding retention obligations — so the reasoning that let SHIP-171 proceed does not transfer to SHIP-172.
- **X-9** must be answered before SHIP-58 (goods categories) can load real reference data.

None of these blocks the start of its milestone; each blocks one specific ticket inside it.

**X-6 is the worked example of what "blocks one specific ticket" costs when nobody counts it.** It appears in no `Depends on` cell anywhere in this file, so SHIP-119 has been *dependency*-startable since SHIP-118 landed and `Docs/11` §6 has listed it as startable throughout. What actually held it was this list, which no tool reads. A reader working from the dependency column alone could never have seen why the ticket sat still for eight waves — which is the argument for keeping these three bullets where somebody reviewing the queue will meet them.

