# Shipper — Delivery Status

**Status:** Living document — update it in the same change that finishes a ticket  
**Audience:** Anyone picking the work up, including a session with no prior context  
**Purpose:** Say where the work actually is, so nobody has to reconstruct it from git.

## Read this first

`Docs/09` says what the work *is*. This says what is *done*. They are separate files on purpose: the backlog is a plan that rarely changes, and this changes every few days.

`make status` prints the machine-checkable half — which tickets have a commit claiming them. It cannot see nuance, so **this file is authoritative** for anything a commit subject does not capture: partly finished tickets, external blockers, and what is safe to start next.

**Last updated:** 2026-08-11, on SHIP-44 — the authentication middleware, which closes the idempotency-scope hole §8 has been carrying as a gate. It follows SHIP-15c, the wave-2 preparation, on the same day. §7 keeps wave 1's sequence rather than tidying it away.

---

## 1. Snapshot

| | Tickets | Points |
|---|---|---|
| **Done** | 38 | 93 |
| Remaining | 165 | 533 |
| **Total** | 203 | 626 |

The totals grew by two tickets rather than shrinking: SHIP-15c and SHIP-23a were added to `Docs/09` in the same change, both work the plan assumed and no ticket owned.

| Milestone | Done | Points |
|---|---|---|
| **X** External | 0 / 9 | 0 / 26 |
| **M0** Foundation | 27 / 32 | 66 / 82 |
| **M1** Identity | 7 / 28 | 18 / 78 |
| **M2** Jobs | 1 / 26 | 3 / 78 |
| **M3** Bidding and award | 0 / 27 | 0 / 95 |
| **M4** Delivery | 0 / 29 | 0 / 101 |
| **M5** Notifications | 0 / 13 | 0 / 45 |
| **M6** Admin | 1 / 20 | 3 / 65 |
| **M7** Hardening | 2 / 19 | 3 / 56 |

**M0 has five tickets left and only one of them is code.** SHIP-24…27 are store signing and upload, blocked on X-2 and X-3. SHIP-23a is the web CI that `Docs/09` now carries as a ticket rather than this file carrying it as a recommendation; it is unblocked and belongs to a client track.

**Wave 2 is prepared and its gate is cleared.** SHIP-15c closed the shared surfaces its three tracks would otherwise have met in, and SHIP-44 has landed — so the freeze on authenticated state-changing endpoints in §8 is lifted. The tracks can start.

**The published contract exists (SHIP-17a), and it is checked rather than believed.** `contracts/openapi.yaml` is assembled from per-domain fragments under `contracts/paths/`, and three tests in `cmd/api` hold it to the service: the manifest and the contract must agree in both directions, live handler responses must satisfy the published schemas, and the error contract must match the `Error` schema for failures that `net/http` writes rather than a handler. That closes `TestEveryRouteIsInTheContract`, the last of the three route-surface guards in `Docs/10` §4.1 to become enforceable.

**The first domain logic exists, and it is in exactly one package.** `internal/identity` holds argon2id password storage and access-token issue; the other seven domains still contain `doc.go` and nothing else. Everything outside `identity` remains foundation and adapters.

That distinction is worth keeping in mind rather than rounding away: `identity` now has code that other domains will want to call, and the rule that stops them calling it directly — one domain never imports another — has its first real opportunity to be broken from here on.

**All four deployables now exist and run.** The Flutter client builds and runs on both simulators, the two Next.js surfaces build and serve, and the Go service serves `/health` to the app. Nothing above the foundation is wired to a real endpoint yet — the app fetches `/health` and nothing else — but the shape `Docs/08` describes is on disk rather than in a document.

## 2. Branch state

| Branch | At | Holds |
|---|---|---|
| `main` | PR #19 | **Wave 1, released 11 August 2026.** Level with `develop` |
| `develop` | PR #18 | Everything below. **Cut new branches from here** |

**Wave 1 has been released.** PR #19 brought 38 commits and 16 tickets across, and `main` is no longer behind `develop` on content. This was the first `develop → main` batch since wave 0, which is by design: releases go in release-sized batches rather than one per ticket.

