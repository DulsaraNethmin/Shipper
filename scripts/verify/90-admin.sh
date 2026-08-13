# shellcheck shell=bash
#
# M6 admin — SHIP-149's append-only audit log, and SHIP-163's dispute intake.
#
# Sourced by scripts/verify-foundation.sh; see 00-stack.sh for what the runner provides.
#
# 90–94 is the admin range. This file is admin's to append to, and no other track's to edit — which
# is the whole reason the script was split (Docs/11 §9, SHIP-15e).
#
# SHIP-149 was pulled a long way forward of the rest of M6 deliberately: an audit trail cannot be
# backfilled, so it exists before there is anything privileged to record.

ticket "SHIP-149  the audit log is append-only in the database, not by convention"

# -q matters here and only here. Without it psql appends the command tag to the result, so
# this captures "<uuid>\nINSERT 0 1" rather than a uuid — and the two checks below then fail
# with `invalid input syntax for type uuid` instead of with the append-only trigger, which
# their `if` cannot tell apart from success. Both reported a pass while exercising nothing.
# The other captures in this file are SELECTs, which emit no tag under -tA.
audit_id="$("$PSQL" "$DATABASE_URL" -qtAc \
  "insert into audit_log (id, actor_type, action, target_type, target_id)
   values (gen_random_uuid(), 'system', 'job.expired', 'job', gen_random_uuid())
   returning id;")"
[[ -n "$audit_id" ]] || fail "an audit entry could not be appended"
ok "an entry can be appended"

if "$PSQL" "$DATABASE_URL" -q -c \
  "update audit_log set reason = 'rewritten' where id = '$audit_id';" >/dev/null 2>&1; then
  fail "an audit entry was rewritten"
fi
ok "UPDATE is refused"

if "$PSQL" "$DATABASE_URL" -q -c \
  "delete from audit_log where id = '$audit_id';" >/dev/null 2>&1; then
  fail "an audit entry was deleted"
fi
ok "DELETE is refused, so the trail survives a psql prompt"

# ==========================================================================================
# SHIP-163 — dispute intake.
#
# # This section is the only place the composition root's half of SHIP-163 is exercised
#
# The Go tests in internal/admin drive the domain against a real database, but they supply their own
# copies of the two ports — a test file may import `jobs`, and cmd/api's adapters live in package
# main where no test has a database to reach. So the party lookup that spans `jobs` and `bids`, and
# the translation of the guard's refusals, are demonstrated here against the built binary or nowhere.
#
# # The fixtures reach a state no endpoint can reach yet
#
# SHIP-63 publishes, SHIP-92 awards, and SHIP-114 records a delivery. None exists, so the job is
# moved with the protocol 000402's trigger demands — a job_status_history row written in the same
# transaction, named by a transaction-local setting — and the award is one accepted bid, which is
# what uq_bids_one_accepted_per_job makes singular. A bare `UPDATE jobs SET status` is refused, so
# even the fixture cannot bypass the guard.
#
# The tokens are minted by the runner and every one of them claims `role: customer`. That is the
# right thing to exercise: this endpoint decides from the job and its accepted bid, not from a claim
# in a token. It also means nothing here signs in, so SHIP-47's per-address bucket is untouched.

# --- the accounts and the delivered job these checks run against ---------------------------------
#
# **The mobile prefix is 0419, and it is this section's alone.** Every account in `make verify` is
# registered against one database that is not reset between runs, so the fixture numbers are a shared
# namespace: 0413x is jobs', 0414x is fleet's, 0417x is delivery's and 04180 is the outbox section's.
# A section reusing another's prefix fails on identity_phone_taken at its very first registration,
# which is a confusing way to be told that two files disagree about a number.

status="$(post_json "verify-adm-cust-$$" /v1/auth/register \
  "{\"email\":\"dispute-customer-$$@example.com\",\"phone\":\"04190$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"customer\"}" \
  "$WORKDIR/dispute-customer.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/dispute-customer.json"; fail "could not register the dispute customer: $status"; }
