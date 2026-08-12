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
