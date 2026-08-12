# Shipper — Delivery Status

**Status:** Living document — update it in the same change that finishes a ticket  
**Audience:** Anyone picking the work up, including a session with no prior context  
**Purpose:** Say where the work actually is, so nobody has to reconstruct it from git.

## Read this first

`Docs/09` says what the work *is*. This says what is *done*. They are separate files on purpose: the backlog is a plan that rarely changes, and this changes every few days.

`make status` prints the machine-checkable half — which tickets have a commit claiming them. It cannot see nuance, so **this file is authoritative** for anything a commit subject does not capture: partly finished tickets, external blockers, and what is safe to start next.

**Last updated:** 2026-08-12, at **SHIP-15i**, the wave-5 pre-step. It follows the wave-4 reconciliation pass in the same day: that pass corrected §1, §2, §6 and §7 after eleven tickets landed across four tracks, and this one adds the two mechanisms wave 5's tracks would otherwise collide in, closes SHIP-91 on the owner's ruling, and redoes the arithmetic both of those move.

---

## 1. Snapshot

| | Tickets | Points |
|---|---|---|
| **Done** | 83 | 232 |
| Remaining | 123 | 407 |
| **Total** | 206 | 639 |

The totals grew by two tickets rather than shrinking: SHIP-15c and SHIP-23a were added to `Docs/09` during wave 2, both work the plan assumed and no ticket owned.

They grew again by one in wave 3: **SHIP-15e** (M0, 5 points), the serial pre-step that split `scripts/verify-foundation.sh` into a harness plus one file per domain and moved the done list out of this file. Totals moved 203 → 204 tickets and 626 → 631 points.

And once more before wave 4: **SHIP-15g** (M0, 5 points), which took 205 tickets and 636 points. Same pattern a fourth time — a shared surface two different tracks had each parked work against, plus a defect neither could fix from a domain branch. **This is now the norm rather than the exception**, and the useful reading is that a wave costs one prep ticket: the pre-step is not overhead the process has failed to eliminate, it is the process. Budget for the next one rather than being surprised by it.

**Wave 5's is SHIP-15i** (M0, 3 points), taking the totals to 206 tickets and 639 points — and it is the first of the five to be *smaller* than the wave before it. Two mechanisms rather than four, because the surfaces wave 5's tracks would meet in are now known well enough to name precisely: the `make verify` count and §3's index, both of which conflicted or were forgotten in wave 4. `internal/money` was considered and deliberately left unwritten; the reasoning is in §3.

**The done total also moved for a reason that is not new work.** SHIP-91 was declared delivered by the owner on 12 August 2026, met by SHIP-80's partial unique index rather than built separately — see §3 and §6. So 83 done rather than 81, of which one ticket is SHIP-15i and one is a ruling.

| Milestone | Done | Points |
|---|---|---|
| **X** External | 0 / 9 | 0 / 26 |
| **M0** Foundation | 31 / 35 | 81 / 95 |
| **M1** Identity | 28 / 28 | 78 / 78 |
| **M2** Jobs | 15 / 26 | 47 / 78 |
| **M3** Bidding and award | 3 / 27 | 10 / 95 |
| **M4** Delivery | 2 / 29 | 5 / 101 |
| **M5** Notifications | 1 / 13 | 5 / 45 |
| **M6** Admin | 1 / 20 | 3 / 65 |
| **M7** Hardening | 2 / 19 | 3 / 56 |

**M0 has four tickets left and not one of them is code.** SHIP-24…27 are store signing and upload, blocked on X-2 and X-3. Every buildable M0 ticket is done, and **SHIP-15i** is the last one added — like SHIP-23a, SHIP-15e and SHIP-15g before it, it was a recommendation in §9 and §3 before it was a ticket. That is now four of the five lettered M0 tickets, which is worth reading as a mechanism rather than a coincidence: this file's §9 is where the next prep ticket is written, one paragraph at a time, by whoever hits the surface first.

**Wave 4 has landed in full.** Four tracks, eleven tickets, thirty-nine points, **no trim taken**, and **M1 closed**. Three domains opened that had held nothing but `doc.go` — `fleet`, `bidding` and `delivery` — and three separate guards fired for the first time and all three worked. §7 has the detail.

**M1 is complete — 28 of 28 tickets, 78 of 78 points.** A person can register, receive an email token and a phone OTP, confirm both, sign in, and stay signed in across app restarts on a real handset, with the role fixed at registration and immutable afterwards by a database trigger rather than by application logic. The platform half was finished in wave 3 — sign-in, sign-out, refresh-token rotation with reuse detection that invalidates the whole device session, an explicit sliding expiry on `device_sessions`, the device list and its revoke, and rate limiting that charges failed sign-ins only and fails **closed** when Redis is down. Wave 4 closed the two that were left, both Flutter: SHIP-50's refresh interceptor, which refreshes once however many requests are refused at the same instant, and SHIP-55's sign-in screen, which deleted the development session stand-in the client had been carrying since SHIP-49.

**M1's exit criterion is now met end to end rather than server-side.** "A person can register, verify, choose a role, and stay signed in across app restarts" was demonstrated on an iPhone 17 simulator through the real Keychain: registered from the form, signed in from the form, landing in the provider half with the role having travelled out of a token the platform signed, and surviving a relaunch on a rotated token. All of the platform half is demonstrated by `make verify`, not asserted.

**The jobs table constrains all twelve statuses, and status is not a settable field.** A `BEFORE UPDATE` trigger refuses any change not described by a `job_status_history` row written in the same transaction, so the guard, the record and the transaction are one condition. `cmd/worker` claims due work under `FOR UPDATE SKIP LOCKED` and survives being run twice.

**The published contract exists (SHIP-17a), and it is checked rather than believed.** `contracts/openapi.yaml` is assembled from per-domain fragments under `contracts/paths/`, and three tests in `cmd/api` hold it to the service: the manifest and the contract must agree in both directions, live handler responses must satisfy the published schemas, and the error contract must match the `Error` schema for failures that `net/http` writes rather than a handler. That closes `TestEveryRouteIsInTheContract`, the last of the three route-surface guards in `Docs/10` §4.1 to become enforceable.

**Domain logic now lives in five packages, not two.** `internal/identity` holds argon2id password storage, access-token issue, refresh rotation and the session surface; `internal/jobs` holds the location value object, the budget, expiry, the store, the ports and the handlers for create, amend, cancel, detail and list; `internal/fleet` holds `vehicles` and the six routes under `/v1/fleet/vehicles`; `internal/bidding` holds the bid statuses behind `bids` and its one-accepted-bid index; `internal/delivery` holds the milestone vocabulary behind `milestones` and `driver_assignments`. Only `profiles`, `notifications` and `admin` still contain `doc.go` and nothing else.

That distinction is worth keeping in mind rather than rounding away: five domains now hold code the others will want to call, and the rule that stops them calling it directly — one domain never imports another — has had several real opportunities to be broken and was not. `delivery` declares its own milestone constants rather than importing `jobs.Statuses`, and SHIP-134's publisher was kept out of `internal/events` for the same reason. Wave 3 was the first evidence for the pre-seeded infrastructure list: `internal/ratelimit` (SHIP-47) and `internal/pagination` (SHIP-66) were both written **with no shared-file edit at all**, which is exactly what registering them in `internal/boundaries` ahead of the code was for. Only `money` remains unwritten, and SHIP-67 declined to write it while a second track was building `bids` in the same wave.

**All four deployables now exist and run, and the app drives the product rather than only the sign-up.** The Flutter client builds and runs on both simulators, the two Next.js surfaces build and serve, the Go service serves `/health` and `/v1`, and `cmd/worker` runs the scheduled work beside it — job expiry and the outbox drain, both against the real stack. Wave 3's SHIP-51…54 took the client past the foundation into registration and verification; wave 4 took it into the marketplace itself — the customer creates a job and reads their own job list from the device, over the same authenticated session the refresh interceptor keeps alive. The two Next.js surfaces are still foundation only.

## 2. Branch state

| Branch | At | Holds |
|---|---|---|
| `main` | PR #19 | **Wave 1, released 11 August 2026.** Now well behind `develop` |
| `develop` | wave 4 merged | Everything below. **Cut new branches from here** |

**`develop` is 125 commits ahead of `main` and holds four waves.** Wave 1 was released as PR #19; everything since — SHIP-17a, the wave-2 pre-step and its three tracks, the wave-3 pre-step (SHIP-15e) and its three lanes, and the wave-4 pre-step (SHIP-15g) and its four tracks — is on `develop` only. The next `develop → main` pull request is the second release, and it is now several times the size of the first.

That figure is `git rev-list --count main..develop`, and it is worth naming the command because the other two readings differ sharply: `--first-parent` gives 26 (one per merged branch, which is the useful review unit) and `--no-merges` gives 90. All three were re-read on this tree rather than adjusted from the last pass.

**Run the revert check before cutting it.** `main`'s history contains a revert, which is the shape where a merge silently resurrects deletions, and the two commands for establishing that it is safe are below. This is not hypothetical here: PR #19 had exactly that shape.

### The wave-4 branches, in merge order

All six were merged locally with `--no-ff`, none through a pull request. The first is the wave-3
reconciliation pass, which landed inside wave 4's window rather than wave 3's — a reconciliation
branch is not a backlog ticket and appears in neither `Docs/09` nor `Docs/11-done.txt`, which is
why it names no ticket in its subject.

| Merge commit | Branch | Brought |
|---|---|---|
| `f958aa6` | `ship-15f-wave-3-reconciliation` | The wave-3 reconciliation pass — no ticket |
| `09071d4` | `ship-15g-wave-4-prep` | SHIP-15g — configuration, the migration guard and `Task.Close`, before the tracks opened |
| `af97e92` | `ship-67-68-budget-and-expiry` | SHIP-67, 68 |
| `c0c982e` | `ship-78-80-fleet-and-bids` | SHIP-78, 80 |
| `6d15d06` | `ship-105-134-delivery-and-outbox` | SHIP-105, 110, 134 |
| `a736d74` | `ship-50-76-mobile-session-and-jobs` | SHIP-50, 55, 71, 76 |

**Four tracks and nothing collided in code.** What needed resolving was this file — the `make
verify` check count for the fourth consecutive merge, and §3 summary-table rows three of the four
tracks had not added — plus one collision that was semantic rather than textual: SHIP-68's verify
section runs the real worker, which drains the outbox, which broke an assertion in SHIP-134's
section the moment the two met. §7 has all of it, and both the check count and the one-binary
worker are now open recommendations in §9 rather than recurring surprises.

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

The last two were merged locally rather than through a pull request, which is why they have no number. **Both conflicted in this file and nowhere else**, and both resolutions were unions — see §7b.

An earlier version of this section chased the exact pull-request number and commit count, and was wrong within a day both times — a commit cannot record the number of the pull request that merges it. **This table is always slightly behind reality, and the fix is to correct it in the next update rather than to try to make it self-aware.** It says "wave 4 merged" rather than a number, for that reason.

The commit count above is the same kind of figure and gets the same treatment: **recount it at each reconciliation, never carry it forward.** It has now been carried forward wrongly once — a handover brief recorded 81 when the true count at the wave-3 reconciliation was 98 — which is the cost of copying a number that a single merge invalidates. The six merges in the wave-4 table above took it from 98 to 125, which is the size of the correction one wave makes.

