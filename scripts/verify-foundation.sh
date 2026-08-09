#!/usr/bin/env bash
#
# Demonstrates the "Done when" criterion of every ticket in SHIP-1..SHIP-9.
#
# Docs/09 makes the acceptance criterion the definition of done: if it cannot be shown,
# the ticket is not finished. This script is how it gets shown — on a developer machine
# now, and from CI once SHIP-20 lands.
#
#   make up && make verify
#
# Exits non-zero on the first failure.

set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
ROOT="$PWD"

# shellcheck disable=SC1091
[[ -f deploy/.env ]] && source deploy/.env

POSTGRES_USER="${POSTGRES_USER:-shipper}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-shipper}"
POSTGRES_DB="${POSTGRES_DB:-shipper}"
POSTGRES_PORT="${POSTGRES_PORT:-5432}"
REDIS_PORT="${REDIS_PORT:-6379}"
DATABASE_URL="${DATABASE_URL:-postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@localhost:${POSTGRES_PORT}/${POSTGRES_DB}?sslmode=disable}"
REDIS_URL="${REDIS_URL:-redis://localhost:${REDIS_PORT}/0}"

# A port of its own, so the run is not affected by whatever is already bound locally and
# so SHIP-5's "listens on a configured port" is actually being exercised.
VERIFY_PORT="${VERIFY_PORT:-18080}"

COMPOSE=(docker compose -f deploy/docker-compose.yml)
KAFKA_BIN=/opt/kafka/bin
PSQL="$(command -v psql || echo "$(brew --prefix libpq 2>/dev/null)/bin/psql")"

WORKDIR="$(mktemp -d)"
SERVER_PID=""

cleanup() {
  [[ -n "$SERVER_PID" ]] && kill "$SERVER_PID" 2>/dev/null || true
  rm -rf "$WORKDIR"
}
trap cleanup EXIT

pass=0
ticket() { printf '\n\033[1m=== %s ===\033[0m\n' "$*"; }
ok()     { printf '  \033[32m✓\033[0m %s\n' "$*"; pass=$((pass + 1)); }
fail()   { printf '  \033[31m✗\033[0m %s\n' "$*"; exit 1; }

# json <file> <expr> — read a value out of a JSON document with python3.
json() { python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))'"$2"')' "$1"; }

# ---------------------------------------------------------------------------------------
ticket "SHIP-1  monorepo directory structure"

for d in apps/mobile apps/admin apps/driver-portal services/core deploy; do
  [[ -d "$d" ]] || fail "$d is missing"
done
ok "apps/mobile, apps/admin, apps/driver-portal, services/core and deploy/ all exist"

# ---------------------------------------------------------------------------------------
ticket "SHIP-2  PostgreSQL reachable with psql from the host"

[[ -x "$PSQL" ]] || fail "psql not found — install it with: brew install libpq"
server_version="$("$PSQL" "$DATABASE_URL" -tAc 'show server_version;')"
ok "psql connected from the host to PostgreSQL $server_version on port $POSTGRES_PORT"

# ---------------------------------------------------------------------------------------
ticket "SHIP-3  Redis reachable with redis-cli from the host"

command -v redis-cli >/dev/null || fail "redis-cli not found — install it with: brew install redis"
[[ "$(redis-cli -u "$REDIS_URL" ping)" == "PONG" ]] || fail "redis-cli ping did not return PONG"
ok "redis-cli ping returned PONG from the host on port $REDIS_PORT"

# ---------------------------------------------------------------------------------------
ticket "SHIP-4  Kafka topic created, message produced and consumed"

topic="shipper.verify.$$"
"${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
  --create --if-not-exists --topic "$topic" --partitions 1 --replication-factor 1 >/dev/null 2>&1
ok "topic $topic created"

payload="verify-payload-$$"
echo "$payload" | "${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-console-producer.sh" \
  --bootstrap-server localhost:9092 --topic "$topic" >/dev/null 2>&1
ok "message produced"

