# Shipper

Australian road-transport marketplace. Customers publish delivery jobs, verified transport providers bid privately, the customer awards one, and the delivery is tracked to completion. Marketplace only — Shipper never holds payment in the MVP.

**There is no application code yet.** This repository is currently nine planning documents. Work proceeds ticket by ticket through `Docs/09-delivery-backlog.md`.

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
- **Domain packages do not import each other.** Enforced by lint (SHIP-11).
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

### Merging

**Claude never merges anything.** Not ticket branches, not pull requests. Prepare the branch, push it, open the pull request if asked — then stop. Every merge is performed by the repository owner.

The flow:

1. **Ticket branch → `develop`**, merged with `--no-ff`. **Never squash.** Every commit is preserved, and the merge commit records which ticket the work belonged to. This pairs with keeping branches: the full topology stays inspectable.
2. **`develop` → `main`** by pull request, in release-sized batches rather than one per ticket.

Rebase on `develop` before requesting a merge. Never request a merge with failing CI.

Because history is not squashed, `git log --oneline` shows every individual commit. For the one-line-per-ticket view, use:

```
git log --first-parent develop
```

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

None yet — M0 has not been built. Once `SHIP-1`…`SHIP-7` land, this section should list the local stack, migration, build, and test commands. Update it as part of those tickets.

## Repository layout

Planned in `Docs/08` Step 1. One repository, four deployables:

```
apps/mobile/          Flutter
apps/admin/           Next.js
apps/driver-portal/   Next.js
services/core/        Go
deploy/               docker-compose for local Postgres, Redis, Kafka
```

Path-filter CI workflows from the first commit — macOS runners for iOS builds cost roughly ten times Linux minutes, and a Go-only change must not trigger one.