dispute_customer_id="$(json "$WORKDIR/dispute-customer.json" '["id"]')"
dispute_customer_token="$(mint_token "$dispute_customer_id")"

status="$(post_json "verify-adm-prov-$$" /v1/auth/register \
  "{\"email\":\"dispute-provider-$$@example.com\",\"phone\":\"04191$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/dispute-provider.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/dispute-provider.json"; fail "could not register the dispute provider: $status"; }
dispute_provider_id="$(json "$WORKDIR/dispute-provider.json" '["id"]')"
dispute_provider_token="$(mint_token "$dispute_provider_id")"

status="$(post_json "verify-adm-other-$$" /v1/auth/register \
  "{\"email\":\"dispute-other-$$@example.com\",\"phone\":\"04192$$\",\"password\":\"correct-horse-battery-staple\",\"role\":\"provider\"}" \
  "$WORKDIR/dispute-other.json")"
[[ "$status" == "201" ]] || { cat "$WORKDIR/dispute-other.json"; fail "could not register the second provider: $status"; }
dispute_other_id="$(json "$WORKDIR/dispute-other.json" '["id"]')"
dispute_other_token="$(mint_token "$dispute_other_id")"

# dispute_draft <name> — a fresh draft owned by the dispute customer, answering with its id.
dispute_draft() {
  local out="$WORKDIR/dispute-$1.json"
  local created
  created="$(curl -s -X POST -o "$out" -w '%{http_code}' \
    -H "$auth_header: Bearer $dispute_customer_token" -H "Idempotency-Key: verify-adm-$1-$$" \
    -H 'Content-Type: application/json' -d '{}' \
    "http://localhost:$VERIFY_PORT/v1/jobs")"
  [[ "$created" == "201" ]] || { cat "$out"; fail "could not create a draft for $1"; }
  json "$out" '["id"]'
}

# dispute_move <job-id> <from> <to> — one guarded transition, exactly as 000402 requires.
#
# The SQL is single-quoted on purpose: `$$` in a double-quoted shell string is the process id, which
# is how a dollar-quoted PL/pgSQL block would silently become nonsense. Values come in as psql
# variables instead.
dispute_move() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 \
    -v job="$1" -v from_status="$2" -v to_status="$3" -v actor="$dispute_customer_id" \
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

# dispute_award <job-id> <provider-id> — the accepted bid SHIP-92 will write.
dispute_award() {
  "$PSQL" "$DATABASE_URL" -q -v ON_ERROR_STOP=1 -v job="$1" -v provider="$2" >/dev/null <<'SQL'
INSERT INTO bids (id, job_id, provider_id, status, amount)
VALUES (gen_random_uuid(), :'job', :'provider', 'Accepted', 450.00);
SQL
}

# dispute_delivered_job <name> — a job published, awarded, driven and delivered. The state a dispute
# is most often raised from, and the one Docs/02 §6.1's seventy-two-hour window runs against.
dispute_delivered_job() {
  local job
  job="$(dispute_draft "$1")"
  dispute_move "$job" Draft Open
  dispute_move "$job" Open Awarded
  dispute_award "$job" "$dispute_provider_id"
  dispute_move "$job" Awarded 'En route to pickup'
  dispute_move "$job" 'En route to pickup' 'Picked up'
  dispute_move "$job" 'Picked up' 'In transit'
  dispute_move "$job" 'In transit' Delivered
  printf '%s' "$job"
}

# dispute_request <token> <key> <job-id> <body> <name> — one intake, keeping the response headers.
#
# The headers are the point for the retry checks: `Idempotency-Replayed` is how a check says *which*
# mechanism answered, and asserting on the status alone could not tell Redis from the index.
dispute_request() {
  curl -s -X POST -o "$WORKDIR/dsp-$5.json" -D "$WORKDIR/dsp-$5.headers" -w '%{http_code}' \
    -H "$auth_header: Bearer $1" -H "Idempotency-Key: $2" \
    -H 'Content-Type: application/json' -d "$4" \
    "http://localhost:$VERIFY_PORT/v1/jobs/$3/disputes"
}

