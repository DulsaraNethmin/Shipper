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

# ---------------------------------------------------------------------------------------
ticket "SHIP-15  a refusal that says *not yet* releases the key rather than storing it"

# The settle rule's other half, and the one that had no check at all until this file grew one.
#
# `internal/httpx/idempotency.go` gives the key back on a 5xx and stores everything else, on the
# reasoning that a settled outcome — including a 4xx the client caused — is the answer, and that
# replaying it is what stops a retry loop turning one rejected bid into ten. That is right about
# every 4xx that describes the request and wrong about the one that does not. **A 429 says *not
# yet*.** Nothing happened, so there is nothing to replay, and the response itself invites the
# retry that storing it would then refuse — without the wait, because a stored response carries a
# status, a content type, a location and a body and no other header, so the replay cannot even say
# how long to wait the second time.
#
# It is checked here rather than in 40-identity.sh because the rule belongs to the middleware
# rather than to sign-in: `POST /v1/auth/login` is only the surface that reaches it first, and
# `POST /v1/admin/sessions` reaches the same line. Sign-in is what the harness can drive past a
# limit end to end, which is why the demonstration is a sign-in and the claim is not.
#
# **Every request in this run arrives from 127.0.0.1**, so SHIP-47's per-address bucket is shared
# with every section below and with every previous run. It is cleared at both ends of this block
# for the reason 40-identity.sh clears it at both ends of SHIP-47's: a later section that got a
# 429 would look like a broken endpoint rather than like this one's leftovers.
# signin_bucket_keys — every key the Credential class leaves behind, in both namespaces.
#
# **Two patterns rather than one, since SHIP-183b.** The account half is still `signin:account:`;
# the address half was renamed to `credential:address:` when it stopped being sign-in's alone —
# refresh and both verification endpoints now spend the same bucket, because Docs/12 §9 puts one
# bucket on a class and never one on a route. A check that kept scanning `rl:v1:signin:*` would
# find the account keys, report a clean namespace and leave the address bucket full for whatever
# ran next.
signin_bucket_keys() {
  redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:signin:*'
  redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:credential:*'
}

for pattern in 'rl:v1:signin:*' 'rl:v1:credential:*'; do
  redis-cli -u "$REDIS_URL" --scan --pattern "$pattern" \
    | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true
done

# idem429_login <key> <email> <password> <label> <name> — one sign-in, keeping the headers.
#
# The headers are the whole point here. `Idempotency-Replayed` is how a check says *which*
# mechanism answered — the limiter or the store — and `Retry-After` is the half a replay drops.
idem429_login() {
  curl -s -X POST -o "$WORKDIR/i429-$5.json" -D "$WORKDIR/i429-$5.headers" -w '%{http_code}' \
    -H "Idempotency-Key: $1" -H 'Content-Type: application/json' \
    -d "{\"email\":\"$2\",\"password\":\"$3\",\"device_label\":\"$4\"}" \
    "http://localhost:$VERIFY_PORT/v1/auth/login"
}

# idem429_header <name> <lower-case header name> — one response header, or nothing.
idem429_header() {
  tr -d '\r' <"$WORKDIR/i429-$1.headers" \
    | awk -F': ' -v want="$2" 'tolower($1) == want { print $2 }' | tail -1
}

# idem429_stored <key> — the idempotency entries Redis holds for a key, one per line.
idem429_stored() { redis-cli -u "$REDIS_URL" --scan --pattern "idem:v1:*:$1"; }

# The address bucket is emptied against accounts that do not exist, so that every *account*
# bucket stays full and the refusal below can only have come from the network-wide limit. That
# matters for more than tidiness: the account limit refills one unit every two minutes and the
# address limit one every twenty seconds, and this check waits out whichever it provoked.
idem429_drained=""
for attempt in $(seq 1 80); do
  status="$(idem429_login "verify-i429-drain-$attempt-$$" "nobody-i429-$attempt-$$@example.com" \
    "not-the-registered-password" "Verify 429 Drain" drain)"
  if [[ "$status" == "429" ]]; then
    idem429_drained="$attempt"
    break
  fi
  [[ "$status" == "400" ]] || { cat "$WORKDIR/i429-drain.json"; fail "drain attempt $attempt returned $status, want 400"; }
done
[[ -n "$idem429_drained" ]] \
  || fail "eighty failed sign-ins from one address were never throttled, so there is no 429 to check"
ok "the per-address sign-in allowance is spent, which is the only 429 this API can be driven to"

