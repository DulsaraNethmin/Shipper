# Shipper

Australian road-transport marketplace. Customers publish delivery jobs, verified transport providers bid privately, the customer awards one, and the delivery is tracked to completion. Marketplace only — Shipper never holds payment in the MVP.

## Start here

**`Docs/11-delivery-status.md` says where the work is** — what is done, what is only half done, what is blocked on something outside this repository, and what is safe to start next. Read it before anything else, and run `make status` to see it counted against the backlog and the commit history.

This file deliberately does **not** repeat that state. An earlier version of this paragraph tried, and described the project as sitting at `SHIP-9` for the entire time `SHIP-10` to `SHIP-15` was being written. One place, kept current, beats two that disagree.

What is stable enough to say here: **M0 is under way, and no domain logic exists yet** — the eight packages under `services/core/internal/` still hold their documentation and nothing else. Work proceeds ticket by ticket through `Docs/09`; `Docs/10` says how to write it.

## Documents are the source of truth

Read the relevant document before implementing. These are decisions, not suggestions — most were argued through and closed deliberately.

| Doc | Covers |
|---|---|
| `Docs/01` | Requirements, scope, permissions, NFRs, release gate |
| `Docs/02` | Job status model, transitions, offline rules |
| `Docs/03` | User journeys |
| `Docs/04` | Verification and moderation |
| `Docs/05` | Policy and legal positions |
| `Docs/06` | Architecture, adapter rules, roadmap |
| `Docs/07` | Flutter client architecture |
| `Docs/08` | Build order and repo structure |
| `Docs/09` | The ticket backlog |
| `Docs/10` | Engineering conventions — the implementation decisions two people would otherwise answer differently |
| `Docs/11` | **Delivery status — read this first.** What is done, what is half done, what is blocked, what to start next |

If something contradicts a document, the document wins — or the document needs updating first. Do not resolve a contradiction silently in code.

## Stack

| Layer | Technology |
|---|---|
| Customer + provider app | Flutter (iOS + Android), single app, role chosen at signup |
| Admin panel | Next.js (web) |
| Driver portal | Next.js (web), link-authenticated, no account |
| API + core platform | Go |
| Database | PostgreSQL |
| Cache / tokens / idempotency | Redis |
| Events | Kafka |
| Object storage | S3 — proof photographs and verification documents, private, reached only by short-lived pre-signed URL. MinIO in development, which speaks the same API |
| Push | Firebase Cloud Messaging |
| Cloud / observability | AWS / Datadog |

There is **no BFF tier**. The Go platform owns the versioned public API directly (`Docs/06` §2.1).

## Invariants — violating these is a defect, not a style choice

- **A customer's budget is never exposed to a provider.** Not as an amount, band, or "budget supplied" flag, through any endpoint or response. Enforced server-side (`Docs/01` §4.3).
- **Exactly one accepted bid per job**, enforced by a database constraint, not application logic alone (`Docs/02` §3).
- **Job status is never a settable field.** All transitions pass one guarded function (`Docs/02` §2).
- **No authorisation decision on the device.** The app may hide or disable; the platform decides (`Docs/07` §3).
- **Every state-changing endpoint accepts an idempotency key.** Mobile clients retry after dropped connections; retries must not duplicate bids, milestones, or proof.
- **Delivered requires photo proof or a recorded exception reason** — never neither (`Docs/01` §4.4).
- **The driver's job-scoped token and the mobile auth token are separate systems.** Neither can be exchanged for the other. The driver token grants access to exactly one job.
- **Audit entries are append-only.** Ordinary administrators cannot delete them.

## Architecture rules

