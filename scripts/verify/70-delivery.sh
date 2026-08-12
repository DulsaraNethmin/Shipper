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
