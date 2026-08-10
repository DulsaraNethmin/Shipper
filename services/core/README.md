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
cmd/api/           entrypoint, wiring, graceful shutdown
internal/
  config/          environment configuration (SHIP-8)
  httpx/           HTTP middleware and helpers: logging, request ID, recovery (SHIP-9)
  buildinfo/       version and commit, injected at build time (SHIP-6)
migrations/        golang-migrate SQL files (SHIP-7)
```

The eight domain packages and the `platform/` adapter tree arrive in **SHIP-10**, with the
import lint rule that enforces their boundaries in **SHIP-11**.

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
- **Domain packages do not import each other.** Enforced by lint in SHIP-11.
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
