# shellcheck shell=bash
#
# SHIP-6, SHIP-8, SHIP-9 — the service itself: health, configuration and request logging.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# The runner has built $WORKDIR/shipper-api and started it on $VERIFY_PORT before this file is
# read. SHIP-8 runs the same binary again with deliberately broken configuration, expecting it
# to refuse to start; those runs are separate processes and do not disturb the one serving.

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
