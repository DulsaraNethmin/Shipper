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
#
# # The last section reads the outbox, and nothing here reads Kafka or starts the worker
#
# SHIP-136 gave this domain its events. That section is fenced on the **job identifiers this file
# created** — which are the aggregate ids of a delivery event — rather than counted over the table,
# because this database persists between runs. Its rows are left unpublished on purpose, for
# 80-notifications.sh to drain.

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

status="$(milestone_request "$delivery_provider_token" "$milestone_key-assigned" '{"milestone":"driver_assigned"}' assigned)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/ms-assigned.json"; fail "driver_assigned returned $status, want 422"; }
[[ "$(json "$WORKDIR/ms-assigned.json" '["error"]["details"][0]["field"]')" == "milestone" ]] \
  || { cat "$WORKDIR/ms-assigned.json"; fail "the refusal does not name the milestone field"; }
ok "driver_assigned is refused here — it has an endpoint of its own, and nothing writes it to this table"

# ---------------------------------------------------------------------------------------
ticket "SHIP-112  a late milestone is recorded as history without moving the job backwards"

# # Why these checks are here and not only in Go
#
# The refusal `delivery` absorbs is produced by neither `delivery` nor `jobs`: the transition guard
# answers one sentinel for a move that points backwards and one that points forwards, and telling
# them apart is the composition root's half — `jobLifecycle.refusal` in cmd/api/routes_delivery.go.
# internal/delivery's tests drive a *copy* of that adapter, because cmd/api has no database in a Go
# test. The production one is exercised here or nowhere, and a copy that drifted would show as a
# late milestone absorbed by one and refused by the other.
#
# $milestone_job is In transit with four milestone rows and one transition into 'En route to pickup'
# behind it. The driver's queued `en_route_to_pickup`, recorded at dawn in a yard with no signal, is
# about to arrive — which is Docs/02 §3.1's own example read backwards.

status="$(milestone_request "$delivery_provider_token" "$milestone_key-late" \
  '{"milestone":"en_route_to_pickup","recorded_at":"2026-08-12T06:40:11Z"}' late)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/ms-late.json"; fail "a late milestone returned $status, want 201 — Docs/02 §3.1 requires it absorbed, not rejected"; }
milestone_late_id="$(json "$WORKDIR/ms-late.json" '["id"]')"
[[ "$milestone_late_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the absorbed milestone came back with no id"
ok "a milestone the job has moved past is recorded rather than refused — the 409 SHIP-111 answered with has gone"

# The row, read from the table rather than from what the endpoint said about itself. This is the
# load-bearing half: a late milestone that is silently discarded is what this ticket exists to stop.
milestone_late_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select milestone || ' @ ' || to_char(actor_recorded_at at time zone 'UTC', 'YYYY-MM-DD HH24:MI:SS')
        || ' arrived ' || (server_recorded_at > actor_recorded_at)
     from milestones where id = '$milestone_late_id';")"
[[ "$milestone_late_row" == "En route to pickup @ 2026-08-12 06:40:11 arrived true" ]] \
  || fail "the absorbed milestone is stored as '$milestone_late_row', want 'En route to pickup @ 2026-08-12 06:40:11 arrived true'"
ok "the row is there afterwards, carrying the actor's own clock uncorrected and the platform's beside it — absorption is what the pair SHIP-110 built is for"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$milestone_job';")" == "5" ]] \
  || fail "the late milestone did not survive its own request"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$milestone_job';")" == "In transit" ]] \
  || fail "a late milestone moved the job backwards"
ok "five rows and the job still In transit — the record grew and the delivery did not go backwards"

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$milestone_job' and to_status = 'En route to pickup';")" == "1" ]] \
  || fail "an absorbed milestone wrote a job_status_history row for a move that did not happen"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select to_status from job_status_history where job_id = '$milestone_job' order by server_recorded_at desc limit 1;")" \
  == "In transit" ]] || fail "the job's latest recorded transition is no longer the one it really made last"
ok "and no transition was recorded for it — 'without moving the job backwards' means the history says nothing moved, because nothing did"

# The retry, because absorption must not have opened a second way past uq_milestones_idempotency.
forget_the_cached_response "$milestone_key-late"
status="$(milestone_request "$delivery_provider_token" "$milestone_key-late" \
  '{"milestone":"en_route_to_pickup","recorded_at":"2026-08-12T06:40:11Z"}' late-again)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/ms-late-again.json"; fail "the retry of an absorbed milestone returned $status, want 200"; }
[[ "$(json "$WORKDIR/ms-late-again.json" '["id"]')" == "$milestone_late_id" ]] \
  || fail "the retry answered with a different milestone"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$milestone_job';")" == "5" ]] \
  || fail "the retry of an absorbed milestone wrote a second row"
ok "retried with its cached response deleted it is answered from the row it already wrote — once per key, absorbed or not"

# --- the other direction, which is deliberately still a refusal ---------------------------------
#
# A milestone the delivery has not *reached* is not a historical fact that arrived out of order. It
# is refused, and SHIP-113 is the ticket that decides what a job cancelled out from under a queued
# update does with one.

milestone_early_job="$(delivery_awarded_job early)"
delivery_move "$milestone_early_job" Awarded 'En route to pickup'

status="$(curl -s -X POST -o "$WORKDIR/ms-early.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $delivery_provider_token" -H "Idempotency-Key: $milestone_key-early" \
  -H 'Content-Type: application/json' -d '{"milestone":"in_transit"}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$milestone_early_job/milestones")"
[[ "$status" == "409" ]] || { cat "$WORKDIR/ms-early.json"; fail "a premature milestone returned $status, want 409"; }
[[ "$(json "$WORKDIR/ms-early.json" '["error"]["code"]')" == "delivery_milestone_not_permitted" ]] \
  || { cat "$WORKDIR/ms-early.json"; fail "expected code=delivery_milestone_not_permitted"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$milestone_early_job';")" == "0" ]] \
  || fail "a premature recording left a row behind; only a late one is kept"
ok "a milestone the delivery has not reached yet is still refused and still rolls back whole — retrying it succeeds, which is what makes the refusal cost a retry rather than a record"

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

# ---------------------------------------------------------------------------------------
ticket "SHIP-120a  a driver records a milestone on the job their link grants, and on no other"

# # What only this section can show
#
# internal/delivery drives the handler over a real database on a mux of its own, and cmd/api drives
# the route through the real middleware chain with no database behind it. Neither package can have
# both at once — cmd/api has no pool in a Go test, and internal/delivery cannot import package main —
# so **the running binary is the only place where a real link, the real guard, the real composition
# root and a real row all meet**. That is also where the attribution is decided: the adapter that
# turns a delivery.Recorder into a jobs.Actor lives in cmd/api/routes_delivery.go and is exercised
# here or nowhere.
#
# # A fixture of its own, deliberately
#
# The jobs above have been driven through several milestones already, and this ticket is about who
# is allowed to record one rather than about where a delivery has got to. Two fresh awarded jobs,
# each with its own driver, keep the two questions apart — and give the "and on no other" check a
# second real delivery to be refused on rather than a job identifier nobody assigned.

drv_job="$(delivery_awarded_job drvms)"
status="$(delivery_request "$delivery_provider_token" "verify-drvms-assign-$$" \
  "/v1/jobs/$drv_job/driver" '{"driver_name":"Nina Alvares","driver_mobile":"+61417000101"}' \
  "$WORKDIR/drvms-assign.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/drvms-assign.json"; fail "assigning the driver returned $status, want 201"; }
drv_token="$(json "$WORKDIR/drvms-assign.json" '["driver_token"]')"
drv_assignment="$(json "$WORKDIR/drvms-assign.json" '["id"]')"

drv_other_job="$(delivery_awarded_job drvms-other)"
status="$(delivery_request "$delivery_provider_token" "verify-drvms-other-assign-$$" \
  "/v1/jobs/$drv_other_job/driver" '{"driver_name":"Owen Blake","driver_mobile":"+61417000102"}' \
  "$WORKDIR/drvms-other-assign.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/drvms-other-assign.json"; fail "assigning the second driver returned $status, want 201"; }

# drv_record <token> <job-id> <key> <body> <name> — one driver milestone, answering with the status.
#
# The credential and the job are separate arguments for the reason open_delivery's are: every check
# below is a pairing of the two, and a helper that built the path out of the token could not express
# the pairing this ticket exists to refuse.
drv_record() {
  curl -s -X POST -o "$WORKDIR/drvms-$5.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $3" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT/v1/driver/jobs/$2/milestones"
}

# --- the recording itself, and what the two tables say about who made it -----------------------

drv_acted_at="$(python3 -c 'import datetime; print((datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(minutes=90)).strftime("%Y-%m-%dT%H:%M:%SZ"))')"
status="$(drv_record "$drv_token" "$drv_job" "verify-drvms-first-$$" \
  "{\"milestone\":\"en_route_to_pickup\",\"recorded_at\":\"$drv_acted_at\"}" first)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/drvms-first.json"; fail "the driver's own link recorded nothing: $status, want 201"; }
[[ "$(json "$WORKDIR/drvms-first.json" '["job_id"]')" == "$drv_job" ]] \
  || fail "the milestone landed on $(json "$WORKDIR/drvms-first.json" '["job_id"]'), want $drv_job"
[[ "$(json "$WORKDIR/drvms-first.json" '["recorded_by"]')" == "driver" ]] \
  || fail "recorded_by is $(json "$WORKDIR/drvms-first.json" '["recorded_by"]'), want driver"
ok "a driver holding a job-scoped link records a milestone on that job — the first write in the service served on a credential that names no account"

drv_milestone_id="$(json "$WORKDIR/drvms-first.json" '["id"]')"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select actor_type || '/' || actor_id from milestones where id = '$drv_milestone_id';")" \
   == "driver/$drv_assignment" ]] \
  || fail "the milestone row is not attributed to the driver_assignments row the link names"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select actor_type || '/' || actor_id from job_status_history
     where job_id = '$drv_job' and to_status = 'En route to pickup';")" == "driver/$drv_assignment" ]] \
  || fail "the transition the driver's milestone caused is attributed to somebody other than the driver"