# The account this waits on. Registered here rather than reused, so its own per-account bucket is
# untouched and the 429 below is the address limit rather than its own.
idem429_email="idem429-$$@example.com"
idem429_password="correct-horse-battery-staple"
# **The 0411 block, and a width that does not depend on the PID.** This line used to read
# `04$(printf '%08d' …)`, which for a *four-digit* PID renders `04` + `00001949` — byte for byte
# what SHIP-28's raw insert below builds as `+6140000$$`. Two sections then registered the same
# number in one run and the second failed on uq_users_phone, so `make verify` was red for every
# harness whose shell PID happened to fall in 1000..9999 and green for every other. Nothing about
# the failure named a phone number, and it surfaced three sections later.
#
# 0411 is used by no other section, so the collision is impossible rather than unlikely, and the
# six-digit pad keeps the number ten digits at any PID width.
idem429_phone="0411$(printf '%06d' "$(( $$ % 1000000 ))")"
status="$(post_json "verify-i429-register-$$" /v1/auth/register \
  "{\"name\":\"Verify Harness\",\"email\":\"$idem429_email\",\"phone\":\"$idem429_phone\",\"password\":\"$idem429_password\",\"role\":\"customer\"}" \
  "$WORKDIR/i429-register.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/i429-register.json"; fail "could not register the 429 account ($status)"; }
idem429_user="$(json "$WORKDIR/i429-register.json" '["id"]')"

# One key, reused for every attempt below, because that is what a client does: the key identifies
# the action, and a retry of that action carries the key it was issued under.
idem429_key="verify-i429-shared-$$"

status="$(idem429_login "$idem429_key" "$idem429_email" "$idem429_password" "Verify 429 Retry" first)"
[[ "$status" == "429" ]] || { cat "$WORKDIR/i429-first.json"; fail "the sign-in behind an empty bucket returned $status, want 429"; }
[[ "$(json "$WORKDIR/i429-first.json" '["error"]["code"]')" == "rate_limited" ]] \
  || { cat "$WORKDIR/i429-first.json"; fail "expected code=rate_limited"; }
[[ -z "$(idem429_header first idempotency-replayed)" ]] || fail "the first request was marked as a replay"

idem429_wait="$(idem429_header first retry-after)"
[[ "$idem429_wait" =~ ^[0-9]+$ && "$idem429_wait" -gt 0 && "$idem429_wait" -le 30 ]] \
  || fail "Retry-After is '$idem429_wait'. A figure near 120 means the per-account bucket refused
     rather than the per-address one, so the drain above spent the wrong allowance"
ok "a correct password is refused 429 with a wait of ${idem429_wait}s — the limit is checked before the credential"

# **The observation the fix is.** A stored response under this key would be visible here, and
# under the settle rule this file was written against it was: the key was completed with the
# refusal against it, and every retry for the life of the entry got that refusal back.
[[ -z "$(idem429_stored "$idem429_key")" ]] \
  || fail "the 429 was stored under $idem429_key:
$(idem429_stored "$idem429_key")
     A refusal that invites a retry cannot also be the answer that retry gets."
ok "the key is released rather than completed — Redis holds no entry for it"

# The retry a client makes while still throttled has to be refused by the limiter rather than by
# the store, and it has to carry a wait of its own. **This is the half that proves the header loss
# is closed rather than argued**: idempotency.Response persists a status, a content type, a
# location and a body, so a replayed 429 arrives with no Retry-After at all.
status="$(idem429_login "$idem429_key" "$idem429_email" "$idem429_password" "Verify 429 Retry" second)"
[[ "$status" == "429" ]] || { cat "$WORKDIR/i429-second.json"; fail "the retry returned $status, want a fresh 429"; }
[[ -z "$(idem429_header second idempotency-replayed)" ]] \
  || fail "the retry was answered from the store, so the client is holding a refusal it can never get past"

idem429_wait="$(idem429_header second retry-after)"
[[ "$idem429_wait" =~ ^[0-9]+$ && "$idem429_wait" -gt 0 && "$idem429_wait" -le 30 ]] \
  || fail "the retried refusal carries Retry-After '$idem429_wait' — told to wait, and not told how long"
ok "the retry is refused by the limiter again, carrying a live Retry-After of ${idem429_wait}s rather than none"

# Waited out rather than guessed, and read off the response rather than transcribed: a hard-coded
# figure asserts a limiter's constants from the outside and stops being true the moment they move.
# One second of slack for the round trip, on top of a figure the platform already rounded up.
sleep "$((idem429_wait + 1))"

status="$(idem429_login "$idem429_key" "$idem429_email" "$idem429_password" "Verify 429 Retry" third)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/i429-third.json"; fail "the sign-in after the stated wait returned $status, want 200"; }
[[ -z "$(idem429_header third idempotency-replayed)" ]] \
  || fail "the sign-in after the wait was answered from the store rather than run"
[[ -n "$(json "$WORKDIR/i429-third.json" '["access_token"]')" ]] \
  || { cat "$WORKDIR/i429-third.json"; fail "the sign-in returned no access token"; }

# The row, not the response. A replay answers without the handler running, so a session that
# exists is what separates an execution from an execution answered twice.
idem429_sessions="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions where user_id = '$idem429_user' and device_label = 'Verify 429 Retry';")"
[[ "$idem429_sessions" == "1" ]] \
  || fail "waiting exactly as long as Retry-After said produced $idem429_sessions sessions, want 1"
ok "a client that waits the stated time and retries under the same key is signed in — the wait means something"

