# shellcheck shell=bash
#
# M4 delivery — putting a driver on an awarded job.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 70–79 is the delivery range. This file is delivery's to append to, and no other track's to edit —
# which is the whole reason the script was split (Docs/11 §9, SHIP-15e).
#
# # This section is the only place SHIP-106's real wiring is exercised
#
# The Go tests in internal/delivery drive the domain against a real database, but they supply their
# own copies of the two ports — a test file may import `jobs`, and cmd/api's adapters live in
# package main where no test has a database to reach. So the composition root's half of SHIP-106
# (the accepted-bid lookup and the translation of the guard's refusals) is demonstrated here,
# against the built binary, or nowhere.
#
# # The fixtures reach a state no endpoint can reach yet
#
# SHIP-63 publishes and SHIP-92 awards. Neither exists, so the job is moved with the protocol
# 000402's trigger demands — a job_status_history row written in the same transaction, named by a
# transaction-local setting — and the award is one accepted bid, which is what
# uq_bids_one_accepted_per_job makes singular. A bare `UPDATE jobs SET status` is refused, so even
# the fixture cannot bypass the guard.
#
# The tokens are minted by the runner and every one of them claims `role: customer`. That is the
# right thing to exercise: this endpoint decides from the accepted bid, not from a claim in a token.
# It also means nothing here signs in, so SHIP-47's per-address bucket is untouched by this file.

# --- the accounts and the awarded job these checks run against ---------------------------------

status="$(post_json "verify-del-cust-$$" /v1/auth/register \
  "{\"email\":\"delivery-customer-$$@example.com\",\"phone\":\"04170$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/delivery-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/delivery-customer.json"; fail "could not register the delivery customer: $status"; }
delivery_customer_id="$(json "$WORKDIR/delivery-customer.json" '["id"]')"
delivery_customer_token="$(mint_token "$delivery_customer_id")"

status="$(post_json "verify-del-prov-$$" /v1/auth/register \
  "{\"email\":\"delivery-provider-$$@example.com\",\"phone\":\"04171$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/delivery-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/delivery-provider.json"; fail "could not register the delivery provider: $status"; }
delivery_provider_id="$(json "$WORKDIR/delivery-provider.json" '["id"]')"
delivery_provider_token="$(mint_token "$delivery_provider_id")"

status="$(post_json "verify-del-other-$$" /v1/auth/register \
  "{\"email\":\"delivery-other-$$@example.com\",\"phone\":\"04172$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/delivery-other.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/delivery-other.json"; fail "could not register the second provider: $status"; }
delivery_other_token="$(mint_token "$(json "$WORKDIR/delivery-other.json" '["id"]')")"

# delivery_request <token> <key> <path> <body> <outfile> — one authenticated state-changing request.
#
# Every writing route here is protected and state-changing, so each call needs a credential *and* an
# idempotency key. Passing both as arguments means a check that forgets one gets a wrong number of
# arguments rather than a 400 or a 401 that has nothing to do with what it was testing.
delivery_request() {
  curl -s -X POST -o "$5" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT$3"
}

# delivery_draft <name> — a fresh draft owned by the delivery customer, answering with its id.
delivery_draft() {
  local out="$WORKDIR/delivery-$1.json"
  local created
  created="$(curl -s -X POST -o "$out" -w '%{http_code}' \
    -H "$auth_header: Bearer $delivery_customer_token" -H "Idempotency-Key: verify-del-$1-$$" \
    -H 'Content-Type: application/json' -d '{}' \
    "http://localhost:$VERIFY_PORT/v1/jobs")"
  [[ "$created" == "201" ]] || { cat "$out"; fail "could not create a draft for $1"; }
  json "$out" '["id"]'
}

# delivery_move <job-id> <from> <to> — one guarded transition, exactly as 000402 requires.
#
# The SQL is single-quoted on purpose: `$$` in a double-quoted shell string is the process id, which
# is how a dollar-quoted PL/pgSQL block would silently become nonsense. Values come in as psql
# variables instead.
delivery_move() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 \
    -v job="$1" -v from_status="$2" -v to_status="$3" -v actor="$delivery_customer_id" \
    >/dev/null <<'SQL'
BEGIN;
INSERT INTO job_status_history
    (id, job_id, from_status, to_status, actor_type, actor_id, actor_recorded_at)
VALUES (gen_random_uuid(), :'job', :'from_status', :'to_status', 'customer', :'actor', now());
SELECT set_config('shipper.job_status_transition',
                  (SELECT id::text FROM job_status_history
                    WHERE job_id = :'job' AND to_status = :'to_status'), true);
UPDATE jobs SET status = :'to_status' WHERE id = :'job';
COMMIT;
SQL
}

# delivery_award <job-id> <provider-id> — the accepted bid SHIP-92 will write.
delivery_award() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -v job="$1" -v provider="$2" >/dev/null <<'SQL'
INSERT INTO bids (id, job_id, provider_id, status, amount)
VALUES (gen_random_uuid(), :'job', :'provider', 'Accepted', 450.00);
SQL
}

# delivery_awarded_job <name> — a job published, awarded to the delivery provider, and ready for a
# driver.
delivery_awarded_job() {
  local job
  job="$(delivery_draft "$1")"
  delivery_move "$job" Draft Open
  delivery_move "$job" Open Awarded
  delivery_award "$job" "$delivery_provider_id"
  printf '%s' "$job"
}

# ---------------------------------------------------------------------------------------
ticket "SHIP-106  POST /v1/jobs/{id}/driver assigns a driver and moves the job to Driver assigned"

delivery_job="$(delivery_awarded_job assign)"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$delivery_job';")" == "Awarded" ]] \
  || fail "the fixture job is not Awarded, so nothing below is testing what it claims"
ok "a job reaches Awarded only through the guard, and one accepted bid names the provider"