ok "and both rows say a driver did it, naming the assignment rather than an account — 000401's convention, through cmd/api's own adapter"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$drv_job';")" == "En route to pickup" ]] \
  || fail "the job did not move, so the driver's milestone reached no guarded transition"
[[ "$(json "$WORKDIR/drvms-first.json" '["recorded_at"]')" != "$(json "$WORKDIR/drvms-first.json" '["accepted_at"]')" ]] \
  || fail "the two clocks are the same value; the driver acted ninety minutes before the platform heard about it"
ok "the job moves through the same guard, and the two clocks stay ninety minutes apart — Docs/02 §3.1 on the credential it was written for"

# --- exactly one job, which is the half of the *Done when* the invariant rests on ---------------

status="$(drv_record "$drv_token" "$drv_other_job" "verify-drvms-wrong-$$" \
  '{"milestone":"en_route_to_pickup"}' wrong)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/drvms-wrong.json"; fail "a link for $drv_job recorded on $drv_other_job: $status, want 404"; }
[[ "$(json "$WORKDIR/drvms-wrong.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/drvms-wrong.json"; fail "expected code=not_found; a 403 would confirm the other delivery exists"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$drv_other_job';")" == "0" ]] \
  || fail "the refused request wrote a milestone onto the other job"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$drv_other_job';")" == "Driver assigned" ]] \
  || fail "the other job moved, so something acted on it"
ok "the same link presented on another delivery gets exactly what a missing job gets, and writes nothing"

# --- neither token system opens the other's milestone route -------------------------------------

status="$(drv_record "$delivery_provider_token" "$drv_job" "verify-drvms-session-$$" \
  '{"milestone":"picked_up"}' as-session)"
[[ "$status" == "401" ]] || { cat "$WORKDIR/drvms-as-session.json"; fail "a mobile session recorded on the driver's route: $status, want 401"; }
[[ "$(json "$WORKDIR/drvms-as-session.json" '["error"]["code"]')" == "unauthenticated" ]] \
  || { cat "$WORKDIR/drvms-as-session.json"; fail "expected code=unauthenticated"; }

status="$(delivery_request "$drv_token" "verify-drvms-onuser-$$" \
  "/v1/jobs/$drv_job/milestones" '{"milestone":"picked_up"}' "$WORKDIR/drvms-on-user-route.json")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/drvms-on-user-route.json"; fail "a driver link recorded on the user route: $status, want 401"; }
ok "a mobile access token is refused on the driver's route and the driver's link is refused on POST /v1/jobs/{id}/milestones — both directions, one binary"

# --- the key, which is why this endpoint exists on a phone with a bad connection -----------------

status="$(curl -s -X POST -o "$WORKDIR/drvms-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $drv_token" -H 'Content-Type: application/json' \
  -d '{"milestone":"picked_up"}' \
  "http://localhost:$VERIFY_PORT/v1/driver/jobs/$drv_job/milestones")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/drvms-nokey.json"; fail "the driver's write was accepted with no idempotency key: $status"; }
[[ "$(json "$WORKDIR/drvms-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || { cat "$WORKDIR/drvms-nokey.json"; fail "expected code=idempotency_key_required"; }
ok "a driver's write with no Idempotency-Key is refused before it reaches a handler"

status="$(drv_record "$drv_token" "$drv_job" "verify-drvms-first-$$" \
  "{\"milestone\":\"en_route_to_pickup\",\"recorded_at\":\"$drv_acted_at\"}" retry)"
[[ "$status" == "201" || "$status" == "200" ]] || { cat "$WORKDIR/drvms-retry.json"; fail "the retry returned $status"; }
[[ "$(json "$WORKDIR/drvms-retry.json" '["id"]')" == "$drv_milestone_id" ]] \
  || fail "the retry answered with a different milestone, so a driver's reconnection recorded a second one"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from milestones where job_id = '$drv_job' and idempotency_key = 'verify-drvms-first-$$';")" == "1" ]] \
  || fail "the key recorded more than one milestone"
ok "and a retry under the same key answers with the milestone the first attempt recorded, once — the driver on a mobile browser this rule exists for"

# --- the evidence line this ticket draws --------------------------------------------------------

status="$(drv_record "$drv_token" "$drv_job" "verify-drvms-key-$$" \
  "{\"milestone\":\"picked_up\",\"proof\":{\"object_key\":\"proof/$drv_job/019bd7a1-2c44-7f10-9a2c-3d4e5f607182\"}}" objkey)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/drvms-objkey.json"; fail "a driver attached a photograph: $status, want 422"; }
[[ "$(json "$WORKDIR/drvms-objkey.json" '["error"]["details"][0]["field"]')" == "proof.object_key" ]] \
  || { cat "$WORKDIR/drvms-objkey.json"; fail "the refusal does not name proof.object_key"; }

status="$(drv_record "$drv_token" "$drv_job" "verify-drvms-exception-$$" \
  '{"milestone":"picked_up","proof":{"exception_reason":"camera_unavailable"}}' exception)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/drvms-exception.json"; fail "a driver could not record a reasoned exception: $status"; }
ok "a driver may say why there is no photograph and may not attach one — there is no route by which a driver could obtain a key, and SHIP-122 builds both halves together"

# --- the event, fenced on this section's own job ------------------------------------------------

[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox
     where aggregate_type = 'delivery' and aggregate_id = '$drv_job'
       and event_type = 'delivery.milestone_recorded' and payload->>'actor_type' = 'driver';")" -ge 1 ]] \
  || fail "the driver's milestone emitted no delivery.milestone_recorded naming a driver"
ok "and the driver's recording emits the domain's own event, saying a driver made it"

unset drv_acted_at

# ---------------------------------------------------------------------------------------
ticket "SHIP-109  a provider reissues a driver's link and the previous one stops working"

# # What only this section can show
#
# internal/delivery holds both tokens and presents each in turn, which is the assertion that matters
# and it is made there against a real database. What is left for the binary is the wiring: that the
# route is served, that it is the *provider's* route rather than the driver's, and that the link the
# response carries is one the running service will actually accept.
#
# # The half of the *Done when* that cannot be met, and it is not a shortcut
#
# "Provider **or admin** can reissue a link." There is no administrator: ck_users_role refuses the
# role, admin sign-in is a separate system, and RequireAdmin is unmapped until SHIP-147 — a route
# declaring it would stop the process at startup rather than be served open. Nothing below is
# narrowed to hide that; Docs/11 §3 records it, and the service method is already the one a second
# route would call.

drv_link_job="$(delivery_awarded_job reissue)"
status="$(delivery_request "$delivery_provider_token" "verify-reissue-assign-$$" \
  "/v1/jobs/$drv_link_job/driver" '{"driver_name":"Priya Raman","driver_mobile":"+61417000201"}' \
  "$WORKDIR/reissue-assign.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/reissue-assign.json"; fail "assigning the driver returned $status, want 201"; }
drv_link_first="$(json "$WORKDIR/reissue-assign.json" '["driver_token"]')"
drv_link_assignment="$(json "$WORKDIR/reissue-assign.json" '["id"]')"

status="$(open_delivery "$drv_link_first" "$drv_link_job" reissue-before)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/driver-reissue-before.json"; fail "the original link did not open the delivery: $status"; }
ok "the link the assignment minted opens the delivery, which is the baseline the refusal below is measured against"

# --- the reissue itself ------------------------------------------------------------------------

