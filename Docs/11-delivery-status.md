# Shipper — Delivery Status

**Status:** Living document — update it in the same change that finishes a ticket  
**Audience:** Anyone picking the work up, including a session with no prior context  
**Purpose:** Say where the work actually is, so nobody has to reconstruct it from git.

## Read this first

`Docs/09` says what the work *is*. This says what is *done*. They are separate files on purpose: the backlog is a plan that rarely changes, and this changes every few days.

`make status` prints the machine-checkable half — which tickets have a commit claiming them. It cannot see nuance, so **this file is authoritative** for anything a commit subject does not capture: partly finished tickets, external blockers, and what is safe to start next.

**Last updated:** 2026-08-12, closing wave 3 — seventeen tickets across three lanes, plus the serial pre-step (SHIP-15e) that made them possible. This is the reconciliation pass the wave rules reserve: §1, §2, §6 and §7 are left alone by each track while it runs, precisely so that three agents do not do the same arithmetic three ways, and are corrected once when the wave lands.

---

## 1. Snapshot

| | Tickets | Points |
|---|---|---|
| **Done** | 70 | 188 |
| Remaining | 135 | 448 |
| **Total** | 205 | 636 |

The totals grew by two tickets rather than shrinking: SHIP-15c and SHIP-23a were added to `Docs/09` during wave 2, both work the plan assumed and no ticket owned.

They grew again by one in wave 3: **SHIP-15e** (M0, 5 points), the serial pre-step that split `scripts/verify-foundation.sh` into a harness plus one file per domain and moved the done list out of this file. Totals moved 203 → 204 tickets and 626 → 631 points.

And once more before wave 4: **SHIP-15g** (M0, 5 points), which took 205 tickets and 636 points. Same pattern a fourth time — a shared surface two different tracks had each parked work against, plus a defect neither could fix from a domain branch. **This is now the norm rather than the exception**, and the useful reading is that a wave costs one prep ticket: the pre-step is not overhead the process has failed to eliminate, it is the process. Budget for the next one rather than being surprised by it.

| Milestone | Done | Points |
|---|---|---|
| **X** External | 0 / 9 | 0 / 26 |
| **M0** Foundation | 30 / 34 | 78 / 92 |
| **M1** Identity | 26 / 28 | 71 / 78 |
| **M2** Jobs | 11 / 26 | 33 / 78 |
| **M3** Bidding and award | 0 / 27 | 0 / 95 |
| **M4** Delivery | 0 / 29 | 0 / 101 |
| **M5** Notifications | 0 / 13 | 0 / 45 |
| **M6** Admin | 1 / 20 | 3 / 65 |
| **M7** Hardening | 2 / 19 | 3 / 56 |

**M0 has four tickets left and not one of them is code.** SHIP-24…27 are store signing and upload, blocked on X-2 and X-3. Every buildable M0 ticket is done, for the fourth time — **SHIP-15g** was the last one, and like SHIP-23a and SHIP-15e before it, it was a recommendation in §9 and §3 before it was a ticket.

**Wave 3 has landed in full.** Three lanes, seventeen tickets, forty-eight points, **no trim taken**, and exactly one conflict in the entire wave — one line of this file holding a number. §7 has the detail, including why the `contracts/openapi.yaml` collision the whole pre-step was designed around never materialised.

**A person can now register, verify, sign in, and stay signed in — M1 is 26 of 28.** Email token and phone OTP are issued on registration, both are legible in the development log on purpose, both are confirmed through their own endpoint, and the role is fixed at registration and immutable afterwards — enforced by a database trigger, not by application logic. Wave 3 added the rest of the session: sign-in, sign-out, refresh-token rotation with reuse detection that invalidates the whole device session, an explicit sliding expiry on `device_sessions`, the device list and its revoke, and rate limiting that charges failed sign-ins only and fails **closed** when Redis is down. All of it is demonstrated by `make verify`, not asserted.

**Only SHIP-50 and SHIP-55 remain in M1, and both are Flutter.** The platform half of identity is finished; what is left is the client's token-refresh interceptor and its login screen. M1's exit criterion — "a person can register, verify, choose a role, and stay signed in across app restarts" — is met server-side and awaits those two on the device.

**The jobs table constrains all twelve statuses, and status is not a settable field.** A `BEFORE UPDATE` trigger refuses any change not described by a `job_status_history` row written in the same transaction, so the guard, the record and the transaction are one condition. `cmd/worker` claims due work under `FOR UPDATE SKIP LOCKED` and survives being run twice.

**The published contract exists (SHIP-17a), and it is checked rather than believed.** `contracts/openapi.yaml` is assembled from per-domain fragments under `contracts/paths/`, and three tests in `cmd/api` hold it to the service: the manifest and the contract must agree in both directions, live handler responses must satisfy the published schemas, and the error contract must match the `Error` schema for failures that `net/http` writes rather than a handler. That closes `TestEveryRouteIsInTheContract`, the last of the three route-surface guards in `Docs/10` §4.1 to become enforceable.

**Domain logic now lives in two packages, not one.** `internal/identity` holds argon2id password storage, access-token issue, refresh rotation and the session surface; `internal/jobs` holds the location value object, the store, the ports and the handlers for create, amend, cancel, detail and list. The other six domains still contain `doc.go` and nothing else.

That distinction is worth keeping in mind rather than rounding away: two domains now hold code the others will want to call, and the rule that stops them calling it directly — one domain never imports another — has had its first real opportunity to be broken and was not. Wave 3 is also the first evidence for the pre-seeded infrastructure list: `internal/ratelimit` (SHIP-47) and `internal/pagination` (SHIP-66) were both written **with no shared-file edit at all**, which is exactly what registering them in `internal/boundaries` ahead of the code was for. Only `money` remains unwritten.

**All four deployables now exist and run, and the app is wired to real product endpoints.** The Flutter client builds and runs on both simulators, the two Next.js surfaces build and serve, and the Go service serves both `/health` and `/v1`. Wave 3's SHIP-51…54 took the client past the foundation: registration, email verification and phone OTP are driven from the device against the live API, through a deep link the router holds across a cold-start keychain restore. The two Next.js surfaces are still foundation only.

## 2. Branch state

| Branch | At | Holds |
|---|---|---|
| `main` | PR #19 | **Wave 1, released 11 August 2026.** Now well behind `develop` |
| `develop` | wave 3 merged | Everything below. **Cut new branches from here** |

**`develop` is 98 commits ahead of `main` and holds three waves.** Wave 1 was released as PR #19; everything since — SHIP-17a, the wave-2 pre-step and its three tracks, the wave-3 pre-step (SHIP-15e) and its three lanes — is on `develop` only. The next `develop → main` pull request is the second release, and it is now considerably larger than the first.

That figure is `git rev-list --count main..develop`, and it is worth naming the command because the other two readings differ sharply: `--first-parent` gives 20 (one per merged branch, which is the useful review unit) and `--no-merges` gives 72. A previous estimate of "81" reproduced under none of them.

**Run the revert check before cutting it.** `main`'s history contains a revert, which is the shape where a merge silently resurrects deletions, and the two commands for establishing that it is safe are below. This is not hypothetical here: PR #19 had exactly that shape.

### The wave-3 branches, in merge order

All four were merged locally with `--no-ff`, none through a pull request, so the record is the merge commit rather than a number.

| Merge commit | Branch | Brought |
|---|---|---|
| `fee58cd` | `ship-15e-wave-3-prep` | SHIP-15e — the verify split and the done-list move, before the lanes opened |
| `d79fa22` | `ship-51-54-registration-journey` | SHIP-51, 52, 53, 54 |
| `3f72216` | `ship-60-66-job-drafts` | SHIP-60, 61, 62, 64, 65, 66 |
| `db3027d` | `ship-39-47-sessions-and-login` | SHIP-39, 40, 41, 42, 43, 46, 47 |

**One conflict across the whole wave**, and it was this file: the `make verify` check-count line. `develop` claimed 123, the identity lane claimed 170, and **the truth was 208** — `develop`'s own figure had already been stale by 20 before the merge, because a wave-2 track added checks without updating the line. Taking either side would have shipped a wrong number; it was resolved by re-running `make verify` on the merged tree and writing what it printed. That is the general rule for this line and it is cheap: the number is measured, never reconciled.

### The wave-2 branches, in merge order

| PR | Branch | Brought |
|---|---|---|
| #21 | `ship-15c-wave-2-prep` | SHIP-15c — the shared surfaces, before the tracks opened |
| #22 | `ship-44-authentication-middleware` | SHIP-44 — the gate §8 had been carrying |
| #24 | `ship-56-67a-jobs-foundation` | SHIP-56, 57, 57a, 67a |
| — | `ship-30-36-registration-and-verification` | SHIP-30, 31, 33, 34, 36, 45 |
| — | `ship-48-49-client-session` | SHIP-48, 49, 23a |

The last two were merged locally rather than through a pull request, which is why they have no number. **Both conflicted in this file and nowhere else**, and both resolutions were unions — see §7a.

An earlier version of this section chased the exact pull-request number and commit count, and was wrong within a day both times — a commit cannot record the number of the pull request that merges it. **This table is always slightly behind reality, and the fix is to correct it in the next update rather than to try to make it self-aware.** It says "wave 3 merged" rather than a number, for that reason.

The commit count above is the same kind of figure and gets the same treatment: **recount it at each reconciliation, never carry it forward.** It has now been carried forward wrongly once — a handover brief recorded 81 when the true count was 98 — which is the cost of copying a number that a single merge invalidates.