status="$(curl -s -X POST -o "$WORKDIR/delivery-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-del-anon-$$" -H 'Content-Type: application/json' \
  -d '{"driver_name":"Sam Patel","driver_mobile":"+61412345678"}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$delivery_job/driver")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/delivery-anon.json"; fail "an unauthenticated assignment returned $status, want 401"; }
ok "it cannot be reached without a credential"

status="$(curl -s -X POST -o "$WORKDIR/delivery-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $delivery_provider_token" -H 'Content-Type: application/json' \
  -d '{"driver_name":"Sam Patel","driver_mobile":"+61412345678"}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$delivery_job/driver")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/delivery-nokey.json"; fail "a request with no Idempotency-Key returned $status, want 400"; }
ok "and not without an Idempotency-Key — a retried phone must not put two drivers on one job"

status="$(delivery_request "$delivery_other_token" "verify-del-stranger-$$" \
  "/v1/jobs/$delivery_job/driver" '{"driver_name":"Sam Patel","driver_mobile":"+61412345678"}' \
  "$WORKDIR/delivery-stranger.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/delivery-stranger.json"; fail "another provider assigned a driver: $status"; }
[[ "$(json "$WORKDIR/delivery-stranger.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/delivery-stranger.json"; fail "expected code=not_found"; }
ok "a provider who did not win the job gets the answer a missing job gets — 404, and no confirmation it exists"

# The number is written the way a person writes it. Normalisation is not cosmetic here: it is what
# SHIP-107's link will be sent to.
status="$(delivery_request "$delivery_provider_token" "verify-del-assign-$$" \
  "/v1/jobs/$delivery_job/driver" '{"driver_name":"  Sam   Patel ","driver_mobile":"0412 345 678"}' \
  "$WORKDIR/delivery-assign.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/delivery-assign.json"; fail "assigning a driver returned $status, want 201"; }
delivery_assignment_id="$(json "$WORKDIR/delivery-assign.json" '["id"]')"
[[ "$delivery_assignment_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the response carries no assignment id"
[[ "$(json "$WORKDIR/delivery-assign.json" '["driver_name"]')" == "Sam Patel" ]] \
  || fail "the driver name was not normalised: $(json "$WORKDIR/delivery-assign.json" '["driver_name"]')"
[[ "$(json "$WORKDIR/delivery-assign.json" '["driver_mobile"]')" == "+61412345678" ]] \
  || fail "the mobile was not stored in E.164: $(json "$WORKDIR/delivery-assign.json" '["driver_mobile"]')"
ok "the awarded provider nominates a driver, and the number is stored in E.164"

# The rows, not the answer the endpoint gave about itself.
delivery_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select j.status || ' ' || d.driver_name || ' ' || d.driver_mobile
     from jobs j join driver_assignments d on d.job_id = j.id
    where j.id = '$delivery_job' and d.unassigned_at is null;")"
[[ "$delivery_row" == "Driver assigned Sam Patel +61412345678" ]] \
  || fail "the stored state is '$delivery_row', want 'Driver assigned Sam Patel +61412345678'"
ok "the job is Driver assigned and the driver is on it — one transaction, both facts"

delivery_history="$("$PSQL" "$DATABASE_URL" -tAc \
  "select h.from_status || '->' || h.to_status || ' ' || h.actor_type || ' ' || (h.actor_id = '$delivery_provider_id')
     from job_status_history h
    where h.job_id = '$delivery_job' and h.to_status = 'Driver assigned';")"
[[ "$delivery_history" == "Awarded->Driver assigned provider true" ]] \
  || fail "the recorded transition is '$delivery_history', want 'Awarded->Driver assigned provider true'"
ok "the move went through the guard and is recorded against the provider who made it"

# A fresh key for the same intent — the phone that lost its connection and was restarted. The
# idempotency middleware cannot absorb this one; the domain does.
status="$(delivery_request "$delivery_provider_token" "verify-del-again-$$" \
  "/v1/jobs/$delivery_job/driver" '{"driver_name":"Sam Patel","driver_mobile":"+61 412 345 678"}' \
  "$WORKDIR/delivery-again.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/delivery-again.json"; fail "nominating the same driver again returned $status, want 200"; }
[[ "$(json "$WORKDIR/delivery-again.json" '["id"]')" == "$delivery_assignment_id" ]] \
  || fail "the repeat answered with a different assignment"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from driver_assignments where job_id = '$delivery_job';")" == "1" ]] \
  || fail "the repeat wrote a second assignment row"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$delivery_job' and to_status = 'Driver assigned';")" == "1" ]] \
  || fail "the repeat wrote a second transition"
ok "the same driver nominated twice with a fresh key is absorbed — one row, one transition"

delivery_again_token="$(json "$WORKDIR/delivery-again.json" '["driver_token"]')"
[[ -n "$delivery_again_token" ]] || fail "the absorbed repeat answered with no driver_token"
ok "and it still answers with a link — a provider who lost the first response has no other way to get one"

status="$(delivery_request "$delivery_provider_token" "verify-del-replace-$$" \
  "/v1/jobs/$delivery_job/driver" '{"driver_name":"Ravi Chandra","driver_mobile":"+61412000999"}' \
  "$WORKDIR/delivery-replace.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/delivery-replace.json"; fail "a second driver was accepted: $status"; }
[[ "$(json "$WORKDIR/delivery-replace.json" '["error"]["code"]')" == "delivery_driver_already_assigned" ]] \
  || { cat "$WORKDIR/delivery-replace.json"; fail "expected code=delivery_driver_already_assigned"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select driver_name from driver_assignments where job_id = '$delivery_job' and unassigned_at is null;")" \
  == "Sam Patel" ]] || fail "the refused replacement changed the driver anyway"
ok "a different driver is refused with a code the app can act on, and the job keeps the driver it has"

# ---------------------------------------------------------------------------------------
ticket "SHIP-106  a provider can self-assign, and the mobile comes from their account"

delivery_self_job="$(delivery_awarded_job self)"

status="$(delivery_request "$delivery_provider_token" "verify-del-self-both-$$" \
  "/v1/jobs/$delivery_self_job/driver" '{"driver_name":"Ravi Chandra","driver_mobile":"+61412345678","self":true}' \
  "$WORKDIR/delivery-self-both.json")"
[[ "$status" == "422" ]] || { cat "$WORKDIR/delivery-self-both.json"; fail "self with a mobile returned $status, want 422"; }
[[ "$(json "$WORKDIR/delivery-self-both.json" '["error"]["details"][0]["field"]')" == "driver_mobile" ]] \
  || { cat "$WORKDIR/delivery-self-both.json"; fail "the refusal does not name driver_mobile"; }
ok "sending both a mobile and self is refused rather than resolved either way"

status="$(delivery_request "$delivery_provider_token" "verify-del-self-$$" \
  "/v1/jobs/$delivery_self_job/driver" '{"driver_name":"Ravi Chandra","self":true}' \
  "$WORKDIR/delivery-self.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/delivery-self.json"; fail "self-assignment returned $status, want 201"; }

delivery_account_phone="$("$PSQL" "$DATABASE_URL" -tAc \
  "select phone from users where id = '$delivery_provider_id';")"
[[ "$(json "$WORKDIR/delivery-self.json" '["driver_mobile"]')" == "$delivery_account_phone" ]] \
  || fail "the self-assignment used $(json "$WORKDIR/delivery-self.json" '["driver_mobile"]'), not the account's $delivery_account_phone"
ok "the provider drives it themselves, on the number the platform verified rather than one they asserted"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$delivery_self_job';")" \
  == "Driver assigned" ]] || fail "a self-assigned job did not move to Driver assigned"
ok "and the job moves exactly as a nomination moves it — Docs/02 §1 has one status for both"

# ---------------------------------------------------------------------------------------
ticket "SHIP-106  a job that cannot take a driver says so, and nothing is written"

# Awarded is the only status Docs/02 §2 has a `→ Driver assigned` row from. This job has an accepted
# bid and is still a Draft, which is the state a client with a stale screen would send.
delivery_draft_job="$(delivery_draft not-awarded)"
delivery_award "$delivery_draft_job" "$delivery_provider_id"

status="$(delivery_request "$delivery_provider_token" "verify-del-draft-$$" \
  "/v1/jobs/$delivery_draft_job/driver" '{"driver_name":"Sam Patel","driver_mobile":"+61412345678"}' \
  "$WORKDIR/delivery-draft.json")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/delivery-draft.json"; fail "a Draft took a driver: $status"; }