status="$(delivery_request "$delivery_provider_token" "verify-reissue-$$" \
  "/v1/jobs/$drv_link_job/driver/link" '{}' "$WORKDIR/reissue.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/reissue.json"; fail "reissuing returned $status, want 200"; }
drv_link_second="$(json "$WORKDIR/reissue.json" '["driver_token"]')"
[[ "$drv_link_second" != "$drv_link_first" ]] || fail "the reissue handed back the same token, so nothing was invalidated"
[[ "$(json "$WORKDIR/reissue.json" '["assignment_id"]')" == "$drv_link_assignment" ]] \
  || fail "the reissue changed the assignment; it replaces the link and not the driver"
[[ "$(json "$WORKDIR/reissue.json" '["issue_count"]')" == "2" ]] \
  || fail "issue_count is $(json "$WORKDIR/reissue.json" '["issue_count"]'), want 2"
ok "a provider is handed a second link for the same driver, on the same assignment, and the count says which"

status="$(open_delivery "$drv_link_first" "$drv_link_job" reissue-old)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/driver-reissue-old.json"; fail "the previous link still opens the delivery: $status, want 404"; }
[[ "$(json "$WORKDIR/driver-reissue-old.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/driver-reissue-old.json"; fail "expected code=not_found — telling a holder their link used to work says something about a delivery they are no longer on"; }
status="$(open_delivery "$drv_link_second" "$drv_link_job" reissue-new)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/driver-reissue-new.json"; fail "the new link does not open the delivery: $status"; }
ok "and the previous one gets exactly what a job that does not exist gets, while the new one opens the delivery — revocation as a read against the row, with no denylist to grow"

status="$(drv_record "$drv_link_first" "$drv_link_job" "verify-reissue-write-$$" \
  '{"milestone":"en_route_to_pickup"}' reissue-write)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/drvms-reissue-write.json"; fail "a superseded link recorded a milestone: $status, want 404"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$drv_link_job';")" == "0" ]] \
  || fail "the superseded link wrote a milestone"
ok "the dead link writes nothing either — one check serves the read and the write, because both go through the assignment"

# --- who may ask -------------------------------------------------------------------------------

status="$(delivery_request "$delivery_other_token" "verify-reissue-other-$$" \
  "/v1/jobs/$drv_link_job/driver/link" '{}' "$WORKDIR/reissue-other.json")"
[[ "$status" == "404" ]] || { cat "$WORKDIR/reissue-other.json"; fail "another provider reissued the link: $status, want 404"; }

status="$(delivery_request "$drv_link_second" "verify-reissue-bydriver-$$" \
  "/v1/jobs/$drv_link_job/driver/link" '{}' "$WORKDIR/reissue-bydriver.json")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/reissue-bydriver.json"; fail "a driver reissued their own link: $status, want 401"; }
ok "a competitor gets what a missing job gets, and the driver cannot reissue their own link at all — which is the point of a credential the provider controls"

status="$(open_delivery "$drv_link_second" "$drv_link_job" reissue-survived)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/driver-reissue-survived.json"; fail "a refused reissue invalidated the live link anyway: $status"; }
ok "and neither refusal touched the live link, so a refused request wrote nothing"

# ---------------------------------------------------------------------------------------
ticket "SHIP-115a  both parties read the driver assignment and every recorded milestone"

# # What only this section can show
#
# The domain's tests drive both handlers over a real database with a subject injected directly. What
# they cannot show is the shelf **served**: that two five-segment GETs are registered beside
# /delivery/proof without the router refusing the set at startup, and that RequireUser is what stands
# in front of them. `make run` dying at startup is the failure this shape exists to avoid, and only a
# running binary can demonstrate that it does not.
#
# It reads the job the SHIP-120a section drove, which already carries a driver, a milestone that
# moved the job and one recorded with a reasoned exception — so the list below has something to be a
# list of.

# read_shelf <token> <job-id> <leaf> <name> — one authenticated read on the shelf.
read_shelf() {
  curl -s -o "$WORKDIR/shelf-$4.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT/v1/jobs/$2/delivery/$3"
}

status="$(read_shelf "$delivery_provider_token" "$drv_job" detail provider-detail)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/shelf-provider-detail.json"; fail "the provider could not read the delivery detail: $status"; }
[[ "$(json "$WORKDIR/shelf-provider-detail.json" '["driver_assigned"]')" == "True" ]] \
  || { cat "$WORKDIR/shelf-provider-detail.json"; fail "driver_assigned is not true on a job with a live driver"; }
[[ "$(json "$WORKDIR/shelf-provider-detail.json" '["driver_name"]')" == "Nina Alvares" ]] \
  || fail "driver_name is $(json "$WORKDIR/shelf-provider-detail.json" '["driver_name"]')"

status="$(read_shelf "$delivery_customer_token" "$drv_job" detail customer-detail)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/shelf-customer-detail.json"; fail "the customer could not read the delivery detail: $status"; }
[[ "$(json "$WORKDIR/shelf-customer-detail.json" '["assignment_id"]')" \
   == "$(json "$WORKDIR/shelf-provider-detail.json" '["assignment_id"]')" ]] \
  || fail "the two parties are looking at different assignments"
ok "both parties to the job read the driver assignment from a five-segment path served beside /delivery/proof"

# The mobile, held as a closed set of keys rather than searched for: a search for driver_mobile
# catches driver_mobile and misses `mobile` or `phone` (SHIP-83's argument, and wave 6's lesson).
[[ "$(json_keys "$WORKDIR/shelf-provider-detail.json")" == "assigned_at,assignment_id,driver_assigned,driver_mobile,driver_name,job_id" ]] \
  || fail "the provider's view carries '$(json_keys "$WORKDIR/shelf-provider-detail.json")'"
[[ "$(json_keys "$WORKDIR/shelf-customer-detail.json")" == "assigned_at,assignment_id,driver_assigned,driver_name,job_id" ]] \
  || fail "the customer's view carries '$(json_keys "$WORKDIR/shelf-customer-detail.json")', and the driver's number should not be among them"
ok "and the driver's number reaches the provider who typed it and not the customer — Docs/01 §4 on a person with no account and no way to consent"

# --- the timeline, including what a status history cannot show -----------------------------------

# A milestone that moves nothing, which is the case this operation exists for: the driver reaches
# the pickup, finds nobody there, and records the same milestone again (Docs/02 §5). It writes a row
# and leaves the job where it stands, so it appears in no timeline derived from the job's status.
status="$(drv_record "$drv_token" "$drv_job" "verify-shelf-repeat-$$" \
  '{"milestone":"picked_up"}' shelf-repeat)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/drvms-shelf-repeat.json"; fail "the repeated milestone returned $status, want 201"; }

status="$(read_shelf "$delivery_customer_token" "$drv_job" milestones customer-milestones)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/shelf-customer-milestones.json"; fail "the customer could not read the milestones: $status"; }
shelf_rows="$(python3 -c 'import json,sys; print(len(json.load(open(sys.argv[1]))["data"]))' "$WORKDIR/shelf-customer-milestones.json")"
shelf_stored="$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$drv_job';")"
[[ "$shelf_rows" == "$shelf_stored" ]] \
  || fail "the list returned $shelf_rows milestones and the table holds $shelf_stored"
# The transitions a *milestone* could have caused, rather than every transition the job has ever
# made — the fixture drove it through Draft, Open and Awarded before any of this, and counting those
# would make the comparison below pass for the wrong reason.
shelf_moves="$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$drv_job'
     and to_status in ('En route to pickup','Picked up','In transit','Delivered');")"
(( shelf_rows > shelf_moves )) \
  || fail "the list has $shelf_rows rows and the job has $shelf_moves transitions; this section is not exercising a milestone that moved nothing"
ok "every recorded milestone is on the list, including the ones that moved nothing — which appear here and in no timeline derived from the job's status"

[[ "$(json "$WORKDIR/shelf-customer-milestones.json" '["has_more"]')" == "False" ]] \
  || fail "has_more is true on a collection smaller than a page"
[[ "$(json "$WORKDIR/shelf-customer-milestones.json" '["data"][0]["recorded_by"]')" == "driver" ]] \
  || fail "the newest milestone does not say who recorded it"
ok "in the collection envelope, newest by the actor's clock first, each row saying who recorded it"

# --- a stranger gets exactly what a missing job gets ---------------------------------------------

# Compared with the request id removed, because "exactly what a missing job gets" is a statement
# about what somebody probing can tell apart — a difference in the message is one they can act on.
shelf_refusal() {
  python3 -c 'import json,sys; e=json.load(open(sys.argv[1]))["error"]; print(e["code"], "/", e["message"])' "$1"
}

