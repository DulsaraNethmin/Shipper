# Shipper — Delivery Status

**Status:** Living document — update it in the same change that finishes a ticket  
**Audience:** Anyone picking the work up, including a session with no prior context  
**Purpose:** Say where the work actually is, so nobody has to reconstruct it from git.

## Read this first

`Docs/09` says what the work *is*. This says what is *done*. They are separate files on purpose: the backlog is a plan that rarely changes, and this changes every few days.

`make status` prints the machine-checkable half — which tickets have a commit claiming them. It cannot see nuance, so **this file is authoritative** for anything a commit subject does not capture: partly finished tickets, external blockers, and what is safe to start next.

**Last updated:** 2026-08-10, preparing wave 1 — SHIP-15b landed, SHIP-37's dependency was amended, and §2's branch table was corrected after describing a position it had already left.

---

## 1. Snapshot

| | Tickets | Points |
|---|---|---|
| **Done** | 21 | ~50 |
| Remaining | 180 | ~569 |
| **Total** | 201 | 619 |

| Milestone | Done | Points |
|---|---|---|
| **X** External | 0 / 9 | 0 / 26 |
| **M0** Foundation | 18 / 30 | 43 / 75 |
| **M1** Identity | 1 / 28 | 2 / 78 |
| **M2** Jobs | 0 / 26 | 0 / 78 |
| **M3** Bidding and award | 0 / 27 | 0 / 95 |
| **M4** Delivery | 0 / 29 | 0 / 101 |
| **M5** Notifications | 0 / 13 | 0 / 45 |
| **M6** Admin | 1 / 20 | 3 / 65 |
| **M7** Hardening | 1 / 19 | 2 / 56 |

**No domain logic has been written.** All eight packages under `services/core/internal/` still contain `doc.go` and nothing else. Everything done so far is foundation.

## 2. Branch state

| Branch | At | Holds |
|---|---|---|
| `main` | PR #9 | Everything below **except this file** |
| `develop` | PR #10 | Everything below. **Cut new branches from here** |

`develop` is three commits ahead of `main` — the PR that added this tracker had not landed when the paragraph you are reading first described the position, which is the failure mode §11 is about, caught one document earlier than usual. `main` does not have `Docs/11-delivery-status.md` at all. Everything else is content-identical.