[[ "$(json "$WORKDIR/delivery-draft.json" '["error"]["code"]')" == "delivery_job_not_assignable" ]] \
  || { cat "$WORKDIR/delivery-draft.json"; fail "expected code=delivery_job_not_assignable"; }
ok "the transition guard refuses the move, and the endpoint says which of the two conflicts it was"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from driver_assignments where job_id = '$delivery_draft_job';")" == "0" ]] \
  || fail "an assignment row survived a refused transition"
ok "and the assignment rolled back with it — a driver on a job that never moved cannot happen"

# ---------------------------------------------------------------------------------------
ticket "SHIP-107  a signed, time-limited, single-job driver token is generated on assignment"

# # Why this is checked here and not only in Go
#
# internal/delivery's tests build the issuer from constants of their own. What they cannot show is
# that the *service as configured* signs with the driver keyset rather than identity's, and that a
# real driver token presented as a mobile credential to a running instance is refused. Both are
# properties of the wiring, and the wiring is what this file exists to exercise.
#
# Nothing in *this* section presents a driver token to a route that accepts one. That is SHIP-108's
# section further down, and the split is deliberate: these checks are about what the token *is*, and
# those are about what happens when it meets a request.

# jwt_segment <token> <0|1> <outfile> — decode a token's header (0) or payload (1) into a JSON file.
#
# python3 rather than `base64 -d`: a JWT segment is base64url with the padding stripped, and neither
# macOS's nor coreutils' base64 accepts that. Nothing here verifies anything — the signature is the
# check below, and keeping the two apart is what lets this one read claims out of a token it has not
# yet trusted, exactly as an attacker would.
jwt_segment() {
  python3 - "$1" "$2" "$3" <<'PY'
import base64, json, sys

token, index, out = sys.argv[1], int(sys.argv[2]), sys.argv[3]
segment = token.split('.')[index]
segment += '=' * (-len(segment) % 4)
json.dump(json.loads(base64.urlsafe_b64decode(segment)), open(out, 'w'))
PY
}

# jwt_claim_names <file> — the claim names a decoded token carries, sorted and comma-separated.
jwt_claim_names() {
  python3 -c 'import json,sys; print(",".join(sorted(json.load(open(sys.argv[1])))))' "$1"
}

# hs256 <signing-input> <key> — the HS256 signature in the form a token carries it.
hs256() { printf '%s' "$1" | openssl dgst -sha256 -hmac "$2" -binary | b64url; }

# The two development keys from deploy/.env.example. Both are public and in this repository; the
# service refuses either outside development. They are written out here rather than derived, because
# what these checks are about is that the *right one of the two* signed the token.
delivery_driver_dev_key="shipper-local-development-driver-token-key-not-a-secret"
delivery_mobile_dev_key="shipper-local-development-signing-key-not-a-secret"

delivery_driver_token="$(json "$WORKDIR/delivery-assign.json" '["driver_token"]')"
delivery_token_expiry="$(json "$WORKDIR/delivery-assign.json" '["driver_token_expires_at"]')"