shelf_missing_job="$(python3 -c 'import uuid; print(uuid.uuid4())')"
for shelf_leaf in detail milestones; do
  status="$(read_shelf "$delivery_other_token" "$drv_job" "$shelf_leaf" "stranger-$shelf_leaf")"
  [[ "$status" == "404" ]] || { cat "$WORKDIR/shelf-stranger-$shelf_leaf.json"; fail "a stranger read /delivery/$shelf_leaf: $status, want 404"; }

  status="$(read_shelf "$delivery_customer_token" "$shelf_missing_job" "$shelf_leaf" "missing-$shelf_leaf")"
  [[ "$status" == "404" ]] || { cat "$WORKDIR/shelf-missing-$shelf_leaf.json"; fail "a job that does not exist answered $status on /delivery/$shelf_leaf"; }

  [[ "$(shelf_refusal "$WORKDIR/shelf-stranger-$shelf_leaf.json")" \
     == "$(shelf_refusal "$WORKDIR/shelf-missing-$shelf_leaf.json")" ]] \
    || fail "a stranger and a missing job are distinguishable on /delivery/$shelf_leaf"
done
ok "a stranger gets exactly what a missing job gets on both paths, down to the message — a 403 would confirm that somebody else's delivery exists"

unset shelf_rows shelf_stored shelf_moves shelf_missing_job shelf_leaf
unset -f read_shelf shelf_refusal drv_record

# ---------------------------------------------------------------------------------------
ticket "SHIP-114  a client receives a short-lived pre-signed URL and uploads directly"

# The *Done when* is "client receives a short-lived pre-signed URL and uploads directly", and the
# word this section exists to demonstrate is **directly**. Everything else here is a Go test as
# well; the upload is the only claim that cannot be made without a running service, a running object
# store and a client that talks to one without going through the other.
#
# # The API is not in the path, and that is asserted rather than described
#
# The URL is checked to be at $STORAGE_ENDPOINT before it is used, the upload goes there, and the
# object is read back out of the bucket from inside the container. If SHIP-114 were ever
# "simplified" into a proxy upload, the first of those three fails.
#
# # Everything here fences on ids
#
# The object key carries the job id and a fresh UUIDv7, and the bucket may be shared with four other
# worktrees (CLAUDE.md's worktree table). So no check counts objects or lists a prefix; each one
# names the key it created, which is the rule that file states for Kafka applied to the one shared
# service that *can* be isolated but is not obliged to be.

proof_key_of() { json "$1" '["object_key"]'; }

status="$(delivery_request "$delivery_provider_token" "verify-del-proof-$$" \
  "/v1/jobs/$delivery_job/proof-uploads" '{"content_type":"image/jpeg","content_length":48}' \
  "$WORKDIR/delivery-proof.json")"
[[ "$status" == "200" ]] \
  || { cat "$WORKDIR/delivery-proof.json"; fail "minting an upload URL answered $status, want 200"; }

proof_key="$(proof_key_of "$WORKDIR/delivery-proof.json")"
proof_url="$(json "$WORKDIR/delivery-proof.json" '["upload_url"]')"
proof_method="$(json "$WORKDIR/delivery-proof.json" '["method"]')"
proof_type="$(json "$WORKDIR/delivery-proof.json" '["content_type"]')"
proof_length="$(json "$WORKDIR/delivery-proof.json" '["content_length"]')"
proof_expires="$(json "$WORKDIR/delivery-proof.json" '["expires_at"]')"

[[ "$proof_method" == "PUT" && "$proof_type" == "image/jpeg" && "$proof_length" == "48" ]] \
  || fail "the response describes a $proof_method of $proof_length bytes of $proof_type"
