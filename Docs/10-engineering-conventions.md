# Shipper — Engineering Conventions

**Status:** Draft  
**Audience:** Engineering  
**Purpose:** Fix the implementation decisions that more than one person, or more than one agent, would otherwise answer differently.

## 1. What this document is for

`Docs/06` sets architectural direction and `Docs/08` sets build order. Neither settles the questions that arise the moment a second person writes code at the same time as the first: which driver, which layering, where handlers live, how a cursor is encoded, what a domain error code is called.

Left open, those questions get answered independently and inconsistently, and the inconsistency is expensive to unpick later because it is spread across every package. This document closes them once.

**Standing.** Same as every other document here: if code contradicts this, the document wins, or the document is amended first. A convention that has become wrong is corrected here in the same change that corrects the code.

**Scope.** This is *how*, not *what*. Product decisions stay in `Docs/01`–`05`; architecture stays in `Docs/06`–`07`; the ticket queue stays in `Docs/09`.

## 2. The Go service

### 2.1 Package layout inside a domain

`Docs/08` Step 1 specifies `service.go`, `ports.go` and `postgres.go`. It gives HTTP handlers no home, which is the gap that produced this section. The full layout:

| File | Holds |
|---|---|
| `doc.go` | What the domain owns, its invariants, its tickets |
| `model.go` | The domain's types and its enum constants |
| `errors.go` | This domain's error codes and its sentinel errors |
| `ports.go` | Interfaces this domain requires **of other domains and of adapters** |
| `service.go` | Domain rules and transaction boundaries |
| `postgres.go` | Persistence — concrete, unexported, not behind an interface |
| `http.go` | Handlers and `RegisterRoutes` |

Handlers live in the domain rather than in `cmd/api` for a specific reason: it keeps `cmd/api/routes.go` free of per-domain edits, which is what allows two domains to be built at once without touching a shared file. A domain importing `internal/httpx` is infrastructure, not a boundary crossing, so the import lint permits it.

A domain may use sub-packages (`internal/jobs/expiry`). `boundaries.Classify` attributes a sub-package to its parent domain, so `jobs/expiry` may import `jobs`, and the closed lists in `internal/boundaries/boundaries.go` need no edit.

**Amends `Docs/08` Step 1 and `services/core/README.md`,** both of which list the layout without `http.go`.

### 2.2 Layering

Two layers, not three. `service.go` holds the rules and owns the transaction; an unexported `postgresStore` holds the SQL. **There is no repository interface.**

`Docs/06` §4.1 is explicit that PostgreSQL is not abstracted: the partial unique index enforcing one accepted bid and the row locking in the award transaction are load-bearing and PostgreSQL-specific, and a mock happily accepts the write the real constraint exists to reject. `ports.go` is for what the domain needs *from elsewhere* — an email sender, a verification check on another domain — and for nothing else.

### 2.3 Interfaces are declared by the consumer

Restating `Docs/06` §4.1 because it is the rule that makes concurrent work possible: `identity/ports.go` declares what identity needs of an email sender, and `internal/platform/email` knows nothing about identity. Go satisfies interfaces structurally, so the two are written independently and meet in `cmd/api`.

The import lint fails the build in both directions.

## 3. Persistence

### 3.1 Driver and SQL

**`github.com/jackc/pgx/v5` with `pgxpool`, used natively rather than through `database/sql`.** The award transaction needs `SELECT … FOR UPDATE`; the outbox will want `LISTEN/NOTIFY`; `uuid`, `numeric` and `jsonb` all round-trip without adaptation. `lib/pq` remains in the module only as an indirect dependency of the migration tool.

**SQL is written by hand** in each domain's `postgres.go` and scanned with `pgx.CollectRows` and `RowToStructByName`. Code generation was considered and rejected: it fights hardest at exactly the two queries that matter most — the dynamic eligibility filter (SHIP-81) and filtered pagination (SHIP-66) — and a generated directory is a merge hazard when several branches are open at once. The compensating control is that every repository method has an integration test against a real database, which `Docs/06` §4.1 already requires.

### 3.2 Transactions, including across domains

`internal/db` declares:

```go
type Runner interface {
    Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
    Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
    QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
```

satisfied by both `*pgxpool.Pool` and `pgx.Tx`. **Every persistence method takes a `Runner` as its first argument after `ctx`.** `db.InTx(ctx, pool, fn)` opens the transaction and owns rollback, including on panic.

Three rules:

