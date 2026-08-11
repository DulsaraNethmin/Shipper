# shellcheck shell=bash
#
# SHIP-1 … SHIP-4 — the local stack: Postgres, Redis and Kafka, reached from the host.
#
# Sourced by scripts/verify-foundation.sh, which owns the harness — ticket, ok, fail, json,
# post_json, mint_token, the service lifecycle, $pass and the summary. `source` runs in the
# runner's shell, so everything it set is in scope here and every ok() counts towards the
# total it prints.
#
# 00–09 runs before the service is built, so nothing here may assume a listening port.

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
