# deploy — local development stack

PostgreSQL, Redis, and Kafka for local development (SHIP-2, SHIP-3, SHIP-4). Production
runs on managed AWS services (`Docs/06` §2); nothing in this directory is a deployment
artefact.

```
make up            # start everything, waiting until each service is healthy
make ps            # status
make down          # stop, keeping data
make reset         # stop and destroy all data
```

## What runs where

| Service | In the compose network | On the host | Purpose |
|---|---|---|---|
| PostgreSQL 17 | `postgres:5432` | `localhost:5432` | System of record |
| Redis 7 | `redis:6379` | `localhost:6379` | Refresh tokens, device registry, idempotency keys, rate limits |
| Kafka 3.9 (KRaft) | `kafka:9092` | `localhost:29092` | Domain events |

The Go service runs **on the host** during development, not in a container, which is why
every service publishes a port. `make run` starts it against the stack.

## Configuration

Copy `.env.example` to `.env` and edit. That file is gitignored; `.env.example` is
committed and documents every variable with its default.

```
cp deploy/.env.example deploy/.env
```

`.env` is optional. The defaults built into `docker-compose.yml` and into
`services/core/internal/config` are identical, so a fresh clone works without it.

### If a port is already in use

`make up` fails with `Bind for 0.0.0.0:5432 failed: port is already allocated` when
another project already holds that port. Rather than stopping the other project, move
Shipper's host-side binding in `deploy/.env`:

```
POSTGRES_PORT=55432
DATABASE_URL=postgres://shipper:shipper@localhost:55432/shipper?sslmode=disable
HTTP_PORT=8081
```

Both variables need changing together — the first moves the published port, the second
tells the service and the migration tool where to find it. Inside the compose network
the services keep their standard ports, so nothing else is affected.

**One consequence worth knowing.** `deploy/.env` is read by `make`, not by the binaries.
Running `./bin/shipper-api` directly picks up the compiled-in defaults instead, which on
a machine with a *different* PostgreSQL on 5432 means connecting to the wrong database
and getting a confusing authentication error. Run through the make targets, or export
the variables yourself.

## Two things in the compose file that are easy to get wrong

**Kafka listeners.** A Kafka client bootstraps once, then reconnects to whatever address
the broker *advertises*. There are two listeners for that reason: `kafka:9092` for
clients inside the network and `localhost:29092` for clients on the host. Advertising
only the internal name is the classic failure — bootstrap succeeds and every subsequent
call then hangs trying to resolve `kafka` from the host.

`KAFKA_LISTENERS` uses an empty host (`PLAINTEXT://:9092`) rather than `0.0.0.0`. The
controller listener is not advertised, so Kafka derives its advertised address from
`listeners`, and a literal `0.0.0.0` there fails validation at startup with
*"advertised.listeners cannot use the nonroutable meta-address"*.

**Postgres collation.** The database is initialised with `--locale=C` so index ordering
is identical on every machine. Left to the host locale it is not, and the resulting
differences surface as tests that pass locally and fail in CI.

## Verifying the stack

`make verify` runs `scripts/verify-foundation.sh`, which demonstrates the acceptance
criterion of every ticket from SHIP-1 to SHIP-9 — including a real psql connection, a
`redis-cli` ping, a Kafka produce/consume round trip, and a migration applied and
reversed.

It needs two host clients that are not part of the stack:

```
brew install libpq redis
```

Homebrew leaves `libpq` unlinked because it collides with a full PostgreSQL install, so
`psql` will not be on `PATH`. The Makefile locates it via `brew --prefix libpq`; nothing
needs adding to your shell profile.