- **The transaction belongs to the domain that owns the invariant.** The award is one transaction and `bidding` owns it, even though it also moves the job.
- A port that must participate in a caller's transaction takes a `Runner`. This is how `bidding` and `jobs` share one transaction without either importing the other.
- **Nested `InTx` is forbidden.** A method that needs a transaction takes a `Runner` and trusts its caller.

### 3.3 Schema conventions

| Concern | Convention |
|---|---|
| Primary keys | Application-generated **UUIDv7** (`uuid.NewV7()`), column `uuid`, `DEFAULT gen_random_uuid()` as a safety net only |
| Timestamps | **`timestamptz` always**, never `timestamp`. `created_at`/`updated_at NOT NULL DEFAULT now()` |
| `updated_at` | Every mutable table attaches the `set_updated_at()` trigger from `000001_init`. A table with the column and no trigger is a defect, and a test catches it |
| Actor time | `Docs/02` §3.1's actor clock and server clock are **two explicit columns**, never one |
| Money | `numeric(12,2)` in PostgreSQL, minor units as `int64` in Go. **Never a float.** AUD is implied; there is no currency column in the MVP |
| Naming | `snake_case`; plural tables; `idx_`, `uq_`, `ck_`, `fk_` prefixes on constraints and indexes |
| Foreign keys | `ON DELETE RESTRICT`, and every foreign key is indexed |
| Deletion | **No soft deletes.** SHIP-171 pseudonymises rather than deletes, so a cascade would destroy the transaction record `Docs/05` §3.1 requires retaining |

UUIDv7 rather than a sequence because identifiers appear in URLs and in the driver-portal link, so a sequential integer leaks volume and invites enumeration. Generating in Go rather than in PostgreSQL because the application needs the identifier *before* the insert — both the outbox and the idempotency store do.

### 3.4 Enumerations

**`text` with a `CHECK` constraint. Not a PostgreSQL `ENUM` type.**

`ALTER TYPE … ADD VALUE` cannot run inside a transaction block alongside its use, and removing or reordering a value means recreating the type and every column that references it. A `CHECK` constraint is an ordinary migration.

Every enumeration is paired with a test that reads the constraint from `pg_constraint` and asserts the PostgreSQL list equals the Go constant list. That test is what stops the twelve job statuses and the eight bid statuses drifting when they are built on different branches.

**Status values are stored exactly as `Docs/02` §1 writes them**, spaces and sentence case included: `'Draft'`, `'Open'`, `'Negotiating'`, `'Awarded'`, `'Driver assigned'`, `'En route to pickup'`, `'Picked up'`, `'In transit'`, `'Delivered'`, `'Completed'`, `'Cancelled'`, `'Disputed'`. Storing the document's own strings means the Go, Dart and TypeScript copies can be diffed against the document rather than against each other.

### 3.5 Migrations

Migrations are numbered in **reserved per-domain blocks** so two open branches cannot claim the same number:

One block per domain, in `Docs/06` §3 order. The registry is `migrations/blocks.go`.

| Block | Owner |
|---|---|
| `000001`–`000099` | Shared: extensions, helpers, `users`, `audit_log`, `outbox` |
| `000100`–`000199` | identity |
| `000200`–`000299` | profiles |
| `000300`–`000399` | fleet |
| `000400`–`000499` | jobs |
| `000500`–`000599` | bidding |
| `000600`–`000699` | delivery |
| `000700`–`000799` | notifications |
| `000800`–`000899` | admin |

Create one with `make migrate-create name=<snake_case> domain=<domain>`, which takes the next free number **within that block**. golang-migrate requires versions to be unique and ascending, not contiguous, so gaps are harmless; and because block order follows dependency order, a `jobs` migration always applies after the `users` table it references.

Two tests enforce it: no duplicate version, and no migration outside its block. CI additionally runs `up` → `down all` → `up` against an empty database, because two migrations that both alter the same table merge cleanly in git and only fail when applied.

**Migrations on `users`, `audit_log` and `outbox` are shared-surface work.** A domain branch does not alter them.

## 4. HTTP

### 4.1 Route registration

`cmd/api/routes.go` owns `newRouter`, the `Route` type, and the middleware chain. Each domain contributes `cmd/api/routes_<domain>.go`, which registers its routes; `registerV1` iterates the registry. **A new domain adds one new file and edits none.**

A route declares its authentication class rather than relying on where it sits in a tree:

```go
Route{Method: "POST", Pattern: "/jobs", Auth: RequireUser, Handler: …}
```

Three tests guard the surface, and all three belong to whoever owns `cmd/api`:

- **`TestRouteTableMatchesGolden`** renders the entire served surface to `routes_golden.txt`. A merge that drops a route registration produces no compile error; this turns it into a one-line diff.
- **`TestNoMutatingRouteIsPublic`** fails any non-`GET`/`HEAD` route with `Auth: Public` unless it is on the explicit allow-list (register, login, refresh, verify-email, verify-phone).
- **`TestEveryRouteIsInTheContract`** cross-checks the manifest against `contracts/openapi.yaml`.

### 4.2 The middleware chain is load-bearing

The ordering in `newRouter` is deliberate and documented in place: `RequestID` outermost so a log record and a recovered panic are both attributable; `Recover` inside `Logger` so a panicking handler still produces a request line; `StandardErrors` innermost so it sees the 404 and 405 that `ServeMux` writes itself.

`StandardErrors` is applied **twice** — once outside, once inside `Idempotent` — because the idempotency middleware stores what it will later replay, and a replay must be byte-identical to the original response. Normalising after storing would replay the raw form.

**Do not tidy this.** Changing it breaks idempotent replay in a way no test outside `internal/httpx` will notice.

### 4.3 Handlers

Handlers have the signature `func(http.ResponseWriter, *http.Request) error`, adapted by `httpx.H`, which writes any returned error through `WriteError`. Returning an error rather than writing one removes the "forgot to return after writing" defect, which otherwise emits two response bodies.

Request bodies are decoded by `httpx.DecodeJSON` with `DisallowUnknownFields` and a **1 MiB** limit. That limit must stay equal to `maxIdempotentRequestBody`; if they diverge, the fingerprint is computed over a body the handler never saw.

Unknown fields are rejected on requests, to catch client typos. Responses stay additive per `Docs/07` §6 — add fields, never repurpose or remove them, and clients tolerate fields they do not know.

### 4.4 Error codes

`internal/httpx` owns the protocol-level codes — the fifteen that exist today. **Domain-specific codes are declared in the domain that raises them:**

```go
var CodeProhibitedCategory = httpx.RegisterCode(
    "prohibited_category", "The goods category may not be published.")
```

Named `<domain>_<condition>`, lower snake case. A test in `cmd/api` asserts uniqueness across the whole registry and regenerates `Docs/10-api-error-codes.md`, which is the list clients branch on. Conflicts in that generated file are resolved by regenerating it, never by hand.

Clients branch on `code`, never on `message`.

### 4.5 Pagination

Keyset, not offset. Request `?limit=&cursor=`; response:

```json
{ "data": [ … ], "next_cursor": "…", "has_more": true }
```

Default limit 20, maximum 100, both from configuration. Offset pagination duplicates and skips rows when the underlying set changes between pages — on the provider job feed, where new jobs arrive continuously, that is a reportable bug rather than a theoretical one.

### 4.6 Validation

Hand-written validators returning `[]httpx.FieldError`, in `internal/validate`. No struct-tag validation library: the error contract already specifies dotted JSON field paths with their own codes, a library brings a second taxonomy that must be translated into the first, and `Docs/06` §5.3 requires validation limits to be changeable server-side without a deploy — which a compile-time tag cannot express.

### 4.7 Wire format

`snake_case` JSON throughout, matching `built_at` and `request_id` in the endpoints that already exist. Enum values are lower snake case on the wire even where the stored form has spaces. A single resource is returned as a bare object; a collection is returned in the envelope above.

## 5. Identity and tokens

| Element | Position |
|---|---|
| Access token | `golang-jwt/jwt/v5`, HS256, keyset selected by a `kid` header so a key can be rotated by configuration. Claims: `sub`, `role`, `sid`, `iat`, `exp`, `jti`, `iss=shipper`, `aud=shipper-mobile`. TTL 15 minutes |
| Refresh token | **Opaque random, stored hashed** against `device_sessions`. Not a JWT |
| Driver token | Separate signing key material **and** `aud=shipper-driver`, carrying exactly one `job_id`. Audience is checked before anything else |
| Password | argon2id, m=64 MiB, t=3, p=4, 16-byte salt, 32-byte key, stored as a **PHC string** in one `password_hash` column |

**No permissions and no verification state in the access token.** `Docs/07` §3 puts every authorisation decision on the platform, and verification state changes during a session — a cached flag would let a customer who was unverified at sign-in publish a job. SHIP-63 reads verification fresh.

**Asymmetric signing buys nothing here.** There is no BFF (`Docs/06` §2.1); the Go service is the only verifier. A keyset with `kid` gives rotation without the operational weight of key distribution.