- **Adapters wrap external integrations only** (email, SMS, push, object storage, geocoding), and only where a second implementation exists today. `Docs/06` §4.1.
- **Do not abstract PostgreSQL.** The partial unique index enforcing one-accepted-bid and the row locking in the award transaction are load-bearing and PostgreSQL-specific. Test against a real database, not mocked repositories.
- **Interfaces are declared by the consuming domain**, never by the implementing package. `delivery/ports.go` declares what delivery needs; the storage package knows nothing about delivery.
- **Domain packages do not import each other**, adapters do not import domains, and domains do not import adapters. Enforced by `make lint-imports` and by a test (SHIP-11). A domain and its adapters meet in `cmd/api` and nowhere else.
- **Infrastructure imports neither a domain nor an adapter** (SHIP-15c). This is the rule easiest to break by accident and hardest to see afterwards: every domain imports `httpx`, so one import of `identity` from inside `httpx` welds all eight to it through an edge that is in no domain's own files. Infrastructure takes a function or an interface it declares itself, and `cmd/api` supplies the closure.
- **Domain events are emitted by the domain, not the API layer.**
- **Anything expected to change under operational pressure lives server-side** — category lists, validation limits, policy copy, feature switches. Flutter has no over-the-air update path for Dart code.

## Working a ticket

Tickets are `X-1`…`X-9` (non-code, external) and `SHIP-1`…`SHIP-185`, in `Docs/09-delivery-backlog.md`. They are in **strict build order** — the next ticket is the lowest-numbered one still open. Dependencies are listed per ticket and never point forward.

Each ticket has a **Done when** line. That is the acceptance criterion; if it cannot be demonstrated, the ticket is not done.

When implementing, state which ticket you are working and check its listed dependencies are complete.

## Git workflow

### Branches

| Branch | Role |
|---|---|
| `main` | Release branch. Only ever updated by a pull request from `develop` |
| `develop` | Stable integration branch. Ticket work merges here |
| `<ticket-id>-<slug>` | One per ticket, branched from `develop` |

**Never commit directly to `main` or `develop`.**

Ticket branches are lowercase, ticket ID first so they sort into build order:

```
ship-39-refresh-token-rotation
ship-92-award-transaction
x-7-privacy-policy
```

**Branches are never deleted.** They remain as the record of what each ticket touched.

Batch tightly-related tickets on one branch only when they form a single reviewable change — `ship-28-33-identity-schema` is reasonable; unrelated tickets are not. `SHIP-91`…`SHIP-95` (the award transaction) get their own branch with nothing else on it.

### Commits

**Claude never runs `git commit`.** Stage nothing, commit nothing. When work is complete, write the proposed commit message to a scratch file and hand it over — the repository owner makes every commit.

This keeps authorship and co-authorship trailers entirely under the owner's control. Do not add `Co-Authored-By` trailers to proposed messages.

Format:

```
SHIP-39: rotate refresh tokens on every use

Each refresh issues a new token and invalidates its predecessor.
Presenting a consumed token invalidates the entire device session.

Done when: each refresh returns a new token and invalidates its
predecessor — verified by TestRefreshRotation.
```

- **Subject:** `<TICKET-ID>: <imperative summary>`, under 72 characters.
- **Body:** why, not what. The diff shows what changed.
- **Close with the *Done when* line** and how it was verified. If that cannot be written honestly, the ticket is not finished.
- One logical change per commit. A commit that needs "and" in its subject is two commits.

### Definition of done

A ticket is done when **all** of these hold:

1. The *Done when* criterion in `Docs/09` is demonstrable — not merely believed.
2. CI is green: build, vet, analyzer, tests.
3. Tests exist where the ticket implies behaviour. `SHIP-95` is not optional.
4. No invariant above is violated. Check the list if the ticket touches bids, status, auth, or proof.
5. Docs are updated if the work changed or revealed a decision. A code change that contradicts a document is not done until the document is corrected.
6. Nothing in the *never commit* list below is in the diff.

Meeting all six makes the branch **ready to merge**, not merged. Hand it to the repository owner.

### What Claude does and does not do

| Action | Who |
|---|---|
| Write code, create branches, edit files | Claude |
| **`git commit`** — **on its own `ship-*` / `x-*` branch only** | Claude |
| `git commit` on `main` or `develop` | **Never** |
| **`git merge`** — ticket branches and pull requests alike | **Owner only** |
| **`git push`** | Owner, unless explicitly asked |