# dispute_replayed <name> — whether the middleware answered that request from its store.
dispute_replayed() {
  grep -qi '^idempotency-replayed: true' "$WORKDIR/dsp-$1.headers"
}

# dispute_forget <subject> <key> — delete the middleware's entry, as a TTL expiry would.
#
# The key is namespaced by the authenticated subject (SHIP-44), which is what stops one client
# reading another's stored response.
dispute_forget() {
  redis-cli -u "$REDIS_URL" del "idem:v1:user:$1:$2" >/dev/null
}

dispute_job="$(dispute_delivered_job intake)"
dispute_incident="$("$PSQL" "$DATABASE_URL" -tAc \
  "select to_char((now() at time zone 'utc') - interval '90 minutes', 'YYYY-MM-DD\"T\"HH24:MI:SS') || 'Z';")"
dispute_body="{\"category\":\"goods_damaged_or_missing\",\"description\":\"Two of the four crates arrived with the sides staved in.\",\"desired_outcome\":\"A record of the damage, and the provider contacted about it.\",\"occurred_at\":\"$dispute_incident\",\"evidence\":[\"Photographed the crates at the depot\",\"The driver's message of 11/08\"]}"

# ---------------------------------------------------------------------------------------
ticket "SHIP-163  POST /v1/jobs/{id}/disputes captures the Docs/04 §7 intake fields and freezes the job"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$dispute_job';")" == "Delivered" ]] \
  || fail "the fixture job is not Delivered, so nothing below is testing what it claims"
ok "a job reaches Delivered only through the guard, and one accepted bid names the provider"

status="$(curl -s -X POST -o "$WORKDIR/dsp-anon.json" -w '%{http_code}' \
  -H "Idempotency-Key: verify-dsp-anon-$$" -H 'Content-Type: application/json' -d "$dispute_body" \
  "http://localhost:$VERIFY_PORT/v1/jobs/$dispute_job/disputes")"
[[ "$status" == "401" ]] || { cat "$WORKDIR/dsp-anon.json"; fail "an unauthenticated intake returned $status, want 401"; }
ok "it cannot be reached without a credential — and RequireAdmin is not what guards it, because the complainant is a user"

status="$(curl -s -X POST -o "$WORKDIR/dsp-nokey.json" -w '%{http_code}' \
  -H "$auth_header: Bearer $dispute_customer_token" -H 'Content-Type: application/json' -d "$dispute_body" \
  "http://localhost:$VERIFY_PORT/v1/jobs/$dispute_job/disputes")"
[[ "$status" == "400" ]] || { cat "$WORKDIR/dsp-nokey.json"; fail "an intake with no Idempotency-Key returned $status, want 400"; }
ok "and not without an Idempotency-Key — the key is what the row is raised under, not just how the retry is absorbed"

# --- who may raise one: Docs/02 §2's "eligible user", and nobody else --------------------

status="$(dispute_request "$dispute_other_token" "verify-dsp-stranger-$$" "$dispute_job" "$dispute_body" stranger)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/dsp-stranger.json"; fail "a provider with no bid on the job raised a dispute: $status"; }
[[ "$(json "$WORKDIR/dsp-stranger.json" '["error"]["code"]')" == "not_found" ]] \
  || { cat "$WORKDIR/dsp-stranger.json"; fail "expected code=not_found"; }
ok "a provider who is not party to the job is refused outright — 404, not a 200 with nothing in it"

status="$(dispute_request "$dispute_customer_token" "verify-dsp-nojob-$$" \
  "00000000-0000-7000-8000-000000000000" "$dispute_body" nojob)"
[[ "$status" == "404" ]] || { cat "$WORKDIR/dsp-nojob.json"; fail "a job that does not exist returned $status, want 404"; }
ok "and a job that does not exist answers identically, so neither confirms the other's existence"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from disputes where job_id = '$dispute_job';")" == "0" ]] \
  || fail "a caller with no right to raise anything wrote a row"
