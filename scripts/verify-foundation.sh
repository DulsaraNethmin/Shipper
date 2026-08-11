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
# The highest number on disk, rather than a literal. Migrations are allocated in per-domain
# blocks (migrations/blocks.go), so the newest one is not the count of them and hard-coding
# either would make this line a chore to update on every schema change.
highest_migration="$(ls migrations/*.up.sql | sed -E 's#.*/0*([0-9]+)_.*#\1#' | sort -n | tail -1)"
[[ "$version_after_up" == "version $highest_migration" ]] \
  || fail "after up, expected 'version $highest_migration', got '$version_after_up'"
ok "migrate up applied every migration, ending at $highest_migration"

"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from pg_proc where proname = 'set_updated_at';" | grep -q 1 \
  || fail "set_updated_at() was not created"
"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from pg_extension where extname = 'citext';" | grep -q 1 \
  || fail "citext extension was not created"
ok "the migration's objects exist in the database"

go run ./cmd/migrate down all >/dev/null
version_after_down="$(go run ./cmd/migrate version)"
[[ "$version_after_down" == "no migrations applied" ]] \
  || fail "after down, expected 'no migrations applied', got '$version_after_down'"
ok "migrate down reversed every one of them"

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
ticket "SHIP-44  one caller's idempotency key cannot read another caller's response"

# SHIP-44 adds no endpoint, so most of it is demonstrated by its tests. This is the half that
# can be shown against a running service, and it is the half that matters: until now every
# idempotency key landed in idem:v1:anonymous:<key>, so a client that guessed another client's
# key was handed that client's stored response body (Docs/11 §8).
#
# Two tokens are minted here rather than obtained, because no sign-in endpoint exists yet
# (SHIP-41). They are signed with the development key from deploy/.env.example, which is public
# and in this repository on purpose — the service refuses it outside development.

# The header name is spelled by RFC 9110 and not by us.
auth_header="Authorization"  # spelling:ok — HTTP header name, RFC 9110

b64url() { openssl base64 -A | tr '+/' '-_' | tr -d '='; }

mint_token() {
  local sub="$1" now exp header payload signing_input signature
  now="$(date +%s)"
  exp=$((now + 900))
  header='{"alg":"HS256","typ":"JWT","kid":"dev"}'
  payload="{\"sub\":\"$sub\",\"role\":\"customer\",\"sid\":\"$(uuidgen | tr 'A-Z' 'a-z')\",\"iat\":$now,\"exp\":$exp,\"jti\":\"$(uuidgen | tr 'A-Z' 'a-z')\",\"iss\":\"shipper\",\"aud\":\"shipper-mobile\"}"
  signing_input="$(printf '%s' "$header" | b64url).$(printf '%s' "$payload" | b64url)"
  signature="$(printf '%s' "$signing_input" \
    | openssl dgst -sha256 -hmac "shipper-local-development-signing-key-not-a-secret" -binary \
    | b64url)"
  printf '%s.%s' "$signing_input" "$signature"
}

alice_id="$(uuidgen | tr 'A-Z' 'a-z')"
bob_id="$(uuidgen | tr 'A-Z' 'a-z')"
alice_token="$(mint_token "$alice_id")"
bob_token="$(mint_token "$bob_id")"

shared_key="verify-scope-$$"

# The path does not exist, and that is deliberate: within /v1 the idempotency claim is taken
# before routing, so even a 404 is stored against the key. Method, path and body are identical
# for both callers, so the scope is the only thing that can separate them.
for token in "$alice_token" "$bob_token"; do
  curl -s -X POST -o /dev/null \
    -H "Idempotency-Key: $shared_key" -H 'Content-Type: application/json' \
    -H "$auth_header: Bearer $token" \
    -d '{"amount":1}' "http://localhost:$VERIFY_PORT/v1/not-a-real-endpoint" >/dev/null
done