**The commit rule is branch-scoped, and that is a deliberate narrowing.** The original rule — stage nothing, commit nothing — was written for one agent handing one branch to one owner. It does not survive several branches being built at once: the owner becomes the serialisation point for every one of them, which is the bottleneck the parallelism exists to remove. And the Definition of Done requires green CI, which runs on commits.

So Claude may commit to a branch it created whose name matches `ship-<n>` or `x-<n>`. It may not commit to `main` or `develop`, may not merge anything, may not open or approve its own pull request, and may not add `Co-Authored-By` trailers. **What enters `develop` and `main` remains entirely the owner's decision**, which is what the rule was protecting.

The flow:

1. **Ticket branch → `develop`**, merged with `--no-ff`. **Never squash.** Every commit is preserved, and the merge commit records which ticket the work belonged to. This pairs with keeping branches: the full topology stays inspectable.
2. **`develop` → `main`** by pull request, in release-sized batches rather than one per ticket.

Catch up to `develop` before requesting a merge, and never request one with failing CI.

**Never commit or merge while a gate is running. Run the gate, wait for it, then commit.** `make verify` and `make check` do not only read the tree, they *rewrite* parts of it — the check count in `Docs/11` §3 and `routes_golden.txt` are both files a gate produces — so a commit that races one captures a half-finished tree, and a gate that races a merge reads a half-finished one. Wave 5 paid in both directions in a single session: a gate overlapping a merge produced a **false failure** — a SHIP-79 verify failure and four phantom "declared done with no commit" tickets, on a tree where nothing was wrong — and a merge committed while its gates were still running produced a **false pass**, publishing a `develop` that carried a stale check count of 299 against a true 355 and a union-ordered route table. `ship-15j-wave-5-merge-repair` exists only to undo the second. A false failure costs an hour; a false pass ships.

**Prefer `git merge develop` into the ticket branch over `git rebase` when the branch has touched a shared file.** Resolving a conflict in the route manifest or the error registry is exactly where a route or a code gets dropped, and a rebase rewrites history so the loss leaves no trace. A merge commit keeps the resolution reviewable. `git log --first-parent develop` still gives the one-line-per-ticket view either way.

After resolving any conflict, re-run `make check` **and** look at the golden files — `services/core/cmd/api/routes_golden.txt` is the one that catches a silently dropped endpoint.

**Expect that file to come back reordered rather than conflicted, and do not read a reorder as damage.** It is `merge=union`, which is what prevents a lost route: a union appends both sides in merge order, while the generator emits them sorted. So after a multi-branch wave the content is right and the order is wrong, and `TestRouteTableMatchesGolden` fails on a tree where nothing is missing. **Confirm the sorted set is unchanged, and only then regenerate:**

```
go test ./cmd/api -run TestRouteTableMatchesGolden -update
```

Confirming first is the part that matters. `-update` will just as happily bless a genuinely missing endpoint, which is the single failure this file exists to catch.

### Working in more than one branch at once

Each concurrent piece of work gets its own git worktree, never the primary tree.