ok "neither refusal left a row behind"

# --- the intake itself, with the incident ninety minutes before the report ---------------

dispute_key="verify-dsp-$$"
status="$(dispute_request "$dispute_customer_token" "$dispute_key" "$dispute_job" "$dispute_body" raise)"
[[ "$status" == "201" ]] || { cat "$WORKDIR/dsp-raise.json"; fail "raising a dispute returned $status, want 201"; }
dispute_id="$(json "$WORKDIR/dsp-raise.json" '["id"]')"
[[ "$dispute_id" =~ ^[0-9a-f-]{36}$ ]] || fail "the response carries no dispute id"
[[ "$(json "$WORKDIR/dsp-raise.json" '["raised_by"]')" == "customer" ]] \
  || fail "raised_by is $(json "$WORKDIR/dsp-raise.json" '["raised_by"]'), want customer — resolved from the job, not from the token's role"
[[ "$(json "$WORKDIR/dsp-raise.json" '["category"]')" == "goods_damaged_or_missing" ]] \
  || fail "the category came back as $(json "$WORKDIR/dsp-raise.json" '["category"]'), want the lower snake case wire form"
ok "a party to the job raises a dispute, and the platform decides which side of it they were on"

# The seven fields of Docs/04 §7, read off the row rather than off the answer the endpoint gave
# about itself. This is the ticket's *Done when*.
dispute_row="$("$PSQL" "$DATABASE_URL" -tAc \
  "select complainant_party || '|' || category || '|' || (description <> '') || '|' ||
          (desired_outcome <> '') || '|' || (occurred_at = '$dispute_incident'::timestamptz) || '|' ||
          cardinality(evidence) || '|' || (created_at > occurred_at)
     from disputes where id = '$dispute_id' and job_id = '$dispute_job'
       and complainant_id = '$dispute_customer_id';")"
[[ "$dispute_row" == "customer|Goods damaged or missing|true|true|true|2|true" ]] \
  || fail "the stored intake is '$dispute_row', want 'customer|Goods damaged or missing|true|true|true|2|true'"
ok "job, complainant, category, description, desired outcome, time of event and evidence are all on the row (Docs/04 §7)"

[[ "$(json "$WORKDIR/dsp-raise.json" '["occurred_at"]')" != "$(json "$WORKDIR/dsp-raise.json" '["raised_at"]')" ]] \
  || fail "occurred_at and raised_at are the same value; the incident and the report have been collapsed"
ok "the incident's time and the report's are two fields and stay two — support reasons about the gap"

# The response's key set, closed. A field added here without a decision would reach a complainant,
# and SHIP-162's internal notes are the field that must never do so.
dispute_keys="$(python3 -c 'import json,sys; print(",".join(sorted(json.load(open(sys.argv[1])).keys())))' "$WORKDIR/dsp-raise.json")"
[[ "$dispute_keys" == "category,description,desired_outcome,evidence,id,job_id,occurred_at,raised_at,raised_by" ]] \
  || fail "the response carries the keys '$dispute_keys'; anything else could be an internal note reaching the complainant (SHIP-162)"
ok "the response is a closed set of keys, so nothing an administrator writes can appear in it by accident"

# --- the job, which is the half of the ticket that is not about the dispute --------------

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$dispute_job';")" == "Disputed" ]] \
  || fail "the job did not freeze; the seventy-two-hour auto-complete would run through an open complaint (Docs/02 §3, §6.1)"
ok "the job is Disputed — one transaction, both facts"

dispute_history="$("$PSQL" "$DATABASE_URL" -tAc \
  "select h.from_status || '->' || h.to_status || ' ' || h.actor_type || ' ' ||
          (h.actor_id = '$dispute_customer_id') || ' ' || coalesce(h.reason, '')
     from job_status_history h
    where h.job_id = '$dispute_job' and h.to_status = 'Disputed';")"
