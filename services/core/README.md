# services/core — Go platform

The public versioned API and the authoritative business rules. One deployable holding the
eight domains from `Docs/06` §3 behind clear internal boundaries.

**There is no BFF tier.** This service owns the public API contract directly, and the mobile
app, admin panel, and driver portal all consume it (`Docs/06` §2.1).

## Running it

All commands run from the repository root. See the root `Makefile`.

```
make up            # start Postgres, Redis, Kafka
make migrate-up    # apply migrations
make run           # run the service on :8080
curl localhost:8080/health
```

Configuration is entirely environment-driven — see `internal/config/config.go` for the
authoritative list of variables, defaults, and which ones are required. Copy
`deploy/.env.example` to `deploy/.env` to override anything locally.

## Layout

```
cmd/api/             entrypoint, wiring, graceful shutdown
cmd/migrate/         migration tool, migrations embedded in the binary (SHIP-7)
cmd/lintboundaries/  the domain boundary lint (SHIP-11)
internal/
  identity/          registration, verification, credentials, sessions
  profiles/          customer and provider profiles, verification state
  fleet/             vehicles and eligibility
  jobs/              the job record and its lifecycle
  bidding/           bids, negotiation, the award
  delivery/          assignment, milestones, proof
  notifications/     outbox, consumers, channel dispatch
  admin/             support, moderation, disputes, audit
  platform/          integration adapters — email, sms, push, storage, geocoding
  config/            environment configuration (SHIP-8)
  httpx/             HTTP middleware and helpers (SHIP-9, SHIP-12, SHIP-14, SHIP-15)
  idempotency/       the Redis-backed idempotency store (SHIP-15)
  boundaries/        the import lint rules (SHIP-11)
  logging/           structured logger construction (SHIP-9)
  buildinfo/         version and commit, injected at build time (SHIP-6)
migrations/          golang-migrate SQL files (SHIP-7)
```

The eight domains are the ones in `Docs/06` §3, and the set is closed — a ninth is an
architectural decision, recorded by adding it to `Domains` in `internal/boundaries`.

### Inside a domain package

Per `Docs/08`:

```
jobs/
  service.go     domain logic and rules
  ports.go       interfaces this domain requires of others
  postgres.go    persistence — concrete, not behind an interface
```

The domain packages are documentation only at SHIP-10. The skeleton exists so the
boundaries are enforced before there is code to bend them.

## API surface

### Versioning (SHIP-13)

Everything a client calls is served under `/v1`. Operational endpoints — `/health` today,
readiness later — sit outside it, because they are consumed by load balancers and
monitoring rather than by API clients, and versioning them would break every health check
on the day v2 ships.

```
GET /health     build info; unversioned, and stays that way
GET /v1/        the version this deployment serves
```

More than one version may be live at once: old app builds persist on devices indefinitely
and Flutter has no over-the-air update path for Dart code (`Docs/06` §5.3). A version is
retired when telemetry shows negligible traffic from the builds that need it — not when
the next one ships. Introducing v2 means a second route group beside the first, not an
edit to `apiVersion`.

### The error contract (SHIP-12)

Every failure — a validation error, a panic, `ServeMux`'s own 404 — has one shape:

```json
{
  "error": {
    "code": "validation_failed",
    "message": "The job could not be published.",
    "request_id": "9f2c1b…",
    "details": [{"field": "goods.category", "code": "prohibited_category", "message": "…"}]
  }
}
```

**Clients branch on `code`, never on `message`.** Messages get reworded and translated; a
build that switched on message text would break on a copy edit, from a store build already
installed on devices. The `request_id` is in the body as well as the header because a user
reporting a problem sends a screenshot, and a header does not survive one.

Nothing internal is ever serialised. Attach the underlying failure with `WithCause` and it
reaches the log, not the client.

**The full code list is `Docs/10-api-error-codes.md`, and it is generated** — from the fifteen
protocol codes `internal/httpx` owns plus every code each domain declares for itself:

```go
var CodeProhibitedCategory = httpx.RegisterCode(
    "prohibited_category", "The goods category may not be published.")
```

Declaring one edits nothing shared, which is the point: a single file listing every code in the
platform is a file every domain has to touch. Registration refuses a duplicate, a code that is
not lower snake case, and a code with no description — the description is what the generated
document says the code means, and a client with only a name to go on guesses.

Regenerate with `go test ./cmd/api -run TestErrorCodeDocumentIsCurrent -update`, and resolve a
conflict in that file by regenerating rather than by choosing a side.

### Idempotency (SHIP-15)

Every state-changing request under `/v1` must carry an `Idempotency-Key`. Repeating it
returns the stored original response, marked `Idempotency-Replayed: true`, without running
the handler again.

| Situation | Answer |
|---|---|
| No key on POST/PUT/PATCH/DELETE | `400 idempotency_key_required` |
| Same key, same request, already answered | the original response, replayed |
| Same key, same request, still running | `409 idempotency_request_in_progress` |
| Same key, **different** request | `409 idempotency_key_reused` |
| Redis unreachable | `503 service_unavailable` — it fails closed |

Failing closed is deliberate. A request arriving while Redis is down is disproportionately
likely to be a retry, and a refusal the client retries costs a moment where a duplicate
award means two providers each believe they have the job.

**SHIP-44 must pass the authenticated subject as the middleware's `scope`** when it
introduces the first protected endpoints. Without it, keys share one namespace and a
client that guesses another's key gets that client's response body.

## Checks

```
make check          vet + the boundary lint + tests
make lint-imports   the boundary lint on its own
make verify         the SHIP-1..15 acceptance criteria, end to end
```

`make test` passes on a clone with no stack running: the tests that need real Redis skip
themselves. `make up` first and they run.

## Architecture rules that apply here

From `Docs/06` §4.1 and `Docs/08`:

- **Adapters wrap external integrations only** — email, SMS, push, object storage,
  geocoding — and only where a second implementation exists *today*. A speculative seam is
  worse than none.
- **PostgreSQL is not abstracted.** The partial unique index enforcing one accepted bid per
  job (SHIP-91) and the row locking in the award transaction (SHIP-92) are load-bearing and
  PostgreSQL-specific. Neither survives a database-swappable abstraction. Test against a
  real PostgreSQL instance, not mocked repositories — a mock happily accepts the write the
  real constraint exists to reject.
- **Interfaces are declared by the consuming domain**, never by the implementing package.
  `delivery/ports.go` declares what delivery needs from storage; the storage package knows
  nothing about delivery.
- **Domain packages do not import each other**, adapters do not import domains, and
  domains do not import adapters. All three are enforced by `make lint-imports`, which
  also runs as a test — see `internal/boundaries`. The two halves of the adapter rule mean
  the arrow between a domain and its adapters does not exist at all: they meet in
  `cmd/api` and nowhere else.
- **Domain events are emitted by the domain, not the API layer** (`Docs/08` slice 5).

## Invariants this service enforces

These are defects when violated, not style choices:

- A customer's budget is **never** exposed to a provider — not as an amount, a band, or a
  "budget supplied" flag, through any endpoint (`Docs/01` §4.3).
- **Exactly one accepted bid per job**, enforced by a database constraint rather than
  application logic alone (`Docs/02` §3).
- **Job status is never a settable field.** Every transition passes one guarded function
  (`Docs/02` §2).
- Every state-changing endpoint accepts an **idempotency key**. Mobile clients retry after
  dropped connections; a retry must not duplicate a bid, milestone, or proof record.
- **Delivered requires photo proof or a recorded exception reason** — never neither
  (`Docs/01` §4.4).
- The driver's job-scoped token and the mobile auth token are **separate systems**. Neither
  can be exchanged for the other.
- **Audit entries are append-only.**