| Item | Rule |
|---|---|
| Test database | **Leave `TEST_TEMPLATE_DB` unset.** The `Makefile` derives it from the directory name, and that is the whole isolation mechanism — `CREATE DATABASE … TEMPLATE` resolves at cluster scope, and every worktree shares one cluster. Two worktrees with the same template name are one database: `make test` in either drops it mid-clone in the other. `TEST_DATABASE_URL` does **not** isolate on a shared cluster, whatever an older version of this table said |
| Ports | `HTTP_PORT` and `VERIFY_PORT` per worktree, likewise |
| Compose | One shared stack. `COMPOSE_PROJECT_NAME` is pinned in the `Makefile` so worktrees do not each start their own and fight over 5432, 6379 and 29092 |
| **Kafka** | **There is no isolation, and there is no equivalent to add.** One broker, one `shipper.job`, and **every worktree publishes into the same topics** — the shared stack is safe for PostgreSQL only because each tree gets its own database on the cluster, and a topic has no such split. So **on a Kafka topic a fence must be an id, not a timestamp**: a concurrent run in another worktree is not ordered against this one, and a `published_at > $fence` window contains that run's events as readily as your own. Wave 5 lost a run proving it — a count over a topic failed on a tree where nothing was wrong, and the count was right about what it saw |
| **Object storage** | **`STORAGE_BUCKET` in `deploy/.env`, one per worktree — and this is the one shared service that genuinely can be split, which is why the row exists rather than repeating the Kafka one.** A bucket is a namespace the store makes on demand, so five trees get five buckets on one MinIO and never see each other's objects; a topic is created from the event catalogue, shared by every tree, and has no per-tree equivalent. `make up` creates whatever `STORAGE_BUCKET` names, so a tree that sets the line has isolation from its next `make up` and needs nothing else. **Unlike `TEST_TEMPLATE_DB` it is not derived from the directory**, so a tree that leaves it alone shares `shipper-dev` with every other tree that did — which is safe for keyed reads and writes and is not safe for a count or a listing. Set it, or fence on an id |
| **`git stash`** | **Never.** The stash is shared across worktrees through one `.git`, and this repository already carries the scar — `CLAUDE.md` was committed with `Stashed changes` conflict markers in it |
| Shared files | Do not edit from a domain branch: `cmd/api/routes.go`, `internal/boundaries/boundaries.go`, `internal/httpx/**`, `go.mod`, the root `Makefile`, migrations in the shared block, `scripts/verify-foundation.sh`, `CLAUDE.md`, `Docs/**`. A domain's own `scripts/verify/<n>-<domain>.sh` is not shared — that is what the split is for. See `Docs/10` §9.2 |

Because history is not squashed, `git log --oneline` shows every individual commit. For the one-line-per-ticket view, use:

```
git log --first-parent develop
```

### Reverting a mutation

Breaking a mechanism deliberately to confirm a test fails is how this repository establishes that a guard is real rather than believed — every `Docs/11` §3 entry that claims one carries the mutation and its outcome. The applying is easy. **Undoing it is where two lanes in wave 7 destroyed an hour of uncommitted work each, in the same way.**

**Never revert a mutation with `git checkout <file>`.** It is the first command anybody reaches for and it is wrong on an unstaged tree: it restores the file *from the index*, which discards every uncommitted change in that file — the mutation and the work sitting beside it, indistinguishably. The mutation is deliberate and reversible; the hour of work next to it is neither.

The recipe:

1. **Copy the file aside before applying anything** — `cp path/to/file /tmp/snap/`, or tar-snapshot the tree if the mutation touches several. Record its checksum at the same time.
2. Apply the mutation, run the test, read the failure.
3. **Restore from the copy, not from git** — `cp /tmp/snap/file path/to/file`.
4. **Confirm with `git diff` *and* the checksum.**

**Step 4's checksum is the part that is easy to drop and is the whole reason the recipe works.** After a destructive `git checkout` the file matches the index exactly, so `git diff` reports nothing — which reads as success and is in fact the signature of the failure. Only a checksum against the copy you took distinguishes "restored" from "reverted to the last commit".

The same argument bans `git stash` here twice over: the worktree table above rules it out because the stash is shared through one `.git`, and it is the wrong instrument for this regardless.

### Never commit

Signing keys, keystores, provisioning profiles, service-account JSON, `.env` files, or any credential. These belong in the CI secret store (`Docs/06` §5.2). `.gitignore` covers the known cases, but check the diff — a leaked signing key means rotating it everywhere it was trusted.

Also avoid committing generated artefacts, `node_modules`, build output, and local database volumes.

### Reviewing agent-written code

There are two review gates and both belong to the repository owner: the merge into `develop`, and the pull request into `main`. Neither is performed by Claude.

