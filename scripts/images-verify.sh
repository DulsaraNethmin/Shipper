#!/usr/bin/env bash
#
# Demonstrate SHIP-187 — every deployable binary builds to an image that starts from
# environment configuration alone, with no file baked in that a deployment would need to change.
#
# # Why this is not a section of scripts/verify/
#
# `make verify` demonstrates the *foundation* tickets, and CLAUDE.md's command table says so in
# those words. Three things make a section the wrong home for this one:
#
#   1. It would put five container builds inside a harness that already takes minutes, on every
#      run, for every developer, whether or not they touched a Dockerfile.
#   2. It would move Docs/11 §3's check count, which is a file the harness rewrites — so a wave
#      merging anything at all would carry a count that depends on whether Docker happened to be
#      running on the machine that measured it.
#   3. `make verify` is not concurrency-safe (CLAUDE.md's worktree table) and this is: it names
#      its containers and its throwaway database after the process, and it never deletes a Kafka
#      topic. Folding a safe check into an unsafe harness makes it unsafe.
#
# So it is its own target, `make images-verify`, and it is run when the images change.
#
# # What it does not prove
#
# It proves the images start and are configured only by the environment. It does not prove they
# are *correct* — `make check` does that, against the same source, before any image is built.
# And it runs against the local compose stack, so it says nothing about SHIP-188's hosted one.

set -euo pipefail

cd "$(dirname "$0")/.."

# The same defaults the acceptance harness uses, and for the same reason: deploy/.env overrides
# whatever a worktree has set, so a tree with its own bucket is checked against its own bucket.
[[ -f deploy/.env ]] && source deploy/.env

POSTGRES_USER="${POSTGRES_USER:-shipper}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-shipper}"
POSTGRES_DB="${POSTGRES_DB:-shipper}"
STORAGE_BUCKET="${STORAGE_BUCKET:-shipper-dev}"
STORAGE_REGION="${STORAGE_REGION:-ap-southeast-2}"
STORAGE_ACCESS_KEY_ID="${STORAGE_ACCESS_KEY_ID:-shipper}"
STORAGE_SECRET_ACCESS_KEY="${STORAGE_SECRET_ACCESS_KEY:-shipperminio}"

IMAGE_PREFIX="${IMAGE_PREFIX:-shipper}"
IMAGE_TAG="${IMAGE_TAG:-dev}"
IMAGES=(api worker notifier migrate topics)

# The compose stack's network. COMPOSE_PROJECT_NAME is pinned to `shipper` in the Makefile so
# every worktree shares one stack, which makes this name stable rather than derived.
NETWORK="${COMPOSE_NETWORK:-shipper_default}"

# Addresses **inside** that network, which are not the addresses on the host. This is the whole
# reason a container needs its own environment rather than deploy/.env's: the broker advertises
# PLAINTEXT://kafka:9092 internally and PLAINTEXT_HOST://localhost:29092 externally, and a
# container handed the host's value connects to itself. Reading deploy/.env and passing it
# through unchanged is the mistake this block exists to make impossible.
PG_IN="postgres:5432"
REDIS_IN="redis:6379"
KAFKA_IN="kafka:9092"
MINIO_IN="http://minio:9000"

# Named after the process so two worktrees running this at once do not collide. Kafka has no
# such split (CLAUDE.md), which is why nothing below deletes or asserts on a topic.
RUN_ID="images-verify-$$"
SCRATCH_DB="shipper_${RUN_ID//-/_}"
API_CONTAINER="$RUN_ID-api"
WORKER_CONTAINER="$RUN_ID-worker"
NOTIFIER_CONTAINER="$RUN_ID-notifier"

pass=0
ok()   { printf '  \033[32m✓\033[0m %s\n' "$*"; pass=$((pass + 1)); }
fail() { printf '  \033[31m✗\033[0m %s\n' "$*"; exit 1; }
step() { printf '\n\033[1m=== %s ===\033[0m\n' "$*"; }

# The same sentinel the acceptance harness uses, and for the reason written at length there: on
# bash 3.2.57 an unbound-variable abort leaves no pending status, so a cleanup trap that ends in a
# successful command becomes the script's own exit code and a run that did nothing reports 0.
FINISHED=0
cleanup() {
  docker rm -f "$API_CONTAINER" "$WORKER_CONTAINER" "$NOTIFIER_CONTAINER" >/dev/null 2>&1 || true
  docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" shipper-postgres \
    psql -U "$POSTGRES_USER" -d postgres -c "drop database if exists $SCRATCH_DB" >/dev/null 2>&1 || true
  [[ "$FINISHED" == 1 ]] || exit 1
}
trap cleanup EXIT