[[ "$delivery_driver_token" =~ ^[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+$ ]] \
  || fail "the assignment carried no signed driver token: '$delivery_driver_token'"
[[ "$delivery_token_expiry" =~ Z$ ]] || fail "driver_token_expires_at is '$delivery_token_expiry', want UTC"
ok "assigning a driver returns a signed token and the instant it stops working — generated on assignment, and nowhere else"

jwt_segment "$delivery_driver_token" 0 "$WORKDIR/delivery-token-header.json"
jwt_segment "$delivery_driver_token" 1 "$WORKDIR/delivery-token-claims.json"

[[ "$(json "$WORKDIR/delivery-token-header.json" '["alg"]')" == "HS256" ]] \
  || fail "the token's alg is $(json "$WORKDIR/delivery-token-header.json" '["alg"]'), want HS256"
[[ "$(json "$WORKDIR/delivery-token-header.json" '["kid"]')" == "driver-dev" ]] \
  || fail "the token names key $(json "$WORKDIR/delivery-token-header.json" '["kid"]'), want the driver keyset's driver-dev"
ok "it is signed HS256 under the driver keyset's own key identifier, not identity's"

# The claim set, held closed. A search for `sub` would catch `sub` and miss `user_id`, which is the
# same argument SHIP-83's budget test makes: assert the whole set, not the absence of one name.
delivery_claim_names="$(jwt_claim_names "$WORKDIR/delivery-token-claims.json")"
[[ "$delivery_claim_names" == "assignment_id,aud,exp,iat,iss,job_id,jti" ]] \
  || fail "the claim set is '$delivery_claim_names', want exactly assignment_id,aud,exp,iat,iss,job_id,jti"
ok "seven claims and no eighth — no sub, no role and no sid, so there is nothing to build a session from"

[[ "$(json "$WORKDIR/delivery-token-claims.json" '["aud"]')" == "shipper-driver" ]] \
  || fail "aud is $(json "$WORKDIR/delivery-token-claims.json" '["aud"]'), want shipper-driver"
[[ "$(json "$WORKDIR/delivery-token-claims.json" '["job_id"]')" == "$delivery_job" ]] \
  || fail "the token grants $(json "$WORKDIR/delivery-token-claims.json" '["job_id"]'), want $delivery_job"
[[ "$(json "$WORKDIR/delivery-token-claims.json" '["assignment_id"]')" == "$delivery_assignment_id" ]] \
  || fail "the token names assignment $(json "$WORKDIR/delivery-token-claims.json" '["assignment_id"]'), want $delivery_assignment_id"
ok "it names one job and the assignment it was minted for — the driver's only identity, since they have no account"

# Seven days, read off the token rather than off the configuration that produced it.
delivery_token_window="$(json "$WORKDIR/delivery-token-claims.json" '["exp"]-json.load(open(sys.argv[1]))["iat"]')"
[[ "$delivery_token_window" == "604800" ]] \
  || fail "the token is valid for ${delivery_token_window}s, want 604800 (seven days)"
ok "time-limited to exactly seven days — long enough for a long-haul delivery, and there is no refresh behind it"

# The signature, against each development key in turn. This is the check that makes "separate
# signing key material" (Docs/10 §5) a fact about the running service rather than about a struct.
delivery_signing_input="${delivery_driver_token%.*}"
[[ "${delivery_driver_token##*.}" == "$(hs256 "$delivery_signing_input" "$delivery_driver_dev_key")" ]] \
  || fail "the token does not verify under the driver signing key"
ok "the signature verifies under the driver token's own key"

[[ "${delivery_driver_token##*.}" != "$(hs256 "$delivery_signing_input" "$delivery_mobile_dev_key")" ]] \
  || fail "the driver token was signed with identity's access token key — the two systems share key material"
ok "and not under the mobile session's — two keysets, which is what stops either verifier ever accepting the other's tokens"

# The separation, over HTTP. `GET /v1/jobs` declares RequireUser, so this is a real driver token
# presented to a real authenticated endpoint on the running binary. The other direction — a mobile
# token presented to a driver route — is in SHIP-108's section, which is the ticket that created a
# route able to refuse one.
status="$(curl -s -o "$WORKDIR/delivery-token-as-session.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $delivery_driver_token" \
  "http://localhost:$VERIFY_PORT/v1/jobs")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/delivery-token-as-session.json"; fail "a driver token was accepted as a mobile session: $status"; }
ok "presented as a mobile credential it is refused 401 — the driver token cannot be exchanged for a session"

# --- single-job, demonstrated across two assignments -------------------------------------------

delivery_second_job="$(delivery_awarded_job second)"
status="$(delivery_request "$delivery_provider_token" "verify-del-second-$$" \
  "/v1/jobs/$delivery_second_job/driver" '{"driver_name":"Ravi Chandra","driver_mobile":"+61412000999"}' \
  "$WORKDIR/delivery-second.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/delivery-second.json"; fail "assigning the second job returned $status, want 201"; }

delivery_second_token="$(json "$WORKDIR/delivery-second.json" '["driver_token"]')"
[[ "$delivery_second_token" != "$delivery_driver_token" ]] || fail "two jobs were assigned and both got the same token"

jwt_segment "$delivery_second_token" 1 "$WORKDIR/delivery-second-claims.json"
[[ "$(json "$WORKDIR/delivery-second-claims.json" '["job_id"]')" == "$delivery_second_job" ]] \
  || fail "the second token grants $(json "$WORKDIR/delivery-second-claims.json" '["job_id"]'), want $delivery_second_job"
[[ "$(json "$WORKDIR/delivery-second-claims.json" '["job_id"]')" != "$delivery_job" ]] \
  || fail "the second job's token grants the first job, which is not a single-job token"
ok "a second assignment gets a token for its own job and no other — one token, one job"

# A repointed token. The job identifier lives inside the signature, so widening it means re-signing
# it, and re-signing it means holding the key. This is what "cannot be widened" actually rests on.
delivery_forged="$(python3 - "$delivery_driver_token" "$delivery_second_job" <<'PY'
import base64, json, sys


def decode(segment):
    return base64.urlsafe_b64decode(segment + '=' * (-len(segment) % 4))


def encode(raw):
    return base64.urlsafe_b64encode(raw).rstrip(b'=').decode()


header, payload, signature = sys.argv[1].split('.')
claims = json.loads(decode(payload))
claims['job_id'] = sys.argv[2]
print('.'.join([header, encode(json.dumps(claims, separators=(',', ':')).encode()), signature]))
PY
)"
[[ "${delivery_forged##*.}" == "${delivery_driver_token##*.}" ]] \
  || fail "the forgery changed the signature, so it is not testing what it claims"
