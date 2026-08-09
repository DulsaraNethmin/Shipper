#!/usr/bin/env bash
#
# Demonstrates the "Done when" criterion of every ticket in SHIP-1..SHIP-15.
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
REDIS_URL="$REDIS_URL" \
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
ticket "SHIP-10  package skeleton for the eight domains and the adapter tree"

for d in identity profiles fleet jobs bidding delivery notifications admin; do
  [[ -d "services/core/internal/$d" ]] || fail "domain package internal/$d is missing"
done
ok "all eight domains from Docs/06 §3 have a package"

for a in email sms push storage geocoding; do
  [[ -d "services/core/internal/platform/$a" ]] || fail "adapter internal/platform/$a is missing"
done
ok "the platform/ tree holds the five adapters from Docs/06 §4.1"

# ---------------------------------------------------------------------------------------
ticket "SHIP-11  the import lint fails on a crossed boundary"

pushd "$ROOT/services/core" >/dev/null
go build -o "$WORKDIR/lintboundaries" ./cmd/lintboundaries
popd >/dev/null

pushd "$ROOT/services/core" >/dev/null
"$WORKDIR/lintboundaries" >/dev/null || fail "the lint reports a violation in this repository"
popd >/dev/null
ok "this repository crosses no boundary"

# A throwaway module that breaks each rule, so the check is shown to fail and not merely
# to pass. A lint nobody has watched fail is a lint nobody knows works.
fixture="$WORKDIR/fixture"
mkdir -p "$fixture/internal/jobs" "$fixture/internal/bidding" \
         "$fixture/internal/platform/email" "$fixture/internal/identity"
printf 'module github.com/DulsaraNethmin/Shipper/services/core\n\ngo 1.25\n' >"$fixture/go.mod"
printf 'package bidding\n'  >"$fixture/internal/bidding/pkg.go"
printf 'package identity\n' >"$fixture/internal/identity/pkg.go"
printf 'package jobs\n\nimport _ "github.com/DulsaraNethmin/Shipper/services/core/internal/bidding"\n' \
  >"$fixture/internal/jobs/pkg.go"
printf 'package email\n\nimport _ "github.com/DulsaraNethmin/Shipper/services/core/internal/identity"\n' \
  >"$fixture/internal/platform/email/pkg.go"

pushd "$fixture" >/dev/null
if "$WORKDIR/lintboundaries" >"$WORKDIR/lint.log" 2>&1; then
  popd >/dev/null
  cat "$WORKDIR/lint.log"
  fail "the lint passed a module that crosses two boundaries"
fi
popd >/dev/null

grep -q "domain imports domain" "$WORKDIR/lint.log" \
  || { cat "$WORKDIR/lint.log"; fail "a domain importing another domain was not reported"; }
grep -q "adapter imports domain" "$WORKDIR/lint.log" \
  || { cat "$WORKDIR/lint.log"; fail "an adapter importing a domain was not reported"; }
ok "a domain importing a domain, and an adapter importing a domain, both fail the build"

# ---------------------------------------------------------------------------------------
ticket "SHIP-13  the public API is served under /v1"

status="$(curl -s -o "$WORKDIR/v1.json" -w '%{http_code}' "http://localhost:$VERIFY_PORT/v1/")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/v1.json"; fail "GET /v1/ returned $status"; }
[[ "$(json "$WORKDIR/v1.json" '["api_version"]')" == "v1" ]] \
  || fail "GET /v1/ did not report api_version=v1"
ok "GET /v1/ reports the version the group serves"