# --- preconditions ---------------------------------------------------------------------------

step "preconditions"

command -v docker >/dev/null || fail "docker is not on PATH"
docker info >/dev/null 2>&1 || fail "the docker daemon is not running"
ok "docker is available"

docker network inspect "$NETWORK" >/dev/null 2>&1 \
  || fail "network $NETWORK does not exist — run 'make up' first"
ok "the compose network $NETWORK exists"

for svc in shipper-postgres shipper-redis shipper-kafka shipper-minio; do
  [[ "$(docker inspect -f '{{.State.Running}}' "$svc" 2>/dev/null)" == "true" ]] \
    || fail "$svc is not running — run 'make up' first"
done
ok "postgres, redis, kafka and minio are running"

# --- SHIP-187: the images build ----------------------------------------------------------------

step "SHIP-187  every deployable binary builds to an image"

build_log="$(mktemp)"
make images >"$build_log" 2>&1 || fail "make images failed: $(tail -30 "$build_log")"
rm -f "$build_log"
ok "make images built all ${#IMAGES[@]} targets"

for t in "${IMAGES[@]}"; do
  docker image inspect "$IMAGE_PREFIX-$t:$IMAGE_TAG" >/dev/null 2>&1 \
    || fail "$IMAGE_PREFIX-$t:$IMAGE_TAG was not produced"
done
ok "all five tags exist: ${IMAGES[*]}"

# --- nothing a deployment would need to change is inside the image ------------------------------

step "SHIP-187  no file baked in that a deployment would need to change"

# The context is services/core, so deploy/.env is unreachable by construction. This checks the
# construction rather than trusting it: a later edit that moves the context to the repository root
# would still build, still start, and quietly ship the signing keys.
for t in "${IMAGES[@]}"; do
  found="$(docker run --rm --entrypoint sh "$IMAGE_PREFIX-$t:$IMAGE_TAG" -c \
    'find / -xdev \( -name ".env" -o -name ".env.*" -o -name "*.env" \) 2>/dev/null' || true)"
  [[ -z "$found" ]] || fail "$t carries an environment file: $found"
done
ok "no .env file in any image"

# The second door, and the one that matters more: an environment file is easy to name and a
# source tree copied in by a COPY that was too wide is not. A runtime stage should hold the base
# image and one binary — nothing that came out of the build context — so anything from the module
# tree appearing here means a runtime stage is inheriting from `build` rather than from `runtime`,
# which would ship the compiler, the module cache and every source file with the service.
for t in "${IMAGES[@]}"; do
  leaked="$(docker run --rm --entrypoint sh "$IMAGE_PREFIX-$t:$IMAGE_TAG" -c \
    'find / -xdev \( -name "*.go" -o -name "go.mod" -o -name "go.sum" -o -name "*.sql" -o -name "Dockerfile" \) 2>/dev/null | head -3' || true)"
  [[ -z "$leaked" ]] || fail "$t carries build-context files: $leaked"
done
ok "no source file, module file or migration from the build context in any image"