The previous version of this section said `develop` was at PR #17 and 36 commits ahead. It was #18 and 38 by the time anyone read it, and that is not an error to guard against — a commit cannot record the number of the pull request that merges it. **The numbers in this table are always one update behind reality, and the fix is to correct them in the next update rather than to try to make them self-aware.**

`main` still shows commits `develop` does not have. Those are the detour, not divergent work: wave 0 reached `main` by being merged (PR #6), reverted (PR #7), and reapplied (PR #9), and PR #19's own merge commit sits on `main` alone. The content is identical; only the shape of the history differs.

### The wave-1 branches, in merge order

| PR | Branch | Brought |
|---|---|---|
| #11 | `ship-15b-wave-1-prep` | SHIP-15b, and SHIP-37's dependency amendment |
| #12 | `ship-15a-close-mobile-decisions` | `Docs/07` §9 closed, `Docs/10` §8.3 reconciled |
| #13 | `ship-22-35-web-and-adapters` | SHIP-22, 23, 32, 35, 59a |
| #14 | `ship-29-38-credentials-and-sessions` | SHIP-29, 37, 38, and the SHIP-149 verify fix |

`ship-16-21-flutter-foundation` exists and is parked at PR #11's merge, holding nothing. It is the branch the Flutter track resumes on — see §7.

**A warning worth keeping.** Reverting a merge does not undo it: the commits stay ancestors forever, so re-merging the same branch brings nothing across and reports success. If a merge to `main` is ever reverted again, the fix is to revert *the revert*, not to merge again.

**PR #19 had exactly the shape that warning describes, and was checked rather than trusted.** The revert `d733c95` sits on `main` and is *not* in `develop`'s ancestry — the setup where a merge silently resurrects deletions. It was safe only because the reapply `c1cb64b` had already restored the content, leaving the revert nothing to take away. The check that established this before merging is worth reusing on any release whose history has a revert in it:

```
git merge-tree --write-tree main develop   # the tree the merge would produce
git rev-parse develop^{tree}               # the tree develop actually has
```

Identical hashes mean the merge result is exactly `develop`'s content. Different hashes mean something is being dropped or added, and the release needs looking at before it is cut, not after.

## 3. Done

Verified by `make verify` — **66 checks**, and `make check` green.

`make verify` covers the foundation tickets it was written for. Work that reaches no HTTP
endpoint is demonstrated by its own tests instead and says so in the row: the wave-1
adapters have none to demonstrate — nothing consumes them until SHIP-33, SHIP-36 and
SHIP-60 — and the two web surfaces are demonstrated by `make web-build` and `make web-dev`.

The Flutter client is demonstrated by `make flutter-check` — the analyzer, the tests, and the
environment test run once per build flavour — and SHIP-16 and SHIP-19 by installing the built
`.app` and `.apk` on an iPhone 17 simulator and a Pixel_10a emulator and reading the API
version off both screens. `make verify` does not cover it: that script exercises HTTP
endpoints, and none of these tickets adds one.

### M0 — Foundation (27 of 32)

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
| **SHIP-15c** | Wave-2 shared surfaces: pre-seeded `Deps`, worktree test isolation, a fourth boundary rule, `httpx.RegisterCode` — *see below* |
| **SHIP-16** | Flutter scaffold — iOS and Android only, floors at iOS 14.0 and Android API 24 |
| **SHIP-17** | Feature folders per `Docs/07` §2, Riverpod and `go_router`, and a boundary test |
| **SHIP-17a** | `contracts/openapi.yaml` from per-domain fragments, and three tests holding it to the service |
| **SHIP-18** | `dio` client — three environments by `--dart-define`, and the Android emulator's host |
| **SHIP-19** | Health round trip — the API version on screen, on both simulators |
| **SHIP-20** | Go CI — build, boundaries, spelling, tests, migration round trip |
| **SHIP-21** | Flutter CI — analyzer, tests, per-flavour environment tests, codegen diff |
| **SHIP-22** | Admin panel scaffold — pnpm workspace, Next.js App Router, placeholder shell |
| **SHIP-23** | Driver portal scaffold — placeholder job page, no token route, no account |

### Elsewhere

| Ticket | Milestone | What |
|---|---|---|
| **SHIP-28** | M1 | `users` — citext email, phone, role, status, verification timestamps |
| **SHIP-29** | M1 | argon2id password hashing, parameters stored in the PHC string |
| **SHIP-32** | M1 | Email adapter — console in development, generic HTTP provider in staging |
| **SHIP-35** | M1 | SMS adapter — same shape, and the OTP is legible in the dev log on purpose |
| **SHIP-37** | M1 | Access token issue — HS256, keyset by `kid`, fifteen minutes, no permissions in the token |
| **SHIP-38** | M1 | `device_sessions` — hashed refresh state, device label, last seen |
| **SHIP-44** | M1 | Authentication middleware — the auth class is now enforced, and idempotency keys are scoped by caller — *see below* |
| **SHIP-59a** | M2 | Geocoding adapter — deterministic stub, and not-found is an outcome, not an error |
| **SHIP-149** | M6 | `audit_log`, append-only enforced by trigger — *see §4* |
| **SHIP-167** | M7 | `GET /v1/app/minimum-version`, configuration-driven |
| **SHIP-179** | M7 | Camera and notification purpose strings, and a test that stops them drifting |

SHIP-149 and SHIP-167 were pulled a long way forward deliberately. Audit is impossible to backfill, and the version gate cannot be retrofitted to builds already on devices — so it has to exist before SHIP-25 puts anything on one.

### What SHIP-15a built

This is the part a new session most needs to know about, because it changes how everything after it is written.

| Mechanism | Where | What it prevents |
|---|---|---|
| Route manifest + golden file | `cmd/api/manifest.go`, `routes_golden.txt` | A route dropped in a merge — which produces no compile error |
| Reserved migration blocks | `migrations/blocks.go` | Two branches drawing the same migration number |
| ~~Domain-local error codes~~ | ~~`httpx.RegisterCode`~~ | **Documented but never built. Built at SHIP-15c — see below** |
| Template-cloned test databases | `internal/testsupport/pgtest` | Parallel packages trampling each other's rows |
| Fail-not-skip | `pgtest`, `redistest` | Tests passing by being skipped |
| Pre-seeded infrastructure list | `internal/boundaries` | Domain branches editing the boundary file |
| Config/`.env.example` agreement | `internal/config/documented_test.go` | An undocumented environment variable |
| `COMPOSE_PROJECT_NAME` pinned | `Makefile` | Worktrees each starting a stack and fighting over ports |

**One row of that table was false for two waves, and it is struck through rather than deleted.** `httpx.RegisterCode` was documented in `Docs/10` §4.4 with a worked example and listed here as built, and neither was true — there was no registration function and no generated code list. Nothing broke, because no domain had raised a code of its own. That is the failure mode worth remembering: a mechanism this file claims exists is one nobody checks for, and it stays absent exactly until two tracks need it in the same week.

Shared packages now available to every domain: `db` (Runner, InTx), `authctx`, `clock`, `validate`, `events` (outbox writer). Registered but not yet written: `pagination`, `ratelimit`, `money` — write them when first needed, no shared edit required.

### What SHIP-15b changed, and why it existed at all

The Australian English check word-matched case-insensitively over every tracked file, and had never seen Dart or TypeScript because none existed. Flutter's `Center` and `color:` matched `centre` and `colour`; Tailwind's `items-center` matched too. **Track B and Track C would have failed CI on their first commit**, before writing a line of their own. <!-- spelling:ok — naming the exempted identifiers -->

The check now has two scopes — `Docs/10` §9.3. Words that cannot be a framework symbol still apply everywhere, `apps/**` included, because `cancelled` is a job status and `licence` is what a provider is verified against; the dozen that are framework API names apply everywhere else. A single unrenameable identifier takes a `spelling:ok` line waiver instead of an exclude, which is how the `Authorization` header will be handled in Go at SHIP-44.

It is a ticket rather than an untracked commit because `Docs/09` says a letter suffix marks work the plan assumed and no ticket owned. This was exactly that, found by reading the lint rather than by CI going red.

### What SHIP-15c built, and what it found

Same mould as SHIP-15b, one wave later and larger. Wave 1's three tracks produced exactly one conflict between them because nothing they wrote met in a shared file. Wave 2 is the first wave that adds HTTP endpoints, and its tracks would meet in five: the `Deps` struct and its literal, the `paths:` block, `routes_golden.txt`, the `Load()` literal, and `.env.example`.

| Built | Where | What it prevents |
|---|---|---|
| Pre-seeded `Deps` — pool and Redis client | `cmd/api/manifest.go`, `main.go` | Three tracks each adding a field to one struct and a line to one literal |
| Per-worktree test template, derived not set | `Makefile`, `pgtest` | One worktree's `make test` dropping another's template mid-clone |
| Fourth boundary rule: infrastructure imports no domain, no adapter | `internal/boundaries` | One import in `httpx` welding all eight domains to `identity` |
| `httpx.RegisterCode` + generated `Docs/10-api-error-codes.md` | `internal/httpx`, `cmd/api` | Two tracks inventing two error taxonomies in one week |
| `merge=union` on the golden file; a written recipe for the `paths:` block | `.gitattributes`, `contracts/openapi.yaml` | A conflict resolved by choosing a side, which drops an endpoint silently |
| `CHECKS` as a variable | `Makefile` | A track's checks being unreachable from `make check` |

**Three things were found while preparing, none of them known before.**

**The worktree test isolation did not work the way four places said it did.** `CLAUDE.md`, `deploy/.env.example`, `Docs/10` §7.1 and `pgtest`'s own failure message all named `TEST_DATABASE_URL` as the mechanism. It never was: `CREATE DATABASE … TEMPLATE` resolves at cluster scope, and `COMPOSE_PROJECT_NAME` is pinned so every worktree shares one cluster deliberately. What isolates is `TEST_TEMPLATE_DB`. Worktree `shipper-wave1-b` had none — recorded reasoning: a Flutter track writes no Go tests — but `make check` runs `make test`, which builds the template first. One `make check` there dropped the primary tree's template.

**`make test-db-template` also had a live data-loss path.** It rewrote the migration URL with `$(subst /$(POSTGRES_DB)?,…)`, a literal that matches nothing on a URL with no query string and then runs every migration into the developer's real database. Latent here only because all four `.env` files carry `?sslmode=disable`. It now rewrites the URL path and refuses to run if the rewrite changed nothing.

**The import lint had a hole shaped exactly like the next ticket.** Its three rules were all about domains and adapters, so infrastructure could import a domain and the lint stayed green — and because every domain imports `httpx`, one such import couples all eight transitively through a file no domain contains. SHIP-44 is the first ticket with a motive. The rule needed no refactoring to adopt, which is the moment to add one.

**Deliberately out of scope: splitting `scripts/verify-foundation.sh`.** It is 660 lines in one file with no include mechanism, and it is the sixth shared surface. Wave 2's track split gives it exactly one client — only the identity track appends — so it can wait. Named here so wave 3 treats it as a decision rather than a discovery.

### What SHIP-44 built, and what it closed

The gate §8 has been describing since wave 1. Three points, and it held back every authenticated state-changing endpoint in five milestones.

**The hole was real and is now closed.** `httpx.Idempotent` was wired with `scope == nil`, so every stored response landed in `idem:v1:anonymous:<key>`. Harmless while nothing was authenticated; the moment something is, a client that guesses another client's key is handed that client's response body — somebody else's job, address or bid. `httpx.SubjectScope` namespaces by caller, and `make verify` now demonstrates it against a running service: two callers, one key, identical body, two separate Redis entries.

**`Route.Auth` was decoration until now.** `attach` ignored it entirely and the class was enforced only by `TestNoMutatingRouteIsPublic` — a test that a route was *declared* correctly, not that the declaration did anything. It is now read at wiring time, and **a class with no middleware behind it panics at startup rather than being served open.** That is the state `RequireDriverToken` (SHIP-108) and `RequireAdmin` (SHIP-147) are in today.

**Authentication is deliberately two middlewares, and that is worth knowing before somebody tidies it.** `ResolveSubject` runs group-wide, outside `Idempotent`, and never rejects; `RequireSubject` runs per route and does. Both halves are forced:

- The scope must be computed after the subject exists, so resolution has to sit further out than idempotency.
- `POST /v1/auth/refresh` is public and is called by exactly the client whose access token has just expired. A middleware that refused a bad credential on sight would lock that client out of the endpoint that replaces it.

**`token_expired` is a new protocol code**, and the sixteenth. Expiry is the one authentication failure a client acts on differently — refresh rather than sign out — and reporting it precisely discloses nothing, because `exp` sits in the payload the client already holds. Every other failure is one undifferentiated `unauthenticated`: naming which check refused a credential is help only somebody probing has a use for.

**SHIP-15c's own acceptance criterion is now demonstrated.** SHIP-44 landed with no field added to `Deps` and no edit to its literal in `main.go`. The verifier is passed to `newRouter` alongside the idempotency store, because it is a collaborator of the router rather than something a handler is built from.

**What is not demonstrated by `make verify`: the 401 itself.** There is no protected route in the service yet — SHIP-44 is the middleware, not an endpoint — so the rejection paths are covered by tests in `internal/httpx` and `cmd/api` rather than by curl. The first route with `Auth: RequireUser` is where that section gets written.

## 4. Partly done — do not treat these as finished

| Ticket | Exists | Missing |
|---|---|---|
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

Strict build order says the next ticket is the lowest-numbered open one. With SHIP-15c and SHIP-44 done that is **SHIP-23a**, and the lowest-numbered *platform* one is **SHIP-30**. These all have satisfied dependencies:

| Ticket | Pts | Area |
|---|---|---|
| SHIP-30 | 3 | Registration endpoint — `users` and argon2id both exist now |
| SHIP-31, 34 | 5 | Email verification token, phone OTP issue and storage |
| SHIP-39 | 5 | Refresh token rotation — `device_sessions` exists, and it needs an expiry column |
| SHIP-48 | 3 | Flutter secure storage — `apps/mobile` exists now, and API 24 was chosen for it |
| SHIP-56 | 3 | `jobs` table |
| SHIP-67a | 3 | `cmd/worker` scheduler |
| SHIP-23a | 2 | Web CI, path-filtered per surface — written at SHIP-15c |
| ~~SHIP-114~~ | 5 | **Not buildable as written — see below** |

**SHIP-114 is on this list no longer, and it is not blocked either.** Its dependencies are met, but `internal/platform/storage/` is `doc.go` alone and `deploy/docker-compose.yml` has no MinIO or equivalent. Its *Done when* — "receives a short-lived pre-signed URL and uploads directly" — cannot be demonstrated on this machine, and wave 1 already paid for counting a ticket whose acceptance criterion needs a tool nobody installed. **It needs a lettered ticket adding object storage to the local stack first**, as shared-platform work. Until then it is neither ready nor blocked on a third party, which is a third category this file did not have.

**SHIP-44 has landed, and the way it was nearly missed is worth keeping.** Its dependencies — SHIP-37 and SHIP-12 — had both been done since wave 1, but §8 described it as a hard gate *ahead*, which reads as future work, and that is why it sat out of this list while blocking five milestones. A ticket described only as a constraint on other work stops being read as work itself.

**The identity package is the real constraint on the next wave, not the gate.** SHIP-30, 31, 34, 39, 40, 41, 42, 43, 44 and 45 all live in `internal/identity`, and `Docs/10` §9.1 gives one package directory to one agent at a time. That is roughly thirty points on one track no matter how many agents are available. Parallelism in wave 2 has to come from elsewhere — `jobs` (SHIP-56), `cmd/worker` (SHIP-67a), the Flutter client (SHIP-48), and SHIP-23a. Storage is not one of the options, for the reason above.

**Public routes still share the anonymous scope, and that remains safe.** `replayOrRefuse` fingerprints method, path and body, so reading another caller's stored response requires sending their exact request — which, on every route on `Docs/10` §4.1's allow-list, means already holding the secret material in their body. `make verify` checks that the anonymous scope still works, because scoping idempotency into uselessness would be a subtler regression than leaving it shared.

## 7. Wave 1 — what landed

Fourteen tickets across four branches. All three tracks completed, but not all at once: the Flutter track was blocked mid-wave, deferred, and finished after the block cleared.

| Track | Planned | Landed |
|---|---|---|
| **A** Go platform | SHIP-29, 38, 37 | All three, plus a fix to SHIP-149's verify section |
| **B** Flutter | SHIP-16, 17, 18, 19, 21, 179 | All six — **after a pause, see below** |
| **C** Web + adapters | SHIP-22, 23, 32, 35, 59a | All five |

The exit criterion was "M0 complete except SHIP-24…27; the app shows the API version from `/health` on both simulators; email and SMS log to console; a signed access token can be issued and verified."

**All four now hold**, with one correction to the first: M0 is complete except SHIP-24…27 **and SHIP-17a**, which the criterion overlooked. SHIP-17a is not blocked by anything; it was simply not in any track.

### Track B was deferred, then unblocked

This section previously read "Track B landed **None**", and that was true when it was written. Keeping the sequence visible is the point — a tracker that is quietly rewritten to look like the plan always worked teaches nothing.

**What happened.** The machine had no Flutter SDK, no Dart, no Android SDK and no CocoaPods, and carried Xcode **Command Line Tools** rather than Xcode, so `xcodebuild` and `simctl` both refused and there was no simulator to run anything on. Every *Done when* in the track is a build-or-run criterion. Writing the code anyway would have produced six tickets nobody could honestly mark done — the failure `Docs/10` §7.2 describes in another key: work counted without being demonstrated.

So the track was cut down to the part needing no toolchain — `Docs/07` §9's open decisions, closed **in `Docs/07`**, where `Docs/10` §10 had already spent a wave claiming they were. Riverpod, Drift, and the OS floors, with the reasoning.

**Then the toolchain was installed** — Xcode 26.6 with the iOS 26.5 runtime, Flutter 3.44.9, CocoaPods, Android SDK 36 with cmdline-tools, licences, an `arm64-v8a` system image and a Pixel AVD — and the full track ran on `ship-16-21-flutter-foundation` and landed all six tickets.

**The deferral paid for itself.** Because the decisions were already closed in `Docs/07`, the track that resumed had nothing left to argue about and went straight to code.

**One defect worth naming**, because it would have survived to a store review: Flutter's template declares `INTERNET` only in the debug and profile manifests, for hot reload. The release manifest had no network permission at all, so every screen would have worked in development and the shipped app would have failed to reach the API.

### Decisions taken during the wave

These were settled before the tracks started, so that three agents did not answer them three ways. They stand unless a ticket revisits them deliberately.

| Question | Decision |
|---|---|
| SHIP-37's dependency | Amended from SHIP-30 to SHIP-28 — §6, and the note under `Docs/09`'s M1 table |
| Minimum OS versions | **iOS 14.0, Android API 24** — argued in `Docs/07` §9 |
| Email, SMS and geocoding vendor | **None yet.** Each `provider.go` speaks a generic HTTP contract over `net/http`. The vendor is named at SHIP-33, SHIP-36 and SHIP-60 |
| Australian English under `apps/**` | The spelling check has two scopes — `Docs/10` §9.3 |

### What wave 1 taught, for the next one

- **Concurrent tracks worked, and the mechanism that made them work was file ownership decided in advance.** Two branches produced one conflict between them, in this file, in two places — both unions where nothing could be dropped silently. Nothing collided in code at all.
- **`go.mod` was never touched**, because the dependencies each track needed were already direct. That was luck as much as planning; a wave whose tracks each need a new module has a `go.sum` conflict waiting, and `Docs/10` §9.2 is right that adding one is a request rather than a commit.
- **A ticket for the shared surface is worth cutting up front.** SHIP-15b existed because the spelling lint would have failed both client tracks on their first commit — found by reading the lint, not by CI going red. A wave that opens a new kind of source file should expect one of these.
- **`make verify` is where a ticket stops being believed and starts being demonstrated**, and it caught its own defect this wave: two SHIP-149 checks had been passing without reaching the trigger they name.
- **A blocked track is worth splitting rather than stalling or faking.** Track B could not demonstrate a single *Done when* without a toolchain, but the decisions in `Docs/07` §9 needed no toolchain at all. Doing that part first meant the resumed track argued about nothing.
- **Check the tools before planning the wave, not after dispatching it.** The Flutter toolchain was missing from the start; finding that out at dispatch cost a re-plan mid-wave. `flutter doctor` — or its equivalent for whatever a track needs — belongs in the readiness check beside the dependency check.

### Wave rules, unchanged

- **A ticket may never depend on another ticket in the same wave on a different track.** A Flutter screen consumes an endpoint from the *previous* wave.
- Every branch: `make check` green, caught up to `develop`, one logical change per commit, closing with the *Done when* line.
- Merge into `develop` with `--no-ff`, never squash. Owner only.
- Tracks do not touch §1, §2 or §7 of this file — three agents doing the same arithmetic on one table is a guaranteed conflict, and `make status` computes the real numbers anyway. Each track adds its own tickets to §3 and the §10 fence; the rest is reconciled once when the wave lands.

## 8. Hard gates ahead

**~~SHIP-44 is a choke point.~~ Cleared — see §3.** `httpx.Idempotent` is wired with `httpx.SubjectScope`, and the freeze on authenticated state-changing endpoints is lifted. `make verify` demonstrates the separation against a running service rather than asserting it.

Kept here rather than deleted, because the shape recurs: this was described only as a constraint on *other* work, and so was never read as work itself while its dependencies had been met since wave 1. A gate with satisfied dependencies belongs in §6 the moment it becomes buildable.

**The next gate of the same kind is SHIP-108, and it has no entry yet.** The driver's job-scoped token is a second verifier, and `Docs/10` §5 requires that neither token system can be exchanged for the other. `identity.AccessTokenVerifier` refuses the driver audience today and there is a test for it — but the other direction cannot be tested until the driver verifier exists. **Whoever writes SHIP-108 writes both directions**, which is the reason this file has always kept both verifiers with one owner.

**One thing SHIP-44 did not do: `RequireDriverToken` and `RequireAdmin` are declarable and unenforced.** A route declaring either now panics at startup rather than being served open, so the failure direction is safe. SHIP-108 and SHIP-147 supply the middleware.

**SHIP-91…95 never parallelise.** Own branch, nothing else on it. The partial unique index, the lock ordering, the idempotency interaction and the race tests are one design; two people produce two lock orderings, which is a deadlock or a lost update. Consider using a second agent adversarially instead — one implements 91–94, another writes SHIP-95 from `Docs/02` §3 and `Docs/08`'s four named races *without reading the implementation*.

**Also single-owner, for reasons in `Docs/10`:** SHIP-57 (the status guard), SHIP-67 with SHIP-83 (budget privacy — test the serialised response, not struct fields), both token verifiers, and the middleware ordering in `newRouter` — which is now load-bearing in a second way, since `ResolveSubject` sitting outside `Idempotent` is what makes the scope work at all.

## 9. Open recommendations nobody has decided

**Job status as a database guarantee.** `CLAUDE.md` says job status is never a settable field, but nothing structurally stops a future `postgres.go` writing `UPDATE jobs SET status = …`, and the boundary lint will not catch it. A `BEFORE UPDATE` trigger rejecting any status change without a session variable set inside the guard's transaction would make it a database guarantee — the same argument as enforcing one-accepted-bid in the database. **Decide at SHIP-57.**

**Whether an adapter's value types get a home.** Wave 1 surfaced a consequence of the consumer-declares-the-interface rule that nobody had hit before. A domain's `ports.go` must name the adapter's method signature and may not import the adapter, so no struct declared in an adapter can appear in one. Geocoding therefore ended up as:

```go
Lookup(ctx context.Context, address string) (lat, lng float64, formatted string, found bool, err error)
```

and not-found is comma-ok rather than a sentinel error, because `errors.Is(err, geocoding.ErrNotFound)` would also be an import. The reasoning is correct and the lint agrees. But a neutral infrastructure package holding a coordinate type — the same shape as the pre-seeded `pagination`, `ratelimit` and `money` — would let both sides name it with no dependency edge either way, and that option was unavailable only because `internal/boundaries` was a forbidden shared edit mid-wave. **Decide at SHIP-60, before three domains adopt the wide signature.**

**~~`httpx.RegisterCode` is documented but does not exist.~~ Decided and built at SHIP-15c.** The registry, the uniqueness tests in `cmd/api`, and the generated `Docs/10-api-error-codes.md` all exist; `Docs/10` §4.4 is now true and says so, including that it was not. The choice was between building the mechanism and amending the document to match reality, and building won because SHIP-30 and SHIP-57 both need it on separate tracks in the same wave.

**~~A ticket for the web CI workflows.~~ Written as SHIP-23a at SHIP-15c.** Two points, path-filtered per surface, dependencies SHIP-22 and SHIP-23 both met. It belongs to a client track rather than to platform work.

**`scripts/verify-foundation.sh` is the sixth shared surface, and it has no include mechanism.** 660 lines in one file, and every ticket with an HTTP acceptance criterion appends to it. SHIP-15c left it alone deliberately: wave 2's split gives it exactly one client, so splitting it would have been a large change to a shared file for no benefit this wave. **It stops being deferrable the moment two tracks both add endpoints** — which is wave 3. Split it the same way `mk/*.mk` is split, or accept a conflict in the one file that demonstrates every acceptance criterion.

**`device_sessions` has no expiry column.** SHIP-38's *Done when* named refresh state, device label and last seen, and the implementation stopped exactly there — correctly, as a scope decision. But a refresh token has to expire, so **SHIP-39 either adds the column or explains where expiry lives instead.** Flagged here so it is a decision rather than a discovery.

**The mobile bundle identifier has no owner and stops being changeable.** `apps/mobile` currently uses a provisional `au.com.shipper` for both the iOS bundle id and the Android application id. **Once X-2 and X-3 publish a build, neither can be changed** — a new identifier is a new app listing, with a new install base. Confirm it before SHIP-25 or SHIP-27, not after. The staging and production hostnames baked into the API client (`api.staging.shipper.com.au`, `api.shipper.com.au`) are provisional in the same way, though those are only configuration; `SHIPPER_API_BASE_URL` overrides them meanwhile.

**`Docs/07` §8 requires staging and production installable on one device, and the client cannot do that yet.** SHIP-18 selects the environment with `--dart-define`, which changes the base URL but not the identifier, so the second build replaces the first. Holding both at once needs a distinct application id per environment — real Xcode and Gradle flavours. **That work belongs with SHIP-24…27**, which are blocked on X-2 and X-3 anyway, but it is not currently in any of their *Done when* lines.

**`scripts/check-spelling.sh` only sees tracked files.** It searches with `git grep`, so a newly created file passes the check until it is staged — which let one through during wave 1. Cheap to fix in the reader rather than the script: run `make lint-spelling` after `git add`, not before. Worth a line in `Docs/10` §9.3, which is where somebody would look.

## 10. The done list, in a form a script can read

Authoritative. `make status` counts these and cross-checks them against the backlog and
against what commit subjects claim. Add a ticket here in the same change that finishes it.

A ticket belongs here only when its *Done when* line in `Docs/09` is demonstrable. The two
in §4 are deliberately absent.

```done
SHIP-1 SHIP-2 SHIP-3 SHIP-4 SHIP-5 SHIP-6 SHIP-7 SHIP-8 SHIP-9
SHIP-10 SHIP-11 SHIP-12 SHIP-13 SHIP-14 SHIP-15 SHIP-15a SHIP-15b SHIP-15c SHIP-16 SHIP-17 SHIP-17a SHIP-18 SHIP-19 SHIP-21
SHIP-20 SHIP-22 SHIP-23 SHIP-28 SHIP-29 SHIP-32 SHIP-35 SHIP-37 SHIP-38 SHIP-44
SHIP-59a SHIP-149 SHIP-167 SHIP-179
```

## 11. Keeping this file honest

Update it in the same change that finishes a ticket. A tracker maintained afterwards is a tracker that is wrong — and this repository has already demonstrated the failure mode: `CLAUDE.md` described the project as sitting at SHIP-9 for the entire time SHIP-10 to SHIP-15 was being written.

`make status` compares three things: the backlog, the list in §10, and the tickets named by commit subjects. It fails when §10 claims something git has never seen, and warns when git has seen something §10 does not mention. It reads subjects only, so a commit finishing two tickets while naming one under-reports — which is why §10 is authoritative and the git side is a check on it rather than the source.