[[ "$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$VERIFY_PORT/v1/health")" == "404" ]] \
  || fail "/health is reachable under /v1"
[[ "$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$VERIFY_PORT/health")" == "200" ]] \
  || fail "/health is not reachable outside /v1"
ok "operational endpoints stay outside the version group"

# ---------------------------------------------------------------------------------------
ticket "SHIP-12  every error has the same shape and a machine-readable code"

error_id="verify-error-id-$$"
status="$(curl -s -o "$WORKDIR/notfound.json" -w '%{http_code}' \
  -H "X-Request-Id: $error_id" "http://localhost:$VERIFY_PORT/v1/no-such-thing")"
[[ "$status" == "404" ]] || fail "expected 404, got $status"
[[ "$(json "$WORKDIR/notfound.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/notfound.json"; fail "the 404 carried no machine-readable code"; }
ok "ServeMux's own 404 arrives as JSON with code=not_found"

status="$(curl -s -X POST -o "$WORKDIR/method.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/health")"
[[ "$status" == "405" ]] || fail "expected 405, got $status"
[[ "$(json "$WORKDIR/method.json" '["error"]["code"]')" == "method_not_allowed" ]] \
  || { cat "$WORKDIR/method.json"; fail "the 405 carried no machine-readable code"; }
ok "a 405 arrives in the same shape with code=method_not_allowed"

# ---------------------------------------------------------------------------------------
ticket "SHIP-14  one request ID, in the header, the body and the log"

[[ "$(json "$WORKDIR/notfound.json" '["error"]["request_id"]')" == "$error_id" ]] \
  || fail "the error body did not carry the request ID"
ok "the ID reached the response body through the request context"

sleep 0.3
grep -qF "\"request_id\":\"$error_id\"" "$WORKDIR/server.log" \
  || fail "no log record carried the request ID"
ok "the same ID identifies the request in the log"

# ---------------------------------------------------------------------------------------
ticket "SHIP-15  a repeated idempotency key replays the original response"

status="$(curl -s -X POST -o "$WORKDIR/nokey.json" -w '%{http_code}' \
  -H 'Content-Type: application/json' -d '{}' "http://localhost:$VERIFY_PORT/v1/jobs")"
[[ "$status" == "400" ]] || fail "a state-changing request without a key returned $status"
[[ "$(json "$WORKDIR/nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || { cat "$WORKDIR/nokey.json"; fail "expected code=idempotency_key_required"; }
ok "a state-changing request without an Idempotency-Key is refused"

idem_key="verify-idem-$$"
curl -s -X POST -D "$WORKDIR/first.headers" -o "$WORKDIR/first.json" \
  -H "Idempotency-Key: $idem_key" -H 'Content-Type: application/json' \
  -d '{"price_aud":450}' "http://localhost:$VERIFY_PORT/v1/jobs" >/dev/null

replayed="$(tr -d '\r' <"$WORKDIR/first.headers" | awk -F': ' 'tolower($1)=="idempotency-replayed"{print $2}')"
[[ -z "$replayed" ]] || fail "the first request was marked as a replay"
ok "the first request with a new key executes"

curl -s -X POST -D "$WORKDIR/second.headers" -o "$WORKDIR/second.json" \
  -H "Idempotency-Key: $idem_key" -H 'Content-Type: application/json' \
  -d '{"price_aud":450}' "http://localhost:$VERIFY_PORT/v1/jobs" >/dev/null

replayed="$(tr -d '\r' <"$WORKDIR/second.headers" | awk -F': ' 'tolower($1)=="idempotency-replayed"{print $2}')"
[[ "$replayed" == "true" ]] || { cat "$WORKDIR/second.headers"; fail "the retry was not replayed"; }
diff -q "$WORKDIR/first.json" "$WORKDIR/second.json" >/dev/null \
  || fail "the replayed body differs from the original"
ok "the retry returns the stored response byte for byte, marked Idempotency-Replayed"

status="$(curl -s -X POST -o "$WORKDIR/reuse.json" -w '%{http_code}' \
  -H "Idempotency-Key: $idem_key" -H 'Content-Type: application/json' \
  -d '{"price_aud":999}' "http://localhost:$VERIFY_PORT/v1/jobs")"
[[ "$status" == "409" ]] || fail "reusing a key for a different request returned $status"
[[ "$(json "$WORKDIR/reuse.json" '["error"]["code"]')" == "idempotency_key_reused" ]] \
  || { cat "$WORKDIR/reuse.json"; fail "expected code=idempotency_key_reused"; }
ok "the same key with a different request is refused rather than answered"

stored="$(redis-cli -u "$REDIS_URL" --scan --pattern "idem:v1:*:$idem_key" | head -1)"
[[ -n "$stored" ]] || fail "no idempotency entry was written to Redis"
ttl="$(redis-cli -u "$REDIS_URL" ttl "$stored")"
[[ "$ttl" -gt 0 ]] || fail "the stored entry has no expiry (ttl=$ttl)"
ok "the entry is in Redis under $stored, expiring in ${ttl}s"

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

printf '\n\033[32m%s checks passed — SHIP-1..SHIP-15 acceptance criteria demonstrated.\033[0m\n\n' "$pass"