[[ "$dispute_history" == "Delivered->Disputed customer true Goods damaged or missing" ]] \
  || fail "the recorded transition is '$dispute_history', want 'Delivered->Disputed customer true Goods damaged or missing'"
ok "the move went through the guard, is recorded against the complainant, and says why the job froze"

# --- the retry, twice, through each mechanism in turn ------------------------------------

status="$(dispute_request "$dispute_customer_token" "$dispute_key" "$dispute_job" "$dispute_body" replay)"
# **201, not 200, and the difference is the whole point of this pair of checks.** The middleware
# replays the stored response *verbatim*, status included, so a retry it absorbs is indistinguishable
# from the original — that is what makes it cheap. The check below it is the same request answered by
# the handler, which knows it is a retry and says 200.
[[ "$status" == "201" ]] || { cat "$WORKDIR/dsp-replay.json"; fail "the immediate retry returned $status, want the stored 201 replayed"; }
dispute_replayed replay || fail "the immediate retry was not replayed by the middleware, so the cheap path is not working"
ok "the same key immediately after replays the stored 201 from Redis, byte for byte — the handler is never reached"

# The entry is deleted, which is what a TTL expiry, an eviction or a failover looks like from the
# handler's side. **This is the check the ticket turns on**: the request now runs for a second time,
# all the way to the table, and must still raise nothing.
dispute_forget "$dispute_customer_id" "$dispute_key"
status="$(dispute_request "$dispute_customer_token" "$dispute_key" "$dispute_job" "$dispute_body" expired)"
[[ "$status" == "200" ]] || { cat "$WORKDIR/dsp-expired.json"; fail "the retry after the cached response expired returned $status, want 200"; }
! dispute_replayed expired || fail "the entry was deleted and the middleware still replayed; this check is proving nothing"
[[ "$(json "$WORKDIR/dsp-expired.json" '["id"]')" == "$dispute_id" ]] \
  || fail "the retry raised a second dispute: $(json "$WORKDIR/dsp-expired.json" '["id"]') is not $dispute_id"
ok "with the cached response gone the request runs again, reaches the table, and is answered from the row it already wrote"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from disputes where job_id = '$dispute_job';")" == "1" ]] \
  || fail "one key raised more than one dispute"
[[ "$("$PSQL" "$DATABASE_URL" -tAc \
  "select count(*) from job_status_history where job_id = '$dispute_job' and to_status = 'Disputed';")" == "1" ]] \
  || fail "the retry froze the job a second time"
ok "one key, three requests, one dispute and one transition — Redis for the first retry and uq_disputes_idempotency for the second"

# A key that raised something else. The middleware refuses this on its fingerprint while its entry
# lives, so the entry is deleted first and the *database* is what answers — with the same code,
# deliberately: a client should not have to know which layer caught it.
dispute_forget "$dispute_customer_id" "$dispute_key"
status="$(dispute_request "$dispute_customer_token" "$dispute_key" "$dispute_job" \
  "$(printf '%s' "$dispute_body" | sed 's/goods_damaged_or_missing/delivery_is_late/')" reused)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/dsp-reused.json"; fail "a key reused for another complaint returned $status, want 409"; }
[[ "$(json "$WORKDIR/dsp-reused.json" '["error"]["code"]')" == "idempotency_key_reused" ]] \
  || { cat "$WORKDIR/dsp-reused.json"; fail "expected code=idempotency_key_reused"; }
ok "the same key against a different complaint is refused by the index, with the code the middleware would have used"

# ---------------------------------------------------------------------------------------
ticket "SHIP-163  a job is frozen once, and a job with no delivery to dispute says so"

# The *other* party to the same delivery, with a key of its own. This is not a retry and must not be
# absorbed as one: it is somebody raising a second dispute on a job that is already frozen.
status="$(dispute_request "$dispute_provider_token" "verify-dsp-second-$$" "$dispute_job" \
  "$(printf '%s' "$dispute_body" | sed 's/goods_damaged_or_missing/customer_unavailable/')" second)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/dsp-second.json"; fail "a second dispute was accepted on a frozen job: $status"; }