# bash 3.2 is what macOS ships, so this stays clear of mapfile and of associative arrays.
scoped="$(redis-cli -u "$REDIS_URL" --scan --pattern "idem:v1:*:$shared_key" | sort)"
scoped_count="$(printf '%s\n' "$scoped" | grep -c . || true)"

[[ "$scoped_count" -eq 2 ]] || {
  printf '  %s\n' "$scoped"
  fail "two callers sharing one key produced $scoped_count Redis entries, want 2 — the scope is shared"
}
grep -q "$alice_id" <<<"$scoped" || fail "no entry is namespaced by the calling user"
grep -q "$bob_id"   <<<"$scoped" || fail "the second caller's entry is not namespaced by them"
ok "two callers using the same key wrote two separate entries, namespaced by user"

if grep -q "idem:v1:anonymous:$shared_key" <<<"$scoped"; then
  fail "an authenticated request still landed in the anonymous namespace"
fi
ok "neither landed in idem:v1:anonymous, which is what a nil scope produced"

# The property that lets SHIP-42 work at all: a client whose access token has just expired
# still has to be able to reach the endpoint that replaces it. A public route must therefore
# tolerate a credential it cannot verify rather than refusing the request.
status="$(curl -s -o /dev/null -w '%{http_code}' \
  -H "$auth_header: Bearer this-is-not-a-token" "http://localhost:$VERIFY_PORT/v1/")"
[[ "$status" == "200" ]] || fail "a public endpoint returned $status for an unusable token; refresh would be unreachable"
ok "a public endpoint still answers with an unusable token attached"

status="$(curl -s -o /dev/null -w '%{http_code}' \
  -H "$auth_header: Bearer $alice_token" "http://localhost:$VERIFY_PORT/v1/")"
[[ "$status" == "200" ]] || fail "a public endpoint returned $status for a valid token"
ok "and answers with a valid one, which is what puts the subject on the context"

# ---------------------------------------------------------------------------------------
ticket "SHIP-28  the users table, with its constraints enforced by the database"

"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from information_schema.tables where table_name = 'users';" | grep -q 1 \
  || fail "the users table does not exist"
ok "users exists"

"$PSQL" "$DATABASE_URL" -q -c \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'verify-$$@example.com', '+6140000$$', 'x', 'customer');" >/dev/null \
  || fail "a valid account could not be created"

if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'VERIFY-$$@EXAMPLE.COM', '+6140001$$', 'x', 'customer');" >/dev/null 2>&1; then
  fail "the same address in different case was accepted as a second account"
fi
ok "email uniqueness is case-insensitive, so one address is one account"

if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'role-$$@example.com', '+6140002$$', 'x', 'admin');" >/dev/null 2>&1; then
  fail "'admin' was accepted as a role; admin sign-in is a separate system (SHIP-147)"
fi
ok "role is constrained to customer and provider"

# ---------------------------------------------------------------------------------------
ticket "SHIP-149  the audit log is append-only in the database, not by convention"

# -q matters here and only here. Without it psql appends the command tag to the result, so
# this captures "<uuid>\nINSERT 0 1" rather than a uuid — and the two checks below then fail
# with `invalid input syntax for type uuid` instead of with the append-only trigger, which
# their `if` cannot tell apart from success. Both reported a pass while exercising nothing.
# The other captures in this file are SELECTs, which emit no tag under -tA.
audit_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into audit_log (id, actor_type, action, target_type, target_id)
   values (gen_random_uuid(), 'system', 'job.expired', 'job', gen_random_uuid())
   returning id;")"
[[ -n "$audit_id" ]] || fail "an audit entry could not be appended"
ok "an entry can be appended"

if "$PSQL" "$DATABASE_URL" -q -c \
  "update audit_log set reason = 'rewritten' where id = '$audit_id';" >/dev/null 2>&1; then
  fail "an audit entry was rewritten"
fi
ok "UPDATE is refused"

if "$PSQL" "$DATABASE_URL" -q -c \
  "delete from audit_log where id = '$audit_id';" >/dev/null 2>&1; then
  fail "an audit entry was deleted"
