# syntax=docker/dockerfile:1

# Container images for Shipper's deployable binaries (SHIP-187).
#
# # Why one file with several targets rather than one file per binary
#
# The five deployables are one Go module, one dependency graph and one compiler invocation.
# Splitting them across five Dockerfiles would mean five copies of the build stage, which is five
# places for the ldflags to drift out of step with the root Makefile's — and a version stamp that
# disagrees between the API and the worker is worse than no stamp, because it reads as two
# deployments when there is one. So the build stage is written once and every runtime target
# copies out of it:
#
#     docker build -f services/core/Dockerfile --target api services/core
#
# `make images` does this for all five and is the intended entry point; mk/images.mk carries the
# argument list.
#
# # The context is services/core, and that is a security property rather than a convenience
#
# deploy/.env lives at the repository root. Building from the root would put a file holding the
# local signing keys, the object-store credential and the database password into the build
# context — where a stray `COPY . .` bakes it into a layer that survives being deleted in a later
# one. Rooting the context at the Go module means the secret is not reachable by any instruction
# in this file, which is a stronger guarantee than remembering to exclude it. services/core/.
# dockerignore then removes what is merely noise.
#
# # What "starts from environment configuration alone" means here
#
# SHIP-187's acceptance criterion is that each image "starts from environment configuration alone,
# with no file baked in that a deployment would need to change". Three things make that true, and
# each is a decision rather than an accident:
#
#   1. internal/config reads every setting through os.LookupEnv and nothing else. There is no
#      config file to mount, no flag a deployment has to remember, and deploy/.env is read by
#      `make` on a developer's host — never by a binary. See internal/config.Load.
#   2. The migrations are embedded in cmd/migrate by //go:embed, so applying a schema needs the
#      image and a DATABASE_URL and no volume. The Kafka topic set is derived from
#      internal/events.Topics() in the same way — cmd/topics reads a declaration out of the code.
#   3. Nothing is baked in that names an environment. There is no default DATABASE_URL in any
#      layer; the development defaults live in Go and are refused outright when SHIPPER_ENV is
#      staging or production, which is the check that stops this image being deployed while still
#      pointing at a developer's laptop.
#
# # Static binaries, and why CGO is off
#
# Every dependency is pure Go — pgx, go-redis, kafka-go, golang-migrate, kin-openapi. Nothing
# links against a C library, so CGO_ENABLED=0 costs nothing and buys a binary that does not care
# which libc the runtime image ships. -trimpath keeps the build host's directory layout out of the
# binary, so two machines building the same commit produce the same bytes.

# ---------------------------------------------------------------------------------------------
# Build
# ---------------------------------------------------------------------------------------------

FROM golang:1.25.4-alpine AS build

WORKDIR /src

# The module files alone first. A change to any .go file below invalidates the COPY that follows
# and everything after it, but not this layer — so an ordinary source edit rebuilds without
# re-downloading the module graph.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# The same three stamps the root Makefile injects, passed in rather than computed: a container
# build has no git history to ask, and a stamp derived inside the image would say "unknown" on
# every build. mk/images.mk supplies them from the same VERSION, COMMIT and BUILT_AT variables
# that `make build` uses, so a host build and an image build of one commit agree.
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown

ENV CGO_ENABLED=0

# One invocation, five binaries. `go build -o /out/` with several packages names each output
# after its command directory, which is what the runtime stages below copy.
RUN go build \
        -trimpath \
        -ldflags "-s -w \
            -X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.version=${VERSION} \
            -X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.commit=${COMMIT} \
            -X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.builtAt=${BUILT_AT}" \
        -o /out/ \
        ./cmd/api ./cmd/worker ./cmd/notifier ./cmd/migrate ./cmd/topics