consumed="$("${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-console-consumer.sh" \
  --bootstrap-server localhost:9092 --topic "$topic" --from-beginning \
  --max-messages 1 --timeout-ms 20000 2>/dev/null | tr -d '\r')"
[[ "$consumed" == "$payload" ]] || fail "consumed '$consumed', expected '$payload'"
ok "message consumed with the same payload"

"${COMPOSE[@]}" exec -T kafka "$KAFKA_BIN/kafka-topics.sh" --bootstrap-server localhost:9092 \
  --delete --topic "$topic" >/dev/null 2>&1
ok "topic deleted"

# ---------------------------------------------------------------------------------------
ticket "SHIP-7  migrations run up and down"

pushd "$ROOT/services/core" >/dev/null
export DATABASE_URL

go run ./cmd/migrate down all >/dev/null 2>&1 || true   # start from a known state

go run ./cmd/migrate up >/dev/null
version_after_up="$(go run ./cmd/migrate version)"
[[ "$version_after_up" == "version 1" ]] || fail "after up, expected 'version 1', got '$version_after_up'"
ok "migrate up applied migration 1"

"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from pg_proc where proname = 'set_updated_at';" | grep -q 1 \
  || fail "set_updated_at() was not created"
"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from pg_extension where extname = 'citext';" | grep -q 1 \
  || fail "citext extension was not created"
ok "the migration's objects exist in the database"

go run ./cmd/migrate down >/dev/null
version_after_down="$(go run ./cmd/migrate version)"
[[ "$version_after_down" == "no migrations applied" ]] \
  || fail "after down, expected 'no migrations applied', got '$version_after_down'"
ok "migrate down reversed it"

if "$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from pg_proc where proname = 'set_updated_at';" | grep -q 1; then
  fail "set_updated_at() survived the rollback"
fi
ok "the migration's objects are gone after rollback"

go run ./cmd/migrate up >/dev/null   # leave the database migrated
popd >/dev/null

# ---------------------------------------------------------------------------------------
ticket "SHIP-5  service builds and listens on a configured port"

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/shipper-api" \
  -ldflags "-X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.version=verify-1.0.0 \
            -X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.commit=$(git rev-parse HEAD) \
            -X github.com/DulsaraNethmin/Shipper/services/core/internal/buildinfo.builtAt=$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  ./cmd/api
popd >/dev/null
ok "service built"

SHIPPER_ENV=development \
HTTP_PORT="$VERIFY_PORT" \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
  "$WORKDIR/shipper-api" >"$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 50); do
  curl -fsS "http://localhost:$VERIFY_PORT/health" >/dev/null 2>&1 && break
  sleep 0.2
done
curl -fsS "http://localhost:$VERIFY_PORT/health" >/dev/null 2>&1 \
  || { cat "$WORKDIR/server.log"; fail "service did not come up on port $VERIFY_PORT"; }
ok "listening on HTTP_PORT=$VERIFY_PORT, which is not the default"

# ---------------------------------------------------------------------------------------
ticket "SHIP-6  GET /health returns 200 with version and commit"

status="$(curl -s -o "$WORKDIR/health.json" -w '%{http_code}' "http://localhost:$VERIFY_PORT/health")"
[[ "$status" == "200" ]] || fail "expected 200, got $status"
ok "GET /health returned 200"

health_version="$(json "$WORKDIR/health.json" '["version"]')"
health_commit="$(json "$WORKDIR/health.json" '["commit"]')"
[[ "$health_version" == "verify-1.0.0" ]] || fail "expected the injected version, got '$health_version'"
[[ "$health_commit" == "$(git rev-parse HEAD)" ]] || fail "expected the current commit, got '$health_commit'"
ok "version='$health_version' and commit='${health_commit:0:12}' come from the build, not a constant"

# ---------------------------------------------------------------------------------------
ticket "SHIP-8  configuration comes from the environment"

ok "the port, log level and log format above were all set by environment variable"

# A default that embeds a local credential must not be usable outside development.
# DATABASE_URL and REDIS_URL are cleared explicitly: make exports both from deploy/.env,
# which would otherwise satisfy the very guard being tested.
if env -u DATABASE_URL -u REDIS_URL \
   SHIPPER_ENV=production HTTP_PORT="$VERIFY_PORT" \
   "$WORKDIR/shipper-api" >"$WORKDIR/prod.log" 2>&1; then
  fail "the service started in production with a defaulted DATABASE_URL"
fi
grep -q "DATABASE_URL: must be set explicitly" "$WORKDIR/prod.log" \
  || { cat "$WORKDIR/prod.log"; fail "expected the credential-default guard to reject startup"; }
ok "refuses to start in production while a credential-bearing default is unset"

if SHIPPER_ENV=production LOG_FORMAT=text DATABASE_URL="postgres://u:p@db/x" REDIS_URL="redis://r:6379/0" \
   "$WORKDIR/shipper-api" >"$WORKDIR/prod2.log" 2>&1; then
  fail "the service started in production with text logs"
fi
grep -q "LOG_FORMAT: must be json" "$WORKDIR/prod2.log" || fail "expected the json-log guard"
ok "refuses text logs outside development, which the log aggregator cannot parse"

if HTTP_PORT=not-a-number "$WORKDIR/shipper-api" >"$WORKDIR/bad.log" 2>&1; then
  fail "the service started with an unparseable port"
fi
grep -q "HTTP_PORT" "$WORKDIR/bad.log" || fail "expected HTTP_PORT to be named in the error"
ok "invalid values are rejected at startup and name the variable at fault"

# ---------------------------------------------------------------------------------------
ticket "SHIP-9  every request logs method, path, status, duration and request ID"

supplied_id="verify-request-id-$$"
returned_id="$(curl -s -D - -o /dev/null -H "X-Request-Id: $supplied_id" \
  "http://localhost:$VERIFY_PORT/health" | tr -d '\r' | awk -F': ' 'tolower($1)=="x-request-id"{print $2}')"
[[ "$returned_id" == "$supplied_id" ]] || fail "expected the supplied request ID echoed, got '$returned_id'"
ok "a client-supplied X-Request-Id is honoured and echoed"

curl -s -o /dev/null "http://localhost:$VERIFY_PORT/nope" || true
sleep 0.3

line="$(grep -F "\"request_id\":\"$supplied_id\"" "$WORKDIR/server.log" | tail -1)"
[[ -n "$line" ]] || { cat "$WORKDIR/server.log"; fail "no log record carried the request ID"; }
echo "$line" >"$WORKDIR/line.json"

for field in method path status duration_ms request_id; do
  python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); sys.exit(0 if sys.argv[2] in d else 1)' \
    "$WORKDIR/line.json" "$field" || fail "the request log has no '$field' field"
done
ok "the request record is JSON carrying method, path, status, duration_ms and request_id"

grep -q '"level":"WARN"' "$WORKDIR/server.log" \
  || fail "expected the 404 to be logged at WARN"
ok "levels are applied by outcome: 2xx at INFO, 4xx at WARN"

# ---------------------------------------------------------------------------------------
ticket "SHIP-5  graceful shutdown"

kill -TERM "$SERVER_PID"
for _ in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || break
  sleep 0.2
done
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""
grep -q "stopped cleanly" "$WORKDIR/server.log" || fail "the service did not shut down cleanly on SIGTERM"
ok "drains and stops cleanly on SIGTERM"

printf '\n\033[32m%s checks passed — SHIP-1..SHIP-9 acceptance criteria demonstrated.\033[0m\n\n' "$pass"
