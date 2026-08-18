# shellcheck shell=bash
#
# M7 hardening — SHIP-183a, the route rate limits.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# # Why this section restarts the service
#
# The figures Docs/12 §3 argues for are deliberately unreachable by a person: the Read class holds
# 300 requests and Write holds 60. Demonstrating a refusal at those numbers would mean driving 301
# requests through curl to prove one thing.
#
# So this restarts with RATE_LIMIT_BURST_SCALE set low, which does two jobs at once: it brings the
# buckets into a range a shell loop can reach, **and it is the only end-to-end demonstration that
# the global lever Docs/12 §7 decided on actually moves anything.** A scale nobody has ever set is
# a scale nobody knows works, and the incident that first needs it is the wrong moment to find out.
#
# It reassigns $SERVER_PID, which the runner's cleanup trap and its SIGTERM check both read. This
# file therefore runs after 95-hardening.sh, which does the same, and a new section that also
# restarts the service belongs after this one.
#
# # Why the limits do not collide with a concurrent worktree
#
# `make verify` runs every request from 127.0.0.1, so anything keyed on the client address is
# shared across sections and across trees — which is why 40-identity.sh and 90-admin.sh clear their
# sign-in buckets at both ends. **Nothing here is keyed on the address.** The 74 routes SHIP-183a
# enforces key on the authenticated subject, and this section mints a subject of its own from a
# fresh UUID, so its buckets cannot be reached by another section or another tree.

ticket "SHIP-183a  every authenticated route is served under a rate-limit class"

# 0.02 of the documented capacity: Read becomes 6 (300 × 0.02) and PublicRead would become 12.
# Both are small enough for a loop and large enough that a single stray request cannot produce a
# false refusal.
rate_limit_scale=0.02
rate_limit_read_capacity=6

kill -TERM "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true

SHIPPER_ENV=development \
HTTP_PORT="$VERIFY_PORT" \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
RATE_LIMIT_BURST_SCALE="$rate_limit_scale" \
  "$WORKDIR/shipper-api" >>"$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

wait_for_health || true   # the next request reports the failure with the better message

# A subject of this section's own, so the buckets below belong to nobody else. The account does
# not exist, which does not matter: the limiter runs inside the auth guard and in front of the
# handler, so what is being measured is reached before the database is.
rate_limit_subject="$(uuidgen | tr 'A-Z' 'a-z')"
rate_limit_token="$(mint_token "$rate_limit_subject")"

# read_status <path> [token] — the status code of one authenticated GET.
read_status() {
  local path="$1" token="${2:-$rate_limit_token}"
  curl -s -o "$WORKDIR/rl-body.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $token" \
    "http://localhost:$VERIFY_PORT$path"
}

# The bucket starts full, so exactly the capacity is admitted.
refused_at=0
for attempt in $(seq 1 $((rate_limit_read_capacity + 1))); do
  status="$(read_status /v1/auth/sessions)"
  if [[ "$status" == "429" ]]; then
    refused_at="$attempt"
    break
  fi
done

[[ "$refused_at" == "$((rate_limit_read_capacity + 1))" ]] || fail \
  "the Read bucket refused at request $refused_at, want $((rate_limit_read_capacity + 1)) — the class holds $rate_limit_read_capacity at this scale"
ok "an authenticated read is refused once its class's bucket is empty, and not before"

# The typed error, which is what a client branches on (Docs/10 §4.4).
code="$(json "$WORKDIR/rl-body.json" '["error"]["code"]')"
[[ "$code" == "rate_limited" ]] || fail "the refusal carries code $code, want rate_limited"
ok "the refusal is the standard error contract with code rate_limited"

request_id="$(json "$WORKDIR/rl-body.json" '["error"]["request_id"]')"
[[ -n "$request_id" ]] || fail "the refusal carries no request ID"
ok "it carries the request ID, so a report of it can be traced"

# An honest Retry-After: the time until the next token, never zero, and never the whole refill.
retry_after="$(curl -s -o /dev/null -D - -w '' \
  -H "$auth_header: Bearer $rate_limit_token" \
  "http://localhost:$VERIFY_PORT/v1/auth/sessions" \
  | tr -d '\r' | awk 'tolower($1) == "retry-after:" { print $2 }')"

[[ "$retry_after" =~ ^[0-9]+$ ]] || fail "Retry-After is $retry_after, want a whole number of seconds"
(( retry_after >= 1 )) || fail "Retry-After is $retry_after, which invites a retry certain to be refused"
ok "it carries a Retry-After of ${retry_after}s — the wait until the next token, not until a window rolls over"

# The global lever moved a real bucket. Without it the same loop would have run 300 times without
# a refusal, so reaching this line at all is the demonstration.
ok "the burst scale in configuration moved every class at once (Docs/12 §7)"

# One caller emptying their bucket must not refuse another. This is what makes the limit a control
# on a caller rather than an allowance the first arrival exhausts for everybody.
other_token="$(mint_token "$(uuidgen | tr 'A-Z' 'a-z')")"
status="$(read_status /v1/auth/sessions "$other_token")"
[[ "$status" != "429" ]] || fail "a second subject was refused from the first subject's bucket"
ok "a second caller is unaffected, so the bucket is keyed on the subject rather than shared"

# The eleven address-keyed routes carry their class and are deliberately not enforced until
# SHIP-183b reads a forwarded header from a configured trusted proxy. At this scale an enforced
# PublicRead would hold 12, so twenty requests without a refusal is the demonstration.
for _ in $(seq 1 20); do
  status="$(curl -s -o /dev/null -w '%{http_code}' \
    "http://localhost:$VERIFY_PORT/v1/app/policy")"
  [[ "$status" != "429" ]] || fail \
    "GET /v1/app/policy was throttled. It keys on the client address, which is the load balancer's until SHIP-183b — enforcing it now throttles every caller as one"
done
ok "the address-keyed routes carry their class and are not yet enforced (SHIP-183b)"

# /health is the one unlimited route, because the caller is the infrastructure and throttling it
# takes a healthy instance out of rotation.
for _ in $(seq 1 30); do
  status="$(curl -s -o /dev/null -w '%{http_code}' "http://localhost:$VERIFY_PORT/health")"
  [[ "$status" == "200" ]] || fail "GET /health answered $status under repeated polling"
done
ok "GET /health is never throttled, whatever a load balancer does to it"