fi
ok "DELETE is refused, so the trail survives a psql prompt"

# ---------------------------------------------------------------------------------------
ticket "SHIP-167  the app is told the minimum build it may be"

status="$(curl -s -o "$WORKDIR/min-version.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/app/minimum-version")"
[[ "$status" == "200" ]] || fail "GET /v1/app/minimum-version returned $status"

ios_floor="$(json "$WORKDIR/min-version.json" '["ios"]["minimum_build"]')"
android_floor="$(json "$WORKDIR/min-version.json" '["android"]["minimum_build"]')"
[[ "$ios_floor" =~ ^[0-9]+$ ]]     || fail "ios.minimum_build is not a number: $ios_floor"
[[ "$android_floor" =~ ^[0-9]+$ ]] || fail "android.minimum_build is not a number: $android_floor"
ok "both platforms report a build floor the launch gate can compare against (iOS $ios_floor, Android $android_floor)"

# The floor follows configuration, so raising it is an operational act rather than a release
# (Docs/07 §6). Restarting with a different value is the whole mechanism.
kill -TERM "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true

SHIPPER_ENV=development \
HTTP_PORT="$VERIFY_PORT" \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
MIN_SUPPORTED_IOS_BUILD=4242 \
  "$WORKDIR/shipper-api" >>"$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 50); do
  curl -fsS "http://localhost:$VERIFY_PORT/health" >/dev/null 2>&1 && break
  sleep 0.2
done

curl -fsS -o "$WORKDIR/min-version-raised.json" \
  "http://localhost:$VERIFY_PORT/v1/app/minimum-version" \
  || fail "the service did not come back up after the floor was raised"
raised="$(json "$WORKDIR/min-version-raised.json" '["ios"]["minimum_build"]')"
[[ "$raised" == "4242" ]] || fail "the floor did not follow MIN_SUPPORTED_IOS_BUILD, got $raised"
ok "the floor is raised by configuration, not by a release"

# The gate has to work for a build too old to authenticate — otherwise the builds it exists
# to retire are exactly the ones that cannot discover they must update (Docs/07 §6).
status="$(curl -s -o /dev/null -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/app/minimum-version")"
[[ "$status" == "200" ]] || fail "the version gate requires authentication (status $status)"
ok "it answers without credentials"

# ---------------------------------------------------------------------------------------
ticket "SHIP-29  passwords are stored as argon2id, and nothing reversible is stored"

pushd "$ROOT/services/core" >/dev/null
if ! password_log="$(go test ./internal/identity/ -run TestPassword -count=1 -v 2>&1)"; then
  echo "$password_log"
  popd >/dev/null
  fail "the password tests do not pass"
fi
popd >/dev/null
ok "round trip, wrong password, salting, tampering and truncation all hold"

# The stored form itself, read out of the test that produced it rather than asserted twice.
# A hash is not a secret; the password that made it is a literal in the test file.
sample="$(grep -oE '\$argon2id\$v=19\$m=[0-9]+,t=[0-9]+,p=[0-9]+\$[A-Za-z0-9+/]+\$[A-Za-z0-9+/]+' \
  <<<"$password_log" | head -1)"
[[ -n "$sample" ]] || { echo "$password_log"; fail "no PHC string appeared in the test output"; }
ok "the stored form is a PHC string, carrying its variant, version and all three costs"

# The parameters being inside the hash is what allows the cost to be raised later without
# invalidating a single stored password (Docs/10 §5).
costs="$(cut -d'$' -f4 <<<"$sample")"
[[ "$costs" =~ ^m=[0-9]+,t=[0-9]+,p=[0-9]+$ ]] || fail "the costs are not in the hash: $costs"
ok "the costs travel with the hash — $costs — so raising the profile needs no migration"

# Reversibility is a property of the schema as much as of the code: one credential column, and
# it holds a derived key.
credential_column="$("$PSQL" "$DATABASE_URL" -tAc \
  "select coalesce(string_agg(table_name || '.' || column_name, ', ' order by table_name), 'none')
     from information_schema.columns
    where table_schema = 'public'
      and column_name ~ '(password|secret|passphrase)'")"