# And the successful outcome settles, so the key is held again. The release is for the refusal,
# not for the endpoint: without this the check above would be satisfied by a change that disabled
# replay for sign-in rather than one that released a deferral.
status="$(idem429_login "$idem429_key" "$idem429_email" "$idem429_password" "Verify 429 Retry" fourth)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/i429-fourth.json"; fail "the retry after the successful sign-in returned $status"; }
[[ "$(idem429_header fourth idempotency-replayed)" == "true" ]] \
  || { cat "$WORKDIR/i429-fourth.headers"; fail "the retry after a successful sign-in was not replayed"; }
diff -q "$WORKDIR/i429-third.json" "$WORKDIR/i429-fourth.json" >/dev/null \
  || fail "the replayed pair differs from the one the sign-in issued"
idem429_sessions="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from device_sessions where user_id = '$idem429_user' and device_label = 'Verify 429 Retry';")"
[[ "$idem429_sessions" == "1" ]] || fail "the replay created a second device session ($idem429_sessions)"
ok "and the sign-in it did issue is replayed byte for byte, still one device — the settle rule still settles"

# --- the 4xx that must still replay, which is what separates a fix from a regression -----------
#
# A change that released the key for *every* 4xx would pass every check above and would be wrong:
# it would turn one rejected bid into ten. The contrast is run against the same endpoint, under
# the same middleware, with the buckets refilled so that both attempts would be admitted and the
# only thing that can stop the second is the store.
for pattern in 'rl:v1:signin:*' 'rl:v1:credential:*'; do
  redis-cli -u "$REDIS_URL" --scan --pattern "$pattern" \
    | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true
done

idem429_wrong_key="verify-i429-wrong-$$"
status="$(idem429_login "$idem429_wrong_key" "$idem429_email" "not-the-registered-password" "Verify 429 Wrong" wrong1)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/i429-wrong1.json"; fail "a wrong password returned $status, want 400"; }
[[ -n "$(idem429_stored "$idem429_wrong_key")" ]] \
  || fail "the 400 was not stored, so a retry loop would re-run a refusal the client cannot fix"

# The tokens left in the address bucket are the side effect that says whether the handler ran: a
# refused credential charges one unit, and a reply from the store charges nothing. Compared with
# half a unit of slack because the bucket refills continuously.
#
# **The key is read out of Redis rather than spelled here.** SHIP-47 keys the address bucket on
# the address the transport reports, and `localhost` resolves to `127.0.0.1` or to `::1` depending
# on the machine — so a literal key name is a check that silently reads nothing and then compares
# two empty strings. It did exactly that on the first run of this section.
idem429_bucket="$(redis-cli -u "$REDIS_URL" --scan --pattern 'rl:v1:credential:address:*' | head -1)"
[[ -n "$idem429_bucket" ]] \
  || fail "the refused credential charged no per-address bucket, so nothing here can tell a replay from a re-run"
idem429_tokens_before="$(redis-cli -u "$REDIS_URL" hget "$idem429_bucket" tokens)"
[[ -n "$idem429_tokens_before" ]] || fail "$idem429_bucket holds no token count"

status="$(idem429_login "$idem429_wrong_key" "$idem429_email" "not-the-registered-password" "Verify 429 Wrong" wrong2)"
[[ "$status" == "400" ]] || { cat "$WORKDIR/i429-wrong2.json"; fail "the retried refusal returned $status, want the stored 400"; }
[[ "$(idem429_header wrong2 idempotency-replayed)" == "true" ]] \
  || { cat "$WORKDIR/i429-wrong2.headers"; fail "a 400 under a reused key was re-run rather than replayed"; }
diff -q "$WORKDIR/i429-wrong1.json" "$WORKDIR/i429-wrong2.json" >/dev/null \
  || fail "the replayed refusal differs from the original"

idem429_tokens_after="$(redis-cli -u "$REDIS_URL" hget "$idem429_bucket" tokens)"
[[ -n "$idem429_tokens_after" ]] || fail "$idem429_bucket holds no token count after the retry"
awk -v before="$idem429_tokens_before" -v after="$idem429_tokens_after" \
  'BEGIN { exit !(after > before - 0.5) }' \
  || fail "$idem429_bucket went from $idem429_tokens_before to $idem429_tokens_after, so the replayed 400 ran the handler again"
ok "a 400 under the same key is still answered from the store, byte for byte, without the handler running"

# The bucket is shared with every section below. Cleared where it was spent, and asserted rather
# than assumed, because a later track's 429 would read as its own endpoint being broken.
for pattern in 'rl:v1:signin:*' 'rl:v1:credential:*'; do
  redis-cli -u "$REDIS_URL" --scan --pattern "$pattern" \
    | xargs -r redis-cli -u "$REDIS_URL" del >/dev/null 2>&1 || true
done
[[ "$(signin_bucket_keys | wc -l | tr -d ' ')" == "0" ]] \
  || fail "this section left sign-in buckets behind, which a later track would be throttled by"
status="$(idem429_login "verify-i429-cleared-$$" "$idem429_email" "$idem429_password" "Verify 429 Cleared" cleared)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/i429-cleared.json"; fail "sign-in is still refused after the buckets were cleared ($status)"; }
ok "the sign-in buckets are empty at the end of this section, and a sign-in still succeeds"