# ---------------------------------------------------------------------------------------------
# Runtime base
# ---------------------------------------------------------------------------------------------
#
# alpine rather than scratch or distroless, matching deploy/docker-compose.yml's postgres:17-alpine
# and redis:7-alpine. It is the smallest base that still has a shell, and a demonstration
# environment is exactly where somebody needs to open one — SHIP-188 has nothing else to debug
# with. scratch would save perhaps five megabytes and cost that.
#
# ca-certificates is not optional. Every outbound call this platform makes is TLS — S3 pre-signed
# uploads, Firebase Cloud Messaging, the geocoder, the email and SMS providers — and a static Go
# binary on a base with no trust store fails all of them with x509: certificate signed by unknown
# authority. That failure appears first as a proof photograph that will not upload.
#
# tzdata for the same class of reason: Australian delivery windows are local time, and a container
# with no zone database resolves Australia/Sydney to UTC silently rather than erroring.

FROM alpine:3.22 AS runtime

RUN apk add --no-cache ca-certificates tzdata

# A numeric, unprivileged user. Nothing here writes to disk — proof photographs go to object
# storage and the logs go to stdout — so there is no directory to own and root would buy nothing
# but the blast radius of a container escape.
RUN addgroup -g 65532 -S shipper && adduser -u 65532 -S -G shipper shipper
USER 65532:65532

# Read by internal/buildinfo through the ldflags above; repeated here so `docker inspect` answers
# the same question `GET /health` does.
ARG VERSION=dev
ARG COMMIT=unknown
LABEL org.opencontainers.image.source="https://github.com/DulsaraNethmin/Shipper" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${COMMIT}"

# ---------------------------------------------------------------------------------------------
# The three long-running services
# ---------------------------------------------------------------------------------------------

# The versioned public API (SHIP-13). HTTP_PORT chooses the port and defaults to 8080; EXPOSE is
# documentation for a reader and a default for `docker run -P`, and binds nothing by itself, so a
# deployment that moves the port needs no change here.
FROM runtime AS api
COPY --from=build /out/api /usr/local/bin/shipper-api
EXPOSE 8080
ENTRYPOINT ["shipper-api"]

# The scheduled task worker (SHIP-67a) — the outbox drain and the timed tasks.
FROM runtime AS worker
COPY --from=build /out/worker /usr/local/bin/shipper-worker
ENTRYPOINT ["shipper-worker"]

# The notification consumer (SHIP-137) — reads the domain topics and sends push, email and SMS.
FROM runtime AS notifier
COPY --from=build /out/notifier /usr/local/bin/shipper-notifier
ENTRYPOINT ["shipper-notifier"]

# ---------------------------------------------------------------------------------------------
# The two one-shot deployment steps
# ---------------------------------------------------------------------------------------------
#
# SHIP-187's title names the API, the worker and the notifier. These two are here because
# SHIP-188 depends on SHIP-187 alone and its acceptance criterion is that the demonstration
# environment answers /health "with migrations and the topic set applied by the same deployment" —
# which cannot be done with three images, and no other row in Docs/09 builds these. They are two
# stages of an existing file rather than new scope; Docs/11 §4 records the widening so it is not
# mistaken later for something the ticket asked for.
#
# Both are run to completion and exit, not supervised. Both are idempotent, so a deployment that
# runs them on every release and an operator who runs one twice by accident are the same case.

# Applies the PostgreSQL schema. The migrations are embedded, so `migrate up` needs DATABASE_URL
# and nothing mounted. No CMD default: `up`, `down` and `force` are not interchangeable and an
# image that picks one for you is an image that picks `down` for you eventually.
FROM runtime AS migrate
COPY --from=build /out/migrate /usr/local/bin/shipper-migrate
ENTRYPOINT ["shipper-migrate"]

# Applies the Kafka topic set (SHIP-135), derived from internal/events.Topics(). It refuses rather
# than repairs a topic whose partition count or replication factor disagrees with the catalogue —
# see cmd/topics for why that is the right failure.
FROM runtime AS topics
COPY --from=build /out/topics /usr/local/bin/shipper-topics
ENTRYPOINT ["shipper-topics"]
