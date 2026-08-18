# shellcheck shell=bash
#
# M7 hardening — SHIP-183b, the address-keyed rate limits behind a trusted proxy.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# # What this section is for
#
# Eleven of the 86 routes key their limit on the client's network address (Docs/12 §8). SHIP-183a
# declared the class on each and enforced none, because internal/httpx read RemoteAddr alone —
# behind a load balancer that is the balancer, so all eleven would have collapsed into one bucket
# per class and refused the world at the first caller. SHIP-183b reads a forwarded header as far as
# a configured trusted proxy allows, and this is the end-to-end demonstration of both halves:
#
#   - **No caller can choose their own bucket.** The security half. An implementation that simply
#     believed X-Forwarded-For would pass every availability check below and be worse than having
#     no limit at all, because it would look like one.
#   - **Two callers behind one proxy are counted apart.** The availability half. An implementation
#     that ignored every header would pass every security check below and throttle the world.
#
# Both are asserted, because either alone is satisfied by an implementation that fails the other.
#
# # Why this section restarts the service, twice
#
# Once with a trusted-proxy hop count, which is the deployed shape and the only one in which the
# address-keyed classes can be exercised at all. Once without, because the *default* configuration
# — trust nothing — is what a deployment that has said nothing runs, and "a forwarded header is
# ignored entirely" is a property of that shape rather than of the first.
#
# It reassigns $SERVER_PID, which the runner's cleanup trap and its SIGTERM check both read, so it
# runs after 96-rate-limits.sh and a new section that also restarts belongs after this one.
#
# # Why the buckets here cannot collide with another section or another worktree
#
# This is the whole reason the section is separate from 96. `make verify` reaches the service from
# 127.0.0.1, and so does every other section and every other tree on the machine — so a bucket keyed
# on the client address is one nobody owns, and CLAUDE.md's rule for a shared key applies: **fence
# on an id, never on timing.** A refill is five minutes wide on the Message class, so a window is
# no fence at all.
#
# The id here is the client address itself. With a hop count configured the address comes from the
# header, so this section mints one out of a UUID and every bucket it touches is unreachable by
# anything that did not know the UUID. That is also why the two restarts below are cheap to reason
# about: nothing in this file depends on the state of any bucket it did not create.

ticket "SHIP-183b  the address-keyed limits are enforced behind a trusted proxy"

# --- The configuration refuses to guess -------------------------------------------------
#
# Docs/09's Done when offers the two mechanisms as alternatives — "a configured trusted-proxy hop
# count *or* CIDR allow-list". They answer the same question by different means, and the plausible
# rules for combining them disagree: does the allow-list gate whether the header is read at all, or
# does it filter which hops the count skips? A deployment that set both would get whichever the code
# happened to prefer, which is a security posture nobody chose.
set +e
SHIPPER_ENV=development \
HTTP_PORT="$VERIFY_PORT" \
LOG_FORMAT=json \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
TRUSTED_PROXY_HOPS=1 \
TRUSTED_PROXY_NETWORKS=10.0.0.0/8 \
  "$WORKDIR/shipper-api" >"$WORKDIR/trusted-proxy-both.log" 2>&1
both_status=$?
set -e

(( both_status != 0 )) || fail \
  "the service started with both TRUSTED_PROXY_HOPS and TRUSTED_PROXY_NETWORKS set. There is no agreed rule for combining them, so it would be trusting whichever one the code happened to prefer"
grep -q 'TRUSTED_PROXY_HOPS' "$WORKDIR/trusted-proxy-both.log" || fail \
  "the refusal does not name TRUSTED_PROXY_HOPS, so nobody reading it knows what to unset"
ok "setting both a hop count and an allow-list refuses to start rather than picking one"

# --- Behind one trusted proxy -----------------------------------------------------------

# 0.02 of the documented capacity, the same scale 96 uses and for the same reason: PublicRead
# holds 600 and demonstrating a refusal at that figure means driving 601 requests through curl.
# It is also the only end-to-end demonstration that the global lever moves an address-keyed class.
proxy_scale=0.02
proxy_public_read_capacity=12   # 600 × 0.02

kill -TERM "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true