**The refresh token is opaque, and PostgreSQL is its record of truth.** Rotation and reuse detection need server-side state regardless, so a self-describing token adds risk without saving a lookup. Redis may hold a denylist as a fast path, but the guarantee in `Docs/02` — that presenting a consumed token invalidates the whole device session — must survive a Redis flush. **This amends `Docs/06` §2.1**, which describes Redis as backing refresh-token state.

**The two token systems are separate by construction.** Different key material and different audiences mean "neither can be exchanged for the other" is a property of the verifier rather than a rule someone remembered. One person writes both verifiers, with a test proving each rejects the other's tokens.

**argon2id parameters are stored with the hash.** The PHC string records `m`, `t` and `p`, so parameters can be raised later and each password upgraded at its owner's next sign-in with no migration — and the code that hashes and the code that verifies cannot silently disagree about them. Tests use a reduced profile from configuration; 64 MiB per hash across parallel tests will thrash a laptop.

### 5.1 The authenticated subject

The subject lives in **`internal/authctx`**, not in `internal/identity`.

This is not a stylistic preference. Every domain needs to read who is calling. If that accessor lives in `identity`, then `jobs`, `bidding` and `delivery` all import `identity`, and every one of them **fails the import lint**. `authctx` is infrastructure, which any domain may sit on.

```go
func Subject(ctx context.Context) (Subject, bool)
```

The context key is unexported, so the only way to populate it is the authentication middleware.

## 6. Cross-cutting infrastructure

| Package | Purpose | Exists |
|---|---|---|
| `internal/db` | `Runner`, `InTx`, pool construction, unique-violation detection | yes |
| `internal/authctx` | The authenticated subject | yes |
| `internal/clock` | `Clock`, `System`, `Fixed` — injected, never `time.Now()` inline | yes |
| `internal/validate` | Field-level validation helpers | yes |
| `internal/events` | The outbox writer and the `Event` type | yes |
| `internal/testsupport` | `pgtest`, `redistest` | yes |
| `internal/pagination` | Cursor encoding and decoding | at SHIP-66 |
| `internal/ratelimit` | One Redis token bucket, used by every limited route | at SHIP-47 |
| `internal/money` | Minor-unit arithmetic | at SHIP-74 |

**All nine are registered in `internal/boundaries/boundaries.go` as infrastructure, including the three that do not exist yet.** That is the point of the pre-seed: the track that first needs a cursor writes `internal/pagination` and nothing else, without editing a file two other tracks are also working around. A registered name with no package behind it is a commitment already made, not an oversight — and adding a *domain* package stays what it should be, a decision someone records.

### 6.1 The event seam

`internal/events` and the `outbox` table exist from the foundation, even though nothing consumes them until SHIP-134.

SHIP-57, SHIP-69 and SHIP-89 all specify that a transition emits an event. Without a seam in place, each is implemented differently and SHIP-136 rewrites all three. Every domain takes an `EventSink` in its constructor and writes through it inside the same transaction as the state change, which is what makes the event durable in the sense `Docs/06` §4 requires.

**The outbox writer is infrastructure, not the `notifications` domain.** A domain writing into a table owned by `notifications` would import it, and fail the lint.

### 6.2 Background work

`cmd/worker`: a ticker plus a `SELECT … FOR UPDATE SKIP LOCKED` claim loop. No external scheduler.

Job expiry (SHIP-68), expiry warnings (SHIP-69), bid expiry (SHIP-89) and the 72-hour auto-complete (SHIP-119) all need this and none of them creates it. `cmd/*` is an entrypoint, so it needs no boundary registration.

### 6.3 Clock

Every constructor that deals in time takes a `clock.Clock`. Four scheduled tasks and every token TTL are otherwise untestable without sleeping, and an inline `time.Now()` is invisible until someone tries to test around it.

## 7. Testing

### 7.1 Against a real database

`Docs/06` §4.1 requires it: "a mock happily accepts a write that the actual constraint would reject."

`make test-db-template` builds `shipper_test_template` once by running every migration into it. `pgtest.DB(t)` then clones it per test binary with `CREATE DATABASE … TEMPLATE …` — a file copy measured in milliseconds, not a migration run — and drops the clone in `t.Cleanup`.

This is needed even with one developer: `go test ./...` already runs packages in parallel, so two packages sharing one database will interfere. `go test -p 4` caps how many do so at once.

`redistest.Client(t)` selects a logical database by package hash and prefixes every key with a per-run nonce. All Redis keys follow `shipper:<version>:<concern>:…`, extending the `idem:v1:` namespacing the idempotency store already uses.

### 7.2 Skipping is not passing

`pgtest` and `redistest` call `t.Skip` **only** when `-short` is set. CI never passes `-short`, and both fail outright when `CI` is set and the dependency is unreachable.