`main` still shows commits `develop` does not have. Those are the detour, not divergent work: wave 0 reached `main` by being merged (PR #6), reverted (PR #7), and reapplied (PR #9), and PR #19's own merge commit sits on `main` alone. The content is identical; only the shape of the history differs.

### The wave-1 branches, in merge order

| PR | Branch | Brought |
|---|---|---|
| #11 | `ship-15b-wave-1-prep` | SHIP-15b, and SHIP-37's dependency amendment |
| #12 | `ship-15a-close-mobile-decisions` | `Docs/07` §9 closed, `Docs/10` §8.3 reconciled |
| #13 | `ship-22-35-web-and-adapters` | SHIP-22, 23, 32, 35, 59a |
| #14 | `ship-29-38-credentials-and-sessions` | SHIP-29, 37, 38, and the SHIP-149 verify fix |

`ship-16-21-flutter-foundation` exists and is parked at PR #11's merge, holding nothing. It is the branch the Flutter track resumed on — see §7c.

**A warning worth keeping.** Reverting a merge does not undo it: the commits stay ancestors forever, so re-merging the same branch brings nothing across and reports success. If a merge to `main` is ever reverted again, the fix is to revert *the revert*, not to merge again.

**PR #19 had exactly the shape that warning describes, and was checked rather than trusted.** The revert `d733c95` sits on `main` and is *not* in `develop`'s ancestry — the setup where a merge silently resurrects deletions. It was safe only because the reapply `c1cb64b` had already restored the content, leaving the revert nothing to take away. The check that established this before merging is worth reusing on any release whose history has a revert in it:

```
git merge-tree --write-tree main develop   # the tree the merge would produce
git rev-parse develop^{tree}               # the tree develop actually has
```

Identical hashes mean the merge result is exactly `develop`'s content. Different hashes mean something is being dropped or added, and the release needs looking at before it is cut, not after.

## 3. Done

Verified by `make verify` — **297 checks across 11 sections**, and `make check` green. Since
SHIP-15e the checks live one file per milestone or domain in `scripts/verify/`, sourced by the
runner; a ticket adds its section by adding a file. Wave 4 added two: SHIP-78's
`scripts/verify/60-fleet.sh` and SHIP-134's `scripts/verify/80-notifications.sh`. SHIP-67 and
SHIP-68 added theirs to the jobs file.

**That figure is now checked by the run itself, and nobody types it (SHIP-15i).** A successful
`make verify` reads the sentence above, compares it to what it just counted, and **fails with the
measured value printed** when the two disagree; `make verify-update` rewrites it. It had conflicted
in four consecutive merges and on three of them *no figure in the conflict was correct*, which is
what a hand-maintained scalar in prose does — it is the one shape a merge cannot resolve. **The
number is still measured and never reconciled**; what changed is that a wrong one no longer
survives a green run. Run `make verify` twice: the second run is the one where `shipper.job`
already exists and SHIP-68's worker publishes into it, which is the condition that broke
SHIP-134's section at the wave-4 merge (see below).

**The guard finds the sentence by its bolding, so keep historical counts unbolded.** "went from
208 checks across 9 sections" further down this file is deliberately plain text; the run refuses
to proceed if it finds the bold form more than once, because a reader could not then tell which
was current.

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

### M0 — Foundation (31 of 35)

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
| **SHIP-15g** | Wave-4 shared surfaces: `GEOCODING_*` and `PAGINATION_*` configuration, the out-of-order migration guard, and `Task.Close` — *see below* |
| **SHIP-15i** | Wave-5 shared surfaces: the `make verify` count checked against this file, a §3 row required of every done ticket, and the cause of an unmapped 500 logged — *see below* |
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
| **SHIP-47** | M1 | Sign-in rate limiting — a Redis token bucket that charges failures only and fails **closed**; the first client of `internal/ratelimit` — *see below* |
| **SHIP-48** | M1 | Flutter secure storage — the refresh token in the Keychain and the Keystore, and nowhere a swap would be possible — *see below* |
| **SHIP-49** | M1 | Flutter session and routing guard — three states, and a cold start that never guesses — *see below* |
| **SHIP-50** | M1 | Flutter token refresh — one refresh per 401 however many requests hit it at once, and the replay carries the original idempotency key — *see below* |
| **SHIP-51** | M1 | Flutter registration screen — the first client screen to call a product endpoint, and one idempotency key per action — *see below* |
| **SHIP-52** | M1 | Flutter role selection — chosen first because the platform fixes it, and the session's role selects the shell — *see below* |
| **SHIP-53** | M1 | Flutter email verification — typed or deep-linked, and the app's first deep link scheme — *see below* |
| **SHIP-54** | M1 | Flutter phone verification — a code on opening, and a resend the platform's interval throttles — *see below* |
| **SHIP-55** | M1 | Flutter sign-in — closes M1, deletes the development session stand-in, and settles biometric unlock as out of the MVP — *see below* |
| **SHIP-56** | M2 | `jobs` — the twelve statuses of `Docs/02` §1 as a `CHECK`, held to the Go constants by test |
| **SHIP-57** | M2 | The transition guard — and the database refuses a status change that did not come through it — *see below* |
| **SHIP-57a** | M2 | `job_status_history` — actor, reason and both clocks, append-only |
| **SHIP-59a** | M2 | Geocoding adapter — deterministic stub, and not-found is an outcome, not an error |
| **SHIP-60** | M2 | The address value object — validated, normalised, and resolved where the platform can; a failed lookup never fails the job — *see below* |
| **SHIP-61** | M2 | `POST /v1/jobs` — the first authenticated state-changing endpoint in the service — *see below* |
| **SHIP-62** | M2 | `PATCH /v1/jobs/{id}` — a partial edit of a draft, and a stranger's edit is indistinguishable from no job at all — *see below* |
| **SHIP-64** | M2 | `POST /v1/jobs/{id}/cancel` — the first endpoint that moves a job, and so the first real client of the status guard — *see below* |
| **SHIP-65** | M2 | `GET /v1/jobs/{id}` — the job in full to its owner, and byte-identical 404s to everybody else — *see below* |
| **SHIP-66** | M2 | `GET /v1/jobs` — the caller's own jobs, keyset-paged; the first client of `internal/pagination` — *see below* |
| **SHIP-67** | M2 | `jobs.budget` — minor units, and three tests rather than one proving it cannot reach a provider — *see below* |
| **SHIP-67a** | M2 | `cmd/worker` — a ticker and a `FOR UPDATE SKIP LOCKED` claim loop; two workers share the backlog rather than duplicating it — *see below* |
| **SHIP-68** | M2 | Job expiry — the deadline is a trigger's, the sweep is the worker's, and `make verify` runs the real binary — *see below* |
| **SHIP-71** | M2 | Flutter locations step — the platform validates and normalises, and an unrecognised address is an outcome the customer walks past, not an error — *see below* |
| **SHIP-76** | M2 | Flutter customer job list — read once and grouped client-side, and a test keeps the budget out of every widget a provider could reach — *see below* |
| **SHIP-78** | M3 | `vehicles` and the six routes under `/v1/fleet/vehicles` — deactivated, never deleted, and one live plate per provider held by a partial unique index — *see below* |
| **SHIP-79** | M3 | `provider_service_areas` and `provider_specialties`, and `GET`/`PATCH /v1/fleet/profile` — a service area is a **set of named regions, not a radius**, so §9's `internal/geo` trigger does not fire at SHIP-81 — *see below* |
| **SHIP-80** | M3 | `bids` — the eight statuses of `Docs/02` §4, and the index that makes a second accepted bid impossible. No endpoint: **demonstrated by its own tests** — *see below* |
| **SHIP-81** | M3 | The job eligibility filter — **one SQL predicate in `fleet`, reaching four tables across three domains**, chosen over ports because ports cannot page a set intersection. Two readers, one clause; no endpoint until SHIP-82 — *see below* |
| **SHIP-82** | M3 | `GET /v1/jobs/open` — the provider's feed, keyset-paged. **Declared in `routes_fleet.go`, not `routes_jobs.go`**: routes follow the domain that answers them, not the first segment of the path. It also deletes SHIP-81's SQL mirror from `make verify` in favour of real HTTP checks — *see below* |
| **SHIP-83** | M3 | `GET /v1/jobs/open/{id}` — one job as a provider sees it, and **the fourth budget proof §8 recorded as still owed**: the serialised response, obtained over HTTP, held to a *closed set of keys* so that a budget renamed `max_price` fails too. The street line and the coordinate are confirmed withheld — *see below* |
| **SHIP-91** | M3 | The one-accepted-bid constraint — **met by SHIP-80 rather than built separately**, and declared done by the owner rather than claimed by a commit — *see below* |
| **SHIP-105** | M4 | `driver_assignments` — the driver has no account, so no foreign key to `users`; one live assignment per job by partial unique index. No endpoint: **demonstrated by its own tests** — *see below* |
| **SHIP-110** | M4 | `milestones` — the actor's clock and the server's kept apart by a trigger that refuses an insert naming the server's. No endpoint: **demonstrated by its own tests** — *see below* |
| **SHIP-134** | M5 | The transactional outbox publisher — a Kafka producer in `cmd/worker`, and the aggregate is the unit of division — *see below* |
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

Shared packages now available to every domain: `db` (Runner, InTx), `authctx`, `clock`, `validate`, `events` (outbox writer), and — since SHIP-47 and SHIP-66 — `ratelimit` and `pagination`. **`money` is the one name still registered and not written**, and SHIP-15i left it that way on purpose: its only consumer is unreachable behind X-4, and the whole value of the pre-seeded list is that whoever needs a package first writes it with no shared edit. See SHIP-15i below before re-opening it. **`ratelimit` was the mechanism's first vindication**: SHIP-47 wrote the package, its first client used it, and `internal/boundaries` was never opened; `pagination` repeated it in the same wave, in a different lane.

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

**~~Its *Done when* says "including budget", and the budget column does not exist.~~ Closed at
SHIP-67 — see below.** It was deliberately not added at SHIP-65: `Docs/11` §8 makes SHIP-67
single-owner with SHIP-83 precisely so the column and the serialisation test proving it cannot
reach a provider land together, and SHIP-67 was not in wave 3. A field that arrives before its
proof is the one arrangement worse than a field that arrives late — so the endpoint was complete
and the sentence was not. **Recorded rather than resolved silently**, and §4 carried SHIP-65 until
SHIP-67 closed it. The `make verify` check asserting the *absence* of a `budget` key was the
tripwire, and SHIP-67 moved it loudly rather than quietly.

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

### What SHIP-50 built, and the two places it put things nobody expected

**A `401` now triggers one refresh and one replay, and several at once still produce one
refresh.** The second clause is the ticket. Three requests going out with the same token and all
being refused is the ordinary shape of a phone coming back into signal — and a client that
refreshed per request would present an already-rotated token, which SHIP-40 answers by revoking
the **whole device session**. The client would be doing to itself exactly what that mechanism
exists to catch somebody else doing. One in-flight future serialises them;
`auth_interceptor_test.dart` holds the refresher open so all three failures land while the
refresh is genuinely in flight, which is a different code path from three landing after one has
finished.

**The other half of "refresh once" is not a lock, and it is the half a lock does not cover.** A
`401` that arrives *after* somebody else's refresh completed has nothing to queue behind. So the
interceptor passes the session the token the failed request actually carried, and the session
answers with the current one when they differ rather than refreshing again.

**The replay reuses the original request's `Idempotency-Key`, and that is correct by construction
rather than by remembering.** It re-sends the same `RequestOptions` object, so the header is
already in it. A fresh key would turn one bid into two — `Docs/07` §4 has the key generated where
the user acts and reused across every retry, and a refresh-and-replay is a retry by any reading:
the platform saw the attempt, refused it for a credential reason, and is about to see it again.
`ActionKey` needed no change; wave 3 had already written the rule down.

**A refresh is sent through a second transport carrying no interceptor at all.** The credential is
body-borne, so a bearer token on it would be the expired token sent to the endpoint that replaces
it — and it would put the refresh inside the interceptor whose answer to a failure is to refresh.
SHIP-42 closed that loop from the platform's side by answering `400` rather than `401`; this
closes it from the client's, so neither side is the only thing holding it.

**Signing out on a failed refresh is decided by `ActionKey.outcomeUnknown`, not by a status
code.** `identity_refresh_token_invalid` means the platform saw the token and will not honour it:
the session is over. A dropped connection, a `503` or a proxy's error page say nothing about
whether the session is alive, and signing out on those would end a perfectly good session because
the signal dropped — a driver in a tunnel. Reusing the predicate that already decides whether to
keep the idempotency key means the retry rule and the sign-out rule cannot drift apart.

**Two things landed where SHIP-49 said they would not.**

- **The access token is a private field on `SessionController`, not a field on `SessionState`.**
  `session_state.dart` had anticipated the opposite. `freezed` writes a `toString` over every
  field, so a token in the state is a token in any log line or crash report that prints the
  session — the thing `Docs/07` §3 forbids in the same sentence as preferences and documents, and
  the reason the *refresh* token was kept out of the state in the first place. It is also not view
  state: no widget renders it, so a field would rebuild every listener on each refresh. The role,
  which is view state, stays in the state. The document was corrected rather than left.
- **`POST /v1/auth/refresh` is in `core/auth`, not on `IdentityRepository`.** The wave brief
  expected the repository. Refreshing is not a screen's action — nothing renders its result and
  the only caller is the session — and putting it on the identity feature would mean `core/auth`
  importing `features/identity`. That is the mobile shape of the edge `CLAUDE.md` calls the one
  easiest to break by accident: every feature already imports `core/`, so one import the other way
  welds all seven to identity. `core/api/auth_interceptor.dart` declares the two-member interface
  it needs from the session and `apiClientProvider` supplies the closure, which is
  `internal/httpx`'s rule written in Dart. Sign-in stayed on `IdentityRepository`, because signing
  in *is* a screen's action.

**The first refresh happens as soon as the keychain answers, and the shell is published before
it.** Without that the role would arrive "whenever something happens to make a request", which in
M1 is nothing at all — and SHIP-52's role-pending shell would be what every cold start showed for
ever. Holding the splash for the round trip was the alternative and is worse: a cold start would
be as slow as the signal, and offline as slow as the connect timeout. The visible consequence is
that "signed in and not yet knowing as whom" is now the window before the refresh answers, plus
the cold start that cannot reach the platform — `role_selection_screen_test.dart` reaches it by
stubbing the refresher unreachable, which is the honest way to reach it.

**`TokenPair` is hand-written rather than `freezed` + `json_serializable`, for the `toString`
reason above.** It also reads only two of the contract's four fields: nothing schedules on
`expires_in`, because this client refreshes when the platform says a token is not accepted and
never on a clock, and `Docs/07` §4 is built on handsets whose clock is wrong after a spell out of
signal.

**`roleFromAccessToken` does not verify and must never start.** The signing key is the platform's
(`internal/identity/token.go`), and a build carrying a verification key would not make the check
mean anything. A device that edits its own token gets a different *shell* and exactly the same
refusals. Three answers, and the difference between two of them matters: a known role, `unknown`
for a role this build has not heard of (which the shell renders as "update the app"), and `null`
for a token that said nothing readable — collapsing the last two would put an update prompt in
front of somebody whose token had simply not arrived yet.

**How it was demonstrated.** `make flutter-check` in the wave-4 worktree: 198 host tests, up from
166. The interceptor tests run through the real `apiClientProvider` transport, the real
`SessionController` and the real single-flight rule, with a stub only at the socket and the
Keychain — an interceptor tested against a hand-made session would pass with the session wired to
nothing, and "concurrent calls refresh once" is a claim about the two of them together.

### What SHIP-55 built, and the decision it was told to settle

**The signed-out shell and the sign-in screen are one screen, not two.** SHIP-49 built the first as
a placeholder holding a disabled button, because `POST /v1/auth/login` did not exist. It does
(SHIP-41), and a landing screen whose only purpose is a button leading to a form is a tap somebody
makes every time they are signed out. `/sign-in` is unchanged, so every redirect, guard entry and
deep link still resolves to it, and `shell-signed-out` is still the key that says the app is
showing the signed-out surface.

**Nothing on the screen navigates on success, deliberately.** The session changing is what moves
the app, through the router's guard — and the role that decides *which* shell comes from the
access token the platform just signed. A screen that also pushed a route would be a second
mechanism deciding where a signed-in user goes, and the two would disagree the first time somebody
signed in from anywhere else.

**Sign-in validates the password for presence only, and that is a real difference from
registration.** `Validators.password` carries the ten-character minimum somebody must *choose*.
Applying it to a password being *presented* would refuse an account whose password predates the
current floor — locally, so the person could not even reach the platform that would have accepted
them. The contract states the same rule from its side.

**`identity_credentials_invalid` goes in the banner and never under the password field.** One code
for a wrong password and for an address with no account is what stops an unauthenticated endpoint
being an account-existence oracle; a client that put the message under one field would undo that
by saying which one was recognised.

**Retrying after a dropped connection reuses the key, and here that is not a nicety.** Each
sign-in creates a device session, because there is no device identifier to match on — so a
retried sign-in is exactly how somebody ends up with a device list they cannot make sense of and a
second session live for thirty days. `ActionKey` retains the key when the outcome is unknown, so
the platform replays its stored pair. A wrong password retires it, because the platform saw that
attempt and a replayed refusal is not what the person asked for.

**`device_label` is derived, not asked for.** The contract observes that a sign-in screen is the
only moment a label could be collected, which reads as an argument for a third field; it is not
taken, because a person signing in on their own phone should not be asked to name it and whatever
they typed would be worse for the one job the field has. It is `iOS 17.0` / `Android 14`, parsed
loosely from `Platform.operatingSystemVersion` — a model name needs `device_info_plus`, which is a
package decision with two native integrations and a store-privacy consequence, and §9 carries it.

**It lives in `core/device/` rather than `core/auth/`, and a test made that choice.**
`token_store_is_not_preferences_test.dart` forbids `dart:io` anywhere under `core/auth/` — the
application documents directory is the third location `Docs/07` §3 rules out — and its own note
says the rule will acquire no exceptions. Reading `Platform` is enough to trip it. The rule was
honoured rather than waived, which is what a rule written that way is for.

**Two honest stand-ins were deleted, which is the other half of the ticket.**
`DevelopmentSessionButton`, its placeholder token and the `kDebugMode` preview button at the end of
signup are gone. SHIP-52's *Done when* — "role is chosen during signup and drives the post-login
shell" — is now demonstrable as one flow rather than as two separate facts with a debug button
between them: choose provider, register, verify both channels, sign in, land in the provider half.

**The end of signup carries the address forward and not the password.** It could have kept the
password from the form in memory and signed in automatically; a plaintext password in the provider
tree is one crash report away from somewhere it must never be, and `Docs/07` §3 draws that line for
tokens, which are the lesser secret. One field to type instead of two.

#### Biometric unlock: **out of the MVP**, and here is what would change the answer

`Docs/07` §9 said this closes "at SHIP-48 onwards" and `Docs/11` §9 moved it to SHIP-55 on the
grounds that an optional local unlock is a gate on a sign-in screen and there was no sign-in
screen. There is one now, so the excuse has expired and the decision is taken: **not in the MVP.**

The reasoning, in the order it actually weighed:

- **It is a convenience over the stored token and never a substitute for it** (`Docs/07` §3, which
  fixed its position long before this). The token is already behind `first_unlock_this_device` on
  iOS and a Keystore-wrapped key on Android — the device passcode gates it. What biometric unlock
  adds is a second gate in front of an app on an **already unlocked** handset. That is a real gain
  and a modest one, and it is not what a marketplace pilot holding no payment details is exposed
  on.
- **The cost is asymmetric across the two platforms, which was measured rather than assumed.**
  iOS is nearly free: `IOSOptions.accessControlFlags` takes `biometryAny`, `biometryCurrentSet` or
  `userPresence` on the store SHIP-48 already built. **Android is not: `AndroidOptions.biometric()`
  requires API 28**, and this app's floor is API 24 — chosen in `Docs/07` §9 precisely because a
  token store that is only *sometimes* hardware-backed is not the guarantee the document makes.
  Enabling it would mean either raising the floor by four API levels or shipping two sets of
  storage options, and "only sometimes biometric" is the same shape of half-guarantee that
  argument rejected.
- **An optional control needs somewhere to turn it off, and there is no settings surface.** Not in
  the app and not in `Docs/09` before SHIP-173. Shipping it as non-optional contradicts §3's own
  wording.
- **`biometryCurrentSet` invalidates the entry when the enrolled biometrics change**, and
  `resetOnError` is `true`, so adding a fingerprint would quietly sign somebody out. That is a
  support event caused by a convenience feature.
- **Deferring costs nothing structurally.** Nothing in the session design moves either way, and
  adopting it later is a change to two constants in the one folder allowed to hold the token —
  which is exactly the property SHIP-48 built for.

**What would change the answer**, named so this is a decision and not a shrug:

1. **The Android floor rising to API 28 for another reason.** SHIP-24 and SHIP-26 touch the Android
   build configuration and already carry the `flutter_secure_storage` 11 / `compileSdk` question
   (§9). If the floor moves there, the Android half of this becomes free and it should be
   reconsidered in the same change.
2. **A settings surface existing.** SHIP-173's in-app account screen is the first, and it is where
   an opt-in would live.
3. **The pilot holding something that makes an unlocked handset a real exposure** — payment details
   (the MVP holds none, `CLAUDE.md`), or a customer address history on a shared device.

Until one of those, this stays out. ~~**`Docs/07` §9's table row and its closing paragraph still
describe it as open and pointing at SHIP-48**; this branch owns `apps/mobile/**` and `Docs/11`
only, so correcting those two lines belongs to whoever reconciles the documents next.~~
**Both were corrected in the wave-4 reconciliation**, and §9's table now reads "three closed, one
open". `Docs/07` §3's position row was deliberately left alone: it says where an optional local
unlock sits relative to the stored token, which the decision does not change.

**How it was demonstrated.** `make flutter-check` in the wave-4 worktree: 219 host tests, up from
198 — the sign-in screen driven through the real router, guard, session and shell, with the socket
and the Keychain the only substitutions. Then on an iPhone 17 simulator against this worktree's API
on 8092, through `apps/mobile/integration_test/sign_in_test.dart`: an account registered by `curl`,
signed in **from the form**, landing in the provider half — with the role having travelled out of a
token the platform signed and through nothing that was told what it was. Its second test relaunches
over the same real Keychain, which is where SHIP-50's first refresh is demonstrated for real: the
service log shows `POST /v1/auth/refresh 200` and the stored token is not the one that was
presented, so the rotation happened.

**One thing worth knowing for anybody running the API in a worktree.** Sign-in answered `500` until
the local `shipper_b` database was rebuilt. `make migrate-up` had reported "no change" against a
schema whose `device_sessions` was missing `refresh_token_expires_at` and `revoked_at`: the
recorded version was `404`, from the jobs block, so the identity block's `000103` and `000104` sat
*below* it and were never applied. That is the condition SHIP-15g's note already describes — a
database bitten before the guard existed is backfilled as healthy — and the fix is the one it
gives: `make migrate-down n=all && make migrate-up`. No repository change; the guard is already
there for new databases.

### SHIP-67 — the budget, and the pairing that could not be honoured

`jobs.budget` is `numeric(12,2)` (000405), held in Go as `int64` cents and published as
`budget_cents`. Minor units rather than a decimal, on Docs/10 §3.3's rule that money is never a
float — a JSON number written `1500.50` is one, and the name carries the unit because a field
called `budget` holding `150000` is a field somebody eventually reads as dollars. The conversion
is two SQL expressions, one per direction, so nothing between the column and the wire rounds.
**`internal/money` was deliberately not written**: it is registered for SHIP-74, wave 4's Track A
is building `bids` in the same wave, and a shared package written by two tracks at once is the
conflict the pre-seeded list exists to avoid.

**The SHIP-83 pairing was broken deliberately, and this is the record §8 asked for.** §8 makes
SHIP-67 single-owner with SHIP-83 so that budget privacy is proved against a *serialised provider
response* rather than against struct fields, and it also records — as a verified fact rather than
a scheduling preference — that SHIP-83 is three dependency hops away. **Decided: build SHIP-67
now, and reserve SHIP-83 to the same owner when it becomes startable.** Deferring both would have
left the column unbuilt for a rule it already satisfies, and SHIP-65's *Done when* incomplete for
a third wave, in exchange for a test on an endpoint that does not exist.

What replaces the pairing is three tests rather than one, and the first is stronger than what the
pairing would have bought:

- **`TestOnlyTheOwnersResponseCarriesTheBudget` parses the package's own source** and fails when
  any struct outside a four-name allow-list declares a budget field, by Go name or by json tag.
  Reflection could only check types somebody remembered to point it at, which is precisely the act
  a future provider response would omit; the source is the complete list by construction. The
  allow-list is checked in both directions, so an entry that has stopped constraining anything
  fails too.
- **`TestNoRefusalLeaksTheBudget`** drives the real handlers and searches every response a
  provider or a second customer can obtain — reads, lists, edits, cancellations, malformed ids.
  Error messages are the one part of a response nobody writes a schema for.
- **`TestTheStatusChangedEventCarriesNoBudget`** reads the stored `outbox` payload. An event
  travels past the last endpoint that could redact anything, to Kafka and every consumer behind it.

**When SHIP-83 lands it adds the fourth**, and that is the test the pairing was actually for.

**The `make verify` tripwire was moved loudly.** `50-jobs.sh` asserted that no `budget` key was
present in a job response, and that assertion existed so that whoever added the column had to come
to the file and say so. The replacement is stronger in both directions: the SHIP-65 section still
refuses a budget on a job created without one — the omitempty rule that lets a client tell "no
budget" from "a budget of nothing" — and a new SHIP-67 section asserts the owner reads their own
back, that the column holds `1500.00` for `150000` cents, and that no response to a provider or to
another customer mentions the word or the amount anywhere.

### SHIP-68 — the deadline is the database's, the sweep is the worker's

Docs/02 §6.3 — the earlier of fourteen days after publication or the pickup date passing — is now
three pieces in three places, and the split is the ticket's main decision.

**000406 sets `expires_at` in a trigger on the transition into Open, not in Go.** The alternative
was to compute it inside `Service.Transition`, and it was rejected for the reason 000402 gives for
the status guard itself: the property wanted is that *every* published job has a deadline, and
publication is not one code path. SHIP-63 publishes, SHIP-93 returns an Awarded job to Open after
a provider cancellation, an administrator may reopen one — three tickets on three branches, and a
deadline computed in one Go function is a deadline three of them can forget. The symptom would be
an Open job that never expires, which nothing reports. `updated_at` is the precedent: a derived
timestamp belonging to a state change is set by the trigger attached to that change.

**It is a default, not a lock.** The trigger fills `expires_at` only when it is `NULL`, so a
caller writing its own value in the same statement keeps it, and a job that already has a deadline
keeps that. Two consequences fall out and both are what Docs/02 §6.3 asks for: SHIP-70's extend
endpoint is an ordinary `UPDATE`, and the clock does not restart every time a job cycles
`Negotiating → Open` as bids expire — which would let a job with a slow trickle of bids live for
ever, the exact stale listing §6.3 is about.

**`cmd/worker` needed no seam and no shared edit**, exactly as SHIP-15g predicted: `tasks_jobs.go`
is a new file with an `init` that calls `register`, and nothing else in the tree changed. No field
was added to `Deps`. The claim query lives in `internal/jobs` because *which* jobs are due names
this domain's table, status and column; the loop lives in `cmd/worker` because *how* work is
claimed is the worker's, and `ClaimIDs` refuses a query without `FOR UPDATE SKIP LOCKED`.

**The claim judges against the worker's clock rather than `now()`**, which is Docs/10 §6.3 being
useful rather than ceremonial: a test advances a `clock.Fixed` by fifteen days and watches the
backstop fire, instead of waiting or writing a deadline into the past to fake one. Two concurrent
passes are tested against a real database and each job is expired exactly once — no lease table,
no leader election, just `SKIP LOCKED` and one transaction per pass.

**`expires_at` is returned to the owner.** SHIP-69 warns forty-eight hours ahead and SHIP-70
extends, and neither is usable by a client that cannot see the deadline; it is omitted while the
job is a Draft, because the clock starts at publication.

**`make verify` runs the real worker binary**, not a stand-in: it publishes one job whose pickup
window closed an hour ago and one with no window at all, checks the two deadlines the trigger
computed, starts `cmd/worker`, waits for the first pass, and then asserts the stale job is
`Cancelled` with an `Open->Cancelled` history row attributed to `system` with no account, an
outbox event, and the live job untouched. The count went from 208 to **224**.

### SHIP-71 — the locations step, and the validation it deliberately does not do

`/jobs/new` is the first screen to hang off the signed-in shell. It takes the two addresses in
their four parts each, saves them, and shows back what the platform made of them.

**Nothing on the screen validates an address, and that is the decision rather than an omission.**
No postcode pattern, no length, no picker holding the eight states. `validators.dart` already
describes the one duplication this client knowingly carries — the password minimum — and the
argument that makes it safe is that it can only fail in the harmless direction. None of these
fields has that property, and one of them is worse than merely unsafe: a client-side "all four
parts are required" rule would refuse the empty address `Docs/01` §4.1 explicitly allows, because
a draft may be saved half-finished and returned to. So the platform decides, its
`validation_failed` details arrive under dotted paths, and the form renders each one beside the
input the path names. A test drives `pickup.postcode` and `dropoff.state` through the real screen
and checks they land under the right two of the eight inputs.

**The state is a text field, not a picker.** `Docs/10` §4.7's exception says a client prints the
state rather than branching on it, and input is accepted in any case and as the spelled-out name.
A picker would be a second copy of the platform's list compiled into a build that has no
over-the-air update path, for no behaviour that depends on it. What is typed is what is sent —
`new south wales` goes out as `new south wales`, and the platform normalises.

**"We could not match this to a place on the map" is drawn as information, not as a failure.**
SHIP-59a requires that a failed lookup does not fail the job, and the platform honours it by
storing the address as typed with no coordinate. The client's half is the other end of that: no
error colour, no warning, and the way on stays enabled. A screen that made a customer resolve a
rural address before continuing would be blocking on something they cannot change.

**Saving twice edits the draft rather than making a second one.** The first save is `POST
/v1/jobs`; every save after it is `PATCH /v1/jobs/{id}` against the id the first returned. Without
that, every corrected postcode leaves an abandoned draft behind — and the customer finds it in
their job list, where nothing can explain it. That is what `ApiClient.patchJson` was added for,
and the idempotency interceptor already covered `PATCH`.

**The idempotency key follows `ActionKey`'s rule, and both directions are tested through the
screen.** A dropped connection keeps the key, because the platform may have created the draft and
the answer may have been lost. A `422` retires it, because the platform saw the request and
refused it, and the customer is about to correct something — replaying that refusal is not what
they asked for.

**The guard needed widening and this is worth knowing before the next screen.** `redirectFor` sent
a signed-in user to `/home` from *every* other location, which was right while the shell was the
only thing to reach. `/jobs/new` is the first that is not, so there is now a `_signedInLocations`
set beside `_signedOutLocations`. **It grants no permission** — reaching the step any other way
still fails server-side on the first request it makes.

**Three pieces landed outside the jobs feature**, each because a second consumer is certain rather
than speculative: `core/api/page.dart` for the list envelope `Docs/10` §4.5 gives every collection,
`shared/formatting/dates.dart` for day-first dates, and `shared/formatting/money.dart` for cents as
AUD. `intl` was deliberately not added: the MVP ships one locale, and what the package would buy is
locale negotiation that would render month-first on a handset set to `en-US`.

**How it was demonstrated.** `make flutter-check` in the wave-4 worktree: 264 host tests, up from
219. The step is driven through the real router, guard, session and shell — signed in as a
customer, opened from the shell's own button, filled in, saved, refused, corrected, saved again —
with the socket and the Keychain the only substitutions. **What was not demonstrated live: the
unresolved-address path against the running API.** `geocoding.UseStub` resolves every address in
development and `NewStub()` is constructed with no unknown list, so a local API cannot answer with
a missing coordinate. The path is covered by the screen tests and by the platform's own
`location_test.go`; a live demonstration needs either a staging deployment with no geocoder
configured or a stub built with an unknown address, and neither belongs in a Flutter ticket.

### SHIP-76 — the customer's jobs, and the budget widget that is private on purpose

The `shell-customer` placeholder is now the customer's own jobs, grouped by status, with
pull-to-refresh. The key is unchanged, so every routing and sign-in test still asserts the same
fact.

**The list is read once and grouped on the device.** `?status=` takes one value, so a screen
showing every status a customer has would need twelve requests to draw itself — twelve round trips
on mobile data, and twelve chances for a partial failure to produce a screen missing a group with
no error to explain it. The contract recommends the opposite and this follows it. The grouping is
a pure function tested as one, and the order comes from `JobStatus.values` rather than from a list
written beside it, so a status added in `Docs/02` §1's order is grouped in that order with nothing
else to edit.

**The group headings carry no counts, and that is honesty rather than an omission.** The list is
paged and the page size is server configuration, so a group holds the jobs that have been read
rather than every job in that status. A number would be right on the first page and quietly wrong
on every screen with more. What the screen says instead, when there is more, is "Showing your most
recent jobs" above a button that asks for the next page — the further pages are the customer's
decision rather than an unbounded read to draw one screen.

**A customer with no jobs sees an empty state, and it is not the failure state.** "You have no
jobs" and "we could not find out" are different things to be told and only one of them has a
retry. A refresh that fails leaves the list on screen with a banner over it, because somebody who
pulled to refresh in a tunnel should still be looking at their jobs.

**`_JobCard` is private and must stay private.** It draws `budget_cents`, which is legitimate
because every job on this screen belongs to the person looking at it — and `Docs/01` §4.3 is
hardest to keep with a shared card taking a budget and a flag saying whether to show it, where the
flag is one careless call site away from wrong and nothing fails.
`budget_stays_on_the_customer_side_test.dart` scans `lib/` with comments stripped and fails when
the budget is named outside a four-file allow list. **It is the client's half of the platform's
`TestOnlyTheOwnersResponseCarriesTheBudget`, and it is not a duplicate of it**: the Go test stops
the field reaching a provider's device, and this stops a widget that renders it being reused on a
provider screen. SHIP-82 writes its own card, as the platform writes its own response type.

**The provider half is asserted to read nothing.** A provider sees no job list and no publish
button, and the fake repository records no call. The platform would refuse a provider creating a
job (`jobs_customer_only`), and that is a different thing from the app not offering it.

**`customerJobsProvider` is auto-disposed, and that is what clears one account's jobs before the
next signs in.** `Docs/07` §3 requires cached job data to go with the token at sign-out; sign-out
unmounts the shell, which drops the last listener. Kept alive it would hold the previous account's
jobs in memory for whoever signed in next on the same handset. A token refresh does *not* dispose
it — the shell rebuilds and the list widget stays mounted — while leaving for the job wizard and
coming back does, which is how a draft just saved appears without anybody pulling.

**One thing SHIP-71 got wrong and this fixed: `core/api/page.dart` exported `Page`.**
`package:flutter/material` exports `Page`, the navigator's route descriptor, so the name is
ambiguous in every file that draws a widget — which is every screen that would consume it. It is
`ApiPage` now, matching `ApiClient`, `ApiFailure` and `ApiHeaders` in the same folder. The
collision was invisible until a widget file imported it.

**How it was demonstrated.** `make flutter-check` in the wave-4 worktree: **292 host tests**, up
from 219 before this branch. Then against this worktree's API on 8092, by `curl`, because the wire
is where a client is actually wrong: a fresh customer's list is `{"data":[],"has_more":false}`
(the empty state's whole premise); a draft created with `"state":"new south wales"` comes back
`"NSW"` with a coordinate and a `formatted`; a half-filled address answers `422` with
`pickup.state` and `pickup.postcode` — the exact keys the form looks up — while an entirely empty
`dropoff` produces no error at all; `PATCH` edits the same id rather than making a second job;
`?status=Draft` is `400` where `?status=draft` is `200`; and `?limit=1` returns `has_more: true`
with a cursor that fetches the second page and then reports `has_more: false`.

**Not demonstrated live: the screens themselves on a simulator.** Everything above is the contract
this client was written against, checked by hand; what has not been done in this branch is
installing the build on a device and driving the two screens against that API, which is what
SHIP-55's report did for sign-in. The widget tests drive the real router, guard, session and shell,
so what a simulator would add is the platform channel and the renderer.

### What SHIP-78 built, and the first test the migration guard ever got

`internal/fleet` is open. Migration `000300` creates `vehicles`, and six routes under
`/v1/fleet/vehicles` let a provider add, edit, deactivate, reactivate, read and list their own
fleet. It is the third domain to hold code, and the first in M3.

**The migration guard fired on the first `make migrate-up` and the message was actionable.** Fleet
draws from block 300–399 and the development database was at `000404`, which is exactly the
situation SHIP-15g predicted this ticket would hit — "days from happening rather than theoretical",
and it happened on the first command. The refusal named the migration, its domain, and the one-line
fix; `make migrate-down n=all && make migrate-up` applied it, and nothing else was needed. Recorded
because SHIP-15g was built on a hand-made reproduction and this is the first real occurrence: the
guard works and the message needed no interpretation.

**Vehicles are deactivated, never deleted, and that is structural.** There is no `DELETE` route and
no `active` field in any request type. `deactivated_at` is not among the columns an edit writes, so
an ordinary `PATCH` cannot take a truck off the road by accident — the same discipline that keeps
`status` out of jobs' `draftColumns`. The reason is not squeamishness about deletion: a vehicle is
named by the bid that won a job (SHIP-89) and by the delivery that followed it (SHIP-105), so the
row outlives the provider's interest in it, and `Docs/05` §3.1 requires that record retained.

**The one-live-plate rule is a partial unique index, not application logic.**
`uq_vehicles_provider_registration` covers only vehicles whose `deactivated_at` is `NULL`, which
buys three things a `SELECT` before the `INSERT` could not: a second live row for one truck is
refused even when two requests race; a retired plate can be given to a replacement vehicle, because
a truck sold and a replacement carrying the same personalised plate is ordinary; and the collision
that only *reactivation* can hit — the plate was taken while this vehicle was off the road —
surfaces at exactly the moment it becomes real. That last case answers `409`
`fleet_duplicate_registration` from a request that supplied no registration at all, which is the
clearest evidence in the repository so far for `Docs/06` §4.1's "do not abstract PostgreSQL": a
mocked repository would accept every one of them.

Registration is normalised to upper case **with its spaces removed**, not merely collapsed. A plate
is written both ways — `ABC 123` on the vehicle and `ABC123` on the paperwork — and keeping the
space would let the index see two live vehicles where there is one, which is precisely the duplicate
it exists to refuse.

**`vehicle_type` is a closed list of eleven and it is not SHIP-79's capability vocabulary.** That
distinction is worth holding on to, because `000404` promised the vocabulary
`jobs.vehicle_requirement` will one day be validated against to the fleet domain and it would be
easy to read this as it. It is not: SHIP-79's list is what a *provider* declares about the work they
take, and this answers the narrower question of what a vehicle is — which cannot be deferred,
because a fleet record that does not say what the vehicle is describes nothing. **Nothing in `jobs`
is validated against these values and nothing in `fleet` reads that column.** The pairing test
`Docs/10` §3.4 requires reads `ck_vehicles_type` out of `pg_constraint` and holds it to
`fleet.VehicleTypes` in both directions.

**Fleet emits no domain event and has no `ports.go`, and both are decisions rather than
omissions.** It is the first domain that needs nothing of another domain and nothing of an adapter —
verification is checked against the *provider* and belongs to `profiles`, and SHIP-81 is where fleet
first has to ask another domain a question. Nor is anything waiting to hear that a provider bought a
van: SHIP-81 reads this table directly rather than a projection, and SHIP-89's bid names a vehicle by
id at the moment it is placed. An event today would have no consumer, and an event with no consumer
is a shape somebody later has to either keep or break. **If SHIP-81 or SHIP-134 finds it wants one,
adding it is additive** — the outbox writer and the `EventSink` shape are already established in
`jobs`.

**A retired vehicle can still be edited, and editing it does not bring it back.** Refusing the edit
was the tempting alternative and it is wrong: a provider correcting the plate on a truck that is off
the road for a month would otherwise create a second row for the same vehicle, which is the
duplication deactivation exists to avoid. Returning to service is its own operation because it is
the one that can collide.

**The fleet list defaults to *every* vehicle rather than the active ones**, which is the less
obvious of the two choices. A screen that silently hid retired vehicles would leave a provider
unable to find the one they need to bring back; `?active=true` is one parameter away for the screen
that wants only what can be offered. `?active=yes` is a `400` rather than an empty list, for the
reason `jobs` refuses `?status=Draft`.

Nothing shared was edited beyond the two lines `contracts/openapi.yaml` reserves per domain, the
regenerated `routes_golden.txt` and `Docs/10-api-error-codes.md`, and this file. `Deps` needed no
field: fleet builds from the clock and the pool alone.

### SHIP-79 — a service area is a set of regions, and that is what keeps `internal/geo` unbuilt

Migration `000301` creates `provider_service_areas` and `provider_specialties`, and
`GET`/`PATCH /v1/fleet/profile` reads and replaces what a provider has declared: where they will
carry freight, and what kind of freight they carry. `Docs/01` §4.2's "nominate service area and
specialties", and the half of eligibility that belongs to a *provider* rather than to one vehicle.

**The decision this ticket existed to take: a service area is a set of named regions — states and
postcodes — and not a radius around a point.** §9 has named "the first ticket needing the distance
between two coordinates in a domain other than `jobs`" as the trigger for writing `internal/geo`
since SHIP-60, and named SHIP-81 as the likely one. **It is not.** SHIP-81 is now a set-membership
query, this domain does no distance arithmetic, and `internal/geo` remains unwritten and
unregistered. Three reasons, in the order they weighed:

1. **A radius would make eligibility depend on a field the platform may not have.** A job's
   coordinate is best-effort by design — SHIP-60 stores the address exactly as typed when a lookup
   fails, and §3 above records that staging and production get *no geocoder at all* until one is
   configured. A radius filter in that state matches nothing: a provider opens the app to an empty
   feed with no error anywhere to explain it. The state and the postcode are typed by the customer
   and validated on the way in, so they are always there.
2. **It is how Australian road freight is quoted.** Providers price by postcode zone and by state
   rather than by kilometres from a depot, and a straight-line radius is wrong about roads — 300 km
   from Melbourne reaches Tasmania across Bass Strait.
3. **Both documents already read it this way.** SHIP-60 gave a job address four parts rather than
   one freeform line "because the suburb, the state and the postcode are values SHIP-79 and SHIP-81
   will compare", and the geo decision itself recorded that "neither names a radius in kilometres".
   This is that reading held to rather than quietly reversed.

**What would reopen it is named rather than left to judgement**, in the same style: a ticket that
genuinely needs a distance — "providers within 50 km of the pickup, ranked" is the shape. The answer
then is still §9's, and the regions stay, because a provider has to be able to say "I do not cross
the Nullarbor" in a form a straight line cannot express. **§9's own entry still says the trigger is
"likely SHIP-81" and that is now known to be wrong**; correcting that sentence is a reconciliation
edit rather than this ticket's, since a domain branch does not own §9.

**An entry names one grain and never two**, and that follows directly from a decision §3 already
records. `ck_provider_service_areas_scope_and_area` accepts either a whole state or one postcode,
never a postcode qualified by a state — because SHIP-60 deliberately refused to validate a postcode
against its state (the allocations have exceptions, 2600 is ACT inside the NSW range, and they move
when Australia Post says so). A row carrying both could disagree with itself, and checking that it
did not would be exactly the validation that decision refuses. One grain per row leaves nothing to
be inconsistent about.

**An empty declaration matches no job rather than every job.** Eligibility is opt-in, or the
provider who has not finished onboarding would be the widest-reaching provider on the platform —
and `Docs/04` §3 requires the area declared before verification passes. `fleet.Profile.Serves` is
the reading SHIP-81 should use; a test in `internal/fleet` and a check in `make verify` both assert
the empty case.

**Three lists, and each of them has three states rather than two.** Omitted or `null` leaves a list
exactly as it is; `[]` clears it; entries replace it whole. That is `VehicleFields`' distinction
applied to a set, and the empty list is not decoration — without it a provider who withdrew from
every single postcode and now covers whole states only could not say so. There is deliberately **no
add-one or remove-one operation**: the screen renders the declaration as chips and sends the set
back, and two operations over one collection is where a client and a server stop agreeing about
what is in it.

**What is replaced is the set, not the rows.** The store deletes only what has left the declaration
and inserts only what is new, so an entry the provider has held since January keeps its `created_at`
through a request that merely resends it. That matters because `Docs/04` §3 makes the service area
something a verification decision is taken against, and "when did they take on Queensland" is the
question support would ask.

**The first advisory lock outside `cmd/worker`, and it is load-bearing rather than defensive.**
Replacing a set is a delete and an insert that must be one decision, and a row lock cannot express
it — what has to be serialised includes the case where the set is currently *empty*, so there are no
rows for `FOR UPDATE` to hold. Without `pg_advisory_xact_lock`, two devices declaring at once in
READ COMMITTED each fail to see the other's uncommitted insert, both survive, and the stored
declaration becomes the union of two sets neither client asked for — a lost update with nothing to
report afterwards. `TestTwoDevicesDeclaringAtOnceDoNotProduceTheUnion` was **watched failing with
the lock removed** before it was believed. The lock class is `79`, following the convention
`cmd/worker/outbox.go` set at `134`: the ticket's number, so two uses cannot share a key space by
accident.

**Twelve specialties, closed, and a declaration is never a permission.** Naming `dangerous_goods`
claims a capability; what a provider is licensed and insured to carry is verification's business
(`Docs/04` §3) and what may not be carried at all is X-9's. This is the capability vocabulary
`000300` and `000404` were both careful to say `vehicle_type` is *not*: that answers what a vehicle
is, this answers what its owner does. The pairing test `Docs/10` §3.4 requires reads the constraint
out of `pg_constraint` in both directions.

**The eight states are a second copy of a list `jobs` also holds**, and that is the boundary rule
working rather than failing. Domains do not import each other, so `fleet` cannot reach
`jobs.States`; eight strings that have not moved since 1975 are cheaper duplicated than registered
in `internal/boundaries` — which is a shared file a domain branch must not edit anyway. Each copy is
paired with its own `CHECK` by its own test, so neither can drift unnoticed.

**Validation names the position, not just the list.** A detail's field is
`service_area.postcodes.2`, because the client renders each entry as its own control and an error
naming only the list leaves the provider to work out which of forty postcodes is meant. It is an
extension of `httpx.FieldError`'s dotted path through an array index rather than a new convention.
The offending value is deliberately not echoed back.

**No new error code, and that is worth stating.** `fleet_provider_only` already exists and covers
the only domain-specific refusal here; everything else is `validation_failed` with details or the
middleware's own business. A domain code earns its place only where a client would otherwise have
to parse a message. The read is deliberately *not* refused to a customer — their declaration is
empty because they have never made one, and a 403 on a read that discloses nothing would make the
client special-case a screen it never shows.

Nothing shared was edited beyond the one line `contracts/openapi.yaml` reserves per path (plus a
sentence on the `Fleet` tag, which is fleet's own block), the regenerated `routes_golden.txt`, and
this file. `Deps` needed no field and `internal/boundaries` was not opened.

### What SHIP-80 built, and the ticket it turns out to have finished

`internal/bidding` is open. Migration `000500` creates `bids`, and it adds no route, no handler and
no error code — the package holds `Status` and nothing else. **It is demonstrated by its own tests**
rather than by `make verify`, the way the wave-1 adapters and `job_status_history` were: there is no
HTTP surface to exercise, and the check count is therefore unchanged **by this ticket**. `make verify`
was not touched and `scripts/verify/` gained no file. (§3's headline carries the current figure for
the whole tree; a per-branch count is a record of one merge and goes stale at the next.)

**The one-accepted-bid invariant is now a database guarantee, and the constraint is
`uq_bids_one_accepted_per_job`** — a partial unique index on `bids (job_id) WHERE status =
'Accepted'`. CLAUDE.md states the invariant as "enforced by a database constraint, not application
logic alone", and the "not alone" is the load-bearing half: a `SELECT` that finds no accepted bid
followed by an `UPDATE` that creates one is correct in a single-threaded reading and wrong under two
customers' requests, two retries of one request, or one request racing its own idempotency replay.
The index also does the *locking*, which is the part worth knowing before SHIP-92 is designed — two
transactions writing the same key into a btree do not race, the second blocks on the first's
uncommitted entry and is then told the answer. `TestOneAcceptedBidPerJobHoldsUnderARace` runs exactly
that and asserts the job ends with one accepted bid.

**That is SHIP-91's *Done when*, met by SHIP-80 — and the owner has since ruled on it. See the next
subsection; the paragraph below is left as written, because it is the finding rather than the
decision.** "A partial unique index makes a second accepted
bid impossible at the database level" is now demonstrable, and this file is not going to claim a
ticket its commit did not name — `Docs/11-done.txt` gains SHIP-80 alone. But **whoever picks up
SHIP-91 should expect to find it already built** and either close it as delivered here or reduce it
to the confirmation on the award branch. It landed early because it could not sensibly land later:
`Docs/09`'s own note beside SHIP-91 says the constraint comes before the endpoint because "it is far
easier to build correct behaviour against a constraint that already exists than to add one
afterwards and discover your data violates it", and a `bids` table shipped without it would have
been a window in which exactly that data could accumulate.

**The rule the table is built on is *constraints now, columns later*.** Adding a nullable column
later is a two-line migration; adding a constraint later is a migration plus whatever has to be done
about the rows that already break it — and the rows that break this one are two providers who each
believe they have the job. So every constraint and index `bids` will want is here, and the columns
are only the ones a bid cannot be a bid without. The migration names what is deferred and who owns
it: SHIP-84 the timing and the vehicle selection, SHIP-87 and SHIP-88 the supersede chain, SHIP-89
the expiry terms, SHIP-92 the award record.

**There is deliberately no "one active bid per provider per job" index, and that is the omission most
likely to look like a mistake.** SHIP-84's *Done when* is "can bid once per job", so the rule is
real — but *active* is defined by the supersede design SHIP-87 and SHIP-88 own, and Docs/02 §4 keeps
superseded offers as readable history, which means several rows per provider per job is the ordinary
case rather than the defect. An index written against a guess at that definition is one the ticket
would have to drop. It is additive whenever the definition is settled.

**`bids.amount` is here and `jobs.budget` still is not, which is the right way round.** The provider's
own asking price is `numeric(12,2)`, AUD implied, per `Docs/10` §3.3 — nothing in this schema
carries, derives from or hints at the customer's budget, and SHIP-67 still owns that column together
with the serialisation test proving it cannot reach a provider (`Docs/01` §4.3). The amount is
nullable, mirroring `000404`'s job draft: a Draft may be incomplete, because refusing an incomplete
row refuses to save what somebody has typed so far. `ck_bids_offer_has_an_amount` is what makes that
coherent — anything past Draft names a price, which matters most at `Accepted`, where an award would
otherwise commit both parties to an unstated amount.

**Unlike `jobs`, bid creation has no guard trigger, and that is a decision.** `000402` refuses a job
inserted at anything but `Draft` because `Docs/02` §2 gives the job lifecycle one entry point and a
guarded function every transition passes. `Docs/02` §4 says no such thing about bids: a provider who
fills the form in and sends it legitimately creates a row at `Submitted`, and a customer's
counter-offer arrives as a row that was never a draft. `DEFAULT 'Draft'` is a convenience for the
provider composing an offer, not a claim about the only way in.

**The migration guard did not fire**, which is the counterpart to SHIP-78's finding rather than a
contradiction of it. Bidding draws from block 500–599 and the database sat at `000404`, so `000500`
is *above* the recorded version and applied cleanly. The guard refuses a migration numbered *below*
it, which is what fleet's `000300` hit. Both behaviours are now observed on real work.

Nothing shared was edited at all: no route file, no `contracts/openapi.yaml` entry, no
`routes_golden.txt`, no `scripts/verify/` file, no `internal/boundaries` change — `bidding` was
already a registered domain and `000500` was already its reserved block. This file and
`Docs/11-done.txt` are the whole of it.

### SHIP-81 — the first query needing three domains' data, and the shape it settles for the rest

`internal/fleet/eligibility.go` answers `Docs/01` §4.3's first line — "Filter jobs by provider
service area, vehicle capability, verification state, and job status" — as **one SQL predicate**.
There is no endpoint; SHIP-82 puts `GET /v1/jobs/open` in front of it. Migration `000302` adds the
one index the filter needs that did not already exist.

#### The decision: one SQL statement in `fleet`, not ports and composition in Go

The four filters read four tables owned by three domains — `provider_service_areas` and `vehicles`
here, `users` in the shared block, and `jobs` in a domain `fleet` may not import. Two shapes were
honestly available, and the reasoning is repeated at the top of `eligibility.go` because the next
such ticket will copy it.

1. **Ports cannot page a set intersection, and an unpageable feed is not a feed.** To return twenty
   eligible jobs, something must know which open jobs are eligible *before* taking twenty. A port
   handing `jobs` back to `fleet` puts the filter in Go, so the page boundary falls on the
   *unfiltered* set: a provider serving one postcode would read every open job on the platform to
   fill one screen, and "the next twenty eligible" would have no answer at all. SHIP-82's *Done
   when* says "paginated", so this is not a performance preference — it is whether the next ticket
   can be built.
2. **The only port shape that *can* page puts `fleet`'s policy inside `jobs`.** That shape is one
   where `jobs` runs the filter — "open jobs in these regions fitting one of these boxes" — which
   is the eligibility rule itself, written in the domain that does not own it. `fleet/doc.go` has
   said since SHIP-78 that eligibility is decided here, and `Docs/07` §3 requires it decided
   server-side in exactly one place.
3. **The Go boundary is about imports and is not being crossed.** `eligibility.go` imports no
   domain; `make lint-imports` and SHIP-11's test are satisfied. CLAUDE.md's *other* rule points
   the same way: **do not abstract PostgreSQL**, and a repository interface introduced only to keep
   another domain's table names out of a file would be exactly that abstraction, bought with an
   N+1.

**What is given up is real and is not answered by a promise to be careful.** A `SELECT` naming
`jobs` is a coupling with no compiler behind it. Three things stand in for one:

- **Every query runs against the real schema in `make check`** — the tests are integration tests by
  construction, so a renamed column is a red build rather than an empty feed.
- **The reach is confined to one file, enforced by a test.**
  `TestOnlyTheEligibilityFilterReadsTheJobsTable` parses the package and fails if any other
  non-test file names the `jobs` table. "Which parts of `fleet` reach into `jobs`" is answerable by
  reading one file.
- **The two job statuses the SQL hard-codes are paired with `ck_jobs_status`** read out of
  `pg_constraint`, in both directions — `Docs/10` §3.4's discipline applied across a domain
  boundary for the first time.

**What would change the answer is named**: a fifth domain's table joining the predicate, or a
filter needing something no SQL expression can express. The shape to move to then is a materialised
eligibility projection fed by domain events — a real design with a real cost, not worth paying for
four tables. **A database view was considered and rejected**: it would have given one definition
readable from Go *and* psql, but PostgreSQL refuses to drop or retype a column a view reads, so a
`fleet` migration creating one would block the jobs track's next migration through a file they
cannot see.

#### The cost that has no clean answer: the SELECT crosses the boundary and the index cannot

The feed reads `WHERE status IN ('Open','Negotiating') AND (expires_at IS NULL OR expires_at > $2)
ORDER BY created_at DESC, id DESC`, and nothing in `jobs` indexes that — `idx_jobs_open_expiry` is
partial on `'Open'` alone and ordered by `expires_at`. The index wanted is roughly
`(created_at DESC, id DESC) WHERE status IN ('Open','Negotiating')`, and **it is in block 400–499,
which a fleet migration may not draw from.** So the feed currently plans as a sequential scan on
`jobs` under a `LIMIT` — correct, and cheap while open jobs number in the hundreds.

This is the one asymmetry worth carrying forward: reading across a domain boundary in SQL works,
and *indexing* across one does not. It is a request to the jobs track when volume warrants it, not
something this branch could have fixed.

**`000302` therefore adds the index that *is* fleet's**: `idx_vehicles_capability`, partial on
vehicles in service and covering the four capacity columns, so the capability check — the one
clause that runs once per candidate job — is an index-only scan.

**The job-first index on `provider_service_areas` was NOT created, and `000301`'s expectation that
SHIP-81 would need it is wrong.** The filter is *provider*-first in both forms: the feed asks
"which open jobs may this provider see" and SHIP-84 asks "may this provider see this job", and both
bind `provider_id` first, which `uq_provider_service_areas` already leads on. The job-first
direction — "which providers serve this postcode" — is the **notification fan-out**, and it is
still M5's. Creating it here would be write amplification for a query nobody makes.

#### Two readers, one predicate

`eligible` is a `WHERE` clause and nothing else. `Service.EligibleJobs` wraps it in a page (SHIP-82
serves that) and `Service.EligibleFor` wraps it in `EXISTS` for one job, which is what SHIP-84 needs
before accepting a bid — `fleet/doc.go` requires both. `TestEligibleForAgreesWithTheFeed` holds them
to each other across every case, because a provider shown a job and then refused a bid on it is the
worst of both.

#### `Docs/02` §1 was read, and it says **two** statuses are biddable, not one

The obvious reading of "only Open jobs are eligible" is wrong. `Docs/02` §1 defines Negotiating as
"one or more active bids or counter-offers exist; **job remains open to eligible bids**", and adds:
"'Negotiating' is a useful presentation status. Technically, the job remains available for eligible
bids unless the customer closes it or awards a bid."

**Nothing can reach Negotiating until SHIP-90**, which is exactly why this had to be got right now:
a filter accepting only `'Open'` would pass every test written today and surface months later as
jobs vanishing from every provider's feed the instant somebody bid on them — which reads as a
bidding bug, not as one line in a predicate. `TestANegotiatingJobIsStillBiddable` and a `make
verify` check both assert it.

**The deadline is part of the status filter rather than a fifth filter.** SHIP-68's sweep runs on a
ticker, so between `expires_at` passing and the worker reaching it the row still says `'Open'`. A
feed trusting the column alone offers work nobody may bid on, and the provider finds out by being
refused after pricing it.

**`j.customer_id <> $1` is `000500`'s clause, handed here by name** — "it is a comparison across two
tables and belongs with SHIP-81's eligibility filter". It is unreachable today (`users.role` is
immutable, and `jobs` refuses a non-customer) and costs one line to be right if either changes.

#### Verification state is deliberately incomplete, and the ticket that completes it is named

`Docs/04` §4's five outcomes — Pending, Verified, Restricted, Rejected, Suspended — **do not exist
yet**: `000002` says they "live with profiles", `internal/profiles` is empty, and block 200–299 is
unused. The filter checks the part of `Docs/04` §3's baseline that does exist, which is its
automated row: email and phone verified, on a provider account with `status = 'active'`.

`'restricted'` is excluded as well as `'suspended'` — §4 makes Restricted "limited access pending
clarification" and §1 says a provider does not bid until baseline checks are complete, so the
conservative direction is also the reversible one.

**The seam is the thing to remember: SHIP-152…154 add their clause to *this* predicate.** If that
work adds a second eligibility check elsewhere, the platform will have two answers to who may bid.

#### Opt-in is one rule, not four coincidences

SHIP-79 settled it for the service area — an empty declaration matches nothing, or the provider who
has not finished onboarding becomes the widest-reaching provider on the platform. `EXISTS` gives the
same answer for the other three by construction: no account row, no declared region, no vehicle in
service, no jobs. `TestAProviderWhoHasDeclaredNothingSeesNothing` checks all four halves separately,
because a `NOT EXISTS` in the wrong place would pass three of them.

**Its mirror image matters as much: a number that is missing never excludes.** `000300` lets a
provider add a truck with a plate and nothing else; `000404` lets a customer publish without
measuring. A comparison is made only where both sides supplied a number, so the filter excludes only
on a *known* mismatch — treating "not stated" as "does not fit" would empty the feed of every job
whose customer left a field blank, which is most of them. **Dimensions are compared axis to axis and
rotation is not modelled**, because modelling it means guessing how goods will be packed, which is
the provider's decision at the tailgate. What would reopen that is providers reporting jobs they
could have taken and never saw.

#### Two disclosure decisions this ticket had to take, because no document had

`EligibleJob` is the first provider-facing shape anywhere outside `internal/jobs`, and that put two
questions on this branch that nobody had answered.

- **The budget guard is package-scoped, and this is the first thing it cannot see.** SHIP-67's
  source-parsing test parses `internal/jobs`; its own header says it is waiting for "SHIP-82's feed
  and SHIP-83's provider detail". SHIP-81 arrives before either.
  `TestNoProviderFacingShapeCarriesTheBudget` is `fleet`'s copy, and it makes a **stronger**
  statement than the original can: the allow-list is empty and must stay empty, because every shape
  in this domain is read by a provider. It also checks the SQL — `jobs.budget` sits three lines from
  `weight_kg`, so the `SELECT` list is the real disclosure boundary, which is why the query names
  columns rather than selecting the row. A `make verify` check asserts the same thing from outside
  Go.
- **The provider's feed carries suburb, state and postcode — not the street line.** No document
  takes a position on when a provider learns the exact door, so this takes the reversible direction,
  which is the argument `Docs/01` §4.3 makes about the budget applied to an address: a provider
  prices on the locality, and the doorstep is needed by whoever drives to it, after the award.
  Disclosing later is easy; withdrawing later is not. **SHIP-83 confirms or reopens it**, and should
  do so explicitly rather than inheriting it silently. The feed carries no customer identity either.

#### How it is demonstrated

`make verify` gained seven checks in `scripts/verify/60-fleet.sh` — a dedicated provider, a real job
created through `POST /v1/jobs` and published through `000402`'s guard, then each filter broken in
turn. **Those checks run a mirror of the predicate rather than the predicate itself**, which is
stated in the section's own header: `internal/fleet/eligibility_test.go` is authoritative, and
SHIP-82 replaces the mirror with the endpoint. What the mirror buys meanwhile is evidence from
outside Go that the four filters are answerable from the real schema against rows the real API
created.

The Go tests are where the *Done when* is met: twelve subtests in
`TestEachFilterExcludesSomething`, one per way a filter can exclude, each starting from a world every
filter accepts and breaking exactly one thing. **The predicate was replaced with `TRUE` and the
suite watched failing** — sixteen subtests, so none of them is decorative.

Shared surfaces: none. No route, no `contracts/openapi.yaml` entry, no `routes_golden.txt` change,
no `internal/boundaries` edit, no `Deps` field, and — as SHIP-79 predicted — **no `internal/geo`**.
This file, `Docs/11-done.txt`, `scripts/verify/60-fleet.sh`, and fleet's own package and migration
block are the whole of it. `internal/bidding` was untouched: it owns none of the four tables, and
SHIP-91…95's award transaction is a single-owner branch that a feed query has no business sitting
in front of.

### SHIP-82 — the feed, and the route that is not in its own domain's file

`GET /v1/jobs/open` puts SHIP-81's predicate in front of a provider, keyset-paged in the envelope
`Docs/10` §4.5 gives every list. Two handlers, two contract operations, no migration and no new
error code: the ticket is the endpoint the previous one was built for.

#### The decision: a route is declared where its answer is decided, not where its path points

The route is `/v1/jobs/open` and it is declared in **`cmd/api/routes_fleet.go`**, which looks wrong
for a second and is the only arrangement that holds. Three things pointed the same way:

1. **`fleet` decides eligibility, so `fleet` serves the endpoint.** The alternative is a
   `fleet.Handler` constructed inside `routes_jobs.go`, which makes one domain's route file depend
   on another domain's package — a shared-file edit in the exact place `Docs/10` §4.1 built the
   manifest to avoid.
2. **Routes are declared, not registered, and the file name is not part of the URL.** The manifest
   sorts by path, `routes_golden.txt` records the served surface whatever file each line came from,
   and `TestEveryRouteIsInTheContract` checks the contract in both directions. Nothing in the
   mechanism cares which file an `init` sat in; the *tracks* care a great deal.
3. **The path is still right, because the resource is a job.** `/v1/jobs/open` is the collection of
   jobs offered to the calling provider and `/v1/jobs/open/{id}` is one member of it. net/http
   prefers the more specific pattern, so it coexists with `jobs`' own `/v1/jobs/{id}` with neither
   file knowing the other exists.

**This wave proves the arrangement rather than asserting it**: track B held `routes_jobs.go` for
the whole of it, and this branch added two routes to the served surface without touching that file
or any other track's. The contract fragment follows the same rule — the operations live in
`contracts/paths/fleet.yaml` and are tagged `Jobs`, because the fragment follows the *serving
domain* and the tag follows the *resource a client is asking about*.

#### What replaced SHIP-81's mirror, and why the count went up rather than down

SHIP-81 demonstrated the four filters in `make verify` with a hand-written copy of the SQL
predicate, marked as a mirror in its own section header, with the note that SHIP-82 would delete
it. **It is deleted.** Every filter check now runs through the endpoint, so there is one
description of the rule instead of two, and a filter that stopped working fails the run instead of
passing against a copy of itself.

The replacement asks **both endpoints on every check and requires them to agree** — the feed
carries the job, or `GET /v1/jobs/open/{id}` answers 200, and never one without the other. That is
the same claim `TestTheThreeReadersOfTheFilterAgree` makes in Go, made again over HTTP, and it is
worth making twice: a provider shown a job in the feed and then refused it on the detail screen is
the worst of both outcomes.

`make verify` went from 288 checks to **292** at this ticket and to **297** once SHIP-83 added its
section — a net gain of nine across the pair, after the deletion. `make verify-update` wrote both
figures; neither was typed.

#### Pagination, and the case a one-field cursor gets wrong

The cursor is `internal/pagination`'s encoding — no second one was invented — over SHIP-81's
`JobCursor`, which is a `created_at` **and** an `id`. The reason is the tie: a customer publishing
several jobs in one sitting writes rows the database timestamps identically, and a keyset ordered
on time alone either repeats those rows forever or drops them, depending on whether the comparison
is `<` or `<=`.

**`TestPagingTheFeedReachesEveryJobExactlyOnce` is written against exactly that.** Seven jobs, three
of which share a `created_at` to the microsecond, paged at every size from one to eight — because a
page size that happens not to land between the tied rows hides the defect completely. The check is
a *multiset*: the number of entries seen is compared as well as the set of identifiers, so a job
returned on two consecutive pages fails a test that a set comparison would pass.

It was verified by mutation rather than by inspection. Replacing the row comparison
`(j.created_at, j.id) < ($3, $4)` with `j.created_at < $3` makes the suite fail at four page sizes,
reporting **five of seven jobs at a page size of one** — two silently gone. `make verify` pages the
live feed one job at a time and asserts the same property against the running service.

#### A provider eligible for nothing gets an empty page, not a refusal

An unverified provider, one who has declared no service area, one with no vehicle in service, and a
customer who followed a link meant for the other role all get `{"data":[],"has_more":false}`. None
of the four is a failure of the request — they are the truthful answer to it — and the client sends
the provider to onboarding from an empty list and the profile it already holds. It is the reading
`Service.Profile` already takes for a read that discloses nothing, and refusing here would make the
client special-case a screen it can render anyway.

### SHIP-83 — the fourth proof, and the two disclosure decisions confirmed

`GET /v1/jobs/open/{id}` is one job out of the feed. The endpoint is small; the test is the ticket.

#### The debt §8 recorded, paid

§8 has said since SHIP-67 that the pairing could not be honoured in one wave, that SHIP-67 landed
with the three strongest proofs available at the time, and that **"SHIP-83 remains reserved to this
owner and adds the fourth: its provider response, serialised, asserted to carry no budget. That is
the test the pairing was actually for, and it is the one thing that is still owed."**

`TestTheProviderResponseCarriesNoBudgetInAnyForm` is that test. It does four things a struct-field
check cannot, and each was chosen against a specific way the weaker version would have passed while
the invariant was broken:

1. **It obtains the response the way a provider obtains it** — an HTTP request through the real
   handler, mounted on the pattern `cmd/api` serves, answered as bytes. A field can be absent from
   a struct and present on the wire through an embedded type, a custom `MarshalJSON`, or a map, and
   a struct check sees none of the three.
2. **It works on the raw bytes, so a key that is present and null still fails.** Decoding into a Go
   type turns `"budget_cents": null` into a zero value indistinguishable from a field never sent —
   and a present-but-null key *is* the "budget supplied" flag `Docs/01` §4.3 forbids, because a
   provider learns which jobs carry a budget from which responses carry the key.
3. **It asserts a closed set of keys rather than searching for the word.** This is the one that
   matters most. A search catches `budget_cents` and misses `max_price`; the allow-list is every
   key this API promises a provider, so a field arriving under *any* name fails. The `OpenJob`
   schema is `additionalProperties: false` for the same reason.
4. **It checks the value, not only the name.** The fixture's budget is a number appearing nowhere
   else in the job, and the body is searched for every rendering an encoder could produce, with
   UUIDs stripped first — a UUID is hexadecimal, so a run of digits can occur inside one by chance,
   rarely enough to pass review and often enough to fail one morning.

It covers **three** responses, not one: the feed, the single job, and a second page reached by
following a cursor, which is a different code path through the same handler and would be reached
only by a provider who scrolled. And it **refuses to run against a job with no budget** — the first
thing it does is read `jobs.budget` back out of the row, because a privacy test whose fixture has
nothing to leak passes forever and proves nothing.

**Both halves were verified by mutation.** A field `max_price float64` carrying 4321.99 was added to
`openJobResponse`: SHIP-81's source-parsing guard **passed** — it searches for "budget" — and the new
test failed on all three responses, naming the key and its JSON path. Renaming that field
`budget_cents` made the source guard fail as well. **That is the gap the closed key set closes**, and
it is the reason this is not simply SHIP-67's test moved.

#### The guard already reached the new shapes, and did not need extending

Run 2's `TestNoProviderFacingShapeCarriesTheBudget` parses **every non-test file in
`internal/fleet`**, so `openJobResponse`, `regionResponse` and `windowResponse` came under it the
moment they were written — confirmed by injecting `BudgetCents` and watching it fail with
`openJobResponse.BudgetCents carries the customer's budget`. Nothing was added to it. What the new
test adds is the axis that guard cannot have: it reads *source*, so it can only ever refuse a name.

`make verify` makes the same closed-key-set assertion from outside Go against the running service,
so neither can be quietly deleted alone.

#### The street line: confirmed, and widened to the coordinate

SHIP-81 chose suburb, state and postcode for the feed and asked SHIP-83 to confirm or reopen it
explicitly. **Confirmed.** No document takes a position on when a provider learns the exact door, so
this stays with the reversible direction — the argument `Docs/01` §4.3 makes about the budget,
applied to an address. A provider prices on the locality, the distance and the state; the doorstep
is needed by whoever drives to it, which is after an award. Disclosing later is easy and
withdrawing later is not.

**The widening is the part that would have been easy to miss: the coordinate is withheld too.**
`jobs` geocodes the whole address, so a pickup coordinate *is* the street line written as two
numbers. A shape that withheld `line` and sent `coordinate` would have kept the letter of the
decision and broken it completely. `TestTheProviderJobCarriesNeitherTheStreetLineNorTheCoordinate`
asserts both against a job that has both, and also asserts the locality *is* there — so it is a
decision about grain rather than a response that forgot the address.

**What reopens it is named**: the awarded provider needs the exact address, and that is a different
shape at a different moment (SHIP-93 onwards), not a field added here.

#### One shape for the feed and the detail view, which is itself a privacy decision

Both endpoints answer with the same type, and a test asserts the detail response is byte-identical
to the feed entry. That is the rule the vehicle endpoints already follow — a client parses one type
whatever it did to obtain the resource — and here it does something further: **two shapes would be
two places a budget field could be added and two responses a test would have to know to check.**
One shape is one of each. A "detail" view carrying a field or two more is exactly where somebody
would later put "just a little more".

#### The third reader of the predicate, and why it is a read rather than a check-then-read

The obvious implementation asks `Service.EligibleFor` and then reads the job. It was rejected: the
read needs its own `WHERE`, and the only honest one is the predicate itself — so the choice is
between naming eligibility twice and naming it once — and two statements can disagree, because the
customer can cancel the job between the check and the read, leaving the detail view serving a job
nobody may bid on. `Service.EligibleJobFor` runs one statement with the same column list and the
same clause.

`EligibleFor` is untouched and is still what SHIP-84 asks before it writes a bid; a caller deciding
whether to permit something wants a boolean, not a row. SHIP-81's `TestEligibleForAgreesWithTheFeed`
is now `TestTheThreeReadersOfTheFilterAgree` and holds all three to each other across every case.

#### A job a provider may not bid on is a job that does not exist

404, with a body byte-identical to a job that is not there, and a message that says nothing about
eligibility — a refusal that explained itself would disclose what the status code is withholding.
Nine cases in Go and two in `make verify`, including the owning customer, who is refused their own
job here: `GET /v1/jobs/{id}` is where they read it, and that response is the one shape in this API
that carries the budget.

Shared surfaces: two `$ref` lines in `contracts/openapi.yaml`, two lines in `routes_golden.txt`
(regenerated, not typed), and §3's check count (written by `make verify-update`). No
`internal/boundaries` edit, no `Deps` field, no migration, no new error code — `not_found` already
says the right thing — and `internal/fleet` still imports no domain.

### SHIP-91 — delivered by SHIP-80, and closed by a ruling rather than by a commit

**The owner declared SHIP-91 delivered on 12 August 2026.** §6 had carried it as the wave's one
open question, deliberately unanswered by the track that found it: `Docs/11-done.txt` names what a
commit subject claimed, and SHIP-80's did not claim this. That is the right rule and it is the
reason the question survived to be decided rather than being settled implicitly by whoever picked
the branch up.

**It was verified against the tree rather than taken on report**, which matters for a ticket nobody
wrote code for:

| Claim | Where it is |
|---|---|
| The index exists and is partial | `services/core/migrations/000500_bids.up.sql` — `CREATE UNIQUE INDEX uq_bids_one_accepted_per_job ON bids (job_id) WHERE status = 'Accepted'` |
| It is *unique* and *restricted*, read back from the catalogue | `services/core/migrations/bids_test.go` reads `pg_indexes` and asserts both, rather than asserting the migration text |
| A second accepted bid is impossible in practice | Three insert-level cases in the same file, plus `TestOneAcceptedBidPerJobHoldsUnderARace` |

SHIP-91's *Done when* — "a partial unique index makes a second accepted bid impossible at the
database level" — is met literally and is tested. The table's own comment names both tickets.

**Two points move from "to build" to "already built", and one sentence changes for SHIP-92.** The
award branch (§8) opens against a constraint that exists, so SHIP-91 is a confirmation step at its
head rather than a migration: read the index back from `pg_indexes`, satisfy yourself it is the one
described here, and get on with the transaction. What it does *not* mean is that the award design
gets easier — the index does the locking as well (see above), and that is the thing SHIP-92's lock
ordering has to be designed against rather than around.

**`make status` will warn "declared done, but no commit subject names them" for SHIP-91, and that
warning is correct.** It is the same shape SHIP-57a has carried since wave 2: work that landed
inside a commit naming a different ticket. The script warns rather than fails for exactly this
case, and silencing it would mean writing a commit that claims work it does not contain.

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

### What SHIP-110 built, and why its two clocks are structural rather than conventional

`milestones`, migration `000601`, and no endpoint either — demonstrated by
`services/core/migrations/milestones_test.go`, on the same reasoning as SHIP-105 above.

**`milestones` is deliberately not `job_status_history`, and conflating them is the mistake that
table's header comment exists to prevent.** A history row is what the job's status *did*, and
`000402`'s guard will not accept a status change no row describes. A milestone is what an actor
*recorded*, and it may legitimately move nothing: `Docs/02` §3.1 requires a queued "Picked up"
arriving after "In transit" to be absorbed rather than rejected (SHIP-112), and a "Delivered"
recorded offline against an administrator's cancellation to be retained with its reason
(SHIP-113). Neither is a transition, so neither could be a history row —
`ck_job_status_history_moves` would refuse it outright, and the guard would have nothing to guard.
A milestone that *does* move the job produces two rows in one transaction saying two different
true things, and that pair is what makes an offline delivery reconstructable.

**The dual timestamps are structural, which is the one thing to check if you review only part of
this.** `000401` keeps its two clocks apart by convention — a `DEFAULT now()` and a guard that
never names the column — and that holds while one function does the writing. Milestones are
written from the driver portal, the mobile sync worker and the admin panel, so
`milestones.server_recorded_at` is `NOT NULL` with **no default** and a `BEFORE INSERT` trigger
that *refuses* an insert naming it and fills it from `now()` otherwise. Two columns are worth
nothing if a caller can write one value to both: that record would say the platform received a
milestone at the exact instant a device claims to have recorded it, which never happens and is
precisely what somebody backdating a delivery would write. The table is append-only besides, so
neither clock can overwrite the other afterwards either. A four-milestone offline batch synced in
one transaction is tested end to end: four recorded times, one arrival.

**An implausible actor time is recorded, not refused.** A device clock hours fast is evidence, and
`Docs/02` §3.1 asks for the update to be absorbed rather than rejected — refusing it would discard
work a driver actually did. There is no bound on `actor_recorded_at` and there should not be one.

**There is deliberately no uniqueness on `(job_id, milestone)`,** and the absence is a decision
with a test on it. A repeated *request* is stopped by the idempotency key (SHIP-15, SHIP-111),
which is a different thing from a repeated *milestone*: a driver who reaches a pickup, finds
nobody there and returns later records "En route to pickup" twice, and `Docs/02` §5 calls that
failed attempt an ordinary outcome. A unique index would refuse it, and would also refuse the late
arrival SHIP-112 exists to absorb.

**The five milestones are a delivery vocabulary, not the twelve job statuses.** `Docs/02` §2 says
so — "describing what a driver records, not constraining what the guard accepts" — and
`internal/delivery` declares its own constants rather than importing `jobs.Statuses`, which
domains may not do anyway. `ck_milestones_milestone` is paired with `delivery.Milestones` in both
directions per `Docs/10` §3.4, and the test also asserts what the constraint must *not* permit:
`'Completed'` is reached by seventy-two hours passing (`Docs/02` §6.1) and nobody records it. The
actor list is narrower than `job_status_history`'s for the same reason — `Docs/02` §3 permits the
awarded provider, their driver, an administrator with a reason, and the platform. **Not a
customer**: confirming a delivery is a status transition, not recording one. `actor_id` carries no
foreign key because it points at three different tables — a `users` row for a provider, a
`driver_assignments` row for a driver, and neither for an administrator — which is the convention
`000401` declared and this follows.

**One note for the wave reconciliation:** both test files are new files in a directory other
tracks also add files to, which is the established pattern (`jobs_test.go`,
`device_sessions_test.go`) rather than an edit to a shared surface. `milestones_test.go` reuses
`quotedLiteral` from `jobs_test.go`, since two package-level names cannot both be that and a
second identically shaped regexp is how the two drift.

### What SHIP-134 built, the dependency it took, and the two designs it rejected

The publisher. `outbox` and `internal/events` have existed since the foundation; §4 has carried
"the publisher, that is M5 and stays there" since wave 1, and this closes it. No migration —
`outbox` is `000004` and needed nothing.

**A new dependency: `github.com/segmentio/kafka-go` v0.4.51.** There was no Kafka client in
`go.mod`, and `go.mod` is a shared surface, so this was authorised deliberately rather than taken
opportunistically (`Docs/10` §9.2). Three things decided it. **Pure Go**, which rules out the
librdkafka bindings — cgo costs the static binary and complicates the Linux runner, and no
document has chosen a vendor. **Synchronous produce as the default**, which is the whole shape an
outbox needs: a call that returns after the broker has acknowledged, not a callback. And **one
module for producing, consuming and admin**, so SHIP-135 and SHIP-137 are served by the same
dependency rather than a second one. franz-go is faster and more actively maintained and would
have been the choice if throughput were the constraint; it is not, for a task draining a few
hundred rows every two seconds, and its produce path is a callback with a `ProduceSync` wrapper
around it. `go mod tidy` pulled `klauspost/compress`, `pierrec/lz4/v4` and the `xdg-go` SCRAM
chain with it. The client sits behind `EventPublisher`, one method declared in `outbox.go`, so
replacing it is a change to one file.

**The publisher lives in `cmd/worker`, not in `internal/events`.** Putting the reader beside the
writer was the obvious first answer and it is the wrong one, for the transitivity reason that
already produced the fourth boundary rule at SHIP-15c: **every domain imports `internal/events`**,
because every domain emits. A Kafka client imported from there is linked into `cmd/api` and into
every domain's test binary to serve one task in one binary none of them run. `internal/events/publish`
would have avoided that and needed no edit to `internal/boundaries` either — a subpackage of
registered infrastructure is classified by its first segment — and it was rejected for a smaller
reason: the drain is a claim loop, claim loops live in `cmd/worker`, and `checkClaim` is the check
that a claim query actually says `FOR UPDATE SKIP LOCKED`. Reaching it from another package means
exporting it or writing the query without it, and the second is the mistake `claim.go` exists to
catch. A platform adapter was never in the running: CLAUDE.md admits one only where a second
implementation exists today, and there is one Kafka.

**Publish, then mark, then commit — and it is the commit that carries the guarantee.** The row is
marked only after the broker acknowledges, and the mark becomes durable only at commit, which
leaves three outcomes and no fourth. The publish fails: the pass returns an error, `db.InTx` rolls
back, `published_at` is still NULL, the advisory locks are released, and the next pass finds the
same rows. That is the unreachable-broker case, and it is why the mark cannot come first. The
publish succeeds and the commit does not: the events are on the broker and the rows still look
unpublished, so they publish again — the at-least-once window `Docs/06` §4.0 names, and the reason
every consumer deduplicates on the event id. Or both succeed. **There is no outcome in which a row
is marked and never published**, and that asymmetry is the entire design: a duplicate is a solved
consumer problem, a lost award notification is not.

**The unit of division is the aggregate, not the row, and that is a correction to what SKIP LOCKED
alone gives you.** `000004` promises ordering per aggregate. A claim that only said
`WHERE published_at IS NULL ORDER BY occurred_at, id FOR UPDATE SKIP LOCKED LIMIT n` does not keep
it once two workers run, which a rolling deployment guarantees: worker A takes the oldest hundred
rows including job X's first two events, worker B skips A's locks and takes the next hundred
including job X's third, and if B reaches the broker first — it will whenever A's batch is larger
or its connection slower — job X's events arrive out of order and nothing reports it. A
transaction-scoped advisory lock per aggregate closes it, because the lock is atomic: B is refused
job X outright and moves to an aggregate nobody holds, so the work is still divided rather than
duplicated. The row lock is kept as well, and not as ceremony — it is what releases a dead
worker's claim.

**Where the qual goes is not a style question, and this is the finding worth carrying forward.**
The first version put `pg_try_advisory_xact_lock` in the claim's `WHERE`. It is shorter and it is
wrong, because **how many rows a qual is evaluated against is the planner's decision, not the
query's**. With the partial index driving an ordered index scan it runs on roughly `LIMIT` rows;
on a small table PostgreSQL prefers a bitmap scan and a `Sort`, which evaluates the qual on the
entire backlog before `LIMIT` applies — so one worker locks every aggregate in the outbox and a
second worker gets nothing. Correct, and a throughput collapse that appears only above a certain
table size. `TestOneAggregateBelongsToOneWorker` found it on five rows. The fix is two statements
with a `MATERIALIZED` CTE: the `LIMIT` is fixed before any lock is attempted, so exactly the
distinct aggregates among the oldest batch rows are tried and no others, whatever the planner
does.

**It creates no topics, deliberately.** SHIP-135 owns the topic set, and a topic conjured here
would take whatever partition count seemed reasonable — partitions can be added but never removed,
and adding one moves every key to a different partition, which silently ends the ordering promise
above. The broker refuses auto-creation too (`deploy/docker-compose.yml`). Until SHIP-135, a
publish to a topic nobody created fails the pass and leaves the rows claimable, which is the
correct direction to fail. `topicFor` is one line and is where SHIP-135 will change it:
`shipper.<aggregate_type>`.

**`Task.Close` was added at SHIP-15g for exactly this and is now used.** The producer is built in
the registration closure and released by `Close`, which the scheduler calls after the loop stops
on a fresh context bounded by `shutdownTimeout` — a flush inherited from the cancelled context
would be cancelled before it began. `kafka.Writer.Close` takes no context, so the wait is done in
`closeWriter`: a close that outlives the window is reported rather than waited on, because holding
a deployment open until SIGKILL discards the buffer the hook exists to protect. **No field was
added to `Deps`.**

**`make verify` printed 235 checks across 10 sections when this branch merged**, the tenth being
`scripts/verify/80-notifications.sh` — 80–89 is the notifications range. (Track A merged after it
and took the tree to the 268 across 11 that §3's headline carries.) It is a real end-to-end
run and not a restatement of the tests: it empties `shipper.job`, registers its own customer and
cancels a draft through the API so that a real endpoint emits one event, writes three more in a
committed transaction and one in a rolled-back one, runs the actual worker binary against the
actual broker, SIGTERMs it, and reads the messages back with Kafka's own console consumer. The
whole path is exercised — endpoint, transaction, outbox, worker, broker, consumer.

**Three things that section found, none of which a test could have.** First, **the console
consumer interleaves**: with three partitions it reads three independent streams, so the first
version of the comparison, which matched ordered lists, failed. That is the shortest available
demonstration that "ordering is per aggregate, not global" is a property of what was built rather
than a sentence in a comment. The check is now a set comparison plus a per-aggregate order check
over every aggregate on the topic — and a payload check that looked at "the first message" fell
to the same assumption a run later, so it now looks its event up by id.

Second, **cross-section state, for the second time in this harness's life.** The original
comparison was against `select id from outbox where published_at is not null` — every job event
the database had ever published — on the reasoning that the topic had just been recreated empty.
SHIP-68 broke it on the merge: that section runs the real worker to demonstrate job expiry, and
the worker runs *every* registered task, so it drains the outbox and marks job events published
before this section wipes the topic. Those rows are legitimately published and legitimately
absent from the fresh topic. **Nothing was lost and the product was right; the assertion was too
broad.** It had also been silently intermittent: on a machine where `shipper.job` did not yet
exist, SHIP-68's publisher passes failed against the missing topic, left every row claimable, and
the broad query happened to agree. The fix is a fence — `published_at > $outbox_fence`, taken
after the topic is recreated and before anything can publish — so the comparison covers exactly
what this section's worker produced. **SHIP-47's rate-limit bucket was the first instance of the
same shape**, and the recipe is the same both times: fence what you assert on, and own what you
assert about. The section now registers its own customer rather than reading 50-jobs.sh's token,
and its header says so for the next section that runs the worker or reads Kafka.

Third, a consequence of the second worth stating on its own: **`cmd/worker` is one binary and
every section that starts it starts every task.** SHIP-68's section is about job expiry and
publishes the outbox as a side effect. That is correct behaviour and it will keep surprising
people.

The transactional half is `services/core/cmd/worker/outbox_test.go`, against a real PostgreSQL
with the broker stood in for by a recorder: an event whose transaction rolled back is never
published, an unreachable broker leaves every row claimable and the next pass publishes them in
order, a crash between publishing and committing publishes the same event twice rather than
losing it, and one aggregate belongs to one worker while a second worker is still handed the
other.

### SHIP-15i — the wave-5 pre-step, and the two things it deliberately did not do

The fifth prep ticket, and the same argument as SHIP-15b, 15c, 15e and 15g one wave later: a
letter suffix marks work the plan assumed and no ticket owned. It is the **smallest** of the five —
three points, two mechanisms — and that is the interesting part. The four before it each closed
surfaces nobody had met yet, on an argument. This one closes two that a wave had already failed
in, with the failures counted.

| Surface | Before | After |
|---|---|---|
| The `make verify` check count | A hand-typed scalar in §3's prose. Conflicted in **four consecutive merges**; on three, no figure in the conflict was correct | Read out of this file at the end of a successful run and compared with what was counted. Wrong figure, failed run, correct value printed. `make verify-update` rewrites it |
| A done ticket's §3 summary-table row | Written by hand and forgotten by three of wave 4's four tracks. **Seven tickets** were missing rows going back to wave 3, past two reconciliation passes | `make status` fails, naming every ticket in `Docs/11-done.txt` with no row in a §3 summary table |
| `httpx.WriteError`'s unmapped 500 | The original error was discarded. The service recorded `status 500` and nothing about why | The cause is logged through `LoggerFrom`, bound to the request ID. **The response body is unchanged**, and a test holds it byte-for-byte |

**The count is checked rather than generated, and that is a decision.** Generating it would mean
owning the sentence it sits in forever, and the sentence is prose somebody reads. Comparing two
numbers costs one `grep` and catches all four of the merges that went wrong — it is the cheaper
half and it is the half that would have worked. The guard finds the figure by its **bold**, refuses
to run if it finds it more than once, and rewrites only the two numbers, which is the same shape as
`go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update` and `routes_golden.txt`.

**§3's summary table is checked and emphatically not generated.** §7 raised generating it from
`Docs/11-done.txt` and that is the wrong trade: the "What" column is one hand-written sentence per
ticket and it is the part of this document worth reading. Generating it would replace eighty
sentences with eighty ticket numbers to remove a defect a completeness check removes anyway. The
reader expands the row shapes the table actually uses — `SHIP-1…4` is a range, `SHIP-12, 13, 14` is
a list — and only the first cell counts, so a cross-reference in the "What" column does not
accidentally satisfy the check for another ticket.

**Both guards were demonstrated failing before they were believed.** The check count was set to a
wrong value and `make verify` refused with the measured one; `make verify-update` then wrote 268
across 11 and the file came back byte-identical to where it started. Two §3 rows were deleted — one
plain, one from a comma-list row — and `make status` named exactly SHIP-12, SHIP-13, SHIP-14 and
SHIP-134. The second guard then caught a real omission unprompted: adding SHIP-15i and SHIP-91 to
`Docs/11-done.txt` before writing their rows failed the check, naming both.

**`WriteError` logs the cause and the response says nothing, which is the whole point.** The error
contract is a published surface and its silence is deliberate — an unmapped error must still become
an opaque 500 with no internal detail. What was missing is that the *platform* was equally in the
dark: this cost wave 4's Track A an afternoon when a sign-in answered 500 against a stale local
database and the only route to the cause was reading the handler. The log record carries the cause,
the method and the path, and the query string is left out for the reason `Logger` leaves it out.
`r` may be nil and the logging survives it, as the request ID already did. Three tests, all
mutation-checked by removing the call: the log record, the byte-identical body, and the nil
request.

**Two things were considered and deliberately not done. Do not re-open either without a reason
this does not answer.**

**`internal/money` stays unwritten.** It is the last name registered in `internal/boundaries` with
no package behind it, and seeding it was the right call — but its only consumer is SHIP-74, which
sits behind SHIP-72 ← SHIP-58 ← X-9 ← X-4, and **X-4 has not started**. Writing it now would mean
guessing at a rounding rule and a currency type for a ticket nobody can start, and the whole value
of the pre-seeded list is that whoever needs it first writes it with no shared edit. That property
does not decay. `ratelimit` and `pagination` are the evidence it works.

**§3's table is not generated, per above.** Both refusals are recorded here rather than left
implicit, because the wave-4 notes recommended one of them and a reader who finds the
recommendation but not the refusal will do it.

## 4. Partly done — do not treat these as finished

| Ticket | Exists | Missing |
|---|---|---|
| **SHIP-149** | `audit_log` table, append-only triggers, tests | The Go write helper its title names |
| ~~**SHIP-134**~~ | ~~`outbox` table, `internal/events` writer~~ | **Closed.** The publisher landed — see §3. `outbox`, the writer and the drain are all in place; what remains is SHIP-135's topics and schema and SHIP-136's emission from the remaining domains, and those are tickets rather than a gap in this one |

**SHIP-65 has left this table.** Its *Done when* — "returns full job including budget" — was met
but for the budget for two waves, and SHIP-67 closed it with the column and the proof together.
§10's note that a ticket can be both done and partly done still stands; SHIP-149 is now its only
live example.

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

Strict build order says the next ticket is the lowest-numbered open one, which is **SHIP-24** — and it is blocked on X-2, as are the other three M0 stragglers. The lowest-numbered ticket that can actually be started is **SHIP-56a**. **Twenty-four tickets have every dependency met: 19 code tickets worth 66 points, plus the five Track-X tickets worth 10.** Build order is a preference rather than a constraint at this point. The list below is computed from `Docs/09`'s dependency column against `Docs/11-done.txt`, not maintained by hand — and it is the **complete** startable set, because an earlier version of this table was a curated selection that read like a full list.

**SHIP-91 has left this table**, closed as delivered by SHIP-80 on the owner's ruling of 12 August 2026 (§3). Nothing became startable in its place: SHIP-92 is the only ticket that depended on it and it also depends on SHIP-88, which is not built.

| Ticket | Pts | Area |
|---|---|---|
| SHIP-56a | 2 | Status codegen for Go, Dart and TypeScript — cut from waves 2, 3 and 4 because it writes into four trees |
| SHIP-69 | 2 | The expiry warning forty-eight hours ahead — `cmd/worker`'s second task, and SHIP-68 unblocked it |
| SHIP-70 | 2 | Extend an expiring job — an ordinary `UPDATE`, because `000406`'s trigger fills `expires_at` only when it is NULL |
| SHIP-77 | 3 | Flutter customer job detail — SHIP-76's list now needs somewhere to tap through to |
| SHIP-79 | 3 | Provider service area and specialties — the capability vocabulary SHIP-78 was careful *not* to be |
| SHIP-98 | 5 | Flutter provider fleet management — the client half of SHIP-78's six routes |
| SHIP-106 | 3 | Assign driver endpoint — SHIP-105 left it the question of who may assign at all |
| SHIP-111 | 5 | Milestone update endpoint — SHIP-110's table, plus the idempotency key and the actor permissions |
| SHIP-135 | 3 | Kafka topics and event schema — **`topicFor` is the one line it changes**, and §9 parks the dead-letter question on it |
| SHIP-163 | 3 | Dispute intake endpoint — dependencies met since SHIP-57 |
| ~~SHIP-114~~ | 5 | **Dependencies met, not buildable** — `internal/platform/storage/` is still `doc.go` alone and compose still has no object storage; see below |
| ~~SHIP-124~~ | 5 | Buildable, but it is `core/queue` for offline delivery — **M4 client work**, three milestones ahead of the endpoints it would queue against |
| ~~SHIP-147~~ | 5 | Buildable, but it edits the `newRouter` middleware chain — **shared-platform work**, not a track slot |
| ~~SHIP-168~~ | 3 | Buildable, but its store link does not exist until X-2/X-3 publish listings |
| ~~SHIP-169~~ | 3 | Buildable, but it touches `users`, in the **shared migration block (1–99)** |
| ~~SHIP-174~~ | 3 | **Not demonstrable** — still no Datadog agent in compose, no `DD_*` configuration, no account |
| ~~SHIP-178~~ | 3 | **Not demonstrable** — install base comes from App Store Connect and Play Console (X-2, X-3) |
| ~~SHIP-182~~ | 5 | **Not demonstrable** — there is no production and no managed backup |
| ~~SHIP-183~~ | 3 | A **decision ticket** — §9 parks the per-account-lockout question here; it also rewrites limits on every domain's routes |

Plus **X-1, X-3, X-4, X-5 and X-6**, none of which is code and none of which has started. X-5 and X-6 need no third party at all.

**Nine of the 19 are struck, which is the useful signal in this table.** Dependencies being met is not the same as a ticket being startable: four are not demonstrable with the tooling that exists, three are shared-platform work a domain branch must not do, one is a decision, and one is in a milestone three ahead of the live one. Each of the nine reasons was re-checked against the tree in the wave-4 pass rather than carried over — `internal/platform/storage/` is still `doc.go`, `deploy/docker-compose.yml` still has no object storage, and `internal/config` still has no `DD_*` fields. **The startable-and-sensible set is ten tickets and 31 points**, which is smaller than the wave-4 slate was and is the first sign that the queue now needs the M3 endpoints opened rather than more foundations.

**~~An open question wave 4 raised and deliberately did not answer: is SHIP-91 already delivered?~~ Answered by the owner on 12 August 2026: yes, and it is closed.** The question is kept rather than deleted because the *way* it was handled is the reusable part. Track C found that SHIP-80's `uq_bids_one_accepted_per_job` already met SHIP-91's *Done when*, recorded the finding, and declined to claim the ticket — which was right, since `Docs/11-done.txt` names what a commit subject claimed. Leaving it visible and unanswered is what stopped it being settled implicitly by whoever picked the award branch up. **The general rule: a track that finds it has finished somebody else's ticket writes the finding down and claims nothing.**

**SHIP-114 is neither ready nor blocked on a third party, which is a third category this file needed.** Its dependencies are met, but `internal/platform/storage/` is `doc.go` alone and `deploy/docker-compose.yml` has no MinIO or equivalent, so "receives a short-lived pre-signed URL and uploads directly" cannot be demonstrated. Wave 1 already paid once for counting a ticket whose acceptance criterion needed a tool nobody had installed. **It needs a lettered ticket adding object storage to the local stack first**, as shared-platform work.

**The identity bottleneck is gone and M1 is closed, so `internal/identity` constrains nothing.** Seven of the eight tickets that lived in the package landed in wave 3, and SHIP-50 and SHIP-55 closed the milestone from the client side in wave 4 — both written *against* the package rather than in it. `Docs/10` §9.1 still gives one package directory to one agent at a time; no queued ticket needs to open `internal/identity` at all. The two that eventually will are SHIP-183, which is a decision before it is a change, and SHIP-169, which is struck above for touching `users` in the shared migration block. **The constraint has moved to `internal/jobs` and `internal/bidding`**: SHIP-69, SHIP-70 and SHIP-77 all read the jobs domain, and SHIP-91…95 own `bidding` outright and never parallelise (§8).

**Public routes still share the anonymous idempotency scope, and that remains safe.** `replayOrRefuse` fingerprints method, path and body, so reading another caller's stored response requires sending their exact request — which, on every route on `Docs/10` §4.1's allow-list, means already holding the secret material in their body. `make verify` checks the anonymous scope still works, because scoping idempotency into uselessness would be a subtler regression than leaving it shared.

## 7. Wave 4 — what landed

One ticket serially, then four tracks concurrently — one more track than wave 3 ran. **Eleven tickets, thirty-nine points, all delivered, no trim taken**, and **M1 closed**.

| Step | Tickets | Landed |
|---|---|---|
| **Pre-step** (serial, primary tree) | SHIP-15g | Merged at `09071d4` before any track started |
| **Track A** mobile | SHIP-50, 55 → SHIP-71, 76 | All four, two sequential runs on one branch |
| **Track B** jobs | SHIP-67, 68 | Both, one run |
| **Track C** fleet and bidding | SHIP-78, 80 | Both, one run |
| **Track D** delivery and notifications | SHIP-105, 110 → SHIP-134 | All three, two sequential runs on one branch |

Seven agent runs in total. The exit criterion was: M1 closes on a real device; a customer's budget exists and cannot reach a provider; an Open job expires on a deadline the database sets; the M3 and M4 tables exist with their constraints; and the outbox actually publishes to Kafka. **All of it holds**, and `make verify` went from 208 checks across 9 sections to **268 across 11**.

### Three domains opened, and none of them reached across a boundary

`fleet`, `bidding` and `delivery` had held `doc.go` and nothing else since SHIP-10. All three now hold code, which takes the count of domains with logic in them from two to five, and the rule that a domain never imports another had its first week of real pressure. `internal/delivery` declares its own five-milestone vocabulary rather than importing `jobs.Statuses`, even though the two look similar enough that conflating them is the mistake `000601`'s header exists to prevent. SHIP-134 kept its Kafka client out of `internal/events` for the transitive reason that produced the fourth boundary rule at SHIP-15c — every domain imports `events`, so a client imported there is linked into every domain's test binary to serve one task in one binary none of them run. **Nothing shared was edited that was not reserved per domain**, and `Deps` gained no field in the entire wave.

### The wave's real lesson: three guards fired for the first time, and all three worked

Each of these was built ahead of the code that would need it, on an argument rather than on evidence. Wave 4 is the first wave where the evidence arrived.

| Guard | Built at | What happened |
|---|---|---|
| The out-of-order migration guard | SHIP-15g | Fired on SHIP-78's very first `make migrate-up`. Fleet draws from block 300–399 against a database at `000404`, which is exactly the case SHIP-15g predicted would be hit "days from now rather than theoretically". The refusal named the migration, its domain and the one-line fix, and needed no interpretation |
| The budget source-parsing test | SHIP-67 | `TestOnlyTheOwnersResponseCarriesTheBudget` parses the package's own source and refuses a budget field on any shape outside a four-name allow-list. It replaced a `make verify` line asserting the *absence* of the key, which is the weaker tripwire, and it is now what SHIP-82 and SHIP-83 will meet as a failing test rather than as a shell assertion |
| The one-accepted-bid partial index | SHIP-80 | `uq_bids_one_accepted_per_job` held under a real race, and the index turned out to do the *locking* as well — two transactions writing the same key into a btree do not race, the second blocks and is then told the answer. That is the thing SHIP-92 has to be designed against |

The counterpart is worth recording beside them: **SHIP-80's migration did *not* trip the guard**, because bidding's block 500–599 sits above `000404`. Both directions are now observed on real work rather than on a hand-made reproduction, which is what SHIP-11 asks of any mechanism this file claims exists.

### What conflicted, and the one shape that keeps conflicting

Nothing collided in code. Three things needed resolving at merge time and all three were in this file or in the verify harness.

**The `make verify` check count, for the fourth consecutive merge.** It is a hand-maintained scalar in prose, which is the one shape a merge cannot resolve, and **in three of those four merges no figure in the conflict was correct**. It is measured, never reconciled — but four repeats is a mechanism asking to exist rather than a habit asking to be kept, and §9 now carries it as an open recommendation: `scripts/verify-foundation.sh` already prints `%s checks passed across %s sections`, so something should write that into this file rather than a human retyping it.

**SHIP-134's verify section, broken by SHIP-68's.** Not a textual conflict at all — SHIP-68's section runs the real worker binary to demonstrate job expiry, and `cmd/worker` is one binary, so it runs *every* registered task and drains the outbox as a side effect. SHIP-134's comparison was against every published job event the database had ever held, which was legitimately no longer the right set. Nothing was lost and the product was right; the assertion was too broad. It is fenced now on `published_at > $outbox_fence`. **The structural half is in §9**, because the next section that starts the worker will meet it again.

**Three of the four tracks landed tickets without adding their §3 summary-table row.** Track D for SHIP-105 and SHIP-110, Track A for all four of its tickets, and Track B for SHIP-67 and SHIP-68; only Track C wrote its own. Nothing was lost in a merge — the rows were simply never written, because an agent writes the prose subsection that explains its work and forgets the index table above it. Track D's and Track A's were caught during merge resolution; Track B's were not, and this pass also found the defect goes back further than wave 4 — SHIP-47, 64, 65 and 66 from wave 3 had no rows either, nor did SHIP-15g. **Seven rows were added in this reconciliation**, which is the largest single correction §3 has needed. Two conclusions, and they are both for wave 5: put the requirement in *every* track brief explicitly rather than assuming the pattern is obvious from the file, and treat §3's summary table as a candidate for being **generated from `Docs/11-done.txt`** — for exactly the reason the check count should be. A hand-maintained index of a hand-maintained list is two things to forget instead of one. **Both were acted on at SHIP-15i, and the second went the other way**: `make status` now *checks* that every done ticket has a row and generates nothing, because the "What" column is the hand-written part worth keeping. See §3.

### What the wave cost in scale

Wave 1 delivered 32 points, wave 2 delivered 37, wave 3 delivered 48, and wave 4 delivered **39 across four tracks**. That is a deliberate step *down* in points and *up* in concurrency: the wave's risk was four tracks rather than three, in four domains rather than two, and it was not worth also carrying wave 3's volume. Seven agent runs, no trim, no conflict in code.

## 7a. Wave 3 — what landed

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

## 7b. Wave 2 — what landed

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

## 7c. Wave 1 — what landed

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
- Tracks do not touch **§1, §2, §6 or §7** of this file — several agents doing the same arithmetic on one table is a guaranteed conflict, and `make status` computes the real numbers anyway. Each track adds its own tickets to §3 and the done list; the rest is reconciled once when the wave lands, as a separate pass. Wave 2 did exactly that and it worked, and wave 4 held under four tracks. **What a track must not skip is its §3 summary-table row**, which three of wave 4's four tracks did — the prose subsection is written and the index above it is forgotten. Say so in the brief.

## 8. Hard gates ahead

**~~SHIP-44 is a choke point.~~ Cleared — see §3.** `httpx.Idempotent` is wired with `httpx.SubjectScope`, and the freeze on authenticated state-changing endpoints is lifted. `make verify` demonstrates the separation against a running service rather than asserting it.

Kept here rather than deleted, because the shape recurs: this was described only as a constraint on *other* work, and so was never read as work itself while its dependencies had been met since wave 1. A gate with satisfied dependencies belongs in §6 the moment it becomes buildable.

**The next gate of the same kind is SHIP-108, and it has no entry yet.** The driver's job-scoped token is a second verifier, and `Docs/10` §5 requires that neither token system can be exchanged for the other. `identity.AccessTokenVerifier` refuses the driver audience today and there is a test for it — but the other direction cannot be tested until the driver verifier exists. **Whoever writes SHIP-108 writes both directions**, which is the reason this file has always kept both verifiers with one owner.

**One thing SHIP-44 did not do: `RequireDriverToken` and `RequireAdmin` are declarable and unenforced.** A route declaring either now panics at startup rather than being served open, so the failure direction is safe. SHIP-108 and SHIP-147 supply the middleware.

**SHIP-92…95 never parallelise.** Own branch, nothing else on it. The lock ordering, the idempotency interaction and the race tests are one design; two people produce two lock orderings, which is a deadlock or a lost update. Consider using a second agent adversarially instead — one implements 92–94, another writes SHIP-95 from `Docs/02` §3 and `Docs/08`'s four named races *without reading the implementation*.

**SHIP-91 is out of that branch entirely, and this is settled rather than open.** The partial unique index was built by SHIP-80 (`uq_bids_one_accepted_per_job` — see §3), which is exactly what `Docs/09`'s note beside SHIP-91 asks for, since it is the one piece far cheaper before the endpoint than after it; the owner declared the ticket delivered on 12 August 2026 and it is in `Docs/11-done.txt`. **The branch starts against a constraint that already exists.** Confirm the index is the one §3 describes and move on — do not write a migration for it, and do not treat its absence as possible.

**What the constraint does not do is make the award easier.** The index also does the locking: two transactions writing the same key into a btree do not race, the second blocks and is then told the answer. That is the behaviour SHIP-92's lock ordering has to be designed *against*, and it is the reason this branch still gets an owner of its own despite starting two points lighter.

**Also single-owner, for reasons in `Docs/10`:** SHIP-57 (the status guard), SHIP-67 with SHIP-83 (budget privacy — test the serialised response, not struct fields), both token verifiers, and the middleware ordering in `newRouter` — which is now load-bearing in a second way, since `ResolveSubject` sitting outside `Idempotent` is what makes the scope work at all.

**~~The SHIP-67 / SHIP-83 pairing cannot be honoured in one wave.~~ Settled at SHIP-67: built now, SHIP-83 reserved to the same owner.** The fact about the dependency graph has not changed — SHIP-83 depends on SHIP-82 → SHIP-81 → (SHIP-79, SHIP-80) → SHIP-78, and wave 4 delivers only SHIP-78 and SHIP-80, leaving three hops. What has changed is that the choice the pairing forced has been made rather than deferred again.

**The decision, and the reasoning it was made on.** Deferring both would have left the column unbuilt for a rule it already satisfies, and SHIP-65's *Done when* incomplete for a third consecutive wave, in exchange for a test against an endpoint that does not exist. So SHIP-67 landed with the strongest proof available today, which turned out to be three tests rather than one — the source-parsing test that refuses a budget field on any shape but the owner's response, a wire test over every response a provider or a stranger can obtain, and a test on the stored event payload. §3 has the detail. ~~**SHIP-83 remains reserved to this owner and adds the fourth**: its provider response, serialised, asserted to carry no budget. That is the test the pairing was actually for, and it is the one thing that is still owed.~~

**~~Still owed.~~ Paid at SHIP-83 — see §3.** `TestTheProviderResponseCarriesNoBudgetInAnyForm` is the fourth proof: the provider's response obtained over HTTP through the real handler, asserted on the raw bytes across all three ways a provider can obtain a job. **It turned out to need to be stronger than the entry asked for.** "Asserted to carry no budget" reads as a search for the field, and a search catches `budget_cents` and misses `max_price` — so the test holds the response to a **closed set of keys** instead, checks the value with identifiers stripped out, and refuses to run at all against a fixture whose budget is NULL. Verified by mutation in both directions: a field named `max_price` passes the source-parsing guard and fails this one; renamed `budget_cents`, it fails both.

**The `make verify` tripwire has been moved, and this is the entry recording it.** The check asserting that no `budget` key was present is gone; what replaced it asserts the owner reads their own budget back and that no provider-facing or stranger-facing response mentions it in any form. **The tripwire is now the source-parsing test rather than a verify line** — whoever writes SHIP-82 or SHIP-83 will meet it as a failing test the moment a provider shape acquires the field, which is earlier and louder than a shell assertion would have been.

**That prediction held exactly, and SHIP-83 found the one thing it does not cover.** The source-parsing guard did fire first, and `internal/fleet`'s copy reached SHIP-82's and SHIP-83's new response types with no change to it — they are non-test files in the package it parses. What it cannot do is refuse a budget under a name that is not "budget", because it reads source and can only match a spelling. SHIP-83's serialised-response test is the axis it lacks, and `make verify` now makes the same closed-key-set assertion from outside Go, so neither can be quietly deleted alone.

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

**~~Biometric unlock is still open.~~ Decided at SHIP-55 — see §3. Out of the MVP, with three named triggers that would reopen it.** The short version: it is a convenience over a token the device passcode already gates, `AndroidOptions.biometric()` needs API 28 against this app's floor of 24, and an optional control needs a settings surface that does not exist before SHIP-173. The reopening triggers are the Android floor moving at SHIP-24/26, a settings screen existing, or the pilot holding something that makes an unlocked handset a real exposure. **`Docs/07` §9's table row and its closing paragraph both record the decision now**, corrected in this reconciliation pass — SHIP-55 owned `apps/mobile/**` and this file, so it could not reach them. `Docs/07` §3's position row is untouched, because it describes where biometric unlock *sits* rather than whether it ships.

**~~§10's done block should probably be `merge=union`, and §3 probably should not.~~ Decided and done at SHIP-15e — see §3.** Both halves were kept: the list is `merge=union` and §3 is not. Since a git attribute applies to a whole file, the list moved to `Docs/11-done.txt`, one ticket per line — which the recommendation had not noticed matters, because a union resolves line by line and the old block put several tickets on one line.

**~~`scripts/verify-foundation.sh` is the sixth shared surface, and it has no include mechanism.~~ Decided and split at SHIP-15e — see §3.** It is a harness plus one file per milestone or domain in `scripts/verify/`, numbered in reserved ranges the way migrations are, and a track adds a file rather than editing one. The count was unchanged at 105 across the split, which is the evidence the move lost nothing. **That 105 is a historical figure, not today's** — wave 3 took it to 208; §3 carries the current count.

**~~`device_sessions` has no expiry column.~~ Decided and built at SHIP-39 — see §3.** An explicit `device_sessions.refresh_token_expires_at`, `NOT NULL` with no default, in migration `000103`. The window **slides** — rewritten on every rotation, 30 days — so inactivity ends a session and daily use never does. **A Redis TTL was rejected** (`Docs/10` §5: a control a cache flush undoes is not one), and so was deriving expiry from `last_seen_at + TTL`, because that is a *display* column which SHIP-46 writes from a device-list **read** — a derived lifetime would mean every future write silently extends a credential. **No absolute session cap, deliberately**: that is a policy control with a product consequence rather than a mechanism, and it is another column and another migration whenever it is wanted.

**Signing out on the device does not end the session on the platform, and SHIP-50 is what makes fixing it possible.** `SessionController.signOut` clears the Keychain and the in-memory access token; `POST /v1/auth/logout` (SHIP-43) is never called, so the refresh token it just discarded stays valid server-side for up to thirty days and the device keeps a row in `GET /v1/auth/sessions`. It was out of scope for SHIP-50 and SHIP-55 — neither *Done when* mentions it, and until SHIP-50 the client had no access token to authenticate the call with. It is now a handful of lines: a fire-and-forget call before the local clear, which must not block or fail the sign-out (`Docs/07` §3 is explicit that the device catching up is what this is). **No ticket owns it.** SHIP-143 is the nearest — "de-registers on sign-out" — and would be a reasonable home, or a small follow-up of its own.

**~~`httpx.WriteError` discards the cause of an unmapped error.~~ Decided and built at SHIP-15i — see §3.** The fallback branch now logs through `LoggerFrom`, so the record carries the request ID the middleware bound. **The response body is unchanged and a test holds it byte-for-byte**, which was the constraint that made this worth doing carefully rather than quickly: the contract's silence on an unmapped 500 is deliberate, and an observability fix that became a disclosure one would be a worse defect than the gap. `r` may be nil, as `WriteError`'s own request-ID guard already assumed, and the logging survives that. The original entry read "whoever next touches `internal/httpx` should take it" — which is a recommendation with no owner, and it was still here two waves later; a prep ticket is what that shape actually needs.

**`device_label` is a platform name and a version rather than a model name.** `core/device/device_label.dart` sends `iOS 17.0` or `Android 14`, which is what `dart:io` can answer. "iPhone 15 Pro" needs `device_info_plus` — a package decision with two native integrations and a store data-safety consequence, deliberately not taken inside a two-point ticket. The field is display text for the device list (SHIP-46) and two handsets may legitimately share a label, so nothing is broken; it is simply less useful than it could be. One function changes when somebody adds the package.

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

**The outbox has no dead-letter path, so one event the broker will never accept stops the
aggregates in its batch.** SHIP-134 fails the whole pass when any event in it fails to publish,
which is right for the case that actually happens — the broker is unreachable, and every row must
stay claimable — but it means a permanently unacceptable event, one over the message-size limit
say, blocks its batch until somebody looks. The pass logs the oldest event's type and id, so it is
findable rather than mysterious. Three ways out and none of them is this ticket's: bound the
payload where the domain writes it, which is the cheapest and probably the right one; park a row
after N failed attempts, which needs a second state column and `000004` says `published_at` is the
publisher's only state; or publish per event and mark per event, which trades the blockage for
more duplicates. **Decide with SHIP-135**, which is where the schema and the size of a payload get
settled anyway.

**~~The `make verify` check count is a hand-maintained scalar in prose.~~ Decided and built at
SHIP-15i — see §3.** Both halves of the recommendation were taken, and the second was answered in
the opposite direction to the one it proposed.

The count is **checked, not generated**: a successful `make verify` reads the figure out of §3,
compares it with what it counted, and fails with the measured value printed. `make verify-update`
rewrites it. The recommendation had named the reasoning itself — "the check is much cheaper than
the rewrite if the goal is only to stop the number being wrong, and it is the half that would have
caught all four" — and the argument that settled it is that the figure sits in a sentence somebody
reads, so generating the line would mean owning its wording forever. The guard finds it by its
bolding and refuses to run if it finds the bold form twice.

**§3's summary table is *not* generated**, and that half was refused rather than deferred. Its
"What" column is one hand-written sentence per ticket and is the part of this document worth
reading; generating it from `Docs/11-done.txt` would replace eighty sentences with eighty ticket
numbers to remove a defect a completeness check removes anyway. `make status` now fails, naming
every done ticket with no row in a §3 summary table. Both guards were demonstrated failing before
they were believed, which is what SHIP-11 asks of any mechanism this file claims exists.

**`cmd/worker` is one binary, so every verify section that starts it starts every registered
task.** SHIP-68's section demonstrates job expiry by running the real worker binary, which also
drains the outbox — which is what broke SHIP-134's section the moment the two met at merge (§3,
§7). That section is fenced now and the fence is correct, but **the condition is structural rather
than a defect in either section**, and it gets sharper with every task registered: SHIP-69's expiry
warning, SHIP-89's bid expiry and SHIP-119's auto-complete are all queued behind it. Three ways
out and none of them owned: a task selector on the binary, so a section starts only what it is
demonstrating; a convention that every section fences what it asserts on, which is what both
instances resolved to and is much the cheapest; or accepting it and saying so in every section
header. The middle one is already the recipe SHIP-47's rate-limit bucket produced — **fence what
you assert on, and own what you assert about** — and it may well be enough on its own. **Decide
before the fourth task registers**, not after the third section breaks.

**No Kafka topics are created, and until SHIP-135 that is a live failure mode rather than a
theoretical one.** SHIP-134 deliberately creates none, and the reasoning is sound: partitions can
be added but never removed, and adding one moves every key to a different partition, which silently
ends the per-aggregate ordering the publisher's advisory-lock design exists to provide. The broker
refuses auto-creation as well. So a publish to a topic nobody created fails the pass and leaves
every row claimable, which is the correct direction to fail — and it has already been seen, as an
intermittently passing `make verify` on a machine where `shipper.job` did not yet exist. **`topicFor`
is the one line SHIP-135 changes**, to `shipper.<aggregate_type>`. What is genuinely undecided is
not that line but *what creates the topics* — a migration-like step in the repository, the compose
stack for local work, or a deployment action for anything else — and whether local and deployed
answers may differ. **Decide with SHIP-135**, beside the schema and the dead-letter question above.

## 10. The done list, in a form a script can read

**The list is `Docs/11-done.txt`**, one ticket per line. It is still authoritative and it is
still updated in the same change that finishes a ticket — it has simply moved out of this
document. `make status` reads it, counts it against the backlog, and cross-checks it against
what commit subjects claim.

A ticket belongs there only when its *Done when* line in `Docs/09` is demonstrable. **Every ticket §4 names is now in the list**, and SHIP-149 is the only row left in it — which is
not a contradiction to be tidied away.

**A ticket can be both**, and this is the shape: it landed, it is named by a commit subject, and one
clause of its *Done when* belongs to a ticket that does not exist yet. SHIP-149 shipped the
append-only `audit_log` and its triggers; the Go write helper is still missing. SHIP-65 was the
other example for two waves — the job detail endpoint and the owner-only rule shipped, the `budget`
field its sentence also names did not, because adding the column before the proof it cannot leak
would have been exactly the wrong order. **SHIP-67 closed it**, which is what this shape is supposed
to end in.

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