SHIPPER_ENV=development \
HTTP_PORT="$VERIFY_PORT" \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
RATE_LIMIT_BURST_SCALE="$proxy_scale" \
TRUSTED_PROXY_HOPS=1 \
  "$WORKDIR/shipper-api" >>"$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

wait_for_health || true   # the next request reports the failure with the better message

# A client address of this section's own, out of a UUID — see the note above on why an id is the
# only fence that works here. The IPv6 documentation prefix, because the /64 is also what the two
# addresses below share, which is the other thing worth demonstrating.
proxy_net="2001:db8:$(uuidgen | tr -d - | cut -c1-4):$(uuidgen | tr -d - | cut -c1-4)"
proxy_client="${proxy_net}::1"
proxy_sibling="${proxy_net}::2"
proxy_stranger="2001:db8:$(uuidgen | tr -d - | cut -c1-4):$(uuidgen | tr -d - | cut -c1-4)::1"

# forwarded_status <chain> [path] — the status of one GET presenting chain as X-Forwarded-For.
#
# The chain is written exactly as a proxy would leave it: whatever the caller sent, with the
# address the proxy accepted the connection from appended on the right.
forwarded_status() {
  curl -s -o "$WORKDIR/tp-body.json" -w '%{http_code}' \
    -H "X-Forwarded-For: $1" \
    "http://localhost:$VERIFY_PORT${2:-/v1/app/policy}"
}

# The bucket starts full, so exactly the capacity is admitted and the next is not.
refused_at=0
for attempt in $(seq 1 $((proxy_public_read_capacity + 1))); do
  status="$(forwarded_status "$proxy_client")"
  if [[ "$status" == "429" ]]; then
    refused_at="$attempt"
    break
  fi
done

[[ "$refused_at" == "$((proxy_public_read_capacity + 1))" ]] || fail \
  "the PublicRead bucket refused at request $refused_at, want $((proxy_public_read_capacity + 1)) — the class holds $proxy_public_read_capacity at this scale, and this is a route nothing else on the machine can reach"
ok "an address-keyed route is refused once its class's bucket is empty, and not before"

code="$(json "$WORKDIR/tp-body.json" '["error"]["code"]')"
[[ "$code" == "rate_limited" ]] || fail "the refusal carries code $code, want rate_limited"
ok "the refusal is the standard error contract with code rate_limited"

retry_after="$(curl -s -o /dev/null -D - -w '' \
  -H "X-Forwarded-For: $proxy_client" \
  "http://localhost:$VERIFY_PORT/v1/app/policy" \
  | tr -d '\r' | awk 'tolower($1) == "retry-after:" { print $2 }')"
[[ "$retry_after" =~ ^[0-9]+$ ]] || fail "Retry-After is $retry_after, want a whole number of seconds"
(( retry_after >= 1 )) || fail "Retry-After is $retry_after, which invites a retry certain to be refused"
ok "it carries a Retry-After of ${retry_after}s — the wait until the next token"

# --- The security half: no caller can choose their own bucket ---------------------------
#
# The client's allowance is spent. Everything a caller controls is to the *left* of what the proxy
# appended, so none of these may buy a fresh bucket. This is the case that fails loudly if the
# chain is ever read from the left instead of from the right.
for forged in \
  "$proxy_stranger, $proxy_client" \
  "203.0.113.1, 203.0.113.2, $proxy_client" \
  "unknown, $proxy_client" \
  "127.0.0.1, $proxy_client"; do
  status="$(forwarded_status "$forged")"
  [[ "$status" == "429" ]] || fail \
    "X-Forwarded-For: '$forged' answered $status rather than 429. The caller moved themselves to a fresh bucket by prepending to the chain, so the limit is evaded and every one of the eleven routes is unenforced"
done
ok "prepending to the forwarded chain does not buy a fresh bucket — the chain is read from the right"

# The same address one /64 apart. A residential IPv6 subscriber is handed a /64 at least, so a
# limit keyed on the full address is one they escape by picking a different one from their own
# prefix — enforced against IPv4 callers and unenforceable against IPv6 ones (Docs/12 §11).
status="$(forwarded_status "$proxy_sibling")"
[[ "$status" == "429" ]] || fail \
  "an address in the same /64 answered $status rather than 429, so one IPv6 caller holds a bucket per address and the class is unenforceable against them"