# The development signing key is public and in deploy/.env.example, so its absence from a *file* is
# checkable and its presence would be a published key shipped in a deployed image.
#
# **/usr/local/bin is excluded, and that exclusion is a finding rather than a convenience.** The
# first version of this check searched the whole filesystem and failed on every image, pointing at
# the binary itself. It was right about what it saw: internal/config declares the key as a Go
# constant — `developmentSigningKey`, config.go — precisely so validate can *refuse* it when
# SHIPPER_ENV is staging or production. Compiling in the string that names a forbidden key is how
# the refusal is possible at all, so it belongs there, and a check that flags it is measuring the
# wrong thing. What must not appear is the key in a file the image did not compile.
#
# **`grep -r /` is the wrong instrument and hung for ten minutes proving it.** grep descends into
# /proc and /sys, where reading a file is a kernel call that can block indefinitely — the container
# sat in `grep -rl` with no output and no timeout until it was killed by hand. `find / -xdev` stops
# at every mount boundary, and /proc, /sys and /dev are each their own mount, so it walks the image
# layers and nothing else. That is also exactly the set this check is asking about: what is baked
# into the image, not what the kernel is exposing to it at runtime.
for t in "${IMAGES[@]}"; do
  hit="$(docker run --rm --entrypoint sh "$IMAGE_PREFIX-$t:$IMAGE_TAG" -c \
    'find / -xdev -type f -not -path "/usr/local/bin/*" -exec grep -l "shipper-local-development-signing-key" {} + 2>/dev/null | head -1' || true)"
  [[ -z "$hit" ]] || fail "$t carries the development signing key in a file at $hit"
done
ok "no development signing key in any file outside the compiled binary"

# Nothing here writes to disk — proof photographs go to object storage, logs go to stdout — so
# root would buy nothing but the blast radius of a container escape.
for t in "${IMAGES[@]}"; do
  uid="$(docker run --rm --entrypoint sh "$IMAGE_PREFIX-$t:$IMAGE_TAG" -c 'id -u')"
  [[ "$uid" == "65532" ]] || fail "$t runs as uid $uid, want 65532"
done
ok "every image runs as the unprivileged uid 65532"

# --- the schema and the topic set apply from the image alone ------------------------------------

step "SHIP-187  migrate and topics need the image and an environment, and nothing mounted"

docker exec -e PGPASSWORD="$POSTGRES_PASSWORD" shipper-postgres \
  psql -U "$POSTGRES_USER" -d postgres -c "create database $SCRATCH_DB" >/dev/null \
  || fail "could not create the throwaway database $SCRATCH_DB"

# A database this run made and this run drops, so the check proves the migrations apply from
# nothing rather than that they already applied to the development database at some point.
migrate_out="$(docker run --rm --network "$NETWORK" \
  -e "DATABASE_URL=postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@$PG_IN/$SCRATCH_DB?sslmode=disable" \
  "$IMAGE_PREFIX-migrate:$IMAGE_TAG" up 2>&1)" \
  || fail "migrate up failed from the image: $migrate_out"

applied="$(docker run --rm --network "$NETWORK" \
  -e "DATABASE_URL=postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@$PG_IN/$SCRATCH_DB?sslmode=disable" \
  "$IMAGE_PREFIX-migrate:$IMAGE_TAG" version 2>&1)" \
  || fail "migrate version failed from the image"
[[ "$applied" == *"0"* ]] || fail "migrate version reported nothing: $applied"
ok "migrate applied the embedded schema to a fresh database with no volume ($applied)"

# Idempotent by design — it answers TOPIC_ALREADY_EXISTS as success and refuses rather than
# repairs a topic that disagrees with the catalogue. Nothing here deletes one, which is what makes
# this safe on a broker every worktree on the machine shares.
docker run --rm --network "$NETWORK" \
  -e "KAFKA_BROKERS=$KAFKA_IN" \
  "$IMAGE_PREFIX-topics:$IMAGE_TAG" >/dev/null \
  || fail "topics failed from the image"
ok "topics applied the catalogue's topic set from the image alone"

# --- the API starts from the environment and reports the build it was given ----------------------

step "SHIP-187  the API starts from environment configuration alone"

docker run -d --name "$API_CONTAINER" --network "$NETWORK" -P \
  -e SHIPPER_ENV=development \
  -e HTTP_PORT=8080 \
  -e "DATABASE_URL=postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@$PG_IN/$POSTGRES_DB?sslmode=disable" \
  -e "REDIS_URL=redis://$REDIS_IN/0" \
  -e "KAFKA_BROKERS=$KAFKA_IN" \
  -e "STORAGE_ENDPOINT=$MINIO_IN" \
  -e "STORAGE_BUCKET=$STORAGE_BUCKET" \
  -e "STORAGE_REGION=$STORAGE_REGION" \
  -e "STORAGE_ACCESS_KEY_ID=$STORAGE_ACCESS_KEY_ID" \
  -e "STORAGE_SECRET_ACCESS_KEY=$STORAGE_SECRET_ACCESS_KEY" \
  -e STORAGE_USE_PATH_STYLE=true \
  "$IMAGE_PREFIX-api:$IMAGE_TAG" >/dev/null \
  || fail "the api container would not start"

# The published port is chosen by the daemon rather than by this script, so a run does not collide
# with whatever a developer already has bound on 8080.
api_addr=""
for _ in $(seq 1 50); do
  api_addr="$(docker port "$API_CONTAINER" 8080/tcp 2>/dev/null | head -1 || true)"
  [[ -n "$api_addr" ]] && break
  sleep 0.2
done
[[ -n "$api_addr" ]] || fail "the api container published no port"
api_url="http://127.0.0.1:${api_addr##*:}"

health=""
for _ in $(seq 1 100); do
  health="$(curl -fsS "$api_url/health" 2>/dev/null || true)"
  [[ -n "$health" ]] && break
  sleep 0.2
done
[[ -n "$health" ]] || fail "GET /health never answered: $(docker logs "$API_CONTAINER" 2>&1 | tail -20)"
ok "GET /health answered 200 with only environment variables supplied"

# The stamp is what proves mk/images.mk's build arguments reach the linker. An image reporting
# "dev" while the host build of the same commit reports a tag is a deployment nobody can identify.
want_version="$(git describe --tags --always --dirty 2>/dev/null || echo dev)"
want_commit="$(git rev-parse HEAD 2>/dev/null || echo unknown)"
got_version="$(printf '%s' "$health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["version"])')"
got_commit="$(printf '%s' "$health" | python3 -c 'import json,sys; print(json.load(sys.stdin)["commit"])')"
[[ "$got_version" == "$want_version" ]] \
  || fail "/health reports version $got_version, want $want_version — the build args are not reaching the linker"
[[ "$got_commit" == "$want_commit" ]] \
  || fail "/health reports commit $got_commit, want $want_commit"
ok "/health reports the version and commit the build was given ($got_version)"

# --- the worker and the notifier start the same way ----------------------------------------------

step "SHIP-187  the worker and the notifier start from environment configuration alone"

start_and_check() {
  local name="$1" image="$2"
  docker run -d --name "$name" --network "$NETWORK" \
    -e SHIPPER_ENV=development \
    -e "DATABASE_URL=postgres://$POSTGRES_USER:$POSTGRES_PASSWORD@$PG_IN/$POSTGRES_DB?sslmode=disable" \
    -e "REDIS_URL=redis://$REDIS_IN/0" \
    -e "KAFKA_BROKERS=$KAFKA_IN" \
    -e "STORAGE_ENDPOINT=$MINIO_IN" \
    -e "STORAGE_BUCKET=$STORAGE_BUCKET" \
    -e "STORAGE_REGION=$STORAGE_REGION" \
    -e "STORAGE_ACCESS_KEY_ID=$STORAGE_ACCESS_KEY_ID" \
    -e "STORAGE_SECRET_ACCESS_KEY=$STORAGE_SECRET_ACCESS_KEY" \
    -e STORAGE_USE_PATH_STYLE=true \
    "$image" >/dev/null || fail "$name would not start"

  # Long enough to fail on a configuration error, which is what this is checking. A process that
  # survives three seconds has read its environment, built its dependencies and entered its loop.
  sleep 3
  local state
  state="$(docker inspect -f '{{.State.Running}}' "$name")"
  [[ "$state" == "true" ]] \
    || fail "$name exited: $(docker logs "$name" 2>&1 | tail -20)"
}

start_and_check "$WORKER_CONTAINER" "$IMAGE_PREFIX-worker:$IMAGE_TAG"
ok "the worker started and stayed up on environment configuration alone"

start_and_check "$NOTIFIER_CONTAINER" "$IMAGE_PREFIX-notifier:$IMAGE_TAG"
ok "the notifier started and stayed up on environment configuration alone"

# --- the image carries no environment of its own ---------------------------------------------------

step "SHIP-187  the image carries no environment of its own"

# The strongest form of the acceptance criterion, and the one a reader should trust most. If any
# deployment-shaped default were baked into a layer, this would start. It must not: with
# SHIPPER_ENV=production and nothing else supplied, internal/config refuses every credential-bearing
# variable that is still sitting on its development default.
#
# So the check is that the image fails, and fails for that reason — a container that would not start
# for some other reason would pass a bare "did it exit non-zero".
refusal="$(docker run --rm -e SHIPPER_ENV=production "$IMAGE_PREFIX-api:$IMAGE_TAG" 2>&1 || true)"
[[ "$refusal" == *"must be set explicitly"* ]] \
  || fail "the api image did not refuse a production start with development defaults: $refusal"

#
# **Each variable is matched against its own rule and not merely by name, and a mutation is what
# established that the looser form was worthless.** Baking `ENV DATABASE_URL=…?sslmode=disable`
# into the api stage — exactly the defect this check exists to catch — left the variable still
# named in the refusal, because a *different* rule then fired about it: "sslmode=disable is not
# permitted". A bare `*"$v"*` test passed on the mutated image and reported sixteen checks green.
# Requiring the phrase that belongs to the credential-default rule distinguishes "nothing was baked
# in" from "something was baked in and turned out to be invalid for an unrelated reason".
for v in DATABASE_URL REDIS_URL IDENTITY_ACCESS_TOKEN_KEYS DELIVERY_DRIVER_TOKEN_KEYS \
         STORAGE_ACCESS_KEY_ID STORAGE_SECRET_ACCESS_KEY; do
  [[ "$refusal" == *"$v: must be set explicitly"* ]] \
    || fail "the refusal did not report $v as an unset credential-bearing default, so something is baked into a layer: $refusal"
done
ok "a production start with no environment is refused, naming all six credential-bearing variables"

# --- summary --------------------------------------------------------------------------------------

printf '\n\033[32m%d checks passed\033[0m — SHIP-187: each of the five deployable binaries builds\n' "$pass"
printf 'to an image that starts from environment configuration alone.\n'

FINISHED=1
