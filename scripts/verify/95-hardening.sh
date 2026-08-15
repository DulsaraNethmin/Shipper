# shellcheck shell=bash
#
# M7 hardening — SHIP-167, the minimum-build gate.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# This section restarts the service with a different MIN_SUPPORTED_IOS_BUILD, because "the floor
# follows configuration rather than a release" cannot be shown without doing so. It reassigns
# $SERVER_PID, which the runner's cleanup trap and its SIGTERM check both read — so this file
# runs last of the sections, and a new section that also restarts the service belongs after it.

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

wait_for_health || true   # the next request reports the failure with the better message

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

ticket "SHIP-167a the app is told the two numbers it applies on the device"

# The service is currently running with MIN_SUPPORTED_IOS_BUILD=4242 and nothing else overridden,
# so this reads the documented defaults — which is worth checking on its own: they are what the
# pilot ships with and what every handset gets until somebody moves them.
status="$(curl -s -o "$WORKDIR/policy.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/app/policy")"
[[ "$status" == "200" ]] || fail "GET /v1/app/policy returned $status"

nudge="$(json "$WORKDIR/policy.json" '["unsynced_nudge_after_seconds"]')"
budget="$(json "$WORKDIR/policy.json" '["proof_compression_budget_bytes"]')"
[[ "$nudge" == "14400" ]]   || fail "unsynced_nudge_after_seconds is not Docs/02 §3.1's four hours: $nudge"
[[ "$budget" == "1048576" ]] || fail "proof_compression_budget_bytes is not one mebibyte: $budget"
ok "the threshold and the budget are served, at the defaults the client compiles in (${nudge}s, ${budget}B)"

# The same argument SHIP-167 makes about the floor, and the reason this row exists at all: both
# numbers are operations tuning knobs on a client with no over-the-air path for Dart code, so a
# restart has to be able to move them. A second restart rather than folding the variables into the
# one above, so that a failure names which endpoint stopped following its configuration.
kill -TERM "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true

SHIPPER_ENV=development \
HTTP_PORT="$VERIFY_PORT" \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
UNSYNCED_NUDGE_AFTER=90m \
PROOF_COMPRESSION_BUDGET_BYTES=262144 \
  "$WORKDIR/shipper-api" >>"$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

wait_for_health || true   # the next request reports the failure with the better message

curl -fsS -o "$WORKDIR/policy-moved.json" \
  "http://localhost:$VERIFY_PORT/v1/app/policy" \
  || fail "the service did not come back up after the client policy was moved"

moved_nudge="$(json "$WORKDIR/policy-moved.json" '["unsynced_nudge_after_seconds"]')"
moved_budget="$(json "$WORKDIR/policy-moved.json" '["proof_compression_budget_bytes"]')"
[[ "$moved_nudge" == "5400" ]] || fail "the nudge threshold did not follow UNSYNCED_NUDGE_AFTER, got $moved_nudge"
[[ "$moved_budget" == "262144" ]] || fail "the budget did not follow PROOF_COMPRESSION_BUDGET_BYTES, got $moved_budget"
ok "both numbers are moved by configuration, not by a store release"

# It travels as seconds rather than as a Go duration string. A client parsing "1h30m0s" would be
# parsing this service's implementation — see `retry_after_seconds` in contracts/paths/identity.yaml.
grep -q '"unsynced_nudge_after_seconds":5400' "$WORKDIR/policy-moved.json" \
  || fail "the threshold is not a bare integer of seconds on the wire: $(cat "$WORKDIR/policy-moved.json")"
ok "the threshold is seconds on the wire, not a duration string"

# Nothing in the response is about the caller, and the app reads it on a device that may have no
# usable session — the nudge fires precisely when the platform is unreachable.
status="$(curl -s -o /dev/null -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/app/policy")"
[[ "$status" == "200" ]] || fail "the client policy requires authentication (status $status)"
ok "it answers without credentials"