[[ "$credential_column" == "users.password_hash" ]] \
  || fail "expected users.password_hash and nothing else, found: $credential_column"
ok "the schema holds exactly one credential column, users.password_hash"

# ---------------------------------------------------------------------------------------
ticket "SHIP-38  one row per device, holding refresh state, a label and last seen"

"$PSQL" "$DATABASE_URL" -tAc \
  "select 1 from information_schema.tables where table_name = 'device_sessions';" | grep -q 1 \
  || fail "the device_sessions table does not exist"
ok "device_sessions exists, at migration 000100 inside identity's reserved block"

columns="$("$PSQL" "$DATABASE_URL" -tAc \
  "select string_agg(column_name || ':' || data_type, ' ' order by column_name)
     from information_schema.columns
    where table_schema = 'public' and table_name = 'device_sessions';")"
for want in "id:uuid" "user_id:uuid" "refresh_token_hash:text" "device_label:text" \
            "last_seen_at:timestamp with time zone" "created_at:timestamp with time zone" \
            "updated_at:timestamp with time zone"; do
  grep -qF "$want" <<<"$columns" || fail "expected a column $want; found: $columns"
done
ok "refresh state, device label and last seen are there, every timestamp with its zone"

# The token is opaque and stored hashed (Docs/10 §5), so what the column may not hold is the
# token. Uniqueness is the part the database has to enforce: one token, one session.
# -q as well as -tA: without it psql appends its own "INSERT 0 1" status line to the value, and
# a captured id with a command tag stuck to it produces a uuid syntax error later rather than a
# constraint failure — which is a check that passes for the wrong reason.
verify_user="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into users (id, email, phone, password_hash, role)
   values (gen_random_uuid(), 'session-$$@example.com', '+6140003$$', 'x', 'customer')
   returning id;")"
"$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions (id, user_id, refresh_token_hash, device_label)
   values (gen_random_uuid(), '$verify_user', 'hash-$$', 'Verify iPhone');" >/dev/null \
  || fail "a device session could not be created"

if "$PSQL" "$DATABASE_URL" -q -c \
  "insert into device_sessions (id, user_id, refresh_token_hash, device_label)
   values (gen_random_uuid(), '$verify_user', 'hash-$$', 'Verify Pixel');" >/dev/null 2>&1; then
  fail "two sessions hold the same refresh token hash"
fi
ok "a refresh token hash belongs to exactly one session"

if "$PSQL" "$DATABASE_URL" -q -c \
  "delete from users where id = '$verify_user';" >/dev/null 2>&1; then
  fail "deleting the account took its sessions with it; the foreign key is not RESTRICT"
fi
ok "ON DELETE RESTRICT holds, so sessions cannot vanish with a deleted account"

trigger="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from pg_trigger tr
     join pg_class cl on cl.oid = tr.tgrelid
     join pg_proc p   on p.oid  = tr.tgfoid
    where cl.relname = 'device_sessions' and p.proname = 'set_updated_at'
      and not tr.tgisinternal;")"
[[ "$trigger" == "1" ]] || fail "device_sessions has no set_updated_at trigger"

fk_index="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from pg_index i
     join pg_class t     on t.oid = i.indrelid
     join pg_attribute a on a.attrelid = t.oid and a.attnum = i.indkey[0]
    where t.relname = 'device_sessions' and a.attname = 'user_id';")"
[[ "$fk_index" -ge 1 ]] || fail "the foreign key on device_sessions.user_id is not indexed"
ok "the updated_at trigger is attached and the foreign key is indexed"

# ---------------------------------------------------------------------------------------
ticket "SHIP-37  a short-lived signed token carrying the user, the role and an expiry"

pushd "$ROOT/services/core" >/dev/null
if ! token_log="$(go test ./internal/identity/ -run 'TestAccessToken|TestKeyset|TestNewAccessToken' -count=1 -v 2>&1)"; then
  echo "$token_log"
  popd >/dev/null
  fail "the access token tests do not pass"