The existing Redis tests skip themselves when Redis is absent, which the CI notes already identify as a trap: they pass by being skipped, which reads as green. A test that quietly does not run is worse than no test, because it is counted.

### 7.3 Demonstrating "done"

`scripts/verify-foundation.sh` demonstrates the acceptance criterion of every foundation ticket end to end, against a running stack. It grows a section per milestone.

**A ticket is not done until its section exists and passes.** `Docs/09` makes *Done when* the acceptance criterion; this is the mechanism that keeps it demonstrable rather than believed.

## 8. Clients

### 8.1 The published contract

`contracts/openapi.yaml`, assembled from per-domain fragments under `contracts/paths/`. A Go test validates real handler responses against the schema.

`Docs/07` §2 already requires the Flutter client to be generated from or validated against a published contract. Until that contract exists, client work is guessing at field names — and the contract is also what allows client work to proceed alongside the endpoint it consumes rather than behind it.

### 8.2 Status enumerations in three languages

`contracts/statuses.yaml` is the source; `make codegen` produces the Go, Dart and TypeScript forms; the generated files are committed and CI asserts `git diff --exit-code` after regenerating.

`Docs/08` names "twelve statuses expressed in three languages" as the reason this is one repository. Generation is what makes that hold.

### 8.3 Flutter

Riverpod for state, `go_router` for routing, `freezed` and `json_serializable` for models, `dio` for transport, and **Drift over SQLite** for the offline queue and cached reads.

Drift specifically because SHIP-124 requires that a queued operation is never silently dropped, which needs transactional local storage rather than a key-value store. **This closes the first two of the four decisions `Docs/07` §9 leaves open.**

Feature folders do not import one another, per `Docs/07` §2; shared behaviour moves to `core/` or `shared/`.

### 8.4 Next.js

App Router, TypeScript in strict mode, a pnpm workspace at the repository root, Tailwind with shadcn/ui. Both web surfaces consume the same public API as the mobile app — the admin panel keeps its own server-side data access for privileged screens, but that is an application detail and not a shared platform tier (`Docs/06` §2.1).

## 9. Working in parallel

### 9.1 File ownership

One person or agent owns a Go package directory at a time; one owns a Flutter feature folder; one owns a Next.js application. Concurrency happens **across** those boundaries, never inside one.

The import lint is what makes this safe on the Go side: two domains physically cannot couple to each other, so two people working in `identity` and `jobs` cannot silently create a dependency between them.

### 9.2 Shared surfaces

These belong to whoever is doing shared-platform work in a given cycle, and are not edited from a domain branch:

`cmd/api/routes.go` · `internal/boundaries/boundaries.go` · `internal/httpx/**` · `go.mod` and `go.sum` · the root `Makefile` · `migrations` in the shared block · `CLAUDE.md` and `Docs/**`

`deploy/.env.example` is hand-written but **machine-checked**: a test reads the loader calls out of `config.go` and fails when a variable is read but not documented, or documented but not read. Generating the file was the alternative and would have needed every key restructured into a declarative table first — a large change to a file several tracks will be editing, for the same guarantee.

The root `Makefile` ends with `-include mk/*.mk`, so a track adds `mk/<track>.mk` and gets its targets without editing the shared file. `make help` greps `$(MAKEFILE_LIST)`, which covers included files, so documented targets appear automatically.

`COMPOSE_PROJECT_NAME` is pinned to `shipper`. Compose otherwise names a project after its directory, so each git worktree would start its own stack and they would fight over ports 5432, 6379 and 29092.

Dependencies are added deliberately, not opportunistically. If a change genuinely needs a new module, that is a request, not a commit — it takes five minutes and avoids a `go.sum` conflict. Never hand-merge `go.sum`: delete it and run `go mod tidy`.

### 9.3 Australian English

`authorisation`, `minimise`, `organisation`, `serialise`. Enforced by `scripts/check-spelling.sh` in CI, because the default for most tooling and most generated text is American.

## 10. What this document changes elsewhere

| Document | Change |
|---|---|
| `Docs/06` §2.1 | Redis is described as backing refresh-token state; PostgreSQL is the record of truth and Redis is a fast-path denylist — see §5 |
| `Docs/07` §9 | State management and local persistence were open; both are decided in §8.3 |
| `Docs/08` Step 1 | The domain file layout gains `http.go` — see §2.1 |
| `Docs/09` | Seven pieces of required work had no ticket; they are added |
| `services/core/README.md` | The layout table gains `http.go` and the infrastructure packages |