Read the diff at the first gate, not the second — by the time work reaches a `develop` → `main` pull request it is batched with other tickets and individual mistakes are harder to see. Pay particular attention to anything touching the award transaction, token handling, or the budget-privacy invariant, where a plausible-looking implementation can be quietly wrong.

## Conventions

- **Australian English** in documents and user-facing copy — *authorisation*, *minimise*, *organisation*. The existing docs are consistent; match them.
- Currency is AUD. Distances are kilometres. Dates in user-facing copy are day-first.
- Job statuses use the exact names in `Docs/02` §1.
- Ticket IDs in commit messages and branches use the **backlog ID** from `Docs/09` (`SHIP-39`, `X-7`), which is what every document references.
- **These are not the same as Jira's issue keys.** Jira assigns its own keys on import, offset by the epic rows, so backlog `SHIP-1` is not Jira `SHIP-1`. The backlog ID is preserved in each Jira summary as `[SHIP-1] …` — search on that to cross-reference.

## Commands

All run from the repository root. `make` with no target lists them.

```
make up             Start Postgres, Redis, Kafka and the object store, waiting until each
                    is healthy, then create this worktree's bucket
make down           Stop the stack, keeping data
make reset          Stop the stack and destroy all data
make ps / logs      Stack status; follow stack logs
make storage-bucket Create this worktree's object-storage bucket, private. Idempotent, and
                    already run by `make up`

make migrate-up     Apply all pending migrations
make migrate-down   Reverse the last migration (make migrate-down n=all for everything)
make migrate-version
make migrate-create name=<snake_case_name> domain=<domain>

make run            Run the API on the host
make build          Build bin/shipper-api and bin/shipper-migrate
make test           Build the test template, then go test ./... -race
make test-db-template  Rebuild the database every integration test is cloned from
make vet
make lint-imports   Check the domain boundaries (SHIP-11)
make lint-spelling  Check Australian English
make check          vet + lint-imports + lint-spelling + test — what CI runs

make verify         Demonstrate every foundation ticket's acceptance criterion end to end
make psql / redis   Open a shell against the local database or cache
make kafka-smoke    Create a topic, produce, consume, delete
```

A typical start: `make up && make migrate-up && make run`, then
`curl localhost:8080/health`.

Configuration is entirely environment-driven. `deploy/.env.example` documents every
variable; copy it to `deploy/.env` to override anything locally. Note that `deploy/.env`
is read by `make`, not by the binaries — run through the make targets or export the
variables yourself.

`make verify` needs two host clients the stack does not provide:
`brew install libpq redis`.

**`migrate-create` requires `domain=`.** Migration numbers are allocated in reserved
per-domain blocks so two branches cannot draw the same one; the ranges are in
`services/core/migrations/blocks.go` and two tests enforce them.

**Integration tests need the stack.** They clone a template database rather than migrating
one per run, and they **fail rather than skip** when PostgreSQL or Redis is missing — a test
that quietly does not run is worse than one that does not exist, because it is counted. Run
`make test`, not bare `go test`: `deploy/.env` is read by `make` alone, so `go test` on its own
looks for PostgreSQL on the default port. `go test -short` skips them deliberately.

The Flutter, admin, and driver-portal commands arrive with SHIP-16, SHIP-22, and SHIP-23.

## Repository layout

Per `Docs/08` Step 1. One repository, four deployables:

