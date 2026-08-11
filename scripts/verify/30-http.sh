# shellcheck shell=bash
#
# SHIP-13, SHIP-12, SHIP-14, SHIP-15, SHIP-44 — the HTTP surface every domain sits on: the /v1
# group, the error contract, request-ID propagation, idempotent replay, and the scope that keeps
# one caller's replay out of another's.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# The token minting SHIP-44 needs is in the runner rather than here, because SHIP-46's protected
# routes and every authenticated endpoint after it need the same thing. mint_token, b64url and
# $auth_header are harness.

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
# The tokens are minted rather than obtained, because no sign-in endpoint exists yet (SHIP-41).
# mint_token is in the runner: it is signed with the development key from deploy/.env.example,
# which is public and in this repository on purpose — the service refuses it outside development
# — and every authenticated section from SHIP-46 onwards needs the same one.

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