ok "two IPv6 addresses in one /64 are one caller"

# --- The availability half: two callers behind one proxy are counted apart --------------
#
# Without this the eleven routes are worse than unlimited behind a balancer: every caller shares one
# bucket and the first to empty it refuses everybody. An implementation that ignored every header
# passes every check above and fails this one.
status="$(forwarded_status "$proxy_stranger")"
[[ "$status" != "429" ]] || fail \
  "a second client behind the same proxy was refused on its first request, so every caller behind the balancer is counted as one and the first throttles the world"
ok "a different client behind the same proxy has its own bucket"

# The Message class, which is where Docs/12 §1 found the worst gap on the whole surface:
# POST /v1/auth/register had no limit of any kind, unauthenticated, and it both creates an account
# and sends an email. At this scale the class holds one — the floor, since 10 × 0.02 rounds to zero
# and a scale is a dial rather than an off switch.
message_client="2001:db8:$(uuidgen | tr -d - | cut -c1-4):$(uuidgen | tr -d - | cut -c1-4)::1"
message_refusals=0
for attempt in 1 2 3; do
  status="$(curl -s -o /dev/null -w '%{http_code}' -X POST \
    -H "X-Forwarded-For: $message_client" \
    -H "Idempotency-Key: verify-tp-message-$$-$attempt" \
    -H 'Content-Type: application/json' \
    -d '{}' "http://localhost:$VERIFY_PORT/v1/auth/register")"
  [[ "$status" == "429" ]] && message_refusals=$((message_refusals + 1))
done

(( message_refusals >= 1 )) || fail \
  "three registrations from one address produced no refusal. POST /v1/auth/register creates an account and sends an email on an unauthenticated route, and Docs/12 §1 calls an unlimited one the worst gap on the surface"
ok "POST /v1/auth/register is bounded per address, which it was not before SHIP-183b"

# --- The default shape: trust nothing ---------------------------------------------------
#
# A deployment that has configured no proxy must ignore the header outright. This is the shape
# most deployments start in and the one where believing a header is a total bypass rather than a
# misconfiguration — so it is worth a restart to demonstrate rather than infer.

kill -TERM "$SERVER_PID" 2>/dev/null || true
wait "$SERVER_PID" 2>/dev/null || true

SHIPPER_ENV=development \
HTTP_PORT="$VERIFY_PORT" \
LOG_FORMAT=json \
LOG_LEVEL=debug \
DATABASE_URL="$DATABASE_URL" \
REDIS_URL="$REDIS_URL" \
RATE_LIMIT_BURST_SCALE="$proxy_scale" \
  "$WORKDIR/shipper-api" >>"$WORKDIR/server.log" 2>&1 &
SERVER_PID=$!

wait_for_health || true

# Two requests, each claiming a *different* fresh address. With no proxy configured both key on
# 127.0.0.1 and spend one bucket, so the second must be refused: the Message class holds one at
# this scale.
#
# The assertion is deliberately on the second request alone and not on the first. 127.0.0.1's
# bucket is shared with every other section and every other tree on this machine, so it may already
# be empty when this runs — which makes the first request's status unknowable and the second's the
# only thing this section may assert. Either way, a fresh bucket for the second is only reachable
# if the forged header was believed.
for claimed in "198.51.100.77" "203.0.113.88"; do
  untrusted_status="$(curl -s -o /dev/null -w '%{http_code}' -X POST \
    -H "X-Forwarded-For: $claimed" \
    -H "Idempotency-Key: verify-tp-untrusted-$$-${claimed//./-}" \
    -H 'Content-Type: application/json' \
    -d '{}' "http://localhost:$VERIFY_PORT/v1/auth/register")"
done

[[ "$untrusted_status" == "429" ]] || fail \
  "with no trusted proxy configured, a second forged X-Forwarded-For answered $untrusted_status rather than 429 — so the header was believed and any caller can pick their own bucket by writing one"
ok "with no proxy configured a forwarded header is ignored entirely, whatever it claims"