[[ "${delivery_forged%.*}" != "${delivery_signing_input}" ]] \
  || fail "the forgery did not change the payload, so it is not testing what it claims"
[[ "${delivery_forged##*.}" != "$(hs256 "${delivery_forged%.*}" "$delivery_driver_dev_key")" ]] \
  || fail "a token repointed at another job still carries a valid signature"
ok "rewriting the job a token names breaks its signature — the grant is inside what was signed"

# ---------------------------------------------------------------------------------------
ticket "SHIP-111  POST /v1/jobs/{id}/milestones records a milestone once per idempotency key"

# # This section is where the two idempotency mechanisms are told apart
#
# Everything below runs against the built binary with SHIP-15's middleware in front of it, which
# is the only place the difference between "Redis replayed the response" and "the database refused
# the row" is observable. The middleware sets `Idempotency-Replayed: true` when it answers from
# its own store; the checks read that header, and one of them deletes the Redis entry first so
# that the request reaches the handler exactly as it would after a TTL expiry — a phone out of
# signal for a day outlives any TTL worth setting, and that is the case the ticket exists for.
#
# Nothing here reads the outbox or Kafka, so no fence is needed. The transitions below do write
# outbox rows, and a later section running cmd/worker will publish them, which is correct.

milestone_job="$(delivery_awarded_job milestones)"
milestone_key="verify-ms-$$"

# milestone_request <token> <key> <body> <name> — one recording, keeping the response headers.
#
# The headers are the point: `Idempotency-Replayed` is how a check says *which* mechanism
# answered, and asserting on the status alone could not tell the two apart.
milestone_request() {
  curl -s -X POST -o "$WORKDIR/ms-$4.json" -D "$WORKDIR/ms-$4.headers" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$3" \
    "http://localhost:$VERIFY_PORT/v1/jobs/$milestone_job/milestones"
}

# replayed_from_redis <name> — whether the middleware answered that request from its store.
replayed_from_redis() {
  grep -qi '^idempotency-replayed: true' "$WORKDIR/ms-$1.headers"
}

# forget_the_cached_response <key> — delete the middleware's entry, as a TTL expiry would.
#
# The key is namespaced by the authenticated subject (SHIP-44), which is what stops one client
# reading another's stored response.
forget_the_cached_response() {
  redis-cli -u "$REDIS_URL" del "idem:v1:user:$delivery_provider_id:$1" >/dev/null
}

status="$(curl -s -X POST -o "$WORKDIR/ms-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: $milestone_key-anon" -H 'Content-Type: application/json' \
  -d '{"milestone":"en_route_to_pickup"}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$milestone_job/milestones")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/ms-anon.json"; fail "an unauthenticated recording returned $status, want 401"; }
ok "it cannot be reached without a credential"

status="$(curl -s -X POST -o "$WORKDIR/ms-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $delivery_provider_token" -H 'Content-Type: application/json' \
  -d '{"milestone":"en_route_to_pickup"}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$milestone_job/milestones")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/ms-nokey.json"; fail "a recording with no Idempotency-Key returned $status, want 400"; }
ok "and not without an Idempotency-Key — the key is what the row is recorded under, not just how the retry is absorbed"

# --- who may record: Docs/02 §3 names three, and only one of them can present a credential ---

status="$(milestone_request "$delivery_other_token" "$milestone_key-stranger" \
  '{"milestone":"en_route_to_pickup"}' stranger)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/ms-stranger.json"; fail "another provider recorded a milestone: $status"; }
ok "a provider who did not win the job gets the answer a missing job gets"

status="$(milestone_request "$delivery_customer_token" "$milestone_key-customer" \
  '{"milestone":"en_route_to_pickup"}' customer)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/ms-customer.json"; fail "the job's own customer recorded a milestone: $status"; }
