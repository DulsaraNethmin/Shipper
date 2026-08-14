# Shipper — Delivery Status

**Status:** Living document — update it in the same change that finishes a ticket  
**Audience:** Anyone picking the work up, including a session with no prior context  
**Purpose:** Say where the work actually is, so nobody has to reconstruct it from git.

## Read this first

`Docs/09` says what the work *is*. This says what is *done*. They are separate files on purpose: the backlog is a plan that rarely changes, and this changes every few days.

`make status` prints the machine-checkable half — which tickets have a commit claiming them. It cannot see nuance, so **this file is authoritative** for anything a commit subject does not capture: partly finished tickets, external blockers, and what is safe to start next.

**Last updated:** 2026-08-13, at the **wave-7 reconciliation** — the arithmetic and the narrative brought back into line after fourteen tickets landed across four tracks. It corrects §1, §2 and §6, writes wave 7 up as the new §7 with the six earlier waves renumbered behind it, closes the §8 and §9 items wave 7 settled, adds the eight it found, and moves the recipient-name gap SHIP-118 recorded into §4 where documentation ahead of code belongs.

**No wave-8 pre-step sits on top of it yet.** The six passes before this one alternated — a reconciliation, then a pre-step cut from it rather than from `develop`, so that adding a ticket to the backlog moves §1's arithmetic once instead of twice. That ruling has held for `15f`→`15g`, `15h`→`15i`, `15k`→`15m` and `15n`→`15p`, and it stands for whoever cuts wave 8's. §6 and §9 are where its paragraphs are being written now, one at a time, by whoever hit the surface first.

**This pass is not a backlog ticket**: like `ship-15d`, `ship-15f`, `ship-15h`, `ship-15j`, `ship-15k` and `ship-15n` before it, a reconciliation branch names no ticket in its subject and appears in neither `Docs/09` nor `Docs/11-done.txt`. The pass beneath it is the other kind — **SHIP-15p, the wave-7 pre-step**, which sat on top of the wave-6 reconciliation and was cut from it rather than from `develop`. It added §1's and §3's SHIP-15p entries, un-struck SHIP-114 in §6, closed the two §9 items it settled, and moved the `make verify` count to what it measured; it appears in both files.

---

## 1. Snapshot

| | Tickets | Points |
|---|---|---|
| **Done** | 121 | 365 |
| Remaining | 87 | 280 |
| **Total** | 208 | 645 |

The totals grew by two tickets rather than shrinking: SHIP-15c and SHIP-23a were added to `Docs/09` during wave 2, both work the plan assumed and no ticket owned.

They grew again by one in wave 3: **SHIP-15e** (M0, 5 points), the serial pre-step that split `scripts/verify-foundation.sh` into a harness plus one file per domain and moved the done list out of this file. Totals moved 203 → 204 tickets and 626 → 631 points.

And once more before wave 4: **SHIP-15g** (M0, 5 points), which took 205 tickets and 636 points. Same pattern a fourth time — a shared surface two different tracks had each parked work against, plus a defect neither could fix from a domain branch. **This is now the norm rather than the exception**, and the useful reading is that a wave costs one prep ticket: the pre-step is not overhead the process has failed to eliminate, it is the process. Budget for the next one rather than being surprised by it.

**Wave 5's is SHIP-15i** (M0, 3 points), taking the totals to 206 tickets and 639 points — and it is the first of the five to be *smaller* than the wave before it. Two mechanisms rather than four, because the surfaces wave 5's tracks would meet in are now known well enough to name precisely: the `make verify` count and §3's index, both of which conflicted or were forgotten in wave 4. `internal/money` was considered and deliberately left unwritten; the reasoning is in §3.

**Wave 6's is SHIP-15m** (M0, 3 points), taking the totals to 207 tickets and 642 points — the sixth prep ticket, and the second in a row to be three rather than five. The wave-5 reconciliation said there was no wave-6 prep ticket *yet*, and that a prep ticket gets written into §9 one paragraph at a time by whoever hits the surface first. That is exactly what happened: **four of the five paragraphs wave 5 left in §9 are struck by this ticket** — the Kafka worktree isolation, the two rules `CLAUDE.md` should carry, and `KAFKA_REPLICATION_FACTOR` — and only the cross-block index is left, which §9 itself calls not urgent. The ticket's *blocking* item came from somewhere else again: §8's `RequireDriverToken` gate, which is a shared-surface edit no domain branch may make and which SHIP-108 is unbuildable without. §6 adds a further candidate in object storage, which SHIP-114 has now been struck for through four consecutive waves.

**Wave 7's is SHIP-15p** (M0, 3 points), taking the totals to 208 tickets and 645 points — the seventh prep ticket, and the third in a row at three rather than five. Its blocking item is the one §6 has been naming for five waves rather than one wave 6 left behind: **there was no object store in the local stack**, so SHIP-114's "a client receives a short-lived pre-signed URL and uploads directly" could not be demonstrated, and every file that would fix it — `deploy/docker-compose.yml`, the root `Makefile`, `internal/config`, `deploy/.env.example`, the CI workflow — is one a domain branch may not edit. **It is also the first prep ticket to have asked**: §9 concluded after three waves of absorbing parked `internal/config` requests that a prep ticket should ask each track at dispatch what configuration it expects to need, SHIP-15m did not ask, and wave 7 did. The `Storage` section is that answer, written before the track opened rather than a wave after it.

**One of the 121 was closed by a ruling rather than by work.** SHIP-91 was declared delivered by the owner on 12 August 2026, met by SHIP-80's partial unique index rather than built separately — see §3 and §6. It is why `make status` reports it, along with SHIP-57a, as declared done with no commit subject naming it; both are correct and neither is wishful.

| Milestone | Done | Points |
|---|---|---|
| **X** External | 0 / 9 | 0 / 26 |
| **M0** Foundation | 33 / 37 | 87 / 101 |
| **M1** Identity | 28 / 28 | 78 / 78 |
| **M2** Jobs | 18 / 26 | 54 / 78 |
| **M3** Bidding and award | 19 / 27 | 66 / 95 |
| **M4** Delivery | 16 / 29 | 60 / 101 |
| **M5** Notifications | 2 / 13 | 8 / 45 |
| **M6** Admin | 2 / 20 | 6 / 65 |
| **M7** Hardening | 3 / 19 | 6 / 56 |

**Every cell above was recomputed from `Docs/09`'s rows against `Docs/11-done.txt` rather than
adjusted from the last pass**, and the nine ticket counts agree with `make status` exactly. The
points column `make status` does not print, so it is the half worth deriving twice: M3 gained
SHIP-92, 93, 94, 95 and 100 at 5 + 3 + 3 + 5 + 3 = 19 over wave 6's 47, and M4 gained SHIP-114, 115,
116, 118, 120, 126 and 129 at 5 + 3 + 3 + 3 + 5 + 2 + 3 = 24 over wave 6's 36. **Re-derive rather
than copy.** The wave-6 pass caught two wrong cells this way — the wave-7 dispatch brief circulated
M3 at 51 and M4 at 34, which sum to 85 against a true 83 — and this pass found no wrong cell, which
is the first time. A number that disagrees with `make status` came from somewhere else and is wrong.

**M0 is 33 of 37 because SHIP-15p landed, and the four left are not code.** SHIP-24…27 are store signing and upload, blocked on X-2 and X-3 — unmoved by wave 7 and unmovable by any wave, because nothing in this repository can reach them. Every buildable M0 ticket is done, and **SHIP-15p** is now the last one added — like SHIP-23a, SHIP-15e, SHIP-15g, SHIP-15i and SHIP-15m before it, it was a recommendation in §9 and §6 before it was a ticket. **That is six of the seven lettered M0 tickets added since the backlog was written** — SHIP-15c, 15e, 15g, 15i, 15m, 15p and 23a, of which only SHIP-15c was not a recommendation first — and the count is spelled out because an earlier version of this sentence said "five of the six" and left the reader to work out which six. It is worth reading as a mechanism rather than a coincidence: this file's §9 and §6 are where the next prep ticket is written, one paragraph at a time, by whoever hits the surface first.

**Wave 7 has landed in full.** Four tracks, fourteen tickets, **forty-nine points**, thirteen agent runs, **no trim taken**. The award transaction was built, raced and proved, three waves after it first became the backlog's most-deferred piece of real work; a photograph now reaches an object store the API is not in the path of, becomes evidence for one recorded milestone, and `Delivered` stopped being a status nothing could reach; the driver portal gained its first product code; and the provider's half of the app can place a bid and record a milestone. It is the largest wave both by points and by ticket count. §7 has the detail.

**M1 is complete — 28 of 28 tickets, 78 of 78 points.** A person can register, receive an email token and a phone OTP, confirm both, sign in, and stay signed in across app restarts on a real handset, with the role fixed at registration and immutable afterwards by a database trigger rather than by application logic. The platform half was finished in wave 3 — sign-in, sign-out, refresh-token rotation with reuse detection that invalidates the whole device session, an explicit sliding expiry on `device_sessions`, the device list and its revoke, and rate limiting that charges failed sign-ins only and fails **closed** when Redis is down. Wave 4 closed the two that were left, both Flutter: SHIP-50's refresh interceptor, which refreshes once however many requests are refused at the same instant, and SHIP-55's sign-in screen, which deleted the development session stand-in the client had been carrying since SHIP-49.

**M1's exit criterion is now met end to end rather than server-side.** "A person can register, verify, choose a role, and stay signed in across app restarts" was demonstrated on an iPhone 17 simulator through the real Keychain: registered from the form, signed in from the form, landing in the provider half with the role having travelled out of a token the platform signed, and surviving a relaunch on a rotated token. All of the platform half is demonstrated by `make verify`, not asserted.

**The jobs table constrains all twelve statuses, and status is not a settable field.** A `BEFORE UPDATE` trigger refuses any change not described by a `job_status_history` row written in the same transaction, so the guard, the record and the transaction are one condition. `cmd/worker` claims due work under `FOR UPDATE SKIP LOCKED` and survives being run twice.

**The published contract exists (SHIP-17a), and it is checked rather than believed.** `contracts/openapi.yaml` is assembled from per-domain fragments under `contracts/paths/`, and three tests in `cmd/api` hold it to the service: the manifest and the contract must agree in both directions, live handler responses must satisfy the published schemas, and the error contract must match the `Error` schema for failures that `net/http` writes rather than a handler. That closes `TestEveryRouteIsInTheContract`, the last of the three route-surface guards in `Docs/10` §4.1 to become enforceable.

**Domain logic lives in six packages now, and every one of the six answers HTTP requests.** `internal/identity` (27 Go files) holds argon2id password storage, access-token issue, refresh rotation and the session surface; `internal/jobs` (24) holds the location value object, the budget, expiry, extension, the store, the ports and the handlers; `internal/delivery` (19) holds driver assignment, the milestone record, proof and the reasoned exception, and the job-scoped driver token and its verifier; `internal/bidding` (12) holds the offer chain, the award transaction and six endpoints; `internal/fleet` (11) holds `vehicles`, the provider's declared service area and specialties, and the eligibility predicate behind the open-jobs feed; `internal/admin` (9) holds dispute intake. Only `profiles` and `notifications` still contain `doc.go` and nothing else — one file each. The manifest holds 41 routes in total.

**No domain crossed into serving HTTP in wave 7, and that is the first wave since wave 4 in which none did.** The claim is made the way the wave-6 one was — `git ls-tree 396a55f` against today, rather than from a snapshot of the current tree, because a snapshot cannot distinguish "gained this wave" from "already had" and wave 6's own dispatch brief got this exact sentence wrong by inferring it from one. At the branch point all six domains that answer HTTP already did, and `profiles` and `notifications` held `doc.go` alone; both still do. What the wave added to those six is depth rather than reach — `bidding` gained `POST /v1/jobs/{id}/award` and the race suite behind it, `delivery` gained proof, the reasoned exception and a reachable `Delivered`.

**What did stop being documentation is an adapter: `internal/platform/storage`.** The same `git ls-tree` shows it holding `doc.go` and nothing else at `396a55f` — as it had since SHIP-10 — against `doc.go`, `s3.go` and `s3_test.go` today. It is the first of the five adapters under `internal/platform/` to be written since wave 1 built email, SMS and geocoding, and it is the one whose specified second implementation was deleted rather than built: `git log --all -- '*platform/storage/local.go'` returns zero commits, and `Docs/06` §4.1 was corrected on the proof branch to say so. `push` is now the only adapter directory still holding `doc.go` alone.

That distinction is worth keeping in mind rather than rounding away: six domains now hold code the others will want to call, and the rule that stops them calling it directly — one domain never imports another — has had several real opportunities to be broken and was not. `delivery` declares its own milestone constants rather than importing `jobs.Statuses`, `bidding` names no job status at all, and SHIP-134's publisher was kept out of `internal/events` for the same reason. Wave 3 was the first evidence for the pre-seeded infrastructure list: `internal/ratelimit` (SHIP-47) and `internal/pagination` (SHIP-66) were both written **with no shared-file edit at all**, which is exactly what registering them in `internal/boundaries` ahead of the code was for. Only `money` remains unwritten — and it now has a second consumer rather than a hypothetical one, because `bidding` duplicates `jobs`' amount cap independently. §9 has the item.

**All four deployables now exist and run, and the app drives both halves of the product rather than only the sign-up.** The Flutter client builds and runs on both simulators, the two Next.js surfaces build and serve, the Go service serves `/health` and `/v1`, and `cmd/worker` runs the scheduled work beside it — job expiry, the expiry warning and the outbox drain, all against the real stack, with `cmd/topics` applying the topic set like a migration. Wave 3's SHIP-51…54 took the client past the foundation into registration and verification; wave 4 took it into the marketplace, where a customer creates a job and reads their own job list from the device; wave 5 took it into the provider surface — SHIP-77's job detail on the customer side, and SHIP-98's fleet management on the provider side, which is the first screen in the app that belongs to one role and refuses the other. **Wave 6 gave the provider half a job feed to work from (SHIP-99) and gave the app a durable local operation queue (SHIP-124) with a worker that drains it (SHIP-125)**, which is the client capability `Docs/07` §4 calls the single most important one. **Wave 7 put the queue to work**: SHIP-100 places a bid, SHIP-129 records milestones through the queue with pending state marked in a word rather than a colour, and SHIP-126 counts what is unsynced above the router so the indicator survives every navigation. **The driver portal stopped being foundation only** — SHIP-120 is the first product code in the fourth deployable — and `apps/admin` is now the one surface that is still a placeholder shell.

## 2. Branch state

| Branch | At | Holds |
|---|---|---|
| `main` | PR #19 | **Wave 1, released 11 August 2026.** Now well behind `develop` |
| `develop` | wave 7 merged | Everything below. **Cut new branches from here** |

**`develop` is 207 commits ahead of `main` and holds seven waves.** Wave 1 was released as PR #19; everything since — SHIP-17a, the wave-2 pre-step and its three tracks, the wave-3 pre-step (SHIP-15e) and its three lanes, the wave-4 pre-step (SHIP-15g) and its four tracks, the wave-5 pre-step (SHIP-15i) and its four tracks, the wave-6 pre-step (SHIP-15m) and its four tracks, and the wave-7 pre-step (SHIP-15p) and its four tracks — is on `develop` only. The next `develop → main` pull request is the second release, and it is now several times the size of the first.

That figure is `git rev-list --count main..develop`, and it is worth naming the command because the other two readings differ sharply: `--first-parent` gives 46 (one per merged branch, which is the useful review unit) and `--no-merges` gives 153. All three were re-read on this tree rather than adjusted from the last pass, and **the wave-7 dispatch brief circulated 206 / 45 / 152** — every one of them exactly one low, because they were measured at `0abdc50` before the direct commit noted below landed. A count of `develop` taken before `develop`'s last commit is the ordinary way this figure goes wrong.

**Run the revert check before cutting it.** `main`'s history contains a revert, which is the shape where a merge silently resurrects deletions, and the two commands for establishing that it is safe are below. This is not hypothetical here: PR #19 had exactly that shape.

### The wave-7 branches, in merge order

All six were merged locally with `--no-ff`, none through a pull request. **The first landed inside
wave 7's window and belongs to wave 6's close** — the shape this section describes for `ship-15f`,
`ship-15h` and `ship-15k`, and the reason a wave's branch table and a wave's calendar never quite
line up. `ship-15n` is the wave-6 reconciliation pass and `ship-15p` is wave 7's pre-step; only the
four at the bottom are the wave's tracks.

| Merge commit | Branch | Brought |
|---|---|---|
| `05829c2` | `ship-15n-wave-6-reconciliation` | The wave-6 reconciliation pass — no ticket |
| `c20c126` | `ship-15p-wave-7-prep` | SHIP-15p — the object store in the stack and in CI, the `STORAGE_*` section asked for at dispatch, and the app test fixture, before the tracks opened |
| `6e68374` | `ship-100-129-provider-and-milestones` | SHIP-100, 126, 129, 168 |
| `4a73405` | `ship-120-driver-portal-landing` | SHIP-120 |
| `7ea172e` | `ship-92-95-award-transaction` | SHIP-92, 93, 94, 95 |
| `0abdc50` | `ship-114-116-proof-and-storage` | SHIP-114, 115, 116, 118 |

**Every one of the four tracks was cut from `ship-15p` rather than from `develop`, and every one of
them merged clean.** That is the rule wave 6 found by accident on a single track, applied
deliberately to all four — §7 has the measurement.

**One commit on `develop` has no branch behind it, and this table cannot represent it.** `23618e5`,
`Docs/11-done.txt: restore the sorted position a union broke`, sits directly on `develop` above
`0abdc50`. It is the sort §3 has been asking for since wave 4: `SHIP-15g` had been sitting after the
M7 group ever since a `merge=union` resolution appended it there, and the window to fix it finally
existed because no branch was open to conflict with. It is noted here rather than left out, because
a reader reconciling this table against `git log --first-parent develop` would otherwise find a
commit the table does not explain and have to work out whether something was lost. Nothing was: the
file's contents are unchanged and only one line moved.

### The wave-6 branches, in merge order

All six were merged locally with `--no-ff`, none through a pull request. **The first landed inside
wave 6's window and belongs to wave 5's close** — the same shape this section already describes for
`ship-15f` and `ship-15h`, and the reason a wave's branch table and a wave's calendar never quite
line up. `ship-15k` is the wave-5 reconciliation pass and `ship-15m` is wave 6's pre-step; only the
four at the bottom are the wave's tracks.

| Merge commit | Branch | Brought |
|---|---|---|
| `e003e34` | `ship-15k-wave-5-reconciliation` | The wave-5 reconciliation pass — no ticket |
| `97b4900` | `ship-15m-wave-6-prep` | SHIP-15m — the driver-guard seam, the Kafka replication factor and three parallel-working rules, before the tracks opened |
| `7cb3da5` | `ship-107-112-driver-token-and-absorption` | SHIP-107, 108, 112 |
| `97bc8ff` | `ship-163-dispute-intake` | SHIP-163 |
| `3aa491a` | `ship-99-124-provider-feed-and-queue` | SHIP-99, 124, 125 |
| `396a55f` | `ship-84-88-bids-and-offers` | SHIP-84, 85, 86, 87, 88 |

**The middle row is the one to read this time.** `ship-107-112` was cut from `ship-15m` rather than
from `develop`, because it needed the driver-token guard seam that pre-step supplies — and it was the
only one of the four tracks to merge without a conflict. The other three were cut from `develop`
while `ship-15k` still sat unmerged, and every one of them met it in this file. §7a has the account;
it is the cheapest process change the wave turned up.

### The wave-5 branches, in merge order

All seven were merged locally with `--no-ff`, none through a pull request. **The first two landed
inside wave 5's window and belong to wave 4's close** — the same shape this section already
describes for `ship-15f`, and the reason a wave's branch table and a wave's calendar never quite
line up. `ship-15h` is the wave-4 reconciliation pass and `ship-15i` is wave 5's pre-step; only the
four in the middle are the wave's tracks.

| Merge commit | Branch | Brought |
|---|---|---|
| `5527994` | `ship-15h-wave-4-reconciliation` | The wave-4 reconciliation pass — no ticket |
| `a00e9ed` | `ship-15i-wave-5-prep` | SHIP-15i — the measured verify count and the §3 row check, before the tracks opened |
| `813ab04` | `ship-77-98-customer-detail-and-fleet` | SHIP-77, 98 |
| `2d6c97f` | `ship-69-135-expiry-and-topics` | SHIP-69, 70, 135 |
| `d28ce60` | `ship-106-111-assignment-and-milestones` | SHIP-106, 111 |
| `b258c91` | `ship-79-83-provider-feed` | SHIP-79, 81, 82, 83 |
| `5820a8b` | `ship-15j-wave-5-merge-repair` | The merge repair — no ticket |

**The last row is the one to read.** `ship-15j` exists because a merge was committed while its own
gates were still running, publishing a `develop` that carried a stale check count and a
union-ordered route table. Nothing was lost and nothing was wrong in the tracks; the tree was
captured before the resolutions landed. §7b has the full account, and the rule it produced —
**never commit or merge while a gate is running** — is in §9 as a line `CLAUDE.md` should carry.

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
section the moment the two met. §7c has all of it, and both the check count and the one-binary
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

The last two were merged locally rather than through a pull request, which is why they have no number. **Both conflicted in this file and nowhere else**, and both resolutions were unions — see §7e.

An earlier version of this section chased the exact pull-request number and commit count, and was wrong within a day both times — a commit cannot record the number of the pull request that merges it. **This table is always slightly behind reality, and the fix is to correct it in the next update rather than to try to make it self-aware.** It says "wave 7 merged" rather than a number, for that reason.

The commit count above is the same kind of figure and gets the same treatment: **recount it at each reconciliation, never carry it forward.** It has been carried forward wrongly once — a handover brief recorded 81 when the true count at the wave-3 reconciliation was 98 — which is the cost of copying a number that a single merge invalidates. The six merges in the wave-4 table took it from 98 to 125, the seven in the wave-5 table took it from 125 to 151, the six in the wave-6 table took it from 151 to 174, and the six in the wave-7 table took it from 174 to 206 — with `23618e5` on top of them making **207**, which is the size of the correction one wave makes. The three readings move at different rates and none of them can be derived from another: 46 first-parent commits is 45 merged branches plus that one direct commit, over seven waves, while 153 no-merge commits is the work itself.

`main` still shows commits `develop` does not have. Those are the detour, not divergent work: wave 0 reached `main` by being merged (PR #6), reverted (PR #7), and reapplied (PR #9), and PR #19's own merge commit sits on `main` alone. The content is identical; only the shape of the history differs.

### The wave-1 branches, in merge order

| PR | Branch | Brought |
|---|---|---|
| #11 | `ship-15b-wave-1-prep` | SHIP-15b, and SHIP-37's dependency amendment |
| #12 | `ship-15a-close-mobile-decisions` | `Docs/07` §9 closed, `Docs/10` §8.3 reconciled |
| #13 | `ship-22-35-web-and-adapters` | SHIP-22, 23, 32, 35, 59a |
| #14 | `ship-29-38-credentials-and-sessions` | SHIP-29, 37, 38, and the SHIP-149 verify fix |

`ship-16-21-flutter-foundation` exists and is parked at PR #11's merge, holding nothing. It is the branch the Flutter track resumed on — see §7f.

**A warning worth keeping.** Reverting a merge does not undo it: the commits stay ancestors forever, so re-merging the same branch brings nothing across and reports success. If a merge to `main` is ever reverted again, the fix is to revert *the revert*, not to merge again.

**PR #19 had exactly the shape that warning describes, and was checked rather than trusted.** The revert `d733c95` sits on `main` and is *not* in `develop`'s ancestry — the setup where a merge silently resurrects deletions. It was safe only because the reapply `c1cb64b` had already restored the content, leaving the revert nothing to take away. The check that established this before merging is worth reusing on any release whose history has a revert in it:

```
git merge-tree --write-tree main develop   # the tree the merge would produce
git rev-parse develop^{tree}               # the tree develop actually has
```

Identical hashes mean the merge result is exactly `develop`'s content. Different hashes mean something is being dropped or added, and the release needs looking at before it is cut, not after.

## 3. Done

Verified by `make verify` — **570 checks across 13 sections**, and `make check` green. Since
SHIP-15e the checks live one file per milestone or domain in `scripts/verify/`, sourced by the
runner; a ticket adds its section by adding a file. Wave 4 added two: SHIP-78's
`scripts/verify/60-fleet.sh` and SHIP-134's `scripts/verify/80-notifications.sh`. SHIP-67 and
SHIP-68 added theirs to the jobs file. **SHIP-15p added no file**: an object store
is stack rather than a domain, so its five checks are a section inside
`scripts/verify/00-stack.sh`, beside SHIP-1…4 and before the service is built.

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

### M0 — Foundation (33 of 37)

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
| **SHIP-15m** | Wave-6 shared surfaces: the `RequireDriverToken` guard seam SHIP-108 fills without editing `routes.go`, `KAFKA_REPLICATION_FACTOR` as configuration, and three parallel-working rules `CLAUDE.md` and `Docs/10` now carry — *see below* |
| **SHIP-15p** | Wave-7 shared surfaces: an S3-compatible object store in the local stack and in CI, the `STORAGE_*` configuration section asked for at dispatch rather than absorbed after it, and the app test fixture that every new configuration section used to break — *see below* |
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
| **SHIP-56a** | M2 | `contracts/statuses.yaml` produces the Go, Dart and TypeScript forms of all three status vocabularies, and a test fails when any of the seven generated files is stale — *see below* |
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
| **SHIP-69** | M2 | The expiry warning forty-eight hours ahead — a second task over the same column, and the job is warned once per *deadline* rather than once per job — *see below* |
| **SHIP-70** | M2 | `POST /v1/jobs/{id}/extend` — an empty body, because the platform computes the deadline. **Not a status transition**, and the pickup date still bounds it — *see below* |
| **SHIP-71** | M2 | Flutter locations step — the platform validates and normalises, and an unrecognised address is an outcome the customer walks past, not an error — *see below* |
| **SHIP-76** | M2 | Flutter customer job list — read once and grouped client-side, and a test keeps the budget out of every widget a provider could reach — *see below* |
| **SHIP-77** | M2 | Flutter customer job detail — the timeline is derived from the current status, because the transition history the database records is served by no endpoint; and sign-out finally tells the platform — *see below* |
| **SHIP-78** | M3 | `vehicles` and the six routes under `/v1/fleet/vehicles` — deactivated, never deleted, and one live plate per provider held by a partial unique index — *see below* |
| **SHIP-79** | M3 | `provider_service_areas` and `provider_specialties`, and `GET`/`PATCH /v1/fleet/profile` — a service area is a **set of named regions, not a radius**, so §9's `internal/geo` trigger does not fire at SHIP-81 — *see below* |
| **SHIP-80** | M3 | `bids` — the eight statuses of `Docs/02` §4, and the index that makes a second accepted bid impossible. No endpoint: **demonstrated by its own tests** — *see below* |
| **SHIP-81** | M3 | The job eligibility filter — **one SQL predicate in `fleet`, reaching four tables across three domains**, chosen over ports because ports cannot page a set intersection. Two readers, one clause; no endpoint until SHIP-82 — *see below* |
| **SHIP-82** | M3 | `GET /v1/jobs/open` — the provider's feed, keyset-paged. **Declared in `routes_fleet.go`, not `routes_jobs.go`**: routes follow the domain that answers them, not the first segment of the path. It also deletes SHIP-81's SQL mirror from `make verify` in favour of real HTTP checks — *see below* |
| **SHIP-83** | M3 | `GET /v1/jobs/open/{id}` — one job as a provider sees it, and **the fourth budget proof §8 recorded as still owed**: the serialised response, obtained over HTTP, held to a *closed set of keys* so that a budget renamed `max_price` fails too. The street line and the coordinate are confirmed withheld — *see below* |
| **SHIP-84** | M3 | `POST /v1/jobs/{id}/bids` — a verified, eligible provider offers a price and two timing commitments. **Opens the `bidding` domain**, and reaches `fleet`'s eligibility answer through a port with no adapter behind it. "Bid once per job" and "a retry is not a second bid" are two partial unique indexes rather than two checks — *see below* |
| **SHIP-85** | M3 | `PATCH /v1/jobs/{id}/bids/{bid_id}` — a provider revises their own live offer **in place**: same row, same status, same key. That last one is the point — writing a revision's key over the placement's would turn a late retry into a `409` for a request that succeeded — *see below* |
| **SHIP-86** | M3 | `POST /v1/jobs/{id}/bids/{bid_id}/withdraw` — the offer becomes `Withdrawn` and the row survives as record. **Idempotent by state rather than by key**, which is stronger than a stored key and is why this endpoint needed neither a column nor a migration — *see below* |
| **SHIP-87** | M3 | `POST /v1/jobs/{id}/bids/{bid_id}/counter` — **the first endpoint in this domain a customer may call**, and one route for both directions because Docs/02 §4's two sentences describe one act. Each counter is a new row and the offer it answers becomes `Superseded`; a counter inherits the terms it does not restate, which is the choice `000501` deferred to it — *see below* |
| **SHIP-88** | M3 | The supersede chain — `superseded_by` on the **displaced** row, which is what turns "only the latest valid offer is acceptable" into a column `CHECK` SHIP-92 cannot violate rather than a rule it must remember. Plus `GET …/history`, because a chain nobody can read is not one that remains readable — *see below* |
| **SHIP-91** | M3 | The one-accepted-bid constraint — **met by SHIP-80 rather than built separately**, and declared done by the owner rather than claimed by a commit — *see below* |
| **SHIP-92** | M3 | `POST /v1/jobs/{id}/award` — one offer accepted and the job moved, in one transaction, against the lock ordering SHIP-88 wrote down rather than one invented here. **A verb on the job, so the bid travels in the body**, and the one rule no constraint can express — that the offer was live when it was accepted — is the only thing application logic checks. Idempotent by **state**, so it needs no key column and no migration — *see below* |
| **SHIP-93** | M3 | The rejection sweep — every offer still live on the awarded job becomes `Rejected` in the award's own transaction, and every offer that had **already** closed keeps the status saying how it closed. One `UPDATE` at step 4 of the recorded lock ordering, no `id <> winner` in it, and the refusal order changed so that a second award still answers `conflict` rather than `bidding_bid_closed` — *see below* |
| **SHIP-94** | M3 | Award idempotency — **two mechanisms, and the ticket is settling which does which work.** Redis replays the response while its entry lives; the accepted offer answers every retry it cannot, including one under a fresh key. They disagree on exactly one request — a key reused for a *different* offer — and the middleware refuses it, rightly. **No key column, no migration, no handler change**: what it adds is the proof, and the two retries nobody had tested — the one that runs after SHIP-93's sweep, and the one that arrives after the delivery has started — *see below* |
| **SHIP-95** | M3 | The award concurrency suite — Docs/08's four races, **written adversarially from the documents by an agent that did not read the implementation**, and every one of them observed racing rather than assumed to: a transaction is held open and `pg_blocking_pids` is polled until PostgreSQL confirms the other is waiting on it. Three properties no existing test could see are now pinned — that the job row is held, that it is held *before* the bid, and that an offer which stopped being live mid-award is not accepted — each demonstrated by breaking the implementation and watching a named test fail. It also found the liveness rule is kept **twice**, and that either guard alone is invisible — *see below* |
| **SHIP-98** | M3 | Flutter provider fleet — the first provider-only surface in the app, and the list endpoint answers a customer `200` rather than refusing them, which is why the device has to say whose surface it is — *see below* |
| **SHIP-99** | M3 | Flutter provider job feed — the provider half of the shell stops being a placeholder. **`GET /v1/jobs/open` accepts no filter at all**, so the *Done when*'s filters are a client-side narrowing the contract delegates to this ticket by name, drawn from a second response type with no field a budget could go in — *see below* |
| **SHIP-100** | M3 | Flutter provider job detail and bid placement — one job over `GET /v1/jobs/open/{id}` and an offer over `POST /v1/jobs/{id}/bids`. **The bid is sent directly and never queued**, which `Docs/07` §4 requires and SHIP-124's private `OperationKind` constructor already made impossible to get wrong; what makes a retry safe is one `ActionKey` per action against SHIP-84's stored key column. It also **closes §9's client-side budget guard** by holding every provider-facing model to a closed key set — *see below* |
| **SHIP-105** | M4 | `driver_assignments` — the driver has no account, so no foreign key to `users`; one live assignment per job by partial unique index. No endpoint: **demonstrated by its own tests** — *see below* |
| **SHIP-106** | M4 | `POST /v1/jobs/{id}/driver` — the awarded provider nominates a driver or drives it themselves, and the job moves in the same transaction. The first endpoint in `delivery`, and the first to reach two other domains through ports rather than imports — *see below* |
| **SHIP-107** | M4 | The driver's job-scoped token — its own keyset, `aud=shipper-driver`, seven days, minted **inside the assignment transaction** and obtainable nowhere else. **The claim set has no `sub`**, so the exchange `Docs/10` §5 forbids has no material to work from rather than merely being refused — *see below* |
| **SHIP-108** | M4 | The driver token verifier and `GET /v1/driver/jobs/{id}` — the first route in the service served on something other than a mobile session. **The one-job check is the auth class**, so a driver route cannot declare the class and skip it, and both directions of the exchange invariant are now demonstrated over HTTP rather than only in Go — *see below* |
| **SHIP-110** | M4 | `milestones` — the actor's clock and the server's kept apart by a trigger that refuses an insert naming the server's. No endpoint: **demonstrated by its own tests** — *see below* |
| **SHIP-111** | M4 | `POST /v1/jobs/{id}/milestones` — what a delivery records, once per idempotency key. Redis makes the retry cheap and a partial unique index makes it correct, and `make verify` tells the two apart by deleting the cached response — *see below* |
| **SHIP-112** | M4 | Out-of-order milestone absorption — a milestone the job has moved past is **kept and moves nothing**, where SHIP-111 refused it and rolled it back. "Backwards" is decided by whether the job has *recorded a transition into* that status, which leaves a premature milestone still refused and still retryable — *see below* |
| **SHIP-114** | M4 | `POST /v1/jobs/{id}/proof-uploads` and `internal/platform/storage` — a short-lived pre-signed URL the client PUTs a photograph to, **directly to the object store with this API in neither direction**. The type and the size are **signed into the URL**, so the platform's limits are enforced by the store on the request that carries the bytes rather than by us on the one that does not. **`local.go` is dropped**: one implementation, exercised locally against a real store — *see below* |
| **SHIP-115** | M4 | `proofs` and `GET /v1/jobs/{id}/delivery/proof` — an uploaded object becomes evidence for **one recorded milestone**, and the customer and the awarded provider read it back through short-lived signed URLs issued *after* an authorisation check. Because the platform is not in the upload path it **asks the store whether the object arrived** rather than believing the client, and records what the store reports — which is also what finally puts SHIP-114's upload limits under a guard inside the domain — *see below* |
| **SHIP-116** | M4 | The reasoned exception — a milestone may be evidenced by a photograph **or** by one of `Docs/01` §4.4's three reasons there is none, and never both and never neither. It is one row in `proofs` rather than a table beside it, because that is the only shape in which "never both" is a `CHECK` at all. Nothing is uploaded and the object store is not contacted; the reader is handed the reason and **no signed URL**, because there is no object to sign one for — *see below* |
| **SHIP-118** | M4 | **The invariant stops being intended and starts being enforced.** `Delivered` becomes recordable — the `Jobs` port gains its fifth move, whose absence had been half of the old refusal — and a recording carrying neither a photograph nor a reasoned exception is refused with nothing written and the job unmoved. Enforced twice: in the domain, where a client is told which of the two to send, and by a **deferred constraint trigger** (`000605`) that refuses the row at `COMMIT` whoever wrote it — *see below* |
| **SHIP-120** | M4 | Driver portal token landing — the first product code in the fourth deployable. The link is `/j/<job-id>#<token>`: the token in the **fragment**, which no server ever receives, moved to `sessionStorage` and stripped from the address bar; **the job identifier carried independently of it**, because a client deriving it from the token would make SHIP-108's one-job check compare the token with itself. Five fields, because five is what the endpoint serves — and **the delivery detail its *Done when* names is not among them**, see §4 — *see below* |
| **SHIP-124** | M4 | Flutter durable operation queue — Drift over SQLite, **FIFO within an ordering key and nothing between keys**, and an operation this build cannot read is **quarantined rather than skipped**. Six ways an operation could vanish, enumerated and tested. No endpoint: **demonstrated by its own tests** — *see below* |
| **SHIP-125** | M4 | Flutter sync worker — **six triggers, because "reconnection" is not a reliable event on a handset**; an exponential backoff stored per operation and ceilinged at five minutes, because nothing can shorten a stored wait; and one idempotency key per operation, minted at enqueue and unchanged on every attempt. Sign-out finally clears the queue. No endpoint: **demonstrated by its own tests** — *see below* |
| **SHIP-126** | M4 | Flutter pending-updates indicator — a bar **above the router and below the content**, so it survives every navigation, because "persistent" in `Docs/02` §3.1 means it does not go away when the screen does. It counts `unsynced` — pending **and in flight** — and shows quarantined work as a second line rather than a fourth number, which closes the hole SHIP-125's exclusion would have left. It lives inside `ShipperApp`, so the worker is **supplied by `main.dart`** rather than reached for, and a test holds that wire — *see below* |
| **SHIP-129** | M4 | Flutter milestone update UI — `/jobs/{id}/delivery`, three large buttons, and a log with **pending marked in a word, an icon and a sentence rather than a colour**. **The only reconciliation signal a client has is the operation leaving the queue** — `send` returns `void` and the row is deleted — so a stale snapshot could say the platform had work it did not, and the guard against that is the one mutation that survived the suite. Finding: **no endpoint serves an awarded job to the provider delivering it**. Driven against a live API on a simulator — *see below* |
| **SHIP-134** | M5 | The transactional outbox publisher — a Kafka producer in `cmd/worker`, and the aggregate is the unit of division — *see below* |
| **SHIP-135** | M5 | The topic set and the event catalogue — three topics applied by `cmd/topics` like a migration, and **no dead-letter path, because a permanently unpublishable row is now unwritable** — *see below* |
| **SHIP-136** | M5 | Nine domain events from `bidding` and `delivery`, declared in each domain's own `events.go` with **no edit to `internal/events`** — the seam SHIP-135 left, used as intended. `shipper.bid` and `shipper.delivery` carry traffic for the first time. The delivery events exist because **the job's status does not carry everything `Docs/01` §4.4 asks an actor to record**: an absorbed late milestone moves nothing and so emitted nothing at all before this. Two of §4.5's six lines cannot be met and are **named rather than narrowed away** — *see below* |
| **SHIP-149** | M6 | `audit_log`, append-only enforced by trigger — *see §4* |
| **SHIP-163** | M6 | `POST /v1/jobs/{id}/disputes` — `Docs/04` §7's intake fields, and raising one **freezes the job** through the guard. M6's first code, and the first endpoint in `admin` — which is a **user** endpoint, not an administrative one. It writes **no audit row**, and the category list is a **reading** of `Docs/02` §5 rather than a quotation — *see below* |
| **SHIP-167** | M7 | `GET /v1/app/minimum-version`, configuration-driven |
| **SHIP-168** | M7 | Flutter launch-time version gate — the app asks the floor at launch and **replaces itself** below it, in **two shapes**: with a store link, and without one, which is the shape the pilot actually ships. An **unreachable API does not block**, because the gate is a courtesy and `/v1` refusing the build is the control. The running build's number is the **native** one, read rather than duplicated into a define — *see below* |
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

### SHIP-56a — one source for three languages, and the reading the documents had already taken

`contracts/statuses.yaml` is the source. `make codegen` writes seven files from it: the Go form of
each vocabulary beside the domain that owns it, the Dart form beside the feature that shows it, and
one TypeScript module in the driver portal.

**The decision was not open, and finding that out was most of the work.** The ticket's *Done when* —
"one source produces all three" — reads two ways: a neutral specification producing all three, or Go
being the source with the other two derived from it. `Docs/10` §8.2 had settled it before the ticket
was written, in one sentence naming this exact file, and three source files carried comments
promising it: `internal/jobs/model.go`, `job_status.dart` and `bid_status.dart` each said "this is
the *N* copy until SHIP-56a lands". `CLAUDE.md` is explicit that a contradiction with a document is
resolved in the document first and never silently in code, so the second reading would have needed
`Docs/10` §8.2 changed and an argument for changing it. There was none: the objections to a neutral
source were that the SQL pairing and the documentation would be at risk, and neither turned out to
be true — see both below.

It is also the reading that leaves nothing lying. Under the alternative, the argument for why nothing
writes the `Countered` bid status keeps existing twice — once as a paragraph explaining a Go
constant, once as a paragraph explaining a Dart enum member, in almost the same words, which is how
it was actually written. It is now one entry in the specification, rendered into all three.

**The SQL pairing survived untouched and got stronger for free.** Nothing generates SQL: migrations
are applied history and cannot be regenerated. `ck_jobs_status`, `ck_bids_status` and
`ck_proofs_exception_reason` are hand-written exactly as they were, and `Docs/10` §3.4's pairing test
per enumeration reads each out of `pg_constraint` and holds it to the Go constants in both
directions. Not one of those tests changed. What changed is what they now pair: the constants are
generated, so the constraint is being compared with the specification. Adding a status to the
specification and regenerating fails three constraints at once — demonstrated, below.

**What is generated is the vocabulary and nothing else**, and the line is "would a second language
want a copy of it?":

| Generated | Not generated, and why |
|---|---|
| The type, its values, the ordered list, `Valid`, `String`, `Wire`, `FromWire` | `Docs/02` §2's transition table and `bidding`'s liveness predicate. Decisions, not names — `Docs/07` §3 puts every such decision on the platform, so a copy on the device would be a second authority for a question that has one |
| The Dart enum, its `@JsonValue` wire forms, `wireName`, `label`, and the `unknown` sentinel | The SQL `CHECK` constraints. Applied history; the §3.4 pairing test is the mechanism and it is unchanged |
| One TypeScript module: a `const` object, a union type, the ordered values, the labels, a type guard | Actor vocabularies — `jobs.ActorType`, `bidding.Party`, Dart's `BidParty`. They name a kind of person rather than a lifecycle state. Moving them later is an entry in the specification and no new mechanism |

**Two forms per value are written out and neither is derived.** Go used to derive the wire form from
the stored form by lower-casing and replacing spaces, arguing that "a transformation cannot disagree
with its input" where a hand-written table beside the constants can. That was right about
hand-written tables and does not survive generation — a table produced from the same source as the
constants cannot disagree with them either. What the derivation cost was visibility: both strings are
published contracts with different audiences, `stored` to `ck_jobs_status` and to anybody reading the
database, `wire` to a build already installed on a phone. A rule hides the second, and the first
status that did not fit the rule would be renamed on the wire by a change nobody read as a rename.

**The generated Dart file is not the file anything imports, and that is deliberate.** It is
`<name>.gen.dart`, with a hand-written `<name>.dart` exporting it. The first draft generated
`bid_status.dart` outright and deleted `isLive` and `BidParty` — which is the failure mode of
generating over hand-written code, found in the first run rather than in a later one. Every consumer
still imports the name it always imported, and no import site in `apps/mobile` changed.
`build_runner` was re-run and produced no diff at all: the enum moving files is invisible to
`json_serializable`.

**`{{n}}` in a doc renders as the value count, spelled out.** Prose saying "one of the twelve" is a
hand-maintained scalar, and §3 above records what those do here — the `make verify` figure conflicted
in four consecutive merges and was wrong in three of them. The specification says `{{n}}` and the
generator counts.

#### The half that is easy to under-build, and how it was demonstrated

"CI fails if a generated file is stale" is not a `make` target that regenerates.
`TestGeneratedFilesAreCurrent` in `services/core/cmd/statusgen` renders the specification in memory
and compares it with every committed output, so it runs under `go test ./...` — under `make test`,
under `make check`, and in the Go workflow, with no CI step of its own. `make codegen` exists to fix
what it finds; `make codegen-check` reports without writing and is deliberately not in `CHECKS`.

A test rather than the `git diff --exit-code` after regenerating that `Docs/10` §8.2 sketched, and
that sentence has been corrected. It is strictly stronger in two ways this repository has paid for.
It **writes nothing**, so it cannot produce the false failure a tree-rewriting gate produced in
wave 5 on a tree where nothing was wrong, and it works on a dirty tree, which is where it is run. And
it fails on a generated file that is **missing entirely**, which a diff of tracked files does not see.

Eight mutations were applied and restored from a tar snapshot, each confirmed with a checksum:

| Mutation | Caught by |
|---|---|
| Rename a status in the specification, do not regenerate | The staleness test, naming all three languages and the first differing line in each |
| Hand-edit a label in a generated Dart file | The staleness test |
| Hand-edit a wire string in a generated Go file | The staleness test |
| Delete a value from the TypeScript only | The staleness test |
| Delete a generated file entirely | The staleness test — the case `git diff` would not see |
| Add a status to the specification and regenerate, with no migration | `ck_jobs_status`, `ck_job_status_history_from_status` and `ck_job_status_history_to_status`, all three |
| A wire form in the wrong case | The specification's own validation, refusing to generate at all |
| Hand-edit a generated Dart file, then run `make check` | `make check` exits 2 — the CI command itself, not a proxy for it |

The one link not demonstrated locally is the workflow trigger. `.github/workflows/go.yml` gained
`apps/**/*.gen.dart` and `apps/**/*.gen.ts`, because the staleness check is a Go test and without
them a hand-edited generated *client* file would start the Flutter or web workflow — neither of which
checks it — and not the Go one, which does. GitHub's path matcher cannot be run here, so that glob
rests on documented `**` semantics rather than on a demonstration. **It costs no macOS minutes and
cannot:** every job in every workflow in this repository is `ubuntu-latest`, the runners that need a
real machine are SHIP-24…27 and blocked on X-2 and X-3, and a change under `services/core/` still
starts the Go workflow alone. The globs are narrow on purpose — a hand-written Dart or TypeScript
file matches neither.

#### A mutation that survived, and it was not this ticket's to fix

**The published contract is a fourth copy of the vocabulary and nothing pairs it with the other
three.** `contracts/paths/jobs.yaml` enumerates the twelve job statuses, `bidding.yaml` the eight bid
statuses, and `delivery.yaml` the three proof exception reasons — all by hand. Deleting `countered`
from `bidding.yaml`'s enum and running `make check` **passes**. So a status added to
`contracts/statuses.yaml` reaches Go, Dart, TypeScript and — through the pairing test — the database,
and does not reach the document clients are generated from.

It is left open on purpose rather than overlooked, and the reason is the shape of the fix. Several
enumerations in those fragments are **legitimate subsets**: `delivery.yaml`'s recordable milestones
are four of the twelve job statuses deliberately, and its actor list is a fifth vocabulary again. A
check that pairs by overlap would fail on every one of them, which is the false-failure pattern this
repository warns hardest about. Making it work needs each fragment to declare *which* vocabulary each
enum is and whether it is the whole of it — a contract decision, belonging to whoever owns the
`cmd/api` contract tests, not smuggled into a two-point codegen ticket. `Docs/10` §8.2 records it.

**Nothing else in the sweep survived, and that is the weaker evidence of the two.** Seven of the
eight mutations attack the same mechanism from different sides — a byte comparison between a render
and a file — so catching all seven says that comparison works and very little else. The mutation that
found something real was the one aimed at a *different* mechanism, which is the one that had not been
built.

#### Notes for whoever adds the next status

Edit `contracts/statuses.yaml` and run `make codegen`. The generator refuses a value with no
documentation, a wire form that is not lower snake case, two values sharing a wire form or collapsing
to one identifier, and a doc written as a sentence rather than a phrase — that last one because Go
opens a comment with the identifier it documents and the other two languages want a sentence, so one
lower-case phrase serves all three and "StatusSubmitted is A live offer" was the first draft.

Then write the migration. The specification cannot, and the §3.4 pairing test is what will tell you
so. **`internal/config` was not needed and is not expected to be** — nothing here is configurable,
and a status vocabulary that could be changed by an environment variable would be a vocabulary the
database constraint disagrees with.

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

### SHIP-69 — warned once per *deadline*, which is a trigger's job rather than a caller's

Docs/02 §6.3's other sentence — "the customer is warned 48 hours before expiry" — is
`job.expiry_warned`, emitted by a second task in `cmd/worker` over the column SHIP-68 built. The
claim, the event and the pass are each the same shape as the expiry sweep's, deliberately: two tasks
that claimed work differently would be two things to reason about when a pass misbehaves at three in
the morning.

**The warning needed somewhere to record that it had happened, and the tempting place was wrong.**
The sweep runs every five minutes and a job sits inside the window for two days, so without a mark
the customer's phone buzzes about five hundred times for one job. The obvious answer — ask the
outbox whether a `job.expiry_warned` event exists for the aggregate — is the wrong table. The outbox
is a hand-off rather than a record: rows are marked published and are prunable the moment they are
(`000004` says so), so the question is answered correctly today and wrongly after the first
clean-up. It is also a read of the events seam by a domain that is only supposed to write through
it. `000407` adds `jobs.expiry_warned_at` instead.

**"Once per deadline" is a trigger, and that is the decision worth reading.** A mark alone gives
"once per job", which would mean a customer who extends is never warned again — SHIP-70 silently
switching SHIP-69 off for exactly the jobs that had used it, with the symptom being a notification
that never arrives. So `000407` clears `expiry_warned_at` whenever `expires_at` changes, on the same
argument `000406` makes for setting the deadline in the first place: moving a deadline is not one
code path. SHIP-70's endpoint is the first, an administrator adjusting a listing is a plausible
second, a republished job is a third, and a line in the extend handler is a line the other two can
forget. Attached to the change rather than to the caller, as `updated_at` and `expires_at` already
are.

**The claim is bounded at both ends, and the lower bound is the interesting one.** `expires_at > $1`
excludes a job whose deadline has already passed, because both sweeps live in one binary and run on
the same interval — so without it, a job that outlived its deadline between two passes gets "expires
in two days" and "has expired" in the same minute, which costs a customer's trust in every later
notification. The forty-eight hours itself is one constant in Go (`jobs.ExpiryWarning`) and the
claim takes the horizon as a parameter, so a test moves the window instead of waiting.

**A warning is not a transition and nothing here pretends otherwise.** The job is `Open` before and
`Open` after; `Service.Transition` is not called, no `job_status_history` row is written, and
`000402`'s guard returns early because the statement does not name `status`. Reusing the status event
would have meant emitting `Open → Open`, which Docs/02 §2 has no row for — and would have put a move
that never happened into a customer's timeline. Both the domain test and `make verify` assert the
history row count is unchanged, because that is the failure a plausible implementation produces.

**The two tasks are separate for their failure modes, not for tidiness.** One task claiming and
doing both would mean a failure in either half rolls back the other, so an outbox that cannot be
written would stop jobs expiring — the wrong trade, since a job left Open past its pickup date
misleads providers while a warning that arrives late merely arrives late.

**One thing this file's next reader has to know about the worker.** `cmd/worker` is one binary, so
every verify section that starts it now runs a warning sweep too. `scripts/verify/50-jobs.sh` leaves
**no job inside the forty-eight-hour window** — each is either already marked warned or has a
deadline days away — so a later section's worker start claims nothing, and there is a check at the
end of that file asserting exactly this. A future check that leaves an `Open` job an hour from its
deadline will be told so there rather than by a puzzling failure three sections later. The count
went from 268 to **275**.

### SHIP-70 — an extension is an ordinary `UPDATE`, and the pickup date still bounds it

`POST /v1/jobs/{id}/extend`, with `{}` as the whole request. Docs/02 §6.3's "can extend in one
action" is one call and no fields.

**It is not a status transition, and that was the ticket's first question.** The answer is no, and
`000406` had already reached it from the other side: its trigger fills `expires_at` only when the
column is NULL precisely so that "SHIP-70's extend endpoint is an ordinary `UPDATE`". A job is
`Open` before an extension and `Open` after, Docs/02 §2 has no row describing it, and `Open → Open`
is a move the guard refuses on purpose. So none of SHIP-57's machinery is involved. That is not a
loophole — `000402`'s own comment says "every other update to a job — its category, its addresses,
its budget — passes straight through", and a deadline is one of those.

**The client does not say how long.** A period in the body would be a client choosing how long the
platform's own listing rule applies to it, which is the same shape as naming a status. The new
deadline is `LEAST(now + 14 days, pickup_window_end)` — `000406`'s rule applied again from the moment
the customer acted. Counted from *now* rather than added to the deadline the job has, so acting
early gains no more than acting late; and `extendRequest` has no fields at all, so
`httpx.DecodeJSON` refuses `{"days": 30}` rather than ignoring it. A client that believed it had
bought thirty days and received fourteen would have no way to tell from a successful response.

**The pickup date still wins, and the refusal is the interesting half.** Docs/02 §6.3 makes the
pickup date the operative rule — "a job whose pickup window has gone is dead regardless of how
recently it was posted" — so an extension that ignored it would put a listing in front of providers
advertising a collection date that had passed, which wastes a bid rather than a glance. A job whose
deadline already *is* its pickup date therefore answers `409 jobs_not_extendable` rather than `200`
with nothing changed, and the message names which of the customer's two dates is ending the job.
Two sentinels share that one code — `ErrJobNotExtendable` (not `Open`) and `ErrExpiryBoundByPickup`
— on the same reasoning that has `ErrNotJobOwner` and `ErrJobNotFound` sharing `not_found`: the
client's action is identical and only the sentence differs.

**Two things this deliberately does not do, and both are findings rather than omissions.** There is
**no cap on the number of extensions**: every job with a pickup window is bounded by it, and a job
without one is the distant-date case the backstop exists for, where the customer's continued
interest is the only signal there is. A cap is a counter column and a policy decision. And **no
endpoint moves the pickup window of an `Open` job** — `PATCH` is Draft-only (`jobs_not_a_draft`), so
the customer told "your pickup date is what is ending this job" has no way to act on that today.
Neither is in SHIP-70's *Done when*; both belong to whoever owns SHIP-63's publish/republish path.

**It emits `job.expiry_extended` although the *Done when* does not ask for one**, and the reason is
SHIP-69. A consumer that has already told the customer "this job expires in two days" has no other
way to learn that it no longer does, and `000407` re-arms the warning — so a second
`job.expiry_warned` would otherwise arrive later with nothing to explain why the first was void. The
payload carries both deadlines for exactly that reason. It is also the only record that an extension
happened at all: `job_status_history` is deliberately not the place for it.

**`make verify` covers both tickets against the real worker and the real endpoint**, and the
fixture worth naming is `age_job` — a plain `UPDATE` bringing a deadline to a day away, because no
check can wait thirteen days, and it is the same statement the endpoint itself makes rather than a
way around anything. The count went from 275 to **285**.

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

### SHIP-77 — the timeline the endpoint does not serve, and the sign-out that finally reaches the platform

Tapping a job in SHIP-76's list opens it in full: both addresses, the goods, the timing, the
customer's own budget, a status timeline, and the actions the platform would permit. The route is
`/jobs/{id}` and the screen re-reads the job from it rather than being handed one across, which is
two things at once — a list page fetched ten minutes ago cannot show a stale status, and SHIP-145's
notification payload reaches the same screen with nothing but an id.

**The ticket's *Done when* says "status timeline", and the endpoint serves no history.** This was
worth finding out on the wire rather than assuming, and the wire is unambiguous: `GET /v1/jobs/{id}`
answers the `Job` schema, which is `additionalProperties: false` and carries one `status`,
`created_at` and `updated_at`. There is no `history` array and no per-transition timestamp anywhere
in it. `job_status_history` exists and is exactly what a timeline wants — SHIP-57a gave it the
actor, the reason and both clocks, append-only — and `Service.History` reads it in Go, but **nothing
exposes it over HTTP**. A read against this worktree's API on 8092 confirmed the response field for
field.

So the timeline is **derived from the current status**, and the design is mostly a list of things it
refuses to claim:

- **It says where the job is**, which the response establishes.
- **It never says when an earlier step happened**, which the response does not. A step behind the
  current one carries no date, because the only candidates would be invented — `updated_at`
  repeated, or steps spaced evenly.
- **A step behind the current one is not ticked as done.** `Docs/02` §2 permits skips:
  `Awarded → En route to pickup` is a permitted move, so a job may pass `Driver assigned` without a
  driver ever being nominated, and `Open → Awarded` skips `Negotiating` whenever the first bid is
  the accepted one. "Past this point" is true of a skipped step; "this happened" is not.
- **A cancelled or disputed job shows two steps, not ten.** The client knows two things about it —
  every job is created as `draft`, and this is where it is now — and eight greyed steps it may
  never have reached would be a screen filling silence with shape.

The one date it does show without qualification is the first step's, because the contract states
that a job is created as `draft`, so `created_at` *is* when it entered that step. The current step
carries `updated_at`, labelled "Last updated" rather than presented as a transition time.

**The screen says all of this out loud**, in one line under the timeline, because a timeline with no
dates and no explanation reads as a screen that failed to load them. When an endpoint serves the
history, `jobTimeline` takes it and the steps gain their real times; nothing else about the screen
changes, which is why the derivation is a pure function rather than logic inside a widget.

**"Available actions" is one action, because one endpoint exists.** `routes_golden.txt` serves three
customer job routes: read, edit-a-draft, and cancel. Editing belongs to the wizard (SHIP-72…75), and
**resuming an existing draft is SHIP-75 specifically, so this screen must not offer it** — the only
path into the wizard today creates a *new* draft, and a customer who tapped it from an existing one
would end up with two. So the action is cancel, confirmed first, and every status that offers
nothing says why instead of going quiet: "no actions available" reads as a fault in the app, where
"a provider has been awarded this delivery, and ending it now is a support matter" sends somebody to
support.

**The app hides; the platform decides, and the 409 path is where that is actually demonstrated.**
`actionsFor` offers Cancel for `draft`, `open` and `negotiating` because `Docs/02` §2 permits nothing
else towards `cancelled` from a customer — but it is a decision about what is worth showing, not a
permission check, and the request goes out regardless of what the client believes. When the platform
answers `409 jobs_not_cancellable` the screen renders the refusal **and re-reads the job**, which is
`Docs/02` §3.1's rule that on conflict the server wins and the app reconciles. The reload
deliberately **keeps the refusal on screen**: a first version cleared it, and the result was a
customer looking at a job that had silently changed under them with no account of why their tap did
nothing.

**`/jobs/{id}` is the first route in this client whose path carries an identifier**, so the
signed-in guard needed something a set of fixed strings could not give it. It has a pattern
alongside the set, matching one segment and nothing below it, and matching shape and nothing else —
it does not check that the id is a UUID, that the job exists, or that the caller owns it. All three
are the platform's (`Docs/07` §3), and a client-side pattern that looked authoritative is how a
guard stops being navigation and starts being a control nobody audited. `/jobs/new` stays the wizard
because it is declared first and go_router takes the first match, and a test asserts that ordering
rather than trusting it.

**The budget is drawn on a second screen now, and the allow list grew by one entry.**
`GET /v1/jobs/{id}` is owner-only and answers `404` to everybody else byte-identically to a job that
does not exist, so every job this screen can reach belongs to the person looking at it — which is
the question `budget_stays_on_the_customer_side_test.dart` asks, and the answer needs no "when" in
it. SHIP-83's provider job detail remains a separate screen reading a separate type.

**Housekeeping: sign-out now calls `POST /v1/auth/logout`, and §9's entry is closed.** The other
item §9 named — the `kDebugMode` development-session button — was already gone; SHIP-55 deleted it,
and §3's own row for SHIP-55 says so.

The logout gap was real and easy to demonstrate: before this, `signOut` cleared the Keychain and the
in-memory token and told nobody, so the discarded refresh token stayed valid server-side for up to
thirty days and the handset kept a row in `GET /v1/auth/sessions`. It is fire-and-forget, dispatched
and not awaited, and its failures are swallowed — `Docs/07` §3 has the device catching up rather
than asking permission, and a sign-out that failed because a train went into a tunnel would be a
defect rather than a safeguard.

**§9 called it "a handful of lines", and it is not, for two reasons neither obvious nor optional.**

*The request must not travel through `AuthInterceptor`.* The interceptor reads the session's access
token at **request** time, and `signOut` clears it at **call** time — so a fire-and-forget logout
dispatched through the ordinary client races the clear and usually loses, going out with no
credential and answering `401`. Worse, the interceptor's answer to a `401` is to refresh and replay,
which on the way out means minting a fresh session in order to end one, and calling `signOut` from
inside `signOut` when that refresh fails. So `SessionEnder` takes the access token as an argument
and sets the header itself, over the transport that carries no session — the token being thrown
away, spent on the request that makes throwing it away mean something.

*The endpoint answers `204`.* `ApiClient.postJson` raises `ApiMalformedResponse` for an empty body,
so a successful sign-out would have been reported as a broken response. Harmless while the only
caller ignores the result, and wrong the moment one does not, so `postNoContent` exists.

`SessionEnder` lives in `core/auth` beside `SessionRefresher` and for the same reason: signing out on
the platform is not a screen's action, and putting it on `IdentityRepository` would make `core/`
import a feature. Two paths pass `notifyingPlatform: false` — a device with no stored token has no
session for the platform to end, and a refused refresh is the platform having already said the
session is over, where telling it back would spend a request from a possibly signal-less device on a
credential it has just refused.

**What is still not closed:** an access token that expired before somebody tapped Sign out cannot
authenticate the call, and refreshing first would be a client minting a credential in order to
destroy one. Fifteen minutes is the window. `DELETE /v1/auth/sessions/{id}` (SHIP-46) remains how a
session that outlived its device is ended.

**How it was demonstrated.** `make flutter-check` in this worktree: **353 host tests**, up from 292
before this branch, analyzer clean, and the environment test green for all three flavours. Then
against this worktree's API on 8092 by `curl`, because the wire is where a client is actually wrong:
`GET /v1/jobs/{id}` returns the owner's job with `budget_cents` and **no history field of any kind**;
`POST /v1/jobs/{id}/cancel` with `{}` answers `200` with `status: cancelled` and a moved
`updated_at`; the same cancellation under a **fresh** key answers `200` again and records nothing
further; a job that is not the caller's answers `404 not_found`; `{"status": "open"}` on the
cancellation is refused `400 bad_request` with *unknown field "status"*, which is the never-a-settable-field
invariant holding from the client's side too; an address typed `"new south wales"` comes back `NSW`;
and `POST /v1/auth/logout` answers `204` with zero bytes after which the device's refresh token is
`identity_refresh_token_invalid`, which is the whole of what the sign-out change buys.

**Not demonstrated live: the `409` refusal, and the screens on a simulator.** A `409` needs an
awarded job, and no endpoint awards one until SHIP-92 — so that path is held by the widget test
alone. And as with SHIP-76, nothing here was driven on a device; the widget tests drive the real
router, guard, session, shell and list, so what a simulator would add is the platform channel and
the renderer.

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

### SHIP-84 — the domain opens, and "once" turns out to be two indexes rather than one check

`POST /v1/jobs/{id}/bids`. A verified, eligible provider offers a price and two commitments about
timing, and the platform records one offer per provider per job however many times the request
arrives. **This is the first code in `internal/bidding`**, which held `doc.go` and `model.go` and
nothing else since SHIP-10.

#### The eligibility question, and the port that needed no adapter

The *Done when* opens with "a verified, eligible provider", and eligibility is `fleet`'s: SHIP-81
built Docs/01 §4.3's four filters as one SQL predicate, and that file's own header names SHIP-84 as
the reader `Service.EligibleFor` was built for. **So this domain does not decide who may bid; it
asks.** `bidding/ports.go` declares a one-method `Eligibility` interface, `cmd/api/routes_bidding.go`
passes a `*fleet.Service`, and neither package names the other.

**Reimplementing the filter would have been the defect, not the import.** Two definitions of who may
bid is one more than Docs/07 §3 permits, and the two would part company the first time one was
corrected — leaving the endpoint that shows a provider a job and the endpoint that accepts their bid
on it disagreeing. That is the worst available outcome: a provider prices a job the feed offered them
and is refused after doing the work.

**The wiring is one argument and one assertion, and that is worth noticing rather than assuming.**
`routes_delivery.go` needs two adapter types, because a transition has four outcomes that cannot
cross the boundary without naming a `jobs` sentinel. This port answers a `bool`, which has no
vocabulary to translate, so `*fleet.Service` satisfies it structurally and the only other line is
`var _ bidding.Eligibility = (*fleet.Service)(nil)` — the compile-time proof, in the one file that is
allowed to know about both. **A port is only as expensive as the vocabulary it has to carry.**

#### Both biddable statuses, and the test that would have caught the other answer

`Docs/02` §1 makes **two** statuses biddable. Negotiating is "one or more active bids or counter-offers
exist; **job remains open to eligible bids**", and §1 adds the sentence that settles it: "Technically,
the job remains available for eligible bids unless the customer closes it or awards a bid."

Nothing can reach Negotiating until SHIP-90, **so an implementation accepting only `Open` would pass
every test written today and every check in `make verify`**. It would surface months from now as jobs
silently refusing bids the moment somebody negotiated, which reads as a bidding defect rather than as
a missing string in a filter. Wave 5 met the same shape in the feed and tested both;
`TestABidIsAcceptedOnBothBiddableStatuses` and a `make verify` section do the same here, moving a job
to Negotiating by hand precisely because no endpoint can. The list itself is not copied — it is
`fleet`'s `biddableStatuses`, read through the port.

#### "Once per job" and "a retry is not a second bid" are two different guarantees

They look like one requirement and they are two, with two indexes behind them and two very different
failure modes.

| Mechanism | What it guarantees | Failure if it were missing |
|---|---|---|
| `uq_bids_one_submitted_per_provider_per_job` | one live offer per provider per job | two prices from one provider on one job |
| `uq_bids_idempotency` on `(job_id, provider_id, idempotency_key)` | a retry is answered from the row it wrote | **a `409` for a request that succeeded** |
| `httpx.Idempotent` (Redis) | the response is replayed, handler never runs | nothing — it is the cheap path, not the correct one |

The second row is the one worth reading twice. Without the stored key, a provider whose bid was
recorded and whose response never arrived would retry, meet the *first* index, and be told "you
already have a live offer" — a client showing a failure for a bid that is live and awaiting an answer.
**Redis makes the retry cheap; the key column makes it correct**, which is the division SHIP-111 drew
for milestones, arrived at here from the opposite direction.

`make verify` tells the two apart the way 70-delivery.sh does: the same key three times, with
`redis-cli del` between the second and the third. `201`, then a byte-identical `201` carrying
`Idempotency-Replayed: true`, then a `200` answered from the row with no such header. One bid at the
end of all three.

**There is no SELECT anywhere in this package asking whether the provider has already bid.** A read
followed by a write is correct in a single-threaded reading and wrong under two taps on one phone.
`ON CONFLICT (job_id, provider_id, idempotency_key) … DO NOTHING` names only the idempotency index as
its arbiter, so a repeated key is absorbed silently while a *second* offer raises `23505` on the other
index and becomes `bidding_already_bid`. PostgreSQL's speculative-insertion protocol checks the
arbiter first, which is why a retry — which conflicts with both — is never mistaken for a second
offer. Eight goroutines under one key produce exactly one row, and exactly one of them believes it
created anything.

#### A constraint that was written, applied, and then removed

`ck_bids_offer_has_timing` — `status = 'Draft' OR (pickup_at IS NOT NULL AND deliver_by IS NOT NULL)`
— is the obvious twin of 000500's `ck_bids_offer_has_an_amount`, and it is **deliberately not in
`000501`**. It is worth recording because its absence looks like an oversight beside the constraint
directly above it.

It binds a design SHIP-87 has not made. A counter-offer is a new row in this table, and whether a
customer countering on *price alone* restates the timing or inherits it is that ticket's decision;
a constraint here settles it in the direction that costs a migration to undo. 000500 declined to write
the one-active-bid index for exactly this reason — "a partial index written against a guess at that
definition is a constraint the ticket would have to drop" — and the same reasoning applies pointing
the other way.

**The evidence was immediate rather than theoretical: it failed six of SHIP-80's own migration
tests**, which insert `Submitted` and `Accepted` bids to exercise `ck_bids_status` and
`uq_bids_one_accepted_per_job` and have no interest in timing. Making them pass would have meant
editing another ticket's test file to accommodate a constraint that ticket had considered and left
out — which is the signal that the constraint, not the test, was wrong. **Needing to edit another
ticket's tests to make your schema change fit is worth treating as evidence about the schema change.**

`ck_bids_timing_is_ordered` stays, and the difference is the test that separates them: no design
anybody could want has a delivery preceding its own collection, so that one binds nobody. "An offer
states its timing" is a product rule and lives in the validator, where it also produces a field error
naming `pickup_at` rather than a constraint violation a caller cannot act on.

#### Two instants, not two windows

The job carries two *windows*, because a customer says "any time Thursday". The bid answers with two
*instants*, because a provider says "I will be there at nine and it will be there by five". Three
reasons: Docs/03 asks for "comparable price, timing" and two instants sort where four numbers do not;
a window restates flexibility the customer already declared rather than committing to anything; and
widening an instant into a window later is additive, where narrowing a stored window is a data
migration with no honest answer for which end was meant.

**The bid's timing is deliberately not bounded by the job's windows.** A provider offering a different
day is making an offer the customer may decline, and Docs/01 §4.3's answer to bids that miss is better
job detail rather than a platform that refuses them.

#### Two privacy rules, and they fail in different places

Docs/01 §4.3 has two lines, and this is the first endpoint where both bite at once.

**The customer's budget** is kept out by there being no job in the response at all — a bid names its
job and copies nothing from it. That is stronger than redaction, and
`TestTheBidResponseCarriesNothingOfTheCustomers` holds the serialised bytes to a **closed set of keys
at every depth**, so a field arriving as `max_price` fails as surely as one arriving as
`budget_cents`. Verified by mutation in both directions, as SHIP-83 asks: a `max_price` field carrying
`432199` fails on the key set *and* on the value search, and the test refuses to run at all against a
fixture whose budget is NULL.

**One provider never seeing another's offer** is the second rule, and **its failure mode is a `WHERE`
clause rather than a response field** — which is why the closed key set cannot cover it. A store read
scoped by job and idempotency key but not by provider hands the second provider the first's bid, price
included, and every key in that response is one this API promises a provider. So
`uq_bids_idempotency` and `postgresStore.bidPlacedUnder` are both scoped `(job_id, provider_id,
idempotency_key)`, and `TestOneProvidersKeyCannotReachAnothersBid` sends **the same key from two
providers** — which is how a competitor would probe for it. Also verified by mutation: dropping
`provider_id` from that one `WHERE` fails the test and nothing else in the suite.

#### Placing a bid does not move the job

`Docs/02` §2 has `Open → Negotiating` on "first bid or counter-offer submitted", and that is
**SHIP-90's**, which depends on SHIP-87 and SHIP-57. Doing it here would make every bid a status
transition, with a `job_status_history` row and a lock on the job, for a presentation change nothing
reads yet. The test asserts both halves — the job is where it was, and its history is unchanged —
because the second is what would catch a move made through some other path.

**No event is emitted either.** SHIP-136 adds bidding's events from a `events.go` exactly like jobs',
editing nothing shared; emitting one now would mean inventing a payload and a version for a catalogue
entry that ticket owns.

#### `bidding` is now the second domain storing an amount independently

`bids.amount` is `numeric(12,2)` and `Bid.AmountCents` is an `int64`, converted in SQL in both
directions — `jobs`' convention for `budget` (Docs/10 §3.3), copied rather than shared.
`maxOfferCents` is `100_000_000`, the same number as `jobs.maxBudgetCents`, arrived at independently.

**That duplication is the trigger the eventual `money` ticket should be aimed at, and this is the
entry recording it.** `internal/money` has been registered in `internal/boundaries` and unwritten
since SHIP-15c; it is now the only pre-seeded package left unbuilt. Writing it is a shared-file edit
a domain branch may not make, so this ticket copied the convention deliberately rather than smuggling
the package in. The trigger has now fired twice — once here and once at the constant — and the third
domain to hold an amount should not have to make the case again.

#### What SHIP-80 handed this ticket and it declined to take

000500 named four things for SHIP-84, and one is not built: **the vehicle or vehicles a bid is offered
on**. Docs/01 §4.2 gives the provider "select one or more", which 000500 read as a join table "if it is
taken literally — and that is SHIP-84's decision to take."

**Declined, with the reasoning recorded rather than the decision deferred silently.** It is not in
this ticket's *Done when* ("price and timing"); Docs/01 §4.3 wants it for the *customer's* comparison,
which is SHIP-102 by way of SHIP-96; and validating it needs a second `fleet` fact — that the vehicle
is the caller's and in service — and therefore a second port method, which is more design than a
three-point ticket should be taking on another ticket's behalf. **The trigger is named: the first
ticket that shows a customer a bid.** `bids` has no vehicle column and no join table, so nothing has
to be undone.

#### Shared surfaces

One `$ref` pair and one `tags:` entry in `contracts/openapi.yaml`, one line in `routes_golden.txt` and
one in `Docs/10-api-error-codes.md` (both regenerated, not typed), and §3's check count (written by
`make verify-update`). No `internal/boundaries` edit, no `Deps` field, no shared-block migration, and
`internal/bidding` imports no domain. `make verify` went from 355 checks across 12 sections to 378
across 13 — twenty-three checks in a new `scripts/verify/61-bidding.sh`, which takes the reserved
60–69 range's upper half beside `60-fleet.sh`.

### SHIP-85 — a revision is an `UPDATE`, and the key it must not touch

`PATCH /v1/jobs/{id}/bids/{bid_id}`. A provider changes the price, timing or conditions of an offer
they have already placed. **The row keeps its identifier, its `Submitted` status and the key it was
placed under**, which is what makes this the same offer at a new number rather than a second offer.

Docs/01 §4.2 gives the provider three verbs — "place, update, and withdraw a bid until it is accepted
or expires" — and this branch built the second and third of them on top of SHIP-84's first.

#### It is an in-place edit, and the alternative would have settled SHIP-87's design

The tempting shape is the one SHIP-87 will need: write a new row and move the old one to `Superseded`.
It is rejected, and for the reason 000500 and 000501 both give for declining to guess — **a
counter-offer is what makes a new row** (Docs/02 §4: "a customer counter-offer supersedes the prior
provider offer"), the link between rows is SHIP-88's supersede chain, and there is no column for one.
Building half of that chain here would hand SHIP-87 a design it did not choose and a migration to undo.

Docs/02 §4 also never says a provider revising their *own* offer supersedes anything. There is no
counter from the other party to supersede: the customer has not answered yet, which is what
`Submitted` means.

**What this does cost is that the intermediate prices are not retained.** Docs/01 §4.3 requires the
platform to "record all offers, counter-offers, withdrawals, and acceptances", and a revision is
none of those three named things — it is an edit to an offer nobody has answered.

**~~If SHIP-88 decides the chain should record revisions too, that is an additive change.~~ SHIP-88
decided, and it does not** — see its entry below. The reasoning it added to this one: making a
revision write a chain entry would leave the three verbs indistinguishable in the history, and a
provider who dropped their price twice before anybody answered would read as a negotiation with
themselves. The chain records every round somebody *answered*, which is what Docs/01 §4.3's list
actually names. The additive route stays open if a later ticket wants the intermediate prices for
some other reason.

#### The key the revision must not write, and the mutation that proves it

`bids.idempotency_key` holds the key the offer was **placed** under. 000501 added it so that a
placement retried after Redis has forgotten it is answered from its own row rather than meeting
`uq_bids_one_submitted_per_provider_per_job` and being told the provider already has a live offer.

**Writing a revision's key over it reintroduces exactly that failure**, and it is the kind of defect
that looks like tidiness. `postgresStore.reviseOffer` names four columns and that is deliberate;
`TestARevisionDoesNotConsumeThePlacementsKey` places under one key, revises, and then sends the
*placement* again. Verified by mutation: adding `idempotency_key = $6` to the `SET` list makes that
test fail with `bidding_already_bid` — a `409` for a request that had succeeded — and nothing else in
the suite notices.

**So this endpoint stores no key at all**, which is the second half of the same finding. A revision is
an `UPDATE` of a row that already exists: applying it twice reaches the state applying it once
reaches, there is no second row for a repeat to create, and no constraint for it to trip. That is the
natural idempotency `PATCH /v1/jobs/{id}` and `PATCH /v1/fleet/vehicles/{id}` already rely on, and
neither of those stores a key either. **Redis makes the retry cheap and the `UPDATE` makes it
correct** — the same division SHIP-84 drew, reaching the opposite conclusion about whether a column is
needed, which is what makes it a division rather than a habit.

#### The whole offer is validated, not only the fields that changed

`Revision.applyTo` merges the request over the stored bid and produces an `Offer`, so the *same*
validator runs. An offer that could not be placed today is therefore not reachable by revising one
that could be placed yesterday.

The consequence is worth stating because it surprises: a provider re-pricing a three-day-old bid whose
`pickup_at` has since passed is refused, **naming a field they did not send**. That is the honest
answer — what they are asking the platform to keep live is an offer to collect in the past, and the
customer could accept it — and validating only what arrived would make the stored offer's coherence
depend on the order somebody edited it in.

#### Eligibility is checked again here and deliberately not on the withdrawal

The one asymmetry in the pair. **A revision produces a live offer at a new number that the customer may
accept the moment it lands**, so it goes through SHIP-81's filter exactly as a placement does: a
provider whose only vehicle left service must not be able to re-price work they can no longer do, and a
job that has been cancelled or awarded elsewhere must not acquire a fresh price. Docs/07 §3 puts that
decision server-side in one place, and there is only one.

That matters more than it looks, because **nothing closes a bid when its job ends** — SHIP-93 is the
ticket that rejects competing bids on award, and it does not exist. Without this check a provider could
re-price an offer against work that is over.

The refusal is the same `404` a placement gets, byte-identically. It reads oddly to a caller who
plainly knows the job exists, and it is still right: the platform must not tell one provider that a job
was awarded, cancelled or expired, because that is what became of work somebody else was given.

#### "Their own" is the whole of the authorisation, and it is two comparisons

`Service.ownBid` reads the row **by its identifier** and then compares `provider_id` and `job_id` in
Go. A `WHERE … AND provider_id = $2` returning nothing could not tell a competitor's bid from a bid
that does not exist, and only one of those is somebody probing — the arrangement `fleet.Service.owned`
takes, and the discipline SHIP-84 established by deleting `provider_id` from a `WHERE` and watching one
test fail.

Both comparisons were mutation-tested. Deleting the ownership check fails
`TestOnlyTheBidsOwnerCanReviseIt`, `TestOnlyTheBidsOwnerCanWithdrawIt` and
`TestAnotherProvidersBidIsUnreachable`; deleting the job comparison fails
`TestABidIsAddressedUnderItsOwnJob`. **The second is the one that is easy to leave out**, and without
it `/v1/jobs/{id}/bids/{bid_id}` would be one resource reachable at as many addresses as there are
jobs — the shape where a rule gets enforced at one address and forgotten at the rest.

#### Shared surfaces, for both tickets on this branch

Two `$ref` entries in `contracts/openapi.yaml` (one per new path), two lines in `routes_golden.txt` and
two in `Docs/10-api-error-codes.md` — all four regenerated rather than typed — and §3's check count,
written by `make verify-update`. **No migration**: a revision writes columns 000501 already added and a
withdrawal writes a status `ck_bids_status` has held since 000500, so block 500–599 is untouched and
the out-of-order guard never fires. No `internal/boundaries` edit, no `Deps` field, and
`internal/bidding` still imports no domain.

### SHIP-86 — withdrawal is idempotent by state, which is stronger than by key

`POST /v1/jobs/{id}/bids/{bid_id}/withdraw`. The offer becomes `Withdrawn`, the customer can no longer
accept it, and **the row survives with its price intact**. Docs/01 §4.3 requires every withdrawal to be
recorded and Docs/02 §4 keeps bid history readable to the customer, the bidding provider and
administrators — so there is no delete on this table and there is not going to be one. The same reading
`fleet` gives a deactivated vehicle.

A verb rather than a `DELETE`, and not a `PATCH` writing `"status": "withdrawn"`: a bid's status is the
platform's, the client names an intent, and `httpx.DecodeJSON` refuses the field outright. The shape
`POST /v1/jobs/{id}/cancel` and `POST /v1/fleet/vehicles/{id}/deactivate` already use.

#### Withdrawing twice succeeds, and that is what makes a retry safe

The question the ticket turns on is what a *retry* of a withdrawal means. The idempotency middleware
absorbs the one that reuses its key; it cannot absorb the one that does not — a phone that lost its
connection, was restarted, and generated a **fresh** key for the same intent, which is the ordinary
shape rather than an exotic one. Refusing that with "this offer is no longer live" would tell a
provider their withdrawal failed when it succeeded.

So a second withdrawal answers `200` with the bid and **writes nothing further**. `jobs` makes the same
call for a repeated cancellation and `fleet` for a repeated deactivation, and both say why: the caller
asked for an outcome, and the outcome holds.

**This is a stronger guarantee than a stored key gives, and it is why no column was needed.** A key
scopes idempotency to one client's one request; state scopes it to the outcome, so two different
clients with two different keys still cannot withdraw one offer twice. `make verify` asserts the row's
`updated_at` across all three repeats — status alone could not tell an absorbed request from one that
rewrote `Withdrawn` over `Withdrawn`.

Verified by mutation: removing the three-line absorption makes the second withdrawal `409
bidding_bid_closed`, failing both the service test and the wire test.

#### Before acceptance, and what happens after is a different ticket

An accepted offer answers `bidding_bid_accepted` and is neither revised nor withdrawn. Docs/01 §4.2
draws the line in the sentence this branch is built on — "until it is accepted or expires" — and
CLAUDE.md's one-accepted-bid invariant is what stands behind it: `uq_bids_one_accepted_per_job` means
an award is a commitment two parties hold, not a state one of them leaves unilaterally.

**Withdrawal after acceptance is a different thing entirely.** Docs/02 §6.2 makes a provider stepping
away from awarded work a *provider cancellation*: the job moves back to Open, every bid closes, and the
cancellation is recorded against the provider. Allowing it here would be that flow with none of its
consequences, and it would break the award silently — the job still `Awarded`, to a bid nobody could
see.

#### `bidding_bid_accepted` and `bidding_bid_closed` are two codes because they are two screens

`bidding_bid_accepted` is good news: the provider won the job, and the app's next screen is that job.
`bidding_bid_closed` covers `Rejected`, `Expired`, `Superseded` — and `Withdrawn`, for a revision —
where the offer is over and the app shows the feed. One code for four statuses, because the client does
the same thing with all four; which of them it was belongs to the provider's own bid list (SHIP-101),
not to an error code.

Docs/10 §4.4's test still applies: a domain code earns its place only where a client would otherwise
parse a message to know what to do. `bidding` had one code at SHIP-84 and has three.

#### No eligibility check, which is the deliberate half of the asymmetry

**A provider must always be able to take back their own offer.** Refusing a withdrawal because their
only vehicle left service, or their verification lapsed, or the job was cancelled underneath them,
would strand a live offer the customer can still accept and the provider can no longer retract — the
worst of both answers. SHIP-81's filter governs what a provider may *offer*; it has no business
governing what they may stop offering. `TestAWithdrawalNeedsNoEligibility` breaks four different
filters and expects a withdrawal through each.

#### Neither verb moves the job, and one of them looks like it should

Docs/02 §2 has `Negotiating → Open` on "all active bids expire, are withdrawn, or are rejected", which
reads like an instruction to this ticket. It is **SHIP-90's**, in both directions — nothing reaches
`Negotiating` until that ticket exists, so there is nothing to move back from. And job status is never
a settable field in any case: a transition passes one guarded function and leaves a
`job_status_history` row in the same transaction, so doing it here would mean doing the half without
the record. Both the test and a `make verify` check assert the job's status *and* its history count,
because the second is what would catch a move made through some other path.

**SHIP-84 left this ticket a property to confirm rather than to build.** It wrote
`TestAWithdrawnOfferCanBeReplaced` against a hand-set status with the note "SHIP-86 should find this
already true"; it did, and the hand-set status is now `Service.WithdrawBid`. A provider who withdraws
leaves `uq_bids_one_submitted_per_provider_per_job`'s predicate and may bid again — a fat-fingered
price is not a job lost forever.

#### The transaction, and the lock that is the reason for it

`ReviseBid` and `WithdrawBid` both refuse a connection pool. `PlaceBid` does not, and the difference is
the mechanism: a placement's correctness is `ON CONFLICT`'s and holds statement by statement, while
these two read a status, decide against it, and write. `postgresStore.lockBid`'s `FOR UPDATE` is what
makes that one decision, and outside a transaction the lock is released the instant the `SELECT`
returns — leaving an award free to commit in the window, and a withdrawal to unpick a bid
`uq_bids_one_accepted_per_job` says two parties are committed to, with nothing to report afterwards.

#### The budget rule now has four responses rather than two

`TestTheBidResponseCarriesNothingOfTheCustomers` held the `201` and the `200` replay to a closed set of
keys at every depth. It now holds four: the revision and the withdrawal answer with the same shape from
two more code paths, and **a shape that is safe on one path and not another is the failure several
paths invite**. Verified by mutation, as SHIP-83 asks: a `max_price` field carrying `432199` fails on
all four subtests, on the key set and on the value search. `make verify` runs the same closed-set
assertion from outside Go against both new responses.

### SHIP-87 — one endpoint, both parties, and the column that had to be reinterpreted

`POST /v1/jobs/{id}/bids/{bid_id}/counter`. Either party answers the other's offer with different
terms; the offer they answered becomes `Superseded` and the counter becomes the live head of the
negotiation. **This is the first endpoint in `internal/bidding` a customer may call** — every one
before it is the provider's alone.

Docs/02 §4 gives the two directions in two sentences — "a customer counter-offer supersedes the prior
provider offer. A provider counter-offer supersedes the prior customer offer" — and they describe one
act, so there is one route. Two would have been two authorisation rules to keep in step, and they
would have parted company the first time one was corrected.

#### `bids.provider_id` stops meaning "who wrote this row", and that is the cheapest of the options

A negotiation is one `(job_id, provider_id)` pair, and every row in a chain carries it — **including
the customer's counters**. So `provider_id` now names the provider a negotiation is *with*, and a new
column `offered_by` names the author.

The alternative was to put the customer's id in `provider_id` for their own rows, and it is much
worse: `uq_bids_one_submitted_per_provider_per_job` would stop meaning "one live offer in this
negotiation", `idx_bids_provider` would stop serving the provider's own bid list, and
`fk_bids_provider` would point at a customer. **One column changes meaning; four objects keep
working**, and `uq_bids_one_submitted_per_provider_per_job` in particular keeps doing the job 000501
built it for without the widening that migration offered — "SHIP-87 and SHIP-88 move the prior offer
out before writing the next" turned out to be exactly right.

**The reinterpretation opens one hole and closing it is the sharpest thing in this ticket.** A
customer's counter carries the provider's id, so `Service.ownBid`'s comparison passes for the
provider — and without an authorship check they could have `PATCH`ed the customer's counter, or
withdrawn it. One party editing the other's offer is the worst available failure in a negotiation, and
it would have looked exactly like SHIP-85 working. `changeable` gained one line;
`TestTheOtherPartysOfferIsNotRevisableOrWithdrawable` and two `make verify` checks are what say so.

#### The widening is a separate path, not a loosened `ownBid`

`Service.reachableBid` is new rather than `ownBid` being relaxed, and the distinction is the whole
reason SHIP-87 did not quietly widen SHIP-85 and SHIP-86. `ownBid` refuses a customer by design — run
2's `TestOnlyTheBidsOwnerCanReviseIt` asserts it *including for the job's own customer* — and a
customer added there would have been added to `PATCH` and `withdraw` at the same time. That is the
shape where a rule is relaxed for one endpoint and silently relaxed for three.

Both comparisons `ownBid` makes are made in the new path too, for the reasons that file already gives:
the row is read by its identifier and judged in Go, so "somebody else's" and "nobody's" stay different
facts behind one 404; and the job in the path is compared, because a bid is addressed under its own
job.

#### A counter inherits what it does not restate, which is `000501`'s deferred decision

That migration removed `ck_bids_offer_has_timing` in as many words: "whether a customer countering on
*price alone* restates the timing or inherits it from the offer it supersedes is that ticket's
decision." **It inherits**, for the reason `Revision` gives about a revision and one more that is
specific to a negotiation: the unchanged fields *are* the agreement so far, and a shape that made both
parties restate them would turn every round into a fresh offer that happened to look similar — which
is the thing "supersedes the prior offer" says a counter is not.

`Counter` and `Revision` share one merge and one validator, so a counter cannot reach a state a
placement could not. The consequence surprises in the same way: countering on price alone against an
offer whose `pickup_at` has since passed is refused, naming a field the caller did not send.

**The constraint that question was blocking is still not added, and the trade has changed rather than
disappeared.** Every offer this platform writes past `Draft` now names both instants, so
`ck_bids_offer_has_timing` would be true — but 000501's *other* finding still holds: it failed six of
SHIP-80's own migration tests, which insert `Submitted` and `Accepted` bids with no timing to exercise
`ck_bids_status`, and making them pass means editing another ticket's test file. **The question is
answered and the constraint is a separate, small ticket**; it is recorded here rather than taken.

#### The two sides are checked against different questions

A **provider's** counter is a live offer the customer may accept the moment it lands, so it goes
through SHIP-81's filter exactly as a placement and a revision do. The refusal is the same `404`.

A **customer's** counter cannot be accepted at all — `ck_bids_only_a_providers_offer_is_accepted`
makes that a database fact — so eligibility is the wrong question and would be asked of the wrong
account anyway. What matters is whether the negotiation can still end anywhere, and the port asks
`jobs` whether the job could still be **awarded**.

**That phrasing is the point, and it is what kept a third copy of the biddable-status list out of the
service.** `jobs` holds Docs/02 §2's transition table in one place and exports `Permitted`, so
`AwardableBy` is `jobs.Permitted(job.Status, jobs.StatusAwarded)` — which selects `Open` and
`Negotiating` today and follows that document automatically if it ever changes. Writing the two
strings in `cmd/api` instead would have put a third copy after `jobs`' table and
`fleet.biddableStatuses`, and the third copy is the one nobody remembers to correct.

The refusal is `bidding_bid_closed` rather than a code of its own, because the client does the same
thing with it as with a superseded offer: the negotiation is over. It is also a temporary shape —
once SHIP-93 closes competing bids on award, the offer itself is `Rejected` and reaches that code
directly.

#### One new error code, and the two that were deliberately reused

`bidding_wrong_party`. **You counter the other party's offer and revise your own**, and both
directions of getting that backwards land on it — countering your own live offer, and revising or
withdrawing one the other party made. It earns a code by Docs/10 §4.4's test because the client's
correct response is a *different request* rather than a different screen.

The other two new refusals reuse. A negotiation the customer can no longer award is
`bidding_bid_closed`; a counter naming no field is `bad_request` with its own message, because a
counter that changes nothing is agreement rather than a client defect and the next screen is the
award. `bidding` had one code at SHIP-84, three at SHIP-86 and has four.

It is `409` and not the `404` a stranger gets, deliberately: the caller is a party to this negotiation
and can read the offer in the negotiation's history a moment later, so hiding it here would be an
inconsistency rather than a disclosure control.

#### The retry has to be answered before the status is checked

A counter is an `INSERT`, so SHIP-84's argument applies where SHIP-85's did not: the key is a
*column*, and a retry that outlives Redis is answered from the record rather than adding a second link
to the chain.

**The ordering is the part worth reading twice.** By the time a retry arrives, its own first attempt
has already superseded the offer it answered — so a status check in front of the key lookup would
refuse the caller's own successful request with "that offer is no longer live". `Service.PlaceBid`
puts its retry first for the same reason and states the principle: a retry asks what happened to a
request, and that answer does not change when the world does.

`uq_bids_idempotency` gained `offered_by`, which is 000501's own argument extended rather than
reversed. That index carries `provider_id` because "two providers whose clients happened to generate
the same key would collide, and the second would be handed the first's bid"; a negotiation now has two
writers inside one `(job_id, provider_id)` pair and the identical sentence applies to them. It is a
strict widening — every row written before 000502 is a provider's offer — and
`TestOneKeyFromEachPartyIsTwoDifferentCounters` sends the same key from both sides, which is how the
defect would be found.

#### Countering does not move the job, and this is the ticket where that reads oddest

Docs/02 §2 has `Open → Negotiating` on "first bid or **counter-offer** submitted". A counter is
literally the second half of that sentence, and it is still **SHIP-90's**, which depends on this
ticket and on SHIP-57 and owns the presentation status in both directions. Job status is never a
settable field in any case: a move passes one guarded function and leaves a `job_status_history` row
in the same transaction, so doing it here would be doing the half without the record. Both the test
and a `make verify` check assert the status **and** the history count, because the second is what
catches a move made through some other path.

#### Shared surfaces

One `$ref` pair in `contracts/openapi.yaml`, one line in `cmd/api/routes_golden.txt` and one in
`Docs/10-api-error-codes.md` (all regenerated, not typed), and §3's check count, written by
`make verify-update`. One migration, `000502`, in bidding's own block 500–599 — which tripped
SHIP-15g's out-of-order guard on the first `make migrate-up` exactly as predicted, at a database on
`602`.

**The guard's printed fix needed one thing the guard does not say, and it cost a dirty database to
find.** `make migrate-down n=all` walks the file list downward from the current version, so it runs a
*new* migration's `down` **before that migration's `up` has ever run** — which fails on the first
`DROP INDEX` of an index that does not exist, and leaves the schema dirty partway through. `000302`,
`000407` and `000602` all use `IF EXISTS` throughout for exactly this reason; `000502` now does too,
and its header says why. **Whoever writes the next below-the-line migration should start from that
rather than discovering it**, and it is worth `scripts/`'s guard message eventually saying so.

No `internal/boundaries` edit and no `Deps` field. `internal/bidding` still imports no domain: the
customer-side facts arrive through a second port, satisfied in `cmd/api` by a small adapter over
`*jobs.Service`.

### SHIP-88 — the chain link goes on the displaced row, and that is why a `CHECK` can hold the award

SHIP-88's *Done when* is "only the latest valid offer is acceptable; full chain remains readable", and
it is two halves with two very different mechanisms: a pair of column constraints, and a read.

#### The link points backwards, and the choice was made on what it makes possible

Two representations were available and 000500 named both — "whether that is a self-referencing column
or a separate offer table is SHIP-87 and SHIP-88's design".

| Where the link lives | What "has this offer been displaced?" becomes |
|---|---|
| `supersedes_bid_id` on the **new** row | a question about some *other* row — no single-row constraint can ask it |
| `superseded_by` on the **displaced** row | a fact **in the row itself** — an ordinary column `CHECK` can |

**The second wins because of what it makes possible, not because of what it stores.**
`ck_bids_superseded_is_not_live` — `superseded_by IS NULL OR status NOT IN ('Submitted','Accepted')` —
is what turns Docs/02 §4's "only the latest valid offer can be accepted" into something SHIP-92
physically cannot violate. An award that writes `status = 'Accepted'` over a countered offer, because
it read the status before taking its lock or did not re-read it at all, is refused by PostgreSQL. The
other direction would have left that as application logic with a comment asking somebody to keep it.

`Submitted` is in the list as well as `Accepted`, doing separate work: a displaced row cannot re-enter
`uq_bids_one_submitted_per_provider_per_job`'s predicate, so **the live offer and the head of the chain
are the same row by construction** rather than by agreement between two writers.

**A separate `offers` table was rejected**, and the reason is `uq_bids_one_accepted_per_job`: it is an
index on `bids`, SHIP-91 was built against it, and the whole award branch is designed around it
(Docs/11 §8). Moving offers out would mean either moving that index to a table nobody wrote it for or
keeping the accepted amount in two places. Every status in `ck_bids_status` is a status of an *offer*;
the table has been the chain all along and only lacked the link.

#### The second constraint is the one counters made necessary

`ck_bids_only_a_providers_offer_is_accepted` — `status <> 'Accepted' OR offered_by = 'provider'`.

With a chain, the head of a negotiation is sometimes the **customer's own offer**, and awarding that
would bind a provider to a price and a date they never agreed to, with `uq_bids_one_accepted_per_job`
allowing it and nothing else noticing. Docs/02 §1 defines Awarded as "Customer has accepted one
**provider** bid; provider commitment exists", and this constraint is that sentence.

It costs a customer nothing: a customer who wants their own number accepted waits for the provider to
counter at it, and *that* row is the provider's commitment and is awardable. `make verify` asserts
both refusals and then awards the provider's head successfully, so the pair is a shape rather than a
wall.

#### What SHIP-92 may rely on, and the lock ordering it should take

Docs/11 §8 warns that two people produce two lock orderings and that is a deadlock or a lost update.
**There is one obvious answer and this is it, written down.**

*What the database enforces, whatever the award remembers:*

| Guarantee | Mechanism |
|---|---|
| At most one `Accepted` bid per job | `uq_bids_one_accepted_per_job` (SHIP-80/91) |
| A displaced offer can never become `Accepted` or `Submitted` | `ck_bids_superseded_is_not_live` |
| Only an offer a **provider** made can be `Accepted` | `ck_bids_only_a_providers_offer_is_accepted` |
| At most one live offer per `(job, provider)` | `uq_bids_one_submitted_per_provider_per_job` (SHIP-84) |
| A chain is a list — no offer is displaced by a counter that displaced another | `uq_bids_one_successor` |

*What application logic still has to do:* **check that the bid it is accepting is `Submitted`.** No
constraint can express "only a live offer becomes accepted", because that is a statement about a
transition rather than about a row, and 000500 deliberately declined to give `bids` the transition
trigger `jobs` has. What the constraints do is make every *other* way of getting it wrong impossible.

*The lock ordering:*

1. **`jobs` first** — `SELECT … FROM jobs WHERE id = $1 FOR UPDATE`. It is the outermost lock and the
   only one shared with the status guard and `job_status_history`.
2. **Then the bid being awarded, by its identifier** — `FOR UPDATE`, and re-read `status`,
   `superseded_by`, `offered_by` and `job_id` under it.
3. **Then the accept** — the write that takes the `uq_bids_one_accepted_per_job` btree entry, which is
   what serialises two awards that somehow got past step 1.
4. **Then the rejection sweep** (SHIP-93), ordered by `id` if it locks explicitly.
5. **Then the transition**, through `jobs`' one guarded function, in the same transaction.

**Everything else in `bidding` locks only its own `bids` rows, by identifier, and never locks a `jobs`
row.** That is true of the counter, the revision and the withdrawal today, and it is what makes the
ordering acyclic: `jobs` → `bids` in one direction, and nothing going the other way. A future writer in
this domain that needs a `jobs` lock has to take it *first*, or reintroduce the cycle.

There is one window this does not close and SHIP-95 owns it: a counter whose eligibility read
committed before an award and whose insert lands after it leaves a live offer on an awarded job. It is
harmless — nothing can accept it, and `uq_bids_one_accepted_per_job` blocks a second award — and it is
the "withdraw-during-award" family by another name. Closing it properly means the counter taking a
`jobs` lock, which is the cycle, or SHIP-93 running last, which is where it already is.

#### The concurrent-counter race, and what it actually proved

`TestConcurrentCountersLeaveExactlyOneLiveOffer` runs eight goroutines against one live offer under
**eight different keys** — one key would be answered as a retry and would prove the idempotency path
instead. Exactly one succeeds, the other seven are refused with `bidding_bid_closed`, and the
negotiation holds two rows with one live offer and one successor.

Three mechanisms stand behind that and the test deliberately does not care which answers: `lockBid`'s
`FOR UPDATE` serialises the transactions, `supersedeHead`'s compare-and-set matches nothing for the
loser, and `uq_bids_one_submitted_per_provider_per_job` refuses the second live offer at the index.
**The first is what makes the refusal legible; the last is what holds if the first two are ever
removed.**

**One claim was written and then corrected, which is worth recording.** The migration first said
`uq_bids_one_successor` was the guard against that race. It is not: the loser's second row would be
refused by 000501's index, not by this one. What `uq_bids_one_successor` actually guarantees is the
direction the column shape does not give — *at most one predecessor per offer*, since "at most one
successor" is already true of a single column. Two offers naming one counter as what displaced them
would be a negotiation that merged rather than a chain, and `TestAChainCannotMergeOrPointAtItself` and
a `make verify` check are what say so. Overclaiming which index does what is exactly the kind of thing
SHIP-92 would then design against.

#### "Readable" needed a read, and it is deliberately not SHIP-96's or SHIP-102's

`GET /v1/jobs/{id}/bids/{bid_id}/history`, addressed through **any** offer in the negotiation, oldest
first. Docs/02 §4 keeps bid history "visible to the customer, bidding provider, and administrators",
and the administrator's third of that is **SHIP-96's** along with the visibility rules in full.

It is emphatically not `GET /v1/jobs/{id}/bids`, which `routes_bidding.go` reserved for **SHIP-102's**
customer comparison at SHIP-84. That is a different resource with a different privacy rule — every
provider's offer side by side, where this is one negotiation's — and serving it here would have taken
that ticket's design.

**The chain read is a negotiation, not a walk along the links**, and that is a decision rather than a
shortcut. SHIP-86 established that a provider who withdraws may bid again, so a negotiation
legitimately holds rows outside any one link chain. Following `superseded_by` from the first row would
omit exactly the rows somebody is most likely to be asking about, and Docs/01 §4.3 requires every
withdrawal to be *recorded*, not merely retained where a query happens to look.
`TestAWithdrawnOfferAndItsReplacementAreBothInTheChain` is that.

It asks `CustomerOf` and deliberately not `AwardableBy`: **a record is at its most useful once the job
is over** — the customer reconstructing why they awarded elsewhere, the provider checking what they
committed to, an administrator handling a dispute. A read gated on the job still being live would go
dark exactly then.

#### The budget and a counter amount are different things, and `Docs/01` §4.3 does settle it

This is the shape this domain has been most careful about, and SHIP-88 is where it bites hardest: **a
counter-offer is an amount the customer chose, in a response a provider reads.**

They are different for a reason the document states rather than implies. Docs/01 §4.3's decision is
about the customer's *maximum*: "The customer's maximum budget is **private**. Providers never see it
— not as an amount, not as a band, and not as a 'budget supplied' indicator." The reasoning it gives
is asymmetry — "if providers can see the maximum, bids converge on it, which defeats the competitive
pricing that is the marketplace's purpose". A counter-offer is the opposite artefact: a number the
customer **deliberately put in front of this provider**, in a negotiation the same document requires
the platform to record (§4.3: "record all offers, counter-offers, withdrawals, and acceptances") and
Docs/02 §4 requires to stay visible to both parties. Withholding it would leave a counter-offer
endpoint whose counters nobody can read.

**`Docs/01` was clear enough to settle it, and no document needs changing.** What is worth telling a
customer, and is a product note rather than a defect: **a customer who counters at exactly their
budget has disclosed their budget**, by their own act. The platform's obligation is not to leak it;
it is not to prevent the customer offering it. If the pilot shows customers doing that habitually,
the fix is a warning in the client at SHIP-103, not a change here.

The mechanism is unchanged and was extended rather than trusted.
`TestTheBidResponseCarriesNothingOfTheCustomers` now holds **six** responses to a closed set of keys
at every depth — the two SHIP-84 had, SHIP-85's and SHIP-86's, and now the counter and the history —
and `make verify` makes the same assertion from outside Go against both new shapes. The counter in
every fixture is placed at a number that is not the budget in any rendering, so the value search stays
meaningful.

**The closed set did its job before a line of the new tests was written.** Adding `offered_by` and
`superseded_by` to the response failed all four existing subtests immediately, which is exactly the
cost it exists to impose: both keys were then added deliberately in three places — the Go list, the
`Bid` schema, and the verify script — rather than arriving with a schema change nobody read.

#### `Countered` is never written, and Docs/02 §4 is ambiguous about why it exists

Docs/02 §4 lists both `Countered` and `Superseded` and describes them in almost the same words — one
offer "answered with a different price or timing", the other "displaced by a counter from either
party". Those are the same event seen from the two ends, and a platform that wrote both would be
writing two statuses for one transition and would then owe clients an answer about which to branch on.

**`Superseded` is the one written, and the published vocabulary had already chosen it**: `CodeBidClosed`
and the `BidNoLongerYours` response in `contracts/paths/bidding.yaml` have both enumerated "rejected,
expired, superseded" since SHIP-85 and neither names `Countered`. This ticket noticed rather than
overlooked. `Countered` stays in `ck_bids_status` because Docs/02 §4 has it and Docs/10 §3.4 pairs the
list with the constraint in both directions, and the trigger for writing it is named in `model.go`: a
ticket that needs to distinguish "displaced because the other party answered" from some other way of
being displaced. There is no other way today. **Reported as a documentation ambiguity rather than
resolved in the document** — see §9.

#### What SHIP-85's entry left open, answered

That subsection recorded a cost — "the intermediate prices are not retained … if SHIP-88 decides the
chain should record revisions too, that is an additive change" — and this is the ticket that decides.

**A revision is still an in-place `UPDATE` and does not become a chain entry.** Docs/02 §4 never says
a provider revising their *own* offer supersedes anything, and it is right: there is no counter from
the other party to supersede, which is what `Submitted` means. Making a revision write a row would
also make the three verbs indistinguishable in the history — a provider who dropped their price twice
before anybody answered would look like a negotiation with itself.

What the chain does record is every round anybody **answered**, which is what Docs/01 §4.3's list
names. SHIP-85's subsection has been amended to point here rather than left holding an open question.

#### Shared surfaces

One `$ref` pair in `contracts/openapi.yaml` and one line in `cmd/api/routes_golden.txt`, both
regenerated rather than typed, and §3's check count, written by `make verify-update`. **No
migration**: `000502` shipped with SHIP-87, because the counter that writes a chain and the
constraints that hold one are the same file, and splitting a migration across two commits would mean
editing one that had already been applied.

No `internal/boundaries` edit, no `Deps` field, and no new port — the chain read uses the
[Negotiation] seam SHIP-87 introduced, asking `CustomerOf` and deliberately not `AwardableBy`.

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

### SHIP-92 — the award, built against a written-down lock ordering rather than an invented one

SHIP-92's *Done when* is "`POST /v1/jobs/{id}/award` accepts one bid and moves the job to `Awarded`
in one transaction", and the three clauses are three separate proofs: the bid row reads `Accepted`,
the job row reads `Awarded` beside a `job_status_history` row describing that exact move, and a
transition made to fail leaves neither.

**Nothing about the ordering was decided on this branch.** §3's SHIP-88 entry gave it —
`jobs` `FOR UPDATE` → the bid by identifier → the accept → the rejection sweep → the transition — and
the implementation follows it step for step, with the comments naming the entry rather than
re-arguing it. That is the whole value of having written it down four waves before it was needed: two
people produce two lock orderings, and the second one is a deadlock.

#### The endpoint is a verb on the job, so the bid travels in the body

`POST /v1/jobs/{id}/award` with `{"bid_id": "…"}`. That is `Docs/09`'s own path and it is the right
one: **the job is what moves.** `withdraw` and `counter` are acts on an offer and leave the job where
it is, so they are verbs under `/bids/{bid_id}`; this ends the bidding, moves the job exactly once,
and the offer is what it selects. `POST …/bids/{bid_id}/award` would have made the two kinds of act
look alike.

It has one consequence worth naming, because it is the only endpoint in this domain with it: **the
two identifiers arrive from two places**, the job from the URL and the bid from the body, so a client
can pair them wrongly in a way no other endpoint here allows. `TestAwardingABidOnAnotherJobIsNotFound`
and a `make verify` check both award a real offer under a real job it is not on, and both expect the
404 a bid that does not exist gets. Without that comparison a customer could award, against their own
job, an offer somebody made on a different one.

#### The one rule no constraint can express, and the four that do

§3's SHIP-88 entry is exact about the division and this ticket did not widen it. Four rules are
schema and hold whatever the code remembers: `uq_bids_one_accepted_per_job` (one accepted bid per
job), `ck_bids_superseded_is_not_live` (a displaced offer can never be accepted),
`ck_bids_only_a_providers_offer_is_accepted` (only a provider's commitment is awardable) and
`uq_bids_one_successor` (a chain is a list).

**The fifth is application logic and it is the only check this service makes about the bid's state:
that it is `Submitted`.** That is a statement about a transition rather than about a row, and 000500
declined to give `bids` the trigger `jobs` has. It runs inside the transaction, under the bid's own
`FOR UPDATE`, and `acceptBid`'s `WHERE status = 'Submitted' AND superseded_by IS NULL` checks it a
second time — the same deliberate redundancy `supersedeHead` carries, and for the same reason: the
lock makes the refusal legible, the compare-and-set makes it true.

**No migration**, because there is nothing to store. An award is an `UPDATE` of a row that already
exists.

#### Idempotent by state, which is why there is no key column

§8 said to read SHIP-86 for "the difference between idempotency by key and idempotency by state,
which the award needs and which is stronger", and the reading held: awarding the offer you already
awarded answers `200` with it and writes nothing further, exactly as a repeated withdrawal does.
**A placement and a counter are inserts and need a stored key** — a retry outliving the middleware's
cache would otherwise create a second row. An award is not, so the record itself answers the retry,
and the guarantee survives a phone that reconnected, restarted and generated a *fresh* key.

The proof that nothing further was recorded is `updated_at` and the history count together:
`bids_set_updated_at` moves the column on any write to the row, so a second accept would show even
though it would set the same status. `make verify` asserts all three mechanisms in sequence — the
middleware's byte-identical replay, a fresh key answered from the row, and three awards leaving one
transition.

**The ordering that makes it work is the retry being answered before the job's status is judged.**
A successful award leaves the job at `Awarded`, so a status check in front of the already-accepted
branch would refuse the caller's own successful request with "that job cannot be awarded". Both
[Service.PlaceBid] and [Service.CounterOffer] put their retries first and state the principle; this
is the third instance of it and the one where getting it backwards would be hardest to notice, since
the wrong answer is a plausible 409.

Awarding a **different** offer on an awarded job is refused, and that is the same rule from the other
side.

#### The port carries an outcome rather than a bool, and it is the first one here that writes

`ports.go` argues at length that a caller deciding whether to *permit* something wants a boolean, and
`Eligibility` and `Negotiation` both take that shape. **`Awarding` does not, and the reason is that it
is asking a different kind of question.** Those two ask "may this happen"; this one asks *and acts*,
and its refusals lead to two different places — "not yours or no such job" is a 404 and "the job has
moved on" is a 409 naming what became of it. A bool would have collapsed them and made `bidding`
choose one answer for both, which is a disclosure decision this domain makes deliberately elsewhere
and would here have been making by accident. So it is `delivery.Jobs`' shape, for `delivery.Jobs`'
reason: `error` could not carry it either, because matching `errors.Is` against `jobs`' sentinels is
an import by another name.

**Two methods rather than one, and the lock ordering is why.** A single `AwardJob(…)` would have to
take the lock and run the transition in one call, leaving nowhere for the two statements about `bids`
that happen in between — and nowhere for SHIP-93's sweep, which the comment in `AwardBid` marks as
step 4 rather than leaving to be found.

#### `cmd/api` writes one statement against another domain's table, and that is the honest place for it

`awardableJobs.LockForAward` runs `SELECT customer_id, status FROM jobs WHERE id = $1 FOR UPDATE`.
`jobs.Service` has `lockJob` and it is unexported, reachable only from its own `Transition`;
exporting it would mean editing another domain from this branch and widening that domain's surface
for one caller.

**This is `acceptedBids` in `routes_delivery.go` pointing the other way**, and that type's own comment
makes the argument: the composition root is where a dependency between two domains is *visible to
anybody reading how the service is wired*, rather than buried in a domain's `postgres.go` where it
would read as a table that domain owns. The trade is real and is stated rather than hidden — two
statements now know that a job has a `customer_id` and a `status`. What keeps them honest is that
nothing in `cmd/api` *interprets* the status: `jobs.Permitted(status, jobs.StatusAwarded)` is asked,
which is `Docs/02` §2's table in the one place that holds it, and the transition still goes through
the guarded function. When `jobs` grows an exported lock for a caller in another domain, this method
becomes a call to it and nothing else changes.

The test package holds a second copy, `jobAwards`, as it already does for `negotiatedJobs`. **The copy
matters more here than it did there**: this port *writes*, and a stub answering "awardable" and
"awarded" would make every award test pass against a service that never locked anything and never
moved a job.

#### No new error code, and the protocol code was waiting for this endpoint

Three refusals, and all three reuse. An offer that is not live is `bidding_bid_closed`, which is where
a client already goes when an offer is over. The customer's own counter is `bidding_wrong_party` with
a message of its own — the third instance of one rule, *you counter the other party's offer, you
revise your own, and you award theirs*, and in all three the client's correct response is a different
request, which is `Docs/10` §4.4's test for whether a code earns its place.

**A job that can no longer be awarded is `conflict`, and that is not a fallback.** The protocol code's
registered description has read "Valid, but it contradicts the current state — a bid on a job that has
just been awarded, **a second award on one job**" since SHIP-12, before this endpoint existed. Using
it honours a decision already recorded, and it keeps the two 409s apart in the way a client needs:
`bidding_bid_closed` sends them back to the job's other offers, `conflict` sends them to the job.

So `Docs/10-api-error-codes.md` is untouched, which is also what the ownership rules for a domain
branch require.

#### What is deliberately absent

**The concurrency suite.** SHIP-95 owns "double award, withdraw-during-award, and
expiry-during-award", and §8 asks for it to be written from `Docs/02` §3 and `Docs/08`'s named races
by somebody who has *not* read this implementation. Writing the double-award race here would have
taken a third of that ticket and anchored the agent who is meant to arrive at it independently. What
stands in for it is `TestTheIndexRefusesASecondAcceptedBid`, which is honest about being a stand-in:
two well-behaved awards are serialised by the `jobs` lock long before they reach the index, so the
test puts the database in the state a race would have to produce — a job still `Open` carrying an
accepted bid, which is what a writer that skipped the job lock would leave — and asserts that
`uq_bids_one_accepted_per_job`'s violation comes back as a conflict rather than as a 500.

~~**SHIP-93's rejection sweep.** Every competing offer is still `Submitted` after an award. It cannot
be awarded, because the job has moved on, and `make verify` asserts that state explicitly rather than
letting it be discovered. `Docs/02` §3's "awarding a job atomically marks one bid accepted and all
others closed" is therefore half met, and the half that is missing is a ticket rather than a gap in
this one. The transaction is shaped to grow it: step 4 is marked in `AwardBid` and the sweep goes
after the accept and before the transition.~~ **Built — see the SHIP-93 entry below.** The prediction
held to the line: the sweep is one statement at step 4 and nothing else about this transaction moved.
What did change is a refusal order this entry did not anticipate, and that entry says why.

#### Mutation testing

Four mutations, run against the real database, each reverted immediately and confirmed with
`git diff` before the next.

| Mutation | Result |
|---|---|
| `acceptable`'s `!b.Status.live()` branch deleted — the one rule no constraint can express | **Caught.** `TestOnlyALiveProvidersOfferCanBeAwarded` (all four statuses), `TestOnlyTheHeadOfAChainCanBeAwarded`, `TestAwardingAClosedOfferAnswersItsOwnCode` |
| `FOR UPDATE` removed from `LockForAward`, in both the `cmd/api` adapter and the test copy | **Survived** |
| Steps 1 and 2 swapped — the bid locked before the job | **Caught**, and for the wrong reason. See below |
| `acceptBid`'s `WHERE … AND status = 'Submitted' AND superseded_by IS NULL` reduced to `WHERE id = $1` | **Survived** |

**Two survived and a third was caught by accident, and that is the finding worth reporting rather
than tuning away.** The three have one thing in common: each removes a *serialisation* mechanism, and
every test in this repository runs its award transactions one after another. A suite that never
overlaps two awards cannot tell `FOR UPDATE` from a plain read, cannot tell `jobs`-then-`bids` from
`bids`-then-`jobs`, and cannot tell a compare-and-set from an unconditional write, because with no
concurrency all three pairs behave identically.

**The swapped ordering is the instructive one.** It failed
`TestAwardingRefusesAnIdentifierThatNamesNothing`, and the message says why: awarding a real bid
against a job that does not exist answered `bidding: no such bid on that job` instead of
`ErrNotJobCustomer`, because the bid lock now runs first and its `job_id` comparison gets there
before the ownership check does. That is a **disclosure-ordering** assertion catching a **lock
ordering** change — a real defect (the endpoint would start telling a caller which of the two
identifiers was wrong) but not the defect the mutation introduces, which is a deadlock against any
future writer that takes `jobs` first. A reader who saw only the red test would conclude the ordering
is covered. It is not.

**This is precisely the hole SHIP-95 exists to fill**, and it is a sharper argument for that ticket
than the backlog's one-line justification. Recorded here so that whoever writes it knows which three
mutations their suite has to kill, and so that nobody reads a green run as evidence that the ordering
is load-bearing *in the tests*. It is load-bearing in the design; today it is held by review, by
`acceptBid`'s redundancy, and by the constraints underneath — not by a test.

One qualification on the fourth, because it is less exposed than the bare "survived" suggests. The
compare-and-set is unreachable while `acceptable` refuses a closed offer two statements earlier, so
under single-threaded conditions it can only ever match. It is there for the reason `supersedeHead`'s
identical clause is there and says so: **a future writer that forgets the lock still cannot accept a
displaced or closed offer.** Removing it is invisible today and is a live hazard the moment somebody
adds a second path to this table.

#### Shared surfaces

One `$ref` line in `contracts/openapi.yaml`'s sorted `paths:` block, one line in
`cmd/api/routes_golden.txt` (regenerated, and confirmed to be exactly one addition), and §3's check
count, written by `make verify-update`. **No migration, no `internal/boundaries` edit, no `Deps`
field, and no new error code** — so `Docs/10-api-error-codes.md` is untouched.

`make verify` went from 481 checks across 13 sections to 499, all
eighteen in `scripts/verify/61-bidding.sh`. Its three new accounts are the first in that file to draw
the **`0416x`** block, and the corrected allocation map now sits in that file's own header — §9
recorded that the only copy lived in `90-admin.sh` and did not mention bidding at all, and that
`61-bidding.sh`'s existing `04145`–`04147` sit inside fleet's block, free by coincidence. The existing
three are left where they are; anything new takes `0416x`.

### SHIP-93 — the sweep is one statement, and what it does *not* touch is the half with no constraint behind it

SHIP-93's *Done when* is "every other bid on the job becomes `Rejected` in the same transaction", and
`Docs/02` §3 is the sentence it comes from: "awarding a job atomically marks one bid accepted and all
others closed." SHIP-92 built the accept and left the sweep marked as step 4 of the recorded lock
ordering. This fills that step, and **nothing else about the transaction moved** — one `UPDATE`, no
migration, no new port method and no new error code.

```sql
UPDATE bids SET status = 'Rejected' WHERE job_id = $1 AND status = 'Submitted'
```

#### `Rejected` was chosen by `Docs/02` §4 rather than here

Eight statuses, and exactly one of them means *the customer said no*: `Rejected`, "an offer the
customer declined". Awarding somebody else **is** declining every other offer, and it is the customer
who did it. The other three closed statuses each name a different event and would be a lie about how
the offer ended — `Superseded` is displacement by a counter, `Withdrawn` is the provider's own act,
`Expired` is the offer's own terms running out. `model.go`'s `StatusRejected` has named this ticket
since SHIP-84, so the vocabulary was settled before the writer arrived. **`ck_bids_status` is
untouched and no status was invented**, which §9's open `Countered`/`Superseded` question makes worth
saying out loud: this ticket adds no third description of one event.

**The awkward case is a customer's own outstanding counter, and it is swept.** A negotiation the
customer did not award can have their own counter at its head — live, `Submitted`, `offered_by =
customer`. Leaving it would strand a live offer on an awarded job, which is exactly what the sweep
exists to prevent, and `Rejected` still reads correctly: they declined it by awarding elsewhere.

#### What a chain's non-head rows get is *nothing*, and that is the half no constraint would catch

The brief for this ticket asked what the sweep does to a chain's already-displaced rows. **It leaves
them exactly as they are**, and the predicate is how: they are `Superseded`, not `Submitted`.

That reads like a narrowing and it is not. `ck_bids_superseded_is_not_live` makes the live offer and
the head of a chain **the same row by construction** (SHIP-88), so `status = 'Submitted'` misses no
live offer anywhere. What it excludes is every row that had already ended — and excluding those is the
point rather than a side effect. Overwriting `Superseded` with `Rejected` would replace the record of
*how* an offer ended with the record of *when*: the history would then say the customer declined an
offer that was actually displaced by their own counter, and `Docs/01` §4.3 requires every offer,
counter-offer and withdrawal to be **recorded**, which a status is.

**No constraint would have refused that write.** `ck_bids_superseded_is_not_live` forbids `Submitted`
and `Accepted` on a displaced row and permits `Rejected`. So unlike almost everything else on this
branch, this rule is application logic with nothing underneath it — which is why there is a subtest
per closed status rather than one for the shape, and why each asserts `updated_at` as well as the
status. A sweep that rewrote `Withdrawn` as `Withdrawn` would pass a status check and fail this one.

#### There is deliberately no `id <> $winner`, and that is what makes the ordering testable

The sweep runs **after** the accept, which SHIP-88's ordering says and SHIP-92's comment repeats: the
accept takes the `uq_bids_one_accepted_per_job` entry and is what serialises two awards, so a sweep in
front of it would run in both of them.

The obvious way to write the statement is "every bid on this job except the winner". It is not
written that way. Excluding the winner by identifier would make the statement correct under an
ordering it is not meant to run in — and moving it in front of the accept would then be a change **no
test could see**. Left out, a sweep that ran first would close the row the accept is about to name,
`acceptBid`'s compare-and-set would match nothing, and the award would fail loudly and roll back with
nothing written.

**That matters more here than it would have in another ticket.** SHIP-92's mutation run found that
the lock ordering is held by review rather than by any test, because no award test starts a second
goroutine. This is one position in that ordering that a single-threaded suite *can* check, and it
checks it because of a clause that is not there. Confirmed by mutation, below.

#### The refusal order changed, and leaving it alone would have retired a registered error code

This is the consequence the ticket did not advertise. Before the sweep, a customer awarding a
*different* offer on a job they had already awarded met a `Submitted` row: `acceptable` passed, the
job's standing refused it, and the answer was `conflict` — the protocol code whose registered
description has read "a second award on one job" since SHIP-12. After the sweep that same offer is
`Rejected`, so an offer-first ordering answers `bidding_bid_closed` instead.

Both are true. Only one is useful. `Docs/10` §4.4's test is where a code sends the client, and §3's
SHIP-92 entry states the split: `bidding_bid_closed` sends them to the job's other offers, `conflict`
sends them to the job. **After an award there are no other offers** — the sweep closed them — so the
first answer sends the customer somewhere empty and the description in the error registry quietly
stops describing anything the platform produces.

So `AwardBid` judges the job's standing immediately after the retry branch and before the offer's:

1. not this customer's job → `404`
2. bid not on the job in the path → `404`
3. **already `Accepted` → the caller's own award, answered** (unchanged, and still first — the retry
   has to be recognised before the job's status is judged, or a successful award refuses its own retry)
4. **job has moved on → `conflict`** ← moved up
5. offer not live → `bidding_bid_closed`
6. the customer's own counter → `bidding_wrong_party`

The retry branch stays in front of all of it, which is the ordering SHIP-92 argued for and this does
not disturb. `TestASecondAwardOnOneJobIsAConflictAtTheWire` now asserts the competing offer is
`Rejected` *before* sending the second award, so the test says which of the two mechanisms it is
exercising instead of passing for either reason.

#### What the sweep buys the rest of the domain, which is more than tidiness

Three verbs gate on `Status.live`. Before this ticket, a competing offer stayed `Submitted` after the
award, so all three let it through: **its provider could revise the price of an offer on a job
somebody else had already won**, or withdraw it, or the customer could counter it. Nothing was
corrupted — the job had moved, so none of it could lead to an award — but every one of those told
somebody their action on dead work had succeeded. All three now answer `bidding_bid_closed`, and one
code covers all three because the client does the same thing with all three.

It also narrows `ErrNegotiationOver` to what it should always have been. A counter on an **awarded**
job now meets a `Rejected` offer and answers through the ordinary closed-offer branch; what still
reaches the negotiation-over branch is a job cancelled or expired without ever being awarded, where
the offers really are live and only the job has moved.

#### Mutation testing

Four mutations, run against the real database, each reverted immediately and confirmed with
`git diff` before the next. These are additions to SHIP-92's table above rather than a replacement
for it.

| Mutation | Result |
|---|---|
| `rejectCompeting`'s `WHERE job_id = $1` dropped — the sweep reaches every job in the database | **Caught**, by `TestTheSweepReachesNoOtherJob` alone: both offers on the bystander job came back `Rejected`. Nothing else in the suite noticed, which is the argument for that test existing |
| `AND status = 'Submitted'` dropped — the sweep reaches every row on the job | **Caught by 11 tests**, including every happy-path award. Louder than expected and for a reason worth knowing: without the predicate the sweep also rewrites the row the accept wrote one statement earlier, so the winner ends `Rejected` and `TestAwardingAcceptsTheBidAndMovesTheJob` fails before any of the four closed-status subtests are reached |
| The sweep moved **in front of** the accept | **Caught by 14 tests**, on `acceptBid`'s compare-and-set matching nothing and the award reporting "was live when it was locked and is not now". This is the catch the absent `id <> $winner` buys, and it is the only position in the recorded lock ordering any test in this repository holds |
| The whole call removed | **Caught by 5**: `TestAnAwardClosesEveryCompetingOffer`, `TestTheSweepClosesALosingNegotiationsHeadAndNotItsHistory`, `TestASweptOfferCanNoLongerBeActedOn`, and both second-award tests. Run because it is the one that says the *Done when* is demonstrated rather than asserted |

**None survived**, which is a different result from SHIP-92's and for a reason worth stating rather
than celebrating: **every one of these is a single-threaded property**. Whether a statement touches
the right rows, and whether it runs before or after another statement in the same transaction, are
both visible to a suite that never overlaps two transactions. SHIP-92's two survivors were both
*serialisation* mechanisms, and those remain exactly as untested as that entry says. This ticket did
not close that hole and did not widen it — see below.

#### What this adds to SHIP-95's list, and what it deliberately does not

**Nothing new that only a race can distinguish.** The sweep is an unconditional `UPDATE` over rows the
job's lock already covers, and it has no compare-and-set to remove and no lock of its own to take —
so there is no third mechanism here that a single-threaded test cannot see. The two survivors named in
SHIP-92's entry are still the whole list.

**One thing SHIP-95 should know about it anyway**, because it changes what a race would produce rather
than adding a mechanism: the sweep locks *several* `bids` rows where every other writer in this domain
locks one. Two sweeps on one job are serialised long before that matters — by the `jobs` lock at step
1 and by the accept at step 3 — and two sweeps on different jobs touch disjoint rows, so there is no
cycle to find. `Docs/11` §3's SHIP-88 entry allowed for a sweep that locks explicitly and asked for
`ORDER BY id` if it did; a plain `UPDATE` takes no explicit lock, so that caveat does not apply, and
if a future edit turns this into `SELECT … FOR UPDATE` it does.

**No concurrency suite was written here**, and §8 is the reason: SHIP-95 is to be written from
`Docs/02` §3 and `Docs/08`'s named races by somebody who has not read this implementation, and a race
test written on this branch would anchor them.

#### Shared surfaces

**None.** No migration, no `internal/boundaries` edit, no `Deps` field, no new route and therefore no
`routes_golden.txt` change, no new error code and so no `Docs/10-api-error-codes.md` edit, and not one
`$ref` in `contracts/openapi.yaml` — the award path already existed and only its description grew.
Everything is inside `internal/bidding/**`, `contracts/paths/bidding.yaml`, `scripts/verify/61-bidding.sh`
and this file.

`make verify` went from 499 checks across 13 sections to 506, all seven in
`scripts/verify/61-bidding.sh`, and its two new accounts take `04162` and `04163` from the `0416x`
block that file's header allocates. **The ordering trap SHIP-92's script warned about was met
immediately and is worth repeating**: every offer in the sweep's fixture — the winner, a losing
negotiation, a plain competitor and one already withdrawn — has to be placed *before* the award, because
once the job is `Awarded` it is offered to nobody and a provider asked to bid afterwards is refused by
the eligibility filter with a `404`. A true answer to a different question that reads exactly like a
broken fixture.

### SHIP-94 — the key is not what makes the retry safe, and §3 had already found that out once

SHIP-94's *Done when* is "a retried award with the same key returns the original outcome, not an
error", and it names the wrong mechanism — which is the reading §8 pointed this branch at rather than
a complaint about the backlog. **§3's SHIP-111 entry settled it a wave ago** and this ticket did not
re-litigate it:

| Mechanism | What it guarantees | For how long |
|---|---|---|
| `httpx.Idempotent` (Redis) | the *response* is replayed and the handler never runs | while the entry lives — a TTL, an eviction, a failover |
| the accepted offer itself | the *act* cannot happen twice | permanently |

**Redis makes the retry cheap; the record makes it correct.** SHIP-111 needed a migration to get the
second half, because a milestone is an insert and a retry outliving the cache creates a second row.
An award needs nothing: it is an `UPDATE` of a row that already exists, so applying it twice reaches
the state applying it once reaches, and the accepted status *is* the record of the request. **No key
column, no migration, and no change to the handler or the service** — SHIP-92 built the state half
deliberately and said so, because the alternative was building the endpoint to be wrong about it
first.

So what this ticket is, honestly, is the proof: the key mechanism demonstrated against the running
binary, the two retries nobody had tested, and this entry saying which mechanism answers which
request. **It is the SHIP-91 shape in miniature** — a *Done when* already met by another ticket's
work — with one difference that matters: SHIP-91 was met by SHIP-80 before anyone looked, and this
was met by SHIP-92 *knowing* it was doing so, which is why that entry names SHIP-94 by number.

#### What the two mechanisms do, and the one request where they disagree

| What the client sends | What answers | What it gets |
|---|---|---|
| the same key, soon | the stored response | the original `200`, byte for byte, `Idempotency-Replayed: true` |
| the same key, past the TTL — or a **fresh** key | the accepted offer | `200`, the same bid, nothing further written |
| the same key naming a **different** offer | neither | `409 idempotency_key_reused` |
| a fresh key naming a different offer | the record | `409 conflict` |

**The third row is the case worth naming and the brief for this ticket was right to ask for it.** The
middleware fingerprints method, path and body, so a key carrying a different `bid_id` is not a retry
at all — it is a second request under a reused key, and replaying the first one's response would tell
a client that something it never sent had succeeded. The record would have answered `conflict`, which
is also true. **The middleware wins because it is in front, and that precedence is right**: "you have
reused a key" is a client defect the client can fix, where `conflict` would send them to look at a job
that is exactly as they left it. Both answers are demonstrated in sequence by `make verify` — the same
request, once under the reused key and once under a key of its own, answering with two different
codes.

`make verify` also deletes the Redis entry with `redis-cli del` and sends the request again, which is
what a TTL expiry, an eviction or a failover looks like from the handler's side. That is the check the
ticket turns on: `Idempotency-Replayed: true` on the second request and **absent** on the third is
what makes "the record answered this one" checkable rather than asserted. SHIP-111's section does the
same thing and this is deliberately the same shape.

#### The two retries nobody had tested, and one of them exists because of SHIP-93

**A retry must not run the sweep again.** Before SHIP-93 a repeated award had exactly one write to not
do; now it has a second, over rows *no caller ever named*. Returning the right bid while quietly
re-closing three offers would satisfy every assertion `TestAwardingTheSameBidTwiceIsTheSameOutcome`
makes. `TestARetriedAwardDoesNotSweepAgain` reads `updated_at` on the competing offers as well as on
the accepted one — `bids_set_updated_at` moves the column on any write, so a sweep that set the status
already there is still visible — and `make verify` makes the same comparison from outside Go.

**A retry must survive the world moving on, and "the world" now goes further than `Awarded`.** SHIP-92
wrote the principle — *a retry asks what happened to a request, and that answer does not change when
the world does* — and no test moved anything. `TestARetryIsAnsweredAfterTheJobHasMovedOnAgain` moves
the job to `En route to pickup`, from which `Docs/02` §2 has no path to `Awarded` at all, and sends the
award again: `200`, the accepted offer, nothing written. That is the phone that found signal in the
afternoon and sent the morning's award, by which time the provider is already driving. Getting it
backwards is invisible in ordinary use and *plausible on inspection* — refusing with `conflict` reads
like a correct answer about a job that has moved on. The same test then awards a **different** offer
under the same conditions and requires the refusal, so it says the branch recognised a retry rather
than stopping checking.

That test needed a fixture change: `transitionBy` names the actor's kind, because `Awarded → En route
to pickup` is the provider's move and every fixture transition in this package had been a customer's.
A history row attributing it to the customer would be a fixture that lies about the one thing 000402's
trigger reads.

#### Mutation testing

Three mutations, each reverted immediately and confirmed with `git diff`. Added to the running record
in the SHIP-92 and SHIP-93 entries above.

| Mutation | Result |
|---|---|
| The already-`Accepted` branch in `AwardBid` deleted — the retry stops being recognised | **Caught by 4**: `TestAwardingTheSameBidTwiceIsTheSameOutcome`, `TestARetriedAwardUnderAFreshKeyIsStillTwoHundred`, and both new tests |
| The job's standing judged **before** the retry branch rather than after | **Caught by the same 4.** This is the ordering SHIP-92 argued for and the mutation that shows the argument was load-bearing: a successful award leaves the job at `Awarded`, so its own retry is refused with `conflict` |
| The refusal reorder undone — the offer judged before the job | **Caught by 3**: both second-award tests and `TestARetryIsAnsweredAfterTheJobHasMovedOnAgain`. SHIP-93's entry is where that change is argued; this is what holds it |

**None survived, and none of them could have.** All three are single-threaded orderings inside one
transaction. **SHIP-92's two survivors are still the whole list of what a race would be needed to
distinguish**, and this ticket added nothing to it: it writes no new statement, takes no new lock, and
its one new branch is a comparison against a status already read under the bid's own `FOR UPDATE`.

**One thing SHIP-95 should know**, and it is about the *middleware* rather than the domain. Two
concurrent awards under one key never both reach the service: the second is refused with
`idempotency_request_in_progress` before the handler runs. So a race test that wants two awards
arriving together has to bypass `httpx.Idempotent` and call the service directly — which is what
SHIP-111's concurrent test does and says why: two API instances, or a cache miss on both sides of a
retry, is the same race with nothing in front of it. A suite written against the middleware would
prove the middleware works and nothing about the award.

#### Shared surfaces

**None**, for the second commit running. No migration, no route, no `routes_golden.txt` line, no error
code, no `contracts/openapi.yaml` edit — `idempotency_key_reused` is `httpx`'s and has been registered
since SHIP-15. Everything is inside `internal/bidding/**`, `contracts/paths/bidding.yaml`,
`scripts/verify/61-bidding.sh` and this file.

`make verify` went from 506 checks across 13 sections to the figure at the top of this section, all
seven in `scripts/verify/61-bidding.sh`. One of them is not about idempotency at all and earns its
place anyway: a **stranger** sending this customer's key is answered in their own scope rather than
handed the stored `200`. That is SHIP-44's subject-scoped namespace demonstrated on the endpoint with
the most to lose from `idem:v1:anonymous:<key>`, which is the gate `CLAUDE.md` held every
authenticated state-changing endpoint behind.

### SHIP-95 — the races were run, and two of the three things they were built to see were not there to be seen by anything else

SHIP-95's *Done when* is "tests prove correctness under double award, withdraw-during-award, and expiry-during-award races". `Docs/08` names a fourth — a retry arriving after the original award succeeded — and all four are covered by `services/core/internal/bidding/race_test.go`, ten tests in a file of their own.

**It was written adversarially, and §8 had asked for exactly that for six waves.** One agent built SHIP-92…94; a second wrote this from `Docs/02` §3, `Docs/08`, `Docs/01` §4.3 and §6, the published `contracts/paths/bidding.yaml`, `errors.go`, `model.go`, the fixtures, and `go doc` for the signatures — without opening `service.go`, `postgres.go`, `ports.go`, `http.go`, `cmd/api/routes_bidding.go`, or any test whose name contains "Award". The arrangement earned its keep: a suite written from the reading that produced the code proves the code agrees with itself, and two of the three properties below turned out to be invisible to every test in the repository.

#### The mechanism: a race is *observed*, not started and hoped for

Two goroutines released together prove nothing. If the second runs to completion before the first reaches its first statement, every assertion afterwards holds for the reason it would hold sequentially — and the test reports a guarantee it never exercised. That is the failure mode this file is most exposed to, so it is designed against directly.

Each deterministic test opens one transaction the test itself drives, runs a call inside it and **does not commit**, starts the racing call in a goroutine, and then polls `pg_blocking_pids` until PostgreSQL reports the second backend waiting on the first's locks. Only then is the held transaction released. If the racing call ever finishes without having waited, `waitUntilBlockedBy` fails the test naming what did not happen. `pg_stat_activity` is filtered to `current_database()`, which is exact rather than indicative: `pgtest.DB` clones a database per test, so the only backends in it are that test's own.

The two free-running tests — six awards released together through a pool sized to them — keep a watcher on the same view throughout and fail if it never sees one attempt waiting on another. Between the two shapes the file covers both "the window is closed when I hold it open" and "the window is closed when the scheduler chooses".

#### What the mutations found

Each of the three properties the suite was commissioned for was demonstrated the way this repository demonstrates a guard: break it, watch a named test fail, put it back. The implementation is unchanged; the mutations were reverted from copies and the tree confirmed by checksum.

| Mutation | What failed | What it printed |
|---|---|---|
| `FOR UPDATE` off the `jobs` read | `TestTheJobIsTakenBeforeTheBid`, plus real deadlocks in `TestConcurrentAwardsOfDifferentOffersLeaveOneAcceptedBid` | five of six racers killed with `SQLSTATE 40P01`, and the double-award test's loser answered `bidding_bid_closed` where the contract publishes `conflict` |
| a bid lock taken **before** the job lock | the same two | "the award was holding the bid row while it waited for the job row" |
| the accept's compare-and-set made unconditional | **nothing** | — |
| the bid's locking re-read made non-locking | `TestAWithdrawalCommittedMidAwardIsNotOverwritten`, `TestAnAwardInFlightRefusesTheWithdrawalRacingIt`, `TestAnExpiryCommittedMidAwardIsNotOverwritten` | "was live when it was locked and is not now" |
| **both** liveness guards removed | the same three | "the award = &lt;nil&gt;, want ErrBidClosed" — the award **succeeded** on a withdrawn offer |

**The fourth row is the finding worth carrying forward.** The rule `Docs/11` §3's SHIP-88 entry says application logic still has to keep — "check that the bid it is accepting is `Submitted`" — is kept *twice*: a locking re-read of the bid at step 2, and a compare-and-set on the accept at step 3. Either alone is sufficient, so removing one is invisible to every test in the repository including this one, and removing the other is caught only by the races below. That is not a defect and nothing was changed. It is a note for whoever next edits `acceptBid` or `lockBid`: the two are one guarantee held twice, the redundancy is not visible from either site, and a change that removes both passes nothing but these three tests.

It also settles a question by black-box inference rather than by reading: because the withdrawal race is refused when only the compare-and-set is removed, the award's liveness decision must be made **under the bid's row lock**. In READ COMMITTED a plain read would have seen the pre-withdrawal version and let the accept through.

**Two of the three commissioned properties were genuinely unseen.** With `race_test.go` removed, the pre-existing package passes under the bid-before-job mutation entirely, and passes under the two-guards-removed mutation except for `TestConcurrentCountersLeaveExactlyOneLiveOffer` — which fires about *counters*, not about the award. No award test noticed either.

#### One test's claim was narrowed by its own mutation, and the pair is deliberate

`TestAnAwardHoldsTheJobRowUntilItCommits` survives the outermost `FOR UPDATE` being deleted, and the reason is worth recording: the transition at step 5 runs through `jobs`' guarded function, which locks the job row itself, so the award still cannot commit past a row somebody else holds. What changes is *when* — the bid has been read, accepted and swept by then, and the ordering has quietly become `bids` → `jobs`.

So the two tests are kept apart rather than merged. One says an award cannot slip past a job another transaction is holding, which is what stops two of them interleaving. The other says nothing on `bids` is touched while it waits, which is what stops the ordering closing into a cycle. The comment on the first was corrected to say so after the mutation showed it over-claimed.

#### The deadlock is real, and is deliberately asserted as an ordering rather than as a deadlock

`Docs/11` §3's SHIP-88 entry predicted the cycle and the mutation produced it: with the bid taken first, two awards on one job each hold a bid and then contend for the job, and the winner's rejection sweep waits for the bid the loser is holding. PostgreSQL killed five of six racers.

The suite still asserts the *ordering* instead, for two reasons. Reaching the deadlock needs both racers past their bid locks before either reaches the job, which is a scheduling coincidence rather than something a test arranges; and a deadlock arrives as an *error*, so a test that accepted "one of them failed" as its pass condition would accept the deadlock as correct behaviour. `TestTheJobIsTakenBeforeTheBid` asks the only question that separates the two orderings from outside — while the award is held at the job row, is the bid it is about to accept still free — with one `FOR UPDATE` under a short `lock_timeout`.

#### Race 4 is two properties at two layers, and the file says which is which

At the HTTP layer a retry is *sequential* and is answered by the middleware's stored response; that is SHIP-94's and `scripts/verify/61-bidding.sh` demonstrates it over the wire. Below the middleware a retry is *concurrent* — two requests under one key never both reach the service, since the second is refused `idempotency_request_in_progress` — so the concurrent half has to be driven at the service layer, exactly as SHIP-111's `TestConcurrentRetriesRecordOneMilestone` is. Six awards of one offer released together all answer with it, one transition is recorded, and one row is `Accepted`.

#### Three `make verify` checks, because the Go suite cannot reach the served adapter

The package's fixtures carry their own copy of `cmd/api`'s `awardableJobs`, which is what makes the Go tests honest about the real `FOR UPDATE` and the real guarded transition — and also means they exercise the copy rather than the served one. So SHIP-95 adds a section that races two awards **through the running binary**, with distinct keys so the middleware does not collapse them: one `200`, one `409 conflict`, one accepted offer, nothing left live, and the job moved to `Awarded` exactly once. What is asserted there is the outcome invariant rather than the interleaving — two curls may or may not overlap on a given run, and a shell check that depended on them overlapping would be flaky rather than strict.

#### What is not covered, said plainly

Nothing in `Docs/08`'s four is left out, and no defect was found. Two limits are worth naming. The expiry race is run against the statement a sweep *is* — one conditional `UPDATE` to `Expired` — because SHIP-89 has no writer yet; when it arrives, the race it has to survive is already written down here. And `TestAnAwardedOfferIsOutOfAnExpirySweepsReach` pins the other direction as a property of what the award *writes* rather than of what a sweep checks: after an award there is nothing on the job in the `Submitted` predicate, so a sweep written against `Docs/02` §4 cannot un-award anything however late it runs.

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

### SHIP-135 — the topic set, the catalogue, and why the outbox needs no dead-letter path

Three points, and most of the value is in closing the two §9 items SHIP-134 opened and named this
ticket for: **nothing created the Kafka topics**, and **the outbox had no dead-letter path**. Both
are decided here, and it turns out they are one decision rather than two.

**The topic set is three topics, one per aggregate — `shipper.job`, `shipper.bid`,
`shipper.delivery` — and `topicFor` moved rather than changed.** §9 predicted it would be "the one
line SHIP-135 changes", and what changed was its address: the mapping is now
`internal/events.TopicFor`, beside the catalogue that decides the set, and `cmd/worker`'s
`topicFor` delegates to it. The aggregate list is closed the way `internal/boundaries.Domains` and
`migrations.Blocks` are closed. All three topics are created now even though only `job` has events,
because a partition count cannot be reduced and deciding all three at once is cheaper than deciding
each under whatever deadline first needs it.

**`cmd/topics` creates them, applied like a migration, and local and deployed are the same step.**
That was §9's genuinely open half. The compose stack was rejected because it would be a second,
hand-maintained topic list that exists only locally — the drift the single implementation avoids,
written down. A start-up step in `cmd/worker` was rejected for two reasons: it runs once per
process rather than once per deployment, which in the harness alone is four worker starts, and it
needs `Create` on the cluster in a process whose whole job is producing. **A schema is applied by a
step somebody runs**, and this repository already has that shape in `cmd/migrate`. The topic list
is derived from `events.Topics()` rather than typed, so it cannot drift from the catalogue;
`make topics` runs it; it is idempotent, and re-running is the intended usage. It also **refuses**
a topic whose partition count disagrees rather than correcting it — adding a partition rehashes
every key and silently ends the per-aggregate ordering the publisher's advisory locks exist to
keep, so that is a decision with a migration behind it, not a repair.

**"Versioned schema" means a `Schema` per event type — aggregate, version, payload field set, and a
payload bound — and the version travels in the message rather than in the topic name.** The obvious
design is `shipper.job.v2`, and it is wrong here structurally rather than aesthetically: **ordering
is promised per aggregate**, Kafka orders within one partition of one topic, so one topic per
aggregate is what makes that promise keepable. A version in the topic name is therefore a version
per *aggregate* — `job.expiry_warned` gaining a field would move `job.status_changed` too, and a
job's events would split across two topics with no order between them. One topic per event type
loses the ordering outright.

**The version is written into the payload when the row is written, not looked up when it is
published**, and that distinction is the whole point. The failure worth preventing is the one
`internal/pagination`'s cursor prefix was built against (SHIP-66): not a rejected event but **a
stale one accepted as meaning something else** after a deployment. A row written before a deploy
and drained after it is entitled to the version it was written under; looking the version up at
publish time would relabel it as the newer shape. It also makes a row self-describing —
`select payload->>'schema_version'` answers "what shape is this" — and it needed no migration,
which matters because `outbox` is in the shared block a domain branch may not touch. The publisher
copies it into the envelope and into a `schema-version` Kafka header, so a consumer can refuse a
version it was not built for without deserialising the body.

**A domain declares its own events, and no domain track will ever edit `internal/events` to add
one.** `internal/jobs/events.go` is one `init` calling `events.Register` for the three job events,
with the payload struct itself as the schema — the field set is derived by reflection rather than
retyped, so the catalogue cannot describe a shape the code does not have. SHIP-136 adds bidding's
and delivery's from files exactly like it. `internal/events` still imports no domain: the catalogue
is a table it holds, not a list it writes.

**The mechanism that makes a forgotten version bump visible is `cmd/api/events_golden.txt`**, and
it is there for the reason `routes_golden.txt` is: a domain registers from its own `init`, so no
single source file lists them, and `cmd/api` is the one binary that links every domain. Each line
is topic, type, version, bound and field set. **A line whose fields changed without its `v`
changing is the defect** — a one-line diff in review, beside the change that caused it, rather than
a consumer's logs a week later. Regenerating it is therefore the moment somebody is asked whether
the version should go up, which is exactly when they can answer.

**The dead-letter decision: there is no dead-letter path, because a permanently unpublishable row
is now unwritable.** §9 offered three ways out and called bounding the payload the cheapest and
probably right one. It is right, and the argument is stronger than "cheapest" once the class is
enumerated. "Permanently unacceptable" can only mean three things: the event type is one nothing
consumes, its aggregate has no topic, or the payload is over the broker's limit. All three are
decided by the catalogue, and **`events.New` and `Outbox.Emit` check all three inside the
transaction making the state change** — where a failure rolls the change back, tells the caller, and
names the line that built the event. So the outbox's remaining failures are all transient (the
broker is unreachable, or the topic set was never applied), and for those, failing the batch and
leaving every row claimable is exactly right. **Rather than build a recovery path, the ticket made
the failure unreachable.** Parking a row after N attempts was rejected on `000004`'s own terms —
`published_at` is the publisher's only state — and publishing per event was rejected because it
weakens the batch guarantee for every event to accommodate one that should never exist.

The bound is 16 KiB per payload against a broker limit of 1,048,588 bytes, and it is not tuned:
its job is to make an unacceptable payload unwritable, and any bound comfortably below the broker's
does that. **The revisit trigger is named rather than left to judgement: the first event whose
payload is legitimately unbounded — a document, a photograph, a manifest — must not travel in the
outbox at all.** It goes to object storage and the event carries the key. If that is ever refused,
the dead-letter question reopens with §9's other two answers still on the table.

**Two smaller things fell out of it.** `events.New` no longer takes an aggregate type — the
catalogue supplies it, so `job.status_changed` can no longer be emitted on the `bid` aggregate,
which would have published to `shipper.bid` and been read by nobody with nothing anywhere
reporting it. And **the budget-privacy invariant is now structural across every domain event there
will ever be**: `cmd/api` refuses any registered field whose name mentions a budget, in bidding and
delivery before they have written a line. `internal/jobs` already read its own events back out of
the outbox and failed on one; this is the version that covers the domains that do not exist yet.

**`make verify` went from 285 checks across 11 sections to 295**, with the ten new ones in
`scripts/verify/80-notifications.sh` demonstrating the topic set against the real broker: all three
topics with three partitions and stated retention, a second application that creates nothing, a
topic staged by hand with one partition being reported and **left alone**, the version in the row,
in the envelope and in the header, and the committed catalogue holding a schema for each of the
three events and no budget field anywhere in it.

**And it found a third instance of this harness's oldest lesson, one layer further out than the
first two: `shipper.job` is shared by every git worktree on the machine.** The header check was
first written as a count over the whole topic — every message of a registered type must carry
`schema-version:1` — and it failed on a run where nothing was wrong. `COMPOSE_PROJECT_NAME` is
pinned so that worktrees share one stack, deliberately, and the isolation that comes with that is
per-worktree databases and ports. **Kafka has no equivalent**: there is one broker and one
`shipper.job`, so a concurrent `make verify` in another worktree — running a build without this
ticket in it — had its own unversioned events on the topic, and the count was right about what it
saw. The check now fences on **the event ids this section created**, which is the recipe SHIP-47's
rate-limit bucket produced and SHIP-134's comparison adopted, with one addition worth carrying
forward: **on a topic the fence has to be an id rather than a timestamp**, because a concurrent run
in another worktree is not ordered against this one. Anything a later section asserts about a Kafka
topic has to identify its messages rather than count them.

**`scripts/verify/50-jobs.sh` now applies the topic set before its first worker start, and claims
no check for it.** That is the §9 finding acted on rather than restated: `cmd/worker` is one binary,
every start runs every task, so that file's three starts also drain the outbox — and against a
missing topic every one of those passes failed silently, because nothing in that section asserted
on them. The topic application is a prerequisite there and an assertion in the notifications
section, which keeps the check where the ticket is.

**One configuration request, deliberately not taken.** The replication factor is the only genuinely
environment-dependent number in the topic set: one is correct for a single-broker compose stack and
wrong for a cluster, which wants three. `internal/config` is a shared surface a domain branch may
not edit, so it is a flag on the command — `go run ./cmd/topics -replication 3` — defaulting to
`events.DefaultReplicationFactor`. A deployment can pass it today with no configuration change.
**Whoever next owns `internal/config` should fold it in as `KAFKA_REPLICATION_FACTOR`**, which is
the same handling `GEOCODING_*` and the page sizes got.

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

**§3's summary table is checked and emphatically not generated.** §7c raised generating it from
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

### SHIP-98 — the first provider surface, and the endpoint that does not refuse a customer

`Docs/01` §4.2's whole measure — a provider can add, edit, deactivate and reactivate a vehicle —
over the six routes SHIP-78 serves and no others. Three routes on the client: `/fleet/vehicles`,
`/fleet/vehicles/new`, and `/fleet/vehicles/{id}`, declared in that order because go_router takes
the first match and `new` is otherwise a vehicle whose id is the word "new". The fleet screen splits
the list into what is on the road and what is not, and adding, editing, taking off the road and
returning to service each carry their own idempotency key.

**Everything before this ticket was the customer's or belonged to neither half.** So this is where
`Docs/07` §1 — the two halves stay genuinely separate inside one app — stopped being a statement
about the shell.

#### The finding: `GET /v1/fleet/vehicles` does not check the caller's role

`internal/fleet/service.go` calls `isProvider` in `Add` and nowhere else. `Vehicles` does not, and
it is right not to: a customer owns no vehicles, so there is nothing for the endpoint to withhold,
and it answers **`200` with an empty page**. Confirmed live, along with the rest:

| As a customer | Live answer |
|---|---|
| `GET /v1/fleet/vehicles` | `200 {"data":[],"has_more":false}` |
| `POST /v1/fleet/vehicles` | `403 fleet_provider_only` |
| `GET /v1/fleet/vehicles/{someone else's id}` | `404 not_found` |

That table is the reason the device has to say whose surface this is rather than drawing it and
letting the platform refuse. **A customer who reached the fleet screen would not be refused** — they
would see an empty fleet, an "Add a vehicle" button, a form to fill in, and a `403` only at the end
of all of it. `ProviderOnly` says so at the start, and the fleet controller is never constructed for
them, so no request is made on their behalf. `the_fleet_is_the_providers_half_test.dart` asserts
both halves of that: the surface answers, and `fleet.calls` is empty.

**None of that is an authorisation control and the file says so at length.** A build with
`ProviderOnly` deleted would show a customer these screens and change nothing about what they could
do with them, which is the property that makes it safe for the client to hold an opinion at all.

#### The router stays blind to the role, deliberately

The obvious place to keep a customer out is `redirectFor`. It was not used, for two reasons, and the
second is the one that would have produced a bug. A guard deciding who may be *where* is an
authorisation control living on the device (`Docs/07` §3). And the role is `null` for the first round
trip of a restored cold start — the keychain holds a refresh token, the role is a claim in the
access token — so a role-aware redirect would bounce a provider off their own fleet every time they
opened the app from a notification, and be reported as "it works the second time". The role decides
what a screen *draws*; the guard decides only where the app is willing to go.

#### The vehicle vocabulary is compiled in, and here is the bounded cost

`CLAUDE.md` keeps anything that changes under operational pressure server-side. `VehicleType`'s
eleven values are in the client anyway, because **no endpoint serves the list** and the alternative
is a free-text box against a closed set the platform refuses with `not_allowed`. What bounds the
cost is `VehicleType.unknown`: a twelfth type decodes rather than throws, is labelled "Not named by
this version" rather than as a fault, and is **omitted from a `PATCH` rather than echoed back** — so
an old build editing such a vehicle can correct its plate without overwriting a type it cannot name.
All eleven wire names were sent to the live platform and accepted, which is the check worth having
on a list that cannot be corrected without a store release.

This is **not** the capability vocabulary `vehicle_requirement` will be validated against. That
belongs to SHIP-79, and `vehicle_requirement` stays free text until it arrives.

#### Two smaller decisions worth finding later

**The edit sends every field the form holds, empties included.** The contract distinguishes absent
(leave alone), empty (clear) and set, and a form that omitted what somebody had emptied would give
them no way to take back a load height they once stated. That is not the hazard `PATCH` exists to
avoid: a field this build has never heard of is never named, so it can never be silently cleared.
Demonstrated live — `"load_height_cm": 0` cleared the height, and the response omitted it.

**A key on a `DropdownMenuItem` names the copy inside the closed button, not the one in the open
menu.** A test can find it and cannot tap it. The key belongs on the item's child, which is what the
menu route rebuilds. Worth knowing before the next screen with a picker.

#### What is not here

`GET` and `PATCH /v1/fleet/profile` — SHIP-79's service area and specialties — are not modelled, not
called and not assumed. They belong to the same domain, to another track, and to a branch that has
not merged; the wave rule is that a ticket never depends on same-wave work from another track.

#### How it was demonstrated

`make flutter-check` green: **429 host tests** (up from 353), the analyzer clean, and the
environment test per build flavour. Separately, every request `ApiFleetRepository` makes was replayed
byte-for-byte against the API on port 8092 with a real provider session — list, add, read, edit,
deactivate, reactivate, the `409 fleet_duplicate_registration` a reactivation collides with when a
replacement is already in service on the same plate, and the `400 idempotency_key_required` a write
gets without a key. **What is held by widget test alone is the screens**: what is drawn, what is
tapped, and which of them a customer is refused. `make verify` does not cover this ticket — it
exercises HTTP endpoints, and this one adds none.

### SHIP-99 — the feed, and the filters the endpoint deliberately does not accept

The provider half of the shell stops being a placeholder and becomes the work, which is what
SHIP-76 did for the customer half. `ProviderJobFeed` reads `GET /v1/jobs/open` (SHIP-82), draws one
card per job, pages by cursor, and narrows by pickup state and by whether a job already has bids on
it. The fleet keeps its entry point — the same `manage-vehicles` key SHIP-98 gave it — moved inside
the feed, because an empty feed is the question the fleet is the answer to.

#### The finding: the endpoint accepts no filter, and the *Done when* says "with filters"

`GET /v1/jobs/open` takes `limit` and `cursor` and **nothing else**. That is deliberate and the
contract argues it: "there is no parameter that widens this and none that narrows it — no state, no
vehicle, no goods type", because eligibility is the platform's decision and a filter parameter would
be a second place for that answer to be argued with (`Docs/07` §3).

The same paragraph names where narrowing does belong — *"a provider narrowing their own feed further
is SHIP-99's client-side business"* — so this is a delegation rather than a gap, and the ticket was
built to it. **What matters is that the narrowing is subtractive and can only be subtractive.** Every
job it is given has already passed the platform's four checks; all it can do is hide some of them
from the person who asked. `open_jobs_filter_test.dart` asserts that as a property over a set of
filters rather than as a comment: whatever is selected, the result is a subset. There is no
arrangement of the controls that shows a provider a job the platform withheld, which is the property
that stops a client-side filter disagreeing with server-side eligibility.

**The options are derived from the jobs, never compiled in.** `CLAUDE.md` keeps anything that changes
under operational pressure server-side and Dart has no over-the-air path, so a list of the eight
states written into the client would be a vocabulary needing a store release. The chips are built
from the states the platform actually sent. A facet with one option is not drawn at all — a single
chip can only ever hide the whole feed.

**Two filters is what the response can honestly support**, and it is worth saying which were
considered and dropped. Sorting or filtering by price is forbidden outright (`Docs/01` §4.3, below).
Distance needs a coordinate the response withholds on purpose. `vehicle_requirement` is free text
until SHIP-79's capability vocabulary exists, so matching on it would be the client inventing an
eligibility rule. What is left — where the job starts, and whether somebody has already bid — is
what a provider deciding whether to price a job actually asks.

#### A filter over a paged list is a filter over what was read, and the screen says so

The consequence a screen gets wrong. Narrow to a state that only appears on page three and nothing
matches until page three arrives. So the feed distinguishes **three** empty-ish states rather than
two: nothing eligible at all, which sends the provider to their fleet; nothing matching the filter
with more still to read, which offers the next page; and nothing matching with the list exhausted.
Conflating the first two would tell a provider with a perfectly good fleet that there is no work.

The counter beside the chips reads "11 of 20 jobs **read**", and the last word is load-bearing: the
list is paged, so "11 of 20" without it would be a claim about the marketplace.

#### The budget: a second type, and a closed key set asserted from the client's side

`Docs/01` §4.3 keeps the customer's maximum away from a provider — not as an amount, a band, or a
"budget supplied" flag. The client mirrors what the platform did rather than reusing `Job` with a
field skipped: `OpenJob` is a separate type with no field a budget could go in, and
`budget_stays_on_the_customer_side_test.dart` needed no new entry on its allow list, which is the
cheapest possible confirmation that no provider-facing file names the budget.

Two tests go further, and both were written because SHIP-83 found that *searching for the word*
catches `budget_cents` and misses `max_price`. `open_job_test.dart` decodes a payload carrying
`budget_cents`, `max_price`, `budget` and `customer_maximum_cents` and asserts the round trip
produces **a closed set of keys**, none containing budget, price, maximum, amount or cents.
`provider_job_feed_test.dart` renders that same payload through the real decoder and asserts none of
the numbers, and no affordance implying a maximum was or was not supplied, reaches the screen.

`JobRegion` is held to the same closed set — `suburb`, `state`, `postcode` — which is SHIP-83's other
disclosure decision confirmed from the client: no street line, and **no coordinate**, because `jobs`
geocodes the whole address so a pickup coordinate *is* the street line written as two numbers.

#### `ProviderOnly` was not used, and that is a decision rather than an oversight

SHIP-98's gate exists because a route is reachable by a deep link with no button involved, and
`GET /v1/fleet/vehicles` answers a customer `200` with an empty page rather than refusing them. Both
halves of that are true of this endpoint as well — the contract lists "a customer who followed a link
meant for the other role" among the four situations answered with `[]`.

**The feed has no route of its own.** It is the provider half of `/home`, exactly as
`CustomerJobList` is the customer half, and the shell's own role switch is what selects it. Wrapping
it in `ProviderOnly` would be a second answer to a question the shell has just answered in the same
frame — the arrangement SHIP-98's own `_AddVehicleButton` argues against — and no test could tell
the two apart. `provider_job_feed_test.dart` asserts what actually matters instead: a customer signed
in reaches their own list and **`feed.calls` is empty**, so nothing is read on their behalf.

**SHIP-100 is where `ProviderOnly` gets its next real client**, because `/jobs/open/{id}` is an
identifier-bearing route a notification payload can deliver somebody straight to.

#### Two smaller things worth finding later

**`/v1/jobs/open` is served by `internal/fleet` and modelled in `features/jobs`**, and neither is a
mistake. `fleet` owns eligibility so the route is declared in `routes_fleet.go` and the shape lives
in `contracts/paths/fleet.yaml`; `Docs/07` §2 puts *discovery* in the `jobs` feature, so the client
puts it beside the customer's list. Both halves of the marketplace are now in one Dart package and
they share no type and no widget — two response shapes, two cards, two controllers.

**`OpenJobsRepository` is separate from `JobsRepository`** for the same reason the platform wrote a
second response type: one repository holding both would be one place a screen could reach the wrong
shape. It models the feed alone. `GET /v1/jobs/open/{id}` is served and deliberately not modelled —
an endpoint no screen calls is dead code nothing holds to the contract, and it arrives with SHIP-100.

#### How it was demonstrated

`make flutter-check` green: **495 host tests** (up from 429), the analyzer clean, and the environment
test per build flavour.

Separately, and this is the part worth trusting: **the *Done when* was run on an iPhone 17 simulator
against the API on port 8092**, through the real `dio` transport and the real Keychain, with nothing
substituted. The fixture was one verified provider serving VIC and NSW with a box truck in service,
and 26 eligible published jobs — every one of which carried a budget its customer had stated — plus
seven QLD jobs the platform excluded and the device never saw. What the run showed, in order: the
feed drawn on arrival; the state chips present and the **status** chips absent, because every job on
page one was `Open` and a one-option facet is not drawn; narrowing to VIC giving `11 of 20 jobs
read`; the cursor followed to a second page; the status facet *appearing* once that page brought a
`Negotiating` job, which is the derived-from-the-data rule working on real data; narrowing to VIC
again giving `14 of 26 jobs read`, where the denominator is the proof the page was appended rather
than substituted; VIC and Negotiating together giving `1 of 26`; and the whole feed scrolled end to
end with no budget, in any form, on any screenful.

The acceptance test that drove it was deliberately **not committed**. It needs a booted simulator, a
running API and a hand-built fixture, so it is a check a person invokes rather than one CI can run —
the same reasoning `make flutter-integration` already carries. `make verify` does not cover this
ticket: it exercises HTTP endpoints and this one adds none.

### SHIP-100 — the offer, the key that makes a retry safe, and the queue it deliberately does not use

`Docs/09`'s *Done when* is one sentence — "a provider can review a job and submit a bid" — and it
spans two of `Docs/07` §2's features. `GET /v1/jobs/open/{id}` (SHIP-83) is the review;
`POST /v1/jobs/{id}/bids` (SHIP-84) is the offer. The feed's cards became tappable, `/jobs/open/{id}`
became a route, and `internal/bidding`'s client half stopped being a `library;` with a note in it.

#### Queue or send: `Docs/07` §4 answers it, and SHIP-124 had already made the wrong answer impossible

The brief asked this to be decided and recorded, so: **the bid is sent directly.** `Docs/07` §4 names
the exclusion in as many words — *"what is deliberately not offline: bidding, awarding, and
negotiation. These are competitive, time-sensitive, and multi-party; a stale local decision is worse
than an honest 'you are offline'."* A price queued at a loading dock and sent four hours later is an
offer against a job that may have been awarded since, made by somebody who believes they have bid.

**The interesting part is that this was not a rule to follow.** SHIP-124 wrote `OperationKind` with a
**private constructor** and exactly two members, both `delivery.*`, so the set of queueable
operations is closed by the compiler. A bid cannot be enqueued: there is no value to enqueue it as.
That entry called this "what a client does with an operation it can never send", and the first
ticket to meet it from the other side found that it had nothing to decide — which is the whole
benefit of a closed set over a documented one. Nothing in `features/bidding` imports `core/queue` or
`core/sync`.

**What makes a retry safe instead is two mechanisms, and they are not the same one.** `ActionKey`
(SHIP-51) mints one key per action and holds it in exactly one circumstance — the previous attempt
failed **without saying whether the platform acted on it** and the request now being sent is
identical. A dropped connection is precisely that case. SHIP-84's `uq_bids_idempotency` on
`(job_id, provider_id, idempotency_key)` is the other half: a **stored column** rather than a cached
response, so the retry is answered `200` with the bid the first attempt placed even after Redis has
forgotten it. Without it the retry would meet the *one live offer* index instead and be told
`bidding_already_bid` — a client showing a failure for a bid that is live and awaiting an answer.
That entry drew the division and this is the client standing on the correct side of it.

**Both directions are tested on the keys the repository actually received**, because getting either
backwards is the defect. A dropped connection and then a retry sends `keys[0] == keys[1]`; a `422`,
a corrected price, and a resend sends `keys[0] != keys[1]` — the platform fingerprints method, path
and body, so an old key on a changed body is `idempotency_key_reused`.

#### Optimistic local state, on a surface that is never offline

`Docs/02` §3.1 asks the client to show optimistic local state clearly marked as pending and reconcile
to whatever the platform returns. The first half is about the queue and does not apply here, and
saying so is more useful than inventing a pending state: **nothing is drawn as offered until the
platform says so.** The attempt is marked — the button disables, a spinner replaces its label — and
`PlaceBidState.bid` is assigned from the response and never from the form.

**The second half does apply, and it is not a formality.** A retry is answered with the offer the
*first* attempt placed, so a screen that rendered what was typed would show a provider a price they
are not standing behind. A test places `450` against a repository answering `399` and asserts the
screen shows `$399.00`.

#### Two features, one screen, and they meet in the router

`Docs/07` §2 puts discovery and detail in `jobs` and bids in `bidding`, and features do not import one
another — `architecture_test.dart` enforces it in both the `package:` and the relative form. This
ticket is the first whose *Done when* crosses that line.

**`OpenJobScreen` declares that it needs a panel and `core/routing/app_router.dart` supplies
`PlaceBidPanel`.** That is the composition-root arrangement the Go side already uses: a domain and
its adapters meet in `cmd/api` and nowhere else, and here the router is the only place allowed to
know about both. `bidPanel` is a required parameter rather than a nullable one, because a job detail
screen with no way to bid is half a ticket and a nullable parameter is how the missing half stops
being obvious.

The panel therefore knows the job's **identifier and nothing else about the job**, which turned out
to cost nothing: SHIP-84 does not require a bid's timing to fall inside the job's windows, because
offering a different day is a legitimate offer the customer may decline.

#### `ProviderOnly` moved to `core/auth`, and the reason it had to is the same rule

SHIP-99 predicted this — *"SHIP-100 is where `ProviderOnly` gets its next real client, because
`/jobs/open/{id}` is an identifier-bearing route a notification payload can deliver somebody straight
to"* — and predicted it correctly. What it could not predict is that the widget was in
`features/fleet`, so the second caller could not import it. Duplicating it would have been the decay
the boundary exists to prevent, so it moved to `core/auth/provider_only.dart`, which is what
`Docs/07` §2 prescribes for behaviour two features need. Nothing about what it does changed, its keys
are unchanged, and SHIP-98's own tests pass untouched.

**Its copy generalised in the move**, deliberately: it named vehicles while the fleet was its only
caller, and a sentence about vehicles in front of somebody who followed a link to a job is worse than
the general one. Threading a per-surface sentence through four call sites buys a few words at the
price of a parameter every future caller has to think about.

**And the reason the widget is needed here is different from the fleet's, which is worth recording
because the fleet's argument does not transfer.** `GET /v1/fleet/vehicles` does not check the
caller's role and answers a customer `200` with an empty page — so a customer would see a form and a
`403` at the end of it. `GET /v1/jobs/open/{id}` *does* refuse them, with `404`, byte-identically to a
job that does not exist. That is the correct answer on the wire and a poor thing to render: "we could
not find that job" is not what happened. **Both surfaces need the widget; only one of them needs it
because the platform is permissive.**

#### The 404 is one message, and the screen does not try to be more specific than the platform was

Nine cases on the platform answer identically — outside the service area, no vehicle that can carry
it, unverified, no longer open, never existed, and the owning customer reading their own job at the
wrong address. SHIP-83 made that indistinguishability the control. So the screen has **one** state
for all of them, it says "this job is not one you can bid on" rather than naming a reason, and it
**offers no retry** — asking again cannot change the answer. A failure that is not a `404` is the
other state, and that one does have a retry, because "there is no such job for you" and "we could not
find out" are different things to be told.

#### The budget guard: §9's client-side item is closed, and the mutations are recorded

`Docs/11` §9 has carried this since wave 6: `budget_stays_on_the_customer_side_test.dart` searches
`lib/` for the regular expression `budgetCents|budget_cents`, so it catches a rename in one direction
and misses one in the other. **Re-run against this tree before anything was written, and both halves
still held**: injecting `@JsonKey(name: 'max_price') int? maxPrice` into `OpenJob` **passed** that
file, and renaming it `budget_cents` **failed** it, naming the file.

**One refinement the entry did not have, found by running the whole suite rather than that file.**
The `max_price` mutation was not undetected on today's tree — it failed **two tests in
`open_job_test.dart`**, which SHIP-99 wrote and which holds `OpenJob.toJson()` to an exact key set.
So the exposure was smaller than §9 stated, and its real shape was different: the closed key set
existed for one type, in a file somebody had to remember to write. **A type with no such test —
which is exactly what this ticket's `Bid` would have been — had no guard against a rename at all.**

**Closed the way SHIP-83 did it**, in `budget_stays_on_the_customer_side_test.dart` itself, so that
the two guards live in one place and neither can be quietly deleted alone:

| Guard | What it catches | What it cannot |
|---|---|---|
| The source scan (kept) | A widget that renders a budget being reused on a provider screen — the failure the rule is realistically broken by | A rename |
| A registry of provider-facing models, each held to a **closed key set** | A field added to `OpenJob`, `JobRegion` or `Bid` **whatever it is called**, and the *value* surviving a round trip under an innocuous key | A provider-facing model in a file nobody registered |
| A source scan over the registered files for `@freezed` types | A **type** added to a registered file and not registered | The same |

**Verified by mutation in three directions, and each fails exactly what it should.** `max_price` on
`OpenJob` now fails `budget_stays_on_the_customer_side_test.dart` on both the key set and the value
search, where before it passed. `customer_ceiling` on `Bid` — a budget under a name containing
neither "budget" nor "price" nor "maximum" — fails the closed key set, which is the axis a spelling
check cannot have. And a new unregistered `@freezed` type in `open_job.dart` fails the structural
test naming the type and its file. Every mutation was reverted immediately and `git diff` confirmed
the tree, generated files included.

**What is left is named rather than argued away**: a provider-facing model in a file nobody adds to
`_providerFacingFiles`. That is the same fail-closed-by-registration property `internal/boundaries`
gives a ninth Go package — adding one is a decision somebody records rather than something that
happens — and the file says so in its own header.

#### Three smaller decisions worth finding later

**`centsFromAud` converts on the text and never through a `double`.** This is the first screen that
takes money *from* a person, and `(double.parse('450.55') * 100).round()` is right far more often
than it is wrong. It refuses more than two decimal places rather than rounding them, because `45.005`
is a price the platform cannot store and rounding it quietly would be the client deciding what the
offer was. No upper bound is checked: `maxOfferCents` is `internal/bidding`'s and `Docs/06` §5.3 keeps
it server-side.

**`rfc3339` exists because `DateTime.toIso8601String()` carries no offset.** A local value encodes as
`2026-08-20T09:00:00.000`, which is not RFC 3339, and `time.Parse(time.RFC3339, …)` refuses it — the
provider is told "that is not a date" about a date they picked from a calendar. The offset is sent
rather than the instant converted to UTC on the device, because the platform normalises anyway and a
second timezone conversion is a second thing to keep correct.

**No client-side ordering or bound check on the timing, on purpose, and there is a test that would
fail if somebody added one.** Both instants come from the same pickers and land on the same value, so
the form sends a delivery that is not after its collection — which `Offer.validate` refuses, with a
message rendered under the field. Two definitions of a rule is one more than `Docs/07` §3 permits, and
the copy on the device is the one that cannot be corrected without a store release.

#### How it was demonstrated

`make flutter-check` green in this worktree: **686 host tests** (up from 594), the analyzer clean,
and the environment test per build flavour. The *Done when* is `place_bid_test.dart`'s first test,
driven through the real app from the sign-in screen: a provider signs in, taps a job in their feed,
reads it, prices it, chooses two instants through the pickers the screen actually uses, sends it, and
sees the offer the platform recorded. `make verify` does not cover this ticket and its count does not
move — that script exercises HTTP endpoints and this one adds none, the same position SHIP-98,
SHIP-99, SHIP-124 and SHIP-125 are in.

**No acceptance run against a live API was performed**, which is a step down from SHIP-99 and is
recorded rather than glossed: that ticket ran its *Done when* on an iPhone 17 simulator against the
API on port 8092 with a hand-built fixture. The equivalent here needs a verified provider, an
eligible job, and a bid that must then be withdrawn before the run can be repeated — and the second
run is where `bidding_already_bid` would be met rather than the `201`. It is worth doing before this
reaches a device, and `make flutter-run` in this worktree already points at the right port.

#### Shared surfaces

`apps/mobile/**` and this file. No Go, no route file, no contract, no migration — which is why this
branch merges first.

### SHIP-106 — the first delivery endpoint, and the question SHIP-105 left it

`POST /v1/jobs/{id}/driver`. The awarded provider names who is carrying the job, and the job moves
to `Driver assigned` in the same transaction. It is the first HTTP surface in `internal/delivery`,
which held `doc.go` and SHIP-110's milestone model and nothing else.

**The interesting part is not the endpoint. It is that it needs two facts it is not allowed to
know.** Whether the caller won the work is a `bidding` fact, and whether the job may move at all is
`jobs`' transition guard — and domains do not import each other. So `delivery/ports.go` declares
two interfaces and `cmd/api/routes_delivery.go` supplies both:

| Port | What delivery asks | Who answers today |
|---|---|---|
| `Awards` | which provider holds this job, or nobody | `acceptedBids` in `cmd/api` — one `SELECT` against the accepted bid |
| `Jobs` | move this job to `Driver assigned`, in my transaction | `jobLifecycle` in `cmd/api`, over `jobs.Service.Transition` |

**`Jobs` names one move rather than taking a status.** A port shaped "move this job to whatever I
say" would be the transition table's second opinion arriving through the back door — this domain
would be choosing the target status, and the one thing CLAUDE.md says about job status is that
nothing outside the guard chooses it. A domain that needs another move declares another method.

**Refusals cross the seam as an enumeration, not as errors.** `errors.Is(err, jobs.ErrJobNotFound)`
is an import by another name, so `JobMove` carries the four outcomes — moved, no such job, already
assigned, not permitted — with a nil error, and only a real failure stays an error. This is the same
constraint `jobs.Geocoder` met and answered with `found bool` (§9); a bool could not carry four.
**Its zero value is `JobMoveUnrecognised` and is refused**, so a half-written adapter cannot read as
success.

**The accepted-bid query is in `cmd/api`, and that is deliberate rather than convenient.**
`internal/bidding` is `doc.go` and `model.go`: SHIP-80 built the table and the one-accepted-bid
index, and SHIP-92 — single-owner, never parallelised (§8) — is what gives that domain a store. The
alternatives were putting the query in `delivery/postgres.go`, where a dependency on another
domain's table reads as a table delivery owns and the import lint cannot see it, or waiting for
SHIP-92. It is five lines in the composition root with a comment saying what deletes it. **When
`bidding` grows its store, the type goes and a method is passed instead; nothing in `delivery`
changes.** That is what the port bought.

**Who may assign — the question 000600 explicitly left to this ticket.** Only the provider on the
accepted bid. Not an administrator: `ck_users_role` refuses `'admin'`, admin sign-in is a separate
system (SHIP-147), and Docs/02 §1 gives `Driver assigned` one primary actor. **So no migration was
needed** — recording *who assigned* would have meant the polymorphic `actor_type`/`actor_id` pair,
and with one possible assigner the `Awarded → Driver assigned` history row already says it. The
delivery block is still at `000601`.

**A stranger's job and a job that does not exist are one 404, byte-identically.** Which jobs a
competitor won is commercial information they never published. The domain keeps
`ErrNotAwardedProvider` and `ErrJobNotFound` apart so a test can tell "the stranger was refused"
from "the job stopped existing", and the wire cannot.

**Self-assignment fills the mobile and asks for the name, which is the honest half of a gap.** The
platform holds the provider's verified number (SHIP-36) and holds no name at all — `users` has no
name column and `profiles` is still `doc.go`. So `{"self": true}` takes the number from the account
and `driver_name` stays required. Sending `self` *and* a mobile is refused rather than resolved
either way: a provider who sent both has two numbers in mind, and the loser is where SHIP-107's link
would have gone. When profiles lands, the name defaults too and the change is additive.

**A repeat naming the same driver is absorbed; a different driver is refused.** The idempotency
middleware handles the retry that reuses its key, and this handles the one that does not — the same
call `jobs.Cancel` and fleet's deactivation make. `delivery_driver_already_assigned` is the answer
to a *different* name, because **replacing a driver is not this ticket and no ticket owns it yet**:
000600 describes the shape (end this assignment, insert another) and SHIP-109 reissues a link to the
same driver, which is a different intent. Whoever picks it up has a sentinel and a code already in
place.

**What stops two drivers is `uq_driver_assignments_active`, not the check in front of it.** The
service reads the live assignment to give a legible answer; the partial unique index is what is
right when two requests race and both read nothing. A test inserts directly, past the check, to see
the index refuse it.

**The assignment row is written before the transition is attempted, so a refused move takes it
with it.** Two tests and one `make verify` check assert the rollback rather than the refusal: a job
that answered 409 and kept a driver is the exact inconsistency the single transaction exists to
prevent.

**`scripts/verify/70-delivery.sh` is new, and it is the only place the `cmd/api` half is
exercised.** The Go tests supply their own copies of both ports — a test file may import `jobs`, and
`cmd/api` has no database in tests — so the real adapters are demonstrated against the built binary
or nowhere. The fixtures reach `Awarded` through 000402's protocol and award through one accepted
bid, so even the fixture cannot bypass the guard. `make verify` went from 268 checks across 11
sections to the figure at the top of this section.

**Not built, deliberately: no milestone row and no domain event.** Assigning a driver is one of
Docs/01 §4.4's five recordable milestones, and whether the endpoint should write one is SHIP-111's
question rather than this ticket's — `milestones` is SHIP-110's table and SHIP-111 owns what writes
to it. The transition already emits `job.status_changed` through the outbox, so nothing downstream
is deaf to the assignment meanwhile.

### SHIP-111 — the milestone endpoint, and which mechanism actually makes a retry safe

`POST /v1/jobs/{id}/milestones`. The awarded provider records that the driver has set off, collected
the goods, or begun the journey; the milestone is written to SHIP-110's table and the job moves in
the same transaction when `Docs/02` §2 has a row for it.

**The interesting part is the second sentence of the *Done when*: "once per idempotency key".** That
is not what SHIP-15's middleware gives you, and reading it as though it were is the mistake this
ticket exists to not make.

| Mechanism | What it guarantees | For how long |
|---|---|---|
| `httpx.Idempotent` (Redis) | the *response* is replayed and the handler never runs | while the entry lives — a TTL, an eviction, a failover |
| `uq_milestones_idempotency` (000602) | the *row* cannot be written twice | permanently |

A driver records a milestone in a pickup bay with no signal (`Docs/01` §4.4 names the conditions) and
the phone syncs when it finds a tower, which may be the next day. That outlives any TTL worth
setting, so the request runs a second time — and the only thing standing between it and a duplicate
on the customer's timeline is the index. **Redis makes the retry cheap; the database makes it
correct.** The handler is written as though the middleware were not there: it inserts with
`ON CONFLICT (job_id, idempotency_key) DO NOTHING`, and when the insert declines it reads what that
key recorded and answers with it.

`ON CONFLICT` rather than catching a unique violation, and each word is load-bearing: a violation
aborts the surrounding transaction, which still has a status transition to make, so catching it
would mean unwinding to a savepoint to ask a question the conflict has already answered. `DO NOTHING`
rather than `DO UPDATE` because the table is append-only and 000601's trigger would refuse the
update — which is the right refusal, since a retry must return what was recorded rather than
overwrite it with a second attempt's timestamp. The index is inferred by its columns and its
predicate because a *partial* unique index has no constraint name to name.

**The key is scoped per job.** It must not be able to refuse a different action — a client reusing
one value across two jobs has made two requests that both deserve to succeed — and it must not be
able to reach another caller's record, which `job_id` already prevents because only the awarded
provider may write a milestone on a job. A subject column would be a second copy of that fact, and a
wrong one the moment SHIP-108's driver records under the same key.

**`make verify` tells the two mechanisms apart by deleting the cached response.** The same key sent
three times: the first records, the second is replayed by the middleware — **as a `201`, byte for
byte, because a replay is the stored response and not a new one** — and then `redis-cli del` removes
the entry and the third request runs all the way to the table and is answered `200` from the row it
already wrote. `Idempotency-Replayed: true` on the second and absent on the third is what makes the
claim checkable rather than asserted. One milestone row and one transition at the end of all three.
Seventeen checks were added to `scripts/verify/70-delivery.sh`, taking the run from 282 checks across
12 sections to the figure at the top of this section.

**The concurrent case is a test rather than a check.** Eight goroutines, one key, eight
transactions, and the assertion is that exactly one believes it recorded anything. The middleware
would refuse seven of them with `idempotency_request_in_progress` and never let them reach the
service, which is exactly why the test bypasses it: two API instances, or a cache miss on both sides
of a retry, is the same race with nothing in front of it. A check-then-write implementation passes
that test approximately never.

**Actor permissions are the other half, and the case worth writing down is the customer.** `Docs/02`
§3 permits "the awarded provider, their assigned driver, or an administrator acting with an audit
reason", and only the first can present a credential today — the driver's job-scoped token is
SHIP-107 and SHIP-108, and admin sign-in is SHIP-147. So the customer who *owns* the job is refused,
with the same `404` a stranger gets: they are shown milestones and they confirm the delivery at the
end, and neither of those is recording one.

**The `Jobs` port grew three named methods rather than one taking a status.** `MoveToEnRouteToPickup`,
`MoveToPickedUp`, `MoveToInTransit` — the shape SHIP-106 argued for, held to when it stopped being
one method. A port shaped "move this job to whatever I pass" would put the target status in
`delivery`'s hands, and the four `jobs.Status` values now appear in `cmd/api` and nowhere else. The
cost is four lines per move in the adapter; the property bought is that **the set of moves this
domain can ask for is fixed at compile time**.

`Delivered` has no method behind it, and that is the enforcement rather than an omission — see
below. The translation of refusals into outcomes was also collapsed into one `move` helper in the
adapter, because four copies of it could translate `jobs.ErrAlreadyInStatus` four ways and every one
of them would still compile and still pass its own test.

**`JobAlreadyDriverAssigned` was renamed to `JobAlreadyInStatus`.** With one move on the port the
specific name was clearer; with four it would be wrong, because a driver re-recording
`en_route_to_pickup` on a job already there is that outcome and has nothing to do with an
assignment. One name for one concept, and the compiler found every use.

**The two clocks are kept apart in both tables, and the transition carries the milestone's claim.**
`recorded_at` is the actor's and is never corrected or bounded — 000601 says why, and a test sends a
time ninety minutes in the past and asserts the column holds exactly that. `accepted_at` is the
platform's, written by the trigger that refuses to be told what to say. The `job_status_history` row
the move leaves carries the *same* actor claim, because a customer shown 06:40 by one and 08:10 by
the other has been told the delivery happened twice. The service takes a `clock.Clock` for the case
where the client sends nothing — an online recording, where there is one clock — which is what
SHIP-106 predicted it would need and is the only reason a clock is there.

**Two decisions this ticket was left, and both went to "no".**

*Does assigning a driver write a `Driver assigned` milestone?* **No, and nothing writes that value
to the table.** `Docs/01` §4.4 numbers it among the five a provider can record, so it stays in
`delivery.Milestones` and in `ck_milestones_milestone`; the endpoint refuses it with a `422` pointing
at `POST /v1/jobs/{id}/driver`. The reasoning is that a milestone row exists to hold a claim the
platform cannot verify and may not be able to act on — that is why it carries the actor's clock and
why it may move nothing. Assigning a driver is not that. It is made online through an authenticated
session against a job that must be `Awarded`, so there is one clock and it cannot be "recorded but
absorbed"; and the fact is already held twice, by `driver_assignments` and by the
`Awarded → Driver assigned` history row. A third copy in an append-only table would outlive a
replaced assignment and could only ever disagree. `Docs/02` §1 supports the split from the other
side: `Driver assigned` is the one of the five whose primary actor is the provider alone, and the
other four all read "Provider / Driver".

*Does the endpoint accept `Delivered`?* **No, until proof exists.** CLAUDE.md's invariant is that
delivered requires photo proof or a recorded exception and never neither, `Docs/01` §4.4 decides it,
and neither can be captured until SHIP-114…SHIP-116. So every `delivered` is refused with
`delivery_proof_required`, and there is no `MoveToDelivered` on the port — the refusal cannot be
removed by editing one file. **SHIP-118 narrows this from "always" to "when the job has neither",
which is the ticket it was already going to be**; the sentinel, the code and the message already say
what that ticket needs to say.

**What is left in place for SHIP-112, deliberately.** A late milestone — a queued `Picked up`
arriving after `In transit` is recorded — is refused today with `delivery_milestone_not_permitted`
and the transaction rolls back whole. `Docs/02` §3.1 says it must be *absorbed*, and that is
SHIP-112's five points rather than this ticket's. What SHIP-111 owes it is a seam: the milestone row
is written before the move is attempted and is already independent of whether the job moved, so
**SHIP-112 changes one `case` of one `switch` — `JobNotAssignable` in `Service.RecordMilestone` —
and nothing else**. There is no uniqueness on `(job_id, milestone)` to get in its way (000601 refused
to add one for exactly this reason), the two clocks it needs are already stored, and the error code's
own description tells clients to keep the update rather than discard it, so a client written against
today's behaviour will not lose records when the answer changes. A test marks the boundary and says
in its name that it is temporary.

**A repeat that is not a retry is a second row.** A driver who reaches a pickup, finds nobody there,
and sets off again records `en_route_to_pickup` twice under two keys: two milestones, one
transition, which is `Docs/02` §5's failed attempt and the case 000601 has no uniqueness for.

**The delivery block moved to `000602`** — one column, one check, one partial unique index. It is the
first migration in this repository written to hold a guarantee that a piece of infrastructure was
already believed to provide.

### SHIP-15m — the wave-6 pre-step, and the seam that is deliberately empty

The sixth prep ticket, and the second at three points rather than five. Three items, and only the
first of them is why the ticket had to exist before the wave rather than during it.

| Surface | Before | After |
|---|---|---|
| `RequireDriverToken` | Declarable in the manifest, enforced by nothing, and **only addable from `cmd/api/routes.go`** — a shared surface the delivery track may not edit, which made SHIP-108 unbuildable from a domain branch | `newRouter` takes the guard as an argument. SHIP-108 fills in `cmd/api/driverauth.go` and edits neither `routes.go`, `manifest.go` nor `Deps` |
| The Kafka replication factor | `cmd/topics -replication`, defaulting to `events.DefaultReplicationFactor`. The third setting in three waves parked against `internal/config` | `KAFKA_REPLICATION_FACTOR`, bounded 1..10 at load, **with the flag kept as an operator override** |
| Three rules found in wave 5 | Recorded in §9 by a pass that could not edit `CLAUDE.md` or `Docs/10` | In `CLAUDE.md`'s worktree table and merge instructions, and in `Docs/10` §9.2 |

**The seam is the deliverable, and the mechanism is deliberately absent.** `newDriverTokenGuard`
returns `nil` today. SHIP-107 signs the job-scoped token and SHIP-108 verifies it; nothing here
signs, expires or verifies anything, and a reviewer who finds token logic in this ticket has found
a defect.

**`nil` means the class is *absent* from the guard map, and that distinction is the whole design.**
A route declaring `RequireDriverToken` still panics at startup, naming the class, exactly as it did
before — §8 has called that "the failure direction is safe" since SHIP-44 and it is unchanged. The
tempting alternative is an entry that refuses every request, and it is strictly worse: it turns a
process that will not start into a route that answers `401` forever, which is indistinguishable from
an expired credential to every client and to whoever gets asked about it. `guardsFor` says so in
place, and two tests fail if somebody makes the map permissive.

**Demonstrated by mutation in both directions rather than asserted.** Mapping the class to a guard
that refuses everything fails `TestWithNoDriverGuardTheClassIsAbsentAndTheRouteRefusesToStart` and
`TestTheClassIsServedExactlyWhenTheConstructorSuppliesAGuard`; dropping a supplied guard on the floor
fails `TestASuppliedDriverGuardServesAndGuardsTheRoute`. The third of those is the one written to
survive SHIP-108 unchanged: it asserts the *rule* — the class is served exactly when
`newDriverTokenGuard` supplies a verifier — rather than today's answer to it. No route is registered
from a `_test.go` `init`, for the reason `auth_test.go` gives, so `routes_golden.txt` is untouched.

**The guard is built in `main.go` and passed alongside `Deps`, not added to it.** The precedent is
`authenticate`, and the argument is the one the note above `type Deps struct` makes: `Deps` is what a
*handler* is built from, and a collaborator of the router is not one. It returns an error rather than
panicking for `newAccessTokenAuthenticator`'s reason too — a keyset that cannot be built is a
configuration error that will still be there after the next restart, and a service that came up
unable to verify any driver token would answer `401` to every driver while reporting itself healthy.
The unused `*config.Config` and `clock.Clock` parameters are there so that the call in `main.go` is
written once: a signature that changed when the body was filled in would put a shared file back in
SHIP-108's diff.

**A decision was taken about where the job-scoped grant lives, and the answer is `internal/delivery`
— but the seam does not name it, which is why it could be taken cheaply.** A guard is an ordinary
`func(http.Handler) http.Handler`, so nothing in `cmd/api` or `internal/httpx` has to know what a
verified driver token puts on the context. Three candidates and the reasoning against two of them:

- **Not `internal/authctx`.** That package holds the mobile session's `Subject`. A job-scoped grant
  sitting beside it is one helper function away from the exchange `Docs/10` §5 forbids — and the
  conversion would be invisible, because every domain already reads that package. `identity` refuses
  the driver audience today with a test; the other direction is SHIP-108's, and it is much easier to
  keep if the two types never share a package.
- **Not `internal/httpx`.** Infrastructure imports neither a domain nor an adapter (SHIP-15c), and
  `httpx` needs nothing here — unlike `httpx.Authenticator`, which exists because the middleware
  itself had to name the subject type. Declaring a grant type there would hand every domain the
  ability to read one when exactly one domain will ever serve a driver-token route.
- **`internal/delivery`, which is the consuming domain** (`Docs/06` §4.1). The guard supplied from
  `cmd/api` may import it, because `cmd/api` is where a domain and its collaborators meet. The rule
  SHIP-108 inherits is written into `driverauth.go`: **do not resolve a driver token into an
  `authctx.Subject`.** A test route in `driverauth_test.go` asserts the negative — a driver-token
  route that reaches its handler with a subject on the context fails.

**`RequireAdmin` is untouched and still absent from every branch.** SHIP-147 gets the same treatment
— a second parameter and a second constructor beside `newDriverTokenGuard` — and until then a route
declaring it stops the process rather than being served open. Generalising the seam to cover both was
considered and not done: one caller is not a pattern, and the shape SHIP-147 wants is knowable only
once it exists.

**One thing the seam does not carry, recorded rather than fixed: a driver-token request scopes its
idempotency key to `anonymous`.** `httpx.SubjectScope` keys on the subject, a driver token
deliberately produces none, and the scope is computed group-wide *outside* `Idempotent` — so no
per-route guard can influence it, whatever it puts on the context. The posture is the same one §6
already accepts for public routes: `replayOrRefuse` fingerprints method, path and body, so reading
somebody else's stored response means reproducing their exact request, and on a driver route that
means already holding the job identifier. It is defensible and it is not free, so it is in §9 for
whoever writes SHIP-112 rather than left for them to discover.

**`KAFKA_REPLICATION_FACTOR` keeps the flag, and that is the interesting half.** The flag is not a
workaround to be tidied away now that configuration exists: an operator applying the topic set to one
cluster by hand should type the number, not set an environment variable. So configuration is the
default and the flag overrides it — which needs `flag.FlagSet.Visit` rather than a zero check,
because a flag's default cannot be a value `config.Load` has not produced yet, and treating `0` as
"not given" would make `-replication 0` silently apply the configured factor instead of being
refused. `internal/config` declares its own copy of the default rather than importing
`internal/events`: config has **no internal dependencies at all** and every binary loads it, so the
import would pull the event catalogue and the database driver into the configuration of processes
that publish nothing. `cmd/topics` imports both and holds the two copies together in a test.

**The three rules are wave 5's findings, moved to where somebody will read them.** The Kafka one is
the sharpest: the worktree table isolates the test database and the ports and pins
`COMPOSE_PROJECT_NAME` so every tree shares one stack, which is safe for PostgreSQL because each tree
gets its own database — and has no equivalent for a topic. **On a Kafka topic a fence must be an id,
not a timestamp.** `scripts/verify/80-notifications.sh` fences on `published_at` and has been correct
by luck; it is another domain's file and this ticket deliberately did not touch it, so the rule is
stated and the instance is left where §9 recorded it.

### SHIP-107 — the driver's token, and the claim it deliberately does not carry

The driver holds a signed, seven-day token naming exactly one job, minted when the provider assigns
them and available from nowhere else. It is the first half of the pair §8 has always kept with one
owner; SHIP-108 is the verifier.

**The interesting decision is an omission.** `Docs/10` §5 specifies separate key material, a
`shipper-driver` audience and one `job_id`, and all three are here — but the claim set is
`job_id`, `assignment_id`, `iat`, `exp`, `jti`, `iss`, `aud` and **nothing else**. No `sub`, no
`role`, no `sid`. An `authctx.Subject` is built from a user id, and this token has none: the
exchange CLAUDE.md forbids is not merely refused by a check somebody could remove, it has no
material to work from. A closed-set test holds the seven keys, for the reason SHIP-83's budget test
holds a closed set of response keys — searching for `sub` catches `sub` and misses `user_id`.

**`assignment_id` is required by code that already exists, not added speculatively.** A driver has
no account, so `milestones.actor_type = 'driver'` names their `driver_assignments` row (000601,
following 000401), and `RecordMilestone` says in place that this is the one field which changes when
a driver can present a credential. Without the claim, SHIP-108 could authenticate a driver and still
have nothing to attribute their work to. It also narrows what `Service.Driver` is for: the token
identifies the assignment, so that lookup exists to answer the question a claim cannot — whether the
assignment is still live.

**Seven days, and the comparison with identity's fifteen minutes is the wrong one.** There is no
refresh: a driver holds no second credential, so the TTL is the whole life of their access rather
than the short leg of a pair. It has to outlast a delivery — Australian long-haul freight is quoted
in days — and it does not have to outlast the job, because the 72-hour completion window
(`Docs/02` §6.1) is the customer's and the driver plays no part in it. Longer is a real cost: the
provider forwards the link by whatever channel they already use (`Docs/01` §4.5), so it lands in a
message thread and stays there. `DELIVERY_DRIVER_TOKEN_TTL`, refused above thirty days at load.

**Two keysets, and configuration refuses one.** `internal/config` grew a `Delivery` section with its
own `kid`-indexed keyset and its own development default — a *different* throwaway from identity's,
because sharing one would leave the two systems separated by the audience alone on every machine
anybody works on. `validate` refuses a configuration in which the two sets share a secret, **in
development as well**, since that is a structural rule rather than a deployment-hardening one. The
audience alone is still sufficient, and the tests prove it by constructing exactly the configuration
that is refused: both directions are exercised with the key material deliberately shared.

**Which direction of the separation this run could prove, and which could not.** Both, in Go —
`internal/delivery`'s test file imports `internal/identity` (the boundary lint skips `_test.go`,
which is what makes it possible), mints a real token from each issuer, and shows each parser
refusing the other's. That is stronger than identity's existing
`TestAccessTokenWithTheDriverAudienceIsRejected`, which builds a driver-audience token by hand
because it cannot import this package. **Over HTTP only one direction exists**: `make verify`
presents a real driver token to `GET /v1/jobs` and gets `401`. The other — a mobile token presented
to a driver route — needs a route that accepts one, and that is SHIP-108's.

**Minted inside the assignment transaction, which costs nothing and buys the rollback.** Signing is
an HMAC over a few hundred bytes and touches no I/O, so putting it before the commit means a failure
to sign takes the assignment with it. The alternative — commit, then mint — would leave a driver on
a job with no way to open it, and nothing downstream would notice, because the row is complete and
the status is right. `Service.AssignDriver` grew a fourth return value and two helpers,
`granted` and `refused`, so that a refusal cannot accidentally carry a token and a fifth success
path cannot forget to mint one.

**It is returned in the assignment response, and it is a credential in there.** Shipper sends no SMS
in the MVP, so the response to the request that created the assignment is the only place a link
comes from. That puts a credential in a body the idempotency middleware stores — under
`idem:v1:user:<provider>:<key>` (SHIP-44), which is a stronger scope than the anonymous one the
sign-in endpoints already store their tokens under, and worth stating because it is not obvious from
the shape. The token is never logged; the expiry is, which is what somebody diagnosing a dead link
actually wants. **No URL is assembled** — the portal's landing route is SHIP-120's to define, and a
base URL here would be this domain asserting a path in an application it does not own.

**A repeated nomination gets a fresh token and the previous one keeps working.** Both grant the same
one job to the same one driver, so nothing is widened, and refusing to reissue would strand a
provider whose phone lost the first response. **Invalidating the previous link is SHIP-109**, which
is a different intent and needs state this ticket does not write. There is a test asserting that
both links verify, so the ticket that changes it has a test to change.

**What makes it single-job is that the job identifier is inside the signature.** Two tests and one
`make verify` check take a valid token, rewrite `job_id` to another job, re-encode, and show the
signature refusing it — which is the property the *Done when* is asking for. Asserting that the
claim contains one job id would have proved nothing on its own. `make verify` also assigns a second
job and confirms its token grants that job and not the first, and verifies the signature under the
driver development key while confirming it does **not** verify under identity's — the one check that
makes "separate key material" a fact about the running service rather than about a struct.

**No migration, and the delivery block is still at `000602`.** The token is stateless and the
assignment row it names already exists. SHIP-109 is the ticket that will need state, and it has a
cheap route to it: a token whose `assignment_id` is no longer live names a row that says so.

`make verify` went from 355 checks across 12 sections to the figure at the top of this section, all
eleven of the new ones in `scripts/verify/70-delivery.sh`. The Go tests cover the issuer; the verify
section covers what only the wiring can show — that the *service as configured* signs with the
driver keyset and not with identity's, and that a real driver token is refused as a session by a
running instance.

**One finding about SHIP-15m's seam, and it is a gap rather than a fault.** The seam did exactly
what it promised for the *router*: `newDriverTokenGuard` is in place, `cmd/api/routes.go`,
`manifest.go`, `main.go` and `Deps` are untouched, and no route was declared with
`RequireDriverToken`. What it did not carry is the **configuration** its own comments say SHIP-108 is
certain to need — `newDriverTokenGuard(cfg, clk)` takes a `*config.Config` that had no driver-token
fields in it. So this ticket edited `internal/config`, `deploy/.env.example`, and two `cmd/api` test
files whose `config.Config` literals build every domain's handler during attach
(`routes_test.go`'s `testDeps`, `routes_app_test.go`'s `routerWithApp`). None of those is on the
forbidden list and none was a conflict, but a wave-7 prep ticket that pre-seeds a configuration
section the way `Deps` was pre-seeded would remove the class.

### SHIP-108 — the verifier, and the check that is the auth class rather than a step inside it

The other half of the pair §8 has always kept with one owner. SHIP-107 signs the link; this spends
it, and adds the one route in the service reached with something other than a mobile session:
`GET /v1/driver/jobs/{id}`, declared `RequireDriverToken`.

**The interesting decision is where the one-job check lives, and it is not where it first looks like
it belongs.** The token names a job, the request names a job in its path, and something has to
compare them. Putting that comparison in the handler is the obvious shape and it is wrong: it makes
the check a thing a handler can *forget*, and a handler that forgot it would answer somebody else's
delivery with a `200` — no compile error, no failing test of its own, and no symptom until a driver
held two links. So the comparison is in the guard, which `attach` installs from the manifest's auth
class. **A route cannot declare `RequireDriverToken` and skip the check, because the check is the
class.** The handler never reads the path at all: the job comes out of the grant, which the guard has
already reconciled with the path.

That leaves one shape the guard cannot check at startup — a route declaring the class on a pattern
with no `{id}`, since a guard is handed a handler rather than a pattern. It fails **closed**:
`PathValue` answers `""`, which is not an identifier, so every request to such a route is refused.
A route that visibly never works, rather than one served with no scope at all, and there is a test
holding that direction because the tempting one-line alternative — "no job in the path, so nothing to
compare, let it through" — passes every other test in the file.

**Both directions of the exchange invariant are now demonstrated over HTTP, which is what this lane
existed to finish.** SHIP-107 could only show one: a driver token presented to `GET /v1/jobs`
answering `401`. The other needed a route that accepts a driver token, and there was none.
`scripts/verify/70-delivery.sh` now presents a *mobile* token to the driver route and gets `401`, two
checks apart from its mirror image, against one running binary in one run.
`TestNeitherTokenSystemOpensTheOthersRoutes` is the same pair in Go through the real router. The
audience is doing that work alone in the domain's tests, where the mobile token is signed with the
**driver keyset's own key** — the arrangement `internal/config` refuses in a deployment, tested
precisely because the guarantee should not depend on it.

**The grant type is exported and its context key and accessor are not.** `DriverGrant` has no
`UserID`, no role and no session id, so the conversion `Docs/10` §5 forbids has nothing to draw on —
SHIP-107 made that structurally true and this ticket had to not put the material back. Keeping
`driverGrantFrom` unexported goes one step further: **nothing outside `internal/delivery` can read a
driver grant at all**, so "delivery is the only domain that serves a driver-token route" is a fact
about the build rather than a rule somebody follows.

**The middleware went into `internal/delivery` rather than staying in `cmd/api`, which is the one
thing SHIP-15m deliberately left open.** Everything it does is that domain's — its error codes, its
path parameter, its grant — and handlers live in the domain, a guard being the front half of one.
What is left in `cmd/api/driverauth.go` is three statements, the same shape as
`newAccessTokenAuthenticator`.

**A fifth error code, and it is the only one this domain puts on an authentication failure.**
`delivery_driver_link_expired`, deliberately **not** `httpx.CodeTokenExpired`, whose whole meaning is
"refresh and retry, do not sign the user out" — advice a driver portal cannot take, because there is
no refresh behind a link and no account to sign back into. A portal branching on `token_expired`
would loop. Everything else is undifferentiated: a bad signature, an unknown `kid`, `alg: none` and a
mobile token all answer `unauthenticated`, which is the call `httpx.ResolveSubject` already makes,
and the wrong job answers `404` because a `403` would confirm to a link-holder that a competitor's
job exists.

**The route is deliberately the smallest one that makes the *Done when* demonstrable.** It answers
which job the link opens, which assignment it belongs to, the driver's name and when the link
expires — five fields, held to a closed set by a test and by a `make verify` check. **The delivery
detail is SHIP-120's**, and it needs a port into `jobs` that a middleware ticket has no business
declaring; fields are added to this shape rather than moved elsewhere, which is additive
(`Docs/07` §6). The driver's **mobile is absent on purpose**, unlike the provider's view of the same
row: the driver knows their own number, and a link travels through whatever channel the provider
already uses, so whoever ends up holding it should learn as little as possible.

**The `{id}` looks redundant against the token and is the mechanism.** The path is the client's
statement of what it means to act on and the token is the platform's statement of what the caller
may act on; comparing them is what makes "exactly one job" *observable from outside* rather than
merely true inside. Drop it and a grant that had silently widened would be undetectable, because
there would be no other job to ask for. It is also what SHIP-121 needs anyway, and what lets a driver
carrying two deliveries hold two links a browser can tell apart.

**`Service.AssignmentFor` takes a `DriverGrant` rather than a job id**, so there is no way to ask it
about an arbitrary job — a grant comes from `Verify` and from nothing else. It is also where the
question a claim cannot answer gets asked: a token is stateless and keeps verifying after the
assignment behind it has ended, so the row is what says the grant has lapsed. That is the lookup
`Service.Driver`'s comment reserved for this ticket and the mechanism **SHIP-109 will reissue
against** — revocation as a read rather than a denylist. Nothing writes `unassigned_at` through an
endpoint yet, so a test writes it directly; the answer is `404`, not `401`, because the credential is
fine and telling a stood-down driver their link is invalid sends them asking for the wrong thing.

**Demonstrated by mutation rather than asserted, four times, each read for its message rather than
its verdict.** Removing the one-job comparison fails three tests, and the bodies in the failure
output show one driver being handed the *other* driver's assignment — which is the defect made
visible rather than described. Making the missing-`{id}` case permissive fails the test named for it.
Removing the expiry distinction turns the code back into `unauthenticated`. Resolving the driver into
an `authctx.Subject` fails `TestADriverRouteProducesNoSubject` — **and `cmd/api` passes**, because the
seam's own probe route is guarded by a stub rather than by the real middleware. That last one is the
argument for the domain-side test existing at all: the seam test holds the *wiring*, and only a test
against the real guard holds the invariant.

**SHIP-15m's seam held for every non-test file, and did not hold for the test callers.**
`cmd/api/routes.go`, `manifest.go`, `main.go` and `Deps` are untouched, exactly as promised — filling
in `newDriverTokenGuard` is what maps the class. What the seam did not carry is that `newRouter`
takes the guard as an *argument*, so every test that builds a router passes one: **seventeen call
sites across five files** passed `nil` while nothing declared the class, and all seventeen had to
become a real guard the moment a route did — including eleven in `routes_identity_test.go`, another
domain's test file. The edit is mechanical and none of those files is shared in `Docs/10` §9.2's
sense, so it cost nothing this time; it is recorded because **`RequireAdmin` will meet it again at
SHIP-147**, and a prep ticket that gives `newRouter` a test constructor taking the guards it needs
would remove the class. The two seam tests passed unchanged throughout, which is what
`TestTheClassIsServedExactlyWhenTheConstructorSuppliesAGuard` was written for: it asserts the rule
rather than today's answer to it, so it kept passing on the day the `nil` stopped being `nil`.

**`internal/config` was not touched.** SHIP-107 added the driver-token section and it carried
everything this ticket needed, which is the pre-seeding argument working one wave after the finding.

**The idempotency scope does not bite this route, and it will bite the next one.** `Docs/11` §9
records that a driver-token request scopes its key to `anonymous` — `SubjectScope` keys on the
subject, a driver token deliberately produces none, and the scope is computed group-wide *outside*
`Idempotent` while a guard runs per route inside it. `GET /v1/driver/jobs/{id}` is a read, so it
never reaches the middleware's store. **SHIP-121's milestone controls are state-changing and will**,
and §9's entry is still the place that decision gets made.

**No migration, and the delivery block is still at `000602`.** Verification is stateless and the row
it checks against already exists.

`make verify` went from 366 checks across 12 sections to the figure at the top of this section, all
ten of the new ones in `scripts/verify/70-delivery.sh`.

### SHIP-112 — absorption, and what "backwards" had to be decided to mean

A queued `en_route_to_pickup` that syncs after the job is already `In transit` is now **kept and
moves nothing**. SHIP-111 refused it with `delivery_milestone_not_permitted` and rolled the row back
with its transaction; `Docs/02` §3.1 had always said it must be "absorbed, not rejected as an
error… the platform accepts the historical fact without moving the job backwards", and this is that
sentence built.

**SHIP-111's seam held exactly as it promised.** The milestone row is inserted before the move is
attempted and is already independent of whether the job moved, so absorbing costs nothing but
declining to return an error. What SHIP-111 could not predict is that the change is *not* confined to
`Service.RecordMilestone` — one `case` of that switch is where absorption happens, and deciding
**which** refusals get it needed a fifth value on the port and a second question asked in `cmd/api`.

**"Backwards" is: the job has already recorded a transition into the status this milestone names.**
That is a fact in `job_status_history`, not an inference over `Docs/02` §2's table, and it is the
document's own phrasing — "a queued update that arrives after a later transition has already been
recorded". Two alternatives were considered and rejected:

| Considered | Rejected because |
|---|---|
| Absorb **every** refusal of the transition guard | It would answer `201` to a milestone the delivery has not reached — an `in_transit` while the job is still on its way to the pickup — and the job would then never move to `In transit` at all. The client is told "recorded" for a move that silently never happens, which is the same loss this ticket exists to prevent, wearing a success code |
| Absorb whatever the job can no longer **reach** by any permitted sequence | A job cancelled or disputed at `Awarded` can no longer reach `Picked up` either, and it has not moved *past* the pickup — it lost the delivery to something else. That is `Docs/02` §3.1's administrative-conflict bullet and **SHIP-113's ticket**, and folding it in here would have finished half of that ticket by accident and in the wrong place |

So the split is **late** against **premature**, and the asymmetry is not fastidiousness: a late
milestone can never succeed on a retry, because `Docs/02` §2 has no way back, so refusing it discards
a driver's record permanently. A premature one succeeds unchanged as soon as the delivery gets there,
so refusing it costs a retry. `TestAMilestoneTheDeliveryHasNotReachedIsRefused` records the second
half by recording the refused milestone, moving the job on, and replaying the identical recording
successfully.

**An absorbed milestone writes one row and nothing else.** No `job_status_history` row — every row in
that table describes a status change, and one written for a move that did not happen would be read by
SHIP-77's timeline and by support as though it had. No status update, and **no domain event**, because
`jobs` emits `job.status_changed` from inside the transition and there was no transition. The one
trace it leaves besides the row is a log line, which is deliberate: absorption is otherwise entirely
invisible, and `Docs/02` §3.1's escalation ladder is about exactly how long updates have been unsynced.

**The distinction is made in `cmd/api`, because it is a question about a `jobs.Status`.**
`jobs.ErrTransitionNotPermitted` says only that the table has no such edge — it has no notion of
forwards, and should not. `jobLifecycle.refusal` asks `jobs.Service.History`, inside the caller's
transaction and after `Transition` has taken the job row `FOR UPDATE`, and answers the domain in the
domain's own vocabulary: a fifth `delivery.JobMove`, `JobAlreadyPast`. `internal/delivery` still names
no status and still holds no second copy of the transition table. `internal/jobs` was not touched.

**`AssignDriver` sees the new outcome too, and refuses it.** A job past `Driver assigned` is still
`delivery_job_not_assignable` — a milestone is a claim about something that already happened, so
keeping it costs nothing, while an assignment is an instruction about who drives the job *now*, and
there is nothing historical to keep. The case is written out rather than left to fall through, because
an unnamed outcome becomes a `500` and that is the right answer for something nobody has considered
and the wrong one for this.

**The response shape did not change, and that was a decision.** No `absorbed` field, no third status
code. The milestone response has never told a client what status the job is in — `Docs/02` §3.1 puts
that reconciliation on the job resource — and a flag here would have to be answered from a *stored*
fact on a retry and a *computed* one on the first attempt, which is two answers to one question. What
changed for a client is that the `409` has gone, which is the whole point: the driver's work is now on
the platform instead of pending on a phone forever. `delivery.Outcome` carries the distinction as far
as the handler and no further.

**SHIP-111's boundary test was inverted rather than deleted**, keeping what it was protecting.
`TestAMilestoneTheJobHasMovedPastIsRefusedForNow` became
`TestAMilestoneTheJobHasMovedPastIsAbsorbedAsHistory`, and its milestone count — which existed to
prove the transaction left nothing behind — is now the assertion that the row survives. Three further
assertions carry the second clause: the status is untouched, the transition count into that status is
still the one the job really made, and the job's latest recorded transition is unchanged.

**`Delivered` cannot be reached through absorption**, and the check that stops it is a matter of
ordering rather than a new rule. `ErrProofRequired` is tested in front of the insert, so a `delivered`
never reaches the switch that absorbs and never reaches the table.
`TestAbsorptionCannotReachDelivered` drives it from a job that has already *been* `Delivered` — the
one state where "the job has moved past it" is true and absorption would otherwise apply.

**It does not make the `anonymous` idempotency scope live, and §9 has the wrong ticket.** §9 says
"decide with SHIP-112, which is the first ticket whose retries are a driver's rather than a
provider's". It is not: `POST /v1/jobs/{id}/milestones` declares `RequireUser` and is the awarded
provider's, SHIP-108's driver route is a read, and **no driver-token route in the service changes
state**. The first one that does is **SHIP-121**, the driver portal's milestone controls, and that is
where §9's decision belongs. Nothing about the scope changed here and nothing needed to.

**No migration** — the delivery block is still at `000602`. Absorption stores no new fact: the row,
its two clocks and its idempotency key were all already there, which is what made a five-point ticket
a change to one switch, one adapter and their two test copies.

`make verify` went from 376 checks across 12 sections to the figure at the top of this section, all
of the new ones in `scripts/verify/70-delivery.sh` — five in a `SHIP-112` section of its own, which
is where the production adapter's half is exercised at all. `internal/delivery`'s tests drive a
*copy* of `jobLifecycle`, because `cmd/api` has no database in a Go test, and a copy that drifted
would show as a late milestone absorbed by one and refused by the other.
### SHIP-163 — M6's first code, and the readings `Docs/04` §7 forced

`POST /v1/jobs/{id}/disputes`. A customer or the awarded provider reports that a delivery went
wrong; the dispute is recorded and **the job freezes in the same transaction**. It opens
`internal/admin`, which held `doc.go` and nothing else, and it is the first migration in the
`000800`–`000899` block.

**The endpoint is a user endpoint, and that is the first thing to know about it.** The domain's name
invites the opposite assumption and `Docs/04` §7 settles it: intake is a *party to the delivery*
reporting a problem, and only the investigation and outcome stages are administrative. The route
declares `RequireUser`. `RequireAdmin` is still declarable with nothing behind it — a route
declaring it panics at startup — and SHIP-164 arrives with SHIP-147's middleware.

#### Raising a dispute freezes the job, and `Docs/02` had already decided that

`Docs/02` §2 has one row for it — "Awarded through Delivered → Disputed, eligible user/admin opens a
supported dispute" — and §3 says what it is for: "a dispute freezes automatic completion until an
administrator resolves it". SHIP-164's *Done when* speaks of an outcome that "unfreezes the job",
which is the same sentence read from the other end.

So intake is an insert **and** a guarded transition, committed together. Nothing here chooses the
target status: `admin` declares `MoveToDisputed` on its own port and `cmd/api` runs
`jobs.Service.Transition`, so the transition table stays in one place. The `job_status_history` row
carries the dispute's **category as its reason**, which is what makes a job's history say why it
froze rather than only that it did. No status was invented — the twelfth status already existed and
had no way of being reached.

#### `Docs/04` §7 names seven fields; two of them needed a decision, and here they are

> "Capture job, complainant, category, description, desired outcome, time of event, and evidence."

Five are unambiguous. **Two are recorded here rather than resolved silently**, which is what
`CLAUDE.md` asks of a documented ambiguity.

**Category — §7 names the field and enumerates no values.** The six in `ck_disputes_category` are
derived from `Docs/02` §5's exception table, the only list in the documents of what actually goes
wrong on a delivery, plus `Other`. Two of §5's seven rows are excluded as operational events rather
than complaints (a lost portal link, unsynced milestones), and one string is shortened: §5 writes
"Customer unavailable at pickup/delivery", and the slash has no legal form under `Docs/10` §4.7's
derived lower-snake-case wire mapping, which is a constraint on three generated client languages
rather than a preference. **`Other` is deliberate** — a closed list with no escape hatch turns every
unanticipated complaint into a mis-filed one, and the filing is what an administrator triages from.

**Evidence — §7 names it and there is nowhere for it to live.** Verification evidence uploads to
private object storage through short-lived pre-signed URLs (`Docs/04` §3.1) and delivery proof does
the same from SHIP-114; neither exists, so no complainant can produce an object reference and no
endpoint would hand them one. Capturing nothing would drop a field the *Done when* names; capturing
an upload path would be building SHIP-114 inside SHIP-163. **What is captured is references in the
complainant's own words** — "photographed the crates at the depot" — as `text[]` the platform stores
and does not resolve. When uploads land, an attachment is a row in a table of its own pointing at
this one, and nothing about the column changes.

**`occurred_at` is required rather than defaulted, and that is the third decision.** §7 names "time
of event" as an intake field distinct from the report, and the platform records the filing time
itself in `created_at`. A default would write the report's time into the incident's column on every
request that omitted it, and support could not afterwards tell that value from one somebody meant —
the same collapse `Docs/02` §3.1 refuses for milestones. It is unbounded backwards (a complaint about
last month is still a complaint) and refused forwards, which is the one bound.

#### It writes no audit row, and that is a scope decision rather than an oversight

§4 records that SHIP-149's Go write helper does not exist. **Intake does not need it and does not
write one.** `audit_log` is for *privileged* actions — `Docs/04` §6 and §9, and SHIP-150's *Done
when* is "all admin mutations write an audit entry". A customer reporting damaged goods is an
ordinary product action that already leaves two durable records: the `disputes` row, which carries
actor, time and reason by construction, and the `job_status_history` row the guard wrote.

Writing the helper here would have been building SHIP-149's missing half on a branch that owns
neither it nor SHIP-150. **SHIP-164 is the privileged mutation** — it resolves the dispute and
unfreezes the job — and it depends on SHIP-150, which is where the helper belongs.

#### A stranger is refused explicitly, which is the wave-5 finding acted on

Wave 5 found six of eight `fleet` endpoints scoping to the caller's own id rather than refusing an
outsider: nothing leaks, and somebody with no business asking is told their request was fine. This
endpoint asks *who the caller is on the job* through an `admin.JobParties` port — one statement in
`cmd/api` joining `jobs` to the accepted bid — and refuses when the answer is nobody. Four callers
are tested and all four are refused: a provider who bid and lost, a customer of another job, an
account with no connection at all, and a job that does not exist. All four get the same `404`, so
none of them confirms anything about the others.

#### Two partial unique indexes, and they are two different promises

|  | What it refuses | What it is for |
|---|---|---|
| `uq_disputes_idempotency` | a second row for `(job_id, idempotency_key)` | a **retry** gets the dispute it already raised, permanently — after Redis has forgotten the response |
| `uq_disputes_open_per_job` | a second row for `job_id` while `resolved_at IS NULL` | a **second raise**, including by the other party, is refused: a job is frozen once, and SHIP-164 unfreezes it by resolving one dispute |

The two are told apart by which index the insert conflicts on. `ON CONFLICT` names the idempotency
index as its arbiter, so PostgreSQL checks that one first and abandons the insert without reaching
the other — a retry is absorbed, and a fresh key against a frozen job reaches
`uq_disputes_open_per_job` and gets `admin_dispute_already_open`. `make verify` demonstrates both by
deleting the Redis entry, exactly as SHIP-111's section does.

**There is no dispute status column.** `Docs/04` §7 names three stages and SHIP-164 owns the workflow
that moves through them; a vocabulary invented at intake for a workflow that does not exist is one
that ticket would have to work around. `resolved_at` is the one distinction intake genuinely makes,
and it is what the open-per-job predicate needs.

#### Two smaller things worth finding later

**A `CHECK` constraint may not contain a subquery**, which is how the per-item evidence bound was
first written and why the migration failed on its first application. What replaced it bounds the
list, refuses a NULL or empty entry, and bounds the total with array operators; Go bounds each item
at 500 characters, where it can name which one was wrong.

**The `make verify` fixture phone numbers are a shared namespace.** The database is not reset between
runs, so `04180` — the outbox section's — collided with this section's first choice and failed at its
very first registration with `identity_phone_taken`, which is a confusing way to be told that two
files disagree about a number. `0419x` is admin's, and the prefixes are now written down in the
section header.

**No new domain event.** The transition already emits `job.status_changed` with `to: Disputed`, which
is what a consumer needs. A `dispute.raised` event would need a fourth aggregate in
`internal/events` and a fourth topic in `cmd/topics`, both shared surfaces, and it belongs to
SHIP-136 rather than here.

#### How it is demonstrated

`scripts/verify/90-admin.sh` grew from 3 checks to 25, across three `ticket` sections, against the
built binary — which is the only place the composition root's half of the ticket runs at all, since
`internal/admin`'s own tests supply their own copies of the two ports. The intake fields are asserted
off the row rather than off the response, the response's key set is held closed so that SHIP-162's
internal notes cannot reach a complainant by accident, and the retry is exercised through both
mechanisms in turn.
### SHIP-124 — the queue, and what a client does with an operation it can never send

`core/queue` stops being a folder with a note in it. `Docs/07` §4 calls the durable queue the single
most important client capability, and its *Done when* has two halves that are not the same size:
"queued operations survive app restart" is a storage question, and "are never silently dropped" is
an invariant over everything the class can do.

**The first half is one paragraph.** Drift over SQLite, which `Docs/10` §8.3 and `Docs/07` §9 closed
long before this ticket, and the argument they closed it on is the one that held up: the requirement
is transactional rather than about storage. An operation, its idempotency key, the time the user
acted and the path to its proof image either all commit or none of them do. `enqueue` returns only
after its transaction commits, which is what makes it safe for a screen to confirm at that point —
`Docs/07` §4's "the UI confirms immediately, marked clearly as pending". Every test runs against a
real file and the restart is a genuine close-and-reopen over the same bytes; an in-memory database
would have made all of them pass while proving nothing about the sentence being demonstrated.

#### The six ways an operation could vanish, and what stops each

This is the ticket. A test showing the happy path surviving a relaunch does not demonstrate "never
silently dropped" at all, so the failure modes were enumerated first and
`queued_operations_are_never_silently_dropped_test.dart` is one group per mode.

| Way it could vanish | What stops it |
|---|---|
| A crash between enqueue and commit | One transaction. Nothing partial can exist, and the confirmation is lost with the operation rather than surviving it |
| A crash mid-drain | The claimed row stays in the table `in_flight`; `recover()` returns it at the next launch, under **the key it was created with**. There is no lease and no timeout — a handset runs one app process, so the next launch is the only other party there is |
| A row this build cannot read | **Quarantined, not skipped.** Skipping is the drop; throwing stalls everything behind it. It becomes `blocked` with a recorded reason, still counted and still listed |
| A queue that grows without bound | A cap that **refuses the new operation rather than evicting the oldest**. Eviction is precisely a silent drop, and it drops the operation that has waited longest |
| Two writers racing | Every state change is a conditional `UPDATE`/`DELETE` whose affected-row count is checked, and the capacity check runs inside the insert's own transaction |
| A removal nobody asked for | Exactly three paths remove a row — `complete`, `acknowledge`, `clear` — and a test drives every other public method against a populated queue and asserts the count does not move |

All three of the mechanisms worth doubting were confirmed by mutation rather than believed: making
`snapshot` skip an unreadable row fails four tests, ignoring the claim's affected-row count fails
two, and dropping the per-key head rule fails three.

#### The poison item: SHIP-135's move, taken as far as it goes, and then not

§9 recorded the same question answered on the platform side, and SHIP-135's answer was better than a
dead-letter path: **a permanently unpublishable outbox row is now unwritable**, because all three of
its failure classes are decided inside the transaction that makes the state change. The client takes
that move and then has to stop, and the reason it has to stop is worth writing down because it is
structural rather than a shortfall.

**Taken.** An operation of a kind that does not exist cannot be written at all — `OperationKind`'s
constructor is private, so the set is closed by the compiler and `enqueue` needs no check for it. A
body over the bound is refused by `enqueue`. A duplicate idempotency key is refused by `enqueue` and
by a `UNIQUE` constraint behind it, which is the same shape as `uq_bids_one_accepted_per_job`.

**Not available.** The platform has one deployment; a handset has whatever build was installed last
week. A row written by *another build of this app* — a kind since removed, a body shape since
changed — is a case no enqueue-time check can reach, because the check that would have caught it was
not in the build that wrote the row. Nor can the client know in advance that the platform will refuse
an operation: that answer arrives hours later, being offline being the entire point.

**So for those two: quarantine and escalate, never delete.** The row moves to `blocked` with the
reason recorded, and what stops it being quiet is not this package but `Docs/02` §3.1's ladder that
`Docs/09` already schedules — the indicator immediately (SHIP-126), the provider nudge at four hours
(SHIP-127), the operations alert at 24 (SHIP-128). Nothing removes it after N attempts; a person
acknowledging it does (SHIP-132). **A dead-letter path was rejected for the same reason SHIP-135
rejected one**: it is a place things go to stop being anybody's problem, and the queue's whole promise
is the opposite.

**The one that would be a silent drop if it were not deliberate: an unrecognised `state`.** Reading
treats *anything* that is not `pending` or `in_flight` as blocked, rather than matching a known list.
The alternative reads as harmless and is not — a state written by a later build would be a row present
in the table and absent from every list, which is a silent drop wearing a database row as a disguise.
Blocked is the default bucket, and `total` is counted in SQL so it depends on nothing decoding.

#### Ordering: FIFO within an ordering key, and nothing between keys

A decision, and it was taken on this queue's own terms rather than on SHIP-112's — that ticket was
being built in the same wave and nothing here depends on it.

**Not global FIFO**: one job's stuck operation would hold up every other job's, which turns one
problem into a stalled queue and is how a driver discovers at six in the evening that the morning
never left the handset. **Not unordered**: `Docs/02` §3.1 has the platform absorb a milestone that
arrives after a later one, and SHIP-112 builds that — but it is a safety net, and sending a driver's
recorded sequence in an arbitrary order would make every reconnection depend on it. **Per key, then**,
conventionally `job:<id>`: one job's sequence arrives in the order it was recorded, and one job's
problem stalls nothing else.

A blocked operation **does** hold its own key, and that is the deliberate half. Releasing what is
behind it past it would silently reorder exactly the sequence the key exists to preserve. It is
head-of-line blocking confined to one job, and it is visible — the pending indicator and the
escalation ladder are counting it, which is what separates "held" from "dropped".

#### Two smaller decisions worth finding later

**The capacity and the body bound are values the queue is given, not constants it holds.**
`CLAUDE.md` keeps anything that changes under operational pressure server-side, and a queue cannot
wait for the platform to tell it how large it may be — being unreachable is the situation it exists
for. So a compiled-in default is unavoidable, and what is available is that it is a default rather
than a constant: `QueuePolicy` is a constructor argument, and a server-supplied bound needs no change
in `core/queue`. Neither number is tuned; their job is to make unbounded growth and an unsendable
body impossible, which is the reasoning SHIP-135 records for the outbox's 16 KiB bound.

**Sign-out does not clear the queue yet, and that is recorded rather than missed.** `Docs/07` §3
requires it and `OperationQueue.clear` exists, returning how many operations it discarded so that
even the one bulk removal is something a user can be told about. What is missing is a queue anything
writes to: **SHIP-125** opens the database at start-up and calls `recover()`, and **SHIP-129** is the
first screen that puts anything in it. Wiring `SessionController.signOut` today would have every
widget test open a platform directory in order to clear a store that is always empty.
`session_controller.dart` carries the same note beside the method.

#### The seam left for SHIP-125, deliberately unbuilt

No sync worker, no backoff, no drain, and no connectivity listener. What is there for it:
`recover()` for start-up and resume; `claim()` which takes the next sendable operation and counts the
attempt; `release(id, nextAttemptAt:)` which stores the delay **durably**, because a backoff reset by
every app launch is no backoff on a handset; `complete(id)` for an accepted one; and
`block(id, reason: refused)` for one the platform would not take, which is the state SHIP-132 renders
from. `QueueSnapshot.unsynced` counts pending and in-flight work and deliberately excludes blocked
work — a number that never falls however long the driver stands in the open is not the number
`Docs/02` §3.1's indicator is asking for. `OperationKind` has two members, `delivery.milestone` and
`delivery.proof`, both named by `Docs/01` §4.4 and `Docs/07` §4 rather than invented here; the proof
is separate because §4 uploads it as a local file and not as part of the milestone request.

#### How it was demonstrated

`make flutter-check` green: **534 host tests** (up from 495), the analyzer clean, and the environment
test per build flavour. `make verify` does not cover this ticket and its count does not move — that
script exercises HTTP endpoints and this one adds none, which is the same position SHIP-80, SHIP-105
and SHIP-110 are in.

**One thing this ticket cannot verify from a Linux CI runner or a macOS host, and it should be
watched on the first device build.** `sqlite3` 3.x supplies its native library through a build hook
rather than through `sqlite3_flutter_libs`, which now resolves to an empty `0.6.0+eol` marker
version. Host tests exercise the hook and pass; `make flutter-build` was not run, because iOS and
Android builds are not part of `flutter-check` and SHIP-24…27 are where they get a runner. Whoever
first builds for a device confirms the library arrives in the bundle.

### SHIP-125 — the worker, and the trigger that has to exist because reconnection does not fire

`core/sync`. SHIP-124 built a queue that never loses what the driver recorded; this is the half that
gets it off the handset. The *Done when* has three clauses and each was taken as a separate
decision, because two of them have an obvious reading that is wrong.

#### "Drains on reconnection" — six triggers, and the honest one is the worker's own

The obvious implementation is a connectivity plugin and a listener. It is wrong in **both**
directions on a phone, which is why there is no new dependency in `pubspec.yaml`:

- **A connectivity event is not sufficient.** "Joined a network" is true on a hotel captive portal,
  on an access point whose uplink is down, and on a bar of GPRS that times out every request. The
  only honest test of "can this device reach the platform" is a request to the platform — which is
  the thing the worker was about to make anyway.
- **A connectivity event is not necessary, and this is the half that strands work.** A route comes
  back with no event at all: a mast recovers, a carrier repairs transit, a captive portal is signed
  into, a VPN reconnects. The operating system reports the same network throughout. **A worker that
  drains only on a connectivity event waits forever for one that never comes**, with a driver's
  afternoon in the queue.

| Trigger | Covers | Source |
|---|---|---|
| `launch` | A relaunch after a crash or a flat battery. Runs `recover()` first | `main` |
| `session` | A credential appeared; nothing could be sent before one | the session listener |
| `recorded` | The driver just recorded something, usually while they still have signal | `SyncWorker.record` |
| `resumed` | The phone came out of a pocket — where an outage most often ended unobserved | `AppLifecycleListener` |
| `scheduled` | **The worker's own wake-up, at the earliest moment any operation may be tried** | itself |
| `connectivity` | A hint from outside | nothing supplies one today |

The fifth is the one that makes the rest safe to get wrong, and it is **a schedule rather than a
poll**, which is the difference between correct and expensive. After every pass the worker computes
the earliest stored `next_attempt_at` among the operations at the head of their ordering key and
arms exactly one wake-up for that instant. An empty queue arms nothing; a queue whose only work is
blocked arms nothing, because a blocked operation is waiting for a person and not a connection; an
operation in a five-minute backoff wakes the phone once in five minutes. Two tests hold the "arms
nothing" cases, because a worker that polled would pass every other test in the file.

`SyncSignals` is the seam a connectivity package plugs into later. Adding one buys **latency** —
draining a second after the radio returns rather than up to a backoff later — and buys nothing for
correctness, which is the right basis on which to weigh a native dependency against the store
privacy declarations §9 already weighs them on.

#### "With exponential backoff" — and the ceiling is the decision, because nothing resets a wait

Base five seconds, ceiling five minutes, **equal jitter**, stored durably through SHIP-124's
`release(id, nextAttemptAt:)`.

The ceiling matters more than it looks, and the reason is a property of the seam rather than a
preference. **SHIP-124's queue can set a wait and cannot clear one** — `release` writes
`next_attempt_at` and no method takes it back — and that is deliberate, because a worker able to
pull an operation forward is a worker able to reorder the sequence its ordering key exists to
preserve. So nothing shortens a stored wait: not a resume, not a connectivity event, not a relaunch.
**The ceiling therefore *is* the worst case between signal returning and the work leaving the
phone**, and five minutes is what a driver watching an indicator that says their work is not
recorded can be asked to accept. An hour would have been cheaper and would have meant standing under
a clear sky for fifty-five minutes with a full bar of signal. What does reset the schedule is the
operation leaving the queue, and a fresh operation starting at zero — which has its own test,
because a queue where one long-failing operation slowed down a new one would be the natural bug.

**Jitter is equal jitter, not full jitter.** Without any, every queued operation on every handset in
a region retries in the same instant after a mast comes back — the thundering herd the ceiling does
not save you from, since the ceiling decides *how often* and the jitter decides *whether they arrive
together*. Full jitter (`0` to the whole delay) spreads better and is the wrong trade here: its short
draws retry pointlessly early against a link that has not changed. Half the nominal delay is never
skipped and the other half is spread.

Doubling is done in a loop that stops at the ceiling rather than computing `base × 2ⁿ` and clamping,
because a phone left in a depot over a long weekend reaches attempt counts where the shift overflows
and a wrapped delay clamps to whatever sign it landed on. A test passes `1 << 40`.

#### "And per-item idempotency keys" — proved twice, because one proof was not the claim

Nothing in `core/sync` mints a key. It was minted where the user acted, stored in the row by
SHIP-124, and put on the wire by `OperationSender` unchanged on every attempt — including the
attempt after a relaunch, and including the replay `AuthInterceptor` performs after refreshing a
token, which reuses the original `RequestOptions` and therefore the original header.

Two tests, and the second exists because the first is not the whole claim. `sync_worker_test.dart`
shows the **row's** key unchanged across three attempts, which a worker that re-minted at send time
would still pass. `operation_sender_test.dart` reads the outgoing `Idempotency-Key` **header** across
three sends. Mutating the sender to call `newIdempotencyKey()` fails the second and nothing else.

`ApiClient` grew one method for this — `send`, which answers with the status code rather than a
decoded body. Reusing `postJson` would have raised `ApiMalformedResponse` for a `2xx` carrying no
JSON object, which for a queued operation is **a success reported as a failure and then retried
forever** against an endpoint that had already recorded it. `postNoContent` exists for the same trap
at one call site; this is the general form.

#### Retry or refuse: the classification, and the three answers that are wrong in an interesting way

Getting this wrong in either direction is the defect. Retrying a refusal burns battery on a request
that cannot converge; quarantining a transient failure strands a real delivery update until somebody
notices. Neither announces itself.

| What came back | Decision | Why |
|---|---|---|
| `2xx`, including a replay | complete | The platform has it |
| no route, a timeout | retry, **stop the pass** | The case the queue exists for |
| `401` | retry, **stop the pass** | About the **credential**, never about the operation |
| `409 idempotency_request_in_progress` | retry | This operation's own earlier attempt is still running |
| `429 rate_limited` | retry, **stop the pass** | About this **caller**, so every operation is equally refused |
| `503 service_unavailable` | retry, **stop the pass** | A dependency is down — including the idempotency store, which fails closed |
| `408`, other `5xx` | retry | The platform may even have committed; the stored key makes trying again safe |
| a body that was not the contract | retry | A proxy's error page says nothing about whether the service saw it |
| any other `4xx` | **block(refused)** | Understood and refused. A retry can only be told no again |
| anything that is not an `ApiFailure` | **block(unsupported)** | The request never left this build |

**Only a `4xx` is a refusal somebody made.** A `3xx`, or a status the transport could not read, is an
unknown, and the safe direction for an unknown is to try again — a wrong quarantine strands a
delivery until a person looks at it.

*The `401` a token refresh would fix.* It never arrives untried: `AuthInterceptor` (SHIP-50) already
refreshed once and replayed. So a `401` here means no credential could be had at that moment, which
on a handset is most often a refresh that failed on the same dead link the operation did. Treating it
as a refusal would quarantine a driver's update because a token expired in a tunnel. It cannot loop
either: the worker does not drain without a session, and a session that has genuinely ended clears
the queue.

*The `409` that is a success in disguise.* A **replayed** success is not a `409` at all —
`httpx.Idempotent` replays the stored response byte for byte, so the second attempt of an accepted
milestone comes back `201` with `Idempotency-Replayed: true` and is completed like any other success.
What is genuinely a `409` is `idempotency_request_in_progress`: the platform is at that moment
running this operation's own first attempt. Refusing it would quarantine an operation seconds before
it was recorded. `409 conflict` — "valid, but it contradicts the current state" — is the opposite,
and is exactly SHIP-132's case.

*The `422` that will change meaning.* SHIP-111 refuses a late milestone with
`delivery_milestone_not_permitted`, and its own message tells the client to keep the update because
SHIP-112 will have the platform absorb it. Quarantine **is** "keep it": the operation stays in the
table with its key and its attempt count until a person acknowledges it.

**Which failures stop the pass is a second decision inside the first.** A failure that came back from
the platform proves there is a route, so the pass moves on to the next ordering key — stopping would
let one job's problem stall another's, the head-of-line block SHIP-124's per-key ordering exists to
confine. Only failures about the link, the caller, or the platform as a whole end a pass, because
nothing else would fare differently and continuing is one radio wake-up per queued operation in
exchange for nothing. The pass terminates for a stated reason rather than by hope: every outcome
either removes the operation or writes a `next_attempt_at` in the future, and `claim()` will not
offer an operation before that time, so each pass offers each key's head at most once.

There is **no dead-letter path and no attempt limit**, which is SHIP-124's position and SHIP-135's
before it. Nothing removes a quarantined operation after N attempts; a person acknowledging it does
(SHIP-132). What is stored beside it is `ApiFailure.toString` — status, machine-readable code and
request id, and deliberately not the platform's copy or the response body, which `Docs/07` §3 keeps
out of anything readable later. A test asserts both halves of that string.

#### Sign-out clears the queue, and the wiring is the answer to SHIP-124's objection

SHIP-124 recorded this as deferred rather than missed, and named the condition: there was no queue
anything wrote to. **That is not quite what changed.** What changed is that until this commit a
leftover row was inert, and from it a leftover row is one the worker will pick up and send under
whatever credential the device holds *next*. The invariant `Docs/07` §3 is protecting goes live with
the worker, not with the first screen that queues anything — so it had to land here.

It is wired as a listener on `sessionProvider` inside `syncWorkerProvider`, not as a call inside
`SessionController.signOut`, for three reasons in increasing order of importance: `core/auth` does
not learn about `core/queue`; a widget test that signs out still touches nothing, because it does not
build this provider, which answers SHIP-124's objection rather than overruling it; and **it also
covers the sign-out that did not finish** — a process killed between clearing the keychain and
clearing the queue leaves rows behind, and the next launch resolves the session to signed out and the
listener fires then. An inline call would have run in exactly the one case that had already happened.
Both are tested against the real providers.

The same listener **pauses** the worker while there is no session, which is what stops a cold start
spending an attempt — and a stored backoff — on a request carrying no bearer token. A session that
cannot be read is treated as no session by `SessionController`, and therefore clears the queue too:
with no refresh token there is no credential those operations could ever be sent under, and the
alternative is unsendable rows waiting for the next account on the device.

#### Where the app is started from, and why `main.dart` changed

`recover()` has to run before the first frame and nothing else in the application looks at an
`in_flight` row, so the worker is started from a provider read outside the widget tree —
`ProviderContainer` plus `UncontrolledProviderScope`, still exactly one scope with every override on
it. **Deliberately not from `ShipperApp`:** every widget test builds that widget, and a worker there
would have each of them open the platform's application-support directory to construct a queue
database. That is SHIP-124's objection arriving from the other direction, and `main` is the one place
the real application exists and no test does.

#### What is left for the four tickets written against this one

`SyncWorker.snapshots` publishes a `QueueSnapshot` after every pass and costs nothing, because the
worker already reads one to decide when to wake — **SHIP-126** reads `unsynced`, which counts pending
and in-flight work and excludes blocked. **SHIP-127**'s four-hour clock is `enqueuedAt`, already
stored. **SHIP-129** calls `SyncWorker.record`, which is enqueue-and-drain in one call so that "wake
the worker afterwards" is not a rule every screen has to remember. **SHIP-132** renders
`snapshot.blocked` and removes an entry through `SyncWorker.queue.acknowledge`. No widget was built.

**SHIP-130 has one thing waiting for it and it is loud rather than silent.** `Docs/07` §4 uploads a
proof photograph as a local file and not as part of the milestone request, which is a multipart send
this build has no code for. Nothing can enqueue one yet — SHIP-130 comes after SHIP-129 — so
`ApiOperationSender` throws for an operation carrying an `attachmentPath`, and the worker quarantines
it rather than looping against a request it cannot construct. A test holds the case so it cannot
become a silent half-send.

#### How it was demonstrated

`make flutter-check` green in the worktree: **594 host tests** (up from 534), the analyzer clean, and
the environment test per build flavour. `make verify` does not cover this ticket and its count does
not move — that script exercises HTTP endpoints and this one adds none, the same position SHIP-80,
SHIP-105, SHIP-110 and SHIP-124 are in.

**Five mutations, and each fails exactly the tests it should.** Making the backoff ignore the attempt
count fails six; treating a `401` as a refusal fails one; removing the pass-stopping break fails the
two that assert it; dropping the per-key head rule from the wake-up calculation fails the blocked-key
test; and minting a fresh key in the sender fails the header test and nothing else. The queue
underneath is the real one over a real SQLite file, and the relaunch tests are genuine
close-and-reopen cycles — an in-memory database would have made the durable-backoff tests pass while
proving nothing.

**No acceptance test against a running API is in the committed suite**, which follows the precedent
SHIP-48 set and SHIP-124 repeated: `flutter test` runs on a Linux CI runner with no device and no
service, and a test that needs either is a test that gets skipped or commented out. What a person can
run by hand is `make up && make migrate-up && make run` in this worktree and `make flutter-run` with
a queued milestone; that was **not** performed for this ticket, and the first device build is also
where SHIP-124's note about `sqlite3`'s native library still wants confirming.

### SHIP-15p — the wave-7 pre-step, and the first one that asked before the wave

The seventh prep ticket, three points, three parts. Only the first of them had to happen before the
wave rather than during it; the other two are things §9 had been carrying with no owner.

| Surface | Before | After |
|---|---|---|
| Object storage | `deploy/docker-compose.yml` ran `postgres`, `redis` and `kafka` and nothing else, so **SHIP-114's *Done when* could not be demonstrated at all** — struck in §6 for five consecutive waves | A pinned MinIO in the stack, healthy under `make up`, with a private bucket made by the same command and a section in `scripts/verify/00-stack.sh` that mints a real pre-signed URL and uploads through it |
| `internal/config` | Track B's settings would have been a domain branch's fifth parked request against a shared surface | A `Storage` section of nine fields, **asked for at dispatch** and documented in `deploy/.env.example` |
| `routes_app_test.go` | `routerWithApp` replaced `deps.Config` wholesale, so every new configuration section broke it silently until somebody noticed | One field overwritten on a copy, and a test that fails if anybody puts the literal back |

**The store is MinIO, pinned to `RELEASE.2025-09-07T16-13-09Z`, and the choice is the same one
`Docs/06` §4.1 makes about PostgreSQL.** MinIO speaks the S3 API, so the implementation that runs in
staging and production is the one exercised locally, against genuine SigV4 semantics rather than
against something that accepts whatever it is given. A mock that never checks a signature would pass
every test SHIP-114 could write and fail the first time a real bucket saw one.

**The bucket is created by `make up` rather than by an init container in compose, and both halves of
that were measured rather than assumed.** The usual shape — a one-shot `mc` container with
`depends_on: service_healthy` — was written, run, and rejected twice over:

- `make up` is `docker compose up -d --wait`, and **`--wait` fails when any service exits, including
  one that exits `0` having done its job.** The run prints `container shipper-minio-init exited (0)`
  and `up` returns 1. Waiting for the stack is the whole of what `make up` is for.
- **The bucket is per-worktree and the stack is shared.** `COMPOSE_PROJECT_NAME` is pinned so that
  every tree uses one MinIO, and an init container runs when its *container* is created — so the
  first tree to bring the stack up would create its bucket and the next four would find nothing. The
  bucket has to be made on every `make up` in every tree, which is a make step and not a service.

`mc` ships inside the MinIO image, so the step needs no second image and no second pull.

**One finding that will save SHIP-114 an afternoon: SigV4 signs the `host` header.** A pre-signed URL
minted against `minio:9000` inside the compose network is refused with `SignatureDoesNotMatch` when
fetched from the host — an error that names neither the address nor the cause. This was reproduced
before the endpoint default was chosen, which is why `STORAGE_ENDPOINT` is the *published* host port
and why the `Makefile` derives it from `MINIO_PORT` rather than assuming 9000. The same property
covers the region, which SigV4 puts in the credential scope: the container is started with
`MINIO_REGION` set from `STORAGE_REGION` so the two cannot drift into a signature failure.

**The bucket is the one shared service that can be isolated per worktree, and `CLAUDE.md`'s worktree
table now says so beside the row that says Kafka cannot.** That row has read "there is no isolation,
and there is no equivalent to add" since SHIP-15m, and the contrast is the point: a topic is created
from the event catalogue and shared by every tree, while a bucket is a namespace the store makes on
demand. One line in `deploy/.env` per tree and five trees never see each other's objects. **It is
deliberately not derived from the directory the way `TEST_TEMPLATE_DB` is**, which is the weaker
half of the decision and is recorded in §9 rather than argued away: a tree that leaves it alone
shares `shipper-dev`, which is safe for a keyed read or write and unsafe for a count or a listing.

**CI runs the same image from a step rather than from `services:`, and that is a limitation of
GitHub Actions rather than a preference.** A service container takes an image, environment, ports and
`docker create` options and **no command**; `minio/minio`'s default command prints its usage and
exits. Two images could have gone in the block and both are worse: `minio/minio:edge-cicd` carries
`server /data` as its command and is an unreleased build frozen in 2021, and `bitnami/minio`, which
starts a server unprompted and creates a bucket from an environment variable, had its public
repository retired in 2025. Either would mean CI exercising a different object store from the one
`make up` runs — the drift this repository avoids everywhere else, since the workflow and the compose
stack already run the same PostgreSQL and the same Redis. The step starts the pinned release, waits
for `/minio/health/live`, and makes the bucket with the same three `mc` commands the `Makefile` uses.
`go.yml`'s existing comment about why both service containers are present is extended rather than
restated, because the argument is the same one: `internal/testsupport` fails rather than skips, so a
storage test written against a missing service turns CI red instead of quietly green.

**`internal/config` gained a `Storage` section because wave 7 asked, and that is the part worth
noticing.** §9 had concluded — after `GEOCODING_*`, the page sizes and `KAFKA_REPLICATION_FACTOR`
were each absorbed a wave late — that "a prep ticket should ask each track up front what
configuration it will want, rather than absorbing a third round of parked requests after the fact",
and then recorded that SHIP-15m did not ask either. Wave 7 asked at dispatch and Track B answered:
endpoint, bucket, region, access key, secret key, path-style flag, pre-signed URL TTL, maximum upload
bytes, and accepted content types. The answer cost a sentence at dispatch and a section here, against
a wave of a track working around its absence.

**Two of the nine are policy rather than plumbing, and that is why they are server-side.**
`STORAGE_MAX_UPLOAD_BYTES` and `STORAGE_ACCEPTED_CONTENT_TYPES` are limits that move under
operational pressure, and `Docs/06` §5.3 is explicit that anything of that kind belongs in Go rather
than compiled into the client, because Flutter has no over-the-air path for Dart code. The concrete
version: a proof photograph a driver cannot upload is a delivery that cannot be completed
(`Docs/01` §4.4), and a limit in the app is one that needs an app release to raise. The type list is
a list rather than an `image/*` prefix so that `image/svg+xml` — a script container browsers execute
— cannot arrive by being an image.

**The endpoint is always typed, and there is no empty state.** `loader.lookup` treats an empty
variable as absent, so an empty `STORAGE_ENDPOINT` would silently fall back to this field's
development default and put a production deployment on somebody's loopback, reported as nothing at
all. AWS's own regional endpoint is `https://s3.<region>.amazonaws.com` and is perfectly typeable, so
a deployment types it — and `validate` refuses a loopback host outside development, which is the same
shape as the existing `sslmode=disable` rule and exists for the same reason. Four more rules join it:
the bucket name is checked against S3's own (it is a value a person types per worktree, and the first
thing that would otherwise notice a bad one is a driver's upload), each media type must be a
lower-case `type/subtype`, the pre-signed TTL is capped at an hour because **nothing revokes a URL
once it is signed**, and both storage credentials are in `credentialBearingDefaults` so a deployment
that set neither is refused at startup rather than at the first upload.

**`routerWithApp` was the §9 item with no owner, and the fix is four lines.** It substituted a whole
`config.Config` literal, so every domain's requirements had to be repeated in it — SHIP-107's
delivery keyset is in there because a router built without one stops the process, not because a
minimum-version test has any use for it — and `Storage` would have been the next section to collect
on that. It now copies whatever configuration it was given and overwrites the one field under test.
**`TestTheAppFixtureOverwritesNothingButApp` is the guard, and it is deliberately written not to name
a section**: it puts a marker in one the test has no use for and compares the whole struct with `App`
blanked on both sides, so a section added tomorrow is covered without anybody remembering. Restoring
the literal fails it; that was demonstrated rather than assumed.

**What this ticket deliberately did not do.** `internal/platform/storage/` still holds `doc.go` and
nothing else — no `local.go`, no `s3.go`, no adapter, no port. That is SHIP-114's work, and a
reviewer who finds an implementation in this ticket has found a defect. Nothing here is imported by a
domain; the only Go this ticket adds is a configuration section and a test fixture.

**One option is recorded rather than decided, and it belongs to SHIP-114.** `doc.go` specifies two
implementations — `local.go` on the filesystem for development, `s3.go` for staging and production —
and that specification was written when development had no object store. It now has one, which means
the S3 implementation can be exercised locally against genuine pre-signed-URL semantics, and the
filesystem one may no longer earn its place: a second implementation that exists only to be the one
nobody deploys is the speculative seam `Docs/06` §4.1 warns about, and `local.go` would have to sign
its own URLs, which is a second signing scheme to keep correct. **The counter-argument is real and is
why this is not decided here**: `Docs/06` §4.1's adapter test is "does a second implementation exist
today", storage is on that list precisely because of the filesystem one, and a developer with no
container running is a case somebody may still want. `doc.go` is untouched. Whoever writes SHIP-114
decides, and either answer wants a sentence in `Docs/06` §4.1 if it changes the table.

**`make verify` went from 476 checks to 481 across the same 13 sections**, all five in the new
SHIP-15p block: the store is live on the host, a pre-signed URL uploads an object directly, a
pre-signed URL reads the same bytes back, the same object is refused without a signature, and the
object is removed. **Every assertion names the key this run created.** The stack is shared and the
bucket may be too, so the section fences on an id rather than on a count or a timestamp — which is
`CLAUDE.md`'s Kafka rule, applied to the one shared service that can be isolated but is not obliged
to be.

### SHIP-129 — the milestone screen, and the reconciliation signal a client does not have

`Docs/09`'s *Done when* is one sentence — "provider records milestones with optimistic local state
clearly marked pending" — and the interesting half is the second. Recording is a tap and a queue row;
**"clearly marked as pending" is the claim that stops being true silently**, because a screen where a
milestone the platform has and a milestone stuck on a phone render identically still passes every
test that only asserts the milestone is on the list.

The screen is `/jobs/{id}/delivery`: three large buttons, and a log of what this device has recorded
with where each one has got to written beside it in a word, an icon and a sentence.

#### The two seams SHIP-125 named were both taken, and one of them has a hazard in it

`SyncWorker.record` is enqueue-and-drain in one call and needed nothing. `SyncWorker.snapshots` is
where the interesting finding is: **a snapshot is read at one instant and delivered through a
broadcast stream at a later one**, so a snapshot read before this screen's own `enqueue` committed
can arrive after it — and it will not contain that recording, because it did not exist yet.

That matters because of what a client can and cannot know. `OperationSender.send` returns `void` on
success and the worker **deletes** the row, so **the only reconciliation signal this client has is
the operation leaving the queue**. There is no response body to read: not the `201`, not
`accepted_at`, not the milestone id. So "the platform has it" is derived from an absence, and an
absence read from a stale snapshot is a screen telling a driver with no signal that Shipper has their
work.

The controller therefore treats a published snapshot as a **trigger** and re-reads
`SyncWorker.queue` itself, and each read carries a sequence number: a read issued *before* an entry
was created may not conclude that the entry has gone. `_applied` discards a read that comes back out
of order. The cost is one extra `SELECT` over a handful of rows per drain; what it buys is that the
one thing this screen says about the platform cannot be said wrongly.

#### What "Recorded" claims, and the sentence it must not grow into

It claims the platform took the operation. It deliberately does **not** claim the job moved, because
an absorbed milestone is a `201` that moves nothing (SHIP-112) and — by that ticket's own decision —
no field in the response distinguishes the two. So the screen says "Recorded"; "the job is now In
transit" would be a client inventing a status, which is `Docs/02` §2's first rule broken by a caption.

This is also why a late milestone is **not** presented as a failure. The client cannot tell an
absorbed recording from an ordinary one and does not try: both are the row leaving the queue, and
both read "Recorded", which is exactly what happened.

#### Every button stays enabled, and that is the document rather than an oversight

Recording one milestone disables nothing, and three separate rules would each be broken by a screen
that walked the five as a chain. `Docs/02` §2 permits `Awarded → En route to pickup` with no
assignment in between, so a provider driving the job themselves would be blocked by their own app.
SHIP-111 records a **second** `en_route_to_pickup` as a second row when a driver reaches a pickup,
finds nobody and sets off again. And SHIP-112 absorbs an update that arrives after a later one rather
than refusing it, so a client that refused to record it would be discarding a driver's work to
protect a rule the platform does not have. `Docs/07` §3 settles the general case: the device may hide
or disable, and it decides nothing.

#### `Delivered` is named and not offered

`CLAUDE.md`'s invariant is that delivered requires photo proof or a recorded exception and never
neither; `POST /v1/jobs/{id}/milestones` refuses every `delivered` with `delivery_proof_required`
until SHIP-118; and this device can capture neither a photograph (SHIP-130) nor an exception
(SHIP-131). A fourth button would queue an operation whose **only** possible outcome is a
quarantined row — work the driver believes they recorded, waiting for a person. So the milestone
stays in the vocabulary, the screen names it and says what it is waiting for, and
`Milestone.offered` is derived from `needsProof` rather than being a second list that can disagree
with the first. `driver_assigned` is absent from the vocabulary altogether, because it has an
endpoint of its own and the milestone endpoint refuses it with a `422` pointing there.

#### The finding: no endpoint serves an awarded job to the provider delivering it

The screen shows the job's identifier and nothing else about the job, and that is a platform gap
rather than a design choice:

| Endpoint | Serves | To the awarded provider |
|---|---|---|
| `GET /v1/jobs/{id}` | the owning customer's own job | `404`, byte-identically to a job that does not exist |
| `GET /v1/jobs/open/{id}` | a job while it is still biddable | stops answering the moment they win it |
| `GET /v1/driver/jobs/{id}` | the job inside a driver link | a different token system, and it cannot be exchanged |

So the addresses, the goods and the windows are reachable today by the driver the provider assigned
and not by the provider. **Two consequences worth naming rather than absorbing**: the screen cannot
show what is being delivered, and a milestone the platform has accepted is not readable back — the
pending ones survive a relaunch because SHIP-124's queue is durable, and the accepted ones do not,
because nothing on the device stored them and nothing serves them. SHIP-133 is the *customer's*
tracking view and is not this. No ticket in `Docs/09` adds the provider's read.

**The same gap is why the route is deep-link only.** `Docs/07` §5 makes that a first-class way in and
SHIP-145 is the push that uses it, but there is no list to reach it from: the provider half of the
shell shows open work to bid on (SHIP-99), and nothing serves the jobs a provider has been awarded.
`_signedInPatterns` gained `^/jobs/[^/]+/delivery$` — one location rather than everything under
`/jobs/{id}/` — and a test holds it, because forgetting it would not look like a broken button. It
would look like a notification that opens the home shell.

#### It was driven against the real API, on a simulator, and that closes SHIP-100's gap

`integration_test/record_milestone_test.dart`, in the shape `sign_in_test.dart` established: the
production widget tree, the production `dio` client with its auth and idempotency interceptors, the
real Drift queue on the device's filesystem, the real worker started the way `main.dart` starts it,
and real HTTP. A provider signs in, follows a link to `/jobs/{id}/delivery`, taps **En route to
pickup**, and the entry settles on "Recorded".

On an iPhone 17 simulator against this worktree's API on `8092`, `POST /v1/jobs/{id}/milestones`
answered `201`, and the row is what `Docs/02` §3.1 asks for:

```
milestone          | En route to pickup
actor_type         | provider
actor_recorded_at  | 2026-08-13 12:15:28+00
server_recorded_at | 2026-08-13 12:15:28.049833+00
idempotency_key    | f397b7c4caeea3989305daaabafe18f2
```

Two clocks, separately, and the key the **queue** minted at the moment the user acted rather than one
the sender invented. The job moved to `En route to pickup` in the same transaction. The awarded job
had to be built the way `scripts/verify/70-delivery.sh` builds one — a guarded transition and one
`Accepted` row in `bids` — because **SHIP-92's award endpoint does not exist**, which is worth
knowing: no journey through the API alone can currently produce a job this screen can act on.

The test is committed and is deliberately outside `make flutter-check` and `CHECKS`, exactly as
SHIP-48's are: it needs a simulator and a running service, and the Flutter CI job is a Linux runner
with neither. Its header names the invocation and the three `--dart-define`s it needs.

#### Mutation testing, and the one that survived

| Mutation | Result |
|---|---|
| `MilestoneSync.pending` given the recorded state's word and sentence | **Caught**, five tests — including the one that renders a settled and an unsettled milestone on the same screen and asserts they differ |
| `rfc3339(at)` replaced with `at.toIso8601String()` | **Caught**, exactly one test, and it is run 1's finding still biting: the local form carries no offset and `time.Parse(time.RFC3339, …)` refuses it |
| The ordering-key filter removed | **Caught**, one test — another job's queued work appearing on this job's screen |
| **The read-sequence guard removed** | **Survived.** Nothing noticed |

The fourth is the one worth reading. The guard is what stops a stale snapshot concluding that a
recording has reached the platform, and **no test in the suite noticed its removal** — a real queue
over a real file resolves too quickly for the interleaving to happen by chance, so the hazard is
invisible to a test that waits for it.

It was not tuned away and it was not left. `stale_snapshot_test.dart` arranges the interleaving
instead of waiting for it: `GatedQueue` lets a read *complete* and holds its **answer**, which is the
shape of the hazard exactly — a fresh read held late is harmless, a stale one held late is the bug.
With the guard the entry stays "Pending"; with the mutation reapplied the test fails and nothing else
does. **The honest summary is that the mutation survived the suite as written and the suite was
wrong, not the guard.**

Every mutation was reverted immediately and `git diff` confirmed the tree.

#### Two smaller decisions worth finding later

**The screen sends `recorded_at` on every recording, including an online one.** The field is
optional and omitting it means "now" on the platform's clock, which is right for a request made the
instant the user acted and wrong for every other one — and the client cannot tell which it is making,
because whether the drain happens now or in four hours is the worker's business. Sending the actor's
clock always is the only version with one answer.

**A quarantined operation this screen never saw recorded is not listed on it.** A `BlockedOperation`
carries no body — it is the shape a row takes when this build could not read one — so it cannot be
named as a milestone. SHIP-132 is the screen for those and SHIP-126's indicator counts them meanwhile,
which is the arrangement that keeps them from being invisible in the interval.

#### How it was demonstrated

`make flutter-check` green in this worktree: **714 host tests** (up from 686), the analyzer clean, and
the environment test per build flavour. The *Done when* is `record_milestone_test.dart`'s first two
tests — recorded with no signal and marked pending, recorded with signal and marked Recorded — and
the acceptance run above, on a simulator against a live API. `make verify` does not cover this ticket
and its count does not move: that script exercises HTTP endpoints and this one adds none, the same
position SHIP-98, SHIP-99, SHIP-100, SHIP-124 and SHIP-125 are in.

### SHIP-126 — the pending count, and what "persistent" had to be taken to mean

`Docs/02` §3.1's escalation ladder has three rungs and this is the first: *"Immediately — the app
shows a persistent indicator of how many updates are pending. The user is never left guessing whether
their work was recorded."* The 4-hour nudge is SHIP-127 and the 24-hour operations alert is SHIP-128;
neither is here.

#### Persistent means it does not go away when the screen does, so it is mounted above the router

`MaterialApp.router`'s `builder` runs below the theme and **above the navigator**, so a widget placed
there is on every route in the application and survives every navigation. An indicator in an app
bar would have been a smaller change and a different ticket: a driver records three milestones at a
loading dock and walks to the next job, and the question "did that go?" travels with them. Answering
it only on the screen where the work was recorded answers it in the one place it is not being asked.

It is a **bar below the content** rather than a badge over it, so it never covers anything — a
floating chip in the bottom corner would sit on the button that publishes a delivery on the customer
shell. `pending_updates_persist_test.dart` is the *Done when*: record twice with no signal on
`/jobs/{id}/delivery`, navigate to the shell, and the count is still there.

#### Putting it inside `ShipperApp` is exactly the arrangement SHIP-124 objected to, so the dependency is inverted

SHIP-124 refused to wire the queue into `SessionController.signOut` because every widget test builds
that path and would open a Drift database in the platform's application-support directory, which a
host test has no plugin behind. SHIP-125 answered the same objection by starting the worker from
`main` and nowhere else. **This ticket puts a widget that reads the queue inside the one widget every
test builds**, which would have undone both.

So `queueWatchProvider` holds the worker and is **`null` by default**. `main.dart` overrides it;
nothing else does. A test that has not asked for a queue gets an empty stream, a `SizedBox.shrink()`
and no database, and the 714 tests that existed before this ticket were unchanged by it.

The cost of that inversion is a wire that can be quietly missing: an application whose `main` forgot
the override runs with an indicator that never appears however full the queue gets — no test fails,
nothing is logged, and the symptom is a driver left guessing, which is the exact thing the ticket
exists to prevent. So **both halves are held**: `sync_wiring_test.dart` asserts the default is `null`,
and asserts that `lib/main.dart` supplies it. The second is a source assertion rather than a call,
because `main()` calls `runApp` and constructs the real queue — the two things a host test cannot do.

#### The number is `unsynced`, and blocked work is a second line rather than part of it

`QueueSnapshot.unsynced` is **pending plus in flight**, which is the sum SHIP-125 chose and gave the
reason for: an operation the platform has refused is not waiting for a connection, it is waiting for
a person, and a number that never falls however long the driver stands in the open is not what
`Docs/02` §3.1 asks for.

**That reasoning leaves a hole, and this ticket closes it rather than inheriting it.** A device with
one quarantined update and nothing pending would show no indicator at all — silence about the one
update that most deserves attention. So the indicator appears when **either** number is above zero,
and the blocked count is a second line in different words: *needs attention* rather than *waiting to
sync*. Two numbers rather than three-of-which-one-is-stuck, so a driver who stands in the open
watches the first fall to zero and the second stay put, which is true and is what tells them the
second needs something other than patience. What lost and to what is SHIP-132's screen.

**The in-flight half of the sum is held by a hand-built snapshot and by nothing else, deliberately.**
A published snapshot almost never carries an in-flight operation — the worker publishes at the end of
a pass, by which time each claimed operation has been completed, released or blocked — so a test
driving the real worker cannot produce one, and the term would be untested against a real queue while
looking well covered. That is why the arithmetic is tested over constructed snapshots and the
persistence over the running application, rather than both being attempted in one place.

#### At zero it draws nothing, and that is a decision rather than an omission

A bar that is always present and usually reads zero is a bar people learn not to read, and this rung
of the ladder is entirely about the case where the number is not zero. What answers "was my work
recorded" in the settled case is SHIP-129's delivery screen, where each recording carries its own
word — a per-item answer, which is the stronger one. The two halves are complementary and the tests
say so in the same file: the indicator goes and "Recorded" stays.

#### Mutation testing

| Mutation | Result |
|---|---|
| The count reads `pending.length` instead of `unsynced` | **Caught**, one test — the constructed snapshot with one operation of each kind |
| The indicator hidden whenever nothing is waiting, ignoring blocked work | **Caught**, two tests |
| The `builder` removed from `MaterialApp.router`, leaving the indicator unmounted | **Caught**, one test — the journey that leaves the screen |
| `main.dart`'s `queueWatchProvider` override removed | **Caught**, one test — the source assertion written for exactly this |

Every mutation was reverted immediately and `git diff` confirmed the tree.

#### How it was demonstrated

`make flutter-check` green in this worktree: **726 host tests** (up from 714), the analyzer clean, and
the environment test per build flavour. `make verify` does not cover this ticket and its count does
not move.

**And on a device, against the live API.** `integration_test/record_milestone_test.dart` — written for
SHIP-129 and extended here — takes `main.dart`'s own override, **pauses the worker before the tap**,
and reads both halves at once: the screen says "Pending" and the bar says "1 update waiting to sync".
The worker is then resumed, the platform answers `201`, and both settle — "Recorded" on the entry and
no bar at all. Pausing is what makes the pending state observable rather than raced: against a
working API it would otherwise last a few milliseconds.

That run also demonstrated something neither ticket claimed. It was the **second** recording of
`en_route_to_pickup` against the same job, and the platform wrote a second row under a second key and
answered `201` — SHIP-111's "a repeat that is not a retry is a second row", which is `Docs/02` §5's
failed pickup attempt. **The app presented it as "Recorded", not as an error**, which is the behaviour
`Docs/02` §3.1 requires of a client whose update the platform keeps without moving the job.

### SHIP-168 — the gate, and the four decisions "at launch" turns out to contain

`Docs/09` is one line: *a build below the floor blocks with an update prompt linking to the store.*
SHIP-167 built the endpoint eight months of tickets early, because `Docs/08` Step 3 is right that a
gate cannot be added retroactively to builds already on devices. This is the other half, and it is
the client's first **self-limiting** feature: everything before it decided what the app could do,
and this decides whether it runs at all.

**The strike this ticket carried for four waves was right about the store and wrong about the
ticket, and the wave-6 reconciliation was right to lift it.** The old reason read "its store link
does not exist until X-2/X-3 publish listings". Building it settles the question the reconciliation
argued from the outside: the pilot's shape — blocked, with no link — is not a degraded version of
the feature, it is **the** version, and the platform decided it deliberately.
`internal/config/config.go` allows an empty `IOS_STORE_URL` rather than refusing it at startup, and
says why in its own comment. So the only thing X-2 and X-3 supply is a URL that resolves. Both
shapes are built, both are tested, and both were driven against the running service.

#### Where the build number comes from, which was the decision with a wrong answer available

The floor is an integer build number per platform, and the running build's number is the **native**
one: `CFBundleVersion` on iOS, `versionCode` on Android, read through `package_info_plus`. Flutter
already writes it — `Info.plist` holds `$(FLUTTER_BUILD_NUMBER)`, `build.gradle.kts` holds
`flutter.versionCode` — so what is compared is exactly what `--build-number` set and, locally,
exactly the `+1` of `version: 1.0.0+1`.

**The alternative needed no package at all and was rejected.** `ApiEnvironment` already selects the
deployment with `--dart-define`, and a `SHIPPER_BUILD_NUMBER` define would have followed that
pattern for nothing. It is a *second* place the build number lives, and nothing can make the two
agree: the release pipeline at SHIP-24…27 would have to pass `--build-number=N` and a matching
define forever, and the first build that passes only one compares the wrong integer. Too low locks
out a supported build; too high admits exactly the build the floor was raised to retire. Neither
produces a test failure or a log line. That is a large silent failure to buy with one avoided
dependency, so the number is read from the place the store itself reads it.

Two packages arrive with this ticket — `package_info_plus` and `url_launcher`, both
flutter.dev-published. `pubspec.yaml` carries the reasoning and the native-footprint check §9 asks
for: neither reads a device identifier, a contact, a location or an advertising id, so unlike
`device_info_plus` — which §9 declines for exactly that reason — **neither the Apple privacy labels
nor the Play data-safety declaration moves.** Both compile against `flutter.compileSdkVersion` with
minSdk 19 and 24, so the API 24 floor that pins `flutter_secure_storage` at 10.x does not move
either, which was the question worth asking before adding anything with a native half.

The comparison is `build.number >= floor` runs, `<` blocks. **Equal to the floor runs**, because
`internal/config` calls the field "the lowest build number still permitted" — and `<=` would lock
out every device on the exact build the floor was just raised to, which is the largest population
there is at that moment.

#### What an unreachable API means: it does not block, and that is the ticket's real decision

The check runs on the first frame, and the app draws normally while it is in flight. **A check that
has not answered, has failed, or has thrown while decoding leaves the app running.**

Three things make failing open right, and they are worth having written down because failing closed
is the instinct:

- **The gate is not a control.** `Docs/07` §3 and `CLAUDE.md` put every authorisation decision on
  the platform, and this is the same rule wearing different clothes: the app may block, the platform
  decides. What actually retires a build is `/v1` refusing it. This screen exists to tell somebody
  *why*, and where to go.
- **Failing closed brands the app on the platform's worst day.** Every device that opened the app
  during an outage would show an update prompt for an update that does not exist, and the way out
  would be a release — which is precisely the loop `Docs/07` §6 says mobile does not have. A gate
  that turns a partial outage into a total one is worse than the builds it guards against.
- **`Docs/07` §4 makes working without signal the client's most important capability.** Holding the
  first frame behind a round trip would mean up to ten seconds of blank screen on a bad connection,
  and an app that will not open on a loading dock.

**The cost of failing open is smaller than it looks, and Riverpod is why.**
`ProviderContainer.defaultRetry` re-runs a failed provider ten times with exponential backoff from
200ms to a 6.4-second ceiling, so a launch that lands in a lift gets its answer about forty-five
seconds later with nobody doing anything, and a device with no signal at all stops asking rather
than polling for as long as the app is open. That default is **kept deliberately** rather than
replaced: SHIP-125 already owns this application's one hand-written backoff, and its five minutes
are for a queue that must eventually drain rather than for a courtesy at start-up.

**One thing found while building it, which the next person to touch `updateVerdictProvider` has to
know: while Riverpod is retrying, the state is `AsyncLoading` *carrying an error*, not
`AsyncError`.** A fail-open written as "block unless the state is an error" would therefore be wrong
in the one case it was written for. Matching on the value is the only safe form.

**The limitation, stated rather than hidden:** the check runs once per process, and a handset
process survives for days. A floor raised this morning reaches a device at its next cold start, not
its next foreground. A resume trigger is the obvious extension — `sync_signals.dart` already listens
to the lifecycle — and it was left out because `Docs/09` says *launch*, and because a second trigger
wants `Docs/07` §6's soft-prompt half, which is a different ticket.

#### "Blocks" was read as the strong word, so the gate replaces the app rather than covering it

`VersionGate` sits inside `MaterialApp.router`'s builder, above the navigator and above SHIP-126's
indicator, and below the floor it returns the prompt **in place of** its child. The router is
therefore not built at all: there is no screen behind this one to reach by dismissing it, by the
Android back gesture, or by a deep link, because there is nothing there. A dialog or a `Stack`
overlay would have left a live application underneath, and on Android a modal barrier is dismissible
almost by definition. There is no "later" — `Docs/07` §6 has a soft prompt for the case where
carrying on is acceptable, and that is a different mechanism for a different situation.

The queue keeps draining while the prompt is up, because the sync worker runs from `main` rather
than from the widget tree. That is deliberate: work a driver already recorded belongs to them, and a
blocked build should still hand it over if the platform will still take it.

#### The screen with no link is the pilot's screen, not a broken one

Both shapes carry the same icon, the same headline — *Update Shipper to keep going* — and the same
explanation. The difference is the last element: a button that goes there, or a sentence saying
where to go. *"Open the app store you installed Shipper from and install the latest version"* is
true whether that was TestFlight, Play internal testing, or eventually a public listing, so the
screen never has to know which. There is no spinner, no disabled button and no empty space where a
control should be — a greyed-out "Update" is exactly the thing that reads as broken.

A link that fails to open falls back to the same sentence and prints the destination, because
`launchUrl` returns `false` when nothing on the device handles a URL and a button that appears to do
nothing is worse than no button, on a screen with no way off it.

**The destination itself remains provisional**, in the same way and for the same reason as the
bundle identifier in §9: `IOS_STORE_URL` and `ANDROID_STORE_URL` are configuration with no correct
value until X-2 and X-3 publish, and setting them is a deployment change rather than a release.

#### Two seams, and the reason both are empty by default

`runningBuildProvider` is `null` until `main.dart` supplies it, which is SHIP-126's inversion used a
second time and for the same objection: **every widget test builds `ShipperApp`**, and a gate that
reached the package-info channel on its own would have every one of them call a plugin with nothing
behind it and then open a connection to whatever base URL the test binary was compiled with. With
the seam empty there is no build number, so nothing is compared and **no request is made at all** —
which is held as a test rather than assumed. The consequence is the same one SHIP-126 wrote down:
an application that never overrides it is never blocked, so `version_gate_wiring_test.dart` holds
`main`'s override with a source assertion.

The launch check travels on `unauthenticatedApiClientProvider`, and that is not tidiness.
`cmd/api/routes_app.go` made the route public because "a build old enough to be blocked may be old
enough that its authentication no longer works" — so putting the check behind the session would
leave exactly those builds unable to discover they must update.

#### Mutation testing

| Mutation | Result |
|---|---|
| `>=` becomes `>`, so a build equal to the floor blocks | **Caught**, two tests — the boundary case and the wiring test's supported build |
| `verdictFor` always returns a link, blank or not | **Caught**, two tests |
| The screen accepts a destination with no scheme as a link | **Caught**, one test |
| The prompt drawn in a `Stack` over the app instead of in place of it | **Caught**, two tests — the shell is still in the tree |
| The "no build number, no request" guard removed from the launch check | **Caught**, two tests |
| `main.dart`'s `runningBuildProvider` override removed | **Caught**, one test — the source assertion |
| **The `try/catch` removed from `RunningBuild.read`** | **SURVIVED** — see below |

**The survivor is the one worth reading.** `RunningBuild.read` swallows everything and answers
`null`, and deleting that guard broke no test in the suite — while in production `main` **awaits** it
before `runApp`, so a `MissingPluginException` there is not an inert gate but an application that
never draws a frame. A launch-time check that can stop the launch is the worst available version of
this ticket, and nothing was holding it. It survived because every host test supplied a build number
rather than reading one, and every device test had a real plugin behind the channel — so the failure
path existed on neither side.

Fixed in the suite rather than tuned away, following SHIP-129's precedent:
`test/core/version/running_build_test.dart` now asks for the build number **with no plugin behind
the channel** and requires `null`, then mocks the platform for the two cases it can only reach that
way. Its tests run in declaration order and have to — `PackageInfo` caches the first answer in a
static with no reset, so the un-mocked case can only be asked first.

#### How it was demonstrated

`make flutter-check` green in this worktree: **766 host tests** (up from 726), the analyzer clean,
and the environment test per build flavour. `make verify` does not cover this ticket and its count
does not move — SHIP-167's section already demonstrates the endpoint, and this ticket adds none.

**And on a device, against the live API**, which is where the *Done when* is actually met.
`integration_test/version_gate_test.dart` runs the production widget tree with the real
`package_info_plus` channel and real HTTP to `GET /v1/app/minimum-version`, and the three cases are
three **service configurations** rather than three fixtures — which is `Docs/07` §6's point that
raising the floor is an operational act, demonstrated rather than restated. On an iPhone 17
simulator and a Pixel emulator, against this worktree's API on port 8092:

| Service configuration | What the device did |
|---|---|
| Default floor of 1 | Ran normally. `RunningBuild.read()` returned the real `+1` from the bundle |
| `MIN_SUPPORTED_*_BUILD=9999`, no store URL | **Blocked**, with the instruction and no button, and no shell anywhere in the tree |
| …and `IOS_STORE_URL` / `ANDROID_STORE_URL` set | **Blocked**, with the button carrying the platform's URL |

The store link is deliberately not tapped on a device: `launchUrl` would leave the simulator's App
Store in front of the harness, and what a live run demonstrates is that the platform's URL reached
the screen. The tap is a host test, over a seam.
### SHIP-120 — the link is the credential, and the job identifier is in it twice on purpose

The fourth deployable has product code in it. `apps/driver-portal` had been a placeholder shell
since wave 1; a driver now opens a link and sees the delivery it names, authenticated by SHIP-107's
token and refused by SHIP-108's verifier.

**The link is `https://<portal>/j/<job-id>#<token>`, and the identifier being in it as well as
inside the token is the mechanism rather than the redundancy.** This is the decision the ticket
turned on and the one every later driver ticket inherits. SHIP-108 put the one-job comparison in the
auth class so no handler could skip it — the path says what the client means to act on, the token
says what the caller may act on, and comparing them is what makes "exactly one job" observable from
outside. **A client that decoded `job_id` out of the token to build the path would make that
comparison compare the token with itself.** It would pass for ever, on any grant, however widely
issued: the platform would still be checking and the check would have nothing left to catch. So the
identifier reaches the portal independently, and **nothing in this application parses a JWT**. The
one claim a page might want is served as `link_expires_at`, which is what the contract already tells
clients to do instead of reading a credential.

**The token lives in the URL fragment on arrival and in `sessionStorage` after it, and the fragment
is stripped from the address bar.** A fragment is the one part of a URL never sent to any server: it
is in no request line, so it reaches no access log, no proxy, no CDN and no `Referer`. A token in
the path or the query is in all of them, at every hop, for as long as those logs are kept — and this
credential lasts seven days and travels through whatever messaging channel the provider already
uses. Stripping it afterwards is the second half: the address bar is the most screenshotted and most
shoulder-surfed surface a driver has, and once the token is in storage the URL is a job identifier
the platform will not serve to anybody else.

The alternatives, and what each costs. **`localStorage`** would leave a working credential on the
device until something deleted it, which is wrong on a phone drivers share and hand over.
**A cookie** would be attached to every request to the origin including ones with nothing to do with
the delivery, and would need a scope and a lifetime decided in the client rather than by the token's
own `exp`. **Leaving it in the URL** is what stripping gives up, and the cost is real: closing the
tab loses the token. That is the right trade because **the message thread the link arrived in is the
durable store** — the driver already has it, it is where they will look, and it is the only copy
that should survive. The fragment is stripped **only once the token is somewhere a reload can
find it**, so a browser refusing storage keeps a working link in its URL rather than a page that
cannot survive the pull-to-refresh a driver on one bar of signal will certainly do.

**An expired or revoked link says something true about the credential and nothing at all about the
job.** Four answers, and the difference between two of them is why SHIP-108 minted a code of its
own. `401` with `delivery_driver_link_expired` is "this link has expired, ask for a new one" —
deliberately not `token_expired`, whose meaning is "refresh and retry" and which would put a portal
with no refresh behind it into a loop. `401` with anything else is "this link is not valid", and the
platform does not distinguish a bad signature from a truncated URL from a mobile token, so neither
does the page. **`404` is one answer to two questions and it stays one**: a valid link on a
*different* job and a *stood-down* driver render identical copy, because `404` was chosen over `403`
precisely so a link-holder cannot confirm a competitor's job exists. A test holds the refusal copy
against a list of disclosing phrases, and the five messages are a `Record` keyed by the refusal
union so a new case without copy is a build failure rather than a blank screen.

**The page shows five fields because five is what the endpoint serves, and it renders every one.**
The endpoint's response *is* the platform's answer to what a driver may see, so a page that dropped
a field would take that decision back off the platform and one that added a field would invent it.
The two identifiers are the least interesting to look at and the most useful on a phone call, so
they are small and at the bottom rather than absent.

**And here is the honest half of the *Done when*, in the SHIP-65 and SHIP-77 shape.** "Opening the
link shows only that job's delivery detail" — the *only that job's* is demonstrable and demonstrated
below; the *delivery detail* is not, because **the endpoint serves no pickup, no drop-off, no goods
and no contact**. SHIP-108 said so in place ("the delivery detail is SHIP-120's… fields are added to
this shape") and this lane may touch no Go. So the page says the details are not carried by the link
yet rather than leaving a blank card, and **`Docs/11` §4 wants a SHIP-120 row**: the screen exists,
one clause of its *Done when* belongs to a Go change nobody has a ticket for. `Docs/03` §3 puts
those fields in the driver's Prepare stage, so this is a product gap and not only a bookkeeping one.
The shape is small — `driverJobResponse` gains the pickup and drop-off locations, the goods
description and a contact, from a port into `jobs` that `delivery` does not yet declare — and it is
additive, so no route moves.

**There is a route handler in front of the API, and it is not a BFF tier.** `CLAUDE.md` is explicit
that the Go platform owns the versioned public API directly. `app/api/driver/jobs/[jobId]/route.ts`
adds nothing to it and hides nothing from it: one route, one method, one upstream path, no logic and
no state, forwarding the platform's status and body with the request id intact. **It exists because
the service serves no CORS headers** — a browser asked to send a bearer credential header
cross-origin sends a preflight `OPTIONS` first, nothing in `internal/httpx` answers one, and the
fetch is refused before the platform sees it. `Docs/10` §8.4 already allows a web surface its own
server-side data access as "an application detail and not a shared platform tier", which is what
this is. **If CORS is added on the Go side this file is deleted** and the browser fetches the
platform directly — but note what would be lost: the API's location is currently a *server*
environment variable read per request (`SHIPPER_API_BASE_URL`), where the direct-fetch shape needs a
`NEXT_PUBLIC_` value inlined into a bundle at build time. "Anything expected to change under
operational pressure lives server-side" is the same argument this repository makes about Dart.

**The narrowness is the security property.** A `rewrites()` entry in `next.config.ts` would have
been three lines and would have proxied everything under `/v1`, making this origin a
credential-forwarding front door to the whole platform. This route reaches one endpoint: `GET`
because no other export exists, a template for the path, and the one hole refused unless it is a job
identifier. `..%2f..%2fv1%2fjobs` produces a `400` and no outbound request. **That refusal is not an
authorisation decision** — it declines to build a URL other than the one the route exists for, and
who may open the job is the platform's answer and the only one forwarded.

**Twenty tests, and no test framework in the dependencies.** Node 22 strips TypeScript types itself,
so `node --test` over `lib/` costs no dependency, no lockfile change and no build step —
`tsconfig.json` sets `allowImportingTsExtensions` so a test imports `./link.ts` by its real name.
`lib/surface.test.ts` is the one worth knowing about: **the set of files that may make a request,
name a credential, or hold one is closed**, so a second call site is a failing test naming the file
rather than a review comment. It reads code with comments stripped, because every doc comment in
this application discusses the credential header and `localStorage` at length — the first draft failed on
the page copy that reads "there is no account to sign in to", which is the page saying the right
thing, and the rule was narrowed from a phrase search to a file set in response.

~~**`make web-check` does not run those tests, and that is a one-line ask rather than a gap this lane
could close.**~~ **Closed by the follow-up run below.** `web-check` is now
`web-lint web-test web-build web-typecheck`, `web-test` exists, and CI runs it because both web
workflows run `web-check`. The original reasoning stood: the target was
`web-lint web-build web-typecheck` in `mk/web.mk`, both that file and
`.github/workflows/web-driver-portal.yml` were outside SHIP-120's ownership, and `pnpm -r run test`
skips a package with no `test` script — which was confirmed rather than assumed before the target
was written, and the admin panel is untouched until it grows one. The point it was making is the one
worth keeping: **a test that quietly does not run is the thing this repository is most careful
about**, which is `CLAUDE.md`'s position on integration tests that skip, applied to a surface where
nothing enforced it.

**Demonstrated in a real browser against a real running service, not asserted.** Headless Chrome
against the built portal on 3002 and the API on 8093, over two awarded jobs each with a real driver
token minted by `POST /v1/jobs/{id}/driver`: the driver's own link renders the delivery; the *same*
link on the *other* job renders "this link no longer opens a delivery"; the second link opens its
own job and not the first; a link with no fragment renders "open the link you were sent"; a
provider's mobile session token in the fragment renders "this link is not valid"; a hand-built
expired token renders "this link has expired"; and `/j/..%2f..%2fv1%2fjobs` renders the same
invalid-link page with no request made. The platform's raw answers behind those six were `200`,
`404 not_found`, `401 unauthenticated`, `401 delivery_driver_link_expired` and `400 bad_request`.

**Three mutations, and the one that survived is the most useful thing in this entry.** Pointing the
page at another job while holding the first job's link renders the `404` copy — the *platform*
refuses it, and there is no client-side branch that could have hidden it instead. Adding a second
`fetch` to a user route fails `surface.test.ts` naming the file. **The third is the one to read:
deriving the job identifier from the token instead of from the path makes the wrong-job case
*succeed*, and every test in the application still passes.** That is the tautology described at the
top of this entry, made concrete: the platform's one-job check is only worth anything while the
client states its intention independently of the credential, and no test on either side of the wire
catches a client that stops doing so. ~~It is recorded because it is the thing SHIP-121, SHIP-122 and
SHIP-123 are most likely to do by accident while making the code tidier.~~ **It is now caught —
`lib/one-job.test.ts`, in the follow-up below.** The finding stands exactly as written; what has
changed is that a test fails when somebody makes it true.

**What SHIP-121's endpoint should look like, which this ticket makes obvious and builds none of.**
§9 already carries the correction — the driver's idempotency scope was booked against SHIP-112 and
is now against SHIP-121 — and it is right that whoever picks it up is adding a route rather than
drawing buttons over an existing one. The shape:

- **`POST /v1/driver/jobs/{id}/milestones`**, `RequireDriverToken`, beside the read rather than
  under `/jobs`. Two credential systems on one path is what SHIP-108 refused for the read and the
  argument is unchanged; `/driver/...` says whose surface it is.
- **It accepts what `recordMilestoneRequest` accepts** — `milestone`, `recorded_at`, `reason` — and
  **refuses `delivered`** for the reason `jobLifecycle` has no method for it: `Docs/01` §4.4 makes
  proof or a recorded exception the condition, and neither can be captured until SHIP-114…116.
- **The actor is `driver`, not `provider`.** `milestones.actor_type = 'driver'` names the
  `driver_assignments` row, and **SHIP-107 put `assignment_id` in the token for exactly this** —
  `RecordMilestone` says in place that this is the one field which changes when a driver can present
  a credential. Nothing has to be looked up to attribute the work.
- **The idempotency key is the open question and it is now reachable.** §9's two shapes stand: a
  second group-wide resolver beside `ResolveSubject` that a driver grant can populate, with
  `SubjectScope` widened to read either; or an explicit ruling that a job-scoped grant scopes on the
  job identifier already in the path. **A third is worth adding: scope on `assignment_id`**, which
  is inside the token, is not in the URL, and is not guessable from anything a link-holder can see —
  where the job identifier is in the path of every request the driver makes, so scoping on it is
  barely stronger than `anonymous`. `anonymous` is defensible for a read and this is a write from a
  phone with a bad connection, which is the whole reason the entry exists.

**What this lane wants from `internal/config`, and it is one variable.** **`DRIVER_PORTAL_BASE_URL`,
so the platform can assemble the link.** SHIP-107 declined to — "a base URL here would be this
domain asserting a path in an application it does not own" — and that was right while the route did
not exist. It exists now: `<base>/j/<job-id>#<token>`. Today the assignment response returns
`driver_token` and the *provider's app* would have to build the URL, which puts a portal path and
hostname inside a Flutter binary that cannot be updated over the air — precisely what `Docs/07` §1
says belongs on the server. A `driver_link` field beside `driver_token`, built from one configuration
value, is the fix, and it is a Go change no wave-7 lane can make. Nothing else is needed: the
portal's own configuration is a server environment variable in its own deployment.

**Two smaller things.** The placeholder `/job` route is deleted rather than kept — a route rendering
a delivery beside no credential is the shape SHIP-23 refused to build, and keeping it once the real
one exists would be worse than never having had it. And the whole portal is `noindex`, because a
delivery page is credential-gated and would index as a refusal.

No new dependency, no lockfile change, no Go, no route, no contract, no migration, and no
`make verify` section — this ticket adds no HTTP endpoint to the platform.

### The two gaps SHIP-120 recorded, closed — and this claims no ticket

A follow-up run on the same branch, holding no ticket of its own: SHIP-120 is delivered and both
findings above are now closed rather than only found. Nothing here builds SHIP-121, SHIP-122 or
SHIP-123, and nothing here touches Go.

**The surviving mutation is caught, and it is caught on the wire.** `lib/one-job.test.ts` opens a
link whose *URL* names one job and whose *token* grants another, and asserts on **the requests the
portal actually issues**. Both hops are real: `openLink` is what the page calls, its `fetch` is
answered by the application's own route handler — the same `GET` Next serves — and that handler's
outbound call is what the test records. Only the platform is a stand-in, and the stand-in
*implements* SHIP-108's check rather than asserting about it, so the wrong-job case is refused there
for the same reason it is refused in production.

**It is deliberately not a source scan**, and the reason is two scars in this file: §7b's budget
guard was satisfied by a rename, and §9 records the client-side one carrying the same blind spot. A
scan for "does anything decode the token" would be a third of those. Asking instead what path left
the process cannot be renamed past.

**Demonstrated in both directions, at all three places the mutation could be made**, each applied,
run, and reverted to a byte-identical file:

| The job identifier derived from the token in | Result |
|---|---|
| `lib/open.ts` — what the page calls | 23 pass, **2 fail**: the upstream path is the token's job, and the wrong-job case returns a delivery instead of the refusal |
| `lib/delivery.ts` — the browser's fetch | 23 pass, **2 fail**, identically |
| `app/api/driver/jobs/[jobId]/route.ts` — the outbound hop | 22 pass, **3 fail**: the two above plus `surface.test.ts`'s single-path assertion |
| nothing mutated | **25 pass** |

The first row is the one to read against the original finding. **Under that same mutation, all five
of `surface.test.ts`'s tests still pass** — the guard that was in place kept passing, exactly as
recorded, and the two that fail are the new ones.

**One structural change made the test possible, and it is worth naming as a cost.** Reading the link
— `tokenForThisView` and `openLink` — moved out of `components/delivery-link.tsx` into
`lib/open.ts`. **Node 22 strips types but not JSX**, so nothing inside a `.tsx` file can be reached
by `node --test`, and the chain had to be somewhere a test could call it. The component is left
holding state and markup, which is the half a test could not check anyway. `lib/alias-hooks.mts` is
the other piece: fifteen lines resolving `@/` the way `tsconfig.json` does, registered by the one
test that imports the route handler, so no other test file's runtime changes.

**`make web-check` runs the tests.** `web-check` is now `web-lint web-test web-build web-typecheck`
— 25 tests where it ran none, and green in a worktree. **The tests sit second rather than last on
purpose**: they cost a tenth of a second against the build's thirty, so a failing guard is reported
before the slow part, and they depend on nothing the build produces. The lint → build → typecheck
order SHIP-15e established is untouched, because that dependency is the type-check's alone.
`pnpm -r run test` skipping a package with no `test` script was **confirmed, not assumed** — the
target exits 0 and never names the admin panel — so the admin surface is unaffected until it grows
a script. Both workflows' job names now read `lint, test, build, typecheck`, because a job name
claiming three checks while the target ran four is the same drift in miniature.

**The path filter was re-demonstrated rather than trusted to have survived**, by evaluating every
workflow's `paths:` block against this run's own changed files:

| A change to | Go | Flutter | Admin | Driver portal |
|---|---|---|---|---|
| `apps/driver-portal/lib/one-job.test.ts` | — | — | — | runs |
| `apps/driver-portal/.gitignore` | — | — | — | runs |
| `apps/admin/.gitignore` | — | — | runs | — |
| `.github/workflows/web-driver-portal.yml` | — | — | — | runs |
| `mk/web.mk` | **runs** | — | runs | runs |
| `services/core/**` | runs | — | — | — |
| `apps/mobile/**` | — | runs | — | — |
| `Docs/**` | — | — | — | — |

A driver-portal change starts the driver-portal workflow and nothing else, which is the line that
mattered. **The `mk/web.mk` row is a finding and it is not this run's to fix**: `go.yml` filters on
`mk/**`, so a change to the *web* make targets starts the **Go** workflow. `flutter.yml` narrowed
that glob deliberately and says so in its header — "it deliberately does not list `mk/**` the way
the Go workflow does" — and the Go workflow did not follow. It is one line in a file this run does
not own, it costs a Linux run rather than a macOS one, and it is reported here rather than changed.

**`next dev` writes `AGENTS.md` and `CLAUDE.md` into the application it serves**, on every start.
They were in no `.gitignore`, so `git add -A` collected them — a generated file arriving in a commit
that has nothing to do with it. The rules are **app-local**, one file per web surface, rather than in
the root `.gitignore`: the root file is shared with every lane and these are not, so the change
lands with no chance of a conflict — and a path-scoped `/CLAUDE.md` in `apps/<app>/` cannot reach
the repository's own `CLAUDE.md`, which a `**/CLAUDE.md` in the shared file very nearly could.
Verified by running `next dev` for **both** surfaces, stopping it, and reading `git status` clean
with `git check-ignore -v` naming the rule.
### SHIP-114 — the upload the API is not in the path of, and the implementation that was deleted before it was written

Five points, one endpoint, one adapter, **no migration and no table**. `POST /v1/jobs/{id}/proof-uploads`
answers the awarded provider with a short-lived pre-signed URL and an object key; the client PUTs the
photograph **to the object store**, and this service sees neither the request nor the bytes. That is
`Docs/06` §5.2 read exactly, and it is the word the *Done when* turns on — "uploads **directly**" —
so it is the one claim `scripts/verify/70-delivery.sh` checks three separate ways: the URL is
asserted to be at `STORAGE_ENDPOINT` before it is used, the upload goes there, and the object is
read back out of the bucket from inside the container.

**The ticket had been struck in §6 for five consecutive waves** for want of a store to demonstrate
against. SHIP-15p supplied it, and the pre-step earned its three points here: the two findings it
recorded — that SigV4 signs the `host` header, and that there is no empty-endpoint state — were both
load-bearing and neither cost this ticket an hour.

#### The recorded question: `local.go` is dropped, and the filesystem implementation is not written

SHIP-15p left this open deliberately and it is now decided. **`internal/platform/storage` holds one
implementation, `s3.go`, used in every environment.** `doc.go` carries the reasoning; the four
arguments are:

- **The premise is gone.** That specification was written at SHIP-10, when development had no object
  store. It has one now, so the implementation that runs in production is the one exercised locally
  against genuine pre-signed-URL semantics — which is the argument `CLAUDE.md` already makes for
  testing against a real PostgreSQL rather than a mocked repository, and the one SHIP-15p made for
  putting a real store in compose rather than a stub.
- **`Docs/06` §4.1's test is "does a second implementation exist today"**, and writing one so that
  the answer becomes yes inverts the test into the thing it was written to refuse. Email and SMS
  have two because console-versus-provider is a real operational difference — development must not
  send mail to a person. Storage has no equivalent: writing a byte into a local bucket harms nobody.
- **It would be a second signing scheme**, plus a verifier inside the API, plus a route serving the
  bytes — and that last part contradicts the one rule this package states most firmly. A filesystem
  implementation is not a second implementation of the same thing; it is a *different architecture*
  that only development would ever run, which is the drift that produces "it worked locally".
- **"A developer with no container running"** is answered the way it already is for PostgreSQL and
  Redis: `make up`. The tests fail rather than skip without the stack, deliberately.

**`Docs/06` §4.1's adapter table said "Local storage in development, S3 deployed", and that row went
wrong the moment `local.go` was dropped. The repository owner has ruled: accept the drop — one
`s3.go`, in every environment.** The row now reads "the store, not the code", and the four arguments
above are condensed into a paragraph under the table, so the document and the tree agree. The row is
**corrected rather than deleted**: `CLAUDE.md` names object storage in its adapter list, the seam is
still there — `delivery` declares the port and may not import the adapter — and what changed is only
that the second implementation is the store rather than a second Go type. That correction is the one
shared-file edit this branch was unlocked for, and it carries no ticket of its own.

#### No SDK, and the reason is the architecture rather than `go.mod`

The signature is about eighty lines of HMAC against the standard library. `aws-sdk-go-v2` was not
taken, and the first reason is not the one a reader expects: **this package makes no request to the
object store, ever.** There is no upload, download, listing or delete — only signing — so an SDK
would contribute a transport, a retry policy and a credential chain that nothing here would call.
The second reason is the rule: `go.mod` is a shared surface no domain branch may edit, so the
dependency would have been a request rather than a commit. It did not decide the design, but it is
what made the question worth asking first.

**The algorithm is held to AWS's own published example** —
`TestTheAWSExampleSignsToThePublishedSignature` reproduces the documented query-string request and
its signature `aeeed9bb…`, a value nobody in this repository computed. Every other test here would
still pass if the implementation were self-consistently wrong; that one would not. Beside it, the
signer is exercised against the running MinIO, which is the only evidence that matters for the
*Done when*.

#### The type and the size are signed, which is what makes them enforcement rather than advice

This is the design decision most worth reading. `STORAGE_MAX_UPLOAD_BYTES` and
`STORAGE_ACCEPTED_CONTENT_TYPES` are checked before a URL is issued — and **that check on its own
enforces nothing**, because the request it inspects carries no bytes. A URL signed over `host` alone
authorises *any* body: a client could ask for a 200 KB JPEG, be told yes, and PUT four hundred
megabytes of anything.

So the URL signs `content-type` and `content-length` as well, and the client is told both back and
must send them exactly. The store recomputes the signature over the headers the request actually
carried, so an upload that changes either is refused with `403 SignatureDoesNotMatch` — **by the
store, on the request that matters, with this service nowhere in the path**. `content-length` is the
only bound available to a pre-signed PUT at all (S3's `content-length-range` belongs to the browser
POST-policy form, a different protocol), which is why a client states the exact size rather than a
maximum. It has the file; it knows.

The consequence for the wire is that `content_type` comes back possibly re-spelled — lower-cased and
trimmed — and a client that sends its own spelling gets a signature failure with no explanation. The
contract says so at the top of the operation.

#### What a retry returns, and why there is no row behind it

**The middleware's replay is the whole of the guarantee, and here that is the right amount.** A
repeat carrying the same `Idempotency-Key` never reaches the handler: the client gets the identical
URL, the identical key and the identical `expires_at`, already running down. One intent bought one
upload slot, and a retry must not extend the life of a credential nothing can revoke. Once the Redis
entry has gone, a retry mints a **new** URL for a **new** object key.

That is deliberately weaker than SHIP-111's arrangement, and the difference is what is durable.
`RecordMilestone` needed a unique index because a second row would be a second *recorded fact* about
the delivery. Here nothing was written the first time — no row, no object — so a second URL leaves at
most one unreferenced object, and SHIP-115 is what decides which key is the job's proof.

**A key derived from the idempotency key was considered and rejected**, and the rejection is the
interesting half. It would make a retry return the same object key, which sounds better and is
worse: a client holding an old key could ask for a fresh URL over an object that already holds
proof, and **proof is evidence**. A key nothing can predict means an issued URL can only ever write
an object that did not exist when it was signed. `TestEveryUploadGetsAKeyOfItsOwn` is what fails if
somebody "fixes" the retry behaviour later.

The key is `proof/<job>/<uuidv7>`, prefixed so it is legible in a log without a lookup, and **with no
file extension** — an extension would have to come from a hard-coded content-type-to-suffix map that
had to stay in step with a *configured* list, which is exactly the drift `Docs/06` §5.3 is about. The
store already records the media type from the signed header.

#### 200 rather than 201, and no migration at all

Nothing is created. The platform holds no record of the URL and the bucket holds no object until the
client PUTs one, so this answers `200` like `POST /v1/auth/login` — the other endpoint whose whole
output is a credential. **SHIP-115 is the ticket that creates something**, and migrations `000603`
onward are still unallocated: "uploaded proof is linked to a job and milestone with access control"
is its *Done when*, not this one's, and a `proof` table written here would have been building into
its way. `internal/platform/storage` likewise has no `PresignDownload`; reading proof back needs an
authorisation check this package must not make and a consumer that does not exist (SHIP-115,
SHIP-155). When one arrives it is four lines.

#### What was needed of `internal/config` beyond SHIP-15p: nothing

All nine fields were used and none was missing, which is the first time a wave-7 track can say that
about a shared surface. The split is worth recording because it is not arbitrary: six describe the
*store* and are read by `cmd/api` into `storage.Options`; three — `PresignTTL`, `MaxUploadBytes`,
`AcceptedContentTypes` — describe what the platform will allow into it and are read into
`delivery.UploadPolicy`. That is `Docs/06` §4.1's division of labour written as two structs, and it
is why the adapter takes the TTL as an argument rather than holding it.

#### Two edits outside this lane's own files, both minimal

`cmd/api/routes_test.go` gained a `testStorageConfig()` beside the argon2 profile and the driver
keyset already there, for the third instance of one cause: the delivery handler is built during
attach from every test that constructs a router, and a signer that cannot sign stops the process.
`config.Storage{}` has an empty endpoint and an empty endpoint is not a URL. **This is the shape
SHIP-15p fixed in `routerWithApp` and did not fix here** — `testDeps` still substitutes a whole
`config.Config` literal, so it collects a section per domain requirement. A guard like
`TestTheAppFixtureOverwritesNothingButApp` cannot help, because this fixture *is* the base. Worth a
§9 item rather than a fix from a domain branch.

And `routes_golden.txt` gained one line. The sorted set was confirmed unchanged apart from the
addition before `-update` was run.

#### Mutation testing: seven mutations, seven caught, and one instructive survivor

Each was reverted immediately and the revert confirmed with `git diff`.

| Mutation | Result |
|---|---|
| Ignore the configured TTL and sign for the protocol maximum | **Caught** — four tests, including `TestAnExpiredURLIsRefused` against the real store |
| Neuter the accepted-content-type check | **Caught** — four cases across the domain and the wire |
| Neuter the size limit | **Caught** — two cases |
| Remove the `awarded != providerID` check | **Caught** — `TestOnlyTheAwardedProviderGetsAnUploadURL`, both refusals, and the 404-parity test |
| Sign `host` alone, dropping the two content headers | **Caught** — and by the test that matters: the *real store accepted a substituted content type and a longer body* |
| Make the object key deterministic per job | **Caught** — `TestEveryUploadGetsAKeyOfItsOwn` |
| Set `mc anonymous set download` on the bucket | **Caught** — twice, by `TestTheBucketHasNoPublicReadPath` and by `scripts/verify/00-stack.sh`'s SHIP-15p block |

**The survivor is inside the fifth**, and it is worth naming rather than tuning away: with the signer
reduced to `host` alone, **`internal/delivery`'s entire test package stays green.** That is correct —
the domain's tests use a stub port, and a domain test that also signed could not fail on the
condition that matters — but it means the enforcement claim has exactly two guards, both outside the
domain: `internal/platform/storage`'s integration tests and `scripts/verify/70-delivery.sh`. Anybody
who ever finds those slow and skips them has removed the check that the platform's upload limits are
enforced at all, and no delivery test will say so.

#### One thing recorded rather than built: SHIP-121 and SHIP-122 both need a driver-token *write*

§9 already carries the mechanism — `httpx.SubjectScope` keys on the `authctx.Subject`, a driver
token deliberately produces none, and the scope is computed group-wide **outside** `Idempotent`
while a guard runs per route **inside** it, so a driver-token request scopes its idempotency key to
`anonymous` whatever the guard does. Building this endpoint sharpens it from a scoping question into
a concrete one, and the shape is now obvious enough to write down:

- **SHIP-121** needs `POST /v1/driver/jobs/{id}/milestones`, `RequireDriverToken`, under `/driver/`
  beside SHIP-108's read. The handler is `RecordMilestone` with `callerID` replaced by
  `driverGrantFrom` and `Record.Actor` set to `ActorDriver` — which `000601` and `milestone.go`
  already declare and nothing can yet reach. `delivery.Awards` is not consulted: a verified grant
  *is* the authorisation, and the grant already names the job and the assignment.
- **SHIP-122** needs the driver's version of this ticket's endpoint,
  `POST /v1/driver/jobs/{id}/proof-uploads`, on the same guard.

**Neither is built here, and the reason is not effort.** Both are state-changing, so both would land
in the `anonymous` idempotency scope — and SHIP-122's response body *is a credential that can write
into the evidence bucket*. That is precisely the cross-tenant read SHIP-44 was written to close, so
the honest position is that **the scope has to be settled before either endpoint exists**, not
alongside them. §9's two options stand: a second group-wide resolver a driver grant can populate
with `SubjectScope` widened to read either, or an explicit decision that a job-scoped grant scopes on
the job identifier already in the path. The first is right if a driver ever holds two links.

`make verify` went from 481 checks to 493 across the same 13 sections, all twelve in
`scripts/verify/70-delivery.sh`: the URL is issued, it points at the store and not at this API, it is
short-lived by its own signed window, the client uploads with it, the bytes are read back out of the
bucket, the object is refused unsigned, a substituted type or size is refused by the store, a
provider who was not awarded the job gets a 404, a script container and an over-limit photograph are
both refused with the field named, the request is refused without an `Idempotency-Key`, a retry
replays the identical URL, and the object is removed. **Every assertion names the key it created**,
because the bucket may be shared with four other worktrees.

### SHIP-115 — the record, and the question of what the platform is entitled to believe

`proofs` (migration `000603`), an optional `proof` on the milestone request, and
`GET /v1/jobs/{id}/delivery/proof`. An uploaded object becomes evidence for **one recorded
milestone**, and through it for one job, and the two parties to the delivery can read it back
through short-lived signed URLs issued after an authorisation check.

#### The hard part is not the table. It is that the platform never saw the photograph

SHIP-114 issues a URL and the client PUTs the bytes **to the store**, with this service in neither
direction. There is no callback and nothing to poll, so the platform cannot tell an upload that
succeeded from one that failed halfway from one that was never attempted. Two failures follow, and
**they are not symmetric**:

| | What it means | Decided |
|---|---|---|
| A row with no object | The platform holds a record saying a delivery was photographed, having never looked | **Refused.** `Service.VerifyProof` asks the store before anything is written |
| An object with no row | A URL was issued and spent and nothing came back to claim it | **Expected.** It is proof of nothing, reachable by nobody, and ages out under a lifecycle rule |

The first direction is the one the invariant rests on. `Docs/01` §4.4 makes proof "the *only*
evidence that the job happened as claimed", and `Docs/04` §7 has an administrator reviewing it in a
dispute; a record that might point at nothing is not evidence, and the moment somebody discovers
that is the moment it is needed. **The cheaper design — record the client's word for it — is the one
this ticket exists to refuse.**

Closing the second direction would have meant writing something when the URL is issued, and
SHIP-114 argues at length why nothing is written then. An unreferenced object is not a defect: the
bucket has no public read path, the keys are unguessable, and UUIDv7 keys were chosen so that a
lifecycle rule over them is expressible.

#### Which cost `internal/platform/storage` its strongest sentence, and the narrowing is deliberate

SHIP-114 wrote "this package makes no request to the object store, ever". `S3.Stored` now makes
one — a pre-signed HEAD, spent immediately, against an object this platform named. **The rule that
was always doing the work is *no transfer through this service*, and a metadata request carries no
body in either direction.** `doc.go` and `s3.go` both say so now, and `S3.PresignDownload` arrived
with it: the four lines SHIP-114 said a download signer would be, now that a consumer exists to
guard it.

A download URL signs `host` alone, which is the *opposite* of the upload and is right for the same
reason: an upload URL binds `content-type` and `content-length` because the request it authorises
carries a body the platform has not seen, and a GET carries none.

#### The record stores what the store reported, and that is what closes SHIP-114's open hole

SHIP-114's mutation testing found that reducing the signer to `host` alone left `internal/delivery`'s
entire test package green — correct, since the domain stubs the port, but it meant the claim "the
platform's upload limits are enforced" had **no guard inside the domain at all**, only
`internal/platform/storage`'s live tests and `70-delivery.sh`.

**It is closed, and by a different guard rather than the same one moved.** `content_type`,
`content_length` and `etag` on a `proofs` row are what the *store* answered with, and `VerifyProof`
measures them against `UploadPolicy` before recording anything. So the limits are now applied to the
object that exists, at the moment it becomes evidence, whatever route it took into the bucket.
`TestProofOverTheSizeLimitIsRefusedEvenThoughItReachedTheBucket` and
`TestProofOfAnUnacceptedTypeIsRefusedEvenThoughItReachedTheBucket` are the two that fail if that
check is removed, and neither depends on the signer.

`etag` is recorded and returned to nobody. A pre-signed PUT stays usable until it expires, so the
holder of an upload URL can overwrite the object inside that window; nothing can revoke the URL, so
the answer is detection rather than prevention. SHIP-155's viewer is where an administrator would be
shown a mismatch.

#### Proof follows the milestone, not the job — and SHIP-112 is what settles it

A photograph is evidence *for a recorded claim*, so it hangs off the `milestones` row. The
consequence worth knowing is that **an absorbed milestone keeps its proof.** SHIP-112 keeps a late
milestone's row and deliberately does not move the job, so a photograph taken at dawn in a yard with
no signal is kept, and appears on a timeline at the time the driver acted rather than at the time
the phone found a tower. Proof hung off the *job* would have had to answer "which one is current"
from the arrival order, which is the one order `Docs/02` §3.1 says not to show a customer. The other
direction is unchanged: a premature milestone is still refused and rolls back whole, and its proof
rolls back with it.

**`job_id` is on the row as well, and it cannot drift.** `fk_proofs_milestone` is a *composite* key
over `(milestone_id, job_id)`, so "this proof's job is its milestone's job" is a fact PostgreSQL
checks. Two separate foreign keys would each hold while permitting a row that puts a photograph of
one delivery on another customer's timeline — which is the shape a mistyped identifier produces, and
which reads perfectly. `services/core/migrations/proofs_test.go` is where that is demonstrated,
because no Go path in this service can produce it, and that is the point rather than a gap.

#### Who may read proof, and the two readers who deliberately may not

| Reader | Gets | Why |
|---|---|---|
| The customer who owns the job | The record, and a short-lived signed URL per photograph | `Docs/01` §4.4's acceptance measure is theirs, and SHIP-133 is the screen |
| The provider it was awarded to | **The same thing** | They took it. Being unable to see what they submitted makes a dispute unanswerable from their side |
| The assigned driver | Nothing, yet | Not distrust: **nothing they could do with it exists.** SHIP-122 is the first ticket that has a driver capture proof at all, and it is blocked on the idempotency scope. Their link is forwardable through whatever channel the provider used and lives seven days, and a proof photograph identifies an address and a recipient — widening that credential before there is a need is a cost with no benefit. **SHIP-122 revisits it** |
| An administrator | Nothing, and cannot | There is no administrator. `ck_users_role` refuses `'admin'` and admin sign-in is SHIP-147. SHIP-155 is that reader, and what it needs from here — the download signing — is now built |

Neither party gets a reduced view, and that is a decision: a photograph is not a field that can be
redacted, and the two of them are looking at the same object when they disagree about a delivery
(`Docs/04` §7). Anybody else gets the 404 a job that does not exist gets. **The download URL is
issued only after the check** — `TestNobodyElseMayReadProofAndNoURLIsSignedForThem` asserts the
signer was never *called* for a refused reader, rather than merely that the answer was empty,
because a credential minted and then discarded has already left the building.

#### The route could not be `GET /v1/jobs/{id}/proof`, and that will cost the next ticket too

`GET /v1/jobs/open/{id}` (SHIP-83) puts a literal in the `{id}` position. It and **any**
three-segment `GET /jobs/{id}/<literal>` both match `/jobs/open/proof` with neither more specific,
so Go's `ServeMux` refuses the pair at registration and the process does not start.
`POST /jobs/{id}/proof-uploads` is unaffected only because the other route is a `GET`.

So the delivery domain took a shelf of its own: `GET /v1/jobs/{id}/delivery/proof`. That turns out
to be worth having rather than a workaround — SHIP-116's exception, SHIP-133's tracking view and a
milestone timeline would each have hit the same wall, and each now has somewhere to go that
composes. **It is recorded here for whoever owns `/jobs/open/{id}`**, because it is the shape that
will keep costing tickets an hour: it is the one route in the manifest that squats a variable
position with a literal, and the collision is invisible until a `GET` is added beside it.

#### What a retry does, and why `proofs` carries no idempotency key of its own

The milestone's key is the whole of it. Proof is written in the same transaction as the milestone it
proves, so a retry that outlives its cached response finds the milestone already recorded under
`uq_milestones_idempotency` and returns before the proof insert is attempted. A key column here
would be a second copy of a guarantee that already holds.

`uq_proofs_object_key` is separate and is access control rather than tidiness: one object is
evidence for one recorded claim, and the same key on two milestones would put a picture of one
delivery against another. Enforced by the index rather than by a read-then-write, because two
concurrent recordings would both read no row.

#### What was needed of `internal/config` beyond SHIP-15p: nothing, and one field worth asking for

All nine fields were already there and the download reuses `PresignTTL`. **A separate, shorter
`STORAGE_DOWNLOAD_TTL` is the field to ask for**, and the reason it is not urgent is that reusing
the upload's lifetime errs long rather than short: an upload TTL has to outlast a phone finishing a
slow PUT, while a download only has to outlast an image rendering. One number serving both makes the
read link longer-lived than it needs to be, and every one of those is a link to a photograph of
somebody's front door. Not a domain branch's edit; it belongs in whatever the next prep ticket
collects.

The metadata request's timeout and its own signing window are adapter constants rather than
configuration, deliberately: nothing is ever handed the URL `Stored` signs for itself, so that
window bounds one in-process round trip rather than describing a policy about what a credential may
reach.

#### Mutation testing: eleven mutations, eleven caught

Each was reverted immediately and the revert confirmed with `git diff`.

| Mutation | Result |
|---|---|
| Believe the client — record proof without asking the store | **Caught** — `TestProofIsRefusedWhenNothingWasUploaded`, and `70-delivery.sh`'s first SHIP-115 check against the real store |
| Treat a store that errors as "no such object" | **Caught** — `TestAStoreThatFailsIsNotAMissingPhotograph`, and the live `TestStoredRefusesAWrongCredentialRatherThanCallingItMissing` |
| Drop the stored-size check | **Caught** — `TestProofOverTheSizeLimitIsRefusedEvenThoughItReachedTheBucket`. **This is SHIP-114's recorded hole, now covered inside the domain** |
| Drop the stored-content-type check | **Caught** — `TestProofOfAnUnacceptedTypeIsRefusedEvenThoughItReachedTheBucket` |
| Record the client's stated type and length instead of the store's | **Caught** — the row assertion in `TestProofIsLinkedToTheJobAndTheMilestoneItProves`, and `70-delivery.sh` reading `image/jpeg 48` out of the table |
| Remove `keyBelongsToJob`, so one job's object can be proof on another | **Caught** — three ways: the domain, the wire table, and `70-delivery.sh` |
| Answer `mayReadProof` true for any caller | **Caught** — `TestNobodyElseMayReadProofAndNoURLIsSignedForThem`, both refused readers, and two verify checks |
| Sign the download URLs before the authorisation check rather than after | **Caught** — that same test's second assertion, which counts what the signer was asked. **Nothing else fails**, which is why the assertion is written against the signer rather than against the response |
| Split `fk_proofs_milestone` into two single-column foreign keys | **Caught** — `TestProofCannotNameOneJobAndAMilestoneOnAnother`, and the raw-SQL forgery in `70-delivery.sh` |
| Make `uq_proofs_object_key` a plain index | **Caught** — and loudly, by ten tests rather than one: `ON CONFLICT (object_key)` infers *that* index, so without it no proof can be recorded at all. The uniqueness is load-bearing for the insert as well as for the rule |
| In the adapter, fold every non-2xx into "no such object" | **Caught** — `TestStoredRefusesAWrongCredentialRatherThanCallingItMissing`, against the real store with a wrong secret. A separate guard from the domain-level one above, and the one that matters: a rotated key would otherwise tell every driver their photograph had failed to upload |

`make verify` went from 493 checks to 507 across the same 13 sections, all fourteen in
`scripts/verify/70-delivery.sh`: a milestone naming an object nobody uploaded is refused, the upload
and the recording then succeed, the row names the job and the milestone and holds what the store
reported, the composite key refuses a forged pair **in raw SQL**, the customer reads it, the URL is
at the store rather than at this API, it fetches the bytes, the same object is refused unsigned, the
awarded provider reads the same record, a stranger gets what a missing job gets, an anonymous read
is refused, one object cannot be proof twice, and another job's key is refused with the field named.

### SHIP-116 — the exception is evidence, not a hole in the rule

`proofs` gains `exception_reason` and loses four `NOT NULL`s (migration `000604`), and the milestone
request's `proof` object gains a second field. A milestone may now be evidenced by a photograph
**or** by one of `Docs/01` §4.4's three reasons there is none — and never by both, and never by
neither.

#### One table, because "never both" is expressible in no other shape

The obvious design is a second table beside `proofs`, and `000603`'s own header had already rejected
it: "exception_reason, and object_key becoming nullable with a CHECK that exactly one of the two is
present". The reason is that **no constraint spans two tables.** A photograph and a reason are
alternatives, so the interesting rule is not that either may exist but that exactly one does, and
with two tables that rule can only ever be application logic — which is precisely what `CLAUDE.md`
says an invariant must not only be.

```sql
CHECK ((num_nonnulls(object_key, content_type, content_length, etag) = 4 AND exception_reason IS NULL)
    OR (num_nonnulls(object_key, content_type, content_length, etag) = 0 AND exception_reason IS NOT NULL))
```

`num_nonnulls` rather than four `IS NOT NULL` conjunctions because the failure it guards is a
*partial* photograph — a key with no entity tag reads as evidence to every query that only selects
`object_key`, and `000603`'s four `NOT NULL`s were the only thing refusing it. Dropping them without
counting would have been the one regression this migration could have introduced silently.

It is also **not** the whole of `CLAUDE.md`'s invariant and does not pretend to be. This makes an
evidence row coherent; a delivered milestone requiring one is a rule about milestones, and that is
SHIP-118.

#### The vocabulary is closed, and there is deliberately no `other`

`recipient_objected`, `camera_unavailable`, `location_unsafe` — `Docs/01` §4.4's own three,
paired with the `CHECK` in both directions by
`TestProofExceptionConstraintMatchesTheGoConstants` (`Docs/10` §3.4). A free-text reason nobody
can group is a moderation queue nobody can triage (`Docs/04` §5), and the driver's own words are not
lost by leaving it out: `milestones.reason` is optional, five hundred characters and one row away.
"The recipient asked me not to photograph their door" goes there, *beside* a selection rather than
instead of one.

The stored form is the wire form, which is `ActorType`'s arrangement rather than `Milestone`'s.
Milestones store `Docs/02` §1's exact strings because a document fixes them; no document fixes
these, so a second spelling would be a translation table with nothing on the other side of it.

#### An exception may stand behind any milestone, and the narrower rule is wrong

The first design restricted it to `Delivered`, on the reasoning that a photograph is only *required*
there. It was reversed before anything was written. `Docs/01` §4.4's three reasons are about
**capture** — a camera, a recipient, a place — and not one of them knows which milestone is being
recorded. A driver who cannot photograph a pickup is in exactly the position the paragraph is about,
and refusing the exception there leaves them choosing between recording nothing and claiming
something they do not have.

What is specific to `Delivered` is that evidence is required at all. That is SHIP-118's rule, and
keeping the two apart is what let this ticket be demonstrated on an ordinary milestone — exactly as
SHIP-115 demonstrated a photograph on `en_route_to_pickup`.

#### Nothing is asked of the object store, and nothing is signed for a reader

An exception has no object, so the recording path never calls `Stored` and the read path never calls
`PresignDownload`. Both are **assertions about a call rather than about an answer**, so both tests
read what the stub was asked rather than what came back — `recordingObjects.lookups` and
`.downloads`. That shape is SHIP-115's finding reused: `internal/platform/storage` will sign a URL
for any key it is handed and says so, so a service that asked it about an exception would receive a
perfectly good credential naming an object that does not exist, and a response assertion could pass
while that happened.

The consequence for a client is one field: a record carries `exception_reason` **or** the
photograph's five, and `download_url` is absent rather than empty. A zero `content_length` is a
statement about a photograph that does not exist; an absent field says nothing.

#### What was assumed about SHIP-117 and X-6, neither of which is this ticket's

| Ticket | What it needs from here | What was built for it |
|---|---|---|
| **SHIP-117** — an exception-completed job enters the moderation queue | To ask which jobs carry one | `idx_proofs_exception`, partial on `exception_reason IS NOT NULL`. **The flag itself is deliberately not here**: whether a job is queued for review is a fact about the *job* and belongs with the queue, which is what `000603` already said. The queue's own question is a join to the `Delivered` milestone, which SHIP-118 makes recordable |
| **X-6** — may an exception-completed job auto-complete under `Docs/02` §6.1? | Not to have been decided | Nothing here presumes an answer. The exception is a durable fact joined to the milestone that carries it, so SHIP-119 can branch either way and no column changes in either direction |

Until SHIP-117 exists the only trace an exception leaves outside the table is a **warning** log line
from the handler — the one place in it that is about operations rather than reconciliation. A
delivery with no photograph is exactly what somebody should be able to find in a log while the queue
is being written.

#### What was needed of `internal/config`: nothing, and run 2's request stands

No new setting. The exception path touches no limit, no lifetime and no bucket — which is itself the
argument for having put the reason vocabulary in a `CHECK` rather than in configuration: it is
paired with a Go constant list by a test, and `Docs/10` §3.4 is explicit that this is how an
enumeration is held.

**The `STORAGE_DOWNLOAD_TTL` SHIP-115 asked for is still the right request**, and this ticket
narrows what it is for rather than widening it: an exception is now one of the records a customer's
tracking screen renders, and it needs no URL at all, so the population of long-lived read links is
smaller than it was but no shorter-lived.

**One request that is not `internal/config`'s and is recorded because somebody will reach for it.**
The three reasons a driver *picks from* are a vocabulary; the **words shown beside them** in the
picker are copy, and `Docs/06` §5.3 puts copy server-side because Flutter has no over-the-air update
path. No endpoint serves them, and SHIP-131 — the camera-permission fallback, which depends on this
ticket — is the first screen that needs them. Compiling three English strings into Dart is the
cheap answer and the one that needs a store review to correct.

#### Mutation testing: seven mutations, seven caught

Each was reverted immediately, by copy rather than by `git checkout`, and confirmed with `git diff`
and a checksum.

| Mutation | Result |
|---|---|
| Make `ck_proofs_photograph_or_exception` always true, so a row may carry both or neither | **Caught** — `TestEvidenceIsAPhotographOrAReasonAndNeverBothOrNeither`. `70-delivery.sh` asserts the same rule in raw SQL against the running database |
| Write `num_nonnulls(...) > 0` in place of `= 4`, so half a photograph is a photograph | **Caught** — that test's `half a photograph is refused` subtest, and **nothing else**. It is the mutation this migration could most easily have shipped with: every ordinary path writes four columns, so no Go test and no verify check can reach it |
| `ProofExceptionReason.Valid` always true, so any string is a reason | **Caught** — three tests, and the interesting part is *how*. The domain test now fails with `ck_proofs_exception_reason` in the message rather than a validation error, which is the two layers behaving exactly as `Docs/10` §4.6 describes: the database still refuses the row, and what the Go check buys is a 422 naming the field instead of a constraint name in a 500 |
| Remove **both** Go refusals of a photograph-and-a-reason together | **Caught** — but by one test rather than three, and this is the mutation worth reading. `TestEvidenceThatIsNotOneOrTheOtherIsRefusedInsideThePackage` fails. `TestAPhotographAndAReasonTogetherAreRefused` and the wire table both keep passing, because `ck_proofs_photograph_or_exception` refuses the row on the way past. The rule holds; what is lost is the ability to say which field was wrong |
| Treat `"proof": {}` as no evidence rather than as a refusal | **Caught** — the wire table's `neither` case and SHIP-115's `TestProofOnTheWireIsRefusedByTheCasesAClientCanCause`. The failure it prevents is the quiet one: a client that meant to send a photograph, silently recorded as having none |
| Sign a download URL for an exception row | **Caught** — `TestAnExceptionIsReadBackWithNoDownloadURLAndNothingIsSignedForIt` by the assertion that counts what the signer was asked, plus both wire tests. The signer-call assertion is the one that would still fail if the URL were minted and then discarded |
| Ask the store about the object an exception does not name (`req.Proof != nil` in place of `proofKey != ""`) | **Caught** — both wire tests, and note that the *domain* `lookups` assertion does not fail, because the mutation is in the handler and the domain test calls the service directly. That is the division working: the handler owns the ordering, so the handler's tests are what hold it |

`make verify` went from 507 checks to 514 across the same 13 sections, all seven in
`scripts/verify/70-delivery.sh`: an exception is recorded against a real MinIO with no bucket
interaction at all, the row holds the reason and four NULLs, the table refuses both-and-neither in
raw SQL, the customer and the awarded provider each read the reason with no URL, and both-together,
neither and an unpublished reason are each refused with the field named and the three published.

### SHIP-118 — the invariant stops being intended

`Delivered` is recordable, and refused when it carries neither a photograph nor a reasoned
exception. `delivery.Jobs` gains `MoveToDelivered`, `Service.RecordMilestone`'s blanket refusal
becomes a condition, and migration `000605` adds a deferred constraint trigger.

**This is the ticket the four before it were for.** SHIP-114 put a photograph in a bucket, SHIP-115
made it evidence, SHIP-116 gave a delivery that could not be photographed something to say instead —
and none of them could reach `Delivered` at all. `CLAUDE.md` has listed "Delivered requires photo
proof or a recorded exception reason — never neither" as an invariant since before any of it existed,
and `Docs/02` §3's own line has had no enforcement since the status model was written.

#### The missing port method was half the enforcement, and it is deliberately given up

`cmd/api/routes_delivery.go` said it plainly: "a method here would be a way to reach that status
without either… having no method behind it as well means the refusal cannot be removed by editing
one file." That was right while nothing could be captured, and it is the wrong shape now — a status
nobody can reach is not a rule, it is an absence.

So the second layer is a real one instead:

```sql
CREATE CONSTRAINT TRIGGER milestones_delivered_has_evidence
    AFTER INSERT ON milestones
    DEFERRABLE INITIALLY DEFERRED
    FOR EACH ROW WHEN (NEW.milestone = 'Delivered')
    EXECUTE FUNCTION milestones_delivered_needs_evidence();
```

**Deferred, because the evidence points at the milestone and can therefore only be written second.**
An ordinary `AFTER INSERT` trigger fires between the two statements and refuses every honest delivery
there is — which is not a hypothetical: making it `NOT DEFERRABLE` fails nine tests. Deferring moves
the check to `COMMIT`, which is the first moment the question is answerable at all.

`WHEN (NEW.milestone = 'Delivered')` keeps the other four free of it. `Docs/01` §4.4 requires a
photograph of one moment, not of five.

#### Two layers, and each one is for something the other cannot do

| Layer | What it is for |
|---|---|
| `Service.RecordMilestone` | The answer a client can act on: `409 delivery_proof_required`, pointing at the camera and the exception path beside it. Nothing is written — not even a row the transaction later unwinds |
| `000605`'s trigger | The rule surviving a writer that never read that function. SHIP-121 gives the driver's portal a milestone endpoint, SHIP-113 rewrites the switch deciding what an administrative conflict does with one, and `cmd/worker` already applies transitions on a timer |

Removing the Go check leaves the invariant standing and the API broken — a `500` with a constraint
name where a `409` should be, which is exactly what `Docs/10` §4.6 says a constraint is a bad
explanation for. That is not a hypothetical either; it is what the mutation run produced, and four
named tests report it.

**The trigger has no actor exemption**, and that is a decision rather than an oversight. `Docs/02` §3
lets an administrator record a delivery "acting with an audit reason"; `CLAUDE.md`'s invariant has no
"unless" in it, so an administrator recording one needs evidence like anybody else. Two SHIP-110
tests recorded a `Delivered` milestone as a convenient arbitrary value and now record `In transit`
instead, each with a line saying why.

#### The ordering with SHIP-112's absorption, which is where this could have gone wrong

The check sits **in front of the insert**, and since SHIP-112 that placement carries weight it did
not have before. Absorption *commits* a milestone whose move was refused — so a `Delivered` reaching
the switch on a job that has already been there would be kept rather than unwound.
`TestAbsorptionCannotReachDelivered` drives precisely that job, and it is one of the four tests that
fail if the condition is removed.

The other direction is unchanged and worth stating: a **late** `Delivered` that does carry evidence
is now absorbed like any other late milestone, keeps its photograph or its reason, and moves nothing.
That is `Docs/02` §3.1 read exactly, and it is what SHIP-115's decision to hang proof off the
milestone bought.

#### What this does *not* build: the recipient name and the delivery note

**Two documents are ahead of the code here, and the gap belongs to SHIP-123 rather than being a
defect in this ticket.** `Docs/01` §4.4 requires a delivered job to carry a **recipient name** and a
**delivery note** as well as proof, and `Docs/02` §3 repeats it, naming `01` §4.4 as authoritative
for the field set. **No column holds either.** `milestones` carries `job_id`, `milestone`,
`actor_type`, `actor_id`, `reason` and the two clocks; `proofs` carries the object metadata and
`exception_reason`; neither table has a field for a recipient or for a note about the delivery
itself. **This ticket opens `Delivered` without them**, and that is recorded here rather than
resolved quietly:

- SHIP-123's *Done when* is "recipient name, note, and proof captured; portal becomes read-only
  after", and it **depends on SHIP-118**. The field set is therefore behind this ticket in build
  order, and capturing it here would be building SHIP-123's API half inside this one.
- What SHIP-118 is judged on is the clause `CLAUDE.md` calls an invariant, and that clause is closed
  end to end.
- `milestones.reason` is a plausible home for the delivery note and was **not** quietly reused for
  it. It is the actor's optional note on any milestone; making it mean a second, specific thing on
  one milestone is exactly the one-column-two-meanings `Docs/10` §3.3 already refuses, in the row
  holding the actor clock and the server clock to two explicit columns rather than one.

Whoever takes SHIP-123 should expect to add both columns and to make them required for `Delivered`,
beside the evidence rule rather than instead of it. **This is a gap with an owner, not an undecided
question**, so a wave reconciliation should carry it as documentation ahead of code against
SHIP-123 rather than as a §9 recommendation waiting on somebody's ruling.

#### SHIP-117 and X-6 are unblocked and untouched

Both now have something to work on that did not exist an hour ago: **an exception-completed job**.
The join is `proofs.exception_reason IS NOT NULL` against the job's `Delivered` milestone, indexed by
SHIP-116's `idx_proofs_exception`, and `70-delivery.sh` asserts it end to end so that neither ticket
finds the fact missing.

Neither is answered here. Nothing flags a job for moderation and nothing completes one; X-6's
question — whether a job completed through the exception path may auto-complete under `Docs/02` §6.1
— is a decision for operations, and the schema branches either way without a column changing.

#### What was needed of `internal/config`: nothing

No setting, no limit and no lifetime. The two requests standing are still SHIP-115's
`STORAGE_DOWNLOAD_TTL` and SHIP-116's note that the *words* beside the three exception reasons are
copy rather than vocabulary, and belong server-side rather than compiled into Dart.

#### Mutation testing: four mutations, four caught

Each was reverted immediately, by copy rather than by `git checkout`, and confirmed with `git diff`
and a checksum.

| Mutation | Result |
|---|---|
| **Remove the refusal itself** — `Delivered` with neither is recorded | **Caught, by four named tests**, and the way it fails is the finding. `TestDeliveredWithNeitherProofNorExceptionIsRefused` and `TestAbsorptionCannotReachDelivered` both report `a delivered milestone needs photo proof or a recorded exception` **from the COMMIT**, and `TestADeliveryWithNothingBehindItIsRefusedAtTheWire` gets a `500` where a `409` belongs. The invariant holds; the API stops being usable |
| `Recording.hasEvidence` always true | **Caught** — ten tests, most of them nothing to do with deliveries: every milestone then tries to write an evidence row it does not have, and `ErrEvidenceNotCoherent` refuses it. A mutation that fails loudly and far from its own subject, which is the shape a shared helper produces |
| The trigger is never created | **Caught** — `TestTheDeliveredRuleIsAlsoTheDatabases` in the domain and `TestADeliveredMilestoneCannotBeWrittenWithoutEvidence` in `migrations`, and **nothing else**. Every path through the service still refuses, which is exactly why the second layer needs a test that goes round the service |
| The trigger is `NOT DEFERRABLE` | **Caught** — nine tests. Evidence points at the milestone, so it is written second; a trigger that fires between the two statements refuses every honest delivery. The deferral is load-bearing rather than tidy |

`make verify` went from 514 checks to 520 across the same 13 sections, all six in
`scripts/verify/70-delivery.sh`: a delivery with neither is refused with the job unmoved and nothing
written, the same row is refused in raw SQL at `COMMIT`, a delivery evidenced by a reasoned exception
is accepted and moves the job through the transition guard leaving one history row, the
exception-completed job is findable by the join SHIP-117 and X-6 both start from, and a delivery
evidenced by a photograph is accepted the same way.

### SHIP-136 — the seam SHIP-135 left, used as intended, and the two lines of §4.5 that could not be met

Five points, and the ticket that gates all thirteen of M5. It opens `jobs`, `bidding` and `delivery`
at once, which is why it had been deferred three times and why it ran alone.

**Nine events, and `internal/events` was not edited to add one of them.** `internal/bidding/events.go`
and `internal/delivery/events.go` are files exactly like `internal/jobs/events.go`: one `init` calling
`events.Register`, the payload struct as the schema, the emit helpers beside it. That is SHIP-135's
prediction holding to the letter — "SHIP-136 adds bidding's and delivery's from files exactly like
it" — and it is the second mechanism in this repository to be demonstrated rather than asserted
(`internal/boundaries`' pre-seeded infrastructure list was the first). `shipper.bid` and
`shipper.delivery` carry traffic for the first time; the topic set was already right.

| Aggregate | Event | Emitted from |
|---|---|---|
| `bid` | `bid.placed` | `PlaceBid`, on the branch that created a row — a retry answered from the record emits nothing |
| `bid` | `bid.revised` | `ReviseBid` |
| `bid` | `bid.withdrawn` | `WithdrawBid`, and **not** on a withdrawal of something already withdrawn |
| `bid` | `bid.countered` | `CounterOffer`, on the counter, naming the offer it displaced |
| `bid` | `bid.accepted` | `AwardBid`, after the transition — an award is not an award until the job has moved |
| `bid` | `bid.rejected` | one per offer the SHIP-93 sweep closed |
| `delivery` | `delivery.driver_assigned` | `AssignDriver`, from `granted`, only when a row was written |
| `delivery` | `delivery.milestone_recorded` | `RecordMilestone`, from `recorded`, **whether or not the job moved** |
| `delivery` | `delivery.proof_recorded` | the evidence, photograph and reasoned exception alike |

**`Docs/01` §4.5 has six lines and two of them cannot be met, which is stated rather than quietly
dropped.** Enumerating the section against what the platform actually does was the first hour of the
ticket and is most of its value.

- *Account verification completed or rejected* — **not emittable.** `internal/profiles` holds
  `doc.go` and nothing else; there is no verification state change in the service to emit from.
  Whichever ticket writes it writes its event, from its own `events.go`.
- *Job published* — already emitted, as `job.status_changed` `Draft → Open`, since SHIP-57.
- *New bid, counter-offer, withdrawal* — the three new bid events above.
- *…or bid expiry* — **not emittable.** `bidding.StatusExpired` is declared, `ck_bids_status` accepts
  it, and **nothing writes it**. **SHIP-89** is the ticket that starts, and its *Done when* already
  reads "bids expire on their own terms and emit an event". Registering a schema for it here would
  have put a line in `events_golden.txt` describing a payload no code marshals, which reads as
  covered; instead `scripts/verify/61-bidding.sh` asserts that no bid is `Expired`, so the day one is
  the day that check fails and names SHIP-89.
- *Bid accepted or job cancelled* — `bid.accepted`, plus `bid.rejected` for every offer the same
  transaction closed, plus `job.status_changed` for the cancellation.
- *Delivery status changes* — see below.
- *Dispute opened or resolved* — **half met, and by the job rather than by a dispute event.**
  `admin.RaiseDispute` (SHIP-163) moves the job to `Disputed` through the guard, so
  `job.status_changed` fires. There is no `dispute` aggregate and a fourth aggregate is a decision
  recorded in `internal/events` rather than something a domain track does; `internal/admin` was also
  outside this ticket's ownership. *Resolved* is SHIP-164 and does not exist. **Whoever writes
  SHIP-164 should decide whether a dispute is a fourth aggregate**, because that is a topic and a
  partition count, and partitions cannot be reduced.

**The delivery events exist because the job's status does not carry what `Docs/01` §4.4 asks an actor
to record, and the absorbed milestone is the sharp case.** Every milestone that *moves* the job
already emitted `job.status_changed`, from `jobs`, inside this domain's transaction. What emitted
nothing at all was everything that writes a row and moves nothing — the repeated pickup attempt of
`Docs/02` §5, and **SHIP-112's absorbed late milestone**, which `Docs/02` §3.1 requires to be
"accepted… without moving the job backwards" and which therefore leaves no `job_status_history` row
and no status event. A driver's queued `Picked up` syncing after `In transit` was invisible to
everything downstream. `delivery.milestone_recorded` carries `job_moved`, which is the one fact a
consumer cannot derive: when it is false there is no `job.status_changed` to correlate with, and a
consumer waiting for one would wait for ever.

**Three things are deliberately not in a payload, and each is a rule rather than a preference.** The
**driver's name and mobile number** (`Docs/01` §5.1 — an event travels onto a topic with seven days
of retention and into every consumer there will ever be; a consumer with a reason to know who is
driving reads the row). The **object key of a photograph** (who may look at it is
`Service.ProofFor`'s decision, made after an authorisation check and issued as a short-lived signed
URL — not a decision a consumer of a topic is in a position to make). And **anything of the job
beyond its identifier**, which keeps `Docs/01` §4.3's budget rule structurally out of reach.
`amount_cents` is not an exception to the last: it is the provider's own number, or on a counter an
amount the customer deliberately offered to that provider, which
`GET /v1/jobs/{id}/bids/{bid_id}/chain` has served since SHIP-88.

**The half of the *Done when* that says "from the domain, not the API layer" now has a test, and it
had none.** `cmd/api/events_domain_test.go` parses every non-test Go file in the service and refuses
an `events.New` or an `Emit` outside a domain — **or inside a domain's `http.go`**, which is the
shape the rule is really about: `Docs/10` §4 puts handlers in the domain, so an emit written there is
inside the domain package and is still the API layer. It lives in `cmd/api` for the reason
`events_golden.txt` does, and a second test holds its allow-list against `internal/boundaries.Domains`
so that infrastructure cannot be added to it to unblock something. Nothing else in the build checks
this: an event emitted from a handler writes the right row, with the right payload, onto the right
topic, and passes every other test in the ticket.

**One store method changed shape and the reason is worth keeping.** `rejectCompeting` returned
nothing and now returns the rows it closed, via `RETURNING`. Each closed offer is a different provider
to tell, so the award emits one `bid.rejected` per row — and the rows have to come from the statement
that closed them. A second `SELECT ... WHERE status = 'Rejected'` would read whatever is rejected
*now*, including offers closed by some earlier act, and attribute them all to this transaction. That
is the mutation that survived, below.

#### Mutation testing: nine mutations, eight caught and one survivor that produced a test

Each was applied to a file copied aside first, reverted from the copy rather than with
`git checkout`, and confirmed with `git diff` **and** `shasum`.

| Mutation | Result |
|---|---|
| Delete the `bid.placed` emit | **Caught** — seven tests in `internal/bidding/events_test.go`, six of them about other verbs, because every fixture places before it revises |
| Move the withdrawal emit into `internal/bidding/http.go` (compiling) | **Caught** — `TestOnlyADomainEmitsADomainEvent`, and nothing else in the build. The row, the payload and the topic are all correct under this mutation |
| Emit the evidence where the `proofs` row is written, before the milestone event | **Caught** — two tests. Both events key on the job, so they share a partition and the order is a guarantee; the mutation puts `delivery.proof_recorded` ahead of the milestone it names |
| `budget_cents` on a provider-visible payload | **Caught three ways** — `TestEventCatalogueMatchesGolden`, SHIP-135's `TestNoDomainEventCarriesABudget`, and the domain's own closed key set. The second is the one that mattered: it was written a wave before this domain had an event |
| Delete the `bid.rejected` sweep loop | **Caught** — three tests, one of which refuses to pass vacuously (`the exchange emitted no bid.rejected, so this test proves nothing`) |
| `job_moved` hard-coded true | **Caught** — `TestAnAbsorbedMilestoneEmitsWithJobMovedFalse`, which exists only because of `Docs/02` §3.1 |
| Drop the `created` guard, so a repeated nomination emits again | **Caught** — the assignment test's second half |
| Move `bid.accepted` from step 5 to step 3 of the award | **Survived, and it is not a defect.** Both positions are inside one transaction, which either commits whole or rolls back whole, so no consumer can tell. **The finding is the general one**: within a transaction the position of an emit is unobservable, so the ordering comments in these files are documentation rather than tested properties — *except* between two events on the same aggregate, where outbox order is partition order, which the evidence mutation above shows is tested |
| `rejectCompeting` reports its rows from a second `SELECT` instead of `RETURNING` | **Survived**, and it produced `TestTheAwardEmitsOnlyForTheOffersItItselfClosed`. It survived because nothing but the sweep writes `Rejected` today and a job is awarded once, so the two queries agree on every fixture — they stop agreeing the first time anything else closes an offer, and `Rejected` is `Docs/02` §4's "an offer the customer declined". The new test inserts an already-closed offer directly and asserts the award emits nothing about it. Re-run with the test in place: **caught** |

**A note on "emit outside the transaction", which the brief asked for and which turns out to be
unconstructible from a domain.** Neither `bidding.Service` nor `delivery.Service` holds a connection —
every method takes the caller's `db.Runner`, which is the shape `Docs/10` §3.2 asks for — so there is
no second runner in scope to emit through. The only place the mistake can be made is the composition
root or a handler, and that is what the source guard above and the existing `ErrNotInTransaction`
checks cover between them. The rollback property itself is held by a test in each domain that fails a
transaction after the state change and asserts the outbox is empty.

**`make verify` went from 555 checks across 13 sections to 570**, in three files. Six in
`scripts/verify/61-bidding.sh` (all six bid events from real endpoints, `bid.expired` absent for a
stated reason, every event keyed on the bid its payload names, the award's three events across two
domains, and no budget on a job that has one); seven in `scripts/verify/70-delivery.sh` (the
assignment with neither the driver's name nor their number, a milestone event reporting
`job_moved: false`, one event per milestone row, both kinds of evidence, no object key, and the
evidence after the claim); and two in `scripts/verify/80-notifications.sh`, which takes them off
`shipper.bid` and `shipper.delivery` with Kafka's own console consumer — every id this run published,
on the topic its aggregate names, all nine types at schema version 1, and no budget in the bytes a
consumer receives.

**That section moved, and the reason is a hazard the next Kafka assertion will meet.** The SHIP-135
section **deletes `shipper.delivery`** to demonstrate that a topic somebody created by hand with the
wrong partition count is reported rather than repaired, and its own header said that was free
"because no code publishes to it yet". **This ticket ended that.** So the SHIP-136 section sits
*above* SHIP-135's rather than below it, and reads the topic before the deletion destroys it. There
is no longer a topic in the set that nothing publishes to: **a section that breaks a topic now has to
run after every section that reads it**, and both headers say so.

The topic assertions are fenced **by event id**, taken from the outbox before the worker starts, and
compared as a **subset** — `shipper.bid` and `shipper.delivery` are not emptied by the harness and are
shared with every worktree on the machine. That is SHIP-135's lesson applied without having to
rediscover it.

## 4. Partly done — do not treat these as finished

| Ticket | Exists | Missing |
|---|---|---|
| **SHIP-149** | `audit_log` table, append-only triggers, tests | The Go write helper its title names |
| **SHIP-77** | The job detail screen, the derived timeline, the available actions | The transition history its *Done when* implies. "Full job detail with **status timeline**" — and no endpoint serves one, so the timeline is derived from the current status and refuses to date what it cannot date. See §9 |
| **SHIP-118** | `Delivered` recordable and refused without evidence, enforced in the domain and by `000605`'s deferred constraint trigger | The **recipient name** and the **delivery note**. `Docs/01` §4.4 requires a delivered job to carry both alongside proof and `Docs/02` §3 repeats it, naming `01` §4.4 as authoritative for the field set — and **no column holds either**. `milestones` has `job_id`, `milestone`, `actor_type`, `actor_id`, `reason` and the two clocks; `proofs` has the object metadata and `exception_reason`. See below |
| ~~**SHIP-134**~~ | ~~`outbox` table, `internal/events` writer~~ | **Closed.** The publisher landed — see §3. `outbox`, the writer and the drain are all in place; what remains is SHIP-135's topics and schema and SHIP-136's emission from the remaining domains, and those are tickets rather than a gap in this one |

**SHIP-65 has left this table.** Its *Done when* — "returns full job including budget" — was met
but for the budget for two waves, and SHIP-67 closed it with the column and the proof together.
§10's note that a ticket can be both done and partly done still stands, and SHIP-77 and SHIP-118 are
the two live examples of it.

**SHIP-118 is here on the ticket's own recommendation, and it differs from the other two rows in one
way worth stating.** SHIP-65's and SHIP-77's missing halves belonged to work that did not exist —
SHIP-65's budget column had no ticket until SHIP-67 was written for it, and SHIP-77's status-history
endpoint still has none. **SHIP-118's has an owner: SHIP-123**, whose *Done when* is "recipient name,
note, and proof captured; portal becomes read-only after" and which depends on SHIP-118 directly. So
the field set is *behind* this ticket in build order, and capturing it here would have been building
SHIP-123's API half inside SHIP-118. §3's SHIP-118 entry calls this "a gap with an owner, not an
undecided question", and that is why it sits here as documentation ahead of code rather than in §9 as
a recommendation waiting on a ruling.

**What it means in practice, said plainly so nobody reads `Docs/01` §4.4 as a description of today.**
A `Delivered` milestone can be recorded right now with a photograph and nothing else. That satisfies
`CLAUDE.md`'s invariant, which is the clause SHIP-118 was judged on and which is closed end to end;
it does not satisfy `Docs/01` §4.4's field set, which is three fields wide. **`milestones.reason` was
deliberately not reused for the delivery note** — it is the actor's optional note on any milestone,
and making it mean a second specific thing on one milestone is the one-column-two-meanings `Docs/10`
§3.3 already refuses. Whoever takes SHIP-123 adds both columns and makes them required for
`Delivered`, beside the evidence rule rather than instead of it. SHIP-123 is two hops back — SHIP-121
then SHIP-122 — and SHIP-121 is startable today (§6).

**SHIP-77 is the SHIP-65 shape exactly, which is why it is here rather than being argued about.**
The screen landed, it is named by a commit subject, it is in `Docs/11-done.txt`, and one clause of
its *Done when* belongs to work that does not exist yet — an endpoint over `job_status_history`,
which no ticket in the backlog adds. The ticket did the honest thing with what it had: rather than
invent dates or tick steps a job may legitimately have skipped, the timeline says where the job is
and says out loud what it does not know. **It stays in the done list either way** — §10 explains
that removing it would hard-fail `make status` rather than making the record more truthful.

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

Strict build order says the next ticket is the lowest-numbered open one, which is **SHIP-24** — and it is blocked on X-2, as are the other three M0 stragglers. The lowest-numbered ticket that can actually be started is **SHIP-56a**. **Twenty-six tickets have every dependency met: 21 code tickets worth 69 points, plus the five Track-X tickets worth 10.** Build order is a preference rather than a constraint at this point. The list below is computed from `Docs/09`'s dependency column against `Docs/11-done.txt`, not maintained by hand — and it is the **complete** startable set, because an earlier version of this table was a curated selection that read like a full list. Compute it from the dependency column and never from ticket order: `Docs/09`'s own header warns that dependencies point backwards with exactly three exceptions, and a tool that assumed otherwise would be wrong about SHIP-15c, SHIP-15e and SHIP-15m.

**Seven tickets left this table in wave 7 and seven arrived, so it holds at 26 tickets and falls from 84 points to 79.** The seven that left are SHIP-92, 100, 114, 120, 126, 129 and 168 — every one of them built. **The other seven the wave closed were never on it**: SHIP-15p was the pre-step, and SHIP-93, 94 and 95 were unblocked by SHIP-92 inside `ship-92-95`, SHIP-115, 116 and 118 by the ticket in front of them on `ship-114-116`. That is what a chain track does to this table, and it is why a wave that closes fourteen tickets leaves the queue the same length: work inside a branch never appears here, and everything the branch unblocks does. **The wave-7 dispatch brief said three left rather than seven**, having counted only the three whose rows carried a discussion paragraph; the set difference is the way to compute this, not the prose.

**The seven arrivals, and what unblocked each.** SHIP-101 came in behind SHIP-100; SHIP-117 and SHIP-119 behind SHIP-116 and SHIP-118; SHIP-121 behind SHIP-120; SHIP-127 behind SHIP-126; SHIP-130 and SHIP-133 behind SHIP-129 and SHIP-115. **Every one of the seven is M3 or M4** — one and six — and **five of the seven are client work**, on the Flutter app or the driver portal; only SHIP-117 and SHIP-119 are platform. That is the queue telling you where the next wave is, and it is a different answer from wave 6's: the platform half of M4 is nearly built and the screens that consume it are not.

**Seven of the 21 are struck, which is the useful signal in this table.** Dependencies being met is not the same as a ticket being startable: four are not demonstrable with the tooling or the accounts that exist, one is held behind a shared surface a domain branch must not edit, one is a decision, and one is the third category — every dependency met and unbuildable in fact. **It was six before wave 7, and exactly one arrived**: SHIP-101. Nothing was un-struck, because there was nothing left to un-strike — **the last two strikes to be lifted were both lifted before the wave and both tickets were then built in it**, which is the outcome that matters. SHIP-114 was un-struck by SHIP-15p putting an object store in the stack; SHIP-168 was un-struck by the wave-6 pass finding its reason had expired. **Both un-strikes were right, and building the tickets is what says so** — §6 rarely gets that kind of feedback, because a row usually leaves this table without anybody learning whether the reason it carried was true.

**Every one of the seven reasons was re-checked against the tree in this pass rather than carried over**, which is the point of writing the reason down instead of the verdict. One did not survive the check in the form it was written — SHIP-174's — and it is corrected in the table above rather than argued below: the strike stands on the absence of an agent, of `DD_*` configuration and of an account, but its old clause "the only two occurrences of Datadog in the repository are comments about log format" is now five occurrences across `internal/config`, `internal/httpx` (twice), `deploy/.env.example` and `contracts/components/schemas/error.yaml`. All five are still comments, and none of them is an integration. **A strike whose reason drifts while its verdict stays right is the failure mode this re-check exists for**, because the next reader checks the reason.

**The "SHIP-92…95 never parallelise" warning this section carried from SHIP-15a onwards is gone**, because the branch has run. §8 keeps the reasoning, struck, along with what the adversarial half of it produced.

| Ticket | Pts | Area |
|---|---|---|
| SHIP-56a | 2 | Status codegen for Go, Dart and TypeScript — cut from waves 2, 3, 4, 5, 6 and 7 because it writes into four trees. **Six cuts** is a ticket nobody will ever pick under contention; it wants a serial slot |
| SHIP-89 | 3 | Bid expiry — the terms an offer runs out on. It fits 000502 without change: expiry moves the head from `Submitted` to `Expired`, which leaves every constraint satisfied. **SHIP-95 has already written down the race it must survive**, against the statement a sweep is |
| SHIP-90 | 2 | The `Negotiating` presentation status — unblocked by SHIP-87, and the ticket four entries in §3 have now deferred to |
| SHIP-96 | 3 | Bid history visibility rules — unblocked by SHIP-88, which built the chain and the two-party read it refines. **It also owns the `Countered`/`Superseded` question** §9 records |
| SHIP-97 | 5 | Job-scoped messaging between customer and provider — unblocked by SHIP-84. A new table in the bidding block and a two-party read |
| SHIP-109 | 3 | Revoke and regenerate the driver token — unblocked by SHIP-108. The verifier exists, so invalidating a link is now a state change rather than a design |
| SHIP-113 | 3 | Administrative conflict resolution — unblocked by SHIP-112, which left this case in the refusal branch on purpose and named the ticket. `Docs/02` §3.1's fourth bullet is ahead of the code by exactly this ticket; see §9 |
| SHIP-117 | 2 | Exception flags the job for moderation — unblocked by SHIP-116 and SHIP-118. The join it starts from is `proofs.exception_reason IS NOT NULL` against the job's `Delivered` milestone, indexed by `idx_proofs_exception` and asserted end to end by `70-delivery.sh`, so the fact is not missing |
| SHIP-119 | 3 | 72-hour auto-complete — unblocked by SHIP-118. **X-6 decides whether an exception-completed job is in scope**, and the schema branches either way without a column changing. It is also the fourth scheduled task, which §9's `cmd/worker` entry says is the point to decide the task-selector question by |
| SHIP-121 | 3 | Driver portal milestone controls — unblocked by SHIP-120. **Not struck, and the reason is worth reading**: it needs a milestone write authenticated by a driver token and no such route exists, so the ticket adds one. That is scope rather than a blocker; see below |
| SHIP-127 | 2 | Flutter four-hour unsynced nudge — unblocked by SHIP-126, and small. `unsynced` is already the number it needs |
| SHIP-130 | 5 | Flutter camera capture with on-device compression — unblocked by SHIP-129 and SHIP-114. The upload endpoint and the presigned URL exist and are exercised against a real store |
| SHIP-133 | 3 | Flutter customer tracking view — unblocked by SHIP-77 and SHIP-115. `GET /v1/jobs/{id}/delivery/proof` serves the proof half; **the latest confirmed milestone is served by nothing**, which is the read gap §9 now carries |
| SHIP-136 | 5 | Emit domain events from job, bid and delivery transitions — startable and unstruck, **deferred out of waves 6 and 7 and now a third time**; see below |
| ~~SHIP-101~~ | 3 | **Every dependency met and unbuildable in fact** — nothing serves a provider their own bids; see below |
| ~~SHIP-147~~ | 5 | Buildable, but **one shared-surface edit stands in front of it**, and the second half is a question about where an administrator's password hashing lives; see below |
| ~~SHIP-169~~ | 3 | Buildable, but it touches `users`, created by `000002_users.up.sql` in the **shared migration block (1–99)** |
| ~~SHIP-174~~ | 3 | **Not demonstrable** — no Datadog agent in `deploy/docker-compose.yml`, no `DD_*` configuration anywhere, no account. Every occurrence of "Datadog" outside the documents is a comment about log format or about where a request ID is looked up |
| ~~SHIP-178~~ | 3 | **Not demonstrable** — install base comes from App Store Connect and Play Console (X-2, X-3), and no crash reporter is in the client |
| ~~SHIP-182~~ | 5 | **Not demonstrable** — there is no production, no managed backup and no restore tooling in `scripts/` or `deploy/` |
| ~~SHIP-183~~ | 3 | A **decision ticket** — §9 parks the per-account-lockout question here; it also rewrites limits on every domain's routes |

Plus **X-1, X-3, X-4, X-5 and X-6**, none of which is code and none of which has started. X-5 and X-6 need no third party at all, and **X-6 now gates a startable ticket** rather than a hypothetical one: SHIP-119 cannot decide for itself whether an exception-completed job may auto-complete.

**The startable-and-sensible set is 14 tickets and 44 points**, down from 15 and 52. The average startable ticket falls again — 52 ÷ 15 = 3.47 becomes 44 ÷ 14 = 3.14 — which is the second consecutive fall and for the same reason as the first: every one of the seven arrivals sits one hop behind something wave 7 built, and a ticket that consumes a surface is smaller than the ticket that opened it. **The reading to take is that the queue is now mostly client work over a platform that is ahead of it**, which is the opposite of where wave 5 left it.

**SHIP-101 is the second instance of §6's third category, and it is worth being precise about what the category is.** Every dependency is met — SHIP-100 landed, and a provider can place a bid from the app today. Its *Done when* is "provider sees their own bids grouped by status", and **there is nothing for that screen to read**. `routes_golden.txt` carries no list-my-bids route in any form; the only bidding *read* on the entire served surface is `GET /v1/jobs/{id}/bids/{bid_id}/history`, which needs a job and a bid identifier the provider would have to have already. Verified against the manifest rather than inferred from the domain. **This is SHIP-114's old shape** — startable by dependency, unbuildable in fact — and it is kept in exactly that form because that is what made SHIP-114's five-wave strike honest enough to close properly when the reason finally expired. The reason here expires when somebody adds the read, which §9 now records as wanting a lettered M3 ticket.

**SHIP-121 is startable and immediately blocked by the same kind of gap, and it is a note rather than a strike.** The driver portal cannot record a milestone: `POST /v1/jobs/{id}/milestones` is `RequireUser` in the manifest, and the only `driver-token` route in the entire service is the read `GET /v1/driver/jobs/{id}`. **Three separate lanes specified the missing endpoint independently** — `POST /v1/driver/jobs/{id}/milestones` under `RequireDriverToken` — and not one of them built it, because it belonged to none of their tickets. That convergence is what makes this a note: the route is designed, the auth class has been mapped and serving since SHIP-108, `internal/delivery` already holds the milestone service behind it, and the idempotency-scope question the driver's half raises is written up in §9. **Whoever takes SHIP-121 is adding a specified endpoint, not discovering that one is needed** — that is scope, and it is the shape SHIP-114 was left in once the store existed. SHIP-101's read has no such design behind it, which is why one is struck and the other is not.

**SHIP-147's strike reason is unchanged from the wave-6 pass and was re-checked rather than carried.** `RequireAdmin` is still absent from every branch of `guardsFor` in `cmd/api/routes.go`, and a route declaring it still stops the process at startup, so a domain branch cannot build the ticket without editing a file `Docs/10` §9.2 forbids it. **SHIP-15m demonstrated the way out on the other guard** and `routes.go`'s own comment promises SHIP-147 the same treatment — a second parameter and a second constructor beside `newDriverTokenGuard`. The measured cost of doing that is now on record rather than estimated: **SHIP-108 changed 15 `nil` call sites across five test files** — `routes_identity_test.go` (10), `auth_test.go` (2) and one each in `manifest_test.go`, `routes_app_test.go` and `routes_test.go` — and a prep ticket that supplies the seam should absorb that churn with a test helper rather than leaving it for the domain ticket. **The second half of SHIP-147's problem is not the guard at all**, and it has no owner: an administrator has a password, argon2id hashing lives in `internal/identity`, and a domain may not import another domain. So SHIP-147 either duplicates the hashing inside `internal/admin`, or somebody promotes it to infrastructure — which is a `boundaries` edit and therefore prep-ticket work of exactly the kind this row is already waiting for. Decide it with the seam, in one change.

**SHIP-136 is startable and not struck, but it cannot share a wave with any track owning `jobs`, `bidding` or `delivery`.** Its *Done when* is "every state change in `Docs/01` §4.5 emits its event from the domain, not the API layer", and there are exactly three domains with state changes in them — so it opens all three at once. `Docs/10` §9.1 gives one package directory to one agent at a time, which makes this a **scheduling constraint rather than a strike**: SHIP-136 is perfectly buildable, it simply consumes three of the wave's four package slots while it runs. SHIP-135 left it the easy half deliberately — a domain declares its own events in its own `events.go` and `internal/events` is never edited to add one — so the work is three small files rather than one shared surface. Schedule it beside client tickets, or serially.

**It has now been deferred three times, and the third deferral is recorded here for the same reason the second was.** Wave 6 cut it because two of its four tracks held `bidding` and `delivery`. Wave 7 cut it because the award branch held `bidding` and the proof lane held `delivery`, which left it a domain short of the three it needs. **A deferral with no reason written down is how a ticket goes quiet**, and SHIP-56a is the worked example — six cuts now, each individually correct, and no wave has ever been the wave that took it. SHIP-136 is two behind it on the same trajectory. **Wave 8 is the first wave that could take it**: four of the startable-and-sensible tickets are client work that opens no Go package at all — SHIP-121's added endpoint aside, SHIP-127, 130 and 133 — so a wave built around them leaves `jobs`, `bidding` and `delivery` free for the whole of it. No previous wave has had that option.

**The package constraint has moved, and `internal/delivery` is now the contended one.** `bidding` is down from five queued tickets to four — SHIP-89, 90, 96 and 97, with SHIP-92 built — and none of the four takes the package alone the way SHIP-92 did. `delivery` has three: SHIP-109, SHIP-113, and the driver milestone endpoint SHIP-121 has to add, in a package where wave 7 already ran a lane for four tickets. `internal/identity` still constrains nothing: no queued ticket needs to open it, and the two that eventually will are SHIP-183, which is a decision before it is a change, and SHIP-169, which is struck above for touching `users` in the shared migration block. SHIP-136 remains the hardest case, because it opens `jobs`, `bidding` and `delivery` in one ticket.

**Public routes still share the anonymous idempotency scope, and that remains safe.** `replayOrRefuse` fingerprints method, path and body, so reading another caller's stored response requires sending their exact request — which, on every route on `Docs/10` §4.1's allow-list, means already holding the secret material in their body. `make verify` checks the anonymous scope still works, because scoping idempotency into uselessness would be a subtler regression than leaving it shared. **The driver's half of that question becomes reachable at SHIP-121** and is written up in §9.

## 7. Wave 7 — what landed

One ticket serially, then four tracks concurrently. **Fourteen tickets, forty-nine points, all
delivered, no trim taken.**

| Step | Tickets | Landed |
|---|---|---|
| **Pre-step** (serial, primary tree) | SHIP-15p | Merged at `c20c126` before any track started |
| **Track A** the award transaction | SHIP-92 → 93 → 94 → 95 | All four, 16 points — three sequential runs on one branch, **the third written adversarially** |
| **Track B** proof and storage | SHIP-114 → 115 → 116 → 118 | All four, 14 points — four sequential runs, the wave's longest chain |
| **Track C** provider client | SHIP-100, 126, 129, 168 | All four, 11 points — three runs |
| **Track D** driver portal | SHIP-120 | 5 points — two runs, the second closing the two gaps the first recorded |

**Thirteen agent runs**, and the exit criterion was: a customer can award one offer and only one,
under the lock ordering SHIP-88 wrote down, with the races observed rather than assumed; a
photograph reaches an object store this API is not in the path of, becomes evidence for one recorded
milestone, and a delivery that could not be photographed has something to say instead; `Delivered`
becomes recordable and is refused when it carries neither; the driver's link opens a page that shows
one job and no other; and the provider's half of the app can place a bid and record a milestone
through the durable queue. **All of it holds**, and `make verify` went from 481 checks across 13
sections to 555 across the same 13. `make flutter-check` went from 594 host tests to 766, and
`make web-check` went from **0 tests to 25** — not because the driver portal had none, but because
`web-check` never ran the suite until this wave wired `web-test` into it between the lint and the
build.

### The two things the wave existed to do, both closed, and both proved rather than asserted

**The award transaction, out of reach for three waves.** SHIP-92's second dependency was SHIP-88,
which sat four hops behind the provider feed, and §8 has carried "SHIP-92…95 never parallelise" since
SHIP-15a wrote this file in wave 1. It is built: one offer accepted and the job moved in one
transaction, against the lock
ordering §3's SHIP-88 entry recorded rather than one invented on the branch, idempotent by **state**
so it needed no key column and no migration. What makes it more than a claim is SHIP-95 — `Docs/08`'s
four named races, each one *observed* by holding a transaction open and polling `pg_blocking_pids`
until PostgreSQL confirms the other backend is waiting, rather than released together and hoped for.

**"Delivered requires photo proof or a recorded exception reason, never neither."** `CLAUDE.md` has
listed that as an invariant since before any of the machinery existed, and `Docs/02` §3's own line
had no enforcement behind it since the status model was written — because `Delivered` was a status
nothing could reach. It is now enforced in the domain, where a client gets `409
delivery_proof_required` and is told which of the two to send, and again by a
`DEFERRABLE INITIALLY DEFERRED` constraint trigger that refuses the row at `COMMIT` whoever wrote it.
**Deferred is load-bearing rather than tidy**: the evidence points at the milestone and can only be
written second, so an ordinary `AFTER INSERT` trigger refuses every honest delivery there is — making
it `NOT DEFERRABLE` fails nine tests. And the second layer is demonstrated the only way a second
layer can be: a **raw SQL insert that bypasses the service entirely** is refused, in the domain's
tests and again in `make verify`.

### Cutting every track from the prep branch removed the conflict wave 6 paid four times

§7a records this as the cheapest process change available, found by accident on a single track.
Wave 7 applied it deliberately to all four, and the result is measurable: **all four branches merged
clean against `ship-15p`.** Wave 6's three tracks that were cut from `develop` while the previous
reconciliation sat unmerged each met it in this file, and the one cut from the pre-step did not.

What remained was smaller and is worth naming precisely, because it is what the rule does not fix.
**One pair met each other**: the Flutter track and the driver-portal track both wrote §3 prose, and
two tracks appending to §3 is the conflict this file will always have. And **the proof branch met the
other three at the end**, being the last of the four to merge — which is the ordinary last-merge cost
that §7e recorded in wave 2 and which no ordering decision removes. The mechanism only removes the
conflict with the *reconciliation*; it does not remove the tracks' conflict with each other.

### The adversarial run earned its place, and the argument is now evidence rather than a recommendation

§8 asked for this for six waves — "consider using a second agent adversarially — one implements
92–94, another writes SHIP-95 from `Docs/02` §3 and `Docs/08`'s four named races *without reading the
implementation*". Wave 7 did it, and the arrangement is no longer a suggestion.

**What "blind" meant, exactly**, because a loose version of this would prove nothing: the author read
`Docs/02` §3, `Docs/08`'s four named races, `contracts/paths/bidding.yaml`, `errors.go`, `model.go`,
the fixtures, and the exported signatures through `go doc` — and did not open `service.go`,
`postgres.go`, `ports.go`, `http.go` or `cmd/api/routes_bidding.go`, nor any test whose name contains
"Award".

**It killed two of the three weaknesses SHIP-92 left, and established that the third was not a
weakness at all.** With `race_test.go` removed, the pre-existing package passes entirely under the
bid-before-job lock mutation, and passes under the both-guards-removed mutation except for a test
that fires about counters rather than about the award. No award test noticed either. The third
finding is the more interesting one: the accept's compare-and-set is **redundant**, because the award
decides liveness under the bid's own `FOR UPDATE`, so either guard alone suffices and **no suite can
distinguish removing one**. Remove both and the award succeeds on a withdrawn offer. Nothing was
changed — it is a note for whoever next edits `acceptBid` or `lockBid`, and it is the kind of finding
a suite written from the same reading as the code cannot produce, because it is a fact about the code
rather than about the specification.

### A report where every mutation was caught is the one to probe

§7a recorded this after wave 6 and wave 7 gives it more evidence, which is why it is restated rather
than referred to. **Four mutations survived across the whole wave, and every one of them produced a
real change:**

| What survived | What it produced |
|---|---|
| The driver portal deriving the job identifier from its own credential | A wire test that asks what path the process actually requested, rather than scanning source for a decode — because a scan is what §7b's budget guard was defeated by |
| A staleness guard whose own suite could not arrange the interleaving | `stale_snapshot_test.dart`, which arranges it — the guard stops a stale snapshot telling a driver with no signal that Shipper has their update |
| A missing `try`/`catch` | Its absence would have stopped the app drawing a frame at all, which no existing test reached |
| The award's redundant compare-and-set | The note above, and the black-box inference that the liveness decision is made under the bid's row lock |

**Two agents volunteered that their own clean sweeps were the weaker result**, unprompted, which is
the review posture this entry is arguing for rather than the finding itself. A mutation set that all
dies is either a tight suite or a set of mutations aimed where the tests already are, and from the
outside those look identical. The useful review question stays "which ones survived, and what did each
one reveal".

### `git checkout <file>` destroyed uncommitted work in two lanes

Two tracks lost work this way while reverting mutations, and it is worth naming because it is the
**obvious** revert and it is wrong on an unstaged tree: `git checkout <file>` restores the file from
the index, which discards every uncommitted change in it — including the ones that are not the
mutation. The mutation is deliberate and reversible; the hour of work sitting beside it in the same
file is neither.

The recipe both lanes converged on afterwards is in §3's mutation tables and belongs in `CLAUDE.md`
beside the existing mutation guidance: **copy the file aside or tar-snapshot the tree first, restore
from the copy, and confirm with `git diff` *and* a checksum.** The checksum is the part that is easy
to drop and is the reason the recipe works — `git diff` reporting nothing proves the file matches the
index, which is exactly what it did after the destructive revert too.

### What the wave cost in scale

Wave 1 delivered 32 points, wave 2 delivered 37, wave 3 delivered 48, wave 4 delivered 39, wave 5
delivered 37, wave 6 delivered 47, and wave 7 delivered **49 across four tracks** — **the largest by
points and the largest by ticket count at fourteen**, taking both records from wave 6 by two and one.
Seven waves have settled the concurrency question completely: four tracks is what works, and the
point total moves with the composition rather than with the effort.

What changed in wave 7 is that **two of the four tracks were four-deep chains at once**, where wave 6
had one five-deep chain and wave 5 had one four-deep. A chain cannot be split across tracks at all, so
two of them consumed half the wave's concurrency for its whole length — and the counterpart held as
well as it did in wave 6: six of the fourteen tickets closed were unblocked inside their own branch
and never appeared in §6 at all.

## 7a. Wave 6 — what landed

One ticket serially, then four tracks concurrently. **Thirteen tickets, forty-seven points, all
delivered, no trim taken.**

| Step | Tickets | Landed |
|---|---|---|
| **Pre-step** (serial, primary tree) | SHIP-15m | Merged at `97b4900` before any track started |
| **Track A** bids and offers | SHIP-84 → 85 → 86 → 87 → 88 | All five, 15 points — five sequential runs on one branch, the longest chain any track has run here |
| **Track B** driver token and absorption | SHIP-107 → 108, SHIP-112 | All three, 13 points |
| **Track C** provider feed and queue | SHIP-99, SHIP-124 → 125 | All three, 13 points |
| **Track D** dispute intake | SHIP-163 | 3 points |

**Twelve agent runs**, and the exit criterion was: a provider can bid, revise and withdraw; either
party can counter and only the latest offer is acceptable; a driver's link is a credential the
service actually verifies and grants exactly one job; a late milestone is absorbed rather than
discarded; a customer or provider can raise a dispute that freezes the job; and the client can queue
work that survives a restart and drain it when the network allows. **All of it holds**, and
`make verify` went from 355 checks across 12 sections to **476 across 13**. `make flutter-check`
went to 594 host tests.

### `internal/bidding` went from a model to five endpoints, and `internal/admin` opened

`bidding` held `doc.go`, `model.go` and `model_test.go` at the branch point and nothing else — the
eight bid statuses behind `bids` and its one-accepted-bid index, with no endpoint over them. It now
serves `POST /v1/jobs/{id}/bids`, `PATCH …/bids/{bid_id}`, `POST …/counter`, `GET …/history` and
`POST …/withdraw`. **That was the last domain holding a table with no HTTP surface**, so all six
domains with logic in them now answer requests.

`internal/admin` opened at SHIP-163 with `POST /v1/jobs/{id}/disputes` — **M6's first code**, and a
*user* endpoint rather than an administrative one, which is the reading `Docs/04` §7 forced rather
than an oversight. §3 has both accounts.

### SHIP-88 made SHIP-92…95 reachable after three waves out of reach

The award transaction has been the backlog's most-deferred piece of real work: SHIP-92's second
dependency was SHIP-88, and SHIP-88 sat four hops behind the provider feed. **It is built**, and
§3's SHIP-88 entry does more than record that — it hands the award branch its lock ordering, the two
column constraints the database will enforce on its behalf, and the reason a `CHECK` can hold the
award at all. `ck_bids_superseded_is_not_live` turns "only the latest valid offer can be accepted"
into something SHIP-92 physically cannot violate, and `ck_bids_only_a_providers_offer_is_accepted`
stops a customer's own counter being awarded. **The branch starts against a design rather than a
blank page**, which is the single most useful thing this wave produced for the next one.

### Cutting a track from the prep branch removes its merge conflict entirely

This is the cheapest process change available and it was found by accident, which is why it is worth
writing down rather than assuming somebody will notice again.

`ship-107-112` needed SHIP-15m's driver-guard seam, so it was cut from `ship-15m-wave-6-prep` rather
than from `develop`. **It was the only one of the four tracks to merge clean.** The other three were
cut from `develop` while the wave-5 reconciliation (`ship-15k`) still sat unmerged, and every one of
them met it in this file.

The mechanism is not subtle once stated: a track cut from `develop` before the reconciliation lands
is guaranteed to conflict with it, because a reconciliation pass rewrites §1, §2 and §6 and a track
appends to §3 in the same file. A track cut from the prep branch already has the reconciliation in
its history and has nothing to resolve. **The rule: cut every track from the pre-step, and merge the
pre-step after the previous wave's reconciliation.** It costs one ordering decision at dispatch.

### Infrastructure, not the work, paced the wave

**Ten agent runs were killed** — two on session limits, three on the host sleeping, one on a
mis-aimed stop, and the remainder on restarts around those. **Not one of them lost work.** That is a
property of how the runs are set up rather than luck, and it is worth keeping deliberately:

- **Agents read the tree before they write to it**, so a restarted run reconstructs its position
  from the repository rather than from a context window it no longer has.
- **Agents write into a real worktree**, so partial work is on disk and in git rather than held in a
  conversation. A killed run leaves a branch somebody can read.

The failure mode this avoids is the one where a run holds an hour of reasoning in context, is
killed, and the next run starts from nothing. **Twelve runs delivered thirteen tickets while ten
were interrupted**, which is what says the property is real rather than untested. **The reading to
take is that the wave was paced by infrastructure and not by the work** — a run count is not an
effort measure here, and a plan that reads it as one will size the next wave wrongly.

### The tracks' own mutation testing was better than the orchestrator's

Worth recording because it inverts the obvious assumption. **Nine orchestrator-run mutations were
all caught**, which reads as a good result and is actually the less informative one. SHIP-112's own
six included **two that survived**, and both survivors produced real changes: one **disproved a
comment the agent had written itself**, and the other **exposed an untested `case`**.

**A report where every mutation was caught is the one to probe.** A mutation set that all dies is
either a genuinely tight test suite or a set of mutations aimed where the tests already are, and
from the outside those look identical. The ones that survive are the only ones that tell you
something you did not already believe — so the useful review question is not "did the mutations
pass" but "which ones survived, and what did each one reveal".

### One wave rule was broken, and it is worth naming

**SHIP-87's and SHIP-88's commits edited §6.** §7f's wave rules say tracks do not touch §1, §2, §6
or §7 — several agents doing the same arithmetic on one table is a guaranteed conflict, and this
pass recomputed §6 from the dependency column anyway, so nothing was lost. It is recorded because
the rule held for four waves and then quietly did not, and a rule nobody notices breaking is one
that stops existing. The track's edits were *correct*; that is not the point. §3 and the done list
are a track's share of this file.

### What the wave cost in scale

Wave 1 delivered 32 points, wave 2 delivered 37, wave 3 delivered 48, wave 4 delivered 39, wave 5
delivered 37, and wave 6 delivered **47 across four tracks** — which was the second largest by points
and the largest by ticket count at thirteen when this was written, and **wave 7 has since taken both
records**, at 49 points across fourteen tickets. Six waves had settled the concurrency question by
this point: four tracks is what works, and the point total moves with the composition rather than
with the effort.

What changed in wave 6 was chain depth again, and further than wave 5 took it. Track A ran
**SHIP-84 → 85 → 86 → 87 → 88, five tickets each blocked by the one before it** — one deeper than
wave 5's four-deep Track A, and five sequential agent runs on a single branch. **A chain cannot be
split across tracks at all**, so its length rather than its point total is what decides whether it
fits in a wave; a five-deep chain occupies one track for the whole wave whatever else is happening.
The counterpart is that a chain track unblocks a great deal at once: five of the eleven tickets that
arrived in §6 came from this one branch.

## 7b. Wave 5 — what landed

One ticket serially, then four tracks concurrently. **Eleven tickets, thirty-seven points, all
delivered, no trim taken.**

| Step | Tickets | Landed |
|---|---|---|
| **Pre-step** (serial, primary tree) | SHIP-15i | Merged at `a00e9ed` before any track started |
| **Track A** provider feed | SHIP-79, 81 → SHIP-82, 83 | All four, 14 points — the wave's largest track, on a four-deep dependency chain |
| **Track B** expiry and topics | SHIP-69, 70, 135 | All three, 7 points |
| **Track C** assignment and milestones | SHIP-106, 111 | Both, 8 points |
| **Track D** customer detail and fleet | SHIP-77, 98 | Both, 8 points |

**Nine agent runs, which is the most any wave has used** — and two of them were restarts after an
infrastructure fault rather than anything in the work. That distinction is worth recording, because
a run count read as an effort measure would say wave 5 was harder than wave 3 at fewer points, and
it was not; seven runs did the work and two paid for the machine.

The exit criterion was: a provider can declare where they work and see only the jobs they are
eligible for, with the customer's budget provably absent from every byte of it; a job warns before
it expires and can be extended; a driver can be assigned and milestones recorded once per key; the
Kafka topics exist with a versioned schema; and the client serves both halves of the marketplace.
**All of it holds**, and `make verify` went from 268 checks across 11 sections to
**355 across 12**.

### `internal/delivery` opened, and the client reached the provider's half

`delivery` held a five-milestone vocabulary and nothing else at the end of wave 4. SHIP-106 gave it
`POST /v1/jobs/{id}/driver` and SHIP-111 gave it `POST /v1/jobs/{id}/milestones`, which makes it the
fourth domain of five to serve HTTP and **the first to reach two other domains through ports rather
than imports** — it needs to know that a job is awarded and who the awarded provider is, and it
learns both through interfaces it declares itself. `internal/bidding` is now the only domain holding
a table with no endpoint over it, and SHIP-84 is where that changes.

On the client, SHIP-98 drew the first screen in the app that belongs to one role and refuses the
other. That matters more than a fleet list: `Docs/07` §1's claim that the two halves stay genuinely
separate inside one app had been a statement about the shell until something existed that only one
half could use.

### The wave's real lesson: a spelling-based guard is not a guard

SHIP-67 built a source-parsing tripwire for the budget-privacy invariant — it reads
`internal/jobs`' own source and refuses a budget field on any shape outside a four-name allow-list —
and wave 4 recorded it as one of three guards that "fired for the first time and all three worked".
Wave 5 found both of its blind spots, and found them within one track.

**It cannot see another package.** `fleet.EligibleJob` (SHIP-81) is the first provider-facing shape
outside `internal/jobs` in the repository's history. The guard did not fail on it and did not pass
it either: it was **absent**, because it parses one package and nothing told anybody that a second
one now needed the same protection.

**It cannot see another name.** A field called `max_price` carrying the customer's maximum passes
it cleanly, because the word "budget" never appears. That is not a hypothetical — SHIP-83 verified
it by mutation in both directions: a field named `max_price` passes the source-parsing guard and
fails the new one, and renamed `budget_cents` it fails both.

**SHIP-83's answer is the shape to copy.** It holds the serialised response to a **closed set of
keys at every depth**, so a new key fails by default whatever it is called, and the value check runs
with identifiers stripped out. An allow-list of what may appear is a different kind of object from a
deny-list of what may not: the first is wrong only when somebody adds a key deliberately and updates
the set, and the second is wrong every time somebody is inventive.

Three enforcers of this invariant now exist and it is worth being clear about what each covers:

| Enforcer | Built at | Blind to |
|---|---|---|
| The source-parsing guards | SHIP-67, copied into `fleet` at SHIP-81 | Any name that is not "budget", and any package neither copy parses |
| The event-catalogue check | SHIP-135 | Renaming, in the same way — it refuses a registered field whose *name* mentions a budget. Its reach is what is remarkable: it covers bidding and delivery before either has written a line |
| The closed key set | SHIP-83 | Nothing, on the responses it covers |

**Only the last is immune to renaming**, and it is the only one that would have caught `max_price`.
The other two are still worth keeping — they fail earlier and in a different place, which is exactly
what a guard should do — but a reader counting three enforcers and concluding the invariant is
trebly protected would be wrong. Two of the three read names, and a name is the thing a mistake
changes.

### The verify count could not go stale, and that is SHIP-15i earning its three points

The `make verify` check count had conflicted in four consecutive merges, and in three of those four
**no figure in the conflict was correct**. SHIP-15i made it a checked figure: a successful run reads
the sentence out of §3, compares it with what it counted, and fails with the measured value printed.

Wave 5 is the first wave it was live for, and the result is unambiguous. **All four tracks moved the
count, to four different values, and not one of them was typed** — every one was written by
`make verify-update` on a tree where the run had just measured it. The merge that assembled them
still conflicted in that line, exactly as before, but for the first time the resolution was a
measurement rather than a negotiation between four numbers of unknown provenance. In wave 4 the same
line conflicted with no correct figure in it at all.

The guard's one operating rule is worth repeating because it is easy to break by accident: it finds
the sentence by its **bolding** and refuses to run if it finds the bold form more than once, so
every historical count in this document — including this section's own, in the exit criterion above
— is deliberately written in a form that does not match. A reconciliation pass that bolded one of
them would break `make verify` on a tree where nothing else was wrong.

### What the merge taught, and both halves are worth keeping

**`routes_golden.txt`'s `merge=union` prevents a *lost* route and guarantees a *reordered* one.**
The attribute is there so a route cannot vanish in a conflict resolution, and it does that job. What
it also does is append both sides in merge order while the generator emits them sorted, so a
union-resolved manifest is correct in content and wrong in order, and the test that reads it fails.
**The recipe: confirm the sorted set is unchanged, and only then regenerate** with
`go test ./cmd/api -run TestRouteTableMatchesGolden -update`. Confirming first is the part that
matters — `-update` will just as happily bless a genuinely missing endpoint, which is the exact
failure the golden file exists to catch, so reaching for it before reading the set turns a guard
into a rubber stamp. This wants a line in `CLAUDE.md`; §9 records that.

**Never commit or merge while a gate is running.** One session produced both failure modes in the
space of an hour, and they are worth naming separately because they look nothing alike:

- **A false failure.** A `make verify` run overlapping a merge reported a SHIP-79 failure and four
  phantom "no commit names them" tickets, on a tree where nothing whatsoever was wrong. Every one of
  the five findings was an artefact of reading a working tree that was being rewritten underneath
  the reader.
- **A false pass.** A merge committed while its own gates were still running captured the tree
  *before* the resolutions landed, and published a `develop` carrying a stale check count of 299
  against a true 355 and a union-ordered route table. The gates went green afterwards, on a tree
  nobody had committed.

The second is much the worse of the two: a false failure costs an hour of confusion, and a false
pass ships. `ship-15j-wave-5-merge-repair` exists to undo it, and §2's branch table keeps it visible
rather than tidying it into the wave. The rule this produces is one line and it belongs in
`CLAUDE.md`: gates run to completion, then the commit happens.

### What the wave cost in scale

Wave 1 delivered 32 points, wave 2 delivered 37, wave 3 delivered 48, wave 4 delivered 39, and wave
5 delivered **37 across four tracks**. Five waves have now settled into a band rather than a trend:
four tracks is the concurrency that works, and the high-water mark of 48 has not been approached
since. What changed in wave 5 was not the volume but the composition. Track A carried 14 points
through **SHIP-79 → 81 → 82 → 83, four tickets each blocked by the one before it** — the deepest
dependency chain any single branch has run here, where wave 3's largest lane was 21 points but never
more than three deep. That is the shape a wave takes once the queue is made of endpoint chains
rather than independent foundations, and it is worth planning for: a four-deep chain cannot be split
across tracks at all, so its length rather than its point total is what decides whether it fits.

## 7c. Wave 4 — what landed

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

## 7d. Wave 3 — what landed

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

## 7e. Wave 2 — what landed

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

## 7f. Wave 1 — what landed

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
- Tracks do not touch **§1, §2, §6, or §7 and its lettered predecessors** — several agents doing the same arithmetic on one table is a guaranteed conflict, and `make status` computes the real numbers anyway. Each track adds its own tickets to §3 and the done list; the rest is reconciled once when the wave lands, as a separate pass. Wave 2 did exactly that and it worked, and it held under four tracks three times before **wave 6 broke it**: SHIP-87's and SHIP-88's commits edited §6, correctly and unasked (§7a). **What a track must not skip is its §3 summary-table row**, which three of wave 4's four tracks did — the prose subsection is written and the index above it is forgotten. That is no longer a matter of remembering: SHIP-15i made it a `make status` failure naming every done ticket with no row, and waves 5 and 6 lost none.
- **Gates run to completion before anything is committed or merged.** Wave 5 broke this once and got a false failure and a false pass out of it in the same session — see §7b. It is `CLAUDE.md`'s rule now, written there at SHIP-15m.
- **Cut every track from the wave's pre-step, not from `develop`.** Wave 6 did it for one track by accident and that track was the only one of four to merge clean (§7a); **wave 7 did it for all four and all four merged clean** (§7). What it does not remove is two tracks meeting each other in §3, which is this file's permanent conflict.

## 8. Hard gates ahead

**~~SHIP-44 is a choke point.~~ Cleared — see §3.** `httpx.Idempotent` is wired with `httpx.SubjectScope`, and the freeze on authenticated state-changing endpoints is lifted. `make verify` demonstrates the separation against a running service rather than asserting it.

Kept here rather than deleted, because the shape recurs: this was described only as a constraint on *other* work, and so was never read as work itself while its dependencies had been met since wave 1. A gate with satisfied dependencies belongs in §6 the moment it becomes buildable.

**~~The next gate of the same kind is SHIP-108.~~ Cleared — see §3.** The driver's job-scoped token is a second verifier, and `Docs/10` §5 requires that neither token system can be exchanged for the other. **Both directions are now demonstrated over HTTP** by `scripts/verify/70-delivery.sh`: a mobile access token on `GET /v1/driver/jobs/{id}` answers 401, and a driver token on a user route answers 401. Before SHIP-108 only one direction existed, because no route accepted a driver token and there was nothing for a mobile token to be refused by.

Kept, struck, because the shape recurs and this is the second instance of it. **The prediction held exactly**: the entry said "whoever writes SHIP-108 writes both directions", and wave 6 put SHIP-107 and SHIP-108 in one slot on one branch, which is what stopped a signed job-scoped token existing with nothing that validates it. **A pair where one half signs and the other half verifies belongs to one owner**, and that is the reusable half. `RequireAdmin` is the next such pair — SHIP-147 signs and verifies an administrator session, and §6 records what stands in front of it.

**One thing SHIP-44 did not do: `RequireDriverToken` and `RequireAdmin` are declarable and unenforced.** A route declaring either panics at startup rather than being served open, so the failure direction is safe. **Half of that is now closed**: SHIP-15m seated the driver guard behind a startup argument, SHIP-108 filled it, and `RequireDriverToken` is mapped and serving. `RequireAdmin` is untouched — it is absent from every branch of `guardsFor`, and a route declaring it still stops the process — and **SHIP-147 gets the same treatment when it arrives** (§6).

**`RequireAdmin` is the only live gate left in this section, and saying so plainly is the point of this paragraph.** Everything else here is struck: SHIP-44's choke point, SHIP-108's verifier pair, the SHIP-67/SHIP-83 pairing, and now SHIP-92…95. What remains beneath it is the standing single-owner list, which is a rule about how work is assigned rather than a thing that must happen before other work can start. **So the honest reading of §8 today is that it is nearly all history**, and the section's own recurring lesson applies to the one entry that is not: a gate described only as a constraint on *other* work never gets read as work itself. `RequireAdmin` has had satisfied dependencies since wave 2, and §6 records the two things standing in front of it.

**~~SHIP-92…95 never parallelise.~~ Delivered — see §3 and §7.** All four landed on `ship-92-95-award-transaction` with nothing else on it, in that order, and the instruction this entry carried for five waves is now history rather than guidance. **The reasoning is kept because it is the only part that transfers**: the lock ordering, the idempotency interaction and the race tests were one design, and two owners produce two lock orderings, which is a deadlock or a lost update. The next piece of work with that property gets the same treatment, and §6 says SHIP-136 is the closest thing currently queued — for a different reason, that it opens three domains at once.

**The adversarial suggestion in this entry was taken, and it is the half worth reading now.** It read "consider using a second agent adversarially — one implements 92–94, another writes SHIP-95 from `Docs/02` §3 and `Docs/08`'s four named races *without reading the implementation*", and that is exactly what happened. **Two of the three properties the blind suite pinned were invisible to every test in the repository**, and the third turned out not to be a property at all — the award keeps its liveness rule twice and either guard alone suffices, so removing one is invisible and removing both lets an award succeed on a withdrawn offer. §7 has the account; §3's SHIP-95 entry has the mutation table. **A suite written from the same reading that produced the code proves the code agrees with itself**, and that is the sentence to carry to the next gate rather than the specific tickets.

**~~What the owner who starts tomorrow should read first.~~ Read instead what they wrote.** §3's SHIP-88 entry was the design document that branch did not otherwise have, and the branch used it; the entries that supersede it for a reader today are §3's **SHIP-92** for the transaction as built, **SHIP-93** for what the rejection sweep deliberately does not touch, **SHIP-94** for which of the two idempotency mechanisms does which work, and **SHIP-95** for the races.

**~~SHIP-91 is out of that branch entirely.~~ It was, and it stayed out.** The partial unique index built by SHIP-80 (`uq_bids_one_accepted_per_job` — see §3) met the ticket, the owner declared it delivered on 12 August 2026, and the award branch wrote no migration for it. **The prediction that the constraint would not make the award easier held exactly**: the index also does the locking, and SHIP-95's mutation run produced the deadlock §3's SHIP-88 entry predicted from the wrong lock ordering — five of six racers killed with `SQLSTATE 40P01`.

**Also single-owner, for reasons in `Docs/10`:** SHIP-57 (the status guard), SHIP-67 with SHIP-83 (budget privacy — test the serialised response, not struct fields), both token verifiers, and the middleware ordering in `newRouter` — which is now load-bearing in a second way, since `ResolveSubject` sitting outside `Idempotent` is what makes the scope work at all.

**~~The SHIP-67 / SHIP-83 pairing cannot be honoured in one wave.~~ Settled at SHIP-67: built now, SHIP-83 reserved to the same owner.** The fact about the dependency graph has not changed — SHIP-83 depends on SHIP-82 → SHIP-81 → (SHIP-79, SHIP-80) → SHIP-78, and wave 4 delivers only SHIP-78 and SHIP-80, leaving three hops. What has changed is that the choice the pairing forced has been made rather than deferred again.

**The decision, and the reasoning it was made on.** Deferring both would have left the column unbuilt for a rule it already satisfies, and SHIP-65's *Done when* incomplete for a third consecutive wave, in exchange for a test against an endpoint that does not exist. So SHIP-67 landed with the strongest proof available today, which turned out to be three tests rather than one — the source-parsing test that refuses a budget field on any shape but the owner's response, a wire test over every response a provider or a stranger can obtain, and a test on the stored event payload. §3 has the detail. ~~**SHIP-83 remains reserved to this owner and adds the fourth**: its provider response, serialised, asserted to carry no budget. That is the test the pairing was actually for, and it is the one thing that is still owed.~~

**~~Still owed.~~ Paid at SHIP-83 — see §3.** `TestTheProviderResponseCarriesNoBudgetInAnyForm` is the fourth proof: the provider's response obtained over HTTP through the real handler, asserted on the raw bytes across all three ways a provider can obtain a job. **It turned out to need to be stronger than the entry asked for.** "Asserted to carry no budget" reads as a search for the field, and a search catches `budget_cents` and misses `max_price` — so the test holds the response to a **closed set of keys** instead, checks the value with identifiers stripped out, and refuses to run at all against a fixture whose budget is NULL. Verified by mutation in both directions: a field named `max_price` passes the source-parsing guard and fails this one; renamed `budget_cents`, it fails both.

**The `make verify` tripwire has been moved, and this is the entry recording it.** The check asserting that no `budget` key was present is gone; what replaced it asserts the owner reads their own budget back and that no provider-facing or stranger-facing response mentions it in any form. **The tripwire is now the source-parsing test rather than a verify line** — whoever writes SHIP-82 or SHIP-83 will meet it as a failing test the moment a provider shape acquires the field, which is earlier and louder than a shell assertion would have been.

**That prediction held exactly, and SHIP-83 found the one thing it does not cover.** The source-parsing guard did fire first, and `internal/fleet`'s copy reached SHIP-82's and SHIP-83's new response types with no change to it — they are non-test files in the package it parses. What it cannot do is refuse a budget under a name that is not "budget", because it reads source and can only match a spelling. SHIP-83's serialised-response test is the axis it lacks, and `make verify` now makes the same closed-key-set assertion from outside Go, so neither can be quietly deleted alone.

## 9. Open recommendations nobody has decided

**~~Job status as a database guarantee.~~ Decided and built at SHIP-57 — see §3.** The trigger exists, and it asks for more than the recommendation did: not merely that a session variable is set, but that it names a `job_status_history` row written in the same transaction which describes this job making exactly this move. The weaker form would have been a flag any caller could set; this one cannot be satisfied without leaving the record, which is what makes SHIP-57a's *Done when* structural rather than remembered.

**~~Whether an adapter's value types get a home.~~ Decided at SHIP-60 — see §3.** **No neutral geo package; the geocoding port keeps its five-return signature.** §9's premise did not survive contact: it warned about deciding "before three domains adopt the wide signature", but the width is adopted **exactly once**, in `jobs/ports.go`, and converted to a `Location` in the next statement — no store method, handler, response type or test carries five return values. What a second domain would adopt is a *coordinate type*, and a wide signature does not force that type to be wide. **The revisit trigger is named rather than left to judgement: the first ticket needing the distance between two coordinates in a domain other than `jobs`.** At that point `internal/geo` gets a `Point` and the haversine, the port narrows to `Lookup(ctx, address) (geo.Point, bool, error)`, and the change is confined to `ports.go`, two adapter methods and one conversion. The original reasoning is kept below because the revisit will need it.

**The trigger has been re-aimed, because the ticket it named was the wrong one.** This entry said "likely SHIP-81" from SHIP-60 until wave 5, and SHIP-81 landed without firing it. SHIP-79 settled a provider's service area as **a set of named regions — states and postcodes — rather than a radius around a point**, on three arguments recorded in §3: a job's coordinate is best-effort by design and staging has no geocoder at all, so a radius filter would silently match nothing; Australian road freight is quoted by postcode zone rather than by kilometres, and a straight line is wrong about roads; and both `Docs/09`'s own reading and SHIP-60's four-part address already assumed comparison by region. So SHIP-81 became a set-membership query and `internal/fleet` does no distance arithmetic at all.

**What is left is a trigger with no ticket behind it, and saying so is the honest version.** No ticket in the backlog today needs the distance between two coordinates — the shape that would is "providers within 50 km of the pickup, ranked", and nothing asks for it. This entry stays open rather than closed because the reasoning below is what a future ranking or radius feature will need, and because the regions stay either way: a provider has to be able to say "I do not cross the Nullarbor" in a form a straight line cannot express. **Do not name a likely ticket again.** Naming one was what made this trigger look satisfied for two waves while nothing had actually happened.

Wave 1 surfaced a consequence of the consumer-declares-the-interface rule that nobody had hit before. A domain's `ports.go` must name the adapter's method signature and may not import the adapter, so no struct declared in an adapter can appear in one. Geocoding therefore ended up as:

```go
Lookup(ctx context.Context, address string) (lat, lng float64, formatted string, found bool, err error)
```

and not-found is comma-ok rather than a sentinel error, because `errors.Is(err, geocoding.ErrNotFound)` would also be an import. The reasoning is correct and the lint agrees. But a neutral infrastructure package holding a coordinate type — the same shape as the pre-seeded `pagination`, `ratelimit` and `money` — would let both sides name it with no dependency edge either way, and that option was unavailable only because `internal/boundaries` was a forbidden shared edit mid-wave.

**~~`httpx.RegisterCode` is documented but does not exist.~~ Decided and built at SHIP-15c.** The registry, the uniqueness tests in `cmd/api`, and the generated `Docs/10-api-error-codes.md` all exist; `Docs/10` §4.4 is now true and says so, including that it was not. The choice was between building the mechanism and amending the document to match reality, and building won because SHIP-30 and SHIP-57 both need it on separate tracks in the same wave.

**~~A ticket for the web CI workflows.~~ Written as SHIP-23a at SHIP-15c, and built — see §3.** Two workflows, path-filtered per surface, and the filter demonstrated against a changed-file matrix rather than believed.

**~~`make web-check` runs its type-check before its build.~~ Decided and fixed at SHIP-15e — see §3.** `web-check` became `web-lint web-build web-typecheck`, and the `make web-build` workaround was deleted from both web workflows. Demonstrated by removing `.next` from both applications and running the target to green — the state CI is always in and a developer never is. **SHIP-120 added the tests in front of the build**, so it now reads `web-lint web-test web-build web-typecheck`: the tests cost a tenth of a second against the build's thirty and depend on nothing the build produces, so a failing guard is reported before the slow part. The lint → build → typecheck ordering this entry settled is untouched, because that dependency is the type-check's alone.

**`Docs/02` §4 gives one event two statuses, and SHIP-87 could not tell them apart.** The section lists both `Countered` — "an offer that has been answered with a different price or timing" — and `Superseded` — "an offer displaced by a counter from either party". Read carefully those are the same transition described from the two ends, and there is no third thing either could mean: an offer is displaced by a counter, or it is not.

**SHIP-87 wrote `Superseded` and left `Countered` with no writer**, because the published vocabulary had already chosen: `bidding_bid_closed` and the `BidNoLongerYours` response in `contracts/paths/bidding.yaml` have enumerated "rejected, expired, superseded" since SHIP-85 and neither names `Countered`. The constant is kept, because Docs/10 §3.4 pairs the Go list with `ck_bids_status` in both directions and Docs/02 §4 is the authority for the list.

**This is reported rather than resolved, deliberately.** Two options and neither is a code change: `Docs/02` §4 gains a sentence saying `Countered` is a synonym retained for the vocabulary and `Superseded` is what the platform writes; or it gains a distinction the two statuses are actually for, at which point `bidding` writes both and `bidding_bid_closed`'s list grows by one. The first costs a sentence and the second costs a ticket. Whoever writes **SHIP-96** — the bid-history visibility rules — is the natural owner, because they are the first person who has to render a chain to three audiences and will notice immediately if a distinction was wanted.

**Re-checked against the tree in this pass, and it is now a fact in the code rather than a prediction about it.** `internal/bidding/model.go` declares `StatusCountered`, lists it in the closed status set that `ck_bids_status` is held against in both directions, and carries a comment saying it is **deliberately never written**. Nothing in the domain assigns it. So the constant, the `CHECK` and `Docs/02` §4 all agree that the value exists, and the only thing that does not is the platform. **SHIP-96 still owns it**, and the cheaper of the two options has not become less cheap by waiting.

**`ck_bids_offer_has_timing` is answerable now and is still not written, and the "answerable" half was confirmed rather than assumed.** `000501` removed it because it would have bound SHIP-87's design. **SHIP-87 has landed**, that design is made, and every offer this platform writes past `Draft` names both instants — so the constraint would hold against the data the service produces today. It is not added because 000501's *other* finding still holds: it failed six of SHIP-80's own migration tests, which insert `Submitted` and `Accepted` bids with no timing in order to exercise `ck_bids_status`, and making them pass means editing another ticket's test file. **A small ticket: one migration plus an edit to `migrations/bids_test.go`'s fixtures.** Worth taking, because a stated-timing rule enforced only by a validator is one a worker or an admin path could bypass.

**`flutter_secure_storage` is held at 10.x because version 11 needs `compileSdk = 37`.** The client compiles against 36 today, and Android Gradle Plugin 9.0.1 names 36 as its own maximum recommended — so taking 11 means moving the SDK and probably the Gradle plugin together. There is no urgency: 10.3.1 uses the same Keystore-wrapped ciphers and the same API 23 requirement. **Decide it with SHIP-24 and SHIP-26**, which are the tickets that touch the Android build configuration anyway.

**~~Biometric unlock is still open.~~ Decided at SHIP-55 — see §3. Out of the MVP, with three named triggers that would reopen it.** The short version: it is a convenience over a token the device passcode already gates, `AndroidOptions.biometric()` needs API 28 against this app's floor of 24, and an optional control needs a settings surface that does not exist before SHIP-173. The reopening triggers are the Android floor moving at SHIP-24/26, a settings screen existing, or the pilot holding something that makes an unlocked handset a real exposure. **`Docs/07` §9's table row and its closing paragraph both record the decision now**, corrected in this reconciliation pass — SHIP-55 owned `apps/mobile/**` and this file, so it could not reach them. `Docs/07` §3's position row is untouched, because it describes where biometric unlock *sits* rather than whether it ships.

**~~§10's done block should probably be `merge=union`, and §3 probably should not.~~ Decided and done at SHIP-15e — see §3.** Both halves were kept: the list is `merge=union` and §3 is not. Since a git attribute applies to a whole file, the list moved to `Docs/11-done.txt`, one ticket per line — which the recommendation had not noticed matters, because a union resolves line by line and the old block put several tickets on one line.

**~~`scripts/verify-foundation.sh` is the sixth shared surface, and it has no include mechanism.~~ Decided and split at SHIP-15e — see §3.** It is a harness plus one file per milestone or domain in `scripts/verify/`, numbered in reserved ranges the way migrations are, and a track adds a file rather than editing one. The count was unchanged at 105 across the split, which is the evidence the move lost nothing. **That 105 is a historical figure, not today's** — wave 3 took it to 208; §3 carries the current count.

**~~`device_sessions` has no expiry column.~~ Decided and built at SHIP-39 — see §3.** An explicit `device_sessions.refresh_token_expires_at`, `NOT NULL` with no default, in migration `000103`. The window **slides** — rewritten on every rotation, 30 days — so inactivity ends a session and daily use never does. **A Redis TTL was rejected** (`Docs/10` §5: a control a cache flush undoes is not one), and so was deriving expiry from `last_seen_at + TTL`, because that is a *display* column which SHIP-46 writes from a device-list **read** — a derived lifetime would mean every future write silently extends a credential. **No absolute session cap, deliberately**: that is a policy control with a product consequence rather than a mechanism, and it is another column and another migration whenever it is wanted.

**~~Signing out on the device does not end the session on the platform.~~ Closed at SHIP-77 — see §3.** `SessionController.signOut` now makes a fire-and-forget `POST /v1/auth/logout` (SHIP-43) before the local clear, so the discarded refresh token stops being honoured immediately instead of lasting up to thirty days, and the device's row leaves `GET /v1/auth/sessions`. Demonstrated on the wire: `204` with zero bytes, after which the refresh token answers `identity_refresh_token_invalid`.

**It was not "a handful of lines", and the two reasons are worth keeping** because both are the kind that a reader estimating this again would miss. The request must **not** travel through `AuthInterceptor` — the interceptor reads the session's access token at *request* time and `signOut` clears it at *call* time, so the naive shape races the clear, goes out unauthenticated, and then meets an interceptor whose answer to a `401` is to refresh and replay, which means minting a session in order to end one and calling `signOut` from inside `signOut`. And the endpoint answers `204`, which `ApiClient.postJson` raised as `ApiMalformedResponse` — a success reported as a broken response. So it is a `SessionEnder` port in `core/auth` taking the token as an argument, over the transport that carries no session, plus `ApiClient.postNoContent`. **One gap remains and is deliberate:** an access token that expired before somebody tapped Sign out cannot authenticate the call, and refreshing first would be a client minting a credential in order to destroy one. Fifteen minutes is the window; `DELETE /v1/auth/sessions/{id}` (SHIP-46) is the other route.

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

**~~The outbox has no dead-letter path.~~ Decided at SHIP-135 — see §3. There is none, because a
permanently unpublishable row is now unwritable.** This entry offered three ways out and called
bounding the payload the cheapest and probably the right one. It is right, and the argument turned
out to be stronger than "cheapest" once the class was enumerated: **"permanently unacceptable" can
only mean three things** — the event type is one nothing consumes, its aggregate has no topic, or
the payload is over the broker's limit. All three are decided by the event catalogue, and
`events.New` and `Outbox.Emit` check all three **inside the transaction making the state change**,
where a failure rolls the change back, tells the caller and names the line that built the event. So
every remaining outbox failure is transient — the broker is unreachable, or the topic set was never
applied — and for those, failing the batch and leaving every row claimable is exactly right.
**Rather than build a recovery path, the ticket made the failure unreachable.** Parking a row after
N attempts was rejected on `000004`'s own terms, that `published_at` is the publisher's only state,
and publishing per event was rejected for weakening the batch guarantee for every event to
accommodate one that should never exist. The bound is 16 KiB against a broker limit of 1,048,588
bytes and is deliberately untuned. **The revisit trigger: the first event whose payload is
legitimately unbounded** — a document, a photograph, a manifest — must not travel in the outbox at
all; it goes to object storage and the event carries the key. If that is ever refused, this question
reopens with the other two answers still on the table.

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
§7c). That section is fenced now and the fence is correct, but **the condition is structural rather
than a defect in either section**, and it gets sharper with every task registered. **SHIP-69
registered the third in wave 5**, so the budget this entry gave itself is down to one: SHIP-89's bid
expiry and SHIP-119's auto-complete are still queued behind it. What wave 5 did do was act on the
finding twice without settling it — SHIP-135's own section fences on the ids it created, and
`scripts/verify/50-jobs.sh` now applies the topic set before its first worker start and claims no
check for it, because that file's three worker starts also drain the outbox and every one of those
passes had been failing silently against a missing topic. Three ways
out and none of them owned: a task selector on the binary, so a section starts only what it is
demonstrating; a convention that every section fences what it asserts on, which is what both
instances resolved to and is much the cheapest; or accepting it and saying so in every section
header. The middle one is already the recipe SHIP-47's rate-limit bucket produced — **fence what
you assert on, and own what you assert about** — and after wave 5 it has now held three times
running, which is the strongest evidence available that it is enough on its own. **Decide before the
fourth task registers**, which is SHIP-89 or SHIP-119, whichever comes first — and **SHIP-119 became
startable in wave 7**, unblocked by SHIP-118, so "before the fourth task" is now a live deadline
rather than a distant one. Wave 7 added no task and no section that starts the worker, so the budget
is unchanged at one.

**~~No Kafka topics are created.~~ Decided and built at SHIP-135 — see §3.** `cmd/topics` creates
them, run by `make topics` and **applied like a migration**: a step somebody runs, which is the
shape this repository already has in `cmd/migrate`. The compose stack was rejected because it would
be a second hand-maintained topic list existing only locally, which is the drift a single
implementation avoids; a start-up step in `cmd/worker` was rejected for running once per *process*
rather than once per deployment, and for needing `Create` on the cluster in a process whose whole
job is producing. Local and deployed are therefore the same step, which was this entry's genuinely
open half. The set is three topics, one per aggregate, derived from `events.Topics()` rather than
typed so it cannot drift from the catalogue; the step is idempotent, and it **refuses** a topic
whose partition count disagrees rather than correcting it, because adding a partition rehashes every
key and silently ends the per-aggregate ordering the advisory locks exist to keep. `topicFor` did
change to `shipper.<aggregate_type>` as predicted, and it moved as well as changed: the mapping is
`internal/events.TopicFor`, beside the catalogue that decides the set.

---

**The seven below were all found in wave 5, and none of them was owned by a ticket when it was
written.** Five wanted a shared surface a domain branch may not edit, which is what a prep ticket is
for; two want an ordinary lettered ticket in the milestone they belong to. **Four of the five were
taken by SHIP-15m** and are struck below — which is the mechanism §1 describes working end to end:
paragraphs accumulate here during a wave and become the next wave's pre-step. An eighth, found while
building that pre-step, is at the end.

**No endpoint serves a job's status history to a customer, and no ticket in the backlog adds one.**
`job_status_history` has recorded the actor, the reason and both clocks since SHIP-57a,
append-only, and `jobs.Service.History` reads it in Go — but nothing exposes it over HTTP.
`GET /v1/jobs/{id}` answers the `Job` schema, which is `additionalProperties: false` and carries one
`status`, `created_at` and `updated_at`; there is no `history` array anywhere in the served surface.
SHIP-152 reads it, but it is admin-side and sits behind SHIP-147 → SHIP-151, none of which is built.
**This is why SHIP-77's status timeline is derived from the current status** and is written as a
list of things it refuses to claim: it will not date a step it cannot date, and it will not tick a
step `Docs/02` §2 permits a job to skip. The screen says so on itself, because a timeline with no
dates and no explanation reads as one that failed to load. **It wants a lettered ticket in M2** —
the endpoint is a read over a table that already holds everything, and `jobTimeline` takes the
history and the steps gain their real times with nothing else about the screen changing.

**Six of the eight fleet endpoints do not check the caller's role, and this is wider than wave 5
first recorded.** SHIP-98 found it as "five of six, `isProvider` is called in `Add` alone"; SHIP-79
then added two endpoints and one check, so the current state of
`services/core/internal/fleet/service.go` is that **`isProvider` is called in `Add` and `Declare`
and in no other method** — `Update`, `Deactivate`, `Reactivate`, `Vehicle`, `Vehicles` and `Profile`
do not check. **This is not a data leak and should not be reported as one.** Every one of those
methods scopes to the caller's own id, so a customer gets an empty list or a `404`, never another
provider's vehicle. What it is is an inconsistency with a real cost: the endpoint declines to
*refuse* somebody who has no business there, which is precisely what "the app may hide or disable;
the platform decides" is supposed to make safe. A customer who reached the fleet screen would have
seen an empty fleet, an Add button, a form to fill in, and a `403` at the very end of all of it.
SHIP-98 gated the surface on the device instead, which is the right call for that ticket and is a
workaround for a platform gap rather than a fix for it. **SHIP-78's gap; it wants a ticket.**

**~~Kafka has no per-worktree isolation, unlike the database and the ports.~~ Written down where it
will be read, at SHIP-15m — see §3.** The rule is now a row in `CLAUDE.md`'s worktree table and the
reasoning below is kept, because a rule whose evidence is deleted is a rule somebody will argue with.
`CLAUDE.md`'s worktree
table isolates `TEST_TEMPLATE_DB` and `HTTP_PORT`/`VERIFY_PORT` per tree and pins
`COMPOSE_PROJECT_NAME` so that all the trees share one stack — which is safe for PostgreSQL because
each tree gets its own database on the shared cluster. **For Kafka there is no equivalent: one
broker, one `shipper.job`, and every worktree publishes into it.** Track B hit it concretely, and
the failure looked exactly like a defect: a count over the topic failed on a run where nothing was
wrong, because a concurrent `make verify` in another worktree — on a build without SHIP-135 in it —
had put unversioned events on the same topic, and the count was right about what it saw. The recipe
is SHIP-47's, with one addition that is the actual finding: **on a Kafka topic a fence must be an id
rather than a timestamp**, because a concurrent run in another worktree is not ordered against this
one. Wave 4's `scripts/verify/80-notifications.sh` fences its SHIP-134 comparison on
`published_at > $outbox_fence`, which is a timestamp — it has been correct by luck rather than by
design, and it is the pattern the next section will copy. **That script is still unfixed and that is
deliberate**: it is another domain's file, and SHIP-15m stated the rule rather than reaching into it.
Whoever next opens `scripts/verify/80-notifications.sh` should fence on the ids it created, the way
SHIP-135's own section already does.

**~~`routes_golden.txt`'s `merge=union` prevents a *lost* route and guarantees a *reordered* one.~~
Decided and written into `CLAUDE.md` at SHIP-15m — see §3.** The recipe sits beside the existing
instruction to read the golden file after resolving a conflict, which is where somebody meets the
failure. The attribute does the job it was added for. What nobody had written down is its other half: a union
appends both sides in merge order while the generator emits them sorted, so a union-resolved
manifest is right in content and wrong in order, and the test fails on a tree where nothing is
missing. **The recipe is: confirm the sorted set is unchanged, and only then run
`go test ./cmd/api -run TestRouteTableMatchesGolden -update`.** Confirming first is the part that
matters — `-update` will just as happily bless a genuinely missing endpoint, which is the one
failure the golden file exists to catch. It is `CLAUDE.md`'s line now, in exactly that place.

**~~Never commit or merge while a gate is running.~~ Decided and written into `CLAUDE.md` at
SHIP-15m — see §3.** It reads as one line — run the gate, wait for it, then commit — with the two
failures below as the reason, beside the merge instructions. One session in wave 5 produced both a **false
failure** — a `make verify` overlapping a merge reported a SHIP-79 failure and four phantom "no
commit names them" tickets on a tree where nothing was wrong — and a **false pass**, where a merge
committed while its gates ran captured the tree before the resolutions landed and published a
`develop` carrying a stale check count of 299 against a true 355 and a union-ordered route table.
The second is much the worse: a false failure costs an hour, a false pass ships, and
`ship-15j-wave-5-merge-repair` exists only to undo it. §7b has the full account.

**~~`KAFKA_REPLICATION_FACTOR` is a flag rather than configuration, and the pattern is the finding
rather than the field.~~ Half closed at SHIP-15m — see §3.** The *field* exists:
`config.Kafka.ReplicationFactor`, read from `KAFKA_REPLICATION_FACTOR`, defaulting to
`DefaultKafkaReplicationFactor`, with `cmd/topics`' `-replication` kept as an operator override that
refuses zero rather than silently falling back. **The *pattern* is not closed** and is restated at
the end of this entry. Track B needed a replication factor — one is correct for a single-broker
compose stack and wrong for a cluster, which wants three — and could not add it, because
`internal/config` is a shared surface a domain branch may not edit. So it became
`go run ./cmd/topics -replication 3`, defaulting to `events.DefaultReplicationFactor`, which a
deployment can pass today with no configuration change and is a perfectly good answer for one field.
**What is worth recording is that this is the third track in three waves to park work against
`internal/config`**: `GEOCODING_*` and the page sizes were parked the same way in wave 3, and
SHIP-15g absorbed them a wave later. The conclusion is not "add the field" — it is that **a prep
ticket should ask each track up front what configuration it will want**, rather than absorbing a
third round of parked requests after the fact. Ask the question at dispatch, when the answer is
cheap.

**~~SHIP-15m did not ask it either, so that half stands open.~~ Closed at SHIP-15p — see §3.**
Wave 7's dispatch asked each track what it expected to need from `internal/config`, Track B answered
with nine fields, and the `Storage` section was written before the track opened rather than a wave
after it. **That is the first time the pattern has been broken in four prep tickets**, and the cost
was what the recommendation said it would be: a sentence at dispatch and a section in the prep
ticket. Keep asking — the entry stays here, struck, because the reasoning is the argument for asking
again next wave rather than a record of one occasion.

**A cross-block index request, and it is genuinely not urgent.** The provider feed wants
`(created_at DESC, id DESC) WHERE status IN ('Open','Negotiating')` on `jobs` — which is migration
block 400–499, and not `fleet`'s to draw from. It seq-scans under a `LIMIT` today, which is entirely
fine at hundreds of jobs and stops being fine at some volume nobody can currently name. Recorded so
that whoever sees the first slow feed does not rediscover it: **the index is known, the block is
known, and the only open question is when.**

---

**One more, found at SHIP-15m while building the `RequireDriverToken` seam.**

**A driver-token request will scope its idempotency key to `anonymous`, and nothing a guard does can
change that.** `httpx.SubjectScope` keys on the `authctx.Subject`, a driver token deliberately
produces none (§3), and the scope is computed group-wide **outside** `Idempotent` — while an auth
guard runs per route, inside it. So the ordering that makes SHIP-44's fix work is the same ordering
that puts the driver's scope out of a guard's reach, whatever the guard puts on the context.

**This is the posture §6 already accepts for public routes, not a new hole**: `replayOrRefuse`
fingerprints method, path and body, so reading somebody else's stored response means reproducing
their exact request — which on `POST /v1/jobs/{id}/milestones` means already holding the job
identifier. It is defensible, and it is worth deciding rather than inheriting, because the driver
half of M4 is entirely idempotent writes from a phone with a bad connection. **The two shapes
available**: a second group-wide resolver beside `ResolveSubject` that a driver grant can also
populate, with `SubjectScope` widened to read either; or an explicit decision that a job-scoped
grant scopes on the job identifier already in the path, which costs nothing and is weaker.

**~~Decide with SHIP-112.~~ Wrong ticket, corrected in this pass.** SHIP-112 landed and no driver
retries anything through it: `POST /v1/jobs/{id}/milestones` is **`RequireUser`** in
`routes_golden.txt`, and the only `driver-token` route on the whole manifest is the read
`GET /v1/driver/jobs/{id}`. **There is no driver-authenticated write in the service at all today**,
so the question this entry raises has never yet been reachable. **The first one is SHIP-121**, the
driver portal's milestone controls — "large touch targets record each milestone from a mobile
browser", which is a driver recording a milestone from a link that carries no account. **Decide with
SHIP-121**, and note the second thing that correction exposes: SHIP-121 needs a milestone write
authenticated by a driver token, and no such route exists. Whoever picks it up is adding one, not
just drawing buttons over an existing endpoint.

**Wave 7 confirmed that from three directions at once, which is the part worth adding.** Three
separate lanes — the driver portal, the proof lane and the milestone screen — each independently
wrote down the same missing route, `POST /v1/driver/jobs/{id}/milestones` under `RequireDriverToken`,
and not one of them built it, because it belonged to none of their tickets. **A gap three
independent readings arrive at is a specification, not an oversight**, and it means SHIP-121 starts
against a design rather than a blank page (§6). The scope decision above is the first thing that
route has to settle, before it is written rather than after.

---

**The eight below were all found in wave 6, and none of them was owned by a ticket when it was
written.** Two want a shared surface a domain branch may not edit, which is what a prep ticket is
for; the rest want an ordinary ticket, a sentence in a document, or a decision. They are written one
paragraph at a time, by whoever hit the surface first, which is the mechanism §1 describes: this is
where wave 7's pre-step got drafted, and **two of the eight have since been taken** — one by
SHIP-15p and one by SHIP-100 — and are struck below.

**~~The client's budget guard is spelling-based and misses a rename.~~ Decided and closed at
SHIP-100 — see §3.** `budget_stays_on_the_customer_side_test.dart` now carries both guards: the
source scan it always had, for the failure the rule is realistically broken by — a widget that
renders a budget being reused on a provider screen — and a registry holding every provider-facing
model to a **closed set of keys**, which is SHIP-83's shape brought across the wire. A field added to
`OpenJob`, `JobRegion` or `Bid` fails whatever it is called, and a *type* added to one of those files
fails until somebody records its key set.

**Two things the original entry had slightly wrong, and both are worth keeping.** The `max_price`
mutation was re-run against the tree at SHIP-100 and still passed the file this entry names — so that
half was right — but it did **not** go undetected: it failed two tests in `open_job_test.dart`, which
SHIP-99 wrote and which already held `OpenJob` to an exact key set. So the exposure was smaller than
stated, and its real shape was different from the one described: the closed key set existed for one
type, in a file somebody had to remember to write, and **a type with no such test had no guard
against a rename at all**. SHIP-100's `Bid` would have been exactly that type. The fix is therefore a
registry with a structural check rather than one more test.

**What remains is named rather than argued away**: a provider-facing model in a file nobody adds to
`_providerFacingFiles`. That is the same fail-closed-by-registration property `internal/boundaries`
gives a ninth Go package, and the file's own header says so.

**`make verify` fixture phone numbers are an undocumented shared namespace, and a track lost twenty
minutes to it.** Every account `make verify` registers needs a unique mobile number, sections are
*sourced* into one process, and two sections that pick the same prefix collide on the phone unique
index. Track D collided with `scripts/verify/80-notifications.sh`. **The map exists in exactly one
place — a comment in `scripts/verify/90-admin.sh`'s header** — and it is `0413x` jobs, `0414x`
fleet, `0417x` delivery, `04180` outbox, `0419x` admin, with `04120` identity's. **Two things that
comment does not say**: `scripts/verify/61-bidding.sh` draws `04145`, `04146` and `04147`, which are
*inside* fleet's `0414x` block and free only because fleet stopped at `04144`; and nothing tells the
next section author that any of this exists. **It wants a line in `Docs/10` §7.3**, beside the
reserved section-number ranges, which is the same kind of allocation and is already documented
there. This pass may not write it — `Docs/**` is a shared surface and this is a documentation-only
reconciliation of one file.

**SHIP-15g's out-of-order migration guard prints a fix that does not work on a fresh migration.**
The message tells the reader to run `make migrate-down n=all && make migrate-up`. On a migration
whose `up` has never run, `down` runs first against a schema that has none of its objects — which
fails, and leaves the database dirty, unless every statement in the `down` uses `IF EXISTS`. **Two
ways out and neither is large**: the guard's message says so, or the convention "a `down` migration
uses `IF EXISTS` throughout" is written into `Docs/10` and enforced the way the block ranges are.
The second is better, because it makes the guard's advice true rather than qualifying it.

**~~`routes_app_test.go`'s `routerWithApp` replaces `deps.Config` wholesale rather than mutating one
field.~~ Decided and fixed at SHIP-15p — see §3.** It now copies the configuration it was given and
overwrites the one field under test, and `TestTheAppFixtureOverwritesNothingButApp` fails if anybody
puts the literal back. **The guard names no configuration section on purpose**: it puts a marker in
one it has no use for and compares the whole struct with `App` blanked, so a section added tomorrow
is covered without anybody remembering this existed. `Storage` would have been the next section to
collect on the old shape, which is how a recommendation with no owner came to have one.

**SHIP-108 had to change 15 `nil` call sites across five test files, and SHIP-147 will meet exactly
this again.** Filling the guard seam turned `newRouter(…, nil)` into `newRouter(…, testDriverGuard())`
in `auth_test.go` (2), `manifest_test.go` (1), `routes_app_test.go` (1), `routes_identity_test.go`
(10) and `routes_test.go` (1). **None of it was interesting and all of it was mandatory**, because a
route declaring an unmapped class panics at startup — which is the seam behaving correctly, met from
the other side. **A prep ticket that supplies a seam should consider absorbing the call-site churn
with it**: SHIP-15m added the parameter and left every caller passing `nil`, and SHIP-147's prep can
instead add the parameter *and* a test helper in the same change, so the domain ticket edits only
its own constructor. The seam was still worth it — the alternative was SHIP-108 editing `routes.go`,
`manifest.go` and `main.go` from a domain branch — and this is a refinement of a mechanism that
worked, not a complaint about it.

**`internal/money` now has its second consumer, and writing it needs no shared-file edit at all.**
`bidding` stores `numeric(12,2)` against `int64` cents exactly as `jobs` does, and
`internal/bidding/service.go`'s `maxOfferCents = 100_000_000` is a second copy of
`internal/jobs/draft.go`'s `maxBudgetCents = 100_000_000` — arrived at independently and, as
`bidding`'s own comment says, deliberately not shared. **One correction worth stating plainly,
because an earlier reading of this had it backwards**: `money` is *already* registered in
`internal/boundaries/boundaries.go` — line 123, `"minor-unit arithmetic in AUD"` — so whoever writes
the package **edits nothing shared**. That is precisely the mechanism `ratelimit` and `pagination`
demonstrated in wave 3, and it is the third time the pre-seeded list has been about to pay off.
**What it needs is an owner and a wave in which no second lane is holding `jobs` or `bidding`**,
which is a scheduling problem rather than a design one. Do not repeat the claim that it requires a
`boundaries` edit.

**`Docs/02` §3.1's fourth bullet describes retention the platform does not do.** It says a queued
update contradicting an administrative action "loses… the attempt is retained in history". A
milestone recorded against a job cancelled or disputed before it reached that status is **refused
and the transaction rolled back** — nothing is retained. That is not a defect: SHIP-112 put the case
in the refusal branch deliberately and said so in three places in `internal/delivery`, and
retaining it is **SHIP-113's *Done when* verbatim** — "a queued update contradicting an admin action
loses and is retained with its reason". **So the document is ahead of the code by exactly one
ticket, and the ticket is startable** (§6). Recorded so that nobody reads §3.1 as a description of
today and reports the gap as a bug.

**~~The `sqlite3` native library is unverified from CI.~~ Confirmed in wave 7 — see §3.** SHIP-124
took `drift` and `drift_flutter`, and `sqlite3` 3.x supplies `libsqlite3` through a build hook rather
than through the superseded `sqlite3_flutter_libs` scripts. The entry's worry was precise: a build
hook is exactly the kind of thing that resolves on a host and fails on a device toolchain, and the
host tests could not tell the difference. **The device builds happened and the hook resolved.**
SHIP-129 ran `integration_test/record_milestone_test.dart` on an iPhone 17 simulator against a live
API, with **the real Drift queue on the device's filesystem** and the worker started the way
`main.dart` starts it, and SHIP-168 built and ran the same application on both an iPhone 17 simulator
and a Pixel emulator. The reason this was worth writing down rather than assuming is that the
iOS side is the one demonstrated *through the queue*; Android is demonstrated as far as the
application building and running with the hook in it. **The remaining question is CI, not the
toolchain** — `make flutter-check` runs on a Linux runner with no device and is green at 766 host
tests, so nothing in CI builds for a device and nothing will until SHIP-24…27.

---

**Three more, found at SHIP-15p while putting the object store in the stack.**

**`STORAGE_BUCKET` is not derived from the directory the way `TEST_TEMPLATE_DB` is, and that is the
weaker half of a decision rather than an oversight.** The `Makefile`'s own comment on
`TEST_TEMPLATE_DB` makes the argument better than this entry can: the failure mode of forgetting to
set it is silent and lands on the *other* worktree, so it is a default that cannot be omitted rather
than an instruction somebody follows. The bucket has the same property — a tree that leaves it alone
shares `shipper-dev` with every other tree that did, which is safe for a keyed read or write and
unsafe for a count or a listing, and the tree that pays is the one that set it. **It was left
explicit because a derived name has to be derived in three places** — the `Makefile`, `config.go`'s
default, and `scripts/verify-foundation.sh` — and three defaults that must agree is the shape this
file spends a lot of words regretting elsewhere. **Revisit the first time two tracks in one wave both
open `internal/platform/storage`**, which wave 7 did not: one track opened it, wrote `s3.go` and
`s3_test.go`, and nothing contended for a bucket. The trigger is unfired rather than retired.

**`deploy/docker-compose.yml` is a shared surface and is not on `Docs/10` §9.2's list.** That list
names `cmd/api/routes.go`, `internal/boundaries`, `internal/httpx/**`, `go.mod`, the root `Makefile`,
the shared migration block, `contracts/openapi.yaml`, `scripts/verify-foundation.sh`, `CLAUDE.md` and
`Docs/**` — and `deploy/.env.example` is discussed a paragraph later without being in the list
either. A compose service, a published port and a named volume are exactly as collision-prone as a
route table: two branches adding a service each merge cleanly and fail on `make up`. **It wants one
line in `Docs/10` §9.2**, which is a shared file this ticket had no other reason to open. Whoever
next opens `Docs/10` should add it.

**`mk/topics.mk`'s header still says the replication factor is "a flag rather than an entry in
`internal/config`", which SHIP-15m made false.** `config.Kafka.ReplicationFactor` exists, is read
from `KAFKA_REPLICATION_FACTOR`, and the flag is now an operator override rather than the only way
to set it — §3's SHIP-15m entry has the detail. The comment is three lines and the fix is one
sentence; it is recorded here rather than made because `mk/topics.mk` belongs to the topics work and
this pass had no reason to open it. **A code change that contradicts a comment is the cheapest kind
of drift to fix and the easiest to walk past**, which is why it is written down rather than left for
whoever is confused by it next.


---

**The eight below were all found in wave 7, and none of them was owned by a ticket when it was
written.** Three want a shared surface a domain branch may not edit, which is what a prep ticket is
for; the rest want an ordinary lettered ticket, a sentence in a document, or a decision. They are
written one paragraph at a time, by whoever hit the surface first, which is the mechanism §1
describes: this is where wave 8's pre-step gets drafted.

**Nothing serves a job to the provider delivering it, and nothing lists a provider's own bids.**
Three lanes hit this independently and none of them owned it. The award lane met it as a
verify-fixture trap: `GET /v1/jobs/open/{id}` is the provider's job read and it stops answering the
moment they win the job, so an awarded job answers `404` to the provider who was just awarded it. The
Flutter lane met it as a product hole: SHIP-129's milestone screen can show a job identifier and
nothing else — no address, no customer, no pickup window — because there is nothing to read. And §6
meets it a third time at SHIP-101, whose *Done when* is "provider sees their own bids grouped by
status" and which has no list-my-bids route to call in any form.

**This is §9's existing status-history entry in a second key**, and the two belong together: a read
over tables that already hold everything, wanted by screens that have shipped, owned by no ticket in
the backlog. The status-history entry has been open since wave 5 for exactly this reason. **It wants
a lettered M3 ticket** — the provider's view of a job they have been awarded, and their own bids
grouped by status — and until it exists SHIP-101 is struck in §6 and SHIP-133's "latest confirmed
milestone" is half-servable. Whoever writes it should read the next entry first, because the route
shape is decided before the ticket is.

**No four-segment `GET /v1/jobs/{id}/<literal>` can ever be registered, and it is not a delivery
problem.** `GET /v1/jobs/open/{id}` (SHIP-83) puts a literal in the `{id}` position. It and any
`GET /v1/jobs/{id}/<literal>` both match `/v1/jobs/open/<literal>` with neither pattern more
specific, so Go's `ServeMux` **panics at registration** and the process does not start — a
`make run` that dies rather than a 404 somebody debugs. **Reproduced in a standalone program rather
than inferred from the documentation.** `POST` escapes only because the conflicting route is a `GET`;
five segments or more are safe, because the literal route has only three after `/v1`.

**The interim convention is the shelf SHIP-115 took** — `GET /v1/jobs/{id}/delivery/proof`, a
domain's own segment under the job — and it composes, which is why it is a convention rather than a
workaround. **The structural fix is moving the open feed off the `{id}` slot**, to something like
`/v1/jobs/open` and `/v1/open-jobs/{id}`, and it is a breaking contract change that gets cheaper the
earlier it is made: today it costs one path in `contracts/paths/`, one route file, one Dart client
method and one verify section. **It lands directly on the entry above**, because a provider's job
detail, a milestone timeline, SHIP-133's tracking view and SHIP-152's admin read are all
four-segment `GET`s in their most natural form, and every one of them will otherwise take the shelf
or take an hour finding out why the process will not start.

**An unmapped `internal_error` sits on a reachable award path, and it was found and deliberately not
fixed.** When `acceptBid`'s compare-and-set matches nothing — which happens if the rejection sweep
were ever to run before the accept — the award produces an error that is none of `bidding`'s
sentinels, so `httpx.WriteError` falls through to a `500 internal_error`. `postgres.go`'s own comment
says that path *should* "fail loudly and roll back", so the behaviour reads as intended rather than
as a bug: the alternative is a wrong award. **What is not intended is the response**, because the
published contract describes no `internal_error` for this endpoint and a client cannot tell it from
the service being broken. Found by SHIP-95's author, who correctly did not fix it — a blind suite's
job is to report what it can see, and changing an error mapping is the implementation's decision.
**Whoever next opens `internal/bidding` should decide it**: either a sentinel and a mapped code, or a
comment saying the 500 is the intended answer and why. §9's `httpx.WriteError` entry means the cause
is at least logged with the request ID now.

**`git checkout <file>` on an unstaged tree destroys the work it is meant to protect.** Two lanes
lost work this way mid-mutation, and the shape is the same both times: a mutation is applied to a
file that also holds an hour of uncommitted work, the run finishes, and the obvious revert takes the
file back to the index — discarding the mutation and everything else in it together. It is the first
command anybody reaches for and it is wrong here, which is why writing it down is worth more than
remembering it.

**The recipe both lanes converged on**: copy the file aside or tar-snapshot the tree before applying
anything, restore from the copy rather than from git, and confirm with `git diff` **and a checksum**.
The checksum is the part that is easy to drop and is the reason the recipe works — after a
destructive `git checkout` the file matches the index exactly, so `git diff` reports nothing and
reads as success. **It wants a line in `CLAUDE.md` beside the existing mutation guidance**, which is
a shared surface this pass may not open.

**`STORAGE_DOWNLOAD_TTL` does not exist, and one TTL serving both directions errs long.**
`PresignTTL` signs the upload URL and, since SHIP-115, the download URLs as well. The two have
different requirements and only one of them is generous: an upload link has to outlast a phone
finishing a slow PUT on a bad connection, while a download link only has to outlast an image
rendering. One number serving both makes every read link longer-lived than it needs to be, and every
one of those is a live link to a photograph of somebody's front door. **A prep-ticket item** — it is
an `internal/config` field, which a domain branch may not add — and SHIP-115 and SHIP-118 both
recorded the request rather than parking a flag.

**A `Docs/06` row described an implementation that never existed, and the pattern is the finding
rather than the row.** §4.1's object-storage line read "Local storage in development, S3 deployed"
from SHIP-10 until this wave, and `git log --all -- '*platform/storage/local.go'` returns **zero
commits**. It was corrected on the proof branch under the owner's ruling, and §3's SHIP-114 entry has
the reasoning: once the local stack runs a real S3-compatible store, writing a filesystem
implementation *so that* the answer to §4.1's own test became yes would have inverted the test.

**This is the fourth documented-but-absent mechanism in this repository**, after `httpx.RegisterCode`,
`httpx.H`/`DecodeJSON` and the worktree test isolation — and §7e already names that as the recurring
defect here, in the sentence "a mechanism this file claims exists is one nobody checks for". What is
new is the *duration*: the other three were found within a wave or two of being written, and this one
survived six waves. The reason is worth naming because it generalises: **nothing checked it because
nothing needed object storage until something did.** A documented mechanism with no consumer is
unfalsifiable until the first consumer arrives, so the audit worth doing is not "is this true" but
"what does this document claim that no code has ever exercised".

**Two smaller ones, recorded so they are not rediscovered.** §3's M4 summary table had `SHIP-129`
above `SHIP-126` — an index-ordering slip that arrived with the Flutter merge rather than a lost row.
**Corrected in this pass**, and recorded rather than tidied silently because of *why* it survived:
`make status` checks that every done ticket **has** a row and cannot see where the row sits, so an
ordering slip in this table is invisible to every guard there is and is found only by somebody
reading. And **two more Kafka false failures** happened this wave, both `make verify` runs
failing on another worktree's publications into the shared `shipper.job` topic. `CLAUDE.md`'s
worktree row is right about there being no isolation to add, the fence-on-an-id rule is right, and
the cost is a re-run rather than an investigation — which is only true because the row exists to be
read. Nothing to fix; two data points that the row is load-bearing.

**§4.1's "does a second implementation exist today?" test is looser than the new paragraph implies,
and this is recorded rather than acted on.** Two of the five adapter rows justify themselves with a
*test double* — push is "Firebase Cloud Messaging, plus a no-op used in tests" and geocoding is
"Provider-backed, with a stub for tests" — so by the standard those rows set, a storage test double
would have counted and object storage would have passed the test the ordinary way. **What actually
distinguishes object storage is the other argument in that paragraph**: a filesystem version needs a
second signing scheme and a route serving the bytes, which contradicts `Docs/06` §5.2's rule that
files are never proxied through the API. That makes it a different architecture rather than a second
implementation, which is a stronger reason and is the one the row should rest on. **Nobody should
rewrite §4.1 on the strength of this** — the rows are right, the test is a heuristic, and tightening
it would put push and geocoding in question for no benefit. It is here so that the next person to
weigh an adapter against that test knows the test does not decide it on its own.

## 10. The done list, in a form a script can read

**The list is `Docs/11-done.txt`**, one ticket per line. It is still authoritative and it is
still updated in the same change that finishes a ticket — it has simply moved out of this
document. `make status` reads it, counts it against the backlog, and cross-checks it against
what commit subjects claim.

A ticket belongs there only when its *Done when* line in `Docs/09` is demonstrable. **Every ticket §4 names is now in the list**, and SHIP-149, SHIP-77 and SHIP-118 are the three live rows in it — which is
not a contradiction to be tidied away.

**A ticket can be both**, and this is the shape: it landed, it is named by a commit subject, and one
clause of what it was supposed to deliver belongs to a ticket that does not exist yet. SHIP-149
shipped the append-only `audit_log` and its triggers; the Go write helper is still missing. SHIP-77
shipped the customer's job detail screen and a timeline derived from the current status; the
transition history its sentence implies is served by no endpoint, and no ticket adds one (§9).
SHIP-65 was the example for two waves — the job detail endpoint and the owner-only rule shipped, the
`budget` field its sentence also names did not, because adding the column before the proof it cannot
leak would have been exactly the wrong order. **SHIP-67 closed it**, which is what this shape is
supposed to end in.

**SHIP-118 is a variant of the shape rather than another instance, and §4 says which.** Its own
*Done when* — "Delivered is rejected without either; verified by test" — is met in full. What is
short is the field set two documents require of a delivered job, and **the ticket that adds it exists
and is numbered**: SHIP-123. So this row is in the list for the ordinary reason and is in §4 so that
nobody reads `Docs/01` §4.4 as a description of what the platform stores today.

**Removing any of them from the list would make `make status` hard-fail**, not go quiet: a commit
subject names each (`a47ba3a` for SHIP-65), and the script exits 1 when git shows a ticket the list
does not declare. The list is the floor of what landed; §4 is where the nuance lives. Keep them in
both.

`Docs/11-done.txt`'s own header still names SHIP-149 as the only such row, and this pass
deliberately left it byte-identical: it added no ticket to the list, and **prose in a union-merged
file is the one thing that file keeps short** — the header has already been duplicated against
itself once by a union. Correct that sentence in the next change that adds a ticket to the list, not
in a pass that adds none.

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
