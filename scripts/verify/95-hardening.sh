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