[[ "$(json "$WORKDIR/ms-customer.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/ms-customer.json"; fail "expected code=not_found"; }
ok "and so does the customer who owns the job — Docs/02 §3 permits the provider, their driver and an administrator, and a customer is none of them"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$milestone_job';")" == "0" ]] \
  || fail "a caller with no right to record anything wrote a row"
ok "neither refusal left a row behind"

# --- the recording itself, with the actor's clock ninety minutes behind the platform's ---

milestone_recorded_at="$("$PSQL" "$DATABASE_URL" -tAc \
  "select to_char((now() at time zone 'utc') - interval '90 minutes', 'YYYY-MM-DD\"T\"HH24:MI:SS') || 'Z';")"

status="$(milestone_request "$delivery_provider_token" "$milestone_key" \
  "{\"milestone\":\"en_route_to_pickup\",\"recorded_at\":\"$milestone_recorded_at\",\"reason\":\"  gate   locked \"}" first)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/ms-first.json"; fail "recording a milestone returned $status, want 201"; }
milestone_id="$(json "$WORKDIR/ms-first.json" '["id"]')"
[[ "$milestone_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the response carries no milestone id"
[[ "$(json "$WORKDIR/ms-first.json" '["milestone"]')" == "en_route_to_pickup" ]] \
  || fail "the milestone came back as $(json "$WORKDIR/ms-first.json" '["milestone"]'), want the lower snake case wire form"
[[ "$(json "$WORKDIR/ms-first.json" '["recorded_by"]')" == "provider" ]] \
  || fail "recorded_by is $(json "$WORKDIR/ms-first.json" '["recorded_by"]'), want provider"
[[ "$(json "$WORKDIR/ms-first.json" '["reason"]')" == "gate locked" ]] \
  || fail "the reason was not collapsed: $(json "$WORKDIR/ms-first.json" '["reason"]')"
ok "the awarded provider records a milestone, and it comes back in the wire vocabulary"

# The two clocks, read from the columns rather than from the response. The actor's is what was
# sent, uncorrected; the platform's is the row's arrival.
milestone_clocks="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (actor_recorded_at = '$milestone_recorded_at'::timestamptz) || ' ' || (server_recorded_at > actor_recorded_at)
     from milestones where id = '$milestone_id';")"
[[ "$milestone_clocks" == "true true" ]] \
  || fail "the two clocks are '$milestone_clocks', want 'true true' — the actor's time was corrected or the two were collapsed"
ok "the actor's clock is stored exactly as sent and the platform's is ninety minutes later — two columns, never one (Docs/02 §3.1)"

milestone_history="$("$PSQL" "$DATABASE_URL" -tAc \
  "select h.from_status || '->' || h.to_status || ' ' || (h.actor_recorded_at = '$milestone_recorded_at'::timestamptz)
     from job_status_history h
    where h.job_id = '$milestone_job' and h.to_status = 'En route to pickup';")"
[[ "$milestone_history" == "Awarded->En route to pickup true" ]] \
  || fail "the recorded transition is '$milestone_history', want 'Awarded->En route to pickup true'"
ok "the job moved through the guard, and the transition carries the same actor claim as the milestone"

# --- the retry, twice, through each mechanism in turn ---

status="$(milestone_request "$delivery_provider_token" "$milestone_key" \
  "{\"milestone\":\"en_route_to_pickup\",\"recorded_at\":\"$milestone_recorded_at\",\"reason\":\"  gate   locked \"}" replay)"
# **201, not 200, and the difference is the whole point of this pair of checks.** The middleware
# replays the stored response *verbatim*, status included, so a retry it absorbs is
# indistinguishable from the original — that is what makes it cheap. The check below it is the
# same request answered by the handler, which knows it is a retry and says 200.
[[ "$status" == "201" ]] || { cat "$WORKDIR/ms-replay.json"; fail "the immediate retry returned $status, want the stored 201 replayed"; }
replayed_from_redis replay || fail "the immediate retry was not replayed by the middleware, so the cheap path is not working"
[[ "$(json "$WORKDIR/ms-replay.json" '["id"]')" == "$milestone_id" ]] \
  || fail "the replay answered with a different milestone"
ok "the same key immediately after replays the stored 201 from Redis, byte for byte — the handler is never reached"

# The entry is deleted, which is what a TTL expiry, an eviction or a failover looks like from the
# handler's side. **This is the check the ticket turns on**: the request now runs for a second
# time, all the way to the table, and must still record nothing.
forget_the_cached_response "$milestone_key"
status="$(milestone_request "$delivery_provider_token" "$milestone_key" \
  "{\"milestone\":\"en_route_to_pickup\",\"recorded_at\":\"$milestone_recorded_at\",\"reason\":\"  gate   locked \"}" expired)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/ms-expired.json"; fail "the retry after the cached response expired returned $status, want 200"; }
! replayed_from_redis expired || fail "the entry was deleted and the middleware still replayed; this check is proving nothing"
[[ "$(json "$WORKDIR/ms-expired.json" '["id"]')" == "$milestone_id" ]] \
  || fail "the retry recorded a second milestone: $(json "$WORKDIR/ms-expired.json" '["id"]') is not $milestone_id"
ok "with the cached response gone the request runs again, reaches the table, and is answered from the row it already wrote"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$milestone_job';")" == "1" ]] \
  || fail "one key recorded more than one milestone"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$milestone_job' and to_status = 'En route to pickup';")" == "1" ]] \
  || fail "the retry moved the job a second time"
ok "one key, three requests, one milestone and one transition — which is Redis for the first retry and uq_milestones_idempotency for the second"

# A key that recorded something else. The middleware refuses this on its fingerprint while its
# entry lives, so the entry is deleted first and the *database* is what answers — with the same
# code, deliberately: a client should not have to know which layer caught it.
forget_the_cached_response "$milestone_key"
status="$(milestone_request "$delivery_provider_token" "$milestone_key" '{"milestone":"picked_up"}' reused)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/ms-reused.json"; fail "a key reused for another milestone returned $status, want 409"; }
[[ "$(json "$WORKDIR/ms-reused.json" '["error"]["code"]')" == "idempotency_key_reused" ]] \
  || { cat "$WORKDIR/ms-reused.json"; fail "expected code=idempotency_key_reused"; }
ok "the same key against a different milestone is refused by the index, with the code the middleware would have used"

# --- a repeat that is not a retry ---

status="$(milestone_request "$delivery_provider_token" "$milestone_key-again" \
  '{"milestone":"en_route_to_pickup","reason":"nobody at the gate, returning at four"}' again)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/ms-again.json"; fail "recording the same milestone under a new key returned $status, want 201"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$milestone_job';")" == "2" ]] \
  || fail "a second recording under a new key was absorbed as a retry"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$milestone_job' and to_status = 'En route to pickup';")" == "1" ]] \
  || fail "the second recording moved the job again"
ok "a failed pickup attempt recorded again under a new key is a second row and no second transition (Docs/02 §5)"

# --- the rest of the delivery, and the two milestones this endpoint will not record ---