```
apps/mobile/          Flutter — placeholder until SHIP-16
apps/admin/           Next.js — placeholder until SHIP-22
apps/driver-portal/   Next.js — placeholder until SHIP-23
services/core/        Go — the versioned public API and domain
  cmd/api/            entrypoint, wiring, graceful shutdown, the route manifest
  cmd/migrate/        migration tool, migrations embedded in the binary
  cmd/lintboundaries/ the domain boundary lint
  internal/           the eight domains: identity, profiles, fleet, jobs,
                      bidding, delivery, notifications, admin
  internal/platform/  integration adapters: email, sms, push, storage, geocoding
  internal/authctx/   the authenticated subject, readable by every domain
  internal/boundaries/  the import lint rules
  internal/buildinfo/ version and commit, injected at link time
  internal/clock/     the injectable clock
  internal/config/    environment configuration
  internal/db/        the Runner seam and the transaction helper
  internal/events/    the outbox writer and the domain event type
  internal/httpx/     middleware: request ID, logging, recovery, error contract,
                      idempotency, and the JSON helpers
  internal/idempotency/ the Redis-backed idempotency store
  internal/logging/   slog handler construction
  internal/testsupport/ pgtest and redistest — real infrastructure for tests
  internal/validate/  field-level validation in the error contract's shape
  migrations/         SQL schema history, in reserved per-domain blocks
deploy/               docker-compose for local Postgres, Redis, Kafka, MinIO
scripts/              verify-foundation.sh — the acceptance harness; the checks are
                      one file per milestone or domain in scripts/verify/, so a track
                      adds a file and edits none. Also check-spelling.sh and
                      delivery-status.sh
mk/                   per-track make targets, glob-included by the root Makefile
.github/workflows/    go.yml (SHIP-20); Flutter and the store pipelines follow
```

Two of the eight domain packages now hold logic — `identity` (passwords, tokens, sessions,
rate limiting) and `jobs` (locations, the store, the handlers) — and the other six hold
documentation and nothing else. Their boundaries were enforced from before they filled up,
because `Docs/08` is right that they are almost impossible to reintroduce later, and the
first two domains to fill have not needed to cross one.

Adding a package directly under `internal/` fails the lint until it is classified as a
domain or as infrastructure in `internal/boundaries/boundaries.go`. That is deliberate —
it makes a ninth domain a decision someone recorded rather than something that happened.

The infrastructure list is **seeded ahead of the code**, and the mechanism has now been
demonstrated rather than asserted: `ratelimit` (SHIP-47) and `pagination` (SHIP-66) were
written in the same wave, in different lanes, **with no shared-file edit between them**. Only
`money` remains registered and unwritten. Whoever first needs one writes the package and edits
nothing shared. A domain that needs internal structure uses a sub-package — `internal/jobs/expiry`
is attributed to `jobs`, may import it, and needs no registration.

## API conventions

- **Every failure uses the standard error contract** — one shape, a machine-readable
  `code`, and the request ID in the body (SHIP-12). Clients branch on `code`, never on
  `message`. See `services/core/README.md` for the shape and the code list.
- **Product endpoints live under `/v1`**; operational endpoints do not (SHIP-13).
- **Every state-changing request carries an `Idempotency-Key`** and is refused without one
  (SHIP-15). The middleware fails closed if Redis is unreachable.
- **Routes are declared, not registered.** A domain adds `cmd/api/routes_<domain>.go` with an
  `init` that calls `register(Route{…})`, and edits no shared file. Every route states its
  auth class. `routes_golden.txt` records the whole served surface, because a route dropped in
  a merge produces no compile error and no test failure — only a missing endpoint.
- **Handlers live in the domain**, in `http.go`, not in `cmd/api`. A domain importing
  `internal/httpx` is sitting on infrastructure, not crossing a boundary.
- **Log through `httpx.LoggerFrom(ctx)`**, which is already bound to the request ID
  (SHIP-14). Outbound HTTP clients wrap their transport in `httpx.PropagateRequestID`.

### The one hard gate before authenticated endpoints

**No authenticated state-changing endpoint may merge before SHIP-44 supplies the idempotency
middleware's `scope`.** It is wired with `nil` today, so every key lands in
`idem:v1:anonymous:<key>` — harmless while nothing is authenticated, and a cross-tenant read the
moment something is: a client that guesses another client's key gets that client's response
body. SHIP-44 passes the authenticated subject and closes it. Until then, protected endpoints
wait.

Path-filter CI workflows from the first commit — macOS runners for iOS builds cost roughly ten times Linux minutes, and a Go-only change must not trigger one.