[[ "$proof_key" == proof/$delivery_job/* ]] \
  || fail "object_key is $proof_key, want it prefixed by proof/$delivery_job/"
ok "the awarded provider is issued an upload URL, an object key under their own job, and the two headers the store will hold them to"

# 200 rather than 201, because nothing was created: no row here and no object there. The record
# that turns an uploaded object into proof is SHIP-115's, and that one is a 201.
[[ "$proof_url" == "$STORAGE_ENDPOINT/"* ]] \
  || fail "upload_url is $proof_url, which is not the object store at $STORAGE_ENDPOINT"
[[ "$proof_url" != *"localhost:$VERIFY_PORT"* && "$proof_url" != *"/v1/"* ]] \
  || fail "upload_url points back at this service: $proof_url"
ok "and it points at the object store rather than at this API — the bytes are never proxied (Docs/06 §5.2)"

# Short-lived, checked two ways: the store's own expiry window is in the URL, and the instant the
# response reports is inside it. Neither number is typed here — internal/config owns the lifetime,
# and a check that hard-coded fifteen minutes would keep passing after somebody stopped reading it.
proof_window="$(python3 - "$proof_url" "$proof_expires" <<'PY'
import datetime, sys, urllib.parse

query = urllib.parse.parse_qs(urllib.parse.urlsplit(sys.argv[1]).query)
expires = int(query["X-Amz-Expires"][0])
reported = datetime.datetime.strptime(sys.argv[2], "%Y-%m-%dT%H:%M:%S.%f%z")
ahead = (reported - datetime.datetime.now(datetime.timezone.utc)).total_seconds()
print(f"{expires} {ahead:.0f}")
PY
)"
read -r proof_expires_seconds proof_seconds_ahead <<<"$proof_window"
(( proof_expires_seconds > 0 && proof_expires_seconds <= 3600 )) \
  || fail "the URL is signed to last $proof_expires_seconds seconds; internal/config caps the lifetime at an hour because nothing can revoke one"
(( proof_seconds_ahead > 0 && proof_seconds_ahead <= proof_expires_seconds + 5 )) \
  || fail "expires_at is $proof_seconds_ahead seconds away against a signed window of $proof_expires_seconds"
ok "the URL is short-lived — $proof_expires_seconds seconds in the signature, and expires_at agrees with it"

# --- the upload itself, with this service in neither direction ---------------------------------

printf '%s' 'not a photograph, but exactly forty-eight bytes.' > "$WORKDIR/proof.bin"
[[ "$(wc -c < "$WORKDIR/proof.bin" | tr -d ' ')" == "48" ]] || fail "the fixture is not 48 bytes"

put_status="$(curl -s -o /dev/null -w '%{http_code}' -X PUT \
  -H "Content-Type: $proof_type" --data-binary "@$WORKDIR/proof.bin" "$proof_url")"
[[ "$put_status" == "200" ]] \
  || fail "the pre-signed PUT answered $put_status — the client could not upload directly, which is the whole of SHIP-114"
ok "the client uploaded the photograph straight to the object store with that URL and nothing else"

stored="$("${COMPOSE[@]}" exec -T minio sh -c \
  'mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; \
   mc cat "local/'"$STORAGE_BUCKET/$proof_key"'"' 2>/dev/null)"
[[ "$stored" == "not a photograph, but exactly forty-eight bytes." ]] \
  || fail "the object in $STORAGE_BUCKET reads back as '$stored'"
ok "and the bytes are in $STORAGE_BUCKET under the key the API named, read back out of the store itself"

unsigned_status="$(curl -s -o /dev/null -w '%{http_code}' "$STORAGE_ENDPOINT/$STORAGE_BUCKET/$proof_key")"
[[ "$unsigned_status" == "403" ]] \
  || fail "an unsigned GET of the proof answered $unsigned_status, want 403 — a proof photograph identifies an address and a recipient"
ok "the same object is refused without a signature: there is no public read path to a proof photograph"

# --- what the store refuses, which is what makes the platform's limits enforcement -------------

status="$(delivery_request "$delivery_provider_token" "verify-del-proof-swap-$$" \
  "/v1/jobs/$delivery_job/proof-uploads" '{"content_type":"image/jpeg","content_length":48}' \
  "$WORKDIR/delivery-proof-swap.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/delivery-proof-swap.json"; fail "minting the second URL answered $status"; }
proof_swap_url="$(json "$WORKDIR/delivery-proof-swap.json" '["upload_url"]')"
proof_swap_key="$(proof_key_of "$WORKDIR/delivery-proof-swap.json")"

[[ "$proof_swap_key" != "$proof_key" ]] \
  || fail "two requests were issued the same object key; a reissued key can overwrite an object that already holds proof"

wrong_type_status="$(curl -s -o /dev/null -w '%{http_code}' -X PUT \
  -H 'Content-Type: image/svg+xml' --data-binary "@$WORKDIR/proof.bin" "$proof_swap_url")"
[[ "$wrong_type_status" == "403" ]] \
  || fail "uploading as image/svg+xml answered $wrong_type_status, want 403 — the content type is signed, so an accepted-formats list the store does not enforce is a list the client gave itself"

printf '%s' 'this body is a different length from the one that was authorised' > "$WORKDIR/proof-longer.bin"
wrong_size_status="$(curl -s -o /dev/null -w '%{http_code}' -X PUT \
  -H "Content-Type: $proof_type" --data-binary "@$WORKDIR/proof-longer.bin" "$proof_swap_url")"
[[ "$wrong_size_status" == "403" ]] \
  || fail "uploading a longer body answered $wrong_size_status, want 403 — the content length is the only bound a pre-signed PUT has"
ok "an upload that changes the type or the size the platform authorised is refused by the store, not by us"

# --- who may ask, and what they may ask for ----------------------------------------------------

status="$(delivery_request "$delivery_other_token" "verify-del-proof-other-$$" \
  "/v1/jobs/$delivery_job/proof-uploads" '{"content_type":"image/jpeg","content_length":48}' \
  "$WORKDIR/delivery-proof-other.json")"
[[ "$status" == "404" ]] \
  || { cat "$WORKDIR/delivery-proof-other.json"; fail "another provider was issued an upload URL for this job: $status"; }
[[ "$(json "$WORKDIR/delivery-proof-other.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/delivery-proof-other.json"; fail "expected code=not_found; a 403 would confirm the job exists and that somebody won it"; }
ok "a provider who was not awarded the job gets what a job that does not exist gets — the platform decides which job a caller may upload against, not the device"

status="$(delivery_request "$delivery_provider_token" "verify-del-proof-svg-$$" \
  "/v1/jobs/$delivery_job/proof-uploads" '{"content_type":"image/svg+xml","content_length":48}' \
  "$WORKDIR/delivery-proof-svg.json")"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/delivery-proof-svg.json"; fail "image/svg+xml was accepted: $status"; }
[[ "$(json "$WORKDIR/delivery-proof-svg.json" '["error"]["details"][0]["field"]')" == "content_type" ]] \
  || { cat "$WORKDIR/delivery-proof-svg.json"; fail "the refusal does not name content_type"; }

proof_over_limit=$(( ${STORAGE_MAX_UPLOAD_BYTES:-10485760} + 1 ))
status="$(delivery_request "$delivery_provider_token" "verify-del-proof-big-$$" \
  "/v1/jobs/$delivery_job/proof-uploads" \
  "{\"content_type\":\"image/jpeg\",\"content_length\":$proof_over_limit}" \
  "$WORKDIR/delivery-proof-big.json")"
[[ "$status" == "422" ]] \
  || { cat "$WORKDIR/delivery-proof-big.json"; fail "$proof_over_limit bytes was accepted: $status"; }
[[ "$(json "$WORKDIR/delivery-proof-big.json" '["error"]["details"][0]["field"]')" == "content_length" ]] \
  || { cat "$WORKDIR/delivery-proof-big.json"; fail "the refusal does not name content_length"; }
ok "a script container being an image and a photograph over the configured limit are both refused before anything is signed, with the field named"

# --- the key, and what a retry gets ------------------------------------------------------------

status="$(curl -s -X POST -o "$WORKDIR/delivery-proof-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $delivery_provider_token" -H 'Content-Type: application/json' \
  -d '{"content_type":"image/jpeg","content_length":48}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$delivery_job/proof-uploads")"
[[ "$status" == "400" ]] \
  || { cat "$WORKDIR/delivery-proof-nokey.json"; fail "minting a URL with no Idempotency-Key answered $status, want 400"; }
[[ "$(json "$WORKDIR/delivery-proof-nokey.json" '["error"]["code"]')" == "idempotency_key_required" ]] \
  || { cat "$WORKDIR/delivery-proof-nokey.json"; fail "expected code=idempotency_key_required"; }
ok "minting a URL is a state-changing request and is refused without an Idempotency-Key — issuing a credential nothing can revoke is not a safe method"

status="$(delivery_request "$delivery_provider_token" "verify-del-proof-$$" \
  "/v1/jobs/$delivery_job/proof-uploads" '{"content_type":"image/jpeg","content_length":48}' \
  "$WORKDIR/delivery-proof-retry.json")"
[[ "$status" == "200" ]] || { cat "$WORKDIR/delivery-proof-retry.json"; fail "the retry answered $status"; }
diff -q "$WORKDIR/delivery-proof.json" "$WORKDIR/delivery-proof-retry.json" >/dev/null \
  || { cat "$WORKDIR/delivery-proof-retry.json"; fail "a retry with the same key returned a different URL; one intent buys one upload slot and a retry must not extend a credential's life"; }
ok "a retry with the same key replays the identical URL and the identical expiry, already running down — a new slot needs a new key"

"${COMPOSE[@]}" exec -T minio sh -c \
  'mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; \
   mc rm --force "local/'"$STORAGE_BUCKET/$proof_key"'" >/dev/null 2>&1 || true' \
  || fail "could not remove the object this section uploaded"
ok "the object this run uploaded was removed from $STORAGE_BUCKET"

# ---------------------------------------------------------------------------------------
ticket "SHIP-115  uploaded proof is linked to a job and a milestone, with access control"

# # What only this section can show
#
# The Go tests in internal/delivery stub the object store, deliberately — a test cannot drive "the
# store is holding a 900 KB SVG" against a real bucket without uploading one. So the *reconciliation*
# this ticket turns on is demonstrated here or nowhere: **the platform is not in the upload path, so
# it asks the store whether the object arrived**, and a recording that names an object nobody
# uploaded is refused by the running service against the running MinIO.
#
# The access control is the other half, and it is checked from four directions on one binary: the
# job's customer, the awarded provider, a provider who won nothing, and a stranger's job.
#
# Everything fences on ids. The bucket may be shared with four other worktrees, so no check counts
# objects or lists a prefix — each names the key it created (CLAUDE.md's worktree table).

proof_job="$(delivery_awarded_job proof115)"

# proof_url_for <job> <key-suffix> <outfile> — one upload URL, answering with its object key.
proof_url_for() {
  local status
  status="$(delivery_request "$delivery_provider_token" "verify-p115-$2-$$" \
    "/v1/jobs/$1/proof-uploads" '{"content_type":"image/jpeg","content_length":48}' "$3")"
  [[ "$status" == "200" ]] || { cat "$3"; fail "minting an upload URL for $2 answered $status"; }
  json "$3" '["object_key"]'
}

# upload_to <outfile> <file> — PUT a file to the URL in a proof-upload response.
upload_to() {
  local put
  put="$(curl -s -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: image/jpeg' \
    --data-binary "@$2" "$(json "$1" '["upload_url"]')")"
  [[ "$put" == "200" ]] || fail "uploading to the pre-signed URL answered $put"
}

# record_milestone_on <job> <key> <body> <name> — one milestone recording on any job.
record_milestone_on() {
  curl -s -X POST -o "$WORKDIR/p115-$4.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $delivery_provider_token" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$3" \
    "http://localhost:$VERIFY_PORT/v1/jobs/$1/milestones"
}

# read_proof <token> <job> <name> — the proof on a job, as whoever holds that token.
read_proof() {
  curl -s -o "$WORKDIR/p115-read-$3.json" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" \
    "http://localhost:$VERIFY_PORT/v1/jobs/$2/delivery/proof"
}

# --- a photograph that never arrived, which is the case the platform cannot infer ---------------

proof115_missing="$(proof_url_for "$proof_job" missing "$WORKDIR/p115-missing.json")"

status="$(record_milestone_on "$proof_job" "verify-p115-nofile-$$" \
  "{\"milestone\":\"en_route_to_pickup\",\"proof\":{\"object_key\":\"$proof115_missing\"}}" nofile)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/p115-nofile.json"; fail "a milestone naming an object nobody uploaded answered $status, want 409"; }
[[ "$(json "$WORKDIR/p115-nofile.json" '["error"]["code"]')" == "delivery_proof_not_uploaded" ]] \
  || { cat "$WORKDIR/p115-nofile.json"; fail "expected code=delivery_proof_not_uploaded"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$proof_job';")" == "0" ]] \
  || fail "a milestone was recorded for a photograph that was never sent"
ok "a URL was issued, the client never uploaded, and the platform refuses to record it as proof — it asked the store rather than taking the client's word"

# --- the upload, and then the record ------------------------------------------------------------

printf '%s' 'not a photograph, but exactly forty-eight bytes.' > "$WORKDIR/p115-proof.bin"

proof115_key="$(proof_url_for "$proof_job" real "$WORKDIR/p115-real.json")"
upload_to "$WORKDIR/p115-real.json" "$WORKDIR/p115-proof.bin"

status="$(record_milestone_on "$proof_job" "verify-p115-record-$$" \
  "{\"milestone\":\"en_route_to_pickup\",\"proof\":{\"object_key\":\"$proof115_key\"}}" record)"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/p115-record.json"; fail "recording a milestone with proof answered $status, want 201"; }
proof115_milestone="$(json "$WORKDIR/p115-record.json" '["id"]')"
ok "the photograph is uploaded straight to the store, and the milestone that cites it is accepted"

# The link, read from the table rather than from what the endpoint said about itself. This is the
# *Done when*: linked to a job and to a milestone.
proof115_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select (p.job_id = '$proof_job') || ' ' || (p.milestone_id = '$proof115_milestone')
        || ' ' || p.content_type || ' ' || p.content_length || ' ' || (length(p.etag) > 0)
     from proofs p where p.object_key = '$proof115_key';")"
[[ "$proof115_row" == "true true image/jpeg 48 true" ]] \
  || fail "the proof row is '$proof115_row', want 'true true image/jpeg 48 true'"
ok "the row names this job and this milestone, and holds the type, the size and the tag the store reported — not what the client said it would send"

# The composite foreign key, from outside Go. A proof whose job disagrees with its milestone's job
# is the row this table exists to make unwritable, and no endpoint can produce it.
proof115_other="$(delivery_awarded_job proof115-other)"
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 >/dev/null 2>&1 <<SQL
INSERT INTO proofs (id, job_id, milestone_id, object_key, content_type, content_length, etag)
VALUES (gen_random_uuid(), '$proof115_other', '$proof115_milestone', 'proof/x/y', 'image/jpeg', 1, 'e');
SQL
then
  fail "a proof row named one job and a milestone recorded on another"
fi
ok "and the pair cannot be forged even in SQL — the composite key makes 'this proof's job is its milestone's job' the database's"

# --- who may read it ----------------------------------------------------------------------------

status="$(read_proof "$delivery_customer_token" "$proof_job" customer)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/p115-read-customer.json"; fail "the job's customer reading proof answered $status"; }
[[ "$(json "$WORKDIR/p115-read-customer.json" '["data"][0]["object_key"]')" == "$proof115_key" ]] \
  || fail "the customer's read does not carry the object just recorded"
[[ "$(json "$WORKDIR/p115-read-customer.json" '["data"][0]["milestone_id"]')" == "$proof115_milestone" ]] \
  || fail "the record does not name the milestone it proves"
ok "the customer who owns the job sees the proof on it — Docs/01 §4.4's acceptance measure"

proof115_download="$(json "$WORKDIR/p115-read-customer.json" '["data"][0]["download_url"]')"
[[ "$proof115_download" == "$STORAGE_ENDPOINT/"* ]] \
  || fail "download_url is $proof115_download, which is not the object store at $STORAGE_ENDPOINT"
[[ "$proof115_download" != *"localhost:$VERIFY_PORT"* ]] \
  || fail "download_url points back at this API; the bytes are never proxied (Docs/06 §5.2)"
ok "and it is a signed URL at the object store rather than a route on this API"

proof115_bytes="$(curl -s "$proof115_download")"
[[ "$proof115_bytes" == "not a photograph, but exactly forty-eight bytes." ]] \
  || fail "the signed download read back as '$proof115_bytes'"
ok "the URL fetches the photograph the driver uploaded, straight from the store"

proof115_unsigned="$(curl -s -o /dev/null -w '%{http_code}' "$STORAGE_ENDPOINT/$STORAGE_BUCKET/$proof115_key")"
[[ "$proof115_unsigned" == "403" ]] \
  || fail "the same object answered $proof115_unsigned unsigned, want 403 — a proof photograph identifies an address and a recipient"
ok "and the same object is refused without a signature: the only way to a photograph is a URL issued after an authorisation check"

# ---------------------------------------------------------------------------------------
ticket "SHIP-15r  a download URL is signed for a shorter window than an upload URL"

# The two lifetimes were one number until SHIP-15r, and the reason they are two is demonstrated here
# rather than in a unit test: what is being shown is that the *service* asks for different windows in
# the two directions, over the wire, against a real signer.
#
# Neither number is typed. internal/config owns both, and a check that hard-coded five minutes and
# fifteen would keep passing after somebody stopped reading them — the same reasoning as SHIP-114's
# upload window above, whose measured figure this compares against.
proof115_download_window="$(python3 -c '
import sys, urllib.parse
query = urllib.parse.parse_qs(urllib.parse.urlsplit(sys.argv[1]).query)
print(int(query["X-Amz-Expires"][0]))
' "$proof115_download")"

(( proof115_download_window > 0 && proof115_download_window <= 3600 )) \
  || fail "the download is signed to last $proof115_download_window seconds; internal/config caps both lifetimes at an hour because nothing revokes either"
ok "the download URL carries its own signed window — $proof115_download_window seconds"

(( proof115_download_window < proof_expires_seconds )) \
  || fail "the download is signed for $proof115_download_window seconds against an upload's $proof_expires_seconds; a read link is a live link to a photograph and must not inherit the window a slow PUT needs"
ok "and it is shorter than the upload's $proof_expires_seconds seconds — the two directions no longer share one lifetime"


status="$(read_proof "$delivery_provider_token" "$proof_job" provider)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/p115-read-provider.json"; fail "the awarded provider reading proof answered $status"; }
[[ "$(json "$WORKDIR/p115-read-provider.json" '["data"][0]["object_key"]')" == "$proof115_key" ]] \
  || fail "the provider's read does not carry the object they recorded"
ok "the provider who was awarded the job sees it too, and the same record — a photograph is not a field that can be redacted for one party"

status="$(read_proof "$delivery_other_token" "$proof_job" stranger)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/p115-read-stranger.json"; fail "a provider who won nothing read the proof: $status"; }
[[ "$(json "$WORKDIR/p115-read-stranger.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/p115-read-stranger.json"; fail "expected code=not_found; a 403 would confirm the delivery exists"; }
# The same request against a job that does not exist. Compared on the code *and* the message rather
# than on the whole body, because every error envelope carries its own request id (SHIP-12) and two
# responses can never be byte-identical over HTTP — the Go test in internal/delivery is where the
# whole body is held to the whole body.
read_proof "$delivery_other_token" "$("$PSQL" "$DATABASE_URL" -tAc 'select gen_random_uuid();')" absent >/dev/null
[[ "$(json "$WORKDIR/p115-read-stranger.json" '["error"]["code"]')" \
   == "$(json "$WORKDIR/p115-read-absent.json" '["error"]["code"]')" ]] \
  || fail "a stranger's 404 carries a different code from a missing job's"
[[ "$(json "$WORKDIR/p115-read-stranger.json" '["error"]["message"]')" \
   == "$(json "$WORKDIR/p115-read-absent.json" '["error"]["message"]')" ]] \
  || fail "a stranger's 404 is worded differently from a missing job's, which tells a competitor the job exists"
ok "a reader who is neither party gets what a job that does not exist gets — same status, same code, same words"

status="$(curl -s -o "$WORKDIR/p115-read-anon.json" -w '%{http_code}' \
  "http://localhost:$VERIFY_PORT/v1/jobs/$proof_job/delivery/proof")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/p115-read-anon.json"; fail "proof was readable with no credential: $status"; }
ok "and it cannot be reached without one at all"

# --- one object, one milestone ------------------------------------------------------------------

status="$(record_milestone_on "$proof_job" "verify-p115-twice-$$" \
  "{\"milestone\":\"picked_up\",\"proof\":{\"object_key\":\"$proof115_key\"}}" twice)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/p115-twice.json"; fail "one object became proof of two milestones: $status"; }
[[ "$(json "$WORKDIR/p115-twice.json" '["error"]["code"]')" == "delivery_proof_already_recorded" ]] \
  || { cat "$WORKDIR/p115-twice.json"; fail "expected code=delivery_proof_already_recorded"; }
ok "the same photograph cannot become the proof of a second milestone — one object is evidence for one recorded claim"

status="$(record_milestone_on "$proof_job" "verify-p115-else-$$" \
  "{\"milestone\":\"picked_up\",\"proof\":{\"object_key\":\"proof/$proof115_other/00000000-0000-7000-8000-000000000000\"}}" else)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/p115-else.json"; fail "another job's object key was accepted: $status"; }
[[ "$(json "$WORKDIR/p115-else.json" '["error"]["details"][0]["field"]')" == "proof.object_key" ]] \
  || { cat "$WORKDIR/p115-else.json"; fail "the refusal does not name proof.object_key"; }
ok "and a key issued against another job is refused on the string alone, before the store is asked anything"

"${COMPOSE[@]}" exec -T minio sh -c \
  'mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; \
   mc rm --force "local/'"$STORAGE_BUCKET/$proof115_key"'" >/dev/null 2>&1 || true' \
  || fail "could not remove the object this section uploaded"
ok "the object this run uploaded was removed from $STORAGE_BUCKET"

# ---------------------------------------------------------------------------------------
ticket "SHIP-116  a reasoned exception is recorded in place of a photograph"

# # What only this section can show
#
# The Go tests stub the object store, so "nothing was uploaded and nothing was asked of the store"
# is asserted there against a stub. Here it is the real MinIO from SHIP-15p, the real signer, and
# the running binary: an exception is recorded with **no bucket interaction at all**, and the row
# the running service wrote is read back out of PostgreSQL rather than out of its own response.
#
# The composition root's half is the same half every section in this file exists for. The exception
# takes cmd/api's `jobLifecycle` and `acceptedBids` exactly as a photograph does, and a wiring that
# only worked for one of the two would pass every Go test in internal/delivery.

exc_job="$(delivery_awarded_job exception)"

# --- the delivery that could not be photographed ------------------------------------------------

status="$(record_milestone_on "$exc_job" "verify-p116-record-$$" \
  '{"milestone":"en_route_to_pickup","reason":"the recipient asked me not to photograph their door","proof":{"exception_reason":"recipient_objected"}}' \
  exc-record)"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/p115-exc-record.json"; fail "recording a milestone with a reasoned exception answered $status, want 201"; }
exc_milestone="$(json "$WORKDIR/p115-exc-record.json" '["id"]')"
ok "a milestone whose photograph was impossible is accepted with a reason in its place — Docs/01 §4.4's exception path, which must never leave a driver unable to finish"

# The *Done when*, read out of the table rather than out of the response: a reason, and no object.
# `is null` on all four rather than an emptiness test, because 000604's CHECK counts NULLs and a row
# holding empty strings would pass it by looking like a photograph.
exc_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select p.exception_reason || ' ' || (p.object_key is null) || ' ' || (p.content_type is null)
        || ' ' || (p.content_length is null) || ' ' || (p.etag is null)
        || ' ' || (p.milestone_id = '$exc_milestone')
     from proofs p where p.job_id = '$exc_job';")"
[[ "$exc_row" == "recipient_objected true true true true true" ]] \
  || fail "the exception row is '$exc_row', want 'recipient_objected true true true true true'"
ok "the row holds the reason, names the milestone it stands behind, and carries no object at all"

# --- what the database refuses whoever is asking ------------------------------------------------

# The invariant as a CHECK, from outside Go. Neither of these rows is reachable through any endpoint
# — the service refuses both before the insert — and that is exactly why they are exercised here.
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 >/dev/null 2>&1 <<SQL
INSERT INTO proofs (id, job_id, milestone_id) VALUES (gen_random_uuid(), '$exc_job', '$exc_milestone');
SQL
then
  fail "a proofs row was written with neither a photograph nor a reason"
fi
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 >/dev/null 2>&1 <<SQL
INSERT INTO proofs (id, job_id, milestone_id, object_key, content_type, content_length, etag, exception_reason)
VALUES (gen_random_uuid(), '$exc_job', '$exc_milestone', 'proof/x/both', 'image/jpeg', 1, 'e', 'camera_unavailable');
SQL
then
  fail "a photograph was recorded alongside a reason there is none"
fi
ok "and in raw SQL the table refuses evidence that is neither and evidence that is both — the reason it is one table rather than two"

# --- both parties read it, and no URL is signed for it ------------------------------------------

status="$(read_proof "$delivery_customer_token" "$exc_job" exc-customer)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/p115-read-exc-customer.json"; fail "the customer reading an exception answered $status"; }
[[ "$(json "$WORKDIR/p115-read-exc-customer.json" '["data"][0]["exception_reason"]')" == "recipient_objected" ]] \
  || { cat "$WORKDIR/p115-read-exc-customer.json"; fail "the customer's read does not carry the reason"; }
[[ "$(json "$WORKDIR/p115-read-exc-customer.json" '["data"][0].get("download_url")')" == "None" ]] \
  || { cat "$WORKDIR/p115-read-exc-customer.json"; fail "a download URL was minted for a delivery with no photograph"; }
ok "the customer is shown why there is no photograph, and no signed URL is minted for an object that does not exist"

status="$(read_proof "$delivery_provider_token" "$exc_job" exc-provider)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/p115-read-exc-provider.json"; fail "the awarded provider reading an exception answered $status"; }
[[ "$(json "$WORKDIR/p115-read-exc-provider.json" '["data"][0]["exception_reason"]')" == "recipient_objected" ]] \
  || fail "the provider's read does not carry the reason they recorded"
ok "and the provider sees the same record, on the same reasoning a photograph is not redacted for one party"

# --- what a client can get wrong ----------------------------------------------------------------

status="$(record_milestone_on "$exc_job" "verify-p116-both-$$" \
  "{\"milestone\":\"picked_up\",\"proof\":{\"object_key\":\"$proof115_key\",\"exception_reason\":\"camera_unavailable\"}}" exc-both)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/p115-exc-both.json"; fail "a photograph and a reason together answered $status, want 422"; }
[[ "$(json "$WORKDIR/p115-exc-both.json" '["error"]["details"][0]["field"]')" == "proof.exception_reason" ]] \
  || { cat "$WORKDIR/p115-exc-both.json"; fail "the refusal does not name proof.exception_reason"; }

status="$(record_milestone_on "$exc_job" "verify-p116-neither-$$" \
  '{"milestone":"picked_up","proof":{}}' exc-neither)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/p115-exc-neither.json"; fail "an empty proof object answered $status, want 422"; }
[[ "$(json "$WORKDIR/p115-exc-neither.json" '["error"]["details"][0]["field"]')" == "proof.object_key" ]] \
  || { cat "$WORKDIR/p115-exc-neither.json"; fail "the refusal does not name proof.object_key"; }

status="$(record_milestone_on "$exc_job" "verify-p116-unknown-$$" \
  '{"milestone":"picked_up","proof":{"exception_reason":"it was raining"}}' exc-unknown)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/p115-exc-unknown.json"; fail "an unpublished reason answered $status, want 422"; }
[[ "$(json "$WORKDIR/p115-exc-unknown.json" '["error"]["details"][0]["message"]')" == *"recipient_objected, camera_unavailable, location_unsafe"* ]] \
  || { cat "$WORKDIR/p115-exc-unknown.json"; fail "the refusal does not tell the client which three reasons there are"; }
ok "both together, neither, and a reason nobody published are each refused with the field named and the three published"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from proofs where job_id = '$exc_job';")" == "1" ]] \
  || fail "a refused request left evidence behind on the job"
ok "and none of the three refusals wrote anything"

# ---------------------------------------------------------------------------------------
ticket "SHIP-118  Delivered is refused with neither proof nor an exception, and accepted with either"

# # What only this section can show
#
# CLAUDE.md's invariant — "Delivered requires photo proof or a recorded exception reason — never
# neither" — end to end on the running binary, through the composition root's own `MoveToDelivered`.
# The Go tests hold both layers separately; this is where they are the same platform.
#
# The `In transit → Delivered` transition is cmd/api's `jobLifecycle` translating the guard, and it
# was the one move `delivery.Jobs` deliberately did not declare until this ticket. A wiring that
# reached Delivered without the domain's check, or a domain check with no wiring behind it, would
# both pass every Go test in internal/delivery and fail here.

deliv_job="$(delivery_awarded_job delivered)"
# One call per step rather than a loop over pairs: three of the four statuses contain a space, so
# anything that word-splits them writes "En" into job_status_history and the CHECK refuses it.
delivery_move "$deliv_job" Awarded "En route to pickup"
delivery_move "$deliv_job" "En route to pickup" "Picked up"
delivery_move "$deliv_job" "Picked up" "In transit"

# --- neither, which is the refusal the invariant is ---------------------------------------------

status="$(record_milestone_on "$deliv_job" "verify-p118-neither-$$" \
  '{"milestone":"delivered"}' deliv-neither)"
[[ "$status" == "409" ]] \
  || { cat "$WORKDIR/p115-deliv-neither.json"; fail "a delivery with nothing behind it answered $status, want 409"; }
[[ "$(json "$WORKDIR/p115-deliv-neither.json" '["error"]["code"]')" == "delivery_proof_required" ]] \
  || { cat "$WORKDIR/p115-deliv-neither.json"; fail "expected code=delivery_proof_required"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$deliv_job';")" == "In transit" ]] \
  || fail "the job reached Delivered with neither a photograph nor a reason"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$deliv_job';")" == "0" ]] \
  || fail "a milestone row survived a refused delivery"
ok "a delivery recorded with neither a photograph nor a reason is refused, the job does not move, and nothing is written — CLAUDE.md's invariant, demonstrated"

# The database's own copy of it, past the service entirely. The insert succeeds and the COMMIT is
# what fails, which is why the trigger is deferred: evidence points at the milestone, so it can only
# ever be written second.
if "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 >/dev/null 2>&1 <<SQL
BEGIN;
INSERT INTO milestones (id, job_id, milestone, actor_type, actor_id, actor_recorded_at)
VALUES (gen_random_uuid(), '$deliv_job', 'Delivered', 'provider', '$delivery_provider_id', now());
COMMIT;
SQL
then
  fail "a delivered milestone was committed in raw SQL with nothing behind it"
fi
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$deliv_job';")" == "0" ]] \
  || fail "the refused transaction left a milestone behind"
ok "and the same row is refused in raw SQL at COMMIT — the rule holds for a writer that never read the service"

# --- a reasoned exception, which is the whole of Docs/01 §4.4's second path ---------------------

status="$(record_milestone_on "$deliv_job" "verify-p118-exc-$$" \
  '{"milestone":"delivered","reason":"handed over at the loading dock","proof":{"exception_reason":"location_unsafe"}}' \
  deliv-exc)"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/p115-deliv-exc.json"; fail "a delivery evidenced by a reasoned exception answered $status, want 201"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$deliv_job';")" == "Delivered" ]] \
  || fail "the job did not move to Delivered on an accepted delivery"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
   "select count(*) from job_status_history where job_id = '$deliv_job' and to_status = 'Delivered';")" == "1" ]] \
  || fail "the move to Delivered left no history row; it went round the guard"
ok "the same delivery with a reason in place of the photograph is accepted, and the job moves through the transition guard"

# What SHIP-117 will read and what X-6 will be decided about — asserted here so neither finds it
# missing. Nothing in this section flags the job or completes it: both are those tickets'.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
   "select p.exception_reason from proofs p join milestones m on m.id = p.milestone_id
     where p.job_id = '$deliv_job' and m.milestone = 'Delivered';")" == "location_unsafe" ]] \
  || fail "an exception-completed delivery cannot be found by joining its evidence to its milestone"
ok "and an exception-completed job is identifiable by a join, which is what SHIP-117's queue and X-6's decision both start from"

# --- a photograph, which is the ordinary path ---------------------------------------------------

deliv_photo_job="$(delivery_awarded_job delivered-photo)"
delivery_move "$deliv_photo_job" Awarded "En route to pickup"
delivery_move "$deliv_photo_job" "En route to pickup" "Picked up"
delivery_move "$deliv_photo_job" "Picked up" "In transit"

deliv_key="$(proof_url_for "$deliv_photo_job" deliv "$WORKDIR/p118-url.json")"
upload_to "$WORKDIR/p118-url.json" "$WORKDIR/p115-proof.bin"

status="$(record_milestone_on "$deliv_photo_job" "verify-p118-photo-$$" \
  "{\"milestone\":\"delivered\",\"proof\":{\"object_key\":\"$deliv_key\"}}" deliv-photo)"
[[ "$status" == "201" ]] \
  || { cat "$WORKDIR/p115-deliv-photo.json"; fail "a photographed delivery answered $status, want 201"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$deliv_photo_job';")" == "Delivered" ]] \
  || fail "a photographed delivery did not move the job"
ok "and a delivery evidenced by a photograph is accepted the same way — both halves of the invariant reach Delivered, and nothing else does"

"${COMPOSE[@]}" exec -T minio sh -c \
  'mc alias set local http://127.0.0.1:9000 "$MINIO_ROOT_USER" "$MINIO_ROOT_PASSWORD" >/dev/null; \
   mc rm --force "local/'"$STORAGE_BUCKET/$deliv_key"'" >/dev/null 2>&1 || true' \
  || fail "could not remove the object this section uploaded"
ok "the object this section uploaded was removed from $STORAGE_BUCKET"

# ---------------------------------------------------------------------------------------
ticket "SHIP-136  every delivery state change emits its event from the domain, into the outbox"

# Docs/01 §4.5's "delivery status changes", read to include everything Docs/01 §4.4 asks an actor to
# record — which is more than the job's status carries.
#
# # What is demonstrated here and what is demonstrated by tests
#
# internal/delivery/events_test.go holds the payloads, the retry and absorption paths, the ordering of
# the milestone event and the evidence event, and the rolled-back transaction that emits nothing.
# cmd/api/events_domain_test.go holds the half of the *Done when* that is about where the emit lives.
#
# **What only this can show is that every assignment, milestone and piece of evidence recorded through
# the served binary above left its event behind** — through cmd/api's own wiring rather than a
# fixture's. 80-notifications.sh takes them the rest of the way onto `shipper.delivery`.
#
# # The fence, and why it is by job identifier
#
# The aggregate id of a delivery event is the **job**, so this section's jobs are its own fence: each
# is created by `delivery_awarded_job` in this run and named nowhere else. A count over
# `aggregate_type = 'delivery'` would include every previous `make verify` on this database, which
# persists between runs.
#
# Nothing here reads Kafka and nothing here starts the worker: these rows are deliberately left
# unpublished.

# delivery_events_on <job> <event_type> — how many of one event type a job produced.
delivery_events_on() {
  "$PSQL" "$DATABASE_URL" -tAc \
    "select count(*) from outbox
      where aggregate_type = 'delivery' and aggregate_id = '$1' and event_type = '$2';"
}

# The assignment. `$delivery_job` had a driver put on it by the first section of this file.
[[ "$(delivery_events_on "$delivery_job" delivery.driver_assigned)" -ge 1 ]] \
  || fail "assigning a driver through the endpoint emitted no delivery.driver_assigned"
ok "putting a driver on a job emits its event, alongside the job's own transition"

# **And the driver's name and mobile are not on it.** Docs/01 §5.1: an event travels through the
# outbox onto a topic with seven days of retention and into every consumer there will ever be. The
# assignment above nominated "Sam Patel" on +61412345678, so this is a real assertion.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox
     where aggregate_type = 'delivery' and aggregate_id = '$delivery_job'
       and (payload::text ilike '%Patel%' or payload::text like '%41234567%');")" == "0" ]] \
  || fail "an assignment event carries the driver's name or number — Docs/01 §5.1 keeps both off the wire"
ok "and it carries neither the driver's name nor their number, which a consumer reads from the row"

# The milestone. `$milestone_job` had several recorded against it, including one the job had already
# moved past — which writes a row, moves nothing, and therefore emits **no** job.status_changed.
# Before this ticket that recording was invisible to everything downstream.
[[ "$(delivery_events_on "$milestone_job" delivery.milestone_recorded)" -ge 1 ]] \
  || fail "recording a milestone through the endpoint emitted no delivery.milestone_recorded"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox
     where aggregate_type = 'delivery' and aggregate_id = '$milestone_job'
       and event_type = 'delivery.milestone_recorded' and (payload->>'job_moved')::boolean is false;")" -ge 1 ]] \
  || fail "no milestone event reports job_moved false, and this file recorded one the job had moved past"
ok "a milestone emits its event whether or not the job moved, and says which — the case job.status_changed cannot report"

# One event per row, which is what the milestone table is the authority on. A count that disagreed
# would mean either a recording that emitted twice or one that emitted not at all.
[[ "$(delivery_events_on "$milestone_job" delivery.milestone_recorded)" \
   == "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from milestones where job_id = '$milestone_job';")" ]] \
  || fail "the milestone events and the milestone rows on $milestone_job do not agree"
ok "and there is exactly one event per recorded milestone — a retry answered from the record emits nothing"

# The evidence, both kinds. `$deliv_job` was delivered on a reasoned exception and
# `$deliv_photo_job` on a photograph, both through the endpoint.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select payload->>'exception_reason' from outbox
     where aggregate_type = 'delivery' and aggregate_id = '$deliv_job'
       and event_type = 'delivery.proof_recorded' and (payload->>'is_exception')::boolean;")" == "location_unsafe" ]] \
  || fail "the exception-completed delivery emitted no proof event naming its reason"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox
     where aggregate_type = 'delivery' and aggregate_id = '$deliv_photo_job'
       and event_type = 'delivery.proof_recorded' and (payload->>'is_exception')::boolean is false;")" -ge 1 ]] \
  || fail "the photographed delivery emitted no proof event"
ok "a photograph and a reasoned exception each emit the same event, told apart by is_exception — Docs/04 §5's queue reads the second"

# **And the object key is not on it.** Where the bytes are is a storage locator, and who may look at
# them is decided by GET /v1/jobs/{id}/delivery/proof after an authorisation check — which is not a
# decision a consumer of a topic is in a position to make.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from outbox
     where aggregate_type = 'delivery' and aggregate_id = '$deliv_photo_job'
       and payload::text like '%$deliv_key%';")" == "0" ]] \
  || fail "a proof event carries the object key; a signed URL is issued after an authorisation check, not from a topic"
ok "and no proof event carries the object key — the photograph is reached through the endpoint that checks who is asking"

# The order, which on this aggregate is a guarantee rather than a preference: both events are about
# the job, so both key onto one partition and Kafka keeps their order. Evidence naming a milestone a
# consumer has not seen yet is what this avoids.
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select event_type from outbox
     where aggregate_type = 'delivery' and aggregate_id = '$deliv_photo_job'
       and event_type in ('delivery.milestone_recorded','delivery.proof_recorded')
     order by id desc limit 1;")" == "delivery.proof_recorded" ]] \
  || fail "the evidence event was written before the milestone it stands behind"
ok "the evidence follows the claim it stands behind, in the outbox and therefore on the partition"

unset deliv_key
unset -f delivery_events_on