status="$(milestone_request "$delivery_provider_token" "$milestone_key-pickup" '{"milestone":"picked_up"}' pickup)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/ms-pickup.json"; fail "recording picked_up returned $status"; }
status="$(milestone_request "$delivery_provider_token" "$milestone_key-transit" '{"milestone":"in_transit"}' transit)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/ms-transit.json"; fail "recording in_transit returned $status"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$milestone_job';")" == "In transit" ]] \
  || fail "the job did not follow its milestones"
ok "the delivery runs through its milestones, each one a named move on the port rather than a status the domain chose"

status="$(milestone_request "$delivery_provider_token" "$milestone_key-delivered" '{"milestone":"delivered"}' delivered)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/ms-delivered.json"; fail "delivered returned $status, want 409"; }
[[ "$(json "$WORKDIR/ms-delivered.json" '["error"]["code"]')" == "delivery_proof_required" ]] \
  || { cat "$WORKDIR/ms-delivered.json"; fail "expected code=delivery_proof_required"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$milestone_job';")" == "In transit" ]] \
  || fail "a job reached Delivered with neither proof nor a recorded exception"
ok "delivered is refused while nothing can prove it — the invariant holds without SHIP-118, which narrows the refusal rather than adding it"

status="$(milestone_request "$delivery_provider_token" "$milestone_key-late" '{"milestone":"en_route_to_pickup"}' late)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/ms-late.json"; fail "a milestone the job has moved past returned $status, want 409"; }
[[ "$(json "$WORKDIR/ms-late.json" '["error"]["code"]')" == "delivery_milestone_not_permitted" ]] \
  || { cat "$WORKDIR/ms-late.json"; fail "expected code=delivery_milestone_not_permitted"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$milestone_job';")" == "4" ]] \
  || fail "the refused recording left a row behind; SHIP-112 keeps it deliberately, and until then the transaction must not"
ok "a milestone the job has moved past is refused and rolls back whole — SHIP-112 is the ticket that absorbs it instead"

status="$(milestone_request "$delivery_provider_token" "$milestone_key-assigned" '{"milestone":"driver_assigned"}' assigned)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/ms-assigned.json"; fail "driver_assigned returned $status, want 422"; }
[[ "$(json "$WORKDIR/ms-assigned.json" '["error"]["details"][0]["field"]')" == "milestone" ]] \
  || { cat "$WORKDIR/ms-assigned.json"; fail "the refusal does not name the milestone field"; }
ok "driver_assigned is refused here — it has an endpoint of its own, and nothing writes it to this table"

# ---------------------------------------------------------------------------------------
ticket "SHIP-108  a driver token grants exactly one job and cannot be exchanged for a user session"

# # What only this section can show
#
# internal/delivery drives the guard on a mux of its own and cmd/api drives it through the real
# router, but both build their key material from constants. What neither can show is the running
# service, configured from deploy/.env, refusing and accepting the right things — and, more to the
# point, **both directions of the exchange invariant against one binary**. SHIP-107 could only prove
# one of them here, because no route accepted a driver token; the route below is what closes it.
#
# Everything runs against the two assignments the SHIP-107 section already made, so no fixture is
# repeated: $delivery_driver_token grants $delivery_job, $delivery_second_token grants
# $delivery_second_job, and $delivery_forged is that first token repointed at the second job with
# its signature left alone.
#
# The route is a GET, so nothing here needs an Idempotency-Key — and that is not an accident.
# Docs/11 §9 records that a driver-token request scopes its idempotency key to `anonymous`, because
# the scope is computed group-wide outside the middleware while a guard runs per route inside it. A
# read does not meet that; SHIP-121's milestone controls will.

# open_delivery <token> <job-id> <name> — one driver-portal read, answering with the status code.
#
# The credential and the job are separate arguments on purpose: every check below is some pairing of
# the two, and a helper that derived one from the other could not express the pairing that matters.
open_delivery() {
  curl -s -o "$WORKDIR/driver-$3.json" -D "$WORKDIR/driver-$3.headers" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT/v1/driver/jobs/$2"
}

status="$(open_delivery "$delivery_driver_token" "$delivery_job" own)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/driver-own.json"; fail "the driver's own link returned $status, want 200"; }
[[ "$(json "$WORKDIR/driver-own.json" '["job_id"]')" == "$delivery_job" ]] \
  || fail "the link opened $(json "$WORKDIR/driver-own.json" '["job_id"]'), want $delivery_job"
[[ "$(json "$WORKDIR/driver-own.json" '["assignment_id"]')" == "$delivery_assignment_id" ]] \
  || fail "the link named assignment $(json "$WORKDIR/driver-own.json" '["assignment_id"]'), want $delivery_assignment_id"
[[ "$(json "$WORKDIR/driver-own.json" '["driver_name"]')" == "Sam Patel" ]] \
  || fail "driver_name is $(json "$WORKDIR/driver-own.json" '["driver_name"]')"
ok "the driver's link opens the one delivery it names — the first route in the service served on a credential that is not a mobile session"

# json_keys <file> — the top-level keys of a JSON object, sorted and comma-separated.
#
# The same shape as jwt_claim_names and deliberately a second function: that one is about a token's
# claims and this is about a response body, and one helper doing both would read as though a response
# were a token.
json_keys() { python3 -c 'import json,sys; print(",".join(sorted(json.load(open(sys.argv[1])))))' "$1"; }

# The response held to a closed set of keys rather than searched for the field it must not carry,
# which is SHIP-83's argument: a search for driver_mobile catches driver_mobile and misses `phone`.
driver_response_keys="$(json_keys "$WORKDIR/driver-own.json")"
[[ "$driver_response_keys" == "assigned_at,assignment_id,driver_name,job_id,link_expires_at" ]] \
  || fail "the driver's view carries '$driver_response_keys', want exactly assigned_at,assignment_id,driver_name,job_id,link_expires_at"