`main` still shows commits `develop` does not have. Those are the detour, not divergent work: wave 0 reached `main` by being merged (PR #6), reverted (PR #7), and reapplied (PR #9), and PR #19's own merge commit sits on `main` alone. The content is identical; only the shape of the history differs.

### The wave-1 branches, in merge order

| PR | Branch | Brought |
|---|---|---|
| #11 | `ship-15b-wave-1-prep` | SHIP-15b, and SHIP-37's dependency amendment |
| #12 | `ship-15a-close-mobile-decisions` | `Docs/07` §9 closed, `Docs/10` §8.3 reconciled |
| #13 | `ship-22-35-web-and-adapters` | SHIP-22, 23, 32, 35, 59a |
| #14 | `ship-29-38-credentials-and-sessions` | SHIP-29, 37, 38, and the SHIP-149 verify fix |

`ship-16-21-flutter-foundation` exists and is parked at PR #11's merge, holding nothing. It is the branch the Flutter track resumed on — see §7b.

**A warning worth keeping.** Reverting a merge does not undo it: the commits stay ancestors forever, so re-merging the same branch brings nothing across and reports success. If a merge to `main` is ever reverted again, the fix is to revert *the revert*, not to merge again.

**PR #19 had exactly the shape that warning describes, and was checked rather than trusted.** The revert `d733c95` sits on `main` and is *not* in `develop`'s ancestry — the setup where a merge silently resurrects deletions. It was safe only because the reapply `c1cb64b` had already restored the content, leaving the revert nothing to take away. The check that established this before merging is worth reusing on any release whose history has a revert in it:

```
git merge-tree --write-tree main develop   # the tree the merge would produce
git rev-parse develop^{tree}               # the tree develop actually has
```

Identical hashes mean the merge result is exactly `develop`'s content. Different hashes mean something is being dropped or added, and the release needs looking at before it is cut, not after.

## 3. Done

Verified by `make verify` — **208 checks**, and `make check` green. Since SHIP-15e the checks
live one file per milestone or domain in `scripts/verify/`, sourced by the runner; a ticket adds
its section by adding a file.

`make verify` covers the foundation tickets it was written for. Work that reaches no HTTP
endpoint is demonstrated by its own tests instead and says so in the row: the wave-1
adapters have none to demonstrate — geocoding is not consumed until SHIP-60 — and the two web
surfaces are demonstrated by `make web-build` and `make web-dev`. The email adapter is consumed
from SHIP-31 and the SMS adapter from SHIP-34, and both are exercised through the console
implementation, which is what `make verify` reads the token and the code out of.

The Flutter client is demonstrated by `make flutter-check` — the analyzer, the tests, and the
environment test run once per build flavour — and SHIP-16 and SHIP-19 by installing the built
`.app` and `.apk` on an iPhone 17 simulator and a Pixel_10a emulator and reading the API
version off both screens. `make verify` does not cover it: that script exercises HTTP
endpoints, and none of these tickets adds one.

**SHIP-48 adds a second Flutter command, and it is not in `make check`.**
`make flutter-integration d=<device>` runs `apps/mobile/integration_test/` on a booted
simulator, against the real Keychain and the real Keystore. It is out of `flutter-check` and
out of `CHECKS` deliberately — it needs a device, and the Flutter CI job is a Linux runner
until SHIP-24…27 — so it is a check a person invokes when the storage or the session changes.
The file's own header says which invocation demonstrates which claim.

### M0 — Foundation (30 of 34)

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
| **SHIP-15e** | Wave-3 shared surfaces: `scripts/verify/` split out, `httpx.H` and `httpx.DecodeJSON` promoted, the done list moved and union-merged — *see below* |
| **SHIP-16** | Flutter scaffold — iOS and Android only, floors at iOS 14.0 and Android API 24 |
| **SHIP-17** | Feature folders per `Docs/07` §2, Riverpod and `go_router`, and a boundary test |
| **SHIP-17a** | `contracts/openapi.yaml` from per-domain fragments, and three tests holding it to the service |
| **SHIP-18** | `dio` client — three environments by `--dart-define`, and the Android emulator's host |
| **SHIP-19** | Health round trip — the API version on screen, on both simulators |
| **SHIP-20** | Go CI — build, boundaries, spelling, tests, migration round trip |
| **SHIP-21** | Flutter CI — analyzer, tests, per-flavour environment tests, codegen diff |
| **SHIP-22** | Admin panel scaffold — pnpm workspace, Next.js App Router, placeholder shell |
| **SHIP-23** | Driver portal scaffold — placeholder job page, no token route, no account |
| **SHIP-23a** | Web CI — one path-filtered workflow per surface, and a Go change starts neither — *see below* |

### Elsewhere

| Ticket | Milestone | What |
|---|---|---|
| **SHIP-28** | M1 | `users` — citext email, phone, role, status, verification timestamps |
| **SHIP-29** | M1 | argon2id password hashing, parameters stored in the PHC string |
| **SHIP-30** | M1 | `POST /v1/auth/register` — an unverified account, duplicates refused by the index — *see below* |
| **SHIP-31** | M1 | Email verification tokens — single-use, one live per account, stored as SHA-256 |
| **SHIP-33** | M1 | `POST /v1/auth/verify-email` and `/v1/auth/resend-verify` — single-use, and a double click is not an error |
| **SHIP-34** | M1 | `POST /v1/auth/request-otp` — six digits, argon2id, two rate limits, and a deliberately uninformative answer |
| **SHIP-36** | M1 | `POST /v1/auth/verify-phone` — one failure code, and the wrong-guess path commits |
| **SHIP-45** | M1 | Role fixed at registration, immutable afterwards by a `BEFORE UPDATE` trigger on `users` |
| **SHIP-32** | M1 | Email adapter — console in development, generic HTTP provider in staging |
| **SHIP-35** | M1 | SMS adapter — same shape, and the OTP is legible in the dev log on purpose |
| **SHIP-37** | M1 | Access token issue — HS256, keyset by `kid`, fifteen minutes, no permissions in the token |
| **SHIP-38** | M1 | `device_sessions` — hashed refresh state, device label, last seen |
| **SHIP-39** | M1 | Refresh token issue and rotation — opaque, hashed, and the expiry question closed — *see below* |
| **SHIP-40** | M1 | Refresh token reuse detection — a spent token ends the whole device session — *see below* |
| **SHIP-41** | M1 | `POST /v1/auth/login` — the endpoint a session starts at, and one answer for every credential failure — *see below* |
| **SHIP-42** | M1 | `POST /v1/auth/refresh` — the first endpoint that issues a session credential — *see below* |
| **SHIP-43** | M1 | `POST /v1/auth/logout` — the first route in the service that requires a credential — *see below* |
| **SHIP-46** | M1 | `GET /v1/auth/sessions` and `DELETE /v1/auth/sessions/{id}` — the device list, and the owner check on the query — *see below* |
| **SHIP-44** | M1 | Authentication middleware — the auth class is now enforced, and idempotency keys are scoped by caller — *see below* |
| **SHIP-48** | M1 | Flutter secure storage — the refresh token in the Keychain and the Keystore, and nowhere a swap would be possible — *see below* |
| **SHIP-49** | M1 | Flutter session and routing guard — three states, and a cold start that never guesses — *see below* |
| **SHIP-51** | M1 | Flutter registration screen — the first client screen to call a product endpoint, and one idempotency key per action — *see below* |
| **SHIP-52** | M1 | Flutter role selection — chosen first because the platform fixes it, and the session's role selects the shell — *see below* |
| **SHIP-53** | M1 | Flutter email verification — typed or deep-linked, and the app's first deep link scheme — *see below* |
| **SHIP-54** | M1 | Flutter phone verification — a code on opening, and a resend the platform's interval throttles — *see below* |
| **SHIP-56** | M2 | `jobs` — the twelve statuses of `Docs/02` §1 as a `CHECK`, held to the Go constants by test |
| **SHIP-57** | M2 | The transition guard — and the database refuses a status change that did not come through it — *see below* |
| **SHIP-57a** | M2 | `job_status_history` — actor, reason and both clocks, append-only |
| **SHIP-59a** | M2 | Geocoding adapter — deterministic stub, and not-found is an outcome, not an error |
| **SHIP-60** | M2 | The address value object — validated, normalised, and resolved where the platform can; a failed lookup never fails the job — *see below* |
| **SHIP-61** | M2 | `POST /v1/jobs` — the first authenticated state-changing endpoint in the service — *see below* |
| **SHIP-62** | M2 | `PATCH /v1/jobs/{id}` — a partial edit of a draft, and a stranger's edit is indistinguishable from no job at all — *see below* |
| **SHIP-67a** | M2 | `cmd/worker` — a ticker and a `FOR UPDATE SKIP LOCKED` claim loop; two workers share the backlog rather than duplicating it — *see below* |
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

Shared packages now available to every domain: `db` (Runner, InTx), `authctx`, `clock`, `validate`, `events` (outbox writer), and — since SHIP-47 — `ratelimit`. Still registered and not yet written: `pagination`, `money` — write them when first needed, no shared edit required. **`ratelimit` is the mechanism's first vindication**: SHIP-47 wrote the package, its first client used it, and `internal/boundaries` was never opened. `CLAUDE.md` still lists `ratelimit` among the unwritten three and needs the one-word correction; it is a shared surface and was not edited from this branch.

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

### What SHIP-15e built

The wave-3 pre-step, and the same argument as SHIP-15b and SHIP-15c one wave later: a letter
suffix marks work the plan assumed and no ticket owned. **Wave 2 had one track adding HTTP
endpoints. Wave 3 has two** — identity and jobs — and that single fact is what each of the four
parts is about.

| Surface | Before | After |
|---|---|---|
| `scripts/verify-foundation.sh` | 1148 lines, 29 sections, one file, no include mechanism | A harness plus `scripts/verify/NN-<name>.sh`, sourced in lexical order. **A track adds a file** |
| `httpx.H`, `httpx.DecodeJSON` | Specified in `Docs/10` §4.3, absent, and written unexported inside `internal/identity` | In `internal/httpx`, with tests, and `identity` uses them like everybody else |
| The 1 MiB body limit | Two literals in two packages, each with a comment asking the other not to move | **One constant.** Divergence is no longer expressible |
| The done list | A fenced block in this file that every track appends to | `Docs/11-done.txt`, one ticket per line, `merge=union` |
| `make web-check` | lint → typecheck → build, which fails on a tree that has never been built | lint → build → typecheck, and the workaround deleted from both workflows |

**The verify script is split the way `mk/*.mk` is split, and the numbering is the way migrations
are numbered.** Ranges are reserved per milestone or domain — `40–49` is identity's, `50–59` is
jobs's — so two branches cannot draw the same one and neither has to read the other's file. The
runner keeps everything shared: `ticket`, `ok`, `fail`, `json`, `post_json`, the token minting,
the service lifecycle, `$pass` and the summary. Sections are *sourced* rather than executed, so
they run in the runner's shell and nothing about the existing checks had to change — the whole
1148 lines moved across by line range, and the count is still exactly 105.

**Three helpers moved from a section into the harness, because wave 3 needs them twice.**
`post_json` was defined inside SHIP-30's section and `mint_token`, `b64url` and `$auth_header`
inside SHIP-44's. Track A's first `RequireUser` route and Track B's job endpoints both want a
token and both want to post one. Two copies of a signing routine would be two answers to what a
valid token looks like.

**The summary line is no longer written down.** It was a literal listing every ticket, on the
last line of the file — a line both endpoint tracks would have edited, and one that had said
`SHIP-1..SHIP-15` long after it stopped being true. `ticket "SHIP-1  …"` already names its
ticket in the first word, so the runner collects them and prints the list itself.

**A file in `scripts/verify/` that is not named `NN-<name>.sh` is refused rather than skipped.**
A section that silently does not run is the same defect as a route dropped in a merge: no error,
no failure, and an acceptance criterion that has quietly stopped being demonstrated.

**`httpx.H` and `httpx.DecodeJSON` were promoted, not amended away.** The choice was the one
SHIP-15c faced with `RegisterCode` — build the mechanism, or correct `Docs/10` to match reality
— and it went the same way for the same reason: the second domain that needs it is in this
wave. Neither had a test while it lived in `identity`; both do now, including a mutation-checked
one for the defect the signature exists to remove (a handler that writes *and* returns nil must
produce one body, not two).

**The body limit is one constant rather than two that agree.** `Docs/10` §4.3 required
`decodeJSON`'s 1 MiB to equal `httpx`'s `maxIdempotentRequestBody`, and enforced it with a
comment in each place asking the other not to move. Both now read `maxRequestBody`. The
idempotency middleware fingerprints the body *before* the handler sees it, so a handler allowed
the larger body would be replayed against bytes it never read — a test could only catch that
afterwards, and one constant cannot drift at all.

**The done list moved out of §10, and that is the whole of the `merge=union` decision** §9 had
been holding. §3 is prose and a union there interleaves two narratives — worse than a conflict,
because it is harder to see. A git attribute applies to a whole file, so the two could only be
treated differently by separating them. `Docs/11-done.txt` is one ticket per line, which is also
load-bearing: a union resolves line by line, and the old block put several tickets on one line,
so two tracks appending to the same line would have conflicted regardless. `make status` reads
the new file, and **fails if a ` ```done ` block ever reappears here** — a second list is a list
nothing reads.

The union was run rather than assumed, in a scratch repository and with its counterfactual: two
branches each appending two tickets merge clean, all four present, no conflict markers — and the
identical merge without the attribute conflicts. An attribute nobody has watched work is an
attribute nobody knows works, which is the same argument SHIP-11 makes about the import lint.

**The web-check reorder deleted its own workaround.** SHIP-23a found the defect and could not
fix it: `mk/web.mk` belongs to the web surfaces, so both workflows carried a `make web-build`
step and a comment pointing at §9 instead. Demonstrated by deleting `.next` from both
applications — the state CI is always in and a developer never is — and running `make web-check`
to green.

**What demonstrates the *Done when*.** "Two tracks add HTTP endpoints without editing a file the
other does" is a claim about a mechanism, so it was exercised rather than asserted: a throwaway
`scripts/verify/50-jobs.sh` was dropped in, `make verify` picked it up with no edit to the runner
and reported 106 checks across 9 sections, and it was removed again. Same shape as SHIP-11's
throwaway module — a mechanism nobody has watched work is a mechanism nobody knows works.

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

### What the identity endpoints built, and what they found

**The first product endpoint in the service is `POST /v1/auth/register`.** Everything before it
was operational, or `/v1/app/minimum-version`, which is the app asking about itself. This is the
first route with a domain behind it, and so the first exercise of every mechanism SHIP-15a and
SHIP-15c put in place: a route file no shared file knew about, a contract fragment referenced by
one added line, error codes registered from the domain, and a golden file that changed by exactly
one row.

**Duplicate rejection is the database's, not the application's.** `Register` inserts and reads
the refusal out of `uq_users_email` or `uq_users_phone` rather than selecting first. A
check-then-insert is a race lost in practice rather than in theory — two taps on a slow
connection — and the index refuses the second registration whatever the application believed.
Same argument as SHIP-91's partial unique index.

**Phone numbers are normalised to E.164 at the boundary, and that is what makes the index
mean anything.** `0412 345 678` and `+61412345678` are two different strings, so without
normalisation `uq_users_phone` never sees the collision and one handset ends up with two
accounts — which would make an OTP ambiguous about which account it verifies. The normaliser
strips only the punctuation people write numbers with and *keeps* anything else, so
`0412 34a 678` is refused rather than silently repaired into somebody else's number. That was a
real defect in the first version of the function, caught by its own test.

**Registration is the one endpoint that discloses whether an address is known.** A duplicate is
answered with `identity_email_taken` or `identity_phone_taken`. The alternative — accept, and
send "you already have an account" by email — is what a bank does, and here it would leave a
person who mistyped their address staring at a success screen for an account that does not exist.
The resend and OTP endpoints do not make that disclosure, because there it buys the caller
nothing.

**Two hashes, chosen opposite ways, and the reason is the search space.** The email
verification token is 32 bytes from `crypto/rand` and is stored as SHA-256; the phone OTP is six
digits and is stored as argon2id at the configured profile. A work factor exists to make a
*small* space expensive, so it is worth 64 MiB on 10^6 possibilities and worth nothing on 2^256.
Getting this the other way round is the mistake worth naming: SHA-256 over six digits is a table
a laptop builds in under a second, and argon2id on the email token would turn the confirm
endpoint into a denial-of-service lever anybody can pull without an account.

**`request-otp` answers identically for every outcome, and that is the design rather than
laziness.** Unknown number, known number, already verified, inside the cooldown, past the hourly
cap: `202` with the same body and a **fixed** `retry_after_seconds`. Reporting the true remaining
cooldown would say "this number was sent a code recently", which says "this number has an
account" — and that makes "does this person use Shipper" answerable one number at a time by
anybody. The rate limit is therefore demonstrated by counting rows rather than by reading a
status. Registration is the deliberate exception, for the reason on `CodeEmailTaken`.

**The OTP rate limit is read from PostgreSQL, and it is not SHIP-47.** SHIP-47's Redis token
bucket is about request volume across the authentication surface. This one is about how many
*messages* an account causes — a bill, and somebody's handset — so it has to survive a Redis
flush, and the table already records every code that was sent.

**The wrong-guess path in `verify-phone` commits, and that is the one place in the domain where
a failure is deliberately not an error inside its transaction.** `db.InTx` rolls back on any
error, so incrementing the attempt counter and then returning `ErrOTPInvalid` from inside the
closure would roll the increment back with it — the column would stay at zero and the five-guess
limit would limit nothing. The closure therefore returns `nil` on a wrong guess, having recorded
it, and the error is produced from the outcome after the commit. It is worth the awkwardness, and
it is worth the comment: the obvious tidy-up reintroduces the defect silently.

**`created_at` is written from the injected clock rather than defaulted**, on both new tables.
The two timestamps in a row have to come from one clock: `expires_at` is computed in Go and
`created_at` was defaulting to the database's `now()`, so `expires_at > created_at` failed under
a `clock.Fixed` in tests and would fail under ordinary skew in production. The rate limits count
over `created_at`, which is the second reason it has to agree with the clock the service reasons
about.

**`httpx.H` and `httpx.DecodeJSON` do not exist**, and `Docs/10` §4.3 has specified both since
SHIP-15a. That is the shape `httpx.RegisterCode` was in until SHIP-15c: documented, listed as
built, and absent, because no domain had needed it yet. `internal/httpx` is a shared surface and
not a domain branch's to edit, so the two are written unexported in `internal/identity/http.go`
and flagged in §9 — the second domain to want them should promote them rather than copy them.

### What SHIP-39 built, and the expiry decision it settles

**§9 has carried "`device_sessions` has no expiry column" since SHIP-38, and it is now decided:
the expiry is an explicit column, `device_sessions.refresh_token_expires_at`, added by
`000103`.** The reasoning is in the migration's own header at length, because a decision recorded
only in a status document is one nobody reads at the point of changing it. In brief, the three
candidates were:

| Where expiry could live | Verdict |
|---|---|
| A Redis key TTL | **Refused by `Docs/10` §5.** Redis may hold a denylist as a fast path, but a security control a cache flush can undo is not one — which is why `device_sessions` is in PostgreSQL at all |
| Derived from `last_seen_at + TTL`, no new column | **Refused.** `last_seen_at` is a *display* column (`000100`), and SHIP-46 will write it from a device-list read. A lifetime derived from it means every future write to a display field silently extends a credential, with nothing to fail on |
| An explicit column | **Chosen.** An expiry that is written can be read, indexed and audited, and "has this session lapsed" becomes a query rather than arithmetic somebody has to remember |

**It is the *token's* expiry, not the *session's*, and the name says so.** Rotation rewrites it on
every use, so the window slides: thirty days of inactivity ends a session and a device in daily
use never reaches it. `Docs/07` §3 asks for a "longer-lived" token rotated on every use, and a
sliding window is what that means — the other reading, an absolute cap from sign-in, would sign a
driver out mid-delivery on a schedule. **An absolute cap is deliberately not added**: it is a
second control with a product consequence rather than a mechanism, and a column nothing writes to
would be guessing at a design nobody has argued. It is another column and another migration if it
is ever wanted.

**`NOT NULL` with no default, and that is the control rather than a style choice.** A session row
with no expiry is a credential that never lapses — a stolen phone signed in forever. There is no
`DEFAULT` for the reason `insertEmailToken` gives about `created_at`: the value is computed in Go
from the injected clock, and a database-side default would come from a second clock that can
disagree with it. `make verify` demonstrates the refusal rather than asserting it.

**The TTL is a constant in `internal/identity`, not configuration**, and that is a scope decision
worth naming. `internal/config` is a shared surface (`Docs/10` §9.2) and three tracks were open;
the constant sits beside the password limits in `service.go`, which make the same argument.
`000103`'s comment says where it would go if it moves.

**Rotation is one row, one hash, and no separate revocation step.** The session holds exactly one
refresh token hash; rotation overwrites it, so the token presented matches nothing the moment the
transaction commits. There is no "mark the old one used" that could be forgotten and no window in
which both work.

**`FOR UPDATE` on the read is what makes rotation single-winner**, and it is checked
deterministically rather than by racing two goroutines and hoping they overlap. A phone on a poor
connection fires the same refresh twice; without the lock both transactions read the same row and
the device ends up holding a token the row has already replaced. `TestARefreshHoldsTheSessionRowUntilItCommits`
holds the row in one transaction, proves a second reader blocks, and proves it sees the *rotated*
row after the commit. Mutation-checked: removing `FOR UPDATE` makes the second read return the
stale row immediately and the test fails on its first assertion. The two-goroutine test alongside
it deliberately asserts only the invariant, because a test whose coverage depends on scheduling
reports a mistake intermittently.

**`Service` now takes an `*AccessTokenIssuer`**, which is a signature change to `NewService` and
the first real consumer of SHIP-37. A service that could create a session without one would hand
out half a credential: a refresh token the caller cannot exchange for anything. The keyset and the
issuer are built inside `identityHandler`, from configuration, with no field added to `Deps` —
which is SHIP-15c's acceptance criterion holding for a second wave.

**One thing the block scheme costs, found here — ~~and worth knowing before the next domain hits
it~~. Closed at SHIP-15g, which now refuses rather than skipping; see §3.**
`cmd/migrate` uses stock golang-migrate, which applies only migrations *above* the recorded
version. A developer database that has jobs migrations applied sits at `000402`, so a new identity
migration at `000103` is silently skipped by `make migrate-up` — it reports "no change" and the
column never appears. Tests and CI are unaffected: `make test-db-template` drops and rebuilds from
scratch, and CI runs `up → down all → up` against an empty database. The local fix is
`make migrate-down n=all && make migrate-up`, which is still the fix — the change is that the tool
now says so instead of reporting success. It would otherwise have bitten every migration in
`identity`, `profiles` and `fleet`, whose blocks sit below `jobs`, starting with SHIP-78 in the
very next wave.

### What SHIP-40 built, and the design it rejected

**A spent refresh token stays recognisable for as long as its session lives, in a ledger:
`consumed_refresh_tokens` (`000104`).** Rotation moves the hash across in the same transaction
that writes the new one, so the invariant is exact — **a refresh token hash is live in
`device_sessions.refresh_token_hash` or spent in the ledger, never both**. That is what makes two
lookups an answer rather than an ambiguity, and `make check` asserts the overlap is empty rather
than trusting the code that maintains it.

**The design that was rejected is the cheap one, and it is worth naming because it looks
sufficient.** Keeping the *previous* hash beside the current one satisfies the acceptance
criterion for exactly one generation, and silently stops satisfying it after two: a token stolen
and then left while the legitimate device refreshes a few times matches neither column, is
answered "unknown", and the session survives. The criterion is "presenting a consumed token
invalidates the entire device session", not "presenting the most recently consumed one".
`TestReuseIsDetectedManyRotationsLater` rotates five times and then presents the first token,
which is the test the cheap design fails.

**A token that matches nothing revokes nothing, and that is the other half.** Only a token this
platform issued and has already rotated away is evidence of anything; revoking on an unrecognised
value would hand anybody a way to end sessions by guessing — a denial of service dressed as a
security control.

**The revocation is written and the refusal is returned after the commit, which is the same trap
`VerifyPhone` documents one credential along.** `db.InTx` rolls back on any error, so returning
`ErrRefreshTokenReused` from inside the closure would undo the revocation with the very error
that reports it — the session would stay live and the log would say it had been ended.
Mutation-checked: making that one change turns
`TestPresentingAConsumedTokenRevokesTheWholeSession` red on both of its subtests, one saying the
session is still `live` and one saying the stolen device can still refresh.

**The caller cannot tell reuse from any other refusal, and the sentinel exists for the service
rather than the client.** `ErrRefreshTokenReused` and `ErrRefreshTokenInvalid` map to one code
(SHIP-42). Two sentinels because "the session was revoked" and "the token was refused" are
different claims that a test and a log line need to distinguish; one code because the remedy is
identical and telling somebody holding a stolen token that the platform noticed is free help.

**The reuse event is logged and not audited, and that is a gap with a name.** SHIP-149 built
`audit_log` and its append-only triggers but not the Go write helper (§4), so there is nothing to
call. This is the event that most deserves one — whoever finishes SHIP-149 should start here. The
token is not logged in any form: a log aggregator holding refresh tokens is the exposure hashing
the column exists to prevent, one system along.

**Revoking a session deliberately does not move its live hash into the ledger.** Refresh checks
`revoked_at` before anything else, so the token buys nothing either way — and moving it would put
one hash in both places and cost the invariant its whole value.

**`device_sessions` can now end, and the three reasons each have a ticket.**
`refresh_token_reused` (SHIP-40), `signed_out` (SHIP-43) and `revoked_by_owner` (SHIP-46) are a
`CHECK` paired with Go constants and a test that reads the constraint back, per `Docs/10` §3.4.
Enumerating SHIP-43's and SHIP-46's values now saves the next run a migration; a reason with no
ticket behind it does not belong there.

**Spent tokens are not pruned, and a session in daily use writes roughly one every fifteen
minutes of activity.** Deleting the spent tokens of a session that has ended is safe — a revoked
session refuses every token it ever issued — and belongs to `cmd/worker` (SHIP-67a) as its own
ticket rather than being smuggled into this one. `000104` says so where somebody would look.

### What SHIP-42 built

**`POST /v1/auth/refresh` is the ninth route and the first that hands back a session
credential.** It is where SHIP-39 and SHIP-40 stop being schema and become behaviour: `make
verify` drives the real endpoint over HTTP, rotates, reads the row, presents the spent token,
and shows the token the device legitimately held stop working with it.

**Public, and it has to be.** The caller is the client whose access token has just expired.
SHIP-44 anticipated exactly this when it split `ResolveSubject` from `RequireSubject` — a
middleware that refused a bad credential on sight would lock that client out of the endpoint that
replaces it.

**It answers `400`, not `401`, and that is a decision rather than an oversight.** SHIP-50's
interceptor refreshes on a `401` and replays the request; a `401` from the refresh endpoint itself
is the one answer that can send a naive implementation round the loop a second time. The
credential is also body-borne, so a `WWW-Authenticate` challenge would describe a scheme this
endpoint does not use. It matches how `verify-email` and `verify-phone` already answer for a
credential that arrives in the body.

**One code for every failure — `identity_refresh_token_invalid`.** Never issued, already rotated
away, expired, session signed out or revoked, account suspended: the remedy is identical and
distinguishing them would tell somebody holding a stolen token which part of it the platform
recognised. It is the seventh domain code and the twenty-third overall.

**Lifetimes are reported as seconds, not instants.** A client comparing a timestamp against its
own clock refreshes at the wrong moment on any handset whose clock is wrong, which on a phone that
has been out of signal is not unusual. `expires_in` and `refresh_token_expires_in` are read from
the service's own TTLs rather than subtracted from a wall clock at the transport edge, so a fixed
clock in a test and ordinary skew in production both give the same answer.

**There is no `token_type` and no account object in the response.** One is a field whose value
never varies, which the contract would have to describe forever; the other would be verification
state cached at refresh time, and `Docs/10` §5 keeps that out of the token for the reason SHIP-63
depends on.

**`contracts/paths/identity.yaml` gains a `TokenPair` schema, and SHIP-41 should reuse it rather
than describe the same body again.** Sign-in returns the same four fields, and two descriptions of
one shape is how a client generator ends up with two types.

**The `paths:` block in `contracts/openapi.yaml` gained one line**, sorted — `/v1/auth/refresh`
sorts before `/v1/auth/register`. That is the one shared file this track touched, and the wave's
jobs track is adding lines to the same block; `Docs/10` §9.2's recipe applies (take both sides,
re-sort, then `make test`).

### What SHIP-41 built, and the two disclosures it closes

**`POST /v1/auth/login` is the tenth route and the endpoint every session starts at.** SHIP-42
issued the first *credential*; this issues the first credential somebody obtained by proving who
they are. Everything else in the domain now either exchanges that session's token or ends it, and
no section of `make verify` has to plant a `device_sessions` row by hand any more.

**One code for a wrong password and for an address with no account —
`identity_credentials_invalid`.** Two answers would make an unauthenticated endpoint an
account-existence oracle for any address anybody cares to try, which is a good deal worse than
registration's deliberate disclosure: that one at least costs the caller an address they control.

**The second half of that disclosure is the response time, and it is the half a plausible
implementation leaves open.** argon2id at m=64 MiB costs tens of milliseconds and a lookup that
misses costs none of them, so a caller timing two requests reads off exactly what the status code
refuses to say. `PasswordHasher.SpendEquivalentWork` derives a key against a fixed salt and
discards it on the no-such-account path.
`TestSignInSpendsTheSameWorkWhetherOrNotTheAccountExists` compares the two against each other
rather than against a figure, so it holds at any profile; mutation-checked by emptying the method,
which drops the unknown-account path to microseconds.

**A suspended account is told so, and that is the one place account standing is disclosed.** It is
safe here and nowhere else: it is said *after* the password verified, so the caller has just proved
they own the account they are being told about. Refresh deliberately says nothing, because the
caller there holds only a token. `403`, because the caller is known and is not permitted.

**The status rule the whole domain now follows, stated once:** a credential presented in the
request *body* is refused with `400`; a credential presented in the bearer header is refused
with `401`. SHIP-42 argued it for refresh from two directions — `WWW-Authenticate` would
describe a scheme the endpoint does not accept, and SHIP-50's interceptor refreshes on a `401` —
and both apply unchanged to sign-in. SHIP-46 below is the first route on the other side of the
rule.

**Sign-in upgrades a password hashed at a weaker profile, which is what makes `Docs/10` §5's claim
true rather than merely available.** The costs travelling in the PHC string mean the argon2id
profile *can* be raised without a migration; nothing took the opportunity until now, and sign-in is
the only moment the plaintext exists to take it with. It is inside the same transaction as the
session insert — there is no "carry on regardless" available from inside a transaction, because a
failed statement has already aborted it.

**Each sign-in creates a device, and the contract says so where a client will read it.** There is
no device identifier to match on, so a client that signs in rather than refreshing leaves the
previous session live for thirty days and shows its owner a device list they cannot make sense of.
`device_label` is required for the same reason: four rows reading "Unknown device" cannot be acted
on at SHIP-46, and the only moment a label can be collected is the one where somebody is looking at
a sign-in screen on the device being named.

**The `paths:` block gained one line**, sorted — `/v1/auth/login` sorts before `/v1/auth/refresh`.
The response `$ref`s the `TokenPair` schema SHIP-42 added rather than describing the same four
fields again.

### What SHIP-43 built, and the first route that requires a credential

**`POST /v1/auth/logout` is the first `RequireUser` route in the service.** SHIP-44 built the
middleware, wired `ResolveSubject` outside `Idempotent` and closed the idempotency-scope hole, and
then nothing used it for a wave. It does now, through the real chain: `make verify` gets a `401`
with a `WWW-Authenticate` challenge for a caller with no credential and for one presenting
nonsense, which is the check wave 2 could not demonstrate at all.

**It is the natural first, and that is not only ordering.** The session being ended is named by the
token being presented — the `sid` claim — so the credential is not a permission check on top of the
request, it *is* the request. There is no body. A body carrying a session identifier would be an
endpoint that can end somebody else's session, which is SHIP-46's job and needs SHIP-46's owner
check.

**"Only" is the half of the criterion a plausible implementation gets wrong.** Revoking every
session the account owns looks entirely correct from the device that asked and is visible only from
the phone in the other pocket. Two devices are signed in through the real endpoint, one signs out,
and the other still refreshes.

**The owner is a predicate on the write.** `revokeOwnDeviceSession` carries `user_id = $2` rather
than the handler checking ownership first. Through the endpoint the identifier comes from a token
this platform signed and cannot name another account's session — which is exactly why the check
belongs in the statement, where no later caller can leave it out.
`TestSignOutCannotEndSomebodyElsesSession` is mutation-checked against removing it.

**Signing out never fails.** A session already revoked, and a token naming one that no longer
exists, both answer `204`: the client has discarded its tokens by the time it reads the response,
and a failure would leave somebody on a screen they cannot get past. The guarded `UPDATE` also
means a repeat does not move the instant the session ended.

**The live refresh token hash deliberately stays in `device_sessions`.** SHIP-40's note asked for
this and it is now enforced by a test and by a `make verify` check: moving it into the ledger would
put one hash in both places and cost that invariant its whole value, for no gain, because rotation
checks `revoked_at` first.

**One thing a client has to know, and the contract says so.** The refresh token dies immediately;
the access token keeps verifying until it expires, because it is signed rather than looked up. That
window is the fifteen minutes in `expires_in`, and it is the price of not doing a database read on
every authenticated request. A client discards both rather than relying on the platform to refuse
the one it still holds.

### What SHIP-46 built, and where the authorisation decision lives

**`GET /v1/auth/sessions` and `DELETE /v1/auth/sessions/{id}`** — the twelfth and thirteenth
routes, and the first pair where a caller names a row that might not be theirs. Sign-out could not:
its session comes from the `sid` claim of a token the platform signed. This one takes an identifier
from the URL, which is the first time in the service that "may this caller do this to this row" is
a real question.

**The answer is a predicate on the query, in both directions.** `liveDeviceSessionsByUser` carries
`user_id = $1` and `deviceSessionOwnedBy` carries `user_id = $2`; neither endpoint checks ownership
in the handler and then acts. Docs/07 §3 puts the decision on the platform, and a decision written
into the statement is one no later caller can leave out. Both are mutation-checked — with the
predicate removed, one account is handed another's device list, and one account signs another's
devices out, from requests that look entirely ordinary.

**A session belonging to somebody else answers exactly what one that never existed answers**:
`404`, `identity_session_not_found`. Distinguishing them would make the endpoint a way of finding
out which identifiers name real sessions, which is the disclosure sign-in refuses to make about
addresses. An identifier that is not a UUID at all gets the same answer, refused before the pool is
touched.

**The list leaves out what cannot act as the account** — revoked sessions, and sessions whose
refresh token has lapsed. The list's question is "which devices can act as me, and let me stop
one", and neither kind can do the first; offering them invites revoking something already dead and
pads a list whose whole value is that an unrecognised row stands out. The rows are still there,
marked rather than deleted, for support and for SHIP-149.

**Reading the list deliberately does not write `last_seen_at`.** SHIP-39's §3 entry anticipated
that it would, as the reason expiry must not be derived from that column, and 000103's header makes
the stronger version of the point. It does not: a read is not activity, and a display column that
every read writes to stops meaning anything. Rotation already records the real thing, at least
every fifteen minutes of use. There is a `make verify` check and a test for it.

**`DELETE`, and nothing is deleted.** The verb describes what happens to the list the client is
looking at, which is the question a REST verb answers; the row is marked `revoked_by_owner` and
kept. `Docs/10` §3.3 requires that, and 000104's `ON DELETE RESTRICT` would refuse the alternative
anyway while spent tokens still name the session. The contract says so where a client will read it.

**Revoking the current session is permitted**, and is the same action as signing out. Refusing it
would produce a device list with exactly one row that cannot be acted on. What differs is the
reason recorded — `signed_out` against `revoked_by_owner` — which is the whole point of SHIP-40
having enumerated both.

**The response uses `Docs/10` §4.5's collection envelope with `next_cursor` always null**, and the
`has_more` beside it is a bound on the response rather than an invitation to page. Every sign-in
creates a session and nothing stops a client signing in a thousand times instead of refreshing, so
the query is capped at a hundred rows. Keyset paging belongs to `internal/pagination`, which was
unwritten when this was written and **which SHIP-66 wrote later in the same wave**. The remaining
gap is smaller and sharper than the original note: the package exists and `GET /v1/auth/sessions`
has simply not adopted it. §9 carries it as a follow-up, not as a missing dependency.

### What SHIP-47 built, and the two positions it takes

**`internal/ratelimit` exists, and nothing shared was edited to make it exist.** It was registered
in `internal/boundaries` ahead of the code, with the description "the Redis token bucket behind
every limited route", and SHIP-47 is the first client. That is the pre-seeded infrastructure list
working exactly as SHIP-15a designed it — the alternative was a domain branch editing the boundary
file mid-wave, which is the merge the seeding exists to prevent.

**A token bucket rather than a fixed window**, because a fixed window lets a caller spend one
window's allowance in its last instant and the next window's in its first — twice the intended
rate, on demand, at a moment of their choosing. A bucket also produces an honest `Retry-After`:
time until the next token, not time until an arbitrary boundary. The refill and the charge are one
Lua script, for the reason the idempotency store's claim gives — a read and a write are two round
trips with a window between them, and two attempts arriving together is the case a rate limit is
*for*.

**Failures are charged and successes are free**, which is what makes this a control on guessing
rather than a cap on signing in. The bucket is checked on the way in and spent on the way out; a
check that spent would throttle somebody for knowing their own password.

**The limit is checked before the credential**, so a correct password is refused too while it
holds. That is the point rather than a rough edge: what the limit protects is the argon2id
derivation and the database round trip, and a throttle a correct password escaped would be a
throttle an attacker escapes by guessing right.

**Position one: it fails closed.** An unreachable Redis refuses the sign-in — `503`, because "this
is temporarily unavailable" is true and "you have done too much" is not. A limiter that failed open
is one an attacker turns off by making Redis unreachable, on the endpoint the limiter exists to
protect. The cost is bounded and worth naming: `httpx.Idempotent` already wraps the whole `/v1`
group and already fails closed on the same Redis, so a client sees no difference during an outage
— failing closed here only means there is one answer rather than two. `TestSignInWithoutARateLimiterCacheIsRefused`
holds it.

**A typed nil is still no client, and that was found rather than anticipated.** `Deps.Redis` is a
`*redis.Client`; assigned to the `redis.UniversalClient` the package takes, a nil one produces an
interface that is **not** nil, so `client == nil` is false and the first call dereferences it. The
symptom was a panic and a `500` — which is fail-*open* in the sense that matters, because a 500
tells nobody a limit was skipped. `ratelimit.New` normalises it once, with a test.

**Position two: `X-Forwarded-For` is not read.** The address comes from `RemoteAddr` and nothing
else. A forwarded header is whatever the client wrote unless a trusted proxy overwrote it, so
honouring one would let any caller pick their own bucket — a limit that looks like a limit and is
not. The other direction has a cost too, and it is a deployment gate rather than a defect: behind a
load balancer, every request would arrive from one address and share one bucket. §9 carries it, and
there is no deployment yet to be wrong about.

**Two limits, and each has an independence half the other cannot show.** Five failures per account
with one back every two minutes; thirty per address with one back every twenty seconds. Both are
mutation-checked in the direction that matters: keying the account bucket on the address makes one
account's failures lock out everybody on the same network, and dropping the address from its key
makes one exhausted address throttle the platform. The figures are constants in `internal/identity`
rather than configuration, for the reason SHIP-39's TTL gives — `internal/config` is a shared
surface and three tracks were open. **SHIP-183 is where every public endpoint's limit gets
considered together**, and that is the ticket that should decide whether any of them belong in
configuration.

**Only sign-in is limited by this.** Registration, the OTP endpoints and the verification endpoints
keep the per-account issue rules SHIP-34 gave them and gain nothing here; SHIP-183 owns the pass
over the rest.

**`make verify` clears the per-address bucket at the point sign-ins begin and again at the end of
this section.** Every request in the script arrives from `127.0.0.1`, so one bucket is shared by
every section below — and by every previous run. Without the clean-up a run that stopped part-way
through SHIP-47 made the *next* run fail in SHIP-41 with a 429 that reads as a broken endpoint.
That was observed, not imagined. It also lets the per-address figure be asserted exactly rather
than derived from counting what the sections above happened to spend.

### What SHIP-48 built, and how it was demonstrated

It reaches no HTTP endpoint — the endpoints that issue a refresh token are SHIP-41 and
SHIP-42 — so `make verify` does not cover it, in the same way it does not cover SHIP-16 or
SHIP-19. It is demonstrated by `make flutter-check`, by `make flutter-integration` on a device,
and by inspecting the stored value on an emulator.

**"Never in preferences" is enforced from four directions, not asserted once.** The *Done when*
line is the kind that a plausible implementation satisfies while being wrong, so:

| Check | Where | Fails when |
|---|---|---|
| The real platform seam records key, value and options | `test/core/auth/token_store_test.dart` | Anything but `flutter_secure_storage` is behind the store |
| `shared_preferences` is absent from `lib/` **and from the lockfile** | `token_store_is_not_preferences_test.dart` | Somebody runs `pub add`, before writing a line |
| One folder imports the package; that folder opens no file | same | A second set of options, or a token cached to disk |
| The real Keychain and Keystore, on a device | `integration_test/session_test.dart` | The token never leaves the process |

The mutation was run rather than imagined: replacing `SecureTokenStore` with an in-memory map
fails eight tests across both files.

**On Android the storage was inspected on the device.** After a token is stored,
`/data/data/au.com.shipper/shared_prefs/FlutterSecureStorage.xml` holds ciphertext under an
`androidx.security.crypto` keyset, and the plaintext token appears nowhere in the application's
data directory. On iOS the equivalent is two invocations of `make flutter-integration` with
`only=`, the second reading in a new process what the first wrote.

**Two things were found while building it.**

`Docs/07` §9 justified the Android floor of API 24 with `EncryptedSharedPreferences`, and the
package no longer uses it — Google deprecated the library behind it. The floor is unchanged,
because the Keystore-wrapped ciphers that replaced it need API 23 just the same, but the
sentence justifying the floor had stopped being true. `Docs/07` §9 is corrected and says that
it was wrong, rather than quietly reading as though it never was.

`flutter_secure_storage` is held at **10.x**, not 11. Version 11 compiles against Android SDK
37 and fails the build against the 36 this project targets. Moving `compileSdk` is an
Android-wide change that also wants a newer Gradle plugin, which belongs with the signing work
rather than inside a three-point storage ticket — §9 carries it.

### What SHIP-49 built

Demonstrated the same way, plus the part only a device shows: the built `.apk` and `.app`
installed on a Pixel emulator and an iPhone 17 simulator, cold-started into each shell.
Force-stopping the Android build and relaunching it lands in the signed-in shell; signing out
removes the keystore entry and the next cold start lands signed out.

**The session has three states and the third is the point.** Reading the keychain is
asynchronous, so a two-state model has to guess for the few frames before the answer arrives —
and both guesses are visible to the user, as a sign-in screen that flashes or a shell with no
data in it. `SessionRestoring` is the honest answer for that window and the router holds a
splash while it is the answer. The guard itself is a pure function of the session and the
location, tested as a table, and **it is navigation rather than authorisation**: reaching a
shell by any means still fails server-side on the first request it makes.

**There is a debug-only button that stores a placeholder token, and it is worth knowing about.**
This wave has no endpoint that issues a refresh token — SHIP-51 and SHIP-55 are the screens
that will — so a cold start into the signed-in shell needs something in the keychain to read.
The signed-out screen carries one, behind `kDebugMode`, which is a compile-time constant: a
profile or release build tree-shakes the widget and its string away entirely, so there is no
flag to misconfigure. The value it writes is not a credential and the platform will refuse it
the moment SHIP-50 refreshes with it, which is the correct outcome — a device believing it has
a session has never been the same thing as having one.

**Both shells are placeholders and neither is role-aware.** `Docs/07` §1 requires the customer
and provider halves to be genuinely separate inside the one app, and the role that selects
between them is returned by the platform, so that split is SHIP-52. The connectivity screen
(SHIP-19) moved to `/health` and is reachable from both shells and during the restore, because
putting it behind the session would have made "can this build reach the API" unanswerable on a
fresh install — which is exactly when it is asked.

### What the registration journey built (SHIP-51…54)

Four screens — role, form, email, mobile — and **the whole of it runs signed out**, which is the
platform's design rather than an oversight the client works around. `POST /v1/auth/register`
returns an account and no token: registering is not signing in, and `POST /v1/auth/login` is
SHIP-41. SHIP-49's guard sent a signed-out user to the sign-in shell from *every* location, so
the first thing this work had to do was widen that to a set of locations rather than one.

**Inline validation is two sources and one presentation.** `shared/validation/validators.dart`
catches what cannot be anything but a mistake — a blank field, an address with no `@`, four
digits where six were asked for — and the platform's `validation_failed` `details` land under
the same inputs, in the dotted paths the form serialised. That is what makes a limit changeable
server-side actually work on a build already installed: the message with the new number comes
from the platform. The one number duplicated is the password minimum, and `validators.dart`
carries the argument for why that duplication is safe in the direction it can fail.

**`ActionKey` is the idempotency rule written down in both directions, and the second direction
is the one that is easy to get wrong.** A fresh key per attempt duplicates a record after a
dropped connection; a shared key across two taps of "send another code" replays the first `202`
so no second message is ever sent — which, from the handset, is indistinguishable from an SMS
running late. A key is therefore retained in exactly one case: the previous attempt failed
without saying whether the platform acted on it, and the body is identical.

**The role is chosen before the form, and the shell has four answers rather than two.** Before,
because SHIP-45 fixes it with a `BEFORE UPDATE` trigger and a permanent decision should not look
like a preference. Four, because a restored cold start holds a refresh token and *no role* —
the role is a claim in the access token (SHIP-37), so it arrives with SHIP-50's first refresh —
and guessing customer there would show every provider the wrong half of the marketplace on every
launch. The fourth is a role this build has never heard of, which says "update the app" rather
than crashing.

**SHIP-53 chose the deep-link scheme, which `internal/identity/verification.go` had explicitly
left to it**: `shipper:///verify-email?token=…`, a custom scheme, because an HTTPS universal and
app link needs a registered domain and both store accounts and X-1, X-2 and X-3 have not started.
No Dart changes when that arrives — a link of either kind resolves to the same route with the same
query parameter.

**It also found a defect SHIP-49 had written down and assigned to the wrong ticket.** SHIP-49
recorded that a deep link arriving during the keychain read is dropped, judged it acceptable while
nothing deep-linked, and named SHIP-143 as the ticket that would fix it. A verification link opens
at a cold start, which *is* the restore window, so for this link it was not a rare case but the
only case: tap the link, land on the sign-in screen. `Redirector` now holds the whole URI —
query included, since the screen without its token is worse than not arriving — reissues it once
the session answers, and still puts it through the guard.

**How it was demonstrated.** `make flutter-check` covers 166 host tests, including the guard as a
table and each screen driven through the real widget tree. The journey itself was run on an
iPhone 17 simulator against this worktree's API on 8092, through
`apps/mobile/integration_test/signup_test.dart` — which is out of `flutter-check` for the reason
`session_test.dart` is, and takes the verification token and OTP as `--dart-define`s because they
exist only in the service log, by design. An account registered through the form arrives in
PostgreSQL as `role=provider` with both `verified_at` columns set by the app's own screens. The
deep link was demonstrated on the Pixel emulator with
`am start -a android.intent.action.VIEW -d "shipper:///verify-email?token=…"`, which cold-starts
the app and lands on "Email confirmed"; on iOS the scheme is registered and the system offers to
open the app, but `simctl openurl` raises a confirmation prompt that needs a real tap, so the iOS
half is confirmed as far as the operating system recognising the scheme.

**One thing this lane could not finish, and it is small.** The verification email still carries a
bare code rather than a link, because the message body is in
`services/core/internal/identity/verification.go` and this lane owns `apps/mobile/**` only. The
screen accepts either, so nothing is broken — but until somebody edits that function, the deep
link is a capability with no message using it.

### What SHIP-23a built

Two workflows, `web-admin.yml` and `web-driver-portal.yml`, path-filtered per surface as
`.github/workflows/README.md` has anticipated since SHIP-1.

**The filter was demonstrated rather than assumed**, by evaluating every workflow's `paths:`
block against representative changed-file sets:

| A change to | Go | Flutter | Admin panel | Driver portal |
|---|---|---|---|---|
| `services/core/**` | runs | — | — | — |
| `apps/mobile/**` | — | runs | — | — |
| `apps/admin/**` | — | — | runs | — |
| `apps/driver-portal/**` | — | — | — | runs |
| `pnpm-lock.yaml` | — | — | runs | runs |
| `Docs/**` | — | — | — | — |

The first row is the *Done when* line: **a Go-only change starts neither web workflow.**

**Both run `make web-check`, which covers both surfaces**, because `mk/web.mk` drives pnpm with
`-r` across the workspace and names no application — deliberately, so a third surface is a line
in `pnpm-workspace.yaml`. So a change to the admin panel also checks the driver portal. That is
redundancy rather than a gap, and the saving worth having is against Go and Flutter changes,
which the filter already collects.

**It also found a defect, which is the sort of thing a first clean-tree run finds.**
`make web-check` is lint → typecheck → build, and on a checkout that has never been built the
typecheck fails: Next.js 16 generates `LayoutProps` and the route types into `.next/types`
during a build, and `tsc --noEmit` has nothing to resolve them against until one has run. It is
invisible locally, because a developer has always built at least once. The workflows run the
build first as a workaround and say so; the fix is one line in `mk/web.mk` and is in §9.

### What the jobs lifecycle foundation built (SHIP-56, SHIP-57, SHIP-57a)

None of these reaches an HTTP endpoint, so none of them has a `make verify` section. They are
demonstrated by their own tests, against a real PostgreSQL — which is not a weaker form of
demonstration here, because half of what they build is a trigger and `Docs/06` §4.1 is right
that "a mock happily accepts a write that the actual constraint would reject".

**`Docs/11` §9's open recommendation is decided, in favour of the database.** `CLAUDE.md` has
always said job status is never a settable field; from `000402` that is a control rather than a
convention. A status change is refused unless a `job_status_history` row **written in the same
transaction** describes it — same job, same two statuses. That one condition carries three
guarantees at once: the change went through the guard, it is recorded with an actor and both
clocks, and it is inside a transaction — because the session variable naming the history row is
transaction-local, so a caller holding a pool rather than a transaction is refused.

**A job is also created as a Draft and nothing else.** `Docs/02` §2 has one entry point, and an
`INSERT` naming another status skipped every check on the way in — a job created at `Awarded` has
no accepted bid behind it, one created at `Delivered` has no proof. That is the half of "never a
settable field" an `UPDATE` trigger alone does not cover.

**What the database deliberately does not know is which moves are legal.** The transition table
of `Docs/02` §2 lives in Go, in one place, with a test that walks the document — including the
moves it is explicit about *refusing*, which is the half that catches a table with something
extra in it. A second copy in SQL would be a copy that drifts, and a move that is legal in one
layer and impossible in the other is a defect reproducible from neither.

**`Transition` takes a `db.Runner`, not a pool.** `bidding` owns the award transaction
(`Docs/10` §3.2) and has to move the job inside it without importing this package, so the guard
joins whatever transaction its caller opened. SHIP-92 needs nothing added for that.

**One thing to know before writing the first jobs endpoint:** this domain raises sentinel errors
and registers no `httpx` codes yet. Nothing serves them, and a published code is a string a
store build on somebody's phone is already branching on. The first endpoint (SHIP-61, SHIP-64)
maps `ErrTransitionNotPermitted`, `ErrAlreadyInStatus` and `ErrJobNotFound` to codes and
regenerates `Docs/10-api-error-codes.md`.

### What SHIP-67a built, and the table it deliberately does not have

`cmd/worker` is the fourth deployable's little brother: a ticker plus a claim loop, exactly as
`Docs/10` §6.2 specifies, and no external scheduler. Four tasks in the backlog need it — job
expiry (SHIP-68), the warning forty-eight hours ahead (SHIP-69), bid expiry (SHIP-89) and the
seventy-two hour auto-complete (SHIP-119) — and none of them creates it.

**There is no `scheduled_tasks` table, and that is the design rather than an omission.** Due work
is rows in domain tables: jobs whose pickup date has passed, bids that have expired, deliveries
seventy-two hours old. A table of scheduled tasks would be a second record of what is due, kept
in step with the first by hand. The instinct to reach for one is strong enough to be worth
naming — and if a later task genuinely needs persistent state of its own, it draws a migration
from its own domain's block.

**Two workers at once is the normal state of a rolling deployment, and it is safe by
construction rather than by arrangement.** There is no leader election and no lease: both
workers tick, both claim, and `SKIP LOCKED` hands the second one the rows the first did not
take. Nothing goes stale if a worker dies holding a claim, because the claim is a row lock and
the lock ends with the connection.

**A pass is one transaction — the claim and the work the claim authorises.** A pass that claimed
rows and committed only half its work is the failure this shape exists to prevent, and it is
what `TestAFailedPassClaimsNothing` checks.

**`ClaimIDs` refuses a query that does not say `FOR UPDATE SKIP LOCKED`.** Both broken forms
work perfectly with one worker: without `SKIP LOCKED` the second worker queues behind the first
instead of sharing, and without `FOR UPDATE` two workers claim the same rows and both act. The
tests were mutation-checked against both — removing `SKIP LOCKED` makes the second claim block
until its deadline, and removing the lock entirely makes two workers claim 361 rows out of 200.

**Demonstrated by its own tests, not by `make verify`**, which drives HTTP endpoints and this
adds none. What has to be shown is two workers at once against one table, which is a thing a
test can arrange and a `curl` cannot.

**One thing left undone deliberately:** the root `Makefile`'s `build` target still builds the
API and the migration tool only. `mk/worker.mk` carries `worker-build` and `worker-run` instead,
because the root `Makefile` is a shared surface and three tracks were open. Folding the third
binary into `build` belongs to whoever owns that file next.

### What SHIP-60 built, and the decision §9 had been holding for it

`internal/jobs/location.go` holds `Address` — four parts, because the suburb, the state and the
postcode are values SHIP-79 and SHIP-81 will compare and a single freeform column would have three
different things parsing them back out — and `Location`, which is an address together with what the
platform resolved it to. The street line stays freeform: unit numbers, lot numbers, PO boxes and
roadside mail boxes are all legitimate first lines of an Australian address.

Normalisation does three things and no more: whitespace is collapsed, the state is resolved to its
abbreviation from any form a person types (`nsw`, `NSW`, `New South Wales`), and the postcode loses
any spaces. **Case is deliberately untouched** — upper-casing the suburb is the Australia Post
convention and it is also how *McDonald Street* stops looking like a place a person wrote.

Two things the schema deliberately does not enforce, both because they are reference data rather
than facts: whether a postcode belongs to its state (the allocations have exceptions — 2600 is ACT
inside the NSW range — and change when Australia Post says so), and any upper bound on the size of a
load. The eight states *are* a `CHECK`, paired with the Go constants by a test, the way `Docs/10`
§3.4 requires of every enumeration.

**`Resolved` is a field rather than a test on the numbers.** (0, 0) is a real point in the Gulf of
Guinea, so "we looked and found it" is a claim two floats cannot make. The columns carry the same
distinction as a NULLable pair bound by `ck_jobs_pickup_coordinate_is_a_pair`, and the response
omits the coordinate object entirely rather than sending zeros.

**A failed lookup does not fail the job**, which is SHIP-59a's rule reaching its first consumer.
Three routes arrive at the same place — no geocoder configured, the provider did not recognise the
address, the lookup did not complete — and all three store the address as typed with no coordinate.
The address itself is deliberately never logged: it is somebody's home, and an application log has a
different retention period and a much wider audience than the job record.

#### The adapter-value-type decision: no neutral geo package, and here is the trigger

§9 has carried this since wave 1 and named SHIP-60 as the moment to settle it. **Settled: the
geocoding port keeps its five-return signature, and no `internal/geo` package is created.**

The recommendation's premise turned out not to hold. It warned about deciding "before three domains
adopt the wide signature" — but the width is not what a domain adopts. It appears exactly once, in
`jobs/ports.go`, and is converted into `Location` in the next statement; no store method, no
handler, no response type and no test carries five return values. What a second domain would adopt
is a *coordinate type*, and a wide signature does not force that type to be wide.

Three further reasons, in the order they weighed:

1. **A struct would not remove the `found bool`.** Not-found is an outcome whatever shape the answer
   has, so the ergonomic gain is one return value — not the comma-ok pattern, which `Docs/06` §4.1
   argues is better than a sentinel precisely because the compiler checks it.
2. **The second consumer is speculative.** What would justify a neutral package is shared distance
   arithmetic, and no ticket asks for any. SHIP-79 has a provider declare a service area and
   SHIP-81 filters on it; neither names a radius in kilometres. `money`, `pagination` and
   `ratelimit` are seeded ahead of the code because their consumers are certain; this one's is not.
3. **The cost is immediate and the benefit is not.** It needs an entry in `internal/boundaries`,
   which is a shared file, in the middle of a wave — the same reason the option was unavailable when
   the recommendation was written.

**The trigger for revisiting is named rather than left to judgement: the first ticket that needs the
distance between two coordinates in a domain other than `jobs`.** SHIP-81 is the likely one. At that
point `internal/geo` is written — `Point` and the haversine, nothing else — the port narrows to
`Lookup(ctx, address) (geo.Point, bool, error)`, and the change is confined to `jobs/ports.go`, the
two adapter methods and one conversion function. Writing a second copy of a haversine is the signal;
a wide signature is not.

**One consequence to expect: staging and production get no geocoder at all.** No document names a
maps vendor and `internal/config` has no `GEOCODING_*` fields to build one from — adding them is a
shared-surface change SHIP-60 could not make from a domain branch. `cmd/api` therefore passes `nil`
outside development and logs it once at startup. Falling back to the deterministic stub was the
alternative and is worse: it writes coordinates that are stable, plausible, inside Australia and
entirely fictional, and a fictional coordinate on a real job is much harder to notice than none.
~~**This is a request rather than a finding.** Whoever next owns `internal/config` adds the two
variables and `newGeocoder` builds the provider from them.~~ **Granted at SHIP-15g — see §3.**
`GEOCODING_BASE_URL` and `GEOCODING_API_KEY` exist, and `newGeocoder` builds
`geocoding.NewProvider` from them. The nil path survives for the case where nothing is configured,
and the refusal to fall back to the stub outside development is unchanged.

### What SHIP-61 built, and what it is the first of

`POST /v1/jobs` creates a Draft owned by whoever the token says is calling. **There is no field
for naming a customer**, and that is not merely convenient: a customer id in the body would be an
authorisation decision made from client input, which `Docs/07` §3 puts on the platform.

**It is the first authenticated state-changing endpoint the service has.** Everything under
`/v1/auth` is public by necessity, so until now SHIP-44's scoped idempotency had nothing to scope.
Keys from this endpoint land in `idem:v1:<subject>:<key>`, which is the gate `CLAUDE.md` held
protected endpoints behind and which SHIP-44 closed.

**Every field is optional**, including both addresses. `Docs/01` §4.1 lets a customer save a draft
and come back to it and the app captures a job over several steps (SHIP-71…75), so an empty body is
a legitimate "start a job for me". Completeness is decided at publication (SHIP-63), which is where
`Docs/02` §2 puts it — "required job details valid". What is checked here is that whatever *was*
supplied is well formed, and the asymmetry that matters is between an address that is absent and one
that is half filled in: the first is a draft in progress, the second is a mistake.

**Only a customer account may create a job, and the rule reads `users.role` rather than the token's
claim.** 000400 said this had to be enforced where the draft is created, because a foreign key
cannot see another table's column. `jobs` reading `users` is sanctioned rather than a boundary
crossed — the table is in the shared migration block precisely because it is read across the whole
service — and what is *not* done is importing `identity` to ask. The verify section demonstrates it
with a provider whose minted token claims `customer`, which is the case a token-only check would
pass.

**Status reaches the wire in lower snake case, and `Status.Wire` derives it rather than tabulating
it.** `Docs/10` §4.7 requires the form; the stored form keeps `Docs/02` §1's own strings. SHIP-56a
owns the mapping in three languages when it lands, and until then this is the Go copy — derived, so
it cannot disagree with the constants, and pinned by a test that writes all twelve out, because a
client already branching on `driver_assigned` cannot have it renamed underneath it.

**The response omits everything that is empty** rather than sending `""` and `0`. A draft is mostly
empty for most of its life, and a client needs to tell "not filled in" from "filled in with
nothing". There is no budget field: SHIP-67 brings the column together with the serialisation test
that proves it cannot reach a provider, rather than the field arriving first and the proof later.

**Two migrations, and one of them moved a ticket earlier than 000400 planned.** 000403 is SHIP-60's
addresses. 000404 carries the draft's own fields — description, dimensions, weight, vehicle
requirement, handling notes and the two date windows — which 000400's forward plan attributed to
SHIP-62. They moved because `POST` and `PATCH` accept one field set and SHIP-61 lands first; two
schemas for one thing is two places for it to drift. The date windows were in no ticket's plan at
all and are needed by SHIP-68, which expires an Open job at the earlier of fourteen days or the
pickup date passing.

### What SHIP-62 built, and the two refusals worth reading

`PATCH /v1/jobs/{id}` applies a partial edit to a draft the caller owns. `PATCH` rather than `PUT`,
because the app edits one step of the job wizard at a time and a `PUT` would require it to send
every field it is not changing — which is how a client that has not been updated for a new field
silently clears it.

**Absent, null and empty are three different things**, and the third is not decoration. Every field
in the request is a pointer, so a non-nil pointer to a zero value clears the field. Without it a
customer could add a handling note and never remove it, because `""` would be indistinguishable
from not mentioning it.

**An address is replaced as a whole, never merged part by part**, and a replaced address discards
its coordinate and is resolved again. Merging would let an edit produce an address made of two
different places with no error reported; keeping the coordinate would send a driver to the previous
one. That invariant is why the coordinate lives on `Location` beside the address rather than as two
more fields on the job — the bad state is unrepresentable rather than merely avoided.

**A stranger's edit answers 404, and byte-identically to a job that does not exist.** A draft is
visible to nobody but its owner, so a 403 would confirm that a job with that id has been created —
information the caller had no way to obtain. The domain still keeps `ErrNotJobOwner` and
`ErrJobNotFound` apart, so a test can tell "the non-owner was refused" from "the job silently
stopped existing", which are the same answer to a client and very different defects. `make verify`
compares the two response bodies rather than only their statuses.

**Ownership is checked before status, and the order is load-bearing.** Checking status first would
let a stranger distinguish somebody else's draft from somebody else's published job by which
refusal came back — the codes differ even though both are refusals.

**Only a draft can be edited.** An Open job carries bids made against the details as they were, so
an edit is a 409 with `jobs_not_a_draft` rather than a silent success or a 403; the client's correct
response is to reload and show the real status. `Docs/02` §3 allows a documented change process
after award, and that is SHIP-69's, not a `PATCH`.

**The edit runs in a transaction and says so rather than trusting its caller.** The read, the
ownership check and the write are one decision against one version of the row, and `lockJob`'s
`FOR UPDATE` only holds for the length of a transaction — outside one the lock is released the
instant the `SELECT` returns and two concurrent edits interleave into a job carrying half of each.
`UpdateDraft` refuses a pool the way `Transition` does.

### What SHIP-64 built, and why it is the run that mattered

`POST /v1/jobs/{id}/cancel` ends a job its owner has not yet had awarded. **It is the first endpoint
in the service that moves a job**, which makes it the first real client of SHIP-57's guard:
creation lands at Draft because 000400 defaults the column, and an edit never touches `status`, so
everything the guard built — the permitted table, the history row, the trigger that refuses a status
write without one, the event — had until now only ever run from a test.

**A verb under the resource, not a field on the job.** `PATCH {"status": "cancelled"}` would be a
client naming a state; this is a client naming an intent and the platform deciding what the state
becomes. That is the whole distinction `Docs/02` §2 exists to hold, and it is why no request schema
in this domain has a `status` field.

**"Unawarded" is enforced by `Docs/02` §2's table rather than by a list in `cancel.go`.** The
endpoint asks `Permitted(status, Cancelled)` — the same exported function the guard itself uses —
so Draft, Open and Negotiating are cancellable and everything from Awarded onward is not, without a
second copy of the lifecycle anywhere. `Docs/02` §6.2 is the reason it stops there: once a provider
has committed, ending the job is a support matter, and after pickup the route out is Disputed.

**Cancelling a job that is already Cancelled answers 200 and writes nothing.** The idempotency
middleware absorbs the retry that reuses its key; this absorbs the one that does not — a phone that
lost its connection, was restarted, and generated a fresh key for the same intent. `ErrAlreadyInStatus`
was separated from `ErrTransitionNotPermitted` at SHIP-57 precisely so a caller could make this
choice, and `Docs/02` §3.1 makes the same call for a queued update that has been overtaken: absorbed,
not reported as an error. No second history row, no second event — verify checks the count rather
than the status.

**A new sentinel rather than a mapping from the general one.** `ErrJobNotCancellable` is narrower
than `ErrTransitionNotPermitted` on purpose: mapping the general sentinel to `jobs_not_cancellable`
at the transport edge would give SHIP-63's publish the wrong code the day it lands. One code per
intent, not one per guard failure.

**The disclosure rule is applied per endpoint, and that is deliberate rather than repetitive.** A
stranger's cancellation answers 404 byte-identically to a job that does not exist, and `make verify`
compares the bodies. A job is discoverable through whichever route forgets the rule, not through the
strictest one.

**`make verify` moves a job to Awarded with SQL, because no endpoint can.** SHIP-63 publishes and
SHIP-92 awards; neither exists. The fixture writes the `job_status_history` row and names it in the
transaction-local setting, which is the only protocol 000402 accepts — a bare `UPDATE jobs SET
status` is refused. So even a fixture written to bypass the guard cannot, which is worth more than
the check it sets up.

### What SHIP-65 built, and the half of its *Done when* that could not be met

`GET /v1/jobs/{id}` returns a job in full to the customer who owns it, and 404 to everybody else.

**Its *Done when* says "including budget", and the budget column does not exist.** It was
deliberately not added. `Docs/11` §8 makes SHIP-67 single-owner with SHIP-83 precisely so the column
and the serialisation test proving it cannot reach a provider land together, and SHIP-67 is not in
this wave. A field that arrives before its proof is the one arrangement worse than a field that
arrives late — so the endpoint is complete and the sentence is not. **Recorded here rather than
resolved silently**, and §4 carries SHIP-65 until SHIP-67 closes it. `make verify` asserts the
absence of a `budget` key, so the day it appears somebody has to come to that file and say so.

**`Docs/09` cannot be satisfied here as written, and that is worth a decision rather than a
workaround.** SHIP-67 owns the budget column and **depends on SHIP-65**, so SHIP-65's *Done when*
asks for a field whose own ticket cannot start until SHIP-65 is finished. No implementation of
SHIP-65 can meet that line; either the words belong to SHIP-67 or the two tickets are one.
`CLAUDE.md` says a contradiction with a document is not resolved silently in code, so it is
recorded here and `Docs/09` is left untouched — **the wording is the repository owner's to correct**.

**The same `jobResponse` the write endpoints answer with**, and `make verify` compares a `GET`
response with the `PATCH` response that preceded it byte for byte. A "detail" shape carrying a field
or two more would make every write response a subset a client has to special-case, which is where a
field quietly goes missing.

**A non-locking read was added beside `lockJob` rather than a flag on it.** `FOR UPDATE` inside a
`GET` serialises every reader behind whatever is writing, and a handler using the pool directly
would hold the lock for an unbounded time. `TestReadingAJobDoesNotWaitOnAWriter` holds the row in
one transaction and reads it from another, so the property is pinned rather than left to a comment.

**Ownership is checked in Go, not in the `WHERE` clause.** `WHERE id = $1 AND customer_id = $2`
returning nothing cannot say whether the job is somebody else's or nobody's, and those are one
answer on the wire and very different defects.

**Reading is not restricted to Draft.** Editing stops there (SHIP-62) because an Open job carries
bids made against the details as they were; reading has no such reason, and a customer who cannot
see their own Open job cannot be shown its bids.

### What SHIP-66 built, and the package it was the first to need

`GET /v1/jobs` returns the calling customer's own jobs, newest first, filterable by status and paged
by cursor.

**`internal/pagination` now exists**, which is the pre-seeded infrastructure list working exactly as
`Docs/10` §6 intended: the entry has named SHIP-66 as its owner since wave 2, and writing the
package required editing no shared file — not `internal/boundaries`, not `Deps`, not `cmd/api`. It
holds the encoding and the bounds and nothing else: a `Cursor` is a list of opaque strings and the
domain decides what they mean, which is what lets a later domain order by something other than a
timestamp with one implementation between them.

**The cursor is versioned, and that is the part worth keeping.** A cursor is held across an app
restart and across a deployment, so the failure to prevent is not a rejected cursor but an accepted
one that means something else now. A one-byte prefix and an insisted-on field count make a stale
token a clear refusal. The split of responsibility is deliberate: `pagination` establishes the
*shape*, and `jobs` establishes the *meaning* — a cursor whose two fields decode but do not parse as
a timestamp and a UUID would otherwise reach the query as a zero time and silently answer with the
first page.

**Two fields, not one, and `created_at` alone would be a bug.** It is not unique — two drafts saved
in the same millisecond are ordinary — so a cursor that could not break the tie would repeat or skip
a job at exactly the page boundary, which is the failure keyset pagination exists to avoid arriving
by another route. `ORDER BY created_at DESC, id DESC` matches `idx_jobs_customer`, so the list
needed no index of its own.

**~~The page sizes are constants in `internal/pagination`, and `Docs/10` §4.5 says they come from
configuration.~~ Granted at SHIP-15g — see §3.** They were constants because `internal/config` had
no fields for them and adding two is a shared-surface change a domain branch cannot make — the same
position SHIP-60 reached over `GEOCODING_*`, which is what turned two requests into one prep
ticket. `PAGINATION_DEFAULT_PAGE_SIZE` and `PAGINATION_MAX_PAGE_SIZE` now exist, and **no caller
changed**, exactly as the request predicted: callers ask `pagination.Limit`, and `cmd/api` installs
the bounds once before serving. They live in `pagination` rather than in `jobs` because every list
endpoint needs the same answer.

**A limit above the maximum is narrowed rather than refused, and a limit of `0` is refused.** The
asymmetry is deliberate: a client asking for more than the platform will give is asking for a page,
and refusing it would turn a server-side tuning change into a broken client — whereas `?limit=abc`
is a client defect that answering with the default would hide.

**`data` is always an array, never `null`.** `pagination.NewPage` is a constructor for that one
reason: a nil slice marshals to `null`, and a client iterating it breaks the first time a new
customer with no jobs opens the app and never again in testing.

**One status per request rather than several.** SHIP-76 shows the customer's jobs grouped by status,
and reading the list once and grouping it beats one request per group; widening the parameter later
is additive. The filter takes the wire form and refuses the stored one — `?status=Draft` is a `400`,
not an empty list, because an empty list would tell a client its filter worked.

### SHIP-15g — the wave-4 pre-step, and one thing it found already built

Three shared surfaces, closed serially before wave 4's tracks opened. The pattern is SHIP-15c's
and SHIP-15e's: a wave costs one prep ticket, and §1 now says so rather than treating it as a
surprise.

**`internal/config` gained what two wave-3 lanes had each parked.** `GEOCODING_BASE_URL` and
`GEOCODING_API_KEY` (SHIP-60's request) and `PAGINATION_DEFAULT_PAGE_SIZE` /
`PAGINATION_MAX_PAGE_SIZE` (SHIP-66's, required by `Docs/10` §4.5). Both lanes had written a
documented constant and filed a request, because `internal/config` is a shared file a domain
branch must not edit — the rule working, but twice in one wave, which is what made it a ticket.

`cmd/api/routes_jobs.go` now builds `geocoding.NewProvider` when a base URL is configured, so the
nil-geocoder path is reached only when nothing is set. **Falling back to the stub outside
development is still refused**, for the reason SHIP-60 gave: it writes coordinates that are
stable, plausible, inside Australia and entirely fictional, and a staging environment full of
those is worse than one with none, because it looks like it works. A key set without a base URL is
now refused at load — the shape of a half-finished configuration whose only symptom would
otherwise be silence.

`internal/pagination` keeps `Limit(raw)`, exactly as SHIP-66 promised: the bounds are package
state installed once by `cmd/api` before it serves, so no caller changed. `SetBounds` ignores
values `internal/config` would have refused, because a zero default would make every page empty
and read as a database with no rows.

**The migration guard — the defect that would have bitten Track C in this very wave.** Stock
golang-migrate applies only migrations numbered *above* the recorded version. Numbers here come
from reserved per-domain blocks, not in time order, so a development database sitting at `000404`
silently skips a new identity migration at `000105`: `make migrate-up` prints "no change" and the
column never appears. **Fleet is block 300–399 and SHIP-78 is in this wave**, so this was days
from happening rather than theoretical.

Decided: **fail loudly, do not reorder.** Applying out of order would run a migration against a
schema it was never written for and that no test covers — a migration is a program, not a patch,
and discovering the mismatch halfway leaves a dirty schema. Refusing is smaller and matches what
this repository already does: `migrate-create` refuses a missing `domain=` rather than guessing.

Answering it at all needed a record, because `schema_migrations` holds one integer and cannot say
whether `000105` ran before the file existed. `cmd/migrate` now keeps `schema_migrations_applied`
beside it, created by the tool rather than by a migration, and a database that predates it is
backfilled from its version on first run. **Demonstrated by hand, not believed**: a throwaway
`000105` on a database at `000404` produced a refusal naming the migration, its domain, and the
one-line fix; `make migrate-down n=all && make migrate-up` then applied it. The narrow gap worth
naming is that a database *already* bitten before the guard existed is backfilled as healthy —
nothing can distinguish that, and the same command fixes it.

**`cmd/worker` did not need the registration seam this ticket was scoped to build.** SHIP-67a
built it in wave 2: `Deps` is pre-seeded, `register` is called from an `init`, and a domain
contributes `cmd/worker/tasks_<domain>.go` and edits nothing shared. SHIP-68 and SHIP-134 will be
its first two clients and **will not collide**. Recorded because the wave-4 plan named this as the
one thing making four tracks safe, and it was already true.

What was genuinely missing is smaller and real: **a task that owns a resource had no way to
release it.** `Deps` carries the pool, the clock and configuration on the reasoning that a task is
a pure function of those — true of job expiry, bid expiry and auto-complete, all query-only, and
false of SHIP-134, which holds a Kafka producer with buffered messages behind it. `Task.Close` is
now optional and called after the task's loop stops, with a fresh context rather than the
cancelled one, since a `Close` inherited from a cancelled context could never flush. A hook on the
task rather than a field on `Deps`, so every future task does not carry a producer it never uses.

### What SHIP-105 built, and the two rules that follow from a driver having no account

`driver_assignments`, migration `000600` — the first in the delivery block, and no endpoint.
**Demonstrated by its own tests**, `services/core/migrations/driver_assignments_test.go`, rather
than by `make verify`, which drives HTTP endpoints and this adds none. That is the treatment
SHIP-56, SHIP-57 and SHIP-67a already had, and it is not the weaker one here: everything asserted
is a constraint, an index or a trigger, and `Docs/06` §4.1 is right that "a mock happily accepts a
write that the actual constraint would reject".

**There is no foreign key to `users` anywhere in the table, because there is nothing to point
at.** The driver portal is link-authenticated (`Docs/07` §3) and a driver holds a job-scoped token
rather than a session. `000401` had already committed to the consequence —
`job_status_history.actor_id` names an *assignment* when `actor_type` is `'driver'` — and this is
the row that reference resolves to. Two rules follow from it, and both are worth reading before
SHIP-106:

- **An assignment's identity is immutable**, enforced by a trigger in the shape `000005` uses for
  `users.role`. Editing `driver_name` is not an update; it silently re-attributes every milestone
  and transition already recorded against that assignment to a different person. Replacing a
  driver ends one assignment and creates another, and the ended row stays.
- **At most one assignment is live per job**, `uq_driver_assignments_active`, a partial unique
  index on `job_id WHERE unassigned_at IS NULL`. Two live assignments become two valid driver
  links the moment SHIP-107 hangs a token off this row, which is how SHIP-108's "exactly one job,
  and nothing else" quietly stops being true. `unassigned_at` is set once and cannot be cleared or
  moved: reviving an assignment restores a driver, and their link, to a job they were taken off.

`ck_driver_assignments_mobile` checks E.164 shape and nothing about allocation, mirroring
identity's `validE164`. It is not tidiness — the mobile is the only channel the platform has to a
person with no account, and a malformed one is a driver who never receives the link and a job that
stalls with nobody knowing why. The name check is `driver_name ~ '\S'` rather than
`btrim(driver_name) <> ''`, because `btrim`'s default character set is the space alone and the
first version of it accepted a tab — which is exactly what a form field returns when somebody tabs
through it. The test caught that, which is the argument for testing against a real database rather
than reading the constraint and believing it.

**What SHIP-106 has to decide, which this deliberately did not:** who made the assignment is not
stored. It is an account for a provider and *not* an account for an administrator —
`ck_users_role` refuses `'admin'`, because admin sign-in is a separate system (SHIP-147) — so
recording it means the polymorphic `actor_type`/`actor_id` pair `audit_log` and
`job_status_history` carry, and SHIP-106 is the ticket that knows whether an administrator may
assign at all. Until then the `Awarded → Driver assigned` history row records who did it. The same
narrowness, which is `000400`'s discipline, leaves the token columns to SHIP-107 and revocation to
SHIP-109. There is also no `CHECK` that the job has reached `Awarded`: a `CHECK` cannot see
another table's column, so SHIP-106 enforces it where the assignment is made, exactly as `000400`
does for the customer's role.

## 4. Partly done — do not treat these as finished

| Ticket | Exists | Missing |
|---|---|---|
| **SHIP-65** | The endpoint, the owner-only rule, the full customer view | The `budget` field its *Done when* names. The column is SHIP-67's, with the proof that it cannot leak |
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

Strict build order says the next ticket is the lowest-numbered open one, which is **SHIP-24** — and it is blocked on X-2, as are the other three M0 stragglers. The lowest-numbered ticket that can actually be started is **SHIP-50**. **Twenty-seven tickets have every dependency met: 22 code tickets worth 79 points, plus the five Track-X tickets worth 10.** Build order is a preference rather than a constraint at this point. The list below is computed from `Docs/09`'s dependency column against `Docs/11-done.txt`, not maintained by hand — and it is the **complete** startable set, because an earlier version of this table was a curated selection that read like a full list.

| Ticket | Pts | Area |
|---|---|---|
| SHIP-50 | 5 | Flutter token refresh interceptor — the mobile analogue of `internal/identity`; every later mobile ticket calls through it |
| SHIP-55 | 2 | Flutter login screen — closes M1, and **must settle biometric unlock**; see §9 |
| SHIP-56a | 2 | Status codegen for Go, Dart and TypeScript — cut from waves 2 and 3 because it writes into four trees |
| SHIP-67 | 3 | Budget stored and never serialised — **the invariant's tripwire**; see §8 on the broken SHIP-83 pairing |
| SHIP-68 | 5 | Job expiry — the first real client of `cmd/worker`, and it needs `000404`'s date-window columns |
| SHIP-71 | 3 | Flutter job creation, locations step |
| SHIP-76 | 3 | Flutter customer job list — SHIP-66 unblocked it |
| SHIP-78, 80 | 8 | `vehicles` and `bids` — the M3 foundation. **SHIP-80 makes the award transaction startable** |
| SHIP-105, 110 | 5 | `driver_assignments`, `milestones` — M4 tables, no endpoints, **same migration block** |
| SHIP-134 | 5 | The outbox publisher — §4 has been carrying its table since wave 1 |
| SHIP-163 | 3 | Dispute intake endpoint — dependencies met since SHIP-57, and **this file had never listed it** |
| ~~SHIP-114~~ | 5 | **Dependencies met, not buildable** — no object storage in the local stack; see below |
| ~~SHIP-124~~ | 5 | Buildable, but it is `core/queue` for offline delivery — M4 work, not M1 closure |
| ~~SHIP-147~~ | 5 | Buildable, but it edits the `newRouter` middleware chain — **shared-platform work**, not a track slot |
| ~~SHIP-168~~ | 3 | Buildable, but its store link does not exist until X-2/X-3 publish listings |
| ~~SHIP-169~~ | 3 | Buildable, but it touches `users`, in the **shared migration block (1–99)** |
| ~~SHIP-174~~ | 3 | **Not demonstrable** — no Datadog agent in compose, no `DD_*` config, no account |
| ~~SHIP-178~~ | 3 | **Not demonstrable** — install base comes from App Store Connect and Play Console (X-2, X-3) |
| ~~SHIP-182~~ | 5 | **Not demonstrable** — there is no production and no managed backup |
| ~~SHIP-183~~ | 3 | A **decision ticket** — §9 parks the per-account-lockout question here; it also rewrites limits on every domain's routes |

Plus **X-1, X-3, X-4, X-5 and X-6**, none of which is code and none of which has started. X-5 and X-6 need no third party at all.

**Nine of the 22 are struck, which is the useful signal in this table.** Dependencies being met is not the same as a ticket being startable: four are not demonstrable with the tooling that exists, three are shared-platform work a domain branch must not do, one is a decision, and one is in the wrong milestone. The startable-and-sensible set is thirteen tickets, and wave 4 takes eleven of them.

**SHIP-114 is neither ready nor blocked on a third party, which is a third category this file needed.** Its dependencies are met, but `internal/platform/storage/` is `doc.go` alone and `deploy/docker-compose.yml` has no MinIO or equivalent, so "receives a short-lived pre-signed URL and uploads directly" cannot be demonstrated. Wave 1 already paid once for counting a ticket whose acceptance criterion needed a tool nobody had installed. **It needs a lettered ticket adding object storage to the local stack first**, as shared-platform work.

**The identity bottleneck is gone.** Seven of the eight tickets that lived in `internal/identity` — SHIP-39, 40, 41, 42, 43, 46, 47 — landed in wave 3, and the eighth, **SHIP-50**, is Flutter: it is written *against* the package, not in it. `Docs/10` §9.1 still gives one package directory to one agent at a time, but for the first time since wave 1 that rule constrains nothing, because no queued ticket needs to open `internal/identity` at all. The next one that will is SHIP-183, which is a decision before it is a change.

**Public routes still share the anonymous idempotency scope, and that remains safe.** `replayOrRefuse` fingerprints method, path and body, so reading another caller's stored response requires sending their exact request — which, on every route on `Docs/10` §4.1's allow-list, means already holding the secret material in their body. `make verify` checks the anonymous scope still works, because scoping idempotency into uselessness would be a subtler regression than leaving it shared.

## 7. Wave 3 — what landed

One ticket serially, then three lanes concurrently. **Seventeen tickets, forty-eight points, all delivered, no trim taken.**

| Step | Tickets | Landed |
|---|---|---|
| **Pre-step** (serial, primary tree) | SHIP-15e | Merged at `fee58cd` before any lane started |
| **Lane A** identity | SHIP-39, 40, 42 → SHIP-41, 43, 46, 47 | All seven, two sequential runs on one branch |
| **Lane B** jobs | SHIP-60, 61, 62 → SHIP-64, 65, 66 | All six, two sequential runs on one branch |
| **Lane C** flutter | SHIP-51, 52, 53, 54 | All four, one run |

The exit criterion was: a person can sign in, stay signed in across a rotation, see and revoke their devices, and be throttled when they guess; a customer can create, amend, cancel, read and list a job draft through the guarded status function; and the client drives registration and verification against the live API. **All of it holds**, and `make verify` went from 105 checks to **208**.

### One conflict in the entire wave, and it was a number

The wave's whole pre-step was designed around an anticipated collision in `contracts/openapi.yaml`: two lanes each adding `$ref` lines to the sorted `paths:` block, with a documented take-both-sides-and-re-sort recipe waiting for it. **It never materialised.** Lane A's four `/v1/auth/*` entries and lane B's `/v1/jobs*` entries landed in different regions of the sorted block and merged cleanly, as did the `tags:` block and `Docs/10-api-error-codes.md`.

The one conflict was this file's `make verify` check-count line — and it is worth recording *why* it was the only one, because the reason is not luck. Every other shared file in the wave either has a `merge=union` attribute (`Docs/11-done.txt`, `routes_golden.txt`), is generated (`Docs/10-api-error-codes.md`), or is sorted with a test holding it sorted (`contracts/openapi.yaml`). The check count is none of those: it is a hand-maintained scalar in prose, which is the one shape a merge cannot resolve. **Three different values met in it and none of the three was right** — `develop` said 123, lane A said 170, the truth was 208, and `develop`'s figure had already been stale by 20 before the merge began. Resolved by measurement, which is the only resolution that could have been correct.

### The pre-seeded infrastructure list had its first real test

`internal/ratelimit` (SHIP-47) and `internal/pagination` (SHIP-66) were both written in this wave, in different lanes, **with no shared-file edit between them.** Both names had been registered in `internal/boundaries` ahead of the code precisely so that whoever needed one first could write it without touching a file another lane held. That is the mechanism working as designed rather than as asserted, and it is the strongest argument yet for seeding a name before the package exists. Only `money` is left unwritten.

### What the wave cost in scale

Wave 1 delivered 32 points, wave 2 delivered 37, wave 3 delivered **48** — a 30% step up that the plan named as the wave's real risk, taken without invoking the trim order. Three lanes, five agent runs, one conflict.

## 7a. Wave 2 — what landed

Two tickets serially, then three tracks concurrently. Thirteen tickets, thirty-seven points, all delivered.

| Step | Tickets | Landed |
|---|---|---|
| **Pre-step** (serial, primary tree) | SHIP-15c, SHIP-44 | Both, on separate branches merged before any track started |
| **Track A** identity | SHIP-30, 45, 31, 34, 33, 36 | All six |
| **Track B** jobs + worker | SHIP-56, 57, 57a, 67a | All four |
| **Track C** client + web CI | SHIP-48, 49, 23a | All three |

The exit criterion was: a person can register, receive an email token and a phone OTP in the development log, confirm both, and be recorded as verified, with the role immutable and enforced by the database; the `jobs` table constrains all twelve statuses behind one guarded function; `cmd/worker` claims work under `FOR UPDATE SKIP LOCKED`; the client persists a refresh token in Keychain and Keystore and routes correctly on cold start; and `routes_golden.txt` shows **exactly five new routes, every one already on the `publicMutatingRoutes` allow-list**.

**All of it holds.** The route count is five, `manifest_test.go` is untouched — so nobody widened the allow-list to make a route fit — and `make verify` went from 66 checks to 105.

### The pre-step earned its place, and that is the wave's main lesson

Wave 1's three tracks produced one conflict because nothing they wrote met in a shared file. Wave 2's would have met in five. SHIP-15c closed them first — a pre-seeded `Deps`, a fourth boundary rule, `httpx.RegisterCode`, a merge recipe for the zero-context files, and an extensible `make check`.

**The result: three tracks, and not one conflict in code.** Everything the three collided in was this file, in §3 and §10, where a union is the only correct resolution and `make status` fails loudly if a line is dropped.

Two of SHIP-15c's mechanisms caught real mistakes within the wave: the protocol-code count test caught SHIP-44 adding `token_expired` and named the three other places that had to change with it, and the boundary rule refused the `httpx → identity` import SHIP-44 had a genuine motive to create.

### Three findings the tracks made, which nobody had before

- **The worktree test isolation never worked.** Four places named `TEST_DATABASE_URL` as the mechanism; `CREATE DATABASE … TEMPLATE` resolves at cluster scope, so only `TEST_TEMPLATE_DB` isolates. One worktree had none, and a single `make check` there dropped the primary tree's template mid-clone.
- **`make test-db-template` had a live data-loss path** — a literal substitution that silently matched nothing on a URL with no query string and then ran every migration into the developer's real database.
- **`httpx.H` and `httpx.DecodeJSON` are specified in `Docs/10` §4.3 and do not exist**, exactly as `httpx.RegisterCode` did not until SHIP-15c. Track A wrote them unexported in `internal/identity` rather than reaching into a shared package. **The second domain to want them should promote, not copy** — see §9.

### What wave 2 taught, for the next one

- **A pre-step that closes shared surfaces pays for itself immediately.** Seven points bought three conflict-free tracks. Wave 3 should ask what its tracks would meet in *before* dispatching, not after.
- **Documented-but-absent mechanisms are the recurring defect here.** Three have now been found — `RegisterCode`, `H`/`DecodeJSON`, and the test isolation. Each was documented, believed, and untrue. **A mechanism this file claims exists is one nobody checks for.**
- **The last merge always conflicts in this file.** Three tracks each append to §3 and §10. §10 in particular is a candidate for `merge=union` in `.gitattributes`, because a union there is always a superset and `make status` fails on a drop rather than passing quietly — unlike YAML, where a union is invalid. Recorded in §9.
- **Agents deviate from a brief in both directions, and both need checking.** One track merged `develop` into its own branch after being told not to; its conflict resolution was nonetheless correct. Another corrected a paragraph in `Docs/07` that was outside its ownership, and was right to — the reasoning behind a decision had stopped being true. Neither was harmful; both were only visible because the diff was read against the stated ownership.

## 7b. Wave 1 — what landed

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
- Tracks do not touch **§1, §2, §6 or §7** of this file — three agents doing the same arithmetic on one table is a guaranteed conflict, and `make status` computes the real numbers anyway. Each track adds its own tickets to §3 and the §10 fence; the rest is reconciled once when the wave lands, as a separate pass. Wave 2 did exactly that and it worked: the only conflicts in the whole wave were in §3 and §10, and both were unions.

## 8. Hard gates ahead

**~~SHIP-44 is a choke point.~~ Cleared — see §3.** `httpx.Idempotent` is wired with `httpx.SubjectScope`, and the freeze on authenticated state-changing endpoints is lifted. `make verify` demonstrates the separation against a running service rather than asserting it.

Kept here rather than deleted, because the shape recurs: this was described only as a constraint on *other* work, and so was never read as work itself while its dependencies had been met since wave 1. A gate with satisfied dependencies belongs in §6 the moment it becomes buildable.

**The next gate of the same kind is SHIP-108, and it has no entry yet.** The driver's job-scoped token is a second verifier, and `Docs/10` §5 requires that neither token system can be exchanged for the other. `identity.AccessTokenVerifier` refuses the driver audience today and there is a test for it — but the other direction cannot be tested until the driver verifier exists. **Whoever writes SHIP-108 writes both directions**, which is the reason this file has always kept both verifiers with one owner.

**One thing SHIP-44 did not do: `RequireDriverToken` and `RequireAdmin` are declarable and unenforced.** A route declaring either now panics at startup rather than being served open, so the failure direction is safe. SHIP-108 and SHIP-147 supply the middleware.

**SHIP-91…95 never parallelise.** Own branch, nothing else on it. The partial unique index, the lock ordering, the idempotency interaction and the race tests are one design; two people produce two lock orderings, which is a deadlock or a lost update. Consider using a second agent adversarially instead — one implements 91–94, another writes SHIP-95 from `Docs/02` §3 and `Docs/08`'s four named races *without reading the implementation*.

**Also single-owner, for reasons in `Docs/10`:** SHIP-57 (the status guard), SHIP-67 with SHIP-83 (budget privacy — test the serialised response, not struct fields), both token verifiers, and the middleware ordering in `newRouter` — which is now load-bearing in a second way, since `ResolveSubject` sitting outside `Idempotent` is what makes the scope work at all.

**The SHIP-67 / SHIP-83 pairing cannot be honoured in one wave, and that is a fact about the dependency graph rather than a scheduling preference.** Verified against `Docs/09`: SHIP-83 depends on SHIP-82 → SHIP-81 → (SHIP-79, SHIP-80) → SHIP-78. SHIP-67 is startable **now**; SHIP-83 is four tickets and three hops away. Any wave that starts SHIP-67 either breaks the pairing or defers a startable ticket for a chain that is not close to landing. **The ticket that takes SHIP-67 must therefore settle this explicitly** — build now and reserve SHIP-83 to the same owner later, or defer both — and record which, here and in §3. What the pairing was protecting is worth restating so the choice is made on it: the invariant must be proved against the **serialised provider response**, not against struct fields, and SHIP-67's own *Done when* is precisely that serialisation test. Note also that `make verify` currently **asserts no `budget` key is present**, so whichever ticket adds the column must change that assertion in the same commit — it is the invariant's tripwire and it should be moved loudly, never quietly.

## 9. Open recommendations nobody has decided

**~~Job status as a database guarantee.~~ Decided and built at SHIP-57 — see §3.** The trigger exists, and it asks for more than the recommendation did: not merely that a session variable is set, but that it names a `job_status_history` row written in the same transaction which describes this job making exactly this move. The weaker form would have been a flag any caller could set; this one cannot be satisfied without leaving the record, which is what makes SHIP-57a's *Done when* structural rather than remembered.

**~~Whether an adapter's value types get a home.~~ Decided at SHIP-60 — see §3.** **No neutral geo package; the geocoding port keeps its five-return signature.** §9's premise did not survive contact: it warned about deciding "before three domains adopt the wide signature", but the width is adopted **exactly once**, in `jobs/ports.go`, and converted to a `Location` in the next statement — no store method, handler, response type or test carries five return values. What a second domain would adopt is a *coordinate type*, and a wide signature does not force that type to be wide. **The revisit trigger is named rather than left to judgement: the first ticket needing the distance between two coordinates in a domain other than `jobs`, likely SHIP-81.** At that point `internal/geo` gets a `Point` and the haversine, the port narrows to `Lookup(ctx, address) (geo.Point, bool, error)`, and the change is confined to `ports.go`, two adapter methods and one conversion. The original reasoning is kept below because the revisit will need it.

Wave 1 surfaced a consequence of the consumer-declares-the-interface rule that nobody had hit before. A domain's `ports.go` must name the adapter's method signature and may not import the adapter, so no struct declared in an adapter can appear in one. Geocoding therefore ended up as:

```go
Lookup(ctx context.Context, address string) (lat, lng float64, formatted string, found bool, err error)
```

and not-found is comma-ok rather than a sentinel error, because `errors.Is(err, geocoding.ErrNotFound)` would also be an import. The reasoning is correct and the lint agrees. But a neutral infrastructure package holding a coordinate type — the same shape as the pre-seeded `pagination`, `ratelimit` and `money` — would let both sides name it with no dependency edge either way, and that option was unavailable only because `internal/boundaries` was a forbidden shared edit mid-wave.

**~~`httpx.RegisterCode` is documented but does not exist.~~ Decided and built at SHIP-15c.** The registry, the uniqueness tests in `cmd/api`, and the generated `Docs/10-api-error-codes.md` all exist; `Docs/10` §4.4 is now true and says so, including that it was not. The choice was between building the mechanism and amending the document to match reality, and building won because SHIP-30 and SHIP-57 both need it on separate tracks in the same wave.

**~~A ticket for the web CI workflows.~~ Written as SHIP-23a at SHIP-15c, and built — see §3.** Two workflows, path-filtered per surface, and the filter demonstrated against a changed-file matrix rather than believed.

**~~`make web-check` runs its type-check before its build.~~ Decided and fixed at SHIP-15e — see §3.** `web-check` is now `web-lint web-build web-typecheck`, and the `make web-build` workaround is deleted from both web workflows. Demonstrated by removing `.next` from both applications and running the target to green — the state CI is always in and a developer never is.

**`flutter_secure_storage` is held at 10.x because version 11 needs `compileSdk = 37`.** The client compiles against 36 today, and Android Gradle Plugin 9.0.1 names 36 as its own maximum recommended — so taking 11 means moving the SDK and probably the Gradle plugin together. There is no urgency: 10.3.1 uses the same Keystore-wrapped ciphers and the same API 23 requirement. **Decide it with SHIP-24 and SHIP-26**, which are the tickets that touch the Android build configuration anyway.

**Biometric unlock is still open, and SHIP-48 is where `Docs/07` §9 said it would close.** It did not, and the reason is that the thing it would sit in front of does not exist yet: an optional local unlock is a gate on a sign-in screen, and the first sign-in screen is SHIP-55. Nothing in the session design moves either way — `Docs/07` §3 already fixes its position as a convenience over the stored token and never a substitute for it — so the cost of leaving it is another wave of nothing happening. `flutter_secure_storage` offers it as an option on the store this ticket built (`AndroidOptions.biometric`, and iOS access-control flags), which means adopting it later is a change to two constants rather than a change to the design. **Decide at SHIP-55.**

**~~§10's done block should probably be `merge=union`, and §3 probably should not.~~ Decided and done at SHIP-15e — see §3.** Both halves were kept: the list is `merge=union` and §3 is not. Since a git attribute applies to a whole file, the list moved to `Docs/11-done.txt`, one ticket per line — which the recommendation had not noticed matters, because a union resolves line by line and the old block put several tickets on one line.

**~~`scripts/verify-foundation.sh` is the sixth shared surface, and it has no include mechanism.~~ Decided and split at SHIP-15e — see §3.** It is a harness plus one file per milestone or domain in `scripts/verify/`, numbered in reserved ranges the way migrations are, and a track adds a file rather than editing one. The count was unchanged at 105 across the split, which is the evidence the move lost nothing. **That 105 is a historical figure, not today's** — wave 3 took it to 208; §3 carries the current count.

**~~`device_sessions` has no expiry column.~~ Decided and built at SHIP-39 — see §3.** An explicit `device_sessions.refresh_token_expires_at`, `NOT NULL` with no default, in migration `000103`. The window **slides** — rewritten on every rotation, 30 days — so inactivity ends a session and daily use never does. **A Redis TTL was rejected** (`Docs/10` §5: a control a cache flush undoes is not one), and so was deriving expiry from `last_seen_at + TTL`, because that is a *display* column which SHIP-46 writes from a device-list **read** — a derived lifetime would mean every future write silently extends a credential. **No absolute session cap, deliberately**: that is a policy control with a product consequence rather than a mechanism, and it is another column and another migration whenever it is wanted.

**The mobile bundle identifier has no owner and stops being changeable.** `apps/mobile` currently uses a provisional `au.com.shipper` for both the iOS bundle id and the Android application id. **Once X-2 and X-3 publish a build, neither can be changed** — a new identifier is a new app listing, with a new install base. Confirm it before SHIP-25 or SHIP-27, not after. The staging and production hostnames baked into the API client (`api.staging.shipper.com.au`, `api.shipper.com.au`) are provisional in the same way, though those are only configuration; `SHIPPER_API_BASE_URL` overrides them meanwhile.

**`Docs/07` §8 requires staging and production installable on one device, and the client cannot do that yet.** SHIP-18 selects the environment with `--dart-define`, which changes the base URL but not the identifier, so the second build replaces the first. Holding both at once needs a distinct application id per environment — real Xcode and Gradle flavours. **That work belongs with SHIP-24…27**, which are blocked on X-2 and X-3 anyway, but it is not currently in any of their *Done when* lines.

**~~`httpx.H` and `httpx.DecodeJSON` are specified in `Docs/10` §4.3 and do not exist.~~ Decided
and built at SHIP-15e — see §3.** Promoted rather than copied, before the jobs track became the
second domain to want them, and `internal/identity` uses them like everybody else. The choice
was between building the mechanism and amending `Docs/10` §4.3 to match reality, and building
won for the same reason it did with `RegisterCode` at SHIP-15c. The body limit — the part that
actually bites, since a second literal is a second place for it to drift — is now one constant
in `internal/httpx` rather than two that agree by comment.

**`X-Forwarded-For` is deliberately unread, and that is a gate on the first deployment behind a
load balancer.** SHIP-47's per-address bucket keys on `RemoteAddr`. Honouring a forwarded header
without a trusted-proxy configuration would let any caller choose their own bucket and evade the
limit entirely; not honouring one behind a proxy makes every request share one bucket, which
throttles everybody at thirty failures. Neither is acceptable in production and there is no
deployment yet to be wrong about. **Decide with the deployment work** — a trusted-proxy hop count
or a CIDR allow-list in `internal/config`, read by `identity.clientIP`.

**A per-account limit is a lockout somebody else can trigger, and the trade is deliberate.** The
bucket keys on the submitted address whether or not it has an account — it must, or never being
throttled would itself disclose that an address is unknown. So a caller can spend somebody else's
allowance by getting their password wrong for them. The per-address limit is what bounds it: an
attacker burns their own thirty to fill six accounts' buckets, and the sustained rate lets them
hold about one account at a time. That is the standard shape and it is worth revisiting rather than
inheriting: a per-account limit counting *distinct* addresses, or one a successful sign-in clears,
both remove it. **Decide at SHIP-183**, with the rest of the surface.

**The device list is bounded but not pageable, and the package it was waiting for now exists.**
`GET /v1/auth/sessions` returns the collection envelope with `next_cursor` always null and a
hundred-row cap, so `has_more` reports a truncation a caller cannot page past. When SHIP-46 wrote
it, `internal/pagination` was registered in `internal/boundaries` and unwritten, and writing the
shared package mid-wave was the alternative it declined. **SHIP-66 wrote the package later in the
same wave**, which retires the reason and leaves a smaller, sharper item: `GET /v1/auth/sessions`
should adopt `internal/pagination`. That is a small follow-up ticket, **not a defect** — the bound
is unreachable for a person, since a hundred *live* sessions means signing in a hundred times in
thirty days without ever refreshing.

**`scripts/check-spelling.sh` only sees tracked files.** It searches with `git grep`, so a newly created file passes the check until it is staged — which let one through during wave 1. Cheap to fix in the reader rather than the script: run `make lint-spelling` after `git add`, not before. Worth a line in `Docs/10` §9.3, which is where somebody would look.

## 10. The done list, in a form a script can read

**The list is `Docs/11-done.txt`**, one ticket per line. It is still authoritative and it is
still updated in the same change that finishes a ticket — it has simply moved out of this
document. `make status` reads it, counts it against the backlog, and cross-checks it against
what commit subjects claim.

A ticket belongs there only when its *Done when* line in `Docs/09` is demonstrable. **Of the three
tickets in §4, only SHIP-134 is absent** — SHIP-65 and SHIP-149 are both in the list *and* partly
done, and that is not a contradiction to be tidied away.

**A ticket can be both**, and this is the shape: it landed, it is named by a commit subject, and one
clause of its *Done when* belongs to a ticket that does not exist yet. SHIP-65 shipped the job
detail endpoint and the owner-only rule; the `budget` field its sentence also names is SHIP-67's,
together with the serialisation test proving it cannot leak — and adding the column before that
proof would be exactly the wrong order. SHIP-149 shipped the append-only `audit_log` and its
triggers; the Go write helper is still missing.

**Removing either from the list would make `make status` hard-fail**, not go quiet: a commit subject
names each (`a47ba3a` for SHIP-65), and the script exits 1 when git shows a ticket the list does not
declare. The list is the floor of what landed; §4 is where the nuance lives. Keep them in both.

**It moved so that it could be `merge=union`, which this document must never be** (SHIP-15e).
Every track in a wave appends to the list, so the last merge of a wave conflicted here every
time — twice in wave 2, both resolved as unions. A union is safe for a flat list of independent
tokens: it is always a superset, and `make status` **fails** when the list names a ticket git
has never seen, so a bad union is caught rather than passing quietly. It is not safe for §3,
which is prose, where a union interleaves two narratives and is harder to notice than a
conflict. A git attribute applies to a whole file, so the two could only be treated differently
by separating them. `.gitattributes` and the file's own header carry the full argument.

One ticket per line is part of it: `merge=union` resolves line by line, and the old block put
several tickets on one line, so two tracks appending to the same line would have conflicted
anyway.

A ` ```done ` block reappearing in this document is now a `make status` failure, because a
second list is a list nothing reads.

## 11. Keeping this file honest

Update it in the same change that finishes a ticket. A tracker maintained afterwards is a tracker that is wrong — and this repository has already demonstrated the failure mode: `CLAUDE.md` described the project as sitting at SHIP-9 for the entire time SHIP-10 to SHIP-15 was being written.

`make status` compares three things: the backlog, the list in §10, and the tickets named by commit subjects. It fails when §10 claims something git has never seen, and warns when git has seen something §10 does not mention. It reads subjects only, so a commit finishing two tickets while naming one under-reports — which is why §10 is authoritative and the git side is a check on it rather than the source.