[[ "$(json "$WORKDIR/dsp-second.json" '["error"]["code"]')" == "admin_dispute_already_open" ]] \
  || { cat "$WORKDIR/dsp-second.json"; fail "expected code=admin_dispute_already_open"; }
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from disputes where job_id = '$dispute_job';")" == "1" ]] \
  || fail "the refused second dispute wrote a row anyway"
ok "the awarded provider is a party and is still refused while one is open — a job is frozen once, and SHIP-164 unfreezes it by resolving one dispute"

# Docs/02 §2 permits 'Disputed' from Awarded through Delivered and from nowhere else. This job is
# still open for bids, which is the state a client with a stale screen would send.
dispute_open_job="$(dispute_draft not-awarded)"
dispute_move "$dispute_open_job" Draft Open

status="$(dispute_request "$dispute_customer_token" "verify-dsp-open-$$" "$dispute_open_job" "$dispute_body" open)"
[[ "$status" == "409" ]] || { cat "$WORKDIR/dsp-open.json"; fail "a job still open for bids was disputed: $status"; }
[[ "$(json "$WORKDIR/dsp-open.json" '["error"]["code"]')" == "admin_job_not_disputable" ]] \
  || { cat "$WORKDIR/dsp-open.json"; fail "expected code=admin_job_not_disputable"; }
ok "the transition guard refuses the move, and the endpoint says which of the two conflicts it was"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from disputes where job_id = '$dispute_open_job';")" == "0" ]] \
  || fail "a dispute row survived a refused transition"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$dispute_open_job';")" == "Open" ]] \
  || fail "a refused intake moved the job anyway"
ok "and the dispute rolled back with it — a complaint against a job that never froze cannot happen"

# ---------------------------------------------------------------------------------------
ticket "SHIP-163  the time of the event is captured, not defaulted"

dispute_validation_job="$(dispute_delivered_job validation)"

# Docs/04 §7 names "time of event" as an intake field distinct from the report. Omitting it is a
# refusal rather than a default, because a default would write the report's time into the incident's
# column and nothing afterwards could tell that value from one somebody meant.
status="$(dispute_request "$dispute_customer_token" "verify-dsp-notime-$$" "$dispute_validation_job" \
  '{"category":"delivery_is_late","description":"Three days late.","desired_outcome":"An explanation."}' notime)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/dsp-notime.json"; fail "an intake with no occurred_at returned $status, want 422"; }
[[ "$(json "$WORKDIR/dsp-notime.json" '["error"]["details"][0]["field"]')" == "occurred_at" ]] \
  || { cat "$WORKDIR/dsp-notime.json"; fail "the refusal does not name occurred_at"; }
ok "an intake with no time of event is refused rather than stamped with the report's time"

dispute_future="$("$PSQL" "$DATABASE_URL" -tAc \
  "select to_char((now() at time zone 'utc') + interval '2 days', 'YYYY-MM-DD\"T\"HH24:MI:SS') || 'Z';")"
status="$(dispute_request "$dispute_customer_token" "verify-dsp-future-$$" "$dispute_validation_job" \
  "{\"category\":\"delivery_is_late\",\"description\":\"Three days late.\",\"desired_outcome\":\"An explanation.\",\"occurred_at\":\"$dispute_future\"}" future)"
[[ "$status" == "422" ]] || { cat "$WORKDIR/dsp-future.json"; fail "an incident two days from now returned $status, want 422"; }
ok "and one that has not happened yet is refused, while a report of something long past is not"

[[ "$("$PSQL" "$DATABASE_URL" -tAc "select count(*) from disputes where job_id = '$dispute_validation_job';")" == "0" ]] \
  || fail "a refused validation wrote a row"
[[ "$("$PSQL" "$DATABASE_URL" -tAc "select status from jobs where id = '$dispute_validation_job';")" == "Delivered" ]] \
  || fail "a refused validation froze the job"
ok "neither refusal touched the job or the table"
