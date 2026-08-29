# deploy — local development stack

PostgreSQL, Redis, Kafka, an S3-compatible object store and a mail catcher for local
development (SHIP-2, SHIP-3, SHIP-4, SHIP-15p, SHIP-200). Production
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
| MinIO | `minio:9000` | `localhost:9000` | Proof photographs and verification documents, private, reached only by pre-signed URL |
| MinIO console | — | `localhost:9001` | A browser view of the bucket. Nothing depends on it |
| Mailpit | `mailpit:1025` | `localhost:1025` | Every email the platform sends in development, caught rather than delivered |
| Mailpit mailbox | — | `localhost:8025` | A browser view of the catcher. Where a verification code is read |

Mailpit's two ports are **fixed rather than per-worktree**, unlike `STORAGE_BUCKET`. Every
worktree shares one stack, and a mailbox a person reads is not something two trees collide
over the way a listing or a count is. Its storage is in-memory, so it has no volume and a
restart empties it — which is the right state for a mailbox whose entire contents are
yesterday's test registrations.

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

## Three things in the compose file that are easy to get wrong

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

**The object store's address is signed, not merely routed to.** SigV4 covers the `host`
header, so a pre-signed URL minted against `minio:9000` inside the network and fetched
from the host is refused as `SignatureDoesNotMatch` — an error naming neither the address
nor the cause. `STORAGE_ENDPOINT` is therefore the *published* host port, and the same
applies to the region, which SigV4 puts in the credential scope: the container is started
with `MINIO_REGION` taken from `STORAGE_REGION` so the two cannot drift apart.

The bucket is created by `make up` rather than by a container in this file. `up -d --wait`
fails when any service exits — including a one-shot init container that exits `0` having
done its job — and the bucket is per-worktree while the stack is shared, so it has to be
made on every `make up` in every tree. `make storage-bucket` is the same step on its own,
and it is idempotent.

## Verifying the stack

`make verify` runs `scripts/verify-foundation.sh`, which demonstrates the acceptance
criterion of every ticket from SHIP-1 to SHIP-9 — including a real psql connection, a
`redis-cli` ping, a Kafka produce/consume round trip, a pre-signed upload and download
against the object store, and a migration applied and reversed.

It needs two host clients that are not part of the stack:

```
brew install libpq redis
```

Homebrew leaves `libpq` unlinked because it collides with a full PostgreSQL install, so
`psql` will not be on `PATH`. The Makefile locates it via `brew --prefix libpq`; nothing
needs adding to your shell profile.