Their histories differ because wave 0 reached `main` through a detour — merged directly (PR #6), reverted (PR #7), reapplied (PR #9). That is resolved; `develop → main` merges normally from now on.

**A warning worth keeping.** Reverting a merge does not undo it: the commits stay ancestors forever, so re-merging the same branch brings nothing across and reports success. If a merge to `main` is ever reverted again, the fix is to revert *the revert*, not to merge again.

## 3. Done

Verified by `make verify` — **47 checks**, and `make check` green.

`make verify` covers the foundation tickets it was written for. Work that reaches no HTTP
endpoint is demonstrated by its own tests instead and says so in the row: the wave-1
adapters have none to demonstrate — nothing consumes them until SHIP-33, SHIP-36 and
SHIP-60 — and the two web surfaces are demonstrated by `make web-build` and `make web-dev`.

### M0 — Foundation (18 of 30)

| Ticket | What |
|---|---|
| SHIP-1…4 | Monorepo, PostgreSQL 17, Redis 7, Kafka 3.9 (KRaft) |
| SHIP-5, 6 | Go service, graceful shutdown, `/health` with build info |
| SHIP-7 | Migration tool, migrations embedded in the binary |
| SHIP-8, 9 | Environment configuration, structured JSON logging |
| SHIP-10, 11 | Eight domain packages, five adapters, the import lint |
| SHIP-12, 13, 14 | Error contract, `/v1` group, request-ID propagation |
| SHIP-15 | Redis idempotency middleware, fail-closed |
| **SHIP-15a** | Conventions (`Docs/10`) and the shared-surface mechanisms |
| **SHIP-15b** | The spelling check scoped for client code — *see below* |
| **SHIP-20** | Go CI — build, boundaries, spelling, tests, migration round trip |

### Elsewhere

| Ticket | Milestone | What |
|---|---|---|
| **SHIP-28** | M1 | `users` — citext email, phone, role, status, verification timestamps |
| **SHIP-32** | M1 | Email adapter — console in development, generic HTTP provider in staging |
| **SHIP-149** | M6 | `audit_log`, append-only enforced by trigger — *see §4* |
| **SHIP-167** | M7 | `GET /v1/app/minimum-version`, configuration-driven |

SHIP-149 and SHIP-167 were pulled a long way forward deliberately. Audit is impossible to backfill, and the version gate cannot be retrofitted to builds already on devices — so it has to exist before SHIP-25 puts anything on one.

### What SHIP-15a built

This is the part a new session most needs to know about, because it changes how everything after it is written.

| Mechanism | Where | What it prevents |
|---|---|---|
| Route manifest + golden file | `cmd/api/manifest.go`, `routes_golden.txt` | A route dropped in a merge — which produces no compile error |
| Reserved migration blocks | `migrations/blocks.go` | Two branches drawing the same migration number |
| Domain-local error codes | `httpx.RegisterCode` | Every domain editing one registry |
| Template-cloned test databases | `internal/testsupport/pgtest` | Parallel packages trampling each other's rows |
| Fail-not-skip | `pgtest`, `redistest` | Tests passing by being skipped |
| Pre-seeded infrastructure list | `internal/boundaries` | Domain branches editing the boundary file |
| Config/`.env.example` agreement | `internal/config/documented_test.go` | An undocumented environment variable |
| `COMPOSE_PROJECT_NAME` pinned | `Makefile` | Worktrees each starting a stack and fighting over ports |

Shared packages now available to every domain: `db` (Runner, InTx), `authctx`, `clock`, `validate`, `events` (outbox writer). Registered but not yet written: `pagination`, `ratelimit`, `money` — write them when first needed, no shared edit required.

### What SHIP-15b changed, and why it existed at all

The Australian English check word-matched case-insensitively over every tracked file, and had never seen Dart or TypeScript because none existed. Flutter's `Center` and `color:` matched `centre` and `colour`; Tailwind's `items-center` matched too. **Track B and Track C would have failed CI on their first commit**, before writing a line of their own. <!-- spelling:ok — naming the exempted identifiers -->

The check now has two scopes — `Docs/10` §9.3. Words that cannot be a framework symbol still apply everywhere, `apps/**` included, because `cancelled` is a job status and `licence` is what a provider is verified against; the dozen that are framework API names apply everywhere else. A single unrenameable identifier takes a `spelling:ok` line waiver instead of an exclude, which is how the `Authorization` header will be handled in Go at SHIP-44.

It is a ticket rather than an untracked commit because `Docs/09` says a letter suffix marks work the plan assumed and no ticket owned. This was exactly that, found by reading the lint rather than by CI going red.

## 4. Partly done — do not treat these as finished

| Ticket | Exists | Missing |
|---|---|---|
| **SHIP-29** | `password_hash` column on `users` | argon2id itself. No hashing code anywhere |
| **SHIP-149** | `audit_log` table, append-only triggers, tests | The Go write helper its title names |
| **SHIP-134** | `outbox` table, `internal/events` writer | The publisher. That is M5 and stays there |

## 5. Blocked — and only by work outside this repository

**Nothing in Track X has started.** Nine tickets, none of them code, all of them slow.

| Ticket | Gates |
|---|---|
| **X-1** D-U-N-S number | X-2, and through it SHIP-24, 25 |
| **X-2** Apple Developer Program | SHIP-24 (iOS signing), SHIP-25 (TestFlight) |
| **X-3** Google Play Console | SHIP-26 (Android signing), SHIP-27 (Play internal) |
| **X-4** Legal brief | X-7, X-8, X-9 — and SHIP-171, 172 |
| **X-5** Pilot metro area | Provider recruitment. No code |
| **X-6** Proof-exception auto-complete | SHIP-119 |
| **X-7** Privacy policy URL | SHIP-180, 181 |
| **X-8** Terms and provider agreement | Pilot users |
| **X-9** Prohibited-goods list | SHIP-58 |

**X-1 → X-2 is the longest pole in the entire plan.** Apple's organisational enrolment commonly takes one to two weeks *after* a D-U-N-S number issues, and obtaining one that does not exist adds more. Every other M0 ticket proceeds without it, so starting costs nothing and waiting costs weeks of finished code with nowhere to put it.

X-5 and X-6 need no third party at all — they are decisions somebody can make this week.

## 6. Ready to start now

Strict build order says the next ticket is the lowest-numbered open one, **SHIP-16**. But these all have satisfied dependencies, which is what makes concurrent tracks possible:

| Ticket | Pts | Area |
|---|---|---|
| SHIP-16 | 2 | Flutter scaffold |
| SHIP-17a | 3 | `contracts/openapi.yaml` |
| SHIP-22, 23 | 4 | Next.js admin, driver portal |
| SHIP-29 | 2 | argon2id |
| SHIP-32, 35 | 6 | Email and SMS adapters |
| SHIP-37 | 3 | Access token issue — *see below* |
| SHIP-38 | 2 | `device_sessions` |
| SHIP-56 | 3 | `jobs` table — unblocked now `users` exists |
| SHIP-59a | 3 | Geocoding adapter |
| SHIP-67a | 3 | `cmd/worker` scheduler |
| SHIP-114 | 5 | Object storage, pre-signed upload |

**SHIP-37's dependency has been amended, and the backlog now says so.** It listed SHIP-30, the registration endpoint. Issuing a signed token is a pure function of a user id, a role, a session id and a clock, all of which exist once `users` does — the endpoint is the first *caller*, not a blocker, and `Docs/09` is explicit that *Depends on* lists real blockers rather than merely earlier tickets. Amended to SHIP-28 in the wave-1 prep change, with the reasoning recorded under the M1 table. This paragraph previously said "treat it as blocked unless you decide otherwise"; it was decided.

## 7. The next wave

Three concurrent tracks, roughly 27 points. One agent per Go package directory, always.

| Track | Tickets | Branch |
|---|---|---|
| **A** Go platform | SHIP-29, 38, 37 | `ship-29-38-credentials-and-sessions` |
| **B** Flutter | SHIP-16, 17, 18, 19, 21, 179 | `ship-16-21-flutter-foundation` |
| **C** Web + adapters | SHIP-22, 23, 32, 35, 59a | `ship-22-35-web-and-adapters` |

A and C need no coordination: `identity/ports.go` declares what it needs of an email sender, Go satisfies interfaces structurally, and neither package imports the other.

Track B must also close the two decisions `Docs/07` §9 leaves open — state management and local persistence — **in `Docs/07`**, not only in code. `Docs/10` §8.3 records the recommendation (Riverpod, go_router, Drift); it needs confirming. Note that §9's first decision is compound: it bundles the minimum supported iOS and Android versions with the state approach, and `Docs/10` §8.3 settles only the state half. Both halves close in `Docs/07`.

**Exit criterion:** M0 complete except SHIP-24…27. The app shows the API version from `/health` on both simulators. Email and SMS log to console. A signed access token can be issued and verified.

### Decided before the wave, so three tracks do not decide it three ways

| Question | Decision |
|---|---|
| SHIP-37's dependency | Amended to SHIP-28 — §6 |
| Minimum OS versions | **iOS 14.0, Android API 24.** Deliberately above `flutter_secure_storage`'s `EncryptedSharedPreferences` floor of API 23, because `Docs/07` §3 puts the refresh token in the Keystore and API 21–22 falls back to something weaker |
| Email, SMS and geocoding vendor | **None yet.** Each `provider.go` speaks a generic HTTP contract over `net/http`, taking base URL, key and sender as its own options. `Docs/06` §4.1's stated pattern is that the seam exists before the vendor does; the vendor is named at SHIP-33, SHIP-36 and SHIP-60, which are the tickets that first send anything real |
| Australian English under `apps/**` | The spelling check now has two scopes — `Docs/10` §9.3. Without it, `Center(` and `color:` fail CI on Flutter's first commit <!-- spelling:ok — naming the exempted identifiers --> |

### Who owns what, this wave

Nothing in the last row is edited by any track. If a ticket appears to need one, that is a finding to report, not a file to open.

| Surface | Owner |
|---|---|
| `internal/identity/**`, the identity migration block, `internal/config/**`, `deploy/.env.example` | **A** |
| `internal/platform/{email,sms,geocoding}/**`, `apps/admin/**`, `apps/driver-portal/**`, `mk/web.mk` | **C** |
| `apps/mobile/**`, `.github/workflows/flutter.yml`, `mk/flutter.mk`, `Docs/07` §9 | **B** |
| `scripts/verify-foundation.sh` | A, then C — which is why **A merges before C** |
| `cmd/api/**`, `routes_golden.txt`, `go.mod`, `go.sum`, the root `Makefile`, `internal/httpx/**`, `internal/boundaries`, `CLAUDE.md` | **nobody** |

Three things make that hold. `go.mod` needs no change — `golang-jwt/jwt/v5` and `golang.org/x/crypto` are already direct dependencies, so SHIP-29 and SHIP-37 add nothing and Track C uses `net/http`. Track C stays out of `internal/config` because its adapters have no consumer until SHIP-33, SHIP-36 and SHIP-60, so there is nothing to configure yet. And **no route is registered anywhere in this wave** — SHIP-29 is a library, SHIP-38 a table, SHIP-37 a token issuer — so `routes_golden.txt` is untouched and the SHIP-44 gate in §8 is not approached.

Tracks do not touch §1 or §2 of this file either: three agents doing the same arithmetic on one table is a guaranteed conflict, and `make status` computes the real numbers anyway. Each track adds its own tickets to §3 and the §10 fence, and §1, §2 and §7 are refreshed once when the wave lands.

### Wave rules

- **A ticket may never depend on another ticket in the same wave on a different track.** A Flutter screen consumes an endpoint from the *previous* wave.
- Every branch: `make check` green, caught up to `develop`, one logical change per commit, closing with the *Done when* line.
- Merge into `develop` with `--no-ff`, never squash. Owner only.

## 8. Hard gates ahead

**SHIP-44 is a choke point, and the reason is a live security hole.** `httpx.Idempotent` is wired with `scope == nil`, so every key lands in `idem:v1:anonymous:<key>`. Harmless while nothing is authenticated; the moment something is, a client that guesses another client's key gets that client's response body. **No authenticated state-changing endpoint may merge before SHIP-44 supplies the authenticated subject.** During that wave exactly one agent touches `cmd/api` and `internal/httpx`.

**SHIP-91…95 never parallelise.** Own branch, nothing else on it. The partial unique index, the lock ordering, the idempotency interaction and the race tests are one design; two people produce two lock orderings, which is a deadlock or a lost update. Consider using a second agent adversarially instead — one implements 91–94, another writes SHIP-95 from `Docs/02` §3 and `Docs/08`'s four named races *without reading the implementation*.

**Also single-owner, for reasons in `Docs/10`:** SHIP-57 (the status guard), SHIP-67 with SHIP-83 (budget privacy — test the serialised response, not struct fields), both token verifiers, and the middleware ordering in `newRouter`.

## 9. Open recommendation nobody has decided

`CLAUDE.md` says job status is never a settable field, but nothing structurally stops a future `postgres.go` writing `UPDATE jobs SET status = …`, and the boundary lint will not catch it. A `BEFORE UPDATE` trigger rejecting any status change without a session variable set inside the guard's transaction would make it a database guarantee — the same argument as enforcing one-accepted-bid in the database. **Decide at SHIP-57.**

## 10. The done list, in a form a script can read

Authoritative. `make status` counts these and cross-checks them against the backlog and
against what commit subjects claim. Add a ticket here in the same change that finishes it.

A ticket belongs here only when its *Done when* line in `Docs/09` is demonstrable. The three
in §4 are deliberately absent.

```done
SHIP-1 SHIP-2 SHIP-3 SHIP-4 SHIP-5 SHIP-6 SHIP-7 SHIP-8 SHIP-9
SHIP-10 SHIP-11 SHIP-12 SHIP-13 SHIP-14 SHIP-15 SHIP-15a SHIP-15b
SHIP-20 SHIP-28 SHIP-32 SHIP-149 SHIP-167
```

## 11. Keeping this file honest

Update it in the same change that finishes a ticket. A tracker maintained afterwards is a tracker that is wrong — and this repository has already demonstrated the failure mode: `CLAUDE.md` described the project as sitting at SHIP-9 for the entire time SHIP-10 to SHIP-15 was being written.

`make status` compares three things: the backlog, the list in §10, and the tickets named by commit subjects. It fails when §10 claims something git has never seen, and warns when git has seen something §10 does not mention. It reads subjects only, so a commit finishing two tickets while naming one under-reports — which is why §10 is authoritative and the git side is a check on it rather than the source.
