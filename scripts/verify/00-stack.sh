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

# ---------------------------------------------------------------------------------------
ticket "SHIP-15p  object store reachable, with a bucket and working pre-signed URLs"

# Everything here is stack rather than service, which is why it sits in this file and runs
# before the API is built.
#
# The point of the section is the *last three* checks rather than the first two. Docs/11 §6
# struck SHIP-114 for five consecutive waves because "a client receives a short-lived pre-signed
# URL and uploads directly" could not be demonstrated against a stack that ran postgres, redis
# and kafka and nothing else. This demonstrates it, before the ticket that needs it opens.
#
# **The object key carries $$ and every assertion names it.** The stack is shared between
# worktrees and the bucket may be too, so this fences on an id rather than on a count or a
# timestamp — the rule CLAUDE.md's worktree table states for Kafka, applied to the one shared
# service that can be isolated but is not obliged to be.

storage_live="$(curl -s -o /dev/null -w '%{http_code}' "$STORAGE_ENDPOINT/minio/health/live")"
[[ "$storage_live" == "200" ]] \
  || fail "$STORAGE_ENDPOINT/minio/health/live answered $storage_live — is the stack up? (make up)"
ok "the object store is live on the host at $STORAGE_ENDPOINT"

# presign <method> <key> — a SigV4 pre-signed URL, printed.
#
# Signed here rather than fetched from `mc share`, and that is the whole reason this check has
# teeth: SigV4 covers the `host` header, so a URL minted inside the compose network against
# minio:9000 is refused when fetched from the host. Signing for the address the client will
# actually use is what SHIP-114 has to get right, so it is what this exercises.
presign() {
  python3 - "$1" "$2" <<'PY'
import datetime, hashlib, hmac, os, sys, urllib.parse

method, key = sys.argv[1], sys.argv[2]
endpoint = os.environ["STORAGE_ENDPOINT"]
host = urllib.parse.urlsplit(endpoint).netloc
access = os.environ["STORAGE_ACCESS_KEY_ID"]
secret = os.environ["STORAGE_SECRET_ACCESS_KEY"]
region = os.environ["STORAGE_REGION"]
bucket = os.environ["STORAGE_BUCKET"]

now = datetime.datetime.now(datetime.timezone.utc)
amzdate, datestamp = now.strftime("%Y%m%dT%H%M%SZ"), now.strftime("%Y%m%d")
scope = f"{datestamp}/{region}/s3/aws4_request"
uri = "/" + bucket + "/" + urllib.parse.quote(key)

query = {
    "X-Amz-Algorithm": "AWS4-HMAC-SHA256",
    "X-Amz-Credential": f"{access}/{scope}",
    "X-Amz-Date": amzdate,
    "X-Amz-Expires": "300",
    "X-Amz-SignedHeaders": "host",
}
canonical_query = "&".join(
    f"{urllib.parse.quote(k, safe='-_.~')}={urllib.parse.quote(v, safe='-_.~')}"
    for k, v in sorted(query.items())
)
canonical_request = "\n".join(
    [method, uri, canonical_query, f"host:{host}\n", "host", "UNSIGNED-PAYLOAD"]
)
to_sign = "\n".join(
    ["AWS4-HMAC-SHA256", amzdate, scope, hashlib.sha256(canonical_request.encode()).hexdigest()]
)

def sign(k, m):
    return hmac.new(k, m.encode(), hashlib.sha256).digest()

signing_key = sign(sign(sign(sign(("AWS4" + secret).encode(), datestamp), region), "s3"), "aws4_request")
signature = hmac.new(signing_key, to_sign.encode(), hashlib.sha256).hexdigest()

print(f"{endpoint}{uri}?{canonical_query}&X-Amz-Signature={signature}")
PY
}

export STORAGE_ENDPOINT STORAGE_BUCKET STORAGE_REGION STORAGE_ACCESS_KEY_ID STORAGE_SECRET_ACCESS_KEY

object_key="verify/$$/proof.txt"
object_body="verify-proof-payload-$$"

# A pre-signed PUT against a bucket that does not exist answers 404, so this is also the check
# that `make up` created one — with a message that says which bucket and what to run.
put_url="$(presign PUT "$object_key")"
put_status="$(curl -s -o /dev/null -w '%{http_code}' -X PUT --data-binary "$object_body" "$put_url")"
[[ "$put_status" == "200" ]] \
  || fail "a pre-signed PUT into $STORAGE_BUCKET answered $put_status — run 'make up', which creates the bucket"
ok "a pre-signed URL uploaded an object directly, with no request through the API"

get_url="$(presign GET "$object_key")"
fetched="$(curl -s "$get_url")"
[[ "$fetched" == "$object_body" ]] || fail "a pre-signed GET returned '$fetched', expected '$object_body'"
ok "a pre-signed URL read the same bytes back"

unsigned="$(curl -s -o /dev/null -w '%{http_code}' "$STORAGE_ENDPOINT/$STORAGE_BUCKET/$object_key")"
[[ "$unsigned" == "403" ]] \
  || fail "an unsigned GET of $object_key answered $unsigned, expected 403 — there is no public read path"
ok "the same object is refused without a signature: everything in the bucket is private"

"${COMPOSE[@]}" exec -T minio sh -c 'set -e; \
  mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; \
  mc rm --recursive --force "local/'"$STORAGE_BUCKET"'/verify/'"$$"'" >/dev/null' \
  || fail "could not remove the object this section created"
ok "the object this run created was removed"