ok "and answers with five fields and no sixth — no mobile number, and no delivery detail, which is SHIP-120's"

# --- exactly one job, and this is the check the *Done when* rests on ---------------------------

status="$(open_delivery "$delivery_driver_token" "$delivery_second_job" wrong)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/driver-wrong.json"; fail "a link for $delivery_job opened $delivery_second_job with status $status, want 404"; }
[[ "$(json "$WORKDIR/driver-wrong.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/driver-wrong.json"; fail "expected code=not_found; a 403 would confirm the other job exists"; }
ok "the same link presented on another job gets exactly what a job that does not exist gets"

status="$(open_delivery "$delivery_second_token" "$delivery_job" wrong-back)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/driver-wrong-back.json"; fail "the second link opened the first job: $status"; }
status="$(open_delivery "$delivery_second_token" "$delivery_second_job" second-own)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/driver-second-own.json"; fail "the second link did not open its own job: $status"; }
ok "and the refusal is symmetric — each link opens its own delivery and neither opens the other's"

# --- the exchange, over HTTP, in both directions at once ---------------------------------------

status="$(open_delivery "$delivery_provider_token" "$delivery_job" as-session)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/driver-as-session.json"; fail "a mobile session token opened the driver's route: $status"; }
[[ "$(json "$WORKDIR/driver-as-session.json" '["error"]["code"]')" == "unauthenticated" ]] \
  || { cat "$WORKDIR/driver-as-session.json"; fail "expected code=unauthenticated"; }
ok "a mobile session token is refused on the driver's route — the direction SHIP-107 could not test, because no route accepted a driver token"

status="$(curl -s -o "$WORKDIR/driver-on-user-route.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $delivery_driver_token" \
  "http://localhost:$VERIFY_PORT/v1/jobs")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/driver-on-user-route.json"; fail "a driver token was accepted as a mobile session: $status"; }
ok "and the driver's link is still refused on a user route — both directions, one binary, one run"

# --- the ways a link fails to be one -----------------------------------------------------------

status="$(curl -s -o "$WORKDIR/driver-none.json" -D "$WORKDIR/driver-none.headers" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/driver/jobs/$delivery_job")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/driver-none.json"; fail "the driver route was reachable with no credential: $status"; }
grep -qi '^www-authenticate: *Bearer' "$WORKDIR/driver-none.headers" \
  || { cat "$WORKDIR/driver-none.headers"; fail "the 401 carries no WWW-Authenticate challenge, which RFC 9110 requires"; }
ok "it cannot be reached with no link at all, and the refusal says what scheme it wanted"

# The forgery from the SHIP-107 section: the first job's token with its `job_id` rewritten to the
# second job and its signature untouched. Presented on the job it now *claims*, so a service that
# read the claim before checking the signature would answer 200.
status="$(open_delivery "$delivery_forged" "$delivery_second_job" forged)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/driver-forged.json"; fail "a repointed token opened the job it was repointed at: $status, want 401"; }
[[ "$(json "$WORKDIR/driver-forged.json" '["error"]["code"]')" == "unauthenticated" ]] \
  || { cat "$WORKDIR/driver-forged.json"; fail "expected code=unauthenticated"; }
ok "a token repointed at the job it is presented on is refused by the signature, before the job is ever compared"

# An expired link, built by hand from the development key rather than by waiting seven days. The
# header names the same kid the service signs with, so everything about it verifies except `exp`.
driver_expired_header="$(printf '{"alg":"HS256","kid":"driver-dev","typ":"JWT"}' | b64url)"
driver_expired_claims="$(python3 - "$delivery_job" "$delivery_assignment_id" <<'PY'
import base64, json, sys, time, uuid

issued = int(time.time()) - 8 * 24 * 3600
claims = {
    "job_id": sys.argv[1],
    "assignment_id": sys.argv[2],
    "iat": issued,
    "exp": issued + 7 * 24 * 3600,
    "jti": str(uuid.uuid4()),
    "iss": "shipper",
    "aud": "shipper-driver",
}
raw = json.dumps(claims, separators=(',', ':')).encode()
print(base64.urlsafe_b64encode(raw).rstrip(b'=').decode())
PY
)"
driver_expired_input="$driver_expired_header.$driver_expired_claims"
driver_expired="$driver_expired_input.$(hs256 "$driver_expired_input" "$delivery_driver_dev_key")"

status="$(open_delivery "$driver_expired" "$delivery_job" expired)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/driver-expired.json"; fail "an expired link returned $status, want 401"; }
[[ "$(json "$WORKDIR/driver-expired.json" '["error"]["code"]')" == "delivery_driver_link_expired" ]] \
  || { cat "$WORKDIR/driver-expired.json"; fail "expected code=delivery_driver_link_expired, got $(json "$WORKDIR/driver-expired.json" '["error"]["code"]')"; }
ok "a link that has run out is refused with a code of its own — a driver has nothing to refresh, so token_expired's 'refresh and retry' would be a loop"

# The same claims signed with identity's key instead. Two keysets is what makes this a 401 rather
# than a working link, and it is the running service's answer rather than a struct's.
driver_wrong_key="$driver_expired_input.$(hs256 "$driver_expired_input" "$delivery_mobile_dev_key")"
status="$(open_delivery "$driver_wrong_key" "$delivery_job" wrong-key)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/driver-wrong-key.json"; fail "a link signed with identity's key was accepted: $status"; }
[[ "$(json "$WORKDIR/driver-wrong-key.json" '["error"]["code"]')" == "unauthenticated" ]] \
  || { cat "$WORKDIR/driver-wrong-key.json"; fail "expected code=unauthenticated"; }
ok "and one signed with the mobile session's key is refused too — separate key material, checked by the service that is running"