fi
popd >/dev/null
ok "the TTL, kid selection, rotation and the driver-audience refusal all hold"

# A real token, decoded here rather than by the library that produced it. Asking the issuer what
# it issued would prove very little; this reads the bytes that would go over the wire.
access_token="$(grep -oE 'access token: [A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+' <<<"$token_log" \
  | head -1 | awk '{print $3}')"
[[ -n "$access_token" ]] || { echo "$token_log"; fail "no access token appeared in the test output"; }

python3 - "$access_token" >"$WORKDIR/token.json" <<'PYTHON'
import base64, json, sys

def segment(s):
    return json.loads(base64.urlsafe_b64decode(s + "=" * (-len(s) % 4)))

header, payload, _signature = sys.argv[1].split(".")
header, payload = segment(header), segment(payload)

json.dump({
    "alg":         header.get("alg"),
    "kid":         header.get("kid"),
    "claim_names": " ".join(sorted(payload)),
    "iss":         payload.get("iss"),
    "aud":         payload.get("aud"),
    "sub":         payload.get("sub"),
    "role":        payload.get("role"),
    "lifetime":    payload.get("exp", 0) - payload.get("iat", 0),
}, sys.stdout)
PYTHON

[[ "$(json "$WORKDIR/token.json" '["alg"]')" == "HS256" ]] \
  || fail "the token is not signed with HS256"
kid="$(json "$WORKDIR/token.json" '["kid"]')"
[[ -n "$kid" && "$kid" != "None" ]] \
  || fail "the token names no signing key, so a key could never be rotated"
ok "HS256, naming its key in the header as kid=$kid"

# Exactly the claim set Docs/10 §5 fixes, checked as a whole. A missing claim breaks something
# loudly; an added one sits there being believed, which is the direction that matters.
claims="$(json "$WORKDIR/token.json" '["claim_names"]')"
[[ "$claims" == "aud exp iat iss jti role sid sub" ]] \
  || fail "the claims are '$claims', and Docs/10 §5 says exactly: aud exp iat iss jti role sid sub"
ok "the claim set is exactly sub, role, sid, iat, exp, jti, iss and aud"

for forbidden in permissions scope scopes verified email_verified phone_verified status; do
  grep -qw "$forbidden" <<<"$claims" && fail "the token carries '$forbidden'"
done
ok "no permission and no verification state — the platform decides both, freshly (Docs/07 §3)"

[[ "$(json "$WORKDIR/token.json" '["iss"]')" == "shipper" ]] || fail "iss is not shipper"
[[ "$(json "$WORKDIR/token.json" '["aud"]')" == "shipper-mobile" ]] \
  || fail "aud is not shipper-mobile, which is what separates this from the driver token"
[[ "$(json "$WORKDIR/token.json" '["role"]')" =~ ^(customer|provider)$ ]] \
  || fail "role is not one of the two the platform issues"
[[ "$(json "$WORKDIR/token.json" '["sub"]')" =~ ^[0-9a-f-]{36}$ ]] || fail "sub is not a user id"
ok "iss=shipper, aud=shipper-mobile, and sub carries the user id"

[[ "$(json "$WORKDIR/token.json" '["lifetime"]')" == "900" ]] \
  || fail "the token lives $(json "$WORKDIR/token.json" '["lifetime"]') seconds, and Docs/10 §5 says 900"
ok "it expires fifteen minutes after it was issued, measured against an injected clock"

# ---------------------------------------------------------------------------------------
ticket "SHIP-30  POST /v1/auth/register creates an unverified account and rejects duplicates"

# Every value is suffixed with the process id, because this runs against the developer's own
# database rather than a throwaway one and the accounts it creates stay there. A fixed address
# would pass once and then report "already taken" forever.
reg_email="register-$$@example.com"
reg_phone_local="04120$$"
reg_phone_e164="+61${reg_phone_local:1}"

# post_json <key> <path> <body> <outfile> — one state-changing request, answering with its status.
post_json() {
  curl -s -X POST -o "$4" -w '%{http_code}' \
    -H "Idempotency-Key: $1" -H 'Content-Type: application/json' \
    -d "$3" "http://localhost:$VERIFY_PORT$2"
}

status="$(post_json "verify-reg-$$" /v1/auth/register \
  "{\"email\":\"$reg_email\",\"phone\":\"$reg_phone_local\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/register.json"; fail "POST /v1/auth/register returned $status, want 201"; }
ok "an account is created and answered with 201"

registered_id="$(json "$WORKDIR/register.json" '["id"]')"
[[ "$registered_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the response carries no account id: $registered_id"
[[ "$(json "$WORKDIR/register.json" '["role"]')" == "customer" ]] \
  || fail "the account did not take the role it was registered with"
[[ "$(json "$WORKDIR/register.json" '["status"]')" == "active" ]] \
  || fail "a new account is not active"

# The half of the criterion that is easiest to lose: *unverified*. Docs/04 §2 requires both
# channels verified before a customer may publish, so an account that arrived verified would
# skip the whole of SHIP-31, 33, 34 and 36 without anything failing.
[[ "$(json "$WORKDIR/register.json" '["email_verified"]')" == "False" ]] \
  || fail "a brand new account reports its email as verified"
[[ "$(json "$WORKDIR/register.json" '["phone_verified"]')" == "False" ]] \
  || fail "a brand new account reports its phone as verified"
ok "it is unverified on both channels, and says so"

# The response is one thing; the row is another. Read the columns the endpoint claims to have
# written, from the database rather than from the answer it gave about itself.
stored="$("$PSQL" "$DATABASE_URL" -tAc \
  "select phone || ' ' || role || ' ' || status || ' ' ||
          coalesce(email_verified_at::text, 'null') || ' ' ||
          coalesce(phone_verified_at::text, 'null')
     from users where id = '$registered_id';")"
[[ "$stored" == "$reg_phone_e164 customer active null null" ]] \
  || fail "the stored row is '$stored', want '$reg_phone_e164 customer active null null'"
ok "the row holds the number in E.164 and both verification timestamps null"

# The password is not recoverable from what was stored. SHIP-29 proves the format; this proves
# the endpoint uses it rather than writing the plaintext into the same column.
stored_hash="$("$PSQL" "$DATABASE_URL" -tAc \
  "select password_hash from users where id = '$registered_id';")"
[[ "$stored_hash" == \$argon2id\$* ]] || fail "the stored credential is not a PHC string: $stored_hash"
grep -q 'correct-horse-battery-staple' <<<"$stored_hash" && fail "the password is in the stored value"
ok "the credential column holds an argon2id hash, not the password"

status="$(post_json "verify-reg-dupe-email-$$" /v1/auth/register \
  "{\"email\":\"$reg_email\",\"phone\":\"0499${$}0\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/register-dupe-email.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/register-dupe-email.json"; fail "a duplicate email returned $status, want 409"; }
[[ "$(json "$WORKDIR/register-dupe-email.json" '["error"]["code"]')" == "identity_email_taken" ]] \
  || fail "expected code=identity_email_taken"
ok "a second account on the same address is refused by uq_users_email"

# The same number in the form a person types it rather than the form it is stored in. Without
# normalisation these are two different strings and the index never sees a collision — which is
# the defect that would give one handset two accounts and make an OTP ambiguous.
status="$(post_json "verify-reg-dupe-phone-$$" /v1/auth/register \
  "{\"email\":\"other-$$@example.com\",\"phone\":\"${reg_phone_local:0:4} ${reg_phone_local:4}\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/register-dupe-phone.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/register-dupe-phone.json"; fail "a duplicate phone returned $status, want 409"; }
[[ "$(json "$WORKDIR/register-dupe-phone.json" '["error"]["code"]')" == "identity_phone_taken" ]] \
  || fail "expected code=identity_phone_taken"
ok "the same number written differently is still the same number"

status="$(post_json "verify-reg-invalid-$$" /v1/auth/register \
  '{"email":"not-an-address","phone":"123","password":"short","role":"driver"}' \
  "$WORKDIR/register-invalid.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/register-invalid.json"; fail "an invalid registration returned $status, want 422"; }
fields="$(json "$WORKDIR/register-invalid.json" '["error"]["details"]' | tr -d "[]{}'\" " | tr ',' '\n' | grep '^field:' | cut -d: -f2 | sort | tr '\n' ' ')"
[[ "$fields" == "email password phone role " ]] \
  || fail "the rejected fields are '$fields', want all four at once"
ok "every bad field is reported at once, so the form takes one round trip and not four"

# Registration is public — it is how a caller obtains credentials in the first place — and it is
# still behind the idempotency middleware like every other state-changing request.
status="$(curl -s -X POST -o "$WORKDIR/register-nokey.json" -w '%{http_code}' \
  -H 'Content-Type: application/json' -d '{}' "http://localhost:$VERIFY_PORT/v1/auth/register")"
[[ "$status" == "400" ]] || fail "registration without an Idempotency-Key returned $status"
[[ "$(json "$WORKDIR/register-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || fail "expected code=idempotency_key_required"
ok "it needs an Idempotency-Key, so a retry cannot produce a second account"

# ---------------------------------------------------------------------------------------
ticket "SHIP-45  the role is chosen at registration and cannot be changed afterwards"

status="$(post_json "verify-provider-$$" /v1/auth/register \
  "{\"email\":\"provider-$$@example.com\",\"phone\":\"0498$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/register-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/register-provider.json"; fail "registering a provider returned $status"; }
[[ "$(json "$WORKDIR/register-provider.json" '["role"]')" == "provider" ]] \
  || fail "the account did not take the provider role"
ok "an account is created as either customer or provider, as asked"

status="$(post_json "verify-admin-role-$$" /v1/auth/register \
  "{\"email\":\"admin-$$@example.com\",\"phone\":\"0497$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"admin\"}" \
  "$WORKDIR/register-admin.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/register-admin.json"; fail "'admin' was accepted as a role (status $status)"; }
ok "there is no third role — administrators sign in through a separate system (SHIP-147)"

# The half that matters, and the reason it is a trigger. A rule the application keeps does not
# apply to a support query typed at a psql prompt, which is precisely the path somebody would use
# to change a role "just this once" — and a customer becoming a provider silently rewrites the
# meaning of every job already attached to the account.
if "$PSQL" "$DATABASE_URL" -q -c \
  "update users set role = 'provider' where id = '$registered_id';" >/dev/null 2>&1; then
  fail "a customer was turned into a provider from a psql prompt"
fi
ok "UPDATE ... SET role is refused by the database, not by application logic alone"

surviving_role="$("$PSQL" "$DATABASE_URL" -tAc \
  "select role from users where id = '$registered_id';")"
[[ "$surviving_role" == "customer" ]] || fail "the role is now '$surviving_role'"
ok "the role survived the attempt"

# The trigger has a WHEN clause, so the writes that happen constantly — verification timestamps,
# account standing — are untouched by it. Without that, every one of them would raise, and the
# failure would look like a broken endpoint rather than a mis-scoped trigger.
"$PSQL" "$DATABASE_URL" -q -c \
  "update users set status = 'restricted' where id = '$registered_id';" >/dev/null \
  || fail "an unrelated update was refused by the role trigger"
"$PSQL" "$DATABASE_URL" -q -c \
  "update users set status = 'active' where id = '$registered_id';" >/dev/null
ok "every other column still updates, so the trigger is scoped to the transition it guards"

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

printf '\n\033[32m%s checks passed — SHIP-1..SHIP-15, SHIP-28..SHIP-30, SHIP-37, SHIP-38, SHIP-44, SHIP-45, SHIP-149 and SHIP-167 acceptance criteria demonstrated.\033[0m\n\n' "$pass"
